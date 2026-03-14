package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"golang.org/x/sync/errgroup"
)

// scProductPathRulesPrompt returns the product canonical path rules fragment
// sourced from the single taxonomy definition. Only product-relevant rules.
func scProductPathRulesPrompt() string {
	rules := taxonomy.DefaultCanonicalRules()
	return fmt.Sprintf(`- Products: %s
  category: %s
  type: %s
  stage: %s
  Example: %s
- For services: /product/service/{domain}/{subdomain}/{slug}
- slug: lowercase, hyphens only, no spaces or special chars`,
		rules.Product.Format,
		strings.Join(rules.Product.Categories, " | "),
		strings.Join(rules.Product.Types, " | "),
		strings.Join(rules.Product.Stages, " | "),
		rules.Product.Example,
	)
}

// scOrgPathRulesPrompt returns org canonical path rules from taxonomy.
func scOrgPathRulesPrompt() string {
	rules := taxonomy.DefaultCanonicalRules()
	return fmt.Sprintf(`- Organizations: %s. Example: %s
- slug: lowercase, hyphens only, no spaces or special chars`,
		rules.Org.Format, rules.Org.Example)
}

// scFacPathRulesPrompt returns facility canonical path rules from taxonomy (single source).
func scFacPathRulesPrompt() string {
	return taxonomy.FacilityCanonicalRulesPrompt()
}

// ---------------------------------------------------------------------------
// Step response types for recursive supply chain unwinding
// ---------------------------------------------------------------------------

// SCStepFacility is a facility discovered for an org.
type SCStepFacility struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	FacilityType string `json:"facilityType,omitempty"` // supply chain role: taxonomy.FacilityRolesForPrompts()
	FacilityKind string `json:"facilityKind,omitempty"` // physical type: taxonomy.FacilityKindsForPrompts()
}

// SCStepProduct is a product discovered for a facility.
type SCStepProduct struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// OrgFacResult maps an org to its child orgs and facilities.
type OrgFacResult struct {
	OrgPath    string           `json:"orgPath"`
	ChildOrgs  []string         `json:"childOrgs,omitempty"`
	Facilities []SCStepFacility `json:"facilities,omitempty"`
}

// FacProductResult maps a facility to products it produces.
type FacProductResult struct {
	FacPath  string          `json:"facPath"`
	Products []SCStepProduct `json:"products,omitempty"`
}

// ProductRecipientResult maps a product to recipient facilities.
type ProductRecipientResult struct {
	ProductPath string           `json:"productPath"`
	Recipients  []SCStepFacility `json:"recipients,omitempty"`
}

// FacInputProductsResult maps a facility to input products it consumes (upstream).
type FacInputProductsResult struct {
	FacPath       string          `json:"facPath"`
	InputProducts []SCStepProduct `json:"inputProducts,omitempty"`
}

// ProductSupplierResult maps a product to facilities that produce/supply it (upstream).
type ProductSupplierResult struct {
	ProductPath string           `json:"productPath"`
	Suppliers   []SCStepFacility `json:"suppliers,omitempty"`
}

// FacOrgResult maps a facility to its owning organization.
type FacOrgResult struct {
	FacPath string `json:"facPath"`
	OrgPath string `json:"orgPath"`
	OrgName string `json:"orgName,omitempty"`
}

// ---------------------------------------------------------------------------
// Batch size limits
// ---------------------------------------------------------------------------

const (
	scStepBatchOrgs     = 10
	scStepBatchFacs     = 15
	scStepBatchProducts = 10
)

// Unwind entity caps to keep LLM calls ~50–60 per direction (target ~600 total for both).
// Without caps, geometric expansion can reach 6000+ calls.
const (
	ScUnwindMaxOrgsPerDepth          = 30
	ScUnwindMaxChildOrgsRecurse      = 20
	ScUnwindMaxFacsPerDepth          = 45
	ScUnwindMaxProductsPerDepth      = 60
	ScUnwindMaxRecipientFacsPerDepth = 45
	ScUnwindMaxChildOrgRecurseLevels = 1
)

type scStepPromptContextKey struct{}

// SupplyChainStepPromptContext carries steering context for recursive step prompts.
type SupplyChainStepPromptContext struct {
	OriginalQuery  string
	DomainPath     string
	Geo            string
	Product        string
	Sector         string
	GeoFilters     []string
	ProductFilters []string
	Direction      string
	Depth          int
}

// WithSupplyChainStepPromptContext attaches step context to ctx.
func WithSupplyChainStepPromptContext(ctx context.Context, c SupplyChainStepPromptContext) context.Context {
	return context.WithValue(ctx, scStepPromptContextKey{}, c)
}

func supplyChainPromptContextBlock(ctx context.Context) string {
	raw := ctx.Value(scStepPromptContextKey{})
	c, ok := raw.(SupplyChainStepPromptContext)
	if !ok {
		return ""
	}
	lines := make([]string, 0, 8)
	if s := strings.TrimSpace(c.OriginalQuery); s != "" {
		lines = append(lines, "- originalQuery: "+s)
	}
	if s := strings.TrimSpace(c.DomainPath); s != "" {
		lines = append(lines, "- domainPath: "+s)
	}
	if s := strings.TrimSpace(c.Geo); s != "" {
		lines = append(lines, "- geo: "+s)
	}
	if s := strings.TrimSpace(c.Product); s != "" {
		lines = append(lines, "- product: "+s)
	}
	if s := strings.TrimSpace(c.Sector); s != "" {
		lines = append(lines, "- sector: "+s)
	}
	if len(c.GeoFilters) > 0 {
		lines = append(lines, "- geoFilters: "+strings.Join(c.GeoFilters, ", "))
	}
	if len(c.ProductFilters) > 0 {
		lines = append(lines, "- productFilters: "+strings.Join(c.ProductFilters, ", "))
	}
	if s := strings.TrimSpace(c.Direction); s != "" {
		lines = append(lines, "- direction: "+s)
	}
	if c.Depth >= 0 {
		lines = append(lines, fmt.Sprintf("- recursionDepth: %d", c.Depth))
	}
	if len(lines) == 0 {
		return ""
	}
	return "INTENT CONTEXT (strict):\n" + strings.Join(lines, "\n") + "\n- Keep results tightly aligned with this context.\n- Prefer in-geo entities/facilities first; include cross-border actors only with direct, explicit evidence.\n- If geoFilters are present, prioritize nodes in those countries/regions and only keep external actors that directly connect to filtered nodes.\n- If productFilters are present, prioritize only matching products/materials/spare parts and their direct transformations.\n- Do NOT emit placeholder orgs like /org/na/na.\n"
}

// ---------------------------------------------------------------------------
// Step 1: org → child orgs + facilities
// ---------------------------------------------------------------------------

func (a *ResearchAgent) FindFacilitiesForOrgs(ctx context.Context, orgPaths []string) ([]OrgFacResult, error) {
	return runBatchesParallel(ctx, orgPaths, scStepBatchOrgs, a.findFacilitiesForOrgsBatch)
}

func (a *ResearchAgent) findFacilitiesForOrgsBatch(ctx context.Context, orgPaths []string) ([]OrgFacResult, error) {
	list := quotedList(orgPaths)
	contextBlock := supplyChainPromptContextBlock(ctx)
	prompt := fmt.Sprintf(`You are a supply chain research agent. For each organization (or government body) below, find:

1. CHILD ORGANIZATIONS (priority): If the org produces goods not only through its own facilities but also through subsidiaries, controlled entities, or partners that manufacture on its behalf — return those child orgs FIRST. We will drill into them in the next iteration. For government bodies (/gov/...): include subordinate agencies, state-owned enterprises they control.

2. PRODUCTION FACILITIES: Factories, plants, refineries, assembly sites, and other facilities that DIRECTLY transform materials into products. Exclude pure warehouses, distribution centers, fulfillment hubs, retail sites, and logistics nodes unless the facility's own primary output is a service. For government bodies: include production, processing, or depot facilities only when they directly manufacture, refine, or store strategic goods as operational stock. Do NOT invent synthetic facilities; if no concrete production facility is known, return an empty facilities list (org-level production will be represented later as org -> PRODUCES -> product).
   Preserve place-bearing names exactly when they are part of the official site/facility name (e.g. Wuxi, Huizhou, Eilat, Binh Duong, Riyadh). Do NOT drop the place token and do NOT collapse such facilities into generic "... general production" labels when a concrete site clue exists.

Organizations (use /org/... or /gov/... paths):
%s

%s

CANONICAL PATH RULES (use ONLY these formats):
%s
%s

Return ONLY valid JSON array:
[
  {
    "orgPath": "/org/...",
    "childOrgs": ["/org/..."],
    "facilities": [{ "path": "/fac/...", "name": "...", "facilityType": "`+taxonomy.FacilityRolesForPrompts()+`", "facilityKind": "`+taxonomy.FacilityKindsForPrompts()+`" }]
  }
]
No markdown. One entry per input organization.`, list, contextBlock, scOrgPathRulesPrompt(), scFacPathRulesPrompt())

	raw, err := a.runStepPrompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("step1 org→fac: %w", err)
	}
	var results []OrgFacResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("step1 parse: %w", err)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Step 2: fac → products
// ---------------------------------------------------------------------------

func (a *ResearchAgent) FindProductsForFacs(ctx context.Context, facPaths []string) ([]FacProductResult, error) {
	return runBatchesParallel(ctx, facPaths, scStepBatchFacs, a.findProductsForFacsBatch)
}

func (a *ResearchAgent) findProductsForFacsBatch(ctx context.Context, facPaths []string) ([]FacProductResult, error) {
	list := quotedList(facPaths)
	contextBlock := supplyChainPromptContextBlock(ctx)
	prompt := fmt.Sprintf(`You are a supply chain research agent. For each facility below, find ALL products or services it produces directly.
Do NOT list products of parent, sibling, or related companies — only what THIS facility makes.
If the facility is a warehouse, distribution center, fulfillment hub, depot, terminal, or logistics node, return an empty products list unless the facility's own primary output is a service under /product/service/...

Product granularity: Prefer product family over specific SKU; prefer concrete commodity over vague category (e.g. carbonated-soft-drinks, not "Coca-Cola 330ml can"). Focus on main outputs; by-products are optional.

Facilities:
%s

%s

CANONICAL PATH RULES (use ONLY these formats):
%s

Return ONLY valid JSON array:
[
  {
    "facPath": "/fac/...",
    "products": [{ "path": "/product/...", "name": "..." }]
  }
]
No markdown. One entry per input facility.`, list, contextBlock, scProductPathRulesPrompt())

	raw, err := a.runStepPrompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("step2 fac→product: %w", err)
	}
	var results []FacProductResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("step2 parse: %w", err)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Step 3: product → recipient facilities
// ---------------------------------------------------------------------------

func (a *ResearchAgent) FindRecipientFacsForProducts(ctx context.Context, productPaths []string) ([]ProductRecipientResult, error) {
	return runBatchesParallel(ctx, productPaths, scStepBatchProducts, a.findRecipientFacsForProductsBatch)
}

func (a *ResearchAgent) findRecipientFacsForProductsBatch(ctx context.Context, productPaths []string) ([]ProductRecipientResult, error) {
	list := quotedList(productPaths)
	contextBlock := supplyChainPromptContextBlock(ctx)
	prompt := fmt.Sprintf(`You are a supply chain research agent. For each product below, find ALL facilities that use this product as INPUT for their own production (manufacturing, processing, assembly, refining).

Include: facilities that transform, process, or assemble this product into something else — the direct next link in the supply chain.
Exclude: pure distributors, retailers, logistics hubs, end consumers — only production facilities.

Products:
%s

%s

CANONICAL PATH RULES (use ONLY these formats):
%s

Return ONLY valid JSON array:
[
  {
    "productPath": "/product/...",
    "recipients": [{ "path": "/fac/...", "name": "...", "facilityType": "manufacturing|processing|distribution|storage", "facilityKind": "factory|warehouse|terminal|seaport" }]
  }
]
No markdown. One entry per input product.`, list, contextBlock, scFacPathRulesPrompt())

	raw, err := a.runStepPrompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("step3 product→fac: %w", err)
	}
	var results []ProductRecipientResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("step3 parse: %w", err)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Step 4: fac → owning org
// ---------------------------------------------------------------------------

func (a *ResearchAgent) FindOrgsForFacs(ctx context.Context, facPaths []string) ([]FacOrgResult, error) {
	return runBatchesParallel(ctx, facPaths, scStepBatchFacs, a.findOrgsForFacsBatch)
}

func (a *ResearchAgent) findOrgsForFacsBatch(ctx context.Context, facPaths []string) ([]FacOrgResult, error) {
	list := quotedList(facPaths)
	contextBlock := supplyChainPromptContextBlock(ctx)
	prompt := fmt.Sprintf(`You are a supply chain research agent. For each facility below, identify the organization that owns or operates it.
When owner cannot be reliably inferred, leave orgPath empty ("") instead of using placeholders.

Facilities:
%s

%s

CANONICAL PATH RULES (use ONLY these formats):
%s

Return ONLY valid JSON array:
[
  {
    "facPath": "/fac/...",
    "orgPath": "/org/...",
    "orgName": "..."
  }
]
No markdown. One entry per input facility.`, list, contextBlock, scOrgPathRulesPrompt())

	raw, err := a.runStepPrompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("step4 fac→org: %w", err)
	}
	var results []FacOrgResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("step4 parse: %w", err)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Upstream steps: fac → input products, product → supplier facilities
// ---------------------------------------------------------------------------

// FindInputProductsForFacs (upstream U1): for each facility, find what products it consumes as inputs.
func (a *ResearchAgent) FindInputProductsForFacs(ctx context.Context, facPaths []string) ([]FacInputProductsResult, error) {
	return runBatchesParallel(ctx, facPaths, scStepBatchFacs, a.findInputProductsForFacsBatch)
}

func (a *ResearchAgent) findInputProductsForFacsBatch(ctx context.Context, facPaths []string) ([]FacInputProductsResult, error) {
	list := quotedList(facPaths)
	contextBlock := supplyChainPromptContextBlock(ctx)
	prompt := fmt.Sprintf(`You are a supply chain research agent (UPSTREAM tracing). For each facility below, find ALL products or materials it consumes as INPUTS for its production (raw materials, components, semi-finished goods, energy, packaging).

Include: raw materials, intermediates, components that this facility transforms or assembles. Focus on significant inputs by volume or value.
Exclude: services, utilities (unless critical), indirect inputs. Prefer product family over SKU; concrete commodity over vague category.

Facilities:
%s

%s

CANONICAL PATH RULES (use ONLY these formats):
%s

Return ONLY valid JSON array:
[
  {
    "facPath": "/fac/...",
    "inputProducts": [{ "path": "/product/...", "name": "..." }]
  }
]
No markdown. One entry per input facility.`, list, contextBlock, scProductPathRulesPrompt())

	raw, err := a.runStepPrompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("upstream U1 fac→inputProducts: %w", err)
	}
	var results []FacInputProductsResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("upstream U1 parse: %w", err)
	}
	return results, nil
}

// FindSupplierFacsForProducts (upstream U2): for each product, find facilities that produce and supply it.
func (a *ResearchAgent) FindSupplierFacsForProducts(ctx context.Context, productPaths []string) ([]ProductSupplierResult, error) {
	return runBatchesParallel(ctx, productPaths, scStepBatchProducts, a.findSupplierFacsForProductsBatch)
}

func (a *ResearchAgent) findSupplierFacsForProductsBatch(ctx context.Context, productPaths []string) ([]ProductSupplierResult, error) {
	list := quotedList(productPaths)
	contextBlock := supplyChainPromptContextBlock(ctx)
	prompt := fmt.Sprintf(`You are a supply chain research agent (UPSTREAM tracing). For each product below, find ALL facilities that PRODUCE and SUPPLY this product to the market.

Include: extraction sites, refineries, processing plants, manufacturing facilities that output this product. The direct upstream link — who makes this product before it reaches consumers/processors.
Exclude: distributors, traders, logistics hubs — only production facilities. Return only concrete facilities (no synthetic general-production placeholders). Use /fac/na/{facilitySlug} ONLY as a last resort after checking facility name, site/city mention, operator context, and source text for geography.

Products:
%s

%s

CANONICAL PATH RULES (use ONLY these formats):
%s

Return ONLY valid JSON array:
[
  {
    "productPath": "/product/...",
    "suppliers": [{ "path": "/fac/...", "name": "...", "facilityType": "extraction|refining|processing|manufacturing", "facilityKind": "mine|refinery|factory|terminal" }]
  }
]
No markdown. One entry per input product.`, list, contextBlock, scFacPathRulesPrompt())

	raw, err := a.runStepPrompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("upstream U2 product→suppliers: %w", err)
	}
	var results []ProductSupplierResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("upstream U2 parse: %w", err)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// runStepPrompt executes a focused step prompt and returns raw JSON bytes.
func (a *ResearchAgent) runStepPrompt(ctx context.Context, prompt string) ([]byte, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:                8000,
			MaxOutputSize:                   8000,
		},
		Verifier: verifier,
	}
	result, _, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// runBatchesParallel runs batch fn in parallel; safe when batches are independent.
func runBatchesParallel[T any](ctx context.Context, items []string, batchSize int, fn func(context.Context, []string) ([]T, error)) ([]T, error) {
	batches := batchStrings(items, batchSize)
	if len(batches) <= 1 {
		if len(batches) == 0 {
			return nil, nil
		}
		return fn(ctx, batches[0])
	}
	allResults := make([][]T, len(batches))
	g, gctx := errgroup.WithContext(ctx)
	for i, batch := range batches {
		i, batch := i, batch
		g.Go(func() error {
			r, err := fn(gctx, batch)
			if err != nil {
				return err
			}
			allResults[i] = r
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var all []T
	for _, r := range allResults {
		all = append(all, r...)
	}
	return all, nil
}

func batchStrings(items []string, size int) [][]string {
	if size <= 0 {
		size = 10
	}
	var batches [][]string
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

func quotedList(items []string) string {
	lines := make([]string, 0, len(items))
	for _, p := range items {
		p = strings.TrimSpace(p)
		if p != "" {
			lines = append(lines, "- "+p)
		}
	}
	return strings.Join(lines, "\n")
}

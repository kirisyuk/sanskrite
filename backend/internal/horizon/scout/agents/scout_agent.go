package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// ScoutAgent извлекает структурированные данные из сырого контента
type ScoutAgent struct {
	executor *ai.AITaskExecutor
}

// NewScoutAgent создает новый Scout Agent
func NewScoutAgent(executor *ai.AITaskExecutor) *ScoutAgent {
	return &ScoutAgent{
		executor: executor,
	}
}

// DiscoveredRelation описывает найденную зависимость
type DiscoveredRelation struct {
	// SourceCanonicalPath is an OPTIONAL explicit source path.
	// When set, overrides the default "THIS FRAGMENT" as edge source.
	// MUST be used for Phase 2 strategic relations where edge direction requires a different source.
	// Example: GATEKEEPS_DECISION edge must originate from ORG/GOV, not from the DECISION_POINT.
	// When answering "Who gatekeeps X?", set SourceCanonicalPath to the gatekeeper's path.
	SourceCanonicalPath string `json:"sourceCanonicalPath,omitempty"`
	// SourceType is the type of the source entity (ORGANIZATION, GOV, etc.) when SourceCanonicalPath is set.
	SourceType string `json:"sourceType,omitempty"`
	// SourceName is the name of the source entity when SourceCanonicalPath is set.
	SourceName string `json:"sourceName,omitempty"`

	// TargetName SHOULD be the full legal/official name where relevant (e.g., "Tesla, Inc.")
	TargetName string `json:"targetName"`
	// TargetLegalName is the legal/official name (with form of ownership where relevant).
	TargetLegalName string `json:"targetLegalName,omitempty"`
	// TargetShortName is the normalized "base name" without ownership suffix (e.g., "Tesla").
	TargetShortName string `json:"targetShortName,omitempty"`
	// IMPORTANT: must match Horizon FragmentType enum values (not human words):
	// ORGANIZATION | FACILITY | GEO_REGION | PRODUCT | EVENT | PERSON
	TargetType   string  `json:"targetType"`
	RelationType string  `json:"relationType"` // SUPPLIES_TO, LOCATED_IN, OWNS, COMPETES_WITH
	Reason       string  `json:"reason"`
	Confidence   float64 `json:"confidence"`
	// IMPORTANT: must be a stable canonical path. DO NOT use /discovered/*
	TargetCanonicalPath string `json:"targetCanonicalPath"`

	// --- Optional edge enrichment (fail-open) ---
	// ValidFrom/ValidTo are real-world effective dates for the relationship.
	// Keep them as strings to avoid hard-failing JSON unmarshalling on date format drift.
	// Supported formats are parsed later: "YYYY-MM-DD" (preferred) and RFC3339.
	ValidFrom *string `json:"validFrom,omitempty"`
	ValidTo   *string `json:"validTo,omitempty"`

	// Metrics are edge-specific numerical metrics (ownership_percent, investment_amount, annual_volume, etc).
	// Values use models.MetricValue (now tolerant to numeric strings like "10M", "45%").
	Metrics map[string]models.MetricValue `json:"metrics,omitempty"`

	// Qualifiers are categorical edge annotations (edgeModality, eventId, round, etc).
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty"`
}

// ScoutResult результат работы Scout Agent
type ScoutResult struct {
	Fragment  models.HorizonFragment `json:"fragment"`
	Relations []DiscoveredRelation   `json:"relations"`
	// SupplyChains is best-effort core output for business graph construction.
	// Each chain is an ordered sequence of nodes representing a supply chain path.
	SupplyChains []SupplyChain `json:"supplyChains,omitempty"`
}

type ProductCandidate struct {
	CanonicalPath string  `json:"canonicalPath,omitempty"`
	Name          string  `json:"name,omitempty"`
	Reason        string  `json:"reason,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

type CarrierProductObservation struct {
	CarrierRef string                 `json:"carrierRef"`
	ProductRef string                 `json:"productRef,omitempty"`
	Metrics    map[string]interface{} `json:"metrics,omitempty"`
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty"`
	Confidence float64                `json:"confidence,omitempty"`
	ObservedAt string                 `json:"observedAt,omitempty"`
}

type SupplyLinkObservation struct {
	SupplierRef string                 `json:"supplierRef,omitempty"`
	CustomerRef string                 `json:"customerRef,omitempty"`
	ProductRef  string                 `json:"productRef,omitempty"`
	Metrics     map[string]interface{} `json:"metrics,omitempty"`
	Qualifiers  map[string]interface{} `json:"qualifiers,omitempty"`
	Confidence  float64                `json:"confidence,omitempty"`
	ObservedAt  string                 `json:"observedAt,omitempty"`
}

type RouteObservation struct {
	SupplierRef  string                 `json:"supplierRef,omitempty"`
	CustomerRef  string                 `json:"customerRef,omitempty"`
	ProductRef   string                 `json:"productRef,omitempty"`
	TransportVia []string               `json:"transportVia,omitempty"`
	VesselType   string                 `json:"vesselType,omitempty"`
	Qualifiers   map[string]interface{} `json:"qualifiers,omitempty"`
	Confidence   float64                `json:"confidence,omitempty"`
	ObservedAt   string                 `json:"observedAt,omitempty"`
}

// SupplyChainPreHotPathPlan is a lightweight decomposition/steering layer that
// runs before hot-path graph building. It must guide the next prompt without
// hard-constraining topology or acting as a path artifact.
type SupplyChainPreHotPathPlan struct {
	Strategy                          string   `json:"strategy,omitempty"`
	StrategyReason                    string   `json:"strategyReason,omitempty"`
	CandidateAnchorTerms              []string `json:"candidateAnchorTerms,omitempty"`
	CandidateProductFamilies          []string `json:"candidateProductFamilies,omitempty"`
	CandidateOperationalAnchorClasses []string `json:"candidateOperationalAnchorClasses,omitempty"`
	CandidateFlowArchetypes           []string `json:"candidateFlowArchetypes,omitempty"`
	CandidateTransitHubTerms          []string `json:"candidateTransitHubTerms,omitempty"`
	CandidateGateTerms                []string `json:"candidateGateTerms,omitempty"`
	CandidateRouteGeographies         []string `json:"candidateRouteGeographies,omitempty"`
	SearchFocus                       []string `json:"searchFocus,omitempty"`
	PreferredEvidence                 []string `json:"preferredEvidence,omitempty"`
	DerivedProductFilters             []string `json:"derivedProductFilters,omitempty"`
	Confidence                        float64  `json:"confidence,omitempty"`
}

// SupplyChainScopedObservations is a top-level scoped observation bundle used for capability/link/route enrichment
// after the graph skeleton is already known.
type SupplyChainScopedObservations struct {
	ProductCandidates          []ProductCandidate          `json:"productCandidates,omitempty"`
	CarrierProductObservations []CarrierProductObservation `json:"carrierProductObservations,omitempty"`
	LinkObservations           []SupplyLinkObservation     `json:"linkObservations,omitempty"`
	RouteObservations          []RouteObservation          `json:"routeObservations,omitempty"`
}

// SupplyChain represents an ordered sequence of supply chain nodes.
// Nodes alternate between actors (org/fac) and products, forming a chain:
// supplier → product → processor → product → customer
type SupplyChain struct {
	// Nodes is an ordered list of entities in the supply chain.
	// Expected pattern: [actor, product, actor, product, ...]
	Nodes []SupplyChainNode `json:"nodes"`
	// Confidence for the entire chain (0.0-1.0)
	Confidence float64 `json:"confidence"`
	// Reason/evidence for this chain
	Reason string `json:"reason,omitempty"`
	// TransportVia lists facility canonical paths the product passes through (ports, terminals, pipelines, warehouses).
	// Same canonicalization and deduplication rules apply as for other fragments.
	TransportVia []string `json:"transportVia,omitempty"`
	// SegmentMetrics: per-segment volume, share, period (from full-query strategy).
	SegmentMetrics []SegmentMetrics `json:"segmentMetrics,omitempty"`
	// Alternatives: alternative suppliers at each (customer, product) step.
	Alternatives []AlternativeSuppliers `json:"alternatives,omitempty"`
	// ProductCandidates are scoped additions proposed by the model for the current job.
	ProductCandidates []ProductCandidate `json:"productCandidates,omitempty"`
	// CarrierProductObservations are reusable actor×product observations for capability building.
	CarrierProductObservations []CarrierProductObservation `json:"carrierProductObservations,omitempty"`
	// LinkObservations are link-specific observations for the current chain.
	LinkObservations []SupplyLinkObservation `json:"linkObservations,omitempty"`
	// RouteObservations are route/path scoped observations for the current chain.
	RouteObservations []RouteObservation `json:"routeObservations,omitempty"`
}

// SegmentMetrics holds metrics for a single segment (fromNodeIndex → toNodeIndex, productIndex).
type SegmentMetrics struct {
	FromNodeIndex int                   `json:"fromNodeIndex"`
	ToNodeIndex   int                   `json:"toNodeIndex"`
	ProductIndex  int                   `json:"productIndex"`
	Volume        *MetricWithConfidence `json:"volume,omitempty"`
	Share         *MetricWithConfidence `json:"share,omitempty"`
	Period        string                `json:"period,omitempty"` // "2023", "2023-Q1"
	Start         string                `json:"start,omitempty"`  // ISO8601
	End           string                `json:"end,omitempty"`    // ISO8601
	Confidence    float64               `json:"confidence"`
}

// MetricWithConfidence is a metric value with unit and confidence.
type MetricWithConfidence struct {
	Value      float64 `json:"value"`
	Unit       string  `json:"unit,omitempty"`
	Confidence float64 `json:"confidence"`
}

// AlternativeSuppliers lists alternative suppliers for a (customer, product) pair.
type AlternativeSuppliers struct {
	CustomerNodeIndex int                   `json:"customerNodeIndex"`
	ProductNodeIndex  int                   `json:"productNodeIndex"`
	Suppliers         []AlternativeContract `json:"suppliers"`
}

// AlternativeContract describes one alternative supplier with substitutability fields.
type AlternativeContract struct {
	SupplierRef       string                `json:"supplierRef"`   // canonical path
	TechnicalFit      float64               `json:"technicalFit"`  // 0-1
	RegulatoryFit     float64               `json:"regulatoryFit"` // 0-1
	AvailableCapacity *MetricWithConfidence `json:"availableCapacity,omitempty"`
	LeadTimeToSwitch  *MetricWithConfidence `json:"leadTimeToSwitch,omitempty"` // days
	CostToSwitch      *MetricWithConfidence `json:"costToSwitch,omitempty"`     // usd
	Confidence        float64               `json:"confidence"`
}

// SupplyChainNode represents a single node in a supply chain.
type SupplyChainNode struct {
	// CanonicalPath is the stable path for this entity (/org/..., /fac/..., /product/...)
	CanonicalPath string `json:"canonicalPath"`
	// Name is the human-readable name
	Name string `json:"name"`
	// Type is the fragment type: ORGANIZATION, FACILITY, PRODUCT
	Type string `json:"type"`
	// Role describes the node's function in the chain (optional): supplier, processor, assembler, distributor, customer
	Role string `json:"role,omitempty"`
	// FacilityType is supply chain role for FACILITY: extraction|refining|processing|manufacturing|distribution|storage|logistics
	FacilityType string `json:"facilityType,omitempty"`
	// FacilityKind is physical type for FACILITY: seaport|factory|cable|pipeline|mine|refinery|etc.
	FacilityKind string `json:"facilityKind,omitempty"`
}

// Вспомогательная структура для парсинга, чтобы поймать name
type extractedFragment struct {
	models.HorizonFragment
	// Backward compatibility: some models may return "name"
	Name string `json:"name"`
	// Preferred outputs:
	LegalName string `json:"legalName,omitempty"`
	ShortName string `json:"shortName,omitempty"`
}

type scoutResultInternal struct {
	Fragment     extractedFragment    `json:"fragment"`
	Relations    []DiscoveredRelation `json:"relations"`
	SupplyChains []SupplyChain        `json:"supplyChains,omitempty"`
}

// ExtractFragmentData извлекает данные HorizonFragment и связи из контента источника.
// Backward-compatible wrapper around ExtractFragmentDataWithGrounding.
func (a *ScoutAgent) ExtractFragmentData(ctx context.Context, content string, targetCanonicalPath string, entityPath string, specialties []string, chainContext string, domainModes []string) (*ScoutResult, error) {
	result, _, err := a.ExtractFragmentDataWithGrounding(ctx, content, targetCanonicalPath, entityPath, specialties, chainContext, domainModes)
	return result, err
}

// ExtractFragmentDataWithGrounding returns extracted result plus grounding evidence from full AI task result.
func (a *ScoutAgent) ExtractFragmentDataWithGrounding(ctx context.Context, content string, targetCanonicalPath string, entityPath string, specialties []string, chainContext string, domainModes []string) (*ScoutResult, *ai.GroundingEvidence, error) {
	prompt := a.buildExtractionPrompt(content, targetCanonicalPath, entityPath, specialties, chainContext, domainModes)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:                16000, // Увеличиваем окно для связей
			// IMPORTANT: For large inputs + reasoning models, output budgets can be consumed by hidden "thinking",
			// leading to truncated JSON. We intentionally allow larger outputs here.
			MaxOutputSize: 16384,
		},
		Verifier: verifier,
	}

	result, fullResult, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract fragment data: %w", err)
	}
	var groundingEvidence *ai.GroundingEvidence
	if fullResult != nil {
		groundingEvidence = fullResult.GroundingEvidence
	}

	// Преобразуем результат
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, groundingEvidence, fmt.Errorf("failed to marshal result: %w", err)
	}

	// Unmarshal to internal structure to catch extra fields like Name
	var internalResult scoutResultInternal
	var scoutResult *ScoutResult

	if err := json.Unmarshal(jsonData, &internalResult); err == nil {
		scoutResult = &ScoutResult{
			Fragment:     internalResult.Fragment.HorizonFragment,
			Relations:    internalResult.Relations,
			SupplyChains: internalResult.SupplyChains,
		}
		// Handle name fields from internal struct (prefer LegalName)
		legal := strings.TrimSpace(internalResult.Fragment.LegalName)
		if legal == "" {
			legal = strings.TrimSpace(internalResult.Fragment.Name)
		}
		short := strings.TrimSpace(internalResult.Fragment.ShortName)

		applyExtractedNameHints(&scoutResult.Fragment, legal, short)
	} else {
		// Fallback: возможно, модель вернула только Fragment (старый формат)
		var fragment models.HorizonFragment
		if err2 := json.Unmarshal(jsonData, &fragment); err2 == nil {
			scoutResult = &ScoutResult{Fragment: fragment}
		} else {
			return nil, groundingEvidence, fmt.Errorf("failed to unmarshal scout result: %w", err)
		}
	}

	// Common Defaults & Validation
	ensureFragmentCanonicalPath(&scoutResult.Fragment, targetCanonicalPath)

	// Валидация Fragment (повторная, если нужно)
	if strings.TrimSpace(scoutResult.Fragment.CanonicalPath) == "" {
		var fragment models.HorizonFragment
		if err := json.Unmarshal(jsonData, &fragment); err == nil {
			scoutResult.Fragment = fragment
			ensureFragmentCanonicalPath(&scoutResult.Fragment, targetCanonicalPath)
		}
	}

	// Handle Name (Fallback)
	if scoutResult.Fragment.Labels == nil {
		scoutResult.Fragment.Labels = make(map[string]string)
	}
	if scoutResult.Fragment.Identifiers == nil {
		scoutResult.Fragment.Identifiers = make(map[string]string)
	}
	if _, ok := scoutResult.Fragment.Labels["en"]; !ok || scoutResult.Fragment.Labels["en"] == "" {
		// Fallback name from path
		parts := strings.Split(targetCanonicalPath, "/")
		if len(parts) > 0 {
			seg := strings.TrimSpace(parts[len(parts)-1])
			seg = strings.ReplaceAll(seg, "-", " ")
			seg = strings.ReplaceAll(seg, "_", " ")
			seg = strings.TrimSpace(seg)
			if seg != "" {
				// Best-effort title casing for readability when segment is slug-like.
				name := seg
				if name == strings.ToLower(name) {
					name = strings.Title(name)
				}
				scoutResult.Fragment.Labels["en"] = name
				if scoutResult.Fragment.Identifiers["legalName"] == "" {
					scoutResult.Fragment.Identifiers["legalName"] = name
				}
			}
		}
	}
	// If shortName is missing, keep it consistent (fallback to label)
	if scoutResult.Fragment.Identifiers["shortName"] == "" && scoutResult.Fragment.Labels["en"] != "" {
		scoutResult.Fragment.Identifiers["shortName"] = scoutResult.Fragment.Labels["en"]
	}
	scoutResult.Fragment.Labels, scoutResult.Fragment.Identifiers = naming.NormalizeFragmentNames(
		scoutResult.Fragment.CanonicalPath,
		scoutResult.Fragment.Labels,
		scoutResult.Fragment.Identifiers,
	)
	applyInferredHomeLocationHint(&scoutResult.Fragment)
	normalizeDiscoveredRelationsNames(scoutResult.Relations)

	// Ensure Type is set
	if scoutResult.Fragment.Class == "" {
		// Infer type from canonicalPath (stable namespaces) or default to Organization.
		switch {
		case strings.Contains(targetCanonicalPath, "/domain/"):
			scoutResult.Fragment.Class = ah.FragmentClassEvidence
			scoutResult.Fragment.Subtype = "domain_legacy"
		case strings.Contains(targetCanonicalPath, "/media/"):
			scoutResult.Fragment.Class = ah.FragmentClassOrg
			scoutResult.Fragment.Subtype = "media_channel"
		case strings.Contains(targetCanonicalPath, "/person/"):
			scoutResult.Fragment.Class = FragmentTypePerson
		case strings.Contains(targetCanonicalPath, "/org/"):
			scoutResult.Fragment.Class = FragmentTypeOrganization
		case strings.Contains(targetCanonicalPath, "/gov/"):
			scoutResult.Fragment.Class = FragmentTypeOrganization
		case strings.Contains(targetCanonicalPath, "/product/"):
			scoutResult.Fragment.Class = FragmentTypeProduct
		case strings.Contains(targetCanonicalPath, "/program/"):
			scoutResult.Fragment.Class = ah.FragmentClassOrg
			scoutResult.Fragment.Subtype = "program"
		case strings.Contains(targetCanonicalPath, "/market/"):
			scoutResult.Fragment.Class = FragmentTypeMarket
		case strings.Contains(targetCanonicalPath, "/fac/"):
			scoutResult.Fragment.Class = FragmentTypeFacility
		case strings.Contains(targetCanonicalPath, "/deposit/"):
			scoutResult.Fragment.Class = ah.FragmentClassSite
			scoutResult.Fragment.Subtype = "deposit"
		case strings.Contains(targetCanonicalPath, "/loc/") || strings.Contains(targetCanonicalPath, "/region/"):
			scoutResult.Fragment.Class = FragmentTypeGeoRegion
		case strings.Contains(targetCanonicalPath, "/event/"):
			scoutResult.Fragment.Class = FragmentTypeEvent
		default:
			scoutResult.Fragment.Class = FragmentTypeOrganization
		}
	}

	if scoutResult.Fragment.TenantID == "" {
		scoutResult.Fragment.TenantID = "global"
	}
	// Canonical industry classification is resolver-owned. Scout may only emit raw industry hints.
	scoutResult.Fragment.IndustryPaths = nil

	if scoutResult.Fragment.Governance.Owner == "" {
		scoutResult.Fragment.Governance.Owner = "SYSTEM"
	}
	if scoutResult.Fragment.Governance.Status == "" {
		scoutResult.Fragment.Governance.Status = string(models.FragmentStatusActive)
	}
	if scoutResult.Fragment.Metadata.EpistemicStatus == "" {
		scoutResult.Fragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
	}
	if scoutResult.Fragment.Metadata.CreatedAt.IsZero() {
		scoutResult.Fragment.Metadata.CreatedAt = time.Now().UTC()
	}
	scoutResult.Fragment.Metadata.LastUpdatedAt = time.Now().UTC()

	return scoutResult, groundingEvidence, nil
}

func normalizeDiscoveredRelationsNames(relations []DiscoveredRelation) {
	for i := range relations {
		name, legal, short := naming.NormalizeRelationNames(
			relations[i].TargetType,
			relations[i].TargetName,
			relations[i].TargetLegalName,
			relations[i].TargetShortName,
		)
		relations[i].TargetName = name
		relations[i].TargetLegalName = legal
		relations[i].TargetShortName = short
	}
}

func applyExtractedNameHints(fragment *models.HorizonFragment, legal, short string) {
	if fragment == nil {
		return
	}
	if fragment.Labels == nil {
		fragment.Labels = make(map[string]string)
	}
	if fragment.Identifiers == nil {
		fragment.Identifiers = make(map[string]string)
	}

	legal = strings.TrimSpace(legal)
	short = strings.TrimSpace(short)

	// Do not overwrite a non-empty label produced in fragment payload.
	if legal != "" && strings.TrimSpace(fragment.Labels["en"]) == "" {
		fragment.Labels["en"] = legal
	}
	if legal != "" && strings.TrimSpace(fragment.Identifiers["legalName"]) == "" {
		fragment.Identifiers["legalName"] = legal
	}
	if short != "" && strings.TrimSpace(fragment.Identifiers["shortName"]) == "" {
		fragment.Identifiers["shortName"] = short
	}
}

func ensureFragmentCanonicalPath(fragment *models.HorizonFragment, targetCanonicalPath string) {
	if fragment == nil {
		return
	}
	if strings.TrimSpace(fragment.CanonicalPath) == "" {
		fragment.CanonicalPath = strings.TrimSpace(targetCanonicalPath)
		return
	}
	fragment.CanonicalPath = strings.TrimSpace(fragment.CanonicalPath)
}

func extractionAllowedRelationTypesByDomainModes(domainModes []string) []string {
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	// For prompt guardrails: if specific modes are present alongside general,
	// prefer specific scope (ignore general). Only return nil when ALL modes are general.
	specific := make([]string, 0, len(domainModes))
	for _, m := range domainModes {
		m = strings.TrimSpace(strings.ToLower(m))
		if m != "" && m != taxonomy.DomainModeGeneral {
			specific = append(specific, m)
		}
	}
	if len(specific) == 0 {
		return nil
	}
	allowedSet := map[string]struct{}{}
	allowed := make([]string, 0, 16)
	for _, mode := range specific {
		for _, edgeType := range contractsemantics.AllowedEdgeStringsForDomainMode(mode) {
			edgeType = strings.TrimSpace(edgeType)
			if edgeType == "" {
				continue
			}
			if _, ok := allowedSet[edgeType]; ok {
				continue
			}
			allowedSet[edgeType] = struct{}{}
			allowed = append(allowed, edgeType)
		}
	}
	// Supply-chain mode allows OWNS/CONTROLS for supply-relevant ownership/control links
	// (ORG/GOV -> ORG/FAC/DEPOSIT). We include both in generation guardrails
	// and enforce exact path predicate during phase-A filtering.
	if taxonomy.HasDomainMode(specific, taxonomy.DomainModeSupplyChain) ||
		taxonomy.HasDomainMode(specific, taxonomy.DomainModeServiceChain) {
		hasOwns := false
		hasControls := false
		for _, t := range allowed {
			if strings.EqualFold(strings.TrimSpace(t), "OWNS") {
				hasOwns = true
			}
			if strings.EqualFold(strings.TrimSpace(t), "CONTROLS") {
				hasControls = true
			}
		}
		if !hasOwns {
			allowed = append(allowed, "OWNS")
		}
		if !hasControls {
			allowed = append(allowed, "CONTROLS")
		}
	}
	return allowed
}

func extractionDomainGuardrailBlock(domainModes []string) string {
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	if len(domainModes) == 0 || taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeGeneral) {
		return ""
	}
	allowed := extractionAllowedRelationTypesByDomainModes(domainModes)
	if len(allowed) == 0 {
		return ""
	}
	modeLabel := strings.Join(domainModes, ", ")
	return fmt.Sprintf(`DOMAIN MODE GUARDRAILS (strict):
- Active domain modes: %s
- Emit ONLY these relationType values: %s
- If evidence suggests another relation family, add it as uncertainty in reason/missing coverage instead of emitting out-of-domain edges.`, modeLabel, strings.Join(allowed, ", "))
}

func extractionDomainRelationCatalogOverrideBlock(domainModes []string) string {
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	if len(domainModes) == 0 || taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeGeneral) {
		return ""
	}
	allowed := extractionAllowedRelationTypesByDomainModes(domainModes)
	if len(allowed) == 0 {
		return ""
	}
	return fmt.Sprintf(`DOMAIN-SCOPED RELATION CATALOG (strict override):
- Ignore generic relation catalog below and emit relationType ONLY from: %s
- If evidence maps to another relation family, record uncertainty in reason and do NOT emit that edge.`, strings.Join(allowed, ", "))
}

const extractionGeneralRelationRulesTemplate = `%s

   ROLE-FIRST DIRECTION CONTRACT (CRITICAL for ORG<->ORG):
   - Do NOT assume source = THIS FRAGMENT for OWNS / INVESTS_IN / ASSURES / CONTROLS.
   - Determine semantic roles first, then set edge direction.
   - For ORG->ORG directed edges above, include:
     qualifiers.source_role and qualifiers.target_role (e.g., owner->owned_entity, investor->investee, assurer->assuree, controller->controlled_entity).
   - For directional actor↔actor DEPENDS_ON/SUPPLIES_TO, also include source_role/target_role (dependent->dependency, supplier->customer).
   - If semantic source is not THIS FRAGMENT, sourceCanonicalPath/sourceType/sourceName is REQUIRED.
   - If direction is unclear, omit the relation (do not guess).

   %s
   - ORGANIZATION ↔ MARKET: relationType MUST be PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES (domestic oversight), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES.
   - Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/ for any relation.

   Each object must include:
   - targetName, targetLegalName, targetShortName
   - targetType: MUST be one of: %s
   - relationType: canonical dependency type
   - reason, confidence, targetCanonicalPath

   Optional edge enrichment fields (REQUIRED WHEN AVAILABLE):
   - validFrom, validTo
   - qualifiers: edgeModality, eventId, round, contractType, integration_type, facet, transmission qualifiers, source_role, target_role, direction_basis
   - metrics: ownership_percent, investment_amount, annual_volume, throughput_share, reroute_* etc.`

func extractionGeneralRelationRulesBlock() string {
	return fmt.Sprintf(
		extractionGeneralRelationRulesTemplate,
		indentPromptBlock(generatedGeneralRelationTaxonomyBlock(), "   "),
		edgesemantics.EdgeDirectionValidationRulesForPrompt(),
		entity_semantics.PromptRelationTargetTypesPipeList(),
	)
}

func buildExtractionRelationsRulesBlock(domainModes []string) string {
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	allowed := extractionAllowedRelationTypesByDomainModes(domainModes)
	if len(allowed) == 0 {
		return extractionGeneralRelationRulesBlock()
	}

	var b strings.Builder
	b.WriteString("   DOMAIN-SCOPED RELATION RULES (active contract):\n")
	b.WriteString(fmt.Sprintf("   - Allowed relationType values ONLY: %s\n", strings.Join(allowed, ", ")))
	b.WriteString("   - Never emit a relation outside this set.\n")
	b.WriteString("   - Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/.\n")

	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) ||
		taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeServiceChain) {
		b.WriteString(`   SUPPLY CHAIN MODE (operational edges only):
   - Do NOT use DEPENDS_ON for supply links. Use PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN, EXTRACTS_FROM.
   - Operational type matrix (hard contract):
     - PRODUCES: ORGANIZATION|FACILITY|DEPOSIT -> PRODUCT
     - SUPPLIES_TO: ORGANIZATION|FACILITY|PRODUCT -> ORGANIZATION|FACILITY
     - TRANSFORMS_INTO: PRODUCT -> PRODUCT (upstream -> downstream processing)
     - ASSEMBLES_INTO: PRODUCT -> PRODUCT (component/subassembly -> assembled downstream product)
     - USED_IN: PRODUCT -> PRODUCT (strong operational input, not transformation, not assembly)
     - EXTRACTS_FROM: FACILITY -> DEPOSIT
   - For actor↔actor SUPPLIES_TO links, include qualifiers.source_role=supplier and qualifiers.target_role=customer.
   - OWNS is allowed ONLY for supply-relevant ownership:
     source MUST be /org/... or /gov/... and target MUST be /org/... or /fac/... or /deposit/....
     Use OWNS when ownership clarifies who controls producing subsidiaries or critical facilities/deposits.
     Do NOT emit OWNS for markets/programs/abstract entities.
   - CONTROLS is allowed ONLY for supply-relevant non-equity control:
     source MUST be /org/... or /gov/... and target MUST be /org/... or /fac/... or /deposit/....
     Use CONTROLS for state/contractual/operational control when ownership is unclear.
     Do NOT emit CONTROLS for markets/programs/abstract entities.
   - LOCATED_IN/OPERATES_IN only for geo/facility anchoring.
     - LOCATED_IN target MUST be GEO_REGION.
     - OPERATES_IN source MUST be ORGANIZATION|PERSON, target MUST be GEO_REGION|FACILITY|DEPOSIT.
     - MARKET MUST NOT be source for OPERATES_IN.
   - PARTICIPATES_IN: ORG/FACILITY -> MARKET (commercial market participation). For "market depends on org" use PARTICIPATES_IN with ORG as source.
   - TRADED_IN: PRODUCT -> MARKET only for product trading/price-discovery context.
   - MARKET source guard in supply_chain: MARKET may emit only PARTICIPATES_IN and HAS_MARKET_SEGMENT (plus DOMAIN scaffold when source is /domain/...).
     Never emit PRODUCES/SUPPLIES_TO/TRANSFORMS_INTO/ASSEMBLES_INTO/USED_IN/EXTRACTS_FROM/OPERATES_IN from MARKET.
   - REGULATES: GOV entity (/gov/...) -> MARKET or ORG (domestic regulatory oversight). NEVER use DEPENDS_ON for government regulation.
   - ADMINISTERS: GOV entity (/gov/...) -> subordinate GOV/PROGRAM/FACILITY (administrative hierarchy).
   - For transmission/capacity add throughput_share, capacity_constraint, reroute_penalty when known.
`)
		b.WriteString(GovEntityRelationRulesBlock())
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeGovernance) {
		b.WriteString(`   GOVERNANCE MODE:
   - OWNS and SUBSIDIARY_OF for equity hierarchy.
   - CONTROLS for explicit non-equity control.
   - MANAGES for formal management (CEO, director, operator).
   - INVESTS_IN for minority/stake investments.
   - BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED for people governance links.
   - ADMINISTERS for GOV/ORG hierarchy into GOV/PROGRAM.
   - ASSURES for external auditors/certifiers participating in corporate governance.
   - Role-first direction for OWNS/SUBSIDIARY_OF/INVESTS_IN/ASSURES/CONTROLS on ORG<->ORG.
   - For those directed ORG->ORG edges include qualifiers.source_role and qualifiers.target_role.
   - Add metrics.ownership_percent and/or voting_percent when evidence exists.
`)
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeAssurance) {
		b.WriteString(`   ASSURANCE MODE:
   - ASSURES: assurer -> assuree. Add qualifiers.assurance_type: audit|certification|regulatory|inspection.
   - For ORG->ORG ASSURES edges include qualifiers.source_role=assurer and qualifiers.target_role=assuree.
   - Do NOT emit AUDITED_BY (legacy inverse form).
   - REGULATES: regulator -> regulated entity (same-jurisdiction domestic oversight only; for sanctions/restrictions use CONTROLS_ACCESS or IMPOSES_CONDITION).
`)
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeMarket) {
		b.WriteString(`   MARKET MODE:
   - PARTICIPATES_IN: entity -> market.
   - TRADED_IN: product -> market for traded commodity/product exposure.
   - ORGANIZATION ↔ MARKET: PARTICIPATES_IN (ORG -> MARKET). GOV entities may also REGULATES (domestic oversight), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES market.
   - HAS_MARKET_SEGMENT: parent market -> segment (child path = parent path + one segment).
   - Keep focus on market structure and placement, not supply/governance edges.
`)
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeStrategic) {
		b.WriteString(`   STRATEGIC MODE:
   - CONTROLS_ACCESS, IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO only.
   - Respect DECISION_POINT/MECHANISM typing and direction.
`)
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModePolicyImpact) {
		b.WriteString(`   POLICY IMPACT MODE:
   - Focus on policy transmission channels from regulator/program to market/org/community.
   - REGULATES/ADMINISTERS for formal domestic public-policy authority (same jurisdiction). For sanctions, trade bans, or extra-territorial restrictions use CONTROLS_ACCESS or IMPOSES_CONDITION.
   - CONTROLS_ACCESS/IMPOSES_CONDITION/USES_MECHANISM/APPLIES_TO for explicit instruments and constraints (including sanctions).
   - INFLUENCES for soft or second-order impact channels when formal authority is absent.
   - DEPENDS_ON/PARTICIPATES_IN/TRADED_IN/LOCATED_IN/OPERATES_IN for exposure context needed in impact modeling.
`)
	}

	b.WriteString(fmt.Sprintf(`
   Each object must include:
   - targetName, targetLegalName, targetShortName
   - targetType: MUST be one of: %s
   - relationType: one of the allowed values listed above
   - reason, confidence, targetCanonicalPath

   Optional edge enrichment fields (REQUIRED WHEN AVAILABLE):
   - validFrom, validTo
   - qualifiers: edgeModality, eventId, round, contractType, integration_type, facet, transmission qualifiers, source_role, target_role, direction_basis
   - metrics: ownership_percent, investment_amount, annual_volume, throughput_share, reroute_* etc.`, entity_semantics.PromptRelationTargetTypesPipeList()))
	return b.String()
}

func extractionCanonicalNamespaceDetailsBlock() string {
	return `CANONICAL NAMESPACE DETAILS (strict addendum):
- /segment/{domain}/{vertical}/{geo} for ORG marketSegments (exactly 3 parts after /segment).
- /media/{platform}/{slug} for concrete media channels/endpoints.
- Products: /product/{category}/{type}/{stage}/{slug}[/{variant-slug}] (single format).
  - For services, use category=service and map service domain/subdomain into {type}/{stage}.
- Markets:
  - L1: /market/{geo}/{domain}; L2: /market/{geo}/{domain}/{vertical}; L3+: parent path + one segment
  - NEVER repeat a segment already in the path (e.g. /market/iq/agriculture/agriculture is INVALID)
  - Example chains: /market/sa/finance → .../banking → .../banking/islamic-banking; /market/tw/technology → .../semiconductors → .../semiconductors/foundry
  - HAS_MARKET_SEGMENT child path = parent path + one segment
- Communities: /community/{geo}/{type}/{slug}; subgroup hierarchy via HAS_SUBGROUP (broader -> narrower).
- Programs: /program/{jurisdiction}/{slug}; if jurisdiction is unknown, use global.
- Facilities: /fac/{geo}/{facilitySlug}; geo like org/gov (ISO2, na, or regional). /fac/na is LAST-RESORT ONLY after checking facility name, site/city mention, operator context, and source text for geography. If the facility name itself contains a clear place clue, infer the ISO2 instead of using na.`
}

// buildExtractionPrompt строит промпт для извлечения данных
func (a *ScoutAgent) buildExtractionPrompt(content string, targetCanonicalPath string, entityPath string, specialties []string, chainContext string, domainModes []string) string {
	specialties = taxonomy.NormalizeSpecialties(specialties)
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	chainBlock := ""
	if strings.TrimSpace(chainContext) != "" {
		chainBlock = strings.TrimSpace(chainContext) + "\n"
	}
	specBlock := buildExtractionSpecializationModulesBlock(specialties, domainModes)
	domainBlock := extractionDomainGuardrailBlock(domainModes)
	if strings.TrimSpace(domainBlock) != "" {
		domainBlock += "\n\n"
	}
	relationCatalogOverride := extractionDomainRelationCatalogOverrideBlock(domainModes)
	if strings.TrimSpace(relationCatalogOverride) != "" {
		relationCatalogOverride += "\n"
	}
	relationRulesBlock := buildExtractionRelationsRulesBlock(domainModes)
	canonicalRulesBlock := taxonomy.ExtendedCanonicalPathRulesPrompt()
	businessRulesBlock := taxonomy.BusinessTagRulesPrompt()
	// Object detail: from entity_semantics (unified per type). Supply-chain mode uses operational-only blocks.
	typeBlock := entity_semantics.ExtractionBlockForPathAndDomainModes(targetCanonicalPath, "", "", "", domainModes)
	if typeBlock != "" {
		typeBlock = typeBlock + "\n\n"
	}
	nameRulesBlock := naming.PromptEntityNameNormalizationRules()
	objectTypeTaskBlock := buildObjectTypeTaskSpecializationBlock(targetCanonicalPath, "", "")
	objectQualifierContractBlock := buildObjectQualifierContractBlock(targetCanonicalPath, "", "")
	coreBusinessTaxonomyBlock := buildCoreBusinessTaxonomyBlock()
	canonicalContractBlock := fmt.Sprintf(`CANONICAL PATH RULES:
%s
%s
- NEVER use "/discovered/*".
- canonicalPath MUST match expected namespace for targetType.
- Domain is config, not graph node: targetCanonicalPath MUST NOT start with /domain/ for non-domain relations.
- If unsure, still output your best stable canonicalPath (do not leave it empty).`, canonicalRulesBlock, extractionCanonicalNamespaceDetailsBlock())

	fragmentContractBlock := fmt.Sprintf(`1. "fragment" Field (Object):
- canonicalPath MUST match target canonical path
- entityPath MUST match provided entity path
- %s
- %s
%s
%s
- industryPaths MUST be omitted. Canonical industry classification is assigned only by the taxonomy resolver, never by the model.
- If the content contains industry evidence, store raw mentions in qualifiers.industryMentions as an array of short strings (e.g. ["lithium-ion batteries", "battery manufacturing", "lfp"]).
- state: map of MetricValue entries
- description: short factual summary
- homeLocation: when geo is relevant, object with { name: string (city/place), iso3166_1: string (ISO3166-1 alpha2, e.g. "AE", "US") }. Required for org/facility/gov to enable geocoding.
  - If the entity/facility name, slug, or source text contains a clear place clue (e.g. Wuxi, Huizhou, Eilat, Binh Duong, Riyadh), set homeLocation.name to that city/site instead of leaving it empty.
  - Set iso3166_1 ONLY when explicit or strongly corroborated by the source/location clue.
  - If city/site clue and iso3166_1 conflict, fix the ISO2 or omit it; never keep a contradictory guess.
  - For org/gov, prefer the best evidence-backed jurisdiction from official name, HQ/site/place clue, and source text; do not relax into /org/na or /gov/na.
- lifecycleStage: concept|planning|permitting|financing|construction|commissioning|operational|expansion|suspended|decommissioning|closed
- expectedStartDate, actualStartDate, expectedEndDate, actualEndDate: ISO8601 when known
- identifiers: ISIN/LEI/Ticker/etc when available
- kbRefs: external KB refs when available
- metric value format:
  - value MUST be JSON number (expand "10M" -> 10000000)
  - unit lowercase (or omitted when unitless)
  - confidence in 0..1
  - observedAt ISO8601
  - provenance method + at least one sourceId`, nameRulesBlock, objectTypeTaskBlock, coreBusinessTaxonomyBlock, objectQualifierContractBlock)

	relationsContractBlock := fmt.Sprintf(`3. "relations" Field (Array of Objects):
- Direction is ROLE-FIRST: for directed ORG<->ORG edges (OWNS/INVESTS_IN/ASSURES/CONTROLS), determine semantic source by role (owner/investor/assurer/controller), not by "current fragment".
- If THIS FRAGMENT is the semantic target for these types, sourceCanonicalPath/sourceType/sourceName is REQUIRED.
- relationType must follow semantic direction.
%s
%s`, relationCatalogOverride, relationRulesBlock)

	supplyChainsContractBlock := `4. "supplyChains" Field (Array of Objects):
- Preferred chain model: org → fac → product → fac → org. Organizations at chain ends, facilities in the middle, products between facilities.
- Fallback model (only when concrete production facility is unknown or not separable for a small org): org → product.
- Use /org/ for organizations, /fac/ for facilities, /product/ for products, /deposit/ for deposits. Do NOT invent synthetic /fac/*-general-production placeholders.
- Each chain has: nodes[], confidence, reason, optional transportVia[] (but expected for route-sensitive legs when inferable).
- Node shape: canonicalPath (REQUIRED), name (REQUIRED), type (REQUIRED enum: ORGANIZATION|FACILITY|PRODUCT|DEPOSIT), role?
- Nodes follow: org → fac → product → fac → org (1-to-many at each level). org → product is allowed only for unknown/non-separated production.
- Include FACILITY where transformation occurs (product → facility → product).
- For extractive flows: facility → deposit → product.
- Only product-carrying flow belongs in supplyChains. If an adjacent organization merely owns, controls, manages, or operates a facility, emit OWNS / CONTROLS / MANAGES separately and do NOT encode FACILITY → PRODUCT → ORG.
- transportVia should contain facility canonical paths for ports/terminals/pipelines/warehouses/chokepoints when known or reasonably inferable from a route-sensitive cross-border flow.
- For maritime, pipeline, rail, corridor, chokepoint, or sanctions-sensitive flows, leaving transportVia empty requires genuine evidence gap, not convenience.
- Product granularity: use model line/product family (iPhone, MacBook, Claude Code), NOT specific SKU (iPhone 17, MacBook Pro M4, Claude Opus). Natural resources: keep taxonomy granularity, avoid over-generalization.`

	scopedObservationsBlock := `5. Scoped supply-chain additions (OPTIONAL but preferred when evidence exists):
- "productCandidates": array of { canonicalPath?, name, reason, confidence }.
  Use this ONLY for genuinely new product/material/component candidates that should extend the current job product scope.
  Do NOT repeat products already present in nodes[] or obvious aliases of the same product.
- "carrierProductObservations": array of { carrierRef, productRef, metrics?, qualifiers?, confidence, observedAt? }.
  Use for reusable actor×product facts that should apply across many links. When evidence clearly supports reusable baseline metrics for a carrier×product in the current chains, these observations are expected.
  Preferred reusable metric keys: annual_capacity, annual_output, lead_time_days_baseline, lead_time_volatility_days, inventory_days_baseline, on_time_delivery_baseline, technical_substitutability, regulatory_substitutability, service_criticality, quality_score, purity_percent, utilization_rate, available_export_share.
- "linkObservations": array of { supplierRef?, customerRef?, productRef, metrics?, qualifiers?, confidence, observedAt? }.
  Use for link-specific overrides or customer/destination-specific facts.
- "routeObservations": array of { supplierRef?, customerRef?, productRef?, transportVia?, vesselType?, qualifiers?, confidence, observedAt? }.
  Use for route/path scoped transport facts only.
- Do NOT move reusable capability baseline metrics into linkObservations just because they were seen on one chain.
- Never create capability entities or graph nodes for these observations. They are scoped evidence only.`

	return joinPromptBlocks(
		`You are a data extraction agent (Scout) for a risk analysis system. Extract structured information about an entity and its dependencies from the provided content.`,
		fmt.Sprintf(`TARGET ENTITY:
- Canonical Path: %s
- Entity Type Path: %s`, targetCanonicalPath, entityPath),
		"CONTENT TO ANALYZE:\n"+content,
		chainBlock,
		specBlock,
		businessRulesBlock,
		typeBlock,
		domainBlock,
		canonicalContractBlock,
		`TASK:
Extract all relevant information and return ONE JSON object with fields:
- fragment
- relations
- supplyChains
Use [] for unknown arrays.`,
		fragmentContractBlock,
		relationsContractBlock,
		supplyChainsContractBlock,
		scopedObservationsBlock,
		"Return ONLY valid JSON. Do not include markdown code fences.",
	)
}

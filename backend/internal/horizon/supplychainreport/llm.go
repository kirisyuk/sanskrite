package supplychainreport

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

const (
	DefaultPolishModel           = "gemini-3-flash-preview"
	DefaultPolishMaxOutputTokens = 32768
	DefaultPolishThinkingBudget  = 32768
)

type SectionPackage struct {
	ID                 string
	Title              string
	Order              int
	Description        string
	Prompt             string
	DeterministicDraft string
	LLMReadyFactsJSON  string
	ShouldPolish       bool
}

type PolishOptions struct {
	Enabled         bool
	Model           string
	MaxOutputTokens int
	ThinkingBudget  *int
}

type GenerateRequest struct {
	Model             string
	SystemInstruction string
	Prompt            string
	MaxOutputTokens   int
	ThinkingBudget    *int
}

type GenerateResponse struct {
	Content    string
	Model      string
	TokensUsed int
}

type Generator interface {
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
}

type PolishResult struct {
	Content    string
	Model      string
	TokensUsed int
}

func normalizePolishOptions(opts PolishOptions) PolishOptions {
	if strings.TrimSpace(opts.Model) == "" {
		opts.Model = DefaultPolishModel
	}
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = DefaultPolishMaxOutputTokens
	}
	if opts.ThinkingBudget == nil {
		budget := DefaultPolishThinkingBudget
		opts.ThinkingBudget = &budget
	}
	return opts
}

func BuildSectionPackages(input *Input) []SectionPackage {
	families := collectFamilySummaries(input)
	hubs := collectHubCounts(input)
	suppliers := collectEndpointCounts(input, "supplier")
	customers := collectEndpointCounts(input, "customer")
	products := collectProductCounts(input)
	corridors := collectCorridorSummaries(input)
	relations := collectRelationSummaries(input)
	modes := collectModeCounts(input)
	paths := collectRepresentativePaths(input)
	zones := collectZoneCounts(input)
	dependencyReadout := buildDependencyReadout(hubs, suppliers, customers, products)
	flowArchetypes := buildFlowArchetypes(relations)
	structuralSignals := buildStructuralSignals(modes)
	roleFacts := buildEntityRoleFacts(mergeRefCountSlices(hubs, suppliers, customers, zones), 24)
	hubRoleFacts := buildEntityRoleFacts(hubs, 16)
	supplyRoleFacts := buildEntityRoleFacts(suppliers, 16)
	demandRoleFacts := buildEntityRoleFacts(customers, 16)
	chainFacts := buildChainArchetypeFacts(paths, 12)
	productFacts := buildStrategicProductFacts(products, paths, corridors, 12)
	footprintNarrative := buildFootprintNarrative(roleFacts, hubs, suppliers, customers)

	return []SectionPackage{
		{
			ID:                 "what_this_network_is",
			Title:              "What This Network Is",
			Order:              1,
			Description:        "Preamble that explains what industrial and logistics system this visible network represents, in business language rather than system language.",
			Prompt:             "Explain what kind of industrial and logistics system the visible network represents, in the style of a top-tier strategy consulting report.",
			DeterministicDraft: buildBusinessIdentityDraft(input, families, products, chainFacts, hubs),
			LLMReadyFactsJSON: marshalFacts(map[string]any{
				"reportSubject":              safeLensName(input.Lens),
				"visibleNetwork":             buildVisibleNetworkSummary(input, families, hubs, suppliers, customers, products),
				"dominantLogisticsPatterns":  businessFamilyFacts(families[:minInt(len(families), 10)]),
				"illustrativeEndToEndChains": chainFacts[:minInt(len(chainFacts), 6)],
				"mainTransferHubs":           businessRefFacts(hubs[:minInt(len(hubs), 12)]),
				"hubRoleCandidates":          hubRoleFacts[:minInt(len(hubRoleFacts), 12)],
				"mainSupplyNodes":            businessRefFacts(suppliers[:minInt(len(suppliers), 10)]),
				"mainDemandNodes":            businessRefFacts(customers[:minInt(len(customers), 10)]),
				"strategicProductStreams":    productFacts[:minInt(len(productFacts), 10)],
				"footprintNarrativeHints":    footprintNarrative,
			}),
			ShouldPolish: true,
		},
		{
			ID:                 "industrial_roles_and_footprint",
			Title:              "Industrial Roles And Footprint",
			Order:              2,
			Description:        "Explain which facilities act as manufacturing sites, relay hubs, integration points, launch or operating sites, and receiving sites, and how the visible footprint is distributed.",
			Prompt:             "Explain the industrial roles and geographic footprint of the network in plain business language.",
			DeterministicDraft: buildBusinessRolesDraft(roleFacts, hubs, suppliers, customers, zones),
			LLMReadyFactsJSON: marshalFacts(map[string]any{
				"roleCandidates":             roleFacts,
				"mainTransferHubs":           businessRefFacts(hubs[:minInt(len(hubs), 16)]),
				"hubRoleCandidates":          hubRoleFacts,
				"mainSupplyNodes":            businessRefFacts(suppliers[:minInt(len(suppliers), 16)]),
				"supplyRoleCandidates":       supplyRoleFacts,
				"mainDemandNodes":            businessRefFacts(customers[:minInt(len(customers), 16)]),
				"demandRoleCandidates":       demandRoleFacts,
				"mainZones":                  businessRefFacts(zones[:minInt(len(zones), 12)]),
				"footprintNarrativeHints":    footprintNarrative,
				"illustrativeEndToEndChains": chainFacts[:minInt(len(chainFacts), 6)],
			}),
			ShouldPolish: true,
		},
		{
			ID:                 "end_to_end_chains",
			Title:              "How The End-To-End Chains Close",
			Order:              3,
			Description:        "Describe the recurring end-to-end chain archetypes and explain how supply moves from origin facilities into integration, delivery, launch, service, or receiving endpoints.",
			Prompt:             "Explain the end-to-end chains in the network, focusing on how they close operationally and what they achieve.",
			DeterministicDraft: buildBusinessChainsDraft(chainFacts, flowArchetypes),
			LLMReadyFactsJSON: marshalFacts(map[string]any{
				"illustrativeEndToEndChains": chainFacts,
				"dominantLogisticsPatterns":  businessFamilyFacts(families[:minInt(len(families), 10)]),
				"illustrativePathExamples":   businessPathFacts(paths[:minInt(len(paths), 8)]),
				"flowArchetypes":             flowArchetypes,
			}),
			ShouldPolish: true,
		},
		{
			ID:                 "strategic_product_streams",
			Title:              "Strategically Important Product Streams",
			Order:              4,
			Description:        "Explain which product streams matter most because they anchor or connect end-to-end chains, rather than simply because they appear often.",
			Prompt:             "Explain which product streams are strategically important and why, based on their role in the visible supply chains.",
			DeterministicDraft: buildBusinessProductDraft(productFacts),
			LLMReadyFactsJSON: marshalFacts(map[string]any{
				"strategicProductStreams":    productFacts,
				"dominantLogisticsPatterns":  businessFamilyFacts(families[:minInt(len(families), 12)]),
				"illustrativeEndToEndChains": chainFacts[:minInt(len(chainFacts), 10)],
				"illustrativeCorridors":      businessCorridorFacts(corridors[:minInt(len(corridors), 10)]),
			}),
			ShouldPolish: true,
		},
		{
			ID:                 "logistics_architecture_and_corridors",
			Title:              "Logistics Architecture And Corridors",
			Order:              5,
			Description:        "Explain the physical movement architecture: relay hubs, long-haul corridors, intermodal handoffs, and representative corridor patterns.",
			Prompt:             "Explain how physical movement is organized across corridors and transfer points in the visible network.",
			DeterministicDraft: buildBusinessArchitectureDraft(corridors, paths, structuralSignals),
			LLMReadyFactsJSON: marshalFacts(map[string]any{
				"illustrativeCorridors":    businessCorridorFacts(corridors[:minInt(len(corridors), 16)]),
				"illustrativePathExamples": businessPathFacts(paths[:minInt(len(paths), 10)]),
				"mainTransferHubs":         businessRefFacts(hubs[:minInt(len(hubs), 16)]),
				"hubRoleCandidates":        hubRoleFacts,
				"structuralSignals":        structuralSignals,
			}),
			ShouldPolish: true,
		},
		{
			ID:                 "dependencies_and_structural_vulnerabilities",
			Title:              "Dependencies And Structural Vulnerabilities",
			Order:              6,
			Description:        "Explain where the network concentrates and which hubs, facilities, geographies, product streams, or handoffs appear structurally important.",
			Prompt:             "Assess concentration and structural dependence in the network in consulting language, without drifting into system jargon.",
			DeterministicDraft: buildBusinessDependencyDraft(hubs, suppliers, customers, products, zones, dependencyReadout),
			LLMReadyFactsJSON: marshalFacts(map[string]any{
				"hubConcentration":           businessRefFacts(hubs[:minInt(len(hubs), 16)]),
				"supplyConcentration":        businessRefFacts(suppliers[:minInt(len(suppliers), 16)]),
				"demandConcentration":        businessRefFacts(customers[:minInt(len(customers), 16)]),
				"productConcentration":       businessRefFacts(products[:minInt(len(products), 12)]),
				"zoneConcentration":          businessRefFacts(zones[:minInt(len(zones), 12)]),
				"dependencyObservations":     dependencyReadout,
				"illustrativeEndToEndChains": chainFacts[:minInt(len(chainFacts), 6)],
			}),
			ShouldPolish: true,
		},
		{
			ID:                 "management_implications",
			Title:              "Management Implications",
			Order:              7,
			Description:        "Translate the visible network structure into management-level implications around dependence, concentration, route architecture, and strategic optionality.",
			Prompt:             "Write the management implications section of a strategy consulting report based on the provided logistics evidence.",
			DeterministicDraft: buildBusinessImplicationsDraft(hubs, suppliers, customers, products, corridors, structuralSignals),
			LLMReadyFactsJSON: marshalFacts(map[string]any{
				"reportSubject":              safeLensName(input.Lens),
				"visibleNetwork":             buildVisibleNetworkSummary(input, families, hubs, suppliers, customers, products),
				"strategicProductStreams":    productFacts[:minInt(len(productFacts), 8)],
				"mainTransferHubs":           businessRefFacts(hubs[:minInt(len(hubs), 8)]),
				"mainSupplyNodes":            businessRefFacts(suppliers[:minInt(len(suppliers), 8)]),
				"mainDemandNodes":            businessRefFacts(customers[:minInt(len(customers), 8)]),
				"illustrativeCorridors":      businessCorridorFacts(corridors[:minInt(len(corridors), 8)]),
				"dependencyObservations":     dependencyReadout,
				"structuralSignals":          structuralSignals,
				"illustrativeEndToEndChains": chainFacts[:minInt(len(chainFacts), 6)],
			}),
			ShouldPolish: true,
		},
		{
			ID:                 "scope_and_evidence_base",
			Title:              "Scope And Evidence Base",
			Order:              8,
			Description:        "Method and evidence appendix describing report coverage, route counts, and the deterministic basis of the report.",
			Prompt:             "List the report coverage, evidence basis, and appendix details behind the report.",
			DeterministicDraft: buildBusinessEvidenceDraft(input),
			LLMReadyFactsJSON: marshalFacts(map[string]any{
				"reportSubject":        safeLensName(input.Lens),
				"lensID":               input.Lens.ID,
				"projectID":            input.Lens.ProjectID,
				"generatedAtUTC":       input.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
				"scopeChecksum":        workscopeDigest(input),
				"initialScopeItems":    len(input.ScopePaths),
				"resolvedScopeItems":   len(input.Resolved.ScopePaths),
				"visibleRouteCount":    len(input.Chains),
				"resolvedEntityLabels": len(input.FragmentsByPath),
				"scopeIdentifiers":     input.Resolved.ScopePaths,
				"routeIdentifiers":     input.Resolved.MaterializedArtifactIDs,
			}),
			ShouldPolish: false,
		},
	}
}

func BuildSectionsFromPackages(packages []SectionPackage, polished map[string]PolishResult) []coremodels.ArtifactSection {
	now := time.Now().UTC()
	statusCompleted := "completed"
	sections := make([]coremodels.ArtifactSection, 0, len(packages))
	for _, pkg := range packages {
		content := sanitizeSectionContent(pkg.ID, pkg.Title, pkg.DeterministicDraft)
		if polished != nil {
			if item, ok := polished[pkg.ID]; ok && strings.TrimSpace(item.Content) != "" {
				content = sanitizeSectionContent(pkg.ID, pkg.Title, item.Content)
			}
		}
		description := pkg.Description
		contentCopy := content
		descCopy := description
		sections = append(sections, coremodels.ArtifactSection{
			ID:                pkg.ID,
			Title:             pkg.Title,
			Order:             pkg.Order,
			Prompt:            pkg.Prompt,
			Content:           &contentCopy,
			GeneratedAt:       &now,
			Description:       &descCopy,
			Required:          true,
			Editable:          true,
			AutoUpdateEnabled: false,
			GenerationStatus:  &statusCompleted,
		})
	}
	return sections
}

func PolishPackages(ctx context.Context, packages []SectionPackage, generator Generator, opts PolishOptions) map[string]PolishResult {
	if generator == nil || !opts.Enabled {
		return nil
	}
	opts = normalizePolishOptions(opts)
	out := make(map[string]PolishResult, len(packages))
	for _, pkg := range packages {
		if !pkg.ShouldPolish {
			continue
		}
		req := GenerateRequest{
			Model:             opts.Model,
			SystemInstruction: polishSystemInstruction(),
			Prompt:            polishPromptForPackage(pkg),
			MaxOutputTokens:   opts.MaxOutputTokens,
			ThinkingBudget:    opts.ThinkingBudget,
		}
		resp, err := generator.Generate(ctx, req)
		if err != nil || resp == nil || strings.TrimSpace(resp.Content) == "" {
			continue
		}
		out[pkg.ID] = PolishResult{
			Content:    cleanPolishedContent(resp.Content, pkg.Title),
			Model:      resp.Model,
			TokensUsed: resp.TokensUsed,
		}
	}
	return out
}

func polishSystemInstruction() string {
	return strings.TrimSpace(`
You are writing one section of a high-grade strategy consulting report about a real-world industrial and logistics system.

Rules:
- Use only facts present in SECTION_FACTS_JSON and DETERMINISTIC_DRAFT.
- Do not invent entities, routes, counts, hubs, products, dependencies, conclusions, or metrics.
- Write as if you are a partner at a top-tier strategy consultancy preparing a client memo.
- Prefer answer-first, concise, structured, management-oriented writing.
- Focus on what the supply chain is, how it works, where it concentrates, which corridors and facilities matter, and what management should understand.
- Treat the facts as evidence about an industrial system, not as a technical graph export.
- Use numbers sparingly. Numbers should support the interpretation, not dominate it.
- Prefer causal and functional language such as: manufactures, assembles, integrates, transfers, relays, launches, receives, operates, supports.
- Humanize labels lightly when obvious, but do not change meaning.
- You may infer a facility role from unmistakable words in entity names such as airport, port, terminal, factory, plant, repair center, ground station, control center, space center, launch site, or agency.
- You may use widely known public knowledge to explain the generic function of unmistakable facilities or hubs, but you must not invent network-specific relationships, flows, or dependencies that are not supported by the provided facts.
- Mention countries or national roles only when they are explicit in the provided names or directly implied by unmistakable well-known facility names; otherwise speak in terms of facilities, hubs, and industrial roles.
- Do not overstate scope. Describe the visible sub-network or operating pattern evidenced here, not an entire national sector, unless the facts clearly support that broader claim.
- Do not introduce timing, stress, or gap analysis unless explicit in the provided facts.
- Preserve numeric values and entity relationships exactly.
- Replace internal system vocabulary with normal domain language even if the source facts use internal field names.
- Outside the appendix, do not use terms such as: lens, graph, node, edge, artifact, path family, canonical path, workscope, materialized artifact, materialized path, relation meanings, transport semantics, scope path, flow refs, zone refs, bottleneck leg.
- Prefer common domain terms instead of system terms. Use this translation guide whenever the source facts or drafts contain internal wording:
  - materialized path / supply-chain path -> route, shipment route, movement, corridor instance
  - materialized flow -> visible route, documented movement, shipment flow
  - leg / leg narratives / legs -> route segment, transport segment, shipment segment, handoff sequence
  - path family / family cluster -> corridor pattern, route archetype, logistics pattern
  - supplier -> origin site, upstream supplier, production site, source facility
  - customer -> destination site, receiving facility, downstream recipient, end-user
  - product axis -> product stream, product category
  - anchor supplier / anchor customer -> primary origin / primary destination
  - node -> facility, site, hub, endpoint, organization
  - edge -> relationship, handoff, transfer link
  - artifact / materialized artifact -> document, evidence item, report input, route identifier
  - workscope / scope path / resolved scope path -> report scope, in-scope reference, in-scope entity, coverage reference
  - scope digest / workscope digest -> scope checksum, coverage checksum
  - canonical path -> identifier (appendix only), otherwise omit
  - transport via refs -> relay hubs, transfer points, intermediate hubs
  - flow refs -> movement references, route references
  - zone refs / affected zone -> industrial zone, operating area, logistics zone
  - discovered vs scoped paths -> additional related routes vs directly in-scope routes
  - validated vs heuristic -> confirmed in current evidence vs inferred from current evidence
  - open gaps -> coverage gaps, unresolved joins
  - bottleneck leg -> constraining route segment, critical handoff
  - route summary / primary mechanism -> corridor summary / primary movement pattern
- If the facts include technical identifiers, keep them confined to the appendix or a clearly labeled traceability note.
- Return markdown for the section only.
- Do not repeat the section title as a heading.
- Do not wrap the answer in code fences.
`)
}

func polishPromptForPackage(pkg SectionPackage) string {
	base := fmt.Sprintf(
		"SECTION_ID: %s\nSECTION_TITLE: %s\nSECTION_PURPOSE: %s\n\nDETERMINISTIC_DRAFT:\n%s\n\nSECTION_FACTS_JSON:\n%s\n",
		pkg.ID,
		pkg.Title,
		pkg.Description,
		strings.TrimSpace(pkg.DeterministicDraft),
		strings.TrimSpace(pkg.LLMReadyFactsJSON),
	)
	return base + "\n" + sectionSpecificInstructions(pkg)
}

func cleanPolishedContent(content string, title string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```markdown")
	content = strings.TrimPrefix(content, "```md")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	lines := strings.Split(content, "\n")
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		normalizedTitle := strings.ToLower(strings.TrimSpace(title))
		if strings.HasPrefix(first, "#") {
			firstTitle := strings.ToLower(strings.TrimSpace(strings.TrimLeft(first, "# ")))
			if firstTitle == normalizedTitle {
				lines = lines[1:]
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func sanitizeSectionContent(sectionID string, title string, content string) string {
	content = cleanPolishedContent(content, title)
	if strings.TrimSpace(sectionID) == "scope_and_evidence_base" {
		return content
	}
	return normalizeBusinessTerminology(content)
}

func normalizeBusinessTerminology(content string) string {
	replacements := []struct {
		pattern string
		value   string
	}{
		{`(?i)\bmaterialized paths\b`, "documented routes"},
		{`(?i)\bmaterialized path\b`, "documented route"},
		{`(?i)\bmaterialized flows\b`, "documented movements"},
		{`(?i)\bmaterialized flow\b`, "documented movement"},
		{`(?i)\bpath families\b`, "corridor patterns"},
		{`(?i)\bpath family\b`, "corridor pattern"},
		{`(?i)\bproduct axis\b`, "product stream"},
		{`(?i)\bscope paths\b`, "in-scope references"},
		{`(?i)\bscope path\b`, "in-scope reference"},
		{`(?i)\bartifact ids\b`, "route identifiers"},
		{`(?i)\bartifact id\b`, "route identifier"},
		{`(?i)\bbottleneck leg\b`, "critical handoff"},
		{`(?i)\bleg narratives\b`, "route segment narratives"},
		{`(?i)\blegs\b`, "route segments"},
		{`(?i)\bleg\b`, "route segment"},
		{`(?i)\bnodes\b`, "sites"},
		{`(?i)\bnode\b`, "site"},
		{`(?i)\bartifacts\b`, "evidence items"},
		{`(?i)\bartifact\b`, "evidence item"},
	}
	for _, item := range replacements {
		re := regexp.MustCompile(item.pattern)
		content = re.ReplaceAllString(content, item.value)
	}
	return content
}

func marshalFacts(value any) string {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func workscopeDigest(input *Input) string {
	if input == nil {
		return ""
	}
	return strings.TrimSpace(workscope.Digest(input.Resolved))
}

func buildVisibleNetworkSummary(input *Input, families []familySummary, hubs []refCount, suppliers []refCount, customers []refCount, products []refCount) map[string]any {
	return map[string]any{
		"subject":                  safeLensName(input.Lens),
		"visibleRoutes":            len(input.Chains),
		"distinctCorridorTypes":    len(families),
		"distinctTransferHubs":     len(hubs),
		"distinctOriginSites":      len(suppliers),
		"distinctDestinationSites": len(customers),
		"distinctProductStreams":   len(products),
	}
}

func mergeRefCountSlices(slices ...[]refCount) []refCount {
	out := make([]refCount, 0)
	seen := map[string]struct{}{}
	for _, items := range slices {
		for _, item := range items {
			key := strings.TrimSpace(firstNonEmpty(item.Ref, item.Label))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

func buildEntityRoleFacts(items []refCount, limit int) []map[string]any {
	out := make([]map[string]any, 0, minInt(len(items), limit))
	for _, item := range items[:minInt(len(items), limit)] {
		label := firstNonEmpty(item.Label, item.Ref)
		role := inferFacilityRole(label)
		out = append(out, map[string]any{
			"name":         label,
			"count":        item.Count,
			"facilityRole": role,
			"roleHint":     roleHint(role),
		})
	}
	return out
}

func inferFacilityRole(label string) string {
	text := strings.ToLower(strings.TrimSpace(label))
	switch {
	case strings.Contains(text, "ground station"), strings.Contains(text, "control center"), strings.Contains(text, "operations center"):
		return "operations or mission control site"
	case strings.Contains(text, "space center"), strings.Contains(text, "launch"), strings.Contains(text, "cosmodrome"):
		return "launch or space integration site"
	case strings.Contains(text, "factory"), strings.Contains(text, "manufactur"), strings.Contains(text, "plant"), strings.Contains(text, "works"):
		return "manufacturing or fabrication site"
	case strings.Contains(text, "repair"), strings.Contains(text, "service"):
		return "repair or service site"
	case strings.Contains(text, "airport"):
		return "air logistics hub"
	case strings.Contains(text, "port"):
		return "maritime logistics hub"
	case strings.Contains(text, "terminal"):
		return "transfer terminal"
	case strings.Contains(text, "rail"):
		return "rail logistics hub"
	case strings.Contains(text, "agency"), strings.Contains(text, "ministry"), strings.Contains(text, "authority"):
		return "institutional or end-user site"
	default:
		return "industrial, integration, or receiving facility"
	}
}

func roleHint(role string) string {
	switch role {
	case "operations or mission control site":
		return "indicates downstream operating, control, or service activity"
	case "launch or space integration site":
		return "indicates integration, launch preparation, or deployment activity"
	case "manufacturing or fabrication site":
		return "indicates upstream production or fabrication capability"
	case "repair or service site":
		return "indicates maintenance, refurbishment, or service support"
	case "air logistics hub", "maritime logistics hub", "transfer terminal", "rail logistics hub":
		return "indicates a relay or gateway role rather than final demand"
	case "institutional or end-user site":
		return "indicates program ownership, demand, or institutional reception"
	default:
		return "indicates a named facility participating in the chain"
	}
}

func buildChainArchetypeFacts(paths []pathSummary, limit int) []map[string]any {
	out := make([]map[string]any, 0, minInt(len(paths), limit))
	seen := map[string]struct{}{}
	for _, path := range paths[:minInt(len(paths), limit*2)] {
		originRole := inferFacilityRole(firstNonEmpty(path.SupplierLabel, path.SupplierRef))
		destinationRole := inferFacilityRole(firstNonEmpty(path.CustomerLabel, path.CustomerRef))
		viaRoles := make([]string, 0, len(path.ViaLabels))
		for _, via := range path.ViaLabels {
			viaRoles = append(viaRoles, inferFacilityRole(via))
		}
		title := buildArchetypeTitle(path.ProductLabel, originRole, viaRoles, destinationRole)
		if _, ok := seen[title]; ok {
			continue
		}
		seen[title] = struct{}{}
		out = append(out, map[string]any{
			"archetype":        title,
			"productStream":    firstNonEmpty(path.ProductLabel, path.ProductRef),
			"origin":           firstNonEmpty(path.SupplierLabel, path.SupplierRef),
			"originRole":       originRole,
			"via":              path.ViaLabels,
			"viaRoles":         viaRoles,
			"destination":      firstNonEmpty(path.CustomerLabel, path.CustomerRef),
			"destinationRole":  destinationRole,
			"movementSegments": path.LegNarratives[:minInt(len(path.LegNarratives), 4)],
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildArchetypeTitle(product string, originRole string, viaRoles []string, destinationRole string) string {
	parts := []string{originRole}
	if len(viaRoles) > 0 {
		parts = append(parts, viaRoles[0])
	}
	parts = append(parts, destinationRole)
	title := strings.Join(parts, " -> ")
	if product = strings.TrimSpace(product); product != "" {
		return product + " through " + title
	}
	return title
}

func buildStrategicProductFacts(products []refCount, paths []pathSummary, corridors []corridorSummary, limit int) []map[string]any {
	out := make([]map[string]any, 0, minInt(len(products), limit))
	for _, item := range products[:minInt(len(products), limit)] {
		name := firstNonEmpty(item.Label, item.Ref)
		originRoles := []string{}
		destinationRoles := []string{}
		examples := []string{}
		for _, path := range paths {
			if !sameProduct(path.ProductRef, path.ProductLabel, item.Ref, item.Label) {
				continue
			}
			originRoles = mergeUniqueStrings(originRoles, []string{inferFacilityRole(firstNonEmpty(path.SupplierLabel, path.SupplierRef))})
			destinationRoles = mergeUniqueStrings(destinationRoles, []string{inferFacilityRole(firstNonEmpty(path.CustomerLabel, path.CustomerRef))})
			examples = mergeUniqueStrings(examples, []string{buildProductExample(path)})
		}
		for _, corridor := range corridors {
			if !sameProduct(corridor.ProductRef, corridor.ProductLabel, item.Ref, item.Label) {
				continue
			}
			examples = mergeUniqueStrings(examples, []string{corridorSentence(corridor)})
		}
		out = append(out, map[string]any{
			"name":                 name,
			"count":                item.Count,
			"originRoles":          originRoles,
			"destinationRoles":     destinationRoles,
			"strategicImportance":  inferProductImportance(name, originRoles, destinationRoles),
			"illustrativeExamples": examples[:minInt(len(examples), 4)],
		})
	}
	return out
}

func sameProduct(refA string, labelA string, refB string, labelB string) bool {
	refA = strings.TrimSpace(refA)
	refB = strings.TrimSpace(refB)
	labelA = strings.TrimSpace(strings.ToLower(labelA))
	labelB = strings.TrimSpace(strings.ToLower(labelB))
	if refA != "" && refB != "" {
		return refA == refB
	}
	return labelA != "" && labelA == labelB
}

func buildProductExample(path pathSummary) string {
	product := firstNonEmpty(path.ProductLabel, path.ProductRef, "product")
	origin := firstNonEmpty(path.SupplierLabel, path.SupplierRef, "origin")
	destination := firstNonEmpty(path.CustomerLabel, path.CustomerRef, "destination")
	if len(path.ViaLabels) == 0 {
		return fmt.Sprintf("%s from %s to %s", product, origin, destination)
	}
	return fmt.Sprintf("%s from %s to %s via %s", product, origin, destination, strings.Join(path.ViaLabels, " -> "))
}

func inferProductImportance(name string, originRoles []string, destinationRoles []string) string {
	switch {
	case hasRole(destinationRoles, "launch or space integration site") && hasRole(originRoles, "manufacturing or fabrication site"):
		return "connects upstream production to downstream integration or deployment"
	case hasRole(destinationRoles, "operations or mission control site"):
		return "extends the chain into downstream operations or service delivery"
	case hasRole(originRoles, "manufacturing or fabrication site"):
		return "acts as an upstream industrial input into broader chains"
	case hasRole(originRoles, "air logistics hub") || hasRole(originRoles, "maritime logistics hub") || hasRole(originRoles, "transfer terminal"):
		return "moves through logistics gateways and appears to support cross-site transfer"
	default:
		return "anchors a visible part of the industrial chain and links multiple facilities"
	}
}

func hasRole(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func buildFootprintNarrative(roleFacts []map[string]any, hubs []refCount, suppliers []refCount, customers []refCount) []string {
	lines := []string{}
	if len(hubs) > 0 {
		lines = append(lines, fmt.Sprintf("Relay activity is visibly concentrated around %s.", joinTopCounts(hubs, 3)))
	}
	if len(suppliers) > 0 {
		lines = append(lines, fmt.Sprintf("Named origin facilities are led by %s.", joinTopCounts(suppliers, 3)))
	}
	if len(customers) > 0 {
		lines = append(lines, fmt.Sprintf("Named receiving or downstream facilities are led by %s.", joinTopCounts(customers, 3)))
	}
	if len(roleFacts) > 0 {
		lines = append(lines, "The visible footprint includes manufacturing facilities, transfer hubs, space or operating sites, and receiving sites rather than a single uniform facility type.")
	}
	return lines
}

func sectionSpecificInstructions(pkg SectionPackage) string {
	common := `
Writing requirements:
- Write for an executive reader who wants to understand the industry logic of the supply chain.
- Lead with explanation, not inventory.
- Do not sound like a platform, data, or analytics system.
- If you use bullets, make each bullet interpretive rather than descriptive.
- Prefer short paragraphs plus, where useful, one compact table.
- Numbers may appear, but they should confirm a point rather than drive the section.
- When interpreting importance, focus on functional role in end-to-end chains, not raw mention counts.
- When multiple named hubs, facilities, or endpoints are present, prefer explicit lists with one-line explanations over burying names in parentheses.`

	switch pkg.ID {
	case "what_this_network_is":
		return strings.TrimSpace(common + `

Section objective:
- Write the preamble that tells the reader what kind of industrial and logistics system this is.
- Explain the visible network as an operating supply system with upstream, transfer, integration, launch, service, or receiving roles if supported by the facts.
- If the named facilities clearly point to recognizable national or regional ecosystems, say so carefully and only when directly supported by the names.
- Treat this as a visible operating slice of a broader industry, unless the facts clearly support calling it a full sector-wide value chain.

Output shape:
- Start with one short answer-first paragraph.
- Follow with 3-4 flat bullets titled implicitly through their wording: industrial logic, geographic posture, system distinctiveness, and named anchors when useful.

Avoid:
- opening with counts
- listing every hub
- claiming that the visible network fully represents an entire country's industry unless the evidence clearly supports it
- talking about sections, data, evidence packs, graphs, or extracted entities`)
	case "industrial_roles_and_footprint":
		return strings.TrimSpace(common + `

Section objective:
- Explain who plays which role in the visible system.
- Translate facilities into business roles: manufacturing, transfer, integration, launch, operations, repair, institutional demand, receiving.
- Describe the footprint as a distributed industrial architecture, not as a list of places.

Output shape:
- One short framing paragraph.
- Then use explicit grouped lists, not just prose:
  Primary production and integration sites: 3-6 bullets
  Key relay hubs: 4-8 bullets
  Launch, operations, or receiving endpoints: 3-6 bullets
- Each bullet must name the facility and explain its apparent role in one sentence.

Interpretation rule:
- Country-level interpretation is allowed only when the facility names make geography explicit or unmistakable.
- If geography is ambiguous, stay at facility and role level.`)
	case "end_to_end_chains":
		return strings.TrimSpace(common + `

Section objective:
- Explain the recurring end-to-end chains and how they close operationally.
- Describe what the chain does in real terms: for example, moving products from fabrication into integration, onward into launch or operations, or into receiving/service sites.
- Use representative chains to explain the system, not to enumerate every route.

Output shape:
- One short framing paragraph.
- Then 3-6 bullets or a compact table. Each item should describe one chain archetype with:
  functional purpose,
  typical origin role,
  transfer pattern,
  downstream closing point.

Avoid:
- repeating raw segment-by-segment route text unless it helps explain an archetype
- treating each chain as equally important`)
	case "strategic_product_streams":
		return strings.TrimSpace(common + `

Section objective:
- Explain which product streams matter because they enable or anchor the visible end-to-end chains.
- Assess importance by role in the chain, not by count alone.
- Clarify whether each product stream looks like an upstream industrial input, an integration-stage component, a delivered output, or an operations/service-enabling stream.

Output shape:
- One short framing paragraph.
- Then a compact table with columns Product stream / Why it matters / Where it sits in the chain / Named chain examples.

Avoid:
- saying a product is important only because it appears often
- reproducing raw product identifiers without humanizing them when obvious`)
	case "logistics_architecture_and_corridors":
		return strings.TrimSpace(common + `

Section objective:
- Explain the physical movement architecture of the network.
- Focus on how corridors are organized, what kinds of hubs relay movement, and where intermodal or cross-border structure appears.
- Make the reader understand the logistics design, not the raw routes.

Output shape:
- One short framing paragraph.
- Then two explicit blocks:
  Key relay hubs: 4-8 bullets naming hubs and why each matters
  Corridor patterns: 3-6 bullets describing dominant movement patterns
- Use named corridor examples selectively to illustrate the architecture.

Avoid:
- route dumps
- long lists of place names without explaining their role`)
	case "dependencies_and_structural_vulnerabilities":
		return strings.TrimSpace(common + `

Section objective:
- Explain the structural dependencies in the visible network.
- Focus on concentration around specific hubs, origin facilities, receiving sites, product streams, and transfer patterns.
- Describe vulnerability in structural terms such as reliance on a small number of relay points, integration sites, or gateway facilities.

Output shape:
- One short framing paragraph.
- Then 4-8 bullets on the main dependencies.
- Each bullet must name the specific facility, hub, zone, or product stream and explain why the network appears dependent on it.

Avoid:
- operational timing or lead-time analysis
- generic risk language without tying it to a concrete facility, corridor, or product stream`)
	case "management_implications":
		return strings.TrimSpace(common + `

Section objective:
- Translate the structure of the visible supply system into management implications.
- Explain what a leadership team should understand about concentration, operating dependence, strategic flexibility, and where optionality seems limited or broader.
- Keep the tone strategic and practical.

Output shape:
- A short paragraph followed by 4-6 bullets starting with action-oriented or implication-oriented phrasing.

Avoid:
- inventing recommendations that are not grounded in the visible structure
- repeating prior sections without synthesis`)
	default:
		return strings.TrimSpace(common + `

Rewrite the section into polished executive markdown. Keep it factual, concise, and readable.`)
	}
}

func businessFamilyFacts(items []familySummary) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"pattern":       item.Title,
			"productStream": firstNonEmpty(item.ProductLabel, item.ProductAxis),
			"origin":        firstNonEmpty(item.SupplierLabel, item.SupplierRef),
			"destination":   firstNonEmpty(item.CustomerLabel, item.CustomerRef),
			"routes":        item.PathCount,
			"typicalHubs":   item.TypicalVia,
		})
	}
	return out
}

func businessRefFacts(items []refCount) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"name":  firstNonEmpty(item.Label, item.Ref),
			"count": item.Count,
		})
	}
	return out
}

func businessCorridorFacts(items []corridorSummary) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"productStream": firstNonEmpty(item.ProductLabel, item.ProductRef),
			"origin":        firstNonEmpty(item.SupplierLabel, item.SupplierRef),
			"destination":   firstNonEmpty(item.CustomerLabel, item.CustomerRef),
			"via":           item.ViaLabels,
			"routes":        item.PathCount,
			"segmentCount":  item.LegCount,
		})
	}
	return out
}

func businessPathFacts(items []pathSummary) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"title":            item.DisplayName,
			"productStream":    firstNonEmpty(item.ProductLabel, item.ProductRef),
			"origin":           firstNonEmpty(item.SupplierLabel, item.SupplierRef),
			"destination":      firstNonEmpty(item.CustomerLabel, item.CustomerRef),
			"via":              item.ViaLabels,
			"movementSegments": item.LegNarratives,
		})
	}
	return out
}

func buildFlowArchetypes(relations []relationSummary) []string {
	out := make([]string, 0, len(relations))
	for _, item := range relations {
		switch strings.TrimSpace(item.Key) {
		case "logistics_leg", "product_flow":
			out = append(out, "physical transfer between facilities and service nodes")
		case "ASSEMBLES_INTO":
			out = append(out, "assembly and integration handoff between product stages")
		case "TRANSFORMS_INTO":
			out = append(out, "product transformation between stages of production or service delivery")
		}
	}
	return mergeUniqueStrings(nil, out)
}

func buildStructuralSignals(modes []relationSummary) []string {
	out := make([]string, 0, len(modes))
	for _, item := range modes {
		switch strings.TrimSpace(item.Key) {
		case "intermodal":
			out = append(out, "flows frequently depend on intermodal handoffs")
		case "cross_border":
			out = append(out, "flows frequently cross borders")
		case "zone_bound":
			out = append(out, "many movements are concentrated in a limited set of industrial or logistics zones")
		case "sea":
			out = append(out, "some corridors rely on maritime transfer points")
		case "rail":
			out = append(out, "some corridors rely on rail transfer")
		}
	}
	return mergeUniqueStrings(nil, out)
}

func buildBusinessExecutiveDraft(input *Input, families []familySummary, hubs []refCount, suppliers []refCount, customers []refCount, products []refCount) string {
	lines := []string{
		fmt.Sprintf("The visible logistics network for `%s` contains %d documented movements across %d recurring logistics patterns.", safeLensName(input.Lens), len(input.Chains), len(families)),
	}
	if len(hubs) > 0 {
		lines = append(lines, fmt.Sprintf("Transfer activity is concentrated in %s.", joinTopCounts(hubs, 3)))
	}
	if len(products) > 0 {
		lines = append(lines, fmt.Sprintf("The network is organized primarily around %s.", joinTopCounts(products, 3)))
	}
	if len(customers) > 0 && len(suppliers) > 0 {
		lines = append(lines, fmt.Sprintf("Demand is concentrated at %s, while supply-side activity is concentrated at %s.", joinTopCounts(customers, 2), joinTopCounts(suppliers, 2)))
	}
	return strings.Join(lines, "\n")
}

func buildBusinessIdentityDraft(input *Input, families []familySummary, products []refCount, chainFacts []map[string]any, hubs []refCount) string {
	lines := []string{
		"The visible network reads as an operating industrial system rather than a simple transport list: named facilities suggest upstream production, relay hubs, and downstream integration, launch, service, or receiving roles.",
	}
	if len(chainFacts) > 0 {
		lines = append(lines, fmt.Sprintf("Its clearest logic is visible in chains such as %s.", joinChainArchetypeTitles(chainFacts, 3)))
	}
	if len(products) > 0 {
		lines = append(lines, fmt.Sprintf("The network appears to be organized around product streams such as %s, which act as the spine of recurring supply movements.", joinTopCounts(products, 3)))
	}
	if len(hubs) > 0 {
		lines = append(lines, fmt.Sprintf("Movement is visibly funneled through a small set of relay hubs led by %s.", joinTopCounts(hubs, 3)))
	}
	if len(families) > 0 {
		lines = append(lines, "The recurring patterns suggest a stable operating structure rather than isolated one-off shipments.")
	}
	return strings.Join(lines, "\n")
}

func buildBusinessRolesDraft(roleFacts []map[string]any, hubs []refCount, suppliers []refCount, customers []refCount, zones []refCount) string {
	lines := []string{
		"The named facilities point to a distributed industrial footprint in which production, relay, integration, operating, and receiving roles are split across a limited set of places.",
	}
	if len(roleFacts) > 0 {
		lines = append(lines, fmt.Sprintf("Examples include %s.", joinRoleExamples(roleFacts, 4)))
	}
	if len(hubs) > 0 {
		lines = append(lines, fmt.Sprintf("Relay activity is anchored on %s.", joinTopCounts(hubs, 3)))
	}
	if len(suppliers) > 0 && len(customers) > 0 {
		lines = append(lines, fmt.Sprintf("The visible footprint starts from facilities such as %s and closes into downstream sites such as %s.", joinTopCounts(suppliers, 2), joinTopCounts(customers, 2)))
	}
	if len(zones) > 0 {
		lines = append(lines, fmt.Sprintf("Zone-level concentration is also visible around %s.", joinTopCounts(zones, 3)))
	}
	return strings.Join(lines, "\n")
}

func buildBusinessChainsDraft(chainFacts []map[string]any, flowArchetypes []string) string {
	lines := []string{
		"The visible supply system closes through a small set of recurring chain shapes, moving products from origin facilities through relay points into downstream industrial or operating endpoints.",
	}
	if len(chainFacts) > 0 {
		lines = append(lines, fmt.Sprintf("The clearest archetypes are %s.", joinChainArchetypeTitles(chainFacts, 4)))
	}
	if len(flowArchetypes) > 0 {
		lines = append(lines, "Across these chains, the repeated operating logic includes "+strings.Join(flowArchetypes[:minInt(len(flowArchetypes), 3)], "; ")+".")
	}
	return strings.Join(lines, "\n")
}

func buildBusinessProductDraft(productFacts []map[string]any) string {
	lines := []string{
		"Strategically important product streams are the ones that hold the visible chains together by connecting production, transfer, integration, and downstream operating stages.",
	}
	if len(productFacts) > 0 {
		lines = append(lines, fmt.Sprintf("The clearest product anchors are %s.", joinProductTitles(productFacts, 4)))
	}
	return strings.Join(lines, "\n")
}

func buildBusinessArchitectureDraft(corridors []corridorSummary, paths []pathSummary, structuralSignals []string) string {
	lines := []string{
		"The logistics architecture is shaped by a limited number of long-haul and relay patterns rather than by a fully diffuse network.",
	}
	if len(corridors) > 0 {
		lines = append(lines, fmt.Sprintf("Representative corridor structures include %s.", joinCorridorTitles(corridors, 4)))
	}
	if len(paths) > 0 {
		lines = append(lines, "Representative movements show repeated use of transfer hubs between named origin and downstream facilities.")
	}
	if len(structuralSignals) > 0 {
		lines = append(lines, "Structurally, "+strings.Join(structuralSignals[:minInt(len(structuralSignals), 3)], "; ")+".")
	}
	return strings.Join(lines, "\n")
}

func joinChainArchetypeTitles(items []map[string]any, limit int) string {
	parts := make([]string, 0, minInt(len(items), limit))
	for _, item := range items[:minInt(len(items), limit)] {
		title, _ := item["archetype"].(string)
		if strings.TrimSpace(title) == "" {
			continue
		}
		parts = append(parts, "`"+strings.TrimSpace(title)+"`")
	}
	if len(parts) == 0 {
		return "a small set of recurring industrial chains"
	}
	return strings.Join(parts, ", ")
}

func joinRoleExamples(items []map[string]any, limit int) string {
	parts := make([]string, 0, minInt(len(items), limit))
	for _, item := range items[:minInt(len(items), limit)] {
		name, _ := item["name"].(string)
		role, _ := item["facilityRole"].(string)
		if strings.TrimSpace(name) == "" || strings.TrimSpace(role) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("`%s` as %s", name, role))
	}
	if len(parts) == 0 {
		return "named facilities with distinct industrial roles"
	}
	return strings.Join(parts, ", ")
}

func joinProductTitles(items []map[string]any, limit int) string {
	parts := make([]string, 0, minInt(len(items), limit))
	for _, item := range items[:minInt(len(items), limit)] {
		name, _ := item["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		parts = append(parts, "`"+strings.TrimSpace(name)+"`")
	}
	if len(parts) == 0 {
		return "the main visible product streams"
	}
	return strings.Join(parts, ", ")
}

func buildBusinessNetworkLogicDraft(families []familySummary, flowArchetypes []string, structuralSignals []string) string {
	lines := []string{
		"The visible network links production and service sites to downstream assembly, launch, control, and receiving facilities.",
	}
	if len(families) > 0 {
		lines = append(lines, fmt.Sprintf("Dominant flow patterns include %s.", joinFamilyTitles(families, 4)))
	}
	if len(flowArchetypes) > 0 {
		lines = append(lines, "The network combines "+strings.Join(flowArchetypes[:minInt(len(flowArchetypes), 3)], "; ")+".")
	}
	if len(structuralSignals) > 0 {
		lines = append(lines, "Operationally, "+strings.Join(structuralSignals[:minInt(len(structuralSignals), 3)], "; ")+".")
	}
	return strings.Join(lines, "\n")
}

func buildBusinessFootprintDraft(hubs []refCount, suppliers []refCount, customers []refCount, zones []refCount) string {
	lines := []string{
		"The visible footprint is shaped by a limited set of transfer hubs, supply locations, receiving facilities, and industrial zones.",
	}
	if len(hubs) > 0 {
		lines = append(lines, fmt.Sprintf("The main transfer system is anchored on %s.", joinTopCounts(hubs, 3)))
	}
	if len(suppliers) > 0 {
		lines = append(lines, fmt.Sprintf("Supply-side activity is led by %s.", joinTopCounts(suppliers, 3)))
	}
	if len(customers) > 0 {
		lines = append(lines, fmt.Sprintf("Receiving activity is led by %s.", joinTopCounts(customers, 3)))
	}
	if len(zones) > 0 {
		lines = append(lines, fmt.Sprintf("Geographic concentration is most visible in %s.", joinTopCounts(zones, 3)))
	}
	return strings.Join(lines, "\n")
}

func buildBusinessCorridorDraft(corridors []corridorSummary, paths []pathSummary) string {
	lines := []string{
		"The visible network is best understood through a small set of illustrative corridors rather than through every individual route instance.",
	}
	if len(corridors) > 0 {
		lines = append(lines, fmt.Sprintf("Illustrative corridors include %s.", joinCorridorTitles(corridors, 4)))
	}
	if len(paths) > 0 {
		lines = append(lines, "Representative examples are available for more complex multi-step movements across assembly, launch, and service sites.")
	}
	return strings.Join(lines, "\n")
}

func buildBusinessDependencyDraft(hubs []refCount, suppliers []refCount, customers []refCount, products []refCount, zones []refCount, dependencyReadout []string) string {
	lines := []string{
		"The network is not evenly distributed: it shows clear concentration around a small number of hubs, facilities, product streams, and zones.",
	}
	for _, line := range dependencyReadout[:minInt(len(dependencyReadout), 4)] {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func buildBusinessImplicationsDraft(hubs []refCount, suppliers []refCount, customers []refCount, products []refCount, corridors []corridorSummary, structuralSignals []string) string {
	lines := []string{
		"Management should read this network primarily through concentration, transfer complexity, and dependence on a limited set of external relay points.",
	}
	if len(hubs) > 0 {
		lines = append(lines, fmt.Sprintf("The clearest operational dependence sits around %s.", joinTopCounts(hubs, 2)))
	}
	if len(corridors) > 0 {
		lines = append(lines, "A small set of visible corridors carries the most strategically relevant flows in the current network snapshot.")
	}
	if len(structuralSignals) > 0 {
		lines = append(lines, "The network also shows repeated handoffs and cross-border movement, which matters for operational coordination even without introducing timing metrics.")
	}
	if len(products) > 0 {
		lines = append(lines, fmt.Sprintf("Product exposure is centered on %s.", joinTopCounts(products, 3)))
	}
	if len(suppliers) > 0 && len(customers) > 0 {
		lines = append(lines, fmt.Sprintf("The most visible origin sites are %s, while the most visible receiving sites are %s.", joinTopCounts(suppliers, 2), joinTopCounts(customers, 2)))
	}
	return strings.Join(lines, "\n")
}

func buildBusinessEvidenceDraft(input *Input) string {
	lines := []string{
		fmt.Sprintf("Subject: `%s`.", safeLensName(input.Lens)),
		fmt.Sprintf("Visible logistics routes included: %d.", len(input.Chains)),
		fmt.Sprintf("Resolved in-scope references: %d.", len(input.Resolved.ScopePaths)),
		fmt.Sprintf("Resolved facility and entity labels: %d.", len(input.FragmentsByPath)),
		fmt.Sprintf("Scope checksum: `%s`.", workscopeDigest(input)),
		"",
		"### In-scope identifiers",
		"",
	}
	for _, path := range input.Resolved.ScopePaths[:minInt(len(input.Resolved.ScopePaths), 40)] {
		lines = append(lines, fmt.Sprintf("- `%s`", path))
	}
	if len(input.Resolved.ScopePaths) > 40 {
		lines = append(lines, fmt.Sprintf("- ... and %d more", len(input.Resolved.ScopePaths)-40))
	}
	return strings.Join(lines, "\n")
}

func joinFamilyTitles(items []familySummary, limit int) string {
	parts := make([]string, 0, minInt(len(items), limit))
	for _, item := range items[:minInt(len(items), limit)] {
		parts = append(parts, "`"+item.Title+"`")
	}
	return strings.Join(parts, ", ")
}

func joinCorridorTitles(items []corridorSummary, limit int) string {
	parts := make([]string, 0, minInt(len(items), limit))
	for _, item := range items[:minInt(len(items), limit)] {
		title := fmt.Sprintf("`%s` from `%s` to `%s`", firstNonEmpty(item.ProductLabel, item.ProductRef, "product"), firstNonEmpty(item.SupplierLabel, item.SupplierRef, "origin"), firstNonEmpty(item.CustomerLabel, item.CustomerRef, "destination"))
		if len(item.ViaLabels) > 0 {
			title += " via `" + strings.Join(item.ViaLabels, " -> ") + "`"
		}
		parts = append(parts, title)
	}
	return strings.Join(parts, ", ")
}

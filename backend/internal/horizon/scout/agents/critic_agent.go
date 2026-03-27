package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// CriticAgent проверяет извлеченные данные на соответствие источникам
type CriticAgent struct {
	executor *ai.AITaskExecutor
}

// NewCriticAgent создает новый Critic Agent
func NewCriticAgent(executor *ai.AITaskExecutor) *CriticAgent {
	return &CriticAgent{
		executor: executor,
	}
}

// ValidationResult результат валидации Critic Agent
type ValidationResult struct {
	HasConflicts bool                 `json:"hasConflicts"`
	Conflicts    []ValidationConflict `json:"conflicts,omitempty"`
	Confidence   float64              `json:"confidence"` // 0..1
	IsValid      bool                 `json:"isValid"`
	MissingInfo  []string             `json:"missingInfo,omitempty"` // Список вопросов для доизучения
}

// ValidationConflict конфликт между извлеченными данными и источниками
type ValidationConflict struct {
	Field          string      `json:"field"`
	ExtractedValue interface{} `json:"extractedValue"`
	SourceValue    interface{} `json:"sourceValue"`
	Reason         string      `json:"reason"`
	Severity       string      `json:"severity"` // "low", "medium", "high"
}

// Validate проверяет извлеченные данные на соответствие источникам
func (a *CriticAgent) Validate(ctx context.Context, extractedData *models.HorizonFragment, relations []DiscoveredRelation, supplyChains []SupplyChain, sourceContent string, specialties []string) (*ValidationResult, error) {
	return a.ValidateWithGraphContext(ctx, extractedData, relations, supplyChains, sourceContent, specialties, "", nil)
}

// ValidateWithGraphContext validates extracted data and can optionally be given a compact "graph context"
// (e.g., existing market/gov skeleton) to assess hierarchy completeness beyond the current source content.
func (a *CriticAgent) ValidateWithGraphContext(
	ctx context.Context,
	extractedData *models.HorizonFragment,
	relations []DiscoveredRelation,
	supplyChains []SupplyChain,
	sourceContent string,
	specialties []string,
	graphContext string,
	domainModes []string,
) (*ValidationResult, error) {
	if a == nil {
		return nil, fmt.Errorf("critic agent is nil")
	}
	domainModes = contractsemantics.SortDomainModesForExecution(domainModes)
	if len(domainModes) > 0 {
		input := CriticValidatorInput{
			Fragment:            extractedData,
			Relations:           relations,
			SupplyChains:        supplyChains,
			WorkingProductScope: collectWorkingProductScopeFromChains(supplyChains),
			SourceContent:       sourceContent,
			GraphContext:        graphContext,
		}
		out, err := a.CriticPipeline().Validate(ctx, input, domainModes)
		if err != nil {
			return nil, err
		}
		if out != nil {
			return enforceValidationRelevance(extractedData, out), nil
		}
	}

	prompt := a.buildValidationPrompt(extractedData, relations, supplyChains, sourceContent, specialties, graphContext)
	out, err := a.runValidationPrompt(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return enforceValidationRelevance(extractedData, out), nil
}

// RuleBasedQuickValidation performs deterministic structural completeness checks per active
// domain mode, producing missingInfo for the research agent. Uses entity_semantics.MissingInfoRules
// and taxonomy domain mode rules as single source of truth.
func RuleBasedQuickValidation(
	fragment *models.HorizonFragment,
	relations []DiscoveredRelation,
	supplyChains []SupplyChain,
	domainModes []string,
) *ValidationResult {
	domainModes = contractsemantics.SortDomainModesForExecution(domainModes)

	// Extract relation types for evaluation
	relTypes := make([]string, 0, len(relations))
	for _, r := range relations {
		rt := strings.TrimSpace(strings.ToUpper(r.RelationType))
		if rt != "" {
			relTypes = append(relTypes, rt)
		}
	}

	targetPath := ""
	communityType := ""
	if fragment != nil {
		targetPath = strings.TrimSpace(fragment.CanonicalPath)
		communityType = strings.TrimSpace(fragment.CommunityType)
	}

	// Resolve entity type and collect rules
	key := entity_semantics.ResolveByPath(targetPath, fragmentTypeOrEmpty(fragment), communityType)
	def := entity_semantics.GetDefinition(key)
	rules := make([]entity_semantics.MissingInfoRule, 0)
	if def != nil && len(def.MissingInfoRules) > 0 {
		rules = append(rules, def.MissingInfoRules...)
	}
	rules = append(rules, entity_semantics.DomainModeRulesForModes(domainModes)...)

	missing := entity_semantics.EvaluateMissingInfoRules(fragment, relTypes, rules, domainModes, targetPath, entity_semantics.EvaluateModeFull)

	// Supply chain invariants (deterministic checks)
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) {
		invariantGaps := checkSupplyChainInvariants(supplyChains, collectWorkingProductScopeFromChains(supplyChains))
		missing = append(missing, invariantGaps...)
	}

	return &ValidationResult{
		IsValid:     true,
		Confidence:  0.5,
		MissingInfo: missing,
		Conflicts:   []ValidationConflict{},
	}
}

func collectWorkingProductScopeFromChains(chains []SupplyChain) []string {
	out := make([]string, 0, len(chains))
	for _, chain := range chains {
		for _, node := range chain.Nodes {
			if cp := strings.TrimSpace(node.CanonicalPath); strings.HasPrefix(cp, "/product/") {
				out = append(out, cp)
			}
		}
		for _, candidate := range chain.ProductCandidates {
			if cp := strings.TrimSpace(candidate.CanonicalPath); strings.HasPrefix(cp, "/product/") {
				out = append(out, cp)
			}
		}
	}
	return dedupWorkingProductScopeRefs(out)
}

func dedupWorkingProductScopeRefs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func fragmentTypeOrEmpty(f *models.HorizonFragment) models.FragmentType {
	if f == nil {
		return ""
	}
	return f.Type
}

// ValidateStrategicOverlay validates DECISION_POINT and MECHANISM entities in Phase 2.
// This is a dedicated method with a focused prompt that only covers strategic layer rules,
// without the general org/fac/person/market validation logic.
func (a *CriticAgent) ValidateStrategicOverlay(
	ctx context.Context,
	fragment *models.HorizonFragment,
	relations []DiscoveredRelation,
	graphContext string,
) (*ValidationResult, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("critic agent not configured")
	}
	if fragment == nil {
		return nil, fmt.Errorf("fragment is required")
	}

	prompt := a.buildStrategicValidationPrompt(fragment, relations, graphContext)

	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         8000,
			MaxOutputSize:            2048,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to validate strategic entity: %w", err)
	}

	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	validationResult, err := parseValidationResultJSON(jsonData)
	if err != nil {
		return nil, err
	}

	return validationResult, nil
}

// buildStrategicValidationPrompt builds a focused prompt for Phase 2 strategic entity validation.
func (a *CriticAgent) buildStrategicValidationPrompt(fragment *models.HorizonFragment, relations []DiscoveredRelation, graphContext string) string {
	fragmentJSON, _ := json.MarshalIndent(fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(relations, "", "  ")

	graphContextBlock := ""
	if strings.TrimSpace(graphContext) != "" {
		graphContextBlock = fmt.Sprintf(`
GRAPH CONTEXT (existing Lens graph):
%s

`, strings.TrimSpace(graphContext))
	}
	var entityType string
	switch fragment.Type {
	case models.FragmentTypeDecisionPoint:
		entityType = "DECISION_POINT"
	case models.FragmentTypeMechanism:
		entityType = "MECHANISM"
	default:
		entityType = "UNKNOWN"
	}

	return fmt.Sprintf(`You are a Phase 2 Strategic Overlay Critic. Your task is to validate DECISION_POINT and MECHANISM entities.

ENTITY TYPE: %s

ENTITY (JSON):
%s

CONNECTED RELATIONS (JSON):
%s
%s
═══════════════════════════════════════════════════════════════════════════════
CORE SEMANTIC DISTINCTION: DECISION POINT vs MECHANISM
═══════════════════════════════════════════════════════════════════════════════

DECISION POINT = A CONCRETE DECISION/ACTION (verb-based)
  - Title MUST be an ACTION (verb phrase): "Grant License", "Approve Export", "Block Access"
  - Represents a MOMENT of choice where something can be blocked/enabled/delayed
  - Path format: /decision/{domain-slug}/{decision-slug}
  - GOOD titles: "Grant Banking License", "Approve EUV Export", "Block Maritime Transit"
  - BAD titles: "Export Control" (noun), "Banking Oversight" (noun), "Regulatory Supervision" (noun)

MECHANISM = A SPECIFIC INSTRUMENT/REGIME OF CONTROL (noun-based)
  - Title MUST be an INSTRUMENT (noun phrase): "Banking License", "Entity List", "Export Control Regime"
  - Represents a REUSABLE tool that agents possess and apply
  - Path format: /mechanism/{category}/{slug}
  - category MUST be: regulatory | economic | technological | contractual | political
  - slug MUST specify the object ("license WHAT?", "controls WHAT?")
  - GOOD slugs: %s
  - BAD slugs: "licensing", "regulation", "oversight", "supervision" (too abstract)

═══════════════════════════════════════════════════════════════════════════════
VALIDATION RULES
═══════════════════════════════════════════════════════════════════════════════

For DECISION_POINT:
1. PATH VALIDATION:
   - Path MUST match /decision/{domain-slug}/{decision-slug}
   - If malformed → conflict with HIGH severity

2. TITLE VALIDATION:
   - Title MUST start with an action verb (Grant, Approve, Block, Authorize, Deny, Permit, Revoke, Suspend)
   - If title is a noun phrase (e.g., "Export Control", "Banking Supervision") → conflict with MEDIUM severity
   - Suggest rephrasing: "Export Control" → "Approve Export" or "Block Export"

3. EDGE COMPLETENESS:
   - MUST have at least one CONTROLS_ACCESS or IMPOSES_CONDITION edge (what does this decision control?)
   - If missing → missingInfo: "What does <decision_point> control or block access to? Return CONTROLS_ACCESS edges with targetCanonicalPath = controlled entity."
   - MUST have at least one GATEKEEPS_DECISION edge pointing TO this decision (who decides?)
   - If missing → missingInfo: "Who gatekeeps <decision_point>? Return GATEKEEPS_DECISION edge with EXPLICIT SOURCE: sourceCanonicalPath=/org/... or /gov/... or /community/... (the gatekeeper), sourceType=ORGANIZATION or GOV, sourceName=gatekeeper name. Target is THIS DECISION POINT. CRITICAL: The gatekeeper MUST be the source, NOT the decision point."
   - SHOULD have USES_MECHANISM edge (how is control exercised?)
   - If missing → missingInfo: "What mechanism does <decision_point> use? Return USES_MECHANISM edge with targetCanonicalPath=/mechanism/{category}/{slug}."

4. ISOLATED DECISION POINTS:
   - Decision point with NO connected edges → HIGH severity conflict
   - Decision point must be connected to the graph

For MECHANISM:
1. PATH VALIDATION:
   - Path MUST match /mechanism/{category}/{slug}
   - category MUST be one of: regulatory, economic, technological, contractual, political
   - If invalid category → conflict with HIGH severity

2. SLUG SPECIFICITY:
   - Slug MUST specify the object (what is being licensed/regulated/controlled?)
   - FORBIDDEN generic slugs: licensing, regulation, oversight, supervision, compliance, monitoring, enforcement
   - If slug is generic → conflict with MEDIUM severity + missingInfo:
     "Specify what <mechanism> applies to (license what / regulate what) and update slug to be concrete."

3. EDGE COMPLETENESS:
   - MUST have at least one USES_MECHANISM edge pointing TO this mechanism (who uses it?)
   - If missing → missingInfo: "What agents/organizations use <mechanism>? Return USES_MECHANISM edges with EXPLICIT SOURCE: sourceCanonicalPath=/org/... or /gov/... or /decision/... (the user), sourceType=ORGANIZATION or GOV or DECISION_POINT, sourceName=user name. Target is THIS MECHANISM. CRITICAL: The user/agent MUST be the source, NOT the mechanism."
   - MUST have at least one APPLIES_TO edge (what/who does it affect? — downstream impact is required)
   - If missing → missingInfo: "What does <mechanism> affect? Return APPLIES_TO edges with targetCanonicalPath = affected entity (org/facility/product/market) from the graph. Source is THIS MECHANISM. CRITICAL: Every mechanism must have concrete downstream targets."

4. ORPHAN MECHANISMS:
   - Mechanism with NO incoming and NO outgoing edges → HIGH severity conflict
   - Every mechanism must be used by someone and apply to something

STRATEGIC EDGE TYPE VALIDATION:
- CONTROLS_ACCESS: source = DECISION_POINT/ORG/GOV/COMMUNITY/PROGRAM, target = ORG/FACILITY/PRODUCT/MARKET/PROGRAM/COMMUNITY
- IMPOSES_CONDITION: source = MECHANISM/ORG/GOV/COMMUNITY/PROGRAM/DECISION_POINT, target = ORG/FACILITY/PRODUCT/MARKET/PROGRAM/COMMUNITY
- GATEKEEPS_DECISION: source = ORG/GOV/COMMUNITY/PROGRAM/PERSON, target = DECISION_POINT
- USES_MECHANISM: source = DECISION_POINT/ORG/GOV/COMMUNITY/PROGRAM, target = MECHANISM (MUST be /mechanism/...)
- APPLIES_TO: source = MECHANISM, target = ORG/FACILITY/PRODUCT/MARKET/COMMUNITY/PROGRAM

CRITICAL: USES_MECHANISM target MUST be /mechanism/... (never /decision/...)

═══════════════════════════════════════════════════════════════════════════════
OUTPUT
═══════════════════════════════════════════════════════════════════════════════

Return a JSON object:
{
  "hasConflicts": boolean,
  "conflicts": [
    {
      "field": "string (e.g., 'title', 'canonicalPath', 'slug', 'edges')",
      "extractedValue": "current value",
      "sourceValue": "expected/suggested value",
      "reason": "explanation",
      "severity": "low" | "medium" | "high"
    }
  ],
  "confidence": 0.0-1.0,
  "isValid": boolean,
  "missingInfo": ["specific question to enrich this entity..."]
}

Return ONLY valid JSON. No markdown fences.
`, entityType, string(fragmentJSON), string(relationsJSON), graphContextBlock, taxonomy.MechanismSlugExamplesString())
}

// buildValidationPrompt строит промпт для валидации данных
func (a *CriticAgent) buildValidationPrompt(extractedData *models.HorizonFragment, relations []DiscoveredRelation, supplyChains []SupplyChain, sourceContent string, specialties []string, graphContext string) string {
	if extractedData == nil {
		extractedData = &models.HorizonFragment{}
	}
	extractedJSON, _ := json.MarshalIndent(extractedData, "", "  ")
	relationsJSON, _ := json.MarshalIndent(relations, "", "  ")
	supplyChainsJSON := "[]"
	if len(supplyChains) > 0 {
		if b, err := json.MarshalIndent(supplyChains, "", "  "); err == nil {
			supplyChainsJSON = string(b)
		}
	}
	specialties = taxonomy.NormalizeSpecialties(specialties)
	specBlock := ""
	if len(specialties) > 0 {
		specBlock = "REQUESTED SPECIALTIES (apply only those listed):\n"
		specBlock += fmt.Sprintf("- %s\n\n", strings.Join(specialties, ", "))
	}

	supplyChainBlock := ""
	supplyChainBlock = fmt.Sprintf(`
EXTRACTED SUPPLY CHAINS (JSON):
%s

`, supplyChainsJSON)

	graphContextBlock := ""
	if strings.TrimSpace(graphContext) != "" {
		graphContextBlock = fmt.Sprintf(`
GRAPH SKELETON CONTEXT (from existing system graph; may be incomplete):
%s

`, strings.TrimSpace(graphContext))
	}
	businessRulesBlock := taxonomy.BusinessTagRulesPrompt() + "\n\n"
	// Object detail: from entity_semantics (unified per type).
	profileBlock := entity_semantics.ResearchBlockForPath(extractedData.CanonicalPath, extractedData.Type, extractedData.CommunityType) + "\n\n"
	objectTypeTaskBlock := buildObjectTypeTaskSpecializationBlockWithHints(extractedData.CanonicalPath, extractedData.Type, extractedData.CommunityType, extractedData.Qualifiers, extractedData.Identifiers, extractedData.SpherePaths) + "\n"
	relevanceGateBlock := buildCriticRelevanceGate(extractedData)
	nameRulesBlock := strings.ReplaceAll(naming.PromptEntityNameNormalizationRules(), "\n", "\n        ")

	canonicalRules := taxonomy.DefaultCanonicalRules()
	canonicalValidationBlock := fmt.Sprintf(`  MANDATORY - Canonical path validation (for fragment AND relations):
    - For the main fragment and EACH relation targetCanonicalPath:
      - If path is /org/... ensure it is %s; otherwise add a question:
        - "Confirm official legal name, jurisdiction (ISO2 or regional token), and canonical org path for <entity>. REQUIRED FORMAT: %s where iso2 is %s; slug is hyphenated base name ONLY — do NOT include legal form (Inc, Ltd, LLC, etc.) in slug."
	      - If path is /gov/... ensure it is %s; otherwise add a question:
	        - "Which country/jurisdiction does this government body belong to? REQUIRED FORMAT: %s with iso2 %s. Example: %s."
	      - Shared naming constraints (for fragment + relations):
	        %s
	      - If path is /product/... ensure it is %s; otherwise add a question:
	        - "Resolve canonical product taxonomy for <product>. REQUIRED FORMAT: %s. category∈{%s}; type∈{%s}; stage∈{%s}. For services, use category=service and map service domain/subdomain into {type}/{stage}. Example: %s."
      - If path is /loc/... ensure it is %s; otherwise add a question:
        - "Resolve canonical GEO path for <location>. REQUIRED FORMAT: %s where cc is %s, admin1 is GeoNames admin code or 'na', city is hyphenated English name. Example: %s."
      - If path is /program/... ensure it is /program/{jurisdictionSlug}/{programSlug} when jurisdiction is known:
        - "Confirm program jurisdiction and canonicalPath. REQUIRED FORMAT: /program/{jurisdiction}/{slug} where jurisdiction is %s and always goes in the 2nd segment."
      - If path is missing segments (e.g., /org/<slug> or /product/<slug> with no taxonomy), treat it as non-canonical and add the appropriate question.
`,
		canonicalRules.Org.Format,
		canonicalRules.Org.Format,
		taxonomy.ISO2GeoDescriptionForPrompts(),
		canonicalRules.Gov.Format,
		canonicalRules.Gov.Format,
		taxonomy.ISO2GeoDescriptionForPrompts(),
		canonicalRules.Gov.Example,
		nameRulesBlock,
		canonicalRules.Product.Format,
		canonicalRules.Product.Format,
		strings.Join(canonicalRules.Product.Categories, ", "),
		strings.Join(canonicalRules.Product.Types, ", "),
		strings.Join(canonicalRules.Product.Stages, ", "),
		canonicalRules.Product.Example,
		canonicalRules.Geo.Format,
		canonicalRules.Geo.Format,
		taxonomy.ISO2GeoDescriptionForPrompts(),
		canonicalRules.Geo.Example,
		taxonomy.ISO2GeoDescriptionForPrompts(),
	)

	return fmt.Sprintf(`You are a validation agent (Critic) for a risk analysis system. Your task is to verify that extracted data matches the source content and assess the completeness of the knowledge graph.

EXTRACTED DATA (JSON):
%s

EXTRACTED RELATIONS (JSON):
%s
%s
%s
SOURCE CONTENT:
%s

%s
%s
%s
%s
%s
TASK: Compare the extracted data with the source content and identify any conflicts, inconsistencies, or missing information. Pay close attention to missing relationships.

	Return a JSON object with:
	- hasConflicts: boolean indicating if conflicts were found
	- conflicts: array of conflict objects (if any) with fields: field, extractedValue, sourceValue, reason, severity
	- confidence: float (0..1) indicating confidence in the validation
	- isValid: boolean indicating if data is valid overall
	- missingInfo: array of strings (questions). Identify CRITICAL missing information based on the entity type (e.g., missing location for a factory, missing capacity metrics). Formulate each item as a specific search query to find this data.

	A conflict should be reported if:
	1. Extracted value contradicts information in source
	2. Extracted value is not supported by source
	3. Metrics are outside valid ranges or inconsistent

	Missing Info logic:
	- If important fields (like location, key metrics for the sector) are empty or unknown, add a specific question to 'missingInfo'.
	- If the entity is an Organization or Facility, and NO supply chain relations (suppliers, customers, competitors) were found, you MUST add a question to find them. Example: "Who are the key suppliers for Tesla?" or "What are the major competitors of this entity?"
	- If DEPENDS_ON relations exist with facet=logistics|supply|infrastructure|distribution but transmission qualifiers are missing, you MUST add targeted questions:
	  - "What share of flow depends on this route/channel (throughput_share 0..1)?"
	  - "How constrained is capacity (capacity_constraint 0..1)?"
	  - "What is reroute penalty and reroute latency/cost delta (reroute_penalty, reroute_latency_delta_hours, reroute_cost_delta_pct)?"
	  - Also require transmission-contract completeness evidence: explicitly mark whether available data is complete, partial, or missing.
	- RELATION SEMANTICS (CRITICAL):
	  - CONTROLS: authority without clear equity ownership (state control, voting rights, contractual control). Do NOT use for formal regulation (REGULATES) or ownership (OWNS).
	  - FOUNDED: historical origin event; does NOT imply ongoing ownership or control.
	  - INFLUENCES: soft power, pressure, or indirect impact; do NOT use for formal authority (REGULATES) or ownership/control (OWNS/CONTROLS).
	  - REGULATES: domestic jurisdictional oversight only (regulator oversees entities within its own jurisdiction). If a government body targets a FOREIGN entity (different country in canonical path), this is NOT REGULATES — it is CONTROLS_ACCESS (sanctions, trade restrictions) or IMPOSES_CONDITION. Flag cross-jurisdiction REGULATES as missingInfo.
	- If the entity is an Organization, Facility, or Government body (/org/, /fac/, /gov/):
	- If no assurance/audit is present (ASSURES), add a question:
	    - "Who provides external assurance to <org>? Auditor, certifier, or regulator. Use ASSURES(assurance_type:audit|certification|regulatory|inspection). Provide assurer and source."
	  
	  - *** CRITICAL *** If marketSegments is empty or missing, you MUST add this question:
	    - "What industry/sector does <entity> operate in? Return as marketSegments array with canonical paths:
	      Format: /segment/{domain}/{vertical}/{geo}
	      Domains: finance, technology, industrial, consumer, healthcare, energy, materials, telecom, real-estate, public-sector, professional-services
	      Verticals: banking, software, automotive, oil-gas, government, consulting, etc.
	      Geo: ISO2 lowercase (us, de, cn) or regional bucket (global, mena, emea, apac, latam, na, gcc, eu, asean, nordics, benelux, anz, cis, ssa, usmca, nafta)
	      Examples: /segment/technology/semiconductors/tw, /segment/industrial/agriculture/kz, /segment/public-sector/government/ru, /segment/finance/banking/us
	      For government bodies use domain=public-sector, vertical=government or specific function (defense, treasury, regulation)."
	  
	  - *** CRITICAL *** If orgSize is empty, missing, or "na", you MUST add this question:
	    - "What is the size of <entity>? Find employee count, revenue, or market cap to determine orgSize:
	      - sme: <500 employees or <$100M revenue (small regional player)
	      - le: 500-5000 employees or $100M-$1B revenue (national player)
	      - enterprise: 5000-50000 employees or $1B-$10B revenue (major multinational)
	      - mega: >50000 employees or >$10B revenue (global giant)
	      For government: federal ministries of G20 countries = enterprise/mega, regional agencies = le/enterprise, municipal = sme/le.
	      Search for annual reports, Wikipedia infobox, LinkedIn company page, or news about company size."
	  - If fragment.tags has no /biz/... business tags, add this question:
	    - "What business role profile best describes <entity>? Return 3-6 tags in fragment.tags using:
	      /biz/function/{slug}, /biz/economic-role/{slug}, /biz/risk-exposure/{slug}, /biz/value-pool/{slug}, /biz/replaceability/{low|medium|high|variable}."
	  
	  - If this looks like a government body (ministry/agency/regulator) but canonicalPath is not /gov/{iso2}/{slug}, add a question:
	    - "Which country/jurisdiction does this government body belong to? Use canonicalPath /gov/{iso2|regional}/{slug} where regional tokens (gcc, eu, asean) go in the geo slot (2nd segment). Use the best evidence-backed jurisdiction instead of defaulting to /gov/na."
	  - If canonicalPath uses /org/na/{slug}, add a question:
	    - "What is the country of origin / primary jurisdiction (ISO2 or regional token) for <org>? Use the entity's legalName/shortName/label to disambiguate and update canonicalPath to /org/{iso2|regional}/{slug} (base name only, do NOT include legal form). Use the best evidence-backed jurisdiction instead of defaulting to /org/na."
	  - If this is a facility and canonicalPath uses /fac/na/{slug} while the facility name, homeLocation.name, or source text contains a clear place clue, add a question:
	    - "What is the correct country/jurisdiction (ISO2) for <facility>? Resolve the facility/site location from the place clue and update canonicalPath to /fac/{iso2}/{slug}. Do NOT keep /fac/na/... when the place is inferable."
	  - If homeLocation.name implies one country but homeLocation.iso3166_1 implies another, add a question:
	    - "Resolve the contradictory facility geography for <facility>. Confirm the site/city and the correct ISO2 country code. Keep homeLocation.name but correct or remove the conflicting iso3166_1."
	
%s
  MANDATORY - Community completeness (for COMMUNITY or /community/*):
    - If the entity is a COMMUNITY (fragment.type == COMMUNITY OR canonicalPath starts with /community/):
      - Community geo MUST be in the 2nd segment: /community/{geo}/{type}/{slug} where geo is ISO2 or regional token (gcc, eu, asean). Never put geo at the end.
      - If communityType is empty/missing, add:
        - "What type of community is <entity>? Return one of trade|activist|professional|criminal|religious|ethnic|interest|dynasty|tribe|representatives|industry-association|key-community."
      - If communityPlatforms is empty AND there are no OPERATES_VIA edges, add:
        - "Which specific communication channels does <community> use? Return /media/{platform}/{slug} and OPERATES_VIA edges."
      - If affiliatedOrgPath is empty AND there are no AFFILIATED_WITH edges, add:
        - "Is <community> affiliated with any formal organizations? Return /org/... paths and AFFILIATED_WITH edges."
      - If there is a dependency on a platform by name (e.g., Telegram/WhatsApp) but no specific channels, add:
        - "Which specific Telegram/WhatsApp/Signal channels or groups does <community> use? Provide /media/... paths."
      - DYNASTY/TRIBE HIERARCHY (for communityType == dynasty or tribe):
        - If communityType is "dynasty" or "tribe" and there are no HAS_SUBGROUP edges:
          - "What are the main branches/sub-clans of <community>? Return /community/{geo}/{type}/{slug} paths and HAS_SUBGROUP edges (broader → narrower)."
          - For dynasties: "What are the key branches of <dynasty>? (e.g., Sudairi Seven for House of Saud)"
          - For tribes: "What are the major sub-tribes/clans of <tribe>? (e.g., Durrani/Ghilzai for Pashtun)"
        - If there are HAS_SUBGROUP children but no parent, check if this is a root community or add:
          - "Is <community> part of a larger dynasty/tribe? Return parent path and HAS_SUBGROUP edge (parent → this)."

  MANDATORY - PERSON cultural affiliation (dynasty/tribe when relevant):
    - If the entity is a PERSON and there are cultural signals in the source text OR strong LLM prior knowledge indicates cultural relevance
      (e.g., titles, clan names, tribal affiliation, royal lineage, dynasty/house membership, "tribe", "clan", "lineage", "house of", "son of"),
      add a missingInfo item to identify dynasty/tribe membership:
      - "Is <person> affiliated with a dynasty or tribe? If yes, return /community/{geo}/dynasty|tribe/{slug} and MEMBER_OF edges."
    - Only ask this when culturally relevant and plausible for the person/context (avoid forcing for modern corporate contexts).

	MANDATORY - Lifecycle stage (always required for FACILITY, DEPOSIT, extractive ORGANIZATION):
	  - If lifecycleStage is empty/missing for a /fac/, /deposit/, or extractive /org/ (mining/O&G sector), add:
	    - "What is the operational status of <entity>? Is it: concept/planning/permitting/financing/construction/commissioning/operational/expansion/suspended/decommissioning/closed?"
	  - If lifecycleStage is 'construction' or 'planning' but expectedStartDate is missing, add:
	    - "When is <entity> expected to become operational? Provide planned start date."
	
	MANDATORY - Products/services for operating organizations (always, not just for products_and_services specialty):
	  - For any /org/ (ORGANIZATION), first determine if it's a HOLDING company or OPERATING company:
	    - HOLDING indicators: has OWNS edges to other /org/, industryPaths indicate finance/investment or the labels contain "holding", "investment", "group"
	    - OPERATING company: produces products/services directly
	  - For OPERATING /org/: if there are NO PRODUCES relations with /product/... targets, add:
	    - "What products does <org> manufacture/produce? Return as /product/{category}/{type}/{stage}/{slug} canonical paths."
	    - "What services does <org> provide? Return as /product/{category}/{type}/{stage}/{slug} with category=service and service domain/subdomain in {type}/{stage}."
	  - For HOLDING /org/: first require subsidiaries, then products on subsidiaries:
	    - If no OWNS edges to other /org/: "What operating subsidiaries does <org> own? Include ownership percentage."
	    - Flag subsidiaries for separate scouting with products/services requirement.
	
	MANDATORY - Products/inputs for FACILITY (always, not just for products_and_services specialty):
	  - For any /fac/ (FACILITY): if there are NO PRODUCES relations, add:
	    - "What products does this facility produce? Return as /product/... canonical paths."
	  - If there are NO input-product relations (SUPPLIES_TO product→org), add:
	    - "What inputs/materials does this facility consume? Return as /product/... canonical paths."
	
	MANDATORY - Extractive industries (Mining and O&G):
	  - If industryPaths indicate mining, hydrocarbons, or another extractive branch:
	    - If no /deposit/ entities are linked (EXTRACTS_FROM or OPERATES_IN), add:
	      - "What deposits/fields/basins does <org/fac> extract from? Return as /deposit/{resource}/{deposit-slug}."
	    - For each product in PRODUCES: if no reserves metrics, add:
	      - "What are the proven/probable reserves for <product> at <entity>? Provide units (metric_tons, moz, bbl, tcf)."
	    - For each product in PRODUCES: if no production metrics, add:
	      - "What is the annual production of <product> at <entity>? Provide units (metric_tons_per_year, oz_per_year, bpd)."
	
	MANDATORY - Edge metrics (for key relationship types):
	  - For OWNS edges: if no ownership_percent metric, add:
	    - "What is the ownership percentage for <owner> in <target>?"
	  - For INVESTS_IN edges: if no round qualifier, add:
	    - "What investment round was this (seed/series_a/series_b/etc.)?"
	  - For INVESTS_IN edges: if no investment_amount metric, consider adding (desired, not mandatory):
	    - "What was the investment amount?"
	
	MANDATORY - Temporal validity (for historical relationships):
	  - If source text contains "former", "ex-", "бывший", "previously", "past", or indicates a relationship ended:
	    - If ValidTo is not set on the edge, add:
	      - "When did <person/org> leave/exit <target>? Provide end date for the relationship."

	MANDATORY - Market sizing & concentration (always required for MARKET):
	  - If the entity is a MARKET (fragment.type == MARKET OR canonicalPath starts with /market/):
	    - If marketDomain is empty/missing, add:
	      - "What is the marketDomain for <market>? Return one of finance|technology|industrial|consumer|healthcare|energy|materials|telecom|real-estate with sources."
	    - If marketVertical is empty/missing, add:
	      - "What is the marketVertical for <market>? Return a specific vertical (e.g., banking, semiconductors, ev-batteries) with sources."
	    - If there are no PARTICIPATES_IN / DOMINATES relations to real players, add:
	      - "Who are the key players in <market> (top companies) and who dominates? Provide names, market shares if available, and sources."
	    - If state is missing market sizing metrics, you MUST add questions:
	      - "What is the TAM (total addressable market) in USD for <market> (latest year)? Provide value and sources. Store as /metric/market/tam_usd."
	      - "What is the market CAGR percent for <market> (provide period, e.g., 2024-2029)? Provide percent, period, and sources. Store as /metric/market/cagr_percent."
	    - If state is missing concentration metrics, add questions:
	      - "What is the CR4 (top-4 market share percent) for <market>? Provide value and sources. Store as /metric/market/cr4_percent."
	      - "What is the HHI (0..10000) for <market>? Provide value and sources. Store as /metric/market/hhi."

	MANDATORY - Market/Gov skeleton completeness (use GRAPH SKELETON CONTEXT when provided):
	  - If GRAPH SKELETON CONTEXT.kind == "market_skeleton":
	    - Your goal is to ensure the market is CONNECTED into a regional/global hierarchy (skeleton), not just populated with players.
	    - If parentEdges is empty AND expectedParentPath is present:
	      - Add a missingInfo item to find/confirm the correct parent market and create HAS_MARKET_SEGMENT edge parent -> this.
	      - Always use segment-first paths: /market/{geo}/{domain}/{vertical}. Do NOT emit legacy for NEW markets.
	    - If childEdges is empty:
	      - Add a missingInfo item: "What are the key immediate sub-markets of <market> in this geography? Return 5-10 /market/... canonical paths and HAS_MARKET_SEGMENT edges (broader -> narrower; child path = parent path + one segment)."
	    - If nearbyPaths suggests many related markets exist but this node has no parent/children, add a missingInfo item:
	      - "How should <market> be placed within the existing market skeleton for <geo>? Use nearbyPaths as candidates; add HAS_MARKET_SEGMENT edges."
	    - If the market path appears legacy (/market/{geo}/{slug}), add a missingInfo item:
	      - "Map legacy market <market> to segment-first /market/{geo}/{domain}/{vertical} (or propose segment dictionary synonyms/parents) and create bridging edges."
	  - If GRAPH SKELETON CONTEXT.kind == "gov_skeleton":
	    - Your goal is to ensure the government body is CONNECTED into an administrative hierarchy (skeleton).
	    - If parentEdges is empty AND parentGovPath is empty:
	      - Add a missingInfo item: "Which superior government body (ministry/department/government) administers <gov>? Return parentGovPath and ADMINISTERS edge superior -> subordinate."
	    - If childEdges is empty AND govType indicates a higher-level body (government|ministry|department):
	      - Add a missingInfo item: "What are the key subordinate agencies/authorities administered by <gov>? Return 5-10 /gov/... paths and ADMINISTERS edges."
	    - If nearbyPaths suggests many gov bodies exist in the jurisdiction but this node has no parent/children, add a missingInfo item:
	      - "Place <gov> into the existing administrative skeleton for <geoIso2> using nearbyPaths as candidates; add ADMINISTERS edges."
	
	- If "management" specialty is requested for an Organization/Facility, you MUST ensure leadership/board/founder coverage:
	  - If there are no PERSON relations relevant to leadership (MANAGES / EMPLOYED_BY / BOARD_MEMBER_OF / FOUNDED), add specific questions like:
	    - "Who is the CEO/CFO/COO/CTO/CIO/CMO/CHRO of <org>?" and "Who are the board members/chair of <org>?" and "Who founded <org>?"
	  - NOTE: PERSON relations may appear inverted due to extraction being anchored on the organization; still treat them as valid signals for completeness.
	  - If "products_and_services" specialty is requested (ENHANCED requirements beyond the baseline):
	  - For ORGANIZATION/FACILITY: in addition to baseline PRODUCES/SUPPLIES_TO, also check for:
	    - TRANSFORMS_INTO / ASSEMBLES_INTO / USED_IN relations for products (value chain + direct assembly + functional input)
	    - Capacity/volume metrics on edges
	    - Add questions for missing transformations/assembly links and metrics.
	  - For PRODUCT: if there are no product-to-product transition relations (TRANSFORMS_INTO / ASSEMBLES_INTO / USED_IN), add a question:
	    - "What is this product produced from, what does it transform into, what does it assemble into, and what is it used in? Provide upstream/downstream products."
	  - TRANSFORMS_INTO DIRECTION INVARIANT: source MUST be upstream (raw/earlier-stage), target MUST be downstream (refined/later-stage).
	    Stage order: extraction < refining < smelting < processing < manufacturing < assembly.
	    If any TRANSFORMS_INTO edge points from a later-stage product to an earlier-stage product, flag it as a direction error.
	  - ASSEMBLES_INTO DIRECTION INVARIANT: source MUST be component/subassembly, target MUST be the downstream assembled product.
	- If "supply_chain" specialty is requested:
	  - Validate that extracted supply chains are COMPLETE and COHERENT:
	    - Nodes must alternate: actor (org/fac) → product → actor → product → ...
	    - Each chain should have at least 3 nodes (actor → product → actor)
	    - Products should have valid /product/... canonical paths
	  - If chains are incomplete or missing, add specific questions (org→fac→product→fac→org model):
	    - "From org: find child orgs (subsidiaries, controlled, partners) and their facilities. Format: JSON."
	    - "From fac: find products THIS facility produces directly. Not products of related/parent companies. Format: JSON."
	    - "From product: find org-recipients (orgs that receive this product). Format: JSON."
	  - Check for GAPS: if an actor produces a product but no org-recipient is found, flag it.
		- If "equity" specialty is requested:
		  - Ensure ownership/control relations are present (OWNS / SUBSIDIARY_OF / CONTROLS / INVESTS_IN).
		  - If none are found for an ORGANIZATION, add questions like:
		    - "Who owns <org> (shareholders/parent company/UBO)? Return OWNS with owner as SOURCE and <org> as TARGET (use explicit sourceCanonicalPath/sourceType/sourceName when owner is not current fragment)." and "What subsidiaries does <org> own/control?"
	- If the entity is a PERSON:
	  - If no RELATIVE_OF relations are present, add a question:
	    - "Who are the direct public relatives (spouse/children/parents/siblings) of <person>? Return RELATIVE_OF links with sources."
	  - If no RELATED_TO relations are present, add a question:
	    - "Who are the publicly known close associates of <person> (business partners/friends/classmates)? Return RELATED_TO links with evidence."
	- If relations seem sparse or inconsistent with the entity's scale, flag it.

	MANDATORY - Strategic layer completeness (for DECISION_POINT and MECHANISM):
	  - If the entity is a DECISION_POINT (fragment.type == DECISION_POINT OR canonicalPath starts with /decision/):
	    - DECISION_POINT path MUST be /decision/{domain-slug}/{decision-slug}. If malformed, add conflict.
	    - If no CONTROLS_ACCESS edges exist (this decision point controlling something), add:
	      - "What does <decision_point> control or block access to? Return CONTROLS_ACCESS edges with targetCanonicalPath = controlled entity."
	    - If no USES_MECHANISM edges exist (linking to a mechanism), add:
	      - "What mechanism does <decision_point> use to exercise control? Return USES_MECHANISM edge with targetCanonicalPath=/mechanism/{category}/{slug}."
	    - If no GATEKEEPS_DECISION edges exist, add:
	      - "Who gatekeeps <decision_point>? Return GATEKEEPS_DECISION edge with EXPLICIT SOURCE: sourceCanonicalPath=/org/... or /gov/... or /community/... (the gatekeeper), sourceType=ORGANIZATION or GOV, sourceName=gatekeeper name. Target is THIS DECISION POINT. CRITICAL: The gatekeeper MUST be the source, NOT the decision point."
	    - If no evidence/provenance is linked, add:
	      - "What is the evidence for <decision_point> existence? Return specific regulatory actions, documents, or events with dates."
	    - Decision points without any connected edges are HIGH SEVERITY conflicts.

	  - If the entity is a MECHANISM (fragment.type == MECHANISM OR canonicalPath starts with /mechanism/):
	    - MECHANISM path MUST be /mechanism/{category}/{slug} where category is one of: regulatory, economic, technological, contractual, political.
	    - If category is not valid, add conflict with HIGH severity.
	    - Mechanism slugs MUST be specific about the object ("license what? / controls what?").
	      - If slug is generic (licensing, regulation, oversight, supervision, compliance, monitoring, enforcement) without object, add:
	      - "Specify what <mechanism> applies to (license what / regulate what) and update to a concrete mechanism slug (e.g., %s)."
	    - If no USES_MECHANISM edges exist (agents using this mechanism), add:
	      - "What agents/organizations use <mechanism>? Return USES_MECHANISM edges with EXPLICIT SOURCE: sourceCanonicalPath=/org/... or /gov/... or /decision/... (the user), sourceType, sourceName. Target is THIS MECHANISM."
	    - If no APPLIES_TO edges exist (what the mechanism affects), add:
	      - "What does <mechanism> apply to? Return APPLIES_TO edges with targetCanonicalPath = affected entity. Source is THIS MECHANISM."
	    - Orphan mechanisms (no incoming or outgoing edges) are HIGH SEVERITY conflicts.

	  - For strategic relations (CONTROLS_ACCESS, IMPOSES_CONDITION, USES_MECHANISM, APPLIES_TO, GATEKEEPS_DECISION):
	    - CONTROLS_ACCESS: source MUST be DECISION_POINT or ORGANIZATION/GOV/COMMUNITY/PROGRAM; target MUST be ORGANIZATION/FACILITY/PRODUCT/MARKET/PROGRAM/COMMUNITY.
	      - Qualifiers MUST include 'type' (block|enable|delay|monitor). Missing qualifiers = MEDIUM severity conflict.
	    - IMPOSES_CONDITION: qualifiers MUST include 'type' (cost|compliance|technical) and 'impact'.
	    - GATEKEEPS_DECISION: source MUST be ORGANIZATION/GOV/COMMUNITY/PROGRAM/PERSON; target MUST be DECISION_POINT.
	      - When asking for gatekeeper, ALWAYS request EXPLICIT SOURCE (sourceCanonicalPath) in missingInfo.
	    - USES_MECHANISM: source MUST be DECISION_POINT/ORGANIZATION/GOV/COMMUNITY/PROGRAM; target MUST be MECHANISM.
	      - When asking for mechanism user (from MECHANISM fragment), ALWAYS request EXPLICIT SOURCE in missingInfo.
	    - APPLIES_TO: source MUST be MECHANISM; target MUST be ORGANIZATION/FACILITY/PRODUCT/MARKET/COMMUNITY/PROGRAM.

Return ONLY valid JSON. Do not include markdown code fences.`, string(extractedJSON), string(relationsJSON), supplyChainBlock, graphContextBlock, sourceContent, specBlock, businessRulesBlock, profileBlock, objectTypeTaskBlock, canonicalValidationBlock, taxonomy.MechanismSlugExamplesString(), relevanceGateBlock)
}

func buildCriticRelevanceGate(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	ft := fragment.Type

	switch {
	case ft == models.FragmentTypePerson || strings.HasPrefix(cp, "/person/"):
		return `CRITICAL RELEVANCE GATE (PERSON):
- Focus ONLY on person-relevant completeness: identity, affiliations, roles, relatives, associates, and person-specific relationship evidence.
- Do NOT request org/fac/market operational packages for PERSON:
  no orgSize, marketSegments, TAM/CAGR/CR4/HHI, supply-chain suppliers/customers, facility lifecycle, audit-assurance, products/services catalogs, ownership cap-table requests.
- If uncertain, prefer fewer high-quality PERSON questions over broad business checklists.
`
	case ft == models.FragmentTypeMarket || strings.HasPrefix(cp, "/market/"):
		return `CRITICAL RELEVANCE GATE (MARKET):
- Focus on market hierarchy, participants, sizing and concentration metrics.
- Avoid person-family/biography requests unless explicitly evidenced as relevant to this market.
`
	case ft == models.FragmentTypeCommunity || strings.HasPrefix(cp, "/community/"):
		return `CRITICAL RELEVANCE GATE (COMMUNITY):
- Focus on community type, channels, affiliations, subgroup hierarchy, and influence links.
- Avoid generic corporate audit/market-sizing checklists unless explicitly grounded.
`
	default:
		return `CRITICAL RELEVANCE GATE:
- Apply only checks and missingInfo that are directly relevant to the entity type/canonicalPath.
- Do NOT emit broad mandatory checklists that are unrelated to the current entity.
`
	}
}

func enforceValidationRelevance(fragment *models.HorizonFragment, out *ValidationResult) *ValidationResult {
	if out == nil || fragment == nil {
		return out
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	ft := fragment.Type
	isPerson := ft == models.FragmentTypePerson || strings.HasPrefix(cp, "/person/")
	if !isPerson {
		return out
	}

	isIrrelevantForPerson := func(text string) bool {
		t := strings.ToLower(strings.TrimSpace(text))
		if t == "" {
			return false
		}
		// PERSON guardrail: suppress known non-person business/market/supply-chain checklists.
		return strings.Contains(t, "supplier") ||
			strings.Contains(t, "supply chain") ||
			strings.Contains(t, "upstream") ||
			strings.Contains(t, "downstream") ||
			strings.Contains(t, "orgsize") ||
			strings.Contains(t, "marketsegments") ||
			strings.Contains(t, "tam") ||
			strings.Contains(t, "cagr") ||
			strings.Contains(t, "cr4") ||
			strings.Contains(t, "hhi") ||
			strings.Contains(t, "audit") ||
			strings.Contains(t, "assurance") ||
			strings.Contains(t, "auditor") ||
			strings.Contains(t, "subsidiar") ||
			strings.Contains(t, "ownership percentage") ||
			strings.Contains(t, "operational status") ||
			strings.Contains(t, "facility") ||
			strings.Contains(t, "deposit") ||
			strings.Contains(t, "products and services") ||
			strings.Contains(t, "produce") ||
			strings.Contains(t, "consume")
	}

	if len(out.MissingInfo) > 0 {
		filteredMissing := make([]string, 0, len(out.MissingInfo))
		for _, q := range out.MissingInfo {
			if isIrrelevantForPerson(q) {
				continue
			}
			filteredMissing = append(filteredMissing, q)
		}
		out.MissingInfo = filteredMissing
	}

	if len(out.Conflicts) > 0 {
		filteredConflicts := make([]ValidationConflict, 0, len(out.Conflicts))
		for _, c := range out.Conflicts {
			blob := strings.TrimSpace(
				strings.Join([]string{
					c.Field,
					c.Reason,
					fmt.Sprint(c.ExtractedValue),
					fmt.Sprint(c.SourceValue),
				}, " "),
			)
			if isIrrelevantForPerson(blob) {
				continue
			}
			filteredConflicts = append(filteredConflicts, c)
		}
		out.Conflicts = filteredConflicts
		out.HasConflicts = len(filteredConflicts) > 0
	}

	return out
}

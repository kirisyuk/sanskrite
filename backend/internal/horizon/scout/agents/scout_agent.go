package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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
func (a *ScoutAgent) ExtractFragmentData(ctx context.Context, content string, targetCanonicalPath string, entityPath string, sectorPath string, specialties []string, chainContext string, domainModes []string) (*ScoutResult, error) {
	result, _, err := a.ExtractFragmentDataWithGrounding(ctx, content, targetCanonicalPath, entityPath, sectorPath, specialties, chainContext, domainModes)
	return result, err
}

// ExtractFragmentDataWithGrounding returns extracted result plus grounding evidence from full AI task result.
func (a *ScoutAgent) ExtractFragmentDataWithGrounding(ctx context.Context, content string, targetCanonicalPath string, entityPath string, sectorPath string, specialties []string, chainContext string, domainModes []string) (*ScoutResult, *ai.GroundingEvidence, error) {
	prompt := a.buildExtractionPrompt(content, targetCanonicalPath, entityPath, sectorPath, specialties, chainContext, domainModes)

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
	if scoutResult.Fragment.CanonicalPath == "" {
		scoutResult.Fragment.CanonicalPath = targetCanonicalPath
	}

	// Валидация Fragment (повторная, если нужно)
	if scoutResult.Fragment.CanonicalPath == "" {
		var fragment models.HorizonFragment
		if err := json.Unmarshal(jsonData, &fragment); err == nil && fragment.CanonicalPath != "" {
			scoutResult.Fragment = fragment
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
	normalizeDiscoveredRelationsNames(scoutResult.Relations)

	// Ensure Type is set
	if scoutResult.Fragment.Type == "" {
		// Infer type from canonicalPath (stable namespaces) or default to Organization.
		switch {
		case strings.Contains(targetCanonicalPath, "/domain/"):
			scoutResult.Fragment.Type = models.FragmentTypeDomain
		case strings.Contains(targetCanonicalPath, "/media/"):
			scoutResult.Fragment.Type = models.FragmentTypeMediaChannel
		case strings.Contains(targetCanonicalPath, "/person/"):
			scoutResult.Fragment.Type = models.FragmentTypePerson
		case strings.Contains(targetCanonicalPath, "/org/"):
			scoutResult.Fragment.Type = models.FragmentTypeOrganization
		case strings.Contains(targetCanonicalPath, "/gov/"):
			scoutResult.Fragment.Type = models.FragmentTypeOrganization
		case strings.Contains(targetCanonicalPath, "/product/"):
			scoutResult.Fragment.Type = models.FragmentTypeProduct
		case strings.Contains(targetCanonicalPath, "/program/"):
			scoutResult.Fragment.Type = models.FragmentTypeProgram
		case strings.Contains(targetCanonicalPath, "/market/"):
			scoutResult.Fragment.Type = models.FragmentTypeMarket
		case strings.Contains(targetCanonicalPath, "/fac/"):
			scoutResult.Fragment.Type = models.FragmentTypeFacility
		case strings.Contains(targetCanonicalPath, "/deposit/"):
			scoutResult.Fragment.Type = models.FragmentTypeDeposit
		case strings.Contains(targetCanonicalPath, "/loc/") || strings.Contains(targetCanonicalPath, "/region/"):
			scoutResult.Fragment.Type = models.FragmentTypeGeoRegion
		case strings.Contains(targetCanonicalPath, "/event/"):
			scoutResult.Fragment.Type = models.FragmentTypeEvent
		default:
			scoutResult.Fragment.Type = models.FragmentTypeOrganization
		}
	}

	if scoutResult.Fragment.TenantID == "" {
		scoutResult.Fragment.TenantID = "global"
	}

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
	return edgesemantics.EdgeAllowlistStrings(specific)
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

const extractionGeneralRelationRulesTemplate = `   - DEPENDS_ON: source depends on target (use this for suppliers, contractors, infrastructure).
     Example: If Panasonic supplies Tesla battery cells, for Tesla (source) -> Panasonic (target) use DEPENDS_ON (NOT SUPPLIES_TO).
     For actor↔actor dependencies (ORG/FACILITY/PROGRAM/COMMUNITY/PERSON), include qualifiers.source_role=dependent and qualifiers.target_role=dependency (or supplier/provider).
   - SUPPLIES_TO: source supplies goods/services to target (use this for customers/recipients). Source MUST be ORGANIZATION or FACILITY (never DEPOSIT).
     For ORG/FACILITY -> ORG/FACILITY supply links, include qualifiers.source_role=supplier and qualifiers.target_role=customer.
   - LOCATED_IN: source is located in target (target MUST be GEO_REGION).
   - OPERATES_IN: source operates in target (source MUST be ORGANIZATION or PERSON, target MUST be GEO_REGION or FACILITY or DEPOSIT).
   - REGULATES: source regulates target (source MUST be ORGANIZATION - only orgs can regulate).
   - OWNS: source owns target (source MUST be ORGANIZATION or PERSON - only orgs/persons can own).
     Direction is strict: owner -> owned entity.
     If evidence says THIS FRAGMENT is owned by X, keep relationType=OWNS and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = THIS FRAGMENT.
     Never output reversed OWNS (THIS FRAGMENT -> its owner).
   - SUBSIDIARY_OF: source is owned/controlled by target (target can be ORGANIZATION or PERSON; will be normalized to OWNS in graph).
   - CONTROLS: source controls target (non-equity/indirect control: voting control, state control, contractual control).
     Do NOT use CONTROLS for external audit/certification/inspection oversight; use ASSURES with qualifiers.assurance_type.
   - MANAGES: source manages target (formal authority: CEO/executive/director/operator).
   - EMPLOYED_BY: source (PERSON) is employed by target (ORGANIZATION).
   - BOARD_MEMBER_OF: source (PERSON) is a board member of target (ORGANIZATION).
   - FOUNDED: source (PERSON or ORGANIZATION) founded target (ORGANIZATION or PROGRAM or COMMUNITY).
   - INVESTS_IN: source (ORGANIZATION or PERSON) invests in target (ORGANIZATION/PROGRAM/PRODUCT).
     Direction is strict: investor -> investee.
     If THIS FRAGMENT is the investee (e.g., "invested in by X"), set explicit sourceCanonicalPath/sourceType/sourceName = X and targetCanonicalPath = THIS FRAGMENT.
   - PRODUCES: source produces target (source MUST be ORGANIZATION or FACILITY or DEPOSIT, target MUST be PRODUCT).
   - CONSUMES: source consumes target (source MUST be ORGANIZATION or FACILITY or PRODUCT, target MUST be PRODUCT).
   - TRANSFORMS_INTO: upstream PRODUCT -> downstream PRODUCT (value-chain transformation).
     Direction is strict: source MUST be the raw/earlier-stage product, target MUST be the refined/later-stage product.
     Example: copper-ore TRANSFORMS_INTO copper-concentrate (NEVER the reverse). iron-ore TRANSFORMS_INTO iron-pellet (NEVER the reverse).
   - EXTRACTS_FROM: source MUST be FACILITY, target MUST be DEPOSIT.
   - If THIS FRAGMENT is DEPOSIT and evidence says "X extracts from this deposit" or "X depends on this deposit":
     use explicit sourceCanonicalPath/sourceType/sourceName for X and keep targetCanonicalPath = THIS FRAGMENT.
     Never emit DEPOSIT -> ORGANIZATION|FACILITY via SUPPLIES_TO.
   - ASSURES: source (ORGANIZATION) provides independent assurance to target (ORGANIZATION/FACILITY/PROGRAM). Add qualifiers.assurance_type: audit | certification | regulatory | inspection.
     If THIS FRAGMENT is audited/certified/inspected by X, keep relationType=ASSURES and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = THIS FRAGMENT.
   - AUDITED_BY is legacy input only. Do NOT emit it; convert to ASSURES and preserve semantics (auditor -> audited entity).
   - PARTNERS_WITH: partnership/collaboration (symmetric; still output as source->target).
   - COMPETES_WITH: competitor relationship (direction is not important, still output as source->target).
   - INFLUENCES: source influences target (soft power; avoid for formal authority/ownership).
   - PARTICIPATES_IN: entity participates in a market (canonical direction: ORG/FACILITY/COMMUNITY/PROGRAM/PRODUCT/PERSON -> MARKET).
   - For ORGANIZATION ↔ MARKET pairs, relationType MUST be PARTICIPATES_IN (direction: ORGANIZATION -> MARKET).
     Exception: GOV entities (/gov/...) may use REGULATES, CONTROLS_ACCESS, or INFLUENCES toward MARKET targets.
     Use qualifiers.marketRole ("leader"/"dominant") + market_share metrics for leadership semantics.
   - HAS_MARKET_SEGMENT: parent market -> segment (child path = parent path + one segment).
   - ADMINISTERS: government/body administers subordinate GOV, PROGRAM, GEO_REGION, FACILITY, or DEPOSIT.
   - OPERATES_VIA: COMMUNITY/ORGANIZATION -> MEDIA_CHANNEL.
   - MEMBER_OF: PERSON -> COMMUNITY.
   - AFFILIATED_WITH: COMMUNITY/PERSON -> ORGANIZATION.
   - RELATIVE_OF, RELATED_TO: person-person relationships only.
   - RENAMED_TO, REBRANDED_TO, LEGAL_SUCCESSOR_OF, MERGED_INTO, SPUN_OFF_FROM, ACQUIRED_BY: ORG lifecycle events.
   - CONTROLS_ACCESS, IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO: strategic overlay.
   - HAS_SUBGROUP: COMMUNITY parent -> narrower community subgroup.

   ROLE-FIRST DIRECTION CONTRACT (CRITICAL for ORG<->ORG):
   - Do NOT assume source = THIS FRAGMENT for OWNS / INVESTS_IN / ASSURES / CONTROLS.
   - Determine semantic roles first, then set edge direction.
   - For ORG->ORG directed edges above, include:
     qualifiers.source_role and qualifiers.target_role (e.g., owner->owned_entity, investor->investee, assurer->assuree, controller->controlled_entity).
   - For directional actor↔actor DEPENDS_ON/SUPPLIES_TO, also include source_role/target_role (dependent->dependency, supplier->customer).
   - If semantic source is not THIS FRAGMENT, sourceCanonicalPath/sourceType/sourceName is REQUIRED.
   - If direction is unclear, omit the relation (do not guess).

   %s
   - ORGANIZATION ↔ MARKET: relationType MUST be PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES/CONTROLS_ACCESS/INFLUENCES.
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
	return fmt.Sprintf(extractionGeneralRelationRulesTemplate, edgesemantics.EdgeDirectionValidationRulesForPrompt(), entity_semantics.PromptRelationTargetTypesPipeList())
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

	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) {
		b.WriteString(`   SUPPLY CHAIN MODE:
   - DEPENDS_ON: consumer -> supplier/infrastructure dependency.
   - SUPPLIES_TO: supplier -> customer flow.
   - For actor↔actor DEPENDS_ON and ORG/FACILITY SUPPLIES_TO links, include qualifiers.source_role and qualifiers.target_role.
   - PRODUCES/CONSUMES: actor -> product.
   - TRANSFORMS_INTO: upstream raw product -> downstream refined product (NEVER reversed; e.g. ore -> concentrate -> anode -> cathode).
   - EXTRACTS_FROM: facility -> deposit.
   - LOCATED_IN/OPERATES_IN only for geo/facility anchoring.
   - PARTICIPATES_IN: ORG/FACILITY -> MARKET (commercial market participation).
   - REGULATES: GOV entity (/gov/...) -> MARKET or ORG (regulatory oversight). NEVER use DEPENDS_ON or PARTICIPATES_IN for government regulation.
   - ADMINISTERS: GOV entity (/gov/...) -> subordinate GOV/PROGRAM/FACILITY (administrative hierarchy).
   - Prefer qualifiers.facet for DEPENDS_ON (communication|infrastructure|payment|distribution|logistics|supply|labor|legitimization|enforcement|regulatory).
   - For transmission dependencies add throughput_share, capacity_constraint, reroute_penalty, reroute_latency_delta_hours, reroute_cost_delta_pct when known.
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
   - REGULATES: regulator -> regulated entity.
`)
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeMarket) {
		b.WriteString(`   MARKET MODE:
   - PARTICIPATES_IN: entity -> market.
   - ORGANIZATION ↔ MARKET: PARTICIPATES_IN (ORG -> MARKET). GOV entities may also REGULATES/CONTROLS_ACCESS/INFLUENCES market.
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
   - REGULATES/ADMINISTERS for formal public-policy authority.
   - CONTROLS_ACCESS/IMPOSES_CONDITION/USES_MECHANISM/APPLIES_TO for explicit instruments and constraints.
   - INFLUENCES for soft or second-order impact channels when formal authority is absent.
   - DEPENDS_ON/PARTICIPATES_IN/LOCATED_IN/OPERATES_IN for exposure context needed in impact modeling.
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
  - Example chain: /market/sa/finance → .../banking → .../banking/islamic-banking
  - HAS_MARKET_SEGMENT child path = parent path + one segment
- Communities: /community/{geo}/{type}/{slug}; subgroup hierarchy via HAS_SUBGROUP (broader -> narrower).
- Programs: /program/{jurisdiction}/{slug}; if jurisdiction is unknown, use global.
- Facilities: /fac/{orgSlug}/{facilitySlug}; if org is unknown, use orgSlug=na.`
}

// buildExtractionPrompt строит промпт для извлечения данных
func (a *ScoutAgent) buildExtractionPrompt(content string, targetCanonicalPath string, entityPath string, sectorPath string, specialties []string, chainContext string, domainModes []string) string {
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
	// Object detail: from entity_semantics (unified per type). Scope from domain modes above.
	typeBlock := entity_semantics.ExtractionBlockForPath(targetCanonicalPath, "", "")
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
- sectorPath MUST match provided sector path
- %s
- %s
%s
%s
- state: map of MetricValue entries
- description: short factual summary
- homeLocation: when geo is relevant, object with { name: string (city/place), iso3166_1: string (ISO3166-1 alpha2, e.g. "AE", "US") }. Required for org/facility/gov to enable geocoding.
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
- Each chain has: nodes[], confidence, reason, optional transportVia[].
- Node shape: canonicalPath (REQUIRED), name (REQUIRED), type (REQUIRED enum: ORGANIZATION|FACILITY|PRODUCT|PROGRAM|MARKET|GEO_REGION|DEPOSIT|COMMUNITY), role?
- Nodes MUST alternate actor -> product -> actor -> product.
- Include FACILITY where transformation occurs (product -> facility -> product).
- For extractive flows: org -> facility -> deposit -> product.
- transportVia should contain facility canonical paths for ports/terminals/pipelines/warehouses when known.`

	return joinPromptBlocks(
		`You are a data extraction agent (Scout) for a risk analysis system. Extract structured information about an entity and its dependencies from the provided content.`,
		fmt.Sprintf(`TARGET ENTITY:
- Canonical Path: %s
- Entity Type Path: %s
- Sector Path: %s`, targetCanonicalPath, entityPath, sectorPath),
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
		"Return ONLY valid JSON. Do not include markdown code fences.",
	)
}

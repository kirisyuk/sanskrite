package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
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
	Fragment     models.HorizonFragment  `json:"fragment"`
	Relations    []DiscoveredRelation    `json:"relations"`
	UnitProfiles []models.OrgUnitProfile `json:"unitProfiles,omitempty"`
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
	// UnitRef is a unit identifier for actor nodes when known (consumer, enterprise, core_ops, etc).
	UnitRef string `json:"unitRef,omitempty"`
	// UnitCode is a controlled unit code for actor nodes.
	// Allowed values: consumer|enterprise|core_ops|it|logistics|corporate|rnd|unclassified.
	UnitCode string `json:"unitCode,omitempty"`
	// UnitLabel is a short human-readable label for the unit.
	UnitLabel string `json:"unitLabel,omitempty"`
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
	Fragment     extractedFragment       `json:"fragment"`
	Relations    []DiscoveredRelation    `json:"relations"`
	UnitProfiles []models.OrgUnitProfile `json:"unitProfiles,omitempty"`
	SupplyChains []SupplyChain           `json:"supplyChains,omitempty"`
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
			UnitProfiles: normalizeUnitProfiles(targetCanonicalPath, internalResult.UnitProfiles),
			SupplyChains: internalResult.SupplyChains,
		}
		// Handle name fields from internal struct (prefer LegalName)
		legal := strings.TrimSpace(internalResult.Fragment.LegalName)
		if legal == "" {
			legal = strings.TrimSpace(internalResult.Fragment.Name)
		}
		short := strings.TrimSpace(internalResult.Fragment.ShortName)

		if legal != "" {
			if scoutResult.Fragment.Labels == nil {
				scoutResult.Fragment.Labels = make(map[string]string)
			}
			scoutResult.Fragment.Labels["en"] = legal
		}
		if scoutResult.Fragment.Identifiers == nil {
			scoutResult.Fragment.Identifiers = make(map[string]string)
		}
		if legal != "" && strings.TrimSpace(scoutResult.Fragment.Identifiers["legalName"]) == "" {
			scoutResult.Fragment.Identifiers["legalName"] = legal
		}
		if short != "" && strings.TrimSpace(scoutResult.Fragment.Identifiers["shortName"]) == "" {
			scoutResult.Fragment.Identifiers["shortName"] = short
		}
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

func normalizeUnitProfiles(parentOrgRef string, in []models.OrgUnitProfile) []models.OrgUnitProfile {
	if len(in) == 0 {
		return nil
	}
	parentOrgRef = strings.TrimSpace(parentOrgRef)
	seen := make(map[string]struct{}, len(in))
	out := make([]models.OrgUnitProfile, 0, len(in))
	for idx, unit := range in {
		unit.ParentOrgRef = strings.TrimSpace(unit.ParentOrgRef)
		if unit.ParentOrgRef == "" {
			unit.ParentOrgRef = parentOrgRef
		}
		unit.UnitCode = models.NormalizeUnitCode(string(unit.UnitCode))
		unit.Label = strings.TrimSpace(unit.Label)
		if unit.Label == "" {
			unit.Label = string(unit.UnitCode)
		}
		unit.UnitID = strings.TrimSpace(unit.UnitID)
		if unit.UnitID == "" {
			unit.UnitID = normalizeUnitID(unit.Label, unit.UnitCode, idx)
		}
		if _, ok := seen[unit.UnitID]; ok {
			continue
		}
		seen[unit.UnitID] = struct{}{}
		out = append(out, unit)
	}
	return out
}

func normalizeUnitID(label string, code models.UnitCode, idx int) string {
	base := slugifyUnitToken(label)
	if base == "" {
		base = slugifyUnitToken(string(code))
	}
	if base == "" {
		base = "unit"
	}
	return fmt.Sprintf("%s-%d", base, idx+1)
}

func slugifyUnitToken(in string) string {
	s := strings.TrimSpace(strings.ToLower(in))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		isAlpha := r >= 'a' && r <= 'z'
		isNum := r >= '0' && r <= '9'
		if isAlpha || isNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

func extractionAllowedRelationTypesByDomainModes(domainModes []string) []string {
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	if len(domainModes) == 0 {
		return nil
	}
	hasSpecific := false
	for _, m := range domainModes {
		m = strings.TrimSpace(strings.ToLower(m))
		if m != "" && m != taxonomy.DomainModeGeneral {
			hasSpecific = true
			break
		}
	}
	if !hasSpecific {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 16)
	for _, m := range domainModes {
		switch strings.TrimSpace(strings.ToLower(m)) {
		case taxonomy.DomainModeSupplyChain:
			for _, t := range []string{
				string(models.DependencyTypeDependsOn),
				string(models.DependencyTypeSuppliesTo),
				string(models.DependencyTypeProduces),
				string(models.DependencyTypeConsumes),
				string(models.DependencyTypeTransformsInto),
				string(models.DependencyTypeExtractsFrom),
				string(models.DependencyTypeLocatedIn),
				string(models.DependencyTypeOperatesIn),
			} {
				if _, ok := seen[t]; ok {
					continue
				}
				seen[t] = struct{}{}
				out = append(out, t)
			}
		case taxonomy.DomainModeGovernance:
			for _, t := range []string{
				string(models.DependencyTypeOwns),
				string(models.DependencyTypeSubsidiaryOf),
				string(models.DependencyTypeControls),
				string(models.DependencyTypeManages),
				string(models.DependencyTypeInvestsIn),
				string(models.DependencyTypeBoardMemberOf),
				string(models.DependencyTypeEmployedBy),
				string(models.DependencyTypeFounded),
				string(models.DependencyTypeAdministers),
			} {
				if _, ok := seen[t]; ok {
					continue
				}
				seen[t] = struct{}{}
				out = append(out, t)
			}
		case taxonomy.DomainModeAssurance:
			for _, t := range []string{
				string(models.DependencyTypeAssures),
				string(models.DependencyTypeAuditedBy),
				string(models.DependencyTypeRegulates),
			} {
				if _, ok := seen[t]; ok {
					continue
				}
				seen[t] = struct{}{}
				out = append(out, t)
			}
		case taxonomy.DomainModeMarket:
			for _, t := range []string{
				string(models.DependencyTypeParticipatesIn),
				string(models.DependencyTypeHasMarketSegment),
			} {
				if _, ok := seen[t]; ok {
					continue
				}
				seen[t] = struct{}{}
				out = append(out, t)
			}
		case taxonomy.DomainModeStrategic:
			for _, t := range []string{
				string(models.DependencyTypeControlsAccess),
				string(models.DependencyTypeImposesCondition),
				string(models.DependencyTypeGatekeepsDecision),
				string(models.DependencyTypeUsesMechanism),
				string(models.DependencyTypeAppliesTo),
			} {
				if _, ok := seen[t]; ok {
					continue
				}
				seen[t] = struct{}{}
				out = append(out, t)
			}
		case taxonomy.DomainModePolicyImpact:
			for _, t := range []string{
				string(models.DependencyTypeRegulates),
				string(models.DependencyTypeControlsAccess),
				string(models.DependencyTypeImposesCondition),
				string(models.DependencyTypeUsesMechanism),
				string(models.DependencyTypeAppliesTo),
				string(models.DependencyTypeInfluences),
				string(models.DependencyTypeDependsOn),
				string(models.DependencyTypeParticipatesIn),
				string(models.DependencyTypeLocatedIn),
				string(models.DependencyTypeOperatesIn),
				string(models.DependencyTypeAdministers),
			} {
				if _, ok := seen[t]; ok {
					continue
				}
				seen[t] = struct{}{}
				out = append(out, t)
			}
		}
	}
	return out
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
   - SUPPLIES_TO: source supplies goods/services to target (use this for customers/recipients).
   - LOCATED_IN: source is located in target (target MUST be GEO_REGION).
   - OPERATES_IN: source operates in target (source MUST be ORGANIZATION, target MUST be GEO_REGION or FACILITY).
   - REGULATES: source regulates target (source MUST be ORGANIZATION - only orgs can regulate).
   - OWNS: source owns target (source MUST be ORGANIZATION or PERSON - only orgs/persons can own).
   - SUBSIDIARY_OF: source is owned/controlled by target (target can be ORGANIZATION or PERSON; will be normalized to OWNS in graph).
   - CONTROLS: source controls target (non-equity/indirect control: voting control, state control, contractual control).
   - MANAGES: source manages target (formal authority: CEO/executive/director/operator).
   - EMPLOYED_BY: source (PERSON) is employed by target (ORGANIZATION).
   - BOARD_MEMBER_OF: source (PERSON) is a board member of target (ORGANIZATION).
   - FOUNDED: source (PERSON or ORGANIZATION) founded target (ORGANIZATION or PROGRAM).
   - INVESTS_IN: source (ORGANIZATION or PERSON) invests in target (ORGANIZATION/PROGRAM/PRODUCT).
   - PRODUCES: source produces target (source MUST be ORGANIZATION or FACILITY or DEPOSIT, target MUST be PRODUCT).
   - CONSUMES: source consumes target (source MUST be ORGANIZATION or FACILITY or PROGRAM, target MUST be PRODUCT).
   - TRANSFORMS_INTO: PRODUCT -> PRODUCT value-chain transformation.
   - EXTRACTS_FROM: source MUST be FACILITY, target MUST be DEPOSIT.
   - AUDITED_BY: source (ORGANIZATION/FACILITY/PROGRAM) is audited by target (ORGANIZATION).
   - PARTNERS_WITH: partnership/collaboration (symmetric; still output as source->target).
   - COMPETES_WITH: competitor relationship (direction is not important, still output as source->target).
   - INFLUENCES: source influences target (soft power; avoid for formal authority/ownership).
   - PARTICIPATES_IN: entity participates in a market (canonical direction: ORG/FACILITY/COMMUNITY/PROGRAM/PRODUCT/PERSON -> MARKET).
   - For ORGANIZATION ↔ MARKET pairs, relationType MUST be PARTICIPATES_IN only (direction: ORGANIZATION -> MARKET).
     Use qualifiers.marketRole ("leader"/"dominant") + market_share metrics for leadership semantics.
   - HAS_MARKET_SEGMENT: parent market -> segment (child path = parent path + one segment).
   - ADMINISTERS: government/body administers subordinate GOV or PROGRAM.
   - OPERATES_VIA: COMMUNITY/ORGANIZATION -> MEDIA_CHANNEL.
   - MEMBER_OF: PERSON -> COMMUNITY.
   - AFFILIATED_WITH: COMMUNITY/PERSON -> ORGANIZATION.
   - RELATIVE_OF, RELATED_TO: person-person relationships only.
   - RENAMED_TO, REBRANDED_TO, LEGAL_SUCCESSOR_OF, MERGED_INTO, SPUN_OFF_FROM, ACQUIRED_BY: ORG lifecycle events.
   - CONTROLS_ACCESS, IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO: strategic overlay.
   - HAS_SUBGROUP: COMMUNITY parent -> narrower community subgroup.

   EDGE DIRECTION VALIDATION RULES (CRITICAL - edges violating these will be REJECTED or REVERSED):
   - OWNS: source MUST be ORGANIZATION or PERSON; target MUST be ORGANIZATION/FACILITY/PRODUCT/DEPOSIT/MEDIA_CHANNEL/COMMUNITY.
   - CONTROLS: source MUST be ORGANIZATION or PERSON or COMMUNITY; target MUST be ORGANIZATION or FACILITY (controller → controlled).
   - EMPLOYED_BY and BOARD_MEMBER_OF: source MUST be PERSON, target MUST be ORGANIZATION.
   - PRODUCES/CONSUMES: source MUST be ORGANIZATION/FACILITY/PROGRAM (or DEPOSIT for PRODUCES), target MUST be PRODUCT.
   - EXTRACTS_FROM: source MUST be FACILITY, target MUST be DEPOSIT.
   - PARTICIPATES_IN/HAS_MARKET_SEGMENT: target MUST be MARKET.
   - ORGANIZATION ↔ MARKET relationType MUST be PARTICIPATES_IN only (ORG -> MARKET).
   - LOCATED_IN target MUST be GEO_REGION.
   - SUPPLIES_TO source/target SHOULD be ORGANIZATION or FACILITY.
   - Strategic edges MUST use DECISION_POINT/MECHANISM compatible source/target types.
   - Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/ for any relation.

   Each object must include:
   - targetName, targetLegalName, targetShortName
   - targetType: MUST be one of: %s
   - relationType: canonical dependency type
   - reason, confidence, targetCanonicalPath

   Optional edge enrichment fields (REQUIRED WHEN AVAILABLE):
   - validFrom, validTo
   - qualifiers: edgeModality, eventId, round, contractType, integration_type, facet, transmission qualifiers
   - qualifiers (unit-first): source_unit_ref, source_unit_code, source_unit_label, target_unit_ref, target_unit_code, target_unit_label
   - metrics: ownership_percent, investment_amount, annual_volume, throughput_share, reroute_* etc.`

func extractionGeneralRelationRulesBlock() string {
	return fmt.Sprintf(extractionGeneralRelationRulesTemplate, entity_semantics.PromptRelationTargetTypesPipeList())
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
   - PRODUCES/CONSUMES: actor -> product.
   - TRANSFORMS_INTO: product -> downstream product.
   - EXTRACTS_FROM: facility -> deposit.
   - LOCATED_IN/OPERATES_IN only for geo/facility anchoring.
   - Prefer qualifiers.facet for DEPENDS_ON (communication|infrastructure|payment|distribution|logistics|supply|labor|legitimization|enforcement|regulatory).
   - For transmission dependencies add throughput_share, capacity_constraint, reroute_penalty, reroute_latency_delta_hours, reroute_cost_delta_pct when known.
`)
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeGovernance) {
		b.WriteString(`   GOVERNANCE MODE:
   - OWNS and SUBSIDIARY_OF for equity hierarchy.
   - CONTROLS for explicit non-equity control.
   - MANAGES for formal management (CEO, director, operator).
   - INVESTS_IN for minority/stake investments.
   - BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED for people governance links.
   - ADMINISTERS for GOV/ORG hierarchy into GOV/PROGRAM.
   - Add metrics.ownership_percent and/or voting_percent when evidence exists.
`)
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeAssurance) {
		b.WriteString(`   ASSURANCE MODE:
   - ASSURES: assurer -> assuree. Add qualifiers.assurance_type: audit|certification|regulatory|inspection.
   - AUDITED_BY: assuree -> auditor (legacy).
   - REGULATES: regulator -> regulated entity.
`)
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeMarket) {
		b.WriteString(`   MARKET MODE:
   - PARTICIPATES_IN: entity -> market.
   - ORGANIZATION ↔ MARKET: relationType MUST be PARTICIPATES_IN only (ORG -> MARKET).
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
   - qualifiers: edgeModality, eventId, round, contractType, integration_type, facet, transmission qualifiers
   - qualifiers (unit-first): source_unit_ref, source_unit_code, source_unit_label, target_unit_ref, target_unit_code, target_unit_label
   - metrics: ownership_percent, investment_amount, annual_volume, throughput_share, reroute_* etc.`, entity_semantics.PromptRelationTargetTypesPipeList()))
	return b.String()
}

func buildUnitIdentificationBlock(targetCanonicalPath string, domainModes []string) string {
	cp := strings.TrimSpace(strings.ToLower(targetCanonicalPath))
	needsUnits := strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/")
	if !needsUnits && !taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) && !taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeGovernance) {
		return ""
	}
	return `UNIT-FIRST PHASE ORDER (MANDATORY):
1) First identify macro units for THIS FRAGMENT and return them in "unitProfiles".
2) Then extract relations and attach unit qualifiers.
3) If uncertain, keep relation and set unit refs/codes to "unclassified" (do not drop relation).

"unitProfiles" Field (Array):
- Return 2-8 unit contours for ORG/GOV targets when evidence exists.
- Each profile shape:
  { "unitId", "parentOrgRef", "unitCode", "label", "status"?, "geo"?, "legalEntityRefs"?, "aliases"?, "confidence"?, "sources"? }
- unitCode MUST be one of: consumer | enterprise | core_ops | it | logistics | corporate | rnd | unclassified.
- parentOrgRef should point to THIS FRAGMENT canonical path.
- unitId must be stable and unique within parentOrgRef.

Relation unit qualifiers (on each relation.qualifiers):
- source_unit_ref / target_unit_ref: unit identifiers or "unclassified"
- source_unit_code / target_unit_code: same enum as unitCode
- source_unit_label / target_unit_label: short human-readable labels`
}

func extractionCanonicalNamespaceDetailsBlock() string {
	return `CANONICAL NAMESPACE DETAILS (strict addendum):
- /segment/{domain}/{vertical}/{geo} for ORG marketSegments (exactly 3 parts after /segment).
- /media/{platform}/{slug} for concrete media channels/endpoints.
- Products:
  - goods: /product/{category}/{type}/{stage}/{slug}[/{model-slug}]
  - services: /product/service/{domain}/{subdomain}/{slug}[/{offering-slug}]
- Markets:
  - prefer /market/{geo}/{domain}/{vertical}
  - HAS_MARKET_SEGMENT child path = parent path + one segment
- Communities: /community/{geo}/{type}/{slug}; subgroup hierarchy via HAS_SUBGROUP (broader -> narrower).
- Programs: prefer /program/{jurisdiction}/{slug} when jurisdiction is known.
- Facilities: prefer /fac/{orgSlug}/{facilitySlug} over flat /fac/{facilitySlug}.`
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
	unitIdentificationBlock := buildUnitIdentificationBlock(targetCanonicalPath, domainModes)
	if strings.TrimSpace(unitIdentificationBlock) != "" {
		unitIdentificationBlock += "\n\n"
	}

	canonicalRulesBlock := taxonomy.ExtendedCanonicalPathRulesPrompt()
	businessRulesBlock := taxonomy.BusinessTagRulesPrompt()
	// Object detail: from entity_semantics (unified per type). Scope from domain modes above.
	typeBlock := entity_semantics.ExtractionBlockForPath(targetCanonicalPath, "", "")
	if typeBlock != "" {
		typeBlock = typeBlock + "\n\n"
	}
	nameRulesBlock := naming.PromptEntityNameNormalizationRules()
	objectTypeTaskBlock := buildObjectTypeTaskSpecializationBlock(targetCanonicalPath, "", "")
	objectQualifierUnitTemplateContractBlock := buildObjectQualifierUnitTemplateContractBlock(targetCanonicalPath, "", "")
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
- homeLocation: location object when available
- lifecycleStage: concept|planning|permitting|financing|construction|commissioning|operational|expansion|suspended|decommissioning|closed
- expectedStartDate, actualStartDate, expectedEndDate, actualEndDate: ISO8601 when known
- identifiers: ISIN/LEI/Ticker/etc when available
- kbRefs: external KB refs when available
- metric value format:
  - value MUST be JSON number (expand "10M" -> 10000000)
  - unit lowercase (or omitted when unitless)
  - confidence in 0..1
  - observedAt ISO8601
  - provenance method + at least one sourceId`, nameRulesBlock, objectTypeTaskBlock, coreBusinessTaxonomyBlock, objectQualifierUnitTemplateContractBlock)

	unitProfilesContractBlock := `2. "unitProfiles" Field (Array of Objects):
- Optional for non-org/non-gov targets. For /org/ and /gov/ return when evidence exists.
- Shape:
  - unitId (stable id), parentOrgRef, unitCode, label
  - unitCode enum: consumer|enterprise|core_ops|it|logistics|corporate|rnd|unclassified
  - optional: status, geo, legalEntityRefs, aliases, confidence, sources`

	relationsContractBlock := fmt.Sprintf(`3. "relations" Field (Array of Objects):
- Each relation is a directed edge FROM THIS FRAGMENT (source) TO targetCanonicalPath (target), unless explicit sourceCanonicalPath is provided.
- relationType must follow semantic direction.
%s
%s
%s`, relationCatalogOverride, unitIdentificationBlock, relationRulesBlock)

	supplyChainsContractBlock := `4. "supplyChains" Field (Array of Objects):
- Each chain has: nodes[], confidence, reason, optional transportVia[].
- Node shape: canonicalPath (REQUIRED), name (REQUIRED), type (REQUIRED enum: ORGANIZATION|FACILITY|PRODUCT|PROGRAM|MARKET|GEO_REGION|DEPOSIT|COMMUNITY), role?, unitRef?, unitCode?, unitLabel?
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
- unitProfiles
- relations
- supplyChains
Use [] for unknown arrays.`,
		fragmentContractBlock,
		unitProfilesContractBlock,
		relationsContractBlock,
		supplyChainsContractBlock,
		"Return ONLY valid JSON. Do not include markdown code fences.",
	)
}

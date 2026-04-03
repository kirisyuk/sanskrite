package agents

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type relationPromptAnnotation struct {
	extra []string
}

type explicitSourcePromptSpec struct {
	edgeType models.DependencyType
	lines    []string
}

var preferredPromptEdgeOrder = []models.DependencyType{
	models.DependencyTypeDependsOn,
	models.DependencyTypeSuppliesTo,
	models.DependencyTypeLocatedIn,
	models.DependencyTypeOperatesIn,
	models.DependencyTypeRegulates,
	models.DependencyTypeOwns,
	models.DependencyTypeSubsidiaryOf,
	models.DependencyTypeControls,
	models.DependencyTypeManages,
	models.DependencyTypeAdministers,
	models.DependencyTypeEmployedBy,
	models.DependencyTypeBoardMemberOf,
	models.DependencyTypeFounded,
	models.DependencyTypeInvestsIn,
	models.DependencyTypePartnersWith,
	models.DependencyTypeProduces,
	models.DependencyTypeTransformsInto,
	models.DependencyTypeAssemblesInto,
	models.DependencyTypeUsedIn,
	models.DependencyTypeExtractsFrom,
	models.DependencyTypeAssures,
	models.DependencyTypeCompetesWith,
	models.DependencyTypeInfluences,
	models.DependencyTypeParticipatesIn,
	models.DependencyTypeTradedIn,
	models.DependencyTypeDominates,
	models.DependencyTypeHasMarketSegment,
	models.DependencyTypeOperatesVia,
	models.DependencyTypeMemberOf,
	models.DependencyTypeAffiliatedWith,
	models.DependencyTypeHasSubgroup,
	models.DependencyTypeRenamedTo,
	models.DependencyTypeRebrandedTo,
	models.DependencyTypeLegalSuccessorOf,
	models.DependencyTypeMergedInto,
	models.DependencyTypeSpunOffFrom,
	models.DependencyTypeAcquiredBy,
	models.DependencyTypeGatekeepsDecision,
	models.DependencyTypeUsesMechanism,
	models.DependencyTypeAppliesTo,
	models.DependencyTypeControlsAccess,
	models.DependencyTypeImposesCondition,
	models.DependencyTypeInGeoScope,
	models.DependencyTypeAffects,
	models.DependencyTypeRelativeOf,
	models.DependencyTypeRelatedTo,
	models.DependencyTypeAssociatedWith,
	models.DependencyTypeCorporateTransition,
	models.DependencyTypeDomainRelation,
	models.DependencyTypeWorksFor,
	models.DependencyTypeKeyPerson,
	models.DependencyTypeHasKeyPerson,
	models.DependencyTypeAuditedBy,
	models.DependencyTypeHasSubmarket,
}

var strategicPromptEdges = map[models.DependencyType]struct{}{
	models.DependencyTypeGatekeepsDecision: {},
	models.DependencyTypeUsesMechanism:     {},
	models.DependencyTypeAppliesTo:         {},
	models.DependencyTypeControlsAccess:    {},
	models.DependencyTypeImposesCondition:  {},
}

var domainPromptEdges = map[models.DependencyType]struct{}{
	models.DependencyTypeFocusesOn:      {},
	models.DependencyTypeInvolvesActor:  {},
	models.DependencyTypeAbout:          {},
	models.DependencyTypeRelatedDomain:  {},
	models.DependencyTypeDomainRelation: {},
	models.DependencyTypeBroadensTo:     {},
	models.DependencyTypeNarrowsTo:      {},
	models.DependencyTypeSurfaces:       {},
}

var legacyPromptReplacements = map[models.DependencyType]string{
	models.DependencyTypeAuditedBy:           "ASSURES",
	models.DependencyTypeHasSubmarket:        "HAS_MARKET_SEGMENT",
	models.DependencyTypeWorksFor:            "EMPLOYED_BY",
	models.DependencyTypeHasKeyPerson:        "KEY_PERSON",
	models.DependencyTypeCorporateTransition: "RENAMED_TO / REBRANDED_TO / LEGAL_SUCCESSOR_OF / MERGED_INTO / SPUN_OFF_FROM / ACQUIRED_BY",
	models.DependencyTypeDomainRelation:      "RELATED_DOMAIN / BROADENS_TO / NARROWS_TO",
}

var relationPromptAnnotations = map[models.DependencyType]relationPromptAnnotation{
	models.DependencyTypeDependsOn: {
		extra: []string{
			"Use when the source requires the target to operate or deliver outcomes.",
			"Do NOT use when the source is the supplier and the target is the customer; use SUPPLIES_TO.",
			"For actor↔actor dependencies include qualifiers.source_role=dependent and qualifiers.target_role=dependency.",
		},
	},
	models.DependencyTypeSuppliesTo: {
		extra: []string{
			"Commercial flow from supplier -> customer/recipient; not ownership or control.",
			"MARKET MUST NOT be source or target for SUPPLIES_TO.",
			"For ORG/FACILITY -> ORG/FACILITY supply links include qualifiers.source_role=supplier and qualifiers.target_role=customer.",
		},
	},
	models.DependencyTypeLocatedIn: {
		extra: []string{
			"Use for physical/geographic placement only.",
		},
	},
	models.DependencyTypeOperatesIn: {
		extra: []string{
			"Use for operational activity or facility operation, not just a registered address.",
			"MARKET MUST NOT be source for OPERATES_IN.",
		},
	},
	models.DependencyTypeRegulates: {
		extra: []string{
			`JURISDICTION RULE: REGULATES means ongoing domestic oversight within the regulator's own jurisdiction.`,
			"Do NOT use REGULATES for sanctions, trade restrictions, or extra-territorial enforcement. Use CONTROLS_ACCESS or IMPOSES_CONDITION instead.",
		},
	},
	models.DependencyTypeOwns: {
		extra: []string{
			"Use OWNS only for legal, beneficial, or controlling ownership of the target.",
			"NEVER output OWNS where targetType=PERSON.",
			"Facilities/products/deposits do not own organizations.",
			`NEVER output OWNS from ORGANIZATION to COMMUNITY if target canonicalPath is /community/{geo}/dynasty/{slug} or /community/{geo}/tribe/{slug}. Dynasties and tribes own organizations, not the reverse.`,
		},
	},
	models.DependencyTypeSubsidiaryOf: {
		extra: []string{
			"Inverse ownership form (child -> parent). Runtime normalizes it into OWNS.",
		},
	},
	models.DependencyTypeControls: {
		extra: []string{
			"Use for voting, state, contractual, or indirect control when ownership is unclear.",
			"Do NOT use CONTROLS for external audit/certification/inspection oversight; use ASSURES with qualifiers.assurance_type.",
		},
	},
	models.DependencyTypeManages: {
		extra: []string{
			"Formal management authority or operational responsibility, not ownership.",
		},
	},
	models.DependencyTypeInvestsIn: {
		extra: []string{
			"Funding or minority stake only; do not use unless investment is explicit.",
			"Direction is strict: investor -> investee.",
		},
	},
	models.DependencyTypePartnersWith: {
		extra: []string{
			"Peer collaboration only; not ownership or supply-chain.",
		},
	},
	models.DependencyTypeProduces: {
		extra: []string{
			"For PRODUCT -> PRODUCT value-chain steps use TRANSFORMS_INTO, ASSEMBLES_INTO, or USED_IN instead of PRODUCES.",
		},
	},
	models.DependencyTypeTransformsInto: {
		extra: []string{
			"Direction must follow the value chain: upstream raw/earlier-stage product -> downstream refined/later-stage product.",
		},
	},
	models.DependencyTypeAssemblesInto: {
		extra: []string{
			"Use for direct component/subassembly -> assembled-product steps.",
		},
	},
	models.DependencyTypeUsedIn: {
		extra: []string{
			"Use for strong operational input semantics where the source is required but not transformed into the target and not assembled into it.",
		},
	},
	models.DependencyTypeAssures: {
		extra: []string{
			"Add qualifiers.assurance_type: audit | certification | regulatory | inspection.",
		},
	},
	models.DependencyTypeInfluences: {
		extra: []string{
			"Use for soft power, pressure, or indirect impact only.",
		},
	},
	models.DependencyTypeParticipatesIn: {
		extra: []string{
			"PARTICIPATES_IN direction is participant -> market.",
			`Put market share in relation.metrics["market_share_percent"] when known.`,
		},
	},
	models.DependencyTypeTradedIn: {
		extra: []string{
			"Use only for product↔market trading or price-discovery context. Do NOT use PARTICIPATES_IN for PRODUCT -> MARKET.",
		},
	},
	models.DependencyTypeOperatesVia: {
		extra: []string{
			"Prefer MEDIA_CHANNEL targets such as /media/{platform}/{channel-slug}, not the platform organization.",
		},
	},
	models.DependencyTypeMemberOf: {
		extra: []string{
			"Use only for PERSON -> COMMUNITY. Do not use for orgs, locations, or facilities.",
		},
	},
	models.DependencyTypeAffiliatedWith: {
		extra: []string{
			"Use for COMMUNITY/PERSON affiliation with a formal organization, not ownership.",
		},
	},
	models.DependencyTypeHasSubgroup: {
		extra: []string{
			"Community hierarchy is broader -> narrower subgroup.",
		},
	},
	models.DependencyTypeGatekeepsDecision: {
		extra: []string{
			"When THIS FRAGMENT is DECISION_POINT, use explicit sourceCanonicalPath for the gatekeeper.",
		},
	},
	models.DependencyTypeUsesMechanism: {
		extra: []string{
			"When THIS FRAGMENT is MECHANISM, use explicit sourceCanonicalPath for the actor or decision that uses it.",
		},
	},
	models.DependencyTypeAppliesTo: {
		extra: []string{
			"When THIS FRAGMENT is the affected entity, use explicit sourceCanonicalPath for the mechanism.",
		},
	},
	models.DependencyTypeControlsAccess: {
		extra: []string{
			"When THIS FRAGMENT is the controlled entity, use explicit sourceCanonicalPath for the controller.",
		},
	},
	models.DependencyTypeImposesCondition: {
		extra: []string{
			"When THIS FRAGMENT is the affected entity, use explicit sourceCanonicalPath for the imposer.",
		},
	},
	models.DependencyTypeInGeoScope: {
		extra: []string{
			"When available include qualifiers.scope_basis to explain why this geo is in scope.",
		},
	},
	models.DependencyTypeAffects: {
		extra: []string{
			"Use for downstream policy/community/market effects. Include pass-through/lag qualifiers when available.",
		},
	},
}

var explicitSourcePromptSpecs = []explicitSourcePromptSpec{
	{
		edgeType: models.DependencyTypeOwns,
		lines: []string{
			`If THIS FRAGMENT is owned by X, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.`,
			`Never emit reversed OWNS (THIS FRAGMENT -> its owner).`,
		},
	},
	{
		edgeType: models.DependencyTypeInvestsIn,
		lines: []string{
			`If THIS FRAGMENT is invested in by X, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.`,
			`Never emit reversed INVESTS_IN (THIS FRAGMENT -> its investor).`,
		},
	},
	{
		edgeType: models.DependencyTypeAssures,
		lines: []string{
			`If THIS FRAGMENT is audited/certified/inspected by X, keep relationType=ASSURES and set sourceCanonicalPath/sourceType/sourceName = X.`,
		},
	},
	{
		edgeType: models.DependencyTypeControls,
		lines: []string{
			`Use explicit source whenever THIS FRAGMENT is the semantic target.`,
			"If THIS FRAGMENT is PRODUCT and evidence says it is controlled by X, set sourceCanonicalPath = X and keep targetCanonicalPath = THIS FRAGMENT.",
		},
	},
	{
		edgeType: models.DependencyTypeGatekeepsDecision,
		lines: []string{
			`If THIS FRAGMENT is DECISION_POINT and you are answering "Who gatekeeps this decision?", set sourceCanonicalPath/sourceType/sourceName to the gatekeeper.`,
		},
	},
	{
		edgeType: models.DependencyTypeUsesMechanism,
		lines: []string{
			`If THIS FRAGMENT is MECHANISM and you are answering "Who uses this mechanism?", set sourceCanonicalPath/sourceType/sourceName to the user.`,
			`If THIS FRAGMENT is DECISION_POINT and you are answering "What mechanism does this decision use?", THIS FRAGMENT remains the source.`,
		},
	},
	{
		edgeType: models.DependencyTypeAppliesTo,
		lines: []string{
			`If THIS FRAGMENT is the affected entity, set sourceCanonicalPath/sourceType/sourceName to the mechanism.`,
		},
	},
	{
		edgeType: models.DependencyTypeControlsAccess,
		lines: []string{
			"If THIS FRAGMENT is the controlled entity, set sourceCanonicalPath/sourceType/sourceName to the controller.",
		},
	},
	{
		edgeType: models.DependencyTypeImposesCondition,
		lines: []string{
			"If THIS FRAGMENT is the affected entity, set sourceCanonicalPath/sourceType/sourceName to the imposer.",
		},
	},
	{
		edgeType: models.DependencyTypeAdministers,
		lines: []string{
			"If THIS FRAGMENT is FACILITY or DEPOSIT and evidence says it is administered by X, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.",
			"/org/ cannot administer /gov/; do not reverse that hierarchy.",
		},
	},
	{
		edgeType: models.DependencyTypeExtractsFrom,
		lines: []string{
			"If THIS FRAGMENT is DEPOSIT and evidence says facility X extracts from it, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.",
		},
	},
}

func domainScaffoldRelationTypesPromptString() string {
	return edgesemantics.DomainScaffoldRelationTypesString()
}

func relationTypesPromptForDomainMode(mode string) string {
	types := contractsemantics.AllowedEdgeStringsForDomainMode(mode)
	if len(types) == 0 {
		return ""
	}
	return strings.Join(types, ", ")
}

func orderedPromptEdgeTypes(includeDomain bool) []models.DependencyType {
	seen := make(map[models.DependencyType]struct{})
	out := make([]models.DependencyType, 0, len(edgesemantics.EdgeDirectionPolicies))
	appendType := func(dt models.DependencyType) {
		if _, ok := seen[dt]; ok {
			return
		}
		if !includeDomain {
			if _, ok := domainPromptEdges[dt]; ok {
				return
			}
		}
		if _, ok := edgesemantics.EdgeDirectionPolicies[dt]; !ok {
			return
		}
		seen[dt] = struct{}{}
		out = append(out, dt)
	}
	for _, dt := range preferredPromptEdgeOrder {
		appendType(dt)
	}
	remaining := make([]string, 0, len(edgesemantics.EdgeDirectionPolicies))
	for _, name := range edgesemantics.AllEdgeTypesList() {
		dt := models.DependencyType(name)
		if !includeDomain {
			if _, ok := domainPromptEdges[dt]; ok {
				continue
			}
		}
		if _, ok := seen[dt]; ok {
			continue
		}
		remaining = append(remaining, name)
	}
	sort.Strings(remaining)
	for _, name := range remaining {
		appendType(models.DependencyType(name))
	}
	return out
}

func renderEdgePromptEntry(dt models.DependencyType, indent string) string {
	rule, ok := edgesemantics.EdgeDirectionPolicies[dt]
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s- %s: %s. Source MUST be %s. Target MUST be %s.\n", indent, dt, strings.TrimSpace(rule.Description), promptFragmentTypes(rule.ValidSourceTypes), promptFragmentTypes(rule.ValidTargetTypes)))
	if replacement := strings.TrimSpace(legacyPromptReplacements[dt]); replacement != "" {
		b.WriteString(fmt.Sprintf("%s  - Legacy compatibility only. Do NOT emit it for new data; prefer %s.\n", indent, replacement))
	}
	if ann, ok := relationPromptAnnotations[dt]; ok {
		for _, extra := range ann.extra {
			if strings.TrimSpace(extra) == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("%s  - %s\n", indent, extra))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func generatedGeneralRelationTaxonomyBlock() string {
	var b strings.Builder
	for _, dt := range orderedPromptEdgeTypes(false) {
		b.WriteString(renderEdgePromptEntry(dt, ""))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func generatedResearchRelationDirectionRulesBlock() string {
	var phase1 []models.DependencyType
	var phase2 []models.DependencyType
	for _, dt := range orderedPromptEdgeTypes(false) {
		if _, ok := strategicPromptEdges[dt]; ok {
			phase2 = append(phase2, dt)
			continue
		}
		phase1 = append(phase1, dt)
	}

	var b strings.Builder
	b.WriteString("RELATION DIRECTION RULES (STRICT):\n")
	b.WriteString("Use ROLE-FIRST semantics for direction:\n")
	b.WriteString("- For directed ORG<->ORG relations (OWNS, SUBSIDIARY_OF, INVESTS_IN, ASSURES, CONTROLS), determine source by semantic role first (owner/investor/assurer/controller), not by CURRENT FRAGMENT.\n")
	b.WriteString("- If CURRENT FRAGMENT is the semantic target, sourceCanonicalPath/sourceType/sourceName is REQUIRED.\n")
	b.WriteString("- For STRATEGIC edge types, use EXPLICIT SOURCE (sourceCanonicalPath) when THIS FRAGMENT would be wrong as source.\n")
	b.WriteString("Choose relationType to match the CORRECT semantic direction:\n\n")
	b.WriteString("=== PHASE 1 RELATIONS ===\n")
	for _, dt := range phase1 {
		b.WriteString(renderEdgePromptEntry(dt, ""))
		b.WriteString("\n")
	}
	b.WriteString("\n=== PHASE 2 STRATEGIC RELATIONS (may require EXPLICIT SOURCE) ===\n")
	for _, dt := range phase2 {
		b.WriteString(renderEdgePromptEntry(dt, ""))
		b.WriteString("\n")
	}
	b.WriteString("- ORGANIZATION ↔ MARKET: PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES (domestic oversight only), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES.\n")
	b.WriteString("- Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/.\n")
	b.WriteString("  DOMAIN targets are allowed only for DOMAIN scaffold relations (")
	b.WriteString(domainScaffoldRelationTypesPromptString())
	b.WriteString(", BROADENS_TO, NARROWS_TO, DEPENDS_ON with DOMAIN source).\n")
	return strings.TrimRight(b.String(), "\n")
}

func generatedExplicitSourceRulesBlock() string {
	var b strings.Builder
	b.WriteString("EXPLICIT SOURCE RULES (CRITICAL for strict direction semantics):\n")
	b.WriteString("Do not assume edges originate FROM THIS FRAGMENT.\n")
	b.WriteString("For direction-sensitive relations, infer semantic source from roles first.\n")
	b.WriteString("When semantic source is not THIS FRAGMENT, you MUST use explicit sourceCanonicalPath.\n\n")
	b.WriteString("WHEN TO USE sourceCanonicalPath (MANDATORY):\n")
	for idx, spec := range explicitSourcePromptSpecs {
		rule, ok := edgesemantics.EdgeDirectionPolicies[spec.edgeType]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("%d. %s: source MUST be %s, target MUST be %s.\n", idx+1, spec.edgeType, promptFragmentTypes(rule.ValidSourceTypes), promptFragmentTypes(rule.ValidTargetTypes)))
		for _, line := range spec.lines {
			b.WriteString(fmt.Sprintf("   - %s\n", line))
		}
	}
	b.WriteString("\nWHEN NOT TO USE sourceCanonicalPath:\n")
	b.WriteString("- For most Phase 1 relations (DEPENDS_ON, SUPPLIES_TO, LOCATED_IN, etc.) the default FROM THIS FRAGMENT is correct.\n")
	b.WriteString("- Exception: use explicit source whenever THIS FRAGMENT is the semantic target for OWNS / INVESTS_IN / ASSURES / CONTROLS.\n")
	b.WriteString("- GEO_REGION exception: when CURRENT FRAGMENT is /loc/... and you discover an organization, community, deposit, program, facility, or person that is in this region, that discovered entity is usually the SOURCE and CURRENT FRAGMENT is the TARGET via LOCATED_IN/OPERATES_IN.\n")
	b.WriteString("- For GEO_REGION tasks, never express regional presence as CURRENT FRAGMENT -> entity with OWNS, MEMBER_OF, HAS_SUBGROUP, or similar social/governance edges.\n")
	return strings.TrimRight(b.String(), "\n")
}

func generatedRoleQualifierRulesBlock() string {
	return `ROLE QUALIFIERS (REQUIRED for directed ORG<->ORG):
- For OWNS / SUBSIDIARY_OF / INVESTS_IN / ASSURES / CONTROLS between ORGANIZATION nodes, include qualifiers.source_role and qualifiers.target_role.
- For directional actor↔actor DEPENDS_ON / SUPPLIES_TO, include source_role/target_role as well.
- Allowed examples:
  - OWNS: owner -> owned_entity
  - SUBSIDIARY_OF: subsidiary -> parent_owner
  - INVESTS_IN: investor -> investee
  - ASSURES: assurer -> assuree
  - CONTROLS: controller -> controlled_entity
  - DEPENDS_ON: dependent -> dependency
  - SUPPLIES_TO: supplier -> customer

CRITICAL: Always verify that the resulting edge direction matches EDGE DIRECTION VALIDATION RULES below.`
}

func generatedDiscoveredRelationSchemaBlock(targetTypeEnum string) string {
	return fmt.Sprintf(`DiscoveredRelation Schema:
{
  "sourceCanonicalPath": "string (OPTIONAL - see EXPLICIT SOURCE RULES below)",
  "sourceType": "string (OPTIONAL - required when sourceCanonicalPath is set)",
  "sourceName": "string (OPTIONAL - required when sourceCanonicalPath is set)",
  "targetName": "string",
  "targetLegalName": "string",
  "targetShortName": "string",
  "targetType": %s,
  "relationType": %s,
  "reason": "string",
  "confidence": 0.0,
  "targetCanonicalPath": "string, REQUIRED",
  "validFrom": "YYYY-MM-DD (optional)",
  "validTo": "YYYY-MM-DD (optional)",
  "qualifiers": { "edgeModality": "actual|projected|historical|potential", "eventId": "/event/{slug}" },
  "metrics": { "ownership_percent": { "value": 45.0, "unit": "percent", "confidence": 0.9, "observedAt": "YYYY-MM-DD", "provenance": {"sourceIds":["..."], "method":"extracted"} } }
}`, targetTypeEnum, edgesemantics.AllEdgeTypesForPrompt())
}

func generatedIntentDecomposerRelationRulesBlock(domainModes []string) string {
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	allowlist := edgesemantics.EdgeAllowlistByDomainModes(domainModes)
	isSupplyChain := taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) ||
		taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeServiceChain)

	filtered := make([]models.DependencyType, 0)
	for _, dt := range orderedPromptEdgeTypes(false) {
		if !edgesemantics.AllowlistContains(allowlist, dt) {
			continue
		}
		filtered = append(filtered, dt)
	}

	var b strings.Builder
	if isSupplyChain {
		b.WriteString("RELATION DIRECTION RULES (STRICT — supply-chain contract only):\n")
	} else {
		b.WriteString("RELATION DIRECTION RULES (STRICT):\n")
	}
	b.WriteString("Each relation is a directed edge FROM TARGET (source) TO targetCanonicalPath (target).\n")
	b.WriteString("Emit a relation only when TARGET is the correct semantic source. Do not reverse direction to describe incoming owners, gatekeepers, controllers, or investors into TARGET.\n")
	if len(filtered) > 0 {
		names := make([]string, 0, len(filtered))
		for _, dt := range filtered {
			names = append(names, string(dt))
		}
		b.WriteString(fmt.Sprintf("Allowed relationType values ONLY: %s.\n", strings.Join(names, ", ")))
	}
	if isSupplyChain {
		b.WriteString("Do NOT use DEPENDS_ON, PARTNERS_WITH, COMPETES_WITH, SUBSIDIARY_OF, OWNS, INVESTS_IN, or other non-supply-chain types.\n")
	} else {
		b.WriteString("If TARGET is not the semantic source for OWNS / INVESTS_IN / ASSURES / CONTROLS / GATEKEEPS_DECISION / USES_MECHANISM / APPLIES_TO / CONTROLS_ACCESS / IMPOSES_CONDITION, omit the relation in decomposition.\n")
	}
	for _, dt := range filtered {
		b.WriteString(renderEdgePromptEntry(dt, ""))
		b.WriteString("\n")
	}
	if !isSupplyChain {
		b.WriteString("  - ORGANIZATION ↔ MARKET: PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES (domestic oversight), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES.\n")
	} else {
		b.WriteString("  - In supply_chain mode, MARKET source may emit only PARTICIPATES_IN or HAS_MARKET_SEGMENT. PRODUCT -> MARKET uses TRADED_IN only for explicit trading/pricing context.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// PromptIntentDecomposerRelationRulesBlock exposes the shared relation-direction contract
// used by intent decomposition so other planners can reuse the exact same prompt block.
func PromptIntentDecomposerRelationRulesBlock(domainModes []string) string {
	return generatedIntentDecomposerRelationRulesBlock(domainModes)
}

// PromptRelationTypesForDomainMode exposes the shared relation allowlist prompt string
// used by composable Research/Critic flows.
func PromptRelationTypesForDomainMode(mode string) string {
	return relationTypesPromptForDomainMode(mode)
}

func promptFragmentTypes(types []models.FragmentType) string {
	if len(types) == 0 {
		return "*"
	}
	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, string(t))
	}
	return strings.Join(parts, " | ")
}

func indentPromptBlock(block, indent string) string {
	block = strings.TrimRight(block, "\n")
	if strings.TrimSpace(block) == "" {
		return ""
	}
	lines := strings.Split(block, "\n")
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}

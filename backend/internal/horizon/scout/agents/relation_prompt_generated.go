package agents

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type relationPromptAnnotation struct {
	extra []string
}

type explicitSourcePromptSpec struct {
	edgeType DependencyType
	lines    []string
}

var preferredPromptEdgeOrder = []DependencyType{
	DependencyTypeDependsOn,
	DependencyTypeSuppliesTo,
	DependencyTypeLocatedIn,
	DependencyTypeOperatesIn,
	DependencyTypeRegulates,
	DependencyTypeOwns,
	DependencyTypeSubsidiaryOf,
	DependencyTypeControls,
	DependencyTypeManages,
	DependencyTypeAdministers,
	DependencyTypeEmployedBy,
	DependencyTypeBoardMemberOf,
	DependencyTypeFounded,
	DependencyTypeInvestsIn,
	DependencyTypePartnersWith,
	DependencyTypeProduces,
	DependencyTypeTransformsInto,
	DependencyTypeAssemblesInto,
	DependencyTypeUsedIn,
	DependencyTypeExtractsFrom,
	DependencyTypeAssures,
	DependencyTypeCompetesWith,
	DependencyTypeInfluences,
	DependencyTypeParticipatesIn,
	DependencyTypeTradedIn,
	DependencyTypeDominates,
	DependencyTypeHasMarketSegment,
	DependencyTypeOperatesVia,
	DependencyTypeMemberOf,
	DependencyTypeAffiliatedWith,
	DependencyTypeHasSubgroup,
	DependencyTypeRenamedTo,
	DependencyTypeRebrandedTo,
	DependencyTypeLegalSuccessorOf,
	DependencyTypeMergedInto,
	DependencyTypeSpunOffFrom,
	DependencyTypeAcquiredBy,
	DependencyTypeGatekeepsDecision,
	DependencyTypeUsesMechanism,
	DependencyTypeAppliesTo,
	DependencyTypeControlsAccess,
	DependencyTypeImposesCondition,
	DependencyTypeInGeoScope,
	DependencyTypeAffects,
	DependencyTypeRelativeOf,
	DependencyTypeRelatedTo,
	DependencyTypeAssociatedWith,
	DependencyTypeCorporateTransition,
	DependencyTypeDomainRelation,
	DependencyTypeWorksFor,
	DependencyTypeKeyPerson,
	DependencyTypeHasKeyPerson,
	DependencyTypeAuditedBy,
	DependencyTypeHasSubmarket,
}

var strategicPromptEdges = map[DependencyType]struct{}{
	DependencyTypeGatekeepsDecision: {},
	DependencyTypeUsesMechanism:     {},
	DependencyTypeAppliesTo:         {},
	DependencyTypeControlsAccess:    {},
	DependencyTypeImposesCondition:  {},
}

// legacyPromptOnlyDirectionPolicies enumerates DependencyType values
// that have NO entry in edgesemantics.EdgeDirectionPolicies (because
// AH folded/dropped them per docs/taxonomy/migration_map.yaml) but
// must remain in the LLM-facing extraction prompt vocabulary during
// the migration window. The graph_intent decoder
// (legacyRelationAliases) folds the LLM's answers back to canonical
// AH at parse time.
//
// This is the prompt-rendering analogue of the scout-level
// EdgeDirectionPolicies overlay in scout/edge_normalizer.go; it lives
// here because scout/agents cannot import scout (circular).
var legacyPromptOnlyDirectionPolicies = map[DependencyType]edgesemantics.EdgeDirectionRule{
	DependencyTypeRelativeOf: {
		Description: "LEGACY (read-side only): kinship link between persons (folded into attributes.relations[] under AH).",
	},
	DependencyTypeRenamedTo: {
		Description: "LEGACY (read-side only): organizational rename succession (folded into nameHistory under AH).",
	},
	DependencyTypeRebrandedTo: {
		Description: "LEGACY (read-side only): organizational rebrand succession (folded into nameHistory under AH).",
	},
	DependencyTypeEmployedBy: {
		Description: "LEGACY (read-side only): person employed by organization (dropped as LLM noise under AH).",
	},
	DependencyTypeWorksFor: {
		Description: "LEGACY (read-side only): person works for organization (dropped as LLM noise under AH).",
	},
	DependencyTypeKeyPerson: {
		Description: "LEGACY (read-side only): person is a key person at organization (folded into attributes.key_persons[] under AH).",
	},
	DependencyTypeHasKeyPerson: {
		Description: "LEGACY (read-side only): organization has key person (folded into attributes.key_persons[] under AH).",
	},
	DependencyTypeFounded: {
		Description: "LEGACY (read-side only): person founded organization (folded into attributes.founders[] under AH).",
	},
	DependencyTypeAuditedBy: {
		Description: "LEGACY (read-side only): organization audited by another org (canonical AH form is assures + assurance_type=audit).",
	},
	DependencyTypeAffiliatedWith: {
		Description: "LEGACY (read-side only): vague actor↔actor affiliation (dropped as LLM noise under AH).",
	},
	DependencyTypeAssociatedWith: {
		Description: "LEGACY (read-side only): symmetric actor↔actor association (dropped as LLM noise under AH).",
	},
	DependencyTypeRelatedTo: {
		Description: "LEGACY (read-side only): generic 'related-to' link (dropped as too vague under AH).",
	},
	DependencyTypeHasSubgroup: {
		Description: "LEGACY (read-side only): community has subgroup (folded into attributes.subgroups[] under AH).",
	},
	DependencyTypeCorporateTransition: {
		Description: "LEGACY (read-side only): umbrella corporate transition edge (renamed/merged/spun-off/acquired).",
	},
	DependencyTypeManages: {
		Description: "LEGACY (read-side only): managerial relation (canonical AH form is `managed_by` with src/dst swapped).",
	},
}

// supplyChainLegacyDecomposerEdges enumerates legacy supply-chain
// relation literals that the AH allowlist drops (per
// docs/taxonomy/migration_map.yaml dispositions
// Dropped/fold_to_attributes) but which the intent-decomposer prompt
// still surfaces during the migration window. The graph_intent
// decoder (legacyRelationAliases) folds them back to canonical AH at
// parse time.
var supplyChainLegacyDecomposerEdges = []DependencyType{
	DependencyTypeTransformsInto,
	DependencyTypeAssemblesInto,
	DependencyTypeUsedIn,
	DependencyTypeExtractsFrom,
	DependencyTypeHasMarketSegment,
	DependencyTypeHasSubmarket,
	DependencyTypeCompetesWith,
	DependencyTypeParticipatesIn,
	DependencyTypeOperatesVia,
}

var domainPromptEdges = map[DependencyType]struct{}{
	DependencyTypeFocusesOn:      {},
	DependencyTypeInvolvesActor:  {},
	DependencyTypeAbout:          {},
	DependencyTypeRelatedDomain:  {},
	DependencyTypeDomainRelation: {},
	DependencyTypeBroadensTo:     {},
	DependencyTypeNarrowsTo:      {},
	DependencyTypeSurfaces:       {},
}

var legacyPromptReplacements = map[DependencyType]string{
	DependencyTypeAuditedBy:           "ASSURES",
	DependencyTypeHasSubmarket:        "HAS_MARKET_SEGMENT",
	DependencyTypeWorksFor:            "EMPLOYED_BY",
	DependencyTypeHasKeyPerson:        "KEY_PERSON",
	DependencyTypeCorporateTransition: "RENAMED_TO / REBRANDED_TO / LEGAL_SUCCESSOR_OF / MERGED_INTO / SPUN_OFF_FROM / ACQUIRED_BY",
	DependencyTypeDomainRelation:      "RELATED_DOMAIN / BROADENS_TO / NARROWS_TO",
}

var relationPromptAnnotations = map[DependencyType]relationPromptAnnotation{
	DependencyTypeDependsOn: {
		extra: []string{
			"Use when the source requires the target to operate or deliver outcomes.",
			"Do NOT use when the source is the supplier and the target is the customer; use SUPPLIES_TO.",
			"For actor↔actor dependencies include qualifiers.source_role=dependent and qualifiers.target_role=dependency.",
		},
	},
	DependencyTypeSuppliesTo: {
		extra: []string{
			"Commercial flow from supplier -> customer/recipient; not ownership or control.",
			"MARKET MUST NOT be source or target for SUPPLIES_TO.",
			"For ORG/FACILITY -> ORG/FACILITY supply links include qualifiers.source_role=supplier and qualifiers.target_role=customer.",
		},
	},
	DependencyTypeLocatedIn: {
		extra: []string{
			"Use for physical/geographic placement only.",
		},
	},
	DependencyTypeOperatesIn: {
		extra: []string{
			"Use for operational activity or facility operation, not just a registered address.",
			"MARKET MUST NOT be source for OPERATES_IN.",
		},
	},
	DependencyTypeRegulates: {
		extra: []string{
			`JURISDICTION RULE: REGULATES means ongoing domestic oversight within the regulator's own jurisdiction.`,
			"Do NOT use REGULATES for sanctions, trade restrictions, or extra-territorial enforcement. Use CONTROLS_ACCESS or IMPOSES_CONDITION instead.",
		},
	},
	DependencyTypeOwns: {
		extra: []string{
			"Use OWNS only for legal, beneficial, or controlling ownership of the target.",
			"NEVER output OWNS where targetType=PERSON.",
			"Facilities/products/deposits do not own organizations.",
			`NEVER output OWNS from ORGANIZATION to COMMUNITY if target canonicalPath is /community/{geo}/dynasty/{slug} or /community/{geo}/tribe/{slug}. Dynasties and tribes own organizations, not the reverse.`,
		},
	},
	DependencyTypeSubsidiaryOf: {
		extra: []string{
			"Inverse ownership form (child -> parent). Runtime normalizes it into OWNS.",
		},
	},
	DependencyTypeControls: {
		extra: []string{
			"Use for voting, state, contractual, or indirect control when ownership is unclear.",
			"Do NOT use CONTROLS for external audit/certification/inspection oversight; use ASSURES with qualifiers.assurance_type.",
		},
	},
	DependencyTypeManages: {
		extra: []string{
			"Formal management authority or operational responsibility, not ownership.",
		},
	},
	DependencyTypeInvestsIn: {
		extra: []string{
			"Funding or minority stake only; do not use unless investment is explicit.",
			"Direction is strict: investor -> investee.",
		},
	},
	DependencyTypePartnersWith: {
		extra: []string{
			"Peer collaboration only; not ownership or supply-chain.",
		},
	},
	DependencyTypeProduces: {
		extra: []string{
			"For PRODUCT -> PRODUCT value-chain steps use TRANSFORMS_INTO, ASSEMBLES_INTO, or USED_IN instead of PRODUCES.",
		},
	},
	DependencyTypeTransformsInto: {
		extra: []string{
			"Direction must follow the value chain: upstream raw/earlier-stage product -> downstream refined/later-stage product.",
		},
	},
	DependencyTypeAssemblesInto: {
		extra: []string{
			"Use for direct component/subassembly -> assembled-product steps.",
		},
	},
	DependencyTypeUsedIn: {
		extra: []string{
			"Use for strong operational input semantics where the source is required but not transformed into the target and not assembled into it.",
		},
	},
	DependencyTypeAssures: {
		extra: []string{
			"Add qualifiers.assurance_type: audit | certification | regulatory | inspection.",
		},
	},
	DependencyTypeInfluences: {
		extra: []string{
			"Use for soft power, pressure, or indirect impact only.",
		},
	},
	DependencyTypeParticipatesIn: {
		extra: []string{
			"PARTICIPATES_IN direction is participant -> market.",
			`Put market share in relation.metrics["market_share_percent"] when known.`,
		},
	},
	DependencyTypeTradedIn: {
		extra: []string{
			"Use only for product↔market trading or price-discovery context. Do NOT use PARTICIPATES_IN for PRODUCT -> MARKET.",
		},
	},
	DependencyTypeOperatesVia: {
		extra: []string{
			"Prefer MEDIA_CHANNEL targets such as /media/{platform}/{channel-slug}, not the platform organization.",
		},
	},
	DependencyTypeMemberOf: {
		extra: []string{
			"Use only for PERSON -> COMMUNITY. Do not use for orgs, locations, or facilities.",
		},
	},
	DependencyTypeAffiliatedWith: {
		extra: []string{
			"Use for COMMUNITY/PERSON affiliation with a formal organization, not ownership.",
		},
	},
	DependencyTypeHasSubgroup: {
		extra: []string{
			"Community hierarchy is broader -> narrower subgroup.",
		},
	},
	DependencyTypeGatekeepsDecision: {
		extra: []string{
			"When THIS FRAGMENT is DECISION_POINT, use explicit sourceCanonicalPath for the gatekeeper.",
		},
	},
	DependencyTypeUsesMechanism: {
		extra: []string{
			"When THIS FRAGMENT is MECHANISM, use explicit sourceCanonicalPath for the actor or decision that uses it.",
		},
	},
	DependencyTypeAppliesTo: {
		extra: []string{
			"When THIS FRAGMENT is the affected entity, use explicit sourceCanonicalPath for the mechanism.",
		},
	},
	DependencyTypeControlsAccess: {
		extra: []string{
			"When THIS FRAGMENT is the controlled entity, use explicit sourceCanonicalPath for the controller.",
		},
	},
	DependencyTypeImposesCondition: {
		extra: []string{
			"When THIS FRAGMENT is the affected entity, use explicit sourceCanonicalPath for the imposer.",
		},
	},
	DependencyTypeInGeoScope: {
		extra: []string{
			"When available include qualifiers.scope_basis to explain why this geo is in scope.",
		},
	},
	DependencyTypeAffects: {
		extra: []string{
			"Use for downstream policy/community/market effects. Include pass-through/lag qualifiers when available.",
		},
	},
}

var explicitSourcePromptSpecs = []explicitSourcePromptSpec{
	{
		edgeType: DependencyTypeOwns,
		lines: []string{
			`If THIS FRAGMENT is owned by X, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.`,
			`Never emit reversed OWNS (THIS FRAGMENT -> its owner).`,
		},
	},
	{
		edgeType: DependencyTypeInvestsIn,
		lines: []string{
			`If THIS FRAGMENT is invested in by X, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.`,
			`Never emit reversed INVESTS_IN (THIS FRAGMENT -> its investor).`,
		},
	},
	{
		edgeType: DependencyTypeAssures,
		lines: []string{
			`If THIS FRAGMENT is audited/certified/inspected by X, keep relationType=ASSURES and set sourceCanonicalPath/sourceType/sourceName = X.`,
		},
	},
	{
		edgeType: DependencyTypeControls,
		lines: []string{
			`Use explicit source whenever THIS FRAGMENT is the semantic target.`,
			"If THIS FRAGMENT is PRODUCT and evidence says it is controlled by X, set sourceCanonicalPath = X and keep targetCanonicalPath = THIS FRAGMENT.",
		},
	},
	{
		edgeType: DependencyTypeGatekeepsDecision,
		lines: []string{
			`If THIS FRAGMENT is DECISION_POINT and you are answering "Who gatekeeps this decision?", set sourceCanonicalPath/sourceType/sourceName to the gatekeeper.`,
		},
	},
	{
		edgeType: DependencyTypeUsesMechanism,
		lines: []string{
			`If THIS FRAGMENT is MECHANISM and you are answering "Who uses this mechanism?", set sourceCanonicalPath/sourceType/sourceName to the user.`,
			`If THIS FRAGMENT is DECISION_POINT and you are answering "What mechanism does this decision use?", THIS FRAGMENT remains the source.`,
		},
	},
	{
		edgeType: DependencyTypeAppliesTo,
		lines: []string{
			`If THIS FRAGMENT is the affected entity, set sourceCanonicalPath/sourceType/sourceName to the mechanism.`,
		},
	},
	{
		edgeType: DependencyTypeControlsAccess,
		lines: []string{
			"If THIS FRAGMENT is the controlled entity, set sourceCanonicalPath/sourceType/sourceName to the controller.",
		},
	},
	{
		edgeType: DependencyTypeImposesCondition,
		lines: []string{
			"If THIS FRAGMENT is the affected entity, set sourceCanonicalPath/sourceType/sourceName to the imposer.",
		},
	},
	{
		edgeType: DependencyTypeAdministers,
		lines: []string{
			"If THIS FRAGMENT is FACILITY or DEPOSIT and evidence says it is administered by X, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.",
			"/org/ cannot administer /gov/; do not reverse that hierarchy.",
		},
	},
	{
		edgeType: DependencyTypeExtractsFrom,
		lines: []string{
			"If THIS FRAGMENT is DEPOSIT and evidence says facility X extracts from it, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.",
		},
	},
}

func domainScaffoldRelationTypesPromptString() string {
	return DomainScaffoldRelationTypesString()
}

func relationTypesPromptForDomainMode(mode string) string {
	types := contractsemantics.AllowedEdgeStringsForDomainMode(mode)
	if len(types) == 0 {
		return ""
	}
	return strings.Join(types, ", ")
}

func orderedPromptEdgeTypes(includeDomain bool) []DependencyType {
	seen := make(map[DependencyType]struct{})
	out := make([]DependencyType, 0, len(edgesemantics.EdgeDirectionPolicies)+len(legacyPromptOnlyDirectionPolicies))
	appendType := func(dt DependencyType) {
		if _, ok := seen[dt]; ok {
			return
		}
		if !includeDomain {
			if _, ok := domainPromptEdges[dt]; ok {
				return
			}
		}
		if _, hasAH := edgesemantics.EdgeDirectionPolicies[dt]; !hasAH {
			if _, hasLegacy := legacyPromptOnlyDirectionPolicies[dt]; !hasLegacy {
				return
			}
		}
		seen[dt] = struct{}{}
		out = append(out, dt)
	}
	for _, dt := range preferredPromptEdgeOrder {
		appendType(dt)
	}
	// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
	// edgesemantics.EdgeDirectionPolicies is keyed by AH RelationName
	// (snake_case). The legacy AllEdgeTypesList() helper returns
	// UPPER_SNAKE wire labels, which would never resolve through
	// appendType because the policies map can't find them. Walk the AH
	// SoT directly so every newly-added AH relation (e.g.
	// incorporated_in, alternate_to, acquired_stake_from) flows into
	// the generated general taxonomy block; the test
	// (TestGeneratedGeneralRelationTaxonomyBlock_CoversAllNonDomainEdgePolicies)
	// pins on this exact invariant.
	remaining := make([]string, 0, len(edgesemantics.EdgeDirectionPolicies))
	for rel := range edgesemantics.EdgeDirectionPolicies {
		dt := DependencyType(rel)
		if !includeDomain {
			if _, ok := domainPromptEdges[dt]; ok {
				continue
			}
		}
		if _, ok := seen[dt]; ok {
			continue
		}
		remaining = append(remaining, string(rel))
	}
	sort.Strings(remaining)
	for _, name := range remaining {
		appendType(DependencyType(name))
	}
	return out
}

func renderEdgePromptEntry(dt DependencyType, indent string) string {
	rule, ok := edgesemantics.EdgeDirectionPolicies[dt]
	if !ok {
		// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
		// fall back to the prompt-only legacy overlay so DependencyType
		// values whose AH disposition is Dropped/fold_to_attributes
		// (RELATIVE_OF, RENAMED_TO, EMPLOYED_BY, …) still surface in the
		// scout extraction prompt vocabulary; without this fallback the
		// LLM stops emitting the legacy wire labels mid-migration even
		// though the graph_intent decoder still folds them through
		// legacyRelationAliases at parse time.
		legacyRule, legacyOK := legacyPromptOnlyDirectionPolicies[dt]
		if !legacyOK {
			return ""
		}
		rule = legacyRule
	}
	// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml +
	// AGENTS.md): the LLM-facing scout extraction prompts pin on the
	// pre-migration UPPER_SNAKE wire labels (OWNS, RELATIVE_OF,
	// RENAMED_TO, …). Render via wireLabelForDependencyType so AH
	// RelationName values like "beneficially_owns" surface as "OWNS"
	// in the prompt body; the graph_intent decoder folds the LLM's
	// answers back through legacyRelationAliases at parse time.
	wireLabel := wireLabelForDependencyType(dt)
	var b strings.Builder
	// Emit "WIRE_LABEL (canonical_ah_name)" so prompts pin on the
	// LLM-facing UPPER_SNAKE form while the AH canonical slug stays
	// visible to anyone (or any test) that scans the block for the
	// exact AH RelationName key. When the wire label and the AH slug
	// coincide (e.g. legacy literals like FOCUSES_ON/focuses_on)
	// only the wire label is rendered.
	header := wireLabel
	if canonical := strings.TrimSpace(string(dt)); canonical != "" && canonical != wireLabel {
		header = fmt.Sprintf("%s (%s)", wireLabel, canonical)
	}
	b.WriteString(fmt.Sprintf("%s- %s: %s. Source MUST be %s. Target MUST be %s.\n", indent, header, strings.TrimSpace(rule.Description), promptFragmentTypes(rule.ValidSourceClasses), promptFragmentTypes(rule.ValidTargetClasses)))
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
	var phase1 []DependencyType
	var phase2 []DependencyType
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
		b.WriteString(fmt.Sprintf("%d. %s: source MUST be %s, target MUST be %s.\n", idx+1, spec.edgeType, promptFragmentTypes(rule.ValidSourceClasses), promptFragmentTypes(rule.ValidTargetClasses)))
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
}`, targetTypeEnum, AllEdgeTypesForPrompt())
}

func generatedIntentDecomposerRelationRulesBlock(domainModes []string) string {
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	allowlist := edgesemantics.EdgeAllowlistByDomainModes(domainModes)
	isSupplyChain := taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) ||
		taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeServiceChain)

	filtered := make([]DependencyType, 0)
	seenFiltered := make(map[DependencyType]struct{})
	for _, dt := range orderedPromptEdgeTypes(false) {
		if !edgesemantics.AllowlistContains(allowlist, dt) {
			continue
		}
		if _, ok := seenFiltered[dt]; ok {
			continue
		}
		seenFiltered[dt] = struct{}{}
		filtered = append(filtered, dt)
	}
	// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
	// edgesemantics.EdgeAllowlistByDomainModes is the AH-canonical
	// projection (snake_case AH RelationName). It silently drops
	// legacy supply-chain literals whose disposition is `Dropped: true`
	// or `fold_to_attributes` (transforms_into, has_market_segment,
	// competes_with, …) even though they remain part of the
	// supply-chain LLM contract during the migration window. Layer
	// them in here from a hardcoded table so the decomposer prompt
	// (and the test pinning the contract) keeps surfacing them; the
	// graph_intent decoder folds the LLM's answers back to canonical
	// AH at parse time via legacyRelationAliases.
	if isSupplyChain {
		for _, legacy := range supplyChainLegacyDecomposerEdges {
			if _, ok := seenFiltered[legacy]; ok {
				continue
			}
			seenFiltered[legacy] = struct{}{}
			filtered = append(filtered, legacy)
		}
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
		b.WriteString("Do NOT use DEPENDS_ON, SUBSIDIARY_OF, OWNS, or other non-supply-chain types. COMPETES_WITH is allowed only for explicit direct competitor relationships. PARTNERS_WITH and OPERATES_VIA are allowed only for explicit operational/ecosystem overlays. INVESTS_IN and MEMBER_OF are allowed only for explicit institutional/structural overlays.\n")
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

func promptFragmentTypes(types []FragmentType) string {
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

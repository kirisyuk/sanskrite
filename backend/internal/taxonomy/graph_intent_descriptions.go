package taxonomy

import "strings"

// GraphIntentNodeTypeDescriptions returns node types with short semantic descriptions for the planner prompt.
// types: sorted list of allowed node type strings (e.g. from GraphIntentAllowedNodeTypesSorted).
func GraphIntentNodeTypeDescriptions(types []string) string {
	descriptions := map[string]string{
		"ORGANIZATION":      "Company, corporation, NGO, or any legal entity with formal structure",
		"GOVERNMENT_AGENCY": "Government body: ministry, department, regulator, state-owned entity",
		"FACILITY":          "Physical site: factory, office, plant, warehouse",
		"GEO_REGION":        "Geographic area: country, city, region, district",
		"DOMAIN":            "Knowledge contract / policy domain (not a real-world entity)",
		"SPHERE":            "Abstract sphere or category",
		"EVENT":             "Occurrence: funding round, merger, regulation change",
		"PRODUCT":           "Good or service: raw material, component, finished product",
		"PROGRAM":           "Government or corporate program, initiative, scheme",
		"MARKET":            "Market segment: industry, sector, vertical",
		"PERSON":            "Individual: executive, founder, key person",
		"MEDIA_CHANNEL":     "Media outlet: channel, publication, platform",
		"DECISION_POINT":    "Strategic decision or choice point in a domain",
		"MECHANISM":         "Control mechanism: regulatory, economic, technological",
		"SUPPLY_LINK":       "Supply chain link between entities",
		"TRACK":             "Tracked object with position/state",
		"DEPOSIT":           "Natural resource deposit: oil field, mine, basin",
		"COMMUNITY":         "Informal social group: network, association, collective",
		"COMMUNITY_DYNASTY": "Family dynasty or lineage",
		"COMMUNITY_TRIBE":   "Tribe or tribal structure",
	}
	var lines []string
	for _, t := range types {
		desc := descriptions[t]
		if desc == "" {
			desc = t
		}
		lines = append(lines, "  - "+t+": "+desc)
	}
	return "Allowed node types (use exact string):\n" + strings.Join(lines, "\n")
}

// GraphIntentRelationTypeDescriptions returns relation types with short semantic descriptions.
// types: sorted list of allowed relation type strings.
func GraphIntentRelationTypeDescriptions(types []string) string {
	descriptions := map[string]string{
		"SUPPLIES_TO":          "Supplier provides goods/services to customer",
		"LOCATED_IN":           "Entity is physically in a location",
		"OWNS":                 "Ownership stake or full ownership",
		"SUBSIDIARY_OF":        "Child company of parent",
		"PRODUCES":             "Produces output (product, service)",
		"OPERATES_IN":          "Operates in a region/facility/deposit",
		"REGULATES":            "Regulator oversees regulated entity",
		"COMPETES_WITH":        "Competitive relationship",
		"DEPENDS_ON":           "Technical or operational dependency",
		"INFLUENCES":           "General influence (policy, market)",
		"TRANSFORMS_INTO":      "Transformation of entity/product",
		"ASSEMBLES_INTO":       "Assembly of component/subassembly into downstream product",
		"USED_IN":              "Operational input product is used in downstream product",
		"EXTRACTS_FROM":        "Extracts from source",
		"PARTICIPATES_IN":      "Entity participates in a market",
		"TRADED_IN":            "Product is traded or price-discovered in a market",
		"DOMINATES":            "Legacy market-leadership label (prefer PARTICIPATES_IN + marketRole)",
		"HAS_MARKET_SEGMENT":   "Parent market has segment (child path = parent path + one segment)",
		"ADMINISTERS":          "Administrative oversight (e.g. ministry over agency)",
		"MANAGES":              "Management relationship",
		"CONTROLS":             "Control over entity",
		"CONTROLS_ACCESS":      "Controls access to resource",
		"EMPLOYED_BY":          "Employment relationship",
		"BOARD_MEMBER_OF":      "Board membership",
		"FOUNDED":              "Founder of entity",
		"INVESTS_IN":           "Investment relationship",
		"PARTNERS_WITH":        "Partnership",
		"IMPOSES_CONDITION":    "Imposes condition on entity",
		"GATEKEEPS_DECISION":   "Gatekeeps a decision",
		"USES_MECHANISM":       "Uses a mechanism",
		"APPLIES_TO":           "Applies to target",
		"ASSOCIATED_WITH":      "Association (aligns, cooperates)",
		"CORPORATE_TRANSITION": "Corporate restructuring",
		"DOMAIN_RELATION":      "Domain-level relation",
		"WORKS_FOR":            "Works for organization",
		"KEY_PERSON":           "Key person of entity",
		"HAS_KEY_PERSON":       "Entity has key person",
		"ASSURES":              "Assures (audit/certification/regulatory; qualifier assurance_type)",
		"AUDITED_BY":           "Audited by auditor (legacy)",
		"RELATIVE_OF":          "Family relation",
		"RELATED_TO":           "General relation",
		"OPERATES_VIA":         "Operates via intermediary",
		"MEMBER_OF":            "Membership",
		"AFFILIATED_WITH":      "Affiliation",
		"FOCUSES_ON":           "Focus on domain/market",
		"INVOLVES_ACTOR":       "Involves actor",
		"IN_GEO_SCOPE":         "In geographic scope",
		"ABOUT":                "About topic",
		"SURFACES":             "Surfaces in context",
		"RELATED_DOMAIN":       "Related domain",
		"BROADENS_TO":          "Broadens to",
		"NARROWS_TO":           "Narrows to",
		"AFFECTS":              "Affects entity",
		"HAS_SUBGROUP":         "Has subgroup",
		"RENAMED_TO":           "Renamed to",
		"REBRANDED_TO":         "Rebranded to",
		"LEGAL_SUCCESSOR_OF":   "Legal successor",
		"MERGED_INTO":          "Merged into",
		"SPUN_OFF_FROM":        "Spun off from",
		"ACQUIRED_BY":          "Acquired by",
		"ALIGNS_WITH":          "Aligns with (use ASSOCIATED_WITH with subtype)",
		"OPPOSES":              "Opposes (use INFLUENCES negative)",
		"COOPERATES_WITH":      "Cooperates with (use ASSOCIATED_WITH)",
		"CONFLICTS_WITH":       "Conflicts with (use INFLUENCES negative)",
	}
	var lines []string
	for _, t := range types {
		desc := descriptions[t]
		if desc == "" {
			desc = t
		}
		lines = append(lines, "  - "+t+": "+desc)
	}
	return "Allowed relation types (use exact string):\n" + strings.Join(lines, "\n")
}

// GraphIntentGroupingKindDescriptions returns grouping kinds with descriptions.
// kinds: sorted list of allowed grouping kind strings.
func GraphIntentGroupingKindDescriptions(kinds []string) string {
	descriptions := map[string]string{
		"governance_structure":  "Governance hierarchy: ministries, agencies, regulators, ownership",
		"market_map":            "Market structure: segments, sectors, verticals",
		"supply_chain_skeleton": "Supply chain: suppliers, producers, customers",
		"ownership_control":     "Ownership and control structure",
		"investment_flow":       "Investment flows and investors",
		"regulatory_landscape":  "Regulatory framework and actors",
		"societal_structure":    "Societal actors: communities, tribes, dynasties",
	}
	var lines []string
	for _, k := range kinds {
		desc := descriptions[k]
		if desc == "" {
			desc = k
		}
		lines = append(lines, "  - "+k+": "+desc)
	}
	return "Allowed grouping kinds (use exact string):\n" + strings.Join(lines, "\n")
}

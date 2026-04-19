package edgesemantics

import (
	"fmt"
	"sort"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// EdgeDirectionRule defines valid source and target fragment classes for a relation.
//
// Sourced from the AgentHorde taxonomy SoT (ah.RelationEndpointKinds +
// ah.RelationEndpointBlacklist). Hand-maintenance of this table is intentionally
// removed: every change MUST happen in agents/shared/agenthorde_common/.../taxonomy.py
// and propagate via `go generate ./internal/taxonomy/ah/...` (see docs/taxonomy/CODEGEN.md).
type EdgeDirectionRule struct {
	ValidSourceClasses []ah.FragmentClass
	ValidTargetClasses []ah.FragmentClass
	// Pairs is the canonical (from, to) catalog used by validators that need
	// per-pair semantics (e.g. supplies physical_leg vs commercial); populated
	// directly from ah.RelationEndpointKinds.
	Pairs []ah.EndpointKindRow
	// Blacklist enumerates explicitly forbidden (from, to) pairs that overlap
	// the positive rows; mirrors ah.RelationEndpointBlacklist.
	Blacklist []ah.EndpointPair
	// Description is a short human-readable summary used in prompt scaffolding.
	Description string
}

// EdgeDirectionPolicies is the runtime view of the AH endpoint catalog, keyed by
// canonical RelationName. Populated at init() from ah.RelationEndpointKinds.
//
// Relations without rows in the AH catalog (e.g. open-ended influences with
// attribute-driven semantics) still appear here with empty pair lists; callers
// MUST treat empty Pairs as "no closed catalog" and fall back to looser checks.
var EdgeDirectionPolicies = map[ah.RelationName]EdgeDirectionRule{}

func init() {
	for _, rel := range ah.RelationNames {
		rows := ah.EndpointKindRowsFor(rel)
		rule := EdgeDirectionRule{
			Pairs:       rows,
			Blacklist:   ah.RelationEndpointBlacklist[rel],
			Description: relationDescription(rel),
		}
		seenSrc := make(map[ah.FragmentClass]struct{}, len(rows))
		seenDst := make(map[ah.FragmentClass]struct{}, len(rows))
		for _, r := range rows {
			if _, ok := seenSrc[r.From]; !ok {
				seenSrc[r.From] = struct{}{}
				rule.ValidSourceClasses = append(rule.ValidSourceClasses, r.From)
			}
			if _, ok := seenDst[r.To]; !ok {
				seenDst[r.To] = struct{}{}
				rule.ValidTargetClasses = append(rule.ValidTargetClasses, r.To)
			}
		}
		sort.Slice(rule.ValidSourceClasses, func(i, j int) bool {
			return rule.ValidSourceClasses[i] < rule.ValidSourceClasses[j]
		})
		sort.Slice(rule.ValidTargetClasses, func(i, j int) bool {
			return rule.ValidTargetClasses[i] < rule.ValidTargetClasses[j]
		})
		EdgeDirectionPolicies[rel] = rule
	}
}

// relationDescription returns a short human-readable summary for prompt scaffolding.
// Curated for the closed AH relation set; new relations land with empty descriptions
// and SHOULD be filled here as part of the SoT change that introduces them.
var relationDescriptionTable = map[ah.RelationName]string{
	ah.RelationNameAcquiredStakeFrom:  "Acquirer acquired equity stake from divestor (org -> org).",
	ah.RelationNameAlternateTo:        "Symmetric alternative-of relation (route/site/vessel-class).",
	ah.RelationNameAssetContribution:  "Asset-in-kind contribution between orgs.",
	ah.RelationNameAssures:            "Independent assurance (audit/certification/regulatory) from org to org/site.",
	ah.RelationNameBenchmarkedTo:      "Product/route/site benchmarked to a market price reference.",
	ah.RelationNameBeneficiallyOwns:   "Beneficial ownership stake (person/org/community -> org).",
	ah.RelationNameCharters:           "Org charters a vessel-class (commercial fleet usage).",
	ah.RelationNameConstrainedBy:      "Route/site/vessel-class constrained by geo or another route.",
	ah.RelationNameControlsVia:        "Non-equity control of an org by another actor.",
	ah.RelationNameDisrupts:           "Evidence/geo disrupts site/route/org/asset (event-time semantics).",
	ah.RelationNameDivestedTo:         "Divestor divested asset to acquirer (org -> org).",
	ah.RelationNameEnactedVia:         "Decision enacted via a specific mechanism.",
	ah.RelationNameExposesTo:          "Endpoint exposed to a regulatory regime.",
	ah.RelationNameGatekeeps:          "Org gatekeeps a strategic decision point.",
	ah.RelationNameHoldsBoardSeat:     "Director-style board representation.",
	ah.RelationNameImposes:            "Mechanism imposes constraint on entity.",
	ah.RelationNameIncorporatedIn:     "Org legally incorporated in geo.",
	ah.RelationNameInfluences:         "Open-ended influence (attribute-driven kind).",
	ah.RelationNameInvestedIn:         "Equity investment org -> org.",
	ah.RelationNameJointVentureWith:   "Joint venture between orgs (symmetric in semantics; directed in storage).",
	ah.RelationNameLegConnects:        "Physical supply-chain leg topology between sites/routes/regions.",
	ah.RelationNameListedOn:           "Org listed on a market/exchange.",
	ah.RelationNameManagedBy:          "Operational management contract (org managed by org).",
	ah.RelationNameMemberOf:           "Person/org member of community/alliance.",
	ah.RelationNameMergedInto:         "Org merged into successor org.",
	ah.RelationNameMitigatedBy:        "Regime exposure mitigated by org/route/regime chain.",
	ah.RelationNameOperates:           "Org operates a site (operator-of-record).",
	ah.RelationNamePassesThrough:      "Route/site/vessel-class passes through corridor/region.",
	ah.RelationNamePricedUnder:        "Product/route/site priced under a market or contract regime.",
	ah.RelationNameProduces:           "Org produces product line (or via attributes.production_kind: extraction etc).",
	ah.RelationNamePublishesBenchmark: "Org publishes price reference benchmark on a market.",
	ah.RelationNameRegulates:          "Regulator (org) regulates region/market/route/site.",
	ah.RelationNameRequiresCapability: "Product requires site capability.",
	ah.RelationNameSpunOut:            "Org spun out subsidiary org.",
	ah.RelationNameSubjectTo:          "Endpoint subject to a regulatory regime.",
	ah.RelationNameSubsidiaryOf:       "Org is subsidiary of parent org.",
	ah.RelationNameSupplies:           "Commercial / actor-to-market / trade-flow / physical-leg supply relation.",
	ah.RelationNameTransshipsAt:       "Cargo transshipment at a hub (site/route/org -> site).",
	ah.RelationNameWields:             "Org wields a control mechanism.",
}

func relationDescription(rel ah.RelationName) string {
	if d, ok := relationDescriptionTable[rel]; ok {
		return d
	}
	return ""
}

// ValidateEdgeDirection reports (valid, shouldReverse, rule) for the given edge.
//
// Behaviour:
//   - Unknown relation: returns (false, false, &EdgeDirectionRule{Description: "unknown relation ..."}).
//   - Open-ended relation (no rows in AH catalog): returns (true, false, &rule).
//   - Pair listed in blacklist: returns (false, false, &rule).
//   - Pair found among rows: returns (true, false, &rule).
//   - Reversed pair found: returns (false, true, &rule).
//   - Otherwise: returns (false, false, &rule).
func ValidateEdgeDirection(rel ah.RelationName, src, dst ah.FragmentClass) (bool, bool, *EdgeDirectionRule) {
	rule, exists := EdgeDirectionPolicies[rel]
	if !exists {
		return false, false, &EdgeDirectionRule{
			Description: fmt.Sprintf("unknown relation %s: missing direction policy", rel),
		}
	}

	for _, b := range rule.Blacklist {
		if b.From == src && b.To == dst {
			return false, false, &rule
		}
	}

	if len(rule.Pairs) == 0 {
		return true, false, &rule
	}

	for _, p := range rule.Pairs {
		if p.From == src && p.To == dst {
			return true, false, &rule
		}
	}
	for _, p := range rule.Pairs {
		if p.From == dst && p.To == src {
			return false, true, &rule
		}
	}
	return false, false, &rule
}

package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// RegulatoryImpactStrictValidator applies deterministic edge-type × path-prefix rules for policy impact graphs.
type RegulatoryImpactStrictValidator struct{}

// ValidateRelations returns relations with invalid structural pairs damped (confidence scaled down).
func (v RegulatoryImpactStrictValidator) ValidateRelations(rels []agents.DiscoveredRelation) []agents.DiscoveredRelation {
	if len(rels) == 0 {
		return rels
	}
	out := make([]agents.DiscoveredRelation, len(rels))
	for i, rel := range rels {
		out[i] = rel
		if ok, _ := regulatoryImpactEdgeStructurallyValid(rel); ok {
			continue
		}
		r := rel
		if r.Confidence > 0.35 {
			r.Confidence *= 0.45
		} else {
			r.Confidence *= 0.7
		}
		if strings.TrimSpace(r.Reason) == "" {
			r.Reason = "regulatory_strict_validator:prefix_mismatch"
		} else {
			r.Reason = r.Reason + ";regulatory_strict_validator:prefix_mismatch"
		}
		out[i] = r
	}
	return out
}

func regulatoryImpactEdgeStructurallyValid(rel agents.DiscoveredRelation) (bool, string) {
	src := strings.TrimSpace(rel.SourceCanonicalPath)
	tgt := strings.TrimSpace(rel.TargetCanonicalPath)
	rt := strings.TrimSpace(taxonomy.NormalizeRelationType(rel.RelationType))
	if tgt == "" {
		return false, "empty_target"
	}
	dt := models.DependencyType(strings.ToUpper(rt))
	switch dt {
	case models.DependencyTypeRegulates:
		return prefixAny(src, "/gov/") && prefixAny(tgt, "/org/", "/fac/", "/market/", "/product/", "/community/"), "regulates"
	case models.DependencyTypeControlsAccess:
		return prefixAny(src, "/gov/") && prefixAny(tgt, "/org/", "/fac/", "/market/", "/product/", "/community/", "/program/"), "controls_access"
	case models.DependencyTypeGatekeepsDecision:
		return prefixAny(src, "/gov/", "/org/") && strings.HasPrefix(tgt, "/decision/"), "gatekeeps_decision"
	case models.DependencyTypeUsesMechanism:
		return strings.HasPrefix(src, "/decision/") && strings.HasPrefix(tgt, "/mechanism/"), "uses_mechanism"
	case models.DependencyTypeAppliesTo:
		return strings.HasPrefix(src, "/mechanism/") && prefixAny(tgt, "/market/", "/org/", "/fac/", "/product/", "/community/", "/program/"), "applies_to"
	case models.DependencyTypeImposesCondition:
		return prefixAny(src, "/mechanism/", "/gov/", "/program/") && prefixAny(tgt, "/org/", "/fac/", "/market/", "/product/", "/community/", "/program/"), "imposes_condition"
	case models.DependencyTypeInGeoScope:
		return prefixAny(src, "/mechanism/", "/decision/", "/program/", "/gov/") && prefixAny(tgt, "/loc/"), "in_geo_scope"
	case models.DependencyTypeAffects:
		return prefixAny(src, "/mechanism/", "/program/", "/gov/", "/community/", "/market/") && prefixAny(tgt, "/org/", "/fac/", "/market/", "/product/", "/community/", "/program/", "/loc/", "/media/"), "affects"
	case models.DependencyTypeInfluences:
		return src != "" && tgt != "", "influences"
	case models.DependencyTypeAdministers:
		return prefixAny(src, "/gov/", "/org/") && prefixAny(tgt, "/program/", "/org/"), "administers"
	case models.DependencyTypeHasSubgroup:
		return strings.HasPrefix(src, "/community/") && strings.HasPrefix(tgt, "/community/"), "has_subgroup"
	case models.DependencyTypeMemberOf:
		return strings.HasPrefix(src, "/person/") && strings.HasPrefix(tgt, "/community/"), "member_of"
	case models.DependencyTypeAffiliatedWith:
		return prefixAny(src, "/community/", "/person/") && prefixAny(tgt, "/org/", "/gov/", "/program/"), "affiliated_with"
	case models.DependencyTypeOperatesVia:
		return prefixAny(src, "/community/", "/org/") && prefixAny(tgt, "/media/"), "operates_via"
	default:
		return true, "unlisted_type_ok"
	}
}

func prefixAny(p string, prefixes ...string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	for _, pre := range prefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
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
	rt := normalizeRelationToken(rel.RelationType)
	if tgt == "" {
		return false, "empty_target"
	}
	dt := DependencyType(rt)
	switch dt {
	case ah.RelationNameRegulates:
		return prefixAny(src, "/gov/") && prefixAny(tgt, "/org/", "/fac/", "/market/", "/product/", "/community/"), "regulates"
	case DependencyType("controls_access"):
		return prefixAny(src, "/gov/") && prefixAny(tgt, "/org/", "/fac/", "/market/", "/product/", "/community/", "/program/"), "controls_access"
	case ah.RelationNameGatekeeps:
		return prefixAny(src, "/gov/", "/org/") && strings.HasPrefix(tgt, "/decision/"), "gatekeeps_decision"
	case ah.RelationNameWields:
		return strings.HasPrefix(src, "/decision/") && strings.HasPrefix(tgt, "/mechanism/"), "uses_mechanism"
	case DependencyType("applies_to"):
		return strings.HasPrefix(src, "/mechanism/") && prefixAny(tgt, "/market/", "/org/", "/fac/", "/product/", "/community/", "/program/"), "applies_to"
	case ah.RelationNameImposes:
		return prefixAny(src, "/mechanism/", "/gov/", "/program/") && prefixAny(tgt, "/org/", "/fac/", "/market/", "/product/", "/community/", "/program/"), "imposes_condition"
	case DependencyType("in_geo_scope"):
		return prefixAny(src, "/mechanism/", "/decision/", "/program/", "/gov/") && prefixAny(tgt, "/loc/"), "in_geo_scope"
	case ah.RelationNameDisrupts:
		return prefixAny(src, "/mechanism/", "/program/", "/gov/", "/community/", "/market/") && prefixAny(tgt, "/org/", "/fac/", "/market/", "/product/", "/community/", "/program/", "/loc/", "/media/"), "affects"
	case ah.RelationNameInfluences:
		return src != "" && tgt != "", "influences"
	case ah.RelationNameOperates:
		return prefixAny(src, "/gov/", "/org/") && prefixAny(tgt, "/program/", "/org/"), "administers"
	case DependencyType("has_subgroup"):
		return strings.HasPrefix(src, "/community/") && strings.HasPrefix(tgt, "/community/"), "has_subgroup"
	case ah.RelationNameMemberOf:
		return strings.HasPrefix(src, "/person/") && strings.HasPrefix(tgt, "/community/"), "member_of"
	case DependencyType("affiliated_with"):
		return prefixAny(src, "/community/", "/person/") && prefixAny(tgt, "/org/", "/gov/", "/program/"), "affiliated_with"
	case DependencyType("operates_via"):
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

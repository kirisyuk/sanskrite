package entity_semantics

import (
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// EvaluateMode specifies how to evaluate rules.
const (
	EvaluateModeFull = "full" // evaluate all triggers
	EvaluateModeSeed = "seed" // only seed rules and rules that fire for empty fragment
)

// EvaluateMissingInfoRules evaluates rules and returns missingInfo questions.
// relationTypes: uppercase relation type strings (e.g. PRODUCES, HAS_MARKET_SEGMENT).
// mode: "full" for full evaluation, "seed" for synthetic seed (only TriggerSeed + rules firing for empty state).
func EvaluateMissingInfoRules(
	fragment *models.HorizonFragment,
	relationTypes []string,
	rules []MissingInfoRule,
	domainModes []string,
	targetPath string,
	mode string,
) []string {
	relSet := make(map[string]bool)
	for _, rt := range relationTypes {
		rt = strings.TrimSpace(rt)
		if rt == "" {
			continue
		}
		if rel, ok := ah.NormalizeRelation(rt); ok {
			relSet[string(rel)] = true
			continue
		}
		if dec, ok := models.NormalizeRelationName(rt); ok && !dec.Dropped && dec.Relation != "" {
			relSet[string(dec.Relation)] = true
			continue
		}
	}
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	domainSet := make(map[string]bool)
	for _, m := range domainModes {
		m = strings.TrimSpace(strings.ToLower(m))
		if m != "" {
			domainSet[m] = true
		}
	}

	var out []string
	seen := make(map[string]struct{})

	for _, r := range rules {
		// Filter by DomainMode: rule applies if DomainMode is empty (general) or in active domainModes
		if r.DomainMode != "" && !domainSet[strings.ToLower(r.DomainMode)] {
			continue
		}

		var fires bool
		switch r.Trigger {
		case TriggerSeed:
			fires = mode == EvaluateModeSeed
		case TriggerRelationMissing:
			if len(r.RelationTypes) > 0 {
				// All must be missing
				allMissing := true
				for _, rt := range r.RelationTypes {
					if relSet[normalizeRuleRelationType(rt)] {
						allMissing = false
						break
					}
				}
				fires = allMissing
			} else {
				fires = !relSet[normalizeRuleRelationType(r.RelationType)]
			}
			// EXTRACTS_FROM for supply chain: only when path suggests extractive
			if fires && normalizeRuleRelationType(r.RelationType) == "extracts_from" && r.DomainMode == taxonomy.DomainModeSupplyChain {
				path := targetPath
				if fragment != nil && fragment.CanonicalPath != "" {
					path = fragment.CanonicalPath
				}
				if !isMiningOrExtractive(path) {
					fires = false
				}
			}
		case TriggerFieldEmpty:
			fires = fragment != nil && isFragmentFieldEmpty(fragment, r.FieldPath)
		case TriggerRelationCountBelow:
			fires = len(relationTypes) < r.MinCount
		default:
			continue
		}

		if !fires {
			continue
		}

		q := fmt.Sprintf(r.QuestionTemplate, targetPath)
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if _, ok := seen[q]; ok {
			continue
		}
		seen[q] = struct{}{}
		out = append(out, q)
	}
	return out
}

func normalizeRuleRelationType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if rel, ok := ah.NormalizeRelation(raw); ok {
		return string(rel)
	}
	if dec, ok := models.NormalizeRelationName(raw); ok && !dec.Dropped && dec.Relation != "" {
		return string(dec.Relation)
	}
	return strings.ToLower(raw)
}

// isFragmentFieldEmpty returns true if the field at fieldPath is empty.
// Supported paths: Labels.en, MarketDomain, MarketVertical, OrgSize, CommunityType,
// Identifiers.legalName, HomeLocation, RadiusKm, State./metric/market/tam_usd, etc.
func isFragmentFieldEmpty(f *models.HorizonFragment, fieldPath string) bool {
	if f == nil {
		return true
	}
	fieldPath = strings.TrimSpace(fieldPath)
	if fieldPath == "" {
		return false
	}

	switch fieldPath {
	case "Labels.en":
		return f.Labels == nil || strings.TrimSpace(f.Labels["en"]) == ""
	case "MarketDomain":
		return strings.TrimSpace(f.MarketDomain) == ""
	case "MarketVertical":
		return strings.TrimSpace(f.MarketVertical) == ""
	case "OrgSize":
		return strings.TrimSpace(f.OrgSize) == "" || strings.ToLower(f.OrgSize) == "na"
	case "CommunityType":
		return strings.TrimSpace(f.CommunityType) == ""
	case "Identifiers.legalName":
		if f.Identifiers == nil {
			return true
		}
		return strings.TrimSpace(f.Identifiers["legalName"]) == ""
	case "HomeLocation":
		return f.HomeLocation == nil
	case "RadiusKm":
		return f.RadiusKm <= 0
	case "MarketSegments":
		return len(f.MarketSegments) == 0
	default:
		if strings.HasPrefix(fieldPath, "State.") {
			key := strings.TrimPrefix(fieldPath, "State.")
			if f.State == nil {
				return true
			}
			v, ok := f.State[key]
			if !ok {
				return true
			}
			// MetricValue.Value is float64; 0 or unset means empty for numeric metrics
			return v.Value == 0
		}
	}
	return false
}

func isMiningOrExtractive(path string) bool {
	p := strings.ToLower(path)
	return strings.Contains(p, "/mining/") || strings.Contains(p, "/extractive/") ||
		strings.Contains(p, "/mineral/") || strings.Contains(p, "/deposit/")
}

// MissingInfoRulesForPath returns MissingInfoRules for the entity at
// the given path. `class` and `subtype` are AH hints used when path
// prefix resolution doesn't bind.
func MissingInfoRulesForPath(canonicalPath string, class ah.FragmentClass, subtype string, communityType string) []MissingInfoRule {
	def := GetDefinitionByPath(canonicalPath, class, subtype, communityType)
	if def == nil || len(def.MissingInfoRules) == 0 {
		return nil
	}
	return def.MissingInfoRules
}

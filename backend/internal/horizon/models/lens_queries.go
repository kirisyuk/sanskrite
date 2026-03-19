package models

import (
	"encoding/json"
	"strings"
)

// LensDomainQuery is the first-class lens-level record of a domain query resolved from the user's prompt.
// It preserves the executable contract that downstream jobs and page enrichers should use when interpreting the lens.
type LensDomainQuery struct {
	CanonicalPath string `json:"canonicalPath" firestore:"canonicalPath"`
	ContractType  string `json:"contractType" firestore:"contractType"`
	Title         string `json:"title" firestore:"title"`
	Description   string `json:"description" firestore:"description"`

	Facets             map[string][]string `json:"facets,omitempty" firestore:"facets,omitempty"`
	Projections        []string            `json:"projections,omitempty" firestore:"projections,omitempty"`
	RelatedDomains     []string            `json:"relatedDomains,omitempty" firestore:"relatedDomains,omitempty"`
	AffectedDomains    []string            `json:"affectedDomains,omitempty" firestore:"affectedDomains,omitempty"`
	FirstOrderEntities []string            `json:"firstOrderEntities,omitempty" firestore:"firstOrderEntities,omitempty"`
	Confidence         float64             `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	Reason             string              `json:"reason,omitempty" firestore:"reason,omitempty"`

	SupplyChainExecutionPlan      *LensSupplyChainExecutionPlan      `json:"supplyChainExecutionPlan,omitempty" firestore:"supplyChainExecutionPlan,omitempty"`
	RegulatoryImpactExecutionPlan *LensRegulatoryImpactExecutionPlan `json:"regulatoryImpactExecutionPlan,omitempty" firestore:"regulatoryImpactExecutionPlan,omitempty"`
	GovernanceExecutionPlan       *LensGovernanceExecutionPlan       `json:"governanceExecutionPlan,omitempty" firestore:"governanceExecutionPlan,omitempty"`
	EcosystemExecutionPlan        *LensEcosystemExecutionPlan        `json:"ecosystemExecutionPlan,omitempty" firestore:"ecosystemExecutionPlan,omitempty"`
}

type LensSupplyChainExecutionPlan struct {
	GraphBuildMode string                        `json:"graphBuildMode,omitempty" firestore:"graphBuildMode,omitempty"`
	Queries        []LensSupplyChainQuery        `json:"queries,omitempty" firestore:"queries,omitempty"`
	OverlayQueries []LensSupplyChainOverlayQuery `json:"overlayQueries,omitempty" firestore:"overlayQueries,omitempty"`
}

type LensSupplyChainQuery struct {
	Anchor        string   `json:"anchor,omitempty" firestore:"anchor,omitempty"`
	Anchors       []string `json:"anchors,omitempty" firestore:"anchors,omitempty"`
	AnchorType    string   `json:"anchorType,omitempty" firestore:"anchorType,omitempty"`
	Direction     string   `json:"direction,omitempty" firestore:"direction,omitempty"`
	Depth         int      `json:"depth,omitempty" firestore:"depth,omitempty"`
	Level         int      `json:"level,omitempty" firestore:"level,omitempty"`
	ProductFilter []string `json:"productFilter,omitempty" firestore:"productFilter,omitempty"`
	GeoFilter     []string `json:"geoFilter,omitempty" firestore:"geoFilter,omitempty"`
	Reason        string   `json:"reason,omitempty" firestore:"reason,omitempty"`
}

type LensSupplyChainOverlayQuery struct {
	Type    string   `json:"type,omitempty" firestore:"type,omitempty"`
	Anchors []string `json:"anchors,omitempty" firestore:"anchors,omitempty"`
	Scope   string   `json:"scope,omitempty" firestore:"scope,omitempty"`
}

type LensRegulatoryImpactExecutionPlan struct {
	Queries []LensRegulatoryImpactQuery `json:"queries,omitempty" firestore:"queries,omitempty"`
}

type LensRegulatoryImpactQuery struct {
	Instrument   string            `json:"instrument,omitempty" firestore:"instrument,omitempty"`
	Jurisdiction string            `json:"jurisdiction,omitempty" firestore:"jurisdiction,omitempty"`
	Parameters   map[string]string `json:"parameters,omitempty" firestore:"parameters,omitempty"`
	ImpactScope  string            `json:"impactScope,omitempty" firestore:"impactScope,omitempty"`
	Depth        int               `json:"depth,omitempty" firestore:"depth,omitempty"`
	SectorFilter []string          `json:"sectorFilter,omitempty" firestore:"sectorFilter,omitempty"`
	Reason       string            `json:"reason,omitempty" firestore:"reason,omitempty"`
}

type LensGovernanceExecutionPlan struct {
	Queries []LensGovernanceQuery `json:"queries,omitempty" firestore:"queries,omitempty"`
}

type LensGovernanceQuery struct {
	Target       string   `json:"target,omitempty" firestore:"target,omitempty"`
	TargetType   string   `json:"targetType,omitempty" firestore:"targetType,omitempty"`
	Direction    string   `json:"direction,omitempty" firestore:"direction,omitempty"`
	Depth        int      `json:"depth,omitempty" firestore:"depth,omitempty"`
	IncludeBoard bool     `json:"includeBoard,omitempty" firestore:"includeBoard,omitempty"`
	GeoFilter    []string `json:"geoFilter,omitempty" firestore:"geoFilter,omitempty"`
	Reason       string   `json:"reason,omitempty" firestore:"reason,omitempty"`
}

type LensEcosystemExecutionPlan struct {
	Queries []LensEcosystemQuery `json:"queries,omitempty" firestore:"queries,omitempty"`
}

type LensEcosystemQuery struct {
	Focus        string   `json:"focus,omitempty" firestore:"focus,omitempty"`
	FocusType    string   `json:"focusType,omitempty" firestore:"focusType,omitempty"`
	Depth        int      `json:"depth,omitempty" firestore:"depth,omitempty"`
	ActorTypes   []string `json:"actorTypes,omitempty" firestore:"actorTypes,omitempty"`
	SectorFilter []string `json:"sectorFilter,omitempty" firestore:"sectorFilter,omitempty"`
	GeoFilter    []string `json:"geoFilter,omitempty" firestore:"geoFilter,omitempty"`
	Reason       string   `json:"reason,omitempty" firestore:"reason,omitempty"`
}

func NormalizeLensDomainQueries(in []LensDomainQuery) []LensDomainQuery {
	if len(in) == 0 {
		return nil
	}
	out := make([]LensDomainQuery, 0, len(in))
	for _, item := range in {
		item.CanonicalPath = strings.TrimSpace(item.CanonicalPath)
		item.ContractType = strings.TrimSpace(item.ContractType)
		item.Title = strings.TrimSpace(item.Title)
		item.Description = strings.TrimSpace(item.Description)
		item.Reason = strings.TrimSpace(item.Reason)
		item.Projections = compactStringSlice(item.Projections)
		item.RelatedDomains = compactStringSlice(item.RelatedDomains)
		item.AffectedDomains = compactStringSlice(item.AffectedDomains)
		item.FirstOrderEntities = compactStringSlice(item.FirstOrderEntities)
		item.Facets = compactFacetMap(item.Facets)
		if item.CanonicalPath == "" && item.ContractType == "" && item.Title == "" && item.Description == "" && len(item.Facets) == 0 {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ParseLensDomainQueries(raw interface{}) []LensDomainQuery {
	if raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case []LensDomainQuery:
		return NormalizeLensDomainQueries(t)
	case []*LensDomainQuery:
		out := make([]LensDomainQuery, 0, len(t))
		for _, item := range t {
			if item == nil {
				continue
			}
			out = append(out, *item)
		}
		return NormalizeLensDomainQueries(out)
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []LensDomainQuery
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	return NormalizeLensDomainQueries(out)
}

func LensDomainQueriesToMaps(in []LensDomainQuery) []map[string]interface{} {
	normalized := NormalizeLensDomainQueries(in)
	if len(normalized) == 0 {
		return nil
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil
	}
	var out []map[string]interface{}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	return out
}

func compactStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactFacetMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if compact := compactStringSlice(values); len(compact) > 0 {
			out[key] = compact
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

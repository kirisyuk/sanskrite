package horizon

import (
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type scopeScoutDirectedExplorationRequest struct {
	DomainMode          string   `json:"domainMode,omitempty"`
	Direction           string   `json:"direction,omitempty"`
	HotPathOnly         bool     `json:"hotPathOnly,omitempty"`
	HotPathNodeIDs      []string `json:"hotPathNodeIds,omitempty"`
	IncludeCapabilities bool     `json:"includeCapabilities,omitempty"`
	IncludeEdgeMetrics  bool     `json:"includeEdgeMetrics,omitempty"`
	CustomQuery         string   `json:"customQuery,omitempty"`
	RenderTarget        string   `json:"renderTarget,omitempty"`
	IgnoreFreshness     bool     `json:"ignoreFreshness,omitempty"`
}

func normalizeDirectedExploration(raw *scopeScoutDirectedExplorationRequest) (*horizonmodels.DirectedExplorationConfig, error) {
	if raw == nil {
		return nil, nil
	}
	domainMode := strings.TrimSpace(strings.ToLower(raw.DomainMode))
	if domainMode == "" {
		domainMode = taxonomy.DomainModeSupplyChain
	}
	if domainMode != taxonomy.DomainModeSupplyChain {
		return nil, fmt.Errorf("directed exploration supports only domainMode=supply_chain")
	}

	direction := strings.TrimSpace(strings.ToLower(raw.Direction))
	switch direction {
	case "up", "upstream":
		direction = "upstream"
	case "down", "downstream":
		direction = "downstream"
	default:
		return nil, fmt.Errorf("directed exploration direction must be upstream or downstream")
	}

	renderTarget := strings.TrimSpace(strings.ToLower(raw.RenderTarget))
	switch renderTarget {
	case "", "graph":
		renderTarget = "graph"
	case "map":
	default:
		return nil, fmt.Errorf("directed exploration renderTarget must be graph or map")
	}

	hotPathNodeIDs := make([]string, 0, len(raw.HotPathNodeIDs))
	seenIDs := make(map[string]struct{}, len(raw.HotPathNodeIDs))
	for _, rawID := range raw.HotPathNodeIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, ok := seenIDs[id]; ok {
			continue
		}
		seenIDs[id] = struct{}{}
		hotPathNodeIDs = append(hotPathNodeIDs, id)
	}

	return &horizonmodels.DirectedExplorationConfig{
		DomainMode:          domainMode,
		Direction:           direction,
		HotPathOnly:         raw.HotPathOnly,
		HotPathNodeIDs:      hotPathNodeIDs,
		IncludeCapabilities: raw.IncludeCapabilities,
		IncludeEdgeMetrics:  raw.IncludeEdgeMetrics,
		CustomQuery:         strings.TrimSpace(raw.CustomQuery),
		RenderTarget:        renderTarget,
		IgnoreFreshness:     raw.IgnoreFreshness,
	}, nil
}

func isAllowedSupplyChainAnchorPath(canonicalPath string) bool {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	return strings.HasPrefix(cp, "/org/") ||
		strings.HasPrefix(cp, "/gov/") ||
		strings.HasPrefix(cp, "/fac/") ||
		strings.HasPrefix(cp, "/product/") ||
		strings.HasPrefix(cp, "/deposit/")
}


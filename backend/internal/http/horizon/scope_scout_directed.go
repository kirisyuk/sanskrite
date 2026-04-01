package horizon

import (
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonsupplychain "github.com/igorkirisyuk/sanskrite/internal/horizon/supplychain"
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

func isAllowedOneHopSupplyChainAnchorPath(canonicalPath string) bool {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	return strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/fac/")
}

func oneHopAnchorTypeForPath(canonicalPath string) string {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	switch {
	case strings.HasPrefix(cp, "/org/"):
		return "ORG"
	case strings.HasPrefix(cp, "/fac/"):
		return "FAC"
	default:
		return ""
	}
}

func shouldRouteDirectedExplorationToOneHop(targetCanonicalPath string, cfg *horizonmodels.DirectedExplorationConfig) bool {
	if cfg == nil {
		return false
	}
	// Only explicit hot-path-only downstream requests should collapse to one-hop.
	// Full directed exploration must stay on the recursive runtime so the graph can
	// continue through consumer branches and longer downstream cascades.
	if strings.TrimSpace(strings.ToLower(cfg.DomainMode)) != taxonomy.DomainModeSupplyChain {
		return false
	}
	if strings.TrimSpace(strings.ToLower(cfg.Direction)) != "downstream" {
		return false
	}
	if !cfg.HotPathOnly {
		return false
	}
	return isAllowedOneHopSupplyChainAnchorPath(targetCanonicalPath)
}

func buildOneHopOptionsForDirectedExploration(targetCanonicalPath string) horizonsupplychain.OneHopDownstreamOptions {
	return horizonsupplychain.NormalizeOptions(horizonsupplychain.OneHopDownstreamOptions{
		AnchorRef:             strings.TrimSpace(targetCanonicalPath),
		AnchorType:            oneHopAnchorTypeForPath(targetCanonicalPath),
		MaxBranchesPerProduct: 12,
		IncludeInputsContext:  true,
		AllowOrgFallback:      true,
		AllowLLMFallback:      true,
	})
}

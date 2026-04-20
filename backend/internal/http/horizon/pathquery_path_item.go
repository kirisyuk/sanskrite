package horizon

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// pathqueryPathItem is the stable JSON shape for one entry in GET …/horizon/pathquery `paths`.
// It deliberately omits persistence fields (tenantId, createdAt, …) from MaterializedArtifact.
type pathqueryPathItem struct {
	ID          string                                    `json:"id"`
	Kind        string                                    `json:"kind"`
	Payload     map[string]interface{}                    `json:"payload"`
	DisplayName string                                    `json:"displayName,omitempty"`
	NodeRefs    []string                                  `json:"nodeRefs,omitempty"`
	RiskOverlay *horizonmodels.SupplyChainPathRiskOverlay `json:"riskOverlay,omitempty"`
}

func pathqueryPathItemsFromArtifacts(arts []*horizonmodels.MaterializedArtifact) []pathqueryPathItem {
	if len(arts) == 0 {
		return nil
	}
	out := make([]pathqueryPathItem, 0, len(arts))
	for _, a := range arts {
		if a == nil || a.Payload == nil {
			continue
		}
		item := pathqueryPathItem{
			ID:          strings.TrimSpace(a.ID),
			Kind:        string(a.Kind),
			Payload:     a.Payload,
			DisplayName: strings.TrimSpace(a.DisplayName),
		}
		if len(a.NodeRefs) > 0 {
			item.NodeRefs = append([]string(nil), a.NodeRefs...)
		}
		if a.RiskOverlay != nil {
			ro := *a.RiskOverlay
			item.RiskOverlay = &ro
		}
		out = append(out, item)
	}
	return out
}

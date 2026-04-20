package pathquery

import (
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathstress"
)

func enrichArtifactsInPlace(paths []*horizonmodels.MaterializedArtifact, includeStress, includeGeo bool) {
	if !includeStress && !includeGeo {
		return
	}
	eval := pathstress.NewEvaluator()
	for _, a := range paths {
		if a == nil {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(a.Payload)
		if err != nil || parsed == nil {
			continue
		}
		if includeStress {
			for i := range parsed.Legs {
				parsed.Legs[i].Stress = eval.EvaluateLeg(parsed.Legs[i])
			}
			parsed.Stress = eval.EvaluatePath(parsed.Legs)
			det := horizonmodels.BuildSupplyChainPathPayloadDetailed(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Family, parsed.Notes)
			a.Payload = det
		}
		if includeGeo {
			_ = eval // P8 placeholder for route/geo enrichers
		}
	}
}

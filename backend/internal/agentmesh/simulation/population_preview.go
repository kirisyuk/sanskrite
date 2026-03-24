package simulation

import horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"

func PreviewPopulationClasses(run *horizonmodels.SimulationRun, worldIndex int) []string {
	world := &horizonmodels.SimulationWorld{WorldIndex: worldIndex}
	raw := fallbackPopulation(run, world)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		class := firstString(item, "agentClass")
		if class == "" {
			continue
		}
		if _, ok := seen[class]; ok {
			continue
		}
		seen[class] = struct{}{}
		out = append(out, class)
	}
	return out
}

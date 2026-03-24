package simulation

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func runScenarioAnchorPaths(run *horizonmodels.SimulationRun) []string {
	if run == nil {
		return nil
	}
	paths := append([]string{}, run.Request.SeedPaths...)
	if run.Request.Signal != nil {
		paths = append(paths, run.Request.Signal.SeedPaths...)
		paths = append(paths, run.Request.Signal.ActorPaths...)
		paths = append(paths, run.Request.Signal.LocationPath)
	}
	recognition := mapValue(run.Recognition)
	for _, candidate := range recognitionObjectCandidates(recognition["objectCandidates"]) {
		if floatValue(candidate, "score", 0) < 0.35 {
			continue
		}
		paths = append(paths, firstString(candidate, "path"))
	}
	return compactStrings(paths)
}

func runHasAnyScenarioPath(run *horizonmodels.SimulationRun, want ...string) bool {
	if run == nil {
		return false
	}
	have := map[string]struct{}{}
	for _, path := range runScenarioAnchorPaths(run) {
		have[strings.TrimSpace(path)] = struct{}{}
	}
	for _, path := range compactStrings(want) {
		if _, ok := have[path]; ok {
			return true
		}
	}
	return false
}

func runHasScenarioPathSet(run *horizonmodels.SimulationRun, want []string, minCount int) bool {
	if run == nil || minCount <= 0 {
		return false
	}
	have := map[string]struct{}{}
	for _, path := range runScenarioAnchorPaths(run) {
		have[strings.TrimSpace(path)] = struct{}{}
	}
	count := 0
	for _, path := range compactStrings(want) {
		if _, ok := have[path]; ok {
			count++
			if count >= minCount {
				return true
			}
		}
	}
	return false
}

func runQueryContainsAll(run *horizonmodels.SimulationRun, tokens ...string) bool {
	if run == nil {
		return false
	}
	corpus := strings.ToLower(strings.TrimSpace(run.Request.Question) + " | " + strings.TrimSpace(run.Request.Mode))
	if run.Request.Signal != nil {
		corpus += " | " + strings.ToLower(strings.TrimSpace(run.Request.Signal.Text))
		corpus += " | " + strings.ToLower(strings.TrimSpace(run.Request.Signal.Type))
	}
	for _, token := range compactStrings(tokens) {
		if !strings.Contains(corpus, strings.ToLower(token)) {
			return false
		}
	}
	return true
}

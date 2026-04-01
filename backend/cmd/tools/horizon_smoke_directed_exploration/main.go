package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	util "github.com/igorkirisyuk/sanskrite/cmd/tools/internal/directedexplorationutil"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type scenarioAttemptResult struct {
	Scenario                string            `json:"scenario"`
	AnchorKind              string            `json:"anchorKind"`
	Attempt                 int               `json:"attempt"`
	Anchor                  util.AnchorSample `json:"anchor"`
	LensID                  string            `json:"lensId,omitempty"`
	JobID                   string            `json:"jobId,omitempty"`
	DurationMs              int64             `json:"durationMs"`
	DirectedCompleted       bool              `json:"directedCompleted"`
	ToolRuns                []map[string]any  `json:"toolRuns,omitempty"`
	HotPathChainsN          int               `json:"hotPathChainsN"`
	MaterializedPaths       int               `json:"materializedPaths"`
	MaterializedSupplyLinks int               `json:"materializedSupplyLinks"`
	DiscoveredNodes         int               `json:"discoveredNodes"`
	DiscoveredEdges         int               `json:"discoveredEdges"`
	Success                 bool              `json:"success"`
	Error                   string            `json:"error,omitempty"`
}

func main() {
	projectID := flag.String("project", "nv-horizon", "GCP / Firestore project id")
	jobProjectID := flag.String("job-project", "codex-directed-exploration-smoke", "Lens/project id used for smoke lenses")
	attempts := flag.Int("attempts", 3, "Max anchors to try per scenario")
	sampleLimit := flag.Int("sample-limit", 12, "How many anchors to sample per kind")
	includeFull := flag.Bool("include-full", false, "Also run slower full directed-enrichment scenarios")
	scenarioFilter := flag.String("scenario", "", "Run only a specific scenario name")
	flag.Parse()

	ctx := context.Background()
	runtime, err := util.NewLocalRuntime(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("runtime init failed: %v", err)
	}
	defer runtime.Close()

	scenarios := []util.DirectedScenario{
		{Name: "org-downstream-hot-path", AnchorKind: "org", Direction: "downstream", HotPathOnly: true, MaxRecursionDepth: 1},
		{Name: "org-upstream-hot-path", AnchorKind: "org", Direction: "upstream", HotPathOnly: true, MaxRecursionDepth: 1},
		{Name: "fac-downstream-hot-path", AnchorKind: "fac", Direction: "downstream", HotPathOnly: true, MaxRecursionDepth: 1},
		{Name: "product-upstream-hot-path", AnchorKind: "product", Direction: "upstream", HotPathOnly: true, MaxRecursionDepth: 1},
		{Name: "deposit-downstream-hot-path", AnchorKind: "deposit", Direction: "downstream", HotPathOnly: true, MaxRecursionDepth: 1},
		{Name: "gov-downstream-hot-path", AnchorKind: "gov", Direction: "downstream", HotPathOnly: true, CustomQuery: "focus on regulated trade corridors and dependent importers", MaxRecursionDepth: 1},
	}
	if *includeFull {
		scenarios = append(scenarios,
			util.DirectedScenario{Name: "org-upstream-full", AnchorKind: "org", Direction: "upstream", IncludeCapabilities: true, IncludeEdgeMetrics: true, CustomQuery: "focus on supplier capabilities, route stress, and chokepoints", MaxRecursionDepth: 1},
			util.DirectedScenario{Name: "fac-downstream-full", AnchorKind: "fac", Direction: "downstream", IncludeCapabilities: true, IncludeEdgeMetrics: true, CustomQuery: "focus on downstream distribution branches, logistics dependencies, and stressed routes", MaxRecursionDepth: 1},
		)
	}
	if filter := strings.TrimSpace(*scenarioFilter); filter != "" {
		filtered := make([]util.DirectedScenario, 0, 1)
		for _, scenario := range scenarios {
			if scenario.Name == filter {
				filtered = append(filtered, scenario)
				break
			}
		}
		if len(filtered) == 0 {
			log.Fatalf("unknown scenario %q", filter)
		}
		scenarios = filtered
	}

	results := make([]scenarioAttemptResult, 0, len(scenarios))
	failedScenarios := make([]string, 0)

	for _, scenario := range scenarios {
		anchors, err := util.PickAnchors(ctx, runtime.FragmentsRepo, scenario.AnchorKind, *sampleLimit)
		if err != nil {
			log.Fatalf("pick anchors for %s: %v", scenario.Name, err)
		}
		if len(anchors) == 0 {
			results = append(results, scenarioAttemptResult{
				Scenario:   scenario.Name,
				AnchorKind: scenario.AnchorKind,
				Error:      "no anchors found",
			})
			failedScenarios = append(failedScenarios, scenario.Name)
			continue
		}

		succeeded := false
		maxAttempts := *attempts
		if maxAttempts <= 0 {
			maxAttempts = 1
		}
		if maxAttempts > len(anchors) {
			maxAttempts = len(anchors)
		}
		for idx := 0; idx < maxAttempts; idx++ {
			anchor := anchors[idx]
			name := fmt.Sprintf("Codex %s %s", scenario.Name, strings.TrimPrefix(anchor.CanonicalPath, "/"))
			lens, err := runtime.CreateLens(ctx, "global", strings.TrimSpace(*jobProjectID), name, []string{anchor.CanonicalPath})
			if err != nil {
				results = append(results, scenarioAttemptResult{
					Scenario:   scenario.Name,
					AnchorKind: scenario.AnchorKind,
					Attempt:    idx + 1,
					Anchor:     anchor,
					Error:      "create lens: " + err.Error(),
				})
				continue
			}
			job, err := runtime.CreateDirectedExplorationJob(ctx, lens, anchor, scenario)
			if err != nil {
				results = append(results, scenarioAttemptResult{
					Scenario:   scenario.Name,
					AnchorKind: scenario.AnchorKind,
					Attempt:    idx + 1,
					Anchor:     anchor,
					LensID:     lens.ID,
					Error:      "create job: " + err.Error(),
				})
				continue
			}

			startedAt := time.Now()
			finishedJob, err := runtime.ExecuteJob(ctx, job.ID)
			durationMs := time.Since(startedAt).Milliseconds()
			if err != nil {
				results = append(results, scenarioAttemptResult{
					Scenario:   scenario.Name,
					AnchorKind: scenario.AnchorKind,
					Attempt:    idx + 1,
					Anchor:     anchor,
					LensID:     lens.ID,
					JobID:      job.ID,
					DurationMs: durationMs,
					Error:      "execute job: " + err.Error(),
				})
				continue
			}

			res := summarizeAttempt(scenario, anchor, lens.ID, finishedJob.ID, idx+1, durationMs, finishedJob)
			results = append(results, res)
			if res.Success {
				succeeded = true
				break
			}
		}
		if !succeeded {
			failedScenarios = append(failedScenarios, scenario.Name)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		log.Fatalf("encode output failed: %v", err)
	}

	if len(failedScenarios) > 0 {
		log.Fatalf("directed exploration smoke failed for scenarios: %s", strings.Join(failedScenarios, ", "))
	}
}

func summarizeAttempt(
	scenario util.DirectedScenario,
	anchor util.AnchorSample,
	lensID string,
	jobID string,
	attempt int,
	durationMs int64,
	job *horizonmodels.ScoutingJob,
) scenarioAttemptResult {
	result := scenarioAttemptResult{
		Scenario:   scenario.Name,
		AnchorKind: scenario.AnchorKind,
		Attempt:    attempt,
		Anchor:     anchor,
		LensID:     lensID,
		JobID:      jobID,
		DurationMs: durationMs,
	}

	state := map[string]interface{}(nil)
	if job != nil {
		state = job.State
	}
	result.DirectedCompleted = parseBool(state["directedExplorationCompleted"])
	result.ToolRuns = parseSliceMap(state["directedExplorationToolRuns"])
	result.HotPathChainsN = parseToolMetricInt(result.ToolRuns, "supply_chain.hot_path", "hotPathChainsN")

	if m, ok := state["supplyChainMaterialization"].(map[string]interface{}); ok {
		result.MaterializedPaths = parseInt(m["materializedPaths"])
		result.MaterializedSupplyLinks = parseInt(m["materializedSupplyLinks"])
	}
	if m, ok := state["supplyChainSummary"].(map[string]interface{}); ok {
		result.DiscoveredNodes = parseInt(m["discoveredNodes"])
		result.DiscoveredEdges = parseInt(m["discoveredEdges"])
	}
	expectedTool := "supply_chain.downstream_unwind"
	if strings.EqualFold(scenario.Direction, "upstream") {
		expectedTool = "supply_chain.upstream_unwind"
	}
	hasHotPathTool := hasToolRun(result.ToolRuns, "supply_chain.hot_path")
	hasDirectionTool := scenario.HotPathOnly || hasToolRun(result.ToolRuns, expectedTool)
	hasHotPathEvidence := result.HotPathChainsN > 0 ||
		result.MaterializedPaths > 0 ||
		result.MaterializedSupplyLinks > 0 ||
		(result.DiscoveredNodes > 0 && result.DiscoveredEdges > 0)
	result.Success = result.DirectedCompleted && hasHotPathTool && hasDirectionTool && hasHotPathEvidence
	return result
}

func parseBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return false
	}
}

func parseInt(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	default:
		return 0
	}
}

func parseSliceMap(v interface{}) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return append([]map[string]any(nil), t...)
	case []interface{}:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
				continue
			}
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func hasToolRun(runs []map[string]any, name string) bool {
	for _, run := range runs {
		if strings.TrimSpace(asString(run["name"])) == name {
			return true
		}
	}
	return false
}

func parseToolMetricInt(runs []map[string]any, name string, metricKey string) int {
	for _, run := range runs {
		if strings.TrimSpace(asString(run["name"])) != name {
			continue
		}
		metrics, ok := run["metrics"].(map[string]any)
		if !ok {
			if raw, ok := run["metrics"].(map[string]interface{}); ok {
				metrics = raw
			}
		}
		return parseInt(metrics[metricKey])
	}
	return 0
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

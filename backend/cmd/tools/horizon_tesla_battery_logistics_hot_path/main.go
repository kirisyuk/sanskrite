package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	horizongcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scenarios/teslabattery"
)

type runAck struct {
	RunID           string    `json:"runId"`
	Status          string    `json:"status"`
	AgentMeshTaskID string    `json:"agentmeshTaskId"`
	CreatedAt       time.Time `json:"createdAt"`
}

type worldsEnvelope struct {
	Worlds []*horizonmodels.SimulationWorld `json:"worlds"`
}

type simulationReport struct {
	ID       string
	Title    string
	Ack      *runAck
	Run      *horizonmodels.SimulationRun
	Bundle   *horizonmodels.SimulationBundle
	Artifact *horizonmodels.SimulationArtifact
	Worlds   []*horizonmodels.SimulationWorld
	Trace    map[string]any
}

type seedSummary struct {
	CreatedFragments []string
	UpdatedFragments []string
	CreatedEdges     []string
	UpdatedEdges     []string
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	edgeTenant := flag.String("edge-tenant", "system", "tenant for horizon_dependency_edges")
	apiBase := flag.String("api-base", firstNonEmpty(os.Getenv("HORIZON_API_BASE_URL"), "https://etigram-api-onq73n4gpa-uc.a.run.app"), "Horizon API base URL")
	functionsKey := flag.String("functions-key", firstNonEmpty(os.Getenv("HORIZON_FUNCTIONS_KEY"), "codex"), "x-functions-key for internal auth")
	seed := flag.Bool("seed", true, "seed or update the canonical hot path into Firestore")
	run := flag.Bool("run", true, "launch the simulation pack after seeding")
	timeout := flag.Duration("timeout", 12*time.Minute, "max wait time for simulation completion")
	pollInterval := flag.Duration("poll-interval", 5*time.Second, "simulation poll interval")
	httpTimeout := flag.Duration("http-timeout", 5*time.Minute, "per-request HTTP timeout")
	only := flag.String("only", "", "comma-separated simulation ids to run from the pack")
	flag.Parse()

	ctx := context.Background()
	spec := teslabattery.Spec(strings.TrimSpace(*projectID))
	if strings.TrimSpace(*only) != "" {
		spec.Simulations = filterSimulations(spec.Simulations, strings.Split(*only, ","))
	}

	var summary *seedSummary
	if *seed {
		client, err := horizongcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
		if err != nil {
			log.Fatalf("firestore init failed: %v", err)
		}
		defer client.Close()

		fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(client.Client())
		edgesRepo := horizonrepo.NewDependencyEdgesRepository(client.Client()).WithFragmentsRepo(fragmentsRepo)

		result, err := applyHotPath(ctx, fragmentsRepo, edgesRepo, strings.TrimSpace(*edgeTenant), spec)
		if err != nil {
			log.Fatalf("hot-path seed failed: %v", err)
		}
		summary = result
		log.Printf("Hot path seeded: %d fragments created, %d fragments updated, %d edges created, %d edges updated",
			len(summary.CreatedFragments), len(summary.UpdatedFragments), len(summary.CreatedEdges), len(summary.UpdatedEdges))
	}

	if !*run {
		if summary != nil {
			printSeedSummary(summary)
		}
		return
	}

	httpClient := &http.Client{Timeout: *httpTimeout}
	reports, err := runScenarioPack(ctx, httpClient, strings.TrimSpace(*apiBase), strings.TrimSpace(*functionsKey), spec, *timeout, *pollInterval)
	if err != nil {
		log.Fatalf("scenario pack failed: %v", err)
	}

	if summary != nil {
		printSeedSummary(summary)
	}
	printScenarioReport(spec, reports)
}

func applyHotPath(
	ctx context.Context,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	edgesRepo *horizonrepo.DependencyEdgesRepository,
	edgeTenant string,
	spec teslabattery.HotPathSpec,
) (*seedSummary, error) {
	out := &seedSummary{}
	pathToFragment := map[string]*horizonmodels.HorizonFragment{}

	for _, fragmentSpec := range spec.Fragments {
		existing, err := fragmentsRepo.GetByCanonicalPath(ctx, fragmentSpec.CanonicalPath)
		switch {
		case err == nil && existing != nil:
			mergeFragment(existing, fragmentSpec)
			if err := fragmentsRepo.Update(ctx, existing); err != nil {
				return nil, fmt.Errorf("update fragment %s: %w", fragmentSpec.CanonicalPath, err)
			}
			out.UpdatedFragments = append(out.UpdatedFragments, fragmentSpec.CanonicalPath)
			pathToFragment[fragmentSpec.CanonicalPath] = existing
		case err != nil && err != horizonrepo.ErrFragmentNotFound:
			return nil, fmt.Errorf("load fragment %s: %w", fragmentSpec.CanonicalPath, err)
		default:
			fragment := fragmentFromSeed(fragmentSpec)
			id, err := fragmentsRepo.Create(ctx, fragment)
			if err != nil {
				return nil, fmt.Errorf("create fragment %s: %w", fragmentSpec.CanonicalPath, err)
			}
			fragment.ID = id
			out.CreatedFragments = append(out.CreatedFragments, fragmentSpec.CanonicalPath)
			pathToFragment[fragmentSpec.CanonicalPath] = fragment
		}
	}

	for _, edgeSpec := range spec.Edges {
		source := pathToFragment[edgeSpec.SourcePath]
		if source == nil {
			fragment, err := fragmentsRepo.GetByCanonicalPath(ctx, edgeSpec.SourcePath)
			if err != nil {
				return nil, fmt.Errorf("resolve edge source %s: %w", edgeSpec.SourcePath, err)
			}
			source = fragment
		}
		target := pathToFragment[edgeSpec.TargetPath]
		if target == nil {
			fragment, err := fragmentsRepo.GetByCanonicalPath(ctx, edgeSpec.TargetPath)
			if err != nil {
				return nil, fmt.Errorf("resolve edge target %s: %w", edgeSpec.TargetPath, err)
			}
			target = fragment
		}

		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, edgeTenant, source.ID, target.ID, edgeSpec.Type)
		switch {
		case err == nil && existing != nil:
			mergeEdge(existing, edgeSpec)
			if err := edgesRepo.Update(ctx, existing); err != nil {
				return nil, fmt.Errorf("update edge %s -> %s (%s): %w", edgeSpec.SourcePath, edgeSpec.TargetPath, edgeSpec.Type, err)
			}
			out.UpdatedEdges = append(out.UpdatedEdges, edgeKey(edgeSpec))
		case err != nil && err != horizonrepo.ErrEdgeNotFound:
			return nil, fmt.Errorf("load edge %s -> %s (%s): %w", edgeSpec.SourcePath, edgeSpec.TargetPath, edgeSpec.Type, err)
		default:
			edge := edgeFromSeed(edgeTenant, source.ID, target.ID, edgeSpec)
			if _, err := edgesRepo.Create(ctx, edge); err != nil {
				return nil, fmt.Errorf("create edge %s -> %s (%s): %w", edgeSpec.SourcePath, edgeSpec.TargetPath, edgeSpec.Type, err)
			}
			out.CreatedEdges = append(out.CreatedEdges, edgeKey(edgeSpec))
		}
	}

	sort.Strings(out.CreatedFragments)
	sort.Strings(out.UpdatedFragments)
	sort.Strings(out.CreatedEdges)
	sort.Strings(out.UpdatedEdges)
	return out, nil
}

func fragmentFromSeed(spec teslabattery.FragmentSeed) *horizonmodels.HorizonFragment {
	return &horizonmodels.HorizonFragment{
		TenantID:       "global",
		Type:           spec.Type,
		Status:         "active",
		CanonicalPath:  spec.CanonicalPath,
		EntityPath:     spec.EntityPath,
		Labels:         cloneStringMap(spec.Labels),
		Description:    strings.TrimSpace(spec.Description),
		Tags:           compactStrings(spec.Tags),
		GovType:        strings.TrimSpace(spec.GovType),
		ParentGovPath:  strings.TrimSpace(spec.ParentGovPath),
		ParentPath:     strings.TrimSpace(spec.ParentPath),
		MarketDomain:   strings.TrimSpace(spec.MarketDomain),
		MarketVertical: strings.TrimSpace(spec.MarketVertical),
		MarketChannels: compactStrings(spec.MarketChannels),
		CommunityType:  strings.TrimSpace(spec.CommunityType),
		Qualifiers:     cloneAnyMap(spec.Qualifiers),
		State:          cloneMetricMap(spec.State),
		Provenance:     horizonmodels.Provenance{Method: "scenario.hot-path.seed"},
		Metadata:       horizonmodels.FragmentMetadata{SourceFrameID: "scenario.hot-path.seed"},
		CreatedBy:      "horizon_tesla_battery_logistics_hot_path",
		UpdatedBy:      "horizon_tesla_battery_logistics_hot_path",
	}
}

func mergeFragment(fragment *horizonmodels.HorizonFragment, spec teslabattery.FragmentSeed) {
	if fragment == nil {
		return
	}
	fragment.Type = firstType(fragment.Type, spec.Type)
	fragment.Status = firstNonEmpty(fragment.Status, "active")
	fragment.EntityPath = firstNonEmpty(fragment.EntityPath, spec.EntityPath)
	fragment.Description = firstNonEmpty(fragment.Description, spec.Description)
	fragment.GovType = firstNonEmpty(fragment.GovType, spec.GovType)
	fragment.ParentGovPath = firstNonEmpty(fragment.ParentGovPath, spec.ParentGovPath)
	fragment.ParentPath = firstNonEmpty(fragment.ParentPath, spec.ParentPath)
	fragment.MarketDomain = firstNonEmpty(fragment.MarketDomain, spec.MarketDomain)
	fragment.MarketVertical = firstNonEmpty(fragment.MarketVertical, spec.MarketVertical)
	fragment.CommunityType = firstNonEmpty(fragment.CommunityType, spec.CommunityType)
	fragment.MarketChannels = unionStrings(fragment.MarketChannels, spec.MarketChannels)
	fragment.Tags = unionStrings(fragment.Tags, spec.Tags)
	fragment.Labels = mergeStringMaps(fragment.Labels, spec.Labels)
	fragment.Qualifiers = mergeAnyMaps(fragment.Qualifiers, spec.Qualifiers)
	fragment.State = mergeMetricMaps(fragment.State, spec.State)
	fragment.UpdatedBy = "horizon_tesla_battery_logistics_hot_path"
	if strings.TrimSpace(fragment.Provenance.Method) == "" {
		fragment.Provenance.Method = "scenario.hot-path.seed"
	}
	if strings.TrimSpace(fragment.Metadata.SourceFrameID) == "" {
		fragment.Metadata.SourceFrameID = "scenario.hot-path.seed"
	}
}

func edgeFromSeed(edgeTenant, sourceID, targetID string, spec teslabattery.EdgeSeed) *horizonmodels.DependencyEdge {
	return &horizonmodels.DependencyEdge{
		TenantID:         edgeTenant,
		SourceFragmentID: sourceID,
		TargetFragmentID: targetID,
		Type:             spec.Type,
		Properties: horizonmodels.EdgeProperties{
			Criticality:   valueOrDefault(spec.Criticality, 0.75),
			Amplification: valueOrDefault(spec.Amplification, 1.0),
			LatencyHours:  spec.LatencyHours,
		},
		Qualifiers: cloneAnyMap(spec.Qualifiers),
		Metrics:    cloneMetricMap(spec.Metrics),
		Provenance: horizonmodels.Provenance{Method: "scenario.hot-path.seed"},
		Metadata:   horizonmodels.EdgeMetadata{SourceFrameID: "scenario.hot-path.seed"},
	}
}

func mergeEdge(edge *horizonmodels.DependencyEdge, spec teslabattery.EdgeSeed) {
	if edge == nil {
		return
	}
	edge.Type = spec.Type
	edge.Properties.Criticality = valueOrDefault(spec.Criticality, edge.Properties.Criticality)
	edge.Properties.Amplification = valueOrDefault(spec.Amplification, edge.Properties.Amplification)
	if spec.LatencyHours > 0 {
		edge.Properties.LatencyHours = spec.LatencyHours
	}
	edge.Qualifiers = mergeAnyMaps(edge.Qualifiers, spec.Qualifiers)
	edge.Metrics = mergeMetricMaps(edge.Metrics, spec.Metrics)
}

func runScenarioPack(
	ctx context.Context,
	client *http.Client,
	apiBase, functionsKey string,
	spec teslabattery.HotPathSpec,
	timeout, pollInterval time.Duration,
) ([]*simulationReport, error) {
	reports := make([]*simulationReport, 0, len(spec.Simulations))
	for _, sim := range spec.Simulations {
		log.Printf("creating simulation %s (%s)", sim.ID, sim.Title)
		ack, err := createSimulationRun(ctx, client, apiBase, functionsKey, sim.Request)
		if err != nil {
			return nil, fmt.Errorf("create simulation %s: %w", sim.ID, err)
		}
		log.Printf("created simulation %s runId=%s agentmeshTaskId=%s", sim.ID, ack.RunID, ack.AgentMeshTaskID)
		reports = append(reports, &simulationReport{ID: sim.ID, Title: sim.Title, Ack: ack})
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for _, report := range reports {
		run, err := waitForRun(waitCtx, client, apiBase, functionsKey, report.Ack.RunID, pollInterval)
		if err != nil {
			return nil, fmt.Errorf("wait for run %s: %w", report.Ack.RunID, err)
		}
		report.Run = run

		artifact, err := getBundleArtifact(waitCtx, client, apiBase, functionsKey, report.Ack.RunID)
		if err != nil {
			return nil, fmt.Errorf("bundle for run %s: %w", report.Ack.RunID, err)
		}
		report.Artifact = artifact
		report.Bundle = decodeBundle(artifact)

		worlds, err := getWorlds(waitCtx, client, apiBase, functionsKey, report.Ack.RunID)
		if err != nil {
			return nil, fmt.Errorf("worlds for run %s: %w", report.Ack.RunID, err)
		}
		report.Worlds = worlds

		trace, err := getTrace(waitCtx, client, apiBase, functionsKey, report.Ack.RunID)
		if err != nil {
			return nil, fmt.Errorf("trace for run %s: %w", report.Ack.RunID, err)
		}
		report.Trace = trace
	}
	return reports, nil
}

func filterSimulations(items []teslabattery.SimulationSpec, wanted []string) []teslabattery.SimulationSpec {
	if len(items) == 0 || len(wanted) == 0 {
		return items
	}
	allow := map[string]struct{}{}
	for _, item := range wanted {
		item = strings.TrimSpace(item)
		if item != "" {
			allow[item] = struct{}{}
		}
	}
	out := make([]teslabattery.SimulationSpec, 0, len(items))
	for _, item := range items {
		if _, ok := allow[strings.TrimSpace(item.ID)]; ok {
			out = append(out, item)
		}
	}
	return out
}

func createSimulationRun(ctx context.Context, client *http.Client, apiBase, functionsKey string, req horizonmodels.SimulationRunRequest) (*runAck, error) {
	var ack runAck
	url := strings.TrimRight(apiBase, "/") + "/api/horizon/simulations"
	if err := doJSON(ctx, client, http.MethodPost, url, functionsKey, req, &ack); err == nil {
		return &ack, nil
	}
	if err := doJSON(ctx, client, http.MethodPost, url, functionsKey, req, &ack); err != nil {
		return nil, err
	}
	return &ack, nil
}

func waitForRun(ctx context.Context, client *http.Client, apiBase, functionsKey, runID string, pollInterval time.Duration) (*horizonmodels.SimulationRun, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		run, err := getRun(ctx, client, apiBase, functionsKey, runID)
		if err != nil {
			return nil, err
		}
		switch strings.TrimSpace(run.Status) {
		case string(horizonmodels.SimulationRunStatusSucceeded),
			string(horizonmodels.SimulationRunStatusDegraded),
			string(horizonmodels.SimulationRunStatusFailed),
			string(horizonmodels.SimulationRunStatusCancelled),
			string(horizonmodels.SimulationRunStatusExpired):
			return run, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func getRun(ctx context.Context, client *http.Client, apiBase, functionsKey, runID string) (*horizonmodels.SimulationRun, error) {
	var run horizonmodels.SimulationRun
	if err := doJSON(ctx, client, http.MethodGet, strings.TrimRight(apiBase, "/")+"/api/horizon/simulations/"+runID, functionsKey, nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func getBundleArtifact(ctx context.Context, client *http.Client, apiBase, functionsKey, runID string) (*horizonmodels.SimulationArtifact, error) {
	var artifact horizonmodels.SimulationArtifact
	if err := doJSON(ctx, client, http.MethodGet, strings.TrimRight(apiBase, "/")+"/api/horizon/simulations/"+runID+"/bundle", functionsKey, nil, &artifact); err != nil {
		return nil, err
	}
	return &artifact, nil
}

func getWorlds(ctx context.Context, client *http.Client, apiBase, functionsKey, runID string) ([]*horizonmodels.SimulationWorld, error) {
	var envelope worldsEnvelope
	if err := doJSON(ctx, client, http.MethodGet, strings.TrimRight(apiBase, "/")+"/api/horizon/simulations/"+runID+"/worlds", functionsKey, nil, &envelope); err != nil {
		return nil, err
	}
	return envelope.Worlds, nil
}

func getTrace(ctx context.Context, client *http.Client, apiBase, functionsKey, runID string) (map[string]any, error) {
	var trace map[string]any
	if err := doJSON(ctx, client, http.MethodGet, strings.TrimRight(apiBase, "/")+"/api/horizon/simulations/"+runID+"/trace", functionsKey, nil, &trace); err != nil {
		return nil, err
	}
	return trace, nil
}

func doJSON(ctx context.Context, client *http.Client, method, url, functionsKey string, body any, out any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(functionsKey) != "" {
		req.Header.Set("x-functions-key", strings.TrimSpace(functionsKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var raw map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&raw)
		return fmt.Errorf("%s %s failed: status=%d body=%v", method, url, resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func decodeBundle(artifact *horizonmodels.SimulationArtifact) *horizonmodels.SimulationBundle {
	if artifact == nil || artifact.Payload == nil {
		return nil
	}
	rawBundle, ok := artifact.Payload["bundle"]
	if !ok {
		return nil
	}
	blob, err := json.Marshal(rawBundle)
	if err != nil {
		return nil
	}
	var bundle horizonmodels.SimulationBundle
	if err := json.Unmarshal(blob, &bundle); err != nil {
		return nil
	}
	return &bundle
}

func printSeedSummary(summary *seedSummary) {
	fmt.Println("== Hot Path Seed Summary ==")
	fmt.Printf("Created fragments: %d\n", len(summary.CreatedFragments))
	for _, item := range summary.CreatedFragments {
		fmt.Printf("  + %s\n", item)
	}
	fmt.Printf("Updated fragments: %d\n", len(summary.UpdatedFragments))
	for _, item := range summary.UpdatedFragments {
		fmt.Printf("  ~ %s\n", item)
	}
	fmt.Printf("Created edges: %d\n", len(summary.CreatedEdges))
	for _, item := range summary.CreatedEdges {
		fmt.Printf("  + %s\n", item)
	}
	fmt.Printf("Updated edges: %d\n", len(summary.UpdatedEdges))
	for _, item := range summary.UpdatedEdges {
		fmt.Printf("  ~ %s\n", item)
	}
}

func printScenarioReport(spec teslabattery.HotPathSpec, reports []*simulationReport) {
	fmt.Println()
	fmt.Println("== Scenario Pack ==")
	fmt.Printf("%s (%s)\n", spec.Title, spec.ID)
	fmt.Printf("Question: %s\n\n", spec.Question)

	for _, report := range reports {
		fmt.Printf("-- %s [%s] --\n", report.Title, report.ID)
		if report.Ack != nil && report.Run != nil {
			fmt.Printf("runId=%s status=%s agentmeshTaskId=%s\n", report.Ack.RunID, report.Run.Status, report.Ack.AgentMeshTaskID)
		}
		if report.Run != nil {
			outcome := report.Run.Summary.OutcomeDistribution
			sensitivity := report.Run.Summary.Sensitivity
			keyMechanisms := report.Run.Summary.KeyMechanisms
			if len(outcome) == 0 && report.Bundle != nil {
				outcome = report.Bundle.OutcomeDistribution
			}
			if len(sensitivity) == 0 && report.Bundle != nil {
				sensitivity = report.Bundle.Sensitivity
			}
			if len(keyMechanisms) == 0 && report.Bundle != nil {
				keyMechanisms = report.Bundle.KeyMechanisms
			}
			fmt.Printf("summary.outcome=%s\n", formatFloatMap(outcome))
			fmt.Printf("summary.sensitivity=%s\n", formatFloatMap(sensitivity))
			fmt.Printf("summary.keyMechanisms=%s\n", strings.Join(keyMechanisms, ", "))
			fmt.Printf("summary.groundingCoverage=%.3f\n", report.Run.Summary.GroundingCoverage)
		}
		if report.Bundle != nil {
			if len(report.Bundle.DisciplinedSignals) > 0 {
				fmt.Println("bundle.disciplinedSignals:")
				for _, item := range report.Bundle.DisciplinedSignals[:min(len(report.Bundle.DisciplinedSignals), 5)] {
					fmt.Printf("  - id=%v score=%v metrics=%v functions=%v summary=%v\n", item["id"], item["score"], item["primaryMetrics"], item["functions"], item["summary"])
				}
			}
			if len(report.Bundle.FunctionExposures) > 0 {
				fmt.Println("bundle.functionExposures:")
				for _, item := range report.Bundle.FunctionExposures[:min(len(report.Bundle.FunctionExposures), 5)] {
					fmt.Printf("  - function=%v score=%v disciplines=%v summary=%v\n", item["function"], item["score"], item["relatedDisciplines"], item["summary"])
				}
			}
			if len(report.Bundle.DecisionDemands) > 0 {
				fmt.Println("bundle.decisionDemands:")
				for _, item := range report.Bundle.DecisionDemands[:min(len(report.Bundle.DecisionDemands), 5)] {
					fmt.Printf("  - title=%v priority=%v functions=%v actions=%v\n", item["title"], item["priority"], item["functions"], item["triggeringActions"])
				}
			}
			if len(report.Bundle.MonitoringSignals) > 0 {
				fmt.Println("bundle.monitoringSignals:")
				for _, item := range report.Bundle.MonitoringSignals[:min(len(report.Bundle.MonitoringSignals), 6)] {
					fmt.Printf("  - title=%v kind=%v functions=%v why=%v\n", item["title"], item["kind"], item["functions"], item["why"])
				}
			}
			if len(report.Bundle.RoleReadouts) > 0 {
				fmt.Println("bundle.roleReadouts:")
				for _, roleID := range []string{"cfo", "coo", "pr_lead"} {
					if raw, ok := report.Bundle.RoleReadouts[roleID]; ok {
						role, _ := raw.(map[string]any)
						fmt.Printf("  - role=%s summary=%v immediateActions=%v weeklyMonitoring=%v\n", roleID, role["summary"], role["immediateActions"], role["weeklyMonitoring"])
					}
				}
			}
			if len(report.Bundle.ExecutiveNarrative) > 0 {
				fmt.Println("bundle.executiveNarrative:")
				for _, line := range report.Bundle.ExecutiveNarrative {
					fmt.Printf("  - %s\n", line)
				}
			}
			if len(report.Bundle.ExecutiveMemo) > 0 {
				fmt.Println("bundle.executiveMemo:")
				printMemoSection("whatIsHappening", report.Bundle.ExecutiveMemo["whatIsHappening"])
				printMemoSection("whatIsAtRisk", report.Bundle.ExecutiveMemo["whatIsAtRisk"])
				printMemoSection("actions30Days", report.Bundle.ExecutiveMemo["actions30Days"])
				printMemoSection("weeklyMonitoring", report.Bundle.ExecutiveMemo["weeklyMonitoring"])
				if pr, ok := report.Bundle.ExecutiveMemo["prResponse"].(map[string]any); ok && len(pr) > 0 {
					printMemoSection("prResponse.expectedHeadwinds", pr["expectedHeadwinds"])
					printMemoSection("prResponse.recommendedLines", pr["recommendedLines"])
					printMemoSection("prResponse.watchouts", pr["watchouts"])
				}
			}
			if len(report.Bundle.ActorReactions) > 0 {
				fmt.Println("bundle.actorReactions:")
				for _, item := range report.Bundle.ActorReactions[:min(len(report.Bundle.ActorReactions), 6)] {
					fmt.Printf("  - class=%v dominantActions=%v summary=%v\n", item["actorClass"], item["dominantActions"], item["reactionSummary"])
				}
			}
		}
		if report.Trace != nil {
			if preview, ok := report.Trace["baselinePreview"].(map[string]any); ok {
				fmt.Printf("baselinePreview=%s\n", formatTracePreview(preview))
			}
		}
		fmt.Println()
	}

	fmt.Println("== C-Level Readout ==")
	for _, line := range synthesizeCLevel(reports) {
		fmt.Printf("- %s\n", line)
	}
}

func synthesizeCLevel(reports []*simulationReport) []string {
	if len(reports) == 0 {
		return nil
	}
	lines := []string{}
	var strongest *simulationReport
	var strongestScore float64
	for _, report := range reports {
		if report.Bundle != nil {
			if len(report.Bundle.DisciplinedSignals) > 0 {
				top := report.Bundle.DisciplinedSignals[0]
				lines = append(lines, fmt.Sprintf("%s: top discipline %v (score=%v).", report.Title, top["id"], top["score"]))
			} else if memoLines := stringSliceAny(report.Bundle.ExecutiveMemo["whatIsHappening"]); len(memoLines) > 0 {
				lines = append(lines, fmt.Sprintf("%s: %s", report.Title, memoLines[0]))
			} else if len(report.Bundle.ExecutiveNarrative) > 0 {
				lines = append(lines, fmt.Sprintf("%s: %s", report.Title, report.Bundle.ExecutiveNarrative[0]))
			}
		}
		if report.Run == nil {
			continue
		}
		score := report.Run.Summary.OutcomeDistribution["risk_score"] +
			report.Run.Summary.OutcomeDistribution["price_pressure"] +
			report.Run.Summary.OutcomeDistribution["supply_stress"]
		if strongest == nil || score > strongestScore {
			strongest = report
			strongestScore = score
		}
	}
	if strongest != nil && strongest.Run != nil {
		lines = append(lines, fmt.Sprintf("Самый напряжённый контур в текущем пакете: %s с профилем %s.", strongest.Title, formatFloatMap(strongest.Run.Summary.OutcomeDistribution)))
	}
	for _, report := range reports {
		if report.Bundle == nil {
			continue
		}
		if len(report.Bundle.DecisionDemands) > 0 {
			lines = append(lines, fmt.Sprintf("Demand, %s: %v.", report.Title, report.Bundle.DecisionDemands[0]["title"]))
		}
		if len(report.Bundle.RoleReadouts) > 0 {
			if raw, ok := report.Bundle.RoleReadouts["pr_lead"]; ok {
				if role, ok := raw.(map[string]any); ok {
					if recs := stringSliceAny(role["immediateActions"]); len(recs) > 0 {
						lines = append(lines, fmt.Sprintf("PR, %s: %s", report.Title, recs[0]))
					}
				}
			}
		}
	}
	return lines
}

func printMemoSection(label string, value any) {
	lines := stringSliceAny(value)
	if len(lines) == 0 {
		return
	}
	fmt.Printf("  %s:\n", label)
	for _, line := range lines {
		fmt.Printf("    - %s\n", line)
	}
}

func stringSliceAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				text = strings.TrimSpace(text)
				if text != "" {
					out = append(out, text)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func formatTracePreview(preview map[string]any) string {
	metrics, _ := preview["metrics"].(map[string]any)
	if len(metrics) == 0 {
		return fmt.Sprintf("status=%v", preview["status"])
	}
	keys := []string{}
	for _, key := range []string{"nodesUpdated", "pathsTraced", "signalsInjected", "nodes_updated", "paths_traced"} {
		if value, ok := metrics[key]; ok {
			keys = append(keys, fmt.Sprintf("%s=%v", key, value))
		}
	}
	if len(keys) == 0 {
		keys = append(keys, fmt.Sprintf("metrics=%v", metrics))
	}
	return strings.Join(keys, ", ")
}

func formatFloatMap(values map[string]float64) string {
	if len(values) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%.3f", key, values[key]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func edgeKey(edge teslabattery.EdgeSeed) string {
	return fmt.Sprintf("%s --%s--> %s", edge.SourcePath, edge.Type, edge.TargetPath)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneAnyMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneMetricMap(in map[string]horizonmodels.MetricValue) map[string]horizonmodels.MetricValue {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]horizonmodels.MetricValue, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeStringMaps(base, patch map[string]string) map[string]string {
	if len(base) == 0 {
		return cloneStringMap(patch)
	}
	out := cloneStringMap(base)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range patch {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if _, ok := out[key]; !ok {
			out[key] = value
		}
	}
	return out
}

func mergeAnyMaps(base, patch map[string]interface{}) map[string]interface{} {
	if len(base) == 0 {
		return cloneAnyMap(patch)
	}
	out := cloneAnyMap(base)
	if out == nil {
		out = map[string]interface{}{}
	}
	for key, value := range patch {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func mergeMetricMaps(base, patch map[string]horizonmodels.MetricValue) map[string]horizonmodels.MetricValue {
	if len(base) == 0 {
		return cloneMetricMap(patch)
	}
	out := cloneMetricMap(base)
	if out == nil {
		out = map[string]horizonmodels.MetricValue{}
	}
	for key, value := range patch {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func unionStrings(base, patch []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(patch))
	for _, value := range append(append([]string{}, base...), patch...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactStrings(values []string) []string {
	return unionStrings(nil, values)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstType(values ...horizonmodels.FragmentType) horizonmodels.FragmentType {
	for _, value := range values {
		if strings.TrimSpace(string(value)) != "" {
			return value
		}
	}
	return ""
}

func valueOrDefault(value, fallback float64) float64 {
	if value != 0 {
		return value
	}
	return fallback
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

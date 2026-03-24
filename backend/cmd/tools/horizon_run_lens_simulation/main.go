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
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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

func main() {
	apiBase := flag.String("api-base", firstNonEmpty(os.Getenv("HORIZON_API_BASE_URL"), "https://etigram-api-onq73n4gpa-uc.a.run.app"), "Horizon API base URL")
	functionsKey := flag.String("functions-key", firstNonEmpty(os.Getenv("HORIZON_FUNCTIONS_KEY"), "codex"), "x-functions-key for internal auth")
	lensID := flag.String("lens", "", "lens id")
	question := flag.String("question", "", "simulation question")
	axis := flag.String("axis", "", "optional simulation axis")
	shockType := flag.String("type", "", "optional shock type")
	magnitude := flag.Float64("magnitude", 0.55, "shock magnitude")
	worldCount := flag.Int("world-count", 8, "world count")
	tickCount := flag.Int("tick-count", 6, "tick count")
	audience := flag.String("audience", "c_level", "audience")
	mode := flag.String("mode", "default", "mode")
	domainModes := flag.String("domain-modes", "", "comma separated domain modes")
	timeout := flag.Duration("timeout", 12*time.Minute, "max wait time")
	pollInterval := flag.Duration("poll-interval", 5*time.Second, "poll interval")
	flag.Parse()

	if strings.TrimSpace(*lensID) == "" || strings.TrimSpace(*question) == "" {
		log.Fatal("--lens and --question are required")
	}

	req := horizonmodels.SimulationRunRequest{
		Question:   strings.TrimSpace(*question),
		LensID:     strings.TrimSpace(*lensID),
		WorldCount: *worldCount,
		TickCount:  *tickCount,
		Audience:   strings.TrimSpace(*audience),
		Mode:       strings.TrimSpace(*mode),
	}
	if strings.TrimSpace(*domainModes) != "" {
		req.DomainModes = compactStrings(strings.Split(*domainModes, ","))
	}
	if strings.TrimSpace(*axis) != "" || strings.TrimSpace(*shockType) != "" {
		req.Signal = &horizonmodels.SimulationShock{
			Axis:      strings.TrimSpace(*axis),
			Type:      strings.TrimSpace(*shockType),
			Magnitude: *magnitude,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	client := &http.Client{Timeout: 120 * time.Second}
	ack, err := createSimulationRun(ctx, client, strings.TrimSpace(*apiBase), strings.TrimSpace(*functionsKey), req)
	if err != nil {
		log.Fatalf("create simulation run failed: %v", err)
	}
	run, err := waitForRun(ctx, client, strings.TrimSpace(*apiBase), strings.TrimSpace(*functionsKey), ack.RunID, *pollInterval)
	if err != nil {
		log.Fatalf("wait for run failed: %v", err)
	}
	artifact, err := getBundleArtifact(ctx, client, strings.TrimSpace(*apiBase), strings.TrimSpace(*functionsKey), ack.RunID)
	if err != nil {
		log.Fatalf("load bundle failed: %v", err)
	}
	worlds, err := getWorlds(ctx, client, strings.TrimSpace(*apiBase), strings.TrimSpace(*functionsKey), ack.RunID)
	if err != nil {
		log.Fatalf("load worlds failed: %v", err)
	}
	bundle := decodeBundle(artifact)

	fmt.Printf("runId=%s status=%s agentmeshTaskId=%s\n", ack.RunID, run.Status, ack.AgentMeshTaskID)
	fmt.Printf("worlds=%d completed=%d failed=%d\n", run.Progress.TotalWorlds, run.Progress.CompletedWorlds, run.Progress.FailedWorlds)
	if bundle == nil {
		return
	}
	printTopKV("outcomes", bundle.OutcomeDistribution)
	printTopLines("keyMechanisms", bundle.KeyMechanisms, 8)
	printTopMaps("protocolSummary", bundle.ProtocolSummary, 6)
	printTopMaps("transactionSummary", bundle.TransactionSummary, 6)
	printTopMaps("actorReactions", bundle.ActorReactions, 8)
	printTopMaps("monitoringSignals", bundle.MonitoringSignals, 10)
	fmt.Printf("worldSummaries=%d\n", len(worlds))
}

func createSimulationRun(ctx context.Context, client *http.Client, apiBase, functionsKey string, req horizonmodels.SimulationRunRequest) (*runAck, error) {
	var ack runAck
	err := doJSON(ctx, client, http.MethodPost, strings.TrimRight(apiBase, "/")+"/api/horizon/simulations", functionsKey, req, &ack)
	if err != nil {
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
	if functionsKey != "" {
		req.Header.Set("x-functions-key", functionsKey)
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

func printTopKV(title string, values map[string]float64) {
	if len(values) == 0 {
		return
	}
	fmt.Println(title + ":")
	for key, value := range values {
		fmt.Printf("  %s=%.3f\n", key, value)
	}
}

func printTopLines(title string, values []string, limit int) {
	if len(values) == 0 {
		return
	}
	fmt.Println(title + ":")
	for _, value := range values[:min(len(values), limit)] {
		fmt.Printf("  - %s\n", value)
	}
}

func printTopMaps(title string, values []map[string]any, limit int) {
	if len(values) == 0 {
		return
	}
	fmt.Println(title + ":")
	for _, value := range values[:min(len(values), limit)] {
		raw, _ := json.Marshal(value)
		fmt.Printf("  - %s\n", string(raw))
	}
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
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
	return out
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

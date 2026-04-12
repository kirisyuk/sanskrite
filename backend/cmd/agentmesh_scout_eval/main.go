package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/vertex"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
)

func main() {
	projectID := flag.String("project", strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), "GCP project ID")
	location := flag.String("location", envOr("AGENTMESH_VERTEX_LOCATION", "global"), "Vertex location")
	dataset := flag.String("dataset", "", "JSONL dataset path")
	gcsPrefix := flag.String("gcs-prefix", envOr("AGENTMESH_EVAL_GCS_PREFIX", ""), "gs:// bucket/prefix for eval artifacts")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" || strings.TrimSpace(*dataset) == "" || strings.TrimSpace(*gcsPrefix) == "" {
		fatalf("project, dataset, and gcs-prefix are required")
	}
	ctx := context.Background()
	vertexSvc, err := vertex.NewService(*projectID, *location, strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")))
	if err != nil {
		fatalf("vertex service: %v", err)
	}
	gcs, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		fatalf("gcs client: %v", err)
	}
	bucket, prefix, err := splitGCS(*gcsPrefix)
	if err != nil {
		fatalf("gcs prefix: %v", err)
	}
	cases, err := readCases(*dataset)
	if err != nil {
		fatalf("read dataset: %v", err)
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	results := make([]map[string]any, 0, len(cases))
	for i, item := range cases {
		task := &contracts.MeshTask{
			ID:         fmt.Sprintf("eval-%s-%03d", runID, i+1),
			TenantID:   stringField(item, "tenantId", "global"),
			Capability: "scout.orchestrate",
			Input:      item,
			SessionRef: fmt.Sprintf("eval-session-%s", uuid.NewString()),
		}
		memories, _ := vertexSvc.FetchMemories(ctx, task, stringField(item, "query", ""))
		output, cost, err := vertexSvc.RunScoutOrchestrator(ctx, task, item, memories)
		result := map[string]any{
			"caseIndex": i,
			"input":     item,
			"cost":      cost,
			"gates":     domainGates(output),
		}
		if err != nil {
			result["error"] = err.Error()
		} else {
			result["output"] = output
		}
		results = append(results, result)
	}
	payload := map[string]any{
		"runId":     runID,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"count":     len(results),
		"results":   results,
	}
	raw, _ := json.MarshalIndent(payload, "", "  ")
	object := strings.Trim(prefix, "/") + "/" + runID + "/results.json"
	if err := gcs.UploadBytes(ctx, bucket, object, raw, "application/json", map[string]string{"kind": "agentmesh-scout-eval"}); err != nil {
		fatalf("upload results: %v", err)
	}
	fmt.Printf("gs://%s/%s\n", bucket, object)
}

func readCases(path string) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		item := map[string]any{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, scanner.Err()
}

func domainGates(output map[string]any) map[string]any {
	return map[string]any{
		"hasPlan":             len(mapValue(output["plan"])) > 0,
		"hasReview":           len(mapValue(output["review"])) > 0,
		"hasReasoningTrace":   len(arrayValue(output["reasoningTrace"])) > 0,
		"hasRuntimeDiagnosis": len(arrayValue(output["runtimeDiagnosis"])) > 0,
		"hasCanonicalRefs":    strings.Contains(strings.ToLower(mustJSON(output)), "canonical"),
		"hasCandidates":       strings.Contains(strings.ToLower(mustJSON(output)), "candidate"),
	}
}

func splitGCS(uri string) (string, string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(uri), "gs://")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", fmt.Errorf("invalid GCS URI %q", uri)
	}
	if len(parts) == 1 {
		return parts[0], "agentmesh/evals", nil
	}
	return parts[0], parts[1], nil
}

func stringField(m map[string]any, key, fallback string) string {
	if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func mapValue(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func arrayValue(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

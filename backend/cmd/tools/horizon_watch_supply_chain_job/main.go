package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
)

func main() {
	projectID := flag.String("project", "etigram", "GCP project id")
	jobID := flag.String("job", "", "root scout job id")
	flag.Parse()

	if strings.TrimSpace(*jobID) == "" {
		log.Fatal("-job is required")
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer client.Close()

	root, err := loadJob(ctx, client, strings.TrimSpace(*jobID))
	if err != nil {
		log.Fatalf("load root job failed: %v", err)
	}

	children, err := loadChildren(ctx, client, strings.TrimSpace(*jobID))
	if err != nil {
		log.Fatalf("load child jobs failed: %v", err)
	}

	out := map[string]any{
		"root":     summarizeJob(root),
		"children": summarizeChildren(children),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatalf("encode output failed: %v", err)
	}
}

func loadJob(ctx context.Context, client *firestore.Client, jobID string) (map[string]any, error) {
	doc, err := client.Collection("agent_jobs").Doc(jobID).Get(ctx)
	if err != nil {
		return nil, err
	}
	data := doc.Data()
	if data == nil {
		return nil, fmt.Errorf("job %s returned empty document", jobID)
	}
	return normalizeMap(data), nil
}

func loadChildren(ctx context.Context, client *firestore.Client, parentJobID string) ([]map[string]any, error) {
	iter := client.Collection("agent_jobs").Where("state.parentJobId", "==", parentJobID).Documents(ctx)
	defer iter.Stop()

	var out []map[string]any
	for {
		doc, err := iter.Next()
		if err != nil {
			if strings.Contains(err.Error(), "no more items") {
				break
			}
			return nil, err
		}
		data := doc.Data()
		if data == nil {
			continue
		}
		out = append(out, normalizeMap(data))
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(asString(out[i]["createdAt"])) < strings.TrimSpace(asString(out[j]["createdAt"]))
	})
	return out, nil
}

func summarizeChildren(children []map[string]any) []map[string]any {
	if len(children) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(children))
	for _, child := range children {
		out = append(out, summarizeJob(child))
	}
	return out
}

func summarizeJob(job map[string]any) map[string]any {
	if job == nil {
		return nil
	}
	state := normalizeMap(job["state"])
	out := map[string]any{
		"id":                  strings.TrimSpace(asString(job["id"])),
		"status":              strings.TrimSpace(asString(job["status"])),
		"currentState":        firstNonEmpty(strings.TrimSpace(asString(job["currentState"])), strings.TrimSpace(asString(state["currentState"]))),
		"projectId":           strings.TrimSpace(asString(job["projectId"])),
		"targetCanonicalPath": strings.TrimSpace(asString(job["targetCanonicalPath"])),
		"createdAt":           strings.TrimSpace(asString(job["createdAt"])),
		"updatedAt":           strings.TrimSpace(asString(job["updatedAt"])),
	}
	if mode := strings.TrimSpace(asString(state["mode"])); mode != "" {
		out["mode"] = mode
	}
	if parent := strings.TrimSpace(asString(state["parentJobId"])); parent != "" {
		out["parentJobId"] = parent
	}
	if pass := strings.TrimSpace(asString(state["pipelinePass"])); pass != "" {
		out["pipelinePass"] = pass
	}
	if summary := normalizeMap(state["supplyChainSummary"]); len(summary) > 0 {
		out["supplyChainSummary"] = pick(summary,
			"discoveredNodes",
			"discoveredEdges",
			"normalizedSegments",
			"materializedSupplyLinks",
			"materializedPaths",
			"gapHints",
			"createdEdges",
			"edgeErrors",
			"fragmentErrors",
		)
	}
	if materialization := normalizeMap(state["supplyChainMaterialization"]); len(materialization) > 0 {
		out["supplyChainMaterialization"] = pick(materialization,
			"materializedSupplyLinks",
			"materializedPaths",
			"normalizedSegments",
			"stage",
		)
	}
	if assembly := normalizeMap(state["supplyChainAssembly"]); len(assembly) > 0 {
		out["supplyChainAssembly"] = pick(assembly,
			"artifactKey",
			"status",
			"stage",
			"provisional",
			"anchorComponentsN",
			"productsN",
			"actorsN",
			"derivedPathsN",
			"openGapsN",
			"backgroundContinuationScheduled",
			"backgroundContinuationJobIds",
		)
	}
	if stages := normalizeMap(state["pipelineStages"]); len(stages) > 0 {
		out["pipelineStages"] = stages
	}
	return out
}

func pick(m map[string]any, keys ...string) map[string]any {
	out := make(map[string]any)
	for _, key := range keys {
		if value, ok := m[key]; ok {
			out[key] = value
		}
	}
	return out
}

func normalizeMap(raw any) map[string]any {
	switch v := raw.(type) {
	case map[string]any:
		return v
	default:
		if raw == nil {
			return nil
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			return nil
		}
		return out
	}
}

func asString(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	default:
		if raw == nil {
			return ""
		}
		return fmt.Sprintf("%v", raw)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

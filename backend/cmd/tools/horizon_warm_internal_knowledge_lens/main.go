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

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hinternalknowledge "github.com/igorkirisyuk/sanskrite/internal/horizon/internalknowledge"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	var (
		projectID      string
		lensID         string
		reason         string
		targetTemp     string
		answerBucket   string
		answerStoreID  string
		vertexLocation string
		timeout        time.Duration
		pollInterval   time.Duration
	)

	flag.StringVar(&projectID, "project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon-test"), "GCP project id")
	flag.StringVar(&lensID, "lens-id", "", "lens id to warm")
	flag.StringVar(&reason, "reason", "manual_trigger", "warmup reason")
	flag.StringVar(&targetTemp, "target-temperature", "full", "wait target temperature: hot|warm|full")
	flag.StringVar(&answerBucket, "answer-bucket", firstNonEmpty(os.Getenv("RAG_DOCUMENTS_BUCKET"), ""), "GCS bucket for prepared answer docs")
	flag.StringVar(&answerStoreID, "answer-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_INTERNAL_KNOWLEDGE_STORE_ID"), os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"), ""), "Vertex datastore for prepared answer substrate")
	flag.StringVar(&vertexLocation, "vertex-location", firstNonEmpty(os.Getenv("VERTEX_SEARCH_LOCATION"), "global"), "Vertex location")
	flag.DurationVar(&timeout, "timeout", 25*time.Minute, "overall wait timeout")
	flag.DurationVar(&pollInterval, "poll", 5*time.Second, "state poll interval")
	flag.Parse()

	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	answerBucket = strings.TrimSpace(answerBucket)
	answerStoreID = strings.TrimSpace(answerStoreID)
	vertexLocation = strings.TrimSpace(vertexLocation)
	targetTemp = strings.ToLower(strings.TrimSpace(targetTemp))

	if lensID == "" {
		log.Fatal("--lens-id is required")
	}
	if projectID == "" {
		log.Fatal("--project is required")
	}
	if answerBucket == "" {
		answerBucket = projectID + "-rag-documents"
	}
	if answerStoreID == "" {
		log.Fatal("--answer-store is required or set VERTEX_SEARCH_INTERNAL_KNOWLEDGE_STORE_ID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	gcsClient, err := gcp.NewGCSClient(ctx, projectID)
	if err != nil {
		log.Fatalf("gcs init failed: %v", err)
	}
	vertexClient := ai.NewVertexAISearchClient(projectID, vertexLocation, answerStoreID, "")

	lensesRepo := hrepo.NewLensesRepository(fs.Client())
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(fs.Client())
	materializedRepo := hrepo.NewMaterializedArtifactsRepository(fs.Client())
	lensArtifactsRepo := hrepo.NewLensArtifactsRepository(fs.Client())
	analyticalPrimitivesRepo := hrepo.NewAnalyticalPrimitivesRepository(fs.Client())
	analyticalAggregatesRepo := hrepo.NewAnalyticalAggregatesRepository(fs.Client())
	signalLandingSummariesRepo := hrepo.NewSignalLandingSummariesRepository(fs.Client())

	lens, err := lensesRepo.Get(ctx, lensID)
	if err != nil {
		log.Fatalf("load lens failed: %v", err)
	}

	builder := hinternalknowledge.NewBuilder(
		fragmentsRepo,
		materializedRepo,
		lensArtifactsRepo,
		analyticalPrimitivesRepo,
		analyticalAggregatesRepo,
		signalLandingSummariesRepo,
	)
	warmer := hinternalknowledge.NewWarmupService(
		hinternalknowledge.WarmupConfig{
			ProjectID:     projectID,
			AnswerBucket:  answerBucket,
			AnswerStoreID: answerStoreID,
		},
		lensesRepo,
		builder,
		gcsClient,
		vertexClient,
		fs.Client(),
	)

	fmt.Printf("warming lens=%s name=%q bucket=%s store=%s\n", lens.ID, strings.TrimSpace(lens.Name), answerBucket, answerStoreID)
	printState("before", lens)
	requestedAt := time.Now().UTC()
	warmer.EnqueueLensWarmup(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ProjectID), lens.ID, strings.TrimSpace(reason))

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	lastSerialized := ""
	for {
		select {
		case <-ctx.Done():
			log.Fatalf("warmup wait timeout: %v", ctx.Err())
		case <-ticker.C:
			fresh, err := lensesRepo.Get(ctx, lens.ID)
			if err != nil {
				log.Printf("poll lens failed: %v", err)
				continue
			}
			state := internalKnowledgeState(fresh)
			serialized := mustJSON(state)
			if serialized != lastSerialized {
				printState("update", fresh)
				lastSerialized = serialized
			}
			status := strings.TrimSpace(anyString(state["status"]))
			switch status {
			case "ready":
				if targetTemperatureReadySince(state, targetTemp, requestedAt) {
					printState("ready", fresh)
					return
				}
			case "failed":
				printState("failed", fresh)
				log.Fatalf("internal knowledge warmup failed")
			}
			if targetTemperatureReadySince(state, targetTemp, requestedAt) {
				printState("target-ready", fresh)
				return
			}
		}
	}
}

func printState(prefix string, lens *hmodels.Lens) {
	state := internalKnowledgeState(lens)
	body, _ := json.MarshalIndent(state, "", "  ")
	fmt.Printf("[%s] internalKnowledge=%s\n", prefix, strings.TrimSpace(string(body)))
}

func internalKnowledgeState(lens *hmodels.Lens) map[string]any {
	if lens == nil {
		return map[string]any{}
	}
	viewConfig := lens.ViewConfig
	if viewConfig == nil {
		return map[string]any{}
	}
	raw := viewConfig["internalKnowledge"]
	switch typed := raw.(type) {
	case map[string]any:
		return typed
	default:
		return map[string]any{}
	}
}

func mustJSON(value any) string {
	body, _ := json.Marshal(value)
	return string(body)
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func targetTemperatureReadySince(state map[string]any, target string, requestedAt time.Time) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	status := strings.ToLower(strings.TrimSpace(anyString(state["status"])))
	if status == "failed" {
		return false
	}
	if target == "" || target == "full" {
		if !strings.EqualFold(status, "ready") {
			return false
		}
		if completedAt, ok := parseStateTime(state["completedAt"]); ok {
			return !completedAt.Before(requestedAt)
		}
		return false
	}
	if readyAt, ok := parseStateTime(state[target+"ReadyAt"]); ok {
		return !readyAt.Before(requestedAt)
	}
	if strings.EqualFold(status, "ready") {
		if completedAt, ok := parseStateTime(state["completedAt"]); ok {
			return !completedAt.Before(requestedAt) && availableTemperatureAtLeast(state, target)
		}
	}
	return false
}

func temperatureRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hot":
		return 1
	case "warm":
		return 2
	case "full":
		return 3
	default:
		return 0
	}
}

func availableTemperatureAtLeast(state map[string]any, target string) bool {
	available := state["availableTemperatures"]
	switch typed := available.(type) {
	case []any:
		for _, item := range typed {
			if temperatureRank(anyString(item)) >= temperatureRank(target) {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if temperatureRank(item) >= temperatureRank(target) {
				return true
			}
		}
	}
	return false
}

func parseStateTime(value any) (time.Time, bool) {
	raw := strings.TrimSpace(anyString(value))
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

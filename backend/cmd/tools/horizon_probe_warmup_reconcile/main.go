package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hinternalknowledge "github.com/igorkirisyuk/sanskrite/internal/horizon/internalknowledge"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	var (
		projectID    string
		lensID       string
		answerStore  string
		answerBucket string
		wait         time.Duration
	)

	flag.StringVar(&projectID, "project", firstNonEmpty(strings.TrimSpace(getenv("GCP_PROJECT_ID")), strings.TrimSpace(getenv("GOOGLE_CLOUD_PROJECT")), "horde-horizon-test-20260405"), "GCP project id")
	flag.StringVar(&lensID, "lens-id", "", "lens id to observe")
	flag.StringVar(&answerStore, "answer-store", firstNonEmpty(strings.TrimSpace(getenv("VERTEX_SEARCH_INTERNAL_KNOWLEDGE_STORE_ID")), "rag-documents-store"), "Vertex internal-knowledge datastore")
	flag.StringVar(&answerBucket, "answer-bucket", "", "GCS answer bucket")
	flag.DurationVar(&wait, "wait", 140*time.Second, "total wait time for reconcile probe")
	flag.Parse()

	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	answerStore = strings.TrimSpace(answerStore)
	answerBucket = strings.TrimSpace(answerBucket)
	if answerBucket == "" {
		answerBucket = projectID + "-rag-documents"
	}
	if lensID == "" {
		log.Fatal("--lens-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), wait+time.Minute)
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
	vertexClient := ai.NewVertexAISearchClient(projectID, "global", answerStore, "")

	lensesRepo := hrepo.NewLensesRepository(fs.Client())
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(fs.Client())
	materializedRepo := hrepo.NewMaterializedArtifactsRepository(fs.Client())
	lensArtifactsRepo := hrepo.NewLensArtifactsRepository(fs.Client())
	analyticalPrimitivesRepo := hrepo.NewAnalyticalPrimitivesRepository(fs.Client())
	analyticalAggregatesRepo := hrepo.NewAnalyticalAggregatesRepository(fs.Client())
	signalLandingSummariesRepo := hrepo.NewSignalLandingSummariesRepository(fs.Client())

	builder := hinternalknowledge.NewBuilder(
		fragmentsRepo,
		materializedRepo,
		lensArtifactsRepo,
		analyticalPrimitivesRepo,
		analyticalAggregatesRepo,
		signalLandingSummariesRepo,
	)
	_ = hinternalknowledge.NewWarmupService(
		hinternalknowledge.WarmupConfig{
			ProjectID:         projectID,
			AnswerBucket:      answerBucket,
			AnswerStoreID:     answerStore,
			ReconcileInterval: 5 * time.Second,
			ReconcileLimit:    64,
			ImportWaitTimeout: 45 * time.Minute,
			StaleRunningAfter: 45 * time.Minute,
		},
		lensesRepo,
		builder,
		gcsClient,
		vertexClient,
		fs.Client(),
	)

	lens, err := lensesRepo.Get(ctx, lensID)
	if err != nil {
		log.Fatalf("load lens failed: %v", err)
	}
	if substrate, err := builder.BuildForLens(ctx, lens, ""); err != nil {
		fmt.Printf("[debug] build substrate failed: %v\n", err)
	} else {
		hotDocs := 0
		for _, doc := range substrate.Docs {
			if strings.EqualFold(strings.TrimSpace(doc.Temperature), hinternalknowledge.PreparedTemperatureHot) {
				hotDocs++
			}
		}
		fmt.Printf("[debug] substrate corpus=%s totalDocs=%d hotHash=%s hotDocs=%d\n",
			strings.TrimSpace(substrate.CorpusID),
			len(substrate.Docs),
			strings.TrimSpace(hinternalknowledge.PreparedSubstrateHashForTemperature(substrate, hinternalknowledge.PreparedTemperatureHot)),
			hotDocs,
		)
	}
	if opName := firstNonEmpty(readStateValue(lens.ViewConfig, "importOperation")); opName != "" {
		statusResp, err := vertexClient.CheckOperationStatus(ctx, opName)
		if err != nil {
			fmt.Printf("[debug] operation check failed: %v\n", err)
		} else {
			fmt.Printf("[debug] operation done=%v metadata=%v error=%v\n", statusResp.Done, statusResp.Metadata, statusResp.Error)
		}
	}

	printState(ctx, lensesRepo, "before", lensID)
	time.Sleep(wait / 2)
	printState(ctx, lensesRepo, "mid", lensID)
	time.Sleep(wait - wait/2)
	printState(ctx, lensesRepo, "after", lensID)
}

func readStateValue(viewConfig map[string]interface{}, key string) string {
	if viewConfig == nil {
		return ""
	}
	state, _ := viewConfig["internalKnowledge"].(map[string]interface{})
	if state == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", state[strings.TrimSpace(key)]))
}

func printState(ctx context.Context, lensesRepo *hrepo.LensesRepository, prefix, lensID string) {
	lens, err := lensesRepo.Get(ctx, lensID)
	if err != nil {
		fmt.Printf("[%s] get error: %v\n", prefix, err)
		return
	}
	state, _ := lens.ViewConfig["internalKnowledge"].(map[string]interface{})
	if state == nil {
		state = map[string]interface{}{}
	}
	fmt.Printf("[%s] status=%v temperature=%v pendingStageHash=%v pendingImportSince=%v pendingImportMetadataUpdatedAt=%v importOperation=%v available=%v lastReason=%v\n",
		prefix,
		state["status"],
		state["temperatureLevel"],
		state["pendingStageHash"],
		state["pendingImportSince"],
		state["pendingImportMetadataUpdatedAt"],
		state["importOperation"],
		state["availableTemperatures"],
		state["lastReason"],
	)
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(key)))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

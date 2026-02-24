// horizon_news_by_fragments runs the news-by-fragments canvas: find fragment → Gemini search grounding → ontology facts → 12-axis signals.
//
// Flow:
//  1. Find fragment(s) in Firestore (by -path or -list to discover)
//  2. Call Gemini with search grounding for recent news
//  3. Create OntologyFact(events) in ontology_facts
//  4. Run SignalPipeline.ProcessEvent for each (12-axis extraction → seed injection → propagation)
//
// Usage:
//
//	GCP_PROJECT_ID=xxx go run ./cmd/tools/horizon_news_by_fragments -project=xxx -path=/org/ae/flydubai-llc
//
//	# List org fragments to pick one
//	go run ./cmd/tools/horizon_news_by_fragments -project=xxx -list -limit=5
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonsignals "github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

const tenantID = "global"

type fragmentLookupAdapter struct {
	repo *horizonrepo.HorizonFragmentsRepository
}

var (
	projectID   = flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project ID")
	path        = flag.String("path", "", "Canonical path of fragment (e.g. /org/ae/flydubai-llc)")
	list        = flag.Bool("list", false, "List org fragments (first N by ID); use -limit")
	limit       = flag.Int("limit", 10, "Max fragments when -list")
	daysBack    = flag.Int("days", 7, "Days back for news search")
	skipSignals = flag.Bool("skip-signals", false, "Skip SignalPipeline (only create facts)")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	if strings.TrimSpace(*projectID) == "" {
		*projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project or GCP_PROJECT_ID required")
	}

	logger := log.New(os.Stdout, "[news-by-fragments] ", log.LstdFlags)

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fs)

	var fragments []discovery.FragmentInput
	if *list {
		fragments = listFragments(ctx, fragmentsRepo, *limit)
		if len(fragments) == 0 {
			log.Fatal("No fragments found")
		}
		fmt.Println("\n=== Fragments (use -path=<canonicalPath> to run) ===")
		for _, f := range fragments {
			fmt.Printf("  %s  (%s)\n", f.CanonicalPath, f.CanonicalName)
		}
		return
	}

	cp := strings.TrimSpace(*path)
	if cp == "" {
		log.Fatal("--path required (or use -list to discover fragments)")
	}

	frag, err := fragmentsRepo.GetByCanonicalPath(ctx, cp)
	if err != nil || frag == nil {
		log.Fatalf("fragment not found for path %q: %v", cp, err)
	}
	label := ""
	if frag.Labels != nil {
		label = strings.TrimSpace(frag.Labels["en"])
	}
	if label == "" {
		label = cp
	}
	fragments = []discovery.FragmentInput{
		{CanonicalPath: cp, CanonicalName: label},
	}
	logger.Printf("Found fragment: %s (%s)", cp, label)

	// Load API key
	apiKey := loadAPIKey(ctx, *projectID)
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY or VERTEX_API_KEY required (env or Secret Manager)")
	}
	_ = os.Setenv("GEMINI_API_KEY", apiKey)

	// AI executor (Gemini with search grounding)
	mm := ai.NewAIModelManager()
	exec := ai.NewAITaskExecutor([]ai.AIService{ai.NewGeminiService(apiKey, mm)}, mm)

	// Repos
	ontologyFactsRepo := repo.NewOntologyFactsRepository(fs)
	dataSourcesRepo := repo.NewDataSourcesRepository(fs)

	lookup := &fragmentLookupAdapter{repo: fragmentsRepo}

	// SignalPipeline (optional)
	var signalPipeline discovery.EventSignalProcessor
	if !*skipSignals {
		horizonEdgesRepo := horizonrepo.NewDependencyEdgesRepository(fs)
		versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fs)
		lensesRepo := horizonrepo.NewLensesRepository(fs)
		profilesRepo := horizonrepo.NewPropagationProfilesRepository(fs)
		profileLookup := horizonpropagation.NewProfileLookup(profilesRepo)
		if err := profileLookup.Initialize(ctx); err != nil {
			logger.Printf("ProfileLookup init: %v (using defaults)", err)
		}
		propagationEngine := horizonpropagation.NewPropagationEngine(
			fragmentsRepo, versionsRepo, horizonEdgesRepo, lensesRepo, nil,
		).WithProfileLookup(profileLookup)
		signalExtractor := horizonsignals.NewSignalExtractor(nil, exec)
		applicability := horizonsignals.NewExternalApplicabilityEvaluator().
			WithFragmentsRepo(fragmentsRepo).
			WithEdgesRepo(horizonEdgesRepo)
		seedInjector := horizonsignals.NewSeedInjector(fragmentsRepo, horizonEdgesRepo).
			WithApplicabilityEvaluator(applicability)
		vertexDS := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"))
		if vertexDS != "" {
			vertexLoc := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_LOCATION"))
			if vertexLoc == "" {
				vertexLoc = "global"
			}
			vertexClient := ai.NewVertexAISearchClient(strings.TrimSpace(*projectID), vertexLoc, vertexDS, "")
			if vertexClient.IsConfigured() {
				seedInjector = seedInjector.WithVertexSearch(vertexClient)
			}
		}
		signalPipeline = horizonscout.NewSignalPipeline(
			signalExtractor, seedInjector, propagationEngine, profileLookup, ontologyFactsRepo,
		).WithEscalationLLM(exec)
		logger.Printf("SignalPipeline wired (12-axis + propagation)")
	} else {
		logger.Printf("SignalPipeline skipped (-skip-signals)")
	}

	canvas := discovery.NewNewsByFragmentsCanvas(discovery.NewsByFragmentsCanvasConfig{
		Exec:            exec,
		FactsRepo:       ontologyFactsRepo,
		DataSourcesRepo: dataSourcesRepo,
		SignalPipeline:  signalPipeline,
		FragmentLookup:  lookup,
		Publisher:       nil,
		Logger:          logger,
	})

	jobID := fmt.Sprintf("news_by_fragments_%d", os.Getpid())
	result, err := canvas.Run(ctx, tenantID, fragments, *daysBack, jobID)
	if err != nil {
		log.Fatalf("canvas run failed: %v", err)
	}

	// Output result
	fmt.Println("\n=== News-by-Fragments Result ===")
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
}

func (a *fragmentLookupAdapter) GetByCanonicalPath(ctx context.Context, path string) (*discovery.FragmentInfo, error) {
	frag, err := a.repo.GetByCanonicalPath(ctx, path)
	if err != nil || frag == nil {
		return nil, err
	}
	label := path
	if frag.Labels != nil {
		label = strings.TrimSpace(frag.Labels["en"])
	}
	if label == "" {
		label = path
	}
	return &discovery.FragmentInfo{
		CanonicalPath: path,
		Label:         label,
		Type:          string(frag.Type),
	}, nil
}

func listFragments(ctx context.Context, repo *horizonrepo.HorizonFragmentsRepository, limit int) []discovery.FragmentInput {
	frags, err := repo.ListByTypeAndTenant(ctx, tenantID, models.FragmentTypeOrganization, limit)
	if err != nil {
		log.Printf("ListByTypeAndTenant failed: %v", err)
		return nil
	}
	var out []discovery.FragmentInput
	for _, f := range frags {
		cp := strings.TrimSpace(f.CanonicalPath)
		if cp == "" {
			continue
		}
		label := cp
		if f.Labels != nil {
			label = strings.TrimSpace(f.Labels["en"])
		}
		if label == "" {
			label = cp
		}
		out = append(out, discovery.FragmentInput{CanonicalPath: cp, CanonicalName: label})
	}
	return out
}

func loadAPIKey(ctx context.Context, projectID string) string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VERTEX_API_KEY")); v != "" {
		return v
	}
	for _, name := range []string{"gemini_api_key", "GEMINI_API_KEY", "vertex_api_key", "VERTEX_API_KEY"} {
		v, err := gcp.LoadSecret(ctx, projectID, name)
		if err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

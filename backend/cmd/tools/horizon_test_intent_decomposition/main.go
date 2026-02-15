package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	horizonlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func getSecret(secretName string) string {
	cmd := exec.Command("gcloud", "secrets", "versions", "access", "latest", "--secret="+secretName, "--project=hatlone-test")
	out, err := cmd.Output()
	if err != nil {
		log.Printf("Warning: Failed to fetch secret %s: %v", secretName, err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

func normalizeCanonicalPath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	candidates := []string{"/org/", "/person/", "/product/", "/fac/", "/loc/", "/region/", "/event/", "/sphere/"}
	for _, c := range candidates {
		if idx := strings.Index(s, c); idx > 0 {
			s = s[idx:]
			break
		}
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

func main() {
	log.Println("=== Testing Intent Decomposition (Tesla Battery Logistics) ===")
	ctx := context.Background()
	projectID := "hatlone-test"

	targetFlag := flag.String("target", "/org/tesla", "Target canonical path")
	focusFlag := flag.String("focus", "Battery logistics supply chain: identify suppliers (cells, cathode/anode, lithium), manufacturing sites, logistics nodes, flows.", "Lens focus")
	forceScoutFlag := flag.Bool("force-scout", false, "Delete existing fragment to force fresh scouting (avoid freshness skip)")
	flag.Parse()

	targetPath := normalizeCanonicalPath(*targetFlag)

	// Secrets
	apiKey := strings.TrimSpace(os.Getenv("VERTEX_API_KEY"))
	if apiKey == "" {
		apiKey = getSecret("VERTEX_API_KEY")
	}
	if apiKey == "" {
		apiKey = getSecret("gemini_api_key")
	}
	if apiKey == "" {
		log.Fatal("FATAL: Could not find VERTEX_API_KEY or gemini_api_key")
	}

	// Firestore
	fsClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Firestore init failed: %v", err)
	}
	defer fsClient.Close()

	// Repos
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient)
	lensesRepo := horizonrepo.NewLensesRepository(fsClient)
	cascadeVisitedRepo := horizonrepo.NewCascadeVisitedRepository(fsClient)
	agentJobsRepo := corerepo.NewAgentJobsRepository(fsClient)
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	framesRepo := horizonrepo.NewFramesRepository(fsClient)
	documentsRepo := corerepo.NewDocumentsRepository(fsClient)
	dataSourcesRepo := corerepo.NewDataSourcesRepository(fsClient)
	factsRepo := corerepo.NewOntologyFactsRepository(fsClient)

	// AI
	modelManager := ai.NewAIModelManager()
	vertexService := ai.NewGeminiService(apiKey, modelManager)
	aiExecutor := ai.NewAITaskExecutor([]ai.AIService{vertexService}, modelManager)

	scoutAgent := horizonagents.NewScoutAgent(aiExecutor)
	criticAgent := horizonagents.NewCriticAgent(aiExecutor)
	resolverAgent := horizonagents.NewResolverAgent(aiExecutor)
	researchAgent := horizonagents.NewResearchAgent(aiExecutor)

	// Engine components
	planner := horizonscout.NewScoutPlanner(dataSourcesRepo, factsRepo, nil) // No Vertex Search for this tool
	wc := crawler.NewWebCrawler()
	rssProvider := prov.NewRSSProvider()
	sourceExecutor := horizonscout.NewSourceExecutor(wc, rssProvider, nil, documentsRepo)
	frameBuilder := horizonscout.NewFrameBuilder()
	workflow := horizonscout.NewScoutWorkflow(agentJobsRepo)

	scoutEngine := horizonscout.NewScoutEngine(
		planner,
		sourceExecutor,
		scoutAgent,
		criticAgent,
		resolverAgent,
		researchAgent,
		frameBuilder,
		workflow,
		fragmentsRepo,
		framesRepo,
		edgesRepo,
		scoutingJobsRepo,
		nil, // realtime disabled
		aiExecutor,
	)
	scoutEngine.WithCascadeVisitedRepo(cascadeVisitedRepo)

	// Optional: delete existing fragment to force fresh scouting.
	if *forceScoutFlag {
		log.Printf("FORCE: deleting existing fragment for %s (to avoid freshness skip)", targetPath)
		if oldFrag, err := fragmentsRepo.GetByCanonicalPath(ctx, targetPath); err == nil && oldFrag != nil && oldFrag.ID != "" {
			if edges, err := edgesRepo.ListBySource(ctx, "global", oldFrag.ID, nil, 1000); err == nil {
				for _, e := range edges {
					_ = edgesRepo.Delete(ctx, e.ID)
				}
			}
			if edges, err := edgesRepo.ListByTarget(ctx, "global", oldFrag.ID, nil, 1000); err == nil {
				for _, e := range edges {
					_ = edgesRepo.Delete(ctx, e.ID)
				}
			}
			if err := fragmentsRepo.Delete(ctx, oldFrag.ID); err != nil {
				log.Printf("Warning: failed to delete old fragment: %v", err)
			} else {
				log.Printf("Deleted existing fragment %s to force fresh scouting", oldFrag.ID)
			}
		}
	}

	// Lens service (only to reuse CreateLens schema)
	lifecycle := horizonlens.NewLifecycleManager(fragmentsRepo)
	calculator := horizonlens.NewMetricsCalculator(fragmentsRepo)
	lensService := horizonlens.NewLensService(lensesRepo, fragmentsRepo, scoutingJobsRepo, nil, lifecycle, calculator, nil)

	lens := &models.Lens{
		TenantID:  "global",
		ProjectID: "test-intent-decomp",
		Name:      "Tesla Battery Logistics (Intent Decomp Test)",
		Scope:     []string{targetPath},
		ViewConfig: map[string]interface{}{
			"focus":             strings.TrimSpace(*focusFlag),
			"spherePaths":       []string{"/sphere/logistics"},
			"keywords":          []string{"tesla", "battery", "cells", "logistics", "supply chain", "procurement"},
			"maxRecursionDepth": 1,
		},
		Status: "ACTIVE",
	}

	lensID, err := lensService.CreateLens(ctx, lens)
	if err != nil {
		log.Fatalf("Failed to create lens: %v", err)
	}
	log.Printf("✅ Lens Created: %s (ID: %s)", lens.Name, lensID)

	workerAgentID := fmt.Sprintf("scout_test_%d", time.Now().UnixNano())
	job := &models.ScoutingJob{
		AgentJob: agentmodels.AgentJob{
			TenantID:  "global",
			ProjectID: "test-intent-decomp",
			Status:    agentmodels.AgentJobStatusPending,
			AgentID:   workerAgentID,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			State: map[string]interface{}{
				"targetCanonicalPath": targetPath,
				"lensId":              lensID,
				"currentState":        string(models.ScoutingJobStatePending),
				"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
				"recursionDepth":      0.0,
				"maxRecursionDepth":   1.0,
				"focus":               strings.TrimSpace(*focusFlag),
				"spherePaths":         []string{"/sphere/logistics"},
				"keywords":            []string{"tesla", "battery", "cells", "logistics", "supply chain", "procurement"},
			},
		},
		TargetCanonicalPath: targetPath,
		ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
		BillingContext: models.BillingContext{
			ChargeModel: string(models.ChargeModelBounty),
			ProjectID:   "test-intent-decomp",
		},
	}

	jobID, err := scoutingJobsRepo.Create(ctx, job)
	if err != nil {
		log.Fatalf("FATAL: Failed to create job: %v", err)
	}
	log.Printf("✅ ScoutingJob Created: %s", jobID)

	createdJob, err := scoutingJobsRepo.Get(ctx, jobID)
	if err != nil {
		log.Fatalf("FATAL: Job not found: %v", err)
	}

	log.Printf("\n--- Executing ScoutingJob ---")
	start := time.Now()
	if err := scoutEngine.ExecuteScoutingJob(ctx, createdJob); err != nil {
		log.Fatalf("❌ ScoutingJob failed: %v", err)
	}
	log.Printf("✅ ScoutingJob completed in %v", time.Since(start))

	log.Printf("\n--- Checking Results ---")
	// sphere fragment
	rootFragment, _ := fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
	sphereFrag, _ := fragmentsRepo.GetByCanonicalPath(ctx, "/sphere/logistics")
	if rootFragment != nil && sphereFrag != nil {
		edgesFromSphere, _ := edgesRepo.ListBySource(ctx, "global", sphereFrag.ID, nil, 100)
		ok := 0
		for _, e := range edgesFromSphere {
			if e.Type == models.DependencyTypeInfluences && e.TargetFragmentID == rootFragment.ID {
				ok++
			}
		}
		log.Printf("Sphere->Root INFLUENCES edges: %d", ok)
	}
	log.Printf("=== Test Complete ===")
}



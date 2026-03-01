package main

import (
	"context"
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
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func getSecret(secretName string) string {
	cmd := exec.Command("gcloud", "secrets", "versions", "access", "latest", "--secret="+secretName, "--project=etigram")
	out, err := cmd.Output()
	if err != nil {
		log.Printf("Warning: Failed to fetch secret %s: %v", secretName, err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

func main() {
	ctx := context.Background()
	projectID := "etigram"

	query := "How does Tesla's battery logistics work?"
	if len(os.Args) > 1 {
		query = strings.Join(os.Args[1:], " ")
	}
	query = strings.TrimSpace(query)
	log.Printf("=== Horizon Intent Scenario ===\nquery=%q", query)

	// Secrets (Vertex)
	apiKey := strings.TrimSpace(os.Getenv("VERTEX_API_KEY"))
	if apiKey == "" {
		apiKey = getSecret("VERTEX_API_KEY")
	}
	if apiKey == "" {
		apiKey = getSecret("gemini_api_key")
	}
	if apiKey == "" {
		log.Fatal("FATAL: missing VERTEX_API_KEY / gemini_api_key")
	}

	// Firestore
	fsClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Firestore init failed: %v", err)
	}
	defer fsClient.Close()

	// Load runtime taxonomy (sphere allowlist/synonyms, relation types)
	taxonomy.InitRuntimeTaxonomyCacheFromFirestore(fsClient, "v1", 10*time.Minute)
	_ = taxonomy.RefreshRuntimeTaxonomy(ctx)

	// AI executor
	modelManager := ai.NewAIModelManager()
	vertexService := ai.NewGeminiService(apiKey, modelManager)
	execAI := ai.NewAITaskExecutor([]ai.AIService{vertexService}, modelManager)

	// Intent resolve (LLM → taxonomy-aligned intent)
	intentAgent := horizonagents.NewIntentResolverAgent(execAI)
	intent, err := intentAgent.Resolve(ctx, query)
	if err != nil {
		log.Fatalf("Intent resolve failed: %v", err)
	}

	log.Printf("Intent:\n- focus: %s\n- spherePaths: %v\n- keywords: %v\n- anchors: %v",
		intent.Focus, intent.SpherePaths, intent.Keywords, func() []string {
			out := []string{}
			for _, a := range intent.Anchors {
				out = append(out, a.CanonicalPath)
			}
			return out
		}())

	anchor := intent.Anchors[0].CanonicalPath

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

	// Scout engine (reuse the normal pipeline)
	scoutAgent := horizonagents.NewScoutAgent(execAI)
	criticAgent := horizonagents.NewCriticAgent(execAI)
	resolverAgent := horizonagents.NewResolverAgent(execAI)
	researchAgent := horizonagents.NewResearchAgent(execAI)
	planner := horizonscout.NewScoutPlanner(dataSourcesRepo, factsRepo, nil)
	wc := crawler.NewWebCrawler()
	rssProvider := prov.NewRSSProvider()
	sourceExecutor := horizonscout.NewSourceExecutor(wc, rssProvider, nil, documentsRepo)
	frameBuilder := horizonscout.NewFrameBuilder()
	workflow := horizonscout.NewScoutWorkflow(agentJobsRepo)
	engine := horizonscout.NewScoutEngine(
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
		nil,
		execAI,
	)
	engine.WithCascadeVisitedRepo(cascadeVisitedRepo)

	// Lens service
	lifecycle := horizonlens.NewLifecycleManager(fragmentsRepo)
	calculator := horizonlens.NewMetricsCalculator(fragmentsRepo)
	lensService := horizonlens.NewLensService(lensesRepo, fragmentsRepo, scoutingJobsRepo, factsRepo, lifecycle, calculator, nil)

	lens := &models.Lens{
		TenantID:  "global",
		ProjectID: "intent-scenario",
		Name:      "Intent Scenario: " + query,
		Scope:     []string{anchor},
		ViewConfig: map[string]interface{}{
			"focus":             intent.Focus,
			"spherePaths":       intent.SpherePaths,
			"keywords":          intent.Keywords,
			"maxRecursionDepth": 1,
		},
		Status: "ACTIVE",
	}

	lensID, err := lensService.CreateLens(ctx, lens)
	if err != nil {
		log.Fatalf("CreateLens failed: %v", err)
	}
	log.Printf("✅ Lens created: id=%s scope=%v", lensID, lens.Scope)

	// Create a deterministic single scouting job for the anchor with the intent signals.
	now := time.Now().UTC()
	job := &models.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			TenantID:  "global",
			ProjectID: "intent-scenario",
			Status:    coremodels.AgentJobStatusPending,
			AgentID:   fmt.Sprintf("scout_intent_%d", now.UnixNano()),
			CreatedAt: now,
			UpdatedAt: now,
			State: map[string]interface{}{
				"targetCanonicalPath": anchor,
				"lensId":              lensID,
				"currentState":        string(models.ScoutingJobStatePending),
				"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
				"recursionDepth":      0.0,
				"maxRecursionDepth":   1.0,
				"focus":               intent.Focus,
				"spherePaths":         intent.SpherePaths,
				"keywords":            intent.Keywords,
			},
		},
		TargetCanonicalPath: anchor,
		ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
		BillingContext: models.BillingContext{
			ChargeModel: string(models.ChargeModelBounty),
			ProjectID:   "intent-scenario",
		},
	}

	jobID, err := scoutingJobsRepo.Create(ctx, job)
	if err != nil {
		log.Fatalf("Create job failed: %v", err)
	}
	log.Printf("✅ ScoutingJob created: %s", jobID)

	loaded, err := scoutingJobsRepo.Get(ctx, jobID)
	if err != nil {
		log.Fatalf("Reload job failed: %v", err)
	}

	start := time.Now()
	if err := engine.ExecuteScoutingJob(ctx, loaded); err != nil {
		log.Fatalf("ExecuteScoutingJob failed: %v", err)
	}
	log.Printf("✅ Scout completed in %v", time.Since(start))
	log.Printf("=== Done ===")
}



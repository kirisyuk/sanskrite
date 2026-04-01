package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	horizonpathbuilder "github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func getSecret(secretName string) string {
	cmd := exec.Command("gcloud", "secrets", "versions", "access", "latest", "--secret="+secretName, "--project=nv-horizon")
	out, err := cmd.Output()
	if err != nil {
		log.Printf("WARN: failed to fetch secret %s: %v", secretName, err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func main() {
	projectID := flag.String("project", "nv-horizon", "GCP project / Firestore project")
	jobID := flag.String("job", "", "Scouting job ID to execute locally")
	flag.Parse()

	if strings.TrimSpace(*jobID) == "" {
		log.Fatal("--job is required")
	}

	ctx := context.Background()

	vertexAPIKey := firstNonEmpty(
		os.Getenv("VERTEX_API_KEY"),
		getSecret("VERTEX_API_KEY"),
		getSecret("gemini_api_key"),
	)
	if vertexAPIKey == "" {
		log.Fatal("missing VERTEX_API_KEY / gemini_api_key")
	}
	openAIKey := firstNonEmpty(
		os.Getenv("OPENAI_API_KEY"),
		getSecret("OPENAI_API_KEY"),
		getSecret("openai_api_key"),
	)
	grokKey := firstNonEmpty(
		os.Getenv("GROK_API_KEY"),
		getSecret("GROK_API_KEY"),
		getSecret("xai_api_key"),
	)

	fsClient, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	taxonomy.InitRuntimeTaxonomyCacheFromFirestore(fsClient, "v1", 10*time.Minute)
	taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient, "v1", 20*time.Minute)
	_ = taxonomy.RefreshRuntimeTaxonomy(ctx)

	modelManager := ai.NewAIModelManager()
	services := []ai.AIService{ai.NewGeminiService(vertexAPIKey, modelManager)}
	if openAIKey != "" {
		services = append(services, ai.NewOpenAIService(openAIKey, modelManager))
	}
	if grokKey != "" {
		services = append(services, ai.NewGrokService(grokKey, modelManager))
	}
	executor := ai.NewAITaskExecutor(services, modelManager)

	fragmentsRepo := repo.NewHorizonFragmentsRepository(fsClient)
	edgesRepo := repo.NewDependencyEdgesRepository(fsClient)
	lensesRepo := repo.NewLensesRepository(fsClient)
	cascadeVisitedRepo := repo.NewCascadeVisitedRepository(fsClient)
	framesRepo := repo.NewFramesRepository(fsClient)
	materializedArtifactsRepo := repo.NewMaterializedArtifactsRepository(fsClient)
	workingProductScopesRepo := repo.NewWorkingProductScopesRepository(fsClient)
	capabilityProfilesRepo := repo.NewCapabilityProfilesRepository(fsClient)
	capabilityObsRepo := repo.NewCapabilityObservationsRepository(fsClient)
	supplyLinkObsRepo := repo.NewSupplyLinkObservationsRepository(fsClient)
	routeObsRepo := repo.NewRouteObservationsRepository(fsClient)
	derivedRiskProfilesRepo := repo.NewDerivedRiskProfilesRepository(fsClient)

	agentJobsRepo := corerepo.NewAgentJobsRepository(fsClient)
	scoutingJobsRepo := repo.NewScoutingJobsRepository(agentJobsRepo)
	documentsRepo := corerepo.NewDocumentsRepository(fsClient)
	dataSourcesRepo := corerepo.NewDataSourcesRepository(fsClient)
	ontologyFactsRepo := corerepo.NewOntologyFactsRepository(fsClient)

	scoutAgent := agents.NewScoutAgent(executor)
	criticAgent := agents.NewCriticAgent(executor)
	resolverAgent := agents.NewResolverAgent(executor)
	researchAgent := agents.NewResearchAgent(executor)
	planner := scout.NewScoutPlanner(dataSourcesRepo, ontologyFactsRepo, nil)
	sourceExecutor := scout.NewSourceExecutor(crawler.NewWebCrawler(), prov.NewRSSProvider(), nil, documentsRepo)
	frameBuilder := scout.NewFrameBuilder()
	workflow := scout.NewScoutWorkflow(agentJobsRepo)

	engine := scout.NewScoutEngine(
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
		executor,
	)
	engine.WithCascadeVisitedRepo(cascadeVisitedRepo).
		WithLensesRepository(lensesRepo).
		WithWorkingProductScopesRepository(workingProductScopesRepo).
		WithCapabilityProfilesRepository(capabilityProfilesRepo).
		WithCapabilityObservationsRepository(capabilityObsRepo).
		WithSupplyLinkObservationsRepository(supplyLinkObsRepo).
		WithRouteObservationsRepository(routeObsRepo).
		WithDerivedRiskProfilesRepository(derivedRiskProfilesRepo).
		WithSupplyChainPathBuilder(horizonpathbuilder.NewSupplyChainPathBuilder(fragmentsRepo, edgesRepo, materializedArtifactsRepo)).
		WithDomainArtifactBuilder(horizonpathbuilder.NewDomainArtifactBuilder(fragmentsRepo, edgesRepo, materializedArtifactsRepo))

	job, err := scoutingJobsRepo.Get(ctx, strings.TrimSpace(*jobID))
	if err != nil {
		log.Fatalf("load scouting job %s: %v", strings.TrimSpace(*jobID), err)
	}
	if job == nil {
		log.Fatalf("job %s not found", strings.TrimSpace(*jobID))
	}

	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	job.Status = agentmodels.AgentJobStatusPending
	job.UpdatedAt = time.Now().UTC()

	log.Printf("Running scout job locally: id=%s project=%s target=%s", job.ID, job.ProjectID, job.TargetCanonicalPath)
	start := time.Now()
	if err := engine.ExecuteScoutingJob(ctx, job); err != nil {
		log.Fatalf("ExecuteScoutingJob failed: %v", err)
	}
	log.Printf("Scout job completed locally in %s", time.Since(start).Round(time.Second))
}

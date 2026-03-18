package directedexplorationutil

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpathbuilder "github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type AnchorSample struct {
	ID            string                     `json:"id"`
	CanonicalPath string                     `json:"canonicalPath"`
	Label         string                     `json:"label,omitempty"`
	Type          horizonmodels.FragmentType `json:"type"`
	UpdatedAt     time.Time                  `json:"updatedAt,omitempty"`
	HasCoords     bool                       `json:"hasCoords,omitempty"`
}

type LocalRuntime struct {
	Firestore        *firestore.Client
	Engine           *horizonscout.ScoutEngine
	FragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	LensesRepo       *horizonrepo.LensesRepository
	ScoutingJobsRepo *horizonrepo.ScoutingJobsRepository
}

type DirectedScenario struct {
	Name                string
	AnchorKind          string
	Direction           string
	HotPathOnly         bool
	IncludeCapabilities bool
	IncludeEdgeMetrics  bool
	CustomQuery         string
	MaxRecursionDepth   int
}

func getSecret(secretName string) string {
	cmd := exec.Command("gcloud", "secrets", "versions", "access", "latest", "--secret="+secretName, "--project=etigram")
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

func NewLocalRuntime(ctx context.Context, projectID string) (*LocalRuntime, error) {
	projectID = strings.TrimSpace(firstNonEmpty(projectID, "etigram"))
	fsClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("firestore init failed: %w", err)
	}

	taxonomy.InitRuntimeTaxonomyCacheFromFirestore(fsClient, "v1", 10*time.Minute)
	taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient, "v1", 20*time.Minute)
	_ = taxonomy.RefreshRuntimeTaxonomy(ctx)

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

	modelManager := ai.NewAIModelManager()
	services := make([]ai.AIService, 0, 4)
	vertexLocation := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1"))
	vertexService, err := ai.NewVertexAIService(projectID, vertexLocation, modelManager)
	if err != nil {
		fsClient.Close()
		return nil, fmt.Errorf("vertex ai init failed: %w", err)
	}
	services = append(services, vertexService)
	if openAIKey != "" {
		services = append(services, ai.NewOpenAIService(openAIKey, modelManager))
	}
	if grokKey != "" {
		services = append(services, ai.NewGrokService(grokKey, modelManager))
	}
	executor := ai.NewAITaskExecutor(services, modelManager)

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient)
	lensesRepo := horizonrepo.NewLensesRepository(fsClient)
	cascadeVisitedRepo := horizonrepo.NewCascadeVisitedRepository(fsClient)
	framesRepo := horizonrepo.NewFramesRepository(fsClient)
	materializedArtifactsRepo := horizonrepo.NewMaterializedArtifactsRepository(fsClient)
	workingProductScopesRepo := horizonrepo.NewWorkingProductScopesRepository(fsClient)
	capabilityProfilesRepo := horizonrepo.NewCapabilityProfilesRepository(fsClient)
	capabilityObsRepo := horizonrepo.NewCapabilityObservationsRepository(fsClient)
	supplyLinkObsRepo := horizonrepo.NewSupplyLinkObservationsRepository(fsClient)
	routeObsRepo := horizonrepo.NewRouteObservationsRepository(fsClient)
	derivedRiskProfilesRepo := horizonrepo.NewDerivedRiskProfilesRepository(fsClient)

	agentJobsRepo := corerepo.NewAgentJobsRepository(fsClient)
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	documentsRepo := corerepo.NewDocumentsRepository(fsClient)
	dataSourcesRepo := corerepo.NewDataSourcesRepository(fsClient)
	ontologyFactsRepo := corerepo.NewOntologyFactsRepository(fsClient)

	scoutAgent := horizonagents.NewScoutAgent(executor)
	criticAgent := horizonagents.NewCriticAgent(executor)
	resolverAgent := horizonagents.NewResolverAgent(executor)
	researchAgent := horizonagents.NewResearchAgent(executor)
	planner := horizonscout.NewScoutPlanner(dataSourcesRepo, ontologyFactsRepo, nil)
	sourceExecutor := horizonscout.NewSourceExecutor(crawler.NewWebCrawler(), prov.NewRSSProvider(), nil, documentsRepo)
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

	return &LocalRuntime{
		Firestore:        fsClient,
		Engine:           engine,
		FragmentsRepo:    fragmentsRepo,
		LensesRepo:       lensesRepo,
		ScoutingJobsRepo: scoutingJobsRepo,
	}, nil
}

func (r *LocalRuntime) Close() error {
	if r == nil || r.Firestore == nil {
		return nil
	}
	return r.Firestore.Close()
}

func PickAnchors(ctx context.Context, repo *horizonrepo.HorizonFragmentsRepository, kind string, limit int) ([]AnchorSample, error) {
	if repo == nil {
		return nil, fmt.Errorf("fragments repo is nil")
	}
	if limit <= 0 {
		limit = 10
	}
	scanLimit := limit * 20
	if scanLimit < 100 {
		scanLimit = 100
	}
	var (
		fragType horizonmodels.FragmentType
		prefix   string
	)
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "org":
		fragType = horizonmodels.FragmentTypeOrganization
		prefix = "/org/"
	case "gov":
		fragType = horizonmodels.FragmentTypeOrganization
		prefix = "/gov/"
	case "fac":
		fragType = horizonmodels.FragmentTypeFacility
		prefix = "/fac/"
	case "product":
		fragType = horizonmodels.FragmentTypeProduct
		prefix = "/product/"
	case "deposit":
		fragType = horizonmodels.FragmentTypeDeposit
		prefix = "/deposit/"
	default:
		return nil, fmt.Errorf("unsupported anchor kind %q", kind)
	}
	fragments, err := repo.ListByTypeAndTenant(ctx, "global", fragType, scanLimit)
	if err != nil {
		return nil, err
	}
	samples := make([]AnchorSample, 0, len(fragments))
	for _, fragment := range fragments {
		if fragment == nil {
			continue
		}
		cp := strings.TrimSpace(fragment.CanonicalPath)
		if !strings.HasPrefix(strings.ToLower(cp), strings.ToLower(prefix)) {
			continue
		}
		if !anchorLooksUsable(cp) {
			continue
		}
		label := strings.TrimSpace(fragment.Labels["en"])
		if label == "" {
			label = strings.TrimSpace(fragment.Identifiers["legalName"])
		}
		if label == "" {
			label = strings.TrimSpace(fragment.Identifiers["shortName"])
		}
		samples = append(samples, AnchorSample{
			ID:            strings.TrimSpace(fragment.ID),
			CanonicalPath: cp,
			Label:         label,
			Type:          fragment.Type,
			UpdatedAt:     fragment.UpdatedAt,
			HasCoords:     fragment.HasCoordinates,
		})
	}
	sort.SliceStable(samples, func(i, j int) bool {
		return anchorSampleScore(samples[i]) > anchorSampleScore(samples[j])
	})
	if len(samples) > limit {
		samples = samples[:limit]
	}
	return samples, nil
}

func anchorLooksUsable(cp string) bool {
	cp = strings.ToLower(strings.TrimSpace(cp))
	if cp == "" || strings.HasPrefix(cp, "/discovered/") {
		return false
	}
	badTokens := []string{
		"/na/",
		"placeholder",
		"synthetic",
		"unknown",
		"unnamed",
		"general-production",
		"general-facility",
	}
	for _, token := range badTokens {
		if strings.Contains(cp, token) {
			return false
		}
	}
	return true
}

func anchorSampleScore(sample AnchorSample) int64 {
	var score int64
	if sample.Label != "" {
		score += 20
	}
	if sample.HasCoords {
		score += 8
	}
	if !sample.UpdatedAt.IsZero() {
		score += sample.UpdatedAt.Unix()
	}
	return score
}

func (r *LocalRuntime) CreateLens(ctx context.Context, tenantID, projectID, name string, scope []string) (*horizonmodels.Lens, error) {
	if r == nil || r.LensesRepo == nil {
		return nil, fmt.Errorf("lenses repo is nil")
	}
	lens := &horizonmodels.Lens{
		TenantID:    strings.TrimSpace(firstNonEmpty(tenantID, "global")),
		ProjectID:   strings.TrimSpace(firstNonEmpty(projectID, "codex-directed-exploration")),
		Name:        strings.TrimSpace(firstNonEmpty(name, "Codex Directed Exploration")),
		Scope:       dedupStrings(scope),
		DomainModes: []string{taxonomy.DomainModeSupplyChain},
		Status:      string(horizonmodels.LensStatusActive),
		ViewConfig:  map[string]any{"source": "codex_directed_exploration_tool"},
		AlertConfig: map[string]any{},
	}
	if _, err := r.LensesRepo.Create(ctx, lens); err != nil {
		return nil, err
	}
	return lens, nil
}

func (r *LocalRuntime) CreateDirectedExplorationJob(ctx context.Context, lens *horizonmodels.Lens, anchor AnchorSample, scenario DirectedScenario) (*horizonmodels.ScoutingJob, error) {
	if r == nil || r.ScoutingJobsRepo == nil {
		return nil, fmt.Errorf("scouting jobs repo is nil")
	}
	if lens == nil {
		return nil, fmt.Errorf("lens is required")
	}
	if strings.TrimSpace(anchor.CanonicalPath) == "" {
		return nil, fmt.Errorf("anchor canonical path is required")
	}
	now := time.Now().UTC()
	maxDepth := scenario.MaxRecursionDepth
	if maxDepth < 0 {
		maxDepth = 0
	}
	state := map[string]any{
		"targetCanonicalPath":                strings.TrimSpace(anchor.CanonicalPath),
		"lensId":                             strings.TrimSpace(lens.ID),
		"currentState":                       string(horizonmodels.ScoutingJobStatePending),
		"scoutingStrategy":                   string(horizonmodels.ScoutingStrategySnapshot),
		"recursionDepth":                     0.0,
		"maxRecursionDepth":                  float64(maxDepth),
		"trigger":                            "tool.directed_explore.smoke",
		"uiScoutingMode":                     taxonomy.DomainModeSupplyChain,
		"domainModes":                        []string{taxonomy.DomainModeSupplyChain},
		"ignoreFreshnessForUserRequest":      true,
		"query":                              strings.TrimSpace(firstNonEmpty(scenario.CustomQuery, scenario.Name)),
		"originalQuery":                      strings.TrimSpace(firstNonEmpty(scenario.CustomQuery, scenario.Name)),
		"relationChildSchedulingOptIn":       true,
		"supplyChainSkipChildScheduling":     true,
		"supplyChainSkipFamilyCoResearch":    true,
		"supplyChainFamilyCoResearchEnabled": false,
		"directedExploration": horizonmodels.DirectedExplorationConfig{
			DomainMode:          taxonomy.DomainModeSupplyChain,
			Direction:           strings.TrimSpace(firstNonEmpty(scenario.Direction, "downstream")),
			HotPathOnly:         scenario.HotPathOnly,
			HotPathNodeIDs:      dedupStrings([]string{strings.TrimSpace(anchor.ID)}),
			IncludeCapabilities: scenario.IncludeCapabilities,
			IncludeEdgeMetrics:  scenario.IncludeEdgeMetrics,
			CustomQuery:         strings.TrimSpace(scenario.CustomQuery),
			RenderTarget:        "graph",
			IgnoreFreshness:     true,
		},
		"hotPathCanonicalPaths": []string{strings.TrimSpace(anchor.CanonicalPath)},
	}
	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			TenantID:  strings.TrimSpace(lens.TenantID),
			ProjectID: strings.TrimSpace(lens.ProjectID),
			Status:    coremodels.AgentJobStatusPending,
			AgentID:   "scout",
			CreatedAt: now,
			UpdatedAt: now,
			State:     state,
		},
		LensID:              strings.TrimSpace(lens.ID),
		TargetCanonicalPath: strings.TrimSpace(anchor.CanonicalPath),
		ScoutingStrategy:    string(horizonmodels.ScoutingStrategySnapshot),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelBounty),
			ProjectID:   strings.TrimSpace(lens.ProjectID),
		},
		CurrentState: horizonmodels.ScoutingJobStatePending,
	}
	jobID, err := r.ScoutingJobsRepo.Create(ctx, job)
	if err != nil {
		return nil, err
	}
	return r.ScoutingJobsRepo.Get(ctx, jobID)
}

func (r *LocalRuntime) ExecuteJob(ctx context.Context, jobID string) (*horizonmodels.ScoutingJob, error) {
	if r == nil || r.Engine == nil || r.ScoutingJobsRepo == nil {
		return nil, fmt.Errorf("runtime is not initialized")
	}
	job, err := r.ScoutingJobsRepo.Get(ctx, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("job %s not found", strings.TrimSpace(jobID))
	}
	job.Status = coremodels.AgentJobStatusPending
	job.UpdatedAt = time.Now().UTC()
	if err := r.Engine.ExecuteDirectedSupplyChainJob(ctx, job); err != nil {
		return nil, err
	}
	return r.ScoutingJobsRepo.Get(ctx, strings.TrimSpace(jobID))
}

func dedupStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
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

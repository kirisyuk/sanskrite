package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	firebase "firebase.google.com/go/v4"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/paypal"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/services"
	"google.golang.org/api/option"

	// orchestrator deps
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/pubsub"
	"github.com/igorkirisyuk/sanskrite/internal/adapters"
	agentmeshcontrol "github.com/igorkirisyuk/sanskrite/internal/agentmesh/controlplane"
	agentmeshpolicy "github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	agentmeshregistry "github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
	agentmeshrepo "github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	agentmeshruntime "github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	agentmeshsubmitter "github.com/igorkirisyuk/sanskrite/internal/agentmesh/submitter"
	agentmeshtasks "github.com/igorkirisyuk/sanskrite/internal/agentmesh/tasks"
	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/discovery/extraction"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/domain"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	"github.com/igorkirisyuk/sanskrite/internal/geonames"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
	"github.com/igorkirisyuk/sanskrite/internal/wikidata"
	"google.golang.org/api/idtoken"

	// Redis cache
	"github.com/igorkirisyuk/sanskrite/internal/cache"

	// Horizon system
	criticalchain "github.com/igorkirisyuk/sanskrite/internal/criticalchain"
	criticalchainpipeline "github.com/igorkirisyuk/sanskrite/internal/criticalchain/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon"
	horizoncriticalpath "github.com/igorkirisyuk/sanskrite/internal/horizon/critical_path"
	horizonenrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	horizonfeedback "github.com/igorkirisyuk/sanskrite/internal/horizon/feedback"
	horizongovdict "github.com/igorkirisyuk/sanskrite/internal/horizon/govdict"
	horizongraphenrich "github.com/igorkirisyuk/sanskrite/internal/horizon/graphenrich"
	horizonimportance "github.com/igorkirisyuk/sanskrite/internal/horizon/importance"
	horizoninternalknowledge "github.com/igorkirisyuk/sanskrite/internal/horizon/internalknowledge"
	horizoninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	horizonmechanismdict "github.com/igorkirisyuk/sanskrite/internal/horizon/mechanismdict"
	horizonmetriccore "github.com/igorkirisyuk/sanskrite/internal/horizon/metriccore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpathbuilder "github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	horizonproductdict "github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonprofile "github.com/igorkirisyuk/sanskrite/internal/horizon/profile"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonqa "github.com/igorkirisyuk/sanskrite/internal/horizon/qa"
	horizonrecommendations "github.com/igorkirisyuk/sanskrite/internal/horizon/recommendations"
	horizonregulatorytransform "github.com/igorkirisyuk/sanskrite/internal/horizon/regulatorytransform"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonrisk "github.com/igorkirisyuk/sanskrite/internal/horizon/risk"
	horizonrtctx "github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	horizonscopeagent "github.com/igorkirisyuk/sanskrite/internal/horizon/scopeagent"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	horizonsignals "github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	horizonsimulation "github.com/igorkirisyuk/sanskrite/internal/horizon/simulation"
	horizonsupplychain "github.com/igorkirisyuk/sanskrite/internal/horizon/supplychain"
	horizontasks "github.com/igorkirisyuk/sanskrite/internal/horizon/tasks"
	horizontelemetry "github.com/igorkirisyuk/sanskrite/internal/horizon/telemetry"
	horizonwatch "github.com/igorkirisyuk/sanskrite/internal/horizon/watch"
	horizonwatchdog "github.com/igorkirisyuk/sanskrite/internal/horizon/watchdog"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
	criticalchainhttp "github.com/igorkirisyuk/sanskrite/internal/http/criticalchain"
	horizonhttp "github.com/igorkirisyuk/sanskrite/internal/http/horizon"
)

// tavilyClientAdapterForAgents адаптирует httphandlers.TavilyClient к agents.TavilySearchClient интерфейсу
// Определен на уровне пакета для избежания циклических импортов
type tavilyClientAdapterForAgents struct {
	client *httphandlers.TavilyClient
}

func (a *tavilyClientAdapterForAgents) Search(ctx context.Context, query string, maxResults int) (*agents.TavilySearchResponse, error) {
	resp, err := a.client.Search(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	// Конвертировать httphandlers.TavilySearchResponse в agents.TavilySearchResponse
	results := make([]agents.TavilySearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		results = append(results, agents.TavilySearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
			Score:   r.Score,
		})
	}
	return &agents.TavilySearchResponse{
		Query:   resp.Query,
		Results: results,
	}, nil
}

// tavilyAdapterForSignalMonitoring адаптирует httphandlers.TavilyClient к SignalMonitoringService интерфейсу
type tavilyAdapterForSignalMonitoring struct {
	client *httphandlers.TavilyClient
}

func (a *tavilyAdapterForSignalMonitoring) Search(ctx context.Context, query string, maxResults int) (*services.TavilySearchResponse, error) {
	resp, err := a.client.Search(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	// Конвертировать httphandlers.TavilySearchResponse в services.TavilySearchResponse
	results := make([]services.TavilySearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		results = append(results, services.TavilySearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
			Score:   r.Score,
		})
	}
	return &services.TavilySearchResponse{
		Results: results,
	}, nil
}

// fragmentLookupForNewsCanvas adapts HorizonFragmentsRepository to discovery.FragmentLookup.
type fragmentLookupForNewsCanvas struct {
	repo *horizonrepo.HorizonFragmentsRepository
}

func (a *fragmentLookupForNewsCanvas) GetByCanonicalPath(ctx context.Context, path string) (*discovery.FragmentInfo, error) {
	f, err := a.repo.GetByCanonicalPath(ctx, path)
	if err != nil || f == nil {
		return nil, err
	}
	label := ""
	if f.Labels != nil {
		if en, ok := f.Labels["en"]; ok && strings.TrimSpace(en) != "" {
			label = strings.TrimSpace(en)
		}
	}
	if label == "" && f.Labels != nil {
		for _, l := range f.Labels {
			if strings.TrimSpace(l) != "" {
				label = strings.TrimSpace(l)
				break
			}
		}
	}
	return &discovery.FragmentInfo{CanonicalPath: f.CanonicalPath, Label: label, Type: string(f.Type)}, nil
}

// enforcementRedirectRecorder adapts CanonicalPathResolver to RedirectRecorder for enforcement path fixes.
type enforcementRedirectRecorder struct {
	r *horizonscout.CanonicalPathResolver
}

func (e *enforcementRedirectRecorder) RecordRedirect(ctx context.Context, fromPath, toPath string, fragmentType horizonmodels.FragmentType, reason string) {
	if e != nil && e.r != nil {
		e.r.RecordDeterministicRedirect(ctx, fromPath, toPath, fragmentType, reason)
	}
}

type server struct {
	fbApp            *firebase.App
	fs               *gcp.FirestoreClient
	users            *repo.UsersRepository
	projs            *repo.ProjectsRepository
	dsrcs            *repo.DataSourcesRepository
	sprofs           *repo.SourceProfilesRepository
	usersHandler     *httphandlers.UsersHandler
	projectsHandler  *httphandlers.ProjectsHandler
	discoveryHandler *httphandlers.DiscoveryHandler
	logger           *log.Logger
}

func buildGraphEnrichFragmentContext(ctx context.Context, tenantID string, frag *horizonmodels.HorizonFragment, frRepo *horizonrepo.HorizonFragmentsRepository, edgesRepo *horizonrepo.DependencyEdgesRepository) horizongraphenrich.FragmentContext {
	if frag == nil || frRepo == nil || edgesRepo == nil || strings.TrimSpace(frag.ID) == "" {
		return horizongraphenrich.FragmentContext{}
	}
	outgoing, _ := edgesRepo.ListBySource(ctx, tenantID, frag.ID, nil, 20)
	incoming, _ := edgesRepo.ListByTarget(ctx, tenantID, frag.ID, nil, 20)
	neighborIDs := map[string]struct{}{}
	routeHints := make([]string, 0, 8)
	zoneHints := make([]string, 0, 8)
	pathHints := []string{frag.CanonicalPath, frag.ParentPath, frag.ParentGovPath, frag.ParentMarketPath, frag.EntityPath}
	pathHints = append(pathHints, frag.IndustryPaths...)
	collectEdge := func(edge *horizonmodels.DependencyEdge) {
		if edge == nil {
			return
		}
		if id := strings.TrimSpace(edge.SourceFragmentID); id != "" && id != strings.TrimSpace(frag.ID) {
			neighborIDs[id] = struct{}{}
		}
		if id := strings.TrimSpace(edge.TargetFragmentID); id != "" && id != strings.TrimSpace(frag.ID) {
			neighborIDs[id] = struct{}{}
		}
		if edge.Qualifiers != nil {
			routeHints = append(routeHints,
				qualifierStringValues(edge.Qualifiers[horizonmodels.QualifierTransportVia])...)
			routeHints = append(routeHints,
				qualifierStringValues(edge.Qualifiers[horizonmodels.QualifierFlowRefs])...)
			zoneHints = append(zoneHints,
				qualifierStringValues(edge.Qualifiers[horizonmodels.QualifierZoneRefs])...)
			zoneHints = append(zoneHints,
				qualifierStringValues(edge.Qualifiers[horizonmodels.QualifierAffectedZoneRefs])...)
		}
		pathHints = append(pathHints, string(edge.Type), string(edge.GetFacet()))
	}
	for _, edge := range outgoing {
		collectEdge(edge)
	}
	for _, edge := range incoming {
		collectEdge(edge)
	}
	ids := make([]string, 0, len(neighborIDs))
	for id := range neighborIDs {
		ids = append(ids, id)
	}
	nearby := make([]*horizonmodels.HorizonFragment, 0, len(ids))
	if len(ids) > 0 {
		if neighbors, err := frRepo.GetMany(ctx, ids); err == nil {
			for _, id := range ids {
				if n := neighbors[id]; n != nil {
					nearby = append(nearby, n)
					pathHints = append(pathHints, n.CanonicalPath, n.EntityPath)
					pathHints = append(pathHints, n.IndustryPaths...)
				}
			}
		}
	}
	return horizongraphenrich.FragmentContext{
		NearbyFragments: nearby,
		IncomingEdges:   incoming,
		OutgoingEdges:   outgoing,
		RouteHints:      compactHintStrings(routeHints),
		ZoneHints:       compactHintStrings(zoneHints),
		PathHints:       compactHintStrings(pathHints),
	}
}

func runtimeReenrichFragment(ctx context.Context, frag *horizonmodels.HorizonFragment, frRepo *horizonrepo.HorizonFragmentsRepository, edgesRepo *horizonrepo.DependencyEdgesRepository) {
	if frag == nil || frRepo == nil || edgesRepo == nil || strings.TrimSpace(frag.ID) == "" {
		return
	}
	tenantID := strings.TrimSpace(frag.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	clone := *frag
	ctxHints := buildGraphEnrichFragmentContext(ctx, tenantID, &clone, frRepo, edgesRepo)
	if !horizongraphenrich.ApplyFragmentHeuristicsWithContext(&clone, ctxHints) {
		return
	}
	clone.IndexPaths = horizonmodels.BuildIndexPaths(&clone, nil)
	if err := frRepo.UpdateDerived(ctx, &clone); err != nil {
		log.Printf("WARNING: runtime graphenrich fragment update failed: id=%s path=%s err=%v", strings.TrimSpace(clone.ID), strings.TrimSpace(clone.CanonicalPath), err)
	}
}

func qualifierStringValues(v interface{}) []string {
	switch raw := v.(type) {
	case []string:
		return compactHintStrings(raw)
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return compactHintStrings(out)
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" && s != "<nil>" {
			return []string{s}
		}
		return nil
	}
}

func compactHintStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
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

func main() {
	addr := getEnv("PORT", "8080")
	ctx := context.Background()

	credsPath := getEnv("GOOGLE_APPLICATION_CREDENTIALS", "")
	var opts []option.ClientOption
	if credsPath != "" {
		opts = append(opts, option.WithCredentialsFile(credsPath))
	}

	projectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
	if projectID != "" && strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")) == "" {
		if key := gcp.GetEnvOrSecret(ctx, projectID, "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" {
			_ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
			log.Printf("Google Maps API key loaded from Secret Manager")
		}
	}

	// Override Firebase Auth Project ID if specified (for when Firebase project differs from GCP project)
	firebaseProjectID := getEnv("FIREBASE_AUTH_PROJECT_ID", projectID)
	log.Printf("Initializing Firebase App with ProjectID: %s (GCP_PROJECT_ID: %s)", firebaseProjectID, projectID)

	// bucket name env or default (used for both projects handler and artifacts handler)
	bucket := os.Getenv("CORE_DOCS_BUCKET")
	if bucket == "" {
		bucket = projectID + "-core-docs"
	}
	fbApp, err := firebase.NewApp(ctx, &firebase.Config{
		ProjectID: firebaseProjectID,
	}, opts...)
	if err != nil {
		log.Fatalf("firebase app init: %v", err)
	}

	// Initialize Firestore client
	fsClient, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore init: %v", err)
	}

	// Load Horizon taxonomy from Firestore and keep it cached (single source of truth).
	taxonomy.InitRuntimeTaxonomyCacheFromFirestore(fsClient.Client(), "v1", 10*time.Minute)
	if err := taxonomy.RefreshRuntimeTaxonomy(ctx); err != nil {
		log.Printf("WARNING: failed to load horizon taxonomy: %v", err)
	}
	taxonomy.StartRuntimeTaxonomyAutoRefresh(ctx, 5*time.Minute, func(err error) {
		if err != nil {
			log.Printf("WARNING: horizon taxonomy refresh failed: %v", err)
		}
	})
	taxonomy.InitIndustryRegistryCacheFromFirestore(fsClient.Client(), "v1", 20*time.Minute)
	if err := taxonomy.RefreshIndustryRegistry(ctx); err != nil {
		log.Printf("WARNING: failed to load industry registry: %v", err)
	}
	taxonomy.StartIndustryRegistryAutoRefresh(ctx, 10*time.Minute, func(err error) {
		if err != nil {
			log.Printf("WARNING: industry registry refresh failed: %v", err)
		}
	})

	// Load market segment normalization dictionary from Firestore (used to derive MarketSegmentNorms).
	taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient.Client(), "v1", 20*time.Minute)
	if err := taxonomy.RefreshMarketSegmentDictionary(ctx); err != nil {
		log.Printf("WARNING: failed to load market segment dictionary: %v", err)
	}
	taxonomy.StartMarketSegmentDictionaryAutoRefresh(ctx, 10*time.Minute, func(err error) {
		if err != nil {
			log.Printf("WARNING: market segment dictionary refresh failed: %v", err)
		}
	})

	// Initialize GCS client for uploads
	gcsClient, err := gcp.NewGCSClient(ctx, projectID)
	if err != nil {
		log.Fatalf("gcs init: %v", err)
	}

	logger := log.New(os.Stdout, "[sanskrite] ", log.LstdFlags|log.Lshortfile)

	var horizonSimulationSubmitter *agentmeshsubmitter.Service
	{
		meshStore := agentmeshrepo.NewStore(fsClient.Client(), gcsClient)
		baseURL := strings.TrimRight(strings.TrimSpace(getEnv("AGENTMESH_PUBLIC_BASE_URL", "")), "/")
		if baseURL == "" {
			baseURL = strings.TrimRight(strings.TrimSpace(getEnv("CLOUD_RUN_URL", "")), "/")
		}
		meshRegistry := agentmeshregistry.NewService(agentmeshregistry.NewHMACSigner(getEnv("AGENTMESH_SERVER_SIGNING_SECRET", "server-local-secret"), "server-local"))
		if err := meshRegistry.BootstrapSystemAgents(ctx, baseURL); err != nil {
			logger.Printf("WARNING: local AgentMesh registry bootstrap for Horizon simulations failed: %v", err)
		} else {
			meshControl := agentmeshcontrol.NewService(meshRegistry, agentmeshpolicy.NewService())
			var meshDispatcher *agentmeshtasks.Dispatcher
			taskTargetURL := strings.TrimSpace(getEnv("AGENTMESH_TASK_TARGET_URL", ""))
			if taskTargetURL != "" {
				taskOIDCSA := getEnv("AGENTMESH_TASKS_OIDC_SERVICE_ACCOUNT", "backend@"+projectID+".iam.gserviceaccount.com")
				if enabledEnv("AGENTMESH_TASKS_DISABLE_OIDC") {
					taskOIDCSA = ""
				}
				meshDispatcher, err = agentmeshtasks.NewDispatcher(
					ctx,
					projectID,
					getEnv("AGENTMESH_TASKS_LOCATION", "us-central1"),
					getEnv("AGENTMESH_TASKS_QUEUE", "nv-horizon-agentmesh"),
					taskTargetURL,
					taskOIDCSA,
					getEnv("AGENTMESH_TASKS_OIDC_AUDIENCE", ""),
				)
				if err != nil {
					logger.Printf("WARNING: local AgentMesh dispatcher for Horizon simulations failed: %v", err)
					meshDispatcher = nil
				}
			}
			meshRuntime := agentmeshruntime.NewService(
				meshStore,
				meshControl,
				nil,
				nil,
				meshDispatcher,
				getEnv("AGENTMESH_ARTIFACT_BUCKET", projectID+"-agentmesh-artifacts"),
				logger,
			)
			horizonSimulationSubmitter = agentmeshsubmitter.NewService(meshRuntime)
		}
	}

	s := &server{fbApp: fbApp, fs: fsClient, logger: logger}
	// Repositories
	s.users = repo.NewUsersRepository(fsClient.Client())
	s.projs = repo.NewProjectsRepository(fsClient.Client())
	s.dsrcs = repo.NewDataSourcesRepository(fsClient.Client())
	s.sprofs = repo.NewSourceProfilesRepository(fsClient.Client())
	// Ontology repositories
	entitiesRepo := repo.NewEntityRepository(fsClient.Client())
	eventsRepo := repo.NewEventRepository(fsClient.Client())
	relationsRepo := repo.NewRelationRepository(fsClient.Client())
	claimsRepo := repo.NewClaimRepository(fsClient.Client())
	conceptsRepo := repo.NewConceptRepository(fsClient.Client())
	metricStateRepo := repo.NewMetricStateRepository(fsClient.Client())
	trendRepo := repo.NewTrendRepository(fsClient.Client())
	causalLinksRepo := repo.NewCausalLinkRepository(fsClient.Client())
	_ = repo.NewScenarioRepository(fsClient.Client())
	_ = repo.NewActionableItemRepository(fsClient.Client())
	sourcesRepo := repo.NewSourceRepository(fsClient.Client())
	trustRepo := repo.NewTrustRepository(fsClient.Client())
	uncertaintyRepo := repo.NewUncertaintyModelRepository(fsClient.Client())
	ontologyFactsRepo := repo.NewOntologyFactsRepository(fsClient.Client())
	// Document indexing: bucket for Documents (GCS → Vertex AI Search)
	// Env RAG_DOCUMENTS_BUCKET kept for backward compatibility
	docsBucket := getEnv("RAG_DOCUMENTS_BUCKET", "")
	var bqClient *gcp.BigQueryClient // Will be initialized later if BigQuery is enabled
	docsRepo := repo.NewDocumentsRepository(fsClient.Client())
	if docsBucket != "" && gcsClient != nil {
		docsRepo = docsRepo.WithStorage(gcsClient, docsBucket)
		logger.Printf("Documents storage enabled: bucket=%s", docsBucket)
	}

	// Artifact templates repo/service/handler (will be wired with AITaskExecutor later)
	artifactTemplatesRepo := repo.NewArtifactTemplatesRepository(fsClient.Client())
	coreDocsRepo := repo.NewCoreDocsRepo(fsClient.Client())
	var artifactTemplateService *services.ArtifactTemplateService
	var artifactTemplatesHandler *httphandlers.ArtifactTemplatesHandler
	var artifactGenerationHandler *httphandlers.ArtifactGenerationHandler
	var artifactOrchestratorService *services.ArtifactOrchestratorService
	var artifactSchedulerService *services.ArtifactSchedulerService
	var signalMonitoringService *services.SignalMonitoringService
	var listeningSignalsHandler *httphandlers.ListeningSignalsHandler
	var documentArtifactSignalsHandler *httphandlers.DocumentArtifactSignalsHandler
	var signalHistoryHandler *httphandlers.SignalHistoryHandler

	// Subscription & Voucher repositories and service
	subsRepo := repo.NewSubscriptionsRepository(fsClient.Client())
	vouchersRepo := repo.NewVouchersRepository(fsClient.Client())
	subscriptionService := services.NewSubscriptionService(subsRepo, vouchersRepo, s.users, logger)

	// Handlers
	s.usersHandler = httphandlers.NewUsersHandler(s.users, logger).WithSubscriptionService(subscriptionService)
	s.projectsHandler = httphandlers.NewProjectsHandler(s.projs, logger).WithDataSources(s.dsrcs)
	s.discoveryHandler = httphandlers.NewDiscoveryHandler(s.projs, s.dsrcs, logger)
	var genaiHandler *httphandlers.GenAIHandler
	// aiExecHandler will be initialized after AITaskExecutor is created
	var aiExecHandler *httphandlers.AIExecHandler
	var aiExec *ai.AITaskExecutor
	var metaHandler *httphandlers.MetaHandler
	var agentsHandler *httphandlers.AgentsHandler
	var agentJobsHandler *httphandlers.AgentJobsHandler
	var agentJobsRepo *repo.AgentJobsRepository
	var verifiedHandler *httphandlers.VerifiedHandler
	var bigqueryHandler *httphandlers.BigQueryHandler
	var bigqueryAIService *services.BigQueryAIService
	var horizonService *horizon.HorizonService
	var horizonHandlers *horizonhttp.HorizonHandlers
	var oneHopDownstreamService *horizonsupplychain.OneHopDownstreamService
	var profilesHandler *horizonhttp.ProfilesHandler
	var profileGenerator *horizonprofile.Generator
	var profileScheduler *horizonprofile.Scheduler
	var enforcementHandlers *horizonhttp.EnforcementHandlers
	var scoutEngine *horizonscout.ScoutEngine
	var riskPipeline *horizonrisk.Pipeline
	var riskJobsRepo *horizonrepo.RiskJobsRepository
	var horizonTelemetryRepo *horizonrepo.HorizonTelemetryRepository
	var canvasIngester *discovery.CanvasIngester
	sourcesDocsRepo := repo.NewDocumentsRepository(fsClient.Client())
	// Note: bqClient will be initialized later, BigQuery sync will be configured after initialization
	sourcesHandler := httphandlers.NewSourcesHandler(repo.NewSourceProjectsRepository(fsClient.Client()), s.dsrcs, logger).WithDocuments(sourcesDocsRepo)

	// Documents handler for artifact queries (created early to use in routes)
	artifactsDocsRepo := docsRepo // Use the same repo instance with BigQuery sync if enabled
	entityRepo := repo.NewEntityRepository(fsClient.Client())
	eventRepo := repo.NewEventRepository(fsClient.Client())
	claimRepo := repo.NewClaimRepository(fsClient.Client())
	// Note: bqClient will be initialized later, BigQuery sync will be configured after initialization
	documentsHandler := httphandlers.NewDocumentsHandler(
		artifactsDocsRepo,
		entityRepo,
		eventRepo,
		claimRepo,
		logger,
	)

	searchEventsRepo := repo.NewSearchEventsRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())
	rulesRepo := repo.NewRulesRepository(fsClient.Client())
	rulesHandler := httphandlers.NewRulesHandler(rulesRepo, logger)
	webhooksHandler := httphandlers.NewWebhooksHandler(logger)

	// Wire orchestrator (AITaskExecutor + ModelManager + Vertex service)
	modelManager := ai.NewAIModelManager()
	var sourcePoller *discovery.SourcePoller
	var ingestHistoryRepo *repo.SourceIngestHistoryRepository
	var sourceRuntimeRepo *repo.SourceRuntimeStatesRepository

	// For brevity, only Vertex provider is wired here; additional providers can be added
	{
		aiServices := []ai.AIService{}
		vertexProjectID := strings.TrimSpace(getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", "")))
		vertexLocation := strings.TrimSpace(getEnv("VERTEX_LOCATION", "us-central1"))
		geminiAPIKey := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, vertexProjectID, "GEMINI_API_KEY", "gemini_api_key"))
		if geminiAPIKey != "" && os.Getenv("GEMINI_API_KEY") == "" {
			_ = os.Setenv("GEMINI_API_KEY", geminiAPIKey)
		}
		if vertexProjectID != "" {
			vertexService, err := ai.NewVertexAIService(vertexProjectID, vertexLocation, modelManager)
			if err != nil {
				log.Printf("HZN_GROUNDING WARN: component=bootstrap phase=vertex_service_init reasonCode=vertex_service_init_failed errorClass=provider_init_error err=%v", err)
			} else {
				if ds := getEnvWithAlt("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID", "VERTEX_SEARCH_HORIZON_DATA_STORE_ID", ""); ds != "" {
					vertexService = vertexService.WithDataStore(ds)
				}
				aiServices = append(aiServices, vertexService)
			}
		} else {
			log.Printf("HZN_GROUNDING WARN: component=bootstrap phase=vertex_service_init reasonCode=missing_project_id errorClass=config_error message=%q", "VertexAIService disabled: missing GCP project id (GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT)")
		}

		if geminiAPIKey != "" {
			aiServices = append(aiServices, ai.NewGeminiService(geminiAPIKey, modelManager))
		}
		if k := os.Getenv("OPENAI_API_KEY"); k != "" {
			aiServices = append(aiServices, ai.NewOpenAIService(k, modelManager))
		}
		if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
			aiServices = append(aiServices, ai.NewClaudeService(k, modelManager))
		}
		if k := os.Getenv("GROK_API_KEY"); k != "" {
			aiServices = append(aiServices, ai.NewGrokService(k, modelManager))
		}

		exec := ai.NewAITaskExecutor(aiServices, modelManager).
			WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client())).
			WithCircuitBreaker(ai.NewCircuitBreaker())
		// Admission control: optional via env. Default disabled (NoopAdmissionController).
		if maxTokens, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("AI_ADMISSION_MAX_TOKENS_PER_CASCADE_MIN"))); maxTokens > 0 {
			exec = exec.WithAdmissionController(ai.NewInMemoryAdmissionController(maxTokens))
		} else {
			exec = exec.WithAdmissionController(ai.NoopAdmissionController())
		}
		aiExec = exec
		aiExecHandler = httphandlers.NewAIExecHandler(exec)
		wc := crawler.NewWebCrawler()
		tgBase := strings.TrimSpace(os.Getenv("TELEGRAM_SERVICE_URL"))
		if tgBase == "" {
			log.Printf("WARNING: TELEGRAM_SERVICE_URL is not set - Telegram features will be disabled")
		}
		tgClient := telegram.NewClient(tgBase)
		projectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
		pub, err := realtime.NewPublisher(ctx, projectID)
		if err != nil {
			log.Printf("WARNING: PubSub publisher init failed: %v (realtime updates disabled)", err)
		}
		if err != nil {
			log.Printf("WARNING: PubSub publisher init failed: %v (realtime updates disabled)", err)
		}

		// Wire artifact services with exec and pub
		artifactTemplateService = services.NewArtifactTemplateService(artifactTemplatesRepo, coreDocsRepo, exec)
		artifactTemplatesHandler = httphandlers.NewArtifactTemplatesHandler(artifactTemplateService)

		// Artifact generation services
		interactionsRepo := repo.NewInteractionsRepository(fsClient.Client())
		artifactContextService := services.NewArtifactContextService(interactionsRepo, coreDocsRepo)

		// Create genaiClient for artifact executor (using combined client)
		// Import gen_combined and gen_gemini packages at top of file
		geminiAPIKey = strings.TrimSpace(gcp.GetEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "gemini_api_key"))
		if geminiAPIKey != "" && os.Getenv("GEMINI_API_KEY") == "" {
			_ = os.Setenv("GEMINI_API_KEY", geminiAPIKey)
		}
		fastModel := os.Getenv("GENAI_FAST_MODEL")
		qualityModel := os.Getenv("GENAI_QUALITY_MODEL")
		geminiClient := gen_gemini.NewClient(geminiAPIKey, projectID, vertexLocation, fastModel, qualityModel)
		openaiAPIKey := os.Getenv("OPENAI_API_KEY")
		genaiClient := gen_combined.NewClient(geminiClient, openaiAPIKey)

		// Initialize BigQuery client and AI service
		bigqueryDatasetID := getEnv("BIGQUERY_DATASET_ID", "nv_horizon_documents")
		if bq, err := gcp.NewBigQueryClient(ctx, projectID, bigqueryDatasetID, opts...); err == nil {
			bqClient = bq
			bigqueryAIService = services.NewBigQueryAIService(bqClient, genaiClient, logger)
			bigqueryHandler = httphandlers.NewBigQueryHandler(bigqueryAIService, logger)
			logger.Printf("BigQuery AI service enabled: project=%s, dataset=%s", projectID, bigqueryDatasetID)
			// Wire BigQuery sync to repositories
			docsRepo = docsRepo.WithBigQuerySync(bqClient)
			coreDocsRepo = coreDocsRepo.WithBigQuerySync(bqClient)
			sourcesDocsRepo = sourcesDocsRepo.WithBigQuerySync(bqClient)
			entityRepo = entityRepo.WithBigQuerySync(bqClient)
			eventRepo = eventRepo.WithBigQuerySync(bqClient)
			claimRepo = claimRepo.WithBigQuerySync(bqClient)
			logger.Printf("BigQuery sync enabled for documents, core docs, sources, and ontology artifacts")
			// Note: BigQuery client will be closed when server shuts down
		} else {
			logger.Printf("BigQuery AI service disabled: %v", err)
		}

		artifactExecutorService := services.NewArtifactExecutorService(genaiClient, artifactContextService, pub).
			WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client()), modelManager)

		generationSessionsRepo := repo.NewGenerationSessionsRepository(fsClient.Client())
		artifactOrchestratorService = services.NewArtifactOrchestratorService(generationSessionsRepo, coreDocsRepo, artifactExecutorService, artifactContextService)
		// Wire storage saver for artifacts (use artifacts handler as saver)
		artifactsHandlerForSaver := httphandlers.NewArtifactsHandler(coreDocsRepo).WithStorage(gcsClient, bucket)
		artifactOrchestratorService = artifactOrchestratorService.WithStorageSaver(artifactsHandlerForSaver)
		artifactGenerationHandler = httphandlers.NewArtifactGenerationHandler(artifactOrchestratorService)

		// Initialize signal repositories and handlers
		listeningSignalsRepo := repo.NewListeningSignalsRepository(fsClient.Client())
		documentArtifactSignalsRepo := repo.NewDocumentArtifactSignalsRepository(fsClient.Client())
		signalHistoryRepo := repo.NewSignalHistoryRepository(fsClient.Client())
		signalSnapshotRefsRepo := repo.NewSignalSnapshotRefsRepository(fsClient.Client())
		listeningSignalsHandler = httphandlers.NewListeningSignalsHandler(listeningSignalsRepo)
		documentArtifactSignalsHandler = httphandlers.NewDocumentArtifactSignalsHandler(documentArtifactSignalsRepo)
		signalHistoryHandler = httphandlers.NewSignalHistoryHandler(signalHistoryRepo, documentArtifactSignalsRepo)

		// Initialize signal monitoring service
		signalMonitoringService = services.NewSignalMonitoringService(
			listeningSignalsRepo,
			signalSnapshotRefsRepo,
			coreDocsRepo,
			docsRepo,
			exec,
		).WithGenAIClient(genaiClient) // Wire GenAI client for structured output

		// Configure thresholds from env vars
		if minRelevance := os.Getenv("SIGNAL_MIN_RELEVANCE_SCORE"); minRelevance != "" {
			if score, err := strconv.ParseFloat(minRelevance, 64); err == nil && score >= 0 && score <= 1 {
				signalMonitoringService = signalMonitoringService.WithRelevanceThreshold(score)
			}
		}
		if minConfidence := os.Getenv("SIGNAL_MIN_CONFIDENCE"); minConfidence != "" {
			if score, err := strconv.ParseFloat(minConfidence, 64); err == nil && score >= 0 && score <= 1 {
				signalMonitoringService = signalMonitoringService.WithConfidenceThreshold(score)
			}
		}

		// Set check interval from env var (default: 6 hours)
		if checkInterval := os.Getenv("SIGNAL_MONITORING_INTERVAL"); checkInterval != "" {
			if parsed, err := time.ParseDuration(checkInterval); err == nil && parsed >= 5*time.Minute {
				signalMonitoringService = signalMonitoringService.WithCheckInterval(parsed)
			} else {
				log.Printf("invalid SIGNAL_MONITORING_INTERVAL: %s, using default 6h", checkInterval)
			}
		}

		// Initialize artifact scheduler service
		artifactSchedulerService = services.NewArtifactSchedulerService(coreDocsRepo, artifactOrchestratorService)
		// Set check interval from env var (default: 1 hour)
		if checkInterval := os.Getenv("ARTIFACT_SCHEDULER_INTERVAL"); checkInterval != "" {
			if parsed, err := time.ParseDuration(checkInterval); err == nil && parsed >= 5*time.Minute {
				artifactSchedulerService = artifactSchedulerService.WithCheckInterval(parsed)
			} else {
				log.Printf("invalid ARTIFACT_SCHEDULER_INTERVAL: %s, using default 1h", checkInterval)
			}
		}
		// Wire ingest history repository EARLY to ensure poller has access to it
		ingestHistoryRepo = repo.NewSourceIngestHistoryRepository(fsClient.Client())
		sourceRuntimeRepo = repo.NewSourceRuntimeStatesRepository(fsClient.Client())

		orchestrator := discovery.NewOrchestrator(exec, wc, tgClient, s.projs, s.dsrcs, pub, logger).WithProfiles(s.sprofs).WithSourceProjects(sourceProjectsRepo)
		sourcePoller = discovery.NewSourcePoller(orchestrator, logger).WithRuntimeStates(sourceRuntimeRepo)
		// Provider registry
		reg := prov.NewRegistry()
		reg.Register(prov.NewRSSProvider())
		// Embedding provider (OpenAI small)
		emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
		_ = emb // wired into orchestrator when ingest endpoints are added
		orchestrator = orchestrator.WithDocumentIngest(reg, docsRepo).WithEmbedding(emb)

		orchestrator = orchestrator.WithIngestHistory(ingestHistoryRepo)
		// We also wire it to sourcesHandler later, but orchestrator needs it now

		// Initialize Vertex AI Search client early (needed for GenAI CoreDocs, DiscoveryHandler, Horizon fragment sync, QA alternatives).
		// Same config as horizon fragment indexing: VERTEX_SEARCH_DATA_STORE_ID or VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID / VERTEX_SEARCH_HORIZON_DATA_STORE_ID.
		vertexSearchProjectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
		vertexSearchLocation := getEnv("VERTEX_SEARCH_LOCATION", "global")
		vertexSearchDataStoreID := getEnv("VERTEX_SEARCH_DATA_STORE_ID", "")
		if vertexSearchDataStoreID == "" {
			vertexSearchDataStoreID = getEnvWithAlt("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID", "VERTEX_SEARCH_HORIZON_DATA_STORE_ID", "")
		}
		if vertexSearchDataStoreID == "" && strings.TrimSpace(vertexSearchProjectID) != "" {
			vertexSearchDataStoreID = gcp.GetEnvOrSecret(context.Background(), vertexSearchProjectID, "VERTEX_SEARCH_DATA_STORE_ID", "vertex_search_data_store_id")
		}
		var vertexSearchClient *ai.VertexAISearchClient
		if vertexSearchDataStoreID != "" {
			vertexSearchClient = ai.NewVertexAISearchClient(vertexSearchProjectID, vertexSearchLocation, vertexSearchDataStoreID, "")
			logger.Printf("Vertex AI Search enabled: project=%s, location=%s, dataStore=%s", vertexSearchProjectID, vertexSearchLocation, vertexSearchDataStoreID)
		}

		// Initialize GenAI handler with usage metrics and CoreDocs tools after all dependencies are ready
		genaiHandler = httphandlers.NewGenAIHandler().
			WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client())).
			WithModelManager(modelManager).
			WithAIExecutor(exec). // Wire task executor for model mode (structured tasks)
			WithCoreDocsTools(coreDocsRepo, interactionsRepo, docsRepo, vertexSearchClient, gcsClient, bucket).
			WithGraphTools(true) // Enable graph tools

		// Initialize Tavily client (shared for both Research mode and Web Search tools)
		// Single source of truth: initialized once and reused
		tavilyClient := httphandlers.GetTavilyClientFromEnv()
		if tavilyClient != nil {
			genaiHandler = genaiHandler.WithTavilyClient(tavilyClient)
			logger.Printf("Tavily client initialized (shared for Research mode and Web Search tools)")

			// Add Tavily client to signal monitoring service if it exists
			if signalMonitoringService != nil {
				tavilyAdapter := &tavilyAdapterForSignalMonitoring{client: tavilyClient}
				signalMonitoringService = signalMonitoringService.WithTavilyClient(tavilyAdapter)
				logger.Printf("Signal monitoring service: Tavily web search enabled")
			}
		} else {
			logger.Printf("Tavily client disabled: TAVILY_API_KEY not set (Research mode and Web Search will not work)")
		}

		// Universal external data cache (Firestore).
		// Used by external providers to avoid repeated calls and stabilize normalization.
		externalCacheRepo := repo.NewExternalDataCacheRepository(fsClient.Client())

		// Shared Redis client (L1 cache for external data, fragments, skeleton, edge dedup).
		redisClient := cache.NewRedisClient()
		if redisClient != nil {
			externalCacheRepo.WithRedis(redisClient)
			logger.Printf("Redis client initialized (L1 cache enabled)")
		} else {
			logger.Printf("Redis not configured (REDIS_ADDR/REDIS_URL/REDIS_HOST not set; L1 cache disabled)")
		}

		// Initialize Wikidata client (for Knowledge Search tool)
		// Wikidata doesn't require API key - uses public API
		wikidataClient := httphandlers.GetWikidataClientFromEnv()
		if wikidataClient != nil {
			genaiHandler = genaiHandler.WithWikidataClient(wikidataClient)
			logger.Printf("Wikidata client initialized (Knowledge Search tool enabled)")
		} else {
			logger.Printf("Wikidata client disabled: initialization failed")
		}

		// Initialize GeoNames client (for deterministic geo hierarchy normalization)
		// Full chain (adapter + telemetry + cache) is built in Horizon block when horizonTelemetryRepo is available.
		geonamesClient := httphandlers.GetGeoNamesClientFromEnv()
		var geonamesClientForHorizon ontology.GeoNamesClientInterface
		if geonamesClient != nil {
			gnUser := strings.TrimSpace(os.Getenv("GEONAMES_USERNAME"))
			if gnUser == "" {
				gnUser = strings.TrimSpace(os.Getenv("HORIZON_GEONAMES_USERNAME"))
			}
			if gnUser == "" {
				gnUser = httphandlers.DefaultGeoNamesUsername
			}
			logger.Printf("GeoNames client initialized (geo hierarchy resolver enabled): username=%s", gnUser)
			// geonamesClientForHorizon built in Horizon block with telemetry wrapper
		} else {
			logger.Printf("GeoNames client disabled: GEONAMES_USERNAME not set")
		}

		// Initialize Ontology tools
		genaiHandler = genaiHandler.WithOntologyTools(
			entitiesRepo,
			eventsRepo,
			claimsRepo,
			relationsRepo,
			trendRepo,
			causalLinksRepo,
			metricStateRepo,
			uncertaintyRepo,
			sourcesRepo,
			trustRepo,
			conceptsRepo,
		)
		logger.Printf("Ontology tools enabled")

		// ============================================================================
		// Ontology System Initialization
		// ============================================================================

		// EntityResolver для разрешения дубликатов entities
		entityResolver := ontology.NewEntityResolver(entitiesRepo, emb, genaiClient, exec, logger)

		// TrustIntegrator для интеграции Source Trust
		trustIntegrator := ontology.NewTrustIntegrator(sourcesRepo, claimsRepo, eventsRepo, trustRepo, logger)

		// ConflictDetector для обнаружения конфликтов
		conflictDetector := ontology.NewConflictDetector(claimsRepo, uncertaintyRepo, trustIntegrator, logger)

		// CausalDetector для обнаружения причинно-следственных связей
		causalDetector := ontology.NewCausalDetector(eventsRepo, causalLinksRepo, genaiClient, exec, logger)

		// DomainService для работы с доменами
		domainService := domain.NewDomainService(conceptsRepo, relationsRepo, genaiClient, exec, logger)

		// EntityEnricher для обогащения из Wikidata
		var entityEnricher *ontology.EntityEnricher
		if wikidataClient != nil {
			wikidataAdapter := wikidata.NewAdapter(wikidataClient)
			var wdForEnricher ontology.WikidataClientInterface = wikidataAdapter
			wdForEnricher = wikidata.NewCachedClient(wdForEnricher, externalCacheRepo, wikidata.CacheOptionsFromEnv())
			entityEnricher = ontology.NewEntityEnricher(entitiesRepo, wdForEnricher, logger)
		}

		// CanonizationPolicy for path normalization (shared with Horizon; unified rules)
		canonizationRulesRepo := horizonrepo.NewCanonizationRulesRepository(fsClient.Client())
		canonizationPolicy := horizonpolicy.NewCanonizationPolicy(canonizationRulesRepo)

		// OntologyExtractor - главный компонент для извлечения онтологических объектов.
		// ВАЖНО: он должен быть инициализирован ДО старта document/chat воркеров, иначе факты не будут создаваться.
		ontologyExtractor := ontology.NewOntologyExtractor(
			entitiesRepo,
			eventsRepo,
			claimsRepo,
			relationsRepo,
			sourcesRepo,
			ontologyFactsRepo,
			entityResolver,
			entityEnricher,
			trustIntegrator,
			conflictDetector,
			causalDetector,
			domainService,
			logger,
		).WithCanonizationPolicy(adapters.NewCanonizationPolicyAdapter(canonizationPolicy))

		// Инициализация стандартной иерархии доменов для "system" tenant
		go func() {
			if err := domainService.InitializeHierarchy(context.Background(), "system"); err != nil {
				logger.Printf("WARNING: Failed to initialize domain hierarchy: %v", err)
			} else {
				logger.Printf("Domain hierarchy initialized for system tenant")
			}
		}()

		s.discoveryHandler = s.discoveryHandler.WithDocuments(docsRepo).WithSearchEvents(searchEventsRepo).WithSourceProjects(sourceProjectsRepo).WithOrchestrator(orchestrator)
		s.discoveryHandler = s.discoveryHandler.WithSourcePoller(sourcePoller)
		if vertexSearchClient != nil {
			s.discoveryHandler = s.discoveryHandler.WithVertexSearch(vertexSearchClient)
		}

		// Настраиваем автоматическую синхронизацию документов с Vertex AI Search.
		// Передаём конкретный gs:// URI (не wildcard), чтобы не превышать лимит Discovery Engine (1000 файлов).
		if vertexSearchClient != nil && docsBucket != "" {
			docsRepo = docsRepo.WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
				if strings.TrimSpace(gcsURI) == "" {
					return "", nil
				}
				operationName, err := vertexSearchClient.ImportDocumentsFromURIs(ctx, []string{gcsURI}, "INCREMENTAL", "content")
				if err != nil {
					logger.Printf("[VertexAISearch] Failed to trigger sync for %s: %v", gcsURI, err)
					return "", err
				}
				logger.Printf("[VertexAISearch] Sync triggered: operation=%s", operationName)
				return operationName, nil
			})
			logger.Printf("Vertex AI Search auto-sync enabled: documents will sync automatically after GCS upload")
		}

		// Start document processor worker (background)
		if pub != nil {
			psClient := pub.Client()
			// Передаем OntologyExtractor в DocumentProcessor
			// Используем агрегированный топик nv_horizon.artifact.events, куда публикуются события EventDocProcess
			worker := discovery.NewDocumentProcessor(psClient, string(realtime.TopicArtifactEvents), docsRepo, rulesRepo, exec, ontologyExtractor, logger)
			go func() {
				ctxW, cancelW := context.WithCancel(ctx)
				defer cancelW()
				if err := worker.Start(ctxW); err != nil {
					log.Printf("document processor stopped: %v", err)
				}
			}()

			// Start facts extractor worker (background) — document.extract → OntologyFacts (RSS facts-only path)
			ndExtractor := extraction.NewNormalizedDocumentExtractor(exec, ontologyExtractor)
			factsExtractor := discovery.NewFactsExtractor(psClient, string(realtime.TopicArtifactEvents), ndExtractor, logger)
			go func() {
				ctxF, cancelF := context.WithCancel(ctx)
				defer cancelF()
				if err := factsExtractor.Start(ctxF); err != nil {
					log.Printf("facts extractor stopped: %v", err)
				}
			}()

			// Start grounding processor worker (background)
			// Subscribe to aggregated operations topic and filter by eventType in the worker.
			grounder := discovery.NewGroundingProcessor(psClient, string(realtime.TopicOperationsEvents), orchestrator, logger)
			go func() {
				ctxG, cancelG := context.WithCancel(ctx)
				defer cancelG()
				if err := grounder.Start(ctxG); err != nil {
					log.Printf("grounding processor stopped: %v", err)
				}
			}()

			// Periodic publisher loop (fallback if Cloud Scheduler not used)
			// Can be disabled via DISABLE_POLLER_TICKER=true when using Cloud Scheduler
			if os.Getenv("DISABLE_POLLER_TICKER") != "true" {
				go func() {
					ctxPP, cancelPP := context.WithCancel(ctx)
					defer cancelPP()
					// Default interval: 1 hour to reduce CPU/billing load
					// Cloud Scheduler should be preferred for production
					// Can be overridden via POLLER_TICKER_INTERVAL env var (e.g., "5m", "30m", "1h")
					interval := 1 * time.Hour
					if customInterval := os.Getenv("POLLER_TICKER_INTERVAL"); customInterval != "" {
						if parsed, err := time.ParseDuration(customInterval); err == nil && parsed >= 1*time.Minute {
							interval = parsed
						} else {
							log.Printf("invalid POLLER_TICKER_INTERVAL: %s, using default 1h", customInterval)
						}
					}
					t := time.NewTicker(interval)
					defer t.Stop()
					for {
						select {
						case <-ctxPP.Done():
							return
						case <-t.C:
							_ = sourcePoller.CoordinateDue(ctxPP)
						}
					}
				}()
			}

			// ChatOntologyProcessor для асинхронной обработки чата
			if interactionsRepo != nil {
				chatOntologyProcessor := ontology.NewChatOntologyProcessor(ontologyExtractor, genaiClient, exec, interactionsRepo, logger)
				// Создаем подписку на chat.ontology.process события
				chatTopic := psClient.Topic(string(realtime.TopicInteractionEvents))
				chatSubID := "chat.ontology.process.worker"
				chatSub := psClient.Subscription(chatSubID)
				chatSubExists, _ := chatSub.Exists(ctx)
				if !chatSubExists {
					if chatTopicExists, _ := chatTopic.Exists(ctx); chatTopicExists {
						if createdSub, err := psClient.CreateSubscription(ctx, chatSubID, pubsub.SubscriptionConfig{
							Topic: chatTopic,
						}); err == nil {
							chatSub = createdSub
							logger.Printf("Created subscription %s for chat ontology processing", chatSubID)
						}
					}
				}
				if chatSubExists || chatSub != nil {
					go func() {
						ctxChat, cancelChat := context.WithCancel(ctx)
						defer cancelChat()
						if err := chatOntologyProcessor.Start(ctxChat, chatSub); err != nil {
							logger.Printf("chat ontology processor stopped: %v", err)
						}
					}()
					logger.Printf("Chat ontology processor started")
				}
			}

			// Wire HTTP handler to allow Cloud Scheduler HTTP trigger
			webhooksHandler = webhooksHandler.WithPoller(sourcePoller)
			// Attach publisher to GenAI handler for streaming events via WithPublisher
			genaiHandler = genaiHandler.WithPublisher(pub)
		}

		// Subscription billing cycle processor (runs daily)
		// Can be disabled via DISABLE_SUBSCRIPTION_BILLING=true when using Cloud Scheduler
		if os.Getenv("DISABLE_SUBSCRIPTION_BILLING") != "true" {
			go func() {
				ctxBilling, cancelBilling := context.WithCancel(ctx)
				defer cancelBilling()
				// Default interval: 24 hours (daily billing)
				// Cloud Scheduler should be preferred for production
				// Can be overridden via SUBSCRIPTION_BILLING_INTERVAL env var (e.g., "1h", "24h")
				interval := 24 * time.Hour
				if customInterval := os.Getenv("SUBSCRIPTION_BILLING_INTERVAL"); customInterval != "" {
					if parsed, err := time.ParseDuration(customInterval); err == nil && parsed >= 1*time.Hour {
						interval = parsed
					} else {
						log.Printf("invalid SUBSCRIPTION_BILLING_INTERVAL: %s, using default 24h", customInterval)
					}
				}
				t := time.NewTicker(interval)
				defer t.Stop()
				// Run immediately on startup
				if err := subscriptionService.ProcessBillingCycle(ctxBilling); err != nil {
					log.Printf("subscription billing cycle failed: %v", err)
				}
				for {
					select {
					case <-ctxBilling.Done():
						return
					case <-t.C:
						if err := subscriptionService.ProcessBillingCycle(ctxBilling); err != nil {
							log.Printf("subscription billing cycle failed: %v", err)
						}
					}
				}
			}()
		}

		// Artifact scheduler (runs periodically to check for scheduled updates)
		// Can be disabled via DISABLE_ARTIFACT_SCHEDULER=true when using Cloud Scheduler
		if os.Getenv("DISABLE_ARTIFACT_SCHEDULER") != "true" && artifactSchedulerService != nil {
			go func() {
				ctxScheduler, cancelScheduler := context.WithCancel(ctx)
				defer cancelScheduler()
				artifactSchedulerService.Start(ctxScheduler)
			}()
			logger.Printf("Artifact scheduler started (check interval: %v)", artifactSchedulerService.CheckInterval())
		}

		// Signal monitoring service (runs periodically to monitor signals and create snapshots)
		// Can be disabled via DISABLE_SIGNAL_MONITORING=true
		if os.Getenv("DISABLE_SIGNAL_MONITORING") != "true" && signalMonitoringService != nil {
			go func() {
				ctxMonitor, cancelMonitor := context.WithCancel(ctx)
				defer cancelMonitor()
				signalMonitoringService.Start(ctxMonitor)
			}()
			logger.Printf("Signal monitoring service started (check interval: %v)", signalMonitoringService.CheckInterval())
		}

		cacheRepo := repo.NewProcessedContentRepository(fsClient.Client())
		interactions := repo.NewInteractionsRepository(fsClient.Client())
		orchestrator = orchestrator.WithCache(cacheRepo).WithInteractions(interactions)

		// Verified URL Analysis handler (initialized after orchestrator and genaiHandler are ready)
		sharesRepo := repo.NewShareTokensRepository(fsClient.Client())
		verifiedHandler = httphandlers.NewVerifiedHandler(
			s.projs,
			sharesRepo,
			interactions,
			s.dsrcs,
			docsRepo,
			orchestrator,
			genaiHandler,
			pub,
			logger,
		)
		// Core docs repo - use existing instance with BigQuery sync if available
		// coreDocsRepo was already created above and configured with BigQuery sync
		// Don't create a new instance here, reuse the existing one
		s.projectsHandler = s.projectsHandler.WithCoreDocs(coreDocsRepo).WithStorage(gcsClient, bucket).WithAIExecutor(exec)
		// Wire document indexing for CoreDoc (docs repo + Vertex AI Search)
		if docsRepo != nil {
			s.projectsHandler = s.projectsHandler.WithDocumentIndexing(docsRepo, vertexSearchClient)
			// Configure automatic indexing callback in CoreDocsRepo
			projectsHandlerRef := s.projectsHandler // Capture reference for closure
			coreDocsRepo = coreDocsRepo.WithIndexingCallback(func(ctx context.Context, tenantID string, cd *models.CoreDoc) error {
				return projectsHandlerRef.IndexCoreDoc(ctx, tenantID, cd)
			})
			logger.Printf("Automatic CoreDoc indexing enabled in repository")
		}
		// Wire BigQuery client if configured
		if bqClient != nil {
			s.projectsHandler = s.projectsHandler.WithBigQuery(bqClient)
		}
		// Wire BigQuery AI Service to projects handler for analytics queries
		if bigqueryAIService != nil {
			s.projectsHandler = s.projectsHandler.WithBigQueryAI(bigqueryAIService)
			logger.Printf("BigQuery AI service wired to projects handler for analytics queries")
		}
		// Wire GenAI client to projects handler for Prepare mode
		if genaiClient != nil {
			s.projectsHandler = s.projectsHandler.WithGenAIClient(genaiClient)
			logger.Printf("GenAI client wired to projects handler for Prepare mode")
		}
		// Wire cascade repos to projects handler
		s.projectsHandler = s.projectsHandler.WithCascade(s.sprofs, interactions)
		s.discoveryHandler = s.discoveryHandler.WithOrchestrator(orchestrator).WithInteractions(interactions).WithProfiles(s.sprofs)
		sourcesHandler = sourcesHandler.WithOrchestrator(orchestrator)
		if pub != nil {
			sourcesHandler = sourcesHandler.WithPublisher(pub)
		}
		// Wire ingest history repository (already created and wired to orchestrator above)
		sourcesHandler = sourcesHandler.WithIngestHistory(ingestHistoryRepo)

		if pub != nil {
			s.projectsHandler = s.projectsHandler.WithPublisher(pub)
		}
		// Meta analysis repos and handler
		metaEvents := repo.NewMetaAnalysesRepository(fsClient.Client())
		metaAggProj := repo.NewMetaAggregateProjectsRepository(fsClient.Client())
		metaAggUser := repo.NewMetaAggregateUsersRepository(fsClient.Client())
		metaHandler = httphandlers.NewMetaHandler(s.projs, interactions, metaEvents, metaAggProj, metaAggUser, exec, logger)
		// Wire to projects handler for cascade delete
		s.projectsHandler = s.projectsHandler.WithMeta(metaEvents, metaAggProj, metaAggUser)
		// Wire project scenarios repository
		projectScenariosRepo := repo.NewProjectScenariosRepository(fsClient.Client())
		s.projectsHandler = s.projectsHandler.WithProjectScenarios(projectScenariosRepo)
		// Wire scenario data repository
		scenarioDataRepo := repo.NewScenarioDataRepository(fsClient.Client())
		s.projectsHandler = s.projectsHandler.WithScenarioData(scenarioDataRepo)

		// Initialize Agent Registry (legacy agents: DataAgent, EntitiesAgent)
		agentRegistry := agents.NewRegistry(exec, genaiClient, pub, logger).
			WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client()), modelManager)
		agentsHandler = httphandlers.NewAgentsHandler(agentRegistry, pub, logger)

		// Initialize Agent Orchestrator (new capabilities-based system)
		agentJobsRepo = repo.NewAgentJobsRepository(fsClient.Client())
		capabilityRegistry := agents.NewCapabilityRegistry()

		// Register basic capability executors (используем coreDocsRepo для DocumentRead/Write)
		docReadExecutor := agents.NewDocumentReadExecutor(coreDocsRepo, logger)
		docReadExecutor.WithDocumentsRepository(docsRepo) // Для поиска CoreDoc ID через Document
		capabilityRegistry.Register(docReadExecutor)
		capabilityRegistry.Register(agents.NewLLMGenerateExecutor(genaiClient, logger))
		capabilityRegistry.Register(agents.NewDocumentWriteExecutor(coreDocsRepo, logger))

		// Register advanced capability executors
		if vertexSearchClient != nil {
			capabilityRegistry.Register(agents.NewDataSearchExecutor(vertexSearchClient, docsRepo, logger))
			logger.Printf("DataSearchExecutor registered with Vertex AI Search")
		} else {
			logger.Printf("WARNING: VertexSearchClient not configured, DataSearchExecutor will not be available")
		}

		// Register BigQuery executor if BigQuery is configured
		if bqClient != nil {
			capabilityRegistry.Register(agents.NewBigQueryQueryExecutor(bqClient, logger))
			logger.Printf("BigQueryQueryExecutor registered")
		}

		// Register LLM Analyze executor
		capabilityRegistry.Register(agents.NewLLMAnalyzeExecutor(genaiClient, logger))

		// Register Entity Search executor (uses EntityResolver for deduplication)
		if entityResolver != nil {
			capabilityRegistry.Register(agents.NewEntitySearchExecutor(entitiesRepo, entityResolver, logger))
			logger.Printf("EntitySearchExecutor registered")
		} else {
			logger.Printf("WARNING: EntityResolver not initialized, EntitySearchExecutor will not be available")
		}

		// Register Entity Deduplicate executor
		if entityResolver != nil && genaiClient != nil {
			capabilityRegistry.Register(agents.NewEntityDeduplicateExecutor(entitiesRepo, entityResolver, genaiClient, logger))
			logger.Printf("EntityDeduplicateExecutor registered")
		} else {
			logger.Printf("WARNING: EntityResolver or GenAI client not initialized, EntityDeduplicateExecutor will not be available")
		}

		// Register Entity Relation Build executor
		if entityResolver != nil && relationsRepo != nil {
			capabilityRegistry.Register(agents.NewEntityRelationBuildExecutor(entitiesRepo, relationsRepo, entityResolver, logger))
			logger.Printf("EntityRelationBuildExecutor registered")
		} else {
			logger.Printf("WARNING: EntityResolver or RelationsRepo not initialized, EntityRelationBuildExecutor will not be available")
		}

		// Register Entity Enrich executor
		if entityEnricher != nil {
			capabilityRegistry.Register(agents.NewEntityEnrichExecutor(entitiesRepo, entityEnricher, logger))
			logger.Printf("EntityEnrichExecutor registered")
		} else {
			logger.Printf("WARNING: EntityEnricher not initialized, EntityEnrichExecutor will not be available")
		}

		// Register Entity Extract executor
		if entityResolver != nil && genaiClient != nil {
			capabilityRegistry.Register(agents.NewEntityExtractExecutor(entitiesRepo, entityResolver, genaiClient, logger))
			logger.Printf("EntityExtractExecutor registered")
		} else {
			logger.Printf("WARNING: EntityResolver or GenAI client not initialized, EntityExtractExecutor will not be available")
		}

		// Register Entity Hierarchy Build executor
		if entitiesRepo != nil && relationsRepo != nil {
			capabilityRegistry.Register(agents.NewEntityHierarchyBuildExecutor(entitiesRepo, relationsRepo, logger))
			logger.Printf("EntityHierarchyBuildExecutor registered")
		} else {
			logger.Printf("WARNING: EntitiesRepo or RelationsRepo not initialized, EntityHierarchyBuildExecutor will not be available")
		}

		// Register Conflict Detect executor
		if conflictDetector != nil && claimsRepo != nil {
			capabilityRegistry.Register(agents.NewConflictDetectExecutor(conflictDetector, claimsRepo, logger))
			logger.Printf("ConflictDetectExecutor registered")
		} else {
			logger.Printf("WARNING: ConflictDetector or ClaimsRepo not initialized, ConflictDetectExecutor will not be available")
		}

		// Register Causal Link Detect executor
		if causalDetector != nil && eventsRepo != nil {
			capabilityRegistry.Register(agents.NewCausalLinkDetectExecutor(causalDetector, eventsRepo, logger))
			logger.Printf("CausalLinkDetectExecutor registered")
		} else {
			logger.Printf("WARNING: CausalDetector or EventsRepo not initialized, CausalLinkDetectExecutor will not be available")
		}

		// Register Relation Find executor
		if relationsRepo != nil {
			capabilityRegistry.Register(agents.NewRelationFindExecutor(relationsRepo, logger))
			logger.Printf("RelationFindExecutor registered")
		} else {
			logger.Printf("WARNING: RelationsRepo not initialized, RelationFindExecutor will not be available")
		}

		// Register Entity Validate executor
		if entitiesRepo != nil {
			capabilityRegistry.Register(agents.NewEntityValidateExecutor(entitiesRepo, logger))
			logger.Printf("EntityValidateExecutor registered")
		} else {
			logger.Printf("WARNING: EntitiesRepo not initialized, EntityValidateExecutor will not be available")
		}

		// Register Entity Merge Attributes executor
		if entitiesRepo != nil {
			capabilityRegistry.Register(agents.NewEntityMergeAttributesExecutor(entitiesRepo, logger))
			logger.Printf("EntityMergeAttributesExecutor registered")
		} else {
			logger.Printf("WARNING: EntitiesRepo not initialized, EntityMergeAttributesExecutor will not be available")
		}

		// Register WebSearch executor (Tavily) if available
		// Используем уже инициализированный tavilyClient из строки 270
		var agentTavilyAdapter agents.TavilySearchClient
		if tavilyClient != nil {
			// Создать адаптер для TavilyClient
			agentTavilyAdapter = &tavilyClientAdapterForAgents{client: tavilyClient}
			capabilityRegistry.Register(agents.NewWebSearchExecutor(agentTavilyAdapter, logger))
			logger.Printf("WebSearchExecutor registered with Tavily")
		} else {
			logger.Printf("WARNING: TAVILY_API_KEY not set, WebSearchExecutor will not be available")
		}

		// Create agent orchestrator (используем другое имя чтобы избежать конфликта с discovery.Orchestrator)
		agentOrchestrator := agents.NewAgentOrchestrator(agentJobsRepo, capabilityRegistry, pub, logger).
			WithRepositories(coreDocsRepo, sourcesDocsRepo, entityRepo, eventRepo, interactions)

		// Configure Tavily for orchestrator if available (через адаптер)
		if agentTavilyAdapter != nil {
			agentOrchestrator = agentOrchestrator.WithTavily(agentTavilyAdapter)
		}

		// BigQuery и GenAI клиенты уже инжектируются через injectDependencies в orchestrator
		// на основе capability, поэтому отдельная конфигурация не требуется

		// Create AgentJobsHandler
		agentJobsHandler = httphandlers.NewAgentJobsHandler(agentOrchestrator, agentJobsRepo, pub, logger)
		logger.Printf("Agent Orchestrator initialized")

		// Horizon repositories
		horizonFragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
		if redisClient != nil {
			horizonFragmentsRepo.WithRedis(redisClient)
		}
		horizonFramesRepo := horizonrepo.NewFramesRepository(fsClient.Client())
		horizonLensesRepo := horizonrepo.NewLensesRepository(fsClient.Client())
		horizonEdgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
		horizonEdgesRepo.WithFragmentsRepo(horizonFragmentsRepo)
		productDictionary := horizonproductdict.NewRuntime(fsClient.Client()).WithTenantID("global")
		if redisClient != nil {
			productDictionary.WithRedis(redisClient)
		}
		horizonFragmentsRepo.WithProductDictionary(productDictionary)
		horizonEdgesRepo.WithProductDictionary(productDictionary)
		mechanismDictionary := horizonmechanismdict.NewRuntime(fsClient.Client())
		govDictionary := horizongovdict.NewRuntime(fsClient.Client())
		regulatoryTransformRegistry := horizonregulatorytransform.NewRegistry()
		if rt, err := horizonregulatorytransform.LoadRegistryFromFirestore(context.Background(), fsClient.Client()); err != nil {
			logger.Printf("WARNING: horizon_regulatory_transform_pairs load failed: %v", err)
		} else if rt != nil {
			regulatoryTransformRegistry = rt
		}
		horizonFragmentsRepo.WithMechanismDictionary(mechanismDictionary).WithGovDictionary(govDictionary)
		horizonEdgesRepo.WithRegulatoryTransformRegistry(regulatoryTransformRegistry)
		if parseEnvBool(getEnv("HORIZON_IMPORTANCE_RUNTIME_ENABLED", "true")) {
			importanceScorer := horizonimportance.NewScorer(horizonFragmentsRepo, horizonEdgesRepo)
			importanceUpdater := horizonimportance.NewRuntimeUpdater("global", importanceScorer, horizonFragmentsRepo, 2048)
			importanceUpdater.Start(context.Background(), 2)
			horizonFragmentsRepo.WithOnUpsert(func(ctx context.Context, fragment *horizonmodels.HorizonFragment) {
				if fragment == nil {
					return
				}
				importanceUpdater.Enqueue(strings.TrimSpace(fragment.ID))
			})
			horizonEdgesRepo.WithOnUpsert(func(ctx context.Context, edge *horizonmodels.DependencyEdge) {
				if edge == nil {
					return
				}
				importanceUpdater.Enqueue(strings.TrimSpace(edge.SourceFragmentID))
				importanceUpdater.Enqueue(strings.TrimSpace(edge.TargetFragmentID))
			})
			logger.Printf("Horizon importance runtime updater enabled")
		}
		horizonFragmentsRepo.WithEdgesRepo(horizonEdgesRepo)
		horizonFragmentsRepo.WithOnUpsert(func(ctx context.Context, fragment *horizonmodels.HorizonFragment) {
			runtimeReenrichFragment(ctx, fragment, horizonFragmentsRepo, horizonEdgesRepo)
		})
		horizonEdgesRepo.WithOnUpsert(func(ctx context.Context, edge *horizonmodels.DependencyEdge) {
			if edge == nil {
				return
			}
			if srcID := strings.TrimSpace(edge.SourceFragmentID); srcID != "" {
				if src, err := horizonFragmentsRepo.Get(ctx, srcID); err == nil && src != nil {
					runtimeReenrichFragment(ctx, src, horizonFragmentsRepo, horizonEdgesRepo)
				}
			}
			if tgtID := strings.TrimSpace(edge.TargetFragmentID); tgtID != "" && tgtID != strings.TrimSpace(edge.SourceFragmentID) {
				if tgt, err := horizonFragmentsRepo.Get(ctx, tgtID); err == nil && tgt != nil {
					runtimeReenrichFragment(ctx, tgt, horizonFragmentsRepo, horizonEdgesRepo)
				}
			}
		})
		enforcementJobsRepo := horizonrepo.NewEnforcementJobsRepository(fsClient.Client())
		policyCheckResultsRepo := horizonrepo.NewPolicyCheckResultsRepository(fsClient.Client())
		horizonCascadeVisitedRepo := horizonrepo.NewCascadeVisitedRepository(fsClient.Client())
		horizonCascadeEntityRegistryRepo := horizonrepo.NewCascadeEntityRegistryRepository(fsClient.Client())
		horizonCanonicalPathRedirectsRepo := horizonrepo.NewCanonicalPathRedirectsRepository(fsClient.Client())
		horizonFragmentMergeAuditRepo := horizonrepo.NewFragmentMergeAuditRepository(fsClient.Client())
		horizonFragmentsMergeRepo := horizonrepo.NewFragmentsMergeRepository(
			horizonFragmentsRepo,
			horizonEdgesRepo,
			horizonFragmentMergeAuditRepo,
		).WithRedirectsRepo(horizonCanonicalPathRedirectsRepo).
			WithLensesRepo(horizonLensesRepo)
		horizonFragmentsRepo.WithOnUpsert(func(ctx context.Context, fragment *horizonmodels.HorizonFragment) {
			if fragment == nil {
				return
			}
			cp := strings.TrimSpace(fragment.CanonicalPath)
			if cp == "" {
				return
			}
			dups, err := horizonFragmentsRepo.ListByCanonicalPath(ctx, cp)
			if err != nil || len(dups) < 2 {
				return
			}
			canonicalFrag, err := horizonFragmentsRepo.GetByCanonicalPath(ctx, cp)
			if err != nil || canonicalFrag == nil || strings.TrimSpace(canonicalFrag.ID) == "" {
				return
			}
			for _, dup := range dups {
				if dup == nil {
					continue
				}
				dupID := strings.TrimSpace(dup.ID)
				if dupID == "" || dupID == strings.TrimSpace(canonicalFrag.ID) {
					continue
				}
				if _, err := horizonFragmentsMergeRepo.MergeFragment(ctx, strings.TrimSpace(canonicalFrag.ID), dupID, "global", "runtime_on_upsert_collision", "server_on_upsert_collision", "", ""); err != nil {
					logger.Printf("[HorizonFragments] WARNING: runtime canonicalPath collision merge failed path=%s canonical=%s duplicate=%s err=%v", cp, canonicalFrag.ID, dupID, err)
				}
			}
		})
		horizonTelemetryRepo = horizonrepo.NewHorizonTelemetryRepository(fsClient.Client())
		horizonEdgesRepo.WithTelemetryRepo(horizonTelemetryRepo)
		horizonWatchdogRepo := horizonrepo.NewWatchdogConfigsRepository(fsClient.Client())
		horizonScoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
		horizonWorkingProductScopesRepo := horizonrepo.NewWorkingProductScopesRepository(fsClient.Client())
		horizonCapabilityProfilesRepo := horizonrepo.NewCapabilityProfilesRepository(fsClient.Client())
		horizonCapabilityObsRepo := horizonrepo.NewCapabilityObservationsRepository(fsClient.Client())
		horizonSupplyLinkObsRepo := horizonrepo.NewSupplyLinkObservationsRepository(fsClient.Client())
		horizonRouteObsRepo := horizonrepo.NewRouteObservationsRepository(fsClient.Client())
		horizonDerivedRiskProfilesRepo := horizonrepo.NewDerivedRiskProfilesRepository(fsClient.Client())
		horizonJobTelemetryRepo := horizonrepo.NewJobTelemetryRepository(fsClient.Client())
		riskJobsRepo = horizonrepo.NewRiskJobsRepository(fsClient.Client())
		horizonFragmentVersionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fsClient.Client())
		horizonProfilesRepo := horizonrepo.NewFragmentProfilesRepository(fsClient.Client())
		horizonProfilePoliciesRepo := horizonrepo.NewFragmentProfileRefreshPoliciesRepository(fsClient.Client())
		horizonLensArtifactsRepo := horizonrepo.NewLensArtifactsRepository(fsClient.Client())
		horizonMaterializedArtifactsRepo := horizonrepo.NewMaterializedArtifactsRepository(fsClient.Client())
		horizonMaterializedArtifactsRepo.WithProductDictionary(productDictionary).
			WithMechanismDictionary(mechanismDictionary).
			WithGovDictionary(govDictionary)
		horizonSimulationRunsRepo := horizonrepo.NewSimulationRunsRepository(fsClient.Client())
		horizonSimulationWorldsRepo := horizonrepo.NewSimulationWorldsRepository(fsClient.Client())
		horizonSimulationAgentsRepo := horizonrepo.NewSimulationAgentsRepository(fsClient.Client())
		horizonSimulationEventsRepo := horizonrepo.NewSimulationEventsRepository(fsClient.Client())
		horizonSimulationArtifactsRepo := horizonrepo.NewSimulationArtifactsRepository(fsClient.Client())
		horizonDomainAnswerSnapshotsRepo := horizonrepo.NewDomainAnswerSnapshotsRepository(fsClient.Client())
		horizonWatchPacksRepo := horizonrepo.NewWatchPacksRepository(fsClient.Client())
		horizonWatchPortfoliosRepo := horizonrepo.NewWatchPortfoliosRepository(fsClient.Client())
		horizonWatchDigestsRepo := horizonrepo.NewWatchDigestsRepository(fsClient.Client())
		horizonWatchTargetsRepo := horizonrepo.NewWatchTargetsRepository(fsClient.Client())
		horizonWatchBriefsRepo := horizonrepo.NewWatchBriefsRepository(fsClient.Client())
		horizonWatchFeedRepo := horizonrepo.NewWatchFeedEntriesRepository(fsClient.Client())
		horizonWatchPreferencesRepo := horizonrepo.NewWatchPreferencesRepository(fsClient.Client())
		horizonWatchPageSnapshotsRepo := horizonrepo.NewWatchPageSnapshotsRepository(fsClient.Client())
		horizonWatchPageHistoryRepo := horizonrepo.NewWatchPageHistoryRepository(fsClient.Client())
		horizonStrategicRecommendationsRepo := horizonrepo.NewStrategicRecommendationsRepository(fsClient.Client())
		horizonScopeAgentConfigsRepo := horizonrepo.NewScopeAgentConfigsRepository(fsClient.Client())
		horizonScopeAgentStatesRepo := horizonrepo.NewScopeAgentStatesRepository(fsClient.Client())
		horizonScopeAgentRunsRepo := horizonrepo.NewScopeAgentRunsRepository(fsClient.Client())
		horizonAgentProposalsRepo := horizonrepo.NewAgentProposalsRepository(fsClient.Client())
		horizonArbitrationDecisionsRepo := horizonrepo.NewArbitrationDecisionsRepository(fsClient.Client())
		horizonWatchSubjectsRepo := horizonrepo.NewWatchSubjectsRepository(fsClient.Client())
		horizonWatchTriggersRepo := horizonrepo.NewWatchTriggersRepository(fsClient.Client())
		horizonMacroAgentPoliciesRepo := horizonrepo.NewMacroAgentPoliciesRepository(fsClient.Client())

		// Horizon spend ledger (actual GenAI $ attribution) for budget enforcement.
		horizonSpendLedger := horizonrepo.NewHorizonSpendLedger(fsClient.Client())
		// Feed spend increments from AITaskExecutor based on real per-call cost.
		if exec != nil {
			exec.WithHorizonSpendSink(horizonSpendLedger)
		}

		// Scout execution runtime status (Cloud Tasks preferred, Firestore poller fallback).
		scoutExecutionReady := false
		scoutExecutionReason := "scout_dispatch_unavailable"

		// Cloud Tasks dispatcher for Scout jobs (managed queue + autoscaling + retries).
		// Root hot-path jobs and background continuation/cascade jobs use separate queues so
		// background retry storms do not starve first-answer delivery.
		var scoutTasksDispatcher *horizontasks.ScoutCloudTasksDispatcher
		var scoutBackgroundTasksDispatcher *horizontasks.ScoutCloudTasksDispatcher
		{
			enabled := strings.TrimSpace(getEnv("ENABLE_CLOUD_TASKS_SCOUT", "1"))
			if enabled == "1" || strings.EqualFold(enabled, "true") {
				location := strings.TrimSpace(getEnv("CLOUD_TASKS_LOCATION", "us-central1"))
				// NOTE: The original queue name "nv-horizon-scout" may be temporarily unavailable after deletion.
				// Use env override in prod; default to a safe v2 name.
				queue := strings.TrimSpace(getEnv("CLOUD_TASKS_SCOUT_QUEUE", "nv-horizon-scout"))
				backgroundQueue := strings.TrimSpace(getEnv("CLOUD_TASKS_SCOUT_BACKGROUND_QUEUE", "nv-horizon-scout-bg"))
				baseURL := strings.TrimSpace(getEnv("CLOUD_TASKS_TARGET_BASE_URL", ""))
				saEmail := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
				aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
				if baseURL != "" && saEmail != "" {
					targetURL := strings.TrimRight(baseURL, "/") + "/api/v1/internal/tasks/scout/run"
					if aud == "" {
						aud = strings.TrimRight(baseURL, "/")
					}
					if d, err := horizontasks.NewScoutCloudTasksDispatcher(ctx, projectID, location, queue, targetURL, saEmail, aud); err == nil {
						scoutTasksDispatcher = d
						scoutBackgroundTasksDispatcher = d
						if backgroundQueue != "" && !strings.EqualFold(backgroundQueue, queue) {
							if bgd, bgErr := horizontasks.NewScoutCloudTasksDispatcher(ctx, projectID, location, backgroundQueue, targetURL, saEmail, aud); bgErr == nil {
								scoutBackgroundTasksDispatcher = bgd
							} else {
								logger.Printf("WARNING: Cloud Tasks (Scout background) disabled due to init error: %v", bgErr)
							}
						}
						scoutExecutionReady = true
						scoutExecutionReason = "scout_cloud_tasks_enabled"
						logger.Printf("Cloud Tasks (Scout) enabled: queue=%s backgroundQueue=%s location=%s target=%s",
							queue,
							strings.TrimSpace(scoutBackgroundTasksDispatcher.QueueName()),
							location,
							targetURL,
						)
						horizonScoutingJobsRepo.WithOnCreate(func(c context.Context, job *horizonmodels.ScoutingJob) error {
							// Best-effort realtime: job created (for Hook->Assemble "Sled" UI).
							// IMPORTANT: publish with tenantId from the job (security namespace), even though fragments are global.
							if pub != nil && job != nil {
								correlationID := ""
								sessionID := ""
								if job.State != nil {
									if v, ok := job.State["correlationId"].(string); ok {
										correlationID = strings.TrimSpace(v)
									}
									if v, ok := job.State["sessionId"].(string); ok {
										sessionID = strings.TrimSpace(v)
									}
								}
								payload := map[string]interface{}{
									"tenantId":            strings.TrimSpace(job.TenantID),
									"projectId":           strings.TrimSpace(job.ProjectID),
									"lensId":              strings.TrimSpace(job.LensID),
									"jobId":               strings.TrimSpace(job.ID),
									"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
									"strategy":            strings.TrimSpace(job.ScoutingStrategy),
								}
								if correlationID != "" {
									payload["correlationId"] = correlationID
								}
								if sessionID != "" {
									payload["sessionId"] = sessionID
								}
								// Do not fail job creation if realtime publish fails.
								_ = pub.PublishJSON(c, realtime.EventType("horizon.scout.job.created"), strings.TrimSpace(job.TenantID), payload)
							}

							isBackground := false
							if job != nil && job.State != nil {
								if v, ok := job.State["parentJobId"].(string); ok && strings.TrimSpace(v) != "" {
									isBackground = true
								}
								if v, ok := job.State["mode"].(string); ok && strings.TrimSpace(v) == "refine_existing_assembly" {
									isBackground = true
								}
							}
							dispatcher := scoutTasksDispatcher
							delay := time.Duration(0)
							queueClass := "foreground"
							if isBackground && scoutBackgroundTasksDispatcher != nil {
								dispatcher = scoutBackgroundTasksDispatcher
								delay = getEnvDuration("CLOUD_TASKS_SCOUT_BACKGROUND_DELAY", 15*time.Second)
								if delay < 0 {
									delay = 0
								}
								queueClass = "background"
							}

							// Enqueue by job ID; execution will load full state from Firestore.
							if err := dispatcher.EnqueueScoutJobWithDelay(c, job.ID, delay); err != nil {
								return err
							}
							logger.Printf("Cloud Tasks (Scout) enqueued: class=%s queue=%s delay=%s jobId=%s target=%s",
								queueClass,
								dispatcher.QueueName(),
								delay.String(),
								job.ID,
								job.TargetCanonicalPath,
							)
							return nil
						})
					} else {
						scoutExecutionReason = "scout_cloud_tasks_init_failed"
						logger.Printf("WARNING: Cloud Tasks (Scout) disabled due to init error: %v", err)
					}
				} else {
					scoutExecutionReason = "scout_cloud_tasks_not_configured"
					logger.Printf("WARNING: Cloud Tasks (Scout) not configured (need CLOUD_TASKS_TARGET_BASE_URL and CLOUD_TASKS_OIDC_SERVICE_ACCOUNT)")
				}
			} else {
				scoutExecutionReason = "scout_cloud_tasks_disabled"
			}
		}

		// Cloud Tasks dispatcher for Risk jobs (separate queue/lifecycle from Scout).
		var riskTasksDispatcher *horizontasks.RiskCloudTasksDispatcher
		{
			enabled := strings.TrimSpace(getEnv("ENABLE_CLOUD_TASKS_RISK", "1"))
			if enabled == "1" || strings.EqualFold(enabled, "true") {
				location := strings.TrimSpace(getEnv("CLOUD_TASKS_LOCATION", "us-central1"))
				queue := strings.TrimSpace(getEnv("CLOUD_TASKS_RISK_QUEUE", "nv-horizon-risk"))
				baseURL := strings.TrimSpace(getEnv("CLOUD_TASKS_TARGET_BASE_URL", ""))
				saEmail := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
				aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
				if baseURL != "" && saEmail != "" {
					targetURL := strings.TrimRight(baseURL, "/") + "/api/v1/internal/tasks/risk/run"
					if aud == "" {
						aud = strings.TrimRight(baseURL, "/")
					}
					if d, err := horizontasks.NewRiskCloudTasksDispatcher(ctx, projectID, location, queue, targetURL, saEmail, aud); err == nil {
						riskTasksDispatcher = d
						logger.Printf("Cloud Tasks (Risk) enabled: queue=%s location=%s target=%s", queue, location, targetURL)
						if riskJobsRepo != nil {
							riskJobsRepo.WithOnCreate(func(c context.Context, job *horizonmodels.RiskJob) error {
								if pub != nil && job != nil {
									payload := map[string]interface{}{
										"schemaVersion":       "risk.ws.v1",
										"tenantId":            strings.TrimSpace(job.TenantID),
										"projectId":           strings.TrimSpace(job.ProjectID),
										"lensId":              strings.TrimSpace(job.LensID),
										"jobId":               strings.TrimSpace(job.ID),
										"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
										"polarity":            strings.TrimSpace(job.Polarity),
										"state":               string(job.CurrentState),
										"stage":               string(job.CurrentStage),
									}
									_ = pub.PublishJSON(c, realtime.EventType("horizon.risk.job.created"), strings.TrimSpace(job.TenantID), payload)
								}
								if err := riskTasksDispatcher.EnqueueRiskJob(c, job.ID); err != nil {
									return err
								}
								logger.Printf("Cloud Tasks (Risk) enqueued: jobId=%s target=%s polarity=%s", job.ID, job.TargetCanonicalPath, job.Polarity)
								return nil
							})
						}
					} else {
						logger.Printf("WARNING: Cloud Tasks (Risk) disabled due to init error: %v", err)
					}
				} else {
					logger.Printf("WARNING: Cloud Tasks (Risk) not configured (need CLOUD_TASKS_TARGET_BASE_URL and CLOUD_TASKS_OIDC_SERVICE_ACCOUNT)")
				}
			}
		}

		var sourceTasksDispatcher *discovery.SourceCloudTasksDispatcher
		var scopeAgentTasksDispatcher *horizontasks.ScopeAgentCloudTasksDispatcher

		// Cloud Tasks dispatcher for Profile refresh (managed queue + autoscaling + retries).
		var profileTasksDispatcher *horizontasks.ProfileCloudTasksDispatcher
		{
			enabled := strings.TrimSpace(getEnv("ENABLE_CLOUD_TASKS_PROFILE", "1"))
			if enabled == "1" || strings.EqualFold(enabled, "true") {
				location := strings.TrimSpace(getEnv("CLOUD_TASKS_LOCATION", "us-central1"))
				queue := strings.TrimSpace(getEnv("CLOUD_TASKS_PROFILE_QUEUE", "nv-horizon-profile-refresh"))
				baseURL := strings.TrimSpace(getEnv("CLOUD_TASKS_TARGET_BASE_URL", ""))
				saEmail := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
				aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
				if baseURL != "" && saEmail != "" {
					targetURL := strings.TrimRight(baseURL, "/") + "/api/v1/internal/tasks/profile/refresh"
					if aud == "" {
						aud = strings.TrimRight(baseURL, "/")
					}
					if d, err := horizontasks.NewProfileCloudTasksDispatcher(ctx, projectID, location, queue, targetURL, saEmail, aud); err == nil {
						profileTasksDispatcher = d
						logger.Printf("Cloud Tasks (Profile) enabled: queue=%s location=%s target=%s", queue, location, targetURL)
					} else {
						logger.Printf("WARNING: Cloud Tasks (Profile) disabled due to init error: %v", err)
					}
				} else {
					logger.Printf("WARNING: Cloud Tasks (Profile) not configured (need CLOUD_TASKS_TARGET_BASE_URL and CLOUD_TASKS_OIDC_SERVICE_ACCOUNT)")
				}
			}

			// Cloud Tasks dispatcher for per-source polling execution.
			{
				enabled := strings.TrimSpace(getEnv("ENABLE_CLOUD_TASKS_SOURCE", "1"))
				if enabled == "1" || strings.EqualFold(enabled, "true") {
					location := strings.TrimSpace(getEnv("CLOUD_TASKS_LOCATION", "us-central1"))
					queue := strings.TrimSpace(getEnv("CLOUD_TASKS_SOURCE_QUEUE", "nv-horizon-source-poll"))
					baseURL := strings.TrimSpace(getEnv("CLOUD_TASKS_TARGET_BASE_URL", ""))
					saEmail := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
					aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
					if baseURL != "" && saEmail != "" {
						targetURL := strings.TrimRight(baseURL, "/") + "/api/v1/internal/tasks/sources/poll"
						if aud == "" {
							aud = strings.TrimRight(baseURL, "/")
						}
						if d, err := discovery.NewSourceCloudTasksDispatcher(ctx, projectID, location, queue, targetURL, saEmail, aud); err == nil {
							sourceTasksDispatcher = d
							logger.Printf("Cloud Tasks (Source) enabled: queue=%s location=%s target=%s", queue, location, targetURL)
						} else {
							logger.Printf("WARNING: Cloud Tasks (Source) disabled due to init error: %v", err)
						}
					} else {
						logger.Printf("WARNING: Cloud Tasks (Source) not configured (need CLOUD_TASKS_TARGET_BASE_URL and CLOUD_TASKS_OIDC_SERVICE_ACCOUNT)")
					}
				}
			}
		}
		{
			enabled := strings.TrimSpace(getEnv("ENABLE_CLOUD_TASKS_SCOPE_AGENT", "1"))
			if enabled == "1" || strings.EqualFold(enabled, "true") {
				location := strings.TrimSpace(getEnv("CLOUD_TASKS_LOCATION", "us-central1"))
				queue := strings.TrimSpace(getEnv("CLOUD_TASKS_SCOPE_AGENT_QUEUE", "nv-horizon-scope-agents"))
				baseURL := strings.TrimSpace(getEnv("CLOUD_TASKS_TARGET_BASE_URL", ""))
				saEmail := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
				aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
				if baseURL != "" && saEmail != "" {
					targetURL := strings.TrimRight(baseURL, "/") + "/api/v1/internal/tasks/scope-agents/run"
					if aud == "" {
						aud = strings.TrimRight(baseURL, "/")
					}
					if d, err := horizontasks.NewScopeAgentCloudTasksDispatcher(ctx, projectID, location, queue, targetURL, saEmail, aud); err == nil {
						scopeAgentTasksDispatcher = d
						logger.Printf("Cloud Tasks (ScopeAgent) enabled: queue=%s location=%s target=%s", queue, location, targetURL)
					} else {
						logger.Printf("WARNING: Cloud Tasks (ScopeAgent) disabled due to init error: %v", err)
					}
				} else {
					logger.Printf("WARNING: Cloud Tasks (ScopeAgent) not configured (need CLOUD_TASKS_TARGET_BASE_URL and CLOUD_TASKS_OIDC_SERVICE_ACCOUNT)")
				}
			}
		}
		if sourcePoller != nil && sourceTasksDispatcher != nil {
			sourcePoller.WithTaskDispatcher(sourceTasksDispatcher)
		}

		// Configure dedicated GCS storage and Vertex AI Search sync for Horizon Fragments
		// We use a separate bucket to keep Horizon fragment indexing isolated and controllable.
		horizonFragmentsBucket := getEnvWithAlt("NV_HORIZON_FRAGMENTS_BUCKET", "HORIZON_FRAGMENTS_BUCKET", "")
		if horizonFragmentsBucket == "" {
			horizonFragmentsBucket = docsBucket // fallback for legacy setups
		}
		var horizonVertexSyncBatcher *horizonrepo.VertexSyncBatcher
		if gcsClient != nil && horizonFragmentsBucket != "" {
			horizonFragmentsRepo.WithStorage(gcsClient, horizonFragmentsBucket)
			horizonEdgesRepo.WithStorage(gcsClient, horizonFragmentsBucket)
			if vertexSearchClient != nil {
				// Use batched sync to avoid quota exhaustion (quota is per-request, not per-document).
				// Defaults are intentionally conservative to avoid spamming ImportDocuments under high write rates.
				batchConfig := horizonrepo.DefaultVertexSyncBatcherConfig()
				// Optional tuning via env:
				// - VERTEX_SYNC_MAX_BATCH_SIZE (int)
				// - VERTEX_SYNC_MIN_BATCH_SIZE (int)
				// - VERTEX_SYNC_FLUSH_INTERVAL_SEC (int)
				if v := strings.TrimSpace(getEnv("VERTEX_SYNC_MAX_BATCH_SIZE", "")); v != "" {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						// Discovery Engine hard limit: max 100 URIs per ImportDocuments request (dataSchema=document)
						if n > 100 {
							n = 100
						}
						batchConfig.MaxBatchSize = n
					}
				}
				if v := strings.TrimSpace(getEnv("VERTEX_SYNC_MIN_BATCH_SIZE", "")); v != "" {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						batchConfig.MinBatchSize = n
					}
				}
				if v := strings.TrimSpace(getEnv("VERTEX_SYNC_FLUSH_INTERVAL_SEC", "")); v != "" {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						batchConfig.FlushInterval = time.Duration(n) * time.Second
					}
				}
				horizonVertexSyncBatcher = horizonrepo.NewVertexSyncBatcher(
					func(ctx context.Context, uris []string) (string, error) {
						// Prefer a dedicated datastore for horizon fragments (store isolation instead of filters).
						if ds := getEnvWithAlt("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID", "VERTEX_SEARCH_HORIZON_DATA_STORE_ID", ""); ds != "" {
							return vertexSearchClient.WithDataStore(ds).ImportDocumentsFromURIs(ctx, uris, "INCREMENTAL", "document")
						}
						// Fallback: legacy behavior (shared datastore)
						return vertexSearchClient.ImportDocumentsFromURIs(ctx, uris, "INCREMENTAL", "document")
					},
					batchConfig,
				)
				horizonFragmentsRepo.WithVertexSyncBatcher(horizonVertexSyncBatcher)
				horizonFragmentsRepo.WithVertexSearchDelete(func(ctx context.Context, fragmentID string) error {
					fragmentID = strings.TrimSpace(fragmentID)
					if fragmentID == "" {
						return nil
					}
					// Keep delete path aligned with import path: use dedicated Horizon datastore when configured.
					if ds := getEnvWithAlt("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID", "VERTEX_SEARCH_HORIZON_DATA_STORE_ID", ""); ds != "" {
						return vertexSearchClient.WithDataStore(ds).DeleteDocument(ctx, fragmentID)
					}
					return vertexSearchClient.DeleteDocument(ctx, fragmentID)
				})
				logger.Printf("Horizon Fragments: Vertex AI Search batched sync enabled (maxBatch=%d minBatch=%d interval=%v)",
					batchConfig.MaxBatchSize, batchConfig.MinBatchSize, batchConfig.FlushInterval)

				edgeDataStoreID := strings.TrimSpace(getEnvWithAlt("VERTEX_SEARCH_NV_HORIZON_EDGE_DATA_STORE_ID", "VERTEX_SEARCH_HORIZON_EDGES_DATA_STORE_ID", ""))
				if edgeDataStoreID == "" && parseEnvBool(getEnv("VERTEX_SEARCH_ENABLE_EDGE_DOCS_IN_HORIZON_STORE", "false")) {
					edgeDataStoreID = strings.TrimSpace(getEnvWithAlt("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID", "VERTEX_SEARCH_HORIZON_DATA_STORE_ID", ""))
				}
				if edgeDataStoreID != "" {
					edgeVertexSyncBatcher := horizonrepo.NewVertexSyncBatcher(
						func(ctx context.Context, uris []string) (string, error) {
							return vertexSearchClient.WithDataStore(edgeDataStoreID).ImportDocumentsFromURIs(ctx, uris, "INCREMENTAL", "document")
						},
						batchConfig,
					)
					horizonEdgesRepo.WithVertexSyncBatcher(edgeVertexSyncBatcher)
					horizonEdgesRepo.WithVertexSearchDelete(func(ctx context.Context, edgeID string) error {
						edgeID = strings.TrimSpace(edgeID)
						if edgeID == "" {
							return nil
						}
						return vertexSearchClient.WithDataStore(edgeDataStoreID).DeleteDocument(ctx, edgeID)
					})
					logger.Printf("Horizon Edges: Vertex AI Search batched sync enabled datastore=%s (maxBatch=%d minBatch=%d interval=%v)",
						edgeDataStoreID, batchConfig.MaxBatchSize, batchConfig.MinBatchSize, batchConfig.FlushInterval)
				} else {
					logger.Printf("Horizon Edges: Vertex AI Search sync disabled (set VERTEX_SEARCH_HORIZON_EDGES_DATA_STORE_ID or VERTEX_SEARCH_NV_HORIZON_EDGE_DATA_STORE_ID)")
				}
			}
		}

		// Configure GCS export + Vertex sync for ontology_facts (unified fact frames)
		if gcsClient != nil && docsBucket != "" {
			ontologyFactsRepo = ontologyFactsRepo.WithStorage(gcsClient, docsBucket)
			if vertexSearchClient != nil {
				ontologyFactsRepo = ontologyFactsRepo.WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
					if strings.TrimSpace(gcsURI) == "" {
						return "", nil
					}
					return vertexSearchClient.ImportDocumentsFromURIs(ctx, []string{gcsURI}, "INCREMENTAL", "document")
				})
				logger.Printf("Ontology facts: Vertex AI Search sync enabled")
			}
		}

		// Wire coordinate enrichment for ontology facts.
		// Spatial source of truth: Nominatim primary, Google Geocoding fallback, fragment lookup.
		{
			var gcForFacts ontology.GeocodingClientInterface
			{
				var googleFacts ontology.GeocodingClientInterface
				if gcClient := httphandlers.GetGoogleGeocodingClientFromEnv(); gcClient != nil {
					gcAdapter := geocoding.NewAdapter(gcClient)
					var gcBase ontology.GeocodingClientInterface = gcAdapter
					if horizonTelemetryRepo != nil {
						gcBase = horizontelemetry.NewGeocodingTelemetryWrapper(gcAdapter, horizonTelemetryRepo)
					}
					googleFacts = geocoding.NewCachedClientWithService(gcBase, externalCacheRepo, geocoding.CacheOptionsFromEnv(), ontology.GeocodingProviderGoogle)
				}
				nomRaw := geocoding.NominatimOptionsFromEnv()
				var nomBase ontology.GeocodingClientInterface = nomRaw
				if horizonTelemetryRepo != nil {
					nomBase = horizontelemetry.NewNominatimTelemetryWrapper(nomRaw, horizonTelemetryRepo)
				}
				nomCached := geocoding.NewCachedClientWithService(nomBase, externalCacheRepo, geocoding.CacheOptionsFromEnv(), ontology.GeocodingProviderNominatim)
				gcForFacts = geocoding.NewFallbackClient(googleFacts, nomCached, logger)
			}
			if gcForFacts != nil {
				enricher := horizonenrich.NewFactCoordinateEnricher(nil, gcForFacts).WithFragmentsRepo(horizonFragmentsRepo)
				ontologyFactsRepo = ontologyFactsRepo.WithCoordinateEnricher(enricher)
				ontologyExtractor = ontologyExtractor.WithFactsRepo(ontologyFactsRepo)
				logger.Printf("Ontology facts: coordinate enrichment enabled (Google Geocoding primary, Nominatim fallback, fragment lookup)")
			}
		}

		// Horizon service dependencies
		horizonDeps := horizon.HorizonServiceDependencies{
			AIExecutor:         exec,
			EmbeddingProvider:  emb,
			VertexSearchClient: vertexSearchClient,
			AgentRegistry:      agentRegistry,
			AgentJobsRepo:      agentJobsRepo,
			RealtimePublisher:  pub,
			EntitiesRepo:       entitiesRepo,
			DocumentsRepo:      docsRepo,
			DataSourcesRepo:    s.dsrcs,
			Crawler:            wc,
			Logger:             logger,
		}

		// Create HorizonService
		horizonService, err = horizon.NewHorizonService(
			horizonDeps,
			horizonFragmentsRepo,
			horizonFramesRepo,
			horizonLensesRepo,
			horizonEdgesRepo,
			horizonWatchdogRepo,
			horizonScoutingJobsRepo,
			horizonFragmentVersionsRepo,
		)
		if err != nil {
			logger.Printf("WARNING: Horizon service initialization failed: %v", err)
		} else {
			if artifactContextService != nil {
				artifactContextService = artifactContextService.WithGraphContextBuilder(
					services.NewArtifactGraphContextBuilder(
						horizonService.Lens(),
						horizonMaterializedArtifactsRepo,
						horizonFragmentsRepo,
						horizonEdgesRepo,
					),
				)
			}
			horizonService.SetScoutExecutionRuntime(scoutExecutionReady, scoutExecutionReason)
			logger.Printf("Horizon scout execution runtime: ready=%t reason=%s", scoutExecutionReady, scoutExecutionReason)
			horizonService.SetOntologyFactsRepository(ontologyFactsRepo)
			_ = horizonWatchPacksRepo

			scopeAgentService := horizonscopeagent.NewService(
				horizonScopeAgentConfigsRepo,
				horizonScopeAgentStatesRepo,
				horizonScopeAgentRunsRepo,
				horizonAgentProposalsRepo,
				horizonArbitrationDecisionsRepo,
				horizonWatchTargetsRepo,
				horizonWatchSubjectsRepo,
				horizonWatchTriggersRepo,
				horizonWatchBriefsRepo,
				horizonWatchFeedRepo,
				agentJobsRepo,
				horizonWatchdogRepo,
				horizonFragmentsRepo,
				horizonMaterializedArtifactsRepo,
				riskJobsRepo,
				ontologyFactsRepo,
				s.dsrcs,
				exec,
				nil,
				nil,
			)
			if scopeAgentTasksDispatcher != nil {
				scopeAgentService.WithBatchDispatcher(func(ctx context.Context, tenantID string) error {
					return scopeAgentTasksDispatcher.EnqueueTenantBatch(ctx, tenantID, 8)
				})
			}
			horizonService.SetScopeAgentService(scopeAgentService)

			watchRuntimeService := horizonwatchruntime.NewService(
				horizonWatchSubjectsRepo,
				horizonWatchTriggersRepo,
				horizonWatchTargetsRepo,
				horizonWatchdogRepo,
				horizonScopeAgentConfigsRepo,
				horizonLensesRepo,
				horizonLensArtifactsRepo,
				horizonFragmentsRepo,
				horizonEdgesRepo,
			).WithScopeAgentRunsRepository(horizonScopeAgentRunsRepo).
				WithMaterializedArtifactsRepository(horizonMaterializedArtifactsRepo).
				WithFactsRepository(ontologyFactsRepo).
				WithWatchPortfoliosRepository(horizonWatchPortfoliosRepo).
				WithWatchDigestsRepository(horizonWatchDigestsRepo).
				WithMacroAgentPoliciesRepository(horizonMacroAgentPoliciesRepo).
				WithAgentProposalsRepository(horizonAgentProposalsRepo).
				WithArbitrationDecisionsRepository(horizonArbitrationDecisionsRepo).
				WithWatchBriefsRepository(horizonWatchBriefsRepo).
				WithWatchFeedEntriesRepository(horizonWatchFeedRepo).
				WithRiskJobsRepository(riskJobsRepo).
				WithScoutingJobsRepository(horizonScoutingJobsRepo).
				WithWakeHook(func(ctx context.Context, tenantID, subjectID, triggerType, triggerReason string, trace map[string]any) ([]string, error) {
					runs, err := scopeAgentService.WakeBySubjectID(ctx, tenantID, subjectID, triggerType, triggerReason, trace)
					if err != nil {
						return nil, err
					}
					ids := make([]string, 0, len(runs))
					for _, run := range runs {
						if run != nil && strings.TrimSpace(run.ID) != "" {
							ids = append(ids, strings.TrimSpace(run.ID))
						}
					}
					return ids, nil
				})
			horizonService.SetWatchRuntimeService(watchRuntimeService)
			scopeAgentService.WithAcceptedProposalHook(func(ctx context.Context, proposal *horizonmodels.AgentProposal) ([]string, error) {
				if proposal == nil || strings.TrimSpace(proposal.ProposalType) != string(horizonmodels.AgentProposalTypeCoverageGap) {
					return nil, nil
				}
				subjectID := strings.TrimSpace(fmt.Sprint(proposal.Payload["watchSubjectId"]))
				if subjectID == "" && strings.TrimSpace(proposal.WatchTargetID) != "" {
					if target, err := horizonWatchTargetsRepo.Get(ctx, strings.TrimSpace(proposal.WatchTargetID)); err == nil && target != nil {
						subjectID = strings.TrimSpace(target.SubjectID)
					}
				}
				if subjectID == "" {
					return nil, nil
				}
				title := strings.TrimSpace(fmt.Sprint(proposal.Payload["title"]))
				summary := strings.TrimSpace(fmt.Sprint(proposal.Payload["summary"]))
				tenantID := strings.TrimSpace(proposal.TenantID)
				if tenantID == "" {
					tenantID = "global"
				}
				reason := strings.TrimSpace(proposal.Explanation)
				if reason == "" {
					reason = summary
				}
				if reason == "" {
					reason = title
				}
				if reason == "" {
					reason = "scope agent coverage gap"
				}
				confidence := proposal.Confidence
				if confidence < 0 {
					confidence = 0
				}
				if confidence > 1 {
					confidence = 1
				}
				trigger, _, err := watchRuntimeService.EmitAndProcessTriggerForSubject(
					ctx,
					tenantID,
					subjectID,
					horizonmodels.WatchTriggerPropagationGap,
					reason,
					confidence,
					confidence,
					map[string]any{
						"proposalId":      strings.TrimSpace(proposal.ID),
						"watchTargetId":   strings.TrimSpace(proposal.WatchTargetID),
						"headline":        strings.TrimSpace(title),
						"summary":         summary,
						"reason":          strings.TrimSpace(proposal.Explanation),
						"canonicalPaths":  append([]string(nil), proposal.TargetCanonicalPaths...),
						"macroAgentClass": fmt.Sprint(proposal.Payload["macroAgentClass"]),
					},
				)
				if err != nil || trigger == nil {
					return nil, err
				}
				return []string{"watch_trigger:" + strings.TrimSpace(trigger.ID)}, nil
			})

			watchService := horizonwatch.NewService(
				horizonWatchTargetsRepo,
				horizonWatchBriefsRepo,
				horizonWatchFeedRepo,
				horizonWatchPreferencesRepo,
				horizonFragmentsRepo,
				riskJobsRepo,
				horizonAgentProposalsRepo,
				horizonArbitrationDecisionsRepo,
				ontologyFactsRepo,
			).WithLensesRepository(horizonLensesRepo).
				WithPagePersistence(horizonWatchPageSnapshotsRepo, horizonWatchPageHistoryRepo).
				WithMaterializedArtifactsRepository(horizonMaterializedArtifactsRepo).
				WithEdgesRepository(horizonEdgesRepo).
				WithRuntimeProvisioner(func(ctx context.Context, target *horizonmodels.WatchTarget) error {
					if _, err := watchRuntimeService.EnsureSubjectForTarget(ctx, target); err != nil {
						return err
					}
					_, err := scopeAgentService.EnsureRuntimeForTarget(ctx, target)
					return err
				}).WithRedis(redisClient).
				WithLensAttentionProvider(func(ctx context.Context, tenantID, lensID string, limit int) (*horizonwatchruntime.LensAttentionView, error) {
					return watchRuntimeService.LensAttention(ctx, tenantID, lensID, limit)
				})
			if genaiHandler != nil {
				watchService = watchService.WithPageEnricher(func(ctx context.Context, input horizonwatch.PageEnrichmentInput) (*horizonwatch.PageEnrichmentOutput, error) {
					if strings.TrimSpace(input.HeroTitle) == "" && len(input.RiskItems) == 0 {
						return nil, nil
					}
					schema := map[string]any{
						"type": "object",
						"properties": map[string]any{
							"heroTitle":         map[string]any{"type": "string"},
							"heroSummary":       map[string]any{"type": "string"},
							"heroWhyNow":        map[string]any{"type": "string"},
							"scopeSummary":      map[string]any{"type": "string"},
							"productSummary":    map[string]any{"type": "string"},
							"supplyPathSummary": map[string]any{"type": "string"},
							"riskSummary":       map[string]any{"type": "string"},
							"affectedSummary":   map[string]any{"type": "string"},
							"directSummary":     map[string]any{"type": "string"},
							"indirectSummary":   map[string]any{"type": "string"},
							"signalSummary":     map[string]any{"type": "string"},
							"scenarioSummary":   map[string]any{"type": "string"},
							"dashboardCaptions": map[string]any{
								"type":                 "object",
								"additionalProperties": map[string]any{"type": "string"},
							},
							"mapCaptions": map[string]any{
								"type":                 "object",
								"additionalProperties": map[string]any{"type": "string"},
							},
						},
						"required": []string{"heroTitle", "heroSummary", "heroWhyNow", "scopeSummary", "productSummary", "supplyPathSummary", "riskSummary", "affectedSummary", "directSummary", "indirectSummary", "signalSummary", "scenarioSummary", "dashboardCaptions", "mapCaptions"},
					}
					payload, _ := json.Marshal(map[string]any{
						"lensId":            input.LensID,
						"lensName":          input.LensName,
						"timeHorizon":       input.TimeHorizon,
						"interestTitle":     input.InterestTitle,
						"interestQuery":     input.InterestQuery,
						"queryOverrides":    input.QueryOverrides,
						"heroTitle":         input.HeroTitle,
						"heroSummary":       input.HeroSummary,
						"anchorItems":       input.AnchorItems,
						"reachItems":        input.ReachItems,
						"productItems":      input.ProductItems,
						"supplyPathItems":   input.SupplyPathItems,
						"riskItems":         input.RiskItems,
						"affectedItems":     input.AffectedItems,
						"directItems":       input.DirectItems,
						"indirectItems":     input.IndirectItems,
						"signalItems":       input.SignalItems,
						"scenarioItems":     input.ScenarioItems,
						"anchorDetails":     input.AnchorDetails,
						"reachDetails":      input.ReachDetails,
						"productDetails":    input.ProductDetails,
						"supplyPathDetails": input.SupplyPathDetails,
						"riskDetails":       input.RiskDetails,
						"affectedDetails":   input.AffectedDetails,
						"directDetails":     input.DirectDetails,
						"indirectDetails":   input.IndirectDetails,
						"signalDetails":     input.SignalDetails,
						"scenarioDetails":   input.ScenarioDetails,
						"evidenceDetails":   input.EvidenceDetails,
						"dashboards":        input.Dashboards,
						"mapFragments":      input.MapFragments,
						"evidenceTitles":    input.EvidenceTitles,
					})
					req := gen_combined.GenAIRequest{
						Model:             "",
						SystemInstruction: "You are assembling a concise external teaser page for a geopolitical and supply-chain intelligence lens. Interpret only lens-specific aggregates derived from the lens scope, expanded graph reach, and user query. Keep the language clear, direct, non-technical, and evidence-aware. Do not invent facts beyond the provided data. Product summaries must explain affected products, their consuming entities, and transformed or downstream products. Direct summaries must explain who is hit first and why. Indirect summaries must explain second-order exposure. Signal summaries must aggregate events or structural signals and their origin. Scenario summaries must describe plausible operational paths forward implied by the current graph.",
						Prompt: fmt.Sprintf(
							"Return exactly one JSON object that enriches this persisted Horizon Watch page. Keep heroSummary and heroWhyNow short, dashboard captions to one sentence, and map captions to one sentence each. Use the detailed section objects, not just the item titles. Preserve the difference between direct and indirect effects, and treat global nodes as relevant only when they are reached from this lens.\n\nStructured input:\n%s",
							string(payload),
						),
						ProjectID:         strings.TrimSpace(input.ProjectID),
						Temperature:       0.2,
						MaxOutputTokens:   2200,
						StrictJSONEnabled: true,
						JSONSchema:        schema,
					}
					resp, err := genaiHandler.GenerateStructuredViaTaskExecutor(ctx, req, 5)
					if err != nil || resp == nil || strings.TrimSpace(resp.Content) == "" {
						return nil, err
					}
					var parsed struct {
						HeroTitle         string            `json:"heroTitle"`
						HeroSummary       string            `json:"heroSummary"`
						HeroWhyNow        string            `json:"heroWhyNow"`
						ScopeSummary      string            `json:"scopeSummary"`
						ProductSummary    string            `json:"productSummary"`
						SupplyPathSummary string            `json:"supplyPathSummary"`
						RiskSummary       string            `json:"riskSummary"`
						AffectedSummary   string            `json:"affectedSummary"`
						DirectSummary     string            `json:"directSummary"`
						IndirectSummary   string            `json:"indirectSummary"`
						SignalSummary     string            `json:"signalSummary"`
						ScenarioSummary   string            `json:"scenarioSummary"`
						DashboardCaptions map[string]string `json:"dashboardCaptions"`
						MapCaptions       map[string]string `json:"mapCaptions"`
					}
					if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
						return nil, err
					}
					return &horizonwatch.PageEnrichmentOutput{
						Model:             strings.TrimSpace(resp.Model),
						HeroTitle:         strings.TrimSpace(parsed.HeroTitle),
						HeroSummary:       strings.TrimSpace(parsed.HeroSummary),
						HeroWhyNow:        strings.TrimSpace(parsed.HeroWhyNow),
						ScopeSummary:      strings.TrimSpace(parsed.ScopeSummary),
						ProductSummary:    strings.TrimSpace(parsed.ProductSummary),
						SupplyPathSummary: strings.TrimSpace(parsed.SupplyPathSummary),
						RiskSummary:       strings.TrimSpace(parsed.RiskSummary),
						AffectedSummary:   strings.TrimSpace(parsed.AffectedSummary),
						DirectSummary:     strings.TrimSpace(parsed.DirectSummary),
						IndirectSummary:   strings.TrimSpace(parsed.IndirectSummary),
						SignalSummary:     strings.TrimSpace(parsed.SignalSummary),
						ScenarioSummary:   strings.TrimSpace(parsed.ScenarioSummary),
						DashboardCaptions: parsed.DashboardCaptions,
						MapCaptions:       parsed.MapCaptions,
						GeneratedAt:       time.Now().UTC(),
					}, nil
				})
			}
			horizonService.SetWatchService(watchService)
			strategicRecommendationsService := horizonrecommendations.NewService(
				horizonStrategicRecommendationsRepo,
				horizonFragmentsRepo,
				horizonEdgesRepo,
				horizonScoutingJobsRepo,
				exec,
				pub,
				logger,
			).WithWatchTriggerHook(func(ctx context.Context, tenantID, canonicalPath, reason string, metadata map[string]any) error {
				return watchRuntimeService.EmitRecommendationApprovedByCanonicalPath(ctx, tenantID, canonicalPath, reason, metadata)
			})
			watchRuntimeService.WithRecommendationActionHook(func(ctx context.Context, input horizonwatchruntime.RecommendationActionInput) error {
				switch input.TriggerType {
				case horizonmodels.WatchTriggerPropagationGap:
					_, _, err := strategicRecommendationsService.Detect(ctx, horizonrecommendations.DetectionInput{
						TenantID:             input.TenantID,
						ProjectID:            input.ProjectID,
						LensID:               input.LensID,
						EventTitle:           input.EventTitle,
						EventSummary:         input.EventSummary,
						SubjectCanonicalPath: input.SubjectCanonicalPath,
						SubjectType:          input.SubjectType,
						SignalAxis:           input.SignalAxis,
						BusinessEndpoints:    input.BusinessEndpoints,
						RemoteEndpoints:      input.RemoteEndpoints,
						PathFamilies:         input.PathFamilies,
						OccurredAt:           input.OccurredAt,
					})
					return err
				default:
					_, _, err := strategicRecommendationsService.ReconcileBySubjectCanonicalPath(ctx, input.TenantID, input.SubjectCanonicalPath, 20)
					return err
				}
			}).WithRecommendationExecutionHook(func(ctx context.Context, recommendationID, jobID, stage, summary string) error {
				_, err := strategicRecommendationsService.MarkExecuting(ctx, recommendationID, jobID, stage, summary)
				return err
			}).WithRecommendationCompletionHook(func(ctx context.Context, job *horizonmodels.ScoutingJob) error {
				return strategicRecommendationsService.HandleScoutingJobComplete(ctx, job)
			})
			horizonService.SetStrategicRecommendationsService(strategicRecommendationsService)
			horizonScoutingJobsRepo.WithOnComplete(func(ctx context.Context, job *horizonmodels.ScoutingJob) error {
				return watchRuntimeService.HandleScoutingJobComplete(ctx, job)
			})

			// Initialize Lens subsystem
			lensLifecycle := horizonlens.NewLifecycleManager(horizonFragmentsRepo)
			lensCalculator := horizonlens.NewMetricsCalculator(horizonFragmentsRepo)
			lensService := horizonlens.NewLensService(horizonLensesRepo, horizonFragmentsRepo, horizonScoutingJobsRepo, ontologyFactsRepo, lensLifecycle, lensCalculator, pub).
				WithLensArtifactsRepo(horizonLensArtifactsRepo)
			lensService.SetWatchSubjectSyncHook(func(ctx context.Context, lensID string) {
				_ = watchRuntimeService.SyncWatchSubjectsForLens(ctx, lensID)
			})

			// Wire cascade cleanup repos for lens deletion (best-effort orphan cleanup)
			horizonLensItemsRepo := horizonrepo.NewLensItemsRepository(fsClient.Client())
			horizonLensItemsHandler := horizonhttp.NewLensItemsHandler(horizonLensItemsRepo, horizonLensesRepo, s.projs)
			horizonExposurePathsRepo := horizonrepo.NewExposurePathsRepository(fsClient.Client())
			if gcsClient != nil && horizonFragmentsBucket != "" {
				horizonMaterializedArtifactsRepo.WithStorage(gcsClient, horizonFragmentsBucket)
				if horizonVertexSyncBatcher != nil {
					horizonMaterializedArtifactsRepo.WithVertexSyncBatcher(horizonVertexSyncBatcher)
				}
			}
			horizonFragmentsMergeRepo = horizonFragmentsMergeRepo.
				WithMaterializedRepo(horizonMaterializedArtifactsRepo).
				WithWatchSubjectsRepo(horizonWatchSubjectsRepo).
				WithOntologyFactsRepo(ontologyFactsRepo).
				WithEntitiesRepo(entitiesRepo)
			lensService.WithCleanupRepos(
				horizonLensItemsRepo,
				horizonExposurePathsRepo,
				horizonMaterializedArtifactsRepo,
				horizonSpendLedger,
				agentJobsRepo, // implements AgentJobsDeleter interface
			)

			horizonService.SetLensService(lensService)
			horizonService.SetLensArtifactsRepo(horizonLensArtifactsRepo)
			horizonService.SetDomainAnswerSnapshotsRepo(horizonDomainAnswerSnapshotsRepo)
			// Initialize Propagation subsystem
			propagationEngine := horizonpropagation.NewPropagationEngine(
				horizonFragmentsRepo,
				horizonFragmentVersionsRepo,
				horizonEdgesRepo,
				horizonLensesRepo,
				pub,
			)
			adapterRegistry := horizonpropagation.NewAdapterRegistry()
			if err := adapterRegistry.Register(horizonpropagation.NewPolicyImpactAdapter()); err != nil {
				logger.Printf("WARNING: policy-impact adapter registration failed: %v", err)
			}
			if err := adapterRegistry.RegisterModeFallback(taxonomy.DomainModePolicyImpact, horizonpropagation.PolicyImpactContractTypeFamily); err != nil {
				logger.Printf("WARNING: policy-impact adapter mode fallback registration failed: %v", err)
			}
			propagationEngine.WithDomainAdapterRegistry(adapterRegistry)
			propagationProfilesRepo := horizonrepo.NewPropagationProfilesRepository(fsClient.Client())
			propagationProfileLookup := horizonpropagation.NewProfileLookup(propagationProfilesRepo)
			if err := propagationProfileLookup.Initialize(context.Background()); err != nil {
				logger.Printf("WARNING: propagation profile lookup initialization fallback: %v", err)
			}
			propagationEngine.WithProfileLookup(propagationProfileLookup)
			horizonService.SetWatchService(watchService)
			wmVertexSearch := vertexSearchClient
			if wmVertexSearch != nil {
				if ds := strings.TrimSpace(getEnvWithAlt("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID", "VERTEX_SEARCH_HORIZON_DATA_STORE_ID", "")); ds != "" {
					wmVertexSearch = wmVertexSearch.WithDataStore(ds)
				}
			}
			orchestrator = orchestrator.WithHorizonMonitorPipeline(
				discovery.NewHorizonMonitorPipeline(
					ontologyFactsRepo,
					exec,
					horizonFragmentsRepo,
					horizonEdgesRepo,
					propagationEngine,
					propagationProfileLookup,
					logger,
				).WithTelemetryRepo(horizonTelemetryRepo).
					WithRealtimePublisher(pub).
					WithStrategicRecommendations(strategicRecommendationsService).
					WithWatchTriggerHook(func(ctx context.Context, tenantID, canonicalPath string, triggerType horizonmodels.WatchTriggerType, reason string, signalStrength, confidence float64, metadata map[string]any) error {
						_, err := watchRuntimeService.EmitTriggerByAnchor(ctx, tenantID, canonicalPath, triggerType, reason, signalStrength, confidence, metadata)
						return err
					}).
					WithVertexSearch(wmVertexSearch),
			)
			interpretationQueue := horizoninterp.NewInterpretationQueue(fsClient.Client())
			interpretationCache := horizoninterp.NewInterpretationCache(fsClient.Client())
			scoreComputer := horizoninterp.NewScoreComputer()
			triggerEvaluator := horizoninterp.NewTriggerEvaluator(scoreComputer, horizonFragmentsRepo, horizonFragmentVersionsRepo)
			interpDispatcher := horizoninterp.NewDispatcher(
				horizonFragmentsRepo,
				triggerEvaluator,
				interpretationQueue,
				horizoninterp.DefaultTriggerConfig(),
			)
			var feedbackMu sync.Mutex
			feedbackThrottleWindow := 2 * time.Minute
			feedbackLastSeen := make(map[string]time.Time)
			isFeedbackThrottled := func(key string) bool {
				key = strings.TrimSpace(key)
				if key == "" {
					return false
				}
				now := time.Now().UTC()
				feedbackMu.Lock()
				defer feedbackMu.Unlock()
				for k, ts := range feedbackLastSeen {
					if now.Sub(ts) > feedbackThrottleWindow {
						delete(feedbackLastSeen, k)
					}
				}
				if ts, ok := feedbackLastSeen[key]; ok && now.Sub(ts) <= feedbackThrottleWindow {
					return true
				}
				feedbackLastSeen[key] = now
				return false
			}
			resolveFeedbackFragment := func(ctx context.Context, event horizonfeedback.Event) *horizonmodels.HorizonFragment {
				if ctx == nil {
					ctx = context.Background()
				}
				if fid := strings.TrimSpace(event.FragmentID); fid != "" {
					if f, err := horizonFragmentsRepo.Get(ctx, fid); err == nil && f != nil {
						return f
					}
				}
				candidates := make([]string, 0, 3)
				if cp := strings.TrimSpace(event.CanonicalPath); cp != "" {
					candidates = append(candidates, cp)
				}
				if raw, ok := event.Payload["representative"]; ok {
					if cp, ok := raw.(string); ok && strings.TrimSpace(cp) != "" {
						candidates = append(candidates, strings.TrimSpace(cp))
					}
				}
				for _, cp := range candidates {
					if f, err := horizonFragmentsRepo.GetByCanonicalPath(ctx, cp); err == nil && f != nil {
						return f
					}
				}
				return nil
			}
			normalizeFeedbackPolarity := func(p string) string {
				p = strings.ToLower(strings.TrimSpace(p))
				if p != "risk" && p != "opportunity" {
					return "risk"
				}
				return p
			}
			enqueueForcedInterpretation := func(ctx context.Context, frag *horizonmodels.HorizonFragment, polarity string, priority horizoninterp.TriggerPriority, triggerType horizoninterp.TriggerType, triggerValue float64) {
				if frag == nil || strings.TrimSpace(frag.ID) == "" {
					return
				}
				polarity = normalizeFeedbackPolarity(polarity)
				hasPending, err := interpretationQueue.HasPending(ctx, frag.ID, polarity)
				if err == nil && hasPending {
					return
				}
				_, err = interpretationQueue.Enqueue(ctx, &horizoninterp.InterpretationRequest{
					FragmentID:    strings.TrimSpace(frag.ID),
					CanonicalPath: strings.TrimSpace(frag.CanonicalPath),
					NodeLabel:     strings.TrimSpace(frag.Labels["en"]),
					Polarity:      polarity,
					Priority:      priority,
					TriggerType:   triggerType,
					TriggerValue:  triggerValue,
					CreatedAt:     time.Now().UTC(),
					Status:        "pending",
				})
				if err != nil {
					logger.Printf("[horizon.feedback] enqueue_failed fragmentId=%s polarity=%s err=%v", strings.TrimSpace(frag.ID), polarity, err)
				}
			}
			triggerPropagationRefresh := func(fragmentID string) {
				fragmentID = strings.TrimSpace(fragmentID)
				if fragmentID == "" || propagationEngine == nil {
					return
				}
				go func(fid string) {
					runCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					if _, err := propagationEngine.TriggerPropagation(runCtx, fid); err != nil {
						logger.Printf("[horizon.feedback] propagation_refresh_failed fragmentId=%s err=%v", fid, err)
					}
				}(fragmentID)
			}
			emitFeedbackWatchTrigger := func(ctx context.Context, frag *horizonmodels.HorizonFragment, eventType, polarity, reason string) {
				if frag == nil {
					return
				}
				_ = watchRuntimeService.SyncSubjectsForFragment(ctx, frag.ID)
				if _, err := watchRuntimeService.EmitTriggerByAnchor(ctx, frag.TenantID, frag.CanonicalPath, horizonmodels.WatchTriggerManual, "feedback:"+strings.TrimSpace(eventType), 0.95, 0.95, map[string]any{
					"feedbackType":   strings.TrimSpace(eventType),
					"feedbackReason": strings.TrimSpace(reason),
					"polarity":       normalizeFeedbackPolarity(polarity),
					"semanticWatermark": strings.Join([]string{
						"feedback",
						strings.TrimSpace(eventType),
						strings.TrimSpace(reason),
						strings.TrimSpace(frag.ID),
					}, ":"),
				}); err != nil {
					logger.Printf("[horizon.feedback] watch_trigger_emit_failed fragmentId=%s eventType=%s err=%v", strings.TrimSpace(frag.ID), strings.TrimSpace(eventType), err)
				}
			}
			feedbackEmitter := horizonfeedback.FuncEmitter(func(ctx context.Context, event horizonfeedback.Event) error {
				eventType := strings.TrimSpace(event.Type)
				if eventType == "" {
					return nil
				}
				frag := resolveFeedbackFragment(ctx, event)
				fragKey := strings.TrimSpace(event.FragmentID)
				if frag != nil && strings.TrimSpace(frag.ID) != "" {
					fragKey = strings.TrimSpace(frag.ID)
				}
				throttleKey := strings.Join([]string{
					eventType,
					fragKey,
					normalizeFeedbackPolarity(event.Polarity),
					strings.TrimSpace(event.Reason),
				}, "|")
				if isFeedbackThrottled(throttleKey) {
					return nil
				}

				logger.Printf("[horizon.feedback] type=%s fragmentId=%s polarity=%s reason=%s",
					eventType,
					fragKey,
					normalizeFeedbackPolarity(event.Polarity),
					strings.TrimSpace(event.Reason),
				)

				switch eventType {
				case "re_evaluate":
					if frag != nil {
						enqueueForcedInterpretation(ctx, frag, event.Polarity, horizoninterp.TriggerPriorityHigh, horizoninterp.TriggerTypeRapidChange, 1.0)
						triggerPropagationRefresh(frag.ID)
						emitFeedbackWatchTrigger(ctx, frag, eventType, event.Polarity, event.Reason)
					}
				case "attention_escalation":
					if frag != nil {
						enqueueForcedInterpretation(ctx, frag, event.Polarity, horizoninterp.TriggerPriorityAlways, horizoninterp.TriggerTypeVIP, 1.0)
						triggerPropagationRefresh(frag.ID)
						emitFeedbackWatchTrigger(ctx, frag, eventType, event.Polarity, event.Reason)
					}
				}
				return nil
			})
			interpretSignalsRepo := horizoninterp.NewOntologySignalFactsRepo(ontologyFactsRepo)
			interpreter := horizoninterp.NewInterpreter(exec, horizonFragmentsRepo, interpretSignalsRepo).
				WithEventFactsResolver(horizoninterp.NewOntologyEventFactsResolver(ontologyFactsRepo)).
				WithFeedbackEmitter(feedbackEmitter)
			interpProcessor := horizoninterp.NewProcessor(
				interpretationQueue,
				interpretationCache,
				interpreter,
			)
			interpProcessor.Start(context.Background(), 30*time.Second, 20)
			propagationEngine.WithFragmentChangedHook(func(ctx context.Context, fragmentID string) {
				_ = interpDispatcher.EvaluateAndEnqueue(ctx, fragmentID)
			})

			// Wire Policy Architecture for semantic propagation
			propagationPoliciesRepo := horizonrepo.NewPropagationPoliciesRepository(fsClient.Client())
			propagationPolicy := horizonpolicy.NewPropagationPolicy(propagationPoliciesRepo)
			policyTelemetry := horizonpolicy.NewFirestoreTelemetryWriter(fsClient.Client())
			propagationPolicyExecutor := horizonpolicy.NewPolicyExecutor(propagationPolicy, policyTelemetry, nil) // LLM executor nil for now
			propagationEngine.WithPolicyExecutor(propagationPolicyExecutor)

			// Wire ValidationPolicy for edge and fragment validation
			validationRulesRepo := horizonrepo.NewValidationRulesRepository(fsClient.Client())
			validationPolicy := horizonpolicy.NewValidationPolicy(validationRulesRepo)
			validationPolicyExecutor := horizonpolicy.NewPolicyExecutor(validationPolicy, policyTelemetry, nil)
			horizonEdgesRepo.WithValidationPolicy(validationPolicyExecutor)
			horizonEdgesRepo.WithFragmentsRepo(horizonFragmentsRepo)

			// Wire CanonizationPolicy executor (policy created earlier, shared with OntologyExtractor)
			canonizationPolicyExecutor := horizonpolicy.NewPolicyExecutor(canonizationPolicy, policyTelemetry, nil)
			policyRegistry := horizonpolicy.NewPolicyRegistry(
				propagationPolicy,
				validationPolicy,
				canonizationPolicy,
			)
			logger.Printf(
				"Policy registry initialized: policies=%v unified_contract=%v shadow_mode=%v",
				policyRegistry.List(),
				horizonpolicy.UnifiedPolicyContractEnabled(),
				horizonpolicy.UnifiedPolicyShadowModeEnabled(),
			)

			// Wire Critical Path Detector
			criticalPathDetector := horizoncriticalpath.NewDetector(horizonFragmentsRepo, horizonEdgesRepo)
			propagationEngine.WithCriticalPathDetector(criticalPathDetector)

			horizonService.SetPropagationEngine(propagationEngine)
			operationalMetricHydrator := horizonmetriccore.NewOperationalHydrator(horizonFragmentsRepo, horizonMaterializedArtifactsRepo, genaiClient, logger)
			simulationService, err := horizonsimulation.NewService(horizonsimulation.Dependencies{
				RunsRepo:                 horizonSimulationRunsRepo,
				WorldsRepo:               horizonSimulationWorldsRepo,
				AgentsRepo:               horizonSimulationAgentsRepo,
				EventsRepo:               horizonSimulationEventsRepo,
				ArtifactsRepo:            horizonSimulationArtifactsRepo,
				FragmentsRepo:            horizonFragmentsRepo,
				EdgesRepo:                horizonEdgesRepo,
				LensesRepo:               horizonLensesRepo,
				MaterializedRepo:         horizonMaterializedArtifactsRepo,
				OntologyFactsRepo:        ontologyFactsRepo,
				PropagationEngine:        propagationEngine,
				PropagationProfileLookup: propagationProfileLookup,
				Submitter:                horizonSimulationSubmitter,
				OperationalHydrator:      operationalMetricHydrator,
				Logger:                   logger,
				Publisher: func(ctx context.Context, eventType string, tenantID string, payload any) error {
					if pub == nil {
						return nil
					}
					return pub.PublishJSON(ctx, realtime.EventType(eventType), tenantID, payload)
				},
			})
			if err != nil {
				logger.Printf("WARNING: simulation service initialization failed: %v", err)
			} else {
				horizonService.SetSimulationService(simulationService)
			}
			scopeAgentService.SetScenarioRuntime(propagationEngine, propagationProfileLookup)
			riskAppetiteRepo := horizonrepo.NewRiskAppetiteProfilesRepository(fsClient.Client())
			appetiteResolver := horizonrisk.NewAppetiteResolver(riskAppetiteRepo, horizonFragmentsRepo, horizonEdgesRepo)
			concentrationAnalyzer := horizonrisk.NewConcentrationAnalyzer(horizonFragmentsRepo).
				WithFeedbackEmitter(feedbackEmitter)
			horizonService.SetRiskServices(appetiteResolver, concentrationAnalyzer)
			if riskJobsRepo != nil {
				riskCfg := horizonrisk.DefaultPipelineConfig()
				enabledRiskPipeline := strings.TrimSpace(getEnv("ENABLE_RISK_PIPELINE", "1"))
				riskCfg.EnablePipeline = enabledRiskPipeline == "1" || strings.EqualFold(enabledRiskPipeline, "true")
				if v := strings.TrimSpace(getEnv("RISK_PIPELINE_MAX_STAGE_RETRIES", "")); v != "" {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						riskCfg.MaxAttempts = n
					}
				}
				if v := strings.TrimSpace(getEnv("RISK_PIPELINE_STAGE_TIMEOUT_SEC", "")); v != "" {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						riskCfg.StageTimeout = time.Duration(n) * time.Second
					}
				}
				stageFlag := func(env string, def bool) bool {
					raw := strings.TrimSpace(getEnv(env, ""))
					if raw == "" {
						return def
					}
					return raw == "1" || strings.EqualFold(raw, "true")
				}
				riskCfg.EnableStage[horizonmodels.RiskStageIdentify] = stageFlag("ENABLE_RISK_STAGE_IDENTIFY", riskCfg.EnableStage[horizonmodels.RiskStageIdentify])
				riskCfg.EnableStage[horizonmodels.RiskStageEscalate] = stageFlag("ENABLE_RISK_STAGE_ESCALATE", riskCfg.EnableStage[horizonmodels.RiskStageEscalate])
				riskCfg.EnableStage[horizonmodels.RiskStageApply] = stageFlag("ENABLE_RISK_STAGE_APPLICABILITY", riskCfg.EnableStage[horizonmodels.RiskStageApply])
				riskCfg.EnableStage[horizonmodels.RiskStageSeed] = stageFlag("ENABLE_RISK_STAGE_SEED", riskCfg.EnableStage[horizonmodels.RiskStageSeed])
				riskCfg.EnableStage[horizonmodels.RiskStagePropagate] = stageFlag("ENABLE_RISK_STAGE_PROPAGATE", riskCfg.EnableStage[horizonmodels.RiskStagePropagate])
				riskCfg.EnableStage[horizonmodels.RiskStageRollup] = stageFlag("ENABLE_RISK_STAGE_ROLLUP", riskCfg.EnableStage[horizonmodels.RiskStageRollup])
				riskCfg.EnableStage[horizonmodels.RiskStageInterpret] = stageFlag("ENABLE_RISK_STAGE_INTERPRET", riskCfg.EnableStage[horizonmodels.RiskStageInterpret])
				riskCfg.EnableStage[horizonmodels.RiskStageAlert] = stageFlag("ENABLE_RISK_STAGE_ALERT", riskCfg.EnableStage[horizonmodels.RiskStageAlert])

				riskPipeline = horizonrisk.NewPipeline(
					riskJobsRepo,
					ontologyFactsRepo,
					horizonFragmentsRepo,
					horizonEdgesRepo,
					horizonExposurePathsRepo,
					propagationEngine,
					propagationProfileLookup,
					appetiteResolver,
					interpreter,
					pub,
					riskCfg,
				).WithFeedbackEmitter(feedbackEmitter).
					WithScoutingJobsRepo(horizonScoutingJobsRepo).
					WithTelemetryRepo(horizonTelemetryRepo)
			}
			horizonFragmentsRepo.WithOnUpsert(func(ctx context.Context, fragment *horizonmodels.HorizonFragment) {
				// Auto-seed appetite profiles when external scopes emerge (market/geo/org with segments).
				appetiteResolver.EnsureForFragment(ctx, fragment)
			})

			// Запускаем фоновый процессор Cold Path задач (каждые 30 секунд)
			ctxBackground := context.Background()
			propagationEngine.StartColdPathProcessor(ctxBackground, 30*time.Second)

			// Initialize Scout subsystem
			scoutPlanner := horizonscout.NewScoutPlanner(s.dsrcs, ontologyFactsRepo, vertexSearchClient)
			if horizonMaterializedArtifactsRepo != nil {
				scoutPlanner.WithMaterializedRepo(horizonMaterializedArtifactsRepo)
			}

			// Source Executor (Telegram client skipped for now)
			rssProvider := prov.NewRSSProvider()
			sourceExecutor := horizonscout.NewSourceExecutor(wc, rssProvider, nil, docsRepo)

			// Scout Agents
			scoutAgent := horizonagents.NewScoutAgent(exec)
			criticAgent := horizonagents.NewCriticAgent(exec)
			resolverAgent := horizonagents.NewResolverAgent(exec)
			researchAgent := horizonagents.NewResearchAgent(exec)

			frameBuilder := horizonscout.NewFrameBuilder()
			scoutWorkflow := horizonscout.NewScoutWorkflow(agentJobsRepo)

			scoutEngine = horizonscout.NewScoutEngine(
				scoutPlanner,
				sourceExecutor,
				scoutAgent,
				criticAgent,
				resolverAgent,
				researchAgent,
				frameBuilder,
				scoutWorkflow,
				horizonFragmentsRepo,
				horizonFramesRepo,
				horizonEdgesRepo,
				horizonScoutingJobsRepo,
				pub,
				exec,
			)
			scoutEngine.
				WithWorkingProductScopesRepository(horizonWorkingProductScopesRepo).
				WithCapabilityProfilesRepository(horizonCapabilityProfilesRepo).
				WithCapabilityObservationsRepository(horizonCapabilityObsRepo).
				WithSupplyLinkObservationsRepository(horizonSupplyLinkObsRepo).
				WithRouteObservationsRepository(horizonRouteObsRepo).
				WithDerivedRiskProfilesRepository(horizonDerivedRiskProfilesRepo)
			// Build GeoNames client with telemetry (adapter -> telemetry -> cache). Only when horizon block runs.
			if geonamesClient != nil && horizonTelemetryRepo != nil {
				gnBase := geonames.NewAdapter(geonamesClient)
				gnWithTelemetry := horizontelemetry.NewGeoNamesTelemetryWrapper(gnBase, horizonTelemetryRepo)
				geonamesClientForHorizon = geonames.NewCachedClient(gnWithTelemetry, externalCacheRepo, geonames.CacheOptionsFromEnv())
			} else if geonamesClient != nil {
				geonamesClientForHorizon = geonames.NewCachedClient(geonames.NewAdapter(geonamesClient), externalCacheRepo, geonames.CacheOptionsFromEnv())
			}
			// Wire Wikidata into Scout (dedup + conservative enrichment-by-QID inside PreCreateValidator).
			// IMPORTANT: We do NOT enable fuzzy Wikidata search here; PreCreateValidator only uses GetEntityData when QID is already present.
			if wikidataClient != nil {
				wdAdapter := wikidata.NewAdapter(wikidataClient)
				var wdForScout ontology.WikidataClientInterface = wdAdapter
				if horizonTelemetryRepo != nil {
					wdForScout = horizontelemetry.NewWikidataTelemetryWrapper(wdAdapter, horizonTelemetryRepo)
				}
				wdForScout = wikidata.NewCachedClient(wdForScout, externalCacheRepo, wikidata.CacheOptionsFromEnv())
				scoutEngine.WithWikidataClient(wdForScout)
			}
			// Wire shared Redis client for cross-job caching (skeleton context, edge dedup).
			if redisClient != nil {
				scoutEngine.WithRedisClient(redisClient)
			}
			// Wire GeoNames into Scout for deterministic /loc normalization (country/admin1/city).
			if geonamesClientForHorizon != nil {
				scoutEngine.WithGeoNamesClient(geonamesClientForHorizon)
			}
			// Wire OpenCorporates into Scout for organization enrichment (registry IDs).
			// Requires OPEN_CORPORATES_API_TOKEN (API returns 401 without it). Set HORIZON_ENABLE_OPEN_CORPORATES=false to disable.
			ocClient := httphandlers.GetOpenCorporatesClientFromEnv()
			if ocClient != nil && ocClient.IsConfigured() {
				var ocForScout horizonenrich.OpenCorporatesClientInterface = ocClient
				if horizonTelemetryRepo != nil {
					ocForScout = horizontelemetry.NewOpenCorporatesTelemetryWrapper(ocClient, horizonTelemetryRepo)
				}
				scoutEngine.WithOpenCorporatesClient(ocForScout)
				logger.Printf("OpenCorporates client wired (org enrichment; enable with HORIZON_ENABLE_OPEN_CORPORATES=true)")
			} else {
				logger.Printf("OpenCorporates skipped: OPEN_CORPORATES_API_TOKEN not set (API requires token; add open_corporates_api_token secret)")
			}
			// Wire geocoding into Scout for address-to-coordinates (org/facility/gov).
			// Primary: Nominatim. Fallback: Google Geocoding.
			// Both wrapped with telemetry and cached via external_data_cache.
			{
				var googleClient ontology.GeocodingClientInterface
				if gcRaw := httphandlers.GetGoogleGeocodingClientFromEnv(); gcRaw != nil {
					gcAdapter := geocoding.NewAdapter(gcRaw)
					var gcBase ontology.GeocodingClientInterface = gcAdapter
					if horizonTelemetryRepo != nil {
						gcBase = horizontelemetry.NewGeocodingTelemetryWrapper(gcAdapter, horizonTelemetryRepo)
					}
					googleClient = geocoding.NewCachedClientWithService(gcBase, externalCacheRepo, geocoding.CacheOptionsFromEnv(), ontology.GeocodingProviderGoogle)
					logger.Printf("Google Geocoding client wired (primary)")
				}

				nomRaw := geocoding.NominatimOptionsFromEnv()
				var nomBase ontology.GeocodingClientInterface = nomRaw
				if horizonTelemetryRepo != nil {
					nomBase = horizontelemetry.NewNominatimTelemetryWrapper(nomRaw, horizonTelemetryRepo)
				}
				nominatimClient := geocoding.NewCachedClientWithService(nomBase, externalCacheRepo, geocoding.CacheOptionsFromEnv(), ontology.GeocodingProviderNominatim)
				logger.Printf("Nominatim geocoding client wired (fallback)")

				combined := geocoding.NewFallbackClient(googleClient, nominatimClient, logger)
				if combined != nil {
					scoutEngine.WithGeocodingClient(combined)
				}
			}
			// Unified ontology facts emitter: allows Scout to persist minimal OntologyFacts even when sources pipeline is paused.
			scoutEngine.WithFactEmitter(ontology.NewFactEmitter(ontologyFactsRepo, pub))
			// Budget enforcement wiring.
			scoutEngine.WithSpendLedger(horizonSpendLedger)
			// Cascade-level visited set (guardrail against cycles/avalanche fanout).
			scoutEngine.WithCascadeVisitedRepo(horizonCascadeVisitedRepo)
			// Cascade-level entity identity memory (dedup across child jobs even with path variants).
			scoutEngine.WithCascadeEntityRegistry(horizonCascadeEntityRegistryRepo)

			// Wire CanonizationPolicy and ValidationPolicy for normalization and validation
			scoutEngine.WithCanonizationPolicy(canonizationPolicyExecutor)
			scoutEngine.WithValidationPolicy(validationPolicyExecutor)
			scoutEngine.WithTelemetryRepo(horizonTelemetryRepo)
			if horizonJobTelemetryRepo != nil {
				scoutEngine.WithJobTelemetryRepo(horizonJobTelemetryRepo)
			}

			// Create and wire CanonicalPathResolver for early canonicalization and deduplication
			// This ensures all paths are canonicalized and deduplicated before processing
			// Reuse the same OrgDeduplicator instance as ScoutEngine to keep dedup logic unified.
			orgDeduplicator := scoutEngine.OrgDeduplicator()
			canonicalPathResolver := horizonscout.NewCanonicalPathResolver(
				horizonFragmentsRepo,
				canonizationPolicyExecutor,
				orgDeduplicator,
			)
			if horizonCanonicalPathRedirectsRepo != nil {
				canonicalPathResolver.WithCanonicalPathRedirectsRepo(horizonCanonicalPathRedirectsRepo)
			}
			if horizonTelemetryRepo != nil {
				canonicalPathResolver.WithTelemetryRepo(horizonTelemetryRepo)
			}
			canonicalPathResolver.WithProductDictionary(productDictionary).
				WithMechanismDictionary(mechanismDictionary).
				WithGovDictionary(govDictionary)
				// Supply chain path materialization: parsed chains → MaterializedArtifact
			if horizonMaterializedArtifactsRepo != nil {
				supplyChainPathBuilder := horizonpathbuilder.NewSupplyChainPathBuilder(
					horizonFragmentsRepo,
					horizonEdgesRepo,
					horizonMaterializedArtifactsRepo,
				)
				scoutEngine.WithSupplyChainPathBuilder(supplyChainPathBuilder)
				scoutEngine.WithServiceChainPathBuilder(
					horizonpathbuilder.NewSupplyChainPathBuilder(
						horizonFragmentsRepo,
						horizonEdgesRepo,
						horizonMaterializedArtifactsRepo,
					).WithArtifactKind(horizonmodels.MaterializedKindServiceChainPath),
				)
				domainArtifactBuilder := horizonpathbuilder.NewDomainArtifactBuilder(
					horizonFragmentsRepo,
					horizonEdgesRepo,
					horizonMaterializedArtifactsRepo,
				).WithProductDictionary(productDictionary)
				scoutEngine.WithDomainArtifactBuilder(domainArtifactBuilder)
			}
			if geonamesClientForHorizon != nil {
				canonicalPathResolver.WithGeoNamesClient(geonamesClientForHorizon)
			}
			scoutEngine.WithCanonicalPathResolver(canonicalPathResolver)
			// Wire LensesRepository for updating lens ViewConfig on assembly completion
			scoutEngine.WithLensesRepository(horizonLensesRepo)
			// Wire ExposurePathsRepository for strict external applicability gating.
			scoutEngine.WithExposurePathsRepository(horizonExposurePathsRepo)
			// Wire LensArtifactsRepository for Phase 2 Strategic Overlay
			scoutEngine.WithLensArtifactsRepo(horizonLensArtifactsRepo)
			// Wire DomainAnswerSnapshotsRepository for DomainContractEngine publish phase.
			scoutEngine.WithDomainAnswerSnapshotsRepo(horizonDomainAnswerSnapshotsRepo)
			internalKnowledgeStoreID := strings.TrimSpace(getEnv("VERTEX_SEARCH_INTERNAL_KNOWLEDGE_STORE_ID", getEnv("VERTEX_SEARCH_DATA_STORE_ID", "")))
			internalKnowledgeBucket := strings.TrimSpace(getEnv("RAG_DOCUMENTS_BUCKET", ""))
			if gcsClient != nil && vertexSearchClient != nil && internalKnowledgeBucket != "" && internalKnowledgeStoreID != "" {
				internalKnowledgeBuilder := horizoninternalknowledge.NewBuilder(
					horizonFragmentsRepo,
					horizonMaterializedArtifactsRepo,
					horizonLensArtifactsRepo,
				)
				internalKnowledgeWarmup := horizoninternalknowledge.NewWarmupService(
					horizoninternalknowledge.WarmupConfig{
						ProjectID:     projectID,
						AnswerBucket:  internalKnowledgeBucket,
						AnswerStoreID: internalKnowledgeStoreID,
					},
					horizonLensesRepo,
					internalKnowledgeBuilder,
					gcsClient,
					vertexSearchClient,
					horizonFragmentsRepo.Firestore(),
				)
				scoutEngine.WithInternalKnowledgeWarmup(internalKnowledgeWarmup)
				lensService.WithInternalKnowledgeWarmup(internalKnowledgeWarmup)
				logger.Printf("Internal knowledge warmup enabled: bucket=%s store=%s", internalKnowledgeBucket, internalKnowledgeStoreID)
			} else {
				logger.Printf("Internal knowledge warmup disabled: bucket=%q store=%q gcs=%t vertex=%t",
					internalKnowledgeBucket,
					internalKnowledgeStoreID,
					gcsClient != nil,
					vertexSearchClient != nil,
				)
			}

			// Подключаем триггер распространения изменений
			scoutEngine.WithPropagationTrigger(func(ctx context.Context, fragmentID string) error {
				_, err := propagationEngine.TriggerPropagation(ctx, fragmentID)
				return err
			})
			// Wire shared axis-based signal propagation components (single source of truth).
			scoutEngine.WithSignalPropagation(propagationEngine, propagationProfileLookup)

			horizonService.SetScoutEngine(scoutEngine)

			// Initialize and wire PolicyChecker for taxonomy/structure validation
			policyChecker := horizonpolicy.NewPolicyChecker(
				horizonFragmentsRepo,
				horizonEdgesRepo,
				horizonpolicy.WithAIExecutor(exec),
				horizonpolicy.WithValidationExecutor(validationPolicyExecutor),
				horizonpolicy.WithGeoNamesClient(geonamesClientForHorizon),
			)
			scoutEngine.WithPolicyChecker(policyChecker)
			logger.Printf("PolicyChecker initialized and wired to ScoutEngine")

			// Initialize enforcement executor and persistence for policy auto-fixes
			enforcementExecutor := horizonpolicy.NewEnforcementExecutor(
				horizonFragmentsRepo,
				horizonEdgesRepo,
				enforcementJobsRepo,
			)
			enforcementExecutor.WithAIExecutor(exec)
			// Allow enforcement to rewrite lens scope after path fixes (best-effort).
			enforcementExecutor.WithLensesRepo(horizonLensesRepo)
			if horizonFragmentsMergeRepo != nil {
				enforcementExecutor.WithMergeFragments(horizonFragmentsMergeRepo)
			}
			if geonamesClientForHorizon != nil {
				enforcementExecutor.WithGeoNamesClient(geonamesClientForHorizon)
			}
			if canonicalPathResolver != nil {
				enforcementExecutor.WithRedirectRecorder(&enforcementRedirectRecorder{r: canonicalPathResolver})
			}
			scoutEngine.WithEnforcementExecutor(enforcementExecutor)
			scoutEngine.WithEnforcementJobsRepo(enforcementJobsRepo)
			scoutEngine.WithPolicyCheckResultsRepo(policyCheckResultsRepo)
			enforcementHandlers = horizonhttp.NewEnforcementHandlers(
				enforcementJobsRepo,
				policyCheckResultsRepo,
				enforcementExecutor,
			)
			logger.Printf("Policy enforcement wired to ScoutEngine")

			logger.Printf("Scout Engine initialized")

			// Wire CanonicalPathResolver into LensService for early canonicalization
			if lensService != nil && canonicalPathResolver != nil {
				lensService.WithCanonicalPathResolver(canonicalPathResolver)
			}

			// Initialize Watchdog subsystem (requires ScoutEngine + SourceExecutor)
			// Watchdog (background monitoring) can generate large volumes of Scout jobs.
			// Keep it disabled by default; enable explicitly via env.
			var watchdogService *horizonwatchdog.WatchdogService
			if watchdogEnabledViaEnv() {
				watchdogMonitor := horizonwatchdog.NewMonitor(s.dsrcs, docsRepo, horizonFragmentsRepo, ontologyFactsRepo, horizonScoutingJobsRepo, scoutEngine, sourceExecutor, exec).
					WithRiskJobsRepository(riskJobsRepo).
					WithScopeAgentWakeHook(func(ctx context.Context, tenantID, canonicalPath, triggerType, triggerReason string, trace map[string]any) error {
						_, err := scopeAgentService.WakeByCanonicalPath(ctx, tenantID, canonicalPath, triggerType, triggerReason, trace)
						return err
					}).
					WithTriggerHook(func(ctx context.Context, config *horizonmodels.WatchdogConfig, fragment *horizonmodels.HorizonFragment, trigger *horizonmodels.WatchTrigger) error {
						if _, err := watchRuntimeService.EnsureSubjectForWatchdogConfig(ctx, config); err != nil {
							return err
						}
						trigger.SubjectID = strings.TrimSpace(config.SubjectID)
						if len(trigger.CanonicalPaths) == 0 && fragment != nil && strings.TrimSpace(fragment.CanonicalPath) != "" {
							trigger.CanonicalPaths = []string{strings.TrimSpace(fragment.CanonicalPath)}
						}
						_, _, err := watchRuntimeService.EmitAndProcessTrigger(ctx, trigger)
						return err
					})
				watchdogScheduler := horizonwatchdog.NewScheduler()
				watchdogService = horizonwatchdog.NewWatchdogService(
					horizonWatchdogRepo,
					horizonFragmentsRepo,
					watchdogMonitor,
					watchdogScheduler,
					scoutEngine,
				)
				horizonService.SetWatchdogService(watchdogService)
				logger.Printf("Horizon Watchdog enabled")
			} else {
				logger.Printf("Horizon Watchdog disabled (set ENABLE_NV_HORIZON_WATCHDOG=1 to enable)")
			}
			if lensService != nil {
				lensService.SetWatchdogLifecycleHook(func(ctx context.Context, fragmentID string) {
					_ = watchRuntimeService.SyncSubjectsForFragment(ctx, fragmentID)
					if watchdogService != nil {
						_ = watchdogService.UpdateConfigLifecycle(ctx, fragmentID)
					}
				})
			}

			// Legacy Firestore poller (disabled by default in prod).
			// Cloud Tasks is the preferred mechanism for executing Scout jobs.
			scoutFirestorePollerEnabled := strings.TrimSpace(getEnv("ENABLE_SCOUT_FIRESTORE_POLLER", "0")) == "1"
			if !scoutExecutionReady && scoutFirestorePollerEnabled {
				scoutExecutionReady = true
				scoutExecutionReason = "scout_firestore_poller_enabled"
			}
			if scoutFirestorePollerEnabled {
				go func() {
					// Задержка перед стартом
					time.Sleep(5 * time.Second)
					logger.Printf("Scout Worker started polling")
					// Limit concurrency to avoid spawning unbounded goroutines and to reduce starvation under backlog.
					const scoutWorkerMaxConcurrent = 3
					sem := make(chan struct{}, scoutWorkerMaxConcurrent)
					ticker := time.NewTicker(10 * time.Second)
					defer ticker.Stop()

					for {
						select {
						case <-ctxBackground.Done():
							return
						case <-ticker.C:
							// Получаем ожидающие задачи.
							// First, prioritize "fresh" jobs to avoid high latency under backlog.
							seen := map[string]struct{}{}
							cutoff := time.Now().UTC().Add(-15 * time.Minute)
							jobs, err := horizonScoutingJobsRepo.ListPendingSince(context.Background(), cutoff, 25)
							if err != nil {
								logger.Printf("Scout Worker poll error: %v", err)
								continue
							}

							// Then, also drain the backlog (FIFO).
							backlog, err := horizonScoutingJobsRepo.ListPending(context.Background(), 25)
							if err != nil {
								logger.Printf("Scout Worker poll error (backlog): %v", err)
								continue
							}
							jobs = append(jobs, backlog...)

							// Priority: Lens-driven jobs (have state.lensId) should not be starved by global backlog.
							// This keeps Hook→Assemble latency acceptable even when watchdog/system jobs pile up.
							lensFirst := make([]*horizonmodels.ScoutingJob, 0, len(jobs))
							rest := make([]*horizonmodels.ScoutingJob, 0, len(jobs))
							for _, j := range jobs {
								if j == nil {
									continue
								}
								lid := strings.TrimSpace(j.LensID)
								if lid == "" && j.State != nil {
									if v, ok := j.State["lensId"].(string); ok {
										lid = strings.TrimSpace(v)
									}
								}
								if lid != "" {
									lensFirst = append(lensFirst, j)
								} else {
									rest = append(rest, j)
								}
							}
							jobs = append(lensFirst, rest...)

							for _, job := range jobs {
								if job == nil || job.ID == "" {
									continue
								}
								if _, ok := seen[job.ID]; ok {
									continue
								}
								seen[job.ID] = struct{}{}
								sem <- struct{}{}
								// Выполняем задачу асинхронно
								go func(j *horizonmodels.ScoutingJob) {
									defer func() { <-sem }()
									execCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
									defer cancel()
									logger.Printf("Scout Worker executing job %s for %s", j.ID, j.TargetCanonicalPath)
									if err := scoutEngine.ExecuteScoutingJob(execCtx, j); err != nil {
										logger.Printf("Scout Job %s failed: %v", j.ID, err)
									} else {
										logger.Printf("Scout Job %s completed", j.ID)
									}
								}(job)
							}
						}
					}
				}()
			}

			// Horizon Fragment Profiles (LLM-based)
			if aiExec != nil {
				profileGenerator = horizonprofile.NewGenerator(aiExec, horizonFragmentsRepo, horizonEdgesRepo, horizonProfilesRepo)
				profileScheduler = horizonprofile.NewScheduler(
					profileGenerator,
					horizonFragmentsRepo,
					horizonEdgesRepo,
					horizonProfilesRepo,
					horizonProfilePoliciesRepo,
					profileTasksDispatcher,
				)
				profilesHandler = horizonhttp.NewProfilesHandler(horizonProfilesRepo, profileGenerator, profileScheduler)
			} else {
				logger.Printf("WARNING: Profile generator not initialized (ai executor is nil)")
			}

			oneHopDownstreamService = horizonsupplychain.NewOneHopDownstreamService(horizonsupplychain.OneHopDownstreamServiceConfig{
				Exec:             exec,
				FragmentsRepo:    horizonFragmentsRepo,
				EdgesRepo:        horizonEdgesRepo,
				MaterializedRepo: horizonMaterializedArtifactsRepo,
				LensesRepo:       horizonLensesRepo,
				ScoutingJobsRepo: horizonScoutingJobsRepo,
				ProductDict:      productDictionary,
				PathResolver:     canonicalPathResolver,
				Logger:           logger,
				Publisher: func(ctx context.Context, eventType string, tenantID string, payload interface{}) error {
					if pub == nil {
						return nil
					}
					return pub.PublishJSON(ctx, realtime.EventType(eventType), tenantID, payload)
				},
			})
			candidateChainCompletionService := horizonsupplychain.NewCandidateChainCompletionService(horizonsupplychain.CandidateChainCompletionDeps{
				LensesRepo:       horizonLensesRepo,
				MaterializedRepo: horizonMaterializedArtifactsRepo,
			})

			// Create HTTP handlers
			horizonHandlers = horizonhttp.NewHorizonHandlers(horizonService).
				WithLensItemsHandler(horizonLensItemsHandler).
				WithMergeFragmentsRepo(horizonFragmentsMergeRepo).
				WithMaterializedArtifactsRepo(horizonMaterializedArtifactsRepo).
				WithCoreDocsRepo(coreDocsRepo).
				WithGCSClient(gcsClient).
				WithOneHopDownstreamService(oneHopDownstreamService).
				WithCandidateChainCompletionService(candidateChainCompletionService).
				WithProductDictionary(productDictionary).
				WithDocumentsRepo(docsRepo).
				WithUsersRepo(s.users).
				WithGenAIHandler(genaiHandler).
				WithInteractionsRepo(interactionsRepo).
				WithOntologyFactsRepo(ontologyFactsRepo).
				WithExposurePathsRepo(horizonExposurePathsRepo).
				WithRiskJobsRepo(riskJobsRepo).
				WithWatchService(watchService).
				WithWatchRuntime(watchRuntimeService).
				WithProfilesHandler(profilesHandler).
				WithPropagationProfileLookup(propagationProfileLookup)

			// SignalPipeline for manual 12-axis extraction (events:extract-signals)
			var signalPipelineForQA *horizonscout.SignalPipeline
			if aiExec != nil && ontologyFactsRepo != nil {
				signalExtractor := horizonsignals.NewSignalExtractor(nil, aiExec)
				applicability := horizonsignals.NewExternalApplicabilityEvaluator().
					WithFragmentsRepo(horizonFragmentsRepo).
					WithEdgesRepo(horizonEdgesRepo)
				if horizonExposurePathsRepo != nil {
					applicability = applicability.WithExposurePathsRepo(horizonExposurePathsRepo)
				}
				seedInjector := horizonsignals.NewSeedInjector(horizonFragmentsRepo, horizonEdgesRepo).
					WithApplicabilityEvaluator(applicability)
				if horizonMaterializedArtifactsRepo != nil {
					seedInjector = seedInjector.WithMaterializedRepo(horizonMaterializedArtifactsRepo)
				}
				if vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
					if horizonDS := getEnvWithAlt("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID", "VERTEX_SEARCH_HORIZON_DATA_STORE_ID", ""); horizonDS != "" {
						seedInjector = seedInjector.WithVertexSearch(vertexSearchClient.WithDataStore(horizonDS))
					}
				}
				signalPipeline := horizonscout.NewSignalPipeline(
					signalExtractor,
					seedInjector,
					propagationEngine,
					propagationProfileLookup,
					ontologyFactsRepo,
				).WithEscalationLLM(aiExec)
				signalPipelineForQA = signalPipeline
				horizonHandlers = horizonHandlers.WithSignalPipeline(signalPipeline)

				// Canvas ingester: system sources → batch LLM extraction → ontology facts → 12-axis signals
				canvasIngester = discovery.NewCanvasIngester(discovery.CanvasIngesterConfig{
					Sources:        s.dsrcs,
					ProvidersReg:   reg,
					Exec:           exec,
					OntologyExt:    ontologyExtractor,
					FactsRepo:      ontologyFactsRepo,
					SignalPipeline: signalPipeline,
					FetchHM:        orchestrator.FetchHorizonMonitorDocuments,
					Logger:         logger,
				})
				logger.Printf("Canvas ingester initialized")

				// News-by-fragments canvas: Gemini search grounding → facts (with CanonicalNames) → 12-axis → propagation; result via WS
				// ontologyFactsRepo and signalPipeline are non-nil here (we're inside their init block)
				if pub != nil {
					var fragmentLookup discovery.FragmentLookup
					if horizonFragmentsRepo != nil {
						fragmentLookup = &fragmentLookupForNewsCanvas{repo: horizonFragmentsRepo}
					}
					newsByFragmentsCanvas := discovery.NewNewsByFragmentsCanvas(discovery.NewsByFragmentsCanvasConfig{
						Exec:            exec,
						FactsRepo:       ontologyFactsRepo,
						DataSourcesRepo: s.dsrcs,
						SignalPipeline:  signalPipeline,
						FragmentLookup:  fragmentLookup,
						Publisher: func(ctx context.Context, eventType string, tenantID string, payload interface{}) error {
							return pub.PublishJSON(ctx, realtime.EventType(eventType), tenantID, payload)
						},
						Logger: logger,
					})
					horizonHandlers = horizonHandlers.WithNewsByFragmentsCanvas(newsByFragmentsCanvas)
					logger.Printf("News-by-fragments canvas initialized (Gemini search grounding)")

					identifyStressesCanvas := horizonhttp.NewIdentifyStressesCanvas(horizonhttp.IdentifyStressesCanvasConfig{
						Exec:                   exec,
						FactsRepo:              ontologyFactsRepo,
						DataSourcesRepo:        s.dsrcs,
						FragmentsRepo:          horizonFragmentsRepo,
						EdgesRepo:              horizonEdgesRepo,
						MaterializedRepo:       horizonMaterializedArtifactsRepo,
						LensesRepo:             horizonLensesRepo,
						SignalPipeline:         signalPipeline,
						CapabilityProfilesRepo: horizonCapabilityProfilesRepo,
						CapabilityObsRepo:      horizonCapabilityObsRepo,
						SupplyLinkObsRepo:      horizonSupplyLinkObsRepo,
						RouteObsRepo:           horizonRouteObsRepo,
						DerivedRiskRepo:        horizonDerivedRiskProfilesRepo,
						Publisher: func(ctx context.Context, eventType string, tenantID string, payload interface{}) error {
							return pub.PublishJSON(ctx, realtime.EventType(eventType), tenantID, payload)
						},
						Logger: logger,
					})
					horizonHandlers = horizonHandlers.WithIdentifyStressesCanvas(identifyStressesCanvas)
					logger.Printf("Identify-stresses canvas initialized")

					completeSubstrateCanvas := horizonhttp.NewCompleteSubstrateCanvas(horizonhttp.CompleteSubstrateCanvasConfig{
						Exec:             exec,
						FragmentsRepo:    horizonFragmentsRepo,
						EdgesRepo:        horizonEdgesRepo,
						MaterializedRepo: horizonMaterializedArtifactsRepo,
						LensesRepo:       horizonLensesRepo,
						Publisher: func(ctx context.Context, eventType string, tenantID string, payload interface{}) error {
							return pub.PublishJSON(ctx, realtime.EventType(eventType), tenantID, payload)
						},
						Logger: logger,
					})
					horizonHandlers = horizonHandlers.WithCompleteSubstrateCanvas(completeSubstrateCanvas)
					logger.Printf("Complete-substrate backend initialized")

					recoverMetricsCanvas := horizonhttp.NewRecoverMetricsCanvas(horizonhttp.RecoverMetricsCanvasConfig{
						Exec:                   exec,
						FragmentsRepo:          horizonFragmentsRepo,
						EdgesRepo:              horizonEdgesRepo,
						MaterializedRepo:       horizonMaterializedArtifactsRepo,
						LensesRepo:             horizonLensesRepo,
						CapabilityProfilesRepo: horizonCapabilityProfilesRepo,
						CapabilityObsRepo:      horizonCapabilityObsRepo,
						SupplyLinkObsRepo:      horizonSupplyLinkObsRepo,
						RouteObsRepo:           horizonRouteObsRepo,
						DerivedRiskRepo:        horizonDerivedRiskProfilesRepo,
						Publisher: func(ctx context.Context, eventType string, tenantID string, payload interface{}) error {
							return pub.PublishJSON(ctx, realtime.EventType(eventType), tenantID, payload)
						},
						Logger: logger,
					})
					horizonHandlers = horizonHandlers.WithRecoverMetricsCanvas(recoverMetricsCanvas)
					logger.Printf("Recover-metrics backend initialized")
				}
			}

			// Lens Q&A (Analysis view): Decompose → Simulate → Interpret (at finish) → Synthesize
			if exec != nil && propagationEngine != nil && propagationProfileLookup != nil &&
				horizonFragmentsRepo != nil && horizonMaterializedArtifactsRepo != nil && ontologyFactsRepo != nil {
				horizonQASessionsRepo := horizonrepo.NewQASessionsRepository(fsClient.Client())
				qaDecomposer := horizonqa.NewQuestionDecomposer(exec)
				qaSeedResolver := horizonqa.NewSeedResolver()
				qaLeafEvidence := horizonqa.NewLeafEvidenceCollector(horizoninterp.NewOntologyEventFactsResolver(ontologyFactsRepo))
				qaEngine := horizonqa.NewEngine(
					qaDecomposer,
					qaSeedResolver,
					qaLeafEvidence,
					propagationEngine,
					propagationProfileLookup,
					horizonFragmentsRepo,
					horizonMaterializedArtifactsRepo,
				)
				qaContextBuilder := horizonqa.NewAnswerContextBuilder()
				qaSynthesizer := horizonqa.NewAnswerSynthesizer(exec)
				horizonHandlers = horizonHandlers.WithQAEngine(qaEngine, qaContextBuilder, qaSynthesizer, horizonQASessionsRepo).
					WithQASufficiencyChecker(horizonqa.NewSufficiencyChecker()).
					WithQASignalGapDetector(horizonqa.NewSignalGapDetector()).
					WithQAGroundingQueryBuilder(horizonqa.NewGroundingQueryBuilder()).
					WithScoutingJobsRepo(horizonScoutingJobsRepo)
				if signalPipelineForQA != nil && docsRepo != nil {
					horizonHandlers = horizonHandlers.WithQABestEffortFiller(
						horizonqa.NewBestEffortFiller(exec, docsRepo, ontologyFactsRepo, signalPipelineForQA, horizonFragmentsRepo),
					)
				}
				logger.Printf("Lens Q&A (Analysis view) initialized")
			}

			if horizonJobTelemetryRepo != nil {
				horizonHandlers = horizonHandlers.WithJobTelemetryRepo(horizonJobTelemetryRepo)
				logger.Printf("Horizon job telemetry API initialized")
			}

			logger.Printf("Horizon system initialized")

			// Wire Horizon Lens service to projects handler for cascade delete
			if horizonService != nil && horizonService.Lens() != nil {
				s.projectsHandler = s.projectsHandler.WithLensService(horizonService.Lens())
				logger.Printf("Horizon Lens service wired to projects handler for cascade delete")
			}
		}
	}

	// Get auth client for middleware
	authClient, err := fbApp.Auth(ctx)
	if err != nil {
		log.Fatalf("auth client init: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	// LB-friendly health check (some setups route only /api/* to the service)
	mux.HandleFunc("/api/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	// AIS vessel positions (AISStream proxy). Key from env AISSTREAM_API_KEY or GCP secret aisstream_api_key; no auth.
	aisstreamVesselsHandler := httphandlers.NewAISStreamVesselsHandler(logger, projectID)
	mux.Handle("/api/v1/vessels/bbox", aisstreamVesselsHandler)
	// Foreground root jobs and background child/refine jobs should not compete for the same tiny slot pool.
	// Otherwise a continuation storm can starve a new hot-path root job and destroy time-to-first-answer.
	scoutDefaultInflightPerInstance := getEnvInt("SCOUT_MAX_INFLIGHT_PER_INSTANCE", 2)
	if scoutDefaultInflightPerInstance < 1 {
		scoutDefaultInflightPerInstance = 1
	}
	scoutForegroundInflightPerInstance := getEnvInt("SCOUT_MAX_FOREGROUND_INFLIGHT_PER_INSTANCE", scoutDefaultInflightPerInstance)
	if scoutForegroundInflightPerInstance < 1 {
		scoutForegroundInflightPerInstance = 1
	}
	scoutBackgroundInflightPerInstance := getEnvInt("SCOUT_MAX_BACKGROUND_INFLIGHT_PER_INSTANCE", scoutDefaultInflightPerInstance*2)
	if scoutBackgroundInflightPerInstance < 1 {
		scoutBackgroundInflightPerInstance = 1
	}
	scoutForegroundLimiter := make(chan struct{}, scoutForegroundInflightPerInstance)
	scoutBackgroundLimiter := make(chan struct{}, scoutBackgroundInflightPerInstance)
	var scoutInFlightByJob sync.Map
	logger.Printf("Scout task limiter enabled: foreground=%d background=%d default=%d",
		scoutForegroundInflightPerInstance,
		scoutBackgroundInflightPerInstance,
		scoutDefaultInflightPerInstance,
	)

	// Cloud Tasks -> Scout job execution (managed queue + retries + autoscaling).
	// Secured via OIDC token issued for the configured service account.
	mux.HandleFunc("/api/v1/internal/tasks/scout/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if scoutEngine == nil || horizonService == nil {
			http.Error(w, "horizon scout not initialized", http.StatusServiceUnavailable)
			return
		}

		// Verify Cloud Tasks OIDC token.
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		const bearer = "Bearer "
		if !strings.HasPrefix(authz, bearer) || len(authz) <= len(bearer) {
			http.Error(w, "missing Authorization bearer token", http.StatusUnauthorized)
			return
		}
		rawTok := strings.TrimSpace(authz[len(bearer):])
		aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
		if aud == "" {
			// Default to service URL (scheme+host).
			aud = "https://" + r.Host
		}
		payload, err := idtoken.Validate(r.Context(), rawTok, aud)
		if err != nil {
			http.Error(w, "invalid cloud tasks token", http.StatusUnauthorized)
			return
		}
		expectedSA := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
		if expectedSA != "" {
			if email, ok := payload.Claims["email"].(string); !ok || !strings.EqualFold(strings.TrimSpace(email), expectedSA) {
				http.Error(w, "unexpected token identity", http.StatusForbidden)
				return
			}
		}

		var body struct {
			JobID string `json:"jobId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		jobID := strings.TrimSpace(body.JobID)
		if jobID == "" {
			http.Error(w, "jobId is required", http.StatusBadRequest)
			return
		}

		// Load job and reconstruct scouting-specific fields from state.
		job, err := horizonService.ScoutingJobs().Get(r.Context(), jobID)
		if err != nil || job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		if job.State != nil {
			if v, ok := job.State["targetCanonicalPath"].(string); ok && strings.TrimSpace(v) != "" {
				job.TargetCanonicalPath = strings.TrimSpace(v)
			}
			if v, ok := job.State["currentState"].(string); ok && strings.TrimSpace(v) != "" {
				job.CurrentState = horizonmodels.ScoutingJobState(strings.TrimSpace(v))
			}
			if v, ok := job.State["lensId"].(string); ok && strings.TrimSpace(v) != "" {
				job.LensID = strings.TrimSpace(v)
			}
			if v, ok := job.State["scoutingStrategy"].(string); ok && strings.TrimSpace(v) != "" {
				job.ScoutingStrategy = strings.TrimSpace(v)
			}
		}
		if skip, finalize, reason := shouldSkipScoutTaskExecution(job); skip {
			logger.Printf("Scout job %s delivery skipped before execute: reason=%s state=%s", jobID, reason, job.CurrentState)
			if finalize {
				_ = scoutEngine.FinalizePipelineRunForceFailed(context.Background(), job, reason)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("skipped: " + reason))
			return
		}
		if _, alreadyRunning := scoutInFlightByJob.LoadOrStore(jobID, struct{}{}); alreadyRunning {
			logger.Printf("Scout job %s duplicate delivery while in-flight; ACK", jobID)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("already in-flight"))
			return
		}
		defer scoutInFlightByJob.Delete(jobID)
		stateString := func(key string) string {
			if job.State == nil {
				return ""
			}
			if value, ok := job.State[key].(string); ok {
				return strings.TrimSpace(value)
			}
			if value, ok := job.State[key]; ok && value != nil {
				return strings.TrimSpace(fmt.Sprintf("%v", value))
			}
			return ""
		}
		jobClass := "foreground"
		limiter := scoutForegroundLimiter
		if stateString("parentJobId") != "" || stateString("mode") == "refine_existing_assembly" {
			jobClass = "background"
			limiter = scoutBackgroundLimiter
		}
		select {
		case limiter <- struct{}{}:
			defer func() { <-limiter }()
		default:
			logger.Printf("Scout executor saturated: class=%s jobId=%s target=%s", jobClass, jobID, job.TargetCanonicalPath)
			http.Error(w, "scout executor saturated (retry later)", http.StatusTooManyRequests)
			return
		}

		// Heavy supply-chain jobs must survive request-scoped cancellation long enough to finish materialization.
		// Keep the execution budget configurable, but capped below the Cloud Run request timeout ceiling.
		execBase := context.Background()
		if rc, ok := horizonrtctx.Get(r.Context()); ok {
			execBase = horizonrtctx.With(execBase, rc)
		}
		execTimeout := getEnvDuration("SCOUT_TASK_EXEC_TIMEOUT", 55*time.Minute)
		if execTimeout <= 0 {
			execTimeout = 55 * time.Minute
		}
		if execTimeout > 59*time.Minute {
			execTimeout = 59 * time.Minute
		}
		execCtx, cancel := context.WithTimeout(execBase, execTimeout)
		defer cancel()

		execErr := error(nil)
		if horizonsupplychain.IsOneHopJob(job) {
			if oneHopDownstreamService == nil {
				execErr = fmt.Errorf("one-hop downstream service is not initialized")
			} else {
				execErr = oneHopDownstreamService.ExecuteJob(execCtx, job)
			}
		} else {
			execErr = scoutEngine.ExecuteScoutingJob(execCtx, job)
		}
		if err := execErr; err != nil {
			logger.Printf("ERROR: Scout job execution failed: jobId=%s, canonicalPath=%s, error=%v", jobID, job.TargetCanonicalPath, err)

			// If the job is already in a terminal state (rejected/failed/verified),
			// ACK immediately — retrying won't change the outcome.
			refreshedJob, _ := horizonService.ScoutingJobs().Get(context.Background(), jobID)
			if refreshedJob != nil {
				switch refreshedJob.CurrentState {
				case horizonmodels.ScoutingJobStateRejected, horizonmodels.ScoutingJobStateFailed, horizonmodels.ScoutingJobStateVerified:
					logger.Printf("Scout job %s is terminal (state=%s), ACK to stop retries", jobID, refreshedJob.CurrentState)
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(fmt.Sprintf("terminal: %s", refreshedJob.CurrentState)))
					return
				}
			}

			// Deterministic data-contract failures should not be retried by Cloud Tasks:
			// retries only replay expensive LLM stages and create load spikes.
			jobForCheck := job
			if refreshedJob != nil {
				jobForCheck = refreshedJob
			}
			if isNonRetryableScoutExecutionErrorWithJob(err, jobForCheck) {
				logger.Printf("Scout job %s failed with non-retryable error; finalizing and ACK", jobID)
				// Finalize before ACK if job not yet terminal (error returned before finalize).
				// Use job (mutated by ExecuteScoutingJob) for pipeline state; it has the latest stage info.
				if job.CurrentState != horizonmodels.ScoutingJobStateRejected && job.CurrentState != horizonmodels.ScoutingJobStateFailed && job.CurrentState != horizonmodels.ScoutingJobStateVerified {
					_ = scoutEngine.FinalizePipelineRunForceFailed(context.Background(), job, "non-retryable contract error")
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("failed (non-retryable)"))
				return
			}

			// Transient failure: return 500 to let Cloud Tasks retry.
			retryCount := 0
			if v := strings.TrimSpace(r.Header.Get("X-CloudTasks-TaskRetryCount")); v != "" {
				if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
					retryCount = n
				}
			}
			if retryCount >= 4 {
				logger.Printf("ERROR: Scout job failed after max retries: jobId=%s, marking as failed", jobID)
				// Use the scouting repo on the poison path too, so on-complete hooks still run:
				// assembly completion, horizon.assembly.ready publication, and internal knowledge warmup
				// must continue to work even when Cloud Tasks gives up retrying a stuck job.
				_ = horizonscout.NewScoutWorkflow(agentJobsRepo).
					WithScoutingJobsRepo(horizonService.ScoutingJobs()).
					TransitionState(context.Background(), jobID, horizonmodels.ScoutingJobStateFailed)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("failed (poisoned)"))
				return
			}
			http.Error(w, fmt.Sprintf("scout execution failed (will retry): %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Cloud Tasks -> Risk job execution (separate queue and lifecycle from Scout).
	mux.HandleFunc("/api/v1/internal/tasks/risk/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if riskPipeline == nil {
			http.Error(w, "risk pipeline not initialized", http.StatusServiceUnavailable)
			return
		}

		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		const bearer = "Bearer "
		if !strings.HasPrefix(authz, bearer) || len(authz) <= len(bearer) {
			http.Error(w, "missing Authorization bearer token", http.StatusUnauthorized)
			return
		}
		rawTok := strings.TrimSpace(authz[len(bearer):])
		aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
		if aud == "" {
			aud = "https://" + r.Host
		}
		payload, err := idtoken.Validate(r.Context(), rawTok, aud)
		if err != nil {
			http.Error(w, "invalid cloud tasks token", http.StatusUnauthorized)
			return
		}
		expectedSA := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
		if expectedSA != "" {
			if email, ok := payload.Claims["email"].(string); !ok || !strings.EqualFold(strings.TrimSpace(email), expectedSA) {
				http.Error(w, "unexpected token identity", http.StatusForbidden)
				return
			}
		}

		var body struct {
			JobID string `json:"jobId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		jobID := strings.TrimSpace(body.JobID)
		if jobID == "" {
			http.Error(w, "jobId is required", http.StatusBadRequest)
			return
		}

		execCtx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()

		job, runErr := riskPipeline.RunJob(execCtx, jobID)
		if runErr != nil {
			retryCount := 0
			if v := strings.TrimSpace(r.Header.Get("X-CloudTasks-TaskRetryCount")); v != "" {
				if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
					retryCount = n
				}
			}
			if job != nil {
				terminal := job.CurrentState == horizonmodels.RiskJobStateCompleted ||
					job.CurrentState == horizonmodels.RiskJobStateFailed ||
					job.CurrentState == horizonmodels.RiskJobStatePoisoned ||
					job.CurrentState == horizonmodels.RiskJobStateCancelled
				if terminal || retryCount >= 4 {
					if !terminal && retryCount >= 4 && riskJobsRepo != nil {
						job.CurrentState = horizonmodels.RiskJobStatePoisoned
						if strings.TrimSpace(job.TerminalReason) == "" {
							job.TerminalReason = "E_STAGE_TIMEOUT"
						}
						_ = riskJobsRepo.Update(context.Background(), job)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"status": "ok",
						"jobId":  jobID,
						"state":  string(job.CurrentState),
					})
					return
				}
			}
			http.Error(w, fmt.Sprintf("risk execution failed (will retry): %v", runErr), http.StatusInternalServerError)
			return
		}

		state := string(horizonmodels.RiskJobStateCompleted)
		if job != nil {
			state = string(job.CurrentState)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"jobId":  jobID,
			"state":  state,
		})
	})

	mux.HandleFunc("/api/v1/internal/tasks/scope-agents/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if horizonService == nil || horizonService.ScopeAgents() == nil {
			http.Error(w, "scope agent service not initialized", http.StatusServiceUnavailable)
			return
		}

		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		const bearer = "Bearer "
		if !strings.HasPrefix(authz, bearer) || len(authz) <= len(bearer) {
			http.Error(w, "missing Authorization bearer token", http.StatusUnauthorized)
			return
		}
		rawTok := strings.TrimSpace(authz[len(bearer):])
		aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
		if aud == "" {
			aud = "https://" + r.Host
		}
		payload, err := idtoken.Validate(r.Context(), rawTok, aud)
		if err != nil {
			http.Error(w, "invalid cloud tasks token", http.StatusUnauthorized)
			return
		}
		expectedSA := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
		if expectedSA != "" {
			if email, ok := payload.Claims["email"].(string); !ok || !strings.EqualFold(strings.TrimSpace(email), expectedSA) {
				http.Error(w, "unexpected token identity", http.StatusForbidden)
				return
			}
		}

		var body struct {
			TenantID string `json:"tenantId"`
			Limit    int    `json:"limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		tenantID := strings.TrimSpace(body.TenantID)
		if tenantID == "" {
			tenantID = "global"
		}
		limit := body.Limit
		if limit <= 0 {
			limit = 8
		}
		reports, err := horizonService.ScopeAgents().RunDueBatches(r.Context(), tenantID, limit)
		if err != nil {
			http.Error(w, fmt.Sprintf("scope agent batch execution failed: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"tenantId": tenantID,
			"reports":  reports,
		})
	})

	// Cloud Tasks -> Profile refresh execution (managed queue + retries + autoscaling).
	// Secured via OIDC token issued for the configured service account.
	mux.HandleFunc("/api/v1/internal/tasks/profile/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if profileGenerator == nil {
			http.Error(w, "profile generator not initialized", http.StatusServiceUnavailable)
			return
		}

		// Verify Cloud Tasks OIDC token.
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		const bearer = "Bearer "
		if !strings.HasPrefix(authz, bearer) || len(authz) <= len(bearer) {
			http.Error(w, "missing Authorization bearer token", http.StatusUnauthorized)
			return
		}
		rawTok := strings.TrimSpace(authz[len(bearer):])
		aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
		if aud == "" {
			aud = "https://" + r.Host
		}
		payload, err := idtoken.Validate(r.Context(), rawTok, aud)
		if err != nil {
			http.Error(w, "invalid cloud tasks token", http.StatusUnauthorized)
			return
		}
		expectedSA := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
		if expectedSA != "" {
			if email, ok := payload.Claims["email"].(string); !ok || !strings.EqualFold(strings.TrimSpace(email), expectedSA) {
				http.Error(w, "unexpected token identity", http.StatusForbidden)
				return
			}
		}

		var body struct {
			FragmentID string `json:"fragmentId"`
			Force      bool   `json:"force"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		fragmentID := strings.TrimSpace(body.FragmentID)
		if fragmentID == "" {
			http.Error(w, "fragmentId is required", http.StatusBadRequest)
			return
		}

		execCtx, cancel := context.WithTimeout(r.Context(), 45*time.Minute)
		defer cancel()

		opts := horizonprofile.DefaultGenerateOptions()
		opts.ForceRegenerate = body.Force
		result, err := profileGenerator.Generate(execCtx, fragmentID, opts)
		if err != nil {
			logger.Printf("ERROR: Profile refresh failed: fragmentId=%s, error=%v", fragmentID, err)

			retryCount := 0
			if v := strings.TrimSpace(r.Header.Get("X-CloudTasks-TaskRetryCount")); v != "" {
				if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
					retryCount = n
				}
			}
			if retryCount >= 4 {
				logger.Printf("ERROR: Profile refresh failed after max retries: fragmentId=%s, acking", fragmentID)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("failed (poisoned)"))
				return
			}
			http.Error(w, fmt.Sprintf("profile refresh failed (will retry): %v", err), http.StatusInternalServerError)
			return
		}

		logger.Printf("Profile refresh completed: fragmentId=%s profileId=%s created=%t", fragmentID, result.Profile.ID, result.Created)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Cloud Tasks -> per-source polling execution.
	// Secured via OIDC token issued for the configured service account.
	mux.HandleFunc("/api/v1/internal/tasks/sources/poll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if sourcePoller == nil {
			http.Error(w, "source poller not initialized", http.StatusServiceUnavailable)
			return
		}

		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		const bearer = "Bearer "
		if !strings.HasPrefix(authz, bearer) || len(authz) <= len(bearer) {
			http.Error(w, "missing Authorization bearer token", http.StatusUnauthorized)
			return
		}
		rawTok := strings.TrimSpace(authz[len(bearer):])
		aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
		if aud == "" {
			aud = "https://" + r.Host
		}
		payload, err := idtoken.Validate(r.Context(), rawTok, aud)
		if err != nil {
			http.Error(w, "invalid cloud tasks token", http.StatusUnauthorized)
			return
		}
		expectedSA := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
		if expectedSA != "" {
			if email, ok := payload.Claims["email"].(string); !ok || !strings.EqualFold(strings.TrimSpace(email), expectedSA) {
				http.Error(w, "unexpected token identity", http.StatusForbidden)
				return
			}
		}

		var body discovery.SourceTaskPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.SourceID) == "" {
			http.Error(w, "sourceId is required", http.StatusBadRequest)
			return
		}

		execCtx, cancel := context.WithTimeout(r.Context(), 45*time.Minute)
		defer cancel()
		if err := sourcePoller.RunSourcePoll(execCtx, body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Cloud Tasks -> Canvas ingest (system sources → batch LLM → ontology facts → 12-axis signals).
	// Secured via OIDC token issued for the configured service account.
	mux.HandleFunc("/api/v1/internal/tasks/canvas/ingest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if canvasIngester == nil {
			http.Error(w, "canvas ingester not initialized", http.StatusServiceUnavailable)
			return
		}

		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		const bearer = "Bearer "
		if !strings.HasPrefix(authz, bearer) || len(authz) <= len(bearer) {
			http.Error(w, "missing Authorization bearer token", http.StatusUnauthorized)
			return
		}
		rawTok := strings.TrimSpace(authz[len(bearer):])
		aud := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_AUDIENCE", ""))
		if aud == "" {
			aud = "https://" + r.Host
		}
		payload, err := idtoken.Validate(r.Context(), rawTok, aud)
		if err != nil {
			http.Error(w, "invalid cloud tasks token", http.StatusUnauthorized)
			return
		}
		expectedSA := strings.TrimSpace(getEnv("CLOUD_TASKS_OIDC_SERVICE_ACCOUNT", ""))
		if expectedSA != "" {
			if email, ok := payload.Claims["email"].(string); !ok || !strings.EqualFold(strings.TrimSpace(email), expectedSA) {
				http.Error(w, "unexpected token identity", http.StatusForbidden)
				return
			}
		}

		var body struct {
			TenantID string `json:"tenantId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		tenantID := strings.TrimSpace(body.TenantID)
		if tenantID == "" {
			tenantID = "global"
		}

		execCtx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()

		docsFetched, eventsCreated, signalsProcessed, err := canvasIngester.Ingest(execCtx, tenantID)
		if err != nil {
			logger.Printf("ERROR: Canvas ingest failed: tenant=%s error=%v", tenantID, err)
			http.Error(w, fmt.Sprintf("canvas ingest failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"tenantId":         tenantID,
			"docsFetched":      docsFetched,
			"eventsCreated":    eventsCreated,
			"signalsProcessed": signalsProcessed,
		})
	})

	// WS hub and Pub/Sub subscriber (disabled when WS is brokered externally)
	hub := realtime.NewHub(logger)
	// ws endpoint: /ws (Authorization: Bearer <FirebaseIDToken> or ?token=<FirebaseIDToken>)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// Try to get token from Authorization header or query parameter
		idToken := ""
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(authHeader) > len(prefix) && authHeader[:len(prefix)] == prefix {
			idToken = authHeader[len(prefix):]
		} else {
			// Fallback to query parameter for WebSocket connections (browser limitation)
			idToken = r.URL.Query().Get("token")
		}

		if idToken == "" {
			http.Error(w, "missing or invalid Authorization", http.StatusUnauthorized)
			return
		}

		client, err := fbApp.Auth(r.Context())
		if err != nil {
			http.Error(w, "auth init error", http.StatusInternalServerError)
			return
		}
		tok, err := client.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		uid := tok.UID
		tenant := ""
		if u, err := s.users.GetByUID(r.Context(), uid); err == nil && u != nil {
			tenant = u.TenantID
		}
		if tenant == "" {
			http.Error(w, "tenant not resolved", http.StatusForbidden)
			return
		}
		hub.ServeWS(w, r, tenant)
	})

	// moved below after authMiddleware is defined

	// Start WS subscriber using the same project Pub/Sub client
	if os.Getenv("ENABLE_INTERNAL_WS_SUBSCRIBER") == "1" {
		projectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
		if projectID != "" {
			psClient, err := pubsub.NewClient(ctx, projectID)
			if err == nil {
				sub := realtime.NewSubscriber(psClient, hub, logger)
				_ = sub.Start(ctx, realtime.AggregatedTopics...)
			} else {
				log.Printf("ws subscriber: pubsub client init error: %v", err)
			}
		}
	}

	// Apply auth middleware to all /api/v1/* routes
	// Compose middleware: attach repos, then auth, then subscription service
	authMiddleware := func(h http.Handler) http.Handler {
		return httphandlers.WithUsersRepo(s.users)(
			httphandlers.AuthMiddleware(authClient)(
				httphandlers.WithSubscriptionService(subscriptionService)(h),
			),
		)
	}

	// Waitlist wiring
	wlRepo := repo.NewWaitlistRepository(fsClient.Client())
	wlHandler := httphandlers.NewWaitlistHandler(wlRepo, s.users, logger)

	// User routes
	mux.Handle("/api/v1/users", authMiddleware(http.HandlerFunc(s.usersHandler.CreateUser)))
	mux.Handle("/api/v1/users/", authMiddleware(http.HandlerFunc(s.routeUsersByID)))
	// Waitlist routes
	mux.Handle("/api/v1/waitlist", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			wlHandler.Join(w, r)
		case http.MethodGet:
			wlHandler.Check(w, r)
		default:
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		}
	}))
	mux.Handle("/api/v1/waitlist/approve", authMiddleware(http.HandlerFunc(wlHandler.Approve)))
	mux.Handle("/api/v1/waitlist/list", authMiddleware(http.HandlerFunc(wlHandler.List)))

	// Subscription routes
	subscriptionsHandler := httphandlers.NewSubscriptionsHandler(subscriptionService, logger)
	mux.Handle("/api/v1/subscriptions/me", authMiddleware(http.HandlerFunc(subscriptionsHandler.GetSubscription)))
	mux.Handle("/api/v1/subscriptions/voucher", authMiddleware(http.HandlerFunc(subscriptionsHandler.ApplyVoucher)))

	// PayPal routes
	var paypalHandler *httphandlers.PayPalHandler
	if paypalClient, err := paypal.NewClient(logger); err == nil {
		paypalHandler = httphandlers.NewPayPalHandler(paypalClient, subscriptionService, subsRepo, logger)
		mux.Handle("/api/v1/paypal/subscription", authMiddleware(http.HandlerFunc(paypalHandler.CreateSubscription)))
		mux.Handle("/api/v1/paypal/subscription/execute", authMiddleware(http.HandlerFunc(paypalHandler.ExecuteSubscription)))
		mux.Handle("/api/v1/paypal/subscription/cancel", authMiddleware(http.HandlerFunc(paypalHandler.CancelSubscription)))
		mux.Handle("/api/v1/paypal/webhook", http.HandlerFunc(paypalHandler.Webhook)) // No auth for webhook
		logger.Printf("PayPal integration enabled")
	} else {
		logger.Printf("PayPal integration disabled: %v", err)
	}

	// Voucher routes (admin only)
	vouchersHandler := httphandlers.NewVouchersHandler(vouchersRepo, s.users, logger)
	mux.Handle("/api/v1/vouchers", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			vouchersHandler.CreateVoucher(w, r)
		case http.MethodGet:
			vouchersHandler.ListVouchers(w, r)
		default:
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		}
	})))
	mux.Handle("/api/v1/vouchers/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(pathParts) < 4 {
			httphandlers.WriteBadRequest(w, "INVALID_PATH", "Voucher ID is required")
			return
		}
		voucherID := pathParts[3]
		switch r.Method {
		case http.MethodGet:
			// Extract voucher ID and call GetVoucher
			r.URL.Path = "/api/v1/vouchers/" + voucherID
			vouchersHandler.GetVoucher(w, r)
		case http.MethodPatch, http.MethodPut:
			r.URL.Path = "/api/v1/vouchers/" + voucherID
			vouchersHandler.UpdateVoucher(w, r)
		case http.MethodDelete:
			r.URL.Path = "/api/v1/vouchers/" + voucherID
			vouchersHandler.DeleteVoucher(w, r)
		default:
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		}
	})))

	// Horizon telemetry (admin only)
	telemetryHandler := httphandlers.NewHorizonTelemetryHandler(horizonTelemetryRepo, s.users, logger)
	mux.Handle("/api/v1/admin/horizon/telemetry/external-services", authMiddleware(http.HandlerFunc(telemetryHandler.ExternalServices)))
	mux.Handle("/api/v1/admin/horizon/telemetry", authMiddleware(http.HandlerFunc(telemetryHandler.List)))

	// Share routes handler (initialized before project sub-routes)
	shareRepo := repo.NewShareTokensRepository(fsClient.Client())
	if horizonHandlers != nil {
		horizonHandlers = horizonHandlers.WithShareTokensRepo(shareRepo)
	}
	shareHandler := httphandlers.NewShareHandler(shareRepo, s.projs, repo.NewInteractionsRepository(fsClient.Client()), logger)
	// Wire CoreDocs and storage for document access in shared projects
	if coreDocsRepo != nil && gcsClient != nil && bucket != "" {
		shareHandler = shareHandler.WithCoreDocs(coreDocsRepo).WithStorage(gcsClient, bucket)
	}

	// Project routes and nested sub-routes
	mux.Handle("/api/v1/projects", authMiddleware(http.HandlerFunc(s.routeProjects)))
	mux.Handle("/api/v1/projects/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sources") && r.Method == http.MethodPost {
			sourcesHandler.AttachSource(w, r)
			return
		}
		// GET /api/v1/projects/{id}/sources/{sourceId}/status
		if strings.Contains(r.URL.Path, "/sources/") && strings.HasSuffix(r.URL.Path, "/status") && r.Method == http.MethodGet {
			sourcesHandler.GetSourceProjectStatus(w, r)
			return
		}
		// PATCH /api/v1/projects/{id}/sources/{sourceId}/status
		if strings.Contains(r.URL.Path, "/sources/") && strings.HasSuffix(r.URL.Path, "/status") && r.Method == http.MethodPatch {
			sourcesHandler.UpdateSourceProjectStatus(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/sources/") && r.Method == http.MethodDelete {
			sourcesHandler.DetachSource(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/search") && r.Method == http.MethodGet {
			s.discoveryHandler.SearchDocuments(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/vector-search") && r.Method == http.MethodPost {
			s.discoveryHandler.VectorSearch(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/vertex-search") && r.Method == http.MethodPost {
			s.discoveryHandler.VertexAISearch(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/ingest") && r.Method == http.MethodPost {
			s.discoveryHandler.IngestProject(w, r)
			return
		}
		// Entities analyze: /projects/{id}/analyze/entities
		if strings.HasSuffix(r.URL.Path, "/analyze/entities") && r.Method == http.MethodPost {
			s.projectsHandler.AnalyzeEntities(w, r)
			return
		}
		// Facts analyze
		if strings.HasSuffix(r.URL.Path, "/analyze/facts") && r.Method == http.MethodPost {
			s.projectsHandler.AnalyzeFacts(w, r)
			return
		}
		// Events analyze
		if strings.HasSuffix(r.URL.Path, "/analyze/events") && r.Method == http.MethodPost {
			s.projectsHandler.AnalyzeEvents(w, r)
			return
		}
		// Metrics analyze
		if strings.HasSuffix(r.URL.Path, "/analyze/metrics") && r.Method == http.MethodPost {
			s.projectsHandler.AnalyzeMetrics(w, r)
			return
		}
		// Decisions analyze
		if strings.HasSuffix(r.URL.Path, "/analyze/decisions") && r.Method == http.MethodPost {
			s.projectsHandler.AnalyzeDecisions(w, r)
			return
		}
		// Core docs: /projects/{id}/docs and /projects/{id}/docs/upload
		if strings.HasSuffix(r.URL.Path, "/docs") {
			switch r.Method {
			case http.MethodGet:
				s.projectsHandler.ListProjectDocs(w, r)
				return
			case http.MethodPost:
				s.projectsHandler.CreateProjectDoc(w, r)
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, "/docs/upload") && r.Method == http.MethodPost {
			s.projectsHandler.UploadProjectDoc(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/docs/from-text") && r.Method == http.MethodPost {
			s.projectsHandler.CreateProjectDocFromText(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/docs/") && r.Method == http.MethodPatch {
			s.projectsHandler.UpdateProjectDoc(w, r)
			return
		}
		// Meta analysis endpoints
		if strings.HasSuffix(r.URL.Path, "/meta:analyze") && r.Method == http.MethodPost {
			if metaHandler == nil {
				httphandlers.WriteInternalError(w, "Meta analysis not configured")
				return
			}
			metaHandler.Analyze(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/meta/events") && r.Method == http.MethodGet {
			if metaHandler == nil {
				httphandlers.WriteInternalError(w, "Meta analysis not configured")
				return
			}
			metaHandler.ListEvents(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/meta/aggregate") && r.Method == http.MethodGet {
			if metaHandler == nil {
				httphandlers.WriteInternalError(w, "Meta analysis not configured")
				return
			}
			metaHandler.GetAggregate(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/parse") && strings.Contains(r.URL.Path, "/docs/") && r.Method == http.MethodPost {
			s.projectsHandler.ParseProjectDoc(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/download") && strings.Contains(r.URL.Path, "/docs/") && r.Method == http.MethodGet {
			s.projectsHandler.DownloadProjectDoc(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/index-status") && strings.Contains(r.URL.Path, "/docs/") && r.Method == http.MethodGet {
			s.projectsHandler.CheckDocumentIndexStatus(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/bigquery-status") && strings.Contains(r.URL.Path, "/docs/") && r.Method == http.MethodGet {
			s.projectsHandler.CheckDocumentBigQueryStatus(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/vertex-search-availability") && strings.Contains(r.URL.Path, "/docs/") && r.Method == http.MethodGet {
			s.projectsHandler.CheckDocumentVertexSearchAvailability(w, r)
			return
		}
		// Analytics endpoints: /api/v1/projects/{id}/analytics/*
		if strings.Contains(r.URL.Path, "/analytics/") && strings.Contains(r.URL.Path, "/projects/") {
			if strings.HasSuffix(r.URL.Path, "/analytics/schema") && r.Method == http.MethodGet {
				s.projectsHandler.GetBigQuerySchema(w, r)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/analytics/generate-sql") && r.Method == http.MethodPost {
				s.projectsHandler.GenerateSQL(w, r)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/analytics/execute-sql") && r.Method == http.MethodPost {
				s.projectsHandler.ExecuteSQL(w, r)
				return
			}
			// Prepare mode endpoints
			if strings.HasSuffix(r.URL.Path, "/prepare/analyze") && r.Method == http.MethodPost {
				s.projectsHandler.PrepareAnalyze(w, r)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/prepare/apply") && r.Method == http.MethodPost {
				s.projectsHandler.PrepareApply(w, r)
				return
			}
			// Legacy endpoint: /api/v1/projects/{id}/analytics/query
			if strings.HasSuffix(r.URL.Path, "/analytics/query") && r.Method == http.MethodPost {
				s.projectsHandler.QueryBigQuery(w, r)
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, "/export") && strings.Contains(r.URL.Path, "/docs/") && r.Method == http.MethodPost {
			s.projectsHandler.ExportDocToDocx(w, r)
			return
		}
		// delete core doc
		if strings.Contains(r.URL.Path, "/docs/") && r.Method == http.MethodDelete {
			s.projectsHandler.DeleteProjectDoc(w, r)
			return
		}
		// Share: create/revoke
		if strings.HasSuffix(r.URL.Path, "/share") && r.Method == http.MethodPost {
			shareHandler.CreateShare(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/share/") && r.Method == http.MethodDelete {
			shareHandler.RevokeShare(w, r)
			return
		}
		// interactions create/delete
		if strings.HasSuffix(r.URL.Path, "/interactions") && r.Method == http.MethodPost {
			s.discoveryHandler.CreateProjectInteraction(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/interactions/") && r.Method == http.MethodPatch {
			s.discoveryHandler.UpdateProjectInteraction(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/interactions/") && r.Method == http.MethodDelete {
			s.discoveryHandler.DeleteProjectInteraction(w, r)
			return
		}
		// Agent routes: /projects/{id}/agents/{agentId}/execute
		if strings.Contains(r.URL.Path, "/agents/") && strings.HasSuffix(r.URL.Path, "/execute") && r.Method == http.MethodPost {
			if agentsHandler == nil {
				httphandlers.WriteInternalError(w, "Agents not configured")
				return
			}
			agentsHandler.ExecuteAgent(w, r)
			return
		}
		// Artifacts CRUD: /projects/{id}/artifacts
		if strings.HasSuffix(r.URL.Path, "/artifacts/from-template") && r.Method == http.MethodPost {
			ah := httphandlers.NewArtifactsHandler(coreDocsRepo).
				WithStorage(gcsClient, bucket).
				WithTemplateService(artifactTemplateService).
				WithOrchestrator(artifactOrchestratorService)
			ah.CreateArtifactFromTemplate(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			ah := httphandlers.NewArtifactsHandler(coreDocsRepo).
				WithStorage(gcsClient, bucket).
				WithTemplateService(artifactTemplateService).
				WithOrchestrator(artifactOrchestratorService)
			switch r.Method {
			case http.MethodGet:
				ah.ListArtifacts(w, r)
				return
			case http.MethodPost:
				ah.CreateArtifact(w, r)
				return
			}
		}
		if strings.Contains(r.URL.Path, "/artifacts/") {
			ah := httphandlers.NewArtifactsHandler(coreDocsRepo).
				WithStorage(gcsClient, bucket).
				WithTemplateService(artifactTemplateService).
				WithOrchestrator(artifactOrchestratorService)
			if r.Method == http.MethodGet {
				ah.GetArtifact(w, r)
				return
			}
			if r.Method == http.MethodPatch {
				ah.UpdateArtifact(w, r)
				return
			}
			if r.Method == http.MethodDelete {
				ah.DeleteArtifact(w, r)
				return
			}
		}
		// Listening Signals routes: /projects/{id}/listening-signals
		// Note: handlers are passed via closure from main()
		if strings.Contains(r.URL.Path, "/listening-signals") && listeningSignalsHandler != nil {
			path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			// Check for /projects/{id}/listening-signals/{signalId}/activate or /deactivate
			if len(path) >= 6 && path[4] == "listening-signals" {
				signalID := path[5]
				action := ""
				if len(path) >= 7 {
					action = path[6]
				}
				if action == "activate" && r.Method == http.MethodPost {
					// Extract signal ID from path and call handler
					// Handler will parse path itself
					listeningSignalsHandler.ActivateSignal(w, r)
					return
				}
				if action == "deactivate" && r.Method == http.MethodPost {
					listeningSignalsHandler.DeactivateSignal(w, r)
					return
				}
				// Get/Update/Delete signal by ID: /projects/{id}/listening-signals/{signalId}
				if signalID != "" && len(path) == 6 {
					switch r.Method {
					case http.MethodGet:
						listeningSignalsHandler.GetSignal(w, r)
					case http.MethodPatch, http.MethodPut:
						listeningSignalsHandler.UpdateSignal(w, r)
					case http.MethodDelete:
						listeningSignalsHandler.DeleteSignal(w, r)
					default:
						httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
					}
					return
				}
			}
			// List/Create signals: /projects/{id}/listening-signals
			if strings.HasSuffix(r.URL.Path, "/listening-signals") {
				switch r.Method {
				case http.MethodPost:
					listeningSignalsHandler.CreateSignal(w, r)
				case http.MethodGet:
					listeningSignalsHandler.ListSignals(w, r)
				default:
					httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
				}
				return
			}
		}
		// Signal History route: /projects/{id}/signal-history
		if strings.HasSuffix(r.URL.Path, "/signal-history") && r.Method == http.MethodGet && signalHistoryHandler != nil {
			signalHistoryHandler.ListByTenant(w, r)
			return
		}
		// Agent Jobs routes (new orchestrator-based system)
		if agentJobsHandler != nil {
			path := r.URL.Path
			if strings.HasSuffix(path, "/agent-jobs") {
				if r.Method == http.MethodPost {
					agentJobsHandler.CreateAgentJob(w, r)
					return
				}
				if r.Method == http.MethodGet {
					agentJobsHandler.ListAgentJobs(w, r)
					return
				}
			}
			if strings.Contains(path, "/agent-jobs/") {
				if strings.HasSuffix(path, "/execute") && r.Method == http.MethodPost {
					agentJobsHandler.ExecuteAgentJob(w, r)
					return
				}
				if strings.HasSuffix(path, "/pause") && r.Method == http.MethodPost {
					agentJobsHandler.PauseAgentJob(w, r)
					return
				}
				if strings.HasSuffix(path, "/resume") && r.Method == http.MethodPost {
					agentJobsHandler.ResumeAgentJob(w, r)
					return
				}
				if strings.HasSuffix(path, "/cancel") && r.Method == http.MethodPost {
					agentJobsHandler.CancelAgentJob(w, r)
					return
				}
				if strings.HasSuffix(path, "/steps") && r.Method == http.MethodPut {
					agentJobsHandler.UpdateAgentJobSteps(w, r)
					return
				}
				// Delete agent job: DELETE /api/v1/projects/{projectId}/agent-jobs/{jobId}
				if r.Method == http.MethodDelete {
					agentJobsHandler.DeleteAgentJob(w, r)
					return
				}
				// Update agent job: PATCH /api/v1/projects/{projectId}/agent-jobs/{jobId}
				if r.Method == http.MethodPatch {
					agentJobsHandler.UpdateAgentJob(w, r)
					return
				}
				// Get agent job: GET /api/v1/projects/{projectId}/agent-jobs/{jobId}
				if r.Method == http.MethodGet {
					agentJobsHandler.GetAgentJob(w, r)
					return
				}
			}
		}

		// fallback to default project by id handler
		// Pass signal handlers via closure
		s.routeProjectsByID(w, r, listeningSignalsHandler, signalHistoryHandler)
	})))

	// Rules routes
	mux.Handle("/api/v1/rules", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			rulesHandler.CreateRule(w, r)
		case http.MethodGet:
			rulesHandler.ListRules(w, r)
		default:
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		}
	})))
	mux.Handle("/api/v1/rules/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			rulesHandler.UpdateRule(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			rulesHandler.DeleteRule(w, r)
			return
		}
		httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
	})))

	// Listening Signals routes (tenant-wide and by ID)
	if listeningSignalsHandler != nil {
		// List/Create tenant-wide signals: /api/v1/listening-signals
		mux.Handle("/api/v1/listening-signals", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				listeningSignalsHandler.CreateSignal(w, r)
			case http.MethodGet:
				listeningSignalsHandler.ListSignals(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
		})))
		// Get/Update/Delete/Activate/Deactivate signal: /api/v1/listening-signals/:id
		mux.Handle("/api/v1/listening-signals/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(path) >= 4 {
				action := path[3]
				switch action {
				case "activate":
					if r.Method == http.MethodPost {
						listeningSignalsHandler.ActivateSignal(w, r)
						return
					}
				case "deactivate":
					if r.Method == http.MethodPost {
						listeningSignalsHandler.DeactivateSignal(w, r)
						return
					}
				}
			}
			// Default: Get/Update/Delete by ID
			switch r.Method {
			case http.MethodGet:
				listeningSignalsHandler.GetSignal(w, r)
			case http.MethodPatch, http.MethodPut:
				listeningSignalsHandler.UpdateSignal(w, r)
			case http.MethodDelete:
				listeningSignalsHandler.DeleteSignal(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
		})))
	}

	// Webhooks
	mux.Handle("/api/v1/webhooks/", http.HandlerFunc(webhooksHandler.Handle))

	// BigQuery AI query endpoint
	if bigqueryHandler != nil {
		mux.Handle("/api/v1/bigquery/query", authMiddleware(http.HandlerFunc(bigqueryHandler.Query)))
		logger.Printf("BigQuery query endpoint registered: /api/v1/bigquery/query")
	}

	// Subscription billing endpoint (for Cloud Scheduler)
	// Accepts x-functions-key header or Cloud Scheduler requests (User-Agent: Google-Cloud-Scheduler)
	mux.Handle("/api/v1/subscriptions/billing", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
			return
		}
		// Check for system call with function key or Cloud Scheduler
		hasFunctionKey := r.Header.Get("x-functions-key") != ""
		isCloudScheduler := r.Header.Get("User-Agent") == "Google-Cloud-Scheduler"
		if !hasFunctionKey && !isCloudScheduler {
			httphandlers.WriteUnauthorized(w, "System access required")
			return
		}
		if err := subscriptionService.ProcessBillingCycle(r.Context()); err != nil {
			logger.Printf("Subscription billing cycle failed: %v", err)
			httphandlers.WriteInternalError(w, "Failed to process billing cycle")
			return
		}
		httphandlers.WriteJSON(w, http.StatusOK, map[string]string{"message": "Billing cycle processed successfully"})
	}))

	// Artifact scheduler endpoint (for Cloud Scheduler)
	// Accepts x-functions-key header or Cloud Scheduler requests (User-Agent: Google-Cloud-Scheduler)
	// Optional query params: tenantId, projectId (if not provided, processes all)
	mux.Handle("/api/v1/artifacts/scheduled-update", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
			return
		}
		// Check for system call with function key or Cloud Scheduler
		hasFunctionKey := r.Header.Get("x-functions-key") != ""
		isCloudScheduler := r.Header.Get("User-Agent") == "Google-Cloud-Scheduler"
		if !hasFunctionKey && !isCloudScheduler {
			httphandlers.WriteUnauthorized(w, "System access required")
			return
		}
		if artifactSchedulerService == nil {
			httphandlers.WriteInternalError(w, "Artifact scheduler service not initialized")
			return
		}
		tenantID := r.URL.Query().Get("tenantId")
		projectID := r.URL.Query().Get("projectId")
		if tenantID != "" && projectID != "" {
			// Process specific project
			if err := artifactSchedulerService.ProcessProjectArtifacts(r.Context(), tenantID, projectID); err != nil {
				logger.Printf("Artifact scheduled update failed for project %s: %v", projectID, err)
				httphandlers.WriteInternalError(w, "Failed to process scheduled updates")
				return
			}
			httphandlers.WriteJSON(w, http.StatusOK, map[string]string{"message": "Scheduled updates processed successfully", "projectId": projectID})
		} else {
			// Process all projects (if possible)
			// Note: This requires listing all projects, which might not be available
			// In production, Cloud Scheduler should call this endpoint per-project
			if err := artifactSchedulerService.ProcessAllProjects(r.Context()); err != nil {
				logger.Printf("Artifact scheduled update failed: %v", err)
				httphandlers.WriteInternalError(w, "Failed to process scheduled updates")
				return
			}
			httphandlers.WriteJSON(w, http.StatusOK, map[string]string{"message": "Scheduled updates processed successfully"})
		}
	}))

	// Signal monitoring endpoint (for Cloud Scheduler or manual trigger)
	// Accepts x-functions-key header or Cloud Scheduler requests (User-Agent: Google-Cloud-Scheduler)
	// Optional query params: tenantId, projectId (if not provided, processes all)
	mux.Handle("/api/v1/signals/process", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
			return
		}
		// Check for system call with function key or Cloud Scheduler
		hasFunctionKey := r.Header.Get("x-functions-key") != ""
		isCloudScheduler := r.Header.Get("User-Agent") == "Google-Cloud-Scheduler"
		if !hasFunctionKey && !isCloudScheduler {
			httphandlers.WriteUnauthorized(w, "System access required")
			return
		}
		if signalMonitoringService == nil {
			httphandlers.WriteInternalError(w, "Signal monitoring service not initialized")
			return
		}
		tenantID := r.URL.Query().Get("tenantId")
		projectIDParam := r.URL.Query().Get("projectId")

		if tenantID != "" {
			// Process signals for specific tenant (optionally filtered by project)
			var projectID *string
			if projectIDParam != "" {
				projectID = &projectIDParam
			}
			if err := signalMonitoringService.ProcessAllActiveSignals(r.Context(), tenantID, projectID); err != nil {
				logger.Printf("Signal processing failed for tenant %s: %v", tenantID, err)
				httphandlers.WriteInternalError(w, "Failed to process signals")
				return
			}
			httphandlers.WriteJSON(w, http.StatusOK, map[string]string{
				"message":  "Signals processed successfully",
				"tenantId": tenantID,
			})
		} else {
			// Process all signals across all tenants
			signals, err := signalMonitoringService.ListAllActiveSignals(r.Context())
			if err != nil {
				logger.Printf("Failed to list signals: %v", err)
				httphandlers.WriteInternalError(w, "Failed to list signals")
				return
			}

			successCount := 0
			errorCount := 0
			for _, signal := range signals {
				if err := signalMonitoringService.ProcessSignal(r.Context(), signal); err != nil {
					logger.Printf("Failed to process signal (signalID=%s): %v", signal.ID, err)
					errorCount++
					continue
				}
				successCount++
			}

			httphandlers.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"message":   "Signal processing completed",
				"processed": successCount,
				"errors":    errorCount,
				"total":     len(signals),
			})
		}
	}))

	// Public share endpoints (unauthenticated)
	mux.Handle("/api/v1/public/share/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		// Route: /api/v1/public/share/{token}/docs/{docId}/download
		if len(parts) >= 8 && parts[5] == "docs" && parts[7] == "download" && r.Method == http.MethodGet {
			shareHandler.DownloadPublicShareDoc(w, r)
			return
		}
		// Route: /api/v1/public/share/{token}/docs/{docId}/export
		if len(parts) >= 8 && parts[5] == "docs" && parts[7] == "export" && r.Method == http.MethodPost {
			shareHandler.ExportPublicShareDocToDocx(w, r)
			return
		}
		// Route: /api/v1/public/share/{token}/docs/{docId}
		if len(parts) >= 7 && parts[5] == "docs" && r.Method == http.MethodGet {
			shareHandler.GetPublicShareDoc(w, r)
			return
		}
		// Route: /api/v1/public/share/{token}/docs
		if len(parts) >= 6 && parts[5] == "docs" && r.Method == http.MethodGet {
			shareHandler.ListPublicShareDocs(w, r)
			return
		}
		// Route: /api/v1/public/share/{token}
		shareHandler.GetPublicShare(w, r)
	}))

	// Verified URL Analysis endpoints
	if verifiedHandler != nil {
		// POST /api/v1/verified - requires system auth (x-functions-key)
		mux.Handle("/api/v1/verified", authMiddleware(http.HandlerFunc(verifiedHandler.CreateVerified)))
		// GET /api/v1/public/verified/{code} - public endpoint
		mux.Handle("/api/v1/public/verified/", http.HandlerFunc(verifiedHandler.GetPublicVerified))
		// GET /api/v1/public/documents/{id} - public endpoint for system documents
		mux.Handle("/api/v1/public/documents/", http.HandlerFunc(verifiedHandler.GetPublicDocument))
	}

	// Admin: source coordination (handled below with jobAuth middleware for Cloud Scheduler)

	// Discovery routes
	mux.Handle("/api/v1/sources-discovery/execute", authMiddleware(http.HandlerFunc(s.discoveryHandler.ExecuteDiscovery)))

	// Horizon routes
	if horizonHandlers != nil {
		horizonHandlers.RegisterRoutes(mux, authMiddleware)
		logger.Printf("Horizon routes registered")
	}
	if enforcementHandlers != nil {
		enforcementHandlers.RegisterRoutes(mux, authMiddleware)
		logger.Printf("Horizon policy enforcement routes registered")
	}

	// Agents routes (legacy)
	mux.Handle("/api/v1/agents", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if agentsHandler == nil {
			httphandlers.WriteInternalError(w, "Agents not configured")
			return
		}
		agentsHandler.ListAgents(w, r)
	})))

	// Usage metrics handler
	usageHandler := httphandlers.NewUsageHandler(repo.NewUsageMetricsRepository(fsClient.Client()), logger)

	// GenAI routes (isolated Gemini client)
	mux.Handle("/api/v1/genai/complete", authMiddleware(http.HandlerFunc(genaiHandler.Complete)))
	mux.Handle("/api/v1/genai/complete-json", authMiddleware(http.HandlerFunc(genaiHandler.CompleteJSON)))
	mux.Handle("/api/v1/genai/complete-json-strict", authMiddleware(http.HandlerFunc(genaiHandler.CompleteJSONStrict)))
	mux.Handle("/api/v1/genai/generate", authMiddleware(http.HandlerFunc(genaiHandler.GenAIGenerate)))
	mux.Handle("/api/v1/genai/complete-with-tools", authMiddleware(http.HandlerFunc(genaiHandler.CompleteWithTools)))
	mux.Handle("/api/v1/genai/stream", authMiddleware(http.HandlerFunc(genaiHandler.Stream)))

	// Usage routes
	mux.Handle("/api/v1/usage", authMiddleware(http.HandlerFunc(usageHandler.GetUsage)))
	mux.Handle("/api/v1/usage/events", authMiddleware(http.HandlerFunc(usageHandler.GetEvents)))
	mux.Handle("/api/v1/usage/avg-timing", authMiddleware(http.HandlerFunc(usageHandler.GetAverageTiming)))
	// Universal structured AI endpoint
	if aiExecHandler != nil {
		mux.Handle("/api/v1/ai/structured", authMiddleware(http.HandlerFunc(aiExecHandler.Structured)))
	}
	// Temporary FE testing endpoint
	mux.Handle("/api/v1/genai/adhoc-json", authMiddleware(http.HandlerFunc(genaiHandler.AdhocJSON)))
	// Note: /api/v1/projects/{id}/datasources is handled by projectsByID pattern

	// Artifact Templates routes
	mux.Handle("/api/v1/artifact-templates", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			artifactTemplatesHandler.ListTemplates(w, r)
		case http.MethodPost:
			artifactTemplatesHandler.CreateTemplate(w, r)
		default:
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		}
	})))
	mux.Handle("/api/v1/artifact-templates/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		if strings.HasSuffix(path, "/generate-structure") && r.Method == http.MethodPost {
			artifactTemplatesHandler.GenerateStructure(w, r)
			return
		}
		if strings.HasSuffix(path, "/from-artifact") && r.Method == http.MethodPost {
			artifactTemplatesHandler.CreateFromArtifact(w, r)
			return
		}
		if strings.HasSuffix(path, "/publish") && r.Method == http.MethodPost {
			artifactTemplatesHandler.PublishTemplate(w, r)
			return
		}
		// /api/v1/artifact-templates/{id}
		switch r.Method {
		case http.MethodGet:
			artifactTemplatesHandler.GetTemplate(w, r)
		case http.MethodPut:
			artifactTemplatesHandler.UpdateTemplate(w, r)
		case http.MethodDelete:
			artifactTemplatesHandler.DeleteTemplate(w, r)
		default:
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		}
	})))

	// Artifact Generation routes
	mux.Handle("/api/v1/artifacts/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		// parts: [api v1 artifacts {id} ...]
		// /api/v1/artifacts/{id}/generate
		if len(parts) >= 5 && parts[4] == "generate" && r.Method == http.MethodPost {
			artifactGenerationHandler.StartGeneration(w, r)
			return
		}
		// /api/v1/artifacts/{id}/sections/{sectionId}/generate
		if len(parts) >= 7 && parts[4] == "sections" && parts[6] == "generate" && r.Method == http.MethodPost {
			artifactGenerationHandler.GenerateSection(w, r)
			return
		}
		// /api/v1/artifacts/{id}/sections/{sectionId}/regenerate
		if len(parts) >= 7 && parts[4] == "sections" && parts[6] == "regenerate" && r.Method == http.MethodPost {
			artifactGenerationHandler.RegenerateSection(w, r)
			return
		}
		// /api/v1/artifacts/{id}/assemble
		if len(parts) >= 5 && parts[4] == "assemble" && r.Method == http.MethodPost {
			artifactGenerationHandler.AssembleDocument(w, r)
			return
		}
		// /api/v1/artifacts/{id}/versions
		if len(parts) >= 5 && parts[4] == "versions" && r.Method == http.MethodPost {
			artifactGenerationHandler.SaveVersion(w, r)
			return
		}
		// /api/v1/artifacts/{id}/versions/{versionId}/restore
		if len(parts) >= 7 && parts[4] == "versions" && parts[6] == "restore" && r.Method == http.MethodPost {
			artifactGenerationHandler.RestoreVersion(w, r)
			return
		}
		// /api/v1/artifacts/{artifactId}/signals
		if len(parts) >= 4 && parts[3] == "signals" && documentArtifactSignalsHandler != nil {
			switch r.Method {
			case http.MethodPost:
				documentArtifactSignalsHandler.CreateLink(w, r)
			case http.MethodGet:
				documentArtifactSignalsHandler.ListLinksByArtifact(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
			return
		}
		// /api/v1/artifacts/{artifactId}/signals/{linkId}
		if len(parts) >= 5 && parts[3] == "signals" && documentArtifactSignalsHandler != nil {
			switch r.Method {
			case http.MethodPatch, http.MethodPut:
				documentArtifactSignalsHandler.UpdateLink(w, r)
			case http.MethodDelete:
				documentArtifactSignalsHandler.DeleteLink(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
			return
		}
		// /api/v1/artifacts/{artifactId}/signal-history
		if len(parts) >= 4 && parts[3] == "signal-history" && r.Method == http.MethodGet && signalHistoryHandler != nil {
			signalHistoryHandler.ListByArtifact(w, r)
			return
		}

		httphandlers.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Route not found", nil)
	})))

	// Sources management routes
	// Source preview endpoints
	mux.Handle("/api/v1/sources/preview/telegram", authMiddleware(http.HandlerFunc(s.discoveryHandler.PreviewTelegramChannel)))
	mux.Handle("/api/v1/sources/preview/rss", authMiddleware(http.HandlerFunc(s.discoveryHandler.PreviewRSSFeed)))

	mux.Handle("/api/v1/sources", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			sourcesHandler.CreateSource(w, r)
		case http.MethodGet:
			sourcesHandler.ListSources(w, r)
		default:
			httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		}
	})))
	mux.Handle("/api/v1/sources/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		// Handle /api/v1/sources/{id}/artifacts
		if len(parts) >= 5 && parts[4] == "artifacts" && r.Method == http.MethodGet {
			documentsHandler.GetSourceArtifacts(w, r)
			return
		}
		// Handle /api/v1/sources/{id}/ingest
		// parts: ["api", "v1", "sources", "{id}", "ingest"]
		if len(parts) >= 5 && parts[4] == "ingest" && r.Method == http.MethodPost {
			sourcesHandler.IngestSource(w, r)
			return
		}
		// Handle /api/v1/sources/{id}/reprocess
		// parts: ["api", "v1", "sources", "{id}", "reprocess"]
		if len(parts) >= 5 && parts[4] == "reprocess" && r.Method == http.MethodPost {
			sourcesHandler.ReprocessDocuments(w, r)
			return
		}
		// Handle /api/v1/sources/{id}/documents
		// parts: ["api", "v1", "sources", "{id}", "documents"]
		if len(parts) >= 5 && parts[4] == "documents" && r.Method == http.MethodGet {
			sourcesHandler.ListSourceDocuments(w, r)
			return
		}
		// Handle /api/v1/sources/{id}/ingest-history
		// parts: ["api", "v1", "sources", "{id}", "ingest-history"]
		if len(parts) >= 5 && parts[4] == "ingest-history" && r.Method == http.MethodGet {
			sourcesHandler.GetSourceIngestHistory(w, r)
			return
		}
		// Handle /api/v1/sources/{id}/status
		// parts: ["api", "v1", "sources", "{id}", "status"]
		if len(parts) >= 5 && parts[4] == "status" && r.Method == http.MethodPatch {
			sourcesHandler.UpdateSourceStatus(w, r)
			return
		}
		// Handle /api/v1/sources/{id}
		// parts: ["api", "v1", "sources", "{id}"]
		if len(parts) >= 4 {
			switch r.Method {
			case http.MethodGet:
				sourcesHandler.GetSource(w, r)
			case http.MethodPut:
				sourcesHandler.UpdateSource(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
			return
		}
		httphandlers.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Route not found", nil)
	})))

	// Generation Sessions routes
	mux.Handle("/api/v1/generation-sessions/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")

		// /api/v1/generation-sessions/{sessionId}
		if len(parts) >= 3 && r.Method == http.MethodGet {
			artifactGenerationHandler.GetSession(w, r)
			return
		}

		// /api/v1/generation-sessions/{sessionId}/cancel
		if len(parts) >= 4 && parts[3] == "cancel" && r.Method == http.MethodPost {
			artifactGenerationHandler.CancelGeneration(w, r)
			return
		}

		httphandlers.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Route not found", nil)
	})))

	// Legacy routes (to be migrated)
	// mux.Handle("/v1/whoami", authMiddleware(http.HandlerFunc(s.whoAmI)))
	// mux.Handle("/v1/users/me", authMiddleware(http.HandlerFunc(s.upsertMe)))
	// mux.Handle("/v1/projects", authMiddleware(http.HandlerFunc(s.createProject)))
	// mux.Handle("/v1/datasources", authMiddleware(http.HandlerFunc(s.createDataSource)))

	// CORS: support multiple origins via FRONTEND_ORIGINS (comma-separated),
	// fallback to single FRONTEND_ORIGIN for backward compatibility
	allowedOriginsEnv := getEnv("FRONTEND_ORIGINS", "")
	var allowedOrigins []string
	if allowedOriginsEnv != "" {
		splitFn := func(r rune) bool { return r == ',' || r == '|' }
		for _, o := range strings.FieldsFunc(allowedOriginsEnv, splitFn) {
			o = strings.TrimSpace(o)
			if o != "" {
				allowedOrigins = append(allowedOrigins, o)
			}
		}
		log.Printf("CORS: parsed %d allowed origins from FRONTEND_ORIGINS: %v", len(allowedOrigins), allowedOrigins)
	} else {
		legacy := getEnv("FRONTEND_ORIGIN", "")
		if legacy != "" {
			allowedOrigins = []string{legacy}
			log.Printf("CORS: using FRONTEND_ORIGIN: %s", legacy)
		} else {
			allowedOrigins = []string{
				"http://localhost:5173",
				"http://localhost:3000",
				"https://nordicvision.io",
				"https://www.nordicvision.io",
				"https://*.nordicvision.io",
				"https://horizon.nordicvision.io",
				"https://www.horizon.nordicvision.io",
				"https://*.horizon.nordicvision.io",
			}
			log.Printf("CORS: using default allowed origins: %v", allowedOrigins)
		}
	}

	// Admin job token middleware for Cloud Scheduler (bypasses Firebase auth for admin route only)
	adminToken := getEnv("ADMIN_JOB_TOKEN", "")
	jobAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Primary: validate explicit job token header when configured
			if adminToken != "" && r.Header.Get("X-Job-Token") == adminToken {
				next.ServeHTTP(w, r)
				return
			}
			// Fallback: if no token configured, allow requests coming from Cloud Scheduler
			// This helps in dev environments where ADMIN_JOB_TOKEN may be missing
			if adminToken == "" {
				ua := r.Header.Get("User-Agent")
				if strings.Contains(ua, "Google-Cloud-Scheduler") {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "admin job token not configured (and request not recognized as Cloud Scheduler)", http.StatusForbidden)
				return
			}
			// Token configured but missing/mismatched
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}

	// Attach admin route without Firebase auth, but with jobAuth
	mux.Handle("/api/v1/admin/sources/coordinate", jobAuth(http.HandlerFunc(s.discoveryHandler.CoordinateSources)))

	// Horizon Watchdog cron endpoint (Cloud Scheduler)
	// POST /api/v1/admin/nv_horizon/watchdog/run?limit=200
	// Secured via the same jobAuth (X-Job-Token header) to avoid exposing cron publicly.
	watchdogCronHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if horizonService == nil || horizonService.Watchdog() == nil {
			// Watchdog is intentionally feature-flagged and may be disabled in this revision.
			// Return 200 to avoid noisy Cloud Scheduler ERROR logs when the cron remains enabled.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"skipped": true,
				"reason":  "watchdog_disabled_or_not_initialized",
			})
			return
		}

		// Parse limit
		limit := 200
		if v := r.URL.Query().Get("limit"); strings.TrimSpace(v) != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				limit = n
			}
		}

		// Distributed lock in Firestore to avoid overlapping runs (scheduler retries / multi-instance).
		// Collection is intentionally separate from domain data.
		lockTTL := 9 * time.Minute
		lockTTLEnv := strings.TrimSpace(os.Getenv("NV_HORIZON_WATCHDOG_CRON_LOCK_TTL"))
		if lockTTLEnv == "" {
			lockTTLEnv = strings.TrimSpace(os.Getenv("HORIZON_WATCHDOG_CRON_LOCK_TTL"))
		}
		if lockTTLEnv == "" {
			lockTTLEnv = strings.TrimSpace(os.Getenv("ETIGRAM_WATCHDOG_CRON_LOCK_TTL"))
		}
		if strings.TrimSpace(lockTTLEnv) != "" {
			if d, err := time.ParseDuration(strings.TrimSpace(lockTTLEnv)); err == nil && d >= 30*time.Second {
				lockTTL = d
			}
		}
		lockOwner := fmt.Sprintf("watchdog-cron-%d", time.Now().UnixNano())
		lockRef := fsClient.Client().Collection("horizon_runtime_locks").Doc("watchdog_cron")
		now := time.Now().UTC()

		type lockDoc struct {
			Owner     string    `firestore:"owner"`
			ExpiresAt time.Time `firestore:"expiresAt"`
			UpdatedAt time.Time `firestore:"updatedAt"`
		}

		acquired := false
		_ = fsClient.Client().RunTransaction(r.Context(), func(ctx context.Context, tx *firestore.Transaction) error {
			snap, err := tx.Get(lockRef)
			if err == nil && snap.Exists() {
				var ld lockDoc
				if err := snap.DataTo(&ld); err == nil {
					if !ld.ExpiresAt.IsZero() && ld.ExpiresAt.After(now) {
						// Already locked
						return nil
					}
				}
			}
			ld := lockDoc{
				Owner:     lockOwner,
				ExpiresAt: now.Add(lockTTL),
				UpdatedAt: now,
			}
			if err := tx.Set(lockRef, ld); err != nil {
				return err
			}
			acquired = true
			return nil
		})

		if !acquired {
			// Return 200 to prevent Cloud Scheduler retries piling up.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"skipped": true,
				"reason":  "lock_not_acquired",
			})
			return
		}

		// Best-effort release: shorten the lease at the end if we still own it.
		defer func() {
			_ = fsClient.Client().RunTransaction(context.Background(), func(ctx context.Context, tx *firestore.Transaction) error {
				snap, err := tx.Get(lockRef)
				if err != nil || !snap.Exists() {
					return nil
				}
				var ld lockDoc
				if err := snap.DataTo(&ld); err != nil {
					return nil
				}
				if ld.Owner != lockOwner {
					return nil
				}
				ld.ExpiresAt = time.Now().UTC()
				ld.UpdatedAt = time.Now().UTC()
				return tx.Set(lockRef, ld)
			})
		}()

		// Run watchdog scans with timeout
		timeout := 8 * time.Minute
		ctxRun, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		stats, err := horizonService.Watchdog().ProcessScheduledScansWithStats(ctxRun, limit)
		if err != nil {
			// Firestore composite index may be creating/building right after deploy.
			// In that case, don't fail Cloud Scheduler with 5xx (it will just retry and spam logs).
			msg := err.Error()
			if strings.Contains(msg, "The query requires an index") {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":      true,
					"skipped": true,
					"reason":  "firestore_index_building",
					"error":   msg,
				})
				return
			}
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    true,
			"stats": stats,
		})
	}
	mux.Handle("/api/v1/admin/nv_horizon/watchdog/run", jobAuth(http.HandlerFunc(watchdogCronHandler)))
	mux.Handle("/api/v1/admin/horizon/watchdog/run", jobAuth(http.HandlerFunc(watchdogCronHandler))) // legacy alias

	// CriticalChain routes (demo endpoints public, admin pipeline jobAuth)
	criticalchainSvc := criticalchain.NewService(fsClient.Client())
	criticalchainRunner := criticalchainpipeline.NewRunner(fsClient.Client())
	criticalchainHandlers := criticalchainhttp.NewHandlers(criticalchainSvc, criticalchainRunner)
	criticalchainHandlers.RegisterRoutes(mux, authMiddleware, jobAuth)
	logger.Printf("CriticalChain routes registered")

	handler := withCORS(allowedOrigins)(mux)

	srv := &http.Server{
		Addr:              ":" + addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server listening on :%s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// routeUsersByID routes /api/v1/users/{id} to appropriate handler based on method
func (s *server) routeUsersByID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.usersHandler.GetUser(w, r)
	case http.MethodPut:
		s.usersHandler.UpdateUser(w, r)
	default:
		httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
	}
}

// routeProjects routes /api/v1/projects to appropriate handler based on method
func (s *server) routeProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.projectsHandler.CreateProject(w, r)
	case http.MethodGet:
		s.projectsHandler.ListProjects(w, r)
	default:
		httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
	}
}

// routeProjectsByID routes /api/v1/projects/{id} to appropriate handler based on method
func (s *server) routeProjectsByID(w http.ResponseWriter, r *http.Request, _ *httphandlers.ListeningSignalsHandler, _ *httphandlers.SignalHistoryHandler) {
	// Check if path is /projects/{id}/datasources
	if strings.HasSuffix(r.URL.Path, "/datasources") {
		s.discoveryHandler.ListProjectSources(w, r)
		return
	}
	// Check if path is /projects/{id}/interactions
	if strings.HasSuffix(r.URL.Path, "/interactions") {
		s.discoveryHandler.ListProjectInteractions(w, r)
		return
	}
	// Check if path is /projects/{id}/chat
	if strings.HasSuffix(r.URL.Path, "/chat") {
		if r.Method == http.MethodPost {
			s.discoveryHandler.ChatMessage(w, r)
			return
		}
		httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	// Check if path is /projects/{id}/scenarios
	if strings.Contains(r.URL.Path, "/scenarios") {
		pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// /api/v1/projects/{id}/scenarios or /api/v1/projects/{id}/scenarios/{scenarioId}
		if len(pathParts) == 5 && pathParts[4] == "scenarios" {
			// List or create scenarios
			switch r.Method {
			case http.MethodGet:
				s.projectsHandler.ListProjectScenarios(w, r)
			case http.MethodPost:
				s.projectsHandler.CreateProjectScenario(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
			return
		} else if len(pathParts) == 6 && pathParts[4] == "scenarios" {
			// Get, update, or delete specific scenario
			switch r.Method {
			case http.MethodGet:
				s.projectsHandler.GetProjectScenario(w, r)
			case http.MethodPut:
				s.projectsHandler.UpdateProjectScenario(w, r)
			case http.MethodDelete:
				s.projectsHandler.DeleteProjectScenario(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
			return
		} else if len(pathParts) == 7 && pathParts[4] == "scenarios" && pathParts[6] == "data" {
			// List or create scenario data: /api/v1/projects/{id}/scenarios/{scenarioId}/data
			switch r.Method {
			case http.MethodGet:
				s.projectsHandler.ListScenarioData(w, r)
			case http.MethodPost:
				s.projectsHandler.CreateScenarioData(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
			return
		} else if len(pathParts) == 8 && pathParts[4] == "scenarios" && pathParts[6] == "data" {
			// Delete scenario data: /api/v1/projects/{id}/scenarios/{scenarioId}/data/{dataId}
			switch r.Method {
			case http.MethodDelete:
				s.projectsHandler.DeleteScenarioData(w, r)
			default:
				httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			}
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		s.projectsHandler.GetProject(w, r)
	case http.MethodPut:
		s.projectsHandler.UpdateProject(w, r)
	case http.MethodDelete:
		s.projectsHandler.DeleteProject(w, r)
	default:
		httphandlers.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
	}
}

// withCORS returns middleware that adds CORS headers and handles OPTIONS preflight.
func withCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			// If no Origin header (non-CORS request), proceed without CORS headers except defaults
			allowOrigin := ""
			if origin != "" {
				// Exact match against allowed origins list
				for _, ao := range allowedOrigins {
					if origin == ao {
						allowOrigin = origin
						break
					}
				}
				// Global wildcard support
				if allowOrigin == "" {
					for _, ao := range allowedOrigins {
						if ao == "*" {
							allowOrigin = origin
							break
						}
					}
				}
				// Subdomain wildcard entries like "*.horizon.nordicvision.io" or URL with scheme
				if allowOrigin == "" {
					u, err := url.Parse(origin)
					if err == nil {
						host := u.Hostname()
						scheme := u.Scheme
						for _, ao := range allowedOrigins {
							// Handle wildcard patterns with scheme like "https://*.horizon.nordicvision.io"
							if strings.HasPrefix(ao, "http://") || strings.HasPrefix(ao, "https://") {
								// Extract hostname from pattern, handling wildcards
								aoScheme := ""
								aoHost := ""
								if strings.HasPrefix(ao, "https://") {
									aoScheme = "https"
									aoHost = strings.TrimPrefix(ao, "https://")
								} else if strings.HasPrefix(ao, "http://") {
									aoScheme = "http"
									aoHost = strings.TrimPrefix(ao, "http://")
								}

								if aoHost != "" && aoScheme == scheme {
									if strings.HasPrefix(aoHost, "*.") {
										// Wildcard pattern: "*.horizon.nordicvision.io" matches "horizon.nordicvision.io", "www.horizon.nordicvision.io", etc.
										suffix := strings.TrimPrefix(aoHost, "*")
										// Match exact domain (horizon.nordicvision.io) or subdomain (www.horizon.nordicvision.io)
										if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
											allowOrigin = origin
											break
										}
									}
								}
							} else if strings.HasPrefix(ao, "*.") {
								// Wildcard pattern without scheme: "*.horizon.nordicvision.io"
								suffix := strings.TrimPrefix(ao, "*")
								// Match exact domain (horizon.nordicvision.io) or subdomain (www.horizon.nordicvision.io)
								if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
									allowOrigin = origin
									break
								}
							}
						}
					}
				}
				// Log only after all matching attempts (exact + wildcard + subdomain patterns),
				// otherwise we can log false negatives.
				if allowOrigin == "" && r.Method == http.MethodOptions {
					log.Printf("CORS: origin '%s' not matched against allowed origins: %v", origin, allowedOrigins)
				}
			}

			if allowOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
				w.Header().Add("Vary", "Origin")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if acrh := r.Header.Get("Access-Control-Request-Headers"); acrh != "" {
				w.Header().Set("Access-Control-Allow-Headers", acrh)
			} else {
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, X-Job-Token")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
			w.Header().Set("Access-Control-Max-Age", "600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func getEnv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func enabledEnv(key string) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false
	}
	return v != "0" && !strings.EqualFold(v, "false")
}

func getEnvInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return def
	}
	return d
}

func parseEnvBool(raw string) bool {
	if v, err := strconv.ParseBool(strings.TrimSpace(raw)); err == nil {
		return v
	}
	return false
}

func watchdogEnabledViaEnv() bool {
	v := strings.TrimSpace(os.Getenv("ENABLE_NV_HORIZON_WATCHDOG"))
	if v == "" {
		v = strings.TrimSpace(os.Getenv("ENABLE_HORIZON_WATCHDOG"))
	}
	if v == "" {
		v = strings.TrimSpace(os.Getenv("ENABLE_ETIGRAM_WATCHDOG"))
	}
	if v == "" {
		v = "0"
	}
	return v == "1"
}

// getEnvWithAlt returns value from key, or altKey if key is empty, or def.
func getEnvWithAlt(key, altKey, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(altKey)); v != "" {
		return v
	}
	return def
}

// isNonRetryableScoutExecutionErrorWithJob checks both the error and job state (reason_code).
func isNonRetryableScoutExecutionErrorWithJob(err error, job *horizonmodels.ScoutingJob) bool {
	if err != nil {
		msg := strings.ToLower(strings.TrimSpace(err.Error()))
		if msg != "" {
			if strings.Contains(msg, "fragment creation blocked:") {
				return true
			}
			if strings.Contains(msg, "governance.owner must be 'system'") {
				return true
			}
			if strings.Contains(msg, "type is required") && (strings.Contains(msg, "fragment") || strings.Contains(msg, "validation error") || strings.Contains(msg, "contract normalization")) {
				return true
			}
			if strings.Contains(msg, "validation error") && (strings.Contains(msg, "type is required") || strings.Contains(msg, "governance.owner") || strings.Contains(msg, "canonicalpath")) {
				return true
			}
			if strings.Contains(msg, "resume detected terminal") || strings.Contains(msg, "failed with non-retryable error") {
				return true
			}
		}
	}
	if job != nil && job.State != nil {
		if rc, ok := job.State["normalizationReasonCode"].(string); ok && strings.TrimSpace(rc) != "" {
			switch strings.TrimSpace(strings.ToLower(rc)) {
			case "type_required", "governance_owner", "canonical_path_required":
				return true
			}
		}
		if !readInterfaceBool(job.State["normalizationRetryable"], true) {
			return true
		}
		if reasons, ok := job.State["terminalOutcomeReasons"].(map[string]interface{}); ok && reasons != nil {
			if rc, ok := reasons["reason_code"].(string); ok {
				switch strings.TrimSpace(strings.ToLower(rc)) {
				case "non_retryable_contract_error", "type_required", "governance_owner", "canonical_path_required":
					return true
				}
			}
		}
		if reasons, ok := job.State["terminalOutcomeReasons"].(map[string]string); ok && reasons != nil {
			if rc, ok := reasons["reason_code"]; ok {
				switch strings.TrimSpace(strings.ToLower(rc)) {
				case "non_retryable_contract_error", "type_required", "governance_owner", "canonical_path_required":
					return true
				}
			}
		}
	}
	return false
}

func shouldSkipScoutTaskExecution(job *horizonmodels.ScoutingJob) (skip bool, finalize bool, reason string) {
	if job == nil {
		return true, false, "missing_job"
	}
	if isTerminalScoutState(job.CurrentState) {
		return true, false, "terminal_state_" + strings.ToLower(strings.TrimSpace(string(job.CurrentState)))
	}
	if isNonRetryableScoutExecutionErrorWithJob(nil, job) {
		return true, true, "non_retryable_job_state"
	}
	if hasNonRetryablePipelineStageFailure(job) {
		return true, true, "pipeline_non_retryable_stage_failure"
	}
	return false, false, ""
}

func isTerminalScoutState(state horizonmodels.ScoutingJobState) bool {
	switch state {
	case horizonmodels.ScoutingJobStateRejected, horizonmodels.ScoutingJobStateFailed, horizonmodels.ScoutingJobStateVerified:
		return true
	default:
		return false
	}
}

func hasNonRetryablePipelineStageFailure(job *horizonmodels.ScoutingJob) bool {
	if job == nil || job.State == nil {
		return false
	}
	runtime, _ := job.State["pipelineRuntime"].(map[string]interface{})
	if runtime == nil {
		return false
	}
	stages, _ := runtime["stages"].(map[string]interface{})
	if stages == nil {
		return false
	}
	for _, raw := range stages {
		entry, _ := raw.(map[string]interface{})
		if entry == nil {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(readInterfaceString(entry["status"])))
		if status != "failed" {
			continue
		}
		if !readInterfaceBool(entry["retryable"], true) {
			return true
		}
	}
	return false
}

func readInterfaceString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return ""
	}
}

func readInterfaceBool(v interface{}, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		if b, err := strconv.ParseBool(strings.TrimSpace(t)); err == nil {
			return b
		}
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	}
	return def
}

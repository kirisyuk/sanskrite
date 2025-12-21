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
	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/domain"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
	"github.com/igorkirisyuk/sanskrite/internal/wikidata"
	"google.golang.org/api/idtoken"

	// Horizon system
	"github.com/igorkirisyuk/sanskrite/internal/horizon"
	horizonlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	horizontasks "github.com/igorkirisyuk/sanskrite/internal/horizon/tasks"
	horizonwatchdog "github.com/igorkirisyuk/sanskrite/internal/horizon/watchdog"
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

func main() {
	addr := getEnv("PORT", "8080")
	ctx := context.Background()

	credsPath := getEnv("GOOGLE_APPLICATION_CREDENTIALS", "")
	var opts []option.ClientOption
	if credsPath != "" {
		opts = append(opts, option.WithCredentialsFile(credsPath))
	}

	projectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))

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

	// Initialize GCS client for uploads
	gcsClient, err := gcp.NewGCSClient(ctx, projectID)
	if err != nil {
		log.Fatalf("gcs init: %v", err)
	}

	logger := log.New(os.Stdout, "[sanskrite] ", log.LstdFlags|log.Lshortfile)

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
	// RAG repos
	ragDocsBucket := getEnv("RAG_DOCUMENTS_BUCKET", "")
	var bqClient *gcp.BigQueryClient // Will be initialized later if BigQuery is enabled
	docsRepo := repo.NewDocumentsRepository(fsClient.Client())
	if ragDocsBucket != "" && gcsClient != nil {
		docsRepo = docsRepo.WithStorage(gcsClient, ragDocsBucket)
		logger.Printf("RAG documents storage enabled: bucket=%s", ragDocsBucket)
	}
	chunksRepo := repo.NewChunksRepository(fsClient.Client())

	// Artifact templates repo/service/handler (will be wired with AITaskExecutor later)
	artifactTemplatesRepo := repo.NewArtifactTemplatesRepository(fsClient.Client())
	coreDocsRepo := repo.NewCoreDocsRepo(fsClient.Client())
	var artifactTemplateService *services.ArtifactTemplateService
	var artifactTemplatesHandler *httphandlers.ArtifactTemplatesHandler
	var artifactGenerationHandler *httphandlers.ArtifactGenerationHandler
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
	var metaHandler *httphandlers.MetaHandler
	var agentsHandler *httphandlers.AgentsHandler
	var agentJobsHandler *httphandlers.AgentJobsHandler
	var agentJobsRepo *repo.AgentJobsRepository
	var verifiedHandler *httphandlers.VerifiedHandler
	var bigqueryHandler *httphandlers.BigQueryHandler
	var bigqueryAIService *services.BigQueryAIService
	var horizonService *horizon.HorizonService
	var horizonHandlers *horizonhttp.HorizonHandlers
	var scoutEngine *horizonscout.ScoutEngine
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

	// For brevity, only Vertex provider is wired here; additional providers can be added
	{
		aiServices := []ai.AIService{}

		if v := os.Getenv("GEMINI_API_KEY"); v != "" {
			aiServices = append(aiServices, ai.NewVertexAIService(v, modelManager))
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
			WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client()))
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
		artifactContextService := services.NewArtifactContextService(interactionsRepo, coreDocsRepo, chunksRepo)

		// Create genaiClient for artifact executor (using combined client)
		// Import gen_combined and gen_gemini packages at top of file
		geminiAPIKey := os.Getenv("GEMINI_API_KEY")
		vertexLocation := os.Getenv("VERTEX_LOCATION")
		if vertexLocation == "" {
			vertexLocation = "us-central1"
		}
		fastModel := os.Getenv("GENAI_FAST_MODEL")
		qualityModel := os.Getenv("GENAI_QUALITY_MODEL")
		geminiClient := gen_gemini.NewClient(geminiAPIKey, projectID, vertexLocation, fastModel, qualityModel)
		openaiAPIKey := os.Getenv("OPENAI_API_KEY")
		genaiClient := gen_combined.NewClient(geminiClient, openaiAPIKey)

		// Initialize BigQuery client and AI service
		bigqueryDatasetID := getEnv("BIGQUERY_DATASET_ID", "sanskrite_documents")
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
		artifactOrchestratorService := services.NewArtifactOrchestratorService(generationSessionsRepo, coreDocsRepo, artifactExecutorService, artifactContextService)
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
		orchestrator := discovery.NewOrchestrator(exec, wc, tgClient, s.projs, s.dsrcs, pub, logger).WithProfiles(s.sprofs).WithSourceProjects(sourceProjectsRepo)
		// Provider registry
		reg := prov.NewRegistry()
		reg.Register(prov.NewRSSProvider())
		// Embedding provider (OpenAI small)
		emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
		_ = emb // wired into orchestrator when ingest endpoints are added
		orchestrator = orchestrator.WithRAG(reg, docsRepo, chunksRepo).WithEmbedding(emb)

		// Wire ingest history repository EARLY to ensure poller has access to it
		ingestHistoryRepo := repo.NewSourceIngestHistoryRepository(fsClient.Client())
		orchestrator = orchestrator.WithIngestHistory(ingestHistoryRepo)
		// We also wire it to sourcesHandler later, but orchestrator needs it now

		// Start SourcePoller worker (listens for source.poll events)
		// Always enable if we have pubsub and orchestrator
		if pub != nil {
			psClient := pub.Client()
			// Ensure topic exists
			topicName := "source.poll"
			topic := psClient.Topic(topicName)
			if exists, err := topic.Exists(ctx); err == nil && !exists {
				_, _ = psClient.CreateTopic(ctx, topicName)
			}
			// Start worker
			sourcePoller := discovery.NewSourcePoller(psClient, topicName, orchestrator, logger)
			go func() {
				// Use background context to survive request context cancellation
				// But respect server shutdown via a separate context if needed
				// For now, just run forever
				if err := sourcePoller.Start(context.Background()); err != nil {
					logger.Printf("SourcePoller worker failed: %v", err)
				}
			}()
			logger.Printf("SourcePoller worker started on topic %s", topicName)
		}

		// Initialize GenAI handler with usage metrics and CoreDocs tools after all dependencies are ready
		genaiHandler = httphandlers.NewGenAIHandler().
			WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client())).
			WithModelManager(modelManager).
			WithAIExecutor(exec). // Wire task executor for model mode (structured tasks)
			WithCoreDocsTools(coreDocsRepo, interactionsRepo, docsRepo, chunksRepo, emb, gcsClient, bucket).
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

		// Initialize Wikidata client (for Knowledge Search tool)
		// Wikidata doesn't require API key - uses public API
		wikidataClient := httphandlers.GetWikidataClientFromEnv()
		if wikidataClient != nil {
			genaiHandler = genaiHandler.WithWikidataClient(wikidataClient)
			logger.Printf("Wikidata client initialized (Knowledge Search tool enabled)")
		} else {
			logger.Printf("Wikidata client disabled: initialization failed")
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
			entityEnricher = ontology.NewEntityEnricher(entitiesRepo, wikidataAdapter, logger)
		}

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
		)

		// Инициализация стандартной иерархии доменов для "system" tenant
		go func() {
			if err := domainService.InitializeHierarchy(context.Background(), "system"); err != nil {
				logger.Printf("WARNING: Failed to initialize domain hierarchy: %v", err)
			} else {
				logger.Printf("Domain hierarchy initialized for system tenant")
			}
		}()

		s.discoveryHandler = s.discoveryHandler.WithDocuments(docsRepo).WithSearchEvents(searchEventsRepo).WithSourceProjects(sourceProjectsRepo).WithChunks(chunksRepo).WithFirestoreClient(fsClient.Client()).WithOrchestrator(orchestrator)

		// Initialize Vertex AI Search client if configured
		vertexSearchProjectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
		// Discovery Engine Data Stores are created in "global" region
		// Default to "global" to match Data Store location
		vertexSearchLocation := getEnv("VERTEX_SEARCH_LOCATION", "global")
		vertexSearchDataStoreID := getEnv("VERTEX_SEARCH_DATA_STORE_ID", "")
		// Vertex AI Search requires OAuth2 (Application Default Credentials), not API key
		// Pass empty string to use OAuth2 authentication
		// Note: GEMINI_API_KEY may be set for other services, but Vertex AI Search uses OAuth2
		var vertexSearchClient *ai.VertexAISearchClient
		if vertexSearchDataStoreID != "" {
			vertexSearchClient = ai.NewVertexAISearchClient(vertexSearchProjectID, vertexSearchLocation, vertexSearchDataStoreID, "")
			s.discoveryHandler = s.discoveryHandler.WithVertexSearch(vertexSearchClient)
			logger.Printf("Vertex AI Search enabled: project=%s, location=%s, dataStore=%s", vertexSearchProjectID, vertexSearchLocation, vertexSearchDataStoreID)

			// Настраиваем автоматическую синхронизацию документов с Vertex AI Search
			// После загрузки документа в GCS автоматически триггерим импорт
			if ragDocsBucket != "" {
				docsRepo = docsRepo.WithVertexSearchSync(func(ctx context.Context, tenantID string) (string, error) {
					// Импортируем документы конкретного tenant (incremental sync)
					gcsPath := fmt.Sprintf("gs://%s/%s/**", ragDocsBucket, tenantID)
					importReq := &ai.ImportDocumentsRequest{
						GCSPath:            gcsPath,
						ReconciliationMode: "INCREMENTAL",
					}
					operationName, err := vertexSearchClient.ImportDocuments(ctx, importReq)
					if err != nil {
						logger.Printf("[VertexAISearch] Failed to trigger sync for tenant %s: %v", tenantID, err)
						return "", err
					}
					logger.Printf("[VertexAISearch] Sync triggered for tenant %s: operation=%s", tenantID, operationName)

					// Start tracking operation asynchronously
					go func(opName string, tid string) {
						trackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
						defer cancel()
						if err := ai.TrackImportOperation(trackCtx, vertexSearchClient, docsRepo, opName, tid, 5*time.Minute); err != nil {
							logger.Printf("[VertexAISearch] Failed to track operation %s: %v", opName, err)
						}
					}(operationName, tenantID)

					return operationName, nil
				})
				logger.Printf("Vertex AI Search auto-sync enabled: documents will sync automatically after GCS upload")
			}
		} else {
			logger.Printf("Vertex AI Search disabled: VERTEX_SEARCH_DATA_STORE_ID not set")
			logger.Printf("  Note: Vertex AI Search uses OAuth2 (Application Default Credentials), not API key")
		}

		// Start document processor worker (background)
		if pub != nil {
			psClient := pub.Client()
			// Передаем OntologyExtractor в DocumentProcessor
			// Используем агрегированный топик hatlone.artifact.events, куда публикуются события EventDocProcess
			worker := discovery.NewDocumentProcessor(psClient, string(realtime.TopicArtifactEvents), docsRepo, rulesRepo, exec, ontologyExtractor, logger)
			go func() {
				ctxW, cancelW := context.WithCancel(ctx)
				defer cancelW()
				if err := worker.Start(ctxW); err != nil {
					log.Printf("document processor stopped: %v", err)
				}
			}()

			// Start grounding processor worker (background)
			grounder := discovery.NewGroundingProcessor(psClient, string(realtime.EventGroundRefresh), orchestrator, logger)
			go func() {
				ctxG, cancelG := context.WithCancel(ctx)
				defer cancelG()
				if err := grounder.Start(ctxG); err != nil {
					log.Printf("grounding processor stopped: %v", err)
				}
			}()

			// Start source poller worker (background)
			poller := discovery.NewSourcePoller(psClient, string(realtime.EventSourcePoll), orchestrator, logger)
			// Wire poller to discovery handler for HTTP endpoint
			s.discoveryHandler = s.discoveryHandler.WithSourcePoller(poller)
			go func() {
				ctxP, cancelP := context.WithCancel(ctx)
				defer cancelP()
				if err := poller.Start(ctxP); err != nil {
					log.Printf("source poller stopped: %v", err)
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
							_ = poller.PublishDue(ctxPP)
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
			webhooksHandler = webhooksHandler.WithPoller(poller)
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
		// Wire RAG dependencies for CoreDoc indexing
		if emb != nil {
			s.projectsHandler = s.projectsHandler.WithRAG(docsRepo, chunksRepo, emb)
			// Configure automatic indexing callback in CoreDocsRepo
			// This ensures CoreDocs are automatically indexed when created/updated
			projectsHandlerRef := s.projectsHandler // Capture reference for closure
			coreDocsRepo = coreDocsRepo.WithIndexingCallback(func(ctx context.Context, tenantID string, cd *models.CoreDoc) error {
				return projectsHandlerRef.IndexCoreDoc(ctx, tenantID, cd)
			})
			logger.Printf("Automatic CoreDoc indexing enabled in repository")
		}
		// Wire Vertex Search client if configured
		if vertexSearchClient != nil {
			s.projectsHandler = s.projectsHandler.WithVertexSearch(vertexSearchClient)
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
		horizonFramesRepo := horizonrepo.NewFramesRepository(fsClient.Client())
		horizonLensesRepo := horizonrepo.NewLensesRepository(fsClient.Client())
		horizonEdgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
		horizonWatchdogRepo := horizonrepo.NewWatchdogConfigsRepository(fsClient.Client())
		horizonScoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
		horizonFragmentVersionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fsClient.Client())

		// Horizon spend ledger (actual GenAI $ attribution) for budget enforcement.
		horizonSpendLedger := horizonrepo.NewHorizonSpendLedger(fsClient.Client())
		// Feed spend increments from AITaskExecutor based on real per-call cost.
		if exec != nil {
			exec.WithHorizonSpendSink(horizonSpendLedger)
		}

		// Cloud Tasks dispatcher for Scout jobs (managed queue + autoscaling + retries).
		var scoutTasksDispatcher *horizontasks.ScoutCloudTasksDispatcher
		{
			enabled := strings.TrimSpace(getEnv("ENABLE_CLOUD_TASKS_SCOUT", "1"))
			if enabled == "1" || strings.EqualFold(enabled, "true") {
				location := strings.TrimSpace(getEnv("CLOUD_TASKS_LOCATION", "us-central1"))
				queue := strings.TrimSpace(getEnv("CLOUD_TASKS_SCOUT_QUEUE", "sanskrite-scout"))
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
						logger.Printf("Cloud Tasks (Scout) enabled: queue=%s location=%s target=%s", queue, location, targetURL)
						horizonScoutingJobsRepo.WithOnCreate(func(c context.Context, job *horizonmodels.ScoutingJob) error {
							// Enqueue by job ID; execution will load full state from Firestore.
							if err := scoutTasksDispatcher.EnqueueScoutJob(c, job.ID); err != nil {
								return err
							}
							logger.Printf("Cloud Tasks (Scout) enqueued: jobId=%s target=%s", job.ID, job.TargetCanonicalPath)
							return nil
						})
					} else {
						logger.Printf("WARNING: Cloud Tasks (Scout) disabled due to init error: %v", err)
					}
				} else {
					logger.Printf("WARNING: Cloud Tasks (Scout) not configured (need CLOUD_TASKS_TARGET_BASE_URL and CLOUD_TASKS_OIDC_SERVICE_ACCOUNT)")
				}
			}
		}

		// Configure dedicated GCS storage and Vertex AI Search sync for Horizon Fragments
		// We use a separate bucket to keep Horizon fragment indexing isolated and controllable.
		horizonFragmentsBucket := strings.TrimSpace(getEnv("HORIZON_FRAGMENTS_BUCKET", ""))
		if horizonFragmentsBucket == "" {
			horizonFragmentsBucket = ragDocsBucket // fallback for legacy setups
		}
		if gcsClient != nil && horizonFragmentsBucket != "" {
			horizonFragmentsRepo.WithStorage(gcsClient, horizonFragmentsBucket)
			if vertexSearchClient != nil {
				horizonFragmentsRepo.WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
					// Prefer a dedicated datastore for horizon fragments (store isolation instead of filters).
					if ds := strings.TrimSpace(getEnv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID", "")); ds != "" {
						return vertexSearchClient.WithDataStore(ds).ImportDocumentsFromURIs(ctx, []string{gcsURI}, "INCREMENTAL", "document")
					}
					// Fallback: legacy behavior (shared datastore) — still uses single-file import to avoid >1000 expansion.
					return vertexSearchClient.ImportDocumentsFromURIs(ctx, []string{gcsURI}, "INCREMENTAL", "document")
				})
				logger.Printf("Horizon Fragments: Vertex AI Search sync enabled")
			}
		}

		// Configure GCS export + Vertex sync for ontology_facts (unified fact frames)
		if gcsClient != nil && ragDocsBucket != "" {
			ontologyFactsRepo = ontologyFactsRepo.WithStorage(gcsClient, ragDocsBucket)
			if vertexSearchClient != nil {
				ontologyFactsRepo = ontologyFactsRepo.WithVertexSearchSync(func(ctx context.Context, tenantID string) (string, error) {
					gcsPath := fmt.Sprintf("gs://%s/%s/ontology_facts/**", ragDocsBucket, tenantID)
					importReq := &ai.ImportDocumentsRequest{
						GCSPath:            gcsPath,
						ReconciliationMode: "INCREMENTAL",
						DataSchema:         "document",
					}
					return vertexSearchClient.ImportDocuments(ctx, importReq)
				})
				logger.Printf("Ontology facts: Vertex AI Search sync enabled")
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
			ChunksRepo:         chunksRepo,
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
			// Initialize Lens subsystem
			lensLifecycle := horizonlens.NewLifecycleManager(horizonFragmentsRepo)
			lensCalculator := horizonlens.NewMetricsCalculator(horizonFragmentsRepo)
			lensService := horizonlens.NewLensService(horizonLensesRepo, horizonFragmentsRepo, horizonScoutingJobsRepo, lensLifecycle, lensCalculator)
			horizonService.SetLensService(lensService)

			// Initialize Propagation subsystem
			propagationEngine := horizonpropagation.NewPropagationEngine(
				horizonFragmentsRepo,
				horizonFragmentVersionsRepo,
				horizonEdgesRepo,
				horizonLensesRepo,
				pub,
			)
			horizonService.SetPropagationEngine(propagationEngine)

			// Запускаем фоновый процессор Cold Path задач (каждые 30 секунд)
			ctxBackground := context.Background()
			propagationEngine.StartColdPathProcessor(ctxBackground, 30*time.Second)

			// Initialize Scout subsystem
			scoutPlanner := horizonscout.NewScoutPlanner(s.dsrcs, ontologyFactsRepo, vertexSearchClient)

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
			// Budget enforcement wiring.
			scoutEngine.WithSpendLedger(horizonSpendLedger)

			// Подключаем триггер распространения изменений
			scoutEngine.WithPropagationTrigger(func(ctx context.Context, fragmentID string) error {
				_, err := propagationEngine.TriggerPropagation(ctx, fragmentID)
				return err
			})

			horizonService.SetScoutEngine(scoutEngine)
			logger.Printf("Scout Engine initialized")

			// Initialize Watchdog subsystem (requires ScoutEngine + SourceExecutor)
			watchdogMonitor := horizonwatchdog.NewMonitor(s.dsrcs, docsRepo, horizonFragmentsRepo, ontologyFactsRepo, horizonScoutingJobsRepo, scoutEngine, sourceExecutor, exec)
			watchdogScheduler := horizonwatchdog.NewScheduler()
			watchdogService := horizonwatchdog.NewWatchdogService(
				horizonWatchdogRepo,
				horizonFragmentsRepo,
				watchdogMonitor,
				watchdogScheduler,
				scoutEngine,
			)
			horizonService.SetWatchdogService(watchdogService)

			// Wire Lens lifecycle -> Watchdog lifecycle (create/pause configs on ACTIVE/DORMANT)
			lensService.SetWatchdogLifecycleHook(func(ctx context.Context, fragmentID string) {
				_ = watchdogService.UpdateConfigLifecycle(ctx, fragmentID)
			})

			// Legacy Firestore poller (disabled by default in prod).
			// Cloud Tasks is the preferred mechanism for executing Scout jobs.
			if strings.TrimSpace(getEnv("ENABLE_SCOUT_FIRESTORE_POLLER", "0")) == "1" {
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

		// Create HTTP handlers
		horizonHandlers = horizonhttp.NewHorizonHandlers(horizonService)
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

		// Execute with generous timeout (Cloud Run request timeout is 3600s).
		execCtx, cancel := context.WithTimeout(r.Context(), 45*time.Minute)
		defer cancel()

		if err := scoutEngine.ExecuteScoutingJob(execCtx, job); err != nil {
			// Retry behavior:
			// - transient failures: return 500 to let Cloud Tasks retry
			// - last attempt: mark failed and ACK (200) so it becomes "poison" (no more retries)
			retryCount := 0
			if v := strings.TrimSpace(r.Header.Get("X-CloudTasks-TaskRetryCount")); v != "" {
				if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
					retryCount = n
				}
			}
			// MaxAttempts is configured on the queue (we set to 5). On the last attempt, retryCount will be 4.
			if retryCount >= 4 {
				// Best-effort: ensure job is terminal, then ACK to stop retries.
				_ = horizonscout.NewScoutWorkflow(agentJobsRepo).TransitionState(context.Background(), jobID, horizonmodels.ScoutingJobStateFailed)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("failed (poisoned)"))
				return
			}
			http.Error(w, "scout execution failed (will retry)", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
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

	// Share routes handler (initialized before project sub-routes)
	shareRepo := repo.NewShareTokensRepository(fsClient.Client())
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
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			ah := httphandlers.NewArtifactsHandler(coreDocsRepo).WithStorage(gcsClient, bucket)
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
			ah := httphandlers.NewArtifactsHandler(coreDocsRepo).WithStorage(gcsClient, bucket)
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

	// Admin: publish-due (handled below with jobAuth middleware for Cloud Scheduler)

	// Discovery routes
	mux.Handle("/api/v1/sources-discovery/execute", authMiddleware(http.HandlerFunc(s.discoveryHandler.ExecuteDiscovery)))

	// Horizon routes
	if horizonHandlers != nil {
		horizonHandlers.RegisterRoutes(mux, authMiddleware)
		logger.Printf("Horizon routes registered")
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
				"https://hatl.one",
				"https://www.hatl.one",
				"https://*.hatl.one",
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
	mux.Handle("/api/v1/admin/sources/publish-due", jobAuth(http.HandlerFunc(s.discoveryHandler.PublishDue)))

	// Horizon Watchdog cron endpoint (Cloud Scheduler)
	// POST /api/v1/admin/horizon/watchdog/run?limit=200
	// Secured via the same jobAuth (X-Job-Token header) to avoid exposing cron publicly.
	mux.Handle("/api/v1/admin/horizon/watchdog/run", jobAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if horizonService == nil || horizonService.Watchdog() == nil {
			http.Error(w, "horizon watchdog not initialized", http.StatusServiceUnavailable)
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
		if v := os.Getenv("HORIZON_WATCHDOG_CRON_LOCK_TTL"); strings.TrimSpace(v) != "" {
			if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil && d >= 30*time.Second {
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
	})))

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
				// Subdomain wildcard entries like "*.hatl.one" or URL with scheme
				if allowOrigin == "" {
					u, err := url.Parse(origin)
					if err == nil {
						host := u.Hostname()
						scheme := u.Scheme
						for _, ao := range allowedOrigins {
							// Handle wildcard patterns with scheme like "https://*.hatl.one"
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
										// Wildcard pattern: "*.hatl.one" matches "hatl.one", "www.hatl.one", etc.
										suffix := strings.TrimPrefix(aoHost, "*")
										// Match exact domain (hatl.one) or subdomain (www.hatl.one)
										if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
											allowOrigin = origin
											break
										}
									}
								}
							} else if strings.HasPrefix(ao, "*.") {
								// Wildcard pattern without scheme: "*.hatl.one"
								suffix := strings.TrimPrefix(ao, "*")
								// Match exact domain (hatl.one) or subdomain (www.hatl.one)
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

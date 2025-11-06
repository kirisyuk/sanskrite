package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/paypal"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/services"
	"google.golang.org/api/option"

	// orchestrator deps
	"cloud.google.com/go/pubsub"
	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

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
	fbApp, err := firebase.NewApp(ctx, &firebase.Config{
		ProjectID: projectID,
	}, opts...)
	if err != nil {
		log.Fatalf("firebase app init: %v", err)
	}

	// Initialize Firestore client
	fsClient, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore init: %v", err)
	}

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
	// RAG repos
	docsRepo := repo.NewDocumentsRepository(fsClient.Client())
	chunksRepo := repo.NewChunksRepository(fsClient.Client())

	// Artifact templates repo/service/handler (will be wired with AITaskExecutor later)
	artifactTemplatesRepo := repo.NewArtifactTemplatesRepository(fsClient.Client())
	coreDocsRepo := repo.NewCoreDocsRepo(fsClient.Client())
	var artifactTemplateService *services.ArtifactTemplateService
	var artifactTemplatesHandler *httphandlers.ArtifactTemplatesHandler
	var artifactGenerationHandler *httphandlers.ArtifactGenerationHandler

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
	var verifiedHandler *httphandlers.VerifiedHandler
	sourcesHandler := httphandlers.NewSourcesHandler(repo.NewSourceProjectsRepository(fsClient.Client()), s.dsrcs, logger)
	searchEventsRepo := repo.NewSearchEventsRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())
	rulesRepo := repo.NewRulesRepository(fsClient.Client())
	rulesHandler := httphandlers.NewRulesHandler(rulesRepo, logger)
	webhooksHandler := httphandlers.NewWebhooksHandler(logger)

	// Wire orchestrator (AITaskExecutor + ModelManager + Vertex service)
	modelManager := ai.NewAIModelManager()

	// Initialize GenAI handler with usage metrics after modelManager is created
	genaiHandler = httphandlers.NewGenAIHandler().
		WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client())).
		WithModelManager(modelManager)
	// For brevity, only Vertex provider is wired here; additional providers can be added
	{
		aiServices := []ai.AIService{}

		if v := os.Getenv("VERTEX_API_KEY"); v != "" {
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
		tgBase := os.Getenv("TELEGRAM_SERVICE_URL")
		tgClient := telegram.NewClient(tgBase)
		projectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
		pub, err := realtime.NewPublisher(ctx, projectID)
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
		vertexAPIKey := os.Getenv("VERTEX_API_KEY")
		vertexLocation := os.Getenv("VERTEX_LOCATION")
		if vertexLocation == "" {
			vertexLocation = "us-central1"
		}
		fastModel := os.Getenv("GENAI_FAST_MODEL")
		qualityModel := os.Getenv("GENAI_QUALITY_MODEL")
		geminiClient := gen_gemini.NewClient(vertexAPIKey, projectID, vertexLocation, fastModel, qualityModel)
		openaiAPIKey := os.Getenv("OPENAI_API_KEY")
		genaiClient := gen_combined.NewClient(geminiClient, openaiAPIKey)

		artifactExecutorService := services.NewArtifactExecutorService(genaiClient, artifactContextService, pub).
			WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client()), modelManager)

		generationSessionsRepo := repo.NewGenerationSessionsRepository(fsClient.Client())
		artifactOrchestratorService := services.NewArtifactOrchestratorService(generationSessionsRepo, coreDocsRepo, artifactExecutorService, artifactContextService)
		artifactGenerationHandler = httphandlers.NewArtifactGenerationHandler(artifactOrchestratorService)
		orchestrator := discovery.NewOrchestrator(exec, wc, tgClient, s.projs, s.dsrcs, pub, logger).WithProfiles(s.sprofs).WithSourceProjects(sourceProjectsRepo)
		// Provider registry
		reg := prov.NewRegistry()
		reg.Register(prov.NewRSSProvider())
		// Embedding provider (OpenAI small)
		emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
		_ = emb // wired into orchestrator when ingest endpoints are added
		orchestrator = orchestrator.WithRAG(reg, docsRepo, chunksRepo).WithEmbedding(emb)
		s.discoveryHandler = s.discoveryHandler.WithDocuments(docsRepo).WithSearchEvents(searchEventsRepo).WithSourceProjects(sourceProjectsRepo).WithChunks(chunksRepo)

		// Start document processor worker (background)
		if pub != nil {
			psClient := pub.Client()
			worker := discovery.NewDocumentProcessor(psClient, string(realtime.EventDocProcess), docsRepo, rulesRepo, exec, logger)
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
		cacheRepo := repo.NewProcessedContentRepository(fsClient.Client())
		interactions := repo.NewInteractionsRepository(fsClient.Client())
		orchestrator = orchestrator.WithCache(cacheRepo).WithInteractions(interactions)

		// Verified URL Analysis handler (initialized after orchestrator and genaiHandler are ready)
		verifiedRepo := repo.NewVerifiedAnalysesRepository(fsClient.Client())
		verifiedHandler = httphandlers.NewVerifiedHandler(
			verifiedRepo,
			s.dsrcs,
			docsRepo,
			orchestrator,
			genaiHandler,
			pub,
			logger,
		)
		// Core docs repo
		coreDocsRepo := repo.NewCoreDocsRepo(fsClient.Client())
		// bucket name env or default
		bucket := os.Getenv("CORE_DOCS_BUCKET")
		if bucket == "" {
			bucket = projectID + "-core-docs"
		}
		s.projectsHandler = s.projectsHandler.WithCoreDocs(coreDocsRepo).WithStorage(gcsClient, bucket).WithAIExecutor(exec)
		// Wire cascade repos to projects handler
		s.projectsHandler = s.projectsHandler.WithCascade(s.sprofs, interactions)
		s.discoveryHandler = s.discoveryHandler.WithOrchestrator(orchestrator).WithInteractions(interactions).WithProfiles(s.sprofs)
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

		// Initialize Agent Registry
		agentRegistry := agents.NewRegistry(exec, genaiClient, pub, logger).
			WithUsageMetrics(repo.NewUsageMetricsRepository(fsClient.Client()), modelManager)
		agentsHandler = httphandlers.NewAgentsHandler(agentRegistry, pub, logger)
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
				_ = sub.Start(ctx,
					string(realtime.EventDiscoveryStatus),
					string(realtime.EventDiscoveryProfile),
					string(realtime.EventDiscoveryAnswer),
					string(realtime.EventDiscoveryProject),
					// Core document processing updates (entities/facts/events/metrics/decisions)
					string(realtime.EventDocProcess),
					// Unified analysis status updates
					string(realtime.EventAnalysisStatus),
					// Subscribe WS hub to GenAI streaming topics
					string(realtime.EventGenAIStream),
					// Subscribe WS hub to Artifact generation topics
					string(realtime.EventArtifactGenerationStarted),
					string(realtime.EventArtifactSectionGenerating),
					string(realtime.EventArtifactSectionChunk),
					string(realtime.EventArtifactSectionCompleted),
					string(realtime.EventArtifactAssemblyStarted),
					string(realtime.EventArtifactAssemblyChunk),
					string(realtime.EventArtifactAssemblyCompleted),
					string(realtime.EventArtifactGenerationCompleted),
					string(realtime.EventArtifactGenerationError),
					string(realtime.EventAgentUpdate),
				)
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

	// Project routes and nested sub-routes
	mux.Handle("/api/v1/projects", authMiddleware(http.HandlerFunc(s.routeProjects)))
	mux.Handle("/api/v1/projects/", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sources") && r.Method == http.MethodPost {
			sourcesHandler.AttachSource(w, r)
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
			ah := httphandlers.NewArtifactsHandler(coreDocsRepo)
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
			ah := httphandlers.NewArtifactsHandler(coreDocsRepo)
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

		// fallback to default project by id handler
		s.routeProjectsByID(w, r)
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

	// Webhooks
	mux.Handle("/api/v1/webhooks/", http.HandlerFunc(webhooksHandler.Handle))

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

	// Public share endpoint (unauthenticated)
	mux.Handle("/api/v1/public/share/", http.HandlerFunc(shareHandler.GetPublicShare))

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

	// Agents routes
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
	} else {
		legacy := getEnv("FRONTEND_ORIGIN", "")
		if legacy != "" {
			allowedOrigins = []string{legacy}
		} else {
			allowedOrigins = []string{
				"http://localhost:5173",
				"http://localhost:3000",
				"https://hatl.one",
				"https://www.hatl.one",
				"https://*.hatl.one",
			}
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
func (s *server) routeProjectsByID(w http.ResponseWriter, r *http.Request) {
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
							if strings.HasPrefix(ao, "http://") || strings.HasPrefix(ao, "https://") {
								if au, err2 := url.Parse(ao); err2 == nil {
									ahost := au.Hostname()
									if strings.HasPrefix(ahost, "*.") {
										if strings.HasSuffix(host, strings.TrimPrefix(ahost, "*")) && au.Scheme == scheme {
											allowOrigin = origin
											break
										}
									}
								}
							} else if strings.HasPrefix(ao, "*.") {
								if strings.HasSuffix(host, strings.TrimPrefix(ao, "*")) {
									allowOrigin = origin
									break
								}
							}
						}
					}
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

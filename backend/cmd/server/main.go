package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"google.golang.org/api/option"

	// orchestrator deps
	"cloud.google.com/go/pubsub"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
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

	// Handlers
	s.usersHandler = httphandlers.NewUsersHandler(s.users, logger)
	s.projectsHandler = httphandlers.NewProjectsHandler(s.projs, logger).WithDataSources(s.dsrcs)
	s.discoveryHandler = httphandlers.NewDiscoveryHandler(s.projs, s.dsrcs, logger)
	sourcesHandler := httphandlers.NewSourcesHandler(repo.NewSourceProjectsRepository(fsClient.Client()), s.dsrcs, logger)
	searchEventsRepo := repo.NewSearchEventsRepository(fsClient.Client())
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient.Client())
	rulesRepo := repo.NewRulesRepository(fsClient.Client())
	rulesHandler := httphandlers.NewRulesHandler(rulesRepo, logger)
	webhooksHandler := httphandlers.NewWebhooksHandler(logger)

	// Wire orchestrator (AITaskExecutor + ModelManager + Vertex service)
	modelManager := ai.NewAIModelManager()
	// For brevity, only Vertex provider is wired here; additional providers can be added
	{
		services := []ai.AIService{}

		if v := os.Getenv("VERTEX_API_KEY"); v != "" {
			services = append(services, ai.NewVertexAIService(v, modelManager))
		}
		if k := os.Getenv("OPENAI_API_KEY"); k != "" {
			services = append(services, ai.NewOpenAIService(k, modelManager))
		}
		if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
			services = append(services, ai.NewClaudeService(k, modelManager))
		}
		if k := os.Getenv("GROK_API_KEY"); k != "" {
			services = append(services, ai.NewGrokService(k, modelManager))
		}

		exec := ai.NewAITaskExecutor(services, modelManager)
		wc := crawler.NewWebCrawler()
		tgBase := os.Getenv("TELEGRAM_SERVICE_URL")
		tgClient := telegram.NewClient(tgBase)
		projectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
		pub, err := realtime.NewPublisher(ctx, projectID)
		if err != nil {
			log.Printf("WARNING: PubSub publisher init failed: %v (realtime updates disabled)", err)
		}
		orchestrator := discovery.NewOrchestrator(exec, wc, tgClient, s.projs, s.dsrcs, pub, logger).WithProfiles(s.sprofs)
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
			go func() {
				ctxPP, cancelPP := context.WithCancel(ctx)
				defer cancelPP()
				t := time.NewTicker(1 * time.Minute)
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

			// Wire HTTP handler to allow Cloud Scheduler HTTP trigger
			webhooksHandler = webhooksHandler.WithPoller(poller)
		}
		cacheRepo := repo.NewProcessedContentRepository(fsClient.Client())
		interactions := repo.NewInteractionsRepository(fsClient.Client())
		orchestrator = orchestrator.WithCache(cacheRepo).WithInteractions(interactions)
		// Wire cascade repos to projects handler
		s.projectsHandler = s.projectsHandler.WithCascade(s.sprofs, interactions)
		s.discoveryHandler = s.discoveryHandler.WithOrchestrator(orchestrator).WithInteractions(interactions).WithProfiles(s.sprofs)
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

	// WS hub and Pub/Sub subscriber
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
	{
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
				)
			} else {
				log.Printf("ws subscriber: pubsub client init error: %v", err)
			}
		}
	}

	// Apply auth middleware to all /api/v1/* routes
	// Compose middleware: attach repos, then auth
	authMiddleware := func(h http.Handler) http.Handler {
		return httphandlers.WithUsersRepo(s.users)(httphandlers.AuthMiddleware(authClient)(h))
	}

	// User routes
	mux.Handle("/api/v1/users", authMiddleware(http.HandlerFunc(s.usersHandler.CreateUser)))
	mux.Handle("/api/v1/users/", authMiddleware(http.HandlerFunc(s.routeUsersByID)))

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

	// Admin: publish-due (allow Cloud Scheduler HTTP trigger)
	mux.Handle("/api/v1/admin/sources/publish-due", authMiddleware(http.HandlerFunc(s.discoveryHandler.PublishDue)))

	// Discovery routes
	mux.Handle("/api/v1/sources-discovery/execute", authMiddleware(http.HandlerFunc(s.discoveryHandler.ExecuteDiscovery)))
	// Note: /api/v1/projects/{id}/datasources is handled by projectsByID pattern

	// Legacy routes (to be migrated)
	// mux.Handle("/v1/whoami", authMiddleware(http.HandlerFunc(s.whoAmI)))
	// mux.Handle("/v1/users/me", authMiddleware(http.HandlerFunc(s.upsertMe)))
	// mux.Handle("/v1/projects", authMiddleware(http.HandlerFunc(s.createProject)))
	// mux.Handle("/v1/datasources", authMiddleware(http.HandlerFunc(s.createDataSource)))

	// CORS for local frontend
	allowedOrigin := getEnv("FRONTEND_ORIGIN", "http://localhost:5173")

	// Admin job token middleware for Cloud Scheduler (bypasses Firebase auth for admin route only)
	adminToken := getEnv("ADMIN_JOB_TOKEN", "")
	jobAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if adminToken == "" {
				http.Error(w, "admin job token not configured", http.StatusForbidden)
				return
			}
			if r.Header.Get("X-Job-Token") != adminToken {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// Attach admin route without Firebase auth, but with jobAuth
	mux.Handle("/api/v1/admin/sources/publish-due", jobAuth(http.HandlerFunc(s.discoveryHandler.PublishDue)))

	handler := withCORS(allowedOrigin)(mux)

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
func withCORS(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/controlplane"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
	agentmeshrepo "github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	agentmeshscout "github.com/igorkirisyuk/sanskrite/internal/agentmesh/scout"
	agentmeshsimulation "github.com/igorkirisyuk/sanskrite/internal/agentmesh/simulation"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/tasks"
	meshtrace "github.com/igorkirisyuk/sanskrite/internal/agentmesh/trace"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/transport/a2a"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/transport/mcp"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/vertex"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"google.golang.org/api/idtoken"
)

func main() {
	var (
		projectID = flag.String("project", strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), "GCP project ID")
		location  = flag.String("location", envOr("AGENTMESH_VERTEX_LOCATION", "global"), "Vertex location")
		port      = flag.String("port", envOr("PORT", "8080"), "HTTP port")
	)
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("GCP project ID is required")
	}

	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()
	gcs, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("gcs client: %v", err)
	}

	logger := log.New(os.Stdout, "[agentmesh] ", log.LstdFlags|log.Lshortfile)
	store := agentmeshrepo.NewStore(fs.Client(), gcs)
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AGENTMESH_PUBLIC_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("CLOUD_RUN_URL")), "/")
	}

	cardSigner, err := registry.NewKMSSigner(ctx, requiredEnv("AGENTMESH_AGENT_CARD_KMS_KEY_VERSION"), envOr("AGENTMESH_AGENT_CARD_KEY_ID", ""))
	if err != nil {
		log.Fatalf("agent card signer: %v", err)
	}
	defer cardSigner.Close()

	registrySvc := registry.NewService(cardSigner)
	if err := registrySvc.BootstrapSystemAgents(ctx, baseURL); err != nil {
		log.Fatalf("bootstrap registry: %v", err)
	}
	for _, agent := range registrySvc.ListAgents() {
		if err := store.SaveAgent(ctx, &agent); err != nil {
			log.Fatalf("save agent %s: %v", agent.ID, err)
		}
		for _, capability := range agent.Capabilities {
			if err := store.SaveCapability(ctx, capability); err != nil {
				log.Fatalf("save capability %s: %v", capability.Name, err)
			}
		}
	}

	policySvc := policy.NewService()
	controlSvc := controlplane.NewService(registrySvc, policySvc)
	vertexSvc, err := vertex.NewService(*projectID, *location, strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")))
	if err != nil {
		log.Fatalf("vertex service: %v", err)
	}
	traceSvc := meshtrace.NewService(store, logger)
	vertexSearchClient := ai.NewVertexAISearchClient(*projectID, envOr("VERTEX_SEARCH_LOCATION", "global"), strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")), "")
	productDictionary := productdict.NewRuntime(fs.Client()).WithTenantID("global")

	modelManager := ai.NewAIModelManager()
	{
		aiServices := []ai.AIService{vertexSvc.AIService()}
		geminiKey := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, *projectID, "GEMINI_API_KEY", "gemini_api_key"))
		if geminiKey != "" {
			if os.Getenv("GEMINI_API_KEY") == "" {
				_ = os.Setenv("GEMINI_API_KEY", geminiKey)
			}
			aiServices = append(aiServices, ai.NewGeminiService(geminiKey, modelManager))
		}
		if k := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, *projectID, "OPENAI_API_KEY", "openai_api_key")); k != "" {
			if os.Getenv("OPENAI_API_KEY") == "" {
				_ = os.Setenv("OPENAI_API_KEY", k)
			}
			aiServices = append(aiServices, ai.NewOpenAIService(k, modelManager))
		}
		if k := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, *projectID, "GROK_API_KEY", "grok_api_key")); k != "" {
			if os.Getenv("GROK_API_KEY") == "" {
				_ = os.Setenv("GROK_API_KEY", k)
			}
			aiServices = append(aiServices, ai.NewGrokService(k, modelManager))
		}
		vertexSvc.WithAIExecutor(
			ai.NewAITaskExecutor(aiServices, modelManager).
				WithCircuitBreaker(ai.NewCircuitBreaker()),
		)
	}

	simulationProcessor := agentmeshsimulation.NewProcessor(
		horizonrepo.NewSimulationRunsRepository(fs.Client()),
		horizonrepo.NewSimulationWorldsRepository(fs.Client()),
		horizonrepo.NewSimulationAgentsRepository(fs.Client()),
		horizonrepo.NewSimulationEventsRepository(fs.Client()),
		horizonrepo.NewSimulationArtifactsRepository(fs.Client()),
		nil,
		logger,
	)
	scoutProcessor := agentmeshscout.NewProcessor(
		corerepo.NewAgentJobsRepository(fs.Client()),
		horizonrepo.NewScoutingJobsRepository(corerepo.NewAgentJobsRepository(fs.Client())),
		horizonrepo.NewHorizonFragmentsRepository(fs.Client()),
		horizonrepo.NewDependencyEdgesRepository(fs.Client()),
		horizonrepo.NewMaterializedArtifactsRepository(fs.Client()),
		productDictionary,
		vertexSearchClient,
		logger,
	)

	var dispatcher *tasks.Dispatcher
	taskTargetURL := strings.TrimSpace(os.Getenv("AGENTMESH_TASK_TARGET_URL"))
	if taskTargetURL != "" {
		taskOIDCSA := envOr("AGENTMESH_TASKS_OIDC_SERVICE_ACCOUNT", "backend@"+*projectID+".iam.gserviceaccount.com")
		if envEnabled("AGENTMESH_TASKS_DISABLE_OIDC") {
			taskOIDCSA = ""
		}
		dispatcher, err = tasks.NewDispatcher(
			ctx,
			*projectID,
			envOr("AGENTMESH_TASKS_LOCATION", "us-central1"),
			envOr("AGENTMESH_TASKS_QUEUE", "nv-horizon-agentmesh"),
			taskTargetURL,
			taskOIDCSA,
			envOr("AGENTMESH_TASKS_OIDC_AUDIENCE", ""),
		)
		if err != nil {
			log.Fatalf("task dispatcher: %v", err)
		}
		defer dispatcher.Close()
	}

	runtimeSvc := runtime.NewService(
		store,
		controlSvc,
		vertexSvc,
		traceSvc,
		dispatcher,
		envOr("AGENTMESH_ARTIFACT_BUCKET", *projectID+"-agentmesh-artifacts"),
		logger,
	).WithProcessor(simulationProcessor).WithProcessor(scoutProcessor)
	if err := store.SavePolicySnapshot(ctx, "default", map[string]any{
		"vertexSubstrate": "enabled",
		"a2aVersion":      "1.0.0",
		"mcpTransport":    "remote-http",
		"updatedBy":       "cmd/agentmesh",
	}); err != nil {
		log.Fatalf("save policy snapshot: %v", err)
	}

	mux := http.NewServeMux()
	a2a.NewHandler(registrySvc, runtimeSvc, store).Register(mux)
	mcp.NewHandler(registrySvc, policySvc, runtimeSvc, store, envOr("AGENTMESH_RUNTIME_LEASE_OWNER", "agentmesh-runtime")).Register(mux)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := runtimeSvc.Readiness(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/v1/internal/tasks/agentmesh/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if err := verifyOIDCToken(r); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
			return
		}
		var payload tasks.TaskPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
			return
		}
		if err := runtimeSvc.ExecuteTask(r.Context(), payload.TaskID, envOr("AGENTMESH_RUNTIME_LEASE_OWNER", "agentmesh-runtime")); err != nil {
			logger.Printf("task execution failed: taskId=%s err=%v", payload.TaskID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "taskId": payload.TaskID})
	})

	server := &http.Server{
		Addr:              ":" + *port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Printf("starting AgentMesh on :%s", *port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func verifyOIDCToken(r *http.Request) error {
	if envEnabled("AGENTMESH_ALLOW_UNAUTH_INTERNAL_TASKS") {
		return nil
	}
	rawToken := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if rawToken == "" {
		return httpError("missing bearer token")
	}
	aud := strings.TrimSpace(os.Getenv("AGENTMESH_TASKS_OIDC_AUDIENCE"))
	if aud == "" {
		aud = strings.TrimSpace(os.Getenv("AGENTMESH_PUBLIC_BASE_URL"))
	}
	payload, err := idtoken.Validate(r.Context(), rawToken, aud)
	if err != nil {
		return err
	}
	expectedSA := strings.TrimSpace(os.Getenv("AGENTMESH_TASKS_OIDC_SERVICE_ACCOUNT"))
	if expectedSA != "" {
		email, _ := payload.Claims["email"].(string)
		if !strings.EqualFold(strings.TrimSpace(email), expectedSA) {
			return httpError("unexpected service account")
		}
	}
	return nil
}

type httpError string

func (e httpError) Error() string { return string(e) }

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envEnabled(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	return value != "0" && !strings.EqualFold(value, "false")
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

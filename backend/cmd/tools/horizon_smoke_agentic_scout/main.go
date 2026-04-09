package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	agentmeshcontracts "github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/controlplane"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
	agentmeshrepo "github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	agentmeshruntime "github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	agentmeshscout "github.com/igorkirisyuk/sanskrite/internal/agentmesh/scout"
	meshtrace "github.com/igorkirisyuk/sanskrite/internal/agentmesh/trace"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/vertex"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID   = flag.String("project", envOr("GCP_PROJECT_ID", "nv-horizon"), "GCP project ID")
		location    = flag.String("location", envOr("AGENTMESH_VERTEX_LOCATION", "global"), "Vertex model location")
		query       = flag.String("query", "", "Natural-language scouting query")
		target      = flag.String("target", "", "Optional canonical target path")
		tenantID    = flag.String("tenant", "global", "Tenant ID")
		projectRef  = flag.String("project-ref", "nv-horizon-test", "Logical Horizon project ID")
		lensID      = flag.String("lens", "", "Optional lens ID")
		allowWrites = flag.Bool("allow-writes", true, "Allow deterministic artifact writes")
		compareOnly = flag.Bool("compare-only", false, "Run in compare-only mode without graph landing semantics")
	)
	flag.Parse()

	if strings.TrimSpace(*query) == "" && strings.TrimSpace(*target) == "" {
		log.Fatal("either --query or --target is required")
	}

	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()
	gcs, err := gcp.NewGCSClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("gcs client: %v", err)
	}

	logger := log.New(os.Stdout, "[agentic-scout-smoke] ", log.LstdFlags|log.Lshortfile)
	store := agentmeshrepo.NewStore(fs.Client(), gcs)
	cardSigner := registry.NewHMACSigner(envOr("AGENTMESH_SERVER_SIGNING_SECRET", "server-local-secret"), "server-local")
	registrySvc := registry.NewService(cardSigner)
	if err := registrySvc.BootstrapSystemAgents(ctx, "https://local.agentmesh.internal"); err != nil {
		log.Fatalf("bootstrap registry: %v", err)
	}
	controlSvc := controlplane.NewService(registrySvc, policy.NewService())
	vertexSvc, err := vertex.NewService(strings.TrimSpace(*projectID), strings.TrimSpace(*location), strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")))
	if err != nil {
		log.Fatalf("vertex service: %v", err)
	}
	traceSvc := meshtrace.NewService(store, logger)
	vertexSearch := ai.NewVertexAISearchClient(strings.TrimSpace(*projectID), envOr("VERTEX_SEARCH_LOCATION", "global"), strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")), "")
	productDictionary := productdict.NewRuntime(fs.Client()).WithTenantID("global")
	agentJobsRepo := corerepo.NewAgentJobsRepository(fs.Client())
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	scoutProcessor := agentmeshscout.NewProcessor(
		agentJobsRepo,
		scoutingJobsRepo,
		horizonrepo.NewHorizonFragmentsRepository(fs.Client()),
		horizonrepo.NewDependencyEdgesRepository(fs.Client()),
		horizonrepo.NewMaterializedArtifactsRepository(fs.Client()),
		productDictionary,
		vertexSearch,
		logger,
	)
	runtimeSvc := agentmeshruntime.NewService(
		store,
		controlSvc,
		vertexSvc,
		traceSvc,
		nil,
		envOr("AGENTMESH_ARTIFACT_BUCKET", strings.TrimSpace(*projectID)+"-agentmesh-artifacts"),
		logger,
	).WithProcessor(scoutProcessor)

	task, err := runtimeSvc.CreateTask(ctx, &agentmeshcontracts.MeshTask{
		TenantID:   strings.TrimSpace(*tenantID),
		Capability: "scout.plan",
		Input: map[string]any{
			"tenantId":            strings.TrimSpace(*tenantID),
			"projectId":           strings.TrimSpace(*projectRef),
			"lensId":              strings.TrimSpace(*lensID),
			"query":               strings.TrimSpace(*query),
			"targetCanonicalPath": strings.TrimSpace(*target),
			"allowWrites":         *allowWrites,
			"compareOnly":         *compareOnly,
		},
	}, policy.RequestContext{CallerAgentID: "internal", DataZone: "internal"}, true)
	if err != nil {
		log.Fatalf("create task: %v", err)
	}
	if err := runtimeSvc.ExecuteTask(ctx, task.ID, "agentic-scout-smoke"); err != nil {
		log.Fatalf("execute task: %v", err)
	}
	finalTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		log.Fatalf("load task: %v", err)
	}
	if finalTask == nil {
		log.Fatal("task not found after execution")
	}

	out := map[string]any{
		"taskId":         finalTask.ID,
		"status":         finalTask.Status,
		"reviewAccepted": finalTask.ReviewAccepted != nil && *finalTask.ReviewAccepted,
		"lastError":      strings.TrimSpace(finalTask.LastError),
		"output":         finalTask.Output,
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(raw))
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

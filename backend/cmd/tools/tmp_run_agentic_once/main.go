package main

import (
	"context"
	"flag"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	agentmeshcontracts "github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/controlplane"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
	agentmeshrepo "github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	agentmeshruntime "github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	agentmeshscout "github.com/igorkirisyuk/sanskrite/internal/agentmesh/scout"
	agentmeshsubmitter "github.com/igorkirisyuk/sanskrite/internal/agentmesh/submitter"
	meshtrace "github.com/igorkirisyuk/sanskrite/internal/agentmesh/trace"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/vertex"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscoutbridge "github.com/igorkirisyuk/sanskrite/internal/horizon/scoutbridge"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	projectID := flag.String("project", strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), "GCP project ID")
	location := flag.String("location", strings.TrimSpace(os.Getenv("AGENTMESH_VERTEX_LOCATION")), "Vertex location")
	query := flag.String("query", "", "Query")
	timeout := flag.Duration("timeout", 2*time.Minute, "Timeout")
	stageOnly := flag.Bool("submit-only", false, "Stop after direct submit diagnostics")
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" || strings.TrimSpace(*query) == "" {
		log.Fatal("--project and --query are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer fs.Close()
	gcs, err := gcp.NewGCSClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("gcs: %v", err)
	}

	logger := log.New(os.Stdout, "[tmp-run-agentic-once] ", log.LstdFlags|log.Lshortfile)
	store := agentmeshrepo.NewStore(fs.Client(), gcs)
	cardSigner := registry.NewHMACSigner("server-local-secret", "server-local")
	registrySvc := registry.NewService(cardSigner)
	if err := registrySvc.BootstrapSystemAgents(ctx, "https://local.agentmesh.internal"); err != nil {
		log.Fatalf("bootstrap registry: %v", err)
	}
	controlSvc := controlplane.NewService(registrySvc, policy.NewService())
	vertexSvc, err := vertex.NewService(strings.TrimSpace(*projectID), strings.TrimSpace(*location), strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")))
	if err != nil {
		log.Fatalf("vertex: %v", err)
	}
	traceSvc := meshtrace.NewService(store, logger)
	vertexSearch := ai.NewVertexAISearchClient(strings.TrimSpace(*projectID), "global", strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")), "")
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
		strings.TrimSpace(*projectID)+"-agentmesh-artifacts",
		logger,
	).WithProcessor(scoutProcessor)
	submitter := agentmeshsubmitter.NewService(runtimeSvc)
	agenticSvc := horizonscoutbridge.NewAgenticService(submitter, store, agentJobsRepo, scoutingJobsRepo, logger)
	executor := horizonscoutbridge.NewExecutor(nil, agenticSvc, logger)

	now := time.Now().UTC()
	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			TenantID:  "global",
			ProjectID: "nv-horizon-test",
			AgentID:   "horizon_scout",
			Title:     "tmp run",
			Status:    coremodels.AgentJobStatusPending,
			AutoMode:  true,
			UserQuery: strings.TrimSpace(*query),
			PlanMode:  coremodels.PlanModeAuto,
			Task: map[string]any{
				"query": strings.TrimSpace(*query),
			},
			State: map[string]any{
				"runtimeMode": horizonpolicy.ScoutRuntimeAgentic,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		ScoutingStrategy: string(horizonmodels.ScoutingStrategyDeepDive),
		CurrentState:     horizonmodels.ScoutingJobStatePending,
	}
	jobID, err := scoutingJobsRepo.Create(ctx, job)
	if err != nil {
		log.Fatalf("create job: %v", err)
	}
	job.ID = jobID
	log.Printf("job=%s created", job.ID)
	if *stageOnly {
		directSubmitDiagnostics(ctx, runtimeSvc, logger, job)
		return
	}
	err = executor.ExecuteScoutingJob(ctx, job)
	if err != nil {
		log.Printf("execute error: %v", err)
	} else {
		log.Printf("execute succeeded")
	}
}

func directSubmitDiagnostics(ctx context.Context, runtimeSvc *agentmeshruntime.Service, logger *log.Logger, job *horizonmodels.ScoutingJob) {
	input := map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"scoutingJobId":       strings.TrimSpace(job.ID),
		"query":               strings.TrimSpace(job.UserQuery),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"compareOnly":         false,
		"allowWrites":         true,
	}
	task := &agentmeshcontracts.MeshTask{
		TenantID:   strings.TrimSpace(job.TenantID),
		Capability: "scout.plan",
		Input:      input,
		Lease: agentmeshcontracts.Lease{
			DedupKey: "scout.plan:" + strings.TrimSpace(job.ID),
		},
	}
	req := policy.RequestContext{
		CallerAgentID: "internal",
		DataZone:      "internal",
	}

	log.Printf("submit-diagnostics starting job=%s dedup=%s", job.ID, task.Lease.DedupKey)
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
			logger.Printf("submit-diagnostics timeout; dumping goroutines")
			_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
		}
	}()

	start := time.Now()
	createdTask, err := runtimeSvc.CreateTask(ctx, task, req, true)
	close(done)
	log.Printf("runtime.CreateTask finished after=%s err=%v", time.Since(start), err)
	if createdTask != nil {
		log.Printf("created task id=%s capability=%s routed=%s status=%s", createdTask.ID, createdTask.Capability, createdTask.RoutedAgentID, createdTask.Status)
	}
	if err != nil {
		log.Printf("runtime.CreateTask error detail: %v", err)
		return
	}

	log.Printf("goroutines=%d", runtime.NumGoroutine())
}

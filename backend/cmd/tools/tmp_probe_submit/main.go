package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"strings"
	"time"

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
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	projectID := flag.String("project", strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), "GCP project ID")
	location := flag.String("location", strings.TrimSpace(os.Getenv("AGENTMESH_VERTEX_LOCATION")), "Vertex location")
	timeout := flag.Duration("timeout", 20*time.Second, "Submit timeout")
	flag.Parse()

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
	logger := log.New(os.Stdout, "[tmp-probe-submit] ", log.LstdFlags|log.Lshortfile)
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

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			fmt.Println("submit context done:", ctx.Err())
			_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
		case <-done:
		}
	}()

	fmt.Println("before submit")
	taskID, err := submitter.Submit(ctx, "global", "scout.plan", map[string]any{
		"tenantId":      "global",
		"projectId":     "nv-horizon-test",
		"scoutingJobId": "probe-submit",
		"query":         "Study lithium supply routes from Chile into Germany for battery materials.",
		"allowWrites":   true,
	}, "scout.plan:probe-submit")
	close(done)
	fmt.Println("after submit")
	fmt.Printf("taskID=%s err=%v\n", taskID, err)
}

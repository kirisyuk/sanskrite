package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
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
	var (
		projectID   = flag.String("project", envOr("GCP_PROJECT_ID", "nv-horizon"), "GCP project ID")
		location    = flag.String("location", envOr("AGENTMESH_VERTEX_LOCATION", "global"), "Vertex model location")
		query       = flag.String("query", "", "Natural-language scouting query")
		target      = flag.String("target", "", "Optional canonical target path")
		tenantID    = flag.String("tenant", "global", "Tenant ID")
		projectRef  = flag.String("project-ref", "nv-horizon-test", "Logical Horizon project ID")
		lensID      = flag.String("lens", "", "Optional lens ID")
		allowWrites = flag.Bool("allow-writes", true, "Allow deterministic graph writes")
		runtimeMode = flag.String("runtime-mode", horizonpolicy.ScoutRuntimeAgentic, "Scout runtime mode")
		timeout     = flag.Duration("timeout", 14*time.Minute, "Execution timeout")
		pollEvery   = flag.Duration("poll-every", 15*time.Second, "Checkpoint poll interval")
	)
	flag.Parse()

	if strings.TrimSpace(*query) == "" && strings.TrimSpace(*target) == "" {
		log.Fatal("either --query or --target is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()
	gcs, err := gcp.NewGCSClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("gcs client: %v", err)
	}

	logger := log.New(os.Stdout, "[agentic-core-smoke] ", log.LstdFlags|log.Lshortfile)
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
	submitter := agentmeshsubmitter.NewService(runtimeSvc)
	agenticSvc := horizonscoutbridge.NewAgenticService(submitter, store, agentJobsRepo, scoutingJobsRepo, logger)
	executor := horizonscoutbridge.NewExecutor(nil, agenticSvc, logger)

	job := buildScoutingJob(*tenantID, *projectRef, *lensID, *query, *target, *runtimeMode, *allowWrites)
	jobID, err := scoutingJobsRepo.Create(ctx, job)
	if err != nil {
		log.Fatalf("create scouting job: %v", err)
	}
	job.ID = jobID
	logger.Printf("created scouting job id=%s mode=%s target=%s", job.ID, *runtimeMode, strings.TrimSpace(*target))

	done := make(chan error, 1)
	go func() {
		done <- executor.ExecuteScoutingJob(ctx, job)
	}()

	monitorRootTask(ctx, logger, scoutingJobsRepo, store, runtimeSvc, job.ID, *pollEvery)

	err = <-done
	finalJob, loadErr := scoutingJobsRepo.Get(context.Background(), job.ID)
	if loadErr != nil {
		log.Printf("reload scouting job failed: %v", loadErr)
	}
	finalTask := loadRootTask(context.Background(), store, finalJob)

	summary := map[string]any{
		"jobId":        job.ID,
		"jobStatus":    "",
		"currentState": "",
		"runtimeMode":  *runtimeMode,
		"error":        "",
	}
	if err != nil {
		summary["error"] = err.Error()
	}
	if finalJob != nil {
		summary["jobStatus"] = string(finalJob.Status)
		summary["currentState"] = stateString(finalJob.State, "currentState")
		summary["jobState"] = finalJob.State
	}
	if finalTask != nil {
		summary["taskId"] = finalTask.ID
		summary["taskStatus"] = finalTask.Status
		summary["reviewAccepted"] = finalTask.ReviewAccepted != nil && *finalTask.ReviewAccepted
		summary["lastError"] = strings.TrimSpace(finalTask.LastError)
		summary["usage"] = finalTask.Usage
		summary["output"] = finalTask.Output
	}

	raw, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(raw))

	if err != nil {
		os.Exit(1)
	}
}

func buildScoutingJob(tenantID, projectID, lensID, query, target, runtimeMode string, allowWrites bool) *horizonmodels.ScoutingJob {
	now := time.Now().UTC()
	identity := smokeIdentity(query, target)
	state := map[string]any{
		"runtimeMode":         strings.TrimSpace(runtimeMode),
		"allowAgenticWrites":  allowWrites,
		"originalQuery":       strings.TrimSpace(query),
		"targetCanonicalPath": strings.TrimSpace(target),
		"lensId":              strings.TrimSpace(lensID),
		"targetIdentityKey":   identity,
		"smokeRun":            true,
		"smokeStartedAt":      now.Format(time.RFC3339),
		"agenticScout": map[string]any{
			"intent":  "core_smoke",
			"request": "agentmesh_vertex_only",
		},
	}
	return &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			TenantID:  strings.TrimSpace(tenantID),
			ProjectID: strings.TrimSpace(projectID),
			AgentID:   "horizon_scout",
			Title:     smokeTitle(query, target),
			Status:    coremodels.AgentJobStatusPending,
			AutoMode:  true,
			UserQuery: strings.TrimSpace(query),
			PlanMode:  coremodels.PlanModeAuto,
			Task: map[string]any{
				"query":               strings.TrimSpace(query),
				"targetCanonicalPath": strings.TrimSpace(target),
			},
			State:     state,
			CreatedAt: now,
			UpdatedAt: now,
		},
		LensID:              strings.TrimSpace(lensID),
		TargetCanonicalPath: strings.TrimSpace(target),
		ScoutingStrategy:    string(horizonmodels.ScoutingStrategyDeepDive),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelBounty),
			MaxCost:     25,
			ProjectID:   strings.TrimSpace(projectID),
		},
		CurrentState: horizonmodels.ScoutingJobStatePending,
	}
}

func smokeIdentity(query, target string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(query) + "|" + strings.TrimSpace(target) + "|" + time.Now().UTC().Format(time.RFC3339Nano)))
	return "core-smoke:" + hex.EncodeToString(sum[:8])
}

func smokeTitle(query, target string) string {
	base := strings.TrimSpace(query)
	if base == "" {
		base = strings.TrimSpace(target)
	}
	if base == "" {
		base = "agentic scout core smoke"
	}
	if len(base) > 96 {
		base = base[:96]
	}
	return "Core scout smoke: " + base
}

func monitorRootTask(ctx context.Context, logger *log.Logger, scoutingJobsRepo *horizonrepo.ScoutingJobsRepository, store *agentmeshrepo.Store, runtimeSvc *agentmeshruntime.Service, jobID string, pollEvery time.Duration) {
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	lastSignature := ""
	lastTaskID := ""
	started := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			logger.Printf("monitor stopped: %v", ctx.Err())
			return
		case <-ticker.C:
			job, err := scoutingJobsRepo.Get(ctx, jobID)
			if err != nil || job == nil {
				if err != nil {
					logger.Printf("monitor job reload failed: %v", err)
				}
				continue
			}
			taskID := nestedString(job.State, "agenticScout", "taskId")
			if taskID == "" {
				taskID = stateString(job.State, "agenticScout.taskId")
			}
			if taskID == "" {
				logger.Printf("monitor job=%s waiting for root task submission status=%s state=%s", job.ID, job.Status, stateString(job.State, "currentState"))
				continue
			}
			if taskID != lastTaskID {
				lastTaskID = taskID
				logger.Printf("monitor job=%s rootTask=%s", job.ID, taskID)
			}
			task, err := store.GetTask(ctx, taskID)
			if err != nil || task == nil {
				if err != nil {
					logger.Printf("monitor task reload failed task=%s err=%v", taskID, err)
				}
				continue
			}
			if !started[task.ID] && task.Status == agentmeshcontracts.TaskStatusPending && runtimeSvc != nil {
				started[task.ID] = true
				logger.Printf("monitor starting local worker for root task=%s", task.ID)
				go func(id string) {
					if err := runtimeSvc.ExecuteTask(ctx, id, "core-smoke-inline"); err != nil && ctx.Err() == nil {
						logger.Printf("local worker failed task=%s err=%v", id, err)
					}
				}(task.ID)
			}
			line := checkpointLine(task)
			if line == "" {
				line = fmt.Sprintf("task=%s status=%s subtasks=%d modelCalls=%d spend=%.4f", task.ID, task.Status, len(task.Subtasks), task.Usage.ModelCalls, task.Usage.EstimatedSpendUSD)
			}
			signature := fmt.Sprintf("%s|%s|%d|%.4f", string(task.Status), line, task.Usage.ModelCalls, task.Usage.EstimatedSpendUSD)
			if signature == lastSignature {
				continue
			}
			lastSignature = signature
			logger.Printf("checkpoint %s", line)
			if isTerminalTaskStatus(task.Status) {
				return
			}
		}
	}
}

func checkpointLine(task *agentmeshcontracts.MeshTask) string {
	if task == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("task=%s", task.ID), fmt.Sprintf("status=%s", task.Status)}
	if checkpoint, ok := task.Output["checkpoint"].(map[string]any); ok {
		if turn, ok := checkpoint["turn"]; ok {
			parts = append(parts, fmt.Sprintf("turn=%v", turn))
		}
		if action := stringValue(checkpoint["action"]); action != "" {
			parts = append(parts, "action="+action)
		}
		if branchID := stringValue(checkpoint["branchId"]); branchID != "" {
			parts = append(parts, "branch="+branchID)
		}
		if bottleneck := stringValue(checkpoint["bottleneck"]); bottleneck != "" {
			parts = append(parts, "bottleneck="+bottleneck)
		}
		if stopReason := stringValue(checkpoint["stopReason"]); stopReason != "" {
			parts = append(parts, "stop="+stopReason)
		}
	}
	if summary := stringValue(task.Output["reasoningSummary"]); summary != "" {
		parts = append(parts, "summary="+truncate(summary, 180))
	}
	if refs, ok := task.Output["taskRefs"].(map[string]any); ok {
		keys := sortedNonEmptyKeys(refs)
		if len(keys) > 0 {
			parts = append(parts, "taskRefs="+strings.Join(keys, ","))
		}
	}
	parts = append(parts, fmt.Sprintf("modelCalls=%d", task.Usage.ModelCalls))
	parts = append(parts, fmt.Sprintf("spend=%.4f", task.Usage.EstimatedSpendUSD))
	return strings.Join(parts, " ")
}

func loadRootTask(ctx context.Context, store *agentmeshrepo.Store, job *horizonmodels.ScoutingJob) *agentmeshcontracts.MeshTask {
	if job == nil {
		return nil
	}
	taskID := nestedString(job.State, "agenticScout", "taskId")
	if taskID == "" {
		taskID = stateString(job.State, "agenticScout.taskId")
	}
	if taskID == "" {
		return nil
	}
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		return nil
	}
	return task
}

func nestedString(root map[string]any, path ...string) string {
	if len(path) == 0 || root == nil {
		return ""
	}
	current := any(root)
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = m[key]
		if !ok {
			return ""
		}
	}
	return stringValue(current)
}

func stateString(state map[string]any, key string) string {
	if state == nil {
		return ""
	}
	return stringValue(state[key])
}

func stringValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func sortedNonEmptyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if stringValue(v) == "" || stringValue(v) == "map[]" || stringValue(v) == "[]" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func isTerminalTaskStatus(status agentmeshcontracts.TaskStatus) bool {
	switch status {
	case agentmeshcontracts.TaskStatusSucceeded, agentmeshcontracts.TaskStatusFailed, agentmeshcontracts.TaskStatusCancelled, agentmeshcontracts.TaskStatusDegraded, agentmeshcontracts.TaskStatusAwaitingReview:
		return true
	default:
		return false
	}
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

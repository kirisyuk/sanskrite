package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	hscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID string
		lensID    string
		jobID     string
		timeout   time.Duration
	)

	flag.StringVar(&projectID, "project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon-test"), "GCP project id")
	flag.StringVar(&lensID, "lens-id", "", "lens id to reconcile")
	flag.StringVar(&jobID, "job-id", "", "optional stuck scout job id to force-fail before reconcile")
	flag.DurationVar(&timeout, "timeout", 2*time.Minute, "reconcile timeout")
	flag.Parse()

	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	jobID = strings.TrimSpace(jobID)
	if projectID == "" {
		log.Fatal("--project is required")
	}
	if lensID == "" {
		log.Fatal("--lens-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	agentJobsRepo := corerepo.NewAgentJobsRepository(fs.Client())
	scoutingJobsRepo := hrepo.NewScoutingJobsRepository(agentJobsRepo)
	lensesRepo := hrepo.NewLensesRepository(fs.Client())

	lens, err := lensesRepo.Get(ctx, lensID)
	if err != nil {
		log.Fatalf("load lens failed: %v", err)
	}

	if jobID != "" {
		job, err := scoutingJobsRepo.Get(ctx, jobID)
		if err != nil {
			log.Fatalf("load scouting job failed: %v", err)
		}
		if job == nil {
			log.Fatalf("job %s not found", jobID)
		}
		fmt.Printf("job before: id=%s status=%s state=%s updatedAt=%s\n", job.ID, strings.TrimSpace(string(job.Status)), strings.TrimSpace(string(job.CurrentState)), job.UpdatedAt.UTC().Format(time.RFC3339Nano))
		if !isTerminal(job) {
			workflow := hscout.NewScoutWorkflow(agentJobsRepo).WithScoutingJobsRepo(scoutingJobsRepo)
			if err := workflow.TransitionState(ctx, job.ID, hmodels.ScoutingJobStateFailed); err != nil {
				log.Fatalf("transition failed: %v", err)
			}
			job, _ = scoutingJobsRepo.Get(ctx, jobID)
			if job != nil {
				fmt.Printf("job after: id=%s status=%s state=%s updatedAt=%s\n", job.ID, strings.TrimSpace(string(job.Status)), strings.TrimSpace(string(job.CurrentState)), job.UpdatedAt.UTC().Format(time.RFC3339Nano))
			}
		}
	}

	engine := hscout.NewScoutEngine(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		hscout.NewScoutWorkflow(agentJobsRepo),
		nil,
		nil,
		nil,
		scoutingJobsRepo,
		nil,
		nil,
	).WithLensesRepository(lensesRepo)

	engine.ReconcileLensAssemblyCompletion(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ProjectID), lens.ID)

	fresh, err := lensesRepo.Get(ctx, lens.ID)
	if err != nil {
		log.Fatalf("reload lens failed: %v", err)
	}
	body, _ := json.MarshalIndent(map[string]any{
		"assemblyStatus":  readViewConfigMap(fresh, "assemblyStatus"),
		"internalKnowledge": readViewConfigMap(fresh, "internalKnowledge"),
	}, "", "  ")
	fmt.Println(string(body))
}

func readViewConfigMap(lens *hmodels.Lens, key string) map[string]any {
	if lens == nil || lens.ViewConfig == nil {
		return map[string]any{}
	}
	if typed, ok := lens.ViewConfig[strings.TrimSpace(key)].(map[string]any); ok && typed != nil {
		return typed
	}
	return map[string]any{}
}

func isTerminal(job *hmodels.ScoutingJob) bool {
	if job == nil {
		return false
	}
	switch job.Status {
	case coremodels.AgentJobStatusCompleted, coremodels.AgentJobStatusFailed, coremodels.AgentJobStatusPartial:
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

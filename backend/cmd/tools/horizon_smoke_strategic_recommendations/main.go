package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrecommendations "github.com/igorkirisyuk/sanskrite/internal/horizon/recommendations"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizontasks "github.com/igorkirisyuk/sanskrite/internal/horizon/tasks"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID     = flag.String("project", "nv-horizon", "GCP project id")
		tenantID      = flag.String("tenant", "global", "tenant id")
		limit         = flag.Int("limit", 10, "max recommendations to list")
		reconcile     = flag.Bool("reconcile", false, "reconcile live recommendations before listing")
		reconcileID   = flag.String("reconcile-id", "", "reconcile a specific recommendation id before listing")
		approveID     = flag.String("approve", "", "recommendation id to approve")
		approveLatest = flag.Bool("approve-latest", false, "approve the most recently updated recommendation")
		userID        = flag.String("user", "system-smoke", "user id used for approve flow")
		enqueueLive   = flag.Bool("enqueue-live", false, "attach real Cloud Tasks enqueue hook to scouting job creation")
		tasksLocation = flag.String("tasks-location", "us-central1", "Cloud Tasks location")
		tasksQueue    = flag.String("tasks-queue", "nv-horizon-scout", "Cloud Tasks scout queue")
		tasksTarget   = flag.String("tasks-target-url", "https://nv-horizon-api-onq73n4gpa-uc.a.run.app/api/v1/internal/tasks/scout/run", "Cloud Tasks target URL")
		tasksSA       = flag.String("tasks-service-account", "backend@nv-horizon.iam.gserviceaccount.com", "OIDC service account for Cloud Tasks")
		tasksAudience = flag.String("tasks-audience", "https://nv-horizon-api-onq73n4gpa-uc.a.run.app", "OIDC audience for Cloud Tasks")
	)
	flag.Parse()

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatal(err)
	}
	defer fsClient.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
	recommendationsRepo := horizonrepo.NewStrategicRecommendationsRepository(fsClient.Client())
	agentJobsRepo := corerepo.NewAgentJobsRepository(fsClient.Client())
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	if *enqueueLive {
		dispatcher, err := horizontasks.NewScoutCloudTasksDispatcher(
			ctx,
			strings.TrimSpace(*projectID),
			strings.TrimSpace(*tasksLocation),
			strings.TrimSpace(*tasksQueue),
			strings.TrimSpace(*tasksTarget),
			strings.TrimSpace(*tasksSA),
			strings.TrimSpace(*tasksAudience),
		)
		if err != nil {
			log.Fatal(err)
		}
		defer dispatcher.Close()
		scoutingJobsRepo.WithOnCreate(func(c context.Context, job *horizonmodels.ScoutingJob) error {
			return dispatcher.EnqueueScoutJob(c, job.ID)
		})
	}
	service := horizonrecommendations.NewService(
		recommendationsRepo,
		fragmentsRepo,
		edgesRepo,
		scoutingJobsRepo,
		nil,
		nil,
		log.New(os.Stdout, "[strategic-rec-smoke] ", log.LstdFlags),
	)

	if *reconcile {
		reconciled, stale, err := service.ReconcileTenant(ctx, strings.TrimSpace(*tenantID), *limit)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("reconciled=%d stale=%d tenant=%s\n", reconciled, stale, strings.TrimSpace(*tenantID))
	}
	if id := strings.TrimSpace(*reconcileID); id != "" {
		rec, superseded, err := service.Reconcile(ctx, id)
		if err != nil {
			log.Fatal(err)
		}
		if rec == nil {
			fmt.Printf("reconcile.id=%s result=nil superseded=%t\n", id, superseded)
		} else {
			fmt.Printf("reconcile.id=%s status=%s superseded=%t subject=%s updated=%s\n",
				id,
				rec.Status,
				superseded,
				strings.TrimSpace(rec.SubjectCanonicalPath),
				rec.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			)
		}
	}

	recs, err := service.List(ctx, strings.TrimSpace(*tenantID), horizonrepo.StrategicRecommendationsListFilter{Limit: *limit})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("tenant=%s recommendations=%d\n", strings.TrimSpace(*tenantID), len(recs))
	for _, rec := range recs {
		if rec == nil {
			continue
		}
		fmt.Printf("%s status=%s priority=%s confidence=%.2f subject=%s missing=%s updated=%s\n",
			strings.TrimSpace(rec.ID),
			rec.Status,
			rec.Priority,
			rec.Confidence,
			strings.TrimSpace(rec.SubjectCanonicalPath),
			strings.Join(rec.ExpectedMissingTargets, ","),
			rec.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		)
	}

	targetID := strings.TrimSpace(*approveID)
	if targetID == "" && *approveLatest && len(recs) > 0 && recs[0] != nil {
		targetID = strings.TrimSpace(recs[0].ID)
	}
	if targetID == "" {
		return
	}
	if !*enqueueLive {
		log.Fatal("approve flow requires --enqueue-live; otherwise the tool creates pending scouting jobs without Cloud Tasks dispatch")
	}

	approved, err := service.Approve(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*userID), targetID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("approved=%s status=%s jobId=%s stage=%s gapStillOpen=%t\n",
		strings.TrimSpace(approved.ID),
		approved.Status,
		strings.TrimSpace(approved.Execution.JobID),
		strings.TrimSpace(approved.Execution.JobStage),
		approved.Execution.GapStillOpen,
	)
	if strings.TrimSpace(approved.Execution.JobID) == "" {
		return
	}
	job, err := scoutingJobsRepo.Get(ctx, strings.TrimSpace(approved.Execution.JobID))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("job=%s strategy=%s target=%s project=%s\n",
		strings.TrimSpace(job.ID),
		strings.TrimSpace(string(job.ScoutingStrategy)),
		strings.TrimSpace(job.TargetCanonicalPath),
		strings.TrimSpace(job.ProjectID),
	)
	fmt.Printf("job.status=%s currentState=%s updated=%s\n",
		strings.TrimSpace(string(job.Status)),
		strings.TrimSpace(string(job.CurrentState)),
		job.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	)
	if job.State != nil {
		fmt.Printf("job.domainHint=%v\n", job.State["strategicDomainContractHint"])
		fmt.Printf("job.anchorTerms=%v\n", job.State["strategicAnchorTerms"])
		fmt.Printf("job.domains=%v\n", job.State["domains"])
		fmt.Printf("job.keywords=%v\n", job.State["keywords"])
	}
}

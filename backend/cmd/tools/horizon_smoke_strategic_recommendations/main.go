package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonrecommendations "github.com/igorkirisyuk/sanskrite/internal/horizon/recommendations"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID     = flag.String("project", "etigram", "GCP project id")
		tenantID      = flag.String("tenant", "global", "tenant id")
		limit         = flag.Int("limit", 10, "max recommendations to list")
		reconcile     = flag.Bool("reconcile", false, "reconcile live recommendations before listing")
		reconcileID   = flag.String("reconcile-id", "", "reconcile a specific recommendation id before listing")
		approveID     = flag.String("approve", "", "recommendation id to approve")
		approveLatest = flag.Bool("approve-latest", false, "approve the most recently updated recommendation")
		userID        = flag.String("user", "system-smoke", "user id used for approve flow")
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
	if job.State != nil {
		fmt.Printf("job.domainHint=%v\n", job.State["strategicDomainContractHint"])
		fmt.Printf("job.anchorTerms=%v\n", job.State["strategicAnchorTerms"])
		fmt.Printf("job.domains=%v\n", job.State["domains"])
		fmt.Printf("job.keywords=%v\n", job.State["keywords"])
	}
}

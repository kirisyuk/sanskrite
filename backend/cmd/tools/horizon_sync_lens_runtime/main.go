package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonlensruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/lensruntime"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscopeagent "github.com/igorkirisyuk/sanskrite/internal/horizon/scopeagent"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID := flag.String("tenant", "", "tenant id (empty = all active lenses)")
	includeGlobal := flag.Bool("include-global", true, "include global active lenses when tenant is set")
	limit := flag.Int("limit", 100, "max number of active lenses to scan")
	lensID := flag.String("lens-id", "", "sync only a single lens")
	flag.Parse()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	lensesRepo := horizonrepo.NewLensesRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	materializedRepo := horizonrepo.NewMaterializedArtifactsRepository(db)
	targetsRepo := horizonrepo.NewWatchTargetsRepository(db)
	dataSourcesRepo := corerepo.NewDataSourcesRepository(db)
	watchRuntime := horizonwatchruntime.NewService(
		horizonrepo.NewWatchSubjectsRepository(db),
		horizonrepo.NewWatchTriggersRepository(db),
		targetsRepo,
		horizonrepo.NewWatchdogConfigsRepository(db),
		horizonrepo.NewScopeAgentConfigsRepository(db),
		lensesRepo,
		horizonrepo.NewLensArtifactsRepository(db),
		fragmentsRepo,
		horizonrepo.NewDependencyEdgesRepository(db),
	).WithMaterializedArtifactsRepository(materializedRepo).
		WithSignalLandingSummariesRepository(horizonrepo.NewSignalLandingSummariesRepository(db)).
		WithMacroAgentPoliciesRepository(horizonrepo.NewMacroAgentPoliciesRepository(db))
	scopeAgent := horizonscopeagent.NewService(
		horizonrepo.NewScopeAgentConfigsRepository(db),
		horizonrepo.NewScopeAgentStatesRepository(db),
		horizonrepo.NewScopeAgentRunsRepository(db),
		horizonrepo.NewAgentProposalsRepository(db),
		horizonrepo.NewArbitrationDecisionsRepository(db),
		targetsRepo,
		horizonrepo.NewWatchSubjectsRepository(db),
		horizonrepo.NewWatchTriggersRepository(db),
		horizonrepo.NewWatchBriefsRepository(db),
		horizonrepo.NewWatchFeedEntriesRepository(db),
		corerepo.NewAgentJobsRepository(db),
		horizonrepo.NewWatchdogConfigsRepository(db),
		fragmentsRepo,
		materializedRepo,
		horizonrepo.NewRiskJobsRepository(db),
		corerepo.NewOntologyFactsRepository(db),
		dataSourcesRepo,
		nil,
		nil,
		nil,
	)
	service := horizonlensruntime.NewService(
		lensesRepo,
		fragmentsRepo,
		materializedRepo,
		targetsRepo,
		dataSourcesRepo,
		watchRuntime,
		scopeAgent,
	)

	if strings.TrimSpace(*lensID) != "" {
		result, err := service.SyncLens(ctx, strings.TrimSpace(*lensID))
		if err != nil {
			log.Fatalf("sync lens failed: %v", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
		return
	}

	report, err := service.SyncAllActiveLenses(ctx, strings.TrimSpace(*tenantID), *includeGlobal, *limit)
	if err != nil {
		log.Fatalf("sync active lenses failed: %v", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

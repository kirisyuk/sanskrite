package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonwatch "github.com/igorkirisyuk/sanskrite/internal/horizon/watch"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const bootstrapPackID = "bootstrap-global-focus-pack-v1"

var expectedTargets = map[string]string{
	"Ukraine":      "/loc/ua",
	"Saudi Arabia": "/loc/sa",
	"Tesla":        "/org/us/tesla",
	"Apple":        "/org/us/apple",
}

func main() {
	ctx := context.Background()

	project := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
	tenantID := flag.String("tenant", "global", "Tenant id")
	flag.Parse()

	if strings.TrimSpace(*project) == "" {
		*project = "etigram"
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *project)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	watchPacksRepo := horizonrepo.NewWatchPacksRepository(db)
	watchTargetsRepo := horizonrepo.NewWatchTargetsRepository(db)
	watchBriefsRepo := horizonrepo.NewWatchBriefsRepository(db)
	watchFeedRepo := horizonrepo.NewWatchFeedEntriesRepository(db)
	watchPrefsRepo := horizonrepo.NewWatchPreferencesRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	riskJobsRepo := horizonrepo.NewRiskJobsRepository(db)
	proposalsRepo := horizonrepo.NewAgentProposalsRepository(db)
	decisionsRepo := horizonrepo.NewArbitrationDecisionsRepository(db)
	scopeConfigsRepo := horizonrepo.NewScopeAgentConfigsRepository(db)
	scopeStatesRepo := horizonrepo.NewScopeAgentStatesRepository(db)
	watchdogRepo := horizonrepo.NewWatchdogConfigsRepository(db)
	watchSubjectsRepo := horizonrepo.NewWatchSubjectsRepository(db)
	macroPoliciesRepo := horizonrepo.NewMacroAgentPoliciesRepository(db)
	factsRepo := corerepo.NewOntologyFactsRepository(db)

	pack, err := watchPacksRepo.Get(ctx, bootstrapPackID)
	if err != nil {
		log.Fatalf("watch pack %q not found: %v", bootstrapPackID, err)
	}
	log.Printf("PACK ok id=%s tenant=%s name=%q status=%s", pack.ID, pack.TenantID, pack.Name, pack.Status)

	targets, err := watchTargetsRepo.ListByPack(ctx, *tenantID, bootstrapPackID)
	if err != nil {
		log.Fatalf("list watch targets by pack failed: %v", err)
	}
	if len(targets) == 0 {
		log.Fatalf("no watch targets found for pack=%s tenant=%s", bootstrapPackID, *tenantID)
	}

	found := map[string]string{}
	for _, target := range targets {
		if target == nil {
			continue
		}
		found[target.Label] = target.CanonicalPath

		if strings.TrimSpace(target.FragmentID) == "" {
			log.Fatalf("target %s has empty fragmentId", target.Label)
		}
		if strings.TrimSpace(target.AgentConfigID) == "" {
			log.Fatalf("target %s has empty agentConfigId", target.Label)
		}
		if strings.TrimSpace(target.LastBriefID) == "" {
			log.Fatalf("target %s has empty lastBriefId", target.Label)
		}

		cfg, err := scopeConfigsRepo.Get(ctx, target.AgentConfigID)
		if err != nil {
			log.Fatalf("scope agent config missing for %s: %v", target.Label, err)
		}
		policies, err := macroPoliciesRepo.ListByTenant(ctx, target.TenantID, 200)
		if err != nil {
			log.Fatalf("macro policies list failed for %s: %v", target.Label, err)
		}
		var macroPolicyID string
		for _, policy := range policies {
			if policy == nil {
				continue
			}
			if strings.TrimSpace(policy.AnchorCanonicalPath) == strings.TrimSpace(target.CanonicalPath) {
				macroPolicyID = strings.TrimSpace(policy.ID)
				break
			}
		}
		if macroPolicyID == "" {
			log.Fatalf("macro policy missing for %s canonicalPath=%s", target.Label, target.CanonicalPath)
		}
		state, err := scopeStatesRepo.GetByConfigID(ctx, cfg.ID)
		if err != nil {
			log.Fatalf("scope agent state missing for %s: %v", target.Label, err)
		}
		brief, err := watchBriefsRepo.Get(ctx, target.LastBriefID)
		if err != nil {
			log.Fatalf("watch brief missing for %s: %v", target.Label, err)
		}
		feed, err := watchFeedRepo.ListByTarget(ctx, target.TenantID, target.ID, 3)
		if err != nil {
			log.Fatalf("watch feed list failed for %s: %v", target.Label, err)
		}
		if _, err := fragmentsRepo.Get(ctx, target.FragmentID); err != nil {
			log.Fatalf("fragment missing for %s: %v", target.Label, err)
		}
		if _, err := watchdogRepo.GetByTargetFragmentID(ctx, target.TenantID, target.FragmentID); err != nil {
			log.Fatalf("watchdog config missing for %s: %v", target.Label, err)
		}
		subjects, err := watchSubjectsRepo.ListByAnchorCanonicalPath(ctx, target.TenantID, target.CanonicalPath, 20)
		if err != nil {
			log.Fatalf("watch subjects lookup failed for %s: %v", target.Label, err)
		}
		foundMacroSubject := false
		for _, subject := range subjects {
			if subject == nil {
				continue
			}
			if strings.TrimSpace(fmt.Sprint(subject.Metadata["macroAgentPolicyId"])) == macroPolicyID {
				foundMacroSubject = true
				break
			}
		}
		if !foundMacroSubject {
			log.Fatalf("compiled macro watch subject missing for %s policy=%s", target.Label, macroPolicyID)
		}

		proposals, _ := proposalsRepo.ListByWatchTarget(ctx, target.TenantID, target.ID, 5)
		log.Printf(
			"TARGET ok label=%q canonicalPath=%s fragmentId=%s agentConfigId=%s macroPolicyId=%s briefId=%s proposals=%d feedEntries=%d watchpoints=%d coverageGaps=%d",
			target.Label,
			target.CanonicalPath,
			target.FragmentID,
			cfg.ID,
			macroPolicyID,
			brief.ID,
			len(proposals),
			len(feed),
			len(state.OpenWatchpoints),
			len(state.CoverageGaps),
		)
	}

	for label, canonicalPath := range expectedTargets {
		got, ok := found[label]
		if !ok {
			log.Fatalf("expected target %q not found in pack", label)
		}
		if strings.TrimSpace(got) != strings.TrimSpace(canonicalPath) {
			log.Fatalf("target %q canonicalPath mismatch: got=%s want=%s", label, got, canonicalPath)
		}
	}

	watchService := horizonwatch.NewService(
		watchTargetsRepo,
		watchBriefsRepo,
		watchFeedRepo,
		watchPrefsRepo,
		fragmentsRepo,
		riskJobsRepo,
		proposalsRepo,
		decisionsRepo,
		factsRepo,
	)

	dashboard, err := watchService.GetDashboard(ctx, *tenantID, "", "combined")
	if err != nil {
		log.Fatalf("watch dashboard build failed: %v", err)
	}
	log.Printf(
		"DASHBOARD ok mode=%s globalHotSlices=%d myWatch=%d feed=%d",
		dashboard.Mode,
		len(dashboard.GlobalHotSlices),
		len(dashboard.MyWatch),
		len(dashboard.Feed),
	)

	for _, target := range targets {
		if target == nil || strings.TrimSpace(target.LastBriefID) == "" {
			continue
		}
		evidence, err := watchService.GetBriefEvidence(ctx, target.LastBriefID)
		if err != nil {
			log.Fatalf("brief evidence failed for %s: %v", target.Label, err)
		}
		paths, err := watchService.GetBriefPaths(ctx, target.LastBriefID)
		if err != nil {
			log.Fatalf("brief paths failed for %s: %v", target.Label, err)
		}
		sources, err := watchService.GetBriefSources(ctx, target.LastBriefID)
		if err != nil {
			log.Fatalf("brief sources failed for %s: %v", target.Label, err)
		}
		log.Printf(
			"BRIEF ok label=%q evidenceItems=%d pathCount=%d sourceCount=%d",
			target.Label,
			countList(evidence["evidence"]),
			countList(paths["canonicalPaths"]),
			countList(sources["sources"]),
		)
	}

	fmt.Println("VERIFY WATCH BOOTSTRAP: OK")
}

func countList(v any) int {
	switch typed := v.(type) {
	case []any:
		return len(typed)
	case []string:
		return len(typed)
	case []map[string]any:
		return len(typed)
	default:
		return 0
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	packID        = "bootstrap-global-focus-pack-v1"
	projectID     = "horizon-watch-bootstrap-global"
	bootstrapUser = "horizon-watch-bootstrap@system.local"
)

type targetSeed struct {
	key           string
	label         string
	kind          string
	scopeType     string
	canonicalPath string
	preferredPath string
	aliases       []string
	domainModes   []string
	roles         []string
	priority      int
	description   string
}

func main() {
	ctx := context.Background()

	project := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
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

	usersRepo := corerepo.NewUsersRepository(db)
	projectsRepo := corerepo.NewProjectsRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	redirectsRepo := horizonrepo.NewCanonicalPathRedirectsRepository(db)
	watchPacksRepo := horizonrepo.NewWatchPacksRepository(db)
	watchTargetsRepo := horizonrepo.NewWatchTargetsRepository(db)
	watchBriefsRepo := horizonrepo.NewWatchBriefsRepository(db)
	watchFeedRepo := horizonrepo.NewWatchFeedEntriesRepository(db)
	watchdogRepo := horizonrepo.NewWatchdogConfigsRepository(db)
	scopeAgentConfigsRepo := horizonrepo.NewScopeAgentConfigsRepository(db)
	scopeAgentStatesRepo := horizonrepo.NewScopeAgentStatesRepository(db)

	if err := ensureBootstrapProject(ctx, usersRepo, projectsRepo); err != nil {
		log.Fatalf("bootstrap project failed: %v", err)
	}
	if err := watchPacksRepo.Upsert(ctx, &horizonmodels.WatchPack{
		ID:          packID,
		TenantID:    "global",
		Name:        "Bootstrap Global Focus Pack v1",
		Description: "Manual watch bootstrap for Ukraine, Saudi Arabia, Tesla, and Apple.",
		Status:      string(horizonmodels.WatchTargetStatusActive),
	}); err != nil {
		log.Fatalf("watch pack upsert failed: %v", err)
	}

	seeds := []targetSeed{
		{
			key:           "ukraine",
			label:         "Ukraine",
			kind:          "country",
			scopeType:     "country",
			canonicalPath: "/loc/ua",
			domainModes:   []string{"conflict", "governance"},
			roles:         []string{"monitor", "risk", "forecast"},
			priority:      95,
			description:   "Country watch for conflict and governance pressure in Ukraine.",
		},
		{
			key:           "saudi-arabia",
			label:         "Saudi Arabia",
			kind:          "country",
			scopeType:     "country",
			canonicalPath: "/loc/sa",
			domainModes:   []string{"governance", "macro", "energy"},
			roles:         []string{"monitor", "risk", "forecast"},
			priority:      88,
			description:   "Country watch for governance, macro and energy-sensitive pressure in Saudi Arabia.",
		},
		{
			key:           "tesla",
			label:         "Tesla",
			kind:          "fragment",
			scopeType:     "entity",
			preferredPath: "/org/us/tesla-inc",
			aliases:       []string{"/org/tesla", "/org/us/tesla", "/org/us/tesla-inc", "/org/tesla/inc"},
			domainModes:   []string{"supply_chain", "market", "regulatory"},
			roles:         []string{"monitor", "risk", "forecast"},
			priority:      90,
			description:   "Entity watch for Tesla across supply-chain, market and regulatory exposure.",
		},
		{
			key:           "apple",
			label:         "Apple",
			kind:          "fragment",
			scopeType:     "entity",
			preferredPath: "/org/us/apple-inc",
			aliases:       []string{"/org/apple", "/org/us/apple", "/org/us/apple-inc"},
			domainModes:   []string{"supply_chain", "technology", "regulatory"},
			roles:         []string{"monitor", "risk", "forecast"},
			priority:      89,
			description:   "Entity watch for Apple across supply-chain, technology and regulatory exposure.",
		},
	}

	for _, seed := range seeds {
		fragment, resolvedPath, resolutionMeta, err := ensureFragment(ctx, fragmentsRepo, redirectsRepo, seed)
		if err != nil {
			log.Fatalf("fragment bootstrap failed for %s: %v", seed.key, err)
		}
		if fragment.Governance.CreatedByProjectID == nil || strings.TrimSpace(*fragment.Governance.CreatedByProjectID) == "" {
			projectRef := projectID
			fragment.Governance.CreatedByProjectID = &projectRef
			if err := fragmentsRepo.UpdateDerived(ctx, fragment); err != nil {
				log.Fatalf("failed to bind fragment %s to bootstrap project: %v", fragment.CanonicalPath, err)
			}
		}

		target := &horizonmodels.WatchTarget{
			TenantID:      "global",
			PackID:        packID,
			Kind:          seed.kind,
			Label:         seed.label,
			CanonicalPath: resolvedPath,
			FragmentID:    fragment.ID,
			DomainModes:   seed.domainModes,
			Roles:         seed.roles,
			Priority:      seed.priority,
			Status:        string(horizonmodels.WatchTargetStatusActive),
			Source:        "bootstrap_manual",
			Metadata: map[string]any{
				"bootstrapKey": seed.key,
				"description":  seed.description,
				"resolution":   resolutionMeta,
			},
		}
		if err := watchTargetsRepo.Upsert(ctx, target); err != nil {
			log.Fatalf("watch target upsert failed for %s: %v", seed.key, err)
		}

		cfg := &horizonmodels.ScopeAgentConfig{
			TenantID:           "global",
			PackID:             packID,
			WatchTargetID:      target.ID,
			TargetFragmentID:   fragment.ID,
			Name:               fmt.Sprintf("%s focused scope agent", seed.label),
			ScopeCanonicalPath: resolvedPath,
			ScopeType:          seed.scopeType,
			DomainModes:        seed.domainModes,
			Roles:              seed.roles,
			Priority:           seed.priority,
			Status:             string(horizonmodels.ScopeAgentStatusRunning),
			ExpansionPolicy: map[string]any{
				"focusMode":      "canonical_scope",
				"allowOverlaps":  true,
				"centralArbiter": true,
			},
			Schedule: map[string]any{
				"debounceSeconds":    180,
				"batchWindowSeconds": 180,
			},
			LLMPolicy: horizonmodels.LLMPolicy{
				Provider:              "gemini",
				PrimaryModel:          "gemini-3-flash-preview",
				EscalationModel:       "gemini-3-pro-preview",
				FallbackModels:        []string{"gemini-2.5-flash", "gemini-2.5-pro"},
				PreferredVertexModel:  "vertex-gemini-3-flash-preview",
				UseSearchGrounding:    true,
				UseDatastoreGrounding: true,
				BatchWindowSeconds:    180,
				MaxBatchSize:          8,
			},
			Metadata: map[string]any{
				"bootstrapKey": seed.key,
				"resolvedPath": resolvedPath,
			},
		}
		if err := scopeAgentConfigsRepo.Upsert(ctx, cfg); err != nil {
			log.Fatalf("scope agent config upsert failed for %s: %v", seed.key, err)
		}

		state := &horizonmodels.ScopeAgentState{
			TenantID:        "global",
			ConfigID:        cfg.ID,
			WatchTargetID:   target.ID,
			Baseline:        map[string]any{"hotness": 0.2, "confidence": 0.35},
			OpenWatchpoints: seed.domainModes,
			CoverageGaps:    []string{"historical_baseline", "scenario_envelope"},
		}
		if err := scopeAgentStatesRepo.Upsert(ctx, state); err != nil {
			log.Fatalf("scope agent state upsert failed for %s: %v", seed.key, err)
		}

		now := time.Now().UTC()
		brief := &horizonmodels.WatchBrief{
			TenantID:       "global",
			TargetID:       target.ID,
			CanonicalPath:  resolvedPath,
			Label:          seed.label,
			ScopeType:      seed.kind,
			Headline:       fmt.Sprintf("%s bootstrap watch active", seed.label),
			Summary:        seed.description,
			Hotness:        0.2,
			Confidence:     0.35,
			RiskPosture:    "baseline",
			DominantThemes: seed.domainModes,
			Explanation:    "Bootstrap placeholder brief. Runtime refresh will replace this with live watchdog/risk/arbiter output.",
			Forecast: map[string]any{
				"watchpoints": seed.domainModes,
				"status":      "bootstrap_placeholder",
			},
			Metadata: map[string]any{
				"bootstrap":   true,
				"generatedAt": now.Format(time.RFC3339),
			},
		}
		if err := watchBriefsRepo.Upsert(ctx, brief); err != nil {
			log.Fatalf("watch brief upsert failed for %s: %v", seed.key, err)
		}
		if _, err := watchFeedRepo.Create(ctx, &horizonmodels.WatchFeedEntry{
			TenantID:       "global",
			TargetID:       target.ID,
			BriefID:        brief.ID,
			EntryType:      "bootstrap",
			Title:          fmt.Sprintf("%s bootstrap watch created", seed.label),
			Summary:        "Initial manual watch target, agent config and placeholder brief were created.",
			CanonicalPaths: []string{resolvedPath},
			Metadata: map[string]any{
				"bootstrapKey": seed.key,
				"packId":       packID,
			},
		}); err != nil {
			log.Fatalf("watch feed create failed for %s: %v", seed.key, err)
		}
		target.LastBriefID = brief.ID
		target.AgentConfigID = cfg.ID
		if err := watchTargetsRepo.Upsert(ctx, target); err != nil {
			log.Fatalf("watch target final update failed for %s: %v", seed.key, err)
		}

		if err := ensureWatchdogConfig(ctx, watchdogRepo, fragment.ID); err != nil {
			log.Fatalf("watchdog config ensure failed for %s: %v", seed.key, err)
		}

		log.Printf("BOOTSTRAPPED key=%s canonicalPath=%s fragmentId=%s targetId=%s agentConfigId=%s", seed.key, resolvedPath, fragment.ID, target.ID, cfg.ID)
	}
}

func ensureBootstrapProject(ctx context.Context, usersRepo *corerepo.UsersRepository, projectsRepo *corerepo.ProjectsRepository) error {
	if err := usersRepo.Upsert(ctx, &coremodels.User{
		ID:       bootstrapUser,
		TenantID: "global",
		Email:    bootstrapUser,
	}); err != nil {
		return err
	}
	projectType := coremodels.ProjectTypeHorizon
	return projectsRepo.Upsert(ctx, &coremodels.Project{
		ID:          projectID,
		Pk:          "global",
		TenantID:    "global",
		DisplayName: "Horizon Watch Bootstrap Global",
		Status:      "active",
		OwnerUserID: bootstrapUser,
		ProjectType: &projectType,
	})
}

func ensureFragment(
	ctx context.Context,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	redirectsRepo *horizonrepo.CanonicalPathRedirectsRepository,
	seed targetSeed,
) (*horizonmodels.HorizonFragment, string, map[string]any, error) {
	candidates := []string{}
	if seed.canonicalPath != "" {
		candidates = append(candidates, seed.canonicalPath)
	}
	if seed.preferredPath != "" {
		candidates = append(candidates, seed.preferredPath)
	}
	candidates = append(candidates, seed.aliases...)
	seen := map[string]struct{}{}
	resolutionMeta := map[string]any{
		"requestedCandidates": compactStrings(candidates),
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if redirect, err := redirectsRepo.Get(ctx, "global", candidate); err == nil && redirect != nil && strings.TrimSpace(redirect.TargetPath) != "" {
			candidate = strings.TrimSpace(redirect.TargetPath)
			resolutionMeta["redirectedFrom"] = appendInterfaceString(resolutionMeta["redirectedFrom"], redirect.SourcePath)
		}
		if fragment, err := fragmentsRepo.GetByCanonicalPath(ctx, candidate); err == nil && fragment != nil {
			resolutionMeta["resolvedExisting"] = candidate
			return fragment, candidate, resolutionMeta, nil
		}
	}

	targetPath := firstNonEmpty(seed.canonicalPath, seed.preferredPath)
	if targetPath == "" {
		return nil, "", nil, fmt.Errorf("no canonical path for seed %s", seed.key)
	}
	now := time.Now().UTC()
	projectRef := projectID
	fragment := &horizonmodels.HorizonFragment{
		TenantID:      "global",
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
		CreatedBy:     "horizon_bootstrap_watch",
		UpdatedBy:     "horizon_bootstrap_watch",
		Labels:        map[string]string{"en": seed.label},
		Description:   seed.description,
		Tags:          []string{"bootstrap", "watch"},
		Status:        "active",
		CanonicalPath: targetPath,
		Type:          inferFragmentType(seed.scopeType),
		State:         map[string]horizonmodels.MetricValue{},
		Metadata: horizonmodels.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			EpistemicStatus: horizonmodels.EpistemicStatusLowConfidence,
		},
		Governance: horizonmodels.Governance{
			Owner:              "SYSTEM",
			ActiveSubscribers:  0,
			Status:             string(horizonmodels.FragmentStatusActive),
			CreatedByProjectID: &projectRef,
		},
	}
	if fragment.Type == horizonmodels.FragmentTypeOrganization {
		fragment.EntityPath = targetPath
	}
	fragmentID, err := fragmentsRepo.Create(ctx, fragment)
	if err != nil {
		return nil, "", nil, err
	}
	fragment.ID = fragmentID
	resolutionMeta["created"] = true
	return fragment, targetPath, resolutionMeta, nil
}

func ensureWatchdogConfig(ctx context.Context, watchdogRepo *horizonrepo.WatchdogConfigsRepository, fragmentID string) error {
	now := time.Now().UTC()
	nextScanAt := now.Add(-1 * time.Minute)
	lastScanAt := now.Add(-2 * time.Hour)
	cfg, err := watchdogRepo.GetByTargetFragmentID(ctx, "global", fragmentID)
	if err != nil || cfg == nil {
		cfg = &horizonmodels.WatchdogConfig{
			TenantID:         "global",
			TargetFragmentID: fragmentID,
			ScanInterval:     "PT1H",
			TriggerThreshold: 0.10,
			Status:           string(horizonmodels.WatchdogStatusRunning),
			LastScanAt:       &lastScanAt,
			NextScanAt:       &nextScanAt,
		}
		_, err = watchdogRepo.Create(ctx, cfg)
		return err
	}
	cfg.Status = string(horizonmodels.WatchdogStatusRunning)
	cfg.TriggerThreshold = 0.10
	cfg.ScanInterval = "PT1H"
	cfg.LastScanAt = &lastScanAt
	cfg.NextScanAt = &nextScanAt
	return watchdogRepo.Update(ctx, cfg)
}

func inferFragmentType(scopeType string) horizonmodels.FragmentType {
	switch strings.TrimSpace(scopeType) {
	case "country":
		return horizonmodels.FragmentTypeGeoRegion
	default:
		return horizonmodels.FragmentTypeOrganization
	}
}

func compactStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendInterfaceString(current any, value string) []string {
	out := []string{}
	switch typed := current.(type) {
	case []string:
		out = append(out, typed...)
	}
	out = append(out, strings.TrimSpace(value))
	return compactStrings(out)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

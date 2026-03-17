package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID   = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
		tenantID    = flag.String("tenant", "global", "tenant id")
		mode        = flag.String("mode", "sync", "mode: sync|explain-subject|explain-trigger|replay-trigger|emit-anchor-trigger|upsert-macro-policy|compile-macro-policies|lens-attention|cleanup-legacy|metrics")
		projectRef  = flag.String("project-ref", "", "macro policy project id")
		subjectID   = flag.String("subject-id", "", "watch subject id")
		triggerID   = flag.String("trigger-id", "", "watch trigger id")
		lensID      = flag.String("lens-id", "", "lens id")
		anchorPath  = flag.String("anchor-path", "", "anchor canonical path")
		artifactKey = flag.String("artifact-key", "", "artifact canonical key")
		name        = flag.String("name", "", "macro policy name")
		agentClass  = flag.String("agent-class", "", "macro agent class")
		domainModes = flag.String("domain-modes", "", "comma-separated domain modes")
		roles       = flag.String("roles", "", "comma-separated roles")
		priority    = flag.Int("priority", 0, "macro policy priority")
		paused      = flag.Bool("paused", false, "create/update macro policy in PAUSED status")
		reason      = flag.String("reason", "manual admin trigger", "trigger reason")
		triggerType = flag.String("trigger-type", "manual", "trigger type")
		limit       = flag.Int("limit", 100, "list limit")
		includeGlob = flag.Bool("include-global", false, "include global tenant during sync")
		apply       = flag.Bool("apply", false, "apply changes for sync/cleanup")
	)
	flag.Parse()

	ctx := context.Background()
	db, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore.NewClient: %v", err)
	}
	defer db.Close()

	agentJobsRepo := coreRepo.NewAgentJobsRepository(db)
	macroPoliciesRepo := horizonrepo.NewMacroAgentPoliciesRepository(db)
	service := horizonwatchruntime.NewService(
		horizonrepo.NewWatchSubjectsRepository(db),
		horizonrepo.NewWatchTriggersRepository(db),
		horizonrepo.NewWatchTargetsRepository(db),
		horizonrepo.NewWatchdogConfigsRepository(db),
		horizonrepo.NewScopeAgentConfigsRepository(db),
		horizonrepo.NewLensesRepository(db),
		horizonrepo.NewLensArtifactsRepository(db),
		horizonrepo.NewHorizonFragmentsRepository(db),
		horizonrepo.NewDependencyEdgesRepository(db),
	).WithScopeAgentRunsRepository(horizonrepo.NewScopeAgentRunsRepository(db)).
		WithMaterializedArtifactsRepository(horizonrepo.NewMaterializedArtifactsRepository(db)).
		WithMacroAgentPoliciesRepository(macroPoliciesRepo).
		WithAgentProposalsRepository(horizonrepo.NewAgentProposalsRepository(db)).
		WithArbitrationDecisionsRepository(horizonrepo.NewArbitrationDecisionsRepository(db)).
		WithWatchBriefsRepository(horizonrepo.NewWatchBriefsRepository(db)).
		WithWatchFeedEntriesRepository(horizonrepo.NewWatchFeedEntriesRepository(db)).
		WithRiskJobsRepository(horizonrepo.NewRiskJobsRepository(db)).
		WithScoutingJobsRepository(horizonrepo.NewScoutingJobsRepository(agentJobsRepo))

	var out any
	switch strings.TrimSpace(*mode) {
	case "sync", "cleanup-legacy":
		if *apply {
			out, err = service.SyncTenant(ctx, strings.TrimSpace(*tenantID), *includeGlob, *limit)
		} else {
			out, err = service.PreviewSyncTenant(ctx, strings.TrimSpace(*tenantID), *includeGlob, *limit)
		}
	case "explain-subject":
		if strings.TrimSpace(*subjectID) == "" {
			log.Fatal("--subject-id is required")
		}
		out, err = service.SubjectDetail(ctx, strings.TrimSpace(*subjectID), *limit)
	case "explain-trigger":
		if strings.TrimSpace(*triggerID) == "" {
			log.Fatal("--trigger-id is required")
		}
		trigger, gerr := service.GetTrigger(ctx, strings.TrimSpace(*triggerID))
		if gerr != nil {
			err = gerr
			break
		}
		detail := map[string]any{"trigger": trigger}
		if trigger != nil && strings.TrimSpace(trigger.SubjectID) != "" {
			if subject, serr := service.SubjectDetail(ctx, trigger.SubjectID, *limit); serr == nil {
				detail["subject"] = subject
			}
		}
		out = detail
	case "replay-trigger":
		if strings.TrimSpace(*triggerID) == "" {
			log.Fatal("--trigger-id is required")
		}
		out, err = service.ReplayTrigger(ctx, strings.TrimSpace(*triggerID))
	case "emit-anchor-trigger":
		if strings.TrimSpace(*anchorPath) == "" {
			log.Fatal("--anchor-path is required")
		}
		created, eerr := service.EmitTriggerByAnchor(
			ctx,
			strings.TrimSpace(*tenantID),
			strings.TrimSpace(*anchorPath),
			parseTriggerType(*triggerType),
			strings.TrimSpace(*reason),
			0.75,
			0.92,
			map[string]any{"emittedBy": "horizon_watchruntime_admin"},
		)
		err = eerr
		out = map[string]any{
			"created":       created,
			"tenantId":      strings.TrimSpace(*tenantID),
			"anchorPath":    strings.TrimSpace(*anchorPath),
			"triggerType":   strings.TrimSpace(*triggerType),
			"triggerReason": strings.TrimSpace(*reason),
		}
	case "upsert-macro-policy":
		policy := &horizonmodels.MacroAgentPolicy{
			TenantID:             strings.TrimSpace(*tenantID),
			ProjectID:            strings.TrimSpace(*projectRef),
			LensID:               strings.TrimSpace(*lensID),
			Name:                 strings.TrimSpace(firstNonEmpty(*name, *agentClass, *anchorPath, *artifactKey)),
			Status:               string(horizonmodels.MacroAgentStatusActive),
			AgentClass:           strings.TrimSpace(*agentClass),
			AnchorCanonicalPath:  strings.TrimSpace(*anchorPath),
			ArtifactCanonicalKey: strings.TrimSpace(*artifactKey),
			DomainModes:          splitCSV(*domainModes),
			Roles:                splitCSV(*roles),
			Priority:             *priority,
		}
		if *paused {
			policy.Status = string(horizonmodels.MacroAgentStatusPaused)
		}
		err = macroPoliciesRepo.Upsert(ctx, policy)
		if err != nil {
			break
		}
		subjects, cerr := service.CompileMacroAgentPolicy(ctx, policy)
		if cerr != nil {
			err = cerr
			break
		}
		out = map[string]any{
			"policy":   policy,
			"subjects": subjects,
		}
	case "compile-macro-policies":
		out, err = service.SyncTenant(ctx, strings.TrimSpace(*tenantID), *includeGlob, *limit)
	case "lens-attention":
		if strings.TrimSpace(*lensID) == "" {
			log.Fatal("--lens-id is required")
		}
		out, err = service.LensAttention(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), *limit)
	case "metrics":
		out = service.MetricsSnapshot()
	default:
		log.Fatalf("unsupported mode %q", strings.TrimSpace(*mode))
	}
	if err != nil {
		log.Fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatal(err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseTriggerType(raw string) horizonmodels.WatchTriggerType {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "fact_delta":
		return horizonmodels.WatchTriggerFactDelta
	case "source_delta":
		return horizonmodels.WatchTriggerSourceDelta
	case "signal_detected":
		return horizonmodels.WatchTriggerSignalDetected
	case "affected_zone_changed":
		return horizonmodels.WatchTriggerAffectedZoneChanged
	case "propagation_gap":
		return horizonmodels.WatchTriggerPropagationGap
	case "recommendation_approved":
		return horizonmodels.WatchTriggerRecommendationApproved
	default:
		return horizonmodels.WatchTriggerManual
	}
}

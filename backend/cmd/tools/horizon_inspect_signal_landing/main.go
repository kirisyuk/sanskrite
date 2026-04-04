package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	var (
		projectID = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon-test"), "GCP project id")
		tenantID  = flag.String("tenant", "global", "tenant id")
		sessionID = flag.String("session-id", "", "signal landing session id")
		scopeRef  = flag.String("scope-ref", "", "scope ref to inspect summaries for")
		includeCycle = flag.Bool("include-cycle", false, "include impacted lens warmup cycle snapshot")
		limit     = flag.Int("limit", 10, "result limit")
	)
	flag.Parse()

	ctx := context.Background()
	db, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore.NewClient: %v", err)
	}
	defer db.Close()

	sessionsRepo := horizonrepo.NewSignalLandingSessionsRepository(db)
	claimsRepo := horizonrepo.NewSignalImpactClaimsRepository(db)
	hitsRepo := horizonrepo.NewSignalLandingHitsRepository(db)
	missingRepo := horizonrepo.NewSignalMissingAnchorCandidatesRepository(db)
	summariesRepo := horizonrepo.NewSignalLandingSummariesRepository(db)
	lensesRepo := horizonrepo.NewLensesRepository(db)
	projectionsRepo := horizonrepo.NewRefScopeProjectionsRepository(db)

	var out any
	switch {
	case strings.TrimSpace(*sessionID) != "":
		session, err := sessionsRepo.Get(ctx, strings.TrimSpace(*sessionID))
		if err != nil {
			log.Fatal(err)
		}
		claims, err := claimsRepo.ListBySession(ctx, strings.TrimSpace(*sessionID), maxInt(*limit, 100))
		if err != nil {
			log.Fatal(err)
		}
		hits, err := hitsRepo.ListBySession(ctx, strings.TrimSpace(*sessionID), maxInt(*limit, 150))
		if err != nil {
			log.Fatal(err)
		}
		missing, err := missingRepo.ListBySession(ctx, strings.TrimSpace(*sessionID), maxInt(*limit, 100))
		if err != nil {
			log.Fatal(err)
		}
		payload := map[string]any{
			"session":           session,
			"claims":            claims,
			"hits":              hits,
			"missingCandidates": missing,
		}
		if *includeCycle && session != nil {
			payload["cycle"] = buildCycleSnapshot(ctx, lensesRepo, projectionsRepo, session, hits)
		}
		out = payload
	case strings.TrimSpace(*scopeRef) != "":
		summaries, err := summariesRepo.ListByScopeRefs(ctx, strings.TrimSpace(*tenantID), []string{strings.TrimSpace(*scopeRef)}, *limit)
		if err != nil {
			log.Fatal(err)
		}
		out = map[string]any{
			"scopeRef":   strings.TrimSpace(*scopeRef),
			"summaries":  summaries,
			"summaryCnt": len(summaries),
		}
	default:
		sessions, err := sessionsRepo.ListRecent(ctx, strings.TrimSpace(*tenantID), *limit)
		if err != nil {
			log.Fatal(err)
		}
		summaries, err := summariesRepo.ListRecent(ctx, strings.TrimSpace(*tenantID), *limit)
		if err != nil {
			log.Fatal(err)
		}
		payload := map[string]any{
			"sessions":      sessions,
			"recentSummary": summaries,
		}
		if *includeCycle && len(sessions) > 0 {
			hits, _ := hitsRepo.ListBySession(ctx, strings.TrimSpace(sessions[0].ID), maxInt(*limit, 150))
			payload["cycle"] = buildCycleSnapshot(ctx, lensesRepo, projectionsRepo, sessions[0], hits)
		}
		out = payload
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func buildCycleSnapshot(ctx context.Context, lensesRepo *horizonrepo.LensesRepository, projectionsRepo *horizonrepo.RefScopeProjectionsRepository, session *horizonmodels.SignalLandingSession, hits []*horizonmodels.SignalLandingHit) map[string]any {
	if session == nil {
		return nil
	}
	refSources := map[string][]string{}
	for _, hit := range hits {
		if hit == nil {
			continue
		}
		ref := strings.TrimSpace(hit.CanonicalRef)
		if ref == "" {
			continue
		}
		refSources[ref] = append(refSources[ref], string(hit.LandingMethod))
	}
	refs := make([]string, 0, len(refSources))
	for ref := range refSources {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	type lensCycle struct {
		ID                    string   `json:"id"`
		Name                  string   `json:"name"`
		TenantID              string   `json:"tenantId"`
		ProjectID             string   `json:"projectId"`
		MatchedRefs           []string `json:"matchedRefs,omitempty"`
		Status                string   `json:"status,omitempty"`
		Reason                string   `json:"reason,omitempty"`
		AvailableTemperatures []string `json:"availableTemperatures,omitempty"`
		CurrentUsableTemp     string   `json:"currentUsableTemperature,omitempty"`
		DocCount              int      `json:"docCount,omitempty"`
		LandingToHotMs        int64    `json:"landingToHotMs,omitempty"`
		LandingToWarmMs       int64    `json:"landingToWarmMs,omitempty"`
		LandingToFullMs       int64    `json:"landingToFullMs,omitempty"`
		RefreshQueuedAt       string   `json:"refreshQueuedAt,omitempty"`
		RefreshRunningAt      string   `json:"refreshRunningAt,omitempty"`
		QueuedAt              string   `json:"queuedAt,omitempty"`
		RunningAt             string   `json:"runningAt,omitempty"`
		CompletedAt           string   `json:"completedAt,omitempty"`
	}

	lensMatches := map[string]*lensCycle{}
	addLens := func(lens *horizonmodels.Lens, ref string) {
		if lens == nil || strings.TrimSpace(lens.ID) == "" {
			return
		}
		item, ok := lensMatches[lens.ID]
		if !ok {
			item = &lensCycle{
				ID:        strings.TrimSpace(lens.ID),
				Name:      strings.TrimSpace(lens.Name),
				TenantID:  strings.TrimSpace(lens.TenantID),
				ProjectID: strings.TrimSpace(lens.ProjectID),
			}
			if lens.ViewConfig != nil {
				if raw, ok := lens.ViewConfig["internalKnowledge"].(map[string]interface{}); ok {
					item.Status = asString(raw["status"])
					item.Reason = asString(raw["lastReason"])
					item.AvailableTemperatures = asStringSlice(raw["availableTemperatures"])
					item.CurrentUsableTemp = currentUsableTemperature(raw)
					item.DocCount = asInt(raw["docCount"])
					item.RefreshQueuedAt = asString(raw["refreshQueuedAt"])
					item.RefreshRunningAt = asString(raw["refreshRunningAt"])
					item.QueuedAt = asString(raw["queuedAt"])
					item.RunningAt = asString(raw["runningAt"])
					item.CompletedAt = asString(raw["completedAt"])
					item.LandingToHotMs = diffMs(session.CreatedAt, parseTime(raw["hotReadyAt"]))
					item.LandingToWarmMs = diffMs(session.CreatedAt, parseTime(raw["warmReadyAt"]))
					item.LandingToFullMs = diffMs(session.CreatedAt, firstNonZeroTime(parseTime(raw["fullReadyAt"]), parseTime(raw["completedAt"])))
				}
			}
			lensMatches[lens.ID] = item
		}
		if ref != "" {
			item.MatchedRefs = appendUnique(item.MatchedRefs, ref)
		}
	}

	for _, ref := range refs {
		if projectionsRepo != nil {
			projections, _ := projectionsRepo.ListByRefAnyTenant(ctx, ref, 20)
			for _, projection := range projections {
				for _, lensID := range projection.LensIDs {
					lens, err := lensesRepo.Get(ctx, strings.TrimSpace(lensID))
					if err == nil && lens != nil {
						addLens(lens, ref)
					}
				}
			}
		}
		if lensesRepo != nil {
			lenses, _ := lensesRepo.FindByScopeAnyTenant(ctx, ref, 20)
			for _, lens := range lenses {
				addLens(lens, ref)
			}
		}
	}

	outLenses := make([]*lensCycle, 0, len(lensMatches))
	for _, item := range lensMatches {
		sort.Strings(item.MatchedRefs)
		outLenses = append(outLenses, item)
	}
	sort.Slice(outLenses, func(i, j int) bool {
		if outLenses[i].CurrentUsableTemp == outLenses[j].CurrentUsableTemp {
			return outLenses[i].ID < outLenses[j].ID
		}
		return outLenses[i].CurrentUsableTemp > outLenses[j].CurrentUsableTemp
	})

	return map[string]any{
		"sessionId":          session.ID,
		"landingCreatedAt":   session.CreatedAt,
		"landingUpdatedAt":   session.UpdatedAt,
		"landingDurationMs":  diffMs(session.CreatedAt, session.UpdatedAt),
		"impactedRefCount":   len(refs),
		"impactedRefs":       refs,
		"impactedLensCount":  len(outLenses),
		"impactedLensStates": outLenses,
	}
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func asStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := asString(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return nil
	}
}

func asInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func parseTime(value any) time.Time {
	raw := strings.TrimSpace(asString(value))
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func diffMs(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func currentUsableTemperature(state map[string]interface{}) string {
	if !parseTime(state["fullReadyAt"]).IsZero() || !parseTime(state["completedAt"]).IsZero() {
		return "full"
	}
	if !parseTime(state["warmReadyAt"]).IsZero() {
		return "warm"
	}
	if !parseTime(state["hotReadyAt"]).IsZero() {
		return "hot"
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

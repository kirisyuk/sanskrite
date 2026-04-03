package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	var (
		projectID = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon-test"), "GCP project id")
		tenantID  = flag.String("tenant", "global", "tenant id")
		sessionID = flag.String("session-id", "", "signal landing session id")
		scopeRef  = flag.String("scope-ref", "", "scope ref to inspect summaries for")
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
		out = map[string]any{
			"session":           session,
			"claims":            claims,
			"hits":              hits,
			"missingCandidates": missing,
		}
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
		out = map[string]any{
			"sessions":      sessions,
			"recentSummary": summaries,
		}
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

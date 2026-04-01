package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID     = flag.String("project", "nv-horizon", "GCP project id")
		tenantID      = flag.String("tenant", "global", "tenant id (default: global)")
		canonicalPath = flag.String("path", "/org/byd", "canonical path to inspect (e.g., /org/byd)")
		limit         = flag.Int("limit", 200, "max facts to scan")
	)
	flag.Parse()

	ctx := context.Background()

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()

	frRepo := horizonrepo.NewHorizonFragmentsRepository(fs)
	factsRepo := corerepo.NewOntologyFactsRepository(fs)

	cp := strings.TrimSpace(*canonicalPath)
	tid := strings.TrimSpace(*tenantID)
	if cp == "" {
		panic("missing -path")
	}
	if tid == "" {
		tid = "global"
	}

	frag, err := frRepo.GetByCanonicalPath(ctx, cp)
	if err != nil {
		panic(err)
	}

	fmt.Printf("=== Horizon Inspect: %s ===\n", cp)
	fmt.Printf("Fragment: id=%s tenant=%s version=%d updatedAt=%s\n", frag.ID, frag.TenantID, frag.Version, frag.UpdatedAt.UTC().Format(time.RFC3339))

	// Print metric keys that look like risks
	riskKeys := make([]string, 0)
	for k := range frag.State {
		if strings.Contains(strings.ToLower(k), "risk") {
			riskKeys = append(riskKeys, k)
		}
	}
	sort.Strings(riskKeys)
	fmt.Printf("Risk-like metric keys (%d): %v\n", len(riskKeys), riskKeys)
	for _, k := range riskKeys {
		mv := frag.State[k]
		fmt.Printf("- %s: value=%.4f conf=%.3f method=%s\n", k, mv.Value, mv.Confidence, strings.TrimSpace(mv.Provenance.Method))
	}

	// Facts (all kinds)
	facts, err := factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tid, cp, time.Time{}, *limit)
	if err != nil {
		panic(err)
	}
	byKind := map[string]int{}
	for _, f := range facts {
		byKind[strings.TrimSpace(f.Kind)]++
	}
	kindKeys := make([]string, 0, len(byKind))
	for k := range byKind {
		kindKeys = append(kindKeys, k)
	}
	sort.Strings(kindKeys)
	fmt.Printf("Facts linked to %s (n=%d)\n", cp, len(facts))
	for _, k := range kindKeys {
		fmt.Printf("- kind=%s: %d\n", k, byKind[k])
	}

	// Print the latest metric fact (best-effort) to validate Scout->OntologyFacts emission.
	var latestMetricFactID string
	var latestMetricFactType string
	var latestMetricFactUpdatedAt time.Time
	var latestMetricSourceRefs []string
	var latestMetricUpdates []string
	for _, f := range facts {
		if strings.TrimSpace(f.Kind) != "metric" {
			continue
		}
		if latestMetricFactID == "" || f.UpdatedAt.After(latestMetricFactUpdatedAt) {
			latestMetricFactID = f.ID
			latestMetricFactType = strings.TrimSpace(f.Type)
			latestMetricFactUpdatedAt = f.UpdatedAt
			latestMetricSourceRefs = []string{}
			for _, sr := range f.SourceRefs {
				st := strings.TrimSpace(sr.SourceType)
				sid := strings.TrimSpace(sr.SourceID)
				if st == "" && sid == "" {
					continue
				}
				latestMetricSourceRefs = append(latestMetricSourceRefs, fmt.Sprintf("%s:%s", st, sid))
			}
			latestMetricUpdates = []string{}
			for _, mu := range f.MetricUpdates {
				mn := strings.TrimSpace(mu.MetricName)
				if mn == "" {
					continue
				}
				latestMetricUpdates = append(latestMetricUpdates, fmt.Sprintf("%s=%.4f(conf=%.2f)", mn, mu.Value, mu.Confidence))
			}
			sort.Strings(latestMetricSourceRefs)
			sort.Strings(latestMetricUpdates)
		}
	}
	if latestMetricFactID != "" {
		fmt.Printf("Latest metric fact: id=%s type=%s updatedAt=%s\n", latestMetricFactID, latestMetricFactType, latestMetricFactUpdatedAt.UTC().Format(time.RFC3339))
		fmt.Printf("- sourceRefs: %v\n", latestMetricSourceRefs)
		if len(latestMetricUpdates) > 10 {
			fmt.Printf("- metricUpdates: %d (showing first 10): %v\n", len(latestMetricUpdates), latestMetricUpdates[:10])
		} else {
			fmt.Printf("- metricUpdates: %d: %v\n", len(latestMetricUpdates), latestMetricUpdates)
		}
	}

	// Signals specifically
	signals, err := factsRepo.ListByCanonicalPathAndKind(ctx, tid, cp, "signal", 200)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Signals for %s: n=%d\n", cp, len(signals))
	for i, s := range signals {
		if i >= 10 {
			fmt.Printf("(truncated; showing first 10)\n")
			break
		}
		fmt.Printf("- signal id=%s type=%s conf=%.3f updatedAt=%s\n",
			s.ID,
			strings.TrimSpace(s.Type),
			s.Confidence,
			s.UpdatedAt.UTC().Format(time.RFC3339),
		)
	}
}

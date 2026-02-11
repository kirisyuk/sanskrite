package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")

	windowHours        = flag.Int("window-hours", 24, "Hours in each comparison window")
	baselineShiftHours = flag.Int("baseline-shift-hours", 24, "How many hours before now baseline window ends")
	limit              = flag.Int("limit", 720, "Telemetry records limit per metric")
	topInvalidLimit    = flag.Int("top-invalid-limit", 20, "Top invalid triples to print")
)

type windowStats struct {
	start    time.Time
	end      time.Time
	counters map[string]int64
}

type invalidTriple struct {
	key   string
	count int64
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *windowHours <= 0 {
		*windowHours = 24
	}
	if *baselineShiftHours <= 0 {
		*baselineShiftHours = *windowHours
	}
	if *limit <= 0 {
		*limit = 720
	}
	if *topInvalidLimit <= 0 {
		*topInvalidLimit = 20
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer func() { _ = client.Close() }()

	repo := horizonrepo.NewHorizonTelemetryRepository(client)
	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}

	kpiRecords, err := repo.List(ctx, tenant, "edge_policy", "kpi_rates", *limit)
	if err != nil {
		log.Fatalf("failed to read kpi_rates telemetry: %v", err)
	}
	invalidRecords, err := repo.List(ctx, tenant, "edge_policy", "top_invalid_triples", *limit)
	if err != nil {
		log.Fatalf("failed to read top_invalid_triples telemetry: %v", err)
	}

	now := time.Now().UTC()
	afterEnd := now
	afterStart := afterEnd.Add(-time.Duration(*windowHours) * time.Hour)
	baselineEnd := now.Add(-time.Duration(*baselineShiftHours) * time.Hour)
	baselineStart := baselineEnd.Add(-time.Duration(*windowHours) * time.Hour)

	after := aggregateWindow(kpiRecords, afterStart, afterEnd)
	baseline := aggregateWindow(kpiRecords, baselineStart, baselineEnd)

	fmt.Printf("Horizon Edge Policy KPI Report\n")
	fmt.Printf("tenant=%s project=%s\n", tenant, strings.TrimSpace(*projectID))
	fmt.Printf("baseline: %s .. %s\n", baselineStart.Format(time.RFC3339), baselineEnd.Format(time.RFC3339))
	fmt.Printf("after:    %s .. %s\n\n", afterStart.Format(time.RFC3339), afterEnd.Format(time.RFC3339))

	printKPIComparison("policy_reject_non_blocking_rate", baseline, after)
	printKPIComparison("auto_fix_rate", baseline, after)
	printKPIComparison("auto_fix_hit_rate", baseline, after)
	printKPIComparison("penalty_rate", baseline, after)
	printKPIComparison("quarantine_rate", baseline, after)
	printKPIComparison("unknown_direction_policy_count", baseline, after)
	printKPIComparison("false_positive_reject_rate", baseline, after)
	printKPIComparison("type_path_mismatch_rate", baseline, after)

	topInvalid := aggregateInvalidTriples(invalidRecords, afterStart, afterEnd)
	fmt.Printf("\nTop invalid triples (after window):\n")
	if len(topInvalid) == 0 {
		fmt.Printf("  (no invalid triples in window)\n")
		return
	}
	limitOut := *topInvalidLimit
	if limitOut > len(topInvalid) {
		limitOut = len(topInvalid)
	}
	for i := 0; i < limitOut; i++ {
		fmt.Printf("  %2d. %s -> %d\n", i+1, topInvalid[i].key, topInvalid[i].count)
	}
}

func aggregateWindow(records []*horizonrepo.HorizonTelemetryRecord, start, end time.Time) windowStats {
	out := windowStats{
		start:    start,
		end:      end,
		counters: map[string]int64{},
	}
	for _, rec := range records {
		if rec == nil {
			continue
		}
		ws := rec.WindowStart.UTC()
		if ws.Before(start) || !ws.Before(end) {
			continue
		}
		for k, v := range rec.Counters {
			out.counters[k] += v
		}
	}
	return out
}

func safeRate(counters map[string]int64, key string) float64 {
	total := counters["policy_decision_total"]
	if total <= 0 {
		return 0
	}
	return float64(counters[key]) / float64(total)
}

func printKPIComparison(key string, baseline, after windowStats) {
	base := safeRate(baseline.counters, key)
	now := safeRate(after.counters, key)
	delta := now - base

	fmt.Printf("%-34s baseline=%.4f after=%.4f delta=%+.4f", key, base, now, delta)
	if key == "unknown_direction_policy_count" {
		fmt.Printf(" (abs baseline=%d after=%d)", baseline.counters[key], after.counters[key])
	}
	fmt.Printf("\n")
}

func aggregateInvalidTriples(
	records []*horizonrepo.HorizonTelemetryRecord,
	start time.Time,
	end time.Time,
) []invalidTriple {
	agg := map[string]int64{}
	for _, rec := range records {
		if rec == nil {
			continue
		}
		ws := rec.WindowStart.UTC()
		if ws.Before(start) || !ws.Before(end) {
			continue
		}
		src := strings.TrimSpace(rec.Dimensions["sourceType"])
		dst := strings.TrimSpace(rec.Dimensions["targetType"])
		edge := strings.TrimSpace(rec.Dimensions["edgeType"])
		if src == "" || dst == "" || edge == "" {
			continue
		}
		key := fmt.Sprintf("%s -> %s (%s)", src, dst, edge)
		agg[key] += rec.Counters["top_invalid_triples"]
	}
	out := make([]invalidTriple, 0, len(agg))
	for k, v := range agg {
		out = append(out, invalidTriple{key: k, count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count == out[j].count {
			return out[i].key < out[j].key
		}
		return out[i].count > out[j].count
	})
	return out
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID = flag.String("project", "nv-horizon", "GCP project ID")
	dryRun    = flag.Bool("dry-run", false, "Only print proposed repairs")
	limit     = flag.Int("limit", 10000, "Max zones to scan")
)

const (
	zoneMetricHypeScore   = "zone_hype_score"
	zoneMetricHits1h      = "zone_hits_1h"
	zoneMetricHits24h     = "zone_hits_24h"
	zoneMetricTotalMent   = "zone_total_mentions"
	zoneMetricLastBoost   = "zone_last_boost"
	zoneMetricLastMatchTS = "zone_last_match_unix"
)

func main() {
	flag.Parse()
	ctx := context.Background()

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore: %v", err)
	}
	defer fs.Close()

	repo := hrepo.NewHorizonFragmentsRepository(fs)
	items, err := repo.ListByTypeAndTenant(ctx, "global", horizonmodels.FragmentTypeAffectedZone, *limit)
	if err != nil {
		log.Fatalf("ListByTypeAndTenant: %v", err)
	}

	now := time.Now().UTC()
	scanned := 0
	repaired := 0
	for _, frag := range items {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
			continue
		}
		scanned++
		if !qualifierBoolValue(frag.Qualifiers, "signalLandingGenerated") {
			continue
		}
		if hasActiveZoneState(frag) {
			continue
		}
		lastMatchedAt, ok := zoneRepairTimestamp(frag)
		if !ok {
			continue
		}
		score := zoneRepairFallbackScore(strings.TrimSpace(fmt.Sprint(frag.Qualifiers["conditionType"])), lastMatchedAt, now)
		if score <= 0 {
			continue
		}
		if *dryRun {
			log.Printf("[dry-run] repair %s score=%.1f matched=%s", frag.CanonicalPath, score, lastMatchedAt.Format(time.RFC3339))
			repaired++
			continue
		}
		applyZoneRepairState(frag, score, lastMatchedAt, now)
		if err := repo.Update(ctx, frag); err != nil {
			log.Printf("repair failed %s: %v", frag.CanonicalPath, err)
			continue
		}
		log.Printf("repaired %s score=%.1f matched=%s", frag.CanonicalPath, score, lastMatchedAt.Format(time.RFC3339))
		repaired++
	}
	log.Printf("signal landing zone activity repair complete: scanned=%d repaired=%d dryRun=%v", scanned, repaired, *dryRun)
}

func hasActiveZoneState(f *horizonmodels.HorizonFragment) bool {
	if f == nil || f.State == nil {
		return false
	}
	mv, ok := f.State[zoneMetricHypeScore]
	return ok && mv.Value > 0
}

func zoneRepairTimestamp(f *horizonmodels.HorizonFragment) (time.Time, bool) {
	parse := func(raw interface{}) (time.Time, bool) {
		value := strings.TrimSpace(fmt.Sprint(raw))
		if value == "" || value == "<nil>" {
			return time.Time{}, false
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, false
		}
		return parsed.UTC(), true
	}
	if ts, ok := parse(f.Qualifiers["lastMatchedAt"]); ok {
		return ts, true
	}
	if ts, ok := parse(f.Qualifiers["signalLandingUpdatedAt"]); ok {
		return ts, true
	}
	if !f.Metadata.LastUpdatedAt.IsZero() {
		return f.Metadata.LastUpdatedAt.UTC(), true
	}
	if !f.UpdatedAt.IsZero() {
		return f.UpdatedAt.UTC(), true
	}
	return time.Time{}, false
}

func zoneRepairFallbackScore(conditionType string, matchedAt, now time.Time) float64 {
	score := 32.0
	switch strings.ToLower(strings.TrimSpace(conditionType)) {
	case "military_conflict":
		score = 64.0
	case "infrastructure_disruption":
		score = 48.0
	case "cyber_disruption":
		score = 36.0
	}
	age := now.Sub(matchedAt.UTC())
	switch {
	case age > 14*24*time.Hour:
		score *= 0.18
	case age > 7*24*time.Hour:
		score *= 0.34
	case age > 72*time.Hour:
		score *= 0.58
	case age > 24*time.Hour:
		score *= 0.78
	}
	return clamp(score, 0, 100)
}

func applyZoneRepairState(f *horizonmodels.HorizonFragment, score float64, matchedAt, now time.Time) {
	if f.State == nil {
		f.State = map[string]horizonmodels.MetricValue{}
	}
	putMetric := func(name string, value, confidence float64, observedAt time.Time) {
		f.State[name] = horizonmodels.MetricValue{
			Value:      value,
			Confidence: confidence,
			ObservedAt: observedAt,
			Provenance: horizonmodels.Provenance{
				Method: "signal_landing_zone_repair",
			},
		}
	}
	putMetric(zoneMetricHypeScore, score, 0.82, now)
	putMetric(zoneMetricHits1h, clamp(0.6+score*0.25, 0, 10000), 0.80, now)
	putMetric(zoneMetricHits24h, clamp(1.0+score*0.35, 0, 50000), 0.80, now)
	putMetric(zoneMetricTotalMent, 1, 0.76, now)
	putMetric(zoneMetricLastBoost, clamp(score, 0, 100), 0.74, now)
	putMetric(zoneMetricLastMatchTS, float64(matchedAt.Unix()), 0.92, now)
	putMetric("zone_severity_score", clamp(score/10.0, 0, 10), 0.78, now)
	if f.Qualifiers == nil {
		f.Qualifiers = map[string]interface{}{}
	}
	f.Qualifiers["hypeStatus"] = zoneRepairStatus(score)
	f.Qualifiers["hypeIndicator"] = zoneRepairIndicator(score)
	f.Qualifiers["lastMatchedAt"] = matchedAt.UTC().Format(time.RFC3339)
	f.Qualifiers["signalLandingUpdatedAt"] = now.Format(time.RFC3339)
	if f.Metadata.LastUpdatedAt.Before(now) {
		f.Metadata.LastUpdatedAt = now
	}
}

func zoneRepairStatus(score float64) string {
	switch {
	case score >= 72:
		return "hot"
	case score >= 48:
		return "active"
	case score >= 26:
		return "watch"
	case score >= 10:
		return "cooling"
	default:
		return "inactive"
	}
}

func zoneRepairIndicator(score float64) int {
	switch {
	case score >= 80:
		return 5
	case score >= 60:
		return 4
	case score >= 40:
		return 3
	case score >= 20:
		return 2
	case score >= 8:
		return 1
	default:
		return 0
	}
}

func qualifierBoolValue(qualifiers map[string]interface{}, key string) bool {
	if len(qualifiers) == 0 || strings.TrimSpace(key) == "" {
		return false
	}
	switch v := qualifiers[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func _decay(v, deltaHours, halfLifeHours float64) float64 {
	if v <= 0 || deltaHours <= 0 || halfLifeHours <= 0 {
		return v
	}
	return v * math.Exp(-math.Ln2*deltaHours/halfLifeHours)
}

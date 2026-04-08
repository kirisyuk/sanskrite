package signallanding

import (
	"math"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	signalLandingZoneMetricHypeScore   = "zone_hype_score"
	signalLandingZoneMetricHits1h      = "zone_hits_1h"
	signalLandingZoneMetricHits24h     = "zone_hits_24h"
	signalLandingZoneMetricTotalMent   = "zone_total_mentions"
	signalLandingZoneMetricLastBoost   = "zone_last_boost"
	signalLandingZoneMetricLastMatchTS = "zone_last_match_unix"
)

type signalLandingZoneActivityState struct {
	Score         float64
	Hits1h        float64
	Hits24h       float64
	TotalMentions float64
	LastBoost     float64
	LastMatchedAt time.Time
	LastUpdatedAt time.Time
}

func parseSignalLandingZoneActivityState(zone *horizonmodels.HorizonFragment) signalLandingZoneActivityState {
	st := signalLandingZoneActivityState{}
	if zone == nil {
		return st
	}
	for key, mv := range zone.State {
		switch strings.TrimSpace(key) {
		case signalLandingZoneMetricHypeScore:
			st.Score = mv.Value
		case signalLandingZoneMetricHits1h:
			st.Hits1h = mv.Value
		case signalLandingZoneMetricHits24h:
			st.Hits24h = mv.Value
		case signalLandingZoneMetricTotalMent:
			st.TotalMentions = mv.Value
		case signalLandingZoneMetricLastBoost:
			st.LastBoost = mv.Value
		case signalLandingZoneMetricLastMatchTS:
			if mv.Value > 0 {
				st.LastMatchedAt = time.Unix(int64(mv.Value), 0).UTC()
			}
		}
		if mv.ObservedAt.After(st.LastUpdatedAt) {
			st.LastUpdatedAt = mv.ObservedAt.UTC()
		}
	}
	if st.LastUpdatedAt.IsZero() {
		st.LastUpdatedAt = zone.Metadata.LastUpdatedAt.UTC()
	}
	if st.LastUpdatedAt.IsZero() {
		st.LastUpdatedAt = zone.UpdatedAt.UTC()
	}
	if st.LastUpdatedAt.IsZero() {
		st.LastUpdatedAt = zone.CreatedAt.UTC()
	}
	if st.LastMatchedAt.IsZero() {
		if raw := strings.TrimSpace(fmtString(zone.Qualifiers["lastMatchedAt"])); raw != "" {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				st.LastMatchedAt = parsed.UTC()
			}
		}
	}
	if st.LastMatchedAt.IsZero() {
		st.LastMatchedAt = st.LastUpdatedAt
	}
	return st
}

func (s *signalLandingZoneActivityState) applyDecay(now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	deltaHours := now.Sub(s.LastUpdatedAt).Hours()
	if deltaHours <= 0 {
		return
	}
	s.Score = decaySignalLandingZoneValue(s.Score, deltaHours, 8.0)
	s.Hits1h = decaySignalLandingZoneValue(s.Hits1h, deltaHours, 1.0)
	s.Hits24h = decaySignalLandingZoneValue(s.Hits24h, deltaHours, 24.0)
	s.LastUpdatedAt = now.UTC()
}

func (s *signalLandingZoneActivityState) applyObservation(now, matchedAt time.Time, boost float64) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if matchedAt.IsZero() {
		matchedAt = now
	}
	s.Score = clampSignalLandingZoneValue(s.Score+boost, 0, 100)
	s.Hits1h = clampSignalLandingZoneValue(s.Hits1h+(0.6+boost*0.25), 0, 10000)
	s.Hits24h = clampSignalLandingZoneValue(s.Hits24h+(1.0+boost*0.35), 0, 50000)
	s.TotalMentions = clampSignalLandingZoneValue(s.TotalMentions+1, 0, 1e9)
	s.LastBoost = boost
	s.LastMatchedAt = matchedAt.UTC()
	s.LastUpdatedAt = now.UTC()
}

func applySignalLandingZoneActivity(zone *horizonmodels.HorizonFragment, session *horizonmodels.SignalLandingSession, claim *horizonmodels.SignalImpactClaim, matchedAt time.Time) {
	if zone == nil {
		return
	}
	if zone.State == nil {
		zone.State = map[string]horizonmodels.MetricValue{}
	}
	now := time.Now().UTC()
	if matchedAt.IsZero() {
		if claim != nil && !claim.UpdatedAt.IsZero() {
			matchedAt = claim.UpdatedAt.UTC()
		} else if session != nil && !session.UpdatedAt.IsZero() {
			matchedAt = session.UpdatedAt.UTC()
		} else {
			matchedAt = now
		}
	}
	st := parseSignalLandingZoneActivityState(zone)
	st.applyDecay(now)
	boost := signalLandingZoneBoost(session, claim)
	st.applyObservation(now, matchedAt, boost)

	putMetric := func(name string, value, confidence float64, observedAt time.Time) {
		provenance := horizonmodels.Provenance{
			Method: "signal_landing_zone",
		}
		if session != nil {
			if session.ID != "" {
				provenance.SourceIDs = []string{strings.TrimSpace(session.ID)}
			}
			provenance.ObservedAt = &observedAt
		}
		zone.State[name] = horizonmodels.MetricValue{
			Value:      value,
			Confidence: confidence,
			ObservedAt: observedAt,
			Provenance: provenance,
		}
	}
	putMetric(signalLandingZoneMetricHypeScore, clampSignalLandingZoneValue(st.Score, 0, 100), 0.92, now)
	putMetric(signalLandingZoneMetricHits1h, clampSignalLandingZoneValue(st.Hits1h, 0, 10000), 0.90, now)
	putMetric(signalLandingZoneMetricHits24h, clampSignalLandingZoneValue(st.Hits24h, 0, 50000), 0.90, now)
	putMetric(signalLandingZoneMetricTotalMent, clampSignalLandingZoneValue(st.TotalMentions, 0, 1e9), 0.88, now)
	putMetric(signalLandingZoneMetricLastBoost, clampSignalLandingZoneValue(st.LastBoost, 0, 100), 0.86, now)
	putMetric(signalLandingZoneMetricLastMatchTS, float64(st.LastMatchedAt.Unix()), 0.96, now)
	putMetric("zone_severity_score", clampSignalLandingZoneValue(st.Score/10.0, 0, 10), 0.88, now)

	if zone.Qualifiers == nil {
		zone.Qualifiers = map[string]interface{}{}
	}
	zone.Qualifiers["hypeStatus"] = signalLandingZoneActivityStatus(st.Score)
	zone.Qualifiers["hypeIndicator"] = signalLandingZoneHypeIndicator(st.Score)
	zone.Qualifiers["lastMatchedAt"] = st.LastMatchedAt.UTC().Format(time.RFC3339)
	zone.Qualifiers["signalLandingUpdatedAt"] = now.Format(time.RFC3339)
	if zone.Metadata.LastUpdatedAt.Before(now) {
		zone.Metadata.LastUpdatedAt = now
	}
}

func signalLandingZoneBoost(session *horizonmodels.SignalLandingSession, claim *horizonmodels.SignalImpactClaim) float64 {
	severity := 0.62
	confidence := 0.72
	conditionType := ""
	if claim != nil {
		if claim.Severity > 0 {
			severity = claim.Severity
		}
		if claim.Confidence > 0 {
			confidence = claim.Confidence
		}
		conditionType = inferSignalLandingConditionType(claim)
	}
	base := 18.0
	base *= 0.92 + clampSignalLandingZoneValue(severity, 0, 1)*1.15
	base *= 0.94 + clampSignalLandingZoneValue(confidence, 0, 1)*0.75
	switch conditionType {
	case "military_conflict":
		base *= 1.75
	case "infrastructure_disruption":
		base *= 1.35
	case "cyber_disruption":
		base *= 1.18
	case "humanitarian_crisis":
		base *= 1.08
	}
	if session != nil && !session.UpdatedAt.IsZero() {
		age := time.Since(session.UpdatedAt.UTC())
		switch {
		case age > 14*24*time.Hour:
			base *= 0.22
		case age > 7*24*time.Hour:
			base *= 0.40
		case age > 72*time.Hour:
			base *= 0.62
		case age > 24*time.Hour:
			base *= 0.82
		}
	}
	return clampSignalLandingZoneValue(base, 8.0, 72.0)
}

func signalLandingZoneActivityStatus(score float64) string {
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

func signalLandingZoneHypeIndicator(score float64) int {
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

func decaySignalLandingZoneValue(v, deltaHours, halfLifeHours float64) float64 {
	if v <= 0 || deltaHours <= 0 || halfLifeHours <= 0 {
		return v
	}
	return v * math.Exp(-math.Ln2*deltaHours/halfLifeHours)
}

func clampSignalLandingZoneValue(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func fmtString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

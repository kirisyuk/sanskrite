package pathbuilder

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type legInterval struct {
	start *time.Time
	end   *time.Time
}

func (b *SupplyChainPathBuilder) evaluatePathSufficiency(ctx context.Context, path *SupplyChainPath, legs []models.SupplyChainPathLeg) *models.SupplyChainPathSufficiency {
	if path == nil || len(path.Segments) == 0 {
		return nil
	}
	suff := &models.SupplyChainPathSufficiency{
		Status: models.SupplyChainSufficiencyValidated,
		Score:  1,
	}

	hasHardFailure := false
	hasWarning := false
	legIntervals := make([]legInterval, len(path.Segments))
	joinHintByIndex := make(map[int]models.SupplyChainJoinAssessment, len(path.JoinAssessmentHints))
	for _, hint := range path.JoinAssessmentHints {
		joinHintByIndex[hint.Index] = hint
	}

	for idx, segment := range path.Segments {
		leg := models.SupplyChainLegAssessment{
			LegIndex:                  idx,
			RouteSensitive:            isRouteSensitiveSegment(segment, legAtIndex(legs, idx)),
			ExplicitTransportEvidence: len(segment.TransportViaRefs) > 0,
			BindingConfidence:         legBindingConfidence(legAtIndex(legs, idx)),
		}
		interval := intervalFromSegment(segment)
		legIntervals[idx] = interval
		leg.TimeKnown = interval.start != nil || interval.end != nil
		switch {
		case !leg.RouteSensitive:
			leg.RouteStatus = "explicit"
		case leg.ExplicitTransportEvidence:
			leg.RouteStatus = "explicit"
		case leg.BindingConfidence > 0:
			leg.RouteStatus = "bound_only"
		default:
			leg.RouteStatus = "unknown"
		}

		switch {
		case leg.RouteSensitive && !leg.ExplicitTransportEvidence && leg.RouteStatus != "explicit":
			leg.Status = models.SupplyChainSufficiencyHeuristic
			leg.Reasons = append(leg.Reasons, "route-sensitive leg lacks explicit transport evidence")
			suff.Warnings = append(suff.Warnings, fmt.Sprintf("leg %d route-sensitive without explicit transport evidence", idx))
			suff.OpenGaps = append(suff.OpenGaps, fmt.Sprintf("Add transport evidence for leg %d", idx))
			hasWarning = true
			suff.Score -= 0.12
		default:
			leg.Status = models.SupplyChainSufficiencyValidated
		}
		if !leg.TimeKnown {
			if leg.Status == models.SupplyChainSufficiencyValidated {
				leg.Status = models.SupplyChainSufficiencyHeuristic
			}
			leg.Reasons = append(leg.Reasons, "time window is unknown")
			suff.Warnings = append(suff.Warnings, fmt.Sprintf("leg %d time window is unknown", idx))
			suff.OpenGaps = append(suff.OpenGaps, fmt.Sprintf("Resolve time window for leg %d", idx))
			hasWarning = true
			suff.Score -= 0.08
		}
		suff.LegAssessments = append(suff.LegAssessments, leg)
	}

	for idx := 0; idx < len(path.Segments)-1; idx++ {
		left := path.Segments[idx]
		right := path.Segments[idx+1]
		hint, hasHint := joinHintByIndex[idx]
		join := models.SupplyChainJoinAssessment{
			Index:              idx,
			LeftLegIndex:       idx,
			RightLegIndex:      idx + 1,
			ExpectedHandoffRef: strings.TrimSpace(left.CustomerRef),
			LeftCustomerRef:    strings.TrimSpace(left.CustomerRef),
			RightSupplierRef:   strings.TrimSpace(right.SupplierRef),
			EntityReuse:        models.SupplyChainJoinEntityReuseOnly,
			Status:             models.SupplyChainSufficiencyValidated,
		}

		if join.LeftCustomerRef == "" || join.RightSupplierRef == "" {
			join.EntityReuse = models.SupplyChainJoinEntityReuseUnknown
		}
		if hasHint {
			if strings.TrimSpace(hint.ExpectedHandoffRef) != "" {
				join.ExpectedHandoffRef = strings.TrimSpace(hint.ExpectedHandoffRef)
			}
			switch strings.TrimSpace(hint.EntityReuse) {
			case models.SupplyChainJoinEntityReuseOnly, models.SupplyChainJoinEntityReuseBlocked, models.SupplyChainJoinEntityReuseUnknown:
				join.EntityReuse = strings.TrimSpace(hint.EntityReuse)
			}
		}

		if join.LeftCustomerRef != "" && join.LeftCustomerRef == join.RightSupplierRef {
			join.ActorContinuity = "pass"
		} else {
			join.ActorContinuity = "fail"
			join.Status = models.SupplyChainSufficiencyForensicGap
			join.Reasons = append(join.Reasons, "actor continuity mismatch")
			suff.HardFailures = append(suff.HardFailures, fmt.Sprintf("join %d actor continuity mismatch", idx))
			hasHardFailure = true
			suff.Score -= 0.28
		}

		switch strings.TrimSpace(join.EntityReuse) {
		case models.SupplyChainJoinEntityReuseUnknown:
			if join.Status == models.SupplyChainSufficiencyValidated {
				join.Status = models.SupplyChainSufficiencyHeuristic
			}
			join.Reasons = append(join.Reasons, "handoff entity unresolved")
			suff.Warnings = append(suff.Warnings, fmt.Sprintf("join %d handoff entity unresolved", idx))
			suff.OpenGaps = append(suff.OpenGaps, fmt.Sprintf("Resolve handoff entity for join %d", idx))
			hasWarning = true
			suff.Score -= 0.10
		}

		productContinuity := "unknown"
		switch {
		case strings.TrimSpace(left.ProductRef) != "" && strings.TrimSpace(left.ProductRef) == strings.TrimSpace(right.ProductRef):
			productContinuity = "same"
		default:
			if b != nil {
				if ok, _ := b.checkProductTransformation(ctx, strings.TrimSpace(left.ProductRef), strings.TrimSpace(right.ProductRef)); ok {
					productContinuity = "transforms_into"
				}
			}
		}
		if hasHint {
			switch strings.TrimSpace(hint.ProductContinuity) {
			case "same", "transforms_into", "contradiction":
				if productContinuity == "unknown" || hint.ProductContinuity == "contradiction" {
					productContinuity = strings.TrimSpace(hint.ProductContinuity)
				}
			}
		}
		join.ProductContinuity = productContinuity
		if productContinuity == "contradiction" {
			join.Status = models.SupplyChainSufficiencyForensicGap
			join.Reasons = append(join.Reasons, "product continuity contradiction")
			suff.HardFailures = append(suff.HardFailures, fmt.Sprintf("join %d product continuity contradiction", idx))
			hasHardFailure = true
			suff.Score -= 0.24
		} else if productContinuity == "unknown" {
			if join.Status == models.SupplyChainSufficiencyValidated {
				join.Status = models.SupplyChainSufficiencyHeuristic
			}
			join.Reasons = append(join.Reasons, "product continuity is not proven")
			suff.Warnings = append(suff.Warnings, fmt.Sprintf("join %d missing explicit product transformation proof", idx))
			suff.OpenGaps = append(suff.OpenGaps, fmt.Sprintf("Resolve product continuity for join %d", idx))
			hasWarning = true
			suff.Score -= 0.12
		}

		temporalContinuity := compareIntervals(legIntervals[idx], legIntervals[idx+1])
		join.TemporalContinuity = temporalContinuity
		switch temporalContinuity {
		case "disjoint":
			join.Status = models.SupplyChainSufficiencyForensicGap
			join.Reasons = append(join.Reasons, "time windows do not overlap")
			suff.HardFailures = append(suff.HardFailures, fmt.Sprintf("join %d has disjoint time windows", idx))
			hasHardFailure = true
			suff.Score -= 0.26
		case "unknown":
			if join.Status == models.SupplyChainSufficiencyValidated {
				join.Status = models.SupplyChainSufficiencyHeuristic
			}
			join.Reasons = append(join.Reasons, "temporal continuity is unknown")
			suff.Warnings = append(suff.Warnings, fmt.Sprintf("join %d temporal continuity is unknown", idx))
			suff.OpenGaps = append(suff.OpenGaps, fmt.Sprintf("Resolve temporal continuity for join %d", idx))
			hasWarning = true
			suff.Score -= 0.10
		}
		if hasHint {
			switch strings.TrimSpace(hint.Status) {
			case models.SupplyChainSufficiencyForensicGap:
				join.Status = models.SupplyChainSufficiencyForensicGap
				hasHardFailure = true
				suff.Score -= 0.12
			case models.SupplyChainSufficiencyHeuristic:
				if join.Status == models.SupplyChainSufficiencyValidated {
					join.Status = models.SupplyChainSufficiencyHeuristic
				}
				hasWarning = true
				suff.Score -= 0.06
			}
			if join.ActorContinuity == "" && strings.TrimSpace(hint.ActorContinuity) != "" {
				join.ActorContinuity = strings.TrimSpace(hint.ActorContinuity)
			}
			join.Reasons = append(join.Reasons, hint.Reasons...)
			for _, gap := range hint.Reasons {
				if strings.TrimSpace(gap) == "" {
					continue
				}
				if join.Status == models.SupplyChainSufficiencyForensicGap {
					suff.HardFailures = append(suff.HardFailures, gap)
				} else {
					suff.Warnings = append(suff.Warnings, gap)
				}
			}
			switch join.EntityReuse {
			case models.SupplyChainJoinEntityReuseBlocked:
				join.Status = models.SupplyChainSufficiencyForensicGap
				join.Reasons = append(join.Reasons, "join validation rejected non-allowlisted entity reuse")
				suff.HardFailures = append(suff.HardFailures, fmt.Sprintf("join %d used non-allowlisted entity", idx))
				hasHardFailure = true
				suff.Score -= 0.18
			case models.SupplyChainJoinEntityReuseUnknown:
				if join.Status == models.SupplyChainSufficiencyValidated {
					join.Status = models.SupplyChainSufficiencyHeuristic
				}
				hasWarning = true
			}
		}

		join.Reasons = uniqueTrimmed(join.Reasons)
		suff.JoinAssessments = append(suff.JoinAssessments, join)
	}
	for _, gap := range path.OpenGaps {
		if strings.TrimSpace(gap) == "" {
			continue
		}
		suff.OpenGaps = append(suff.OpenGaps, gap)
		if !hasHardFailure {
			hasWarning = true
		}
	}

	switch {
	case hasHardFailure:
		suff.Status = models.SupplyChainSufficiencyForensicGap
	case hasWarning:
		suff.Status = models.SupplyChainSufficiencyHeuristic
	default:
		suff.Status = models.SupplyChainSufficiencyValidated
	}
	if len(path.Segments) == 1 && len(suff.LegAssessments) == 1 && suff.LegAssessments[0].Status != models.SupplyChainSufficiencyValidated {
		suff.Status = models.SupplyChainSufficiencyHeuristic
	}
	suff.Score = math.Max(0.05, math.Min(1, suff.Score))
	suff.HardFailures = uniqueTrimmed(suff.HardFailures)
	suff.Warnings = uniqueTrimmed(suff.Warnings)
	suff.OpenGaps = uniqueTrimmed(suff.OpenGaps)
	suff.Summary = buildSufficiencySummary(suff)
	return suff
}

func legAtIndex(legs []models.SupplyChainPathLeg, idx int) *models.SupplyChainPathLeg {
	for i := range legs {
		if legs[i].Index == idx {
			return &legs[i]
		}
	}
	if idx >= 0 && idx < len(legs) {
		return &legs[idx]
	}
	return nil
}

func legBindingConfidence(leg *models.SupplyChainPathLeg) float64 {
	if leg == nil {
		return 0
	}
	return leg.BindingConfidence
}

func isRouteSensitiveSegment(segment PathSegment, leg *models.SupplyChainPathLeg) bool {
	if len(segment.TransportViaRefs) > 0 {
		return true
	}
	if leg != nil {
		mode := strings.TrimSpace(leg.Mode)
		switch {
		case strings.HasPrefix(mode, "pipeline"), mode == "sea", mode == "rail", mode == "air":
			return true
		}
	}
	return pathISO2(segment.SupplierRef) != "" && pathISO2(segment.CustomerRef) != "" && pathISO2(segment.SupplierRef) != pathISO2(segment.CustomerRef)
}

func pathISO2(ref string) string {
	ref = strings.Trim(strings.TrimSpace(ref), "/")
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, "/")
	if len(parts) < 3 {
		return ""
	}
	iso := strings.ToLower(strings.TrimSpace(parts[1]))
	if len(iso) != 2 {
		return ""
	}
	return iso
}

func intervalFromSegment(segment PathSegment) legInterval {
	start := parseMetricTime(segment.Metrics["start"])
	end := parseMetricTime(segment.Metrics["end"])
	return legInterval{start: start, end: end}
}

func parseMetricTime(metric models.MetricValue) *time.Time {
	candidate := ""
	if metric.Unit != nil {
		candidate = strings.TrimSpace(*metric.Unit)
	}
	if candidate == "" {
		return nil
	}
	layouts := []string{time.RFC3339, "2006-01-02", "2006-01", time.RFC3339Nano}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, candidate); err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	return nil
}

func compareIntervals(left, right legInterval) string {
	if left.start == nil && left.end == nil {
		return "unknown"
	}
	if right.start == nil && right.end == nil {
		return "unknown"
	}
	leftStart := resolveIntervalStart(left)
	leftEnd := resolveIntervalEnd(left)
	rightStart := resolveIntervalStart(right)
	rightEnd := resolveIntervalEnd(right)
	if leftStart.IsZero() || leftEnd.IsZero() || rightStart.IsZero() || rightEnd.IsZero() {
		return "unknown"
	}
	if leftEnd.Before(rightStart) || rightEnd.Before(leftStart) {
		return "disjoint"
	}
	return "overlap"
}

func resolveIntervalStart(in legInterval) time.Time {
	if in.start != nil {
		return *in.start
	}
	if in.end != nil {
		return *in.end
	}
	return time.Time{}
}

func resolveIntervalEnd(in legInterval) time.Time {
	if in.end != nil {
		return *in.end
	}
	if in.start != nil {
		return *in.start
	}
	return time.Time{}
}

func buildSufficiencySummary(s *models.SupplyChainPathSufficiency) string {
	if s == nil {
		return ""
	}
	switch strings.TrimSpace(s.Status) {
	case models.SupplyChainSufficiencyValidated:
		return "Continuity validated across all joins and legs"
	case models.SupplyChainSufficiencyForensicGap:
		return fmt.Sprintf("Forensic gaps detected: %d hard failures, %d open gaps", len(s.HardFailures), len(s.OpenGaps))
	default:
		return fmt.Sprintf("Heuristic continuity: %d warnings, %d open gaps", len(s.Warnings), len(s.OpenGaps))
	}
}

func uniqueTrimmed(values []string) []string {
	seen := make(map[string]struct{}, len(values))
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

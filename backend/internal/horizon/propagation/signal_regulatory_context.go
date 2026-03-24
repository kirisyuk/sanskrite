package propagation

import (
	"context"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// enrichSignalRegulatoryContextFromSeeds fills RegulatoryContext when propagation
// starts from MECHANISM, DECISION_POINT, or /gov/ organization fragments.
func (e *PropagationEngine) enrichSignalRegulatoryContextFromSeeds(ctx context.Context, signal *horizonmodels.SignalDescriptor, seedFragmentIDs []string) {
	if e == nil || signal == nil || len(seedFragmentIDs) == 0 || e.fragmentsRepo == nil {
		return
	}
	if signal.RegulatoryContext != nil && strings.TrimSpace(signal.RegulatoryContext.InstrumentPath) != "" {
		return
	}
	for _, id := range seedFragmentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		frag, err := e.fragmentsRepo.Get(ctx, id)
		if err != nil || frag == nil {
			continue
		}
		cp := strings.TrimSpace(frag.CanonicalPath)
		switch frag.Type {
		case horizonmodels.FragmentTypeMechanism:
			if !strings.HasPrefix(cp, "/mechanism/") {
				continue
			}
			rc := &horizonmodels.SignalRegulatoryContext{
				InstrumentPath: cp,
				Jurisdiction:   regulatoryJurisdictionHintFromPath(cp),
				Severity:       stringFromQualifiers(frag.Qualifiers, "severity", "regulatorySeverity", "riskSeverity"),
				EffectiveDate:  timeFromFragmentQualifiers(frag.Qualifiers),
			}
			signal.RegulatoryContext = rc
			if strings.TrimSpace(signal.Mechanism) == "" {
				signal.Mechanism = lastPathSegment(cp)
			}
			return
		case horizonmodels.FragmentTypeDecisionPoint:
			if strings.HasPrefix(cp, "/decision/") {
				signal.RegulatoryContext = &horizonmodels.SignalRegulatoryContext{
					Jurisdiction:  regulatoryJurisdictionHintFromPath(cp),
					EffectiveDate: timeFromFragmentQualifiers(frag.Qualifiers),
				}
				return
			}
		default:
			if frag.Type == horizonmodels.FragmentTypeOrganization && strings.HasPrefix(cp, "/gov/") {
				signal.RegulatoryContext = &horizonmodels.SignalRegulatoryContext{
					Jurisdiction: regulatoryJurisdictionHintFromPath(cp),
				}
				return
			}
		}
	}
}

func regulatoryJurisdictionHintFromPath(p string) string {
	p = strings.TrimSpace(p)
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if seg == "" {
			continue
		}
		if seg == "gov" || seg == "decision" || seg == "mechanism" {
			if i+1 < len(parts) && parts[i+1] != "" {
				return parts[i+1]
			}
		}
	}
	return ""
}

func lastPathSegment(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	return parts[len(parts)-1]
}

func stringFromQualifiers(q map[string]interface{}, keys ...string) string {
	if q == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := q[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func timeFromFragmentQualifiers(q map[string]interface{}) time.Time {
	return parseTimeFromMap(q, "effectiveDate", "effective_date", "regulatoryEffectiveDate")
}

func parseTimeFromMap(m map[string]interface{}, keys ...string) time.Time {
	if m == nil {
		return time.Time{}
	}
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case time.Time:
			if !t.IsZero() {
				return t
			}
		case string:
			s := strings.TrimSpace(t)
			if s == "" {
				continue
			}
			if tt, err := time.Parse(time.RFC3339, s); err == nil {
				return tt
			}
			if tt, err := time.Parse("2006-01-02", s); err == nil {
				return tt
			}
		}
	}
	return time.Time{}
}

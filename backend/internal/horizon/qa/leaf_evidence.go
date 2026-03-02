package qa

import (
	"context"
	"strings"
	"time"

	horizoninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// LeafEvidenceCollector collects physical signals at terminal nodes (FACILITY, DEPOSIT, AFFECTED_ZONE, GEO_REGION).
type LeafEvidenceCollector struct {
	eventFactsResolver horizoninterp.EventFactsResolver
}

// NewLeafEvidenceCollector creates a new LeafEvidenceCollector.
func NewLeafEvidenceCollector(eventFactsResolver horizoninterp.EventFactsResolver) *LeafEvidenceCollector {
	return &LeafEvidenceCollector{eventFactsResolver: eventFactsResolver}
}

// Leaf fragment types that accumulate real-world signals.
var leafFragmentTypes = map[string]bool{
	string(horizonmodels.FragmentTypeFacility):    true,
	string(horizonmodels.FragmentTypeDeposit):     true,
	string(horizonmodels.FragmentTypeAffectedZone): true,
	string(horizonmodels.FragmentTypeGeoRegion):   true,
}

// Collect returns leaf evidence for the given fragments.
// Fragments should already be loaded; only those with leaf types and State metrics are processed.
func (c *LeafEvidenceCollector) Collect(ctx context.Context, fragments []*horizonmodels.HorizonFragment, affectedFragmentIDs map[string]bool) []horizonmodels.LeafSignalEvidence {
	if len(fragments) == 0 {
		return nil
	}
	var out []horizonmodels.LeafSignalEvidence
	for _, frag := range fragments {
		if frag == nil {
			continue
		}
		if !leafFragmentTypes[string(frag.Type)] {
			continue
		}
		if affectedFragmentIDs != nil && !affectedFragmentIDs[frag.ID] {
			continue
		}
		ev := c.collectFromFragment(ctx, frag)
		out = append(out, ev...)
	}
	return out
}

func (c *LeafEvidenceCollector) collectFromFragment(ctx context.Context, frag *horizonmodels.HorizonFragment) []horizonmodels.LeafSignalEvidence {
	if len(frag.State) == 0 {
		return nil
	}
	name := frag.CanonicalPath
	if frag.Labels != nil {
		if en, ok := frag.Labels["en"]; ok && strings.TrimSpace(en) != "" {
			name = strings.TrimSpace(en)
		}
	}

	var out []horizonmodels.LeafSignalEvidence
	for metricPath, mv := range frag.State {
		if mv.Value <= 0 {
			continue
		}
		axis, modality := extractAxisModalityFromMetricPath(metricPath)
		if axis == "" {
			continue
		}
		// Prefer actual/confirmed modality
		if modality != "" && modality != "actual" && modality != "confirmed" {
			// Still include projected etc. but we could filter
		}
		summary := ""
		if c.eventFactsResolver != nil && len(mv.Provenance.Contributions) > 0 {
			for _, contrib := range mv.Provenance.Contributions {
				if contrib.SourceEventID != "" {
					if s, err := c.eventFactsResolver.ResolveEventSummary(ctx, contrib.SourceEventID); err == nil && s != "" {
						summary = strings.TrimSpace(s)
						break
					}
				}
			}
		}
		if summary == "" {
			summary = metricPath
		}
		obsAt := mv.ObservedAt
		if obsAt.IsZero() && mv.Provenance.ObservedAt != nil {
			obsAt = *mv.Provenance.ObservedAt
		}
		if obsAt.IsZero() {
			obsAt = time.Now().UTC()
		}
		out = append(out, horizonmodels.LeafSignalEvidence{
			FragmentID:    frag.ID,
			CanonicalPath: frag.CanonicalPath,
			Name:          name,
			SignalType:    inferSignalType(metricPath),
			Axis:          axis,
			Amplitude:     mv.Value,
			Modality:      modality,
			ObservedAt:    obsAt,
			Summary:       summary,
		})
	}
	return out
}

// extractAxisModalityFromMetricPath parses /metric/risk/physical_flow/supply/vat_increase or similar.
func extractAxisModalityFromMetricPath(path string) (axis, modality string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		return "", ""
	}
	// /metric/{polarity}/{axis}/{mechanism?}
	axis = parts[2]
	modality = ""
	// Modality might be in mechanism or we infer from path - for now axis is enough
	return axis, modality
}

func inferSignalType(metricPath string) string {
	if strings.Contains(metricPath, "ais") || strings.Contains(metricPath, "vessel") {
		return "ais_disruption"
	}
	if strings.Contains(metricPath, "satellite") || strings.Contains(metricPath, "fire") {
		return "satellite_fire"
	}
	if strings.Contains(metricPath, "outage") || strings.Contains(metricPath, "internet") {
		return "internet_outage"
	}
	if strings.Contains(metricPath, "conflict") || strings.Contains(metricPath, "geo") {
		return "conflict_event"
	}
	if strings.Contains(metricPath, "news") || strings.Contains(metricPath, "rss") {
		return "rss_news"
	}
	return "risk:temporal_anomaly"
}

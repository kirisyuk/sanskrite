package focusobject

import (
	"fmt"
	"strings"
)

func Markdown(snapshot *Snapshot) string {
	if snapshot == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Focus Object Report\n\n")
	fmt.Fprintf(&b, "## Focus\n")
	fmt.Fprintf(&b, "- Anchor: `%s`\n", snapshot.Focus.AnchorCanonicalPath)
	fmt.Fprintf(&b, "- Label: %s\n", snapshot.Focus.DisplayLabel)
	if len(snapshot.Focus.UniversePaths) > 0 {
		fmt.Fprintf(&b, "- Universe paths: %d\n", len(snapshot.Focus.UniversePaths))
	}
	fmt.Fprintf(&b, "- Runtime mode: `%s`\n", snapshot.Focus.RuntimeMode)
	fmt.Fprintf(&b, "- Source mode: `%s`\n\n", snapshot.Focus.SourceMode)

	fmt.Fprintf(&b, "## Position\n")
	fmt.Fprintf(&b, "- Risk pressure: %.2f\n", snapshot.Position.RiskPressure)
	fmt.Fprintf(&b, "- Opportunity pressure: %.2f\n", snapshot.Position.OpportunityPressure)
	fmt.Fprintf(&b, "- Artifact exposure: %.2f\n", snapshot.Position.ArtifactExposure)
	fmt.Fprintf(&b, "- Coverage score: %.2f\n", snapshot.Position.CoverageScore)
	fmt.Fprintf(&b, "- Monitoring readiness: %.2f\n", snapshot.Position.MonitoringReadiness)
	if len(snapshot.Position.DominantAxes) > 0 {
		fmt.Fprintf(&b, "- Dominant axes: %s\n", strings.Join(snapshot.Position.DominantAxes, ", "))
	}
	if len(snapshot.Position.Warnings) > 0 {
		fmt.Fprintf(&b, "- Warnings: %s\n", strings.Join(snapshot.Position.Warnings, " | "))
	}
	fmt.Fprintln(&b)

	if len(snapshot.Threats) > 0 {
		fmt.Fprintf(&b, "## Top Threats\n")
		for _, item := range snapshot.Threats {
			fmt.Fprintf(&b, "- %s (%.2f): %s\n", item.Headline, item.Score, item.Why)
		}
		fmt.Fprintln(&b)
	}
	if len(snapshot.Tailwinds) > 0 {
		fmt.Fprintf(&b, "## Top Tailwinds\n")
		for _, item := range snapshot.Tailwinds {
			fmt.Fprintf(&b, "- %s (%.2f): %s\n", item.Headline, item.Score, item.Why)
		}
		fmt.Fprintln(&b)
	}
	if len(snapshot.PathExposures) > 0 {
		fmt.Fprintf(&b, "## Path Exposure\n")
		for _, item := range snapshot.PathExposures {
			fmt.Fprintf(&b, "- %s (%.2f): %s\n", firstNonEmpty(item.DisplayName, item.ArtifactCanonicalKey), item.ExposureScore, item.Why)
		}
		fmt.Fprintln(&b)
	}
	if len(snapshot.AffinityClusters) > 0 {
		fmt.Fprintf(&b, "## Control Affinity\n")
		for _, item := range snapshot.AffinityClusters {
			fmt.Fprintf(&b, "- %s [%s]: %s\n", firstNonEmpty(item.Label, item.ClusterID), item.Kind, strings.Join(item.CanonicalPaths, ", "))
		}
		fmt.Fprintln(&b)
	}
	if len(snapshot.EndpointHits) > 0 {
		fmt.Fprintf(&b, "## Endpoint Hits\n")
		for _, item := range snapshot.EndpointHits {
			fmt.Fprintf(&b, "- %s (peak=%.2f, agg=%.2f, hops=%d, axis=%s, scope=%s): %s\n",
				firstNonEmpty(item.Label, item.CanonicalPath),
				item.PeakScore,
				item.AggregateScore,
				item.MaxHops,
				firstNonEmpty(item.DominantAxis, "n/a"),
				firstNonEmpty(item.Scope, "n/a"),
				item.Why,
			)
		}
		fmt.Fprintln(&b)
	}
	if len(snapshot.EndpointPackets) > 0 {
		fmt.Fprintf(&b, "## Endpoint Packets\n")
		for _, item := range snapshot.EndpointPackets {
			fmt.Fprintf(&b, "- %s (signals=%d, cluster=%s): %s\n",
				firstNonEmpty(item.Label, item.CanonicalPath),
				len(item.SignalIDs),
				firstNonEmpty(item.ClusterID, "none"),
				item.Why,
			)
			if len(item.Context) > 0 {
				ctx := make([]string, 0, minInt(len(item.Context), 4))
				for _, ctxItem := range item.Context[:minInt(len(item.Context), 4)] {
					ctx = append(ctx, fmt.Sprintf("%s[%s]", firstNonEmpty(ctxItem.Label, ctxItem.CanonicalPath), ctxItem.Relationship))
				}
				fmt.Fprintf(&b, "  context: %s\n", strings.Join(ctx, "; "))
			}
		}
		fmt.Fprintln(&b)
	}
	if len(snapshot.ImpactedEntities) > 0 {
		fmt.Fprintf(&b, "## Legacy Impacted Entities\n")
		for _, item := range snapshot.ImpactedEntities {
			fmt.Fprintf(&b, "- %s (%.2f, hops=%d, %s): %s\n", firstNonEmpty(item.Label, item.CanonicalPath), item.ImpactScore, item.Hops, firstNonEmpty(item.Classification, "cascade"), item.Why)
		}
		fmt.Fprintln(&b)
	}
	if len(snapshot.Propagation) > 0 {
		fmt.Fprintf(&b, "## Propagation Audit\n")
		for _, audit := range snapshot.Propagation {
			fmt.Fprintf(&b, "- %s [signal=%s, score=%.2f, hops<=%d]\n", audit.Headline, audit.SignalID, audit.SignalScore, audit.MaxHops)
			if len(audit.Reached) > 0 {
				reached := make([]string, 0, minInt(len(audit.Reached), 4))
				for _, item := range audit.Reached[:minInt(len(audit.Reached), 4)] {
					reached = append(reached, fmt.Sprintf("%s %.2f/%dh/%s", firstNonEmpty(item.Label, item.CanonicalPath), item.Score, item.Hops, firstNonEmpty(item.Classification, "cascade")))
				}
				fmt.Fprintf(&b, "  reached: %s\n", strings.Join(reached, "; "))
			}
			for _, step := range audit.Steps[:minInt(len(audit.Steps), 6)] {
				fmt.Fprintf(&b, "  %s -> %s [%s%s %s->%s] in=%.2f weight=%.2f amp=%.2f crit=%.2f out=%.2f %s",
					step.FromPath,
					step.ToPath,
					step.EdgeType,
					map[bool]string{true: ":reverse", false: ""}[step.Reverse],
					firstNonEmpty(step.AxisIn, audit.Axis),
					firstNonEmpty(step.AxisOut, step.AxisIn),
					step.InputScore,
					step.EdgeWeight,
					step.Amplification,
					step.Criticality,
					step.OutputScore,
					step.Outcome,
				)
				if step.TransformReason != "" {
					fmt.Fprintf(&b, " (xf=%s %.2f)", step.TransformReason, step.TransformFactor)
				}
				if step.Reason != "" {
					fmt.Fprintf(&b, " (%s)", step.Reason)
				}
				fmt.Fprintln(&b)
			}
			if len(audit.PathFamilies) > 0 {
				families := make([]string, 0, minInt(len(audit.PathFamilies), 3))
				for _, item := range audit.PathFamilies[:minInt(len(audit.PathFamilies), 3)] {
					families = append(families, fmt.Sprintf("%s %.2f", firstNonEmpty(item.Label, item.Key), item.Score))
				}
				fmt.Fprintf(&b, "  families: %s\n", strings.Join(families, "; "))
			}
		}
		fmt.Fprintln(&b)
	}
	if len(snapshot.TodayMentions) > 0 {
		fmt.Fprintf(&b, "## Today Mentions\n")
		for _, item := range snapshot.TodayMentions {
			fmt.Fprintf(&b, "- %s (%.2f): %s\n", item.Headline, item.Score, item.Why)
		}
	}
	return b.String()
}

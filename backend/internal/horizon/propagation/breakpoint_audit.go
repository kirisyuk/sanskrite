package propagation

import (
	"sort"
	"strings"
)

type BreakpointReasonSummary struct {
	Reason string
	Count  int
}

type BreakpointFamilySummary struct {
	Key             string
	Drops           int
	ThresholdDrops  int
	TransferSkips   int
	HopLimitDrops   int
	AvgAmplitudeOut float64
	MaxHops         int
	SamplePaths     []string
}

type BreakpointAuditSummary struct {
	TotalDrops          int
	ThresholdDrops      int
	TransferSkips       int
	HopLimitDrops       int
	BusinessBreakpoints int
	PathBreakpoints     int
	TopFamilies         []BreakpointFamilySummary
	TopReasons          []BreakpointReasonSummary
}

func AuditTraceBreakpoints(trace []SignalPropagationTraceEvent, limit int) BreakpointAuditSummary {
	summary := BreakpointAuditSummary{}
	if len(trace) == 0 {
		return summary
	}
	if limit <= 0 {
		limit = 4
	}
	type familyAgg struct {
		BreakpointFamilySummary
		amplitudeSum float64
	}
	familyAggs := make(map[string]*familyAgg)
	reasonCounts := make(map[string]int)

	for _, ev := range trace {
		if strings.TrimSpace(ev.Outcome) != "dropped" {
			continue
		}
		summary.TotalDrops++
		reason := strings.TrimSpace(ev.Reason)
		if reason != "" {
			reasonCounts[reason]++
		}
		switch reason {
		case "damped_below_threshold":
			summary.ThresholdDrops++
		case "transfer_skip":
			summary.TransferSkips++
		case "hop_limit":
			summary.HopLimitDrops++
		}
		families := classifyPathFamilies(ev)
		if len(families) == 0 {
			families = []string{"direct_dependency"}
		}
		for _, family := range families {
			if family == "" {
				continue
			}
			agg := familyAggs[family]
			if agg == nil {
				agg = &familyAgg{
					BreakpointFamilySummary: BreakpointFamilySummary{
						Key:         family,
						SamplePaths: make([]string, 0, 3),
					},
				}
				familyAggs[family] = agg
			}
			agg.Drops++
			agg.amplitudeSum += ev.AmplitudeOut
			if ev.Hops > agg.MaxHops {
				agg.MaxHops = ev.Hops
			}
			switch reason {
			case "damped_below_threshold":
				agg.ThresholdDrops++
			case "transfer_skip":
				agg.TransferSkips++
			case "hop_limit":
				agg.HopLimitDrops++
			}
			if sample := strings.TrimSpace(ev.ToPath); sample != "" && len(agg.SamplePaths) < 3 {
				agg.SamplePaths = appendUniqueString(agg.SamplePaths, sample)
			}
		}
	}

	families := make([]BreakpointFamilySummary, 0, len(familyAggs))
	for _, agg := range familyAggs {
		if agg == nil || agg.Drops == 0 {
			continue
		}
		agg.AvgAmplitudeOut = agg.amplitudeSum / float64(agg.Drops)
		if agg.Key == "chokepoint_logistics" || agg.Key == "energy_supply_chain" || agg.Key == "facility_dependency" || agg.Key == "market_exposure" {
			summary.BusinessBreakpoints += agg.Drops
		}
		families = append(families, agg.BreakpointFamilySummary)
	}
	sort.Slice(families, func(i, j int) bool {
		if families[i].Drops == families[j].Drops {
			return families[i].Key < families[j].Key
		}
		return families[i].Drops > families[j].Drops
	})
	summary.PathBreakpoints = len(families)
	if len(families) > limit {
		summary.TopFamilies = families[:limit]
	} else {
		summary.TopFamilies = families
	}

	reasons := make([]BreakpointReasonSummary, 0, len(reasonCounts))
	for reason, count := range reasonCounts {
		reasons = append(reasons, BreakpointReasonSummary{Reason: reason, Count: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].Count == reasons[j].Count {
			return reasons[i].Reason < reasons[j].Reason
		}
		return reasons[i].Count > reasons[j].Count
	})
	if len(reasons) > limit {
		summary.TopReasons = reasons[:limit]
	} else {
		summary.TopReasons = reasons
	}
	return summary
}

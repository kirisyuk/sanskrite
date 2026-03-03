package qa

import (
	"fmt"
	"strings"
	"time"
)

// GroundingQuery is a targeted search query derived from signal gaps.
type GroundingQuery struct {
	Query             string      `json:"query"`
	TargetFragmentIDs []string    `json:"targetFragmentIds"`
	TargetAxes        []string    `json:"targetAxes"`
	SourceGaps        []SignalGap `json:"sourceGaps"`
}

// GroundingQueryBuilder produces search queries from SignalGap entries,
// grouping by entity to avoid redundant lookups.
type GroundingQueryBuilder struct{}

func NewGroundingQueryBuilder() *GroundingQueryBuilder {
	return &GroundingQueryBuilder{}
}

type gapGroup struct {
	fragmentID string
	entityName string
	axes       []string
	gaps       []SignalGap
}

// Build creates one GroundingQuery per entity (FragmentID), merging all axes
// for that entity into a single broader search query.
func (b *GroundingQueryBuilder) Build(gaps []SignalGap, question string) []GroundingQuery {
	if len(gaps) == 0 {
		return nil
	}

	groups := b.groupByFragment(gaps)
	year := fmt.Sprintf("%d", time.Now().Year())

	queries := make([]GroundingQuery, 0, len(groups))
	for _, g := range groups {
		axisDesc := b.axisDescription(g.axes)
		q := fmt.Sprintf("%s %s %s", g.entityName, axisDesc, year)
		queries = append(queries, GroundingQuery{
			Query:             strings.TrimSpace(q),
			TargetFragmentIDs: []string{g.fragmentID},
			TargetAxes:        g.axes,
			SourceGaps:        g.gaps,
		})
	}
	return queries
}

func (b *GroundingQueryBuilder) groupByFragment(gaps []SignalGap) []gapGroup {
	order := make([]string, 0)
	idx := map[string]int{}
	var groups []gapGroup

	for _, gap := range gaps {
		i, ok := idx[gap.FragmentID]
		if !ok {
			i = len(groups)
			idx[gap.FragmentID] = i
			order = append(order, gap.FragmentID)
			groups = append(groups, gapGroup{
				fragmentID: gap.FragmentID,
				entityName: gap.EntityName,
			})
		}
		g := &groups[i]
		if !containsString(g.axes, gap.Axis) {
			g.axes = append(g.axes, gap.Axis)
		}
		g.gaps = append(g.gaps, gap)
	}
	return groups
}

var axisLabels = map[string]string{
	"legal/access":           "legal access",
	"legal/compliance":       "compliance risk",
	"financial/settlement":   "financial settlement",
	"financial/credit":       "credit risk",
	"physical_flow/supply":   "supply availability",
	"physical_flow/demand":   "demand trends",
	"logistics/capacity":     "logistics capacity",
	"logistics/latency":      "logistics latency",
	"operational/continuity": "operational continuity",
	"info/uncertainty":       "regulatory uncertainty",
	"info/reputation":        "reputation",
	"geo/conflict":           "geopolitical risk",
}

func (b *GroundingQueryBuilder) axisDescription(axes []string) string {
	labels := make([]string, 0, len(axes))
	for _, a := range axes {
		if l, ok := axisLabels[a]; ok {
			labels = append(labels, l)
		} else {
			labels = append(labels, strings.ReplaceAll(a, "/", " "))
		}
	}
	return strings.Join(labels, " ")
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

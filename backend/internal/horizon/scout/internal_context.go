package scout

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func truncText(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func fragmentBestName(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if s := strings.TrimSpace(f.Labels["en"]); s != "" {
		return s
	}
	if s := strings.TrimSpace(f.Identifiers["shortName"]); s != "" {
		return s
	}
	if s := strings.TrimSpace(f.Identifiers["legalName"]); s != "" {
		return s
	}
	return strings.TrimSpace(f.CanonicalPath)
}

// buildGraphBackedInternalContext builds deterministic internal context from existing system data:
// - target fragment profile
// - 1-hop incoming/outgoing graph neighborhood
// - latest ontology facts for canonical path
//
// This is used as fallback when source/doc aggregation returns no content.
func (e *ScoutEngine) buildGraphBackedInternalContext(ctx context.Context, tenantID, targetCanonicalPath string) string {
	if e == nil || e.fragmentsRepo == nil {
		return ""
	}
	targetCanonicalPath = strings.TrimSpace(targetCanonicalPath)
	if targetCanonicalPath == "" {
		return ""
	}
	target, err := e.fragmentsRepo.GetByCanonicalPath(ctx, targetCanonicalPath)
	if err != nil || target == nil || strings.TrimSpace(target.ID) == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("INTERNAL GRAPH CONTEXT (deterministic, from system state)\n")
	b.WriteString("Target:\n")
	b.WriteString(fmt.Sprintf("- canonicalPath: %s\n", strings.TrimSpace(target.CanonicalPath)))
	b.WriteString(fmt.Sprintf("- type: %s\n", strings.TrimSpace(string(target.Type))))
	if name := fragmentBestName(target); name != "" {
		b.WriteString(fmt.Sprintf("- name: %s\n", name))
	}
	if s := strings.TrimSpace(target.Description); s != "" {
		b.WriteString(fmt.Sprintf("- description: %s\n", truncText(s, 280)))
	}
	if s := strings.TrimSpace(target.Identifiers["legalName"]); s != "" {
		b.WriteString(fmt.Sprintf("- legalName: %s\n", s))
	}
	if s := strings.TrimSpace(target.Identifiers["shortName"]); s != "" {
		b.WriteString(fmt.Sprintf("- shortName: %s\n", s))
	}
	if len(target.State) > 0 {
		keys := make([]string, 0, len(target.State))
		for k := range target.State {
			k = strings.TrimSpace(k)
			if k != "" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) > 10 {
			keys = keys[:10]
		}
		b.WriteString("- stateMetrics:\n")
		for _, k := range keys {
			mv := target.State[k]
			b.WriteString(fmt.Sprintf("  - %s = %.4f", k, mv.Value))
			if mv.Unit != nil && strings.TrimSpace(*mv.Unit) != "" {
				b.WriteString(" ")
				b.WriteString(strings.TrimSpace(*mv.Unit))
			}
			b.WriteString("\n")
		}
	}

	neighbors := 0
	if e.edgesRepo != nil {
		appendEdgeLines := func(title string, edges []*models.DependencyEdge, outgoing bool) {
			if len(edges) == 0 {
				return
			}
			b.WriteString(title + ":\n")
			for _, ed := range edges {
				if ed == nil {
					continue
				}
				otherID := strings.TrimSpace(ed.SourceFragmentID)
				if outgoing {
					otherID = strings.TrimSpace(ed.TargetFragmentID)
				}
				if otherID == "" {
					continue
				}
				other, err := e.fragmentsRepo.Get(ctx, otherID)
				if err != nil || other == nil {
					continue
				}
				neighbors++
				b.WriteString(fmt.Sprintf("  - [%s] %s (%s)\n",
					strings.TrimSpace(string(ed.Type)),
					strings.TrimSpace(other.CanonicalPath),
					truncText(fragmentBestName(other), 80),
				))
			}
		}

		outEdges, _ := e.edgesRepo.ListBySource(ctx, "global", target.ID, nil, 15)
		inEdges, _ := e.edgesRepo.ListByTarget(ctx, "global", target.ID, nil, 15)
		appendEdgeLines("OutgoingEdges", outEdges, true)
		appendEdgeLines("IncomingEdges", inEdges, false)
	}

	factsCount := 0
	if e.factsRepo != nil {
		loadFacts := func(tid string) []*coreModels.OntologyFact {
			facts, ferr := e.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, strings.TrimSpace(tid), targetCanonicalPath, time.Time{}, 12)
			if ferr != nil {
				return nil
			}
			return facts
		}
		facts := loadFacts(tenantID)
		if len(facts) == 0 && strings.TrimSpace(tenantID) != "global" {
			facts = loadFacts("global")
		}
		if len(facts) > 0 {
			b.WriteString("RecentFacts:\n")
			for _, f := range facts {
				if f == nil {
					continue
				}
				factsCount++
				title := truncText(f.Title, 100)
				if title == "" {
					title = truncText(f.Summary, 100)
				}
				line := fmt.Sprintf("  - [%s/%s] %s", strings.TrimSpace(f.Kind), strings.TrimSpace(f.Type), title)
				if f.ObservedAt != nil {
					line += " @" + f.ObservedAt.UTC().Format(time.RFC3339)
				}
				b.WriteString(line + "\n")
			}
		}
	}

	// If we only have an almost-empty shell, don't treat it as useful context.
	if neighbors == 0 && factsCount == 0 && strings.TrimSpace(target.Description) == "" && len(target.State) == 0 {
		return ""
	}
	return strings.TrimSpace(b.String())
}

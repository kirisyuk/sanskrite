package qa

import (
	"context"
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// AnswerContextBuilder assembles the LLM prompt context for answer synthesis.
type AnswerContextBuilder struct {
	fillReport *FillReport
}

// NewAnswerContextBuilder creates a new AnswerContextBuilder.
func NewAnswerContextBuilder() *AnswerContextBuilder {
	return &AnswerContextBuilder{}
}

// WithFillReport attaches a best-effort grounding report. When set,
// a "RECENTLY ENRICHED SIGNALS" section is appended to the context.
func (b *AnswerContextBuilder) WithFillReport(report *FillReport) *AnswerContextBuilder {
	b.fillReport = report
	return b
}

const contextTokenBudget = 24000 // ~6000 tokens at ~4 chars/token

type contextHintFlags struct {
	skipAlternatives bool
	skipGaps         bool
	prioritizeImpact bool
	prioritizeTraces bool
}

func parseContextHints(hints []string) contextHintFlags {
	var f contextHintFlags
	for _, h := range hints {
		switch h {
		case "skip_alternatives":
			f.skipAlternatives = true
		case "prioritize_impact":
			f.prioritizeImpact = true
		case "prioritize_traces":
			f.prioritizeTraces = true
		case "skip_gaps":
			f.skipGaps = true
		}
	}
	return f
}

// BuildContext assembles the structured context for the AnswerSynthesizer LLM call.
// Token budget: ~6000 tokens (~24000 chars). Sections are capped and skipped when over budget.
// ContextHints from the QuestionPlan control section ordering and inclusion.
// Alternatives come from DomainAnswerSnapshot.
func (b *AnswerContextBuilder) BuildContext(
	ctx context.Context,
	plan *horizonmodels.QuestionPlan,
	simResult *horizonmodels.SimulationResult,
	domainSnapshot *horizonmodels.DomainAnswerSnapshot,
	artifactSummaries []string,
) (string, error) {
	hints := parseContextHints(plan.ContextHints)

	var sb strings.Builder
	overBudget := func() bool { return sb.Len() > contextTokenBudget }

	// Header — always included
	sb.WriteString(fmt.Sprintf("QUESTION: %s\n", plan.NormalizedQuestion))
	sb.WriteString(fmt.Sprintf("ANSWER MODE: %s\n", plan.AnswerMode))
	sb.WriteString(fmt.Sprintf("TIME HORIZON: %s\n\n", plan.TimeHorizon))

	// SCENARIO PROJECTION (Simulation) — explicitly labeled as hypothetical
	// This prevents the LLM from confusing "what-if" simulation with "ground truth".
	if len(simResult.NodeImpacts) > 0 {
		sb.WriteString("=== SCENARIO PROJECTION (HYPOTHETICAL SIMULATION RESULTS) ===\n")
		sb.WriteString("NOTE: These are projected impacts based on the graph structure. They are NOT current real-world observations.\n")
		sb.WriteString("Use these to answer \"what would happen\" or \"how would it spread\".\n")
		
		impactSection := b.buildImpactMapSection(simResult)
		sb.WriteString(impactSection)
		
		tracesSection := b.buildTracesSection(simResult, true)
		sb.WriteString(tracesSection)
	} else {
		sb.WriteString("(No simulation impacts projected)\n\n")
	}

	// REAL-WORLD EVIDENCE (Ground Truth)
	sb.WriteString("=== REAL-WORLD EVIDENCE (GROUND TRUTH) ===\n")
	sb.WriteString("NOTE: These are actual observed facts (leaf signals) or known supply chains. Use these to ground your answer in reality.\n")

	// SUPPLY CHAIN PATHS (from artifact summaries)
	if !overBudget() {
		sb.WriteString("\n-- KNOWN SUPPLY CHAINS (MaterializedArtifacts) --\n")
		for _, s := range artifactSummaries {
			sb.WriteString(fmt.Sprintf("- %s\n", s))
		}
	}

	// LEAF EVIDENCE — cap at 10 for better grounding
	if !overBudget() {
		sb.WriteString("\n-- OBSERVED SIGNALS (Leaf Evidence) --\n")
		maxLeaf := 10
		for i, le := range simResult.LeafEvidence {
			if i >= maxLeaf {
				break
			}
			sb.WriteString(fmt.Sprintf("[L%d] %s: %s = %.2f — %s [%s, %s]\n",
				i+1, le.CanonicalPath, le.Axis, le.Amplitude, le.Summary, le.Modality, le.ObservedAt.Format("2006-01-02 15:04")))
		}
		if len(simResult.LeafEvidence) == 0 {
			sb.WriteString("(no external leaf signals observed)\n")
		}
		sb.WriteString("\n")
	}

	// PRE-COMPUTED IMPACT BREAKDOWN — cap at 10 nodes for budget
	if !overBudget() {
		sb.WriteString("=== IMPACT BREAKDOWN (from simulation — use for impactBreakdown in JSON) ===\n")
		for i, ni := range simResult.NodeImpacts {
			if i >= 10 {
				break
			}
			for axis, amp := range ni.AxisAmplitudes {
				entity := ni.CanonicalPath
				if ni.Name != "" {
					entity = ni.Name
				}
				sb.WriteString(fmt.Sprintf("- entity=%s axis=%s amplitude=%.2f hops=%d\n", entity, axis, amp, ni.Hops))
			}
		}
		sb.WriteString("\n")
	}

	// QUANTITATIVE HINT
	if !overBudget() && b.shouldAddQuantitativeHint(plan, simResult) {
		sb.WriteString("=== QUANTITATIVE HINT (use for cost/price impact — provide numeric ranges) ===\n")
		sb.WriteString("- Pass-through heuristic: amplitude 0.6+ → 60-80% pass-through; 0.3-0.6 → 30-50%; <0.3 → 10-30%\n")
		sb.WriteString("- For VAT/tariff: input % × pass-through × amplitude ≈ cost delta at that stage. Example: 5% VAT, amplitude 0.72 at nickel ore → ~3-4% cost increase there; amplitude 0.12 at battery cell → ~0.5-1.5% at final product.\n")
		sb.WriteString("- ALWAYS provide numeric ranges when the question asks about cost/price. Avoid only \"likely\" or \"potentially\" without numbers.\n")
		sb.WriteString("\n")
	}

	// EXISTING ANALYSIS (DomainAnswerSnapshot)
	if !overBudget() && domainSnapshot != nil {
		sb.WriteString("=== EXISTING ANALYSIS ===\n")
		if domainSnapshot.Thesis.Text != "" {
			sb.WriteString(fmt.Sprintf("Domain thesis: %s\n", domainSnapshot.Thesis.Text))
		}
		if len(domainSnapshot.CriticalDependencies) > 0 {
			sb.WriteString("Critical deps: ")
			for i, cd := range domainSnapshot.CriticalDependencies {
				if i > 0 {
					sb.WriteString("; ")
				}
				sb.WriteString(fmt.Sprintf("[%s -> %s: criticality %.2f]", cd.SourceCanonicalPath, cd.TargetCanonicalPath, cd.TopDependencyScore))
			}
			sb.WriteString("\n")
		}
		if len(domainSnapshot.ConcentrationEvidence) > 0 {
			sb.WriteString("Concentration / market share: ")
			for i, ce := range domainSnapshot.ConcentrationEvidence {
				if i > 0 {
					sb.WriteString("; ")
				}
				part := ce.MarketCanonicalPath
				if ce.CR4 != nil {
					part += fmt.Sprintf(" CR4=%.2f", *ce.CR4)
				}
				if ce.DataGap != "" {
					part += " (" + ce.DataGap + ")"
				}
				sb.WriteString(part)
			}
			sb.WriteString("\n")
		}
		if !hints.skipAlternatives && len(domainSnapshot.Alternatives) > 0 {
			sb.WriteString("Alternatives: ")
			for i, alt := range domainSnapshot.Alternatives {
				if i > 0 {
					sb.WriteString("; ")
				}
				part := alt.Title
				if alt.CostDelta != "" {
					part += " cost " + alt.CostDelta
				}
				if alt.Feasibility != "" {
					part += " (" + alt.Feasibility + ")"
				}
				sb.WriteString("[" + part + "]")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// DATA GAPS — skippable via "skip_gaps" hint, cap at 3 for budget
	if !overBudget() && !hints.skipGaps {
		sb.WriteString("=== DATA GAPS ===\n")
		if domainSnapshot != nil && len(domainSnapshot.DataGaps) > 0 {
			maxGaps := 3
			for i, g := range domainSnapshot.DataGaps {
				if i >= maxGaps {
					break
				}
				desc := g.Hint
				if desc == "" {
					desc = g.GapType
				}
				if desc != "" {
					sb.WriteString(fmt.Sprintf("- %s\n", desc))
				}
			}
		}
		sb.WriteString("\n")
	}

	// RECENTLY ENRICHED SIGNALS — from best-effort grounding (optional)
	if !overBudget() && b.fillReport != nil && len(b.fillReport.FilledGaps) > 0 {
		sb.WriteString("=== RECENTLY ENRICHED SIGNALS (from best-effort web grounding — treat as supplementary) ===\n")
		maxEnriched := 5
		for i, fg := range b.fillReport.FilledGaps {
			if i >= maxEnriched {
				break
			}
			sb.WriteString(fmt.Sprintf("[E%d] %s — axis=%s amplitude=%.2f confidence=%.2f\n",
				i+1, fg.Gap.CanonicalPath, fg.InjectedAxis, fg.Amplitude, fg.Confidence))
			for _, src := range fg.Sources {
				title := src.Title
				if title == "" {
					title = src.URL
				}
				sb.WriteString(fmt.Sprintf("     source: %s (%s)\n", title, src.URL))
			}
		}
		if len(b.fillReport.FilledGaps) > maxEnriched {
			sb.WriteString(fmt.Sprintf("(%d more enriched signals omitted)\n", len(b.fillReport.FilledGaps)-maxEnriched))
		}
		if len(b.fillReport.FailedGaps) > 0 {
			sb.WriteString(fmt.Sprintf("Note: %d gaps could not be filled (data may be incomplete).\n", len(b.fillReport.FailedGaps)))
		}
		sb.WriteString("\n")
	}

	// INSTRUCTIONS — always included
	enrichedHint := ""
	if b.fillReport != nil && len(b.fillReport.FilledGaps) > 0 {
		enrichedHint = "\n- ENRICHED SIGNALS [E1]-[EN]: supplementary web-grounded data. Cite as [E1], [E2] etc. Lower confidence than primary evidence."
	}

	sb.WriteString(fmt.Sprintf(`ROLE: You are an expert Supply Chain Risk Analyst advising a C-level executive.
TONE: Professional, concise, actionable. No fluff. No technical jargon (amplitude, hops).

TRANSLATION RULES (Internal Math -> Business Language):
- Amplitude 0.75 - 1.00 → CRITICAL Impact (e.g. stoppage)
- Amplitude 0.40 - 0.74 → HIGH Impact (e.g. major cost spike)
- Amplitude 0.15 - 0.39 → MODERATE Impact
- Amplitude < 0.15      → LOW Impact

MANDATORY OUTPUT STRUCTURE (JSON):
{
  "executiveSummary": "One powerful sentence summarizing the net impact.",
  "riskLevel": "CRITICAL" | "HIGH" | "MODERATE" | "LOW",
  "riskScenario": "A clear narrative: The event at [Source] causes [Impact A], which cascades to [Impact B], ultimately affecting [Target].",
  "recommendations": ["Action 1", "Action 2", "Action 3"],
  "text": "Full detailed analysis...",
  "confidence": 0.0-1.0,
  "evidence": [...],
  "impactBreakdown": [...]
}

%s
- impactBreakdown: derive from SCENARIO PROJECTION — entity, axis, amplitude, hops.
- QUANTIFICATION: When QUANTITATIVE HINT is present, translate amplitudes into estimated % impacts on cost/time.
- Recommendations: Suggest specific mitigation steps (diversify, stockpile, hedge) based on the graph structure.
`, enrichedHint))

	return sb.String(), nil
}

// buildImpactMapSection renders the IMPACT MAP section.
// Capped at 15 nodes for token budget.
func (b *AnswerContextBuilder) buildImpactMapSection(
	simResult *horizonmodels.SimulationResult,
) string {
	const maxNodes = 15
	var sb strings.Builder

	sb.WriteString("-- PROJECTED NODE IMPACTS (Simulation) --\n")
	topK := maxNodes
	if len(simResult.NodeImpacts) < topK {
		topK = len(simResult.NodeImpacts)
	}
	for i := 0; i < topK; i++ {
		ni := simResult.NodeImpacts[i]
		// Don't use [1], [2] numbering to avoid confusion with evidence citations.
		sb.WriteString(fmt.Sprintf("> Node: %s\n", ni.CanonicalPath))
		if ni.Name != "" && ni.Name != ni.CanonicalPath {
			sb.WriteString(fmt.Sprintf("  Name: %s\n", ni.Name))
		}
		for axis, amp := range ni.AxisAmplitudes {
			sb.WriteString(fmt.Sprintf("  - %s: %.2f (hops: %d)\n", axis, amp, ni.Hops))
		}
	}
	if topK == 0 {
		sb.WriteString("(no nodes affected in simulation)\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

// buildTracesSection renders the PROPAGATION CHAINS section.
// Capped at 3 chains for token budget.
func (b *AnswerContextBuilder) buildTracesSection(
	simResult *horizonmodels.SimulationResult,
	hasImpacts bool,
) string {
	const maxChains = 3
	var sb strings.Builder

	sb.WriteString("-- PROJECTED PROPAGATION PATHS --\n")
	count := 0
	for i, tp := range simResult.TracePaths {
		if count >= maxChains {
			break
		}
		if len(tp.NodeCanonicalPaths) == 0 {
			continue
		}
		pathStr := ""
		for j, p := range tp.NodeCanonicalPaths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if pathStr != "" {
				pathStr += " → "
			}
			ampStr := ""
			if j < len(tp.Amplitudes) && tp.Amplitudes[j] > 0 {
				ampStr = fmt.Sprintf(" (%.2f)", tp.Amplitudes[j])
			}
			pathStr += p + ampStr
		}
		if pathStr != "" {
			sb.WriteString(fmt.Sprintf("Path %d [%s]: %s\n", i+1, tp.Axis, pathStr))
			count++
		}
	}
	if len(simResult.TracePaths) == 0 && hasImpacts {
		sb.WriteString("(paths available in IMPACT MAP above)\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

// shouldAddQuantitativeHint returns true when the question likely asks about cost/price impact
// and we have amplitude data to support numeric ranges.
func (b *AnswerContextBuilder) shouldAddQuantitativeHint(plan *horizonmodels.QuestionPlan, simResult *horizonmodels.SimulationResult) bool {
	if plan.AnswerMode != "impact_assessment" {
		return false
	}
	if len(simResult.NodeImpacts) == 0 {
		return false
	}
	q := strings.ToLower(plan.NormalizedQuestion)
	if strings.Contains(q, "cost") || strings.Contains(q, "price") || strings.Contains(q, "vat") ||
		strings.Contains(q, "tariff") || strings.Contains(q, "tax") || strings.Contains(q, "%") {
		return true
	}
	return false
}

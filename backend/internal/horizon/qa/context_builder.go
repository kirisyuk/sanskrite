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
	sb.WriteString(fmt.Sprintf("TIME HORIZON: %s\n", plan.TimeHorizon))
	sb.WriteString(fmt.Sprintf("SCENARIO TYPE: %s\n\n", b.describeScenarioType(plan)))

	sb.WriteString("=== DECISION BRIEF ===\n")
	sb.WriteString(fmt.Sprintf("Assumed shock: %s\n", b.describeHypotheticalSignals(plan)))
	sb.WriteString(fmt.Sprintf("Primary observation target: %s\n", b.describeTargetFilter(plan)))
	sb.WriteString(fmt.Sprintf("What matters most: %s\n\n", b.describeDecisionFocus(plan)))

	sb.WriteString("=== PROJECTED CONSEQUENCES ===\n")
	sb.WriteString("These are projected business consequences from the graph. They are hypothetical unless confirmed by observed evidence below.\n")
	if len(simResult.NodeImpacts) == 0 {
		sb.WriteString("(No downstream propagation was projected from the current assumptions.)\n")
	}

	sb.WriteString(b.buildScenarioWindowsSection(simResult))
	impactSection := b.buildImpactMapSection(simResult)
	sb.WriteString(impactSection)

	tracesSection := b.buildTracesSection(simResult, len(simResult.NodeImpacts) > 0)
	sb.WriteString(tracesSection)

	// REAL-WORLD EVIDENCE (Ground Truth)
	sb.WriteString("=== GROUND TRUTH AND KNOWN STRUCTURE ===\n")
	sb.WriteString("Use this section to distinguish what is already observed from what is only projected.\n")

	// SUPPLY CHAIN PATHS (from artifact summaries)
	if !overBudget() {
		sb.WriteString("\n-- KNOWN PATHS / CHAINS --\n")
		for _, s := range artifactSummaries {
			sb.WriteString(fmt.Sprintf("- %s\n", s))
		}
	}

	// LEAF EVIDENCE — cap at 10 for better grounding
	if !overBudget() {
		sb.WriteString("\n-- OBSERVED SIGNALS --\n")
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
		sb.WriteString("=== STRUCTURED IMPACT INPUT ===\n")
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
		sb.WriteString("=== QUANTIFICATION HINT ===\n")
		sb.WriteString("- Pass-through heuristic: amplitude 0.6+ → 60-80% pass-through; 0.3-0.6 → 30-50%; <0.3 → 10-30%\n")
		sb.WriteString("- For VAT/tariff: input % × pass-through × amplitude ≈ cost delta at that stage. Example: 5% VAT, amplitude 0.72 at nickel ore → ~3-4% cost increase there; amplitude 0.12 at battery cell → ~0.5-1.5% at final product.\n")
		sb.WriteString("- ALWAYS provide numeric ranges when the question asks about cost/price. Avoid only \"likely\" or \"potentially\" without numbers.\n")
		sb.WriteString("\n")
	}

	// EXISTING ANALYSIS (DomainAnswerSnapshot)
	if !overBudget() && domainSnapshot != nil {
		sb.WriteString("=== EXISTING ANALYSIS / BASELINE ===\n")
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
		sb.WriteString("=== UNCERTAINTY / DATA GAPS ===\n")
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
		sb.WriteString("=== RECENTLY ENRICHED SIGNALS ===\n")
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

	sb.WriteString(fmt.Sprintf(`ROLE: You are a strategic risk analyst advising an executive on "what happens if..." questions.
TONE: Decision-first, concrete, business-facing. No internal graph jargon in the final answer.

TRANSLATION RULES (Internal Math -> Business Language):
- Amplitude 0.75 - 1.00 → CRITICAL Impact (e.g. stoppage)
- Amplitude 0.40 - 0.74 → HIGH Impact (e.g. major cost spike)
- Amplitude 0.15 - 0.39 → MODERATE Impact
- Amplitude < 0.15      → LOW Impact

MANDATORY OUTPUT STRUCTURE (JSON):
{
  "executiveSummary": "One direct sentence that answers the business question.",
  "riskLevel": "CRITICAL" | "HIGH" | "MODERATE" | "LOW",
  "riskScenario": "Short narrative: shock -> transmission path -> remote consequence.",
  "firstOrderEffects": [
    {
      "summary": "What changes first",
      "horizon": "immediate|short_term|medium_term|long_term",
      "severity": "CRITICAL|HIGH|MODERATE|LOW",
      "affectedEntities": ["entity names or canonical paths"],
      "mechanism": "short causal description"
    }
  ],
  "secondOrderEffects": [
    {
      "summary": "What changes later or farther downstream",
      "horizon": "immediate|short_term|medium_term|long_term",
      "severity": "CRITICAL|HIGH|MODERATE|LOW",
      "affectedEntities": ["entity names or canonical paths"],
      "mechanism": "short causal description"
    }
  ],
  "watchpoints": [
    {
      "signal": "what to monitor next",
      "whyItMatters": "why this confirms or disproves the scenario",
      "expectedWithin": "immediate|short_term|medium_term|long_term"
    }
  ],
  "assumptions": ["explicit assumption or model limit"],
  "recommendations": ["Action 1", "Action 2", "Action 3"],
  "text": "Short decision memo in plain language.",
  "confidence": 0.0-1.0,
  "evidence": [...],
  "impactBreakdown": [...]
}

%s
- Start with the business consequence, not with the graph mechanics.
- Use firstOrderEffects for direct hits near the shock and secondOrderEffects for farther downstream effects.
- Prefer non-obvious remote endpoints when the graph supports them.
- impactBreakdown: derive from STRUCTURED IMPACT INPUT — entity, axis, amplitude, hops.
- QUANTIFICATION: When QUANTIFICATION HINT is present, translate amplitudes into estimated %% impacts on cost/time.
- Recommendations: give operational or portfolio actions, not generic advice.
- If evidence is weak, say what would change your view in watchpoints/assumptions.
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

	sb.WriteString("-- IMPACT MAP / PROJECTED NODE IMPACTS (Simulation) --\n")
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

func (b *AnswerContextBuilder) buildScenarioWindowsSection(simResult *horizonmodels.SimulationResult) string {
	var sb strings.Builder
	sb.WriteString("-- TIME WINDOWS / HOW THE EFFECT UNFOLDS --\n")
	if simResult == nil || len(simResult.NodeImpacts) == 0 {
		sb.WriteString("(no projected time-window effects)\n\n")
		return sb.String()
	}
	firstOrder := make([]string, 0, 3)
	secondOrder := make([]string, 0, 3)
	for _, ni := range simResult.NodeImpacts {
		label := strings.TrimSpace(ni.Name)
		if label == "" {
			label = strings.TrimSpace(ni.CanonicalPath)
		}
		if label == "" {
			continue
		}
		topAxis, topAmp := strongestAxis(ni.AxisAmplitudes)
		line := fmt.Sprintf("%s — %s %s (%.2f)", horizonLabelForHops(ni.Hops), label, topAxis, topAmp)
		if ni.Hops <= 1 {
			if len(firstOrder) < 3 {
				firstOrder = append(firstOrder, line)
			}
			continue
		}
		if len(secondOrder) < 3 {
			secondOrder = append(secondOrder, line)
		}
	}
	if len(firstOrder) == 0 && len(secondOrder) == 0 {
		sb.WriteString("(no projected time-window effects)\n\n")
		return sb.String()
	}
	if len(firstOrder) > 0 {
		sb.WriteString("First-order effects:\n")
		for _, item := range firstOrder {
			sb.WriteString("- " + item + "\n")
		}
	}
	if len(secondOrder) > 0 {
		sb.WriteString("Downstream / later effects:\n")
		for _, item := range secondOrder {
			sb.WriteString("- " + item + "\n")
		}
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

	sb.WriteString("-- PROPAGATION CHAINS / PROJECTED PROPAGATION PATHS --\n")
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
	} else if len(simResult.TracePaths) == 0 {
		sb.WriteString("(no propagation paths available)\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

func (b *AnswerContextBuilder) describeScenarioType(plan *horizonmodels.QuestionPlan) string {
	if plan == nil {
		return "current-state assessment"
	}
	if len(plan.HypotheticalSignals) > 0 {
		return "what-if scenario"
	}
	switch strings.TrimSpace(plan.AnswerMode) {
	case "risk_forecast":
		return "forward-looking risk assessment"
	case "alternative_sourcing":
		return "alternative-path assessment"
	case "dependency_audit":
		return "dependency review"
	case "blocker_analysis":
		return "blocker diagnosis"
	default:
		return "current-state assessment"
	}
}

func (b *AnswerContextBuilder) describeHypotheticalSignals(plan *horizonmodels.QuestionPlan) string {
	if plan == nil || len(plan.HypotheticalSignals) == 0 {
		return "No hypothetical shock injected; answer should focus on current exposure and existing risk."
	}
	parts := make([]string, 0, len(plan.HypotheticalSignals))
	for i, sig := range plan.HypotheticalSignals {
		if i >= 3 {
			break
		}
		mech := strings.TrimSpace(sig.Mechanism)
		if mech == "" {
			mech = strings.TrimSpace(sig.Axis)
		}
		target := strings.Join(sig.SeedHints, ", ")
		if target == "" {
			target = "lens scope"
		}
		parts = append(parts, fmt.Sprintf("%s on %s (%s, amplitude %.2f)", mech, target, sig.TimeHorizon, sig.Amplitude))
	}
	if len(parts) == 0 {
		return "No hypothetical shock injected."
	}
	return strings.Join(parts, "; ")
}

func (b *AnswerContextBuilder) describeTargetFilter(plan *horizonmodels.QuestionPlan) string {
	if plan == nil {
		return "no explicit target filter"
	}
	parts := []string{}
	if len(plan.TargetFilter.ObservePathPatterns) > 0 {
		parts = append(parts, "paths="+strings.Join(plan.TargetFilter.ObservePathPatterns[:minInt(3, len(plan.TargetFilter.ObservePathPatterns))], ", "))
	}
	if len(plan.TargetFilter.ObserveAxes) > 0 {
		parts = append(parts, "axes="+strings.Join(plan.TargetFilter.ObserveAxes[:minInt(4, len(plan.TargetFilter.ObserveAxes))], ", "))
	}
	if len(parts) == 0 {
		return "broad lens scope"
	}
	return strings.Join(parts, " | ")
}

func (b *AnswerContextBuilder) describeDecisionFocus(plan *horizonmodels.QuestionPlan) string {
	if plan == nil {
		return "Explain the likely business effect, the transmission path, and what to monitor."
	}
	switch strings.TrimSpace(plan.AnswerMode) {
	case "impact_assessment":
		return "Explain what changes first, what spreads later, and where business pain concentrates."
	case "risk_forecast":
		return "Focus on forward-looking risk, timing, and the earliest reliable warning signs."
	case "alternative_sourcing":
		return "Focus on fallback paths, substitutes, and the trade-offs of switching."
	case "blocker_analysis":
		return "Focus on the single biggest blocker, why it matters, and what removes it."
	case "dependency_audit":
		return "Focus on concentrated dependencies, weak links, and remote downstream exposure."
	default:
		return "Explain the likely business effect, the transmission path, and what to monitor."
	}
}

func strongestAxis(axisAmplitudes map[string]float64) (string, float64) {
	var (
		bestAxis string
		bestAmp  float64
	)
	for axis, amp := range axisAmplitudes {
		if amp > bestAmp || bestAxis == "" {
			bestAxis = axis
			bestAmp = amp
		}
	}
	return bestAxis, bestAmp
}

func horizonLabelForHops(hops int) string {
	switch {
	case hops <= 0:
		return "immediate"
	case hops == 1:
		return "short_term"
	case hops == 2:
		return "medium_term"
	default:
		return "long_term"
	}
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

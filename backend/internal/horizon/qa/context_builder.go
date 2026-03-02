package qa

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// AnswerContextBuilder assembles the LLM prompt context for answer synthesis.
type AnswerContextBuilder struct{}

// NewAnswerContextBuilder creates a new AnswerContextBuilder.
func NewAnswerContextBuilder() *AnswerContextBuilder {
	return &AnswerContextBuilder{}
}

// BuildContext assembles the structured context for the AnswerSynthesizer LLM call.
// Hard limit: ~6000 tokens input. Aggressive pruning.
// When nodeInterpretations is non-nil, uses human-readable interpretations (humanization at finish)
// instead of raw axis/amplitude — per design: humanization only at the ends.
// vertexAlternatives: entities from Vertex AI Search (alternative suppliers/products) — use for alternatives section.
func (b *AnswerContextBuilder) BuildContext(
	ctx context.Context,
	plan *horizonmodels.QuestionPlan,
	simResult *horizonmodels.SimulationResult,
	domainSnapshot *horizonmodels.DomainAnswerSnapshot,
	artifactSummaries []string,
	nodeInterpretations []*interpretation.RiskInterpretation,
	vertexAlternatives []*horizonmodels.HorizonFragment,
) (string, error) {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("QUESTION: %s\n", plan.NormalizedQuestion))
	sb.WriteString(fmt.Sprintf("ANSWER MODE: %s\n", plan.AnswerMode))
	sb.WriteString(fmt.Sprintf("TIME HORIZON: %s\n\n", plan.TimeHorizon))

	// IMPACT MAP — human-readable when interpretations present (humanization at finish)
	if len(nodeInterpretations) > 0 {
		sb.WriteString("=== NODE INTERPRETATIONS (human-readable, from propagation + physical signals) ===\n")
		for i, interp := range nodeInterpretations {
			if i >= 15 {
				break
			}
			sb.WriteString(fmt.Sprintf("[%d] %s (%s)\n", i+1, interp.CanonicalPath, interp.NodeID))
			sb.WriteString(fmt.Sprintf("    Summary: %s\n", strings.TrimSpace(interp.Summary)))
			if interp.Impact.Operational != "" {
				sb.WriteString(fmt.Sprintf("    Operational: %s\n", interp.Impact.Operational))
			}
			if len(interp.Mitigations) > 0 {
				sb.WriteString("    Mitigations: ")
				for j, m := range interp.Mitigations {
					if j > 0 {
						sb.WriteString("; ")
					}
					sb.WriteString(m.Action)
				}
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	} else {
		// Fallback: raw simulation output (when interpreter not wired)
		sb.WriteString("=== IMPACT MAP (simulation: nodes affected by propagated signals) ===\n")
		topK := 15
		if len(simResult.NodeImpacts) < topK {
			topK = len(simResult.NodeImpacts)
		}
		for i := 0; i < topK; i++ {
			ni := simResult.NodeImpacts[i]
			sb.WriteString(fmt.Sprintf("[%d] %s — ", i+1, ni.CanonicalPath))
			for axis, amp := range ni.AxisAmplitudes {
				sb.WriteString(fmt.Sprintf("%s: %.2f ", axis, amp))
			}
			sb.WriteString(fmt.Sprintf("(%d hops)\n", ni.Hops))
			if ni.Name != "" && ni.Name != ni.CanonicalPath {
				sb.WriteString(fmt.Sprintf("    Name: %s\n", ni.Name))
			}
		}
		if topK == 0 {
			sb.WriteString("(no propagation — answer from leaf evidence and artifacts only)\n")
		}
		sb.WriteString("\n")
	}

	// PROPAGATION CHAINS — how the signal flows (seed → node1 → node2)
	sb.WriteString("=== PROPAGATION CHAINS (how influence flows through the graph) ===\n")
	for i, tp := range simResult.TracePaths {
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
				ampStr = fmt.Sprintf(" [%.2f]", tp.Amplitudes[j])
			}
			pathStr += p + ampStr
		}
		if pathStr != "" {
			sb.WriteString(fmt.Sprintf("Chain %d (axis=%s): %s\n", i+1, tp.Axis, pathStr))
		}
	}
	if len(simResult.TracePaths) == 0 && (len(nodeInterpretations) > 0 || len(simResult.NodeImpacts) > 0) {
		sb.WriteString("(paths available in IMPACT MAP above)\n")
	}
	sb.WriteString("\n")

	// SUPPLY CHAIN PATHS (from artifact summaries)
	sb.WriteString("=== SUPPLY CHAIN PATHS (from MaterializedArtifacts) ===\n")
	for _, s := range artifactSummaries {
		sb.WriteString(fmt.Sprintf("- %s\n", s))
	}
	sb.WriteString("\n")

	// LEAF EVIDENCE — real-world signals at endpoints (AIS, satellite, news)
	sb.WriteString("=== LEAF EVIDENCE (physical signals at endpoints — interpret these) ===\n")
	for i, le := range simResult.LeafEvidence {
		sb.WriteString(fmt.Sprintf("[L%d] %s: %s = %.2f — %s [%s, %s]\n",
			i+1, le.CanonicalPath, le.Axis, le.Amplitude, le.Summary, le.Modality, le.ObservedAt.Format("2006-01-02 15:04")))
	}
	if len(simResult.LeafEvidence) == 0 {
		sb.WriteString("(no leaf signals — answer from simulation only)\n")
	}
	sb.WriteString("\n")

	// PRE-COMPUTED IMPACT BREAKDOWN (from simulation — use this, do not invent)
	sb.WriteString("=== IMPACT BREAKDOWN (from simulation — use for impactBreakdown in JSON) ===\n")
	for i, ni := range simResult.NodeImpacts {
		if i >= 15 {
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

	// QUANTITATIVE HINT — for cost/tariff questions, help synthesizer produce numeric ranges
	if b.shouldAddQuantitativeHint(plan, simResult) {
		sb.WriteString("=== QUANTITATIVE HINT (use for cost/price impact — provide numeric ranges) ===\n")
		sb.WriteString("- Pass-through heuristic: amplitude 0.6+ → 60-80% pass-through; 0.3-0.6 → 30-50%; <0.3 → 10-30%\n")
		sb.WriteString("- For VAT/tariff: input % × pass-through × amplitude ≈ cost delta at that stage. Example: 5% VAT, amplitude 0.72 at nickel ore → ~3-4% cost increase there; amplitude 0.12 at battery cell → ~0.5-1.5% at final product.\n")
		sb.WriteString("- ALWAYS provide numeric ranges when the question asks about cost/price. Avoid only \"likely\" or \"potentially\" without numbers.\n")
		sb.WriteString("\n")
	}

	// EXISTING ANALYSIS (DomainAnswerSnapshot)
	if domainSnapshot != nil {
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
		if len(domainSnapshot.Alternatives) > 0 {
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

	// VERTEX SEARCH ALTERNATIVES (entities from semantic search — use for alternatives in answer)
	if len(vertexAlternatives) > 0 {
		sb.WriteString("=== ALTERNATIVE ENTITIES FROM SEARCH (use for alternatives — real data from index) ===\n")
		for i, f := range vertexAlternatives {
			if i >= 10 {
				break
			}
			name := f.CanonicalPath
			if f.Labels != nil {
				if en, ok := f.Labels["en"]; ok && en != "" {
					name = en
				}
			}
			sb.WriteString(fmt.Sprintf("[V%d] %s (%s) — %s\n", i+1, name, f.CanonicalPath, string(f.Type)))
		}
		sb.WriteString("\n")
	}

	// DATA GAPS
	sb.WriteString("=== DATA GAPS ===\n")
	if domainSnapshot != nil && len(domainSnapshot.DataGaps) > 0 {
		for _, g := range domainSnapshot.DataGaps {
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

	if len(nodeInterpretations) > 0 {
		sb.WriteString(`INSTRUCTIONS:
- GROUND EVERY CLAIM in the data above. Do NOT invent evidence.
- NODE INTERPRETATIONS [1]-[N]: human-readable summaries from propagation + physical signals. Use these directly.
- PROPAGATION CHAINS: show how influence flows (seed → node1 → node2). Use this to explain "how does it propagate?"
- LEAF EVIDENCE [L1]-[LN]: real-world signals. Cite these when available. If none, say "based on simulation only."
- Cite as [1], [2], [L1] etc. matching the numbered items above.
- impactBreakdown: derive from NODE INTERPRETATIONS or IMPACT BREAKDOWN — entity, axis, amplitude, hops. Do NOT invent.
- QUANTIFICATION: When QUANTITATIVE HINT is present, include specific numeric ranges (e.g. "2-4% cost increase"). Avoid only "likely" or "potentially" without numbers.
- ALTERNATIVES: For impact_assessment or alternative_sourcing, suggest 1-3 actionable alternatives. Prefer ALTERNATIVE ENTITIES FROM SEARCH [V1]-[VN] when present (real indexed data); else use ALTERNATIVES from EXISTING ANALYSIS; else infer from graph.
- Return JSON: { "text": "...", "confidence": 0.0-1.0, "evidence": [{type, canonicalPath, summary, sourceRef}], "impactBreakdown": [{entity, axis, amplitude, hops}], "alternatives": [...], "dataGaps": [...] }
`)
	} else {
		sb.WriteString(`INSTRUCTIONS:
- GROUND EVERY CLAIM in the data above. Do NOT invent evidence.
- IMPACT MAP [1]-[N]: simulation results (which nodes got which signal amplitudes). Interpret: what does amplitude 0.7 on legal/access mean for that entity?
- PROPAGATION CHAINS: show how influence flows (seed → node1 → node2). Use this to explain "how does it propagate?"
- LEAF EVIDENCE [L1]-[LN]: real-world signals. Cite these when available. If none, say "based on simulation only."
- Cite as [1], [2], [L1] etc. matching the numbered items above.
- impactBreakdown: derive from IMPACT MAP — entity, axis, amplitude, hops. Do NOT invent.
- QUANTIFICATION: When QUANTITATIVE HINT is present, include specific numeric ranges (e.g. "2-4% cost increase"). Avoid only "likely" or "potentially" without numbers.
- ALTERNATIVES: For impact_assessment or alternative_sourcing, suggest 1-3 actionable alternatives. Prefer ALTERNATIVE ENTITIES FROM SEARCH [V1]-[VN] when present (real indexed data); else use ALTERNATIVES from EXISTING ANALYSIS; else infer from graph.
- Return JSON: { "text": "...", "confidence": 0.0-1.0, "evidence": [{type, canonicalPath, summary, sourceRef}], "impactBreakdown": [{entity, axis, amplitude, hops}], "alternatives": [...], "dataGaps": [...] }
`)
	}

	return sb.String(), nil
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

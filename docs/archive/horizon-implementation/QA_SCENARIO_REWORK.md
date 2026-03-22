# QA Scenario Rework

> Scope: this document describes the QA changes that were actually implemented to move Lens Q&A from a graph-technical answerer to a scenario-first "what happens if" module.

## Why This Rework Was Needed

The previous QA flow was too technical in two ways:
- it surfaced graph mechanics instead of projected consequences;
- it answered like a simulation trace, not like a decision brief.

That was acceptable for internal verification, but weak for the real use case:
- ask a hypothetical question;
- understand what changes first;
- understand what spreads later;
- understand what to watch next;
- understand what to do now.

In other words, QA had to move from `graph report` to `scenario answer`.

## Design Goal

The new QA module should answer questions in the form:
- what happens if X occurs;
- who gets hit first;
- what spreads downstream;
- what signals would confirm or invalidate the scenario;
- what action is justified under current uncertainty.

The deterministic graph simulation remains the substrate. The presentation and answer contract are now scenario-first.

## Files Changed

Backend:
- `backend/internal/horizon/models/qa.go`
- `backend/internal/horizon/qa/context_builder.go`
- `backend/internal/horizon/qa/decomposer.go`
- `backend/internal/horizon/qa/synthesizer.go`

Frontend:
- `web/src/components/QAPanel.tsx`

## 1. Answer Contract Reworked

The QA answer model was expanded so the system can return structured scenario output instead of only a freeform summary.

### New fields in `QAAnswer`

- `ExecutiveSummary`
- `RiskScenario`
- `FirstOrderEffects`
- `SecondOrderEffects`
- `Watchpoints`
- `Assumptions`
- `Recommendations`

### New supporting types

#### `QAEffect`
Represents a projected effect with:
- summary;
- horizon;
- severity;
- affected entities;
- mechanism.

#### `QAWatchpoint`
Represents what to monitor next:
- signal;
- why it matters;
- expected timing.

## 2. Context Builder Reframed Around Scenario Logic

The QA context builder was rewritten so the model is no longer primed to produce a technical propagation explanation first.

### New framing

The prompt now builds a decision-oriented scenario context with sections such as:
- `DECISION BRIEF`
- `GROUND TRUTH AND KNOWN STRUCTURE`
- `QUANTIFICATION HINT`
- `EXISTING ANALYSIS / BASELINE`
- `UNCERTAINTY / DATA GAPS`
- `RECENTLY ENRICHED SIGNALS`

### What changed conceptually

Instead of asking the model to summarize simulation output, the prompt now asks it to:
- identify the assumed shock;
- separate first-order from second-order effects;
- prefer remote business effects over obvious nearby restatements;
- explicitly state assumptions when evidence is incomplete;
- produce monitorable watchpoints, not only conclusions.

### New structured output schema

The synthesizer prompt now asks for JSON with fields such as:
- `executiveSummary`
- `riskLevel`
- `riskScenario`
- `firstOrderEffects`
- `secondOrderEffects`
- `watchpoints`
- `assumptions`
- `recommendations`
- `text`
- `confidence`
- `evidence`
- `impactBreakdown`

## 3. Question Decomposition Tightened for What-If Analysis

The decomposer prompt was updated so the system treats a question as:
- an executive what-if request;
- a graph simulation plan;
- a target selection problem;
- a timing problem.

### New decomposition emphasis

The decomposer now explicitly prefers:
- downstream business consequences over obvious local echoes;
- realistic time horizons;
- farther endpoints when they are better aligned with the question;
- scenario framing over raw graph traversal language.

This matters because the rest of the QA pipeline depends heavily on how the question is normalized before simulation starts.

## 4. Synthesizer Can Recover Scenario Structure Even If The Model Does Not

One weakness in the earlier QA flow was that if the LLM failed to return a strong structured answer, the result degraded too much.

That was corrected.

### New fallback behavior

The synthesizer now derives scenario structure from simulation output when necessary:
- first-order effects from low-hop impacts;
- second-order effects from higher-hop propagation;
- watchpoints from leaf evidence and weaker tails;
- fallback risk scenario text from strongest derived effects.

This is important because it keeps QA scenario-shaped even under imperfect model output.

### Practical effect

The answer no longer collapses back to a graph-tech response just because the model omitted some structured fields.

## 5. Frontend Reworked From Technical Panel To Scenario Brief

`QAPanel.tsx` was reorganized so the default reading experience is business-facing.

### What changed in the UI

The panel now leads with:
- risk badge;
- time horizon;
- confidence;
- executive summary;
- compact risk scenario narrative.

Below that, the answer is organized into sections such as:
- `What changes first`
- `What spreads later`
- `What to monitor next`
- `What to do now`
- `Fallbacks and alternatives`
- `What could change this view`

### What moved out of the way

Technical diagnostics are still available, but no longer dominate the interface. They are pushed into a lower-priority expandable diagnostics section.

That section still contains:
- propagation detail;
- seed failures;
- evidence list;
- impact breakdown.

The result is that QA remains debuggable for us without forcing the user to read it as an engineering console.

## 6. Language and Interaction Changes

Several interaction details were changed to reinforce the scenario mode.

### Prompting and labels

- input placeholder now pushes the user toward `Ask what happens if...`
- auto-scout language was softened to mean evidence reinforcement, not a technical fallback
- status labels were rewritten from pipeline jargon into scenario-language, for example:
  - mapping the shock and exposed pathways;
  - estimating who gets hit first and later;
  - writing the decision brief.

### Why this matters

These are not cosmetic-only changes. They change how the module is perceived and therefore how it is used.

If the panel sounds like a graph debugger, users ask graph-debugger questions. If it sounds like a scenario system, users ask scenario questions.

## 7. What The Rework Does Not Change

This rework does not replace the deterministic simulation substrate.

The intended architecture remains:
1. decompose the question;
2. simulate on graph/path substrate;
3. synthesize a grounded answer.

The graph, signals, propagation paths, and evidence remain the basis of truth. The rework changes:
- answer contract;
- prompt framing;
- fallback logic;
- user presentation.

That is the correct boundary.

## 8. Current Residual Gaps

The QA module is now structurally better, but it is not finished.

### Remaining gaps

1. `path stress` is not yet expressed strongly enough inside QA answers.
2. `bottleneck leg` and `dominant factors` should become first-class in the answer, not just in path metadata.
3. supply shock and policy/regulatory shock still need stronger parity in answer quality.
4. the panel still does not drive map/path visualization tightly enough from QA output.

## 9. Why This Rework Matters For The Broader System

This QA change is not isolated UI work.

It is necessary for the larger goal of the system:
- detect shocks from RSS;
- propagate them through graph and paths;
- surface non-obvious downstream consequences early;
- answer in a form a decision-maker can actually use.

Without this QA rework, even a strong graph and path engine would still look weaker than it is, because the last mile would remain too technical.

## 10. Verification

The implementation was validated with:
- `go test ./backend/internal/horizon/qa ./backend/internal/horizon/models ./backend/internal/http/horizon -count=1`
- `npm run build:ci`

Both passed.

## 11. Next Logical Step

After this QA rework, the correct next step is to tighten the bridge between QA and the new path-centric substrate.

Specifically:
- bring `path stress` into answer generation explicitly;
- expose bottleneck legs and zone pressure in scenario output;
- let QA drive graph/map overlays using path-aware consequences instead of only node summaries.

That is the path from `better QA wording` to `real early warning reasoning`.

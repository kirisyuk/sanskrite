# Agentic Scouting Environment — Target Architecture

## Premise

The current ScoutEngine (~8000 LOC `engine.go` + ~60 satellite files) is a hand-coded
orchestration layer that algorithmically routes LLM calls through domain contracts,
research strategies, critic validators, and pipeline stages. This is the exact job
a reasoning orchestrator does natively and adaptively.

The canonical control plane (product identity, transforms, synonyms, path validation,
schema enforcement) is **not** part of this replacement — it stays as **tools** that
agents call.

## Core Objectives

1. **Improve graph quality**
   The reasoning environment must produce a more intelligent, context-aware,
   evidence-sensitive graph construction process than the current algorithmic
   pipeline. The expected result is a higher-quality graph: better entities,
   better edges, better corrections, fewer brittle inferences, and more faithful
   materialization of real-world structure.

2. **Replace rigid algorithmic expansion with selective reasoning**
   The new environment should prefer adaptive branch choice, explicit gap
   detection, query reframing, and bounded iteration over fixed-stage expansion.

3. **Target lower total investigation cost, while treating cost reduction as an evaluated outcome**
   The design goal is to reduce total investigation cost by avoiding unnecessary
   stages, weak branches, and low-value retries. This is a target and should be
   measured during shadow comparison; it is not assumed as an automatic property
   of agentic execution.

4. **Support target-specific path and logistics materialization when the investigation requires it**
   In supply-chain investigations, success is often not just a better entity-edge
   graph. The environment must be able to reason its way toward target-scoped
   materialized paths, supply links, and concrete legs when those are required by
   the investigative goal.

## Design Principles

1. **Domain is context, not agent type.** There is no "supply chain agent" or
   "governance agent". The orchestrator understands — from the query, from existing
   graph context, from its own reasoning — that it needs to investigate a supply chain,
   or ownership structure, or regulatory impact. It dispatches universal researchers
   with the right framing.

2. **Agents are roles, not domains.** The environment has: thinker (orchestrator),
   searchers (researchers), checker (critic), writer (materializer). Each is
   domain-agnostic. Domain knowledge lives in the orchestrator's instruction and
   in the tools it has access to.

3. **Tools encode invariants.** Canonical path resolution, schema validation,
   product transforms, edge type constraints — these are deterministic tools.
   Agents call them; agents don't re-derive them.

4. **The orchestrator plans, re-plans, and terminates.** No hardcoded stage graph.
   The orchestrator decides what to do next based on what it already knows, what's
   missing, and what the critic says. It can do 2 research rounds or 12. It can
   investigate ownership first or supply chain first. It decides.

5. **State is shared, not passed.** ADK session state is the single shared memory.
   Researchers write findings there; the critic reads them; the orchestrator reads
   everything. No brittle inter-agent message contracts.

6. **The reasoning core knows enforcement exists, but does not simulate it.**
   The orchestrator must know that canonicalization, policy checks, geo coordinate
   mapping, and write validation exist and can reject underspecified claims.
   But those rules remain deterministic and authoritative tools, not prompt-level
   logic reimplemented inside the orchestrator.

7. **Existing graph state is prior knowledge, not unquestioned truth.**
   The agentic environment starts from what the system already knows, but it must
   be able to confirm, strengthen, specialize, contradict, deprecate, or replace
   existing graph facts with explicit correction semantics and traceability.

8. **Roll out in parallel, not by mutating the current Scout pipeline.**
   The agentic environment should be a separate runtime path with its own sessions,
   traces, tool layer, and evaluation. Routing between classic Scout and agentic
   Scout happens at the boundary, with shadow mode and switchback capability.

9. **Path construction remains inside the same reasoning loop.**
   Materialized supply paths, links, and legs must not be implemented as a
   hardcoded side-pipeline outside the agentic environment. They should emerge
   from the same cycle of planning, investigation, critique, replanning,
   execution, and landing that governs the rest of the environment.

---

## Placement in Vertex + AgentMesh

This environment is not a standalone sidecar and not a rewrite of `ScoutEngine`.
It is a new mesh capability family inside the existing `AgentMesh`, with Vertex AI
remaining the managed execution substrate.

### Target Substrate

- **Vertex AI Studio / Agent Engine / Reasoning Engine** remain the managed runtime
  for model execution, sessions, memory, evaluation, and code execution where used.
- **AgentMesh** remains the internal control plane for routing, task lifecycle,
  policy, lineage, approval, and artifact persistence.
- **Horizon graph + canonical control plane** remain the system of record for
  world state, graph lookups, canonicalization, and materialized writes.

### Why this belongs in AgentMesh

The simulation runtime already lives inside `AgentMesh` as a disciplined
multi-agent environment. The agentic scouting environment should follow the same
pattern:

- simulation remains one mesh runtime family
- scouting becomes a second mesh runtime family
- both share common control-plane mechanics
- both execute on Vertex through `AgentMesh`

This means we are not "adding agentic Scout to ScoutEngine". We are adding a new
AgentMesh mesh/runtime on top of Vertex, next to the existing simulation mesh.

### Proposed Mesh Capability Families

At the AgentMesh level, the new environment should be exposed as a small set of
capability families rather than one opaque monolith:

- `scout.plan` — initial orchestration and branch formation
- `scout.research` — focused evidence collection for one question or branch
- `scout.critic` — branch or whole-investigation review
- `scout.materialize` — validated graph writes and corrections
- `scout.path` — path synthesis and landing when the orchestrator determines that
  the target requires target-scoped path construction
- `scout.review` — final mesh-level verification before task success
- `scout.aggregate` — optional final synthesis artifact for UI / lens runtime

The orchestrator uses these capabilities internally, but AgentMesh also sees them
as routable units for tracing, policy, retries, and cost attribution.

---

## Vertex-Native Execution Requirements

This environment is intentionally designed to be Vertex-native, both for runtime
discipline and to ensure that the team's available Vertex credits are spent on
the actual reasoning system rather than bypassed by an external runtime.

### Primary Rule

The central reasoning runtime must execute on Vertex.

That includes:

- orchestrator execution
- research / critic / materializer subagent execution
- session lifecycle
- long-lived memory where used
- evaluation and trace-visible agent runs
- code execution where used for programmatic tool orchestration

In other words, the "Claude Code-like" central harness belongs inside the Vertex
runtime layer, not outside it.

### What may remain outside Vertex

Deterministic Horizon services may remain outside the Vertex reasoning runtime
and be exposed as governed tools. Examples:

- canonical path resolution backends
- graph repositories and Firestore access services
- policy enforcement services
- geo coordinate mapping / revalidation services
- product synonym / transform dictionary services
- Vertex Search access wrappers

These are part of the data plane / validation plane, not replacements for the
reasoning runtime.

### AgentMesh Boundary

`AgentMesh` is the control plane, not the model execution plane.

It should own:

- capability routing
- task lifecycle
- policy and approvals
- retries and leases
- review flow
- artifact persistence
- lineage and cost attribution

It should not replace the central reasoning runtime hosted on Vertex.

### Vertex Credit Utilization Goal

If the goal includes utilizing available Vertex credits, the architecture should
prefer Vertex-native services whenever they are a good fit for the reasoning path:

- Vertex Agent Engine / Reasoning Engine for agent execution
- Vertex Sessions / Memory Bank for session state and long-lived memory
- Vertex evaluation for quality measurement
- Vertex code execution for tool-heavy orchestration where appropriate
- Vertex grounding with Google Search or your search API for research branches

### Non-Goal

It is explicitly not a goal to build an architecture where Vertex is used only
for superficial model calls while the real agent harness, memory, or orchestration
runtime lives elsewhere.

The intended design is:

- **Vertex hosts the reasoning runtime**
- **AgentMesh governs and routes it**
- **Horizon services ground, validate, and persist its outputs**

---

## Reasoning Harness

The core of the environment is not just the orchestrator prompt. It is a
reasoning harness: a query engine + state model + delegation model + bounded
control flow.

### Harness Responsibilities

The harness is responsible for:

- assembling stable and dynamic prompt layers
- maintaining the session epistemic state
- choosing between direct tool use, code-based orchestration, and subagent delegation
- interpreting deterministic tool failures as research signals
- enforcing bounded retries and stop conditions
- writing structured artifacts for downstream review, UI, and comparison

### Practical Main Loop

The orchestrator loop should be implemented conceptually as:

1. **Ingest**
   Read target, lens context, tenant context, and existing graph snapshot.

2. **Anchor**
   Check what is already known via `search_knowledge_base`,
   `lookup_fragment`, and `get_graph_neighborhood`.

3. **Frame**
   Build or update the investigation plan: active hypotheses, open questions,
   investigation branches, and their dependency structure. This is the most
   important reasoning step and is described in detail below
   (see "Framing: How the Orchestrator Builds an Investigation Plan").

4. **Choose next action**
   Select the highest-value next step using the decision framework described
   below (see "Action Selection: How the Orchestrator Chooses What to Do Next").

5. **Execute**
   Run the selected step — tool call, subagent dispatch, or programmatic
   orchestration batch.

6. **Interpret**
   Convert results into structured epistemic updates. This is not a passive
   read — the orchestrator must explicitly update session state:
   - promote or demote hypotheses based on new evidence
   - mark branches as strengthened, weakened, or blocked
   - register new contradictions
   - update materialization readiness scores
   - record new open questions surfaced by the evidence
   - update the plan if new branches are needed or existing ones are obsolete

7. **Bound**
   Apply retry counters, budget limits, repeated-gap detection, and denial policy.

8. **Loop or stop**
   Continue until a valid end state is reached (see "Convergence and
   Termination Mechanics").

### Framing: How the Orchestrator Builds an Investigation Plan

The Frame step is where the orchestrator converts raw context into a structured
reasoning plan. This is the highest-leverage reasoning moment in the entire loop
and must not be left to unstructured intuition.

#### Inputs to framing

The orchestrator frames from four sources:

1. **Target specification** — what the user or system asked to investigate.
2. **Anchor results** — what the graph already knows (entities, edges,
   neighborhoods, existing paths, existing coverage gaps).
3. **Structural reasoning categories** — the orchestrator checks whether the
   target naturally involves ownership/control, policy impact, supply-chain
   pathing, market structure, or risk. It does this by recognizing structural
   cues in the target and anchor data, not by running through a checklist.
4. **Prior state** — if this is a re-frame after research rounds, the
   orchestrator also has: accumulated evidence, critic feedback, deterministic
   tool failures, and the current plan's branch statuses.

#### Framing output: the Investigation Plan

The Frame step produces or updates an `InvestigationPlan` — a first-class
artifact in session state:

```python
class InvestigationPlan(BaseModel):
    plan_version: int
    target_summary: str
    structural_categories_detected: list[str]
    branches: list[InvestigationBranch]
    branch_dependencies: list[BranchDependency]
    open_questions: list[OpenQuestion]
    materialization_strategy: Literal[
        "batch_at_end", "progressive", "progressive_with_rollback"
    ]
    re_plan_trigger: str | None = None

class BranchDependency(BaseModel):
    upstream_branch_id: str
    downstream_branch_id: str
    dependency_kind: Literal[
        "blocks",           # downstream cannot start until upstream resolves
        "informs",          # downstream benefits from upstream but can start
        "contradicts",      # both investigate competing explanations
    ]

class OpenQuestion(BaseModel):
    question_id: str
    text: str
    linked_branch_ids: list[str]
    priority: float
    source: Literal[
        "initial_framing", "anchor_gap", "research_result",
        "critic_feedback", "tool_failure", "re_plan"
    ]
```

#### Framing reasoning protocol

When framing, the orchestrator should explicitly reason through:

1. **What is the target really asking for?**
   Not just the surface query, but the structural deliverable: a graph? a
   target-scoped path? an ownership tree? a regulatory impact map?

2. **What structural forms are relevant?**
   Does this target involve ownership chains, policy impact layers, supply-chain
   paths, market positioning, or combinations? The orchestrator recognizes these
   from the Structural Reasoning Categories, not from a domain label on the input.

3. **What do we already know, and where are the gaps?**
   The anchor step provides existing graph state. The orchestrator must classify
   each relevant area as: well-covered, partially covered, or absent. Gaps become
   branches or open questions.

4. **What hypotheses are worth testing?**
   For each structural gap, the orchestrator proposes at least one hypothesis
   (see "Hypothesis Lifecycle" below). Multiple competing hypotheses for the same
   gap are encouraged — they become parallel or contradicting branches.

5. **What are the dependencies?**
   Some branches are meaningless without others. For example, a supply-chain
   path cannot be assembled until the upstream entity-edge substrate exists. The
   orchestrator makes these dependencies explicit in `branch_dependencies`.

6. **What materialization strategy fits?**
   For small investigations, batch-at-end is fine. For large multi-entity
   investigations, progressive materialization with rollback is needed (see
   "Progressive Materialization and Backtracking" below).

#### Re-planning

Re-planning is not "start over." It is a targeted mutation of the existing plan:

- **Add branches** when new evidence reveals investigation areas that were not
  visible at initial framing.
- **Kill branches** when evidence makes them irrelevant or when the critic
  confirms they add no value.
- **Re-prioritize** when a branch that was secondary becomes blocking for the
  overall deliverable.
- **Split branches** when a branch turns out to be too broad to investigate or
  critique as a unit.
- **Change materialization strategy** when the investigation grows larger or
  more uncertain than initially expected.

Every re-plan increments `plan_version` and records `re_plan_trigger` (what
caused the re-plan: critic feedback, tool failure, new evidence, budget
pressure, etc.).

---

### Hypothesis Lifecycle

A hypothesis is a testable claim that the orchestrator is investigating. It is
not just a string — it is a structured reasoning object.

```python
class Hypothesis(BaseModel):
    hypothesis_id: str
    branch_id: str
    statement: str
    status: Literal[
        "proposed",
        "under_investigation",
        "supported",
        "weakly_supported",
        "contradicted",
        "confirmed_materializable",
        "rejected",
    ]
    supporting_evidence: list[str]
    contradicting_evidence: list[str]
    confidence: float  # 0.0-1.0
    test_criteria: str
    last_updated_by: Literal[
        "orchestrator_framing", "research_result", "critic_review",
        "tool_output", "re_plan"
    ]
```

#### Lifecycle stages

1. **Proposed** — the orchestrator creates the hypothesis during framing. It
   must include `test_criteria`: what evidence would confirm or refute it.

2. **Under investigation** — a researcher has been dispatched to gather evidence
   relevant to this hypothesis. Multiple hypotheses can be under investigation
   simultaneously.

3. **Supported / Weakly supported** — evidence has been gathered. "Supported"
   means multiple credible sources converge. "Weakly supported" means some
   evidence exists but it is thin, single-source, or indirect.

4. **Contradicted** — evidence directly conflicts with the hypothesis. The
   orchestrator must decide: re-investigate with a reframed query, or reject.

5. **Confirmed materializable** — the hypothesis has passed critic review, the
   evidence is strong, and the corresponding facts are ready for materialization.

6. **Rejected** — the hypothesis is false or untestable. The orchestrator records
   why, so that future re-planning does not re-open the same dead end.

#### Connection to branches

Each `InvestigationBranch` tracks one or more hypotheses. A branch's status is
derived from its hypotheses:

- All hypotheses confirmed → branch is `materializable`
- Some confirmed, some under investigation → branch is `researching`
- Any hypothesis contradicted without resolution → branch is `needs_review`
- All hypotheses rejected → branch is `closed`

This means branches are not opaque work items. Their progress is traceable
through hypothesis status.

---

### Action Selection: How the Orchestrator Chooses What to Do Next

The orchestrator must not choose the next action by intuition alone. It should
follow a decision framework that considers the current plan state, epistemic
state, and budget.

#### Decision inputs

1. **Plan state** — which branches are open, which are blocked on dependencies,
   which hypotheses are under investigation.
2. **Epistemic state** — what is known, what is contradicted, what is missing.
3. **Critic feedback** — if the critic recently ran, what did it flag?
4. **Budget state** — remaining turns, token budget, cost so far.
5. **Deterministic signals** — recent tool failures, policy denials, ambiguity
   flags.

#### Decision priority hierarchy

The orchestrator should prefer actions roughly in this order, but with judgment:

1. **Resolve blockers first.**
   If a branch is blocked on a dependency, and the upstream branch can be
   advanced, do that. If it is blocked on a deterministic failure (ambiguous
   geo, invalid path, policy rejection), attempt a reframed query or gather
   the specific missing context.

2. **Advance the highest-information-gain branch.**
   Among non-blocked branches, prefer the one where one more research pass is
   most likely to move a hypothesis from "proposed" or "under investigation"
   to "supported" or "contradicted." Estimate this from:
   - how many open questions target this branch
   - whether the branch has already consumed multiple research rounds with
     diminishing returns
   - whether the branch unlocks downstream branches

3. **Use direct tools before subagents.**
   If the question can be answered by a single tool call (lookup, precheck,
   validation), do not spawn a researcher. Subagent dispatch has a real cost
   in tokens, latency, and context.

4. **Call the critic when the plan demands it, not continuously.**
   The critic should be invoked when:
   - a branch transitions from researching to believed-materializable
   - the orchestrator suspects contradictions between branches
   - a full re-plan is about to happen and the orchestrator wants external
     assessment of the current state
   - the investigation is about to terminate and final quality must be assessed

   The critic should NOT be invoked:
   - after every single research round
   - for trivial tool-call results
   - when the orchestrator is confident about a single-fact update

5. **Materialize when a branch is confirmed, not when everything is done.**
   If the plan uses progressive materialization, branches that reach
   `confirmed_materializable` should be materialized promptly. Waiting
   for the entire investigation to complete wastes the opportunity to
   free context and demonstrate incremental progress.

6. **Terminate rather than repeat.**
   If the same gaps persist after two re-plan cycles targeting those gaps,
   downgrade the branch to `candidate_only` or `needs_human_review` and
   move on.

#### Structured reasoning at the choice point

At each Choose step, the orchestrator should produce a brief internal reasoning
trace (this is part of its generation, visible in the session trace):

```
PLAN STATE: 4 branches, 2 researching, 1 blocked on dependency, 1 materializable
HIGHEST GAIN: branch-3 (ownership chain, 1 hypothesis under investigation,
  upstream evidence just arrived, expected to resolve with one more search)
BLOCKERS: branch-2 depends on branch-1 which is still researching
ACTION: dispatch researcher for branch-3, question: "..."
REASON: branch-3 is closest to resolution and unblocks path construction
```

This trace is not enforced by code — it is part of the orchestrator's prompt
discipline and is visible in AgentMesh session traces for debugging and
evaluation.

---

### Delegation Protocol

Delegation is not just "call a subagent." The orchestrator must send structured
context and expect structured returns.

#### Orchestrator → Researcher

**What is sent:**

```python
class ResearchDispatch(BaseModel):
    branch_id: str
    hypothesis_id: str | None
    question: str
    context_summary: str        # compact summary of what is already known
    evidence_to_corroborate: list[str] | None
    evidence_to_refute: list[str] | None
    source_preferences: list[str] | None  # e.g., "official filings", "trade press"
    max_search_rounds: int = 3
```

The researcher does NOT receive the full session state. It receives a compact
`context_summary` — enough to avoid duplicate work and frame the search, but
not the entire epistemic history. This keeps the researcher's context focused.

**What comes back:**

```python
class ResearchMemo(BaseModel):
    branch_id: str
    hypothesis_id: str | None
    findings: list[Finding]
    contradictions_found: list[str]
    confidence_assessment: Literal["high", "medium", "low"]
    suggested_follow_ups: list[str] | None
    sources_used: list[str]

class Finding(BaseModel):
    fact: str
    source: str
    source_date: str | None
    confidence: Literal["high", "medium", "low"]
    relevance_to_question: str
```

#### Orchestrator → Critic

**What is sent:**

The critic receives the full current epistemic state for the branches under
review, including all hypotheses, evidence summaries, and contradiction lists.
Unlike the researcher, the critic needs broad context to assess quality.

**What comes back:**

`CriticFeedback` (already defined in the document) — grade, confidence,
strengths, gaps, contradictions, follow-up questions.

#### Orchestrator → Materializer

**What is sent:**

```python
class MaterializationDispatch(BaseModel):
    branch_ids: list[str]
    confirmed_hypotheses: list[str]
    entities_to_create_or_update: list[dict]
    edges_to_create_or_update: list[dict]
    paths_to_materialize: list[dict] | None
    links_to_materialize: list[dict] | None
    legs_to_materialize: list[dict] | None
    correction_proposals: list[dict] | None
```

**What comes back:**

```python
class MaterializationReport(BaseModel):
    created: list[str]
    updated: list[str]
    skipped: list[str]
    failed: list[MaterializationFailure]
    corrections_applied: list[str]

class MaterializationFailure(BaseModel):
    item: str
    reason: str
    status: Literal[
        "invalid_path", "duplicate", "edge_forbidden",
        "policy_rejected", "insufficient_geo", "candidate_only"
    ]
```

#### Integration back into session state

After any subagent returns, the orchestrator's Interpret step must:

1. Update the relevant hypotheses with new evidence or contradictions.
2. Update branch status if the return changes it.
3. Register new open questions from `suggested_follow_ups` or critic
   `follow_up_questions`.
4. Record materialization failures as deterministic signals for re-planning.
5. Update `epistemic_state` fields (known_facts, contradictions, etc.).

This is explicit structured work, not just "read the result and think about it."

---

### Turn Structure and Context Management

In production, an ADK agent on Vertex operates in turns. Each turn the model
sees its context, generates a response (possibly with tool calls or subagent
dispatches), and receives results. Context management is the primary practical
challenge of agentic systems.

#### What happens in one turn

A single orchestrator turn should correspond to one cycle of:
**Frame (or update) → Choose → Execute → Interpret**

The Ingest and Anchor steps happen only on the first turn. Subsequent turns
start from the current plan state and proceed to the next action.

A single turn may include:
- one or more direct tool calls (e.g., lookup + precheck)
- one subagent dispatch (researcher, critic, or materializer)
- one plan update (re-prioritization, branch creation, hypothesis update)

A single turn should NOT try to:
- dispatch multiple subagents sequentially within one generation
- run the entire investigation end-to-end
- produce the final materialization and the initial research in the same turn

#### Context window discipline

The orchestrator's active context at any turn consists of:

1. **Stable layers** (cached, rarely changing) — role, epistemic rules,
   tool contracts, canonical rules, safety rules.
2. **Plan summary** — current `InvestigationPlan` compressed to branch titles,
   statuses, priorities, and top-level dependency structure.
3. **Active branch detail** — full hypothesis and evidence detail ONLY for the
   branch(es) being worked on this turn.
4. **Recent signals** — last critic feedback, last tool failures, last
   materialization report.
5. **Compact history** — a running summary of what has been done so far, NOT
   the full transcript of all prior turns.

#### What must NOT be in active context

- Full evidence text from all branches (summarize instead)
- Complete research memos from past turns (extract key findings into epistemic state)
- Tool definitions for tools unlikely to be used this turn
- Raw web content from researcher results

#### Context compression protocol

After every N turns (recommended: every 3-5 turns, or whenever active context
exceeds 60% of the window), the orchestrator should produce a **state
compaction**:

- Compress completed branches into one-line summaries
- Replace detailed evidence lists with evidence summary strings
- Archive resolved contradictions
- Promote confirmed hypotheses to `known_facts`

This compaction is written to session state and becomes the new baseline for
subsequent turns. The full detail remains in session state for audit, but the
orchestrator's active context only carries the compact version.

#### Long investigations

For investigations that span many turns (20+), the orchestrator should:

- rely on structured session state rather than conversation history
- use plan state and epistemic state as the primary reasoning input
- treat the conversation transcript as lossy — anything important must be in
  structured state

This is why the session state schema is "more important than the chat
transcript" as stated earlier.

---

### Convergence and Termination Mechanics

The environment must have concrete mechanisms for measuring progress and
deciding when to stop. "Marginal information gain below threshold" is the
principle — this section defines how it works in practice.

#### Progress measurement

Progress is measured per-branch and globally.

**Per-branch progress:**
- Number of hypotheses that moved from `proposed` → `supported` or `rejected`
  in the last N turns
- Change in `materialization_readiness` score over the last N turns
- Whether the branch's `open_questions` list is shrinking or growing

**Global progress:**
- Fraction of initial branches that have reached a terminal status
  (`materializable`, `closed`, `candidate_only`)
- Number of contradictions that have been resolved vs newly created
- Total remaining open questions vs total resolved

#### Stall detection

A branch is stalling when:
- Two consecutive research rounds produced no hypothesis status change
- The same open questions persist after re-framing
- The critic gives the same `gaps` list twice

A global stall occurs when:
- No branch made progress in the last 3 turns
- Total open questions increased despite research effort
- Budget utilization exceeds 70% with less than 50% of branches resolved

#### Termination rules

When stall is detected, the orchestrator must choose:

1. **Reframe and retry** (max once per branch per stall) — change the
   research question angle, try different source types, narrow the scope.
2. **Downgrade the branch** to `candidate_only` — the evidence is insufficient
   for materialization but may be useful as a candidate for future work.
3. **Downgrade to `needs_human_review`** — the investigation hit a dead end
   that may require human judgment or access to non-public sources.
4. **Terminate the investigation** — all actionable branches are resolved or
   downgraded. Produce a final summary.

The orchestrator should NOT:
- Keep retrying the same approach hoping for different results
- Invent evidence to fill gaps
- Force-materialize claims that the critic or deterministic tools rejected

---

### Progressive Materialization and Backtracking

For non-trivial investigations, materialization should not be all-or-nothing.

#### Progressive materialization

When `materialization_strategy` is `progressive` or
`progressive_with_rollback`:

- Branches that reach `confirmed_materializable` are materialized promptly,
  even while other branches are still being researched.
- Each materialization round produces a `MaterializationReport` that is
  recorded in session state.
- Materialized facts are added to the `known_facts` in epistemic state and
  become available as anchor context for subsequent branches.

Benefits:
- Reduces active context size (materialized branches can be compressed)
- Allows downstream branches to use freshly materialized facts
- Demonstrates incremental value even if the investigation is interrupted

#### Backtracking

When new evidence contradicts a previously materialized fact:

1. The orchestrator records a `correction_proposal` in session state.
2. The correction proposal includes: what was materialized, what new evidence
   contradicts it, what the replacement claim is, and the evidence strength
   of both the original and the replacement.
3. The materializer is dispatched with explicit correction semantics
   (`deprecate_existing`, `contradict_existing`, `replace_existing`).
4. The correction is traceable in the materialization report.

Backtracking is expensive and should be rare. The design prefers conservative
initial materialization (only materialize when the critic confirms) over
aggressive materialization followed by frequent corrections.

#### What happens when the critic and orchestrator disagree

If the critic says `fail` but the orchestrator believes the evidence is
sufficient:

1. The orchestrator may re-submit to the critic with additional context or
   a narrower question — but only once.
2. If the critic fails the same branch twice, the orchestrator must either
   gather more evidence or downgrade the branch. It cannot override the
   critic by materializing anyway.
3. This rule prevents the orchestrator from rationalizing its way past
   quality gates.

---

### When to use direct tools vs subagents

The orchestrator should not spawn subagents for everything.

Use **direct tools** when:

- checking if an entity already exists
- reading graph neighborhood
- validating a candidate canonical path
- probing whether a branch is blocked on policy or geo ambiguity
- testing whether a write is likely to succeed

Use a **research subagent** when:

- a question requires multi-query evidence gathering
- the answer needs synthesis across web + internal knowledge
- a branch needs corroboration from multiple source types

Use a **critic subagent** when:

- multiple branches must be judged together
- contradictions or confidence mismatches need structured review
- the orchestrator suspects branch instability but wants an explicit pass/fail verdict

Use a **materializer subagent** when:

- the branch is believed to be write-ready
- corrections to existing graph facts are proposed
- multiple entities/edges need to be committed with deterministic validation

Use a **path-capable subagent or path mode** when:

- the investigation goal is target-specific supply realization rather than a
  generic graph of facts
- the orchestrator has enough graph facts to attempt path assembly
- the current bottleneck is continuity of a chain rather than existence of a single relation
- concrete supply links or logistics legs are part of the required deliverable

### Programmatic Tool Orchestration

One lesson from modern production agent harnesses is that not every multi-step
tool workflow should be expressed as repeated natural-language tool calls.

For tool-heavy deterministic loops, use code-based orchestration in a controlled
execution environment where possible. This is particularly useful for:

- batched graph neighborhood expansion
- branch scoring over many candidate entities/edges
- repeated deterministic prechecks over a candidate set
- aggregation of multiple search results before returning a compact summary

The model should therefore have two execution styles available:

- **natural-language reasoning** for branch choice, reframing, prioritization, and critique
- **programmatic orchestration** for loops, filtering, aggregation, and repeated tool invocation

This keeps intermediate noise out of the model's active context and makes control
flow more explicit.

### No hardcoded bypass for path logic

Supply-chain path construction is important enough that it may require extra tool
support and richer state. But it must still remain inside the same orchestrated
reasoning loop.

That means:

- no separate hardcoded supply-path engine that bypasses branch planning
- no unconditional pathbuilder pass that runs for every supply-chain case
- no standalone logistics branch outside orchestrator control

Instead:

- the orchestrator decides when path construction is required
- the orchestrator decides when a path attempt is premature
- the critic can reject a weak path hypothesis
- deterministic path tools help assemble, validate, and land paths once the
  orchestrator has gathered enough evidence

Path construction is therefore a first-class outcome of the environment, not a
separate system hiding behind it.

---

## Prompt and Context Architecture

The environment should not rely on one giant mutable prompt.

### Stable Prompt Layers

These should stay as cache-friendly and rarely-changing as possible:

- orchestrator role and mission
- epistemic control rules
- write-boundary rules
- canonical control-plane reminders
- high-level safety and bounded-iteration rules
- deterministic service failure semantics

### Dynamic Prompt Layers

These should be refreshed each turn:

- current task target
- lens context
- active branch summaries
- unresolved contradictions
- materialization candidates
- latest critic feedback
- latest deterministic denial reasons

### Tool Loading Discipline

The orchestrator should not load every tool definition into active context at
all times if the tool surface becomes large. The design should prefer:

- always-available core tools
- deferred / discoverable tools for less common operations
- compact tool descriptions focused on decision usefulness

The point is not minimalism for its own sake; it is preserving reasoning context
for the target investigation instead of wasting it on unused capability metadata.

---

## Production Harness Lessons Applied

The design below follows patterns that have become visible in modern
production-grade agent harnesses.

### 1. The harness is the product

The model is not the architecture. The value is in:

- the query loop
- context layering
- delegation rules
- tool governance
- retry and denial policy
- review and artifact discipline

This document therefore treats the reasoning engine as a runtime system, not as
"a good prompt for Gemini".

### 2. Tools should be loaded on demand

If the scouting environment grows to many deterministic services and optional
retrieval tools, the orchestrator should not pay the token cost of every tool
definition on every turn.

Design implication:

- keep a compact always-on core tool set
- let optional tools be discoverable/deferred
- keep active context focused on the current investigation rather than the whole tool catalog

### 3. Multi-step tool work should often be code, not prose

When the task is "call many tools, filter, loop, aggregate, and only keep the
important result", code-based orchestration is more reliable than repeated
natural-language tool round-trips.

Design implication:

- support controlled programmatic tool orchestration for repetitive deterministic loops
- keep raw intermediate outputs out of the orchestrator's main reasoning context

### 4. Action gating must be separate from reasoning

A production agent must not be governed only by its internal reasoning quality.
It also needs an action boundary.

Design implication:

- deterministic prechecks before risky writes or corrections
- bounded retries after denials
- denial reasons fed back as structured constraints, not as crashes
- mesh-level reviewer after task completion

### 5. Tool outputs are not automatically trustworthy

Search results, web pages, remote tool outputs, and external artifacts may
contain bad instructions, contaminated text, or irrelevant noise.

Design implication:

- screen or sanitize high-risk tool outputs before folding them into active context
- keep the orchestrator anchored on user intent and current branch goals
- do not treat external prose as instructions

### 6. Delegation needs its own governance

Subagents are not "free". Delegation changes scope, context, and cost.

Design implication:

- delegation should happen through explicit capability families
- outbound handoff and final return should be reviewable in AgentMesh traces
- subagent usage should be attributable per branch and per task

---

## Session State Schema

The reasoning engine needs a disciplined session state. This state is more
important than the chat transcript.

### Required top-level state objects

```python
class ScoutSessionState(BaseModel):
    target: dict
    target_kind: str
    tenant_id: str
    project_id: str | None = None
    lens_id: str | None = None
    runtime_mode: Literal["classic_scout", "agentic_scout", "shadow_compare", "agentic_primary_with_switchback"]
    investigation_plan: InvestigationPlan
    turn_counter: int = 0
    denial_counter: int = 0
    repeated_gap_counter: int = 0
    budget_state: dict
    graph_context: dict
    epistemic_state: dict
    hypotheses: list[Hypothesis]
    candidate_entities: list[dict]
    candidate_edges: list[dict]
    candidate_paths: list[dict]
    candidate_links: list[dict]
    candidate_legs: list[dict]
    research_memos: list[ResearchMemo]
    correction_proposals: list[dict]
    critic_feedback: list[CriticFeedback]
    materialization_reports: list[MaterializationReport]
    deterministic_failures: list[dict]
    context_compaction_log: list[str]
    final_outcome: str | None = None
```

Note: `InvestigationPlan`, `Hypothesis`, `ResearchMemo`, `CriticFeedback`,
`MaterializationReport` and their substructures (`InvestigationBranch`,
`BranchDependency`, `OpenQuestion`, `Finding`, `MaterializationFailure`)
are defined in the Reasoning Harness sections above. The session state is the
authoritative location for these objects — the orchestrator reads and writes
them explicitly at each turn, not implicitly through conversation history.

### Epistemic state payload

`epistemic_state` should minimally include:

- `known_facts` — facts confirmed by evidence and/or materialized
- `contradictions` — active contradictions between evidence sources
- `stale_claims` — previously known facts that new evidence weakens
- `resolved_contradictions` — contradictions that have been settled (archived)
- `high_value_unknowns` — gaps that would significantly advance the investigation
- `path_candidates` — supply-chain path hypotheses under construction
- `path_gaps` — identified continuity breaks in candidate paths
- `target_binding_gaps` — where paths are generic rather than target-specific
- `logistics_grounding_gaps` — where links/legs lack concrete evidence

Note: `active_hypotheses` and `open_questions` are now tracked as structured
objects in `session_state.hypotheses` and `session_state.investigation_plan.branches[].open_questions`
respectively, rather than as flat lists inside epistemic state. This makes their
lifecycle explicitly manageable.

### Branch object

Each active branch should be a first-class object:

```python
class InvestigationBranch(BaseModel):
    branch_id: str
    title: str
    branch_kind: Literal["supply_chain", "ownership", "regulatory", "market", "strategic", "path_construction", "logistics_routing", "mixed"]
    priority: float
    status: Literal["open", "researching", "critic_review", "materializable", "candidate_only", "closed", "needs_review"]
    core_question: str
    hypothesis_ids: list[str]
    evidence_summary: list[str]
    contradictions: list[str]
    deterministic_blocks: list[str]
    refinement_count: int = 0
    expected_information_gain: float = 0.0
    materialization_readiness: float = 0.0
    research_rounds_spent: int = 0
    last_progress_turn: int = 0
```

The orchestrator should think in terms of branches, not just a flat to-do list.

Branch kinds are descriptive labels for the current shape of an investigation.
They are not instructions to invoke separate hardcoded domain pipelines. The
orchestrator may move between these categories fluidly as it reasons.

Branch status is derived from hypothesis statuses (see "Hypothesis Lifecycle"
in the Reasoning Harness section). `research_rounds_spent` and
`last_progress_turn` are used by stall detection in the convergence mechanics.

### Path-scoped artifacts

For supply-chain investigations, the session may also contain target-scoped
artifacts that are not reducible to generic edges:

```python
class CandidatePath(BaseModel):
    path_id: str
    target_ref: str
    objective: str
    product_axis: list[str]
    node_refs: list[str]
    step_refs: list[str]
    confidence: float
    continuity_status: Literal["continuous", "broken", "ambiguous"]
    target_binding_status: Literal["bound", "weak", "unbound"]
    evidence_summary: list[str]

class CandidateLink(BaseModel):
    link_id: str
    path_id: str
    from_ref: str
    to_ref: str
    product_ref: str | None = None
    link_kind: str
    confidence: float

class CandidateLeg(BaseModel):
    leg_id: str
    link_id: str
    origin_ref: str
    destination_ref: str
    via_refs: list[str]
    movement_kind: Literal["supply", "shipment", "transfer", "assembly_flow", "unknown"]
    evidence_strength: Literal["high", "medium", "low"]
```

These artifacts are not special-cased outside the reasoning loop. They are the
objects the loop is sometimes trying to produce.

---

## Agents

### 1. Orchestrator (`scout_orchestrator`)

**Role**: The reasoning core. Plans, dispatches, evaluates, re-plans, decides to materialize.

**Model**: `gemini-2.5-pro` (needs strong reasoning + long context for plan/re-plan).

**Type**: `LlmAgent` with sub-agents as tools (`AgentTool`).

**AgentMesh capability**: primary implementation behind `scout.plan`, with authority
to invoke `scout.research`, `scout.critic`, `scout.materialize`, `scout.path`,
and optionally `scout.aggregate`.

**Has access to**:
- `researcher` (as AgentTool) — to dispatch focused research tasks
- `critic` (as AgentTool) — to request quality review of accumulated evidence
- `materializer` (as AgentTool) — to commit validated findings to the knowledge graph
- Direct tools: `search_knowledge_base`, `lookup_fragment`, `get_graph_neighborhood`,
  `resolve_canonical_path`, `validate_canonical_path`, `geo_coord_mapping_status`,
  `policy_precheck`

**Instruction core** (not a prompt — a reasoning frame):
```
You are a geopolitical and economic intelligence analyst.

Given a scouting target (entity, signal, or question), your job is to build
a complete, validated intelligence picture and materialize it as structured
knowledge.

You reason about WHAT to investigate, dispatch researchers to gather evidence,
critically evaluate what comes back, and decide when the picture is complete
enough to materialize.

You understand that investigations may involve:
- Supply/service chains (who supplies what to whom, through which facilities,
  via which products/transforms)
- Ownership and control (who owns whom, beneficial ownership, state control,
  corporate hierarchies)
- Regulatory and policy impact (which regulations affect which entities,
  sanctions, trade controls, compliance requirements)
- Market structure (who competes, market segments, market shares)
- Strategic positioning (alliances, dependencies, leverage points)
- Risk vectors (concentration, geopolitical exposure, single points of failure)

You do NOT need separate instructions for each domain. You reason about what
matters for THIS specific target and plan accordingly.

TOOLS available to you:
- researcher: dispatch a focused research task with a specific question
- critic: ask for quality/completeness review of current findings
- materializer: commit validated findings to the knowledge graph
- search_knowledge_base: check what we already know about an entity
- lookup_fragment: get a specific entity by canonical path
- get_graph_neighborhood: see existing connections around an entity
- resolve_canonical_path: canonicalize a candidate entity or product
- validate_canonical_path: check whether a candidate path is structurally valid
- policy_precheck: test whether a proposed write is likely to be rejected
- geo_coord_mapping_status: determine whether geographic evidence is strong enough
  for facility/location materialization

CANONICAL RULES:
{canonical_rules_block}

MATERIALIZATION RULES:
- Before creating any entity, check if it already exists (search_knowledge_base)
- Use resolve_canonical_path tool to get the correct path
- Every edge must have a valid type from the allowed set
- Product identity uses canonical product paths, not raw names
- If policy/tool output says "underspecified", gather missing context instead of
  trying to force materialization
- If geo mapping is ambiguous, keep the claim in a candidate/unresolved state
  until the ambiguity is narrowed
```

**Behavior** (emergent, not hardcoded):
- On receiving a scouting target, first checks what's already known
- Formulates 3-7 research questions based on what's missing
- Dispatches researchers (can be parallel via ADK)
- Reviews accumulated evidence, identifies gaps
- Uses deterministic tools opportunistically during reasoning to test whether a
  branch is materializable or still underspecified
- Decides whether a supply-chain investigation requires path construction, link
  construction, or concrete logistics legs rather than only generic edge creation
- May dispatch more research, or ask critic for review
- When satisfied, calls materializer to commit
- Terminates with a structured summary or an explicit unresolved/candidate outcome

### 2. Researcher (`scout_researcher`)

**Role**: Focused evidence gatherer. Takes a specific question, searches, returns
structured findings.

**Model**: `gemini-2.5-flash` (fast, grounding-capable, cost-effective for search).

**Type**: `LlmAgent` with `google_search` tool + `search_knowledge_base` tool +
`lookup_fragment` tool.

**AgentMesh capability**: `scout.research`

**Instruction core**:
```
You are an evidence-gathering researcher.

You receive a specific research question and context from the orchestrator.
Your job is to find high-quality, verifiable evidence that answers the question.

For each finding:
- State the fact clearly
- Note the source (URL, publication, date)
- Assess confidence (high/medium/low)
- Flag contradictions or ambiguities

You search both the open web (google_search) and the existing knowledge base
(search_knowledge_base) to find evidence. Prefer official sources, filings,
operator pages, and high-signal trade press over generic summaries.

Do NOT make strategic judgments. Do NOT decide what to investigate next.
Report what you found and let the orchestrator decide.

Output: structured research memo in the session state.
```

**Parallelism**: The orchestrator can dispatch multiple researchers simultaneously
for independent questions. ADK supports this via multiple `AgentTool` calls.

### 3. Critic (`scout_critic`)

**Role**: Quality gate. Reviews accumulated evidence for completeness,
contradictions, and confidence.

**Model**: `gemini-2.5-pro` (needs reasoning to spot gaps and contradictions).

**Type**: `LlmAgent` with `output_schema=CriticFeedback`.

**AgentMesh capability**: `scout.critic`

**Instruction core**:
```
You are a rigorous intelligence analyst reviewer.

Review the accumulated research findings against the orchestrator's
investigation goals. Evaluate:

1. Evidence completeness — are all key questions answered?
2. Source quality — are findings backed by credible, verifiable sources?
3. Internal consistency — do findings contradict each other?
4. Confidence calibration — are confidence levels appropriate?
5. Missing connections — are there obvious relationships not yet explored?

If the evidence is sufficient for materialization: grade "pass".
If gaps remain: grade "fail" with specific questions that need answering.

Be especially strict about:
- Ownership claims without official source backing
- Supply chain links without facility/product specificity
- Regulatory claims without specific regulation/date references
```

**Output schema**:
```python
class CriticFeedback(BaseModel):
    grade: Literal["pass", "fail"]
    confidence: float  # 0.0-1.0
    strengths: list[str]
    gaps: list[str]
    contradictions: list[str]
    follow_up_questions: list[str] | None = None
```

### 4. Materializer (`scout_materializer`)

**Role**: The write gate. Takes validated findings and commits them to the
knowledge graph through deterministic tools.

**Model**: `gemini-2.5-flash` (structured output, tool calling).

**Type**: `LlmAgent` with write tools.

**AgentMesh capability**: `scout.materialize`

**Tools**:
- `resolve_canonical_path` — deterministic canonical path resolution
- `validate_and_create_fragment` — creates fragment with pre-commit validation
- `validate_and_create_edge` — creates edge with type/direction validation
- `lookup_product_transform` — checks product transform dictionary
- `lookup_product_synonym` — checks product synonym dictionary
- `search_knowledge_base` — dedup check before creation

**Instruction core**:
```
You are a knowledge graph materializer.

Given validated research findings from the orchestrator, your job is to
create or update entities and relationships in the knowledge graph.

BEFORE creating anything:
1. Check if the entity already exists (search_knowledge_base)
2. Resolve the canonical path (resolve_canonical_path)
3. For products, check the synonym dictionary and transform dictionary
4. For edges, verify the edge type is in the allowed set

RULES:
- Never create duplicates. If an entity exists, update it.
- Use canonical paths, not raw names.
- Every facility needs a geographic context.
- Every supply chain link needs: source → product → destination.
- Product identity is determined by canonical product path.
- If a product or relationship is not in the dictionaries, write it to
  the candidate collection — do not invent ad-hoc paths.

Output: structured materialization report (what was created/updated/skipped).
```

---

### 5. Path Synthesis / Path Landing (`scout.path`)

**Role**: Assemble, validate, and land target-specific supply paths, links, and
legs when the orchestrator determines that the investigation requires more than
generic entity-edge graph construction.

**Model**: `gemini-2.5-flash` or `gemini-2.5-pro` depending on path complexity and
how much reasoning vs deterministic expansion is required.

**AgentMesh capability**: `scout.path`

**Important**:
This is not a hardcoded bypass around the orchestrator. It is a capability the
orchestrator may invoke when:

- a supply-chain target requires a path artifact
- enough upstream facts exist to attempt path synthesis
- the critic indicates that graph facts alone are insufficient

**Core tasks**:

- bind a path to a specific target actor or objective
- assemble canonical product steps into a coherent candidate chain
- identify continuity breaks
- distinguish generic market adjacency from target-specific path relevance
- produce candidate links and candidate legs
- send weak or unresolved cases back as explicit gaps rather than forcing a path

**Output**:

- candidate paths
- candidate links
- candidate legs
- path gaps
- path continuity assessment
- target-binding assessment

The output of `scout.path` may still be rejected by the critic, refined by more
research, or landed by `scout.materialize`.

---

## Mesh-Level Review and Finalization

Inside `AgentMesh`, the environment should still participate in the existing
runtime discipline of worker → reviewer → final task outcome.

### `scout.review`

This is distinct from the internal critic.

- The **critic** helps the orchestrator decide whether to continue research.
- The **mesh reviewer** decides whether the completed task is acceptable as a
  mesh artifact and whether writes / corrections / outputs satisfy runtime policy.

The reviewer should validate at least:

- final outcome classification
- artifact completeness
- write provenance
- correction semantics presence when existing graph facts were changed
- denial / retry limits were respected
- final cost and trace snapshots are persisted

### `scout.aggregate`

Optional final synthesis step for:

- executive narrative
- technical narrative
- lens-side summary
- shadow-compare artifact
- human-review packet

This keeps orchestration output separate from presentation artifacts.

---

## Tools (deterministic, not reasoning)

These are the system's invariant-enforcement layer. They replace what used to be
hardcoded in the pipeline. The reasoning core must know these tools exist, what
they are for, and the kinds of failure states they can emit. But the reasoning
core must not attempt to reproduce their logic internally.

| Tool | What it does | Replaces |
|------|-------------|----------|
| `search_knowledge_base` | Vertex AI Search over indexed fragments | `planner.vertexSearchClient.Search` |
| `lookup_fragment` | Firestore get by canonical path | `fragmentsRepo.GetByCanonicalPath` |
| `get_graph_neighborhood` | Get edges + connected fragments for an entity | `edgesRepo.GetBySource/Target` |
| `resolve_canonical_path` | Cache → Session → Firestore → Vertex → LLM chain | `resolveCanonicalPath` |
| `validate_canonical_path` | Structural + type validation | `isCanonicalPathValid` + `canonicalPathMatchesType` |
| `policy_precheck` | Preflight check before a write or graph correction | Policy / enforcement gates |
| `geo_coord_mapping_status` | Assess whether facility/location evidence is geo-materializable | Geo mapping / coordinate revalidation |
| `expand_canonical_product_chain` | Expand canonical product steps from transform / assembly dictionaries | Product path / pathbuilder substrate |
| `build_candidate_supply_paths` | Assemble target-scoped supply paths from known nodes and product steps | Materialized path synthesis layer |
| `validate_supply_path` | Check continuity, target binding, and path admissibility | Path validation / pathbuilder |
| `materialize_supply_path` | Persist a validated target-scoped supply path artifact | Materialized path runtime |
| `materialize_supply_link` | Persist a path link between two concrete path nodes | Supply link materialization |
| `materialize_supply_leg` | Persist a concrete operational leg from point to point | Logistics / leg materialization |
| `validate_and_create_fragment` | Schema check + dedup + create | Fragment creation pipeline |
| `validate_and_create_edge` | Edge type validation + create | Edge creation pipeline |
| `lookup_product_transform` | Product transform dictionary | `ProductCanonicalizer` |
| `lookup_product_synonym` | Product synonym dictionary | Synonym resolution |
| `google_search` | Web search with grounding | Built-in ADK tool |

### Deterministic Service Contract

The deterministic layer serves three roles:

1. **Constraint surface for reasoning**
   The orchestrator knows that writes can fail because of invalid canonical paths,
   missing geo context, forbidden edge types, missing product transform resolution,
   or policy rejection.

2. **Authoritative validation**
   The deterministic layer, not the orchestrator, decides whether a path is valid,
   whether a geo claim is sufficiently grounded, and whether a proposed write is
   allowed.

3. **Research signal generation**
   Rejections such as `underspecified`, `ambiguous_geo`, `candidate_only`,
   `edge_forbidden`, or `policy_rejected` are not just write failures. They become
   inputs to the orchestrator's next research step.

The orchestrator should therefore reason in the presence of these services, not
in ignorance of them and not by reimplementing them.

### Supply-chain path semantics

For supply-chain investigations, the deterministic layer must support three
distinct but related outputs:

1. **Graph facts**
   General entities and relations such as organizations, facilities, products,
   transforms, assemblies, dependencies, and ownership/control links.

2. **Target-scoped materialized paths**
   Canonical chains that answer a specific question for a specific target, such as
   "for NVIDIA, this chip capability is realized through these upstream products,
   actors, facilities, and steps".

3. **Operational links and legs**
   Concrete path segments and point-to-point shoulders used for route realism,
   logistics exposure, and operational chain interpretation.

The reasoning environment should understand that the presence of a good graph does
not automatically imply the existence of a valid target-scoped path artifact.
Path artifacts must be explicitly assembled, validated, and landed.

### Recommended Tool Return Semantics

Deterministic services should return compact, machine-meaningful statuses rather
than prose-heavy blobs. Examples:

- `resolve_canonical_path`
  - `resolved`
  - `ambiguous`
  - `candidate_only`
  - `rejected`

- `geo_coord_mapping_status`
  - `sufficient`
  - `approximate_only`
  - `ambiguous_geo`
  - `missing_geo`

- `policy_precheck`
  - `allowed`
  - `allowed_with_warning`
  - `underspecified`
  - `rejected`

- `validate_and_create_edge`
  - `created`
  - `updated`
  - `duplicate`
  - `edge_forbidden`
  - `candidate_only`

- `validate_supply_path`
  - `continuous`
  - `broken`
  - `ambiguous`
  - `target_unbound`
  - `candidate_only`

- `materialize_supply_leg`
  - `created`
  - `updated`
  - `insufficient_grounding`
  - `candidate_only`

The orchestrator should learn these status codes as part of its stable prompt layer.

---

## Epistemic Control

This section defines how the environment reasons about existing knowledge,
insufficient evidence, unstable branches, corrections, and stopping conditions.

### Existing Graph Semantics

Existing graph state is treated as prior knowledge, not absolute truth.
For any pre-existing entity or edge, the environment may classify it as:

- `confirmed`
- `strengthened`
- `specialized`
- `weakly_supported`
- `stale`
- `contradicted`
- `candidate_only`

Materialization should never silently overwrite the meaning of an existing fact.
If an existing fact changes, the system should emit correction semantics such as:

- `confirm_existing`
- `strengthen_existing`
- `specialize_existing`
- `deprecate_existing`
- `contradict_existing`
- `replace_existing`

Every correction should carry a trace stating what existed before, what is now
asserted, which evidence triggered the change, and whether the correction is
reversible or still provisional.

### Data Completeness Model

The orchestrator should not decide "this is incomplete" by intuition alone.
Each active investigation branch should be evaluable across these dimensions:

- `entity_coverage` — do the key entities exist?
- `relation_coverage` — do the key relationships exist?
- `mechanism_coverage` — do we know why the relationship exists?
- `source_strength` — do we have strong enough sources?
- `spatiotemporal_specificity` — do we know where and when this is true?
- `directionality_and_role` — do we know source/target direction and each actor's role?
- `alternative_explanations` — do competing interpretations remain unresolved?
- `path_continuity` — can the known facts be assembled into a coherent path?
- `target_binding` — is the path specific to the target, or only generically relevant?
- `logistics_grounding` — are concrete links or legs grounded strongly enough where required?

A branch is incomplete when one or more of these remain weak for a claim that is
structural to the target investigation.

### Branch Selection and Research Prioritization

The orchestrator should choose the next research step by expected information gain,
not by domain labels.

Priority should go to branches that:

- unlock multiple downstream branches
- resolve contradictions
- reduce uncertainty around structurally important claims
- raise a claim from candidate/ambiguous to materializable
- turn a generic chain hypothesis into a target-scoped path artifact
- are likely to succeed with one more high-quality research pass

This makes branch ordering adaptive. A regulatory sub-branch may come first in one
case and last in another, depending on what is blocking understanding.

### Branch Stability and Query Reframing

A branch is unstable when:

- it depends on one weak source
- it contains a long inference chain with few direct facts
- new sources diverge instead of converging
- the critic repeatedly rejects the same key claim
- deterministic tools return ambiguity or under-specification
- the chain can be sketched as a generic industry path but cannot be bound to the target
- logistics shoulders exist only as broad guesses without point-to-point grounding

When a branch is unstable, the orchestrator should not merely broaden search.
It should reframe the question. Examples:

- from "Who supplies X?" to "What direct evidence exists of commercial or logistics linkage to X?"
- from "Does A own B?" to "Which official filings or registry records establish ownership or control?"
- from "Is there regulatory impact?" to "Which exact law, date, jurisdiction, and affected subject establish this impact?"
- from "This is the upstream chain" to "Which exact steps in this chain are evidenced specifically for the target, and where does the chain break?"
- from "There is a route through these facilities" to "Which point-to-point links and legs are concretely evidenced versus merely inferred?"

This is query reframing rather than blind query expansion.

### Bounded Iteration and Termination

The environment must stop not only when it succeeds, but also when it can no
longer productively improve the picture.

The detailed mechanics of progress measurement, stall detection, and termination
rules are defined in "Convergence and Termination Mechanics" in the Reasoning
Harness section. The principles below remain the governing intent.

Termination may occur when:

- the critic passes the current evidence as sufficient for materialization
- per-branch or global progress stalls (see stall detection rules)
- the same gaps repeat across refinement rounds
- the same deterministic rejections repeat without better context
- cost/time budget exceeds expected value of another pass
- the branch is downgraded to `candidate_only` or `needs_human_review`

This means the system has at least three valid end states:

- `materialized`
- `partially_materialized_with_candidates`
- `unresolved_needs_review`

---

## Structural Reasoning Categories

The environment must be able to reason fluently across several recurring
structural categories of investigation. These are not separate pipelines.
They are recurring shapes of world structure that the orchestrator should be
able to recognize, investigate, critique, and materialize when relevant.

### Ownership / Beneficial Ownership / Control

The environment should understand that ownership/control work is not just
"find a few OWNS edges". The current codebase already treats this domain with
more structure:

- `ownership-structure` is fundamentally hierarchical
- `control-map` is fundamentally influence/control oriented
- both rely on supporting evidence beyond a single direct relation

The reasoning environment should therefore be able to think in terms of:

- ultimate owners
- downstream owned branches
- beneficial ownership
- voting rights
- stake percentages
- control mechanisms
- people-layer and board-layer support
- community/media/operates-via support where relevant

This mirrors important details in the current contracts:

- primary ownership edges include `OWNS`, `SUBSIDIARY_OF`, `CONTROLS`, `INVESTS_IN`
- supporting structure includes `HAS_SUBGROUP`, `MEMBER_OF`, `AFFILIATED_WITH`,
  `OPERATES_VIA`, `BOARD_MEMBER_OF`, `EMPLOYED_BY`, `FOUNDED`, `MANAGES`,
  `ADMINISTERS`
- thin ownership nodes are currently identified when evidence, support layer, or
  control qualifiers are missing
- useful qualifiers include `ownership_percent`, `stake_pct`, `voting_percent`,
  `voting_rights_percent`, `share_type`, `control_type`, and `mechanism`

The agentic environment should absorb this as reasoning ontology:

- if a graph contains an ownership chain but not its evidentiary support, the
  orchestrator should treat the chain as thin rather than complete
- if control exists without explicit ownership, that should still be representable
- if ownership/control can only be supported through people/community/media layers,
  the orchestrator should understand that these layers may be part of a complete answer

The desired outcome is not a domain-specific ownership pipeline, but a graph that
can be intelligently strengthened toward:

- hierarchical ownership structure
- influence/control map
- explicit control semantics
- evidentiary annotations on control claims

### Policy Impact / Regulatory Mechanism Structure

The environment should understand that policy impact is also a structured graph
shape, not just a bag of "regulates" edges.

The current codebase already implies a layered causal model:

- `/gov/` layer
- `/decision/` layer
- `/mechanism/` layer
- affected layer (`/org/`, `/fac/`, `/market/`, `/product/`, `/community/`, `/program/`)
- optional transmission layers (`/loc/`, `/community/`, `/program/`, `/media/`)

The reasoning environment should be able to recognize and investigate this shape:

- regulator or governing authority
- decisions and gatekeeping
- mechanisms or instruments
- imposed conditions / controls / access constraints
- affected entities, facilities, markets, products, or communities
- geographic scope
- program execution or media/community transmission where present

This mirrors key details in the current validators:

- structural edge validation depends on path-prefix compatibility
- reachability is evaluated from `/gov/` seeds through decisions and mechanisms
- orphan mechanism/decision nodes are treated as suspicious
- graph sufficiency distinguishes missing gov/decision/mechanism/affected layers
- qualifier completeness matters for regulatory claims
- temporal consistency matters for `effective_date`, `phase_in_*`, and sunset logic
- quantitative qualifiers such as `rate_from` / `rate_to` improve validation quality

The agentic environment should therefore reason like this:

- a regulatory graph with no government seed is structurally weak
- a mechanism that does not connect back to a governing or decision layer is incomplete
- a policy-impact answer without affected objects or geo scope may be insufficient
- a mechanism with broken temporal logic should not be treated as a strong landed fact

Again, this does not mean a separate regulatory pipeline. It means the
orchestrator must know how to see and improve this structural form when the
investigation calls for it.

### Supply-Chain Pathing

Supply-chain work often needs:

- factual graph structure
- target-scoped path construction
- link / leg grounding

This is covered in detail in `Supply-Chain Path Materialization` below and should
be treated as another structural category the orchestrator can enter when the
investigation requires it.

### Unifying Principle

Ownership/control, policy impact, and supply-chain pathing are not separate
algorithmic runtimes. They are:

- recurring structural forms in the graph
- recurring evidence patterns
- recurring insufficiency patterns
- recurring landing targets

The orchestrator should be able to recognize these forms, reason about their
gaps, ask the right follow-up questions, and improve the graph accordingly.

---

## Supply-Chain Path Materialization

Supply-chain work in this environment must not be interpreted narrowly as
"creating entities and some generic edges".

For many target investigations the real output is a target-specific path:

- not only "TSMC manufactures chips"
- but "for NVIDIA, this capability is realized through this chain of products,
  actors, facilities, links, and legs"

This requires the environment to reason from facts toward paths.

### Three-layer outcome model

When working a supply-chain target, the orchestrator may need to produce all three
of the following layers:

1. **Entity-edge graph**
   The world facts: who exists, what facilities exist, what products exist, who
   transforms or assembles what, who depends on whom.

2. **Materialized path**
   A target-scoped chain that binds those facts into an interpretable supply path.

3. **Operational pathing**
   Concrete links and legs between points, facilities, terminals, corridors, or
   operational stages when logistics realism matters.

### Path construction stays agentic

Path construction must remain a result of:

- planning
- research
- critique
- replanning
- execution
- landing

It must not be implemented as a hidden hardcoded branch that runs regardless of
whether the orchestrator thinks it is needed.

### Typical path-oriented reasoning flow

For a supply-chain case, the orchestrator should reason roughly like this:

1. Build the factual substrate:
   entities, facilities, products, transforms, assemblies, ownership, dependencies.

2. Decide whether the target requires a path artifact:
   is a generic graph sufficient, or does the question require a target-bound chain?

3. If a path is required, attempt candidate path synthesis:
   use canonical product identity, transform/assembly dictionaries, and known graph
   facts to assemble the best current path hypothesis.

4. Test continuity and target specificity:
   where does the chain break, where are hops generic rather than target-bound,
   where are logistics shoulders missing?

5. Research the breaks:
   dispatch focused research to close specific continuity gaps instead of broadly
   repeating the whole supply-chain investigation.

6. Re-attempt path synthesis:
   update path hypotheses, links, and legs.

7. Land only what is sufficiently grounded:
   materialize the continuous and justified part of the path; keep the rest as
   candidates or unresolved gaps.

### Consequence for the environment

This means that in supply-chain investigations, graph quality includes not only:

- better fragments
- better edges
- better corrections

but also:

- more truthful target binding
- more coherent canonical product chains
- fewer broken materialized paths
- fewer fake or over-inferred logistics legs

That is why path quality must be treated as a first-class dimension of graph quality.

---

## Execution Flow (emergent, not prescribed)

There is NO fixed stage graph. The orchestrator reasons. But a **typical** flow
for "Investigate Tesla's battery supply chain" might look like:

```
Orchestrator thinks:
  "Battery supply chain for Tesla. Let me check what we already know."

  → search_knowledge_base("Tesla")
  → get_graph_neighborhood("/org/us/tesla")

  "We have Tesla as an org, some existing suppliers. But no battery-specific
   supply chain. I need to investigate:
   1. Who are Tesla's battery cell suppliers?
   2. What raw materials go into Tesla's batteries?
   3. Where are the key processing facilities?
   4. What are the regulatory constraints (e.g., IRA, EU battery regulation)?"

  → researcher("Who are Tesla's current battery cell suppliers? Include
     CATL, Panasonic, LG, BYD, and any new entrants. Need facility locations.")

  → researcher("What raw materials are in Tesla's battery cells?
     Lithium, nickel, cobalt, graphite — trace the supply chain to mines/refiners.")

  → researcher("What regulations affect Tesla's battery supply chain?
     IRA domestic content requirements, EU Battery Regulation, CBAM.")

  [researchers return findings in parallel]

Orchestrator reviews:
  "Good findings on suppliers and materials. Regulatory coverage is thin.
   Let me get the critic's view."

  → critic(review accumulated findings against goals)

Critic responds:
  grade: "fail"
  gaps: ["No facility-level detail for CATL plants supplying Tesla",
         "Missing cobalt refining stage between mine and cathode",
         "No IRA compliance status for each supplier"]

Orchestrator re-plans:
  → researcher("CATL facilities that supply Tesla — specific plant names,
     locations, capacity. Include Kirin battery plants.")

  → researcher("Cobalt refining companies between DRC mines and battery
     cathode manufacturers. Focus on Umicore, Freeport Cobalt, CNGR.")

  [researchers return]

Orchestrator:
  "Now I have enough for materialization."

  → materializer(create fragments + edges for the supply chain graph)

Materializer:
  → resolve_canonical_path("CATL", type=ORGANIZATION) → /org/cn/catl
  → lookup_fragment("/org/cn/catl") → exists, update
  → validate_and_create_edge(tesla → SUPPLIED_BY → catl, product=battery-cell)
  → resolve_canonical_path("Lithium carbonate", type=PRODUCT)
     → /product/raw/chemical/refining/lithium-carbonate
  ... etc

  Returns: materialization report
```

The flow above could have been 3 rounds or 8. The orchestrator decided.
No `pipeline_orchestration.go` needed.

Note: the Tesla example above is a simplified happy path. It does not show
contradictions, tool failures, reframing, backtracking, or multi-structural
reasoning. The following example does.

### Hard case: SMIC advanced chip capacity under US export controls

This example shows a complex investigation with contradictions, tool failures,
reframing, progressive materialization, and multi-structural reasoning
(supply chain + regulatory impact + ownership/control).

```
TURN 1 — Ingest + Anchor + Frame

Orchestrator:
  Target: "SMIC's advanced semiconductor manufacturing capacity and how
  US export controls affect it."

  → search_knowledge_base("SMIC")
  → get_graph_neighborhood("/org/cn/smic")

  Anchor results: SMIC exists as /org/cn/smic. Some basic edges:
  headquartered in Shanghai, listed on STAR Market. No equipment supply
  chain. No regulatory impact structure. Thin ownership (state ownership
  suspected but no structured evidence).

  FRAME (plan version 1):
    Structural categories detected: supply_chain, regulatory, ownership
    Branches:
      B1: Equipment supply chain — who supplies SMIC's lithography,
          deposition, etch, metrology equipment (priority: 0.9)
      B2: Regulatory impact — US export controls on SMIC, specific
          rules, mechanisms, affected products (priority: 0.8)
      B3: Ownership/control — state ownership, CCP influence,
          beneficial ownership structure (priority: 0.6)
      B4: Path construction — target-scoped path for advanced node
          capability (priority: 0.7, blocked on B1)
    Dependencies:
      B4 blocks_on B1 (cannot build a path without equipment substrate)
      B2 informs B1 (controls constrain which equipment can flow)
    Materialization strategy: progressive

  Hypotheses:
    H1 (B1): ASML is the primary lithography supplier, but EUV is blocked
    H2 (B1): Applied Materials, Lam Research, KLA supply deposition/etch/metrology
    H3 (B2): Entity List restrictions block EUV and advanced DUV tools
    H4 (B2): October 2022 and October 2023 rules progressively tightened access
    H5 (B3): Chinese state entities hold controlling stake via IC funds

  ACTION: dispatch researchers for B1 and B2 in parallel (B3 lower priority,
    B4 blocked)

  → researcher(branch=B1, question="What semiconductor equipment does SMIC
     use for advanced nodes (7nm, 5nm attempts)? Specific equipment models,
     suppliers, facility locations. Focus on lithography, deposition, etch.")

  → researcher(branch=B2, question="What specific US export control rules
     target SMIC? Entity List entries, BIS rules from Oct 2022 and Oct 2023,
     specific ECCN categories, license requirements.")


TURN 2 — Interpret research results + re-assess

  Researcher B1 returns:
    findings:
      - ASML supplied DUV (TWINSCAN NXT:2000) but EUV blocked since 2019
      - Applied Materials, Lam Research, KLA historically supplied tools
      - After Oct 2022 rules, even advanced DUV may require license
      - SMIC reportedly using multi-patterning DUV for 7nm (Huawei Kirin 9000S)
      - SOME SOURCES claim SMIC acquired "a few" EUV machines before the ban;
        OTHER SOURCES say zero EUV machines reached SMIC
    contradictions: ["EUV machine delivery to SMIC: conflicting reports"]
    confidence: medium

  Researcher B2 returns:
    findings:
      - SMIC added to Entity List December 2020
      - BIS rule October 7, 2022: restrictions on advanced compute and
        semiconductor manufacturing equipment
      - BIS rule October 17, 2023: further tightening, closed loopholes
      - Specific ECCNs: 3B001 (equipment), 3B002 (masks and substrates)
    confidence: high

  Orchestrator INTERPRET:
    H1 updated: supported (DUV confirmed, EUV blocked) BUT contradiction
      on whether any EUV units were delivered pre-ban
    H2 updated: supported (Applied/Lam/KLA confirmed as historical suppliers)
    H3 updated: supported with high confidence
    H4 updated: supported with high confidence

    Contradiction registered: "EUV delivery to SMIC — 0 units vs a few units"
    This contradiction affects B1 and B4 (path accuracy depends on whether
    EUV capability exists at SMIC)

  PLAN STATE: B1 has a contradiction, B2 is near-materializable, B4 still blocked
  HIGHEST GAIN: resolve the EUV contradiction in B1 — it's structural
  ACTION: reframe research question for the contradiction

  → researcher(branch=B1, hypothesis=H1,
     question="Did SMIC ever take delivery of an ASML EUV lithography system?
     Seek: ASML annual reports, shipping records, Congressional Research Service
     reports, credible industry analysts (not social media). The question is
     specifically whether any EUV unit was physically delivered and installed,
     not whether one was ordered.",
     evidence_to_corroborate=["Bloomberg 2023 report claiming 1 unit"],
     evidence_to_refute=["ASML CEO statement saying no EUV shipped to China"])


TURN 3 — Resolve contradiction + materialize B2 + start B3

  Researcher returns on EUV question:
    findings:
      - ASML CEO Peter Wennink stated publicly: no EUV system shipped to China
      - ASML 2022 annual report shows zero China EUV revenue
      - Congressional Research Service 2023 report confirms no EUV delivery
      - The "few EUV machines" claim traces to unverified social media posts
    confidence: high
    contradictions: resolved — no EUV delivered

  Orchestrator INTERPRET:
    H1 contradiction resolved: zero EUV machines at SMIC. SMIC uses
    multi-patterning DUV only for advanced nodes.
    H1 status: supported → confirmed_materializable (for DUV part)
    B1 status: researching → partially materializable (equipment substrate
    is clear enough for B4 to unblock)

  B2 was already near-ready. Call critic on B2.

  → critic(review B2: regulatory impact structure for US export controls on SMIC)

  Critic responds:
    grade: pass
    strengths: ["Specific rules cited", "ECCNs identified", "Timeline clear"]
    gaps: ["No /gov/ seed for BIS/Commerce Department in graph yet",
           "No /mechanism/ node for Entity List entry"]

  Orchestrator:
    Critic passed B2 on evidence quality but noted structural gaps in the
    regulatory graph form. This is exactly what Structural Reasoning Categories
    predicts — a regulatory answer needs gov → decision → mechanism → affected.

    → materializer(branch=B2,
       entities: [/gov/us/doc-bis, /decision/us/entity-list-smic-2020,
                  /mechanism/us/ear-ecn-3b001-restriction, ...],
       edges: [/gov/us/doc-bis → ISSUED → /decision/...,
               /decision/... → IMPLEMENTS → /mechanism/...,
               /mechanism/... → RESTRICTS → /org/cn/smic])

  Materializer returns:
    created: [/gov/us/doc-bis, /decision/us/entity-list-smic-2020, ...]
    failed: [{item: "/mechanism/us/ear-ecn-3b001-restriction",
              reason: "ambiguous_geo — mechanism has no geographic scope qualifier",
              status: "candidate_only"}]

  Orchestrator INTERPRET:
    Most of B2 materialized. One mechanism node failed on geo scope.
    This is a deterministic signal, not a research gap — the mechanism
    applies to US-origin items globally, need to add scope qualifier.

    → policy_precheck(mechanism node with scope="global/us-origin-items")
    → validate_and_create_fragment(... with geo_scope="global")

  Materializer retry succeeds.
  B2 status: materialized. Compress B2 to summary in active context.

  Now start B3 (ownership) since we have capacity.

  → researcher(branch=B3, question="Who are SMIC's largest shareholders?
     Focus on Chinese state-affiliated entities: National IC Fund (Big Fund),
     Datang Telecom. Need: stake percentages, board representation,
     government appointments.")


TURN 4 — B3 results + unlock B4

  Researcher B3 returns:
    findings:
      - National IC Industry Investment Fund (Big Fund): ~15% stake
      - Datang Telecom (state-owned): ~7% stake
      - SMIC co-CEO Zhao Haijun and Liang Mong Song — key figures
      - Board includes government-appointed directors
      - Total state-affiliated ownership estimated 30-40% through various vehicles
    confidence: medium (exact percentages vary by source and date)

  Orchestrator INTERPRET:
    H5 status: supported but not fully quantified
    B3 has evidence but thin on exact control mechanisms — the ownership
    chain exists, but the orchestrator recognizes (per Structural Reasoning
    Categories) that state control may be exercised through board appointments
    and policy directives, not just equity.

    This is not blocking for the main investigation. Downgrade B3 to
    candidate_only for the exact control structure; materialize the known
    ownership edges with explicit "approximate" qualifiers.

  → materializer(branch=B3,
     entities: [/org/cn/national-ic-fund],
     edges: [/org/cn/national-ic-fund → INVESTS_IN → /org/cn/smic,
             qualifiers: {stake_pct: "~15%", share_type: "equity",
                          qualifier_confidence: "approximate"}])

  B3 status: partially materialized with candidates for control structure.

  Now B4 is unblocked (B1 equipment substrate exists, B2 regulatory context
  is materialized).

  FRAME (plan version 2, re-plan):
    B4 activated: path construction for SMIC advanced node capability
    New hypothesis H6: "SMIC's 7nm capability relies on ASML DUV
    multi-patterning + Applied Materials CVD + Lam etch, with all
    post-2022 equipment flows requiring BIS license approval"
    B4 dependency on B1: satisfied
    B4 informed by B2: regulatory constraints shape which paths are viable

  → scout.path(target="SMIC 7nm semiconductor manufacturing",
     known_nodes=[/org/cn/smic, /org/nl/asml, /org/us/applied-materials, ...],
     product_axis=[/product/equipment/lithography/duv, ...],
     constraint="post-Oct-2022 equipment requires BIS license")


TURN 5 — Path synthesis + critic + landing

  scout.path returns:
    candidate_path:
      /org/nl/asml → DUV_TWINSCAN → /org/cn/smic (pre-2022, uncertain post)
      /org/us/applied-materials → CVD_tools → /org/cn/smic (license required)
      /org/us/lam-research → etch_tools → /org/cn/smic (license required)
    continuity_status: "broken" — cannot confirm post-2022 equipment deliveries
    target_binding: "bound" — specific to SMIC 7nm
    gaps: ["Post-2022 equipment delivery status unknown for all US suppliers",
           "No evidence of alternative Chinese equipment suppliers filling gaps"]

  Orchestrator:
    Path is structurally correct but has a continuity break at the
    Oct 2022 boundary. This is not a research failure — it reflects
    real-world uncertainty. Two options:
    a) Research whether any licenses were granted post-2022
    b) Research whether Chinese domestic equipment suppliers are filling gaps

  → researcher(branch=B4, question="Have any BIS licenses been granted for
     semiconductor equipment exports to SMIC after October 2022? Any known
     denials? Check BIS press releases, Congressional testimony, Reuters/Bloomberg.")

  → researcher(branch=B4, question="Chinese domestic semiconductor equipment
     manufacturers: Naura Technology, AMEC, Shanghai Micro Electronics.
     Can any of them supply advanced lithography, deposition, or etch tools
     comparable to ASML/Applied/Lam for 7nm production?")

  Researchers return:
    - Some licenses granted for "legacy" equipment, denied for advanced
    - Naura and AMEC progressing but not at parity for advanced nodes
    - Shanghai Micro Electronics (SMEE) lithography: 90nm capability, nowhere
      near DUV immersion for 7nm

  Orchestrator:
    The path has a real structural break that cannot be researched away.
    This is the correct answer: SMIC's advanced capability path is
    partially severed by export controls, with no confirmed domestic
    substitute at the required performance level.

  → critic(review: full investigation state — B1+B2+B3+B4)

  Critic:
    grade: "pass"
    strengths: ["Equipment substrate well-evidenced", "Regulatory structure
                 complete with gov/decision/mechanism layers",
                 "Path continuity break is real and correctly identified"]
    gaps: ["Exact post-2022 DUV delivery volumes unknown"]

  Orchestrator accepts. Materialize the path with the continuity break
  explicitly represented.

  → materializer(paths, links, with continuity_status="broken" at Oct 2022
     boundary, gap reason="US export control restriction")

  TERMINATION:
    B1: materialized (equipment entities and edges)
    B2: materialized (regulatory structure)
    B3: partially materialized, control structure as candidate
    B4: materialized with explicit continuity break

    Final outcome: "partially_materialized_with_candidates"
    Unresolved: exact state control mechanism, exact post-2022 delivery volumes

    Plan version: 2
    Total turns: 5
    Branches: 4 (3 materialized, 1 partially materialized)
```

This example demonstrates:

- **Multi-structural reasoning** — supply chain, regulatory impact, and ownership
  investigated as interrelated structural forms, not as separate pipelines.
- **Contradiction handling** — EUV delivery claim researched, resolved with
  reframed query and source-quality assessment.
- **Tool failure as signal** — geo scope rejection from materializer led to
  targeted fix, not broad re-research.
- **Progressive materialization** — B2 materialized while B1 was still being
  refined, freeing context and providing anchor data for B4.
- **Dependency management** — B4 waited for B1 to resolve before activating.
- **Knowing when to stop** — the path continuity break is a real-world fact,
  not a failure of the investigation. The orchestrator recognized this and
  materialized it as a structural property of the path.
- **Downgrade rather than invent** — B3 ownership control structure was
  downgraded to candidate rather than force-materialized with weak evidence.

---

## Runtime Modes and Rollout Strategy

The agentic environment should be built as a parallel runtime, not as a mutation
of the current Scout pipeline.

### Runtime Modes

- `classic_scout` — current Go Scout pipeline
- `agentic_scout` — new AgentMesh-hosted scouting runtime on Vertex
- `shadow_compare` — run both, materialize only from the classic path, compare outputs
- `agentic_primary_with_switchback` — agentic path writes, classic path remains available as fallback

### Why Parallel Rollout

Trying to inject the new environment directly into `ScoutEngine` would create a
hybrid system with duplicated reasoning and poor observability. A separate runtime
keeps the concerns clear:

- classic Scout remains stable
- agentic Scout gets its own sessions, traces, and evaluation
- routing decisions become explicit and reversible
- failure analysis stays attributable to one system at a time
- AgentMesh remains the single control plane for both simulation mesh and scouting mesh

### Minimum Routing Layer

The Go backend only needs a thin routing decision:

- which runtime should handle this request?
- should this request also run in shadow mode?
- should writes be allowed from the selected runtime?
- if the agentic path fails, is switchback to classic allowed automatically?
- which AgentMesh capability should be invoked first (`scout.plan` in the normal case)?

### Switchback Discipline

Switchback should be operationally cheap. The rollout is healthy only if the team
can disable `agentic_scout` and return traffic to `classic_scout` without code
rewrites or graph repair.

That implies:

- same input contract at the routing boundary
- same deterministic knowledge services behind both runtimes
- explicit write provenance showing which runtime produced each change

### Evaluation During Rollout

During `shadow_compare`, compare at least:

- entity and edge coverage
- correction quality against existing graph facts
- rejection rate from deterministic services
- proportion of claims left as candidates
- total cost
- latency
- critic pass/fail rate
- number of refinement rounds per completed investigation

The goal is not just "did the agentic path answer something useful?" but "did it
produce a more trustworthy and more economical investigation outcome than the
classic pipeline?"

For supply-chain cases, comparison should also include:

- target-scoped path quality
- number of continuous vs broken candidate paths
- proportion of paths that are target-bound vs generic
- supply link and leg grounding quality
- path reuse vs path over-generation
- number of unresolved continuity gaps per completed investigation

---

## What Dies

| Current component | Why it dies |
|-------------------|------------ |
| `execution_plan_contract.go` | ADK guardrails (`max_iterations`, token budget) |
| `domain_contract_runner.go` | Orchestrator reasoning replaces hardcoded DAG |
| `pipeline_orchestration.go` | Agent Engine sessions/traces/recovery |
| `research_pipeline.go` + all strategy files | Single universal researcher + orchestrator context |
| `critic_pipeline.go` + all validator files | Single critic + deterministic invariant tools |
| `intent_resolver_agent.go` | Orchestrator understands intent natively |
| `domain_mode_filter.go` | Orchestrator reasons about relevance |
| `strategic_phase_policy.go` | Orchestrator decides strategy |
| `scope_policy.go` | Orchestrator manages scope |
| Most of `engine.go` | The orchestrator IS the engine |

## What Survives (as tools)

| Current component | Becomes |
|-------------------|---------|
| `canonicalizer.go` (resolution chain) | `resolve_canonical_path` tool |
| `taxonomy/` (path validation, slugify) | `validate_canonical_path` tool |
| `supply_chain_validator.go` (invariants) | Pre-commit check inside materializer |
| `naming/` (entity name normalization) | Helper inside `resolve_canonical_path` |
| Product synonym/transform dictionaries | `lookup_product_synonym/transform` tools |
| `fragmentsRepo`, `edgesRepo` | Backend for `lookup_fragment`, `create_fragment` tools |
| Vertex AI Search client | Backend for `search_knowledge_base` tool |
| `edgesemantics/` (allowed edge types) | Validation inside `validate_and_create_edge` |
| pathbuilder / materialized path semantics | Tool-backed path synthesis and validation layer |

## What Transforms

| Current | Future |
|---------|--------|
| Firestore-based scouting jobs | Agent Engine Sessions |
| Standalone Scout execution path | AgentMesh-hosted scouting capability family on Vertex |
| Pipeline-level mode switching inside Scout code | External routing layer between runtimes |
| Hand-rolled trace logging | Cloud Trace (OpenTelemetry) via Agent Engine |
| `realtime.EventType` publishing | Agent Engine events + streaming |
| Budget tracking in job state | ADK token budget + Agent Engine monitoring |
| Go backend calling LLMs | Python ADK agents on Agent Engine Runtime |
| Evaluation via manual testing | Gen AI Evaluation service + ADK eval |

---

## Runtime Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                         Go Backend                            │
│  (HTTP API, auth, Horizon services, lens runtime, routing)   │
│                                                              │
│  Submits governed task to AgentMesh:                         │
│    capability=scout.plan                                     │
│    target="Tesla battery supply chain"                       │
│    tenant_id="..." lens_id="..."                             │
└──────────────────────┬───────────────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────────────┐
│                          AgentMesh                           │
│  controlplane • policy • runtime • tasks • trace • review   │
│                                                              │
│  scout.plan        → orchestrator                            │
│  scout.research    → researcher                              │
│  scout.critic      → critic                                  │
│  scout.materialize → materializer                            │
│  scout.review      → mesh reviewer                           │
│  scout.aggregate   → final synthesis artifact                │
└──────────────────────┬───────────────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────────────┐
│              Vertex AI Agent Engine / Reasoning Engine        │
│                                                              │
│  orchestrator (Gemini 2.5 Pro)                               │
│    ├─ AgentTool: researcher                                  │
│    ├─ AgentTool: critic                                      │
│    ├─ AgentTool: materializer                                │
│    ├─ search_knowledge_base                                  │
│    ├─ lookup_fragment                                        │
│    ├─ get_graph_neighborhood                                 │
│    ├─ resolve_canonical_path                                 │
│    ├─ policy_precheck                                        │
│    └─ geo_coord_mapping_status                               │
│                                                              │
│  Sessions ─── Memory Bank ─── Traces ─── Evaluation          │
└──────────────────────┬───────────────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────────────┐
│       Horizon Graph + Deterministic Knowledge Services        │
│  Firestore fragments/edges • Vertex Search • canonical plane  │
│  policy enforcement • geo mapping • transform/synonym dicts   │
└──────────────────────────────────────────────────────────────┘
```

---

## Migration Path

### Phase 0: Tool layer (Go → Python bridge)
Wrap existing Go repos (fragments, edges, Vertex Search, canonical resolver)
as Cloud Functions / Cloud Run services callable as ADK tools. Include policy
precheck and geo coord mapping / revalidation services in this layer from day one.

In parallel, register a new scouting capability family in `AgentMesh` rather than
touching the classic Scout runtime.

### Phase 1: Single-entity scouting
Build the 4-agent environment for single entity investigation.
Run in parallel with existing ScoutEngine. Compare quality + cost. No writes from
the agentic path yet; use shadow evaluation only.

### Phase 2: Signal-triggered scouting
Connect to the existing signal landing pipeline: when a signal lands,
route to either classic or agentic runtime through a routing layer. Enable
switchback and selective shadow mode.

Initial execution should enter `AgentMesh` via `scout.plan`, not by calling Vertex
directly from Horizon handlers.

### Phase 3: Directed exploration + lens scope
Move directed exploration and lens scope to orchestrator-driven multi-entity
investigations. Allow agentic writes for selected tenants or lens types once
shadow comparisons are consistently favorable.

### Phase 4: Decommission ScoutEngine
Only after quality, correction discipline, rejection rates, and operational
switchback confidence are all proven should the Go-based scout pipeline be retired.

---

## Cost Model (Vertex AI Studio credits)

Estimated per single-entity investigation:

| Component | Tokens (approx) | Cost (approx) |
|-----------|-----------------|----------------|
| Orchestrator (2.5 Pro) planning | ~2K in + ~1K out | ~$0.02 |
| Orchestrator (2.5 Pro) re-planning x2 | ~4K in + ~2K out | ~$0.04 |
| Researcher x3 (2.5 Flash + grounding) | ~6K in + ~6K out | ~$0.01 |
| Critic (2.5 Pro) | ~4K in + ~1K out | ~$0.02 |
| Materializer (2.5 Flash) | ~3K in + ~2K out | ~$0.005 |
| Google Search grounding | ~3 queries | ~$0.10 |
| **Total per entity** | | **~$0.20** |

With $1000 credits: **~5000 entity investigations**, or roughly **50-100 full
lens scopes** (each investigating 50-100 entities in a supply chain / ownership
/ regulatory graph).

This is significantly more cost-effective than the current approach where
each entity goes through intent resolution + domain contract + research
strategies + critic pipeline + pipeline orchestration, each with separate
LLM calls that often duplicate reasoning context.

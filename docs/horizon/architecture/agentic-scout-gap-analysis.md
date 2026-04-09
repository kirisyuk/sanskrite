# Agentic Scout Implementation Gap Analysis

Source requirements: `docs/horizon/architecture/agentic-scouting-environment.md`

This document compares the current uncommitted implementation against the target architecture defined in `agentic-scouting-environment.md`, with special focus on planning, reasoning, re-planning, execution, shared state, path construction, and materialization.

## Scope Reviewed

Primary implementation files reviewed:

- `backend/internal/agentmesh/scout/processor.go`
- `backend/internal/agentmesh/scout/types.go`
- `backend/internal/agentmesh/scout/helpers.go`
- `backend/internal/agentmesh/runtime/service.go`
- `backend/internal/agentmesh/vertex/service.go`
- `backend/internal/horizon/scoutbridge/service.go`
- `backend/internal/horizon/policy/scout_runtime_flags.go`
- `backend/cmd/agentmesh/main.go`
- `backend/cmd/server/main.go`
- `backend/internal/http/horizon/fragments.go`
- `backend/internal/horizon/pathbuilder/supply_chain_query_engine.go`

## Executive Conclusion

The current delta does **not** implement the agentic reasoning runtime described in `agentic-scouting-environment.md`.

What has been implemented is:

- a new `AgentMesh`-hosted scouting execution path
- capability names aligned with the target design (`scout.plan`, `scout.research`, `scout.critic`, `scout.path`, `scout.materialize`, `scout.aggregate`, `scout.review`)
- Vertex-backed JSON generation for some stages
- rollout modes and bridge wiring (`classic_scout`, `agentic_scout`, `shadow_compare`, `agentic_primary_with_switchback`)
- deterministic pathbuilder integration

What has **not** been implemented is the core of the architecture:

- a true orchestrator reasoning loop
- evolving structured investigation state
- branch and hypothesis lifecycle
- real re-planning
- bounded multi-round research
- critic-driven restructuring of the investigation
- materialization/correction as a true reasoning role
- shared Vertex-native session state for the investigation

In practice, the implementation is still a fixed multi-stage pipeline in Go with LLM-assisted substeps.

---

## 1. Reasoning Harness Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `Reasoning Harness`
- `Practical Main Loop`
- `Framing: How the Orchestrator Builds an Investigation Plan`
- `Action Selection: How the Orchestrator Chooses What to Do Next`
- `Convergence and Termination Mechanics`

The document requires:

- a stateful orchestrator loop
- explicit framing
- next-action choice driven by state
- interpret/update state after each step
- bounded iteration
- loop-or-stop behavior
- no hardcoded stage graph

### Current implementation

The implementation hardcodes a single orchestration chain:

`buildPlan -> run all research branches -> critic -> optional path -> materialize -> aggregate -> review`

There is no true loop, no repeated framing, and no runtime-level next-action choice.

```text
processPlan()
  -> buildPlanningContext()
  -> buildPlan()
  -> for each branch: scout.research
  -> scout.critic
  -> maybe scout.path
  -> scout.materialize
  -> scout.aggregate
  -> scout.review
```

### Gap

- No orchestrator loop
- No `Frame -> Choose -> Execute -> Interpret -> Bound -> Loop`
- No adaptive step selection
- No re-entry into planning
- No branch-level stop/continue decisions

### Why this matters

This is the single largest architectural mismatch. The target design is a reasoning runtime. The current code is a procedural pipeline with LLM-assisted stages.

---

## 2. Plan-as-Artifact Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `Framing: How the Orchestrator Builds an Investigation Plan`
- `InvestigationPlan`
- `BranchDependency`
- `OpenQuestion`
- `Re-planning`

The document expects the plan to be:

- a first-class runtime artifact
- mutable over time
- dependency-aware
- tied to materialization strategy
- versioned or at least explicitly re-shaped during investigation

### Current implementation

`InvestigationPlan` exists but is only a thin DTO:

- goal
- focus products / geos / actors
- wantsPathMaterialize
- maxResearchRounds
- rationale
- branches

There is no:

- plan version
- branch dependency graph
- open question structure
- re-plan trigger
- branch history
- materialization strategy

### Gap

The current `InvestigationPlan` is not a living investigation artifact. It is only a single initial dispatch bundle.

### Why this matters

Without a mutable plan, there is no real re-planning. Without dependencies, the system cannot reason about what blocks what. Without open questions as structured objects, it cannot evolve intelligently.

---

## 3. Branch and Hypothesis Lifecycle Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `Hypothesis Lifecycle`
- `Session State Schema`
- `InvestigationBranch`
- `Epistemic Control`

The document requires:

- structured hypotheses
- test criteria
- status transitions
- support / contradiction / rejection states
- branch progress derived from hypothesis progress

### Current implementation

There is no hypothesis model at all.

`InvestigationBranch` currently contains only:

- `ID`
- `Question`
- `Role`
- hint arrays
- `RequiresPath`

There is no:

- hypothesis object
- branch status
- expected information gain
- refinement count
- contradiction state
- materialization readiness
- research-round tracking

### Gap

The runtime has no internal representation of what is being tested, only a set of research prompts.

### Why this matters

Without hypotheses, the system cannot:

- reason about claim quality
- know what has been confirmed vs contradicted
- meaningfully downgrade weak branches
- drive critic-based reframing

---

## 4. Shared State / Session Memory Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `State is shared, not passed`
- `Turn Structure and Context Management`
- `Vertex-Native Execution Requirements`
- `Session State Schema`

The document expects:

- a shared investigation state
- ideally Vertex-native session/memory behavior
- stateful turns
- compacted active context
- explicit state updates between steps

### Current implementation

The general `AgentMesh` runtime supports Vertex sessions and memory banks.

However, the scout processor bypasses that path for model-driven scouting steps:

- it constructs a pseudo-task
- directly calls `rt.Vertex().ExecuteTask(...)`
- does not create or use a shared `SessionRef`
- does not append investigation events into a shared session

Instead, state is passed around via task input maps and task outputs.

### Gap

- No shared investigation session
- No real multi-turn shared memory for the scout runtime
- No context compaction discipline
- No structured epistemic state updates across turns

### Why this matters

This breaks one of the most important architectural ideas in the document: the investigation should evolve in shared state, not through ad hoc passing of intermediate payloads.

---

## 5. Delegation Protocol Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `Delegation Protocol`
- `When to use direct tools vs subagents`
- `Action Selection`

The document expects:

- structured orchestrator-to-researcher handoff
- structured researcher return
- critic as governed quality gate
- materializer as distinct role
- orchestrator integration of results back into state

### Current implementation

There are child mesh tasks and distinct capabilities.

But in practice:

- child tasks are created and executed synchronously
- the parent waits for each one immediately
- there is no broader orchestration protocol
- there is no structured investigation-state integration phase beyond decoding and passing outputs downstream

### Gap

Delegation exists mechanically, but not architecturally. It is task fan-out, not multi-agent reasoning delegation.

### Why this matters

The document is not asking for “some child tasks.” It is asking for role-based delegated reasoning inside a controlled investigation loop.

---

## 6. Critic Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `Critic`
- `Hypothesis Lifecycle`
- `Convergence and Termination Mechanics`
- `Progressive Materialization and Backtracking`

The document expects critic feedback to:

- judge completeness and contradictions
- influence re-planning
- open follow-up questions
- block premature materialization
- shape branch evolution

### Current implementation

`scout.critic` exists and returns a structured object:

- accepted findings
- risky claims
- open questions
- materializePath
- pathProducts / focusActors / focusGeos

But this feedback does not drive a genuine re-plan. It mainly influences whether path materialization should run.

### Gap

- Critic is a reviewer stage, not a reasoning-control mechanism
- Critic cannot re-open planning
- Critic does not cause branch mutation or bounded retry behavior

### Why this matters

The critic is supposed to be one of the main epistemic controls of the system. Right now it is informative, but not structurally controlling.

---

## 7. Path Construction Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `No hardcoded bypass for path logic`
- `Supply-Chain Path Materialization`
- `Path construction stays agentic`
- `Path Synthesis / Path Landing`

The document requires:

- path construction to stay inside the same reasoning loop
- path to be invoked when warranted by the investigation
- path to be refined through research/critique/replanning
- explicit treatment of path continuity / target binding / logistics grounding

### Current implementation

`scout.path` exists, but it is mostly deterministic:

- gather product refs
- call `TraceByProductFast`
- maybe materialize paths
- maybe build assembly graph

This is useful infrastructure, but it is not an agentic path reasoning loop.

### Gap

- Path is a downstream deterministic stage, not a first-class reasoning branch
- No path hypotheses
- No path continuity state machine
- No target-binding-aware re-plan loop

### Why this matters

The document explicitly rejected a hardcoded path side-pipeline. The current design is closer to that rejected shape than to the intended one.

---

## 8. Materializer / Corrections Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `Materializer`
- `Progressive Materialization and Backtracking`
- `Existing Graph Semantics`
- `correction_proposals`

The document expects:

- create/update/correction behavior
- progressive materialization
- correction semantics such as `replace_existing`, `deprecate_existing`, `contradict_existing`
- backtracking when new evidence changes prior conclusions

### Current implementation

`scout.materialize` does not materialize graph facts in the sense described by the document.

It mostly:

- inspects path output
- summarizes persisted artifact IDs
- returns a summary

It does not:

- reason about writes
- perform create/update/correction flows
- implement progressive branch materialization
- emit correction semantics

### Gap

The current materializer is bookkeeping around path artifacts, not a genuine materialization role.

### Why this matters

One of the document's most important goals is graph quality improvement through intelligent creation, correction, and selective landing. That is not present here.

---

## 9. Bounded Iteration and Convergence Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `Convergence and Termination Mechanics`
- `Bounded Iteration and Termination`

The document expects:

- bounded retries
- repeated-gap detection
- stall detection
- downgrade to candidate or needs-review
- stop when marginal information gain falls

### Current implementation

`MaxResearchRounds` exists in the plan model but is not used to drive an iterative research loop.

There is no:

- repeated-gap counter
- branch downgrade mechanism
- stall detection
- information-gain logic
- reframe-once rule

### Gap

Termination is determined by reaching the end of the pipeline, not by epistemic convergence.

### Why this matters

Without bounded iteration semantics, the system cannot claim to be doing controlled agentic investigation. It is just finishing a workflow.

---

## 10. Vertex-Native Central Runtime Gap

### Source requirement

From `agentic-scouting-environment.md`:

- `Vertex-Native Execution Requirements`
- `Primary Rule`
- `AgentMesh Boundary`

The document expects:

- central reasoning runtime inside Vertex
- AgentMesh as routing/control plane
- Vertex sessions/memory/evaluation around the reasoning system itself

### Current implementation

Vertex is used as a generation backend for planning/research/critic/aggregate/review prompts.

But the central orchestration logic lives in Go inside `backend/internal/agentmesh/scout/processor.go`.

### Gap

This is “Go orchestration + Vertex calls,” not “Vertex-hosted central reasoning runtime governed by AgentMesh.”

### Why this matters

This is an architectural, not cosmetic, mismatch. The target design intentionally put the reasoning harness inside the Vertex runtime layer.

---

## 11. Rollout / Runtime Modes: Mostly Aligned

### Source requirement

From `agentic-scouting-environment.md`:

- `Runtime Modes and Rollout Strategy`

### Current implementation

This part is comparatively well aligned:

- `classic_scout`
- `agentic_scout`
- `shadow_compare`
- `agentic_primary_with_switchback`

are implemented through:

- `backend/internal/horizon/scoutbridge/service.go`
- `backend/internal/horizon/policy/scout_runtime_flags.go`
- server wiring in `backend/cmd/server/main.go`

### Assessment

The rollout and routing boundary are in place.

### Important note

This does **not** mean the target runtime is implemented. It only means the switchover scaffolding exists.

---

## 12. Testing Gap

### Source requirement

Implied across the document:

- the behavior should be observable, bounded, and verifiable
- stateful agentic behavior should be testable

### Current implementation

There are only thin tests around:

- scout runtime flags
- one generic runtime helper

There are no meaningful tests for:

- orchestration loop behavior
- re-planning
- hypothesis lifecycle
- bounded iteration
- critic-driven branch changes
- progressive materialization
- backtracking/corrections
- shared session behavior

### Gap

The current test surface does not validate the core claims of the implementation.

---

## Summary of What Is Actually Present

The current delta successfully introduces:

- a new `AgentMesh` scout processor
- capability family naming aligned with the document
- bridge-based runtime switching
- Vertex-backed structured generation for several stages
- deterministic pathbuilder integration

These are useful building blocks.

## Summary of What Is Missing

The current delta does **not** yet implement:

- a true stateful orchestrator
- evolving plan/hypothesis/branch state
- re-planning
- bounded multi-round research
- critic-driven investigation control
- true materialization/correction role
- shared Vertex session-based investigation memory

## Final Judgment

This implementation should be treated as:

**an infrastructure scaffold for a future agentic scout runtime**

not as:

**the agentic scouting environment described in `agentic-scouting-environment.md`**

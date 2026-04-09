# Prompt for Neighbor Agent: Implement Real Agentic Scout Runtime

You need to bring the current implementation into alignment with the target architecture in `docs/horizon/architecture/agentic-scouting-environment.md`.

This is **not** a request to cosmetically improve the current agentic scout. It is a request to replace the current fixed multi-stage DAG with a real stateful reasoning runtime, as far as is practically possible in the current codebase.

Before making any changes, read:

- `docs/horizon/architecture/agentic-scouting-environment.md`
- `docs/horizon/architecture/agentic-scout-gap-analysis.md`

Then review the current implementation in:

- `backend/internal/agentmesh/scout/processor.go`
- `backend/internal/agentmesh/scout/types.go`
- `backend/internal/agentmesh/scout/helpers.go`
- `backend/internal/agentmesh/runtime/service.go`
- `backend/internal/agentmesh/vertex/service.go`
- `backend/internal/horizon/scoutbridge/service.go`
- `backend/internal/horizon/policy/scout_runtime_flags.go`
- `backend/cmd/agentmesh/main.go`
- `backend/cmd/server/main.go`

## Core Problem

The current code is still a fixed procedural pipeline in Go:

`plan -> research branches -> critic -> optional path -> materialize -> aggregate -> review`

This does **not** satisfy the architecture document.

The document requires:

- a reasoning harness
- a stateful orchestrator loop
- framing, action choice, execution, interpretation, bounding, and looping
- evolving shared investigation state
- branch and hypothesis lifecycle
- re-planning
- bounded iteration
- path construction as part of the reasoning loop
- real materialization/correction behavior

## Your Objective

Implement a true agentic scouting runtime, not a prettier DAG.

The goal is to make the runtime behave like a reasoning investigation system, not like a fixed-stage pipeline that happens to call LLMs.

## Required End-State Properties

Your implementation must move the system materially toward these properties:

1. **Stateful orchestrator loop**

   The runtime must behave like:

   - frame or update plan
   - choose next action
   - execute
   - interpret results
   - apply bounds
   - loop or stop

   Do **not** leave orchestration as one hardcoded route through fixed stages.

2. **Structured investigation state**

   Introduce and actually use explicit state objects such as:

   - `InvestigationPlan`
   - `InvestigationBranch`
   - `Hypothesis`
   - open questions
   - branch dependencies
   - turn counter
   - re-plan trigger
   - branch history
   - bounded-iteration counters

   These must not be decorative DTOs. Runtime behavior must depend on them.

3. **Real re-planning**

   Critic results, tool failures, contradictions, and new evidence must be able to:

   - add branches
   - kill branches
   - split branches
   - reprioritize branches
   - trigger query reframing
   - change materialization strategy

   Re-planning must be a real runtime event, not just a new field in output.

4. **Hypothesis lifecycle**

   The runtime must explicitly model and evolve hypotheses:

   - proposed
   - under investigation
   - supported / weakly supported
   - contradicted
   - confirmed materializable
   - rejected

   Branch progress should be traceable through hypothesis progress.

5. **Bounded iteration**

   `maxResearchRounds` and related bounded-execution ideas must become real control semantics.

   The runtime must be able to:

   - detect repeated gaps
   - stop retrying weak branches
   - downgrade to candidate / needs-review
   - terminate on stall

6. **Critic as epistemic control**

   Critic must not be just a review stage.

   It must be able to influence the live investigation by:

   - identifying contradictions
   - forcing re-plan
   - withholding materialization
   - generating follow-up investigation questions

7. **Path stays inside the reasoning loop**

   `scout.path` must not remain just a late deterministic phase.

   It should become an action the orchestrator chooses when the investigation state justifies it.

   The runtime should support:

   - path attempt
   - continuity break detection
   - targeted research to close path gaps
   - partial landing when appropriate

8. **Materializer becomes real**

   `scout.materialize` must become more than artifact bookkeeping.

   It should support:

   - create/update behavior
   - progressive materialization when branches become ready
   - correction proposals
   - explicit reporting of what was created, updated, skipped, downgraded, or rejected

9. **Shared state discipline**

   Do not rely only on passing `map[string]any` forward through fixed tasks.

   Prefer a real shared investigation state. If feasible, make stronger use of the existing Vertex session/memory path in `runtime/service.go`.

   If full Vertex session integration is not practical in one pass, then at minimum implement an explicit durable investigation state model and a true stateful loop.

10. **Do not break runtime rollout modes**

   Preserve:

   - `classic_scout`
   - `agentic_scout`
   - `shadow_compare`
   - `agentic_primary_with_switchback`

   The routing boundary is already useful and should remain intact.

## What You Must Not Do

- Do not replace one hardcoded stage graph with another slightly more complicated hardcoded stage graph.
- Do not add `Hypothesis` or `InvestigationPlan` objects that are never actually used by orchestration logic.
- Do not claim “re-planning” when the code still only plans once.
- Do not leave `maxResearchRounds` or similar controls as dead configuration fields.
- Do not keep `scout.path` as a one-way late-stage deterministic appendage while pretending it is part of the reasoning loop.
- Do not keep `scout.materialize` as summary/bookkeeping only.
- Do not regress classic scout or rollout controls.

## Why This Matters

This implementation work is important because without it the architecture loses its purpose.

The whole point of `agentic-scouting-environment.md` is not “use more LLM calls.” It is:

- improve graph quality through evidence-sensitive reasoning
- replace rigid algorithmic expansion with selective investigation
- support correction, contradiction handling, and selective landing
- produce better supply-chain paths, ownership structures, and policy-impact graphs
- reduce wasted work by stopping weak branches and focusing high-value unknowns

If orchestration stays a fixed DAG, then the system still cannot truly:

- reason about what is missing
- distinguish strong and weak branches
- handle contradictions correctly
- reframe queries when a branch is unstable
- stop intelligently
- progressively improve the graph

In that case it remains a pipeline, not an agentic investigation environment.

## How To Approach The Work

Recommended execution plan:

1. Do your own gap analysis first against `agentic-scouting-environment.md`.
2. Design the minimal viable state model needed to support a real loop.
3. Replace fixed orchestration with a state-driven loop.
4. Add branch/hypothesis lifecycle.
5. Make critic capable of changing investigation flow.
6. Integrate path as a loop action rather than as a forced late stage.
7. Upgrade materialization to support progressive landing and correction semantics.
8. Add tests that prove runtime behavior, not just helper correctness.

If helpful, use multiple Cursor subagents in parallel for:

- runtime/state-model work
- Vertex session integration
- path/materialization integration
- tests/verification

But the final result must be one coherent runtime, not disconnected pieces.

## How To Verify Quality

Do **not** stop at “it compiles.”

You must verify the result against runtime behavior.

### Architectural verification

Confirm that:

- orchestration is no longer one fixed route
- the orchestrator can choose different next actions based on state
- the plan can change during execution
- critic can alter the investigation path
- bounded iteration is real

### State-model verification

Confirm that:

- branches have statuses and evolve
- hypotheses exist and evolve
- re-plan trigger and turn counters change over time
- open questions are tracked structurally
- bounded-execution counters affect decisions

### Tests to add

At minimum, add tests for:

1. initial framing produces structured plan state
2. critic feedback triggers re-plan
3. repeated gap detection prevents infinite retry
4. weak branches downgrade to candidate / needs-review
5. path action only runs when the state warrants it
6. progressive materialization works
7. correction/backtracking can occur when new evidence changes prior conclusions

### Manual scenario checks

Run at least these scenario types:

1. **Supply-chain case with continuity break**
   Expected: path attempt -> gap detection -> targeted follow-up -> partial landing or unresolved outcome

2. **Ownership/control case with thin evidence**
   Expected: weak claims are not force-landed as strong facts

3. **Policy impact case with missing mechanism layer**
   Expected: structural incompleteness is recognized and handled through re-plan or downgrade

### Acceptance bar

Only consider the task complete if you can honestly say:

- orchestration is no longer just a fixed DAG
- investigation state really evolves
- re-planning really exists
- bounded iteration really exists
- path/materialization really participate in the reasoning flow
- tests actually prove this

## Final Report Expectations

When you are done, report:

- which architectural gaps were closed
- which ones remain
- what runtime behaviors are now truly supported
- what tests were added
- what was verified manually
- what still needs follow-up

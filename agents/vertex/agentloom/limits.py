"""Central caps for AgentLoom (plan: single table, env-tunable for MVP)."""

from __future__ import annotations

import os

# Graph delta per agent result (was hard-coded 18)
MAX_GRAPH_DELTA_OBSERVATIONS = int(os.getenv("AGENTLOOM_MAX_GRAPH_DELTA", "64"))
MAX_ANSWERED_QUESTIONS = int(os.getenv("AGENTLOOM_MAX_ANSWERED_QUESTIONS", "12"))
MAX_GAPS = int(os.getenv("AGENTLOOM_MAX_GAPS", "16"))
MAX_NEXT_QUESTIONS = int(os.getenv("AGENTLOOM_MAX_NEXT_QUESTIONS", "10"))
MAX_HANDOFF_RECOMMENDATIONS = int(os.getenv("AGENTLOOM_MAX_HANDOFFS", "6"))

# Working graph safety (plan I3)
MAX_WORKING_GRAPH_OBSERVATIONS = int(os.getenv("AGENTLOOM_MAX_WORKING_GRAPH", "500"))

# Stagnation / repeated gap (plan I2)
# Count of consecutive supervisor turns with no material graph progress (_material_progress in agent.py).
# When this threshold is met and the supervisor still picks a specialized investigator, delegate policy either
# recycles another supply investigation (if there is still no business-complete chain) or escalates to critic.
STAGNATION_IDLE_INVESTIGATOR_REDIRECT_CYCLES = int(
    os.getenv("AGENTLOOM_STAGNATION_IDLE_INVESTIGATOR_REDIRECT_CYCLES", "3")
)
REPEATED_GAP_THRESHOLD = int(os.getenv("AGENTLOOM_REPEATED_GAP_THRESHOLD", "2"))

# Supervisor main loop depth (shared with delegation policy).
MAX_SUPERVISOR_CYCLES = int(os.getenv("AGENTLOOM_MAX_SUPERVISOR_CYCLES", "10"))

# Budget pressure vs MAX_SUPERVISOR_CYCLES (fractions of max remaining)
BUDGET_SOFT_FRACTION = float(os.getenv("AGENTLOOM_BUDGET_SOFT_FRACTION", "0.35"))

# When the supervisor for-loop exits without compose/stop: stop (default) or one-shot deterministic compose.
_LIMIT_RAW = (os.getenv("AGENTLOOM_LIMIT_EXHAUSTED_STRATEGY", "stop") or "stop").strip().lower()
LIMIT_EXHAUSTED_STRATEGY: str = _LIMIT_RAW if _LIMIT_RAW in ("stop", "compose_with_caveats") else "stop"

# Observation merge: when confidences within epsilon, prefer stronger supportType (plan B1 tie-break).
MERGE_CONFIDENCE_EPSILON = float(os.getenv("AGENTLOOM_MERGE_CONFIDENCE_EPSILON", "0.03"))

# Supervisor snapshot: larger caps + README warns these are still lossy vs full state.
SUPERVISOR_SNAPSHOT_OPEN_GAPS_MAX = int(os.getenv("AGENTLOOM_SUPERVISOR_SNAPSHOT_OPEN_GAPS", "20"))
SUPERVISOR_SNAPSHOT_LAST_RESULT_SUMMARY_CHARS = int(
    os.getenv("AGENTLOOM_SUPERVISOR_SNAPSHOT_LAST_SUMMARY_CHARS", "1200")
)
SUPERVISOR_SNAPSHOT_FRONTIER_MAX_ITEMS = int(os.getenv("AGENTLOOM_SUPERVISOR_SNAPSHOT_FRONTIER_ITEMS", "8"))
SUPERVISOR_SNAPSHOT_FRONTIER_STRING_CHARS = int(
    os.getenv("AGENTLOOM_SUPERVISOR_SNAPSHOT_FRONTIER_STRING_CHARS", "240")
)

# Delegate constraint: governance/policy before grounded chain
DELEGATE_FLOW_FIRST_STAGNATION_EXCEPTION = int(os.getenv("AGENTLOOM_FLOW_FIRST_STAGNATION_EXCEPTION", "2"))

# Optional: require at least N document/mixed-grounded complete chains before critic (0 = off).
CRITIC_MIN_GROUNDED_CHAINS = int(os.getenv("AGENTLOOM_CRITIC_MIN_GROUNDED_CHAINS", "0"))

# When true, allow critic_agent even without structural/grounded business-complete chains if the graph has a
# typed operational backbone (see reachability typedBackboneChainCount) and stagnation is high enough.
CRITIC_ALLOW_TYPED_BACKBONE = os.getenv("AGENTLOOM_CRITIC_ALLOW_TYPED_BACKBONE", "").strip().lower() in (
    "1",
    "true",
    "on",
    "yes",
)
CRITIC_TYPED_BACKBONE_MIN_STAGNATION = int(os.getenv("AGENTLOOM_CRITIC_TYPED_BACKBONE_MIN_STAGNATION", "3"))

# flow_backbone todo: also satisfied by typedBackboneChainCount when merge runs (default on).
TODO_FLOW_BACKBONE_ACCEPTS_TYPED_BACKBONE = os.getenv(
    "AGENTLOOM_TODO_FLOW_BACKBONE_ACCEPTS_TYPED_BACKBONE", "1"
).strip().lower() not in ("0", "false", "off", "no")

# When COMPOSE_REQUIRES_TODOS is on, still allow compose if critic recommends and typed backbone exists.
COMPOSE_ALLOW_WITH_TYPED_BACKBONE = os.getenv("AGENTLOOM_COMPOSE_ALLOW_WITH_TYPED_BACKBONE", "").strip().lower() in (
    "1",
    "true",
    "on",
    "yes",
)

# Supervisor reachability list previews (0 = unlimited for lists in snapshot; counts always included).
SUPERVISOR_REACHABILITY_LIST_CAP = int(os.getenv("AGENTLOOM_SUPERVISOR_REACHABILITY_LIST_CAP", "8"))

# When true, compose gate also requires investigation todos (corridor + flow_backbone) satisfied.
COMPOSE_REQUIRES_TODOS_SATISFIED = os.getenv("AGENTLOOM_COMPOSE_REQUIRES_TODOS", "").strip().lower() in (
    "1",
    "true",
    "on",
    "yes",
)

# Question intent: structured LLM extract (Vertex), then regex+rubric fallback on any failure.
QUESTION_INTENT_LLM = os.getenv("AGENTLOOM_QUESTION_INTENT_LLM", "").strip().lower() in ("1", "true", "on", "yes")
QUESTION_INTENT_MODEL = (os.getenv("AGENTLOOM_QUESTION_INTENT_MODEL", "") or os.getenv("AGENTLOOM_WORKER_MODEL", "")).strip()
QUESTION_INTENT_MAX_OUTPUT_TOKENS = int(os.getenv("AGENTLOOM_QUESTION_INTENT_MAX_OUTPUT_TOKENS", "1024"))
# When set with QUESTION_INTENT_LLM, merge LLM corridor fields only; rubric/wants* axes stay heuristic.
QUESTION_INTENT_LLM_CORRIDOR_ONLY = os.getenv("AGENTLOOM_QUESTION_INTENT_LLM_CORRIDOR_ONLY", "").strip().lower() in (
    "1",
    "true",
    "on",
    "yes",
)

# Delegation: recycle supply on stagnation when no business-complete chain (legacy default on).
DELEGATE_STAGNATION_RECYCLE_SUPPLY = os.getenv("AGENTLOOM_DELEGATE_STAGNATION_RECYCLE_SUPPLY", "1").strip().lower() not in (
    "0",
    "false",
    "off",
    "no",
)

# Hypothesis runtime: deterministic supply-chain turn normalization (redirects partial/bridge work).
HYPOTHESIS_RUNTIME_STEER = os.getenv("AGENTLOOM_HYPOTHESIS_RUNTIME_STEER", "1").strip().lower() not in (
    "0",
    "false",
    "off",
    "no",
)
# When HYPOTHESIS_RUNTIME_STEER is on: override (replace LLM decision), advisory (hint only), or off (no normalize).
_HYP_STEER_MODE_RAW = (os.getenv("AGENTLOOM_HYPOTHESIS_RUNTIME_STEER_MODE") or "override").strip().lower()
HYPOTHESIS_RUNTIME_STEER_MODE: str = (
    _HYP_STEER_MODE_RAW if _HYP_STEER_MODE_RAW in ("override", "advisory", "off") else "override"
)

# Replace-entity hop-shrink guard (agent.py). When true, skip hop-based regression rejection.
DISABLE_REPLACE_HOP_REGRESSION = os.getenv("AGENTLOOM_DISABLE_REPLACE_HOP_REGRESSION", "").strip().lower() in (
    "1",
    "true",
    "on",
    "yes",
)

# Typed backbone critic escape may also trigger on tool-idle streak (investigator graph_delta empty).
CRITIC_ALLOW_TYPED_TOOL_IDLE = os.getenv("AGENTLOOM_CRITIC_ALLOW_TYPED_TOOL_IDLE", "").strip().lower() in (
    "1",
    "true",
    "on",
    "yes",
)
CRITIC_TYPED_BACKBONE_MIN_TOOL_IDLE = int(os.getenv("AGENTLOOM_CRITIC_TYPED_BACKBONE_MIN_TOOL_IDLE", "3"))

# Red team: compose gate when investigation preflight expects breadth (see agentloom.preflight).
RED_TEAM_BREADTH_WEIGHT_THRESHOLD = float(os.getenv("AGENTLOOM_RED_TEAM_BREADTH_WEIGHT_THRESHOLD", "0.65"))
# When false, preflight never requires a red-team pass for compose (ops kill-switch; prefer narrow use).
RED_TEAM_REQUIRED_FOR_BREADTH = os.getenv("AGENTLOOM_RED_TEAM_REQUIRED_FOR_BREADTH", "1").strip().lower() not in (
    "0",
    "false",
    "off",
    "no",
)
# When true, compose gate skips red-team satisfaction (debug / emergency only).
ALLOW_COMPOSE_WITHOUT_RED_TEAM = os.getenv("AGENTLOOM_ALLOW_COMPOSE_WITHOUT_RED_TEAM", "").strip().lower() in (
    "1",
    "true",
    "on",
    "yes",
)

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
STAGNATION_FORCE_CRITIC_CYCLES = int(os.getenv("AGENTLOOM_STAGNATION_FORCE_CRITIC", "3"))
REPEATED_GAP_THRESHOLD = int(os.getenv("AGENTLOOM_REPEATED_GAP_THRESHOLD", "2"))

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

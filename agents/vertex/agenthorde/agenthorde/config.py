"""AgentHorde caps and model ids (env-tunable; no hidden Vertex model aliasing)."""

from __future__ import annotations

import os

# Align with ADK heuristic when the root agent lives under .../agenthorde/agents/ (avoids app-name mismatch warning).
APP_NAME = os.getenv("AGENTHORDE_APP_NAME", "agents")
USER_ID = "agenthorde-runner"
SESSION_ID = "default"

SUPERVISOR_MODEL = os.getenv("AGENTHORDE_SUPERVISOR_MODEL", "gemini-3-flash-preview")
WORKER_MODEL = os.getenv("AGENTHORDE_WORKER_MODEL", "gemini-3-flash-preview")
CRITIC_MODEL = os.getenv("AGENTHORDE_CRITIC_MODEL", "gemini-3.1-pro-preview")

MAX_SUPERVISOR_CYCLES = int(os.getenv("AGENTHORDE_MAX_SUPERVISOR_CYCLES", "8"))

# Canonicalizer (post-run)
CANONICALIZER_ENTITY_MODEL = os.getenv("AGENTHORDE_CANONICALIZER_ENTITY_MODEL", "gemini-3.1-pro-preview")
CANONICALIZER_RELATION_MODEL = os.getenv("AGENTHORDE_CANONICALIZER_RELATION_MODEL", "gemini-3.1-pro-preview")
CANONICALIZER_METRIC_MODEL = os.getenv("AGENTHORDE_CANONICALIZER_METRIC_MODEL", "gemini-3-flash-preview")
CANONICALIZER_FUZZY_THRESHOLD = int(os.getenv("AGENTHORDE_CANONICALIZER_FUZZY_THRESHOLD", "85"))
CANONICALIZER_MERGE_CANDIDATE_LO = int(os.getenv("AGENTHORDE_CANONICALIZER_MERGE_CANDIDATE_LO", "80"))
# Path collision merge in phase 3 (same-type fuzzy on slug surfaces); defaults to entity dedup fuzzy threshold.
CANONICALIZER_PATH_COLLISION_MERGE_THRESHOLD = int(
    os.getenv("AGENTHORDE_CANONICALIZER_PATH_COLLISION_MERGE_THRESHOLD")
    or os.getenv("CANONICALIZER_PATH_COLLISION_MERGE_THRESHOLD")
    or str(CANONICALIZER_FUZZY_THRESHOLD)
)
CANONICALIZER_LLM_MAX_RETRIES = int(
    os.getenv(
        "AGENTHORDE_CANONICALIZER_LLM_MAX_RETRIES",
        os.getenv("CANONICALIZER_LLM_MAX_RETRIES", "3"),
    )
)
CANONICALIZER_LLM_TIMEOUT_SEC = float(
    os.getenv(
        "AGENTHORDE_CANONICALIZER_LLM_TIMEOUT_SEC",
        os.getenv("CANONICALIZER_LLM_TIMEOUT_SEC", "120"),
    )
)
# Phase 7 evidence fan-out: link each claim to additional corroborating sources beyond top-1.
# Max supported_by edges added from one claim fragment to source fragments.
CANONICALIZER_EVIDENCE_FANOUT_MAX = int(
    os.getenv(
        "AGENTHORDE_CANONICALIZER_EVIDENCE_FANOUT_MAX",
        os.getenv("CANONICALIZER_EVIDENCE_FANOUT_MAX", "8"),
    )
)
# Fuzzy-match score (0..100) required on web_search.answer vs claim text to count as supporting source.
CANONICALIZER_EVIDENCE_FANOUT_MIN_SCORE = int(
    os.getenv(
        "AGENTHORDE_CANONICALIZER_EVIDENCE_FANOUT_MIN_SCORE",
        os.getenv("CANONICALIZER_EVIDENCE_FANOUT_MIN_SCORE", "55"),
    )
)
# Looser threshold used ONLY when the claim has no direct evidence_refs at all; keeps
# Malacca-like prose-heavy runs from losing all retrieved sources.
CANONICALIZER_EVIDENCE_FALLBACK_MIN_SCORE = int(
    os.getenv(
        "AGENTHORDE_CANONICALIZER_EVIDENCE_FALLBACK_MIN_SCORE",
        os.getenv("CANONICALIZER_EVIDENCE_FALLBACK_MIN_SCORE", "45"),
    )
)
_hp = os.getenv("HORIZON_FIRESTORE_PROJECT", "").strip()
_gp = os.getenv("GOOGLE_CLOUD_PROJECT", "").strip()
HORIZON_FIRESTORE_PROJECT = _hp or _gp or None

STATE_PACK_KEY = "ah_pack"
STATE_SUPERVISOR_DECISION_KEY = "ah_supervisor_decision"
STATE_ANALYZER_OUTPUT_KEY = "ah_analyzer_output"
STATE_GOVERNANCE_OUTPUT_KEY = "ah_governance_output"
STATE_CRITIC_OUTPUT_KEY = "ah_critic_output"
STATE_RED_TEAM_OUTPUT_KEY = "ah_red_team_output"
STATE_FINAL_ANSWER_KEY = "ah_final_answer"
STATE_TOOL_LEDGER_KEY = "ah_tool_ledger"

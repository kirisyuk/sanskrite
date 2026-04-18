"""AgentHorde Vertex/ADK runtime: models, session keys, critic fallback (env-tunable)."""

from __future__ import annotations

import os

from agenthorde_common.config import (
    CANONICALIZER_ENTITY_MODEL,
    CANONICALIZER_EVIDENCE_FALLBACK_MIN_SCORE,
    CANONICALIZER_EVIDENCE_FANOUT_MAX,
    CANONICALIZER_EVIDENCE_FANOUT_MIN_SCORE,
    CANONICALIZER_FUZZY_THRESHOLD,
    CANONICALIZER_LLM_MAX_RETRIES,
    CANONICALIZER_LLM_TIMEOUT_SEC,
    CANONICALIZER_MERGE_CANDIDATE_LO,
    CANONICALIZER_METRIC_MODEL,
    CANONICALIZER_PATH_COLLISION_MERGE_THRESHOLD,
    CANONICALIZER_PHASE1_CHUNK_MAX,
    CANONICALIZER_PHASE1_MAX_DEPTH,
    CANONICALIZER_RELATION_MODEL,
    HORIZON_FIRESTORE_PROJECT,
)
from agenthorde_common.runtime.state_keys import (
    DEFAULT_MAX_SUPERVISOR_CYCLES,
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_CRITIC_OUTPUT_KEY,
    STATE_FINAL_ANSWER_KEY,
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_PACK_KEY,
    STATE_RED_TEAM_OUTPUT_KEY,
    STATE_SANCTIONS_OUTPUT_KEY,
    STATE_SUPERVISOR_DECISION_KEY,
    STATE_TOOL_LEDGER_KEY,
)

# Align with ADK heuristic when the root agent lives under .../agenthorde/agents/ (avoids app-name mismatch warning).
APP_NAME = os.getenv("AGENTHORDE_APP_NAME", "agents")
USER_ID = "agenthorde-runner"
SESSION_ID = "default"

SUPERVISOR_MODEL = os.getenv("AGENTHORDE_SUPERVISOR_MODEL", "gemini-3-flash-preview")
WORKER_MODEL = os.getenv("AGENTHORDE_WORKER_MODEL", "gemini-3-flash-preview")
CRITIC_MODEL = os.getenv("AGENTHORDE_CRITIC_MODEL", "gemini-3.1-pro-preview")

MAX_SUPERVISOR_CYCLES = int(
    os.getenv("AGENTHORDE_MAX_SUPERVISOR_CYCLES", str(DEFAULT_MAX_SUPERVISOR_CYCLES))
)


def _parse_csv_env(name: str, default: str) -> list[str]:
    raw = os.getenv(name, default)
    return [item.strip() for item in raw.split(",") if item.strip()]


# ---- Critic OpenRouter fallback (temporary in-Python path; roadmap: backend /api/v1/ai/critic). ----
CRITIC_FALLBACK_MODELS: list[str] = _parse_csv_env(
    "AGENTHORDE_CRITIC_FALLBACKS",
    "openai/o4-mini,z-ai/glm-5.1,deepseek/deepseek-chat-v3.1,nvidia/nemotron-3-super-120b-a12b",
)
CRITIC_OR_STRICT: bool = os.getenv("AGENTHORDE_CRITIC_OR_STRICT", "1") not in ("0", "false", "False")
CRITIC_OR_MAX_OUTPUT_TOKENS: int = int(os.getenv("AGENTHORDE_CRITIC_OR_MAX_OUTPUT_TOKENS", "6144"))
CRITIC_OR_MODEL_OVERRIDES: dict[str, dict] = {
    "openai/o4-mini": {
        "temperature": None,
        "extra_body": {"reasoning": {"effort": "low"}},
    },
}
CRITIC_AB_LOG_PATH: str = os.getenv(
    "AGENTHORDE_CRITIC_AB_LOG",
    "/tmp/agenthorde_smoke/critic_ab.jsonl",
)

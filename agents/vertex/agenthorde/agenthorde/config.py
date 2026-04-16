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

STATE_PACK_KEY = "ah_pack"
STATE_SUPERVISOR_DECISION_KEY = "ah_supervisor_decision"
STATE_ANALYZER_OUTPUT_KEY = "ah_analyzer_output"
STATE_GOVERNANCE_OUTPUT_KEY = "ah_governance_output"
STATE_CRITIC_OUTPUT_KEY = "ah_critic_output"
STATE_RED_TEAM_OUTPUT_KEY = "ah_red_team_output"
STATE_FINAL_ANSWER_KEY = "ah_final_answer"
STATE_TOOL_LEDGER_KEY = "ah_tool_ledger"

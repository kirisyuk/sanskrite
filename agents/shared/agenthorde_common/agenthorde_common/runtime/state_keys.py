"""Session state keys shared by Vertex ADK and Azure runtimes."""

from __future__ import annotations

STATE_PACK_KEY = "ah_pack"
STATE_SUPERVISOR_DECISION_KEY = "ah_supervisor_decision"
STATE_ANALYZER_OUTPUT_KEY = "ah_analyzer_output"
STATE_GOVERNANCE_OUTPUT_KEY = "ah_governance_output"
STATE_SANCTIONS_OUTPUT_KEY = "ah_sanctions_output"
STATE_CRITIC_OUTPUT_KEY = "ah_critic_output"
STATE_RED_TEAM_OUTPUT_KEY = "ah_red_team_output"
STATE_QUESTION_PLAN_KEY = "ah_question_plan"
STATE_FINAL_ANSWER_KEY = "ah_final_answer"
STATE_TOOL_LEDGER_KEY = "ah_tool_ledger"

DEFAULT_MAX_SUPERVISOR_CYCLES = 8

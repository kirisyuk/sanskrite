from __future__ import annotations

# pyright: reportMissingImports=false

from google.adk.agents import LlmAgent
from google.adk.planners import BuiltInPlanner
from google.genai import types as genai_types

from agenthorde.config import CRITIC_MODEL, SUPERVISOR_MODEL, WORKER_MODEL
from agenthorde_common.prompts.instructions import (
    ANALYZER_INSTRUCTION,
    CRITIC_INSTRUCTION,
    GOVERNANCE_CONTROL_INSTRUCTION,
    QUESTION_PLANNER_INSTRUCTION,
    RED_TEAM_INSTRUCTION,
    SANCTIONS_ANALYZER_INSTRUCTION,
    SUPERVISOR_ROUTER_INSTRUCTION,
)
from agenthorde_common.schemas.agents_io import (
    AnalyzerOutput,
    CriticVerdict,
    QuestionPlan,
    RedTeamOutput,
    SupervisorDecision,
)
from agenthorde.tools.builtins import (
    ah_knowledge_search,
    ah_retrieve,
    web_search,
    worker_tools,
)

_THINKING = BuiltInPlanner(thinking_config=genai_types.ThinkingConfig(include_thoughts=True))

supervisor_router = LlmAgent(
    model=SUPERVISOR_MODEL,
    name="supervisor_router",
    description="Chooses the next agent mission under cycle and temperature policy.",
    planner=_THINKING,
    instruction=SUPERVISOR_ROUTER_INSTRUCTION,
    output_schema=SupervisorDecision,
    disallow_transfer_to_parent=True,
    disallow_transfer_to_peers=True,
    output_key="ah_supervisor_decision",
)

supply_chain_analyzer = LlmAgent(
    model=WORKER_MODEL,
    name="supply_chain_analyzer",
    description="Analyzes supply-chain chains whole or in fragments with typed tools.",
    planner=_THINKING,
    instruction=ANALYZER_INSTRUCTION,
    tools=worker_tools(),
    output_schema=AnalyzerOutput,
    disallow_transfer_to_parent=True,
    disallow_transfer_to_peers=True,
    output_key="ah_analyzer_output",
)

governance_control_analyzer = LlmAgent(
    model=WORKER_MODEL,
    name="governance_control_analyzer",
    description="Maps legal-entity ownership, control, and jurisdictional structure; not cargo logistics.",
    planner=_THINKING,
    instruction=GOVERNANCE_CONTROL_INSTRUCTION,
    tools=worker_tools(),
    output_schema=AnalyzerOutput,
    disallow_transfer_to_parent=True,
    disallow_transfer_to_peers=True,
    output_key="ah_governance_output",
)

sanctions_analyzer = LlmAgent(
    model=WORKER_MODEL,
    name="sanctions_analyzer",
    description="Investigates sanctions regimes, export controls, and circumvention patterns (shadow fleet, shell cos, STS, FDPR, CHIPS guardrails).",
    planner=_THINKING,
    instruction=SANCTIONS_ANALYZER_INSTRUCTION,
    tools=worker_tools(),
    output_schema=AnalyzerOutput,
    disallow_transfer_to_parent=True,
    disallow_transfer_to_peers=True,
    output_key="ah_sanctions_output",
)

critic_agent = LlmAgent(
    model=CRITIC_MODEL,
    name="critic_agent",
    description="Strict sufficiency and mission-shaping review.",
    planner=_THINKING,
    instruction=CRITIC_INSTRUCTION,
    tools=[web_search, ah_knowledge_search, ah_retrieve],
    output_schema=CriticVerdict,
    disallow_transfer_to_parent=True,
    disallow_transfer_to_peers=True,
    output_key="ah_critic_output",
)

red_team_agent = LlmAgent(
    model=WORKER_MODEL,
    name="red_team_agent",
    description="Stress-tests framing when temperature and evidence warrant.",
    planner=_THINKING,
    instruction=RED_TEAM_INSTRUCTION,
    tools=worker_tools(),
    output_schema=RedTeamOutput,
    disallow_transfer_to_parent=True,
    disallow_transfer_to_peers=True,
    output_key="ah_red_team_output",
)

question_planner = LlmAgent(
    model=SUPERVISOR_MODEL,
    name="question_planner",
    description=(
        "Cycle-0 deterministic decomposition of the user question into a "
        "structured QuestionPlan (named entities, regimes, sub-domains, "
        "quantitative requirements, jurisdictional scope). Drives downstream "
        "routing and coverage discipline."
    ),
    planner=_THINKING,
    instruction=QUESTION_PLANNER_INSTRUCTION,
    output_schema=QuestionPlan,
    disallow_transfer_to_parent=True,
    disallow_transfer_to_peers=True,
    output_key="ah_question_plan",
)

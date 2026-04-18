"""Single-turn OpenAI (Azure) invocations mirroring ADK LlmAgent behavior."""

from __future__ import annotations

import logging
import time
from typing import Any, MutableMapping

from agenthorde_common.llm.openai_tool_definitions import CRITIC_OPENAI_TOOLS, WORKER_OPENAI_TOOLS

_LOG = logging.getLogger("agenthorde_azure.agent_turns")
from agenthorde_common.llm.openai_tool_loop import parse_structured_only, run_tool_loop_then_parse
from agenthorde_common.orchestration.supervisor_core import _ANALYZER_OUTPUT_KEY_FOR_AGENT
from agenthorde_common.prompts.instructions import (
    ANALYZER_INSTRUCTION,
    GOVERNANCE_CONTROL_INSTRUCTION,
    QUESTION_PLANNER_INSTRUCTION,
    RED_TEAM_INSTRUCTION,
    SANCTIONS_ANALYZER_INSTRUCTION,
    SUPERVISOR_ROUTER_INSTRUCTION,
)
from agenthorde_common.runtime.state_keys import (
    STATE_QUESTION_PLAN_KEY,
    STATE_RED_TEAM_OUTPUT_KEY,
    STATE_SUPERVISOR_DECISION_KEY,
)
from agenthorde_common.schemas.agents_io import (
    AnalyzerOutput,
    QuestionPlan,
    RedTeamOutput,
    SupervisorDecision,
)

from agenthorde_azure.openai_dispatch import build_tool_dispatch

_SYSTEM = (
    "You are an AgentHorde specialist. Obey tool requirements in the user message. "
    "Use tools honestly; the session ledger must reflect real tool calls. "
    "Your final structured output is validated against a JSON schema."
)


def _format_worker_instruction(template: str, state: MutableMapping[str, Any]) -> str:
    return template.format(
        ah_mission=str(state.get("ah_mission") or ""),
        ah_pack_json=str(state.get("ah_pack_json") or ""),
        ah_question_plan_json=str(state.get("ah_question_plan_json") or "{}"),
    )


async def run_question_planner_turn(
    *,
    client: Any,
    deployment: str,
    state: MutableMapping[str, Any],
) -> QuestionPlan | None:
    """Cycle-0 deterministic decomposition of the user question into a QuestionPlan.

    Mirrors the Vertex ``question_planner`` LlmAgent. Best-effort: failure is
    swallowed so downstream prompts simply see an empty plan and fall back to
    their built-in heuristics.
    """
    user = QUESTION_PLANNER_INSTRUCTION.format(
        ah_user_question=str(state.get("ah_user_question") or ""),
        ah_pack_json=str(state.get("ah_pack_json") or "{}"),
    )
    t0 = time.perf_counter()
    _LOG.info("question_planner: parse_structured_only start (model=%s chars=%d)", deployment, len(user))
    try:
        plan = await parse_structured_only(
            client=client,
            model=deployment,
            system_preamble=_SYSTEM,
            user_message=user,
            response_model=QuestionPlan,
            temperature=0.2,
        )
    except Exception as exc:  # noqa: BLE001 - planner is optional scaffolding
        _LOG.warning("question_planner: failed in %.1fs: %s", time.perf_counter() - t0, exc)
        return None
    _LOG.info(
        "question_planner: parsed in %.1fs entities=%d sub_domains=%d",
        time.perf_counter() - t0,
        len(plan.named_entities),
        len(plan.sub_domains),
    )
    state[STATE_QUESTION_PLAN_KEY] = plan.model_dump(mode="json", by_alias=True)
    return plan


async def run_supervisor_router_turn(
    *,
    client: Any,
    deployment: str,
    state: MutableMapping[str, Any],
) -> SupervisorDecision:
    user = SUPERVISOR_ROUTER_INSTRUCTION.format(
        ah_pack_json=str(state.get("ah_pack_json") or ""),
        ah_user_question=str(state.get("ah_user_question") or ""),
        ah_question_plan_json=str(state.get("ah_question_plan_json") or "{}"),
        ah_critic_json=str(state.get("ah_critic_json") or "{}"),
        ah_analyzer_json=str(state.get("ah_analyzer_json") or "{}"),
        ah_governance_json=str(state.get("ah_governance_json") or "{}"),
        ah_sanctions_json=str(state.get("ah_sanctions_json") or "{}"),
        ah_red_team_json=str(state.get("ah_red_team_json") or "{}"),
    )
    t0 = time.perf_counter()
    _LOG.info("supervisor_router: parse_structured_only start (model=%s chars=%d)", deployment, len(user))
    dec = await parse_structured_only(
        client=client,
        model=deployment,
        system_preamble=_SYSTEM,
        user_message=user,
        response_model=SupervisorDecision,
        temperature=0.2,
    )
    _LOG.info(
        "supervisor_router: parsed in %.1fs next_agent=%s",
        time.perf_counter() - t0,
        getattr(dec, "next_agent", "?"),
    )
    state[STATE_SUPERVISOR_DECISION_KEY] = dec.model_dump(mode="json", by_alias=True)
    return dec


async def run_named_worker_turn(
    *,
    client: Any,
    deployment: str,
    agent_name: str,
    state: MutableMapping[str, Any],
) -> None:
    if agent_name == "red_team_agent":
        user = _format_worker_instruction(RED_TEAM_INSTRUCTION, state)
        dispatch = build_tool_dispatch(client, deployment)
        t0 = time.perf_counter()
        _LOG.info("worker %s: tool-loop start (model=%s prompt_chars=%d)", agent_name, deployment, len(user))
        out = await run_tool_loop_then_parse(
            client=client,
            model=deployment,
            system_preamble=_SYSTEM,
            user_message=user,
            tools=WORKER_OPENAI_TOOLS,
            dispatch=dispatch,
            state=state,
            response_model=RedTeamOutput,
            temperature=0.2,
        )
        _LOG.info("worker %s: done in %.1fs", agent_name, time.perf_counter() - t0)
        state[STATE_RED_TEAM_OUTPUT_KEY] = out.model_dump(mode="json", by_alias=True)
        return

    templates = {
        "supply_chain_analyzer": ANALYZER_INSTRUCTION,
        "governance_control_analyzer": GOVERNANCE_CONTROL_INSTRUCTION,
        "sanctions_analyzer": SANCTIONS_ANALYZER_INSTRUCTION,
    }
    tmpl = templates.get(agent_name)
    if tmpl is None:
        raise ValueError(f"unknown worker {agent_name}")
    user = _format_worker_instruction(tmpl, state)
    dispatch = build_tool_dispatch(client, deployment)
    t0 = time.perf_counter()
    _LOG.info("worker %s: tool-loop start (model=%s prompt_chars=%d)", agent_name, deployment, len(user))
    out = await run_tool_loop_then_parse(
        client=client,
        model=deployment,
        system_preamble=_SYSTEM,
        user_message=user,
        tools=WORKER_OPENAI_TOOLS,
        dispatch=dispatch,
        state=state,
        response_model=AnalyzerOutput,
        temperature=0.2,
    )
    _LOG.info("worker %s: done in %.1fs", agent_name, time.perf_counter() - t0)
    key = _ANALYZER_OUTPUT_KEY_FOR_AGENT[agent_name]
    state[key] = out.model_dump(mode="json", by_alias=True)

"""End-to-end smoke for the full critic/analyzer/red_team/planner scaffolding.

Single-pass on one mini-tier model per role (defaults to ``openai/gpt-5.4-mini``).
Each role check has an explicit pass/fail criterion derived from the new
prompt discipline; we PRINT pass/fail per criterion so the human can read
whether the scaffolding is doing what it promises.

This is a content-coverage smoke, not a tool-loop integration test:
analyzer + red_team are run in tools-disabled mode (the model is told to
emit ``evidenceRefs: ["prior_knowledge"]`` instead of calling web_search).

Roles + criteria:
  * question_planner  — emits ≥3 namedEntities, ≥1 sub-domain matches
                        ``supply_chain``, sets spofRequested=true.
  * supply_chain_analyzer — proposedEntities contains at least one of
                        {ZEISS, Cymer, Trumpf} (Tier-2 semi chokepoint).
  * critic_agent      — emits recommendedNextAgent != "none" when blockers
                        are non-empty.
  * red_team_agent    — alternativeNarratives has ≥2 entries; probeMissions
                        contain at least one item that names an entity AND a
                        jurisdiction or date marker.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import sys
import time
from pathlib import Path

THIS = Path(__file__).resolve()
ROOT = THIS.parent.parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from agenthorde.critic.openrouter_client import (  # noqa: E402
    OpenRouterCriticClient,
    OpenRouterCriticError,
)
from agenthorde.critic.strict_schema import to_openai_strict_schema  # noqa: E402
from agenthorde_common.critic.prompt import render_critic_prompt  # noqa: E402
from agenthorde_common.prompts.instructions import (  # noqa: E402
    ANALYZER_INSTRUCTION,
    QUESTION_PLANNER_INSTRUCTION,
    RED_TEAM_INSTRUCTION,
)
from agenthorde_common.runtime.state_keys import (  # noqa: E402
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_PACK_KEY,
    STATE_QUESTION_PLAN_KEY,
    STATE_SANCTIONS_OUTPUT_KEY,
)
from agenthorde_common.schemas.agents_io import (  # noqa: E402
    AnalyzerOutput,
    CriticVerdict,
    QuestionPlan,
    RedTeamOutput,
)


SEMI_QUESTION = (
    "Analyse the advanced-node semiconductor supply chain under CHIPS Act / "
    "US export control pressure. Focus on dependencies between TSMC, ASML, "
    "Samsung, Intel, SMIC, and Japanese/Dutch equipment vendors (Tokyo Electron, "
    "ASML); cover both the design and lithography layers. Flag single points of failure."
)


_PACK = {
    "userQuestion": SEMI_QUESTION,
    "cycleIndex": 1,
    "supervisorCap": 8,
    "researchTemperature": 0.5,
    "reasoningGraph": {"nodes": [], "edges": []},
    "openQuestions": [],
    "toolLedger": {"webSearchCalls": 0, "retrieveCalls": 0},
}


_NO_TOOLS_PREAMBLE = (
    "You are operating in tools-disabled mode for this call. Do NOT request "
    "web_search, retrieve, knowledge_search, or apply. All context you need is "
    "in the prompt body. Where the prompt asks you to call tools, satisfy the "
    "instruction by emitting node-level prior knowledge: set every "
    "proposedNodes[i].evidenceRefs to [\"prior_knowledge\"]. Return the "
    "schema-conformant JSON object and nothing else."
)


def _build_planner_prompt() -> str:
    return QUESTION_PLANNER_INSTRUCTION.format(
        ah_user_question=SEMI_QUESTION,
        ah_pack_json=json.dumps(_PACK, ensure_ascii=False),
    )


def _build_analyzer_prompt(plan: dict | None) -> str:
    body = ANALYZER_INSTRUCTION.format(
        ah_mission="(no supervisor mission yet — infer from question)",
        ah_pack_json=json.dumps(_PACK, ensure_ascii=False),
        ah_question_plan_json=json.dumps(plan or {}, ensure_ascii=False),
    )
    return _NO_TOOLS_PREAMBLE + "\n\n" + body


def _build_critic_prompt(plan: dict | None, analyzer_out: dict | None) -> str:
    state: dict = {
        "ah_user_question": SEMI_QUESTION,
        STATE_PACK_KEY: _PACK,
        STATE_ANALYZER_OUTPUT_KEY: analyzer_out or {},
        STATE_GOVERNANCE_OUTPUT_KEY: {},
        STATE_SANCTIONS_OUTPUT_KEY: {},
        STATE_QUESTION_PLAN_KEY: plan or {},
    }
    return render_critic_prompt(state, tools_disabled=True)


def _build_red_team_prompt(plan: dict | None) -> str:
    body = RED_TEAM_INSTRUCTION.format(
        ah_pack_json=json.dumps(_PACK, ensure_ascii=False),
        ah_mission=(
            "Stress-test the converging semiconductor monopoly narrative; "
            "surface ≥2 alternative framings of the same evidence pack."
        ),
        ah_question_plan_json=json.dumps(plan or {}, ensure_ascii=False),
    )
    return _NO_TOOLS_PREAMBLE + "\n\n" + body


async def _call(
    client: OpenRouterCriticClient,
    model: str,
    prompt: str,
    schema: dict,
    *,
    max_output_tokens: int,
    timeout_s: float,
    extra_body: dict | None = None,
    temperature: float | None = 0.2,
) -> tuple[dict | None, dict]:
    """Return (parsed_json | None, meta) where meta carries timing + raw."""
    meta: dict = {"latency_ms": None, "input_tokens": None, "output_tokens": None,
                  "finish_reason": None, "error": None, "raw": ""}
    t0 = time.perf_counter()
    try:
        result = await client.chat_structured(
            model=model,
            prompt=prompt,
            json_schema=schema,
            strict=True,
            max_output_tokens=max_output_tokens,
            temperature=temperature,
            extra_body=extra_body,
        )
    except OpenRouterCriticError as exc:
        meta["latency_ms"] = int((time.perf_counter() - t0) * 1000)
        meta["error"] = f"{type(exc).__name__}: {exc}"
        return None, meta
    meta["latency_ms"] = result.latency_ms
    meta["input_tokens"] = result.input_tokens
    meta["output_tokens"] = result.output_tokens
    meta["finish_reason"] = result.finish_reason
    meta["raw"] = result.content
    try:
        parsed = json.loads(result.content)
    except json.JSONDecodeError as exc:
        meta["error"] = f"invalid_json: {exc}"
        return None, meta
    return parsed, meta


# ---- Per-role pass/fail criteria --------------------------------------------

def check_planner(parsed: dict | None) -> dict:
    if not parsed:
        return {"ok": False, "reason": "no parsed output"}
    try:
        plan = QuestionPlan.model_validate(parsed)
    except Exception as exc:
        return {"ok": False, "reason": f"schema_fail: {exc}"}
    n_entities = len(plan.named_entities)
    sub_names = {sd.name for sd in plan.sub_domains}
    has_sc = "supply_chain" in sub_names
    spof_ok = plan.spof_requested is True
    crits = {
        "named_entities>=3": n_entities >= 3,
        "supply_chain in subDomains": has_sc,
        "spofRequested=true": spof_ok,
    }
    return {
        "ok": all(crits.values()),
        "criteria": crits,
        "n_entities": n_entities,
        "named_entities": plan.named_entities,
        "sub_domains": [sd.name for sd in plan.sub_domains],
        "spof": spof_ok,
    }


_TIER2_SEMI = re.compile(r"\b(zeiss|cymer|trumpf|jsr|shin[-\s]?etsu|tokyo ohka|tok|photronics|toppan|sumco|globalwafers|amkor|ase|spil|ibiden|unimicron|arm)\b", re.I)


def check_analyzer(parsed: dict | None) -> dict:
    if not parsed:
        return {"ok": False, "reason": "no parsed output"}
    try:
        ao = AnalyzerOutput.model_validate(parsed)
    except Exception as exc:
        return {"ok": False, "reason": f"schema_fail: {exc}"}
    entity_labels = [e.label for e in ao.proposed_entities]
    tier2_hits = sorted({m.group(1).lower() for label in entity_labels for m in [_TIER2_SEMI.search(label)] if m})
    crits = {
        "proposedEntities >= 5": len(entity_labels) >= 5,
        "≥1 Tier-2 chokepoint emitted": bool(tier2_hits),
    }
    return {
        "ok": all(crits.values()),
        "criteria": crits,
        "entity_count": len(entity_labels),
        "tier2_hits": tier2_hits,
        "entities_preview": entity_labels[:12],
    }


def check_critic(parsed: dict | None) -> dict:
    if not parsed:
        return {"ok": False, "reason": "no parsed output"}
    try:
        cv = CriticVerdict.model_validate(parsed)
    except Exception as exc:
        return {"ok": False, "reason": f"schema_fail: {exc}"}
    has_blockers = bool(cv.blockers)
    rec = cv.recommended_next_agent
    crits = {
        "recommendedNextAgent populated": rec in {
            "supply_chain_analyzer", "governance_control_analyzer",
            "sanctions_analyzer", "red_team_agent", "none",
        },
        "blockers→non-none routing": (not has_blockers) or (rec != "none"),
        "comp consistency (sufficient↔go)": (
            (cv.composition_recommendation != "go") or (cv.sufficient and not cv.blockers)
        ),
    }
    return {
        "ok": all(crits.values()),
        "criteria": crits,
        "sufficient": cv.sufficient,
        "n_blockers": len(cv.blockers),
        "recommendedNextAgent": rec,
        "compositionRecommendation": cv.composition_recommendation,
    }


def _probe_concrete(text: str) -> bool:
    has_entity = bool(re.search(r"[A-Z][A-Za-z0-9&\.\- ]{2,}", text))
    juris_or_date = bool(
        re.search(r"\b(20\d{2}|Q[1-4]\s*20\d{2}|FY?20\d{2})\b", text)
        or re.search(r"\b(US|EU|UK|JP|NL|TW|CN|RU|KR|IN|UAE|HK|BVI|OFAC|BIS|SDN|VEB|METI|FDPR|EAR|SECO|OFSI)\b", text)
    )
    return has_entity and juris_or_date


def check_red_team(parsed: dict | None) -> dict:
    if not parsed:
        return {"ok": False, "reason": "no parsed output"}
    try:
        rt = RedTeamOutput.model_validate(parsed)
    except Exception as exc:
        return {"ok": False, "reason": f"schema_fail: {exc}"}
    n_alts = len(rt.alternative_narratives)
    n_probes = len(rt.probe_missions)
    concrete_probes = [p for p in rt.probe_missions if _probe_concrete(p)]
    crits = {
        "alternativeNarratives >= 2": n_alts >= 2,
        "≥1 concrete probeMission": bool(concrete_probes),
        "dominantNarrative populated": bool(rt.dominant_narrative.strip()),
    }
    return {
        "ok": all(crits.values()),
        "criteria": crits,
        "n_alternatives": n_alts,
        "n_probes": n_probes,
        "concrete_probes": concrete_probes[:3],
    }


# ---- Driver ------------------------------------------------------------------

def _reasoning_extra(effort: str | None) -> dict | None:
    """Build OpenRouter ``reasoning`` extension body from CLI effort string."""
    if not effort or effort == "none":
        return None
    return {"reasoning": {"effort": effort}}


async def _main() -> None:
    parser = argparse.ArgumentParser(description="End-to-end scaffolding smoke")
    parser.add_argument("--model", default="openai/gpt-5.4-mini",
                        help="Default model for all roles unless per-role override given")
    parser.add_argument("--timeout", type=float, default=600.0)
    parser.add_argument("--max-output-tokens", type=int, default=4096,
                        help="Default token cap for all roles")
    parser.add_argument("--reasoning-effort", default="none",
                        choices=["none", "low", "medium", "high"],
                        help="Default reasoning.effort sent to OpenRouter (omitted when 'none')")
    # Per-role overrides. Empty string means "inherit default".
    for role in ("planner", "analyzer", "critic", "red-team"):
        parser.add_argument(f"--{role}-model", default="",
                            help=f"Override model for {role} role")
        parser.add_argument(f"--{role}-max-output-tokens", type=int, default=0,
                            help=f"Override max_output_tokens for {role} (0 = inherit)")
        parser.add_argument(f"--{role}-reasoning-effort", default="",
                            choices=["", "none", "low", "medium", "high"],
                            help=f"Override reasoning.effort for {role} ('' = inherit)")
    parser.add_argument("--out", default="/tmp/agenthorde_smoke/full_loop_smoke.json")
    args = parser.parse_args()

    def role_cfg(role: str) -> tuple[str, int, dict | None]:
        attr = role.replace("-", "_")
        m = getattr(args, f"{attr}_model") or args.model
        cap_override = getattr(args, f"{attr}_max_output_tokens")
        cap = cap_override if cap_override > 0 else args.max_output_tokens
        effort_override = getattr(args, f"{attr}_reasoning_effort")
        effort = effort_override if effort_override != "" else args.reasoning_effort
        return m, cap, _reasoning_extra(effort)

    if not os.getenv("OPENROUTER_API_KEY"):
        print("ERROR: OPENROUTER_API_KEY is not set", file=sys.stderr)
        sys.exit(2)

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    client = OpenRouterCriticClient(timeout_s=args.timeout)

    schemas = {
        "planner": to_openai_strict_schema(QuestionPlan.model_json_schema(by_alias=True)),
        "analyzer": to_openai_strict_schema(AnalyzerOutput.model_json_schema(by_alias=True)),
        "critic": to_openai_strict_schema(CriticVerdict.model_json_schema(by_alias=True)),
        "red_team": to_openai_strict_schema(RedTeamOutput.model_json_schema(by_alias=True)),
    }

    planner_model, planner_cap, planner_xb = role_cfg("planner")
    analyzer_model, analyzer_cap, analyzer_xb = role_cfg("analyzer")
    critic_model, critic_cap, critic_xb = role_cfg("critic")
    redteam_model, redteam_cap, redteam_xb = role_cfg("red-team")

    print(f"default model = {args.model}  default cap = {args.max_output_tokens}  "
          f"default reasoning = {args.reasoning_effort}")
    print(f"  planner   = {planner_model}  cap={planner_cap}  reasoning={planner_xb}")
    print(f"  analyzer  = {analyzer_model}  cap={analyzer_cap}  reasoning={analyzer_xb}")
    print(f"  critic    = {critic_model}  cap={critic_cap}  reasoning={critic_xb}")
    print(f"  red_team  = {redteam_model}  cap={redteam_cap}  reasoning={redteam_xb}")
    print(f"question = {SEMI_QUESTION!r}\n")

    # ---- Planner --------------------------------------------------------------
    print("[1/4] question_planner ...")
    planner_prompt = _build_planner_prompt()
    plan_parsed, plan_meta = await _call(
        client, planner_model, planner_prompt, schemas["planner"],
        max_output_tokens=planner_cap, timeout_s=args.timeout, extra_body=planner_xb,
    )
    plan_check = check_planner(plan_parsed)
    print(f"      lat={plan_meta['latency_ms']}ms  ok={plan_check['ok']}")
    print(f"      named_entities={plan_check.get('named_entities')}")
    print(f"      sub_domains={plan_check.get('sub_domains')}  spof={plan_check.get('spof')}")
    print(f"      criteria={plan_check.get('criteria')}")
    if plan_meta.get("error"):
        print(f"      error={plan_meta['error']}")
    print()

    # ---- Analyzer -------------------------------------------------------------
    print("[2/4] supply_chain_analyzer ...")
    analyzer_prompt = _build_analyzer_prompt(plan_parsed)
    ana_parsed, ana_meta = await _call(
        client, analyzer_model, analyzer_prompt, schemas["analyzer"],
        max_output_tokens=analyzer_cap, timeout_s=args.timeout, extra_body=analyzer_xb,
    )
    ana_check = check_analyzer(ana_parsed)
    print(f"      lat={ana_meta['latency_ms']}ms  ok={ana_check['ok']}")
    print(f"      proposedEntities={ana_check.get('entity_count')}  tier2_hits={ana_check.get('tier2_hits')}")
    print(f"      entities_preview={ana_check.get('entities_preview')}")
    print(f"      criteria={ana_check.get('criteria')}")
    if ana_meta.get("error"):
        print(f"      error={ana_meta['error']}")
    print()

    # ---- Critic ---------------------------------------------------------------
    print("[3/4] critic_agent ...")
    critic_prompt = _build_critic_prompt(plan_parsed, ana_parsed)
    cv_parsed, cv_meta = await _call(
        client, critic_model, critic_prompt, schemas["critic"],
        max_output_tokens=critic_cap, timeout_s=args.timeout, extra_body=critic_xb,
    )
    cv_check = check_critic(cv_parsed)
    print(f"      lat={cv_meta['latency_ms']}ms  ok={cv_check['ok']}")
    print(f"      sufficient={cv_check.get('sufficient')}  blockers={cv_check.get('n_blockers')}  "
          f"comp={cv_check.get('compositionRecommendation')}  next={cv_check.get('recommendedNextAgent')}")
    print(f"      criteria={cv_check.get('criteria')}")
    if cv_meta.get("error"):
        print(f"      error={cv_meta['error']}")
    print()

    # ---- Red team -------------------------------------------------------------
    print("[4/4] red_team_agent ...")
    rt_prompt = _build_red_team_prompt(plan_parsed)
    rt_parsed, rt_meta = await _call(
        client, redteam_model, rt_prompt, schemas["red_team"],
        max_output_tokens=redteam_cap, timeout_s=args.timeout, extra_body=redteam_xb,
    )
    rt_check = check_red_team(rt_parsed)
    print(f"      lat={rt_meta['latency_ms']}ms  ok={rt_check['ok']}")
    print(f"      alternatives={rt_check.get('n_alternatives')}  probes={rt_check.get('n_probes')}")
    print(f"      concrete_probes={rt_check.get('concrete_probes')}")
    print(f"      criteria={rt_check.get('criteria')}")
    if rt_meta.get("error"):
        print(f"      error={rt_meta['error']}")
    print()

    # ---- Summary --------------------------------------------------------------
    summary = {
        "default_model": args.model,
        "default_max_output_tokens": args.max_output_tokens,
        "default_reasoning_effort": args.reasoning_effort,
        "role_config": {
            "planner": {"model": planner_model, "cap": planner_cap, "extra_body": planner_xb},
            "analyzer": {"model": analyzer_model, "cap": analyzer_cap, "extra_body": analyzer_xb},
            "critic": {"model": critic_model, "cap": critic_cap, "extra_body": critic_xb},
            "red_team": {"model": redteam_model, "cap": redteam_cap, "extra_body": redteam_xb},
        },
        "planner": {"meta": plan_meta, "check": plan_check, "parsed": plan_parsed},
        "analyzer": {"meta": ana_meta, "check": ana_check, "parsed": ana_parsed},
        "critic": {"meta": cv_meta, "check": cv_check, "parsed": cv_parsed},
        "red_team": {"meta": rt_meta, "check": rt_check, "parsed": rt_parsed},
    }
    out_path.write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    overall = all(c["check"]["ok"] for c in (
        summary["planner"], summary["analyzer"], summary["critic"], summary["red_team"]
    ))
    print(f"OVERALL: {'PASS' if overall else 'FAIL'}")
    print(f"saved={out_path}")


if __name__ == "__main__":
    asyncio.run(_main())

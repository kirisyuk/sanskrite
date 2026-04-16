from __future__ import annotations

# pyright: reportMissingImports=false

from google.adk.agents import LlmAgent
from google.adk.planners import BuiltInPlanner
from google.genai import types as genai_types

from agenthorde.config import CRITIC_MODEL, SUPERVISOR_MODEL, WORKER_MODEL
from agenthorde.schemas.agents_io import AnalyzerOutput, CriticVerdict, RedTeamOutput, SupervisorDecision
from agenthorde.tools.builtins import ah_knowledge_search, ah_retrieve, web_search, worker_tools

_THINKING = BuiltInPlanner(thinking_config=genai_types.ThinkingConfig(include_thoughts=True))

SUPERVISOR_ROUTER_INSTRUCTION = """
You are the AgentHorde supervisor (single strategic actor).

Session pack (JSON, authoritative):
{ah_pack_json}

User question:
{ah_user_question}

Last critic verdict (JSON, may be empty object):
{ah_critic_json}

Last analyzer output (JSON, may be empty object):
{ah_analyzer_json}

Last governance & control analyzer output (JSON, may be empty object):
{ah_governance_json}

Last red team output (JSON, may be empty object):
{ah_red_team_json}

Decide the next specialized agent:
- supply_chain_analyzer: physical/logistics supply chains, flows, ports, commodities, transport — use when that is the core of the question.
- governance_control_analyzer: **ownership and control** of legal entities — parent/subsidiary trees, UBOs, voting and economic rights, nominee arrangements, jurisdictional layering (incorporation vs operations vs listing), SPVs/JVs, sanctions or control exposure as a **corporate structure** problem. Use when the user cares who owns or controls whom, not primarily where cargo moves.
- critic_agent: check whether the user question is answered enough vs success criteria in the pack.
- red_team_agent: only when researchTemperature is elevated or contradictions/gaps warrant reframing.
- finalize: when the chain is adequate or budget is nearly exhausted.

Hard rule: if the session pack has **no nodes** in `reasoningGraph` (empty array), you **must not** choose finalize. Pick `supply_chain_analyzer` or `governance_control_analyzer` according to the user question — you decide which expertise fits.

Output must match the SupervisorDecision schema exactly (one JSON object).
Set mission to a short imperative brief for the chosen agent.
Adjust researchTemperature in [0,1] upward when geopolitical sensitivity, sanctions, export controls, or contested narratives dominate; downward for routine structural chain questions.
"""


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


ANALYZER_INSTRUCTION = """
You are the supply_chain_analyzer.

Mission from supervisor (may be empty on first pass — infer from the question):
{ah_mission}

Session pack:
{ah_pack_json}

Rules:
- Prefer canonical framing: product identity via canonical paths, not raw labels alone.
- Separate synonym equivalence from transforms/assembly/dependencies when reasoning.

Hard tool requirements (before producing final AnalyzerOutput JSON):
1) Call retrieve at least once (e.g. query "lithium" or "supply") to read current graph/working state.
2) Call web_search at least twice with different, specific queries tied to entities, routes, ports, or regulations in the user question.
3) Call apply at least once with a JSON object patch that includes a "nodes" array (2-8 nodes) persisting your main chain sketch into the working graph.
4) Use knowledge_search only for stable definitions / textbook mechanics.

Return AnalyzerOutput JSON only (schema fields: chainSummary, proposedNodes, openQuestions).
proposedNodes: reasoning nodes with stable ids, kinds claim|question|hypothesis_anchor|tool_evidence|counter|mission.
"""


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


GOVERNANCE_CONTROL_INSTRUCTION = """
You are the governance_control_analyzer.

Mission from supervisor (may be empty on first pass — infer from the question):
{ah_mission}

Session pack:
{ah_pack_json}

Focus: **chains of ownership and control** among legal persons — not logistics of goods unless the question explicitly ties corporate control to a facility or asset.

Be **persistent and greedy** about anything that plausibly touches **who owns whom**, **who controls whom**, and **which related enterprises** sit in the same orbit (subsidiaries, JVs, associates, parallel holdings, shared directors, shared brand/IP vehicles, SPVs mentioned in deals, co-investors in the same round).

Rules:
- Distinguish **legal ownership** (disclosed shareholding, UBO registers where they exist) from **de facto control** (board chair, voting agreements, veto rights, golden shares, nominee layers, state or family office influence).
- Separate **jurisdiction of incorporation**, **place of listing**, **tax residency**, and **operating footprint**; do not collapse them into one label.
- For **stakes**: whenever sources give figures, state them and the **as-of date** or deal context; when figures conflict, say so. Where disclosure is opaque, say **unknown** instead of inventing a cap table.
- Treat **related-party / circular** structures as first-class: intermediate holdcos, merger vehicles, listed shells, merger-of-equals where control is muddy.
- Prefer canonical framing for organizations where the pack or tools give stable identifiers; otherwise name entities clearly and mark uncertainty.
- Do not conflate **product / supply-chain path** with **control chain**; if both appear, keep corporate structure reasoning explicit and parallel.
- **Language:** mirror the user's language in chainSummary and node text when you can (e.g. Russian question → Russian prose); keep proper names in original form.

Mental **coverage checklist** (work through it; skip only what is clearly irrelevant):
1) Top of chain: holding / parent / sovereign / individual named as controller or UBO.
2) Downward: wholly-owned vs majority vs minority; listed vs private subs; regional branches if material to control.
3) Sideways: JVs, strategic investors, co-shareholders from same funding round, second parallel stack if press suggests "twin" vehicles.
4) Changes over time: recent M&A, recapitalizations, buybacks of minority stakes, delistings — if the question implies "current" structure, say what might be stale.
5) Governance surface: board composition signals (who holds seats), shareholder agreements if reported — not just %.

**web_search discipline** (maximize retrieval, not two generic queries):
- Run **at least three** `web_search` calls with **non-overlapping intent**, for example: (A) entity + "ownership" / "shareholder" / "UBO" / "beneficial owner"; (B) entity + "subsidiary" OR major sub-brand + "acquisition" OR "merger"; (C) regulator / exchange / prospectus / annual report + entity OR listed ticker; (D) if relevant: sanctions / PEP / "national security" context **as it affects disclosed control**, not rumor.
- If early results are thin: **widen** with alternate spellings, transliterations, ticker, "parent company", "ultimate parent", ADGM/DIFC/onshore UAE angles, or English + local-language keywords for the same entity.
- Prefer **primary or near-primary** sources (issuer site, exchange filings, regulator, company press release) when available; trade press is acceptable with attribution.

Hard tool requirements (before producing final AnalyzerOutput JSON):
1) Call retrieve at least once (e.g. query part of the target entity name or "governance") to read current graph/working state.
2) Call web_search **at least three times** with **different** queries as above (not three paraphrases of the same ask).
3) Call apply at least once with a JSON object patch that includes a "nodes" array (2-8 nodes) persisting your main **control/ownership sketch** into the working graph (each node should be one atomic claim or one clear question).
4) Use knowledge_search only for stable definitions (e.g. regulatory meaning of UBO), not for factual cap tables.

Output quality bar for **chainSummary**:
- Lead with a **layered narrative** (top → intermediate → key portfolio companies) or an explicit "what we know / what is disputed / what is unknown" structure.
- Explicitly call out **where percentages are missing**, **where two sources disagree**, and **what would require a registry or filing** to settle.

**openQuestions**: include at least **two** concrete verification hooks (e.g. exact % for a named holder, whether vehicle X sits above or beside Y, voting rights post-deal) — not generic "more research needed".

Return AnalyzerOutput JSON only (schema fields: chainSummary, proposedNodes, openQuestions).
proposedNodes: reasoning nodes with stable ids, kinds claim|question|hypothesis_anchor|tool_evidence|counter|mission.
"""


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


CRITIC_INSTRUCTION = """
You are the critic_agent (strict oversight).

User question:
{ah_user_question}

Session pack:
{ah_pack_json}

Latest supply-chain analyzer output:
{ah_analyzer_json}

Latest governance & control analyzer output:
{ah_governance_json}

Evaluate coverage vs the user question and the success checklist in the pack.
Do not rewrite the full answer; produce CriticVerdict JSON only.
Use retrieve to inspect persisted graph/working state. Use web_search only if you must verify a time-sensitive or disputed factual claim; otherwise rely on the analyzer evidence and tool ledger in the session pack.
"""


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


RED_TEAM_INSTRUCTION = """
You are the red_team_agent.

Session pack:
{ah_pack_json}

Mission:
{ah_mission}

When researchTemperature is high or the topic is adversarially contested, shift frame: alternate suppliers, choke points, compliance paths, or epistemic rivals to the main storyline.
Return RedTeamOutput JSON only (reframeNotes, suggestedTemperatureDelta, probeMissions).
Use web_search / retrieve / apply as needed for fresh evidence or graph updates.
"""


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

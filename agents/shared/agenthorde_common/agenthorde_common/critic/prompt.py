"""Critic prompt rendering (same template as Vertex ``CRITIC_INSTRUCTION``).

The critic prompt has two responsibilities:

1. Surface the latest analyzer outputs and the session pack to the model.
2. Hand the model an explicit **object registry** with stable ids so
   critical / advisory finding entries can cite specific entities, claims,
   and pack fragments instead of producing free-form prose. The registry
   is built in Python from already-validated state — the model never
   assigns ids itself for cross-document scope, only references the ids
   we hand it.
"""

from __future__ import annotations

import json
from typing import Any

from agenthorde_common.prompts.instructions import CRITIC_INSTRUCTION
from agenthorde_common.runtime.state_keys import (
    STATE_ANALYZER_OUTPUT_KEY,
    STATE_DECISION_INFLUENCE_OUTPUT_KEY,
    STATE_GOVERNANCE_OUTPUT_KEY,
    STATE_PACK_KEY,
    STATE_QUESTION_PLAN_KEY,
    STATE_SANCTIONS_OUTPUT_KEY,
)

_NO_TOOLS_PREAMBLE = (
    "You are operating in tools-disabled mode for this call. Do NOT request "
    "web_search, retrieve, or any tool. All context you need is provided "
    "below. Produce CriticVerdict JSON strictly adhering to the supplied "
    "schema and return nothing else."
)

_REGISTRY_ROW_LIMIT = 80


def _compact(raw: Any, limit: int = 14000) -> str:
    if raw is None:
        return "{}"
    if isinstance(raw, str):
        s = raw
    else:
        try:
            s = json.dumps(raw, ensure_ascii=False, default=str)
        except TypeError:
            s = str(raw)
    if len(s) > limit:
        return s[: limit - 20] + "\n... [truncated]"
    return s


def _maybe_parse(raw: Any) -> dict[str, Any] | None:
    """Return a dict if ``raw`` is dict / parseable JSON string; else None."""

    if raw is None:
        return None
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, str):
        s = raw.strip()
        if not s:
            return None
        try:
            parsed = json.loads(s)
        except (ValueError, json.JSONDecodeError):
            return None
        return parsed if isinstance(parsed, dict) else None
    if hasattr(raw, "model_dump"):
        try:
            return raw.model_dump(mode="json", by_alias=True)
        except Exception:
            return None
    return None


def _short(text: str, n: int = 140) -> str:
    t = " ".join(str(text or "").split())
    return t if len(t) <= n else t[: n - 3] + "..."


def _registry_rows_from_analyzer(
    analyzer: dict[str, Any] | None, *, scope: str
) -> list[tuple[str, str, str]]:
    """Return ``[(prefixed_id, kind, label/text), ...]`` from one analyzer output."""

    if not analyzer:
        return []
    out: list[tuple[str, str, str]] = []
    for ent in analyzer.get("proposedEntities") or analyzer.get("proposed_entities") or []:
        if not isinstance(ent, dict):
            continue
        eid = str(ent.get("id") or "").strip()
        label = str(ent.get("label") or "").strip()
        if not eid or not label:
            continue
        out.append((f"{scope}_{eid}", "entity", label))
    for node in analyzer.get("proposedNodes") or analyzer.get("proposed_nodes") or []:
        if not isinstance(node, dict):
            continue
        nid = str(node.get("id") or "").strip()
        kind = str(node.get("kind") or "node").strip()
        text = str(node.get("text") or "").strip()
        if not nid or not text:
            continue
        out.append((f"{scope}_{nid}", kind, text))
    return out


def _registry_rows_from_pack(pack: dict[str, Any] | None) -> list[tuple[str, str, str]]:
    """Return ``[(fragment_id, kind, text), ...]`` from session pack ``reasoningGraph.nodes``.

    Pack-level fragment ids are already globally scoped within the session,
    so we keep them un-prefixed (``n1``, ``n10``, ...).
    """

    if not pack:
        return []
    graph = pack.get("reasoningGraph") or pack.get("reasoning_graph") or {}
    if not isinstance(graph, dict):
        return []
    nodes = graph.get("nodes") or []
    if not isinstance(nodes, list):
        return []
    out: list[tuple[str, str, str]] = []
    for node in nodes:
        if not isinstance(node, dict):
            continue
        nid = str(node.get("id") or "").strip()
        kind = str(node.get("kind") or "node").strip()
        text = str(node.get("text") or node.get("label") or "").strip()
        if not nid:
            continue
        out.append((nid, kind, text))
    return out


def _build_registry_block(state: dict[str, Any]) -> str:
    """Assemble a markdown registry table from pack + analyzer outputs in state.

    Scopes:
      * pack fragments → no prefix (``n1`` ... ``n12``)
      * supply_chain analyzer → ``sc_<id>``
      * governance analyzer  → ``gov_<id>``
      * sanctions analyzer   → ``san_<id>``
      * decision_influence analyzer → ``dec_<id>``
    """

    pack = _maybe_parse(state.get("ah_pack_json")) or _maybe_parse(state.get(STATE_PACK_KEY))
    sc = _maybe_parse(state.get("ah_analyzer_json")) or _maybe_parse(state.get(STATE_ANALYZER_OUTPUT_KEY))
    gov = _maybe_parse(state.get("ah_governance_json")) or _maybe_parse(state.get(STATE_GOVERNANCE_OUTPUT_KEY))
    san = _maybe_parse(state.get("ah_sanctions_json")) or _maybe_parse(state.get(STATE_SANCTIONS_OUTPUT_KEY))
    di = _maybe_parse(state.get("ah_decision_influence_json")) or _maybe_parse(
        state.get(STATE_DECISION_INFLUENCE_OUTPUT_KEY)
    )

    rows: list[tuple[str, str, str]] = []
    rows.extend(_registry_rows_from_pack(pack))
    rows.extend(_registry_rows_from_analyzer(sc, scope="sc"))
    rows.extend(_registry_rows_from_analyzer(gov, scope="gov"))
    rows.extend(_registry_rows_from_analyzer(san, scope="san"))
    rows.extend(_registry_rows_from_analyzer(di, scope="dec"))

    if not rows:
        return (
            "(empty — no analyzer entities or pack fragments were available; "
            "findings may use refs: [] only when gapClass starts with methodology.*)"
        )

    seen: set[str] = set()
    deduped: list[tuple[str, str, str]] = []
    for rid, kind, text in rows:
        if rid in seen:
            continue
        seen.add(rid)
        deduped.append((rid, kind, text))
        if len(deduped) >= _REGISTRY_ROW_LIMIT:
            break

    lines = ["| id | kind | label / text |", "|----|------|--------------|"]
    for rid, kind, text in deduped:
        safe_text = _short(text, n=140).replace("|", r"\|")
        lines.append(f"| `{rid}` | {kind} | {safe_text} |")
    if len(rows) > len(deduped):
        lines.append(f"| ... | ... | ({len(rows) - len(deduped)} additional rows truncated) |")
    return "\n".join(lines)


_DOMAIN_FOCUS_HEADER = "## Domain coverage focus (this cycle)\n"

_DOMAIN_HINTS = {
    "supply_chain": (
        "**Supply-chain analyzer ran.** Beyond the generic discipline above, walk these:\n"
        " - Tier-2 chokepoints: for every Tier-1 supplier in `proposedEntities`, is its critical input enumerated? "
        "(EUV → ZEISS SMT optics / Cymer light source / Trumpf laser; advanced node → JSR/Shin-Etsu/TOK photoresist, "
        "Photronics/Toppan masks, SUMCO/GlobalWafers wafers; packaging → Amkor/ASE/SPIL; LNG → liquefaction trains; "
        "lithium → spodumene refiners; rare earths → separation/refining capacity; pharma API → KSM suppliers).\n"
        " - Vessel / corridor mismatch: do `passes_through` / `transships_at` rows respect class limits "
        "(Suezmax→Suez, Q-Max→Cape, Aframax→Baltic/Turkish Straits, Valemax→Cape, Panamax→Panama)?\n"
        " - Incoterm / pricing benchmark gaps: where the question implies tradeable cargo, are PRA benchmarks "
        "(Platts Brent/JKM, Argus Urals/ESPO, OPIS rack) named and tied to flows?\n"
        " - Geographic vs vendor SPOF separation (see SPOF taxonomy in generic discipline)."
    ),
    "governance": (
        "**Governance & control analyzer ran.** Beyond the generic discipline above, walk these:\n"
        " - UBO depth: is the chain followed from operating entity → intermediate holdcos → ultimate "
        "controller (individual / sovereign / family pact)? Bare \"X owns Y\" without the layers above is a "
        "`governance.layering` critical finding.\n"
        " - Jurisdiction layering separation: are **incorporation**, **listing**, **tax residency**, and "
        "**operating footprint** kept distinct, or collapsed into a single \"based in\" claim?\n"
        " - As-of dates on stakes: every percentage in attributes (`stake_pct`) needs an as-of date or deal "
        "context. Numbers without provenance are critical findings (`governance.voting_vs_economic`) when the "
        "question implies a current cap table.\n"
        " - Sub-vehicle enumeration: SPVs, JVs, mergers-of-equals, listed shells, SPACs — did the analyzer "
        "treat related-party / circular structures as first-class, or skip them?\n"
        " - Legal vs de-facto control: disclosed shareholding vs board seats, golden shares, voting "
        "agreements, nominee layers — these should be separate claims, not conflated."
    ),
    "decision_influence": (
        "**Decision & influence analyzer ran.** Beyond the generic discipline above, walk these:\n"
        " - Decision-maker enumeration: for each `decision` fragment, is the **formal authority** named "
        "(specific agency / court / board / committee / minister / regulator), with **jurisdiction** and "
        "**legal basis** distinct from the operating entity it constrains?\n"
        " - Mechanism specificity: every `mechanism` fragment must have `mechanismCategory` (sanction, "
        "license, permit, court_order, tariff, quota, subsidy, advisory, lobbying, signaling, ...) and a "
        "named **issuing authority**. Bare \"the rules\" or \"policy\" is a `decision.opacity` finding.\n"
        " - Hard gates vs soft shaping: are `gatekeeps` (block / approve) and `wields` (controls a "
        "mechanism) kept distinct from `influences` (informal shaping with `influenceKind`)? "
        "Lobbying ≠ regulation; advisory ≠ enforcement; signaling ≠ rule-making.\n"
        " - `enacted_via` chain: when a decision exists, is the `mechanism` that **operationalises** it "
        "named, and is `imposes` (mechanism → constrained target) emitted? Decisions without mechanisms "
        "or mechanisms without targets are critical findings (`decision.dangling`).\n"
        " - Informal-influence completeness: think tanks, family offices, dynasties, ex-officials, "
        "diaspora networks, peer-pressure clubs (G7, OPEC+, BRICS) — were the relevant non-state "
        "influencers covered, or only the formal regulator?\n"
        " - Lifecycle dating: every decision needs `decidedAt` / `effectiveAt` / `expiresAt` where "
        "available; otherwise emit a `decision.timing` advisory finding."
    ),
    "sanctions": (
        "**Sanctions analyzer ran.** Beyond the generic discipline above, walk these:\n"
        " - Designation completeness: for each sanctioned entity, is the **specific list** named "
        "(OFAC SDN vs SSI vs UK OFSI vs EU consolidated vs UN 1718/1737 vs SECO vs SEMA), with **as-of date**?\n"
        " - Regime jurisdictional distinctness (see generic discipline): US BIS / FDPR vs Dutch VEB vs "
        "Japanese METI vs EU dual-use vs UK SECO — do not merge.\n"
        " - Mitigators / licenses: are general licenses, OFAC exemptions, humanitarian carve-outs, "
        "and pending license applications emitted via `mitigated_by` rows, or invisible?\n"
        " - Circumvention enumeration: shadow fleet, STS, flag-hopping, third-country transshipment "
        "(UAE↔RU, TR↔RU, HK↔IR), shell-company layering, dual-hatted brokers — was the relevant subset "
        "addressed, or only the headline designation?\n"
        " - Secondary-sanctions exposure: are non-US counterparties analyzed for indirect exposure "
        "(USD clearing, US-origin tech, US-person involvement), not only primary designations?"
    ),
}


def _domain_focus_block(state: dict[str, Any]) -> str:
    """Dispatch per-domain coverage hints based on which analyzer outputs are non-empty.

    Future-proofing for the eventual critic split: each block here is a self-contained
    module that can be lifted into a domain-specific critic without semantic drift.
    """

    sc = _maybe_parse(state.get("ah_analyzer_json")) or _maybe_parse(state.get(STATE_ANALYZER_OUTPUT_KEY))
    gov = _maybe_parse(state.get("ah_governance_json")) or _maybe_parse(state.get(STATE_GOVERNANCE_OUTPUT_KEY))
    san = _maybe_parse(state.get("ah_sanctions_json")) or _maybe_parse(state.get(STATE_SANCTIONS_OUTPUT_KEY))
    di = _maybe_parse(state.get("ah_decision_influence_json")) or _maybe_parse(
        state.get(STATE_DECISION_INFLUENCE_OUTPUT_KEY)
    )

    def _populated(out: dict[str, Any] | None) -> bool:
        if not out:
            return False
        if any(out.get(k) for k in ("proposedEntities", "proposed_entities", "proposedNodes", "proposed_nodes")):
            return True
        return bool((out.get("chainSummary") or out.get("chain_summary") or "").strip())

    blocks: list[str] = []
    if _populated(sc):
        blocks.append(_DOMAIN_HINTS["supply_chain"])
    if _populated(gov):
        blocks.append(_DOMAIN_HINTS["governance"])
    if _populated(san):
        blocks.append(_DOMAIN_HINTS["sanctions"])
    if _populated(di):
        blocks.append(_DOMAIN_HINTS["decision_influence"])

    if not blocks:
        return (
            _DOMAIN_FOCUS_HEADER
            + "(no analyzer outputs populated this cycle — apply only the generic discipline above)"
        )
    return _DOMAIN_FOCUS_HEADER + "\n\n".join(blocks)


def render_critic_prompt(state: dict[str, Any], *, tools_disabled: bool = True) -> str:
    """Build critic prompt from session state (``_sync_prompt_keys`` / Azure loop)."""
    body = CRITIC_INSTRUCTION.format(
        ah_user_question=str(state.get("ah_user_question") or "").strip() or "(empty)",
        ah_pack_json=str(state.get("ah_pack_json") or _compact(state.get(STATE_PACK_KEY))),
        ah_analyzer_json=str(state.get("ah_analyzer_json") or _compact(state.get(STATE_ANALYZER_OUTPUT_KEY))),
        ah_governance_json=str(state.get("ah_governance_json") or _compact(state.get(STATE_GOVERNANCE_OUTPUT_KEY))),
        ah_sanctions_json=str(state.get("ah_sanctions_json") or _compact(state.get(STATE_SANCTIONS_OUTPUT_KEY))),
        ah_decision_influence_json=str(
            state.get("ah_decision_influence_json")
            or _compact(state.get(STATE_DECISION_INFLUENCE_OUTPUT_KEY))
        ),
        ah_question_plan_json=str(
            state.get("ah_question_plan_json") or _compact(state.get(STATE_QUESTION_PLAN_KEY))
        ),
        ah_critic_registry=_build_registry_block(state),
        ah_critic_domain_focus=_domain_focus_block(state),
    )
    if tools_disabled:
        return _NO_TOOLS_PREAMBLE + "\n\n" + body
    return body

from __future__ import annotations

import os
from typing import cast

from agentloom.contracts import (
    BranchState,
    CandidatePath,
    GraphFragment,
    GraphObservation,
    HypothesisState,
    InternalBrief,
    InvestigationRole,
    LearnedFinding,
)
from agentloom.gap_clustering import cluster_gap_texts


def _truncate(s: str, limit: int = 280) -> str:
    t = s.strip()
    if len(t) <= limit:
        return t
    return t[: limit - 1] + "…"


def _dedupe_preserve_order(values: list[str]) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for v in values:
        v = v.strip()
        if not v or v in seen:
            continue
        seen.add(v)
        out.append(v)
    return out


def _evidence_rank(h: HypothesisState) -> tuple[int, float]:
    order = {"verified": 0, "mixed": 1, "reasoned": 2, "hypothesis": 3}
    return (order.get(h.evidence_status, 9), -h.confidence)


def build_internal_brief(
    question: str,
    selected_roles: list[str],
    working_graph: list[GraphObservation],
    graph_fragments: list[GraphFragment],
    branches: list[BranchState],
    hypotheses: list[HypothesisState],
    candidate_paths: list[CandidatePath],
    learned_findings: list[LearnedFinding],
    open_gap_texts: list[str],
    reachability_report: dict | None,
    *,
    max_bullets_per_axis: int = 8,
    max_finding_digest: int = 6,
    max_entities: int = 24,
) -> InternalBrief:
    rr = reachability_report or {}
    health = str(rr.get("healthSummary") or "").strip()
    broken = rr.get("brokenChains") or []
    broken_n = len(broken) if isinstance(broken, list) else 0
    complete_count = rr.get("completeChainCount")
    if complete_count is None:
        complete_count = len(rr.get("completeChains") or [])
    try:
        complete_int = int(complete_count)
    except (TypeError, ValueError):
        complete_int = 0

    bridge_short: list[str] = []
    for item in rr.get("bridgeCandidates") or []:
        if isinstance(item, dict):
            ent = item.get("entity") or item.get("name") or ""
            issue = item.get("issue") or ""
            if ent:
                bridge_short.append(_truncate(f"{ent}: {issue}".strip(": "), 200))
        elif isinstance(item, str):
            bridge_short.append(_truncate(item, 200))
        if len(bridge_short) >= 6:
            break

    supply_bullets: list[str] = []
    gov_bullets: list[str] = []
    policy_bullets: list[str] = []
    alt_routes: list[str] = []

    hy_sorted = sorted(hypotheses, key=_evidence_rank)
    for h in hy_sorted:
        if len(supply_bullets) >= max_bullets_per_axis and len(gov_bullets) >= max_bullets_per_axis and len(policy_bullets) >= max_bullets_per_axis:
            break
        text = _truncate(h.summary or h.statement, 320)
        if not text:
            continue
        roles = set(h.active_roles)
        if "supply_chain" in roles and len(supply_bullets) < max_bullets_per_axis:
            if h.path_kind in ("alternative", "overview") and text not in alt_routes:
                alt_routes.append(text)
            elif h.path_kind != "alternative" or len(supply_bullets) < max_bullets_per_axis:
                supply_bullets.append(text)
        if "governance_and_control" in roles and len(gov_bullets) < max_bullets_per_axis:
            gov_bullets.append(text)
        if "policy_impact" in roles and len(policy_bullets) < max_bullets_per_axis:
            policy_bullets.append(text)

    # Fill from candidate paths when hypothesis lists are thin
    for path in candidate_paths:
        if path.path_kind in ("alternative", "overview") and len(alt_routes) < max_bullets_per_axis:
            s = _truncate(path.summary or path.title, 320)
            if s and s not in alt_routes:
                alt_routes.append(s)
        text_lower = f"{path.title} {path.summary}".lower()
        bl = _truncate(path.summary or path.title, 320)
        if not bl:
            continue
        if any(k in text_lower for k in ("regulation", "policy", "compliance", "due diligence", "battery regulation")):
            if len(policy_bullets) < max_bullets_per_axis and bl not in policy_bullets:
                policy_bullets.append(bl)
        elif any(k in text_lower for k in ("own", "control", "parent", "subsidiary", "jv ", "joint venture")):
            if len(gov_bullets) < max_bullets_per_axis and bl not in gov_bullets:
                gov_bullets.append(bl)
        elif _path_looks_supply(path) and len(supply_bullets) < max_bullets_per_axis and bl not in supply_bullets:
            supply_bullets.append(bl)

    for frag in graph_fragments:
        line = _truncate((frag.human_summary or frag.title or "").strip(), 320)
        if not line:
            continue
        ft = frag.fragment_type
        if ft == "ownership_overlay" and line not in gov_bullets and len(gov_bullets) < max_bullets_per_axis:
            gov_bullets.append(line)
        elif ft == "policy_overlay" and line not in policy_bullets and len(policy_bullets) < max_bullets_per_axis:
            policy_bullets.append(line)
        elif ft in ("overview_chain", "chain_fragment", "alternative_chain", "hypothesis") and line not in supply_bullets:
            if len(supply_bullets) < max_bullets_per_axis:
                supply_bullets.append(line)

    # Key entities from graph
    entities: list[str] = []
    for obs in working_graph[:200]:
        for part in (obs.subject, obs.object):
            if part and part not in entities:
                entities.append(part)
        if len(entities) >= max_entities:
            break
    if len(entities) < max_entities:
        for frag in graph_fragments:
            for obs in frag.observations[:50]:
                for part in (obs.subject, obs.object):
                    if part and part not in entities:
                        entities.append(part)
                    if len(entities) >= max_entities:
                        break

    finding_digest: list[str] = []
    ranked_findings = sorted(
        learned_findings,
        key=lambda f: ({"verified": 0, "mixed": 1, "reasoned": 2, "hypothesis": 3}.get(f.evidence_status, 9), -f.confidence),
    )
    for f in ranked_findings[:max_finding_digest]:
        ent = ", ".join(f.related_entities[:4]) if f.related_entities else ""
        line = _truncate(f.summary + (f" ({ent})" if ent else ""), 300)
        if line:
            finding_digest.append(line)

    caveats: list[str] = []
    if complete_int == 0 and health:
        caveats.append("No fully grounded end-to-end operational chain was confirmed in the working graph.")
    hypo_obs = sum(1 for o in working_graph if o.support_type == "hypothesis")
    if hypo_obs > 0:
        caveats.append("Some edges rely on hypothesis-level or model knowledge and are not document-grounded.")
    if open_gap_texts:
        caveats.append("Public procurement and direct supplier-to-plant links may be absent from open sources.")

    use_emb = os.getenv("AGENTLOOM_GAP_CLUSTER_EMBEDDINGS", "").strip().lower() in ("1", "true", "on", "yes")
    clustered = cluster_gap_texts(open_gap_texts, prefer_embedding_clustering=use_emb)

    role_literals: list[InvestigationRole] = [
        cast(InvestigationRole, r)
        for r in selected_roles
        if r in ("supply_chain", "governance_and_control", "policy_impact")
    ]

    reach_parts: list[str] = []
    if health:
        reach_parts.append(health)
    reach_parts.append(f"completeChains~{complete_int}")
    if broken_n:
        reach_parts.append(f"brokenChainReports~{broken_n}")

    return InternalBrief(
        questionOneLiner=_truncate(question, 400),
        selectedRoles=role_literals,
        reachabilityOneLiner=_truncate("; ".join(reach_parts), 500),
        completeChainCount=complete_int,
        bridgeCandidatesShort=_dedupe_preserve_order(bridge_short)[:8],
        supplyChainNarrativeBullets=_dedupe_preserve_order(supply_bullets)[:max_bullets_per_axis],
        governanceBullets=_dedupe_preserve_order(gov_bullets)[:max_bullets_per_axis],
        policyBullets=_dedupe_preserve_order(policy_bullets)[:max_bullets_per_axis],
        alternativeRouteFamilies=_dedupe_preserve_order(alt_routes)[:max_bullets_per_axis],
        keyEntities=entities[:max_entities],
        clusteredGaps=clustered,
        learnedFindingsDigest=finding_digest,
        epistemicCaveats=_dedupe_preserve_order(caveats),
    )


def _path_looks_supply(path: CandidatePath) -> bool:
    t = f"{path.title} {path.summary}".lower()
    if any(x in t for x in ("ownership", "owns", "control", "regulation", "policy", "compliance")):
        return False
    return any(
        x in t
        for x in (
            "supply",
            "supplier",
            "route",
            "refiner",
            "refinery",
            "battery",
            "facility",
            "transform",
            "product",
            "upstream",
            "downstream",
            "midstream",
        )
    )

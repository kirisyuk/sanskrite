"""Phase 6: metric structuring (LLM + deterministic attach)."""

from __future__ import annotations

import json
from typing import Any

from agenthorde.canonicalizer.llm_client import LLMPhaseClient
from agenthorde.canonicalizer.metrics_normalize import normalize_metric_value, score_edge_for_ownership_metric
from agenthorde.canonicalizer.phases.phase0_input import WorkingState
from agenthorde.canonicalizer.schemas import CanonicalEdge, CanonicalEntity, CanonicalFragment, MetricValue, Provenance


def _parse_numeric(val: Any) -> float | None:
    if val is None:
        return None
    if isinstance(val, (int, float)):
        return float(val)
    s = str(val).strip().replace(",", "")
    if not s:
        return None
    if s.endswith("%"):
        s = s[:-1].strip()
    try:
        return float(s)
    except ValueError:
        return None


_OWNERSHIP_METRIC_NAMES = frozenset(
    (
        "stake_pct",
        "pct",
        "ownership_pct",
        "ownership_percent",
        "stake_percent",
    )
)


async def run_phase6(
    entities: list[CanonicalEntity],
    fragments: list[CanonicalFragment],
    edges: list[CanonicalEdge],
    ws: WorkingState,
    llm: LLMPhaseClient,
    model: str,
    run_id: str,
    version: str,
    entity_to_fragment: dict[str, str],
) -> None:
    user = json.dumps(
        {
            "claims": [{"id": n.id, "text": n.text} for n in ws.claim_nodes],
            "entities": [{"entity_id": e.entity_id, "path": e.canonical_path} for e in entities],
        },
        ensure_ascii=False,
    )
    raw = await llm.generate_json("6", model, user)
    frag_by_id = {f.id: f for f in fragments}
    for m in raw.get("metrics") or []:
        if not isinstance(m, dict):
            continue
        name = str(m.get("metric_name") or m.get("metricName") or "").strip()
        if not name:
            continue
        raw_val = m.get("value")
        parsed = _parse_numeric(raw_val)
        if parsed is None:
            continue
        unit = str(m.get("unit") or "")
        conf = float(m.get("confidence") or 0.7)
        cids = [str(x) for x in (m.get("claim_ids") or m.get("claimIds") or [])]
        eid = str(m.get("entity_id") or m.get("entityId") or "")
        directional = bool(m.get("directional"))
        v_norm, u_norm, kind_str = normalize_metric_value(parsed, unit, directional=directional)
        fid = entity_to_fragment.get(eid)
        mv = MetricValue(
            value=v_norm,
            unit=u_norm,
            confidence=conf,
            provenance=Provenance(run_id=run_id, canonicalizer_version=version, claim_ids=cids),
            kind="directional" if kind_str == "directional" else "magnitude",
        )
        hint = {entity_to_fragment[eid]} if eid and entity_to_fragment.get(eid) else set()
        if fid and fid in frag_by_id:
            frag_by_id[fid].metrics[name] = mv
        elif name in _OWNERSHIP_METRIC_NAMES or "pct" in name.lower() or "ownership" in name.lower():
            best: CanonicalEdge | None = None
            best_score = 0
            for e in edges:
                if e.relation == "produces":
                    continue
                sc = score_edge_for_ownership_metric(e, hint)
                if sc > best_score:
                    best_score = sc
                    best = e
            if best is not None:
                best.metrics[name] = mv
                try:
                    best.attributes.setdefault("pct", float(v_norm))
                except (TypeError, ValueError):
                    pass

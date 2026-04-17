"""
Phase 7: evidence layer.

Architecture note
-----------------
Reasoning anchors (claims, counters, hypotheses) and source documents both live in
an audit/archive substrate separate from the structural graph:

- Graph (`fragments`, `edges`): only business fragments and structural edges.
  No /evidence/* fragments. No `supported_by`, `contradicts`, or `under_hypothesis`
  edges. Business edges already reference supporting claims via
  `CanonicalEdge.source_claim_ids`, so the join stays recoverable.

- Evidence archive (`EvidenceArchive`):
  * `sources`           — one `EvidenceSource` per URL returned by web_search.
  * `links`             — claim_id → source_id join rows (`asserted_by` top-1,
                          `supported_by` fan-out above the score threshold).
  * `claims`            — claim anchors (id, text, hypothesis_id).
  * `hypotheses`        — hypothesis anchors (id, text).
  * `counters`          — counter-claim anchors with `contradicts_claim_id`.

Business fragments live in the graph substrate (Neo4j / property graph). The
archive maps naturally to GCS (raw content) + a metadata store (BigQuery /
Firestore) holding the five lists here.
"""

from __future__ import annotations

import hashlib
from typing import Any

from rapidfuzz import fuzz

from agenthorde.canonicalizer.phases.phase0_input import WorkingState
from agenthorde.config import (
    CANONICALIZER_EVIDENCE_FALLBACK_MIN_SCORE,
    CANONICALIZER_EVIDENCE_FANOUT_MAX,
    CANONICALIZER_EVIDENCE_FANOUT_MIN_SCORE,
)
from agenthorde.canonicalizer.schemas import (
    CanonicalEdge,
    CanonicalFragment,
    EvidenceArchive,
    EvidenceClaim,
    EvidenceCounter,
    EvidenceHypothesis,
    EvidenceLink,
    EvidenceSource,
    SourceRef,
)


def _url_hash(url: str) -> str:
    return hashlib.sha256(url.encode("utf-8")).hexdigest()[:16]


def _is_hex16(s: str) -> bool:
    if len(s) != 16:
        return False
    try:
        int(s, 16)
    except ValueError:
        return False
    return True


def _resolve_direct_ref(ref: str, known_source_ids: set[str]) -> str | None:
    """Normalize a proposedNodes[i].evidenceRefs entry into a known source id.

    - URLs → sha16(url); kept if archive has a matching source (always will, because
      archive is built from the same tool_ledger the analyzer cited).
    - 16-hex → treated as already-hashed id; kept if present in archive.
    - "prior_knowledge" / empty → None (skip, do not create link).
    """
    r = (ref or "").strip()
    if not r or r.lower() == "prior_knowledge":
        return None
    if r.startswith("http://") or r.startswith("https://"):
        sid = _url_hash(r)
        return sid if sid in known_source_ids else None
    if _is_hex16(r):
        return r if r in known_source_ids else None
    return None


def _collect_ledger_entries(tool_ledger: list[dict[str, Any]]) -> list[tuple[str, list[SourceRef]]]:
    out: list[tuple[str, list[SourceRef]]] = []
    for entry in tool_ledger or []:
        if not isinstance(entry, dict) or entry.get("tool") != "web_search" or not entry.get("ok"):
            continue
        payload = entry.get("payload") or {}
        answer = str(payload.get("answer") or "")
        refs: list[SourceRef] = []
        for s in payload.get("sources") or []:
            if not isinstance(s, dict):
                continue
            url = str(s.get("url") or "").strip()
            if not url:
                continue
            refs.append(
                SourceRef(
                    url=url,
                    title=str(s.get("title") or ""),
                    domain=str(s.get("domain") or "").strip() or "unknown",
                )
            )
        if answer and refs:
            out.append((answer, refs))
    return out


def _best_source_id_for_claim(
    claim_text: str,
    ledger_entries: list[tuple[str, list[SourceRef]]],
) -> tuple[str, int] | None:
    """Return (source_id, score) for the best matching source, or None."""
    ct = (claim_text or "").strip().lower()
    if not ct:
        return None
    best = 0
    best_url = ""
    best_key = ""
    for answer, refs in ledger_entries:
        sc = int(fuzz.partial_ratio(ct, answer.lower()))
        if not refs:
            continue
        key = refs[0].url
        if sc > best or (sc == best and best_url and key < best_key):
            best = sc
            best_url = refs[0].url
            best_key = key
    if not best_url:
        return None
    return _url_hash(best_url), best


def _ranked_supporting_source_ids(
    claim_text: str,
    ledger_entries: list[tuple[str, list[SourceRef]]],
    *,
    min_score: int,
    max_per_claim: int,
) -> list[tuple[str, int]]:
    """Return [(source_id, score), ...] above min_score, sorted by -score then URL."""
    ct = (claim_text or "").strip().lower()
    if not ct:
        return []
    per_url: dict[str, tuple[int, str]] = {}  # url -> (score, url)
    for answer, refs in ledger_entries:
        sc = int(fuzz.partial_ratio(ct, answer.lower()))
        if sc < min_score:
            continue
        for r in refs:
            prev = per_url.get(r.url)
            if prev is None or sc > prev[0]:
                per_url[r.url] = (sc, r.url)
    ranked = sorted(per_url.values(), key=lambda x: (-x[0], x[1]))[:max_per_claim]
    return [(_url_hash(url), sc) for sc, url in ranked]


def run_phase7(
    tool_ledger: list[dict[str, Any]],
    fragments: list[CanonicalFragment],
    edges: list[CanonicalEdge],
    ws: WorkingState,
    run_id: str,
    version: str,
) -> EvidenceArchive:
    """
    Populate the evidence archive: sources, claim/hypothesis/counter anchors, and
    claim-source links. No graph fragments or edges are created here.
    """
    archive = EvidenceArchive()

    # --- 1. Evidence sources (archive.sources) — one record per URL.
    for entry in tool_ledger or []:
        if not isinstance(entry, dict):
            continue
        if entry.get("tool") != "web_search" or not entry.get("ok"):
            continue
        payload = entry.get("payload") or {}
        for s in payload.get("sources") or []:
            if not isinstance(s, dict):
                continue
            url = str(s.get("url") or "").strip()
            if not url:
                continue
            dom = str(s.get("domain") or "").strip() or "unknown"
            title = str(s.get("title") or "")
            sid = _url_hash(url)
            archive.sources.append(
                EvidenceSource(id=sid, url=url, domain=dom, title=title)
            )
    seen_sources: set[str] = set()
    uniq_sources: list[EvidenceSource] = []
    for src in archive.sources:
        if src.id in seen_sources:
            continue
        seen_sources.add(src.id)
        uniq_sources.append(src)
    archive.sources = uniq_sources

    known_source_ids = {s.id for s in archive.sources}
    ledger_entries = _collect_ledger_entries(tool_ledger)
    claim_node_by_id = {n.id: n for n in ws.claim_nodes}

    # Union of claim ids referenced by any business edge or metric + all claim nodes.
    claim_ids_used: set[str] = set()
    for e in edges:
        claim_ids_used.update(e.source_claim_ids)
    for f in fragments:
        for _k, mv in f.metrics.items():
            if mv.provenance:
                claim_ids_used.update(mv.provenance.claim_ids)
    needed_claim_ids = set(claim_ids_used) | {n.id for n in ws.claim_nodes}

    # --- 2. Claim anchors in the archive.
    for cid in sorted(needed_claim_ids):
        node = claim_node_by_id.get(cid)
        claim_text = node.text if node else ""
        hyp_id = (node.hypothesis_id or None) if node else None
        archive.claims.append(
            EvidenceClaim(id=cid, text=claim_text, hypothesis_id=hyp_id)
        )

        if node is None:
            continue

        # --- 3. Claim → source links for archive.
        already_linked_sources: set[str] = set()
        refs_raw = list(node.evidence_refs or [])
        analyzer_declared_refs = bool(refs_raw)
        direct_sids: list[str] = []
        for ref in refs_raw:
            sid = _resolve_direct_ref(str(ref), known_source_ids)
            if sid is None or sid in already_linked_sources:
                continue
            direct_sids.append(sid)
            already_linked_sources.add(sid)

        if direct_sids:
            primary_sid = direct_sids[0]
            archive.links.append(
                EvidenceLink(
                    claim_id=cid,
                    source_id=primary_sid,
                    relation="asserted_by",
                    score=100,
                    rank=0,
                )
            )
            for rank_i, sid in enumerate(direct_sids[1:], start=1):
                archive.links.append(
                    EvidenceLink(
                        claim_id=cid,
                        source_id=sid,
                        relation="supported_by",
                        score=100,
                        rank=rank_i,
                    )
                )
        elif analyzer_declared_refs:
            # Analyzer declared refs (e.g. prior_knowledge) — respect and skip fuzzy.
            pass
        else:
            best = _best_source_id_for_claim(node.text or "", ledger_entries)
            if best is not None:
                sid, sc = best
                already_linked_sources.add(sid)
                archive.links.append(
                    EvidenceLink(
                        claim_id=cid,
                        source_id=sid,
                        relation="asserted_by",
                        score=sc,
                        rank=0,
                    )
                )
            fanout_min = (
                CANONICALIZER_EVIDENCE_FALLBACK_MIN_SCORE
                if best is None
                else CANONICALIZER_EVIDENCE_FANOUT_MIN_SCORE
            )
            ranked = _ranked_supporting_source_ids(
                node.text or "",
                ledger_entries,
                min_score=fanout_min,
                max_per_claim=CANONICALIZER_EVIDENCE_FANOUT_MAX,
            )
            rank = 1
            for sid, sc in ranked:
                if sid in already_linked_sources:
                    continue
                already_linked_sources.add(sid)
                archive.links.append(
                    EvidenceLink(
                        claim_id=cid,
                        source_id=sid,
                        relation="supported_by",
                        score=sc,
                        rank=rank,
                    )
                )
                rank += 1

    # --- 4. Hypothesis anchors (archive-only).
    hyp_texts: dict[str, str] = {}
    for n in ws.claim_nodes:
        hid = (n.hypothesis_id or "").strip()
        if hid and hid not in hyp_texts:
            hyp_texts[hid] = ""
    for n in ws.hypothesis_nodes:
        hid = (n.hypothesis_id or "").strip() or n.id
        hyp_texts[hid] = n.text or hyp_texts.get(hid, "")
    for hid in sorted(hyp_texts):
        archive.hypotheses.append(
            EvidenceHypothesis(id=hid, text=hyp_texts[hid])
        )

    # --- 5. Counter anchors (archive-only): each counter rebuts the most-recent
    # preceding claim in the reasoning-graph order.
    pos = {n.id: i for i, n in enumerate(ws.all_nodes)}
    for counter in ws.counter_nodes:
        i = pos.get(counter.id)
        prev_claim_id: str | None = None
        if i is not None:
            for j in range(i - 1, -1, -1):
                if ws.all_nodes[j].kind == "claim":
                    prev_claim_id = ws.all_nodes[j].id
                    break
        archive.counters.append(
            EvidenceCounter(
                id=counter.id,
                text=counter.text or "",
                contradicts_claim_id=prev_claim_id,
            )
        )

    return archive

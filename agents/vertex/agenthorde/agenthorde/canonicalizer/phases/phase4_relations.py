"""Phase 4: LLM relation extraction between resolved entities."""

from __future__ import annotations

import json
import uuid

from typing import Any

from agenthorde.canonicalizer.llm_client import LLMPhaseClient
from agenthorde.canonicalizer.phases.phase0_input import WorkingState
from agenthorde.canonicalizer.schemas import (
    CanonicalEdge,
    CanonicalEntity,
    Candidates,
    Provenance,
    UnknownProductCandidate,
    UnknownRelationCandidate,
)
from agenthorde.canonicalizer.slug import normalize_surface, slugify
from agenthorde.canonicalizer.taxonomy import (
    RELATION_ALLOWED_LAYERS,
    RELATION_LAYER_SECTION,
    RELATION_NAMES,
    canonicalize_llm_synonym,
    default_layer_for_relation,
    is_forbidden_endpoint_pair,
    relation_kind_for_endpoints,
    repair_relation,
    should_swap_endpoints,
)

# Same namespace as pipeline._build_fragments for deterministic fragment ids.
_FRAGMENT_UUID_NS = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")


def _product_fragment_id(canonical_path: str) -> str:
    return str(uuid.uuid5(_FRAGMENT_UUID_NS, canonical_path))


def _resolve_supplies_product(raw: str, entities: list[CanonicalEntity]) -> CanonicalEntity | None:
    """Match LLM cargo label to a product entity (path or surface)."""
    rp = (raw or "").strip()
    if not rp:
        return None
    products = [e for e in entities if e.type == "product"]
    rpn = rp.lower().rstrip("/")
    if rp.startswith("/product/"):
        for e in products:
            if e.canonical_path.lower().rstrip("/") == rpn:
                return e
    ns = normalize_surface(rp)
    for e in products:
        for sf in e.surface_forms:
            if normalize_surface(sf) == ns:
                return e
        slug = slugify(ns)
        parts = [x for x in e.canonical_path.split("/") if x]
        if len(parts) >= 3 and parts[-1] == slug:
            return e
    return None

# Cap on context excerpts included in the Phase 4 LLM payload to bound prompt size.
_FINAL_ANSWER_CAP = 8000
_CHAIN_SUMMARY_CAP = 2000
_WORKING_GRAPH_CAP = 8000


def _inflect_variants(surface: str) -> list[str]:
    """Generate light singular/plural variants to bridge analyzer vs. phase1 surface drift.

    Example: analyzer writes "VLCC" in proposedRelations but phase1 extracted the
    vessel class as surface "VLCCs". We still want to resolve the same entity.
    """
    s = (surface or "").strip()
    if not s:
        return []
    variants = {s}
    low = s.lower()
    if low.endswith("es") and len(low) > 3:
        variants.add(s[:-2])
    if low.endswith("s") and len(low) > 2:
        variants.add(s[:-1])
    if not low.endswith("s"):
        variants.add(s + "s")
    return list(variants)


def _resolve_surface_to_entity(
    surface: str, entities: list[CanonicalEntity]
) -> CanonicalEntity | None:
    """Surface-label lookup used by the proposedRelations pre-pass."""
    s = (surface or "").strip()
    if not s:
        return None
    if s.startswith("/"):
        key = s.lower().rstrip("/")
        for e in entities:
            if e.canonical_path.lower().rstrip("/") == key:
                return e
    ns = normalize_surface(s)
    for e in entities:
        for sf in e.surface_forms:
            if normalize_surface(sf) == ns:
                return e
    # Singular/plural fallback — mostly rescues vessel_class surface drift
    # (VLCC vs VLCCs, Valemax vs Valemaxes). Inflect BOTH sides so we catch
    # "query singular vs entity plural" and vice versa.
    query_variants = {normalize_surface(v) for v in _inflect_variants(s)}
    for e in entities:
        for sf in e.surface_forms:
            ent_variants = {normalize_surface(v) for v in _inflect_variants(sf)}
            if query_variants & ent_variants:
                return e
    return None


def _build_edge_from_raw(
    r: dict[str, Any],
    entities: list[CanonicalEntity],
    eids: set[str],
    etype: dict[str, str],
    candidates: Candidates,
    run_id: str,
    version: str,
) -> CanonicalEdge | None:
    """Apply the full vocabulary/endpoint/supplies-cargo pipeline to one raw relation row.

    Shared by the analyzer pre-pass and the LLM output path so both produce identical
    edges (same synonym mapping, endpoint swap, cargo resolution, layer defaults).
    """
    rel_raw = str(r.get("relation") or "").strip()
    fid = str(r.get("from_id") or r.get("fromId") or "")
    tid = str(r.get("to_id") or r.get("toId") or "")
    ft = etype.get(fid, "")
    tt = etype.get(tid, "")

    synonym_note: str | None = None
    rel = rel_raw
    if rel not in RELATION_NAMES:
        canon, invert, syn_note = canonicalize_llm_synonym(rel_raw, ft, tt)
        if canon is None:
            candidates.unknown_relations.append(
                UnknownRelationCandidate(
                    relation=rel_raw or "unknown",
                    from_id=fid,
                    to_id=tid,
                    reason="unknown_relation",
                )
            )
            return None
        rel = canon
        synonym_note = syn_note
        if invert:
            fid, tid = tid, fid
            ft, tt = tt, ft

    if fid not in eids or tid not in eids:
        return None

    if should_swap_endpoints(rel, ft, tt):
        fid, tid = tid, fid
        ft, tt = tt, ft

    new_rel, note = repair_relation(rel, ft, tt)
    rel = new_rel

    # Three-tier endpoint check at edge-build time:
    #   forbidden   -> record candidate, drop the edge entirely.
    #   recognized  -> stamp attributes.kind with the projection label.
    #   unspecified -> stamp attributes.kind="unspecified" and record candidate
    #                  so the pair can be reviewed for catalog promotion later.
    endpoint_kind: str | None
    if is_forbidden_endpoint_pair(rel, ft, tt):
        candidates.unknown_relations.append(
            UnknownRelationCandidate(
                relation=rel,
                from_id=fid,
                to_id=tid,
                reason="forbidden_endpoint_pair",
            )
        )
        return None
    endpoint_kind = relation_kind_for_endpoints(rel, ft, tt)
    if endpoint_kind is None:
        candidates.unknown_relations.append(
            UnknownRelationCandidate(
                relation=rel,
                from_id=fid,
                to_id=tid,
                reason="endpoint_pair_not_in_catalog",
            )
        )

    # Layer must be in the per-relation allowed set. The LLM and analyzer
    # often hand us a best-effort "structural" for every edge, which violates
    # relations like `benchmarked_to` (contractual-only) or `charters`
    # (contractual/flow). Clamp to the canonical default when the supplied
    # layer isn't allowed, rather than silently producing a layer_error.
    layer_raw = str(r.get("layer") or "").strip()
    allowed_layers = RELATION_ALLOWED_LAYERS.get(rel, frozenset(("structural",)))
    if layer_raw and layer_raw in allowed_layers:
        layer = layer_raw
    else:
        layer = default_layer_for_relation(rel)
    ls = str(
        r.get("layer_section")
        or r.get("layerSection")
        or RELATION_LAYER_SECTION.get(rel, "flow_topology")
    )
    if rel == "produces":
        ls = "catalog"
    attrs_in = r.get("attributes") if isinstance(r.get("attributes"), dict) else {}
    attrs: dict[str, Any] = dict(attrs_in or {})
    attrs["kind"] = endpoint_kind or "unspecified"

    if rel == "supplies":
        raw_p = str(attrs.get("product") or attrs.get("productPath") or "").strip()
        if not raw_p:
            candidates.unknown_products.append(
                UnknownProductCandidate(
                    surface="",
                    from_site_fragment_id=fid,
                    to_site_fragment_id=tid,
                    reason="missing_cargo",
                )
            )
            return None
        ent_p = _resolve_supplies_product(raw_p, entities)
        if ent_p is None:
            candidates.unknown_products.append(
                UnknownProductCandidate(
                    surface=raw_p,
                    from_site_fragment_id=fid,
                    to_site_fragment_id=tid,
                    reason="unresolved_cargo",
                )
            )
            return None
        attrs["product_fragment_id"] = _product_fragment_id(ent_p.canonical_path)
        attrs["product"] = ent_p.canonical_path

    as_of = r.get("as_of") or r.get("asOf")
    if as_of is not None and as_of != "":
        attrs["asOf"] = as_of

    qualifiers: dict[str, object] = {}
    em = r.get("edge_modality") or r.get("edgeModality")
    if em is not None and em != "":
        qualifiers["edge_modality"] = em
    proj = r.get("projected")
    if proj is not None:
        qualifiers["projected"] = proj
    if note:
        qualifiers["relationRepair"] = note
    if synonym_note:
        qualifiers["relationSynonym"] = synonym_note
    origin = r.get("origin")
    if origin:
        qualifiers["origin"] = str(origin)

    vf = r.get("valid_from") or r.get("validFrom")
    vt = r.get("valid_to") or r.get("validTo")
    vf_s = str(vf).strip() if vf not in (None, "") else None
    vt_s = str(vt).strip() if vt not in (None, "") else None

    claim_ids = [str(x) for x in (r.get("source_claim_ids") or r.get("sourceClaimIds") or [])]
    prov = Provenance(
        run_id=run_id,
        canonicalizer_version=version,
        claim_ids=list(claim_ids),
    )
    return CanonicalEdge(
        id=str(uuid.uuid4()),
        source_fragment_id=fid,
        target_fragment_id=tid,
        relation=rel,
        layer=layer if layer in ("structural", "contractual", "flow") else "structural",  # type: ignore[arg-type]
        layer_section=ls,  # type: ignore[arg-type]
        attributes=attrs,
        qualifiers=qualifiers,
        provenance=prov,
        source_claim_ids=list(claim_ids),
        confidence=float(r.get("per_claim_confidence") or r.get("perClaimConfidence") or 0.7),
        valid_from=vf_s,
        valid_to=vt_s,
    )


def _edges_from_proposed_relations(
    analyzer_relations: list[dict[str, Any]],
    entities: list[CanonicalEntity],
    candidates: Candidates,
    run_id: str,
    version: str,
) -> list[CanonicalEdge]:
    """Resolve analyzer-provided (src, dst, relation) surface rows to CanonicalEdges."""
    if not analyzer_relations:
        return []
    eids = {e.entity_id for e in entities}
    etype = {e.entity_id: e.type for e in entities}
    out: list[CanonicalEdge] = []
    for pr in analyzer_relations:
        if not isinstance(pr, dict):
            continue
        src_surface = str(pr.get("src") or "").strip()
        dst_surface = str(pr.get("dst") or "").strip()
        relation = str(pr.get("relation") or "").strip()
        if not src_surface or not dst_surface or not relation:
            continue
        ent_src = _resolve_surface_to_entity(src_surface, entities)
        ent_dst = _resolve_surface_to_entity(dst_surface, entities)
        if ent_src is None or ent_dst is None:
            candidates.unknown_relations.append(
                UnknownRelationCandidate(
                    relation=relation,
                    from_id=ent_src.entity_id if ent_src else src_surface,
                    to_id=ent_dst.entity_id if ent_dst else dst_surface,
                    reason="unresolved_endpoint_surface",
                )
            )
            continue
        row = {
            "relation": relation,
            "from_id": ent_src.entity_id,
            "to_id": ent_dst.entity_id,
            "attributes": dict(pr.get("attributes") or {}),
            "source_claim_ids": list(pr.get("sourceClaimIds") or pr.get("source_claim_ids") or []),
            "origin": "analyzer_proposed",
        }
        edge = _build_edge_from_raw(row, entities, eids, etype, candidates, run_id, version)
        if edge is not None:
            out.append(edge)
    return out


def _edge_dedup_key(e: CanonicalEdge) -> tuple[str, str, str, str, str]:
    # Include product_fragment_id for supplies so multi-cargo edges between same sites
    # don't collapse. Empty disambiguator for other relations.
    disambiguator = ""
    if e.relation == "supplies":
        disambiguator = str((e.attributes or {}).get("product_fragment_id") or "")
    return (e.source_fragment_id, e.target_fragment_id, e.relation, e.layer_section, disambiguator)


async def run_phase4(
    entities: list[CanonicalEntity],
    ws: WorkingState,
    llm: LLMPhaseClient,
    model: str,
    candidates: Candidates,
    run_id: str,
    version: str,
) -> list[CanonicalEdge]:
    eids = {e.entity_id for e in entities}
    etype = {e.entity_id: e.type for e in entities}

    # Pre-pass: fold analyzer proposedRelations into edges before the LLM call so the
    # LLM can focus on what the analyzer missed.
    pre_edges = _edges_from_proposed_relations(
        getattr(ws, "analyzer_relations", []) or [], entities, candidates, run_id, version
    )
    edges: list[CanonicalEdge] = list(pre_edges)
    edge_keys: set[tuple[str, str, str, str]] = {_edge_dedup_key(e) for e in pre_edges}

    known_edges_payload = [
        {
            "from_id": e.source_fragment_id,
            "to_id": e.target_fragment_id,
            "relation": e.relation,
            "layer_section": e.layer_section,
        }
        for e in pre_edges
    ]

    ent_payload = [
        {
            "entity_id": e.entity_id,
            "type": e.type,
            "subtype": e.subtype,
            "canonical_path": e.canonical_path,
            "surface_forms": e.surface_forms,
        }
        for e in entities
    ]
    claims_payload: list[dict[str, str | None]] = []
    for n in ws.claim_nodes:
        claims_payload.append({"id": n.id, "kind": "claim", "text": n.text, "hypothesisId": n.hypothesis_id})
    for n in ws.counter_nodes:
        claims_payload.append({"id": n.id, "kind": "counter", "text": n.text, "hypothesisId": n.hypothesis_id})
    for n in ws.hypothesis_nodes:
        claims_payload.append({"id": n.id, "kind": "hypothesis_anchor", "text": n.text, "hypothesisId": n.hypothesis_id})

    # Full context: the LLM needs more than just explicit claims to cover non-ownership
    # families (incorporated_in, listed_on, operates, holds_board_seat, ...).
    final_answer_excerpt = (ws.final_answer or "")[:_FINAL_ANSWER_CAP]
    chain_summaries: list[str] = []
    for cs in ws.chain_summaries or []:
        if not cs:
            continue
        chain_summaries.append(cs[:_CHAIN_SUMMARY_CAP])
    working_graph_excerpt = (ws.working_graph_text or "")[:_WORKING_GRAPH_CAP]

    payload = {
        "entities": ent_payload,
        "claims": claims_payload,
        "final_answer_excerpt": final_answer_excerpt,
        "chain_summaries": chain_summaries,
        "working_graph_excerpt": working_graph_excerpt,
        "known_edges": known_edges_payload,
    }
    user = json.dumps(payload, ensure_ascii=False)
    raw = await llm.generate_json("4", model, user)

    for r in raw.get("relations") or []:
        if not isinstance(r, dict):
            continue
        edge = _build_edge_from_raw(r, entities, eids, etype, candidates, run_id, version)
        if edge is None:
            continue
        key = _edge_dedup_key(edge)
        if key in edge_keys:
            continue
        edge_keys.add(key)
        edges.append(edge)
    return edges

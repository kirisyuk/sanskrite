"""Follow-up plan builder.

Scope:
- Fragments that came out of canonicalization but remain structurally isolated
  (orphan_nodes) are surfaced as ``aggregate_mentions`` with a best-effort
  ``reason``. Downstream agents can expand them in a targeted follow-up run.
- Unresolved candidate collections (products, relations, transforms) become
  ``deferred_research`` items with a family tag so the supervisor can pick a
  specialized agent next time.
- ``known_anchors`` is a compact handoff list carrying canonical_path,
  fragment_class, and the top surface forms. It is meant to be seeded into the
  next run's user question so the analyzers don't re-discover them.

This module is intentionally pure: no Firestore or LLM calls, deterministic.
"""

from __future__ import annotations

from typing import Any

from agenthorde_common.canonicalizer.schemas import (
    CanonicalEdge,
    CanonicalFragment,
    Candidates,
    ValidationReport,
)

_KNOWN_ANCHOR_SURFACE_LIMIT = 4
_KNOWN_ANCHOR_LIMIT = 64
_AGGREGATE_LIMIT = 32
_DEFERRED_LIMIT = 32


def _fragment_by_id(fragments: list[CanonicalFragment]) -> dict[str, CanonicalFragment]:
    return {f.id: f for f in fragments}


def _aggregate_mentions(
    fragments: list[CanonicalFragment],
    report: ValidationReport,
) -> list[dict[str, Any]]:
    """One row per orphan fragment; best-effort reason comes from validation."""
    by_id = _fragment_by_id(fragments)
    orphan_ids = list(report.orphan_nodes or [])
    out: list[dict[str, Any]] = []
    for fid in orphan_ids[:_AGGREGATE_LIMIT]:
        frag = by_id.get(fid)
        if frag is None:
            continue
        out.append(
            {
                "fragmentId": fid,
                "canonicalPath": frag.canonical_path,
                "fragmentClass": frag.fragment_class,
                "surfaceForms": list(frag.surface_forms)[:_KNOWN_ANCHOR_SURFACE_LIMIT],
                "reason": "orphan_no_structural_edge",
                "status": "aggregate_mention",
            }
        )
    return out


def _deferred_research(
    candidates: Candidates,
    report: ValidationReport,
) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    for u in candidates.unknown_products[:_DEFERRED_LIMIT]:
        items.append(
            {
                "family": "product_dictionary",
                "reason": u.reason or "unresolved_cargo_on_supplies_edge",
                "surface": u.surface,
                "suggestedAgent": "supply_chain_analyzer",
            }
        )
    for ur in candidates.unknown_relations[:_DEFERRED_LIMIT]:
        items.append(
            {
                "family": "relation_vocabulary",
                "reason": ur.reason or "relation_not_in_core_vocabulary",
                "relation": ur.relation,
                "suggestedAgent": "supply_chain_analyzer",
            }
        )
    for ut in candidates.unknown_transforms[:_DEFERRED_LIMIT]:
        items.append(
            {
                "family": "transform_dictionary",
                "reason": "unknown_product_to_product_transform",
                "fromProduct": ut.from_product,
                "toProduct": ut.to_product,
                "suggestedAgent": "supply_chain_analyzer",
            }
        )
    for um in candidates.merge_candidates[:_DEFERRED_LIMIT]:
        items.append(
            {
                "family": "synonym_dictionary",
                "reason": um.reason or "fuzzy_merge_candidate",
                "surfaces": list(um.surfaces),
                "suggestedAgent": "governance_control_analyzer",
            }
        )
    for rc in (candidates.registry_coverage_issues or [])[:_DEFERRED_LIMIT]:
        items.append(
            {
                "family": "entity_registry",
                "reason": rc.reason or "registry_coverage_gap",
                "agentRegistryId": rc.agent_registry_id,
                "label": rc.label,
                "suggestedAgent": "supply_chain_analyzer",
            }
        )
    # Dictionary warnings often hint at Incoterms/sanctions surfaces that slipped.
    for warn in (report.dictionary_warnings or [])[:_DEFERRED_LIMIT]:
        items.append(
            {
                "family": "dictionary_warning",
                "reason": str(warn)[:240],
                "suggestedAgent": "supply_chain_analyzer",
            }
        )
    return items[: _DEFERRED_LIMIT * 2]


def _known_anchors(
    fragments: list[CanonicalFragment],
    edges: list[CanonicalEdge],
) -> list[dict[str, Any]]:
    """Top fragments by incident edges + confidence, for downstream seeding."""
    incidence: dict[str, int] = {}
    for e in edges:
        incidence[e.source_fragment_id] = incidence.get(e.source_fragment_id, 0) + 1
        incidence[e.target_fragment_id] = incidence.get(e.target_fragment_id, 0) + 1

    def _score(f: CanonicalFragment) -> tuple[int, float]:
        return (incidence.get(f.id, 0), float(f.confidence or 0.0))

    ranked = sorted(fragments, key=_score, reverse=True)
    out: list[dict[str, Any]] = []
    for f in ranked[:_KNOWN_ANCHOR_LIMIT]:
        out.append(
            {
                "canonicalPath": f.canonical_path,
                "fragmentClass": f.fragment_class,
                "surfaceForms": list(f.surface_forms)[:_KNOWN_ANCHOR_SURFACE_LIMIT],
                "incidentEdgeCount": incidence.get(f.id, 0),
                "confidence": round(float(f.confidence or 0.0), 3),
            }
        )
    return out


def build_follow_up_plan(
    fragments: list[CanonicalFragment],
    edges: list[CanonicalEdge],
    candidates: Candidates,
    report: ValidationReport,
) -> dict[str, Any]:
    """Produce the ``follow_up_plan`` blob attached to CanonicalizerResult."""
    aggregate = _aggregate_mentions(fragments, report)
    deferred = _deferred_research(candidates, report)
    anchors = _known_anchors(fragments, edges)
    return {
        "aggregateMentions": aggregate,
        "deferredResearch": deferred,
        "knownAnchors": anchors,
        "counters": {
            "orphanCount": len(report.orphan_nodes or []),
            "unresolvedEndpoints": sum(
                1 for iss in report.relation_errors if iss.code == "unresolved_endpoint_surface"
            ),
            "unknownRelations": len(candidates.unknown_relations or []),
            "unknownTransforms": len(candidates.unknown_transforms or []),
            "unknownProducts": len(candidates.unknown_products or []),
            "mergeCandidates": len(candidates.merge_candidates or []),
            "registryCoverageGaps": len(candidates.registry_coverage_issues or []),
        },
    }

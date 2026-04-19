"""Validate canonical paths, relation endpoints, layers."""

from __future__ import annotations

import re

from agenthorde_common.canonicalizer.schemas import CanonicalEdge, CanonicalFragment, ValidationIssue, ValidationReport
from agenthorde_common.canonicalizer.taxonomy import (
    ASSURANCE_TYPES,
    COMMUNITY_SUBTYPES,
    DISRUPTION_KINDS,
    FRAGMENT_CLASSES,
    INCOTERMS_2020,
    INFLUENCE_KINDS,
    MECHANISM_CATEGORIES,
    MOBILE_ASSET_KINDS,
    RELATION_ALLOWED_LAYERS,
    is_forbidden_endpoint_pair,
    path_fragment_class,
    relation_kind_for_endpoints,
)


def _segments(path: str) -> list[str]:
    p = (path or "").strip().lower()
    if not p.startswith("/"):
        return []
    return [x for x in p.split("/") if x]


def validate_canonical_path(path: str) -> list[ValidationIssue]:
    issues: list[ValidationIssue] = []
    segs = _segments(path)
    if not segs:
        issues.append(ValidationIssue(code="path_empty", message="Empty or invalid path", detail={"path": path}))
        return issues
    if segs[0] not in FRAGMENT_CLASSES:
        issues.append(
            ValidationIssue(
                code="path_bad_class",
                message=f"Unknown fragment class: {segs[0]}",
                detail={"path": path},
            )
        )
        return issues
    if len(segs) < 2:
        issues.append(
            ValidationIssue(
                code="path_missing_second",
                message="Path must have at least 2 segments",
                detail={"path": path},
            )
        )
        return issues
    # Evidence: /evidence/source/{domain}/{hash} or /evidence/claim/...
    cls = segs[0]
    if cls == "evidence" and len(segs) < 3:
        issues.append(
            ValidationIssue(
                code="path_evidence_shallow",
                message="/evidence paths need subtype + id",
                detail={"path": path},
            )
        )
    if cls == "product" and len(segs) < 3:
        issues.append(
            ValidationIssue(
                code="path_product_shallow",
                message="/product/{family}/{slug} requires 3+ segments",
                detail={"path": path},
            )
        )
    if cls == "community":
        # /community/{geo}/{subtype}/{slug}: 4 segments; subtype must be in catalog.
        if len(segs) < 4:
            issues.append(
                ValidationIssue(
                    code="path_community_shallow",
                    message="/community/{geo}/{subtype}/{slug} requires 4 segments",
                    detail={"path": path},
                )
            )
        elif segs[2] not in COMMUNITY_SUBTYPES:
            issues.append(
                ValidationIssue(
                    code="path_community_bad_subtype",
                    message=f"Unknown community subtype: {segs[2]}",
                    detail={"path": path, "subtype": segs[2]},
                )
            )
    if cls == "decision":
        # /decision/{domain-slug}/{slug}: 3 segments. domain-slug is a free
        # slug (no closed catalog yet) but it must obey slug regex below.
        if len(segs) < 3:
            issues.append(
                ValidationIssue(
                    code="path_decision_shallow",
                    message="/decision/{domain-slug}/{slug} requires 3 segments",
                    detail={"path": path},
                )
            )
    if cls == "mechanism":
        # /mechanism/{category}/{slug}: 3 segments; category must be in
        # MECHANISM_CATEGORIES (closed enum).
        if len(segs) < 3:
            issues.append(
                ValidationIssue(
                    code="path_mechanism_shallow",
                    message="/mechanism/{category}/{slug} requires 3 segments",
                    detail={"path": path},
                )
            )
        elif segs[1] not in MECHANISM_CATEGORIES:
            issues.append(
                ValidationIssue(
                    code="path_mechanism_bad_category",
                    message=f"Unknown mechanism category: {segs[1]}",
                    detail={"path": path, "category": segs[1]},
                )
            )
    if cls == "mobile_asset":
        # /mobile_asset/{kind}/{geo}/{slug}-{id8}: 4 segments; kind must be in
        # MOBILE_ASSET_KINDS (closed enum). The trailing -{id8} is folded into
        # the slug segment so segment count stays at 4.
        if len(segs) < 4:
            issues.append(
                ValidationIssue(
                    code="path_mobile_asset_shallow",
                    message="/mobile_asset/{kind}/{geo}/{slug}-{id8} requires 4 segments",
                    detail={"path": path},
                )
            )
        elif segs[1] not in MOBILE_ASSET_KINDS:
            issues.append(
                ValidationIssue(
                    code="path_mobile_asset_bad_kind",
                    message=f"Unknown mobile_asset kind: {segs[1]}",
                    detail={"path": path, "kind": segs[1]},
                )
            )
    slug_re = re.compile(r"^[a-z0-9][a-z0-9-]{0,47}$")
    for i, s in enumerate(segs[1:], start=1):
        if s in ("na", "unknown"):
            continue
        if cls == "evidence" and len(segs) > 2 and segs[1] == "source" and i == 2:
            continue  # domain segment may contain dots
        if cls == "geo" and i == 1 and len(s) == 2 and s.isalpha():
            continue  # ISO2
        if cls in ("org", "person", "site", "community") and i == 1 and len(s) == 2 and s.isalpha():
            continue  # ISO2 slot
        if cls == "community" and i == 2 and s in COMMUNITY_SUBTYPES:
            continue  # community subtype slot (already validated above)
        if not slug_re.match(s) and not (len(s) == 2 and s.isalpha()):
            issues.append(
                ValidationIssue(
                    code="path_bad_segment",
                    message=f"Segment {i} has invalid slug form: {s!r}",
                    detail={"path": path, "segment": s},
                )
            )
    return issues


def validate_layer(relation: str, layer: str) -> bool:
    allowed = RELATION_ALLOWED_LAYERS.get(relation, frozenset(("structural",)))
    return layer in allowed


def validate_graph(
    fragments: list[CanonicalFragment],
    edges: list[CanonicalEdge],
) -> ValidationReport:
    report = ValidationReport()
    frag_by_id = {f.id: f for f in fragments}
    frag_by_path = {f.canonical_path: f for f in fragments}

    for f in fragments:
        for iss in validate_canonical_path(f.canonical_path):
            report.path_errors.append(iss)

    for e in edges:
        sf = frag_by_id.get(e.source_fragment_id)
        tf = frag_by_id.get(e.target_fragment_id)
        if not sf or not tf:
            report.relation_errors.append(
                ValidationIssue(
                    code="edge_missing_fragment",
                    message="Edge references unknown fragment id",
                    detail={"edgeId": e.id, "from": e.source_fragment_id, "to": e.target_fragment_id},
                )
            )
            continue
        fa = path_fragment_class(sf.canonical_path) or sf.fragment_class
        ta = path_fragment_class(tf.canonical_path) or tf.fragment_class
        fa_s = fa or ""
        ta_s = ta or ""
        # Three-tier endpoint check:
        #   forbidden    -> relation_errors (hard)
        #   recognized   -> ok, kind label should be on attributes (phase 4)
        #   unspecified  -> relation_kind_warnings (soft)
        if is_forbidden_endpoint_pair(e.relation, fa_s, ta_s):
            report.relation_errors.append(
                ValidationIssue(
                    code="relation_endpoints_forbidden",
                    message=f"Relation {e.relation} forbidden for {fa_s}->{ta_s}",
                    detail={
                        "edgeId": e.id,
                        "from": fa_s,
                        "to": ta_s,
                        "relation": e.relation,
                    },
                )
            )
        else:
            kind = relation_kind_for_endpoints(e.relation, fa_s, ta_s)
            if kind is None:
                report.relation_kind_warnings.append(
                    ValidationIssue(
                        code="relation_endpoints_unspecified",
                        message=(
                            f"Relation {e.relation} for {fa_s}->{ta_s} is not in the kind "
                            "catalog; edge kept as unspecified projection"
                        ),
                        detail={
                            "edgeId": e.id,
                            "from": fa_s,
                            "to": ta_s,
                            "relation": e.relation,
                        },
                    )
                )
        if not validate_layer(e.relation, e.layer):
            report.layer_errors.append(
                ValidationIssue(
                    code="layer_not_allowed",
                    message=f"Layer {e.layer} not allowed for {e.relation}",
                    detail={"edgeId": e.id},
                )
            )

        if e.relation == "supplies":
            pid = (e.attributes or {}).get("product_fragment_id")
            prod_ok = (
                isinstance(pid, str)
                and pid in frag_by_id
                and frag_by_id[pid].fragment_class == "product"
            )
            if not prod_ok:
                report.relation_errors.append(
                    ValidationIssue(
                        code="supply_missing_cargo",
                        message="supplies edge must have attributes.product_fragment_id pointing to a product fragment",
                        detail={"edgeId": e.id, "product_fragment_id": pid},
                    )
                )

        if e.relation in ("supplies", "charters"):
            inc = (e.attributes or {}).get("incoterm")
            if inc is not None and inc != "":
                inc_s = str(inc).strip().upper()
                if inc_s not in INCOTERMS_2020:
                    report.dictionary_warnings.append(
                        f"bad_incoterm: edgeId={e.id} relation={e.relation} incoterm={inc!r}"
                    )

        if e.relation == "influences":
            kind = (e.attributes or {}).get("influence_kind")
            if not isinstance(kind, str) or not kind.strip():
                report.relation_errors.append(
                    ValidationIssue(
                        code="influences_missing_kind",
                        message="influences edge must carry attributes.influence_kind (closed enum InfluenceKind)",
                        detail={"edgeId": e.id},
                    )
                )
            elif kind not in INFLUENCE_KINDS:
                report.dictionary_warnings.append(
                    f"unknown_influence_kind: edgeId={e.id} influence_kind={kind!r}"
                )

        if e.relation == "assures":
            atype = (e.attributes or {}).get("assurance_type")
            if not isinstance(atype, str) or not atype.strip():
                report.relation_errors.append(
                    ValidationIssue(
                        code="assures_missing_type",
                        message="assures edge must carry attributes.assurance_type (closed enum AssuranceType)",
                        detail={"edgeId": e.id},
                    )
                )
            elif atype not in ASSURANCE_TYPES:
                report.dictionary_warnings.append(
                    f"unknown_assurance_type: edgeId={e.id} assurance_type={atype!r}"
                )

        if e.relation == "disrupts":
            dkind = (e.attributes or {}).get("disruption_kind")
            if not isinstance(dkind, str) or not dkind.strip():
                report.relation_errors.append(
                    ValidationIssue(
                        code="disrupts_missing_kind",
                        message="disrupts edge must carry attributes.disruption_kind (closed enum DisruptionKind)",
                        detail={"edgeId": e.id},
                    )
                )
            elif dkind not in DISRUPTION_KINDS:
                report.dictionary_warnings.append(
                    f"unknown_disruption_kind: edgeId={e.id} disruption_kind={dkind!r}"
                )

    product_paths_lower = {f.canonical_path.lower() for f in fragments if f.fragment_class == "product"}
    for f in fragments:
        if f.fragment_class != "site":
            continue
        for st in f.site_transforms:
            for inp in st.inputs:
                ip = (inp or "").strip().lower()
                if not ip or ip not in product_paths_lower:
                    report.transform_errors.append(
                        ValidationIssue(
                            code="unknown_transform_io",
                            message="SiteTransform input path does not match a product fragment",
                            detail={"siteId": f.id, "input": inp},
                        )
                    )
            op = (st.output_product or "").strip().lower()
            if not op or op not in product_paths_lower:
                report.transform_errors.append(
                    ValidationIssue(
                        code="unknown_transform_io",
                        message="SiteTransform output path does not match a product fragment",
                        detail={"siteId": f.id, "output": st.output_product},
                    )
                )

    # Orphan nodes: business fragments that no edge references. Evidence anchors
    # no longer live in the graph, so the old skip-list is obsolete.
    connected: set[str] = set()
    for e in edges:
        connected.add(e.source_fragment_id)
        connected.add(e.target_fragment_id)
    for f in fragments:
        if f.id in connected:
            continue
        if f.canonical_path.startswith("/evidence/"):
            # Legacy shape — should not appear anymore; still tolerate silently.
            continue
        report.orphan_nodes.append(f.id)
    return report

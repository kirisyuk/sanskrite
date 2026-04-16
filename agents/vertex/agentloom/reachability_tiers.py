"""Reachability tiers beyond strict business-complete / grounded chains (typed operational backbone)."""

from __future__ import annotations

from typing import Any

from agentloom.contracts import EntityNode, FlowLeg, OperationalGraphState, ProcessStep
from agentloom.operational_graph import flow_leg_line, process_step_line

_PROCESS_KINDS_BACKBONE = frozenset({"extraction", "transformation", "assembly"})


def _node_label(node_index: dict[str, EntityNode], node_id: str) -> str:
    if not node_id:
        return ""
    node = node_index.get(node_id)
    if node is None:
        return node_id
    return (node.label or node_id).strip() or node_id


def compute_typed_operational_backbone_entries(
    graph_state: OperationalGraphState,
    node_index: dict[str, EntityNode],
) -> list[dict[str, Any]]:
    """
    MVP "good enough" backbone: meaningful typed flow + transform/assembly/extraction,
    multiple hosts, at least one PRODUCT reference — without full `_business_chain_profile`.

    Returns 0–1 summary entries compatible with reachability list shape (source, sink, hops, path,
    groundedEdgeCount / weakEdgeCount for optional display).
    """
    legs = list(graph_state.flow_legs)
    steps = list(graph_state.process_steps)
    if not legs or not steps:
        return []

    has_backbone_step = any(
        (s.process_kind or "").strip().lower() in _PROCESS_KINDS_BACKBONE for s in steps
    )
    if not has_backbone_step:
        return []

    hosts: set[str] = set()
    for leg in legs:
        if leg.supplier_node_id:
            hosts.add(leg.supplier_node_id)
        if leg.customer_node_id:
            hosts.add(leg.customer_node_id)
    for s in steps:
        if s.host_node_id:
            hosts.add(s.host_node_id)
    if len(hosts) < 2:
        return []

    def _is_product_ref(node_id: str) -> bool:
        if not node_id:
            return False
        node = node_index.get(node_id)
        if node is not None and (node.entity_type or "").upper() == "PRODUCT":
            return True
        return (node_id.split(":", 1)[0].strip().upper() == "PRODUCT") if ":" in node_id else False

    product_seen = False
    for leg in legs:
        if _is_product_ref(leg.product_node_id):
            product_seen = True
            break
    for s in steps:
        for oid in s.output_node_ids or ():
            if _is_product_ref(str(oid)):
                product_seen = True
                break
        for iid in s.input_node_ids or ():
            if _is_product_ref(str(iid)):
                product_seen = True
                break
        if product_seen:
            break
    if not product_seen:
        return []

    path_lines: list[str] = []
    for leg in legs[:12]:
        path_lines.append(flow_leg_line(leg, node_index))
    for s in steps[:12]:
        path_lines.append(process_step_line(s, node_index))

    grounded = 0
    weak = 0
    for leg in legs:
        st = (leg.support_type or "").strip().lower()
        conf = float(leg.confidence or 0.0)
        if st in {"document", "mixed"} and conf >= 0.5:
            grounded += 1
        else:
            weak += 1
    for s in steps:
        st = (s.support_type or "").strip().lower()
        conf = float(s.confidence or 0.0)
        if st in {"document", "mixed"} and conf >= 0.5:
            grounded += 1
        else:
            weak += 1

    extraction_steps = [s for s in steps if (s.process_kind or "").strip().lower() == "extraction"]
    source_label = ""
    if extraction_steps:
        st0 = extraction_steps[0]
        dep_labs = [_node_label(node_index, str(nid)) for nid in (st0.input_node_ids or [])[:3] if nid]
        source_label = ", ".join(str(x) for x in dep_labs if x) or _node_label(node_index, st0.host_node_id)
    if not source_label and legs:
        leg0 = legs[0]
        source_label = _node_label(node_index, leg0.supplier_node_id)

    sink_host = ""
    if legs:
        last = legs[-1]
        sink_host = last.customer_node_id or ""
    if not sink_host and steps:
        sink_host = steps[-1].host_node_id or ""
    sink_label = _node_label(node_index, sink_host)

    hops = min(len(path_lines), len(legs) + len(steps))
    entry: dict[str, Any] = {
        "source": source_label or "operational_backbone",
        "sink": sink_label or "operational_backbone",
        "sinkType": "FACILITY",
        "hops": hops,
        "path": [p for p in path_lines if p.strip()][:8],
        "groundedEdgeCount": grounded,
        "weakEdgeCount": weak,
        "routeFingerprint": [],
        "tier": "typed_backbone",
        "structuralReason": (
            "Typed operational backbone (non-audit): at least one flow leg, one process step "
            "(extraction/transform/assembly), 2+ hosts, and a PRODUCT reference; not evaluated as business-complete."
        ),
    }
    return [entry]

from __future__ import annotations

from collections import OrderedDict
from typing import Any, TypeVar

from agentloom.contracts import (
    EntityNode,
    FlowLeg,
    GraphFragment,
    GraphObservation,
    OperationalGraphState,
    ProcessStep,
)

T = TypeVar("T")
_KNOWN_ENTITY_TYPES = {
    "DEPOSIT",
    "FACILITY",
    "GEO_REGION",
    "ORGANIZATION",
    "PERSON",
    "PRODUCT",
    "PROGRAM",
}
_FLOW_ENDPOINT_FORBIDDEN_TYPES = {"PRODUCT", "DEPOSIT", "GEO_REGION"}
_PROCESS_HOST_ALLOWED_TYPES = {"FACILITY", "ORGANIZATION"}
_OPERATIONAL_RELATIONS = {
    "SUPPLIES_TO",
    "PRODUCES",
    "TRANSFORMS_INTO",
    "ASSEMBLES_INTO",
    "USED_IN",
    "EXTRACTS_FROM",
}


def _slug(value: str) -> str:
    out = []
    last_dash = False
    for ch in (value or "").lower():
        if ch.isalnum():
            out.append(ch)
            last_dash = False
        elif not last_dash:
            out.append("-")
            last_dash = True
    text = "".join(out).strip("-")
    return text or "node"


def synthetic_id(entity_type: str, label: str) -> str:
    return f"{_slug(entity_type or 'entity')}:{_slug(label)}"


def _normalized_label(value: str) -> str:
    return " ".join((value or "").strip().lower().split())


def _coerce_observation(raw: Any, *, index: int = 0) -> GraphObservation | None:
    if isinstance(raw, GraphObservation):
        return raw
    if not isinstance(raw, dict):
        return None
    payload = dict(raw)
    payload.setdefault("observationId", payload.get("observation_id") or f"legacy-observation-{index}")
    try:
        return GraphObservation.model_validate(payload)
    except Exception:
        return None


def _support_rank(support_type: str) -> int:
    order = ("hypothesis", "model_knowledge", "mixed", "document")
    try:
        return order.index(support_type)
    except ValueError:
        return 0


def _merge_lists(left: list[str], right: list[str], *, limit: int = 24) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for raw in [*left, *right]:
        item = str(raw or "").strip()
        if not item or item in seen:
            continue
        seen.add(item)
        out.append(item)
        if len(out) >= limit:
            break
    return out


def _pick_stronger(
    existing: T,
    incoming: T,
    existing_support: str,
    incoming_support: str,
    existing_conf: float,
    incoming_conf: float,
) -> T:
    if abs(incoming_conf - existing_conf) > 1e-9:
        return incoming if incoming_conf > existing_conf else existing
    return incoming if _support_rank(incoming_support) >= _support_rank(existing_support) else existing


def _normalize_node(node: EntityNode) -> EntityNode:
    node_id = (node.node_id or "").strip() or synthetic_id(node.entity_type, node.label)
    return node.model_copy(update={"node_id": node_id})


def _node_type_hint(nodes: dict[str, EntityNode], node_id: str) -> str:
    node = nodes.get(node_id)
    if node is not None and node.entity_type:
        return node.entity_type.upper()
    if ":" not in node_id:
        return ""
    prefix = node_id.split(":", 1)[0].strip().upper()
    return prefix if prefix in _KNOWN_ENTITY_TYPES else ""


def _fragment_has_native_operational_primitives(fragment: GraphFragment) -> bool:
    return bool(fragment.flow_legs or fragment.process_steps)


def _merge_node(existing: EntityNode, incoming: EntityNode) -> EntityNode:
    chosen = _pick_stronger(
        existing,
        incoming,
        existing.support_type,
        incoming.support_type,
        float(existing.confidence),
        float(incoming.confidence),
    )
    other = incoming if chosen is existing else existing
    return chosen.model_copy(
        update={
            "label": chosen.label or other.label,
            "entity_type": chosen.entity_type or other.entity_type,
            "canonical_ref": chosen.canonical_ref or other.canonical_ref,
            "qualifiers": _merge_lists(list(existing.qualifiers), list(incoming.qualifiers)),
            "confidence": max(float(existing.confidence), float(incoming.confidence)),
            "support_type": chosen.support_type,
            "provenance": _merge_lists(list(existing.provenance), list(incoming.provenance)),
            "support_note": chosen.support_note or other.support_note,
            "branch_id": chosen.branch_id or other.branch_id,
            "alternative_group_id": chosen.alternative_group_id or other.alternative_group_id,
            "slot_id": chosen.slot_id or other.slot_id,
            "slot_role": chosen.slot_role or other.slot_role,
        }
    )


def _flow_leg_key(leg: FlowLeg) -> tuple[str, str, str, tuple[str, ...], str, str]:
    return (
        leg.supplier_node_id,
        leg.customer_node_id,
        leg.product_node_id,
        tuple(leg.route_node_ids),
        leg.branch_id,
        leg.alternative_group_id,
    )


def _merge_flow_leg(existing: FlowLeg, incoming: FlowLeg) -> FlowLeg:
    chosen = _pick_stronger(
        existing,
        incoming,
        existing.support_type,
        incoming.support_type,
        float(existing.confidence),
        float(incoming.confidence),
    )
    other = incoming if chosen is existing else existing
    return chosen.model_copy(
        update={
            "route_node_ids": _merge_lists(list(existing.route_node_ids), list(incoming.route_node_ids), limit=12),
            "qualifiers": _merge_lists(list(existing.qualifiers), list(incoming.qualifiers)),
            "confidence": max(float(existing.confidence), float(incoming.confidence)),
            "support_type": chosen.support_type,
            "provenance": _merge_lists(list(existing.provenance), list(incoming.provenance)),
            "support_note": chosen.support_note or other.support_note,
            "slot_id": chosen.slot_id or other.slot_id,
            "slot_role": chosen.slot_role or other.slot_role,
        }
    )


def _process_step_key(step: ProcessStep) -> tuple[str, str, tuple[str, ...], tuple[str, ...], str, str]:
    return (
        step.host_node_id,
        step.process_kind,
        tuple(step.input_node_ids),
        tuple(step.output_node_ids),
        step.branch_id,
        step.alternative_group_id,
    )


def _merge_process_step(existing: ProcessStep, incoming: ProcessStep) -> ProcessStep:
    chosen = _pick_stronger(
        existing,
        incoming,
        existing.support_type,
        incoming.support_type,
        float(existing.confidence),
        float(incoming.confidence),
    )
    other = incoming if chosen is existing else existing
    return chosen.model_copy(
        update={
            "input_node_ids": _merge_lists(list(existing.input_node_ids), list(incoming.input_node_ids), limit=12),
            "output_node_ids": _merge_lists(list(existing.output_node_ids), list(incoming.output_node_ids), limit=12),
            "qualifiers": _merge_lists(list(existing.qualifiers), list(incoming.qualifiers)),
            "confidence": max(float(existing.confidence), float(incoming.confidence)),
            "support_type": chosen.support_type,
            "provenance": _merge_lists(list(existing.provenance), list(incoming.provenance)),
            "support_note": chosen.support_note or other.support_note,
            "slot_id": chosen.slot_id or other.slot_id,
            "slot_role": chosen.slot_role or other.slot_role,
        }
    )


def _overlay_key(observation: GraphObservation) -> tuple[str, str, str, str, tuple[str, ...]]:
    return (
        observation.subject_id or observation.subject,
        observation.relation,
        observation.object_id or observation.object,
        observation.branch_id,
        tuple(observation.qualifiers),
    )


def normalize_graph_state(graph: OperationalGraphState) -> OperationalGraphState:
    nodes: OrderedDict[str, EntityNode] = OrderedDict()
    for raw in graph.entity_nodes:
        node = _normalize_node(raw)
        existing = nodes.get(node.node_id)
        nodes[node.node_id] = node if existing is None else _merge_node(existing, node)

    flow_legs: OrderedDict[tuple[str, str, str, tuple[str, ...], str, str], FlowLeg] = OrderedDict()
    for raw in graph.flow_legs:
        leg = raw.model_copy(
            update={
                "leg_id": (raw.leg_id or "").strip() or f"{raw.branch_id or 'graph'}-leg-{len(flow_legs) + 1}",
            }
        )
        key = _flow_leg_key(leg)
        existing = flow_legs.get(key)
        flow_legs[key] = leg if existing is None else _merge_flow_leg(existing, leg)

    process_steps: OrderedDict[tuple[str, str, tuple[str, ...], tuple[str, ...], str, str], ProcessStep] = OrderedDict()
    for raw in graph.process_steps:
        step = raw.model_copy(
            update={
                "step_id": (raw.step_id or "").strip() or f"{raw.branch_id or 'graph'}-step-{len(process_steps) + 1}",
            }
        )
        key = _process_step_key(step)
        existing = process_steps.get(key)
        process_steps[key] = step if existing is None else _merge_process_step(existing, step)

    overlays: OrderedDict[tuple[str, str, str, str, tuple[str, ...]], GraphObservation] = OrderedDict()
    for raw in graph.overlay_edges:
        obs = raw.model_copy(
            update={
                "subject_id": raw.subject_id or synthetic_id(raw.subject_type, raw.subject),
                "object_id": raw.object_id or synthetic_id(raw.object_type, raw.object),
            }
        )
        overlays[_overlay_key(obs)] = obs

    normalized = OperationalGraphState(
        entityNodes=list(nodes.values()),
        flowLegs=list(flow_legs.values()),
        processSteps=list(process_steps.values()),
        overlayEdges=list(overlays.values()),
    )
    _validate_graph_state(normalized)
    return normalized


def _validate_graph_state(graph: OperationalGraphState) -> None:
    nodes = {
        node.node_id: node
        for node in graph.entity_nodes
        if node.node_id
    }
    for leg in graph.flow_legs:
        product_type = _node_type_hint(nodes, leg.product_node_id)
        if product_type and product_type != "PRODUCT":
            raise ValueError(
                f"flowLeg {leg.leg_id or '(unlabeled)'} productNodeId must point to PRODUCT, got {product_type}"
            )
        for endpoint_id, role in (
            (leg.supplier_node_id, "supplierNodeId"),
            (leg.customer_node_id, "customerNodeId"),
        ):
            endpoint_type = _node_type_hint(nodes, endpoint_id)
            if endpoint_type in _FLOW_ENDPOINT_FORBIDDEN_TYPES:
                raise ValueError(
                    f"flowLeg {leg.leg_id or '(unlabeled)'} {role} must be an operator/facility endpoint, got {endpoint_type}"
                )
        for route_id in leg.route_node_ids:
            route_type = _node_type_hint(nodes, route_id)
            if route_type == "PRODUCT":
                raise ValueError(
                    f"flowLeg {leg.leg_id or '(unlabeled)'} routeNodeIds cannot point to PRODUCT nodes"
                )

    for step in graph.process_steps:
        host_type = _node_type_hint(nodes, step.host_node_id)
        if host_type and host_type not in _PROCESS_HOST_ALLOWED_TYPES:
            raise ValueError(
                f"processStep {step.step_id or '(unlabeled)'} hostNodeId must be FACILITY or ORGANIZATION, got {host_type}"
            )
        if not step.output_node_ids:
            raise ValueError(f"processStep {step.step_id or '(unlabeled)'} must have at least one outputNodeId")
        input_expected = "DEPOSIT" if step.process_kind == "extraction" else "PRODUCT"
        for input_id in step.input_node_ids:
            input_type = _node_type_hint(nodes, input_id)
            if input_type and input_type != input_expected:
                raise ValueError(
                    f"processStep {step.step_id or '(unlabeled)'} input {input_id} must be {input_expected}, got {input_type}"
                )
        for output_id in step.output_node_ids:
            output_type = _node_type_hint(nodes, output_id)
            if output_type and output_type != "PRODUCT":
                raise ValueError(
                    f"processStep {step.step_id or '(unlabeled)'} output {output_id} must be PRODUCT, got {output_type}"
                )


def empty_graph_state() -> OperationalGraphState:
    return OperationalGraphState(entityNodes=[], flowLegs=[], processSteps=[], overlayEdges=[])


def graph_state_from_raw(raw: Any) -> OperationalGraphState:
    if isinstance(raw, OperationalGraphState):
        return normalize_graph_state(raw)
    if isinstance(raw, dict):
        return normalize_graph_state(OperationalGraphState.model_validate(raw))
    if isinstance(raw, list):
        observations: list[GraphObservation] = []
        for idx, item in enumerate(raw, start=1):
            observation = _coerce_observation(item, index=idx)
            if observation is not None:
                observations.append(observation)
        return graph_state_from_observations(observations)
    return empty_graph_state()


def graph_state_to_payload(graph: OperationalGraphState) -> dict[str, Any]:
    normalized = normalize_graph_state(graph)
    return normalized.model_dump(mode="json", by_alias=True)


def graph_state_from_observations(observations: list[GraphObservation]) -> OperationalGraphState:
    if not observations:
        return empty_graph_state()
    nodes, legs, steps, overlays = _legacy_graph_components_from_observations(
        observations,
        supporting_nodes=[],
    )
    return normalize_graph_state(
        OperationalGraphState(
            entityNodes=nodes,
            flowLegs=legs,
            processSteps=steps,
            overlayEdges=overlays,
        )
    )


def _node_index_by_label(nodes: list[EntityNode]) -> dict[tuple[str, str], str]:
    index: dict[tuple[str, str], str] = {}
    for raw in nodes:
        node = _normalize_node(raw)
        label_key = _normalized_label(node.label or node.canonical_ref)
        if not label_key:
            continue
        entity_type = (node.entity_type or "").upper()
        if entity_type:
            index[(entity_type, label_key)] = node.node_id
        index.setdefault(("", label_key), node.node_id)
    return index


def _register_observation_endpoint(
    target: dict[str, EntityNode],
    observation: GraphObservation,
    *,
    side: str,
) -> str:
    if side == "subject":
        node_id = observation.subject_id or synthetic_id(observation.subject_type, observation.subject)
        label = observation.subject
        entity_type = observation.subject_type
    else:
        node_id = observation.object_id or synthetic_id(observation.object_type, observation.object)
        label = observation.object
        entity_type = observation.object_type
    if not node_id:
        return ""
    if node_id not in target:
        target[node_id] = _normalize_node(
            EntityNode(
                nodeId=node_id,
                label=label or node_id,
                entityType=entity_type,
                confidence=observation.confidence,
                supportType=observation.support_type,
                provenance=list(observation.provenance),
                supportNote=observation.support_note,
                branchId=observation.branch_id,
                alternativeGroupId=observation.alternative_group_id,
                slotId=observation.slot_id,
                slotRole=observation.slot_role,
            )
        )
    return node_id


def _legacy_host_node_id(
    label_index: dict[tuple[str, str], str],
    host_label: str,
) -> str:
    label_key = _normalized_label(host_label)
    if not label_key:
        return ""
    return (
        label_index.get(("FACILITY", label_key))
        or label_index.get(("ORGANIZATION", label_key))
        or label_index.get(("", label_key))
        or synthetic_id("FACILITY", host_label)
    )


def _legacy_graph_components_from_observations(
    observations: list[GraphObservation],
    *,
    supporting_nodes: list[EntityNode],
) -> tuple[list[EntityNode], list[FlowLeg], list[ProcessStep], list[GraphObservation]]:
    if not observations and not supporting_nodes:
        return [], [], [], []

    node_map: OrderedDict[str, EntityNode] = OrderedDict()
    for raw in supporting_nodes:
        node = _normalize_node(raw)
        node_map[node.node_id] = node
    for observation in observations:
        _register_observation_endpoint(node_map, observation, side="subject")
        _register_observation_endpoint(node_map, observation, side="object")

    label_index = _node_index_by_label(list(node_map.values()))
    produce_outputs: dict[tuple[str, str, str], list[str]] = {}
    produce_observations: dict[tuple[str, str, str], list[GraphObservation]] = {}
    for observation in observations:
        subject_id = observation.subject_id or synthetic_id(observation.subject_type, observation.subject)
        object_id = observation.object_id or synthetic_id(observation.object_type, observation.object)
        if observation.relation != "PRODUCES":
            continue
        subject_type = _node_type_hint(node_map, subject_id)
        object_type = _node_type_hint(node_map, object_id)
        if subject_type not in _PROCESS_HOST_ALLOWED_TYPES or object_type != "PRODUCT":
            continue
        key = (subject_id, observation.branch_id, observation.alternative_group_id)
        produce_outputs.setdefault(key, [])
        if object_id not in produce_outputs[key]:
            produce_outputs[key].append(object_id)
        produce_observations.setdefault(key, []).append(observation)

    used_observation_ids: set[str] = set()
    all_legs: list[FlowLeg] = []
    all_steps: list[ProcessStep] = []
    all_overlays: list[GraphObservation] = []

    for observation in observations:
        subject_id = observation.subject_id or synthetic_id(observation.subject_type, observation.subject)
        object_id = observation.object_id or synthetic_id(observation.object_type, observation.object)
        relation = observation.relation
        if relation == "SUPPLIES_TO":
            product_label = next(
                (item.split(":", 1)[1].strip() for item in observation.qualifiers if item.startswith("product:")),
                "",
            )
            if product_label:
                product_id = (
                    label_index.get(("PRODUCT", _normalized_label(product_label)))
                    or synthetic_id("PRODUCT", product_label)
                )
                if product_id not in node_map:
                    node_map[product_id] = _normalize_node(
                        EntityNode(
                            nodeId=product_id,
                            label=product_label,
                            entityType="PRODUCT",
                            confidence=observation.confidence,
                            supportType=observation.support_type,
                            provenance=list(observation.provenance),
                            supportNote=observation.support_note,
                            branchId=observation.branch_id,
                            alternativeGroupId=observation.alternative_group_id,
                            slotId=observation.slot_id,
                            slotRole=observation.slot_role,
                        )
                    )
                    label_index = _node_index_by_label(list(node_map.values()))
                supplier_type = _node_type_hint(node_map, subject_id)
                customer_type = _node_type_hint(node_map, object_id)
                if supplier_type not in _FLOW_ENDPOINT_FORBIDDEN_TYPES and customer_type not in _FLOW_ENDPOINT_FORBIDDEN_TYPES:
                    all_legs.append(
                        FlowLeg(
                            legId=observation.observation_id,
                            supplierNodeId=subject_id,
                            customerNodeId=object_id,
                            productNodeId=product_id,
                            qualifiers=[item for item in observation.qualifiers if not item.startswith("product:")],
                            confidence=observation.confidence,
                            supportType=observation.support_type,
                            provenance=list(observation.provenance),
                            supportNote=observation.support_note,
                            branchId=observation.branch_id,
                            alternativeGroupId=observation.alternative_group_id,
                            slotId=observation.slot_id,
                            slotRole=observation.slot_role,
                        )
                    )
                    used_observation_ids.add(observation.observation_id)
                    continue
        elif relation == "EXTRACTS_FROM":
            host_id = subject_id
            deposit_id = object_id
            if _node_type_hint(node_map, subject_id) == "DEPOSIT" and _node_type_hint(node_map, object_id) != "DEPOSIT":
                host_id = object_id
                deposit_id = subject_id
            key = (host_id, observation.branch_id, observation.alternative_group_id)
            outputs = produce_outputs.get(key) or []
            if _node_type_hint(node_map, host_id) in _PROCESS_HOST_ALLOWED_TYPES and _node_type_hint(node_map, deposit_id) == "DEPOSIT" and outputs:
                all_steps.append(
                    ProcessStep(
                        stepId=observation.observation_id,
                        hostNodeId=host_id,
                        processKind="extraction",
                        inputNodeIds=[deposit_id],
                        outputNodeIds=list(outputs),
                        qualifiers=list(observation.qualifiers),
                        confidence=observation.confidence,
                        supportType=observation.support_type,
                        provenance=list(observation.provenance),
                        supportNote=observation.support_note,
                        branchId=observation.branch_id,
                        alternativeGroupId=observation.alternative_group_id,
                        slotId=observation.slot_id,
                        slotRole=observation.slot_role,
                    )
                )
                used_observation_ids.add(observation.observation_id)
                used_observation_ids.update(item.observation_id for item in produce_observations.get(key, []))
                continue
        elif relation in {"TRANSFORMS_INTO", "ASSEMBLES_INTO"}:
            host_label = next(
                (item.split(":", 1)[1].strip() for item in observation.qualifiers if item.startswith("host:")),
                "",
            )
            host_id = _legacy_host_node_id(label_index, host_label)
            if host_label and host_id not in node_map:
                node_map[host_id] = _normalize_node(
                    EntityNode(
                        nodeId=host_id,
                        label=host_label,
                        entityType="FACILITY",
                        confidence=observation.confidence,
                        supportType=observation.support_type,
                        provenance=list(observation.provenance),
                        supportNote=observation.support_note,
                        branchId=observation.branch_id,
                        alternativeGroupId=observation.alternative_group_id,
                        slotId=observation.slot_id,
                        slotRole=observation.slot_role,
                    )
                )
                label_index = _node_index_by_label(list(node_map.values()))
            if host_label and _node_type_hint(node_map, subject_id) == "PRODUCT" and _node_type_hint(node_map, object_id) == "PRODUCT":
                all_steps.append(
                    ProcessStep(
                        stepId=observation.observation_id,
                        hostNodeId=host_id,
                        processKind="transformation" if relation == "TRANSFORMS_INTO" else "assembly",
                        inputNodeIds=[subject_id],
                        outputNodeIds=[object_id],
                        qualifiers=[item for item in observation.qualifiers if not item.startswith("host:")],
                        confidence=observation.confidence,
                        supportType=observation.support_type,
                        provenance=list(observation.provenance),
                        supportNote=observation.support_note,
                        branchId=observation.branch_id,
                        alternativeGroupId=observation.alternative_group_id,
                        slotId=observation.slot_id,
                        slotRole=observation.slot_role,
                    )
                )
                used_observation_ids.add(observation.observation_id)
                continue
        elif relation == "PRODUCES":
            key = (subject_id, observation.branch_id, observation.alternative_group_id)
            if (
                _node_type_hint(node_map, subject_id) in _PROCESS_HOST_ALLOWED_TYPES
                and _node_type_hint(node_map, object_id) == "PRODUCT"
                and observation.observation_id not in used_observation_ids
                and not any(
                    step.host_node_id == subject_id
                    and step.branch_id == observation.branch_id
                    and step.alternative_group_id == observation.alternative_group_id
                    and object_id in step.output_node_ids
                    for step in all_steps
                )
            ):
                all_steps.append(
                    ProcessStep(
                        stepId=observation.observation_id,
                        hostNodeId=subject_id,
                        processKind="transformation",
                        inputNodeIds=[],
                        outputNodeIds=[object_id],
                        qualifiers=list(observation.qualifiers),
                        confidence=observation.confidence,
                        supportType=observation.support_type,
                        provenance=list(observation.provenance),
                        supportNote=observation.support_note,
                        branchId=observation.branch_id,
                        alternativeGroupId=observation.alternative_group_id,
                        slotId=observation.slot_id,
                        slotRole=observation.slot_role,
                    )
                )
                used_observation_ids.add(observation.observation_id)
                continue

        if relation in _OPERATIONAL_RELATIONS and observation.observation_id in used_observation_ids:
            continue
        all_overlays.append(observation)

    return list(node_map.values()), all_legs, all_steps, all_overlays


def graph_state_from_fragments(fragments: list[GraphFragment]) -> OperationalGraphState:
    all_nodes: list[EntityNode] = []
    all_legs: list[FlowLeg] = []
    all_steps: list[ProcessStep] = []
    all_overlays: list[GraphObservation] = []
    for fragment in fragments:
        all_nodes.extend(fragment.entity_nodes)
        all_legs.extend(fragment.flow_legs)
        all_steps.extend(fragment.process_steps)
        all_overlays.extend(fragment.overlay_edges)
        if fragment.observations and not _fragment_has_native_operational_primitives(fragment):
            legacy_nodes, legacy_legs, legacy_steps, legacy_overlays = _legacy_graph_components_from_observations(
                fragment.observations,
                supporting_nodes=fragment.entity_nodes,
            )
            all_nodes.extend(legacy_nodes)
            all_legs.extend(legacy_legs)
            all_steps.extend(legacy_steps)
            all_overlays.extend(legacy_overlays)
    return normalize_graph_state(
        OperationalGraphState(
            entityNodes=all_nodes,
            flowLegs=all_legs,
            processSteps=all_steps,
            overlayEdges=all_overlays,
        )
    )


def node_map(graph: OperationalGraphState) -> dict[str, EntityNode]:
    return {
        node.node_id: node
        for node in normalize_graph_state(graph).entity_nodes
        if node.node_id
    }


def node_label(nodes: dict[str, EntityNode], node_id: str) -> str:
    node = nodes.get(node_id)
    if node is None:
        return node_id
    return node.label or node.canonical_ref or node.node_id


def node_type(nodes: dict[str, EntityNode], node_id: str) -> str:
    node = nodes.get(node_id)
    if node is None:
        return ""
    return node.entity_type


def _derive_flow_leg_observation(leg: FlowLeg, nodes: dict[str, EntityNode]) -> GraphObservation:
    qualifiers = list(leg.qualifiers)
    product_label = node_label(nodes, leg.product_node_id)
    if product_label:
        qualifiers.append(f"product:{product_label}")
    for route_id in leg.route_node_ids:
        qualifiers.append(f"via:{node_label(nodes, route_id)}")
    return GraphObservation(
        observationId=leg.leg_id,
        subjectId=leg.supplier_node_id,
        subject=node_label(nodes, leg.supplier_node_id),
        subjectType=node_type(nodes, leg.supplier_node_id),
        relation="SUPPLIES_TO",
        objectId=leg.customer_node_id,
        object=node_label(nodes, leg.customer_node_id),
        objectType=node_type(nodes, leg.customer_node_id),
        qualifiers=qualifiers,
        confidence=leg.confidence,
        supportType=leg.support_type,
        provenance=list(leg.provenance),
        supportNote=leg.support_note,
        branchId=leg.branch_id,
        alternativeGroupId=leg.alternative_group_id,
        slotId=leg.slot_id,
        slotRole=leg.slot_role,
    )


def _derive_process_step_observations(step: ProcessStep, nodes: dict[str, EntityNode]) -> list[GraphObservation]:
    host_label = node_label(nodes, step.host_node_id)
    host_type = node_type(nodes, step.host_node_id)
    base_qualifiers = list(step.qualifiers)
    if host_label:
        base_qualifiers.append(f"host:{host_label}")

    observations: list[GraphObservation] = []
    for output_id in step.output_node_ids:
        observations.append(
            GraphObservation(
                observationId=f"{step.step_id}-produce-{output_id}",
                subjectId=step.host_node_id,
                subject=host_label,
                subjectType=host_type,
                relation="PRODUCES",
                objectId=output_id,
                object=node_label(nodes, output_id),
                objectType=node_type(nodes, output_id),
                qualifiers=list(base_qualifiers),
                confidence=step.confidence,
                supportType=step.support_type,
                provenance=list(step.provenance),
                supportNote=step.support_note,
                branchId=step.branch_id,
                alternativeGroupId=step.alternative_group_id,
                slotId=step.slot_id,
                slotRole=step.slot_role,
            )
        )

    if step.process_kind == "extraction":
        for input_id in step.input_node_ids:
            observations.append(
                GraphObservation(
                    observationId=f"{step.step_id}-extract-{input_id}",
                    subjectId=step.host_node_id,
                    subject=host_label,
                    subjectType=host_type,
                    relation="EXTRACTS_FROM",
                    objectId=input_id,
                    object=node_label(nodes, input_id),
                    objectType=node_type(nodes, input_id),
                    qualifiers=list(step.qualifiers),
                    confidence=step.confidence,
                    supportType=step.support_type,
                    provenance=list(step.provenance),
                    supportNote=step.support_note,
                    branchId=step.branch_id,
                    alternativeGroupId=step.alternative_group_id,
                    slotId=step.slot_id,
                    slotRole=step.slot_role,
                )
            )
        return observations

    relation = "TRANSFORMS_INTO" if step.process_kind == "transformation" else "ASSEMBLES_INTO"
    for input_id in step.input_node_ids:
        if node_type(nodes, input_id) != "PRODUCT":
            continue
        for output_id in step.output_node_ids:
            if node_type(nodes, output_id) != "PRODUCT":
                continue
            observations.append(
                GraphObservation(
                    observationId=f"{step.step_id}-{relation.lower()}-{input_id}-{output_id}",
                    subjectId=input_id,
                    subject=node_label(nodes, input_id),
                    subjectType=node_type(nodes, input_id),
                    relation=relation,
                    objectId=output_id,
                    object=node_label(nodes, output_id),
                    objectType=node_type(nodes, output_id),
                    qualifiers=list(base_qualifiers),
                    confidence=step.confidence,
                    supportType=step.support_type,
                    provenance=list(step.provenance),
                    supportNote=step.support_note,
                    branchId=step.branch_id,
                    alternativeGroupId=step.alternative_group_id,
                    slotId=step.slot_id,
                    slotRole=step.slot_role,
                )
            )
    return observations


def fragment_observation_projection(fragment: GraphFragment) -> list[GraphObservation]:
    normalized_fragment = fragment.model_copy(deep=True)
    graph = OperationalGraphState(
        entityNodes=list(normalized_fragment.entity_nodes),
        flowLegs=list(normalized_fragment.flow_legs),
        processSteps=list(normalized_fragment.process_steps),
        overlayEdges=list(normalized_fragment.overlay_edges),
    )
    projected = observation_projection(graph)
    if _fragment_has_native_operational_primitives(normalized_fragment):
        return projected
    deduped: OrderedDict[tuple[str, str, str, str, tuple[str, ...]], GraphObservation] = OrderedDict()
    for raw in [*normalized_fragment.overlay_edges, *normalized_fragment.observations, *projected]:
        obs = raw.model_copy(
            update={
                "subject_id": raw.subject_id or synthetic_id(raw.subject_type, raw.subject),
                "object_id": raw.object_id or synthetic_id(raw.object_type, raw.object),
            }
        )
        key = (
            obs.subject_id or obs.subject,
            obs.relation,
            obs.object_id or obs.object,
            obs.branch_id,
            tuple(obs.qualifiers),
        )
        if key not in deduped:
            deduped[key] = obs
    return list(deduped.values())


def observation_projection(graph: OperationalGraphState) -> list[GraphObservation]:
    normalized = normalize_graph_state(graph)
    nodes = node_map(normalized)
    out: list[GraphObservation] = list(normalized.overlay_edges)
    for leg in normalized.flow_legs:
        out.append(_derive_flow_leg_observation(leg, nodes))
    for step in normalized.process_steps:
        out.extend(_derive_process_step_observations(step, nodes))
    deduped: OrderedDict[tuple[str, str, str, str, tuple[str, ...]], GraphObservation] = OrderedDict()
    for raw in out:
        obs = raw.model_copy(
            update={
                "subject_id": raw.subject_id or synthetic_id(raw.subject_type, raw.subject),
                "object_id": raw.object_id or synthetic_id(raw.object_type, raw.object),
            }
        )
        key = (
            obs.subject_id or obs.subject,
            obs.relation,
            obs.object_id or obs.object,
            obs.branch_id,
            tuple(obs.qualifiers),
        )
        if key not in deduped:
            deduped[key] = obs
    return list(deduped.values())


def graph_counts(graph: OperationalGraphState) -> dict[str, int]:
    normalized = normalize_graph_state(graph)
    observations = observation_projection(normalized)
    return {
        "entityNodeCount": len(normalized.entity_nodes),
        "flowLegCount": len(normalized.flow_legs),
        "processStepCount": len(normalized.process_steps),
        "overlayEdgeCount": len(normalized.overlay_edges),
        "graphObservationCount": len(observations),
        "documentObservationCount": sum(1 for obs in observations if obs.support_type == "document"),
        "mixedObservationCount": sum(1 for obs in observations if obs.support_type == "mixed"),
        "groundedFlowLegCount": sum(1 for leg in normalized.flow_legs if leg.support_type in {"document", "mixed"}),
        "groundedProcessStepCount": sum(1 for step in normalized.process_steps if step.support_type in {"document", "mixed"}),
    }


def support_types_for_graph(graph: OperationalGraphState) -> set[str]:
    normalized = normalize_graph_state(graph)
    support_types = {edge.support_type for edge in normalized.overlay_edges}
    support_types |= {leg.support_type for leg in normalized.flow_legs}
    support_types |= {step.support_type for step in normalized.process_steps}
    return {item for item in support_types if item}


def evidence_status_for_graph(graph: OperationalGraphState) -> str:
    support_types = support_types_for_graph(graph)
    if not support_types:
        return "reasoned"
    if support_types == {"document"}:
        return "verified"
    if support_types == {"hypothesis"}:
        return "hypothesis"
    if "document" in support_types and len(support_types) > 1:
        return "mixed"
    return "reasoned"


def flow_leg_line(leg: FlowLeg, nodes: dict[str, EntityNode]) -> str:
    product = node_label(nodes, leg.product_node_id)
    supplier = node_label(nodes, leg.supplier_node_id)
    customer = node_label(nodes, leg.customer_node_id)
    if product:
        return f"{supplier} --[{product}]--> {customer}"
    return f"{supplier} --> {customer}"


def process_step_line(step: ProcessStep, nodes: dict[str, EntityNode]) -> str:
    host = node_label(nodes, step.host_node_id)
    inputs = ", ".join(node_label(nodes, node_id) for node_id in step.input_node_ids[:4] if node_id)
    outputs = ", ".join(node_label(nodes, node_id) for node_id in step.output_node_ids[:4] if node_id)
    verb = {
        "extraction": "extracts",
        "transformation": "transforms",
        "assembly": "assembles",
    }.get(step.process_kind, step.process_kind)
    if inputs and outputs:
        return f"{host} {verb} {inputs} -> {outputs}"
    if outputs:
        return f"{host} {verb} -> {outputs}"
    return f"{host} {verb}"


def overlay_edge_line(observation: GraphObservation) -> str:
    return f"{observation.subject} --{observation.relation}--> {observation.object}"


def graph_context_lines(graph: OperationalGraphState, *, limit: int = 12) -> list[str]:
    normalized = normalize_graph_state(graph)
    nodes = node_map(normalized)
    lines: list[str] = []
    for leg in normalized.flow_legs:
        lines.append(flow_leg_line(leg, nodes))
    for step in normalized.process_steps:
        lines.append(process_step_line(step, nodes))
    for overlay in normalized.overlay_edges:
        lines.append(overlay_edge_line(overlay))
    deduped: list[str] = []
    seen: set[str] = set()
    for line in lines:
        if not line or line in seen:
            continue
        seen.add(line)
        deduped.append(line)
        if len(deduped) >= limit:
            break
    return deduped


def graph_entity_labels(graph: OperationalGraphState, *, limit: int = 24) -> list[str]:
    labels: list[str] = []
    for node in normalize_graph_state(graph).entity_nodes:
        label = (node.label or node.canonical_ref or node.node_id).strip()
        if not label or label in labels:
            continue
        labels.append(label)
        if len(labels) >= limit:
            break
    return labels


def fragment_operational_graph(fragment: GraphFragment) -> OperationalGraphState:
    return graph_state_from_fragments([fragment])

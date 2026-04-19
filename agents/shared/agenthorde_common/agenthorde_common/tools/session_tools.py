"""Session pack tools (retrieve, apply, knowledge_search, extend_cycle_cap) — cloud-agnostic."""

from __future__ import annotations

import json
import math
import re
import uuid
from typing import Any, MutableMapping

from pydantic import ValidationError

from agenthorde_common.knowledge.domain_priors import (
    available_domains as _domain_priors_available,
    lookup as _domain_priors_lookup,
)
from agenthorde_common.runtime.state_keys import STATE_PACK_KEY
from agenthorde_common.schemas.state import ApplyPatchRecord, SessionPack
from agenthorde_common.schemas.tools_io import (
    ApplyToolInput,
    ApplyToolOutput,
    KnowledgeSearchOutput,
    RetrieveToolInput,
    RetrieveToolOutput,
    ToolEnvelope,
)
from agenthorde_common.tools.tool_ledger import append_tool_ledger


def _load_pack(state: MutableMapping[str, Any]) -> SessionPack:
    raw = state.get(STATE_PACK_KEY) or {}
    if isinstance(raw, SessionPack):
        return raw
    try:
        return SessionPack.model_validate(raw)
    except ValidationError:
        return SessionPack()


def _save_pack(state: MutableMapping[str, Any], pack: SessionPack) -> None:
    state[STATE_PACK_KEY] = pack.model_dump(mode="json", by_alias=True)


def ah_knowledge_search(query: str, scope: str = "general", *, state: MutableMapping[str, Any]) -> dict[str, Any]:
    q = (query or "").strip()
    if not q:
        env = ToolEnvelope(ok=False, tool="knowledge_search", status="validation_error", message="empty query")
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    tense = bool(re.search(r"\b(2024|2025|2026|today|latest|current|sanction|export control|embargo)\b", q, re.I))
    out = KnowledgeSearchOutput(
        answer=(
            "Stub knowledge response: treat as non-authoritative narrative memory. "
            "Prefer web_search for time-sensitive or contested facts."
        ),
        stability="low" if tense else "high",
        caveats=["knowledge_search is a stub; verify with web_search or retrieve when needed."],
    )
    env = ToolEnvelope(ok=True, tool="knowledge_search", payload=out.model_dump(mode="json", by_alias=True))
    append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
    return env.model_dump(mode="json", by_alias=True)


def ah_retrieve(
    query: str,
    scope: str = "all",
    limit: int = 24,
    *,
    state: MutableMapping[str, Any],
) -> dict[str, Any]:
    if not str(query or "").strip():
        env = ToolEnvelope(ok=False, tool="retrieve", status="validation_error", message="empty query")
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    try:
        inp = RetrieveToolInput(query=query, scope=scope, limit=limit)  # type: ignore[arg-type]
    except ValidationError as exc:
        env = ToolEnvelope(ok=False, tool="retrieve", status="validation_error", message=str(exc))
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    pack = _load_pack(state)
    qlow = inp.query.lower().strip()
    matches: list[dict[str, Any]] = []

    def _scan(obj: Any, path: str) -> None:
        if len(matches) >= inp.limit:
            return
        if isinstance(obj, dict):
            blob = json.dumps(obj, ensure_ascii=False)[:2000]
            if qlow and qlow in blob.lower():
                matches.append({"path": path, "preview": blob[:800]})
            for k, v in list(obj.items())[:80]:
                _scan(v, f"{path}/{k}")
        elif isinstance(obj, list):
            for i, v in enumerate(obj[:80]):
                _scan(v, f"{path}[{i}]")

    if inp.scope in ("all", "working_graph"):
        _scan(pack.working_graph, "workingGraph")
    if inp.scope in ("all", "reasoning_graph"):
        rg = pack.reasoning_graph.model_dump(mode="json", by_alias=True)
        _scan(rg, "reasoningGraph")

    out = RetrieveToolOutput(matches=matches[: inp.limit], scope=inp.scope)
    env = ToolEnvelope(ok=True, tool="retrieve", payload=out.model_dump(mode="json", by_alias=True))
    append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
    return env.model_dump(mode="json", by_alias=True)


def ah_apply(
    patch_json: str,
    hypothesis_id: str,
    *,
    state: MutableMapping[str, Any],
) -> dict[str, Any]:
    try:
        body = json.loads(patch_json) if patch_json else {}
    except json.JSONDecodeError as exc:
        env = ToolEnvelope(ok=False, tool="apply", status="validation_error", message=f"invalid json: {exc}")
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    try:
        inp = ApplyToolInput(
            hypothesis_id=hypothesis_id or str(body.get("hypothesisId") or ""),
            patch=body if isinstance(body, dict) else {},
        )
    except ValidationError as exc:
        env = ToolEnvelope(ok=False, tool="apply", status="validation_error", message=str(exc))
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    pack = _load_pack(state)
    idx = len(pack.apply_log)
    rec = ApplyPatchRecord(
        schema_version=inp.schema_version,
        hypothesis_id=inp.hypothesis_id,
        patch=inp.patch,
    )
    pack.apply_log.append(rec)

    nodes = inp.patch.get("nodes")
    if isinstance(nodes, list):
        if "nodes" not in pack.working_graph:
            pack.working_graph["nodes"] = []
        wg_nodes = pack.working_graph["nodes"]
        if isinstance(wg_nodes, list):
            for n in nodes:
                if isinstance(n, dict):
                    if not n.get("id"):
                        n = {**n, "id": str(uuid.uuid4())[:12]}
                    wg_nodes.append(n)

    _save_pack(state, pack)
    out = ApplyToolOutput(
        applied_index=idx,
        working_graph_node_count=len(pack.working_graph.get("nodes") or []) if isinstance(pack.working_graph, dict) else 0,
    )
    env = ToolEnvelope(ok=True, tool="apply", payload=out.model_dump(mode="json", by_alias=True))
    append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
    return env.model_dump(mode="json", by_alias=True)


def ah_domain_priors_lookup(
    domain: str,
    query: str = "",
    *,
    state: MutableMapping[str, Any],
) -> dict[str, Any]:
    """Industrial dependency-depth hints for a domain (callable analyst tool).

    See :mod:`agenthorde_common.knowledge.domain_priors` for the underlying
    dictionary. Returns the lookup result wrapped in a :class:`ToolEnvelope`
    and persisted into the session tool ledger so the critic can audit which
    priors the analyzer actually consulted.

    The tool is intentionally **opt-in**: the analyzer decides when (and
    whether) it needs an industrial hint, so unrelated questions do not pay
    the token cost of the priors block. Unknown ``domain`` returns an empty
    hint list plus the available domain catalog as a self-correction signal.
    """

    dom = (domain or "").strip()
    if not dom:
        env = ToolEnvelope(
            ok=False,
            tool="domain_priors_lookup",
            status="validation_error",
            message="empty domain (call with one of: " + ", ".join(_domain_priors_available()) + ")",
            payload={"available_domains": _domain_priors_available()},
        )
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)

    payload = _domain_priors_lookup(dom, query)
    ok = bool(payload.get("hints")) or "available_domains" not in payload
    env = ToolEnvelope(ok=ok, tool="domain_priors_lookup", payload=payload)
    append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
    return env.model_dump(mode="json", by_alias=True)


def extend_cycle_cap(
    reason: str,
    uncovered_families: list[str] | None,
    *,
    state: MutableMapping[str, Any],
) -> dict[str, Any]:
    pack = _load_pack(state)
    remaining = max(0, 3 - int(pack.extensions_used or 0))
    if remaining <= 0:
        env = ToolEnvelope(
            ok=False,
            tool="extend_cycle_cap",
            status="validation_error",
            message="cap already extended 3 times this run",
            payload={
                "extensionsUsed": int(pack.extensions_used or 0),
                "supervisorCap": int(pack.supervisor_cap or 0),
            },
        )
        append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
        return env.model_dump(mode="json", by_alias=True)
    base = max(1, int(pack.initial_supervisor_cap or pack.supervisor_cap or 1))
    step = max(1, math.ceil(base * 0.1))
    pack.supervisor_cap = int(pack.supervisor_cap) + step
    pack.extensions_used = int(pack.extensions_used or 0) + 1
    pack.extensions_pending_red_team = True
    _save_pack(state, pack)
    payload = {
        "extensionsUsed": int(pack.extensions_used),
        "supervisorCap": int(pack.supervisor_cap),
        "initialSupervisorCap": int(pack.initial_supervisor_cap or base),
        "addedCycles": int(step),
        "reason": str(reason or "").strip(),
        "uncoveredFamilies": list(uncovered_families or []),
        "redTeamGate": True,
    }
    env = ToolEnvelope(ok=True, tool="extend_cycle_cap", payload=payload)
    append_tool_ledger(state, env.model_dump(mode="json", by_alias=True))
    return env.model_dump(mode="json", by_alias=True)

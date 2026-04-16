"""Shared context packing (critic, future supervisor bundles)."""

from __future__ import annotations

from typing import Any


def pack_critic_bundle(
    mission: dict[str, Any],
    selected_roles: list[str],
    active_role_contracts: list[dict[str, Any]],
    corpus_index: dict[str, Any],
    reachability: dict[str, Any],
    *,
    question_intent: dict[str, Any] | None,
    intent_meta: dict[str, Any] | None,
    investigation_todos_compact: list[dict[str, Any]],
    shared_memory_compact: dict[str, Any],
    investigation_preflight: dict[str, Any] | None = None,
    red_team_artifact: dict[str, Any] | None = None,
    red_team_satisfied: bool = False,
) -> dict[str, Any]:
    return {
        "mission": mission,
        "selectedRoles": selected_roles,
        "activeRoleContracts": active_role_contracts,
        "corpusIndex": corpus_index,
        "reachability": reachability,
        "questionIntent": question_intent or {},
        "intentExtractionMeta": intent_meta or {},
        "investigationTodos": investigation_todos_compact,
        "sharedMemory": shared_memory_compact,
        "investigationPreflight": investigation_preflight or {},
        "redTeamArtifact": red_team_artifact or {},
        "redTeamSatisfied": red_team_satisfied,
    }

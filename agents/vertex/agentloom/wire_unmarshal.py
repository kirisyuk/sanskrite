"""Strict wire JSON helpers for AgentLoom (schemaVersion + recovery path for abnormal LLM text)."""

from __future__ import annotations

import json
import re
from typing import Any, TypeVar

from pydantic import BaseModel

from agentloom.contracts import AGENTLOOM_WIRE_SCHEMA_VERSION

T = TypeVar("T", bound=BaseModel)


def strip_json_fence(text: str) -> str:
    stripped = text.strip()
    if stripped.startswith("```"):
        stripped = re.sub(r"^```(?:json)?\s*", "", stripped)
        stripped = re.sub(r"\s*```$", "", stripped)
    return stripped.strip()


def recover_wire_json_text(text: str) -> str:
    """Non-normative recovery: strip fences and take the outermost JSON object substring."""
    stripped = strip_json_fence(text)
    start = stripped.find("{")
    end = stripped.rfind("}")
    if start >= 0 and end > start:
        return stripped[start : end + 1]
    return stripped


def parse_wire_json(text: str) -> Any:
    return json.loads(recover_wire_json_text(text))


def assert_wire_schema_version(payload: dict[str, Any], *, model_name: str) -> None:
    v = payload.get("schemaVersion", AGENTLOOM_WIRE_SCHEMA_VERSION)
    if v != AGENTLOOM_WIRE_SCHEMA_VERSION:
        raise ValueError(f"{model_name}: unsupported schemaVersion {v!r} (expected {AGENTLOOM_WIRE_SCHEMA_VERSION})")


def unmarshal_wire_model(model_cls: type[T], text: str, *, context: str = "") -> T:
    """Strict validate after optional text recovery. Rejects wrong schemaVersion before Pydantic."""
    try:
        data = parse_wire_json(text)
    except (json.JSONDecodeError, ValueError) as exc:
        raise ValueError(f"{model_cls.__name__}: invalid JSON{context}: {exc}") from exc
    if not isinstance(data, dict):
        raise ValueError(f"{model_cls.__name__}: expected JSON object, got {type(data).__name__}{context}")
    assert_wire_schema_version(data, model_name=model_cls.__name__)
    return model_cls.model_validate(data)

from __future__ import annotations

from typing import Literal

import pytest
from pydantic import BaseModel, ConfigDict, Field

from agenthorde.critic.strict_schema import to_openai_strict_schema
from agenthorde_common.schemas.agents_io import CriticVerdict


class _Inner(BaseModel):
    model_config = ConfigDict(extra="forbid")

    alpha: int = 0
    beta: str = ""


class _Outer(BaseModel):
    model_config = ConfigDict(extra="forbid")

    inner: _Inner = Field(default_factory=_Inner)
    tag: Literal["a", "b"] = "a"
    things: list[str] = Field(default_factory=list)


class TestStrictSchemaNormalization:
    def test_top_level_object_is_tightened(self) -> None:
        schema = to_openai_strict_schema(CriticVerdict.model_json_schema(by_alias=True))
        assert schema["type"] == "object"
        assert schema["additionalProperties"] is False
        assert set(schema["required"]) == set(schema["properties"].keys())

    def test_defaults_are_stripped(self) -> None:
        raw = CriticVerdict.model_json_schema(by_alias=True)
        assert any(
            isinstance(p, dict) and "default" in p
            for p in raw["properties"].values()
        ), "pydantic schema must have defaults for this test to be meaningful"
        schema = to_openai_strict_schema(raw)
        for prop in schema["properties"].values():
            assert "default" not in prop

    def test_refs_are_inlined(self) -> None:
        raw = _Outer.model_json_schema()
        assert "$defs" in raw
        schema = to_openai_strict_schema(raw)
        assert "$defs" not in schema
        inner = schema["properties"]["inner"]
        assert inner.get("type") == "object"
        assert inner["additionalProperties"] is False
        assert set(inner["required"]) == {"alpha", "beta"}

    @pytest.mark.parametrize("key", ["minItems", "maxItems", "minLength", "pattern"])
    def test_incompatible_keywords_are_removed(self, key: str) -> None:
        raw = {
            "type": "object",
            "properties": {
                "x": {"type": "array", "items": {"type": "string"}, key: 1},
            },
        }
        schema = to_openai_strict_schema(raw)
        assert key not in schema["properties"]["x"]

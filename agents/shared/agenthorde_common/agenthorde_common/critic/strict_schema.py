"""Normalize a Pydantic v2 JSON schema for OpenAI / OpenRouter strict mode.

OpenAI's structured-output "strict" mode (which OpenRouter forwards to the
selected provider) rejects several JSON-Schema keywords that Pydantic
emits by default, and additionally requires every field of every object
to be listed in `required` (no implicit-optional fields).

This module walks the schema, inlines `$ref` targets from `$defs`, drops
keywords that strict mode does not accept, forces `additionalProperties:
false` everywhere, and promotes every property to `required`.

The normalized schema is still a valid JSON Schema; the only semantic
change relative to Pydantic's output is "everything is required". That
is acceptable because our critic model is prompted to produce default
values for unknown fields rather than omit them.
"""

from __future__ import annotations

from copy import deepcopy
from typing import Any

_STRICT_INCOMPATIBLE_KEYS: frozenset[str] = frozenset(
    {
        # OpenAI strict mode rejects these at validation time.
        "default",
        "examples",
        "format",
        "exclusiveMinimum",
        "exclusiveMaximum",
        "minLength",
        "maxLength",
        "minItems",
        "maxItems",
        "pattern",
        "patternProperties",
        "unevaluatedProperties",
        "uniqueItems",
        "multipleOf",
        # Anthropic structured-output (proxied via OpenRouter) rejects these
        # on number / integer types: "For 'number' type, properties maximum,
        # minimum are not supported". OpenAI accepts them but they are not
        # essential for prompt-level conformance — Pydantic re-validates on
        # the client side, so dropping them here keeps cross-provider parity.
        "minimum",
        "maximum",
        # Pydantic-specific metadata.
        "discriminator",
    }
)


def _inline_refs(node: Any, defs: dict[str, Any]) -> Any:
    """Replace every `{"$ref": "#/$defs/X"}` with a deep copy of `$defs["X"]`."""

    if isinstance(node, dict):
        if "$ref" in node and isinstance(node["$ref"], str):
            ref = node["$ref"]
            prefix = "#/$defs/"
            if ref.startswith(prefix):
                key = ref[len(prefix) :]
                if key in defs:
                    target = deepcopy(defs[key])
                    return _inline_refs(target, defs)
            return deepcopy(node)
        return {k: _inline_refs(v, defs) for k, v in node.items() if k != "$defs"}
    if isinstance(node, list):
        return [_inline_refs(v, defs) for v in node]
    return node


def _strip_incompatible(node: Any) -> Any:
    if isinstance(node, dict):
        out: dict[str, Any] = {}
        for k, v in node.items():
            if k in _STRICT_INCOMPATIBLE_KEYS:
                continue
            out[k] = _strip_incompatible(v)
        return out
    if isinstance(node, list):
        return [_strip_incompatible(v) for v in node]
    return node


def _tighten_objects(node: Any) -> Any:
    """Force `additionalProperties: false` and `required = all keys` on every object."""

    if isinstance(node, dict):
        # Descend first so nested objects are tightened before the outer.
        out = {k: _tighten_objects(v) for k, v in node.items()}
        node_type = out.get("type")
        if node_type == "object" or (isinstance(node_type, list) and "object" in node_type):
            props = out.get("properties")
            if isinstance(props, dict):
                out["required"] = list(props.keys())
                out["additionalProperties"] = False
        return out
    if isinstance(node, list):
        return [_tighten_objects(v) for v in node]
    return node


def to_openai_strict_schema(raw_schema: dict[str, Any]) -> dict[str, Any]:
    """Return a strict-mode-safe JSON Schema derived from a Pydantic model schema.

    Parameters
    ----------
    raw_schema
        The schema produced by `pydantic.BaseModel.model_json_schema()`.

    Returns
    -------
    dict
        A JSON Schema suitable for `response_format.json_schema.schema`
        with `strict: true`.
    """

    defs = raw_schema.get("$defs") or raw_schema.get("definitions") or {}
    inlined = _inline_refs(raw_schema, defs)
    cleaned = _strip_incompatible(inlined)
    tightened = _tighten_objects(cleaned)
    # The top-level schema object itself must be an object with fixed shape.
    if isinstance(tightened, dict) and tightened.get("type") == "object":
        tightened.setdefault("additionalProperties", False)
    return tightened

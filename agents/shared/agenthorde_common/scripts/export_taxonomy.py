"""Export the AgentHorde taxonomy (SoT) to JSON for Go/TS codegen.

Single source of truth lives in
``agenthorde_common.canonicalizer.taxonomy``; this script dumps a stable,
diff-friendly JSON snapshot consumed by:

    * backend codegen (`go generate ./...` -> `taxonomy_generated.go`)
    * web codegen   (`npm run gen:taxonomy` -> `taxonomy.generated.ts`)
    * docs / lint   (`docs/taxonomy/*` reference material, CI guards)

The output schema is intentionally explicit and stable; producers are free to
add new top-level keys but MUST NOT remove or rename existing ones without
updating every consumer in the same commit.

Usage::

    python -m scripts.export_taxonomy [--out shared/taxonomy/taxonomy.json] [--check]

``--check`` exits non-zero if the on-disk file does not match what would be
emitted (used by CI to keep generated artifacts in sync).
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

from agenthorde_common.canonicalizer.taxonomy import (
    ATTRIBUTE_SPEC,
    DECISION_LAYER_FRAGMENT_CLASSES,
    FRAGMENT_CLASSES,
    INCOTERMS_2020,
    RELATION_ALLOWED_LAYERS,
    RELATION_ENDPOINT_BLACKLIST,
    RELATION_ENDPOINT_KINDS,
    RELATION_LAYER_SECTION,
    RELATION_NAMES,
    SANCTIONS_REGIMES_CATALOG,
    STRUCTURAL_FRAGMENT_CLASSES,
    SUBTYPE_ENUMS,
)

# Stable enum -> canonical taxonomy.py constant name. Keep in sync with
# SUBTYPE_ENUMS (which is the runtime view); this map is what codegen
# consumers see in the JSON.
_ENUM_DESCRIPTIONS: dict[str, str] = {
    "CommunitySubtype": "Closed enum, slot 3 of /community/{geo}/{subtype}/{slug}.",
    "ProductFamily": "Closed enum, slot 2 of /product/{family}/{slug}.",
    "EvidenceSubtype": "Closed enum, prefix of slot 3 of /evidence/{iso2}/{subtype}-{slug}.",
    "MechanismCategory": "Closed enum, slot 2 of /mechanism/{category}/{slug}.",
    "DecisionSubtype": "Closed enum, attribute on /decision fragments (no path slot).",
    "InfluenceKind": "Closed enum, required `influence_kind` attribute on `influences` edges.",
    "AssuranceType": "Closed enum, required `assurance_type` attribute on `assures` edges.",
}


def _sorted_pairs(table: dict[str, dict[tuple[str, str], str]]) -> dict[str, list[dict[str, str]]]:
    out: dict[str, list[dict[str, str]]] = {}
    for relation in sorted(table.keys()):
        rows: list[dict[str, str]] = []
        for (frm, to), kind in sorted(table[relation].items()):
            rows.append({"from": frm, "to": to, "kind": kind})
        out[relation] = rows
    return out


def _sorted_blacklist(table: dict[str, frozenset[tuple[str, str]]]) -> dict[str, list[dict[str, str]]]:
    out: dict[str, list[dict[str, str]]] = {}
    for relation in sorted(table.keys()):
        rows: list[dict[str, str]] = []
        for (frm, to) in sorted(table[relation]):
            rows.append({"from": frm, "to": to})
        out[relation] = rows
    return out


def build_payload() -> dict[str, Any]:
    return {
        "schema_version": "2.0",
        "fragment_classes": {
            "all": sorted(FRAGMENT_CLASSES),
            "structural": sorted(STRUCTURAL_FRAGMENT_CLASSES),
            "decision_layer": sorted(DECISION_LAYER_FRAGMENT_CLASSES),
        },
        "subtype_enums": {
            name: {
                "description": _ENUM_DESCRIPTIONS.get(name, ""),
                "values": sorted(values),
            }
            for name, values in sorted(SUBTYPE_ENUMS.items())
        },
        "relations": {
            "names": sorted(RELATION_NAMES),
            "allowed_layers": {
                rel: sorted(layers)
                for rel, layers in sorted(RELATION_ALLOWED_LAYERS.items())
            },
            "layer_sections": dict(sorted(RELATION_LAYER_SECTION.items())),
            "endpoint_kinds": _sorted_pairs(RELATION_ENDPOINT_KINDS),
            "endpoint_blacklist": _sorted_blacklist(RELATION_ENDPOINT_BLACKLIST),
        },
        "incoterms": sorted(INCOTERMS_2020),
        "sanctions_regimes": {
            code: {
                "canonical_path": str(spec.get("canonical_path") or ""),
                "label": str(spec.get("label") or ""),
                "jurisdiction_iso2": str(spec.get("jurisdiction_iso2") or ""),
                "aliases": list(spec.get("aliases") or ()),
            }
            for code, spec in sorted(SANCTIONS_REGIMES_CATALOG.items())
        },
        "attribute_spec": [
            {
                "name": str(spec["name"]),
                "carrier": str(spec["carrier"]),
                "applies_to": list(spec.get("applies_to") or []),
                "type": str(spec.get("type") or ""),
                "required": bool(spec.get("required") or False),
                "description": str(spec.get("description") or ""),
            }
            for spec in ATTRIBUTE_SPEC
        ],
    }


def _serialize(payload: dict[str, Any]) -> str:
    # Deterministic, diff-friendly: sorted keys (where applicable above), 2-space
    # indent, trailing newline. Lists are kept in their declaration order so
    # ATTRIBUTE_SPEC reads top-down as authored.
    return json.dumps(payload, indent=2, ensure_ascii=False, sort_keys=False) + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--out",
        type=Path,
        default=Path("shared/taxonomy/taxonomy.json"),
        help="Path to write the JSON dump (default: shared/taxonomy/taxonomy.json).",
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="Exit non-zero if --out exists but does not match the freshly built payload.",
    )
    args = parser.parse_args(argv)

    payload = build_payload()
    rendered = _serialize(payload)

    out: Path = args.out
    if args.check:
        if not out.exists():
            sys.stderr.write(f"export_taxonomy: --check failed; {out} does not exist\n")
            return 2
        existing = out.read_text(encoding="utf-8")
        if existing != rendered:
            sys.stderr.write(
                f"export_taxonomy: --check failed; {out} is stale. "
                "Re-run `python -m scripts.export_taxonomy` and commit the result.\n"
            )
            return 2
        return 0

    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(rendered, encoding="utf-8")
    sys.stdout.write(f"wrote {out} ({len(rendered)} bytes)\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

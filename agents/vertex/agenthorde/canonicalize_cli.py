#!/usr/bin/env python3
"""Run the AgentHorde canonicalizer on a JSON dump (no full agent run)."""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from typing import Any

PACKAGE_DIR = os.path.dirname(os.path.abspath(__file__))
if PACKAGE_DIR not in sys.path:
    sys.path.insert(0, PACKAGE_DIR)


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Canonicalize a session dump: JSON with pack, analyzerOutput, governanceOutput, toolLedger, finalAnswer."
    )
    p.add_argument(
        "--input-file",
        required=True,
        help="Path to JSON (CanonicalizerInput shape or superset).",
    )
    p.add_argument(
        "--compact",
        action="store_true",
        help="Print minified JSON (default is indented).",
    )
    return p.parse_args()


def _load_input(path: str) -> dict[str, Any]:
    with open(path, encoding="utf-8") as f:
        raw = json.load(f)
    if not isinstance(raw, dict):
        raise SystemExit("Input must be a JSON object")
    return raw


async def _run(path: str) -> dict[str, Any]:
    from agenthorde.canonicalizer import canonicalize
    from agenthorde.canonicalizer.schemas import CanonicalizerInput

    blob = _load_input(path)
    cin = CanonicalizerInput.model_validate(blob)
    res = await canonicalize(cin)
    return res.model_dump(mode="json", by_alias=True)


def main() -> None:
    args = parse_args()
    out = asyncio.run(_run(args.input_file))
    if args.compact:
        print(json.dumps(out, ensure_ascii=False, separators=(",", ":")))
    else:
        print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()

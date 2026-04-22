"""AgentHorde post-run canonicalizer: 9-phase pipeline to Horizon-shaped graph."""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from agenthorde_common.canonicalizer.schemas import CanonicalizerInput, CanonicalizerResult

__all__ = [
    "CANONICALIZER_VERSION",
    "CanonicalizerInput",
    "CanonicalizerResult",
    "canonicalize",
    "run_pipeline",
]


def __getattr__(name: str):
    if name == "CANONICALIZER_VERSION":
        from agenthorde_common.canonicalizer.version import CANONICALIZER_VERSION

        return CANONICALIZER_VERSION
    if name == "CanonicalizerInput":
        from agenthorde_common.canonicalizer.schemas import CanonicalizerInput

        return CanonicalizerInput
    if name == "CanonicalizerResult":
        from agenthorde_common.canonicalizer.schemas import CanonicalizerResult

        return CanonicalizerResult
    if name == "canonicalize":
        from agenthorde_common.canonicalizer.pipeline import canonicalize as fn

        return fn
    if name == "run_pipeline":
        from agenthorde_common.canonicalizer.pipeline import run_pipeline as fn

        return fn
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")

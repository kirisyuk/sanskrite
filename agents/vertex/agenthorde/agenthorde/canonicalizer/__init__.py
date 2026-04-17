"""AgentHorde post-run canonicalizer: 9-phase pipeline to Horizon-shaped graph."""

from __future__ import annotations

from agenthorde.canonicalizer.schemas import CanonicalizerInput, CanonicalizerResult
from agenthorde.canonicalizer.version import CANONICALIZER_VERSION

__all__ = [
    "CANONICALIZER_VERSION",
    "CanonicalizerInput",
    "CanonicalizerResult",
    "canonicalize",
    "run_pipeline",
]


def __getattr__(name: str):
    if name == "canonicalize":
        from agenthorde.canonicalizer.pipeline import canonicalize as fn

        return fn
    if name == "run_pipeline":
        from agenthorde.canonicalizer.pipeline import run_pipeline as fn

        return fn
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")

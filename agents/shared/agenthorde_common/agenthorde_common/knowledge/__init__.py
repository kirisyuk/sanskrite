"""Callable knowledge surface for analyzers / critic.

These priors used to live inline in system prompts (one block per domain),
which made the bench biased toward whichever industries we had typed up and
hid cross-runtime cost (every analyzer call paid for the same long block,
every cycle, regardless of relevance).

They now live as a callable lookup so:
* the model decides whether it needs an industrial hint at all,
* unused domains do not consume input tokens,
* the priors are inspectable as a versioned artifact rather than embedded prose.

See :func:`agenthorde_common.knowledge.domain_priors.lookup`.
"""

from agenthorde_common.knowledge.domain_priors import (
    DOMAIN_PRIORS,
    available_domains,
    lookup,
)

__all__ = ["DOMAIN_PRIORS", "available_domains", "lookup"]

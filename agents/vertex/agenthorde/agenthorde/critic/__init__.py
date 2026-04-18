"""Critic fallback subsystem.

Top-level flow:
1. Attempt the Vertex-native ADK `critic_agent` (primary path).
2. If that fails (quota, schema mismatch, empty output), fall through an
   ordered chain of OpenRouter-hosted models (GLM / DeepSeek R1 / Nemotron
   Super / DeepSeek V3.1 by default). Each attempt is wrapped in strict
   JSON-Schema output and validated against `CriticVerdict`.
3. Every attempt is logged as one line in the critic A/B log for offline
   comparison.

All public helpers are re-exported here so call-sites only import from
`agenthorde.critic`.
"""

from agenthorde.critic.ab_log import CriticAttemptLog, append_ab_log
from agenthorde.critic.fallback import run_critic_with_fallback
from agenthorde.critic.openrouter_client import (
    OpenRouterCriticClient,
    OpenRouterCriticError,
)
from agenthorde.critic.prompt import render_critic_prompt
from agenthorde.critic.strict_schema import to_openai_strict_schema

__all__ = [
    "CriticAttemptLog",
    "OpenRouterCriticClient",
    "OpenRouterCriticError",
    "append_ab_log",
    "render_critic_prompt",
    "run_critic_with_fallback",
    "to_openai_strict_schema",
]

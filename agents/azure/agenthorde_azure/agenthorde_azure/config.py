"""Azure OpenAI runtime settings (env). Canonicalizer env vars live in ``agenthorde_common.config``."""

from __future__ import annotations

import os

AZURE_OPENAI_ENDPOINT = os.getenv("AZURE_OPENAI_ENDPOINT", "").rstrip("/")
AZURE_OPENAI_API_KEY = os.getenv("AZURE_OPENAI_API_KEY", "").strip()
AZURE_OPENAI_DEPLOYMENT = os.getenv("AZURE_OPENAI_DEPLOYMENT", "").strip()
AZURE_OPENAI_API_VERSION = os.getenv("AZURE_OPENAI_API_VERSION", "2025-03-01-preview").strip()

# Microsoft Foundry project API base (optional; same API key as OpenAI resource today).
# Example: https://<resource>.services.ai.azure.com/api/projects/<project-id>
AZURE_FOUNDRY_PROJECT_ENDPOINT = os.getenv("AZURE_FOUNDRY_PROJECT_ENDPOINT", "").rstrip("/")

AZURE_SUPERVISOR_DEPLOYMENT = os.getenv("AZURE_SUPERVISOR_DEPLOYMENT", "").strip() or AZURE_OPENAI_DEPLOYMENT
AZURE_WORKER_DEPLOYMENT = os.getenv("AZURE_WORKER_DEPLOYMENT", "").strip() or AZURE_OPENAI_DEPLOYMENT
AZURE_CRITIC_DEPLOYMENT = os.getenv("AZURE_CRITIC_DEPLOYMENT", "").strip() or AZURE_OPENAI_DEPLOYMENT

# Optional: embedding deployment name (same resource/key as chat).
AZURE_EMBEDDING_DEPLOYMENT = os.getenv("AZURE_EMBEDDING_DEPLOYMENT", "").strip()

MAX_SUPERVISOR_CYCLES = int(
    os.getenv("AGENTHORDE_MAX_SUPERVISOR_CYCLES", os.getenv("AZURE_MAX_SUPERVISOR_CYCLES", "8"))
)


def _parse_csv_env(name: str, default: str) -> list[str]:
    raw = os.getenv(name, default)
    return [item.strip() for item in raw.split(",") if item.strip()]


CRITIC_FALLBACK_MODELS: list[str] = _parse_csv_env(
    "AGENTHORDE_CRITIC_FALLBACKS",
    "openai/o4-mini,z-ai/glm-5.1,deepseek/deepseek-chat-v3.1,nvidia/nemotron-3-super-120b-a12b",
)
CRITIC_OR_STRICT: bool = os.getenv("AGENTHORDE_CRITIC_OR_STRICT", "1") not in ("0", "false", "False")
CRITIC_OR_MAX_OUTPUT_TOKENS: int = int(os.getenv("AGENTHORDE_CRITIC_OR_MAX_OUTPUT_TOKENS", "6144"))
CRITIC_OR_MODEL_OVERRIDES: dict[str, dict] = {
    "openai/o4-mini": {
        "temperature": None,
        "extra_body": {"reasoning": {"effort": "low"}},
    },
}
CRITIC_AB_LOG_PATH: str = os.getenv(
    "AGENTHORDE_CRITIC_AB_LOG",
    "/tmp/agenthorde_smoke/critic_ab.jsonl",
)

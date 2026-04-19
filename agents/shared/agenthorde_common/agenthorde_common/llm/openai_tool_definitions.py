"""OpenAI-style ``chat.completions`` function tools for AgentHorde (dispatch: ``tools.openai_function_dispatch``)."""

from __future__ import annotations

WORKER_OPENAI_TOOLS: list[dict[str, object]] = [
    {
        "type": "function",
        "function": {
            "name": "web_search",
            "description": "Search the public web for time-sensitive or source-backed facts. Returns answer text and source URLs.",
            "parameters": {
                "type": "object",
                "properties": {"search_query": {"type": "string", "description": "Focused search query"}},
                "required": ["search_query"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "knowledge_search",
            "description": "Stub stable-knowledge lookup (textbook mechanics). Prefer web_search for fresh facts.",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string"},
                    "scope": {"type": "string", "default": "general"},
                },
                "required": ["query"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "retrieve",
            "description": "Search the persisted working_graph / reasoning_graph in the session pack.",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string"},
                    "scope": {"type": "string", "description": "all | working_graph | reasoning_graph"},
                    "limit": {"type": "integer", "default": 24},
                },
                "required": ["query"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "apply",
            "description": "Apply a JSON patch to the session pack (e.g. append reasoning nodes to working_graph).",
            "parameters": {
                "type": "object",
                "properties": {
                    "patch_json": {"type": "string", "description": "JSON string of the patch object"},
                    "hypothesis_id": {"type": "string", "default": ""},
                },
                "required": ["patch_json"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "domain_priors_lookup",
            "description": (
                "Look up industrial dependency-depth priors for a domain. Use when the analysis "
                "names a Tier-1 surface (e.g. ASML, TSMC, a strait, an API maker) and you want to "
                "know the typical deeper dependency that tends to be the actual chokepoint. "
                "Available domains: semiconductors, critical_minerals, energy_seaborne, "
                "shipping_governance, pharma_api, price_reporting. Returns hint blocks; do not "
                "treat as authoritative — verify with web_search before claiming current facts."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "domain": {
                        "type": "string",
                        "description": (
                            "Domain key. Use one of: semiconductors, critical_minerals, "
                            "energy_seaborne, shipping_governance, pharma_api, price_reporting."
                        ),
                    },
                    "query": {
                        "type": "string",
                        "description": (
                            "Optional surface entity / phenomenon name to filter hints "
                            "(e.g. 'ASML EUV', 'rare earths', 'Russian crude'). "
                            "Empty string returns all hints in the domain."
                        ),
                        "default": "",
                    },
                },
                "required": ["domain"],
            },
        },
    },
]

CRITIC_OPENAI_TOOLS: list[dict[str, object]] = WORKER_OPENAI_TOOLS

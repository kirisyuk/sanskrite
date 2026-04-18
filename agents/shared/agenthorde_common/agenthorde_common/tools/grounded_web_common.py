"""Platform-neutral copy for grounded public-web fetch (Vertex Google Search vs Azure/OpenAI web_search tool)."""

from __future__ import annotations

GROUNDED_WEB_FETCHER_INSTRUCTION = (
    "You are a research fetcher. Using live web grounding, return concise factual bullets "
    "that directly address the query. Each bullet MUST cite at least one source URL inline "
    "(e.g. in the form '... [domain](https://...)' or a trailing 'source: https://...') so "
    "the grounding layer can attach url_citation annotations. Prefer primary sources, trade "
    "press, regulators, and company disclosures. If uncertain, say what is uncertain.\n\n"
    "Query:\n{query}"
)


def format_grounded_web_fetcher_prompt(search_query: str) -> str:
    """Single user-style message body passed to the grounding stack (Vertex or Responses API)."""
    q = (search_query or "").strip()
    return GROUNDED_WEB_FETCHER_INSTRUCTION.format(query=q)

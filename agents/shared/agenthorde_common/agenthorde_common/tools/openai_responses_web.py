"""Parse OpenAI Responses API objects (Azure OpenAI / Foundry or OpenAI) for web-grounded answers."""

from __future__ import annotations

import re
from typing import Any
from urllib.parse import urlparse

# Azure o-series models are inconsistent about emitting ``url_citation`` annotations even when
# ``web_search`` was clearly invoked: some runs put URLs only in inline markdown (``[title](url)``)
# or as raw ``https://…``. We therefore extract both annotation- and text-level citations.
_MD_LINK_RE = re.compile(r"\[([^\]\n]{1,200})\]\((https?://[^\s)]+)\)")
_BARE_URL_RE = re.compile(r"(?<![\(\[\w])(https?://[^\s\]\)>\"'`]+)")


def output_text_from_openai_response(response: Any) -> str:
    """Aggregate model-visible text (same semantics as ``response.output_text`` on typed clients)."""
    text = getattr(response, "output_text", None)
    if isinstance(text, str):
        return text.strip()
    return ""


def _safe_domain(url: str) -> str:
    try:
        return urlparse(url).netloc or ""
    except ValueError:
        return ""


def _add_citation(
    out: list[dict[str, Any]], seen: set[str], *, url: str, title: str, limit: int
) -> bool:
    """Append a deduped ``{title,url,domain}`` record; return False when the ``limit`` is hit."""
    url = (url or "").strip().rstrip(".,;)")
    if not url or url in seen:
        return True
    seen.add(url)
    out.append({"title": (title or "").strip(), "url": url, "domain": _safe_domain(url)})
    return len(out) < limit


def citations_from_openai_response(response: Any, *, limit: int = 24) -> list[dict[str, Any]]:
    """Harvest sources from Responses API output.

    Priority order:
      1. ``url_citation`` annotations on ``message.content[].annotations`` (canonical path)
      2. Inline markdown links ``[title](https://…)`` inside ``output_text`` (fallback for o-series
         runs that skip annotation objects)
      3. Bare ``https://…`` URLs in ``output_text`` (last-resort fallback)
    """
    out: list[dict[str, Any]] = []
    seen: set[str] = set()

    text_blobs: list[str] = []
    for item in getattr(response, "output", None) or []:
        if getattr(item, "type", None) != "message":
            continue
        for part in getattr(item, "content", None) or []:
            if getattr(part, "type", None) != "output_text":
                continue
            txt = getattr(part, "text", None)
            if isinstance(txt, str) and txt:
                text_blobs.append(txt)
            for ann in getattr(part, "annotations", None) or []:
                if getattr(ann, "type", None) != "url_citation":
                    continue
                url = (getattr(ann, "url", None) or "").strip()
                title = (getattr(ann, "title", None) or "").strip()
                if not _add_citation(out, seen, url=url, title=title, limit=limit):
                    return out

    if not text_blobs:
        fallback_text = output_text_from_openai_response(response)
        if fallback_text:
            text_blobs.append(fallback_text)

    for blob in text_blobs:
        for m in _MD_LINK_RE.finditer(blob):
            if not _add_citation(out, seen, url=m.group(2), title=m.group(1), limit=limit):
                return out
        for m in _BARE_URL_RE.finditer(blob):
            if not _add_citation(out, seen, url=m.group(1), title="", limit=limit):
                return out
    return out

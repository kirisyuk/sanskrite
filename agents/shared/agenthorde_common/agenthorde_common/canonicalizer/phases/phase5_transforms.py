"""Phase 5: attach product transforms to /site nodes using core transform dictionary."""

from __future__ import annotations

from typing import Literal, cast

from agenthorde_common.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde_common.canonicalizer.schemas import Candidates, CanonicalEdge, CanonicalFragment, SiteTransform, UnknownTransformCandidate


def run_phase5(
    fragments: list[CanonicalFragment],
    edges: list[CanonicalEdge],
    dict_runtime: DictionaryRuntime | None,
    candidates: Candidates,
) -> None:
    dict_runtime = dict_runtime or DictionaryRuntime(None)
    if not dict_runtime._loaded:
        dict_runtime.refresh()

    frag_by_id = {f.id: f for f in fragments}
    sites = [f for f in fragments if f.fragment_class == "site"]
    seen_transform_keys: set[tuple[str, tuple[str, ...], str]] = set()
    seen_failed_pairs: set[tuple[str, str, str]] = set()

    def _cargo_path(edge: CanonicalEdge) -> str | None:
        attrs = edge.attributes or {}
        pid = attrs.get("product_fragment_id")
        if pid and isinstance(pid, str) and pid in frag_by_id:
            f = frag_by_id[pid]
            if f.fragment_class == "product":
                return f.canonical_path
        p = attrs.get("product") or attrs.get("productPath")
        if p and str(p).strip().startswith("/product/"):
            return str(p).strip()
        return None

    for site in sites:
        incoming: list[str] = []
        outgoing: list[str] = []
        for e in edges:
            if e.relation != "supplies":
                continue
            path = _cargo_path(e)
            if not path:
                continue
            if e.target_fragment_id == site.id:
                incoming.append(path)
            if e.source_fragment_id == site.id:
                outgoing.append(path)

        # Group successful (input -> output) pairs by output; emit one SiteTransform per output with all inputs.
        grouped: dict[str, list[tuple[str, str]]] = {}
        for pin in incoming:
            for pout in outgoing:
                if pin.lower() == pout.lower():
                    continue
                fp = (site.id, pin.lower(), pout.lower())
                ok, kind = dict_runtime.lookup_transform(pin, pout)
                if ok:
                    nk = kind if kind in ("transformation", "assembly", "usage") else "transformation"
                    grouped.setdefault(pout, []).append((pin, nk))
                elif fp not in seen_failed_pairs:
                    seen_failed_pairs.add(fp)
                    candidates.unknown_transforms.append(
                        UnknownTransformCandidate(
                            siteFragmentId=site.id, fromProduct=pin, toProduct=pout
                        )
                    )

        for pout, pairs in grouped.items():
            inputs_list = [p[0] for p in pairs]
            kinds = [p[1] for p in pairs]
            nk: str = "assembly" if "assembly" in kinds else ("usage" if "usage" in kinds else "transformation")
            key = (site.id, tuple(sorted(i.lower() for i in inputs_list)), pout.lower())
            if key in seen_transform_keys:
                continue
            seen_transform_keys.add(key)
            site.site_transforms.append(
                SiteTransform(
                    inputs=inputs_list,
                    output_product=pout,
                    kind=cast(Literal["transformation", "assembly", "usage"], nk),
                    validated=True,
                )
            )

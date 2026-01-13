#!/usr/bin/env python3
"""
Build frequency tables from market segments with normalization patterns.

Input (CSV) is produced by python/analyze_market_segments.py:
  backend/market_segments_counts.csv with columns: count, segment, label

We parse segment paths:
  /segment/{domain}/{vertical}/{geo}

Outputs (two CSVs):
1) Domain/vertical counts (domain normalized):
   - backend/market_segments_domain_vertical_counts.csv
   - columns: count, domain, vertical

2) Market classification counts (channel-aware, domain synonyms collapsed):
   - backend/market_segments_market_l1_l2_counts.csv
   - columns: count, market_l1, market_l2, channel

Notes:
- "public-sector/public/government/gov/b2g" are treated as the same channel ("public-sector")
- "financial-services/fintech" are treated as "finance" domain
- Channel-like domains (b2b/b2c/commercial/industrial/consumer/public-sector) are treated as channel, and their
  vertical becomes market_l1 (sector).
"""

from __future__ import annotations

import argparse
import csv
from collections import Counter
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class DV:
    domain: str
    vertical: str


def parse_segment(segment_path: str) -> DV | None:
    seg = (segment_path or "").strip()
    if not seg.startswith("/segment/"):
        return None
    parts = [p for p in seg.split("/") if p]
    # parts: ["segment", domain, vertical, geo]
    if len(parts) < 4:
        return None
    return DV(domain=parts[1], vertical=parts[2])

def normalize_domain(domain: str) -> str:
    d = (domain or "").strip().lower()
    if not d:
        return ""
    # Collapse public-ish synonyms into one channel
    if d in {"public-sector", "public", "government", "gov", "b2g"}:
        return "public-sector"
    # Collapse finance-ish synonyms into one sector-domain
    if d in {"financial-services", "fintech"}:
        return "finance"
    # Minor cleanup
    if d == "industrials":
        return "industrial"
    return d


CHANNEL_LIKE = {
    "b2b",
    "b2c",
    "commercial",
    "industrial",
    "consumer",
    "public-sector",  # after normalization
}

FINANCE_VERTICALS = {
    "banking",
    "retail-banking",
    "investment",
    "asset-management",
    "private-equity",
    "venture-capital",
    "financial-services",
    "fintech",
    "insurance",
    "payments",
    "crypto",
    "cryptocurrency",
}

TECH_VERTICALS = {
    "software",
    "it-services",
    "cybersecurity",
    "semiconductors",
    "electronics",
}

ENERGY_VERTICALS = {
    "oil-and-gas",
    "utilities",
    "renewables",
}


def normalize_market_from_channel_vertical(vertical: str) -> tuple[str, str]:
    """
    When the segment domain is channel-like (b2b/b2c/commercial/...), the 'vertical' token often carries
    the actual market signal. This function maps that vertical into (market_l1, market_l2).
    """
    v = (vertical or "").strip().lower()
    if not v:
        return "", ""

    # Finance family
    if v in FINANCE_VERTICALS:
        return "finance", v if v not in {"banking"} else "banking"

    # Technology family
    if v in TECH_VERTICALS:
        return "technology", v

    # Energy family
    if v in ENERGY_VERTICALS:
        return "energy", v

    # Default: treat vertical as the market_l1 (sector), with no market_l2.
    return v, ""


@dataclass(frozen=True)
class MarketKey:
    market_l1: str
    market_l2: str
    channel: str


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--in",
        dest="in_path",
        default="backend/market_segments_counts.csv",
        help="Input CSV path (default: backend/market_segments_counts.csv)",
    )
    ap.add_argument(
        "--out-domain-vertical",
        dest="out_domain_vertical",
        default="backend/market_segments_domain_vertical_counts.csv",
        help="Output CSV path for normalized (domain,vertical) (default: backend/market_segments_domain_vertical_counts.csv)",
    )
    ap.add_argument(
        "--out-market",
        dest="out_market",
        default="backend/market_segments_market_l1_l2_counts.csv",
        help="Output CSV path for market_l1/l2/channel table (default: backend/market_segments_market_l1_l2_counts.csv)",
    )
    ap.add_argument(
        "--top",
        dest="top_n",
        type=int,
        default=100,
        help="How many top rows to print (default: 100)",
    )
    args = ap.parse_args()

    in_path = Path(args.in_path)
    out_dv_path = Path(args.out_domain_vertical)
    out_market_path = Path(args.out_market)

    dv_counts: Counter[DV] = Counter()
    market_counts: Counter[MarketKey] = Counter()

    with in_path.open("r", encoding="utf-8") as f:
        r = csv.DictReader(f)
        for row in r:
            seg = (row.get("segment") or "").strip()
            cnt = int(row.get("count") or "0")
            dv = parse_segment(seg)
            if not dv:
                continue
            nd = normalize_domain(dv.domain)
            nv = (dv.vertical or "").strip().lower()
            if not nd or not nv:
                continue

            dv_counts[DV(domain=nd, vertical=nv)] += cnt

            # Channel-aware market normalization:
            # - If domain is channel-like: channel=domain, market_l1=vertical (sector), market_l2=""
            # - Else: channel="", market_l1=domain (sector), market_l2=vertical (sub-sector)
            if nd in CHANNEL_LIKE:
                ml1, ml2 = normalize_market_from_channel_vertical(nv)
                mk = MarketKey(market_l1=ml1, market_l2=ml2, channel=nd)
            else:
                mk = MarketKey(market_l1=nd, market_l2=nv, channel="")
            market_counts[mk] += cnt

    if not dv_counts:
        raise SystemExit(f"No domain/vertical rows parsed from {in_path}")

    items = sorted(dv_counts.items(), key=lambda kv: (-kv[1], kv[0].domain, kv[0].vertical))
    market_items = sorted(market_counts.items(), key=lambda kv: (-kv[1], kv[0].market_l1, kv[0].market_l2, kv[0].channel))

    out_dv_path.parent.mkdir(parents=True, exist_ok=True)
    with out_dv_path.open("w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["count", "domain", "vertical"])
        for dv, cnt in items:
            w.writerow([cnt, dv.domain, dv.vertical])

    out_market_path.parent.mkdir(parents=True, exist_ok=True)
    with out_market_path.open("w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["count", "market_l1", "market_l2", "channel"])
        for mk, cnt in market_items:
            w.writerow([cnt, mk.market_l1, mk.market_l2, mk.channel])

    top_n = max(0, args.top_n)
    print(f"Unique (domain, vertical) after domain normalization: {len(items)}")
    print(f"Wrote: {out_dv_path}")
    print(f"Unique market keys (market_l1, market_l2, channel): {len(market_items)}")
    print(f"Wrote: {out_market_path}")
    print("")
    print(f"Top {top_n} market keys:")
    for i, (mk, cnt) in enumerate(market_items[:top_n], start=1):
        key = f"{mk.market_l1}/{mk.market_l2}" if mk.market_l2 else mk.market_l1
        ch = f"  [channel={mk.channel}]" if mk.channel else ""
        print(f"{i:>3}. {cnt:>7}  {key}{ch}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())


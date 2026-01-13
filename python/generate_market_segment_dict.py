#!/usr/bin/env python3
"""
Generate Firestore dictionary payload for market segment normalization.

Input:
  backend/market_segments_market_l1_l2_counts.csv (produced by analyze_market_segments_domain_vertical.py)
  columns: count, market_l1, market_l2, channel

Output:
  JSON matching backend/internal/models.MarketSegmentDictionary (without requiring timestamps).
  This JSON can be uploaded via:
    go run ./cmd/tools/horizon_seed_market_segment_dict --project <PROJECT> --version v1 --from-json <file>
"""

from __future__ import annotations

import argparse
import csv
import json
from collections import defaultdict
from pathlib import Path
from typing import Any


DEFAULT_CHANNEL_DOMAINS = [
    "b2b",
    "b2c",
    "commercial",
    "industrial",
    "consumer",
    # public-ish synonyms (we normalize them on the backend)
    "public-sector",
    "public",
    "government",
    "gov",
    "b2g",
]

DEFAULT_DOMAIN_ALIASES = {
    "public": "public-sector",
    "government": "public-sector",
    "gov": "public-sector",
    "b2g": "public-sector",
    "fintech": "finance",
    "financial-services": "finance",
    "industrials": "industrial",
}

DEFAULT_VERTICAL_ALIASES = {
    "retail-banking": "banking",
    "cryptocurrency": "crypto",
}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--in",
        dest="in_path",
        default="backend/market_segments_market_l1_l2_counts.csv",
        help="Input CSV path (default: backend/market_segments_market_l1_l2_counts.csv)",
    )
    ap.add_argument(
        "--out",
        dest="out_path",
        default="backend/market_segment_dict_v1.json",
        help="Output JSON path (default: backend/market_segment_dict_v1.json)",
    )
    ap.add_argument(
        "--min-count",
        dest="min_count",
        type=int,
        default=10,
        help="Only include verticalFamilies derived from rows with count >= N (default: 10)",
    )
    args = ap.parse_args()

    in_path = Path(args.in_path)
    out_path = Path(args.out_path)

    # vertical -> best (familyDomain, count)
    best_family: dict[str, tuple[str, int]] = {}

    # channels observed (normalized)
    channels_seen: set[str] = set()

    with in_path.open("r", encoding="utf-8") as f:
        r = csv.DictReader(f)
        for row in r:
            cnt = int(row.get("count") or "0")
            market_l1 = (row.get("market_l1") or "").strip().lower()
            market_l2 = (row.get("market_l2") or "").strip().lower()
            channel = (row.get("channel") or "").strip().lower()
            if channel:
                channels_seen.add(channel)

            # We derive verticalFamilies only when market_l2 is present for a channel row:
            # it means "vertical maps to family market_l1".
            if channel and market_l2 and market_l1 and cnt >= max(0, args.min_count):
                prev = best_family.get(market_l2)
                if prev is None or cnt > prev[1]:
                    best_family[market_l2] = (market_l1, cnt)

    segment_vertical_families = {v: fam for v, (fam, _cnt) in best_family.items()}

    # Build final doc payload.
    payload: dict[str, Any] = {
        # version/updatedAt are stamped by Go uploader; can be present but not required.
        "segmentChannelDomains": sorted(set(DEFAULT_CHANNEL_DOMAINS) | channels_seen),
        "segmentDomainAliases": dict(DEFAULT_DOMAIN_ALIASES),
        "segmentVerticalAliases": dict(DEFAULT_VERTICAL_ALIASES),
        "segmentVerticalFamilies": dict(sorted(segment_vertical_families.items())),
        # Start empty: this is where you add nuanced overrides over time.
        "segmentNormRules": {},
    }

    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"Wrote: {out_path}")
    print(f"Derived verticalFamilies: {len(payload['segmentVerticalFamilies'])}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())


#!/usr/bin/env python3
"""
Analyze Horizon market segments report produced by:
  backend/cmd/tools/horizon_list_market_segments

Input format (example lines):
    105  /segment/b2b/manufacturing/global             B2b / Manufacturing / Global

Outputs:
 - CSV with columns: count, segment, label
 - Prints top N to stdout
"""

from __future__ import annotations

import argparse
import csv
import re
from dataclasses import dataclass
from pathlib import Path


LINE_RE = re.compile(r"^\s*(\d+)\s+(/segment/\S+)\s+(.*\S)\s*$")


@dataclass(frozen=True)
class Row:
    count: int
    segment: str
    label: str


def parse_report(text: str) -> list[Row]:
    rows: list[Row] = []
    for line in text.splitlines():
        m = LINE_RE.match(line)
        if not m:
            continue
        rows.append(Row(count=int(m.group(1)), segment=m.group(2), label=m.group(3)))
    return rows


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--in",
        dest="in_path",
        default="backend/market_segments_report.txt",
        help="Path to market_segments_report.txt (default: backend/market_segments_report.txt)",
    )
    ap.add_argument(
        "--out",
        dest="out_path",
        default="backend/market_segments_counts.csv",
        help="Output CSV path (default: backend/market_segments_counts.csv)",
    )
    ap.add_argument(
        "--top",
        dest="top_n",
        type=int,
        default=50,
        help="How many top rows to print (default: 50)",
    )
    args = ap.parse_args()

    in_path = Path(args.in_path)
    out_path = Path(args.out_path)

    text = in_path.read_text(encoding="utf-8", errors="replace")
    rows = parse_report(text)

    if not rows:
        raise SystemExit(f"No segment rows parsed from {in_path}")

    # Already aggregated in the report, but ensure stable ordering.
    rows_sorted = sorted(rows, key=lambda r: (-r.count, r.segment))

    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["count", "segment", "label"])
        for r in rows_sorted:
            w.writerow([r.count, r.segment, r.label])

    top_n = max(0, args.top_n)
    print(f"Parsed {len(rows_sorted)} unique segments")
    print(f"Wrote: {out_path}")
    print("")
    print(f"Top {top_n}:")
    for i, r in enumerate(rows_sorted[:top_n], start=1):
        print(f"{i:>3}. {r.count:>7}  {r.segment}  {r.label}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())


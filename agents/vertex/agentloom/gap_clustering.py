from __future__ import annotations

import random
import re
from collections import Counter, OrderedDict

from agentloom.contracts import GapCluster


# (pattern_name, compiled regex, human-readable label)
_CLUSTER_RULES: list[tuple[str, re.Pattern[str], str]] = [
    (
        "intermediate_salts",
        re.compile(r"sulfate|hydroxide|precipitate|leach|electrowin|matte|concentrate", re.I),
        "Intermediate salts / hydrometallurgy",
    ),
    ("contract_procurement", re.compile(r"contract|supplier|procurement|offtake|supply\s*link", re.I), "Contracts and procurement visibility"),
    ("upstream_geology", re.compile(r"laterite|ore\s*body|deposit|mine\s*output|tailings|concentrator", re.I), "Upstream geology and mine output"),
    ("downstream_sink", re.compile(r"gigafactory|cell plant|pack assembly|automotive battery|ev battery|module assembly", re.I), "Downstream cell / pack sink"),
    ("converter_midstream", re.compile(r"cathode|precursor|converter|refiner|refinery|smelter|roaster", re.I), "Converter / precursor layer"),
    ("regulation", re.compile(r"regulation|regulatory|due\s*diligence|traceability|battery\s*passport|compliance", re.I), "Regulatory and compliance"),
    ("ownership", re.compile(r"ownership|owns|control|subsidiary|jv|joint\s*venture|investor", re.I), "Ownership and control"),
    ("bridge_flow", re.compile(r"bridge|disconnected|missing\s*link|operational\s*link|flow\s*chain", re.I), "Structural bridge / flow gaps"),
]


def _tokenize_gap(text: str) -> set[str]:
    return set(re.findall(r"[a-z0-9]{3,}", text.lower()))


def _jaccard(a: set[str], b: set[str]) -> float:
    if not a and not b:
        return 1.0
    inter = len(a & b)
    union = len(a | b)
    return inter / union if union else 0.0


def _jaccard_embedding_clusters(normalized: list[str], max_clusters: int) -> list[GapCluster] | None:
    """
    Deterministic k-medoids style clustering over token Jaccard similarity (no external embedding API).
    """
    n = len(normalized)
    if n < 2:
        return None
    tok_sets = [_tokenize_gap(t) for t in normalized]
    k = min(max_clusters, max(2, min(7, 1 + n // 3)))
    k = min(k, n)
    rng = random.Random(hash(tuple(normalized[:5])) & 0xFFFFFFFF)
    medoids = list(range(k))
    if k < n:
        medoids = rng.sample(range(n), k)

    for _ in range(24):
        assignments: list[int] = []
        for i in range(n):
            best_j = 0
            best_sim = -1.0
            for j, m in enumerate(medoids):
                sim = _jaccard(tok_sets[i], tok_sets[m])
                if sim > best_sim:
                    best_sim = sim
                    best_j = j
            assignments.append(best_j)
        clusters: list[list[int]] = [[] for _ in range(k)]
        for idx, ci in enumerate(assignments):
            clusters[ci].append(idx)
        new_medoids: list[int] = []
        for ci, members in enumerate(clusters):
            if not members:
                new_medoids.append(medoids[ci] if ci < len(medoids) else 0)
                continue
            best_m = members[0]
            best_score = -1.0
            for cand in members:
                score = sum(_jaccard(tok_sets[cand], tok_sets[o]) for o in members)
                if score > best_score:
                    best_score = score
                    best_m = cand
            new_medoids.append(best_m)
        if new_medoids == medoids:
            break
        medoids = new_medoids

    out: list[GapCluster] = []
    seen_labels: dict[str, int] = {}
    for ci, members in enumerate(clusters):
        if not members:
            continue
        texts = [normalized[i] for i in members]
        top_tokens = Counter()
        for i in members:
            top_tokens.update(tok_sets[i])
        label_bits = [w for w, _ in top_tokens.most_common(4)]
        label = "Embedding cluster: " + (", ".join(label_bits) if label_bits else f"group-{ci+1}")
        if label in seen_labels:
            seen_labels[label] += 1
            label = f"{label} ({seen_labels[label]})"
        else:
            seen_labels[label] = 0
        out.append(
            GapCluster(
                clusterId=f"gap-emb-{ci+1}",
                label=label[:120],
                memberGapTexts=texts[:25],
            )
        )
    return out if out else None


def cluster_gap_texts(
    gap_texts: list[str],
    *,
    max_clusters: int = 7,
    prefer_embedding_clustering: bool = False,
) -> list[GapCluster]:
    """
    Cluster open gap strings. With prefer_embedding_clustering, uses token Jaccard k-medoids; else regex buckets.
    """
    normalized = [g.strip() for g in gap_texts if g and str(g).strip()]
    if not normalized:
        return []

    if prefer_embedding_clustering:
        emb = _jaccard_embedding_clusters(normalized, max_clusters)
        if emb:
            return emb[:max_clusters]

    buckets: OrderedDict[str, list[str]] = OrderedDict()
    for rule_id, _pattern, label in _CLUSTER_RULES:
        buckets[label] = []

    other_key = "Other structural gaps"
    buckets[other_key] = []

    for text in normalized:
        placed = False
        lower = text.lower()
        for _rule_id, pattern, label in _CLUSTER_RULES:
            if pattern.search(lower):
                if text not in buckets[label]:
                    buckets[label].append(text)
                placed = True
                break
        if not placed and text not in buckets[other_key]:
            buckets[other_key].append(text)

    clusters: list[GapCluster] = []
    cid = 0
    for label, members in buckets.items():
        if not members:
            continue
        cid += 1
        clusters.append(
            GapCluster(
                clusterId=f"gap-cluster-{cid}",
                label=label,
                memberGapTexts=members[:20],
            )
        )

    clusters = clusters[:max_clusters]

    # Attach any gaps not yet in a cluster (should be rare) to last cluster or Other
    seen = {m for c in clusters for m in c.member_gap_texts}
    remainder = [t for t in normalized if t not in seen]
    if remainder and clusters:
        last = clusters[-1]
        merged = _dedupe_preserve_order(list(last.member_gap_texts) + remainder)
        clusters[-1] = GapCluster(
            clusterId=last.cluster_id,
            label=last.label,
            memberGapTexts=merged[:25],
        )

    return clusters


def _dedupe_preserve_order(values: list[str]) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for v in values:
        if v in seen:
            continue
        seen.add(v)
        out.append(v)
    return out

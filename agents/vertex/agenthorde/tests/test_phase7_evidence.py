from agenthorde.canonicalizer.phases.phase0_input import WorkingState
from agenthorde.canonicalizer.phases.phase7_evidence import run_phase7
from agenthorde.canonicalizer.schemas import CanonicalEdge, CanonicalFragment
from agenthorde.schemas.graph import ReasoningNode


def test_phase7_sources_go_to_archive_not_graph() -> None:
    """Source documents must land in EvidenceArchive, NOT in CanonicalGraph.fragments."""
    frags: list[CanonicalFragment] = []
    edges: list[CanonicalEdge] = []
    ledger = [
        {
            "tool": "web_search",
            "ok": True,
            "payload": {
                "answer": "Example Corp published guidance on margins.",
                "sources": [{"url": "https://example.com/a", "title": "Ex", "domain": "example.com"}],
            },
        }
    ]
    ws = WorkingState(
        claim_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Example Corp published guidance on margins."),
        ],
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Example Corp published guidance on margins."),
        ],
    )
    archive = run_phase7(ledger, frags, edges, ws, "run", "1.0.0")

    # Neither source fragments nor claim anchors may leak into the graph fragments.
    assert not any(f.canonical_path.startswith("/evidence/") for f in frags)
    # No evidence-related edges land in the graph (supported_by/asserted_by/contradicts/under_hypothesis).
    assert not any(
        e.relation in ("supported_by", "asserted_by", "contradicts", "under_hypothesis")
        for e in edges
    )

    # The archive holds the source, a claim record, and the claim-source link.
    assert len(archive.sources) == 1
    src = archive.sources[0]
    assert src.url == "https://example.com/a"
    assert src.domain == "example.com"

    assert any(c.id == "c1" for c in archive.claims)

    asserted = [l for l in archive.links if l.relation == "asserted_by"]
    assert len(asserted) == 1
    assert asserted[0].claim_id == "c1"
    assert asserted[0].source_id == src.id
    assert asserted[0].rank == 0


def test_phase7_fanout_lands_in_archive_links() -> None:
    """
    Every web_search source whose answer matches the claim text above threshold
    must be joined to the claim in archive.links as supported_by (with rank).
    Unrelated URLs stay in archive.sources but receive no link.
    """
    frags: list[CanonicalFragment] = []
    edges: list[CanonicalEdge] = []
    ledger = [
        {
            "tool": "web_search",
            "ok": True,
            "payload": {
                "answer": "Tesla, Inc. is incorporated in Delaware and reincorporated in Texas in 2024.",
                "sources": [
                    {"url": "https://sec.gov/a", "title": "10-K", "domain": "sec.gov"},
                    {"url": "https://tesla.com/about", "title": "About", "domain": "tesla.com"},
                ],
            },
        },
        {
            "tool": "web_search",
            "ok": True,
            "payload": {
                "answer": "Tesla reincorporated in Texas in 2024; Delaware was its previous state of incorporation.",
                "sources": [
                    {"url": "https://reuters.com/a", "title": "R1", "domain": "reuters.com"},
                    {"url": "https://ft.com/a", "title": "FT1", "domain": "ft.com"},
                ],
            },
        },
        {
            "tool": "web_search",
            "ok": True,
            "payload": {
                "answer": "Completely unrelated topic about oil refineries.",
                "sources": [
                    {"url": "https://nowhere.example/x", "title": "Nope", "domain": "nowhere.example"},
                ],
            },
        },
    ]
    ws = WorkingState(
        claim_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Tesla reincorporated from Delaware to Texas in 2024."),
        ],
        all_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Tesla reincorporated from Delaware to Texas in 2024."),
        ],
    )
    archive = run_phase7(ledger, frags, edges, ws, "run", "1.0.0")

    # All 5 distinct URLs must be recorded as sources.
    assert {s.domain for s in archive.sources} == {"sec.gov", "tesla.com", "reuters.com", "ft.com", "nowhere.example"}

    # At least 1 asserted_by + >=2 supported_by fan-out links for the claim.
    asserted = [l for l in archive.links if l.relation == "asserted_by" and l.claim_id == "c1"]
    fanout = [l for l in archive.links if l.relation == "supported_by" and l.claim_id == "c1"]
    assert len(asserted) == 1
    assert len(fanout) >= 2

    # Primary has rank 0, fan-out has rank >=1 and strictly increasing.
    assert asserted[0].rank == 0
    ranks = sorted(l.rank for l in fanout)
    assert ranks[0] >= 1 and ranks == sorted(set(ranks))

    # Unrelated source exists as a source record but has NO link.
    nowhere = next(s for s in archive.sources if s.domain == "nowhere.example")
    assert not any(l.source_id == nowhere.id for l in archive.links)


def test_phase7_direct_refs_preferred_over_fuzzy() -> None:
    """When node.evidence_refs contains matching URLs, Phase 7 creates direct links
    with score=100 and skips the fuzzy path."""
    frags: list[CanonicalFragment] = []
    edges: list[CanonicalEdge] = []
    ledger = [
        {
            "tool": "web_search",
            "ok": True,
            "payload": {
                "answer": "Some unrelated fuzzy answer that would score low.",
                "sources": [
                    {"url": "https://sec.gov/filing-x", "title": "10-K", "domain": "sec.gov"},
                    {"url": "https://reuters.com/story-y", "title": "R", "domain": "reuters.com"},
                ],
            },
        }
    ]
    ws = WorkingState(
        claim_nodes=[
            ReasoningNode(
                id="c1",
                kind="claim",
                text="Tesla reincorporated in Texas.",
                evidence_refs=["https://sec.gov/filing-x", "https://reuters.com/story-y"],
            ),
        ],
        all_nodes=[
            ReasoningNode(
                id="c1",
                kind="claim",
                text="Tesla reincorporated in Texas.",
                evidence_refs=["https://sec.gov/filing-x", "https://reuters.com/story-y"],
            ),
        ],
    )
    archive = run_phase7(ledger, frags, edges, ws, "run", "1.0.0")

    links = [l for l in archive.links if l.claim_id == "c1"]
    # Exactly 2 links (one asserted_by + one supported_by), all at score 100.
    assert len(links) == 2
    assert all(l.score == 100 for l in links)
    kinds = sorted(l.relation for l in links)
    assert kinds == ["asserted_by", "supported_by"]
    # source ids match sha16(url) for the given URLs.
    source_ids = {l.source_id for l in links}
    expected = {s.id for s in archive.sources}
    assert source_ids == expected


def test_phase7_fuzzy_fallback_when_refs_empty() -> None:
    """No evidence_refs on the claim, but retrieved answers partially match → fallback
    threshold still yields supported_by links (Malacca-like prose case)."""
    frags: list[CanonicalFragment] = []
    edges: list[CanonicalEdge] = []
    ledger = [
        {
            "tool": "web_search",
            "ok": True,
            "payload": {
                "answer": (
                    "Singapore dominates bunker fuel trading with roughly 50 million tonnes per year "
                    "and handles major transshipment flows in the Malacca Strait."
                ),
                "sources": [
                    {"url": "https://imf.org/sg", "title": "IMF SG", "domain": "imf.org"},
                ],
            },
        }
    ]
    ws = WorkingState(
        claim_nodes=[
            ReasoningNode(
                id="c1",
                kind="claim",
                text="Singapore dominates bunker fuel trading.",
                evidence_refs=[],
            ),
        ],
        all_nodes=[
            ReasoningNode(
                id="c1",
                kind="claim",
                text="Singapore dominates bunker fuel trading.",
                evidence_refs=[],
            ),
        ],
    )
    archive = run_phase7(ledger, frags, edges, ws, "run", "1.0.0")
    # Fuzzy assert link must exist and have score < 100 (distinguishes from direct path).
    asserted = [l for l in archive.links if l.relation == "asserted_by" and l.claim_id == "c1"]
    assert len(asserted) == 1
    assert asserted[0].score < 100
    assert asserted[0].source_id in {s.id for s in archive.sources}


def test_phase7_prior_knowledge_ref_skipped() -> None:
    """'prior_knowledge' marker should NOT produce any link for the claim."""
    frags: list[CanonicalFragment] = []
    edges: list[CanonicalEdge] = []
    ledger = [
        {
            "tool": "web_search",
            "ok": True,
            "payload": {
                "answer": "Random stuff unrelated to the claim.",
                "sources": [
                    {"url": "https://x.example/a", "title": "X", "domain": "x.example"},
                ],
            },
        }
    ]
    ws = WorkingState(
        claim_nodes=[
            ReasoningNode(
                id="c1",
                kind="claim",
                text="Pi is approximately 3.14159.",
                evidence_refs=["prior_knowledge"],
            ),
        ],
        all_nodes=[
            ReasoningNode(
                id="c1",
                kind="claim",
                text="Pi is approximately 3.14159.",
                evidence_refs=["prior_knowledge"],
            ),
        ],
    )
    archive = run_phase7(ledger, frags, edges, ws, "run", "1.0.0")
    # With prior_knowledge marker, no direct link; fuzzy text mismatch means no link either.
    claim_links = [l for l in archive.links if l.claim_id == "c1"]
    assert claim_links == []


def test_phase7_counters_and_hypotheses_in_archive_not_graph() -> None:
    """Counter and hypothesis nodes are archived, not materialized as evidence fragments."""
    frags: list[CanonicalFragment] = []
    edges: list[CanonicalEdge] = []
    ws = WorkingState(
        claim_nodes=[
            ReasoningNode(id="c1", kind="claim", text="Base claim.", hypothesis_id="h1"),
        ],
        counter_nodes=[
            ReasoningNode(id="x1", kind="counter", text="But actually the opposite is true."),
        ],
        hypothesis_nodes=[
            ReasoningNode(id="h1", kind="hypothesis_anchor", text="Everything hinges on X."),
        ],
        all_nodes=[
            ReasoningNode(id="h1", kind="hypothesis_anchor", text="Everything hinges on X."),
            ReasoningNode(id="c1", kind="claim", text="Base claim.", hypothesis_id="h1"),
            ReasoningNode(id="x1", kind="counter", text="But actually the opposite is true."),
        ],
    )
    archive = run_phase7([], frags, edges, ws, "run", "1.0.0")

    assert not any(f.canonical_path.startswith("/evidence/") for f in frags)
    assert not any(
        e.relation in ("contradicts", "under_hypothesis", "supported_by", "asserted_by")
        for e in edges
    )

    assert {c.id for c in archive.claims} == {"c1"}
    assert archive.claims[0].hypothesis_id == "h1"
    assert {h.id for h in archive.hypotheses} == {"h1"}
    assert {c.id for c in archive.counters} == {"x1"}
    assert archive.counters[0].contradicts_claim_id == "c1"

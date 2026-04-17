from agenthorde.canonicalizer.confidence import aggregate_edge_confidence, noisy_or
from agenthorde.canonicalizer.schemas import CanonicalEdge, SourceRef


def test_noisy_or_saturates_with_independent_sources() -> None:
    low = noisy_or([0.2])
    high = noisy_or([0.8, 0.8, 0.8])
    assert low < high
    assert high > 0.95


def test_aggregate_edge_confidence_one_vs_many_sources() -> None:
    edge = CanonicalEdge(
        id="e1",
        source_fragment_id="a",
        target_fragment_id="b",
        relation="supplies",
        confidence=0.75,
        source_claim_ids=["c1"],
    )
    one = aggregate_edge_confidence(
        edge,
        {
            "c1": [SourceRef(url="https://blog.example/x", title="x", domain="blog.example")],
        },
    )
    many = aggregate_edge_confidence(
        edge,
        {
            "c1": [
                SourceRef(url="https://sec.gov/a", title="a", domain="sec.gov"),
                SourceRef(url="https://sec.gov/b", title="b", domain="sec.gov"),
                SourceRef(url="https://sec.gov/c", title="c", domain="sec.gov"),
            ],
        },
    )
    assert many >= one

from agenthorde_common.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde_common.canonicalizer.phases.phase2_dedup import run_phase2
from agenthorde_common.canonicalizer.schemas import Candidates, ContextHints, EntityMention


def test_dedup_merges_identical_normalized_surfaces() -> None:
    mentions = [
        EntityMention(
            mention_id="m0",
            surface="G42",
            type="org",
            subtype="private_holdco",
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="ae"),
        ),
        EntityMention(
            mention_id="m1",
            surface="G42",
            type="org",
            subtype="private_holdco",
            claim_refs=["c2"],
            context_hints=ContextHints(iso2_hint="ae"),
        ),
    ]
    c = Candidates()
    ent, _ = run_phase2(mentions, 85, 80, DictionaryRuntime(None), c)
    assert len(ent) == 1
    assert ent[0].mention_count == 2


def test_dedup_jurisdiction_gate_prevents_eu_us_chips_act_collapse() -> None:
    """EU Chips Act and US CHIPS Act share tokens but are different regimes.

    The phase2 jurisdiction gate must hard-stop the merge based on iso2 derived
    from the surface ("EU" vs "US"), and emit a namespace_conflict candidate so
    the gap is visible to the next cycle.
    """

    mentions = [
        EntityMention(
            mention_id="m0",
            surface="EU Chips Act",
            type="regime",
            subtype="industrial_policy",
            claim_refs=["c1"],
            context_hints=ContextHints(),  # iso2 must be inferred from surface
        ),
        EntityMention(
            mention_id="m1",
            surface="US CHIPS Act",
            type="regime",
            subtype="industrial_policy",
            claim_refs=["c2"],
            context_hints=ContextHints(),
        ),
    ]
    c = Candidates()
    ent, _ = run_phase2(mentions, 85, 80, DictionaryRuntime(None), c)
    assert len(ent) == 2, "regimes from different jurisdictions must not merge"
    surfaces = {tuple(e.surface_forms) for e in ent}
    assert ("EU Chips Act",) in surfaces
    assert ("US CHIPS Act",) in surfaces
    reasons = [m.reason for m in c.merge_candidates]
    assert any(r.startswith("namespace_conflict:") for r in reasons), (
        f"expected namespace_conflict candidate, got reasons={reasons}"
    )


def test_dedup_jurisdiction_gate_allows_merge_when_iso2_matches() -> None:
    """Two mentions with same iso2 hint and similar surfaces still merge."""

    mentions = [
        EntityMention(
            mention_id="m0",
            surface="US CHIPS Act",
            type="regime",
            subtype="industrial_policy",
            claim_refs=["c1"],
            context_hints=ContextHints(),
        ),
        EntityMention(
            mention_id="m1",
            surface="US Chips Act",
            type="regime",
            subtype="industrial_policy",
            claim_refs=["c2"],
            context_hints=ContextHints(),
        ),
    ]
    c = Candidates()
    ent, _ = run_phase2(mentions, 85, 80, DictionaryRuntime(None), c)
    assert len(ent) == 1, "same-jurisdiction surfaces with high similarity should merge"
    assert ent[0].mention_count == 2


def test_dedup_jurisdiction_gate_skips_non_jurisdictional_classes() -> None:
    """vessel_class / route / product are not jurisdiction-bound; gate must not fire."""

    mentions = [
        EntityMention(
            mention_id="m0",
            surface="VLCC",
            type="vessel_class",
            subtype="vlcc",
            claim_refs=["c1"],
            context_hints=ContextHints(iso2_hint="us"),
        ),
        EntityMention(
            mention_id="m1",
            surface="VLCC",
            type="vessel_class",
            subtype="vlcc",
            claim_refs=["c2"],
            context_hints=ContextHints(iso2_hint="cn"),
        ),
    ]
    c = Candidates()
    ent, _ = run_phase2(mentions, 85, 80, DictionaryRuntime(None), c)
    assert len(ent) == 1, "vessel_class is global; iso2 hint must not gate merge"

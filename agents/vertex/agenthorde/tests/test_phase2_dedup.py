from agenthorde.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde.canonicalizer.phases.phase2_dedup import run_phase2
from agenthorde.canonicalizer.schemas import Candidates, ContextHints, EntityMention


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
    ent = run_phase2(mentions, 85, 80, DictionaryRuntime(None), c)
    assert len(ent) == 1
    assert ent[0].mention_count == 2

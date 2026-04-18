import uuid

from agenthorde_common.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde_common.canonicalizer.phases.phase5_transforms import run_phase5
from agenthorde_common.canonicalizer.schemas import Candidates, CanonicalEdge, CanonicalFragment

_NS = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")


def _fid(path: str) -> str:
    return str(uuid.uuid5(_NS, path))


def test_phase5_attaches_known_transform() -> None:
    site = CanonicalFragment(
        id="s1",
        canonical_path="/site/cl/refinery",
        fragment_class="site",
    )
    pin_path = "/product/lithium/spodumene-concentrate"
    pout_path = "/product/lithium/lithium-carbonate"
    p_in = CanonicalFragment(
        id=_fid(pin_path),
        canonical_path=pin_path,
        fragment_class="product",
    )
    p_out = CanonicalFragment(
        id=_fid(pout_path),
        canonical_path=pout_path,
        fragment_class="product",
    )
    frags = [site, p_in, p_out]
    edges = [
        CanonicalEdge(
            id="x1",
            source_fragment_id="s0",
            target_fragment_id="s1",
            relation="supplies",
            attributes={"product_fragment_id": p_in.id, "product": pin_path},
        ),
        CanonicalEdge(
            id="x2",
            source_fragment_id="s1",
            target_fragment_id="s2",
            relation="supplies",
            attributes={"product_fragment_id": p_out.id, "product": pout_path},
        ),
    ]
    rt = DictionaryRuntime(None)
    rt._loaded = True
    rt._transform_pairs = {(pin_path, pout_path)}
    rt._transform_kind = {(pin_path, pout_path): "transformation"}
    c = Candidates()
    run_phase5(frags, edges, rt, c)
    assert len(site.site_transforms) == 1
    assert site.site_transforms[0].validated
    assert site.site_transforms[0].inputs == [pin_path]
    assert site.site_transforms[0].output_product == pout_path

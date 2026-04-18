from agenthorde_common.canonicalizer.product_rollup import rollup_product_surface


def test_rollup_strips_plaid_suffix() -> None:
    rolled, orig = rollup_product_surface("Tesla Model S Plaid")
    assert orig == "Tesla Model S Plaid"
    assert "plaid" not in rolled.lower()
    assert "model s" in rolled.lower()


def test_rollup_no_change_for_plain_name() -> None:
    rolled, orig = rollup_product_surface("Megapack")
    assert orig is None
    assert rolled == "Megapack"

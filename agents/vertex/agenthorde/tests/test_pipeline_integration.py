"""Full pipeline with mocked LLM (no Vertex)."""

from __future__ import annotations

import asyncio

from agenthorde_common.canonicalizer.dictionaries import DictionaryRuntime
from agenthorde_common.canonicalizer.llm_client import LLMPhaseClient
from agenthorde_common.canonicalizer.pipeline import run_pipeline
from agenthorde_common.canonicalizer.schemas import CanonicalizerInput


def test_pipeline_mock_llm_produces_subsidiary_edge() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Acme",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Beta Holdings",
                        "type": "org",
                        "subtype": "private_holdco",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "subsidiary_of",
                        "layer_section": "ownership",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e1",
                        "attributes": {"pct": 100},
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.9,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Acme is a unit of Beta Holdings."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t1",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        assert len(res.graph.fragments) >= 2
        assert any(e.relation == "subsidiary_of" for e in res.graph.edges)

    asyncio.run(_go())


def test_pipeline_invalid_input_short_circuits() -> None:
    mock = LLMPhaseClient(mock_responses={})
    inp = CanonicalizerInput(
        pack={},
        analyzer_output={},
        governance_output={},
        tool_ledger=[],
        final_answer="",
        run_id="t-invalid",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        assert any(x.code == "invalid_input" for x in res.validation_report.input_errors)
        assert len(res.graph.fragments) == 0

    asyncio.run(_go())


def test_pipeline_repairs_subsidiary_person_to_org() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Jane",
                        "type": "person",
                        "subtype": "executive",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Acme",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "subsidiary_of",
                        "layer_section": "ownership",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e1",
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Jane controls Acme."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t2",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        assert any(e.relation == "beneficially_owns" for e in res.graph.edges)

    asyncio.run(_go())


def test_pipeline_registered_in_is_mapped_to_incorporated_in() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Acme",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Delaware",
                        "type": "geo",
                        "subtype": "state",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "registered_in",
                        "layer_section": "ownership",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e1",
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Acme is registered in Delaware."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-syn-reg",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        relations = [e.relation for e in res.graph.edges]
        assert "incorporated_in" in relations
        assert all(r != "registered_in" for r in relations)
        mapped = [e for e in res.graph.edges if e.relation == "incorporated_in"]
        assert mapped and mapped[0].qualifiers.get("relationSynonym", "").startswith("synonym:registered_in")

    asyncio.run(_go())


def test_pipeline_org_owns_org_inverts_to_subsidiary_of() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Parent Co",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Child Co",
                        "type": "org",
                        "subtype": "private_subsidiary",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "owns",
                        "layer_section": "ownership",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e1",
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Parent Co owns Child Co."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-syn-owns",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        subs = [e for e in res.graph.edges if e.relation == "subsidiary_of"]
        assert len(subs) == 1
        e = subs[0]
        # The surviving edge must be Child -> Parent (inverted from "Parent owns Child").
        src = next(f for f in res.graph.fragments if f.id == e.source_fragment_id)
        tgt = next(f for f in res.graph.fragments if f.id == e.target_fragment_id)
        assert "child" in src.canonical_path.lower()
        assert "parent" in tgt.canonical_path.lower()
        assert (e.qualifiers.get("relationSynonym") or "").startswith("synonym:owns->subsidiary_of")

    asyncio.run(_go())


def test_site_supplies_chain_mock() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Mine A",
                        "type": "site",
                        "subtype": "mine",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "cl"},
                        "per_claim_confidence": 0.85,
                    },
                    {
                        "surface": "Refinery B",
                        "type": "site",
                        "subtype": "refinery",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "cl"},
                        "per_claim_confidence": 0.85,
                    },
                    {
                        "surface": "Battery Plant C",
                        "type": "site",
                        "subtype": "battery_plant",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "de"},
                        "per_claim_confidence": 0.85,
                    },
                    {
                        "surface": "Spodumene concentrate",
                        "type": "product",
                        "subtype": "ore",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.85,
                    },
                    {
                        "surface": "Lithium carbonate",
                        "type": "product",
                        "subtype": "chemical",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.85,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "supplies",
                        "layer_section": "flow_topology",
                        "layer": "flow",
                        "from_id": "e0",
                        "to_id": "e1",
                        "attributes": {"product": "/product/ore/spodumene-concentrate"},
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    },
                    {
                        "relation": "supplies",
                        "layer_section": "flow_topology",
                        "layer": "flow",
                        "from_id": "e1",
                        "to_id": "e2",
                        "attributes": {"product": "/product/chemical/lithium-carbonate"},
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    },
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Spodumene moves mine to refinery to plant."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t3",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        supplies = [e for e in res.graph.edges if e.relation == "supplies"]
        assert len(supplies) >= 2
        assert all((e.attributes or {}).get("product_fragment_id") for e in supplies)
        assert not any(
            x.code == "supply_missing_cargo" for x in res.validation_report.relation_errors
        )

    asyncio.run(_go())


def test_pipeline_tesla_produces_megapack() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Tesla, Inc.",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Megapack",
                        "type": "product",
                        "subtype": "energy_storage",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "produces",
                        "layer_section": "catalog",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e1",
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.85,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Tesla produces Megapack energy storage."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-produces",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        prod_edges = [e for e in res.graph.edges if e.relation == "produces"]
        assert len(prod_edges) == 1
        assert prod_edges[0].layer_section == "catalog"

    asyncio.run(_go())


def test_pipeline_manufactures_synonym_maps_to_produces() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Acme Corp",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Widget",
                        "type": "product",
                        "subtype": "industrial",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "manufactures",
                        "layer_section": "ownership",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e1",
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Acme Corp manufactures Widget."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-mfg",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        assert any(e.relation == "produces" for e in res.graph.edges)
        assert all(e.relation != "manufactures" for e in res.graph.edges)

    asyncio.run(_go())


def test_pipeline_mine_two_products_two_supplies_edges() -> None:
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Open pit mine",
                        "type": "site",
                        "subtype": "mine",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "au"},
                        "per_claim_confidence": 0.85,
                    },
                    {
                        "surface": "Processing plant",
                        "type": "site",
                        "subtype": "refinery",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "au"},
                        "per_claim_confidence": 0.85,
                    },
                    {
                        "surface": "Spodumene",
                        "type": "product",
                        "subtype": "ore",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.85,
                    },
                    {
                        "surface": "Lepidolite",
                        "type": "product",
                        "subtype": "ore",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.85,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "supplies",
                        "layer_section": "flow_topology",
                        "layer": "flow",
                        "from_id": "e0",
                        "to_id": "e1",
                        "attributes": {"product": "/product/ore/spodumene"},
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    },
                    {
                        "relation": "supplies",
                        "layer_section": "flow_topology",
                        "layer": "flow",
                        "from_id": "e0",
                        "to_id": "e1",
                        "attributes": {"product": "/product/ore/lepidolite"},
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    },
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [
                    {
                        "id": "n1",
                        "kind": "claim",
                        "text": "The mine supplies spodumene and lepidolite to the plant.",
                    }
                ],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-dual",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        supplies = [e for e in res.graph.edges if e.relation == "supplies"]
        assert len(supplies) == 2
        cargos = {(e.attributes or {}).get("product") for e in supplies}
        assert "/product/ore/spodumene" in cargos
        assert "/product/ore/lepidolite" in cargos
        assert not any(x.code == "supply_missing_cargo" for x in res.validation_report.relation_errors)

    asyncio.run(_go())


def test_pipeline_produces_reversed_endpoints_are_swapped() -> None:
    """LLM emits produces (product -> org); should be swapped into org -> product."""
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Tesla, Inc.",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Model S",
                        "type": "product",
                        "subtype": "ev",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "produces",
                        "layer_section": "catalog",
                        "layer": "structural",
                        "from_id": "e1",  # product (wrong side)
                        "to_id": "e0",    # org
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Tesla produces the Model S."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-produces-swap",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        prod = [e for e in res.graph.edges if e.relation == "produces"]
        assert len(prod) == 1
        frag_by_id = {f.id: f for f in res.graph.fragments}
        src = frag_by_id[prod[0].source_fragment_id]
        tgt = frag_by_id[prod[0].target_fragment_id]
        assert src.fragment_class == "org"
        assert tgt.fragment_class == "product"
        assert not any(
            i.code == "relation_endpoints_forbidden"
            for i in res.validation_report.relation_errors
        )
        assert prod[0].attributes.get("kind") == "org_produces_product_line"

    asyncio.run(_go())


def test_pipeline_co_shipped_goes_to_unknown_relations() -> None:
    """co_shipped was removed from the taxonomy; LLM emission must not crash, edge must be dropped."""
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Port A",
                        "type": "site",
                        "subtype": "port",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "sg"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Port B",
                        "type": "site",
                        "subtype": "port",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "cn"},
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "co_shipped",
                        "layer_section": "flow_topology",
                        "layer": "flow",
                        "from_id": "e0",
                        "to_id": "e1",
                        "attributes": {"products": ["a", "b"]},
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.7,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [
                    {
                        "id": "n1",
                        "kind": "claim",
                        "text": "Port A and Port B share a vessel carrying multiple commodities.",
                    }
                ],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-coship",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        assert not any(e.relation == "co_shipped" for e in res.graph.edges)
        assert any(
            u.relation == "co_shipped" for u in res.candidates.unknown_relations
        )

    asyncio.run(_go())


def test_pipeline_sources_go_to_archive_not_graph() -> None:
    """
    End-to-end: web_search sources must not appear as graph fragments, but must
    appear in CanonicalizerResult.evidence_archive with asserted_by/supported_by links.
    """
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Acme",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Delaware",
                        "type": "geo",
                        "subtype": "state",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "incorporated_in",
                        "layer_section": "ownership",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e1",
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.85,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Acme is incorporated in Delaware."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[
            {
                "tool": "web_search",
                "ok": True,
                "payload": {
                    "answer": "Acme is incorporated in Delaware according to its 10-K filing.",
                    "sources": [
                        {"url": "https://sec.gov/acme-10k", "title": "10-K", "domain": "sec.gov"},
                        {"url": "https://acme.com/about", "title": "About", "domain": "acme.com"},
                    ],
                },
            }
        ],
        final_answer="Acme is incorporated in Delaware.",
        run_id="t-archive",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))

        # Evidence no longer leaks into the graph — no /evidence/* fragments
        # and no asserted_by / supported_by / contradicts / under_hypothesis edges.
        assert not any(f.canonical_path.startswith("/evidence/") for f in res.graph.fragments)
        assert not any(
            e.relation in ("asserted_by", "supported_by", "contradicts", "under_hypothesis")
            for e in res.graph.edges
        )

        # Archive populated.
        assert len(res.evidence_archive.sources) == 2
        assert {s.domain for s in res.evidence_archive.sources} == {"sec.gov", "acme.com"}
        asserted = [l for l in res.evidence_archive.links if l.relation == "asserted_by"]
        assert len(asserted) >= 1
        assert asserted[0].claim_id == "n1"

    asyncio.run(_go())


def test_pipeline_consumes_analyzer_proposed_relations() -> None:
    """Phase 4 pre-pass folds analyzer proposedRelations into edges even when LLM returns none."""
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Tesla, Inc.",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "agentId": "analyzer:e1",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Model S",
                        "type": "product",
                        "subtype": "ev",
                        "agentId": "analyzer:e2",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {"relations": []},
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Tesla produces the Model S."}],
                "edges": [],
            }
        },
        analyzer_output={
            "chainSummary": "Tesla produces the Model S.",
            "proposedEntities": [
                {"id": "e1", "label": "Tesla, Inc."},
                {"id": "e2", "label": "Model S"},
            ],
            "proposedNodes": [],
            "proposedRelations": [
                {
                    "srcId": "e1",
                    "dstId": "e2",
                    "relation": "produces",
                    "sourceClaimIds": ["n1"],
                }
            ],
            "openQuestions": [],
        },
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-ana-edge",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        prod = [e for e in res.graph.edges if e.relation == "produces"]
        assert len(prod) == 1
        assert prod[0].layer_section == "catalog"
        assert prod[0].qualifiers.get("origin") == "analyzer_proposed"

    asyncio.run(_go())


def test_pipeline_proposed_relation_union_with_llm() -> None:
    """Analyzer gives one edge, LLM returns a different one — both appear, no duplication."""
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Tesla, Inc.",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "agentId": "analyzer:e1",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Model S",
                        "type": "product",
                        "subtype": "ev",
                        "agentId": "analyzer:e2",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "NASDAQ",
                        "type": "market",
                        "subtype": "exchange",
                        "agentId": "analyzer:e3",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "listed_on",
                        "layer_section": "ownership",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e2",
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [
                    {
                        "id": "n1",
                        "kind": "claim",
                        "text": "Tesla produces the Model S and is listed on NASDAQ.",
                    }
                ],
                "edges": [],
            }
        },
        analyzer_output={
            "chainSummary": "Tesla produces the Model S and is listed on NASDAQ.",
            "proposedEntities": [
                {"id": "e1", "label": "Tesla, Inc."},
                {"id": "e2", "label": "Model S"},
                {"id": "e3", "label": "NASDAQ"},
            ],
            "proposedNodes": [],
            "proposedRelations": [
                {
                    "srcId": "e1",
                    "dstId": "e2",
                    "relation": "produces",
                    "sourceClaimIds": ["n1"],
                }
            ],
            "openQuestions": [],
        },
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-union",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        rels = sorted({e.relation for e in res.graph.edges if e.relation != "supported_by"})
        assert "produces" in rels
        assert "listed_on" in rels
        produces = [e for e in res.graph.edges if e.relation == "produces"]
        listed = [e for e in res.graph.edges if e.relation == "listed_on"]
        assert len(produces) == 1
        assert len(listed) == 1

    asyncio.run(_go())


def test_pipeline_proposed_relation_dedup_with_llm() -> None:
    """If analyzer + LLM emit the same edge, it appears only once."""
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Tesla, Inc.",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "agentId": "analyzer:e1",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Model S",
                        "type": "product",
                        "subtype": "ev",
                        "agentId": "analyzer:e2",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "produces",
                        "layer_section": "catalog",
                        "layer": "structural",
                        "from_id": "e0",
                        "to_id": "e1",
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Tesla produces the Model S."}],
                "edges": [],
            }
        },
        analyzer_output={
            "chainSummary": "Tesla produces the Model S.",
            "proposedEntities": [
                {"id": "e1", "label": "Tesla, Inc."},
                {"id": "e2", "label": "Model S"},
            ],
            "proposedNodes": [],
            "proposedRelations": [
                {
                    "srcId": "e1",
                    "dstId": "e2",
                    "relation": "produces",
                    "sourceClaimIds": ["n1"],
                }
            ],
            "openQuestions": [],
        },
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-dedup",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        produces = [e for e in res.graph.edges if e.relation == "produces"]
        assert len(produces) == 1
        assert produces[0].qualifiers.get("origin") == "analyzer_proposed"

    asyncio.run(_go())


def test_pipeline_actor_level_supplies_and_carrier_hub_land_with_kinds() -> None:
    """Key Malacca-style case — analyzer emits actor-level supplies org->geo
    and carrier-level transships_at org->site. Both should land with
    recognized kind labels, not relation_errors."""
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Saudi Aramco",
                        "type": "org",
                        "subtype": "oil_major",
                        "agentId": "analyzer:e1",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "sa"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "China",
                        "type": "geo",
                        "subtype": "country",
                        "agentId": "analyzer:e2",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "cn"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Maersk",
                        "type": "org",
                        "subtype": "carrier",
                        "agentId": "analyzer:e3",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "dk"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Singapore",
                        "type": "site",
                        "subtype": "port",
                        "agentId": "analyzer:e4",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "sg"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "crude oil",
                        "type": "product",
                        "subtype": "hydrocarbon",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {"relations": []},
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [
                    {
                        "id": "n1",
                        "kind": "claim",
                        "text": (
                            "Saudi Aramco supplies crude oil to China; "
                            "Maersk transships at Singapore."
                        ),
                    }
                ],
                "edges": [],
            }
        },
        analyzer_output={
            "chainSummary": (
                "Saudi Aramco supplies crude oil to China; Maersk transships at Singapore."
            ),
            "proposedEntities": [
                {"id": "e1", "label": "Saudi Aramco"},
                {"id": "e2", "label": "China"},
                {"id": "e3", "label": "Maersk"},
                {"id": "e4", "label": "Singapore"},
            ],
            "proposedNodes": [],
            "proposedRelations": [
                {
                    "srcId": "e1",
                    "dstId": "e2",
                    "relation": "supplies",
                    "attributes": {"product": "crude oil"},
                    "sourceClaimIds": ["n1"],
                },
                {
                    "srcId": "e3",
                    "dstId": "e4",
                    "relation": "transships_at",
                    "sourceClaimIds": ["n1"],
                },
            ],
            "openQuestions": [],
        },
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-kind-projections",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        supplies = [e for e in res.graph.edges if e.relation == "supplies"]
        assert len(supplies) == 1
        assert supplies[0].attributes.get("kind") == "actor_to_region"
        transships = [e for e in res.graph.edges if e.relation == "transships_at"]
        assert len(transships) == 1
        assert transships[0].attributes.get("kind") == "carrier_hub"
        # No forbidden-endpoint hard errors, no unspecified warnings for these.
        assert not any(
            re.code == "relation_endpoints_forbidden"
            for re in res.validation_report.relation_errors
        )
        assert not any(
            w.code == "relation_endpoints_unspecified"
            for w in res.validation_report.relation_kind_warnings
        )

    asyncio.run(_go())


def test_pipeline_supplies_incoterm_valid() -> None:
    """supplies edge with attributes.incoterm from INCOTERMS_2020 passes validation."""
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Port Hedland",
                        "type": "site",
                        "subtype": "port",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "au"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Port Shanghai",
                        "type": "site",
                        "subtype": "port",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "cn"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Iron Ore",
                        "type": "product",
                        "subtype": "bulk",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "supplies",
                        "layer_section": "flow_topology",
                        "layer": "flow",
                        "from_id": "e0",
                        "to_id": "e1",
                        "attributes": {"product": "Iron Ore", "incoterm": "FOB"},
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [
                    {
                        "id": "n1",
                        "kind": "claim",
                        "text": "Iron ore ships FOB Port Hedland to Port Shanghai.",
                    }
                ],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-inco-ok",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        supplies = [e for e in res.graph.edges if e.relation == "supplies"]
        assert len(supplies) == 1
        assert supplies[0].attributes.get("incoterm") == "FOB"
        assert not any(
            "bad_incoterm" in w for w in res.validation_report.dictionary_warnings
        )

    asyncio.run(_go())


def test_pipeline_supplies_incoterm_invalid_warns() -> None:
    """supplies with incoterm outside INCOTERMS_2020 → bad_incoterm warning (soft)."""
    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Port Hedland",
                        "type": "site",
                        "subtype": "port",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "au"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Port Shanghai",
                        "type": "site",
                        "subtype": "port",
                        "claim_refs": ["n1"],
                        "context_hints": {"iso2Hint": "cn"},
                        "per_claim_confidence": 0.9,
                    },
                    {
                        "surface": "Iron Ore",
                        "type": "product",
                        "subtype": "bulk",
                        "claim_refs": ["n1"],
                        "per_claim_confidence": 0.9,
                    },
                ]
            },
            "4": {
                "relations": [
                    {
                        "relation": "supplies",
                        "layer_section": "flow_topology",
                        "layer": "flow",
                        "from_id": "e0",
                        "to_id": "e1",
                        "attributes": {"product": "Iron Ore", "incoterm": "XYZ"},
                        "source_claim_ids": ["n1"],
                        "per_claim_confidence": 0.8,
                    }
                ]
            },
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n1", "kind": "claim", "text": "Iron ore ships XYZ from port to port."}],
                "edges": [],
            }
        },
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-inco-bad",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        supplies = [e for e in res.graph.edges if e.relation == "supplies"]
        assert len(supplies) == 1
        warns = res.validation_report.dictionary_warnings
        assert any("bad_incoterm" in w for w in warns), warns
        # Soft warn only — no hard relation_errors about the incoterm itself.
        assert not any(
            "bad_incoterm" in (ri.code or "") for ri in res.validation_report.relation_errors
        )

    asyncio.run(_go())


# ---------------------------------------------------------------------------
# Adaptation tests for L1+L2+L3 agent changes:
#   * sanctions_output is a first-class blob (not just merged via pack)
#   * pack.proposedEntities[].addedAtCycle propagates to fragment provenance
#   * cross-doc relation refs (n12, analyzer:e3 stale) are recorded distinctly
# ---------------------------------------------------------------------------


def test_pipeline_sanctions_output_flows_through() -> None:
    """sanctions_output (chainSummary + proposedEntities + proposedRelations) reaches phase4.

    Previously phase0 only looked at analyzer_output / governance_output, so anything
    coming from sanctions_analyzer was silently dropped (only entities that survived
    the supervisor pack-merge made it through). Now sanctions_output is a peer blob.
    """

    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "SMIC",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["sanctions:c1"],
                        "context_hints": {"iso2Hint": "cn"},
                        "per_claim_confidence": 0.9,
                        "agentId": "sanctions:e1",
                    },
                    {
                        "surface": "OFAC SDN",
                        "type": "regime",
                        "subtype": "sanctions_list",
                        "claim_refs": ["sanctions:c1"],
                        "per_claim_confidence": 0.9,
                        "agentId": "sanctions:e2",
                    },
                ]
            },
            "4": {"relations": []},
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={"reasoningGraph": {"nodes": [], "edges": []}},
        analyzer_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        sanctions_output={
            "chainSummary": "SMIC is sanctioned under the OFAC SDN list.",
            "proposedEntities": [
                {"id": "e1", "label": "SMIC"},
                {"id": "e2", "label": "OFAC SDN"},
            ],
            "proposedRelations": [
                {"srcId": "e1", "dstId": "e2", "relation": "designated_under"},
            ],
            "proposedNodes": [],
            "openQuestions": [],
        },
        tool_ledger=[],
        final_answer="",
        run_id="t-sanctions",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        assert not any(x.code == "invalid_input" for x in res.validation_report.input_errors)
        assert len(res.graph.fragments) >= 2
        # SMIC and OFAC SDN must both materialize as canonical fragments.
        labels = {sf for f in res.graph.fragments for sf in f.surface_forms}
        assert "SMIC" in labels
        assert "OFAC SDN" in labels

    asyncio.run(_go())


def test_pipeline_added_at_cycle_propagates_from_pack() -> None:
    """pack.proposedEntities[].addedAtCycle reaches CanonicalFragment.attributes.addedAtCycle.

    Provenance signal so downstream consumers can distinguish entities added on
    cycle 1 vs cycle 7 of the same supervisor session.
    """

    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "TSMC",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["analyzer:c1"],
                        "context_hints": {"iso2Hint": "tw"},
                        "per_claim_confidence": 0.95,
                        "agentId": "analyzer:e1",
                    },
                    {
                        "surface": "ASML",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["analyzer:c2"],
                        "context_hints": {"iso2Hint": "nl"},
                        "per_claim_confidence": 0.95,
                        "agentId": "analyzer:e2",
                    },
                ]
            },
            "4": {"relations": []},
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "cycleIndex": 7,
            "reasoningGraph": {"nodes": [], "edges": []},
            "proposedEntities": [
                {"namespacedId": "analyzer:e1", "label": "TSMC", "addedAtCycle": 1},
                {"namespacedId": "analyzer:e2", "label": "ASML", "addedAtCycle": 5},
            ],
        },
        analyzer_output={
            "chainSummary": "TSMC depends on ASML EUV.",
            "proposedEntities": [
                {"id": "e1", "label": "TSMC"},
                {"id": "e2", "label": "ASML"},
            ],
            "proposedNodes": [],
            "openQuestions": [],
        },
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-provenance",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        by_surface = {sf: f for f in res.graph.fragments for sf in f.surface_forms}
        tsmc = by_surface.get("TSMC")
        asml = by_surface.get("ASML")
        assert tsmc is not None and asml is not None
        assert int((tsmc.attributes or {}).get("addedAtCycle") or 0) == 1
        assert int((asml.attributes or {}).get("addedAtCycle") or 0) == 5
        # Provenance.extra also carries it for downstream consumers reading the provenance block.
        tsmc_prov = tsmc.provenance.model_dump(mode="json", by_alias=True)
        assert tsmc_prov.get("agentFirstCycle") == 1

    asyncio.run(_go())


def test_pipeline_pack_fragment_relation_ref_recorded_distinctly() -> None:
    """Cross-doc refs to pack.reasoningGraph fragment ids (n12) get a dedicated input issue.

    Agents in follow-up mode are allowed by ``AnalyzerOutput`` to emit relations whose
    endpoints are pack reasoning-graph node ids. The canonicalizer cannot resolve those
    to canonical entities (they don't go through phase1), but the failure should be
    distinguishable from "unknown id" or "stale namespaced ref" so downstream tooling
    can decide whether to stitch the pack-fragment in later.
    """

    mock = LLMPhaseClient(
        mock_responses={
            "1": {
                "mentions": [
                    {
                        "surface": "Acme",
                        "type": "org",
                        "subtype": "listed_issuer",
                        "claim_refs": ["analyzer:c1"],
                        "context_hints": {"iso2Hint": "us"},
                        "per_claim_confidence": 0.9,
                        "agentId": "analyzer:e1",
                    },
                ]
            },
            "4": {"relations": []},
            "6": {"metrics": []},
        }
    )
    inp = CanonicalizerInput(
        pack={
            "reasoningGraph": {
                "nodes": [{"id": "n12", "kind": "claim", "text": "Pre-existing claim about something."}],
                "edges": [],
            }
        },
        analyzer_output={
            "chainSummary": "Acme depends on the n12-anchored claim.",
            "proposedEntities": [{"id": "e1", "label": "Acme"}],
            "proposedRelations": [
                {"srcId": "e1", "dstId": "n12", "relation": "depends_on"},
                {"srcId": "e1", "dstId": "analyzer:e99", "relation": "depends_on"},
            ],
            "proposedNodes": [],
            "openQuestions": [],
        },
        governance_output={"chainSummary": "", "proposedNodes": [], "openQuestions": []},
        tool_ledger=[],
        final_answer="",
        run_id="t-crossdoc",
    )

    async def _go() -> None:
        res = await run_pipeline(inp, llm=mock, dict_runtime=DictionaryRuntime(None))
        codes = [x.code for x in res.validation_report.input_errors]
        assert "relation_endpoint_pack_fragment_ref" in codes
        assert "relation_endpoint_stale_namespaced_id" in codes
        # No fallback into the silent-drop bucket: every unresolved endpoint
        # picks up one of the two new dedicated codes.
        assert "entity_id_not_in_registry" not in codes

    asyncio.run(_go())

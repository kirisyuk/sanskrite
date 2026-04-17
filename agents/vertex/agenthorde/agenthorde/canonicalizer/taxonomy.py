"""Closed AH taxonomy: 11 fragment classes, relations from README §1–5, layers."""

from __future__ import annotations

from typing import FrozenSet, Literal

# First path segment (without leading slash) — must match README enum.
FragmentClass = Literal[
    "org",
    "person",
    "geo",
    "site",
    "product",
    "vessel_class",
    "route",
    "market",
    "regime",
    "project",
    "evidence",
]

# Incoterms 2020 — optional attribute on `supplies` / `charters` edges indicating
# commercial delivery terms. Canonical two-letter codes (ICC).
INCOTERMS_2020: FrozenSet[str] = frozenset(
    (
        "EXW",
        "FCA",
        "CPT",
        "CIP",
        "DAP",
        "DPU",
        "DDP",
        "FAS",
        "FOB",
        "CFR",
        "CIF",
    )
)


FRAGMENT_CLASSES: FrozenSet[str] = frozenset(
    (
        "org",
        "person",
        "geo",
        "site",
        "product",
        "vessel_class",
        "route",
        "market",
        "regime",
        "project",
        "evidence",
    )
)

Layer = Literal["structural", "contractual", "flow"]

LayerSection = Literal[
    "flow_topology",
    "actors_ops",
    "regimes_risks",
    "ownership",
    "catalog",
    "evidence_meta",
    "corporate_events",
]

# All relation names allowed by the canonicalizer (README + plan extras).
RelationType = Literal[
    # §1 flow / topology
    "supplies",
    "leg_connects",
    "transships_at",
    "passes_through",
    "alternate_to",
    "produces",
    # §2 actors
    "operates",
    "charters",
    "regulates",
    # §3 regimes / risks
    "constrained_by",
    "requires_capability",
    "subject_to",
    "exposes_to",
    "mitigated_by",
    "priced_under",
    "benchmarked_to",
    # §4 ownership
    "subsidiary_of",
    "beneficially_owns",
    "controls_via",
    "holds_board_seat",
    "joint_venture_with",
    "listed_on",
    "incorporated_in",
    "managed_by",
    # corporate events (flow)
    "invested_in",
    "acquired_stake_from",
    "asset_contribution",
    "merged_into",
    "spun_out",
    "divested_to",
    # §5 evidence
    "asserted_by",
    "supported_by",
    "contradicts",
    "under_hypothesis",
]

RELATION_NAMES: FrozenSet[str] = frozenset(
    (
        "supplies",
        "leg_connects",
        "transships_at",
        "passes_through",
        "alternate_to",
        "produces",
        "operates",
        "charters",
        "regulates",
        "constrained_by",
        "requires_capability",
        "subject_to",
        "exposes_to",
        "mitigated_by",
        "priced_under",
        "benchmarked_to",
        "subsidiary_of",
        "beneficially_owns",
        "controls_via",
        "holds_board_seat",
        "joint_venture_with",
        "listed_on",
        "incorporated_in",
        "managed_by",
        "invested_in",
        "acquired_stake_from",
        "asset_contribution",
        "merged_into",
        "spun_out",
        "divested_to",
        "asserted_by",
        "supported_by",
        "contradicts",
        "under_hypothesis",
    )
)

# relation -> allowed layers (default structural if missing)
RELATION_ALLOWED_LAYERS: dict[str, FrozenSet[str]] = {
    "supplies": frozenset(("structural", "contractual", "flow")),
    "leg_connects": frozenset(("structural", "flow")),
    "transships_at": frozenset(("structural", "flow")),
        # passes_through: structural for the topology statement ("this route passes
        # through this strait"), flow for a specific shipment instance.
        "passes_through": frozenset(("structural", "flow")),
        "alternate_to": frozenset(("structural", "flow")),
    "produces": frozenset(("structural",)),
    "operates": frozenset(("structural",)),
    "charters": frozenset(("contractual", "flow")),
    "regulates": frozenset(("structural",)),
    "constrained_by": frozenset(("structural",)),
    "requires_capability": frozenset(("structural",)),
    "subject_to": frozenset(("structural", "flow")),
    "exposes_to": frozenset(("structural",)),
    "mitigated_by": frozenset(("structural",)),
    "priced_under": frozenset(("contractual", "flow")),
    "benchmarked_to": frozenset(("contractual",)),
    "subsidiary_of": frozenset(("structural",)),
    "beneficially_owns": frozenset(("structural",)),
    "controls_via": frozenset(("structural",)),
    "holds_board_seat": frozenset(("structural",)),
    "joint_venture_with": frozenset(("structural",)),
    "listed_on": frozenset(("structural",)),
    "incorporated_in": frozenset(("structural",)),
    "managed_by": frozenset(("structural",)),
    "invested_in": frozenset(("flow",)),
    "acquired_stake_from": frozenset(("flow",)),
    "asset_contribution": frozenset(("flow",)),
    "merged_into": frozenset(("flow",)),
    "spun_out": frozenset(("flow",)),
    "divested_to": frozenset(("flow",)),
    "asserted_by": frozenset(("structural",)),
    "supported_by": frozenset(("structural",)),
    "contradicts": frozenset(("structural",)),
    "under_hypothesis": frozenset(("structural",)),
}

RELATION_LAYER_SECTION: dict[str, LayerSection] = {
    "supplies": "flow_topology",
    "leg_connects": "flow_topology",
    "transships_at": "flow_topology",
    "passes_through": "flow_topology",
    "alternate_to": "flow_topology",
    "produces": "catalog",
    "operates": "actors_ops",
    "charters": "actors_ops",
    "regulates": "actors_ops",
    "constrained_by": "regimes_risks",
    "requires_capability": "regimes_risks",
    "subject_to": "regimes_risks",
    "exposes_to": "regimes_risks",
    "mitigated_by": "regimes_risks",
    "priced_under": "regimes_risks",
    "benchmarked_to": "regimes_risks",
    "subsidiary_of": "ownership",
    "beneficially_owns": "ownership",
    "controls_via": "ownership",
    "holds_board_seat": "ownership",
    "joint_venture_with": "ownership",
    "listed_on": "ownership",
    "incorporated_in": "ownership",
    "managed_by": "ownership",
    "invested_in": "corporate_events",
    "acquired_stake_from": "corporate_events",
    "asset_contribution": "corporate_events",
    "merged_into": "corporate_events",
    "spun_out": "corporate_events",
    "divested_to": "corporate_events",
    "asserted_by": "evidence_meta",
    "supported_by": "evidence_meta",
    "contradicts": "evidence_meta",
    "under_hypothesis": "evidence_meta",
}


# -----------------------------------------------------------------------------
# Relation endpoint *kinds* — explicit semantic projections of each relation.
#
# Source of truth (and narrative explanation) lives in:
#   docs/horizon/architecture/supply-chain-relation-endpoint-kinds.md
#
# For every (relation, from_class, to_class) triple in RELATION_ENDPOINT_KINDS
# we assign a short snake_case `kind` label. The canonicalizer uses this table
# at phase-4 edge-building time to stamp `attributes.kind` on the resulting
# CanonicalEdge, and again at validation time to decide whether a pair is
# recognized, unspecified, or forbidden.
#
# Three-tier semantics (see validation.py):
#   - recognized   : (from, to) in RELATION_ENDPOINT_KINDS[relation]
#                    → attributes.kind = <label>; no validation write.
#   - unspecified  : (from, to) not in kinds AND not in blacklist
#                    → attributes.kind = "unspecified";
#                      ValidationReport.relation_kind_warnings + candidate
#   - forbidden    : (from, to) in RELATION_ENDPOINT_BLACKLIST[relation]
#                    → edge is NOT emitted;
#                      ValidationReport.relation_errors + candidate
# -----------------------------------------------------------------------------

RELATION_ENDPOINT_KINDS: dict[str, dict[tuple[str, str], str]] = {
    # §1 material flow / topology
    "supplies": {
        ("site", "site"): "physical_leg",
        ("org", "site"): "seller_to_destination",
        ("org", "org"): "commercial",
        ("org", "geo"): "actor_to_region",
        ("org", "market"): "actor_to_market",
        ("org", "route"): "actor_via_corridor",
        ("site", "geo"): "site_to_region",
        ("site", "market"): "site_to_market",
        ("geo", "geo"): "trade_flow",
        ("geo", "market"): "region_to_market",
    },
    "leg_connects": {
        ("site", "site"): "site_to_site",
        ("site", "route"): "site_to_route",
        ("route", "site"): "route_to_site",
        ("route", "geo"): "route_to_region",
        ("site", "geo"): "site_to_region",
    },
    "transships_at": {
        ("route", "site"): "route_hub",
        ("route", "geo"): "route_region",
        ("org", "site"): "carrier_hub",
        ("site", "site"): "hub_cascade",
    },
    "passes_through": {
        ("route", "geo"): "route_over_region",
        ("route", "site"): "route_over_site",
        ("route", "route"): "route_over_corridor",
        ("vessel_class", "route"): "class_over_corridor",
        ("vessel_class", "site"): "class_over_site",
        ("vessel_class", "geo"): "class_over_region",
        # A strait / canal / lock is modeled as a `site` but is a choke point
        # *on* a corridor (route). "Bosporus passes through Black Sea corridor",
        # "Suez Canal passes through Mediterranean→Red Sea corridor".
        ("site", "route"): "chokepoint_on_corridor",
    },
    "alternate_to": {
        ("route", "route"): "route_alt",
        ("site", "site"): "site_alt",
        ("vessel_class", "vessel_class"): "fleet_alt",
    },
    # §2 catalog
    "produces": {
        ("org", "product"): "org_produces_product_line",
    },
    # §3 actors / ops
    "operates": {
        ("org", "site"): "site_operator",
    },
    "charters": {
        ("org", "vessel_class"): "fleet_charter",
    },
    "regulates": {
        ("org", "geo"): "regulates_region",
        ("org", "site"): "regulates_site",
        ("org", "route"): "regulates_corridor",
        ("org", "market"): "regulates_market",
    },
    # §4 regimes / risks
    "constrained_by": {
        ("site", "geo"): "site_geo_rule",
        ("vessel_class", "geo"): "class_geo_rule",
        ("vessel_class", "route"): "class_over_route_draft",
        ("vessel_class", "site"): "class_over_site_draft",
        ("route", "route"): "route_over_corridor",
        ("route", "geo"): "route_over_region",
        ("site", "route"): "site_over_corridor",
    },
    "requires_capability": {
        ("product", "site"): "product_needs_capability",
    },
    "subject_to": {
        ("org", "regime"): "org_subject_to",
        ("site", "regime"): "site_subject_to",
        ("route", "regime"): "route_subject_to",
        ("vessel_class", "regime"): "class_subject_to",
        ("product", "regime"): "product_subject_to",
        ("geo", "regime"): "region_subject_to",
        ("market", "regime"): "market_subject_to",
        ("person", "regime"): "person_subject_to",
    },
    "exposes_to": {
        ("org", "regime"): "org_exposed",
        ("site", "regime"): "site_exposed",
        ("route", "regime"): "route_exposed",
        ("vessel_class", "regime"): "class_exposed",
        ("product", "regime"): "product_exposed",
        ("geo", "regime"): "region_exposed",
        ("market", "regime"): "market_exposed",
        ("person", "regime"): "person_exposed",
    },
    "mitigated_by": {
        ("regime", "route"): "regime_mitigated_by_route",
        ("regime", "org"): "regime_mitigated_by_org",
        ("regime", "regime"): "regime_chain",
    },
    "priced_under": {
        ("route", "regime"): "route_incoterm",
        ("route", "market"): "route_benchmark",
        ("product", "regime"): "product_incoterm",
        ("product", "market"): "product_benchmark",
        ("site", "regime"): "site_incoterm",
        ("site", "market"): "site_benchmark",
    },
    "benchmarked_to": {
        ("product", "market"): "product_benchmark",
        ("route", "market"): "route_benchmark",
        ("site", "market"): "site_benchmark",
    },
    # §5 ownership
    "subsidiary_of": {
        ("org", "org"): "subsidiary",
    },
    "beneficially_owns": {
        ("person", "org"): "ubo_stake",
        ("org", "org"): "institutional_stake",
    },
    "controls_via": {
        ("person", "org"): "person_controls_org",
        ("org", "org"): "org_controls_org",
    },
    "holds_board_seat": {
        ("person", "org"): "director",
        ("org", "org"): "nominee_org",
    },
    "joint_venture_with": {
        ("org", "org"): "jv",
    },
    "listed_on": {
        ("org", "market"): "listed",
    },
    "incorporated_in": {
        ("org", "geo"): "incorporated",
    },
    "managed_by": {
        ("org", "org"): "management_contract",
    },
    # §6 corporate events
    "invested_in": {
        ("org", "org"): "equity_investment",
    },
    "acquired_stake_from": {
        ("org", "org"): "secondary_stake_acquisition",
    },
    "asset_contribution": {
        ("org", "org"): "asset_in_kind",
    },
    "merged_into": {
        ("org", "org"): "merger",
    },
    "spun_out": {
        ("org", "org"): "spinoff",
    },
    "divested_to": {
        ("org", "org"): "divestiture",
    },
    # §7 evidence meta — not emitted into the graph (see EvidenceArchive).
    # Left intentionally empty: any such edge, if ever produced, becomes
    # unspecified → warning, not an error.
}


def _forbidden_pairs_for_unary(
    *,
    target: str,
    include_target_in_allowed_sources: bool = True,
) -> frozenset[tuple[str, str]]:
    """Blacklist for relations of shape (any_class) -> fixed target.

    We enumerate all FRAGMENT_CLASSES as *source* and explicitly mark as
    forbidden anything that isn't the target-typed endpoint, keeping the
    source free. Used for `subject_to`, `exposes_to` (target='regime'),
    `mitigated_by` (source='regime').
    """
    # Currently unused helper kept for future explicit blacklists; validation
    # falls through to "unspecified" for unknown source classes.
    del target, include_target_in_allowed_sources
    return frozenset()


# Explicit blacklists. Keep narrow: only pairs we know are semantically
# meaningless. Everything else is "unspecified" (soft warning).
RELATION_ENDPOINT_BLACKLIST: dict[str, frozenset[tuple[str, str]]] = {
    "supplies": frozenset(
        (
            ("person", "person"),
            ("person", "product"),
            ("product", "person"),
            ("vessel_class", "person"),
            ("person", "vessel_class"),
            ("product", "product"),
            ("regime", "regime"),
            ("evidence", "evidence"),
        )
    ),
    "leg_connects": frozenset(
        (
            ("org", "org"),
            ("org", "site"),
            ("site", "org"),
            ("person", "person"),
            ("product", "product"),
            ("regime", "regime"),
        )
    ),
    "transships_at": frozenset(
        (
            ("person", "site"),
            ("product", "site"),
            ("regime", "site"),
            ("vessel_class", "vessel_class"),
        )
    ),
    "passes_through": frozenset(
        (
            ("org", "geo"),
            ("org", "site"),
            ("person", "site"),
            ("product", "site"),
        )
    ),
    "alternate_to": frozenset(
        (
            # Mixing classes at alt is nonsense: those belong in other relations.
            ("route", "site"),
            ("site", "route"),
            ("route", "vessel_class"),
            ("vessel_class", "route"),
            ("route", "org"),
            ("site", "org"),
        )
    ),
    "produces": frozenset(
        (
            ("product", "org"),
            ("person", "product"),
            ("site", "product"),
        )
    ),
    "operates": frozenset(
        (
            ("org", "org"),
            ("person", "site"),
            ("site", "org"),
        )
    ),
    "charters": frozenset(
        (
            ("person", "vessel_class"),
            ("vessel_class", "org"),
            ("site", "vessel_class"),
        )
    ),
    "regulates": frozenset(
        (
            ("site", "org"),
            ("route", "org"),
            ("geo", "org"),
            ("person", "org"),
        )
    ),
    # constrained_by: if source is an org/person/product we don't have a
    # modelled interpretation — forbid and steer into subject_to / exposes_to.
    "constrained_by": frozenset(
        (
            ("org", "geo"),
            ("org", "route"),
            ("org", "site"),
            ("person", "geo"),
            ("product", "geo"),
        )
    ),
    # requires_capability is narrowly typed (product→site); forbid clear misuses.
    "requires_capability": frozenset(
        (
            ("site", "product"),
            ("org", "site"),
            ("person", "site"),
        )
    ),
    # subject_to / exposes_to: target must be regime. Forbid non-regime targets.
    "subject_to": frozenset(),  # handled procedurally (see is_forbidden_endpoint_pair)
    "exposes_to": frozenset(),  # handled procedurally
    # mitigated_by: source must be regime. Forbid non-regime sources.
    "mitigated_by": frozenset(),  # handled procedurally
    # priced_under: target must be market or regime.
    "priced_under": frozenset(),  # handled procedurally
    # benchmarked_to: target must be market.
    "benchmarked_to": frozenset(),  # handled procedurally
    # Ownership / corp events: narrow classes. Forbid clear type inversions.
    "subsidiary_of": frozenset(
        (
            ("person", "org"),
            ("org", "person"),
            ("person", "person"),
        )
    ),
    "beneficially_owns": frozenset(
        (
            ("org", "person"),
            ("person", "person"),
        )
    ),
    "controls_via": frozenset(
        (
            ("org", "person"),
            ("person", "person"),
        )
    ),
    "holds_board_seat": frozenset(
        (
            ("org", "person"),
            ("person", "person"),
        )
    ),
    "joint_venture_with": frozenset(
        (
            ("person", "org"),
            ("org", "person"),
            ("person", "person"),
        )
    ),
    "listed_on": frozenset(
        (
            ("market", "org"),
            ("person", "market"),
        )
    ),
    "incorporated_in": frozenset(
        (
            ("geo", "org"),
            ("person", "geo"),
        )
    ),
    "managed_by": frozenset(
        (
            ("person", "org"),
            ("org", "person"),
        )
    ),
    "invested_in": frozenset(
        (
            ("person", "org"),
            ("org", "person"),
        )
    ),
    "acquired_stake_from": frozenset(
        (
            ("person", "org"),
            ("org", "person"),
        )
    ),
    "asset_contribution": frozenset(),
    "merged_into": frozenset(),
    "spun_out": frozenset(),
    "divested_to": frozenset(),
}


def relation_kind_for_endpoints(
    relation: str,
    from_class: str,
    to_class: str,
) -> str | None:
    """Return the `kind` label for a recognized projection, else None.

    None means either `unspecified` (keep edge with attributes.kind="unspecified"
    and emit kind-warning) or `forbidden` — callers must additionally consult
    `is_forbidden_endpoint_pair` to distinguish.
    """
    return RELATION_ENDPOINT_KINDS.get(relation, {}).get((from_class, to_class))


def is_forbidden_endpoint_pair(
    relation: str,
    from_class: str,
    to_class: str,
) -> bool:
    """Procedural + table-driven blacklist check.

    Procedural cases (target/source must be of a fixed class):
        subject_to / exposes_to  : to_class must be 'regime'
        mitigated_by             : from_class must be 'regime'
        priced_under             : to_class must be 'regime' or 'market'
        benchmarked_to           : to_class must be 'market'

    Then the per-relation explicit blacklist table is consulted.
    """
    if relation in ("subject_to", "exposes_to"):
        if to_class != "regime":
            return True
    if relation == "mitigated_by":
        if from_class != "regime":
            return True
    if relation == "priced_under":
        if to_class not in ("regime", "market"):
            return True
    if relation == "benchmarked_to":
        if to_class != "market":
            return True
    bl = RELATION_ENDPOINT_BLACKLIST.get(relation, frozenset())
    return (from_class, to_class) in bl


def path_fragment_class(canonical_path: str) -> str | None:
    p = (canonical_path or "").strip().lower()
    if not p.startswith("/"):
        return None
    parts = [x for x in p.split("/") if x]
    if not parts:
        return None
    c = parts[0]
    return c if c in FRAGMENT_CLASSES else None


def relation_allowed_for_endpoints(relation: str, from_class: str, to_class: str) -> bool:
    """DEPRECATED binary whitelist check.

    Superseded by `relation_kind_for_endpoints` (recognized projections) +
    `is_forbidden_endpoint_pair` (hard blacklist). This wrapper is retained for
    backwards compatibility with callers that only need a yes/no answer;
    semantically it now means "recognized projection exists for this pair".

    Note: under the new model, `False` here no longer implies the edge is
    invalid — it may simply be `unspecified` (soft warning). Validation.py
    handles the three-tier logic directly against the kind/blacklist tables.
    """
    if relation not in RELATION_NAMES:
        return False
    return relation_kind_for_endpoints(relation, from_class, to_class) is not None


def _legacy_relation_whitelist(relation: str, from_class: str, to_class: str) -> bool:
    """Exact previous behaviour, kept purely for historical tests / debug."""
    r = relation
    a, b = from_class, to_class
    if r not in RELATION_NAMES:
        return False
    rules: dict[str, list[tuple[str, str]]] = {
        "supplies": [("site", "site")],
        # A "leg" of a logistics path: either two sites, a site anchored to a route
        # (e.g. port → strait), a route anchored to a site/geo (strait → port, strait → country),
        # or a site→geo hop where the geo is the destination region.
        "leg_connects": [
            ("site", "site"),
            ("site", "route"),
            ("route", "site"),
            ("route", "geo"),
            ("site", "geo"),
        ],
        # Transship can happen at a site, but also at a geo (a port-free anchorage zone)
        # or between two route segments meeting at a hub.
        "transships_at": [("route", "site"), ("route", "geo")],
        # A route or vessel class passes through a geo, another route (chokepoint) or site
        # (a specific strait/lock/canal modeled as a site).
        "passes_through": [
            ("route", "geo"),
            ("route", "site"),
            ("route", "route"),
            ("vessel_class", "route"),
            ("vessel_class", "site"),
            ("vessel_class", "geo"),
        ],
        "alternate_to": [("route", "route"), ("site", "site")],
        "produces": [("org", "product")],
        "operates": [("org", "site")],
        # A charter covers a specific vessel or an entire class of vessels.
        "charters": [("org", "vessel_class")],
        "regulates": [("org", "geo"), ("org", "site"), ("org", "route"), ("org", "market")],
        # Physical / regulatory constraints: a route can be constrained by another route
        # (chokepoint gating a corridor), a vessel class is draft-constrained by a route
        # or site, and sites can be geo-constrained.
        "constrained_by": [
            ("site", "geo"),
            ("vessel_class", "geo"),
            ("vessel_class", "route"),
            ("vessel_class", "site"),
            ("route", "route"),
            ("route", "geo"),
            ("site", "route"),
        ],
        "requires_capability": [("product", "site")],
        "subject_to": [],  # any -> regime
        "exposes_to": [],
        "mitigated_by": [("regime", "route"), ("regime", "org")],
        # Commercial pricing: a cargo / product / route / leg can be priced under
        # an incoterm/pricing regime or benchmarked on a market hub.
        "priced_under": [
            ("route", "regime"),
            ("route", "market"),
            ("product", "regime"),
            ("product", "market"),
            ("site", "regime"),
            ("site", "market"),
        ],
        "benchmarked_to": [
            ("product", "market"),
            ("route", "market"),
            ("site", "market"),
        ],
        "subsidiary_of": [("org", "org")],
        # Beneficial ownership covers both:
        #   person -> org (ultimate beneficial owner, founders, insiders)
        #   org    -> org (institutional holders: Vanguard/BlackRock/State Street stake in listed issuer)
        # Controlling vs non-controlling stake is expressed via attributes.stake_pct, not via a separate relation.
        "beneficially_owns": [("person", "org"), ("org", "org")],
        "controls_via": [("person", "org"), ("org", "org")],
        "holds_board_seat": [("person", "org"), ("org", "org")],
        "joint_venture_with": [("org", "org")],
        "listed_on": [("org", "market")],
        "incorporated_in": [("org", "geo")],
        "managed_by": [("org", "org")],
        "invested_in": [("org", "org")],
        "acquired_stake_from": [("org", "org")],
        "asset_contribution": [("org", "org")],
        "merged_into": [("org", "org")],
        "spun_out": [("org", "org")],
        "divested_to": [("org", "org")],
        "asserted_by": [],
        "supported_by": [],
        "contradicts": [("evidence", "evidence")],
        "under_hypothesis": [],
    }
    if r in ("subject_to", "exposes_to"):
        return b == "regime" and a in FRAGMENT_CLASSES
    if r == "asserted_by":
        return a == "evidence" and b == "evidence"
    if r == "supported_by":
        return b == "evidence" and a in FRAGMENT_CLASSES
    if r == "under_hypothesis":
        return b == "evidence" and a in FRAGMENT_CLASSES
    pairs = rules.get(r, [])
    if not pairs:
        return False
    return (a, b) in pairs


def default_layer_for_relation(relation: str) -> str:
    layers = RELATION_ALLOWED_LAYERS.get(relation, frozenset(("structural",)))
    return "structural" if "structural" in layers else next(iter(layers))


def repair_relation(relation: str, from_class: str, to_class: str) -> tuple[str, str | None]:
    """
    Auto-fix common LLM mistakes after endpoint swapping (see `should_swap_endpoints`).
    Returns (new_relation, repair_note or None). Does not change endpoints.
    """
    r = relation
    a, b = from_class, to_class
    if r == "subsidiary_of" and a == "person" and b == "org":
        return "beneficially_owns", "subsidiary_of_rewired_person_org"
    if r == "subsidiary_of" and a == "org" and b == "person":
        return "beneficially_owns", "subsidiary_of_rewired_org_person_inverted"
    if r == "joint_venture_with" and a == "person" and b == "org":
        return "controls_via", "jv_rewired_person_org"
    return r, None


def should_swap_endpoints(relation: str, from_class: str, to_class: str) -> bool:
    """If True, caller should swap source/target and re-check."""
    if relation == "listed_on" and from_class == "market" and to_class == "org":
        return True
    if relation == "beneficially_owns" and from_class == "org" and to_class == "person":
        return True
    if relation == "produces" and from_class == "product" and to_class == "org":
        return True
    return False


# Plain one-to-one synonyms where the canonical relation does not depend on endpoint types.
# Keys are normalized (lowercased, no spaces). Values are canonical relation names.
_FLAT_RELATION_SYNONYMS: dict[str, str] = {
    "registered_in": "incorporated_in",
    "registeredin": "incorporated_in",
    "domiciled_in": "incorporated_in",
    "headquartered_in": "incorporated_in",
    "hq_in": "incorporated_in",
    "incorporatedin": "incorporated_in",
    "listed_at": "listed_on",
    "traded_on": "listed_on",
    "trades_on": "listed_on",
    "quoted_on": "listed_on",
    "parent_of": "subsidiary_of",  # direction is handled by caller (swap)
    "owned_by": "subsidiary_of",  # direction handled by caller (swap)
    "owns_subsidiary": "subsidiary_of",
    "has_subsidiary": "subsidiary_of",
    "board_member_of": "holds_board_seat",
    "director_of": "holds_board_seat",
    "ceo_of": "holds_board_seat",
    "runs": "operates",
    "run_by": "operates",  # direction handled later
    "operator_of": "operates",
    "controls": "controls_via",
    "regulated_by": "regulates",  # direction handled later
    "manufactures": "produces",
    "makes": "produces",
    "outputs": "produces",
    "builds": "produces",
    "assembles": "produces",
    "produces_product": "produces",
}

# Synonyms that should swap endpoints as part of the canonicalization.
# Keys are normalized LLM relation; value is the canonical name. Caller must swap fid/tid.
_INVERSE_RELATION_SYNONYMS: dict[str, str] = {
    "owned_by": "subsidiary_of",
    "parent_of": "subsidiary_of",  # "A parent_of B" means B subsidiary_of A
    "run_by": "operates",  # "site run_by org" means org operates site
    "regulated_by": "regulates",
}


def canonicalize_llm_synonym(
    relation: str,
    from_class: str,
    to_class: str,
) -> tuple[str | None, bool, str | None]:
    """
    Map common LLM wording to canonical relation names.

    Returns (canonical_relation_or_None, invert_endpoints, note_or_None).
    - canonical_relation_or_None: None if no mapping found (caller should treat as unknown).
    - invert_endpoints: True if caller must swap fid/tid after mapping.
    - note: human-readable repair note for qualifiers.

    Endpoint-dependent mappings:
    - "owns" / "holds": person->org -> beneficially_owns; org->org -> subsidiary_of (inverted).
    - "subsidiary" noun form: treated same as parent_of when org->org (requires swap).
    """
    if not relation:
        return None, False, None
    key = relation.strip().lower().replace(" ", "_").replace("-", "_")
    if key in RELATION_NAMES:
        return key, False, None

    # Endpoint-sensitive handling for generic "owns" / "holds".
    if key in ("owns", "holds", "holds_stake_in", "has_stake_in", "stake_in"):
        if from_class == "person" and to_class == "org":
            return "beneficially_owns", False, f"synonym:{key}->beneficially_owns"
        if from_class == "org" and to_class == "org":
            # "A owns B" (org->org) == "B subsidiary_of A" -> invert.
            return "subsidiary_of", True, f"synonym:{key}->subsidiary_of(inverted)"
        return None, False, None

    if key in _INVERSE_RELATION_SYNONYMS:
        return _INVERSE_RELATION_SYNONYMS[key], True, f"synonym:{key}->{_INVERSE_RELATION_SYNONYMS[key]}(inverted)"
    if key in _FLAT_RELATION_SYNONYMS:
        return _FLAT_RELATION_SYNONYMS[key], False, f"synonym:{key}->{_FLAT_RELATION_SYNONYMS[key]}"
    return None, False, None

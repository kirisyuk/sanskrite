"""Industrial dependency-depth priors callable as a tool.

What lives here
---------------
For each domain (e.g. ``"semiconductors"``, ``"shipping"``) we keep:

* a short ``description`` explaining what kind of question the priors apply to,
* a list of ``hints`` — each hint is a `surface entity / phenomenon` (Tier-1 in
  supply-chain language, but the language is intentionally generic) plus its
  one or two **deeper dependencies** that are typically the actual chokepoint
  even when the surface entity is what gets named in the press.

The lookup function is **deliberately literal**: it does substring matching
against the entity / surface label and returns the matching hint blocks. We do
NOT do semantic search here — the analyzer's own LLM call is the semantic
layer. This file is just a structured, inspectable knowledge dictionary.

Why callable, not inlined
-------------------------
Originally these priors were inlined into every analyzer's system prompt as
"Domain priors". That had three problems:

1. **Domain bias in the bench.** We had detailed priors for semiconductors but
   nothing for shipping / energy / agri / pharma at the same depth, so the
   model looked smarter on semi than on other industries because it was
   literally being fed the answers.
2. **Token cost.** Every analyzer call on every cycle paid for the entire
   priors block even when the question was about something else.
3. **Hidden contract.** When the priors changed, the analyzer's system prompt
   silently changed too — there was no way to attribute a behavior shift to
   priors vs prompt vs model.

By making it a tool the model calls **when it judges it needs an industrial
hint**, we get clean separation: prompt = methodology, knowledge = data.

Adding a new domain
-------------------
Just append a new key to ``DOMAIN_PRIORS`` with the same shape. Keep the
``surface`` label literal — it's what the lookup matches against. Keep the
``deeper`` list short (1-3 entries) — the analyzer should still do the
synthesis; this is a hint, not a full graph.
"""

from __future__ import annotations

from typing import Any


_HintBlock = dict[str, Any]


# ---------------------------------------------------------------------------
# Domain knowledge — append-only; do not delete entries without a migration.
# ---------------------------------------------------------------------------

DOMAIN_PRIORS: dict[str, dict[str, Any]] = {
    "semiconductors": {
        "description": (
            "Advanced-node manufacturing stack (lithography → materials → "
            "masks → wafers → packaging → EDA / IP). Apply when the question "
            "names foundries, equipment vendors, EDA, or chip-related export "
            "controls."
        ),
        "hints": [
            {
                "surface": "ASML EUV",
                "deeper": [
                    "ZEISS SMT (optics, single-supplier worldwide)",
                    "Cymer (light source, ASML-owned subsidiary)",
                    "Trumpf (CO2 laser source for Cymer)",
                ],
                "note": "DUV is also ASML-dominant; Nikon and Canon are weak alternates.",
            },
            {
                "surface": "TSMC advanced node",
                "deeper": [
                    "JSR / Shin-Etsu Chemical / TOK / Sumitomo / Fujifilm (photoresist, Japan-concentrated)",
                    "Photronics / Toppan / DNP (photomasks)",
                    "SUMCO / Shin-Etsu Handotai / GlobalWafers / Siltronic (300mm wafers)",
                    "Linde / Air Liquide / Showa Denko (specialty etch / deposition gases)",
                ],
            },
            {
                "surface": "TSMC CoWoS / advanced packaging",
                "deeper": [
                    "Amkor / ASE / SPIL / JCET / Powertech (OSAT)",
                    "Ibiden / Unimicron / AT&S / Shinko (advanced packaging substrates)",
                ],
            },
            {
                "surface": "Cadence / Synopsys / Siemens EDA",
                "deeper": [
                    "ARM Holdings (CPU IP)",
                    "Imagination Technologies (GPU IP)",
                    "RISC-V Foundation (open ISA)",
                ],
            },
            {
                "surface": "HBM memory",
                "deeper": [
                    "SK Hynix / Samsung / Micron (three-way Tier-1, single-product critical)",
                    "Advanced packaging substrate makers",
                ],
            },
        ],
    },
    "critical_minerals": {
        "description": (
            "Refining / processing chokepoints for battery and magnet inputs. "
            "Apply when the question names lithium, rare earths, cobalt, "
            "nickel, manganese, graphite, or downstream batteries / EVs."
        ),
        "hints": [
            {
                "surface": "Lithium",
                "deeper": [
                    "Spodumene refining (China-dominated even when ore is Australian)",
                    "Brine processing in Chile / Argentina (different chemistry, slower)",
                ],
            },
            {
                "surface": "Rare earths",
                "deeper": [
                    "Separation / refining capacity (China >85% of refining even when ore mined elsewhere)",
                    "Magnet manufacturing (China-concentrated for NdFeB sintered magnets)",
                ],
            },
            {
                "surface": "Cobalt",
                "deeper": [
                    "DRC mining (artisanal share material)",
                    "Chinese hydrometallurgical refining (CMOC / Huayou)",
                ],
            },
            {
                "surface": "Nickel",
                "deeper": [
                    "Indonesia HPAL / Class-1 distinction (battery-grade vs stainless feed)",
                    "Norilsk (Russia) for Class-1 sulfide nickel",
                ],
            },
        ],
    },
    "energy_seaborne": {
        "description": (
            "Seaborne energy logistics (crude, refined products, LNG). Apply "
            "when the question names tankers, oil corridors, gas trains, "
            "Russian / Iranian / Venezuelan sanctions, price caps, or "
            "specific straits / canals."
        ),
        "hints": [
            {
                "surface": "Crude oil tankers",
                "deeper": [
                    "Vessel-class corridor sizing: Suezmax → Suez; VLCC cannot transit Suez fully laden → Malacca / Cape; "
                    "Aframax → Baltic / Turkish Straits; ULCC → Cape only.",
                    "Shadow fleet ageing (avg vessel age, IACS class withdrawal)",
                    "P&I insurance coverage (IG vs non-IG) as a sanctions filter",
                ],
            },
            {
                "surface": "LNG carriers",
                "deeper": [
                    "Vessel sizing: Q-Max / Q-Flex → Suez or Cape; standard LNGCs flexible.",
                    "Liquefaction trains (long lead time; concrete chokepoint behind 'LNG export' surface)",
                    "Re-gasification capacity at receiving terminals",
                ],
            },
            {
                "surface": "Russian crude exports",
                "deeper": [
                    "G7 oil price cap mechanism (attestation chain, P&I insurance gate)",
                    "Shadow fleet (often UAE / Marshall Islands flag, ex-IG insurance)",
                    "STS hubs (Lakonikos Gulf, Ceuta, Sohar) for ship-to-ship transfer",
                    "PRA benchmarks: Argus → Urals / ESPO; Platts → Dated Brent",
                ],
            },
            {
                "surface": "Strait of Malacca",
                "deeper": [
                    "Alternates: Sunda Strait (depth-limited); Lombok Strait (deeper, longer detour)",
                    "Singapore bunkering hub dependency",
                    "PLA-Navy reach via SCS as a strategic chokepoint",
                ],
            },
        ],
    },
    "shipping_governance": {
        "description": (
            "Vessel ownership, control, flagging, and insurance layers. "
            "Apply when the question names ship operators, owners, UBOs, "
            "flag states, P&I clubs, or class societies."
        ),
        "hints": [
            {
                "surface": "Vessel ownership",
                "deeper": [
                    "Operator vs owner vs commercial manager vs technical manager (often four different entities)",
                    "Beneficial owner (often a one-ship SPV in BVI / Marshall Islands / UAE)",
                    "Disponent owner under bareboat charter (controls, but not the registered owner)",
                ],
            },
            {
                "surface": "Flag state",
                "deeper": [
                    "IACS class society (DNV / ABS / LR / BV / NK / KR / RINA / CCS / RS / IRS / PRS / CRS)",
                    "Port-state-control regimes (Paris MoU, Tokyo MoU)",
                    "Flag-of-convenience risk (Liberia / Marshall Islands / Panama vs national flags)",
                ],
            },
            {
                "surface": "P&I insurance",
                "deeper": [
                    "International Group (IG) of P&I Clubs (13 clubs, ~90% of world tonnage)",
                    "Non-IG cover (Russian / Chinese / shadow-market alternatives)",
                ],
            },
        ],
    },
    "pharma_api": {
        "description": (
            "Active pharmaceutical ingredient supply chain. Apply when the "
            "question names APIs, generics, KSMs, or pharmaceutical "
            "geographic concentration."
        ),
        "hints": [
            {
                "surface": "Branded API",
                "deeper": [
                    "Key Starting Material (KSM) supplier (often China / India)",
                    "KSM jurisdiction is the strategic dependency, not the API plant itself",
                    "Multi-source qualification status under FDA / EMA",
                ],
            },
            {
                "surface": "Generic finished dose",
                "deeper": [
                    "API source (often single-source)",
                    "Excipient supplier concentration",
                    "FDA inspection history (warning letters / import alerts)",
                ],
            },
        ],
    },
    "price_reporting": {
        "description": (
            "Price Reporting Agency (PRA) benchmarks for commodities. Apply "
            "when the question names benchmarks, indices, or specific "
            "commodity-pricing rails."
        ),
        "hints": [
            {
                "surface": "Crude / LNG benchmarks",
                "deeper": [
                    "S&P Global Platts: Dated Brent, JKM (LNG), Dubai",
                    "Argus: Urals, ESPO, Russian Crude, Sour Crude Index",
                    "OPIS: US Rack (gasoline / diesel)",
                    "ICIS: European petrochemicals",
                ],
            },
        ],
    },
}


# ---------------------------------------------------------------------------
# Public lookup
# ---------------------------------------------------------------------------


def available_domains() -> list[str]:
    """Return sorted list of domain keys callable through :func:`lookup`."""

    return sorted(DOMAIN_PRIORS.keys())


def _match_query(query: str, hint: _HintBlock) -> bool:
    q = (query or "").strip().lower()
    if not q:
        return True
    surface = str(hint.get("surface") or "").lower()
    if q in surface or surface in q:
        return True
    # Token-level overlap on multi-word surface labels — cheap and predictable.
    surface_tokens = {t for t in surface.replace("/", " ").replace("(", " ").replace(")", " ").split() if t}
    query_tokens = {t for t in q.replace("/", " ").split() if t}
    return bool(surface_tokens & query_tokens)


def lookup(domain: str, query: str = "") -> dict[str, Any]:
    """Look up dependency-depth hints for a domain and (optional) query.

    Parameters
    ----------
    domain:
        One of :func:`available_domains`. Unknown domain returns an empty
        result with the available list, so the caller can self-correct.
    query:
        Free-form term (entity name, product, surface label). When empty,
        all hints for the domain are returned.

    Returns
    -------
    dict with shape::

        {
          "domain": "semiconductors",
          "description": "...",
          "hints": [
            {"surface": "ASML EUV", "deeper": [...], "note": "..."},
            ...
          ],
          "available_domains": ["semiconductors", ...]   # only when domain unknown
        }
    """

    dom_key = (domain or "").strip().lower()
    block = DOMAIN_PRIORS.get(dom_key)
    if block is None:
        return {
            "domain": dom_key,
            "description": "",
            "hints": [],
            "available_domains": available_domains(),
            "note": "domain not found; pick one from available_domains",
        }
    raw_hints = list(block.get("hints") or [])
    matched = [h for h in raw_hints if _match_query(query, h)]
    return {
        "domain": dom_key,
        "description": str(block.get("description") or ""),
        "hints": matched,
        "query": (query or "").strip(),
        "match_count": len(matched),
        "total_hints_in_domain": len(raw_hints),
    }

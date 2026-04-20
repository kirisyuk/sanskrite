# Materialized artifact payloads (Horizon) — AH shape

All `MaterializedArtifact.Payload` values use schema version **`v: 1`**. Kinds (`SUPPLY_CHAIN_PATH`, `SUPPLY_CHAIN_ASSEMBLY_GRAPH`, governance/regulatory relation graphs, etc.) are unchanged.

Types below use Go field names; JSON keys are the `json` tags on the structs in `backend/internal/horizon/models/materialized_artifact.go`.

## Common rules

- **Nodes** use `class` (`ah.FragmentClass`: `org`, `site`, `product`, `regime`, …) and optional `subtype` (e.g. `deposit` on `site`).
- **Edges** use `relation` (`ah.RelationName`, snake_case: `supplies`, `leg_connects`, `operates`, …).
- **Supply-chain operational semantics** on path edges live in `qualifiers` and `metrics` (e.g. `role: product_flow` vs `logistics_leg`), not in a separate legacy `type` field.
- **Product transforms** (`TRANSFORMS_INTO`, `ASSEMBLES_INTO`, `USED_IN`) are **not** path edges. They appear as `siteTransforms[]` on the relevant **site/org** node in a `SUPPLY_CHAIN_PATH`, or on **actor** (and optionally **product**) nodes in an assembly graph.

---

## `SUPPLY_CHAIN_PATH` / `SERVICE_CHAIN_PATH`

Payload shape: `SupplyChainPathPayload`.

- **`nodes[]`**: `class`, `subtype?`, `ref`, `siteTransforms?`.
- **`edges[]`**: `relation` (`supplies` | `leg_connects` for operational flow), `from`, `to`, `viaProductIndex?`, `productNodeIndex?`, `productRef?`, `supplyLinkCanonicalPath?`, `segmentIndex?`, `carriesProduct`, `qualifiers`, `metrics`.
- **`legs[]`**: operational legs with supplier/customer/product refs, `supplyLinkCanonicalPath`, route overlays, stress, etc.

Example (minimal):

```json
{
  "v": 1,
  "nodes": [
    { "class": "org", "ref": "/org/a" },
    { "class": "product", "ref": "/product/p" },
    { "class": "site", "ref": "/fac/port" }
  ],
  "edges": [
    {
      "relation": "supplies",
      "from": 0,
      "to": 2,
      "viaProductIndex": 1,
      "productRef": "/product/p",
      "supplyLinkCanonicalPath": "/supply-link/global/a/p/c",
      "segmentIndex": 0,
      "carriesProduct": true
    }
  ]
}
```

---

## `SUPPLY_CHAIN_ASSEMBLY_GRAPH`

Payload shape: `SupplyChainAssemblyGraphPayload`.

- **`productNodes[]`**, **`actorNodes[]`**: `class`, `subtype?`, `ref`, `role?`, `siteTransforms?`.
- **`supplyEdges[]`**, **`routeBindings[]`**, **`coverage`**, branch metadata, notes — unchanged at the JSON key level.
- **Removed from the serialized payload**: top-level `transformEdges` and `assemblyEdges`. The same information is represented as `siteTransforms` on nodes (and derived/synthetic lists are built in code when needed).

---

## Relations graphs (`GOVERNANCE_CHAIN`, `REGULATORY_IMPACT_MAP`, `ECOSYSTEM_MAP`, timelines, cascades, …)

Payload shape: `RelationsGraphPayload`.

- **`nodes[]`**: `class`, `subtype?`, `ref`, `name?`.
- **`edges[]`**: `from`, `to`, `relation` (canonical `ah.RelationName`).

---

## Reference implementation

- Structs and validation: `backend/internal/horizon/models/materialized_artifact.go`
- Builders / merge / repair: `backend/internal/horizon/models/materialized_artifact_helpers.go`
- Writers: `backend/internal/horizon/pathbuilder/*.go`, `scout/*`, `supplychainreport/*`, `watch/*`, `internal/http/horizon/*` as applicable.

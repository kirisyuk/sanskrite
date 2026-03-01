# Scout Heuristics Registry

This document catalogs internal heuristics used in the Scout pipeline. When modifying heuristics, update this registry and add a code comment referencing it.

| Heuristic | File | Description | Source (logs/telemetry) |
|-----------|------|-------------|-------------------------|
| `highest-population exact-name match` | `canonical_path_resolver.go:573` | When multiple geo candidates match by name, select the one with highest population | GCP telemetry geo resolution |
| `warmup threshold 5 observations` | `canonical_path_resolver.go:1115` | Canonical path rewrites require 5 observations before becoming stable | Design decision |
| `Firestore facts → Vertex Search → heuristic` | `planner.go:82-85` | Priority order for source planning: facts first, then semantic search, then doc-based heuristic | Design decision |
| `segment-first when possible` | `engine.go:5981` | Market hierarchy: prefer segment-first resolution, heuristic fallback | Design decision |
| `org_deduplicator` heuristics | `org_deduplicator.go:963` | Identity matching and merge rules for organization deduplication | Design decision; intentionally simple and safe |
| `product_deduplicator` | `product_deduplicator.go:136` | Safe heuristic for defense/tech model designations and similar SKUs | Design decision |
| `edge direction semantics` | `edge_materializer.go:132`, `engine_ext.go:5223` | Best-effort enforcement of directional semantics for common relations | Design decision |
| `freshness heuristic` | `engine.go:4413` | Skip research when existing fragment has sufficient metrics | Design decision |
| `gov hierarchy default` | `gov_hierarchy_builder.go:113` | Default heuristics for government hierarchy construction | Design decision |
| `missing_info platform detection` | `missing_info_patterns.go:363` | Best-effort heuristic based on platform names for source detection | Design decision |

## Usage in Code

Add a comment when implementing or modifying a heuristic:

```go
// Heuristic: highest-population exact-name match (see docs/horizon/scout/HEURISTICS_REGISTRY.md)
```

## Maintenance

When adding or changing heuristics:

1. Add an entry to the table above
2. Document the source (logs, telemetry, design decision)
3. Add a code comment referencing this registry
4. For telemetry-backed heuristics, include date or report reference

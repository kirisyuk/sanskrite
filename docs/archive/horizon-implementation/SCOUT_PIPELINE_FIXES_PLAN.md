# Scout Pipeline Critical + High Priority Fixes — Implementation Plan

**Status**: Pre-implementation
**Issues**: 9 total (4 Critical, 5 High)
**Scope**: Surgical fixes, no architectural changes, all fail-open safe

---

## Research Findings (exact locations verified)

### File 1: `backend/internal/taxonomy/runtime_contracts.go`

- **Insert point**: After `builtInAllowedDepositResources` (line 236), before `builtInAllowedLifecycleStages` (line 238)
- **Pattern**: Uses existing `setOf()` helper (line 6)
- **Source data**: Community types from `canonical_rules.go:242`; Market domains = canonical targets from `builtInDomainAliases`

### File 2: `backend/internal/taxonomy/runtime.go`

- **Insert point**: After `IsValidProductStage()` (lines 426–433), before `GetProductStageOrder()` (line 435)
- **Pattern**: Identical to `IsValidProductStage` — normalize, lookup in allowlist
- **Dependency**: `NormalizeMarketDomain()` exists at `market_segments_norm.go:112`

### File 3: `backend/internal/taxonomy/paths.go`

- **`case "community"` (line 292)**: After geo validation (line 301–303), add community type check against `parts[2]`
- **`case "market"` (line 314)**: After geo validation (line 320–322), add market domain check against `parts[2]`
- **Enforcement**: Community = hard error (like product categories). Market = warn-only (soft, `fmt.Printf`)

### File 4: `backend/internal/horizon/scout/agents/research_pipeline.go`

- **Insert point**: Line 57 (after strategy loop ends, before `rawContent` join on line 59)
- **DiscoveredRelation fields**: `RelationType`, `TargetCanonicalPath`, `SourceCanonicalPath`, `Confidence` — all accessible
- **Needs `"strings"` import**: Already imported (line 5)

### File 5: `backend/internal/horizon/scout/agents/research_strategies_impl.go`

- **General strategy (reference pattern)**: Line 1243 uses `buildResearchExistingEntitiesBlock(input.ExistingEntities)`, result placed in `fmt.Sprintf` args at line 1263 alongside `researchPreferExistingEntitiesBlock()`
- **7 domain strategies to fix** (each currently uses only `researchPreferExistingEntitiesBlock()`):
  1. `buildSupplyChainEnrichmentPrompt` — line 1306, Sprintf at ~1375
  2. `buildGovernanceEnrichmentPrompt` — line 1441, Sprintf at ~1510
  3. `buildAssuranceEnrichmentPrompt` — line 1514, Sprintf at ~1566
  4. `buildMarketEnrichmentPrompt` — line 1570, Sprintf at ~1632
  5. `buildStrategicEnrichmentPrompt` — line 1636, Sprintf at ~1682
  6. `buildPolicyImpactEnrichmentPrompt` — line 1686, Sprintf at ~1753
  7. `buildEcosystemEnrichmentPrompt` — line 1756, Sprintf at ~1812
- **Pattern per function**: Add `existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)` at top, insert `%s` placeholder before the `researchPreferExistingEntitiesBlock()` call in the prompt template, add variable to `fmt.Sprintf` args

### File 6: `backend/internal/horizon/scout/agents/critic_pipeline.go`

- **Issue 3 — Merge dedup** (lines 80–95):
  - Current `mergeValidationResults` does naive `append` — duplicates possible
  - Fix: Dedup `Conflicts` by `Field` (keep higher severity), dedup `MissingInfo` by lowercased trimmed string
  - Keep: min confidence, AND for IsValid
- **Issue 7 — Strategic scope** (lines 48–59):
  - Current: only checks `FragmentTypeDecisionPoint/Mechanism` and `/decision/`/`/mechanism/` paths
  - Fix: Also check `hasStrategicRelations(input.Relations)` — any relation whose type maps to `models.SemanticGroupStrategic` via `models.SemanticGroupForEdgeType()` (from `edge_semantic_groups.go`)
  - `SemanticGroupStrategic` mapped types: `CONTROLS_ACCESS`, `IMPOSES_CONDITION`, `GATEKEEPS_DECISION`, `USES_MECHANISM`, `APPLIES_TO`

### File 7: `backend/internal/horizon/scout/edge_materializer.go`

- **Issue 9 — Cross-jurisdiction penalty** (lines 469–475):
  - Current: log-only (`fmt.Printf`)
  - Fix: Add confidence penalty (`*0.7`), policy penalty (`max(policyPenalty, 0.3)`), status = `PolicyStatusPenalized`, reason = `cross_jurisdiction_{src}_{dst}`
  - Existing helpers: `models.PolicyStatusPenalized` (in `policy_qualifiers.go:8`), `policy.MergePolicyStatus()`, `extractJurisdiction()` (in `session_graph.go:399`)
  - Variables `policyPenalty`, `policyStatus`, `policyReason` defined at lines 51–53
- **Issue 1 — Inverted ownership detection**:
  - Insert point: After `normalizeRelationEdge` (line 134), before self-loop check (line 143)
  - New function `detectLikelyInvertedOwnershipEdge()` checks:
    - `rel.Reason` containing "owned by", "invested in by", "controlled by", "subsidiary of", "audited by"
    - `qualifiers["source_role"]` = "owned_entity"/"investee"/"controlled_entity"
    - `qualifiers["target_role"]` = "owner"/"investor"/"controller"
  - Only for OWNS/INVESTS_IN/CONTROLS/ASSURES without explicit `sourceCanonicalPath`
  - If detected: swap `edgeSourceID`/`edgeTargetID`

### File 8: `backend/internal/horizon/scout/supply_chain_parser.go`

- **ParsedChainStats struct** (line 71–82): Add `ConsecutiveSameTypeNodes int` field
- **Insert point in ParseSupplyChains**: After `len(chain.Nodes) < 2` check (line 113), before transport-via loop (line 119)
- **New method**: `validateChainAlternation(nodes, chainIdx)` — checks consecutive nodes for same-type pairs (actor-actor or product-product)
- **ParseWarnings field**: Already exists on `ParsedChainResult` (line 67)

---

## Implementation Order

| Step | Task | Files | Issues |
|------|------|-------|--------|
| 1 | Add allowlists | `runtime_contracts.go` | 5, 6 |
| 2 | Add validators | `runtime.go` | 5, 6 |
| 3 | Validate in paths | `paths.go` | 5, 6 |
| 4 | Relation dedup | `research_pipeline.go` | 2 |
| 5 | ExistingEntities to all strategies | `research_strategies_impl.go` | 2, 4 |
| 6 | Critic merge dedup + strategic scope | `critic_pipeline.go` | 3, 7 |
| 7 | Cross-jurisdiction penalty + inverted ownership | `edge_materializer.go` | 1, 9 |
| 8 | Chain alternation validation | `supply_chain_parser.go` | 8 |
| 9 | Build + test all | — | — |

---

## Exact Code Changes

### 1. `runtime_contracts.go` — Insert after line 236

```go
var builtInAllowedCommunityTypes = setOf(
	"trade", "activist", "professional", "criminal", "religious", "ethnic",
	"interest", "dynasty", "tribe", "representatives", "industry-association", "key-community",
)

var builtInAllowedMarketDomains = setOf(
	"finance", "technology", "healthcare", "energy", "materials",
	"industrial", "consumer", "telecom", "real-estate",
	"professional-services", "public-sector", "utilities",
	"transportation", "agriculture", "defense", "education",
	"media", "entertainment", "hospitality", "retail",
	"automotive", "aerospace", "mining", "oil-gas",
	"pharmaceuticals", "chemicals", "construction",
)
```

### 2. `runtime.go` — Insert after line 433

```go
// IsValidCommunityType checks if a community type slug is in the allowlist.
func IsValidCommunityType(communityType string) bool {
	ct := strings.ToLower(strings.TrimSpace(communityType))
	if ct == "" {
		return false
	}
	_, ok := builtInAllowedCommunityTypes[ct]
	return ok
}

// IsValidMarketDomain checks if a market domain slug is in the allowlist.
func IsValidMarketDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return false
	}
	d = NormalizeMarketDomain(d)
	_, ok := builtInAllowedMarketDomains[d]
	return ok
}
```

### 3. `paths.go` — In `ValidateCanonicalPath()`

**After line 303 (end of community geo validation):**
```go
communityType := strings.ToLower(strings.TrimSpace(parts[2]))
if communityType != "" && !IsValidCommunityType(communityType) {
	return fmt.Errorf("community canonical path has unknown type %q: %s", communityType, path)
}
```

**After line 322 (end of market geo validation):**
```go
domain := strings.ToLower(strings.TrimSpace(parts[2]))
if domain != "" && !IsValidMarketDomain(domain) {
	fmt.Printf("[ValidateCanonicalPath] WARNING: unknown market domain %q in %s\n", domain, path)
}
```

### 4. `research_pipeline.go` — After line 57

```go
allRelations = deduplicateRelations(allRelations)
```

**New function (bottom of file):**
```go
func deduplicateRelations(rels []DiscoveredRelation) []DiscoveredRelation {
	if len(rels) <= 1 {
		return rels
	}
	type relKey struct{ rt, tcp, scp string }
	seen := make(map[relKey]int, len(rels))
	out := make([]DiscoveredRelation, 0, len(rels))
	for _, r := range rels {
		k := relKey{
			rt:  strings.TrimSpace(strings.ToUpper(r.RelationType)),
			tcp: strings.TrimSpace(r.TargetCanonicalPath),
			scp: strings.TrimSpace(r.SourceCanonicalPath),
		}
		if idx, ok := seen[k]; ok {
			if r.Confidence > out[idx].Confidence {
				out[idx] = r
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, r)
	}
	return out
}
```

### 5. `research_strategies_impl.go` — Per each of 7 functions

**Pattern** (example for `buildSupplyChainEnrichmentPrompt`):
- Add `existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)` near top
- Add `%s` placeholder in prompt template before `researchPreferExistingEntitiesBlock()` position
- Add `existingEntitiesBlock` to `fmt.Sprintf` args before `researchPreferExistingEntitiesBlock()`

### 6. `critic_pipeline.go` — Replace `mergeValidationResults`

```go
func mergeValidationResults(a, b *ValidationResult) *ValidationResult {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	a.HasConflicts = a.HasConflicts || b.HasConflicts

	// Dedup conflicts by Field — keep higher severity per field.
	severityOrder := map[string]int{"info": 0, "warning": 1, "error": 2, "critical": 3}
	conflictByField := make(map[string]ValidationConflict, len(a.Conflicts))
	for _, c := range a.Conflicts {
		conflictByField[c.Field] = c
	}
	for _, c := range b.Conflicts {
		if existing, ok := conflictByField[c.Field]; ok {
			if severityOrder[strings.ToLower(c.Severity)] > severityOrder[strings.ToLower(existing.Severity)] {
				conflictByField[c.Field] = c
			}
		} else {
			conflictByField[c.Field] = c
		}
	}
	deduped := make([]ValidationConflict, 0, len(conflictByField))
	for _, c := range conflictByField {
		deduped = append(deduped, c)
	}
	a.Conflicts = deduped

	// Dedup MissingInfo by lowercased trimmed string.
	seenMissing := make(map[string]struct{}, len(a.MissingInfo))
	merged := make([]string, 0, len(a.MissingInfo)+len(b.MissingInfo))
	for _, m := range a.MissingInfo {
		k := strings.ToLower(strings.TrimSpace(m))
		if _, ok := seenMissing[k]; !ok {
			seenMissing[k] = struct{}{}
			merged = append(merged, m)
		}
	}
	for _, m := range b.MissingInfo {
		k := strings.ToLower(strings.TrimSpace(m))
		if _, ok := seenMissing[k]; !ok {
			seenMissing[k] = struct{}{}
			merged = append(merged, m)
		}
	}
	a.MissingInfo = merged

	if b.Confidence < a.Confidence {
		a.Confidence = b.Confidence
	}
	a.IsValid = a.IsValid && b.IsValid
	return a
}
```

**Add strategic scope expansion** (after line 53):
```go
if !isStrategicEntity {
	isStrategicEntity = hasStrategicRelations(input.Relations)
}
```

**New helper:**
```go
func hasStrategicRelations(relations []DiscoveredRelation) bool {
	for _, r := range relations {
		dt := models.DependencyType(strings.ToUpper(strings.TrimSpace(r.RelationType)))
		if models.SemanticGroupForEdgeType(dt) == models.SemanticGroupStrategic {
			return true
		}
	}
	return false
}
```

### 7. `edge_materializer.go`

**Replace lines 469–475 with:**
```go
if isCrossJurisdictionSuspicious(edgeType, resolvedSourcePath, resolvedTargetPath) {
	srcJurisdiction := extractJurisdiction(resolvedSourcePath)
	dstJurisdiction := extractJurisdiction(resolvedTargetPath)
	fmt.Printf("[CrossJurisdiction] PENALIZED edge: %s (%s) -> %s (%s) [%s]\n",
		resolvedSourcePath, srcJurisdiction, resolvedTargetPath, dstJurisdiction, edgeType)
	relConfidence = relConfidence * 0.7
	if 0.3 > policyPenalty {
		policyPenalty = 0.3
	}
	policyStatus = policy.MergePolicyStatus(policyStatus, models.PolicyStatusPenalized)
	policyReason = fmt.Sprintf("cross_jurisdiction_%s_%s", srcJurisdiction, dstJurisdiction)
}
```

**Add `detectLikelyInvertedOwnershipEdge()` function** and call after line 140, before self-loop check (line 143):
```go
if rel.SourceCanonicalPath == "" {
	if detectLikelyInvertedOwnershipEdge(edgeType, rel) {
		edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
		directionChanged = true
	}
}
```

**New function:**
```go
func detectLikelyInvertedOwnershipEdge(edgeType models.DependencyType, rel agents.DiscoveredRelation) bool {
	switch edgeType {
	case models.DependencyTypeOwns, models.DependencyTypeInvestsIn,
		models.DependencyTypeControls, models.DependencyTypeAssures:
		// check
	default:
		return false
	}
	reason := strings.ToLower(strings.TrimSpace(rel.Reason))
	invertedPhrases := []string{"owned by", "invested in by", "controlled by", "subsidiary of", "audited by"}
	for _, phrase := range invertedPhrases {
		if strings.Contains(reason, phrase) {
			return true
		}
	}
	if q := rel.Qualifiers; q != nil {
		sr, _ := q["source_role"].(string)
		tr, _ := q["target_role"].(string)
		sr = strings.ToLower(strings.TrimSpace(sr))
		tr = strings.ToLower(strings.TrimSpace(tr))
		invertedSourceRoles := map[string]bool{"owned_entity": true, "investee": true, "controlled_entity": true}
		invertedTargetRoles := map[string]bool{"owner": true, "investor": true, "controller": true}
		if invertedSourceRoles[sr] || invertedTargetRoles[tr] {
			return true
		}
	}
	return false
}
```

### 8. `supply_chain_parser.go`

**Add to `ParsedChainStats` (after line 81):**
```go
ConsecutiveSameTypeNodes int `json:"consecutiveSameTypeNodes"`
```

**Add call in `ParseSupplyChains` after line 116 (before transport-via loop):**
```go
altWarnings, altCount := p.validateChainAlternation(chain.Nodes, chainIdx)
result.ParseWarnings = append(result.ParseWarnings, altWarnings...)
result.Stats.ConsecutiveSameTypeNodes += altCount
```
(where `chainIdx` comes from changing `for _, chain := range chains` to `for chainIdx, chain := range chains`)

**New method:**
```go
func (p *SupplyChainParser) validateChainAlternation(nodes []agents.SupplyChainNode, chainIdx int) ([]string, int) {
	var warnings []string
	count := 0
	for i := 1; i < len(nodes); i++ {
		prevType := classifyNodeType(nodes[i-1])
		currType := classifyNodeType(nodes[i])
		if prevType == currType && prevType != "unknown" {
			count++
			warnings = append(warnings, fmt.Sprintf(
				"chain[%d]: consecutive same-type nodes at positions %d-%d (%s, %s): both %s",
				chainIdx, i-1, i, strings.TrimSpace(nodes[i-1].CanonicalPath), strings.TrimSpace(nodes[i].CanonicalPath), prevType))
		}
	}
	return warnings, count
}

func classifyNodeType(node agents.SupplyChainNode) string {
	nt := strings.ToLower(strings.TrimSpace(node.Type))
	switch nt {
	case "actor", "org", "government", "gov":
		return "actor"
	case "product", "resource", "material":
		return "product"
	default:
		cp := strings.ToLower(strings.TrimSpace(node.CanonicalPath))
		if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/gov/") {
			return "actor"
		}
		if strings.HasPrefix(cp, "/product/") {
			return "product"
		}
		return "unknown"
	}
}
```

---

## Verification Commands

```bash
# Build check
cd backend && go build ./...

# Affected package tests
cd backend && go test ./internal/taxonomy/... -v
cd backend && go test ./internal/horizon/scout/... -v
cd backend && go test ./internal/horizon/scout/agents/... -v
cd backend && go test ./internal/horizon/models/... -v
```

---

## Verified Cross-References

| Item | Location | Accessibility | Notes |
|------|----------|--------------|-------|
| `extractJurisdiction()` | `session_graph.go:399` (pkg `scout`) | Accessible from `edge_materializer.go` (same pkg) | lowercase = unexported, same-package = OK |
| `classifyNodeType()` | **NEW** — will be defined in `supply_chain_parser.go` | Same pkg `scout` | Takes `agents.SupplyChainNode` (exported struct from `agents` pkg, already imported) |
| `detectLikelyInvertedOwnershipEdge()` | **NEW** — will be defined in `edge_materializer.go` | Same pkg `scout` | Guard `rel.SourceCanonicalPath == ""` prevents double-swap with `normalizeRelationEdge` (line 134) which runs first |

---

## Risk Assessment

| Change | Risk | Mitigation |
|--------|------|------------|
| Community type hard error | Could reject valid LLM output with new types | Allowlist is comprehensive; unknown types = real errors |
| Market domain warn-only | Zero breakage risk | Log-only, no rejection |
| Relation dedup | Could merge different relations with same key | Key includes type + both canonical paths — collision unlikely |
| ExistingEntities in strategies | Extra prompt tokens | Block is empty-string when no entities, zero cost |
| Critic merge dedup | Could lose lower-severity conflicts | Keeps higher severity — correct behavior |
| Cross-jurisdiction penalty | 0.7x confidence may be aggressive | Still creates edge; penalty is recoverable via policy review |
| Inverted ownership swap | Could double-swap if normalizeRelationEdge already handled | Guard: only when `SourceCanonicalPath == ""` (no explicit source); `normalizeRelationEdge` (line 134) runs first |
| Chain alternation | Warn-only | No edge rejection, just ParseWarnings |

# Organization Identity Anchor & Corporate Transitions (rename/rebrand/successor)

Статус: ✅ РЕАЛИЗОВАНО (v2, сверка с кодом 2026-02-13)  
Источник: вынесено из `docs/horizon/implementation/roadmap.md` в отдельный документ для реализации отдельным агентом.

## Сводка

| Раздел | Статус |
|--------|--------|
| 1) Identity anchor (приоритет в dedup) | ✅ Готово |
| 2) NameHistory (темпоральная история имён) | ✅ Готово |
| 3) Redirect-aware path resolution | ✅ Готово |
| 4) Transition edge types | ✅ Готово |
| 5) "Матрица не в пустоту" | ✅ Готово |

## Проблема

Система не поддерживает корпоративные переходы (Tinkoff Bank -> T-Bank, смена собственности + ребрендинг). Path/slug и текущее имя перегружены ролью идентичности, что ломается при rename/rebrand.

## Текущее состояние (подтверждено кодом)

- `Identifiers` — `map[string]string`, обогащён identity anchors (~55 ключей).
- `NameHistory []NameHistoryEntry` — темпоральная история имён с Kind (short/legal/brand/aka) и ValidFrom/ValidTo.
- Dedup: identity anchor first (`findByIdentityAnchor` → `FindByIdentifierAndType`), затем name-based, включая cross-match current↔historical names.
- `canonical_path_redirects` — Firestore-backed с stability tracking, `applyRedirects` (Step 0, до 4 hops).
- 6 transition edge types + direction policy + canonicalization matrix.
- "Матрица не в пустоту": fallback `transition_unclassified → INFLUENCES + penalized`.

## План работ

### 1) Identity anchor (приоритет в dedup) — ✅ ГОТОВО

- `[ЕСТЬ]` `orgIdentityAnchorPriority` (`org_deduplicator.go:42-99`) — 55+ ключей:
  - Международные: `wikidataId`, `lei`, `duns`
  - Национальные: `ogrn`, `inn`, `crn`, `vat`, `siren`, `siret`, `uid`, `ust_idnr`, `cvr`, `ico`, `partita_iva`, `nif`, `cif`, `nip`, `krs`, `cui`, `matricule`, `bin`, `edrpou`, `uic`, `idno`, `voen`, `ein`, `abn`, `acn`, `usc`, `hn`, `gstin`, `cin`, `pan`, `uen`, `brn`, `cnpj`, `cuit`, `rfc`, `trn`, `tin`, `vkn`, `npwp`, `ssm`, `mst`, `pin`, `ruc`, `rut`, `nit`, `cedula_juridica`, `orgnr`, `y_tunnus`, `rc`, `rn`, `cc`, `id`
- `[ЕСТЬ]` `findByIdentityAnchor` — Strategy 1 в Tier 1 dedup, перед `legalName`/`label`/`shortName`.
- `[ЕСТЬ]` `FindByIdentifierAndType` (`repo/fragments.go`) — Firestore query по `identifiers.{key}` + type filter.
- `[ЕСТЬ]` Нормализация значений при поиске: upper/lower/cleaned variants (`normalizeIdentifierLookupValues`).

### 2) NameHistory (темпоральная история имён) — ✅ ГОТОВО

- `[ЕСТЬ]` `NameHistoryEntry` struct: `Name`, `Kind`, `ValidFrom`, `ValidTo` (`fragment.go:188-195`).
- `[ЕСТЬ]` `NameHistory []NameHistoryEntry` на `HorizonFragment` (`fragment.go:136-137`).
- `[ЕСТЬ]` Kind нормализация: `short | legal | brand | aka` (`fragment.go:412-418`).
- `[ЕСТЬ]` Используется в dedup — Strategy 4.5 (`org_deduplicator.go`): cross-match current ↔ historical names.
- `[ЕСТЬ]` Заполнение из Wikidata (`enrich/wikidata_conservative.go: populateNameHistoryFromWikidata`):
  - P5768 (former official name) → Kind `legal`
  - P1449 (nickname) → Kind `aka`
  - Wikidata aliases (English) → Kind `aka`
  - Дедупликация: не добавляет имена, совпадающие с текущими labels/identifiers.
- `[ЕСТЬ]` Заполнение из AI-агентов: research_agent prompt содержит секцию "Name History (ORG/PERSON)" с guidance по `nameHistory` в output JSON. `mergeFragment` мержит `NameHistory` из AI-результата.
- `[ЕСТЬ]` Vector search: `GenerateVectorText()` включает NameHistory как "Former names: ..." для семантического поиска по историческим именам.

### 3) Redirect-aware path resolution — ✅ ГОТОВО

- `[ЕСТЬ]` `canonical_path_redirects` — Firestore collection (`repo/canonical_path_redirects.go`) с stability tracking: `StableTarget`, `StableUntil`, `Variability`, `SeenCount`.
- `[ЕСТЬ]` `applyRedirects()` — Step 0 в `CanonicalPathResolver`, до 4 hops, cycle detection через `seen` map.
- `[ЕСТЬ]` Redirect recording: `RecordObservation` с warmup и stability tracking.
- Для Tinkoff: `/org/ru/tinkoff-bank` → `/org/ru/t-bank` — поддерживается автоматически при записи redirect.

### 4) Transition edge types (для случаев смены юрлица) — ✅ ГОТОВО

Все 6 типов реализованы в `DependencyType` (`models/dependency_edge.go:161-174`):
- `RENAMED_TO` — legal name rename, та же сущность.
- `REBRANDED_TO` — brand rename, та же сущность.
- `LEGAL_SUCCESSOR_OF` — новое юрлицо-наследник старого.
- `MERGED_INTO` — слияние.
- `SPUN_OFF_FROM` — выделение.
- `ACQUIRED_BY` — M&A событие.

Интеграция:
- `[ЕСТЬ]` `IsTransitionEdgeType()` helper (`dependency_edge.go:175-188`).
- `[ЕСТЬ]` Direction policy (`edgesemantics/direction_policy.go:334-365`): все ORG→ORG.
- `[ЕСТЬ]` Canonicalization matrix (`edge_canonicalization.go:69-76`): ORG→ORG сохраняет тип, non-ORG → `INFLUENCES`.
- `[ЕСТЬ]` В промптах агентов: `intent_decomposer`, `research_agent`, `scout_agent`.
- `[ЕСТЬ]` `NormalizeRelationType` synonyms (`taxonomy/runtime.go`): `RENAMED→RENAMED_TO`, etc.

### 5) Правило "матрица не в пустоту" — ✅ ГОТОВО

Реализовано в 3 местах с единым паттерном:
- `repo/dependency_edges.go:187-214` — при персистенции edge
- `scout/edge_materializer.go:263-282` — при материализации
- `scout/engine_ext.go:4228-4245` — в processRelationsInternal

Паттерн:
1. `IsTransitionLikeRelationType(rawType) && !IsTransitionEdgeType(edgeType) && ORG→ORG`
2. → Retag to `INFLUENCES`
3. → `policyStatus = penalized`, `policyReason = "transition_unclassified"`
4. → Edge НИКОГДА не удаляется и не обнуляется.

## Применение к Tinkoff -> T-Bank

- Если то же юрлицо (ребрендинг):
  - один `ORGANIZATION`, обновить `shortName`/`legalName`, добавить `nameHistory`, redirect `/org/ru/tinkoff-bank` -> `/org/ru/t-bank`.
  - dedup: по identity anchor (`wikidataId`/`ogrn`/`inn`), затем по `nameHistory` + current names.
- Если юрлицо изменилось:
  - два `ORGANIZATION` + `LEGAL_SUCCESSOR_OF`, ownership/restructure рёбра с `ValidFrom`/`ValidTo`.

## Артефакты

- `backend/internal/horizon/scout/org_deduplicator.go` — identity anchor priority, NameHistory dedup
- `backend/internal/horizon/models/fragment.go` — `NameHistory`, `NameHistoryEntry`, `GenerateVectorText`
- `backend/internal/horizon/models/dependency_edge.go` — transition types, `IsTransitionEdgeType`
- `backend/internal/horizon/enrich/wikidata_conservative.go` — `populateNameHistoryFromWikidata`
- `backend/internal/http/wikidata_client.go` — aliases parsing, P5768/P1449 extraction
- `backend/internal/ontology/entity_enricher.go` — `WikidataEntityData.Aliases`
- `backend/internal/horizon/scout/agents/research_agent.go` — nameHistory prompt + merge
- `backend/internal/horizon/repo/canonical_path_redirects.go` — redirect storage
- `backend/internal/horizon/scout/canonical_path_resolver.go` — `applyRedirects`
- `backend/internal/horizon/edgesemantics/direction_policy.go` — transition direction rules
- `backend/internal/horizon/models/edge_canonicalization.go` — transition canonicalization
- `backend/internal/horizon/scout/edge_materializer.go` — transition_unclassified fallback
- `backend/internal/horizon/repo/dependency_edges.go` — transition_unclassified fallback

## Связанный документ

- `docs/horizon/implementation/BIG_BANG_EDGE_POLICY_REMEDIATION_PLAN.md` (policyStatus/penalty/quarantine контракт)

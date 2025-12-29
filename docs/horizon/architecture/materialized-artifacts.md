# Materialized Artifacts (Overlays) in Horizon

## Зачем нужен отдельный базовый тип

Horizon-граф (fragments + dependency edges) — это **модель мира** и “каркас” реальности (узлы/отношения).
Но многие практические ответы строятся не прямым чтением графа, а через **слои/оверлеи**:
- materialized supply chain path (упорядоченный путь/подграф, собранный алгоритмом)
- political / influence slice (контекстные влияния, санкции, рычаги, условия)
- любые иные “derived views” (risk exposure paths, compliance paths, logistics corridors…)

Чтобы не смешивать “мир” и “вычисленное представление”, вводим отдельный базовый тип:
**Materialized Artifact** (также можно называть **Graph Overlay** или **Materialized Fragment**).

## Терминология

- **World Graph**: `HorizonFragment` + `DependencyEdge` (tenantId="global" для каркаса мира).
- **Overlay / Materialized Artifact**: отдельная сущность, которая хранит **упорядоченную структуру** (path / subgraph / slice)
  и ссылки на модельные метрики/observations, но не пытается стать “новым миром”.

## Базовый контракт: MaterializedArtifact

MaterializedArtifact — tenant-scoped (и может быть `tenantId="global"` как “глобальная assumption-модель”).

```go
// MaterializedArtifact is a persisted, algorithmically built overlay over the world graph.
// It is NOT a replacement for HorizonFragment; it is a derived view that references the world graph.
type MaterializedArtifact struct {
    ID        string `json:"id" firestore:"id"`
    TenantID  string `json:"tenantId" firestore:"tenantId"` // "global" or tenant
    ProjectID string `json:"projectId,omitempty" firestore:"projectId,omitempty"`
    LensID    string `json:"lensId,omitempty" firestore:"lensId,omitempty"`

    // Kind defines schema of Payload.
    Kind MaterializedKind `json:"kind" firestore:"kind"`

    // A stable key for idempotent upserts & dedup (like exposure keys).
    CanonicalKey string `json:"canonicalKey" firestore:"canonicalKey"`

    // References to model-layer metric observations (stored outside ontology_facts).
    MetricObservationIDs []string `json:"metricObservationIds,omitempty" firestore:"metricObservationIds,omitempty"`

    // Evidence links (ontology_facts) that justify this overlay (optional).
    EvidenceFactIDs []string `json:"evidenceFactIds,omitempty" firestore:"evidenceFactIds,omitempty"`

    // Payload is a versioned, kind-specific JSON object.
    Payload map[string]interface{} `json:"payload,omitempty" firestore:"payload,omitempty"`

    CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
    UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}
```

### MaterializedKind (первый enum — supply chain)

```go
type MaterializedKind string

const (
    // Supply chain: path/slice built from world graph + tenant/global overlays.
    MaterializedKindSupplyChainPath MaterializedKind = "SUPPLY_CHAIN_PATH"

    // Future:
    // MaterializedKindInfluenceSlice  MaterializedKind = "INFLUENCE_SLICE"
    // MaterializedKindExposurePath   MaterializedKind = "EXPOSURE_PATH"
)
```

## Kind: SUPPLY_CHAIN_PATH (первый)

Цель: хранить materialized “фрагмент/путь” цепочки поставок как **упорядоченный массив** ссылок на сущности мира.

### Payload контракт (v1)

```json
{
  "v": 1,
  "nodes": [
    {"type": "org", "ref": "/org/albemarle"},
    {"type": "product", "ref": "/product/raw/chemical/refining/lithium-carbonate"},
    {"type": "org", "ref": "/org/catl"},
    {"type": "product", "ref": "/product/component/electronic/manufacturing/battery-cell"},
    {"type": "org", "ref": "/org/bmw"}
  ],
  "edges": [
    {"type": "SUPPLY_LINK", "from": 0, "to": 2, "viaProductIndex": 1},
    {"type": "SUPPLY_LINK", "from": 2, "to": 4, "viaProductIndex": 3},
    {"type": "ONTOLOGY", "relation": "TRANSFORMS_INTO", "fromProductIndex": 1, "toProductIndex": 3, "mode": "optional"}
  ],
  "notes": {
    "buildMode": "tenant_first_global_fallback",
    "maxDepth": 6
  }
}
```

### Важные правила (v1)

- **Путь может заканчиваться на org/fac без конечного продукта** (например, “шахта → литий → BMW”).
- Связь product(i)→product(i+1) через `TRANSFORMS_INTO` — **опциональная** (filter/rank/strict-mode).
- **Multi-tenancy precedence на уровне сегмента**: tenant overlays/metrics перекрывают global; global используется как fallback для связности.

## Метрики и evidence: разделение ответственности

- `ontology_facts` — evidence pipeline (извлечение/нормализация фактов).
- Метрики supply chain (включая units, time windows, modality, confidence, источники) живут в отдельной сущности наблюдений
  и **ссылаются** на `ontology_facts` как proof points.
- MaterializedArtifact хранит ссылки на метрики (observation IDs) и (опционально) на evidence (fact IDs).

## Почему это лучше, чем “новые узлы мира”

- Оверлей — это **слой**, который можно включать/выключать/пересобирать без изменения world graph.
- Это позволяет строить разные “срезы” (supply chain vs political vs risk exposure) поверх одного каркаса.
- Это упрощает UX: пользователь видит “путь/подграф” как объект анализа, а не тонны низкоуровневых ребер.

## См. также

- [Supply Chain & Influence Model](supply-chain-influence-model.md)
- `backend/internal/horizon/models/exposure_path.go` (аналогичный подход: materialized NodeIDs/EdgeIDs для path artifacts)



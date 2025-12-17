# Семантический поиск сущностей

## Обзор

Для гибкого семантического поиска HorizonFragment и Entity в системе используется подход с явным полем `vectorText`, которое содержит человекочитаемый набор атрибутов сущности. Это поле векторизуется и используется для семантического поиска через Vertex AI или Firestore Vector Search.

## Принцип работы

### Поле vectorText

`vectorText` — это специальное текстовое поле, которое:
- Содержит все релевантные атрибуты сущности в человекочитаемом формате
- Дублирует информацию в разных форматах для лучшего поиска
- Используется исключительно для векторизации и семантического поиска
- Не используется для отображения пользователю (для этого есть структурированные поля)

### Векторизация

- При создании/обновлении HorizonFragment или Entity автоматически генерируется `vectorText`
- `vectorText` векторизуется через Vertex AI Embeddings API или OpenAI Embeddings
- Вектор сохраняется в поле `vectorEmbedding` в Firestore
- Для поиска используется векторное сравнение (cosine similarity)

## Структура vectorText для HorizonFragment

### Формат

`vectorText` формируется как многострочный текст, содержащий все релевантные атрибуты:

```
{canonicalPath}
{canonicalName}
{aliases}
{entityPath}
{sectorPath}
{location}
{description}
{labels}
{tags}
{identifiers}
{metrics}
{kbRefs}
```

### Правила формирования

1. **Канонический путь и имя**:
   ```
   /org/tsmc/fab/21
   TSMC Fab 21
   Taiwan Semiconductor Manufacturing Company Fab 21
   ```

2. **Иерархические пути** (дублируются в разных форматах):
   ```
   /Infrastructure/Manufacturing/Factory
   Infrastructure Manufacturing Factory
   Factory Manufacturing Infrastructure
   ```

3. **Отраслевой путь**:
   ```
   /Technology/Semiconductors/Manufacturing
   Technology Semiconductors Manufacturing
   Semiconductor Manufacturing Technology
   ```

4. **Локация** (если есть):
   ```
   Hsinchu, Taiwan
   Taiwan Hsinchu
   TW Hsinchu
   ISO3166-1: TW
   ISO3166-2: TW-HSQ
   ```

5. **Метрики** (название и значение):
   ```
   operational_health: 0.85
   operational health 85%
   capacity_utilization: 0.92
   capacity utilization 92%
   water_intensity: 0.95
   water intensity high
   ```

6. **Идентификаторы**:
   ```
   ISIN: US1234567890
   Ticker: TSM
   LEI: 5493000KJTIIGC8Y1R12
   ```

7. **Теги и метки**:
   ```
   Tags: semiconductor critical-infrastructure
   Label EN: TSMC Fab 21
   Label RU: TSMC Фабрика 21
   ```

8. **KBRefs** (если есть):
   ```
   Wikidata: Q123456
   ```

### Пример полного vectorText

```
/org/tsmc/fab/21
TSMC Fab 21
Taiwan Semiconductor Manufacturing Company Fab 21
TSMC Factory 21

/Infrastructure/Manufacturing/Factory
Infrastructure Manufacturing Factory
Factory Manufacturing Infrastructure
Manufacturing Factory

/Technology/Semiconductors/Manufacturing
Technology Semiconductors Manufacturing
Semiconductor Manufacturing Technology
Semiconductors Manufacturing

Hsinchu, Taiwan
Taiwan Hsinchu
TW Hsinchu
ISO3166-1: TW
ISO3166-2: TW-HSQ

operational_health: 0.85
operational health 85%
capacity_utilization: 0.92
capacity utilization 92%
water_intensity: 0.95
water intensity high
energy_intensity: 0.88
energy intensity high

ISIN: US1234567890
Ticker: TSM
LEI: 5493000KJTIIGC8Y1R12

Tags: semiconductor critical-infrastructure
Label EN: TSMC Fab 21
Label RU: TSMC Фабрика 21

Wikidata: Q123456

Taiwan Semiconductor Manufacturing Company semiconductor factory manufacturing chips processors
```

## Структура vectorText для Entity

Для Entity `vectorText` формируется аналогично, но без метрик:

```
{canonicalName}
{aliases}
{type}
{location}
{description}
{labels}
{tags}
{identifiers}
{kbRefs}
```

### Пример для Entity

```
Apple Inc.
Apple
AAPL
Apple Computer Inc.

Organization
Corporation
Public Company

Cupertino, California
California Cupertino
US Cupertino
ISO3166-1: US
ISO3166-2: US-CA

Ticker: AAPL
ISIN: US0378331005
LEI: 5493000KJTIIGC8Y1R12

Tags: technology consumer-electronics
Label EN: Apple Inc.
Label RU: Apple Inc.

Wikidata: Q312

Apple technology consumer electronics iPhone iPad Mac computers software services
```

## Реализация

### Добавление полей в модели

```go
type HorizonFragment struct {
    // ... существующие поля
    
    // Семантический поиск
    VectorText      string    `json:"vectorText,omitempty" firestore:"vectorText,omitempty"`
    VectorEmbedding []float64 `json:"vectorEmbedding,omitempty" firestore:"vectorEmbedding,omitempty"`
    VectorModel     string    `json:"vectorModel,omitempty" firestore:"vectorModel,omitempty"` // text-embedding-3-small
    VectorVersion   string    `json:"vectorVersion,omitempty" firestore:"vectorVersion,omitempty"` // v1
}

type Entity struct {
    // ... существующие поля
    
    // Семантический поиск
    VectorText      string    `json:"vectorText,omitempty" firestore:"vectorText,omitempty"`
    VectorEmbedding []float64 `json:"vectorEmbedding,omitempty" firestore:"vectorEmbedding,omitempty"`
    VectorModel     string    `json:"vectorModel,omitempty" firestore:"vectorModel,omitempty"`
    VectorVersion   string    `json:"vectorVersion,omitempty" firestore:"vectorVersion,omitempty"`
}
```

### Генерация vectorText

```go
// GenerateVectorText генерирует vectorText для HorizonFragment
func GenerateVectorText(fragment *HorizonFragment, entity *Entity) string {
    var parts []string
    
    // Канонический путь
    parts = append(parts, fragment.CanonicalPath)
    
    // Имя (из Entity или canonicalPath)
    if entity != nil && entity.CanonicalName != "" {
        parts = append(parts, entity.CanonicalName)
        // Вариации имени
        parts = append(parts, generateNameVariations(entity.CanonicalName)...)
    }
    
    // Алиасы
    if entity != nil {
        for _, alias := range entity.Aliases {
            parts = append(parts, alias)
        }
    }
    
    // Иерархические пути (дублируются)
    if fragment.EntityPath != "" {
        parts = append(parts, fragment.EntityPath)
        parts = append(parts, formatPathAsWords(fragment.EntityPath)...)
    }
    
    if fragment.SectorPath != "" {
        parts = append(parts, fragment.SectorPath)
        parts = append(parts, formatPathAsWords(fragment.SectorPath)...)
    }
    
    // Локация
    if fragment.HomeLocation != nil {
        parts = append(parts, formatLocation(fragment.HomeLocation)...)
    }
    
    // Метрики
    for name, metric := range fragment.State {
        parts = append(parts, formatMetric(name, metric)...)
    }
    
    // Идентификаторы
    if fragment.Identifiers != nil {
        for scheme, value := range fragment.Identifiers {
            parts = append(parts, fmt.Sprintf("%s: %s", scheme, value))
        }
    }
    
    // Теги и метки
    if len(fragment.Tags) > 0 {
        parts = append(parts, "Tags: "+strings.Join(fragment.Tags, " "))
    }
    
    if fragment.Labels != nil {
        for lang, label := range fragment.Labels {
            parts = append(parts, fmt.Sprintf("Label %s: %s", lang, label))
        }
    }
    
    // KBRefs
    if fragment.KBRefs != nil {
        for _, ref := range fragment.KBRefs {
            parts = append(parts, fmt.Sprintf("%s: %s", ref.KB, ref.ExternalID))
        }
    }
    
    // Описание
    if fragment.Description != "" {
        parts = append(parts, fragment.Description)
    }
    
    return strings.Join(parts, "\n")
}

// formatPathAsWords преобразует /Path/To/Type в "Path To Type" и "Type To Path"
func formatPathAsWords(path string) []string {
    parts := strings.Split(strings.Trim(path, "/"), "/")
    if len(parts) == 0 {
        return nil
    }
    
    var variations []string
    // Прямой порядок
    variations = append(variations, strings.Join(parts, " "))
    
    // Обратный порядок
    reversed := make([]string, len(parts))
    for i := len(parts) - 1; i >= 0; i-- {
        reversed[len(parts)-1-i] = parts[i]
    }
    variations = append(variations, strings.Join(reversed, " "))
    
    return variations
}

// formatMetric форматирует метрику для поиска
func formatMetric(name string, metric MetricValue) []string {
    var parts []string
    
    // Техническое имя
    parts = append(parts, fmt.Sprintf("%s: %.2f", name, metric.Value))
    
    // Человекочитаемое имя
    humanName := strings.ReplaceAll(name, "_", " ")
    parts = append(parts, fmt.Sprintf("%s %.0f%%", humanName, metric.Value*100))
    
    // Качественное описание
    if metric.Value > 0.8 {
        parts = append(parts, fmt.Sprintf("%s high", humanName))
    } else if metric.Value < 0.2 {
        parts = append(parts, fmt.Sprintf("%s low", humanName))
    }
    
    return parts
}

// formatLocation форматирует локацию для поиска
func formatLocation(loc *Location) []string {
    var parts []string
    
    if loc.Name != "" {
        parts = append(parts, loc.Name)
        // Разделяем на компоненты
        components := strings.Split(loc.Name, ",")
        for _, comp := range components {
            parts = append(parts, strings.TrimSpace(comp))
        }
    }
    
    if loc.ISO3166_1 != nil {
        parts = append(parts, fmt.Sprintf("ISO3166-1: %s", *loc.ISO3166_1))
    }
    
    if loc.ISO3166_2 != nil {
        parts = append(parts, fmt.Sprintf("ISO3166-2: %s", *loc.ISO3166_2))
    }
    
    return parts
}
```

### Векторизация

```go
// UpdateVectorEmbedding обновляет векторное представление HorizonFragment
func UpdateVectorEmbedding(ctx context.Context, fragment *HorizonFragment, emb ai.EmbeddingProvider) error {
    // Генерируем vectorText
    vectorText := GenerateVectorText(fragment, nil) // Entity загружается отдельно если нужно
    fragment.VectorText = vectorText
    
    // Векторизуем
    embeddings, err := emb.EmbedTexts(ctx, []string{vectorText})
    if err != nil {
        return fmt.Errorf("failed to embed vectorText: %w", err)
    }
    
    if len(embeddings) == 0 {
        return fmt.Errorf("no embeddings returned")
    }
    
    fragment.VectorEmbedding = embeddings[0]
    fragment.VectorModel = emb.ModelID()
    fragment.VectorVersion = "v1"
    
    return nil
}
```

### Семантический поиск

```go
// SearchHorizonFragments выполняет семантический поиск HorizonFragment
func SearchHorizonFragments(
    ctx context.Context,
    query string,
    tenantID string,
    topK int,
    emb ai.EmbeddingProvider,
    repo *HorizonFragmentRepository,
) ([]*HorizonFragment, error) {
    // Векторизуем запрос
    queryEmbeddings, err := emb.EmbedTexts(ctx, []string{query})
    if err != nil {
        return nil, fmt.Errorf("failed to embed query: %w", err)
    }
    
    if len(queryEmbeddings) == 0 {
        return nil, fmt.Errorf("no query embedding returned")
    }
    
    queryVector := queryEmbeddings[0]
    
    // Используем Firestore Vector Search
    col := repo.client.Collection("horizon_fragments")
    q := col.Where("tenantId", "==", tenantID)
    
    vectorQuery := q.FindNearest("vectorEmbedding", queryVector, topK, firestore.DistanceMeasureCosine, nil)
    docs, err := vectorQuery.Documents(ctx).GetAll()
    if err != nil {
        return nil, fmt.Errorf("vector search failed: %w", err)
    }
    
    var fragments []*HorizonFragment
    for _, doc := range docs {
        var fragment HorizonFragment
        if err := doc.DataTo(&fragment); err != nil {
            continue
        }
        fragments = append(fragments, &fragment)
    }
    
    return fragments, nil
}
```

## Индексы Firestore

Для векторного поиска требуется composite index с векторным полем:

```json
{
  "indexes": [
    {
      "collectionGroup": "horizon_fragments",
      "queryScope": "COLLECTION",
      "fields": [
        {
          "fieldPath": "tenantId",
          "order": "ASCENDING"
        },
        {
          "fieldPath": "vectorEmbedding",
          "vectorConfig": {
            "dimension": 1536,
            "flat": {}
          }
        }
      ]
    },
    {
      "collectionGroup": "entities",
      "queryScope": "COLLECTION",
      "fields": [
        {
          "fieldPath": "tenantId",
          "order": "ASCENDING"
        },
        {
          "fieldPath": "vectorEmbedding",
          "vectorConfig": {
            "dimension": 1536,
            "flat": {}
          }
        }
      ]
    }
  ]
}
```

## Обновление vectorText

`vectorText` должен обновляться автоматически при:
- Создании HorizonFragment
- Обновлении HorizonFragment (изменение state, метрик, локации)
- Связывании с Entity (для получения canonicalName, aliases)
- Обновлении Entity (если HorizonFragment связан)

## Использование в Scout Agent

Scout Agent может использовать семантический поиск для:
- Поиска существующих HorizonFragment перед созданием новых
- Разрешения дубликатов
- Поиска связанных сущностей

## См. также

- [Модель данных](data-model.md)
- [Firestore Vector Search](https://cloud.google.com/firestore/docs/vector-search)
- [Vertex AI Embeddings](https://cloud.google.com/vertex-ai/docs/generative-ai/embeddings)


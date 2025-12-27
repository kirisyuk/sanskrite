# Анализ дублирования работы при создании линзы

## Проблемы, выявленные из лога и кода

### 1. **Планирование полагается на Vertex AI Search, который не синхронизирован**

**Проблема:**
- В `planner.go:92-138` планирование использует Vertex AI Search для поиска фактов (`ontology_facts`)
- Если Vertex AI Search не находит фактов (или они еще не проиндексированы), возвращается пустой список источников `[]*models.DataSource{}`
- Это сигнализирует ScoutEngine запустить Research, даже если фрагмент уже существует в системе

**Код:**
```go
// planner.go:132-134
if len(resp.Results) == 0 || len(sourceIDs) == 0 {
    return []*models.DataSource{}  // Пустой список = запустить Research
}
```

**Последствия:**
- Новые факты, созданные Research, не сразу доступны в Vertex AI Search (нужна индексация)
- Когда создается новый scouting job для связанного entity, планирование снова не находит источников
- Это приводит к повторному запуску Research для того же entity

### 2. **Проверка существующего фрагмента происходит ПОСЛЕ планирования**

**Проблема:**
- В `engine.go:513-643` проверяется freshness существующего фрагмента
- Но планирование источников (строка 646-760) происходит ДО проверки, есть ли у фрагмента достаточно контента
- Если планирование не нашло источников, система запускает Research, даже если фрагмент уже имеет данные

**Последовательность:**
1. Проверка freshness (строка 513) - может пропустить, если фрагмент не свежий
2. Планирование источников (строка 646) - использует Vertex AI Search
3. Если источников нет → Research запускается (строка 802)

**Проблема:** Не проверяется, есть ли у существующего фрагмента достаточно данных (State, Description, Relations) перед планированием.

### 3. **Проверка контента не учитывает структуру существующего фрагмента**

**Проблема:**
- В `engine.go:802` проверяется только `aggregatedContent == ""`
- `aggregatedContent` формируется только из источников, найденных в планировании
- Если планирование не нашло источников, `aggregatedContent` будет пустым, даже если фрагмент уже существует и имеет данные

**Код:**
```go
// engine.go:802-804
if aggregatedContent == "" {
    fmt.Printf("[ScoutEngine] No internal content for %s. Initiating full research.\n", job.TargetCanonicalPath)
    e.logLine(ctx, job, "info", "> no internal content; initiating full research")
    // Запускается Research, даже если фрагмент уже существует
}
```

**Что не проверяется:**
- Есть ли у существующего фрагмента State с метриками?
- Есть ли Description?
- Есть ли Relations (edges)?
- Достаточно ли данных для работы без Research?

### 4. **Новые факты не сразу доступны для планирования**

**Проблема:**
- Когда Research находит новые связи (`engine.go:978-981`), они сохраняются как edges
- Но эти edges не сразу индексируются в Vertex AI Search
- Когда создается новый scouting job для связанного entity (`engine_ext.go:372-376`), планирование снова не находит источников

**Цикл дублирования:**
1. Research находит новые связи → создаются edges
2. Создаются новые scouting jobs для связанных entities
3. Новые jobs проходят планирование → Vertex AI Search не находит фактов (еще не проиндексированы)
4. Запускается Research снова → находит те же связи → создаются дубликаты edges
5. Повторяется для каждого связанного entity

### 5. **Не используется существующая структура фрагмента**

**Проблема:**
- Система не проверяет, есть ли у существующего фрагмента достаточно структурированных данных
- Не используется информация из самого фрагмента (State, Relations, Description) для принятия решения о необходимости Research

**Что можно использовать:**
- `fragment.State` - метрики фрагмента
- `fragment.Description` - описание
- Edges, связанные с фрагментом (через `edgesRepo`)
- Существующие Relations

## Рекомендации по исправлению

### 1. **Проверять существующий фрагмент ПЕРЕД планированием**

**Решение:**
- Переместить проверку существующего фрагмента на более ранний этап
- Проверять не только freshness, но и наличие достаточного контента
- Если фрагмент существует и имеет достаточно данных, пропускать планирование и Research

**Код для проверки:**
```go
// Проверка достаточности данных существующего фрагмента
hasEnoughData := false
if existingFragment != nil {
    hasEnoughData = len(existingFragment.State) > 0 || 
                    existingFragment.Description != "" ||
                    existingFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence
}
```

### 2. **Использовать Firestore для проверки фактов вместо Vertex AI Search**

**Решение:**
- В планировании сначала проверять Firestore напрямую (`factsRepo.ListUpdatedSinceByCanonicalPath`)
- Использовать Vertex AI Search только как fallback или для семантического поиска
- Это гарантирует, что новые факты сразу доступны для планирования

**Код:**
```go
// Сначала проверяем Firestore напрямую
facts, err := p.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tenantID, targetCanonicalPath, since, 200)
if err == nil && len(facts) > 0 {
    // Используем факты из Firestore
    return p.materializeSources(ctx, allSources, extractSourceIDs(facts), strategy)
}
// Только потом используем Vertex AI Search как fallback
```

### 3. **Проверять существующие edges перед созданием новых scouting jobs**

**Решение:**
- В `processDiscoveredRelations` проверять, есть ли уже edges для связанных entities
- Если edge уже существует и фрагмент verified, не создавать новый scouting job

**Код:**
```go
// Проверка существующих edges перед созданием scouting job
existingEdge, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceFragment.ID, targetID, edgeType)
if err == nil && existingEdge != nil {
    // Edge уже существует, проверяем фрагмент
    if targetFragment != nil && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
        // Фрагмент уже verified, не создаем новый scouting job
        continue
    }
}
```

### 4. **Использовать структуру существующего фрагмента для принятия решений**

**Решение:**
- Проверять наличие данных в существующем фрагменте перед запуском Research
- Использовать информацию из фрагмента для обогащения, а не только внешние источники

**Код:**
```go
// Проверка достаточности данных перед Research
if existingFragment != nil {
    hasState := len(existingFragment.State) > 0
    hasDescription := existingFragment.Description != ""
    hasRelations := // проверка через edgesRepo
    
    if hasState || hasDescription || hasRelations {
        // Используем существующие данные, не запускаем Research
        aggregatedContent = existingFragment.GenerateVectorText()
    }
}
```

### 5. **Добавить дедупликацию scouting jobs**

**Решение:**
- Проверять, есть ли уже pending/executing scouting job для того же canonicalPath
- Не создавать новый job, если уже есть активный

**Код:**
```go
// Проверка существующих активных jobs
existingJobs, err := e.scoutingJobsRepo.ListByCanonicalPathAndStatus(ctx, targetPath, []string{"pending", "executing"})
if err == nil && len(existingJobs) > 0 {
    // Уже есть активный job, не создаем новый
    continue
}
```

## Выводы

Основные причины дублирования работы:

1. **Vertex AI Search не синхронизирован** - новые факты не сразу доступны для планирования
2. **Планирование происходит до проверки существующего фрагмента** - не используется информация, которая уже есть в системе
3. **Не проверяется достаточность данных существующего фрагмента** - система запускает Research даже когда данные уже есть
4. **Новые edges не проверяются перед созданием scouting jobs** - создаются дубликаты jobs для уже обработанных entities
5. **Не используется структура существующего фрагмента** - игнорируется информация из State, Relations, Description

## Приоритет исправлений

1. **Высокий:** Проверка существующего фрагмента перед планированием
2. **Высокий:** Использование Firestore для проверки фактов вместо Vertex AI Search
3. **Средний:** Проверка существующих edges перед созданием scouting jobs
4. **Средний:** Использование структуры существующего фрагмента
5. **Низкий:** Дедупликация scouting jobs


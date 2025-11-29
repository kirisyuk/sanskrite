# Сущности системы Sanskrite

Полный список всех сущностей (моделей данных) в системе, организованный по категориям.

## 📄 Документы и Контент

### CoreDocs (`core_docs`)
- **CoreDoc** - базовый документ проекта (загруженный пользователем или созданный системой)
  - Может быть статическим или динамическим артефактом
  - Содержит `ExtractedText`, `StoragePath` (GCS), метаданные парсинга
  - Связан с проектом через `ProjectID`

### RAG Documents (`rag_documents`)
- **Document** - нормализованный документ для chunking/embedding
  - Связан с `SourceID` (источник данных)
  - Содержит `FullText`, `StoragePath`, статусы обработки
  - Используется для Vertex AI Search индексации

### Chunks (`rag_chunks`)
- **Chunk** - фрагмент документа для векторного поиска
  - Связан с `DocumentID`
  - Содержит embeddings, метаданные позиции в документе

### Processed Content (`processed_content`)
- **ProcessedContent** - кэш обработанного контента
  - Используется для оптимизации повторной обработки

## 🎨 Артефакты и Шаблоны

### Artifact Templates (`artifact_templates`)
- **ArtifactTemplate** - шаблон для генерации артефактов
  - Содержит структуру секций, промпты, настройки генерации
  - Используется для создания динамических документов (`entities.json`, `facts.json`, etc.)

### Artifact Sections
- **ArtifactSection** - секция артефакта
- **ArtifactVersion** - версия артефакта
- **ArtifactSectionSnapshot** - снимок секции
- **ArtifactContextSnapshot** - снимок контекста
- **ArtifactGenerationMeta** - метаданные генерации

### Generation Sessions (`generation_sessions`)
- **GenerationSession** - сессия генерации артефакта
  - Отслеживает процесс генерации секций
  - Содержит результаты и ошибки

## 🔍 Источники Данных

### Data Sources (`core_datasources`)
- **DataSource** - источник данных (web, RSS, Telegram, Twitter, document)
  - Содержит `SourceDetails` с параметрами для каждого типа
  - Статусы: `active`, `paused`, `error`
  - Связан с `ProjectID` и `TenantID`

### Source Profiles (`core_source_profiles`)
- **SourceProfile** - профиль источника с мета-анализом
  - **SourceProfileData** - данные профиля
  - **EditorialStance** - редакционная позиция
  - **Agenda** - повестка дня
  - **BehaviorPatterns** - паттерны поведения
  - **ContentPatterns** - паттерны контента
  - **EmotionalToneDistribution** - распределение эмоционального тона
  - **FocusEntity** - фокусные сущности
  - **Topic** - темы
  - **EditorialMethod** - редакционный метод
  - **Goal** - цели
  - **Metrics** - метрики
  - **RedFlag** - красные флаги
  - **CoordinatedSource** - координация источников

### Source Projects (`rag_source_projects`)
- **SourceProject** - связь источника с проектом
  - Содержит настройки фильтрации и обработки

### Source Ingest History (`source_ingest_history`)
- **SourceIngestHistory** - история обработки источника
  - Отслеживает успешные/неуспешные попытки ингеста

## 🧠 Онтологическая Модель

### Entities (`ontology_entities`)
- **Entity** - сущность (Person, Organization, Product, Location, Document, Facility, SupplyChainNode, GovernmentProject, IndexMetric, EventEntity, Other)
  - Базовые поля: `BaseOntologyFields` (ID, TenantID, Version, Labels, Description, Tags, Status)
  - Содержит `canonicalName`, `aliases`, `identifiers`, `attributes`
  - Может иметь `homeLocation`, `kbRefs` (ссылки на внешние базы знаний)

### Events (`ontology_events`)
- **Event** - событие (дискретное происшествие во времени)
  - Содержит `type`, `title`, `occurredAt`, `location`, `participants`
  - Связан с `claims`, `metrics`, `relatedEvents`

### Claims (`ontology_claims`)
- **Claim** - утверждение (subject-predicate-object триплет)
  - Содержит `subjectRef`, `predicate`, `objectRef`, `stance`, `confidence`
  - Может иметь `temporal`, `qualifiers`, `provenance`

### Relations (`ontology_relations`)
- **Relation** - отношение между сущностями/концептами
  - Содержит `subjectId`, `predicate`, `objectId`, `direction`, `confidence`
  - Может иметь `qualifiers`, `validFrom`, `validTo`, `provenance`

### Concepts (`ontology_concepts`)
- **Concept** - концепт (абстрактная идея/категория)
  - Типы: Domain, Process, Strategy, TrendConcept, Risk, Opportunity, RelationType, Policy, Other
  - Содержит `name`, `kind`, `hierarchy`, `relationsToConcepts`, `kbRefs`

### Metric States (`ontology_metric_states`)
- **MetricState** - состояние метрики в момент времени
  - Содержит `metricName`, `value`, `unit`, `subjectRef`, `observedAt`
  - Может иметь `aggregationType`, `qualifiers`, `provenance`

### Trends (`ontology_trends`)
- **Trend** - тренд (паттерн изменений во времени)
  - Типы: growth, decline, stability, volatility, cyclical, seasonal
  - Содержит `trendType`, `subjectRef`, `metricName`, `direction`, `magnitude`, `timeRange`

### Causal Links (`ontology_causal_links`)
- **CausalLink** - причинно-следственная связь
  - Содержит `causeRef`, `effectRef`, `confidence`, `mechanism`, `temporalGap`

### Scenarios (`ontology_scenarios`)
- **Scenario** - сценарий (возможное будущее состояние)
  - Типы: forecast, whatIf, counterfactual, plan
  - Содержит `title`, `scenarioType`, `timeHorizon`, `probability`, `assumptions`, `outcomes`

### Actionable Items (`ontology_actionable_items`)
- **ActionableItem** - actionable item (задача, рекомендация, решение)
  - Типы: task, recommendation, decision, milestone
  - Содержит `title`, `itemType`, `assignedTo`, `dueDate`, `priority`, `status`, `relatedEntityIds`

### Sources (`ontology_sources`)
- **Source** - источник информации для онтологии
  - Связан с документами и утверждениями

### Trust (`ontology_trusts`)
- **Trust** - модель доверия к источнику/утверждению

### Uncertainty Models (`ontology_uncertainty_models`)
- **UncertaintyModel** - модель неопределенности

## 👥 Пользователи и Проекты

### Users (`core_users`)
- **User** - пользователь системы
  - Содержит `Email`, `DisplayName`, `AuthType`, `Role`, `Status`
  - Может иметь `PreferredLanguage`, `CountryOfResidence`, `ParentUserID`
  - Содержит `CompanyRef`, `JobTitle`, `Department`, `OrgUnitRef`
  - Имеет `DefaultProjectID`, `ProjectRoles`

### Projects (`core_projects`)
- **Project** - проект пользователя
  - Содержит `Name`, `Description`, `Status`
  - Связан с `TenantID` и `UserID` (владелец)
  - Может иметь `DefaultArtifactConfig`

### Waitlist (`core_waitlist`)
- **WaitlistEntry** - запись в листе ожидания
  - Для регистрации новых пользователей

## 💬 Взаимодействия и Чат

### Interactions (`core_interactions`)
- **Interaction** - взаимодействие пользователя с системой
  - Может быть user message или assistant response
  - Связан с `ProjectID`, `UserID`
  - Содержит `Content`, `Type`, `Status`, `Metadata`

## 🔐 Правила и Безопасность

### Rules (`rag_rules`)
- **Rule** - правило для обработки документов
  - Используется для фильтрации и трансформации контента

### Share Tokens (`core_share_tokens`)
- **ShareToken** - токен для публичного доступа к проектам/документам
  - Содержит `Token`, `ProjectID`, `ExpiresAt`, `AccessLevel`

## 📊 Аналитика и Метрики

### Meta Analyses (`meta_analyses`)
- **MetaAnalysisEvent** - событие мета-анализа
  - Отслеживает аналитические события

### Meta Aggregates
- **MetaAggregateProject** (`meta_aggregate_project`) - агрегат мета-данных проекта
- **MetaAggregateUser** (`meta_aggregate_user`) - агрегат мета-данных пользователя

### Usage Metrics (`usage_metrics`)
- **UsageMetric** - метрика использования (API calls, costs, etc.)
  - Отслеживает использование LLM, Tavily, и других сервисов
  - Содержит `Provider`, `Model`, `Cost`, `Tokens`, `Duration`

### Search Events (`rag_search_events`)
- **SearchEvent** - событие поиска
  - Отслеживает поисковые запросы и результаты

## 🔄 Задачи и Очереди

### Jobs (`rag_jobs`)
- **Job** - задача обработки (legacy)
  - Используется для асинхронной обработки документов

### Agent Jobs (`agent_jobs`)
- **AgentJob** - задача агента с шагами
  - Содержит `Steps` (AgentStep) с capabilities
  - Отслеживает статус выполнения, прогресс
  - Поддерживает автоматический и ручной режимы

## 💳 Подписки и Биллинг

### Subscriptions (`subscriptions`)
- **Subscription** - подписка пользователя
  - Содержит `PlanID`, `Status`, `CurrentPeriodStart`, `CurrentPeriodEnd`
  - Может иметь `CancelAtPeriodEnd`, `TrialEnd`

### Vouchers (`vouchers`)
- **Voucher** - ваучер для подписки
  - Содержит `Code`, `ValidFrom`, `ValidTo`, `DurationMonths`
  - Отслеживает `UsedCount`, `MaxUses`, `IsActive`

## 📊 BigQuery Таблицы

### Metadata Tables
- **core_docs_metadata** - метаданные CoreDocs в BigQuery
  - Синхронизируется асинхронно из Firestore
  - Используется для аналитических запросов

- **documents_metadata** - метаданные RAG Documents в BigQuery
  - Синхронизируется асинхронно из Firestore

### Object Tables
- **core_docs_text_table** - Object Table для чтения файлов из GCS
  - Используется для анализа содержимого документов через BigQuery

### Ontology Tables
- **ontology_entities** - сущности онтологии в BigQuery
- **ontology_events** - события онтологии в BigQuery
- **ontology_relations** - отношения онтологии в BigQuery
- **ontology_claims** - утверждения онтологии в BigQuery
- И другие онтологические таблицы...

## 🔗 Связи между сущностями

### Основные связи:
1. **User** → **Project** (один ко многим)
2. **Project** → **CoreDoc** (один ко многим)
3. **Project** → **DataSource** (один ко многим)
4. **DataSource** → **Document** (один ко многим)
5. **Document** → **Chunk** (один ко многим)
6. **CoreDoc** → **ArtifactTemplate** (для динамических артефактов)
7. **Entity** ↔ **Relation** ↔ **Entity** (многие ко многим через Relation)
8. **Event** → **Entity** (participants)
9. **Claim** → **Entity/Concept** (subjectRef, objectRef)
10. **MetricState** → **Entity/Event** (subjectRef)
11. **Trend** → **Entity/Event** (subjectRef)
12. **CausalLink** → **Entity/Event/Claim** (causeRef, effectRef)
13. **Scenario** → **Entity/Event** (через assumptions/outcomes)
14. **ActionableItem** → **Entity** (assignedTo, relatedEntityIds)

## 📝 Примечания

- Все сущности имеют базовые поля: `ID`, `TenantID`, `CreatedAt`, `UpdatedAt`
- Онтологические сущности используют `BaseOntologyFields` с дополнительными полями версионирования
- Многие сущности синхронизируются в BigQuery для аналитики
- RAG документы индексируются в Vertex AI Search для семантического поиска
- Динамические артефакты генерируются на основе шаблонов и обновляются автоматически


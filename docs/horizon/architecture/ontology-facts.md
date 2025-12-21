## Ontology Facts (`ontology_facts`)

### Зачем
`ontology_facts` — это **унифицированный поток нормализованных фактов/событий**, производимый онтологическим пайплайном (из документов/чатов/и т.д.).

Ключевая цель: сделать Watchdog **инкрементальным** и **дешёвым**:
- Watchdog больше не сканирует “сырые источники” (web/telegram/rss) сам.
- Он читает только новые нормализованные факты с watermark (`lastScanAt`) и запускает Scout **только при наличии релевантных фактов**.

### Связь с таксономией Horizon
Каждый факт несёт `canonicalPaths` (Horizon-first пути), а также `actorPaths` и `locationPath`. Это позволяет:
- однозначно фильтровать факты по целевому `HorizonFragment.canonicalPath`
- избежать узла “локальные vs глобальные источники”

Нормализация путей делается общим кодом (см. `backend/internal/taxonomy/paths.go`).

### Индексация в Vertex AI Search
Факт экспортируется в GCS в виде JSON-документа (Discovery Engine `dataSchema=document`):
- `content` = `vectorText` (единый “векторный текст” факта)
- `structData` = фильтры (`type`, `tenantId`, `kind`, `factType`, `canonicalPaths`, `actorPaths`, `locationPath`, `confidence`, timestamps)

Путь в GCS:
- `gs://$RAG_DOCUMENTS_BUCKET/{tenantId}/ontology_facts/{factId}.json`

Импорт в Vertex:
- инкрементальный `ImportDocuments` по `gs://.../{tenantId}/ontology_facts/**`

### Watchdog бизнес-процесс (новый)
1) Источники поллятся и создают Documents.
2) `OntologyExtractor` извлекает entities/events/claims и эмитит `ontology_facts`.
3) Cloud Scheduler дергает `/api/v1/admin/horizon/watchdog/run`.
4) Watchdog берёт due-конфиги и читает **новые факты** по `canonicalPath` с `since = max(config.CreatedAt, config.LastScanAt)`.
5) Если есть релевантные факты и их “сигнал” (сейчас: `max(confidence)`) выше `triggerThreshold`, Watchdog запускает `ScoutingJob` с `sourceIds` из фактов.




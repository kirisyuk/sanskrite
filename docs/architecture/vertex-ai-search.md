# Vertex AI Search (RAG) в Sanskrite

Каноничная документация по изоляции тенантов, путям в GCS и поведению синхронизации. Код смотрите в репозитории (поиск по `vertexSearch`, `VertexSearch`, `VERTEX_SEARCH_`).

## Документы (актуальные)

| Документ | Назначение |
|----------|------------|
| [vertex-search-multi-tenancy.md](vertex-search-multi-tenancy.md) | Модель данных в GCS, метаданные, фильтрация по tenant, API `POST /api/v1/projects/{projectId}/vertex-search` |
| [VERTEX_SEARCH_SYNC_BEHAVIOR.md](VERTEX_SEARCH_SYNC_BEHAVIOR.md) | Инкрементальная синхронизация GCS → datastore, типичные задержки и проверки |

## Операции и скрипты

- Деплой / проверка env: [docs/deployment/VERTEX_SEARCH_SETUP.md](../deployment/VERTEX_SEARCH_SETUP.md), [scripts/VERTEX_SEARCH_SETUP.md](../../scripts/VERTEX_SEARCH_SETUP.md)
- Синхронизация: `backend/cmd/sync-all-vertex-search/README.md`, `scripts/sync-all-vertex-search.sh`
- Смоук: `scripts/test-vertex-search.sh`

## Архив

Старые варианты quick start и пошаговых заметок: [docs/archive/vertex-search/](../archive/vertex-search/) (могут дублировать или расходиться с кодом).

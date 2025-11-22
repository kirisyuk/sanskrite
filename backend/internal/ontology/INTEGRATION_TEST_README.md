# Интеграционные тесты онтологической системы

Эти тесты проверяют работу всего пайплайна онтологии с использованием реального LLM.

## Требования

Для запуска тестов необходимо установить следующие переменные окружения:

```bash
export GEMINI_API_KEY="your-gemini-api-key"
export GCP_PROJECT_ID="your-gcp-project-id"
export OPENAI_API_KEY="your-openai-api-key"  # Опционально, для embeddings
export GOOGLE_APPLICATION_CREDENTIALS="path/to/credentials.json"  # Опционально
```

## Запуск тестов

### Все тесты
```bash
cd backend
go test -v ./internal/ontology -run Test
```

### Конкретный тест
```bash
# Тест разрешения сущностей с транслитерацией
go test -v ./internal/ontology -run TestEntityResolution_Transliteration

# Тест полного пайплайна
go test -v ./internal/ontology -run TestFullPipeline

# Тест классификации доменов
go test -v ./internal/ontology -run TestDomainClassification

# Тест извлечения фактов
go test -v ./internal/ontology -run TestClaimExtraction

# Тест обнаружения причинно-следственных связей
go test -v ./internal/ontology -run TestCausalLinkDetection

# Тест обнаружения конфликтов
go test -v ./internal/ontology -run TestConflictDetection
```

## Что тестируется

### 1. TestEntityResolution_Transliteration
- Разрешение сущностей с точным совпадением
- Разрешение через транслитерацию (кириллица ↔ латиница)
- Использование LLM для разрешения ("Нвидиа" → "Nvidia")
- Автоматическое добавление aliases

### 2. TestEntityEnrichment_Wikidata
- Обогащение сущностей через Wikidata
- Добавление KBRefs с Wikidata Q-ID
- Обновление атрибутов и aliases

### 3. TestDomainClassification
- Инициализация иерархии доменов
- Классификация entities в домены через LLM
- Создание Relations между entities и доменами

### 4. TestClaimExtraction
- Извлечение фактов (Claims) из текста через LLM
- Создание Claims с SubjectRef/ObjectRef
- Связывание Claims с источниками

### 5. TestCausalLinkDetection
- Обнаружение причинно-следственных связей между событиями
- Использование LLM для анализа причинности
- Создание CausalLink объектов

### 6. TestConflictDetection
- Обнаружение конфликтующих Claims
- Создание UncertaintyModel для конфликтов
- Интеграция с TrustIntegrator

### 7. TestFullPipeline
- Полный пайплайн от документа до онтологии:
  1. Извлечение данных через LLM (entities, events, claims)
  2. Создание онтологических объектов
  3. Классификация доменов
  4. Проверка созданных entities, claims, events

## Важные замечания

1. **Тесты используют реальный LLM** - убедитесь, что у вас есть доступ к API
2. **Тесты создают данные в Firestore** - используйте тестовый проект или эмулятор
3. **Тесты автоматически очищают данные** после выполнения
4. **Тесты могут быть медленными** из-за вызовов LLM (таймаут 60-120 секунд)

## Использование Firestore эмулятора

Для локального тестирования можно использовать эмулятор:

```bash
# Запустить эмулятор
gcloud beta emulators firestore start --host-port=localhost:8080

# В другом терминале
export FIRESTORE_EMULATOR_HOST=localhost:8080
export GCP_PROJECT_ID=test-project
go test -v ./internal/ontology
```

## Примеры успешного выполнения

```
=== RUN   TestEntityResolution_Transliteration
    integration_test.go:123: Created test entity: Nvidia (ID: abc123)
    integration_test.go:135: ✓ Test 1 passed: Exact match 'Nvidia'
    integration_test.go:145: ✓ Test 2 passed: Transliteration 'Нвидиа' -> 'Nvidia'
--- PASS: TestEntityResolution_Transliteration (5.23s)

=== RUN   TestFullPipeline
    integration_test.go:570: Step 1: Extracting entities, events, and claims from document using LLM...
    integration_test.go:575: ✓ LLM extraction: 2 entities, 1 events, 2 claims
    integration_test.go:580: ✓ Document extraction completed
    integration_test.go:585: ✓ Created 2 entities
    integration_test.go:595: ✓ Classified 'Apple Inc.' into domains
    integration_test.go:609: ✓ Created 2 claims
    integration_test.go:616: ✓ Created 1 events
--- PASS: TestFullPipeline (12.45s)
```


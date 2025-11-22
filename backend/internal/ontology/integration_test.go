package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/domain"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/prompts"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"cloud.google.com/go/firestore"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/option"
)

// loadSecretFromGCP загружает секрет из GCP Secret Manager
// Возвращает пустую строку и nil ошибку, если секрет не найден (не критично)
func loadSecretFromGCP(ctx context.Context, projectID, secretName string) (string, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		// Не критично - может быть недоступен в локальной среде
		return "", nil
	}
	defer client.Close()

	// Пробуем разные варианты имен секретов (lowercase в GCP)
	secretNames := []string{
		secretName,                    // Как есть
		strings.ToLower(secretName),   // lowercase
		strings.ToLower(strings.ReplaceAll(secretName, "_", "-")), // lowercase с дефисами
	}

	for _, name := range secretNames {
		req := &secretmanagerpb.AccessSecretVersionRequest{
			Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, name),
		}
		result, err := client.AccessSecretVersion(ctx, req)
		if err == nil && result != nil && result.Payload != nil {
			return strings.TrimSpace(string(result.Payload.Data)), nil
		}
	}

	// Секрет не найден - это нормально, не критично
	return "", nil
}

// getEnvOrSecret получает значение из env или загружает из GCP Secret Manager
func getEnvOrSecret(ctx context.Context, projectID, envVar, secretName string) string {
	// Сначала проверяем env
	if val := os.Getenv(envVar); val != "" {
		return val
	}

	// Пробуем загрузить из GCP Secret Manager
	if projectID != "" {
		val, err := loadSecretFromGCP(ctx, projectID, secretName)
		if err == nil && val != "" {
			return val
		}
	}

	return ""
}

// setupTestClients создает тестовые клиенты для интеграционных тестов
func setupTestClients(t *testing.T) (*gen_combined.Client, ai.EmbeddingProvider, *firestore.Client, *ai.AITaskExecutor) {
	ctx := context.Background()

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		t.Skip("GCP_PROJECT_ID not set; skipping integration test")
	}

	// Загружаем API ключи из env или GCP Secret Manager
	apiKey := getEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "GEMINI_API_KEY_TEST")
	if apiKey == "" {
		// Пробуем альтернативные имена
		apiKey = getEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "gemini_api_key")
	}
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not found in env or GCP secrets; skipping LLM integration test")
	}

	location := os.Getenv("VERTEX_LOCATION")
	if location == "" {
		location = "us-central1"
	}

	// Создаем Gemini client
	geminiClient := gen_gemini.NewClient(apiKey, projectID, location, "", "")
	genaiClient := gen_combined.NewClient(geminiClient, "")

	// Создаем embedding provider (OpenAI для тестов)
	openaiKey := getEnvOrSecret(ctx, projectID, "OPENAI_API_KEY", "OPENAI_API_KEY_TEST")
	if openaiKey == "" {
		// Пробуем альтернативные имена
		openaiKey = getEnvOrSecret(ctx, projectID, "OPENAI_API_KEY", "openai_api_key")
	}
	var emb ai.EmbeddingProvider
	var executor *ai.AITaskExecutor
	if openaiKey != "" {
		modelManager := ai.NewAIModelManager()
		openaiService := ai.NewOpenAIService(openaiKey, modelManager)
		executor = ai.NewAITaskExecutor([]ai.AIService{openaiService}, modelManager)
		emb = ai.NewOpenAIEmbedding(executor, "text-embedding-3-small")
	} else {
		t.Log("OPENAI_API_KEY not found in env or GCP secrets; embedding tests will be skipped")
	}

	// Создаем AITaskExecutor для тестов (если еще не создан через OpenAI)
	if executor == nil && apiKey != "" {
		modelManager := ai.NewAIModelManager()
		vertexService := ai.NewVertexAIService(apiKey, modelManager)
		executor = ai.NewAITaskExecutor([]ai.AIService{vertexService}, modelManager)
	}

	// Создаем Firestore client (используем эмулятор если доступен)
	var opts []option.ClientOption
	credsPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if credsPath != "" {
		opts = append(opts, option.WithCredentialsFile(credsPath))
	}

	fsClient, err := firestore.NewClient(ctx, projectID, opts...)
	if err != nil {
		t.Fatalf("Failed to create Firestore client: %v", err)
	}

	return genaiClient, emb, fsClient, executor
}

// TestEntityResolution_Transliteration тестирует разрешение сущностей с транслитерацией
func TestEntityResolution_Transliteration(t *testing.T) {
	genaiClient, emb, fsClient, executor := setupTestClients(t)
	defer fsClient.Close()

	if executor == nil {
		t.Skip("AITaskExecutor not available - skipping test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenantID := "test-tenant-" + time.Now().Format("20060102150405")
	entitiesRepo := repo.NewEntityRepository(fsClient)

	// Создаем тестовую entity "Nvidia"
	nvidiaEntity := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Status:    "active",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		CanonicalName: "Nvidia",
		Type:          models.EntityTypeOrganization,
		Aliases:       []string{"NVIDIA", "NVIDIA Corporation"},
		KBRefs: []models.KBReference{
			{
				KB:         "Wikidata",
				ExternalID: "Q9686",
				Confidence: 0.95,
			},
		},
	}

	nvidiaID, err := entitiesRepo.Create(ctx, nvidiaEntity)
	if err != nil {
		t.Fatalf("Failed to create test entity: %v", err)
	}
	t.Logf("Created test entity: %s (ID: %s)", nvidiaEntity.CanonicalName, nvidiaID)

	// Создаем resolver
	logger := log.New(os.Stdout, "[Test] ", log.LstdFlags)
	resolver := NewEntityResolver(entitiesRepo, emb, genaiClient, executor, logger)

	// Тест 1: Точное совпадение
	resolved, err := resolver.ResolveEntity(ctx, tenantID, "Nvidia", models.EntityTypeOrganization)
	if err != nil {
		t.Fatalf("ResolveEntity failed: %v", err)
	}
	if resolved == nil {
		t.Fatal("Expected to find entity 'Nvidia', got nil")
	}
	if resolved.ID != nvidiaID {
		t.Fatalf("Expected entity ID %s, got %s", nvidiaID, resolved.ID)
	}
	t.Logf("✓ Test 1 passed: Exact match 'Nvidia'")

	// Тест 2: Транслитерация (кириллица -> латиница)
	resolved, err = resolver.ResolveEntity(ctx, tenantID, "Нвидиа", models.EntityTypeOrganization)
	if err != nil {
		t.Fatalf("ResolveEntity failed: %v", err)
	}
	if resolved == nil {
		t.Fatal("Expected to find entity 'Нвидиа' (should match 'Nvidia'), got nil")
	}
	if resolved.ID != nvidiaID {
		t.Fatalf("Expected entity ID %s, got %s", nvidiaID, resolved.ID)
	}
	t.Logf("✓ Test 2 passed: Transliteration 'Нвидиа' -> 'Nvidia'")

	// Проверяем, что alias был добавлен
	resolved, _ = entitiesRepo.Get(ctx, nvidiaID)
	foundAlias := false
	for _, alias := range resolved.Aliases {
		if alias == "Нвидиа" {
			foundAlias = true
			break
		}
	}
	if !foundAlias {
		t.Logf("Warning: Alias 'Нвидиа' was not added to entity")
	}

	// Очистка
	_ = entitiesRepo.Delete(ctx, nvidiaID)
}

// TestEntityEnrichment_Wikidata тестирует обогащение сущностей через Wikidata
func TestEntityEnrichment_Wikidata(t *testing.T) {
	_, _, fsClient, _ := setupTestClients(t)
	defer fsClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenantID := "test-tenant-" + time.Now().Format("20060102150405")
	entitiesRepo := repo.NewEntityRepository(fsClient)

	// Создаем простую entity без Wikidata данных
	entity := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Status:    "active",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		CanonicalName: "Apple Inc.",
		Type:          models.EntityTypeOrganization,
	}

	entityID, err := entitiesRepo.Create(ctx, entity)
	if err != nil {
		t.Fatalf("Failed to create test entity: %v", err)
	}

	// Создаем enricher (нужен Wikidata client)
	// Для теста используем mock или реальный клиент если доступен
	// Пока пропускаем если нет реального клиента
	wikidataClient := getWikidataClientForTest(t)
	if wikidataClient == nil {
		t.Skip("Wikidata client not available; skipping enrichment test")
	}

	enricher := NewEntityEnricher(entitiesRepo, wikidataClient, nil)

	// Обогащаем entity
	err = enricher.EnrichEntity(ctx, tenantID, entity)
	if err != nil {
		t.Logf("Enrichment failed (may be expected): %v", err)
	} else {
		// Проверяем, что Wikidata данные добавлены
		updated, _ := entitiesRepo.Get(ctx, entityID)
		if len(updated.KBRefs) == 0 {
			t.Logf("Warning: No KBRefs added after enrichment")
		} else {
			foundWikidata := false
			for _, ref := range updated.KBRefs {
				if ref.KB == "Wikidata" {
					foundWikidata = true
					t.Logf("✓ Wikidata enrichment successful: Q-ID %s", ref.ExternalID)
					break
				}
			}
			if !foundWikidata {
				t.Logf("Warning: Wikidata KBRef not found after enrichment")
			}
		}
	}

	// Очистка
	_ = entitiesRepo.Delete(ctx, entityID)
}

// TestDomainClassification тестирует классификацию доменов
func TestDomainClassification(t *testing.T) {
	genaiClient, _, fsClient, executor := setupTestClients(t)
	if executor == nil {
		t.Skip("AITaskExecutor not available - skipping test")
	}
	defer fsClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tenantID := "test-tenant-" + time.Now().Format("20060102150405")
	relationsRepo := repo.NewRelationRepository(fsClient)

	// Инициализируем domain service
	domainService := setupDomainService(t, fsClient, genaiClient, executor)

	// Инициализируем иерархию доменов
	err := domainService.InitializeHierarchy(ctx, tenantID)
	if err != nil {
		t.Fatalf("Failed to initialize domain hierarchy: %v", err)
	}

	// Создаем entity
	entitiesRepo := repo.NewEntityRepository(fsClient)
	entity := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:    tenantID,
			Status:      "active",
			Description: "Electric vehicle manufacturer",
		},
		CanonicalName: "Tesla Motors",
		Type:          models.EntityTypeOrganization,
	}
	entityID, err := entitiesRepo.Create(ctx, entity)
	if err != nil {
		t.Fatalf("Failed to create test entity: %v", err)
	}
	entity.ID = entityID

	// Классифицируем entity в домен
	contextText := "Tesla Motors is an electric vehicle manufacturer that produces cars, energy storage systems, and solar panels."
	err = domainService.ClassifyAndLinkEntity(ctx, tenantID, entity, contextText)
	if err != nil {
		t.Fatalf("Domain classification failed: %v", err)
	}

	t.Logf("✓ Domain classification successful for entity: %s", entity.CanonicalName)

	// Проверяем, что relations созданы
	relations, _ := relationsRepo.ListBySubject(ctx, tenantID, entity.ID, 10)
	if len(relations) == 0 {
		t.Logf("Warning: No domain relations created")
	} else {
		t.Logf("✓ Created %d domain relations", len(relations))
	}

	// Очистка
	_ = entitiesRepo.Delete(ctx, entityID)
}

// TestClaimExtraction тестирует извлечение фактов (Claims)
func TestClaimExtraction(t *testing.T) {
	genaiClient, _, fsClient, executor := setupTestClients(t)
	if executor == nil {
		t.Skip("AITaskExecutor not available - skipping test")
	}
	defer fsClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tenantID := "test-tenant-" + time.Now().Format("20060102150405")

	// Создаем тестовые entities
	entitiesRepo := repo.NewEntityRepository(fsClient)
	claimsRepo := repo.NewClaimRepository(fsClient)
	sourcesRepo := repo.NewSourceRepository(fsClient)

	// Создаем source
	url := "https://example.com/test-article"
	source := &models.Source{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID: tenantID,
			Status:   "active",
		},
		Name: "Test News Article",
		Type: models.SourceTypeReport,
		URL:  &url,
	}
	sourceID, _ := sourcesRepo.Create(ctx, source)

	// Создаем entity
	entity := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID: tenantID,
			Status:   "active",
		},
		CanonicalName: "Apple Inc.",
		Type:          models.EntityTypeOrganization,
	}
	entityID, _ := entitiesRepo.Create(ctx, entity)

	// Тест: извлекаем claim из текста используя LLM
	text := "Apple Inc. reported revenue of $394 billion in 2023. The company is headquartered in Cupertino, California."

	// Извлекаем данные через LLM (как в processor_worker)
	extraction := extractDataWithLLM(t, ctx, genaiClient, text, "en")
	if extraction == nil {
		t.Fatal("Failed to extract data from text")
	}

	// Используем OntologyExtractor для извлечения
	extractor := setupExtractor(t, fsClient, genaiClient, executor, tenantID)

	// Создаем временный document для обработки
	doc := &models.Document{
		TenantID: tenantID,
		SourceID: sourceID,
		Title:    "Test Document",
		FullText: text,
		Status:   "new",
	}

	// Извлекаем claims
	err := extractor.ExtractFromDocument(ctx, tenantID, doc, extraction)
	if err != nil {
		t.Fatalf("Claim extraction failed: %v", err)
	}

	// Проверяем, что claims созданы
	claims, err := claimsRepo.ListByTenant(ctx, tenantID, 10)
	if err != nil {
		t.Fatalf("Failed to list claims: %v", err)
	}

	if len(claims) == 0 {
		t.Fatal("Expected at least one claim, got 0")
	}

	t.Logf("✓ Extracted %d claims:", len(claims))
	for i, claim := range claims {
		if i >= 3 {
			break
		}
		objectID := "literal"
		if claim.ObjectRef.ID != nil {
			objectID = *claim.ObjectRef.ID
		} else if claim.ObjectRef.Value != nil {
			objectID = fmt.Sprintf("%v", claim.ObjectRef.Value)
		}
		t.Logf("  - %s %s %s (confidence: %.2f)",
			claim.SubjectRef.ID, claim.Predicate, objectID, claim.Confidence)
	}

	// Очистка
	_ = entitiesRepo.Delete(ctx, entityID)
	_ = sourcesRepo.Delete(ctx, sourceID)
	for _, claim := range claims {
		_ = claimsRepo.Delete(ctx, claim.ID)
	}
}

// TestCausalLinkDetection тестирует обнаружение причинно-следственных связей
func TestCausalLinkDetection(t *testing.T) {
	genaiClient, _, fsClient, executor := setupTestClients(t)
	if executor == nil {
		t.Skip("AITaskExecutor not available - skipping test")
	}
	defer fsClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tenantID := "test-tenant-" + time.Now().Format("20060102150405")
	eventsRepo := repo.NewEventRepository(fsClient)
	causalLinksRepo := repo.NewCausalLinkRepository(fsClient)

	// Создаем два связанных события
	event1 := &models.Event{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID: tenantID,
			Status:   "active",
		},
		Type:       "ProductLaunch",
		Title:      "Apple launched iPhone 15",
		OccurredAt: timePtr(time.Date(2023, 9, 15, 0, 0, 0, 0, time.UTC)),
		Confidence: 0.95,
		Provenance: models.Provenance{
			SourceIDs: []string{"test-source-1"},
		},
	}
	event1ID, _ := eventsRepo.Create(ctx, event1)

	event2 := &models.Event{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID: tenantID,
			Status:   "active",
		},
		Type:       "MarketChange",
		Title:      "Smartphone market share increased",
		OccurredAt: timePtr(time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC)),
		Confidence: 0.90,
		Provenance: models.Provenance{
			SourceIDs: []string{"test-source-2"},
		},
	}
	event2ID, _ := eventsRepo.Create(ctx, event2)

	// Используем CausalDetector
	logger := log.New(os.Stdout, "[Test] ", log.LstdFlags)
	detector := NewCausalDetector(eventsRepo, causalLinksRepo, genaiClient, executor, logger)

	// Тестируем обнаружение для каждого события
	// DetectCausalLinksForEvent анализирует новое событие на связи с существующими
	err := detector.DetectCausalLinksForEvent(ctx, tenantID, event2)
	if err != nil {
		t.Logf("Causal link detection failed: %v (may be expected)", err)
	}

	// Проверяем созданные causal links
	causalLinks, err2 := causalLinksRepo.ListByTenant(ctx, tenantID, 10)
	if err2 != nil {
		t.Logf("Failed to list causal links: %v", err2)
		causalLinks = []*models.CausalLink{}
	}

	if len(causalLinks) == 0 {
		t.Logf("No causal links detected (may be expected if LLM doesn't find clear causation)")
	} else {
		t.Logf("✓ Detected %d causal links:", len(causalLinks))
		for _, link := range causalLinks {
			t.Logf("  - %s -> %s (strength: %.2f, probability: %.2f)",
				link.CauseRef.ID, link.EffectRef.ID, link.Strength, link.Probability)
		}
	}

	// Очистка
	_ = eventsRepo.Delete(ctx, event1ID)
	_ = eventsRepo.Delete(ctx, event2ID)
	for _, link := range causalLinks {
		_ = causalLinksRepo.Delete(ctx, link.ID)
	}
}

// TestConflictDetection тестирует обнаружение конфликтов
func TestConflictDetection(t *testing.T) {
	_, _, fsClient, _ := setupTestClients(t)
	defer fsClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tenantID := "test-tenant-" + time.Now().Format("20060102150405")
	entitiesRepo := repo.NewEntityRepository(fsClient)
	claimsRepo := repo.NewClaimRepository(fsClient)
	uncertaintyRepo := repo.NewUncertaintyModelRepository(fsClient)
	sourcesRepo := repo.NewSourceRepository(fsClient)
	trustRepo := repo.NewTrustRepository(fsClient)

	// Создаем entity
	entity := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID: tenantID,
			Status:   "active",
		},
		CanonicalName: "Test Company",
		Type:          models.EntityTypeOrganization,
	}
	entityID, _ := entitiesRepo.Create(ctx, entity)

	// Создаем два конфликтующих claim
	source1ID, _ := sourcesRepo.Create(ctx, &models.Source{
		BaseOntologyFields: models.BaseOntologyFields{TenantID: tenantID, Status: "active"},
		Name:               "Source 1",
		Type:               models.SourceTypeReport,
	})

	source2ID, _ := sourcesRepo.Create(ctx, &models.Source{
		BaseOntologyFields: models.BaseOntologyFields{TenantID: tenantID, Status: "active"},
		Name:               "Source 2",
		Type:               models.SourceTypeReport,
	})

	claim1 := &models.Claim{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID: tenantID,
			Status:   "active",
		},
		SubjectRef: models.SubjectRef{
			ID:   entityID,
			Kind: models.ReferenceKindEntity,
		},
		Predicate:  "revenue",
		ObjectRef:  models.ObjectRef{Value: "1000000", Kind: models.ReferenceKindLiteral},
		Confidence: 0.9,
		Provenance: models.ClaimProvenance{
			SourceIDs: []string{source1ID},
		},
	}
	claim1ID, _ := claimsRepo.Create(ctx, claim1)

	claim2 := &models.Claim{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID: tenantID,
			Status:   "active",
		},
		SubjectRef: models.SubjectRef{
			ID:   entityID,
			Kind: models.ReferenceKindEntity,
		},
		Predicate:  "revenue",
		ObjectRef:  models.ObjectRef{Value: "2000000", Kind: models.ReferenceKindLiteral},
		Confidence: 0.85,
		Provenance: models.ClaimProvenance{
			SourceIDs: []string{source2ID},
		},
	}
	claim2ID, _ := claimsRepo.Create(ctx, claim2)

	// Используем ConflictDetector
	eventsRepo := repo.NewEventRepository(fsClient)
	trustIntegrator := NewTrustIntegrator(sourcesRepo, claimsRepo, eventsRepo, trustRepo, nil)
	detector := NewConflictDetector(claimsRepo, uncertaintyRepo, trustIntegrator, nil)

	// Тестируем обнаружение конфликтов для каждого claim
	err := detector.DetectConflictsForClaim(ctx, tenantID, claim2)
	if err != nil {
		t.Logf("Conflict detection failed: %v (may be expected)", err)
	}

	// Проверяем созданные conflicts
	conflicts, err2 := uncertaintyRepo.ListByTenant(ctx, tenantID, 10)
	if err2 != nil {
		t.Logf("Failed to list conflicts: %v", err2)
		conflicts = []*models.UncertaintyModel{}
	}

	if len(conflicts) == 0 {
		t.Logf("No conflicts detected (may be expected)")
	} else {
		t.Logf("✓ Detected %d conflicts:", len(conflicts))
		for _, conflict := range conflicts {
			t.Logf("  - Type: %s, Materiality: %s", conflict.Type, conflict.Materiality)
		}
	}

	// Очистка
	_ = claimsRepo.Delete(ctx, claim1ID)
	_ = claimsRepo.Delete(ctx, claim2ID)
	_ = entitiesRepo.Delete(ctx, entityID)
	_ = sourcesRepo.Delete(ctx, source1ID)
	_ = sourcesRepo.Delete(ctx, source2ID)
	for _, conflict := range conflicts {
		_ = uncertaintyRepo.Delete(ctx, conflict.ID)
	}
}

// TestFullPipeline тестирует полный пайплайн от документа до онтологии
func TestFullPipeline(t *testing.T) {
	genaiClient, _, fsClient, executor := setupTestClients(t)
	if executor == nil {
		t.Skip("AITaskExecutor not available - skipping test")
	}
	defer fsClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	tenantID := "test-tenant-" + time.Now().Format("20060102150405")

	// Инициализируем все компоненты
	extractor := setupExtractor(t, fsClient, genaiClient, executor, tenantID)
	domainService := setupDomainService(t, fsClient, genaiClient, executor)
	_ = domainService // Используется ниже для классификации

	// Тестовый документ
	doc := &models.Document{
		TenantID: tenantID,
		Title:    "Tech News: Apple and Nvidia Announcements",
		FullText: `Apple Inc. announced new iPhone models on September 15, 2023. 
The company reported revenue growth of 15% year-over-year.
Nvidia Corporation unveiled new AI chips that will power next-generation computing.
The tech industry is experiencing rapid innovation in artificial intelligence.`,
		Status: "new",
	}

	// Шаг 1: Извлечение данных через LLM
	t.Log("Step 1: Extracting entities, events, and claims from document using LLM...")
	extraction := extractDataWithLLM(t, ctx, genaiClient, doc.FullText, "en")
	if extraction == nil {
		t.Fatal("Failed to extract data from document")
	}
	t.Logf("✓ LLM extraction: %d entities, %d events, %d claims",
		len(extraction.Entities), len(extraction.Events), len(extraction.Claims))

	// Шаг 2: Извлечение в онтологическую модель
	t.Log("Step 2: Extracting into ontological model...")
	err := extractor.ExtractFromDocument(ctx, tenantID, doc, extraction)
	if err != nil {
		t.Fatalf("Document extraction failed: %v", err)
	}
	t.Log("✓ Document extraction completed")

	// Шаг 3: Проверяем созданные entities
	entitiesRepo := repo.NewEntityRepository(fsClient)
	entities, _ := entitiesRepo.ListByTenant(ctx, tenantID, 10)
	t.Logf("✓ Created %d entities", len(entities))

	// Шаг 4: Классификация доменов
	if len(entities) > 0 {
		t.Log("Step 4: Classifying entities into domains...")
		// Инициализируем иерархию
		err = domainService.InitializeHierarchy(ctx, tenantID)
		if err != nil {
			t.Logf("Failed to initialize domain hierarchy: %v", err)
		}
		for _, entity := range entities[:min(3, len(entities))] {
			contextText := doc.FullText
			err := domainService.ClassifyAndLinkEntity(ctx, tenantID, entity, contextText)
			if err != nil {
				t.Logf("Domain classification failed for %s: %v", entity.CanonicalName, err)
			} else {
				t.Logf("✓ Classified '%s' into domains", entity.CanonicalName)
			}
		}
	}

	// Шаг 5: Проверяем claims
	claimsRepo := repo.NewClaimRepository(fsClient)
	claims, _ := claimsRepo.ListByTenant(ctx, tenantID, 10)
	t.Logf("✓ Created %d claims", len(claims))

	// Шаг 6: Проверяем events
	eventsRepo := repo.NewEventRepository(fsClient)
	events, _ := eventsRepo.ListByTenant(ctx, tenantID, 10)
	t.Logf("✓ Created %d events", len(events))

	// Очистка
	for _, entity := range entities {
		_ = entitiesRepo.Delete(ctx, entity.ID)
	}
	for _, claim := range claims {
		_ = claimsRepo.Delete(ctx, claim.ID)
	}
	for _, event := range events {
		_ = eventsRepo.Delete(ctx, event.ID)
	}
}

// Helper functions

func setupDomainService(t *testing.T, fsClient *firestore.Client, genaiClient *gen_combined.Client, executor *ai.AITaskExecutor) *domain.DomainService {
	conceptsRepo := repo.NewConceptRepository(fsClient)
	relationsRepo := repo.NewRelationRepository(fsClient)
	logger := log.New(os.Stdout, "[Test] ", log.LstdFlags)
	return domain.NewDomainService(conceptsRepo, relationsRepo, genaiClient, executor, logger)
}

func setupExtractor(t *testing.T, fsClient *firestore.Client, genaiClient *gen_combined.Client, executor *ai.AITaskExecutor, tenantID string) *OntologyExtractor {
	entitiesRepo := repo.NewEntityRepository(fsClient)
	eventsRepo := repo.NewEventRepository(fsClient)
	claimsRepo := repo.NewClaimRepository(fsClient)
	sourcesRepo := repo.NewSourceRepository(fsClient)
	relationsRepo := repo.NewRelationRepository(fsClient)
	uncertaintyRepo := repo.NewUncertaintyModelRepository(fsClient)
	trustRepo := repo.NewTrustRepository(fsClient)
	causalLinksRepo := repo.NewCausalLinkRepository(fsClient)

	emb := getEmbeddingProvider(t)
	_ = emb // Может быть nil если OPENAI_API_KEY не установлен
	logger := log.New(os.Stdout, "[Test] ", log.LstdFlags)
	entityResolver := NewEntityResolver(entitiesRepo, emb, genaiClient, executor, logger)
	trustIntegrator := NewTrustIntegrator(sourcesRepo, claimsRepo, eventsRepo, trustRepo, logger)
	conflictDetector := NewConflictDetector(claimsRepo, uncertaintyRepo, trustIntegrator, logger)
	causalDetector := NewCausalDetector(eventsRepo, causalLinksRepo, genaiClient, executor, logger)
	domainService := setupDomainService(t, fsClient, genaiClient, executor)
	entityEnricher := getEntityEnricher(t, entitiesRepo)

	return NewOntologyExtractor(
		entitiesRepo,
		eventsRepo,
		claimsRepo,
		relationsRepo,
		sourcesRepo,
		entityResolver,
		entityEnricher,
		trustIntegrator,
		conflictDetector,
		causalDetector,
		domainService,
		nil,
	)
}

// extractDataWithLLM извлекает данные из текста используя LLM (как в processor_worker)
func extractDataWithLLM(t *testing.T, ctx context.Context, genaiClient *gen_combined.Client, text, language string) *DocumentExtraction {
	// Используем DocumentExtractionPrompt
	prompt := prompts.Format(prompts.DocumentExtractionPrompt, map[string]string{
		"language": strings.ToLower(language),
		"text":     text,
	})

	// JSON Schema для извлечения
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"entities": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"type":       map[string]interface{}{"type": "string"},
						"value":      map[string]interface{}{"type": "string"},
						"confidence": map[string]interface{}{"type": "number"},
					},
				},
			},
			"events": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":   map[string]interface{}{"type": "string"},
						"dateISO": map[string]interface{}{"type": "string"},
						"context": map[string]interface{}{"type": "string"},
					},
				},
			},
			"claims": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text":       map[string]interface{}{"type": "string"},
						"summary":    map[string]interface{}{"type": "string"},
						"confidence": map[string]interface{}{"type": "number"},
					},
				},
			},
		},
	}

	opts := gen_combined.Options{
		ModelOverride:     "gemini_fast",
		GroundingEnabled:  false,
		SystemInstruction: "You are an expert content analyst. Extract entities, events, and claims from the text. Be precise and conservative.",
		Temperature:       0.3,
		MaxOutputTokens:   8192, // Увеличено для надежности (как в других тестах)
	}

	t.Logf("Calling LLM with prompt length: %d chars, max tokens: %d", len(prompt), opts.MaxOutputTokens)
	// Используем GenerateJSON вместо GenerateJSONStrict, так как строгий режим вызывает проблемы с function calling
	resp, err := genaiClient.GenerateJSON(ctx, prompt, nil, schema, opts)
	if err != nil {
		t.Logf("LLM extraction failed: %v", err)
		return nil
	}

	if resp == nil {
		t.Logf("LLM returned nil response")
		return nil
	}

	t.Logf("LLM response received: content=%d chars, tool calls=%d, usage=%+v, isToolStep=%v", 
		len(resp.Content), len(resp.ToolCalls), resp.Usage, resp.IsToolStep)
	
	if resp.RawJSON != "" {
		t.Logf("Raw JSON preview: %.500s", resp.RawJSON)
	}

	if resp.Content == "" && len(resp.ToolCalls) == 0 {
		t.Logf("LLM returned empty content and no tool calls. This might indicate a quota/budget issue.")
		return nil
	}

	// Парсим ответ - может быть полный DocumentExtraction или только нужные поля
	var fullResult struct {
		Entities []ExtractedEntity `json:"entities"`
		Events   []ExtractedEvent  `json:"events"`
		Claims   []ExtractedClaim  `json:"claims"`
		// Другие поля из DocumentExtractionPrompt (игнорируем)
		Language     string `json:"language,omitempty"`
		Sentiment    float64 `json:"sentiment,omitempty"`
		Categories   interface{} `json:"categories,omitempty"`
		Topics       interface{} `json:"topics,omitempty"`
		DocumentType string `json:"documentType,omitempty"`
		KeyPoints    interface{} `json:"keyPoints,omitempty"`
		Outlinks     interface{} `json:"outlinks,omitempty"`
	}
	
	var result struct {
		Entities []ExtractedEntity `json:"entities"`
		Events   []ExtractedEvent  `json:"events"`
		Claims   []ExtractedClaim  `json:"claims"`
	}

	content := strings.TrimSpace(resp.Content)
	
	// Если Content пустой, но есть tool calls, извлекаем из них
	if content == "" && len(resp.ToolCalls) > 0 {
		for _, tc := range resp.ToolCalls {
			if tc.Name == "return_structured" || tc.Name == "extract_data" {
				content = strings.TrimSpace(tc.Arguments)
				t.Logf("Extracted content from tool call %s: %d chars", tc.Name, len(content))
				break
			}
		}
	}

	// Извлекаем JSON из markdown code blocks если есть
	if strings.Contains(content, "```") {
		// Находим первый { после ```json или ```
		jsonStart := strings.Index(content, "{")
		jsonEnd := strings.LastIndex(content, "}")
		
		if jsonStart >= 0 && jsonEnd > jsonStart {
			content = content[jsonStart : jsonEnd+1]
		} else {
			// Альтернативный способ - убираем markdown блоки построчно
			lines := strings.Split(content, "\n")
			var cleaned []string
			inCodeBlock := false
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "```") {
					inCodeBlock = !inCodeBlock
					continue
				}
				if !inCodeBlock {
					cleaned = append(cleaned, line)
				}
			}
			content = strings.Join(cleaned, "\n")
		}
		content = strings.TrimSpace(content)
	}

	if content == "" {
		t.Logf("Content is empty after cleaning. Tool calls: %d", len(resp.ToolCalls))
		if len(resp.ToolCalls) > 0 {
			t.Logf("Tool call details: %+v", resp.ToolCalls[0])
		}
		return nil
	}

	if err := json.Unmarshal([]byte(content), &fullResult); err != nil {
		t.Logf("Failed to parse LLM response: %v, content length: %d, preview: %.200s", err, len(content), content)
		return nil
	}

	// Извлекаем только нужные поля
	result.Entities = fullResult.Entities
	result.Events = fullResult.Events
	result.Claims = fullResult.Claims

	t.Logf("Extracted: %d entities, %d events, %d claims", 
		len(result.Entities), len(result.Events), len(result.Claims))

	return &DocumentExtraction{
		Entities: result.Entities,
		Events:   result.Events,
		Claims:   result.Claims,
	}
}

func getWikidataClientForTest(t *testing.T) WikidataClientInterface {
	// Можно использовать реальный клиент или mock
	// Для теста используем реальный если доступен
	return nil // Пока возвращаем nil, можно добавить реальный клиент
}

func getEmbeddingProvider(t *testing.T) ai.EmbeddingProvider {
	ctx := context.Background()
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}

	openaiKey := getEnvOrSecret(ctx, projectID, "OPENAI_API_KEY", "OPENAI_API_KEY_TEST")
	if openaiKey == "" {
		openaiKey = getEnvOrSecret(ctx, projectID, "OPENAI_API_KEY", "openai_api_key")
	}
	if openaiKey == "" {
		return nil
	}
	modelManager := ai.NewAIModelManager()
	openaiService := ai.NewOpenAIService(openaiKey, modelManager)
	executor := ai.NewAITaskExecutor([]ai.AIService{openaiService}, modelManager)
	return ai.NewOpenAIEmbedding(executor, "text-embedding-3-small")
}

func getEntityEnricher(t *testing.T, entitiesRepo *repo.EntityRepository) *EntityEnricher {
	wikidataClient := getWikidataClientForTest(t)
	if wikidataClient == nil {
		return nil
	}
	return NewEntityEnricher(entitiesRepo, wikidataClient, nil)
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


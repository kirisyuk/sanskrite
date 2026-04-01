package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ontology"

	firebase "firebase.google.com/go/v4"
	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
	"google.golang.org/api/option"
)

// tavilyClientAdapter адаптирует httphandlers.TavilyClient к agents.TavilySearchClient интерфейсу
type tavilyClientAdapter struct {
	client *httphandlers.TavilyClient
}

func (a *tavilyClientAdapter) Search(ctx context.Context, query string, maxResults int) (*agents.TavilySearchResponse, error) {
	resp, err := a.client.Search(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	// Конвертировать httphandlers.TavilySearchResponse в agents.TavilySearchResponse
	results := make([]agents.TavilySearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		results = append(results, agents.TavilySearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
			Score:   r.Score,
		})
	}
	return &agents.TavilySearchResponse{
		Query:   resp.Query,
		Results: results,
	}, nil
}

// wikidataAdapterForEnricher адаптирует httphandlers.WikidataClient к ontology.WikidataClientInterface
type wikidataAdapterForEnricher struct {
	client *httphandlers.WikidataClient
}

func (a *wikidataAdapterForEnricher) Search(ctx context.Context, query string, maxResults int) (*ontology.WikidataSearchResponse, error) {
	resp, err := a.client.Search(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	results := make([]ontology.WikidataSearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		results = append(results, ontology.WikidataSearchResult{
			ID:          r.ID,
			Title:       r.Title,
			Label:       r.Label,
			Description: r.Description,
			URL:         r.URL,
		})
	}
	return &ontology.WikidataSearchResponse{
		Query:   resp.Query,
		Results: results,
	}, nil
}

func (a *wikidataAdapterForEnricher) GetEntityData(ctx context.Context, qID string) (*ontology.WikidataEntityData, error) {
	data, err := a.client.GetEntityData(ctx, qID)
	if err != nil {
		return nil, err
	}
	return &ontology.WikidataEntityData{
		ID:            data.ID,
		Label:         data.Label,
		Description:   data.Description,
		URL:           data.URL,
		Properties:    data.Properties,
		PropertiesRaw: data.PropertiesRaw,
	}, nil
}

func (a *wikidataAdapterForEnricher) ConvertToEntity(ctx context.Context, entityData *ontology.WikidataEntityData, tenantID string) (*models.Entity, error) {
	return a.client.ConvertToEntity(ctx, &httphandlers.WikidataEntityData{
		ID:            entityData.ID,
		Label:         entityData.Label,
		Description:   entityData.Description,
		URL:           entityData.URL,
		Properties:    entityData.Properties,
		PropertiesRaw: entityData.PropertiesRaw,
	}, tenantID)
}

// Тестовая программа для проверки работы AgentOrchestrator
// getProjectID пытается получить GCP project ID из различных источников
func getProjectID() string {
	// 1. Проверяем GCP_PROJECT_ID
	if projectID := os.Getenv("GCP_PROJECT_ID"); projectID != "" {
		return projectID
	}
	// 2. Проверяем GOOGLE_CLOUD_PROJECT (стандартная переменная GCP)
	if projectID := os.Getenv("GOOGLE_CLOUD_PROJECT"); projectID != "" {
		return projectID
	}
	// 3. Пытаемся получить из gcloud config (если доступен)
	// Это будет работать только если gcloud установлен и настроен
	return ""
}

func main() {
	ctx := context.Background()

	// Инициализация Firestore
	projectID := getProjectID()
	if projectID == "" {
		log.Fatal("GCP_PROJECT_ID or GOOGLE_CLOUD_PROJECT environment variable is required.\n" +
			"Set it via: export GCP_PROJECT_ID=your-project-id\n" +
			"Or ensure gcloud is configured: gcloud config set project your-project-id")
	}

	opts := []option.ClientOption{}
	if credsPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credsPath != "" {
		opts = append(opts, option.WithCredentialsFile(credsPath))
	}

	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, opts...)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}

	fsClient, err := app.Firestore(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize Firestore: %v", err)
	}
	defer fsClient.Close()

	// Инициализация репозиториев
	jobsRepo := repo.NewAgentJobsRepository(fsClient)
	coreDocsRepo := repo.NewCoreDocsRepo(fsClient)
	docsRepo := repo.NewDocumentsRepository(fsClient) // Для поиска CoreDoc ID через Document
	// chunksRepo := repo.NewChunksRepository(fsClient) // Не используется в Scenario 5

	// Создание logger
	logger := log.New(os.Stdout, "[AgentTest] ", log.LstdFlags)

	// Инициализация GenAI клиента (загружаем секреты из GCP или env)
	geminiAPIKey := gcp.GetEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "gemini_api_key")
	if geminiAPIKey == "" {
		log.Fatal("GEMINI_API_KEY is required (set env var or secret in GCP Secret Manager)")
	}
	vertexLocation := os.Getenv("VERTEX_LOCATION")
	if vertexLocation == "" {
		vertexLocation = "us-central1"
	}
	geminiClient := gen_gemini.NewClient(geminiAPIKey, projectID, vertexLocation, "", "")
	genaiClient := gen_combined.NewClient(geminiClient, "")

	// Инициализация embeddings для индексации документов
	modelManager := ai.NewAIModelManager()
	openaiAPIKey := gcp.GetEnvOrSecret(ctx, projectID, "OPENAI_API_KEY", "openai_api_key")
	var emb ai.EmbeddingProvider
	if openaiAPIKey != "" {
		aiServices := []ai.AIService{ai.NewOpenAIService(openaiAPIKey, modelManager)}
		exec := ai.NewAITaskExecutor(aiServices, modelManager)
		emb = ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
		logger.Printf("Using OpenAI for embeddings")
	} else {
		aiServices := []ai.AIService{ai.NewGeminiService(geminiAPIKey, modelManager)}
		exec := ai.NewAITaskExecutor(aiServices, modelManager)
		emb = ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
		logger.Printf("Using Vertex AI (Gemini) for embeddings")
	}

	// Создание capability registry
	capabilityRegistry := agents.NewCapabilityRegistry()

	// Регистрация исполнителей
	docReadExecutor := agents.NewDocumentReadExecutor(coreDocsRepo, logger)
	docReadExecutor.WithDocumentsRepository(docsRepo) // Для поиска CoreDoc ID через Document
	capabilityRegistry.Register(docReadExecutor)
	capabilityRegistry.Register(agents.NewLLMGenerateExecutor(genaiClient, logger))
	capabilityRegistry.Register(agents.NewDocumentWriteExecutor(coreDocsRepo, logger))

	// Vertex AI Search для data_search (загружаем из секретов или используем дефолт)
	vertexSearchLocation := os.Getenv("VERTEX_SEARCH_LOCATION")
	if vertexSearchLocation == "" {
		vertexSearchLocation = "global"
	}
	vertexSearchDataStoreID := gcp.GetEnvOrSecret(ctx, projectID, "VERTEX_SEARCH_DATA_STORE_ID", "vertex_search_data_store_id")
	if vertexSearchDataStoreID == "" {
		// Используем дефолтное значение из документации
		vertexSearchDataStoreID = "rag-documents-store"
		logger.Printf("Using default VERTEX_SEARCH_DATA_STORE_ID: %s", vertexSearchDataStoreID)
	} else {
		logger.Printf("Loaded VERTEX_SEARCH_DATA_STORE_ID from env/secret: %s", vertexSearchDataStoreID)
	}
	vertexSearchClient := ai.NewVertexAISearchClient(projectID, vertexSearchLocation, vertexSearchDataStoreID, "")
	capabilityRegistry.Register(agents.NewDataSearchExecutor(vertexSearchClient, docsRepo, logger))
	logger.Printf("Vertex AI Search client registered for data_search capability (dataStore: %s)", vertexSearchDataStoreID)

	// Tavily client для web_search (если доступен)
	tavilyAPIKey := gcp.GetEnvOrSecret(ctx, projectID, "TAVILY_API_KEY", "tavily_api_key")
	var tavilyClient *httphandlers.TavilyClient
	if tavilyAPIKey != "" {
		tavilyClient = httphandlers.NewTavilyClient(tavilyAPIKey)
		// Создать адаптер для TavilyClient
		tavilyAdapter := &tavilyClientAdapter{client: tavilyClient}
		capabilityRegistry.Register(agents.NewWebSearchExecutor(tavilyAdapter, logger))
		logger.Printf("Tavily client registered for web_search capability")
	} else {
		logger.Printf("WARNING: TAVILY_API_KEY not set, web_search capability will not be available")
	}

	// BigQuery для bigquery_query (если доступен)
	bigqueryDatasetID := os.Getenv("BIGQUERY_DATASET_ID")
	if bigqueryDatasetID == "" {
		bigqueryDatasetID = "nv_horizon_documents"
	}
	bigqueryClient, err := gcp.NewBigQueryClient(ctx, projectID, bigqueryDatasetID)
	if err == nil {
		capabilityRegistry.Register(agents.NewBigQueryQueryExecutor(bigqueryClient, logger))
		logger.Printf("BigQuery client registered for bigquery_query capability")
	} else {
		logger.Printf("WARNING: Failed to initialize BigQuery client: %v", err)
	}

	// LLM Analyze executor
	capabilityRegistry.Register(agents.NewLLMAnalyzeExecutor(genaiClient, logger))

	// LLM Validate executor (для проверки релевантности и итерационного создания шагов)
	capabilityRegistry.Register(agents.NewLLMValidateExecutor(genaiClient, capabilityRegistry, logger))

	// Entity repositories для ontology capabilities
	entitiesRepo := repo.NewEntityRepository(fsClient)
	relationsRepo := repo.NewRelationRepository(fsClient)
	claimsRepo := repo.NewClaimRepository(fsClient)
	eventsRepo := repo.NewEventRepository(fsClient)
	uncertaintyRepo := repo.NewUncertaintyModelRepository(fsClient)
	causalLinksRepo := repo.NewCausalLinkRepository(fsClient)
	sourcesRepo := repo.NewSourceRepository(fsClient)
	trustRepo := repo.NewTrustRepository(fsClient)

	// EntityResolver для дедупликации
	aiServices := []ai.AIService{}
	if geminiAPIKey != "" {
		aiServices = append(aiServices, ai.NewGeminiService(geminiAPIKey, modelManager))
	}
	if openaiAPIKey != "" {
		aiServices = append(aiServices, ai.NewOpenAIService(openaiAPIKey, modelManager))
	}
	exec := ai.NewAITaskExecutor(aiServices, modelManager)
	entityResolver := ontology.NewEntityResolver(entitiesRepo, emb, genaiClient, exec, logger)

	// EntityEnricher для обогащения из Wikidata
	wikidataClient := httphandlers.GetWikidataClientFromEnv()
	var entityEnricher *ontology.EntityEnricher
	if wikidataClient != nil {
		// WikidataClient уже реализует WikidataClientInterface через методы Search, GetEntityData, ConvertToEntity
		// Создаем адаптер, который реализует интерфейс
		wikidataAdapter := &wikidataAdapterForEnricher{client: wikidataClient}
		entityEnricher = ontology.NewEntityEnricher(entitiesRepo, wikidataAdapter, logger)
	}

	// TrustIntegrator и ConflictDetector
	trustIntegrator := ontology.NewTrustIntegrator(sourcesRepo, claimsRepo, eventsRepo, trustRepo, logger)
	conflictDetector := ontology.NewConflictDetector(claimsRepo, uncertaintyRepo, trustIntegrator, logger)

	// CausalDetector
	causalDetector := ontology.NewCausalDetector(eventsRepo, causalLinksRepo, genaiClient, exec, logger)

	// Регистрация Entity executors
	capabilityRegistry.Register(agents.NewEntitySearchExecutor(entitiesRepo, entityResolver, logger))
	capabilityRegistry.Register(agents.NewEntityDeduplicateExecutor(entitiesRepo, entityResolver, genaiClient, logger))
	capabilityRegistry.Register(agents.NewEntityRelationBuildExecutor(entitiesRepo, relationsRepo, entityResolver, logger))
	if entityEnricher != nil {
		capabilityRegistry.Register(agents.NewEntityEnrichExecutor(entitiesRepo, entityEnricher, logger))
	}
	capabilityRegistry.Register(agents.NewEntityExtractExecutor(entitiesRepo, entityResolver, genaiClient, logger))
	capabilityRegistry.Register(agents.NewEntityHierarchyBuildExecutor(entitiesRepo, relationsRepo, logger))
	capabilityRegistry.Register(agents.NewConflictDetectExecutor(conflictDetector, claimsRepo, logger))
	capabilityRegistry.Register(agents.NewCausalLinkDetectExecutor(causalDetector, eventsRepo, logger))
	capabilityRegistry.Register(agents.NewRelationFindExecutor(relationsRepo, logger))
	capabilityRegistry.Register(agents.NewEntityValidateExecutor(entitiesRepo, logger))
	capabilityRegistry.Register(agents.NewEntityMergeAttributesExecutor(entitiesRepo, logger))
	logger.Printf("All ontology capabilities registered")

	// Регистрация executors для управления источниками
	dataSourcesRepo := repo.NewDataSourcesRepository(fsClient)
	sourceProjectsRepo := repo.NewSourceProjectsRepository(fsClient)

	// Telegram client для чтения каналов
	// Используем тот же подход, что и в server/main.go и run_worker/main.go
	telegramServiceURL := strings.TrimSpace(os.Getenv("TELEGRAM_SERVICE_URL"))
	if telegramServiceURL == "" {
		logger.Printf("WARNING: TELEGRAM_SERVICE_URL is not set - Telegram features will be disabled")
	}
	telegramClient := telegram.NewClient(telegramServiceURL)

	// Publisher для WebSocket событий (может быть nil для тестов)
	var publisher *realtime.Publisher
	publisher, pubsubErr := realtime.NewPublisher(ctx, projectID)
	if pubsubErr != nil {
		logger.Printf("WARNING: Failed to create publisher: %v (WebSocket events will not be sent)", pubsubErr)
		publisher = nil
	}

	capabilityRegistry.Register(agents.NewSourceListExecutor(dataSourcesRepo, sourceProjectsRepo, logger))
	capabilityRegistry.Register(agents.NewTelegramChannelReadExecutor(telegramClient, logger))
	capabilityRegistry.Register(agents.NewSourceCreateExecutor(dataSourcesRepo, sourceProjectsRepo, publisher, logger))

	// PlanCalibrate executor
	capabilityRegistry.Register(agents.NewPlanCalibrateExecutor(genaiClient, capabilityRegistry, logger))
	logger.Printf("Source management capabilities registered")

	// Создание orchestrator
	orchestrator := agents.NewAgentOrchestrator(jobsRepo, capabilityRegistry, nil, logger).
		WithRepositories(coreDocsRepo, nil, nil, nil, nil).
		WithGenAI(genaiClient) // Нужно для автоматического планирования

	// Примечание: WithTavily, WithBigQuery уже не нужны здесь,
	// так как клиенты уже переданы в конструкторы capability executors
	// Но WithGenAI нужен для PlanGenerateExecutor и ReplanExecutor

	// Определить какой тест запускать из аргументов командной строки
	testToRun := ""
	if len(os.Args) > 1 {
		testToRun = os.Args[1]
	}

	// Запустить только указанный тест, или все если не указан
	if testToRun == "" {
		// Запустить все тесты
		fmt.Println("\n=== Test Scenario 1: Simple 3-step job ===")
		testScenario1(ctx, orchestrator, jobsRepo, coreDocsRepo)
		fmt.Println("\n=== Test Scenario 5: Entity Search → Deduplicate → Build Relations ===")
		testScenario5(ctx, orchestrator, jobsRepo, entitiesRepo, relationsRepo, entityResolver)
		fmt.Println("\n=== Test Scenario 6: All New Capabilities ===")
		testScenario6(ctx, orchestrator, jobsRepo, entitiesRepo, relationsRepo, claimsRepo, eventsRepo, entityEnricher, conflictDetector, causalDetector, entityResolver, genaiClient)
		fmt.Println("\n=== Test Scenario 7: Auto Planning ===")
		testScenario7AutoPlanning(ctx, orchestrator, jobsRepo, coreDocsRepo)
		fmt.Println("\n=== Test Scenario 8: Replanning ===")
		testScenario8Replanning(ctx, orchestrator, jobsRepo, coreDocsRepo)
		fmt.Println("\n=== Test Scenario 9: Dynamic Supply Planning with Data Improvement ===")
		testScenario9(ctx, orchestrator, jobsRepo, coreDocsRepo, fsClient)
		fmt.Println("\n=== Test Scenario 10: Automatic Telegram Source Discovery ===")
		testScenario10(ctx, orchestrator, jobsRepo, dataSourcesRepo, sourceProjectsRepo, telegramClient, publisher, fsClient)
	} else {
		// Запустить только указанный тест
		switch testToRun {
		case "1", "simple":
			fmt.Println("\n=== Test Scenario 1: Simple 3-step job ===")
			testScenario1(ctx, orchestrator, jobsRepo, coreDocsRepo)
		case "5", "entity":
			fmt.Println("\n=== Test Scenario 5: Entity Search → Deduplicate → Build Relations ===")
			testScenario5(ctx, orchestrator, jobsRepo, entitiesRepo, relationsRepo, entityResolver)
		case "6", "all-capabilities":
			fmt.Println("\n=== Test Scenario 6: All New Capabilities ===")
			testScenario6(ctx, orchestrator, jobsRepo, entitiesRepo, relationsRepo, claimsRepo, eventsRepo, entityEnricher, conflictDetector, causalDetector, entityResolver, genaiClient)
		case "7", "auto-planning":
			fmt.Println("\n=== Test Scenario 7: Auto Planning ===")
			testScenario7AutoPlanning(ctx, orchestrator, jobsRepo, coreDocsRepo)
		case "8", "replanning":
			fmt.Println("\n=== Test Scenario 8: Replanning ===")
			testScenario8Replanning(ctx, orchestrator, jobsRepo, coreDocsRepo)
		case "9", "supply-planning":
			fmt.Println("\n=== Test Scenario 9: Dynamic Supply Planning with Data Improvement ===")
			testScenario9(ctx, orchestrator, jobsRepo, coreDocsRepo, fsClient)
		case "10", "telegram-discovery":
			fmt.Println("\n=== Test Scenario 10: Automatic Telegram Source Discovery ===")
			testScenario10(ctx, orchestrator, jobsRepo, dataSourcesRepo, sourceProjectsRepo, telegramClient, publisher, fsClient)
		default:
			fmt.Printf("Unknown test scenario: %s\n", testToRun)
			fmt.Println("Available scenarios: 1, 5, 6, 7, 8, 9, 10")
		}
	}

	// Тестовый сценарий 2: Поиск → Чтение → Анализ → Сохранение
	// fmt.Println("\n=== Test Scenario 2: Search → Read → Analyze → Write ===")
	// testScenario2(ctx, orchestrator, jobsRepo, coreDocsRepo, docsRepo)
	_ = testScenario2 // Suppress unused function warning

	// Тестовый сценарий 3: Job с некритичным шагом (должен продолжить при ошибке)
	// fmt.Println("\n=== Test Scenario 3: Non-critical step failure handling ===")
	// testScenario3(ctx, orchestrator, jobsRepo)
	_ = testScenario3 // Suppress unused function warning

	// Тестовый сценарий 4: BigQuery → Анализ → Сохранение отчета
	// fmt.Println("\n=== Test Scenario 4: BigQuery → Analyze → Save Report ===")
	// testScenario4(ctx, orchestrator, jobsRepo, coreDocsRepo)
	_ = testScenario4       // Suppress unused function warning
	_ = indexCoreDocForTest // Suppress unused function warning
}

func testScenario1(ctx context.Context, orchestrator *agents.AgentOrchestrator, jobsRepo *repo.AgentJobsRepository, coreDocsRepo *repo.CoreDocsRepo) {
	// Создать тестовый документ, если его нет
	testDocID := "test-doc-1"
	testDoc, err := coreDocsRepo.Get(ctx, testDocID)
	if err != nil {
		// Создать тестовый документ с фиксированным ID используя Upsert
		now := time.Now().UTC()
		sizeBytes := int64(100)
		extractedText := "Это тестовый документ для проверки работы агентов. Содержит информацию о поставках картофеля."
		testDoc = &models.CoreDoc{
			ID:            testDocID,
			TenantID:      "test-tenant",
			ProjectID:     "test-project",
			Filename:      "test-document.txt",
			ContentType:   "text/plain",
			SizeBytes:     &sizeBytes,
			ExtractedText: &extractedText,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := coreDocsRepo.Upsert(ctx, testDoc); err != nil {
			log.Printf("WARNING: Failed to create test document: %v", err)
		} else {
			fmt.Printf("Created test document: %s\n", testDocID)
		}
	} else {
		fmt.Printf("Test document already exists: %s\n", testDocID)
	}

	// Создать job
	task := map[string]interface{}{
		"userQuery":   "Подготовить документы для аналитики",
		"documentIds": []string{testDocID},
	}

	job, err := orchestrator.CreateJob(ctx, "test-tenant", "test-project", "test-user", "document_preparation", task, true, "")
	if err != nil {
		log.Fatalf("Failed to create job: %v", err)
	}

	fmt.Printf("Created job: %s\n", job.ID)

	// Добавить шаги
	steps := []models.AgentStep{
		{
			ID:         "step1",
			Capability: "document_read",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"documentIds": []string{testDocID},
			},
			DependsOn:  []string{},
			IsCritical: true,
		},
		{
			ID:         "step2",
			Capability: "llm_generate",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"prompt": "Проанализируй документ и предложи улучшения",
			},
			DependsOn:  []string{"step1"},
			IsCritical: false,
		},
		{
			ID:         "step3",
			Capability: "document_write",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"documentId": testDocID,
				"content":    "Improved content",
			},
			DependsOn:  []string{"step2"},
			IsCritical: true,
		},
	}

	job.Steps = steps
	if err := jobsRepo.Update(ctx, job); err != nil {
		log.Fatalf("Failed to update job with steps: %v", err)
	}

	// Выполнить job
	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
	}

	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	// Получить финальный статус
	finalJob, _ := jobsRepo.Get(ctx, job.ID)
	if finalJob != nil {
		fmt.Printf("Final status: %s, progress: %.2f\n", finalJob.Status, finalJob.Progress)
		for _, step := range finalJob.Steps {
			fmt.Printf("  Step %s: %s - %s\n", step.ID, step.Capability, step.Status)
		}
	}
}

func testScenario2(ctx context.Context, orchestrator *agents.AgentOrchestrator, jobsRepo *repo.AgentJobsRepository, coreDocsRepo *repo.CoreDocsRepo, _ *repo.DocumentsRepository) {
	// Сценарий: Найти документ → Прочитать → Проанализировать → Сохранить изменения
	fmt.Println("\n--- Scenario 2: Search → Read → Analyze → Write ---")

	// Используем реальный тенант и projectId из существующих документов
	testTenantID := "kirisyuk@gmail.com"
	testProjectID := "KtwHOHp0LTNAG63HrkC6" // Реальный projectId из пути GCS

	// Создать документ для поиска
	searchDocID := "search-test-doc"
	now := time.Now().UTC()
	sizeBytes := int64(200)
	extractedText := "Документ о поставках картофеля в 2024 году. Объем поставок составил 1000 тонн. Основные поставщики: ООО Агро, ИП Иванов."
	searchDoc := &models.CoreDoc{
		ID:            searchDocID,
		TenantID:      testTenantID,
		ProjectID:     testProjectID,
		Filename:      "potato-supplies-2024.txt",
		ContentType:   "text/plain",
		SizeBytes:     &sizeBytes,
		ExtractedText: &extractedText,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := coreDocsRepo.Upsert(ctx, searchDoc); err != nil {
		log.Printf("WARNING: Failed to create search test document: %v", err)
	} else {
		fmt.Printf("Created search test document: %s (tenant: %s, project: %s)\n", searchDocID, testTenantID, testProjectID)
	}

	task := map[string]interface{}{
		"userQuery":   "Найти и улучшить документ о поставках",
		"searchQuery": "поставки картофеля",
	}

	job, err := orchestrator.CreateJob(ctx, testTenantID, testProjectID, "test-user", "document_improvement", task, true, "")
	if err != nil {
		log.Printf("Failed to create job: %v", err)
		return
	}

	fmt.Printf("Created job: %s\n", job.ID)

	steps := []models.AgentStep{
		{
			ID:         "search1",
			Capability: "data_search",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"query": "поставки картофеля",
			},
			DependsOn:  []string{},
			IsCritical: false, // Поиск некритичен
		},
		{
			ID:         "read1",
			Capability: "document_read",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"documentIds": []string{searchDocID}, // Используем известный ID, но в реальности брали бы из search1
			},
			DependsOn:  []string{"search1"},
			IsCritical: true,
		},
		{
			ID:         "analyze1",
			Capability: "llm_generate",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"prompt": "Проанализируй документ и предложи улучшенную версию с более структурированными данными",
			},
			DependsOn:  []string{"read1"},
			IsCritical: false,
		},
		{
			ID:         "write1",
			Capability: "document_write",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"documentId": searchDocID,
				"content":    "Улучшенная версия документа будет здесь",
			},
			DependsOn:  []string{"analyze1"},
			IsCritical: true,
		},
	}

	job.Steps = steps
	if err := jobsRepo.Update(ctx, job); err != nil {
		log.Printf("Failed to update job with steps: %v", err)
		return
	}

	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
	}

	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	finalJob, _ := jobsRepo.Get(ctx, job.ID)
	if finalJob != nil {
		fmt.Printf("Final status: %s, progress: %.2f\n", finalJob.Status, finalJob.Progress)
		for _, step := range finalJob.Steps {
			fmt.Printf("  Step %s: %s - %s\n", step.ID, step.Capability, step.Status)
		}
	}
}

// indexCoreDocForTest индексирует CoreDoc (создает Document, Vertex индексирует через GCS)
func indexCoreDocForTest(ctx context.Context, cd *models.CoreDoc, docsRepo *repo.DocumentsRepository) error {
	if cd.ExtractedText == nil || *cd.ExtractedText == "" {
		return fmt.Errorf("no text to index")
	}

	text := strings.TrimSpace(*cd.ExtractedText)
	if text == "" {
		return fmt.Errorf("no text to index")
	}

	virtualSourceID := fmt.Sprintf("project:%s", cd.ProjectID)
	externalID := fmt.Sprintf("coredoc:%s", cd.ID)
	now := time.Now().UTC()

	existingDoc, _ := docsRepo.GetByCoreDocID(ctx, cd.TenantID, cd.ID)
	if existingDoc != nil {
		existingDoc.FullText = text
		existingDoc.Title = cd.Filename
		existingDoc.ContentType = cd.ContentType
		existingDoc.DocumentType = "coredoc"
		existingDoc.DocumentPath = fmt.Sprintf("coredoc/%s/%s", cd.ProjectID, cd.ID)
		existingDoc.ContentHash = repo.HashContent(text)
		existingDoc.UpdatedAt = now
		existingDoc.Status = "indexed"
		return docsRepo.Upsert(ctx, existingDoc)
	}

	doc := &models.Document{
		TenantID:     cd.TenantID,
		SourceID:     virtualSourceID,
		ExternalID:   externalID,
		Title:        cd.Filename,
		DocumentPath: fmt.Sprintf("coredoc/%s/%s", cd.ProjectID, cd.ID),
		FullText:     text,
		Status:       "indexed",
		ContentType:  cd.ContentType,
		DocumentType: "coredoc",
		ContentHash:  repo.HashContent(text),
		IngestedAt:   now,
		CreatedAt:    now,
		UpdatedAt:    now,
		Attrs: map[string]any{
			"coreDocId":    cd.ID,
			"projectId":    cd.ProjectID,
			"documentType": "coredoc",
		},
	}
	_, err := docsRepo.Create(ctx, doc)
	return err
}

func testScenario3(ctx context.Context, orchestrator *agents.AgentOrchestrator, jobsRepo *repo.AgentJobsRepository) {
	// Job где некритичный шаг падает, но job продолжается
	fmt.Println("\n--- Scenario 3: Non-critical step failure ---")

	task := map[string]interface{}{
		"userQuery": "Тест обработки ошибок",
	}

	job, err := orchestrator.CreateJob(ctx, "test-tenant", "test-project", "test-user", "error_handling_test", task, true, "")
	if err != nil {
		log.Printf("Failed to create job: %v", err)
		return
	}

	fmt.Printf("Created job: %s\n", job.ID)

	steps := []models.AgentStep{
		{
			ID:         "step1",
			Capability: "llm_generate",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"prompt": "Простое задание",
			},
			DependsOn:  []string{},
			IsCritical: false, // Некритичный шаг
		},
		{
			ID:         "step2",
			Capability: "llm_generate",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"prompt": "Этот шаг должен выполниться даже если step1 упал",
			},
			DependsOn:  []string{"step1"},
			IsCritical: false,
		},
		{
			ID:         "step3",
			Capability: "document_read",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"documentIds": []string{"non-existent-doc"}, // Упадет
			},
			DependsOn:  []string{"step2"},
			IsCritical: false, // Некритичный, но упадет
		},
		{
			ID:         "step4",
			Capability: "llm_generate",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"prompt": "Этот шаг должен выполниться",
			},
			DependsOn:  []string{"step3"}, // Зависит от step3, но step3 некритичный
			IsCritical: false,
		},
	}

	job.Steps = steps
	if err := jobsRepo.Update(ctx, job); err != nil {
		log.Printf("Failed to update job with steps: %v", err)
		return
	}

	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
	}

	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	finalJob, _ := jobsRepo.Get(ctx, job.ID)
	if finalJob != nil {
		fmt.Printf("Final status: %s, progress: %.2f\n", finalJob.Status, finalJob.Progress)
		for _, step := range finalJob.Steps {
			statusInfo := fmt.Sprintf("%s", step.Status)
			if step.Error != nil {
				statusInfo += fmt.Sprintf(" (error: %s)", *step.Error)
			}
			fmt.Printf("  Step %s: %s - %s\n", step.ID, step.Capability, statusInfo)
		}
	}
}

func testScenario4(ctx context.Context, orchestrator *agents.AgentOrchestrator, jobsRepo *repo.AgentJobsRepository, _ *repo.CoreDocsRepo) {
	// Сценарий: BigQuery запрос → Анализ результатов → Сохранение отчета
	fmt.Println("\n--- Scenario 4: BigQuery → Analyze → Save Report ---")

	task := map[string]interface{}{
		"userQuery": "Проанализировать данные в BigQuery",
	}

	job, err := orchestrator.CreateJob(ctx, "test-tenant", "test-project", "test-user", "bigquery_analysis", task, true, "")
	if err != nil {
		log.Printf("Failed to create job: %v", err)
		return
	}

	fmt.Printf("Created job: %s\n", job.ID)

	steps := []models.AgentStep{
		{
			ID:         "bq1",
			Capability: "bigquery_query",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"sql": "SELECT COUNT(*) as total_docs FROM `nv_horizon_documents.core_docs_metadata` LIMIT 1",
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		{
			ID:         "analyze1",
			Capability: "llm_generate",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"prompt": "Проанализируй результаты запроса и создай краткий отчет",
			},
			DependsOn:  []string{"bq1"},
			IsCritical: false,
		},
		{
			ID:         "save1",
			Capability: "document_write",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"documentId": "test-doc-1",
				"content":    "Отчет будет сохранен здесь",
			},
			DependsOn:  []string{"analyze1"},
			IsCritical: false,
		},
	}

	job.Steps = steps
	if err := jobsRepo.Update(ctx, job); err != nil {
		log.Printf("Failed to update job with steps: %v", err)
		return
	}

	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
	}

	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	finalJob, _ := jobsRepo.Get(ctx, job.ID)
	if finalJob != nil {
		fmt.Printf("Final status: %s, progress: %.2f\n", finalJob.Status, finalJob.Progress)
		for _, step := range finalJob.Steps {
			fmt.Printf("  Step %s: %s - %s\n", step.ID, step.Capability, step.Status)
		}
	}
}

func testScenario5(ctx context.Context, orchestrator *agents.AgentOrchestrator, jobsRepo *repo.AgentJobsRepository, entitiesRepo *repo.EntityRepository, relationsRepo *repo.RelationRepository, _ *ontology.EntityResolver) {
	// Сценарий: Поиск сущностей → Дедупликация → Построение связей
	fmt.Println("\n--- Scenario 5: Entity Search → Deduplicate → Build Relations ---")

	testTenantID := "kirisyuk@gmail.com"
	testProjectID := "KtwHOHp0LTNAG63HrkC6"

	// Создаем тестовые сущности: "Китай" и "Кытай" (дубликаты)
	now := time.Now().UTC()
	entity1ID := "test-entity-china-1"
	entity2ID := "test-entity-china-2"

	// Создаем первую сущность "Китай"
	entity1 := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			ID:        entity1ID,
			TenantID:  testTenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "test",
			UpdatedBy: "test",
			Labels:    map[string]string{"ru": "Китай", "en": "China"},
			Status:    "active",
		},
		CanonicalName: "Китай",
		Type:          models.EntityTypeLocation,
		Aliases:       []string{"China"},
		KBRefs: []models.KBReference{
			{
				KB:         "Wikidata",
				ExternalID: "Q148",
				Confidence: 1.0,
				LastSync:   &now,
			},
		},
	}

	// Создаем вторую сущность "Кытай" (дубликат)
	entity2 := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			ID:        entity2ID,
			TenantID:  testTenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "test",
			UpdatedBy: "test",
			Labels:    map[string]string{"ru": "Кытай"},
			Status:    "active",
		},
		CanonicalName: "Кытай",
		Type:          models.EntityTypeLocation,
		Aliases:       []string{},
	}

	// Создаем сущности в Firestore
	if err := entitiesRepo.Upsert(ctx, entity1); err != nil {
		log.Printf("WARNING: Failed to create entity1: %v", err)
	} else {
		fmt.Printf("Created entity1: %s (%s)\n", entity1ID, entity1.CanonicalName)
	}

	if err := entitiesRepo.Upsert(ctx, entity2); err != nil {
		log.Printf("WARNING: Failed to create entity2: %v", err)
	} else {
		fmt.Printf("Created entity2: %s (%s)\n", entity2ID, entity2.CanonicalName)
	}

	// Создаем связь между сущностями (для тестирования relation_build)
	relationID := "test-relation-1"
	relation := &models.Relation{
		BaseOntologyFields: models.BaseOntologyFields{
			ID:        relationID,
			TenantID:  testTenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "test",
			UpdatedBy: "test",
			Status:    "active",
		},
		SubjectID:  entity1ID,
		Predicate:  "locatedIn",
		ObjectID:   entity2ID,
		Direction:  models.RelationDirectionSubjectToObject,
		Confidence: 0.8,
	}

	if err := relationsRepo.Upsert(ctx, relation); err != nil {
		log.Printf("WARNING: Failed to create relation: %v", err)
	} else {
		fmt.Printf("Created relation: %s -> %s\n", entity1ID, entity2ID)
	}

	// Создаем job
	task := map[string]interface{}{
		"userQuery": "Найти и дедуплицировать сущности, затем построить связи",
	}

	job, err := orchestrator.CreateJob(ctx, testTenantID, testProjectID, "test-user", "entity_deduplication", task, true, "")
	if err != nil {
		log.Printf("Failed to create job: %v", err)
		return
	}

	fmt.Printf("Created job: %s\n", job.ID)

	// Шаги: поиск → дедупликация → построение связей
	steps := []models.AgentStep{
		{
			ID:         "search1",
			Capability: "entity_search",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"query":      []string{"Китай", "Кытай"},
				"entityType": "Location",
				"maxResults": 10,
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		{
			ID:         "deduplicate1",
			Capability: "entity_deduplicate",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"entityIds": []string{entity1ID, entity2ID},
			},
			DependsOn:  []string{"search1"},
			IsCritical: false,
		},
		{
			ID:         "build_relations1",
			Capability: "entity_relation_build",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"entityId": entity1ID,
				"maxDepth": 2,
			},
			DependsOn:  []string{"deduplicate1"},
			IsCritical: false,
		},
	}

	job.Steps = steps
	if err := jobsRepo.Update(ctx, job); err != nil {
		log.Printf("Failed to update job with steps: %v", err)
		return
	}

	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
	}

	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	finalJob, _ := jobsRepo.Get(ctx, job.ID)
	if finalJob != nil {
		fmt.Printf("Final status: %s, progress: %.2f\n", finalJob.Status, finalJob.Progress)
		for _, step := range finalJob.Steps {
			statusInfo := string(step.Status)
			if step.Error != nil {
				statusInfo += fmt.Sprintf(" (error: %s)", *step.Error)
			}
			fmt.Printf("  Step %s: %s - %s\n", step.ID, step.Capability, statusInfo)
			if step.Output != nil {
				if count, ok := step.Output["count"].(float64); ok {
					fmt.Printf("    Output count: %.0f\n", count)
				}
			}
		}
	}

	// Проверяем результат дедупликации
	finalEntity1, _ := entitiesRepo.Get(ctx, entity1ID)
	finalEntity2, _ := entitiesRepo.Get(ctx, entity2ID)
	if finalEntity1 != nil {
		fmt.Printf("\nEntity1 after deduplication: %s, aliases: %v, status: %s\n",
			finalEntity1.CanonicalName, finalEntity1.Aliases, finalEntity1.Status)
	}
	if finalEntity2 != nil {
		fmt.Printf("Entity2 after deduplication: %s, aliases: %v, status: %s\n",
			finalEntity2.CanonicalName, finalEntity2.Aliases, finalEntity2.Status)
	}
}

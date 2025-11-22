package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// VertexAISearchClient клиент для работы с Vertex AI Search API
// Использует REST API для поиска по документам, индексированным из GCS
//
// Автоматическая синхронизация:
// - Документы автоматически сохраняются в GCS при создании/обновлении через DocumentsRepository
// - После загрузки в GCS бэкенд автоматически триггерит импорт в Vertex AI Search
// - Импорт выполняется асинхронно (не блокирует основной поток)
// - Документы становятся доступны для поиска в течение 1-2 минут
//
// Multi-tenancy и изоляция данных:
// - Документы хранятся в GCS как: gs://bucket/{tenantId}/{sourceId}/{docId}.txt
// - Metadata включает tenantId для фильтрации
// - API автоматически применяет фильтр: tenantId:"current_user" OR tenantId:"global"
// - Тенанты не могут получить доступ к документам друг друга
type VertexAISearchClient struct {
	projectID   string
	location    string
	dataStoreID string
	apiKey      string
	httpClient  *http.Client
}

// NewVertexAISearchClient создает новый клиент для Vertex AI Search
func NewVertexAISearchClient(projectID, location, dataStoreID, apiKey string) *VertexAISearchClient {
	return &VertexAISearchClient{
		projectID:   projectID,
		location:    location,
		dataStoreID: dataStoreID,
		apiKey:      apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SearchRequest параметры поиска
type SearchRequest struct {
	Query              string                `json:"query"`
	PageSize           int                   `json:"pageSize,omitempty"`
	PageToken          string                `json:"pageToken,omitempty"`
	Filter             string                `json:"filter,omitempty"`
	OrderBy            string                `json:"orderBy,omitempty"`
	BoostSpec          *BoostSpec            `json:"boostSpec,omitempty"`
	SpellCorrectionSpec *SpellCorrectionSpec `json:"spellCorrectionSpec,omitempty"`
}

// BoostSpec спецификация для повышения релевантности
type BoostSpec struct {
	ConditionBoostSpecs []ConditionBoostSpec `json:"conditionBoostSpecs,omitempty"`
}

// ConditionBoostSpec условие для повышения релевантности
type ConditionBoostSpec struct {
	Condition string  `json:"condition"`
	Boost     float64 `json:"boost"`
}

// SpellCorrectionSpec спецификация для исправления опечаток
type SpellCorrectionSpec struct {
	Mode string `json:"mode,omitempty"` // "AUTO" | "SUGGESTION_ONLY"
}

// SearchResponse ответ от Vertex AI Search API
type SearchResponse struct {
	Results       []SearchResult `json:"results"`
	NextPageToken string         `json:"nextPageToken,omitempty"`
	TotalSize     int            `json:"totalSize,omitempty"`
}

// SearchResult результат поиска
type SearchResult struct {
	ID          string                 `json:"id"`
	Document    SearchDocument         `json:"document"`
	RelevanceScore float64             `json:"relevanceScore,omitempty"`
}

// SearchDocument документ из результата поиска
type SearchDocument struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	URI      string                 `json:"uri"`
	Title    string                 `json:"title,omitempty"`
	StructData map[string]interface{} `json:"structData,omitempty"`
	JSONData   string               `json:"jsonData,omitempty"`
}

// Search выполняет поиск по документам в Vertex AI Search
func (c *VertexAISearchClient) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if c.projectID == "" || c.location == "" || c.dataStoreID == "" {
		return nil, fmt.Errorf("vertex AI search not configured: project, location, or dataStoreID missing")
	}

	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	if req.PageSize == 0 {
		req.PageSize = 10
	}

	// Формируем URL для REST API
	// Формат: https://discoveryengine.googleapis.com/v1beta/projects/{project}/locations/{location}/dataStores/{dataStore}/servingConfigs/default_search:search
	url := fmt.Sprintf(
		"https://discoveryengine.googleapis.com/v1beta/projects/%s/locations/%s/dataStores/%s/servingConfigs/default_search:search",
		c.projectID,
		c.location,
		c.dataStoreID,
	)

	requestBody := map[string]interface{}{
		"query": req.Query,
		"pageSize": req.PageSize,
	}
	if req.PageToken != "" {
		requestBody["pageToken"] = req.PageToken
	}
	if req.Filter != "" {
		requestBody["filter"] = req.Filter
	}
	if req.OrderBy != "" {
		requestBody["orderBy"] = req.OrderBy
	}
	if req.BoostSpec != nil {
		requestBody["boostSpec"] = req.BoostSpec
	}
	if req.SpellCorrectionSpec != nil {
		requestBody["spellCorrectionSpec"] = req.SpellCorrectionSpec
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Используем API key для аутентификации
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	} else {
		// Альтернатива: использовать Application Default Credentials через gcloud auth
		// В этом случае нужен Bearer token от gcloud auth print-access-token
		return nil, fmt.Errorf("API key is required for Vertex AI Search")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.Printf("[VertexAISearch] Searching: query=%s, pageSize=%d", req.Query, req.PageSize)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vertex AI search API error %d: %s", resp.StatusCode, string(body))
	}

	var searchResp SearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	log.Printf("[VertexAISearch] Found %d results", len(searchResp.Results))
	return &searchResp, nil
}

// IsConfigured проверяет, настроен ли клиент
func (c *VertexAISearchClient) IsConfigured() bool {
	return c.projectID != "" && c.location != "" && c.dataStoreID != "" && c.apiKey != ""
}

// ImportDocumentsRequest параметры для импорта документов
type ImportDocumentsRequest struct {
	GCSPath          string // gs://bucket/path pattern (например, gs://bucket/tenantId/**)
	ReconciliationMode string // "INCREMENTAL" или "FULL"
}

// ImportDocuments импортирует документы из GCS в Vertex AI Search
// Выполняется асинхронно - возвращает operation name для отслеживания статуса
func (c *VertexAISearchClient) ImportDocuments(ctx context.Context, req *ImportDocumentsRequest) (string, error) {
	if c.projectID == "" || c.location == "" || c.dataStoreID == "" {
		return "", fmt.Errorf("vertex AI search not configured: project, location, or dataStoreID missing")
	}

	if req.GCSPath == "" {
		return "", fmt.Errorf("GCS path is required")
	}

	reconciliationMode := req.ReconciliationMode
	if reconciliationMode == "" {
		reconciliationMode = "INCREMENTAL" // По умолчанию инкрементальный импорт
	}

	// Формируем URL для REST API ImportDocuments
	// Формат: https://discoveryengine.googleapis.com/v1beta/projects/{project}/locations/{location}/collections/{collection}/dataStores/{dataStore}/branches/{branch}/documents:import
	url := fmt.Sprintf(
		"https://discoveryengine.googleapis.com/v1beta/projects/%s/locations/%s/collections/default_collection/dataStores/%s/branches/default_branch/documents:import",
		c.projectID,
		c.location,
		c.dataStoreID,
	)

	requestBody := map[string]interface{}{
		"gcsSource": map[string]interface{}{
			"inputUris": []string{req.GCSPath},
			"dataSchema": "content", // Для plain text файлов
		},
		"reconciliationMode": reconciliationMode,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	} else {
		return "", fmt.Errorf("API key is required for Vertex AI Search")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Goog-User-Project", c.projectID)

	log.Printf("[VertexAISearch] Importing documents from: %s", req.GCSPath)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vertex AI search import API error %d: %s", resp.StatusCode, string(body))
	}

	var importResp map[string]interface{}
	if err := json.Unmarshal(body, &importResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	operationName, ok := importResp["name"].(string)
	if !ok {
		return "", fmt.Errorf("invalid response format: missing operation name")
	}

	log.Printf("[VertexAISearch] Import operation started: %s", operationName)
	return operationName, nil
}

// CheckDocumentStatus проверяет, проиндексирован ли документ в Vertex AI Search
// Использует поиск по URI документа для проверки его наличия в индексе
func (c *VertexAISearchClient) CheckDocumentStatus(ctx context.Context, documentURI string, tenantID string) (bool, error) {
	if c.projectID == "" || c.location == "" || c.dataStoreID == "" {
		return false, fmt.Errorf("vertex AI search not configured: project, location, or dataStoreID missing")
	}

	if documentURI == "" {
		return false, fmt.Errorf("document URI is required")
	}

	// Используем поиск по URI документа
	// Формируем фильтр для поиска конкретного документа
	filter := fmt.Sprintf(`uri:"%s"`, documentURI)
	if tenantID != "" {
		// Добавляем фильтр по tenantId для безопасности
		filter = fmt.Sprintf(`(%s) AND (tenantId:"%s" OR tenantId:"global")`, filter, tenantID)
	}

	searchReq := &SearchRequest{
		Query:    documentURI, // Используем URI как query для поиска
		PageSize: 1,          // Нам нужен только один результат
		Filter:   filter,
	}

	searchResp, err := c.Search(ctx, searchReq)
	if err != nil {
		// Если поиск вернул ошибку, считаем что документ не проиндексирован
		return false, nil
	}

	// Если найден хотя бы один результат с таким URI - документ проиндексирован
	if len(searchResp.Results) > 0 {
		for _, result := range searchResp.Results {
			if result.Document.URI == documentURI {
				return true, nil
			}
		}
	}

	return false, nil
}


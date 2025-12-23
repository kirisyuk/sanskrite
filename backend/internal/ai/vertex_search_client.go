package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
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
	tokenSource oauth2.TokenSource // Для OAuth2 аутентификации
}

// WithDataStore returns a shallow-cloned client targeting a different Data Store ID.
// It reuses the same auth (API key / ADC token source) and HTTP client.
func (c *VertexAISearchClient) WithDataStore(dataStoreID string) *VertexAISearchClient {
	if c == nil {
		return nil
	}
	clone := *c
	clone.dataStoreID = dataStoreID
	return &clone
}

// NewVertexAISearchClient создает новый клиент для Vertex AI Search
// Если apiKey пустой, будет использоваться Application Default Credentials (OAuth2)
func NewVertexAISearchClient(projectID, location, dataStoreID, apiKey string) *VertexAISearchClient {
	client := &VertexAISearchClient{
		projectID:   projectID,
		location:    location,
		dataStoreID: dataStoreID,
		apiKey:      apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Если API ключ не указан, пытаемся использовать Application Default Credentials
	if apiKey == "" {
		ctx := context.Background()
		ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err == nil {
			client.tokenSource = ts
			log.Printf("[VertexAISearch] Using Application Default Credentials for authentication")
		} else {
			log.Printf("[VertexAISearch] Warning: failed to initialize ADC: %v", err)
		}
	}

	return client
}

// SearchRequest параметры поиска
type SearchRequest struct {
	Query               string               `json:"query"`
	PageSize            int                  `json:"pageSize,omitempty"`
	PageToken           string               `json:"pageToken,omitempty"`
	Filter              string               `json:"filter,omitempty"`
	OrderBy             string               `json:"orderBy,omitempty"`
	BoostSpec           *BoostSpec           `json:"boostSpec,omitempty"`
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
	ID             string         `json:"id"`
	Document       SearchDocument `json:"document"`
	RelevanceScore float64        `json:"relevanceScore,omitempty"`
}

// SearchDocument документ из результата поиска
type SearchDocument struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	URI               string                 `json:"uri"`
	Title             string                 `json:"title,omitempty"`
	StructData        map[string]interface{} `json:"structData,omitempty"`
	DerivedStructData map[string]interface{} `json:"derivedStructData,omitempty"` // Автоматически генерируемые данные из GCS
	JSONData          string                 `json:"jsonData,omitempty"`
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
	// Формат: https://discoveryengine.googleapis.com/v1beta/projects/{project}/locations/{location}/collections/default_collection/dataStores/{dataStore}/servingConfigs/default_search:search
	url := fmt.Sprintf(
		"https://discoveryengine.googleapis.com/v1beta/projects/%s/locations/%s/collections/default_collection/dataStores/%s/servingConfigs/default_search:search",
		c.projectID,
		c.location,
		c.dataStoreID,
	)

	requestBody := map[string]interface{}{
		"query":    req.Query,
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

	// Используем API key или OAuth2 токен для аутентификации
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	} else if c.tokenSource != nil {
		// Используем OAuth2 токен из Application Default Credentials
		token, err := c.tokenSource.Token()
		if err != nil {
			return nil, fmt.Errorf("failed to get OAuth2 token: %w", err)
		}
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	} else {
		return nil, fmt.Errorf("authentication required: either API key or Application Default Credentials")
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

	// Логируем сырой ответ для отладки (первые 2000 символов)
	if len(body) > 0 {
		bodyPreview := string(body)
		if len(bodyPreview) > 2000 {
			bodyPreview = bodyPreview[:2000] + "..."
		}
		log.Printf("[VertexAISearch] Raw response preview (first 2000 chars): %s", bodyPreview)
	}

	var searchResp SearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	log.Printf("[VertexAISearch] Found %d results", len(searchResp.Results))
	
	// Логируем структуру первого результата для отладки
	if len(searchResp.Results) > 0 {
		firstResult := searchResp.Results[0]
		log.Printf("[VertexAISearch] Sample result: ID=%s, URI=%s, Title=%s", firstResult.ID, firstResult.Document.URI, firstResult.Document.Title)
		if firstResult.Document.StructData != nil {
			log.Printf("[VertexAISearch] Sample structData keys: %v", getMapKeys(firstResult.Document.StructData))
			// Логируем первые несколько ключей-значений
			count := 0
			for k, v := range firstResult.Document.StructData {
				if count < 5 {
					log.Printf("[VertexAISearch] structData[%s] = %v", k, v)
					count++
				}
			}
		} else {
			log.Printf("[VertexAISearch] Sample result has no structData")
		}
		// Логируем полный JSON первого результата для отладки (первые 500 символов)
		if resultJSON, err := json.Marshal(firstResult); err == nil {
			resultStr := string(resultJSON)
			if len(resultStr) > 500 {
				resultStr = resultStr[:500] + "..."
			}
			log.Printf("[VertexAISearch] Sample result JSON: %s", resultStr)
		}
	}
	
	return &searchResp, nil
}

// getMapKeys возвращает список ключей из map для логирования
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// IsConfigured проверяет, настроен ли клиент
func (c *VertexAISearchClient) IsConfigured() bool {
	return c.projectID != "" && c.location != "" && c.dataStoreID != "" && (c.apiKey != "" || c.tokenSource != nil)
}

// ImportDocumentsRequest параметры для импорта документов
type ImportDocumentsRequest struct {
	GCSPath            string // gs://bucket/path pattern (например, gs://bucket/tenantId/**)
	ReconciliationMode string // "INCREMENTAL" или "FULL"
	DataSchema         string // "content" (plain text) или "document" (JSONL с structData)
}

// ImportDocumentsFromURIs imports a specific list of GCS URIs (NOT a wildcard/pattern).
// This is the recommended mode when you may exceed Discovery Engine's file expansion limits.
// Note: Discovery Engine imposes a hard limit on number of files per import request (e.g. 1000).
func (c *VertexAISearchClient) ImportDocumentsFromURIs(ctx context.Context, inputURIs []string, reconciliationMode, dataSchema string) (string, error) {
	if c.projectID == "" || c.location == "" || c.dataStoreID == "" {
		return "", fmt.Errorf("vertex AI search not configured: project, location, or dataStoreID missing")
	}
	if len(inputURIs) == 0 {
		return "", fmt.Errorf("inputURIs is required")
	}
	if reconciliationMode == "" {
		reconciliationMode = "INCREMENTAL"
	}
	if dataSchema == "" {
		dataSchema = "content"
	}

	url := fmt.Sprintf(
		"https://discoveryengine.googleapis.com/v1beta/projects/%s/locations/%s/collections/default_collection/dataStores/%s/branches/default_branch/documents:import",
		c.projectID,
		c.location,
		c.dataStoreID,
	)

	requestBody := map[string]interface{}{
		"gcsSource": map[string]interface{}{
			"inputUris":  inputURIs,
			"dataSchema": dataSchema,
		},
		"reconciliationMode": reconciliationMode,
	}

	log.Printf("[VertexAISearch] Importing URIs: count=%d dataSchema=%s reconciliationMode=%s", len(inputURIs), dataSchema, reconciliationMode)

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Auth
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	} else if c.tokenSource != nil {
		token, err := c.tokenSource.Token()
		if err != nil {
			return "", fmt.Errorf("failed to get OAuth2 token: %w", err)
		}
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	} else {
		return "", fmt.Errorf("authentication required: either API key or Application Default Credentials")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Goog-User-Project", c.projectID)

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
	if name, ok := importResp["name"].(string); ok && name != "" {
		return name, nil
	}
	return "", fmt.Errorf("unexpected import response: %s", string(body))
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

	dataSchema := req.DataSchema
	if dataSchema == "" {
		dataSchema = "content" // По умолчанию для обратной совместимости
	}

	requestBody := map[string]interface{}{
		"gcsSource": map[string]interface{}{
			"inputUris":  []string{req.GCSPath},
			"dataSchema": dataSchema,
		},
		"reconciliationMode": reconciliationMode,
	}
	
	// Логируем параметры импорта для отладки
	log.Printf("[VertexAISearch] Importing with dataSchema=%s, GCSPath=%s, reconciliationMode=%s", dataSchema, req.GCSPath, reconciliationMode)

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Используем API key или OAuth2 токен для аутентификации
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	} else if c.tokenSource != nil {
		// Используем OAuth2 токен из Application Default Credentials
		token, err := c.tokenSource.Token()
		if err != nil {
			return "", fmt.Errorf("failed to get OAuth2 token: %w", err)
		}
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	} else {
		return "", fmt.Errorf("authentication required: either API key or Application Default Credentials")
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

// CheckDocumentStatus проверяет, доступен ли конкретный документ в Vertex AI Search
// Выполняет поиск с фильтром по tenantId и проверяет, что конкретный URI присутствует в результатах
func (c *VertexAISearchClient) CheckDocumentStatus(ctx context.Context, documentURI string, tenantID string) (bool, error) {
	if c.projectID == "" || c.location == "" || c.dataStoreID == "" {
		return false, fmt.Errorf("vertex AI search not configured: project, location, or dataStoreID missing")
	}

	if documentURI == "" {
		return false, fmt.Errorf("document URI is required")
	}

	// Extract filename from URI for search query
	uriParts := strings.Split(documentURI, "/")
	if len(uriParts) == 0 {
		return false, fmt.Errorf("invalid document URI format")
	}
	filename := uriParts[len(uriParts)-1]
	// Remove .txt extension if present for better search
	searchQuery := strings.TrimSuffix(filename, ".txt")
	if searchQuery == "" {
		searchQuery = filename
	}

	// Perform search with tenant filter to check if the specific document is available
	// Filter by tenantId to ensure we only check documents for this tenant
	filter := fmt.Sprintf(`tenantId:"%s"`, tenantID)
	searchReq := &SearchRequest{
		Query:    searchQuery,
		PageSize: 50, // Get more results to find the specific document
		Filter:   filter,
	}

	searchResp, err := c.Search(ctx, searchReq)
	if err != nil {
		// If search returns an error, document is likely not available
		log.Printf("[VertexAISearch] CheckDocumentStatus search error for URI %s: %v", documentURI, err)
		return false, nil
	}

	// Check if the specific document URI is in the search results
	// This is more accurate than just checking if any results exist
	for _, result := range searchResp.Results {
		if result.Document.URI == documentURI {
			log.Printf("[VertexAISearch] CheckDocumentStatus: found document %s in search results", documentURI)
			return true, nil
		}
		// Also check structData for documentId or storagePath
		if result.Document.StructData != nil {
			if docID, ok := result.Document.StructData["documentId"].(string); ok && docID != "" {
				// Check if URI contains this documentId
				if strings.Contains(documentURI, docID) {
					log.Printf("[VertexAISearch] CheckDocumentStatus: found document %s by documentId %s", documentURI, docID)
					return true, nil
				}
			}
		}
	}

	log.Printf("[VertexAISearch] CheckDocumentStatus: document %s not found in search results (found %d results for query '%s')", documentURI, len(searchResp.Results), searchQuery)
	return false, nil
}

// OperationStatus represents the status of a long-running operation
type OperationStatus struct {
	Done   bool        `json:"done"`
	Error  *Error      `json:"error,omitempty"`
	Result interface{} `json:"response,omitempty"`
}

// Error represents an error in an operation
type Error struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Details []interface{} `json:"details,omitempty"`
}

// CheckOperationStatus проверяет статус операции через Operations API
// operationName format: projects/{project}/locations/{location}/collections/{collection}/dataStores/{dataStore}/branches/{branch}/operations/{operationId}
func (c *VertexAISearchClient) CheckOperationStatus(ctx context.Context, operationName string) (*OperationStatus, error) {
	if c.projectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}

	// Operations API endpoint
	// Format: https://discoveryengine.googleapis.com/v1/{operationName}
	url := fmt.Sprintf("https://discoveryengine.googleapis.com/v1/%s", operationName)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Используем OAuth2 токен для аутентификации
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	} else if c.tokenSource != nil {
		token, err := c.tokenSource.Token()
		if err != nil {
			return nil, fmt.Errorf("failed to get OAuth2 token: %w", err)
		}
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	} else {
		return nil, fmt.Errorf("authentication required: either API key or Application Default Credentials")
	}

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
		return nil, fmt.Errorf("operations API error %d: %s", resp.StatusCode, string(body))
	}

	var status OperationStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &status, nil
}

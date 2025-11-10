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
// Синхронизация данных:
// - Документы автоматически сохраняются в GCS при создании/обновлении через DocumentsRepository
// - Vertex AI Search синхронизирует данные из GCS с настраиваемой частотой:
//   * Ежедневно
//   * Каждые 3 дня
//   * Каждые 5 дней
// - Первая синхронизация происходит ~1 час после создания Data Store
// - Частота синхронизации настраивается при создании Data Store в консоли
// - Дополнительная настройка синхронизации в коде не требуется
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


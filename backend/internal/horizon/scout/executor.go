package scout

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

// SourceExecutor выполняет запросы к источникам данных
type SourceExecutor struct {
	crawler        *crawler.WebCrawler
	rssProvider    *providers.RSSProvider
	telegramClient *telegram.Client
	documentsRepo  *repo.DocumentsRepository
}

// NewSourceExecutor создает новый исполнитель источников
func NewSourceExecutor(
	crawler *crawler.WebCrawler,
	rssProvider *providers.RSSProvider,
	telegramClient *telegram.Client,
	documentsRepo *repo.DocumentsRepository,
) *SourceExecutor {
	return &SourceExecutor{
		crawler:        crawler,
		rssProvider:    rssProvider,
		telegramClient: telegramClient,
		documentsRepo:  documentsRepo,
	}
}

// ExecuteDataSourceResult результат выполнения DataSource
type ExecuteDataSourceResult struct {
	Content     string
	DocumentID  string
	SourceIDs   []string
	DocumentIDs []string
	SourceURL   string // URL источника для Evidence
	SourceHash  string // SHA256 хеш контента для Evidence
	SourceType  string // Тип источника (web, rss, telegram_channel, document)
}

// ExecuteDataSource выполняет запрос к DataSource и возвращает контент
func (e *SourceExecutor) ExecuteDataSource(ctx context.Context, source *models.DataSource) (*ExecuteDataSourceResult, error) {
	switch models.NormalizeSourceKind(source.Kind) {
	case models.SourceKindWeb:
		return e.executeWebSource(ctx, source)
	case models.SourceKindRSS:
		return e.executeRSSSource(ctx, source)
	case models.SourceKindTelegramChannel:
		return e.executeTelegramSource(ctx, source)
	case "document":
		return e.executeDocumentSource(ctx, source)
	case models.SourceKindHorizonFeed:
		return e.executeHorizonMonitorFeedSource(ctx, source)
	case models.SourceKindLLMSearchGrounding:
		// Quasi-source: not executable; content already in ontology facts from Gemini grounding.
		return &ExecuteDataSourceResult{SourceIDs: []string{source.ID}, SourceType: models.SourceTypeLLMSearchGrounding}, nil
	default:
		return nil, fmt.Errorf("unsupported source kind: %s", source.Kind)
	}
}

// executeWebSource выполняет запрос к web источнику через crawler
func (e *SourceExecutor) executeWebSource(ctx context.Context, source *models.DataSource) (*ExecuteDataSourceResult, error) {
	if source.Details.URI == nil {
		return nil, fmt.Errorf("web source missing URI")
	}
	result, err := e.crawler.FetchContent(ctx, *source.Details.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch web content: %w", err)
	}

	// Сохраняем как Document для прослеживаемости
	document := &models.Document{
		TenantID:     source.TenantID,
		SourceID:     source.ID,
		URL:          *source.Details.URI,
		FullText:     result.Text,
		ContentType:  result.ContentType,
		Status:       "new",
		DocumentPath: fmt.Sprintf("web/%s", source.ID),
	}
	documentID, err := e.documentsRepo.Create(ctx, document)
	if err != nil {
		return nil, fmt.Errorf("failed to create document: %w", err)
	}

	// Вычисляем хеш контента для Evidence
	hash := sha256.Sum256([]byte(result.Text))
	hashStr := fmt.Sprintf("sha256:%x", hash)

	return &ExecuteDataSourceResult{
		Content:     result.Text,
		DocumentID:  documentID,
		SourceIDs:   []string{source.ID},
		DocumentIDs: []string{documentID},
		SourceURL:   *source.Details.URI,
		SourceHash:  hashStr,
		SourceType:  "web",
	}, nil
}

// executeRSSSource выполняет запрос к RSS источнику через RSS Provider
func (e *SourceExecutor) executeRSSSource(ctx context.Context, source *models.DataSource) (*ExecuteDataSourceResult, error) {
	if e.rssProvider == nil {
		return nil, fmt.Errorf("RSS provider not configured")
	}
	if source.Details.RSSURL == nil {
		return nil, fmt.Errorf("RSS source missing RSSURL")
	}

	// Используем RSS Provider для получения контента
	results, err := e.rssProvider.Ground(ctx, source.TenantID, source.ID, []string{*source.Details.RSSURL})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch RSS content: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no RSS content found")
	}

	// Агрегируем контент из всех результатов
	var contentParts []string
	var documentIDs []string
	for _, result := range results {
		if result.FullText != "" {
			contentParts = append(contentParts, result.FullText)
		}
		// Note: NormalizedDocument doesn't have ID field, documents are created separately
		// DocumentIDs will be populated when documents are created
	}

	content := strings.Join(contentParts, "\n\n")
	if content == "" {
		return nil, fmt.Errorf("RSS source returned empty content")
	}

	// Вычисляем хеш контента для Evidence
	hash := sha256.Sum256([]byte(content))
	hashStr := fmt.Sprintf("sha256:%x", hash)

	sourceURL := ""
	if source.Details.RSSURL != nil {
		sourceURL = *source.Details.RSSURL
	}

	return &ExecuteDataSourceResult{
		Content: content,
		// Use first document if present; RSS normalization may not always create documents.
		DocumentID: func() string {
			if len(documentIDs) > 0 {
				return documentIDs[0]
			}
			return ""
		}(),
		SourceIDs:   []string{source.ID},
		DocumentIDs: documentIDs,
		SourceURL:   sourceURL,
		SourceHash:  hashStr,
		SourceType:  "rss",
	}, nil
}

// executeTelegramSource выполняет запрос к Telegram каналу
func (e *SourceExecutor) executeTelegramSource(ctx context.Context, source *models.DataSource) (*ExecuteDataSourceResult, error) {
	if e.telegramClient == nil {
		return nil, fmt.Errorf("Telegram client not configured")
	}
	if source.Details.ChannelUsername == nil {
		return nil, fmt.Errorf("Telegram source missing ChannelUsername")
	}

	// Получаем последние сообщения из канала
	messages, err := e.telegramClient.FetchUpdates(ctx, *source.Details.ChannelUsername, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Telegram updates: %w", err)
	}

	// Агрегируем текст сообщений
	var contentParts []string
	for _, msg := range messages {
		if msg.Text != "" {
			contentParts = append(contentParts, msg.Text)
		}
	}

	content := strings.Join(contentParts, "\n\n")
	if content == "" {
		return nil, fmt.Errorf("Telegram source returned empty content")
	}

	// Сохраняем как Document
	document := &models.Document{
		TenantID:     source.TenantID,
		SourceID:     source.ID,
		FullText:     content,
		ContentType:  "text/plain",
		Status:       "new",
		DocumentPath: fmt.Sprintf("telegram/%s", source.ID),
	}
	documentID, err := e.documentsRepo.Create(ctx, document)
	if err != nil {
		return nil, fmt.Errorf("failed to create document: %w", err)
	}

	// Вычисляем хеш контента для Evidence
	hash := sha256.Sum256([]byte(content))
	hashStr := fmt.Sprintf("sha256:%x", hash)

	// Для Telegram источника URL формируется из канала
	sourceURL := fmt.Sprintf("https://t.me/%s", *source.Details.ChannelUsername)

	return &ExecuteDataSourceResult{
		Content:     content,
		DocumentID:  documentID,
		SourceIDs:   []string{source.ID},
		DocumentIDs: []string{documentID},
		SourceURL:   sourceURL,
		SourceHash:  hashStr,
		SourceType:  "telegram_channel",
	}, nil
}

// executeDocumentSource выполняет обработку документа через DocumentsRepository
func (e *SourceExecutor) executeDocumentSource(ctx context.Context, source *models.DataSource) (*ExecuteDataSourceResult, error) {
	if e.documentsRepo == nil {
		return nil, fmt.Errorf("Documents repository not configured")
	}
	// Для document источника нужно получить документ по sourceID
	// Получаем последний документ для этого источника
	docs, err := e.documentsRepo.ListBySource(ctx, source.TenantID, source.ID, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to get document: %w", err)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no documents found for source")
	}
	document := docs[0]

	// Используем FullText (ExtractedText может быть недоступен в этой версии модели)
	content := document.FullText

	if content == "" {
		return nil, fmt.Errorf("document has no extractable text")
	}

	// Вычисляем хеш контента для Evidence
	hash := sha256.Sum256([]byte(content))
	hashStr := fmt.Sprintf("sha256:%x", hash)

	sourceURL := ""
	if document.URL != "" {
		sourceURL = document.URL
	} else if source.Details.URI != nil {
		sourceURL = *source.Details.URI
	}

	return &ExecuteDataSourceResult{
		Content:     content,
		DocumentID:  document.ID,
		SourceIDs:   []string{source.ID},
		DocumentIDs: []string{document.ID},
		SourceURL:   sourceURL,
		SourceHash:  hashStr,
		SourceType:  "document",
	}, nil
}

func (e *SourceExecutor) executeHorizonMonitorFeedSource(ctx context.Context, source *models.DataSource) (*ExecuteDataSourceResult, error) {
	if source.Details.URI == nil || strings.TrimSpace(*source.Details.URI) == "" {
		return nil, fmt.Errorf("horizon_monitor_feed source missing URI")
	}
	if e.documentsRepo == nil {
		return nil, fmt.Errorf("Documents repository not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(*source.Details.URI), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	for k, v := range source.Details.RequestHeaders {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch horizon monitor feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("horizon monitor feed returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("failed reading horizon monitor feed body: %w", err)
	}

	content := strings.TrimSpace(string(body))
	if content == "" {
		return nil, fmt.Errorf("horizon monitor feed returned empty content")
	}

	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			if pretty, merr := json.MarshalIndent(parsed, "", "  "); merr == nil {
				content = string(pretty)
			}
		}
	}

	now := time.Now().UTC()
	document := &models.Document{
		TenantID:     source.TenantID,
		SourceID:     source.ID,
		URL:          strings.TrimSpace(*source.Details.URI),
		Title:        source.DisplayName,
		FullText:     content,
		ContentType:  "application/json",
		DocumentType: "feed_snapshot",
		Status:       "new",
		DocumentPath: fmt.Sprintf("horizon-monitor/%s/%s", source.ID, now.Format("20060102T150405")),
	}
	documentID, err := e.documentsRepo.Create(ctx, document)
	if err != nil {
		return nil, fmt.Errorf("failed to create document: %w", err)
	}

	hash := sha256.Sum256([]byte(content))
	hashStr := fmt.Sprintf("sha256:%x", hash)

	return &ExecuteDataSourceResult{
		Content:     content,
		DocumentID:  documentID,
		SourceIDs:   []string{source.ID},
		DocumentIDs: []string{documentID},
		SourceURL:   strings.TrimSpace(*source.Details.URI),
		SourceHash:  hashStr,
		SourceType:  "horizon_monitor_feed",
	}, nil
}

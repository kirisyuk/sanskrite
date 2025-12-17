package scout

import (
	"context"
	"fmt"
	"strings"

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
}

// ExecuteDataSource выполняет запрос к DataSource и возвращает контент
func (e *SourceExecutor) ExecuteDataSource(ctx context.Context, source *models.DataSource) (*ExecuteDataSourceResult, error) {
	switch source.Kind {
	case "web":
		return e.executeWebSource(ctx, source)
	case "rss":
		return e.executeRSSSource(ctx, source)
	case "telegram_channel":
		return e.executeTelegramSource(ctx, source)
	case "document":
		return e.executeDocumentSource(ctx, source)
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
		TenantID:    source.TenantID,
		SourceID:    source.ID,
		URL:         *source.Details.URI,
		FullText:    result.Text,
		ContentType: result.ContentType,
		Status:      "new",
		DocumentPath: fmt.Sprintf("web/%s", source.ID),
	}
	documentID, err := e.documentsRepo.Create(ctx, document)
	if err != nil {
		return nil, fmt.Errorf("failed to create document: %w", err)
	}

	return &ExecuteDataSourceResult{
		Content:     result.Text,
		DocumentID:  documentID,
		SourceIDs:   []string{source.ID},
		DocumentIDs: []string{documentID},
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

	return &ExecuteDataSourceResult{
		Content:     content,
		DocumentID:  documentIDs[0], // Используем первый документ как основной
		SourceIDs:   []string{source.ID},
		DocumentIDs: documentIDs,
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

	return &ExecuteDataSourceResult{
		Content:     content,
		DocumentID:  documentID,
		SourceIDs:   []string{source.ID},
		DocumentIDs: []string{documentID},
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

	return &ExecuteDataSourceResult{
		Content:     content,
		DocumentID:  document.ID,
		SourceIDs:   []string{source.ID},
		DocumentIDs: []string{document.ID},
	}, nil
}


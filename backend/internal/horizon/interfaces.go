package horizon

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// HorizonServiceDependencies содержит зависимости от основной системы
type HorizonServiceDependencies struct {
	// AI система
	AIExecutor       *ai.AITaskExecutor
	EmbeddingProvider ai.EmbeddingProvider
	VertexSearchClient *ai.VertexAISearchClient

	// Agent система
	AgentRegistry  agents.Registry
	AgentJobsRepo *repo.AgentJobsRepository

	// Realtime система
	RealtimePublisher *realtime.Publisher

	// Репозитории основной системы (для связи с Entity, Document)
	EntitiesRepo   *repo.EntityRepository
	DocumentsRepo  *repo.DocumentsRepository
	ChunksRepo     *repo.ChunksRepository
	DataSourcesRepo *repo.DataSourcesRepository

	// Другие зависимости
	Crawler CrawlerInterface // Абстракция над internal/crawler
	Logger  LoggerInterface
}

// CrawlerInterface абстракция над crawler для обработки web источников
type CrawlerInterface interface {
	FetchContent(ctx context.Context, url string) (*crawler.FetchResult, error)
}

// LoggerInterface абстракция для логирования
type LoggerInterface interface {
	Printf(format string, v ...interface{})
}


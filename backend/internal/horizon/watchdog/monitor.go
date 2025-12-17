package watchdog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// Monitor осуществляет мониторинг источников и обнаружение изменений
type Monitor struct {
	dataSourcesRepo *repo.DataSourcesRepository
	documentsRepo   *repo.DocumentsRepository
	fragmentsRepo   *horizonrepo.HorizonFragmentsRepository
	scoutEngine     *scout.ScoutEngine
	executor        *scout.SourceExecutor
	changeAssessor  *ChangeAssessor
}

// NewMonitor создает новый мониторинг
func NewMonitor(
	dataSourcesRepo *repo.DataSourcesRepository,
	documentsRepo *repo.DocumentsRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	scoutEngine *scout.ScoutEngine,
	executor *scout.SourceExecutor,
	aiExecutor *ai.AITaskExecutor,
) *Monitor {
	return &Monitor{
		dataSourcesRepo: dataSourcesRepo,
		documentsRepo:   documentsRepo,
		fragmentsRepo:   fragmentsRepo,
		scoutEngine:     scoutEngine,
		executor:        executor,
		changeAssessor:  NewChangeAssessor(aiExecutor),
	}
}

// ScanSources сканирует источники для WatchdogConfig
func (m *Monitor) ScanSources(ctx context.Context, config *horizonmodels.WatchdogConfig) error {
	// Получаем источники для сканирования
	var sources []*models.DataSource
	var err error

	if config.UseProjectSources {
		// Получаем все источники проекта через fragment
		fragment, err := horizonrepo.NewHorizonFragmentsRepository(nil).Get(ctx, config.TargetFragmentID)
		if err == nil && fragment.Governance.CreatedByProjectID != nil {
			sources, err = m.dataSourcesRepo.ListByProject(ctx, "global", *fragment.Governance.CreatedByProjectID)
			if err != nil {
				return fmt.Errorf("failed to get project sources: %w", err)
			}
		}
	} else {
		sources, err = m.getSourcesByIDs(ctx, config.SourceIDs)
		if err != nil {
			return fmt.Errorf("failed to get sources: %w", err)
		}
	}

	// Сканируем каждый источник
	for _, source := range sources {
		if err := m.scanSource(ctx, source, config); err != nil {
			// Логируем ошибку, но продолжаем сканирование других источников
			_ = err
		}
	}

	return nil
}

// scanSource сканирует один источник и проверяет изменения
func (m *Monitor) scanSource(ctx context.Context, source *models.DataSource, config *horizonmodels.WatchdogConfig) error {
	// Получаем контент источника через SourceExecutor
	result, err := m.executor.ExecuteDataSource(ctx, source)
	if err != nil {
		return fmt.Errorf("failed to fetch content: %w", err)
	}

	if result.Content == "" {
		return nil // Нет контента для проверки
	}

	// Вычисляем хеш контента
	contentHash := m.calculateContentHash(result.Content)

	// Получаем предыдущий хеш из последнего Document источника
	previousHash := m.getPreviousHash(ctx, source.ID)

	// Сравниваем хеши
	if contentHash == previousHash {
		return nil // Изменений нет
	}

	// Вычисляем степень изменения через LLM
	oldContent := ""
	if previousHash != "" {
		// Получаем старый документ для сравнения
		oldDocs, err := m.documentsRepo.ListBySource(ctx, "", source.ID, 1)
		if err == nil && len(oldDocs) > 0 {
			oldContent = oldDocs[0].FullText
		}
	}

	// Получаем targetCanonicalPath из fragment для контекста
	targetCanonicalPath := ""
	if fragment, err := m.fragmentsRepo.Get(ctx, config.TargetFragmentID); err == nil {
		targetCanonicalPath = fragment.CanonicalPath
	}

	changeAmount, err := m.changeAssessor.AssessChangeAmount(ctx, oldContent, result.Content, targetCanonicalPath)
	if err != nil {
		// Fallback на простую проверку при ошибке LLM
		if oldContent == "" {
			changeAmount = 1.0
		} else {
			changeAmount = 0.5 // Среднее значение при ошибке
		}
	}

	if changeAmount < config.TriggerThreshold {
		// Изменение незначительно, обновляем хеш но не создаем запрос
		m.saveHash(ctx, source.ID, contentHash, result.DocumentID)
		return nil
	}

	// Изменение превышает порог, создаем ScoutRequest
	if err := m.createScoutRequest(ctx, config, source); err != nil {
		return fmt.Errorf("failed to create scout request: %w", err)
	}

	// Сохраняем новый хеш
	m.saveHash(ctx, source.ID, contentHash, result.DocumentID)

	return nil
}

// calculateContentHash вычисляет SHA256 хеш контента
func (m *Monitor) calculateContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// getPreviousHash получает предыдущий хеш источника из последнего Document
func (m *Monitor) getPreviousHash(ctx context.Context, sourceID string) string {
	// Получаем последний документ источника (tenantID может быть пустым для публичных источников)
	docs, err := m.documentsRepo.ListBySource(ctx, "", sourceID, 1)
	if err != nil || len(docs) == 0 {
		return ""
	}
	// Используем ContentHash из Document
	return docs[0].ContentHash
}

// saveHash сохраняет хеш источника через обновление Document
func (m *Monitor) saveHash(ctx context.Context, sourceID string, hash string, documentID string) {
	if documentID == "" {
		return
	}
	// Хеш уже сохранен в Document при создании через SourceExecutor
	// Дополнительных действий не требуется
}


// createScoutRequest создает запрос на разведку через Scout Engine
func (m *Monitor) createScoutRequest(ctx context.Context, config *horizonmodels.WatchdogConfig, source *models.DataSource) error {
	if m.scoutEngine == nil {
		return fmt.Errorf("scout engine not configured")
	}

	// Получаем fragment для определения targetCanonicalPath
		fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(nil)
	fragment, err := fragmentsRepo.Get(ctx, config.TargetFragmentID)
	if err != nil {
		return fmt.Errorf("failed to get fragment: %w", err)
	}

	// Создаем ScoutingJob для обновления fragment
	scoutingJob := &horizonmodels.ScoutingJob{
		AgentJob: models.AgentJob{
			TenantID:  fragment.TenantID,
			ProjectID: *fragment.Governance.CreatedByProjectID,
			Status:    "pending",
			AgentID:   "scout",
			Task: map[string]interface{}{
				"targetCanonicalPath": fragment.CanonicalPath,
				"sourceIds":           []string{source.ID},
				"scoutingStrategy":    "Snapshot",
			},
			State: map[string]interface{}{
				"targetCanonicalPath": fragment.CanonicalPath,
				"lensId":              "",
				"currentState":        string(horizonmodels.ScoutingJobStatePending),
			},
		},
		TargetCanonicalPath: fragment.CanonicalPath,
		SourceIDs:           []string{source.ID},
		ScoutingStrategy:    string(horizonmodels.ScoutingStrategySnapshot),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelAccess),
			ProjectID:   *fragment.Governance.CreatedByProjectID,
		},
		CurrentState: horizonmodels.ScoutingJobStatePending,
	}

	// Запускаем ScoutingJob через Scout Engine
	return m.scoutEngine.ExecuteScoutingJob(ctx, scoutingJob)
}

// getSourcesByIDs получает источники по ID
func (m *Monitor) getSourcesByIDs(ctx context.Context, sourceIDs []string) ([]*models.DataSource, error) {
	var sources []*models.DataSource
	for _, id := range sourceIDs {
		source, err := m.dataSourcesRepo.Get(ctx, id)
		if err != nil {
			continue // Пропускаем недоступные источники
		}
		sources = append(sources, source)
	}
	return sources, nil
}

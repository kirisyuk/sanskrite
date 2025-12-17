package watchdog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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
}

// NewMonitor создает новый мониторинг
func NewMonitor(
	dataSourcesRepo *repo.DataSourcesRepository,
	documentsRepo *repo.DocumentsRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	scoutEngine *scout.ScoutEngine,
	executor *scout.SourceExecutor,
) *Monitor {
	return &Monitor{
		dataSourcesRepo: dataSourcesRepo,
		documentsRepo:   documentsRepo,
		fragmentsRepo:   fragmentsRepo,
		scoutEngine:     scoutEngine,
		executor:        executor,
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

	// Вычисляем степень изменения на основе сравнения документов
	changeAmount := m.calculateChangeAmount(ctx, source.ID, result.Content, previousHash)
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

// calculateChangeAmount вычисляет степень изменения на основе сравнения документов
func (m *Monitor) calculateChangeAmount(ctx context.Context, sourceID string, newContent string, oldHash string) float64 {
	if oldHash == "" {
		return 1.0 // Первое сканирование - считаем максимальным изменением
	}

	// Получаем старый документ для сравнения (tenantID может быть пустым для публичных источников)
	oldDocs, err := m.documentsRepo.ListBySource(ctx, "", sourceID, 1)
	if err != nil || len(oldDocs) == 0 {
		return 1.0 // Если не можем найти старый документ, считаем максимальным изменением
	}

	oldContent := oldDocs[0].FullText
	// ExtractedText может быть недоступен в этой версии модели, используем FullText

	// Вычисляем степень изменения через сравнение длины и содержимого
	// Упрощенный алгоритм: используем отношение различий к общей длине
	oldLen := len(oldContent)
	newLen := len(newContent)
	if oldLen == 0 && newLen == 0 {
		return 0.0
	}
	if oldLen == 0 || newLen == 0 {
		return 1.0
	}

	// Вычисляем процент различий через простое сравнение строк
	// В реальной реализации можно использовать более сложные алгоритмы (diff, семантическое сравнение)
	diff := 0
	maxLen := oldLen
	if newLen > maxLen {
		maxLen = newLen
	}
	minLen := oldLen
	if newLen < minLen {
		minLen = newLen
	}

	// Простое сравнение по символам
	for i := 0; i < minLen; i++ {
		if oldContent[i] != newContent[i] {
			diff++
		}
	}
	diff += maxLen - minLen

	// Нормализуем к [0..1]
	changeAmount := float64(diff) / float64(maxLen)
	if changeAmount > 1.0 {
		changeAmount = 1.0
	}

	return changeAmount
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

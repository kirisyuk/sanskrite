package watchdog

import (
	"context"
	"fmt"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
)

// WatchdogService осуществляет фоновый мониторинг источников
type WatchdogService struct {
	configRepo    *repo.WatchdogConfigsRepository
	fragmentsRepo *repo.HorizonFragmentsRepository
	monitor       *Monitor
	scheduler     *Scheduler
	scoutEngine   *scout.ScoutEngine
}

// ScheduledScanStats - метрики выполнения одного прогона Watchdog
type ScheduledScanStats struct {
	TenantID         string `json:"tenantId"`
	Limit            int    `json:"limit"`
	ConfigsFetched   int    `json:"configsFetched"`
	ConfigsScanned   int    `json:"configsScanned"`
	ScanErrors       int    `json:"scanErrors"`
	ScanTimeUpdates  int    `json:"scanTimeUpdates"`
	StartedAtUnixMs  int64  `json:"startedAtUnixMs"`
	FinishedAtUnixMs int64  `json:"finishedAtUnixMs"`
	DurationMs       int64  `json:"durationMs"`
}

// NewWatchdogService создает новый Watchdog Service
func NewWatchdogService(
	configRepo *repo.WatchdogConfigsRepository,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	monitor *Monitor,
	scheduler *Scheduler,
	scoutEngine *scout.ScoutEngine,
) *WatchdogService {
	return &WatchdogService{
		configRepo:    configRepo,
		fragmentsRepo: fragmentsRepo,
		monitor:       monitor,
		scheduler:     scheduler,
		scoutEngine:   scoutEngine,
	}
}

// StartMonitoring запускает мониторинг для WatchdogConfig
func (s *WatchdogService) StartMonitoring(ctx context.Context, configID string) error {
	config, err := s.configRepo.Get(ctx, configID)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	config.Status = string(models.WatchdogStatusRunning)
	return s.configRepo.Update(ctx, config)
}

// StopMonitoring останавливает мониторинг для WatchdogConfig
func (s *WatchdogService) StopMonitoring(ctx context.Context, configID string) error {
	config, err := s.configRepo.Get(ctx, configID)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	config.Status = string(models.WatchdogStatusPaused)
	return s.configRepo.Update(ctx, config)
}

// ProcessScheduledScans обрабатывает запланированные сканирования
func (s *WatchdogService) ProcessScheduledScans(ctx context.Context, limit int) error {
	_, err := s.ProcessScheduledScansWithStats(ctx, limit)
	return err
}

// ProcessScheduledScansWithStats обрабатывает запланированные сканирования и возвращает статистику.
// Используется для Cloud Scheduler/cron endpoint.
func (s *WatchdogService) ProcessScheduledScansWithStats(ctx context.Context, limit int) (*ScheduledScanStats, error) {
	started := time.Now().UTC()
	stats := &ScheduledScanStats{
		TenantID:        "global",
		Limit:           limit,
		StartedAtUnixMs: started.UnixMilli(),
	}

	// Hard budget per run to prevent cost blowups (algorithmic, not env-driven).
	// Tuning can later move into WatchdogConfig or a dedicated policy doc.
	budget := NewRunBudget(8 /*embedding pairs*/, 3 /*LLM assessments*/)
	logf("phase=run_start limit=%d budget.embedPairs=%d budget.llm=%d", limit, 8, 3)

	configs, err := s.configRepo.ListScheduled(ctx, "global", limit)
	if err != nil {
		logf("phase=list_scheduled error=%v", err)
		return stats, fmt.Errorf("failed to list scheduled configs: %w", err)
	}
	stats.ConfigsFetched = len(configs)
	logf("phase=list_scheduled fetched=%d", stats.ConfigsFetched)

	for _, config := range configs {
		stats.ConfigsScanned++
		logf("phase=config_start configId=%s targetFragmentId=%s triggerThreshold=%.4f scanInterval=%s useProjectSources=%v sourceIds=%d",
			config.ID, config.TargetFragmentID, config.TriggerThreshold, config.ScanInterval, config.UseProjectSources, len(config.SourceIDs))
		if err := s.monitor.ScanSources(ctx, config, budget); err != nil {
			// Логируем ошибку, но продолжаем обработку других конфигураций
			_ = err
			stats.ScanErrors++
			logf("phase=config_error configId=%s error=%v", config.ID, err)
		}

		// Обновляем время следующего сканирования
		nextScanAt, err := s.scheduler.CalculateNextScanTime(config.ScanInterval, time.Now())
		if err == nil {
			if updateErr := s.configRepo.UpdateScanTime(ctx, config.ID, time.Now(), nextScanAt); updateErr == nil {
				stats.ScanTimeUpdates++
			}
		}
		logf("phase=config_done configId=%s", config.ID)
	}

	finished := time.Now().UTC()
	stats.FinishedAtUnixMs = finished.UnixMilli()
	stats.DurationMs = finished.Sub(started).Milliseconds()
	ep, llm := budget.Snapshot()
	logf("phase=run_done fetched=%d scanned=%d scanErrors=%d scanTimeUpdates=%d durationMs=%d used.embedPairs=%d used.llm=%d",
		stats.ConfigsFetched, stats.ConfigsScanned, stats.ScanErrors, stats.ScanTimeUpdates, stats.DurationMs, ep, llm)
	return stats, nil
}

// CreateConfigForFragment создает WatchdogConfig для HorizonFragment
func (s *WatchdogService) CreateConfigForFragment(ctx context.Context, fragmentID string, scanInterval string, triggerThreshold float64, sourceIDs []string) (string, error) {
	config := &models.WatchdogConfig{
		TenantID:         "global",
		TargetFragmentID: fragmentID,
		ScanInterval:     scanInterval,
		SourceIDs:        sourceIDs,
		TriggerThreshold: triggerThreshold,
		Status:           string(models.WatchdogStatusRunning),
	}

	now := time.Now()
	nextScanAt, err := s.scheduler.CalculateNextScanTime(scanInterval, now)
	if err == nil {
		config.NextScanAt = &nextScanAt
	}

	return s.configRepo.Create(ctx, config)
}

// UpdateConfigLifecycle обновляет жизненный цикл конфигурации на основе статуса фрагмента
func (s *WatchdogService) UpdateConfigLifecycle(ctx context.Context, fragmentID string) error {
	fragment, err := s.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return fmt.Errorf("failed to get fragment: %w", err)
	}

	config, err := s.configRepo.GetByTargetFragmentID(ctx, "global", fragmentID)
	if err != nil {
		// Конфигурация не существует, создаем если фрагмент активен
		if fragment.Governance.Status == string(models.FragmentStatusActive) {
			_, _ = s.CreateConfigForFragment(ctx, fragmentID, "PT1H", 0.1, []string{})
		}
		return nil
	}

	// Управляем статусом на основе статуса фрагмента
	if fragment.Governance.Status == string(models.FragmentStatusActive) {
		if config.Status != string(models.WatchdogStatusRunning) {
			return s.StartMonitoring(ctx, config.ID)
		}
	} else {
		if config.Status != string(models.WatchdogStatusPaused) {
			return s.StopMonitoring(ctx, config.ID)
		}
	}

	return nil
}


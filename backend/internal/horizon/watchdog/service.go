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
	configs, err := s.configRepo.ListScheduled(ctx, "global", limit)
	if err != nil {
		return fmt.Errorf("failed to list scheduled configs: %w", err)
	}

	for _, config := range configs {
		if err := s.monitor.ScanSources(ctx, config); err != nil {
			// Логируем ошибку, но продолжаем обработку других конфигураций
			_ = err
			continue
		}

		// Обновляем время следующего сканирования
		nextScanAt, err := s.scheduler.CalculateNextScanTime(config.ScanInterval, time.Now())
		if err == nil {
			_ = s.configRepo.UpdateScanTime(ctx, config.ID, time.Now(), nextScanAt)
		}
	}

	return nil
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


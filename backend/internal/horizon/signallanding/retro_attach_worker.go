package signallanding

import (
	"context"
	"log"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type RetroAttachWorker struct {
	queueRepo      *horizonrepo.SignalRetroAttachQueueRepository
	fragmentsRepo  *horizonrepo.HorizonFragmentsRepository
	service        *Service
	logger         *log.Logger
	maturityWindow time.Duration
	interval       time.Duration
	limit          int
	onAttach       func(context.Context, []string) error
}

func NewRetroAttachWorker(
	queueRepo *horizonrepo.SignalRetroAttachQueueRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	service *Service,
) *RetroAttachWorker {
	return &RetroAttachWorker{
		queueRepo:      queueRepo,
		fragmentsRepo:  fragmentsRepo,
		service:        service,
		maturityWindow: 3 * time.Minute,
		interval:       2 * time.Minute,
		limit:          24,
		logger:         log.Default(),
	}
}

func (w *RetroAttachWorker) WithLogger(logger *log.Logger) *RetroAttachWorker {
	if logger != nil {
		w.logger = logger
	}
	return w
}

func (w *RetroAttachWorker) WithWarmupHook(hook func(context.Context, []string) error) *RetroAttachWorker {
	w.onAttach = hook
	return w
}

func (w *RetroAttachWorker) EnqueueFragment(ctx context.Context, fragment *horizonmodels.HorizonFragment, source string) {
	if w == nil || w.queueRepo == nil || fragment == nil || !eligibleRetroAttachFragment(fragment) {
		return
	}
	item := &horizonmodels.SignalRetroAttachQueueItem{
		TenantID:      strings.TrimSpace(firstNonEmpty(fragment.TenantID, "global")),
		FragmentID:    strings.TrimSpace(fragment.ID),
		CanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
		FragmentType:  fragment.Type,
		Source:        strings.TrimSpace(source),
	}
	if err := w.queueRepo.UpsertObservation(ctx, item, w.maturityWindow); err != nil && w.logger != nil {
		w.logger.Printf("retro attach enqueue failed path=%s err=%v", strings.TrimSpace(fragment.CanonicalPath), err)
	}
}

func (w *RetroAttachWorker) Start(ctx context.Context) {
	if w == nil || w.queueRepo == nil || w.fragmentsRepo == nil || w.service == nil {
		return
	}
	interval := w.interval
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.processOnce(ctx)
			}
		}
	}()
}

func (w *RetroAttachWorker) processOnce(ctx context.Context) {
	items, err := w.queueRepo.ListReady(ctx, w.limit, time.Now().UTC())
	if err != nil {
		if w.logger != nil {
			w.logger.Printf("retro attach list ready failed: %v", err)
		}
		return
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		_ = w.queueRepo.UpdateStatus(ctx, item.ID, horizonmodels.SignalRetroAttachStatusProcessing, item.AttemptCount+1, "", "")
		fragment, err := w.loadFragment(ctx, item)
		if err != nil || fragment == nil {
			_ = w.queueRepo.UpdateStatus(ctx, item.ID, horizonmodels.SignalRetroAttachStatusFailed, item.AttemptCount+1, "", firstNonEmpty(errString(err), "fragment load failed"))
			continue
		}
		result, err := w.service.RetroAttachFragment(ctx, fragment)
		if err != nil {
			_ = w.queueRepo.UpdateStatus(ctx, item.ID, horizonmodels.SignalRetroAttachStatusFailed, item.AttemptCount+1, "", err.Error())
			continue
		}
		status := horizonmodels.SignalRetroAttachStatusIgnored
		lastResult := "no active signal landing match"
		if result != nil && len(result.AttachedSessionIDs) > 0 {
			status = horizonmodels.SignalRetroAttachStatusAttached
			lastResult = "attached to active signal sessions"
			if w.onAttach != nil && len(result.WarmupRefs) > 0 {
				if err := w.onAttach(ctx, result.WarmupRefs); err != nil && w.logger != nil {
					w.logger.Printf("retro attach warmup hook failed refs=%d err=%v", len(result.WarmupRefs), err)
				}
			}
		}
		_ = w.queueRepo.UpdateStatus(ctx, item.ID, status, item.AttemptCount+1, lastResult, "")
	}
}

func (w *RetroAttachWorker) loadFragment(ctx context.Context, item *horizonmodels.SignalRetroAttachQueueItem) (*horizonmodels.HorizonFragment, error) {
	if item == nil || w.fragmentsRepo == nil {
		return nil, nil
	}
	if strings.TrimSpace(item.FragmentID) != "" {
		if fragment, err := w.fragmentsRepo.Get(ctx, item.FragmentID); err == nil && fragment != nil {
			return fragment, nil
		}
	}
	if strings.TrimSpace(item.CanonicalPath) != "" {
		if fragment, err := w.fragmentsRepo.GetByCanonicalPathForTenant(ctx, item.TenantID, item.CanonicalPath); err == nil && fragment != nil {
			return fragment, nil
		}
		return w.fragmentsRepo.GetByCanonicalPath(ctx, item.CanonicalPath)
	}
	return nil, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

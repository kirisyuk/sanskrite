package importance

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type RuntimeUpdater struct {
	tenantID      string
	scorer        *Scorer
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository
	queue         chan string

	mu      sync.Mutex
	pending map[string]struct{}
}

func NewRuntimeUpdater(tenantID string, scorer *Scorer, fragmentsRepo *horizonrepo.HorizonFragmentsRepository, queueSize int) *RuntimeUpdater {
	if queueSize <= 0 {
		queueSize = 1024
	}
	return &RuntimeUpdater{
		tenantID:      strings.TrimSpace(tenantID),
		scorer:        scorer,
		fragmentsRepo: fragmentsRepo,
		queue:         make(chan string, queueSize),
		pending:       map[string]struct{}{},
	}
}

func (u *RuntimeUpdater) Start(ctx context.Context, workers int) {
	if u == nil || u.scorer == nil || u.fragmentsRepo == nil {
		return
	}
	if workers <= 0 {
		workers = 2
	}
	for i := 0; i < workers; i++ {
		go u.worker(ctx)
	}
}

func (u *RuntimeUpdater) Enqueue(fragmentID string) {
	if u == nil {
		return
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return
	}

	u.mu.Lock()
	if _, ok := u.pending[fragmentID]; ok {
		u.mu.Unlock()
		return
	}
	u.pending[fragmentID] = struct{}{}
	u.mu.Unlock()

	select {
	case u.queue <- fragmentID:
	default:
		u.finish(fragmentID)
		log.Printf("[importance.runtime] queue full, dropping fragmentId=%s", fragmentID)
	}
}

func (u *RuntimeUpdater) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case fragmentID := <-u.queue:
			u.handle(ctx, fragmentID)
		}
	}
}

func (u *RuntimeUpdater) handle(ctx context.Context, fragmentID string) {
	defer u.finish(fragmentID)

	hctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	score, frag, err := u.scorer.ScoreFragment(hctx, u.tenantID, fragmentID)
	if err != nil || frag == nil {
		if err != nil {
			log.Printf("[importance.runtime] score failed fragmentId=%s err=%v", fragmentID, err)
		}
		return
	}

	clone := *frag
	ApplyScorecard(&clone, score, time.Now().UTC())
	if err := u.fragmentsRepo.UpdateDerived(hctx, &clone); err != nil {
		log.Printf("[importance.runtime] update failed fragmentId=%s path=%s err=%v", strings.TrimSpace(clone.ID), strings.TrimSpace(clone.CanonicalPath), err)
		return
	}
}

func (u *RuntimeUpdater) finish(fragmentID string) {
	u.mu.Lock()
	delete(u.pending, strings.TrimSpace(fragmentID))
	u.mu.Unlock()
}

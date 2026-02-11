package interpretation

import (
	"context"
	"log"
	"strings"
	"time"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// Dispatcher evaluates trigger conditions and enqueues interpretation requests.
type Dispatcher struct {
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository
	evaluator     *TriggerEvaluator
	queue         *InterpretationQueue
	config        TriggerConfig
}

func NewDispatcher(
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	evaluator *TriggerEvaluator,
	queue *InterpretationQueue,
	config TriggerConfig,
) *Dispatcher {
	return &Dispatcher{
		fragmentsRepo: fragmentsRepo,
		evaluator:     evaluator,
		queue:         queue,
		config:        config,
	}
}

// EvaluateAndEnqueue loads the fragment, evaluates triggers, and enqueues missing requests.
func (d *Dispatcher) EvaluateAndEnqueue(ctx context.Context, fragmentID string) error {
	if d == nil || d.fragmentsRepo == nil || d.evaluator == nil || d.queue == nil {
		return nil
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return nil
	}

	frag, err := d.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || frag == nil {
		return err
	}

	triggers, err := d.evaluator.Evaluate(ctx, frag, d.config)
	if err != nil || len(triggers) == 0 {
		return err
	}

	for _, t := range triggers {
		hasPending, err := d.queue.HasPending(ctx, frag.ID, t.Polarity)
		if err == nil && hasPending {
			continue
		}

		req := &InterpretationRequest{
			FragmentID:    strings.TrimSpace(frag.ID),
			CanonicalPath: strings.TrimSpace(frag.CanonicalPath),
			NodeLabel:     strings.TrimSpace(frag.Labels["en"]),
			Polarity:      strings.TrimSpace(t.Polarity),
			Priority:      t.Priority,
			TriggerType:   t.Type,
			TriggerValue:  t.Value,
			CreatedAt:     time.Now().UTC(),
			Status:        "pending",
		}
		if _, err := d.queue.Enqueue(ctx, req); err != nil {
			log.Printf("%s enqueue_failed fragmentId=%s polarity=%s err=%v", signalsys.LogQueue, strings.TrimSpace(frag.ID), strings.TrimSpace(t.Polarity), err)
		}
	}
	return nil
}


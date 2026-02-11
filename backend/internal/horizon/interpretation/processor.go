package interpretation

import (
	"context"
	"log"
	"strings"
	"time"

	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// Processor consumes pending interpretation requests and materializes interpretations.
type Processor struct {
	queue       *InterpretationQueue
	cache       *InterpretationCache
	interpreter *Interpreter
}

func NewProcessor(queue *InterpretationQueue, cache *InterpretationCache, interpreter *Interpreter) *Processor {
	return &Processor{
		queue:       queue,
		cache:       cache,
		interpreter: interpreter,
	}
}

func (p *Processor) ProcessBatch(ctx context.Context, limit int) error {
	if p == nil || p.queue == nil || p.interpreter == nil {
		return nil
	}
	requests, err := p.queue.Dequeue(ctx, limit)
	if err != nil || len(requests) == 0 {
		return err
	}

	for _, req := range requests {
		if req == nil || strings.TrimSpace(req.ID) == "" {
			continue
		}
		if err := p.queue.MarkProcessing(ctx, req.ID); err != nil {
			continue
		}

		out, err := p.interpreter.Interpret(ctx, req)
		if err != nil {
			_ = p.queue.MarkFailed(ctx, req.ID, err.Error())
			log.Printf("%s process_failed requestId=%s fragmentId=%s err=%v", signalsys.LogInterpret, strings.TrimSpace(req.ID), strings.TrimSpace(req.FragmentID), err)
			continue
		}

		if p.cache != nil && out != nil {
			if err := p.cache.Set(ctx, out); err != nil {
				log.Printf("%s cache_set_failed requestId=%s fragmentId=%s err=%v", signalsys.LogCache, strings.TrimSpace(req.ID), strings.TrimSpace(req.FragmentID), err)
			}
		}
		_ = p.queue.MarkCompleted(ctx, req.ID)
	}
	return nil
}

// Start launches a background polling loop.
func (p *Processor) Start(ctx context.Context, interval time.Duration, batchSize int) {
	if p == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				_ = p.ProcessBatch(runCtx, batchSize)
				cancel()
			}
		}
	}()
}


package feedback

import (
	"context"
	"time"
)

// Event is a generic feedback signal emitted by downstream analyzers/interpreters
// back into the risk pipeline for re-evaluation or attention escalation.
type Event struct {
	Type          string         `json:"type"`
	FragmentID    string         `json:"fragmentId,omitempty"`
	CanonicalPath string         `json:"canonicalPath,omitempty"`
	Polarity      string         `json:"polarity,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	Payload       map[string]any `json:"payload,omitempty"`
	EmittedAt     time.Time      `json:"emittedAt"`
}

// Emitter is an optional integration point; implementations can enqueue jobs,
// publish telemetry, or route events to any internal queue.
type Emitter interface {
	Emit(ctx context.Context, event Event) error
}

// FuncEmitter adapts a function to Emitter.
type FuncEmitter func(ctx context.Context, event Event) error

func (f FuncEmitter) Emit(ctx context.Context, event Event) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

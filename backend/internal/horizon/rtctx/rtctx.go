package rtctx

import "context"

// Context holds realtime routing + correlation metadata for Horizon events.
//
// IMPORTANT:
// - TenantID is used as a security namespace (WS connections are bucketed by tenant).
// - ProjectID is the minimum routing key for WS delivery (clients subscribe by projectId).
// - LensID is used by the UI to scope assembly/cockpit feeds.
// - CorrelationID ties all events for a single "assemble" request together.
// - SessionID is optional "future-proofing": we propagate it but do not route by it yet.
type Context struct {
	TenantID      string
	ProjectID     string
	LensID        string
	CorrelationID string
	SessionID     string
}

type key struct{}

// With attaches Horizon realtime context to ctx.
func With(ctx context.Context, c Context) context.Context {
	return context.WithValue(ctx, key{}, c)
}

// Get returns the Horizon realtime context from ctx (if present).
func Get(ctx context.Context) (Context, bool) {
	v := ctx.Value(key{})
	if v == nil {
		return Context{}, false
	}
	c, ok := v.(Context)
	return c, ok
}

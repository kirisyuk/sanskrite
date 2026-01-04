package lens

import "context"

// Update options are carried via context to avoid breaking request schemas.

type ctxKeySkipScouting struct{}

// WithSkipScouting marks the current lens update operation as "scope-only":
// it should NOT create scouting jobs for newly added scope items.
func WithSkipScouting(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeySkipScouting{}, true)
}

// SkipScouting returns true if the current lens update should suppress scouting job creation.
func SkipScouting(ctx context.Context) bool {
	v := ctx.Value(ctxKeySkipScouting{})
	b, ok := v.(bool)
	return ok && b
}



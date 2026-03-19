package datasource

import (
	"context"
	"time"
)

// RawRecord is the raw data from an external source before normalization.
type RawRecord struct {
	Source   string
	Data     interface{}
	FetchedAt time.Time
}

// CanonicalRecord is the normalized record ready for storage.
type CanonicalRecord struct {
	Kind     string      // "material" | "facility" | "route" | "conflict_zone" | "processing_step"
	ID       string
	Payload  interface{}
}

// SourceSchema describes the adapter's output schema.
type SourceSchema struct {
	SourceID   string
	RecordKind string
	Fields     []string
}

// RateLimitConfig for adapter throttling.
type RateLimitConfig struct {
	RequestsPerMinute int
	Burst            int
}

// Adapter fetches and normalizes data from external sources (whitepaper 3.4).
type Adapter interface {
	Name() string
	Fetch(ctx context.Context, since time.Time) ([]RawRecord, error)
	Normalize(raw RawRecord) (CanonicalRecord, error)
	Schema() SourceSchema
	RateLimit() RateLimitConfig
}

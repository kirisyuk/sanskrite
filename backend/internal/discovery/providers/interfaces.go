package providers

import (
	"context"
	"time"
)

// Capability describes provider abilities and operational limits
type Capability struct {
	SupportsPolling bool
	SupportsWebhook bool
	MaxItemsPerRun  int
	RateLimitPerMin int
	PageLimit       int
}

// NormalizedDocument is a provider-agnostic unit produced by ingest
type NormalizedDocument struct {
	ExternalID        string
	Title             string
	URL               string
	DocumentPath      string
	FullText          string
	Language          string
	ContentType       string // plainText, html, markdown, binary
	DocumentType      string // post, article, pdf, word
	Authors           []string
	SourceMeta        map[string]any
	OriginPublishedAt *time.Time
}

// Provider is an extensible interface for sources
type Provider interface {
	// Key unique type key, e.g., "rss", "telegram", "web"
	Key() string
	// Capability returns provider capabilities
	Capability() Capability

	// Ingest pulls new/updated items and returns normalized documents
	Ingest(ctx context.Context, tenantID string, sourceID string) ([]NormalizedDocument, error)
	// Search optional direct provider search
	Search(ctx context.Context, tenantID string, sourceID string, query string, opts map[string]any) ([]NormalizedDocument, error)
	// Ground optional revalidation by references
	Ground(ctx context.Context, tenantID string, sourceID string, references []string) ([]NormalizedDocument, error)
	// Webhook handles provider push events
	Webhook(ctx context.Context, tenantID string, sourceID string, event map[string]any) error
}

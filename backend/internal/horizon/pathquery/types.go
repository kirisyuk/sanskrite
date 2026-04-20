package pathquery

import (
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// EndpointKind distinguishes how From/To are interpreted before resolution.
type EndpointKind int

const (
	EndpointCanonicalPath EndpointKind = iota
	EndpointFragmentID
)

// Endpoint is one end of a path query (From or To).
type Endpoint struct {
	Kind  EndpointKind
	Value string
}

// SearchContext participates in cache keying and runtime filtering.
type SearchContext struct {
	LensID string
	Scope  string
	AsOf   time.Time
}

// CacheMode controls read/write behavior for horizon_path_cache.
type CacheMode uint8

const (
	CacheModeDefault CacheMode = iota
	CacheModeDisabled
	CacheModeRefresh
)

const DefaultCacheTTL = 24 * time.Hour

// Query is the pathquery input (design doc §3.3).
type Query struct {
	Tenant        string
	Project       string
	From          Endpoint
	To            *Endpoint
	ProductAxis   string
	MaxHops       int
	RelationKinds []ah.RelationName
	IncludeStress bool
	IncludeGeo    bool
	CacheMode     CacheMode
	CacheTTL      time.Duration
	Context       SearchContext
}

// Diagnostics mirrors design doc §3.4.
type Diagnostics struct {
	MaterializedHits      int      `json:"materializedHits"`
	BFSExpandedNodes      int      `json:"bfsExpandedNodes"`
	PathbuilderRuns       int      `json:"pathbuilderRuns"`
	ExposurePathFamilies  int      `json:"exposurePathFamilies"`
	TruncatedMaxHops      bool     `json:"truncatedMaxHops"`
	TruncatedResultCap    bool     `json:"truncatedResultCap"`
	ProductAxisFiltered   bool     `json:"productAxisFiltered"`
	RelationFilterApplied bool     `json:"relationFilterApplied"`
	Warnings              []string `json:"warnings,omitempty"`
}

// Result is the pathquery output.
type Result struct {
	Paths       []*horizonmodels.MaterializedArtifact
	FromCache   bool
	GeneratedAt time.Time
	CacheKey    string
	Diagnostics Diagnostics
}

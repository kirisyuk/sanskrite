package models

import (
	"fmt"
	"strings"
	"time"
)

// FragmentProfile represents a comprehensive, LLM-generated profile/dossier for a HorizonFragment.
// This is the "snapshot" of structured information about an entity at a point in time.
// Content is primarily sourced from LLM with Search Grounding, with graph context for identification.
type FragmentProfile struct {
	ID            string       `json:"id" firestore:"id"`
	FragmentID    string       `json:"fragmentId" firestore:"fragmentId"`
	FragmentType  FragmentType `json:"fragmentType" firestore:"fragmentType"`
	CanonicalPath string       `json:"canonicalPath" firestore:"canonicalPath"`

	// Versioning
	Version    int       `json:"version" firestore:"version"`
	CapturedAt time.Time `json:"capturedAt" firestore:"capturedAt"`
	ValidUntil *time.Time `json:"validUntil,omitempty" firestore:"validUntil,omitempty"` // Optional: when to consider stale

	// Content: type-specific sections
	// Keys: "overview", "financials", "leadership", "risks", "market_position", etc.
	Sections map[string]ProfileSection `json:"sections" firestore:"sections"`

	// Dashboard: high-grade analytics blocks derived from sections (optional; v2+).
	// This is designed for interactive dashboards (KPIs, scenarios, watchlists, charts).
	Dashboard *ProfileDashboard `json:"dashboard,omitempty" firestore:"dashboard,omitempty"`

	// Generation metadata
	Generation ProfileGeneration `json:"generation" firestore:"generation"`

	// Lifecycle
	Status    ProfileStatus `json:"status" firestore:"status"`
	CreatedAt time.Time     `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt" firestore:"updatedAt"`
}

// ProfileSection represents a single section of a profile (e.g., "overview", "financials").
type ProfileSection struct {
	Key        string         `json:"key" firestore:"key"`               // Section identifier
	Title      string         `json:"title" firestore:"title"`           // Human-readable title
	Content    string         `json:"content" firestore:"content"`       // Markdown/plain text content
	Structured map[string]any `json:"structured,omitempty" firestore:"structured,omitempty"` // Structured data (metrics, lists, etc.)
	Datasets   []ProfileDataset `json:"datasets,omitempty" firestore:"datasets,omitempty"`   // Visualization-ready datasets (v2+)
	Sources    []string       `json:"sources,omitempty" firestore:"sources,omitempty"`       // URLs/references
	Confidence float64        `json:"confidence" firestore:"confidence"` // 0.0-1.0
	Order      int            `json:"order" firestore:"order"`           // Display order
}

// ProfileDatasetType is a visualization type for datasets embedded in sections/dashboard.
type ProfileDatasetType string

const (
	ProfileDatasetTypeTimeSeries ProfileDatasetType = "timeseries"
	ProfileDatasetTypeBar        ProfileDatasetType = "bar"
	ProfileDatasetTypePie        ProfileDatasetType = "pie"
	ProfileDatasetTypeTable      ProfileDatasetType = "table"
)

// ProfileDataset is a visualization-ready dataset.
// It is intentionally generic enough to support different chart components on the frontend.
type ProfileDataset struct {
	ID          string             `json:"id" firestore:"id"`
	Title       string             `json:"title" firestore:"title"`
	Type        ProfileDatasetType `json:"type" firestore:"type"`
	Description string             `json:"description,omitempty" firestore:"description,omitempty"`
	Unit        string             `json:"unit,omitempty" firestore:"unit,omitempty"`         // e.g., "billion", "percent"
	Currency    string             `json:"currency,omitempty" firestore:"currency,omitempty"` // e.g., "USD"

	// For charts (timeseries/bar/pie): one or more series.
	Series []ProfileDatasetSeries `json:"series,omitempty" firestore:"series,omitempty"`

	// For tables: column schema + row data.
	Table *ProfileDatasetTable `json:"table,omitempty" firestore:"table,omitempty"`

	// Optional: dataset-level sources (if different from section sources).
	Sources    []string `json:"sources,omitempty" firestore:"sources,omitempty"`
	Confidence float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

type ProfileDatasetSeries struct {
	Name   string               `json:"name,omitempty" firestore:"name,omitempty"`
	Points []ProfileDatasetPoint `json:"points" firestore:"points"`
}

type ProfileDatasetPoint struct {
	X    string         `json:"x" firestore:"x"` // category or time label (e.g., "2024", "Taiwan")
	Y    float64        `json:"y" firestore:"y"`
	// Meta is intentionally flexible: LLM may emit object OR string/boolean flags.
	// We accept any JSON type here to avoid hard failures on minor schema drift.
	Meta any `json:"meta,omitempty" firestore:"meta,omitempty"`
}

type ProfileDatasetTable struct {
	Columns []ProfileDatasetTableColumn `json:"columns" firestore:"columns"`
	Rows    []map[string]any            `json:"rows" firestore:"rows"`
}

type ProfileDatasetTableColumn struct {
	ID     string `json:"id" firestore:"id"`         // key in row map
	Title  string `json:"title" firestore:"title"`   // display label
	Format string `json:"format,omitempty" firestore:"format,omitempty"` // e.g., "currency", "percent", "number"
}

// ProfileDashboard provides high-grade analytic blocks for interactive dashboards.
// It is optional and may be absent for older schema versions.
type ProfileDashboard struct {
	Thesis     string              `json:"thesis,omitempty" firestore:"thesis,omitempty"`         // 1-sentence thesis
	KPIs       []ProfileKPI         `json:"kpis,omitempty" firestore:"kpis,omitempty"`           // top metrics strip
	Scenarios  []ProfileScenario    `json:"scenarios,omitempty" firestore:"scenarios,omitempty"` // bull/base/bear
	Watchlist  []ProfileWatchItem   `json:"watchlist,omitempty" firestore:"watchlist,omitempty"` // leading indicators
	Charts     []ProfileDataset     `json:"charts,omitempty" firestore:"charts,omitempty"`       // cross-section charts
	Tables     []ProfileDataset     `json:"tables,omitempty" firestore:"tables,omitempty"`       // cross-section tables (Type=table)
	Notes      string              `json:"notes,omitempty" firestore:"notes,omitempty"`
	Confidence float64             `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

type ProfileKPI struct {
	ID         string  `json:"id" firestore:"id"`
	Label      string  `json:"label" firestore:"label"`
	Value      string  `json:"value" firestore:"value"`             // human-formatted (e.g., "$109B")
	Subtext    string  `json:"subtext,omitempty" firestore:"subtext,omitempty"` // e.g., "(2024)"
	TrendValue string  `json:"trendValue,omitempty" firestore:"trendValue,omitempty"` // e.g., "+12% YoY"
	Severity   string  `json:"severity,omitempty" firestore:"severity,omitempty"`     // "positive"|"warning"|"negative"|"neutral"
	Confidence float64 `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

type ProfileScenario struct {
	ID          string            `json:"id" firestore:"id"`
	Name        string            `json:"name" firestore:"name"`                 // "Bull"/"Base"/"Bear"
	Probability float64           `json:"probability" firestore:"probability"`   // 0..1
	Assumptions []string          `json:"assumptions,omitempty" firestore:"assumptions,omitempty"`
	Metrics     map[string]string `json:"metrics,omitempty" firestore:"metrics,omitempty"` // e.g., {"2026_revenue":"$138B"}
	Triggers    []string          `json:"triggers,omitempty" firestore:"triggers,omitempty"`
}

type ProfileWatchItem struct {
	ID         string   `json:"id" firestore:"id"`
	Indicator  string   `json:"indicator" firestore:"indicator"`
	Current    string   `json:"current,omitempty" firestore:"current,omitempty"`
	Threshold  string   `json:"threshold,omitempty" firestore:"threshold,omitempty"`
	Signal     string   `json:"signal,omitempty" firestore:"signal,omitempty"`
	Frequency  string   `json:"frequency,omitempty" firestore:"frequency,omitempty"` // "weekly"|"monthly"|...
	Sources    []string `json:"sources,omitempty" firestore:"sources,omitempty"`
	Confidence float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

// ProfileGeneration contains metadata about how the profile was generated.
type ProfileGeneration struct {
	Model         string              `json:"model" firestore:"model"`
	GroundingUsed bool                `json:"groundingUsed" firestore:"groundingUsed"`
	PromptTokens  int                 `json:"promptTokens" firestore:"promptTokens"`
	OutputTokens  int                 `json:"outputTokens" firestore:"outputTokens"`
	DurationMs    int                 `json:"durationMs" firestore:"durationMs"`
	GraphContext  GraphContextSummary `json:"graphContext" firestore:"graphContext"`
	GeneratedAt   time.Time           `json:"generatedAt" firestore:"generatedAt"`
	SchemaVersion string              `json:"schemaVersion" firestore:"schemaVersion"` // For backward compatibility
	GenerationNotes string            `json:"generationNotes,omitempty" firestore:"generationNotes,omitempty"`
}

// GraphContextSummary summarizes the graph context used for profile generation.
// This helps LLM identify the entity precisely using neighboring nodes.
type GraphContextSummary struct {
	NeighborCount  int      `json:"neighborCount" firestore:"neighborCount"`
	EdgeCount      int      `json:"edgeCount" firestore:"edgeCount"`
	EdgeTypes      []string `json:"edgeTypes,omitempty" firestore:"edgeTypes,omitempty"`
	TopNeighbors   []string `json:"topNeighbors,omitempty" firestore:"topNeighbors,omitempty"` // Canonical paths of key neighbors
	FragmentLabels []string `json:"fragmentLabels,omitempty" firestore:"fragmentLabels,omitempty"` // Labels from the fragment itself
}

// ProfileStatus represents the status of a profile.
type ProfileStatus string

const (
	ProfileStatusActive     ProfileStatus = "ACTIVE"
	ProfileStatusGenerating ProfileStatus = "GENERATING"
	ProfileStatusFailed     ProfileStatus = "FAILED"
	ProfileStatusStale      ProfileStatus = "STALE"
)

// FragmentProfileRefreshPolicy defines how profiles of a specific type should be refreshed.
type FragmentProfileRefreshPolicy struct {
	ID           string       `json:"id" firestore:"id"`
	FragmentType FragmentType `json:"fragmentType" firestore:"fragmentType"`
	
	// Schedule: cron expression (e.g., "0 3 * * *" for 3 AM daily)
	Schedule string `json:"schedule" firestore:"schedule"`
	
	// Selection criteria
	TopPercent   float64 `json:"topPercent" firestore:"topPercent"`     // 0.10 = top 10% by edge count
	MaxPerRun    int     `json:"maxPerRun" firestore:"maxPerRun"`       // Safety cap per run
	MinEdgeCount int     `json:"minEdgeCount" firestore:"minEdgeCount"` // Skip fragments with fewer edges
	
	// Freshness
	MinAgeDays int `json:"minAgeDays" firestore:"minAgeDays"` // Don't refresh if profile is younger than N days
	
	// Control
	Enabled   bool      `json:"enabled" firestore:"enabled"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

// ProfileSectionSpec defines the expected sections for a fragment type.
// Used by the generator to know what to request from LLM.
type ProfileSectionSpec struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"` // Hint for LLM
	Required    bool   `json:"required"`
	Order       int    `json:"order"`
}

// DefaultSectionsForType returns the default section specs for a fragment type.
func DefaultSectionsForType(fragmentType FragmentType) []ProfileSectionSpec {
	switch fragmentType {
	case FragmentTypeOrganization:
		return []ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Brief company description, industry, founding, headquarters", Required: true, Order: 1},
			{Key: "financials", Title: "Financials", Description: "Revenue, profit, market cap, funding, key financial metrics", Required: false, Order: 2},
			{Key: "leadership", Title: "Leadership", Description: "CEO, key executives, board composition", Required: false, Order: 3},
			{Key: "products_services", Title: "Products & Services", Description: "Main products, services, business lines", Required: false, Order: 4},
			{Key: "market_position", Title: "Market Position", Description: "Market share, competitive position, key competitors", Required: false, Order: 5},
			{Key: "risks", Title: "Risks & Challenges", Description: "Key risks, regulatory issues, challenges", Required: false, Order: 6},
			{Key: "recent_developments", Title: "Recent Developments", Description: "Recent news, announcements, events", Required: false, Order: 7},
		}
	case FragmentTypeMarket:
		return []ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Market definition, scope, key characteristics", Required: true, Order: 1},
			{Key: "size_metrics", Title: "Market Size", Description: "TAM, current size, historical growth", Required: false, Order: 2},
			{Key: "growth_outlook", Title: "Growth Outlook", Description: "CAGR, growth drivers, forecasts", Required: false, Order: 3},
			{Key: "competition", Title: "Competitive Landscape", Description: "Key players, market shares, concentration (HHI, CR4)", Required: false, Order: 4},
			{Key: "trends", Title: "Trends & Drivers", Description: "Key trends, technology shifts, regulatory changes", Required: false, Order: 5},
			{Key: "barriers", Title: "Barriers & Risks", Description: "Entry barriers, risks, challenges", Required: false, Order: 6},
			{Key: "regional", Title: "Regional Dynamics", Description: "Geographic breakdown, regional differences", Required: false, Order: 7},
		}
	case FragmentTypeProduct:
		return []ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Product description, category, manufacturer", Required: true, Order: 1},
			{Key: "specifications", Title: "Specifications", Description: "Technical specs, features, variants", Required: false, Order: 2},
			{Key: "market_position", Title: "Market Position", Description: "Market share, positioning, target segment", Required: false, Order: 3},
			{Key: "supply_chain", Title: "Supply Chain", Description: "Key inputs, suppliers, manufacturing locations", Required: false, Order: 4},
			{Key: "alternatives", Title: "Alternatives & Substitutes", Description: "Competing products, substitutes", Required: false, Order: 5},
			{Key: "pricing", Title: "Pricing", Description: "Price points, pricing trends", Required: false, Order: 6},
		}
	default:
		// For GOV (which uses FragmentTypeOrganization with /gov/ path) and others
		// Check if it's a gov body by convention
		return []ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "General description and purpose", Required: true, Order: 1},
			{Key: "structure", Title: "Structure", Description: "Organizational structure, key divisions", Required: false, Order: 2},
			{Key: "mandate", Title: "Mandate & Functions", Description: "Legal mandate, responsibilities, functions", Required: false, Order: 3},
			{Key: "regulations", Title: "Regulations", Description: "Key regulations issued or enforced", Required: false, Order: 4},
			{Key: "recent_actions", Title: "Recent Actions", Description: "Recent decisions, announcements, enforcement actions", Required: false, Order: 5},
		}
	}
}

// DefaultRefreshPolicies returns the default refresh policies for all fragment types.
func DefaultRefreshPolicies() []FragmentProfileRefreshPolicy {
	now := time.Now().UTC()
	return []FragmentProfileRefreshPolicy{
		{
			ID:           "default_organization",
			FragmentType: FragmentTypeOrganization,
			Schedule:     "0 3 * * *", // 3 AM daily
			TopPercent:   0.10,
			MaxPerRun:    500,
			MinEdgeCount: 3,
			MinAgeDays:   1,
			Enabled:      true,
			UpdatedAt:    now,
		},
		{
			ID:           "default_market",
			FragmentType: FragmentTypeMarket,
			Schedule:     "0 4 * * *", // 4 AM daily
			TopPercent:   0.10,
			MaxPerRun:    200,
			MinEdgeCount: 2,
			MinAgeDays:   1,
			Enabled:      true,
			UpdatedAt:    now,
		},
		{
			ID:           "default_product",
			FragmentType: FragmentTypeProduct,
			Schedule:     "0 5 * * *", // 5 AM daily
			TopPercent:   0.10,
			MaxPerRun:    300,
			MinEdgeCount: 2,
			MinAgeDays:   1,
			Enabled:      true,
			UpdatedAt:    now,
		},
		// GOV uses ORGANIZATION type with /gov/ path prefix - handled separately in scheduler
	}
}

// Validate validates the FragmentProfile.
func (p *FragmentProfile) Validate() error {
	if strings.TrimSpace(p.FragmentID) == "" {
		return fmt.Errorf("fragmentId is required")
	}
	if strings.TrimSpace(p.CanonicalPath) == "" {
		return fmt.Errorf("canonicalPath is required")
	}
	if p.FragmentType == "" {
		return fmt.Errorf("fragmentType is required")
	}
	if p.Sections == nil {
		p.Sections = make(map[string]ProfileSection)
	}
	if p.Status == "" {
		p.Status = ProfileStatusActive
	}
	return nil
}

// IsStale returns true if the profile is considered stale.
func (p *FragmentProfile) IsStale(maxAge time.Duration) bool {
	if p.Status == ProfileStatusStale {
		return true
	}
	if p.ValidUntil != nil && time.Now().After(*p.ValidUntil) {
		return true
	}
	return time.Since(p.CapturedAt) > maxAge
}

// GetSection returns a section by key, or nil if not found.
func (p *FragmentProfile) GetSection(key string) *ProfileSection {
	if p.Sections == nil {
		return nil
	}
	if section, ok := p.Sections[key]; ok {
		return &section
	}
	return nil
}

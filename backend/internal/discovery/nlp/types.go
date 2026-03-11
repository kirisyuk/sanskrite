package nlp

import "time"

type Mode string

const (
	ModeRules  Mode = "rules"
	ModeShadow Mode = "shadow"
	ModeHybrid Mode = "hybrid"
	ModeModel  Mode = "model"
)

func NormalizeMode(v string, fallback Mode) Mode {
	switch Mode(v) {
	case ModeRules, ModeShadow, ModeHybrid, ModeModel:
		return Mode(v)
	default:
		return fallback
	}
}

type DocumentInput struct {
	SourceID     string                 `json:"sourceId"`
	SourceHost   string                 `json:"sourceHost"`
	SourceURL    string                 `json:"sourceUrl,omitempty"`
	Title        string                 `json:"title"`
	Summary      string                 `json:"summary,omitempty"`
	Body         string                 `json:"body,omitempty"`
	URL          string                 `json:"url,omitempty"`
	PublishedAt  *time.Time             `json:"publishedAt,omitempty"`
	Language     string                 `json:"language,omitempty"`
	SourceType   string                 `json:"sourceType,omitempty"`
	CategoryHint string                 `json:"categoryHint,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

type ArticleAssessment struct {
	IsEvent             bool                   `json:"isEvent"`
	EventClass          string                 `json:"eventClass,omitempty"`
	EventConfidence     float64                `json:"eventConfidence,omitempty"`
	AnalysisScore       float64                `json:"analysisScore,omitempty"`
	AdvisoryScore       float64                `json:"advisoryScore,omitempty"`
	BroadScopeScore     float64                `json:"broadScopeScore,omitempty"`
	ConcreteOutageScore float64                `json:"concreteOutageScore,omitempty"`
	ShouldCreateZone    bool                   `json:"shouldCreateZone"`
	ShouldWarmExisting  bool                   `json:"shouldWarmExisting"`
	ShouldOnlySignal    bool                   `json:"shouldOnlySignal"`
	RejectionReason     string                 `json:"rejectionReason,omitempty"`
	Reasons             []string               `json:"reasons,omitempty"`
	Raw                 map[string]interface{} `json:"raw,omitempty"`
}

type ExtractedEntity struct {
	Surface        string                 `json:"surface"`
	Normalized     string                 `json:"normalized,omitempty"`
	EntityType     string                 `json:"entityType"`
	Role           string                 `json:"role,omitempty"`
	Confidence     float64                `json:"confidence,omitempty"`
	StartOffset    int                    `json:"startOffset,omitempty"`
	EndOffset      int                    `json:"endOffset,omitempty"`
	CountryCode    string                 `json:"countryCode,omitempty"`
	GeoHint        string                 `json:"geoHint,omitempty"`
	LinkCandidates []string               `json:"linkCandidates,omitempty"`
	Attributes     map[string]interface{} `json:"attributes,omitempty"`
}

type EntityExtractionResult struct {
	Entities []ExtractedEntity      `json:"entities"`
	Raw      map[string]interface{} `json:"raw,omitempty"`
}

type EventGate interface {
	Assess(doc DocumentInput) (*ArticleAssessment, error)
}

type EntityExtractor interface {
	Extract(doc DocumentInput) (*EntityExtractionResult, error)
}

package models

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// This file holds *universal* model primitives shared across HorizonFragment,
// DependencyEdge, MaterializedArtifact, RouteGeometryProfile and friends.
//
// Closed enumerations that describe the AgentHorde taxonomy itself
// (FragmentClass, RelationName, MechanismCategory, AssuranceType,
// DisruptionKind, MobileAssetKind, etc.) live in
// `internal/taxonomy/ah` and are imported by name from the other model files.
// Do NOT add new domain enums here — extend the taxonomy SoT instead.

var reISO6709Pair = regexp.MustCompile(`^([+-]\d+(?:\.\d+)?)([+-]\d+(?:\.\d+)?)/?$`)

// MetricValue представляет гибкую метрику с описанием, типом и метаданными
type MetricValue struct {
	Value       float64      `json:"value" firestore:"value"`
	Unit        *string      `json:"unit,omitempty" firestore:"unit,omitempty"`
	ErrorMargin *ErrorMargin `json:"errorMargin,omitempty" firestore:"errorMargin,omitempty"`
	Confidence  float64      `json:"confidence" firestore:"confidence"` // 0..1
	ObservedAt  time.Time    `json:"observedAt" firestore:"observedAt"`
	FreshUntil  *time.Time   `json:"freshUntil,omitempty" firestore:"freshUntil,omitempty"`
	SourceKind  *string      `json:"sourceKind,omitempty" firestore:"sourceKind,omitempty"`
	Provenance  Provenance   `json:"provenance" firestore:"provenance"`
}

// ErrorMargin представляет интервал ошибки для метрики
type ErrorMargin struct {
	Low   *float64 `json:"low,omitempty" firestore:"low,omitempty"`
	High  *float64 `json:"high,omitempty" firestore:"high,omitempty"`
	Value *float64 `json:"value,omitempty" firestore:"value,omitempty"` // для симметричного интервала
}

// Location представляет местоположение с ISO стандартами
type Location struct {
	Name      string  `json:"name,omitempty" firestore:"name,omitempty"`
	ISO6709   *string `json:"iso6709,omitempty" firestore:"iso6709,omitempty"`     // координаты ISO 6709
	ISO3166_1 *string `json:"iso3166_1,omitempty" firestore:"iso3166_1,omitempty"` // ISO 3166-1 alpha-2 (страна)
	ISO3166_2 *string `json:"iso3166_2,omitempty" firestore:"iso3166_2,omitempty"` // ISO 3166-2 (субъект)
}

// locationJSON is used for flexible unmarshalling (LLMs often return "country" instead of "iso3166_1").
type locationJSON struct {
	Name          string  `json:"name,omitempty"`
	ISO6709       *string `json:"iso6709,omitempty"`
	ISO3166_1     *string `json:"iso3166_1,omitempty"`
	ISO3166_2     *string `json:"iso3166_2,omitempty"`
	Country       *string `json:"country,omitempty"`
	CountryCode   *string `json:"countryCode,omitempty"`
	CountryAlpha2 *string `json:"countryAlpha2,omitempty"`
}

// UnmarshalJSON accepts both canonical fields and common LLM aliases (country, countryCode, countryAlpha2).
func (l *Location) UnmarshalJSON(data []byte) error {
	var raw locationJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	l.Name = strings.TrimSpace(raw.Name)
	l.ISO6709 = raw.ISO6709
	l.ISO3166_2 = raw.ISO3166_2
	if raw.ISO3166_1 != nil && strings.TrimSpace(*raw.ISO3166_1) != "" {
		v := strings.TrimSpace(*raw.ISO3166_1)
		l.ISO3166_1 = &v
	} else if raw.CountryAlpha2 != nil && strings.TrimSpace(*raw.CountryAlpha2) != "" {
		v := strings.TrimSpace(*raw.CountryAlpha2)
		if len(v) == 2 {
			l.ISO3166_1 = &v
		}
	} else if raw.CountryCode != nil && strings.TrimSpace(*raw.CountryCode) != "" {
		v := strings.TrimSpace(*raw.CountryCode)
		if len(v) == 2 {
			l.ISO3166_1 = &v
		}
	} else if raw.Country != nil && strings.TrimSpace(*raw.Country) != "" {
		v := strings.TrimSpace(*raw.Country)
		if len(v) == 2 {
			l.ISO3166_1 = &v
		}
	}
	return nil
}

// Coordinates is a typed geographic coordinate.
type Coordinates struct {
	Lat float64 `json:"lat" firestore:"lat"`
	Lon float64 `json:"lon" firestore:"lon"`
}

// IsValidCoordinates validates lat/lon ranges and guards against NaN/Inf.
func IsValidCoordinates(lat, lon float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return false
	}
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// ParseISO6709Coordinates parses "+lat-lon/" style ISO6709 coordinates.
func ParseISO6709Coordinates(raw string) (float64, float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, 0, false
	}
	m := reISO6709Pair.FindStringSubmatch(s)
	if len(m) != 3 {
		return 0, 0, false
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
	if err != nil {
		return 0, 0, false
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
	if err != nil {
		return 0, 0, false
	}
	if !IsValidCoordinates(lat, lon) {
		return 0, 0, false
	}
	return lat, lon, true
}

// KBReference представляет ссылку на внешнюю базу знаний
type KBReference struct {
	KB         string     `json:"kb" firestore:"kb"`
	ExternalID string     `json:"externalId" firestore:"externalId"`
	Confidence float64    `json:"confidence" firestore:"confidence"` // 0..1
	LastSync   *time.Time `json:"lastSync,omitempty" firestore:"lastSync,omitempty"`
}

// Provenance представляет прослеживаемость источника данных
type Provenance struct {
	SourceIDs                []string                   `json:"sourceIds" firestore:"sourceIds"`
	SourceRefs               []SourceRef                `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	DataSourceIDs            []string                   `json:"dataSourceIds,omitempty" firestore:"dataSourceIds,omitempty"`
	DocumentIDs              []string                   `json:"documentIds,omitempty" firestore:"documentIds,omitempty"`
	Contributions            []Contribution             `json:"contributions,omitempty" firestore:"contributions,omitempty"`
	ObservedAt               *time.Time                 `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
	Method                   string                     `json:"method,omitempty" firestore:"method,omitempty"`
	GroundingContractVersion string                     `json:"groundingContractVersion,omitempty" firestore:"groundingContractVersion,omitempty"`
	OriginMode               string                     `json:"originMode,omitempty" firestore:"originMode,omitempty"`
	InternalEvidence         *GroundingInternalEvidence `json:"internalEvidence,omitempty" firestore:"internalEvidence,omitempty"`
	ExternalEvidence         *GroundingExternalEvidence `json:"externalEvidence,omitempty" firestore:"externalEvidence,omitempty"`
	ResolutionStats          *GroundingResolutionStats  `json:"resolutionStats,omitempty" firestore:"resolutionStats,omitempty"`
}

// SourceRef представляет ссылку на источник данных
type SourceRef struct {
	SourceID   string  `json:"sourceId" firestore:"sourceId"`
	SourceType string  `json:"sourceType" firestore:"sourceType"`
	DocumentID *string `json:"documentId,omitempty" firestore:"documentId,omitempty"`
}

// GroundingInternalEvidence captures internal datastore evidence used by grounded calls.
type GroundingInternalEvidence struct {
	FragmentIDs   []string `json:"fragmentIds,omitempty" firestore:"fragmentIds,omitempty"`
	DatastoreURIs []string `json:"datastoreUris,omitempty" firestore:"datastoreUris,omitempty"`
	SourceIDs     []string `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	DocumentIDs   []string `json:"documentIds,omitempty" firestore:"documentIds,omitempty"`
}

// GroundingExternalEvidence captures external web evidence used by grounded calls.
type GroundingExternalEvidence struct {
	WebURLs   []string `json:"webUrls,omitempty" firestore:"webUrls,omitempty"`
	Citations []string `json:"citations,omitempty" firestore:"citations,omitempty"`
}

// GroundingResolutionStats captures grounding-aware materialization outcomes.
type GroundingResolutionStats struct {
	RelationsResolvedByGrounding int `json:"relationsResolvedByGrounding,omitempty" firestore:"relationsResolvedByGrounding,omitempty"`
	RelationsCreatedNew          int `json:"relationsCreatedNew,omitempty" firestore:"relationsCreatedNew,omitempty"`
	DedupRedirectsByGrounding    int `json:"dedupRedirectsByGrounding,omitempty" firestore:"dedupRedirectsByGrounding,omitempty"`
}

// Contribution represents a single source contribution to a metric at a node.
type Contribution struct {
	SignalID      string    `json:"signalId" firestore:"signalId"`
	SourceEventID string    `json:"sourceEventId" firestore:"sourceEventId"`
	Path          []string  `json:"path" firestore:"path"`
	Amplitude     float64   `json:"amplitude" firestore:"amplitude"`
	Hops          int       `json:"hops" firestore:"hops"`
	ObservedAt    time.Time `json:"observedAt" firestore:"observedAt"`
}

// Governance управляет жизненным циклом HorizonFragment
type Governance struct {
	Owner              string  `json:"owner" firestore:"owner"`
	ActiveSubscribers  int     `json:"activeSubscribers" firestore:"activeSubscribers"`
	Status             string  `json:"status" firestore:"status"`
	CreatedByProjectID *string `json:"createdByProjectId,omitempty" firestore:"createdByProjectId,omitempty"`
}

// EpistemicStatus статус эпистемической достоверности
type EpistemicStatus string

const (
	EpistemicStatusVerified      EpistemicStatus = "VERIFIED"
	EpistemicStatusRejected      EpistemicStatus = "REJECTED"
	EpistemicStatusLowConfidence EpistemicStatus = "LOW_CONFIDENCE"
)

// FragmentStatus статус жизненного цикла HorizonFragment
type FragmentStatus string

const (
	FragmentStatusActive   FragmentStatus = "ACTIVE"
	FragmentStatusDormant  FragmentStatus = "DORMANT"
	FragmentStatusArchived FragmentStatus = "ARCHIVED"
)

// ExtractionMethod метод извлечения данных
type ExtractionMethod string

const (
	ExtractionMethodNLP     ExtractionMethod = "NLP"
	ExtractionMethodHuman   ExtractionMethod = "human"
	ExtractionMethodAPI     ExtractionMethod = "API"
	ExtractionMethodKBAlign ExtractionMethod = "KBAlign"
)

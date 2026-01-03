package models

import "time"

// MetricValue представляет гибкую метрику с описанием, типом и метаданными
type MetricValue struct {
	Value       float64      `json:"value" firestore:"value"`
	Unit        *string      `json:"unit,omitempty" firestore:"unit,omitempty"`
	ErrorMargin *ErrorMargin `json:"errorMargin,omitempty" firestore:"errorMargin,omitempty"`
	Confidence  float64      `json:"confidence" firestore:"confidence"` // 0..1
	ObservedAt  time.Time    `json:"observedAt" firestore:"observedAt"`
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

// KBReference представляет ссылку на внешнюю базу знаний
type KBReference struct {
	KB         string     `json:"kb" firestore:"kb"` // Wikidata, DBpedia, etc.
	ExternalID string     `json:"externalId" firestore:"externalId"`
	Confidence float64    `json:"confidence" firestore:"confidence"` // 0..1
	LastSync   *time.Time `json:"lastSync,omitempty" firestore:"lastSync,omitempty"`
}

// Provenance представляет прослеживаемость источника данных
type Provenance struct {
	SourceIDs     []string    `json:"sourceIds" firestore:"sourceIds"` // для обратной совместимости
	SourceRefs    []SourceRef `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	DataSourceIDs []string    `json:"dataSourceIds,omitempty" firestore:"dataSourceIds,omitempty"` // DataSource IDs
	DocumentIDs   []string    `json:"documentIds,omitempty" firestore:"documentIds,omitempty"`     // Document IDs
	ObservedAt    *time.Time  `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
	Method        string      `json:"method,omitempty" firestore:"method,omitempty"` // extracted, curated, imported
}

// SourceRef представляет ссылку на источник данных
type SourceRef struct {
	SourceID   string  `json:"sourceId" firestore:"sourceId"`                         // DataSource ID или Document ID
	SourceType string  `json:"sourceType" firestore:"sourceType"`                     // "dataSource", "document", "web", "rss", "telegram_channel"
	DocumentID *string `json:"documentId,omitempty" firestore:"documentId,omitempty"` // Если SourceType = "dataSource", может быть Document
}

// Governance управляет жизненным циклом HorizonFragment
type Governance struct {
	Owner              string  `json:"owner" firestore:"owner"`                                               // Всегда "SYSTEM"
	ActiveSubscribers  int     `json:"activeSubscribers" firestore:"activeSubscribers"`                       // Количество активных Lens
	Status             string  `json:"status" firestore:"status"`                                             // "ACTIVE", "DORMANT", "ARCHIVED"
	CreatedByProjectID *string `json:"createdByProjectId,omitempty" firestore:"createdByProjectId,omitempty"` // Проект, оплативший создание (для биллинга)
}

// FragmentType тип HorizonFragment
type FragmentType string

const (
	FragmentTypeOrganization FragmentType = "ORGANIZATION"
	FragmentTypeFacility     FragmentType = "FACILITY"
	FragmentTypeGeoRegion    FragmentType = "GEO_REGION"
	FragmentTypeSphere       FragmentType = "SPHERE"
	FragmentTypeEvent        FragmentType = "EVENT"
	FragmentTypeProduct      FragmentType = "PRODUCT"
	FragmentTypeProgram      FragmentType = "PROGRAM"
	FragmentTypeMarket       FragmentType = "MARKET"
	FragmentTypePerson       FragmentType = "PERSON"
	// FragmentTypeMediaChannel represents a "last-mile" media endpoint (e.g., a specific public Telegram channel),
	// not a media holding/company. This helps model narratives and distribution channels separately from owners.
	FragmentTypeMediaChannel FragmentType = "MEDIA_CHANNEL"
	FragmentTypeSupplyLink   FragmentType = "SUPPLY_LINK" // Tenant-specific supply chain link
)

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

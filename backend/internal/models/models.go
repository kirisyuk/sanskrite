package models

import "time"

// SourceDetails хранит параметры, специфичные для типа источника
type SourceDetails struct {
	// web
	URI *string `json:"uri,omitempty" firestore:"uri"`

	// rss
	RSSURL *string `json:"rssUrl,omitempty" firestore:"rssUrl"`

	// topic_based_interest
	SearchQuery         *string `json:"searchQuery,omitempty" firestore:"searchQuery"`
	OriginalSearchQuery *string `json:"originalSearchQuery,omitempty" firestore:"originalSearchQuery"`

	// telegram_channel
	ChannelUsername        *string `json:"channelUsername,omitempty" firestore:"channelUsername"`
	LastProcessedMessageID *int    `json:"lastProcessedMessageId,omitempty" firestore:"lastProcessedMessageId"`

	// twitter_search
	SinceTweetID *string `json:"sinceTweetId,omitempty" firestore:"sinceTweetId"`

	// document
	OriginalFileName *string `json:"originalFileName,omitempty" firestore:"originalFileName"`
	ContentType      *string `json:"contentType,omitempty" firestore:"contentType"`

	// legacy compatibility
	SourceURI *string `json:"sourceUri,omitempty" firestore:"sourceUri"`
}

// DataSource соответствует Core.DataSources
type DataSource struct {
	ID          string  `json:"id" firestore:"id"`
	TenantID    string  `json:"tenantId" firestore:"tenantId"`
	Pk          string  `json:"pk" firestore:"pk"`
	DisplayName string  `json:"displayName" firestore:"displayName"`
	Kind        string  `json:"kind" firestore:"kind"`
	Status      string  `json:"status" firestore:"status"`
	ResearchID  *string `json:"researchId,omitempty" firestore:"researchId"`
	SessionID   *string `json:"sessionId,omitempty" firestore:"sessionId"`

	OwnerUserID string        `json:"ownerUserId" firestore:"ownerUserId"`
	Details     SourceDetails `json:"sourceDetails" firestore:"sourceDetails"`

	LastPolledAt *time.Time `json:"lastPolledAt,omitempty" firestore:"lastPolledAt"`
	LastFoundAt  *time.Time `json:"lastFoundAt,omitempty" firestore:"lastFoundAt"`

	// Adaptive polling
	NextPollAt     *time.Time `json:"nextPollAt,omitempty" firestore:"nextPollAt"`
	PollBackoffSec *int       `json:"pollBackoffSec,omitempty" firestore:"pollBackoffSec"`

	ErrorCount int `json:"errorCount" firestore:"errorCount"`

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	TrustLevel float64   `json:"trustLevel" firestore:"trustLevel"`
	FirstSeen  time.Time `json:"firstSeenAt" firestore:"firstSeenAt"`
	LastSeen   time.Time `json:"lastSeenAt" firestore:"lastSeenAt"`
}

// SourceProfile документ хранит профиль источника отдельно от DataSource
type SourceProfile struct {
	ID             string     `json:"id" firestore:"id"`
	TenantID       string     `json:"tenantId" firestore:"tenantId"`
	DataSourceID   string     `json:"dataSourceId" firestore:"dataSourceId"`
	SchemaVersion  string     `json:"schemaVersion" firestore:"schemaVersion"`
	CreatedAt      time.Time  `json:"createdAt" firestore:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt" firestore:"updatedAt"`
	LastAnalyzedAt *time.Time `json:"lastAnalyzedAt,omitempty" firestore:"lastAnalyzedAt"`

	Profile SourceProfileData `json:"profile" firestore:"profile"`
}

// SourceProfileData matches SourceProfileDataDto from old frontend
type SourceProfileData struct {
	EditorialStance         EditorialStance     `json:"editorialStance"`
	Agenda                  *Agenda             `json:"agenda,omitempty"`
	Motivation              string              `json:"motivation"`
	TrustScore              float64             `json:"trustScore"`
	TrustRationale          string              `json:"trustRationale"`
	BehaviorPatterns        BehaviorPatterns    `json:"behaviorPatterns"`
	ContentPatterns         *ContentPatterns    `json:"contentPatterns,omitempty"`
	Topics                  []Topic             `json:"topics,omitempty"`
	EditorialMethods        []EditorialMethod   `json:"editorialMethods,omitempty"`
	Goals                   []Goal              `json:"goals,omitempty"`
	GoalAlignmentScore      *float64            `json:"goalAlignmentScore,omitempty"`
	Metrics                 *Metrics            `json:"metrics,omitempty"`
	TargetAudienceCountries []string            `json:"targetAudienceCountries,omitempty"`
	RedFlags                []RedFlag           `json:"redFlags,omitempty"`
	CoordinatedSources      []CoordinatedSource `json:"coordinatedSources,omitempty"`
	Flags                   []string            `json:"flags,omitempty"`
}

type EditorialStance struct {
	Political  string   `json:"political"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence,omitempty"`
}

type Agenda struct {
	PrimaryGoal    *string  `json:"primaryGoal,omitempty"`
	Description    *string  `json:"description,omitempty"`
	PromotedThemes []string `json:"promotedThemes,omitempty"`
	AvoidedThemes  []string `json:"avoidedThemes,omitempty"`
	TargetAudience *string  `json:"targetAudience,omitempty"`
}

type BehaviorPatterns struct {
	PublishingCadence   string  `json:"publishingCadence"`
	EventResponsiveness float64 `json:"eventResponsiveness"`
	ContentConsistency  float64 `json:"contentConsistency"`
	OriginalityScore    float64 `json:"originalityScore"`
}

type ContentPatterns struct {
	DominantFramings          []string                   `json:"dominantFramings,omitempty"`
	EmotionalToneDistribution *EmotionalToneDistribution `json:"emotionalToneDistribution,omitempty"`
	FocusEntityNames          []FocusEntity              `json:"focusEntityNames,omitempty"`
}

type EmotionalToneDistribution struct {
	Neutral *float64 `json:"neutral,omitempty"`
	Outrage *float64 `json:"outrage,omitempty"`
	Fear    *float64 `json:"fear,omitempty"`
	Hope    *float64 `json:"hope,omitempty"`
}

type FocusEntity struct {
	EntityName       string  `json:"entityName"`
	MentionCount     int     `json:"mentionCount"`
	AverageSentiment float64 `json:"averageSentiment"`
}

type Topic struct {
	Name  string  `json:"name"`
	Share float64 `json:"share"`
	Trend *string `json:"trend,omitempty"` // "up", "down", "flat"
}

type EditorialMethod struct {
	Method string  `json:"method"`
	Share  float64 `json:"share"`
}

type Goal struct {
	Goal       string  `json:"goal"`
	Confidence float64 `json:"confidence"`
}

type Metrics struct {
	ClaimDensity       *float64 `json:"claimDensity,omitempty"`
	ExternalRefShare   *float64 `json:"externalRefShare,omitempty"`
	ResponseLatencyAvg *float64 `json:"responseLatencyAvg,omitempty"`
	Polarization       *float64 `json:"polarization,omitempty"`
	CTAShare           *float64 `json:"ctaShare,omitempty"`
	PromoLinkShare     *float64 `json:"promoLinkShare,omitempty"`
	EvidenceQuality    *float64 `json:"evidenceQuality,omitempty"`
}

type RedFlag struct {
	Code      string  `json:"code"`
	Score     float64 `json:"score"`
	Rationale *string `json:"rationale,omitempty"`
}

type CoordinatedSource struct {
	SourceID          string  `json:"sourceId"`
	CoordinationScore float64 `json:"coordinationScore"`
}

// --- New RAG data model types ---

// SourceProject links a logical source to a project with per-project config
type SourceProject struct {
	ID         string             `json:"id" firestore:"id"`
	TenantID   string             `json:"tenantId" firestore:"tenantId"`
	SourceID   string             `json:"sourceId" firestore:"sourceId"`
	ProjectID  string             `json:"projectId" firestore:"projectId"`
	Status     string             `json:"status" firestore:"status"` // active/paused
	Weights    map[string]float64 `json:"weights,omitempty" firestore:"weights"`
	SyncConfig map[string]any     `json:"syncConfig,omitempty" firestore:"syncConfig"`
	CreatedAt  time.Time          `json:"createdAt" firestore:"createdAt"`
	UpdatedAt  time.Time          `json:"updatedAt" firestore:"updatedAt"`
}

// --- Meta Analysis (Human↔LLM coaching) ---

// MetaAnalyzer is a kind of analyzer we run over an interaction window
type MetaAnalyzer string

const (
	MetaAnalyzerThinking  MetaAnalyzer = "thinking"
	MetaAnalyzerBiases    MetaAnalyzer = "biases"
	MetaAnalyzerDecisions MetaAnalyzer = "decisions"
	MetaAnalyzerComm      MetaAnalyzer = "comm"
	MetaAnalyzerLearning  MetaAnalyzer = "learning"
)

// EnumScore captures a classified label with intensity and confidence
type EnumScore struct {
	Label      string   `json:"label" firestore:"label"`
	Intensity  float64  `json:"intensity" firestore:"intensity"`
	Confidence float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	Rationale  *string  `json:"rationale,omitempty" firestore:"rationale,omitempty"`
	Evidence   []string `json:"evidence,omitempty" firestore:"evidence,omitempty"`
}

// MetaCommon stores analyzer-agnostic dialog control metrics
type MetaCommon struct {
	WhoLeads        string  `json:"whoLeads" firestore:"whoLeads"`               // User|AI|Mixed
	LeadershipIndex float64 `json:"leadershipIndex" firestore:"leadershipIndex"` // 0..1
	FocusRetention  float64 `json:"focusRetention" firestore:"focusRetention"`   // 0..1
	StructureScore  float64 `json:"structureScore" firestore:"structureScore"`   // 0..1
}

// ThinkingPatternsResult mirrors taxonomy discussed with enums + scores
type ThinkingPatternsResult struct {
	Depth        EnumScore   `json:"depth" firestore:"depth"`               // Surface|Analytical|Systemic
	TimeHorizon  []EnumScore `json:"timeHorizon" firestore:"timeHorizon"`   // Strategic/Tactical/Reactive distribution
	Uncertainty  EnumScore   `json:"uncertainty" firestore:"uncertainty"`   // Avoids|Acknowledges|WorksWithIt
	ThinkingType []EnumScore `json:"thinkingType" firestore:"thinkingType"` // Convergent|Divergent|Integrative
}

// BiasEntry single bias item
type BiasEntry struct {
	Label      string   `json:"label" firestore:"label"`
	Intensity  float64  `json:"intensity" firestore:"intensity"`
	Confidence float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	Rationale  *string  `json:"rationale,omitempty" firestore:"rationale,omitempty"`
	Evidence   []string `json:"evidence,omitempty" firestore:"evidence,omitempty"`
}

// CognitiveBiasesResult captures detected biases and simple metrics
type CognitiveBiasesResult struct {
	Biases  []BiasEntry        `json:"biases" firestore:"biases"`
	Metrics map[string]float64 `json:"metrics,omitempty" firestore:"metrics,omitempty"` // e.g., pro_vs_con_ratio, assumption_updates
}

// DecisionMakingResult captures decision style axes
type DecisionMakingResult struct {
	Process       EnumScore `json:"process" firestore:"process"`             // Intuitive|Analytical|Consensus
	RiskAttitude  EnumScore `json:"riskAttitude" firestore:"riskAttitude"`   // RiskAverse|RiskNeutral|RiskSeeking
	CriteriaStyle EnumScore `json:"criteriaStyle" firestore:"criteriaStyle"` // Mono|Multi|Adaptive
	Speed         EnumScore `json:"speed" firestore:"speed"`                 // Impulsive|Reflective|Paralyzing
}

// CommunicationPatternsResult captures clarity/context/abstraction/feedback
type CommunicationPatternsResult struct {
	Clarity             EnumScore `json:"clarity" firestore:"clarity"`
	Abstraction         EnumScore `json:"abstraction" firestore:"abstraction"`
	ContextCompleteness EnumScore `json:"contextCompleteness" firestore:"contextCompleteness"`
	FeedbackLoop        EnumScore `json:"feedbackLoop" firestore:"feedbackLoop"`
}

// LearningTrajectoryResult captures evolution axes
type LearningTrajectoryResult struct {
	Dynamics      EnumScore `json:"dynamics" firestore:"dynamics"`           // Linear|Deepening|Jumping
	Integration   EnumScore `json:"integration" firestore:"integration"`     // Isolated|Connected|Synthetic
	MetaAwareness EnumScore `json:"metaAwareness" firestore:"metaAwareness"` // None|Partial|Full
}

// MetaWindow describes the analyzed slice
type MetaWindow struct {
	Mode         string     `json:"mode" firestore:"mode"` // lastN|time
	N            *int       `json:"n,omitempty" firestore:"n,omitempty"`
	Minutes      *int       `json:"minutes,omitempty" firestore:"minutes,omitempty"`
	MessageCount int        `json:"messageCount,omitempty" firestore:"messageCount,omitempty"`
	StartAt      *time.Time `json:"startAt,omitempty" firestore:"startAt,omitempty"`
	EndAt        *time.Time `json:"endAt,omitempty" firestore:"endAt,omitempty"`
}

// MetaAnalysisEvent is an append-only event produced by an analyzer run
type MetaAnalysisEvent struct {
	ID         string       `json:"id" firestore:"id"`
	TenantID   string       `json:"tenantId" firestore:"tenantId"`
	ProjectID  string       `json:"projectId" firestore:"projectId"`
	UserID     string       `json:"userId" firestore:"userId"`
	Analyzer   MetaAnalyzer `json:"analyzer" firestore:"analyzer"`
	Version    string       `json:"version" firestore:"version"`
	Model      string       `json:"model" firestore:"model"`
	TokensUsed int          `json:"tokensUsed" firestore:"tokensUsed"`
	Window     MetaWindow   `json:"window" firestore:"window"`
	Common     MetaCommon   `json:"common" firestore:"common"`
	// Raw result for flexibility and forward compatibility
	Result    map[string]any `json:"result" firestore:"result"`
	CreatedAt time.Time      `json:"createdAt" firestore:"createdAt"`
}

// MetaAggregateProject stores rolling EMA aggregates per project
type MetaAggregateProject struct {
	ID                       string         `json:"id" firestore:"id"` // same as projectId
	TenantID                 string         `json:"tenantId" firestore:"tenantId"`
	ProjectID                string         `json:"projectId" firestore:"projectId"`
	OwnerUserID              string         `json:"ownerUserId" firestore:"ownerUserId"`
	Version                  string         `json:"version" firestore:"version"`
	UpdatedAt                time.Time      `json:"updatedAt" firestore:"updatedAt"`
	Samples                  int            `json:"samples" firestore:"samples"`
	WhoLeads                 string         `json:"whoLeads" firestore:"whoLeads"`
	LeadershipIndex          float64        `json:"leadershipIndex" firestore:"leadershipIndex"`
	FocusRetention           float64        `json:"focusRetention" firestore:"focusRetention"`
	StructureScore           float64        `json:"structureScore" firestore:"structureScore"`
	Effectiveness            float64        `json:"effectiveness" firestore:"effectiveness"`
	LastAnalyzers            []string       `json:"lastAnalyzers,omitempty" firestore:"lastAnalyzers,omitempty"`
	LastResults              map[string]any `json:"lastResults,omitempty" firestore:"lastResults,omitempty"`
	LastAnalyzedMessageCount int            `json:"lastAnalyzedMessageCount" firestore:"lastAnalyzedMessageCount"`
	LastAnalyzedAt           time.Time      `json:"lastAnalyzedAt" firestore:"lastAnalyzedAt"`
}

// MetaAggregateUser stores weighted aggregates across projects for a user
type MetaAggregateUser struct {
	ID              string    `json:"id" firestore:"id"` // tenant_user composite
	TenantID        string    `json:"tenantId" firestore:"tenantId"`
	UserID          string    `json:"userId" firestore:"userId"`
	Version         string    `json:"version" firestore:"version"`
	UpdatedAt       time.Time `json:"updatedAt" firestore:"updatedAt"`
	Samples         int       `json:"samples" firestore:"samples"`
	LeadershipIndex float64   `json:"leadershipIndex" firestore:"leadershipIndex"`
	FocusRetention  float64   `json:"focusRetention" firestore:"focusRetention"`
	StructureScore  float64   `json:"structureScore" firestore:"structureScore"`
	Effectiveness   float64   `json:"effectiveness" firestore:"effectiveness"`
}

// Document represents a normalized unit of content to be chunked/embedded
type Document struct {
	ID                string           `json:"id" firestore:"id"`
	TenantID          string           `json:"tenantId" firestore:"tenantId"`
	SourceID          string           `json:"sourceId" firestore:"sourceId"`
	ExternalID        string           `json:"externalId" firestore:"externalId"`
	Title             string           `json:"title,omitempty" firestore:"title"`
	URL               string           `json:"url,omitempty" firestore:"url"`
	DocumentPath      string           `json:"documentPath" firestore:"documentPath"`
	FullText          string           `json:"fullText" firestore:"fullText"`
	Status            string           `json:"status" firestore:"status"`                       // new → embedded → processing → processed; error:*
	ContentType       string           `json:"contentType,omitempty" firestore:"contentType"`   // plainText, html, markdown, binary
	DocumentType      string           `json:"documentType,omitempty" firestore:"documentType"` // post, article, pdf, word
	Language          string           `json:"language,omitempty" firestore:"language"`
	Authors           []string         `json:"authors,omitempty" firestore:"authors"`
	SourceMeta        map[string]any   `json:"sourceMeta,omitempty" firestore:"sourceMeta"`
	ContentHash       string           `json:"contentHash" firestore:"contentHash"`
	Categories        []map[string]any `json:"categories,omitempty" firestore:"categories"` // {label, confidence, method, taxonomyId, version, at}
	Attrs             map[string]any   `json:"attrs,omitempty" firestore:"attrs"`
	OriginPublishedAt *time.Time       `json:"originPublishedAt,omitempty" firestore:"originPublishedAt"`
	IngestedAt        time.Time        `json:"ingestedAt" firestore:"ingestedAt"`
	CreatedAt         time.Time        `json:"createdAt" firestore:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt" firestore:"updatedAt"`
}

// Chunk stores vectorized searchable fragments of a document
type Chunk struct {
	ID               string         `json:"id" firestore:"id"`
	TenantID         string         `json:"tenantId" firestore:"tenantId"`
	DocID            string         `json:"docId" firestore:"docId"`
	SourceID         string         `json:"sourceId" firestore:"sourceId"`
	Position         int            `json:"position" firestore:"position"`
	Text             string         `json:"text" firestore:"text"`
	Embedding        []float64      `json:"embedding,omitempty" firestore:"embedding"`
	EmbeddingModel   string         `json:"embeddingModel,omitempty" firestore:"embeddingModel"`
	EmbeddingVersion string         `json:"embeddingVersion,omitempty" firestore:"embeddingVersion"`
	Attrs            map[string]any `json:"attrs,omitempty" firestore:"attrs"`
	ContentHash      string         `json:"contentHash" firestore:"contentHash"`
	CreatedAt        time.Time      `json:"createdAt" firestore:"createdAt"`
}

// Rule represents a Decision Intel rule configuration
type Rule struct {
	ID        string         `json:"id" firestore:"id"`
	TenantID  string         `json:"tenantId" firestore:"tenantId"`
	Scope     map[string]any `json:"scope" firestore:"scope"` // {level: global|tenant|project|source, ids: []}
	Kind      string         `json:"kind" firestore:"kind"`   // ingestion|enrichment|ranking|post-answer
	Priority  int            `json:"priority" firestore:"priority"`
	Version   string         `json:"version" firestore:"version"`
	Config    map[string]any `json:"config" firestore:"config"`
	CreatedAt time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

// Job tracks ingestion/reindex/migration operations
type Job struct {
	ID        string         `json:"id" firestore:"id"`
	TenantID  string         `json:"tenantId" firestore:"tenantId"`
	Type      string         `json:"type" firestore:"type"`
	Status    string         `json:"status" firestore:"status"` // pending|running|completed|failed
	Attempt   int            `json:"attempt" firestore:"attempt"`
	Payload   map[string]any `json:"payload,omitempty" firestore:"payload"`
	LastError *string        `json:"lastError,omitempty" firestore:"lastError"`
	CreatedAt time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

// SearchEvent captures search/grounding telemetry
type SearchEvent struct {
	ID            string         `json:"id" firestore:"id"`
	TenantID      string         `json:"tenantId" firestore:"tenantId"`
	ProjectID     string         `json:"projectId" firestore:"projectId"`
	Query         string         `json:"query" firestore:"query"`
	Filters       map[string]any `json:"filters,omitempty" firestore:"filters"`
	ResultsDocIDs []string       `json:"resultsDocIds,omitempty" firestore:"resultsDocIds"`
	ClickedDocID  *string        `json:"clickedDocId,omitempty" firestore:"clickedDocId"`
	Feedback      *string        `json:"feedback,omitempty" firestore:"feedback"`
	CreatedAt     time.Time      `json:"createdAt" firestore:"createdAt"`
}

// User соответствует Core.Users
type User struct {
	ID       string `json:"id" firestore:"id"`
	TenantID string `json:"tenantId" firestore:"tenantId"`
	Email    string `json:"email" firestore:"email"`

	DisplayName *string        `json:"displayName,omitempty" firestore:"displayName"`
	AuthType    *string        `json:"authType,omitempty" firestore:"authType"`
	Role        *string        `json:"role,omitempty" firestore:"role"`
	Status      *string        `json:"status,omitempty" firestore:"status"`
	Metadata    map[string]any `json:"metadata,omitempty" firestore:"metadata"`

	// Preferred UI language (ISO 3166-1 alpha-2, uppercase), default: "EN"
	PreferredLanguage *string `json:"preferredLanguage,omitempty" firestore:"preferredLanguage"`

	// Country of residence (ISO 3166-1 alpha-2, uppercase)
	CountryOfResidence *string `json:"countryOfResidence,omitempty" firestore:"countryOfResidence"`

	// Parent-child relation for supervised accounts
	ParentUserID *string `json:"parentUserId,omitempty" firestore:"parentUserId"`

	// Demographics (optional)
	Gender *string `json:"gender,omitempty" firestore:"gender"`
	Age    *int    `json:"age,omitempty" firestore:"age"`

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	SchemaVersion string `json:"schemaVersion" firestore:"schemaVersion"`

	CompanyRef        *string  `json:"companyRef,omitempty" firestore:"companyRef"`
	CompanyConfidence *float64 `json:"companyConfidence,omitempty" firestore:"companyConfidence"`
	JobTitle          *string  `json:"jobTitle,omitempty" firestore:"jobTitle"`
	Department        *string  `json:"department,omitempty" firestore:"department"`
	OrgUnitRef        *string  `json:"orgUnitRef,omitempty" firestore:"orgUnitRef"`

	DefaultProjectID *string           `json:"defaultProjectId,omitempty" firestore:"defaultProjectId"`
	ProjectRoles     map[string]string `json:"projectRoles,omitempty" firestore:"projectRoles"`

	AssignedGoalIDs       []string `json:"assignedGoalIds,omitempty" firestore:"assignedGoalIds"`
	AssignedInitiativeIDs []string `json:"assignedInitiativeIds,omitempty" firestore:"assignedInitiativeIds"`
}

// WaitlistEntry represents an email awaiting access approval
type WaitlistEntry struct {
	ID        string    `json:"id" firestore:"id"`
	Email     string    `json:"email" firestore:"email"`
	Status    string    `json:"status" firestore:"status"` // pending|approved|rejected
	Note      *string   `json:"note,omitempty" firestore:"note"`
	InvitedBy *string   `json:"invitedBy,omitempty" firestore:"invitedBy"`
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

// Project соответствует Core.Projects
type Project struct {
	ID          string `json:"id" firestore:"id"`
	Pk          string `json:"pk" firestore:"pk"`
	TenantID    string `json:"tenantId" firestore:"tenantId"`
	DisplayName string `json:"displayName" firestore:"displayName"`

	Description *string `json:"description,omitempty" firestore:"description"`
	Status      string  `json:"status" firestore:"status"`

	OwnerUserID string    `json:"ownerUserId" firestore:"ownerUserId"`
	CreatedAt   time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt" firestore:"updatedAt"`

	// For Discovery chat sorting
	LastInteractionAt *time.Time `json:"lastInteractionAt,omitempty" firestore:"lastInteractionAt"`

	// View type for filtering (e.g., "discovery", "workspace")
	View *string `json:"view,omitempty" firestore:"view"`

	IsExternal                   *bool   `json:"isExternal,omitempty" firestore:"isExternal"`
	LinkedProjectRef             *string `json:"linkedProjectRef,omitempty" firestore:"linkedProjectRef"`
	SharingPolicy                *string `json:"sharingPolicy,omitempty" firestore:"sharingPolicy"`
	OntologyOverlayRef           *string `json:"ontologyOverlayRef,omitempty" firestore:"ontologyOverlayRef"`
	CompanyRef                   *string `json:"companyRef,omitempty" firestore:"companyRef"`
	OrganizationName             *string `json:"organizationName,omitempty" firestore:"organizationName"`
	BusinessUnitName             *string `json:"businessUnitName,omitempty" firestore:"businessUnitName"`
	EnableRiskProfileSuggestions *bool   `json:"enableRiskProfileSuggestions,omitempty" firestore:"enableRiskProfileSuggestions"`
	AiGuided                     *bool   `json:"aiguided,omitempty" firestore:"aiguided"`
}

// --- Core project documents (user-uploaded or externally linked) ---

// ExternalDocRef describes a file living in an external service (Google Drive, OneDrive, etc.)
type ExternalDocRef struct {
	// Provider id, e.g. "gdrive", "onedrive", "box", "sharepoint"
	Provider string `json:"provider" firestore:"provider"`
	// Canonical URL to open the document (view link)
	URL string `json:"url" firestore:"url"`
	// Optional provider-specific identifiers
	ResourceID *string `json:"resourceId,omitempty" firestore:"resourceId,omitempty"`
	DriveID    *string `json:"driveId,omitempty" firestore:"driveId,omitempty"`
	OwnerEmail *string `json:"ownerEmail,omitempty" firestore:"ownerEmail,omitempty"`
	ETag       *string `json:"eTag,omitempty" firestore:"eTag,omitempty"`
	MimeType   *string `json:"mimeType,omitempty" firestore:"mimeType,omitempty"`
}

// CoreDoc represents a project-scoped document the user attached to a project.
// It can be stored in our storage (StoragePath) or referenced externally (External).
type CoreDoc struct {
	ID          string  `json:"id" firestore:"id"`
	TenantID    string  `json:"tenantId" firestore:"tenantId"`
	ProjectID   string  `json:"projectId" firestore:"projectId"`
	Filename    string  `json:"filename" firestore:"filename"`
	ContentType string  `json:"contentType" firestore:"contentType"`
	Description *string `json:"description,omitempty" firestore:"description,omitempty"`
	SizeBytes   *int64  `json:"sizeBytes,omitempty" firestore:"sizeBytes,omitempty"`
	// If uploaded to our storage (e.g., GCS), this contains a gs:// path or bucket/key
	StoragePath *string `json:"storagePath,omitempty" firestore:"storagePath,omitempty"`
	// If the document is an external reference (e.g., Google Docs), it's set
	External *ExternalDocRef `json:"external,omitempty" firestore:"external,omitempty"`

	// Minimal ownership/telemetry
	CreatedBy *string   `json:"createdBy,omitempty" firestore:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	// Parsed text (extraction result) and metadata
	ExtractedText *string        `json:"extractedText,omitempty" firestore:"extractedText,omitempty"`
	ParseStatus   *string        `json:"parseStatus,omitempty" firestore:"parseStatus,omitempty"` // new|parsed|error
	ParseMeta     map[string]any `json:"parseMeta,omitempty" firestore:"parseMeta,omitempty"`
	PageCount     *int           `json:"pageCount,omitempty" firestore:"pageCount,omitempty"`
}

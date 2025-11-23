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
	FullText          string           `json:"fullText" firestore:"fullText"`                           // Kept for backward compatibility, but content should be in GCS
	StoragePath       *string          `json:"storagePath,omitempty" firestore:"storagePath,omitempty"` // gs://bucket/path to content in GCS
	Status            string           `json:"status" firestore:"status"`                               // new → embedded → processing → processed; error:*
	ContentType       string           `json:"contentType,omitempty" firestore:"contentType"`           // plainText, html, markdown, binary
	DocumentType      string           `json:"documentType,omitempty" firestore:"documentType"`         // post, article, pdf, word
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

// SourceIngestHistory хранит историю обработок источника
type SourceIngestHistory struct {
	ID          string                 `json:"id" firestore:"id"`
	TenantID    string                 `json:"tenantId" firestore:"tenantId"`
	SourceID    string                 `json:"sourceId" firestore:"sourceId"`
	Kind        string                 `json:"kind" firestore:"kind"` // telegram_channel, rss, web
	Status      string                 `json:"status" firestore:"status"` // success, error, partial
	Error       *string                `json:"error,omitempty" firestore:"error,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty" firestore:"params,omitempty"` // Параметры обработки (lastMessageID, URL, etc.)
	DocumentsProcessed int             `json:"documentsProcessed" firestore:"documentsProcessed"`
	DocumentsCreated    int             `json:"documentsCreated" firestore:"documentsCreated"`
	DurationMs          *int            `json:"durationMs,omitempty" firestore:"durationMs,omitempty"`
	StartedAt   time.Time              `json:"startedAt" firestore:"startedAt"`
	CompletedAt *time.Time             `json:"completedAt,omitempty" firestore:"completedAt,omitempty"`
	CreatedAt   time.Time              `json:"createdAt" firestore:"createdAt"`
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
	// Verified project code (for /verified/{code} URLs)
	VerifiedCode *string `json:"verifiedCode,omitempty" firestore:"verifiedCode,omitempty"`
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
// Can also be a dynamic artifact with generation config (DocType = "dynamic_artifact").
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
	// For artifacts: contains the full assembled document (current version)
	ExtractedText *string        `json:"extractedText,omitempty" firestore:"extractedText,omitempty"`
	ParseStatus   *string        `json:"parseStatus,omitempty" firestore:"parseStatus,omitempty"` // new|parsed|error
	ParseMeta     map[string]any `json:"parseMeta,omitempty" firestore:"parseMeta,omitempty"`
	PageCount     *int           `json:"pageCount,omitempty" firestore:"pageCount,omitempty"`

	// Artifact-specific fields
	DocType        *string         `json:"docType,omitempty" firestore:"docType,omitempty"`               // "static" | "dynamic_artifact"
	ArtifactConfig *ArtifactConfig `json:"artifactConfig,omitempty" firestore:"artifactConfig,omitempty"` // nil for static docs
}

// ArtifactConfig contains configuration for dynamic artifact generation
type ArtifactConfig struct {
	Format              string                     `json:"format" firestore:"format"`                                               // "text" | "table" | "report" | "presentation"
	Focus               string                     `json:"focus" firestore:"focus"`                                                 // "brief" | "analysis" | "narrative" | "proposal" | "technical" | "executive"
	OrchestratorModel   string                     `json:"orchestratorModel" firestore:"orchestratorModel"`                         // model for final review/assembly
	DefaultModel        string                     `json:"defaultModel" firestore:"defaultModel"`                                   // default model for sections
	ReviewPrompt        string                     `json:"reviewPrompt" firestore:"reviewPrompt"`                                   // prompt for orchestrator review
	Structure           []ArtifactSection          `json:"structure" firestore:"structure"`                                         // document structure (sections)
	ContextSources      ArtifactContextSources     `json:"contextSources" firestore:"contextSources"`                               // context for generation
	AutoUpdateEnabled   bool                       `json:"autoUpdateEnabled" firestore:"autoUpdateEnabled"`                         // enable auto-updates
	AutoUpdateConfig    *ArtifactAutoUpdate        `json:"autoUpdateConfig,omitempty" firestore:"autoUpdateConfig,omitempty"`       // auto-update configuration
	CurrentVersionID    string                     `json:"currentVersionID" firestore:"currentVersionID"`                           // ID of current version
	VersionsRetained    int                        `json:"versionsRetained" firestore:"versionsRetained"`                           // number of versions to keep (default: 10)
	Versions            []ArtifactVersion          `json:"versions" firestore:"versions"`                                           // version history (sorted by timestamp desc)
	StructureGeneration *StructureGenerationConfig `json:"structureGeneration,omitempty" firestore:"structureGeneration,omitempty"` // how structure was created
	GenerationSettings  GenerationSettings         `json:"generationSettings" firestore:"generationSettings"`                       // generation settings
}

// ArtifactSection defines a section of the artifact with its generation config
type ArtifactSection struct {
	ID                string                  `json:"id" firestore:"id"`
	Title             string                  `json:"title" firestore:"title"`
	Order             int                     `json:"order" firestore:"order"`
	Prompt            string                  `json:"prompt" firestore:"prompt"`                                         // prompt for generating this section
	Model             *string                 `json:"model,omitempty" firestore:"model,omitempty"`                       // override default model
	ContextOverride   *ArtifactContextSources `json:"contextOverride,omitempty" firestore:"contextOverride,omitempty"`   // override context sources
	Content           *string                 `json:"content,omitempty" firestore:"content,omitempty"`                   // current content (from current version)
	GeneratedAt       *time.Time              `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`           // when this section was last generated
	TokensUsed        *int                    `json:"tokensUsed,omitempty" firestore:"tokensUsed,omitempty"`             // tokens used for generation
	Description       *string                 `json:"description,omitempty" firestore:"description,omitempty"`           // what this section should contain
	Required          bool                    `json:"required" firestore:"required"`                                     // is this section required
	Editable          bool                    `json:"editable" firestore:"editable"`                                     // can user edit/delete this section
	AutoUpdateEnabled bool                    `json:"autoUpdateEnabled" firestore:"autoUpdateEnabled"`                   // enable auto-updates for this section
	DependsOn         []string                `json:"dependsOn,omitempty" firestore:"dependsOn,omitempty"`               // IDs of sections that must be generated first
	GenerationStatus  *string                 `json:"generationStatus,omitempty" firestore:"generationStatus,omitempty"` // "pending" | "generating" | "completed" | "failed" | "skipped"
	PromptHistory     []PromptVersion         `json:"promptHistory,omitempty" firestore:"promptHistory,omitempty"`       // history of prompt changes
}

// ArtifactContextSources defines what context to use for generation
type ArtifactContextSources struct {
	ConversationRange *struct {
		From string `json:"from" firestore:"from"` // interaction/message ID
		To   string `json:"to" firestore:"to"`     // interaction/message ID
	} `json:"conversationRange,omitempty" firestore:"conversationRange,omitempty"`
	Documents      []string `json:"documents,omitempty" firestore:"documents,omitempty"`           // CoreDoc IDs
	SearchQuery    *string  `json:"searchQuery,omitempty" firestore:"searchQuery,omitempty"`       // search query for grounding
	PinnedMessages []string `json:"pinnedMessages,omitempty" firestore:"pinnedMessages,omitempty"` // interaction IDs
}

// ArtifactAutoUpdate defines auto-update behavior
type ArtifactAutoUpdate struct {
	Trigger     string                     `json:"trigger" firestore:"trigger"`                             // "on_new_message" | "daily" | "weekly" | "on_new_document"
	Conditions  *ArtifactUpdateConditions  `json:"conditions,omitempty" firestore:"conditions,omitempty"`   // conditions for triggering update
	SmartUpdate *ArtifactSmartUpdateConfig `json:"smartUpdate,omitempty" firestore:"smartUpdate,omitempty"` // smart update configuration
}

// ArtifactUpdateConditions defines when to trigger auto-update
type ArtifactUpdateConditions struct {
	MinNewMessages     *int     `json:"minNewMessages,omitempty" firestore:"minNewMessages,omitempty"`     // minimum new messages to trigger
	MinContextChange   *float64 `json:"minContextChange,omitempty" firestore:"minContextChange,omitempty"` // minimum context change (0-1)
	Keywords           []string `json:"keywords,omitempty" firestore:"keywords,omitempty"`                 // trigger if these keywords appear
	SearchQueryChanged bool     `json:"searchQueryChanged" firestore:"searchQueryChanged"`                 // trigger if search results changed
}

// ArtifactSmartUpdateConfig defines smart update behavior
type ArtifactSmartUpdateConfig struct {
	CheckQuality        bool    `json:"checkQuality" firestore:"checkQuality"`               // check quality before replacing
	MinQualityThreshold float64 `json:"minQualityThreshold" firestore:"minQualityThreshold"` // don't replace if new version is worse (0-1)
	NotifyOnUpdate      bool    `json:"notifyOnUpdate" firestore:"notifyOnUpdate"`           // notify user on update
}

// ArtifactVersion represents a snapshot of the artifact at a point in time
type ArtifactVersion struct {
	ID                string                             `json:"id" firestore:"id"`
	Timestamp         time.Time                          `json:"timestamp" firestore:"timestamp"`
	Trigger           string                             `json:"trigger" firestore:"trigger"`                     // "manual" | "auto" | "scheduled"
	FullDocument      string                             `json:"fullDocument" firestore:"fullDocument"`           // full assembled document
	Sections          map[string]ArtifactSectionSnapshot `json:"sections" firestore:"sections"`                   // sectionId -> snapshot
	ContextSnapshot   ArtifactContextSnapshot            `json:"contextSnapshot" firestore:"contextSnapshot"`     // context at generation time
	GenerationMeta    ArtifactGenerationMeta             `json:"generationMeta" firestore:"generationMeta"`       // generation metadata
	GenerationMode    string                             `json:"generationMode" firestore:"generationMode"`       // "auto" | "interactive"
	GenerationParams  GenerationParams                   `json:"generationParams" firestore:"generationParams"`   // parameters used for generation
	StructureSnapshot []ArtifactSection                  `json:"structureSnapshot" firestore:"structureSnapshot"` // structure at generation time
}

// ArtifactSectionSnapshot captures a section's state in a version
type ArtifactSectionSnapshot struct {
	Content     string               `json:"content" firestore:"content"`
	Prompt      string               `json:"prompt" firestore:"prompt"` // prompt used for generation
	Model       string               `json:"model" firestore:"model"`   // model used for generation
	TokensUsed  int                  `json:"tokensUsed" firestore:"tokensUsed"`
	GeneratedAt time.Time            `json:"generatedAt" firestore:"generatedAt"`
	Quality     *float64             `json:"quality,omitempty" firestore:"quality,omitempty"`         // optional quality score (0-1)
	QualityMeta *ArtifactQualityMeta `json:"qualityMeta,omitempty" firestore:"qualityMeta,omitempty"` // detailed quality metrics
}

// ArtifactQualityMeta contains detailed quality metrics for a section
type ArtifactQualityMeta struct {
	Coherence    float64 `json:"coherence" firestore:"coherence"`       // text coherence (0-1)
	Completeness float64 `json:"completeness" firestore:"completeness"` // completeness of response (0-1)
	Relevance    float64 `json:"relevance" firestore:"relevance"`       // relevance to context (0-1)
}

// ArtifactContextSnapshot captures the context state at generation time
type ArtifactContextSnapshot struct {
	ConversationRange *struct {
		From string `json:"from" firestore:"from"`
		To   string `json:"to" firestore:"to"`
	} `json:"conversationRange,omitempty" firestore:"conversationRange,omitempty"`
	Documents         []string `json:"documents" firestore:"documents"` // document IDs
	SearchQuery       *string  `json:"searchQuery,omitempty" firestore:"searchQuery,omitempty"`
	PinnedMessages    []string `json:"pinnedMessages" firestore:"pinnedMessages"`                           // interaction IDs
	MessageCount      int      `json:"messageCount" firestore:"messageCount"`                               // number of messages in range
	DocumentTitles    []string `json:"documentTitles" firestore:"documentTitles"`                           // titles of documents used
	SearchResultCount *int     `json:"searchResultCount,omitempty" firestore:"searchResultCount,omitempty"` // number of search results
}

// ArtifactGenerationMeta contains metadata about the generation process
type ArtifactGenerationMeta struct {
	TotalTokensUsed   int      `json:"totalTokensUsed" firestore:"totalTokensUsed"`
	DurationMs        int64    `json:"durationMs" firestore:"durationMs"`
	SectionsGenerated int      `json:"sectionsGenerated" firestore:"sectionsGenerated"`
	Errors            []string `json:"errors,omitempty" firestore:"errors,omitempty"`
}

// StructureGenerationConfig describes how the artifact structure was created
type StructureGenerationConfig struct {
	Mode             string     `json:"mode" firestore:"mode"`                                             // "manual" | "ai_generated" | "template"
	SourceTemplate   *string    `json:"sourceTemplate,omitempty" firestore:"sourceTemplate,omitempty"`     // ID of template if used
	GeneratedBy      *string    `json:"generatedBy,omitempty" firestore:"generatedBy,omitempty"`           // model that generated structure
	GenerationPrompt *string    `json:"generationPrompt,omitempty" firestore:"generationPrompt,omitempty"` // prompt for structure generation
	GeneratedAt      *time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
	UserModified     bool       `json:"userModified" firestore:"userModified"` // user edited after generation
}

// GenerationSettings contains general generation settings
type GenerationSettings struct {
	PreferredLanguage        string  `json:"preferredLanguage" firestore:"preferredLanguage"`                                   // "en", "ru", "es"...
	SystemPrompt             string  `json:"systemPrompt" firestore:"systemPrompt"`                                             // system prompt for all operations
	OrchestratorSystemPrompt *string `json:"orchestratorSystemPrompt,omitempty" firestore:"orchestratorSystemPrompt,omitempty"` // for final assembly
	Tone                     *string `json:"tone,omitempty" firestore:"tone,omitempty"`                                         // "formal", "casual", "technical", "executive"
	Audience                 *string `json:"audience,omitempty" firestore:"audience,omitempty"`                                 // "technical", "executive", "general"
	DetailLevel              *string `json:"detailLevel,omitempty" firestore:"detailLevel,omitempty"`                           // "brief", "moderate", "detailed"
}

// PromptVersion tracks changes to section prompts
type PromptVersion struct {
	Prompt        string    `json:"prompt" firestore:"prompt"`
	ChangedAt     time.Time `json:"changedAt" firestore:"changedAt"`
	ChangedBy     string    `json:"changedBy" firestore:"changedBy"`                             // userID or "system"
	ResultQuality *float64  `json:"resultQuality,omitempty" firestore:"resultQuality,omitempty"` // quality of result with this prompt (0-1)
}

// GenerationParams captures parameters used for a specific version generation
type GenerationParams struct {
	PreferredLanguage string            `json:"preferredLanguage" firestore:"preferredLanguage"`
	SystemPrompt      string            `json:"systemPrompt" firestore:"systemPrompt"`
	OrchestratorModel string            `json:"orchestratorModel" firestore:"orchestratorModel"`
	SectionModels     map[string]string `json:"sectionModels" firestore:"sectionModels"` // sectionID -> model
	Tone              *string           `json:"tone,omitempty" firestore:"tone,omitempty"`
	Audience          *string           `json:"audience,omitempty" firestore:"audience,omitempty"`
	DetailLevel       *string           `json:"detailLevel,omitempty" firestore:"detailLevel,omitempty"`
}

// ArtifactTemplate defines a reusable artifact template
type ArtifactTemplate struct {
	ID                           string            `json:"id" firestore:"id"`
	Name                         string            `json:"name" firestore:"name"`
	Description                  string            `json:"description" firestore:"description"`
	Category                     string            `json:"category" firestore:"category"` // "business", "technical", "legal", "research"
	Type                         string            `json:"type" firestore:"type"`         // "system" | "community" | "custom"
	RecommendedFormat            string            `json:"recommendedFormat" firestore:"recommendedFormat"`
	RecommendedFocus             string            `json:"recommendedFocus" firestore:"recommendedFocus"`
	RecommendedOrchestratorModel string            `json:"recommendedOrchestratorModel" firestore:"recommendedOrchestratorModel"`
	RecommendedSectionModel      string            `json:"recommendedSectionModel" firestore:"recommendedSectionModel"`
	DefaultStructure             []SectionTemplate `json:"defaultStructure" firestore:"defaultStructure"`
	SystemPrompts                map[string]string `json:"systemPrompts" firestore:"systemPrompts"` // language -> prompt
	CreatedBy                    string            `json:"createdBy" firestore:"createdBy"`         // "system" or userID
	TenantID                     *string           `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	CreatedAt                    time.Time         `json:"createdAt" firestore:"createdAt"`
	UpdatedAt                    time.Time         `json:"updatedAt" firestore:"updatedAt"`
	IsPublic                     bool              `json:"isPublic" firestore:"isPublic"`
	IsFeatured                   bool              `json:"isFeatured" firestore:"isFeatured"`
	UsageCount                   int               `json:"usageCount" firestore:"usageCount"`
	Rating                       float64           `json:"rating" firestore:"rating"`
	RatingCount                  int               `json:"ratingCount" firestore:"ratingCount"`
	SourceArtifactID             *string           `json:"sourceArtifactId,omitempty" firestore:"sourceArtifactId,omitempty"` // if created from artifact
	Tags                         []string          `json:"tags" firestore:"tags"`
}

// SectionTemplate defines a template for an artifact section
type SectionTemplate struct {
	ID             string   `json:"id" firestore:"id"`
	Title          string   `json:"title" firestore:"title"`
	Order          int      `json:"order" firestore:"order"`
	Description    string   `json:"description" firestore:"description"`
	PromptTemplate string   `json:"promptTemplate" firestore:"promptTemplate"` // template with placeholders
	Required       bool     `json:"required" firestore:"required"`
	Editable       bool     `json:"editable" firestore:"editable"`
	DependsOn      []string `json:"dependsOn,omitempty" firestore:"dependsOn,omitempty"`
	ExampleContent *string  `json:"exampleContent,omitempty" firestore:"exampleContent,omitempty"` // optional example
}

// GenerationSession tracks an active artifact generation session
type GenerationSession struct {
	ID                string                   `json:"id" firestore:"id"`
	ArtifactID        string                   `json:"artifactId" firestore:"artifactId"`
	Mode              string                   `json:"mode" firestore:"mode"`     // "auto" | "interactive"
	Status            string                   `json:"status" firestore:"status"` // "preparing" | "generating" | "reviewing" | "completed" | "failed"
	PreferredLanguage string                   `json:"preferredLanguage" firestore:"preferredLanguage"`
	SystemPrompt      string                   `json:"systemPrompt" firestore:"systemPrompt"`
	CurrentSection    *string                  `json:"currentSection,omitempty" firestore:"currentSection,omitempty"`
	CompletedSections []string                 `json:"completedSections" firestore:"completedSections"`
	Progress          float64                  `json:"progress" firestore:"progress"` // 0-1
	SectionResults    map[string]SectionResult `json:"sectionResults" firestore:"sectionResults"`
	Errors            []GenerationError        `json:"errors" firestore:"errors"`
	WSConnectionID    *string                  `json:"wsConnectionId,omitempty" firestore:"wsConnectionId,omitempty"` // for interactive mode
	CreatedAt         time.Time                `json:"createdAt" firestore:"createdAt"`
	UpdatedAt         time.Time                `json:"updatedAt" firestore:"updatedAt"`
}

// SectionResult captures the result of generating a section
type SectionResult struct {
	SectionID  string        `json:"sectionId" firestore:"sectionId"`
	Content    string        `json:"content" firestore:"content"`
	Model      string        `json:"model" firestore:"model"`
	TokensUsed int           `json:"tokensUsed" firestore:"tokensUsed"`
	Duration   time.Duration `json:"duration" firestore:"duration"`
	Quality    *float64      `json:"quality,omitempty" firestore:"quality,omitempty"`
	Status     string        `json:"status" firestore:"status"` // "pending" | "generating" | "completed" | "failed"
}

// GenerationError captures errors during generation
type GenerationError struct {
	SectionID *string   `json:"sectionId,omitempty" firestore:"sectionId,omitempty"`
	Message   string    `json:"message" firestore:"message"`
	Timestamp time.Time `json:"timestamp" firestore:"timestamp"`
}

// ---- System-level extracted analysis types ----
// NOTE: Entity and Event types are now defined in ontology.go
// Use models.Entity and models.Event from ontology.go for all new code

// Metric describes a measurable indicator with methodology
type Metric struct {
	Key         string   `json:"key" firestore:"key"`
	Label       string   `json:"label" firestore:"label"`
	Description string   `json:"description,omitempty" firestore:"description,omitempty"`
	Value       any      `json:"value" firestore:"value"`
	Unit        string   `json:"unit,omitempty" firestore:"unit,omitempty"`
	UpdateISO   string   `json:"updateISO,omitempty" firestore:"updateISO,omitempty"`
	UpdateDays  *int     `json:"updateDays,omitempty" firestore:"updateDays,omitempty"`
	Evidence    []string `json:"evidence,omitempty" firestore:"evidence,omitempty"`
	SourceIDs   []string `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	Confidence  float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`

	Definition string `json:"definition" firestore:"definition"`
	Scope      struct {
		Geography string `json:"geography,omitempty" firestore:"geography,omitempty"`
		Sector    string `json:"sector,omitempty" firestore:"sector,omitempty"`
		Industry  string `json:"industry,omitempty" firestore:"industry,omitempty"`
		Period    string `json:"period,omitempty" firestore:"period,omitempty"`
	} `json:"scope" firestore:"scope"`
	Computation struct {
		Formula     string `json:"formula,omitempty" firestore:"formula,omitempty"`
		Numerator   string `json:"numerator,omitempty" firestore:"numerator,omitempty"`
		Denominator string `json:"denominator,omitempty" firestore:"denominator,omitempty"`
		Aggregation string `json:"aggregation,omitempty" firestore:"aggregation,omitempty"`
	} `json:"computation" firestore:"computation"`
	DetectionCues struct {
		Keywords         []string `json:"keywords,omitempty" firestore:"keywords,omitempty"`
		Patterns         []string `json:"patterns,omitempty" firestore:"patterns,omitempty"`
		PositiveExamples []string `json:"positiveExamples,omitempty" firestore:"positiveExamples,omitempty"`
		NegativeExamples []string `json:"negativeExamples,omitempty" firestore:"negativeExamples,omitempty"`
	} `json:"detectionCues" firestore:"detectionCues"`
	DataSources  []string `json:"dataSources,omitempty" firestore:"dataSources,omitempty"`
	QualityNotes string   `json:"qualityNotes,omitempty" firestore:"qualityNotes,omitempty"`
}

// Decision captures a decision with actors (entities) and normalized scores
type Decision struct {
	ID        string   `json:"id,omitempty" firestore:"id,omitempty"`
	Title     string   `json:"title" firestore:"title"`
	Rationale string   `json:"rationale" firestore:"rationale"`
	Options   []string `json:"options,omitempty" firestore:"options,omitempty"`
	Selected  string   `json:"selected,omitempty" firestore:"selected,omitempty"`
	Criteria  []struct {
		Name  string  `json:"name" firestore:"name"`
		Score float64 `json:"score" firestore:"score"`
	} `json:"criteria,omitempty" firestore:"criteria,omitempty"`
	Actors []struct {
		ID   string `json:"id,omitempty" firestore:"id,omitempty"`
		Name string `json:"name" firestore:"name"`
		Type string `json:"type,omitempty" firestore:"type,omitempty"`
	} `json:"actors,omitempty" firestore:"actors,omitempty"`
	Evidence   []string `json:"evidence,omitempty" firestore:"evidence,omitempty"`
	SourceIDs  []string `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	Severity   float64  `json:"severity,omitempty" firestore:"severity,omitempty"`
	Confidence float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

// --- Subscription & Billing ---

// SubscriptionTier represents subscription tier levels
type SubscriptionTier string

const (
	SubscriptionTierFree SubscriptionTier = "free"
	SubscriptionTierPro  SubscriptionTier = "pro"
	SubscriptionTierMax  SubscriptionTier = "max"
)

// Subscription represents a user's subscription
type Subscription struct {
	ID       string `json:"id" firestore:"id"`
	TenantID string `json:"tenantId" firestore:"tenantId"`
	UserID   string `json:"userId" firestore:"userId"`

	// Subscription tier: free, pro, max
	Tier SubscriptionTier `json:"tier" firestore:"tier"`

	// Subscription status: active, expired, cancelled
	Status string `json:"status" firestore:"status"` // active|expired|cancelled

	// Billing cycle information
	StartedAt          time.Time  `json:"startedAt" firestore:"startedAt"`                                 // When subscription started
	CurrentPeriodStart time.Time  `json:"currentPeriodStart" firestore:"currentPeriodStart"`               // Start of current billing period
	CurrentPeriodEnd   time.Time  `json:"currentPeriodEnd" firestore:"currentPeriodEnd"`                   // End of current billing period
	NextBillingDate    *time.Time `json:"nextBillingDate,omitempty" firestore:"nextBillingDate,omitempty"` // Next billing date (null for free tier)

	// Cancellation: if true, subscription will be cancelled at period end (no auto-renewal)
	CancelAtPeriodEnd bool `json:"cancelAtPeriodEnd" firestore:"cancelAtPeriodEnd"`

	// Voucher information (if subscription is based on a voucher)
	VoucherID *string `json:"voucherId,omitempty" firestore:"voucherId,omitempty"`

	// Payment information (for future payment integration)
	PaymentProvider *string `json:"paymentProvider,omitempty" firestore:"paymentProvider,omitempty"` // stripe, paypal, voucher, admin
	PaymentID       *string `json:"paymentId,omitempty" firestore:"paymentId,omitempty"`             // External payment subscription ID

	// PayPal-specific fields
	PayPalPlanID         *string `json:"paypalPlanId,omitempty" firestore:"paypalPlanId,omitempty"`
	PayPalAgreementID    *string `json:"paypalAgreementId,omitempty" firestore:"paypalAgreementId,omitempty"`
	PayPalSubscriptionID *string `json:"paypalSubscriptionId,omitempty" firestore:"paypalSubscriptionId,omitempty"`

	// Metadata
	Metadata map[string]any `json:"metadata,omitempty" firestore:"metadata,omitempty"`

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

// Voucher represents a promotional code/voucher for subscription
type Voucher struct {
	ID   string `json:"id" firestore:"id"`
	Code string `json:"code" firestore:"code"` // Unique voucher code (e.g., "PRO2024Q1")

	// Voucher tier: pro, max (free tier doesn't need vouchers)
	Tier SubscriptionTier `json:"tier" firestore:"tier"` // pro|max

	// Validity period
	ValidFrom time.Time  `json:"validFrom" firestore:"validFrom"`
	ValidTo   *time.Time `json:"validTo,omitempty" firestore:"validTo,omitempty"` // nil = no expiration

	// Duration: how many months the voucher grants
	DurationMonths int `json:"durationMonths" firestore:"durationMonths"` // e.g., 1, 3, 6, 12

	// Usage tracking
	MaxUses   *int `json:"maxUses,omitempty" firestore:"maxUses,omitempty"` // nil = unlimited
	UsedCount int  `json:"usedCount" firestore:"usedCount"`                 // Current usage count
	IsActive  bool `json:"isActive" firestore:"isActive"`                   // Admin can deactivate without deleting

	// Created by admin
	CreatedBy string  `json:"createdBy" firestore:"createdBy"`           // Admin user ID
	Note      *string `json:"note,omitempty" firestore:"note,omitempty"` // Admin note

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

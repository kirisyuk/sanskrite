package models

import "time"

// SourceDetails хранит параметры, специфичные для типа источника
type SourceDetails struct {
	// web
	URI *string `json:"uri,omitempty"`

	// rss
	RSSURL *string `json:"rssUrl,omitempty"`

	// topic_based_interest
	SearchQuery         *string `json:"searchQuery,omitempty"`
	OriginalSearchQuery *string `json:"originalSearchQuery,omitempty"`

	// telegram_channel
	ChannelUsername        *string `json:"channelUsername,omitempty"`
	LastProcessedMessageID *int    `json:"lastProcessedMessageId,omitempty"`

	// twitter_search
	SinceTweetID *string `json:"sinceTweetId,omitempty"`

	// document
	OriginalFileName *string `json:"originalFileName,omitempty"`
	ContentType      *string `json:"contentType,omitempty"`

	// legacy compatibility
	SourceURI *string `json:"sourceUri,omitempty"`
}

// DataSource соответствует Core.DataSources
type DataSource struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Pk          string `json:"pk"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`

	ProjectID         *string  `json:"projectId,omitempty"`
	RelatedProjectIDs []string `json:"relatedProjectIds,omitempty"` // Additional projects this source is linked to
	ResearchID        *string  `json:"researchId,omitempty"`
	SessionID         *string  `json:"sessionId,omitempty"`

	OwnerUserID string        `json:"ownerUserId"`
	Details     SourceDetails `json:"sourceDetails"`

	// Source profile data (from AI analysis)
	Profile *SourceProfileData `json:"profile,omitempty"`

	LastPolledAt *time.Time `json:"lastPolledAt,omitempty"`
	LastFoundAt  *time.Time `json:"lastFoundAt,omitempty"`

	ErrorCount int `json:"errorCount"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	TrustLevel float64   `json:"trustLevel"`
	FirstSeen  time.Time `json:"firstSeenAt"`
	LastSeen   time.Time `json:"lastSeenAt"`
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

// User соответствует Core.Users
type User struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	Email    string `json:"email"`

	DisplayName *string        `json:"displayName,omitempty"`
	AuthType    *string        `json:"authType,omitempty"`
	Role        *string        `json:"role,omitempty"`
	Status      *string        `json:"status,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	SchemaVersion string `json:"schemaVersion"`

	CompanyRef        *string  `json:"companyRef,omitempty"`
	CompanyConfidence *float64 `json:"companyConfidence,omitempty"`
	JobTitle          *string  `json:"jobTitle,omitempty"`
	Department        *string  `json:"department,omitempty"`
	OrgUnitRef        *string  `json:"orgUnitRef,omitempty"`

	DefaultProjectID *string           `json:"defaultProjectId,omitempty"`
	ProjectRoles     map[string]string `json:"projectRoles,omitempty"`

	AssignedGoalIDs       []string `json:"assignedGoalIds,omitempty"`
	AssignedInitiativeIDs []string `json:"assignedInitiativeIds,omitempty"`
}

// Project соответствует Core.Projects
type Project struct {
	ID          string `json:"id"`
	Pk          string `json:"pk"`
	TenantID    string `json:"tenantId"`
	DisplayName string `json:"displayName"`

	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`

	OwnerUserID string    `json:"ownerUserId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// For Discovery chat sorting
	LastInteractionAt *time.Time `json:"lastInteractionAt,omitempty"`

	// View type for filtering (e.g., "discovery", "workspace")
	View *string `json:"view,omitempty"`

	IsExternal                   *bool   `json:"isExternal,omitempty"`
	LinkedProjectRef             *string `json:"linkedProjectRef,omitempty"`
	SharingPolicy                *string `json:"sharingPolicy,omitempty"`
	OntologyOverlayRef           *string `json:"ontologyOverlayRef,omitempty"`
	CompanyRef                   *string `json:"companyRef,omitempty"`
	OrganizationName             *string `json:"organizationName,omitempty"`
	BusinessUnitName             *string `json:"businessUnitName,omitempty"`
	EnableRiskProfileSuggestions *bool   `json:"enableRiskProfileSuggestions,omitempty"`
	AiGuided                     *bool   `json:"aiguided,omitempty"`
}

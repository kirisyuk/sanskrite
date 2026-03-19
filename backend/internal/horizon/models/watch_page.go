package models

import (
	"fmt"
	"strings"
	"time"
)

type WatchPageBuildStatus string

const (
	WatchPageBuildStatusNotBuilt WatchPageBuildStatus = "NOT_BUILT"
	WatchPageBuildStatusReady    WatchPageBuildStatus = "READY"
	WatchPageBuildStatusStale    WatchPageBuildStatus = "STALE"
	WatchPageBuildStatusFailed   WatchPageBuildStatus = "FAILED"
)

type WatchPageHero struct {
	Eyebrow     string    `json:"eyebrow,omitempty" firestore:"eyebrow,omitempty"`
	Title       string    `json:"title,omitempty" firestore:"title,omitempty"`
	Summary     string    `json:"summary,omitempty" firestore:"summary,omitempty"`
	WhyNow      string    `json:"whyNow,omitempty" firestore:"whyNow,omitempty"`
	TimeHorizon string    `json:"timeHorizon,omitempty" firestore:"timeHorizon,omitempty"`
	RiskPosture string    `json:"riskPosture,omitempty" firestore:"riskPosture,omitempty"`
	SourceRefs  []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BackingIDs  []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageDashboardMetric struct {
	ID             string    `json:"id" firestore:"id"`
	Title          string    `json:"title" firestore:"title"`
	Value          string    `json:"value" firestore:"value"`
	Subtitle       string    `json:"subtitle,omitempty" firestore:"subtitle,omitempty"`
	Tone           string    `json:"tone,omitempty" firestore:"tone,omitempty"`
	DeltaValue     string    `json:"deltaValue,omitempty" firestore:"deltaValue,omitempty"`
	DeltaDirection string    `json:"deltaDirection,omitempty" firestore:"deltaDirection,omitempty"`
	Caption        string    `json:"caption,omitempty" firestore:"caption,omitempty"`
	SourceRefs     []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BackingIDs     []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt    time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageRiskItem struct {
	ID            string    `json:"id" firestore:"id"`
	Title         string    `json:"title" firestore:"title"`
	Summary       string    `json:"summary,omitempty" firestore:"summary,omitempty"`
	RiskPosture   string    `json:"riskPosture,omitempty" firestore:"riskPosture,omitempty"`
	CanonicalPath string    `json:"canonicalPath,omitempty" firestore:"canonicalPath,omitempty"`
	Mechanisms    []string  `json:"mechanisms,omitempty" firestore:"mechanisms,omitempty"`
	SourceRefs    []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BackingIDs    []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt   time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageAffectedItem struct {
	ID            string    `json:"id" firestore:"id"`
	Label         string    `json:"label" firestore:"label"`
	CanonicalPath string    `json:"canonicalPath" firestore:"canonicalPath"`
	Reason        string    `json:"reason,omitempty" firestore:"reason,omitempty"`
	Rank          int       `json:"rank,omitempty" firestore:"rank,omitempty"`
	SourceRefs    []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BackingIDs    []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt   time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageScopeItem struct {
	ID            string    `json:"id" firestore:"id"`
	Label         string    `json:"label" firestore:"label"`
	CanonicalPath string    `json:"canonicalPath" firestore:"canonicalPath"`
	Kind          string    `json:"kind,omitempty" firestore:"kind,omitempty"`
	Summary       string    `json:"summary,omitempty" firestore:"summary,omitempty"`
	SourceRefs    []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BackingIDs    []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt   time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageSupplyPath struct {
	ID                string    `json:"id" firestore:"id"`
	Title             string    `json:"title" firestore:"title"`
	Summary           string    `json:"summary,omitempty" firestore:"summary,omitempty"`
	ProductAxis       string    `json:"productAxis,omitempty" firestore:"productAxis,omitempty"`
	AnchorSupplier    string    `json:"anchorSupplier,omitempty" firestore:"anchorSupplier,omitempty"`
	AnchorCustomer    string    `json:"anchorCustomer,omitempty" firestore:"anchorCustomer,omitempty"`
	StressScore       float64   `json:"stressScore,omitempty" firestore:"stressScore,omitempty"`
	BindingConfidence float64   `json:"bindingConfidence,omitempty" firestore:"bindingConfidence,omitempty"`
	RouteRefs         []string  `json:"routeRefs,omitempty" firestore:"routeRefs,omitempty"`
	AffectedZoneRefs  []string  `json:"affectedZoneRefs,omitempty" firestore:"affectedZoneRefs,omitempty"`
	BackingIDs        []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt       time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageSignalItem struct {
	ID             string    `json:"id" firestore:"id"`
	Kind           string    `json:"kind" firestore:"kind"`
	Title          string    `json:"title" firestore:"title"`
	Summary        string    `json:"summary,omitempty" firestore:"summary,omitempty"`
	CanonicalPaths []string  `json:"canonicalPaths,omitempty" firestore:"canonicalPaths,omitempty"`
	SourceLabel    string    `json:"sourceLabel,omitempty" firestore:"sourceLabel,omitempty"`
	SourceKind     string    `json:"sourceKind,omitempty" firestore:"sourceKind,omitempty"`
	BackingIDs     []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt    time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageScenarioItem struct {
	ID                string    `json:"id" firestore:"id"`
	Title             string    `json:"title" firestore:"title"`
	Description       string    `json:"description,omitempty" firestore:"description,omitempty"`
	TargetPaths       []string  `json:"targetPaths,omitempty" firestore:"targetPaths,omitempty"`
	TriggerConditions []string  `json:"triggerConditions,omitempty" firestore:"triggerConditions,omitempty"`
	Disconfirmers     []string  `json:"disconfirmers,omitempty" firestore:"disconfirmers,omitempty"`
	Summary           string    `json:"summary,omitempty" firestore:"summary,omitempty"`
	BackingIDs        []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt       time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageMapFragment struct {
	ID             string    `json:"id" firestore:"id"`
	Title          string    `json:"title" firestore:"title"`
	Caption        string    `json:"caption,omitempty" firestore:"caption,omitempty"`
	CanonicalPaths []string  `json:"canonicalPaths,omitempty" firestore:"canonicalPaths,omitempty"`
	LayerHints     []string  `json:"layerHints,omitempty" firestore:"layerHints,omitempty"`
	ScopeType      string    `json:"scopeType,omitempty" firestore:"scopeType,omitempty"`
	BriefID        string    `json:"briefId,omitempty" firestore:"briefId,omitempty"`
	BackingIDs     []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	GeneratedAt    time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageEvidenceItem struct {
	ID             string    `json:"id" firestore:"id"`
	Kind           string    `json:"kind" firestore:"kind"`
	Title          string    `json:"title" firestore:"title"`
	Summary        string    `json:"summary,omitempty" firestore:"summary,omitempty"`
	CanonicalPaths []string  `json:"canonicalPaths,omitempty" firestore:"canonicalPaths,omitempty"`
	BriefID        string    `json:"briefId,omitempty" firestore:"briefId,omitempty"`
	SourceRefs     []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	GeneratedAt    time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageSectionEnrichment struct {
	Key         string    `json:"key" firestore:"key"`
	Content     string    `json:"content,omitempty" firestore:"content,omitempty"`
	SourceRefs  []string  `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	BackingIDs  []string  `json:"backingIds,omitempty" firestore:"backingIds,omitempty"`
	Model       string    `json:"model,omitempty" firestore:"model,omitempty"`
	Fallback    bool      `json:"fallback,omitempty" firestore:"fallback,omitempty"`
	GeneratedAt time.Time `json:"generatedAt,omitempty" firestore:"generatedAt,omitempty"`
}

type WatchPageSnapshot struct {
	ID                     string                                `json:"id" firestore:"id"`
	TenantID               string                                `json:"tenantId" firestore:"tenantId"`
	LensID                 string                                `json:"lensId" firestore:"lensId"`
	LensName               string                                `json:"lensName,omitempty" firestore:"lensName,omitempty"`
	ProjectID              string                                `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	TimeHorizon            string                                `json:"timeHorizon,omitempty" firestore:"timeHorizon,omitempty"`
	InterestTitle          string                                `json:"interestTitle,omitempty" firestore:"interestTitle,omitempty"`
	InterestQuery          string                                `json:"interestQuery,omitempty" firestore:"interestQuery,omitempty"`
	QueryOverrides         []string                              `json:"queryOverrides,omitempty" firestore:"queryOverrides,omitempty"`
	PageFraming            map[string]any                        `json:"pageFraming,omitempty" firestore:"pageFraming,omitempty"`
	BuildStatus            WatchPageBuildStatus                  `json:"buildStatus" firestore:"buildStatus"`
	BuildError             string                                `json:"buildError,omitempty" firestore:"buildError,omitempty"`
	FreshAt                time.Time                             `json:"freshAt" firestore:"freshAt"`
	StaleAfterAt           time.Time                             `json:"staleAfterAt" firestore:"staleAfterAt"`
	LastRefreshRequestedAt *time.Time                            `json:"lastRefreshRequestedAt,omitempty" firestore:"lastRefreshRequestedAt,omitempty"`
	Hero                   WatchPageHero                         `json:"hero" firestore:"hero"`
	Anchors                []WatchPageScopeItem                  `json:"anchors,omitempty" firestore:"anchors,omitempty"`
	Reach                  []WatchPageScopeItem                  `json:"reach,omitempty" firestore:"reach,omitempty"`
	Products               []WatchPageScopeItem                  `json:"products,omitempty" firestore:"products,omitempty"`
	SupplyPaths            []WatchPageSupplyPath                 `json:"supplyPaths,omitempty" firestore:"supplyPaths,omitempty"`
	WhatIsAtRisk           []WatchPageRiskItem                   `json:"whatIsAtRisk,omitempty" firestore:"whatIsAtRisk,omitempty"`
	Affected               []WatchPageAffectedItem               `json:"affected,omitempty" firestore:"affected,omitempty"`
	DirectlyAffected       []WatchPageAffectedItem               `json:"directlyAffected,omitempty" firestore:"directlyAffected,omitempty"`
	IndirectlyAffected     []WatchPageAffectedItem               `json:"indirectlyAffected,omitempty" firestore:"indirectlyAffected,omitempty"`
	Dashboards             []WatchPageDashboardMetric            `json:"dashboards,omitempty" firestore:"dashboards,omitempty"`
	MapFragments           []WatchPageMapFragment                `json:"mapFragments,omitempty" firestore:"mapFragments,omitempty"`
	Signals                []WatchPageSignalItem                 `json:"signals,omitempty" firestore:"signals,omitempty"`
	Scenarios              []WatchPageScenarioItem               `json:"scenarios,omitempty" firestore:"scenarios,omitempty"`
	Evidence               []WatchPageEvidenceItem               `json:"evidence,omitempty" firestore:"evidence,omitempty"`
	SectionEnrichments     map[string]WatchPageSectionEnrichment `json:"sectionEnrichments,omitempty" firestore:"sectionEnrichments,omitempty"`
	Metadata               map[string]any                        `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt              time.Time                             `json:"createdAt" firestore:"createdAt"`
	UpdatedAt              time.Time                             `json:"updatedAt" firestore:"updatedAt"`
}

func (s *WatchPageSnapshot) Validate() error {
	if s == nil {
		return fmt.Errorf("watch page snapshot is required")
	}
	if strings.TrimSpace(s.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(s.LensID) == "" {
		return fmt.Errorf("lensId is required")
	}
	switch s.BuildStatus {
	case WatchPageBuildStatusNotBuilt, WatchPageBuildStatusReady, WatchPageBuildStatusStale, WatchPageBuildStatusFailed:
	default:
		return fmt.Errorf("invalid buildStatus")
	}
	return nil
}

type WatchPageHistoryEntry struct {
	ID         string         `json:"id" firestore:"id"`
	TenantID   string         `json:"tenantId" firestore:"tenantId"`
	LensID     string         `json:"lensId" firestore:"lensId"`
	SnapshotID string         `json:"snapshotId,omitempty" firestore:"snapshotId,omitempty"`
	EntryType  string         `json:"entryType" firestore:"entryType"`
	Key        string         `json:"key" firestore:"key"`
	Payload    map[string]any `json:"payload,omitempty" firestore:"payload,omitempty"`
	CreatedAt  time.Time      `json:"createdAt" firestore:"createdAt"`
}

func (e *WatchPageHistoryEntry) Validate() error {
	if e == nil {
		return fmt.Errorf("watch page history entry is required")
	}
	if strings.TrimSpace(e.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(e.LensID) == "" {
		return fmt.Errorf("lensId is required")
	}
	if strings.TrimSpace(e.EntryType) == "" {
		return fmt.Errorf("entryType is required")
	}
	if strings.TrimSpace(e.Key) == "" {
		return fmt.Errorf("key is required")
	}
	return nil
}

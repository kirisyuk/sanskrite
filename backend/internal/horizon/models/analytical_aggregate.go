package models

import (
	"strconv"
	"strings"
	"time"
)

type AnalyticalAggregate struct {
	ID                  string     `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID            string     `json:"tenantId" firestore:"tenantId"`
	ScopeType           string     `json:"scopeType,omitempty" firestore:"scopeType,omitempty"`
	ScopeRef            string     `json:"scopeRef" firestore:"scopeRef"`
	AggregateType       string     `json:"aggregateType" firestore:"aggregateType"`
	Layer               string     `json:"layer,omitempty" firestore:"layer,omitempty"`
	SourceClass         string     `json:"sourceClass,omitempty" firestore:"sourceClass,omitempty"`
	SubjectRef          string     `json:"subjectRef,omitempty" firestore:"subjectRef,omitempty"`
	LensRefs            []string   `json:"lensRefs,omitempty" firestore:"lensRefs,omitempty"`
	WatchSubjectIDs     []string   `json:"watchSubjectIds,omitempty" firestore:"watchSubjectIds,omitempty"`
	ProductRefs         []string   `json:"productRefs,omitempty" firestore:"productRefs,omitempty"`
	NodeRefs            []string   `json:"nodeRefs,omitempty" firestore:"nodeRefs,omitempty"`
	RouteRefs           []string   `json:"routeRefs,omitempty" firestore:"routeRefs,omitempty"`
	ZoneRefs            []string   `json:"zoneRefs,omitempty" firestore:"zoneRefs,omitempty"`
	AffectedRefs        []string   `json:"affectedRefs,omitempty" firestore:"affectedRefs,omitempty"`
	BasisRefs           []string   `json:"basisRefs,omitempty" firestore:"basisRefs,omitempty"`
	DerivedPrimitiveIDs []string   `json:"derivedPrimitiveIds,omitempty" firestore:"derivedPrimitiveIds,omitempty"`
	PrimitiveTypes      []string   `json:"primitiveTypes,omitempty" firestore:"primitiveTypes,omitempty"`
	PrimitiveCount      int        `json:"primitiveCount,omitempty" firestore:"primitiveCount,omitempty"`
	BasisDigest         string     `json:"basisDigest,omitempty" firestore:"basisDigest,omitempty"`
	RefreshClass        string     `json:"refreshClass,omitempty" firestore:"refreshClass,omitempty"`
	RecomputeTriggers   []string   `json:"recomputeTriggers,omitempty" firestore:"recomputeTriggers,omitempty"`
	SeverityScore       float64    `json:"severityScore,omitempty" firestore:"severityScore,omitempty"`
	SeverityBand        string     `json:"severityBand,omitempty" firestore:"severityBand,omitempty"`
	Confidence          float64    `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	CoverageStatus      string     `json:"coverageStatus,omitempty" firestore:"coverageStatus,omitempty"`
	GateStatus          string     `json:"gateStatus,omitempty" firestore:"gateStatus,omitempty"`
	Explanation         string     `json:"explanation,omitempty" firestore:"explanation,omitempty"`
	MissingData         []string   `json:"missingData,omitempty" firestore:"missingData,omitempty"`
	ComputedAt          time.Time  `json:"computedAt,omitempty" firestore:"computedAt,omitempty"`
	StaleAt             *time.Time `json:"staleAt,omitempty" firestore:"staleAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt" firestore:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt" firestore:"updatedAt"`
}

func (a *AnalyticalAggregate) Normalize() {
	if a == nil {
		return
	}
	now := time.Now().UTC()
	a.TenantID = strings.TrimSpace(a.TenantID)
	a.ScopeType = strings.TrimSpace(a.ScopeType)
	a.ScopeRef = strings.TrimSpace(a.ScopeRef)
	a.AggregateType = strings.TrimSpace(a.AggregateType)
	a.Layer = strings.TrimSpace(a.Layer)
	a.SourceClass = strings.TrimSpace(a.SourceClass)
	a.SubjectRef = strings.TrimSpace(a.SubjectRef)
	a.BasisDigest = strings.TrimSpace(a.BasisDigest)
	a.RefreshClass = strings.TrimSpace(a.RefreshClass)
	a.CoverageStatus = strings.TrimSpace(a.CoverageStatus)
	a.GateStatus = strings.TrimSpace(a.GateStatus)
	a.Explanation = strings.TrimSpace(a.Explanation)
	a.SeverityBand = normalizeSeverityBand(a.SeverityBand, a.SeverityScore)
	a.LensRefs = uniqueSortedRefs(a.LensRefs)
	a.WatchSubjectIDs = uniqueSortedStrings(a.WatchSubjectIDs)
	a.ProductRefs = uniqueSortedRefs(a.ProductRefs)
	a.NodeRefs = uniqueSortedRefs(a.NodeRefs)
	a.RouteRefs = uniqueSortedRefs(a.RouteRefs)
	a.ZoneRefs = uniqueSortedRefs(a.ZoneRefs)
	a.AffectedRefs = uniqueSortedRefs(a.AffectedRefs)
	a.BasisRefs = uniqueSortedRefs(a.BasisRefs)
	a.DerivedPrimitiveIDs = uniqueSortedStrings(a.DerivedPrimitiveIDs)
	a.PrimitiveTypes = uniqueSortedStrings(a.PrimitiveTypes)
	a.RecomputeTriggers = uniqueSortedStrings(a.RecomputeTriggers)
	a.MissingData = uniqueSortedStrings(a.MissingData)
	if a.ComputedAt.IsZero() {
		a.ComputedAt = now
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
}

func (a *AnalyticalAggregate) GenerateVectorText() string {
	if a == nil {
		return ""
	}
	parts := make([]string, 0, 14)
	if a.AggregateType != "" {
		parts = append(parts, "Aggregate: "+a.AggregateType)
	}
	if a.ScopeRef != "" {
		parts = append(parts, "Scope: "+a.ScopeRef)
	}
	if a.SubjectRef != "" {
		parts = append(parts, "Subject: "+a.SubjectRef)
	}
	if len(a.LensRefs) > 0 {
		parts = append(parts, "Lenses: "+strings.Join(a.LensRefs, ", "))
	}
	if len(a.ProductRefs) > 0 {
		parts = append(parts, "Products: "+strings.Join(a.ProductRefs, ", "))
	}
	if len(a.NodeRefs) > 0 {
		parts = append(parts, "Nodes: "+strings.Join(a.NodeRefs, ", "))
	}
	if len(a.RouteRefs) > 0 {
		parts = append(parts, "Routes: "+strings.Join(a.RouteRefs, ", "))
	}
	if len(a.ZoneRefs) > 0 {
		parts = append(parts, "Zones: "+strings.Join(a.ZoneRefs, ", "))
	}
	if a.PrimitiveCount > 0 {
		parts = append(parts, "PrimitiveCount: "+strconv.Itoa(a.PrimitiveCount))
	}
	if len(a.PrimitiveTypes) > 0 {
		parts = append(parts, "PrimitiveTypes: "+strings.Join(a.PrimitiveTypes, ", "))
	}
	if a.SeverityScore > 0 {
		parts = append(parts, "Severity: "+formatFloatCompact(a.SeverityScore))
	}
	if a.Confidence > 0 {
		parts = append(parts, "Confidence: "+formatFloatCompact(a.Confidence))
	}
	if a.Explanation != "" {
		parts = append(parts, "Explanation: "+a.Explanation)
	}
	if len(a.MissingData) > 0 {
		parts = append(parts, "Missing: "+strings.Join(a.MissingData, ", "))
	}
	return strings.Join(parts, "\n")
}

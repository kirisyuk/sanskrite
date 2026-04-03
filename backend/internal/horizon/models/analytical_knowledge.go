package models

import (
	"strconv"
	"strings"
	"time"
)

type AnalyticalPrimitive struct {
	ID                string                 `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID          string                 `json:"tenantId" firestore:"tenantId"`
	ScopeType         string                 `json:"scopeType,omitempty" firestore:"scopeType,omitempty"`
	ScopeRef          string                 `json:"scopeRef" firestore:"scopeRef"`
	PrimitiveType     string                 `json:"primitiveType" firestore:"primitiveType"`
	Layer             string                 `json:"layer,omitempty" firestore:"layer,omitempty"`
	SourceClass       string                 `json:"sourceClass,omitempty" firestore:"sourceClass,omitempty"`
	RefreshClass      string                 `json:"refreshClass,omitempty" firestore:"refreshClass,omitempty"`
	SubjectRef        string                 `json:"subjectRef,omitempty" firestore:"subjectRef,omitempty"`
	ProductRefs       []string               `json:"productRefs,omitempty" firestore:"productRefs,omitempty"`
	NodeRefs          []string               `json:"nodeRefs,omitempty" firestore:"nodeRefs,omitempty"`
	RouteRefs         []string               `json:"routeRefs,omitempty" firestore:"routeRefs,omitempty"`
	ZoneRefs          []string               `json:"zoneRefs,omitempty" firestore:"zoneRefs,omitempty"`
	AffectedRefs      []string               `json:"affectedRefs,omitempty" firestore:"affectedRefs,omitempty"`
	BasisRefs         []string               `json:"basisRefs,omitempty" firestore:"basisRefs,omitempty"`
	BasisDigest       string                 `json:"basisDigest,omitempty" firestore:"basisDigest,omitempty"`
	RecomputeTriggers []string               `json:"recomputeTriggers,omitempty" firestore:"recomputeTriggers,omitempty"`
	SeverityScore     float64                `json:"severityScore,omitempty" firestore:"severityScore,omitempty"`
	SeverityBand      string                 `json:"severityBand,omitempty" firestore:"severityBand,omitempty"`
	Confidence        float64                `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	CoverageStatus    string                 `json:"coverageStatus,omitempty" firestore:"coverageStatus,omitempty"`
	GateStatus        string                 `json:"gateStatus,omitempty" firestore:"gateStatus,omitempty"`
	Explanation       string                 `json:"explanation,omitempty" firestore:"explanation,omitempty"`
	MissingData       []string               `json:"missingData,omitempty" firestore:"missingData,omitempty"`
	Qualifiers        map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
	EvidenceFactIDs   []string               `json:"evidenceFactIds,omitempty" firestore:"evidenceFactIds,omitempty"`
	SourceIDs         []string               `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	ComputedAt        time.Time              `json:"computedAt,omitempty" firestore:"computedAt,omitempty"`
	StaleAt           *time.Time             `json:"staleAt,omitempty" firestore:"staleAt,omitempty"`
	CreatedAt         time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

func (p *AnalyticalPrimitive) Normalize() {
	if p == nil {
		return
	}
	now := time.Now().UTC()
	p.TenantID = strings.TrimSpace(p.TenantID)
	p.ScopeType = strings.TrimSpace(p.ScopeType)
	p.ScopeRef = strings.TrimSpace(p.ScopeRef)
	p.PrimitiveType = strings.TrimSpace(p.PrimitiveType)
	p.Layer = strings.TrimSpace(p.Layer)
	p.SourceClass = strings.TrimSpace(p.SourceClass)
	p.RefreshClass = strings.TrimSpace(p.RefreshClass)
	p.SubjectRef = strings.TrimSpace(p.SubjectRef)
	p.BasisDigest = strings.TrimSpace(p.BasisDigest)
	p.CoverageStatus = strings.TrimSpace(p.CoverageStatus)
	p.GateStatus = strings.TrimSpace(p.GateStatus)
	p.Explanation = strings.TrimSpace(p.Explanation)
	p.SeverityBand = normalizeSeverityBand(p.SeverityBand, p.SeverityScore)
	p.ProductRefs = uniqueSortedRefs(p.ProductRefs)
	p.NodeRefs = uniqueSortedRefs(p.NodeRefs)
	p.RouteRefs = uniqueSortedRefs(p.RouteRefs)
	p.ZoneRefs = uniqueSortedRefs(p.ZoneRefs)
	p.AffectedRefs = uniqueSortedRefs(p.AffectedRefs)
	p.BasisRefs = uniqueSortedRefs(p.BasisRefs)
	p.RecomputeTriggers = uniqueSortedStrings(p.RecomputeTriggers)
	p.MissingData = uniqueSortedStrings(p.MissingData)
	p.EvidenceFactIDs = uniqueSortedStrings(p.EvidenceFactIDs)
	p.SourceIDs = uniqueSortedStrings(p.SourceIDs)
	if p.ComputedAt.IsZero() {
		p.ComputedAt = now
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
}

func (p *AnalyticalPrimitive) GenerateVectorText() string {
	if p == nil {
		return ""
	}
	parts := make([]string, 0, 12)
	if p.PrimitiveType != "" {
		parts = append(parts, "Primitive: "+p.PrimitiveType)
	}
	if p.ScopeRef != "" {
		parts = append(parts, "Scope: "+p.ScopeRef)
	}
	if p.SubjectRef != "" {
		parts = append(parts, "Subject: "+p.SubjectRef)
	}
	if len(p.ProductRefs) > 0 {
		parts = append(parts, "Products: "+strings.Join(p.ProductRefs, ", "))
	}
	if len(p.NodeRefs) > 0 {
		parts = append(parts, "Nodes: "+strings.Join(p.NodeRefs, ", "))
	}
	if len(p.RouteRefs) > 0 {
		parts = append(parts, "Routes: "+strings.Join(p.RouteRefs, ", "))
	}
	if len(p.ZoneRefs) > 0 {
		parts = append(parts, "Zones: "+strings.Join(p.ZoneRefs, ", "))
	}
	if len(p.AffectedRefs) > 0 {
		parts = append(parts, "Affected: "+strings.Join(p.AffectedRefs, ", "))
	}
	if p.SeverityScore > 0 {
		parts = append(parts, "Severity: "+formatFloatCompact(p.SeverityScore))
	}
	if p.Confidence > 0 {
		parts = append(parts, "Confidence: "+formatFloatCompact(p.Confidence))
	}
	if p.Explanation != "" {
		parts = append(parts, "Explanation: "+p.Explanation)
	}
	if len(p.MissingData) > 0 {
		parts = append(parts, "Missing: "+strings.Join(p.MissingData, ", "))
	}
	return strings.Join(parts, "\n")
}

func normalizeSeverityBand(raw string, score float64) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw != "" {
		return raw
	}
	switch {
	case score >= 0.8:
		return "critical"
	case score >= 0.6:
		return "high"
	case score >= 0.35:
		return "medium"
	case score > 0:
		return "low"
	default:
		return ""
	}
}

func formatFloatCompact(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}

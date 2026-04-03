package models

import (
	"fmt"
	"strings"
	"time"
)

type ChangedRefEvent struct {
	TenantID      string    `json:"tenantId" firestore:"tenantId"`
	Ref           string    `json:"ref" firestore:"ref"`
	RefType       string    `json:"refType,omitempty" firestore:"refType,omitempty"`
	ChangeKind    string    `json:"changeKind,omitempty" firestore:"changeKind,omitempty"`
	MetricName    string    `json:"metricName,omitempty" firestore:"metricName,omitempty"`
	OldValue      *float64  `json:"oldValue,omitempty" firestore:"oldValue,omitempty"`
	NewValue      *float64  `json:"newValue,omitempty" firestore:"newValue,omitempty"`
	SourceClass   string    `json:"sourceClass,omitempty" firestore:"sourceClass,omitempty"`
	ChangedAt     time.Time `json:"changedAt" firestore:"changedAt"`
	CorrelationID string    `json:"correlationId,omitempty" firestore:"correlationId,omitempty"`
}

func (e *ChangedRefEvent) Normalize() {
	if e == nil {
		return
	}
	e.TenantID = strings.TrimSpace(e.TenantID)
	e.Ref = strings.TrimSpace(e.Ref)
	e.RefType = strings.TrimSpace(e.RefType)
	e.ChangeKind = strings.TrimSpace(e.ChangeKind)
	e.MetricName = strings.TrimSpace(e.MetricName)
	e.SourceClass = strings.TrimSpace(e.SourceClass)
	e.CorrelationID = strings.TrimSpace(e.CorrelationID)
	if e.ChangedAt.IsZero() {
		e.ChangedAt = time.Now().UTC()
	}
}

func (e *ChangedRefEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("changed ref event is required")
	}
	e.Normalize()
	if e.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if e.Ref == "" {
		return fmt.Errorf("ref is required")
	}
	return nil
}

type AffectedScopeDelta struct {
	TenantID           string                  `json:"tenantId" firestore:"tenantId"`
	ChangedRefs        []string                `json:"changedRefs,omitempty" firestore:"changedRefs,omitempty"`
	FragmentRefs       []string                `json:"fragmentRefs,omitempty" firestore:"fragmentRefs,omitempty"`
	PathFamilyIDs      []string                `json:"pathFamilyIds,omitempty" firestore:"pathFamilyIds,omitempty"`
	LensIDs            []string                `json:"lensIds,omitempty" firestore:"lensIds,omitempty"`
	WatchSubjectIDs    []string                `json:"watchSubjectIds,omitempty" firestore:"watchSubjectIds,omitempty"`
	AggregateScopeRefs []string                `json:"aggregateScopeRefs,omitempty" firestore:"aggregateScopeRefs,omitempty"`
	PropagationPaths   [][]string              `json:"propagationPaths,omitempty" firestore:"propagationPaths,omitempty"`
	SeverityHint       float64                 `json:"severityHint,omitempty" firestore:"severityHint,omitempty"`
	Metadata           map[string]interface{}  `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	ComputedAt         time.Time               `json:"computedAt" firestore:"computedAt"`
}

func (d *AffectedScopeDelta) Normalize() {
	if d == nil {
		return
	}
	d.TenantID = strings.TrimSpace(d.TenantID)
	d.ChangedRefs = uniqueSortedRefs(d.ChangedRefs)
	d.FragmentRefs = uniqueSortedRefs(d.FragmentRefs)
	d.PathFamilyIDs = uniqueSortedStrings(d.PathFamilyIDs)
	d.LensIDs = uniqueSortedStrings(d.LensIDs)
	d.WatchSubjectIDs = uniqueSortedStrings(d.WatchSubjectIDs)
	d.AggregateScopeRefs = uniqueSortedRefs(d.AggregateScopeRefs)
	if d.ComputedAt.IsZero() {
		d.ComputedAt = time.Now().UTC()
	}
}

type RefScopeProjection struct {
	ID                     string    `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID               string    `json:"tenantId" firestore:"tenantId"`
	Ref                    string    `json:"ref" firestore:"ref"`
	LensIDs                []string  `json:"lensIds,omitempty" firestore:"lensIds,omitempty"`
	WatchSubjectIDs        []string  `json:"watchSubjectIds,omitempty" firestore:"watchSubjectIds,omitempty"`
	PathFamilyIDs          []string  `json:"pathFamilyIds,omitempty" firestore:"pathFamilyIds,omitempty"`
	AggregateScopeRefs     []string  `json:"aggregateScopeRefs,omitempty" firestore:"aggregateScopeRefs,omitempty"`
	MaterializedArtifactKeys []string `json:"materializedArtifactKeys,omitempty" firestore:"materializedArtifactKeys,omitempty"`
	UpdatedAt              time.Time `json:"updatedAt" firestore:"updatedAt"`
	BasisDigest            string    `json:"basisDigest,omitempty" firestore:"basisDigest,omitempty"`
}

func (p *RefScopeProjection) Normalize() {
	if p == nil {
		return
	}
	p.TenantID = strings.TrimSpace(p.TenantID)
	p.Ref = strings.TrimSpace(p.Ref)
	p.LensIDs = uniqueSortedStrings(p.LensIDs)
	p.WatchSubjectIDs = uniqueSortedStrings(p.WatchSubjectIDs)
	p.PathFamilyIDs = uniqueSortedStrings(p.PathFamilyIDs)
	p.AggregateScopeRefs = uniqueSortedRefs(p.AggregateScopeRefs)
	p.MaterializedArtifactKeys = uniqueSortedStrings(p.MaterializedArtifactKeys)
	p.BasisDigest = strings.TrimSpace(p.BasisDigest)
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
}

type PathFamilyProjection struct {
	ID                 string    `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID           string    `json:"tenantId" firestore:"tenantId"`
	FamilyID           string    `json:"familyId" firestore:"familyId"`
	LensIDs            []string  `json:"lensIds,omitempty" firestore:"lensIds,omitempty"`
	ArtifactKeys       []string  `json:"artifactKeys,omitempty" firestore:"artifactKeys,omitempty"`
	NodeRefs           []string  `json:"nodeRefs,omitempty" firestore:"nodeRefs,omitempty"`
	RouteRefs          []string  `json:"routeRefs,omitempty" firestore:"routeRefs,omitempty"`
	ZoneRefs           []string  `json:"zoneRefs,omitempty" firestore:"zoneRefs,omitempty"`
	AggregateScopeRefs []string  `json:"aggregateScopeRefs,omitempty" firestore:"aggregateScopeRefs,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt" firestore:"updatedAt"`
	BasisDigest        string    `json:"basisDigest,omitempty" firestore:"basisDigest,omitempty"`
}

func (p *PathFamilyProjection) Normalize() {
	if p == nil {
		return
	}
	p.TenantID = strings.TrimSpace(p.TenantID)
	p.FamilyID = strings.TrimSpace(p.FamilyID)
	p.LensIDs = uniqueSortedStrings(p.LensIDs)
	p.ArtifactKeys = uniqueSortedStrings(p.ArtifactKeys)
	p.NodeRefs = uniqueSortedRefs(p.NodeRefs)
	p.RouteRefs = uniqueSortedRefs(p.RouteRefs)
	p.ZoneRefs = uniqueSortedRefs(p.ZoneRefs)
	p.AggregateScopeRefs = uniqueSortedRefs(p.AggregateScopeRefs)
	p.BasisDigest = strings.TrimSpace(p.BasisDigest)
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
}

type CompiledWatchSubject struct {
	ID                       string                 `json:"id,omitempty" firestore:"id,omitempty"`
	WatchSubjectID           string                 `json:"watchSubjectId" firestore:"watchSubjectId"`
	TenantID                 string                 `json:"tenantId" firestore:"tenantId"`
	AnchorRef                string                 `json:"anchorRef" firestore:"anchorRef"`
	LensIDs                  []string               `json:"lensIds,omitempty" firestore:"lensIds,omitempty"`
	CompiledRefSet           []string               `json:"compiledRefSet,omitempty" firestore:"compiledRefSet,omitempty"`
	CompiledPathFamilyIDs    []string               `json:"compiledPathFamilyIds,omitempty" firestore:"compiledPathFamilyIds,omitempty"`
	CompiledAggregateScopeRefs []string            `json:"compiledAggregateScopeRefs,omitempty" firestore:"compiledAggregateScopeRefs,omitempty"`
	RuntimeMode              WatchSubjectRuntimeMode `json:"runtimeMode" firestore:"runtimeMode"`
	Priority                 int                    `json:"priority" firestore:"priority"`
	Metadata                 map[string]interface{} `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	UpdatedAt                time.Time              `json:"updatedAt" firestore:"updatedAt"`
	BasisDigest              string                 `json:"basisDigest,omitempty" firestore:"basisDigest,omitempty"`
}

func (c *CompiledWatchSubject) Normalize() {
	if c == nil {
		return
	}
	c.WatchSubjectID = strings.TrimSpace(c.WatchSubjectID)
	c.TenantID = strings.TrimSpace(c.TenantID)
	c.AnchorRef = strings.TrimSpace(c.AnchorRef)
	c.LensIDs = uniqueSortedStrings(c.LensIDs)
	c.CompiledRefSet = uniqueSortedRefs(c.CompiledRefSet)
	c.CompiledPathFamilyIDs = uniqueSortedStrings(c.CompiledPathFamilyIDs)
	c.CompiledAggregateScopeRefs = uniqueSortedRefs(c.CompiledAggregateScopeRefs)
	c.BasisDigest = strings.TrimSpace(c.BasisDigest)
	if c.Priority == 0 {
		c.Priority = 50
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = time.Now().UTC()
	}
}

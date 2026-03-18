package models

import (
	"sort"
	"strings"
	"time"
)

type WorkingProductStatus string

const (
	WorkingProductStatusSeeded         WorkingProductStatus = "seeded"
	WorkingProductStatusDiscovered     WorkingProductStatus = "discovered"
	WorkingProductStatusConfirmed      WorkingProductStatus = "confirmed"
	WorkingProductStatusRejected       WorkingProductStatus = "rejected"
	WorkingProductStatusStaleCandidate WorkingProductStatus = "stale_candidate"
)

type WorkingProductEntry struct {
	ProductRef            string               `json:"productRef" firestore:"productRef"`
	PrimaryOperationalRef string               `json:"primaryOperationalRef,omitempty" firestore:"primaryOperationalRef,omitempty"`
	EquivalentProductRefs []string             `json:"equivalentProductRefs,omitempty" firestore:"equivalentProductRefs,omitempty"`
	SeedRefs              []string             `json:"seedRefs,omitempty" firestore:"seedRefs,omitempty"`
	Aliases               []string             `json:"aliases,omitempty" firestore:"aliases,omitempty"`
	SourceKinds           []string             `json:"sourceKinds,omitempty" firestore:"sourceKinds,omitempty"`
	DiscoveryReasons      []string             `json:"discoveryReasons,omitempty" firestore:"discoveryReasons,omitempty"`
	Status                WorkingProductStatus `json:"status,omitempty" firestore:"status,omitempty"`
	Confidence            float64              `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	CoverageStatus        string               `json:"coverageStatus,omitempty" firestore:"coverageStatus,omitempty"`
	LastExpandedAt        *time.Time           `json:"lastExpandedAt,omitempty" firestore:"lastExpandedAt,omitempty"`
	LastValidatedAt       *time.Time           `json:"lastValidatedAt,omitempty" firestore:"lastValidatedAt,omitempty"`
	UpdatedAt             time.Time            `json:"updatedAt,omitempty" firestore:"updatedAt,omitempty"`
}

type WorkingProductScope struct {
	JobID               string                `json:"jobId" firestore:"jobId"`
	TenantID            string                `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	LensID              string                `json:"lensId,omitempty" firestore:"lensId,omitempty"`
	DomainPath          string                `json:"domainPath,omitempty" firestore:"domainPath,omitempty"`
	CanonicalProducts   []WorkingProductEntry `json:"canonicalProducts,omitempty" firestore:"canonicalProducts,omitempty"`
	RejectedProductRefs []string              `json:"rejectedProductRefs,omitempty" firestore:"rejectedProductRefs,omitempty"`
	LastExpandedAt      time.Time             `json:"lastExpandedAt,omitempty" firestore:"lastExpandedAt,omitempty"`
	LastValidatedAt     time.Time             `json:"lastValidatedAt,omitempty" firestore:"lastValidatedAt,omitempty"`
	CreatedAt           time.Time             `json:"createdAt" firestore:"createdAt"`
	UpdatedAt           time.Time             `json:"updatedAt" firestore:"updatedAt"`
}

type ProductScopeObservation struct {
	ID         string               `json:"id,omitempty" firestore:"id,omitempty"`
	JobID      string               `json:"jobId" firestore:"jobId"`
	ProductRef string               `json:"productRef" firestore:"productRef"`
	RawName    string               `json:"rawName,omitempty" firestore:"rawName,omitempty"`
	SourceKind string               `json:"sourceKind,omitempty" firestore:"sourceKind,omitempty"`
	Reason     string               `json:"reason,omitempty" firestore:"reason,omitempty"`
	Status     WorkingProductStatus `json:"status,omitempty" firestore:"status,omitempty"`
	Confidence float64              `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	SourceIDs  []string             `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	ObservedAt time.Time            `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
	CreatedAt  time.Time            `json:"createdAt" firestore:"createdAt"`
	UpdatedAt  time.Time            `json:"updatedAt" firestore:"updatedAt"`
}

type CapabilityProfile struct {
	ID             string                 `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID       string                 `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	CarrierRef     string                 `json:"carrierRef" firestore:"carrierRef"`
	ProductRef     string                 `json:"productRef" firestore:"productRef"`
	Metrics        map[string]MetricValue `json:"metrics,omitempty" firestore:"metrics,omitempty"`
	Qualifiers     map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
	CoverageStatus string                 `json:"coverageStatus,omitempty" firestore:"coverageStatus,omitempty"`
	LastObservedAt time.Time              `json:"lastObservedAt,omitempty" firestore:"lastObservedAt,omitempty"`
	LastRolledUpAt time.Time              `json:"lastRolledUpAt,omitempty" firestore:"lastRolledUpAt,omitempty"`
	CreatedAt      time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt      time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

type CapabilityObservation struct {
	ID         string                 `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID   string                 `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	CarrierRef string                 `json:"carrierRef" firestore:"carrierRef"`
	ProductRef string                 `json:"productRef" firestore:"productRef"`
	Metrics    map[string]MetricValue `json:"metrics,omitempty" firestore:"metrics,omitempty"`
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
	SourceIDs  []string               `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	ObservedAt time.Time              `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
	CreatedAt  time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

type SupplyLinkObservation struct {
	ID                      string                 `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID                string                 `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	SupplyLinkCanonicalPath string                 `json:"supplyLinkCanonicalPath" firestore:"supplyLinkCanonicalPath"`
	ProductRef              string                 `json:"productRef,omitempty" firestore:"productRef,omitempty"`
	Metrics                 map[string]MetricValue `json:"metrics,omitempty" firestore:"metrics,omitempty"`
	Qualifiers              map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
	SourceIDs               []string               `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	ObservedAt              time.Time              `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
	CreatedAt               time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt               time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

type RouteObservation struct {
	ID                      string                 `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID                string                 `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	SupplyLinkCanonicalPath string                 `json:"supplyLinkCanonicalPath,omitempty" firestore:"supplyLinkCanonicalPath,omitempty"`
	PathArtifactKey         string                 `json:"pathArtifactKey,omitempty" firestore:"pathArtifactKey,omitempty"`
	LegIndex                *int                   `json:"legIndex,omitempty" firestore:"legIndex,omitempty"`
	ProductRef              string                 `json:"productRef,omitempty" firestore:"productRef,omitempty"`
	TransportViaRefs        []string               `json:"transportViaRefs,omitempty" firestore:"transportViaRefs,omitempty"`
	Metrics                 map[string]MetricValue `json:"metrics,omitempty" firestore:"metrics,omitempty"`
	Qualifiers              map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
	SourceIDs               []string               `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	ObservedAt              time.Time              `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
	CreatedAt               time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt               time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

type DerivedRiskProfile struct {
	ID              string                 `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID        string                 `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	ScopeRef        string                 `json:"scopeRef" firestore:"scopeRef"`
	ProductRef      string                 `json:"productRef,omitempty" firestore:"productRef,omitempty"`
	ProfileType     string                 `json:"profileType" firestore:"profileType"`
	Metrics         map[string]MetricValue `json:"metrics,omitempty" firestore:"metrics,omitempty"`
	Qualifiers      map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
	EvidenceFactIDs []string               `json:"evidenceFactIds,omitempty" firestore:"evidenceFactIds,omitempty"`
	CreatedAt       time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

func (s *WorkingProductScope) Normalize() {
	if s == nil {
		return
	}
	s.JobID = strings.TrimSpace(s.JobID)
	s.TenantID = strings.TrimSpace(s.TenantID)
	s.LensID = strings.TrimSpace(s.LensID)
	s.DomainPath = strings.TrimSpace(s.DomainPath)
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	s.CanonicalProducts = normalizeWorkingProductEntries(s.CanonicalProducts)
	s.RejectedProductRefs = uniqueSortedRefs(s.RejectedProductRefs)
}

func (s *WorkingProductScope) CanonicalProductRefs() []string {
	if s == nil || len(s.CanonicalProducts) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.CanonicalProducts))
	for _, entry := range s.CanonicalProducts {
		if entry.Status == WorkingProductStatusRejected || entry.Status == WorkingProductStatusStaleCandidate {
			continue
		}
		ref := strings.TrimSpace(entry.PrimaryOperationalRef)
		if ref == "" {
			ref = strings.TrimSpace(entry.ProductRef)
		}
		if ref == "" {
			continue
		}
		out = append(out, ref)
	}
	return uniqueSortedRefs(out)
}

func (s *WorkingProductScope) MergeEntry(entry WorkingProductEntry) bool {
	if s == nil {
		return false
	}
	entry.ProductRef = strings.TrimSpace(entry.ProductRef)
	entry.PrimaryOperationalRef = strings.TrimSpace(entry.PrimaryOperationalRef)
	if entry.PrimaryOperationalRef == "" {
		entry.PrimaryOperationalRef = entry.ProductRef
	}
	if entry.ProductRef == "" {
		entry.ProductRef = entry.PrimaryOperationalRef
	}
	if entry.ProductRef == "" {
		return false
	}
	now := time.Now().UTC()
	entry.UpdatedAt = now
	entry.EquivalentProductRefs = uniqueSortedStrings(append(entry.EquivalentProductRefs, entry.ProductRef))
	entry.SeedRefs = uniqueSortedStrings(entry.SeedRefs)
	entry.Aliases = uniqueSortedStrings(entry.Aliases)
	entry.SourceKinds = uniqueSortedStrings(entry.SourceKinds)
	entry.DiscoveryReasons = uniqueSortedStrings(entry.DiscoveryReasons)
	if entry.Status == "" {
		entry.Status = WorkingProductStatusDiscovered
	}
	for idx := range s.CanonicalProducts {
		existing := &s.CanonicalProducts[idx]
		existingRef := strings.TrimSpace(existing.PrimaryOperationalRef)
		if existingRef == "" {
			existingRef = strings.TrimSpace(existing.ProductRef)
		}
		if existingRef != entry.PrimaryOperationalRef && strings.TrimSpace(existing.ProductRef) != entry.ProductRef {
			continue
		}
		if existing.PrimaryOperationalRef == "" {
			existing.PrimaryOperationalRef = existing.ProductRef
		}
		if existing.ProductRef == "" {
			existing.ProductRef = existing.PrimaryOperationalRef
		}
		existing.EquivalentProductRefs = uniqueSortedStrings(append(existing.EquivalentProductRefs, entry.EquivalentProductRefs...))
		existing.SeedRefs = uniqueSortedStrings(append(existing.SeedRefs, entry.SeedRefs...))
		existing.Aliases = uniqueSortedStrings(append(existing.Aliases, entry.Aliases...))
		existing.SourceKinds = uniqueSortedStrings(append(existing.SourceKinds, entry.SourceKinds...))
		existing.DiscoveryReasons = uniqueSortedStrings(append(existing.DiscoveryReasons, entry.DiscoveryReasons...))
		if entry.Confidence > existing.Confidence {
			existing.Confidence = entry.Confidence
		}
		if entry.CoverageStatus != "" {
			existing.CoverageStatus = strings.TrimSpace(entry.CoverageStatus)
		}
		if entry.Status != "" && existing.Status != WorkingProductStatusConfirmed {
			existing.Status = entry.Status
		}
		if entry.LastExpandedAt != nil {
			existing.LastExpandedAt = entry.LastExpandedAt
		}
		if entry.LastValidatedAt != nil {
			existing.LastValidatedAt = entry.LastValidatedAt
		}
		existing.UpdatedAt = now
		s.UpdatedAt = now
		return true
	}
	if entry.LastExpandedAt == nil {
		entry.LastExpandedAt = &now
	}
	s.CanonicalProducts = append(s.CanonicalProducts, entry)
	s.CanonicalProducts = normalizeWorkingProductEntries(s.CanonicalProducts)
	s.UpdatedAt = now
	return true
}

func normalizeWorkingProductEntries(entries []WorkingProductEntry) []WorkingProductEntry {
	if len(entries) == 0 {
		return nil
	}
	byRef := make(map[string]WorkingProductEntry, len(entries))
	for _, entry := range entries {
		ref := strings.TrimSpace(entry.ProductRef)
		primaryRef := strings.TrimSpace(entry.PrimaryOperationalRef)
		if primaryRef == "" {
			primaryRef = ref
		}
		if ref == "" {
			ref = primaryRef
		}
		if ref == "" {
			continue
		}
		existing, ok := byRef[primaryRef]
		if !ok {
			entry.ProductRef = ref
			entry.PrimaryOperationalRef = primaryRef
			entry.EquivalentProductRefs = uniqueSortedStrings(append(entry.EquivalentProductRefs, ref))
			entry.SeedRefs = uniqueSortedStrings(entry.SeedRefs)
			entry.Aliases = uniqueSortedStrings(entry.Aliases)
			entry.SourceKinds = uniqueSortedStrings(entry.SourceKinds)
			entry.DiscoveryReasons = uniqueSortedStrings(entry.DiscoveryReasons)
			byRef[primaryRef] = entry
			continue
		}
		existing.EquivalentProductRefs = uniqueSortedStrings(append(existing.EquivalentProductRefs, entry.EquivalentProductRefs...))
		existing.SeedRefs = uniqueSortedStrings(append(existing.SeedRefs, entry.SeedRefs...))
		existing.Aliases = uniqueSortedStrings(append(existing.Aliases, entry.Aliases...))
		existing.SourceKinds = uniqueSortedStrings(append(existing.SourceKinds, entry.SourceKinds...))
		existing.DiscoveryReasons = uniqueSortedStrings(append(existing.DiscoveryReasons, entry.DiscoveryReasons...))
		if entry.Confidence > existing.Confidence {
			existing.Confidence = entry.Confidence
		}
		if entry.CoverageStatus != "" {
			existing.CoverageStatus = entry.CoverageStatus
		}
		if existing.Status == "" || existing.Status == WorkingProductStatusSeeded {
			existing.Status = entry.Status
		}
		if existing.LastExpandedAt == nil && entry.LastExpandedAt != nil {
			existing.LastExpandedAt = entry.LastExpandedAt
		}
		if entry.LastValidatedAt != nil {
			existing.LastValidatedAt = entry.LastValidatedAt
		}
		if entry.UpdatedAt.After(existing.UpdatedAt) {
			existing.UpdatedAt = entry.UpdatedAt
		}
		byRef[primaryRef] = existing
	}
	out := make([]WorkingProductEntry, 0, len(byRef))
	for _, entry := range byRef {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PrimaryOperationalRef == out[j].PrimaryOperationalRef {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].PrimaryOperationalRef < out[j].PrimaryOperationalRef
	})
	return out
}

func uniqueSortedRefs(values []string) []string {
	return uniqueSortedStrings(values)
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

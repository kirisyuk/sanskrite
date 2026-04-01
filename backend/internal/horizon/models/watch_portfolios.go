package models

import (
	"fmt"
	"strings"
	"time"
)

type WatchPortfolioKind string

const (
	WatchPortfolioKindClient      WatchPortfolioKind = "client"
	WatchPortfolioKindProspective WatchPortfolioKind = "prospective"
	WatchPortfolioKindInternal    WatchPortfolioKind = "internal"
	WatchPortfolioKindMixed       WatchPortfolioKind = "mixed"
)

type WatchPortfolioEntryType string

const (
	WatchPortfolioEntryClient      WatchPortfolioEntryType = "client"
	WatchPortfolioEntryProspective WatchPortfolioEntryType = "prospective"
	WatchPortfolioEntryInternal    WatchPortfolioEntryType = "internal"
)

type WatchPortfolioEntry struct {
	ID                    string         `json:"id" firestore:"id"`
	EntryType             string         `json:"entryType" firestore:"entryType"`
	Label                 string         `json:"label" firestore:"label"`
	CanonicalPath         string         `json:"canonicalPath,omitempty" firestore:"canonicalPath,omitempty"`
	SubjectID             string         `json:"subjectId,omitempty" firestore:"subjectId,omitempty"`
	SourceTenantID        string         `json:"sourceTenantId,omitempty" firestore:"sourceTenantId,omitempty"`
	SourceLensID          string         `json:"sourceLensId,omitempty" firestore:"sourceLensId,omitempty"`
	RelatedCanonicalPaths []string       `json:"relatedCanonicalPaths,omitempty" firestore:"relatedCanonicalPaths,omitempty"`
	AutoEnsureFocus       bool           `json:"autoEnsureFocus" firestore:"autoEnsureFocus"`
	Metadata              map[string]any `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt             time.Time      `json:"createdAt" firestore:"createdAt"`
	UpdatedAt             time.Time      `json:"updatedAt" firestore:"updatedAt"`
}

func (e *WatchPortfolioEntry) Validate() error {
	if e == nil {
		return fmt.Errorf("portfolio entry is required")
	}
	if strings.TrimSpace(e.EntryType) == "" {
		e.EntryType = string(WatchPortfolioEntryInternal)
	}
	if strings.TrimSpace(e.Label) == "" && strings.TrimSpace(e.CanonicalPath) == "" && strings.TrimSpace(e.SubjectID) == "" {
		return fmt.Errorf("entry label, canonicalPath or subjectId is required")
	}
	if strings.TrimSpace(e.Label) == "" {
		e.Label = strings.TrimSpace(portfolioFirstNonEmpty(strings.TrimSpace(e.CanonicalPath), strings.TrimSpace(e.SubjectID)))
	}
	return nil
}

type WatchPortfolio struct {
	ID          string                `json:"id" firestore:"id"`
	TenantID    string                `json:"tenantId" firestore:"tenantId"`
	UserID      string                `json:"userId,omitempty" firestore:"userId,omitempty"`
	Name        string                `json:"name" firestore:"name"`
	Kind        string                `json:"kind" firestore:"kind"`
	Description string                `json:"description,omitempty" firestore:"description,omitempty"`
	Status      string                `json:"status" firestore:"status"`
	Entries     []WatchPortfolioEntry `json:"entries,omitempty" firestore:"entries,omitempty"`
	Metadata    map[string]any        `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt   time.Time             `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time             `json:"updatedAt" firestore:"updatedAt"`
}

func (p *WatchPortfolio) Validate() error {
	if p == nil {
		return fmt.Errorf("portfolio is required")
	}
	if strings.TrimSpace(p.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch strings.TrimSpace(p.Kind) {
	case string(WatchPortfolioKindClient), string(WatchPortfolioKindProspective), string(WatchPortfolioKindInternal), string(WatchPortfolioKindMixed), "":
	default:
		return fmt.Errorf("invalid kind")
	}
	if strings.TrimSpace(p.Kind) == "" {
		p.Kind = string(WatchPortfolioKindMixed)
	}
	if strings.TrimSpace(p.Status) == "" {
		p.Status = "ACTIVE"
	}
	for i := range p.Entries {
		if p.Entries[i].ID == "" {
			p.Entries[i].ID = fmt.Sprintf("entry_%d", i+1)
		}
		if err := p.Entries[i].Validate(); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
	}
	return nil
}

func portfolioFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

package watchruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type PortfolioEntryView struct {
	EntryID            string                     `json:"entryId"`
	EntryType          string                     `json:"entryType"`
	Label              string                     `json:"label"`
	CanonicalPath      string                     `json:"canonicalPath,omitempty"`
	SourceTenantID     string                     `json:"sourceTenantId,omitempty"`
	SubjectID          string                     `json:"subjectId,omitempty"`
	MonitoringSnapshot *SubjectMonitoringSnapshot `json:"monitoringSnapshot,omitempty"`
	ResolutionError    string                     `json:"resolutionError,omitempty"`
}

type PortfolioView struct {
	Portfolio         *horizonmodels.WatchPortfolio `json:"portfolio"`
	Entries           []PortfolioEntryView          `json:"entries,omitempty"`
	PriorityPoints    []WideRiskObserverPoint       `json:"priorityPoints,omitempty"`
	MaterialSubstrate MaterialSignalSubstrate       `json:"materialSubstrate"`
	CoverageGaps      []string                      `json:"coverageGaps,omitempty"`
	GeneratedAt       time.Time                     `json:"generatedAt"`
}

func (s *Service) ListPortfolios(ctx context.Context, tenantID string, limit int) ([]*horizonmodels.WatchPortfolio, error) {
	if s == nil || s.portfoliosRepo == nil {
		return nil, fmt.Errorf("portfolio runtime is not configured")
	}
	return s.portfoliosRepo.ListByTenant(ctx, strings.TrimSpace(firstNonEmpty(tenantID, "global")), limit)
}

func (s *Service) UpsertPortfolio(ctx context.Context, portfolio *horizonmodels.WatchPortfolio) error {
	if s == nil || s.portfoliosRepo == nil || portfolio == nil {
		return fmt.Errorf("portfolio runtime is not configured")
	}
	return s.portfoliosRepo.Upsert(ctx, portfolio)
}

func (s *Service) PortfolioView(ctx context.Context, portfolioID string) (*PortfolioView, error) {
	if s == nil || s.portfoliosRepo == nil {
		return nil, fmt.Errorf("portfolio runtime is not configured")
	}
	portfolio, err := s.portfoliosRepo.Get(ctx, strings.TrimSpace(portfolioID))
	if err != nil || portfolio == nil {
		return nil, err
	}
	now := time.Now().UTC()
	entries := make([]PortfolioEntryView, 0, len(portfolio.Entries))
	points := make([]WideRiskObserverPoint, 0, len(portfolio.Entries))
	coverageGaps := []string{}
	allBundles := []MaterialSignalBundle{}
	for _, entry := range portfolio.Entries {
		view := PortfolioEntryView{
			EntryID:        strings.TrimSpace(entry.ID),
			EntryType:      strings.TrimSpace(entry.EntryType),
			Label:          strings.TrimSpace(entry.Label),
			CanonicalPath:  strings.TrimSpace(entry.CanonicalPath),
			SourceTenantID: strings.TrimSpace(firstNonEmpty(entry.SourceTenantID, portfolio.TenantID, "global")),
			SubjectID:      strings.TrimSpace(entry.SubjectID),
		}
		detail, derr := s.resolvePortfolioEntry(ctx, portfolio, &entry)
		if derr != nil {
			view.ResolutionError = derr.Error()
			entries = append(entries, view)
			continue
		}
		view.SubjectID = strings.TrimSpace(detail.Subject.ID)
		view.MonitoringSnapshot = detail.MonitoringSnapshot
		entries = append(entries, view)
		points = append(points, portfolioPointsFromDetail(&entry, detail, now)...)
		if detail.MonitoringSnapshot != nil {
			coverageGaps = append(coverageGaps, detail.MonitoringSnapshot.OpenQuestions...)
			allBundles = append(allBundles, detail.MonitoringSnapshot.MaterialSubstrate.Bundles...)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left := 0
		right := 0
		if entries[i].MonitoringSnapshot != nil {
			left = len(entries[i].MonitoringSnapshot.Threats)
		}
		if entries[j].MonitoringSnapshot != nil {
			right = len(entries[j].MonitoringSnapshot.Threats)
		}
		if left == right {
			return entries[i].Label < entries[j].Label
		}
		return left > right
	})
	return &PortfolioView{
		Portfolio:         portfolio,
		Entries:           entries,
		PriorityPoints:    collapseWideRiskPoints(points, 24),
		MaterialSubstrate: collapseMaterialBundles(allBundles, 12),
		CoverageGaps:      compactUnique(coverageGaps),
		GeneratedAt:       now,
	}, nil
}

func (s *Service) resolvePortfolioEntry(ctx context.Context, portfolio *horizonmodels.WatchPortfolio, entry *horizonmodels.WatchPortfolioEntry) (*SubjectDetail, error) {
	if s == nil || entry == nil {
		return nil, fmt.Errorf("portfolio entry is required")
	}
	if strings.TrimSpace(entry.SubjectID) != "" {
		return s.SubjectDetail(ctx, strings.TrimSpace(entry.SubjectID), 12)
	}
	sourceTenantID := strings.TrimSpace(firstNonEmpty(entry.SourceTenantID, portfolio.TenantID, "global"))
	if strings.TrimSpace(entry.CanonicalPath) == "" {
		return nil, fmt.Errorf("portfolio entry canonicalPath is required")
	}
	if s.subjectsRepo != nil {
		if subjects, err := s.subjectsRepo.ListByAnchorCanonicalPath(ctx, sourceTenantID, strings.TrimSpace(entry.CanonicalPath), 24); err == nil {
			for _, subject := range subjects {
				if subject == nil || strings.TrimSpace(subject.ID) == "" {
					continue
				}
				return s.SubjectDetail(ctx, strings.TrimSpace(subject.ID), 12)
			}
		}
	}
	if !entry.AutoEnsureFocus {
		return nil, fmt.Errorf("subject not found and autoEnsureFocus is disabled")
	}
	target := &horizonmodels.WatchTarget{
		TenantID:      sourceTenantID,
		UserID:        strings.TrimSpace(portfolio.UserID),
		Kind:          string(horizonmodels.WatchSubjectKindFocusObject),
		Label:         strings.TrimSpace(firstNonEmpty(entry.Label, entry.CanonicalPath)),
		CanonicalPath: strings.TrimSpace(entry.CanonicalPath),
		Source:        "portfolio_auto_ensure",
		Status:        string(horizonmodels.WatchTargetStatusActive),
		Metadata: map[string]any{
			"focusObject": map[string]any{
				"anchorCanonicalPath":   strings.TrimSpace(entry.CanonicalPath),
				"displayLabel":          strings.TrimSpace(firstNonEmpty(entry.Label, entry.CanonicalPath)),
				"relatedCanonicalPaths": append([]string(nil), entry.RelatedCanonicalPaths...),
			},
			"portfolioId":      strings.TrimSpace(portfolio.ID),
			"portfolioEntryId": strings.TrimSpace(entry.ID),
		},
	}
	subject, err := s.EnsureSubjectForTarget(ctx, target)
	if err != nil || subject == nil {
		return nil, err
	}
	return s.SubjectDetail(ctx, strings.TrimSpace(subject.ID), 12)
}

func portfolioPointsFromDetail(entry *horizonmodels.WatchPortfolioEntry, detail *SubjectDetail, now time.Time) []WideRiskObserverPoint {
	if detail == nil || detail.Subject == nil || detail.MonitoringSnapshot == nil {
		return nil
	}
	points := wideRiskPointsFromDetail(detail, now)
	for i := range points {
		points[i].ID = "portfolio:" + strings.TrimSpace(entry.ID) + ":" + strings.TrimSpace(points[i].CanonicalPath)
		points[i].OwnerSubjectID = strings.TrimSpace(detail.Subject.ID)
	}
	return points
}

func collapseMaterialBundles(bundles []MaterialSignalBundle, limit int) MaterialSignalSubstrate {
	if len(bundles) == 0 {
		return MaterialSignalSubstrate{}
	}
	grouped := map[string]*MaterialSignalBundle{}
	sourceSet := map[string]struct{}{}
	totalSignals := 0
	latestObserved := time.Time{}
	for _, bundle := range bundles {
		key := strings.TrimSpace(bundle.ID)
		if key == "" {
			key = strings.TrimSpace(bundle.Headline)
		}
		existing := grouped[key]
		if existing == nil {
			copy := bundle
			copy.CanonicalPaths = append([]string(nil), bundle.CanonicalPaths...)
			copy.SourceRefs = append([]string(nil), bundle.SourceRefs...)
			copy.SignalIDs = append([]string(nil), bundle.SignalIDs...)
			copy.Axes = append([]string(nil), bundle.Axes...)
			copy.Polarities = append([]string(nil), bundle.Polarities...)
			grouped[key] = &copy
			existing = &copy
		} else {
			existing.SignalCount += bundle.SignalCount
			existing.CanonicalPaths = compactUnique(append(existing.CanonicalPaths, bundle.CanonicalPaths...))
			existing.SourceRefs = compactUnique(append(existing.SourceRefs, bundle.SourceRefs...))
			existing.SignalIDs = compactUnique(append(existing.SignalIDs, bundle.SignalIDs...))
			existing.Axes = compactUnique(append(existing.Axes, bundle.Axes...))
			existing.Polarities = compactUnique(append(existing.Polarities, bundle.Polarities...))
			if bundle.LatestObserved.After(existing.LatestObserved) {
				existing.LatestObserved = bundle.LatestObserved
			}
		}
		totalSignals += bundle.SignalCount
		for _, src := range bundle.SourceRefs {
			if strings.TrimSpace(src) != "" {
				sourceSet[strings.TrimSpace(src)] = struct{}{}
			}
		}
		if bundle.LatestObserved.After(latestObserved) {
			latestObserved = bundle.LatestObserved
		}
	}
	out := make([]MaterialSignalBundle, 0, len(grouped))
	for _, bundle := range grouped {
		out = append(out, *bundle)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SignalCount == out[j].SignalCount {
			return out[i].LatestObserved.After(out[j].LatestObserved)
		}
		return out[i].SignalCount > out[j].SignalCount
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	coverage := 0.0
	if totalSignals > 0 {
		coverage += minFloat(0.6, float64(totalSignals)*0.04)
	}
	if len(sourceSet) > 0 {
		coverage += minFloat(0.4, float64(len(sourceSet))*0.06)
	}
	return MaterialSignalSubstrate{
		Bundles:        out,
		SignalCount:    totalSignals,
		SourceCount:    len(sourceSet),
		CoverageScore:  minFloat(1, coverage),
		LatestObserved: latestObserved,
	}
}

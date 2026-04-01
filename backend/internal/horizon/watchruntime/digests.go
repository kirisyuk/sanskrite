package watchruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func (s *Service) BuildLensMonitoringDigest(ctx context.Context, tenantID, lensID string, persist bool) (*horizonmodels.MonitoringDigest, error) {
	if s == nil {
		return nil, fmt.Errorf("watch runtime is not configured")
	}
	view, err := s.WideRiskObserver(ctx, tenantID, lensID, 24)
	if err != nil || view == nil {
		return nil, err
	}
	digest := digestFromWideRiskView(strings.TrimSpace(firstNonEmpty(tenantID, "global")), view)
	if persist && s.digestsRepo != nil {
		if err := s.digestsRepo.Upsert(ctx, digest); err != nil {
			return nil, err
		}
	}
	return digest, nil
}

func (s *Service) BuildPortfolioMonitoringDigest(ctx context.Context, portfolioID string, persist bool) (*horizonmodels.MonitoringDigest, error) {
	if s == nil {
		return nil, fmt.Errorf("watch runtime is not configured")
	}
	view, err := s.PortfolioView(ctx, portfolioID)
	if err != nil || view == nil || view.Portfolio == nil {
		return nil, err
	}
	digest := digestFromPortfolioView(view)
	if persist && s.digestsRepo != nil {
		if err := s.digestsRepo.Upsert(ctx, digest); err != nil {
			return nil, err
		}
	}
	return digest, nil
}

func (s *Service) GetMonitoringDigest(ctx context.Context, digestID string) (*horizonmodels.MonitoringDigest, error) {
	if s == nil || s.digestsRepo == nil {
		return nil, fmt.Errorf("digest runtime is not configured")
	}
	return s.digestsRepo.Get(ctx, digestID)
}

func (s *Service) ListRecentDigestsByScope(ctx context.Context, tenantID, scopeType, scopeID string, limit int) ([]*horizonmodels.MonitoringDigest, error) {
	if s == nil || s.digestsRepo == nil {
		return nil, fmt.Errorf("digest runtime is not configured")
	}
	return s.digestsRepo.ListRecentByScope(ctx, strings.TrimSpace(firstNonEmpty(tenantID, "global")), scopeType, scopeID, limit)
}

func digestFromWideRiskView(tenantID string, view *WideRiskObserverView) *horizonmodels.MonitoringDigest {
	now := time.Now().UTC()
	points := serializeDigestPoints(view.PriorityPoints, 8)
	headline := firstNonEmpty(buildDigestHeadlineFromPoints(view.PriorityPoints), strings.TrimSpace(view.LensName), "Daily wide risk digest")
	summary := fmt.Sprintf("%d priority points across %d subjects, %d signals and %d sources.",
		len(view.PriorityPoints), len(view.Subjects), view.MaterialSubstrate.SignalCount, view.MaterialSubstrate.SourceCount)
	return &horizonmodels.MonitoringDigest{
		TenantID:         strings.TrimSpace(firstNonEmpty(tenantID, "global")),
		ScopeType:        string(horizonmodels.MonitoringDigestScopeLens),
		ScopeID:          strings.TrimSpace(view.LensID),
		ScopeLabel:       strings.TrimSpace(view.LensName),
		DateKey:          now.Format("2006-01-02"),
		WindowStart:      now.Add(-24 * time.Hour),
		WindowEnd:        now,
		Headline:         headline,
		Summary:          summary,
		RiskPosture:      digestRiskPosture(view.PriorityPoints),
		PriorityPoints:   points,
		AffectedSubjects: digestAffectedSubjectsFromPoints(view.PriorityPoints),
		CoverageGaps:     append([]string(nil), view.CoverageGaps...),
		SignalCount:      view.MaterialSubstrate.SignalCount,
		SourceCount:      view.MaterialSubstrate.SourceCount,
		SubjectCount:     len(view.Subjects),
		Metadata: map[string]any{
			"scope":            "wide_risk_observer",
			"bundleCount":      len(view.MaterialSubstrate.Bundles),
			"generatedAt":      view.GeneratedAt.Format(time.RFC3339),
			"materialCoverage": view.MaterialSubstrate.CoverageScore,
		},
	}
}

func digestFromPortfolioView(view *PortfolioView) *horizonmodels.MonitoringDigest {
	now := time.Now().UTC()
	points := serializeDigestPoints(view.PriorityPoints, 8)
	headline := firstNonEmpty(buildDigestHeadlineFromPoints(view.PriorityPoints), strings.TrimSpace(view.Portfolio.Name), "Portfolio monitoring digest")
	summary := fmt.Sprintf("%d priority points across %d portfolio entries, %d signals and %d sources.",
		len(view.PriorityPoints), len(view.Entries), view.MaterialSubstrate.SignalCount, view.MaterialSubstrate.SourceCount)
	return &horizonmodels.MonitoringDigest{
		TenantID:         strings.TrimSpace(view.Portfolio.TenantID),
		ScopeType:        string(horizonmodels.MonitoringDigestScopePortfolio),
		ScopeID:          strings.TrimSpace(view.Portfolio.ID),
		ScopeLabel:       strings.TrimSpace(view.Portfolio.Name),
		DateKey:          now.Format("2006-01-02"),
		WindowStart:      now.Add(-24 * time.Hour),
		WindowEnd:        now,
		Headline:         headline,
		Summary:          summary,
		RiskPosture:      digestRiskPosture(view.PriorityPoints),
		PriorityPoints:   points,
		AffectedSubjects: digestAffectedSubjectsFromPoints(view.PriorityPoints),
		CoverageGaps:     append([]string(nil), view.CoverageGaps...),
		SignalCount:      view.MaterialSubstrate.SignalCount,
		SourceCount:      view.MaterialSubstrate.SourceCount,
		SubjectCount:     len(view.Entries),
		Metadata: map[string]any{
			"scope":            "portfolio",
			"portfolioKind":    strings.TrimSpace(view.Portfolio.Kind),
			"bundleCount":      len(view.MaterialSubstrate.Bundles),
			"materialCoverage": view.MaterialSubstrate.CoverageScore,
		},
	}
}

func serializeDigestPoints(points []WideRiskObserverPoint, limit int) []map[string]any {
	if len(points) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, minInt(len(points), limit))
	for _, point := range points {
		out = append(out, map[string]any{
			"id":               point.ID,
			"ownerSubjectId":   point.OwnerSubjectID,
			"subjectId":        point.SubjectID,
			"canonicalPath":    point.CanonicalPath,
			"label":            point.Label,
			"headline":         point.Headline,
			"summary":          point.Summary,
			"hotness":          point.Hotness,
			"confidence":       point.Confidence,
			"riskPosture":      point.RiskPosture,
			"why":              point.Why,
			"affectedSubjects": append([]string(nil), point.AffectedSubjects...),
			"backingIds":       append([]string(nil), point.BackingIDs...),
			"generatedAt":      point.GeneratedAt.Format(time.RFC3339),
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func buildDigestHeadlineFromPoints(points []WideRiskObserverPoint) string {
	if len(points) == 0 {
		return ""
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].Hotness > points[j].Hotness })
	top := points[0]
	if strings.TrimSpace(top.Headline) == "" {
		return ""
	}
	return top.Headline
}

func digestRiskPosture(points []WideRiskObserverPoint) string {
	if len(points) == 0 {
		return "baseline"
	}
	top := 0.0
	for _, point := range points {
		if point.Hotness > top {
			top = point.Hotness
		}
	}
	switch {
	case top >= 0.8:
		return "elevated"
	case top >= 0.45:
		return "monitoring"
	default:
		return "baseline"
	}
}

func digestAffectedSubjectsFromPoints(points []WideRiskObserverPoint) []string {
	out := make([]string, 0, len(points)*2)
	for _, point := range points {
		out = append(out, point.AffectedSubjects...)
	}
	return compactUnique(out)
}

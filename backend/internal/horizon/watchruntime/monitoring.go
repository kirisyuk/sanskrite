package watchruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/focusobject"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

type MaterialSignalBundle struct {
	ID             string    `json:"id"`
	Headline       string    `json:"headline"`
	Summary        string    `json:"summary,omitempty"`
	CanonicalPaths []string  `json:"canonicalPaths,omitempty"`
	SourceRefs     []string  `json:"sourceRefs,omitempty"`
	SignalIDs      []string  `json:"signalIds,omitempty"`
	Axes           []string  `json:"axes,omitempty"`
	Polarities     []string  `json:"polarities,omitempty"`
	SignalCount    int       `json:"signalCount"`
	LatestObserved time.Time `json:"latestObserved,omitempty"`
}

type MaterialSignalSubstrate struct {
	Bundles        []MaterialSignalBundle `json:"bundles,omitempty"`
	SignalCount    int                    `json:"signalCount"`
	SourceCount    int                    `json:"sourceCount"`
	CoverageScore  float64                `json:"coverageScore"`
	LatestObserved time.Time              `json:"latestObserved,omitempty"`
}

type SubjectMonitoringSnapshot struct {
	SubjectID             string                      `json:"subjectId"`
	SubjectKind           string                      `json:"subjectKind"`
	DisplayLabel          string                      `json:"displayLabel"`
	AnchorCanonicalPath   string                      `json:"anchorCanonicalPath"`
	CurrentPosition       map[string]any              `json:"currentPosition,omitempty"`
	DirectSignals         []focusobject.SignalSummary `json:"directSignals,omitempty"`
	PropagatedExposure    []focusobject.EndpointHit   `json:"propagatedExposure,omitempty"`
	ArtifactExposure      []focusobject.PathExposure  `json:"artifactExposure,omitempty"`
	SourceCoverage        map[string]any              `json:"sourceCoverage,omitempty"`
	Threats               []focusobject.SignalSummary `json:"threats,omitempty"`
	Tailwinds             []focusobject.SignalSummary `json:"tailwinds,omitempty"`
	OpenQuestions         []string                    `json:"openQuestions,omitempty"`
	RecommendedEscalation string                      `json:"recommendedEscalation,omitempty"`
	MaterialSubstrate     MaterialSignalSubstrate     `json:"materialSubstrate"`
	GeneratedAt           time.Time                   `json:"generatedAt"`
}

type WideRiskObserverSubject struct {
	SubjectID             string   `json:"subjectId"`
	DisplayLabel          string   `json:"displayLabel"`
	AnchorCanonicalPath   string   `json:"anchorCanonicalPath"`
	RecommendedEscalation string   `json:"recommendedEscalation,omitempty"`
	ThreatCount           int      `json:"threatCount"`
	TailwindCount         int      `json:"tailwindCount"`
	EndpointCount         int      `json:"endpointCount"`
	OpenQuestions         []string `json:"openQuestions,omitempty"`
}

type WideRiskObserverPoint struct {
	ID               string    `json:"id"`
	OwnerSubjectID   string    `json:"ownerSubjectId,omitempty"`
	SubjectID        string    `json:"subjectId"`
	CanonicalPath    string    `json:"canonicalPath"`
	Label            string    `json:"label"`
	Headline         string    `json:"headline"`
	Summary          string    `json:"summary,omitempty"`
	Hotness          float64   `json:"hotness"`
	Confidence       float64   `json:"confidence"`
	RiskPosture      string    `json:"riskPosture,omitempty"`
	Why              string    `json:"why,omitempty"`
	AffectedSubjects []string  `json:"affectedSubjects,omitempty"`
	BackingIDs       []string  `json:"backingIds,omitempty"`
	GeneratedAt      time.Time `json:"generatedAt"`
}

type WideRiskObserverView struct {
	LensID            string                    `json:"lensId"`
	LensName          string                    `json:"lensName,omitempty"`
	MaterialSubstrate MaterialSignalSubstrate   `json:"materialSubstrate"`
	PriorityPoints    []WideRiskObserverPoint   `json:"priorityPoints,omitempty"`
	Subjects          []WideRiskObserverSubject `json:"subjects,omitempty"`
	CoverageGaps      []string                  `json:"coverageGaps,omitempty"`
	GeneratedAt       time.Time                 `json:"generatedAt"`
}

func (s *Service) buildSubjectMonitoringSnapshot(ctx context.Context, detail *SubjectDetail) *SubjectMonitoringSnapshot {
	if s == nil || detail == nil || detail.Subject == nil {
		return nil
	}
	subject := detail.Subject
	now := time.Now().UTC()
	paths := subjectMonitoringPaths(subject, detail.FocusSnapshot)
	facts := s.collectFocusFacts(ctx, strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")), paths, 72)
	substrate := buildMaterialSignalSubstrate(facts, 10)
	landingSummaries := s.collectLandingSummaries(ctx, strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")), paths, 24)
	sourceCoverage := map[string]any{
		"signalCount":         substrate.SignalCount,
		"sourceCount":         substrate.SourceCount,
		"coverageScore":       substrate.CoverageScore,
		"bundleCount":         len(substrate.Bundles),
		"latestObserved":      substrate.LatestObserved,
		"landingSummaryCount": len(landingSummaries),
		"landedRefCount":      landingRefCount(landingSummaries),
		"affectedZoneCount":   landingAffectedZoneCount(landingSummaries),
		"missingPreciseZone":  anyLandingMissingPreciseZone(landingSummaries),
	}
	out := &SubjectMonitoringSnapshot{
		SubjectID:           strings.TrimSpace(subject.ID),
		SubjectKind:         string(subject.SubjectKind),
		DisplayLabel:        strings.TrimSpace(subject.DisplayLabel),
		AnchorCanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
		SourceCoverage:      sourceCoverage,
		MaterialSubstrate:   substrate,
		GeneratedAt:         now,
	}
	if detail.FocusSnapshot != nil {
		out.CurrentPosition = map[string]any{
			"riskPressure":        detail.FocusSnapshot.Position.RiskPressure,
			"opportunityPressure": detail.FocusSnapshot.Position.OpportunityPressure,
			"artifactExposure":    detail.FocusSnapshot.Position.ArtifactExposure,
			"coverageScore":       detail.FocusSnapshot.Position.CoverageScore,
			"monitoringReadiness": detail.FocusSnapshot.Position.MonitoringReadiness,
			"dominantAxes":        append([]string(nil), detail.FocusSnapshot.Position.DominantAxes...),
		}
		out.Threats = append([]focusobject.SignalSummary(nil), detail.FocusSnapshot.Threats...)
		out.Tailwinds = append([]focusobject.SignalSummary(nil), detail.FocusSnapshot.Tailwinds...)
		out.DirectSignals = directSignalsFromFocusSnapshot(detail.FocusSnapshot)
		out.PropagatedExposure = append([]focusobject.EndpointHit(nil), detail.FocusSnapshot.EndpointHits...)
		out.ArtifactExposure = append([]focusobject.PathExposure(nil), detail.FocusSnapshot.PathExposures...)
		out.OpenQuestions = compactUnique(append([]string{}, detail.FocusSnapshot.Position.Warnings...))
	} else {
		brief := s.latestBriefForSubject(subject)
		if brief != nil {
			out.CurrentPosition = map[string]any{
				"hotness":     brief.Hotness,
				"confidence":  brief.Confidence,
				"riskPosture": brief.RiskPosture,
			}
			out.OpenQuestions = compactUnique(append(out.OpenQuestions, brief.DominantThemes...))
			out.Threats = append(out.Threats, focusobject.SignalSummary{
				SignalID:   strings.TrimSpace(brief.ID),
				Headline:   strings.TrimSpace(brief.Headline),
				Summary:    strings.TrimSpace(brief.Summary),
				Polarity:   "risk",
				Axis:       firstNonEmpty(strings.Join(brief.DominantThemes, ","), "watch/general"),
				Score:      brief.Hotness,
				Directness: "watch_brief",
				ObservedAt: brief.UpdatedAt,
			})
		}
	}
	out.OpenQuestions = compactUnique(append(out.OpenQuestions, landingOpenQuestions(landingSummaries)...))
	if out.CurrentPosition == nil {
		out.CurrentPosition = map[string]any{}
	}
	if len(landingSummaries) > 0 {
		out.CurrentPosition["landingSummaryCount"] = len(landingSummaries)
		out.CurrentPosition["landingAffectedZones"] = firstNStrings(landingAffectedZones(landingSummaries), 6)
		out.CurrentPosition["landingConfirmedRefs"] = firstNStrings(landingConfirmedRefs(landingSummaries), 8)
	}
	out.RecommendedEscalation = recommendedEscalationForSnapshot(out, detail)
	return out
}

func (s *Service) SubjectMonitoringSnapshot(ctx context.Context, subjectID string, limit int) (*SubjectMonitoringSnapshot, error) {
	detail, err := s.SubjectDetail(ctx, subjectID, limit)
	if err != nil || detail == nil {
		return nil, err
	}
	return detail.MonitoringSnapshot, nil
}

func (s *Service) WideRiskObserver(ctx context.Context, tenantID, lensID string, limit int) (*WideRiskObserverView, error) {
	if s == nil || s.lensesRepo == nil || s.subjectsRepo == nil || strings.TrimSpace(lensID) == "" {
		return nil, fmt.Errorf("wide risk observer is not configured")
	}
	lens, err := s.lensesRepo.Get(ctx, strings.TrimSpace(lensID))
	if err != nil || lens == nil {
		return nil, err
	}
	subjects, err := s.subjectsRepo.ListByLens(ctx, strings.TrimSpace(firstNonEmpty(tenantID, lens.TenantID, "global")), strings.TrimSpace(lensID), maxInt(limit, 100))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	points := make([]WideRiskObserverPoint, 0, len(subjects))
	subjectViews := make([]WideRiskObserverSubject, 0, len(subjects))
	coverageGaps := []string{}
	allFacts := []*coremodels.OntologyFact{}
	for _, subject := range subjects {
		if subject == nil || strings.TrimSpace(subject.ID) == "" {
			continue
		}
		detail, derr := s.SubjectDetail(ctx, subject.ID, 12)
		if derr != nil || detail == nil || detail.Subject == nil || detail.MonitoringSnapshot == nil {
			continue
		}
		subjectViews = append(subjectViews, WideRiskObserverSubject{
			SubjectID:             strings.TrimSpace(detail.Subject.ID),
			DisplayLabel:          strings.TrimSpace(detail.Subject.DisplayLabel),
			AnchorCanonicalPath:   strings.TrimSpace(detail.Subject.AnchorCanonicalPath),
			RecommendedEscalation: strings.TrimSpace(detail.MonitoringSnapshot.RecommendedEscalation),
			ThreatCount:           len(detail.MonitoringSnapshot.Threats),
			TailwindCount:         len(detail.MonitoringSnapshot.Tailwinds),
			EndpointCount:         len(detail.MonitoringSnapshot.PropagatedExposure),
			OpenQuestions:         append([]string(nil), detail.MonitoringSnapshot.OpenQuestions...),
		})
		coverageGaps = append(coverageGaps, detail.MonitoringSnapshot.OpenQuestions...)
		points = append(points, wideRiskPointsFromDetail(detail, now)...)
		allFacts = append(allFacts, s.collectFocusFacts(ctx, strings.TrimSpace(firstNonEmpty(detail.Subject.TenantID, "global")), subjectMonitoringPaths(detail.Subject, detail.FocusSnapshot), 72)...)
	}
	sort.SliceStable(subjectViews, func(i, j int) bool {
		if subjectViews[i].ThreatCount == subjectViews[j].ThreatCount {
			return subjectViews[i].DisplayLabel < subjectViews[j].DisplayLabel
		}
		return subjectViews[i].ThreatCount > subjectViews[j].ThreatCount
	})
	view := &WideRiskObserverView{
		LensID:            strings.TrimSpace(lens.ID),
		LensName:          strings.TrimSpace(lens.Name),
		MaterialSubstrate: buildMaterialSignalSubstrate(allFacts, 12),
		PriorityPoints:    collapseWideRiskPoints(points, limit),
		Subjects:          subjectViews,
		CoverageGaps:      compactUnique(coverageGaps),
		GeneratedAt:       now,
	}
	return view, nil
}

func buildMaterialSignalSubstrate(facts []*coremodels.OntologyFact, limit int) MaterialSignalSubstrate {
	grouped := map[string]*MaterialSignalBundle{}
	seenSources := map[string]struct{}{}
	latestObserved := time.Time{}
	for _, fact := range facts {
		if fact == nil || strings.TrimSpace(fact.Kind) != "signal" {
			continue
		}
		key := materialBundleKey(fact)
		bundle := grouped[key]
		if bundle == nil {
			bundle = &MaterialSignalBundle{
				ID:             key,
				Headline:       strings.TrimSpace(firstNonEmpty(fact.Title, fact.Summary, fact.Type)),
				Summary:        strings.TrimSpace(fact.Summary),
				CanonicalPaths: compactUnique(append([]string(nil), fact.CanonicalPaths...)),
			}
			grouped[key] = bundle
		}
		descriptor := interpretation.DescriptorFromSignalFact(fact)
		bundle.SignalCount++
		bundle.SignalIDs = append(bundle.SignalIDs, strings.TrimSpace(fact.ID))
		if descriptor != nil {
			bundle.Axes = append(bundle.Axes, strings.TrimSpace(descriptor.Axis))
			bundle.Polarities = append(bundle.Polarities, strings.TrimSpace(descriptor.Polarity))
		}
		bundle.CanonicalPaths = compactUnique(append(bundle.CanonicalPaths, fact.CanonicalPaths...))
		for _, ref := range fact.SourceRefs {
			src := strings.TrimSpace(firstNonEmpty(ref.SourceID, ref.SourceType))
			if src != "" {
				bundle.SourceRefs = append(bundle.SourceRefs, src)
				seenSources[src] = struct{}{}
			}
		}
		observed := factObservedAt(fact)
		if observed.After(bundle.LatestObserved) {
			bundle.LatestObserved = observed
		}
		if observed.After(latestObserved) {
			latestObserved = observed
		}
	}
	bundles := make([]MaterialSignalBundle, 0, len(grouped))
	totalSignals := 0
	for _, bundle := range grouped {
		bundle.SignalIDs = compactUnique(bundle.SignalIDs)
		bundle.Axes = compactUnique(bundle.Axes)
		bundle.Polarities = compactUnique(bundle.Polarities)
		bundle.SourceRefs = compactUnique(bundle.SourceRefs)
		totalSignals += bundle.SignalCount
		bundles = append(bundles, *bundle)
	}
	sort.SliceStable(bundles, func(i, j int) bool {
		if bundles[i].SignalCount == bundles[j].SignalCount {
			return bundles[i].LatestObserved.After(bundles[j].LatestObserved)
		}
		return bundles[i].SignalCount > bundles[j].SignalCount
	})
	if limit > 0 && len(bundles) > limit {
		bundles = bundles[:limit]
	}
	coverageScore := 0.0
	if totalSignals > 0 {
		coverageScore += minFloat(0.6, float64(totalSignals)*0.06)
	}
	if len(seenSources) > 0 {
		coverageScore += minFloat(0.4, float64(len(seenSources))*0.08)
	}
	return MaterialSignalSubstrate{
		Bundles:        bundles,
		SignalCount:    totalSignals,
		SourceCount:    len(seenSources),
		CoverageScore:  minFloat(1, coverageScore),
		LatestObserved: latestObserved,
	}
}

func (s *Service) collectLandingSummaries(ctx context.Context, tenantID string, paths []string, limit int) []*horizonmodels.SignalLandingSummary {
	if s == nil || s.signalLandingSummariesRepo == nil || len(paths) == 0 {
		return nil
	}
	items, err := s.signalLandingSummariesRepo.ListByScopeRefs(ctx, strings.TrimSpace(firstNonEmpty(tenantID, "global")), compactUnique(paths), maxInt(limit, 24))
	if err != nil {
		return nil
	}
	return items
}

func landingRefCount(items []*horizonmodels.SignalLandingSummary) int {
	refs := map[string]struct{}{}
	for _, item := range items {
		if item == nil {
			continue
		}
		for _, ref := range append(append([]string{}, item.TopConfirmedRefs...), item.TopInferredRefs...) {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			refs[ref] = struct{}{}
		}
	}
	return len(refs)
}

func landingAffectedZoneCount(items []*horizonmodels.SignalLandingSummary) int {
	return len(landingAffectedZones(items))
}

func anyLandingMissingPreciseZone(items []*horizonmodels.SignalLandingSummary) bool {
	for _, item := range items {
		if item != nil && item.MissingPreciseZone {
			return true
		}
	}
	return false
}

func landingOpenQuestions(items []*horizonmodels.SignalLandingSummary) []string {
	out := []string{}
	if anyLandingMissingPreciseZone(items) {
		out = append(out, "A precise affected zone is still missing for at least one active disruption signal.")
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		for _, missing := range firstNStrings(item.TopMissingCandidates, 4) {
			out = append(out, "Resolve missing anchor: "+strings.TrimSpace(missing))
		}
	}
	return compactUnique(out)
}

func landingAffectedZones(items []*horizonmodels.SignalLandingSummary) []string {
	out := []string{}
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, item.AffectedZoneRefs...)
	}
	return compactUnique(out)
}

func landingConfirmedRefs(items []*horizonmodels.SignalLandingSummary) []string {
	out := []string{}
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, item.TopConfirmedRefs...)
	}
	return compactUnique(out)
}

func firstNStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	values = compactUnique(values)
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func materialBundleKey(fact *coremodels.OntologyFact) string {
	if fact == nil {
		return ""
	}
	if fact.EventID != nil && strings.TrimSpace(*fact.EventID) != "" {
		return "event:" + strings.TrimSpace(*fact.EventID)
	}
	headline := strings.TrimSpace(firstNonEmpty(fact.Title, fact.Type, fact.ID))
	source := ""
	if len(fact.SourceRefs) > 0 {
		source = strings.TrimSpace(firstNonEmpty(fact.SourceRefs[0].SourceID, fact.SourceRefs[0].SourceType))
	}
	dateKey := factObservedAt(fact).UTC().Format("2006-01-02")
	return strings.TrimSpace(firstNonEmpty(source, "signal")) + ":" + headline + ":" + dateKey
}

func directSignalsFromFocusSnapshot(snapshot *focusobject.Snapshot) []focusobject.SignalSummary {
	if snapshot == nil {
		return nil
	}
	out := make([]focusobject.SignalSummary, 0, len(snapshot.Threats)+len(snapshot.Tailwinds))
	for _, item := range append(append([]focusobject.SignalSummary{}, snapshot.Threats...), snapshot.Tailwinds...) {
		if strings.EqualFold(strings.TrimSpace(item.Directness), "direct") || strings.EqualFold(strings.TrimSpace(item.Directness), "anchor") {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		out = append(out, snapshot.Threats...)
		out = append(out, snapshot.Tailwinds...)
	}
	return out
}

func subjectMonitoringPaths(subject *horizonmodels.WatchSubject, snapshot *focusobject.Snapshot) []string {
	paths := []string{}
	if subject != nil {
		paths = append(paths, strings.TrimSpace(subject.AnchorCanonicalPath))
		paths = append(paths, subject.UniversePaths...)
	}
	if snapshot != nil {
		paths = append(paths, snapshot.Focus.UniversePaths...)
		for _, packet := range snapshot.EndpointPackets {
			paths = append(paths, strings.TrimSpace(packet.CanonicalPath))
			paths = append(paths, packet.ClusterPeers...)
		}
	}
	return compactUnique(paths)
}

func recommendedEscalationForSnapshot(snapshot *SubjectMonitoringSnapshot, detail *SubjectDetail) string {
	if snapshot == nil {
		return "warm_only"
	}
	riskPressure := float64FromAny(snapshot.CurrentPosition["riskPressure"])
	artifactExposure := float64FromAny(snapshot.CurrentPosition["artifactExposure"])
	coverageScore := snapshot.MaterialSubstrate.CoverageScore
	if coverageScore < 0.35 || len(snapshot.OpenQuestions) >= 3 {
		return "enqueue_scout"
	}
	if riskPressure >= 0.70 || artifactExposure >= 0.70 {
		return "enqueue_risk"
	}
	if len(snapshot.PropagatedExposure) > 0 || len(snapshot.Threats) > 0 || len(snapshot.Tailwinds) > 0 {
		return "wake_scope_agent"
	}
	if detail != nil && len(detail.Triggers) > 0 {
		return "wake_scope_agent"
	}
	return "warm_only"
}

func (s *Service) latestBriefForSubject(subject *horizonmodels.WatchSubject) *horizonmodels.WatchBrief {
	if s == nil || s.watchBriefsRepo == nil || subject == nil {
		return nil
	}
	briefs, err := s.watchBriefsRepo.ListByTenant(context.Background(), strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")), 100)
	if err != nil {
		return nil
	}
	var best *horizonmodels.WatchBrief
	for _, brief := range briefs {
		if brief == nil {
			continue
		}
		if strings.TrimSpace(brief.SubjectID) != strings.TrimSpace(subject.ID) && strings.TrimSpace(brief.CanonicalPath) != strings.TrimSpace(subject.AnchorCanonicalPath) {
			continue
		}
		if best == nil || brief.UpdatedAt.After(best.UpdatedAt) {
			best = brief
		}
	}
	return best
}

func wideRiskPointsFromDetail(detail *SubjectDetail, now time.Time) []WideRiskObserverPoint {
	if detail == nil || detail.Subject == nil || detail.MonitoringSnapshot == nil {
		return nil
	}
	subject := detail.Subject
	snapshot := detail.MonitoringSnapshot
	point := WideRiskObserverPoint{
		ID:            "wide:" + strings.TrimSpace(subject.ID),
		SubjectID:     strings.TrimSpace(subject.ID),
		CanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
		Label:         strings.TrimSpace(subject.DisplayLabel),
		GeneratedAt:   now,
	}
	if coverage := executionCoverageMetadata(subject.Metadata); len(coverage) > 0 {
		point.OwnerSubjectID = strings.TrimSpace(firstNonEmpty(asString(coverage["primaryOwnerSubjectId"]), subject.ID))
	}
	if point.OwnerSubjectID == "" {
		point.OwnerSubjectID = point.SubjectID
	}
	if len(snapshot.Threats) > 0 {
		top := snapshot.Threats[0]
		point.Headline = strings.TrimSpace(top.Headline)
		point.Summary = strings.TrimSpace(firstNonEmpty(top.Summary, top.Why))
		point.Hotness = top.Score
		point.Confidence = minFloat(1, float64(len(snapshot.MaterialSubstrate.Bundles))*0.15+0.35)
		point.RiskPosture = "threat"
		point.Why = strings.TrimSpace(top.Why)
	} else if len(snapshot.Tailwinds) > 0 {
		top := snapshot.Tailwinds[0]
		point.Headline = strings.TrimSpace(top.Headline)
		point.Summary = strings.TrimSpace(firstNonEmpty(top.Summary, top.Why))
		point.Hotness = top.Score
		point.Confidence = minFloat(1, float64(len(snapshot.MaterialSubstrate.Bundles))*0.15+0.35)
		point.RiskPosture = "tailwind"
		point.Why = strings.TrimSpace(top.Why)
	} else {
		return nil
	}
	for _, item := range snapshot.PropagatedExposure {
		if strings.TrimSpace(item.CanonicalPath) == strings.TrimSpace(subject.AnchorCanonicalPath) {
			continue
		}
		point.AffectedSubjects = append(point.AffectedSubjects, strings.TrimSpace(item.CanonicalPath))
		if len(point.BackingIDs) < 6 {
			point.BackingIDs = append(point.BackingIDs, append([]string(nil), item.SignalIDs...)...)
		}
	}
	point.AffectedSubjects = compactUnique(point.AffectedSubjects)
	point.BackingIDs = compactUnique(point.BackingIDs)
	return []WideRiskObserverPoint{point}
}

func collapseWideRiskPoints(points []WideRiskObserverPoint, limit int) []WideRiskObserverPoint {
	if len(points) == 0 {
		return nil
	}
	type ownerKey struct {
		owner string
		path  string
	}
	merged := map[ownerKey]*WideRiskObserverPoint{}
	for _, point := range points {
		if strings.TrimSpace(point.Headline) == "" {
			continue
		}
		key := ownerKey{
			owner: strings.TrimSpace(firstNonEmpty(point.OwnerSubjectID, point.SubjectID)),
			path:  strings.TrimSpace(point.CanonicalPath),
		}
		existing := merged[key]
		if existing == nil || point.Hotness > existing.Hotness {
			copy := point
			copy.AffectedSubjects = compactUnique(point.AffectedSubjects)
			copy.BackingIDs = compactUnique(point.BackingIDs)
			merged[key] = &copy
			continue
		}
		existing.AffectedSubjects = compactUnique(append(existing.AffectedSubjects, point.AffectedSubjects...))
		existing.BackingIDs = compactUnique(append(existing.BackingIDs, point.BackingIDs...))
	}
	out := make([]WideRiskObserverPoint, 0, len(merged))
	for _, point := range merged {
		out = append(out, *point)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hotness == out[j].Hotness {
			return out[i].Headline < out[j].Headline
		}
		return out[i].Hotness > out[j].Hotness
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func float64FromAny(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		var out float64
		fmt.Sscanf(strings.TrimSpace(t), "%f", &out)
		return out
	default:
		return 0
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
	redis "github.com/redis/go-redis/v9"
)

const (
	ServiceLineGeo        = "geo"
	ServiceLineMonitoring = "monitoring"
	ServiceLineScenario   = "scenario"

	watchSnapshotCacheVersion = "v1"
	defaultWatchSnapshotTTL   = 90 * time.Second
)

type DashboardRequest struct {
	TenantID     string
	UserID       string
	Mode         string
	LensID       string
	ServiceLine  string
	TimeHorizon  string
	QuickFilters []string
}

type DashboardWidget struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Title    string         `json:"title"`
	Value    string         `json:"value"`
	Subtitle string         `json:"subtitle,omitempty"`
	Tone     string         `json:"tone,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type MapFragmentReadModel struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Caption        string   `json:"caption,omitempty"`
	CanonicalPaths []string `json:"canonicalPaths,omitempty"`
	LayerHints     []string `json:"layerHints,omitempty"`
	ScopeType      string   `json:"scopeType,omitempty"`
	BriefID        string   `json:"briefId,omitempty"`
}

type MonitoringDeltaSnapshot struct {
	ID                 string    `json:"id"`
	Label              string    `json:"label"`
	CanonicalPath      string    `json:"canonicalPath"`
	Headline           string    `json:"headline"`
	Summary            string    `json:"summary"`
	RiskPosture        string    `json:"riskPosture,omitempty"`
	Hotness            float64   `json:"hotness"`
	Confidence         float64   `json:"confidence"`
	Status             string    `json:"status,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt"`
	DominantThemes     []string  `json:"dominantThemes,omitempty"`
	TopAffectedTargets []string  `json:"topAffectedTargets,omitempty"`
}

type ScenarioContractPreset struct {
	ID                string                 `json:"id"`
	Title             string                 `json:"title"`
	Description       string                 `json:"description,omitempty"`
	ContractType      string                 `json:"contractType"`
	DomainModes       []string               `json:"domainModes,omitempty"`
	TargetPaths       []string               `json:"targetPaths,omitempty"`
	SeedFragmentIDs   []string               `json:"seedFragmentIds,omitempty"`
	TriggerConditions []string               `json:"triggerConditions,omitempty"`
	Disconfirmers     []string               `json:"disconfirmers,omitempty"`
	Parameters        map[string]interface{} `json:"parameters,omitempty"`
}

type ScenarioWorkspaceSnapshot struct {
	Scope       string                   `json:"scope"`
	LensID      string                   `json:"lensId,omitempty"`
	LensName    string                   `json:"lensName,omitempty"`
	BaselineRef string                   `json:"baselineRef,omitempty"`
	TargetPaths []string                 `json:"targetPaths,omitempty"`
	Contracts   []ScenarioContractPreset `json:"contracts,omitempty"`
	GeneratedAt time.Time                `json:"generatedAt"`
}

type DashboardSnapshot struct {
	Mode              string                                 `json:"mode"`
	Scope             string                                 `json:"scope"`
	ServiceLine       string                                 `json:"serviceLine"`
	LensID            string                                 `json:"lensId,omitempty"`
	LensName          string                                 `json:"lensName,omitempty"`
	TimeHorizon       string                                 `json:"timeHorizon,omitempty"`
	GlobalHotSlices   []*horizonmodels.WatchBrief            `json:"globalHotSlices"`
	MyWatch           []*horizonmodels.WatchBrief            `json:"myWatch"`
	Feed              []*horizonmodels.WatchFeedEntry        `json:"feed"`
	Preferences       *horizonmodels.WatchPreferences        `json:"preferences,omitempty"`
	Dashboards        []DashboardWidget                      `json:"dashboards,omitempty"`
	MapFragments      []MapFragmentReadModel                 `json:"mapFragments,omitempty"`
	Monitoring        []*MonitoringDeltaSnapshot             `json:"monitoring,omitempty"`
	LensAttention     *horizonwatchruntime.LensAttentionView `json:"lensAttention,omitempty"`
	ScenarioWorkspace *ScenarioWorkspaceSnapshot             `json:"scenarioWorkspace,omitempty"`
	GeneratedAt       time.Time                              `json:"generatedAt"`
	Cached            bool                                   `json:"cached,omitempty"`
}

type supplyChainSubstrate struct {
	CanonicalKey       string
	DisplayName        string
	AnchorSupplier     string
	AnchorCustomer     string
	ProductAxis        string
	NodeRefs           []string
	GeoRefs            []string
	FlowRefs           []string
	ZoneRefs           []string
	AffectedZoneRefs   []string
	SpatialSemantics   []string
	LegCount           int
	BindingConfidence  float64
	PathStressScore    float64
	BottleneckLeg      int
	RecoveryLatencyHrs float64
}

func normalizeServiceLine(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case ServiceLineMonitoring:
		return ServiceLineMonitoring
	case ServiceLineScenario:
		return ServiceLineScenario
	default:
		return ServiceLineGeo
	}
}

func normalizeWatchMode(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return string(horizonmodels.WatchModeCombined)
	}
	switch v {
	case string(horizonmodels.WatchModeGlobal), string(horizonmodels.WatchModeMy), string(horizonmodels.WatchModeCombined):
		return v
	default:
		return string(horizonmodels.WatchModeCombined)
	}
}

func (s *Service) WithRedis(client *redis.Client) *Service {
	if s == nil {
		return s
	}
	s.redisClient = client
	return s
}

func (s *Service) WithLensAttentionProvider(fn func(context.Context, string, string, int) (*horizonwatchruntime.LensAttentionView, error)) *Service {
	if s == nil {
		return s
	}
	s.lensAttentionProvider = fn
	return s
}

func (s *Service) GetDashboardSnapshot(ctx context.Context, req DashboardRequest) (*DashboardSnapshot, error) {
	if s == nil || s.targetsRepo == nil || s.briefsRepo == nil {
		return nil, nil
	}
	req.TenantID = strings.TrimSpace(firstNonEmpty(req.TenantID, "global"))
	req.Mode = strings.TrimSpace(req.Mode)
	req.ServiceLine = normalizeServiceLine(req.ServiceLine)
	req.LensID = strings.TrimSpace(req.LensID)
	req.TimeHorizon = strings.TrimSpace(req.TimeHorizon)

	cacheKey := s.watchSnapshotCacheKey(req)
	if snapshot := s.readSnapshotCache(ctx, cacheKey); snapshot != nil {
		snapshot.Cached = true
		return snapshot, nil
	}

	snapshot, err := s.buildDashboardSnapshot(ctx, req)
	if err != nil {
		return nil, err
	}
	s.writeSnapshotCache(ctx, cacheKey, snapshot)
	return snapshot, nil
}

func (s *Service) GetScenarioWorkspace(ctx context.Context, req DashboardRequest) (*ScenarioWorkspaceSnapshot, error) {
	snapshot, err := s.GetDashboardSnapshot(ctx, req)
	if err != nil || snapshot == nil {
		return nil, err
	}
	return snapshot.ScenarioWorkspace, nil
}

func (s *Service) buildDashboardSnapshot(ctx context.Context, req DashboardRequest) (*DashboardSnapshot, error) {
	globalTargets, err := s.targetsRepo.ListByTenant(ctx, "global", false, 100)
	if err != nil {
		return nil, err
	}

	tenantTargets := []*horizonmodels.WatchTarget{}
	if req.TenantID != "global" {
		tenantTargets, err = s.targetsRepo.ListByTenant(ctx, req.TenantID, false, 100)
		if err != nil {
			return nil, err
		}
	}

	globalBriefs, err := s.refreshBriefs(ctx, globalTargets)
	if err != nil {
		return nil, err
	}
	myBriefs, err := s.refreshBriefs(ctx, tenantTargets)
	if err != nil {
		return nil, err
	}
	globalBriefs = filterBriefsByQuickFilters(globalBriefs, req.QuickFilters)
	myBriefs = filterBriefsByQuickFilters(myBriefs, req.QuickFilters)

	sort.Slice(globalBriefs, func(i, j int) bool { return globalBriefs[i].Hotness > globalBriefs[j].Hotness })
	sort.Slice(myBriefs, func(i, j int) bool { return myBriefs[i].Hotness > myBriefs[j].Hotness })

	var prefs *horizonmodels.WatchPreferences
	if s.prefsRepo != nil && strings.TrimSpace(req.UserID) != "" {
		prefs, _ = s.prefsRepo.GetByUser(ctx, req.TenantID, req.UserID)
	}
	effectiveMode := normalizeWatchMode(req.Mode)
	if strings.TrimSpace(req.Mode) == "" && prefs != nil {
		effectiveMode = normalizeWatchMode(prefs.Mode)
	}
	effectiveHorizon := strings.TrimSpace(req.TimeHorizon)
	if effectiveHorizon == "" && prefs != nil {
		effectiveHorizon = strings.TrimSpace(prefs.TimeHorizon)
	}
	if effectiveHorizon == "" {
		effectiveHorizon = "7d"
	}

	feed := s.buildDashboardFeed(ctx, effectiveMode, globalBriefs, myBriefs)

	scope := "tenant"
	if req.LensID != "" {
		scope = "lens"
	}

	var lens *horizonmodels.Lens
	if req.LensID != "" && s.lensesRepo != nil {
		lens, _ = s.lensesRepo.Get(ctx, req.LensID)
	}

	var lensAttention *horizonwatchruntime.LensAttentionView
	if req.LensID != "" && s.lensAttentionProvider != nil {
		lensAttention, _ = s.lensAttentionProvider(ctx, req.TenantID, req.LensID, 24)
	}
	supplyChainSubstrate := s.collectLensSupplyChainSubstrate(ctx, lens)
	substrateBriefs := buildSubstrateBriefs(lens, supplyChainSubstrate)

	effectiveGlobalBriefs := globalBriefs
	effectiveMyBriefs := myBriefs
	if req.LensID != "" && len(substrateBriefs) > 0 {
		effectiveGlobalBriefs = prependSyntheticBriefs(substrateBriefs, globalBriefs, 12)
		effectiveMyBriefs = prependSyntheticBriefs(substrateBriefs, myBriefs, 24)
	}

	combinedBriefs := make([]*horizonmodels.WatchBrief, 0, len(effectiveGlobalBriefs)+len(effectiveMyBriefs))
	combinedBriefs = append(combinedBriefs, effectiveGlobalBriefs...)
	combinedBriefs = append(combinedBriefs, effectiveMyBriefs...)

	snapshot := &DashboardSnapshot{
		Mode:            effectiveMode,
		Scope:           scope,
		ServiceLine:     req.ServiceLine,
		LensID:          req.LensID,
		TimeHorizon:     effectiveHorizon,
		GlobalHotSlices: truncateBriefs(effectiveGlobalBriefs, 12),
		MyWatch:         truncateBriefs(effectiveMyBriefs, 24),
		Feed:            feed,
		Preferences:     prefs,
		LensAttention:   lensAttention,
		GeneratedAt:     time.Now().UTC(),
	}
	if lensAttention != nil {
		snapshot.LensName = strings.TrimSpace(lensAttention.LensName)
	} else if lens != nil {
		snapshot.LensName = strings.TrimSpace(lens.Name)
	}

	req.Mode = effectiveMode
	req.TimeHorizon = effectiveHorizon
	snapshot.Dashboards = s.buildDashboardWidgets(req, effectiveGlobalBriefs, effectiveMyBriefs, feed, lensAttention, lens, supplyChainSubstrate)
	snapshot.MapFragments = s.buildMapFragments(req, effectiveGlobalBriefs, effectiveMyBriefs, lensAttention, lens, supplyChainSubstrate)
	snapshot.Monitoring = s.buildMonitoringSnapshots(ctx, req, effectiveMyBriefs, combinedBriefs, lensAttention, lens, supplyChainSubstrate)
	snapshot.ScenarioWorkspace = s.buildScenarioWorkspace(req, combinedBriefs, lensAttention, lens, supplyChainSubstrate)
	s.applyServiceLineShaping(snapshot, req)

	return snapshot, nil
}

func (s *Service) buildDashboardFeed(ctx context.Context, mode string, globalBriefs, myBriefs []*horizonmodels.WatchBrief) []*horizonmodels.WatchFeedEntry {
	feed := []*horizonmodels.WatchFeedEntry{}
	briefSource := globalBriefs
	switch mode {
	case string(horizonmodels.WatchModeMy):
		briefSource = myBriefs
	case string(horizonmodels.WatchModeCombined):
		briefSource = append(append([]*horizonmodels.WatchBrief{}, globalBriefs...), myBriefs...)
	}
	feedSeen := map[string]struct{}{}
	for _, brief := range briefSource {
		if brief == nil {
			continue
		}
		entries, err := s.feedRepo.ListByTarget(ctx, brief.TenantID, brief.TargetID, 3)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			key := strings.TrimSpace(entry.ID)
			if key == "" {
				key = strings.Join([]string{entry.TargetID, entry.EntryType, entry.CreatedAt.Format(time.RFC3339Nano)}, "|")
			}
			if _, ok := feedSeen[key]; ok {
				continue
			}
			feedSeen[key] = struct{}{}
			feed = append(feed, entry)
		}
	}
	sort.Slice(feed, func(i, j int) bool { return feed[i].CreatedAt.After(feed[j].CreatedAt) })
	if len(feed) > 12 {
		feed = feed[:12]
	}
	return feed
}

func (s *Service) buildDashboardWidgets(req DashboardRequest, globalBriefs, myBriefs []*horizonmodels.WatchBrief, feed []*horizonmodels.WatchFeedEntry, lensAttention *horizonwatchruntime.LensAttentionView, lens *horizonmodels.Lens, substrate []supplyChainSubstrate) []DashboardWidget {
	executive := globalBriefs
	if len(myBriefs) > 0 {
		executive = myBriefs
	}
	if req.Mode == string(horizonmodels.WatchModeGlobal) {
		executive = globalBriefs
	}

	topHotness := 0.0
	confidenceAvg := 0.0
	if len(executive) > 0 {
		topHotness = executive[0].Hotness
		limit := minInt(len(executive), 5)
		for i := 0; i < limit; i++ {
			confidenceAvg += executive[i].Confidence
		}
		confidenceAvg = confidenceAvg / float64(limit)
	}
	escalating := 0
	for _, brief := range append(append([]*horizonmodels.WatchBrief{}, globalBriefs...), myBriefs...) {
		if brief != nil && brief.Hotness >= 0.7 {
			escalating++
		}
	}

	coverageValue := len(myBriefs)
	if lensAttention != nil {
		coverageValue = len(lensAttention.Subjects)
	}

	switch req.ServiceLine {
	case ServiceLineMonitoring:
		coverageGaps := 0
		liveDeltaCount := len(feed)
		if lensAttention != nil {
			for _, subject := range lensAttention.Subjects {
				if subject == nil {
					continue
				}
				coverageGaps += len(subject.CoverageGaps)
			}
			liveDeltaCount = len(lensAttention.Feed)
			if req.LensID != "" && len(lensAttention.Subjects) == 0 {
				coverageGaps = maxInt(coverageGaps, 1)
			}
		}
		monitoringSource := myBriefs
		if req.LensID != "" && lensAttention != nil && len(lensAttention.Subjects) == 0 {
			monitoringSource = nil
		} else if req.Mode == string(horizonmodels.WatchModeGlobal) || len(monitoringSource) == 0 {
			monitoringSource = globalBriefs
		}
		activeEscalations := 0
		for _, brief := range monitoringSource {
			if brief != nil && (brief.Hotness >= 0.65 || strings.EqualFold(strings.TrimSpace(brief.RiskPosture), "elevated")) {
				activeEscalations++
			}
		}
		return []DashboardWidget{
			{
				ID:       "monitoring-coverage",
				Kind:     "metric",
				Title:    "Active watch",
				Value:    fmt.Sprintf("%d", coverageValue),
				Subtitle: "Scopes or subjects under active monitoring",
				Tone:     "neutral",
			},
			{
				ID:       "monitoring-escalating",
				Kind:     "metric",
				Title:    "Escalating",
				Value:    fmt.Sprintf("%d", activeEscalations),
				Subtitle: "Scopes requiring disciplined review",
				Tone:     toneFromValue(float64(activeEscalations) / float64(maxInt(1, len(monitoringSource)))),
			},
			{
				ID:       "monitoring-feed",
				Kind:     "metric",
				Title:    "Live deltas",
				Value:    fmt.Sprintf("%d", liveDeltaCount),
				Subtitle: monitoringFeedSubtitle(req, lensAttention),
				Tone:     toneFromValue(float64(liveDeltaCount) / 12.0),
			},
			{
				ID:       "monitoring-gaps",
				Kind:     "metric",
				Title:    "Coverage gaps",
				Value:    fmt.Sprintf("%d", coverageGaps),
				Subtitle: "Known blind spots in current monitoring",
				Tone:     toneFromValue(float64(coverageGaps) / 6.0),
			},
		}
	case ServiceLineScenario:
		targetPathCount := 0
		if len(substrate) > 0 {
			for _, item := range substrate {
				targetPathCount += len(compactUnique(append(append(append(append([]string{}, item.NodeRefs...), item.GeoRefs...), item.FlowRefs...), item.AffectedZoneRefs...)))
			}
		} else if lensAttention != nil {
			for _, subject := range lensAttention.Subjects {
				if subject == nil {
					continue
				}
				targetPathCount++
				targetPathCount += len(subject.UniversePaths)
			}
		}
		if targetPathCount == 0 {
			for _, brief := range append(append([]*horizonmodels.WatchBrief{}, globalBriefs...), myBriefs...) {
				if brief == nil {
					continue
				}
				targetPathCount++
				targetPathCount += len(brief.TopAffectedTargets)
			}
		}
		return []DashboardWidget{
			{
				ID:       "scenario-baseline-pressure",
				Kind:     "metric",
				Title:    "Baseline pressure",
				Value:    fmt.Sprintf("%d%%", int(topHotness*100)),
				Subtitle: "Current baseline before branching",
				Tone:     toneFromValue(topHotness),
			},
			{
				ID:       "scenario-contracts",
				Kind:     "metric",
				Title:    "Scenario contracts",
				Value:    "3",
				Subtitle: scenarioContractsSubtitle(lens, substrate),
				Tone:     "accent",
			},
			{
				ID:       "scenario-target-paths",
				Kind:     "metric",
				Title:    "Target paths",
				Value:    fmt.Sprintf("%d", targetPathCount),
				Subtitle: "Projected scope touched by scenario branches",
				Tone:     toneFromValue(float64(targetPathCount) / 12.0),
			},
			{
				ID:       "scenario-readiness",
				Kind:     "metric",
				Title:    "Branch readiness",
				Value:    fmt.Sprintf("%d%%", int(confidenceAvg*100)),
				Subtitle: "Confidence in current branching baseline",
				Tone:     toneFromValue(confidenceAvg),
			},
		}
	}

	widgets := []DashboardWidget{
		{
			ID:       "pressure-index",
			Kind:     "metric",
			Title:    "Pressure index",
			Value:    fmt.Sprintf("%d%%", int(topHotness*100)),
			Subtitle: "Current top-scope pressure",
			Tone:     toneFromValue(topHotness),
		},
		{
			ID:       "coverage",
			Kind:     "metric",
			Title:    "Coverage",
			Value:    fmt.Sprintf("%d", coverageValue),
			Subtitle: "Watched targets in active scope",
			Tone:     "neutral",
		},
		{
			ID:       "signal-velocity",
			Kind:     "metric",
			Title:    "Signal velocity",
			Value:    fmt.Sprintf("%d", len(feed)),
			Subtitle: "Fresh deltas in live stream",
			Tone:     toneFromValue(float64(len(feed)) / 12.0),
		},
		{
			ID:       "confidence-band",
			Kind:     "metric",
			Title:    "Confidence band",
			Value:    fmt.Sprintf("%d%%", int(confidenceAvg*100)),
			Subtitle: fmt.Sprintf("%d scopes are escalating", escalating),
			Tone:     toneFromValue(confidenceAvg),
		},
	}
	if req.ServiceLine == ServiceLineScenario {
		widgets = append(widgets, DashboardWidget{
			ID:       "scenario-contracts",
			Kind:     "metric",
			Title:    "Scenario contracts",
			Value:    "3",
			Subtitle: "Supply chain, governance, regulatory",
			Tone:     "accent",
		})
	}
	return widgets
}

func (s *Service) buildMapFragments(req DashboardRequest, globalBriefs, myBriefs []*horizonmodels.WatchBrief, lensAttention *horizonwatchruntime.LensAttentionView, lens *horizonmodels.Lens, substrate []supplyChainSubstrate) []MapFragmentReadModel {
	out := []MapFragmentReadModel{}
	seen := map[string]struct{}{}
	add := func(id, title, caption, scopeType string, paths []string) {
		if len(out) >= 4 {
			return
		}
		paths = compactUnique(paths)
		if len(paths) == 0 {
			return
		}
		key := strings.Join(paths, "|")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, MapFragmentReadModel{
			ID:             id,
			Title:          title,
			Caption:        caption,
			CanonicalPaths: paths,
			LayerHints:     layerHintsForPaths(paths),
			ScopeType:      scopeType,
			BriefID:        id,
		})
	}

	if req.LensID != "" && len(substrate) > 0 {
		for _, item := range substrate {
			add(
				"artifact:"+strings.Trim(strings.TrimPrefix(item.CanonicalKey, "/"), "/"),
				firstNonEmpty(item.DisplayName, labelFromCanonicalPath(item.AnchorCustomer), labelFromCanonicalPath(item.AnchorSupplier), "Supply chain path"),
				substrateCaption(item),
				"path_overlay",
				substrateCanonicalPaths(item),
			)
		}
	}

	source := globalBriefs
	if req.Mode != string(horizonmodels.WatchModeGlobal) && len(myBriefs) > 0 {
		source = myBriefs
	}
	for _, brief := range source {
		if brief == nil {
			continue
		}
		add(
			brief.ID,
			brief.Label,
			firstNonEmpty(brief.Headline, brief.Summary),
			brief.ScopeType,
			append([]string{brief.CanonicalPath}, brief.TopAffectedTargets...),
		)
	}

	if lensAttention != nil {
		for _, subject := range lensAttention.Subjects {
			if subject == nil {
				continue
			}
			add(
				subject.SubjectID,
				subject.DisplayLabel,
				firstNonEmpty(subject.Status, subject.SubjectKind),
				subject.SubjectKind,
				append([]string{subject.AnchorCanonicalPath}, subject.UniversePaths...),
			)
		}
	}
	if req.ServiceLine == ServiceLineMonitoring && len(out) > 2 {
		return out[:2]
	}
	if req.ServiceLine == ServiceLineScenario && len(out) > 2 {
		return out[:2]
	}
	return out
}

func (s *Service) buildMonitoringSnapshots(ctx context.Context, req DashboardRequest, myBriefs, combinedBriefs []*horizonmodels.WatchBrief, lensAttention *horizonwatchruntime.LensAttentionView, lens *horizonmodels.Lens, substrate []supplyChainSubstrate) []*MonitoringDeltaSnapshot {
	if lensAttention != nil && len(lensAttention.Subjects) > 0 {
		out := make([]*MonitoringDeltaSnapshot, 0, len(lensAttention.Subjects))
		for _, subject := range lensAttention.Subjects {
			if subject == nil {
				continue
			}
			updatedAt := time.Time{}
			if subject.LastRun != nil {
				updatedAt = subject.LastRun.UpdatedAt
			}
			out = append(out, &MonitoringDeltaSnapshot{
				ID:                 subject.SubjectID,
				Label:              subject.DisplayLabel,
				CanonicalPath:      subject.AnchorCanonicalPath,
				Headline:           firstNonEmpty(subject.DisplayLabel, subject.AnchorCanonicalPath),
				Summary:            firstNonEmpty(strings.Join(subject.CoverageGaps, " • "), strings.Join(subject.DomainModes, ", ")),
				Status:             subject.Status,
				UpdatedAt:          updatedAt,
				DominantThemes:     append([]string(nil), subject.DomainModes...),
				TopAffectedTargets: append([]string(nil), subject.UniversePaths...),
			})
		}
		return out
	}

	if req.LensID != "" && lens != nil {
		if len(substrate) > 0 {
			return buildSubstrateMonitoringSnapshots(lens, substrate)
		}
		return []*MonitoringDeltaSnapshot{buildLensBootstrapMonitoringSnapshot(lens)}
	}

	source := myBriefs
	if req.Mode == string(horizonmodels.WatchModeGlobal) {
		source = combinedBriefs
	}
	out := make([]*MonitoringDeltaSnapshot, 0, minInt(len(source), 8))
	for _, brief := range source {
		if brief == nil {
			continue
		}
		out = append(out, &MonitoringDeltaSnapshot{
			ID:                 brief.ID,
			Label:              brief.Label,
			CanonicalPath:      brief.CanonicalPath,
			Headline:           brief.Headline,
			Summary:            brief.Summary,
			RiskPosture:        brief.RiskPosture,
			Hotness:            brief.Hotness,
			Confidence:         brief.Confidence,
			Status:             strings.TrimSpace(asString(brief.Metadata["targetStatus"])),
			UpdatedAt:          brief.UpdatedAt,
			DominantThemes:     append([]string(nil), brief.DominantThemes...),
			TopAffectedTargets: append([]string(nil), brief.TopAffectedTargets...),
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func monitoringFeedSubtitle(req DashboardRequest, lensAttention *horizonwatchruntime.LensAttentionView) string {
	if req.LensID != "" && lensAttention != nil {
		if len(lensAttention.Subjects) == 0 {
			return "Lens runtime not materialized yet"
		}
		return "Lens-native interpreted changes"
	}
	return "Interpreted changes in the watch layer"
}

func buildLensBootstrapMonitoringSnapshot(lens *horizonmodels.Lens) *MonitoringDeltaSnapshot {
	if lens == nil {
		return nil
	}
	scopePaths := make([]string, 0, len(lens.Scope))
	for _, path := range lens.Scope {
		path = strings.TrimSpace(path)
		if !isWatchScopeTargetPath(path) {
			continue
		}
		scopePaths = append(scopePaths, path)
	}
	summary := "Lens runtime is not materialized yet. Core watchruntime expects tenant sync or explicit watch/bootstrap compilation before subject-level monitoring becomes available."
	if modes := compactUnique(append([]string(nil), lens.DomainModes...)); len(modes) > 0 {
		summary = summary + " Domain modes: " + strings.Join(modes, ", ")
	}
	return &MonitoringDeltaSnapshot{
		ID:                 "lens-bootstrap:" + strings.TrimSpace(lens.ID),
		Label:              firstNonEmpty(strings.TrimSpace(lens.Name), strings.TrimSpace(lens.ID)),
		CanonicalPath:      "/lens/" + strings.TrimSpace(lens.ID),
		Headline:           "Monitoring runtime requires materialization",
		Summary:            summary,
		RiskPosture:        "bootstrap_required",
		Hotness:            0.35,
		Confidence:         0.92,
		Status:             "PENDING_RUNTIME",
		UpdatedAt:          lens.UpdatedAt,
		DominantThemes:     append([]string(nil), lens.DomainModes...),
		TopAffectedTargets: firstNStrings(compactUnique(scopePaths), 6),
	}
}

func (s *Service) applyServiceLineShaping(snapshot *DashboardSnapshot, req DashboardRequest) {
	if snapshot == nil {
		return
	}
	switch req.ServiceLine {
	case ServiceLineGeo:
		snapshot.GlobalHotSlices = truncateBriefs(snapshot.GlobalHotSlices, 8)
		snapshot.MyWatch = truncateBriefs(snapshot.MyWatch, 12)
		snapshot.Monitoring = truncateMonitoringSnapshots(snapshot.Monitoring, 3)
		snapshot.Feed = truncateFeedEntries(snapshot.Feed, 6)
		snapshot.ScenarioWorkspace = nil
	case ServiceLineMonitoring:
		snapshot.GlobalHotSlices = truncateBriefs(snapshot.GlobalHotSlices, 4)
		snapshot.MyWatch = truncateBriefs(snapshot.MyWatch, 16)
		snapshot.MapFragments = nil
		snapshot.ScenarioWorkspace = nil
	case ServiceLineScenario:
		snapshot.GlobalHotSlices = truncateBriefs(snapshot.GlobalHotSlices, 3)
		snapshot.MyWatch = truncateBriefs(snapshot.MyWatch, 8)
		snapshot.Monitoring = nil
		snapshot.Feed = truncateFeedEntries(snapshot.Feed, 4)
		snapshot.MapFragments = truncateMapFragments(snapshot.MapFragments, 2)
	}
}

func (s *Service) buildScenarioWorkspace(req DashboardRequest, briefs []*horizonmodels.WatchBrief, lensAttention *horizonwatchruntime.LensAttentionView, lens *horizonmodels.Lens, substrate []supplyChainSubstrate) *ScenarioWorkspaceSnapshot {
	targetPaths, seedFragmentIDs := buildScenarioScopeInputs(briefs, lensAttention, lens, substrate)
	locationPath := deriveScenarioLocationPath(targetPaths)
	baselineRef := describeScenarioBaseline(lens, substrate)
	artifactParameters := scenarioArtifactParameters(substrate)

	contracts := []ScenarioContractPreset{
		{
			ID:           "supply-chain",
			Title:        "Supply Chain Disruption",
			Description:  "Stress-test landed-cost escalation, vendor substitution pressure, and downstream throughput drag.",
			ContractType: "policy-impact",
			DomainModes:  []string{"supply_chain", "strategic"},
			TargetPaths:  firstNStrings(targetPaths, 8),
			TriggerConditions: []string{
				"Critical corridor throughput falls below baseline",
				"Primary vendor substitution risk climbs",
			},
			Disconfirmers: []string{
				"Alternative routes absorb pressure without sustained delay",
				"Exposure remains local and does not propagate to dependent markets",
			},
			Parameters: map[string]interface{}{
				"location_path":             locationPath,
				"vat_rate_from":             12.0,
				"vat_rate_to":               14.0,
				"pass_through_rate":         0.88,
				"demand_elasticity":         -0.42,
				"compliance_elasticity":     0.12,
				"shadow_sensitivity":        0.18,
				"price_basket_share":        0.48,
				"wage_indexation_rate":      0.20,
				"investment_sensitivity":    0.58,
				"consumption_tax_base_coef": 0.82,
			},
			SeedFragmentIDs: firstNStrings(seedFragmentIDs, 6),
		},
		{
			ID:           "governance",
			Title:        "Governance Stress",
			Description:  "Model policy incoherence, compliance slippage, and institutional response drag.",
			ContractType: "policy-impact",
			DomainModes:  []string{"governance", "policy_impact"},
			TargetPaths:  firstNStrings(targetPaths, 8),
			TriggerConditions: []string{
				"Policy posture becomes inconsistent across allied nodes",
				"Trust deterioration changes procurement or alliance behavior",
			},
			Disconfirmers: []string{
				"Official posture stabilizes and trust signals recover",
				"No measurable change appears in partner behavior",
			},
			Parameters: map[string]interface{}{
				"location_path":             locationPath,
				"vat_rate_from":             12.0,
				"vat_rate_to":               13.0,
				"pass_through_rate":         0.54,
				"demand_elasticity":         -0.26,
				"compliance_elasticity":     0.33,
				"shadow_sensitivity":        0.34,
				"price_basket_share":        0.36,
				"wage_indexation_rate":      0.12,
				"investment_sensitivity":    0.72,
				"consumption_tax_base_coef": 0.74,
			},
			SeedFragmentIDs: firstNStrings(seedFragmentIDs, 6),
		},
		{
			ID:           "regulatory",
			Title:        "Regulatory Impact",
			Description:  "Test licensing, compliance, sanctions, and approval-surface tightening.",
			ContractType: "policy-impact",
			DomainModes:  []string{"policy_impact", "strategic", "market"},
			TargetPaths:  firstNStrings(targetPaths, 8),
			TriggerConditions: []string{
				"Controls tighten on dual-use or financial channels",
				"Cross-border compliance costs begin to reshape routing decisions",
			},
			Disconfirmers: []string{
				"Regulatory moves remain symbolic and do not affect execution",
				"Counterparties keep operating under the same approvals baseline",
			},
			Parameters: map[string]interface{}{
				"location_path":             locationPath,
				"vat_rate_from":             12.0,
				"vat_rate_to":               15.0,
				"pass_through_rate":         0.74,
				"demand_elasticity":         -0.31,
				"compliance_elasticity":     0.24,
				"shadow_sensitivity":        0.22,
				"price_basket_share":        0.41,
				"wage_indexation_rate":      0.18,
				"investment_sensitivity":    0.64,
				"consumption_tax_base_coef": 0.78,
			},
			SeedFragmentIDs: firstNStrings(seedFragmentIDs, 6),
		},
	}
	for i := range contracts {
		for key, value := range artifactParameters {
			contracts[i].Parameters[key] = value
		}
	}

	scope := "tenant"
	if req.LensID != "" {
		scope = "lens"
	}
	workspace := &ScenarioWorkspaceSnapshot{
		Scope:       scope,
		LensID:      req.LensID,
		BaselineRef: baselineRef,
		TargetPaths: firstNStrings(targetPaths, 12),
		Contracts:   contracts,
		GeneratedAt: time.Now().UTC(),
	}
	if lensAttention != nil {
		workspace.LensName = strings.TrimSpace(lensAttention.LensName)
	} else if lens != nil {
		workspace.LensName = strings.TrimSpace(lens.Name)
	}
	return workspace
}

func buildScenarioScopeInputs(briefs []*horizonmodels.WatchBrief, lensAttention *horizonwatchruntime.LensAttentionView, lens *horizonmodels.Lens, substrate []supplyChainSubstrate) ([]string, []string) {
	targetPaths := []string{}
	seedFragmentIDs := []string{}
	if len(substrate) > 0 {
		for _, item := range substrate {
			targetPaths = append(targetPaths, substrateCanonicalPaths(item)...)
			if lens != nil {
				for _, path := range item.NodeRefs {
					if fragmentID := strings.TrimSpace(lens.ScopeFragments[path]); fragmentID != "" {
						seedFragmentIDs = append(seedFragmentIDs, fragmentID)
					}
				}
				for _, path := range []string{item.AnchorSupplier, item.AnchorCustomer} {
					if fragmentID := strings.TrimSpace(lens.ScopeFragments[path]); fragmentID != "" {
						seedFragmentIDs = append(seedFragmentIDs, fragmentID)
					}
				}
			}
		}
	}
	if lensAttention != nil {
		for _, subject := range lensAttention.Subjects {
			if subject == nil {
				continue
			}
			targetPaths = append(targetPaths, subject.AnchorCanonicalPath)
			targetPaths = append(targetPaths, subject.UniversePaths...)
			if id := strings.TrimSpace(subject.AnchorFragmentID); id != "" {
				seedFragmentIDs = append(seedFragmentIDs, id)
			}
		}
	}
	if lens != nil {
		for _, path := range lens.Scope {
			path = strings.TrimSpace(path)
			if !isWatchScopeTargetPath(path) {
				continue
			}
			targetPaths = append(targetPaths, path)
		}
		for _, fragmentID := range lens.ScopeFragments {
			if id := strings.TrimSpace(fragmentID); id != "" {
				seedFragmentIDs = append(seedFragmentIDs, id)
			}
		}
	}
	for _, brief := range briefs {
		if brief == nil {
			continue
		}
		if lens != nil && !briefTouchesLensScope(brief, lens) {
			continue
		}
		targetPaths = append(targetPaths, brief.CanonicalPath)
		targetPaths = append(targetPaths, brief.TopAffectedTargets...)
	}
	return compactUnique(targetPaths), compactUnique(seedFragmentIDs)
}

func (s *Service) collectLensSupplyChainSubstrate(ctx context.Context, lens *horizonmodels.Lens) []supplyChainSubstrate {
	if lens == nil {
		return nil
	}
	byKey := map[string]supplyChainSubstrate{}
	add := func(key string, ref horizonmodels.ScopeArtifactRef) {
		key = normalizeArtifactCanonicalKey(key)
		if key == "" || strings.TrimSpace(ref.Kind) != string(horizonmodels.MaterializedKindSupplyChainPath) {
			return
		}
		byKey[key] = supplyChainSubstrate{
			CanonicalKey:       key,
			DisplayName:        strings.TrimSpace(ref.DisplayName),
			AnchorSupplier:     strings.TrimSpace(ref.AnchorSupplier),
			AnchorCustomer:     strings.TrimSpace(ref.AnchorCustomer),
			ProductAxis:        strings.TrimSpace(ref.ProductAxis),
			NodeRefs:           compactUnique(append([]string(nil), ref.NodeRefs...)),
			GeoRefs:            compactUnique(append([]string(nil), ref.GeoRefs...)),
			FlowRefs:           compactUnique(append([]string(nil), ref.FlowRefs...)),
			ZoneRefs:           compactUnique(append([]string(nil), ref.ZoneRefs...)),
			AffectedZoneRefs:   compactUnique(append([]string(nil), ref.AffectedZoneRefs...)),
			SpatialSemantics:   compactUnique(append([]string(nil), ref.SpatialSemantics...)),
			LegCount:           ref.LegCount,
			BindingConfidence:  ref.BindingConfidence,
			PathStressScore:    ref.PathStressScore,
			BottleneckLeg:      ref.BottleneckLegIndex,
			RecoveryLatencyHrs: ref.RecoveryLatencyHrs,
		}
	}
	for key, ref := range lens.ScopeArtifacts {
		add(key, ref)
	}
	if s != nil && s.materializedArtifactsRepo != nil && strings.TrimSpace(lens.ID) != "" {
		if artifacts, err := s.materializedArtifactsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 64); err == nil {
			for _, artifact := range artifacts {
				if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
					continue
				}
				add(artifact.CanonicalKey, scopeArtifactRefFromMaterializedArtifact(artifact))
			}
		}
		if len(lens.ScopeArtifacts) > 0 {
			keys := make([]string, 0, len(lens.ScopeArtifacts))
			for key, ref := range lens.ScopeArtifacts {
				if strings.TrimSpace(ref.Kind) != string(horizonmodels.MaterializedKindSupplyChainPath) {
					continue
				}
				keys = append(keys, normalizeArtifactCanonicalKey(key))
			}
			if fetched, err := s.materializedArtifactsRepo.BatchGetByCanonicalKeys(ctx, strings.TrimSpace(firstNonEmpty(lens.TenantID, "global")), horizonmodels.MaterializedKindSupplyChainPath, compactUnique(keys)); err == nil {
				for _, artifact := range fetched {
					if artifact == nil {
						continue
					}
					add(artifact.CanonicalKey, scopeArtifactRefFromMaterializedArtifact(artifact))
				}
			}
		}
	}
	out := make([]supplyChainSubstrate, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PathStressScore == out[j].PathStressScore {
			if out[i].BindingConfidence == out[j].BindingConfidence {
				if out[i].LegCount == out[j].LegCount {
					return out[i].CanonicalKey < out[j].CanonicalKey
				}
				return out[i].LegCount > out[j].LegCount
			}
			return out[i].BindingConfidence > out[j].BindingConfidence
		}
		return out[i].PathStressScore > out[j].PathStressScore
	})
	return out
}

func scopeArtifactRefFromMaterializedArtifact(artifact *horizonmodels.MaterializedArtifact) horizonmodels.ScopeArtifactRef {
	if artifact == nil {
		return horizonmodels.ScopeArtifactRef{}
	}
	horizonmodels.PopulateSupplyChainPathIndexes(artifact)
	ref := horizonmodels.ScopeArtifactRef{
		Kind:             string(artifact.Kind),
		DisplayName:      strings.TrimSpace(artifact.DisplayName),
		AnchorSupplier:   strings.TrimSpace(artifact.AnchorSupplier),
		AnchorCustomer:   strings.TrimSpace(artifact.AnchorCustomer),
		ProductAxis:      strings.TrimSpace(artifact.ProductAxis),
		NodeRefs:         compactUnique(append([]string(nil), artifact.NodeRefs...)),
		Status:           strings.TrimSpace(artifact.PathStatus),
		GeoRefs:          compactUnique(append([]string(nil), artifact.GeoRefs...)),
		FlowRefs:         compactUnique(append([]string(nil), artifact.FlowRefs...)),
		ZoneRefs:         compactUnique(append([]string(nil), artifact.ZoneRefs...)),
		AffectedZoneRefs: compactUnique(append([]string(nil), artifact.AffectedZoneRefs...)),
	}
	if artifact.Kind == horizonmodels.MaterializedKindSupplyChainPath {
		if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
			summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
			ref.GeoRefs = summary.GeoRefs
			ref.FlowRefs = summary.FlowRefs
			ref.ZoneRefs = summary.ZoneRefs
			ref.AffectedZoneRefs = summary.AffectedZoneRefs
			ref.SpatialSemantics = summary.SpatialSemantics
			ref.LegCount = summary.LegCount
			ref.BindingConfidence = summary.BindingConfidence
			ref.PathStressScore = summary.PathStressScore
			ref.BottleneckLegIndex = summary.BottleneckLegIndex
			ref.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
		}
	}
	return ref
}

func buildSubstrateMonitoringSnapshots(lens *horizonmodels.Lens, substrate []supplyChainSubstrate) []*MonitoringDeltaSnapshot {
	out := make([]*MonitoringDeltaSnapshot, 0, minInt(len(substrate)+1, 4))
	if lens != nil {
		out = append(out, buildLensBootstrapMonitoringSnapshot(lens))
	}
	for _, item := range substrate {
		out = append(out, &MonitoringDeltaSnapshot{
			ID:            "artifact-monitor:" + strings.Trim(strings.TrimPrefix(item.CanonicalKey, "/"), "/"),
			Label:         firstNonEmpty(item.DisplayName, labelFromCanonicalPath(item.AnchorCustomer), labelFromCanonicalPath(item.AnchorSupplier), "Supply chain path"),
			CanonicalPath: "/artifact/" + strings.Trim(strings.TrimPrefix(item.CanonicalKey, "/"), "/"),
			Headline:      firstNonEmpty(item.DisplayName, "Supply chain substrate"),
			Summary:       substrateCaption(item),
			RiskPosture:   substrateRiskPosture(item),
			Hotness:       maxFloat(item.PathStressScore, 0.28),
			Confidence:    maxFloat(item.BindingConfidence, 0.62),
			Status:        "PATH_SUBSTRATE",
			UpdatedAt:     lens.UpdatedAt,
			DominantThemes: compactUnique([]string{
				"supply_chain",
				item.ProductAxis,
			}),
			TopAffectedTargets: firstNStrings(substrateCanonicalPaths(item), 6),
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func buildSubstrateBriefs(lens *horizonmodels.Lens, substrate []supplyChainSubstrate) []*horizonmodels.WatchBrief {
	if len(substrate) == 0 {
		return nil
	}
	tenantID := "global"
	if lens != nil && strings.TrimSpace(lens.TenantID) != "" {
		tenantID = strings.TrimSpace(lens.TenantID)
	}
	out := make([]*horizonmodels.WatchBrief, 0, minInt(len(substrate), 4))
	now := time.Now().UTC()
	for _, item := range substrate {
		artifactPath := "/artifact/" + strings.Trim(strings.TrimPrefix(item.CanonicalKey, "/"), "/")
		paths := firstNStrings(substrateCanonicalPaths(item), 8)
		explanation := "Lens-scoped Watch is grounded in the materialized supply-chain substrate for this scope. Interpretation, monitoring, and scenario overlays should resolve through this path before expanding to adjacent context."
		out = append(out, &horizonmodels.WatchBrief{
			ID:            "artifact-brief:" + strings.Trim(strings.TrimPrefix(item.CanonicalKey, "/"), "/"),
			TenantID:      tenantID,
			TargetID:      "artifact:" + strings.Trim(strings.TrimPrefix(item.CanonicalKey, "/"), "/"),
			CanonicalPath: artifactPath,
			Label:         firstNonEmpty(item.DisplayName, labelFromCanonicalPath(item.AnchorCustomer), labelFromCanonicalPath(item.AnchorSupplier), "Supply chain substrate"),
			ScopeType:     "path_overlay",
			Headline:      firstNonEmpty(item.DisplayName, "Supply chain substrate"),
			Summary:       firstNonEmpty(substrateCaption(item), explanation),
			Explanation:   explanation,
			Hotness:       maxFloat(item.PathStressScore, 0.3),
			Confidence:    maxFloat(item.BindingConfidence, 0.68),
			RiskPosture:   substrateRiskPosture(item),
			DominantThemes: compactUnique([]string{
				"supply_chain",
				"path_carried",
				item.ProductAxis,
			}),
			TopAffectedTargets: paths,
			Metadata: map[string]any{
				"syntheticArtifact":        true,
				"syntheticKind":            "supply_chain_substrate",
				"artifactCanonicalKey":     item.CanonicalKey,
				"artifactDisplayName":      item.DisplayName,
				"artifactCanonicalPath":    artifactPath,
				"artifactPaths":            append([]string(nil), paths...),
				"artifactFlowRefs":         append([]string(nil), item.FlowRefs...),
				"artifactAffectedZoneRefs": append([]string(nil), item.AffectedZoneRefs...),
				"productAxis":              item.ProductAxis,
				"anchorSupplier":           item.AnchorSupplier,
				"anchorCustomer":           item.AnchorCustomer,
				"pathStressScore":          item.PathStressScore,
				"bindingConfidence":        item.BindingConfidence,
				"bottleneckLegIndex":       item.BottleneckLeg,
				"recoveryLatencyHours":     item.RecoveryLatencyHrs,
				"spatialSemantics":         append([]string(nil), item.SpatialSemantics...),
				"localEvidence": []map[string]any{
					{
						"type":    "scope_artifact",
						"summary": firstNonEmpty(item.DisplayName, item.CanonicalKey),
					},
					{
						"type":    "binding",
						"summary": fmt.Sprintf("Binding confidence %.0f%% with path stress %.0f%%", maxFloat(item.BindingConfidence, 0)*100, maxFloat(item.PathStressScore, 0)*100),
					},
				},
				"localSources": []string{
					item.CanonicalKey,
					"lens.scopeArtifacts",
				},
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func prependSyntheticBriefs(prepend []*horizonmodels.WatchBrief, existing []*horizonmodels.WatchBrief, limit int) []*horizonmodels.WatchBrief {
	merged := make([]*horizonmodels.WatchBrief, 0, len(prepend)+len(existing))
	seen := make(map[string]struct{}, len(prepend)+len(existing))
	add := func(brief *horizonmodels.WatchBrief) {
		if brief == nil {
			return
		}
		key := strings.TrimSpace(firstNonEmpty(brief.ID, brief.TargetID, brief.CanonicalPath))
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, brief)
	}
	for _, brief := range prepend {
		add(brief)
		if limit > 0 && len(merged) >= limit {
			return merged[:limit]
		}
	}
	for _, brief := range existing {
		add(brief)
		if limit > 0 && len(merged) >= limit {
			return merged[:limit]
		}
	}
	return merged
}

func scenarioContractsSubtitle(lens *horizonmodels.Lens, substrate []supplyChainSubstrate) string {
	if len(substrate) > 0 {
		return "Supply-chain substrate with governance and regulatory overlays"
	}
	if lens != nil && containsStringFold(lens.DomainModes, "supply_chain") {
		return "Supply chain, governance, regulatory overlays"
	}
	return "Supply chain, governance, regulatory"
}

func describeScenarioBaseline(lens *horizonmodels.Lens, substrate []supplyChainSubstrate) string {
	if len(substrate) > 0 {
		top := substrate[0]
		return firstNonEmpty(top.DisplayName, labelFromCanonicalPath(top.AnchorCustomer), labelFromCanonicalPath(top.AnchorSupplier), strings.TrimSpace(top.CanonicalKey))
	}
	if lens != nil {
		return firstNonEmpty(strings.TrimSpace(lens.Name), "Baseline watch scope")
	}
	return ""
}

func scenarioArtifactParameters(substrate []supplyChainSubstrate) map[string]interface{} {
	if len(substrate) == 0 {
		return nil
	}
	top := substrate[0]
	params := map[string]interface{}{
		"baseline_artifact_key":       top.CanonicalKey,
		"baseline_binding_confidence": top.BindingConfidence,
		"baseline_path_stress_score":  top.PathStressScore,
	}
	if top.AnchorSupplier != "" {
		params["anchor_supplier"] = top.AnchorSupplier
	}
	if top.AnchorCustomer != "" {
		params["anchor_customer"] = top.AnchorCustomer
	}
	if top.ProductAxis != "" {
		params["product_axis"] = top.ProductAxis
	}
	if len(top.FlowRefs) > 0 {
		params["flow_refs"] = append([]string(nil), top.FlowRefs...)
	}
	if len(top.AffectedZoneRefs) > 0 {
		params["affected_zone_refs"] = append([]string(nil), top.AffectedZoneRefs...)
	}
	return params
}

func substrateCanonicalPaths(item supplyChainSubstrate) []string {
	paths := []string{}
	paths = append(paths, strings.TrimSpace(item.AnchorSupplier))
	paths = append(paths, strings.TrimSpace(item.AnchorCustomer))
	paths = append(paths, item.NodeRefs...)
	paths = append(paths, item.GeoRefs...)
	paths = append(paths, item.FlowRefs...)
	paths = append(paths, item.ZoneRefs...)
	paths = append(paths, item.AffectedZoneRefs...)
	out := make([]string, 0, len(paths))
	for _, path := range compactUnique(paths) {
		if isWatchScopeTargetPath(path) {
			out = append(out, path)
		}
	}
	return out
}

func substrateCaption(item supplyChainSubstrate) string {
	parts := []string{}
	if item.ProductAxis != "" {
		parts = append(parts, item.ProductAxis)
	}
	if item.AnchorSupplier != "" || item.AnchorCustomer != "" {
		parts = append(parts, strings.Trim(strings.Join([]string{labelFromCanonicalPath(item.AnchorSupplier), labelFromCanonicalPath(item.AnchorCustomer)}, " -> "), " ->"))
	}
	if item.LegCount > 0 {
		parts = append(parts, fmt.Sprintf("%d legs", item.LegCount))
	}
	if item.LegCount > 0 && item.PathStressScore > 0 {
		parts = append(parts, fmt.Sprintf("bottleneck leg %d", item.BottleneckLeg+1))
	}
	if item.RecoveryLatencyHrs > 0 {
		parts = append(parts, fmt.Sprintf("%.0fh recovery", item.RecoveryLatencyHrs))
	}
	if len(item.SpatialSemantics) > 0 {
		parts = append(parts, strings.Join(firstNStrings(item.SpatialSemantics, 2), ", "))
	}
	return strings.Join(compactUnique(parts), " • ")
}

func substrateRiskPosture(item supplyChainSubstrate) string {
	switch {
	case item.PathStressScore >= 0.75:
		return "elevated"
	case item.PathStressScore >= 0.45:
		return "watching"
	default:
		return "tracked"
	}
}

func normalizeArtifactCanonicalKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/artifact/") {
		return "/" + strings.Trim(strings.TrimPrefix(raw, "/artifact/"), "/")
	}
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	return "/" + strings.Trim(raw, "/")
}

func containsStringFold(items []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), want) {
			return true
		}
	}
	return false
}

func briefTouchesLensScope(brief *horizonmodels.WatchBrief, lens *horizonmodels.Lens) bool {
	if brief == nil || lens == nil {
		return false
	}
	scopeSet := make(map[string]struct{}, len(lens.Scope))
	for _, path := range lens.Scope {
		path = strings.TrimSpace(path)
		if !isWatchScopeTargetPath(path) {
			continue
		}
		scopeSet[path] = struct{}{}
	}
	if _, ok := scopeSet[strings.TrimSpace(brief.CanonicalPath)]; ok {
		return true
	}
	for _, target := range brief.TopAffectedTargets {
		if _, ok := scopeSet[strings.TrimSpace(target)]; ok {
			return true
		}
	}
	return false
}

func isWatchScopeTargetPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	return !horizonmodels.IsNonFragmentScopeEntry(path)
}

func deriveScenarioLocationPath(paths []string) string {
	best := ""
	bestScore := -1
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if !strings.HasPrefix(path, "/loc/") {
			continue
		}
		normalized := normalizeScenarioLocationPath(path)
		if normalized == "" {
			continue
		}
		score := scenarioLocationScore(normalized)
		if score > bestScore {
			best = normalized
			bestScore = score
		}
	}
	if best == "" {
		return "/loc/global"
	}
	return best
}

func normalizeScenarioLocationPath(path string) string {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return ""
	}
	if len(parts) >= 3 && len(parts[1]) == 2 {
		return "/" + strings.Join(parts[:2], "/")
	}
	return "/" + strings.Join(parts[:minInt(len(parts), 2)], "/")
}

func scenarioLocationScore(path string) int {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return 0
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return 0
	}
	token := parts[1]
	score := 1
	if len(token) == 2 {
		score = 3
	}
	if token == "global" {
		score = 0
	}
	return score
}

func (s *Service) watchSnapshotCacheKey(req DashboardRequest) string {
	modeKey := strings.TrimSpace(req.Mode)
	if modeKey == "" {
		modeKey = "auto"
	} else {
		modeKey = normalizeWatchMode(modeKey)
	}
	horizonKey := strings.TrimSpace(req.TimeHorizon)
	if horizonKey == "" {
		horizonKey = "auto"
	}
	return strings.Join([]string{
		"horizon:watch:snapshot",
		watchSnapshotCacheVersion,
		strings.TrimSpace(firstNonEmpty(req.TenantID, "global")),
		strings.TrimSpace(firstNonEmpty(req.UserID, "anon")),
		modeKey,
		normalizeServiceLine(req.ServiceLine),
		strings.TrimSpace(firstNonEmpty(req.LensID, "global")),
		horizonKey,
	}, ":")
}

func truncateMonitoringSnapshots(in []*MonitoringDeltaSnapshot, limit int) []*MonitoringDeltaSnapshot {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func truncateFeedEntries(in []*horizonmodels.WatchFeedEntry, limit int) []*horizonmodels.WatchFeedEntry {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func truncateMapFragments(in []MapFragmentReadModel, limit int) []MapFragmentReadModel {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Service) readSnapshotCache(ctx context.Context, key string) *DashboardSnapshot {
	if s == nil || s.redisClient == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	raw, err := s.redisClient.Get(ctx, key).Bytes()
	if err != nil || len(raw) == 0 {
		return nil
	}
	var snapshot DashboardSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil
	}
	return &snapshot
}

func (s *Service) writeSnapshotCache(ctx context.Context, key string, snapshot *DashboardSnapshot) {
	if s == nil || s.redisClient == nil || strings.TrimSpace(key) == "" || snapshot == nil {
		return
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	_ = s.redisClient.Set(ctx, key, payload, defaultWatchSnapshotTTL).Err()
}

func (s *Service) invalidateSnapshotCache(ctx context.Context, tenantID, userID string) {
	if s == nil || s.redisClient == nil {
		return
	}
	patterns := []string{
		fmt.Sprintf("horizon:watch:snapshot:%s:%s:*", watchSnapshotCacheVersion, strings.TrimSpace(firstNonEmpty(tenantID, "global"))),
	}
	if strings.TrimSpace(userID) != "" {
		patterns = append(patterns, fmt.Sprintf("horizon:watch:snapshot:%s:%s:%s:*", watchSnapshotCacheVersion, strings.TrimSpace(firstNonEmpty(tenantID, "global")), strings.TrimSpace(userID)))
	}
	for _, pattern := range patterns {
		iter := s.redisClient.Scan(ctx, 0, pattern, 200).Iterator()
		for iter.Next(ctx) {
			_ = s.redisClient.Del(ctx, iter.Val()).Err()
		}
	}
}

func toneFromValue(v float64) string {
	switch {
	case v >= 0.75:
		return "critical"
	case v >= 0.5:
		return "elevated"
	case v >= 0.25:
		return "watch"
	default:
		return "calm"
	}
}

func layerHintsForPaths(paths []string) []string {
	out := []string{}
	for _, path := range paths {
		path = strings.TrimSpace(strings.ToLower(path))
		switch {
		case strings.HasPrefix(path, "/loc/"), strings.HasPrefix(path, "/region/"):
			out = append(out, "geo")
		case strings.HasPrefix(path, "/corridor/"), strings.HasPrefix(path, "/route/"):
			out = append(out, "corridor")
		case strings.HasPrefix(path, "/market/"):
			out = append(out, "market")
		default:
			out = append(out, "objects")
		}
	}
	return compactUnique(out)
}

func firstNStrings(in []string, limit int) []string {
	if len(in) == 0 || limit <= 0 {
		return nil
	}
	in = compactUnique(in)
	if len(in) <= limit {
		return in
	}
	return append([]string(nil), in[:limit]...)
}

func filterBriefsByQuickFilters(briefs []*horizonmodels.WatchBrief, filters []string) []*horizonmodels.WatchBrief {
	if len(filters) == 0 {
		return briefs
	}
	normalized := make([]string, 0, len(filters))
	for _, filter := range filters {
		filter = strings.TrimSpace(strings.ToLower(filter))
		if filter != "" {
			normalized = append(normalized, filter)
		}
	}
	if len(normalized) == 0 {
		return briefs
	}
	out := make([]*horizonmodels.WatchBrief, 0, len(briefs))
	for _, brief := range briefs {
		if brief == nil {
			continue
		}
		haystack := strings.ToLower(strings.Join(append([]string{
			brief.Label,
			brief.Headline,
			brief.Summary,
			brief.CanonicalPath,
			brief.ScopeType,
			brief.RiskPosture,
		}, append(append([]string{}, brief.DominantThemes...), brief.TopAffectedTargets...)...), " "))
		matched := false
		for _, filter := range normalized {
			if strings.Contains(haystack, filter) {
				matched = true
				break
			}
		}
		if matched {
			out = append(out, brief)
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func asString(raw any) string {
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(raw))
	}
}

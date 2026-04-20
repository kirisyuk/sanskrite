package watch

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

const defaultWatchPageFreshnessWindow = 24 * time.Hour

type PageSectionEnricher func(context.Context, PageEnrichmentInput) (*PageEnrichmentOutput, error)

type PageEnrichmentInput struct {
	TenantID          string
	ProjectID         string
	LensID            string
	LensName          string
	TimeHorizon       string
	InterestTitle     string
	InterestQuery     string
	QueryOverrides    []string
	HeroTitle         string
	HeroSummary       string
	AnchorItems       []string
	ReachItems        []string
	ProductItems      []string
	SupplyPathItems   []string
	RiskItems         []string
	AffectedItems     []string
	DirectItems       []string
	IndirectItems     []string
	SignalItems       []string
	ScenarioItems     []string
	AnchorDetails     []horizonmodels.WatchPageScopeItem
	ReachDetails      []horizonmodels.WatchPageScopeItem
	ProductDetails    []horizonmodels.WatchPageScopeItem
	SupplyPathDetails []horizonmodels.WatchPageSupplyPath
	RiskDetails       []horizonmodels.WatchPageRiskItem
	AffectedDetails   []horizonmodels.WatchPageAffectedItem
	DirectDetails     []horizonmodels.WatchPageAffectedItem
	IndirectDetails   []horizonmodels.WatchPageAffectedItem
	SignalDetails     []horizonmodels.WatchPageSignalItem
	ScenarioDetails   []horizonmodels.WatchPageScenarioItem
	EvidenceDetails   []horizonmodels.WatchPageEvidenceItem
	Dashboards        []horizonmodels.WatchPageDashboardMetric
	MapFragments      []horizonmodels.WatchPageMapFragment
	EvidenceTitles    []string
	BackingIDs        []string
}

type PageEnrichmentOutput struct {
	Model             string
	HeroTitle         string
	HeroSummary       string
	HeroWhyNow        string
	ScopeSummary      string
	ProductSummary    string
	SupplyPathSummary string
	RiskSummary       string
	AffectedSummary   string
	DirectSummary     string
	IndirectSummary   string
	SignalSummary     string
	ScenarioSummary   string
	DashboardCaptions map[string]string
	MapCaptions       map[string]string
	GeneratedAt       time.Time
}

type lensAssemblyArtifact struct {
	Artifact *horizonmodels.MaterializedArtifact
	Parsed   *horizonmodels.SupplyChainAssemblyGraphPayload
}

type pageImpactAgg struct {
	path    string
	label   string
	reason  string
	score   float64
	backing []string
}

func normalizedWatchInterest(label, canonicalPath, interestTitle, interestQuery string, queryOverrides []string) (string, string, []string) {
	label = strings.TrimSpace(firstNonEmpty(label, labelFromCanonicalPath(canonicalPath), "Watch surface"))
	interestTitle = strings.TrimSpace(firstNonEmpty(interestTitle, label+" risk picture"))
	interestQuery = strings.TrimSpace(firstNonEmpty(interestQuery, fmt.Sprintf("%s current risks, affected entities, and geographic concentration", label)))
	merged := compactUnique(append([]string{interestQuery, interestTitle, label}, queryOverrides...))
	if strings.TrimSpace(canonicalPath) != "" && strings.TrimSpace(canonicalPath) != label {
		merged = compactUnique(append(merged, strings.TrimSpace(canonicalPath)))
	}
	return interestTitle, interestQuery, merged
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Service) GetLensPageSnapshot(ctx context.Context, tenantID, lensID string) (*horizonmodels.WatchPageSnapshot, error) {
	if s == nil || strings.TrimSpace(lensID) == "" {
		return nil, nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	lensID = strings.TrimSpace(lensID)
	var lens *horizonmodels.Lens
	if s.lensesRepo != nil {
		lens, _ = s.lensesRepo.Get(ctx, lensID)
	}
	if s.pageSnapshotsRepo == nil {
		return s.notBuiltLensPageSnapshot(tenantID, lensID, lens, ""), nil
	}
	snapshot, err := s.pageSnapshotsRepo.GetByLens(ctx, tenantID, lensID)
	if err != nil || snapshot == nil {
		return s.notBuiltLensPageSnapshot(tenantID, lensID, lens, ""), nil
	}
	if snapshot.BuildStatus == horizonmodels.WatchPageBuildStatusReady && !snapshot.StaleAfterAt.IsZero() && time.Now().UTC().After(snapshot.StaleAfterAt) {
		copy := *snapshot
		copy.BuildStatus = horizonmodels.WatchPageBuildStatusStale
		snapshot = &copy
	}
	return snapshot, nil
}

func (s *Service) RefreshLensPageSnapshot(ctx context.Context, tenantID, userID, lensID, timeHorizon string) (*horizonmodels.WatchPageSnapshot, error) {
	if s == nil || s.pageSnapshotsRepo == nil || s.lensesRepo == nil || strings.TrimSpace(lensID) == "" {
		return nil, fmt.Errorf("lens watch page refresh not configured")
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	lens, err := s.lensesRepo.Get(ctx, strings.TrimSpace(lensID))
	if err != nil || lens == nil {
		return nil, fmt.Errorf("failed to load lens: %w", err)
	}
	prev, _ := s.pageSnapshotsRepo.GetByLens(ctx, tenantID, lensID)
	base, err := s.buildDashboardSnapshot(ctx, DashboardRequest{
		TenantID:    tenantID,
		UserID:      strings.TrimSpace(userID),
		Mode:        string(horizonmodels.WatchModeCombined),
		LensID:      strings.TrimSpace(lensID),
		ServiceLine: ServiceLineGeo,
		TimeHorizon: strings.TrimSpace(firstNonEmpty(timeHorizon, "7d")),
	})
	if err != nil {
		return nil, err
	}
	snapshot := s.assembleLensPageSnapshot(ctx, lens, base, prev)
	if err := s.pageSnapshotsRepo.Upsert(ctx, snapshot); err != nil {
		return nil, err
	}
	s.appendLensPageHistory(ctx, snapshot)
	return snapshot, nil
}

func (s *Service) notBuiltLensPageSnapshot(tenantID, requestedLensID string, lens *horizonmodels.Lens, timeHorizon string) *horizonmodels.WatchPageSnapshot {
	now := time.Now().UTC()
	lensName := ""
	projectID := ""
	lensID := strings.TrimSpace(requestedLensID)
	pageFraming := map[string]any{}
	if lens != nil {
		lensName = strings.TrimSpace(lens.Name)
		projectID = strings.TrimSpace(lens.ProjectID)
		lensID = strings.TrimSpace(firstNonEmpty(lens.ID, lensID))
	}
	interestTitle, interestQuery, queryOverrides := normalizedWatchInterest(
		firstNonEmpty(lensName, "Lens watch"),
		"/lens/"+strings.TrimSpace(lensID),
		"",
		lensOriginalQuery(lens),
		lensDomainQueryOverrides(lens),
	)
	return &horizonmodels.WatchPageSnapshot{
		TenantID:       tenantID,
		LensID:         lensID,
		LensName:       lensName,
		ProjectID:      projectID,
		TimeHorizon:    strings.TrimSpace(firstNonEmpty(timeHorizon, "7d")),
		InterestTitle:  interestTitle,
		InterestQuery:  interestQuery,
		QueryOverrides: queryOverrides,
		PageFraming:    pageFraming,
		BuildStatus:    horizonmodels.WatchPageBuildStatusNotBuilt,
		FreshAt:        now,
		StaleAfterAt:   now,
		Hero: horizonmodels.WatchPageHero{
			Eyebrow:     "Lens watch surface",
			Title:       firstNonEmpty(lensName, "Watch page not built yet"),
			Summary:     "This page reads from a persisted lens watch snapshot. Use refresh to build the first teaser surface.",
			TimeHorizon: strings.TrimSpace(firstNonEmpty(timeHorizon, "7d")),
			GeneratedAt: now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (s *Service) assembleLensPageSnapshot(
	ctx context.Context,
	lens *horizonmodels.Lens,
	base *DashboardSnapshot,
	prev *horizonmodels.WatchPageSnapshot,
) *horizonmodels.WatchPageSnapshot {
	now := time.Now().UTC()
	lensID := ""
	lensName := ""
	projectID := ""
	if lens != nil {
		lensID = strings.TrimSpace(lens.ID)
		lensName = strings.TrimSpace(lens.Name)
		projectID = strings.TrimSpace(lens.ProjectID)
	}
	visibleBriefs := pickVisibleBriefsFromDashboard(base)
	primaryBrief := firstBrief(visibleBriefs)
	interestTitle, interestQuery, queryOverrides := derivePageInterest(lens, primaryBrief)
	substrate := s.collectLensSupplyChainSubstrate(ctx, lens)
	assemblies := s.collectLensAssemblyArtifacts(ctx, lens)
	graph := s.collectLensDiagnosticGraph(ctx, lens, base, substrate, assemblies)
	if base != nil && base.ScenarioWorkspace == nil {
		baseCopy := *base
		baseCopy.ScenarioWorkspace = s.buildScenarioWorkspace(
			DashboardRequest{
				TenantID:    lensTenantID(lens),
				LensID:      lensID,
				TimeHorizon: base.TimeHorizon,
			},
			pickVisibleBriefsFromDashboard(base),
			base.LensAttention,
			lens,
			substrate,
		)
		base = &baseCopy
	}
	anchors := buildWatchPageAnchors(lens, base, substrate, assemblies, now)
	reach := buildWatchPageReach(lens, base, substrate, assemblies, now)
	products := buildWatchPageProducts(assemblies, substrate, now)
	reach = mergeScopeItems(reach, buildGraphReachItems(graph, now), 12)
	products = mergeScopeItems(buildGraphProductItems(graph, now), products, 8)
	supplyPaths := buildWatchPageSupplyPaths(substrate, now)
	riskItems := buildWatchPageRiskItems(visibleBriefs, now)
	riskItems = mergeRiskItems(buildGraphRiskItems(graph, now), riskItems, 4)
	affectedItems := buildWatchPageAffectedItems(visibleBriefs, now)
	directlyAffected, indirectlyAffected := buildWatchPageImpactSets(base, assemblies, substrate, graph, affectedItems, now)
	affectedItems = mergeAffectedItems(directlyAffected, mergeAffectedItems(indirectlyAffected, affectedItems, 8), 8)
	dashboards := buildWatchPageDashboardMetrics(base.Dashboards, prev, now)
	mapFragments := mergeMapFragments(buildWatchPageMapFragments(base.MapFragments, now), buildGraphMapFragments(graph, now), 4)
	signals := buildWatchPageSignals(base, graph, now)
	scenarios := buildWatchPageScenarios(base, graph, products, supplyPaths, directlyAffected, indirectlyAffected, now)
	evidence := mergeEvidenceItems(buildWatchPageEvidenceItems(visibleBriefs, now), buildGraphEvidenceItems(graph, now), 6)
	hero := buildWatchPageHero(primaryBrief, lensName, base.TimeHorizon, interestTitle, interestQuery, now)
	hero = buildLensOverviewHero(lens, base, hero, interestTitle, interestQuery, products, supplyPaths, affectedItems, riskItems, now)
	sectionEnrichments := buildDeterministicPageSectionEnrichments(
		interestQuery,
		anchors,
		reach,
		products,
		supplyPaths,
		riskItems,
		affectedItems,
		directlyAffected,
		indirectlyAffected,
		signals,
		scenarios,
		dashboards,
		mapFragments,
		evidence,
		backingIDsFromBriefs(visibleBriefs),
		now,
	)

	if out, err := s.enrichLensPage(ctx, PageEnrichmentInput{
		TenantID:          strings.TrimSpace(firstNonEmpty(lensTenantID(lens), "global")),
		ProjectID:         projectID,
		LensID:            lensID,
		LensName:          lensName,
		TimeHorizon:       base.TimeHorizon,
		InterestTitle:     interestTitle,
		InterestQuery:     interestQuery,
		QueryOverrides:    queryOverrides,
		HeroTitle:         hero.Title,
		HeroSummary:       hero.Summary,
		AnchorItems:       labelsFromScopeItems(anchors),
		ReachItems:        labelsFromScopeItems(reach),
		ProductItems:      labelsFromScopeItems(products),
		SupplyPathItems:   titlesFromSupplyPaths(supplyPaths),
		RiskItems:         titlesFromRiskItems(riskItems),
		AffectedItems:     titlesFromAffectedItems(affectedItems),
		DirectItems:       titlesFromAffectedItems(directlyAffected),
		IndirectItems:     titlesFromAffectedItems(indirectlyAffected),
		SignalItems:       titlesFromSignals(signals),
		ScenarioItems:     titlesFromScenarios(scenarios),
		AnchorDetails:     append([]horizonmodels.WatchPageScopeItem(nil), anchors...),
		ReachDetails:      append([]horizonmodels.WatchPageScopeItem(nil), reach...),
		ProductDetails:    append([]horizonmodels.WatchPageScopeItem(nil), products...),
		SupplyPathDetails: append([]horizonmodels.WatchPageSupplyPath(nil), supplyPaths...),
		RiskDetails:       append([]horizonmodels.WatchPageRiskItem(nil), riskItems...),
		AffectedDetails:   append([]horizonmodels.WatchPageAffectedItem(nil), affectedItems...),
		DirectDetails:     append([]horizonmodels.WatchPageAffectedItem(nil), directlyAffected...),
		IndirectDetails:   append([]horizonmodels.WatchPageAffectedItem(nil), indirectlyAffected...),
		SignalDetails:     append([]horizonmodels.WatchPageSignalItem(nil), signals...),
		ScenarioDetails:   append([]horizonmodels.WatchPageScenarioItem(nil), scenarios...),
		EvidenceDetails:   append([]horizonmodels.WatchPageEvidenceItem(nil), evidence...),
		Dashboards:        dashboards,
		MapFragments:      mapFragments,
		EvidenceTitles:    titlesFromEvidenceItems(evidence),
		BackingIDs:        backingIDsFromBriefs(visibleBriefs),
	}); err == nil && out != nil {
		applyPageEnrichment(&hero, riskItems, affectedItems, directlyAffected, indirectlyAffected, products, supplyPaths, dashboards, mapFragments, signals, scenarios, sectionEnrichments, out, backingIDsFromBriefs(visibleBriefs))
	}

	buildStatus := horizonmodels.WatchPageBuildStatusReady
	if len(visibleBriefs) == 0 && len(mapFragments) == 0 && len(dashboards) == 0 && len(anchors) == 0 && len(products) == 0 && len(signals) == 0 {
		buildStatus = horizonmodels.WatchPageBuildStatusNotBuilt
	}
	return &horizonmodels.WatchPageSnapshot{
		TenantID:           strings.TrimSpace(firstNonEmpty(lensTenantID(lens), "global")),
		LensID:             lensID,
		LensName:           lensName,
		ProjectID:          projectID,
		TimeHorizon:        strings.TrimSpace(firstNonEmpty(base.TimeHorizon, "7d")),
		InterestTitle:      interestTitle,
		InterestQuery:      interestQuery,
		QueryOverrides:     queryOverrides,
		PageFraming:        derivePageFraming(lens, primaryBrief),
		BuildStatus:        buildStatus,
		FreshAt:            now,
		StaleAfterAt:       now.Add(defaultWatchPageFreshnessWindow),
		Hero:               hero,
		Anchors:            anchors,
		Reach:              reach,
		Products:           products,
		SupplyPaths:        supplyPaths,
		WhatIsAtRisk:       riskItems,
		Affected:           affectedItems,
		DirectlyAffected:   directlyAffected,
		IndirectlyAffected: indirectlyAffected,
		Dashboards:         dashboards,
		MapFragments:       mapFragments,
		Signals:            signals,
		Scenarios:          scenarios,
		Evidence:           evidence,
		SectionEnrichments: sectionEnrichments,
		Metadata: map[string]any{
			"briefCount":               len(visibleBriefs),
			"baseGeneratedAt":          base.GeneratedAt.Format(time.RFC3339),
			"mapFragmentCount":         len(mapFragments),
			"dashboardCount":           len(dashboards),
			"evidenceCount":            len(evidence),
			"affectedEntityCount":      len(affectedItems),
			"anchorCount":              len(anchors),
			"reachCount":               len(reach),
			"productCount":             len(products),
			"supplyPathCount":          len(supplyPaths),
			"directlyAffectedCount":    len(directlyAffected),
			"indirectlyAffectedCount":  len(indirectlyAffected),
			"signalCount":              len(signals),
			"scenarioCount":            len(scenarios),
			"graphSeedCount":           lensDiagnosticSeedCount(graph),
			"graphNodeCount":           lensDiagnosticNodeCount(graph),
			"graphDirectRelationCount": lensDiagnosticDirectRelationCount(graph),
			"graphPropagationCount":    lensDiagnosticPropagationCount(graph),
		},
		CreatedAt: firstSnapshotCreatedAt(prev, now),
		UpdatedAt: now,
	}
}

func firstSnapshotCreatedAt(prev *horizonmodels.WatchPageSnapshot, now time.Time) time.Time {
	if prev != nil && !prev.CreatedAt.IsZero() {
		return prev.CreatedAt
	}
	return now
}

func (s *Service) enrichLensPage(ctx context.Context, in PageEnrichmentInput) (*PageEnrichmentOutput, error) {
	if s == nil || s.pageEnricher == nil {
		return nil, nil
	}
	return s.pageEnricher(ctx, in)
}

func (s *Service) appendLensPageHistory(ctx context.Context, snapshot *horizonmodels.WatchPageSnapshot) {
	if s == nil || s.pageHistoryRepo == nil || snapshot == nil || strings.TrimSpace(snapshot.LensID) == "" {
		return
	}
	for _, metric := range snapshot.Dashboards {
		_, _ = s.pageHistoryRepo.Create(ctx, &horizonmodels.WatchPageHistoryEntry{
			TenantID:   snapshot.TenantID,
			LensID:     snapshot.LensID,
			SnapshotID: snapshot.ID,
			EntryType:  "metric",
			Key:        metric.ID,
			Payload: map[string]any{
				"title":          metric.Title,
				"value":          metric.Value,
				"deltaValue":     metric.DeltaValue,
				"deltaDirection": metric.DeltaDirection,
			},
		})
	}
	for _, item := range snapshot.Affected {
		_, _ = s.pageHistoryRepo.Create(ctx, &horizonmodels.WatchPageHistoryEntry{
			TenantID:   snapshot.TenantID,
			LensID:     snapshot.LensID,
			SnapshotID: snapshot.ID,
			EntryType:  "affected",
			Key:        strings.TrimSpace(firstNonEmpty(item.ID, item.CanonicalPath)),
			Payload: map[string]any{
				"label":         item.Label,
				"canonicalPath": item.CanonicalPath,
				"rank":          item.Rank,
			},
		})
	}
	for _, item := range snapshot.MapFragments {
		_, _ = s.pageHistoryRepo.Create(ctx, &horizonmodels.WatchPageHistoryEntry{
			TenantID:   snapshot.TenantID,
			LensID:     snapshot.LensID,
			SnapshotID: snapshot.ID,
			EntryType:  "map_fragment",
			Key:        item.ID,
			Payload: map[string]any{
				"title":          item.Title,
				"canonicalPaths": append([]string(nil), item.CanonicalPaths...),
				"layerHints":     append([]string(nil), item.LayerHints...),
			},
		})
	}
}

func lensTenantID(lens *horizonmodels.Lens) string {
	if lens == nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(lens.TenantID, "global"))
}

func pickVisibleBriefsFromDashboard(snapshot *DashboardSnapshot) []*horizonmodels.WatchBrief {
	if snapshot == nil {
		return nil
	}
	source := append(append([]*horizonmodels.WatchBrief{}, snapshot.MyWatch...), snapshot.GlobalHotSlices...)
	out := make([]*horizonmodels.WatchBrief, 0, len(source))
	seen := map[string]struct{}{}
	for _, brief := range source {
		if brief == nil {
			continue
		}
		key := strings.TrimSpace(firstNonEmpty(brief.ID, brief.TargetID, brief.CanonicalPath))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, brief)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hotness == out[j].Hotness {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Hotness > out[j].Hotness
	})
	return out
}

func firstBrief(briefs []*horizonmodels.WatchBrief) *horizonmodels.WatchBrief {
	if len(briefs) == 0 {
		return nil
	}
	return briefs[0]
}

func derivePageInterest(lens *horizonmodels.Lens, brief *horizonmodels.WatchBrief) (string, string, []string) {
	label := ""
	canonicalPath := ""
	interestTitle := ""
	interestQuery := ""
	queryOverrides := lensDomainQueryOverrides(lens)
	if lens != nil {
		label = strings.TrimSpace(firstNonEmpty(lens.Name, lens.ID))
		canonicalPath = "/lens/" + strings.TrimSpace(lens.ID)
		interestQuery = strings.TrimSpace(firstNonEmpty(lensOriginalQuery(lens), interestQuery))
	}
	if brief != nil {
		interestTitle = strings.TrimSpace(firstNonEmpty(brief.InterestTitle, interestTitle))
		interestQuery = strings.TrimSpace(firstNonEmpty(brief.InterestQuery, interestQuery))
		queryOverrides = append(queryOverrides, brief.QueryOverrides...)
	}
	return normalizedWatchInterest(label, canonicalPath, interestTitle, interestQuery, queryOverrides)
}

func buildWatchPageHero(
	brief *horizonmodels.WatchBrief,
	lensName, timeHorizon, interestTitle, interestQuery string,
	now time.Time,
) horizonmodels.WatchPageHero {
	hero := horizonmodels.WatchPageHero{
		Eyebrow:     "Lens teaser surface",
		Title:       firstNonEmpty(interestTitle, lensName, "Lens watch"),
		Summary:     firstNonEmpty(interestQuery, "A persisted risk picture will appear after refresh."),
		TimeHorizon: strings.TrimSpace(firstNonEmpty(timeHorizon, "7d")),
		GeneratedAt: now,
	}
	if brief == nil {
		hero.WhyNow = "Refresh the lens watch page to assemble current risks, affected entities, dashboards, and map fragments."
		return hero
	}
	hero.Title = firstNonEmpty(strings.TrimSpace(brief.Headline), hero.Title)
	hero.Summary = firstNonEmpty(strings.TrimSpace(brief.Summary), hero.Summary)
	hero.WhyNow = firstNonEmpty(strings.TrimSpace(brief.Explanation), "Current briefing is synthesized from watch activity, facts, and accepted proposals.")
	hero.RiskPosture = strings.TrimSpace(brief.RiskPosture)
	hero.SourceRefs = append([]string(nil), brief.TopEvidence...)
	hero.BackingIDs = compactUnique([]string{strings.TrimSpace(brief.ID), strings.TrimSpace(brief.TargetID), strings.TrimSpace(brief.SubjectID)})
	return hero
}

func buildLensOverviewHero(
	lens *horizonmodels.Lens,
	base *DashboardSnapshot,
	hero horizonmodels.WatchPageHero,
	interestTitle, interestQuery string,
	products []horizonmodels.WatchPageScopeItem,
	supplyPaths []horizonmodels.WatchPageSupplyPath,
	affected []horizonmodels.WatchPageAffectedItem,
	risks []horizonmodels.WatchPageRiskItem,
	now time.Time,
) horizonmodels.WatchPageHero {
	if lens == nil {
		return hero
	}
	hero.Title = strings.TrimSpace(firstNonEmpty(interestTitle, hero.Title, lens.Name))
	hero.Summary = strings.TrimSpace(firstNonEmpty(
		lensOverviewSummary(interestQuery, products, supplyPaths, affected),
		hero.Summary,
	))
	hero.WhyNow = strings.TrimSpace(firstNonEmpty(
		lensOverviewWhyNow(base, supplyPaths, risks),
		hero.WhyNow,
	))
	if strings.TrimSpace(hero.RiskPosture) == "" && len(risks) > 0 {
		hero.RiskPosture = strings.TrimSpace(risks[0].RiskPosture)
	}
	hero.GeneratedAt = now
	return hero
}

func lensOverviewSummary(
	interestQuery string,
	products []horizonmodels.WatchPageScopeItem,
	supplyPaths []horizonmodels.WatchPageSupplyPath,
	affected []horizonmodels.WatchPageAffectedItem,
) string {
	parts := []string{}
	if q := strings.TrimSpace(interestQuery); q != "" {
		parts = append(parts, q)
	}
	context := []string{}
	if len(supplyPaths) > 0 {
		context = append(context, fmt.Sprintf("%d materialized supply paths", len(supplyPaths)))
	}
	if len(products) > 0 {
		context = append(context, fmt.Sprintf("%d tracked products", len(products)))
	}
	if len(context) > 0 {
		parts = append(parts, "Current lens picture is grounded in "+strings.Join(context, " and "))
	}
	labels := firstScopeLabels(products, 2)
	affectedLabels := firstAffectedLabels(affected, 3)
	details := []string{}
	if len(labels) > 0 {
		details = append(details, "key product pressure around "+strings.Join(labels, ", "))
	}
	if len(affectedLabels) > 0 {
		details = append(details, "downstream exposure reaching "+strings.Join(affectedLabels, ", "))
	}
	if len(details) > 0 {
		parts = append(parts, strings.Join(details, " with "))
	}
	return strings.Join(compactUnique(parts), ". ")
}

func lensOverviewWhyNow(
	base *DashboardSnapshot,
	supplyPaths []horizonmodels.WatchPageSupplyPath,
	risks []horizonmodels.WatchPageRiskItem,
) string {
	parts := []string{}
	if len(supplyPaths) > 0 {
		parts = append(parts, "This page is assembled from lens-owned supply-chain substrate and expanded graph reach, not from global watch briefs")
	}
	if len(risks) > 0 {
		riskTitles := firstRiskTitles(risks, 2)
		if len(riskTitles) > 0 {
			parts = append(parts, "The clearest pressure points right now are "+strings.Join(riskTitles, " and "))
		}
	}
	if base != nil && base.LensAttention != nil && len(base.LensAttention.Subjects) > 0 {
		parts = append(parts, fmt.Sprintf("%d lens runtime subjects are currently carrying this scope forward", len(base.LensAttention.Subjects)))
	}
	return strings.Join(compactUnique(parts), ". ")
}

func firstScopeLabels(items []horizonmodels.WatchPageScopeItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		label := strings.TrimSpace(firstNonEmpty(item.Label, item.CanonicalPath))
		if label == "" {
			continue
		}
		if len([]rune(label)) <= 2 {
			continue
		}
		out = append(out, label)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return compactUnique(out)
}

func firstAffectedLabels(items []horizonmodels.WatchPageAffectedItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		label := strings.TrimSpace(firstNonEmpty(item.Label, item.CanonicalPath))
		if label == "" {
			continue
		}
		out = append(out, label)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return compactUnique(out)
}

func firstRiskTitles(items []horizonmodels.WatchPageRiskItem, limit int) []string {
	out := make([]string, 0, minInt(len(items), limit))
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		out = append(out, title)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return compactUnique(out)
}

func buildWatchPageRiskItems(briefs []*horizonmodels.WatchBrief, now time.Time) []horizonmodels.WatchPageRiskItem {
	if len(briefs) == 0 {
		return []horizonmodels.WatchPageRiskItem{{
			ID:          "risk:not_enough_signal",
			Title:       "Not enough signal yet",
			Summary:     "The lens does not have a persisted high-signal picture yet. Refresh after more watch activity or broaden the watched interest.",
			GeneratedAt: now,
		}}
	}
	out := make([]horizonmodels.WatchPageRiskItem, 0, minInt(len(briefs), 4))
	for _, brief := range briefs {
		if brief == nil {
			continue
		}
		out = append(out, horizonmodels.WatchPageRiskItem{
			ID:            strings.TrimSpace(brief.ID),
			Title:         firstNonEmpty(strings.TrimSpace(brief.Headline), strings.TrimSpace(brief.Label)),
			Summary:       firstNonEmpty(strings.TrimSpace(brief.Explanation), strings.TrimSpace(brief.Summary)),
			RiskPosture:   strings.TrimSpace(brief.RiskPosture),
			CanonicalPath: strings.TrimSpace(brief.CanonicalPath),
			Mechanisms:    compactUnique(append([]string{}, brief.DominantThemes...)),
			SourceRefs:    append([]string(nil), brief.TopEvidence...),
			BackingIDs:    compactUnique([]string{strings.TrimSpace(brief.ID), strings.TrimSpace(brief.TargetID), strings.TrimSpace(brief.SubjectID)}),
			GeneratedAt:   now,
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func buildWatchPageAffectedItems(briefs []*horizonmodels.WatchBrief, now time.Time) []horizonmodels.WatchPageAffectedItem {
	type agg struct {
		label      string
		path       string
		reason     string
		score      float64
		sourceRefs []string
		backingIDs []string
	}
	index := map[string]*agg{}
	for _, brief := range briefs {
		if brief == nil {
			continue
		}
		for _, path := range compactUnique(append([]string{brief.CanonicalPath}, brief.TopAffectedTargets...)) {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			entry := index[path]
			if entry == nil {
				entry = &agg{
					label: labelFromCanonicalPath(path),
					path:  path,
				}
				index[path] = entry
			}
			entry.score += maxFloat(0.2, brief.Hotness) + maxFloat(0.1, brief.Confidence/2)
			entry.reason = firstNonEmpty(entry.reason, brief.Headline, brief.Summary)
			entry.sourceRefs = compactUnique(append(entry.sourceRefs, brief.TopEvidence...))
			entry.backingIDs = compactUnique(append(entry.backingIDs, brief.ID, brief.TargetID, brief.SubjectID))
		}
	}
	type ranked struct {
		key string
		agg *agg
	}
	items := make([]ranked, 0, len(index))
	for key, item := range index {
		items = append(items, ranked{key: key, agg: item})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].agg.score == items[j].agg.score {
			return items[i].agg.label < items[j].agg.label
		}
		return items[i].agg.score > items[j].agg.score
	})
	if len(items) == 0 {
		return []horizonmodels.WatchPageAffectedItem{{
			ID:            "affected:not_enough_signal",
			Label:         "Affected entities will appear after the first build",
			CanonicalPath: "/lens/placeholder",
			Reason:        "The page needs at least one persisted watch brief with affected targets.",
			Rank:          1,
			GeneratedAt:   now,
		}}
	}
	out := make([]horizonmodels.WatchPageAffectedItem, 0, minInt(len(items), 6))
	for i, item := range items {
		out = append(out, horizonmodels.WatchPageAffectedItem{
			ID:            item.key,
			Label:         item.agg.label,
			CanonicalPath: item.agg.path,
			Reason:        item.agg.reason,
			Rank:          i + 1,
			SourceRefs:    append([]string(nil), item.agg.sourceRefs...),
			BackingIDs:    append([]string(nil), item.agg.backingIDs...),
			GeneratedAt:   now,
		})
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func buildWatchPageDashboardMetrics(
	in []DashboardWidget,
	prev *horizonmodels.WatchPageSnapshot,
	now time.Time,
) []horizonmodels.WatchPageDashboardMetric {
	prevByID := map[string]horizonmodels.WatchPageDashboardMetric{}
	if prev != nil {
		for _, item := range prev.Dashboards {
			prevByID[strings.TrimSpace(item.ID)] = item
		}
	}
	out := make([]horizonmodels.WatchPageDashboardMetric, 0, len(in))
	for _, widget := range in {
		item := horizonmodels.WatchPageDashboardMetric{
			ID:          strings.TrimSpace(widget.ID),
			Title:       strings.TrimSpace(widget.Title),
			Value:       strings.TrimSpace(widget.Value),
			Subtitle:    strings.TrimSpace(widget.Subtitle),
			Tone:        strings.TrimSpace(widget.Tone),
			GeneratedAt: now,
		}
		if prevItem, ok := prevByID[item.ID]; ok {
			current, currentOK := metricNumericValue(item.Value)
			prevValue, prevOK := metricNumericValue(prevItem.Value)
			if currentOK && prevOK {
				diff := current - prevValue
				if diff > 0 {
					item.DeltaDirection = "up"
					item.DeltaValue = metricDeltaLabel(diff)
				} else if diff < 0 {
					item.DeltaDirection = "down"
					item.DeltaValue = metricDeltaLabel(diff)
				} else {
					item.DeltaDirection = "flat"
					item.DeltaValue = "0"
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func buildWatchPageMapFragments(in []MapFragmentReadModel, now time.Time) []horizonmodels.WatchPageMapFragment {
	out := make([]horizonmodels.WatchPageMapFragment, 0, minInt(len(in), 4))
	for _, item := range in {
		out = append(out, horizonmodels.WatchPageMapFragment{
			ID:             strings.TrimSpace(item.ID),
			Title:          strings.TrimSpace(item.Title),
			Caption:        strings.TrimSpace(item.Caption),
			CanonicalPaths: append([]string(nil), item.CanonicalPaths...),
			LayerHints:     append([]string(nil), item.LayerHints...),
			ScopeType:      strings.TrimSpace(item.ScopeType),
			BriefID:        strings.TrimSpace(item.BriefID),
			BackingIDs:     compactUnique([]string{strings.TrimSpace(item.BriefID)}),
			GeneratedAt:    now,
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func buildWatchPageEvidenceItems(briefs []*horizonmodels.WatchBrief, now time.Time) []horizonmodels.WatchPageEvidenceItem {
	out := make([]horizonmodels.WatchPageEvidenceItem, 0, minInt(len(briefs), 5))
	for _, brief := range briefs {
		if brief == nil {
			continue
		}
		out = append(out, horizonmodels.WatchPageEvidenceItem{
			ID:             strings.TrimSpace(brief.ID),
			Kind:           "brief",
			Title:          firstNonEmpty(strings.TrimSpace(brief.Headline), strings.TrimSpace(brief.Label)),
			Summary:        firstNonEmpty(strings.TrimSpace(brief.Explanation), strings.TrimSpace(brief.Summary)),
			CanonicalPaths: compactUnique(append([]string{brief.CanonicalPath}, brief.TopAffectedTargets...)),
			BriefID:        strings.TrimSpace(brief.ID),
			SourceRefs:     append([]string(nil), brief.TopEvidence...),
			GeneratedAt:    now,
		})
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func (s *Service) collectLensAssemblyArtifacts(ctx context.Context, lens *horizonmodels.Lens) []lensAssemblyArtifact {
	if s == nil || s.materializedArtifactsRepo == nil || lens == nil || strings.TrimSpace(lens.ID) == "" {
		return nil
	}
	artifacts, err := s.materializedArtifactsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 64)
	if err != nil {
		return nil
	}
	out := make([]lensAssemblyArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainAssemblyGraph {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		out = append(out, lensAssemblyArtifact{
			Artifact: artifact,
			Parsed:   parsed,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Artifact == nil || out[j].Artifact == nil {
			return i < j
		}
		return out[i].Artifact.UpdatedAt.After(out[j].Artifact.UpdatedAt)
	})
	return out
}

func buildWatchPageAnchors(
	lens *horizonmodels.Lens,
	base *DashboardSnapshot,
	substrate []supplyChainSubstrate,
	assemblies []lensAssemblyArtifact,
	now time.Time,
) []horizonmodels.WatchPageScopeItem {
	type item struct {
		path    string
		label   string
		kind    string
		summary string
		backing []string
	}
	ordered := []item{}
	seen := map[string]struct{}{}
	add := func(path, label, kind, summary string, backing ...string) {
		path = strings.TrimSpace(path)
		label = strings.TrimSpace(firstNonEmpty(label, labelFromCanonicalPath(path)))
		if path == "" || label == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		ordered = append(ordered, item{path: path, label: label, kind: strings.TrimSpace(kind), summary: strings.TrimSpace(summary), backing: compactUnique(backing)})
	}
	if lens != nil {
		for _, path := range lens.Scope {
			if isWatchScopeTargetPath(path) {
				add(path, "", "lens_scope", "Anchor included directly in the lens scope.", lens.ID)
			}
		}
	}
	if base != nil && base.LensAttention != nil {
		for _, subject := range base.LensAttention.Subjects {
			if subject == nil {
				continue
			}
			add(subject.AnchorCanonicalPath, subject.DisplayLabel, subject.SubjectKind, "Tracked as a watch subject anchor for this lens.", subject.SubjectID)
		}
	}
	for _, item := range substrate {
		for _, path := range []string{item.AnchorSupplier, item.AnchorCustomer} {
			add(path, "", "supply_path_anchor", "Anchor endpoint for a materialized supply path inside the lens.", item.CanonicalKey)
		}
	}
	for _, assembly := range assemblies {
		for _, path := range append(append([]string{}, assembly.Parsed.AnchorSet...), assembly.Parsed.ResolvedAnchors...) {
			add(path, "", "assembly_anchor", "Resolved assembly anchor contributing to the downstream impact graph.", assembly.Artifact.ID)
		}
	}
	out := make([]horizonmodels.WatchPageScopeItem, 0, minInt(len(ordered), 8))
	for _, current := range ordered {
		out = append(out, horizonmodels.WatchPageScopeItem{
			ID:            current.kind + ":" + strings.Trim(strings.TrimPrefix(current.path, "/"), "/"),
			Label:         current.label,
			CanonicalPath: current.path,
			Kind:          current.kind,
			Summary:       current.summary,
			BackingIDs:    current.backing,
			GeneratedAt:   now,
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func buildWatchPageReach(
	lens *horizonmodels.Lens,
	base *DashboardSnapshot,
	substrate []supplyChainSubstrate,
	assemblies []lensAssemblyArtifact,
	now time.Time,
) []horizonmodels.WatchPageScopeItem {
	type item struct {
		path    string
		label   string
		kind    string
		summary string
		backing []string
	}
	ordered := []item{}
	seen := map[string]struct{}{}
	add := func(path, label, kind, summary string, backing ...string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		ordered = append(ordered, item{
			path:    path,
			label:   strings.TrimSpace(firstNonEmpty(label, labelFromCanonicalPath(path))),
			kind:    strings.TrimSpace(kind),
			summary: strings.TrimSpace(summary),
			backing: compactUnique(backing),
		})
	}
	if base != nil && base.LensAttention != nil {
		for _, subject := range base.LensAttention.Subjects {
			if subject == nil {
				continue
			}
			for _, path := range compactUnique(subject.UniversePaths) {
				add(path, "", "subject_universe", "Reachable through the current watch subject universe.", subject.SubjectID)
			}
		}
	}
	for _, item := range substrate {
		for _, path := range substrateCanonicalPaths(item) {
			add(path, "", "supply_path_reach", "Reached through a materialized supply path in the lens.", item.CanonicalKey)
		}
	}
	for _, assembly := range assemblies {
		for _, node := range append(append([]horizonmodels.SupplyChainAssemblyNode{}, assembly.Parsed.ActorNodes...), assembly.Parsed.ProductNodes...) {
			kind := "assembly_actor"
			if node.Class == ah.FragmentClassProduct || strings.HasPrefix(strings.TrimSpace(node.Ref), "/product/") {
				kind = "assembly_product"
			}
			add(node.Ref, node.Name, kind, "Resolved in the supply-chain assembly graph attached to this lens.", assembly.Artifact.ID)
		}
	}
	out := make([]horizonmodels.WatchPageScopeItem, 0, minInt(len(ordered), 12))
	for _, current := range ordered {
		out = append(out, horizonmodels.WatchPageScopeItem{
			ID:            current.kind + ":" + strings.Trim(strings.TrimPrefix(current.path, "/"), "/"),
			Label:         current.label,
			CanonicalPath: current.path,
			Kind:          current.kind,
			Summary:       current.summary,
			BackingIDs:    current.backing,
			GeneratedAt:   now,
		})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func buildWatchPageProducts(
	assemblies []lensAssemblyArtifact,
	substrate []supplyChainSubstrate,
	now time.Time,
) []horizonmodels.WatchPageScopeItem {
	type agg struct {
		path            string
		label           string
		consumers       []string
		transformedInto []string
		transformedFrom []string
		backing         []string
		score           float64
	}
	products := map[string]*agg{}
	ensure := func(path, label string) *agg {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil
		}
		entry := products[path]
		if entry == nil {
			entry = &agg{
				path:  path,
				label: strings.TrimSpace(firstNonEmpty(label, labelFromCanonicalPath(path))),
			}
			products[path] = entry
		}
		if strings.TrimSpace(label) != "" {
			entry.label = strings.TrimSpace(label)
		}
		return entry
	}
	for _, assembly := range assemblies {
		actorLabels := map[string]string{}
		productLabels := map[string]string{}
		for _, node := range assembly.Parsed.ActorNodes {
			actorLabels[strings.TrimSpace(node.Ref)] = strings.TrimSpace(firstNonEmpty(node.Name, labelFromCanonicalPath(node.Ref)))
		}
		for _, node := range assembly.Parsed.ProductNodes {
			ref := strings.TrimSpace(node.Ref)
			productLabels[ref] = strings.TrimSpace(firstNonEmpty(node.Name, labelFromCanonicalPath(node.Ref)))
			if entry := ensure(ref, node.Name); entry != nil {
				entry.backing = compactUnique(append(entry.backing, assembly.Artifact.ID))
				entry.score += maxFloat(0.4, node.DecisionValueScore)
			}
		}
		for _, edge := range assembly.Parsed.SupplyEdges {
			entry := ensure(edge.ProductRef, productLabels[strings.TrimSpace(edge.ProductRef)])
			if entry == nil {
				continue
			}
			if consumer := strings.TrimSpace(actorLabels[strings.TrimSpace(edge.CustomerRef)]); consumer != "" {
				entry.consumers = compactUnique(append(entry.consumers, consumer))
			}
			entry.backing = compactUnique(append(entry.backing, assembly.Artifact.ID))
			entry.score += maxFloat(0.5, edge.RouteCriticality) + maxFloat(0.2, edge.Confidence/2)
		}
		for _, an := range assembly.Parsed.ActorNodes {
			for _, st := range an.SiteTransforms {
				for _, inRef := range st.InputProducts {
					for _, outRef := range st.OutputProducts {
						from := ensure(inRef, productLabels[strings.TrimSpace(inRef)])
						to := ensure(outRef, productLabels[strings.TrimSpace(outRef)])
						if from != nil && to != nil {
							from.transformedInto = compactUnique(append(from.transformedInto, to.label))
							to.transformedFrom = compactUnique(append(to.transformedFrom, from.label))
							from.backing = compactUnique(append(from.backing, assembly.Artifact.ID))
							to.backing = compactUnique(append(to.backing, assembly.Artifact.ID))
							from.score += maxFloat(0.35, st.Confidence)
							to.score += maxFloat(0.25, st.Confidence/2)
						}
					}
				}
			}
		}
		for _, pn := range assembly.Parsed.ProductNodes {
			for _, st := range pn.SiteTransforms {
				for _, inRef := range st.InputProducts {
					for _, outRef := range st.OutputProducts {
						from := ensure(inRef, productLabels[strings.TrimSpace(inRef)])
						to := ensure(outRef, productLabels[strings.TrimSpace(outRef)])
						if from != nil && to != nil {
							from.transformedInto = compactUnique(append(from.transformedInto, to.label))
							to.transformedFrom = compactUnique(append(to.transformedFrom, from.label))
							from.backing = compactUnique(append(from.backing, assembly.Artifact.ID))
							to.backing = compactUnique(append(to.backing, assembly.Artifact.ID))
							from.score += maxFloat(0.35, st.Confidence)
							to.score += maxFloat(0.25, st.Confidence/2)
						}
					}
				}
			}
		}
	}
	for _, item := range substrate {
		path := strings.TrimSpace(item.ProductAxis)
		if path == "" {
			continue
		}
		if entry := ensure(path, labelFromCanonicalPath(path)); entry != nil {
			entry.backing = compactUnique(append(entry.backing, item.CanonicalKey))
			entry.score += maxFloat(1.0, item.PathStressScore*3) + maxFloat(0.4, item.BindingConfidence)
		}
	}
	keys := make([]string, 0, len(products))
	for key := range products {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left := products[keys[i]]
		right := products[keys[j]]
		if left.score != right.score {
			return left.score > right.score
		}
		if len(left.consumers) == len(right.consumers) {
			if len(left.transformedInto) == len(right.transformedInto) {
				return left.label < right.label
			}
			return len(left.transformedInto) > len(right.transformedInto)
		}
		return len(left.consumers) > len(right.consumers)
	})
	out := make([]horizonmodels.WatchPageScopeItem, 0, minInt(len(keys), 8))
	for _, key := range keys {
		entry := products[key]
		parts := []string{}
		if len(entry.consumers) > 0 {
			parts = append(parts, "Consumed by "+strings.Join(firstNStrings(entry.consumers, 3), ", "))
		}
		if len(entry.transformedInto) > 0 {
			parts = append(parts, "Transforms into "+strings.Join(firstNStrings(entry.transformedInto, 2), ", "))
		}
		if len(entry.transformedFrom) > 0 {
			parts = append(parts, "Derived from "+strings.Join(firstNStrings(entry.transformedFrom, 2), ", "))
		}
		out = append(out, horizonmodels.WatchPageScopeItem{
			ID:            "product:" + strings.Trim(strings.TrimPrefix(entry.path, "/"), "/"),
			Label:         entry.label,
			CanonicalPath: entry.path,
			Kind:          "product",
			Summary:       strings.Join(parts, " • "),
			BackingIDs:    compactUnique(entry.backing),
			GeneratedAt:   now,
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func buildWatchPageSupplyPaths(substrate []supplyChainSubstrate, now time.Time) []horizonmodels.WatchPageSupplyPath {
	out := make([]horizonmodels.WatchPageSupplyPath, 0, minInt(len(substrate), 6))
	for _, item := range substrate {
		out = append(out, horizonmodels.WatchPageSupplyPath{
			ID:                strings.TrimSpace(item.CanonicalKey),
			Title:             firstNonEmpty(strings.TrimSpace(item.DisplayName), substrateCaption(item), "Supply path"),
			Summary:           substrateCaption(item),
			ProductAxis:       strings.TrimSpace(item.ProductAxis),
			AnchorSupplier:    strings.TrimSpace(item.AnchorSupplier),
			AnchorCustomer:    strings.TrimSpace(item.AnchorCustomer),
			StressScore:       item.PathStressScore,
			BindingConfidence: item.BindingConfidence,
			RouteRefs:         append([]string(nil), item.FlowRefs...),
			AffectedZoneRefs:  append([]string(nil), item.AffectedZoneRefs...),
			BackingIDs:        compactUnique([]string{strings.TrimSpace(item.CanonicalKey)}),
			GeneratedAt:       now,
		})
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func buildWatchPageImpactSets(
	base *DashboardSnapshot,
	assemblies []lensAssemblyArtifact,
	substrate []supplyChainSubstrate,
	graph *lensDiagnosticGraph,
	fallback []horizonmodels.WatchPageAffectedItem,
	now time.Time,
) ([]horizonmodels.WatchPageAffectedItem, []horizonmodels.WatchPageAffectedItem) {
	direct := map[string]*pageImpactAgg{}
	indirect := map[string]*pageImpactAgg{}
	add := func(bucket map[string]*pageImpactAgg, path, label, reason string, score float64, backing ...string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		entry := bucket[path]
		if entry == nil {
			entry = &pageImpactAgg{
				path:  path,
				label: strings.TrimSpace(firstNonEmpty(label, labelFromCanonicalPath(path))),
			}
			bucket[path] = entry
		}
		entry.score += score
		entry.reason = firstNonEmpty(entry.reason, strings.TrimSpace(reason))
		entry.backing = compactUnique(append(entry.backing, backing...))
	}
	useAssemblyActors := len(substrate) == 0
	for _, assembly := range assemblies {
		if useAssemblyActors {
			for _, node := range assembly.Parsed.ActorNodes {
				path := strings.TrimSpace(node.Ref)
				if path == "" {
					continue
				}
				reason := firstNonEmpty(strings.TrimSpace(node.Role), strings.TrimSpace(node.BranchClass), "Assembly actor")
				switch {
				case node.IsPrimaryFocus || strings.EqualFold(strings.TrimSpace(node.PriorityTier), "primary") || node.AnchorDistance <= 1:
					add(direct, path, node.Name, reason, 2.0, assembly.Artifact.ID)
				case node.IsTerminalDistributionNode || node.AnchorDistance >= 2 || strings.EqualFold(strings.TrimSpace(node.PriorityTier), "secondary") || strings.EqualFold(strings.TrimSpace(node.BranchClass), "support"):
					add(indirect, path, node.Name, reason, 1.0, assembly.Artifact.ID)
				}
			}
		}
		for _, path := range assembly.Parsed.TopDistributionNodes {
			add(indirect, path, "", "Downstream distribution node in the assembly graph.", 1.2, assembly.Artifact.ID)
		}
		for _, path := range assembly.Parsed.TopSuppliers {
			add(indirect, path, "", "Primary supplier surfaced by the assembly graph.", 0.9, assembly.Artifact.ID)
		}
	}
	for _, item := range substrate {
		if item.AnchorCustomer != "" {
			add(direct, item.AnchorCustomer, "", "Immediate receiving or processing node on a stressed supply path.", maxFloat(2.4, item.PathStressScore*3), item.CanonicalKey)
		}
		if item.ProductAxis != "" {
			add(direct, item.ProductAxis, "", "Critical product carried on the highest-stress supply path.", maxFloat(2.1, item.PathStressScore*2.4), item.CanonicalKey)
		}
		if item.AnchorSupplier != "" {
			add(indirect, item.AnchorSupplier, "", "Upstream supplier whose disruption propagates into the lens.", maxFloat(0.9, item.PathStressScore*1.2), item.CanonicalKey)
		}
	}
	if len(direct) == 0 {
		for i, item := range fallback {
			if i >= 3 {
				break
			}
			add(direct, item.CanonicalPath, item.Label, item.Reason, float64(3-i), item.BackingIDs...)
		}
	}
	if len(indirect) == 0 {
		for i, item := range fallback {
			if i < 3 {
				continue
			}
			add(indirect, item.CanonicalPath, item.Label, item.Reason, float64(len(fallback)-i), item.BackingIDs...)
		}
	}
	return rankedAffectedItems(direct, now, 6), rankedAffectedItems(indirect, now, 6)
}

func rankedAffectedItems(source map[string]*pageImpactAgg, now time.Time, limit int) []horizonmodels.WatchPageAffectedItem {
	type ranked struct {
		key string
		agg *pageImpactAgg
	}
	items := make([]ranked, 0, len(source))
	for key, item := range source {
		items = append(items, ranked{key: key, agg: item})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].agg.score == items[j].agg.score {
			leftPriority := canonicalPathImpactPriority(items[i].agg.path)
			rightPriority := canonicalPathImpactPriority(items[j].agg.path)
			if leftPriority != rightPriority {
				return leftPriority > rightPriority
			}
			return items[i].agg.label < items[j].agg.label
		}
		return items[i].agg.score > items[j].agg.score
	})
	out := make([]horizonmodels.WatchPageAffectedItem, 0, minInt(len(items), limit))
	for i, item := range items {
		out = append(out, horizonmodels.WatchPageAffectedItem{
			ID:            item.key,
			Label:         item.agg.label,
			CanonicalPath: item.agg.path,
			Reason:        item.agg.reason,
			Rank:          i + 1,
			BackingIDs:    compactUnique(item.agg.backing),
			GeneratedAt:   now,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func canonicalPathImpactPriority(path string) int {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(path, "/org/"), strings.HasPrefix(path, "/fac/"), strings.HasPrefix(path, "/gov/"):
		return 5
	case strings.HasPrefix(path, "/product/"):
		return 4
	case strings.HasPrefix(path, "/route/"), strings.HasPrefix(path, "/flow/"):
		return 3
	case strings.HasPrefix(path, "/zone/"), strings.HasPrefix(path, "/market/"):
		return 2
	case strings.HasPrefix(path, "/geo/"), strings.HasPrefix(path, "/loc/"):
		return 1
	default:
		return 0
	}
}

func buildWatchPageSignals(base *DashboardSnapshot, graph *lensDiagnosticGraph, now time.Time) []horizonmodels.WatchPageSignalItem {
	type signal struct {
		id         string
		kind       string
		title      string
		summary    string
		paths      []string
		source     string
		sourceKind string
		backing    []string
		ts         time.Time
	}
	items := []signal{}
	if base != nil && base.LensAttention != nil {
		for _, entry := range base.LensAttention.Feed {
			if entry == nil {
				continue
			}
			items = append(items, signal{
				id:         strings.TrimSpace(firstNonEmpty(entry.ProposalID, entry.SubjectID+":"+entry.Kind+":"+entry.CreatedAt.Format(time.RFC3339Nano))),
				kind:       strings.TrimSpace(firstNonEmpty(entry.Kind, "signal")),
				title:      strings.TrimSpace(entry.Title),
				summary:    strings.TrimSpace(entry.Summary),
				paths:      compactUnique(append([]string(nil), entry.CanonicalPaths...)),
				source:     "Lens attention",
				sourceKind: strings.TrimSpace(firstNonEmpty(asString(entry.Metadata["triggerType"]), "watchruntime")),
				backing:    compactUnique([]string{strings.TrimSpace(entry.SubjectID), strings.TrimSpace(entry.ProposalID)}),
				ts:         entry.CreatedAt,
			})
		}
	}
	if base != nil {
		for _, item := range base.Monitoring {
			if item == nil {
				continue
			}
			items = append(items, signal{
				id:         strings.TrimSpace(firstNonEmpty(item.ID, item.CanonicalPath, item.Headline)),
				kind:       "monitoring",
				title:      strings.TrimSpace(firstNonEmpty(item.Headline, item.Label)),
				summary:    strings.TrimSpace(item.Summary),
				paths:      compactUnique(append([]string{item.CanonicalPath}, item.TopAffectedTargets...)),
				source:     "Lens monitoring",
				sourceKind: strings.TrimSpace(firstNonEmpty(item.Status, item.RiskPosture, "monitoring")),
				backing:    compactUnique([]string{strings.TrimSpace(item.ID), strings.TrimSpace(item.CanonicalPath)}),
				ts:         item.UpdatedAt,
			})
		}
		for _, entry := range base.Feed {
			if entry == nil {
				continue
			}
			items = append(items, signal{
				id:         strings.TrimSpace(firstNonEmpty(entry.ID, entry.TargetID+":"+entry.EntryType+":"+entry.CreatedAt.Format(time.RFC3339Nano))),
				kind:       strings.TrimSpace(firstNonEmpty(entry.EntryType, "feed")),
				title:      strings.TrimSpace(entry.Title),
				summary:    strings.TrimSpace(entry.Summary),
				paths:      compactUnique(append([]string(nil), entry.CanonicalPaths...)),
				source:     "Watch feed",
				sourceKind: strings.TrimSpace(firstNonEmpty(entry.EntryType, "feed")),
				backing:    compactUnique([]string{strings.TrimSpace(entry.TargetID), strings.TrimSpace(entry.BriefID), strings.TrimSpace(entry.ProposalID), strings.TrimSpace(entry.DecisionID), strings.TrimSpace(entry.RiskJobID)}),
				ts:         entry.CreatedAt,
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ts.After(items[j].ts) })
	seen := map[string]struct{}{}
	out := make([]horizonmodels.WatchPageSignalItem, 0, minInt(len(items), 8))
	for _, item := range items {
		key := strings.TrimSpace(firstNonEmpty(item.id, item.title))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, horizonmodels.WatchPageSignalItem{
			ID:             key,
			Kind:           item.kind,
			Title:          item.title,
			Summary:        item.summary,
			CanonicalPaths: item.paths,
			SourceLabel:    item.source,
			SourceKind:     item.sourceKind,
			BackingIDs:     compactUnique(item.backing),
			GeneratedAt:    now,
		})
		if len(out) >= 8 {
			break
		}
	}
	return mergeSignals(out, buildGraphSignals(graph, now), 8)
}

func buildWatchPageScenarios(
	base *DashboardSnapshot,
	graph *lensDiagnosticGraph,
	products []horizonmodels.WatchPageScopeItem,
	supplyPaths []horizonmodels.WatchPageSupplyPath,
	direct []horizonmodels.WatchPageAffectedItem,
	indirect []horizonmodels.WatchPageAffectedItem,
	now time.Time,
) []horizonmodels.WatchPageScenarioItem {
	if base == nil || base.ScenarioWorkspace == nil {
		return buildGraphScenarios(graph, nil, products, supplyPaths, direct, indirect, now)
	}
	out := make([]horizonmodels.WatchPageScenarioItem, 0, minInt(len(base.ScenarioWorkspace.Contracts), 4))
	for _, contract := range base.ScenarioWorkspace.Contracts {
		out = append(out, horizonmodels.WatchPageScenarioItem{
			ID:                strings.TrimSpace(contract.ID),
			Title:             strings.TrimSpace(contract.Title),
			Description:       strings.TrimSpace(contract.Description),
			TargetPaths:       append([]string(nil), contract.TargetPaths...),
			TriggerConditions: append([]string(nil), contract.TriggerConditions...),
			Disconfirmers:     append([]string(nil), contract.Disconfirmers...),
			BackingIDs:        compactUnique([]string{strings.TrimSpace(contract.ID)}),
			GeneratedAt:       now,
		})
		if len(out) >= 4 {
			break
		}
	}
	return buildGraphScenarios(graph, out, products, supplyPaths, direct, indirect, now)
}

func labelsFromScopeItems(in []horizonmodels.WatchPageScopeItem) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, firstNonEmpty(item.Label, item.CanonicalPath))
	}
	return compactUnique(out)
}

func titlesFromSupplyPaths(in []horizonmodels.WatchPageSupplyPath) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, firstNonEmpty(item.Title, item.ProductAxis, item.AnchorCustomer))
	}
	return compactUnique(out)
}

func titlesFromRiskItems(in []horizonmodels.WatchPageRiskItem) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, firstNonEmpty(item.Title, item.Summary))
	}
	return compactUnique(out)
}

func titlesFromAffectedItems(in []horizonmodels.WatchPageAffectedItem) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, firstNonEmpty(item.Label, item.CanonicalPath))
	}
	return compactUnique(out)
}

func titlesFromEvidenceItems(in []horizonmodels.WatchPageEvidenceItem) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, firstNonEmpty(item.Title, item.Summary))
	}
	return compactUnique(out)
}

func titlesFromSignals(in []horizonmodels.WatchPageSignalItem) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, firstNonEmpty(item.Title, item.Summary))
	}
	return compactUnique(out)
}

func titlesFromScenarios(in []horizonmodels.WatchPageScenarioItem) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, firstNonEmpty(item.Title, item.Description))
	}
	return compactUnique(out)
}

func backingIDsFromBriefs(briefs []*horizonmodels.WatchBrief) []string {
	out := make([]string, 0, len(briefs)*3)
	for _, brief := range briefs {
		if brief == nil {
			continue
		}
		out = append(out, brief.ID, brief.TargetID, brief.SubjectID)
	}
	return compactUnique(out)
}

func applyPageEnrichment(
	hero *horizonmodels.WatchPageHero,
	riskItems []horizonmodels.WatchPageRiskItem,
	affected []horizonmodels.WatchPageAffectedItem,
	direct []horizonmodels.WatchPageAffectedItem,
	indirect []horizonmodels.WatchPageAffectedItem,
	products []horizonmodels.WatchPageScopeItem,
	supplyPaths []horizonmodels.WatchPageSupplyPath,
	dashboards []horizonmodels.WatchPageDashboardMetric,
	mapFragments []horizonmodels.WatchPageMapFragment,
	signals []horizonmodels.WatchPageSignalItem,
	scenarios []horizonmodels.WatchPageScenarioItem,
	sectionEnrichments map[string]horizonmodels.WatchPageSectionEnrichment,
	out *PageEnrichmentOutput,
	backingIDs []string,
) {
	if hero == nil || out == nil {
		return
	}
	generatedAt := out.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(out.HeroTitle) != "" {
		hero.Title = strings.TrimSpace(out.HeroTitle)
	}
	if strings.TrimSpace(out.HeroSummary) != "" {
		hero.Summary = strings.TrimSpace(out.HeroSummary)
	}
	if strings.TrimSpace(out.HeroWhyNow) != "" {
		hero.WhyNow = strings.TrimSpace(out.HeroWhyNow)
	}
	hero.GeneratedAt = generatedAt
	sectionEnrichments["hero"] = horizonmodels.WatchPageSectionEnrichment{
		Key:         "hero",
		Content:     strings.TrimSpace(firstNonEmpty(out.HeroSummary, hero.Summary)),
		BackingIDs:  compactUnique(backingIDs),
		Model:       strings.TrimSpace(out.Model),
		GeneratedAt: generatedAt,
	}
	if strings.TrimSpace(out.ScopeSummary) != "" {
		sectionEnrichments["scope"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "scope",
			Content:     strings.TrimSpace(out.ScopeSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
	}
	if strings.TrimSpace(out.ProductSummary) != "" {
		sectionEnrichments["products"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "products",
			Content:     strings.TrimSpace(out.ProductSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
		if len(products) > 0 && strings.TrimSpace(products[0].Summary) == "" {
			products[0].Summary = strings.TrimSpace(out.ProductSummary)
		}
	}
	if strings.TrimSpace(out.SupplyPathSummary) != "" {
		sectionEnrichments["supply_paths"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "supply_paths",
			Content:     strings.TrimSpace(out.SupplyPathSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
		if len(supplyPaths) > 0 && strings.TrimSpace(supplyPaths[0].Summary) == "" {
			supplyPaths[0].Summary = strings.TrimSpace(out.SupplyPathSummary)
		}
	}
	if strings.TrimSpace(out.RiskSummary) != "" {
		sectionEnrichments["what_is_at_risk"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "what_is_at_risk",
			Content:     strings.TrimSpace(out.RiskSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
		if len(riskItems) > 0 && strings.TrimSpace(riskItems[0].Summary) == "" {
			riskItems[0].Summary = strings.TrimSpace(out.RiskSummary)
		}
	}
	if strings.TrimSpace(out.AffectedSummary) != "" {
		sectionEnrichments["affected"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "affected",
			Content:     strings.TrimSpace(out.AffectedSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
		if len(affected) > 0 && strings.TrimSpace(affected[0].Reason) == "" {
			affected[0].Reason = strings.TrimSpace(out.AffectedSummary)
		}
	}
	if strings.TrimSpace(out.DirectSummary) != "" {
		sectionEnrichments["direct_effects"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "direct_effects",
			Content:     strings.TrimSpace(out.DirectSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
		if len(direct) > 0 && strings.TrimSpace(direct[0].Reason) == "" {
			direct[0].Reason = strings.TrimSpace(out.DirectSummary)
		}
	}
	if strings.TrimSpace(out.IndirectSummary) != "" {
		sectionEnrichments["indirect_effects"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "indirect_effects",
			Content:     strings.TrimSpace(out.IndirectSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
		if len(indirect) > 0 && strings.TrimSpace(indirect[0].Reason) == "" {
			indirect[0].Reason = strings.TrimSpace(out.IndirectSummary)
		}
	}
	if strings.TrimSpace(out.SignalSummary) != "" {
		sectionEnrichments["signals"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "signals",
			Content:     strings.TrimSpace(out.SignalSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
		if len(signals) > 0 && strings.TrimSpace(signals[0].Summary) == "" {
			signals[0].Summary = strings.TrimSpace(out.SignalSummary)
		}
	}
	if strings.TrimSpace(out.ScenarioSummary) != "" {
		sectionEnrichments["scenarios"] = horizonmodels.WatchPageSectionEnrichment{
			Key:         "scenarios",
			Content:     strings.TrimSpace(out.ScenarioSummary),
			BackingIDs:  compactUnique(backingIDs),
			Model:       strings.TrimSpace(out.Model),
			GeneratedAt: generatedAt,
		}
		if len(scenarios) > 0 && strings.TrimSpace(scenarios[0].Summary) == "" {
			scenarios[0].Summary = strings.TrimSpace(out.ScenarioSummary)
		}
	}
	for i := range dashboards {
		if caption := strings.TrimSpace(out.DashboardCaptions[dashboards[i].ID]); caption != "" {
			dashboards[i].Caption = caption
			sectionEnrichments["dashboard:"+dashboards[i].ID] = horizonmodels.WatchPageSectionEnrichment{
				Key:         "dashboard:" + dashboards[i].ID,
				Content:     caption,
				BackingIDs:  compactUnique(backingIDs),
				Model:       strings.TrimSpace(out.Model),
				GeneratedAt: generatedAt,
			}
		}
	}
	for i := range mapFragments {
		if caption := strings.TrimSpace(out.MapCaptions[mapFragments[i].ID]); caption != "" {
			mapFragments[i].Caption = caption
			sectionEnrichments["map:"+mapFragments[i].ID] = horizonmodels.WatchPageSectionEnrichment{
				Key:         "map:" + mapFragments[i].ID,
				Content:     caption,
				BackingIDs:  compactUnique(backingIDs),
				Model:       strings.TrimSpace(out.Model),
				GeneratedAt: generatedAt,
			}
		}
	}
}

func buildDeterministicPageSectionEnrichments(
	interestQuery string,
	anchors []horizonmodels.WatchPageScopeItem,
	reach []horizonmodels.WatchPageScopeItem,
	products []horizonmodels.WatchPageScopeItem,
	supplyPaths []horizonmodels.WatchPageSupplyPath,
	risks []horizonmodels.WatchPageRiskItem,
	affected []horizonmodels.WatchPageAffectedItem,
	direct []horizonmodels.WatchPageAffectedItem,
	indirect []horizonmodels.WatchPageAffectedItem,
	signals []horizonmodels.WatchPageSignalItem,
	scenarios []horizonmodels.WatchPageScenarioItem,
	dashboards []horizonmodels.WatchPageDashboardMetric,
	mapFragments []horizonmodels.WatchPageMapFragment,
	evidence []horizonmodels.WatchPageEvidenceItem,
	backingIDs []string,
	now time.Time,
) map[string]horizonmodels.WatchPageSectionEnrichment {
	sections := map[string]horizonmodels.WatchPageSectionEnrichment{}
	put := func(key, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		sections[key] = horizonmodels.WatchPageSectionEnrichment{
			Key:         key,
			Content:     content,
			BackingIDs:  compactUnique(append([]string(nil), backingIDs...)),
			Fallback:    true,
			GeneratedAt: now,
		}
	}
	put("scope", deterministicScopeSummary(interestQuery, anchors, reach))
	put("products", deterministicProductSummary(products))
	put("supply_paths", deterministicSupplyPathSummary(supplyPaths, mapFragments))
	put("what_is_at_risk", deterministicRiskSummary(risks))
	put("affected", deterministicAffectedSummary(affected, direct, indirect))
	put("direct_effects", deterministicImpactSummary("direct", direct))
	put("indirect_effects", deterministicImpactSummary("indirect", indirect))
	put("signals", deterministicSignalSummary(signals, dashboards))
	put("scenarios", deterministicScenarioSummary(scenarios, evidence))
	return sections
}

func deterministicScopeSummary(
	interestQuery string,
	anchors []horizonmodels.WatchPageScopeItem,
	reach []horizonmodels.WatchPageScopeItem,
) string {
	parts := []string{}
	if q := strings.TrimSpace(interestQuery); q != "" {
		parts = append(parts, "This page stays framed by the user question: "+q)
	}
	anchorLabels := firstScopeLabels(anchors, 3)
	if len(anchorLabels) > 0 {
		parts = append(parts, "It starts from anchors "+strings.Join(anchorLabels, ", "))
	}
	reachLabels := firstScopeLabels(reach, 4)
	if len(reachLabels) > 0 {
		parts = append(parts, "and expands that scope into "+strconv.Itoa(len(reach))+" reachable nodes including "+strings.Join(reachLabels, ", "))
	}
	return strings.Join(parts, ". ")
}

func deterministicProductSummary(products []horizonmodels.WatchPageScopeItem) string {
	if len(products) == 0 {
		return ""
	}
	lines := []string{}
	for _, item := range products {
		label := strings.TrimSpace(firstNonEmpty(item.Label, item.CanonicalPath))
		if label == "" {
			continue
		}
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			lines = append(lines, label+" is "+lowerFirst(summary))
		} else {
			lines = append(lines, label+" sits inside the tracked product graph")
		}
		if len(lines) >= 3 {
			break
		}
	}
	return strings.Join(lines, ". ")
}

func deterministicSupplyPathSummary(paths []horizonmodels.WatchPageSupplyPath, fragments []horizonmodels.WatchPageMapFragment) string {
	if len(paths) == 0 {
		return ""
	}
	parts := []string{}
	top := firstNStrings(titlesFromSupplyPaths(paths), 2)
	if len(top) > 0 {
		parts = append(parts, "The most exposed paths right now are "+strings.Join(top, " and "))
	}
	zoneRefs := 0
	for _, item := range paths {
		zoneRefs += len(item.AffectedZoneRefs)
	}
	if zoneRefs > 0 {
		parts = append(parts, fmt.Sprintf("with geographic pressure concentrated across %d mapped tension points", zoneRefs))
	} else if len(fragments) > 0 {
		parts = append(parts, fmt.Sprintf("with %d map fragments showing where tension clusters", len(fragments)))
	}
	return strings.Join(parts, ". ")
}

func deterministicRiskSummary(risks []horizonmodels.WatchPageRiskItem) string {
	if len(risks) == 0 {
		return ""
	}
	topTitles := firstRiskTitles(risks, 3)
	mechanisms := []string{}
	for _, item := range risks {
		mechanisms = append(mechanisms, item.Mechanisms...)
	}
	parts := []string{}
	if len(topTitles) > 0 {
		parts = append(parts, "Risk is currently concentrated around "+strings.Join(topTitles, ", "))
	}
	if m := firstNStrings(compactUnique(mechanisms), 4); len(m) > 0 {
		parts = append(parts, "with mechanisms spanning "+strings.Join(m, ", "))
	}
	return strings.Join(parts, ". ")
}

func deterministicAffectedSummary(
	affected []horizonmodels.WatchPageAffectedItem,
	direct []horizonmodels.WatchPageAffectedItem,
	indirect []horizonmodels.WatchPageAffectedItem,
) string {
	if len(affected) == 0 && len(direct) == 0 && len(indirect) == 0 {
		return ""
	}
	parts := []string{}
	if len(direct) > 0 {
		parts = append(parts, fmt.Sprintf("%d entities or nodes are in the immediate blast radius", len(direct)))
	}
	if len(indirect) > 0 {
		parts = append(parts, fmt.Sprintf("%d more appear in downstream exposure chains", len(indirect)))
	}
	if labels := firstAffectedLabels(affected, 4); len(labels) > 0 {
		parts = append(parts, "led by "+strings.Join(labels, ", "))
	}
	return strings.Join(parts, ". ")
}

func deterministicImpactSummary(kind string, items []horizonmodels.WatchPageAffectedItem) string {
	if len(items) == 0 {
		return ""
	}
	lines := []string{}
	for _, item := range items {
		label := strings.TrimSpace(firstNonEmpty(item.Label, item.CanonicalPath))
		if label == "" {
			continue
		}
		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			if kind == "direct" {
				reason = "attached to the first-order supply graph"
			} else {
				reason = "reachable through a downstream dependency chain"
			}
		}
		lines = append(lines, label+" is "+lowerFirst(reason))
		if len(lines) >= 3 {
			break
		}
	}
	return strings.Join(lines, ". ")
}

func deterministicSignalSummary(signals []horizonmodels.WatchPageSignalItem, dashboards []horizonmodels.WatchPageDashboardMetric) string {
	if len(signals) == 0 {
		if len(dashboards) == 0 {
			return ""
		}
		return "The page is currently grounded in persisted lens aggregates even when runtime event flow is still thin."
	}
	sourceLabels := []string{}
	sourceKinds := []string{}
	titles := []string{}
	for _, item := range signals {
		if label := strings.TrimSpace(item.SourceLabel); label != "" {
			sourceLabels = append(sourceLabels, label)
		}
		if kind := strings.TrimSpace(firstNonEmpty(item.SourceKind, item.Kind)); kind != "" {
			sourceKinds = append(sourceKinds, kind)
		}
		if title := strings.TrimSpace(firstNonEmpty(item.Title, item.Summary)); title != "" {
			titles = append(titles, title)
		}
	}
	parts := []string{
		fmt.Sprintf("%d aggregated signals are feeding this lens picture", len(signals)),
	}
	if labels := firstNStrings(compactUnique(sourceLabels), 3); len(labels) > 0 {
		parts = append(parts, "originating from "+strings.Join(labels, ", "))
	}
	if kinds := firstNStrings(compactUnique(sourceKinds), 3); len(kinds) > 0 {
		parts = append(parts, "with signal types "+strings.Join(kinds, ", "))
	}
	if top := firstNStrings(compactUnique(titles), 2); len(top) > 0 {
		parts = append(parts, "with the strongest current signal around "+strings.Join(top, " and "))
	}
	return strings.Join(parts, ". ")
}

func deterministicScenarioSummary(scenarios []horizonmodels.WatchPageScenarioItem, evidence []horizonmodels.WatchPageEvidenceItem) string {
	if len(scenarios) == 0 {
		return ""
	}
	titles := firstNStrings(titlesFromScenarios(scenarios), 3)
	parts := []string{}
	if len(titles) > 0 {
		parts = append(parts, "Current scenario framing stress-tests "+strings.Join(titles, ", "))
	}
	if len(evidence) > 0 {
		parts = append(parts, fmt.Sprintf("against %d evidence rails already assembled in this lens", len(evidence)))
	}
	return strings.Join(parts, ". ")
}

func lowerFirst(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 1 {
		return strings.ToLower(s)
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func derivePageFraming(lens *horizonmodels.Lens, brief *horizonmodels.WatchBrief) map[string]any {
	out := map[string]any{}
	if lens != nil && strings.TrimSpace(lens.Name) != "" {
		out["lensName"] = strings.TrimSpace(lens.Name)
	}
	if q := lensOriginalQuery(lens); q != "" {
		out["originalQuery"] = q
	}
	if dq := horizonmodels.LensDomainQueriesToMaps(lensDomainQueries(lens)); len(dq) > 0 {
		out["domainQueries"] = dq
	}
	if brief != nil {
		if strings.TrimSpace(brief.InterestTitle) != "" {
			out["interestTitle"] = strings.TrimSpace(brief.InterestTitle)
		}
		if strings.TrimSpace(brief.InterestQuery) != "" {
			out["interestQuery"] = strings.TrimSpace(brief.InterestQuery)
		}
	}
	return out
}

func lensOriginalQuery(lens *horizonmodels.Lens) string {
	if lens == nil {
		return ""
	}
	if q := strings.TrimSpace(lens.OriginalQuery); q != "" {
		return q
	}
	if lens.ViewConfig == nil {
		return ""
	}
	if q, ok := lens.ViewConfig["originalQuery"].(string); ok {
		return strings.TrimSpace(q)
	}
	return ""
}

func lensDomainQueries(lens *horizonmodels.Lens) []horizonmodels.LensDomainQuery {
	if lens == nil {
		return nil
	}
	if len(lens.DomainQueries) > 0 {
		return horizonmodels.NormalizeLensDomainQueries(lens.DomainQueries)
	}
	if lens.ViewConfig == nil {
		return nil
	}
	return horizonmodels.ParseLensDomainQueries(lens.ViewConfig["domains"])
}

func lensDomainQueryOverrides(lens *horizonmodels.Lens) []string {
	queries := lensDomainQueries(lens)
	if len(queries) == 0 {
		return nil
	}
	out := make([]string, 0, len(queries)*4)
	for _, item := range queries {
		out = append(out,
			strings.TrimSpace(item.Title),
			strings.TrimSpace(item.Description),
			strings.TrimSpace(item.CanonicalPath),
		)
		out = append(out, item.FirstOrderEntities...)
	}
	return compactUnique(out)
}

func metricNumericValue(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return 0, false
	}
	value, err := strconv.ParseFloat(b.String(), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func metricDeltaLabel(diff float64) string {
	if diff == float64(int64(diff)) {
		return fmt.Sprintf("%+d", int64(diff))
	}
	return fmt.Sprintf("%+.1f", diff)
}

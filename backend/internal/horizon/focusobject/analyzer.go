package focusobject

import (
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

const (
	defaultUniverseDepth       = 2
	defaultLookbackHours       = 24
	defaultMaxImpactedEntities = 8
	defaultMaxPropagationHops  = 5
	maxUniverseNodes           = 48
)

type Analyzer struct{}

func NewAnalyzer() *Analyzer { return &Analyzer{} }

func (a *Analyzer) Analyze(req AnalyzeRequest) (*Snapshot, error) {
	if strings.TrimSpace(req.Focus.AnchorCanonicalPath) == "" {
		return nil, fmt.Errorf("focus anchorCanonicalPath is required")
	}
	now := req.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	lookback := time.Duration(firstPositive(req.Focus.LookbackHours, defaultLookbackHours)) * time.Hour
	maxImpacted := firstPositive(req.Focus.MaxImpactedEntities, defaultMaxImpactedEntities)
	maxPropagationHops := firstPositive(req.Focus.MaxPropagationHops, defaultMaxPropagationHops)

	idx := buildIndex(req.Fragments, req.Edges, req.Artifacts, req.Facts)
	universe := buildUniverse(req.Focus, idx)
	universeSet := make(map[string]UniverseNode, len(universe))
	universePaths := make([]string, 0, len(universe))
	for _, node := range universe {
		universeSet[node.CanonicalPath] = node
		universePaths = append(universePaths, node.CanonicalPath)
	}

	matchedArtifacts := selectMatchedArtifacts(universeSet, req.Artifacts)
	artifactSet := make(map[string]PathExposure, len(matchedArtifacts))
	for _, item := range matchedArtifacts {
		artifactSet[item.ArtifactCanonicalKey] = item
	}

	factByID := make(map[string]*coremodels.OntologyFact, len(req.Facts))
	for _, fact := range req.Facts {
		if fact == nil || strings.TrimSpace(fact.ID) == "" {
			continue
		}
		factByID[strings.TrimSpace(fact.ID)] = fact
	}

	riskItems := make([]SignalSummary, 0, len(req.Signals))
	opportunityItems := make([]SignalSummary, 0, len(req.Signals))
	impactAccumulator := map[string]*ImpactedEntity{}
	artifactSignalScores := map[string][]string{}
	propagationAudits := make([]PropagationAudit, 0, len(req.Signals))
	summaryBySignalID := map[string]SignalSummary{}

	for _, signal := range req.Signals {
		if signal == nil {
			continue
		}
		if signal.ObservedAt.IsZero() || signal.ObservedAt.Before(now.Add(-lookback)) {
			continue
		}
		summary, ok := summarizeSignal(signal, factByID[strings.TrimSpace(signal.FactID)], now, universeSet, matchedArtifacts, idx)
		if !ok {
			continue
		}
		summaryBySignalID[summary.SignalID] = summary
		impacts, audit := propagateSignal(signal, summary, idx, universeSet, req.Focus.AnchorCanonicalPath, maxImpacted, maxPropagationHops)
		if len(audit.Reached) > 0 || len(audit.Steps) > 0 {
			propagationAudits = append(propagationAudits, audit)
		}
		concerned := make([]string, 0, len(impacts)+1)
		concerned = append(concerned, req.Focus.AnchorCanonicalPath)
		for _, item := range impacts {
			if item == nil || strings.TrimSpace(item.CanonicalPath) == "" {
				continue
			}
			concerned = append(concerned, item.CanonicalPath)
			accumulateImpactedEntity(impactAccumulator, item)
		}
		summary.ConcernedPaths = compactUnique(concerned)
		summary.Why = summarizeSignalWhy(summary, impacts)
		for _, artifact := range matchedArtifacts {
			if artifactMatchesSignal(artifact, signal) {
				artifactSignalScores[artifact.ArtifactCanonicalKey] = append(artifactSignalScores[artifact.ArtifactCanonicalKey], summary.SignalID)
			}
		}
		if strings.EqualFold(summary.Polarity, "opportunity") {
			opportunityItems = append(opportunityItems, summary)
		} else {
			riskItems = append(riskItems, summary)
		}
	}

	sortSignalSummaries(riskItems)
	sortSignalSummaries(opportunityItems)

	pathExposures := enrichPathExposures(matchedArtifacts, artifactSignalScores)
	affinityClusters := buildAffinityClusters(req.Focus.AnchorCanonicalPath, idx, universeSet)
	endpointHits := buildEndpointHits(req.Focus, idx, universeSet, summaryBySignalID, propagationAudits, affinityClusters, maxImpacted)
	endpointPackets := buildEndpointInterpretationPackets(req.Focus, idx, endpointHits, affinityClusters, maxImpacted)
	impacted := finalizeImpactedEntities(impactAccumulator, req.Focus.AnchorCanonicalPath, maxImpacted)
	if len(endpointHits) > 0 {
		impacted = legacyImpactedEntitiesFromEndpointHits(endpointHits, req.Focus.AnchorCanonicalPath, maxImpacted)
	}
	position := summarizePosition(req.Focus, req.Fragments, req.Facts, riskItems, opportunityItems, pathExposures, impacted, endpointHits, idx)

	displayLabel := strings.TrimSpace(req.Focus.DisplayLabel)
	if displayLabel == "" {
		displayLabel = labelForPath(idx.fragmentsByPath[strings.TrimSpace(req.Focus.AnchorCanonicalPath)], req.Focus.AnchorCanonicalPath)
	}
	artifactKeys := make([]string, 0, len(pathExposures))
	backingPaths := make([]string, 0, len(pathExposures)*2)
	for _, item := range pathExposures {
		artifactKeys = append(artifactKeys, item.ArtifactCanonicalKey)
		backingPaths = append(backingPaths, item.CounterpartyPaths...)
	}
	focus := SuggestedWatchSubject{
		AnchorCanonicalPath:   strings.TrimSpace(req.Focus.AnchorCanonicalPath),
		DisplayLabel:          displayLabel,
		UniversePaths:         firstNStrings(universePaths, 24),
		BackingCanonicalPaths: firstNStrings(compactUnique(append(append([]string{}, req.Focus.RelatedCanonicalPaths...), backingPaths...)), 24),
		ArtifactCanonicalKeys: artifactKeys,
		DomainModes:           compactUnique(req.Focus.DomainModes),
		QueryOverrides:        compactUnique(append([]string{displayLabel}, req.Focus.QueryOverrides...)),
		SourceMode:            string(horizonmodels.WatchSubjectSourceHybrid),
		RuntimeMode:           string(horizonmodels.WatchSubjectRuntimeFocusRuntime),
	}

	mentions := buildTodayMentions(focus.AnchorCanonicalPath, riskItems, opportunityItems, impacted)

	return &Snapshot{
		Focus:            focus,
		Position:         position,
		Universe:         universe,
		Threats:          trimSignalSummaries(riskItems, 6),
		Tailwinds:        trimSignalSummaries(opportunityItems, 4),
		PathExposures:    firstNPathExposures(pathExposures, 4),
		AffinityClusters: affinityClusters,
		EndpointHits:     endpointHits,
		EndpointPackets:  endpointPackets,
		ImpactedEntities: impacted,
		Propagation:      trimPropagationAudits(propagationAudits, 4),
		TodayMentions:    mentions,
		GeneratedAt:      now,
	}, nil
}

type analyzerIndex struct {
	fragmentsByID   map[string]*horizonmodels.HorizonFragment
	fragmentsByPath map[string]*horizonmodels.HorizonFragment
	adjacency       map[string][]graphLink
}

type graphLink struct {
	From          string
	To            string
	Type          DependencyType
	Weight        float64
	Amplification float64
	Criticality   float64
	Reverse       bool
	Channel       string
	ArtifactKey   string
}

func buildIndex(
	fragments []*horizonmodels.HorizonFragment,
	edges []*horizonmodels.DependencyEdge,
	artifacts []*horizonmodels.MaterializedArtifact,
	facts []*coremodels.OntologyFact,
) analyzerIndex {
	idx := analyzerIndex{
		fragmentsByID:   map[string]*horizonmodels.HorizonFragment{},
		fragmentsByPath: map[string]*horizonmodels.HorizonFragment{},
		adjacency:       map[string][]graphLink{},
	}
	for _, fragment := range fragments {
		if fragment == nil {
			continue
		}
		if id := strings.TrimSpace(fragment.ID); id != "" {
			idx.fragmentsByID[id] = fragment
		}
		if cp := strings.TrimSpace(fragment.CanonicalPath); cp != "" {
			idx.fragmentsByPath[cp] = fragment
		}
	}
	for _, edge := range edges {
		if edge == nil {
			continue
		}
		source := canonicalPathForEdgeEndpoint(idx.fragmentsByID, edge.SourceFragmentID)
		target := canonicalPathForEdgeEndpoint(idx.fragmentsByID, edge.TargetFragmentID)
		if source == "" || target == "" || source == target {
			continue
		}
		weight := propagationEdgeWeight(edge)
		amp := propagationAmplification(edge)
		crit := propagationCriticality(edge)
		idx.adjacency[source] = append(idx.adjacency[source], graphLink{
			From:          source,
			To:            target,
			Type:          edge.Relation,
			Weight:        weight,
			Amplification: amp,
			Criticality:   crit,
		})
		idx.adjacency[target] = append(idx.adjacency[target], graphLink{
			From:          target,
			To:            source,
			Type:          edge.Relation,
			Weight:        reversePropagationWeight(edge.Relation, weight),
			Amplification: amp,
			Criticality:   crit,
			Reverse:       true,
		})
	}
	for _, artifact := range artifacts {
		if artifact != nil {
			indexArtifactOverlayEdges(&idx, artifact)
		}
		if artifact != nil && shouldPopulateArtifactIndexes(artifact) {
			horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		}
	}
	_ = facts
	return idx
}

func buildUniverse(focus FocusObjectInput, idx analyzerIndex) []UniverseNode {
	depthLimit := firstPositive(focus.UniverseDepth, defaultUniverseDepth)
	nodes := map[string]*UniverseNode{}
	queue := []UniverseNode{}
	add := func(path string, distance int, reason string, inMatchedPath bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if existing, ok := nodes[path]; ok {
			if distance < existing.Distance {
				existing.Distance = distance
			}
			if inMatchedPath {
				existing.InMatchedPath = true
			}
			existing.Reasons = compactUnique(append(existing.Reasons, reason))
			return
		}
		frag := idx.fragmentsByPath[path]
		node := &UniverseNode{
			CanonicalPath: path,
			Label:         labelForPath(frag, path),
			Type:          fragmentTypeString(frag),
			Reasons:       compactUnique([]string{reason}),
			Distance:      distance,
			InMatchedPath: inMatchedPath,
		}
		nodes[path] = node
		queue = append(queue, *node)
	}

	add(focus.AnchorCanonicalPath, 0, "anchor", true)
	for _, related := range focus.RelatedCanonicalPaths {
		add(related, 0, "explicit_related", true)
	}

	for head := 0; head < len(queue); head++ {
		current := queue[head]
		if current.Distance >= depthLimit || len(nodes) >= maxUniverseNodes {
			continue
		}
		for _, link := range idx.adjacency[current.CanonicalPath] {
			if !universeRelationAllowed(link.Type) {
				continue
			}
			reason := "edge:" + string(link.Type)
			if _, ok := nodes[link.To]; ok {
				existing := nodes[link.To]
				existing.Reasons = compactUnique(append(existing.Reasons, reason))
				continue
			}
			add(link.To, current.Distance+1, reason, false)
		}
	}

	ordered := make([]UniverseNode, 0, len(nodes))
	for _, node := range nodes {
		ordered = append(ordered, *node)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Distance == ordered[j].Distance {
			return ordered[i].CanonicalPath < ordered[j].CanonicalPath
		}
		return ordered[i].Distance < ordered[j].Distance
	})
	return ordered
}

type localPropagationProfile struct {
	AxisKey         string
	DefaultDamping  float64
	MaxHops         int
	StrongEdges     map[DependencyType]horizonmodels.StrongEdgeSpec
	Transformations []horizonmodels.TransformationRule
	Fallback        bool
}

func resolveLocalPropagationProfile(axis string, requestedMaxHops int) localPropagationProfile {
	axisKey := normalizeLocalSignalAxis(axis)
	if axisKey == "industrial_policy" {
		return localPropagationProfile{
			AxisKey:        axisKey,
			DefaultDamping: 0.74,
			MaxHops:        firstPositive(requestedMaxHops, 3),
			StrongEdges: map[DependencyType]horizonmodels.StrongEdgeSpec{
				DependencyTypeOwns:       {Type: string(DependencyTypeOwns), Direction: "source_to_target", Weight: 1.0},
				DependencyTypeControls:   {Type: string(DependencyTypeControls), Direction: "source_to_target", Weight: 0.85},
				DependencyTypeManages:    {Type: string(DependencyTypeManages), Direction: "source_to_target", Weight: 0.80},
				DependencyTypeSuppliesTo: {Type: string(DependencyTypeSuppliesTo), Direction: "both", Weight: 0.72},
			},
			Transformations: []horizonmodels.TransformationRule{
				{OnEdgeType: string(DependencyTypeSuppliesTo), ToAxis: horizonmodels.AxisPhysicalFlowSupply, Factor: 0.72},
			},
		}
	}
	if horizonmodels.IsValidAxis(axisKey) {
		profile := horizonmodels.DefaultPropagationProfiles()[axisKey]
		strong := make(map[DependencyType]horizonmodels.StrongEdgeSpec, len(profile.StrongEdges))
		for _, spec := range profile.StrongEdges {
			raw := strings.TrimSpace(spec.Type)
			if raw == "" {
				continue
			}
			strong[DependencyType(raw)] = spec
		}
		return localPropagationProfile{
			AxisKey:         axisKey,
			DefaultDamping:  clampRange(profile.DefaultDamping, 0.55, 0.98),
			MaxHops:         firstPositive(requestedMaxHops, profile.DefaultMaxHops),
			StrongEdges:     strong,
			Transformations: append([]horizonmodels.TransformationRule(nil), profile.Transformations...),
		}
	}
	return localPropagationProfile{
		AxisKey:        strings.TrimSpace(axis),
		DefaultDamping: 0.80,
		MaxHops:        firstPositive(requestedMaxHops, defaultMaxPropagationHops),
		Fallback:       true,
	}
}

func normalizeLocalSignalAxis(axis string) string {
	normalized := strings.ToLower(strings.TrimSpace(axis))
	switch normalized {
	case horizonmodels.AxisLogisticsCapacity,
		horizonmodels.AxisLogisticsLatency,
		horizonmodels.AxisPhysicalFlowSupply,
		horizonmodels.AxisPhysicalFlowDemand,
		horizonmodels.AxisGeoConflict,
		horizonmodels.AxisOperationalContinuity,
		horizonmodels.AxisLegalAccess,
		horizonmodels.AxisLegalCompliance,
		horizonmodels.AxisFinancialSettlement,
		horizonmodels.AxisFinancialCredit,
		horizonmodels.AxisInfoUncertainty,
		horizonmodels.AxisInfoReputation:
		return normalized
	case "logistics_capacity":
		return horizonmodels.AxisLogisticsCapacity
	case "logistics_latency":
		return horizonmodels.AxisLogisticsLatency
	case "physical_flow_supply":
		return horizonmodels.AxisPhysicalFlowSupply
	case "physical_flow_demand":
		return horizonmodels.AxisPhysicalFlowDemand
	case "geo_conflict":
		return horizonmodels.AxisGeoConflict
	case "operational_continuity":
		return horizonmodels.AxisOperationalContinuity
	case "legal_access":
		return horizonmodels.AxisLegalAccess
	case "legal_compliance":
		return horizonmodels.AxisLegalCompliance
	case "financial_settlement":
		return horizonmodels.AxisFinancialSettlement
	case "financial_credit":
		return horizonmodels.AxisFinancialCredit
	case "info_uncertainty":
		return horizonmodels.AxisInfoUncertainty
	case "info_reputation":
		return horizonmodels.AxisInfoReputation
	default:
		return normalized
	}
}

func profileEdgeWeight(profile localPropagationProfile, link graphLink) (float64, bool) {
	if profile.Fallback || len(profile.StrongEdges) == 0 {
		return 1, true
	}
	spec, ok := profile.StrongEdges[link.Type]
	if !ok {
		return 0, false
	}
	dir := strings.TrimSpace(spec.Direction)
	switch dir {
	case "both":
		return clampRange(spec.Weight, 0.2, 1.5), true
	case "source_to_target":
		return clampRange(spec.Weight, 0.2, 1.5), !link.Reverse
	case "target_to_source":
		return clampRange(spec.Weight, 0.2, 1.5), link.Reverse
	default:
		return clampRange(spec.Weight, 0.2, 1.5), true
	}
}

type localAxisTransform struct {
	ToAxis     string
	Factor     float64
	EdgeWeight float64
	Reason     string
}

func applyProfileAxisTransform(profile localPropagationProfile, link graphLink) (localAxisTransform, bool) {
	for _, trans := range profile.Transformations {
		if !strings.EqualFold(strings.TrimSpace(trans.OnEdgeType), string(link.Type)) {
			continue
		}
		nextAxis := normalizeLocalSignalAxis(trans.ToAxis)
		if !horizonmodels.IsValidAxis(nextAxis) {
			continue
		}
		factor := trans.Factor
		if factor <= 0 {
			factor = 1
		}
		return localAxisTransform{
			ToAxis: nextAxis,
			Factor: clampRange(factor, 0.35, 1.15),
			Reason: "profile_transform",
		}, true
	}
	return localAxisTransform{}, false
}

func consequenceTransferRule(currentAxis string, link graphLink) (localAxisTransform, bool) {
	axis := normalizeLocalSignalAxis(currentAxis)
	if link.Reverse {
		return localAxisTransform{}, false
	}
	switch axis {
	case horizonmodels.AxisLegalAccess:
		if link.Type == DependencyTypeSuppliesTo {
			return localAxisTransform{
				ToAxis:     horizonmodels.AxisPhysicalFlowSupply,
				Factor:     0.78,
				EdgeWeight: 0.76,
				Reason:     "legal_access_to_supply_shortfall",
			}, true
		}
	case horizonmodels.AxisLegalCompliance:
		if link.Type == DependencyTypeSuppliesTo {
			return localAxisTransform{
				ToAxis:     horizonmodels.AxisPhysicalFlowSupply,
				Factor:     0.72,
				EdgeWeight: 0.70,
				Reason:     "legal_compliance_to_supply_shortfall",
			}, true
		}
	case horizonmodels.AxisFinancialSettlement:
		if link.Type == DependencyTypeSuppliesTo {
			return localAxisTransform{
				ToAxis:     horizonmodels.AxisPhysicalFlowSupply,
				Factor:     0.68,
				EdgeWeight: 0.66,
				Reason:     "settlement_to_supply_shortfall",
			}, true
		}
	case horizonmodels.AxisOperationalContinuity:
		if link.Type == DependencyTypeSuppliesTo {
			return localAxisTransform{
				ToAxis:     horizonmodels.AxisPhysicalFlowSupply,
				Factor:     0.82,
				EdgeWeight: 0.74,
				Reason:     "operational_loss_to_supply_shortfall",
			}, true
		}
	}
	return localAxisTransform{}, false
}

func maybeTransitAmplify(score float64, axisKey string, fromFrag, toFrag *horizonmodels.HorizonFragment) float64 {
	if !horizonmodels.IsValidAxis(axisKey) {
		return score
	}
	if hprop.IsChokepointFragment(fromFrag) || hprop.IsChokepointFragment(toFrag) {
		return hprop.TransitAmplify(score, axisKey)
	}
	return score
}

func directionForLink(link graphLink) string {
	if link.Reverse {
		return "target_to_source"
	}
	return "source_to_target"
}

func channelForAxis(axis string) string {
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisLogisticsCapacity,
		horizonmodels.AxisLogisticsLatency,
		horizonmodels.AxisOperationalContinuity:
		return "operational"
	case horizonmodels.AxisPhysicalFlowSupply,
		horizonmodels.AxisPhysicalFlowDemand,
		horizonmodels.AxisFinancialCredit,
		horizonmodels.AxisFinancialSettlement:
		return "economic"
	case horizonmodels.AxisGeoConflict:
		return "geopolitical"
	case horizonmodels.AxisLegalAccess,
		horizonmodels.AxisLegalCompliance:
		return "regulatory"
	default:
		return "mixed"
	}
}

func propagationScope(fromPath, toPath string) string {
	if countryToken(fromPath) != "" && countryToken(fromPath) == countryToken(toPath) {
		return "domestic"
	}
	if countryToken(fromPath) != "" && countryToken(toPath) != "" && countryToken(fromPath) != countryToken(toPath) {
		return "cross_border"
	}
	return "unknown"
}

func propagationSpillover(fromPath, toPath, channel string) string {
	scope := propagationScope(fromPath, toPath)
	switch {
	case channel == "economic" && scope == "cross_border":
		return "cross_border_economic"
	case channel == "operational" && scope == "cross_border":
		return "cross_border_operational"
	case scope == "domestic":
		return "direct_domestic"
	default:
		return "indirect"
	}
}

func pathFamilyHighlights(signal *horizonmodels.SignalDescriptor, trace []hprop.SignalPropagationTraceEvent, limit int) []PathFamilyHighlight {
	summary := hprop.ScorePathFamilies(signal, trace, limit)
	out := make([]PathFamilyHighlight, 0, len(summary.TopFamilies))
	for _, item := range summary.TopFamilies {
		out = append(out, PathFamilyHighlight{
			Key:         item.Key,
			Label:       item.Label,
			Score:       item.Score,
			Steps:       item.Steps,
			MaxHops:     item.MaxHops,
			Channels:    append([]string(nil), item.Channels...),
			EdgeTypes:   append([]string(nil), item.EdgeTypes...),
			SamplePaths: append([]string(nil), item.SamplePaths...),
		})
	}
	return out
}

func weakAmplitudeThresholdForAxis(axis string) float64 {
	switch normalizeLocalSignalAxis(axis) {
	case horizonmodels.AxisPhysicalFlowDemand,
		horizonmodels.AxisPhysicalFlowSupply:
		return 0.08
	case horizonmodels.AxisLegalAccess,
		horizonmodels.AxisFinancialSettlement:
		return 0.10
	default:
		return hprop.WeakAmplitudeThreshold
	}
}

func weakAmplitudeHopCutoffForAxis(axis string) int {
	switch normalizeLocalSignalAxis(axis) {
	case horizonmodels.AxisPhysicalFlowDemand,
		horizonmodels.AxisPhysicalFlowSupply:
		return hprop.MaxHopsForWeakAmplitude + 2
	case horizonmodels.AxisLegalAccess,
		horizonmodels.AxisFinancialSettlement:
		return hprop.MaxHopsForWeakAmplitude + 1
	default:
		return hprop.MaxHopsForWeakAmplitude
	}
}

func indexArtifactOverlayEdges(idx *analyzerIndex, artifact *horizonmodels.MaterializedArtifact) {
	if idx == nil || artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
		return
	}
	legs, err := horizonmodels.GetSupplyChainPathLegs(artifact)
	if err != nil || len(legs) == 0 {
		return
	}
	artifactKey := strings.TrimSpace(artifact.CanonicalKey)
	for _, leg := range legs {
		supplier := strings.TrimSpace(leg.SupplierRef)
		customer := strings.TrimSpace(leg.CustomerRef)
		if supplier != "" && customer != "" && supplier != customer {
			amp := clampRange(maxFloat(leg.BindingConfidence, leg.SegmentConfidence), 0.55, 1.25)
			crit := 0.55
			if leg.Stress != nil {
				crit = clampRange(0.35+leg.Stress.StressScore*0.65, 0.25, 1.0)
			}
			addVirtualGraphLink(idx, supplier, customer, DependencyTypeSuppliesTo, 0.94, amp, crit, "operational", artifactKey)
		}
		overlayTargets := compactUnique(append(append([]string{}, leg.TransportViaRefs...), append(append([]string{}, leg.ZoneRefs...), leg.AffectedZoneRefs...)...))
		for _, overlayPath := range overlayTargets {
			overlayPath = strings.TrimSpace(overlayPath)
			if overlayPath == "" {
				continue
			}
			amp := clampRange(maxFloat(leg.BindingConfidence, leg.SegmentConfidence), 0.60, 1.30)
			crit := 0.62
			if leg.Stress != nil {
				crit = clampRange(0.40+maxFloat(leg.Stress.ZonePressure, leg.Stress.AffectedZonePressure)*0.60, 0.30, 1.0)
			}
			if supplier != "" {
				addVirtualGraphLink(idx, overlayPath, supplier, DependencyTypeAffects, 0.88, amp, crit, "operational", artifactKey)
			}
			if customer != "" {
				addVirtualGraphLink(idx, overlayPath, customer, DependencyTypeAffects, 0.82, amp, crit, "operational", artifactKey)
			}
		}
	}
}

func addVirtualGraphLink(
	idx *analyzerIndex,
	source string,
	target string,
	edgeType DependencyType,
	weight float64,
	amp float64,
	crit float64,
	channel string,
	artifactKey string,
) {
	if idx == nil {
		return
	}
	source = strings.TrimSpace(source)
	target = strings.TrimSpace(target)
	if source == "" || target == "" || source == target {
		return
	}
	forward := graphLink{
		From:          source,
		To:            target,
		Type:          edgeType,
		Weight:        clampRange(weight, 0.18, 1.10),
		Amplification: clampRange(amp, 0.55, 1.35),
		Criticality:   clampRange(crit, 0.2, 1.0),
		Channel:       strings.TrimSpace(firstNonEmpty(channel, "mixed")),
		ArtifactKey:   artifactKey,
	}
	reverse := forward
	reverse.From = target
	reverse.To = source
	reverse.Weight = reversePropagationWeight(edgeType, forward.Weight)
	reverse.Reverse = true
	idx.adjacency[source] = append(idx.adjacency[source], forward)
	idx.adjacency[target] = append(idx.adjacency[target], reverse)
}

func summarizeSignal(
	signal *horizonmodels.SignalDescriptor,
	fact *coremodels.OntologyFact,
	now time.Time,
	universe map[string]UniverseNode,
	artifacts []PathExposure,
	idx analyzerIndex,
) (SignalSummary, bool) {
	matchedPaths := []string{}
	directness := ""
	addMatched := func(paths ...string) {
		for _, path := range paths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, ok := universe[path]; ok {
				matchedPaths = append(matchedPaths, path)
			}
		}
	}
	addMatched(signal.SeedPaths...)
	addMatched(signal.ActorPaths...)
	addMatched(signal.LocationPath)
	matchedPaths = compactUnique(matchedPaths)

	if containsString(matchedPaths, universeAnchor(universe)) {
		directness = "anchor"
	} else if len(matchedPaths) > 0 {
		directness = "universe"
	}
	if directness == "" {
		for _, artifact := range artifacts {
			if artifactMatchesSignal(artifact, signal) {
				directness = "path_overlay"
				break
			}
		}
	}
	if directness == "" {
		return SignalSummary{}, false
	}

	timeScore := recencyScore(now, signal.ObservedAt)
	score := clamp01(signal.Amplitude * maxFloat(signal.Confidence, 0.35) * escalationMultiplier(signal.Constraints.EscalationLevel) * timeScore)
	headline := strings.TrimSpace(firstNonEmpty(factTitle(fact), signalHeadline(signal)))
	summary := strings.TrimSpace(firstNonEmpty(factSummary(fact), signalSummary(signal)))
	sourceRefs := []string{}
	if fact != nil {
		for _, ref := range fact.SourceRefs {
			if src := strings.TrimSpace(ref.SourceID); src != "" {
				sourceRefs = append(sourceRefs, src)
			}
		}
	}
	sourceRefs = compactUnique(append(sourceRefs, signal.SourceRefs...))
	return SignalSummary{
		SignalID:     strings.TrimSpace(signal.ID),
		FactID:       strings.TrimSpace(signal.FactID),
		Headline:     headline,
		Summary:      summary,
		Polarity:     strings.TrimSpace(firstNonEmpty(signal.Polarity, "risk")),
		Axis:         strings.TrimSpace(signal.Axis),
		Mechanism:    strings.TrimSpace(signal.Mechanism),
		Score:        score,
		Directness:   directness,
		ObservedAt:   signal.ObservedAt.UTC(),
		SourceRefs:   sourceRefs,
		MatchedPaths: matchedPaths,
	}, true
}

func propagateSignal(
	signal *horizonmodels.SignalDescriptor,
	summary SignalSummary,
	idx analyzerIndex,
	universe map[string]UniverseNode,
	anchorPath string,
	maxImpacted int,
	maxHops int,
) ([]*ImpactedEntity, PropagationAudit) {
	type queueItem struct {
		Path        string
		Axis        string
		Score       float64
		Route       []string
		Steps       int
		Consequence string
	}
	initialAxis := normalizeLocalSignalAxis(summary.Axis)
	profile := resolveLocalPropagationProfile(initialAxis, maxHops)
	audit := PropagationAudit{
		SignalID:               strings.TrimSpace(summary.SignalID),
		Headline:               summary.Headline,
		Axis:                   initialAxis,
		Polarity:               strings.TrimSpace(summary.Polarity),
		SignalScore:            summary.Score,
		MaxHops:                profile.MaxHops,
		MinAmplitudeThreshold:  hprop.MinAmplitudeThreshold,
		WeakAmplitudeThreshold: weakAmplitudeThresholdForAxis(initialAxis),
		WeakAmplitudeHopCutoff: weakAmplitudeHopCutoffForAxis(initialAxis),
	}
	starts := compactUnique(append(append([]string{}, signal.SeedPaths...), signal.ActorPaths...))
	if strings.TrimSpace(signal.LocationPath) != "" {
		starts = append(starts, strings.TrimSpace(signal.LocationPath))
	}
	if len(starts) == 0 {
		starts = append(starts, summary.MatchedPaths...)
	}
	starts = compactUnique(starts)
	audit.SeedPaths = append([]string(nil), starts...)
	best := map[string]*ImpactedEntity{}
	visited := map[string]float64{}
	reached := map[string]PropagationReached{}
	queue := make([]queueItem, 0, len(starts))
	damping := hprop.NewWaveDamping()
	thresholds := hprop.NewThresholdManager()
	trace := make([]hprop.SignalPropagationTraceEvent, 0, 24)
	for _, start := range starts {
		start = strings.TrimSpace(start)
		if start == "" {
			continue
		}
		queue = append(queue, queueItem{Path: start, Axis: initialAxis, Score: summary.Score, Route: []string{start}})
		visited[start+"|"+initialAxis] = summary.Score
	}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		itemProfile := resolveLocalPropagationProfile(item.Axis, maxHops)
		if item.Score < hprop.MinAmplitudeThreshold || item.Steps >= itemProfile.MaxHops {
			continue
		}
		damping.SetBaseDampingFactor(itemProfile.DefaultDamping)
		for _, link := range idx.adjacency[item.Path] {
			if item.Consequence != "" && link.Type == DependencyTypeSuppliesTo && link.Reverse {
				audit.Steps = append(audit.Steps, PropagationStep{
					FromPath:      item.Path,
					ToPath:        link.To,
					EdgeType:      string(link.Type),
					Reverse:       link.Reverse,
					Hop:           item.Steps + 1,
					AxisIn:        item.Axis,
					AxisOut:       item.Axis,
					InputScore:    item.Score,
					EdgeWeight:    clampRange(link.Weight, 0.01, 1.25),
					Amplification: link.Amplification,
					Criticality:   link.Criticality,
					Outcome:       "dropped",
					Reason:        "reverse_blocked_after_supplier_specific_shortfall",
				})
				continue
			}
			specWeight, allowed := profileEdgeWeight(itemProfile, link)
			channel := firstNonEmpty(strings.TrimSpace(link.Channel), channelForAxis(item.Axis))
			nextAxis := item.Axis
			transformFactor := 1.0
			transformReason := ""
			if !allowed {
				if consequence, ok := consequenceTransferRule(item.Axis, link); ok {
					allowed = true
					specWeight = clampRange(consequence.EdgeWeight, 0.2, 1.15)
					nextAxis = consequence.ToAxis
					transformFactor = consequence.Factor
					transformReason = consequence.Reason
				}
			}
			step := PropagationStep{
				FromPath:        item.Path,
				ToPath:          link.To,
				EdgeType:        string(link.Type),
				Reverse:         link.Reverse,
				Hop:             item.Steps + 1,
				AxisIn:          item.Axis,
				AxisOut:         nextAxis,
				InputScore:      item.Score,
				EdgeWeight:      clampRange(link.Weight*specWeight, 0.01, 1.25),
				Amplification:   link.Amplification,
				Criticality:     link.Criticality,
				TransformFactor: transformFactor,
				TransformReason: transformReason,
			}
			if !allowed {
				step.Outcome = "dropped"
				step.Reason = "edge_not_in_profile"
				audit.Steps = append(audit.Steps, step)
				continue
			}
			if transform, ok := applyProfileAxisTransform(itemProfile, link); ok {
				nextAxis = transform.ToAxis
				transformFactor = transform.Factor
				transformReason = firstNonEmpty(transformReason, transform.Reason)
				step.AxisOut = nextAxis
				step.TransformFactor = transformFactor
				step.TransformReason = transformReason
			}
			outgoingScore := maybeTransitAmplify(item.Score, item.Axis, idx.fragmentsByPath[item.Path], idx.fragmentsByPath[link.To])
			preDamping := clamp01(outgoingScore * link.Weight * specWeight)
			nextScore := clamp01(damping.ApplyDamping(preDamping, link.Amplification, link.Criticality) * transformFactor)
			step.PreDampingScore = preDamping
			step.OutputScore = nextScore
			weakThreshold := weakAmplitudeThresholdForAxis(nextAxis)
			weakHopCutoff := weakAmplitudeHopCutoffForAxis(nextAxis)
			switch {
			case nextScore < hprop.MinAmplitudeThreshold:
				step.Outcome = "dropped"
				step.Reason = "below_min_amplitude"
				audit.Steps = append(audit.Steps, step)
				continue
			case step.Hop >= weakHopCutoff && nextScore < weakThreshold:
				step.Outcome = "dropped"
				step.Reason = "weak_tail_noise"
				audit.Steps = append(audit.Steps, step)
				continue
			case !thresholds.ExceedsThreshold(nextAxis, nextScore, nil):
				step.Outcome = "dropped"
				step.Reason = "below_threshold"
				audit.Steps = append(audit.Steps, step)
				continue
			}
			visitKey := link.To + "|" + nextAxis
			if prev, ok := visited[visitKey]; ok && prev >= nextScore {
				step.Outcome = "dropped"
				step.Reason = "not_stronger_than_existing"
				audit.Steps = append(audit.Steps, step)
				continue
			}
			step.Outcome = "propagated"
			audit.Steps = append(audit.Steps, step)
			trace = append(trace, hprop.SignalPropagationTraceEvent{
				SignalID:     strings.TrimSpace(signal.ID),
				FromPath:     item.Path,
				ToPath:       link.To,
				EdgeType:     string(link.Type),
				Direction:    directionForLink(link),
				Channel:      channel,
				Scope:        propagationScope(item.Path, link.To),
				Spillover:    propagationSpillover(item.Path, link.To, channel),
				AxisIn:       item.Axis,
				AxisOut:      nextAxis,
				AmplitudeIn:  item.Score,
				AmplitudeOut: nextScore,
				Hops:         step.Hop,
				Outcome:      "propagated",
				Factors: map[string]float64{
					"profile_default_damping": itemProfile.DefaultDamping,
					"profile_edge_weight":     specWeight,
					"edge_weight":             link.Weight,
					"amplification":           link.Amplification,
					"criticality":             link.Criticality,
					"transform_factor":        transformFactor,
				},
			})
			visited[visitKey] = nextScore
			route := append(append([]string{}, item.Route...), link.To)
			nextConsequence := item.Consequence
			if strings.HasSuffix(transformReason, "_to_supply_shortfall") {
				nextConsequence = transformReason
			}
			queue = append(queue, queueItem{Path: link.To, Axis: nextAxis, Score: nextScore, Route: route, Steps: item.Steps + 1, Consequence: nextConsequence})
			frag := idx.fragmentsByPath[link.To]
			classification := classifyPropagationTarget(link, link.To, universe)
			reached[link.To] = betterReached(
				reached[link.To],
				PropagationReached{
					CanonicalPath:  link.To,
					Label:          labelForPath(frag, link.To),
					Type:           fragmentTypeString(frag),
					Score:          nextScore,
					Hops:           item.Steps + 1,
					Classification: classification,
					Path:           route,
				},
			)
			driver := firstNonEmpty(summary.Headline, summary.Axis)
			if current, ok := best[link.To]; ok {
				if nextScore > current.ImpactScore {
					current.ImpactScore = nextScore
					current.Hops = item.Steps + 1
					current.Classification = classification
					current.Path = route
					current.Why = fmt.Sprintf("%s reaches %s through %s", driver, labelForPath(frag, link.To), strings.Join(route, " -> "))
				}
				current.Drivers = compactUnique(append(current.Drivers, driver))
				continue
			}
			best[link.To] = &ImpactedEntity{
				CanonicalPath:  link.To,
				Label:          labelForPath(frag, link.To),
				Type:           fragmentTypeString(frag),
				ImpactScore:    nextScore,
				Hops:           item.Steps + 1,
				Classification: classification,
				Drivers:        []string{driver},
				Path:           route,
				Why:            fmt.Sprintf("%s reaches %s through %s", driver, labelForPath(frag, link.To), strings.Join(route, " -> ")),
			}
		}
	}
	out := make([]*ImpactedEntity, 0, len(best))
	for _, item := range best {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ImpactScore == out[j].ImpactScore {
			return out[i].CanonicalPath < out[j].CanonicalPath
		}
		return out[i].ImpactScore > out[j].ImpactScore
	})
	if len(out) > maxImpacted {
		out = out[:maxImpacted]
	}
	audit.PathFamilies = pathFamilyHighlights(signal, trace, 4)
	audit.Reached = finalizePropagationReached(reached, maxImpacted*2)
	audit.Steps = finalizePropagationSteps(audit.Steps, maxHops, maxImpacted)
	return out, audit
}

func selectMatchedArtifacts(universe map[string]UniverseNode, artifacts []*horizonmodels.MaterializedArtifact) []PathExposure {
	out := make([]PathExposure, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		matched := false
		for _, path := range compactUnique(append(append([]string{}, artifact.NodeRefs...), artifact.AnchorSupplier, artifact.AnchorCustomer)) {
			if _, ok := universe[strings.TrimSpace(path)]; ok {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		counterparties := make([]string, 0, 4)
		counterparties = append(counterparties, strings.TrimSpace(artifact.AnchorSupplier), strings.TrimSpace(artifact.AnchorCustomer))
		if legs, err := horizonmodels.GetSupplyChainPathLegs(artifact); err == nil {
			for _, leg := range legs {
				counterparties = append(counterparties, strings.TrimSpace(leg.SupplierRef), strings.TrimSpace(leg.CustomerRef))
			}
		}
		out = append(out, PathExposure{
			ArtifactID:           strings.TrimSpace(artifact.ID),
			ArtifactCanonicalKey: strings.TrimSpace(artifact.CanonicalKey),
			DisplayName:          strings.TrimSpace(firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey)),
			ProductAxis:          strings.TrimSpace(artifact.ProductAxis),
			ExposureScore: clamp01(
				maxFloat(artifact.PathStressScore, 0)*0.55 +
					(1-clamp01(artifact.BindingScore))*0.20 +
					clamp01(artifact.RecoveryLatencyHrs/96.0)*0.25,
			),
			PathStressScore:    artifact.PathStressScore,
			BindingScore:       artifact.BindingScore,
			RecoveryLatencyHrs: artifact.RecoveryLatencyHrs,
			CounterpartyPaths:  firstNStrings(compactUnique(counterparties), 8),
			Why:                fmt.Sprintf("Path stress %.2f, binding %.2f, recovery latency %.0fh", artifact.PathStressScore, artifact.BindingScore, artifact.RecoveryLatencyHrs),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ExposureScore == out[j].ExposureScore {
			return out[i].ArtifactCanonicalKey < out[j].ArtifactCanonicalKey
		}
		return out[i].ExposureScore > out[j].ExposureScore
	})
	return out
}

func enrichPathExposures(in []PathExposure, artifactSignalScores map[string][]string) []PathExposure {
	out := append([]PathExposure(nil), in...)
	for idx := range out {
		out[idx].MatchedSignals = compactUnique(artifactSignalScores[out[idx].ArtifactCanonicalKey])
		if len(out[idx].MatchedSignals) > 0 {
			out[idx].ExposureScore = clamp01(out[idx].ExposureScore + 0.18)
			out[idx].Why = firstNonEmpty(out[idx].Why, "Path matched relevant signals")
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ExposureScore == out[j].ExposureScore {
			return out[i].ArtifactCanonicalKey < out[j].ArtifactCanonicalKey
		}
		return out[i].ExposureScore > out[j].ExposureScore
	})
	return out
}

func summarizePosition(
	focus FocusObjectInput,
	fragments []*horizonmodels.HorizonFragment,
	facts []*coremodels.OntologyFact,
	riskItems []SignalSummary,
	opportunityItems []SignalSummary,
	pathExposures []PathExposure,
	impacted []ImpactedEntity,
	endpointHits []EndpointHit,
	idx analyzerIndex,
) PositionSummary {
	riskPressure := sumTopSignalScores(riskItems, 3)
	opportunityPressure := sumTopSignalScores(opportunityItems, 2)
	endpointRisk := 0.0
	endpointOpportunity := 0.0
	anchorPath := strings.TrimSpace(focus.AnchorCanonicalPath)
	for _, item := range endpointHits {
		if strings.TrimSpace(item.CanonicalPath) != anchorPath {
			continue
		}
		if containsString(item.Polarities, "opportunity") {
			endpointOpportunity = maxFloat(endpointOpportunity, item.PeakScore)
		}
		if containsString(item.Polarities, "risk") {
			endpointRisk = maxFloat(endpointRisk, item.PeakScore)
		}
	}
	riskPressure = clamp01(maxFloat(riskPressure, endpointRisk))
	opportunityPressure = clamp01(maxFloat(opportunityPressure, endpointOpportunity))
	artifactExposure := 0.0
	if len(pathExposures) > 0 {
		artifactExposure = averageTopPathExposure(pathExposures, 2)
	}
	coverageInputs := 0.0
	if idx.fragmentsByPath[strings.TrimSpace(focus.AnchorCanonicalPath)] != nil {
		coverageInputs += 0.30
	}
	if len(facts) > 0 {
		coverageInputs += minFloat(0.25, float64(len(facts))*0.03)
	}
	if len(pathExposures) > 0 {
		coverageInputs += minFloat(0.25, float64(len(pathExposures))*0.12)
	}
	if len(riskItems)+len(opportunityItems) > 0 {
		coverageInputs += minFloat(0.20, float64(len(riskItems)+len(opportunityItems))*0.05)
	}
	coverageScore := clamp01(coverageInputs)
	readiness := clamp01(coverageScore*0.55 + minFloat(0.25, float64(len(impacted))*0.05) + artifactExposure*0.20)
	axes := dominantAxes(riskItems, opportunityItems, 4)
	warnings := []string{}
	if idx.fragmentsByPath[strings.TrimSpace(focus.AnchorCanonicalPath)] == nil {
		warnings = append(warnings, "anchor fragment is not present in the local graph fixture")
	}
	if len(pathExposures) == 0 {
		warnings = append(warnings, "no matched materialized paths for focus object")
	}
	if len(riskItems)+len(opportunityItems) == 0 {
		warnings = append(warnings, "no recent signals matched the focus universe")
	}
	return PositionSummary{
		RiskPressure:        clamp01(riskPressure),
		OpportunityPressure: clamp01(opportunityPressure),
		ArtifactExposure:    clamp01(artifactExposure),
		CoverageScore:       coverageScore,
		MonitoringReadiness: readiness,
		DominantAxes:        axes,
		Warnings:            warnings,
	}
}

func buildTodayMentions(anchorPath string, risks, tailwinds []SignalSummary, impacted []ImpactedEntity) []TodayMention {
	out := make([]TodayMention, 0, minInt(len(risks)+len(tailwinds), 6))
	appendMention := func(item SignalSummary) {
		paths := compactUnique(append([]string{anchorPath}, item.ConcernedPaths...))
		why := strings.TrimSpace(item.Why)
		if why == "" {
			why = firstNonEmpty(item.Summary, item.Headline)
		}
		out = append(out, TodayMention{
			ID:                   "signal:" + strings.TrimSpace(item.SignalID),
			Headline:             item.Headline,
			Polarity:             item.Polarity,
			Score:                item.Score,
			SubjectCanonicalPath: anchorPath,
			ConcernedPaths:       firstNStrings(paths, 6),
			Why:                  why,
			SourceSignalID:       item.SignalID,
		})
	}
	for _, item := range trimSignalSummaries(risks, 4) {
		appendMention(item)
	}
	for _, item := range trimSignalSummaries(tailwinds, 2) {
		appendMention(item)
	}
	if len(out) == 0 {
		for _, item := range impacted {
			out = append(out, TodayMention{
				ID:                   "impact:" + strings.TrimSpace(item.CanonicalPath),
				Headline:             firstNonEmpty(item.Label, item.CanonicalPath),
				Polarity:             "risk",
				Score:                item.ImpactScore,
				SubjectCanonicalPath: anchorPath,
				ConcernedPaths:       firstNStrings(compactUnique([]string{anchorPath, item.CanonicalPath}), 4),
				Why:                  item.Why,
			})
			if len(out) >= 3 {
				break
			}
		}
	}
	return out
}

func canonicalPathForEdgeEndpoint(fragmentByID map[string]*horizonmodels.HorizonFragment, id string) string {
	fragment := fragmentByID[strings.TrimSpace(id)]
	if fragment == nil {
		return ""
	}
	return strings.TrimSpace(fragment.CanonicalPath)
}

func propagationEdgeWeight(edge *horizonmodels.DependencyEdge) float64 {
	if edge == nil {
		return 0.45
	}
	return clampRange(baseEdgeWeight(edge.Relation), 0.18, 0.95)
}

func propagationAmplification(edge *horizonmodels.DependencyEdge) float64 {
	if edge == nil || edge.Properties.Amplification <= 0 {
		return 1
	}
	return clampRange(edge.Properties.Amplification, 0.55, 1.35)
}

func propagationCriticality(edge *horizonmodels.DependencyEdge) float64 {
	if edge == nil || edge.Properties.Criticality <= 0 {
		return 0.5
	}
	return clampRange(edge.Properties.Criticality, 0.2, 1.0)
}

func reversePropagationWeight(edgeType DependencyType, forward float64) float64 {
	switch edgeType {
	case DependencyTypeLocatedIn,
		DependencyTypeOperatesIn,
		DependencyTypeOwns,
		DependencyTypeSubsidiaryOf,
		DependencyTypeSuppliesTo,
		DependencyTypeDependsOn,
		DependencyTypeProduces,
		DependencyTypeConsumes,
		DependencyTypeTransformsInto,
		DependencyTypeAssemblesInto,
		DependencyTypeUsedIn,
		DependencyTypeHasMarketSegment,
		DependencyTypeParticipatesIn,
		DependencyTypeTradedIn,
		DependencyTypeHasSubmarket:
		return clampRange(forward*0.82, 0.14, 0.88)
	default:
		return clampRange(forward*0.62, 0.12, 0.74)
	}
}

func baseEdgeWeight(edgeType DependencyType) float64 {
	switch edgeType {
	case DependencyTypeSuppliesTo:
		return 0.92
	case DependencyTypeDependsOn:
		return 0.88
	case DependencyTypeProduces,
		DependencyTypeConsumes,
		DependencyTypeTransformsInto,
		DependencyTypeAssemblesInto,
		DependencyTypeUsedIn:
		return 0.82
	case DependencyTypeOwns,
		DependencyTypeSubsidiaryOf,
		DependencyTypeControls,
		DependencyTypeManages:
		return 0.74
	case DependencyTypeOperatesIn,
		DependencyTypeLocatedIn:
		return 0.65
	case DependencyTypeParticipatesIn,
		DependencyTypeDominates,
		DependencyTypeHasMarketSegment,
		DependencyTypeTradedIn,
		DependencyTypeHasSubmarket:
		return 0.74
	case DependencyTypeRegulates,
		DependencyTypeControlsAccess,
		DependencyTypeAppliesTo,
		DependencyTypeImposesCondition,
		DependencyTypeGatekeepsDecision:
		return 0.60
	default:
		return 0.48
	}
}

func universeRelationAllowed(edgeType DependencyType) bool {
	switch edgeType {
	case DependencyTypeOwns,
		DependencyTypeSubsidiaryOf,
		DependencyTypeControls,
		DependencyTypeManages,
		DependencyTypeOperatesIn,
		DependencyTypeLocatedIn,
		DependencyTypeSuppliesTo,
		DependencyTypeDependsOn,
		DependencyTypeProduces,
		DependencyTypeConsumes,
		DependencyTypeTransformsInto,
		DependencyTypeAssemblesInto,
		DependencyTypeUsedIn,
		DependencyTypeParticipatesIn,
		DependencyTypeDominates:
		return true
	default:
		return false
	}
}

func accumulateImpactedEntity(out map[string]*ImpactedEntity, in *ImpactedEntity) {
	if in == nil || strings.TrimSpace(in.CanonicalPath) == "" {
		return
	}
	key := strings.TrimSpace(in.CanonicalPath)
	if existing, ok := out[key]; ok {
		if in.ImpactScore > existing.ImpactScore {
			existing.ImpactScore = in.ImpactScore
			existing.Hops = in.Hops
			existing.Classification = in.Classification
			existing.Path = append([]string(nil), in.Path...)
			existing.Why = in.Why
		}
		existing.Drivers = compactUnique(append(existing.Drivers, in.Drivers...))
		return
	}
	cp := *in
	cp.Drivers = compactUnique(cp.Drivers)
	out[key] = &cp
}

func finalizeImpactedEntities(items map[string]*ImpactedEntity, anchorPath string, limit int) []ImpactedEntity {
	out := make([]ImpactedEntity, 0, len(items))
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.CanonicalPath) == "" || strings.TrimSpace(item.CanonicalPath) == strings.TrimSpace(anchorPath) {
			continue
		}
		item.Drivers = compactUnique(item.Drivers)
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ImpactScore == out[j].ImpactScore {
			if out[i].Hops == out[j].Hops {
				return out[i].CanonicalPath < out[j].CanonicalPath
			}
			return out[i].Hops < out[j].Hops
		}
		return out[i].ImpactScore > out[j].ImpactScore
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func finalizePropagationReached(items map[string]PropagationReached, limit int) []PropagationReached {
	out := make([]PropagationReached, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].Hops == out[j].Hops {
				return out[i].CanonicalPath < out[j].CanonicalPath
			}
			return out[i].Hops < out[j].Hops
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func finalizePropagationSteps(items []PropagationStep, maxHops, maxImpacted int) []PropagationStep {
	limit := maxInt(12, maxHops*maxImpacted)
	if len(items) <= limit {
		return append([]PropagationStep(nil), items...)
	}
	return append([]PropagationStep(nil), items[:limit]...)
}

func buildAffinityClusters(anchorPath string, idx analyzerIndex, universe map[string]UniverseNode) []AffinityCluster {
	relevant := map[string]struct{}{}
	for path := range universe {
		relevant[strings.TrimSpace(path)] = struct{}{}
	}
	if strings.TrimSpace(anchorPath) != "" {
		relevant[strings.TrimSpace(anchorPath)] = struct{}{}
	}

	visited := map[string]bool{}
	out := make([]AffinityCluster, 0, 4)
	clusterSeq := 0
	for path := range relevant {
		path = strings.TrimSpace(path)
		if path == "" || visited[path] {
			continue
		}
		component := []string{}
		reasons := []string{}
		queue := []string{path}
		seenInComponent := map[string]bool{}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if visited[current] {
				continue
			}
			visited[current] = true
			seenInComponent[current] = true
			component = append(component, current)
			for _, link := range idx.adjacency[current] {
				if !controlAffinityAllowed(link.Type) {
					continue
				}
				reasons = append(reasons, string(link.Type))
				next := strings.TrimSpace(link.To)
				if next == "" || seenInComponent[next] {
					continue
				}
				queue = append(queue, next)
			}
		}
		if len(component) <= 1 {
			continue
		}
		sort.Strings(component)
		clusterSeq++
		labelPath := component[0]
		if containsString(component, anchorPath) {
			labelPath = strings.TrimSpace(anchorPath)
		}
		label := labelForPath(idx.fragmentsByPath[labelPath], labelPath)
		out = append(out, AffinityCluster{
			ClusterID:       fmt.Sprintf("cluster-%02d", clusterSeq),
			Kind:            "control_affinity",
			Label:           label,
			CanonicalPaths:  component,
			AnchorContained: containsString(component, anchorPath),
			Reasons:         compactUnique(reasons),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AnchorContained != out[j].AnchorContained {
			return out[i].AnchorContained
		}
		if len(out[i].CanonicalPaths) == len(out[j].CanonicalPaths) {
			return out[i].ClusterID < out[j].ClusterID
		}
		return len(out[i].CanonicalPaths) > len(out[j].CanonicalPaths)
	})
	return out
}

func buildAffinityMembership(clusters []AffinityCluster) map[string]string {
	out := map[string]string{}
	for _, cluster := range clusters {
		for _, path := range cluster.CanonicalPaths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, ok := out[path]; ok {
				continue
			}
			out[path] = cluster.ClusterID
		}
	}
	return out
}

func controlAffinityAllowed(edgeType DependencyType) bool {
	switch edgeType {
	case DependencyTypeOwns,
		DependencyTypeSubsidiaryOf,
		DependencyTypeControls,
		DependencyTypeManages,
		DependencyTypeBoardMemberOf:
		return true
	default:
		return false
	}
}

func buildEndpointHits(
	focus FocusObjectInput,
	idx analyzerIndex,
	universe map[string]UniverseNode,
	summaries map[string]SignalSummary,
	audits []PropagationAudit,
	clusters []AffinityCluster,
	limit int,
) []EndpointHit {
	clusterByPath := buildAffinityMembership(clusters)
	items := map[string]*EndpointHit{}
	anchorPath := strings.TrimSpace(focus.AnchorCanonicalPath)
	for _, audit := range audits {
		summary, ok := summaries[audit.SignalID]
		if !ok {
			continue
		}
		for _, reached := range audit.Reached {
			key := strings.TrimSpace(reached.CanonicalPath)
			if key == "" {
				continue
			}
			hit := items[key]
			if hit == nil {
				hit = &EndpointHit{
					CanonicalPath:   key,
					Label:           firstNonEmpty(reached.Label, labelForPath(idx.fragmentsByPath[key], key)),
					Type:            firstNonEmpty(reached.Type, fragmentTypeString(idx.fragmentsByPath[key])),
					PeakScore:       reached.Score,
					AggregateScore:  reached.Score,
					MaxHops:         reached.Hops,
					DominantAxis:    audit.Axis,
					Polarities:      []string{audit.Polarity},
					SignalIDs:       []string{audit.SignalID},
					SignalHeadlines: []string{firstNonEmpty(summary.Headline, audit.Headline)},
					PathFamilies:    topPathFamilyLabels(audit.PathFamilies, 3),
					Classifications: []string{reached.Classification},
					SamplePath:      append([]string(nil), reached.Path...),
					Scope:           endpointScope(anchorPath, key, universe),
					ClusterID:       clusterByPath[key],
					Why:             fmt.Sprintf("%s reaches %s at %.2f through %s", firstNonEmpty(summary.Headline, audit.Headline), firstNonEmpty(reached.Label, key), reached.Score, strings.Join(reached.Path, " -> ")),
				}
				items[key] = hit
				continue
			}
			hit.AggregateScore = clamp01(hit.AggregateScore + reached.Score)
			hit.MaxHops = maxInt(hit.MaxHops, reached.Hops)
			hit.Polarities = compactUnique(append(hit.Polarities, audit.Polarity))
			hit.SignalIDs = compactUnique(append(hit.SignalIDs, audit.SignalID))
			hit.SignalHeadlines = compactUnique(append(hit.SignalHeadlines, firstNonEmpty(summary.Headline, audit.Headline)))
			hit.PathFamilies = compactUnique(append(hit.PathFamilies, topPathFamilyLabels(audit.PathFamilies, 3)...))
			hit.Classifications = compactUnique(append(hit.Classifications, reached.Classification))
			if reached.Score > hit.PeakScore {
				hit.PeakScore = reached.Score
				hit.DominantAxis = audit.Axis
				hit.SamplePath = append([]string(nil), reached.Path...)
				hit.Why = fmt.Sprintf("%s reaches %s at %.2f through %s", firstNonEmpty(summary.Headline, audit.Headline), firstNonEmpty(reached.Label, key), reached.Score, strings.Join(reached.Path, " -> "))
			}
		}
	}
	out := make([]EndpointHit, 0, len(items))
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.CanonicalPath) == "" {
			continue
		}
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PeakScore == out[j].PeakScore {
			return out[i].CanonicalPath < out[j].CanonicalPath
		}
		return out[i].PeakScore > out[j].PeakScore
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func endpointScope(anchorPath, path string, universe map[string]UniverseNode) string {
	if strings.TrimSpace(path) == strings.TrimSpace(anchorPath) {
		return "anchor"
	}
	if _, ok := universe[strings.TrimSpace(path)]; ok {
		return "focus_universe"
	}
	return "remote"
}

func legacyImpactedEntitiesFromEndpointHits(hits []EndpointHit, anchorPath string, limit int) []ImpactedEntity {
	out := make([]ImpactedEntity, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.CanonicalPath) == "" || strings.TrimSpace(hit.CanonicalPath) == strings.TrimSpace(anchorPath) {
			continue
		}
		out = append(out, ImpactedEntity{
			CanonicalPath:  hit.CanonicalPath,
			Label:          hit.Label,
			Type:           hit.Type,
			ImpactScore:    hit.PeakScore,
			Hops:           hit.MaxHops,
			Classification: firstNonEmpty(firstString(hit.Classifications), hit.Scope),
			Drivers:        append([]string(nil), hit.SignalHeadlines...),
			Path:           append([]string(nil), hit.SamplePath...),
			Why:            hit.Why,
		})
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildEndpointInterpretationPackets(
	focus FocusObjectInput,
	idx analyzerIndex,
	hits []EndpointHit,
	clusters []AffinityCluster,
	limit int,
) []EndpointInterpretationPacket {
	clusterByID := map[string]AffinityCluster{}
	for _, cluster := range clusters {
		clusterByID[cluster.ClusterID] = cluster
	}
	out := make([]EndpointInterpretationPacket, 0, len(hits))
	for _, hit := range hits {
		clusterPeers := []string{}
		if cluster, ok := clusterByID[hit.ClusterID]; ok {
			for _, path := range cluster.CanonicalPaths {
				if strings.TrimSpace(path) == strings.TrimSpace(hit.CanonicalPath) {
					continue
				}
				clusterPeers = append(clusterPeers, path)
			}
		}
		out = append(out, EndpointInterpretationPacket{
			PacketID:        "endpoint:" + strings.TrimSpace(hit.CanonicalPath),
			CanonicalPath:   hit.CanonicalPath,
			Label:           hit.Label,
			Type:            hit.Type,
			PeakScore:       hit.PeakScore,
			AggregateScore:  hit.AggregateScore,
			MaxHops:         hit.MaxHops,
			Axes:            compactUnique([]string{hit.DominantAxis}),
			Polarities:      append([]string(nil), hit.Polarities...),
			SignalIDs:       append([]string(nil), hit.SignalIDs...),
			SignalHeadlines: append([]string(nil), hit.SignalHeadlines...),
			PathFamilies:    append([]string(nil), hit.PathFamilies...),
			SamplePath:      append([]string(nil), hit.SamplePath...),
			ClusterID:       hit.ClusterID,
			ClusterPeers:    firstNStrings(compactUnique(clusterPeers), 6),
			Context:         buildEndpointInterpretationContext(hit, idx),
			Why:             hit.Why,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PeakScore == out[j].PeakScore {
			return out[i].CanonicalPath < out[j].CanonicalPath
		}
		return out[i].PeakScore > out[j].PeakScore
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildEndpointInterpretationContext(hit EndpointHit, idx analyzerIndex) []InterpretationContextNode {
	if strings.TrimSpace(hit.CanonicalPath) == "" {
		return nil
	}
	out := make([]InterpretationContextNode, 0, 6)
	seen := map[string]struct{}{}
	add := func(path string, relationship string, reasons ...string) {
		path = strings.TrimSpace(path)
		relationship = strings.TrimSpace(relationship)
		if path == "" || relationship == "" {
			return
		}
		key := path + "|" + relationship
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		frag := idx.fragmentsByPath[path]
		out = append(out, InterpretationContextNode{
			CanonicalPath: path,
			Label:         labelForPath(frag, path),
			Type:          fragmentTypeString(frag),
			Relationship:  relationship,
			Reasons:       compactUnique(reasons),
		})
	}
	for _, link := range idx.adjacency[strings.TrimSpace(hit.CanonicalPath)] {
		switch {
		case link.Type == DependencyTypeSuppliesTo && link.Reverse:
			relationship := "supplier_candidate"
			reason := "supplies_endpoint"
			if containsString(hit.SamplePath, link.To) {
				relationship = "in_path_supplier"
				reason = "supplier_on_reached_path"
			} else {
				relationship = "alternate_supplier_candidate"
				reason = "supplier_not_on_reached_path"
			}
			add(link.To, relationship, reason)
		case link.Type == DependencyTypeSuppliesTo && !link.Reverse:
			add(link.To, "downstream_customer", "endpoint_supplies_to_target")
		case controlAffinityAllowed(link.Type):
			add(link.To, "control_affinity", string(link.Type))
		}
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func classifyPropagationTarget(link graphLink, target string, universe map[string]UniverseNode) string {
	if _, ok := universe[strings.TrimSpace(target)]; ok {
		return "focus_universe"
	}
	switch link.Type {
	case DependencyTypeSuppliesTo:
		if link.Reverse {
			return "upstream_entanglement"
		}
		return "downstream_consequence"
	case DependencyTypeDependsOn:
		if link.Reverse {
			return "dependency_exposure"
		}
		return "dependency_trace"
	case DependencyTypeOwns,
		DependencyTypeSubsidiaryOf,
		DependencyTypeControls,
		DependencyTypeManages:
		return "ownership_cascade"
	default:
		if link.Reverse {
			return "reverse_propagation"
		}
		return "extended_cascade"
	}
}

func betterReached(current PropagationReached, candidate PropagationReached) PropagationReached {
	if strings.TrimSpace(current.CanonicalPath) == "" {
		return candidate
	}
	if candidate.Score > current.Score {
		return candidate
	}
	if candidate.Score == current.Score && candidate.Hops < current.Hops {
		return candidate
	}
	return current
}

func trimPropagationAudits(items []PropagationAudit, limit int) []PropagationAudit {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SignalScore == items[j].SignalScore {
			return items[i].SignalID < items[j].SignalID
		}
		return items[i].SignalScore > items[j].SignalScore
	})
	if len(items) <= limit {
		return append([]PropagationAudit(nil), items...)
	}
	return append([]PropagationAudit(nil), items[:limit]...)
}

func buildBusinessImpacts(
	focus FocusObjectInput,
	idx analyzerIndex,
	summaries map[string]SignalSummary,
	audits []PropagationAudit,
	limit int,
) []BusinessImpact {
	items := map[string]*BusinessImpact{}
	anchorPath := strings.TrimSpace(focus.AnchorCanonicalPath)
	for _, audit := range audits {
		summary, ok := summaries[audit.SignalID]
		if !ok {
			continue
		}
		if summary.Directness == "anchor" {
			frag := idx.fragmentsByPath[anchorPath]
			overlay := hprop.BuildPropagationNodeOverlay(frag)
			impact := BusinessImpact{
				CanonicalPath:   anchorPath,
				Label:           labelForPath(frag, anchorPath),
				Type:            fragmentTypeString(frag),
				Stance:          stanceForPolarity(summary.Polarity),
				Interestingness: clamp01(summary.Score*0.55 + overlay.OperationalScore*0.15 + overlay.CriticalityScore*0.15),
				ImpactScore:     summary.Score,
				Families:        topPathFamilyLabels(audit.PathFamilies, 2),
				Drivers:         []string{summary.Headline},
				Reasons:         []string{"anchor_direct_signal"},
				Path:            []string{anchorPath},
				Why:             fmt.Sprintf("%s is directly exposed to %s", labelForPath(frag, anchorPath), summary.Headline),
			}
			mergeBusinessImpact(items, impact)
		}
		for _, reached := range audit.Reached {
			frag := idx.fragmentsByPath[reached.CanonicalPath]
			overlay := hprop.BuildPropagationNodeOverlay(frag)
			remote := isRemotePath(anchorPath, reached.CanonicalPath)
			stance := deriveReachedStance(reached, summary, idx)
			interestingness := clamp01(
				reached.Score*0.48 +
					overlay.OperationalScore*0.14 +
					overlay.BridgeScore*0.12 +
					overlay.CriticalityScore*0.12 +
					topPathFamilyScore(audit.PathFamilies, 2)*0.14 +
					boolFloat(remote)*0.07 +
					boolFloat(reached.Hops >= 2)*0.05,
			)
			reasons := []string{classificationReason(reached.Classification)}
			if remote {
				reasons = append(reasons, "remote_tail")
			}
			if reached.Hops >= 2 {
				reasons = append(reasons, "multi_hop")
			}
			if stance == "beneficiary" {
				reasons = append(reasons, "market_substitution")
			}
			reasons = append(reasons, topPathFamilyLabels(audit.PathFamilies, 2)...)
			mergeBusinessImpact(items, BusinessImpact{
				CanonicalPath:   reached.CanonicalPath,
				Label:           reached.Label,
				Type:            reached.Type,
				Stance:          stance,
				Interestingness: interestingness,
				ImpactScore:     reached.Score,
				Hops:            reached.Hops,
				Remote:          remote,
				Families:        topPathFamilyLabels(audit.PathFamilies, 2),
				Drivers:         []string{summary.Headline},
				Reasons:         compactUnique(reasons),
				Path:            append([]string(nil), reached.Path...),
				Why:             fmt.Sprintf("%s %s via %s", reached.Label, businessWhyVerb(stance), strings.Join(reached.Path, " -> ")),
			})
		}
		for _, impact := range deriveBeneficiaryCandidates(anchorPath, idx, summary, audit) {
			mergeBusinessImpact(items, impact)
		}
	}
	out := make([]BusinessImpact, 0, len(items))
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.CanonicalPath) == "" {
			continue
		}
		item.Drivers = compactUnique(item.Drivers)
		item.Reasons = compactUnique(item.Reasons)
		item.Families = compactUnique(item.Families)
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Interestingness == out[j].Interestingness {
			if out[i].ImpactScore == out[j].ImpactScore {
				return out[i].CanonicalPath < out[j].CanonicalPath
			}
			return out[i].ImpactScore > out[j].ImpactScore
		}
		return out[i].Interestingness > out[j].Interestingness
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func deriveBeneficiaryCandidates(anchorPath string, idx analyzerIndex, summary SignalSummary, audit PropagationAudit) []BusinessImpact {
	if !strings.EqualFold(summary.Polarity, "risk") {
		return nil
	}
	marketPressure := topPathFamilyScore(audit.PathFamilies, 3)
	if marketPressure < 0.40 {
		return nil
	}
	harmed := make(map[string]struct{}, len(audit.Reached))
	for _, reached := range audit.Reached {
		harmed[strings.TrimSpace(reached.CanonicalPath)] = struct{}{}
	}
	out := make([]BusinessImpact, 0, 4)
	for _, reached := range audit.Reached {
		frag := idx.fragmentsByPath[reached.CanonicalPath]
		if !isDemandSinkCandidate(frag) {
			continue
		}
		for _, link := range idx.adjacency[reached.CanonicalPath] {
			if link.Type != DependencyTypeSuppliesTo || !link.Reverse {
				continue
			}
			candidatePath := strings.TrimSpace(link.To)
			if candidatePath == "" {
				continue
			}
			if candidatePath == strings.TrimSpace(anchorPath) && auditTouchesPath(audit, candidatePath) {
				continue
			}
			if _, ok := harmed[candidatePath]; ok {
				continue
			}
			candidateFrag := idx.fragmentsByPath[candidatePath]
			overlay := hprop.BuildPropagationNodeOverlay(candidateFrag)
			if !looksLikeBeneficiary(candidateFrag, overlay) {
				continue
			}
			interestingness := clamp01(
				reached.Score*0.42 +
					marketPressure*0.20 +
					overlay.OperationalScore*0.10 +
					overlay.CriticalityScore*0.12 +
					overlay.BridgeScore*0.06 +
					boolFloat(isRemotePath(anchorPath, candidatePath))*0.08,
			)
			if interestingness < 0.30 {
				continue
			}
			out = append(out, BusinessImpact{
				CanonicalPath:   candidatePath,
				Label:           labelForPath(candidateFrag, candidatePath),
				Type:            fragmentTypeString(candidateFrag),
				Stance:          "beneficiary",
				Interestingness: interestingness,
				ImpactScore:     clamp01(reached.Score*0.65 + marketPressure*0.25),
				Hops:            reached.Hops + 1,
				Remote:          isRemotePath(anchorPath, candidatePath),
				Families:        topPathFamilyLabels(audit.PathFamilies, 3),
				Drivers:         []string{summary.Headline},
				Reasons:         []string{"alternate_supplier", "market_tightening"},
				Path:            []string{candidatePath, reached.CanonicalPath},
				Why:             fmt.Sprintf("%s can benefit as an alternate supplier into %s while %s is stressed", labelForPath(candidateFrag, candidatePath), firstNonEmpty(reached.Label, reached.CanonicalPath), summary.Headline),
			})
		}
	}
	return out
}

func mergeBusinessImpact(items map[string]*BusinessImpact, candidate BusinessImpact) {
	if items == nil || strings.TrimSpace(candidate.CanonicalPath) == "" {
		return
	}
	key := strings.TrimSpace(candidate.CanonicalPath) + "|" + strings.TrimSpace(candidate.Stance)
	if existing, ok := items[key]; ok {
		if candidate.Interestingness > existing.Interestingness {
			existing.Interestingness = candidate.Interestingness
			existing.ImpactScore = candidate.ImpactScore
			existing.Hops = candidate.Hops
			existing.Remote = candidate.Remote
			existing.Path = append([]string(nil), candidate.Path...)
			existing.Why = candidate.Why
		}
		existing.Drivers = compactUnique(append(existing.Drivers, candidate.Drivers...))
		existing.Reasons = compactUnique(append(existing.Reasons, candidate.Reasons...))
		existing.Families = compactUnique(append(existing.Families, candidate.Families...))
		return
	}
	copyCandidate := candidate
	copyCandidate.Drivers = compactUnique(copyCandidate.Drivers)
	copyCandidate.Reasons = compactUnique(copyCandidate.Reasons)
	copyCandidate.Families = compactUnique(copyCandidate.Families)
	items[key] = &copyCandidate
}

func topPathFamilyScore(items []PathFamilyHighlight, limit int) float64 {
	top := items
	if len(top) > limit {
		top = top[:limit]
	}
	total := 0.0
	for _, item := range top {
		total += item.Score
	}
	if len(top) == 0 {
		return 0
	}
	return clamp01(total / float64(len(top)))
}

func topPathFamilyLabels(items []PathFamilyHighlight, limit int) []string {
	top := items
	if len(top) > limit {
		top = top[:limit]
	}
	out := make([]string, 0, len(top))
	for _, item := range top {
		out = append(out, firstNonEmpty(item.Label, item.Key))
	}
	return compactUnique(out)
}

func stanceForPolarity(polarity string) string {
	if strings.EqualFold(strings.TrimSpace(polarity), "opportunity") {
		return "beneficiary"
	}
	return "victim"
}

func businessWhyVerb(polarity string) string {
	switch strings.ToLower(strings.TrimSpace(polarity)) {
	case "opportunity", "beneficiary":
		return "benefits"
	default:
		return "is pressured"
	}
}

func classificationReason(classification string) string {
	switch strings.TrimSpace(classification) {
	case "dependency_exposure":
		return "dependency_exposure"
	case "downstream_consequence":
		return "downstream_consequence"
	case "upstream_entanglement":
		return "upstream_entanglement"
	default:
		return "graph_reach"
	}
}

func isDemandSinkCandidate(fragment *horizonmodels.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	switch fragment.Class {
	case FragmentTypeMarket, FragmentTypeOrganization, FragmentTypeFacility:
		return true
	default:
		return false
	}
}

func looksLikeBeneficiary(fragment *horizonmodels.HorizonFragment, overlay hprop.PropagationNodeOverlay) bool {
	if fragment == nil {
		return false
	}
	if fragment.IsLegacyDeposit() {
		return true
	}
	switch fragment.Class {
	case FragmentTypeFacility, FragmentTypeOrganization:
		return true
	}
	switch overlay.BusinessRole {
	case "producer", "assembler", "shipper", "market_operator", "transit_controller":
		return true
	}
	return overlay.OperationalScore >= 0.55 || overlay.CriticalityScore >= 0.60
}

func deriveReachedStance(reached PropagationReached, summary SignalSummary, idx analyzerIndex) string {
	if strings.EqualFold(summary.Polarity, "opportunity") {
		return "beneficiary"
	}
	if strings.EqualFold(summary.Polarity, "risk") && pathLooksLikeMarketSubstitution(reached.Path, idx) {
		return "beneficiary"
	}
	return "victim"
}

func pathLooksLikeMarketSubstitution(path []string, idx analyzerIndex) bool {
	if len(path) < 2 {
		return false
	}
	tail := strings.TrimSpace(path[len(path)-1])
	prev := strings.TrimSpace(path[len(path)-2])
	if tail == "" || prev == "" {
		return false
	}
	prevFrag := idx.fragmentsByPath[prev]
	if prevFrag == nil || prevFrag.Class != FragmentTypeMarket {
		return false
	}
	for _, link := range idx.adjacency[tail] {
		if strings.TrimSpace(link.To) == prev && link.Type == DependencyTypeSuppliesTo && !link.Reverse {
			return true
		}
	}
	return false
}

func auditTouchesPath(audit PropagationAudit, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, seed := range audit.SeedPaths {
		if strings.TrimSpace(seed) == path {
			return true
		}
	}
	for _, reached := range audit.Reached {
		for _, stepPath := range reached.Path {
			if strings.TrimSpace(stepPath) == path {
				return true
			}
		}
	}
	return false
}

func artifactMatchesSignal(artifact PathExposure, signal *horizonmodels.SignalDescriptor) bool {
	if signal == nil {
		return false
	}
	for _, chainRef := range signal.ChainRefs {
		chainRef = strings.TrimSpace(chainRef)
		if chainRef == "" {
			continue
		}
		if chainRef == strings.TrimSpace(artifact.ArtifactCanonicalKey) || chainRef == strings.TrimSpace(artifact.ArtifactID) {
			return true
		}
	}
	for _, path := range compactUnique(append(append([]string{}, signal.SeedPaths...), signal.ActorPaths...)) {
		if containsString(artifact.CounterpartyPaths, path) {
			return true
		}
	}
	return false
}

func recencyScore(now, observedAt time.Time) float64 {
	if observedAt.IsZero() {
		return 0.65
	}
	hours := now.Sub(observedAt.UTC()).Hours()
	switch {
	case hours <= 6:
		return 1.0
	case hours <= 24:
		return 0.9
	case hours <= 48:
		return 0.72
	default:
		return 0.55
	}
}

func escalationMultiplier(level string) float64 {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case horizonmodels.EscalationLevelCritical:
		return 1.28
	case horizonmodels.EscalationLevelWarning:
		return 1.18
	case horizonmodels.EscalationLevelWatch:
		return 1.08
	default:
		return 1.0
	}
}

func summarizeSignalWhy(summary SignalSummary, impacted []*ImpactedEntity) string {
	if len(impacted) == 0 {
		return firstNonEmpty(summary.Summary, summary.Headline)
	}
	labels := make([]string, 0, minInt(len(impacted), 3))
	for _, item := range impacted {
		if item == nil {
			continue
		}
		labels = append(labels, firstNonEmpty(item.Label, item.CanonicalPath))
		if len(labels) >= 3 {
			break
		}
	}
	return fmt.Sprintf("%s affects %s", firstNonEmpty(summary.Headline, summary.Axis), strings.Join(labels, ", "))
}

func firstString(items []string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return ""
}

func signalHeadline(signal *horizonmodels.SignalDescriptor) string {
	base := strings.TrimSpace(strings.ReplaceAll(signal.Axis, "_", " "))
	if mech := strings.TrimSpace(strings.ReplaceAll(signal.Mechanism, "_", " ")); mech != "" {
		return firstNonEmpty(mech, base)
	}
	if base == "" {
		return "focus signal"
	}
	return base
}

func signalSummary(signal *horizonmodels.SignalDescriptor) string {
	parts := []string{}
	if signal.Axis != "" {
		parts = append(parts, strings.ReplaceAll(strings.TrimSpace(signal.Axis), "_", " "))
	}
	if signal.Mechanism != "" {
		parts = append(parts, strings.ReplaceAll(strings.TrimSpace(signal.Mechanism), "_", " "))
	}
	if len(signal.SeedPaths) > 0 {
		parts = append(parts, "seeded at "+strings.Join(firstNStrings(compactUnique(signal.SeedPaths), 2), ", "))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func factTitle(fact *coremodels.OntologyFact) string {
	if fact == nil {
		return ""
	}
	return strings.TrimSpace(fact.Title)
}

func factSummary(fact *coremodels.OntologyFact) string {
	if fact == nil {
		return ""
	}
	return strings.TrimSpace(fact.Summary)
}

func universeAnchor(universe map[string]UniverseNode) string {
	for _, node := range universe {
		if node.Distance == 0 && containsString(node.Reasons, "anchor") {
			return node.CanonicalPath
		}
	}
	return ""
}

func sortSignalSummaries(items []SignalSummary) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].ObservedAt.After(items[j].ObservedAt)
		}
		return items[i].Score > items[j].Score
	})
}

func trimSignalSummaries(items []SignalSummary, limit int) []SignalSummary {
	if len(items) <= limit {
		return append([]SignalSummary(nil), items...)
	}
	return append([]SignalSummary(nil), items[:limit]...)
}

func firstNPathExposures(items []PathExposure, limit int) []PathExposure {
	if len(items) <= limit {
		return append([]PathExposure(nil), items...)
	}
	return append([]PathExposure(nil), items[:limit]...)
}

func sumTopSignalScores(items []SignalSummary, limit int) float64 {
	total := 0.0
	for _, item := range trimSignalSummaries(items, limit) {
		total += item.Score
	}
	return total
}

func averageTopPathExposure(items []PathExposure, limit int) float64 {
	top := firstNPathExposures(items, limit)
	if len(top) == 0 {
		return 0
	}
	total := 0.0
	for _, item := range top {
		total += item.ExposureScore
	}
	return clamp01(total / float64(len(top)))
}

func dominantAxes(risks, opportunities []SignalSummary, limit int) []string {
	type score struct {
		axis  string
		value float64
	}
	scores := map[string]float64{}
	for _, item := range append(append([]SignalSummary{}, risks...), opportunities...) {
		axis := strings.TrimSpace(item.Axis)
		if axis == "" {
			continue
		}
		scores[axis] += item.Score
	}
	list := make([]score, 0, len(scores))
	for axis, value := range scores {
		list = append(list, score{axis: axis, value: value})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].value == list[j].value {
			return list[i].axis < list[j].axis
		}
		return list[i].value > list[j].value
	})
	out := make([]string, 0, minInt(len(list), limit))
	for _, item := range list[:minInt(len(list), limit)] {
		out = append(out, item.axis)
	}
	return out
}

func labelForPath(fragment *horizonmodels.HorizonFragment, path string) string {
	if fragment != nil {
		if label := strings.TrimSpace(fragment.Labels["en"]); label != "" {
			return label
		}
		for _, label := range fragment.Labels {
			label = strings.TrimSpace(label)
			if label != "" {
				return label
			}
		}
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) == 0 {
		return strings.TrimSpace(path)
	}
	return strings.ReplaceAll(parts[len(parts)-1], "-", " ")
}

func fragmentTypeString(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	return string(fragment.Class)
}

func shouldPopulateArtifactIndexes(artifact *horizonmodels.MaterializedArtifact) bool {
	if artifact == nil {
		return false
	}
	if len(artifact.NodeRefs) > 0 ||
		len(artifact.GeoRefs) > 0 ||
		len(artifact.FlowRefs) > 0 ||
		len(artifact.ZoneRefs) > 0 ||
		len(artifact.AffectedZoneRefs) > 0 ||
		artifact.PathStressScore > 0 ||
		artifact.BindingScore > 0 ||
		artifact.RecoveryLatencyHrs > 0 ||
		artifact.LegCount > 0 {
		return false
	}
	return true
}

func clamp01(v float64) float64 { return clampRange(v, 0, 1) }

func clampRange(v, minValue, maxValue float64) float64 {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func countryToken(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[1]))
}

func isRemotePath(anchorPath, otherPath string) bool {
	anchorCountry := countryToken(anchorPath)
	otherCountry := countryToken(otherPath)
	return anchorCountry != "" && otherCountry != "" && anchorCountry != otherCountry
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func compactUnique(values []string) []string {
	seen := map[string]struct{}{}
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
	return out
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func firstNStrings(values []string, limit int) []string {
	values = compactUnique(values)
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

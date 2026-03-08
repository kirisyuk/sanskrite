package discovery

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/geoambiguity"
	mapactivity "github.com/igorkirisyuk/sanskrite/internal/horizon/mapactivity"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	wmIngestModeDocumentFeed = "document_feed"
	wmIngestModeOntologyFact = "ontology_fact"
	wmIngestModeQueryOnly    = "query_only"

	rssSignalTelemetryComponent = "rss_signal_pipeline"
	rssSignalTelemetryMetric    = "wm_signal_flow"
)

var (
	wmSeedUpperAcronymRegex = regexp.MustCompile(`\b[A-Z][A-Z0-9&+\-]{1,}\b`)
	wmSeedPhraseRegex       = regexp.MustCompile(`\b(?:[A-Z][a-z]+|[A-Z]{2,})(?:\s+(?:of|the|and|for|in|on|at|de|del|al|el|bin|ibn|st\.?|saint))?\s+(?:[A-Z][a-z]+|[A-Z]{2,})(?:\s+(?:[A-Z][a-z]+|[A-Z]{2,}))*\b`)
)

func normalizeHorizonMonitorIngestMode(ds *coremodels.DataSource) string {
	if ds == nil || ds.Details.HorizonMonitorIngestMode == nil {
		return wmIngestModeDocumentFeed
	}
	mode := strings.ToLower(strings.TrimSpace(*ds.Details.HorizonMonitorIngestMode))
	switch mode {
	case wmIngestModeDocumentFeed, wmIngestModeOntologyFact, wmIngestModeQueryOnly:
		return mode
	default:
		return wmIngestModeDocumentFeed
	}
}

// horizonMonitorShouldPublishDocumentProcess controls whether Horizon Monitor ingest should enqueue
// generic document.process extraction (LLM-heavy). We keep this off for now because
// Horizon Monitor has a dedicated deterministic pipeline below.
func horizonMonitorShouldPublishDocumentProcess(_ string) bool {
	return false
}

func sourceDisablesLLMProcessing(ds *coremodels.DataSource) bool {
	return coremodels.ShouldDisableLLMProcessing(ds)
}

type horizonMonitorSignalObservation struct {
	CountryCode      string
	CountryName      string
	SignalType       string
	Severity         string
	Title            string
	Timestamp        time.Time
	Lat              float64
	Lon              float64
	HasCoordinates   bool
	SourceID         string
	Classification   horizonMonitorNewsClassification
	ClassificationOK bool
}

type horizonMonitorTopicGeoObservation struct {
	CountryCode    string
	CountryName    string
	Topic          string
	Timestamp      time.Time
	Lat            float64
	Lon            float64
	HasCoordinates bool
	SourceID       string
}

type horizonMonitorCountryAggregate struct {
	CountryCode         string
	CountryName         string
	SignalCounts        map[string]int
	TotalSignals        int
	HighSeverityCount   int
	DistinctSignalTypes int
	ConvergenceScore    float64
	LastSeen            time.Time
	SourceIDs           []string
}

type horizonMonitorGridAggregate struct {
	CellID              string
	CellCenterLat       float64
	CellCenterLon       float64
	SignalCounts        map[string]int
	TotalSignals        int
	DistinctSignalTypes int
	ConvergenceScore    float64
	LastSeen            time.Time
}

type horizonMonitorTopicCountryAggregate struct {
	CountryCode   string
	CountryName   string
	Topic         string
	TotalMentions int
	LastSeen      time.Time
	SourceIDs     []string
}

type horizonMonitorFeedEvent struct {
	SourceID       string
	FeedType       string
	Category       string
	ExternalID     string
	Title          string
	Summary        string
	URL            string
	OccurredAt     time.Time
	CountryCode    string
	CountryName    string
	LocationPath   string
	Lat            float64
	Lon            float64
	HasCoordinates bool
	Severity       string
	Confidence     float64
	EventType      string
	SignalType     string
	SpherePaths    []string
	Raw            map[string]interface{}
}

type horizonMonitorSignalBlueprint struct {
	SignalType string
	Axis       string
	Mechanism  string
	Sphere     []string
}

type horizonMonitorNewsClassification struct {
	Level      string
	Category   string
	Confidence float64
	Source     string
}

type affectedZoneCandidate struct {
	CanonicalPath  string
	Label          string
	ConditionType  string
	ZoneType       string
	ZoneSubType    string
	GeoToken       string
	CountryCode    string
	CountryName    string
	RadiusKm       float64
	Lat            float64
	Lon            float64
	HasCoordinates bool
	GeoPrecision   string
	WarmOnly       bool
	Reason         string
}

type affectedZoneState struct {
	Score         float64
	Hits1h        float64
	Hits24h       float64
	TotalMentions float64
	LastBoost     float64
	LastMatchedAt time.Time
	LastUpdatedAt time.Time
}

const (
	zoneMetricHypeScore     = "zone_hype_score"
	zoneMetricHits1h        = "zone_hits_1h"
	zoneMetricHits24h       = "zone_hits_24h"
	zoneMetricTotalMentions = "zone_total_mentions"
	zoneMetricLastBoost     = "zone_last_boost"
	zoneMetricLastMatchTS   = "zone_last_match_unix"

	zoneHalfLifeScoreH = 8.0
	zoneHalfLife1hH    = 1.0
	zoneHalfLife24hH   = 24.0

	rssHeuristicsTelemetryComponent = "rss_heuristics"
	rssHeuristicsTelemetryMetric    = "wm_quality"
)

type rssHeuristicsQualityCounters struct {
	eventsTotal           int64
	categoryKnownTotal    int64
	highOrCriticalTotal   int64
	nonEventFiltered      int64
	signalSuppressedTotal int64
	zonesTotal            int64
	zonesTypedTotal       int64
	zonesGeocodedTotal    int64
	zonesPreciseGeoTotal  int64
	zonesApproxGeoTotal   int64
	zonesWithCountry      int64
	zonesMapReadyTotal    int64
	zonesWarmOnlyTotal    int64
}

type rssSignalFlowCounters struct {
	signalsTotal                   int64
	signalsSeededTotal             int64
	signalsNoSeedTotal             int64
	signalsPropagatedTotal         int64
	signalsZeroReachTotal          int64
	signalsZeroBusinessTotal       int64
	signalsHybridModeTotal         int64
	signalsTypedRWRModeTotal       int64
	signalsLocationOnlyTotal       int64
	signalsSemanticNamedTotal      int64
	signalsInfoUncertaintyTotal    int64
	signalsBusinessEndpointTotal   int64
	signalsNonObviousEndpointTotal int64
	signalsRemoteEndpointTotal     int64
	signalsLongPathEndpointTotal   int64
	signalsPathFamilyTotal         int64
	signalsBusinessPathFamilyTotal int64
	signalsCrossBorderFamilyTotal  int64
	signalsBreakpointTotal         int64
	breakpointThresholdTotal       int64
	breakpointTransferSkipTotal    int64
	breakpointHopLimitTotal        int64
	breakpointBusinessTotal        int64
	facilityEndpointsTotal         int64
	seedsTotal                     int64
	affectedTotal                  int64
	pathsTotal                     int64
	traceDroppedTotal              int64
	tailSelectedTotal              int64
	chainRefsTotal                 int64
	warningOrCriticalTotal         int64
}

type horizonMonitorSignalFlowAudit struct {
	Axis                 string
	SeedCount            int
	AffectedFragments    int
	PathsTraced          int
	TraceDropped         int
	ChainRefs            int
	EscalationLevel      string
	PropagationTried     bool
	LocationOnly         bool
	SemanticSeedNames    int
	BusinessEndpoints    int
	NonObviousEndpoints  int
	FacilityEndpoints    int
	RemoteEndpoints      int
	LongPathEndpoints    int
	PathFamilies         int
	BusinessPathFamilies int
	CrossBorderFamilies  int
	Breakpoints          int
	ThresholdBreakpoints int
	TransferSkips        int
	HopLimitBreakpoints  int
	BusinessBreakpoints  int
	KernelMode           string
	TailSelected         int
}

func (q *rssSignalFlowCounters) observe(audit *horizonMonitorSignalFlowAudit) {
	if audit == nil {
		return
	}
	q.signalsTotal++
	if audit.SeedCount > 0 {
		q.signalsSeededTotal++
		q.seedsTotal += int64(audit.SeedCount)
	} else {
		q.signalsNoSeedTotal++
	}
	if audit.ChainRefs > 0 {
		q.chainRefsTotal += int64(audit.ChainRefs)
	}
	if audit.LocationOnly {
		q.signalsLocationOnlyTotal++
	}
	switch strings.TrimSpace(audit.KernelMode) {
	case horizonpropagation.SignalKernelModeHybrid:
		q.signalsHybridModeTotal++
	case horizonpropagation.SignalKernelModeTypedRWR:
		q.signalsTypedRWRModeTotal++
	}
	if audit.SemanticSeedNames > 0 {
		q.signalsSemanticNamedTotal++
	}
	if audit.BusinessEndpoints > 0 {
		q.signalsBusinessEndpointTotal++
	} else if audit.PropagationTried && audit.SeedCount > 0 {
		q.signalsZeroBusinessTotal++
	}
	if audit.NonObviousEndpoints > 0 {
		q.signalsNonObviousEndpointTotal++
	}
	if audit.RemoteEndpoints > 0 {
		q.signalsRemoteEndpointTotal++
	}
	if audit.LongPathEndpoints > 0 {
		q.signalsLongPathEndpointTotal++
	}
	if audit.PathFamilies > 0 {
		q.signalsPathFamilyTotal++
	}
	if audit.BusinessPathFamilies > 0 {
		q.signalsBusinessPathFamilyTotal++
	}
	if audit.CrossBorderFamilies > 0 {
		q.signalsCrossBorderFamilyTotal++
	}
	if audit.Breakpoints > 0 {
		q.signalsBreakpointTotal++
	}
	if audit.ThresholdBreakpoints > 0 {
		q.breakpointThresholdTotal += int64(audit.ThresholdBreakpoints)
	}
	if audit.TransferSkips > 0 {
		q.breakpointTransferSkipTotal += int64(audit.TransferSkips)
	}
	if audit.HopLimitBreakpoints > 0 {
		q.breakpointHopLimitTotal += int64(audit.HopLimitBreakpoints)
	}
	if audit.BusinessBreakpoints > 0 {
		q.breakpointBusinessTotal += int64(audit.BusinessBreakpoints)
	}
	if audit.FacilityEndpoints > 0 {
		q.facilityEndpointsTotal += int64(audit.FacilityEndpoints)
	}
	if audit.PropagationTried && audit.AffectedFragments > 0 {
		q.signalsPropagatedTotal++
		q.affectedTotal += int64(audit.AffectedFragments)
		q.pathsTotal += int64(audit.PathsTraced)
	} else if audit.PropagationTried && audit.SeedCount > 0 {
		q.signalsZeroReachTotal++
	}
	if audit.TraceDropped > 0 {
		q.traceDroppedTotal += int64(audit.TraceDropped)
	}
	if audit.TailSelected > 0 {
		q.tailSelectedTotal += int64(audit.TailSelected)
	}
	switch strings.ToLower(strings.TrimSpace(audit.EscalationLevel)) {
	case horizonmodels.EscalationLevelWarning, horizonmodels.EscalationLevelCritical:
		q.warningOrCriticalTotal++
	}
	if strings.EqualFold(strings.TrimSpace(audit.Axis), horizonmodels.AxisInfoUncertainty) {
		q.signalsInfoUncertaintyTotal++
	}
}

func (q rssSignalFlowCounters) telemetryCounters() map[string]int64 {
	return map[string]int64{
		"signals_total":                      q.signalsTotal,
		"signals_seeded_total":               q.signalsSeededTotal,
		"signals_no_seed_total":              q.signalsNoSeedTotal,
		"signals_propagated_total":           q.signalsPropagatedTotal,
		"signals_zero_reach_total":           q.signalsZeroReachTotal,
		"signals_zero_business_total":        q.signalsZeroBusinessTotal,
		"signals_hybrid_mode_total":          q.signalsHybridModeTotal,
		"signals_typed_rwr_mode_total":       q.signalsTypedRWRModeTotal,
		"signals_location_only_total":        q.signalsLocationOnlyTotal,
		"signals_semantic_named_total":       q.signalsSemanticNamedTotal,
		"signals_info_uncertainty_total":     q.signalsInfoUncertaintyTotal,
		"signals_business_endpoint_total":    q.signalsBusinessEndpointTotal,
		"signals_non_obvious_endpoint_total": q.signalsNonObviousEndpointTotal,
		"signals_remote_endpoint_total":      q.signalsRemoteEndpointTotal,
		"signals_long_path_endpoint_total":   q.signalsLongPathEndpointTotal,
		"signals_path_family_total":          q.signalsPathFamilyTotal,
		"signals_business_path_family_total": q.signalsBusinessPathFamilyTotal,
		"signals_cross_border_family_total":  q.signalsCrossBorderFamilyTotal,
		"signals_breakpoint_total":           q.signalsBreakpointTotal,
		"breakpoint_threshold_total":         q.breakpointThresholdTotal,
		"breakpoint_transfer_skip_total":     q.breakpointTransferSkipTotal,
		"breakpoint_hop_limit_total":         q.breakpointHopLimitTotal,
		"breakpoint_business_total":          q.breakpointBusinessTotal,
		"facility_endpoints_total":           q.facilityEndpointsTotal,
		"seeds_total":                        q.seedsTotal,
		"affected_fragments_total":           q.affectedTotal,
		"propagation_paths_total":            q.pathsTotal,
		"trace_dropped_total":                q.traceDroppedTotal,
		"tail_selected_total":                q.tailSelectedTotal,
		"chain_refs_total":                   q.chainRefsTotal,
		"warning_or_critical_total":          q.warningOrCriticalTotal,
	}
}

func (q *rssHeuristicsQualityCounters) observe(classification horizonMonitorNewsClassification, zone *affectedZoneCandidate, nonEventFiltered bool) {
	q.eventsTotal++
	category := strings.ToLower(strings.TrimSpace(classification.Category))
	if category != "" && category != "general" {
		q.categoryKnownTotal++
	}
	switch strings.ToLower(strings.TrimSpace(classification.Level)) {
	case "high", "critical":
		q.highOrCriticalTotal++
	}
	if nonEventFiltered {
		q.nonEventFiltered++
	}
	if zone == nil {
		return
	}
	q.zonesTotal++
	if strings.TrimSpace(zone.ZoneType) != "" && strings.TrimSpace(zone.ZoneSubType) != "" {
		q.zonesTypedTotal++
	}
	if zone.HasCoordinates {
		q.zonesGeocodedTotal++
	}
	switch strings.ToLower(strings.TrimSpace(zone.GeoPrecision)) {
	case "precise":
		q.zonesPreciseGeoTotal++
	case "approximate_country":
		q.zonesApproxGeoTotal++
	}
	if strings.TrimSpace(zone.CountryCode) != "" {
		q.zonesWithCountry++
	}
	if zone.WarmOnly {
		q.zonesWarmOnlyTotal++
	}
	if strings.EqualFold(strings.TrimSpace(zone.GeoPrecision), "precise") &&
		zone.HasCoordinates && zone.RadiusKm > 0 &&
		strings.TrimSpace(zone.ZoneType) != "" &&
		strings.TrimSpace(zone.ZoneSubType) != "" {
		q.zonesMapReadyTotal++
	}
}

func (q rssHeuristicsQualityCounters) telemetryCounters() map[string]int64 {
	return map[string]int64{
		"events_total":             q.eventsTotal,
		"category_known_total":     q.categoryKnownTotal,
		"high_or_critical_total":   q.highOrCriticalTotal,
		"non_event_filtered_total": q.nonEventFiltered,
		"signal_suppressed_total":  q.signalSuppressedTotal,
		"zones_total":              q.zonesTotal,
		"zones_typed_total":        q.zonesTypedTotal,
		"zones_geocoded_total":     q.zonesGeocodedTotal,
		"zones_precise_geo_total":  q.zonesPreciseGeoTotal,
		"zones_approx_geo_total":   q.zonesApproxGeoTotal,
		"zones_with_country_total": q.zonesWithCountry,
		"zones_map_ready_total":    q.zonesMapReadyTotal,
		"zones_warm_only_total":    q.zonesWarmOnlyTotal,
	}
}

type horizonTrackObservation struct {
	Kind            horizonmodels.TrackKind
	ObjectType      string
	IdScheme        string // mmsi, icao24, imo, flight — for ID-in-path canonical format
	ExternalTrackID string
	DisplayName     string
	HeadingDeg      *float64
	SpeedKts        *float64
	AltitudeM       *float64
	DepthM          *float64
}

type horizonMonitorCountryBriefOutput struct {
	Summary          string   `json:"summary"`
	CurrentSituation string   `json:"currentSituation"`
	MilitarySecurity string   `json:"militarySecurity"`
	KeyRiskFactors   []string `json:"keyRiskFactors"`
	RegionalContext  string   `json:"regionalContext"`
	WatchItems       []string `json:"watchItems"`
	Confidence       float64  `json:"confidence"`
}

// HorizonMonitorPipeline transforms Horizon Monitor feeds to our ontology/signal model.
// It is deterministic-first and uses AI only for compact country briefs (strict JSON).
type HorizonMonitorPipeline struct {
	factsRepo          *corerepo.OntologyFactsRepository
	fragmentsRepo      *horizonrepo.HorizonFragmentsRepository
	telemetryRepo      *horizonrepo.HorizonTelemetryRepository
	realtimePublisher  *realtime.Publisher
	vertexSearch       *ai.VertexAISearchClient
	exec               *ai.AITaskExecutor
	mapActivityEngine  *mapactivity.Engine
	seedInjector       *signals.SeedInjector
	propagationEngine  *horizonpropagation.PropagationEngine
	profileLookup      *horizonpropagation.ProfileLookup
	logger             *log.Logger
	window             time.Duration
	waveThrottleWindow time.Duration
	enableAIClassify   bool
	enableAIBriefs     bool

	mu            sync.Mutex
	observations  []horizonMonitorSignalObservation
	topicObs      []horizonMonitorTopicGeoObservation
	recentWaves   map[string]time.Time
	lastBriefByCC map[string]time.Time
}

func NewHorizonMonitorPipeline(
	factsRepo *corerepo.OntologyFactsRepository,
	exec *ai.AITaskExecutor,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	edgesRepo *horizonrepo.DependencyEdgesRepository,
	propagationEngine *horizonpropagation.PropagationEngine,
	profileLookup *horizonpropagation.ProfileLookup,
	logger *log.Logger,
) *HorizonMonitorPipeline {
	var seedInjector *signals.SeedInjector
	if fragmentsRepo != nil && edgesRepo != nil {
		applicability := signals.NewExternalApplicabilityEvaluator().
			WithFragmentsRepo(fragmentsRepo).
			WithEdgesRepo(edgesRepo)
		seedInjector = signals.NewSeedInjector(fragmentsRepo, edgesRepo).
			WithApplicabilityEvaluator(applicability)
	}
	return &HorizonMonitorPipeline{
		factsRepo:          factsRepo,
		fragmentsRepo:      fragmentsRepo,
		exec:               exec,
		mapActivityEngine:  mapactivity.NewEngine(factsRepo, logger),
		seedInjector:       seedInjector,
		propagationEngine:  propagationEngine,
		profileLookup:      profileLookup,
		logger:             logger,
		window:             24 * time.Hour,
		waveThrottleWindow: 30 * time.Minute,
		enableAIClassify:   envBoolDefaultFalse("HORIZON_MONITOR_ENABLE_AI_CLASSIFICATION"),
		enableAIBriefs:     envBoolDefaultFalse("HORIZON_MONITOR_ENABLE_AI_BRIEFS"),
		recentWaves:        make(map[string]time.Time),
		lastBriefByCC:      make(map[string]time.Time),
	}
}

func (p *HorizonMonitorPipeline) WithTelemetryRepo(repo *horizonrepo.HorizonTelemetryRepository) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.telemetryRepo = repo
	return p
}

func (p *HorizonMonitorPipeline) WithRealtimePublisher(pub *realtime.Publisher) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.realtimePublisher = pub
	return p
}

func (p *HorizonMonitorPipeline) WithVertexSearch(client *ai.VertexAISearchClient) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.vertexSearch = client
	return p
}

func (p *HorizonMonitorPipeline) ProcessDocuments(ctx context.Context, tenantID string, ds *coremodels.DataSource, feedURL string, docs []prov.NormalizedDocument) error {
	if p == nil || p.factsRepo == nil || ds == nil {
		return nil
	}
	if len(docs) == 0 {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	mode := normalizeHorizonMonitorIngestMode(ds)
	if mode == wmIngestModeQueryOnly {
		return nil
	}

	feedType := ""
	if ds.Details.HorizonMonitorFeedType != nil {
		feedType = strings.TrimSpace(*ds.Details.HorizonMonitorFeedType)
	}
	category := ""
	if ds.Details.HorizonMonitorCategory != nil {
		category = strings.TrimSpace(*ds.Details.HorizonMonitorCategory)
	}
	signalHint := ""
	if ds.Details.HorizonMonitorSignalType != nil {
		signalHint = strings.TrimSpace(*ds.Details.HorizonMonitorSignalType)
	}

	newObservations := make([]horizonMonitorSignalObservation, 0, len(docs))
	topicObservations := make([]horizonMonitorTopicGeoObservation, 0, len(docs))
	mapEvents := make([]mapactivity.ObservedEvent, 0, len(docs))
	aiClassificationsUsed := 0
	qualityCounters := rssHeuristicsQualityCounters{}
	signalCounters := rssSignalFlowCounters{}

	for _, doc := range docs {
		event := p.buildEventFromDocument(ds.ID, feedType, category, signalHint, feedURL, doc)
		if event == nil {
			continue
		}
		if err := p.upsertTrackFragment(ctx, tenantID, event); err != nil {
			p.logf("horizon monitor pipeline: track upsert failed source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}

		if mode == wmIngestModeOntologyFact {
			eventFact, err := p.upsertEventFact(ctx, tenantID, ds.ID, event)
			if err != nil {
				p.logf("horizon monitor pipeline: failed to upsert event fact source=%s feedType=%s ext=%s: %v", ds.ID, event.FeedType, event.ExternalID, err)
			} else {
				if shouldSuppressHeuristicSignalPropagation(event) {
					qualityCounters.signalSuppressedTotal++
				}
				if signal := p.buildSignalDescriptor(eventFact, event); signal != nil {
					audit, err := p.upsertAndPropagateSignal(ctx, tenantID, ds.ID, signal, eventFact)
					signalCounters.observe(audit)
					if err != nil {
						p.logf("horizon monitor pipeline: signal propagation failed source=%s signal=%s: %v", ds.ID, signal.ID, err)
					}
				}
			}
		}

		classification := classifyHorizonMonitorNewsKeyword(strings.Join([]string{event.Title, event.Summary}, " "))
		classification = normalizeClassificationWithSourceHint(classification, event)
		if p.exec != nil && p.enableAIClassify && mode == wmIngestModeOntologyFact && classification.Confidence < 0.65 && aiClassificationsUsed < 4 {
			if refined, ok := p.classifyHorizonMonitorNewsWithAI(ctx, event.Title); ok {
				classification = refined
				aiClassificationsUsed++
			}
		}
		techTopics := detectTechTopicsFromText(strings.Join([]string{event.Title, event.Summary}, " "))
		if err := p.upsertNewsClassificationFact(ctx, tenantID, ds.ID, event, classification, techTopics); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert news classification source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		for _, topic := range techTopics {
			topic = strings.TrimSpace(topic)
			if topic == "" {
				continue
			}
			topicObservations = append(topicObservations, horizonMonitorTopicGeoObservation{
				CountryCode:    event.CountryCode,
				CountryName:    event.CountryName,
				Topic:          topic,
				Timestamp:      event.OccurredAt,
				Lat:            event.Lat,
				Lon:            event.Lon,
				HasCoordinates: event.HasCoordinates,
				SourceID:       ds.ID,
			})
		}
		zoneText := normalizeKeywordText(strings.Join([]string{
			strings.TrimSpace(event.Title),
			strings.TrimSpace(event.Summary),
			strings.TrimSpace(classification.Category),
			strings.TrimSpace(classification.Level),
		}, " "))
		nonEventFiltered := zoneText != "" && isLikelyNonEventText(zoneText)
		zoneCandidate := inferAffectedZoneCandidate(event, classification)
		qualityCounters.observe(classification, zoneCandidate, nonEventFiltered)
		if err := p.upsertAffectedZoneFragmentCandidate(ctx, tenantID, ds.ID, event, classification, zoneCandidate); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert affected zone source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}

		newObservations = append(newObservations, horizonMonitorSignalObservation{
			CountryCode:      event.CountryCode,
			CountryName:      event.CountryName,
			SignalType:       firstNonEmptyString(strings.TrimSpace(event.SignalType), "risk:temporal_anomaly"),
			Severity:         strings.TrimSpace(event.Severity),
			Title:            event.Title,
			Timestamp:        event.OccurredAt,
			Lat:              event.Lat,
			Lon:              event.Lon,
			HasCoordinates:   event.HasCoordinates,
			SourceID:         ds.ID,
			Classification:   classification,
			ClassificationOK: true,
		})
		mapEvents = append(mapEvents, mapactivity.ObservedEvent{
			Title:         event.Title,
			Summary:       event.Summary,
			Severity:      event.Severity,
			CountryCode:   event.CountryCode,
			FeedType:      event.FeedType,
			Category:      classification.Category,
			Level:         classification.Level,
			OccurredAt:    event.OccurredAt,
			SourceEventID: event.ExternalID,
		})
	}

	if len(newObservations) == 0 {
		return nil
	}
	p.emitRSSHeuristicsTelemetry(ctx, tenantID, qualityCounters)
	p.emitRSSSignalFlowTelemetry(ctx, tenantID, signalCounters)

	countries, cells := p.mergeAndAggregate(newObservations)
	topicCountries := p.mergeTopicGeoAggregates(topicObservations)
	if err := p.upsertCountryRollupFacts(ctx, tenantID, countries); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert country rollups: %v", err)
	}
	if err := p.upsertGridCellFacts(ctx, tenantID, cells); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert grid rollups: %v", err)
	}
	if err := p.upsertTopicCountryRollupFacts(ctx, tenantID, topicCountries); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert topic geo rollups: %v", err)
	}
	if len(mapEvents) > 0 && p.mapActivityEngine != nil {
		if err := p.mapActivityEngine.ObserveEvents(ctx, tenantID, ds.ID, mapEvents); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert map activity: %v", err)
		}
	}
	if mode == wmIngestModeOntologyFact {
		if p.enableAIBriefs {
			if err := p.upsertCountryBriefFacts(ctx, tenantID, countries); err != nil {
				p.logf("horizon monitor pipeline: failed to upsert country briefs: %v", err)
			}
		}
	}

	return nil
}

// ProcessFeedEvents processes pre-built feed events (for EventAdapter path).
// Plan §8: use when adapter returns events directly instead of payload.
func (p *HorizonMonitorPipeline) ProcessFeedEvents(ctx context.Context, tenantID string, ds *coremodels.DataSource, feedURL string, events []*horizonMonitorFeedEvent) error {
	if p == nil || p.factsRepo == nil || ds == nil {
		return nil
	}
	if len(events) == 0 {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	mode := normalizeHorizonMonitorIngestMode(ds)
	if mode == wmIngestModeQueryOnly {
		return nil
	}
	newObservations := make([]horizonMonitorSignalObservation, 0, len(events))
	topicObservations := make([]horizonMonitorTopicGeoObservation, 0, len(events))
	mapEvents := make([]mapactivity.ObservedEvent, 0, len(events))
	aiClassificationsUsed := 0
	qualityCounters := rssHeuristicsQualityCounters{}
	signalCounters := rssSignalFlowCounters{}
	for _, event := range events {
		if event == nil {
			continue
		}
		if err := p.upsertTrackFragment(ctx, tenantID, event); err != nil {
			p.logf("horizon monitor pipeline: track upsert failed source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		if mode == wmIngestModeOntologyFact {
			eventFact, err := p.upsertEventFact(ctx, tenantID, ds.ID, event)
			if err != nil {
				p.logf("horizon monitor pipeline: failed to upsert event fact source=%s feedType=%s ext=%s: %v", ds.ID, event.FeedType, event.ExternalID, err)
			} else {
				if shouldSuppressHeuristicSignalPropagation(event) {
					qualityCounters.signalSuppressedTotal++
				}
				if signal := p.buildSignalDescriptor(eventFact, event); signal != nil {
					audit, err := p.upsertAndPropagateSignal(ctx, tenantID, ds.ID, signal, eventFact)
					signalCounters.observe(audit)
					if err != nil {
						p.logf("horizon monitor pipeline: signal propagation failed source=%s signal=%s: %v", ds.ID, signal.ID, err)
					}
				}
			}
		}
		classification := classifyHorizonMonitorNewsKeyword(strings.Join([]string{event.Title, event.Summary}, " "))
		classification = normalizeClassificationWithSourceHint(classification, event)
		if p.exec != nil && p.enableAIClassify && mode == wmIngestModeOntologyFact && classification.Confidence < 0.65 && aiClassificationsUsed < 4 {
			if refined, ok := p.classifyHorizonMonitorNewsWithAI(ctx, event.Title); ok {
				classification = refined
				aiClassificationsUsed++
			}
		}
		techTopics := detectTechTopicsFromText(strings.Join([]string{event.Title, event.Summary}, " "))
		if err := p.upsertNewsClassificationFact(ctx, tenantID, ds.ID, event, classification, techTopics); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert news classification source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		for _, topic := range techTopics {
			topic = strings.TrimSpace(topic)
			if topic == "" {
				continue
			}
			topicObservations = append(topicObservations, horizonMonitorTopicGeoObservation{
				CountryCode:    event.CountryCode,
				CountryName:    event.CountryName,
				Topic:          topic,
				Timestamp:      event.OccurredAt,
				Lat:            event.Lat,
				Lon:            event.Lon,
				HasCoordinates: event.HasCoordinates,
				SourceID:       ds.ID,
			})
		}
		zoneText := normalizeKeywordText(strings.Join([]string{
			strings.TrimSpace(event.Title),
			strings.TrimSpace(event.Summary),
			strings.TrimSpace(classification.Category),
			strings.TrimSpace(classification.Level),
		}, " "))
		nonEventFiltered := zoneText != "" && isLikelyNonEventText(zoneText)
		zoneCandidate := inferAffectedZoneCandidate(event, classification)
		qualityCounters.observe(classification, zoneCandidate, nonEventFiltered)
		if err := p.upsertAffectedZoneFragmentCandidate(ctx, tenantID, ds.ID, event, classification, zoneCandidate); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert affected zone source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		newObservations = append(newObservations, horizonMonitorSignalObservation{
			CountryCode:      event.CountryCode,
			CountryName:      event.CountryName,
			SignalType:       firstNonEmptyString(strings.TrimSpace(event.SignalType), "risk:temporal_anomaly"),
			Severity:         strings.TrimSpace(event.Severity),
			Title:            event.Title,
			Timestamp:        event.OccurredAt,
			Lat:              event.Lat,
			Lon:              event.Lon,
			HasCoordinates:   event.HasCoordinates,
			SourceID:         ds.ID,
			Classification:   classification,
			ClassificationOK: true,
		})
		mapEvents = append(mapEvents, mapactivity.ObservedEvent{
			Title:         event.Title,
			Summary:       event.Summary,
			Severity:      event.Severity,
			CountryCode:   event.CountryCode,
			FeedType:      event.FeedType,
			Category:      classification.Category,
			Level:         classification.Level,
			OccurredAt:    event.OccurredAt,
			SourceEventID: event.ExternalID,
		})
	}
	if len(newObservations) == 0 {
		return nil
	}
	p.emitRSSHeuristicsTelemetry(ctx, tenantID, qualityCounters)
	p.emitRSSSignalFlowTelemetry(ctx, tenantID, signalCounters)
	countries, cells := p.mergeAndAggregate(newObservations)
	topicCountries := p.mergeTopicGeoAggregates(topicObservations)
	if err := p.upsertCountryRollupFacts(ctx, tenantID, countries); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert country rollups: %v", err)
	}
	if err := p.upsertGridCellFacts(ctx, tenantID, cells); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert grid rollups: %v", err)
	}
	if err := p.upsertTopicCountryRollupFacts(ctx, tenantID, topicCountries); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert topic geo rollups: %v", err)
	}
	if len(mapEvents) > 0 && p.mapActivityEngine != nil {
		if err := p.mapActivityEngine.ObserveEvents(ctx, tenantID, ds.ID, mapEvents); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert map activity: %v", err)
		}
	}
	if mode == wmIngestModeOntologyFact {
		if p.enableAIBriefs {
			if err := p.upsertCountryBriefFacts(ctx, tenantID, countries); err != nil {
				p.logf("horizon monitor pipeline: failed to upsert country briefs: %v", err)
			}
		}
	}
	return nil
}

func envBoolDefaultFalse(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func horizonMonitorLineage(
	current coremodels.FactExtractionLayer,
	llmEnabled bool,
	llmUsed bool,
) *coremodels.FactExtractionLineage {
	steps := []coremodels.FactExtractionStep{
		{
			Layer:         coremodels.FactExtractionLayerSourceMechanics,
			Driver:        "rss",
			Method:        "poll_parse_normalize",
			Deterministic: true,
			LLMUsed:       false,
		},
	}
	appendStep := func(layer coremodels.FactExtractionLayer, driver, method string, deterministic, stepLLM bool) {
		for _, step := range steps {
			if step.Layer == layer {
				return
			}
		}
		steps = append(steps, coremodels.FactExtractionStep{
			Layer:         layer,
			Driver:        strings.TrimSpace(driver),
			Method:        strings.TrimSpace(method),
			Deterministic: deterministic,
			LLMUsed:       stepLLM,
		})
	}

	switch current {
	case coremodels.FactExtractionLayerHeuristic:
		appendStep(coremodels.FactExtractionLayerHeuristic, "keyword_rules", "canonicalize_and_score", true, false)
	case coremodels.FactExtractionLayerLLM:
		appendStep(coremodels.FactExtractionLayerHeuristic, "keyword_rules", "canonicalize_and_score", true, false)
		appendStep(coremodels.FactExtractionLayerLLM, "genai", "refine_or_summarize", false, true)
	case coremodels.FactExtractionLayerSignal:
		appendStep(coremodels.FactExtractionLayerHeuristic, "keyword_rules", "canonicalize_and_score", true, false)
		appendStep(coremodels.FactExtractionLayerSignal, "signal_derivation", "propagate_watchdog_seed", true, false)
	case coremodels.FactExtractionLayerInterpretation, coremodels.FactExtractionLayerWarning:
		appendStep(coremodels.FactExtractionLayerHeuristic, "keyword_rules", "canonicalize_and_score", true, false)
		appendStep(coremodels.FactExtractionLayerSignal, "signal_derivation", "propagate_watchdog_seed", true, false)
		appendStep(current, "graph_reasoning", "impact_interpretation", true, false)
	}

	return &coremodels.FactExtractionLineage{
		Pipeline:     "horizon_monitor",
		SourceType:   "rss",
		CurrentLayer: current,
		LLMEnabled:   llmEnabled,
		LLMUsed:      llmUsed,
		Steps:        steps,
	}
}

func horizonMonitorFactTags(current coremodels.FactExtractionLayer, llmEnabled bool, llmUsed bool, extra ...string) []string {
	tags := []string{
		"horizon-monitor",
		"source_layer:rss",
		"source_mechanic:rss",
		"processing_layer:heuristic",
		"extraction_pipeline:horizon_monitor",
		"extraction_current:" + string(current),
		"llm_enabled:" + strconv.FormatBool(llmEnabled),
		"llm_used:" + strconv.FormatBool(llmUsed),
	}
	if current == coremodels.FactExtractionLayerHeuristic || current == coremodels.FactExtractionLayerSignal || current == coremodels.FactExtractionLayerInterpretation || current == coremodels.FactExtractionLayerWarning {
		tags = append(tags, "processing_mode:heuristic")
	}
	if current == coremodels.FactExtractionLayerLLM {
		tags = append(tags, "processing_mode:llm")
	}
	tags = append(tags, extra...)
	return uniqueStrings(tags)
}

func horizonMonitorFragmentExtraction(currentLayer string, llmUsed bool) map[string]interface{} {
	return map[string]interface{}{
		"pipeline":     "horizon_monitor",
		"sourceType":   "rss",
		"sourceLayer":  "source_mechanics",
		"sourceDriver": "rss",
		"processing":   "heuristic",
		"currentLayer": strings.TrimSpace(currentLayer),
		"llmEnabled":   false,
		"llmUsed":      llmUsed,
		"upstreamLayers": []string{
			"source_mechanics",
			"heuristic",
		},
	}
}

func (p *HorizonMonitorPipeline) emitRSSHeuristicsTelemetry(ctx context.Context, tenantID string, counters rssHeuristicsQualityCounters) {
	if p == nil || p.telemetryRepo == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	data := counters.telemetryCounters()
	if data["events_total"] <= 0 {
		return
	}
	if err := p.telemetryRepo.Increment(
		ctx,
		tenantID,
		rssHeuristicsTelemetryComponent,
		rssHeuristicsTelemetryMetric,
		time.Hour,
		nil,
		data,
	); err != nil {
		p.logf("horizon monitor pipeline: failed to emit rss heuristics telemetry: %v", err)
	}
}

func (p *HorizonMonitorPipeline) emitRSSSignalFlowTelemetry(ctx context.Context, tenantID string, counters rssSignalFlowCounters) {
	if p == nil || p.telemetryRepo == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	data := counters.telemetryCounters()
	if data["signals_total"] <= 0 {
		return
	}
	if err := p.telemetryRepo.Increment(
		ctx,
		tenantID,
		rssSignalTelemetryComponent,
		rssSignalTelemetryMetric,
		time.Hour,
		nil,
		data,
	); err != nil {
		p.logf("horizon monitor pipeline: failed to emit rss signal-flow telemetry: %v", err)
	}
}

func horizonMonitorEscalationLevelFromSeverity(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return horizonmodels.EscalationLevelCritical
	case "high":
		return horizonmodels.EscalationLevelWarning
	case "medium":
		return horizonmodels.EscalationLevelWatch
	default:
		return horizonmodels.EscalationLevelNone
	}
}

func (p *HorizonMonitorPipeline) buildEventFromDocument(sourceID, feedType, category, signalHint, feedURL string, doc prov.NormalizedDocument) *horizonMonitorFeedEvent {
	now := time.Now().UTC()
	e := &horizonMonitorFeedEvent{
		SourceID:   strings.TrimSpace(sourceID),
		FeedType:   strings.TrimSpace(feedType),
		Category:   strings.TrimSpace(category),
		ExternalID: firstNonEmptyString(strings.TrimSpace(doc.ExternalID), shortHash(feedURL+"|"+doc.Title+"|"+doc.FullText)),
		Title:      firstNonEmptyString(strings.TrimSpace(doc.Title), "Horizon Monitor event"),
		Summary:    strings.TrimSpace(doc.FullText),
		URL:        firstNonEmptyString(strings.TrimSpace(doc.URL), strings.TrimSpace(feedURL)),
		OccurredAt: now,
		Severity:   "medium",
		Confidence: 0.70,
	}
	if doc.OriginPublishedAt != nil && !doc.OriginPublishedAt.IsZero() {
		e.OccurredAt = doc.OriginPublishedAt.UTC()
	}

	raw := parseJSONMap(doc.FullText)
	e.Raw = raw

	if title := firstNonEmptyString(
		asString(raw["title"]),
		asString(raw["name"]),
		asString(raw["headline"]),
		asString(raw["summary"]),
	); title != "" {
		e.Title = title
	}
	if summary := firstNonEmptyString(
		asString(raw["description"]),
		asString(raw["summary"]),
		asString(raw["text"]),
	); summary != "" {
		e.Summary = summary
	}
	if url := firstNonEmptyString(asString(raw["url"]), asString(raw["link"]), asString(raw["sourceUrl"])); url != "" {
		e.URL = url
	}
	if t := parseAnyTime(
		raw["publishedAt"],
		raw["published"],
		raw["pubDate"],
		raw["timestamp"],
		raw["time"],
		raw["date"],
		raw["acq_date"],
	); t != nil {
		e.OccurredAt = t.UTC()
	}

	if lat, lon, ok := extractCoordinates(raw); ok {
		e.Lat = lat
		e.Lon = lon
		e.HasCoordinates = true
	}

	countryCode := normalizeCountryCode(firstNonEmptyString(
		asString(raw["countryCode"]),
		asString(raw["country_code"]),
		asString(raw["iso2"]),
		asString(raw["country"]),
		asString(raw["region"]),
	))
	if countryCode == "" {
		countryCode = inferCountryCodeFromText(strings.Join([]string{e.Title, e.Summary}, " "))
	}
	if countryCode == "" && e.HasCoordinates {
		countryCode = inferCountryCodeFromLatLon(e.Lat, e.Lon)
	}
	e.CountryCode = countryCode
	if countryCode != "" {
		e.CountryName = firstNonEmptyString(asString(raw["country"]), countryNameFromCode(countryCode), countryCode)
		e.LocationPath = locationPathFromCountryCode(countryCode)
	}

	e.Severity = normalizeHorizonMonitorSeverity(firstNonEmptyString(
		asString(raw["severity"]),
		asString(raw["level"]),
		asString(raw["priority"]),
		asString(raw["status"]),
	))
	if e.Severity == "" {
		e.Severity = inferSeverityFromPayload(raw, e.FeedType)
	}
	e.Confidence = confidenceFromSeverity(e.Severity)

	classification := normalizeClassificationWithSourceHint(
		classifyHorizonMonitorNewsKeyword(strings.Join([]string{e.Title, e.Summary}, " ")),
		e,
	)
	inferredSignalType := inferHorizonMonitorSignalType(signalHint, e, classification)
	if blueprint, ok := horizonMonitorBlueprintFromSignal(inferredSignalType); ok {
		e.SignalType = blueprint.SignalType
		e.EventType = eventTypeFromSignalType(blueprint.SignalType)
		e.SpherePaths = append([]string{}, blueprint.Sphere...)
	} else {
		e.SignalType = firstNonEmptyString(inferredSignalType, "risk:temporal_anomaly")
		e.EventType = eventTypeFromSignalType(e.SignalType)
		e.SpherePaths = spherePathsFromCategory(e.Category)
	}
	if strings.TrimSpace(e.EventType) == "" {
		e.EventType = "other"
	}

	if e.Summary == "" {
		e.Summary = e.Title
	}
	if len(e.Summary) > 4000 {
		e.Summary = e.Summary[:4000]
	}
	return e
}

func (p *HorizonMonitorPipeline) upsertEventFact(ctx context.Context, tenantID, sourceID string, e *horizonMonitorFeedEvent) (*coremodels.OntologyFact, error) {
	if e == nil {
		return nil, fmt.Errorf("event is nil")
	}
	now := time.Now().UTC()
	id := "hm_evt_" + shortHash(strings.Join([]string{sourceID, e.FeedType, e.ExternalID, e.EventType}, "|"))
	canonicalPath := "/event/horizon-monitor/" + slug(e.FeedType) + "/" + slug(e.ExternalID)
	canonicalPaths := []string{canonicalPath}
	if strings.TrimSpace(e.LocationPath) != "" {
		canonicalPaths = append(canonicalPaths, strings.TrimSpace(e.LocationPath))
	}
	canonicalPaths = uniqueStrings(canonicalPaths)
	var locPtr *string
	if strings.TrimSpace(e.LocationPath) != "" {
		lp := strings.TrimSpace(e.LocationPath)
		locPtr = &lp
	}
	sourceRefID := firstNonEmptyString(strings.TrimSpace(e.URL), strings.TrimSpace(e.ExternalID))
	sourceType := coremodels.SourceTypeWeb
	if strings.TrimSpace(e.URL) == "" {
		sourceType = coremodels.SourceTypeDocument
	}
	f := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:             id,
			TenantID:       tenantID,
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
			CreatedBy:      "horizon_monitor_pipeline",
			UpdatedBy:      "horizon_monitor_pipeline",
			Labels:         map[string]string{"en": e.Title},
			Status:         "active",
			CanonicalPaths: canonicalPaths,
			LocationPath:   locPtr,
			SpherePaths:    uniqueStrings(e.SpherePaths),
			Tags: horizonMonitorFactTags(
				coremodels.FactExtractionLayerHeuristic,
				false,
				false,
				"feed_type:"+strings.TrimSpace(e.FeedType),
				"severity:"+strings.TrimSpace(e.Severity),
			),
		},
		Kind:       "event",
		Type:       strings.TrimSpace(e.EventType),
		Title:      strings.TrimSpace(e.Title),
		Summary:    strings.TrimSpace(e.Summary),
		Confidence: wmClamp01(e.Confidence),
		OccurredAt: &e.OccurredAt,
		ObservedAt: &e.OccurredAt,
		SourceIDs:  uniqueStrings([]string{strings.TrimSpace(sourceID)}),
		SourceRefs: []coremodels.SourceRef{{SourceID: sourceRefID, SourceType: sourceType}},
		ExtractionLineage: horizonMonitorLineage(
			coremodels.FactExtractionLayerHeuristic,
			false,
			false,
		),
	}
	if err := p.factsRepo.Upsert(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

func (p *HorizonMonitorPipeline) buildSignalDescriptor(eventFact *coremodels.OntologyFact, e *horizonMonitorFeedEvent) *horizonmodels.SignalDescriptor {
	if eventFact == nil || e == nil {
		return nil
	}
	if shouldSuppressHeuristicSignalPropagation(e) {
		return nil
	}
	blueprint, ok := horizonMonitorBlueprintFromSignal(strings.TrimSpace(e.SignalType))
	if !ok {
		return nil
	}
	amplitude := amplitudeFromSeverity(e.Severity)
	locationPath := ""
	if eventFact.LocationPath != nil {
		locationPath = strings.TrimSpace(*eventFact.LocationPath)
	}
	signalID := "hm_sig_" + shortHash(strings.Join([]string{eventFact.ID, blueprint.SignalType, locationPath}, "|"))
	seedNames := heuristicSignalSeedNames(e)
	seedPaths := heuristicSignalSeedPaths(e, locationPath)
	s := &horizonmodels.SignalDescriptor{
		ID:           signalID,
		FactID:       strings.TrimSpace(eventFact.ID),
		CreatedAt:    time.Now().UTC(),
		Axis:         blueprint.Axis,
		Mechanism:    blueprint.Mechanism,
		Amplitude:    amplitude,
		Polarity:     "risk",
		LocationPath: locationPath,
		SeedPaths:    seedPaths,
		SpherePaths:  uniqueStrings(append(eventFact.SpherePaths, blueprint.Sphere...)),
		SeedNames:    seedNames,
		Constraints: horizonmodels.SignalConstraints{
			TimeHorizon:     horizonmodels.SignalTimeHorizonShortTerm,
			Timeframe:       horizonmodels.SignalTimeHorizonShortTerm,
			Modality:        horizonmodels.SignalModalityActual,
			Likelihood:      wmClamp01(eventFact.Confidence),
			Impact:          amplitude,
			EscalationLevel: horizonMonitorEscalationLevelFromSeverity(e.Severity),
		},
		Confidence: wmClamp01(eventFact.Confidence),
		SourceRefs: uniqueStrings(eventFact.SourceIDs),
		ObservedAt: e.OccurredAt,
	}
	if err := s.Validate(); err != nil {
		return nil
	}
	return s
}

func shouldSuppressHeuristicSignalPropagation(e *horizonMonitorFeedEvent) bool {
	if e == nil {
		return false
	}
	if signalTypeFromFeedType(strings.TrimSpace(e.FeedType)) != "" {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{e.Title, e.Summary}, " "))
	if (isLikelyStrategicPlanningText(text) || isLikelyRoundupCommentaryText(text)) && !hasDirectDisruptionTrigger(text) {
		return true
	}
	signalType := strings.TrimSpace(e.SignalType)
	if signalType == "" {
		return false
	}
	if signalType != "risk:temporal_anomaly" {
		return false
	}
	if hasAnyKeyword(text, "earthquake", "flood", "wildfire", "storm", "outage", "explosion", "attack", "strike", "missile", "drone") {
		return false
	}
	return true
}

func isLikelyStrategicPlanningText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text,
		"strategy", "cyber strategy", "new strategy", "policy", "doctrine", "roadmap",
		"production", "quadruple production", "ramp up production", "increase production",
		"makers agree", "calls for tougher responses", "by the numbers", "outlook", "analysis",
	) && !hasStrongEventSignal(text) {
		return true
	}
	return false
}

func hasDirectDisruptionTrigger(text string) bool {
	return hasAnyKeyword(text,
		"attack", "attacked", "outage", "explosion", "earthquake", "flood", "wildfire",
		"missile strike", "drone strike", "airstrike", "rocket attack", "shelling",
		"closed airport", "airport closure", "shipping disruption", "port closure", "shutdown",
	)
}

func hasCivilProtestSignal(text string) bool {
	return hasAnyKeyword(text,
		"protest", "demonstration", "rally", "riot", "street march", "marchers",
		"labor strike", "workers strike", "dock strike", "general strike",
	)
}

func hasDefenseIndustrialContext(text string) bool {
	return hasAnyKeyword(text,
		"missile makers", "production line", "munitions", "procurement", "contract award",
		"laser test", "task force", "anti-drone", "air defence", "air defense",
		"interceptor", "fighter", "battery", "defense industrial", "defence industrial",
	)
}

func isLikelyRoundupCommentaryText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	return hasAnyKeyword(text,
		"let s talk about", "did and didn t cover", "this week", "roundup",
		"podcast", "interview", "opinion", "analysis podcast", "bunker talk",
	)
}

func (p *HorizonMonitorPipeline) upsertAndPropagateSignal(
	ctx context.Context,
	tenantID, sourceID string,
	signal *horizonmodels.SignalDescriptor,
	eventFact *coremodels.OntologyFact,
) (*horizonMonitorSignalFlowAudit, error) {
	if signal == nil || eventFact == nil {
		return nil, nil
	}
	waveKey := strings.TrimSpace(signal.ID)
	if p.isWaveThrottled(waveKey) {
		return nil, nil
	}
	audit := &horizonMonitorSignalFlowAudit{
		Axis:              strings.TrimSpace(signal.Axis),
		EscalationLevel:   strings.TrimSpace(signal.Constraints.EscalationLevel),
		LocationOnly:      len(signal.SeedPaths) == 0 && len(signal.SeedNames) == 0 && strings.TrimSpace(signal.LocationPath) != "",
		SemanticSeedNames: len(signal.SeedNames),
	}

	signalFact := horizonMonitorSignalFact(signal, tenantID, sourceID, eventFact)
	if err := p.factsRepo.Upsert(ctx, signalFact); err != nil {
		return audit, err
	}

	if p.seedInjector != nil {
		seedIDs, err := p.seedInjector.Inject(ctx, signal)
		if errors.Is(err, signals.ErrNoSeedNodes) {
			p.markWaveSeen(waveKey)
			return audit, nil
		}
		if err != nil {
			return audit, err
		}
		audit.SeedCount = len(seedIDs)
		audit.ChainRefs = len(signal.ChainRefs)
		if len(seedIDs) > 0 && p.propagationEngine != nil && p.profileLookup != nil {
			audit.PropagationTried = true
			kernelCfg := horizonpropagation.DefaultHeuristicSignalKernelConfig(signal)
			audit.KernelMode = kernelCfg.Mode
			propagationCtx := horizonpropagation.WithSignalKernelConfig(ctx, kernelCfg)
			result, err := p.propagationEngine.PropagateSignalFromSeeds(propagationCtx, signal, p.profileLookup, seedIDs)
			if result != nil {
				audit.AffectedFragments = len(result.AffectedFragments)
				audit.PathsTraced = len(result.PropagationPaths)
				audit.TraceDropped = result.TraceDropped
				audit.TailSelected = int(math.Round(result.Metrics["typed_rwr_selected"]))
				if endpointSummary, scoreErr := horizonpropagation.ScoreInterestingEndpoints(ctx, p.fragmentsRepo, signal, result.PropagationPaths, result.Trace, 5); scoreErr == nil {
					audit.BusinessEndpoints = endpointSummary.BusinessEndpoints
					audit.NonObviousEndpoints = endpointSummary.NonObviousEndpoints
					audit.FacilityEndpoints = endpointSummary.FacilityEndpoints
					audit.RemoteEndpoints = endpointSummary.RemoteEndpoints
					audit.LongPathEndpoints = endpointSummary.LongPathEndpoints
				}
				pathFamilySummary := horizonpropagation.ScorePathFamilies(signal, result.Trace, 4)
				audit.PathFamilies = pathFamilySummary.PathFamilies
				audit.BusinessPathFamilies = pathFamilySummary.BusinessPathFamilies
				audit.CrossBorderFamilies = pathFamilySummary.CrossBorderFamilies
				breakpointSummary := horizonpropagation.AuditTraceBreakpoints(result.Trace, 4)
				audit.Breakpoints = breakpointSummary.PathBreakpoints
				audit.ThresholdBreakpoints = breakpointSummary.ThresholdDrops
				audit.TransferSkips = breakpointSummary.TransferSkips
				audit.HopLimitBreakpoints = breakpointSummary.HopLimitDrops
				audit.BusinessBreakpoints = breakpointSummary.BusinessBreakpoints
			}
			if err != nil {
				return audit, err
			}
		}
	}

	p.markWaveSeen(waveKey)
	return audit, nil
}

func horizonMonitorSignalFact(signal *horizonmodels.SignalDescriptor, tenantID, sourceID string, eventFact *coremodels.OntologyFact) *coremodels.OntologyFact {
	now := time.Now().UTC()
	var locPtr *string
	if strings.TrimSpace(signal.LocationPath) != "" {
		lp := strings.TrimSpace(signal.LocationPath)
		locPtr = &lp
	}
	canonicalPaths := uniqueStrings(signal.SeedPaths)
	if len(canonicalPaths) == 0 && locPtr != nil {
		canonicalPaths = []string{*locPtr}
	}
	etype := "risk:" + strings.TrimSpace(signal.Mechanism)
	if strings.TrimSpace(signal.Mechanism) == "" {
		etype = "risk:" + strings.TrimSpace(signal.Axis)
	}
	eventID := strings.TrimSpace(eventFact.ID)
	return &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:             strings.TrimSpace(signal.ID),
			TenantID:       tenantID,
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
			CreatedBy:      "horizon_monitor_pipeline",
			UpdatedBy:      "horizon_monitor_pipeline",
			Labels:         map[string]string{"en": "Horizon Monitor signal"},
			Status:         "active",
			CanonicalPaths: canonicalPaths,
			LocationPath:   locPtr,
			ActorPaths:     uniqueStrings(signal.ActorPaths),
			SpherePaths:    uniqueStrings(signal.SpherePaths),
			Tags: horizonMonitorFactTags(
				coremodels.FactExtractionLayerSignal,
				false,
				false,
				"signal_axis:"+strings.TrimSpace(signal.Axis),
				"signal_mechanism:"+strings.TrimSpace(signal.Mechanism),
				"signal_type:"+etype,
			),
		},
		Kind:       "signal",
		Type:       etype,
		Title:      "Horizon Monitor signal: " + strings.TrimSpace(signal.Mechanism),
		Summary:    fmt.Sprintf("Amplitude %.2f, axis %s, mechanism %s", signal.Amplitude, signal.Axis, signal.Mechanism),
		Confidence: wmClamp01(signal.Confidence),
		ObservedAt: &signal.ObservedAt,
		EventID:    &eventID,
		SourceIDs:  uniqueStrings([]string{strings.TrimSpace(sourceID)}),
		SourceRefs: eventFact.SourceRefs,
		ExtractionLineage: horizonMonitorLineage(
			coremodels.FactExtractionLayerSignal,
			false,
			false,
		),
	}
}

func (p *HorizonMonitorPipeline) upsertNewsClassificationFact(
	ctx context.Context,
	tenantID, sourceID string,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	techTopics []string,
) error {
	if e == nil {
		return nil
	}
	now := time.Now().UTC()
	id := "hm_news_cls_" + shortHash(strings.Join([]string{sourceID, e.ExternalID}, "|"))
	var locPtr *string
	if strings.TrimSpace(e.LocationPath) != "" {
		lp := strings.TrimSpace(e.LocationPath)
		locPtr = &lp
	}
	canonicalPaths := []string{"/event/horizon-monitor/news/" + slug(e.ExternalID)}
	if locPtr != nil {
		canonicalPaths = append(canonicalPaths, *locPtr)
	}
	geoToken := "global"
	if cc := strings.ToLower(strings.TrimSpace(e.CountryCode)); len(cc) == 2 {
		geoToken = cc
	}
	for _, topic := range uniqueStrings(techTopics) {
		if cp, ok := taxonomy.SegmentCanonicalPath("technology", topic, geoToken); ok {
			canonicalPaths = append(canonicalPaths, cp)
		}
	}
	tags := []string{
		"classification",
		"news_level:" + strings.TrimSpace(cls.Level),
		"news_category:" + strings.TrimSpace(cls.Category),
		"classification_source:" + strings.TrimSpace(cls.Source),
		"processing_mode:" + classificationProcessingMode(cls.Source),
		"llm:" + strconv.FormatBool(strings.EqualFold(strings.TrimSpace(cls.Source), "ai")),
	}
	for _, topic := range uniqueStrings(techTopics) {
		tags = append(tags, "tech_topic:"+strings.TrimSpace(topic))
	}
	f := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:             id,
			TenantID:       tenantID,
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
			CreatedBy:      "horizon_monitor_pipeline",
			UpdatedBy:      "horizon_monitor_pipeline",
			Labels:         map[string]string{"en": e.Title},
			Status:         "active",
			CanonicalPaths: uniqueStrings(canonicalPaths),
			LocationPath:   locPtr,
			SpherePaths:    uniqueStrings(e.SpherePaths),
			Tags:           horizonMonitorFactTags(coremodels.FactExtractionLayerHeuristic, strings.EqualFold(strings.TrimSpace(cls.Source), "ai"), strings.EqualFold(strings.TrimSpace(cls.Source), "ai"), tags...),
		},
		Kind:       "other",
		Type:       "news_classification",
		Title:      e.Title,
		Summary:    fmt.Sprintf("Classified as %s/%s (%s)", cls.Level, cls.Category, cls.Source),
		Confidence: wmClamp01(cls.Confidence),
		OccurredAt: &e.OccurredAt,
		ObservedAt: &now,
		SourceIDs:  uniqueStrings([]string{sourceID}),
		SourceRefs: []coremodels.SourceRef{{SourceID: firstNonEmptyString(e.URL, e.ExternalID), SourceType: coremodels.SourceTypeWeb}},
		ExtractionLineage: horizonMonitorLineage(
			coremodels.FactExtractionLayerHeuristic,
			strings.EqualFold(strings.TrimSpace(cls.Source), "ai"),
			strings.EqualFold(strings.TrimSpace(cls.Source), "ai"),
		),
	}
	return p.factsRepo.Upsert(ctx, f)
}

func (p *HorizonMonitorPipeline) upsertTrackFragment(ctx context.Context, tenantID string, e *horizonMonitorFeedEvent) error {
	if p == nil || p.fragmentsRepo == nil || e == nil {
		return nil
	}
	if !isRealCoordinate(e.Lat, e.Lon, e.HasCoordinates) {
		return nil
	}
	tr, ok := extractTrackObservation(e)
	if !ok {
		return nil
	}
	if !isAllowedTrackObjectType(strings.TrimSpace(tr.ObjectType)) {
		return nil
	}
	if strings.TrimSpace(tenantID) == "" {
		tenantID = "global"
	}
	geoToken := strings.ToLower(strings.TrimSpace(e.CountryCode))
	if len(geoToken) != 2 {
		geoToken = "global"
	}
	trackIdentity := strings.TrimSpace(tr.ExternalTrackID)
	if trackIdentity == "" {
		trackIdentity = "event-" + shortHash(strings.Join([]string{
			strings.TrimSpace(e.SourceID),
			strings.TrimSpace(e.ExternalID),
			strconv.FormatFloat(e.Lat, 'f', 6, 64),
			strconv.FormatFloat(e.Lon, 'f', 6, 64),
		}, "|"))
	}
	baseName := firstNonEmptyString(strings.TrimSpace(tr.DisplayName), trackIdentity, strings.TrimSpace(e.Title), "track")
	canonicalPath := strings.Join([]string{
		"/track",
		string(tr.Kind),
		geoToken,
		slug(baseName) + "-" + shortHash(strings.Join([]string{string(tr.Kind), trackIdentity}, "|")),
	}, "/")
	canonicalPath = strings.ReplaceAll(canonicalPath, "//", "/")

	now := time.Now().UTC()
	iso6709 := formatISO6709(e.Lat, e.Lon)
	location := &horizonmodels.Location{
		Name:    firstNonEmptyString(strings.TrimSpace(e.CountryName), strings.ToUpper(geoToken)),
		ISO6709: &iso6709,
	}
	if len(geoToken) == 2 {
		cc := strings.ToUpper(geoToken)
		location.ISO3166_1 = &cc
	}
	trackData := &horizonmodels.TrackFragmentData{
		Kind:            tr.Kind,
		ObjectType:      strings.TrimSpace(tr.ObjectType),
		ExternalTrackID: trackIdentity,
		SourceID:        strings.TrimSpace(e.SourceID),
		Lat:             e.Lat,
		Lon:             e.Lon,
		HeadingDeg:      tr.HeadingDeg,
		SpeedKts:        tr.SpeedKts,
		AltitudeM:       tr.AltitudeM,
		DepthM:          tr.DepthM,
		LastSeenAt:      e.OccurredAt,
		Confidence:      wmClamp01(e.Confidence),
		GeoHash:         encodeGeoHash(e.Lat, e.Lon, 7),
	}
	if trackData.LastSeenAt.IsZero() {
		trackData.LastSeenAt = now
	}

	incoming := &horizonmodels.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: canonicalPath,
		Type:          horizonmodels.FragmentTypeTrack,
		Labels:        map[string]string{"en": baseName},
		Tags: uniqueStrings([]string{
			"horizon-monitor",
			"track",
			"source_layer:rss",
			"source_mechanic:rss",
			"processing_layer:heuristic",
			"extraction_pipeline:horizon_monitor",
			"extraction_current:heuristic",
			"llm_enabled:false",
			"llm_used:false",
			"track_kind:" + string(tr.Kind),
			"track_object:" + strings.TrimSpace(tr.ObjectType),
		}),
		Status:      string(horizonmodels.FragmentStatusActive),
		State:       map[string]horizonmodels.MetricValue{},
		Identifiers: map[string]string{"externalTrackId": trackIdentity, "sourceObjectId": trackIdentity},
		Qualifiers: map[string]interface{}{
			"track": map[string]interface{}{
				"kind":       string(tr.Kind),
				"objectType": strings.TrimSpace(tr.ObjectType),
			},
			"extraction": horizonMonitorFragmentExtraction("heuristic", false),
		},
		HomeLocation: location,
		Track:        trackData,
		Provenance: horizonmodels.Provenance{
			Method:    "horizon_monitor_pipeline",
			SourceIDs: uniqueStrings([]string{strings.TrimSpace(e.SourceID)}),
			ObservedAt: func() *time.Time {
				t := e.OccurredAt
				if t.IsZero() {
					t = now
				}
				return &t
			}(),
		},
		Metadata: horizonmodels.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			LastVerifiedAt:  now,
			EpistemicStatus: horizonmodels.EpistemicStatusLowConfidence,
		},
		Governance: horizonmodels.Governance{
			Owner:  "SYSTEM",
			Status: string(horizonmodels.FragmentStatusActive),
		},
	}

	existing, err := p.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
	if err != nil && !errors.Is(err, horizonrepo.ErrFragmentNotFound) {
		return err
	}
	if existing == nil {
		_, err = p.fragmentsRepo.Create(ctx, incoming)
		return err
	}
	if !mergeTrackFragment(existing, incoming) {
		return nil
	}
	return p.fragmentsRepo.Update(ctx, existing)
}

func mergeTrackFragment(dst, src *horizonmodels.HorizonFragment) bool {
	if dst == nil || src == nil {
		return false
	}
	changed := false
	if dst.Type != horizonmodels.FragmentTypeTrack {
		dst.Type = horizonmodels.FragmentTypeTrack
		changed = true
	}
	if dst.Labels == nil {
		dst.Labels = map[string]string{}
	}
	if v := strings.TrimSpace(src.Labels["en"]); v != "" && strings.TrimSpace(dst.Labels["en"]) != v {
		dst.Labels["en"] = v
		changed = true
	}
	mergedTags := uniqueStrings(append(dst.Tags, src.Tags...))
	if strings.Join(dst.Tags, "|") != strings.Join(mergedTags, "|") {
		dst.Tags = mergedTags
		changed = true
	}
	if dst.Identifiers == nil {
		dst.Identifiers = map[string]string{}
	}
	for k, v := range src.Identifiers {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		if strings.TrimSpace(dst.Identifiers[k]) != v {
			dst.Identifiers[k] = v
			changed = true
		}
	}
	if src.HomeLocation != nil {
		dst.HomeLocation = src.HomeLocation
		changed = true
	}
	if src.Track != nil {
		dst.Track = src.Track
		changed = true
	}
	if dst.Metadata.LastUpdatedAt.Before(src.Metadata.LastUpdatedAt) {
		dst.Metadata.LastUpdatedAt = src.Metadata.LastUpdatedAt
	}
	return changed
}

func (p *HorizonMonitorPipeline) upsertAffectedZoneFragmentCandidate(
	ctx context.Context,
	tenantID, sourceID string,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	candidate *affectedZoneCandidate,
) error {
	if p == nil || p.fragmentsRepo == nil || e == nil {
		return nil
	}
	if candidate == nil {
		return nil
	}
	now := time.Now().UTC()
	matchedAt := e.OccurredAt.UTC()
	if matchedAt.IsZero() {
		matchedAt = now
	}
	boost := affectedZoneBoost(e, cls, candidate)

	existing, err := p.fragmentsRepo.GetByCanonicalPath(ctx, candidate.CanonicalPath)
	if err != nil && !errors.Is(err, horizonrepo.ErrFragmentNotFound) {
		return err
	}
	if existing == nil {
		if matched := p.findDeterministicAffectedZoneMatch(ctx, candidate); matched != nil {
			existing = matched
		}
	}
	if existing == nil {
		if matched := p.findSemanticAffectedZoneMatch(ctx, candidate, e); matched != nil {
			existing = matched
		}
	}
	if existing == nil {
		if candidate.WarmOnly {
			return nil
		}
		incoming := buildIncomingAffectedZoneFragment(candidate, sourceID, e, cls, now, matchedAt, boost)
		createdID, createErr := p.fragmentsRepo.Create(ctx, incoming)
		if createdID != "" {
			incoming.ID = createdID
		}
		if createErr == nil {
			p.publishAffectedZoneUpsert(ctx, tenantID, incoming, sourceID, "created")
		}
		return createErr
	}
	if !mergeAffectedZoneFragment(existing, candidate, sourceID, e, cls, now, matchedAt, boost) {
		return nil
	}
	if err := p.fragmentsRepo.Update(ctx, existing); err != nil {
		return err
	}
	p.publishAffectedZoneUpsert(ctx, tenantID, existing, sourceID, "updated")
	return nil
}

func (p *HorizonMonitorPipeline) findDeterministicAffectedZoneMatch(
	ctx context.Context,
	candidate *affectedZoneCandidate,
) *horizonmodels.HorizonFragment {
	if p == nil || p.fragmentsRepo == nil || candidate == nil {
		return nil
	}
	prefix := "/zone/" + strings.ToLower(strings.TrimSpace(candidate.GeoToken)) + "/" + strings.ToLower(strings.TrimSpace(candidate.ConditionType)) + "/"
	if !strings.HasPrefix(prefix, "/zone/") || strings.HasSuffix(prefix, "//") {
		return nil
	}
	fragments, err := p.fragmentsRepo.ListByCanonicalPathPrefix(ctx, prefix, 48)
	if err != nil || len(fragments) == 0 {
		return nil
	}
	return selectDeterministicAffectedZoneMatch(candidate, fragments)
}

func (p *HorizonMonitorPipeline) findSemanticAffectedZoneMatch(
	ctx context.Context,
	candidate *affectedZoneCandidate,
	event *horizonMonitorFeedEvent,
) *horizonmodels.HorizonFragment {
	if p == nil || p.fragmentsRepo == nil || p.vertexSearch == nil || !p.vertexSearch.IsConfigured() || candidate == nil {
		return nil
	}
	queryParts := []string{
		strings.TrimSpace(candidate.Label),
		strings.TrimSpace(candidate.ConditionType),
		strings.TrimSpace(candidate.ZoneSubType),
		strings.TrimSpace(candidate.CountryName),
	}
	if event != nil {
		queryParts = append(queryParts, strings.TrimSpace(event.Title), strings.TrimSpace(event.Summary))
	}
	query := strings.TrimSpace(strings.Join(queryParts, " "))
	if query == "" {
		return nil
	}
	if len(query) > 800 {
		query = query[:800]
	}
	resp, err := p.vertexSearch.Search(ctx, &ai.SearchRequest{
		Query:    query,
		PageSize: 16,
	})
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return nil
	}
	bestPath, bestScore := selectSemanticAffectedZoneCanonicalPath(candidate, resp.Results)
	if bestPath == "" || bestScore < 0.88 {
		return nil
	}
	f, err := p.fragmentsRepo.GetByCanonicalPath(ctx, bestPath)
	if err != nil || f == nil || f.Type != horizonmodels.FragmentTypeAffectedZone {
		return nil
	}
	return f
}

func selectSemanticAffectedZoneCanonicalPath(candidate *affectedZoneCandidate, results []ai.SearchResult) (string, float64) {
	if candidate == nil || len(results) == 0 {
		return "", 0
	}
	wantGeoToken := strings.ToLower(strings.TrimSpace(candidate.GeoToken))
	wantCountry := strings.ToLower(strings.TrimSpace(candidate.CountryCode))
	wantCondition := strings.ToLower(strings.TrimSpace(candidate.ConditionType))
	wantLabel := normalizePhrase(strings.TrimSpace(candidate.Label))
	bestPath := ""
	bestScore := 0.0
	for _, r := range results {
		cp := ""
		if r.Document.StructData != nil {
			cp = strings.TrimSpace(asString(r.Document.StructData["canonicalPath"]))
		}
		if cp == "" {
			continue
		}
		geo, condition, slug, ok := parseZoneCanonicalPath(cp)
		if !ok {
			continue
		}
		base := r.RelevanceScore
		if base <= 0 {
			continue
		}
		geoOK, geoBonus := semanticZoneGeoScore(wantGeoToken, wantCountry, geo)
		if !geoOK {
			continue
		}
		score := base + geoBonus
		if wantCondition != "" {
			if condition != wantCondition {
				continue
			}
			score += 0.10
		}
		if wantLabel != "" && slug != "" {
			slugNorm := normalizePhrase(strings.ReplaceAll(slug, "-", " "))
			if slugNorm == wantLabel {
				score += 0.10
			} else if strings.Contains(wantLabel, slugNorm) || strings.Contains(slugNorm, wantLabel) {
				score += 0.06
			}
		}
		if score > bestScore {
			bestScore = score
			bestPath = cp
		}
	}
	return bestPath, bestScore
}

func selectDeterministicAffectedZoneMatch(candidate *affectedZoneCandidate, fragments []*horizonmodels.HorizonFragment) *horizonmodels.HorizonFragment {
	if candidate == nil || len(fragments) == 0 {
		return nil
	}
	wantLabel := normalizePhrase(strings.TrimSpace(candidate.Label))
	wantSubtype := normalizePhrase(strings.TrimSpace(candidate.ZoneSubType))
	bestScore := 0.0
	var best *horizonmodels.HorizonFragment
	for _, frag := range fragments {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
			continue
		}
		geo, condition, slugPath, ok := parseZoneCanonicalPath(strings.TrimSpace(frag.CanonicalPath))
		if !ok || condition != strings.ToLower(strings.TrimSpace(candidate.ConditionType)) {
			continue
		}
		score := 0.25
		if geo == strings.ToLower(strings.TrimSpace(candidate.GeoToken)) {
			score += 0.25
		}
		if frag.Qualifiers != nil {
			if subtype := normalizePhrase(strings.TrimSpace(asString(frag.Qualifiers["zoneSubType"]))); subtype != "" && subtype == wantSubtype {
				score += 0.14
			}
			if precision := strings.TrimSpace(asString(frag.Qualifiers["geoPrecision"])); precision == "precise" && strings.EqualFold(candidate.GeoPrecision, "precise") {
				score += 0.06
			}
		}
		slugNorm := normalizePhrase(strings.ReplaceAll(slugPath, "-", " "))
		labelNorm := normalizePhrase(strings.TrimSpace(frag.Labels["en"]))
		if wantLabel != "" {
			switch {
			case slugNorm == wantLabel || labelNorm == wantLabel:
				score += 0.26
			case slugNorm != "" && (strings.Contains(wantLabel, slugNorm) || strings.Contains(slugNorm, wantLabel)):
				score += 0.16
			case labelNorm != "" && (strings.Contains(wantLabel, labelNorm) || strings.Contains(labelNorm, wantLabel)):
				score += 0.12
			}
		}
		if frag.Metadata.LastUpdatedAt.After(time.Now().UTC().Add(-72 * time.Hour)) {
			score += 0.05
		}
		if score > bestScore {
			bestScore = score
			best = frag
		}
	}
	if bestScore < 0.52 {
		return nil
	}
	return best
}

func semanticZoneGeoScore(wantGeoToken, wantCountry, zoneGeo string) (bool, float64) {
	zoneGeo = strings.ToLower(strings.TrimSpace(zoneGeo))
	if zoneGeo == "" {
		return false, 0
	}
	if wantCountry != "" {
		if zoneGeo == wantCountry {
			return true, 0.14
		}
		if strings.Contains(zoneGeo, "-") {
			parts := strings.Split(zoneGeo, "-")
			for _, p := range parts {
				if strings.TrimSpace(p) == wantCountry {
					return true, 0.10
				}
			}
		}
		if zoneGeo == "global" {
			return true, 0.03
		}
		return false, 0
	}
	if wantGeoToken != "" {
		if zoneGeo == wantGeoToken {
			return true, 0.10
		}
		if wantGeoToken == "global" {
			return true, 0.02
		}
	}
	return true, 0
}

func parseZoneCanonicalPath(cp string) (geo, condition, slug string, ok bool) {
	cp = strings.TrimSpace(strings.ToLower(cp))
	if !strings.HasPrefix(cp, "/zone/") {
		return "", "", "", false
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) != 4 || parts[0] != "zone" {
		return "", "", "", false
	}
	geo = strings.TrimSpace(parts[1])
	condition = strings.TrimSpace(parts[2])
	slug = strings.TrimSpace(parts[3])
	if geo == "" || condition == "" || slug == "" {
		return "", "", "", false
	}
	return geo, condition, slug, true
}

func buildIncomingAffectedZoneFragment(
	c *affectedZoneCandidate,
	sourceID string,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	now, matchedAt time.Time,
	boost float64,
) *horizonmodels.HorizonFragment {
	state := affectedZoneState{
		LastUpdatedAt: now,
	}
	state.applyObservation(now, matchedAt, boost)

	incoming := &horizonmodels.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: c.CanonicalPath,
		Type:          horizonmodels.FragmentTypeAffectedZone,
		Labels:        map[string]string{"en": c.Label},
		Description:   strings.TrimSpace(e.Title),
		Tags: affectedZoneTags(c, cls, e).
			Merge(),
		Status:      string(horizonmodels.FragmentStatusActive),
		State:       map[string]horizonmodels.MetricValue{},
		Qualifiers:  affectedZoneQualifiers(c, cls, state, e),
		Identifiers: map[string]string{"sourceObjectId": strings.TrimSpace(e.ExternalID)},
		RadiusKm:    c.RadiusKm,
		Provenance: horizonmodels.Provenance{
			Method:    "heuristic_rss_zone",
			SourceIDs: uniqueStrings([]string{strings.TrimSpace(sourceID)}),
			SourceRefs: []horizonmodels.SourceRef{
				{SourceID: firstNonEmptyString(strings.TrimSpace(e.URL), strings.TrimSpace(e.ExternalID)), SourceType: "rss"},
			},
			ObservedAt: &matchedAt,
		},
		Metadata: horizonmodels.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			LastVerifiedAt:  now,
			EpistemicStatus: horizonmodels.EpistemicStatusLowConfidence,
		},
		Governance: horizonmodels.Governance{
			Owner:  "SYSTEM",
			Status: string(horizonmodels.FragmentStatusActive),
		},
	}
	applyAffectedZoneState(incoming, state, now, matchedAt, sourceID, e)
	applyAffectedZoneCoordinates(incoming, c)
	return incoming
}

func mergeAffectedZoneFragment(
	dst *horizonmodels.HorizonFragment,
	c *affectedZoneCandidate,
	sourceID string,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	now, matchedAt time.Time,
	boost float64,
) bool {
	if dst == nil || c == nil {
		return false
	}
	if dst.Type != horizonmodels.FragmentTypeAffectedZone {
		return false
	}
	changed := false

	if dst.Labels == nil {
		dst.Labels = map[string]string{}
	}
	if strings.TrimSpace(dst.Labels["en"]) == "" && strings.TrimSpace(c.Label) != "" {
		dst.Labels["en"] = strings.TrimSpace(c.Label)
		changed = true
	}
	if title := strings.TrimSpace(e.Title); title != "" && strings.TrimSpace(dst.Description) != title {
		dst.Description = title
		changed = true
	}

	mergedTags := uniqueStrings(append(dst.Tags, affectedZoneTags(c, cls, e).items...))
	if strings.Join(mergedTags, "|") != strings.Join(dst.Tags, "|") {
		dst.Tags = mergedTags
		changed = true
	}

	if c.RadiusKm > 0 {
		if dst.RadiusKm <= 0 || c.RadiusKm > dst.RadiusKm {
			dst.RadiusKm = c.RadiusKm
			changed = true
		}
	}
	if applyAffectedZoneCoordinates(dst, c) {
		changed = true
	}

	if dst.Identifiers == nil {
		dst.Identifiers = map[string]string{}
	}
	if ext := strings.TrimSpace(e.ExternalID); ext != "" && strings.TrimSpace(dst.Identifiers["sourceObjectId"]) != ext {
		dst.Identifiers["sourceObjectId"] = ext
		changed = true
	}

	st := parseAffectedZoneState(dst)
	st.applyDecay(now)
	prevScore := st.Score
	prevHits1h := st.Hits1h
	prevHits24h := st.Hits24h
	prevMentions := st.TotalMentions
	st.applyObservation(now, matchedAt, boost)
	if st.Score != prevScore || st.Hits1h != prevHits1h || st.Hits24h != prevHits24h || st.TotalMentions != prevMentions {
		applyAffectedZoneState(dst, st, now, matchedAt, sourceID, e)
		changed = true
	}
	if dst.Qualifiers == nil {
		dst.Qualifiers = map[string]interface{}{}
	}
	for k, v := range affectedZoneQualifiers(c, cls, st, e) {
		if k == "" {
			continue
		}
		if fmt.Sprintf("%v", dst.Qualifiers[k]) != fmt.Sprintf("%v", v) {
			dst.Qualifiers[k] = v
			changed = true
		}
	}

	if dst.Provenance.SourceIDs == nil {
		dst.Provenance.SourceIDs = []string{}
	}
	mergedSourceIDs := uniqueStrings(append(dst.Provenance.SourceIDs, strings.TrimSpace(sourceID)))
	if strings.Join(mergedSourceIDs, "|") != strings.Join(dst.Provenance.SourceIDs, "|") {
		dst.Provenance.SourceIDs = mergedSourceIDs
		changed = true
	}
	if matchedAtPtr := &matchedAt; dst.Provenance.ObservedAt == nil || matchedAt.After(dst.Provenance.ObservedAt.UTC()) {
		dst.Provenance.ObservedAt = matchedAtPtr
		changed = true
	}
	if method := "heuristic_rss_zone"; strings.TrimSpace(dst.Provenance.Method) != method {
		dst.Provenance.Method = method
		changed = true
	}
	if refID := firstNonEmptyString(strings.TrimSpace(e.URL), strings.TrimSpace(e.ExternalID)); refID != "" {
		exists := false
		for _, ref := range dst.Provenance.SourceRefs {
			if strings.TrimSpace(ref.SourceID) == refID && strings.TrimSpace(ref.SourceType) == "rss" {
				exists = true
				break
			}
		}
		if !exists {
			dst.Provenance.SourceRefs = append(dst.Provenance.SourceRefs, horizonmodels.SourceRef{SourceID: refID, SourceType: "rss"})
			changed = true
		}
	}

	if dst.Metadata.CreatedAt.IsZero() {
		dst.Metadata.CreatedAt = now
		changed = true
	}
	if dst.Metadata.LastVerifiedAt.Before(now) {
		dst.Metadata.LastVerifiedAt = now
		changed = true
	}
	if dst.Metadata.LastUpdatedAt.Before(now) {
		dst.Metadata.LastUpdatedAt = now
		changed = true
	}
	if dst.Governance.Owner == "" {
		dst.Governance.Owner = "SYSTEM"
		changed = true
	}
	if strings.TrimSpace(dst.Governance.Status) == "" {
		dst.Governance.Status = string(horizonmodels.FragmentStatusActive)
		changed = true
	}
	if strings.TrimSpace(dst.Status) == "" {
		dst.Status = string(horizonmodels.FragmentStatusActive)
		changed = true
	}

	return changed
}

func (p *HorizonMonitorPipeline) publishAffectedZoneUpsert(
	ctx context.Context,
	tenantID string,
	f *horizonmodels.HorizonFragment,
	sourceID string,
	operation string,
) {
	if p == nil || p.realtimePublisher == nil || f == nil || f.Type != horizonmodels.FragmentTypeAffectedZone {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	score := 0.0
	lastMatchUnix := float64(0)
	if f.State != nil {
		if mv, ok := f.State[zoneMetricHypeScore]; ok {
			score = clamp(mv.Value, 0, 100)
		}
		if mv, ok := f.State[zoneMetricLastMatchTS]; ok {
			lastMatchUnix = mv.Value
		}
	}
	lastMatchedAt := ""
	if lastMatchUnix > 0 {
		lastMatchedAt = time.Unix(int64(lastMatchUnix), 0).UTC().Format(time.RFC3339)
	}
	qualifierString := func(key string) string {
		if f.Qualifiers == nil || strings.TrimSpace(key) == "" {
			return ""
		}
		v, ok := f.Qualifiers[key]
		if !ok || v == nil {
			return ""
		}
		switch t := v.(type) {
		case string:
			return strings.TrimSpace(t)
		default:
			s := strings.TrimSpace(fmt.Sprintf("%v", t))
			if s == "<nil>" {
				return ""
			}
			return s
		}
	}
	payload := map[string]interface{}{
		"tenantId":      tenantID,
		"fragmentId":    strings.TrimSpace(f.ID),
		"canonicalPath": strings.TrimSpace(f.CanonicalPath),
		"type":          string(f.Type),
		"label":         strings.TrimSpace(f.Labels["en"]),
		"status":        strings.TrimSpace(f.Status),
		"epistemicStatus": func() string {
			return strings.TrimSpace(string(f.Metadata.EpistemicStatus))
		}(),
		"version":            f.Version,
		"operation":          strings.TrimSpace(operation),
		"processingMode":     "heuristic",
		"sourceLayer":        "rss",
		"processingLayer":    "heuristic",
		"extractionPipeline": "horizon_monitor",
		"extractionCurrent":  "heuristic",
		"llm":                false,
		"sourceId":           strings.TrimSpace(sourceID),
		"radiusKm":           f.RadiusKm,
		"zoneType":           qualifierString("zoneType"),
		"zoneSubType":        qualifierString("zoneSubType"),
		"geoPrecision":       qualifierString("geoPrecision"),
		"warmOnly":           strings.EqualFold(qualifierString("warmOnly"), "true"),
		"hype":               score,
		"hypeIndicator":      hypeIndicator(score),
		"activityStatus":     activityStatus(score),
		"lastMatchedAt":      lastMatchedAt,
		"updatedAt":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	if c := f.Coordinates; c != nil && horizonmodels.IsRealGeoCoordinate(c.Lat, c.Lon) {
		payload["coordinates"] = map[string]float64{"lat": c.Lat, "lon": c.Lon}
	}
	if strings.TrimSpace(payload["label"].(string)) == "" {
		payload["label"] = strings.TrimSpace(f.CanonicalPath)
	}
	if strings.TrimSpace(payload["status"].(string)) == "" {
		payload["status"] = strings.TrimSpace(f.Governance.Status)
	}
	_ = p.realtimePublisher.PublishJSON(ctx, realtime.EventType("horizon.graph.node.upserted"), tenantID, payload)
}

func applyAffectedZoneCoordinates(f *horizonmodels.HorizonFragment, c *affectedZoneCandidate) bool {
	if f == nil || c == nil {
		return false
	}
	if !c.HasCoordinates || !horizonmodels.IsRealGeoCoordinate(c.Lat, c.Lon) {
		return false
	}
	changed := false
	if f.Coordinates == nil || !horizonmodels.IsRealGeoCoordinate(f.Coordinates.Lat, f.Coordinates.Lon) ||
		math.Abs(f.Coordinates.Lat-c.Lat) > 0.0001 || math.Abs(f.Coordinates.Lon-c.Lon) > 0.0001 {
		f.Coordinates = &horizonmodels.Coordinates{Lat: c.Lat, Lon: c.Lon}
		changed = true
	}
	iso := formatISO6709(c.Lat, c.Lon)
	if f.HomeLocation == nil {
		f.HomeLocation = &horizonmodels.Location{}
	}
	if strings.TrimSpace(f.HomeLocation.Name) == "" && strings.TrimSpace(c.Label) != "" {
		f.HomeLocation.Name = strings.TrimSpace(c.Label)
		changed = true
	}
	if f.HomeLocation.ISO6709 == nil || strings.TrimSpace(*f.HomeLocation.ISO6709) != iso {
		isoCopy := iso
		f.HomeLocation.ISO6709 = &isoCopy
		changed = true
	}
	if cc := strings.ToUpper(strings.TrimSpace(c.CountryCode)); len(cc) == 2 {
		if f.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*f.HomeLocation.ISO3166_1) != cc {
			ccCopy := cc
			f.HomeLocation.ISO3166_1 = &ccCopy
			changed = true
		}
	}
	if !f.HasCoordinates {
		f.HasCoordinates = true
		changed = true
	}
	return changed
}

func applyAffectedZoneState(
	f *horizonmodels.HorizonFragment,
	st affectedZoneState,
	now, matchedAt time.Time,
	sourceID string,
	e *horizonMonitorFeedEvent,
) {
	if f == nil {
		return
	}
	if f.State == nil {
		f.State = map[string]horizonmodels.MetricValue{}
	}
	putMetric := func(name string, value, confidence float64, observedAt time.Time) {
		f.State[name] = horizonmodels.MetricValue{
			Value:      value,
			Confidence: confidence,
			ObservedAt: observedAt,
			Provenance: horizonmodels.Provenance{
				Method:    "heuristic_rss_zone",
				SourceIDs: uniqueStrings([]string{strings.TrimSpace(sourceID)}),
				SourceRefs: []horizonmodels.SourceRef{
					{SourceID: firstNonEmptyString(strings.TrimSpace(e.URL), strings.TrimSpace(e.ExternalID)), SourceType: "rss"},
				},
				ObservedAt: &observedAt,
			},
		}
	}
	putMetric(zoneMetricHypeScore, clamp(st.Score, 0, 100), 0.94, now)
	putMetric(zoneMetricHits1h, clamp(st.Hits1h, 0, 10000), 0.94, now)
	putMetric(zoneMetricHits24h, clamp(st.Hits24h, 0, 50000), 0.94, now)
	putMetric(zoneMetricTotalMentions, clamp(st.TotalMentions, 0, 1e9), 0.92, now)
	putMetric(zoneMetricLastBoost, clamp(st.LastBoost, 0, 100), 0.90, now)
	putMetric(zoneMetricLastMatchTS, float64(matchedAt.Unix()), 0.98, now)
	// For compatibility with generic zone semantics in Horizon profiles.
	putMetric("zone_severity_score", clamp(st.Score/10.0, 0, 10), 0.90, now)
}

func parseAffectedZoneState(f *horizonmodels.HorizonFragment) affectedZoneState {
	st := affectedZoneState{}
	if f == nil {
		return st
	}
	for key, mv := range f.State {
		switch strings.TrimSpace(key) {
		case zoneMetricHypeScore:
			st.Score = mv.Value
		case zoneMetricHits1h:
			st.Hits1h = mv.Value
		case zoneMetricHits24h:
			st.Hits24h = mv.Value
		case zoneMetricTotalMentions:
			st.TotalMentions = mv.Value
		case zoneMetricLastBoost:
			st.LastBoost = mv.Value
		case zoneMetricLastMatchTS:
			if mv.Value > 0 {
				st.LastMatchedAt = time.Unix(int64(mv.Value), 0).UTC()
			}
		}
	}
	st.LastUpdatedAt = f.Metadata.LastUpdatedAt.UTC()
	if st.LastUpdatedAt.IsZero() {
		st.LastUpdatedAt = f.UpdatedAt.UTC()
	}
	if st.LastUpdatedAt.IsZero() {
		st.LastUpdatedAt = time.Now().UTC()
	}
	if st.LastMatchedAt.IsZero() {
		st.LastMatchedAt = st.LastUpdatedAt
	}
	return st
}

func (s *affectedZoneState) applyDecay(now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	deltaH := now.Sub(s.LastUpdatedAt).Hours()
	if deltaH <= 0 {
		return
	}
	s.Score = decayExp(s.Score, deltaH, zoneHalfLifeScoreH)
	s.Hits1h = decayExp(s.Hits1h, deltaH, zoneHalfLife1hH)
	s.Hits24h = decayExp(s.Hits24h, deltaH, zoneHalfLife24hH)
	s.LastUpdatedAt = now
}

func (s *affectedZoneState) applyObservation(now, matchedAt time.Time, boost float64) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if matchedAt.IsZero() {
		matchedAt = now
	}
	s.Score = clamp(s.Score+boost, 0, 100)
	s.Hits1h = clamp(s.Hits1h+(0.6+boost*0.25), 0, 10000)
	s.Hits24h = clamp(s.Hits24h+(1.0+boost*0.35), 0, 50000)
	s.TotalMentions = clamp(s.TotalMentions+1, 0, 1e9)
	s.LastBoost = boost
	s.LastMatchedAt = matchedAt.UTC()
	s.LastUpdatedAt = now.UTC()
}

func affectedZoneBoost(e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification, c *affectedZoneCandidate) float64 {
	levelW := map[string]float64{
		"critical": 1.45,
		"high":     1.20,
		"medium":   1.0,
		"low":      0.75,
		"info":     0.55,
	}
	severityW := map[string]float64{
		"critical": 1.3,
		"high":     1.1,
		"medium":   1.0,
		"low":      0.8,
	}
	base := 8.0 * firstPositive(levelW[strings.ToLower(strings.TrimSpace(cls.Level))], 0.6)
	base *= firstPositive(severityW[strings.ToLower(strings.TrimSpace(e.Severity))], 1.0)
	switch strings.TrimSpace(c.ConditionType) {
	case "military_conflict":
		base *= 1.15
	case "earthquake_affected", "flood_zone", "wildfire_zone":
		base *= 1.08
	case "humanitarian_crisis":
		base *= 1.04
	case "cyber_disruption", "infrastructure_disruption":
		base *= 0.96
	}
	if e.HasCoordinates && horizonmodels.IsRealGeoCoordinate(e.Lat, e.Lon) {
		base *= 1.05
	}
	if c != nil && c.WarmOnly {
		base *= 0.72
	}
	return clamp(base, 2.0, 22.0)
}

func affectedZoneQualifiers(
	c *affectedZoneCandidate,
	cls horizonMonitorNewsClassification,
	st affectedZoneState,
	e *horizonMonitorFeedEvent,
) map[string]interface{} {
	out := map[string]interface{}{
		"conditionType":          strings.TrimSpace(c.ConditionType),
		"zoneType":               strings.TrimSpace(c.ZoneType),
		"zoneSubType":            strings.TrimSpace(c.ZoneSubType),
		"classificationCategory": strings.TrimSpace(cls.Category),
		"classificationLevel":    strings.TrimSpace(cls.Level),
		"classificationSource":   strings.TrimSpace(cls.Source),
		"processingMode":         "heuristic",
		"llm":                    false,
		"sourceLayer":            "rss",
		"processingLayer":        "heuristic",
		"geoPrecision":           strings.TrimSpace(c.GeoPrecision),
		"warmOnly":               c.WarmOnly,
		"hypeStatus":             activityStatus(st.Score),
		"hypeIndicator":          hypeIndicator(st.Score),
		"lastMatchedAt":          st.LastMatchedAt.UTC().Format(time.RFC3339),
		"extraction":             horizonMonitorFragmentExtraction("heuristic", false),
	}
	if strings.TrimSpace(c.Reason) != "" {
		out["reason"] = strings.TrimSpace(c.Reason)
	}
	if e != nil && strings.TrimSpace(e.URL) != "" {
		out["sourceUrl"] = strings.TrimSpace(e.URL)
	}
	return out
}

type tagSet struct {
	items []string
}

func (t tagSet) Merge() []string { return uniqueStrings(t.items) }

func affectedZoneTags(c *affectedZoneCandidate, cls horizonMonitorNewsClassification, e *horizonMonitorFeedEvent) tagSet {
	tags := []string{
		"horizon-monitor",
		"affected-zone",
		"source_layer:rss",
		"source_mechanic:rss",
		"processing_mode:heuristic",
		"processing_layer:heuristic",
		"extraction_pipeline:horizon_monitor",
		"extraction_current:heuristic",
		"llm_enabled:false",
		"llm_used:false",
		"llm:false",
		"geo_precision:" + strings.TrimSpace(c.GeoPrecision),
		"warm_only:" + strconv.FormatBool(c.WarmOnly),
		"condition_type:" + strings.TrimSpace(c.ConditionType),
		"zone_type:" + strings.TrimSpace(c.ZoneType),
		"zone_subtype:" + strings.TrimSpace(c.ZoneSubType),
		"news_level:" + strings.TrimSpace(cls.Level),
		"news_category:" + strings.TrimSpace(cls.Category),
		"classification_source:" + strings.TrimSpace(cls.Source),
		"country:" + strings.ToLower(strings.TrimSpace(c.CountryCode)),
	}
	if e != nil {
		if ft := strings.TrimSpace(e.FeedType); ft != "" {
			tags = append(tags, "feed_type:"+slug(ft))
		}
		if cat := strings.TrimSpace(e.Category); cat != "" {
			tags = append(tags, "source_category:"+slug(cat))
		}
	}
	return tagSet{items: tags}
}

func inferAffectedZoneCandidate(e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification) *affectedZoneCandidate {
	if e == nil {
		return nil
	}
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(e.Title),
		strings.TrimSpace(e.Summary),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	if text == "" {
		return nil
	}
	if isLikelyNonEventText(text) {
		return nil
	}
	conditionType, zoneType, zoneSubType := inferZoneConditionAndSubtype(text, cls)
	if conditionType == "" {
		return nil
	}
	warmOnly := !shouldCreateAffectedZone(text, cls.Level, conditionType)

	geoToken, countryCode, countryName := inferZoneGeo(e)
	place := inferZonePlaceName(e)
	if strings.TrimSpace(place) == "" {
		place = firstNonEmptyString(countryName, countryCode, "emerging hotspot")
	}
	pathSlug := canonicalPathSlug(place)
	if pathSlug == "" || pathSlug == "na" {
		pathSlug = canonicalPathSlug(firstNonEmptyString(countryName, countryCode, "emerging-hotspot"))
	}
	if pathSlug == "" || pathSlug == "na" {
		pathSlug = "emerging-hotspot"
	}

	label := humanizeZoneLabel(place, countryName, zoneSubType)
	if strings.TrimSpace(label) == "" {
		label = firstNonEmptyString(countryName, strings.ToUpper(countryCode), "Affected Zone")
	}
	lat, lon, hasCoord, geoPrecision := inferAffectedZoneCoordinates(e, place, countryCode)

	return &affectedZoneCandidate{
		CanonicalPath:  "/zone/" + geoToken + "/" + conditionType + "/" + pathSlug,
		Label:          label,
		ConditionType:  conditionType,
		ZoneType:       zoneType,
		ZoneSubType:    zoneSubType,
		GeoToken:       geoToken,
		CountryCode:    strings.ToUpper(strings.TrimSpace(countryCode)),
		CountryName:    strings.TrimSpace(countryName),
		RadiusKm:       inferZoneRadiusKm(conditionType, cls.Level, e.Severity),
		Lat:            lat,
		Lon:            lon,
		HasCoordinates: hasCoord,
		GeoPrecision:   geoPrecision,
		WarmOnly:       warmOnly,
		Reason:         "keyword_heuristic",
	}
}

func shouldCreateAffectedZone(text, level, conditionType string) bool {
	if text == "" || isLikelyNonEventText(text) {
		return false
	}
	eventScore := wmHeuristics().strongEventScore(text)
	hasEventSignal := eventScore >= 1.15
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "critical":
		return true
	case "high":
		if hasEventSignal {
			return true
		}
		return conditionType == "earthquake_affected" ||
			conditionType == "tsunami_affected" ||
			conditionType == "volcanic_eruption" ||
			conditionType == "flood_zone" ||
			conditionType == "wildfire_zone" ||
			conditionType == "humanitarian_crisis" ||
			conditionType == "disease_outbreak" ||
			conditionType == "environmental_hazard"
	case "medium":
		if conditionType == "military_conflict" && eventScore >= 0.95 {
			return true
		}
		return hasEventSignal
	case "low":
		if !hasEventSignal && !(conditionType == "military_conflict" && eventScore >= 1.0) {
			return false
		}
		if conditionType == "military_conflict" ||
			conditionType == "earthquake_affected" ||
			conditionType == "tsunami_affected" ||
			conditionType == "volcanic_eruption" ||
			conditionType == "flood_zone" ||
			conditionType == "wildfire_zone" ||
			conditionType == "disease_outbreak" ||
			conditionType == "environmental_hazard" {
			return true
		}
		return conditionType == "cyber_disruption" || conditionType == "infrastructure_disruption"
	default:
		return false
	}
}

func inferZoneConditionAndSubtype(text string, cls horizonMonitorNewsClassification) (conditionType, zoneType, zoneSubType string) {
	if cond, ztyp, sub, _ := wmHeuristics().InferZoneCondition(text); cond != "" {
		return cond, ztyp, sub
	}
	switch strings.ToLower(strings.TrimSpace(cls.Category)) {
	case "military", "conflict":
		return "military_conflict", "military_conflict", "armed_tension"
	case "disaster":
		return "natural_disaster", "natural_disaster", "multi_hazard"
	case "health":
		return "disease_outbreak", "humanitarian_crisis", "epidemic"
	case "environmental":
		return "environmental_hazard", "natural_disaster", "toxic_pollution"
	case "terrorism":
		return "military_conflict", "military_conflict", "terror_threat"
	case "crime":
		return "political_tension", "political_tension", "public_security"
	case "protest", "diplomatic":
		return "political_tension", "political_tension", "civil_tension"
	case "cyber", "infrastructure", "tech":
		return "infrastructure_disruption", "infrastructure_disruption", "systems_disruption"
	case "economic":
		return "political_tension", "political_tension", "economic_pressure"
	default:
		return "", "", ""
	}
}

func inferZoneGeo(e *horizonMonitorFeedEvent) (geoToken, countryCode, countryName string) {
	countryCode = strings.ToUpper(strings.TrimSpace(e.CountryCode))
	countryName = strings.TrimSpace(e.CountryName)
	if countryCode == "" && e != nil {
		countryCode = inferCountryCodeFromText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
		if countryCode != "" {
			countryName = firstNonEmptyString(countryName, countryNameFromCode(countryCode))
		}
	}
	if len(countryCode) == 2 {
		return strings.ToLower(countryCode), countryCode, firstNonEmptyString(countryName, countryNameFromCode(countryCode))
	}
	if lp := strings.TrimSpace(e.LocationPath); strings.HasPrefix(lp, "/loc/") {
		parts := strings.Split(strings.Trim(lp, "/"), "/")
		if len(parts) >= 2 && len(strings.TrimSpace(parts[1])) == 2 {
			cc := strings.ToUpper(strings.TrimSpace(parts[1]))
			return strings.ToLower(cc), cc, firstNonEmptyString(countryName, countryNameFromCode(cc))
		}
	}
	if countryName == "" {
		countryName = "Global"
	}
	return "global", "", countryName
}

func inferZonePlaceName(e *horizonMonitorFeedEvent) string {
	if e == nil {
		return ""
	}
	for _, key := range []string{
		"city", "region", "province", "state", "district", "location", "place", "area",
		"governorate", "oblast", "county", "locality", "prefecture",
	} {
		if v := strings.TrimSpace(asString(e.Raw[key])); isPlausiblePlaceName(v) {
			return v
		}
	}
	if v := extractKnownPlaceFromText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")); isPlausiblePlaceName(v) {
		return v
	}
	if v := extractPlaceFromTitle(strings.TrimSpace(e.Title)); isPlausiblePlaceName(v) {
		return v
	}
	return firstNonEmptyString(strings.TrimSpace(e.CountryName), countryNameFromCode(strings.TrimSpace(e.CountryCode)))
}

func isPlausiblePlaceName(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	norm := normalizePhrase(v)
	if norm == "" {
		return false
	}
	bad := map[string]struct{}{
		"breaking": {}, "analysis": {}, "news": {}, "report": {}, "update": {},
		"military": {}, "conflict": {}, "disaster": {}, "situation": {}, "region submenu": {},
	}
	if _, ok := bad[norm]; ok {
		return false
	}
	return true
}

func extractPlaceFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	clean := strings.NewReplacer(",", " ", ";", " ", ":", " ", "(", " ", ")", " ", "|", " ", "\"", " ", "'", " ").Replace(title)
	words := strings.Fields(clean)
	if len(words) == 0 {
		return ""
	}
	preps := map[string]struct{}{
		"in": {}, "near": {}, "around": {}, "outside": {}, "at": {}, "off": {}, "across": {}, "from": {},
	}
	stop := map[string]struct{}{
		"the": {}, "a": {}, "an": {}, "latest": {}, "breaking": {}, "report": {}, "reports": {},
		"says": {}, "said": {}, "amid": {}, "after": {}, "before": {}, "during": {}, "over": {}, "under": {},
		"attack": {}, "attacks": {}, "strike": {}, "strikes": {}, "war": {}, "conflict": {}, "flood": {}, "earthquake": {},
	}
	for i, w := range words {
		w = strings.ToLower(strings.TrimSpace(w))
		if _, ok := preps[w]; !ok {
			continue
		}
		parts := make([]string, 0, 4)
		for j := i + 1; j < len(words) && len(parts) < 4; j++ {
			token := strings.Trim(words[j], " .-_/")
			if token == "" {
				break
			}
			lower := strings.ToLower(token)
			if _, bad := stop[lower]; bad {
				if len(parts) == 0 {
					continue
				}
				break
			}
			if _, isPrep := preps[lower]; isPrep {
				break
			}
			parts = append(parts, token)
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	return ""
}

func extractKnownPlaceFromText(text string) string {
	text = normalizePhrase(text)
	if text == "" {
		return ""
	}
	best := ""
	bestLen := 0
	for place := range zoneHotspotCoords {
		needle := normalizePhrase(place)
		if needle == "" {
			continue
		}
		if strings.Contains(text, needle) && len(needle) > bestLen {
			best = place
			bestLen = len(needle)
		}
	}
	for alias, cc := range countryNameToCode {
		needle := normalizePhrase(alias)
		if needle == "" || cc == "" {
			continue
		}
		if strings.Contains(text, needle) && len(needle) > bestLen {
			best = countryNameFromCode(cc)
			bestLen = len(needle)
		}
	}
	return strings.TrimSpace(best)
}

func isLikelyNonEventText(text string) bool {
	return wmHeuristics().IsLikelyNonEvent(text)
}

func hasStrongEventSignal(text string) bool {
	return wmHeuristics().HasStrongEvent(text)
}

func inferAffectedZoneCoordinates(e *horizonMonitorFeedEvent, place, countryCode string) (float64, float64, bool, string) {
	if e != nil && e.HasCoordinates && horizonmodels.IsRealGeoCoordinate(e.Lat, e.Lon) {
		return e.Lat, e.Lon, true, "precise"
	}
	if place != "" {
		if lat, lon, ok := knownZoneHotspotCoordinates(place, countryCode); ok {
			return lat, lon, true, "precise"
		}
	}
	if lat, lon, ok := countryCentroid(countryCode); ok {
		return lat, lon, true, "approximate_country"
	}
	return 0, 0, false, "none"
}

func inferZoneRadiusKm(conditionType, level, severity string) float64 {
	base := map[string]float64{
		"military_conflict":         140,
		"natural_disaster":          190,
		"earthquake_affected":       240,
		"tsunami_affected":          260,
		"volcanic_eruption":         220,
		"flood_zone":                210,
		"wildfire_zone":             170,
		"humanitarian_crisis":       260,
		"disease_outbreak":          230,
		"environmental_hazard":      200,
		"political_tension":         110,
		"infrastructure_disruption": 120,
		"cyber_disruption":          90,
	}
	r := firstPositive(base[strings.TrimSpace(conditionType)], 120)
	levelMul := map[string]float64{
		"critical": 1.25,
		"high":     1.12,
		"medium":   1.0,
		"low":      0.88,
		"info":     0.76,
	}
	severityMul := map[string]float64{
		"critical": 1.15,
		"high":     1.06,
		"medium":   1.0,
		"low":      0.9,
	}
	r *= firstPositive(levelMul[strings.ToLower(strings.TrimSpace(level))], 1.0)
	r *= firstPositive(severityMul[strings.ToLower(strings.TrimSpace(severity))], 1.0)
	return clamp(r, 40, 450)
}

func humanizeZoneLabel(place, countryName, zoneSubType string) string {
	place = strings.TrimSpace(place)
	countryName = strings.TrimSpace(countryName)
	sub := strings.TrimSpace(strings.ReplaceAll(zoneSubType, "_", " "))
	switch {
	case place != "" && sub != "":
		return place + " (" + sub + ")"
	case place != "":
		return place
	case countryName != "" && sub != "":
		return countryName + " (" + sub + ")"
	default:
		return firstNonEmptyString(countryName, "Affected zone")
	}
}

func firstPositive(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func activityStatus(score float64) string {
	switch {
	case score >= 72:
		return "hot"
	case score >= 48:
		return "active"
	case score >= 26:
		return "watch"
	case score >= 10:
		return "cooling"
	default:
		return "inactive"
	}
}

func hypeIndicator(score float64) int {
	switch {
	case score >= 80:
		return 5
	case score >= 60:
		return 4
	case score >= 40:
		return 3
	case score >= 20:
		return 2
	case score >= 8:
		return 1
	default:
		return 0
	}
}

func normalizePhrase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "/", " ")
	return strings.Join(strings.Fields(s), " ")
}

func countryCentroid(countryCode string) (float64, float64, bool) {
	cc := strings.ToUpper(strings.TrimSpace(countryCode))
	if cc == "" {
		return 0, 0, false
	}
	if pt, ok := zoneCountryCentroids[cc]; ok && horizonmodels.IsRealGeoCoordinate(pt.Lat, pt.Lon) {
		return pt.Lat, pt.Lon, true
	}
	return 0, 0, false
}

func knownZoneHotspotCoordinates(place, countryCode string) (float64, float64, bool) {
	key := normalizePhrase(place)
	if key == "" {
		return 0, 0, false
	}
	if pt, ok := zoneHotspotCoords[key]; ok && horizonmodels.IsRealGeoCoordinate(pt.Lat, pt.Lon) {
		if cc := strings.TrimSpace(pt.CountryCode); cc == "" || strings.EqualFold(cc, strings.TrimSpace(countryCode)) {
			return pt.Lat, pt.Lon, true
		}
	}
	return 0, 0, false
}

type geoPoint struct {
	Lat         float64
	Lon         float64
	CountryCode string
}

var zoneHotspotCoords = map[string]geoPoint{
	"donetsk":          {Lat: 48.0159, Lon: 37.8029, CountryCode: "UA"},
	"kharkiv":          {Lat: 49.9935, Lon: 36.2304, CountryCode: "UA"},
	"kyiv":             {Lat: 50.4501, Lon: 30.5234, CountryCode: "UA"},
	"crimea":           {Lat: 45.3453, Lon: 34.4992, CountryCode: "UA"},
	"gaza":             {Lat: 31.3547, Lon: 34.3088, CountryCode: "PS"},
	"gaza strip":       {Lat: 31.4000, Lon: 34.3500, CountryCode: "PS"},
	"southern lebanon": {Lat: 33.2500, Lon: 35.4000, CountryCode: "LB"},
	"red sea":          {Lat: 21.0000, Lon: 39.0000, CountryCode: ""},
	"black sea":        {Lat: 43.0000, Lon: 34.0000, CountryCode: ""},
	"taiwan strait":    {Lat: 24.0000, Lon: 119.5000, CountryCode: ""},
	"south china sea":  {Lat: 12.0000, Lon: 114.0000, CountryCode: ""},
	"hormuz":           {Lat: 26.5667, Lon: 56.2500, CountryCode: ""},
	"strait of hormuz": {Lat: 26.5667, Lon: 56.2500, CountryCode: ""},
	"suez":             {Lat: 30.5852, Lon: 32.2654, CountryCode: "EG"},
	"suez canal":       {Lat: 30.2000, Lon: 32.5500, CountryCode: "EG"},
}

var zoneCountryCentroids = map[string]geoPoint{
	"AE": {Lat: 24.3000, Lon: 54.4000},
	"AF": {Lat: 33.0000, Lon: 66.0000},
	"AR": {Lat: -34.0000, Lon: -64.0000},
	"AU": {Lat: -25.2744, Lon: 133.7751},
	"BR": {Lat: -14.2350, Lon: -51.9253},
	"CA": {Lat: 56.1304, Lon: -106.3468},
	"CL": {Lat: -35.6751, Lon: -71.5430},
	"CN": {Lat: 35.8617, Lon: 104.1954},
	"DE": {Lat: 51.1657, Lon: 10.4515},
	"EG": {Lat: 26.8206, Lon: 30.8025},
	"ES": {Lat: 40.4637, Lon: -3.7492},
	"FR": {Lat: 46.2276, Lon: 2.2137},
	"GB": {Lat: 55.3781, Lon: -3.4360},
	"ID": {Lat: -0.7893, Lon: 113.9213},
	"IL": {Lat: 31.0461, Lon: 34.8516},
	"IN": {Lat: 20.5937, Lon: 78.9629},
	"IQ": {Lat: 33.2232, Lon: 43.6793},
	"IR": {Lat: 32.4279, Lon: 53.6880},
	"IT": {Lat: 41.8719, Lon: 12.5674},
	"JP": {Lat: 36.2048, Lon: 138.2529},
	"KR": {Lat: 35.9078, Lon: 127.7669},
	"LB": {Lat: 33.8547, Lon: 35.8623},
	"MX": {Lat: 23.6345, Lon: -102.5528},
	"MY": {Lat: 4.2105, Lon: 101.9758},
	"NG": {Lat: 9.0820, Lon: 8.6753},
	"NL": {Lat: 52.1326, Lon: 5.2913},
	"PK": {Lat: 30.3753, Lon: 69.3451},
	"PH": {Lat: 12.8797, Lon: 121.7740},
	"PL": {Lat: 51.9194, Lon: 19.1451},
	"PS": {Lat: 31.9522, Lon: 35.2332},
	"RU": {Lat: 61.5240, Lon: 105.3188},
	"SA": {Lat: 23.8859, Lon: 45.0792},
	"SD": {Lat: 12.8628, Lon: 30.2176},
	"SE": {Lat: 60.1282, Lon: 18.6435},
	"SG": {Lat: 1.3521, Lon: 103.8198},
	"SY": {Lat: 34.8021, Lon: 38.9968},
	"TH": {Lat: 15.8700, Lon: 100.9925},
	"TR": {Lat: 38.9637, Lon: 35.2433},
	"TW": {Lat: 23.6978, Lon: 120.9605},
	"UA": {Lat: 48.3794, Lon: 31.1656},
	"US": {Lat: 37.0902, Lon: -95.7129},
	"VN": {Lat: 14.0583, Lon: 108.2772},
	"YE": {Lat: 15.5527, Lon: 48.5164},
	"ZA": {Lat: -30.5595, Lon: 22.9375},
}

func isRealCoordinate(lat, lon float64, hasCoordinates bool) bool {
	if !hasCoordinates {
		return false
	}
	return horizonmodels.IsRealGeoCoordinate(lat, lon)
}

func extractTrackObservation(e *horizonMonitorFeedEvent) (horizonTrackObservation, bool) {
	if e == nil {
		return horizonTrackObservation{}, false
	}
	raw := e.Raw
	feedType := strings.TrimSpace(e.FeedType)
	signalType := strings.TrimSpace(e.SignalType)

	air := signalType == "risk:military_flight" || feedType == "opensky_flights"
	sea := signalType == "risk:military_vessel" || feedType == "ais_snapshot"
	if !air && !sea {
		return horizonTrackObservation{}, false
	}

	readIDWithScheme := func(keys []struct{ k, scheme string }) (id, scheme string) {
		for _, kv := range keys {
			if v := strings.TrimSpace(asString(raw[kv.k])); v != "" {
				return v, kv.scheme
			}
		}
		return "", ""
	}
	heading, _ := lookupFloat(raw, "heading", "track", "true_track", "course")
	speed, speedOK := lookupFloat(raw, "velocity", "speed", "speed_knots", "sog")
	altitude, altitudeOK := lookupFloat(raw, "baro_altitude", "geo_altitude", "altitude")
	depth, depthOK := lookupFloat(raw, "depth", "depth_m")
	out := horizonTrackObservation{
		DisplayName: firstNonEmptyString(strings.TrimSpace(e.Title), asString(raw["name"]), asString(raw["callsign"])),
	}
	if headingVal, ok := lookupFloat(raw, "heading", "track", "true_track", "course"); ok {
		out.HeadingDeg = float64Ptr(headingVal)
	} else if heading != 0 {
		out.HeadingDeg = float64Ptr(heading)
	}
	if speedOK {
		out.SpeedKts = float64Ptr(speed)
	}
	if altitudeOK {
		out.AltitudeM = float64Ptr(altitude)
	}
	if depthOK {
		out.DepthM = float64Ptr(depth)
	}

	if air {
		out.Kind = horizonmodels.TrackKindAir
		out.ObjectType = "military_aircraft"
		out.ExternalTrackID, out.IdScheme = readIDWithScheme([]struct{ k, scheme string }{
			{"icao24", "icao24"}, {"icao", "icao24"}, {"hex", "icao24"},
			{"flight", "flight"}, {"callsign", "flight"}, {"id", "flight"},
		})
		if strings.TrimSpace(out.DisplayName) == "" {
			out.DisplayName = firstNonEmptyString(asString(raw["callsign"]), asString(raw["flight"]), out.ExternalTrackID, "air-track")
		}
		if !hasMovableTrackEvidence(out) {
			return horizonTrackObservation{}, false
		}
		return out, true
	}
	out.Kind = horizonmodels.TrackKindSea
	out.ObjectType = "vessel"
	out.ExternalTrackID, out.IdScheme = readIDWithScheme([]struct{ k, scheme string }{
		{"mmsi", "mmsi"}, {"imo", "imo"}, {"vessel_id", "mmsi"}, {"ship_id", "mmsi"}, {"id", "mmsi"},
	})
	if strings.TrimSpace(out.DisplayName) == "" {
		out.DisplayName = firstNonEmptyString(asString(raw["name"]), asString(raw["ship_name"]), asString(raw["vessel_name"]), out.ExternalTrackID, "sea-track")
	}
	if !hasMovableTrackEvidence(out) {
		return horizonTrackObservation{}, false
	}
	return out, true
}

func hasMovableTrackEvidence(out horizonTrackObservation) bool {
	return strings.TrimSpace(out.ExternalTrackID) != "" ||
		out.SpeedKts != nil ||
		out.HeadingDeg != nil ||
		out.AltitudeM != nil ||
		out.DepthM != nil
}

func isAllowedTrackObjectType(objectType string) bool {
	switch strings.TrimSpace(strings.ToLower(objectType)) {
	case "military_aircraft", "vessel", "ground_vehicle", "convoy",
		"bulk_carrier", "tanker", "cargo", "ore_carrier":
		return true
	default:
		return false
	}
}

func float64Ptr(v float64) *float64 {
	x := v
	return &x
}

func (p *HorizonMonitorPipeline) mergeAndAggregate(newObs []horizonMonitorSignalObservation) (map[string]horizonMonitorCountryAggregate, map[string]horizonMonitorGridAggregate) {
	now := time.Now().UTC()
	p.mu.Lock()
	defer p.mu.Unlock()

	p.observations = append(p.observations, newObs...)
	cutoff := now.Add(-p.window)
	filtered := p.observations[:0]
	for _, obs := range p.observations {
		if obs.Timestamp.IsZero() || obs.Timestamp.Before(cutoff) {
			continue
		}
		filtered = append(filtered, obs)
	}
	p.observations = filtered

	countries := make(map[string]horizonMonitorCountryAggregate)
	cells := make(map[string]horizonMonitorGridAggregate)

	for _, obs := range p.observations {
		cc := strings.TrimSpace(strings.ToUpper(obs.CountryCode))
		if cc == "" {
			cc = "XX"
		}
		agg, ok := countries[cc]
		if !ok {
			agg = horizonMonitorCountryAggregate{
				CountryCode:  cc,
				CountryName:  firstNonEmptyString(obs.CountryName, countryNameFromCode(cc), cc),
				SignalCounts: make(map[string]int),
				SourceIDs:    []string{},
			}
		}
		st := firstNonEmptyString(strings.TrimSpace(obs.SignalType), "risk:temporal_anomaly")
		agg.SignalCounts[st] = agg.SignalCounts[st] + 1
		agg.TotalSignals++
		if strings.EqualFold(strings.TrimSpace(obs.Severity), "high") {
			agg.HighSeverityCount++
		}
		agg.LastSeen = maxTime(agg.LastSeen, obs.Timestamp)
		agg.SourceIDs = append(agg.SourceIDs, strings.TrimSpace(obs.SourceID))
		countries[cc] = agg

		if !obs.HasCoordinates {
			continue
		}
		cellID := horizonMonitorCellID(obs.Lat, obs.Lon)
		cell, ok := cells[cellID]
		if !ok {
			cell = horizonMonitorGridAggregate{
				CellID:        cellID,
				CellCenterLat: math.Floor(obs.Lat) + 0.5,
				CellCenterLon: math.Floor(obs.Lon) + 0.5,
				SignalCounts:  make(map[string]int),
			}
		}
		cell.SignalCounts[st] = cell.SignalCounts[st] + 1
		cell.TotalSignals++
		cell.LastSeen = maxTime(cell.LastSeen, obs.Timestamp)
		cells[cellID] = cell
	}

	for cc, agg := range countries {
		agg.SourceIDs = uniqueStrings(agg.SourceIDs)
		agg.DistinctSignalTypes = len(agg.SignalCounts)
		typeBonus := float64(agg.DistinctSignalTypes) * 20
		countBonus := math.Min(30, float64(agg.TotalSignals)*5)
		severityBonus := float64(agg.HighSeverityCount) * 10
		agg.ConvergenceScore = math.Min(100, typeBonus+countBonus+severityBonus)
		countries[cc] = agg
	}
	for cellID, cell := range cells {
		cell.DistinctSignalTypes = len(cell.SignalCounts)
		typeScore := float64(cell.DistinctSignalTypes) * 25
		countBoost := math.Min(25, float64(cell.TotalSignals)*2)
		cell.ConvergenceScore = math.Min(100, typeScore+countBoost)
		cells[cellID] = cell
	}

	p.gcWaveStateLocked(now)
	return countries, cells
}

func (p *HorizonMonitorPipeline) mergeTopicGeoAggregates(newObs []horizonMonitorTopicGeoObservation) map[string]horizonMonitorTopicCountryAggregate {
	now := time.Now().UTC()
	p.mu.Lock()
	defer p.mu.Unlock()

	p.topicObs = append(p.topicObs, newObs...)
	cutoff := now.Add(-p.window)
	filtered := p.topicObs[:0]
	for _, obs := range p.topicObs {
		if obs.Timestamp.IsZero() || obs.Timestamp.Before(cutoff) {
			continue
		}
		filtered = append(filtered, obs)
	}
	p.topicObs = filtered

	out := make(map[string]horizonMonitorTopicCountryAggregate)
	for _, obs := range p.topicObs {
		topic := strings.TrimSpace(obs.Topic)
		if topic == "" {
			continue
		}
		cc := strings.TrimSpace(strings.ToUpper(obs.CountryCode))
		if cc == "" {
			cc = "XX"
		}
		key := strings.ToLower(cc) + "|" + topic
		agg, ok := out[key]
		if !ok {
			agg = horizonMonitorTopicCountryAggregate{
				CountryCode: cc,
				CountryName: firstNonEmptyString(obs.CountryName, countryNameFromCode(cc), cc),
				Topic:       topic,
				SourceIDs:   []string{},
			}
		}
		agg.TotalMentions++
		agg.LastSeen = maxTime(agg.LastSeen, obs.Timestamp)
		agg.SourceIDs = append(agg.SourceIDs, strings.TrimSpace(obs.SourceID))
		out[key] = agg
	}

	for key, agg := range out {
		agg.SourceIDs = uniqueStrings(agg.SourceIDs)
		out[key] = agg
	}
	return out
}

func (p *HorizonMonitorPipeline) upsertCountryRollupFacts(ctx context.Context, tenantID string, countries map[string]horizonMonitorCountryAggregate) error {
	for cc, agg := range countries {
		cc = strings.TrimSpace(strings.ToUpper(cc))
		if cc == "" {
			continue
		}
		id := "hm_rollup_country_" + strings.ToLower(cc)
		locPath := locationPathFromCountryCode(cc)
		var locPtr *string
		if locPath != "" {
			lp := locPath
			locPtr = &lp
		}
		observedAt := agg.LastSeen
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		metricUpdates := []coremodels.FactMetricUpdate{
			{MetricName: "hm_total_signals", Value: float64(agg.TotalSignals), Confidence: 0.95, ObservedAt: &observedAt},
			{MetricName: "hm_high_severity_signals", Value: float64(agg.HighSeverityCount), Confidence: 0.95, ObservedAt: &observedAt},
			{MetricName: "hm_distinct_signal_types", Value: float64(agg.DistinctSignalTypes), Confidence: 0.95, ObservedAt: &observedAt},
			{MetricName: "hm_convergence_score", Value: agg.ConvergenceScore, Confidence: 0.95, ObservedAt: &observedAt},
		}
		for st, cnt := range agg.SignalCounts {
			name := "hm_signal_count_" + slug(strings.TrimPrefix(strings.TrimSpace(st), "risk:"))
			metricUpdates = append(metricUpdates, coremodels.FactMetricUpdate{
				MetricName: name,
				Value:      float64(cnt),
				Confidence: 0.90,
				ObservedAt: &observedAt,
			})
		}
		tags := []string{
			"country_rollup",
			"country:" + strings.ToLower(cc),
			"llm:false",
		}
		for st := range agg.SignalCounts {
			tags = append(tags, "signal_type:"+strings.TrimSpace(st))
		}
		f := &coremodels.OntologyFact{
			BaseOntologyFields: coremodels.BaseOntologyFields{
				ID:             id,
				TenantID:       tenantID,
				Version:        1,
				CreatedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
				CreatedBy:      "horizon_monitor_pipeline",
				UpdatedBy:      "horizon_monitor_pipeline",
				Labels:         map[string]string{"en": "Horizon Monitor country rollup: " + agg.CountryName},
				Status:         "active",
				CanonicalPaths: uniqueStrings([]string{locPath}),
				LocationPath:   locPtr,
				Tags:           horizonMonitorFactTags(coremodels.FactExtractionLayerHeuristic, false, false, tags...),
			},
			Kind:          "metric",
			Type:          "horizon_monitor_country_rollup",
			Title:         "Horizon Monitor Country Rollup: " + agg.CountryName,
			Summary:       fmt.Sprintf("%s: %d signals (%d high), %d types, convergence score %.0f", agg.CountryName, agg.TotalSignals, agg.HighSeverityCount, agg.DistinctSignalTypes, agg.ConvergenceScore),
			Confidence:    0.95,
			ObservedAt:    &observedAt,
			SourceIDs:     agg.SourceIDs,
			MetricUpdates: metricUpdates,
			ExtractionLineage: horizonMonitorLineage(
				coremodels.FactExtractionLayerHeuristic,
				false,
				false,
			),
		}
		if err := p.factsRepo.Upsert(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

func (p *HorizonMonitorPipeline) upsertTopicCountryRollupFacts(ctx context.Context, tenantID string, countries map[string]horizonMonitorTopicCountryAggregate) error {
	for _, agg := range countries {
		cc := strings.TrimSpace(strings.ToUpper(agg.CountryCode))
		topic := strings.TrimSpace(agg.Topic)
		if cc == "" || topic == "" || cc == "XX" {
			continue
		}
		geoToken := strings.ToLower(cc)
		topicPath, ok := taxonomy.SegmentCanonicalPath("technology", topic, geoToken)
		if !ok || strings.TrimSpace(topicPath) == "" {
			continue
		}
		locPath := locationPathFromCountryCode(cc)
		var locPtr *string
		if locPath != "" {
			lp := locPath
			locPtr = &lp
		}
		observedAt := agg.LastSeen
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		id := "hm_topic_geo_" + strings.ToLower(cc) + "_" + slug(topic)
		f := &coremodels.OntologyFact{
			BaseOntologyFields: coremodels.BaseOntologyFields{
				ID:             id,
				TenantID:       tenantID,
				Version:        1,
				CreatedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
				CreatedBy:      "horizon_monitor_pipeline",
				UpdatedBy:      "horizon_monitor_pipeline",
				Labels:         map[string]string{"en": fmt.Sprintf("Topic rollup: %s in %s", topic, agg.CountryName)},
				Status:         "active",
				CanonicalPaths: uniqueStrings([]string{locPath, topicPath}),
				LocationPath:   locPtr,
				SpherePaths:    []string{"/technology"},
				Tags: horizonMonitorFactTags(
					coremodels.FactExtractionLayerHeuristic,
					false,
					false,
					"topic_geo_rollup",
					"llm:false",
					"topic:"+slug(topic),
					"country:"+strings.ToLower(cc),
				),
			},
			Kind:       "metric",
			Type:       "horizon_monitor_topic_geo_rollup",
			Title:      fmt.Sprintf("%s activity in %s", topic, agg.CountryName),
			Summary:    fmt.Sprintf("%s mentioned %d times in %s during the active observation window", topic, agg.TotalMentions, agg.CountryName),
			Confidence: 0.92,
			ObservedAt: &observedAt,
			SourceIDs:  agg.SourceIDs,
			MetricUpdates: []coremodels.FactMetricUpdate{
				{MetricName: "hm_topic_mentions", Value: float64(agg.TotalMentions), Confidence: 0.92, ObservedAt: &observedAt},
			},
			ExtractionLineage: horizonMonitorLineage(
				coremodels.FactExtractionLayerHeuristic,
				false,
				false,
			),
		}
		if err := p.factsRepo.Upsert(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

func (p *HorizonMonitorPipeline) upsertGridCellFacts(ctx context.Context, tenantID string, cells map[string]horizonMonitorGridAggregate) error {
	for cellID, cell := range cells {
		if cell.DistinctSignalTypes < 2 {
			continue
		}
		observedAt := cell.LastSeen
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		id := "hm_rollup_grid_" + slug(cellID)
		tags := []string{
			"geo_grid_cell",
			"cell:" + slug(cellID),
			"lat_bucket:" + strconv.FormatFloat(math.Floor(cell.CellCenterLat), 'f', 0, 64),
			"lon_bucket:" + strconv.FormatFloat(math.Floor(cell.CellCenterLon), 'f', 0, 64),
			"llm:false",
		}
		for st := range cell.SignalCounts {
			tags = append(tags, "signal_type:"+strings.TrimSpace(st))
		}
		metricUpdates := []coremodels.FactMetricUpdate{
			{MetricName: "hm_grid_total_signals", Value: float64(cell.TotalSignals), Confidence: 0.95, ObservedAt: &observedAt},
			{MetricName: "hm_grid_distinct_signal_types", Value: float64(cell.DistinctSignalTypes), Confidence: 0.95, ObservedAt: &observedAt},
			{MetricName: "hm_grid_convergence_score", Value: cell.ConvergenceScore, Confidence: 0.95, ObservedAt: &observedAt},
		}
		for st, cnt := range cell.SignalCounts {
			metricUpdates = append(metricUpdates, coremodels.FactMetricUpdate{
				MetricName: "hm_grid_signal_count_" + slug(strings.TrimPrefix(st, "risk:")),
				Value:      float64(cnt),
				Confidence: 0.9,
				ObservedAt: &observedAt,
			})
		}
		f := &coremodels.OntologyFact{
			BaseOntologyFields: coremodels.BaseOntologyFields{
				ID:             id,
				TenantID:       tenantID,
				Version:        1,
				CreatedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
				CreatedBy:      "horizon_monitor_pipeline",
				UpdatedBy:      "horizon_monitor_pipeline",
				Labels:         map[string]string{"en": "Horizon Monitor grid cell " + cellID},
				Status:         "active",
				CanonicalPaths: []string{"/geo/grid/" + slug(cellID)},
				Tags:           horizonMonitorFactTags(coremodels.FactExtractionLayerHeuristic, false, false, tags...),
			},
			Kind:          "metric",
			Type:          "horizon_monitor_geo_cell",
			Title:         "Horizon Monitor Geo Cell " + cellID,
			Summary:       fmt.Sprintf("Cell %s: %d signals, %d types, score %.0f", cellID, cell.TotalSignals, cell.DistinctSignalTypes, cell.ConvergenceScore),
			Confidence:    0.95,
			ObservedAt:    &observedAt,
			MetricUpdates: metricUpdates,
			ExtractionLineage: horizonMonitorLineage(
				coremodels.FactExtractionLayerHeuristic,
				false,
				false,
			),
		}
		if err := p.factsRepo.Upsert(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

func (p *HorizonMonitorPipeline) upsertCountryBriefFacts(ctx context.Context, tenantID string, countries map[string]horizonMonitorCountryAggregate) error {
	top := make([]horizonMonitorCountryAggregate, 0, len(countries))
	for _, agg := range countries {
		if agg.TotalSignals <= 0 {
			continue
		}
		top = append(top, agg)
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].ConvergenceScore == top[j].ConvergenceScore {
			return top[i].TotalSignals > top[j].TotalSignals
		}
		return top[i].ConvergenceScore > top[j].ConvergenceScore
	})
	if len(top) > 5 {
		top = top[:5]
	}

	now := time.Now().UTC()
	for _, agg := range top {
		cc := strings.TrimSpace(strings.ToUpper(agg.CountryCode))
		if cc == "" || cc == "XX" {
			continue
		}
		if !p.shouldRefreshBrief(cc, now) {
			continue
		}
		brief := p.generateCountryBrief(ctx, agg)
		locPath := locationPathFromCountryCode(cc)
		var locPtr *string
		if locPath != "" {
			lp := locPath
			locPtr = &lp
		}
		f := &coremodels.OntologyFact{
			BaseOntologyFields: coremodels.BaseOntologyFields{
				ID:             "hm_country_brief_" + strings.ToLower(cc),
				TenantID:       tenantID,
				Version:        1,
				CreatedAt:      now,
				UpdatedAt:      now,
				CreatedBy:      "horizon_monitor_pipeline",
				UpdatedBy:      "horizon_monitor_pipeline",
				Labels:         map[string]string{"en": "Country brief: " + agg.CountryName},
				Status:         "active",
				CanonicalPaths: uniqueStrings([]string{locPath}),
				LocationPath:   locPtr,
				Tags: horizonMonitorFactTags(
					coremodels.FactExtractionLayerLLM,
					true,
					true,
					"country_brief",
					"country:"+strings.ToLower(cc),
				),
			},
			Kind:       "other",
			Type:       "country_brief",
			Title:      "Country Brief: " + agg.CountryName,
			Summary:    brief.Summary,
			Confidence: wmClamp01(brief.Confidence),
			ObservedAt: &now,
			SourceIDs:  agg.SourceIDs,
			ExtractionLineage: horizonMonitorLineage(
				coremodels.FactExtractionLayerLLM,
				true,
				true,
			),
		}
		if err := p.factsRepo.Upsert(ctx, f); err != nil {
			return err
		}
		p.markBriefRefreshed(cc, now)
	}
	return nil
}

func (p *HorizonMonitorPipeline) generateCountryBrief(ctx context.Context, agg horizonMonitorCountryAggregate) horizonMonitorCountryBriefOutput {
	fallback := buildDeterministicCountryBrief(agg)
	if p.exec == nil || !p.enableAIBriefs {
		return fallback
	}

	typeCounts := make([]string, 0, len(agg.SignalCounts))
	for st, cnt := range agg.SignalCounts {
		typeCounts = append(typeCounts, fmt.Sprintf("%s=%d", st, cnt))
	}
	sort.Strings(typeCounts)

	prompt := strings.Builder{}
	prompt.WriteString("You are generating a short country risk brief for internal analytics. ")
	prompt.WriteString("Return strict JSON only.\n")
	prompt.WriteString("Country: ")
	prompt.WriteString(agg.CountryName)
	prompt.WriteString(" (")
	prompt.WriteString(strings.ToUpper(agg.CountryCode))
	prompt.WriteString(")\n")
	prompt.WriteString("Signals in last 24h: ")
	prompt.WriteString(strconv.Itoa(agg.TotalSignals))
	prompt.WriteString("\n")
	prompt.WriteString("High-severity signals: ")
	prompt.WriteString(strconv.Itoa(agg.HighSeverityCount))
	prompt.WriteString("\n")
	prompt.WriteString("Distinct signal types: ")
	prompt.WriteString(strconv.Itoa(agg.DistinctSignalTypes))
	prompt.WriteString("\n")
	prompt.WriteString("Convergence score: ")
	prompt.WriteString(fmt.Sprintf("%.1f", agg.ConvergenceScore))
	prompt.WriteString("\n")
	prompt.WriteString("Type counts: ")
	prompt.WriteString(strings.Join(typeCounts, ", "))
	prompt.WriteString("\n\n")
	prompt.WriteString("Schema:\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"summary\": string,\n")
	prompt.WriteString("  \"currentSituation\": string,\n")
	prompt.WriteString("  \"militarySecurity\": string,\n")
	prompt.WriteString("  \"keyRiskFactors\": string[],\n")
	prompt.WriteString("  \"regionalContext\": string,\n")
	prompt.WriteString("  \"watchItems\": string[],\n")
	prompt.WriteString("  \"confidence\": number\n")
	prompt.WriteString("}\n")

	verifier := ai.NewGenericVerifier(func(out horizonMonitorCountryBriefOutput) error {
		if strings.TrimSpace(out.Summary) == "" || strings.TrimSpace(out.CurrentSituation) == "" {
			return fmt.Errorf("summary/currentSituation are required")
		}
		if out.Confidence < 0 || out.Confidence > 1 {
			return fmt.Errorf("confidence must be in [0,1]")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContextBriefing,
		InputContent: prompt.String(),
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            700,
		},
		Verifier: verifier,
	}
	parsed, err := p.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return fallback
	}
	if out, ok := parsed.(horizonMonitorCountryBriefOutput); ok {
		if strings.TrimSpace(out.Summary) == "" {
			return fallback
		}
		out.Confidence = wmClamp01(out.Confidence)
		return out
	}
	return fallback
}

func buildDeterministicCountryBrief(agg horizonMonitorCountryAggregate) horizonMonitorCountryBriefOutput {
	typeList := make([]string, 0, len(agg.SignalCounts))
	for st := range agg.SignalCounts {
		typeList = append(typeList, st)
	}
	sort.Strings(typeList)
	level := "elevated"
	if agg.ConvergenceScore >= 70 {
		level = "high"
	} else if agg.ConvergenceScore < 35 {
		level = "moderate"
	}
	summary := fmt.Sprintf("%s shows %s multi-signal activity (%d signals, score %.0f).", agg.CountryName, level, agg.TotalSignals, agg.ConvergenceScore)
	return horizonMonitorCountryBriefOutput{
		Summary:          summary,
		CurrentSituation: fmt.Sprintf("Observed %d signals in the last 24 hours across %d signal types.", agg.TotalSignals, agg.DistinctSignalTypes),
		MilitarySecurity: fmt.Sprintf("Security pressure indicator: %d high-severity signals.", agg.HighSeverityCount),
		KeyRiskFactors: []string{
			"Convergence score indicates concentration of cross-domain signals.",
			"Signal mix: " + strings.Join(typeList, ", "),
		},
		RegionalContext: "Cross-border context should be validated against neighboring countries' rollups.",
		WatchItems: []string{
			"Growth in high-severity signal count",
			"New signal types entering the 24h window",
			"Persistence of logistics/infrastructure disruption markers",
		},
		Confidence: 0.72,
	}
}

func (p *HorizonMonitorPipeline) classifyHorizonMonitorNewsWithAI(ctx context.Context, title string) (horizonMonitorNewsClassification, bool) {
	title = strings.TrimSpace(title)
	if title == "" || p.exec == nil {
		return horizonMonitorNewsClassification{}, false
	}
	levels := []string{"critical", "high", "medium", "low", "info"}
	categories := []string{"conflict", "protest", "disaster", "diplomatic", "economic", "terrorism", "cyber", "health", "environmental", "military", "crime", "infrastructure", "tech", "general"}
	prompt := strings.Builder{}
	prompt.WriteString("Classify this headline for geopolitical monitoring. Return JSON only.\n")
	prompt.WriteString("Headline: ")
	prompt.WriteString(title)
	prompt.WriteString("\nAllowed levels: ")
	prompt.WriteString(strings.Join(levels, ", "))
	prompt.WriteString("\nAllowed categories: ")
	prompt.WriteString(strings.Join(categories, ", "))
	prompt.WriteString("\nSchema: {\"level\": string, \"category\": string, \"confidence\": number}\n")

	verifier := ai.NewGenericVerifier(func(out struct {
		Level      string  `json:"level"`
		Category   string  `json:"category"`
		Confidence float64 `json:"confidence"`
	}) error {
		if !containsString(levels, strings.ToLower(strings.TrimSpace(out.Level))) {
			return fmt.Errorf("invalid level")
		}
		if !containsString(categories, strings.ToLower(strings.TrimSpace(out.Category))) {
			return fmt.Errorf("invalid category")
		}
		if out.Confidence < 0 || out.Confidence > 1 {
			return fmt.Errorf("confidence out of range")
		}
		return nil
	})
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContextAnalysis,
		InputContent: prompt.String(),
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            120,
		},
		Verifier: verifier,
	}
	parsed, err := p.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return horizonMonitorNewsClassification{}, false
	}
	out, ok := parsed.(struct {
		Level      string  `json:"level"`
		Category   string  `json:"category"`
		Confidence float64 `json:"confidence"`
	})
	if !ok {
		return horizonMonitorNewsClassification{}, false
	}
	return horizonMonitorNewsClassification{
		Level:      strings.ToLower(strings.TrimSpace(out.Level)),
		Category:   strings.ToLower(strings.TrimSpace(out.Category)),
		Confidence: wmClamp01(out.Confidence),
		Source:     "ai",
	}, true
}

func (p *HorizonMonitorPipeline) shouldRefreshBrief(countryCode string, now time.Time) bool {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	last := p.lastBriefByCC[countryCode]
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= 30*time.Minute
}

func (p *HorizonMonitorPipeline) markBriefRefreshed(countryCode string, at time.Time) {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" {
		return
	}
	p.mu.Lock()
	p.lastBriefByCC[countryCode] = at.UTC()
	p.mu.Unlock()
}

func (p *HorizonMonitorPipeline) isWaveThrottled(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	for k, ts := range p.recentWaves {
		if now.Sub(ts) > p.waveThrottleWindow {
			delete(p.recentWaves, k)
		}
	}
	if ts, ok := p.recentWaves[key]; ok {
		return now.Sub(ts) <= p.waveThrottleWindow
	}
	return false
}

func (p *HorizonMonitorPipeline) markWaveSeen(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	p.mu.Lock()
	p.recentWaves[key] = time.Now().UTC()
	p.mu.Unlock()
}

func (p *HorizonMonitorPipeline) gcWaveStateLocked(now time.Time) {
	for k, ts := range p.recentWaves {
		if now.Sub(ts) > p.waveThrottleWindow {
			delete(p.recentWaves, k)
		}
	}
}

func (p *HorizonMonitorPipeline) logf(format string, args ...interface{}) {
	if p != nil && p.logger != nil {
		p.logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func horizonMonitorCellID(lat, lon float64) string {
	return fmt.Sprintf("%d,%d", int(math.Floor(lat)), int(math.Floor(lon)))
}

func inferHorizonMonitorSignalType(signalHint string, e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification) string {
	if explicit := normalizeSignalTypeHint(signalHint); explicit != "" {
		return explicit
	}
	if e == nil {
		return "risk:temporal_anomaly"
	}
	if mapped := signalTypeFromFeedType(e.FeedType); mapped != "" {
		return mapped
	}

	text := normalizeKeywordText(strings.Join([]string{e.Title, e.Summary, e.Category}, " "))
	category := strings.ToLower(strings.TrimSpace(cls.Category))
	sourceCategory := strings.ToLower(strings.TrimSpace(e.Category))
	militaryContext := hasAnyKeyword(text,
		"fighter", "jet", "air defence", "air defense", "royal air force", "raf", "fighter aircraft",
		"air force", "squadron", "missile", "drone", "navy", "army", "troops", "military", "defence", "defense",
	)
	defenseIndustrialContext := hasDefenseIndustrialContext(text)
	weatherContext := hasAnyKeyword(text,
		"storm", "rainfall", "wind speeds", "evacuation", "seismic", "weather service", "cyclone warning", "flooding", "aftershock",
	)
	if (isLikelyStrategicPlanningText(text) || isLikelyRoundupCommentaryText(text)) && !hasDirectDisruptionTrigger(text) {
		return "risk:temporal_anomaly"
	}
	if hasAnyKeyword(text, "missile makers", "quadruple production", "ramp up production", "increase production") && !hasDirectDisruptionTrigger(text) {
		return "risk:temporal_anomaly"
	}
	if hasAnyKeyword(text, "f-16", "jdam", "precision bomb", "anti-drone system", "anti drone system", "laser test", "ramat david air base") {
		return "risk:conflict_event"
	}
	if defenseIndustrialContext && !hasDirectDisruptionTrigger(text) {
		if hasAnyKeyword(text, "labor strike", "workers strike", "dock strike", "general strike") {
			return "risk:protest"
		}
		if hasAnyKeyword(text, "air defence", "air defense", "anti-drone", "interceptor", "fighter", "missile", "drone", "laser test", "task force") {
			return "risk:conflict_event"
		}
		return "risk:temporal_anomaly"
	}

	switch {
	case hasCivilProtestSignal(text):
		return "risk:protest"
	case militaryContext && hasAnyKeyword(text, "f-16", "jdam", "precision bomb", "anti-drone system", "air base", "surface to air", "munition", "fighter bomber"):
		return "risk:conflict_event"
	case hasAnyKeyword(text, "airspace", "airport", "flight", "runway", "aviation", "airline", "civil aviation", "closed airport", "airport closure"):
		if hasAnyKeyword(text, "military flight", "fighter jet", "military aircraft", "bomber", "surveillance flight") {
			return "risk:military_flight"
		}
		return "risk:aviation_disruption"
	case hasAnyKeyword(text, "strait", "canal", "shipping", "vessel", "port", "tanker", "container ship", "red sea", "hormuz", "bab al mandab", "maritime", "sea lane", "freight lane"):
		return "risk:maritime_hazard"
	case hasAnyKeyword(text, "internet outage", "network outage", "telecom outage", "service outage", "platform outage", "cloud outage", "dns outage", "ddos", "ransomware", "cyber attack", "cyberattack", "data breach", "internet shutdown"):
		if hasAnyKeyword(text, "internet", "telecom", "dns", "ddos", "network", "internet shutdown") || category == "cyber" || sourceCategory == "cyber" {
			return "risk:internet_outage"
		}
		return "risk:service_outage"
	case hasAnyKeyword(text, "typhoon", "typhoons", "hurricane", "cyclone") && militaryContext && !weatherContext:
		return "risk:conflict_event"
	case hasAnyKeyword(text, "earthquake", "wildfire", "flood", "storm", "hurricane", "typhoon", "cyclone", "heatwave", "heat wave", "drought", "outbreak", "epidemic", "pandemic", "landslide", "eruption", "tsunami"):
		return "risk:climate_anomaly"
	case hasAnyKeyword(text, "missile", "drone", "airstrike", "shelling", "troops", "military", "conflict", "rocket", "explosion", "attack", "navy", "army", "militia", "insurgent", "ceasefire", "war", "combat", "hostage", "terror"):
		return "risk:conflict_event"
	}

	switch category {
	case "protest":
		return "risk:protest"
	case "military", "conflict", "terrorism", "crime":
		return "risk:conflict_event"
	case "cyber":
		return "risk:internet_outage"
	case "infrastructure":
		if hasAnyKeyword(text, "port", "shipping", "strait", "vessel", "tanker", "canal") {
			return "risk:maritime_hazard"
		}
		if hasAnyKeyword(text, "flight", "airport", "airspace", "airline") {
			return "risk:aviation_disruption"
		}
		return "risk:service_outage"
	case "disaster", "environmental", "health":
		return "risk:climate_anomaly"
	case "economic":
		if hasAnyKeyword(text, "lng", "pipeline", "refinery", "terminal", "grid", "power plant", "power station", "oil field", "gas field") {
			return "risk:service_outage"
		}
	}

	return "risk:temporal_anomaly"
}

func normalizeSignalTypeHint(signalHint string) string {
	signalHint = strings.TrimSpace(signalHint)
	if signalHint == "" {
		return ""
	}
	if _, ok := horizonMonitorBlueprintFromSignal(signalHint); ok {
		return signalHint
	}
	return signalTypeFromFeedType(signalHint)
}

func hasAnyKeyword(text string, keywords ...string) bool {
	for _, kw := range keywords {
		kw = normalizeKeywordText(kw)
		if kw == "" {
			continue
		}
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func signalTypeFromFeedType(feedType string) string {
	switch strings.TrimSpace(feedType) {
	case "gdelt_geo_protest", "acled_protests":
		return "risk:protest"
	case "opensky_flights":
		return "risk:military_flight"
	case "ais_snapshot":
		return "risk:ais_disruption"
	case "cloudflare_outages":
		return "risk:internet_outage"
	case "nasa_firms_fires":
		return "risk:satellite_fire"
	case "climate_anomalies":
		return "risk:climate_anomaly"
	case "service_status":
		return "risk:service_outage"
	case "ucdp_ged_events", "hapi_conflict_context", "acled_conflict":
		return "risk:conflict_event"
	case "nga_warnings":
		return "risk:maritime_hazard"
	case "faa_airport_status":
		return "risk:aviation_disruption"
	case "usgs_earthquakes":
		return "risk:temporal_anomaly"
	default:
		if strings.HasPrefix(strings.TrimSpace(feedType), "gdelt_doc_") {
			return "risk:temporal_anomaly"
		}
		return ""
	}
}

func heuristicSignalSeedNames(e *horizonMonitorFeedEvent) []string {
	if e == nil {
		return nil
	}
	text := strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lower := normalizeKeywordText(text)
	seeds := make([]string, 0, 8)
	seen := make(map[string]bool)
	add := func(seed string) {
		seed = strings.TrimSpace(seed)
		if seed == "" {
			return
		}
		key := strings.ToLower(seed)
		if seen[key] {
			return
		}
		seen[key] = true
		seeds = append(seeds, seed)
	}

	for _, pair := range []struct {
		match string
		seed  string
	}{
		{"strait of hormuz", "Strait of Hormuz"},
		{"bab al mandab", "Bab el-Mandeb"},
		{"red sea", "Red Sea"},
		{"suez canal", "Suez Canal"},
		{"taiwan strait", "Taiwan Strait"},
		{"panama canal", "Panama Canal"},
		{"houthi", "Houthis"},
		{"houthi rebels", "Houthis"},
		{"irgc", "Islamic Revolutionary Guard Corps"},
		{"idf", "Israel Defense Forces"},
		{"israel defense forces", "Israel Defense Forces"},
		{"pentagon", "United States Department of Defense"},
		{"kremlin", "Government of Russia"},
		{"qatarenergy", "QatarEnergy"},
		{"gazprom", "Gazprom"},
		{"opec+", "OPEC+"},
		{"opec", "OPEC"},
		{"hamas", "Hamas"},
		{"hezbollah", "Hezbollah"},
	} {
		if strings.Contains(lower, pair.match) {
			add(pair.seed)
		}
	}

	for _, anchor := range extractGeoAnchorSeedNames(e, 5) {
		add(anchor)
	}

	for _, phrase := range extractHeuristicSeedPhrases(e.Title, 5) {
		add(phrase)
	}
	for _, phrase := range extractHeuristicSeedPhrases(e.Summary, 4) {
		add(phrase)
	}

	if strings.TrimSpace(e.CountryName) != "" && len(seeds) > 0 {
		add(strings.TrimSpace(e.CountryName) + " government")
	}
	if len(seeds) > 8 {
		seeds = seeds[:8]
	}
	return seeds
}

func heuristicSignalSeedPaths(e *horizonMonitorFeedEvent, locationPath string) []string {
	if e == nil {
		return nil
	}
	text := strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")
	countries := extractCountryCodesFromText(text, 3)
	if len(countries) <= 1 {
		return nil
	}
	primary := strings.ToUpper(strings.TrimSpace(countryFromLocationPathString(locationPath)))
	paths := make([]string, 0, len(countries))
	seen := make(map[string]bool)
	for _, cc := range countries {
		cc = strings.ToUpper(strings.TrimSpace(cc))
		if cc == "" {
			continue
		}
		if cc == primary && primary != "" {
			continue
		}
		path := locationPathFromCountryCode(cc)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func extractHeuristicSeedPhrases(text string, maxCount int) []string {
	text = strings.TrimSpace(text)
	if text == "" || maxCount <= 0 {
		return nil
	}
	results := make([]string, 0, maxCount)
	seen := make(map[string]bool)
	add := func(seed string) {
		seed = strings.TrimSpace(strings.Trim(seed, " ,.;:()[]{}\"'"))
		if seed == "" {
			return
		}
		lower := strings.ToLower(seed)
		if seen[lower] || isWeakSeedPhrase(seed) {
			return
		}
		seen[lower] = true
		results = append(results, seed)
	}

	for _, match := range wmSeedPhraseRegex.FindAllString(text, -1) {
		add(match)
		if len(results) >= maxCount {
			return results
		}
	}
	for _, match := range wmSeedUpperAcronymRegex.FindAllString(text, -1) {
		add(match)
		if len(results) >= maxCount {
			return results
		}
	}
	return results
}

func extractGeoAnchorSeedNames(e *horizonMonitorFeedEvent, maxCount int) []string {
	if e == nil || maxCount <= 0 {
		return nil
	}
	text := strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")
	normalized := normalizeKeywordText(text)
	if normalized == "" {
		return nil
	}
	results := make([]string, 0, maxCount)
	seen := make(map[string]bool)
	add := func(seed string) {
		seed = strings.TrimSpace(seed)
		if seed == "" || len(results) >= maxCount {
			return
		}
		key := strings.ToLower(seed)
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, seed)
	}

	countries := extractCountryCodesFromText(text, 3)
	if hasAnyKeyword(normalized, "air defence", "air defense", "air-defence", "air-defence") {
		for _, cc := range countries {
			if name := strings.TrimSpace(countryNameFromCode(cc)); name != "" {
				add(name + " air defense")
			}
		}
	}
	if hasAnyKeyword(normalized, "navy", "naval", "navies") {
		for _, cc := range countries {
			if name := strings.TrimSpace(countryNameFromCode(cc)); name != "" {
				add(name + " navy")
			}
		}
	}
	if hasAnyKeyword(normalized, "air force", "fighter", "fighters", "typhoon", "typhoons", "f-16", "raf") {
		for _, cc := range countries {
			if name := strings.TrimSpace(countryNameFromCode(cc)); name != "" {
				add(name + " air force")
			}
		}
	}
	if hasAnyKeyword(normalized, "port", "shipping", "maritime", "tanker", "canal", "strait", "gulf") {
		for _, cc := range countries {
			if name := strings.TrimSpace(countryNameFromCode(cc)); name != "" {
				add(name + " port")
			}
		}
	}
	if hasAnyKeyword(normalized, "lng", "natural gas", "gas", "pipeline", "refinery", "oil", "petrochem") {
		for _, cc := range countries {
			if name := strings.TrimSpace(countryNameFromCode(cc)); name != "" {
				add(name + " energy")
			}
		}
	}
	for _, cc := range countries {
		name := strings.TrimSpace(countryNameFromCode(cc))
		if name != "" {
			add(name)
		}
	}
	return results
}

func isWeakSeedPhrase(seed string) bool {
	entityIndicators := []string{
		"government", "department", "ministry", "forces", "force", "guard", "corps", "command",
		"committee", "administration", "agency", "authority", "council", "bank", "office",
		"house", "pentagon", "kremlin", "railways", "railway", "port", "airport", "terminal",
		"pipeline", "refinery", "plant", "field", "market", "services", "energy", "canal",
		"strait", "gulf", "sea", "fleet", "army", "navy", "air force", "idf", "irgc", "opec",
	}
	stop := map[string]bool{
		"the": true, "a": true, "an": true, "breaking": true, "live": true,
		"analysis": true, "news": true, "update": true, "middle east": true,
		"europe": true, "asia": true, "africa": true, "iran": true, "iraq": true,
		"israel": true, "syria": true, "lebanon": true, "turkey": true, "russia": true,
		"ukraine": true, "china": true, "india": true, "pakistan": true, "saudi arabia": true,
		"related posts": true, "backstage partner": true, "exquisite class": true,
		"bunker talk": true, "the osmp": true,
	}
	norm := normalizePhrase(seed)
	if norm == "" {
		return true
	}
	if isDocumentMarkupSeedPhrase(seed, norm) {
		return true
	}
	if stop[norm] {
		return true
	}
	if strings.Contains(norm, "appears with mysterious") || strings.Contains(norm, "let s talk about") || strings.Contains(norm, "didn t cover") {
		return true
	}
	if len(strings.Fields(seed)) == 1 && len(seed) < 4 {
		return true
	}
	if strings.HasPrefix(norm, "in ") || strings.HasPrefix(norm, "on ") || strings.HasPrefix(norm, "at ") {
		return true
	}
	if !seedLooksLikeEntity(seed, norm, entityIndicators) {
		return true
	}
	return false
}

func isDocumentMarkupSeedPhrase(seed, norm string) bool {
	if seed == "" || norm == "" {
		return true
	}
	for _, phrase := range []string{
		"top secret",
		"secret noforn",
		"hand distribution only",
		"distribution only",
		"printed by",
		"classified by",
		"declassify on",
		"codex level",
		"for official use only",
		"controlled unclassified information",
	} {
		if strings.Contains(norm, phrase) {
			return true
		}
	}
	words := strings.Fields(strings.TrimSpace(seed))
	if len(words) == 0 {
		return true
	}
	upper := 0
	for _, word := range words {
		word = strings.Trim(word, " ,.;:()[]{}\"'")
		if len(word) >= 2 && word == strings.ToUpper(word) {
			upper++
		}
	}
	if len(words) >= 2 && upper == len(words) && !seedLooksOperationalAllCaps(norm) {
		return true
	}
	return false
}

func seedLooksOperationalAllCaps(norm string) bool {
	for _, keep := range []string{
		"idf",
		"irgc",
		"raf",
		"iaea",
		"opec",
		"opec+",
		"nti",
		"fas",
		"csis",
		"ecfr",
	} {
		if norm == keep {
			return true
		}
	}
	return false
}

func seedLooksLikeEntity(seed, norm string, indicators []string) bool {
	if seed == "" || norm == "" {
		return false
	}
	for _, indicator := range indicators {
		if strings.Contains(norm, indicator) {
			return true
		}
	}
	for _, token := range strings.Fields(seed) {
		if len(token) >= 2 && token == strings.ToUpper(token) {
			return true
		}
	}
	words := strings.Fields(seed)
	if len(words) >= 3 {
		longCapitalized := 0
		for _, word := range words {
			word = strings.Trim(word, " ,.;:()[]{}\"'")
			if len(word) >= 5 && len(word) > 0 && strings.ToUpper(word[:1])+strings.ToLower(word[1:]) == word {
				longCapitalized++
			}
		}
		return longCapitalized >= 2
	}
	return false
}

func horizonMonitorBlueprintFromSignal(signalType string) (horizonMonitorSignalBlueprint, bool) {
	signalType = strings.TrimSpace(signalType)
	switch signalType {
	case "risk:protest":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisGeoConflict,
			Mechanism:  "protest",
			Sphere:     []string{"/sphere/governance/political"},
		}, true
	case "risk:military_flight":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisGeoConflict,
			Mechanism:  "military_flight",
			Sphere:     []string{"/sphere/defense"},
		}, true
	case "risk:ais_disruption":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisLogisticsCapacity,
			Mechanism:  "ais_disruption",
			Sphere:     []string{"/sphere/logistics"},
		}, true
	case "risk:internet_outage", "risk:service_outage":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisOperationalContinuity,
			Mechanism:  strings.TrimPrefix(signalType, "risk:"),
			Sphere:     []string{"/sphere/infrastructure/telecom"},
		}, true
	case "risk:satellite_fire":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisOperationalContinuity,
			Mechanism:  "satellite_fire",
			Sphere:     []string{"/sphere/environment/climate"},
		}, true
	case "risk:climate_anomaly":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisOperationalContinuity,
			Mechanism:  "climate_anomaly",
			Sphere:     []string{"/sphere/environment/climate"},
		}, true
	case "risk:conflict_event":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisGeoConflict,
			Mechanism:  "conflict_event",
			Sphere:     []string{"/sphere/defense"},
		}, true
	case "risk:maritime_hazard":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisLogisticsCapacity,
			Mechanism:  "maritime_hazard",
			Sphere:     []string{"/sphere/logistics"},
		}, true
	case "risk:aviation_disruption":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisLogisticsCapacity,
			Mechanism:  "aviation_disruption",
			Sphere:     []string{"/sphere/logistics"},
		}, true
	case "risk:temporal_anomaly":
		return horizonMonitorSignalBlueprint{
			SignalType: signalType,
			Axis:       horizonmodels.AxisInfoUncertainty,
			Mechanism:  "temporal_anomaly",
			Sphere:     []string{"/sphere/social/information"},
		}, true
	default:
		return horizonMonitorSignalBlueprint{}, false
	}
}

func eventTypeFromSignalType(signalType string) string {
	switch strings.TrimSpace(signalType) {
	case "risk:protest":
		return "protest"
	case "risk:military_flight", "risk:conflict_event":
		return "military"
	case "risk:internet_outage", "risk:service_outage", "risk:ais_disruption", "risk:maritime_hazard", "risk:aviation_disruption":
		return "infrastructure"
	case "risk:satellite_fire", "risk:climate_anomaly", "risk:temporal_anomaly":
		return "disaster"
	default:
		return "other"
	}
}

func spherePathsFromCategory(category string) []string {
	switch strings.TrimSpace(strings.ToLower(category)) {
	case "unrest", "conflict", "military":
		return []string{"/sphere/defense"}
	case "maritime", "transport":
		return []string{"/sphere/logistics"}
	case "infrastructure":
		return []string{"/sphere/infrastructure"}
	case "natural":
		return []string{"/sphere/environment"}
	case "market", "economy", "economic":
		return []string{"/sphere/finance"}
	default:
		return []string{"/sphere/social/information"}
	}
}

func amplitudeFromSeverity(severity string) float64 {
	switch strings.TrimSpace(strings.ToLower(severity)) {
	case "high", "critical":
		return 0.85
	case "medium", "major":
		return 0.60
	case "low", "minor":
		return 0.35
	default:
		return 0.50
	}
}

func confidenceFromSeverity(severity string) float64 {
	switch strings.TrimSpace(strings.ToLower(severity)) {
	case "high", "critical":
		return 0.85
	case "medium", "major":
		return 0.72
	case "low", "minor":
		return 0.60
	default:
		return 0.65
	}
}

func normalizeHorizonMonitorSeverity(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "critical", "severe", "high", "total":
		return "high"
	case "major", "elevated", "medium", "moderate":
		return "medium"
	case "minor", "low", "info", "nominal":
		return "low"
	default:
		return ""
	}
}

func inferSeverityFromPayload(raw map[string]interface{}, feedType string) string {
	if strings.TrimSpace(feedType) == "nasa_firms_fires" {
		if br, ok := lookupFloat(raw, "brightness"); ok {
			switch {
			case br >= 360:
				return "high"
			case br >= 320:
				return "medium"
			default:
				return "low"
			}
		}
	}
	if z, ok := lookupFloat(raw, "zScore", "z_score"); ok {
		switch {
		case z >= 3.0:
			return "high"
		case z >= 2.0:
			return "medium"
		default:
			return "low"
		}
	}
	if n, ok := lookupFloat(raw, "count", "total", "events", "currentCount"); ok {
		switch {
		case n >= 10:
			return "high"
		case n >= 5:
			return "medium"
		default:
			return "low"
		}
	}
	return "medium"
}

func parseJSONMap(raw string) map[string]interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]interface{}{}
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		return obj
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		if len(arr) > 0 {
			if first, ok := arr[0].(map[string]interface{}); ok {
				return first
			}
		}
	}
	return map[string]interface{}{}
}

func extractCoordinates(raw map[string]interface{}) (float64, float64, bool) {
	if len(raw) == 0 {
		return 0, 0, false
	}
	if lat, okLat := lookupFloat(raw, "lat", "latitude", "y"); okLat {
		if lon, okLon := lookupFloat(raw, "lon", "lng", "longitude", "x"); okLon {
			return lat, lon, true
		}
	}
	if geo, ok := raw["geometry"].(map[string]interface{}); ok {
		if coords, ok := geo["coordinates"].([]interface{}); ok && len(coords) >= 2 {
			lon, okLon := toFloat(coords[0])
			lat, okLat := toFloat(coords[1])
			if okLat && okLon {
				return lat, lon, true
			}
		}
	}
	if coords, ok := raw["coordinates"].([]interface{}); ok && len(coords) >= 2 {
		lon, okLon := toFloat(coords[0])
		lat, okLat := toFloat(coords[1])
		if okLat && okLon {
			return lat, lon, true
		}
	}
	if point, ok := raw["point"].(map[string]interface{}); ok {
		if lat, okLat := lookupFloat(point, "lat", "latitude"); okLat {
			if lon, okLon := lookupFloat(point, "lon", "lng", "longitude"); okLon {
				return lat, lon, true
			}
		}
	}
	return 0, 0, false
}

func lookupFloat(m map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		for k, v := range m {
			if !strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(key)) {
				continue
			}
			if f, ok := toFloat(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func toFloat(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f, true
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func classifyHorizonMonitorNewsKeyword(title string) horizonMonitorNewsClassification {
	return wmHeuristics().Classify(title)
}

func normalizeClassificationWithSourceHint(cls horizonMonitorNewsClassification, e *horizonMonitorFeedEvent) horizonMonitorNewsClassification {
	if e == nil {
		return cls
	}
	text := strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")
	mappedCat := mapSourceCategoryToClassificationCategory(strings.TrimSpace(e.Category), strings.TrimSpace(e.FeedType), text)
	eventScore := wmHeuristics().strongEventScore(text)
	hasStrongEvent := eventScore >= 1.15

	level := strings.ToLower(strings.TrimSpace(cls.Level))
	category := strings.ToLower(strings.TrimSpace(cls.Category))

	if category == "" || category == "general" {
		if mappedCat != "" {
			cls.Category = mappedCat
			if level == "" || level == "info" {
				cls.Level = "low"
				level = "low"
			}
			if cls.Confidence < 0.52 {
				cls.Confidence = 0.52
			}
			if strings.EqualFold(strings.TrimSpace(cls.Source), "keyword") || strings.EqualFold(strings.TrimSpace(cls.Source), "keyword_ac") {
				cls.Source = "keyword_ac+source"
			}
			category = strings.ToLower(strings.TrimSpace(cls.Category))
		}
	}

	if hasStrongEvent {
		switch level {
		case "", "info":
			cls.Level = "medium"
			level = "medium"
		case "low":
			cls.Level = "medium"
			level = "medium"
		}
		if eventScore >= 2.25 && (category == "military" || category == "conflict" || category == "cyber" || category == "disaster" || category == "infrastructure") {
			if level == "medium" {
				cls.Level = "high"
				level = "high"
			}
		}
		switch level {
		case "high":
			if cls.Confidence < 0.73 {
				cls.Confidence = 0.73
			}
		case "medium":
			if cls.Confidence < 0.62 {
				cls.Confidence = 0.62
			}
		}
	}

	if strings.TrimSpace(cls.Category) == "" {
		cls.Category = "general"
	}
	if strings.TrimSpace(cls.Level) == "" {
		cls.Level = "info"
	}
	if strings.TrimSpace(cls.Source) == "" {
		cls.Source = "keyword_ac"
	}
	return cls
}

func mapSourceCategoryToClassificationCategory(sourceCategory, feedType, text string) string {
	cat := strings.ToLower(strings.TrimSpace(sourceCategory))
	if cat == "" {
		cat = strings.ToLower(strings.TrimSpace(feedType))
	}
	switch cat {
	case "defense", "military", "war":
		return "military"
	case "cyber", "security":
		return "cyber"
	case "disaster", "humanitarian":
		return "disaster"
	case "health", "medical":
		return "health"
	case "environment", "environmental", "climate":
		return "environmental"
	case "terror", "terrorism":
		return "terrorism"
	case "crime", "public-safety":
		return "crime"
	case "shipping", "maritime", "logistics", "infrastructure":
		return "infrastructure"
	case "economic", "finance", "energy":
		return "economic"
	case "mainstream", "wire":
		if wmHeuristics().strongEventScore(text) >= 1.6 {
			return "conflict"
		}
		return "diplomatic"
	case "intl", "international", "gov", "government":
		return "diplomatic"
	case "research", "analysis":
		if wmHeuristics().strongEventScore(text) >= 1.7 {
			return "conflict"
		}
		return "diplomatic"
	default:
		return ""
	}
}

func classificationProcessingMode(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "ai":
		return "ai_refined"
	default:
		return "heuristic_keyword"
	}
}

func detectTechTopicsFromText(text string) []string {
	lower := normalizeKeywordText(text)
	if lower == "" {
		return nil
	}
	rules := map[string][]string{
		"artificial-intelligence": {"artificial intelligence", "machine learning", "generative ai", "large language model", "llm", "ai model", "ai chip"},
		"semiconductors":          {"semiconductor", "chipmaker", "chip fab", "foundry", "fabless", "wafer", "lithography", "tsmc", "asml"},
		"cybersecurity":           {"cyber attack", "ransomware", "malware", "zero day", "ddos", "botnet", "phishing", "vulnerability"},
		"cloud-infrastructure":    {"cloud outage", "data center", "datacenter", "hyperscaler", "kubernetes", "server farm"},
		"telecom-5g":              {"5g", "6g", "telecom", "mobile network", "base station", "spectrum auction"},
		"quantum-tech":            {"quantum", "quantum computing", "quantum processor", "qubit"},
		"space-tech":              {"satellite", "launch vehicle", "spaceport", "orbital", "payload", "starlink"},
		"robotics-autonomy":       {"robotics", "autonomous", "drone swarm", "industrial robot", "uav", "unmanned"},
		"battery-energy-tech":     {"battery", "ev battery", "solid state battery", "grid storage", "lithium ion"},
		"biotech-health-tech":     {"biotech", "genome", "mrna", "vaccine platform", "clinical trial", "biosecurity"},
	}
	out := make([]string, 0, 4)
	for topic, keys := range rules {
		if keywordAnyMatch(lower, keys) {
			out = append(out, topic)
		}
	}
	sort.Strings(out)
	return uniqueStrings(out)
}

func keywordAnyMatch(text string, keywords []string) bool {
	if text == "" || len(keywords) == 0 {
		return false
	}
	padded := " " + text + " "
	for _, kw := range keywords {
		kw = strings.TrimSpace(strings.ToLower(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(padded, " "+kw+" ") {
			return true
		}
	}
	return false
}

func normalizeKeywordText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"_", " ",
		"-", " ",
		"/", " ",
		".", " ",
		",", " ",
		":", " ",
		";", " ",
		"(", " ",
		")", " ",
		"'", " ",
		"\"", " ",
	)
	s = replacer.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func locationPathFromCountryCode(cc string) string {
	cc = strings.ToLower(strings.TrimSpace(cc))
	if len(cc) != 2 {
		return ""
	}
	return "/loc/" + cc
}

func normalizeCountryCode(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) == 2 {
		return strings.ToUpper(v)
	}
	lower := strings.ToLower(v)
	if cc, ok := countryNameToCode[lower]; ok {
		return cc
	}
	return ""
}

func inferCountryCodeFromText(text string) string {
	codes := extractCountryCodesFromText(text, 1)
	if len(codes) == 0 {
		return ""
	}
	return codes[0]
}

func extractCountryCodesFromText(text string, limit int) []string {
	rawText := strings.TrimSpace(text)
	text = normalizeKeywordText(text)
	if text == "" {
		return nil
	}
	if limit <= 0 {
		limit = 1
	}
	padded := " " + text + " "
	scores := map[string]int{}
	add := func(alias, cc string, weight int) {
		alias = normalizePhrase(alias)
		cc = strings.ToUpper(strings.TrimSpace(cc))
		if alias == "" || cc == "" {
			return
		}
		if geoambiguity.IsAmbiguousCountryName(alias) && !geoambiguity.CountryAliasAllowedInText(alias, rawText) {
			return
		}
		if strings.Contains(alias, " ") {
			if strings.Contains(text, alias) {
				scores[cc] += weight
			}
			return
		}
		if strings.Contains(padded, " "+alias+" ") || strings.Contains(padded, " "+alias+" s ") {
			scores[cc] += weight
		}
	}
	for alias, cc := range countryNameToCode {
		add(alias, cc, 3)
	}
	for alias, cc := range countryTextAliasToCode {
		add(alias, cc, 2)
	}
	type rankedCountry struct {
		cc    string
		score int
	}
	ranked := make([]rankedCountry, 0, len(scores))
	for cc, score := range scores {
		if score <= 0 {
			continue
		}
		ranked = append(ranked, rankedCountry{cc: cc, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].cc < ranked[j].cc
		}
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]string, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.cc)
	}
	return out
}

func countryFromLocationPathString(locationPath string) string {
	path := strings.Trim(strings.TrimSpace(locationPath), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[1]))
}

func inferCountryCodeFromLatLon(lat, lon float64) string {
	switch {
	case lat >= 25 && lat <= 40 && lon >= 44 && lon <= 63:
		return "IR"
	case lat >= 29 && lat <= 33 && lon >= 34 && lon <= 36:
		return "IL"
	case lat >= 15 && lat <= 32 && lon >= 34 && lon <= 55:
		return "SA"
	case lat >= 20 && lat <= 55 && lon >= 73 && lon <= 135:
		return "CN"
	case lat >= 22 && lat <= 25 && lon >= 120 && lon <= 122:
		return "TW"
	case lat >= 8 && lat <= 37 && lon >= 68 && lon <= 97:
		return "IN"
	case lat >= 44 && lat <= 52 && lon >= 22 && lon <= 40:
		return "UA"
	case lat >= 50 && lat <= 82 && lon >= 20 && lon <= 180:
		return "RU"
	case lat >= 22 && lat <= 32 && lon >= 25 && lon <= 35:
		return "EG"
	case lat >= 24 && lat <= 49 && lon >= -125 && lon <= -66:
		return "US"
	default:
		return ""
	}
}

func countryNameFromCode(cc string) string {
	cc = strings.ToUpper(strings.TrimSpace(cc))
	if cc == "" {
		return ""
	}
	if name, ok := countryCodeToName[cc]; ok {
		return name
	}
	return cc
}

var countryNameToCode = map[string]string{
	"afghanistan":              "AF",
	"algeria":                  "DZ",
	"argentina":                "AR",
	"australia":                "AU",
	"austria":                  "AT",
	"azerbaijan":               "AZ",
	"bahrain":                  "BH",
	"bangladesh":               "BD",
	"belarus":                  "BY",
	"belgium":                  "BE",
	"bolivia":                  "BO",
	"brazil":                   "BR",
	"bulgaria":                 "BG",
	"canada":                   "CA",
	"chile":                    "CL",
	"china":                    "CN",
	"colombia":                 "CO",
	"czech republic":           "CZ",
	"denmark":                  "DK",
	"ecuador":                  "EC",
	"egypt":                    "EG",
	"estonia":                  "EE",
	"ethiopia":                 "ET",
	"finland":                  "FI",
	"france":                   "FR",
	"georgia":                  "GE",
	"germany":                  "DE",
	"greece":                   "GR",
	"hungary":                  "HU",
	"india":                    "IN",
	"indonesia":                "ID",
	"iran":                     "IR",
	"israel":                   "IL",
	"italy":                    "IT",
	"saudi arabia":             "SA",
	"united arab emirates":     "AE",
	"iraq":                     "IQ",
	"ireland":                  "IE",
	"japan":                    "JP",
	"jordan":                   "JO",
	"kazakhstan":               "KZ",
	"kenya":                    "KE",
	"kuwait":                   "KW",
	"lebanon":                  "LB",
	"libya":                    "LY",
	"lithuania":                "LT",
	"malaysia":                 "MY",
	"mexico":                   "MX",
	"morocco":                  "MA",
	"netherlands":              "NL",
	"new zealand":              "NZ",
	"nigeria":                  "NG",
	"north korea":              "KP",
	"norway":                   "NO",
	"oman":                     "OM",
	"pakistan":                 "PK",
	"peru":                     "PE",
	"philippines":              "PH",
	"poland":                   "PL",
	"portugal":                 "PT",
	"qatar":                    "QA",
	"romania":                  "RO",
	"russia":                   "RU",
	"singapore":                "SG",
	"slovakia":                 "SK",
	"south africa":             "ZA",
	"south korea":              "KR",
	"spain":                    "ES",
	"sudan":                    "SD",
	"sweden":                   "SE",
	"switzerland":              "CH",
	"syria":                    "SY",
	"taiwan":                   "TW",
	"thailand":                 "TH",
	"turkey":                   "TR",
	"turkiye":                  "TR",
	"ukraine":                  "UA",
	"united kingdom":           "GB",
	"united states":            "US",
	"united states of america": "US",
	"usa":                      "US",
	"u.s.":                     "US",
	"u.s.a.":                   "US",
	"venezuela":                "VE",
	"vietnam":                  "VN",
	"yemen":                    "YE",
	"uk":                       "GB",
}

var countryCodeToName = map[string]string{
	"AF": "Afghanistan",
	"AR": "Argentina",
	"AT": "Austria",
	"AU": "Australia",
	"AZ": "Azerbaijan",
	"BD": "Bangladesh",
	"BE": "Belgium",
	"BG": "Bulgaria",
	"BH": "Bahrain",
	"BO": "Bolivia",
	"BR": "Brazil",
	"BY": "Belarus",
	"CA": "Canada",
	"CH": "Switzerland",
	"CL": "Chile",
	"CO": "Colombia",
	"CZ": "Czech Republic",
	"DK": "Denmark",
	"EE": "Estonia",
	"ET": "Ethiopia",
	"FI": "Finland",
	"IR": "Iran",
	"IL": "Israel",
	"SA": "Saudi Arabia",
	"AE": "United Arab Emirates",
	"DZ": "Algeria",
	"EC": "Ecuador",
	"IQ": "Iraq",
	"SY": "Syria",
	"YE": "Yemen",
	"JO": "Jordan",
	"LB": "Lebanon",
	"CN": "China",
	"TW": "Taiwan",
	"JP": "Japan",
	"KR": "South Korea",
	"KP": "North Korea",
	"KZ": "Kazakhstan",
	"KE": "Kenya",
	"KW": "Kuwait",
	"LY": "Libya",
	"LT": "Lithuania",
	"MA": "Morocco",
	"MY": "Malaysia",
	"MX": "Mexico",
	"NG": "Nigeria",
	"NO": "Norway",
	"NZ": "New Zealand",
	"OM": "Oman",
	"PE": "Peru",
	"PH": "Philippines",
	"PL": "Poland",
	"PS": "Palestine",
	"PT": "Portugal",
	"QA": "Qatar",
	"RO": "Romania",
	"SD": "Sudan",
	"SE": "Sweden",
	"SG": "Singapore",
	"SK": "Slovakia",
	"TH": "Thailand",
	"TR": "Turkey",
	"IN": "India",
	"PK": "Pakistan",
	"UA": "Ukraine",
	"RU": "Russia",
	"EG": "Egypt",
	"US": "United States",
	"GB": "United Kingdom",
	"DE": "Germany",
	"FR": "France",
	"GE": "Georgia",
	"GR": "Greece",
	"HU": "Hungary",
	"ID": "Indonesia",
	"IE": "Ireland",
	"IT": "Italy",
	"NL": "Netherlands",
	"ES": "Spain",
	"VE": "Venezuela",
	"VN": "Vietnam",
	"ZA": "South Africa",
}

var countryTextAliasToCode = map[string]string{
	"american":     "US",
	"britain":      "GB",
	"british":      "GB",
	"chinese":      "CN",
	"french":       "FR",
	"german":       "DE",
	"indian":       "IN",
	"iranian":      "IR",
	"iraqi":        "IQ",
	"israeli":      "IL",
	"italian":      "IT",
	"japanese":     "JP",
	"lebanese":     "LB",
	"pakistani":    "PK",
	"palestinian":  "PS",
	"polish":       "PL",
	"russian":      "RU",
	"saudi":        "SA",
	"south korean": "KR",
	"spanish":      "ES",
	"syrian":       "SY",
	"taiwanese":    "TW",
	"turkish":      "TR",
	"ukrainian":    "UA",
	"u k":          "GB",
	"u s":          "US",
	"u s a":        "US",
	"uae":          "AE",
	"un":           "",
}

func slug(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return "na"
	}
	replacer := strings.NewReplacer("/", "_", " ", "_", ":", "_", "-", "_", ".", "_", ",", "_")
	out := replacer.Replace(in)
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	out = strings.Trim(out, "_")
	if out == "" {
		return "na"
	}
	return out
}

func canonicalPathSlug(in string) string {
	out := strings.TrimSpace(taxonomy.Slugify(strings.TrimSpace(in)))
	if out == "" {
		return "na"
	}
	return out
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:8])
}

func formatISO6709(lat, lon float64) string {
	return fmt.Sprintf("%+.6f%+.6f/", lat, lon)
}

func encodeGeoHash(lat, lon float64, precision int) string {
	if precision <= 0 {
		precision = 7
	}
	base32 := "0123456789bcdefghjkmnpqrstuvwxyz"
	bits := []int{16, 8, 4, 2, 1}
	latMin, latMax := -90.0, 90.0
	lonMin, lonMax := -180.0, 180.0
	even := true
	bit := 0
	ch := 0
	out := strings.Builder{}
	out.Grow(precision)
	for out.Len() < precision {
		if even {
			mid := (lonMin + lonMax) / 2
			if lon >= mid {
				ch |= bits[bit]
				lonMin = mid
			} else {
				lonMax = mid
			}
		} else {
			mid := (latMin + latMax) / 2
			if lat >= mid {
				ch |= bits[bit]
				latMin = mid
			} else {
				latMax = mid
			}
		}
		even = !even
		if bit < 4 {
			bit++
			continue
		}
		out.WriteByte(base32[ch])
		bit = 0
		ch = 0
	}
	return out.String()
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func containsString(haystack []string, needle string) bool {
	needle = strings.TrimSpace(strings.ToLower(needle))
	for _, v := range haystack {
		if strings.TrimSpace(strings.ToLower(v)) == needle {
			return true
		}
	}
	return false
}

func wmClamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clamp(v, lo, hi float64) float64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func decayExp(v, deltaHours, halfLifeHours float64) float64 {
	if v <= 0 {
		return 0
	}
	if deltaHours <= 0 {
		return v
	}
	if halfLifeHours <= 0 {
		return 0
	}
	return v * math.Pow(0.5, deltaHours/halfLifeHours)
}

func maxTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.After(a) {
		return b
	}
	return a
}

func trackCanonicalPath(e *horizonMonitorFeedEvent, tr horizonTrackObservation) string {
	geoToken := strings.ToLower(strings.TrimSpace(e.CountryCode))
	if len(geoToken) != 2 {
		geoToken = "global"
	}
	trackIdentity := strings.TrimSpace(tr.ExternalTrackID)
	idScheme := strings.TrimSpace(strings.ToLower(tr.IdScheme))
	if trackIdentity == "" {
		trackIdentity = "event-" + shortHash(strings.Join([]string{
			strings.TrimSpace(e.SourceID),
			strings.TrimSpace(e.ExternalID),
			strconv.FormatFloat(e.Lat, 'f', 6, 64),
			strconv.FormatFloat(e.Lon, 'f', 6, 64),
		}, "|"))
		idScheme = ""
	}
	var lastSeg string
	if idScheme != "" && isValidTrackIdScheme(idScheme) {
		lastSeg = idScheme + "-" + normalizeTrackIdForPath(idScheme, trackIdentity)
	} else {
		baseName := firstNonEmptyString(strings.TrimSpace(tr.DisplayName), trackIdentity, strings.TrimSpace(e.Title), "track")
		lastSeg = slug(baseName) + "-" + shortHash(strings.Join([]string{string(tr.Kind), trackIdentity}, "|"))
	}
	canonicalPath := strings.Join([]string{"/track", string(tr.Kind), geoToken, lastSeg}, "/")
	return strings.ReplaceAll(canonicalPath, "//", "/")
}

func isValidTrackIdScheme(s string) bool {
	switch s {
	case "mmsi", "icao24", "imo", "flight":
		return true
	default:
		return false
	}
}

func normalizeTrackIdForPath(scheme, id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	switch scheme {
	case "mmsi":
		// 9 digits, leading zeros allowed
		if len(id) == 9 && isDigits(id) {
			return id
		}
		return slug(id)
	case "icao24":
		// 6 hex chars, lowercase
		if len(id) == 6 && isHex(id) {
			return id
		}
		return slug(id)
	case "imo":
		// 7 digits (IMO ship number)
		if len(id) == 7 && isDigits(id) {
			return id
		}
		return slug(id)
	case "flight":
		// callsign: slug, lowercase
		return slug(id)
	default:
		return slug(id)
	}
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isHex(s string) bool {
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

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
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	discoverynlp "github.com/igorkirisyuk/sanskrite/internal/discovery/nlp"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/geoambiguity"
	mapactivity "github.com/igorkirisyuk/sanskrite/internal/horizon/mapactivity"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrecommendations "github.com/igorkirisyuk/sanskrite/internal/horizon/recommendations"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonsignallanding "github.com/igorkirisyuk/sanskrite/internal/horizon/signallanding"
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

	affectedZoneCoolingStep  = 2 * time.Hour
	affectedZoneCoolingLimit = 1000
)

var (
	wmSeedUpperAcronymRegex = regexp.MustCompile(`\b[A-Z][A-Z0-9&+\-]{1,}\b`)
	wmSeedPhraseRegex       = regexp.MustCompile(`\b(?:[A-Z][a-z]+|[A-Z]{2,})(?:\s+(?:of|the|and|for|in|on|at|de|del|al|el|bin|ibn|st\.?|saint))?\s+(?:[A-Z][a-z]+|[A-Z]{2,})(?:\s+(?:[A-Z][a-z]+|[A-Z]{2,}))*\b`)
)

func normalizeHorizonMonitorIngestMode(ds *coremodels.DataSource) string {
	if ds == nil {
		return wmIngestModeDocumentFeed
	}
	mode := strings.ToLower(strings.TrimSpace(coremodels.SourceOutputModeValue(ds)))
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

type horizonMonitorLLMArticleAdjudication struct {
	SchemaVersion      string  `json:"schemaVersion"`
	IsEvent            bool    `json:"isEvent"`
	EventClass         string  `json:"eventClass"`
	EventConfidence    float64 `json:"eventConfidence"`
	ShouldCreateZone   bool    `json:"shouldCreateZone"`
	ShouldWarmExisting bool    `json:"shouldWarmExisting"`
	ShouldOnlySignal   bool    `json:"shouldOnlySignal"`
	ConditionType      string  `json:"conditionType"`
	Place              string  `json:"place"`
	CountryCode        string  `json:"countryCode"`
	GeoConfidence      float64 `json:"geoConfidence"`
	Reason             string  `json:"reason"`
}

type affectedZoneCandidate struct {
	CanonicalPath        string
	Label                string
	ConditionType        string
	ZoneType             string
	ZoneSubType          string
	InferenceScore       float64
	EventConfidence      float64
	GeoConfidence        float64
	ResolutionConfidence float64
	GeoToken             string
	CountryCode          string
	CountryName          string
	RadiusKm             float64
	Lat                  float64
	Lon                  float64
	HasCoordinates       bool
	GeoPrecision         string
	GeoResolutionMethod  string
	LinkedFragmentPaths  []string
	WarmOnly             bool
	Reason               string
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

	affectedZoneRecoveryStatusActive     = "active_outage"
	affectedZoneRecoveryStatusRecovering = "recovering"
	affectedZoneRecoveryStatusRestored   = "restored"

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
	case "precise", "entity_anchor":
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
	if (strings.EqualFold(strings.TrimSpace(zone.GeoPrecision), "precise") || strings.EqualFold(strings.TrimSpace(zone.GeoPrecision), "entity_anchor")) &&
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

type affectedZoneGeoAssistOutput struct {
	Place       string  `json:"place"`
	CountryCode string  `json:"countryCode"`
	GeoKind     string  `json:"geoKind"`
	Confidence  float64 `json:"confidence"`
}

// HorizonMonitorPipeline transforms Horizon Monitor feeds to our ontology/signal model.
// It is deterministic-first and uses AI only for compact country briefs (strict JSON).
type HorizonMonitorPipeline struct {
	factsRepo                *corerepo.OntologyFactsRepository
	fragmentsRepo            *horizonrepo.HorizonFragmentsRepository
	edgesRepo                *horizonrepo.DependencyEdgesRepository
	telemetryRepo            *horizonrepo.HorizonTelemetryRepository
	realtimePublisher        *realtime.Publisher
	vertexSearch             *ai.VertexAISearchClient
	strategicRecommendations *horizonrecommendations.Service
	signalLanding            *horizonsignallanding.Service
	impactWarmupHook         func(context.Context, string, []string, string) error
	watchTriggerHook         func(context.Context, string, string, horizonmodels.WatchTriggerType, string, float64, float64, map[string]any) error
	exec                     *ai.AITaskExecutor
	mapActivityEngine        *mapactivity.Engine
	seedInjector             *signals.SeedInjector
	propagationEngine        *horizonpropagation.PropagationEngine
	profileLookup            *horizonpropagation.ProfileLookup
	logger                   *log.Logger
	window                   time.Duration
	waveThrottleWindow       time.Duration
	enableAIClassify         bool
	enableAIBriefs           bool
	enableLLMEventGate       bool
	llmEventGateModel        string
	llmEventGateMaxPerBatch  int
	eventGateMode            discoverynlp.Mode
	entityExtractMode        discoverynlp.Mode
	eventGate                discoverynlp.EventGate
	entityExtractor          discoverynlp.EntityExtractor

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
		factsRepo:               factsRepo,
		fragmentsRepo:           fragmentsRepo,
		edgesRepo:               edgesRepo,
		exec:                    exec,
		mapActivityEngine:       mapactivity.NewEngine(factsRepo, logger),
		seedInjector:            seedInjector,
		propagationEngine:       propagationEngine,
		profileLookup:           profileLookup,
		logger:                  logger,
		window:                  24 * time.Hour,
		waveThrottleWindow:      30 * time.Minute,
		enableAIClassify:        envBoolDefaultFalse("HORIZON_MONITOR_ENABLE_AI_CLASSIFICATION"),
		enableAIBriefs:          envBoolDefaultFalse("HORIZON_MONITOR_ENABLE_AI_BRIEFS"),
		enableLLMEventGate:      envBoolDefaultFalse("HORIZON_MONITOR_ENABLE_LLM_EVENT_ADJUDICATION"),
		llmEventGateModel:       firstNonEmptyString(strings.TrimSpace(os.Getenv("HORIZON_MONITOR_LLM_EVENT_MODEL")), "gemini-3.1-flash-lite-preview"),
		llmEventGateMaxPerBatch: envIntOrDefault("HORIZON_MONITOR_LLM_EVENT_ADJUDICATION_MAX_PER_BATCH", 6),
		eventGateMode:           discoverynlp.NormalizeMode(strings.ToLower(strings.TrimSpace(os.Getenv("EVENT_GATE_MODE"))), discoverynlp.ModeRules),
		entityExtractMode:       discoverynlp.NormalizeMode(strings.ToLower(strings.TrimSpace(os.Getenv("ENTITY_EXTRACT_MODE"))), discoverynlp.ModeRules),
		eventGate:               discoverynlp.NewHTTPEventGate(strings.TrimSpace(os.Getenv("EVENT_GATE_SERVICE_URL")), envDurationOrDefault("EVENT_GATE_TIMEOUT_MS", 1200*time.Millisecond)),
		entityExtractor:         discoverynlp.NewHTTPEntityExtractor(strings.TrimSpace(os.Getenv("ENTITY_EXTRACT_SERVICE_URL")), envDurationOrDefault("ENTITY_EXTRACT_TIMEOUT_MS", 2*time.Second)),
		recentWaves:             make(map[string]time.Time),
		lastBriefByCC:           make(map[string]time.Time),
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

func (p *HorizonMonitorPipeline) WithStrategicRecommendations(service *horizonrecommendations.Service) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.strategicRecommendations = service
	return p
}

func (p *HorizonMonitorPipeline) WithWatchTriggerHook(hook func(context.Context, string, string, horizonmodels.WatchTriggerType, string, float64, float64, map[string]any) error) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.watchTriggerHook = hook
	return p
}

func (p *HorizonMonitorPipeline) WithSignalLanding(service *horizonsignallanding.Service) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.signalLanding = service
	return p
}

func (p *HorizonMonitorPipeline) WithImpactWarmupHook(hook func(context.Context, string, []string, string) error) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.impactWarmupHook = hook
	return p
}

func (p *HorizonMonitorPipeline) WithEventGate(mode discoverynlp.Mode, gate discoverynlp.EventGate) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.eventGateMode = mode
	p.eventGate = gate
	return p
}

func (p *HorizonMonitorPipeline) WithEntityExtractor(mode discoverynlp.Mode, extractor discoverynlp.EntityExtractor) *HorizonMonitorPipeline {
	if p == nil {
		return p
	}
	p.entityExtractMode = mode
	p.entityExtractor = extractor
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
	llmAdjudicationsUsed := p.llmEventGateMaxPerBatch
	qualityCounters := rssHeuristicsQualityCounters{}
	signalCounters := rssSignalFlowCounters{}

	for _, doc := range docs {
		event := p.buildEventFromDocument(ds.ID, feedType, category, signalHint, feedURL, doc)
		if event == nil {
			continue
		}
		classification := classifyHorizonMonitorNewsKeyword(strings.Join([]string{event.Title, event.Summary}, " "))
		classification = normalizeClassificationWithSourceHint(classification, event)
		if p.exec != nil && p.enableAIClassify && mode == wmIngestModeOntologyFact && classification.Confidence < 0.65 && aiClassificationsUsed < 4 {
			if refined, ok := p.classifyHorizonMonitorNewsWithAI(ctx, event.Title); ok {
				classification = refined
				aiClassificationsUsed++
			}
		}
		nlpDoc := p.buildNLPDocumentInput(ds, event, doc.FullText, doc.Language)
		articleAssessment, llmAdj := p.assessArticleWithNLP(ctx, nlpDoc, classification, &llmAdjudicationsUsed)
		entityExtraction := p.extractEntitiesWithNLP(nlpDoc)
		p.applyNLPResultToEvent(event, articleAssessment, entityExtraction, llmAdj)
		if err := p.upsertTrackFragment(ctx, tenantID, event); err != nil {
			p.logf("horizon monitor pipeline: track upsert failed source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		var signalAudit *horizonMonitorSignalFlowAudit

		if mode == wmIngestModeOntologyFact {
			eventFact, err := p.upsertEventFact(ctx, tenantID, ds.ID, event)
			if err != nil {
				p.logf("horizon monitor pipeline: failed to upsert event fact source=%s feedType=%s ext=%s: %v", ds.ID, event.FeedType, event.ExternalID, err)
			} else {
				forceConcrete := shouldForceConcreteIncidentHandling(event, classification, articleAssessment)
				if shouldSuppressHeuristicSignalPropagation(event) || (!forceConcrete && articleAssessment != nil && !articleAssessment.IsEvent && !articleAssessment.ShouldOnlySignal && !articleAssessment.ShouldWarmExisting) {
					qualityCounters.signalSuppressedTotal++
				}
				if signal := p.buildSignalDescriptor(eventFact, event); signal != nil {
					if p.signalLanding != nil {
						if landingResult, err := p.signalLanding.LandEvent(ctx, eventFact, []*horizonmodels.SignalDescriptor{signal}); err != nil {
							p.logf("horizon monitor pipeline: signal landing failed source=%s signal=%s: %v", ds.ID, signal.ID, err)
						} else {
							p.maybeEnqueueWarmup(ctx, tenantID, landingResult)
						}
					}
					if forceConcrete || articleAssessment == nil || articleAssessment.IsEvent || articleAssessment.ShouldOnlySignal || articleAssessment.ShouldWarmExisting {
						audit, err := p.upsertAndPropagateSignal(ctx, tenantID, ds.ID, signal, eventFact)
						signalAudit = audit
						signalCounters.observe(audit)
						if err != nil {
							p.logf("horizon monitor pipeline: signal propagation failed source=%s signal=%s: %v", ds.ID, signal.ID, err)
						}
					}
				}
			}
		}
		techTopics := detectTechTopicsFromText(strings.Join([]string{event.Title, event.Summary}, " "))
		if err := p.upsertNewsClassificationFact(ctx, tenantID, ds.ID, event, classification, techTopics); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert news classification source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		if err := p.applyAffectedZoneRecoveryObservation(ctx, ds.ID, event); err != nil {
			p.logf("horizon monitor pipeline: failed to apply affected zone recovery source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
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
		var zoneCandidate *affectedZoneCandidate
		if shouldForceConcreteIncidentHandling(event, classification, articleAssessment) || articleAssessment == nil || articleAssessment.ShouldCreateZone || articleAssessment.ShouldWarmExisting {
			zoneCandidate = p.inferAffectedZoneCandidate(ctx, event, classification)
		}
		qualityCounters.observe(classification, zoneCandidate, nonEventFiltered)
		zoneFragment, err := p.upsertAffectedZoneFragmentCandidate(ctx, tenantID, ds.ID, event, classification, zoneCandidate)
		if err != nil {
			p.logf("horizon monitor pipeline: failed to upsert affected zone source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		p.maybeCreateStrategicGapRecommendation(ctx, tenantID, "", "", zoneFragment, event, signalAudit)
		p.emitWatchRuntimeSignals(ctx, tenantID, zoneFragment, event, signalAudit)

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
	if err := p.coolAffectedZones(ctx, tenantID, ds.ID, affectedZoneCoolingLimit); err != nil {
		p.logf("horizon monitor pipeline: failed to cool affected zones: %v", err)
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
	llmAdjudicationsUsed := p.llmEventGateMaxPerBatch
	qualityCounters := rssHeuristicsQualityCounters{}
	signalCounters := rssSignalFlowCounters{}
	for _, event := range events {
		if event == nil {
			continue
		}
		classification := classifyHorizonMonitorNewsKeyword(strings.Join([]string{event.Title, event.Summary}, " "))
		classification = normalizeClassificationWithSourceHint(classification, event)
		if p.exec != nil && p.enableAIClassify && mode == wmIngestModeOntologyFact && classification.Confidence < 0.65 && aiClassificationsUsed < 4 {
			if refined, ok := p.classifyHorizonMonitorNewsWithAI(ctx, event.Title); ok {
				classification = refined
				aiClassificationsUsed++
			}
		}
		nlpDoc := p.buildNLPDocumentInput(ds, event, event.Summary, "")
		articleAssessment, llmAdj := p.assessArticleWithNLP(ctx, nlpDoc, classification, &llmAdjudicationsUsed)
		entityExtraction := p.extractEntitiesWithNLP(nlpDoc)
		p.applyNLPResultToEvent(event, articleAssessment, entityExtraction, llmAdj)
		if err := p.upsertTrackFragment(ctx, tenantID, event); err != nil {
			p.logf("horizon monitor pipeline: track upsert failed source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		var signalAudit *horizonMonitorSignalFlowAudit
		if mode == wmIngestModeOntologyFact {
			eventFact, err := p.upsertEventFact(ctx, tenantID, ds.ID, event)
			if err != nil {
				p.logf("horizon monitor pipeline: failed to upsert event fact source=%s feedType=%s ext=%s: %v", ds.ID, event.FeedType, event.ExternalID, err)
			} else {
				forceConcrete := shouldForceConcreteIncidentHandling(event, classification, articleAssessment)
				if shouldSuppressHeuristicSignalPropagation(event) || (!forceConcrete && articleAssessment != nil && !articleAssessment.IsEvent && !articleAssessment.ShouldOnlySignal && !articleAssessment.ShouldWarmExisting) {
					qualityCounters.signalSuppressedTotal++
				}
				if signal := p.buildSignalDescriptor(eventFact, event); signal != nil {
					if p.signalLanding != nil {
						if landingResult, err := p.signalLanding.LandEvent(ctx, eventFact, []*horizonmodels.SignalDescriptor{signal}); err != nil {
							p.logf("horizon monitor pipeline: signal landing failed source=%s signal=%s: %v", ds.ID, signal.ID, err)
						} else {
							p.maybeEnqueueWarmup(ctx, tenantID, landingResult)
						}
					}
					if forceConcrete || articleAssessment == nil || articleAssessment.IsEvent || articleAssessment.ShouldOnlySignal || articleAssessment.ShouldWarmExisting {
						audit, err := p.upsertAndPropagateSignal(ctx, tenantID, ds.ID, signal, eventFact)
						signalAudit = audit
						signalCounters.observe(audit)
						if err != nil {
							p.logf("horizon monitor pipeline: signal propagation failed source=%s signal=%s: %v", ds.ID, signal.ID, err)
						}
					}
				}
			}
		}
		techTopics := detectTechTopicsFromText(strings.Join([]string{event.Title, event.Summary}, " "))
		if err := p.upsertNewsClassificationFact(ctx, tenantID, ds.ID, event, classification, techTopics); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert news classification source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		if err := p.applyAffectedZoneRecoveryObservation(ctx, ds.ID, event); err != nil {
			p.logf("horizon monitor pipeline: failed to apply affected zone recovery source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
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
		var zoneCandidate *affectedZoneCandidate
		if shouldForceConcreteIncidentHandling(event, classification, articleAssessment) || articleAssessment == nil || articleAssessment.ShouldCreateZone || articleAssessment.ShouldWarmExisting {
			zoneCandidate = p.inferAffectedZoneCandidate(ctx, event, classification)
		}
		qualityCounters.observe(classification, zoneCandidate, nonEventFiltered)
		zoneFragment, err := p.upsertAffectedZoneFragmentCandidate(ctx, tenantID, ds.ID, event, classification, zoneCandidate)
		if err != nil {
			p.logf("horizon monitor pipeline: failed to upsert affected zone source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
		}
		p.maybeCreateStrategicGapRecommendation(ctx, tenantID, "", "", zoneFragment, event, signalAudit)
		p.emitWatchRuntimeSignals(ctx, tenantID, zoneFragment, event, signalAudit)
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
	if err := p.coolAffectedZones(ctx, tenantID, ds.ID, affectedZoneCoolingLimit); err != nil {
		p.logf("horizon monitor pipeline: failed to cool affected zones: %v", err)
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

func envDurationOrDefault(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return fallback
}

func envIntOrDefault(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func mergeStringAnyMaps(parts ...map[string]any) map[string]any {
	out := make(map[string]any)
	for _, part := range parts {
		for key, value := range part {
			key = strings.TrimSpace(key)
			if key == "" || value == nil {
				continue
			}
			out[key] = value
		}
	}
	return out
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
		SourceID:   sanitizeHorizonMonitorText(sourceID),
		FeedType:   sanitizeHorizonMonitorText(feedType),
		Category:   sanitizeHorizonMonitorText(category),
		ExternalID: sanitizeHorizonMonitorText(firstNonEmptyString(strings.TrimSpace(doc.ExternalID), shortHash(feedURL+"|"+doc.Title+"|"+doc.FullText))),
		Title:      sanitizeHorizonMonitorText(firstNonEmptyString(strings.TrimSpace(doc.Title), "Horizon Monitor event")),
		Summary:    sanitizeHorizonMonitorText(doc.FullText),
		URL:        sanitizeHorizonMonitorText(firstNonEmptyString(strings.TrimSpace(doc.URL), strings.TrimSpace(feedURL))),
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
		e.Title = sanitizeHorizonMonitorText(title)
	}
	if summary := firstNonEmptyString(
		asString(raw["description"]),
		asString(raw["summary"]),
		asString(raw["text"]),
	); summary != "" {
		e.Summary = sanitizeHorizonMonitorText(summary)
	}
	if url := firstNonEmptyString(asString(raw["url"]), asString(raw["link"]), asString(raw["sourceUrl"])); url != "" {
		e.URL = sanitizeHorizonMonitorText(url)
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
		e.CountryName = sanitizeHorizonMonitorText(firstNonEmptyString(asString(raw["country"]), countryNameFromCode(countryCode), countryCode))
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

func (p *HorizonMonitorPipeline) buildNLPDocumentInput(ds *coremodels.DataSource, event *horizonMonitorFeedEvent, body, language string) discoverynlp.DocumentInput {
	host := affectedZoneSourceHost(event)
	sourceType := ""
	categoryHint := ""
	if ds != nil {
		sourceType = strings.TrimSpace(ds.Kind)
		categoryHint = strings.TrimSpace(firstNonEmptyString(
			stringPointerValue(ds.Details.HorizonMonitorCategory),
			stringPointerValue(ds.Details.Category),
			stringPointerValue(ds.PrimarySourceCategoryPath),
		))
	}
	var publishedAt *time.Time
	if event != nil && !event.OccurredAt.IsZero() {
		ts := event.OccurredAt.UTC()
		publishedAt = &ts
	}
	metadata := map[string]interface{}{}
	if event != nil {
		if event.CountryCode != "" {
			metadata["countryCode"] = event.CountryCode
		}
		if event.CountryName != "" {
			metadata["countryName"] = event.CountryName
		}
		if event.FeedType != "" {
			metadata["feedType"] = event.FeedType
		}
		if event.Category != "" {
			metadata["eventCategory"] = event.Category
		}
		if event.SignalType != "" {
			metadata["signalType"] = event.SignalType
		}
	}
	return discoverynlp.DocumentInput{
		SourceID:     strings.TrimSpace(event.SourceID),
		SourceHost:   host,
		SourceURL:    strings.TrimSpace(event.URL),
		Title:        strings.TrimSpace(event.Title),
		Summary:      strings.TrimSpace(event.Summary),
		Body:         strings.TrimSpace(body),
		URL:          strings.TrimSpace(event.URL),
		PublishedAt:  publishedAt,
		Language:     strings.TrimSpace(language),
		SourceType:   sourceType,
		CategoryHint: categoryHint,
		Metadata:     metadata,
	}
}

func stringPointerValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func asFloat64(v interface{}) (float64, bool) {
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
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func (p *HorizonMonitorPipeline) ruleArticleAssessment(doc discoverynlp.DocumentInput, cls horizonMonitorNewsClassification) *discoverynlp.ArticleAssessment {
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(doc.Title),
		strings.TrimSpace(doc.Summary),
		strings.TrimSpace(doc.Body),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	host := strings.TrimSpace(doc.SourceHost)
	profile := wmHeuristics().ProfileArticle(text, host)
	isAnalysis := profile.likelyAnalysisRoundup()
	isAdvisory := profile.likelyAdvisoryCampaignWithoutOutage()
	isBroad := profile.likelyBroadRegionalRoundup()
	isNoise := isLikelyNonEventText(text)
	warmOnly := looksLikeWarmConflictPosture(text, cls)
	semanticSuppressed := shouldSuppressAffectedZoneBySemanticDrift(text, nil, cls)
	profileSuppressed := shouldSuppressAffectedZoneByEventProfile(text, nil, cls, "", profile)
	exerciseLike := looksLikeExerciseWithoutIncidentText(text)
	forcePostureLike := looksLikeForcePostureDeploymentText(text, cls)
	drugInterdictionLike := looksLikeDrugInterdictionText(text)
	administrativeCommentaryLike := looksLikeAdministrativeCommentaryText(text)
	liveBlogLike := looksLikeLiveBlogPoliticalCommentaryText(text)
	civilIncidentLike := looksLikeCivilIncidentWithoutMilitaryContext(text)
	cyberCampaignLike := looksLikeCyberCampaignWithoutOperationalImpact(text)
	multiCountryRoundup := looksLikeMultiCountryDeploymentRoundup(text, cls)
	retrospectiveLike := looksLikeRetrospectiveConflictNarrativeText(text)
	legalCommercialLike := looksLikeLegalCommercialDisputeText(text)
	sportsPoliticalLike := looksLikeSportsPoliticalHumanInterestText(text)
	explainerQuestionLike := looksLikeExplainerQuestionText(text)
	concreteIncident := looksLikeConcreteOperationalIncident(text, cls)
	if semanticSuppressed || profileSuppressed || exerciseLike || forcePostureLike || drugInterdictionLike || administrativeCommentaryLike || liveBlogLike || civilIncidentLike || cyberCampaignLike || multiCountryRoundup || retrospectiveLike || legalCommercialLike || sportsPoliticalLike || explainerQuestionLike {
		isAnalysis = isAnalysis || semanticSuppressed || exerciseLike || forcePostureLike || administrativeCommentaryLike || liveBlogLike || multiCountryRoundup || retrospectiveLike || legalCommercialLike || sportsPoliticalLike || explainerQuestionLike
		isAdvisory = isAdvisory || drugInterdictionLike || cyberCampaignLike
		isBroad = isBroad || multiCountryRoundup
		isNoise = isNoise || civilIncidentLike
	}
	if concreteIncident {
		isAnalysis = false
		isAdvisory = false
		isBroad = false
		isNoise = false
	}
	shouldCreateZone := !isNoise && !isAnalysis && !isAdvisory && !isBroad
	if concreteIncident {
		shouldCreateZone = true
	} else if warmOnly {
		shouldCreateZone = true
	}
	isEvent := shouldCreateZone || warmOnly
	eventClass := "real_event"
	rejectionReason := ""
	reasons := make([]string, 0, 4)
	switch {
	case isNoise:
		eventClass = "noise"
		rejectionReason = "non_event_text"
		reasons = append(reasons, "non_event_text")
	case isAnalysis:
		eventClass = "analysis"
		rejectionReason = "analysis_roundup"
		reasons = append(reasons, "analysis_roundup")
	case isAdvisory:
		eventClass = "advisory"
		rejectionReason = "advisory_without_outage"
		reasons = append(reasons, "advisory_without_outage")
	case isBroad:
		eventClass = "broad_scope"
		rejectionReason = "broad_scope_without_incident"
		reasons = append(reasons, "broad_scope_without_incident")
	}
	if concreteIncident {
		eventClass = "real_event"
		rejectionReason = ""
		reasons = append(reasons, "concrete_incident_override")
	}
	if administrativeCommentaryLike {
		reasons = append(reasons, "administrative_commentary")
	}
	if exerciseLike {
		reasons = append(reasons, "exercise_without_incident")
	}
	if forcePostureLike {
		reasons = append(reasons, "force_posture_without_incident")
	}
	if drugInterdictionLike {
		reasons = append(reasons, "drug_interdiction")
	}
	if liveBlogLike {
		reasons = append(reasons, "live_blog_commentary")
	}
	if civilIncidentLike {
		reasons = append(reasons, "civil_incident_without_military_context")
	}
	if cyberCampaignLike {
		reasons = append(reasons, "cyber_campaign_without_operational_impact")
	}
	if multiCountryRoundup {
		reasons = append(reasons, "multi_country_roundup")
	}
	if retrospectiveLike {
		reasons = append(reasons, "retrospective_or_feature_conflict")
	}
	if legalCommercialLike {
		reasons = append(reasons, "legal_or_commercial_dispute")
	}
	if sportsPoliticalLike {
		reasons = append(reasons, "sports_or_human_interest_politics")
	}
	if explainerQuestionLike {
		reasons = append(reasons, "explainer_question")
	}
	if warmOnly {
		reasons = append(reasons, "warm_conflict_posture")
	}
	confidence := 0.68 + cls.Confidence*0.22
	if shouldCreateZone {
		confidence = wmClamp01(math.Max(confidence, 0.76))
	}
	if rejectionReason != "" {
		confidence = wmClamp01(math.Max(confidence, 0.72))
	}
	return &discoverynlp.ArticleAssessment{
		IsEvent:             isEvent,
		EventClass:          eventClass,
		EventConfidence:     confidence,
		AnalysisScore:       ternaryFloat64(isAnalysis, 0.86, 0.12),
		AdvisoryScore:       ternaryFloat64(isAdvisory, 0.88, 0.10),
		BroadScopeScore:     ternaryFloat64(isBroad, 0.84, 0.15),
		ConcreteOutageScore: ternaryFloat64(profile.ConcreteOutageScore >= 1.15, 0.82, 0.18),
		ShouldCreateZone:    shouldCreateZone,
		ShouldWarmExisting:  warmOnly,
		ShouldOnlySignal:    !shouldCreateZone && !warmOnly && !isNoise && rejectionReason == "" && !semanticSuppressed && !profileSuppressed && !administrativeCommentaryLike && !exerciseLike && !drugInterdictionLike && !civilIncidentLike && !multiCountryRoundup,
		RejectionReason:     rejectionReason,
		Reasons:             reasons,
		Raw: map[string]interface{}{
			"mode":             "rules",
			"sourceHost":       host,
			"classification":   strings.TrimSpace(cls.Category),
			"classificationLv": strings.TrimSpace(cls.Level),
		},
	}
}

func (p *HorizonMonitorPipeline) assessArticleWithNLP(ctx context.Context, doc discoverynlp.DocumentInput, cls horizonMonitorNewsClassification, llmBudget *int) (*discoverynlp.ArticleAssessment, *horizonMonitorLLMArticleAdjudication) {
	rule := p.ruleArticleAssessment(doc, cls)
	var llmAdj *horizonMonitorLLMArticleAdjudication
	base := rule
	if p == nil || p.eventGate == nil || p.eventGateMode == discoverynlp.ModeRules {
		base = rule
	} else {
		model, err := p.eventGate.Assess(doc)
		if err != nil || model == nil {
			p.logf("horizon monitor pipeline: event gate fallback host=%s title=%q err=%v", doc.SourceHost, trimForLog(doc.Title, 80), err)
			base = rule
		} else {
			switch p.eventGateMode {
			case discoverynlp.ModeShadow:
				if rule.EventClass != model.EventClass || rule.ShouldCreateZone != model.ShouldCreateZone {
					p.logf("horizon monitor pipeline: event gate shadow divergence host=%s rule=%s model=%s title=%q", doc.SourceHost, rule.EventClass, model.EventClass, trimForLog(doc.Title, 80))
				}
				base = rule
			case discoverynlp.ModeModel:
				base = normalizeArticleAssessment(model, rule)
			case discoverynlp.ModeHybrid:
				merged := mergeArticleAssessments(doc, rule, model)
				if merged != nil && (rule.EventClass != model.EventClass ||
					rule.ShouldCreateZone != merged.ShouldCreateZone ||
					rule.ShouldWarmExisting != merged.ShouldWarmExisting ||
					rule.ShouldOnlySignal != merged.ShouldOnlySignal) {
					p.logf(
						"horizon monitor pipeline: event gate hybrid host=%s rule=%s model=%s final=%s create=%t warm=%t signal=%t title=%q",
						doc.SourceHost,
						rule.EventClass,
						model.EventClass,
						merged.EventClass,
						merged.ShouldCreateZone,
						merged.ShouldWarmExisting,
						merged.ShouldOnlySignal,
						trimForLog(doc.Title, 96),
					)
				}
				base = merged
			default:
				base = rule
			}
		}
	}
	if llmRemaining(llmBudget) > 0 {
		if adjudication, ok := p.assessArticleWithLLM(ctx, doc, cls, base); ok && adjudication != nil {
			llmAdj = adjudication
			if merged := mergeArticleAssessmentWithLLM(doc, base, adjudication); merged != nil {
				base = merged
			}
			decrementLLMBudget(llmBudget)
		}
	}
	return base, llmAdj
}

func (p *HorizonMonitorPipeline) extractEntitiesWithNLP(doc discoverynlp.DocumentInput) *discoverynlp.EntityExtractionResult {
	if p == nil || p.entityExtractor == nil || p.entityExtractMode == discoverynlp.ModeRules {
		return nil
	}
	res, err := p.entityExtractor.Extract(doc)
	if err != nil || res == nil || len(res.Entities) == 0 {
		if err != nil {
			p.logf("horizon monitor pipeline: entity extract fallback host=%s title=%q err=%v", doc.SourceHost, trimForLog(doc.Title, 80), err)
		}
		return nil
	}
	return res
}

func llmRemaining(budget *int) int {
	if budget == nil {
		return 0
	}
	return *budget
}

func decrementLLMBudget(budget *int) {
	if budget == nil {
		return
	}
	if *budget > 0 {
		*budget--
	}
}

func (p *HorizonMonitorPipeline) assessArticleWithLLM(ctx context.Context, doc discoverynlp.DocumentInput, cls horizonMonitorNewsClassification, base *discoverynlp.ArticleAssessment) (*horizonMonitorLLMArticleAdjudication, bool) {
	if p == nil || p.exec == nil || !p.enableLLMEventGate {
		return nil, false
	}
	if !shouldEscalateArticleToLLM(doc, cls, base) {
		return nil, false
	}
	prompt := strings.Builder{}
	prompt.WriteString("You classify one news document for a geopolitical affected-zone pipeline.\n")
	prompt.WriteString("Goal: distinguish real operational events from analysis/advisory/broad_scope/noise.\n")
	prompt.WriteString("Concrete blackouts, cyber outages, transport shutdowns, strikes with operational stoppage, outbreaks, toxic spills, evacuations, port closures, rail closures, and direct physical disruptions are real events.\n")
	prompt.WriteString("Pure explainers, live blogs, weekly roundups, forward-looking policy commentary, and abstract risk analysis are not real events.\n")
	prompt.WriteString("Return strict JSON only.\n")
	prompt.WriteString("\nDocument:\n")
	prompt.WriteString("sourceHost: " + strings.TrimSpace(doc.SourceHost) + "\n")
	prompt.WriteString("title: " + strings.TrimSpace(doc.Title) + "\n")
	prompt.WriteString("summary: " + truncateString(strings.TrimSpace(doc.Summary), 800) + "\n")
	prompt.WriteString("categoryHint: " + strings.TrimSpace(cls.Category) + "\n")
	prompt.WriteString("levelHint: " + strings.TrimSpace(cls.Level) + "\n")
	if base != nil {
		prompt.WriteString("\nWeak label:\n")
		prompt.WriteString(fmt.Sprintf("isEvent=%t eventClass=%s eventConfidence=%.2f shouldCreateZone=%t shouldWarmExisting=%t shouldOnlySignal=%t rejectionReason=%s\n",
			base.IsEvent,
			strings.TrimSpace(base.EventClass),
			base.EventConfidence,
			base.ShouldCreateZone,
			base.ShouldWarmExisting,
			base.ShouldOnlySignal,
			strings.TrimSpace(base.RejectionReason),
		))
	}
	prompt.WriteString("\nAllowed eventClass: real_event, analysis, advisory, broad_scope, noise\n")
	prompt.WriteString("Allowed conditionType: military_conflict, infrastructure_disruption, cyber_disruption, political_tension, humanitarian_crisis, disease_outbreak, environmental_hazard, natural_disaster, earthquake_affected, flood_zone, wildfire_zone, none\n")
	prompt.WriteString("Schema: {\"schemaVersion\": string, \"isEvent\": bool, \"eventClass\": string, \"eventConfidence\": number, \"shouldCreateZone\": bool, \"shouldWarmExisting\": bool, \"shouldOnlySignal\": bool, \"conditionType\": string, \"place\": string, \"countryCode\": string, \"geoConfidence\": number, \"reason\": string}\n")

	verifier := ai.NewGenericVerifier(func(out horizonMonitorLLMArticleAdjudication) error {
		switch strings.TrimSpace(out.EventClass) {
		case "real_event", "analysis", "advisory", "broad_scope", "noise":
		default:
			return fmt.Errorf("invalid eventClass")
		}
		if out.EventConfidence < 0 || out.EventConfidence > 1 {
			return fmt.Errorf("eventConfidence out of range")
		}
		if out.GeoConfidence < 0 || out.GeoConfidence > 1 {
			return fmt.Errorf("geoConfidence out of range")
		}
		switch strings.TrimSpace(out.ConditionType) {
		case "", "none", "military_conflict", "infrastructure_disruption", "cyber_disruption", "political_tension", "humanitarian_crisis", "disease_outbreak", "environmental_hazard", "natural_disaster", "earthquake_affected", "flood_zone", "wildfire_zone":
		default:
			return fmt.Errorf("invalid conditionType")
		}
		if cc := normalizeCountryCode(out.CountryCode); cc != "" && len(cc) != 2 {
			return fmt.Errorf("invalid countryCode")
		}
		if strings.TrimSpace(out.SchemaVersion) == "" {
			return fmt.Errorf("schemaVersion is required")
		}
		return nil
	})
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContextAnalysis,
		InputContent: prompt.String(),
		AIRequirements: ai.AIRequirements{
			PreferredModel:           strings.TrimSpace(p.llmEventGateModel),
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         2500,
			MaxOutputSize:            260,
		},
		Verifier: verifier,
	}
	parsed, usedModel, err := p.exec.ExecuteStructuredTaskWithModel(ctx, task)
	if err != nil {
		p.logf("horizon monitor pipeline: llm adjudication failed host=%s title=%q err=%v", doc.SourceHost, trimForLog(doc.Title, 90), err)
		return nil, false
	}
	out, ok := parsed.(horizonMonitorLLMArticleAdjudication)
	if !ok {
		return nil, false
	}
	out.SchemaVersion = strings.TrimSpace(out.SchemaVersion)
	out.EventClass = strings.TrimSpace(out.EventClass)
	out.ConditionType = strings.TrimSpace(out.ConditionType)
	out.CountryCode = normalizeCountryCode(out.CountryCode)
	out.Place = strings.TrimSpace(out.Place)
	out.Reason = strings.TrimSpace(out.Reason)
	out.EventConfidence = wmClamp01(out.EventConfidence)
	out.GeoConfidence = wmClamp01(out.GeoConfidence)
	if usedModel != "" {
		p.logf("horizon monitor pipeline: llm adjudication host=%s class=%s create=%t conf=%.2f model=%s title=%q", doc.SourceHost, out.EventClass, out.ShouldCreateZone, out.EventConfidence, usedModel, trimForLog(doc.Title, 96))
	}
	return &out, true
}

func shouldEscalateArticleToLLM(doc discoverynlp.DocumentInput, cls horizonMonitorNewsClassification, base *discoverynlp.ArticleAssessment) bool {
	if base == nil {
		return true
	}
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(doc.Title),
		strings.TrimSpace(doc.Summary),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	docText := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(doc.Title),
		strings.TrimSpace(doc.Summary),
	}, " "))
	if text == "" {
		return false
	}
	if !base.IsEvent && base.AnalysisScore >= 0.90 && base.EventConfidence >= 0.85 {
		return false
	}
	if hasAnyKeyword(text,
		"blackout", "power outage", "grid outage", "telecom outage", "internet outage", "network outage",
		"cyber attack", "cyberattack", "ransomware", "data center outage", "offline",
		"chemical spill", "toxic spill", "toxic leak", "gas leak", "oil spill", "water shutdown", "contamination",
		"dock strike", "dockworkers strike", "dock workers strike", "workers strike", "general strike", "walkout",
		"cargo handling halted", "cargo handling suspended", "rail service suspended", "train service suspended",
		"outbreak", "epidemic", "pandemic", "cholera", "measles", "mpox", "dengue", "health emergency",
		"evacuation ordered", "evacuations ordered",
	) {
		return true
	}
	if looksLikeAmbiguousViolentIncidentText(docText) {
		return true
	}
	if base.ShouldWarmExisting || base.ShouldOnlySignal {
		return true
	}
	if base.EventConfidence < 0.82 {
		return true
	}
	if base.ConcreteOutageScore >= 0.38 && !base.ShouldCreateZone {
		return true
	}
	if strings.TrimSpace(base.EventClass) == "analysis" && hasAnyKeyword(text,
		"port", "terminal", "grid", "telecom", "internet", "pipeline", "airport", "rail", "metro", "river", "spill",
	) {
		return true
	}
	return false
}

func looksLikeAmbiguousViolentIncidentText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if !hasAnyKeyword(text,
		"killed", "kill", "dead", "deaths", "injured", "wounded",
		"fire", "blaze", "burned", "burnt", "explosion", "blast", "crash",
		"bus", "airport", "school", "building", "train", "station",
	) {
		return false
	}
	if hasAnyKeyword(text,
		"missile", "rocket", "drone", "airstrike", "strike", "shelling", "bombardment",
		"military", "army", "navy", "air force", "airbase", "base", "troops", "embassy",
		"warship", "fighter jet", "fighter", "hezbollah", "houthi", "houthis", "rebels", "militants",
		"israel", "iran", "gaza", "baghdad", "hormuz", "bab el mandeb",
	) {
		return false
	}
	return true
}

func articleAssessmentFromLLM(adjudication *horizonMonitorLLMArticleAdjudication, fallback *discoverynlp.ArticleAssessment) *discoverynlp.ArticleAssessment {
	if adjudication == nil {
		return fallback
	}
	eventClass := strings.TrimSpace(adjudication.EventClass)
	if eventClass == "" {
		eventClass = "analysis"
	}
	out := &discoverynlp.ArticleAssessment{
		IsEvent:            adjudication.IsEvent,
		EventClass:         firstNonEmptyString(eventClass, "analysis"),
		EventConfidence:    wmClamp01(adjudication.EventConfidence),
		ShouldCreateZone:   adjudication.ShouldCreateZone,
		ShouldWarmExisting: adjudication.ShouldWarmExisting,
		ShouldOnlySignal:   adjudication.ShouldOnlySignal,
		RejectionReason:    firstNonEmptyString(boolToRejectionReason(adjudication.IsEvent, adjudication.EventClass), ""),
		Reasons:            []string{"llm:event_adjudication"},
		Raw:                map[string]interface{}{"backend": "llm_event_adjudication", "schemaVersion": adjudication.SchemaVersion},
	}
	switch out.EventClass {
	case "analysis":
		out.AnalysisScore = math.Max(out.AnalysisScore, out.EventConfidence)
	case "advisory":
		out.AdvisoryScore = math.Max(out.AdvisoryScore, out.EventConfidence)
	case "broad_scope":
		out.BroadScopeScore = math.Max(out.BroadScopeScore, out.EventConfidence)
	case "real_event":
		out.ConcreteOutageScore = math.Max(out.ConcreteOutageScore, out.EventConfidence)
	}
	if fallback != nil {
		out.AnalysisScore = math.Max(out.AnalysisScore, fallback.AnalysisScore)
		out.AdvisoryScore = math.Max(out.AdvisoryScore, fallback.AdvisoryScore)
		out.BroadScopeScore = math.Max(out.BroadScopeScore, fallback.BroadScopeScore)
		out.ConcreteOutageScore = math.Max(out.ConcreteOutageScore, fallback.ConcreteOutageScore)
	}
	return out
}

func mergeArticleAssessmentWithLLM(doc discoverynlp.DocumentInput, base *discoverynlp.ArticleAssessment, adjudication *horizonMonitorLLMArticleAdjudication) *discoverynlp.ArticleAssessment {
	if adjudication == nil {
		return base
	}
	llm := articleAssessmentFromLLM(adjudication, base)
	if base == nil {
		return llm
	}
	out := *base
	if !llm.IsEvent && llm.EventConfidence >= 0.84 {
		out.IsEvent = false
		out.ShouldCreateZone = false
		out.ShouldWarmExisting = false
		out.ShouldOnlySignal = false
		out.EventClass = firstNonEmptyString(llm.EventClass, out.EventClass)
		out.RejectionReason = firstNonEmptyString(llm.RejectionReason, out.RejectionReason)
	}
	if llm.IsEvent && llm.EventConfidence >= 0.88 && (base.EventConfidence < 0.90 || !base.ShouldCreateZone) {
		out.IsEvent = true
		out.EventClass = firstNonEmptyString(llm.EventClass, out.EventClass)
		out.ShouldCreateZone = llm.ShouldCreateZone || out.ShouldCreateZone
		out.ShouldWarmExisting = llm.ShouldWarmExisting || out.ShouldWarmExisting
		out.ShouldOnlySignal = llm.ShouldOnlySignal || out.ShouldOnlySignal
		out.RejectionReason = ""
	}
	out.EventConfidence = wmClamp01(math.Max(out.EventConfidence, llm.EventConfidence))
	out.AnalysisScore = math.Max(out.AnalysisScore, llm.AnalysisScore)
	out.AdvisoryScore = math.Max(out.AdvisoryScore, llm.AdvisoryScore)
	out.BroadScopeScore = math.Max(out.BroadScopeScore, llm.BroadScopeScore)
	out.ConcreteOutageScore = math.Max(out.ConcreteOutageScore, llm.ConcreteOutageScore)
	out.Reasons = uniqueStrings(append(append([]string{}, out.Reasons...), llm.Reasons...))
	if out.Raw == nil {
		out.Raw = map[string]interface{}{}
	}
	out.Raw["llm"] = llm.Raw
	out.Raw["llmReason"] = adjudication.Reason
	out.Raw["llmGeoConfidence"] = adjudication.GeoConfidence
	if base.Raw != nil {
		out.Raw["base"] = base.Raw
	}
	if shouldLLMGeoHintsApply(adjudication) {
		out.Raw["llmPlace"] = adjudication.Place
		out.Raw["llmCountryCode"] = adjudication.CountryCode
		out.Raw["llmConditionType"] = adjudication.ConditionType
	}
	return &out
}

func shouldLLMGeoHintsApply(adjudication *horizonMonitorLLMArticleAdjudication) bool {
	return adjudication != nil &&
		adjudication.IsEvent &&
		adjudication.EventConfidence >= 0.86 &&
		adjudication.GeoConfidence >= 0.82 &&
		strings.TrimSpace(adjudication.Place) != ""
}

func boolToRejectionReason(isEvent bool, eventClass string) string {
	if isEvent {
		return ""
	}
	switch strings.TrimSpace(eventClass) {
	case "analysis":
		return "llm_analysis"
	case "advisory":
		return "llm_advisory"
	case "broad_scope":
		return "llm_broad_scope"
	case "noise":
		return "llm_noise"
	default:
		return "llm_non_event"
	}
}

func (p *HorizonMonitorPipeline) applyNLPResultToEvent(event *horizonMonitorFeedEvent, assessment *discoverynlp.ArticleAssessment, entities *discoverynlp.EntityExtractionResult, llmAdj *horizonMonitorLLMArticleAdjudication) {
	if event == nil {
		return
	}
	if event.Raw == nil {
		event.Raw = map[string]interface{}{}
	}
	if assessment != nil {
		event.Raw["nlpArticleAssessment"] = map[string]interface{}{
			"isEvent":             assessment.IsEvent,
			"eventClass":          assessment.EventClass,
			"eventConfidence":     assessment.EventConfidence,
			"analysisScore":       assessment.AnalysisScore,
			"advisoryScore":       assessment.AdvisoryScore,
			"broadScopeScore":     assessment.BroadScopeScore,
			"concreteOutageScore": assessment.ConcreteOutageScore,
			"shouldCreateZone":    assessment.ShouldCreateZone,
			"shouldWarmExisting":  assessment.ShouldWarmExisting,
			"shouldOnlySignal":    assessment.ShouldOnlySignal,
			"rejectionReason":     assessment.RejectionReason,
			"reasons":             assessment.Reasons,
			"raw":                 assessment.Raw,
		}
	}
	if llmAdj != nil {
		event.Raw["llmArticleAdjudication"] = map[string]interface{}{
			"schemaVersion":      llmAdj.SchemaVersion,
			"isEvent":            llmAdj.IsEvent,
			"eventClass":         llmAdj.EventClass,
			"eventConfidence":    llmAdj.EventConfidence,
			"shouldCreateZone":   llmAdj.ShouldCreateZone,
			"shouldWarmExisting": llmAdj.ShouldWarmExisting,
			"shouldOnlySignal":   llmAdj.ShouldOnlySignal,
			"conditionType":      llmAdj.ConditionType,
			"place":              llmAdj.Place,
			"countryCode":        llmAdj.CountryCode,
			"geoConfidence":      llmAdj.GeoConfidence,
			"reason":             llmAdj.Reason,
		}
	}
	if entities != nil && len(entities.Entities) > 0 {
		out := make([]map[string]interface{}, 0, len(entities.Entities))
		for _, entity := range entities.Entities {
			if strings.TrimSpace(entity.Surface) == "" || strings.TrimSpace(entity.EntityType) == "" {
				continue
			}
			out = append(out, map[string]interface{}{
				"surface":        entity.Surface,
				"normalized":     entity.Normalized,
				"entityType":     entity.EntityType,
				"role":           entity.Role,
				"confidence":     entity.Confidence,
				"countryCode":    entity.CountryCode,
				"geoHint":        entity.GeoHint,
				"linkCandidates": entity.LinkCandidates,
				"attributes":     entity.Attributes,
			})
		}
		if len(out) > 0 {
			key := "nlpEntities"
			if p != nil && p.entityExtractMode == discoverynlp.ModeShadow {
				key = "nlpEntitiesShadow"
			}
			event.Raw[key] = out
			if key == "nlpEntitiesShadow" {
				event.Raw["nlpEntitiesShadowCount"] = len(out)
			}
		}
	}
}

func mergeArticleAssessments(doc discoverynlp.DocumentInput, rule, model *discoverynlp.ArticleAssessment) *discoverynlp.ArticleAssessment {
	if rule == nil {
		return normalizeArticleAssessment(model, nil)
	}
	if model == nil {
		return rule
	}
	out := *rule
	if !rule.IsEvent && model.IsEvent && model.EventConfidence < 0.65 {
		out.EventConfidence = wmClamp01(math.Max(rule.EventConfidence, model.EventConfidence))
		out.AnalysisScore = math.Max(rule.AnalysisScore, model.AnalysisScore)
		out.AdvisoryScore = math.Max(rule.AdvisoryScore, model.AdvisoryScore)
		out.BroadScopeScore = math.Max(rule.BroadScopeScore, model.BroadScopeScore)
		out.ConcreteOutageScore = math.Max(rule.ConcreteOutageScore, model.ConcreteOutageScore)
		out.Reasons = uniqueStrings(append(append([]string{}, rule.Reasons...), append(model.Reasons, "hybrid:rule_guard")...))
		if out.Raw == nil {
			out.Raw = map[string]interface{}{}
		}
		out.Raw["rule"] = rule.Raw
		out.Raw["model"] = model.Raw
		return &out
	}
	if !model.IsEvent && model.EventConfidence >= 0.72 {
		out.IsEvent = false
		out.ShouldCreateZone = false
		if !rule.ShouldWarmExisting {
			out.ShouldWarmExisting = false
		}
		out.ShouldOnlySignal = rule.ShouldOnlySignal && model.ShouldOnlySignal
		out.RejectionReason = firstNonEmptyString(model.RejectionReason, rule.RejectionReason)
		out.EventClass = firstNonEmptyString(model.EventClass, rule.EventClass)
	}
	if shouldModelVetoAnalysisHeavyHost(doc, rule, model) {
		out.IsEvent = false
		out.ShouldCreateZone = false
		out.ShouldWarmExisting = false
		out.ShouldOnlySignal = false
		out.RejectionReason = firstNonEmptyString(model.RejectionReason, "analysis_heavy_host_model_veto")
		out.EventClass = firstNonEmptyString(model.EventClass, rule.EventClass)
		out.Reasons = uniqueStrings(append(out.Reasons, "hybrid:model_host_veto"))
	}
	if rule.ShouldWarmExisting && model.ShouldWarmExisting {
		out.ShouldWarmExisting = true
	}
	out.EventConfidence = wmClamp01(math.Max(rule.EventConfidence, model.EventConfidence))
	out.AnalysisScore = math.Max(rule.AnalysisScore, model.AnalysisScore)
	out.AdvisoryScore = math.Max(rule.AdvisoryScore, model.AdvisoryScore)
	out.BroadScopeScore = math.Max(rule.BroadScopeScore, model.BroadScopeScore)
	out.ConcreteOutageScore = math.Max(rule.ConcreteOutageScore, model.ConcreteOutageScore)
	out.Reasons = uniqueStrings(append(append([]string{}, rule.Reasons...), model.Reasons...))
	if out.Raw == nil {
		out.Raw = map[string]interface{}{}
	}
	out.Raw["rule"] = rule.Raw
	out.Raw["model"] = model.Raw
	return &out
}

func shouldModelVetoAnalysisHeavyHost(doc discoverynlp.DocumentInput, rule, model *discoverynlp.ArticleAssessment) bool {
	host := strings.ToLower(strings.TrimSpace(doc.SourceHost))
	if !isAnalysisHeavyHost(host) {
		return false
	}
	if model == nil || rule == nil {
		return false
	}
	if model.IsEvent {
		return false
	}
	if model.EventConfidence < 0.72 {
		return false
	}
	switch model.EventClass {
	case "analysis", "noise", "broad_scope":
	default:
		return false
	}
	if rule.ShouldWarmExisting || model.ShouldWarmExisting {
		return false
	}
	if rule.ConcreteOutageScore >= 0.72 || model.ConcreteOutageScore >= 0.72 {
		return false
	}
	return true
}

func isAnalysisHeavyHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "warontherocks.com", "realcleardefense.com", "brookings.edu", "csis.org", "chathamhouse.org", "carnegieendowment.org", "ecfr.eu", "foreignpolicy.com", "worldbank.org", "blogs.worldbank.org", "imf.org":
		return true
	default:
		return false
	}
}

func normalizeArticleAssessment(model, fallback *discoverynlp.ArticleAssessment) *discoverynlp.ArticleAssessment {
	if model == nil {
		return fallback
	}
	out := *model
	if out.EventClass == "" && fallback != nil {
		out.EventClass = fallback.EventClass
	}
	if out.EventConfidence == 0 && fallback != nil {
		out.EventConfidence = fallback.EventConfidence
	}
	return &out
}

func ternaryFloat64(cond bool, yes, no float64) float64 {
	if cond {
		return yes
	}
	return no
}

func trimForLog(v string, max int) string {
	v = strings.TrimSpace(v)
	if max <= 0 || len(v) <= max {
		return v
	}
	if max <= 3 {
		return v[:max]
	}
	return v[:max-3] + "..."
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
	blueprint = refineSignalBlueprintForEvent(blueprint, e)
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

func sanitizeHorizonMonitorText(s string) string {
	return strings.ToValidUTF8(strings.TrimSpace(s), "")
}

func refineSignalBlueprintForEvent(blueprint horizonMonitorSignalBlueprint, e *horizonMonitorFeedEvent) horizonMonitorSignalBlueprint {
	if e == nil {
		return blueprint
	}
	text := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary), strings.TrimSpace(e.Category)}, " "))
	if looksLikeEnergySupplyShock(text) {
		blueprint.Axis = horizonmodels.AxisPhysicalFlowSupply
		blueprint.Mechanism = "energy_supply_disruption"
		blueprint.Sphere = uniqueStrings(append(append([]string{}, blueprint.Sphere...), "/sphere/logistics", "/sphere/energy"))
		return blueprint
	}
	if strings.TrimSpace(blueprint.SignalType) == "risk:maritime_hazard" &&
		hasAnyKeyword(text,
			"gps jamming", "jammed navigation", "shipping disruption", "closed to shipping", "port closure", "canal blockage",
			"strait of hormuz", "suez canal", "red sea", "bab el mandeb", "bab al mandab",
		) {
		blueprint.Axis = horizonmodels.AxisLogisticsLatency
		blueprint.Mechanism = "route_disruption"
		blueprint.Sphere = uniqueStrings(append(append([]string{}, blueprint.Sphere...), "/sphere/logistics"))
	}
	return blueprint
}

func looksLikeEnergySupplyShock(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if !hasDirectDisruptionTrigger(text) && !hasAnyKeyword(text, "supply disruption", "supply shock", "export halt", "loading operations", "oil export", "terminal fire") {
		return false
	}
	return hasAnyKeyword(text,
		"oil", "crude", "lng", "natural gas", "petrochem", "refinery", "oil export", "energy",
		"kharg island", "fujairah", "hormuz", "strait of hormuz", "tanker", "loading operations", "terminal",
	)
}

func extractKnownMaritimeChokepointFromText(text string) string {
	text = normalizePhrase(text)
	if text == "" {
		return ""
	}
	for _, candidate := range []string{
		"bab el mandeb",
		"bab-el-mandeb",
		"bab al mandab",
		"strait of hormuz",
		"hormuz",
		"suez canal",
		"taiwan strait",
		"malacca strait",
		"panama canal",
		"strait of gibraltar",
		"english channel",
		"red sea",
		"southern red sea",
	} {
		if strings.Contains(text, normalizePhrase(candidate)) {
			switch normalizePhrase(candidate) {
			case "bab el mandeb", "bab-el-mandeb", "bab al mandab":
				return "Bab el-Mandeb"
			case "hormuz", "strait of hormuz":
				return "Strait of Hormuz"
			case "red sea", "southern red sea":
				return "Red Sea"
			default:
				return strings.TrimSpace(candidate)
			}
		}
	}
	return ""
}

func looksLikeCredibleChokepointThreat(text string, cls horizonMonitorNewsClassification) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if extractKnownMaritimeChokepointFromText(text) == "" {
		return false
	}
	if hasAnyKeyword(text,
		"why ", "how ", "at a glance", "by the numbers", "what happens if", "what comes next",
		"as it happened", "live blog", "live updates", "opinion", "analysis", "explainer",
	) && !hasAnyKeyword(text, "will remain closed", "block shipping", "closed to shipping", "threaten shipping", "maritime attacks") {
		return false
	}
	if looksLikeExerciseWithoutIncidentText(text) ||
		looksLikeAdministrativeCommentaryText(text) ||
		looksLikeForcePostureDeploymentText(text, cls) ||
		looksLikeChokepointSecurityMissionWithoutOperationalDisruption(text) ||
		looksLikeLiveBlogPoliticalCommentaryText(text) ||
		looksLikeRetrospectiveConflictNarrativeText(text) {
		return false
	}
	if hasAnyKeyword(text,
		"block shipping", "threaten shipping", "threat to shipping", "warned shipping", "warning to shipping",
		"closed to shipping", "will remain closed", "close the strait", "close the gulf", "closure threat",
		"blockade", "maritime attacks", "attack vessels", "attack ships", "target vessels", "target shipping",
		"anti ship", "anti-ship", "sea lane threat", "shipping lane threat", "escalates attacks on infrastructure and transport",
	) {
		return true
	}
	if hasAnyKeyword(text, "houthi", "houthis", "rebels", "militants", "iran", "irgc", "hezbollah") &&
		hasAnyKeyword(text, "shipping", "tanker", "vessel", "maritime", "strait", "sea lane", "chokepoint") &&
		hasAnyKeyword(text, "threat", "warn", "closure", "closed", "block", "attack") {
		return true
	}
	return false
}

func looksLikeConcreteOperationalIncident(text string, cls horizonMonitorNewsClassification) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if !hasDirectDisruptionTrigger(text) && !hasAnyKeyword(text,
		"rocket fire", "drone attack", "under fire", "projectile hit", "projectile fell",
		"operations halted", "operations stopped", "operations suspended", "loading operations",
		"gps jamming", "jammed navigation", "warship hit", "tanker hit", "terminal fire",
		"strike", "strikes", "was hit", "were hit", "fire at",
	) {
		return false
	}
	if hasAnyKeyword(text,
		"why did", "why has", "why have", "how long could", "what happens if", "what comes next",
		"what happened in", "at a glance", "by the numbers", "lessons from", "legacy of",
		"as it happened", "live blog", "live updates", "latest updates", "counterfactual",
	) {
		return false
	}
	if looksLikeExerciseWithoutIncidentText(text) ||
		looksLikeForcePostureDeploymentText(text, cls) ||
		looksLikeChokepointSecurityMissionWithoutOperationalDisruption(text) ||
		looksLikeAdministrativeCommentaryText(text) ||
		looksLikeLegalCommercialDisputeText(text) ||
		looksLikeSportsPoliticalHumanInterestText(text) ||
		looksLikeCivilIncidentWithoutMilitaryContext(text) ||
		looksLikeDrugInterdictionText(text) ||
		looksLikeLiveBlogPoliticalCommentaryText(text) ||
		looksLikeRetrospectiveConflictNarrativeText(text) ||
		looksLikeOffshoreShippingIncidentAttributedElsewhere(text) {
		return false
	}
	category := strings.ToLower(strings.TrimSpace(cls.Category))
	if category == "military" || category == "conflict" || category == "infrastructure" || category == "terrorism" {
		return true
	}
	return hasAnyKeyword(text,
		"port", "terminal", "shipping", "tanker", "strait", "canal", "refinery", "depot",
		"airport", "airbase", "embassy", "power plant", "substation", "pipeline", "grid",
		"loading operations", "navigation", "oil export", "oil facility", "island",
		"facility", "infrastructure", "energy infrastructure", "industrial city", "industrial complex",
		"plant", "complex", "lng", "natural gas", "gas", "helium", "processing complex",
	)
}

func looksLikeConflictDrivenOperationalDisruption(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text,
		"labor strike", "workers strike", "dock strike", "dockworkers strike", "dock workers strike",
		"walkout", "union action", "industrial action",
	) && !hasAnyKeyword(text,
		"missile strike", "drone strike", "airstrike", "rocket strike", "naval strike",
	) {
		return false
	}
	if !hasAnyKeyword(text,
		"drone attack", "drone strike", "missile strike", "missile attack", "rocket attack", "rocket fire",
		"projectile hit", "projectile fell", "airstrike", "under fire", "was hit", "were hit",
		"attack", "attacked", "embassy hit", "warship hit", "tanker hit", "drones strike",
	) {
		return false
	}
	return hasAnyKeyword(text,
		"port", "terminal", "shipping", "tanker", "strait", "canal", "loading operations",
		"gps jamming", "refinery", "depot", "pipeline", "airport", "airbase", "oil export",
		"oil facility", "embassy", "grid", "power plant", "substation", "island",
	)
}

func looksLikeLaborStrikeOperationalDisruption(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if !hasAnyKeyword(text,
		"labor strike", "workers strike", "dock strike", "dockworkers strike", "dock workers strike",
		"walkout", "union action", "industrial action",
	) {
		return false
	}
	return hasAnyKeyword(text,
		"port", "terminal", "shipping", "container", "cargo", "operations", "shutdown", "shuts down",
		"closure", "closed", "freight", "rail-linked", "maritime",
	)
}

func shouldForceConcreteIncidentHandling(e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification, assessment *discoverynlp.ArticleAssessment) bool {
	if e == nil {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(e.Title),
		strings.TrimSpace(e.Summary),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	if !looksLikeConcreteOperationalIncident(text, cls) {
		return false
	}
	if assessment == nil {
		return true
	}
	if assessment.ShouldCreateZone || assessment.ShouldWarmExisting || assessment.ShouldOnlySignal {
		return true
	}
	return assessment.ConcreteOutageScore >= 0.72 || !assessment.IsEvent
}

func shouldSuppressHeuristicSignalPropagation(e *horizonMonitorFeedEvent) bool {
	if e == nil {
		return false
	}
	if signalTypeFromFeedType(strings.TrimSpace(e.FeedType)) != "" {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{e.Title, e.Summary}, " "))
	if looksLikeConcreteOperationalIncident(text, horizonMonitorNewsClassification{}) {
		return false
	}
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
		"missile strike", "drone strike", "drone attack", "airstrike", "rocket attack", "rocket fire", "shelling",
		"closed airport", "airport closure", "shipping disruption", "port closure", "shutdown",
		"offline", "remains offline", "still offline",
		"operations halted", "operations stopped", "operations suspended", "loading operations",
		"gps jamming", "projectile hit", "projectile fell", "under fire", "damaged", "warship hit", "tanker hit",
		"strike", "strikes", "was hit", "were hit", "fire at",
	)
}

func looksLikeAffectedZoneRecoveryText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if !hasAnyKeyword(text,
		"operations resumed", "operations resume", "loading resumed", "loadings resumed", "exports resumed",
		"production resumed", "shipments resumed", "reopened", "reopen", "restored", "back online",
		"restart", "restarted", "resumes service", "service restored", "terminal reopens", "port reopens",
		"throughput restored", "supply resumed",
	) {
		return false
	}
	if hasAnyKeyword(text,
		"preparing to reopen", "plan to reopen", "mission to reopen", "could reopen", "may reopen",
		"hopes to reopen", "aims to reopen", "talks to reopen", "security mission", "defensive mission",
	) {
		return false
	}
	return hasAnyKeyword(text,
		"port", "terminal", "shipping", "tanker", "strait", "canal", "loading operations", "exports",
		"lng", "gas", "oil", "refinery", "pipeline", "plant", "grid", "airport", "airbase", "service",
	)
}

func looksLikeStickyOperationalOutage(text, conditionType string) bool {
	text = normalizeKeywordText(text)
	conditionType = strings.TrimSpace(conditionType)
	if text == "" || conditionType == "" {
		return false
	}
	if looksLikeAffectedZoneRecoveryText(text) {
		return false
	}
	if looksLikeCredibleChokepointThreat(text, horizonMonitorNewsClassification{}) {
		return true
	}
	if conditionType != "infrastructure_disruption" && conditionType != "military_conflict" {
		return false
	}
	if hasDirectDisruptionTrigger(text) || looksLikeConflictDrivenOperationalDisruption(text) {
		return hasAnyKeyword(text,
			"port", "terminal", "shipping", "tanker", "strait", "canal", "loading operations",
			"lng", "gas", "oil", "exports", "refinery", "pipeline", "plant", "grid", "airport", "airbase",
			"operations halted", "operations stopped", "operations suspended", "closure", "closed to shipping",
		)
	}
	return false
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

func (p *HorizonMonitorPipeline) maybeEnqueueWarmup(ctx context.Context, tenantID string, result *horizonsignallanding.Result) {
	if p == nil || p.impactWarmupHook == nil || result == nil {
		return
	}
	refs := make([]string, 0, len(result.Hits)+len(result.Summaries)*2)
	for _, hit := range result.Hits {
		if hit == nil {
			continue
		}
		refs = append(refs, strings.TrimSpace(hit.CanonicalRef))
	}
	for _, summary := range result.Summaries {
		if summary == nil {
			continue
		}
		refs = append(refs, strings.TrimSpace(summary.ScopeRef))
		refs = append(refs, summary.AffectedZoneRefs...)
	}
	refs = uniqueStrings(refs)
	if len(refs) == 0 {
		p.logf("horizon monitor pipeline: impact warmup skipped tenant=%s reason=no_refs", tenantID)
		return
	}
	if strings.TrimSpace(tenantID) == "" {
		tenantID = "global"
	}
	p.logf("horizon monitor pipeline: impact warmup enqueue tenant=%s refs=%d", tenantID, len(refs))
	if err := p.impactWarmupHook(ctx, strings.TrimSpace(tenantID), refs, "watch_signal_landing_delta"); err != nil {
		p.logf("horizon monitor pipeline: impact warmup enqueue failed tenant=%s refs=%d err=%v", tenantID, len(refs), err)
		return
	}
	p.logf("horizon monitor pipeline: impact warmup enqueued tenant=%s refs=%d", tenantID, len(refs))
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
) (*horizonmodels.HorizonFragment, error) {
	if p == nil || p.fragmentsRepo == nil || e == nil {
		return nil, nil
	}
	if candidate == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	matchedAt := e.OccurredAt.UTC()
	if matchedAt.IsZero() {
		matchedAt = now
	}

	existing, err := p.fragmentsRepo.GetByCanonicalPath(ctx, candidate.CanonicalPath)
	if err != nil && !errors.Is(err, horizonrepo.ErrFragmentNotFound) {
		return nil, err
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
	if existing != nil && existing.CanonicalPath != candidate.CanonicalPath && shouldPreferSpecificAffectedZoneCreation(candidate, existing) {
		existing = nil
	}
	if shouldSkipAffectedZoneAsStale(now, matchedAt, e, candidate) {
		if existing == nil {
			return nil, nil
		}
		st := parseAffectedZoneState(existing)
		prevScore := st.Score
		prevHits1h := st.Hits1h
		prevHits24h := st.Hits24h
		prevMentions := st.TotalMentions
		st.applyDecay(now)
		if st.Score == prevScore && st.Hits1h == prevHits1h && st.Hits24h == prevHits24h && st.TotalMentions == prevMentions {
			return existing, nil
		}
		applyAffectedZoneState(existing, st, now, st.LastMatchedAt, sourceID, e)
		if existing.Qualifiers == nil {
			existing.Qualifiers = map[string]interface{}{}
		}
		existing.Qualifiers["staleObservationIgnored"] = true
		existing.Qualifiers["staleObservationAgeHours"] = math.Round(now.Sub(matchedAt).Hours())
		return existing, p.fragmentsRepo.Update(ctx, existing)
	}
	boost := affectedZoneBoost(e, cls, candidate)
	if existing == nil {
		if candidate.WarmOnly && !looksLikeConcreteOperationalIncident(normalizeKeywordText(strings.Join([]string{
			strings.TrimSpace(e.Title),
			strings.TrimSpace(e.Summary),
			strings.TrimSpace(cls.Category),
			strings.TrimSpace(cls.Level),
		}, " ")), cls) {
			return nil, nil
		}
		incoming := buildIncomingAffectedZoneFragment(candidate, sourceID, e, cls, now, matchedAt, boost)
		createdID, createErr := p.fragmentsRepo.Create(ctx, incoming)
		if createdID != "" {
			incoming.ID = createdID
		}
		if createErr == nil {
			if linkErr := p.upsertAffectedZoneGraphLinks(ctx, incoming, candidate, sourceID, e); linkErr != nil {
				return nil, linkErr
			}
			p.publishAffectedZoneUpsert(ctx, tenantID, incoming, sourceID, "created")
		}
		return incoming, createErr
	}
	if !mergeAffectedZoneFragment(existing, candidate, sourceID, e, cls, now, matchedAt, boost) {
		return existing, p.upsertAffectedZoneGraphLinks(ctx, existing, candidate, sourceID, e)
	}
	if err := p.fragmentsRepo.Update(ctx, existing); err != nil {
		return nil, err
	}
	if err := p.upsertAffectedZoneGraphLinks(ctx, existing, candidate, sourceID, e); err != nil {
		return nil, err
	}
	p.publishAffectedZoneUpsert(ctx, tenantID, existing, sourceID, "updated")
	return existing, nil
}

func (p *HorizonMonitorPipeline) upsertAffectedZoneGraphLinks(
	ctx context.Context,
	zone *horizonmodels.HorizonFragment,
	candidate *affectedZoneCandidate,
	sourceID string,
	e *horizonMonitorFeedEvent,
) error {
	if p == nil || p.edgesRepo == nil || p.fragmentsRepo == nil || zone == nil || candidate == nil {
		return nil
	}
	zoneID := strings.TrimSpace(zone.ID)
	if zoneID == "" || zone.Type != horizonmodels.FragmentTypeAffectedZone {
		return nil
	}
	affectsTargets, locatedInTarget := p.resolveAffectedZoneGraphTargets(ctx, candidate)
	if coordTarget := bestAffectedZoneCoordinateTarget(locatedInTarget, affectsTargets); p.refreshAffectedZoneCoordinatesFromLinkedTarget(zone, candidate, coordTarget) {
		if err := p.fragmentsRepo.Update(ctx, zone); err != nil {
			return err
		}
	}
	for _, target := range affectsTargets {
		if err := p.upsertAffectedZoneEdge(ctx, zone, target, horizonmodels.DependencyTypeAffects, sourceID, e, candidate); err != nil {
			return err
		}
	}
	if locatedInTarget != nil {
		if err := p.upsertAffectedZoneEdge(ctx, zone, locatedInTarget, horizonmodels.DependencyTypeLocatedIn, sourceID, e, candidate); err != nil {
			return err
		}
	}
	return p.reconcileAffectedZoneGraphEdges(ctx, zone, affectsTargets, locatedInTarget)
}

func (p *HorizonMonitorPipeline) reconcileAffectedZoneGraphEdges(
	ctx context.Context,
	zone *horizonmodels.HorizonFragment,
	affectsTargets []*horizonmodels.HorizonFragment,
	locatedInTarget *horizonmodels.HorizonFragment,
) error {
	if p == nil || p.edgesRepo == nil || zone == nil || strings.TrimSpace(zone.ID) == "" {
		return nil
	}
	wantAffects := make(map[string]struct{}, len(affectsTargets))
	for _, target := range affectsTargets {
		if target == nil || strings.TrimSpace(target.ID) == "" {
			continue
		}
		wantAffects[strings.TrimSpace(target.ID)] = struct{}{}
	}
	wantLocatedIn := make(map[string]struct{}, 1)
	if locatedInTarget != nil && strings.TrimSpace(locatedInTarget.ID) != "" {
		wantLocatedIn[strings.TrimSpace(locatedInTarget.ID)] = struct{}{}
	}
	edges, err := p.edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(zone.ID), nil, 128)
	if err != nil {
		return err
	}
	for _, edge := range edges {
		if edge == nil || strings.TrimSpace(edge.ID) == "" {
			continue
		}
		switch edge.Type {
		case horizonmodels.DependencyTypeAffects:
			if !isManagedAffectedZoneGraphEdge(edge, zone) {
				continue
			}
			if _, ok := wantAffects[strings.TrimSpace(edge.TargetFragmentID)]; ok {
				continue
			}
			if err := p.edgesRepo.Delete(ctx, strings.TrimSpace(edge.ID)); err != nil {
				return err
			}
		case horizonmodels.DependencyTypeLocatedIn:
			if !isManagedAffectedZoneGraphEdge(edge, zone) {
				continue
			}
			if _, ok := wantLocatedIn[strings.TrimSpace(edge.TargetFragmentID)]; ok {
				continue
			}
			if err := p.edgesRepo.Delete(ctx, strings.TrimSpace(edge.ID)); err != nil {
				return err
			}
		}
	}
	return nil
}

func isManagedAffectedZoneGraphEdge(edge *horizonmodels.DependencyEdge, zone *horizonmodels.HorizonFragment) bool {
	if edge == nil || zone == nil {
		return false
	}
	if strings.TrimSpace(edge.SourceFragmentID) != strings.TrimSpace(zone.ID) {
		return false
	}
	if strings.TrimSpace(edge.Provenance.Method) == "horizon_monitor_affected_zone" {
		return true
	}
	return strings.TrimSpace(asString(edge.Qualifiers["source"])) == "horizon_monitor_affected_zone"
}

func bestAffectedZoneCoordinateTarget(locatedInTarget *horizonmodels.HorizonFragment, affectsTargets []*horizonmodels.HorizonFragment) *horizonmodels.HorizonFragment {
	if locatedInTarget != nil {
		if _, _, hasCoord := horizonrepo.ResolveFragmentCoordinates(locatedInTarget); hasCoord {
			return locatedInTarget
		}
	}
	for _, target := range affectsTargets {
		if target == nil {
			continue
		}
		if _, _, hasCoord := horizonrepo.ResolveFragmentCoordinates(target); hasCoord {
			return target
		}
	}
	return locatedInTarget
}

func (p *HorizonMonitorPipeline) refreshAffectedZoneCoordinatesFromLinkedTarget(
	zone *horizonmodels.HorizonFragment,
	candidate *affectedZoneCandidate,
	target *horizonmodels.HorizonFragment,
) bool {
	if zone == nil || candidate == nil || target == nil {
		return false
	}
	lat, lon, hasCoord := horizonrepo.ResolveFragmentCoordinates(target)
	if !hasCoord || !horizonmodels.IsRealGeoCoordinate(lat, lon) {
		return p.refreshAffectedZoneHomeLocation(zone, candidate)
	}
	currentLat, currentLon, hasCurrent := horizonrepo.ResolveFragmentCoordinates(zone)
	replace := !hasCurrent
	if !replace {
		distanceKm := greatCircleDistanceKm(currentLat, currentLon, lat, lon)
		if distanceKm > 250 &&
			(strings.TrimSpace(candidate.GeoPrecision) == "precise" || strings.EqualFold(strings.TrimSpace(candidate.GeoToken), "sea")) {
			replace = true
		}
	}
	if !replace && strings.TrimSpace(qualifierStringValue(zone.Qualifiers, "geoPrecision")) == "approximate_country" {
		replace = true
	}
	if !replace {
		return p.refreshAffectedZoneHomeLocation(zone, candidate)
	}
	zone.Coordinates = &horizonmodels.Coordinates{Lat: lat, Lon: lon}
	if zone.Qualifiers == nil {
		zone.Qualifiers = map[string]interface{}{}
	}
	zone.Qualifiers["geoLinkedTargetPath"] = strings.TrimSpace(target.CanonicalPath)
	zone.Qualifiers["geoResolutionMethod"] = "linked_target"
	zone.Qualifiers["geoPrecision"] = firstNonEmptyString(strings.TrimSpace(candidate.GeoPrecision), "precise")
	changed := p.refreshAffectedZoneHomeLocation(zone, candidate)
	if zone.Metadata.LastUpdatedAt.Before(time.Now().UTC()) {
		zone.Metadata.LastUpdatedAt = time.Now().UTC()
	}
	return true || changed
}

func (p *HorizonMonitorPipeline) refreshAffectedZoneHomeLocation(zone *horizonmodels.HorizonFragment, candidate *affectedZoneCandidate) bool {
	if zone == nil || candidate == nil {
		return false
	}
	if zone.HomeLocation == nil {
		zone.HomeLocation = &horizonmodels.Location{}
	}
	changed := false
	if strings.TrimSpace(zone.HomeLocation.Name) != strings.TrimSpace(candidate.Label) && strings.TrimSpace(candidate.Label) != "" {
		zone.HomeLocation.Name = strings.TrimSpace(candidate.Label)
		changed = true
	}
	if cc := strings.ToUpper(strings.TrimSpace(candidate.CountryCode)); len(cc) == 2 {
		if zone.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*zone.HomeLocation.ISO3166_1) != cc {
			zone.HomeLocation.ISO3166_1 = &cc
			changed = true
		}
	}
	if zone.Coordinates != nil && horizonmodels.IsRealGeoCoordinate(zone.Coordinates.Lat, zone.Coordinates.Lon) {
		iso := formatISO6709(zone.Coordinates.Lat, zone.Coordinates.Lon)
		if zone.HomeLocation.ISO6709 == nil || strings.TrimSpace(*zone.HomeLocation.ISO6709) != iso {
			zone.HomeLocation.ISO6709 = &iso
			changed = true
		}
	}
	return changed
}

func (p *HorizonMonitorPipeline) resolveAffectedZoneGraphTargets(
	ctx context.Context,
	candidate *affectedZoneCandidate,
) ([]*horizonmodels.HorizonFragment, *horizonmodels.HorizonFragment) {
	if p == nil || p.fragmentsRepo == nil || candidate == nil {
		return nil, nil
	}
	targets := make([]*horizonmodels.HorizonFragment, 0, 6)
	seen := make(map[string]struct{}, 8)
	var locatedIn *horizonmodels.HorizonFragment
	addTarget := func(f *horizonmodels.HorizonFragment) {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			return
		}
		if !isAffectedZoneBindableFragmentType(f.Type) {
			return
		}
		key := strings.TrimSpace(f.ID)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, f)
	}
	setLocatedIn := func(f *horizonmodels.HorizonFragment) {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			return
		}
		if !isGeoRegionLikeFragment(f) {
			return
		}
		if locatedIn == nil || shouldPreferAffectedZoneGeoTarget(f, locatedIn) {
			locatedIn = f
		}
	}
	for _, path := range uniqueStrings(candidate.LinkedFragmentPaths) {
		frag, err := p.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(path))
		if err != nil || frag == nil {
			continue
		}
		addTarget(frag)
		if isGeoRegionLikeFragment(frag) {
			setLocatedIn(frag)
			continue
		}
		if geo := p.primaryGeoRelationTarget(ctx, frag); geo != nil {
			setLocatedIn(geo)
		}
		for _, related := range p.relatedAffectedZoneFragments(ctx, frag) {
			addTarget(related)
		}
	}
	for _, frag := range p.directAffectedZoneLocationTargets(ctx, candidate) {
		addTarget(frag)
		if isGeoRegionLikeFragment(frag) {
			setLocatedIn(frag)
		} else if geo := p.primaryGeoRelationTarget(ctx, frag); geo != nil {
			setLocatedIn(geo)
		}
		for _, related := range p.relatedAffectedZoneFragments(ctx, frag) {
			addTarget(related)
		}
	}
	if len(targets) == 0 {
		for _, frag := range p.fallbackAffectedZoneLinkTargets(ctx, candidate) {
			addTarget(frag)
			if locatedIn == nil {
				if isGeoRegionLikeFragment(frag) {
					setLocatedIn(frag)
				} else if geo := p.primaryGeoRelationTarget(ctx, frag); geo != nil {
					setLocatedIn(geo)
				}
			}
			for _, related := range p.relatedAffectedZoneFragments(ctx, frag) {
				addTarget(related)
			}
		}
	}
	targets = filterRedundantAffectedZoneTargets(targets, locatedIn)
	return targets, locatedIn
}

func filterRedundantAffectedZoneTargets(targets []*horizonmodels.HorizonFragment, locatedIn *horizonmodels.HorizonFragment) []*horizonmodels.HorizonFragment {
	if len(targets) <= 1 {
		return targets
	}
	out := make([]*horizonmodels.HorizonFragment, 0, len(targets))
	for i, target := range targets {
		if target == nil || strings.TrimSpace(target.ID) == "" {
			continue
		}
		if isGeoRegionLikeFragment(target) {
			targetPath := strings.TrimSpace(target.CanonicalPath)
			if locatedIn != nil && isMoreSpecificAffectedZoneGeoPath(strings.TrimSpace(locatedIn.CanonicalPath), targetPath) {
				continue
			}
			redundant := false
			for j, other := range targets {
				if i == j || other == nil || strings.TrimSpace(other.ID) == "" || !isGeoRegionLikeFragment(other) {
					continue
				}
				if isMoreSpecificAffectedZoneGeoPath(strings.TrimSpace(other.CanonicalPath), targetPath) {
					redundant = true
					break
				}
			}
			if redundant {
				continue
			}
		}
		out = append(out, target)
	}
	return out
}

func shouldPreferAffectedZoneGeoTarget(candidate, current *horizonmodels.HorizonFragment) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	candidatePath := strings.TrimSpace(candidate.CanonicalPath)
	currentPath := strings.TrimSpace(current.CanonicalPath)
	if isMoreSpecificAffectedZoneGeoPath(candidatePath, currentPath) {
		return true
	}
	if isMoreSpecificAffectedZoneGeoPath(currentPath, candidatePath) {
		return false
	}
	_, _, candidateHasCoord := horizonrepo.ResolveFragmentCoordinates(candidate)
	_, _, currentHasCoord := horizonrepo.ResolveFragmentCoordinates(current)
	return candidateHasCoord && !currentHasCoord
}

func isMoreSpecificAffectedZoneGeoPath(specific, broad string) bool {
	specific = strings.TrimSpace(specific)
	broad = strings.TrimSpace(broad)
	if specific == "" || broad == "" || specific == broad {
		return false
	}
	if !isGeoRegionLikeCanonicalPath(specific) || !isGeoRegionLikeCanonicalPath(broad) {
		return false
	}
	return strings.HasPrefix(specific, broad+"/")
}

func isGeoRegionLikeCanonicalPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/loc/") || strings.HasPrefix(path, "/geo/") || strings.HasPrefix(path, "/region/")
}

func (p *HorizonMonitorPipeline) fallbackAffectedZoneLinkTargets(
	ctx context.Context,
	candidate *affectedZoneCandidate,
) []*horizonmodels.HorizonFragment {
	if p == nil || p.fragmentsRepo == nil || candidate == nil {
		return nil
	}
	out := make([]*horizonmodels.HorizonFragment, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(f *horizonmodels.HorizonFragment) {
		if f == nil || strings.TrimSpace(f.ID) == "" || !isAffectedZoneBindableFragmentType(f.Type) {
			return
		}
		key := strings.TrimSpace(f.ID)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	for _, direct := range p.directAffectedZoneLocationTargets(ctx, candidate) {
		add(direct)
	}
	if p.vertexSearch == nil || !p.vertexSearch.IsConfigured() {
		return out
	}
	query := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(candidate.Label),
		strings.TrimSpace(candidate.CountryName),
		strings.TrimSpace(candidate.ConditionType),
	}, " "))
	if query == "" {
		return out
	}
	resp, err := p.vertexSearch.Search(ctx, &ai.SearchRequest{Query: query, PageSize: 8})
	if err != nil || resp == nil {
		return out
	}
	type scored struct {
		frag  *horizonmodels.HorizonFragment
		score float64
	}
	scoredResults := make([]scored, 0, len(resp.Results))
	for _, result := range resp.Results {
		score := result.RelevanceScore
		if score == 0 && result.RankSignals != nil {
			score = result.RankSignals.SemanticSimilarityScore
		}
		if score < 0.60 {
			continue
		}
		fragID := extractFragmentIDFromGCSURI(result.Document.URI)
		if fragID == "" {
			fragID = strings.TrimSuffix(strings.TrimSpace(result.Document.ID), ".json")
		}
		if fragID == "" {
			continue
		}
		frag, ferr := p.fragmentsRepo.Get(ctx, fragID)
		if ferr != nil || frag == nil || !isAffectedZoneBindableFragmentType(frag.Type) {
			continue
		}
		scoredResults = append(scoredResults, scored{frag: frag, score: affectedZoneFallbackTargetScore(candidate, frag, score)})
	}
	sort.Slice(scoredResults, func(i, j int) bool {
		if scoredResults[i].score == scoredResults[j].score {
			return strings.TrimSpace(scoredResults[i].frag.CanonicalPath) < strings.TrimSpace(scoredResults[j].frag.CanonicalPath)
		}
		return scoredResults[i].score > scoredResults[j].score
	})
	for _, item := range scoredResults {
		if item.score < 0.82 {
			continue
		}
		add(item.frag)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func (p *HorizonMonitorPipeline) directAffectedZoneLocationTargets(
	ctx context.Context,
	candidate *affectedZoneCandidate,
) []*horizonmodels.HorizonFragment {
	if p == nil || p.fragmentsRepo == nil || candidate == nil {
		return nil
	}
	_, _, slugPath, ok := parseZoneCanonicalPath(strings.TrimSpace(candidate.CanonicalPath))
	if !ok || slugPath == "" {
		return nil
	}
	directCandidates := []string{}
	if strings.EqualFold(strings.TrimSpace(candidate.GeoToken), "sea") {
		directCandidates = append(directCandidates,
			"/loc/int/na/"+strings.TrimSpace(slugPath),
			"/loc/int/"+strings.TrimSpace(slugPath),
			"/fac/na/"+strings.TrimSpace(slugPath),
		)
	} else if cc := strings.ToLower(strings.TrimSpace(candidate.CountryCode)); len(cc) == 2 {
		directCandidates = append(directCandidates,
			"/loc/"+cc+"/na/"+strings.TrimSpace(slugPath),
			"/geo/"+strings.TrimSpace(slugPath),
		)
	}
	out := make([]*horizonmodels.HorizonFragment, 0, len(directCandidates))
	seen := make(map[string]struct{}, len(directCandidates))
	for _, path := range uniqueStrings(directCandidates) {
		frag, err := p.fragmentsRepo.GetByCanonicalPath(ctx, path)
		if err == nil && frag != nil && isAffectedZoneBindableFragmentType(frag.Type) {
			if _, ok := seen[strings.TrimSpace(frag.ID)]; ok {
				continue
			}
			seen[strings.TrimSpace(frag.ID)] = struct{}{}
			out = append(out, frag)
		}
	}
	return out
}

func affectedZoneFallbackTargetScore(candidate *affectedZoneCandidate, frag *horizonmodels.HorizonFragment, base float64) float64 {
	if candidate == nil || frag == nil {
		return 0
	}
	score := base
	switch frag.Type {
	case horizonmodels.FragmentTypeFacility:
		score += 0.22
	case horizonmodels.FragmentTypeDeposit:
		score += 0.18
	case horizonmodels.FragmentTypeGeoRegion:
		score += 0.20
	case horizonmodels.FragmentTypeOrganization:
		score += 0.10
	case horizonmodels.FragmentTypeProduct:
		score += 0.14
	case horizonmodels.FragmentTypeProgram:
		score += 0.06
	}
	wantCountry := strings.ToUpper(strings.TrimSpace(candidate.CountryCode))
	if cc := affectedZoneCountryFromFragment(frag); wantCountry != "" && cc != "" && strings.EqualFold(cc, wantCountry) {
		score += 0.12
	}
	if strings.Contains(strings.TrimSpace(frag.CanonicalPath), "/na/") {
		score -= 0.08
	} else if frag.Type == horizonmodels.FragmentTypeFacility || frag.Type == horizonmodels.FragmentTypeOrganization {
		score += 0.06
	}
	label := normalizePhrase(strings.TrimSpace(frag.Labels["en"]))
	wantLabel := normalizePhrase(strings.TrimSpace(candidate.Label))
	if wantLabel != "" && label != "" {
		switch {
		case label == wantLabel:
			score += 0.12
		case strings.Contains(label, wantLabel), strings.Contains(wantLabel, label):
			score += 0.08
		}
	}
	if strings.EqualFold(strings.TrimSpace(candidate.GeoToken), "sea") && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/loc/int/") {
		score += 0.16
	}
	return score
}

func greatCircleDistanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	toRadians := func(v float64) float64 { return v * math.Pi / 180.0 }
	dLat := toRadians(lat2 - lat1)
	dLon := toRadians(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRadians(lat1))*math.Cos(toRadians(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(a))
}

func (p *HorizonMonitorPipeline) primaryGeoRelationTarget(
	ctx context.Context,
	frag *horizonmodels.HorizonFragment,
) *horizonmodels.HorizonFragment {
	if p == nil || p.edgesRepo == nil || p.fragmentsRepo == nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
		return nil
	}
	edgeTypes := []horizonmodels.DependencyType{
		horizonmodels.DependencyTypeLocatedIn,
		horizonmodels.DependencyTypeOperatesIn,
	}
	for _, edgeType := range edgeTypes {
		edges, err := p.edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(frag.ID), &edgeType, 16)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge == nil || strings.TrimSpace(edge.TargetFragmentID) == "" {
				continue
			}
			target, ferr := p.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID))
			if ferr == nil && isGeoRegionLikeFragment(target) {
				return target
			}
		}
	}
	return nil
}

func (p *HorizonMonitorPipeline) relatedAffectedZoneFragments(
	ctx context.Context,
	frag *horizonmodels.HorizonFragment,
) []*horizonmodels.HorizonFragment {
	if p == nil || p.edgesRepo == nil || p.fragmentsRepo == nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
		return nil
	}
	out := make([]*horizonmodels.HorizonFragment, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		target, err := p.fragmentsRepo.Get(ctx, id)
		if err != nil || target == nil || strings.TrimSpace(target.ID) == "" {
			return
		}
		if target.Type != horizonmodels.FragmentTypeFacility &&
			target.Type != horizonmodels.FragmentTypeOrganization &&
			target.Type != horizonmodels.FragmentTypeDeposit &&
			target.Type != horizonmodels.FragmentTypeProduct &&
			!isGeoRegionLikeFragment(target) {
			return
		}
		out = append(out, target)
	}
	switch frag.Type {
	case horizonmodels.FragmentTypeFacility, horizonmodels.FragmentTypeDeposit:
		for _, edgeType := range []horizonmodels.DependencyType{
			horizonmodels.DependencyTypeOwns,
			horizonmodels.DependencyTypeManages,
			horizonmodels.DependencyTypeControls,
		} {
			edges, err := p.edgesRepo.ListByTarget(ctx, "global", strings.TrimSpace(frag.ID), &edgeType, 16)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				if edge != nil {
					add(edge.SourceFragmentID)
				}
			}
		}
		for _, edgeType := range []horizonmodels.DependencyType{horizonmodels.DependencyTypeProduces} {
			edges, err := p.edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(frag.ID), &edgeType, 24)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				if edge != nil {
					add(edge.TargetFragmentID)
				}
			}
		}
	case horizonmodels.FragmentTypeOrganization:
		for _, edgeType := range []horizonmodels.DependencyType{
			horizonmodels.DependencyTypeOwns,
			horizonmodels.DependencyTypeManages,
			horizonmodels.DependencyTypeControls,
			horizonmodels.DependencyTypeProduces,
		} {
			edges, err := p.edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(frag.ID), &edgeType, 16)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				if edge != nil {
					add(edge.TargetFragmentID)
				}
			}
		}
	case horizonmodels.FragmentTypeProduct:
		for _, edgeType := range []horizonmodels.DependencyType{
			horizonmodels.DependencyTypeProduces,
			horizonmodels.DependencyTypeSuppliesTo,
		} {
			edges, err := p.edgesRepo.ListByTarget(ctx, "global", strings.TrimSpace(frag.ID), &edgeType, 24)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				if edge != nil {
					add(edge.SourceFragmentID)
				}
			}
		}
	case horizonmodels.FragmentTypeGeoRegion:
		for _, edgeType := range []horizonmodels.DependencyType{
			horizonmodels.DependencyTypeLocatedIn,
			horizonmodels.DependencyTypeOperatesIn,
		} {
			edges, err := p.edgesRepo.ListByTarget(ctx, "global", strings.TrimSpace(frag.ID), &edgeType, 24)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				if edge != nil {
					add(edge.SourceFragmentID)
				}
			}
		}
	}
	return out
}

func isGeoRegionLikeFragment(f *horizonmodels.HorizonFragment) bool {
	if f == nil {
		return false
	}
	if f.Type == horizonmodels.FragmentTypeGeoRegion {
		return true
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	return strings.HasPrefix(cp, "/loc/") || strings.HasPrefix(cp, "/geo/") || strings.HasPrefix(cp, "/region/")
}

func isAffectedZoneBindableFragmentType(fragmentType horizonmodels.FragmentType) bool {
	switch fragmentType {
	case horizonmodels.FragmentTypeFacility,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeDeposit,
		horizonmodels.FragmentTypeGeoRegion,
		horizonmodels.FragmentTypeProduct,
		horizonmodels.FragmentTypeProgram:
		return true
	default:
		return false
	}
}

func (p *HorizonMonitorPipeline) upsertAffectedZoneEdge(
	ctx context.Context,
	zone *horizonmodels.HorizonFragment,
	target *horizonmodels.HorizonFragment,
	edgeType horizonmodels.DependencyType,
	sourceID string,
	e *horizonMonitorFeedEvent,
	candidate *affectedZoneCandidate,
) error {
	if p == nil || p.edgesRepo == nil || zone == nil || target == nil || candidate == nil {
		return nil
	}
	sourceFragmentID := strings.TrimSpace(zone.ID)
	targetFragmentID := strings.TrimSpace(target.ID)
	if sourceFragmentID == "" || targetFragmentID == "" || sourceFragmentID == targetFragmentID {
		return nil
	}
	now := time.Now().UTC()
	qualifiers := map[string]interface{}{
		"source":            "horizon_monitor_affected_zone",
		"zoneCanonicalPath": strings.TrimSpace(zone.CanonicalPath),
		"conditionType":     strings.TrimSpace(candidate.ConditionType),
		"geoPrecision":      strings.TrimSpace(candidate.GeoPrecision),
	}
	if e != nil {
		if title := strings.TrimSpace(e.Title); title != "" {
			qualifiers["sourceTitle"] = title
		}
		if url := strings.TrimSpace(e.URL); url != "" {
			qualifiers["sourceUrl"] = url
		}
	}
	properties := horizonmodels.EdgeProperties{
		Criticality:   wmClamp01(math.Max(candidate.EventConfidence, candidate.GeoConfidence)),
		Amplification: 1.0,
	}
	if edgeType == horizonmodels.DependencyTypeAffects && strings.TrimSpace(candidate.ConditionType) == "military_conflict" {
		properties.Criticality = wmClamp01(math.Max(properties.Criticality, 0.82))
	}
	if edgeType == horizonmodels.DependencyTypeLocatedIn {
		properties.Criticality = wmClamp01(math.Max(properties.Criticality, 0.58))
	}
	existing, err := p.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceFragmentID, targetFragmentID, edgeType)
	if err == nil && existing != nil {
		changed := false
		if existing.Qualifiers == nil {
			existing.Qualifiers = map[string]interface{}{}
		}
		for key, value := range qualifiers {
			if fmt.Sprintf("%v", existing.Qualifiers[key]) != fmt.Sprintf("%v", value) {
				existing.Qualifiers[key] = value
				changed = true
			}
		}
		if existing.Properties.Criticality < properties.Criticality {
			existing.Properties.Criticality = properties.Criticality
			changed = true
		}
		if existing.Properties.Amplification == 0 {
			existing.Properties.Amplification = 1.0
			changed = true
		}
		if existing.Metadata.LastUpdatedAt.Before(now) {
			existing.Metadata.LastUpdatedAt = now
			changed = true
		}
		if existing.Provenance.Method == "" {
			existing.Provenance.Method = "horizon_monitor_affected_zone"
			changed = true
		}
		mergedSourceIDs := uniqueStrings(append(existing.Provenance.SourceIDs, strings.TrimSpace(sourceID)))
		if strings.Join(existing.Provenance.SourceIDs, "|") != strings.Join(mergedSourceIDs, "|") {
			existing.Provenance.SourceIDs = mergedSourceIDs
			changed = true
		}
		if changed {
			return p.edgesRepo.Update(ctx, existing)
		}
		return nil
	}
	edge := &horizonmodels.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: sourceFragmentID,
		TargetFragmentID: targetFragmentID,
		Type:             edgeType,
		Properties:       properties,
		Qualifiers:       qualifiers,
		Provenance: horizonmodels.Provenance{
			Method:    "horizon_monitor_affected_zone",
			SourceIDs: uniqueStrings([]string{strings.TrimSpace(sourceID)}),
		},
		Metadata: horizonmodels.EdgeMetadata{
			CreatedAt:     now,
			LastUpdatedAt: now,
		},
	}
	_, createErr := p.edgesRepo.Create(ctx, edge)
	return createErr
}

func (p *HorizonMonitorPipeline) applyAffectedZoneRecoveryObservation(
	ctx context.Context,
	sourceID string,
	e *horizonMonitorFeedEvent,
) error {
	if p == nil || p.fragmentsRepo == nil || e == nil {
		return nil
	}
	text := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
	if !looksLikeAffectedZoneRecoveryText(text) {
		return nil
	}
	place := inferZonePlaceName(e, text, "infrastructure_disruption")
	geoToken, _, countryName := inferZoneGeo(e, place, text, "infrastructure_disruption")
	place = normalizeAffectedZonePlace(place, countryName)
	slug := canonicalPathSlug(place)
	if slug == "" || slug == "na" {
		return nil
	}
	candidates := []string{
		"/zone/" + strings.TrimSpace(geoToken) + "/infrastructure_disruption/" + slug,
		"/zone/" + strings.TrimSpace(geoToken) + "/military_conflict/" + slug,
	}
	now := time.Now().UTC()
	for _, path := range uniqueStrings(candidates) {
		frag, err := p.fragmentsRepo.GetByCanonicalPath(ctx, path)
		if err != nil || frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
			continue
		}
		if !applyStickyRecoveryObservation(frag, e, now) {
			continue
		}
		if frag.Metadata.LastUpdatedAt.Before(now) {
			frag.Metadata.LastUpdatedAt = now
		}
		if frag.Metadata.LastVerifiedAt.Before(now) {
			frag.Metadata.LastVerifiedAt = now
		}
		if err := p.fragmentsRepo.Update(ctx, frag); err != nil {
			return err
		}
	}
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
	wantChokepoint := affectedZoneMaritimeIdentityKey(candidate.Label, candidate.CanonicalPath)
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
		if wantChokepoint != "" && !affectedZoneSemanticMatchAllowsDifferentMaritimeIdentity(wantChokepoint, slug, cp) {
			continue
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
	wantChokepoint := affectedZoneMaritimeIdentityKey(candidate.Label, candidate.CanonicalPath)
	bestScore := 0.0
	var best *horizonmodels.HorizonFragment
	for _, frag := range fragments {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(frag.Status), string(horizonmodels.FragmentStatusArchived)) {
			continue
		}
		geo, condition, slugPath, ok := parseZoneCanonicalPath(strings.TrimSpace(frag.CanonicalPath))
		if !ok || condition != strings.ToLower(strings.TrimSpace(candidate.ConditionType)) {
			continue
		}
		if wantChokepoint != "" && !affectedZoneSemanticMatchAllowsDifferentMaritimeIdentity(wantChokepoint, slugPath, frag.CanonicalPath) {
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

func shouldPreferSpecificAffectedZoneCreation(candidate *affectedZoneCandidate, existing *horizonmodels.HorizonFragment) bool {
	if candidate == nil || existing == nil {
		return false
	}
	if strings.TrimSpace(existing.CanonicalPath) == strings.TrimSpace(candidate.CanonicalPath) {
		return false
	}
	_, _, existingSlug, ok := parseZoneCanonicalPath(existing.CanonicalPath)
	if !ok {
		return false
	}
	existingPrecision := strings.TrimSpace(qualifierStringValue(existing.Qualifiers, "geoPrecision"))
	candidatePrecise := candidate.GeoPrecision == "precise" || candidate.GeoPrecision == "entity_anchor"
	existingCountrySlug := normalizePhrase(existingSlug) == normalizePhrase(candidate.CountryName) ||
		normalizePhrase(existingSlug) == normalizePhrase(countryNameFromCode(candidate.CountryCode))
	existingGenericOperationalSlug := isGenericOperationalZoneSlug(existingSlug)
	if candidatePrecise && (existingPrecision == "approximate_country" || existingCountrySlug || existingGenericOperationalSlug) {
		return true
	}
	st := parseAffectedZoneState(existing)
	if !st.LastMatchedAt.IsZero() {
		lastMatchedAt := st.LastMatchedAt.UTC()
		if time.Since(lastMatchedAt) > 36*time.Hour &&
			candidate.ResolutionConfidence >= 0.74 &&
			candidate.GeoConfidence >= 0.66 &&
			(existingPrecision == "approximate_country" || existingCountrySlug || existingGenericOperationalSlug) {
			return true
		}
	}
	return false
}

func isGenericOperationalZoneSlug(slug string) bool {
	switch normalizePhrase(strings.ReplaceAll(strings.TrimSpace(slug), "-", " ")) {
	case "port", "terminal", "airport", "airbase", "refinery", "plant", "field", "facility", "complex", "industrial city":
		return true
	default:
		return false
	}
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
		Description:   affectedZoneNarrativeDescription(c),
		Tags: affectedZoneTags(c, cls, e).
			Merge(),
		Status:      string(horizonmodels.FragmentStatusActive),
		State:       map[string]horizonmodels.MetricValue{},
		Qualifiers:  affectedZoneQualifiersWithSource(c, cls, state, e, false),
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
	applyAffectedZonePrimaryEvidence(incoming, c, cls, e)
	applyAffectedZoneIdentity(incoming, c)
	applyAffectedZoneCoordinates(incoming, c)
	applyStickyOperationalOutageQualifiers(incoming, c, e)
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
	if desc := affectedZoneNarrativeDescription(c); desc != "" && shouldRefreshAffectedZoneDescription(dst, e) && strings.TrimSpace(dst.Description) != desc {
		dst.Description = desc
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
	if applyAffectedZoneIdentity(dst, c) {
		changed = true
	}
	if applyAffectedZoneCoordinates(dst, c) {
		changed = true
	}
	if applyStickyOperationalOutageQualifiers(dst, c, e) {
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
	for k, v := range affectedZoneQualifiersWithSource(c, cls, st, e, false) {
		if k == "" {
			continue
		}
		if fmt.Sprintf("%v", dst.Qualifiers[k]) != fmt.Sprintf("%v", v) {
			dst.Qualifiers[k] = v
			changed = true
		}
	}
	if applyAffectedZonePrimaryEvidence(dst, c, cls, e) {
		changed = true
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

func (p *HorizonMonitorPipeline) maybeCreateStrategicGapRecommendation(
	ctx context.Context,
	tenantID, projectID, lensID string,
	zone *horizonmodels.HorizonFragment,
	event *horizonMonitorFeedEvent,
	audit *horizonMonitorSignalFlowAudit,
) {
	if p == nil || p.strategicRecommendations == nil || zone == nil || zone.Type != horizonmodels.FragmentTypeAffectedZone {
		return
	}
	input := horizonrecommendations.DetectionInput{
		TenantID:             strings.TrimSpace(tenantID),
		ProjectID:            strings.TrimSpace(projectID),
		LensID:               strings.TrimSpace(lensID),
		SourceID:             firstNonEmptyString(event.SourceID),
		EventTitle:           strings.TrimSpace(firstNonEmptyString(event.Title)),
		EventSummary:         strings.TrimSpace(firstNonEmptyString(event.Summary)),
		SubjectCanonicalPath: strings.TrimSpace(zone.CanonicalPath),
		SubjectType:          horizonmodels.StrategicRecommendationSubjectAffectedZone,
		OccurredAt:           time.Now().UTC(),
	}
	if event != nil && !event.OccurredAt.IsZero() {
		input.OccurredAt = event.OccurredAt.UTC()
	}
	if audit != nil {
		input.SignalAxis = strings.TrimSpace(audit.Axis)
		input.BusinessEndpoints = audit.BusinessEndpoints
		input.RemoteEndpoints = audit.RemoteEndpoints
		input.PathFamilies = audit.PathFamilies
	}
	if _, _, err := p.strategicRecommendations.Detect(ctx, input); err != nil {
		p.logf("horizon monitor pipeline: strategic recommendation detect failed zone=%s: %v", zone.CanonicalPath, err)
	}
}

func (p *HorizonMonitorPipeline) emitWatchRuntimeSignals(
	ctx context.Context,
	tenantID string,
	zone *horizonmodels.HorizonFragment,
	event *horizonMonitorFeedEvent,
	audit *horizonMonitorSignalFlowAudit,
) {
	if p == nil || p.watchTriggerHook == nil {
		return
	}
	tenantID = strings.TrimSpace(firstNonEmptyString(tenantID, "global"))
	anchors := p.watchTriggerAnchorsForZone(ctx, tenantID, zone)
	if len(anchors) == 0 {
		anchors = watchTriggerAnchorsForEventFallback(event)
	}
	if len(anchors) == 0 {
		return
	}
	baseMeta := map[string]any{}
	if zone != nil {
		baseMeta["zoneFragmentId"] = strings.TrimSpace(zone.ID)
		baseMeta["zoneType"] = string(zone.Type)
	}
	if event != nil {
		baseMeta["eventTitle"] = strings.TrimSpace(event.Title)
		baseMeta["eventSourceId"] = strings.TrimSpace(event.SourceID)
		baseMeta["severity"] = strings.TrimSpace(event.Severity)
		baseMeta["eventCountryCode"] = strings.ToUpper(strings.TrimSpace(event.CountryCode))
	}
	for _, anchor := range anchors {
		meta := mergeStringAnyMaps(baseMeta, map[string]any{
			"watchAnchor":     anchor,
			"watchAnchorType": watchAnchorType(zone, event, anchor),
		})
		_ = p.watchTriggerHook(ctx, tenantID, anchor, horizonmodels.WatchTriggerAffectedZoneChanged, "affected zone updated by monitor pipeline", 0.85, 0.90, meta)
	}
	if audit == nil {
		return
	}
	signalMeta := mergeStringAnyMaps(baseMeta, map[string]any{
		"axis":                 strings.TrimSpace(audit.Axis),
		"seedCount":            audit.SeedCount,
		"affectedFragments":    audit.AffectedFragments,
		"businessEndpoints":    audit.BusinessEndpoints,
		"remoteEndpoints":      audit.RemoteEndpoints,
		"pathFamilies":         audit.PathFamilies,
		"businessPathFamilies": audit.BusinessPathFamilies,
		"crossBorderFamilies":  audit.CrossBorderFamilies,
		"nonObviousEndpoints":  audit.NonObviousEndpoints,
		"escalationLevel":      strings.TrimSpace(audit.EscalationLevel),
	})
	for _, anchor := range anchors {
		meta := mergeStringAnyMaps(signalMeta, map[string]any{
			"watchAnchor":     anchor,
			"watchAnchorType": watchAnchorType(zone, event, anchor),
		})
		_ = p.watchTriggerHook(ctx, tenantID, anchor, horizonmodels.WatchTriggerSignalDetected, "signal propagated from horizon monitor pipeline", 0.90, 0.85, meta)
		if audit.PropagationTried && audit.SeedCount > 0 && audit.RemoteEndpoints == 0 && audit.BusinessPathFamilies == 0 {
			_ = p.watchTriggerHook(ctx, tenantID, anchor, horizonmodels.WatchTriggerPropagationGap, "propagation gap after signal propagation", 0.70, 0.80, meta)
		}
	}
}

func (p *HorizonMonitorPipeline) watchTriggerAnchorsForZone(ctx context.Context, tenantID string, zone *horizonmodels.HorizonFragment) []string {
	if zone == nil || zone.Type != horizonmodels.FragmentTypeAffectedZone {
		return nil
	}
	anchors := make([]string, 0, 12)
	seen := make(map[string]struct{}, 12)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		anchors = append(anchors, path)
	}
	add(zone.CanonicalPath)
	add(zone.ParentPath)
	add(zone.ParentGovPath)
	add(zone.ParentMarketPath)
	add(zone.EntityPath)
	add(zone.AffiliatedOrgPath)
	for _, path := range zone.SpherePaths {
		add(path)
	}
	if p.edgesRepo != nil && strings.TrimSpace(zone.ID) != "" {
		edgeType := horizonmodels.DependencyTypeAffects
		if edges, err := p.edgesRepo.ListBySource(ctx, tenantID, strings.TrimSpace(zone.ID), &edgeType, 12); err == nil {
			for _, edge := range edges {
				if edge == nil || strings.TrimSpace(edge.TargetFragmentID) == "" || p.fragmentsRepo == nil {
					continue
				}
				target, targetErr := p.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID))
				if targetErr != nil || target == nil {
					continue
				}
				add(target.CanonicalPath)
				add(target.ParentPath)
				add(target.EntityPath)
			}
		}
	}
	return anchors
}

func watchTriggerAnchorsForEventFallback(event *horizonMonitorFeedEvent) []string {
	if event == nil {
		return nil
	}
	cc := strings.ToLower(strings.TrimSpace(event.CountryCode))
	if len(cc) != 2 {
		return nil
	}
	return []string{"/loc/" + cc}
}

func watchAnchorType(zone *horizonmodels.HorizonFragment, event *horizonMonitorFeedEvent, anchor string) string {
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return ""
	}
	if zone == nil {
		if event != nil && strings.EqualFold(anchor, "/loc/"+strings.TrimSpace(event.CountryCode)) {
			return "event_country"
		}
		return "event_fallback"
	}
	switch anchor {
	case strings.TrimSpace(zone.CanonicalPath):
		return "affected_zone"
	case strings.TrimSpace(zone.ParentPath), strings.TrimSpace(zone.ParentGovPath), strings.TrimSpace(zone.ParentMarketPath):
		return "zone_parent"
	case strings.TrimSpace(zone.EntityPath), strings.TrimSpace(zone.AffiliatedOrgPath):
		return "zone_entity"
	default:
		return "affected_target"
	}
}

func (p *HorizonMonitorPipeline) coolAffectedZones(ctx context.Context, tenantID, sourceID string, limit int) error {
	if p == nil || p.fragmentsRepo == nil {
		return nil
	}
	if limit <= 0 {
		limit = affectedZoneCoolingLimit
	}
	items, err := p.fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(firstNonEmptyString(tenantID, "global")), horizonmodels.FragmentTypeAffectedZone, limit)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, frag := range items {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
			continue
		}
		st := parseAffectedZoneState(frag)
		if now.Sub(st.LastUpdatedAt) < affectedZoneCoolingStep {
			continue
		}
		prevScore := st.Score
		prevHits1h := st.Hits1h
		prevHits24h := st.Hits24h
		st.applyDecay(now)
		stickyHeld := maintainStickyAffectedZoneActivity(frag, &st, now)
		if !stickyHeld && math.Abs(st.Score-prevScore) < 0.05 && math.Abs(st.Hits1h-prevHits1h) < 0.05 && math.Abs(st.Hits24h-prevHits24h) < 0.05 {
			continue
		}
		applyAffectedZoneState(frag, st, now, st.LastMatchedAt, sourceID, nil)
		if frag.Metadata.LastUpdatedAt.Before(now) {
			frag.Metadata.LastUpdatedAt = now
		}
		if frag.Metadata.LastVerifiedAt.Before(now) {
			frag.Metadata.LastVerifiedAt = now
		}
		if err := p.fragmentsRepo.Update(ctx, frag); err != nil {
			return err
		}
	}
	return nil
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

func applyAffectedZoneIdentity(f *horizonmodels.HorizonFragment, c *affectedZoneCandidate) bool {
	if f == nil || c == nil {
		return false
	}
	changed := false
	label := strings.TrimSpace(c.Label)
	if label == "" {
		label = affectedZoneNarrativeDescription(c)
	}
	if f.Identifiers == nil {
		f.Identifiers = map[string]string{}
	}
	if label != "" && strings.TrimSpace(f.Identifiers["legalName"]) != label {
		f.Identifiers["legalName"] = label
		changed = true
	}
	if label != "" && strings.TrimSpace(f.Identifiers["shortName"]) != label {
		f.Identifiers["shortName"] = label
		changed = true
	}
	if cp := strings.TrimSpace(f.CanonicalPath); cp != "" {
		mergedParts := make([]string, 0, 4)
		for _, raw := range strings.Split(strings.TrimSpace(f.Identifiers["merged_paths"]), ",") {
			if s := strings.TrimSpace(raw); s != "" {
				mergedParts = append(mergedParts, s)
			}
		}
		mergedParts = uniqueStrings(append(mergedParts, cp))
		merged := strings.Join(mergedParts, ",")
		if strings.TrimSpace(f.Identifiers["merged_paths"]) != merged {
			f.Identifiers["merged_paths"] = merged
			changed = true
		}
	}
	if f.HomeLocation == nil {
		f.HomeLocation = &horizonmodels.Location{}
	}
	if label != "" && strings.TrimSpace(f.HomeLocation.Name) != label {
		f.HomeLocation.Name = label
		changed = true
	}
	if cc := strings.ToUpper(strings.TrimSpace(c.CountryCode)); len(cc) == 2 {
		if f.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*f.HomeLocation.ISO3166_1) != cc {
			ccCopy := cc
			f.HomeLocation.ISO3166_1 = &ccCopy
			changed = true
		}
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
		sourceRefs := []horizonmodels.SourceRef{}
		if e != nil {
			if refID := firstNonEmptyString(strings.TrimSpace(e.URL), strings.TrimSpace(e.ExternalID)); refID != "" {
				sourceRefs = append(sourceRefs, horizonmodels.SourceRef{SourceID: refID, SourceType: "rss"})
			}
		}
		f.State[name] = horizonmodels.MetricValue{
			Value:      value,
			Confidence: confidence,
			ObservedAt: observedAt,
			Provenance: horizonmodels.Provenance{
				Method:     "heuristic_rss_zone",
				SourceIDs:  uniqueStrings([]string{strings.TrimSpace(sourceID)}),
				SourceRefs: sourceRefs,
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
	applyAffectedZoneStatusQualifiers(f, st)
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

func applyAffectedZoneStatusQualifiers(f *horizonmodels.HorizonFragment, st affectedZoneState) {
	if f == nil {
		return
	}
	if f.Qualifiers == nil {
		f.Qualifiers = map[string]interface{}{}
	}
	f.Qualifiers["hypeStatus"] = activityStatus(st.Score)
	f.Qualifiers["hypeIndicator"] = hypeIndicator(st.Score)
	if !st.LastMatchedAt.IsZero() {
		f.Qualifiers["lastMatchedAt"] = st.LastMatchedAt.UTC().Format(time.RFC3339)
	}
}

func qualifierBoolValue(qualifiers map[string]interface{}, key string) bool {
	if len(qualifiers) == 0 || strings.TrimSpace(key) == "" {
		return false
	}
	switch v := qualifiers[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func affectedZoneRecoveryStatus(f *horizonmodels.HorizonFragment) string {
	if f == nil || len(f.Qualifiers) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(asString(f.Qualifiers["recoveryStatus"])))
}

func applyStickyOperationalOutageQualifiers(
	f *horizonmodels.HorizonFragment,
	c *affectedZoneCandidate,
	e *horizonMonitorFeedEvent,
) bool {
	if f == nil {
		return false
	}
	text := ""
	conditionType := ""
	if c != nil {
		conditionType = strings.TrimSpace(c.ConditionType)
	}
	if f.Qualifiers != nil && conditionType == "" {
		conditionType = strings.TrimSpace(qualifierStringValue(f.Qualifiers, "conditionType"))
	}
	if e != nil {
		text = normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
	}
	if text == "" {
		text = normalizeKeywordText(strings.Join([]string{
			qualifierStringValue(f.Qualifiers, "sourceTitle"),
			qualifierStringValue(f.Qualifiers, "sourceSummary"),
			f.Description,
			f.Labels["en"],
		}, " "))
	}
	sticky := looksLikeStickyOperationalOutage(text, conditionType)
	if !sticky && !qualifierBoolValue(f.Qualifiers, "requiresExplicitRecovery") {
		return false
	}
	if f.Qualifiers == nil {
		f.Qualifiers = map[string]interface{}{}
	}
	changed := false
	if qualifierBoolValue(f.Qualifiers, "requiresExplicitRecovery") != sticky {
		f.Qualifiers["requiresExplicitRecovery"] = sticky
		changed = true
	}
	if sticky {
		status := affectedZoneRecoveryStatusActive
		current := affectedZoneRecoveryStatus(f)
		if current == "" || current == affectedZoneRecoveryStatusRecovering {
			if current != status {
				f.Qualifiers["recoveryStatus"] = status
				changed = true
			}
		}
		if e != nil && !e.OccurredAt.IsZero() {
			ts := e.OccurredAt.UTC().Format(time.RFC3339)
			if qualifierStringValue(f.Qualifiers, "outageObservedAt") != ts {
				f.Qualifiers["outageObservedAt"] = ts
				changed = true
			}
		}
	}
	return changed
}

func applyStickyRecoveryObservation(
	f *horizonmodels.HorizonFragment,
	e *horizonMonitorFeedEvent,
	now time.Time,
) bool {
	if f == nil || e == nil {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
	if !looksLikeAffectedZoneRecoveryText(text) || !qualifierBoolValue(f.Qualifiers, "requiresExplicitRecovery") {
		return false
	}
	if f.Qualifiers == nil {
		f.Qualifiers = map[string]interface{}{}
	}
	if affectedZoneRecoveryStatus(f) != affectedZoneRecoveryStatusRestored {
		f.Qualifiers["recoveryStatus"] = affectedZoneRecoveryStatusRestored
	}
	recoveryAt := now.UTC()
	if !e.OccurredAt.IsZero() {
		recoveryAt = e.OccurredAt.UTC()
	}
	if qualifierStringValue(f.Qualifiers, "recoveryObservedAt") != recoveryAt.Format(time.RFC3339) {
		f.Qualifiers["recoveryObservedAt"] = recoveryAt.Format(time.RFC3339)
	}
	st := parseAffectedZoneState(f)
	st.applyDecay(now)
	st.Score = math.Min(st.Score, 18)
	st.Hits1h = 0
	st.Hits24h = math.Min(st.Hits24h, 2)
	st.LastBoost = 0
	applyAffectedZoneState(f, st, now, st.LastMatchedAt, "", e)
	return true
}

func maintainStickyAffectedZoneActivity(f *horizonmodels.HorizonFragment, st *affectedZoneState, now time.Time) bool {
	if f == nil || st == nil || !qualifierBoolValue(f.Qualifiers, "requiresExplicitRecovery") {
		return false
	}
	if affectedZoneRecoveryStatus(f) == affectedZoneRecoveryStatusRestored {
		return false
	}
	age := now.Sub(st.LastMatchedAt)
	if age < 0 {
		age = 0
	}
	floor := 26.0
	if age <= 72*time.Hour {
		floor = 42.0
	}
	changed := false
	if st.Score < floor {
		st.Score = floor
		changed = true
	}
	if st.Hits24h < 1 {
		st.Hits24h = 1
		changed = true
	}
	if f.Qualifiers == nil {
		f.Qualifiers = map[string]interface{}{}
	}
	if affectedZoneRecoveryStatus(f) != affectedZoneRecoveryStatusActive {
		f.Qualifiers["recoveryStatus"] = affectedZoneRecoveryStatusActive
		changed = true
	}
	return changed
}

func affectedZoneBoost(e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification, c *affectedZoneCandidate) float64 {
	levelW := map[string]float64{
		"critical": 1.55,
		"high":     1.30,
		"medium":   1.0,
		"low":      0.75,
		"info":     0.55,
	}
	severityW := map[string]float64{
		"critical": 1.4,
		"high":     1.18,
		"medium":   1.0,
		"low":      0.8,
	}
	base := 14.0 * firstPositive(levelW[strings.ToLower(strings.TrimSpace(cls.Level))], 0.6)
	base *= firstPositive(severityW[strings.ToLower(strings.TrimSpace(e.Severity))], 1.0)
	if c != nil {
		base *= 0.9 + wmClamp01(c.EventConfidence)*0.45
		base *= 0.95 + wmClamp01(c.GeoConfidence)*0.35
	}
	conditionType := ""
	if c != nil {
		conditionType = strings.TrimSpace(c.ConditionType)
	}
	switch conditionType {
	case "military_conflict":
		base *= 2.25
	case "earthquake_affected", "flood_zone", "wildfire_zone":
		base *= 1.18
	case "humanitarian_crisis":
		base *= 1.10
	case "cyber_disruption", "infrastructure_disruption":
		base *= 1.35
	}
	if c != nil && c.GeoPrecision == "entity_anchor" {
		base *= 1.12
	}
	if c != nil && looksLikeOperationalPlaceLabel(c.Label) {
		base *= 1.10
	}
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(firstNonEmptyString(e.Title, "")),
		strings.TrimSpace(firstNonEmptyString(e.Summary, "")),
		strings.TrimSpace(cls.Category),
	}, " "))
	if looksLikeConcreteOperationalIncident(text, cls) {
		switch conditionType {
		case "military_conflict":
			base = math.Max(base, 34)
		case "infrastructure_disruption":
			base = math.Max(base, 28)
		}
	}
	if hasAnyKeyword(text, "port", "terminal", "strait", "canal", "shipping", "loading operations", "drone attack", "missile strike", "projectile", "embassy hit") {
		base *= 1.12
	}
	if e.HasCoordinates && horizonmodels.IsRealGeoCoordinate(e.Lat, e.Lon) {
		base *= 1.05
	}
	if c != nil && c.WarmOnly {
		base *= 0.72
	}
	if e != nil && !e.OccurredAt.IsZero() {
		age := time.Since(e.OccurredAt.UTC())
		switch {
		case age > 14*24*time.Hour:
			base *= 0.22
		case age > 7*24*time.Hour:
			base *= 0.40
		case age > 72*time.Hour:
			base *= 0.62
		case age > 24*time.Hour:
			base *= 0.82
		}
	}
	return clamp(base, 4.0, 72.0)
}

func affectedZoneQualifiers(
	c *affectedZoneCandidate,
	cls horizonMonitorNewsClassification,
	st affectedZoneState,
	e *horizonMonitorFeedEvent,
) map[string]interface{} {
	return affectedZoneQualifiersWithSource(c, cls, st, e, true)
}

func affectedZoneQualifiersWithSource(
	c *affectedZoneCandidate,
	cls horizonMonitorNewsClassification,
	st affectedZoneState,
	e *horizonMonitorFeedEvent,
	includeSource bool,
) map[string]interface{} {
	out := map[string]interface{}{
		"conditionType":            strings.TrimSpace(c.ConditionType),
		"zoneType":                 strings.TrimSpace(c.ZoneType),
		"zoneSubType":              strings.TrimSpace(c.ZoneSubType),
		"zoneInferenceScore":       c.InferenceScore,
		"zoneEventConfidence":      c.EventConfidence,
		"zoneGeoConfidence":        c.GeoConfidence,
		"zoneResolutionConfidence": c.ResolutionConfidence,
		"classificationCategory":   strings.TrimSpace(cls.Category),
		"classificationLevel":      strings.TrimSpace(cls.Level),
		"classificationSource":     strings.TrimSpace(cls.Source),
		"processingMode":           "heuristic",
		"llm":                      false,
		"sourceLayer":              "rss",
		"processingLayer":          "heuristic",
		"geoPrecision":             strings.TrimSpace(c.GeoPrecision),
		"geoResolutionMethod":      strings.TrimSpace(c.GeoResolutionMethod),
		"warmOnly":                 c.WarmOnly,
		"hypeStatus":               activityStatus(st.Score),
		"hypeIndicator":            hypeIndicator(st.Score),
		"lastMatchedAt":            st.LastMatchedAt.UTC().Format(time.RFC3339),
		"extraction":               horizonMonitorFragmentExtraction("heuristic", false),
	}
	if strings.TrimSpace(c.Reason) != "" {
		out["reason"] = strings.TrimSpace(c.Reason)
	}
	if includeSource {
		if e != nil && strings.TrimSpace(e.URL) != "" {
			out["sourceUrl"] = strings.TrimSpace(e.URL)
		}
		if e != nil && strings.TrimSpace(e.Title) != "" {
			out["sourceTitle"] = strings.TrimSpace(e.Title)
		}
		if e != nil && strings.TrimSpace(e.Summary) != "" {
			out["sourceSummary"] = truncateString(strings.TrimSpace(e.Summary), 1200)
		}
		if e != nil && !e.OccurredAt.IsZero() {
			out["sourceOccurredAt"] = e.OccurredAt.UTC().Format(time.RFC3339)
		}
		if e != nil && strings.TrimSpace(e.CountryCode) != "" {
			out["sourceCountryCode"] = strings.ToUpper(strings.TrimSpace(e.CountryCode))
		}
	}
	return out
}

func affectedZoneNarrativeDescription(c *affectedZoneCandidate) string {
	if c == nil {
		return ""
	}
	label := strings.TrimSpace(c.Label)
	subtype := strings.TrimSpace(c.ZoneSubType)
	cond := strings.ReplaceAll(strings.TrimSpace(c.ConditionType), "_", " ")
	switch {
	case label != "" && subtype != "":
		return label + " — " + strings.ReplaceAll(subtype, "_", " ")
	case label != "" && cond != "":
		return label + " — " + cond
	case label != "":
		return label
	default:
		return strings.TrimSpace(c.CanonicalPath)
	}
}

func shouldRefreshAffectedZoneDescription(dst *horizonmodels.HorizonFragment, e *horizonMonitorFeedEvent) bool {
	if dst == nil {
		return false
	}
	desc := strings.TrimSpace(dst.Description)
	if desc == "" {
		return true
	}
	if e == nil {
		return false
	}
	title := strings.TrimSpace(e.Title)
	if title == "" {
		return false
	}
	if desc == title {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(qualifierStringValue(dst.Qualifiers, "sourceTitle")), title) {
		return true
	}
	return false
}

func applyAffectedZonePrimaryEvidence(
	f *horizonmodels.HorizonFragment,
	c *affectedZoneCandidate,
	cls horizonMonitorNewsClassification,
	e *horizonMonitorFeedEvent,
) bool {
	if f == nil || e == nil {
		return false
	}
	if f.Qualifiers == nil {
		f.Qualifiers = map[string]interface{}{}
	}
	if !shouldPromoteAffectedZonePrimaryEvidence(f, c, cls, e) {
		return false
	}
	changed := false
	for k, v := range affectedZoneQualifiersWithSource(c, cls, parseAffectedZoneState(f), e, true) {
		if !strings.HasPrefix(k, "source") {
			continue
		}
		if fmt.Sprintf("%v", f.Qualifiers[k]) != fmt.Sprintf("%v", v) {
			f.Qualifiers[k] = v
			changed = true
		}
	}
	score := affectedZonePrimaryEvidenceScore(c, cls, e)
	if fmt.Sprintf("%v", f.Qualifiers["primarySourceScore"]) != fmt.Sprintf("%.3f", score) {
		f.Qualifiers["primarySourceScore"] = score
		changed = true
	}
	if fmt.Sprintf("%v", f.Qualifiers["primarySourceClassificationCategory"]) != strings.TrimSpace(cls.Category) {
		f.Qualifiers["primarySourceClassificationCategory"] = strings.TrimSpace(cls.Category)
		changed = true
	}
	if fmt.Sprintf("%v", f.Qualifiers["primarySourceClassificationLevel"]) != strings.TrimSpace(cls.Level) {
		f.Qualifiers["primarySourceClassificationLevel"] = strings.TrimSpace(cls.Level)
		changed = true
	}
	if desc := affectedZoneNarrativeDescription(c); desc != "" && strings.TrimSpace(f.Description) != desc {
		f.Description = desc
		changed = true
	}
	return changed
}

func shouldPromoteAffectedZonePrimaryEvidence(
	f *horizonmodels.HorizonFragment,
	c *affectedZoneCandidate,
	cls horizonMonitorNewsClassification,
	e *horizonMonitorFeedEvent,
) bool {
	if f == nil || e == nil {
		return false
	}
	newScore := affectedZonePrimaryEvidenceScore(c, cls, e)
	if newScore <= 0 {
		return false
	}
	currentScore := toFloatDefault(qualifierStringValue(f.Qualifiers, "primarySourceScore"), 0)
	currentTitle := strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceTitle"))
	if currentTitle == "" || currentScore <= 0 {
		return true
	}
	currentOccurredAt := parseRFC3339Loose(qualifierStringValue(f.Qualifiers, "sourceOccurredAt"))
	newOccurredAt := e.OccurredAt.UTC()
	if currentScore < 0.40 && newScore >= currentScore {
		return true
	}
	if newScore >= currentScore+0.18 {
		return true
	}
	if currentOccurredAt.IsZero() {
		return newScore >= currentScore
	}
	if !newOccurredAt.IsZero() && newOccurredAt.After(currentOccurredAt) && newScore >= currentScore+0.08 {
		return true
	}
	return false
}

func affectedZonePrimaryEvidenceScore(c *affectedZoneCandidate, cls horizonMonitorNewsClassification, e *horizonMonitorFeedEvent) float64 {
	if e == nil {
		return 0
	}
	text := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
	profile := wmHeuristics().ProfileArticle(text, affectedZoneSourceHost(e))
	if shouldSuppressAffectedZoneBySourceProfile(e) || shouldSuppressAffectedZoneBySemanticDrift(text, e, cls) {
		return 0
	}
	conditionType := ""
	if c != nil {
		conditionType = strings.TrimSpace(c.ConditionType)
	}
	if shouldSuppressAffectedZoneByEventProfile(text, e, cls, conditionType, profile) {
		return 0
	}
	if c != nil && shouldSuppressWeakAffectedZoneInference(text, e, cls, strings.TrimSpace(c.ConditionType), c.InferenceScore) {
		return 0
	}
	score := 0.35
	if hasStrongEventSignal(text) {
		score += 0.24
	}
	if hasDirectDisruptionTrigger(text) {
		score += 0.18
	}
	if profile.ConcreteOutageScore >= 0.35 {
		score += 0.15
	}
	if c != nil {
		score += clamp(c.InferenceScore, 0, 1) * 0.20
		if strings.EqualFold(c.GeoPrecision, "precise") {
			score += 0.08
		} else if strings.EqualFold(c.GeoPrecision, "approximate_country") {
			score -= 0.05
		}
		if c.WarmOnly {
			score -= 0.10
		}
	}
	if strings.EqualFold(strings.TrimSpace(cls.Level), "critical") || strings.EqualFold(strings.TrimSpace(cls.Level), "high") {
		score += 0.06
	}
	if profile.AnalysisScore >= 0.35 || profile.AdvisoryScore >= 0.35 || profile.BroadScopeScore >= 0.30 {
		score -= 0.25
	}
	if looksLikeCapabilityOrFeatureText(text) {
		score -= 0.28
	}
	return clamp(score, 0, 1)
}

func parseRFC3339Loose(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func shouldUseAffectedZoneStoredSourceEvidence(f *horizonmodels.HorizonFragment) bool {
	if f == nil {
		return false
	}
	title := strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceTitle"))
	if title == "" {
		return false
	}
	score := toFloatDefault(qualifierStringValue(f.Qualifiers, "primarySourceScore"), 0)
	if score < 0.45 {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{
		title,
		qualifierStringValue(f.Qualifiers, "sourceSummary"),
		qualifierStringValue(f.Qualifiers, "classificationCategory"),
		qualifierStringValue(f.Qualifiers, "classificationLevel"),
	}, " "))
	event := &horizonMonitorFeedEvent{
		Title:       title,
		Summary:     qualifierStringValue(f.Qualifiers, "sourceSummary"),
		URL:         qualifierStringValue(f.Qualifiers, "sourceUrl"),
		CountryCode: qualifierStringValue(f.Qualifiers, "sourceCountryCode"),
	}
	cls := horizonMonitorNewsClassification{
		Category: qualifierStringValue(f.Qualifiers, "classificationCategory"),
		Level:    qualifierStringValue(f.Qualifiers, "classificationLevel"),
		Source:   qualifierStringValue(f.Qualifiers, "classificationSource"),
	}
	profile := wmHeuristics().ProfileArticle(text, affectedZoneSourceHost(event))
	conditionType := qualifierStringValue(f.Qualifiers, "conditionType")
	storedCC := strings.ToUpper(strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceCountryCode")))
	if storedCC != "" {
		explicitTextCC := inferCountryCodeFromAffectedZoneText(strings.Join([]string{
			title,
			qualifierStringValue(f.Qualifiers, "sourceSummary"),
		}, " "), storedCC)
		if explicitTextCC != "" && !strings.EqualFold(explicitTextCC, storedCC) {
			return false
		}
		if place := firstNonEmptyString(
			extractKnownPlaceFromText(strings.Join([]string{title, qualifierStringValue(f.Qualifiers, "sourceSummary")}, " ")),
			extractPlaceFromTitle(title),
		); place != "" {
			if _, placeCC, _ := inferZoneGeoFromPlace(place); placeCC != "" && !strings.EqualFold(placeCC, storedCC) {
				return false
			}
		}
	}
	if shouldSuppressAffectedZoneBySourceProfile(event) ||
		shouldSuppressAffectedZoneBySemanticDrift(text, event, cls) ||
		shouldSuppressAffectedZoneByEventProfile(text, event, cls, conditionType, profile) ||
		shouldSuppressWeakAffectedZoneInference(text, event, cls, conditionType, toFloatDefault(qualifierStringValue(f.Qualifiers, "zoneInferenceScore"), 0)) {
		return false
	}
	return true
}

func clearAffectedZonePrimaryEvidence(f *horizonmodels.HorizonFragment) bool {
	if f == nil || f.Qualifiers == nil {
		return false
	}
	changed := false
	for _, key := range []string{
		"sourceTitle",
		"sourceSummary",
		"sourceUrl",
		"sourceOccurredAt",
		"sourceCountryCode",
		"primarySourceScore",
		"primarySourceClassificationCategory",
		"primarySourceClassificationLevel",
	} {
		if _, ok := f.Qualifiers[key]; ok {
			delete(f.Qualifiers, key)
			changed = true
		}
	}
	return changed
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

func (p *HorizonMonitorPipeline) inferAffectedZoneCandidate(ctx context.Context, e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification) *affectedZoneCandidate {
	return inferAffectedZoneCandidateWithContext(ctx, p, e, cls)
}

func inferAffectedZoneCandidate(e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification) *affectedZoneCandidate {
	return inferAffectedZoneCandidateWithContext(context.Background(), nil, e, cls)
}

func affectedZoneEventConfidence(e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification) float64 {
	eventSeverityConfidence := 0.0
	if e != nil {
		eventSeverityConfidence = e.Confidence
	}
	conf := wmClamp01(firstPositive(cls.Confidence, eventSeverityConfidence))
	if e != nil && e.Raw != nil {
		if assessment, ok := e.Raw["nlpArticleAssessment"].(map[string]interface{}); ok {
			if v, ok := asFloat64(assessment["eventConfidence"]); ok {
				conf = wmClamp01(math.Max(conf, v))
			}
		}
	}
	title := ""
	summary := ""
	if e != nil {
		title = e.Title
		summary = e.Summary
	}
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(title),
		strings.TrimSpace(summary),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	if looksLikeConcreteOperationalIncident(text, cls) {
		conf = wmClamp01(math.Max(conf, 0.86))
	}
	return conf
}

func affectedZoneGeoConfidence(place, countryCode, countryName, geoPrecision string, hasCoord bool, anchor *affectedZoneEntityAnchor) float64 {
	place = strings.TrimSpace(place)
	countryCode = strings.TrimSpace(countryCode)
	countryName = strings.TrimSpace(countryName)
	geoPrecision = strings.TrimSpace(strings.ToLower(geoPrecision))
	score := 0.18
	if isPlausiblePlaceName(place) && !isWeakAffectedZonePlace(place, countryName) && !looksLikeBadAffectedZonePlaceHint(place) {
		score += 0.24
	}
	switch geoPrecision {
	case "entity_anchor":
		score += 0.34
	case "precise":
		score += 0.30
	case "approximate_country":
		score += 0.10
	}
	if hasCoord {
		score += 0.12
	}
	if anchor != nil && anchor.HasCoordinates {
		score += 0.12
	}
	if countryCode != "" {
		score += 0.08
	}
	placeNorm := normalizePhrase(place)
	if _, ok := zoneHotspotCoords[placeNorm]; ok {
		score += 0.12
	}
	if hasAnyKeyword(placeNorm, "port", "terminal", "refinery", "airport", "airbase", "island", "strait", "canal", "field", "complex") {
		score += 0.08
	}
	if placeNorm == normalizePhrase(countryName) && geoPrecision == "approximate_country" {
		score -= 0.16
	}
	return wmClamp01(score)
}

func affectedZoneResolutionConfidence(
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	place, countryCode, countryName, geoPrecision, conditionType string,
	hasCoord bool,
	anchor *affectedZoneEntityAnchor,
) float64 {
	eventConf := affectedZoneEventConfidence(e, cls)
	geoConf := affectedZoneGeoConfidence(place, countryCode, countryName, geoPrecision, hasCoord, anchor)
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(firstNonEmptyString(e.Title, "")),
		strings.TrimSpace(firstNonEmptyString(e.Summary, "")),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	penalty := 0.0
	if looksLikeLiveBlogPoliticalCommentaryText(text) || looksLikeRetrospectiveConflictNarrativeText(text) {
		penalty += 0.12
	}
	if looksLikeMultiCountryDeploymentRoundup(text, cls) {
		penalty += 0.10
	}
	if strings.TrimSpace(conditionType) == "military_conflict" && looksLikeConcreteOperationalIncident(text, cls) {
		eventConf = wmClamp01(math.Max(eventConf, 0.90))
	}
	return wmClamp01(eventConf*0.58 + geoConf*0.42 - penalty)
}

func shouldAttemptAffectedZoneGeoAIRefinement(
	p *HorizonMonitorPipeline,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	place, countryName, geoPrecision string,
	hasCoord bool,
	resolutionConfidence float64,
) bool {
	if p == nil || p.exec == nil || e == nil {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(e.Title),
		strings.TrimSpace(e.Summary),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	if !looksLikeConcreteOperationalIncident(text, cls) {
		return false
	}
	if resolutionConfidence >= 0.72 {
		return false
	}
	if affectedZoneEventConfidence(e, cls) < 0.78 {
		return false
	}
	if hasCoord && (geoPrecision == "precise" || geoPrecision == "entity_anchor") && !isWeakAffectedZonePlace(place, countryName) {
		return false
	}
	return isWeakAffectedZonePlace(place, countryName) || geoPrecision == "none" || geoPrecision == "approximate_country"
}

func (p *HorizonMonitorPipeline) refineAffectedZoneGeoWithAI(
	ctx context.Context,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	place, countryCode, countryName string,
) (affectedZoneGeoAssistOutput, bool) {
	if p == nil || p.exec == nil || e == nil {
		return affectedZoneGeoAssistOutput{}, false
	}
	entityBits := make([]string, 0, 6)
	for _, entity := range extractNLPEntityMaps(e.Raw["nlpEntities"]) {
		surface := strings.TrimSpace(firstNonEmptyString(asString(entity["normalized"]), asString(entity["surface"])))
		if surface == "" {
			continue
		}
		entityBits = append(entityBits, fmt.Sprintf("%s[%s/%s]", surface, normalizePhrase(asString(entity["entityType"])), normalizePhrase(asString(entity["role"]))))
		if len(entityBits) >= 6 {
			break
		}
	}
	prompt := strings.Builder{}
	prompt.WriteString("Identify the single most specific operational location impacted by the incident. Return strict JSON only.\n")
	prompt.WriteString("Do not return a country unless the article gives no more specific location.\n")
	prompt.WriteString("If the article is too vague, return an empty place and confidence below 0.5.\n")
	prompt.WriteString("Headline: ")
	prompt.WriteString(strings.TrimSpace(e.Title))
	prompt.WriteString("\nSummary: ")
	prompt.WriteString(truncateString(strings.TrimSpace(e.Summary), 500))
	prompt.WriteString("\nCategory: ")
	prompt.WriteString(strings.TrimSpace(cls.Category))
	prompt.WriteString("\nLevel: ")
	prompt.WriteString(strings.TrimSpace(cls.Level))
	prompt.WriteString("\nCurrentPlaceHint: ")
	prompt.WriteString(strings.TrimSpace(place))
	prompt.WriteString("\nCurrentCountryHint: ")
	prompt.WriteString(strings.TrimSpace(firstNonEmptyString(countryCode, countryName)))
	if len(entityBits) > 0 {
		prompt.WriteString("\nNlpEntities: ")
		prompt.WriteString(strings.Join(entityBits, ", "))
	}
	prompt.WriteString("\nSchema: {\"place\": string, \"countryCode\": string, \"geoKind\": string, \"confidence\": number}\n")

	verifier := ai.NewGenericVerifier(func(out affectedZoneGeoAssistOutput) error {
		if out.Confidence < 0 || out.Confidence > 1 {
			return fmt.Errorf("confidence must be in [0,1]")
		}
		if cc := strings.TrimSpace(out.CountryCode); cc != "" && len(normalizeCountryCode(cc)) != 2 {
			return fmt.Errorf("invalid countryCode")
		}
		switch strings.TrimSpace(strings.ToLower(out.GeoKind)) {
		case "", "facility", "port", "city", "region", "country", "chokepoint", "island", "airport", "terminal", "refinery":
		default:
			return fmt.Errorf("invalid geoKind")
		}
		return nil
	})
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContextAnalysis,
		InputContent: prompt.String(),
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            160,
		},
		Verifier: verifier,
	}
	parsed, err := p.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return affectedZoneGeoAssistOutput{}, false
	}
	out, ok := parsed.(affectedZoneGeoAssistOutput)
	if !ok {
		return affectedZoneGeoAssistOutput{}, false
	}
	out.Place = strings.TrimSpace(out.Place)
	out.CountryCode = normalizeCountryCode(out.CountryCode)
	out.GeoKind = strings.TrimSpace(strings.ToLower(out.GeoKind))
	out.Confidence = wmClamp01(out.Confidence)
	if out.Place == "" || out.Confidence < 0.76 || !isPlausiblePlaceName(out.Place) || looksLikeBadAffectedZonePlaceHint(out.Place) {
		return affectedZoneGeoAssistOutput{}, false
	}
	return out, true
}

func inferAffectedZoneCandidateWithContext(ctx context.Context, p *HorizonMonitorPipeline, e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification) *affectedZoneCandidate {
	if e == nil {
		return nil
	}
	if nlpAssessmentSuppressesZone(e, cls) {
		return nil
	}
	if shouldSuppressAffectedZoneBySourceProfile(e) {
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
	concreteIncident := looksLikeConcreteOperationalIncident(text, cls)
	credibleChokepointThreat := looksLikeCredibleChokepointThreat(text, cls)
	profile := wmHeuristics().ProfileArticle(text, affectedZoneSourceHost(e))
	if looksLikeExplainerQuestionText(text) && !concreteIncident && !credibleChokepointThreat {
		return nil
	}
	if isLikelyNonEventText(text) && !concreteIncident && !looksLikeWarmConflictPosture(text, cls) && !credibleChokepointThreat {
		return nil
	}
	if shouldSuppressAffectedZoneBySemanticDrift(text, e, cls) && !concreteIncident && !credibleChokepointThreat {
		return nil
	}
	conditionType, zoneType, zoneSubType, inferenceScore := inferZoneConditionAndSubtypeScored(text, cls)
	if llmCond, llmZoneType, llmZoneSubType, llmScore, ok := llmAffectedZoneConditionOverride(e); ok {
		conditionType = llmCond
		zoneType = llmZoneType
		zoneSubType = llmZoneSubType
		inferenceScore = math.Max(inferenceScore, llmScore)
	}
	if conditionType == "" {
		return nil
	}
	if shouldSuppressAffectedZoneByEventProfile(text, e, cls, conditionType, profile) && !concreteIncident && !credibleChokepointThreat {
		return nil
	}
	if shouldSuppressWeakAffectedZoneInference(text, e, cls, conditionType, inferenceScore) {
		return nil
	}
	warmOnly := !shouldCreateAffectedZone(text, cls.Level, conditionType)

	place := inferZonePlaceName(e, text, conditionType)
	geoToken, countryCode, countryName := inferZoneGeo(e, place, text, conditionType)
	if warmOnly && geoToken == "global" && e != nil {
		if cc := normalizeCountryCode(e.CountryCode); cc != "" {
			geoToken = strings.ToLower(cc)
			countryCode = cc
			countryName = firstNonEmptyString(strings.TrimSpace(e.CountryName), countryNameFromCode(cc))
		}
	}
	var anchor *affectedZoneEntityAnchor
	if shouldAttemptAffectedZoneEntityAnchor(place, countryName, "none", false, e) {
		anchorCountryCode := countryCode
		if shouldRelaxAffectedZoneAnchorCountryHint(place, countryName, countryCode) {
			anchorCountryCode = ""
		}
		anchor = resolveAffectedZoneEntityAnchor(ctx, p, e, place, anchorCountryCode)
	}
	if anchor != nil {
		if isWeakAffectedZonePlace(place, countryName) || placeSpecificScopeLooksUnreliable(place, countryName, "approximate_country", false) {
			place = firstNonEmptyString(anchor.Place, place)
		}
		if strings.TrimSpace(countryCode) == "" {
			countryCode = strings.TrimSpace(anchor.CountryCode)
		}
		if strings.TrimSpace(countryName) == "" {
			countryName = strings.TrimSpace(anchor.CountryName)
		}
		if strings.TrimSpace(geoToken) == "" || strings.EqualFold(strings.TrimSpace(geoToken), "global") {
			geoToken = strings.TrimSpace(anchor.GeoToken)
		}
	}
	if strings.TrimSpace(place) == "" {
		place = firstNonEmptyString(countryName, countryCode, "emerging hotspot")
	}
	place = normalizeAffectedZonePlace(place, countryName)
	eventConfidence := affectedZoneEventConfidence(e, cls)
	if shouldSuppressAffectedZoneByPlaceQuality(place, countryName, conditionType, text, e) {
		return nil
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
	resolutionConfidence := affectedZoneResolutionConfidence(e, cls, place, countryCode, countryName, geoPrecision, conditionType, hasCoord, anchor)
	if shouldPreferAffectedZoneAnchor(anchor, place, countryName, geoPrecision, hasCoord) {
		place = firstNonEmptyString(anchor.Place, place)
		geoToken = firstNonEmptyString(anchor.GeoToken, geoToken)
		countryCode = firstNonEmptyString(anchor.CountryCode, countryCode)
		countryName = firstNonEmptyString(anchor.CountryName, countryName)
		lat = anchor.Lat
		lon = anchor.Lon
		hasCoord = anchor.HasCoordinates
		geoPrecision = strings.TrimSpace(anchor.GeoPrecision)
	}
	if shouldAttemptAffectedZoneGeoAIRefinement(p, e, cls, place, countryName, geoPrecision, hasCoord, resolutionConfidence) {
		if refined, ok := p.refineAffectedZoneGeoWithAI(ctx, e, cls, place, countryCode, countryName); ok {
			place = normalizeAffectedZonePlace(refined.Place, countryName)
			if refined.CountryCode != "" {
				countryCode = refined.CountryCode
				countryName = firstNonEmptyString(countryNameFromCode(refined.CountryCode), countryName)
			}
			if g, cc, cn := inferZoneGeoFromPlace(place); g != "" {
				geoToken = g
				countryCode = firstNonEmptyString(cc, countryCode)
				countryName = firstNonEmptyString(cn, countryName)
			}
			lat, lon, hasCoord, geoPrecision = inferAffectedZoneCoordinates(e, place, countryCode)
			if hasCoord && geoPrecision == "precise" {
				anchor = nil
			}
			resolutionConfidence = affectedZoneResolutionConfidence(e, cls, place, countryCode, countryName, geoPrecision, conditionType, hasCoord, anchor)
		}
	}
	if placeSpecificScopeLooksUnreliable(place, countryName, geoPrecision, hasCoord) {
		place = firstNonEmptyString(countryName, countryNameFromCode(countryCode), place)
		pathSlug = canonicalPathSlug(place)
		if pathSlug == "" || pathSlug == "na" {
			pathSlug = "emerging-hotspot"
		}
		label = humanizeZoneLabel(place, countryName, zoneSubType)
	}
	if shouldSuppressAffectedZoneByGeoCoherence(place, geoToken, countryCode, geoPrecision, e) {
		return nil
	}
	if shouldSuppressGenericAffectedZone(geoToken, place, pathSlug, conditionType) {
		return nil
	}

	return &affectedZoneCandidate{
		CanonicalPath:        "/zone/" + geoToken + "/" + conditionType + "/" + pathSlug,
		Label:                label,
		ConditionType:        conditionType,
		ZoneType:             zoneType,
		ZoneSubType:          zoneSubType,
		InferenceScore:       inferenceScore,
		EventConfidence:      eventConfidence,
		GeoConfidence:        affectedZoneGeoConfidence(place, countryCode, countryName, geoPrecision, hasCoord, anchor),
		ResolutionConfidence: resolutionConfidence,
		GeoToken:             geoToken,
		CountryCode:          strings.ToUpper(strings.TrimSpace(countryCode)),
		CountryName:          strings.TrimSpace(countryName),
		RadiusKm:             inferZoneRadiusKm(conditionType, cls.Level, e.Severity),
		Lat:                  lat,
		Lon:                  lon,
		HasCoordinates:       hasCoord,
		GeoPrecision:         geoPrecision,
		GeoResolutionMethod:  firstNonEmptyString(strings.TrimSpace(geoPrecision), "heuristic"),
		LinkedFragmentPaths:  collectAffectedZoneLinkedFragmentPathsWithContext(ctx, p, e, anchor),
		WarmOnly:             warmOnly,
		Reason:               "keyword_heuristic",
	}
}

func llmAffectedZoneConditionOverride(e *horizonMonitorFeedEvent) (conditionType, zoneType, zoneSubType string, score float64, ok bool) {
	if e == nil || e.Raw == nil {
		return "", "", "", 0, false
	}
	raw, has := e.Raw["llmArticleAdjudication"]
	if !has {
		return "", "", "", 0, false
	}
	m, okMap := raw.(map[string]interface{})
	if !okMap || len(m) == 0 {
		return "", "", "", 0, false
	}
	isEvent, _ := m["isEvent"].(bool)
	if !isEvent {
		return "", "", "", 0, false
	}
	eventConfidence, _ := asFloat64(m["eventConfidence"])
	if eventConfidence < 0.88 {
		return "", "", "", 0, false
	}
	switch strings.TrimSpace(asString(m["conditionType"])) {
	case "military_conflict":
		return "military_conflict", "military_conflict", "armed_tension", eventConfidence, true
	case "infrastructure_disruption":
		return "infrastructure_disruption", "infrastructure_disruption", "systems_disruption", eventConfidence, true
	case "cyber_disruption":
		return "cyber_disruption", "infrastructure_disruption", "systems_disruption", eventConfidence, true
	case "political_tension":
		return "political_tension", "political_tension", "civil_tension", eventConfidence, true
	case "humanitarian_crisis":
		return "humanitarian_crisis", "humanitarian_crisis", "humanitarian_emergency", eventConfidence, true
	case "disease_outbreak":
		return "disease_outbreak", "humanitarian_crisis", "epidemic", eventConfidence, true
	case "environmental_hazard":
		return "environmental_hazard", "natural_disaster", "toxic_pollution", eventConfidence, true
	case "natural_disaster":
		return "natural_disaster", "natural_disaster", "multi_hazard", eventConfidence, true
	case "earthquake_affected":
		return "earthquake_affected", "natural_disaster", "seismic_impact", eventConfidence, true
	case "flood_zone":
		return "flood_zone", "natural_disaster", "flooding", eventConfidence, true
	case "wildfire_zone":
		return "wildfire_zone", "natural_disaster", "wildfire", eventConfidence, true
	default:
		return "", "", "", 0, false
	}
}

func collectAffectedZoneLinkedFragmentPaths(e *horizonMonitorFeedEvent, anchor *affectedZoneEntityAnchor) []string {
	if e == nil {
		return nil
	}
	paths := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if !looksLikeAffectedZoneLinkPath(path) {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	add(strings.TrimSpace(e.LocationPath))
	if anchor != nil {
		add(strings.TrimSpace(anchor.FragmentPath))
	}
	for _, path := range heuristicSignalSeedPaths(e, strings.TrimSpace(e.LocationPath)) {
		if strings.HasPrefix(strings.TrimSpace(path), "/loc/int/") {
			add(path)
		}
	}
	return pruneAffectedZoneLinkedFragmentPaths(paths)
}

func collectAffectedZoneLinkedFragmentPathsWithContext(
	ctx context.Context,
	p *HorizonMonitorPipeline,
	e *horizonMonitorFeedEvent,
	anchor *affectedZoneEntityAnchor,
) []string {
	paths := collectAffectedZoneLinkedFragmentPaths(e, anchor)
	if e == nil || p == nil || p.fragmentsRepo == nil {
		return paths
	}
	seen := make(map[string]struct{}, len(paths)+8)
	for _, path := range paths {
		if path = strings.TrimSpace(path); path != "" {
			seen[path] = struct{}{}
		}
	}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if !looksLikeAffectedZoneLinkPath(path) {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}

	seeds := extractAffectedZoneObjectAnchors(e, 6)
	if anchor != nil && strings.TrimSpace(anchor.Place) != "" {
		seeds = uniqueStrings(append([]string{strings.TrimSpace(anchor.Place)}, seeds...))
	}
	if place := strings.TrimSpace(extractPlaceFromTitle(strings.TrimSpace(e.Title))); place != "" && isPlausiblePlaceName(place) {
		seeds = uniqueStrings(append([]string{place}, seeds...))
	}
	wantCountry := strings.ToUpper(strings.TrimSpace(firstNonEmptyString(e.CountryCode)))
	for _, path := range resolveAffectedZoneLinkedFragments(ctx, p, e, wantCountry, seeds, 4) {
		add(path)
	}
	return pruneAffectedZoneLinkedFragmentPaths(paths)
}

func resolveAffectedZoneLinkedFragments(
	ctx context.Context,
	p *HorizonMonitorPipeline,
	e *horizonMonitorFeedEvent,
	wantCountry string,
	seeds []string,
	maxCount int,
) []string {
	if p == nil || p.fragmentsRepo == nil || e == nil || maxCount <= 0 {
		return nil
	}
	type scoredPath struct {
		path  string
		score float64
	}
	bestByPath := map[string]scoredPath{}
	record := func(path string, score float64) {
		path = strings.TrimSpace(path)
		if !looksLikeAffectedZoneLinkPath(path) || score <= 0 {
			return
		}
		prev, ok := bestByPath[path]
		if ok && prev.score >= score {
			return
		}
		bestByPath[path] = scoredPath{path: path, score: score}
	}

	for _, seed := range seeds {
		seed = strings.TrimSpace(seed)
		if seed == "" {
			continue
		}
		for _, frag := range exactAffectedZoneAnchorFragments(ctx, p, seed) {
			score := affectedZoneLinkedFragmentScore(ctx, seed, wantCountry, e, frag, 0.96, p)
			if score < 0.82 {
				continue
			}
			record(strings.TrimSpace(frag.CanonicalPath), score)
			if frag.Type == horizonmodels.FragmentTypeOrganization {
				if fallback, fallbackScore := resolveAffectedZoneOperationalAnchorFromOrg(ctx, p, frag, e, wantCountry); fallbackScore > 0 && strings.TrimSpace(fallback.FragmentPath) != "" {
					record(strings.TrimSpace(fallback.FragmentPath), score+fallbackScore)
				}
			}
		}
		if p.vertexSearch == nil || !p.vertexSearch.IsConfigured() {
			continue
		}
		resp, err := p.vertexSearch.Search(ctx, &ai.SearchRequest{Query: seed, PageSize: 6})
		if err != nil || resp == nil {
			continue
		}
		for _, result := range resp.Results {
			score := result.RelevanceScore
			if score == 0 && result.RankSignals != nil {
				score = result.RankSignals.SemanticSimilarityScore
			}
			if score < 0.72 {
				continue
			}
			fragID := extractFragmentIDFromGCSURI(result.Document.URI)
			if fragID == "" {
				fragID = strings.TrimSuffix(strings.TrimSpace(result.Document.ID), ".json")
			}
			if fragID == "" {
				continue
			}
			frag, ferr := p.fragmentsRepo.Get(ctx, fragID)
			if ferr != nil || frag == nil {
				continue
			}
			linkScore := affectedZoneLinkedFragmentScore(ctx, seed, wantCountry, e, frag, score, p)
			if linkScore < 0.82 {
				continue
			}
			record(strings.TrimSpace(frag.CanonicalPath), linkScore)
			if frag.Type == horizonmodels.FragmentTypeOrganization {
				if fallback, fallbackScore := resolveAffectedZoneOperationalAnchorFromOrg(ctx, p, frag, e, wantCountry); fallbackScore > 0 && strings.TrimSpace(fallback.FragmentPath) != "" {
					record(strings.TrimSpace(fallback.FragmentPath), linkScore+fallbackScore)
				}
			}
		}
	}

	scored := make([]scoredPath, 0, len(bestByPath))
	for _, item := range bestByPath {
		scored = append(scored, item)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].path < scored[j].path
		}
		return scored[i].score > scored[j].score
	})
	limit := maxCount
	if len(scored) < limit {
		limit = len(scored)
	}
	out := make([]string, 0, limit)
	for _, item := range scored {
		out = append(out, item.path)
		if len(out) >= maxCount {
			break
		}
	}
	return out
}

func pruneAffectedZoneLinkedFragmentPaths(paths []string) []string {
	if len(paths) <= 1 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for i, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if isGeoRegionLikeCanonicalPath(path) {
			redundant := false
			for j, other := range paths {
				if i == j {
					continue
				}
				other = strings.TrimSpace(other)
				if isMoreSpecificAffectedZoneGeoPath(other, path) {
					redundant = true
					break
				}
			}
			if redundant {
				continue
			}
		}
		out = append(out, path)
	}
	return out
}

func looksLikeAffectedZoneLinkPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return false
	}
	switch {
	case strings.HasPrefix(path, "/loc/"),
		strings.HasPrefix(path, "/geo/"),
		strings.HasPrefix(path, "/region/"),
		strings.HasPrefix(path, "/fac/"),
		strings.HasPrefix(path, "/org/"),
		strings.HasPrefix(path, "/deposit/"),
		strings.HasPrefix(path, "/product/"),
		strings.HasPrefix(path, "/program/"):
		return true
	default:
		return false
	}
}

func nlpAssessmentSuppressesZone(e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification) bool {
	if e == nil || e.Raw == nil {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(e.Title),
		strings.TrimSpace(e.Summary),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	if looksLikeConcreteOperationalIncident(text, cls) {
		return false
	}
	m, ok := e.Raw["nlpArticleAssessment"].(map[string]interface{})
	if !ok || len(m) == 0 {
		return false
	}
	shouldCreate, _ := m["shouldCreateZone"].(bool)
	shouldWarm, _ := m["shouldWarmExisting"].(bool)
	shouldOnlySignal, _ := m["shouldOnlySignal"].(bool)
	if shouldCreate || shouldWarm || shouldOnlySignal {
		return false
	}
	if rejected, ok := m["rejectionReason"].(string); ok && strings.TrimSpace(rejected) != "" {
		return true
	}
	if isEvent, ok := m["isEvent"].(bool); ok && !isEvent {
		return true
	}
	return false
}

func shouldCreateAffectedZone(text, level, conditionType string) bool {
	if text == "" || isLikelyNonEventText(text) {
		return false
	}
	if looksLikeCredibleChokepointThreat(text, horizonMonitorNewsClassification{}) {
		return conditionType == "military_conflict" || conditionType == "infrastructure_disruption"
	}
	if looksLikeConcreteOperationalIncident(text, horizonMonitorNewsClassification{}) {
		switch conditionType {
		case "military_conflict", "infrastructure_disruption", "earthquake_affected", "tsunami_affected", "volcanic_eruption", "flood_zone", "wildfire_zone", "humanitarian_crisis", "environmental_hazard":
			return true
		}
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
	conditionType, zoneType, zoneSubType, _ = inferZoneConditionAndSubtypeScored(text, cls)
	return conditionType, zoneType, zoneSubType
}

func inferZoneConditionAndSubtypeScored(text string, cls horizonMonitorNewsClassification) (conditionType, zoneType, zoneSubType string, score float64) {
	if looksLikeCredibleChokepointThreat(text, cls) {
		return "military_conflict", "military_conflict", "armed_tension", 1.16
	}
	if looksLikeLaborStrikeOperationalDisruption(text) {
		return "infrastructure_disruption", "infrastructure_disruption", "systems_disruption", 1.18
	}
	if cond, ztyp, sub, zoneScore := wmHeuristics().InferZoneCondition(text); cond != "" {
		if cond == "infrastructure_disruption" && looksLikeConflictDrivenOperationalDisruption(text) {
			return "military_conflict", "military_conflict", "armed_tension", math.Max(zoneScore, 1.24)
		}
		score = zoneScore
		return cond, ztyp, sub, score
	}
	if looksLikeConcreteOperationalIncident(text, cls) {
		if looksLikeConflictDrivenOperationalDisruption(text) {
			return "military_conflict", "military_conflict", "armed_tension", 1.24
		}
		if hasAnyKeyword(text,
			"port", "terminal", "shipping", "tanker", "strait", "canal", "loading operations",
			"gps jamming", "refinery", "depot", "pipeline", "airport", "airbase", "oil export", "oil facility",
		) {
			return "infrastructure_disruption", "infrastructure_disruption", "systems_disruption", 1.22
		}
		return "military_conflict", "military_conflict", "armed_tension", 1.22
	}
	switch strings.ToLower(strings.TrimSpace(cls.Category)) {
	case "military", "conflict":
		if looksLikeAdministrativeCommentaryText(text) {
			return "", "", "", 0
		}
		if looksLikeExerciseWithoutIncidentText(text) {
			return "", "", "", 0
		}
		if looksLikeCivilIncidentWithoutMilitaryContext(text) {
			return "political_tension", "political_tension", "public_security", 0.84
		}
		return "military_conflict", "military_conflict", "armed_tension", 1.18
	case "disaster":
		return "natural_disaster", "natural_disaster", "multi_hazard", 1.12
	case "health":
		return "disease_outbreak", "humanitarian_crisis", "epidemic", 1.10
	case "environmental":
		return "environmental_hazard", "natural_disaster", "toxic_pollution", 1.08
	case "terrorism":
		if looksLikeCivilIncidentWithoutMilitaryContext(text) {
			return "political_tension", "political_tension", "public_security", 0.90
		}
		return "military_conflict", "military_conflict", "terror_threat", 1.04
	case "crime":
		return "political_tension", "political_tension", "public_security", 0.92
	case "protest", "diplomatic":
		return "political_tension", "political_tension", "civil_tension", 0.90
	case "cyber", "infrastructure", "tech":
		if wmHeuristics().ProfileArticle(text, "").likelyAdvisoryCampaignWithoutOutage() {
			return "", "", "", 0
		}
		return "infrastructure_disruption", "infrastructure_disruption", "systems_disruption", 0.94
	case "economic":
		return "political_tension", "political_tension", "economic_pressure", 0.86
	default:
		return "", "", "", 0
	}
}

func shouldSuppressWeakAffectedZoneInference(
	text string,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	conditionType string,
	inferenceScore float64,
) bool {
	if strings.TrimSpace(conditionType) == "" {
		return true
	}
	if looksLikeConcreteOperationalIncident(text, cls) {
		return false
	}
	if looksLikeCredibleChokepointThreat(text, cls) {
		return false
	}
	if inferenceScore >= 1.55 {
		return false
	}
	text = normalizeKeywordText(text)
	eventScore := wmHeuristics().strongEventScore(text)
	host := affectedZoneSourceHost(e)
	profile := wmHeuristics().ProfileArticle(text, host)
	if inferenceScore < 0.92 {
		return true
	}
	if profile.likelyAnalysisRoundup() && !looksLikeWarmConflictPosture(text, cls) {
		return true
	}
	if hasAnyKeyword(text,
		"last 24 hours", "what to know", "live blog", "live updates", "roundup", "at a glance",
		"decision", "authorize", "implementation", "analysis", "opinion", "speech", "warn",
		"why ", "how ",
	) && eventScore < 1.65 {
		if !(strings.TrimSpace(conditionType) == "military_conflict" && hasAnyKeyword(text,
			"air defence", "air defense", "fighter", "interceptor", "deployment", "deterrence", "reinforce", "reinforcement", "typhoon", "navies", "regional confrontation",
		)) {
			return true
		}
	}
	switch strings.TrimSpace(conditionType) {
	case "political_tension":
		if looksLikeAdministrativeCommentaryText(text) && !hasAnyKeyword(text, "sanctions", "embargo", "border clash", "ceasefire collapse", "protest", "riot", "unrest") {
			return true
		}
		if eventScore < 1.20 && !hasAnyKeyword(text, "protest", "riot", "unrest", "state of emergency", "sanctions", "embargo", "border clash", "ceasefire collapse") {
			return true
		}
	case "infrastructure_disruption", "cyber_disruption":
		hasConcreteOutage := hasAnyKeyword(text, "outage", "blackout", "closure", "breach", "ransomware", "cable cut", "pipeline sabotage", "port closure", "shutdown", "offline", "operations suspended")
		if hasAnyKeyword(text, "no specific", "no concrete", "no confirmed", "without a concrete outage", "without confirmed disruption") {
			hasConcreteOutage = false
		}
		if profile.likelyAdvisoryCampaignWithoutOutage() {
			return true
		}
		if eventScore < 1.12 && !hasConcreteOutage {
			return true
		}
		if hasAnyKeyword(text, "countries", "multiple countries", "16 countries", "campaign", "botnet", "web shell", "researchers warn") &&
			!hasConcreteOutage {
			return true
		}
	case "military_conflict":
		if looksLikeAdministrativeCommentaryText(text) || looksLikeCivilIncidentWithoutMilitaryContext(text) || looksLikeCapabilityOrFeatureText(text) {
			return true
		}
		if looksLikeExerciseWithoutIncidentText(text) {
			return true
		}
		if hasAnyKeyword(text, "rewild borders", "countries can rewild borders", "deter invasions", "environment chief", "natural border control") {
			return true
		}
		if eventScore < 1.08 && !hasAnyKeyword(text,
			"attack", "strike", "missile", "shelling", "drone strike", "airstrike", "clash", "troops", "warship", "hostage", "terror attack",
			"air defence", "air defense", "fighter", "interceptor", "deployment", "deterrence", "reinforce", "reinforcement", "typhoon", "navies",
		) {
			return true
		}
	case "disease_outbreak":
		if looksLikeAdministrativeCommentaryText(text) || looksLikeCapabilityOrFeatureText(text) {
			return true
		}
		if !hasAnyKeyword(text,
			"outbreak", "epidemic", "pandemic", "cases", "infected", "infection", "cholera", "measles", "mpox",
			"dengue", "avian flu", "h5n1", "health emergency", "vaccination campaign", "vaccination resumes", "virus",
		) {
			return true
		}
	case "humanitarian_crisis":
		if looksLikeAdministrativeCommentaryText(text) || looksLikeCapabilityOrFeatureText(text) {
			return true
		}
		if !hasAnyKeyword(text,
			"famine", "aid", "refugee", "displaced", "humanitarian", "malnutrition", "shelter", "relief", "hunger",
			"cholera", "crisis", "evacuation", "civilians", "humanitarian corridor",
		) {
			return true
		}
	case "natural_disaster":
		if eventScore < 1.18 && !hasAnyKeyword(text, "hurricane", "typhoon", "cyclone", "storm surge", "displaced", "evacuation") {
			return true
		}
	}
	if profile.likelyBroadRegionalRoundup() && conditionType != "military_conflict" {
		return true
	}
	if (host == "realcleardefense.com" || host == "warontherocks.com" || host == "foreignpolicy.com") && inferenceScore < 1.35 && eventScore < 1.55 {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(cls.Category), "economic") && eventScore < 1.35 {
		return true
	}
	return false
}

func looksLikeWarmConflictPosture(text string, cls horizonMonitorNewsClassification) bool {
	if !strings.EqualFold(strings.TrimSpace(cls.Category), "military") && !strings.EqualFold(strings.TrimSpace(cls.Category), "conflict") {
		return false
	}
	text = normalizeKeywordText(text)
	if !hasAnyKeyword(text, "conflict", "tension", "regional confrontation", "force posture", "air defence", "air defense") {
		return false
	}
	return hasAnyKeyword(text,
		"fighter", "interceptor", "deployment", "deterrence", "reinforce", "reinforcement", "typhoon", "navies", "air defence", "air defense",
	)
}

func inferZoneGeo(e *horizonMonitorFeedEvent, place, text, conditionType string) (geoToken, countryCode, countryName string) {
	if llmPlace, llmCountry, ok := llmAffectedZoneGeoHints(e); ok {
		if geoToken, countryCode, countryName = inferZoneGeoFromPlace(llmPlace); geoToken != "" {
			return geoToken, countryCode, countryName
		}
		if llmCountry != "" {
			return strings.ToLower(llmCountry), llmCountry, countryNameFromCode(llmCountry)
		}
	}
	if geoToken, countryCode, countryName = inferZoneGeoFromPlace(place); geoToken != "" {
		return geoToken, countryCode, countryName
	}
	if cc, cn := inferZoneCountryFromNLPEntities(e); cc != "" {
		return strings.ToLower(cc), cc, firstNonEmptyString(cn, countryNameFromCode(cc))
	}
	if textCountry := inferCountryCodeFromAffectedZoneText(text, countryCode); textCountry != "" &&
		!looksLikeMultiCountryDeploymentRoundup(normalizeKeywordText(text), horizonMonitorNewsClassification{}) &&
		!looksLikeForcePostureDeploymentText(normalizeKeywordText(text), horizonMonitorNewsClassification{}) &&
		!looksLikeRetrospectiveConflictNarrativeText(normalizeKeywordText(text)) {
		return strings.ToLower(textCountry), textCountry, countryNameFromCode(textCountry)
	}
	countryCode = ""
	countryName = ""
	if e != nil {
		countryCode = strings.ToUpper(strings.TrimSpace(e.CountryCode))
		countryName = strings.TrimSpace(e.CountryName)
	}
	if countryCode == "" && e != nil {
		countryCode = inferCountryCodeFromText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
		if countryCode != "" {
			countryName = firstNonEmptyString(countryName, countryNameFromCode(countryCode))
		}
	}
	if lp := strings.TrimSpace(e.LocationPath); strings.HasPrefix(lp, "/loc/") {
		parts := strings.Split(strings.Trim(lp, "/"), "/")
		if len(parts) >= 2 && len(strings.TrimSpace(parts[1])) == 2 {
			cc := strings.ToUpper(strings.TrimSpace(parts[1]))
			if shouldTrustAffectedZoneCountryFallback(e, text, conditionType, cc) {
				return strings.ToLower(cc), cc, firstNonEmptyString(countryName, countryNameFromCode(cc))
			}
		}
	}
	if len(countryCode) == 2 && shouldTrustAffectedZoneCountryFallback(e, text, conditionType, countryCode) {
		return strings.ToLower(countryCode), countryCode, firstNonEmptyString(countryName, countryNameFromCode(countryCode))
	}
	if countryName == "" {
		countryName = "Global"
	}
	return "global", "", countryName
}

func inferZoneCountryFromNLPEntities(e *horizonMonitorFeedEvent) (countryCode, countryName string) {
	if e == nil || e.Raw == nil {
		return "", ""
	}
	bestScore := 0.0
	bestCode := ""
	bestName := ""
	for _, entity := range extractNLPEntityMaps(e.Raw["nlpEntities"]) {
		etype := normalizePhrase(asString(entity["entityType"]))
		role := normalizePhrase(asString(entity["role"]))
		cc := normalizeCountryCode(firstNonEmptyString(asString(entity["countryCode"]), asString(entity["geoHint"])))
		name := strings.TrimSpace(firstNonEmptyString(asString(entity["normalized"]), asString(entity["surface"])))
		if etype != "country" && etype != "city" && etype != "region" && role != "place" && role != "facility" && role != "target" {
			continue
		}
		score := 0.25
		if role == "place" || role == "facility" || role == "target" {
			score += 0.25
		}
		if cc != "" {
			score += 0.25
		}
		if conf, ok := asFloat64(entity["confidence"]); ok {
			score += conf * 0.25
		}
		if score > bestScore {
			bestScore = score
			bestCode = cc
			bestName = name
		}
	}
	if bestCode == "" {
		return "", ""
	}
	if bestName == "" {
		bestName = countryNameFromCode(bestCode)
	}
	return strings.ToUpper(bestCode), bestName
}

func shouldTrustAffectedZoneCountryFallback(e *horizonMonitorFeedEvent, text, conditionType, countryCode string) bool {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" {
		return false
	}
	if e != nil {
		if lp := strings.TrimSpace(e.LocationPath); strings.HasPrefix(lp, "/loc/") {
			if strings.EqualFold(countryFromLocationPathString(lp), countryCode) {
				return true
			}
		}
	}
	text = normalizeKeywordText(text)
	countryName := normalizeKeywordText(countryNameFromCode(countryCode))
	hasCountryMention := countryName != "" && strings.Contains(text, countryName)
	switch strings.TrimSpace(conditionType) {
	case "military_conflict", "political_tension":
		return false
	case "cyber_disruption", "infrastructure_disruption":
		return hasCountryMention &&
			hasAnyKeyword(text, "outage", "shutdown", "closure", "blackout", "offline", "spill", "port closure", "operations suspended") &&
			affectedZoneSourceHost(e) != "news.google.com"
	default:
		return hasCountryMention || affectedZoneSourceHost(e) == "earthquake.usgs.gov" || affectedZoneSourceHost(e) == "who.int"
	}
}

func inferZonePlaceName(e *horizonMonitorFeedEvent, text, conditionType string) string {
	if e == nil {
		return ""
	}
	if place, _, ok := llmAffectedZoneGeoHints(e); ok && isPlausiblePlaceName(place) {
		return place
	}
	if v := extractPossessiveOperationalPlaceFromTitle(strings.TrimSpace(e.Title)); isPlausiblePlaceName(v) {
		return v
	}
	if v := extractKnownMaritimeChokepointFromText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")); isPlausiblePlaceName(v) {
		return v
	}
	if v := inferZonePlaceFromNLPEntities(e); isPlausiblePlaceName(v) {
		return v
	}
	if v := extractUSGSPlaceFromTitle(strings.TrimSpace(e.Title)); isPlausiblePlaceName(v) {
		return v
	}
	for _, key := range []string{
		"city", "region", "province", "state", "district", "location", "place", "area",
		"governorate", "oblast", "county", "locality", "prefecture",
	} {
		if v := strings.TrimSpace(asString(e.Raw[key])); isPlausiblePlaceName(v) {
			return v
		}
	}
	if v := extractPlaceFromTitle(strings.TrimSpace(e.Title)); isPlausiblePlaceName(v) && !isGenericAffectedZonePlace(v) {
		return v
	}
	if v := extractKnownPlaceFromText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")); isPlausiblePlaceName(v) {
		return v
	}
	if !shouldUseCountryNameAsAffectedZonePlaceFallback(e, text, conditionType) {
		return ""
	}
	return firstNonEmptyString(strings.TrimSpace(e.CountryName), countryNameFromCode(strings.TrimSpace(e.CountryCode)))
}

func llmAffectedZoneGeoHints(e *horizonMonitorFeedEvent) (place string, countryCode string, ok bool) {
	if e == nil || e.Raw == nil {
		return "", "", false
	}
	raw, has := e.Raw["llmArticleAdjudication"]
	if !has {
		return "", "", false
	}
	m, okMap := raw.(map[string]interface{})
	if !okMap || len(m) == 0 {
		return "", "", false
	}
	isEvent, _ := m["isEvent"].(bool)
	if !isEvent {
		return "", "", false
	}
	eventConfidence, _ := asFloat64(m["eventConfidence"])
	geoConfidence, _ := asFloat64(m["geoConfidence"])
	if eventConfidence < 0.86 || geoConfidence < 0.82 {
		return "", "", false
	}
	place = strings.TrimSpace(asString(m["place"]))
	countryCode = normalizeCountryCode(asString(m["countryCode"]))
	if place == "" {
		return "", "", false
	}
	return place, countryCode, true
}

func inferZonePlaceFromNLPEntities(e *horizonMonitorFeedEvent) string {
	if e == nil || e.Raw == nil {
		return ""
	}
	entities := extractNLPEntityMaps(e.Raw["nlpEntities"])
	bestScore := 0.0
	best := ""
	for _, entity := range entities {
		etype := normalizePhrase(asString(entity["entityType"]))
		role := normalizePhrase(asString(entity["role"]))
		surface := strings.TrimSpace(firstNonEmptyString(asString(entity["normalized"]), asString(entity["surface"]), asString(entity["geoHint"])))
		if surface == "" {
			continue
		}
		score := 0.0
		switch etype {
		case "facility", "port", "terminal", "pipeline", "refinery", "fab", "chokepoint", "route", "city", "region", "country":
			score += 0.4
		}
		switch role {
		case "place", "facility", "route", "chokepoint", "target":
			score += 0.35
		}
		if cc := strings.ToUpper(strings.TrimSpace(asString(entity["countryCode"]))); cc != "" && strings.EqualFold(cc, e.CountryCode) {
			score += 0.12
		}
		if conf, ok := asFloat64(entity["confidence"]); ok {
			score += conf * 0.3
		}
		if score > bestScore && isPlausiblePlaceName(surface) && !isGenericAffectedZonePlace(surface) {
			bestScore = score
			best = surface
		}
	}
	return best
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
	if len(strings.Fields(norm)) > 4 {
		return false
	}
	bad := map[string]struct{}{
		"breaking": {}, "analysis": {}, "news": {}, "report": {}, "update": {},
		"military": {}, "conflict": {}, "disaster": {}, "situation": {}, "region submenu": {},
	}
	if _, ok := bad[norm]; ok {
		return false
	}
	badTokens := map[string]struct{}{
		"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "being": {},
		"wants": {}, "want": {}, "why": {}, "how": {}, "disrupting": {}, "deepen": {},
		"delivers": {}, "validates": {}, "eliminating": {}, "lessons": {},
		"decision": {}, "authorize": {}, "implementation": {}, "hours": {}, "warn": {}, "warning": {},
		"analysis": {}, "report": {}, "roundup": {}, "glance": {}, "live": {}, "updates": {},
	}
	for _, token := range strings.Fields(norm) {
		if _, ok := badTokens[token]; ok {
			return false
		}
	}
	if !hasAnyKeyword(norm,
		"sea", "strait", "canal", "gulf", "region", "valley", "basin", "corridor",
		"province", "state", "city", "county", "district", "prefecture", "governorate", "oblast",
		"island", "islands", "coast", "coastal", "port", "terminal", "airport", "field", "complex",
	) && hasAnyKeyword(norm, "decision", "authorize", "implementation", "report", "analysis", "warning", "hours") {
		return false
	}
	return true
}

func extractPlaceFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if possessive := extractPossessiveOperationalPlaceFromTitle(title); possessive != "" {
		return possessive
	}
	if prefix := extractPlacePrefixFromTitle(title); prefix != "" {
		return prefix
	}
	clean := strings.NewReplacer(",", " ", ";", " ", ":", " ", "(", " ", ")", " ", "|", " ", "\"", " ", "'", " ").Replace(title)
	words := strings.Fields(clean)
	if len(words) == 0 {
		return ""
	}
	preps := map[string]struct{}{
		"in": {}, "near": {}, "around": {}, "outside": {}, "at": {}, "on": {}, "off": {}, "across": {}, "from": {}, "of": {}, "to": {},
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
			if lower == "of" && len(parts) > 0 && geographicHeadAllowsOf(parts[0]) {
				parts = append(parts, token)
				continue
			}
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
			candidate := strings.Join(parts, " ")
			if isLikelyTitlePlaceCandidate(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func extractPossessiveOperationalPlaceFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	clean := strings.NewReplacer(",", " ", ";", " ", ":", " ", "(", " ", ")", " ", "|", " ", "\"", " ").Replace(title)
	words := strings.Fields(clean)
	if len(words) < 2 {
		return ""
	}
	stop := map[string]struct{}{
		"stops": {}, "stop": {}, "works": {}, "work": {}, "hit": {}, "hits": {}, "under": {}, "after": {},
		"before": {}, "amid": {}, "as": {}, "says": {}, "say": {}, "vows": {}, "to": {}, "will": {},
		"could": {}, "should": {}, "did": {}, "does": {}, "warns": {}, "warn": {}, "why": {}, "what": {},
		"where": {}, "how": {}, "when": {}, "remains": {}, "remain": {}, "orders": {}, "order": {},
		"policy": {}, "plan": {}, "stance": {}, "position": {}, "response": {},
	}
	for i, raw := range words[:len(words)-1] {
		token := strings.TrimSpace(raw)
		if !strings.Contains(token, "'s") && !strings.Contains(token, "’s") {
			continue
		}
		owner := strings.TrimSuffix(strings.TrimSuffix(token, "'s"), "’s")
		if owner == "" {
			continue
		}
		if normalizeCountryCode(owner) == "" && !strings.EqualFold(strings.TrimSpace(owner), "UAE") && inferCountryCodeFromText(owner) == "" {
			continue
		}
		parts := make([]string, 0, 4)
		for j := i + 1; j < len(words) && len(parts) < 4; j++ {
			part := strings.Trim(words[j], " .-_/")
			if part == "" {
				break
			}
			lower := strings.ToLower(part)
			if _, blocked := stop[lower]; blocked {
				break
			}
			if lower == "of" && len(parts) > 0 && geographicHeadAllowsOf(parts[0]) {
				parts = append(parts, part)
				continue
			}
			runes := []rune(part)
			if len(parts) > 0 && (len(runes) == 0 || !(runes[0] >= 'A' && runes[0] <= 'Z')) &&
				!hasAnyKeyword(lower, "port", "terminal", "island", "strait", "canal", "airport", "airbase", "refinery", "field") {
				break
			}
			parts = append(parts, part)
			if hasAnyKeyword(lower, "port", "terminal", "island", "strait", "canal", "airport", "airbase", "refinery", "field") {
				if j+1 < len(words) && strings.EqualFold(strings.Trim(words[j+1], " .-_/"), "of") && geographicHeadAllowsOf(part) {
					continue
				}
				break
			}
		}
		candidate := strings.TrimSpace(strings.Join(parts, " "))
		if candidate == "" {
			continue
		}
		if isLikelyTitlePlaceCandidate(candidate) || isPlausiblePlaceName(candidate) {
			return candidate
		}
	}
	return ""
}

func affectedZoneMaritimeIdentityKey(parts ...string) string {
	text := normalizePhrase(strings.Join(parts, " "))
	if text == "" {
		return ""
	}
	return normalizePhrase(extractKnownMaritimeChokepointFromText(text))
}

func affectedZoneSemanticMatchAllowsDifferentMaritimeIdentity(wantIdentity, slug, canonicalPath string) bool {
	wantIdentity = normalizePhrase(wantIdentity)
	if wantIdentity == "" {
		return true
	}
	gotIdentity := affectedZoneMaritimeIdentityKey(strings.ReplaceAll(strings.TrimSpace(slug), "-", " "), canonicalPath)
	return gotIdentity == "" || gotIdentity == wantIdentity
}

func extractPlacePrefixFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	for _, sep := range []string{":", " - ", " — "} {
		if idx := strings.Index(title, sep); idx > 0 {
			candidate := strings.TrimSpace(title[:idx])
			if isLikelyTitlePlaceCandidate(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func shouldUseCountryNameAsAffectedZonePlaceFallback(e *horizonMonitorFeedEvent, text, conditionType string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		text = normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
	}
	if text == "" {
		return false
	}
	if extractKnownMaritimeChokepointFromText(text) != "" {
		return false
	}
	if looksLikeExerciseWithoutIncidentText(text) ||
		looksLikeForcePostureDeploymentText(text, horizonMonitorNewsClassification{}) ||
		looksLikeDrugInterdictionText(text) ||
		looksLikeAdministrativeCommentaryText(text) ||
		looksLikeLiveBlogPoliticalCommentaryText(text) ||
		looksLikeCivilIncidentWithoutMilitaryContext(text) ||
		looksLikeCyberCampaignWithoutOperationalImpact(text) ||
		looksLikeMultiCountryDeploymentRoundup(text, horizonMonitorNewsClassification{}) ||
		looksLikeRetrospectiveConflictNarrativeText(text) ||
		looksLikeLegalCommercialDisputeText(text) ||
		looksLikeSportsPoliticalHumanInterestText(text) {
		return false
	}
	switch strings.TrimSpace(conditionType) {
	case "military_conflict", "political_tension", "humanitarian_crisis":
		if len(extractCountryCodesFromText(text, 3)) > 1 {
			return false
		}
	}
	cc := ""
	if e != nil {
		cc = strings.ToUpper(strings.TrimSpace(e.CountryCode))
		if lp := strings.TrimSpace(e.LocationPath); strings.HasPrefix(lp, "/loc/") {
			if lpCC := strings.ToUpper(strings.TrimSpace(countryFromLocationPathString(lp))); lpCC != "" {
				cc = lpCC
			}
		}
	}
	if cc == "" {
		return false
	}
	if strings.Contains(text, normalizeKeywordText(countryNameFromCode(cc))) {
		return true
	}
	return false
}

func geographicHeadAllowsOf(head string) bool {
	switch normalizePhrase(head) {
	case "strait", "gulf", "sea", "canal", "port", "bay", "cape", "bank":
		return true
	default:
		return false
	}
}

func isGenericAffectedZonePlace(place string) bool {
	switch normalizePhrase(place) {
	case "strait", "gulf", "sea", "canal", "region", "middle east":
		return true
	default:
		return false
	}
}

var usgsMagnitudeTitlePlaceRE = regexp.MustCompile(`(?i)^m\s*\d+(?:\.\d+)?\s*-\s*(.+)$`)
var usgsDistancePrefixRE = regexp.MustCompile(`(?i)^\d+\s*km\s+[a-z-]+\s+of\s+`)

func extractUSGSPlaceFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	m := usgsMagnitudeTitlePlaceRE.FindStringSubmatch(title)
	if len(m) != 2 {
		return ""
	}
	candidate := strings.TrimSpace(m[1])
	candidate = usgsDistancePrefixRE.ReplaceAllString(candidate, "")
	candidate = strings.Trim(candidate, " -.,")
	return candidate
}

func isLikelyTitlePlaceCandidate(candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if !isPlausiblePlaceName(candidate) {
		return false
	}
	norm := normalizePhrase(candidate)
	if norm == "" {
		return false
	}
	if looksLikeMilitaryAssetName(norm) {
		return false
	}
	if _, ok := zoneHotspotCoords[norm]; ok {
		return true
	}
	if inferCountryCodeFromText(candidate) != "" {
		return true
	}
	if hasAnyKeyword(norm,
		"sea", "strait", "canal", "gulf", "region", "valley", "basin", "corridor",
		"province", "state", "city", "county", "district", "prefecture", "governorate", "oblast",
		"island", "islands", "coast", "coastal", "port", "terminal", "airport", "field", "complex",
	) {
		return true
	}
	upperTokens := 0
	for _, part := range strings.Fields(candidate) {
		if part == "" {
			continue
		}
		r := []rune(part)
		if len(r) > 0 && r[0] >= 'A' && r[0] <= 'Z' {
			upperTokens++
		}
	}
	return upperTokens >= 2
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

func affectedZoneSourceHost(e *horizonMonitorFeedEvent) string {
	if e == nil {
		return ""
	}
	raw := strings.TrimSpace(e.URL)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return strings.TrimPrefix(host, "www.")
}

func shouldSuppressAffectedZoneBySourceProfile(e *horizonMonitorFeedEvent) bool {
	if e == nil {
		return false
	}
	host := affectedZoneSourceHost(e)
	if host == "" {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
	eventScore := wmHeuristics().strongEventScore(text)
	switch host {
	case "realcleardefense.com", "warontherocks.com", "foreignpolicy.com":
		if eventScore < 1.65 {
			return true
		}
		if !strings.Contains(text, "attack") &&
			!strings.Contains(text, "strike") &&
			!strings.Contains(text, "missile") &&
			!strings.Contains(text, "earthquake") &&
			!strings.Contains(text, "outbreak") &&
			!strings.Contains(text, "outage") &&
			!strings.Contains(text, "blackout") &&
			!strings.Contains(text, "flood") &&
			!strings.Contains(text, "wildfire") {
			return true
		}
	case "who.int":
		if strings.Contains(text, "validates") ||
			strings.Contains(text, "eliminating") ||
			strings.Contains(text, "certified") ||
			strings.Contains(text, "progress") {
			return true
		}
	case "worldbank.org", "blogs.worldbank.org", "csis.org":
		if wmHeuristics().ProfileArticle(text, host).AnalysisScore >= 1.35 && eventScore < 1.85 {
			return true
		}
	}
	return false
}

func shouldSuppressAffectedZoneBySemanticDrift(text string, e *horizonMonitorFeedEvent, cls horizonMonitorNewsClassification) bool {
	if text == "" {
		return true
	}
	if looksLikeConcreteOperationalIncident(text, cls) {
		return false
	}
	eventScore := wmHeuristics().strongEventScore(text)
	if (strings.Contains(text, "strait of hormuz") ||
		strings.Contains(text, "suez canal") ||
		strings.Contains(text, "red sea") ||
		strings.Contains(text, "black sea") ||
		strings.Contains(text, "taiwan strait")) &&
		!looksLikeChokepointSecurityMissionWithoutOperationalDisruption(text) {
		return false
	}
	if strings.Contains(text, "at a glance") ||
		strings.Contains(text, "lessons from") ||
		strings.Contains(text, "foreign policy speech") ||
		strings.Contains(text, "public health failures") ||
		strings.Contains(text, "legacy of") ||
		strings.Contains(text, "paradox of") {
		if eventScore < 1.8 {
			return true
		}
	}
	if (strings.Contains(text, "why ") || strings.Contains(text, "how ")) && eventScore < 1.35 {
		return true
	}
	if hasAnyKeyword(text,
		"why did", "how long could", "what happens if", "what would have happened if",
		"what comes next", "what happened in", "by the numbers",
		"as it happened", "live blog", "live updates", "counterfactual",
	) {
		return true
	}
	if strings.Contains(text, "validates") ||
		strings.Contains(text, "eliminating mother to child transmission") {
		return true
	}
	if looksLikeCapabilityOrFeatureText(text) && !looksLikeWarmConflictPosture(text, cls) {
		return true
	}
	if looksLikeRetrospectiveConflictNarrativeText(text) {
		return true
	}
	if looksLikeLegalCommercialDisputeText(text) {
		return true
	}
	if looksLikeSportsPoliticalHumanInterestText(text) {
		return true
	}
	if looksLikeAdministrativeCommentaryText(text) && !looksLikeWarmConflictPosture(text, cls) {
		return true
	}
	if looksLikeExerciseWithoutIncidentText(text) {
		return true
	}
	if looksLikeForcePostureDeploymentText(text, cls) && !looksLikeWarmConflictPosture(text, cls) {
		return true
	}
	if looksLikeDrugInterdictionText(text) {
		return true
	}
	if looksLikeLiveBlogPoliticalCommentaryText(text) {
		return true
	}
	if looksLikeCivilIncidentWithoutMilitaryContext(text) {
		return true
	}
	if looksLikeCyberCampaignWithoutOperationalImpact(text) {
		return true
	}
	if looksLikeCompanyScopedCyberDataBreachWithoutOutage(text) {
		return true
	}
	if looksLikeOffshoreShippingIncidentAttributedElsewhere(text) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(cls.Category), "infrastructure") &&
		looksLikeChokepointSecurityMissionWithoutOperationalDisruption(text) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(cls.Category), "health") &&
		(strings.Contains(text, "validates") || strings.Contains(text, "eliminating")) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(cls.Category), "infrastructure") &&
		strings.Contains(text, "industrial base") &&
		!strings.Contains(text, "outage") &&
		!strings.Contains(text, "disrupt") &&
		!strings.Contains(text, "closure") {
		return true
	}
	return false
}

func looksLikeAdministrativeCommentaryText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	return hasAnyKeyword(text,
		"pricing deal", "pricing deals", "drug pricing", "access to medicines",
		"set to be announced", "new supreme leader", "tries to strike a balance", "strike a balance",
		"securing stability and growth", "stability and growth", "finding freedom and healing",
		"on the road with", "taunts", "order diplomats", "diplomats to leave",
		"offer refuge", "offers refuge", "policy to offer refuge", "calls trump s policy", "calls policy racist", "white afrikaners",
		"soft power", "rare instance of direct criticism", "direct criticism", "what is the game plan", "factsheet", "policy paper",
		"flag ceremony", "wrongful detainee day", "hostage and wrongful detainee day",
		"remarks by", "remarks to", "statement by", "press release", "commemoration",
		"anniversary event", "ceremony",
		"the d brief", "and a bit more", "rewild borders", "countries can rewild borders",
		"deter invasions", "environment chief", "natural border control",
		"what happens if", "must not be a staging ground", "to discuss regional security",
		"strategic agreement", "office attendance", "certification portfolio", "workforce readiness",
		"draw congressional scrutiny", "congressional scrutiny", "in other news", "blueprint", "review",
		"fuels debate", "debate in", "offensive strategy", "what would have happened if",
		"how long could the war last", "what happens if", "as it happened",
		"live updates", "latest updates", "live blog", "live politics", "politics latest updates",
	)
}

func looksLikeExplainerQuestionText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasDirectDisruptionTrigger(text) {
		return false
	}
	if hasAnyKeyword(text,
		"why does", "why do", "why did", "why is", "why are", "what is", "what are",
		"how does", "how do", "how did", "what it is and why it matters", "why it matters",
		"matter to the", "what to know", "at a glance", "explainer",
	) {
		return true
	}
	return false
}

func looksLikeLiveBlogPoliticalCommentaryText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text, "missile strike", "airstrike", "troops under fire", "port closure", "blackout", "earthquake", "flood", "wildfire") {
		return false
	}
	return hasAnyKeyword(text,
		"live updates", "latest updates", "live blog", "as it happened",
		"white house", "us politics", "politics latest updates", "office attendance", "policy paper",
	)
}

func looksLikeRetrospectiveConflictNarrativeText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text, "explosion", "airstrike", "missile strike", "drone strike", "blackout", "port closure", "operations suspended") {
		return false
	}
	return hasAnyKeyword(text,
		"final voyage", "legacy of", "what happened after", "the demise of", "the story of",
		"the warship sunk by the us", "sunk by the us", "retrospective", "lessons from", "looking back",
	)
}

func looksLikeLegalCommercialDisputeText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text, "explosion", "airstrike", "missile strike", "drone strike", "outage", "closure") {
		return false
	}
	return hasAnyKeyword(text,
		"ready to fight", "sale proceeds", "over proceeds", "sale dispute", "court battle", "lawsuit",
		"sanctions imposed on", "ownership transfer", "asset freeze", "club sale", "commercial dispute",
	)
}

func looksLikeSportsPoliticalHumanInterestText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text, "explosion", "airstrike", "missile strike", "drone strike", "outage", "closure") {
		return false
	}
	return hasAnyKeyword(text,
		"footballers", "safe house", "asian cup protest", "athletes", "sports protest", "players",
	)
}

func looksLikeChokepointSecurityMissionWithoutOperationalDisruption(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text,
		"outage", "shutdown", "closure", "closed", "closed to shipping", "operations suspended",
		"terminal closed", "port closed", "pipeline rupture", "pipeline closed", "service disruption",
		"service outage", "blackout", "collision", "attack on tanker", "missile strike", "airstrike",
		"explosion", "fire at", "sank", "sunk", "damaged", "damaging shipping",
	) {
		return false
	}
	return hasAnyKeyword(text,
		"reopen strait", "reopen the strait", "defensive mission", "escort mission", "secure shipping route",
		"protect shipping", "shipping protection", "allies preparing", "preparing mission", "maritime security mission",
	)
}

func looksLikeCapabilityOrFeatureText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text,
		"attack", "strike", "missile strike", "airstrike", "shelling", "clash", "troops under fire", "warship hit",
		"killed", "injured", "hostage", "outage", "shutdown", "blackout", "closure", "explosion", "breach",
	) {
		return false
	}
	return hasAnyKeyword(text,
		"meet the startups", "military specific ai", "military-specific ai", "soft power", "investment deals",
		"strategic agreement", "draw congressional scrutiny", "congressional scrutiny", "missile capability",
		"anti ship missile capability", "anti-ship missile capability", "to develop", "review", "blueprint",
		"seeks order suppressing identities", "office attendance", "what happens if", "why hedging is no longer enough",
		"must not be a staging ground", "to discuss regional security", "calls on europe", "unlikely to continue",
	)
}

func looksLikeCivilIncidentWithoutMilitaryContext(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text,
		"missile", "airstrike", "drone strike", "shelling", "troops", "warship",
		"frigate", "destroyer", "fighter jet", "interceptor", "frontline", "hostilities",
		"ground offensive", "counterstrike", "invasion", "ceasefire",
	) {
		return false
	}
	return hasAnyKeyword(text,
		"police investigate", "nightclub", "outside the us embassy", "outside u s embassy",
		"embassy", "injures", "including minors", "minors", "synagogue", "mosque", "church",
		"community center", "festival", "market square", "restaurant", "school", "civilian",
		"bombing", "blast", "explosion",
	)
}

func looksLikeDrugInterdictionText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text,
		"missile", "airstrike", "drone strike", "shelling", "warship", "fighter jet", "interceptor",
		"ground offensive", "frontline", "invasion", "ceasefire", "troops under fire",
	) {
		return false
	}
	return hasAnyKeyword(text,
		"drug boat", "drug ops", "counter narcotics", "counter-narcotics", "narcotics",
		"cartel", "smuggling boat", "suspected drug boat", "drug interdiction",
	)
}

func looksLikeExerciseWithoutIncidentText(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if !hasAnyKeyword(text, "exercise", "drill", "rehearsal", "rehearse", "war game", "training mission", "training exercise", "pre deployment exercises", "pre-deployment exercises") {
		return false
	}
	if hasAnyKeyword(text, "hold maritime attack drills", "held maritime attack drills", "rehearse anti ship missions", "rehearse anti-ship missions", "wraps pre deployment exercises", "wraps pre-deployment exercises") {
		return true
	}
	if hasAnyKeyword(text,
		"strike", "missile", "airstrike", "shelling", "drone strike", "troops under fire",
		"clash", "collision", "shot down", "intercepted", "interception", "hostage", "terror attack",
	) {
		return false
	}
	return true
}

func looksLikeForcePostureDeploymentText(text string, cls horizonMonitorNewsClassification) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text,
		"missile strike", "airstrike", "shelling", "drone strike", "troops under fire",
		"hit on residential", "killed", "injured", "hostage", "explosion", "port closure", "blackout",
	) {
		return false
	}
	if !(strings.EqualFold(strings.TrimSpace(cls.Category), "military") || strings.EqualFold(strings.TrimSpace(cls.Category), "conflict") || hasAnyKeyword(text, "fighter", "frigate", "carrier", "air defence", "air defense", "anti-drone", "anti missile", "anti-missile")) {
		return false
	}
	return hasAnyKeyword(text,
		"deploy", "deployed", "deployment", "sends", "sent", "dispatch", "dispatched",
		"operate from", "enhancing security", "additional measures may be taken",
		"bolster", "reinforcement", "air defense missions", "combat air patrol",
	)
}

func looksLikeMultiCountryDeploymentRoundup(text string, cls horizonMonitorNewsClassification) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(cls.Category), "military") || strings.EqualFold(strings.TrimSpace(cls.Category), "conflict") {
		if hasAnyKeyword(text, "attack", "airstrike", "missile strike", "shelling", "troops under fire", "drone strike", "hostage", "explosion", "outage", "closure") {
			return false
		}
		if len(extractCountryCodesFromText(text, 4)) >= 2 &&
			hasAnyKeyword(text,
				"sends", "deploys", "increase deployments", "naval and air deployments",
				"regional security", "amid", "threats", "war spreads", "increase middle east naval", "air deployments",
			) {
			return true
		}
	}
	return false
}

func inferCountryCodeFromAffectedZoneText(text, existingCountry string) string {
	text = normalizeKeywordText(text)
	if text == "" {
		return ""
	}
	codes := extractCountryCodesFromText(text, 4)
	if len(codes) == 0 {
		return ""
	}
	existingCountry = strings.ToUpper(strings.TrimSpace(existingCountry))
	for _, code := range codes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		if existingCountry != "" && code == existingCountry {
			return code
		}
	}
	if len(codes) == 1 {
		return strings.ToUpper(strings.TrimSpace(codes[0]))
	}
	return ""
}

func looksLikeCyberCampaignWithoutOperationalImpact(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text, "outage", "shutdown", "offline", "blackout", "service disruption", "service outage", "operations suspended", "payment outage", "port closure") {
		return false
	}
	return hasAnyKeyword(text,
		"malware chains", "dual malware chains", "targets governments", "targets government entities",
		"phishing campaign", "espionage campaign", "credential theft", "credential harvesting",
		"uses malware", "backdoor", "rat", "remote access trojan", "campaign targets",
	)
}

func looksLikeCompanyScopedCyberDataBreachWithoutOutage(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if hasAnyKeyword(text,
		"outage", "service outage", "service disruption", "shutdown", "offline", "blackout",
		"operations suspended", "payment outage", "port closure", "terminal closed", "pipeline closed",
	) {
		return false
	}
	if !hasAnyKeyword(text, "data breach", "breach", "service provider hack", "hacked service provider", "hack") {
		return false
	}
	return hasAnyKeyword(text,
		"employee data", "customer data", "employees and customers", "subsidiary",
		"service provider", "discloses data breach", "stolen data",
		"surveillance and wiretap systems", "wiretap systems", "surveillance systems",
		"fbi investigates breach", "investigates breach",
	)
}

func looksLikeOffshoreShippingIncidentAttributedElsewhere(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	if !hasAnyKeyword(text, "tanker", "gas tanker", "lng carrier", "cargo ship", "bulk carrier", "vessel", "carrier") {
		return false
	}
	if !hasAnyKeyword(text, "off ", "off the coast of", "mediterranean sea off", "near the coast of") {
		return false
	}
	if !hasAnyKeyword(text, "accuses", "blames", "described as a terrorist attack", "terrorist attack") {
		return false
	}
	if hasAnyKeyword(text, "libyan forces", "libya navy", "libyan navy", "libya militia", "libyan militia", "tripoli", "benghazi") {
		return false
	}
	return true
}

func shouldSuppressAffectedZoneByEventProfile(
	text string,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	conditionType string,
	profile wmArticleProfile,
) bool {
	if looksLikeConcreteOperationalIncident(text, cls) {
		return false
	}
	host := affectedZoneSourceHost(e)
	eventScore := wmHeuristics().strongEventScore(text)
	if profile.likelyAnalysisRoundup() && !looksLikeWarmConflictPosture(text, cls) {
		return true
	}
	if looksLikeCapabilityOrFeatureText(text) && !looksLikeWarmConflictPosture(text, cls) {
		return true
	}
	if looksLikeExerciseWithoutIncidentText(text) {
		return true
	}
	if looksLikeForcePostureDeploymentText(text, cls) && !looksLikeWarmConflictPosture(text, cls) {
		return true
	}
	if looksLikeDrugInterdictionText(text) {
		return true
	}
	if looksLikeLiveBlogPoliticalCommentaryText(text) {
		return true
	}
	if (conditionType == "cyber_disruption" || conditionType == "infrastructure_disruption") && profile.likelyAdvisoryCampaignWithoutOutage() {
		return true
	}
	if conditionType == "infrastructure_disruption" && looksLikeChokepointSecurityMissionWithoutOperationalDisruption(text) {
		return true
	}
	if conditionType == "cyber_disruption" && looksLikeCyberCampaignWithoutOperationalImpact(text) {
		return true
	}
	if (conditionType == "cyber_disruption" || conditionType == "infrastructure_disruption") && looksLikeCompanyScopedCyberDataBreachWithoutOutage(text) {
		return true
	}
	if conditionType == "military_conflict" && looksLikeOffshoreShippingIncidentAttributedElsewhere(text) {
		return true
	}
	if profile.likelyBroadRegionalRoundup() {
		switch conditionType {
		case "wildfire_zone", "humanitarian_crisis", "environmental_hazard", "political_tension":
			return true
		case "military_conflict":
			if !looksLikeWarmConflictPosture(text, cls) && eventScore < 1.95 {
				return true
			}
		}
	}
	if (host == "csis.org" || host == "worldbank.org" || host == "blogs.worldbank.org") && profile.AnalysisScore >= 1.35 && eventScore < 1.85 {
		return true
	}
	if host == "reliefweb.int" && profile.BroadScopeScore >= 1.35 && eventScore < 1.9 {
		return true
	}
	return false
}

func affectedZoneFreshnessLimit(conditionType string) time.Duration {
	switch strings.TrimSpace(conditionType) {
	case "earthquake_affected", "tsunami_affected", "volcanic_eruption", "flood_zone", "wildfire_zone", "environmental_hazard":
		return 14 * 24 * time.Hour
	case "cyber_disruption", "infrastructure_disruption":
		return 21 * 24 * time.Hour
	case "disease_outbreak", "humanitarian_crisis":
		return 30 * 24 * time.Hour
	case "military_conflict", "political_tension":
		return 21 * 24 * time.Hour
	default:
		return 21 * 24 * time.Hour
	}
}

func shouldSkipAffectedZoneAsStale(now, matchedAt time.Time, e *horizonMonitorFeedEvent, c *affectedZoneCandidate) bool {
	if c == nil || matchedAt.IsZero() {
		return false
	}
	limit := affectedZoneFreshnessLimit(c.ConditionType)
	if limit <= 0 {
		return false
	}
	if now.Sub(matchedAt) <= limit {
		return false
	}
	host := affectedZoneSourceHost(e)
	if host == "earthquake.usgs.gov" || host == "cisa.gov" || host == "thehackernews.com" || host == "news.google.com" || host == "bbc.com" || host == "nytimes.com" {
		return now.Sub(matchedAt) > (limit * 2)
	}
	return true
}

func shouldSuppressGenericAffectedZone(geoToken, place, pathSlug, conditionType string) bool {
	geoToken = strings.TrimSpace(strings.ToLower(geoToken))
	pathSlug = strings.TrimSpace(strings.ToLower(pathSlug))
	place = normalizePhrase(place)
	if geoToken != "global" {
		return strings.Contains(pathSlug, "domain-v1") ||
			strings.Contains(pathSlug, "supply-chain") ||
			strings.Contains(pathSlug, "last-24-hours") ||
			strings.Contains(pathSlug, "but-warn")
	}
	if strings.TrimSpace(place) == "" || pathSlug == "" || pathSlug == "global" || pathSlug == "emerging-hotspot" {
		return true
	}
	if !isLegitResidualGlobalAffectedZonePlace(place) {
		return true
	}
	if strings.Contains(place, "global") ||
		strings.Contains(pathSlug, "domain-v1") ||
		strings.Contains(pathSlug, "supply-chain") ||
		looksLikeBadGlobalAffectedZoneSlug("/zone/global/"+strings.TrimSpace(conditionType)+"/"+pathSlug) {
		return true
	}
	return false
}

func shouldSuppressAffectedZoneByPlaceQuality(place, countryName, conditionType, text string, e *horizonMonitorFeedEvent) bool {
	place = normalizePhrase(place)
	countryName = normalizePhrase(countryName)
	text = normalizeKeywordText(text)
	if place == "" {
		return true
	}
	if place == countryName {
		return false
	}
	if looksLikeBadAffectedZonePlaceHint(place) {
		return true
	}
	if first := firstToken(place); first != "" && (isNumericToken(first) || first == "ongoing" || first == "last") {
		return true
	}
	switch strings.TrimSpace(conditionType) {
	case "cyber_disruption", "infrastructure_disruption":
		if hasAnyKeyword(place, "botnet", "campaign", "web shell", "countries", "country", "nations") {
			return true
		}
	}
	host := affectedZoneSourceHost(e)
	if (host == "thehackernews.com" || host == "krebsonsecurity.com") &&
		!hasAnyKeyword(text, "outage", "shutdown", "closure", "blackout", "disruption", "service down") &&
		hasAnyKeyword(place, "botnet", "campaign", "web shell", "countries", "country", "nations") {
		return true
	}
	return false
}

func looksLikeBadAffectedZonePlaceHint(place string) bool {
	place = normalizePhrase(place)
	if place == "" {
		return true
	}
	return strings.Contains(place, "last 24 hours") ||
		strings.Contains(place, "this week") ||
		strings.Contains(place, "priority area") ||
		strings.Contains(place, "and beyond") ||
		strings.Contains(place, "across latin america") ||
		place == "middle east" ||
		strings.Contains(place, "military activity") ||
		strings.Contains(place, "ongoing") ||
		strings.Contains(place, "web shell") ||
		strings.Contains(place, "botnet") ||
		strings.Contains(place, "campaign") ||
		strings.Contains(place, "officials say") ||
		strings.Contains(place, "as presidents") ||
		strings.Contains(place, "16 countries") ||
		strings.Contains(place, "nations only") ||
		strings.Contains(place, "only deepwater") ||
		strings.Contains(place, "deepwater vlcc") ||
		strings.Contains(place, "but warn")
}

func firstToken(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func isNumericToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func placeSpecificScopeLooksUnreliable(place, countryName, geoPrecision string, hasCoord bool) bool {
	place = normalizePhrase(place)
	countryName = normalizePhrase(countryName)
	if place == "" || place == countryName {
		return false
	}
	if geoPrecision != "approximate_country" || !hasCoord {
		return false
	}
	if strings.Contains(place, " sea") || strings.Contains(place, "strait") || strings.Contains(place, "canal") || strings.Contains(place, "gulf") {
		return false
	}
	return true
}

func shouldSuppressAffectedZoneByGeoCoherence(place, geoToken, countryCode, geoPrecision string, e *horizonMonitorFeedEvent) bool {
	place = normalizePhrase(place)
	geoToken = strings.TrimSpace(strings.ToLower(geoToken))
	countryCode = strings.TrimSpace(strings.ToUpper(countryCode))
	if place == "" {
		return false
	}
	eventText := ""
	if e != nil {
		eventText = strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")
	}
	if chokepoint := normalizePhrase(extractKnownMaritimeChokepointFromText(eventText)); chokepoint != "" {
		if place != chokepoint {
			return true
		}
		if geoToken != "sea" {
			return true
		}
	}
	if cc := inferCountryCodeFromText(place); cc != "" && cc != countryCode && countryCode != "" {
		return true
	}
	if geoPrecision == "approximate_country" && place != "" && countryCode == "" && geoToken != "sea" {
		return true
	}
	if e != nil && strings.TrimSpace(e.LocationPath) != "" {
		lpCC := strings.ToUpper(strings.TrimSpace(countryFromLocationPathString(e.LocationPath)))
		if lpCC != "" && countryCode != "" && lpCC != countryCode && inferCountryCodeFromText(place) == countryCode {
			return true
		}
	}
	return false
}

func inferZoneGeoFromPlace(place string) (geoToken, countryCode, countryName string) {
	p := normalizePhrase(place)
	if p == "" {
		return "", "", ""
	}
	if pt, ok := zoneHotspotCoords[p]; ok {
		if cc := strings.ToUpper(strings.TrimSpace(pt.CountryCode)); cc != "" {
			return strings.ToLower(cc), cc, countryNameFromCode(cc)
		}
		if strings.Contains(p, "strait") || strings.Contains(p, "sea") || strings.Contains(p, "canal") || strings.Contains(p, "shoal") || strings.Contains(p, "channel") || strings.Contains(p, "gulf") || strings.Contains(p, "mandeb") {
			switch p {
			case "taiwan strait":
				return "tw-cn", "", "Taiwan Strait"
			default:
				return "sea", "", strings.TrimSpace(place)
			}
		}
	}
	if cc := inferCountryCodeFromText(place); cc != "" {
		return strings.ToLower(cc), cc, countryNameFromCode(cc)
	}
	return "", "", ""
}

func normalizeAffectedZonePlace(place, countryName string) string {
	place = strings.TrimSpace(place)
	if place == "" {
		return strings.TrimSpace(countryName)
	}
	normalized := normalizeKeywordText(place)
	if strings.Contains(normalized, "domain v1") ||
		strings.Contains(normalized, "supply chain") ||
		strings.Contains(normalized, "affected zone") ||
		strings.Contains(normalized, "canonical path") {
		return strings.TrimSpace(countryName)
	}
	if cc := inferCountryCodeFromText(place); cc != "" {
		return countryNameFromCode(cc)
	}
	for _, suffix := range []string{" this week", " this month", " today", " yesterday"} {
		if strings.HasSuffix(strings.ToLower(place), suffix) {
			trimmed := strings.TrimSpace(place[:len(place)-len(suffix)])
			if trimmed != "" {
				place = trimmed
			}
			break
		}
	}
	return place
}

func shouldRelaxAffectedZoneAnchorCountryHint(place, countryName, countryCode string) bool {
	if isWeakAffectedZonePlace(place, countryName) {
		return true
	}
	placeNorm := normalizePhrase(place)
	if placeNorm == "" {
		return true
	}
	if looksLikeBadAffectedZonePlaceHint(placeNorm) {
		return true
	}
	if strings.TrimSpace(countryCode) == "" {
		return true
	}
	return false
}

func truncateString(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max])
}

type affectedZoneEntityAnchor struct {
	Place          string
	GeoToken       string
	CountryCode    string
	CountryName    string
	Lat            float64
	Lon            float64
	HasCoordinates bool
	GeoPrecision   string
	FragmentPath   string
}

func resolveAffectedZoneEntityAnchor(
	ctx context.Context,
	p *HorizonMonitorPipeline,
	e *horizonMonitorFeedEvent,
	place string,
	countryCode string,
) *affectedZoneEntityAnchor {
	if p == nil || p.fragmentsRepo == nil || e == nil {
		return nil
	}
	anchors := extractAffectedZoneObjectAnchors(e, 4)
	if place = strings.TrimSpace(place); place != "" && isPlausiblePlaceName(place) {
		anchors = uniqueStrings(append([]string{place}, anchors...))
	}
	if len(anchors) == 0 {
		return nil
	}
	wantCountry := strings.ToUpper(strings.TrimSpace(firstNonEmptyString(countryCode, e.CountryCode)))
	type scoredAnchor struct {
		anchor affectedZoneEntityAnchor
		score  float64
	}
	best := scoredAnchor{}
	for _, seed := range anchors {
		for _, frag := range exactAffectedZoneAnchorFragments(ctx, p, seed) {
			candidate, candScore := buildAffectedZoneAnchorFromFragment(ctx, seed, wantCountry, e, frag, 0.96, p)
			if candScore <= best.score {
				continue
			}
			best = scoredAnchor{anchor: candidate, score: candScore}
		}
		if p.vertexSearch == nil || !p.vertexSearch.IsConfigured() {
			continue
		}
		resp, err := p.vertexSearch.Search(ctx, &ai.SearchRequest{Query: seed, PageSize: 4})
		if err != nil || resp == nil || len(resp.Results) == 0 {
			continue
		}
		for _, result := range resp.Results {
			score := result.RelevanceScore
			if score == 0 && result.RankSignals != nil {
				score = result.RankSignals.SemanticSimilarityScore
			}
			if score < 0.70 {
				continue
			}
			fragID := extractFragmentIDFromGCSURI(result.Document.URI)
			if fragID == "" {
				fragID = strings.TrimSuffix(strings.TrimSpace(result.Document.ID), ".json")
			}
			if fragID == "" {
				continue
			}
			frag, ferr := p.fragmentsRepo.Get(ctx, fragID)
			if ferr != nil || frag == nil {
				continue
			}
			candidate, candScore := buildAffectedZoneAnchorFromFragment(ctx, seed, wantCountry, e, frag, score, p)
			if candScore <= best.score {
				continue
			}
			best = scoredAnchor{anchor: candidate, score: candScore}
		}
	}
	if best.score < 0.92 || !best.anchor.HasCoordinates {
		return nil
	}
	return &best.anchor
}

func exactAffectedZoneAnchorFragments(
	ctx context.Context,
	p *HorizonMonitorPipeline,
	seed string,
) []*horizonmodels.HorizonFragment {
	if p == nil || p.fragmentsRepo == nil {
		return nil
	}
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return nil
	}
	out := make([]*horizonmodels.HorizonFragment, 0, 6)
	seen := make(map[string]struct{}, 6)
	for _, fragmentType := range []horizonmodels.FragmentType{
		horizonmodels.FragmentTypeFacility,
		horizonmodels.FragmentTypeDeposit,
		horizonmodels.FragmentTypeGeoRegion,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeProduct,
	} {
		queries := make([][]*horizonmodels.HorizonFragment, 0, 3)
		if fragments, err := p.fragmentsRepo.FindByLabel(ctx, "en", seed, fragmentType); err == nil {
			queries = append(queries, fragments)
		}
		switch fragmentType {
		case horizonmodels.FragmentTypeFacility, horizonmodels.FragmentTypeDeposit, horizonmodels.FragmentTypeOrganization:
			if fragments, err := p.fragmentsRepo.FindByIdentifierAndType(ctx, "shortName", seed, fragmentType); err == nil {
				queries = append(queries, fragments)
			}
			if fragments, err := p.fragmentsRepo.FindByIdentifierAndType(ctx, "legalName", seed, fragmentType); err == nil {
				queries = append(queries, fragments)
			}
		}
		for _, fragments := range queries {
			for _, frag := range fragments {
				if frag == nil || strings.TrimSpace(frag.ID) == "" {
					continue
				}
				if _, ok := seen[strings.TrimSpace(frag.ID)]; ok {
					continue
				}
				seen[strings.TrimSpace(frag.ID)] = struct{}{}
				out = append(out, frag)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftScore := affectedZoneExactAnchorFragmentScore(seed, out[i])
		rightScore := affectedZoneExactAnchorFragmentScore(seed, out[j])
		if leftScore == rightScore {
			return strings.TrimSpace(out[i].CanonicalPath) < strings.TrimSpace(out[j].CanonicalPath)
		}
		return leftScore > rightScore
	})
	return out
}

func affectedZoneExactAnchorFragmentScore(seed string, frag *horizonmodels.HorizonFragment) float64 {
	if frag == nil {
		return 0
	}
	score := 0.0
	switch frag.Type {
	case horizonmodels.FragmentTypeFacility:
		score += 0.36
	case horizonmodels.FragmentTypeDeposit:
		score += 0.32
	case horizonmodels.FragmentTypeGeoRegion:
		score += 0.24
	case horizonmodels.FragmentTypeOrganization:
		score += 0.12
	case horizonmodels.FragmentTypeProduct:
		score += 0.14
	default:
		return 0
	}
	label := strings.TrimSpace(frag.Labels["en"])
	if label == "" {
		label = humanizePathTail(frag.CanonicalPath)
	}
	seedNorm := normalizePhrase(seed)
	labelNorm := normalizePhrase(label)
	switch {
	case seedNorm != "" && labelNorm == seedNorm:
		score += 0.10
	case seedNorm != "" && strings.Contains(labelNorm, seedNorm):
		score += 0.18
	case seedNorm != "" && strings.Contains(seedNorm, labelNorm):
		score += 0.04
	}
	if looksLikeOperationalAnchorPath(strings.Join([]string{label, strings.TrimSpace(frag.CanonicalPath)}, " ")) {
		score += 0.22
	}
	if _, _, hasCoord := horizonrepo.ResolveFragmentCoordinates(frag); hasCoord {
		score += 0.08
	}
	if strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/na/") {
		score -= 0.02
	}
	return score
}

func affectedZoneLinkedFragmentScore(
	ctx context.Context,
	seed string,
	wantCountry string,
	e *horizonMonitorFeedEvent,
	frag *horizonmodels.HorizonFragment,
	baseScore float64,
	p *HorizonMonitorPipeline,
) float64 {
	if frag == nil {
		return 0
	}
	score := baseScore
	switch frag.Type {
	case horizonmodels.FragmentTypeFacility:
		score += 0.28
	case horizonmodels.FragmentTypeDeposit:
		score += 0.26
	case horizonmodels.FragmentTypeOrganization:
		score += 0.24
	case horizonmodels.FragmentTypeProduct:
		score += 0.22
	case horizonmodels.FragmentTypeGeoRegion:
		score += 0.16
	default:
		return 0
	}
	score += affectedZoneExactAnchorFragmentScore(seed, frag) * 0.22

	label := strings.TrimSpace(frag.Labels["en"])
	if label == "" {
		label = humanizePathTail(frag.CanonicalPath)
	}
	text := normalizeKeywordText(strings.Join([]string{
		seed,
		label,
		strings.TrimSpace(frag.CanonicalPath),
		func() string {
			if e == nil {
				return ""
			}
			return strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")
		}(),
	}, " "))
	seedNorm := normalizePhrase(seed)
	labelNorm := normalizePhrase(label)
	if seedNorm != "" && labelNorm == seedNorm {
		score += 0.10
	} else if seedNorm != "" && (strings.Contains(labelNorm, seedNorm) || strings.Contains(seedNorm, labelNorm)) {
		score += 0.06
	}
	if looksLikeOperationalAnchorPath(text) {
		score += 0.08
	}
	energyContext := hasAnyKeyword(text, "lng", "helium", "natural gas", "gas", "industrial city", "energy infrastructure", "processing complex", "industrial gas")
	maritimeContext := hasAnyKeyword(text, "shipping", "maritime", "container", "cargo", "dockworker", "dockworkers", "port", "terminal")
	pathText := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(frag.CanonicalPath), label}, " "))
	if energyContext {
		if hasAnyKeyword(pathText, "qatarenergy", "industrial city", "industrial-city", "lng", "gas", "helium", "processing", "complex") {
			score += 0.18
		}
		if hasAnyKeyword(pathText, "/fac/port/", " port") && !maritimeContext {
			score -= 0.12
		}
	}
	if maritimeContext {
		if hasAnyKeyword(pathText, "/fac/port/", " port", "terminal", "maritime", "shipping") {
			score += 0.12
		}
	}
	if cc := affectedZoneCountryFromFragment(frag); wantCountry != "" && cc != "" && strings.EqualFold(cc, wantCountry) {
		score += 0.08
	}
	if frag.Type == horizonmodels.FragmentTypeProduct && hasAnyKeyword(text, "lng", "helium", "natural gas", "gas", "oil", "crude", "shipping", "freight", "container") {
		score += 0.08
	}
	if frag.Type == horizonmodels.FragmentTypeOrganization && p != nil {
		if fallback, fallbackScore := resolveAffectedZoneOperationalAnchorFromOrg(ctx, p, frag, e, wantCountry); fallbackScore > 0 && strings.TrimSpace(fallback.FragmentPath) != "" {
			score += 0.04
		}
	}
	return score
}

func buildAffectedZoneAnchorFromFragment(
	ctx context.Context,
	seed string,
	wantCountry string,
	e *horizonMonitorFeedEvent,
	frag *horizonmodels.HorizonFragment,
	baseScore float64,
	p *HorizonMonitorPipeline,
) (affectedZoneEntityAnchor, float64) {
	if frag == nil {
		return affectedZoneEntityAnchor{}, 0
	}
	placeholderFacility := strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/na/")
	if placeholderFacility && shouldRejectAffectedZonePlaceholderFacilityAnchor(seed, e, frag) {
		return affectedZoneEntityAnchor{}, 0
	}
	lat, lon, hasCoord := horizonrepo.ResolveFragmentCoordinates(frag)
	if !hasCoord {
		return affectedZoneEntityAnchor{}, 0
	}
	cc := affectedZoneCountryFromFragment(frag)
	if cc == "" && placeholderFacility && wantCountry != "" {
		cc = strings.ToUpper(strings.TrimSpace(wantCountry))
	}
	if wantCountry != "" && cc != "" && !strings.EqualFold(cc, wantCountry) {
		return affectedZoneEntityAnchor{}, 0
	}
	label := strings.TrimSpace(frag.Labels["en"])
	if label == "" {
		label = humanizePathTail(frag.CanonicalPath)
	}
	score := baseScore
	switch frag.Type {
	case horizonmodels.FragmentTypeFacility:
		score += 0.30
	case horizonmodels.FragmentTypeDeposit:
		score += 0.28
	case horizonmodels.FragmentTypeGeoRegion:
		score += 0.24
	case horizonmodels.FragmentTypeOrganization:
		score += 0.08
	default:
		return affectedZoneEntityAnchor{}, 0
	}
	if hasAnyKeyword(normalizeKeywordText(strings.Join([]string{e.Title, e.Summary}, " ")), normalizeKeywordText(label)) {
		score += 0.08
	}
	if strings.EqualFold(strings.TrimSpace(seed), label) {
		score += 0.06
	}
	if looksLikeOperationalAnchorPath(strings.Join([]string{label, strings.TrimSpace(frag.CanonicalPath)}, " ")) {
		score += 0.08
	}
	if placeholderFacility {
		score -= 0.02
	}
	if cc != "" && wantCountry != "" && strings.EqualFold(cc, wantCountry) {
		score += 0.08
	}
	score += affectedZoneExactAnchorFragmentScore(seed, frag) * 0.30
	if frag.Type == horizonmodels.FragmentTypeOrganization {
		if fallback, fallbackScore := resolveAffectedZoneOperationalAnchorFromOrg(ctx, p, frag, e, wantCountry); fallbackScore > 0 {
			return fallback, score + fallbackScore
		}
	}
	geoToken := strings.ToLower(strings.TrimSpace(cc))
	if geoToken == "" {
		if strings.Contains(strings.ToLower(frag.CanonicalPath), "/strait/") ||
			strings.Contains(strings.ToLower(frag.CanonicalPath), "strait") ||
			strings.Contains(strings.ToLower(frag.CanonicalPath), "canal") ||
			strings.Contains(strings.ToLower(frag.CanonicalPath), "sea") ||
			strings.Contains(strings.ToLower(frag.CanonicalPath), "gulf") {
			geoToken = "sea"
		}
	}
	if geoToken == "" {
		geoToken = "global"
	}
	return affectedZoneEntityAnchor{
		Place:          label,
		GeoToken:       geoToken,
		CountryCode:    cc,
		CountryName:    countryNameFromCode(cc),
		Lat:            lat,
		Lon:            lon,
		HasCoordinates: true,
		GeoPrecision:   "entity_anchor",
		FragmentPath:   strings.TrimSpace(frag.CanonicalPath),
	}, score
}

func shouldRejectAffectedZonePlaceholderFacilityAnchor(seed string, e *horizonMonitorFeedEvent, frag *horizonmodels.HorizonFragment) bool {
	if frag == nil || !strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/na/") {
		return false
	}
	if _, _, hasCoord := horizonrepo.ResolveFragmentCoordinates(frag); !hasCoord {
		return true
	}
	label := strings.TrimSpace(frag.Labels["en"])
	if label == "" {
		label = humanizePathTail(frag.CanonicalPath)
	}
	seedNorm := normalizePhrase(seed)
	labelNorm := normalizePhrase(label)
	textNorm := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(seed),
		strings.TrimSpace(label),
		strings.TrimSpace(frag.CanonicalPath),
	}, " "))
	if e != nil {
		textNorm = normalizeKeywordText(strings.Join([]string{
			textNorm,
			strings.TrimSpace(e.Title),
			strings.TrimSpace(e.Summary),
		}, " "))
	}
	if seedNorm != "" && (labelNorm == seedNorm || strings.Contains(labelNorm, seedNorm) || strings.Contains(seedNorm, labelNorm)) {
		return false
	}
	if looksLikeOperationalAnchorPath(textNorm) {
		return false
	}
	return true
}

func resolveAffectedZoneOperationalAnchorFromOrg(
	ctx context.Context,
	p *HorizonMonitorPipeline,
	org *horizonmodels.HorizonFragment,
	e *horizonMonitorFeedEvent,
	wantCountry string,
) (affectedZoneEntityAnchor, float64) {
	if p == nil || p.edgesRepo == nil || p.fragmentsRepo == nil || org == nil || strings.TrimSpace(org.ID) == "" {
		return affectedZoneEntityAnchor{}, 0
	}
	edgeTypes := []horizonmodels.DependencyType{
		horizonmodels.DependencyTypeOwns,
		horizonmodels.DependencyTypeManages,
		horizonmodels.DependencyTypeControls,
	}
	text := normalizeKeywordText(strings.Join([]string{e.Title, e.Summary}, " "))
	best := affectedZoneEntityAnchor{}
	bestScore := 0.0
	for _, et := range edgeTypes {
		edges, err := p.edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(org.ID), &et, 64)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge == nil || strings.TrimSpace(edge.TargetFragmentID) == "" {
				continue
			}
			frag, ferr := p.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID))
			if ferr != nil || frag == nil {
				continue
			}
			if frag.Type != horizonmodels.FragmentTypeFacility && frag.Type != horizonmodels.FragmentTypeDeposit && frag.Type != horizonmodels.FragmentTypeGeoRegion {
				continue
			}
			cc := affectedZoneCountryFromFragment(frag)
			if wantCountry != "" && cc != "" && !strings.EqualFold(cc, wantCountry) {
				continue
			}
			lat, lon, hasCoord := horizonrepo.ResolveFragmentCoordinates(frag)
			if !hasCoord {
				continue
			}
			label := strings.TrimSpace(frag.Labels["en"])
			if label == "" {
				label = humanizePathTail(frag.CanonicalPath)
			}
			score := 0.22
			if strings.Contains(text, normalizeKeywordText(label)) {
				score += 0.22
			}
			if cc != "" && wantCountry != "" && strings.EqualFold(cc, wantCountry) {
				score += 0.18
			}
			if looksLikeOperationalAnchorPath(frag.CanonicalPath) {
				score += 0.12
			}
			if score <= bestScore {
				continue
			}
			bestScore = score
			best = affectedZoneEntityAnchor{
				Place:          label,
				GeoToken:       strings.ToLower(firstNonEmptyString(cc, "global")),
				CountryCode:    cc,
				CountryName:    countryNameFromCode(cc),
				Lat:            lat,
				Lon:            lon,
				HasCoordinates: true,
				GeoPrecision:   "entity_anchor",
				FragmentPath:   strings.TrimSpace(frag.CanonicalPath),
			}
		}
	}
	return best, bestScore
}

func extractAffectedZoneObjectAnchors(e *horizonMonitorFeedEvent, maxCount int) []string {
	if e == nil || maxCount <= 0 {
		return nil
	}
	out := make([]string, 0, maxCount)
	seen := map[string]struct{}{}
	addRaw := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || len(out) >= maxCount {
			return
		}
		if len(strings.Fields(v)) > 5 || len(v) > 72 {
			return
		}
		if !looksLikeAffectedZoneEntitySeed(v, e) {
			return
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	add := func(v string) {
		if !looksLikeAffectedZoneEntitySeed(v, e) {
			return
		}
		addRaw(v)
	}
	for _, key := range []string{
		"company", "companyName", "organization", "organisation", "operator", "owner",
		"facility", "plant", "refinery", "port", "terminal", "airport", "field", "deposit",
		"asset", "site", "location",
	} {
		if e.Raw != nil {
			if v := strings.TrimSpace(asString(e.Raw[key])); v != "" {
				add(v)
			}
		}
	}
	countryName := normalizePhrase(firstNonEmptyString(strings.TrimSpace(e.CountryName), countryNameFromCode(strings.TrimSpace(e.CountryCode))))
	text := strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " ")
	if place := inferZonePlaceName(e, normalizeKeywordText(text), ""); place != "" {
		placeNorm := normalizePhrase(place)
		if placeNorm != "" && placeNorm != countryName && isPlausiblePlaceName(place) {
			addRaw(place)
		}
	}
	for _, seed := range heuristicSignalSeedNames(e) {
		seedNorm := normalizePhrase(seed)
		if seedNorm == "" || seedNorm == countryName || seedNorm == countryName+" government" {
			continue
		}
		add(seed)
	}
	for _, entity := range extractNLPEntityMaps(e.Raw["nlpEntities"]) {
		etype := normalizePhrase(asString(entity["entityType"]))
		role := normalizePhrase(asString(entity["role"]))
		if etype == "" {
			continue
		}
		if etype != "company" && etype != "facility" && etype != "port" && etype != "terminal" && etype != "pipeline" &&
			etype != "refinery" && etype != "fab" && etype != "chokepoint" && etype != "government body" && etype != "government_body" &&
			role != "actor" && role != "facility" && role != "target" && role != "route" && role != "chokepoint" {
			continue
		}
		add(firstNonEmptyString(asString(entity["normalized"]), asString(entity["surface"])))
	}
	return out
}

func extractNLPEntityMaps(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func shouldPreferAffectedZoneAnchor(anchor *affectedZoneEntityAnchor, place, countryName, geoPrecision string, hasCoord bool) bool {
	if anchor == nil || !anchor.HasCoordinates {
		return false
	}
	if !hasCoord || geoPrecision == "none" || geoPrecision == "approximate_country" {
		return true
	}
	return isWeakAffectedZonePlace(place, countryName)
}

func isWeakAffectedZonePlace(place, countryName string) bool {
	placeNorm := normalizePhrase(place)
	countryNorm := normalizePhrase(countryName)
	if placeNorm == "" || placeNorm == countryNorm {
		return true
	}
	switch placeNorm {
	case "global", "emerging hotspot", "emerging-hotspot":
		return true
	}
	return false
}

func looksLikeOperationalPlaceLabel(label string) bool {
	label = normalizePhrase(label)
	if label == "" {
		return false
	}
	return hasAnyKeyword(label, "port", "terminal", "refinery", "airport", "airbase", "island", "strait", "canal", "field", "complex", "embassy")
}

func shouldAttemptAffectedZoneEntityAnchor(place, countryName, geoPrecision string, hasCoord bool, e *horizonMonitorFeedEvent) bool {
	if e == nil {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
	operationalContext := looksLikeOperationalPlaceLabel(place) || hasAnyKeyword(text,
		"facility", "plant", "refinery", "port", "terminal",
		"airport", "factory", "fab", "field", "deposit", "pipeline", "industrial city",
		"complex", "site", "infrastructure", "energy infrastructure", "lng", "natural gas", "gas", "helium",
	)
	placeSpecific := !isWeakAffectedZonePlace(place, countryName) &&
		!placeSpecificScopeLooksUnreliable(place, countryName, geoPrecision, hasCoord) &&
		!looksLikeBadAffectedZonePlaceHint(place)
	if placeSpecific {
		geoToken, placeCountryCode, _ := inferZoneGeoFromPlace(place)
		if geoToken != "" && !operationalContext {
			return false
		}
		lpCountry := ""
		if e != nil {
			lpCountry = strings.TrimSpace(countryFromLocationPathString(e.LocationPath))
		}
		if lpCountry == "" &&
			normalizeCountryCode(e.CountryCode) == "" &&
			normalizeCountryCode(placeCountryCode) == "" &&
			!(operationalContext && hasCoord) {
			return false
		}
	}
	if !hasAnyKeyword(text,
		"company", "operator", "owner", "facility", "plant", "refinery", "port", "terminal",
		"airport", "factory", "fab", "field", "deposit", "pipeline", "mine", "industrial city",
		"complex", "site", "infrastructure", "energy infrastructure", "lng", "natural gas", "gas", "helium",
	) && len(extractAffectedZoneObjectAnchors(e, 3)) == 0 {
		return false
	}
	return true
}

func looksLikeAffectedZoneEntitySeed(seed string, e *horizonMonitorFeedEvent) bool {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return false
	}
	seedNorm := normalizePhrase(seed)
	if seedNorm == "" {
		return false
	}
	if looksLikeMilitaryAssetName(seedNorm) {
		return false
	}
	if inferCountryCodeFromText(seed) != "" {
		return false
	}
	if hasAnyKeyword(seedNorm,
		"government", "ministry", "official", "officials", "president", "prime minister", "army", "navy", "air force",
		"speech", "glance", "analysis", "report", "roundup", "strategy", "update",
		"research", "gender", "worker", "dead", "death", "arrest", "detention", "warning", "warnings",
	) && !hasAnyKeyword(seedNorm,
		"terminal", "port", "refinery", "plant", "pipeline", "airport", "factory", "fab", "field", "deposit", "complex", "site",
	) {
		return false
	}
	if len(strings.Fields(seedNorm)) == 1 && !hasAnyKeyword(seedNorm,
		"ras", "hook", "zeebrugge", "rotterdam", "malacca", "hormuz", "south", "north",
	) {
		return false
	}
	titleSummary := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary)}, " "))
	if !looksLikeOperationalEntitySeed(seedNorm) && !looksLikeCorporateEntitySeed(seedNorm) {
		return false
	}
	if titleSummary != "" && !hasAnyKeyword(seedNorm,
		"terminal", "port", "refinery", "plant", "pipeline", "airport", "factory", "fab", "field", "deposit", "complex", "city", "site",
	) && !strings.Contains(titleSummary, seedNorm) {
		return false
	}
	return true
}

func looksLikeOperationalEntitySeed(seedNorm string) bool {
	return hasAnyKeyword(seedNorm,
		"terminal", "port", "refinery", "plant", "pipeline", "airport", "factory", "fab",
		"field", "deposit", "complex", "industrial city", "site", "hub", "mine", "shipyard",
		"strait", "canal", "sea", "gulf",
	)
}

func looksLikeCorporateEntitySeed(seedNorm string) bool {
	return hasAnyKeyword(seedNorm,
		" plc", " ltd", " llc", " inc", " corp", " corporation", " company", " co ", " group",
		" holdings", " energy", " petroleum", " gas", " lng", " shipping", " logistics", " terminals",
	)
}

func looksLikeMilitaryAssetName(seedNorm string) bool {
	return hasAnyKeyword(seedNorm,
		"uss ", "hms ", "fs ", "iaf ", "raf ",
		"aircraft carrier", "carrier", "destroyer", "frigate", "cruiser", "warship",
		"task force", "fleet", "squadron",
	)
}

func affectedZoneCountryFromFragment(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if cc := strings.ToUpper(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1)); len(cc) == 2 {
			return cc
		}
	}
	cp := strings.Trim(strings.TrimSpace(fragment.CanonicalPath), "/")
	parts := strings.Split(cp, "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "fac", "org", "gov", "loc", "deposit", "market", "community":
		if cc := strings.ToUpper(strings.TrimSpace(parts[1])); len(cc) == 2 {
			if cc == "NA" && (parts[0] == "fac" || parts[0] == "deposit" || parts[0] == "org") {
				return ""
			}
			return cc
		}
	}
	return ""
}

func looksLikeOperationalAnchorPath(path string) bool {
	p := normalizeKeywordText(path)
	return strings.Contains(p, "port") ||
		strings.Contains(p, "terminal") ||
		strings.Contains(p, "refinery") ||
		strings.Contains(p, "plant") ||
		strings.Contains(p, "fab") ||
		strings.Contains(p, "field") ||
		strings.Contains(p, "complex") ||
		strings.Contains(p, "pipeline") ||
		strings.Contains(p, "industrial city") ||
		strings.Contains(p, "industrial complex") ||
		strings.Contains(p, "gas processing") ||
		strings.Contains(p, "lng")
}

func humanizePathTail(path string) string {
	cp := strings.Trim(strings.TrimSpace(path), "/")
	if cp == "" {
		return ""
	}
	parts := strings.Split(cp, "/")
	if len(parts) == 0 {
		return ""
	}
	tail := strings.TrimSpace(parts[len(parts)-1])
	if tail == "" {
		return ""
	}
	tail = strings.ReplaceAll(tail, "-", " ")
	tail = strings.ReplaceAll(tail, "_", " ")
	return strings.Title(tail)
}

func extractFragmentIDFromGCSURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	parts := strings.Split(uri, "/")
	if len(parts) == 0 {
		return ""
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	return strings.TrimSuffix(last, ".json")
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
	"donetsk":                     {Lat: 48.0159, Lon: 37.8029, CountryCode: "UA"},
	"kharkiv":                     {Lat: 49.9935, Lon: 36.2304, CountryCode: "UA"},
	"kyiv":                        {Lat: 50.4501, Lon: 30.5234, CountryCode: "UA"},
	"crimea":                      {Lat: 45.3453, Lon: 34.4992, CountryCode: "UA"},
	"gaza":                        {Lat: 31.3547, Lon: 34.3088, CountryCode: "PS"},
	"gaza strip":                  {Lat: 31.4000, Lon: 34.3500, CountryCode: "PS"},
	"west bank":                   {Lat: 31.9500, Lon: 35.2500, CountryCode: "PS"},
	"northern cyprus":             {Lat: 35.2500, Lon: 33.2500, CountryCode: "CY"},
	"southern lebanon":            {Lat: 33.2500, Lon: 35.4000, CountryCode: "LB"},
	"north kivu":                  {Lat: -0.9000, Lon: 29.2000, CountryCode: "CD"},
	"fergana valley":              {Lat: 40.4000, Lon: 71.8000, CountryCode: ""},
	"kashmir region":              {Lat: 34.1000, Lon: 74.6000, CountryCode: ""},
	"sir creek":                   {Lat: 23.9167, Lon: 68.5833, CountryCode: ""},
	"marib":                       {Lat: 15.4700, Lon: 45.3250, CountryCode: "YE"},
	"marib energy zone":           {Lat: 15.4700, Lon: 45.3250, CountryCode: "YE"},
	"baghdad":                     {Lat: 33.3152, Lon: 44.3661, CountryCode: "IQ"},
	"erbil":                       {Lat: 36.1911, Lon: 44.0092, CountryCode: "IQ"},
	"tehran":                      {Lat: 35.6892, Lon: 51.3890, CountryCode: "IR"},
	"isfahan":                     {Lat: 32.6546, Lon: 51.6680, CountryCode: "IR"},
	"bandar abbas":                {Lat: 27.1832, Lon: 56.2666, CountryCode: "IR"},
	"kharg island":                {Lat: 29.2600, Lon: 50.3300, CountryCode: "IR"},
	"kharg island oil export hub": {Lat: 29.2600, Lon: 50.3300, CountryCode: "IR"},
	"khoy":                        {Lat: 38.5503, Lon: 44.9521, CountryCode: "IR"},
	"khoy region":                 {Lat: 38.5503, Lon: 44.9521, CountryCode: "IR"},
	"fujairah":                    {Lat: 25.1288, Lon: 56.3265, CountryCode: "AE"},
	"port of fujairah":            {Lat: 25.1122, Lon: 56.3575, CountryCode: "AE"},
	"cianjur":                     {Lat: -6.8173, Lon: 107.1423, CountryCode: "ID"},
	"west java cianjur":           {Lat: -6.8173, Lon: 107.1423, CountryCode: "ID"},
	"eastern iburi":               {Lat: 42.7000, Lon: 141.9000, CountryCode: "JP"},
	"red sea":                     {Lat: 21.0000, Lon: 39.0000, CountryCode: ""},
	"southern red sea":            {Lat: 14.5000, Lon: 42.7000, CountryCode: ""},
	"red sea security crisis":     {Lat: 17.5000, Lon: 41.5000, CountryCode: ""},
	"north sea":                   {Lat: 56.0000, Lon: 3.0000, CountryCode: ""},
	"bab el mandeb":               {Lat: 12.5850, Lon: 43.3330, CountryCode: ""},
	"bab-el-mandeb":               {Lat: 12.5850, Lon: 43.3330, CountryCode: ""},
	"bab al mandab":               {Lat: 12.5850, Lon: 43.3330, CountryCode: ""},
	"black sea":                   {Lat: 43.0000, Lon: 34.0000, CountryCode: ""},
	"taiwan strait":               {Lat: 24.0000, Lon: 119.5000, CountryCode: ""},
	"east china sea":              {Lat: 27.5000, Lon: 126.0000, CountryCode: ""},
	"south china sea":             {Lat: 12.0000, Lon: 114.0000, CountryCode: ""},
	"hormuz":                      {Lat: 26.5667, Lon: 56.2500, CountryCode: ""},
	"strait of hormuz":            {Lat: 26.5667, Lon: 56.2500, CountryCode: ""},
	"english channel":             {Lat: 50.3000, Lon: -1.5000, CountryCode: ""},
	"strait of gibraltar":         {Lat: 35.9667, Lon: -5.6000, CountryCode: ""},
	"malacca strait":              {Lat: 3.0000, Lon: 100.5000, CountryCode: ""},
	"panama canal":                {Lat: 9.0800, Lon: -79.6800, CountryCode: "PA"},
	"suez":                        {Lat: 30.5852, Lon: 32.2654, CountryCode: "EG"},
	"suez canal":                  {Lat: 30.2000, Lon: 32.5500, CountryCode: "EG"},
	"salalah":                     {Lat: 17.0197, Lon: 54.0897, CountryCode: "OM"},
	"salalah port":                {Lat: 16.9437, Lon: 54.0053, CountryCode: "OM"},
}

var zoneCountryCentroids = map[string]geoPoint{
	"AE": {Lat: 24.3000, Lon: 54.4000},
	"AF": {Lat: 33.0000, Lon: 66.0000},
	"AR": {Lat: -34.0000, Lon: -64.0000},
	"AU": {Lat: -25.2744, Lon: 133.7751},
	"BR": {Lat: -14.2350, Lon: -51.9253},
	"BD": {Lat: 23.6850, Lon: 90.3563},
	"BH": {Lat: 26.0667, Lon: 50.5577},
	"CA": {Lat: 56.1304, Lon: -106.3468},
	"CL": {Lat: -35.6751, Lon: -71.5430},
	"CN": {Lat: 35.8617, Lon: 104.1954},
	"DE": {Lat: 51.1657, Lon: 10.4515},
	"EG": {Lat: 26.8206, Lon: 30.8025},
	"ES": {Lat: 40.4637, Lon: -3.7492},
	"FR": {Lat: 46.2276, Lon: 2.2137},
	"GB": {Lat: 55.3781, Lon: -3.4360},
	"GR": {Lat: 39.0742, Lon: 21.8243},
	"ID": {Lat: -0.7893, Lon: 113.9213},
	"IE": {Lat: 53.1424, Lon: -7.6921},
	"IL": {Lat: 31.0461, Lon: 34.8516},
	"IN": {Lat: 20.5937, Lon: 78.9629},
	"IQ": {Lat: 33.2232, Lon: 43.6793},
	"IR": {Lat: 32.4279, Lon: 53.6880},
	"IT": {Lat: 41.8719, Lon: 12.5674},
	"JP": {Lat: 36.2048, Lon: 138.2529},
	"KR": {Lat: 35.9078, Lon: 127.7669},
	"KP": {Lat: 40.3399, Lon: 127.5101},
	"LB": {Lat: 33.8547, Lon: 35.8623},
	"MX": {Lat: 23.6345, Lon: -102.5528},
	"MY": {Lat: 4.2105, Lon: 101.9758},
	"NG": {Lat: 9.0820, Lon: 8.6753},
	"NL": {Lat: 52.1326, Lon: 5.2913},
	"PK": {Lat: 30.3753, Lon: 69.3451},
	"PH": {Lat: 12.8797, Lon: 121.7740},
	"PG": {Lat: -6.3150, Lon: 143.9555},
	"PE": {Lat: -9.1900, Lon: -75.0152},
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
	"NO": {Lat: 60.4720, Lon: 8.4689},
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
		if hasAnyKeyword(text, "port", "terminal", "shipping", "container", "cargo", "operations", "shutdown", "shuts down", "closed", "closure") {
			if hasAnyKeyword(text, "shipping", "port", "terminal", "container", "cargo", "maritime") {
				return "risk:maritime_hazard"
			}
			return "risk:service_outage"
		}
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
	if place := strings.TrimSpace(extractKnownPlaceFromText(text)); place != "" {
		add(humanizeHeuristicSeedName(place))
		for _, alias := range heuristicSignalPlaceSeedAliases(place, e) {
			add(alias)
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
	primary := strings.ToUpper(strings.TrimSpace(countryFromLocationPathString(locationPath)))
	paths := make([]string, 0, len(countries))
	seen := make(map[string]bool)
	addPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if place := normalizePhrase(extractKnownPlaceFromText(text)); place == "strait of hormuz" {
		addPath("/loc/int/strait-of-hormuz")
	}
	if place := normalizePhrase(extractKnownMaritimeChokepointFromText(text)); place != "" {
		switch place {
		case "red sea":
			addPath("/loc/int/red-sea")
		case "bab el mandeb":
			addPath("/loc/int/bab-el-mandeb")
		case "strait of hormuz":
			addPath("/loc/int/strait-of-hormuz")
		}
	}
	if strings.Contains(normalizePhrase(text), "red sea") {
		addPath("/loc/int/red-sea")
	}
	for _, cc := range countries {
		cc = strings.ToUpper(strings.TrimSpace(cc))
		if cc == "" {
			continue
		}
		if cc == primary && primary != "" {
			continue
		}
		addPath(locationPathFromCountryCode(cc))
	}
	if hasDirectDisruptionTrigger(normalizeKeywordText(text)) {
		if cc := strings.ToUpper(strings.TrimSpace(e.CountryCode)); cc != "" {
			addPath(locationPathFromCountryCode(cc))
		}
	}
	return paths
}

func humanizeHeuristicSeedName(seed string) string {
	parts := strings.Fields(strings.TrimSpace(strings.ReplaceAll(seed, "-", " ")))
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		switch strings.ToLower(part) {
		case "of", "el", "al", "and":
			parts[i] = strings.ToLower(part)
		default:
			runes := []rune(strings.ToLower(part))
			if len(runes) == 0 {
				continue
			}
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
			parts[i] = string(runes)
		}
	}
	return strings.Join(parts, " ")
}

func heuristicSignalPlaceSeedAliases(place string, e *horizonMonitorFeedEvent) []string {
	place = normalizePhrase(place)
	if place == "" {
		return nil
	}
	out := make([]string, 0, 3)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	humanized := humanizeHeuristicSeedName(place)
	switch place {
	case "fujairah":
		add("Port of Fujairah")
		add("Fujairah oil terminal")
	case "salalah port":
		add("Port of Salalah")
	case "kharg island":
		add("Kharg Island Iran")
		add("Kharg Island oil export hub")
		add("Iranian oil export hub")
	}
	if humanized != "" && strings.TrimSpace(e.CountryName) != "" && !strings.Contains(strings.ToLower(humanized), strings.ToLower(strings.TrimSpace(e.CountryName))) {
		add(humanized + " " + strings.TrimSpace(e.CountryName))
	}
	return uniqueStrings(out)
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
	profile := wmHeuristics().ProfileArticle(text, affectedZoneSourceHost(e))

	level := strings.ToLower(strings.TrimSpace(cls.Level))
	category := strings.ToLower(strings.TrimSpace(cls.Category))

	if profile.likelyAnalysisRoundup() && !looksLikeWarmConflictPosture(text, cls) {
		cls.Level = "info"
		cls.Category = "general"
		cls.Confidence = 0.30
		cls.Source = "keyword_ac+profile"
		return cls
	}
	if profile.likelyAdvisoryCampaignWithoutOutage() && (category == "cyber" || category == "infrastructure" || category == "general") {
		cls.Level = "low"
		cls.Category = "cyber"
		if cls.Confidence < 0.58 {
			cls.Confidence = 0.58
		}
		cls.Source = "keyword_ac+profile"
		level = "low"
		category = "cyber"
		hasStrongEvent = false
	}
	if (category == "military" || category == "conflict") && looksLikeAdministrativeCommentaryText(text) {
		cls.Level = "info"
		cls.Category = "diplomatic"
		if cls.Confidence > 0.42 || cls.Confidence == 0 {
			cls.Confidence = 0.42
		}
		cls.Source = "keyword_ac+profile"
		level = "info"
		category = "diplomatic"
		hasStrongEvent = false
	}
	if (category == "military" || category == "conflict") && looksLikeCivilIncidentWithoutMilitaryContext(text) {
		cls.Level = "medium"
		cls.Category = "crime"
		if cls.Confidence < 0.60 {
			cls.Confidence = 0.60
		}
		cls.Source = "keyword_ac+profile"
		level = "medium"
		category = "crime"
		hasStrongEvent = false
	}

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
	"PG": "Papua New Guinea",
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

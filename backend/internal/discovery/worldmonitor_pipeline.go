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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	wmIngestModeDocumentFeed = "document_feed"
	wmIngestModeOntologyFact = "ontology_fact"
	wmIngestModeQueryOnly    = "query_only"
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
	exec               *ai.AITaskExecutor
	seedInjector       *signals.SeedInjector
	propagationEngine  *horizonpropagation.PropagationEngine
	profileLookup      *horizonpropagation.ProfileLookup
	logger             *log.Logger
	window             time.Duration
	waveThrottleWindow time.Duration

	mu            sync.Mutex
	observations  []horizonMonitorSignalObservation
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
		seedInjector:       seedInjector,
		propagationEngine:  propagationEngine,
		profileLookup:      profileLookup,
		logger:             logger,
		window:             24 * time.Hour,
		waveThrottleWindow: 30 * time.Minute,
		recentWaves:        make(map[string]time.Time),
		lastBriefByCC:      make(map[string]time.Time),
	}
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
	aiClassificationsUsed := 0

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
				if signal := p.buildSignalDescriptor(eventFact, event); signal != nil {
					if err := p.upsertAndPropagateSignal(ctx, tenantID, ds.ID, signal, eventFact); err != nil {
						p.logf("horizon monitor pipeline: signal propagation failed source=%s signal=%s: %v", ds.ID, signal.ID, err)
					}
				}
			}
		}

		classification := classifyHorizonMonitorNewsKeyword(event.Title)
		if p.exec != nil && mode == wmIngestModeOntologyFact && classification.Confidence < 0.65 && aiClassificationsUsed < 4 {
			if refined, ok := p.classifyHorizonMonitorNewsWithAI(ctx, event.Title); ok {
				classification = refined
				aiClassificationsUsed++
			}
		}
		if err := p.upsertNewsClassificationFact(ctx, tenantID, ds.ID, event, classification); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert news classification source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
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
	}

	if len(newObservations) == 0 {
		return nil
	}

	countries, cells := p.mergeAndAggregate(newObservations)
	if err := p.upsertCountryRollupFacts(ctx, tenantID, countries); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert country rollups: %v", err)
	}
	if err := p.upsertGridCellFacts(ctx, tenantID, cells); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert grid rollups: %v", err)
	}
	if mode == wmIngestModeOntologyFact {
		if err := p.upsertCountryBriefFacts(ctx, tenantID, countries); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert country briefs: %v", err)
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
	aiClassificationsUsed := 0
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
				if signal := p.buildSignalDescriptor(eventFact, event); signal != nil {
					if err := p.upsertAndPropagateSignal(ctx, tenantID, ds.ID, signal, eventFact); err != nil {
						p.logf("horizon monitor pipeline: signal propagation failed source=%s signal=%s: %v", ds.ID, signal.ID, err)
					}
				}
			}
		}
		classification := classifyHorizonMonitorNewsKeyword(event.Title)
		if p.exec != nil && mode == wmIngestModeOntologyFact && classification.Confidence < 0.65 && aiClassificationsUsed < 4 {
			if refined, ok := p.classifyHorizonMonitorNewsWithAI(ctx, event.Title); ok {
				classification = refined
				aiClassificationsUsed++
			}
		}
		if err := p.upsertNewsClassificationFact(ctx, tenantID, ds.ID, event, classification); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert news classification source=%s ext=%s: %v", ds.ID, event.ExternalID, err)
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
	}
	if len(newObservations) == 0 {
		return nil
	}
	countries, cells := p.mergeAndAggregate(newObservations)
	if err := p.upsertCountryRollupFacts(ctx, tenantID, countries); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert country rollups: %v", err)
	}
	if err := p.upsertGridCellFacts(ctx, tenantID, cells); err != nil {
		p.logf("horizon monitor pipeline: failed to upsert grid rollups: %v", err)
	}
	if mode == wmIngestModeOntologyFact {
		if err := p.upsertCountryBriefFacts(ctx, tenantID, countries); err != nil {
			p.logf("horizon monitor pipeline: failed to upsert country briefs: %v", err)
		}
	}
	return nil
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

	if blueprint, ok := horizonMonitorBlueprintFromSignal(firstNonEmptyString(strings.TrimSpace(signalHint), signalTypeFromFeedType(e.FeedType))); ok {
		e.SignalType = blueprint.SignalType
		e.EventType = eventTypeFromSignalType(blueprint.SignalType)
		e.SpherePaths = append([]string{}, blueprint.Sphere...)
	} else {
		e.SignalType = firstNonEmptyString(strings.TrimSpace(signalHint), signalTypeFromFeedType(e.FeedType), "risk:temporal_anomaly")
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
			Tags: uniqueStrings([]string{
				"horizon-monitor",
				"feed_type:" + strings.TrimSpace(e.FeedType),
				"severity:" + strings.TrimSpace(e.Severity),
			}),
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
	s := &horizonmodels.SignalDescriptor{
		ID:           signalID,
		FactID:       strings.TrimSpace(eventFact.ID),
		CreatedAt:    time.Now().UTC(),
		Axis:         blueprint.Axis,
		Mechanism:    blueprint.Mechanism,
		Amplitude:    amplitude,
		Polarity:     "risk",
		LocationPath: locationPath,
		SpherePaths:  uniqueStrings(append(eventFact.SpherePaths, blueprint.Sphere...)),
		Constraints: horizonmodels.SignalConstraints{
			TimeHorizon: horizonmodels.SignalTimeHorizonShortTerm,
			Timeframe:   horizonmodels.SignalTimeHorizonShortTerm,
			Modality:    horizonmodels.SignalModalityActual,
			Likelihood:  wmClamp01(eventFact.Confidence),
			Impact:      amplitude,
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

func (p *HorizonMonitorPipeline) upsertAndPropagateSignal(
	ctx context.Context,
	tenantID, sourceID string,
	signal *horizonmodels.SignalDescriptor,
	eventFact *coremodels.OntologyFact,
) error {
	if signal == nil || eventFact == nil {
		return nil
	}
	waveKey := strings.TrimSpace(signal.ID)
	if p.isWaveThrottled(waveKey) {
		return nil
	}

	signalFact := horizonMonitorSignalFact(signal, tenantID, sourceID, eventFact)
	if err := p.factsRepo.Upsert(ctx, signalFact); err != nil {
		return err
	}

	if p.seedInjector != nil {
		seedIDs, err := p.seedInjector.Inject(ctx, signal)
		if err == nil && len(seedIDs) > 0 && p.propagationEngine != nil && p.profileLookup != nil {
			_, _ = p.propagationEngine.PropagateSignalFromSeeds(ctx, signal, p.profileLookup, seedIDs)
		}
	}

	p.markWaveSeen(waveKey)
	return nil
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
			Tags: uniqueStrings([]string{
				"horizon-monitor",
				"signal_axis:" + strings.TrimSpace(signal.Axis),
				"signal_mechanism:" + strings.TrimSpace(signal.Mechanism),
				"signal_type:" + etype,
			}),
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
	}
}

func (p *HorizonMonitorPipeline) upsertNewsClassificationFact(
	ctx context.Context,
	tenantID, sourceID string,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
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
			Tags: uniqueStrings([]string{
				"horizon-monitor",
				"classification",
				"news_level:" + strings.TrimSpace(cls.Level),
				"news_category:" + strings.TrimSpace(cls.Category),
			}),
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
		tags := []string{"horizon-monitor", "country_rollup", "country:" + strings.ToLower(cc)}
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
				Tags:           uniqueStrings(tags),
			},
			Kind:          "metric",
			Type:          "horizon_monitor_country_rollup",
			Title:         "Horizon Monitor Country Rollup: " + agg.CountryName,
			Summary:       fmt.Sprintf("%s: %d signals (%d high), %d types, convergence score %.0f", agg.CountryName, agg.TotalSignals, agg.HighSeverityCount, agg.DistinctSignalTypes, agg.ConvergenceScore),
			Confidence:    0.95,
			ObservedAt:    &observedAt,
			SourceIDs:     agg.SourceIDs,
			MetricUpdates: metricUpdates,
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
			"horizon-monitor",
			"geo_grid_cell",
			"cell:" + slug(cellID),
			"lat_bucket:" + strconv.FormatFloat(math.Floor(cell.CellCenterLat), 'f', 0, 64),
			"lon_bucket:" + strconv.FormatFloat(math.Floor(cell.CellCenterLon), 'f', 0, 64),
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
				Tags:           uniqueStrings(tags),
			},
			Kind:          "metric",
			Type:          "horizon_monitor_geo_cell",
			Title:         "Horizon Monitor Geo Cell " + cellID,
			Summary:       fmt.Sprintf("Cell %s: %d signals, %d types, score %.0f", cellID, cell.TotalSignals, cell.DistinctSignalTypes, cell.ConvergenceScore),
			Confidence:    0.95,
			ObservedAt:    &observedAt,
			MetricUpdates: metricUpdates,
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
				Tags: uniqueStrings([]string{
					"horizon-monitor",
					"country_brief",
					"country:" + strings.ToLower(cc),
				}),
			},
			Kind:       "other",
			Type:       "country_brief",
			Title:      "Country Brief: " + agg.CountryName,
			Summary:    brief.Summary,
			Confidence: wmClamp01(brief.Confidence),
			ObservedAt: &now,
			SourceIDs:  agg.SourceIDs,
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
	if p.exec == nil {
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
	lower := strings.ToLower(strings.TrimSpace(title))
	if lower == "" {
		return horizonMonitorNewsClassification{Level: "info", Category: "general", Confidence: 0.35, Source: "keyword"}
	}

	critical := map[string]string{
		"nuclear strike":  "military",
		"nuclear attack":  "military",
		"invasion":        "conflict",
		"coup":            "military",
		"genocide":        "conflict",
		"chemical attack": "terrorism",
	}
	high := map[string]string{
		"airstrike":       "conflict",
		"missile":         "military",
		"troops deployed": "military",
		"terror":          "terrorism",
		"cyber attack":    "cyber",
		"ransomware":      "cyber",
		"earthquake":      "disaster",
		"hurricane":       "disaster",
		"blackout":        "infrastructure",
		"internet outage": "infrastructure",
		"service outage":  "infrastructure",
	}
	medium := map[string]string{
		"protest":           "protest",
		"riot":              "protest",
		"unrest":            "protest",
		"sanctions":         "economic",
		"embargo":           "economic",
		"trade war":         "economic",
		"military exercise": "military",
		"wildfire":          "disaster",
		"flood":             "disaster",
	}
	low := map[string]string{
		"election":   "diplomatic",
		"summit":     "diplomatic",
		"treaty":     "diplomatic",
		"agreement":  "diplomatic",
		"gdp":        "economic",
		"inflation":  "economic",
		"regulation": "economic",
	}

	if cat, ok := matchKeywordCategory(lower, critical); ok {
		return horizonMonitorNewsClassification{Level: "critical", Category: cat, Confidence: 0.90, Source: "keyword"}
	}
	if cat, ok := matchKeywordCategory(lower, high); ok {
		return horizonMonitorNewsClassification{Level: "high", Category: cat, Confidence: 0.82, Source: "keyword"}
	}
	if cat, ok := matchKeywordCategory(lower, medium); ok {
		return horizonMonitorNewsClassification{Level: "medium", Category: cat, Confidence: 0.70, Source: "keyword"}
	}
	if cat, ok := matchKeywordCategory(lower, low); ok {
		return horizonMonitorNewsClassification{Level: "low", Category: cat, Confidence: 0.58, Source: "keyword"}
	}
	return horizonMonitorNewsClassification{Level: "info", Category: "general", Confidence: 0.35, Source: "keyword"}
}

func matchKeywordCategory(text string, mapping map[string]string) (string, bool) {
	for kw, cat := range mapping {
		if strings.Contains(text, kw) {
			return cat, true
		}
	}
	return "", false
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
	"iran":                 "IR",
	"israel":               "IL",
	"saudi arabia":         "SA",
	"united arab emirates": "AE",
	"iraq":                 "IQ",
	"syria":                "SY",
	"yemen":                "YE",
	"jordan":               "JO",
	"lebanon":              "LB",
	"china":                "CN",
	"taiwan":               "TW",
	"japan":                "JP",
	"south korea":          "KR",
	"north korea":          "KP",
	"india":                "IN",
	"pakistan":             "PK",
	"ukraine":              "UA",
	"russia":               "RU",
	"egypt":                "EG",
	"united states":        "US",
	"usa":                  "US",
	"united kingdom":       "GB",
	"uk":                   "GB",
	"germany":              "DE",
	"france":               "FR",
}

var countryCodeToName = map[string]string{
	"IR": "Iran",
	"IL": "Israel",
	"SA": "Saudi Arabia",
	"AE": "United Arab Emirates",
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
	"IN": "India",
	"PK": "Pakistan",
	"UA": "Ukraine",
	"RU": "Russia",
	"EG": "Egypt",
	"US": "United States",
	"GB": "United Kingdom",
	"DE": "Germany",
	"FR": "France",
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

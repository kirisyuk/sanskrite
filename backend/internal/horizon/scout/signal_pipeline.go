package scout

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signallanding"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// SignalPipeline integrates signal extraction, injection, and propagation.
//
// NOTE: Scout no longer creates signals. This pipeline is reserved for the future Signaling Pipeline
// (news flow → events → 12-axis signals → propagation). Scout only populates the graph (facts, fragments, edges).
type SignalPipeline struct {
	signalExtractor      *signals.SignalExtractor
	escalationClassifier *signals.EscalationClassifier
	seedInjector         *signals.SeedInjector
	propagationEngine    *propagation.PropagationEngine
	profileLookup        *propagation.ProfileLookup
	factsRepo            *corerepo.OntologyFactsRepository
	signalLanding        *signallanding.Service
	waveThrottleWindow   time.Duration

	mu          sync.Mutex
	recentWaves map[string]time.Time // key: eventId|signalKey -> last propagation time
}

// NewSignalPipeline creates a new SignalPipeline.
func NewSignalPipeline(
	signalExtractor *signals.SignalExtractor,
	seedInjector *signals.SeedInjector,
	propagationEngine *propagation.PropagationEngine,
	profileLookup *propagation.ProfileLookup,
	factsRepo *corerepo.OntologyFactsRepository,
) *SignalPipeline {
	return &SignalPipeline{
		signalExtractor:      signalExtractor,
		escalationClassifier: signals.NewEscalationClassifier(factsRepo),
		seedInjector:         seedInjector,
		propagationEngine:    propagationEngine,
		profileLookup:        profileLookup,
		factsRepo:            factsRepo,
		waveThrottleWindow:   30 * time.Minute,
		recentWaves:          make(map[string]time.Time),
	}
}

func (p *SignalPipeline) WithEscalationLLM(aiExecutor *ai.AITaskExecutor) *SignalPipeline {
	if p == nil || p.escalationClassifier == nil {
		return p
	}
	p.escalationClassifier.WithAIExecutor(aiExecutor)
	return p
}

func (p *SignalPipeline) WithSignalLanding(service *signallanding.Service) *SignalPipeline {
	if p == nil {
		return p
	}
	p.signalLanding = service
	return p
}

// ProcessEvent processes an event through the signal pipeline.
// This should be called after an event OntologyFact is created.
func (p *SignalPipeline) ProcessEvent(ctx context.Context, event *coremodels.OntologyFact) error {
	if event == nil || event.Kind != "event" {
		return nil
	}
	if p == nil || p.signalExtractor == nil || p.seedInjector == nil {
		return nil
	}
	log.Printf("%s start eventId=%s type=%s", signalsys.LogPipeline, strings.TrimSpace(event.ID), strings.TrimSpace(event.Type))

	// Step 1: Extract signals from event
	signalDescriptors, err := p.signalExtractor.Extract(ctx, event)
	if err != nil {
		return fmt.Errorf("signal extraction failed: %w", err)
	}

	if len(signalDescriptors) == 0 {
		log.Printf("%s no signals extracted eventId=%s", signalsys.LogPipeline, strings.TrimSpace(event.ID))
		return nil // No signals extracted
	}

	if p.signalLanding != nil {
		if _, lerr := p.signalLanding.LandEvent(ctx, event, signalDescriptors); lerr != nil {
			log.Printf("%s landing failed eventId=%s err=%v", signalsys.LogPipeline, strings.TrimSpace(event.ID), lerr)
		}
	}

	existingKeys := p.loadExistingSignalKeysForEvent(ctx, event)

	// Step 2: Save signals as OntologyFact(kind=signal)
	for _, signal := range signalDescriptors {
		if signal == nil {
			continue
		}
		// Generate ID if not set
		if signal.ID == "" {
			signal.ID = fmt.Sprintf("sig_%d", time.Now().UnixNano())
		}
		// Deterministic escalation baseline before persistence/propagation.
		p.enrichSignalEscalation(ctx, signal)
		signalKey := p.buildSignalKey(signal)
		waveKey := strings.TrimSpace(event.ID) + "|" + signalKey
		if p.isWaveThrottled(waveKey) {
			log.Printf("%s skip_throttled_same_event eventId=%s signalKey=%s", signalsys.LogPipeline, strings.TrimSpace(event.ID), signalKey)
			continue
		}
		if existingKeys[signalKey] {
			log.Printf("%s skip_duplicate_same_event eventId=%s signalKey=%s", signalsys.LogPipeline, strings.TrimSpace(event.ID), signalKey)
			p.markWaveSeen(waveKey)
			continue
		}

		// Persist as OntologyFact(kind=signal) when available (best-effort).
		if p.factsRepo != nil {
			signalFact := p.signalToFact(signal, event, signalKey)
			if signalFactID, err := p.factsRepo.Create(ctx, signalFact); err == nil && signalFactID != "" {
				signal.ID = signalFactID
				existingKeys[signalKey] = true
			}
		}
		signal.FactID = event.ID

		// Step 3: Inject metrics into seed nodes
		seedFragmentIDs, err := p.seedInjector.Inject(ctx, signal)
		if errors.Is(err, signals.ErrNoSeedNodes) {
			log.Printf("%s no_seeds signalId=%s axis=%s location=%s", signalsys.LogPipeline, strings.TrimSpace(signal.ID), strings.TrimSpace(signal.Axis), strings.TrimSpace(signal.LocationPath))
			continue
		}
		if err != nil {
			log.Printf("%s seed injection failed signalId=%s err=%v", signalsys.LogPipeline, strings.TrimSpace(signal.ID), err)
			continue
		}
		log.Printf("%s seeded signalId=%s seeds=%d metric=%s", signalsys.LogPipeline, strings.TrimSpace(signal.ID), len(seedFragmentIDs), strings.TrimSpace(signal.MetricPath()))

		// Step 4: Propagate signal through graph
		if p.propagationEngine != nil && p.profileLookup != nil {
			if _, err := p.propagationEngine.PropagateSignalFromSeeds(ctx, signal, p.profileLookup, seedFragmentIDs); err != nil {
				log.Printf("%s propagation failed signalId=%s err=%v", signalsys.LogPipeline, strings.TrimSpace(signal.ID), err)
				continue
			}
		}
		p.markWaveSeen(waveKey)
	}

	log.Printf("%s done eventId=%s signals=%d", signalsys.LogPipeline, strings.TrimSpace(event.ID), len(signalDescriptors))
	return nil
}

func (p *SignalPipeline) enrichSignalEscalation(ctx context.Context, signal *horizonmodels.SignalDescriptor) {
	if p == nil || signal == nil || p.escalationClassifier == nil {
		return
	}
	assessment, err := p.escalationClassifier.Assess(ctx, signal)
	if err != nil || assessment == nil {
		return
	}
	signal.Constraints.EscalationLevel = strings.TrimSpace(assessment.Level)
	signal.Constraints.EscalationConfidence = assessment.Confidence
	signal.Constraints.EvidenceBuckets = assessment.Buckets

	if signal.Constraints.Likelihood <= 0 {
		signal.Constraints.Likelihood = signal.Confidence
	}
	if signal.Constraints.Impact <= 0 {
		signal.Constraints.Impact = signal.Amplitude
	}
	if strings.TrimSpace(signal.Constraints.TimeHorizon) == "" && strings.TrimSpace(signal.Constraints.Timeframe) == "" {
		signal.Constraints.TimeHorizon = horizonmodels.SignalTimeHorizonShortTerm
		signal.Constraints.Timeframe = horizonmodels.SignalTimeHorizonShortTerm
	}
	switch strings.TrimSpace(assessment.Level) {
	case horizonmodels.EscalationLevelCritical:
		signal.Constraints.Modality = horizonmodels.StrongerSignalModality(signal.Constraints.Modality, horizonmodels.SignalModalityDisruption)
	case horizonmodels.EscalationLevelWarning, horizonmodels.EscalationLevelWatch:
		signal.Constraints.Modality = horizonmodels.StrongerSignalModality(signal.Constraints.Modality, horizonmodels.SignalModalityEscalation)
	}
}

// signalToFact converts SignalDescriptor to OntologyFact for storage.
func (p *SignalPipeline) signalToFact(signal *horizonmodels.SignalDescriptor, sourceEvent *coremodels.OntologyFact, signalKey string) *coremodels.OntologyFact {
	now := time.Now()
	var locPtr *string
	if strings.TrimSpace(signal.LocationPath) != "" {
		lp := strings.TrimSpace(signal.LocationPath)
		locPtr = &lp
	}
	var eventID *string
	if strings.TrimSpace(sourceEvent.ID) != "" {
		id := strings.TrimSpace(sourceEvent.ID)
		eventID = &id
	}
	signalType := fmt.Sprintf("%s:%s", signal.Polarity, signal.Axis)
	if strings.TrimSpace(signal.Mechanism) != "" {
		signalType = signalType + ":" + strings.TrimSpace(signal.Mechanism)
	}
	return &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:        signal.ID,
			TenantID:  sourceEvent.TenantID,
			Version:   1,
			CreatedAt: signal.CreatedAt,
			UpdatedAt: now,
			CreatedBy: "system",
			UpdatedBy: "system",
			Labels:    map[string]string{"en": fmt.Sprintf("%s signal: %s", signal.Polarity, signal.Axis)},
			Tags: []string{
				"signal_polarity:" + strings.TrimSpace(signal.Polarity),
				"signal_axis:" + strings.TrimSpace(signal.Axis),
				fmt.Sprintf("signal_impact:%.4f", signal.Constraints.Impact),
				fmt.Sprintf("signal_likelihood:%.4f", signal.Constraints.Likelihood),
				fmt.Sprintf("signal_amplitude:%.4f", signal.Amplitude),
				fmt.Sprintf("signal_confidence:%.4f", signal.Confidence),
				"signal_modality:" + strings.TrimSpace(signal.Constraints.Modality),
				"signal_horizon:" + strings.TrimSpace(signal.Constraints.TimeHorizon),
				"signal_escalation:" + strings.TrimSpace(signal.Constraints.EscalationLevel),
				"source_event:" + strings.TrimSpace(sourceEvent.ID),
				"signal_key:" + signalKey,
			},
			Status:         "active",
			CanonicalPaths: signal.SeedPaths,
			ActorPaths:     signal.ActorPaths,
			SpherePaths:    signal.SpherePaths,
			LocationPath:   locPtr,
		},
		Kind:  "signal",
		Type:  signalType,
		Title: fmt.Sprintf("%s signal: %s", signal.Polarity, signal.Axis),
		Summary: fmt.Sprintf(
			"Amplitude: %.2f; Impact: %.2f; Likelihood: %.2f; Modality: %s; Horizon: %s; Escalation: %s; Mechanism: %s",
			signal.Amplitude,
			signal.Constraints.Impact,
			signal.Constraints.Likelihood,
			strings.TrimSpace(signal.Constraints.Modality),
			strings.TrimSpace(signal.Constraints.TimeHorizon),
			strings.TrimSpace(signal.Constraints.EscalationLevel),
			signal.Mechanism,
		),
		Confidence: signal.Confidence,
		ObservedAt: &signal.ObservedAt,
		EventID:    eventID,
		SourceIDs:  signal.SourceRefs,
		SourceRefs: sourceEvent.SourceRefs,
	}
}

func (p *SignalPipeline) loadExistingSignalKeysForEvent(ctx context.Context, event *coremodels.OntologyFact) map[string]bool {
	out := make(map[string]bool)
	if p == nil || p.factsRepo == nil || event == nil || strings.TrimSpace(event.ID) == "" {
		return out
	}
	facts, err := p.factsRepo.ListByEventIDAndKind(ctx, strings.TrimSpace(event.TenantID), strings.TrimSpace(event.ID), "signal", 500)
	if err != nil {
		return out
	}
	for _, f := range facts {
		if f == nil {
			continue
		}
		if key, ok := extractTagValue(f.Tags, "signal_key:"); ok && strings.TrimSpace(key) != "" {
			out[key] = true
			continue
		}
		// Backward compatibility for older signal facts without signal_key tags.
		if key := buildSignalKeyFromFactFallback(f); key != "" {
			out[key] = true
		}
	}
	return out
}

func extractTagValue(tags []string, prefix string) (string, bool) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", false
	}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(t, prefix)), true
		}
	}
	return "", false
}

func (p *SignalPipeline) buildSignalKey(signal *horizonmodels.SignalDescriptor) string {
	if signal == nil {
		return ""
	}
	seed := uniqueSorted(signal.SeedPaths)
	actors := uniqueSorted(signal.ActorPaths)
	return strings.Join([]string{
		strings.TrimSpace(signal.Polarity),
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Mechanism),
		strings.TrimSpace(signal.LocationPath),
		strings.Join(seed, ","),
		strings.Join(actors, ","),
	}, "|")
}

func buildSignalKeyFromFactFallback(f *coremodels.OntologyFact) string {
	if f == nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(f.Type), ":")
	polarity := ""
	axis := ""
	mechanism := ""
	if len(parts) >= 2 {
		polarity = strings.TrimSpace(parts[0])
		axis = strings.TrimSpace(parts[1])
	}
	if len(parts) >= 3 {
		mechanism = strings.TrimSpace(parts[2])
	}
	location := ""
	if f.LocationPath != nil {
		location = strings.TrimSpace(*f.LocationPath)
	}
	return strings.Join([]string{
		polarity,
		axis,
		mechanism,
		location,
		strings.Join(uniqueSorted(f.CanonicalPaths), ","),
		strings.Join(uniqueSorted(f.ActorPaths), ","),
	}, "|")
}

func uniqueSorted(in []string) []string {
	seen := map[string]struct{}{}
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

func (p *SignalPipeline) isWaveThrottled(waveKey string) bool {
	if p == nil {
		return false
	}
	waveKey = strings.TrimSpace(waveKey)
	if waveKey == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	for key, ts := range p.recentWaves {
		if now.Sub(ts) > p.waveThrottleWindow {
			delete(p.recentWaves, key)
		}
	}
	if ts, ok := p.recentWaves[waveKey]; ok {
		return now.Sub(ts) <= p.waveThrottleWindow
	}
	return false
}

func (p *SignalPipeline) markWaveSeen(waveKey string) {
	if p == nil {
		return
	}
	waveKey = strings.TrimSpace(waveKey)
	if waveKey == "" {
		return
	}
	p.mu.Lock()
	p.recentWaves[waveKey] = time.Now().UTC()
	p.mu.Unlock()
}

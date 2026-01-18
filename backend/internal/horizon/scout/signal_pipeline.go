package scout

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// SignalPipeline integrates signal extraction, injection, and propagation into ScoutEngine.
type SignalPipeline struct {
	signalExtractor   *signals.SignalExtractor
	seedInjector      *signals.SeedInjector
	propagationEngine *propagation.PropagationEngine
	profileLookup     *propagation.ProfileLookup
	factsRepo         *corerepo.OntologyFactsRepository
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
		signalExtractor:   signalExtractor,
		seedInjector:      seedInjector,
		propagationEngine: propagationEngine,
		profileLookup:     profileLookup,
		factsRepo:         factsRepo,
	}
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

	// Step 2: Save signals as OntologyFact(kind=signal)
	for _, signal := range signalDescriptors {
		if signal == nil {
			continue
		}
		// Generate ID if not set
		if signal.ID == "" {
			signal.ID = fmt.Sprintf("sig_%d", time.Now().UnixNano())
		}

		// Persist as OntologyFact(kind=signal) when available (best-effort).
		if p.factsRepo != nil {
			signalFact := p.signalToFact(signal, event)
			if signalFactID, err := p.factsRepo.Create(ctx, signalFact); err == nil && signalFactID != "" {
				signal.ID = signalFactID
			}
		}
		signal.FactID = event.ID

		// Step 3: Inject metrics into seed nodes
		seedFragmentIDs, err := p.seedInjector.Inject(ctx, signal)
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
	}

	log.Printf("%s done eventId=%s signals=%d", signalsys.LogPipeline, strings.TrimSpace(event.ID), len(signalDescriptors))
	return nil
}

// signalToFact converts SignalDescriptor to OntologyFact for storage.
func (p *SignalPipeline) signalToFact(signal *horizonmodels.SignalDescriptor, sourceEvent *coremodels.OntologyFact) *coremodels.OntologyFact {
	now := time.Now()
	var locPtr *string
	if strings.TrimSpace(signal.LocationPath) != "" {
		lp := strings.TrimSpace(signal.LocationPath)
		locPtr = &lp
	}
	return &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:          signal.ID,
			TenantID:    sourceEvent.TenantID,
			Version:     1,
			CreatedAt:   signal.CreatedAt,
			UpdatedAt:   now,
			CreatedBy:   "system",
			UpdatedBy:   "system",
			Labels:      map[string]string{"en": fmt.Sprintf("%s signal: %s", signal.Polarity, signal.Axis)},
			Status:      "active",
			CanonicalPaths: signal.SeedPaths,
			ActorPaths:     signal.ActorPaths,
			SpherePaths:    signal.SpherePaths,
			LocationPath:   locPtr,
		},
		Kind:       "signal",
		Type:       fmt.Sprintf("%s:%s", signal.Polarity, signal.Axis),
		Title:      fmt.Sprintf("%s signal: %s", signal.Polarity, signal.Axis),
		Summary:    fmt.Sprintf("Amplitude: %.2f, Mechanism: %s", signal.Amplitude, signal.Mechanism),
		Confidence: signal.Confidence,
		ObservedAt: &signal.ObservedAt,
		SourceIDs:  signal.SourceRefs,
		SourceRefs: sourceEvent.SourceRefs,
	}
}


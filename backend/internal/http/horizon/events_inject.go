package horizon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

// InjectEvent is a debug endpoint to inject a global event and immediately run the SignalDescriptor pipeline.
// POST /api/horizon/events:inject
//
// Body (minimal):
//
//	{
//	  "title": "...",
//	  "summary": "...",
//	  "canonicalPaths": ["/org/ae/flydubai-llc"],   // used as default seedPaths by extractor
//	  "locationPath": "/loc/ir",
//	  "actorPaths": ["/gov/ir/civil-aviation"],
//	  "spherePaths": ["/sphere/logistics"],
//	  "occurredAt": "2026-01-18T12:00:00Z",
//	  "confidence": 0.8,
//	  "sourceUrl": "https://..."
//	}
func (h *HorizonHandlers) InjectEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if h.factsRepo == nil {
		http.Error(w, "OntologyFactsRepository not configured", http.StatusServiceUnavailable)
		return
	}
	if h.service == nil || h.service.Fragments() == nil || h.service.Edges() == nil {
		http.Error(w, "Horizon service not available", http.StatusServiceUnavailable)
		return
	}
	if h.service.AIExecutor() == nil {
		http.Error(w, "AI executor not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Title          string   `json:"title"`
		Summary        string   `json:"summary,omitempty"`
		Type           string   `json:"type,omitempty"`
		CanonicalPaths []string `json:"canonicalPaths,omitempty"`
		LocationPath   string   `json:"locationPath,omitempty"`
		ActorPaths     []string `json:"actorPaths,omitempty"`
		SpherePaths    []string `json:"spherePaths,omitempty"`
		OccurredAt     *string  `json:"occurredAt,omitempty"`
		Confidence     *float64 `json:"confidence,omitempty"`
		SourceURL      string   `json:"sourceUrl,omitempty"`
		DryRun         bool     `json:"dryRun,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()

	conf := 0.8
	if req.Confidence != nil {
		conf = *req.Confidence
	}
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}

	var occurredAt *time.Time
	if req.OccurredAt != nil && strings.TrimSpace(*req.OccurredAt) != "" {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.OccurredAt)); err == nil {
			tt := t.UTC()
			occurredAt = &tt
		}
	}

	var locPtr *string
	if lp := strings.TrimSpace(req.LocationPath); lp != "" {
		locPtr = &lp
	}

	eventType := strings.TrimSpace(req.Type)
	if eventType == "" {
		eventType = "manual_inject"
	}

	eventFact := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			TenantID:       strings.TrimSpace(authInfo.TenantID),
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
			CreatedBy:      strings.TrimSpace(authInfo.UID),
			UpdatedBy:      strings.TrimSpace(authInfo.UID),
			Labels:         map[string]string{"en": title},
			Status:         "active",
			CanonicalPaths: req.CanonicalPaths,
			LocationPath:   locPtr,
			ActorPaths:     req.ActorPaths,
			SpherePaths:    req.SpherePaths,
		},
		Kind:       "event",
		Type:       eventType,
		Title:      title,
		Summary:    strings.TrimSpace(req.Summary),
		Confidence: conf,
		OccurredAt: occurredAt,
		ObservedAt: occurredAt,
	}
	if u := strings.TrimSpace(req.SourceURL); u != "" {
		eventFact.SourceRefs = []coremodels.SourceRef{{SourceID: u, SourceType: coremodels.SourceTypeWeb}}
	}

	// Persist event (unless dryRun).
	if !req.DryRun {
		id, err := h.factsRepo.Create(r.Context(), eventFact)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to create event fact: %v", err), http.StatusBadRequest)
			return
		}
		eventFact.ID = id
	} else {
		eventFact.ID = "dryrun_event"
	}

	log.Printf("%s event stored eventId=%s type=%s title=%q", signalsys.LogInject, strings.TrimSpace(eventFact.ID), strings.TrimSpace(eventFact.Type), strings.TrimSpace(eventFact.Title))

	// Run pipeline synchronously (debug/inspection).
	extractor := signals.NewSignalExtractor(nil, h.service.AIExecutor())
	applicability := signals.NewExternalApplicabilityEvaluator().
		WithFragmentsRepo(h.service.Fragments()).
		WithEdgesRepo(h.service.Edges())
	if h.exposurePathsRepo != nil {
		applicability = applicability.WithExposurePathsRepo(h.exposurePathsRepo)
	}
	seedInjector := signals.NewSeedInjector(h.service.Fragments(), h.service.Edges()).
		WithApplicabilityEvaluator(applicability)
	propEngine := propagation.NewPropagationEngine(h.service.Fragments(), nil, h.service.Edges(), nil, h.service.RealtimePublisher())
	profiles := propagation.NewProfileLookup(nil) // defaults-only for now

	extracted, err := extractor.Extract(r.Context(), eventFact)
	if err != nil {
		http.Error(w, fmt.Sprintf("signal extraction failed: %v", err), http.StatusBadRequest)
		return
	}

	type signalOut struct {
		Signal        any      `json:"signal"`
		SeedFragments []string `json:"seedFragments"`
		Propagation   any      `json:"propagation,omitempty"`
	}

	out := make([]signalOut, 0, len(extracted))
	for _, sig := range extracted {
		if sig == nil {
			continue
		}
		sig.FactID = eventFact.ID
		if sig.CreatedAt.IsZero() {
			sig.CreatedAt = now
		}

		seedIDs, _ := seedInjector.Inject(r.Context(), sig)

		var propRes any
		if propEngine != nil {
			if pr, err := propEngine.PropagateSignalFromSeeds(r.Context(), sig, profiles, seedIDs); err == nil {
				propRes = pr
			}
		}

		out = append(out, signalOut{
			Signal:        sig,
			SeedFragments: seedIDs,
			Propagation:   propRes,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"eventFactId": eventFact.ID,
		"signals":     out,
	})
}

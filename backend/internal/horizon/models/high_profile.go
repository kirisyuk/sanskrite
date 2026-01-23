package models

import "time"

// =============================================================================
// HIGH-PROFILE SIGNALS (global, precomputed)
// =============================================================================
//
// Purpose (business):
// - Surface "critical / high influence" areas to the user without recomputing graph-derived heuristics on every request.
// - Keep the shape stable for UI and for offline backfills.
//
// Notes:
// - This is GLOBAL (fragment-level). Lens-specific "in-this-lens" signals should live in lens/cockpit overlays.
// - Evidence is intentionally bounded (top-K) to keep Firestore documents small.
//
// Storage:
// - Embedded into HorizonFragment as `highProfile` (optional).
// - Scores are normalized 0..1.
//
// Versioning:
// - Version is bumped when the semantics or keys meaningfully change.
// - Generators/backfills should set UpdatedAt.

// HighProfile stores global, precomputed "high-profile" signals for a fragment.
type HighProfile struct {
	// Version of the high-profile computation contract (NOT the fragment version).
	Version int `json:"version" firestore:"version"`

	// UpdatedAt is when the signals were last computed.
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	// Signals is a map keyed by canonical signal key.
	// Examples:
	// - "dynasty_influence"
	// - "dynasty_power"
	// - "regulated_exposure"
	// - "is_regulator"
	Signals map[string]HighProfileSignal `json:"signals,omitempty" firestore:"signals,omitempty"`
}

// HighProfileSignal is a single signal value with optional bounded evidence.
type HighProfileSignal struct {
	// Score is normalized 0..1 (0 = none, 1 = strongest).
	Score float64 `json:"score" firestore:"score"`

	// Level is an optional UI-friendly bucket derived from Score.
	// Expected values: "low" | "medium" | "high" | "critical".
	Level string `json:"level,omitempty" firestore:"level,omitempty"`

	// Confidence is a best-effort confidence (0..1) of this signal.
	// It can be derived from edge criticality, provenance quality, etc.
	Confidence float64 `json:"confidence,omitempty" firestore:"confidence,omitempty"`

	// Evidence provides top-K pointers for explainability.
	Evidence *HighProfileEvidence `json:"evidence,omitempty" firestore:"evidence,omitempty"`
}

// HighProfileEvidence is a bounded "why" payload.
// Keep it small: the UI can always fetch full edge/fragment objects by IDs/paths if needed.
type HighProfileEvidence struct {
	// EdgeIDs are Horizon dependency edge IDs that most contributed to the signal.
	EdgeIDs []string `json:"edgeIds,omitempty" firestore:"edgeIds,omitempty"`

	// NeighborPaths are canonical paths of the most relevant neighbors.
	NeighborPaths []string `json:"neighborPaths,omitempty" firestore:"neighborPaths,omitempty"`

	// Note is an optional short explanation for UI/debug.
	Note string `json:"note,omitempty" firestore:"note,omitempty"`
}


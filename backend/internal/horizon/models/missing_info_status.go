package models

import "time"

// MissingInfoStatus tracks the status of missingInfo questions to prevent infinite research loops.
// Each record represents a specific question for a specific fragment.
type MissingInfoStatus struct {
	ID string `json:"id" firestore:"id"` // Deterministic: hash(fragmentID + questionHash)

	// Fragment and question identification
	FragmentID   string `json:"fragmentId" firestore:"fragmentId"`     // The fragment this question relates to
	QuestionHash string `json:"questionHash" firestore:"questionHash"` // Hash of the normalized question text

	// Human-readable (for debugging)
	QuestionText string `json:"questionText" firestore:"questionText"` // Original question text

	// Status tracking
	// Status values: "open" | "resolved" | "exhausted"
	// - open: question is pending research
	// - resolved: research found the answer
	// - exhausted: research failed after max attempts
	Status string `json:"status" firestore:"status"`

	// Attempt tracking
	Attempts       int        `json:"attempts" firestore:"attempts"`             // Number of research attempts
	LastAttemptAt  *time.Time `json:"lastAttemptAt" firestore:"lastAttemptAt"`   // When the last attempt was made
	NextAttemptAt  *time.Time `json:"nextAttemptAt" firestore:"nextAttemptAt"`   // Earliest time for next attempt (cooldown)
	ExhaustedAt    *time.Time `json:"exhaustedAt" firestore:"exhaustedAt"`       // When it was marked exhausted
	ResolvedAt     *time.Time `json:"resolvedAt" firestore:"resolvedAt"`         // When it was resolved

	// Fingerprint for cache invalidation
	// If the fragment state changes significantly (new sources), we reset the status.
	SourceFingerprint string `json:"sourceFingerprint" firestore:"sourceFingerprint"` // Hash of source state

	// Timestamps
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

// MissingInfoStatusOpen is the status for questions pending research.
const MissingInfoStatusOpen = "open"

// MissingInfoStatusResolved is the status for questions that were answered.
const MissingInfoStatusResolved = "resolved"

// MissingInfoStatusExhausted is the status for questions that failed after max attempts.
const MissingInfoStatusExhausted = "exhausted"

// DefaultMaxAttempts is the default number of attempts before marking as exhausted.
const DefaultMaxAttempts = 3

// DefaultCooldownHours is the default cooldown between attempts in hours.
const DefaultCooldownHours = 24

// DefaultExhaustedTTLDays is the default TTL for exhausted status in days.
// After this period, the status can be reset if sources change.
const DefaultExhaustedTTLDays = 30

// IsSearchable returns true if the question should be researched (open and not in cooldown).
func (s *MissingInfoStatus) IsSearchable() bool {
	if s.Status != MissingInfoStatusOpen {
		return false
	}
	if s.NextAttemptAt != nil && time.Now().Before(*s.NextAttemptAt) {
		return false // Still in cooldown
	}
	return true
}

// IsExhausted returns true if the question has been marked as exhausted.
func (s *MissingInfoStatus) IsExhausted() bool {
	return s.Status == MissingInfoStatusExhausted
}

// IsInCooldown returns true if the question is in cooldown period.
func (s *MissingInfoStatus) IsInCooldown() bool {
	if s.Status != MissingInfoStatusOpen {
		return false
	}
	return s.NextAttemptAt != nil && time.Now().Before(*s.NextAttemptAt)
}

// ShouldResetForNewSources returns true if the exhausted status should be reset
// due to new source information (fingerprint changed).
func (s *MissingInfoStatus) ShouldResetForNewSources(newFingerprint string) bool {
	if s.Status != MissingInfoStatusExhausted {
		return false
	}
	if newFingerprint == "" || s.SourceFingerprint == "" {
		return false
	}
	return newFingerprint != s.SourceFingerprint
}


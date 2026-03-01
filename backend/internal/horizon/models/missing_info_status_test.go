package models

import (
	"testing"
	"time"
)

func TestMissingInfoStatus_Constants(t *testing.T) {
	// Verify status constants are defined correctly
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{"open", MissingInfoStatusOpen, "open"},
		{"resolved", MissingInfoStatusResolved, "resolved"},
		{"exhausted", MissingInfoStatusExhausted, "exhausted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.status != tt.want {
				t.Errorf("MissingInfoStatus constant = %v, want %v", tt.status, tt.want)
			}
		})
	}
}

func TestMissingInfoStatus_DefaultConstants(t *testing.T) {
	if DefaultMaxAttempts != 3 {
		t.Errorf("DefaultMaxAttempts = %v, want 3", DefaultMaxAttempts)
	}
	if DefaultCooldownHours != 24 {
		t.Errorf("DefaultCooldownHours = %v, want 24", DefaultCooldownHours)
	}
	if DefaultExhaustedTTLDays != 30 {
		t.Errorf("DefaultExhaustedTTLDays = %v, want 30", DefaultExhaustedTTLDays)
	}
}

func TestMissingInfoStatus_StructFields(t *testing.T) {
	now := time.Now()
	nextAttempt := now.Add(24 * time.Hour)

	status := &MissingInfoStatus{
		ID:                "test-id-123",
		FragmentID:        "fragment-123",
		QuestionHash:      "hash456",
		QuestionText:      "What is the ownership percentage for OWNS relation between X and Y?",
		Status:            MissingInfoStatusOpen,
		Attempts:          2,
		LastAttemptAt:     &now,
		NextAttemptAt:     &nextAttempt,
		SourceFingerprint: "source-fp-789",
		CreatedAt:         now.Add(-48 * time.Hour),
		UpdatedAt:         now,
	}

	// Verify all fields are accessible and correctly set
	if status.ID == "" {
		t.Error("ID should not be empty")
	}
	if status.FragmentID != "fragment-123" {
		t.Errorf("FragmentID = %v, want fragment-123", status.FragmentID)
	}
	if status.QuestionHash != "hash456" {
		t.Errorf("QuestionHash = %v, want hash456", status.QuestionHash)
	}
	if status.QuestionText == "" {
		t.Error("QuestionText should not be empty")
	}
	if status.Status != MissingInfoStatusOpen {
		t.Errorf("Status = %v, want open", status.Status)
	}
	if status.Attempts != 2 {
		t.Errorf("Attempts = %v, want 2", status.Attempts)
	}
	if status.LastAttemptAt == nil {
		t.Error("LastAttemptAt should not be nil")
	}
	if status.NextAttemptAt == nil {
		t.Error("NextAttemptAt should not be nil")
	}
	if status.SourceFingerprint != "source-fp-789" {
		t.Errorf("SourceFingerprint = %v, want source-fp-789", status.SourceFingerprint)
	}
	if status.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if status.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestMissingInfoStatus_IsSearchable(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	tests := []struct {
		name string
		mis  MissingInfoStatus
		want bool
	}{
		{
			name: "open with no cooldown - searchable",
			mis: MissingInfoStatus{
				Status: MissingInfoStatusOpen,
			},
			want: true,
		},
		{
			name: "open with past NextAttemptAt - searchable",
			mis: MissingInfoStatus{
				Status:        MissingInfoStatusOpen,
				NextAttemptAt: &past,
			},
			want: true,
		},
		{
			name: "open with future NextAttemptAt - not searchable (cooldown)",
			mis: MissingInfoStatus{
				Status:        MissingInfoStatusOpen,
				NextAttemptAt: &future,
			},
			want: false,
		},
		{
			name: "resolved - not searchable",
			mis: MissingInfoStatus{
				Status: MissingInfoStatusResolved,
			},
			want: false,
		},
		{
			name: "exhausted - not searchable",
			mis: MissingInfoStatus{
				Status: MissingInfoStatusExhausted,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mis.IsSearchable(); got != tt.want {
				t.Errorf("IsSearchable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingInfoStatus_IsExhausted(t *testing.T) {
	tests := []struct {
		name string
		mis  MissingInfoStatus
		want bool
	}{
		{
			name: "exhausted status",
			mis:  MissingInfoStatus{Status: MissingInfoStatusExhausted},
			want: true,
		},
		{
			name: "open status",
			mis:  MissingInfoStatus{Status: MissingInfoStatusOpen},
			want: false,
		},
		{
			name: "resolved status",
			mis:  MissingInfoStatus{Status: MissingInfoStatusResolved},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mis.IsExhausted(); got != tt.want {
				t.Errorf("IsExhausted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingInfoStatus_IsInCooldown(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	tests := []struct {
		name string
		mis  MissingInfoStatus
		want bool
	}{
		{
			name: "open with future NextAttemptAt - in cooldown",
			mis: MissingInfoStatus{
				Status:        MissingInfoStatusOpen,
				NextAttemptAt: &future,
			},
			want: true,
		},
		{
			name: "open with past NextAttemptAt - not in cooldown",
			mis: MissingInfoStatus{
				Status:        MissingInfoStatusOpen,
				NextAttemptAt: &past,
			},
			want: false,
		},
		{
			name: "open with no NextAttemptAt - not in cooldown",
			mis: MissingInfoStatus{
				Status: MissingInfoStatusOpen,
			},
			want: false,
		},
		{
			name: "exhausted - not in cooldown",
			mis: MissingInfoStatus{
				Status:        MissingInfoStatusExhausted,
				NextAttemptAt: &future,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mis.IsInCooldown(); got != tt.want {
				t.Errorf("IsInCooldown() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingInfoStatus_ShouldResetForNewSources(t *testing.T) {
	tests := []struct {
		name           string
		mis            MissingInfoStatus
		newFingerprint string
		want           bool
	}{
		{
			name: "exhausted with different fingerprint - should reset",
			mis: MissingInfoStatus{
				Status:            MissingInfoStatusExhausted,
				SourceFingerprint: "old-fingerprint",
			},
			newFingerprint: "new-fingerprint",
			want:           true,
		},
		{
			name: "exhausted with same fingerprint - should not reset",
			mis: MissingInfoStatus{
				Status:            MissingInfoStatusExhausted,
				SourceFingerprint: "same-fingerprint",
			},
			newFingerprint: "same-fingerprint",
			want:           false,
		},
		{
			name: "open status - should not reset",
			mis: MissingInfoStatus{
				Status:            MissingInfoStatusOpen,
				SourceFingerprint: "old-fingerprint",
			},
			newFingerprint: "new-fingerprint",
			want:           false,
		},
		{
			name: "empty new fingerprint - should not reset",
			mis: MissingInfoStatus{
				Status:            MissingInfoStatusExhausted,
				SourceFingerprint: "old-fingerprint",
			},
			newFingerprint: "",
			want:           false,
		},
		{
			name: "empty old fingerprint - should reset",
			mis: MissingInfoStatus{
				Status:            MissingInfoStatusExhausted,
				SourceFingerprint: "",
			},
			newFingerprint: "new-fingerprint",
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mis.ShouldResetForNewSources(tt.newFingerprint); got != tt.want {
				t.Errorf("ShouldResetForNewSources(%q) = %v, want %v", tt.newFingerprint, got, tt.want)
			}
		})
	}
}

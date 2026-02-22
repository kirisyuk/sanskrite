package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// FragmentMergeAuditSource identifies the origin of a merge operation.
type FragmentMergeAuditSource string

const (
	MergeAuditSourceEnforcement   FragmentMergeAuditSource = "enforcement"
	MergeAuditSourceGraphIntent   FragmentMergeAuditSource = "graph_intent"
	MergeAuditSourceLensCombine   FragmentMergeAuditSource = "lens_combine"
	MergeAuditSourceToolDedup     FragmentMergeAuditSource = "tool_dedup"
	MergeAuditSourceToolMerge     FragmentMergeAuditSource = "tool_merge"
	MergeAuditSourceToolImport    FragmentMergeAuditSource = "tool_import"
)

// FragmentMergeAuditRecord stores an immutable record of a fragment merge for traceability.
// Collection: horizon_fragment_merge_audit
//
// Enables lookup: given sourceFragmentID or sourceCanonicalPath, find the target fragment
// that absorbed it. Every merge path (enforcement, graph intent, lens combine, CLI tools)
// must record here and update identifiers.merged_paths on the target fragment.
type FragmentMergeAuditRecord struct {
	// Source (duplicate) — the fragment that was merged into target and typically deleted
	SourceFragmentID   string `json:"sourceFragmentId" firestore:"sourceFragmentId"`
	SourceCanonicalPath string `json:"sourceCanonicalPath" firestore:"sourceCanonicalPath"`
	SourceTenantID      string `json:"sourceTenantId" firestore:"sourceTenantId"`

	// Target (primary) — the fragment that received the merge
	TargetFragmentID   string `json:"targetFragmentId" firestore:"targetFragmentId"`
	TargetCanonicalPath string `json:"targetCanonicalPath" firestore:"targetCanonicalPath"`
	TargetTenantID      string `json:"targetTenantId" firestore:"targetTenantId"`

	// Origin
	Source   FragmentMergeAuditSource `json:"source" firestore:"source"`
	ToolName string                  `json:"toolName,omitempty" firestore:"toolName,omitempty"` // e.g. horizon_smart_dedup, horizon_deduplicate_fragments
	JobID   string                  `json:"jobId,omitempty" firestore:"jobId,omitempty"`         // enforcement job, scouting job, etc.
	LensID  string                  `json:"lensId,omitempty" firestore:"lensId,omitempty"`       // for lens_combine / graph_intent

	Timestamp time.Time `json:"timestamp" firestore:"timestamp"`
}

// FragmentMergeAuditRepository writes merge audit records for traceability.
type FragmentMergeAuditRepository struct {
	col *firestore.CollectionRef
}

// NewFragmentMergeAuditRepository creates a new repository.
func NewFragmentMergeAuditRepository(db *firestore.Client) *FragmentMergeAuditRepository {
	if db == nil {
		return &FragmentMergeAuditRepository{}
	}
	return &FragmentMergeAuditRepository{
		col: db.Collection("horizon_fragment_merge_audit"),
	}
}

// Record appends an audit record. Best-effort: does not fail the merge if write fails.
func (r *FragmentMergeAuditRepository) Record(ctx context.Context, rec *FragmentMergeAuditRecord) error {
	if r == nil || r.col == nil {
		return nil
	}
	if rec == nil {
		return nil
	}
	rec.SourceFragmentID = strings.TrimSpace(rec.SourceFragmentID)
	rec.TargetFragmentID = strings.TrimSpace(rec.TargetFragmentID)
	if rec.SourceFragmentID == "" || rec.TargetFragmentID == "" {
		return nil
	}
	if rec.SourceFragmentID == rec.TargetFragmentID {
		return nil
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}
	_, err := r.col.NewDoc().Set(ctx, rec)
	return err
}

// RecordMerge is a convenience helper for recording a merge with minimal params.
func (r *FragmentMergeAuditRepository) RecordMerge(
	ctx context.Context,
	sourceFragmentID, sourceCanonicalPath, sourceTenantID string,
	targetFragmentID, targetCanonicalPath, targetTenantID string,
	source FragmentMergeAuditSource,
	toolName, jobID, lensID string,
) error {
	return r.Record(ctx, &FragmentMergeAuditRecord{
		SourceFragmentID:    sourceFragmentID,
		SourceCanonicalPath: sourceCanonicalPath,
		SourceTenantID:      strings.TrimSpace(sourceTenantID),
		TargetFragmentID:    targetFragmentID,
		TargetCanonicalPath: targetCanonicalPath,
		TargetTenantID:      strings.TrimSpace(targetTenantID),
		Source:              source,
		ToolName:            strings.TrimSpace(toolName),
		JobID:               strings.TrimSpace(jobID),
		LensID:              strings.TrimSpace(lensID),
		Timestamp:           time.Now().UTC(),
	})
}

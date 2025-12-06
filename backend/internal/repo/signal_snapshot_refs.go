package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// Create creates a new signal-snapshot reference
func (r *SignalSnapshotRefsRepository) Create(ctx context.Context, ref *models.SignalSnapshotRef) (string, error) {
	if ref == nil || ref.SignalID == "" || ref.SnapshotDocID == "" {
		return "", ErrInvalidDocument
	}

	now := time.Now().UTC()
	ref.CreatedAt = now
	ref.UpdatedAt = now
	if ref.CapturedAt.IsZero() {
		ref.CapturedAt = now
	}

	doc := r.col.NewDoc()
	ref.ID = doc.ID

	if _, err := doc.Set(ctx, ref); err != nil {
		return "", err
	}

	return doc.ID, nil
}

// Get retrieves a reference by ID
func (r *SignalSnapshotRefsRepository) Get(ctx context.Context, id string) (*models.SignalSnapshotRef, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}

	var ref models.SignalSnapshotRef
	if err := snap.DataTo(&ref); err != nil {
		return nil, err
	}

	return &ref, nil
}

// ListBySignal lists all snapshot references for a signal, ordered by capturedAt (newest first)
func (r *SignalSnapshotRefsRepository) ListBySignal(ctx context.Context, signalID string, limit int) ([]*models.SignalSnapshotRef, error) {
	if limit <= 0 {
		limit = 100
	}

	docs, err := r.col.Where("signalId", "==", signalID).
		OrderBy("capturedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	refs := make([]*models.SignalSnapshotRef, 0, len(docs))
	for _, doc := range docs {
		var ref models.SignalSnapshotRef
		if err := doc.DataTo(&ref); err != nil {
			continue
		}
		refs = append(refs, &ref)
	}

	return refs, nil
}

// GetLatestBySignal returns the most recent snapshot reference for a signal
func (r *SignalSnapshotRefsRepository) GetLatestBySignal(ctx context.Context, signalID string) (*models.SignalSnapshotRef, error) {
	refs, err := r.ListBySignal(ctx, signalID, 1)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, errors.New("no snapshots found for signal")
	}
	return refs[0], nil
}

// ListBySnapshotDoc lists all references for a specific snapshot document (if multiple signals share it)
func (r *SignalSnapshotRefsRepository) ListBySnapshotDoc(ctx context.Context, snapshotDocID string) ([]*models.SignalSnapshotRef, error) {
	docs, err := r.col.Where("snapshotDocId", "==", snapshotDocID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	refs := make([]*models.SignalSnapshotRef, 0, len(docs))
	for _, doc := range docs {
		var ref models.SignalSnapshotRef
		if err := doc.DataTo(&ref); err != nil {
			continue
		}
		refs = append(refs, &ref)
	}

	return refs, nil
}

// ListUnusedBySignal lists snapshots for a signal that haven't been used by any artifacts yet
func (r *SignalSnapshotRefsRepository) ListUnusedBySignal(ctx context.Context, signalID string, limit int) ([]*models.SignalSnapshotRef, error) {
	if limit <= 0 {
		limit = 100
	}

	// Get all snapshots for signal
	all, err := r.ListBySignal(ctx, signalID, limit*2) // Get more to filter
	if err != nil {
		return nil, err
	}

	// Filter unused ones
	unused := make([]*models.SignalSnapshotRef, 0)
	for _, ref := range all {
		if len(ref.UsedInArtifacts) == 0 {
			unused = append(unused, ref)
			if len(unused) >= limit {
				break
			}
		}
	}

	return unused, nil
}

// ListNewerThan lists snapshots for a signal that were captured after a specific snapshot
func (r *SignalSnapshotRefsRepository) ListNewerThan(ctx context.Context, signalID string, afterSnapshotRefID string) ([]*models.SignalSnapshotRef, error) {
	// Get the reference to find its capturedAt timestamp
	afterRef, err := r.Get(ctx, afterSnapshotRefID)
	if err != nil {
		return nil, err
	}

	// Get all snapshots for signal
	all, err := r.ListBySignal(ctx, signalID, 1000) // Get many to filter
	if err != nil {
		return nil, err
	}

	// Filter newer ones
	newer := make([]*models.SignalSnapshotRef, 0)
	for _, ref := range all {
		if ref.CapturedAt.After(afterRef.CapturedAt) {
			newer = append(newer, ref)
		}
	}

	return newer, nil
}

// MarkAsUsed marks a snapshot as used by an artifact
func (r *SignalSnapshotRefsRepository) MarkAsUsed(ctx context.Context, snapshotRefID string, artifactID string) error {
	if snapshotRefID == "" || artifactID == "" {
		return ErrInvalidDocument
	}

	ref, err := r.Get(ctx, snapshotRefID)
	if err != nil {
		return err
	}

	// Check if already marked
	for _, aid := range ref.UsedInArtifacts {
		if aid == artifactID {
			return nil // Already marked
		}
	}

	// Add artifact ID
	ref.UsedInArtifacts = append(ref.UsedInArtifacts, artifactID)
	ref.UpdatedAt = time.Now().UTC()

	_, err = r.col.Doc(snapshotRefID).Set(ctx, ref)
	return err
}

// MarkAsUnused removes artifact from used list (if needed)
func (r *SignalSnapshotRefsRepository) MarkAsUnused(ctx context.Context, snapshotRefID string, artifactID string) error {
	if snapshotRefID == "" || artifactID == "" {
		return ErrInvalidDocument
	}

	ref, err := r.Get(ctx, snapshotRefID)
	if err != nil {
		return err
	}

	// Remove artifact ID
	newUsed := make([]string, 0, len(ref.UsedInArtifacts))
	for _, aid := range ref.UsedInArtifacts {
		if aid != artifactID {
			newUsed = append(newUsed, aid)
		}
	}

	ref.UsedInArtifacts = newUsed
	ref.UpdatedAt = time.Now().UTC()

	_, err = r.col.Doc(snapshotRefID).Set(ctx, ref)
	return err
}

// Update updates a reference
func (r *SignalSnapshotRefsRepository) Update(ctx context.Context, ref *models.SignalSnapshotRef) error {
	if ref == nil || ref.ID == "" {
		return ErrInvalidDocument
	}

	ref.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(ref.ID).Set(ctx, ref)
	return err
}

// Delete deletes a reference
func (r *SignalSnapshotRefsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}

	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteBySignal deletes all references for a signal
func (r *SignalSnapshotRefsRepository) DeleteBySignal(ctx context.Context, signalID string) error {
	refs, err := r.ListBySignal(ctx, signalID, 10000) // Get all
	if err != nil {
		return err
	}

	for _, ref := range refs {
		if err := r.Delete(ctx, ref.ID); err != nil {
			return err
		}
	}

	return nil
}

// DeleteBySnapshotDoc deletes all references for a snapshot document
func (r *SignalSnapshotRefsRepository) DeleteBySnapshotDoc(ctx context.Context, snapshotDocID string) error {
	refs, err := r.ListBySnapshotDoc(ctx, snapshotDocID)
	if err != nil {
		return err
	}

	for _, ref := range refs {
		if err := r.Delete(ctx, ref.ID); err != nil {
			return err
		}
	}

	return nil
}


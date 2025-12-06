package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// Create creates a new signal history entry
func (r *SignalHistoryRepository) Create(ctx context.Context, history *models.SignalHistory) (string, error) {
	if history == nil || history.SignalID == "" || history.TenantID == "" {
		return "", ErrInvalidDocument
	}

	now := time.Now().UTC()
	if history.DetectedAt.IsZero() {
		history.DetectedAt = now
	}
	history.CreatedAt = now

	doc := r.col.NewDoc()
	history.ID = doc.ID

	if _, err := doc.Set(ctx, history); err != nil {
		return "", err
	}

	return doc.ID, nil
}

// Get retrieves a history entry by ID
func (r *SignalHistoryRepository) Get(ctx context.Context, id string) (*models.SignalHistory, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}

	var history models.SignalHistory
	if err := snap.DataTo(&history); err != nil {
		return nil, err
	}

	return &history, nil
}

// ListBySignal lists history entries for a specific signal, ordered by detection time (newest first)
func (r *SignalHistoryRepository) ListBySignal(ctx context.Context, signalID string, limit int) ([]*models.SignalHistory, error) {
	if limit <= 0 {
		limit = 100
	}

	docs, err := r.col.Where("signalId", "==", signalID).
		OrderBy("detectedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	history := make([]*models.SignalHistory, 0, len(docs))
	for _, doc := range docs {
		var entry models.SignalHistory
		if err := doc.DataTo(&entry); err != nil {
			continue
		}
		history = append(history, &entry)
	}

	return history, nil
}

// ListByTenant lists history entries for a tenant (optionally filtered by project)
func (r *SignalHistoryRepository) ListByTenant(ctx context.Context, tenantID string, projectID *string, limit int) ([]*models.SignalHistory, error) {
	if limit <= 0 {
		limit = 100
	}

	var q firestore.Query
	if projectID != nil && *projectID != "" {
		q = r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", *projectID)
	} else {
		q = r.col.Where("tenantId", "==", tenantID)
	}

	docs, err := q.OrderBy("detectedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	history := make([]*models.SignalHistory, 0, len(docs))
	for _, doc := range docs {
		var entry models.SignalHistory
		if err := doc.DataTo(&entry); err != nil {
			continue
		}
		history = append(history, &entry)
	}

	return history, nil
}

// ListByArtifact lists history entries for signals linked to a specific artifact
func (r *SignalHistoryRepository) ListByArtifact(ctx context.Context, artifactID string, limit int, linkRepo *DocumentArtifactSignalsRepository) ([]*models.SignalHistory, error) {
	if limit <= 0 {
		limit = 100
	}

	// Get all signals linked to this artifact
	links, err := linkRepo.ListByArtifact(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	if len(links) == 0 {
		return []*models.SignalHistory{}, nil
	}

	// Collect signal IDs
	signalIDs := make([]string, 0, len(links))
	for _, link := range links {
		signalIDs = append(signalIDs, link.SignalID)
	}

	// Query history for these signals
	// Note: Firestore doesn't support "IN" queries with more than 10 items efficiently
	// For now, we'll query each signal separately and merge results
	allHistory := make([]*models.SignalHistory, 0)
	seen := make(map[string]bool)

	for _, signalID := range signalIDs {
		history, err := r.ListBySignal(ctx, signalID, limit)
		if err != nil {
			continue
		}
		for _, entry := range history {
			if !seen[entry.ID] {
				seen[entry.ID] = true
				allHistory = append(allHistory, entry)
			}
		}
	}

	// Sort by detectedAt desc
	for i := 0; i < len(allHistory)-1; i++ {
		for j := i + 1; j < len(allHistory); j++ {
			if allHistory[i].DetectedAt.Before(allHistory[j].DetectedAt) {
				allHistory[i], allHistory[j] = allHistory[j], allHistory[i]
			}
		}
	}

	// Limit results
	if len(allHistory) > limit {
		allHistory = allHistory[:limit]
	}

	return allHistory, nil
}

// CountBySignal counts history entries for a signal
func (r *SignalHistoryRepository) CountBySignal(ctx context.Context, signalID string) (int, error) {
	docs, err := r.col.Where("signalId", "==", signalID).Documents(ctx).GetAll()
	if err != nil {
		return 0, err
	}
	return len(docs), nil
}

// Delete deletes a history entry
func (r *SignalHistoryRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}

	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteOldEntries deletes history entries older than specified duration
func (r *SignalHistoryRepository) DeleteOldEntries(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().UTC().Add(-olderThan)
	
	// Note: Firestore doesn't support efficient range queries on timestamp
	// This is a simplified implementation - in production, consider using
	// a scheduled job or Cloud Functions to clean up old entries
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	deleted := 0
	for _, doc := range docs {
		var entry models.SignalHistory
		if err := doc.DataTo(&entry); err != nil {
			continue
		}
		if entry.DetectedAt.Before(cutoff) {
			if err := r.Delete(ctx, entry.ID); err != nil {
				continue
			}
			deleted++
		}
	}

	return nil
}


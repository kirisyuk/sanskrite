package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// Create creates a new document-artifact-signal relationship
func (r *DocumentArtifactSignalsRepository) Create(ctx context.Context, link *models.DocumentArtifactSignal) (string, error) {
	if link == nil || link.DocumentArtifactID == "" || link.SignalID == "" {
		return "", ErrInvalidDocument
	}

	now := time.Now().UTC()
	link.CreatedAt = now
	link.UpdatedAt = now

	doc := r.col.NewDoc()
	link.ID = doc.ID

	if _, err := doc.Set(ctx, link); err != nil {
		return "", err
	}

	return doc.ID, nil
}

// Get retrieves a link by ID
func (r *DocumentArtifactSignalsRepository) Get(ctx context.Context, id string) (*models.DocumentArtifactSignal, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}

	var link models.DocumentArtifactSignal
	if err := snap.DataTo(&link); err != nil {
		return nil, err
	}

	return &link, nil
}

// Upsert creates or updates a link
func (r *DocumentArtifactSignalsRepository) Upsert(ctx context.Context, link *models.DocumentArtifactSignal) error {
	if link == nil || link.DocumentArtifactID == "" || link.SignalID == "" {
		return ErrInvalidDocument
	}

	now := time.Now().UTC()
	if link.CreatedAt.IsZero() {
		link.CreatedAt = now
	}
	link.UpdatedAt = now

	if link.ID == "" {
		doc := r.col.NewDoc()
		link.ID = doc.ID
	}

	_, err := r.col.Doc(link.ID).Set(ctx, link)
	return err
}

// ListByArtifact lists all signals linked to a document-artifact
func (r *DocumentArtifactSignalsRepository) ListByArtifact(ctx context.Context, artifactID string) ([]*models.DocumentArtifactSignal, error) {
	docs, err := r.col.Where("documentArtifactId", "==", artifactID).
		OrderBy("priority", firestore.Desc).
		OrderBy("createdAt", firestore.Desc).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	links := make([]*models.DocumentArtifactSignal, 0, len(docs))
	for _, doc := range docs {
		var link models.DocumentArtifactSignal
		if err := doc.DataTo(&link); err != nil {
			continue
		}
		links = append(links, &link)
	}

	return links, nil
}

// ListBySignal lists all artifacts linked to a signal
func (r *DocumentArtifactSignalsRepository) ListBySignal(ctx context.Context, signalID string) ([]*models.DocumentArtifactSignal, error) {
	docs, err := r.col.Where("signalId", "==", signalID).
		OrderBy("priority", firestore.Desc).
		OrderBy("createdAt", firestore.Desc).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	links := make([]*models.DocumentArtifactSignal, 0, len(docs))
	for _, doc := range docs {
		var link models.DocumentArtifactSignal
		if err := doc.DataTo(&link); err != nil {
			continue
		}
		links = append(links, &link)
	}

	return links, nil
}

// ListActiveByArtifact lists only active signals for an artifact
func (r *DocumentArtifactSignalsRepository) ListActiveByArtifact(ctx context.Context, artifactID string) ([]*models.DocumentArtifactSignal, error) {
	all, err := r.ListByArtifact(ctx, artifactID)
	if err != nil {
		return nil, err
	}

	active := make([]*models.DocumentArtifactSignal, 0)
	for _, link := range all {
		if link.IsActive {
			active = append(active, link)
		}
	}

	return active, nil
}

// FindLink finds a link between artifact and signal (if exists)
func (r *DocumentArtifactSignalsRepository) FindLink(ctx context.Context, artifactID, signalID string) (*models.DocumentArtifactSignal, error) {
	docs, err := r.col.Where("documentArtifactId", "==", artifactID).
		Where("signalId", "==", signalID).
		Limit(1).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, errors.New("link not found")
	}

	var link models.DocumentArtifactSignal
	if err := docs[0].DataTo(&link); err != nil {
		return nil, err
	}

	return &link, nil
}

// Update updates an existing link
func (r *DocumentArtifactSignalsRepository) Update(ctx context.Context, link *models.DocumentArtifactSignal) error {
	if link == nil || link.ID == "" {
		return ErrInvalidDocument
	}

	link.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(link.ID).Set(ctx, link)
	return err
}

// Delete deletes a link
func (r *DocumentArtifactSignalsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}

	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteByArtifact deletes all links for an artifact
func (r *DocumentArtifactSignalsRepository) DeleteByArtifact(ctx context.Context, artifactID string) error {
	links, err := r.ListByArtifact(ctx, artifactID)
	if err != nil {
		return err
	}

	for _, link := range links {
		if err := r.Delete(ctx, link.ID); err != nil {
			return err
		}
	}

	return nil
}

// DeleteBySignal deletes all links for a signal
func (r *DocumentArtifactSignalsRepository) DeleteBySignal(ctx context.Context, signalID string) error {
	links, err := r.ListBySignal(ctx, signalID)
	if err != nil {
		return err
	}

	for _, link := range links {
		if err := r.Delete(ctx, link.ID); err != nil {
			return err
		}
	}

	return nil
}


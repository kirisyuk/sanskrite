package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

var ErrInvalidTemplate = errors.New("invalid template")

// ArtifactTemplatesRepository manages artifact templates in Firestore
type ArtifactTemplatesRepository struct {
	col *firestore.CollectionRef
}

// NewArtifactTemplatesRepository creates a new repository instance
func NewArtifactTemplatesRepository(db *firestore.Client) *ArtifactTemplatesRepository {
	return &ArtifactTemplatesRepository{col: db.Collection("artifact_templates")}
}

// Create creates a new custom template
func (r *ArtifactTemplatesRepository) Create(ctx context.Context, t *models.ArtifactTemplate) (string, error) {
	if t == nil || t.Name == "" || t.CreatedBy == "" {
		return "", ErrInvalidTemplate
	}

	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now

	doc := r.col.NewDoc()
	t.ID = doc.ID

	if _, err := doc.Set(ctx, t); err != nil {
		return "", err
	}

	return doc.ID, nil
}

// Get retrieves a template by ID
func (r *ArtifactTemplatesRepository) Get(ctx context.Context, id string) (*models.ArtifactTemplate, error) {
	if id == "" {
		return nil, ErrInvalidTemplate
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}

	var t models.ArtifactTemplate
	if err := snap.DataTo(&t); err != nil {
		return nil, err
	}

	return &t, nil
}

// ListPublic returns public (community) templates
func (r *ArtifactTemplatesRepository) ListPublic(ctx context.Context, category *string, limit int) ([]*models.ArtifactTemplate, error) {
	q := r.col.Where("isPublic", "==", true)

	if category != nil {
		q = q.Where("category", "==", *category)
	}

	q = q.OrderBy("usageCount", firestore.Desc)

	if limit > 0 {
		q = q.Limit(limit)
	}

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	templates := make([]*models.ArtifactTemplate, 0, len(docs))
	for _, d := range docs {
		var t models.ArtifactTemplate
		if err := d.DataTo(&t); err != nil {
			continue
		}
		templates = append(templates, &t)
	}

	return templates, nil
}

// ListByTenant returns templates for a specific tenant
func (r *ArtifactTemplatesRepository) ListByTenant(ctx context.Context, tenantID string) ([]*models.ArtifactTemplate, error) {
	q := r.col.Where("tenantId", "==", tenantID).OrderBy("createdAt", firestore.Desc)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	templates := make([]*models.ArtifactTemplate, 0, len(docs))
	for _, d := range docs {
		var t models.ArtifactTemplate
		if err := d.DataTo(&t); err != nil {
			continue
		}
		templates = append(templates, &t)
	}

	return templates, nil
}

// Update updates an existing template
func (r *ArtifactTemplatesRepository) Update(ctx context.Context, t *models.ArtifactTemplate) error {
	if t == nil || t.ID == "" {
		return ErrInvalidTemplate
	}

	t.UpdatedAt = time.Now().UTC()

	_, err := r.col.Doc(t.ID).Set(ctx, t)
	return err
}

// Delete deletes a template
func (r *ArtifactTemplatesRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidTemplate
	}

	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// IncrementUsage increments the usage counter for a template
func (r *ArtifactTemplatesRepository) IncrementUsage(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidTemplate
	}

	_, err := r.col.Doc(id).Update(ctx, []firestore.Update{
		{Path: "usageCount", Value: firestore.Increment(1)},
		{Path: "updatedAt", Value: time.Now().UTC()},
	})

	return err
}

// UpdateRating updates the rating for a template
func (r *ArtifactTemplatesRepository) UpdateRating(ctx context.Context, id string, newRating float64) error {
	if id == "" {
		return ErrInvalidTemplate
	}

	// Get current template to calculate new average
	template, err := r.Get(ctx, id)
	if err != nil {
		return err
	}

	newRatingCount := template.RatingCount + 1
	newAvgRating := ((template.Rating * float64(template.RatingCount)) + newRating) / float64(newRatingCount)

	_, err = r.col.Doc(id).Update(ctx, []firestore.Update{
		{Path: "rating", Value: newAvgRating},
		{Path: "ratingCount", Value: newRatingCount},
		{Path: "updatedAt", Value: time.Now().UTC()},
	})

	return err
}

// ListFeatured returns featured templates
func (r *ArtifactTemplatesRepository) ListFeatured(ctx context.Context, limit int) ([]*models.ArtifactTemplate, error) {
	q := r.col.Where("isFeatured", "==", true).OrderBy("usageCount", firestore.Desc)

	if limit > 0 {
		q = q.Limit(limit)
	}

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	templates := make([]*models.ArtifactTemplate, 0, len(docs))
	for _, d := range docs {
		var t models.ArtifactTemplate
		if err := d.DataTo(&t); err != nil {
			continue
		}
		templates = append(templates, &t)
	}

	return templates, nil
}

// SearchByTags searches templates by tags
func (r *ArtifactTemplatesRepository) SearchByTags(ctx context.Context, tags []string, limit int) ([]*models.ArtifactTemplate, error) {
	if len(tags) == 0 {
		return []*models.ArtifactTemplate{}, nil
	}

	// Firestore array-contains only works with single value, so we search for first tag
	// In production, you might want to use a more sophisticated search solution
	q := r.col.Where("tags", "array-contains", tags[0]).OrderBy("usageCount", firestore.Desc)

	if limit > 0 {
		q = q.Limit(limit)
	}

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	templates := make([]*models.ArtifactTemplate, 0, len(docs))
	for _, d := range docs {
		var t models.ArtifactTemplate
		if err := d.DataTo(&t); err != nil {
			continue
		}

		// Filter by additional tags in application layer
		if len(tags) > 1 {
			hasAllTags := true
			for _, tag := range tags[1:] {
				found := false
				for _, tTag := range t.Tags {
					if tTag == tag {
						found = true
						break
					}
				}
				if !found {
					hasAllTags = false
					break
				}
			}
			if !hasAllTags {
				continue
			}
		}

		templates = append(templates, &t)
	}

	return templates, nil
}

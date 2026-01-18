package repo

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrPropagationProfileNotFound = errors.New("propagation profile not found")

// PropagationProfilesRepository manages propagation profiles in Firestore.
type PropagationProfilesRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

// NewPropagationProfilesRepository creates a new PropagationProfilesRepository.
func NewPropagationProfilesRepository(client *firestore.Client) *PropagationProfilesRepository {
	return &PropagationProfilesRepository{
		col: client.Collection("horizon_propagation_profiles"),
		db:  client,
	}
}

// Get retrieves a propagation profile by axis key.
func (r *PropagationProfilesRepository) Get(ctx context.Context, axisKey string) (*models.PropagationProfile, error) {
	if axisKey == "" {
		return nil, ErrInvalidDocument
	}

	// Try to get from Firestore first
	snap, err := r.col.Doc(axisKey).Get(ctx)
	if err == nil && snap.Exists() {
		var profile models.PropagationProfile
		if err := snap.DataTo(&profile); err != nil {
			return nil, fmt.Errorf("failed to unmarshal profile: %w", err)
		}
		return &profile, nil
	}

	// Fallback to default profiles
	defaultProfiles := models.DefaultPropagationProfiles()
	if profile, ok := defaultProfiles[axisKey]; ok {
		return &profile, nil
	}

	return nil, ErrPropagationProfileNotFound
}

// GetAll retrieves all propagation profiles.
func (r *PropagationProfilesRepository) GetAll(ctx context.Context) (map[string]*models.PropagationProfile, error) {
	profiles := make(map[string]*models.PropagationProfile)

	// Load defaults first
	defaultProfiles := models.DefaultPropagationProfiles()
	for key, profile := range defaultProfiles {
		profiles[key] = &profile
	}

	// Override with Firestore profiles if they exist
	docs, err := r.col.Documents(ctx).GetAll()
	if err == nil {
		for _, doc := range docs {
			var profile models.PropagationProfile
			if err := doc.DataTo(&profile); err != nil {
				continue
			}
			profiles[profile.AxisKey] = &profile
		}
	}

	return profiles, nil
}

// Set creates or updates a propagation profile.
func (r *PropagationProfilesRepository) Set(ctx context.Context, profile *models.PropagationProfile) error {
	if profile == nil {
		return ErrInvalidDocument
	}
	if profile.AxisKey == "" {
		return fmt.Errorf("axisKey is required")
	}

	_, err := r.col.Doc(profile.AxisKey).Set(ctx, profile)
	return err
}

// SeedDefaultProfiles seeds default propagation profiles into Firestore.
// This should be called during initialization to ensure all profiles exist.
func (r *PropagationProfilesRepository) SeedDefaultProfiles(ctx context.Context) error {
	defaultProfiles := models.DefaultPropagationProfiles()
	batch := r.db.Batch()
	batchCount := 0
	const maxBatchSize = 500

	for _, profile := range defaultProfiles {
		ref := r.col.Doc(profile.AxisKey)
		batch.Set(ref, profile)
		batchCount++

		if batchCount >= maxBatchSize {
			if _, err := batch.Commit(ctx); err != nil {
				return fmt.Errorf("failed to commit batch: %w", err)
			}
			batch = r.db.Batch()
			batchCount = 0
		}
	}

	if batchCount > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit final batch: %w", err)
		}
	}

	return nil
}

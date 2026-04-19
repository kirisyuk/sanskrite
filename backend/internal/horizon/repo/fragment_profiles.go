package repo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

var ErrFragmentProfileNotFound = errors.New("fragment profile not found")

// FragmentProfilesRepository manages FragmentProfile documents in Firestore.
type FragmentProfilesRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

// NewFragmentProfilesRepository creates a new FragmentProfilesRepository.
func NewFragmentProfilesRepository(client *firestore.Client) *FragmentProfilesRepository {
	return &FragmentProfilesRepository{
		col: client.Collection("horizon_fragment_profiles"),
		db:  client,
	}
}

// Create creates a new FragmentProfile.
func (r *FragmentProfilesRepository) Create(ctx context.Context, profile *models.FragmentProfile) (string, error) {
	if profile == nil {
		return "", ErrInvalidDocument
	}
	if err := profile.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	profile.CreatedAt = now
	profile.UpdatedAt = now
	if profile.CapturedAt.IsZero() {
		profile.CapturedAt = now
	}
	if profile.Version == 0 {
		profile.Version = 1
	}
	if profile.Status == "" {
		profile.Status = models.ProfileStatusActive
	}

	doc := r.col.NewDoc()
	profile.ID = doc.ID

	if _, err := doc.Set(ctx, profile); err != nil {
		return "", err
	}

	return profile.ID, nil
}

// Get retrieves a FragmentProfile by ID.
func (r *FragmentProfilesRepository) Get(ctx context.Context, id string) (*models.FragmentProfile, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrFragmentProfileNotFound
		}
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrFragmentProfileNotFound
	}

	var profile models.FragmentProfile
	if err := snap.DataTo(&profile); err != nil {
		return nil, err
	}

	return &profile, nil
}

// GetByFragmentID retrieves the latest profile for a fragment.
func (r *FragmentProfilesRepository) GetByFragmentID(ctx context.Context, fragmentID string) (*models.FragmentProfile, error) {
	if strings.TrimSpace(fragmentID) == "" {
		return nil, ErrInvalidDocument
	}

	// NOTE: Prefer reliability over requiring Firestore composite indexes.
	// Even Where(fragmentId==) + OrderBy(capturedAt) may require a composite index.
	// We query by fragmentId only and select the latest ACTIVE profile in-memory.
	docs, err := r.col.
		Where("fragmentId", "==", fragmentID).
		Limit(50).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrFragmentProfileNotFound
	}

	var best *models.FragmentProfile
	for _, doc := range docs {
		var profile models.FragmentProfile
		if err := doc.DataTo(&profile); err != nil {
			continue
		}
		if !strings.EqualFold(string(profile.Status), string(models.ProfileStatusActive)) {
			continue
		}
		if best == nil || profile.CapturedAt.After(best.CapturedAt) {
			p := profile
			best = &p
		}
	}

	if best == nil {
		return nil, ErrFragmentProfileNotFound
	}
	return best, nil
}

// GetByCanonicalPath retrieves the latest profile by canonical path.
func (r *FragmentProfilesRepository) GetByCanonicalPath(ctx context.Context, canonicalPath string) (*models.FragmentProfile, error) {
	if strings.TrimSpace(canonicalPath) == "" {
		return nil, ErrInvalidDocument
	}

	// Same rationale as GetByFragmentID: avoid composite index requirements.
	docs, err := r.col.
		Where("canonicalPath", "==", canonicalPath).
		Limit(50).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrFragmentProfileNotFound
	}

	var best *models.FragmentProfile
	for _, doc := range docs {
		var profile models.FragmentProfile
		if err := doc.DataTo(&profile); err != nil {
			continue
		}
		if !strings.EqualFold(string(profile.Status), string(models.ProfileStatusActive)) {
			continue
		}
		if best == nil || profile.CapturedAt.After(best.CapturedAt) {
			p := profile
			best = &p
		}
	}

	if best == nil {
		return nil, ErrFragmentProfileNotFound
	}
	return best, nil
}

// ListByFragmentID retrieves all profile versions for a fragment, ordered by capturedAt desc.
func (r *FragmentProfilesRepository) ListByFragmentID(ctx context.Context, fragmentID string, limit int) ([]*models.FragmentProfile, error) {
	if strings.TrimSpace(fragmentID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 10
	}

	// IMPORTANT:
	// Avoid composite index requirements by NOT mixing Where(fragmentId==...) with OrderBy(capturedAt).
	// We fetch up to `limit` docs and sort in-memory by CapturedAt desc.
	docs, err := r.col.
		Where("fragmentId", "==", fragmentID).
		Limit(limit).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	profiles := make([]*models.FragmentProfile, 0, len(docs))
	for _, doc := range docs {
		var profile models.FragmentProfile
		if err := doc.DataTo(&profile); err != nil {
			continue
		}
		profiles = append(profiles, &profile)
	}

	// Sort by capturedAt desc (newest first)
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i] == nil || profiles[j] == nil {
			return false
		}
		return profiles[i].CapturedAt.After(profiles[j].CapturedAt)
	})

	return profiles, nil
}

// ListStale retrieves profiles older than the given time, for refresh scheduling.
func (r *FragmentProfilesRepository) ListStale(ctx context.Context, olderThan time.Time, fragmentType ah.FragmentClass, limit int) ([]*models.FragmentProfile, error) {
	if limit <= 0 {
		limit = 100
	}

	q := r.col.
		Where("status", "==", string(models.ProfileStatusActive)).
		Where("capturedAt", "<", olderThan).
		OrderBy("capturedAt", firestore.Asc).
		Limit(limit)

	if fragmentType != "" {
		q = r.col.
			Where("fragmentClass", "==", string(fragmentType)).
			Where("status", "==", string(models.ProfileStatusActive)).
			Where("capturedAt", "<", olderThan).
			OrderBy("capturedAt", firestore.Asc).
			Limit(limit)
	}

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	profiles := make([]*models.FragmentProfile, 0, len(docs))
	for _, doc := range docs {
		var profile models.FragmentProfile
		if err := doc.DataTo(&profile); err != nil {
			continue
		}
		profiles = append(profiles, &profile)
	}

	return profiles, nil
}

// Update updates an existing FragmentProfile.
func (r *FragmentProfilesRepository) Update(ctx context.Context, profile *models.FragmentProfile) error {
	if profile == nil || strings.TrimSpace(profile.ID) == "" {
		return ErrInvalidDocument
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	profile.UpdatedAt = time.Now().UTC()

	_, err := r.col.Doc(profile.ID).Set(ctx, profile)
	return err
}

// UpdateStatus updates only the status of a profile.
func (r *FragmentProfilesRepository) UpdateStatus(ctx context.Context, id string, status models.ProfileStatus) error {
	if strings.TrimSpace(id) == "" {
		return ErrInvalidDocument
	}

	now := time.Now().UTC()
	_, err := r.col.Doc(id).Update(ctx, []firestore.Update{
		{Path: "status", Value: string(status)},
		{Path: "updatedAt", Value: now},
	})
	return err
}

// MarkStale marks a profile as stale.
func (r *FragmentProfilesRepository) MarkStale(ctx context.Context, id string) error {
	return r.UpdateStatus(ctx, id, models.ProfileStatusStale)
}

// Delete deletes a FragmentProfile.
func (r *FragmentProfilesRepository) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return ErrInvalidDocument
	}

	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// DeleteByFragmentID deletes all profiles for a fragment.
func (r *FragmentProfilesRepository) DeleteByFragmentID(ctx context.Context, fragmentID string) error {
	if strings.TrimSpace(fragmentID) == "" {
		return ErrInvalidDocument
	}

	// Avoid composite index requirements: fetch by fragmentId only (no OrderBy),
	// then delete each profile doc.
	docs, err := r.col.
		Where("fragmentId", "==", fragmentID).
		Limit(1000).
		Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, doc := range docs {
		if doc == nil || doc.Ref == nil {
			continue
		}
		_, err := doc.Ref.Delete(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

// CountByType counts profiles by fragment type.
func (r *FragmentProfilesRepository) CountByType(ctx context.Context, fragmentType ah.FragmentClass) (int, error) {
	q := r.col.Where("fragmentClass", "==", string(fragmentType))
	if fragmentType == "" {
		q = r.col.Query
	}

	// Firestore doesn't have COUNT, so we need to fetch docs
	// For large collections, consider using a counter document or BigQuery
	docs, err := q.Select().Documents(ctx).GetAll()
	if err != nil {
		return 0, err
	}

	return len(docs), nil
}

// ExistsForFragment checks if a profile exists for a fragment.
func (r *FragmentProfilesRepository) ExistsForFragment(ctx context.Context, fragmentID string) (bool, error) {
	if strings.TrimSpace(fragmentID) == "" {
		return false, ErrInvalidDocument
	}

	docs, err := r.col.
		Where("fragmentId", "==", fragmentID).
		Where("status", "==", string(models.ProfileStatusActive)).
		Limit(1).
		Select().
		Documents(ctx).GetAll()
	if err != nil {
		return false, err
	}

	return len(docs) > 0, nil
}

// GetLatestCapturedAt returns the most recent capturedAt for a fragment type.
// Useful for scheduler to determine when last refresh happened.
func (r *FragmentProfilesRepository) GetLatestCapturedAt(ctx context.Context, fragmentType ah.FragmentClass) (*time.Time, error) {
	q := r.col.
		Where("fragmentClass", "==", string(fragmentType)).
		Where("status", "==", string(models.ProfileStatusActive)).
		OrderBy("capturedAt", firestore.Desc).
		Limit(1)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}

	var profile models.FragmentProfile
	if err := docs[0].DataTo(&profile); err != nil {
		return nil, err
	}

	return &profile.CapturedAt, nil
}

// FragmentProfileRefreshPoliciesRepository manages refresh policies.
type FragmentProfileRefreshPoliciesRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

// NewFragmentProfileRefreshPoliciesRepository creates a new repository for refresh policies.
func NewFragmentProfileRefreshPoliciesRepository(client *firestore.Client) *FragmentProfileRefreshPoliciesRepository {
	return &FragmentProfileRefreshPoliciesRepository{
		col: client.Collection("horizon_fragment_profile_policies"),
		db:  client,
	}
}

// Get retrieves a policy by ID.
func (r *FragmentProfileRefreshPoliciesRepository) Get(ctx context.Context, id string) (*models.FragmentProfileRefreshPolicy, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, fmt.Errorf("policy not found: %s", id)
	}

	var policy models.FragmentProfileRefreshPolicy
	if err := snap.DataTo(&policy); err != nil {
		return nil, err
	}

	return &policy, nil
}

// GetByType retrieves the policy for a fragment type.
func (r *FragmentProfileRefreshPoliciesRepository) GetByType(ctx context.Context, fragmentType ah.FragmentClass) (*models.FragmentProfileRefreshPolicy, error) {
	docs, err := r.col.
		Where("fragmentClass", "==", string(fragmentType)).
		Where("enabled", "==", true).
		Limit(1).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		// Return default policy
		defaults := models.DefaultRefreshPolicies()
		for _, p := range defaults {
			if p.FragmentClass == fragmentType {
				return &p, nil
			}
		}
		return nil, fmt.Errorf("no policy found for type: %s", fragmentType)
	}

	var policy models.FragmentProfileRefreshPolicy
	if err := docs[0].DataTo(&policy); err != nil {
		return nil, err
	}

	return &policy, nil
}

// List retrieves all policies.
func (r *FragmentProfileRefreshPoliciesRepository) List(ctx context.Context) ([]*models.FragmentProfileRefreshPolicy, error) {
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	policies := make([]*models.FragmentProfileRefreshPolicy, 0, len(docs))
	for _, doc := range docs {
		var policy models.FragmentProfileRefreshPolicy
		if err := doc.DataTo(&policy); err != nil {
			continue
		}
		policies = append(policies, &policy)
	}

	return policies, nil
}

// Upsert creates or updates a policy.
func (r *FragmentProfileRefreshPoliciesRepository) Upsert(ctx context.Context, policy *models.FragmentProfileRefreshPolicy) error {
	if policy == nil || strings.TrimSpace(policy.ID) == "" {
		return ErrInvalidDocument
	}

	policy.UpdatedAt = time.Now().UTC()

	_, err := r.col.Doc(policy.ID).Set(ctx, policy)
	return err
}

// SeedDefaults seeds the default policies if they don't exist.
func (r *FragmentProfileRefreshPoliciesRepository) SeedDefaults(ctx context.Context) error {
	defaults := models.DefaultRefreshPolicies()
	for _, policy := range defaults {
		// Check if exists
		_, err := r.col.Doc(policy.ID).Get(ctx)
		if err == nil {
			// Already exists, skip
			continue
		}
		// Create
		if _, err := r.col.Doc(policy.ID).Set(ctx, policy); err != nil {
			return fmt.Errorf("failed to seed policy %s: %w", policy.ID, err)
		}
	}
	return nil
}

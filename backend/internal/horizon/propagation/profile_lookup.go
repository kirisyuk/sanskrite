package propagation

import (
	"context"
	"fmt"
	"sync"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// ProfileLookup provides cached access to propagation profiles.
type ProfileLookup struct {
	profilesRepo *repo.PropagationProfilesRepository
	cache        map[string]*horizonmodels.PropagationProfile
	mu           sync.RWMutex
	initialized  bool
}

// NewProfileLookup creates a new ProfileLookup.
func NewProfileLookup(profilesRepo *repo.PropagationProfilesRepository) *ProfileLookup {
	return &ProfileLookup{
		profilesRepo: profilesRepo,
		cache:        make(map[string]*horizonmodels.PropagationProfile),
		initialized:  false,
	}
}

// Initialize loads all propagation profiles from Firestore into cache.
// Should be called at startup.
func (p *ProfileLookup) Initialize(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}

	// Load all profiles from Firestore
	profiles, err := p.profilesRepo.GetAll(ctx)
	if err != nil {
		// Fallback to defaults if Firestore fails
		defaultProfiles := horizonmodels.DefaultPropagationProfiles()
		for key, profile := range defaultProfiles {
			p.cache[key] = &profile
		}
		p.initialized = true
		return fmt.Errorf("failed to load profiles from Firestore, using defaults: %w", err)
	}

	// Use Firestore profiles (they override defaults)
	for key, profile := range profiles {
		p.cache[key] = profile
	}

	// Ensure all default profiles exist (fill gaps)
	defaultProfiles := horizonmodels.DefaultPropagationProfiles()
	for key, profile := range defaultProfiles {
		if _, exists := p.cache[key]; !exists {
			p.cache[key] = &profile
		}
	}

	p.initialized = true
	return nil
}

// Get retrieves a propagation profile by axis key.
// Returns default profile if axis not found in cache.
func (p *ProfileLookup) Get(axisKey string) (*horizonmodels.PropagationProfile, error) {
	if !horizonmodels.IsValidAxis(axisKey) {
		return nil, fmt.Errorf("invalid axis: %s", axisKey)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	// Check cache first
	if profile, ok := p.cache[axisKey]; ok {
		return profile, nil
	}

	// Fallback to default
	defaultProfiles := horizonmodels.DefaultPropagationProfiles()
	if profile, ok := defaultProfiles[axisKey]; ok {
		return &profile, nil
	}

	return nil, fmt.Errorf("profile not found for axis: %s", axisKey)
}

// Refresh reloads a specific profile from Firestore.
func (p *ProfileLookup) Refresh(ctx context.Context, axisKey string) error {
	if !horizonmodels.IsValidAxis(axisKey) {
		return fmt.Errorf("invalid axis: %s", axisKey)
	}

	profile, err := p.profilesRepo.Get(ctx, axisKey)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[axisKey] = profile

	return nil
}

// RefreshAll reloads all profiles from Firestore.
func (p *ProfileLookup) RefreshAll(ctx context.Context) error {
	return p.Initialize(ctx)
}

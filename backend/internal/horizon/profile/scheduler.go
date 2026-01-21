package profile

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tasks"
)

// Scheduler handles scheduled profile refresh operations.
type Scheduler struct {
	generator    *Generator
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo    *repo.DependencyEdgesRepository
	profilesRepo *repo.FragmentProfilesRepository
	policiesRepo *repo.FragmentProfileRefreshPoliciesRepository
	tasksDispatcher *tasks.ProfileCloudTasksDispatcher
}

// NewScheduler creates a new profile refresh Scheduler.
func NewScheduler(
	generator *Generator,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	profilesRepo *repo.FragmentProfilesRepository,
	policiesRepo *repo.FragmentProfileRefreshPoliciesRepository,
	tasksDispatcher *tasks.ProfileCloudTasksDispatcher,
) *Scheduler {
	return &Scheduler{
		generator:     generator,
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		profilesRepo:  profilesRepo,
		policiesRepo:  policiesRepo,
		tasksDispatcher: tasksDispatcher,
	}
}

// RefreshResult contains the result of a scheduled refresh run.
type RefreshResult struct {
	FragmentType    models.FragmentType
	CandidatesFound int
	ProfilesCreated int
	ProfilesSkipped int
	ProfilesFailed  int
	DurationMs      int
	Errors          []string
}

// RunRefresh executes a refresh run for a specific fragment type.
func (s *Scheduler) RunRefresh(ctx context.Context, fragmentType models.FragmentType) (*RefreshResult, error) {
	startTime := time.Now()
	result := &RefreshResult{
		FragmentType: fragmentType,
		Errors:       make([]string, 0),
	}

	// Get policy for this type
	policy, err := s.policiesRepo.GetByType(ctx, fragmentType)
	if err != nil {
		return nil, fmt.Errorf("failed to get policy for type %s: %w", fragmentType, err)
	}

	if !policy.Enabled {
		log.Printf("[ProfileScheduler] Policy disabled for type %s, skipping", fragmentType)
		return result, nil
	}

	log.Printf("[ProfileScheduler] Starting refresh for type=%s topPercent=%.2f maxPerRun=%d minEdgeCount=%d",
		fragmentType, policy.TopPercent, policy.MaxPerRun, policy.MinEdgeCount)

	// Find candidates: top N% by edge count
	candidates, err := s.findCandidates(ctx, fragmentType, policy)
	if err != nil {
		return nil, fmt.Errorf("failed to find candidates: %w", err)
	}

	result.CandidatesFound = len(candidates)
	log.Printf("[ProfileScheduler] Found %d candidates for type %s", len(candidates), fragmentType)

	// Async mode via Cloud Tasks
	if s.tasksDispatcher != nil {
		for _, candidate := range candidates {
			// Check if fresh profile already exists
			if policy.MinAgeDays > 0 {
				existing, err := s.profilesRepo.GetByFragmentID(ctx, candidate.ID)
				if err == nil && existing != nil {
					age := time.Since(existing.CapturedAt)
					if age < time.Duration(policy.MinAgeDays)*24*time.Hour {
						result.ProfilesSkipped++
						continue
					}
				}
			}

			if err := s.tasksDispatcher.EnqueueProfileRefresh(ctx, candidate.ID, false); err != nil {
				result.ProfilesFailed++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", candidate.CanonicalPath, err))
				log.Printf("[ProfileScheduler] Failed to enqueue profile refresh for %s: %v", candidate.CanonicalPath, err)
				continue
			}

			result.ProfilesCreated++
		}

		result.DurationMs = int(time.Since(startTime).Milliseconds())
		log.Printf("[ProfileScheduler] Enqueued refresh tasks for type=%s: enqueued=%d skipped=%d failed=%d duration=%dms",
			fragmentType, result.ProfilesCreated, result.ProfilesSkipped, result.ProfilesFailed, result.DurationMs)
		return result, nil
	}

	// Generate profiles for candidates
	opts := GenerateOptions{
		MaxNeighbors:      10,
		ForceRegenerate:   false,
		MarkPreviousStale: true,
	}

	for _, candidate := range candidates {
		// Check if fresh profile already exists
		if policy.MinAgeDays > 0 {
			existing, err := s.profilesRepo.GetByFragmentID(ctx, candidate.ID)
			if err == nil && existing != nil {
				age := time.Since(existing.CapturedAt)
				if age < time.Duration(policy.MinAgeDays)*24*time.Hour {
					result.ProfilesSkipped++
					continue
				}
			}
		}

		// Generate profile
		genResult, err := s.generator.Generate(ctx, candidate.ID, opts)
		if err != nil {
			result.ProfilesFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", candidate.CanonicalPath, err))
			log.Printf("[ProfileScheduler] Failed to generate profile for %s: %v", candidate.CanonicalPath, err)
			continue
		}

		if genResult.Created {
			result.ProfilesCreated++
		} else {
			result.ProfilesSkipped++
		}
	}

	result.DurationMs = int(time.Since(startTime).Milliseconds())

	log.Printf("[ProfileScheduler] Completed refresh for type=%s: created=%d skipped=%d failed=%d duration=%dms",
		fragmentType, result.ProfilesCreated, result.ProfilesSkipped, result.ProfilesFailed, result.DurationMs)

	return result, nil
}

// fragmentWithDegree holds a fragment and its edge count for sorting.
type fragmentWithDegree struct {
	Fragment  *models.HorizonFragment
	EdgeCount int
}

// findCandidates finds the top N% fragments by edge count.
func (s *Scheduler) findCandidates(
	ctx context.Context,
	fragmentType models.FragmentType,
	policy *models.FragmentProfileRefreshPolicy,
) ([]*models.HorizonFragment, error) {
	// For GOV entities, we use ORGANIZATION type with /gov/ path prefix
	isGov := fragmentType == "GOV" || fragmentType == "GOVERNMENT"
	actualType := fragmentType
	if isGov {
		actualType = models.FragmentTypeOrganization
	}

	// Get all fragments of this type
	// Note: In production, this should use pagination for large datasets
	fragments, err := s.fragmentsRepo.ListByTypeAndTenant(ctx, "global", actualType, 10000)
	if err != nil {
		return nil, fmt.Errorf("failed to list fragments: %w", err)
	}

	// Filter for GOV if needed
	if isGov {
		filtered := make([]*models.HorizonFragment, 0)
		for _, f := range fragments {
			if strings.HasPrefix(f.CanonicalPath, "/gov/") {
				filtered = append(filtered, f)
			}
		}
		fragments = filtered
	}

	if len(fragments) == 0 {
		return nil, nil
	}

	log.Printf("[ProfileScheduler] Counting edges for %d fragments of type %s", len(fragments), fragmentType)

	// Count edges via single scan to avoid per-fragment queries
	fragmentIDs := make(map[string]struct{}, len(fragments))
	for _, f := range fragments {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		fragmentIDs[f.ID] = struct{}{}
	}

	counts, err := s.edgesRepo.CountDegreesForFragmentIDs(ctx, "global", fragmentIDs, 5000)
	if err != nil {
		return nil, fmt.Errorf("failed to count degrees: %w", err)
	}

	withDegree := make([]fragmentWithDegree, 0, len(fragments))
	for _, f := range fragments {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		edgeCount := counts[f.ID]

		// Apply minimum edge count filter
		if edgeCount < policy.MinEdgeCount {
			continue
		}

		withDegree = append(withDegree, fragmentWithDegree{
			Fragment:  f,
			EdgeCount: edgeCount,
		})
	}

	if len(withDegree) == 0 {
		return nil, nil
	}

	// Sort by edge count descending
	sort.Slice(withDegree, func(i, j int) bool {
		return withDegree[i].EdgeCount > withDegree[j].EdgeCount
	})

	// Take top N%
	topCount := int(float64(len(withDegree)) * policy.TopPercent)
	if topCount < 1 {
		topCount = 1
	}
	if topCount > policy.MaxPerRun {
		topCount = policy.MaxPerRun
	}
	if topCount > len(withDegree) {
		topCount = len(withDegree)
	}

	log.Printf("[ProfileScheduler] Selected top %d fragments (%.0f%% of %d with >=%d edges)",
		topCount, policy.TopPercent*100, len(withDegree), policy.MinEdgeCount)

	// Extract fragments
	result := make([]*models.HorizonFragment, 0, topCount)
	for i := 0; i < topCount; i++ {
		result = append(result, withDegree[i].Fragment)
	}

	return result, nil
}

// RunAllTypes runs refresh for all enabled fragment types.
func (s *Scheduler) RunAllTypes(ctx context.Context) (map[models.FragmentType]*RefreshResult, error) {
	results := make(map[models.FragmentType]*RefreshResult)

	policies, err := s.policiesRepo.List(ctx)
	if err != nil {
		// Fall back to defaults
		defaults := models.DefaultRefreshPolicies()
		policies = make([]*models.FragmentProfileRefreshPolicy, 0, len(defaults))
		for i := range defaults {
			policies = append(policies, &defaults[i])
		}
	}

	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		result, err := s.RunRefresh(ctx, policy.FragmentType)
		if err != nil {
			log.Printf("[ProfileScheduler] Error running refresh for %s: %v", policy.FragmentType, err)
			results[policy.FragmentType] = &RefreshResult{
				FragmentType: policy.FragmentType,
				Errors:       []string{err.Error()},
			}
			continue
		}

		results[policy.FragmentType] = result
	}

	return results, nil
}

// RefreshSingle refreshes a single fragment's profile on demand.
func (s *Scheduler) RefreshSingle(ctx context.Context, fragmentID string) (*GenerateResult, error) {
	opts := GenerateOptions{
		MaxNeighbors:      10,
		ForceRegenerate:   true,
		MarkPreviousStale: true,
	}

	return s.generator.Generate(ctx, fragmentID, opts)
}

// RefreshSingleWithOptions refreshes a single fragment with optional force flag.
func (s *Scheduler) RefreshSingleWithOptions(ctx context.Context, fragmentID string, force bool) (*GenerateResult, error) {
	opts := GenerateOptions{
		MaxNeighbors:      10,
		ForceRegenerate:   force,
		MarkPreviousStale: true,
	}

	return s.generator.Generate(ctx, fragmentID, opts)
}

package scout

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

const stateKeyWorkingProductScope = "workingProductScope"

func loadWorkingProductScopeFromJob(job *models.ScoutingJob) *models.WorkingProductScope {
	if job == nil || job.State == nil {
		return nil
	}
	raw, ok := job.State[stateKeyWorkingProductScope]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case *models.WorkingProductScope:
		if typed == nil {
			return nil
		}
		scope := *typed
		scope.Normalize()
		return &scope
	case models.WorkingProductScope:
		scope := typed
		scope.Normalize()
		return &scope
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var scope models.WorkingProductScope
		if err := json.Unmarshal(data, &scope); err != nil {
			return nil
		}
		scope.Normalize()
		if scope.JobID == "" {
			scope.JobID = strings.TrimSpace(job.ID)
		}
		if scope.TenantID == "" {
			scope.TenantID = strings.TrimSpace(job.TenantID)
		}
		if scope.LensID == "" {
			scope.LensID = strings.TrimSpace(job.LensID)
		}
		return &scope
	}
}

func ensureWorkingProductScopeSeeded(job *models.ScoutingJob, rawProducts []string, sourceKind, domainPath string) *models.WorkingProductScope {
	if job == nil {
		return nil
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	scope := loadWorkingProductScopeFromJob(job)
	if scope == nil {
		scope = &models.WorkingProductScope{
			JobID:      strings.TrimSpace(job.ID),
			TenantID:   strings.TrimSpace(job.TenantID),
			LensID:     strings.TrimSpace(job.LensID),
			DomainPath: strings.TrimSpace(domainPath),
		}
	}
	if scope.DomainPath == "" {
		scope.DomainPath = strings.TrimSpace(domainPath)
	}
	now := time.Now().UTC()
	for _, raw := range rawProducts {
		ref, alias := normalizeWorkingProductSeed(raw)
		if ref == "" {
			continue
		}
		entry := models.WorkingProductEntry{
			ProductRef:       ref,
			Aliases:          nonEmptyStrings(alias),
			SourceKinds:      nonEmptyStrings(strings.TrimSpace(sourceKind)),
			DiscoveryReasons: nonEmptyStrings("query_seed"),
			Status:           models.WorkingProductStatusSeeded,
			Confidence:       0.85,
			LastExpandedAt:   &now,
			LastValidatedAt:  &now,
		}
		scope.MergeEntry(entry)
	}
	scope.Normalize()
	mirrorWorkingProductScopeToJob(job, scope)
	return scope
}

func mergeWorkingProductScopeFromChains(job *models.ScoutingJob, chains []agents.SupplyChain, sourceKind string) *models.WorkingProductScope {
	if job == nil || len(chains) == 0 {
		return loadWorkingProductScopeFromJob(job)
	}
	scope := ensureWorkingProductScopeSeeded(job, nil, sourceKind, strings.TrimSpace(job.TargetCanonicalPath))
	if scope == nil {
		return nil
	}
	now := time.Now().UTC()
	for _, chain := range chains {
		for _, node := range chain.Nodes {
			cp := canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(node.CanonicalPath))
			if !strings.HasPrefix(cp, "/product/") {
				continue
			}
			entry := models.WorkingProductEntry{
				ProductRef:       cp,
				Aliases:          nonEmptyStrings(strings.TrimSpace(node.Name)),
				SourceKinds:      nonEmptyStrings(strings.TrimSpace(sourceKind)),
				DiscoveryReasons: nonEmptyStrings(strings.TrimSpace(chain.Reason)),
				Status:           models.WorkingProductStatusDiscovered,
				Confidence:       discoveredProductConfidence(chain.Confidence),
				LastExpandedAt:   &now,
			}
			scope.MergeEntry(entry)
		}
	}
	scope.LastExpandedAt = now
	scope.Normalize()
	mirrorWorkingProductScopeToJob(job, scope)
	return scope
}

func mirrorWorkingProductScopeToJob(job *models.ScoutingJob, scope *models.WorkingProductScope) {
	if job == nil || scope == nil {
		return
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	job.State[stateKeyWorkingProductScope] = *scope
	job.State["workingProductRefs"] = scope.CanonicalProductRefs()
}

func persistWorkingProductScope(ctx context.Context, engine *ScoutEngine, job *models.ScoutingJob, scope *models.WorkingProductScope) {
	if engine == nil || job == nil || scope == nil {
		return
	}
	scope.JobID = strings.TrimSpace(job.ID)
	if scope.TenantID == "" {
		scope.TenantID = strings.TrimSpace(job.TenantID)
	}
	if scope.LensID == "" {
		scope.LensID = strings.TrimSpace(job.LensID)
	}
	scope.Normalize()
	if engine.workingProductScopesRepo != nil {
		_ = engine.workingProductScopesRepo.Upsert(ctx, scope)
	}
	if engine.scoutingJobsRepo != nil && strings.TrimSpace(job.ID) != "" {
		_ = engine.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
			stateKeyWorkingProductScope: *scope,
			"workingProductRefs":        scope.CanonicalProductRefs(),
		})
	}
}

func normalizeWorkingProductSeed(raw string) (ref string, alias string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if strings.HasPrefix(raw, "/product/") {
		return canonical.NormalizeCanonicalPathForPersist(raw), raw
	}
	slug := strings.ToLower(strings.TrimSpace(raw))
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = strings.ReplaceAll(slug, " ", "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "", raw
	}
	return canonical.NormalizeCanonicalPathForPersist("/product/" + slug), raw
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func discoveredProductConfidence(value float64) float64 {
	if value < 0.55 {
		return 0.55
	}
	if value > 1 {
		return 1
	}
	return value
}

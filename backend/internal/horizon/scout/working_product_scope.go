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

const (
	workingProductCandidateAcceptThreshold = 0.72
	workingProductCandidateStaleThreshold  = 0.55
)

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
			ProductRef:            ref,
			PrimaryOperationalRef: ref,
			EquivalentProductRefs: nonEmptyStrings(ref),
			SeedRefs:              nonEmptyStrings(ref),
			Aliases:               nonEmptyStrings(alias),
			SourceKinds:           nonEmptyStrings(strings.TrimSpace(sourceKind)),
			DiscoveryReasons:      nonEmptyStrings("query_seed"),
			Status:                models.WorkingProductStatusSeeded,
			Confidence:            0.85,
			LastExpandedAt:        &now,
			LastValidatedAt:       &now,
		}
		mergeWorkingProductEntryResolved(scope, entry)
	}
	scope.Normalize()
	mirrorWorkingProductScopeToJob(job, scope)
	return scope
}

func ensureWorkingProductScopeLoaded(
	ctx context.Context,
	engine *ScoutEngine,
	job *models.ScoutingJob,
	rawProducts []string,
	sourceKind, domainPath string,
) *models.WorkingProductScope {
	if job == nil {
		return nil
	}
	if scope := loadWorkingProductScopeFromJob(job); scope != nil {
		if len(rawProducts) > 0 || strings.TrimSpace(domainPath) != "" {
			scope = ensureWorkingProductScopeSeeded(job, rawProducts, sourceKind, domainPath)
		}
		return scope
	}

	if engine != nil && engine.workingProductScopesRepo != nil && strings.TrimSpace(job.ID) != "" {
		if persisted, err := engine.workingProductScopesRepo.GetByJobID(ctx, job.ID); err == nil && persisted != nil {
			if persisted.JobID == "" {
				persisted.JobID = strings.TrimSpace(job.ID)
			}
			if persisted.TenantID == "" {
				persisted.TenantID = strings.TrimSpace(job.TenantID)
			}
			if persisted.LensID == "" {
				persisted.LensID = strings.TrimSpace(job.LensID)
			}
			if persisted.DomainPath == "" {
				persisted.DomainPath = strings.TrimSpace(domainPath)
			}
			mirrorWorkingProductScopeToJob(job, persisted)
			if len(rawProducts) > 0 || strings.TrimSpace(domainPath) != "" {
				persisted = ensureWorkingProductScopeSeeded(job, rawProducts, sourceKind, domainPath)
				persistWorkingProductScope(ctx, engine, job, persisted)
			}
			return persisted
		}
	}

	legacySeeds := legacyWorkingProductSeeds(job, rawProducts)
	scope := ensureWorkingProductScopeSeeded(job, legacySeeds, sourceKind, domainPath)
	if scope != nil && engine != nil {
		persistWorkingProductScope(ctx, engine, job, scope)
	}
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
				ProductRef:            cp,
				PrimaryOperationalRef: cp,
				EquivalentProductRefs: nonEmptyStrings(cp),
				Aliases:               nonEmptyStrings(strings.TrimSpace(node.Name)),
				SourceKinds:           nonEmptyStrings(strings.TrimSpace(sourceKind)),
				DiscoveryReasons:      nonEmptyStrings(strings.TrimSpace(chain.Reason)),
				Status:                models.WorkingProductStatusDiscovered,
				Confidence:            discoveredProductConfidence(chain.Confidence),
				LastExpandedAt:        &now,
			}
			mergeWorkingProductEntryResolved(scope, entry)
		}
		for _, candidate := range chain.ProductCandidates {
			mergeWorkingProductCandidate(scope, candidate, chain, sourceKind, now)
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

func markWorkingProductCoverage(job *models.ScoutingJob, productRef, coverageStatus string, status models.WorkingProductStatus) *models.WorkingProductScope {
	productRef = strings.TrimSpace(productRef)
	if job == nil || productRef == "" {
		return loadWorkingProductScopeFromJob(job)
	}
	scope := loadWorkingProductScopeFromJob(job)
	if scope == nil {
		return nil
	}
	now := time.Now().UTC()
	updated := false
	for idx := range scope.CanonicalProducts {
		if strings.TrimSpace(scope.CanonicalProducts[idx].ProductRef) != productRef {
			continue
		}
		if strings.TrimSpace(coverageStatus) != "" {
			scope.CanonicalProducts[idx].CoverageStatus = strings.TrimSpace(coverageStatus)
		}
		if status != "" {
			scope.CanonicalProducts[idx].Status = status
		}
		scope.CanonicalProducts[idx].LastValidatedAt = &now
		scope.CanonicalProducts[idx].UpdatedAt = now
		updated = true
		break
	}
	if !updated {
		mergeWorkingProductEntryResolved(scope, models.WorkingProductEntry{
			ProductRef:            productRef,
			PrimaryOperationalRef: productRef,
			EquivalentProductRefs: nonEmptyStrings(productRef),
			Status:                status,
			CoverageStatus:        strings.TrimSpace(coverageStatus),
			LastValidatedAt:       &now,
			UpdatedAt:             now,
		})
	}
	scope.LastValidatedAt = now
	scope.UpdatedAt = now
	mirrorWorkingProductScopeToJob(job, scope)
	return scope
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

func legacyWorkingProductSeeds(job *models.ScoutingJob, rawProducts []string) []string {
	out := append([]string(nil), rawProducts...)
	if job == nil || job.State == nil {
		return dedupStrings(out)
	}
	out = append(out, parseStateStringSlice(job.State["workingProductRefs"])...)
	return dedupStrings(out)
}

func mergeWorkingProductCandidate(
	scope *models.WorkingProductScope,
	candidate agents.ProductCandidate,
	chain agents.SupplyChain,
	sourceKind string,
	now time.Time,
) {
	if scope == nil {
		return
	}
	ref, alias := normalizeWorkingProductSeed(firstNonEmptyString(candidate.CanonicalPath, candidate.Name))
	if ref == "" {
		return
	}
	confidence := normalizeProductCandidateConfidence(candidate.Confidence, chain.Confidence)
	status := classifyWorkingProductCandidate(confidence)
	reasons := nonEmptyStrings(strings.TrimSpace(candidate.Reason), strings.TrimSpace(chain.Reason))
	aliases := nonEmptyStrings(alias, strings.TrimSpace(candidate.Name))

	if existing := findWorkingProductEntry(scope, ref); existing != nil {
		mergeWorkingProductEntryResolved(scope, models.WorkingProductEntry{
			ProductRef:            ref,
			PrimaryOperationalRef: firstNonEmptyString(existing.PrimaryOperationalRef, existing.ProductRef),
			EquivalentProductRefs: nonEmptyStrings(ref),
			Aliases:               aliases,
			SourceKinds:           nonEmptyStrings(strings.TrimSpace(sourceKind), "product_candidate"),
			DiscoveryReasons:      reasons,
			Status:                status,
			Confidence:            confidence,
			LastExpandedAt:        &now,
			LastValidatedAt:       &now,
		})
		if status == models.WorkingProductStatusRejected {
			scope.RejectedProductRefs = dedupStrings(append(scope.RejectedProductRefs, ref))
		}
		return
	}

	mergeWorkingProductEntryResolved(scope, models.WorkingProductEntry{
		ProductRef:            ref,
		PrimaryOperationalRef: ref,
		EquivalentProductRefs: nonEmptyStrings(ref),
		Aliases:               aliases,
		SourceKinds:           nonEmptyStrings(strings.TrimSpace(sourceKind), "product_candidate"),
		DiscoveryReasons:      reasons,
		Status:                status,
		Confidence:            confidence,
		LastExpandedAt:        &now,
		LastValidatedAt:       &now,
	})
	if status == models.WorkingProductStatusRejected {
		scope.RejectedProductRefs = dedupStrings(append(scope.RejectedProductRefs, ref))
	}
}

func findWorkingProductEntry(scope *models.WorkingProductScope, productRef string) *models.WorkingProductEntry {
	if scope == nil {
		return nil
	}
	productRef = strings.TrimSpace(productRef)
	if productRef == "" {
		return nil
	}
	for idx := range scope.CanonicalProducts {
		entry := &scope.CanonicalProducts[idx]
		if strings.TrimSpace(entry.ProductRef) == productRef || strings.TrimSpace(entry.PrimaryOperationalRef) == productRef {
			return entry
		}
		if containsCanonicalRef(entry.EquivalentProductRefs, productRef) || containsCanonicalRef(entry.SeedRefs, productRef) {
			return entry
		}
		if sharesWorkingProductOperationalKey(append([]string{entry.ProductRef, entry.PrimaryOperationalRef}, append(entry.EquivalentProductRefs, entry.Aliases...)...), []string{productRef}) {
			return entry
		}
		if anyAliasMatchesOperationalKey(entry.Aliases, productRef) {
			return &scope.CanonicalProducts[idx]
		}
	}
	return nil
}

func mergeWorkingProductEntryResolved(scope *models.WorkingProductScope, entry models.WorkingProductEntry) {
	if scope == nil {
		return
	}
	ref := firstNonEmptyString(entry.PrimaryOperationalRef, entry.ProductRef)
	if ref == "" {
		return
	}
	if existing := findWorkingProductEntry(scope, ref); existing != nil {
		preferred := choosePreferredOperationalProductRef(firstNonEmptyString(existing.PrimaryOperationalRef, existing.ProductRef), ref)
		equivalentRefs := dedupStrings(append(existing.EquivalentProductRefs, entry.EquivalentProductRefs...))
		equivalentRefs = dedupStrings(append(equivalentRefs, ref, existing.ProductRef, existing.PrimaryOperationalRef))
		existing.EquivalentProductRefs = equivalentRefs
		existing.SeedRefs = dedupStrings(append(existing.SeedRefs, entry.SeedRefs...))
		existing.Aliases = dedupStrings(append(existing.Aliases, entry.Aliases...))
		existing.SourceKinds = dedupStrings(append(existing.SourceKinds, entry.SourceKinds...))
		existing.DiscoveryReasons = dedupStrings(append(existing.DiscoveryReasons, entry.DiscoveryReasons...))
		if entry.Confidence > existing.Confidence {
			existing.Confidence = entry.Confidence
		}
		if entry.Status != "" && existing.Status != models.WorkingProductStatusConfirmed {
			existing.Status = entry.Status
		}
		if strings.TrimSpace(entry.CoverageStatus) != "" {
			existing.CoverageStatus = strings.TrimSpace(entry.CoverageStatus)
		}
		if entry.LastExpandedAt != nil {
			existing.LastExpandedAt = entry.LastExpandedAt
		}
		if entry.LastValidatedAt != nil {
			existing.LastValidatedAt = entry.LastValidatedAt
		}
		if preferred != "" {
			existing.PrimaryOperationalRef = preferred
			existing.ProductRef = preferred
		}
		existing.UpdatedAt = time.Now().UTC()
		scope.Normalize()
		return
	}
	scope.MergeEntry(entry)
}

func choosePreferredOperationalProductRef(current, candidate string) string {
	current = strings.TrimSpace(current)
	candidate = strings.TrimSpace(candidate)
	if current == "" {
		return candidate
	}
	if candidate == "" {
		return current
	}
	if scoreOperationalProductRef(candidate) > scoreOperationalProductRef(current) {
		return candidate
	}
	return current
}

func scoreOperationalProductRef(ref string) int {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return -1000
	}
	score := 0
	switch {
	case strings.Contains(ref, "/product/material/"):
		score += 8
	case strings.Contains(ref, "/product/raw/"):
		score += 7
	case strings.Contains(ref, "/product/energy/"):
		score += 6
	case strings.Contains(ref, "/product/commodity/"):
		score += 5
	case strings.Contains(ref, "/product/finished/"):
		score += 1
	}
	if strings.Contains(ref, "/finished/mechanical/assembly/") {
		score -= 4
	}
	score -= strings.Count(ref, "/")
	return score
}

func workingProductOperationalKey(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if !strings.HasPrefix(ref, "/product/") {
		ref = strings.ToLower(ref)
		ref = strings.ReplaceAll(ref, "_", "-")
		ref = strings.ReplaceAll(ref, " ", "-")
		for strings.Contains(ref, "--") {
			ref = strings.ReplaceAll(ref, "--", "-")
		}
		return strings.Trim(ref, "-")
	}
	ref = canonical.NormalizeCanonicalPathForPersist(ref)
	ref = strings.TrimSuffix(ref, "/")
	if idx := strings.LastIndex(ref, "/"); idx >= 0 && idx+1 < len(ref) {
		return ref[idx+1:]
	}
	return ref
}

func workingProductOperationalKeys(value string) []string {
	key := workingProductOperationalKey(value)
	if key == "" {
		return nil
	}
	out := []string{key}
	parts := strings.Fields(strings.ReplaceAll(key, "-", " "))
	if len(parts) >= 2 {
		var acronym strings.Builder
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			acronym.WriteByte(part[0])
		}
		if acronym.Len() >= 2 {
			out = append(out, strings.ToLower(acronym.String()))
		}
	}
	return dedupStrings(out)
}

func containsCanonicalRef(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func anyAliasMatchesOperationalKey(aliases []string, productRef string) bool {
	return sharesWorkingProductOperationalKey(aliases, []string{productRef})
}

func sharesWorkingProductOperationalKey(left []string, right []string) bool {
	leftKeys := map[string]struct{}{}
	for _, value := range left {
		for _, key := range workingProductOperationalKeys(value) {
			leftKeys[key] = struct{}{}
		}
	}
	if len(leftKeys) == 0 {
		return false
	}
	for _, value := range right {
		for _, key := range workingProductOperationalKeys(value) {
			if _, ok := leftKeys[key]; ok {
				return true
			}
		}
	}
	return false
}

func classifyWorkingProductCandidate(confidence float64) models.WorkingProductStatus {
	switch {
	case confidence >= workingProductCandidateAcceptThreshold:
		return models.WorkingProductStatusDiscovered
	case confidence >= workingProductCandidateStaleThreshold:
		return models.WorkingProductStatusStaleCandidate
	default:
		return models.WorkingProductStatusRejected
	}
}

func normalizeProductCandidateConfidence(candidate, chain float64) float64 {
	confidence := candidate
	if confidence <= 0 {
		confidence = chain
	}
	if confidence <= 0 {
		confidence = 0.5
	}
	if confidence > 1 {
		confidence = 1
	}
	return confidence
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

package signallanding

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type RetroAttachResult struct {
	FragmentRef        string
	AttachedSessionIDs []string
	CreatedHitRefs     []string
	ResolvedCandidates []string
	WarmupRefs         []string
}

func (s *Service) RetroAttachFragment(ctx context.Context, fragment *horizonmodels.HorizonFragment) (*RetroAttachResult, error) {
	if s == nil || fragment == nil {
		return nil, nil
	}
	if !eligibleRetroAttachFragment(fragment) {
		return &RetroAttachResult{FragmentRef: strings.TrimSpace(fragment.CanonicalPath)}, nil
	}
	fragmentRef := strings.TrimSpace(fragment.CanonicalPath)
	if fragmentRef == "" {
		return nil, fmt.Errorf("fragment canonical path is required")
	}
	tenantID := strings.TrimSpace(fragment.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	sessions, err := s.listActiveRetroAttachSessions(ctx, tenantID, 64)
	if err != nil {
		return nil, err
	}
	result := &RetroAttachResult{
		FragmentRef: fragmentRef,
		WarmupRefs:  []string{fragmentRef},
	}
	if len(sessions) == 0 {
		return result, nil
	}
	seenWarmup := map[string]struct{}{fragmentRef: {}}
	for _, session := range sessions {
		if session == nil || !sessionIsRetroAttachActive(session, time.Now().UTC()) {
			continue
		}
		claims, err := s.claimsRepo.ListBySession(ctx, session.ID, 64)
		if err != nil {
			continue
		}
		candidates, err := s.candidatesRepo.ListBySession(ctx, session.ID, 64)
		if err != nil {
			continue
		}
		sessionHits, err := s.hitsRepo.ListBySession(ctx, session.ID, 96)
		if err != nil {
			continue
		}
		attachedThisSession := false
		for _, claim := range claims {
			score, reasons, resolvedCandidates := s.retroAttachScore(fragment, claim, candidates, sessionHits)
			if score < 0.56 {
				continue
			}
			newHit := &horizonmodels.SignalLandingHit{
				SessionID:              strings.TrimSpace(session.ID),
				ClaimID:                strings.TrimSpace(claim.ID),
				TenantID:               strings.TrimSpace(session.TenantID),
				CanonicalRef:           fragmentRef,
				RefKind:                refKindFromCanonicalPath(fragmentRef),
				LandingClass:           horizonmodels.SignalLandingClassInferred,
				LandingMethod:          horizonmodels.SignalLandingMethodRetroAttach,
				Confidence:             clamp01(score),
				Importance:             clamp01(maxFloat(score, claim.Severity)),
				Why:                    "new graph object matched an active signal landing context after materialization",
				SupportingEvidenceRefs: compactUniqueStrings(claim.SourceEvidenceRefs),
				SupportingPaths:        compactUniqueStrings(reasons),
				CreatedAt:              time.Now().UTC(),
			}
			newHit.Normalize()
			if err := s.hitsRepo.Upsert(ctx, newHit); err != nil {
				return result, err
			}
			newHits := []*horizonmodels.SignalLandingHit{newHit}
			if claim.ZoneEligibility != horizonmodels.SignalZoneEligibilityForbidden {
				if zoneRef, zerr := s.ensureAffectedZoneForAnchor(ctx, session, claim, fragmentRef); zerr == nil && strings.TrimSpace(zoneRef) != "" {
					zoneHit := &horizonmodels.SignalLandingHit{
						SessionID:              strings.TrimSpace(session.ID),
						ClaimID:                strings.TrimSpace(claim.ID),
						TenantID:               strings.TrimSpace(session.TenantID),
						CanonicalRef:           zoneRef,
						RefKind:                refKindFromCanonicalPath(zoneRef),
						LandingClass:           horizonmodels.SignalLandingClassInferred,
						LandingMethod:          horizonmodels.SignalLandingMethodRetroAttach,
						Confidence:             clamp01(score * 0.9),
						Importance:             clamp01(maxFloat(score, claim.Severity)),
						Why:                    "active signal landing retro-attached a precise affected zone for the new anchor",
						SupportingEvidenceRefs: compactUniqueStrings(claim.SourceEvidenceRefs),
						SupportingPaths:        compactUniqueStrings(append(reasons, zoneRef)),
						CreatedAt:              time.Now().UTC(),
					}
					zoneHit.Normalize()
					if err := s.hitsRepo.Upsert(ctx, zoneHit); err == nil {
						newHits = append(newHits, zoneHit)
						if _, ok := seenWarmup[zoneRef]; !ok {
							seenWarmup[zoneRef] = struct{}{}
							result.WarmupRefs = append(result.WarmupRefs, zoneRef)
						}
					}
				}
			}
			if countryRef := retroAttachCountryScopeRef(fragment); countryRef != "" {
				if _, ok := seenWarmup[countryRef]; !ok {
					seenWarmup[countryRef] = struct{}{}
					result.WarmupRefs = append(result.WarmupRefs, countryRef)
				}
			}
			if err := s.upsertRetroAttachSummaries(ctx, session, claim, fragment, newHits, candidates); err != nil {
				return result, err
			}
			for _, candidate := range resolvedCandidates {
				candidate.Status = "resolved"
				candidate.SuggestedCanonicalPath = fragmentRef
				candidate.UpdatedAt = time.Now().UTC()
				if err := s.candidatesRepo.Upsert(ctx, candidate); err == nil {
					result.ResolvedCandidates = append(result.ResolvedCandidates, candidate.Label)
				}
			}
			result.CreatedHitRefs = append(result.CreatedHitRefs, fragmentRef)
			attachedThisSession = true
		}
		if attachedThisSession {
			result.AttachedSessionIDs = append(result.AttachedSessionIDs, strings.TrimSpace(session.ID))
			session.UpdatedAt = time.Now().UTC()
			if session.ActiveUntil.Before(session.UpdatedAt.Add(6 * time.Hour)) {
				session.ActiveUntil = session.UpdatedAt.Add(6 * time.Hour)
			}
			_ = s.sessionsRepo.Upsert(ctx, session)
		}
	}
	result.AttachedSessionIDs = compactUniqueStrings(result.AttachedSessionIDs)
	result.CreatedHitRefs = compactUniqueStrings(result.CreatedHitRefs)
	result.ResolvedCandidates = compactUniqueStrings(result.ResolvedCandidates)
	result.WarmupRefs = compactUniqueStrings(result.WarmupRefs)
	return result, nil
}

func (s *Service) listActiveRetroAttachSessions(ctx context.Context, tenantID string, limit int) ([]*horizonmodels.SignalLandingSession, error) {
	if s == nil || s.sessionsRepo == nil {
		return nil, nil
	}
	items := make([]*horizonmodels.SignalLandingSession, 0, limit)
	appendItems := func(list []*horizonmodels.SignalLandingSession) {
		for _, item := range list {
			if item != nil {
				items = append(items, item)
			}
		}
	}
	if tenantID == "" {
		tenantID = "global"
	}
	if list, err := s.sessionsRepo.ListRecent(ctx, tenantID, limit); err == nil {
		appendItems(list)
	}
	if tenantID != "global" {
		if list, err := s.sessionsRepo.ListRecent(ctx, "global", limit); err == nil {
			appendItems(list)
		}
	}
	byID := map[string]*horizonmodels.SignalLandingSession{}
	for _, item := range items {
		if item != nil && strings.TrimSpace(item.ID) != "" {
			byID[item.ID] = item
		}
	}
	out := make([]*horizonmodels.SignalLandingSession, 0, len(byID))
	for _, item := range byID {
		if sessionIsRetroAttachActive(item, time.Now().UTC()) {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Service) retroAttachScore(
	fragment *horizonmodels.HorizonFragment,
	claim *horizonmodels.SignalImpactClaim,
	candidates []*horizonmodels.SignalMissingAnchorCandidate,
	sessionHits []*horizonmodels.SignalLandingHit,
) (float64, []string, []*horizonmodels.SignalMissingAnchorCandidate) {
	score := 0.0
	reasons := []string{}
	resolved := []*horizonmodels.SignalMissingAnchorCandidate{}
	fragmentTokens := retroAttachTokens(fragment)
	fragmentCountry := retroAttachCountryScopeRef(fragment)
	fragmentKind := strings.ToLower(strings.TrimSpace(string(fragment.Type)))

	if fragmentCountry != "" && containsString(compactUniqueStrings(claim.CountryHints), fragmentCountry) {
		score += 0.18
		reasons = append(reasons, "country_hint:"+fragmentCountry)
	}
	if overlap := tokenOverlapCount(fragmentTokens, retroAttachHintTokens(claim)); overlap > 0 {
		score += minFloat(0.42, 0.14*float64(overlap))
		reasons = append(reasons, "claim_hint_overlap")
	}
	if overlap := tokenOverlapCount(fragmentTokens, retroAttachSessionHitTokens(sessionHits)); overlap > 0 {
		score += minFloat(0.2, 0.08*float64(overlap))
		reasons = append(reasons, "active_session_context_overlap")
	}
	for _, candidate := range candidates {
		if candidate == nil || !strings.EqualFold(strings.TrimSpace(candidate.Status), "open") {
			continue
		}
		if fragmentCountry != "" && strings.TrimSpace(candidate.HintCountry) != "" && !strings.EqualFold(strings.TrimSpace(candidate.HintCountry), fragmentCountry) {
			continue
		}
		normalizedLabel := normalizeCandidateLabel(candidate.Label)
		if normalizedLabel != "" && tokenOverlapCount(fragmentTokens, strings.Fields(normalizedLabel)) > 0 {
			score += 0.48
			reasons = append(reasons, "missing_candidate:"+normalizedLabel)
			resolved = append(resolved, candidate)
			continue
		}
		if tokenOverlapCount(fragmentTokens, normalizeCandidateAliases(candidate.CandidateAliases)) > 0 {
			score += 0.38
			reasons = append(reasons, "candidate_alias_overlap")
			resolved = append(resolved, candidate)
		}
	}
	if fragmentKind == "facility" && len(claim.FacilityHints) > 0 {
		score += 0.06
	}
	if fragmentKind == "product" && len(claim.ProductHints) > 0 {
		score += 0.06
	}
	if fragmentKind == "geo_region" && claim.ZoneEligibility != horizonmodels.SignalZoneEligibilityForbidden {
		score += 0.05
	}
	return clamp01(score), compactUniqueStrings(reasons), compactUniqueCandidates(resolved)
}

func (s *Service) upsertRetroAttachSummaries(
	ctx context.Context,
	session *horizonmodels.SignalLandingSession,
	claim *horizonmodels.SignalImpactClaim,
	fragment *horizonmodels.HorizonFragment,
	newHits []*horizonmodels.SignalLandingHit,
	candidates []*horizonmodels.SignalMissingAnchorCandidate,
) error {
	if s == nil || s.summariesRepo == nil || fragment == nil {
		return nil
	}
	builders := map[string]*summaryBuilder{}
	accumulateSummaries(builders, session, claim, newHits, candidates)
	scopeRefs := []string{strings.TrimSpace(fragment.CanonicalPath)}
	if countryRef := retroAttachCountryScopeRef(fragment); countryRef != "" {
		scopeRefs = append(scopeRefs, countryRef)
	}
	for _, scopeRef := range compactUniqueStrings(scopeRefs) {
		builder := builders[scopeRef]
		if builder == nil {
			builder = &summaryBuilder{tenantID: session.TenantID, scopeRef: scopeRef}
			builders[scopeRef] = builder
		}
		builder.sessionIDs = append(builder.sessionIDs, session.ID)
		builder.claims = append(builder.claims, firstNonEmpty(strings.Join(claim.ClaimLabels, ", "), claim.Axis))
		builder.updatedAt = time.Now().UTC()
		for _, hit := range newHits {
			if hit == nil {
				continue
			}
			switch hit.LandingClass {
			case horizonmodels.SignalLandingClassConfirmed:
				builder.confirmed = append(builder.confirmed, hit.CanonicalRef)
			default:
				builder.inferred = append(builder.inferred, hit.CanonicalRef)
			}
			switch hit.RefKind {
			case "product":
				builder.products = append(builder.products, hit.CanonicalRef)
			case "facility":
				builder.facilities = append(builder.facilities, hit.CanonicalRef)
			case "route", "zone", "geo":
				builder.routes = append(builder.routes, hit.CanonicalRef)
			}
		}
	}
	for _, builder := range builders {
		if builder == nil {
			continue
		}
		if err := s.summariesRepo.Upsert(ctx, builder.Build()); err != nil {
			return err
		}
	}
	return nil
}

func sessionIsRetroAttachActive(session *horizonmodels.SignalLandingSession, now time.Time) bool {
	if session == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(session.Status), "closed") {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	activeUntil := session.ActiveUntil
	if activeUntil.IsZero() {
		base := nonZeroTime(session.UpdatedAt)
		if base.IsZero() {
			base = nonZeroTime(session.CreatedAt)
		}
		activeUntil = base.Add(72 * time.Hour)
	}
	return now.Before(activeUntil)
}

func eligibleRetroAttachFragment(fragment *horizonmodels.HorizonFragment) bool {
	if fragment == nil || strings.TrimSpace(fragment.CanonicalPath) == "" {
		return false
	}
	switch fragment.Type {
	case horizonmodels.FragmentTypeFacility,
		horizonmodels.FragmentTypeGeoRegion,
		horizonmodels.FragmentTypeProduct,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeAffectedZone:
		return true
	default:
		return false
	}
}

func retroAttachCountryScopeRef(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil && strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) != "" {
		return "/loc/" + strings.ToLower(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1))
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(fragment.CanonicalPath), "/"), "/")
	if len(parts) >= 2 && len(parts[1]) == 2 {
		return "/loc/" + strings.ToLower(parts[1])
	}
	return ""
}

func retroAttachTokens(fragment *horizonmodels.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	values := []string{strings.TrimSpace(fragment.CanonicalPath)}
	if fragment.Labels != nil {
		values = append(values, strings.TrimSpace(fragment.Labels["en"]))
	}
	if fragment.HomeLocation != nil {
		values = append(values, strings.TrimSpace(fragment.HomeLocation.Name))
	}
	return tokenizeRetroAttach(values...)
}

func retroAttachHintTokens(claim *horizonmodels.SignalImpactClaim) []string {
	if claim == nil {
		return nil
	}
	values := append(append(append(append(append([]string{}, claim.ClaimLabels...), claim.ClaimAliases...), claim.ProductHints...), claim.RouteHints...), claim.FacilityHints...)
	return tokenizeRetroAttach(values...)
}

func retroAttachSessionHitTokens(hits []*horizonmodels.SignalLandingHit) []string {
	values := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit != nil {
			values = append(values, hit.CanonicalRef)
		}
	}
	return tokenizeRetroAttach(values...)
}

func tokenizeRetroAttach(values ...string) []string {
	out := make([]string, 0, len(values)*4)
	seen := map[string]struct{}{}
	for _, value := range values {
		value = normalizeCandidateLabel(value)
		if value == "" {
			continue
		}
		for _, part := range strings.Fields(value) {
			if len(part) < 3 {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeCandidateAliases(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeCandidateLabel(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return compactUniqueStrings(out)
}

func tokenOverlapCount(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	seen := map[string]struct{}{}
	for _, value := range a {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	count := 0
	for _, value := range b {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			count++
		}
	}
	return count
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func compactUniqueCandidates(values []*horizonmodels.SignalMissingAnchorCandidate) []*horizonmodels.SignalMissingAnchorCandidate {
	seen := map[string]struct{}{}
	out := make([]*horizonmodels.SignalMissingAnchorCandidate, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		key := strings.TrimSpace(value.ID)
		if key == "" {
			key = strings.TrimSpace(value.Label)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

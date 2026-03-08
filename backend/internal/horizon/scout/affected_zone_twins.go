package scout

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type AffectedZoneIdentity struct {
	Geo             string
	ConditionType   string
	ConditionFamily string
	Slug            string
	IdentitySlug    string
}

func ParseAffectedZoneIdentity(canonicalPath string) (AffectedZoneIdentity, bool) {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" || !strings.HasPrefix(cp, "/zone/") {
		return AffectedZoneIdentity{}, false
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) != 4 || parts[0] != "zone" {
		return AffectedZoneIdentity{}, false
	}
	geo := strings.ToLower(strings.TrimSpace(parts[1]))
	conditionType := strings.ToLower(strings.TrimSpace(parts[2]))
	slug := strings.ToLower(strings.TrimSpace(parts[3]))
	if geo == "" || conditionType == "" || slug == "" {
		return AffectedZoneIdentity{}, false
	}
	identitySlug := NormalizeAffectedZoneIdentitySlug(slug)
	if identitySlug == "" {
		identitySlug = slug
	}
	return AffectedZoneIdentity{
		Geo:             geo,
		ConditionType:   conditionType,
		ConditionFamily: normalizeAffectedZoneConditionFamily(conditionType),
		Slug:            slug,
		IdentitySlug:    identitySlug,
	}, true
}

func NormalizeAffectedZoneIdentitySlug(slug string) string {
	tokens := splitZoneSlug(slug)
	if len(tokens) == 0 {
		return ""
	}
	genericSuffixes := map[string]struct{}{
		"area":     {},
		"corridor": {},
		"lanes":    {},
		"maritime": {},
		"passage":  {},
		"region":   {},
		"route":    {},
		"routes":   {},
		"sector":   {},
		"transit":  {},
		"vicinity": {},
		"waters":   {},
		"zone":     {},
	}
	for len(tokens) > 1 {
		last := tokens[len(tokens)-1]
		if _, ok := genericSuffixes[last]; !ok {
			break
		}
		tokens = tokens[:len(tokens)-1]
	}
	return strings.Join(tokens, "-")
}

func FindAffectedZoneTwinCandidates(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	canonicalPath string,
	selfID string,
	limit int,
) ([]*models.HorizonFragment, error) {
	if fragmentsRepo == nil {
		return nil, nil
	}
	identity, ok := ParseAffectedZoneIdentity(canonicalPath)
	if !ok {
		return nil, nil
	}
	if limit <= 0 {
		limit = 128
	}
	items, err := fragmentsRepo.ListByCanonicalPathPrefix(ctx, "/zone/"+identity.Geo+"/", limit)
	if err != nil {
		return nil, err
	}
	out := make([]*models.HorizonFragment, 0, len(items))
	selfID = strings.TrimSpace(selfID)
	for _, frag := range items {
		if frag == nil || frag.Type != models.FragmentTypeAffectedZone {
			continue
		}
		if selfID != "" && strings.TrimSpace(frag.ID) == selfID {
			continue
		}
		candIdentity, ok := ParseAffectedZoneIdentity(frag.CanonicalPath)
		if !ok {
			continue
		}
		if candIdentity.Geo != identity.Geo || candIdentity.ConditionFamily != identity.ConditionFamily || candIdentity.IdentitySlug != identity.IdentitySlug {
			continue
		}
		out = append(out, frag)
	}
	sort.Slice(out, func(i, j int) bool {
		si := AffectedZoneEvidenceScore(out[i])
		sj := AffectedZoneEvidenceScore(out[j])
		if si == sj {
			return strings.TrimSpace(out[i].CanonicalPath) < strings.TrimSpace(out[j].CanonicalPath)
		}
		return si > sj
	})
	return out, nil
}

func SelectAffectedZoneTwinReplacement(incoming *models.HorizonFragment, candidates []*models.HorizonFragment) (*models.HorizonFragment, string) {
	if incoming == nil || incoming.Type != models.FragmentTypeAffectedZone {
		return nil, ""
	}
	incomingIdentity, ok := ParseAffectedZoneIdentity(incoming.CanonicalPath)
	if !ok {
		return nil, ""
	}
	incomingScore := AffectedZoneEvidenceScore(incoming)
	if !isSkeletalAffectedZone(incoming, incomingScore) {
		return nil, ""
	}

	var best *models.HorizonFragment
	bestScore := 0.0
	for _, cand := range candidates {
		if cand == nil || cand.Type != models.FragmentTypeAffectedZone {
			continue
		}
		if strings.TrimSpace(cand.ID) == strings.TrimSpace(incoming.ID) {
			continue
		}
		candIdentity, ok := ParseAffectedZoneIdentity(cand.CanonicalPath)
		if !ok {
			continue
		}
		if candIdentity.Geo != incomingIdentity.Geo || candIdentity.ConditionFamily != incomingIdentity.ConditionFamily || candIdentity.IdentitySlug != incomingIdentity.IdentitySlug {
			continue
		}
		score := AffectedZoneEvidenceScore(cand)
		if score > bestScore {
			best = cand
			bestScore = score
		}
	}
	if best == nil || bestScore < 0.45 || bestScore < incomingScore+0.20 {
		return nil, ""
	}
	return best, fmt.Sprintf(
		"reuse richer affected-zone twin %s (score=%.3f) for skeletal %s (score=%.3f)",
		strings.TrimSpace(best.CanonicalPath), bestScore, strings.TrimSpace(incoming.CanonicalPath), incomingScore,
	)
}

func ShouldAutoMergeAffectedZoneTwin(primary, duplicate *models.HorizonFragment) (bool, string) {
	if primary == nil || duplicate == nil {
		return false, ""
	}
	primaryIdentity, okA := ParseAffectedZoneIdentity(primary.CanonicalPath)
	duplicateIdentity, okB := ParseAffectedZoneIdentity(duplicate.CanonicalPath)
	if !okA || !okB {
		return false, ""
	}
	if primaryIdentity.Geo != duplicateIdentity.Geo ||
		primaryIdentity.ConditionFamily != duplicateIdentity.ConditionFamily ||
		primaryIdentity.IdentitySlug != duplicateIdentity.IdentitySlug {
		return false, ""
	}
	if strings.TrimSpace(primary.CanonicalPath) == strings.TrimSpace(duplicate.CanonicalPath) {
		return false, ""
	}
	primaryScore := AffectedZoneEvidenceScore(primary)
	duplicateScore := AffectedZoneEvidenceScore(duplicate)
	if primaryScore < 0.45 || duplicateScore > 0.22 || primaryScore < duplicateScore+0.20 {
		return false, ""
	}
	if !isSkeletalAffectedZone(duplicate, duplicateScore) {
		return false, ""
	}
	return true, fmt.Sprintf(
		"same geo/family/identity slug (%s/%s/%s), primary %.3f vs duplicate %.3f",
		primaryIdentity.Geo, primaryIdentity.ConditionFamily, primaryIdentity.IdentitySlug, primaryScore, duplicateScore,
	)
}

func AffectedZoneEvidenceScore(fragment *models.HorizonFragment) float64 {
	if fragment == nil || fragment.Type != models.FragmentTypeAffectedZone {
		return 0
	}
	score := 0.0
	score += 0.30 * metricValue(fragment, models.MetricPathImportanceConflictBlastRadius)
	score += 0.26 * metricValue(fragment, models.MetricPathImportanceBusinessBlastRadius)
	score += 0.18 * metricValue(fragment, models.MetricPathImportanceStrategic)
	score += 0.10 * metricValue(fragment, models.MetricPathImportanceBridge)
	if _, _, ok := repo.ResolveFragmentCoordinates(fragment); ok {
		score += 0.06
	}
	if fragment.RadiusKm > 0 {
		score += 0.05
	}
	if hasSubstantiveAffectedZoneDescription(fragment.Description) {
		score += 0.03
	}
	if label := strings.TrimSpace(fragment.Labels["en"]); label != "" {
		score += 0.02
	}
	switch fragment.Metadata.EpistemicStatus {
	case models.EpistemicStatusVerified:
		score += 0.04
	case models.EpistemicStatusLowConfidence:
		score -= 0.02
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func metricValue(fragment *models.HorizonFragment, key string) float64 {
	if fragment == nil || fragment.State == nil {
		return 0
	}
	return fragment.State[key].Value
}

func isSkeletalAffectedZone(fragment *models.HorizonFragment, score float64) bool {
	if fragment == nil {
		return true
	}
	if score <= 0.18 {
		return true
	}
	if score > 0.24 {
		return false
	}
	_, _, hasCoords := repo.ResolveFragmentCoordinates(fragment)
	if hasCoords || fragment.RadiusKm > 0 {
		return false
	}
	if hasSubstantiveAffectedZoneDescription(fragment.Description) {
		return false
	}
	return fragment.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence
}

func normalizeAffectedZoneConditionFamily(conditionType string) string {
	cond := strings.ToLower(strings.TrimSpace(conditionType))
	switch cond {
	case "military_conflict", "political_tension", "security_conflict", "maritime", "maritime_chokepoint", "maritime_passage", "territorial_dispute":
		return "conflict_surface"
	case "natural_disaster", "earthquake_affected", "flood_zone", "cyclone_affected", "storm_affected", "tsunami_affected", "wildfire_affected", "drought":
		return "natural_hazard"
	case "humanitarian_crisis":
		return "humanitarian_crisis"
	case "cyber_disruption":
		return "cyber_disruption"
	default:
		switch {
		case strings.Contains(cond, "conflict"), strings.Contains(cond, "tension"), strings.Contains(cond, "maritime"), strings.Contains(cond, "security"):
			return "conflict_surface"
		case strings.Contains(cond, "disaster"), strings.Contains(cond, "earthquake"), strings.Contains(cond, "flood"), strings.Contains(cond, "cyclone"), strings.Contains(cond, "storm"), strings.Contains(cond, "wildfire"), strings.Contains(cond, "tsunami"), strings.Contains(cond, "drought"):
			return "natural_hazard"
		case strings.Contains(cond, "humanitarian"):
			return "humanitarian_crisis"
		case strings.Contains(cond, "cyber"):
			return "cyber_disruption"
		default:
			return cond
		}
	}
}

func splitZoneSlug(slug string) []string {
	raw := strings.ToLower(strings.TrimSpace(slug))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func hasSubstantiveAffectedZoneDescription(description string) bool {
	desc := strings.TrimSpace(description)
	if desc == "" {
		return false
	}
	lower := strings.ToLower(desc)
	if strings.HasPrefix(lower, "discovered via relation from ") {
		return false
	}
	if strings.HasPrefix(lower, "created via relation from ") {
		return false
	}
	return true
}

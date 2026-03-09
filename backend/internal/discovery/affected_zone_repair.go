package discovery

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type AffectedZoneRepairAction string

const (
	AffectedZoneRepairNoop     AffectedZoneRepairAction = "noop"
	AffectedZoneRepairCooled   AffectedZoneRepairAction = "cooled"
	AffectedZoneRepairUpdated  AffectedZoneRepairAction = "updated"
	AffectedZoneRepairRetarget AffectedZoneRepairAction = "retargeted"
	AffectedZoneRepairArchived AffectedZoneRepairAction = "archived"
)

type AffectedZoneRepairResult struct {
	FragmentID   string
	OldPath      string
	NewPath      string
	Action       AffectedZoneRepairAction
	Reason       string
	Updated      bool
	Archived     bool
	Cooled       bool
	PathMoved    bool
	Merged       bool
	ScoreBefore  float64
	ScoreAfter   float64
	LastMatched  string
	GeoPrecision string
}

type AffectedZoneRepairer struct {
	pipeline *HorizonMonitorPipeline
	migrator *horizonrepo.FragmentPathMigrator
}

func NewAffectedZoneRepairer(
	pipeline *HorizonMonitorPipeline,
	migrator *horizonrepo.FragmentPathMigrator,
) *AffectedZoneRepairer {
	return &AffectedZoneRepairer{
		pipeline: pipeline,
		migrator: migrator,
	}
}

func (r *AffectedZoneRepairer) RepairFragment(
	ctx context.Context,
	tenantID string,
	frag *horizonmodels.HorizonFragment,
	now time.Time,
	sourceID string,
	apply bool,
) (*AffectedZoneRepairResult, error) {
	if r == nil || r.pipeline == nil || r.pipeline.fragmentsRepo == nil {
		return nil, fmt.Errorf("affected zone repairer is not configured")
	}
	if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	st := parseAffectedZoneState(frag)
	scoreBefore := st.Score
	st.applyDecay(now)
	scoreAfter := st.Score

	event := synthesizeAffectedZoneEventFromFragment(frag)
	classification := synthesizeAffectedZoneClassificationFromFragment(frag)
	candidate := inferAffectedZoneCandidate(event, classification)

	result := &AffectedZoneRepairResult{
		FragmentID:   strings.TrimSpace(frag.ID),
		OldPath:      strings.TrimSpace(frag.CanonicalPath),
		NewPath:      strings.TrimSpace(frag.CanonicalPath),
		Action:       AffectedZoneRepairNoop,
		ScoreBefore:  scoreBefore,
		ScoreAfter:   scoreAfter,
		LastMatched:  st.LastMatchedAt.UTC().Format(time.RFC3339),
		GeoPrecision: strings.TrimSpace(qualifierStringValue(frag.Qualifiers, "geoPrecision")),
	}

	if candidate == nil {
		if shouldArchiveAffectedZoneFragment(frag, event, classification, st, now) {
			result.Action = AffectedZoneRepairArchived
			result.Archived = true
			result.Reason = affectedZoneArchiveReason(frag, event, classification, st, now)
			if apply {
				archiveAffectedZoneFragment(frag, result.Reason, st, now, sourceID, event)
				if err := r.pipeline.fragmentsRepo.Update(ctx, frag); err != nil {
					return result, err
				}
				result.Updated = true
			}
			return result, nil
		}
		candidate = fallbackAffectedZoneCandidateFromFragment(frag)
		if candidate != nil && reconcileAffectedZoneFragmentForRepair(frag, candidate, classification, st, now, sourceID, event) {
			result.Action = AffectedZoneRepairUpdated
			result.Reason = "legacy affected zone backfilled from canonical path"
			if apply {
				if err := r.pipeline.fragmentsRepo.Update(ctx, frag); err != nil {
					return result, err
				}
				result.Updated = true
			}
			result.ScoreAfter = parseAffectedZoneState(frag).Score
			result.GeoPrecision = strings.TrimSpace(qualifierStringValue(frag.Qualifiers, "geoPrecision"))
			return result, nil
		}
		if applyZoneStateRepair(frag, st, now, sourceID, event) {
			result.Action = AffectedZoneRepairCooled
			result.Cooled = true
			if apply {
				if err := r.pipeline.fragmentsRepo.Update(ctx, frag); err != nil {
					return result, err
				}
				result.Updated = true
			}
		}
		return result, nil
	}

	targetPath := strings.TrimSpace(candidate.CanonicalPath)
	if targetPath != "" && targetPath != strings.TrimSpace(frag.CanonicalPath) {
		result.Action = AffectedZoneRepairRetarget
		result.NewPath = targetPath
		result.Reason = "candidate path refined by updated heuristics"
		if apply && r.migrator != nil {
			moveRes, err := r.migrator.MoveCanonicalPath(ctx, strings.TrimSpace(frag.ID), targetPath, "affected_zone_repair")
			if err != nil {
				return result, err
			}
			result.PathMoved = true
			result.Merged = moveRes != nil && moveRes.MergedIntoExisting
			if updated, err := r.pipeline.fragmentsRepo.GetByCanonicalPath(ctx, targetPath); err == nil && updated != nil {
				frag = updated
			}
		}
	}

	if reconcileAffectedZoneFragmentForRepair(frag, candidate, classification, st, now, sourceID, event) {
		if result.Action == AffectedZoneRepairNoop {
			result.Action = AffectedZoneRepairUpdated
			result.Reason = "affected zone updated by improved heuristics"
		}
		if apply {
			if err := r.pipeline.fragmentsRepo.Update(ctx, frag); err != nil {
				return result, err
			}
			result.Updated = true
		}
	}
	result.ScoreAfter = parseAffectedZoneState(frag).Score
	result.GeoPrecision = strings.TrimSpace(qualifierStringValue(frag.Qualifiers, "geoPrecision"))
	return result, nil
}

func fallbackAffectedZoneCandidateFromFragment(f *horizonmodels.HorizonFragment) *affectedZoneCandidate {
	if f == nil {
		return nil
	}
	geoToken, conditionType, slug, ok := parseZoneCanonicalPath(strings.TrimSpace(f.CanonicalPath))
	if !ok {
		return nil
	}
	zoneType, zoneSubType := fallbackZoneTypeAndSubtype(conditionType)
	if zoneType == "" {
		return nil
	}
	label := strings.TrimSpace(f.Labels["en"])
	slugPlace := strings.TrimSpace(strings.ReplaceAll(slug, "-", " "))
	if label == "" {
		label = humanizeZoneLabel(slugPlace, countryNameFromCode(geoToken), zoneSubType)
	}
	placeSource := label
	if !isPlausiblePlaceName(label) || looksLikeBadAffectedZonePlaceHint(label) {
		placeSource = slugPlace
	}
	place := normalizeAffectedZonePlace(firstNonEmptyString(placeSource, slugPlace), countryNameFromCode(currentAffectedZoneCountryCode(f)))
	candidateCountryCode := strings.ToUpper(currentAffectedZoneCountryCode(f))
	if candidateCountryCode == "" && len(strings.TrimSpace(geoToken)) == 2 {
		candidateCountryCode = strings.ToUpper(strings.TrimSpace(geoToken))
	}
	candidateCountryName := countryNameFromCode(candidateCountryCode)
	if inferredGeo, inferredCC, inferredCountry := inferZoneGeoFromPlace(place); inferredGeo != "" {
		switch inferredGeo {
		case "sea", "tw-cn":
			geoToken = inferredGeo
			candidateCountryCode = ""
			candidateCountryName = strings.TrimSpace(inferredCountry)
		default:
			if inferredCC != "" {
				candidateCountryCode = strings.ToUpper(strings.TrimSpace(inferredCC))
				candidateCountryName = firstNonEmptyString(strings.TrimSpace(inferredCountry), countryNameFromCode(candidateCountryCode))
				geoToken = strings.ToLower(candidateCountryCode)
			}
		}
	}
	candidate := &affectedZoneCandidate{
		CanonicalPath: "/zone/" + strings.TrimSpace(geoToken) + "/" + strings.TrimSpace(conditionType) + "/" + strings.TrimSpace(slug),
		Label:         label,
		ConditionType: conditionType,
		ZoneType:      zoneType,
		ZoneSubType:   zoneSubType,
		GeoToken:      geoToken,
		CountryCode:   candidateCountryCode,
		CountryName:   candidateCountryName,
		RadiusKm:      f.RadiusKm,
		GeoPrecision:  strings.TrimSpace(firstNonEmptyString(qualifierStringValue(f.Qualifiers, "geoPrecision"), "none")),
		WarmOnly:      false,
		Reason:        "legacy_path_backfill",
	}
	if f.Coordinates != nil && horizonmodels.IsRealGeoCoordinate(f.Coordinates.Lat, f.Coordinates.Lon) {
		candidate.Lat = f.Coordinates.Lat
		candidate.Lon = f.Coordinates.Lon
		candidate.HasCoordinates = true
	}
	if !candidate.HasCoordinates {
		if lat, lon, ok := knownZoneHotspotCoordinates(place, candidate.CountryCode); ok {
			candidate.Lat = lat
			candidate.Lon = lon
			candidate.HasCoordinates = true
			candidate.GeoPrecision = "precise"
		} else if lat, lon, ok := countryCentroid(candidate.CountryCode); ok {
			candidate.Lat = lat
			candidate.Lon = lon
			candidate.HasCoordinates = true
			candidate.GeoPrecision = "approximate_country"
		}
	}
	if candidate.RadiusKm <= 0 {
		candidate.RadiusKm = inferZoneRadiusKm(conditionType, qualifierStringValue(f.Qualifiers, "classificationLevel"), "")
	}
	return candidate
}

func fallbackZoneTypeAndSubtype(conditionType string) (string, string) {
	switch strings.TrimSpace(conditionType) {
	case "military_conflict":
		return "military_conflict", "armed_tension"
	case "political_tension":
		return "political_tension", "civil_tension"
	case "cyber_disruption":
		return "infrastructure_disruption", "systems_disruption"
	case "infrastructure_disruption":
		return "infrastructure_disruption", "systems_disruption"
	case "earthquake_affected":
		return "natural_disaster", "earthquake"
	case "tsunami_affected":
		return "natural_disaster", "tsunami"
	case "volcanic_eruption":
		return "natural_disaster", "volcanic_eruption"
	case "flood_zone":
		return "natural_disaster", "flood"
	case "wildfire_zone":
		return "natural_disaster", "wildfire"
	case "natural_disaster":
		return "natural_disaster", "multi_hazard"
	case "disease_outbreak":
		return "humanitarian_crisis", "epidemic"
	case "humanitarian_crisis":
		return "humanitarian_crisis", "humanitarian_emergency"
	case "environmental_hazard":
		return "natural_disaster", "toxic_pollution"
	case "maritime_chokepoint", "navigation_route":
		return "infrastructure_disruption", "maritime_route"
	default:
		return "", ""
	}
}

func synthesizeAffectedZoneEventFromFragment(f *horizonmodels.HorizonFragment) *horizonMonitorFeedEvent {
	if f == nil {
		return nil
	}
	event := &horizonMonitorFeedEvent{
		SourceID:   "affected_zone_repair",
		FeedType:   "affected_zone_repair",
		Category:   qualifierStringValue(f.Qualifiers, "classificationCategory"),
		ExternalID: strings.TrimSpace(f.Identifiers["sourceObjectId"]),
		Title: firstNonEmptyString(
			qualifierStringValue(f.Qualifiers, "sourceTitle"),
			strings.TrimSpace(f.Description),
			strings.TrimSpace(f.Labels["en"]),
		),
		Summary: firstNonEmptyString(
			qualifierStringValue(f.Qualifiers, "sourceSummary"),
			strings.TrimSpace(f.Description),
		),
		URL:         qualifierStringValue(f.Qualifiers, "sourceUrl"),
		CountryCode: firstNonEmptyString(qualifierStringValue(f.Qualifiers, "sourceCountryCode"), currentAffectedZoneCountryCode(f)),
		CountryName: countryNameFromCode(firstNonEmptyString(qualifierStringValue(f.Qualifiers, "sourceCountryCode"), currentAffectedZoneCountryCode(f))),
		Severity:    strings.ToLower(strings.TrimSpace(qualifierStringValue(f.Qualifiers, "classificationLevel"))),
		Confidence:  0.70,
	}
	text := strings.Join([]string{strings.TrimSpace(event.Title), strings.TrimSpace(event.Summary), strings.TrimSpace(event.Category), strings.TrimSpace(event.Severity)}, " ")
	if event.CountryCode != "" && shouldTrustAffectedZoneCountryFallback(event, text, qualifierStringValue(f.Qualifiers, "conditionType"), event.CountryCode) {
		event.LocationPath = locationPathFromCountryCode(event.CountryCode)
	}
	if f.Coordinates != nil && horizonmodels.IsRealGeoCoordinate(f.Coordinates.Lat, f.Coordinates.Lon) {
		event.Lat = f.Coordinates.Lat
		event.Lon = f.Coordinates.Lon
		event.HasCoordinates = true
	}
	if observed := f.Provenance.ObservedAt; observed != nil && !observed.IsZero() {
		event.OccurredAt = observed.UTC()
	}
	if event.OccurredAt.IsZero() {
		if ts := metricValueFloat(f.State, zoneMetricLastMatchTS); ts > 0 {
			event.OccurredAt = time.Unix(int64(ts), 0).UTC()
		}
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = f.UpdatedAt.UTC()
	}
	if event.Title == "" {
		event.Title = strings.TrimSpace(f.Labels["en"])
	}
	if event.Summary == "" {
		event.Summary = event.Title
	}
	return event
}

func synthesizeAffectedZoneClassificationFromFragment(f *horizonmodels.HorizonFragment) horizonMonitorNewsClassification {
	if f == nil {
		return horizonMonitorNewsClassification{}
	}
	return horizonMonitorNewsClassification{
		Level:      qualifierStringValue(f.Qualifiers, "classificationLevel"),
		Category:   qualifierStringValue(f.Qualifiers, "classificationCategory"),
		Confidence: 0.70,
		Source:     firstNonEmptyString(qualifierStringValue(f.Qualifiers, "classificationSource"), "repair"),
	}
}

func reconcileAffectedZoneFragmentForRepair(
	f *horizonmodels.HorizonFragment,
	candidate *affectedZoneCandidate,
	cls horizonMonitorNewsClassification,
	st affectedZoneState,
	now time.Time,
	sourceID string,
	e *horizonMonitorFeedEvent,
) bool {
	if f == nil || candidate == nil {
		return false
	}
	changed := false
	if f.Labels == nil {
		f.Labels = map[string]string{}
	}
	if label := strings.TrimSpace(candidate.Label); label != "" && strings.TrimSpace(f.Labels["en"]) != label {
		f.Labels["en"] = label
		changed = true
	}
	if candidate.RadiusKm > 0 && (f.RadiusKm <= 0 || math.Abs(f.RadiusKm-candidate.RadiusKm) > 0.1) {
		f.RadiusKm = candidate.RadiusKm
		changed = true
	}
	if applyAffectedZoneCoordinates(f, candidate) {
		changed = true
	}
	if applyZoneStateRepair(f, st, now, sourceID, e) {
		changed = true
	}
	if f.Qualifiers == nil {
		f.Qualifiers = map[string]interface{}{}
	}
	for k, v := range affectedZoneQualifiers(candidate, cls, st, e) {
		if fmt.Sprintf("%v", f.Qualifiers[k]) != fmt.Sprintf("%v", v) {
			f.Qualifiers[k] = v
			changed = true
		}
	}
	delete(f.Qualifiers, "staleObservationIgnored")
	delete(f.Qualifiers, "repairArchivedReason")
	delete(f.Qualifiers, "repairArchivedAt")
	if f.Metadata.LastVerifiedAt.Before(now) {
		f.Metadata.LastVerifiedAt = now
		changed = true
	}
	if f.Metadata.LastUpdatedAt.Before(now) {
		f.Metadata.LastUpdatedAt = now
		changed = true
	}
	if strings.TrimSpace(f.Status) != string(horizonmodels.FragmentStatusActive) {
		f.Status = string(horizonmodels.FragmentStatusActive)
		changed = true
	}
	if strings.TrimSpace(f.Governance.Status) != string(horizonmodels.FragmentStatusActive) {
		f.Governance.Status = string(horizonmodels.FragmentStatusActive)
		changed = true
	}
	return changed
}

func applyZoneStateRepair(
	f *horizonmodels.HorizonFragment,
	st affectedZoneState,
	now time.Time,
	sourceID string,
	e *horizonMonitorFeedEvent,
) bool {
	if f == nil {
		return false
	}
	prev := parseAffectedZoneState(f)
	if math.Abs(prev.Score-st.Score) < 0.05 &&
		math.Abs(prev.Hits1h-st.Hits1h) < 0.05 &&
		math.Abs(prev.Hits24h-st.Hits24h) < 0.05 &&
		math.Abs(prev.TotalMentions-st.TotalMentions) < 0.05 {
		return false
	}
	applyAffectedZoneState(f, st, now, st.LastMatchedAt, sourceID, e)
	return true
}

func shouldArchiveAffectedZoneFragment(
	f *horizonmodels.HorizonFragment,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	st affectedZoneState,
	now time.Time,
) bool {
	if f == nil {
		return false
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	if genericGlobalAffectedZonePath(cp) {
		return true
	}
	if looksLikeBadGlobalAffectedZoneSlug(cp) {
		return true
	}
	if geo, _, _, ok := parseZoneCanonicalPath(cp); ok && geo == "global" {
		place := currentAffectedZonePlace(f)
		if looksLikeBadAffectedZonePlaceHint(place) || isGenericAffectedZonePlace(place) {
			return true
		}
		if !isLegitResidualGlobalAffectedZonePlace(place) {
			return true
		}
	}
	if currentPlace := currentAffectedZonePlace(f); currentPlace != "" {
		if cc := currentAffectedZoneCountryCode(f); cc != "" {
			if placeCC := inferCountryCodeFromText(currentPlace); placeCC != "" && !strings.EqualFold(placeCC, cc) {
				return true
			}
		}
	}
	text := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary), strings.TrimSpace(cls.Category), strings.TrimSpace(cls.Level)}, " "))
	if shouldSuppressAffectedZoneBySourceProfile(e) || shouldSuppressAffectedZoneBySemanticDrift(text, e, cls) {
		return true
	}
	if shouldSuppressAffectedZoneByEventProfile(text, e, cls, qualifierStringValue(f.Qualifiers, "conditionType"), wmHeuristics().ProfileArticle(text, affectedZoneSourceHost(e))) {
		return true
	}
	if shouldSuppressWeakAffectedZoneInference(
		text,
		e,
		cls,
		qualifierStringValue(f.Qualifiers, "conditionType"),
		toFloatDefault(qualifierStringValue(f.Qualifiers, "zoneInferenceScore"), 0),
	) {
		return true
	}
	limit := affectedZoneFreshnessLimit(qualifierStringValue(f.Qualifiers, "conditionType"))
	if limit <= 0 {
		limit = 21 * 24 * time.Hour
	}
	if !st.LastMatchedAt.IsZero() && now.Sub(st.LastMatchedAt) > (limit*3) && st.Score < 8 {
		return true
	}
	return false
}

func affectedZoneArchiveReason(
	f *horizonmodels.HorizonFragment,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	st affectedZoneState,
	now time.Time,
) string {
	cp := strings.TrimSpace(f.CanonicalPath)
	if genericGlobalAffectedZonePath(cp) {
		return "generic global affected zone"
	}
	if looksLikeBadGlobalAffectedZoneSlug(cp) {
		return "global affected zone with headline-like slug"
	}
	if geo, _, _, ok := parseZoneCanonicalPath(cp); ok && geo == "global" {
		place := currentAffectedZonePlace(f)
		if looksLikeBadAffectedZonePlaceHint(place) || isGenericAffectedZonePlace(place) {
			return "global affected zone with weak place hint"
		}
		if !isLegitResidualGlobalAffectedZonePlace(place) {
			return "global affected zone without stable geospatial anchor"
		}
	}
	if currentPlace := currentAffectedZonePlace(f); currentPlace != "" {
		if cc := currentAffectedZoneCountryCode(f); cc != "" {
			if placeCC := inferCountryCodeFromText(currentPlace); placeCC != "" && !strings.EqualFold(placeCC, cc) {
				return "country/place mismatch"
			}
		}
	}
	text := normalizeKeywordText(strings.Join([]string{strings.TrimSpace(e.Title), strings.TrimSpace(e.Summary), strings.TrimSpace(cls.Category), strings.TrimSpace(cls.Level)}, " "))
	if shouldSuppressAffectedZoneBySourceProfile(e) {
		return "suppressed by source profile"
	}
	if shouldSuppressAffectedZoneBySemanticDrift(text, e, cls) {
		return "semantic drift / non-event article"
	}
	if shouldSuppressAffectedZoneByEventProfile(text, e, cls, qualifierStringValue(f.Qualifiers, "conditionType"), wmHeuristics().ProfileArticle(text, affectedZoneSourceHost(e))) {
		return "suppressed by event profile"
	}
	if shouldSuppressWeakAffectedZoneInference(
		text,
		e,
		cls,
		qualifierStringValue(f.Qualifiers, "conditionType"),
		toFloatDefault(qualifierStringValue(f.Qualifiers, "zoneInferenceScore"), 0),
	) {
		return "weak affected zone inference"
	}
	return "stale low-confidence affected zone"
}

func toFloatDefault(raw string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
		return v
	}
	return fallback
}

func archiveAffectedZoneFragment(
	f *horizonmodels.HorizonFragment,
	reason string,
	st affectedZoneState,
	now time.Time,
	sourceID string,
	e *horizonMonitorFeedEvent,
) {
	if f == nil {
		return
	}
	applyAffectedZoneState(f, st, now, st.LastMatchedAt, sourceID, e)
	if f.Qualifiers == nil {
		f.Qualifiers = map[string]interface{}{}
	}
	f.Qualifiers["repairArchivedReason"] = strings.TrimSpace(reason)
	f.Qualifiers["repairArchivedAt"] = now.UTC().Format(time.RFC3339)
	f.Qualifiers["hypeStatus"] = "inactive"
	f.Qualifiers["hypeIndicator"] = "cold"
	f.Status = string(horizonmodels.FragmentStatusArchived)
	f.Governance.Status = string(horizonmodels.FragmentStatusArchived)
	f.Metadata.LastUpdatedAt = now
	f.Metadata.LastVerifiedAt = now
	f.Metadata.EpistemicStatus = horizonmodels.EpistemicStatusLowConfidence
}

func genericGlobalAffectedZonePath(canonicalPath string) bool {
	geo, _, slug, ok := parseZoneCanonicalPath(canonicalPath)
	if !ok {
		return false
	}
	slug = strings.TrimSpace(strings.ToLower(slug))
	return geo == "global" && (slug == "" || slug == "global" || slug == "emerging-hotspot")
}

func looksLikeBadGlobalAffectedZoneSlug(canonicalPath string) bool {
	geo, _, slug, ok := parseZoneCanonicalPath(canonicalPath)
	if !ok || geo != "global" {
		return false
	}
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return false
	}
	if strings.Contains(slug, "decision-") ||
		strings.Contains(slug, "authorize") ||
		strings.Contains(slug, "last-24-hours") ||
		strings.Contains(slug, "but-warn") ||
		strings.Contains(slug, "analysis") ||
		strings.Contains(slug, "report") ||
		strings.Contains(slug, "officials-say") ||
		strings.Contains(slug, "international-women") ||
		strings.Contains(slug, "as-presidents") ||
		strings.Contains(slug, "force-against") {
		return true
	}
	if strings.Count(slug, "-") >= 4 &&
		!strings.Contains(slug, "strait") &&
		!strings.Contains(slug, "canal") &&
		!strings.Contains(slug, "sea") &&
		!strings.Contains(slug, "gulf") &&
		!strings.Contains(slug, "latin-america") &&
		!strings.Contains(slug, "middle-east") {
		return true
	}
	return false
}

func currentAffectedZonePlace(f *horizonmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if label := strings.TrimSpace(f.Labels["en"]); label != "" {
		return label
	}
	_, _, slug, ok := parseZoneCanonicalPath(strings.TrimSpace(f.CanonicalPath))
	if !ok {
		return ""
	}
	return strings.ReplaceAll(strings.TrimSpace(slug), "-", " ")
}

func currentAffectedZoneCountryCode(f *horizonmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if cc := strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceCountryCode")); len(cc) == 2 {
		return strings.ToUpper(cc)
	}
	geo, _, _, ok := parseZoneCanonicalPath(strings.TrimSpace(f.CanonicalPath))
	if ok && len(strings.TrimSpace(geo)) == 2 {
		return strings.ToUpper(strings.TrimSpace(geo))
	}
	return affectedZoneCountryFromFragment(f)
}

func isLegitResidualGlobalAffectedZonePlace(place string) bool {
	place = strings.TrimSpace(place)
	if place == "" {
		return false
	}
	geo, _, _ := inferZoneGeoFromPlace(place)
	if geo == "sea" || geo == "tw-cn" {
		return true
	}
	return false
}

func qualifierStringValue(qualifiers map[string]interface{}, key string) string {
	if qualifiers == nil || strings.TrimSpace(key) == "" {
		return ""
	}
	v, ok := qualifiers[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func metricValueFloat(state map[string]horizonmodels.MetricValue, key string) float64 {
	if state == nil {
		return 0
	}
	return state[key].Value
}

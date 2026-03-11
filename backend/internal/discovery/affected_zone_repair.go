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

	if candidate == nil {
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
	preferStoredSource := shouldUseAffectedZoneStoredSourceEvidence(f)
	sourceTitle := ""
	sourceSummary := ""
	sourceURL := ""
	sourceCountryCode := ""
	if preferStoredSource {
		sourceTitle = qualifierStringValue(f.Qualifiers, "sourceTitle")
		sourceSummary = qualifierStringValue(f.Qualifiers, "sourceSummary")
		sourceURL = qualifierStringValue(f.Qualifiers, "sourceUrl")
		sourceCountryCode = qualifierStringValue(f.Qualifiers, "sourceCountryCode")
	}
	event := &horizonMonitorFeedEvent{
		SourceID:   "affected_zone_repair",
		FeedType:   "affected_zone_repair",
		Category:   qualifierStringValue(f.Qualifiers, "classificationCategory"),
		ExternalID: strings.TrimSpace(f.Identifiers["sourceObjectId"]),
		Title: firstNonEmptyString(
			sourceTitle,
			strings.TrimSpace(f.Description),
			strings.TrimSpace(f.Labels["en"]),
		),
		Summary: firstNonEmptyString(
			sourceSummary,
			strings.TrimSpace(f.Description),
		),
		URL:         sourceURL,
		CountryCode: firstNonEmptyString(sourceCountryCode, currentAffectedZoneCountryCode(f)),
		CountryName: countryNameFromCode(firstNonEmptyString(qualifierStringValue(f.Qualifiers, "sourceCountryCode"), currentAffectedZoneCountryCode(f))),
		Severity:    strings.ToLower(strings.TrimSpace(qualifierStringValue(f.Qualifiers, "classificationLevel"))),
		Confidence:  0.70,
	}
	text := strings.Join([]string{strings.TrimSpace(event.Title), strings.TrimSpace(event.Summary), strings.TrimSpace(event.Category), strings.TrimSpace(event.Severity)}, " ")
	textBody := strings.Join([]string{strings.TrimSpace(event.Title), strings.TrimSpace(event.Summary)}, " ")
	if inferred := inferCountryCodeFromText(strings.Join([]string{strings.TrimSpace(event.Title), strings.TrimSpace(event.Summary)}, " ")); inferred != "" {
		if (strings.TrimSpace(event.CountryCode) == "" || looksLikeAdministrativeCommentaryText(text) || looksLikeLiveBlogPoliticalCommentaryText(text) || strings.Contains(strings.ToLower(strings.TrimSpace(event.Title)), ":")) && !strings.EqualFold(inferred, event.CountryCode) {
			event.CountryCode = inferred
			event.CountryName = countryNameFromCode(inferred)
		}
	}
	if place := extractKnownPlaceFromText(textBody); place != "" {
		if _, placeCC, placeCountry := inferZoneGeoFromPlace(place); placeCC != "" && !strings.EqualFold(placeCC, event.CountryCode) {
			event.CountryCode = placeCC
			event.CountryName = firstNonEmptyString(strings.TrimSpace(placeCountry), countryNameFromCode(placeCC))
		}
	}
	if explicitCC := inferCountryCodeFromAffectedZoneText(textBody, event.CountryCode); explicitCC != "" && !strings.EqualFold(explicitCC, event.CountryCode) {
		event.CountryCode = explicitCC
		event.CountryName = countryNameFromCode(explicitCC)
	}
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
		if ts := parseRFC3339Loose(qualifierStringValue(f.Qualifiers, "sourceOccurredAt")); !ts.IsZero() {
			event.OccurredAt = ts.UTC()
		}
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
	if !shouldUseAffectedZoneStoredSourceEvidence(f) {
		if clearAffectedZonePrimaryEvidence(f) {
			changed = true
		}
		if desc := affectedZoneNarrativeDescription(candidate); desc != "" && strings.TrimSpace(f.Description) != desc {
			f.Description = desc
			changed = true
		}
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
	if hasAffectedZoneIdentityDrift(f) {
		return true
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
	if title := strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceTitle")); title != "" {
		summary := strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceSummary"))
		if isSyntheticAffectedZoneNarrative(title) && (summary == "" || strings.EqualFold(summary, title)) {
			return true
		}
	}
	if currentPlace := currentAffectedZonePlace(f); currentPlace != "" {
		if looksLikeMilitaryAssetName(normalizeKeywordText(currentPlace)) {
			return true
		}
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
	if isStaleGenericCountryAffectedZone(f, st, now) {
		return true
	}
	if isStaleSemanticallyInvalidAffectedZone(f, e, cls, st, now) {
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
	if hasAffectedZoneIdentityDrift(f) {
		return "identity drift / corrupted zone anchor"
	}
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
	if title := strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceTitle")); title != "" {
		summary := strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceSummary"))
		if isSyntheticAffectedZoneNarrative(title) && (summary == "" || strings.EqualFold(summary, title)) {
			return "synthetic affected zone narrative"
		}
	}
	if currentPlace := currentAffectedZonePlace(f); currentPlace != "" {
		if looksLikeMilitaryAssetName(normalizeKeywordText(currentPlace)) {
			return "military asset mistaken for place"
		}
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
	if isStaleGenericCountryAffectedZone(f, st, now) {
		return "stale generic country-level affected zone"
	}
	if isStaleSemanticallyInvalidAffectedZone(f, e, cls, st, now) {
		return "stale semantically invalid affected zone"
	}
	return "stale low-confidence affected zone"
}

func hasAffectedZoneIdentityDrift(f *horizonmodels.HorizonFragment) bool {
	if f == nil {
		return false
	}
	geo, _, slug, ok := parseZoneCanonicalPath(strings.TrimSpace(f.CanonicalPath))
	if !ok {
		return false
	}
	canonicalGeo := strings.ToUpper(strings.TrimSpace(geo))
	canonicalPlace := normalizePhrase(strings.ReplaceAll(strings.TrimSpace(slug), "-", " "))
	if canonicalPlace == "" {
		canonicalPlace = normalizePhrase(currentAffectedZonePlace(f))
	}
	zoneInference := toFloatDefault(qualifierStringValue(f.Qualifiers, "zoneInferenceScore"), 0)
	if strings.EqualFold(strings.TrimSpace(qualifierStringValue(f.Qualifiers, "geoPrecision")), "precise") && canonicalPlace != "" {
		if legal := normalizePhrase(strings.TrimSpace(f.Identifiers["legalName"])); legal != "" && strings.Contains(legal, canonicalPlace) {
			if zoneInference > 1.5 {
				return false
			}
		}
	}
	if isSyntheticAffectedZoneNarrative(strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceTitle"))) &&
		strings.EqualFold(strings.TrimSpace(qualifierStringValue(f.Qualifiers, "geoPrecision")), "approximate_country") {
		return true
	}
	fields := []string{
		strings.TrimSpace(f.Identifiers["legalName"]),
		strings.TrimSpace(f.Identifiers["merged_paths"]),
	}
	if f.HomeLocation != nil {
		fields = append(fields, strings.TrimSpace(f.HomeLocation.Name))
	}
	for _, field := range fields {
		norm := normalizePhrase(field)
		if norm == "" {
			continue
		}
		if canonicalPlace != "" && strings.Contains(norm, canonicalPlace) {
			continue
		}
		if candidatePlace := extractKnownPlaceFromText(field); candidatePlace != "" && canonicalPlace != "" && normalizePhrase(candidatePlace) != canonicalPlace {
			return true
		}
		if len(canonicalGeo) == 2 {
			if cc := inferCountryCodeFromText(field); cc != "" && !strings.EqualFold(cc, canonicalGeo) {
				return true
			}
		}
	}
	sourceCC := strings.ToUpper(strings.TrimSpace(qualifierStringValue(f.Qualifiers, "sourceCountryCode")))
	if len(canonicalGeo) == 2 && sourceCC != "" && !strings.EqualFold(sourceCC, canonicalGeo) {
		sourceTitle := normalizeKeywordText(strings.Join([]string{
			qualifierStringValue(f.Qualifiers, "sourceTitle"),
			qualifierStringValue(f.Qualifiers, "sourceSummary"),
		}, " "))
		if looksLikeAdministrativeCommentaryText(sourceTitle) || strings.Contains(sourceTitle, "factsheet") || strings.Contains(sourceTitle, "policy paper") {
			return true
		}
	}
	if zoneInference <= 1.5 {
		if mergedNorm := normalizePhrase(strings.TrimSpace(f.Identifiers["merged_paths"])); mergedNorm != "" && canonicalPlace != "" && !strings.Contains(mergedNorm, canonicalPlace) {
			return true
		}
	}
	return false
}

func isSyntheticAffectedZoneNarrative(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	norm := normalizePhrase(raw)
	return strings.Contains(norm, "kinetic operations") ||
		strings.Contains(norm, "mass displacement") ||
		strings.Contains(norm, "civil unrest") ||
		strings.Contains(norm, "cyber outage") ||
		strings.Contains(norm, "cyber disruption") ||
		strings.Contains(norm, "systems disruption") ||
		strings.Contains(norm, "critical infra")
}

func isStaleGenericCountryAffectedZone(f *horizonmodels.HorizonFragment, st affectedZoneState, now time.Time) bool {
	if f == nil || st.LastMatchedAt.IsZero() {
		return false
	}
	geo, _, slug, ok := parseZoneCanonicalPath(strings.TrimSpace(f.CanonicalPath))
	if !ok || len(strings.TrimSpace(geo)) != 2 {
		return false
	}
	countryName := normalizeKeywordText(countryNameFromCode(geo))
	if countryName == "" {
		return false
	}
	place := normalizeKeywordText(currentAffectedZonePlace(f))
	slugNorm := normalizeKeywordText(strings.ReplaceAll(strings.TrimSpace(slug), "-", " "))
	if place != "" && place != countryName {
		return false
	}
	if place == "" && slugNorm != countryName {
		return false
	}
	if now.Sub(st.LastMatchedAt) <= 365*24*time.Hour {
		return false
	}
	if st.Score >= 12 {
		return false
	}
	return true
}

func isStaleSemanticallyInvalidAffectedZone(
	f *horizonmodels.HorizonFragment,
	e *horizonMonitorFeedEvent,
	cls horizonMonitorNewsClassification,
	st affectedZoneState,
	now time.Time,
) bool {
	if f == nil || e == nil {
		return false
	}
	age := now.Sub(st.LastMatchedAt)
	if st.LastMatchedAt.IsZero() || age <= 365*24*time.Hour {
		return false
	}
	text := normalizeKeywordText(strings.Join([]string{
		strings.TrimSpace(e.Title),
		strings.TrimSpace(e.Summary),
		strings.TrimSpace(cls.Category),
		strings.TrimSpace(cls.Level),
	}, " "))
	if text == "" {
		return false
	}
	host := affectedZoneSourceHost(e)
	conditionType := qualifierStringValue(f.Qualifiers, "conditionType")
	profile := wmHeuristics().ProfileArticle(text, host)
	if shouldSuppressAffectedZoneBySourceProfile(e) ||
		shouldSuppressAffectedZoneBySemanticDrift(text, e, cls) ||
		shouldSuppressAffectedZoneByEventProfile(text, e, cls, conditionType, profile) ||
		shouldSuppressWeakAffectedZoneInference(text, e, cls, conditionType, toFloatDefault(qualifierStringValue(f.Qualifiers, "zoneInferenceScore"), 0)) {
		return true
	}
	if !e.OccurredAt.IsZero() && now.Sub(e.OccurredAt) > 365*24*time.Hour && profile.AnalysisScore >= 0.85 {
		return true
	}
	return false
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

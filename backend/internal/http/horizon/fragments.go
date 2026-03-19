package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type geoObjectResponseItem struct {
	ID              string              `json:"id"`
	CanonicalPath   string              `json:"canonicalPath"`
	Type            models.FragmentType `json:"type"`
	FacilityKind    string              `json:"facilityKind,omitempty"`
	Label           string              `json:"label,omitempty"`
	Description     string              `json:"description,omitempty"`
	Status          string              `json:"status,omitempty"`
	Coordinates     *models.Coordinates `json:"coordinates,omitempty"`
	RadiusKm        float64             `json:"radiusKm,omitempty"`
	ZoneType        string              `json:"zoneType,omitempty"`
	ZoneSubType     string              `json:"zoneSubType,omitempty"`
	GeoPrecision    string              `json:"geoPrecision,omitempty"`
	SourceLayer     string              `json:"sourceLayer,omitempty"`
	ProcessingLayer string              `json:"processingLayer,omitempty"`
	Hype            float64             `json:"hype,omitempty"`
	HypeIndicator   int                 `json:"hypeIndicator,omitempty"`
	ActivityStatus  string              `json:"activityStatus,omitempty"`
	ConflictLinked  bool                `json:"conflictLinked,omitempty"`
	ThreatClass     string              `json:"threatClass,omitempty"`
	LastMatchedAt   *time.Time          `json:"lastMatchedAt,omitempty"`
	InScope         bool                `json:"inScope"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

type geoObjectsByPathsRequest struct {
	CanonicalPaths []string `json:"canonicalPaths"`
	Types          []string `json:"types,omitempty"`
	CreatedAfter   string   `json:"createdAfter,omitempty"`
	CreatedBefore  string   `json:"createdBefore,omitempty"`
}

func fragmentToGeoItem(f *models.HorizonFragment, inScope bool) (geoObjectResponseItem, bool) {
	if f == nil || f.Coordinates == nil {
		return geoObjectResponseItem{}, false
	}
	facilityKind := ""
	if f.Type == models.FragmentTypeFacility || strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.CanonicalPath)), "/fac/") {
		kind := taxonomy.NormalizeFacilityKind(strings.TrimSpace(f.Identifiers["facilityKind"]))
		if kind == taxonomy.FacilityKindUnknown {
			kind = taxonomy.InferFacilityKind(
				f.CanonicalPath,
				f.Labels["en"],
				f.Description,
				f.EntityPath,
				strings.Join(f.IndustryPaths, " "),
				f.Identifiers["facilityType"],
				f.Identifiers["supplyChainRole"],
			)
		}
		if kind == taxonomy.FacilityKindUnknown {
			kind = taxonomy.FacilityKindOther
		}
		facilityKind = string(kind)
	}
	label := strings.TrimSpace(f.Labels["en"])
	if label == "" {
		label = strings.TrimSpace(f.Identifiers["legalName"])
	}
	if label == "" {
		label = strings.TrimSpace(f.Identifiers["shortName"])
	}
	if label == "" {
		for _, v := range f.Labels {
			if v = strings.TrimSpace(v); v != "" {
				label = v
				break
			}
		}
	}
	if label == "" && len(f.AlternativeNames) > 0 {
		label = strings.TrimSpace(f.AlternativeNames[0])
	}
	if label == "" && len(f.NameHistory) > 0 {
		label = strings.TrimSpace(f.NameHistory[0].Name)
	}
	zoneType := ""
	zoneSubType := ""
	geoPrecision := ""
	sourceLayer := ""
	processingLayer := ""
	hype := 0.0
	hypeIndicator := 0
	activityStatus := ""
	conflictLinked := false
	threatClass := ""
	var lastMatchedAt *time.Time
	if f.Type == models.FragmentTypeAffectedZone {
		zoneType = strings.TrimSpace(asString(f.Qualifiers["zoneType"]))
		zoneSubType = strings.TrimSpace(asString(f.Qualifiers["zoneSubType"]))
		geoPrecision = strings.TrimSpace(asString(f.Qualifiers["geoPrecision"]))
		sourceLayer = strings.TrimSpace(asString(f.Qualifiers["sourceLayer"]))
		processingLayer = strings.TrimSpace(asString(f.Qualifiers["processingLayer"]))
		hype, hypeIndicator, activityStatus, lastMatchedAt = readZoneActivitySnapshot(f, time.Now().UTC())
		conflictLinked, threatClass = deriveAffectedZoneThreatEnvelope(f)
	}
	return geoObjectResponseItem{
		ID:              f.ID,
		CanonicalPath:   f.CanonicalPath,
		Type:            f.Type,
		FacilityKind:    facilityKind,
		Label:           label,
		Description:     strings.TrimSpace(f.Description),
		Status:          strings.TrimSpace(f.Governance.Status),
		Coordinates:     f.Coordinates,
		RadiusKm:        f.RadiusKm,
		ZoneType:        zoneType,
		ZoneSubType:     zoneSubType,
		GeoPrecision:    geoPrecision,
		SourceLayer:     sourceLayer,
		ProcessingLayer: processingLayer,
		Hype:            hype,
		HypeIndicator:   hypeIndicator,
		ActivityStatus:  activityStatus,
		ConflictLinked:  conflictLinked,
		ThreatClass:     threatClass,
		LastMatchedAt:   lastMatchedAt,
		InScope:         inScope,
		CreatedAt:       f.CreatedAt,
		UpdatedAt:       f.UpdatedAt,
	}, true
}

func deriveAffectedZoneThreatEnvelope(f *models.HorizonFragment) (bool, string) {
	if f == nil {
		return false, ""
	}
	conditionType := strings.ToLower(strings.TrimSpace(asString(f.Qualifiers["conditionType"])))
	zoneSubType := strings.ToLower(strings.TrimSpace(asString(f.Qualifiers["zoneSubType"])))
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		asString(f.Qualifiers["sourceTitle"]),
		asString(f.Qualifiers["sourceSummary"]),
		f.Description,
		f.Labels["en"],
		asString(f.Qualifiers["reason"]),
	}, " ")))
	if conditionType == "military_conflict" || conditionType == "security_conflict" {
		return true, "kinetic"
	}
	if strings.Contains(zoneSubType, "kinetic") || strings.Contains(zoneSubType, "armed_tension") || strings.Contains(zoneSubType, "terror") {
		return true, "kinetic"
	}
	if hasAnyKeyword(text,
		"drone attack", "drones strike", "drone strike", "missile strike", "projectile", "airstrike", "rocket attack",
		"ships were hit", "embassy hit", "under attack", "attacks and gps jamming", "gps jamming", "shelling", "bombard",
		"port after drones strike", "operations after drone attack", "explosion after strike", "strike killed",
	) {
		return true, "conflict-linked"
	}
	return false, ""
}

func hasAnyKeyword(text string, keywords ...string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword != "" && strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func parseGeoObjectFilter(
	typeValues []string,
	createdAfter string,
	createdBefore string,
) horizonrepo.GeoListFilter {
	var filter horizonrepo.GeoListFilter
	for _, rawType := range typeValues {
		for _, part := range strings.Split(strings.TrimSpace(rawType), ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			filter.Types = append(filter.Types, models.FragmentType(strings.ToUpper(part)))
		}
	}
	if raw := strings.TrimSpace(createdAfter); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.CreatedAfter = &t
		} else if t, err := time.Parse("2006-01-02", raw); err == nil {
			filter.CreatedAfter = &t
		}
	}
	if raw := strings.TrimSpace(createdBefore); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.CreatedBefore = &t
		} else if t, err := time.Parse("2006-01-02", raw); err == nil {
			end := t.Add(24*time.Hour - time.Nanosecond)
			filter.CreatedBefore = &end
		}
	}
	return filter
}

func (h *HorizonHandlers) geoObjectsForCanonicalPaths(
	ctx context.Context,
	canonicalPaths []string,
	filter *horizonrepo.GeoListFilter,
	inScope bool,
) ([]geoObjectResponseItem, error) {
	if len(canonicalPaths) == 0 {
		return []geoObjectResponseItem{}, nil
	}
	seen := make(map[string]struct{}, len(canonicalPaths))
	ordered := make([]string, 0, len(canonicalPaths))
	for _, path := range canonicalPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		ordered = append(ordered, path)
	}
	if len(ordered) == 0 {
		return []geoObjectResponseItem{}, nil
	}

	fragments, err := h.service.Fragments().ListByCanonicalPaths(ctx, ordered)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]*models.HorizonFragment, len(fragments))
	for _, fragment := range fragments {
		if fragment == nil {
			continue
		}
		cp := strings.TrimSpace(fragment.CanonicalPath)
		if cp == "" {
			continue
		}
		byPath[cp] = fragment
	}

	items := make([]geoObjectResponseItem, 0, len(ordered))
	for _, path := range ordered {
		fragment := byPath[path]
		if fragment == nil {
			continue
		}
		if filter != nil {
			if len(filter.Types) > 0 {
				match := false
				for _, ft := range filter.Types {
					if ft == fragment.Type {
						match = true
						break
					}
				}
				if !match {
					continue
				}
			}
			if filter.CreatedAfter != nil && fragment.CreatedAt.Before(*filter.CreatedAfter) {
				continue
			}
			if filter.CreatedBefore != nil && fragment.CreatedAt.After(*filter.CreatedBefore) {
				continue
			}
		}
		if lat, lon, ok := horizonrepo.ResolveFragmentCoordinates(fragment); ok {
			fragment.Coordinates = &models.Coordinates{Lat: lat, Lon: lon}
		} else {
			continue
		}
		if item, ok := fragmentToGeoItem(fragment, inScope); ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func readZoneActivitySnapshot(f *models.HorizonFragment, now time.Time) (float64, int, string, *time.Time) {
	if f == nil {
		return 0, 0, "", nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	score := metricValue(f.State, "zone_hype_score")
	hits1h := metricValue(f.State, "zone_hits_1h")
	hits24h := metricValue(f.State, "zone_hits_24h")
	lastMatchTS := metricValue(f.State, "zone_last_match_unix")

	updatedAt := f.Metadata.LastUpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = f.UpdatedAt.UTC()
	}
	if updatedAt.IsZero() {
		updatedAt = now
	}
	deltaH := now.Sub(updatedAt).Hours()
	if deltaH > 0 {
		score = decayValue(score, deltaH, 8.0)
		hits1h = decayValue(hits1h, deltaH, 1.0)
		hits24h = decayValue(hits24h, deltaH, 24.0)
		_ = hits1h
		_ = hits24h
	}
	score = clamp(score, 0, 100)
	status := zoneActivityStatus(score)
	indicator := zoneHypeIndicator(score)
	var lastMatchedAt *time.Time
	if lastMatchTS > 0 {
		t := time.Unix(int64(lastMatchTS), 0).UTC()
		lastMatchedAt = &t
	}
	return score, indicator, status, lastMatchedAt
}

func metricValue(state map[string]models.MetricValue, key string) float64 {
	if state == nil || strings.TrimSpace(key) == "" {
		return 0
	}
	mv, ok := state[key]
	if !ok {
		return 0
	}
	return mv.Value
}

func decayValue(v, deltaHours, halfLifeHours float64) float64 {
	if v <= 0 || deltaHours <= 0 || halfLifeHours <= 0 {
		return v
	}
	return v * math.Exp(-math.Ln2*deltaHours/halfLifeHours)
}

func zoneActivityStatus(score float64) string {
	switch {
	case score >= 72:
		return "hot"
	case score >= 48:
		return "active"
	case score >= 26:
		return "watch"
	case score >= 10:
		return "cooling"
	default:
		return "inactive"
	}
}

func zoneHypeIndicator(score float64) int {
	switch {
	case score >= 80:
		return 5
	case score >= 60:
		return 4
	case score >= 40:
		return 3
	case score >= 20:
		return 2
	case score >= 8:
		return 1
	default:
		return 0
	}
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ListGeoObjectsByPaths returns geo-bound fragments for an explicit set of canonical paths.
// POST /api/horizon/geo-objects/by-paths
func (h *HorizonHandlers) ListGeoObjectsByPaths(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if httputil.GetAuthInfo(r.Context()) == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req geoObjectsByPathsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	filter := parseGeoObjectFilter(req.Types, req.CreatedAfter, req.CreatedBefore)
	items, err := h.geoObjectsForCanonicalPaths(r.Context(), req.CanonicalPaths, &filter, true)
	if err != nil {
		log.Printf("ListGeoObjectsByPaths error: paths=%d: %v", len(req.CanonicalPaths), err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "private, max-age=60")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":          items,
		"total":          len(items),
		"requestedCount": len(req.CanonicalPaths),
	})
}

// ListFragments получает список HorizonFragment через Lens scope
func (h *HorizonHandlers) ListFragments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Получаем lensID из query параметра
	lensID := r.URL.Query().Get("lensId")
	if lensID == "" {
		http.Error(w, "lensId query parameter is required", http.StatusBadRequest)
		return
	}

	// Получаем Lens для определения scope
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ к Lens
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Получаем все HorizonFragment из scope
	fragments, err := h.service.Fragments().ListByCanonicalPaths(r.Context(), lens.Scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"fragments": fragments,
		"total":     len(fragments),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ListGeoObjects returns geo-bound fragments for map rendering.
// GET /api/horizon/geo-objects?limit=2000&pathPrefix=/loc/&types=ORGANIZATION,FACILITY&createdAfter=2024-01-01&createdBefore=2025-12-31
func (h *HorizonHandlers) ListGeoObjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if httputil.GetAuthInfo(r.Context()) == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	limit := 2000
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 5000 {
		limit = 5000
	}
	pathPrefix := strings.TrimSpace(r.URL.Query().Get("pathPrefix"))
	onlyLoc := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("onlyLoc")), "1") ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("onlyLoc")), "true")
	if pathPrefix == "" && onlyLoc {
		pathPrefix = "/loc/"
	}

	filter := parseGeoObjectFilter(
		[]string{strings.TrimSpace(r.URL.Query().Get("types"))},
		strings.TrimSpace(r.URL.Query().Get("createdAfter")),
		strings.TrimSpace(r.URL.Query().Get("createdBefore")),
	)

	lensID := strings.TrimSpace(r.URL.Query().Get("lensId"))

	var items []geoObjectResponseItem
	if lensID != "" {
		var err error
		items, err = h.listGeoObjectsForLens(r.Context(), lensID, &filter)
		if err != nil {
			log.Printf("ListGeoObjects lens error: lensId=%s: %v", lensID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		fragments, err := h.service.Fragments().ListGeoBound(r.Context(), limit, pathPrefix, &filter)
		if err != nil {
			log.Printf("ListGeoObjects error: limit=%d pathPrefix=%q: %v", limit, pathPrefix, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = make([]geoObjectResponseItem, 0, len(fragments))
		for _, f := range fragments {
			if item, ok := fragmentToGeoItem(f, true); ok {
				items = append(items, item)
			}
		}
	}

	w.Header().Set("Cache-Control", "private, max-age=60")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":      items,
		"total":      len(items),
		"limit":      limit,
		"pathPrefix": pathPrefix,
		"lensId":     lensID,
	})
}

// listGeoObjectsForLens returns geo-bound fragments for a lens: scope members + 1-hop neighbors.
func (h *HorizonHandlers) listGeoObjectsForLens(
	ctx context.Context,
	lensID string,
	filter *horizonrepo.GeoListFilter,
) ([]geoObjectResponseItem, error) {
	lens, err := h.service.Lens().GetLens(ctx, lensID)
	if err != nil {
		return nil, fmt.Errorf("lens not found: %w", err)
	}

	// --- Resolve scope paths (same pattern as cockpit) ---
	rawScope := append([]string(nil), lens.Scope...)
	scopePaths := make([]string, 0, len(rawScope)+8)
	seen := map[string]struct{}{}
	for _, s := range rawScope {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "/query/") && lens.ResolvedQueries != nil {
			if resolved := lens.ResolvedQueries[s]; len(resolved) > 0 {
				for _, rp := range resolved {
					rp = strings.TrimSpace(rp)
					if rp == "" {
						continue
					}
					if _, ok := seen[rp]; ok {
						continue
					}
					seen[rp] = struct{}{}
					scopePaths = append(scopePaths, rp)
				}
				continue
			}
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		scopePaths = append(scopePaths, s)
	}

	// Canonicalize via resolver (best-effort).
	resolvedScope := make([]string, 0, len(scopePaths))
	if se := h.service.Scout(); se != nil {
		if resolver := se.CanonicalPathResolver(); resolver != nil {
			for _, p := range scopePaths {
				ft := scout.InferFragmentTypeFromCanonicalPath(p)
				if ft == "" {
					ft = models.FragmentTypeOrganization
				}
				if rr, rerr := resolver.Resolve(ctx, p, ft); rerr == nil && rr != nil && strings.TrimSpace(rr.CanonicalPath) != "" {
					resolvedScope = append(resolvedScope, strings.TrimSpace(rr.CanonicalPath))
				} else {
					resolvedScope = append(resolvedScope, p)
				}
			}
		}
	}
	if len(resolvedScope) == 0 {
		resolvedScope = scopePaths
	}

	// Dedupe.
	seen2 := map[string]struct{}{}
	outScope := make([]string, 0, len(resolvedScope))
	for _, p := range resolvedScope {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen2[p]; ok {
			continue
		}
		seen2[p] = struct{}{}
		outScope = append(outScope, p)
	}

	// --- Fetch scope fragments ---
	scopeFragments, err := h.service.Fragments().ListByCanonicalPaths(ctx, outScope)
	if err != nil {
		return nil, fmt.Errorf("scope fragments: %w", err)
	}

	scopeIDs := make(map[string]struct{}, len(scopeFragments))
	for _, f := range scopeFragments {
		if f != nil && f.ID != "" {
			scopeIDs[f.ID] = struct{}{}
		}
	}

	// --- Collect 1-hop neighbor IDs via edges ---
	edgesRepo := h.service.Edges()
	neighborIDs := make(map[string]struct{}, 64)
	for _, f := range scopeFragments {
		if f == nil || f.ID == "" {
			continue
		}
		outgoing, _ := edgesRepo.ListBySource(ctx, "global", f.ID, nil, 200)
		for _, e := range outgoing {
			if e == nil {
				continue
			}
			tid := strings.TrimSpace(e.TargetFragmentID)
			if tid != "" {
				if _, inScope := scopeIDs[tid]; !inScope {
					neighborIDs[tid] = struct{}{}
				}
			}
		}
		incoming, _ := edgesRepo.ListByTarget(ctx, "global", f.ID, nil, 200)
		for _, e := range incoming {
			if e == nil {
				continue
			}
			sid := strings.TrimSpace(e.SourceFragmentID)
			if sid != "" {
				if _, inScope := scopeIDs[sid]; !inScope {
					neighborIDs[sid] = struct{}{}
				}
			}
		}
	}

	// --- Batch-fetch neighbor fragments ---
	var neighborFragments []*models.HorizonFragment
	if len(neighborIDs) > 0 {
		ids := make([]string, 0, len(neighborIDs))
		for id := range neighborIDs {
			ids = append(ids, id)
		}
		nbMap, err := h.service.Fragments().GetMany(ctx, ids)
		if err != nil {
			log.Printf("listGeoObjectsForLens: GetMany neighbors error: %v", err)
		} else {
			for _, f := range nbMap {
				neighborFragments = append(neighborFragments, f)
			}
		}
	}

	// --- Build response items with coordinate resolution ---
	items := make([]geoObjectResponseItem, 0, len(scopeFragments)+len(neighborFragments))
	addFragment := func(f *models.HorizonFragment, inScope bool) {
		if f == nil {
			return
		}
		if filter != nil {
			if len(filter.Types) > 0 {
				match := false
				for _, ft := range filter.Types {
					if ft == f.Type {
						match = true
						break
					}
				}
				if !match {
					return
				}
			}
			if filter.CreatedAfter != nil && f.CreatedAt.Before(*filter.CreatedAfter) {
				return
			}
			if filter.CreatedBefore != nil && f.CreatedAt.After(*filter.CreatedBefore) {
				return
			}
		}
		if lat, lon, ok := horizonrepo.ResolveFragmentCoordinates(f); ok {
			f.Coordinates = &models.Coordinates{Lat: lat, Lon: lon}
		} else {
			return
		}
		if item, ok := fragmentToGeoItem(f, inScope); ok {
			items = append(items, item)
		}
	}

	for _, f := range scopeFragments {
		addFragment(f, true)
	}
	for _, f := range neighborFragments {
		addFragment(f, false)
	}

	return items, nil
}

// GetFragment получает HorizonFragment по ID
func (h *HorizonHandlers) GetFragment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем ID из пути: /api/horizon/fragments/{id}
	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Ensure entity mapping on access (best-effort)
	_, _, _, _ = h.service.EnsureEntityForFragmentObject(r.Context(), fragment)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fragment)
}

// DeleteFragment deletes a HorizonFragment by ID (best-effort cleanup of related edges/profiles).
func (h *HorizonHandlers) DeleteFragment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Extract fragment ID from path: /api/horizon/fragments/{id}
	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	fragment, err := h.service.Fragments().Get(ctx, fragmentID)
	if err != nil || fragment == nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Access control: allow tenant owner, otherwise require admin (HITL policy).
	if strings.TrimSpace(fragment.TenantID) != "" && strings.TrimSpace(fragment.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		p := &horizonpolicy.HumanInTheLoopPolicy{UsersRepo: h.usersRepo}
		if !p.IsAdmin(ctx, authInfo.TenantID, authInfo.UID) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	// Best-effort: delete fragment profiles (if configured).
	if h.profilesHandler != nil && h.profilesHandler.profilesRepo != nil {
		if err := h.profilesHandler.profilesRepo.DeleteByFragmentID(ctx, fragmentID); err != nil {
			log.Printf("[DeleteFragment] WARNING: failed to delete profiles fragmentId=%s err=%v", fragmentID, err)
		}
	}

	if err := h.service.Fragments().Delete(ctx, fragmentID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"fragmentId": fragmentID,
	})
}

// GetFragmentByPath получает HorizonFragment по canonicalPath
func (h *HorizonHandlers) GetFragmentByPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	canonicalPath := r.URL.Query().Get("path")
	if canonicalPath == "" {
		http.Error(w, "path query parameter is required", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().GetByCanonicalPath(r.Context(), canonicalPath)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Ensure entity mapping on access (best-effort)
	_, _, _, _ = h.service.EnsureEntityForFragmentObject(r.Context(), fragment)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fragment)
}

// EnsureFragmentEntity triggers create/link/enrich for a fragment's entity mapping.
// POST /api/horizon/fragments/{id}/ensure-entity
func (h *HorizonHandlers) EnsureFragmentEntity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/ensure-entity"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	entity, created, enriched, err := h.service.EnsureEntityForFragment(r.Context(), fragmentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entity == nil {
		// Not supported / no-op
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"fragmentId": fragmentID,
			"entity":     nil,
			"created":    false,
			"enriched":   false,
			"status":     "noop",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"fragmentId": fragmentID,
		"entityId":   entity.ID,
		"entity":     entity,
		"created":    created,
		"enriched":   enriched,
		"status":     "ok",
	})
}

// GetFragmentEntity returns the entity mapped to a fragment (ensuring it exists by default).
// GET /api/horizon/fragments/{id}/entity
func (h *HorizonHandlers) GetFragmentEntity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/entity"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	// Ensure on access (default behavior)
	entity, _, _, err := h.service.EnsureEntityForFragment(r.Context(), fragmentID)
	if err != nil {
		log.Printf("[GetFragmentEntity] EnsureEntityForFragment failed: fragmentId=%s err=%v", fragmentID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entity == nil {
		http.Error(w, "Entity not available for this fragment type", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entity)
}

// EnrichFragmentEntity forces or triggers Wikidata enrichment for the entity linked to a fragment.
// POST /api/horizon/fragments/{id}/enrich-entity?force=1
func (h *HorizonHandlers) EnrichFragmentEntity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/enrich-entity"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	force := r.URL.Query().Get("force")
	forceBool := force == "1" || strings.EqualFold(force, "true")

	entity, err := h.service.EnrichEntityForFragment(r.Context(), fragmentID, forceBool)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"fragmentId": fragmentID,
		"entityId":   entity.ID,
		"force":      forceBool,
		"entity":     entity,
	})
}

// RefreshFragment создает ScoutingJob для принудительного обновления HorizonFragment
func (h *HorizonHandlers) RefreshFragment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	if fragmentID == "" {
		http.Error(w, "Fragment ID required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		Strategy    string   `json:"strategy"`
		MaxCost     float64  `json:"maxCost"`
		Specialties []string `json:"specialties,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Определяем стратегию
	strategy := models.ScoutingStrategySnapshot
	if req.Strategy != "" {
		strategy = models.ScoutingStrategy(req.Strategy)
	}

	// Создаем ScoutingJob для обновления
	scoutEngine := h.service.Scout()
	if scoutEngine == nil {
		http.Error(w, "Scout Engine not available", http.StatusServiceUnavailable)
		return
	}

	// Создаем ScoutingJob через репозиторий
	scoutingJob := &models.ScoutingJob{
		AgentJob: agentmodels.AgentJob{
			TenantID:  fragment.TenantID,
			ProjectID: "", // Обновление не привязано к проекту
			Status:    "pending",
			AgentID:   "scout",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			State: map[string]interface{}{
				"targetCanonicalPath": fragment.CanonicalPath,
				"currentState":        string(models.ScoutingJobStatePending),
			},
		},
		TargetCanonicalPath: fragment.CanonicalPath,
		ScoutingStrategy:    string(strategy),
		CurrentState:        models.ScoutingJobStatePending,
	}
	// Optional: pass specialties into job state (normalized).
	if specs := taxonomy.NormalizeSpecialties(req.Specialties); len(specs) > 0 {
		if scoutingJob.State == nil {
			scoutingJob.State = map[string]interface{}{}
		}
		scoutingJob.State["specialties"] = specs
	}

	jobID, err := h.service.ScoutingJobs().Create(r.Context(), scoutingJob)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create scouting job: %v", err), http.StatusInternalServerError)
		return
	}

	// Запускаем выполнение ScoutingJob асинхронно
	go func() {
		ctx := context.Background()
		scoutingJob.ID = jobID
		if err := scoutEngine.ExecuteScoutingJob(ctx, scoutingJob); err != nil {
			// Логируем ошибку, но не блокируем ответ
			_ = err
		}
	}()

	response := map[string]interface{}{
		"scoutingJobId":       jobID,
		"status":              "pending",
		"estimatedCompletion": nil,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

// ExpandProductMaterials expands upstream and downstream materials for a product fragment.
// POST /api/horizon/fragments/{id}/expand-materials
// Body (optional): {"upstreamDepth": 3, "downstreamDepth": 3, "includeProducers": true}
// Response: {"productId": "...", "upstreamCount": N, "downstreamCount": N, "producersCount": N, "duration": "..."}
func (h *HorizonHandlers) ExpandProductMaterials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Extract fragment ID from path: /api/horizon/fragments/{id}/expand-materials
	fragmentIDOrPath := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentIDOrPath = strings.TrimSuffix(fragmentIDOrPath, "/expand-materials")
	if fragmentIDOrPath == "" {
		http.Error(w, "Fragment ID or canonical path required", http.StatusBadRequest)
		return
	}

	// Get Scout Engine
	scoutEngine := h.service.Scout()
	if scoutEngine == nil {
		http.Error(w, "Scout Engine not available", http.StatusServiceUnavailable)
		return
	}

	// Parse optional request body for options
	var reqBody struct {
		UpstreamDepth    int   `json:"upstreamDepth"`
		DownstreamDepth  int   `json:"downstreamDepth"`
		IncludeProducers *bool `json:"includeProducers"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			// Ignore parse errors, use defaults
			_ = err
		}
	}

	// Build options
	opts := scout.ExpandProductMaterialsOptions{
		UpstreamDepth:    3,
		DownstreamDepth:  3,
		IncludeProducers: true,
	}
	if reqBody.UpstreamDepth > 0 {
		opts.UpstreamDepth = reqBody.UpstreamDepth
	}
	if reqBody.DownstreamDepth > 0 {
		opts.DownstreamDepth = reqBody.DownstreamDepth
	}
	if reqBody.IncludeProducers != nil {
		opts.IncludeProducers = *reqBody.IncludeProducers
	}

	// Create a context with timeout for the synchronous operation
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Call the expand method
	result, err := scoutEngine.ExpandProductMaterials(ctx, fragmentIDOrPath, opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to expand materials: %v", err), http.StatusBadRequest)
		return
	}

	// Build response
	response := map[string]interface{}{
		"productId":       result.ProductID,
		"productPath":     result.ProductPath,
		"upstreamCount":   result.UpstreamCount,
		"downstreamCount": result.DownstreamCount,
		"producersCount":  result.ProducersCount,
		"edgesCount":      result.EdgesCount,
		"duration":        result.Duration.String(),
		"status":          "completed",
	}
	if result.WikidataID != "" {
		response["wikidataId"] = result.WikidataID
	}
	if len(result.Errors) > 0 {
		response["warnings"] = result.Errors
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetFragmentVersions получает историю версий HorizonFragment
func (h *HorizonHandlers) GetFragmentVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем fragmentID из пути: /api/horizon/fragments/{id}/versions
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/fragments/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) < 2 || pathParts[1] != "versions" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	fragmentID := pathParts[0]

	// Проверяем существование фрагмента
	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 100 {
			limit = parsedLimit
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	// Получаем версии через репозиторий
	versions, err := h.service.FragmentVersions().ListByFragment(r.Context(), fragment.TenantID, fragmentID, limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get versions: %v", err), http.StatusInternalServerError)
		return
	}

	// Преобразуем в формат ответа
	versionResponses := make([]map[string]interface{}, 0, len(versions))
	for _, v := range versions {
		versionResponses = append(versionResponses, map[string]interface{}{
			"versionId":     v.VersionID,
			"state":         v.State,
			"sourceFrameId": v.SourceFrameID,
			"timestamp":     v.Timestamp,
		})
	}

	response := map[string]interface{}{
		"versions": versionResponses,
		"total":    len(versions),
		"limit":    limit,
		"offset":   offset,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

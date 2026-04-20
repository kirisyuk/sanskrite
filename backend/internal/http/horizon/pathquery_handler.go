package horizon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathquery"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// GetPathquery handles GET /api/horizon/pathquery (and /api/v1/horizon/pathquery).
func (h *HorizonHandlers) GetPathquery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth := httputil.GetAuthInfo(r.Context())
	if auth == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	if h.pathqueryService == nil {
		http.Error(w, "Path query not available", http.StatusServiceUnavailable)
		return
	}

	tenantID := strings.TrimSpace(auth.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	fromRaw := strings.TrimSpace(r.URL.Query().Get("from"))
	if fromRaw == "" {
		http.Error(w, "from is required", http.StatusBadRequest)
		return
	}
	fromEP, err := parsePathqueryEndpoint(fromRaw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var toEP *pathquery.Endpoint
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		ep, perr := parsePathqueryEndpoint(raw)
		if perr != nil {
			http.Error(w, perr.Error(), http.StatusBadRequest)
			return
		}
		toEP = &ep
	}

	q := pathquery.Query{
		Tenant:        tenantID,
		Project:       strings.TrimSpace(r.URL.Query().Get("project")),
		From:          fromEP,
		To:            toEP,
		ProductAxis:   strings.TrimSpace(r.URL.Query().Get("productAxis")),
		IncludeStress: strings.TrimSpace(r.URL.Query().Get("includeStress")) == "1" || strings.EqualFold(r.URL.Query().Get("includeStress"), "true"),
		IncludeGeo:    strings.TrimSpace(r.URL.Query().Get("includeGeo")) == "1" || strings.EqualFold(r.URL.Query().Get("includeGeo"), "true"),
		Context: pathquery.SearchContext{
			LensID: strings.TrimSpace(r.URL.Query().Get("lensId")),
			Scope:  strings.TrimSpace(r.URL.Query().Get("scope")),
		},
	}
	if mh := strings.TrimSpace(r.URL.Query().Get("maxHops")); mh != "" {
		if v, err := strconv.Atoi(mh); err == nil {
			q.MaxHops = v
		}
	}
	if rel := strings.TrimSpace(r.URL.Query().Get("relations")); rel != "" {
		for _, part := range strings.Split(rel, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			q.RelationKinds = append(q.RelationKinds, ah.RelationName(part))
		}
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("cacheMode"))) {
	case "disabled", "off", "no":
		q.CacheMode = pathquery.CacheModeDisabled
	case "refresh", "force":
		q.CacheMode = pathquery.CacheModeRefresh
	default:
		q.CacheMode = pathquery.CacheModeDefault
	}
	if ttl := strings.TrimSpace(r.URL.Query().Get("cacheTtl")); ttl != "" {
		if d, err := time.ParseDuration(ttl); err == nil {
			q.CacheTTL = d
		}
	}
	if asOf := strings.TrimSpace(r.URL.Query().Get("asOf")); asOf != "" {
		if t, err := time.Parse(time.RFC3339, asOf); err == nil {
			q.Context.AsOf = t
		} else if d, err := time.Parse("2006-01-02", asOf); err == nil {
			q.Context.AsOf = d.UTC()
		}
	}

	res, err := h.pathqueryService.QueryPaths(r.Context(), q)
	if err != nil {
		http.Error(w, "path query failed", http.StatusInternalServerError)
		return
	}

	pathPayloads := make([]map[string]interface{}, 0, len(res.Paths))
	for _, a := range res.Paths {
		if a == nil || a.Payload == nil {
			continue
		}
		pathPayloads = append(pathPayloads, a.Payload)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"fromCache":   res.FromCache,
		"generatedAt": res.GeneratedAt,
		"cacheKey":    res.CacheKey,
		"diagnostics": res.Diagnostics,
		"paths":       pathPayloads,
	})
}

func parsePathqueryEndpoint(raw string) (pathquery.Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "id:") {
		return pathquery.Endpoint{Kind: pathquery.EndpointFragmentID, Value: strings.TrimSpace(strings.TrimPrefix(raw, "id:"))}, nil
	}
	if !strings.HasPrefix(raw, "/") {
		return pathquery.Endpoint{}, errors.New("from/to must be a canonical path (/) or id:<fragmentId>")
	}
	return pathquery.Endpoint{Kind: pathquery.EndpointCanonicalPath, Value: raw}, nil
}

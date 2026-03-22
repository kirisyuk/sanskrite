package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	neturl "net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbinding"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathstress"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	defaultIdentifyStressesMaxNodes     = 40
	defaultIdentifyStressesMaxArtifacts = 25
	defaultIdentifyStressesMaxFindings  = 120
	defaultIdentifyStressesMaxRelations = 240
	identifyStressesBatchSize           = 12
)

var (
	identifyPrimaryScalarMetrics = map[string]struct{}{
		"supply_chain_criticality":            {},
		"route_fragility":                     {},
		"route_rerouteability":                {},
		"confidence":                          {},
		"volume":                              {},
		"share":                               {},
		"lead_time_days":                      {},
		"customs_clearance_days":              {},
		"inventory_cover_days_at_destination": {},
		"switch_time_days":                    {},
		"technical_fit":                       {},
		"regulatory_fit":                      {},
		"fill_rate":                           {},
		"rejection_rate":                      {},
		"annual_capacity":                     {},
		"annual_output":                       {},
		"lead_time_days_baseline":             {},
		"lead_time_volatility_days":           {},
		"inventory_days_baseline":             {},
		"on_time_delivery_baseline":           {},
		"yield_ratio":                         {},
		"scrap_rate":                          {},
		"quality_score":                       {},
		"purity_percent":                      {},
		"available_export_share":              {},
		"utilization_rate":                    {},
		"downtime_days_per_year":              {},
		"technical_substitutability":          {},
		"regulatory_substitutability":         {},
		"service_criticality":                 {},
		"path_stress_score":                   {},
		"path_fragility":                      {},
		"path_rerouteability":                 {},
		"path_zone_pressure":                  {},
		"path_affected_pressure":              {},
		"recovery_latency_hours":              {},
		"binding_confidence":                  {},
		"segment_confidence":                  {},
	}
	identifyControlQualifierKeys = map[string]struct{}{
		"export_license_required":   {},
		"license_required":          {},
		"sanctions_touchpoint":      {},
		"dual_use_flag":             {},
		"approved_vendor_only":      {},
		"service_contract_required": {},
	}
	identifyMechanismByStressor = map[string][]string{
		"sanctions":                       {"control_exposure"},
		"export_controls":                 {"control_exposure"},
		"regulatory_block":                {"control_exposure"},
		"customs_restrictions":            {"customs_friction"},
		"port_or_border_congestion":       {"customs_friction", "route_fragility"},
		"route_disruption":                {"route_fragility"},
		"carrier_or_service_interruption": {"service_dependency", "route_fragility"},
		"supplier_outage":                 {"sole_source_lock_in", "service_dependency"},
		"capacity_drop":                   {"inventory_vulnerability", "service_dependency"},
		"inventory_shortfall":             {"inventory_vulnerability"},
		"security_conflict":               {"route_fragility", "control_exposure"},
		"natural_hazard":                  {"route_fragility", "inventory_vulnerability"},
		"labor_action":                    {"route_fragility", "customs_friction"},
		"insurance_finance_restriction":   {"control_exposure", "service_dependency"},
		"cyber_operational_disruption":    {"service_dependency", "route_fragility"},
	}
	identifyCoverageRequiredMetricKeys = []string{
		"lead_time_days",
		"inventory_cover_days_at_destination",
		"share",
		"switch_time_days",
		"technical_fit",
		"regulatory_fit",
	}
	identifyCapabilityMetricKeys = []string{
		"annual_capacity",
		"annual_output",
		"lead_time_days_baseline",
		"lead_time_volatility_days",
		"inventory_days_baseline",
		"on_time_delivery_baseline",
		"yield_ratio",
		"scrap_rate",
		"quality_score",
		"purity_percent",
		"available_export_share",
		"utilization_rate",
		"downtime_days_per_year",
		"technical_substitutability",
		"regulatory_substitutability",
		"service_criticality",
	}
)

type IdentifyStressesRequest struct {
	ScopeMode             string                          `json:"scopeMode,omitempty"`
	SelectedCanonicalPath string                          `json:"selectedCanonicalPath,omitempty"`
	VisibleNodeRefs       []string                        `json:"visibleNodeRefs,omitempty"`
	VisibleArtifactKeys   []string                        `json:"visibleArtifactKeys,omitempty"`
	VisibleSupplyLinkRefs []string                        `json:"visibleSupplyLinkRefs,omitempty"`
	VisibleRelations      []IdentifyStressVisibleRelation `json:"visibleRelations,omitempty"`
	ClientViewID          string                          `json:"clientViewId,omitempty"`
	MaxNodes              int                             `json:"maxNodes,omitempty"`
	MaxArtifacts          int                             `json:"maxArtifacts,omitempty"`
	MaxFindings           int                             `json:"maxFindings,omitempty"`
}

type IdentifyStressVisibleRelation struct {
	EdgeID              string  `json:"edgeId,omitempty"`
	SourceCanonicalPath string  `json:"sourceCanonicalPath,omitempty"`
	TargetCanonicalPath string  `json:"targetCanonicalPath,omitempty"`
	RelationType        string  `json:"relationType,omitempty"`
	RelationFamily      string  `json:"relationFamily,omitempty"`
	RelationRole        string  `json:"relationRole,omitempty"`
	BusinessMeaning     string  `json:"businessMeaning,omitempty"`
	WhyRelevant         string  `json:"whyRelevant,omitempty"`
	Criticality         float64 `json:"criticality,omitempty"`
}

type IdentifyStressesScopeSummary struct {
	ScopeMode             string `json:"scopeMode"`
	SelectedCanonicalPath string `json:"selectedCanonicalPath,omitempty"`
	VisibleNodes          int    `json:"visibleNodes"`
	AcceptedNodes         int    `json:"acceptedNodes"`
	VisibleArtifacts      int    `json:"visibleArtifacts"`
	AcceptedArtifacts     int    `json:"acceptedArtifacts"`
	AcceptedSupplyLinks   int    `json:"acceptedSupplyLinks"`
	VisibleRelations      int    `json:"visibleRelations"`
	AcceptedRelations     int    `json:"acceptedRelations"`
}

type IdentifyStressesResponse struct {
	JobID         string                       `json:"jobId"`
	AcceptedScope IdentifyStressesScopeSummary `json:"acceptedScope"`
	StartedAt     string                       `json:"startedAt"`
	StreamChannel string                       `json:"streamChannel"`
}

type IdentifyStressesDebugResult struct {
	ScopeSummary          IdentifyStressesScopeSummary `json:"scopeSummary"`
	AcceptedNodeRefs      []string                     `json:"acceptedNodeRefs,omitempty"`
	AcceptedArtifacts     []string                     `json:"acceptedArtifactKeys,omitempty"`
	AcceptedSupplyLinks   []string                     `json:"acceptedSupplyLinks,omitempty"`
	AcceptedGeoRefs       []string                     `json:"acceptedGeoRefs,omitempty"`
	AcceptedZoneRefs      []string                     `json:"acceptedZoneRefs,omitempty"`
	AcceptedAffectedZones []string                     `json:"acceptedAffectedZones,omitempty"`
	GroundedDossier       string                       `json:"groundedDossier,omitempty"`
	GroundingEvidence     []StressEvidence             `json:"groundingEvidence,omitempty"`
	Findings              []StressFinding              `json:"findings,omitempty"`
}

type IdentifyStressesCanvas struct {
	exec                   *ai.AITaskExecutor
	factsRepo              *corerepo.OntologyFactsRepository
	dataSourcesRepo        *corerepo.DataSourcesRepository
	fragmentsRepo          *horizonrepo.HorizonFragmentsRepository
	edgesRepo              *horizonrepo.DependencyEdgesRepository
	materializedRepo       *horizonrepo.MaterializedArtifactsRepository
	lensesRepo             *horizonrepo.LensesRepository
	signalPipeline         *horizonscout.SignalPipeline
	capabilityProfilesRepo *horizonrepo.CapabilityProfilesRepository
	capabilityObsRepo      *horizonrepo.CapabilityObservationsRepository
	supplyLinkObsRepo      *horizonrepo.SupplyLinkObservationsRepository
	routeObsRepo           *horizonrepo.RouteObservationsRepository
	derivedRiskRepo        *horizonrepo.DerivedRiskProfilesRepository
	publisher              func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	logger                 *log.Logger
}

type IdentifyStressesCanvasConfig struct {
	Exec                   *ai.AITaskExecutor
	FactsRepo              *corerepo.OntologyFactsRepository
	DataSourcesRepo        *corerepo.DataSourcesRepository
	FragmentsRepo          *horizonrepo.HorizonFragmentsRepository
	EdgesRepo              *horizonrepo.DependencyEdgesRepository
	MaterializedRepo       *horizonrepo.MaterializedArtifactsRepository
	LensesRepo             *horizonrepo.LensesRepository
	SignalPipeline         *horizonscout.SignalPipeline
	CapabilityProfilesRepo *horizonrepo.CapabilityProfilesRepository
	CapabilityObsRepo      *horizonrepo.CapabilityObservationsRepository
	SupplyLinkObsRepo      *horizonrepo.SupplyLinkObservationsRepository
	RouteObsRepo           *horizonrepo.RouteObservationsRepository
	DerivedRiskRepo        *horizonrepo.DerivedRiskProfilesRepository
	Publisher              func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	Logger                 *log.Logger
}

func NewIdentifyStressesCanvas(cfg IdentifyStressesCanvasConfig) *IdentifyStressesCanvas {
	return &IdentifyStressesCanvas{
		exec:                   cfg.Exec,
		factsRepo:              cfg.FactsRepo,
		dataSourcesRepo:        cfg.DataSourcesRepo,
		fragmentsRepo:          cfg.FragmentsRepo,
		edgesRepo:              cfg.EdgesRepo,
		materializedRepo:       cfg.MaterializedRepo,
		lensesRepo:             cfg.LensesRepo,
		signalPipeline:         cfg.SignalPipeline,
		capabilityProfilesRepo: cfg.CapabilityProfilesRepo,
		capabilityObsRepo:      cfg.CapabilityObsRepo,
		supplyLinkObsRepo:      cfg.SupplyLinkObsRepo,
		routeObsRepo:           cfg.RouteObsRepo,
		derivedRiskRepo:        cfg.DerivedRiskRepo,
		publisher:              cfg.Publisher,
		logger:                 cfg.Logger,
	}
}

type StressFinding struct {
	ID                 string                    `json:"id"`
	TargetType         string                    `json:"targetType"`
	TargetRefs         []string                  `json:"targetRefs,omitempty"`
	ArtifactKey        string                    `json:"artifactKey,omitempty"`
	LegIndex           *int                      `json:"legIndex,omitempty"`
	SupplyLinkRef      string                    `json:"supplyLinkRef,omitempty"`
	ProductRef         string                    `json:"productRef,omitempty"`
	StressorType       string                    `json:"stressorType,omitempty"`
	Mechanism          string                    `json:"mechanism,omitempty"`
	Severity           string                    `json:"severity,omitempty"`
	Confidence         float64                   `json:"confidence,omitempty"`
	EffectiveAt        string                    `json:"effectiveAt,omitempty"`
	TimeHorizon        string                    `json:"timeHorizon,omitempty"`
	GeoRefs            []string                  `json:"geoRefs,omitempty"`
	FlowRefs           []string                  `json:"flowRefs,omitempty"`
	ZoneRefs           []string                  `json:"zoneRefs,omitempty"`
	AffectedZoneRefs   []string                  `json:"affectedZoneRefs,omitempty"`
	MetricObservations []StressMetricObservation `json:"metricObservations,omitempty"`
	QualifierDeltas    map[string]interface{}    `json:"qualifierDeltas,omitempty"`
	Evidence           []StressEvidence          `json:"evidence,omitempty"`
	Summary            string                    `json:"summary,omitempty"`
	EventType          string                    `json:"eventType,omitempty"`
	FactType           string                    `json:"factType,omitempty"`
}

type StressMetricObservation struct {
	MetricKey   string  `json:"metricKey"`
	Value       float64 `json:"value"`
	Unit        string  `json:"unit,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Scope       string  `json:"scope,omitempty"`
	Description string  `json:"description,omitempty"`
}

func (s *StressFinding) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.ID = coerceJSONString(raw["id"])
	s.TargetType = coerceJSONString(raw["targetType"])
	s.TargetRefs = coerceJSONStringSlice(raw["targetRefs"])
	s.ArtifactKey = coerceJSONString(raw["artifactKey"])
	if idx, ok := coerceJSONInt(raw["legIndex"]); ok {
		s.LegIndex = &idx
	} else {
		s.LegIndex = nil
	}
	s.SupplyLinkRef = coerceJSONString(raw["supplyLinkRef"])
	s.ProductRef = coerceJSONString(raw["productRef"])
	s.StressorType = coerceJSONString(raw["stressorType"])
	s.Mechanism = coerceJSONString(raw["mechanism"])
	s.Severity = coerceJSONString(raw["severity"])
	s.Confidence = coerceJSONFloat(raw["confidence"])
	s.EffectiveAt = coerceJSONString(raw["effectiveAt"])
	s.TimeHorizon = coerceJSONString(raw["timeHorizon"])
	s.GeoRefs = coerceJSONStringSlice(raw["geoRefs"])
	s.FlowRefs = coerceJSONStringSlice(raw["flowRefs"])
	s.ZoneRefs = coerceJSONStringSlice(raw["zoneRefs"])
	s.AffectedZoneRefs = coerceJSONStringSlice(raw["affectedZoneRefs"])
	if qualifierDeltas, ok := raw["qualifierDeltas"].(map[string]interface{}); ok && len(qualifierDeltas) > 0 {
		s.QualifierDeltas = qualifierDeltas
	} else {
		s.QualifierDeltas = nil
	}
	s.Summary = coerceJSONString(raw["summary"])
	s.EventType = coerceJSONString(raw["eventType"])
	s.FactType = coerceJSONString(raw["factType"])

	if metricsRaw, ok := raw["metricObservations"]; ok {
		metricsData, err := json.Marshal(metricsRaw)
		if err != nil {
			return err
		}
		var metrics []StressMetricObservation
		if err := json.Unmarshal(metricsData, &metrics); err != nil {
			return err
		}
		s.MetricObservations = metrics
	} else {
		s.MetricObservations = nil
	}

	if evidenceRaw, ok := raw["evidence"]; ok {
		evidenceData, err := json.Marshal(evidenceRaw)
		if err != nil {
			return err
		}
		var evidence []StressEvidence
		if err := json.Unmarshal(evidenceData, &evidence); err != nil {
			return err
		}
		s.Evidence = evidence
	} else {
		s.Evidence = nil
	}
	return nil
}

func (m *StressMetricObservation) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.MetricKey = coerceJSONString(firstNonEmptyValue(raw["metricKey"], raw["key"]))
	m.Value = coerceJSONFloat(firstNonEmptyValue(raw["value"], raw["metricValue"]))
	m.Unit = coerceJSONString(raw["unit"])
	m.Confidence = coerceJSONFloat(raw["confidence"])
	m.Scope = coerceJSONString(raw["scope"])
	m.Description = coerceJSONString(raw["description"])
	return nil
}

type StressEvidence struct {
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Domain  string `json:"domain,omitempty"`
}

type stressFindingEnvelope struct {
	Findings []StressFinding `json:"findings"`
}

type typedJSONVerifier[T any] struct{}

func (v typedJSONVerifier[T]) VerifyAndParse(rawJSON string) (interface{}, error) {
	var out T
	cleaned, err := (&ai.BaseVerifier{}).CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func coerceJSONString(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func coerceJSONStringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return uniqueCanonicalRefs(v)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text := coerceJSONString(item)
			if text != "" {
				out = append(out, text)
			}
		}
		return uniqueCanonicalRefs(out)
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		return []string{text}
	default:
		text := coerceJSONString(raw)
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func coerceJSONFloat(raw interface{}) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case bool:
		if v {
			return 1
		}
		return 0
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0
		}
		if strings.EqualFold(trimmed, "true") {
			return 1
		}
		if strings.EqualFold(trimmed, "false") {
			return 0
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0
		}
		return parsed
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func coerceJSONInt(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func firstNonEmptyValue(values ...interface{}) interface{} {
	for _, value := range values {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(v) != "" {
				return v
			}
		default:
			return value
		}
	}
	return nil
}

func (h *HorizonHandlers) IdentifyStresses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.identifyStressesCanvas == nil {
		http.Error(w, "Identify stresses canvas not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	lensID = strings.TrimSuffix(strings.TrimSpace(lensID), "/identify-stresses")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}
	if h.lensItemsHandler == nil || h.lensItemsHandler.lensesRepo == nil {
		http.Error(w, "Lens repository not available", http.StatusServiceUnavailable)
		return
	}
	lens, err := h.lensItemsHandler.lensesRepo.Get(r.Context(), lensID)
	if err != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	var req IdentifyStressesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	req.ScopeMode = strings.TrimSpace(strings.ToLower(req.ScopeMode))
	if req.ScopeMode == "" {
		req.ScopeMode = "visible_set"
	}
	if req.ScopeMode != "visible_set" {
		http.Error(w, "scopeMode must be visible_set", http.StatusBadRequest)
		return
	}
	if req.MaxNodes <= 0 {
		req.MaxNodes = defaultIdentifyStressesMaxNodes
	}
	if req.MaxArtifacts <= 0 {
		req.MaxArtifacts = defaultIdentifyStressesMaxArtifacts
	}
	if req.MaxFindings <= 0 {
		req.MaxFindings = defaultIdentifyStressesMaxFindings
	}
	req.MaxNodes = minInt(req.MaxNodes, 80)
	req.MaxArtifacts = minInt(req.MaxArtifacts, 50)
	req.MaxFindings = minInt(req.MaxFindings, 240)

	jobID := "identify_stresses_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	summary, err := h.identifyStressesCanvas.ResolveScopeSummary(r.Context(), lens, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := IdentifyStressesResponse{
		JobID:         jobID,
		AcceptedScope: summary,
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		StreamChannel: "realtime",
	}
	bgReq := req
	bgLens := *lens
	go h.identifyStressesCanvas.RunDetached(strings.TrimSpace(authInfo.TenantID), &bgLens, bgReq, jobID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

type identifyStressScope struct {
	SelectedCanonicalPath string
	NodeRefs              []string
	NodeFragments         []*horizonmodels.HorizonFragment
	Artifacts             []*horizonmodels.MaterializedArtifact
	ArtifactKeys          []string
	SupplyLinkRefs        []string
	VisibleRelations      []IdentifyStressVisibleRelation
	GeoRefs               []string
	ZoneRefs              []string
	AffectedZoneRefs      []string
}

func (c *IdentifyStressesCanvas) ResolveScopeSummary(ctx context.Context, lens *horizonmodels.Lens, req IdentifyStressesRequest) (IdentifyStressesScopeSummary, error) {
	scope, err := c.resolveScope(ctx, lens, req)
	if err != nil {
		return IdentifyStressesScopeSummary{}, err
	}
	return IdentifyStressesScopeSummary{
		ScopeMode:             req.ScopeMode,
		SelectedCanonicalPath: scope.SelectedCanonicalPath,
		VisibleNodes:          len(uniqueCanonicalRefs(req.VisibleNodeRefs)),
		AcceptedNodes:         len(scope.NodeRefs),
		VisibleArtifacts:      len(uniqueCanonicalRefs(req.VisibleArtifactKeys)),
		AcceptedArtifacts:     len(scope.Artifacts),
		AcceptedSupplyLinks:   len(scope.SupplyLinkRefs),
		VisibleRelations:      len(normalizeVisibleRelations(req.VisibleRelations)),
		AcceptedRelations:     len(scope.VisibleRelations),
	}, nil
}

func (c *IdentifyStressesCanvas) DebugIdentify(ctx context.Context, tenantID string, lens *horizonmodels.Lens, req IdentifyStressesRequest) (*IdentifyStressesDebugResult, error) {
	if c == nil {
		return nil, fmt.Errorf("identify-stresses canvas is nil")
	}
	if c.exec == nil {
		return nil, fmt.Errorf("identify-stresses: AI executor is not configured")
	}
	if c.fragmentsRepo == nil || c.materializedRepo == nil || c.lensesRepo == nil {
		return nil, fmt.Errorf("identify-stresses: repositories are not configured")
	}

	scope, err := c.resolveScope(ctx, lens, req)
	if err != nil {
		return nil, err
	}
	summary, err := c.ResolveScopeSummary(ctx, lens, req)
	if err != nil {
		return nil, err
	}
	grounded, groundingEvidence, err := c.runGroundedSynthesis(ctx, tenantID, lens, scope)
	if err != nil {
		return nil, err
	}
	findings, err := c.normalizeGroundedSynthesis(ctx, tenantID, lens, scope, grounded)
	if err != nil {
		return nil, err
	}
	findings = c.enrichAndBindFindings(findings, scope)
	findings = c.filterAndPrioritizeFindings(findings, scope, req.MaxFindings)
	return &IdentifyStressesDebugResult{
		ScopeSummary:          summary,
		AcceptedNodeRefs:      append([]string(nil), scope.NodeRefs...),
		AcceptedArtifacts:     append([]string(nil), scope.ArtifactKeys...),
		AcceptedSupplyLinks:   append([]string(nil), scope.SupplyLinkRefs...),
		AcceptedGeoRefs:       append([]string(nil), scope.GeoRefs...),
		AcceptedZoneRefs:      append([]string(nil), scope.ZoneRefs...),
		AcceptedAffectedZones: append([]string(nil), scope.AffectedZoneRefs...),
		GroundedDossier:       grounded,
		GroundingEvidence:     append([]StressEvidence(nil), groundingEvidence...),
		Findings:              append([]StressFinding(nil), findings...),
	}, nil
}

func (c *IdentifyStressesCanvas) Run(ctx context.Context, tenantID string, lens *horizonmodels.Lens, req IdentifyStressesRequest, jobID string) error {
	return c.run(ctx, tenantID, lens, req, jobID)
}

func (c *IdentifyStressesCanvas) RunDetached(tenantID string, lens *horizonmodels.Lens, req IdentifyStressesRequest, jobID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := c.run(ctx, firstNonEmptyString(tenantID, strings.TrimSpace(lens.TenantID), "global"), lens, req, jobID); err != nil {
		_ = c.publish(ctx, "horizon.identify_stresses.failed", firstNonEmptyString(tenantID, strings.TrimSpace(lens.TenantID), "global"), map[string]interface{}{
			"jobId":     jobID,
			"tenantId":  firstNonEmptyString(tenantID, strings.TrimSpace(lens.TenantID), "global"),
			"lensId":    strings.TrimSpace(lens.ID),
			"projectId": strings.TrimSpace(lens.ProjectID),
			"error":     err.Error(),
			"status":    "failed",
		})
	}
}

func (c *IdentifyStressesCanvas) publishIdentifyStressesCompletion(
	ctx context.Context,
	tenantID string,
	lens *horizonmodels.Lens,
	jobID string,
	status string,
	findingsApplied int,
	eventsPersisted int,
	affectedZonesCreated int,
	artifactsUpdated int,
	supplyLinksUpdated int,
	propagationBatches int,
	propagationErrors int,
	errors []string,
) {
	payload := map[string]interface{}{
		"jobId":                jobID,
		"tenantId":             tenantID,
		"lensId":               strings.TrimSpace(lens.ID),
		"projectId":            strings.TrimSpace(lens.ProjectID),
		"status":               firstNonEmptyString(status, "completed"),
		"findingsApplied":      findingsApplied,
		"eventsPersisted":      eventsPersisted,
		"affectedZonesCreated": affectedZonesCreated,
		"artifactsUpdated":     artifactsUpdated,
		"supplyLinksUpdated":   supplyLinksUpdated,
		"propagationBatches":   propagationBatches,
		"propagationErrors":    propagationErrors,
		"completedAt":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	if len(errors) > 0 {
		payload["error"] = strings.TrimSpace(errors[0])
		payload["errors"] = append([]string(nil), errors...)
	}
	_ = c.publish(ctx, "horizon.identify_stresses.completed", tenantID, payload)
}

func (c *IdentifyStressesCanvas) run(ctx context.Context, tenantID string, lens *horizonmodels.Lens, req IdentifyStressesRequest, jobID string) error {
	if c == nil {
		return fmt.Errorf("identify-stresses canvas is nil")
	}
	if c.exec == nil {
		return fmt.Errorf("identify-stresses: AI executor is not configured")
	}
	if c.fragmentsRepo == nil || c.materializedRepo == nil || c.lensesRepo == nil {
		return fmt.Errorf("identify-stresses: repositories are not configured")
	}

	scope, err := c.resolveScope(ctx, lens, req)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_ = c.publish(ctx, "horizon.identify_stresses.started", tenantID, map[string]interface{}{
		"jobId":                 jobID,
		"tenantId":              tenantID,
		"lensId":                strings.TrimSpace(lens.ID),
		"projectId":             strings.TrimSpace(lens.ProjectID),
		"scopeMode":             req.ScopeMode,
		"selectedCanonicalPath": scope.SelectedCanonicalPath,
		"startedAt":             now.Format(time.RFC3339Nano),
	})
	_ = c.publish(ctx, "horizon.identify_stresses.scope_resolved", tenantID, map[string]interface{}{
		"jobId":                 jobID,
		"tenantId":              tenantID,
		"lensId":                strings.TrimSpace(lens.ID),
		"projectId":             strings.TrimSpace(lens.ProjectID),
		"acceptedNodeRefs":      scope.NodeRefs,
		"acceptedArtifactKeys":  scope.ArtifactKeys,
		"acceptedSupplyLinks":   scope.SupplyLinkRefs,
		"acceptedRelationCount": len(scope.VisibleRelations),
		"acceptedGeoRefs":       scope.GeoRefs,
		"acceptedZoneRefs":      scope.ZoneRefs,
		"acceptedAffectedZones": scope.AffectedZoneRefs,
	})

	grounded, groundingEvidence, err := c.runGroundedSynthesis(ctx, tenantID, lens, scope)
	if err != nil {
		return err
	}
	findings, err := c.normalizeGroundedSynthesis(ctx, tenantID, lens, scope, grounded)
	if err != nil {
		return err
	}
	findings = c.enrichAndBindFindings(findings, scope)
	findings = c.filterAndPrioritizeFindings(findings, scope, req.MaxFindings)

	if len(findings) == 0 {
		c.publishIdentifyStressesCompletion(ctx, tenantID, lens, jobID, "completed", 0, 0, 0, 0, 0, 0, 0, nil)
		return nil
	}

	var totalApplied int
	var totalEvents int
	var totalZones int
	var totalArtifacts int
	var totalSupplyLinks int
	var propagationBatches int
	var propagationErrors int
	var partialErrors []string

	for start := 0; start < len(findings); start += identifyStressesBatchSize {
		end := start + identifyStressesBatchSize
		if end > len(findings) {
			end = len(findings)
		}
		res, err := c.applyFindingBatch(ctx, tenantID, lens, scope, findings[start:end], groundingEvidence)
		if err != nil {
			if totalApplied > 0 || totalEvents > 0 || totalZones > 0 || totalArtifacts > 0 || totalSupplyLinks > 0 {
				partialErrors = append(partialErrors, err.Error())
				c.publishIdentifyStressesCompletion(
					ctx,
					tenantID,
					lens,
					jobID,
					"partial_success",
					totalApplied,
					totalEvents,
					totalZones,
					totalArtifacts,
					totalSupplyLinks,
					propagationBatches,
					propagationErrors,
					partialErrors,
				)
				return nil
			}
			return err
		}
		totalApplied += res.FindingsApplied
		totalEvents += res.EventsPersisted
		totalZones += res.AffectedZonesCreated
		totalArtifacts += res.ArtifactsUpdated
		totalSupplyLinks += res.SupplyLinksUpdated

		_ = c.publish(ctx, "horizon.identify_stresses.finding_applied", tenantID, map[string]interface{}{
			"jobId":                jobID,
			"tenantId":             tenantID,
			"lensId":               strings.TrimSpace(lens.ID),
			"projectId":            strings.TrimSpace(lens.ProjectID),
			"batchStart":           start,
			"batchEnd":             end,
			"batchFindings":        len(findings[start:end]),
			"findingsApplied":      res.FindingsApplied,
			"touchedArtifacts":     res.TouchedArtifactKeys,
			"touchedNodes":         res.TouchedNodeRefs,
			"touchedSupplyLinks":   res.TouchedSupplyLinkRefs,
			"affectedZonesCreated": res.AffectedZonesCreated,
		})

		if len(res.CreatedEvents) > 0 && c.signalPipeline != nil {
			propagationBatches++
			_ = c.publish(ctx, "horizon.identify_stresses.propagation_started", tenantID, map[string]interface{}{
				"jobId":         jobID,
				"tenantId":      tenantID,
				"lensId":        strings.TrimSpace(lens.ID),
				"projectId":     strings.TrimSpace(lens.ProjectID),
				"eventsInBatch": len(res.CreatedEvents),
			})
			for _, eventFact := range res.CreatedEvents {
				if err := c.signalPipeline.ProcessEvent(ctx, eventFact); err != nil {
					propagationErrors++
					partialErrors = append(partialErrors, fmt.Sprintf("propagation %s: %v", strings.TrimSpace(eventFact.ID), err))
				}
			}
		}

		if len(res.TouchedArtifactKeys) > 0 {
			_ = c.publish(ctx, "horizon.identify_stresses.overlay_updated", tenantID, map[string]interface{}{
				"jobId":                jobID,
				"tenantId":             tenantID,
				"lensId":               strings.TrimSpace(lens.ID),
				"projectId":            strings.TrimSpace(lens.ProjectID),
				"touchedArtifactKeys":  res.TouchedArtifactKeys,
				"touchedArtifactCount": len(res.TouchedArtifactKeys),
				"requiresRefetch":      len(res.TouchedArtifactKeys) > 50,
			})
		}
	}

	status := "completed"
	if len(partialErrors) > 0 {
		status = "partial_success"
	}
	c.publishIdentifyStressesCompletion(
		ctx,
		tenantID,
		lens,
		jobID,
		status,
		totalApplied,
		totalEvents,
		totalZones,
		totalArtifacts,
		totalSupplyLinks,
		propagationBatches,
		propagationErrors,
		partialErrors,
	)
	return nil
}

func (c *IdentifyStressesCanvas) resolveScope(ctx context.Context, lens *horizonmodels.Lens, req IdentifyStressesRequest) (*identifyStressScope, error) {
	if lens == nil {
		return nil, fmt.Errorf("lens is required")
	}
	selected := strings.TrimSpace(req.SelectedCanonicalPath)
	visibleRelations := prioritizeVisibleRelations(
		normalizeVisibleRelations(req.VisibleRelations),
		selected,
		uniqueCanonicalRefs(req.VisibleNodeRefs),
		defaultIdentifyStressesMaxRelations,
	)
	relationNodeRefs := make([]string, 0, len(visibleRelations)*2)
	for _, relation := range visibleRelations {
		if relation.SourceCanonicalPath != "" {
			relationNodeRefs = append(relationNodeRefs, relation.SourceCanonicalPath)
		}
		if relation.TargetCanonicalPath != "" {
			relationNodeRefs = append(relationNodeRefs, relation.TargetCanonicalPath)
		}
	}
	nodeRefs := uniqueCanonicalRefs(append(append([]string{selected}, req.VisibleNodeRefs...), relationNodeRefs...))
	artifactKeys := uniqueCanonicalRefs(req.VisibleArtifactKeys)
	supplyLinkRefs := uniqueCanonicalRefs(req.VisibleSupplyLinkRefs)

	scope := &identifyStressScope{
		SelectedCanonicalPath: selected,
		VisibleRelations:      visibleRelations,
	}

	tenantID := firstNonEmptyString(strings.TrimSpace(lens.TenantID), "global")
	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(artifactKeys))
	for _, artKey := range artifactKeys {
		if c.materializedRepo == nil {
			break
		}
		keyCandidates := uniqueStrings([]string{
			strings.TrimSpace(artKey),
			strings.TrimPrefix(strings.TrimSpace(artKey), "/"),
		})
		tenantCandidates := uniqueStrings([]string{tenantID, "global"})
		var artifact *horizonmodels.MaterializedArtifact
		for _, tenantCandidate := range tenantCandidates {
			for _, keyCandidate := range keyCandidates {
				found, err := c.materializedRepo.GetByCanonicalKeyAnyKind(ctx, tenantCandidate, keyCandidate)
				if err == nil && found != nil {
					artifact = found
					break
				}
			}
			if artifact != nil {
				break
			}
		}
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		artifacts = append(artifacts, artifact)
	}
	if seeded := c.bootstrapVisiblePathArtifacts(ctx, lens, selected, nodeRefs, visibleRelations, supplyLinkRefs, artifactKeys, req.MaxArtifacts); len(seeded) > 0 {
		merged := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts)+len(seeded))
		seen := make(map[string]struct{}, len(artifacts)+len(seeded))
		for _, artifact := range append(artifacts, seeded...) {
			if artifact == nil {
				continue
			}
			key := strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, artifact)
		}
		artifacts = merged
	}
	if synthesized := c.synthesizeOrphanSupplyLinkArtifacts(ctx, lens, supplyLinkRefs, artifacts, req.MaxArtifacts); len(synthesized) > 0 {
		merged := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts)+len(synthesized))
		seen := make(map[string]struct{}, len(artifacts)+len(synthesized))
		for _, artifact := range append(artifacts, synthesized...) {
			if artifact == nil {
				continue
			}
			key := strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, artifact)
		}
		artifacts = merged
	}
	requestedSupplyLinks := setFromStrings(supplyLinkRefs)
	sort.SliceStable(artifacts, func(i, j int) bool {
		leftCoverage := overlapCount(collectSupplyLinkRefsFromArtifact(artifacts[i]), requestedSupplyLinks)
		rightCoverage := overlapCount(collectSupplyLinkRefsFromArtifact(artifacts[j]), requestedSupplyLinks)
		if leftCoverage != rightCoverage {
			return leftCoverage > rightCoverage
		}
		leftSynthetic := isIdentifySyntheticArtifact(artifacts[i])
		rightSynthetic := isIdentifySyntheticArtifact(artifacts[j])
		if leftSynthetic != rightSynthetic {
			return leftSynthetic
		}
		if artifacts[i].PathStressScore == artifacts[j].PathStressScore {
			return strings.TrimSpace(artifacts[i].CanonicalKey) < strings.TrimSpace(artifacts[j].CanonicalKey)
		}
		return artifacts[i].PathStressScore > artifacts[j].PathStressScore
	})
	if req.MaxArtifacts > 0 && len(artifacts) > req.MaxArtifacts {
		artifacts = artifacts[:req.MaxArtifacts]
	}
	scope.Artifacts = artifacts
	scope.ArtifactKeys = make([]string, 0, len(artifacts))
	geoSet := make(map[string]struct{})
	zoneSet := make(map[string]struct{})
	affectedSet := make(map[string]struct{})
	supplyLinkSet := make(map[string]struct{})
	for _, ref := range supplyLinkRefs {
		supplyLinkSet[ref] = struct{}{}
	}
	for _, artifact := range artifacts {
		scope.ArtifactKeys = append(scope.ArtifactKeys, strings.TrimSpace(artifact.CanonicalKey))
		for _, ref := range artifact.GeoRefs {
			addRef(geoSet, ref)
		}
		for _, ref := range artifact.ZoneRefs {
			addRef(zoneSet, ref)
		}
		for _, ref := range artifact.AffectedZoneRefs {
			addRef(affectedSet, ref)
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			addRef(supplyLinkSet, leg.SupplyLinkCanonicalPath)
			for _, ref := range leg.GeoRefs {
				addRef(geoSet, ref)
			}
			for _, ref := range leg.ZoneRefs {
				addRef(zoneSet, ref)
			}
			for _, ref := range leg.AffectedZoneRefs {
				addRef(affectedSet, ref)
			}
		}
		for _, edge := range parsed.Edges {
			addRef(supplyLinkSet, edge.SupplyLinkCanonicalPath)
		}
	}
	scope.SupplyLinkRefs = sortedRefKeys(supplyLinkSet)
	scope.GeoRefs = sortedRefKeys(geoSet)
	scope.ZoneRefs = sortedRefKeys(zoneSet)
	scope.AffectedZoneRefs = sortedRefKeys(affectedSet)
	preferredNodeSet := make(map[string]struct{})
	addRef(preferredNodeSet, selected)
	for _, artifact := range artifacts {
		for _, ref := range collectArtifactNodeRefs(artifact) {
			addRef(preferredNodeSet, ref)
		}
	}
	nodeRefs = uniqueCanonicalRefs(append(nodeRefs, sortedRefKeys(preferredNodeSet)...))
	if len(nodeRefs) > 0 && c.fragmentsRepo != nil {
		frags, err := c.fragmentsRepo.ListByCanonicalPaths(ctx, nodeRefs)
		if err != nil {
			return nil, fmt.Errorf("identify-stresses: load node scope: %w", err)
		}
		byPath := make(map[string]*horizonmodels.HorizonFragment, len(frags))
		for _, frag := range frags {
			if frag == nil {
				continue
			}
			cp := strings.TrimSpace(frag.CanonicalPath)
			if cp != "" {
				byPath[cp] = frag
			}
		}
		scored := make([]*horizonmodels.HorizonFragment, 0, len(byPath))
		for _, cp := range nodeRefs {
			if frag := byPath[cp]; frag != nil {
				scored = append(scored, frag)
			}
		}
		sort.SliceStable(scored, func(i, j int) bool {
			left := scored[i]
			right := scored[j]
			leftPath := strings.TrimSpace(left.CanonicalPath)
			rightPath := strings.TrimSpace(right.CanonicalPath)
			if leftPath == selected {
				return true
			}
			if rightPath == selected {
				return false
			}
			_, leftPreferred := preferredNodeSet[leftPath]
			_, rightPreferred := preferredNodeSet[rightPath]
			if leftPreferred != rightPreferred {
				return leftPreferred
			}
			leftScore := metricValue(left.State, "supply_chain_criticality")
			rightScore := metricValue(right.State, "supply_chain_criticality")
			if leftScore == rightScore {
				return leftPath < rightPath
			}
			return leftScore > rightScore
		})
		if req.MaxNodes > 0 && len(scored) > req.MaxNodes {
			scored = scored[:req.MaxNodes]
		}
		scope.NodeFragments = scored
		scope.NodeRefs = make([]string, 0, len(scored))
		for _, frag := range scored {
			scope.NodeRefs = append(scope.NodeRefs, strings.TrimSpace(frag.CanonicalPath))
		}
	}
	return scope, nil
}

func (c *IdentifyStressesCanvas) bootstrapVisiblePathArtifacts(
	ctx context.Context,
	lens *horizonmodels.Lens,
	selected string,
	nodeRefs []string,
	visibleRelations []IdentifyStressVisibleRelation,
	supplyLinkRefs []string,
	artifactKeys []string,
	maxArtifacts int,
) []*horizonmodels.MaterializedArtifact {
	if c.materializedRepo == nil || lens == nil {
		return nil
	}
	if len(artifactKeys) >= maxInt(minInt(maxArtifacts, 6), 3) && len(supplyLinkRefs) >= 6 {
		return nil
	}
	candidateNodeRefs := uniqueCanonicalRefs(nodeRefs)
	if selected != "" {
		candidateNodeRefs = uniqueCanonicalRefs(append([]string{selected}, candidateNodeRefs...))
	}
	for _, relation := range visibleRelations {
		if relation.SourceCanonicalPath != "" {
			candidateNodeRefs = append(candidateNodeRefs, relation.SourceCanonicalPath)
		}
		if relation.TargetCanonicalPath != "" {
			candidateNodeRefs = append(candidateNodeRefs, relation.TargetCanonicalPath)
		}
	}
	candidateNodeRefs = uniqueCanonicalRefs(candidateNodeRefs)
	if len(candidateNodeRefs) == 0 {
		return nil
	}

	type scoredArtifact struct {
		artifact *horizonmodels.MaterializedArtifact
		score    float64
	}
	foundByKey := map[string]scoredArtifact{}
	queryLimit := maxInt(maxArtifacts*8, 120)
	for _, tenantCandidate := range uniqueStrings([]string{strings.TrimSpace(lens.TenantID), "global"}) {
		found, err := c.materializedRepo.FindByNodeRefsAny(ctx, tenantCandidate, candidateNodeRefs, horizonmodels.MaterializedKindSupplyChainPath, queryLimit)
		if err != nil {
			if c.logger != nil {
				c.logger.Printf("identify-stresses: bootstrap path discovery warning tenant=%s: %v", tenantCandidate, err)
			}
			continue
		}
		for _, artifact := range found {
			if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
				continue
			}
			horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
			score := scoreVisiblePathCandidate(artifact, selected, candidateNodeRefs, visibleRelations, supplyLinkRefs)
			if score <= 0 {
				continue
			}
			key := strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
			existing, ok := foundByKey[key]
			if !ok || score > existing.score {
				foundByKey[key] = scoredArtifact{artifact: artifact, score: score}
			}
		}
	}
	if len(foundByKey) == 0 {
		return nil
	}
	scored := make([]scoredArtifact, 0, len(foundByKey))
	for _, item := range foundByKey {
		scored = append(scored, item)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return strings.TrimSpace(scored[i].artifact.CanonicalKey) < strings.TrimSpace(scored[j].artifact.CanonicalKey)
		}
		return scored[i].score > scored[j].score
	})

	importLimit := maxInt(minInt(maxArtifacts, 24), 6)
	out := make([]*horizonmodels.MaterializedArtifact, 0, minInt(len(scored), importLimit))
	for _, item := range scored {
		if len(out) >= importLimit {
			break
		}
		artifact := c.ensureLensScopedArtifact(ctx, lens, item.artifact)
		if artifact == nil {
			continue
		}
		out = append(out, artifact)
	}
	if len(out) > 0 && c.logger != nil {
		c.logger.Printf("identify-stresses: bootstrapped %d route artifacts for lens %s", len(out), strings.TrimSpace(lens.ID))
	}
	return out
}

func (c *IdentifyStressesCanvas) ensureLensScopedArtifact(ctx context.Context, lens *horizonmodels.Lens, artifact *horizonmodels.MaterializedArtifact) *horizonmodels.MaterializedArtifact {
	if c.materializedRepo == nil || lens == nil || artifact == nil {
		return nil
	}
	key := strings.TrimSpace(artifact.CanonicalKey)
	if key == "" {
		return nil
	}
	tenantID := strings.TrimSpace(lens.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if strings.TrimSpace(artifact.TenantID) == tenantID {
		if strings.TrimSpace(artifact.LensID) == "" || strings.TrimSpace(artifact.LensID) == strings.TrimSpace(lens.ID) {
			if strings.TrimSpace(artifact.LensID) != strings.TrimSpace(lens.ID) {
				artifact.LensID = strings.TrimSpace(lens.ID)
				artifact.UpdatedAt = time.Now().UTC()
				artifact.UpdatedBy = "identify_stresses:bootstrap_scope"
				if err := c.materializedRepo.Update(ctx, artifact); err != nil {
					if c.logger != nil {
						c.logger.Printf("identify-stresses: bootstrap update warning for %s: %v", key, err)
					}
					return nil
				}
			}
			c.upsertLensScopeArtifact(ctx, lens, artifact)
			return artifact
		}
		return nil
	}

	existing, err := c.materializedRepo.GetByCanonicalKey(ctx, tenantID, horizonmodels.MaterializedKindSupplyChainPath, key)
	if err == nil && existing != nil {
		if strings.TrimSpace(existing.LensID) == "" || strings.TrimSpace(existing.LensID) == strings.TrimSpace(lens.ID) {
			if strings.TrimSpace(existing.LensID) != strings.TrimSpace(lens.ID) {
				existing.LensID = strings.TrimSpace(lens.ID)
				existing.UpdatedAt = time.Now().UTC()
				existing.UpdatedBy = "identify_stresses:bootstrap_scope"
				if err := c.materializedRepo.Update(ctx, existing); err != nil {
					if c.logger != nil {
						c.logger.Printf("identify-stresses: bootstrap relink warning for %s: %v", key, err)
					}
					return nil
				}
			}
			c.upsertLensScopeArtifact(ctx, lens, existing)
			return existing
		}
		return nil
	}

	clone := *artifact
	clone.ID = ""
	clone.TenantID = tenantID
	clone.LensID = strings.TrimSpace(lens.ID)
	clone.CreatedAt = time.Time{}
	clone.UpdatedAt = time.Time{}
	clone.CreatedBy = "identify_stresses:bootstrap_scope"
	clone.UpdatedBy = "identify_stresses:bootstrap_scope"
	id, err := c.materializedRepo.Create(ctx, &clone)
	if err != nil {
		if c.logger != nil {
			c.logger.Printf("identify-stresses: bootstrap clone warning for %s: %v", key, err)
		}
		return nil
	}
	created, err := c.materializedRepo.Get(ctx, id)
	if err != nil || created == nil {
		if c.logger != nil {
			c.logger.Printf("identify-stresses: bootstrap reload warning for %s: %v", key, err)
		}
		return nil
	}
	c.upsertLensScopeArtifact(ctx, lens, created)
	return created
}

func (c *IdentifyStressesCanvas) upsertLensScopeArtifact(ctx context.Context, lens *horizonmodels.Lens, artifact *horizonmodels.MaterializedArtifact) {
	if c.lensesRepo == nil || lens == nil || artifact == nil {
		return
	}
	if err := c.lensesRepo.UpsertScopeArtifact(ctx, strings.TrimSpace(lens.ID), strings.TrimSpace(artifact.CanonicalKey), identifyScopeArtifactRef(artifact)); err != nil && c.logger != nil {
		c.logger.Printf("identify-stresses: scope artifact upsert warning for %s: %v", strings.TrimSpace(artifact.CanonicalKey), err)
	}
}

func (c *IdentifyStressesCanvas) synthesizeOrphanSupplyLinkArtifacts(
	ctx context.Context,
	lens *horizonmodels.Lens,
	supplyLinkRefs []string,
	artifacts []*horizonmodels.MaterializedArtifact,
	maxArtifacts int,
) []*horizonmodels.MaterializedArtifact {
	if c.fragmentsRepo == nil || c.materializedRepo == nil || lens == nil {
		return nil
	}
	synthLimit := maxInt(minInt(maxArtifacts, 12), 6)
	out := make([]*horizonmodels.MaterializedArtifact, 0, synthLimit)
	for _, ref := range supplyLinkRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		frag, err := c.fragmentsRepo.GetByCanonicalPath(ctx, ref)
		if err != nil || frag == nil {
			continue
		}
		productRef := ""
		if frag.ProductRef != nil {
			productRef = strings.TrimSpace(*frag.ProductRef)
		}
		if hasMatchingArtifactForSupplyLink(artifacts, ref, productRef) || hasMatchingArtifactForSupplyLink(out, ref, productRef) {
			continue
		}
		artifact := c.ensureSyntheticSupplyLinkArtifact(ctx, lens, frag)
		if artifact == nil {
			continue
		}
		out = append(out, artifact)
		if len(out) >= synthLimit {
			break
		}
	}
	if len(out) > 0 && c.logger != nil {
		c.logger.Printf("identify-stresses: synthesized %d single-link path artifacts for lens %s", len(out), strings.TrimSpace(lens.ID))
	}
	return out
}

func hasMatchingArtifactForSupplyLink(artifacts []*horizonmodels.MaterializedArtifact, supplyLinkRef, productRef string) bool {
	supplyLinkRef = strings.TrimSpace(supplyLinkRef)
	productRef = strings.TrimSpace(productRef)
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if !containsString(collectSupplyLinkRefsFromArtifact(artifact), supplyLinkRef) {
			continue
		}
		if productRef == "" || containsString(collectArtifactProductRefs(artifact), productRef) {
			return true
		}
	}
	return false
}

func (c *IdentifyStressesCanvas) ensureSyntheticSupplyLinkArtifact(ctx context.Context, lens *horizonmodels.Lens, fragment *horizonmodels.HorizonFragment) *horizonmodels.MaterializedArtifact {
	artifact := buildSingleSupplyLinkArtifact(fragment, firstNonEmptyString(strings.TrimSpace(lens.TenantID), "global"), strings.TrimSpace(lens.ID))
	if artifact == nil {
		return nil
	}
	return c.ensureLensScopedArtifact(ctx, lens, artifact)
}

func scoreVisiblePathCandidate(
	artifact *horizonmodels.MaterializedArtifact,
	selected string,
	nodeRefs []string,
	visibleRelations []IdentifyStressVisibleRelation,
	supplyLinkRefs []string,
) float64 {
	if artifact == nil {
		return 0
	}
	score := 0.0
	nodeSet := make(map[string]struct{}, len(nodeRefs))
	for _, ref := range nodeRefs {
		addRef(nodeSet, ref)
	}
	artifactNodes := collectArtifactNodeRefs(artifact)
	nodeOverlap := 0
	for _, ref := range artifactNodes {
		if _, ok := nodeSet[ref]; ok {
			nodeOverlap++
		}
	}
	score += float64(nodeOverlap) * 16
	if selected != "" && containsString(artifactNodes, selected) {
		score += 28
	}
	if strings.TrimSpace(artifact.AnchorSupplier) == selected || strings.TrimSpace(artifact.AnchorCustomer) == selected {
		score += 18
	}
	if overlap := overlapCount(collectSupplyLinkRefsFromArtifact(artifact), setFromStrings(supplyLinkRefs)); overlap > 0 {
		score += float64(overlap) * 22
	}
	relationMatches := 0
	for _, relation := range visibleRelations {
		if relation.SourceCanonicalPath == "" && relation.TargetCanonicalPath == "" {
			continue
		}
		if containsString(artifactNodes, relation.SourceCanonicalPath) || containsString(artifactNodes, relation.TargetCanonicalPath) {
			relationMatches++
		}
		if strings.TrimSpace(relation.SourceCanonicalPath) != "" && strings.TrimSpace(relation.TargetCanonicalPath) != "" {
			if artifactContainsLegEndpoints(artifact, relation.SourceCanonicalPath, relation.TargetCanonicalPath) {
				relationMatches += 2
			}
		}
	}
	score += float64(relationMatches) * 6
	score += artifact.PathStressScore * 8
	score += math.Min(float64(artifact.LegCount), 6) * 1.5
	return score
}

func (c *IdentifyStressesCanvas) runGroundedSynthesis(ctx context.Context, tenantID string, lens *horizonmodels.Lens, scope *identifyStressScope) (string, []StressEvidence, error) {
	prompt := c.buildGroundedSynthesisPrompt(lens, scope)
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:            "gemini-3-flash-preview",
			MinContextWindow:          32000,
			MaxOutputSize:             16384,
			MinCapabilityScore:        6,
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
		},
		TenantID:  tenantID,
		Project:   strings.TrimSpace(lens.ProjectID),
		UseCase:   "horizon.identify_stresses",
		CascadeID: strings.TrimSpace(lens.ID),
	}
	res, err := c.exec.ExecuteTask(ctx, task)
	if err != nil {
		return "", nil, fmt.Errorf("identify-stresses grounded synthesis: %w", err)
	}
	text := strings.TrimSpace(res.Content)
	evidence := make([]StressEvidence, 0, len(res.Citations))
	for _, citation := range res.Citations {
		rawURL := strings.TrimSpace(citation.URL)
		if rawURL == "" {
			continue
		}
		domain := evidenceDomain(rawURL)
		evidence = append(evidence, StressEvidence{
			URL:    rawURL,
			Title:  strings.TrimSpace(citation.Title),
			Domain: domain,
		})
	}
	return text, evidence, nil
}

func (c *IdentifyStressesCanvas) normalizeGroundedSynthesis(ctx context.Context, tenantID string, lens *horizonmodels.Lens, scope *identifyStressScope, grounded string) ([]StressFinding, error) {
	prompt := c.buildNormalizationPrompt(lens, scope, grounded)
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		Verifier:     typedJSONVerifier[stressFindingEnvelope]{},
		AIRequirements: ai.AIRequirements{
			PreferredModel:           "gemini-3-flash-preview",
			MinContextWindow:         32000,
			MaxOutputSize:            16384,
			MinCapabilityScore:       6,
			TextGenerationCapability: true,
		},
		TenantID:  tenantID,
		Project:   strings.TrimSpace(lens.ProjectID),
		UseCase:   "horizon.identify_stresses",
		CascadeID: strings.TrimSpace(lens.ID),
	}
	raw, err := c.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		fallback := parseGroundedDossierFindings(grounded)
		if len(fallback) > 0 {
			return fallback, nil
		}
		return nil, fmt.Errorf("identify-stresses normalize: %w", err)
	}
	typed, ok := raw.(stressFindingEnvelope)
	if !ok {
		fallback := parseGroundedDossierFindings(grounded)
		if len(fallback) > 0 {
			return fallback, nil
		}
		return nil, fmt.Errorf("identify-stresses normalize: unexpected verifier result")
	}
	if len(typed.Findings) == 0 {
		if fallback := parseGroundedDossierFindings(grounded); len(fallback) > 0 {
			return fallback, nil
		}
	}
	return typed.Findings, nil
}

func parseGroundedDossierFindings(grounded string) []StressFinding {
	lines := strings.Split(grounded, "\n")
	blocks := make([][]string, 0, 8)
	current := []string{}
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(strings.ToUpper(line), "FINDING ") {
			if len(current) > 0 {
				blocks = append(blocks, current)
			}
			current = []string{line}
			continue
		}
		if len(current) == 0 {
			continue
		}
		current = append(current, rawLine)
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}
	out := make([]StressFinding, 0, len(blocks))
	for _, block := range blocks {
		finding := parseGroundedDossierBlock(block)
		if strings.TrimSpace(finding.TargetType) == "" && len(finding.TargetRefs) == 0 && strings.TrimSpace(finding.Summary) == "" {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func parseGroundedDossierBlock(lines []string) StressFinding {
	finding := StressFinding{
		QualifierDeltas: map[string]interface{}{},
		FactType:        "stress_finding",
	}
	var evidence []StressEvidence
	evidenceMode := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(line), "FINDING ") {
			finding.ID = "dossier_" + normalizeStressToken(strings.TrimPrefix(strings.ToLower(line), "finding "))
			continue
		}
		if evidenceMode {
			if strings.HasPrefix(line, "- ") {
				url := strings.TrimSpace(strings.TrimPrefix(line, "- "))
				if url != "" {
					evidence = append(evidence, StressEvidence{URL: url, Domain: evidenceDomain(url)})
				}
				continue
			}
			evidenceMode = false
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := normalizeStressToken(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "targettype":
			finding.TargetType = normalizeStressToken(value)
		case "targetrefs":
			finding.TargetRefs = splitDossierRefs(value)
		case "artifactkey":
			finding.ArtifactKey = strings.TrimSpace(value)
		case "legindex":
			if idx, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				finding.LegIndex = &idx
			}
		case "supplylinkref":
			finding.SupplyLinkRef = strings.TrimSpace(value)
		case "productref":
			finding.ProductRef = strings.TrimSpace(value)
		case "stressortype":
			finding.StressorType = normalizeStressorToken(value)
		case "primarymechanism", "mechanism":
			finding.Mechanism = normalizeMechanismToken(value)
		case "severity":
			finding.Severity = normalizeSeverityToken(value)
		case "confidence":
			finding.Confidence = coerceJSONFloat(value)
		case "effectiveat":
			finding.EffectiveAt = strings.TrimSpace(value)
		case "timehorizon":
			finding.TimeHorizon = normalizeTimeHorizonToken(value)
		case "georefs":
			finding.GeoRefs = splitDossierRefs(value)
		case "flowrefs":
			finding.FlowRefs = splitDossierRefs(value)
		case "zonerefs":
			finding.ZoneRefs = splitDossierRefs(value)
		case "affectedzonerefs":
			finding.AffectedZoneRefs = splitDossierRefs(value)
		case "metricimpacts":
			finding.MetricObservations = parseDossierMetricImpacts(value)
		case "qualifierdeltas":
			finding.QualifierDeltas = parseDossierQualifiers(value)
		case "summary":
			finding.Summary = value
		case "evidence":
			evidenceMode = true
		}
	}
	if len(evidence) > 0 {
		finding.Evidence = evidence
	}
	if finding.EventType == "" {
		finding.EventType = finding.StressorType
	}
	if len(finding.QualifierDeltas) == 0 {
		finding.QualifierDeltas = nil
	}
	return finding
}

func splitDossierRefs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	return uniqueCanonicalRefs(out)
}

func parseDossierMetricImpacts(value string) []StressMetricObservation {
	items := strings.Split(value, ";")
	out := make([]StressMetricObservation, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		metricParts := strings.Split(item, "|")
		left := strings.TrimSpace(metricParts[0])
		scope := ""
		confidence := 0.0
		if len(metricParts) > 1 {
			scope = strings.TrimSpace(metricParts[1])
		}
		if len(metricParts) > 2 {
			confidence = coerceJSONFloat(metricParts[2])
		}
		keyValue := strings.SplitN(left, "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		metricKey := normalizeStressToken(strings.TrimSpace(keyValue[0]))
		right := strings.TrimSpace(keyValue[1])
		valuePart := right
		unit := ""
		if fields := strings.Fields(right); len(fields) > 1 {
			valuePart = fields[0]
			unit = strings.Join(fields[1:], " ")
		}
		if valuePart == "" {
			continue
		}
		out = append(out, StressMetricObservation{
			MetricKey:   metricKey,
			Value:       coerceJSONFloat(valuePart),
			Unit:        unit,
			Confidence:  confidence,
			Scope:       scope,
			Description: "",
		})
	}
	return out
}

func parseDossierQualifiers(value string) map[string]interface{} {
	items := strings.Split(value, ";")
	out := make(map[string]interface{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := normalizeStressToken(parts[0])
		rawValue := strings.TrimSpace(parts[1])
		switch strings.ToLower(rawValue) {
		case "true":
			out[key] = true
		case "false":
			out[key] = false
		default:
			if number, err := strconv.ParseFloat(rawValue, 64); err == nil {
				out[key] = number
			} else {
				out[key] = rawValue
			}
		}
	}
	return out
}

func normalizeStressToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '/' {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	out = strings.ReplaceAll(out, "/", "_")
	out = strings.ReplaceAll(out, "__", "_")
	return strings.Trim(out, "_")
}

func normalizeStressorToken(value string) string {
	token := normalizeStressToken(value)
	switch token {
	case "security_conflict":
		return "security_conflict"
	case "route_disruption", "route_fragility":
		return "route_disruption"
	case "carrier_service_interruption", "carrier_or_service_interruption", "service_interruption":
		return "carrier_or_service_interruption"
	case "supplier_outage":
		return "supplier_outage"
	case "capacity_shock", "capacity_drop":
		return "capacity_drop"
	case "inventory_shortfall", "inventory_vulnerability":
		return "inventory_shortfall"
	case "labor_action":
		return "labor_action"
	case "export_controls", "export_control":
		return "export_controls"
	case "customs_restrictions", "customs_friction":
		return "customs_restrictions"
	}
	return token
}

func normalizeMechanismToken(value string) string {
	token := normalizeStressToken(value)
	switch token {
	case "route_disruption", "route_fragility":
		return "route_fragility"
	case "carrier_service_interruption", "carrier_or_service_interruption", "service_dependency":
		return "service_dependency"
	case "supplier_outage", "sole_source_lock_in":
		return "sole_source_lock_in"
	case "capacity_shock", "inventory_vulnerability", "inventory_shortfall":
		return "inventory_vulnerability"
	case "control_exposure", "export_controls", "regulatory_block":
		return "control_exposure"
	case "customs_friction", "customs_restrictions", "port_or_border_congestion":
		return "customs_friction"
	case "low_substitutability":
		return "low_substitutability"
	}
	return token
}

func normalizeSeverityToken(value string) string {
	token := normalizeStressToken(value)
	switch token {
	case "critical", "high", "medium", "low":
		return token
	}
	return ""
}

func normalizeTimeHorizonToken(value string) string {
	token := normalizeStressToken(value)
	switch token {
	case "immediate", "near_term", "medium_term", "long_term", "unknown":
		return token
	}
	return ""
}

func (c *IdentifyStressesCanvas) enrichAndBindFindings(findings []StressFinding, scope *identifyStressScope) []StressFinding {
	if len(findings) == 0 || scope == nil {
		return findings
	}
	out := make([]StressFinding, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		normalized := c.bindFindingToScope(finding, scope)
		fingerprint := identifyStressFingerprint(normalized)
		if _, ok := seen[fingerprint]; ok {
			continue
		}
		seen[fingerprint] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func (c *IdentifyStressesCanvas) bindFindingToScope(finding StressFinding, scope *identifyStressScope) StressFinding {
	if scope == nil || len(scope.Artifacts) == 0 {
		return finding
	}
	finding.TargetRefs = normalizeRefsAgainstScope(finding.TargetRefs, scope.NodeRefs)
	finding.GeoRefs = normalizeRefsAgainstScope(finding.GeoRefs, scope.GeoRefs)
	finding.ZoneRefs = normalizeRefsAgainstScope(finding.ZoneRefs, scope.ZoneRefs)
	finding.AffectedZoneRefs = normalizeRefsAgainstScope(finding.AffectedZoneRefs, scope.AffectedZoneRefs)
	routeScoped := false
	if strings.TrimSpace(finding.ArtifactKey) != "" || strings.TrimSpace(finding.SupplyLinkRef) != "" || strings.TrimSpace(finding.ProductRef) != "" {
		routeScoped = true
	}
	currentMatch := artifactBindingMatch{}
	if currentKey := strings.TrimPrefix(strings.TrimSpace(finding.ArtifactKey), "/"); currentKey != "" {
		for _, artifact := range scope.Artifacts {
			if artifact == nil {
				continue
			}
			if strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/") == currentKey {
				currentMatch = artifactBindingForFinding(artifact, finding)
				break
			}
		}
	}
	bestMatch := bestArtifactBindingForFinding(scope.Artifacts, finding)
	shouldReplace := false
	switch {
	case bestMatch.Artifact == nil || bestMatch.Score < 30:
	case currentMatch.Artifact == nil:
		shouldReplace = true
	case currentMatch.Score < 20:
		shouldReplace = true
	case bindingMismatch(currentMatch, finding) && bestMatch.Score >= currentMatch.Score:
		shouldReplace = true
	case bestMatch.Score > currentMatch.Score+25:
		shouldReplace = true
	}
	if shouldReplace {
		finding.ArtifactKey = strings.TrimSpace(bestMatch.Artifact.CanonicalKey)
		if bestMatch.LinkRef != "" {
			finding.SupplyLinkRef = bestMatch.LinkRef
		}
		if bestMatch.LegIndex >= 0 {
			legIndex := bestMatch.LegIndex
			finding.LegIndex = &legIndex
		}
		if bestMatch.ProductRef != "" {
			finding.ProductRef = bestMatch.ProductRef
		}
	}
	if bestMatch.Artifact != nil && bestMatch.Score >= 30 {
		if finding.ArtifactKey == "" {
			finding.ArtifactKey = strings.TrimSpace(bestMatch.Artifact.CanonicalKey)
		}
		if finding.ProductRef == "" {
			finding.ProductRef = bestProductRefForArtifact(bestMatch.Artifact, finding)
		}
		if finding.SupplyLinkRef == "" {
			finding.SupplyLinkRef = bestMatch.LinkRef
		}
		if finding.LegIndex == nil && bestMatch.LegIndex >= 0 {
			legIndex := bestMatch.LegIndex
			finding.LegIndex = &legIndex
		}
		routeScoped = true
	}
	if routeScoped {
		switch {
		case strings.TrimSpace(finding.ArtifactKey) != "" && finding.LegIndex != nil:
			finding.TargetType = "path_leg"
		case strings.TrimSpace(finding.ArtifactKey) != "":
			finding.TargetType = "path"
		case strings.TrimSpace(finding.SupplyLinkRef) != "":
			finding.TargetType = "supply_link"
		}
	}
	return finding
}

type artifactBindingMatch struct {
	Artifact       *horizonmodels.MaterializedArtifact
	LegIndex       int
	LinkRef        string
	ProductRef     string
	Score          float64
	ProductMatched bool
	LinkMatched    bool
}

func bindingMismatch(match artifactBindingMatch, finding StressFinding) bool {
	if match.Artifact == nil {
		return true
	}
	if strings.TrimSpace(finding.SupplyLinkRef) != "" && !match.LinkMatched {
		return true
	}
	if strings.TrimSpace(finding.ProductRef) != "" && !match.ProductMatched {
		return true
	}
	return false
}

func normalizeRefsAgainstScope(refs []string, allowed []string) []string {
	if len(refs) == 0 {
		return nil
	}
	if len(allowed) == 0 {
		return uniqueCanonicalRefs(refs)
	}
	allowedByToken := make(map[string]string, len(allowed))
	for _, ref := range allowed {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		allowedByToken[normalizeStressToken(ref)] = ref
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if strings.HasPrefix(ref, "/") {
			out = append(out, ref)
			continue
		}
		token := normalizeStressToken(ref)
		if token == "" {
			continue
		}
		if mapped, ok := allowedByToken[token]; ok {
			out = append(out, mapped)
			continue
		}
		for _, candidate := range allowed {
			candidateToken := normalizeStressToken(candidate)
			if strings.Contains(candidateToken, token) || strings.Contains(token, candidateToken) {
				out = append(out, candidate)
				break
			}
		}
	}
	return uniqueCanonicalRefs(out)
}

func (c *IdentifyStressesCanvas) filterAndPrioritizeFindings(findings []StressFinding, scope *identifyStressScope, maxFindings int) []StressFinding {
	if len(findings) == 0 {
		return nil
	}
	allowedNodes := make(map[string]struct{}, len(scope.NodeRefs))
	for _, ref := range scope.NodeRefs {
		addRef(allowedNodes, ref)
	}
	allowedArtifacts := make(map[string]struct{}, len(scope.ArtifactKeys))
	for _, ref := range scope.ArtifactKeys {
		addRef(allowedArtifacts, strings.TrimPrefix(strings.TrimSpace(ref), "/"))
	}
	allowedSupplyLinks := make(map[string]struct{}, len(scope.SupplyLinkRefs))
	for _, ref := range scope.SupplyLinkRefs {
		addRef(allowedSupplyLinks, strings.TrimSpace(ref))
	}
	out := make([]StressFinding, 0, len(findings))
	for _, finding := range findings {
		finding.TargetType = normalizeStressToken(finding.TargetType)
		finding.StressorType = normalizeStressorToken(finding.StressorType)
		finding.Mechanism = normalizeMechanismToken(finding.Mechanism)
		finding.Severity = normalizeSeverityToken(finding.Severity)
		finding.TimeHorizon = normalizeTimeHorizonToken(finding.TimeHorizon)
		finding.ArtifactKey = strings.TrimPrefix(strings.TrimSpace(finding.ArtifactKey), "/")
		finding.SupplyLinkRef = strings.TrimSpace(finding.SupplyLinkRef)
		finding.ProductRef = strings.TrimSpace(finding.ProductRef)
		finding.TargetRefs = uniqueCanonicalRefs(finding.TargetRefs)
		finding.GeoRefs = uniqueCanonicalRefs(finding.GeoRefs)
		finding.FlowRefs = uniqueCanonicalRefs(finding.FlowRefs)
		finding.ZoneRefs = uniqueCanonicalRefs(finding.ZoneRefs)
		finding.AffectedZoneRefs = uniqueCanonicalRefs(finding.AffectedZoneRefs)
		if finding.Confidence <= 0 {
			finding.Confidence = 0.62
		}
		if finding.Mechanism == "" {
			if candidates := identifyMechanismByStressor[finding.StressorType]; len(candidates) > 0 {
				finding.Mechanism = candidates[0]
			}
		}
		if finding.EventType == "" {
			finding.EventType = finding.StressorType
		}
		if finding.FactType == "" {
			finding.FactType = "stress_finding"
		}
		keep := false
		switch finding.TargetType {
		case "path", "path_leg":
			if finding.ArtifactKey != "" {
				if _, ok := allowedArtifacts[finding.ArtifactKey]; ok {
					keep = true
				}
			}
		case "supply_link", "product_on_link":
			if finding.SupplyLinkRef != "" {
				if _, ok := allowedSupplyLinks[finding.SupplyLinkRef]; ok {
					keep = true
				}
			}
			if !keep && finding.ArtifactKey != "" {
				_, keep = allowedArtifacts[finding.ArtifactKey]
			}
		default:
			for _, ref := range finding.TargetRefs {
				if _, ok := allowedNodes[ref]; ok {
					keep = true
					break
				}
			}
		}
		if !keep {
			continue
		}
		if strings.TrimSpace(finding.ID) == "" {
			finding.ID = identifyStressFingerprint(finding)
		}
		out = append(out, finding)
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftScore := findingPriorityScore(out[i])
		rightScore := findingPriorityScore(out[j])
		if leftScore == rightScore {
			return identifyStressFingerprint(out[i]) < identifyStressFingerprint(out[j])
		}
		return leftScore > rightScore
	})
	if maxFindings > 0 && len(out) > maxFindings {
		out = out[:maxFindings]
	}
	return out
}

type applyBatchResult struct {
	FindingsApplied       int
	EventsPersisted       int
	AffectedZonesCreated  int
	ArtifactsUpdated      int
	SupplyLinksUpdated    int
	TouchedArtifactKeys   []string
	TouchedNodeRefs       []string
	TouchedSupplyLinkRefs []string
	CreatedEvents         []*coremodels.OntologyFact
}

func (c *IdentifyStressesCanvas) applyFindingBatch(ctx context.Context, tenantID string, lens *horizonmodels.Lens, scope *identifyStressScope, findings []StressFinding, groundingEvidence []StressEvidence) (*applyBatchResult, error) {
	now := time.Now().UTC()
	quasiSourceID := ""
	if c.dataSourcesRepo != nil {
		if id, err := c.dataSourcesRepo.EnsureLLMSearchQuasiSource(ctx); err == nil {
			quasiSourceID = id
		}
	}

	artifactByKey := make(map[string]*horizonmodels.MaterializedArtifact, len(scope.Artifacts))
	for _, artifact := range scope.Artifacts {
		if artifact == nil {
			continue
		}
		artifactByKey[strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")] = artifact
	}
	fragmentByPath := make(map[string]*horizonmodels.HorizonFragment, len(scope.NodeFragments))
	for _, frag := range scope.NodeFragments {
		if frag == nil {
			continue
		}
		fragmentByPath[strings.TrimSpace(frag.CanonicalPath)] = frag
	}
	supplyLinkFragments := map[string]*horizonmodels.HorizonFragment{}
	for _, ref := range scope.SupplyLinkRefs {
		if frag, err := c.fragmentsRepo.GetByCanonicalPath(ctx, ref); err == nil && frag != nil {
			supplyLinkFragments[strings.TrimSpace(ref)] = frag
			fragmentByPath[strings.TrimSpace(ref)] = frag
		}
	}

	createdEvents := make([]*coremodels.OntologyFact, 0, len(findings))
	updatedArtifacts := map[string]*horizonmodels.MaterializedArtifact{}
	updatedFragments := map[string]*horizonmodels.HorizonFragment{}
	updatedEdges := map[string]*horizonmodels.DependencyEdge{}
	metricObservationIDs := map[string][]string{}
	evidenceFactIDs := map[string][]string{}
	touchedSupplyLinkRefs := map[string]struct{}{}
	touchedNodeRefs := map[string]struct{}{}
	createdZones := 0

	for _, finding := range findings {
		eventFact, claimFact, err := c.upsertStressFacts(ctx, tenantID, lens, finding, quasiSourceID, groundingEvidence, now)
		if err != nil {
			return nil, err
		}
		if eventFact != nil {
			createdEvents = append(createdEvents, eventFact)
		}
		evidenceIDs := []string{}
		if claimFact != nil {
			evidenceIDs = append(evidenceIDs, strings.TrimSpace(claimFact.ID))
		}
		if eventFact != nil {
			evidenceIDs = append(evidenceIDs, strings.TrimSpace(eventFact.ID))
		}

		for _, zoneRef := range finding.AffectedZoneRefs {
			zoneRef = strings.TrimSpace(zoneRef)
			if zoneRef == "" {
				continue
			}
			if _, ok := fragmentByPath[zoneRef]; ok {
				continue
			}
			zoneFrag, zoneCreated, err := c.upsertAffectedZoneFragment(ctx, tenantID, zoneRef, finding, now)
			if err != nil {
				continue
			}
			if zoneFrag != nil {
				fragmentByPath[zoneRef] = zoneFrag
				updatedFragments[zoneFrag.ID] = zoneFrag
				if zoneCreated {
					createdZones++
				}
			}
		}

		touchedArtifacts := c.findTouchedArtifacts(finding, artifactByKey)
		if len(touchedArtifacts) == 0 && finding.ArtifactKey != "" {
			if art := artifactByKey[strings.TrimPrefix(strings.TrimSpace(finding.ArtifactKey), "/")]; art != nil {
				touchedArtifacts = append(touchedArtifacts, art)
			}
		}

		switch finding.TargetType {
		case "supply_link", "product_on_link":
			if frag := c.ensureFragmentByPath(ctx, finding.SupplyLinkRef, fragmentByPath); frag != nil {
				c.applyFindingToFragment(frag, finding, evidenceIDs, now, true)
				updatedFragments[frag.ID] = frag
				addRef(touchedSupplyLinkRefs, finding.SupplyLinkRef)
			}
		default:
			for _, ref := range finding.TargetRefs {
				if frag := c.ensureFragmentByPath(ctx, ref, fragmentByPath); frag != nil {
					c.applyFindingToFragment(frag, finding, evidenceIDs, now, strings.HasPrefix(strings.TrimSpace(ref), "/supply-link/"))
					updatedFragments[frag.ID] = frag
					addRef(touchedNodeRefs, ref)
				}
			}
		}

		for _, artifact := range touchedArtifacts {
			updated, touchedEdgeIDs, obsIDs := c.applyFindingToArtifact(ctx, tenantID, artifact, finding, evidenceIDs, now)
			if updated == nil {
				continue
			}
			key := strings.TrimPrefix(strings.TrimSpace(updated.CanonicalKey), "/")
			artifactByKey[key] = updated
			updatedArtifacts[key] = updated
			if len(obsIDs) > 0 {
				metricObservationIDs[key] = append(metricObservationIDs[key], obsIDs...)
			}
			evidenceFactIDs[key] = append(evidenceFactIDs[key], evidenceIDs...)
			for _, edgeID := range touchedEdgeIDs {
				if edgeID == "" || c.edgesRepo == nil {
					continue
				}
				if edge, err := c.edgesRepo.Get(ctx, edgeID); err == nil && edge != nil {
					updatedEdges[edgeID] = edge
				}
			}
			for _, ref := range collectSupplyLinkRefsFromArtifact(updated) {
				addRef(touchedSupplyLinkRefs, ref)
			}
			for _, ref := range updated.NodeRefs {
				addRef(touchedNodeRefs, ref)
			}
		}
	}

	for _, frag := range updatedFragments {
		if frag == nil {
			continue
		}
		frag.Version++
		if err := c.fragmentsRepo.Update(ctx, frag); err != nil {
			return nil, fmt.Errorf("identify-stresses: update fragment %s: %w", frag.CanonicalPath, err)
		}
		_ = c.publishNodeUpsert(ctx, tenantID, frag, "identify_stresses")
	}

	for _, edge := range updatedEdges {
		if edge == nil || c.edgesRepo == nil {
			continue
		}
		if err := c.edgesRepo.Update(ctx, edge); err != nil {
			return nil, fmt.Errorf("identify-stresses: update edge %s: %w", edge.ID, err)
		}
		_ = c.publishEdgeUpsert(ctx, tenantID, edge)
	}

	updatedArtifactKeys := make([]string, 0, len(updatedArtifacts))
	for key, artifact := range updatedArtifacts {
		if artifact == nil {
			continue
		}
		artifact.MetricObservationIDs = uniqueStrings(append(artifact.MetricObservationIDs, metricObservationIDs[key]...))
		artifact.EvidenceFactIDs = uniqueStrings(append(artifact.EvidenceFactIDs, evidenceFactIDs[key]...))
		annotateSupplyChainArtifactRiskOverlay(artifact)
		if err := c.materializedRepo.Update(ctx, artifact); err != nil {
			return nil, fmt.Errorf("identify-stresses: update artifact %s: %w", artifact.CanonicalKey, err)
		}
		updatedArtifactKeys = append(updatedArtifactKeys, key)
		_ = c.syncLensScopeArtifact(ctx, lens, artifact)
		_ = c.publish(ctx, "horizon.identify_stresses.artifact_upserted", tenantID, artifactRealtimePayload(artifact))
	}

	_ = c.publish(ctx, "horizon.identify_stresses.events_persisted", tenantID, map[string]interface{}{
		"tenantId":        tenantID,
		"lensId":          strings.TrimSpace(lens.ID),
		"projectId":       strings.TrimSpace(lens.ProjectID),
		"eventsPersisted": len(createdEvents),
		"claimsPersisted": len(createdEvents),
	})

	return &applyBatchResult{
		FindingsApplied:       len(findings),
		EventsPersisted:       len(createdEvents),
		AffectedZonesCreated:  createdZones,
		ArtifactsUpdated:      len(updatedArtifacts),
		SupplyLinksUpdated:    len(touchedSupplyLinkRefs),
		TouchedArtifactKeys:   uniqueStrings(updatedArtifactKeys),
		TouchedNodeRefs:       sortedRefKeys(touchedNodeRefs),
		TouchedSupplyLinkRefs: sortedRefKeys(touchedSupplyLinkRefs),
		CreatedEvents:         createdEvents,
	}, nil
}

func (c *IdentifyStressesCanvas) upsertStressFacts(ctx context.Context, tenantID string, lens *horizonmodels.Lens, finding StressFinding, quasiSourceID string, groundingEvidence []StressEvidence, now time.Time) (*coremodels.OntologyFact, *coremodels.OntologyFact, error) {
	if c.factsRepo == nil {
		return nil, nil, nil
	}
	fingerprint := identifyStressFingerprint(finding)
	targetPath := firstFindingCanonicalPath(finding)
	if targetPath == "" {
		targetPath = finding.SupplyLinkRef
	}
	var existingEvent *coremodels.OntologyFact
	if targetPath != "" {
		if facts, err := c.factsRepo.ListByCanonicalPath(ctx, tenantID, targetPath, "event", 50); err == nil {
			for _, fact := range facts {
				if fact == nil {
					continue
				}
				if factHasTag(fact, "stress_fingerprint:"+fingerprint) {
					existingEvent = fact
					break
				}
			}
		}
	}
	if existingEvent != nil {
		if finding.Confidence > existingEvent.Confidence {
			existingEvent.Confidence = finding.Confidence
		}
		existingEvent.UpdatedAt = now
		if err := c.factsRepo.Upsert(ctx, existingEvent); err != nil {
			return nil, nil, err
		}
		var claimFact *coremodels.OntologyFact
		if existingEvent.ClaimID != nil && strings.TrimSpace(*existingEvent.ClaimID) != "" {
			claimFact, _ = c.factsRepo.Get(ctx, strings.TrimSpace(*existingEvent.ClaimID))
		}
		return existingEvent, claimFact, nil
	}

	claimFact := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			TenantID:       tenantID,
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
			CreatedBy:      "system",
			UpdatedBy:      "system",
			Labels:         map[string]string{"en": claimTitleForFinding(finding)},
			Tags:           stressFactTags(finding, fingerprint, "claim"),
			Status:         "active",
			CanonicalPaths: claimCanonicalPaths(finding),
			ActorPaths:     uniqueCanonicalRefs(append([]string{finding.SupplyLinkRef, finding.ProductRef}, finding.TargetRefs...)),
			SpherePaths:    stressSpherePaths(finding),
		},
		Kind:       "claim",
		Type:       firstNonEmptyString(finding.FactType, "stress_finding"),
		Title:      claimTitleForFinding(finding),
		Summary:    strings.TrimSpace(finding.Summary),
		Confidence: clamp01(firstNonZero(finding.Confidence, 0.62)),
		OccurredAt: parseOptionalTime(finding.EffectiveAt),
		ObservedAt: &now,
		SourceIDs:  filterEmptyStrings([]string{quasiSourceID}),
		SourceRefs: evidenceToSourceRefs(finding, groundingEvidence),
	}
	claimID, err := c.factsRepo.Create(ctx, claimFact)
	if err != nil {
		return nil, nil, err
	}
	claimFact.ID = claimID

	eventFact := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			TenantID:       tenantID,
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
			CreatedBy:      "system",
			UpdatedBy:      "system",
			Labels:         map[string]string{"en": eventTitleForFinding(finding)},
			Tags:           stressFactTags(finding, fingerprint, "event"),
			Status:         "active",
			CanonicalPaths: claimCanonicalPaths(finding),
			ActorPaths:     uniqueCanonicalRefs(append([]string{finding.SupplyLinkRef, finding.ProductRef}, finding.TargetRefs...)),
			SpherePaths:    stressSpherePaths(finding),
		},
		Kind:          "event",
		Type:          firstNonEmptyString(finding.EventType, finding.StressorType, "stress_event"),
		Title:         eventTitleForFinding(finding),
		Summary:       strings.TrimSpace(finding.Summary),
		Confidence:    clamp01(firstNonZero(finding.Confidence, 0.62)),
		OccurredAt:    parseOptionalTime(finding.EffectiveAt),
		ObservedAt:    &now,
		ClaimID:       stringPtr(claimID),
		SourceIDs:     filterEmptyStrings([]string{quasiSourceID}),
		SourceRefs:    evidenceToSourceRefs(finding, groundingEvidence),
		MetricUpdates: metricUpdatesFromFinding(finding, now),
		ExtractionLineage: &coremodels.FactExtractionLineage{
			Pipeline:     "identify_stresses",
			SourceType:   coremodels.SourceTypeLLMSearchGrounding,
			CurrentLayer: coremodels.FactExtractionLayerLLM,
			LLMEnabled:   true,
			LLMUsed:      true,
			Steps: []coremodels.FactExtractionStep{
				{Layer: coremodels.FactExtractionLayerLLM, Driver: "gemini_search_grounding", Method: "identify_stresses", Deterministic: false, LLMUsed: true},
			},
		},
	}
	eventID, err := c.factsRepo.Create(ctx, eventFact)
	if err != nil {
		return nil, nil, err
	}
	eventFact.ID = eventID
	return eventFact, claimFact, nil
}

func (c *IdentifyStressesCanvas) upsertAffectedZoneFragment(ctx context.Context, tenantID, canonicalPath string, finding StressFinding, now time.Time) (*horizonmodels.HorizonFragment, bool, error) {
	if c.fragmentsRepo == nil {
		return nil, false, nil
	}
	existing, err := c.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
	if err == nil && existing != nil {
		if existing.Qualifiers == nil {
			existing.Qualifiers = map[string]interface{}{}
		}
		existing.Qualifiers["zoneType"] = firstNonEmptyString(stringQualifier(finding.QualifierDeltas, "zoneType"), "stress_zone")
		if subtype := stringQualifier(finding.QualifierDeltas, "zoneSubType"); subtype != "" {
			existing.Qualifiers["zoneSubType"] = subtype
		}
		if len(finding.Evidence) > 0 {
			existing.Qualifiers["sourceUrl"] = strings.TrimSpace(finding.Evidence[0].URL)
		}
		existing.Metadata.LastFactsAppliedAt = now
		return existing, false, nil
	}
	label := humanizeRefLabel(canonicalPath)
	zone := &horizonmodels.HorizonFragment{
		TenantID:      tenantID,
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
		CreatedBy:     "system",
		UpdatedBy:     "system",
		Labels:        map[string]string{"en": label},
		Status:        string(horizonmodels.FragmentStatusActive),
		CanonicalPath: canonicalPath,
		Type:          horizonmodels.FragmentTypeAffectedZone,
		EntityPath:    "/AffectedZone",
		State:         map[string]horizonmodels.MetricValue{},
		Qualifiers: map[string]interface{}{
			"zoneType":    firstNonEmptyString(stringQualifier(finding.QualifierDeltas, "zoneType"), "stress_zone"),
			"zoneSubType": firstNonEmptyString(stringQualifier(finding.QualifierDeltas, "zoneSubType"), strings.TrimSpace(finding.StressorType)),
		},
		Metadata: horizonmodels.FragmentMetadata{
			CreatedAt:          now,
			LastUpdatedAt:      now,
			LastVerifiedAt:     now,
			LastFactsAppliedAt: now,
			EpistemicStatus:    horizonmodels.EpistemicStatusLowConfidence,
		},
		Governance: horizonmodels.Governance{
			Owner:  "SYSTEM",
			Status: string(horizonmodels.FragmentStatusActive),
		},
	}
	if lat, lon, ok := findingCoordinates(finding); ok {
		zone.Coordinates = &horizonmodels.Coordinates{Lat: lat, Lon: lon}
		zone.HasCoordinates = true
	}
	if radius := numericQualifier(finding.QualifierDeltas, "radiusKm"); radius > 0 {
		zone.RadiusKm = radius
	}
	id, err := c.fragmentsRepo.Create(ctx, zone)
	if err != nil {
		return nil, false, err
	}
	zone.ID = id
	return zone, true, nil
}

func (c *IdentifyStressesCanvas) applyFindingToFragment(fragment *horizonmodels.HorizonFragment, finding StressFinding, evidenceIDs []string, now time.Time, isSupplyLink bool) {
	if fragment == nil {
		return
	}
	if fragment.State == nil {
		fragment.State = map[string]horizonmodels.MetricValue{}
	}
	if fragment.Qualifiers == nil {
		fragment.Qualifiers = map[string]interface{}{}
	}
	metricProvenance := horizonmodels.Provenance{
		Method:     "identify_stresses",
		SourceIDs:  append([]string(nil), evidenceIDs...),
		ObservedAt: &now,
	}
	for _, obs := range finding.MetricObservations {
		key := strings.TrimSpace(obs.MetricKey)
		if key == "" {
			continue
		}
		unit := strings.TrimSpace(obs.Unit)
		confidence := clamp01(firstNonZero(obs.Confidence, finding.Confidence, 0.65))
		fragment.State[key] = horizonmodels.MetricValue{
			Value:      obs.Value,
			Unit:       stringPtrIfNotEmpty(unit),
			Confidence: confidence,
			ObservedAt: now,
			Provenance: metricProvenance,
		}
	}
	severityScore := findingPriorityScore(finding)
	currentCriticality := metricValue(fragment.State, "supply_chain_criticality")
	if severityScore > currentCriticality {
		fragment.State["supply_chain_criticality"] = horizonmodels.MetricValue{
			Value:      severityScore,
			Confidence: clamp01(firstNonZero(finding.Confidence, 0.68)),
			ObservedAt: now,
			Provenance: metricProvenance,
		}
	}
	if isSupplyLink {
		if routeFragility := metricValue(fragment.State, "route_fragility"); routeFragility == 0 {
			fragilityBase := severityScore * 0.72
			if finding.Mechanism == "route_fragility" || finding.Mechanism == "customs_friction" || finding.Mechanism == "service_dependency" {
				fragilityBase = math.Max(fragilityBase, severityScore)
			}
			fragment.State["route_fragility"] = horizonmodels.MetricValue{
				Value:      clamp01(math.Max(fragilityBase, 0.25)),
				Confidence: clamp01(firstNonZero(finding.Confidence, 0.64)),
				ObservedAt: now,
				Provenance: metricProvenance,
			}
		}
		if routeRerouteability := metricValue(fragment.State, "route_rerouteability"); routeRerouteability == 0 {
			fragment.State["route_rerouteability"] = horizonmodels.MetricValue{
				Value:      clamp01(1 - severityScore*0.6),
				Confidence: clamp01(firstNonZero(finding.Confidence, 0.62)),
				ObservedAt: now,
				Provenance: metricProvenance,
			}
		}
	}

	mergeRiskQualifierState(fragment.Qualifiers, finding, fragment.State)
	if len(finding.AffectedZoneRefs) > 0 {
		fragment.Qualifiers["affectedZoneRefs"] = uniqueCanonicalRefs(append(interfaceToStrings(fragment.Qualifiers["affectedZoneRefs"]), finding.AffectedZoneRefs...))
	}
	if len(finding.ZoneRefs) > 0 {
		fragment.Qualifiers["zoneRefs"] = uniqueCanonicalRefs(append(interfaceToStrings(fragment.Qualifiers["zoneRefs"]), finding.ZoneRefs...))
	}
	if len(finding.GeoRefs) > 0 {
		fragment.Qualifiers["geoRefs"] = uniqueCanonicalRefs(append(interfaceToStrings(fragment.Qualifiers["geoRefs"]), finding.GeoRefs...))
	}
	for key, raw := range finding.QualifierDeltas {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		fragment.Qualifiers[key] = raw
	}
	fragment.Metadata.LastFactsAppliedAt = now
	fragment.Metadata.LastUpdatedAt = now
}

func (c *IdentifyStressesCanvas) applyFindingToArtifact(ctx context.Context, tenantID string, artifact *horizonmodels.MaterializedArtifact, finding StressFinding, evidenceIDs []string, now time.Time) (*horizonmodels.MaterializedArtifact, []string, []string) {
	if artifact == nil {
		return nil, nil, nil
	}
	copyArtifact := *artifact
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(copyArtifact.Payload)
	if err != nil || parsed == nil {
		return nil, nil, nil
	}
	copyArtifact.Payload = horizonmodels.BuildSupplyChainPathPayloadDetailed(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Family, parsed.Notes)
	parsed, _ = horizonmodels.ParseSupplyChainPathPayload(copyArtifact.Payload)
	if parsed == nil {
		return nil, nil, nil
	}

	stressEval := pathstress.NewEvaluator()
	bindingEngine := pathbinding.NewEngine(c.fragmentsRepo, c.edgesRepo)
	var touchedEdgeIDs []string
	var observationIDs []string
	var touched bool
	touchedLegs := map[int]struct{}{}
	touchedSupplyLinks := map[string]struct{}{}

	matchesLeg := func(leg horizonmodels.SupplyChainPathLeg) bool {
		if finding.LegIndex != nil && leg.Index == *finding.LegIndex {
			return true
		}
		if finding.ArtifactKey != "" && strings.TrimPrefix(strings.TrimSpace(copyArtifact.CanonicalKey), "/") == strings.TrimPrefix(strings.TrimSpace(finding.ArtifactKey), "/") {
			if finding.TargetType == "path" {
				return true
			}
		}
		if finding.SupplyLinkRef != "" && strings.TrimSpace(leg.SupplyLinkCanonicalPath) == strings.TrimSpace(finding.SupplyLinkRef) {
			return true
		}
		if finding.ProductRef != "" && strings.TrimSpace(leg.ProductRef) == strings.TrimSpace(finding.ProductRef) {
			if finding.SupplyLinkRef == "" || strings.TrimSpace(leg.SupplyLinkCanonicalPath) == strings.TrimSpace(finding.SupplyLinkRef) {
				return true
			}
		}
		for _, ref := range finding.TargetRefs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if ref == strings.TrimSpace(leg.SupplierRef) || ref == strings.TrimSpace(leg.CustomerRef) {
				return true
			}
		}
		return false
	}

	for idx := range parsed.Legs {
		leg := &parsed.Legs[idx]
		if !matchesLeg(*leg) {
			continue
		}
		touched = true
		touchedLegs[leg.Index] = struct{}{}
		addRef(touchedSupplyLinks, leg.SupplyLinkCanonicalPath)
		leg.GeoRefs = uniqueCanonicalRefs(append(leg.GeoRefs, finding.GeoRefs...))
		leg.FlowRefs = uniqueCanonicalRefs(append(leg.FlowRefs, finding.FlowRefs...))
		leg.ZoneRefs = uniqueCanonicalRefs(append(leg.ZoneRefs, finding.ZoneRefs...))
		leg.AffectedZoneRefs = uniqueCanonicalRefs(append(leg.AffectedZoneRefs, finding.AffectedZoneRefs...))
		leg.SpatialSemantics = uniqueStrings(append(leg.SpatialSemantics, inferSpatialSemanticsFromFinding(finding)...))
		binding := bindingEngine.BindSegment(ctx, pathbinding.SegmentInput{
			SupplierRef:       leg.SupplierRef,
			CustomerRef:       leg.CustomerRef,
			ProductRef:        leg.ProductRef,
			TransportViaRefs:  leg.TransportViaRefs,
			SegmentConfidence: leg.SegmentConfidence,
		})
		leg.Mode = firstNonEmptyString(leg.Mode, binding.Mode)
		leg.GeoRefs = uniqueCanonicalRefs(append(leg.GeoRefs, binding.GeoRefs...))
		leg.FlowRefs = uniqueCanonicalRefs(append(leg.FlowRefs, binding.FlowRefs...))
		leg.AlternateFlowRefs = uniqueCanonicalRefs(append(leg.AlternateFlowRefs, binding.AlternateFlowRefs...))
		leg.ZoneRefs = uniqueCanonicalRefs(append(leg.ZoneRefs, binding.ZoneRefs...))
		leg.AffectedZoneRefs = uniqueCanonicalRefs(append(leg.AffectedZoneRefs, binding.AffectedZoneRefs...))
		leg.SpatialSemantics = uniqueStrings(append(leg.SpatialSemantics, binding.SpatialSemantics...))
		leg.BindingConfidence = maxFloat(leg.BindingConfidence, binding.BindingConfidence)
		leg.Stress = stressEval.EvaluateLeg(*leg)
		if leg.Stress != nil {
			leg.Stress.DominantFactors = uniqueStrings(append(leg.Stress.DominantFactors, finding.Mechanism, finding.StressorType))
		}
	}

	for idx := range parsed.Edges {
		edge := &parsed.Edges[idx]
		segmentIndex := -1
		if edge.SegmentIndex != nil {
			segmentIndex = *edge.SegmentIndex
		}
		match := false
		if finding.SupplyLinkRef != "" && strings.TrimSpace(edge.SupplyLinkCanonicalPath) == strings.TrimSpace(finding.SupplyLinkRef) {
			match = true
		}
		if finding.ArtifactKey != "" && strings.TrimPrefix(strings.TrimSpace(copyArtifact.CanonicalKey), "/") == strings.TrimPrefix(strings.TrimSpace(finding.ArtifactKey), "/") && finding.TargetType == "path" {
			match = true
		}
		if _, ok := touchedLegs[segmentIndex]; ok {
			match = true
		}
		if finding.ProductRef != "" && strings.TrimSpace(edge.ProductRef) == strings.TrimSpace(finding.ProductRef) {
			match = true
		}
		if !match {
			continue
		}
		touched = true
		if edge.Metrics == nil {
			edge.Metrics = map[string]horizonmodels.MetricValue{}
		}
		if edge.Qualifiers == nil {
			edge.Qualifiers = map[string]interface{}{}
		}
		for _, obs := range finding.MetricObservations {
			key := strings.TrimSpace(obs.MetricKey)
			if key == "" {
				continue
			}
			edge.Metrics[key] = makeMetricValueFromObservation(obs, evidenceIDs, now)
		}
		mergeRiskQualifierState(edge.Qualifiers, finding, edge.Metrics)
		for key, raw := range finding.QualifierDeltas {
			edge.Qualifiers[key] = raw
		}
		if segmentIndex >= 0 && segmentIndex < len(parsed.Legs) {
			leg := parsed.Legs[segmentIndex]
			edge.Qualifiers[horizonmodels.QualifierRouteMode] = leg.Mode
			edge.Qualifiers[horizonmodels.QualifierGeoRefs] = append([]string(nil), leg.GeoRefs...)
			edge.Qualifiers[horizonmodels.QualifierFlowRefs] = append([]string(nil), leg.FlowRefs...)
			edge.Qualifiers[horizonmodels.QualifierAlternateFlowRefs] = append([]string(nil), leg.AlternateFlowRefs...)
			edge.Qualifiers[horizonmodels.QualifierZoneRefs] = append([]string(nil), leg.ZoneRefs...)
			edge.Qualifiers[horizonmodels.QualifierAffectedZoneRefs] = append([]string(nil), leg.AffectedZoneRefs...)
			edge.Qualifiers[horizonmodels.QualifierSpatialSemantics] = append([]string(nil), leg.SpatialSemantics...)
			edge.Qualifiers[horizonmodels.QualifierBindingConfidence] = leg.BindingConfidence
			if leg.Stress != nil {
				edge.Qualifiers[horizonmodels.QualifierPathStressScore] = leg.Stress.StressScore
				edge.Qualifiers[horizonmodels.QualifierPathFragility] = leg.Stress.Fragility
				edge.Qualifiers[horizonmodels.QualifierPathRerouteability] = leg.Stress.Rerouteability
				edge.Qualifiers[horizonmodels.QualifierPathZonePressure] = leg.Stress.ZonePressure
				edge.Qualifiers[horizonmodels.QualifierPathAffectedPressure] = leg.Stress.AffectedZonePressure
				edge.Qualifiers[horizonmodels.QualifierRecoveryLatencyHours] = leg.Stress.RecoveryLatencyHrs
			}
		}
		if strings.TrimSpace(edge.SupplyLinkID) != "" {
			touchedEdgeIDs = append(touchedEdgeIDs, strings.TrimSpace(edge.SupplyLinkID))
		}
	}

	if !touched {
		return nil, nil, nil
	}

	parsed.Stress = stressEval.EvaluatePath(parsed.Legs)
	if parsed.Notes == nil {
		parsed.Notes = map[string]interface{}{}
	}
	parsed.Notes["identifyStressesUpdatedAt"] = now.Format(time.RFC3339Nano)
	parsed.Notes["identifyStressesMechanisms"] = uniqueStrings(append(interfaceToStrings(parsed.Notes["identifyStressesMechanisms"]), finding.Mechanism, finding.StressorType))
	copyArtifact.Payload = horizonmodels.BuildSupplyChainPathPayloadDetailed(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Family, parsed.Notes)
	horizonmodels.PopulateMaterializedArtifactIndexes(&copyArtifact)
	summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
	copyArtifact.LegCount = summary.LegCount
	copyArtifact.BindingScore = summary.BindingConfidence
	copyArtifact.PathStressScore = summary.PathStressScore
	copyArtifact.BottleneckLeg = summary.BottleneckLegIndex

	routeObsIDs, supplyObsIDs := c.persistObservationArtifacts(ctx, tenantID, &copyArtifact, finding, evidenceIDs, now)
	observationIDs = append(observationIDs, routeObsIDs...)
	observationIDs = append(observationIDs, supplyObsIDs...)
	return &copyArtifact, uniqueStrings(touchedEdgeIDs), uniqueStrings(observationIDs)
}

func (c *IdentifyStressesCanvas) persistObservationArtifacts(ctx context.Context, tenantID string, artifact *horizonmodels.MaterializedArtifact, finding StressFinding, evidenceIDs []string, now time.Time) ([]string, []string) {
	if artifact == nil {
		return nil, nil
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return nil, nil
	}
	routeObsIDs := []string{}
	supplyObsIDs := []string{}
	if c.routeObsRepo != nil {
		if parsed.Stress != nil {
			pathObs := &horizonmodels.RouteObservation{
				TenantID:        tenantID,
				PathArtifactKey: strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/"),
				ProductRef:      strings.TrimSpace(artifact.ProductAxis),
				Metrics: map[string]horizonmodels.MetricValue{
					"route_fragility":        makeDerivedMetric(parsed.Stress.Fragility, now, evidenceIDs),
					"route_rerouteability":   makeDerivedMetric(parsed.Stress.Rerouteability, now, evidenceIDs),
					"path_zone_pressure":     makeDerivedMetric(parsed.Stress.ZonePressure, now, evidenceIDs),
					"path_affected_pressure": makeDerivedMetric(parsed.Stress.AffectedZonePressure, now, evidenceIDs),
					"recovery_latency_hours": makeDerivedMetric(parsed.Stress.RecoveryLatencyHrs, now, evidenceIDs),
					"path_stress_score":      makeDerivedMetric(parsed.Stress.PathScore, now, evidenceIDs),
					"binding_confidence":     makeDerivedMetric(artifact.BindingScore, now, evidenceIDs),
				},
				Qualifiers: map[string]interface{}{
					"geoRefs":          append([]string(nil), artifact.GeoRefs...),
					"flowRefs":         append([]string(nil), artifact.FlowRefs...),
					"zoneRefs":         append([]string(nil), artifact.ZoneRefs...),
					"affectedZoneRefs": append([]string(nil), artifact.AffectedZoneRefs...),
					"bottleneck":       parsed.Stress.BottleneckLegIndex,
					"dominantFactors":  append([]string(nil), parsed.Stress.DominantFactors...),
				},
				SourceIDs:  append([]string(nil), evidenceIDs...),
				ObservedAt: now,
			}
			if id, err := c.routeObsRepo.Create(ctx, pathObs); err == nil && id != "" {
				routeObsIDs = append(routeObsIDs, id)
			}
		}
		for _, leg := range parsed.Legs {
			if leg.Stress == nil {
				continue
			}
			legIndex := leg.Index
			legObs := &horizonmodels.RouteObservation{
				TenantID:                tenantID,
				SupplyLinkCanonicalPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
				PathArtifactKey:         strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/"),
				LegIndex:                &legIndex,
				ProductRef:              strings.TrimSpace(leg.ProductRef),
				TransportViaRefs:        append([]string(nil), leg.TransportViaRefs...),
				Metrics: map[string]horizonmodels.MetricValue{
					"binding_confidence":     makeDerivedMetric(leg.BindingConfidence, now, evidenceIDs),
					"segment_confidence":     makeDerivedMetric(leg.SegmentConfidence, now, evidenceIDs),
					"route_fragility":        makeDerivedMetric(leg.Stress.Fragility, now, evidenceIDs),
					"route_rerouteability":   makeDerivedMetric(leg.Stress.Rerouteability, now, evidenceIDs),
					"path_zone_pressure":     makeDerivedMetric(leg.Stress.ZonePressure, now, evidenceIDs),
					"path_affected_pressure": makeDerivedMetric(leg.Stress.AffectedZonePressure, now, evidenceIDs),
					"recovery_latency_hours": makeDerivedMetric(leg.Stress.RecoveryLatencyHrs, now, evidenceIDs),
					"path_stress_score":      makeDerivedMetric(leg.Stress.StressScore, now, evidenceIDs),
				},
				Qualifiers: map[string]interface{}{
					"mode":              leg.Mode,
					"geoRefs":           append([]string(nil), leg.GeoRefs...),
					"flowRefs":          append([]string(nil), leg.FlowRefs...),
					"alternateFlowRefs": append([]string(nil), leg.AlternateFlowRefs...),
					"zoneRefs":          append([]string(nil), leg.ZoneRefs...),
					"affectedZoneRefs":  append([]string(nil), leg.AffectedZoneRefs...),
					"spatialSemantics":  append([]string(nil), leg.SpatialSemantics...),
					"vesselType":        leg.VesselType,
					"dominantFactors":   append([]string(nil), leg.Stress.DominantFactors...),
				},
				SourceIDs:  append([]string(nil), evidenceIDs...),
				ObservedAt: now,
			}
			if id, err := c.routeObsRepo.Create(ctx, legObs); err == nil && id != "" {
				routeObsIDs = append(routeObsIDs, id)
			}
		}
	}

	if c.supplyLinkObsRepo != nil || c.capabilityObsRepo != nil || c.capabilityProfilesRepo != nil || c.derivedRiskRepo != nil {
		for _, edge := range parsed.Edges {
			if strings.TrimSpace(edge.SupplyLinkCanonicalPath) == "" {
				continue
			}
			if c.supplyLinkObsRepo != nil {
				obs := &horizonmodels.SupplyLinkObservation{
					TenantID:                tenantID,
					SupplyLinkCanonicalPath: strings.TrimSpace(edge.SupplyLinkCanonicalPath),
					ProductRef:              strings.TrimSpace(firstNonEmptyString(edge.ProductRef, finding.ProductRef)),
					Metrics:                 cloneMetricMap(edge.Metrics),
					Qualifiers:              cloneInterfaceMap(edge.Qualifiers),
					SourceIDs:               append([]string(nil), evidenceIDs...),
					ObservedAt:              now,
				}
				if id, err := c.supplyLinkObsRepo.Create(ctx, obs); err == nil && id != "" {
					supplyObsIDs = append(supplyObsIDs, id)
				}
			}
			productRef := strings.TrimSpace(firstNonEmptyString(edge.ProductRef, finding.ProductRef))
			supplierRef, _ := parsedEdgeEndpointRefs(parsed, edge)
			if c.capabilityObsRepo != nil && supplierRef != "" && productRef != "" {
				obs := &horizonmodels.CapabilityObservation{
					TenantID:   tenantID,
					CarrierRef: supplierRef,
					ProductRef: productRef,
					Metrics:    selectMetricKeys(cloneMetricMap(edge.Metrics), identifyCapabilityMetricKeys),
					Qualifiers: cloneInterfaceMap(edge.Qualifiers),
					SourceIDs:  append([]string(nil), evidenceIDs...),
					ObservedAt: now,
				}
				if id, err := c.capabilityObsRepo.Create(ctx, obs); err == nil && id != "" {
					supplyObsIDs = append(supplyObsIDs, id)
				}
			}
			if c.capabilityProfilesRepo != nil && supplierRef != "" && productRef != "" {
				profile, _ := c.capabilityProfilesRepo.Get(ctx, supplierRef, productRef)
				if profile == nil {
					profile = &horizonmodels.CapabilityProfile{TenantID: tenantID, CarrierRef: supplierRef, ProductRef: productRef}
				}
				if profile.Metrics == nil {
					profile.Metrics = map[string]horizonmodels.MetricValue{}
				}
				for key, metric := range selectMetricKeys(cloneMetricMap(edge.Metrics), identifyCapabilityMetricKeys) {
					profile.Metrics[key] = metric
				}
				if profile.Qualifiers == nil {
					profile.Qualifiers = map[string]interface{}{}
				}
				for key, value := range cloneInterfaceMap(edge.Qualifiers) {
					profile.Qualifiers[key] = value
				}
				profile.CoverageStatus = computeCoverageStatus(profile.Metrics)
				profile.LastObservedAt = now
				profile.LastRolledUpAt = now
				_ = c.capabilityProfilesRepo.Upsert(ctx, profile)
			}
			if c.derivedRiskRepo != nil {
				scopeRef := strings.TrimSpace(edge.SupplyLinkCanonicalPath)
				if scopeRef == "" {
					scopeRef = strings.TrimSpace(artifact.CanonicalKey)
				}
				profile := &horizonmodels.DerivedRiskProfile{
					TenantID:        tenantID,
					ScopeRef:        scopeRef,
					ProductRef:      productRef,
					ProfileType:     "supply_link_risk",
					Metrics:         selectMetricKeys(cloneMetricMap(edge.Metrics), identifyPrimaryScalarMetrics),
					Qualifiers:      cloneInterfaceMap(edge.Qualifiers),
					EvidenceFactIDs: append([]string(nil), evidenceIDs...),
				}
				_ = c.derivedRiskRepo.Upsert(ctx, profile)
			}
		}
	}
	return uniqueStrings(routeObsIDs), uniqueStrings(supplyObsIDs)
}

func (c *IdentifyStressesCanvas) syncLensScopeArtifact(ctx context.Context, lens *horizonmodels.Lens, artifact *horizonmodels.MaterializedArtifact) error {
	if c == nil || c.lensesRepo == nil || lens == nil || artifact == nil {
		return nil
	}
	ref := horizonmodels.ScopeArtifactRef{
		Kind:           string(artifact.Kind),
		DisplayName:    artifact.DisplayName,
		AnchorSupplier: artifact.AnchorSupplier,
		AnchorCustomer: artifact.AnchorCustomer,
		ProductAxis:    artifact.ProductAxis,
		NodeRefs:       append([]string(nil), artifact.NodeRefs...),
		Status:         artifact.PathStatus,
	}
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
		summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
		ref.GeoRefs = summary.GeoRefs
		ref.FlowRefs = summary.FlowRefs
		ref.ZoneRefs = summary.ZoneRefs
		ref.AffectedZoneRefs = summary.AffectedZoneRefs
		ref.SpatialSemantics = summary.SpatialSemantics
		ref.LegCount = summary.LegCount
		ref.BindingConfidence = summary.BindingConfidence
		ref.PathStressScore = summary.PathStressScore
		ref.BottleneckLegIndex = summary.BottleneckLegIndex
		ref.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
	}
	return c.lensesRepo.UpsertScopeArtifact(ctx, strings.TrimSpace(lens.ID), strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/"), ref)
}

func (c *IdentifyStressesCanvas) findTouchedArtifacts(finding StressFinding, artifactByKey map[string]*horizonmodels.MaterializedArtifact) []*horizonmodels.MaterializedArtifact {
	if len(artifactByKey) == 0 {
		return nil
	}
	out := []*horizonmodels.MaterializedArtifact{}
	seen := map[string]struct{}{}
	add := func(artifact *horizonmodels.MaterializedArtifact) {
		if artifact == nil {
			return
		}
		key := strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
		if key == "" {
			key = strings.TrimSpace(artifact.ID)
		}
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, artifact)
	}
	if finding.ArtifactKey != "" {
		if artifact := artifactByKey[strings.TrimPrefix(strings.TrimSpace(finding.ArtifactKey), "/")]; artifact != nil {
			add(artifact)
		}
	}
	for _, artifact := range artifactByKey {
		if artifact == nil {
			continue
		}
		if finding.SupplyLinkRef != "" && containsString(collectSupplyLinkRefsFromArtifact(artifact), finding.SupplyLinkRef) {
			add(artifact)
			continue
		}
		if finding.ProductRef != "" && containsString(collectArtifactProductRefs(artifact), finding.ProductRef) {
			add(artifact)
			continue
		}
		for _, targetRef := range finding.TargetRefs {
			if containsString(artifact.NodeRefs, targetRef) {
				add(artifact)
				break
			}
		}
	}
	return out
}

func (c *IdentifyStressesCanvas) ensureFragmentByPath(ctx context.Context, canonicalPath string, cache map[string]*horizonmodels.HorizonFragment) *horizonmodels.HorizonFragment {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return nil
	}
	if frag := cache[canonicalPath]; frag != nil {
		return frag
	}
	if c.fragmentsRepo == nil {
		return nil
	}
	frag, err := c.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
	if err != nil || frag == nil {
		return nil
	}
	cache[canonicalPath] = frag
	return frag
}

func (c *IdentifyStressesCanvas) buildGroundedSynthesisPrompt(lens *horizonmodels.Lens, scope *identifyStressScope) string {
	var b strings.Builder
	b.WriteString("You are identifying current supply-chain stressors for a Horizon lens.\n")
	b.WriteString("Return a compact research dossier grounded in current web search evidence.\n")
	b.WriteString("Do not generalize beyond the scope. Prefer stressors active now or very recently active.\n")
	b.WriteString("Write the dossier in a machine-friendly plain-text format so a second model can convert it into strict JSON without ambiguity.\n")
	b.WriteString("Do not write markdown tables. Do not write code fences. Do not write prose essays.\n\n")
	b.WriteString("Lens:\n")
	b.WriteString(fmt.Sprintf("- lensId: %s\n- projectId: %s\n", strings.TrimSpace(lens.ID), strings.TrimSpace(lens.ProjectID)))
	if scope.SelectedCanonicalPath != "" {
		b.WriteString("- selectedCanonicalPath: " + scope.SelectedCanonicalPath + "\n")
	}
	b.WriteString("\nAllowed node refs:\n")
	for _, frag := range scope.NodeFragments {
		if frag == nil {
			continue
		}
		label := fragmentDisplayName(frag)
		b.WriteString(fmt.Sprintf("- %s | type=%s | label=%s | supply_chain_criticality=%.3f\n", strings.TrimSpace(frag.CanonicalPath), frag.Type, label, metricValue(frag.State, "supply_chain_criticality")))
	}
	b.WriteString("\nAllowed supply path artifacts:\n")
	for _, artifact := range scope.Artifacts {
		if artifact == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("- artifactKey=%s | display=%s | supplier=%s | customer=%s | product=%s | pathStress=%.3f\n",
			strings.TrimSpace(artifact.CanonicalKey),
			strings.TrimSpace(artifact.DisplayName),
			strings.TrimSpace(artifact.AnchorSupplier),
			strings.TrimSpace(artifact.AnchorCustomer),
			strings.TrimSpace(artifact.ProductAxis),
			artifact.PathStressScore,
		))
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			b.WriteString(fmt.Sprintf("  - leg=%d | supplyLink=%s | supplier=%s | customer=%s | product=%s | via=%s | geo=%s | flow=%s | zone=%s | affected=%s\n",
				leg.Index,
				strings.TrimSpace(leg.SupplyLinkCanonicalPath),
				strings.TrimSpace(leg.SupplierRef),
				strings.TrimSpace(leg.CustomerRef),
				strings.TrimSpace(leg.ProductRef),
				strings.Join(leg.TransportViaRefs, ","),
				strings.Join(leg.GeoRefs, ","),
				strings.Join(leg.FlowRefs, ","),
				strings.Join(leg.ZoneRefs, ","),
				strings.Join(leg.AffectedZoneRefs, ","),
			))
		}
	}
	if len(scope.SupplyLinkRefs) > 0 {
		b.WriteString("\nAllowed explicit supply links:\n")
		for _, ref := range scope.SupplyLinkRefs {
			b.WriteString("- " + ref + "\n")
		}
	}
	if len(scope.VisibleRelations) > 0 {
		b.WriteString("\nVisible graph relations on the map:\n")
		for _, relation := range scope.VisibleRelations {
			b.WriteString(fmt.Sprintf("- source=%s | target=%s | type=%s | family=%s | role=%s | criticality=%.3f | edgeId=%s\n",
				strings.TrimSpace(relation.SourceCanonicalPath),
				strings.TrimSpace(relation.TargetCanonicalPath),
				strings.TrimSpace(relation.RelationType),
				strings.TrimSpace(relation.RelationFamily),
				strings.TrimSpace(relation.RelationRole),
				relation.Criticality,
				strings.TrimSpace(relation.EdgeID),
			))
			if relation.BusinessMeaning != "" {
				b.WriteString("  businessMeaning: " + relation.BusinessMeaning + "\n")
			}
			if relation.WhyRelevant != "" {
				b.WriteString("  whyRelevant: " + relation.WhyRelevant + "\n")
			}
		}
	}
	b.WriteString("\nResearch task:\n")
	b.WriteString("- Identify sanctions, export-controls, customs friction, port/border congestion, route disruption, carrier/service interruption, supplier outage, capacity shock, inventory shortfall, regulatory block, security conflict, natural hazard, labor action, insurance or finance restrictions, cyber operational disruption.\n")
	b.WriteString("- Focus on direct impacts to listed products, shipments, supply links, paths, legs, orgs, facilities, and affected zones.\n")
	b.WriteString("- Prefer findings that bind to an allowed artifactKey, supplyLinkRef, or path leg whenever the evidence can reasonably be mapped to a listed shipment or route.\n")
	b.WriteString("- If the evidence names only a node or facility, still explain which listed products or deliveries are affected and attach the matching artifactKey or supplyLinkRef when possible.\n")
	b.WriteString("- Avoid generic country-level risk blur unless it directly explains a listed product flow, plant, exporter, importer, port, pipeline, or shipment path.\n")
	b.WriteString("- For each stressor mention the best matching target refs from scope, current severity, why it matters operationally, affected zones if any, and concrete metrics or booleans implied by evidence.\n")
	b.WriteString("- Include source URLs inline in the dossier.\n")
	b.WriteString("- Prefer concise blocks per target.\n")
	b.WriteString("\nRequired dossier format for each finding:\n")
	b.WriteString("FINDING <number>\n")
	b.WriteString("targetType: <node|supply_link|path|path_leg|product_on_link>\n")
	b.WriteString("targetRefs: <comma-separated canonical refs from scope>\n")
	b.WriteString("artifactKey: <artifact key from scope or empty>\n")
	b.WriteString("legIndex: <integer or empty>\n")
	b.WriteString("supplyLinkRef: <canonical supply-link ref or empty>\n")
	b.WriteString("productRef: <canonical product ref or empty>\n")
	b.WriteString("stressorType: <allowed taxonomy value>\n")
	b.WriteString("primaryMechanism: <allowed mechanism value>\n")
	b.WriteString("severity: <low|medium|high|critical>\n")
	b.WriteString("confidence: <number 0..1>\n")
	b.WriteString("effectiveAt: <RFC3339 date-time, YYYY-MM-DD, or empty>\n")
	b.WriteString("timeHorizon: <immediate|near_term|medium_term|long_term|unknown>\n")
	b.WriteString("geoRefs: <comma-separated canonical refs>\n")
	b.WriteString("flowRefs: <comma-separated canonical refs>\n")
	b.WriteString("zoneRefs: <comma-separated canonical refs>\n")
	b.WriteString("affectedZoneRefs: <comma-separated canonical refs>\n")
	b.WriteString("metricImpacts: <semicolon-separated items metricKey=value unit|scope|confidence>\n")
	b.WriteString("qualifierDeltas: <semicolon-separated key=value items>\n")
	b.WriteString("summary: <1-3 sentences>\n")
	b.WriteString("evidence: <one URL per line prefixed by '- '>\n")
	b.WriteString("\nRules:\n")
	b.WriteString("- Use only refs and artifact keys that appear in the supplied scope.\n")
	b.WriteString("- Do not invent targets outside the scope.\n")
	b.WriteString("- Prefer business-meaningful findings: product shortage, route blockage, export restriction, plant outage, customs friction, carrier interruption, or sanctions impact on a real shipment or supply dependency.\n")
	b.WriteString("- When several node-only facts roll up into the same product flow, emit the path/path_leg/supply_link impact instead of duplicating weak node-only findings.\n")
	b.WriteString("- confidence must always be numeric, never words, never quoted.\n")
	b.WriteString("- metric values should be explicit numbers or booleans when possible.\n")
	b.WriteString("- If the evidence is weak, lower confidence instead of adding prose hedging.\n")
	return b.String()
}

func (c *IdentifyStressesCanvas) buildNormalizationPrompt(lens *horizonmodels.Lens, scope *identifyStressScope, grounded string) string {
	var b strings.Builder
	b.WriteString("Normalize the grounded research dossier into one strict JSON object.\n")
	b.WriteString("Return JSON only. Do not return markdown. Do not return code fences. Do not return commentary. Do not return any text before or after the JSON object.\n")
	b.WriteString("The response MUST be valid JSON starting with '{' and ending with '}'.\n")
	b.WriteString("The JSON shape MUST be exactly {\"findings\":[...]}. If there are no valid findings, return {\"findings\":[]}.\n")
	b.WriteString("Every finding must stay inside the provided scope.\n")
	b.WriteString("Allowed targetType values: node, supply_link, path, path_leg, product_on_link.\n")
	b.WriteString("Allowed stressorType values: sanctions, export_controls, customs_restrictions, port_or_border_congestion, route_disruption, carrier_or_service_interruption, supplier_outage, capacity_drop, inventory_shortfall, regulatory_block, security_conflict, natural_hazard, labor_action, insurance_finance_restriction, cyber_operational_disruption.\n")
	b.WriteString("Allowed mechanism values: control_exposure, customs_friction, route_fragility, inventory_vulnerability, service_dependency, sole_source_lock_in, low_substitutability.\n")
	b.WriteString("Allowed severity values: low, medium, high, critical.\n")
	b.WriteString("For each finding include these fields exactly: id, targetType, targetRefs, artifactKey, legIndex, supplyLinkRef, productRef, stressorType, mechanism, severity, confidence, effectiveAt, timeHorizon, geoRefs, flowRefs, zoneRefs, affectedZoneRefs, metricObservations, qualifierDeltas, evidence, summary, eventType, factType.\n")
	b.WriteString("metricObservations items shape: {\"metricKey\":\"string\",\"value\":number|boolean,\"unit\":\"string\",\"confidence\":number,\"scope\":\"string\",\"description\":\"string\"}\n")
	b.WriteString("evidence items shape: {\"url\":\"string\",\"title\":\"string\",\"snippet\":\"string\",\"domain\":\"string\"}\n")
	b.WriteString("qualifierDeltas must be a JSON object with string keys and scalar JSON values.\n")
	b.WriteString("Use [] for empty arrays, {} for empty qualifierDeltas, and \"\" for empty optional strings. Do not use null.\n")
	b.WriteString("confidence must always be a JSON number between 0 and 1. Never return it as a string.\n")
	b.WriteString("legIndex must be an integer or 0 when unknown for a path_leg finding; use 0 for non-leg findings.\n")
	b.WriteString("If a value is unknown, keep the field but use an empty string, empty array, empty object, or 0 as appropriate.\n")
	b.WriteString("Do not invent refs, artifact keys, metrics, or evidence that are not supported by the dossier.\n")
	b.WriteString("Do not merge distinct stressors into one finding if they affect different targets or mechanisms.\n")
	b.WriteString("Prefer findings that point to a real shipment, supply link, path, or path_leg over generic node-only findings when both are possible from the dossier.\n")
	b.WriteString("If a node-only stressor clearly affects one or more listed supply paths, copy the matching artifactKey and supplyLinkRef into the finding.\n")
	b.WriteString("Use concise summaries only.\n")
	b.WriteString("\nExample output shape:\n")
	b.WriteString("{\"findings\":[{\"id\":\"stress_example\",\"targetType\":\"path_leg\",\"targetRefs\":[\"/org/example-supplier\"],\"artifactKey\":\"artifact/example\",\"legIndex\":2,\"supplyLinkRef\":\"/supply-link/example\",\"productRef\":\"/product/example\",\"stressorType\":\"route_disruption\",\"mechanism\":\"route_fragility\",\"severity\":\"high\",\"confidence\":0.84,\"effectiveAt\":\"2026-03-19\",\"timeHorizon\":\"immediate\",\"geoRefs\":[\"/geo/example-port\"],\"flowRefs\":[],\"zoneRefs\":[],\"affectedZoneRefs\":[],\"metricObservations\":[{\"metricKey\":\"path_stress_score\",\"value\":0.81,\"unit\":\"score\",\"confidence\":0.84,\"scope\":\"path_leg\",\"description\":\"Route disruption materially increases leg stress.\"}],\"qualifierDeltas\":{\"bottleneck\":true},\"evidence\":[{\"url\":\"https://example.com\",\"title\":\"Example\",\"snippet\":\"Short evidence snippet.\",\"domain\":\"example.com\"}],\"summary\":\"Brief summary.\",\"eventType\":\"route_disruption\",\"factType\":\"stress_finding\"}]}\n")
	b.WriteString("Only use artifactKey values from this list:\n")
	for _, key := range scope.ArtifactKeys {
		b.WriteString("- " + strings.TrimPrefix(strings.TrimSpace(key), "/") + "\n")
	}
	b.WriteString("Only use targetRefs or supplyLinkRef values from this scope:\n")
	for _, ref := range append(append([]string{}, scope.NodeRefs...), scope.SupplyLinkRefs...) {
		b.WriteString("- " + ref + "\n")
	}
	if len(scope.AffectedZoneRefs) > 0 {
		b.WriteString("Existing affected zones:\n")
		for _, ref := range scope.AffectedZoneRefs {
			b.WriteString("- " + ref + "\n")
		}
	}
	if len(scope.VisibleRelations) > 0 {
		b.WriteString("Visible graph relations to respect:\n")
		for _, relation := range scope.VisibleRelations {
			b.WriteString(fmt.Sprintf("- %s -> %s | type=%s | role=%s | edgeId=%s\n",
				strings.TrimSpace(relation.SourceCanonicalPath),
				strings.TrimSpace(relation.TargetCanonicalPath),
				strings.TrimSpace(relation.RelationType),
				strings.TrimSpace(relation.RelationRole),
				strings.TrimSpace(relation.EdgeID),
			))
		}
	}
	b.WriteString("\nGrounded dossier:\n")
	b.WriteString(grounded)
	return b.String()
}

func (c *IdentifyStressesCanvas) publish(ctx context.Context, eventType string, tenantID string, payload interface{}) error {
	if c == nil || c.publisher == nil {
		return nil
	}
	return c.publisher(ctx, eventType, firstNonEmptyString(tenantID, "global"), payload)
}

func normalizeVisibleRelations(in []IdentifyStressVisibleRelation) []IdentifyStressVisibleRelation {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]IdentifyStressVisibleRelation, 0, len(in))
	for _, raw := range in {
		rel := IdentifyStressVisibleRelation{
			EdgeID:              strings.TrimSpace(raw.EdgeID),
			SourceCanonicalPath: strings.TrimSpace(raw.SourceCanonicalPath),
			TargetCanonicalPath: strings.TrimSpace(raw.TargetCanonicalPath),
			RelationType:        strings.TrimSpace(strings.ToUpper(raw.RelationType)),
			RelationFamily:      strings.TrimSpace(strings.ToLower(raw.RelationFamily)),
			RelationRole:        strings.TrimSpace(strings.ToLower(raw.RelationRole)),
			BusinessMeaning:     strings.TrimSpace(raw.BusinessMeaning),
			WhyRelevant:         strings.TrimSpace(raw.WhyRelevant),
			Criticality:         raw.Criticality,
		}
		if rel.SourceCanonicalPath == "" && rel.TargetCanonicalPath == "" && rel.EdgeID == "" {
			continue
		}
		key := strings.Join([]string{
			rel.EdgeID,
			rel.SourceCanonicalPath,
			rel.TargetCanonicalPath,
			rel.RelationType,
			rel.RelationRole,
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, rel)
	}
	return out
}

func prioritizeVisibleRelations(in []IdentifyStressVisibleRelation, selected string, preferredNodeRefs []string, limit int) []IdentifyStressVisibleRelation {
	if len(in) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = defaultIdentifyStressesMaxRelations
	}
	preferredSet := make(map[string]struct{}, len(preferredNodeRefs)+1)
	for _, ref := range preferredNodeRefs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			preferredSet[ref] = struct{}{}
		}
	}
	selected = strings.TrimSpace(selected)
	if selected != "" {
		preferredSet[selected] = struct{}{}
	}
	type scoredRelation struct {
		relation IdentifyStressVisibleRelation
		score    float64
	}
	scored := make([]scoredRelation, 0, len(in))
	for _, rel := range in {
		score := 0.0
		source := strings.TrimSpace(rel.SourceCanonicalPath)
		target := strings.TrimSpace(rel.TargetCanonicalPath)
		relType := strings.ToUpper(strings.TrimSpace(rel.RelationType))
		family := strings.ToLower(strings.TrimSpace(rel.RelationFamily))
		role := strings.ToLower(strings.TrimSpace(rel.RelationRole))

		if selected != "" && (source == selected || target == selected) {
			score += 200
		}
		if _, ok := preferredSet[source]; ok {
			score += 50
		}
		if _, ok := preferredSet[target]; ok {
			score += 50
		}
		switch role {
		case "path_leg":
			score += 160
		case "direct":
			score += 80
		case "context":
			score += 45
		case "propagation":
			score += 15
		case "lens_edge":
			score += 10
		}
		switch family {
		case "supply_path":
			score += 120
		case "supply_chain":
			score += 90
		case "dependency":
			score += 55
		case "propagation":
			score += 10
		}
		switch relType {
		case "SUPPLY_PATH_LEG", "SUPPLIES_TO", "SUPPLY_LINK", "DELIVERS_TO":
			score += 100
		case "DEPENDS_ON", "RELIES_ON", "USES":
			score += 60
		case "LOCATED_IN", "HAS_MARKET_SEGMENT":
			score -= 80
		case "INFLUENCES":
			score -= 20
		}
		if rel.Criticality > 0 {
			score += rel.Criticality * 100
		}
		if strings.TrimSpace(rel.BusinessMeaning) != "" {
			score += 10
		}
		if strings.TrimSpace(rel.WhyRelevant) != "" {
			score += 10
		}
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredRelation{relation: rel, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		left := scored[i].relation
		right := scored[j].relation
		return strings.Join([]string{
			left.RelationRole,
			left.RelationType,
			left.SourceCanonicalPath,
			left.TargetCanonicalPath,
			left.EdgeID,
		}, "|") < strings.Join([]string{
			right.RelationRole,
			right.RelationType,
			right.SourceCanonicalPath,
			right.TargetCanonicalPath,
			right.EdgeID,
		}, "|")
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]IdentifyStressVisibleRelation, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.relation)
	}
	return out
}

func (c *IdentifyStressesCanvas) publishNodeUpsert(ctx context.Context, tenantID string, fragment *horizonmodels.HorizonFragment, operation string) error {
	if fragment == nil {
		return nil
	}
	payload := map[string]interface{}{
		"tenantId":        tenantID,
		"fragmentId":      strings.TrimSpace(fragment.ID),
		"canonicalPath":   strings.TrimSpace(fragment.CanonicalPath),
		"type":            string(fragment.Type),
		"label":           fragmentDisplayName(fragment),
		"status":          strings.TrimSpace(fragment.Status),
		"epistemicStatus": strings.TrimSpace(string(fragment.Metadata.EpistemicStatus)),
		"version":         fragment.Version,
		"operation":       operation,
		"updatedAt":       time.Now().UTC().Format(time.RFC3339Nano),
	}
	if fragment.Coordinates != nil {
		payload["coordinates"] = map[string]float64{"lat": fragment.Coordinates.Lat, "lon": fragment.Coordinates.Lon}
	}
	if fragment.RadiusKm > 0 {
		payload["radiusKm"] = fragment.RadiusKm
	}
	if fragment.Qualifiers != nil {
		if zoneType := stringQualifier(fragment.Qualifiers, "zoneType"); zoneType != "" {
			payload["zoneType"] = zoneType
		}
		if zoneSubType := stringQualifier(fragment.Qualifiers, "zoneSubType"); zoneSubType != "" {
			payload["zoneSubType"] = zoneSubType
		}
	}
	return c.publish(ctx, "horizon.graph.node.upserted", tenantID, payload)
}

func (c *IdentifyStressesCanvas) publishEdgeUpsert(ctx context.Context, tenantID string, edge *horizonmodels.DependencyEdge) error {
	if edge == nil {
		return nil
	}
	src := ""
	dst := ""
	if c.fragmentsRepo != nil {
		if frag, err := c.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.SourceFragmentID)); err == nil && frag != nil {
			src = strings.TrimSpace(frag.CanonicalPath)
		}
		if frag, err := c.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID)); err == nil && frag != nil {
			dst = strings.TrimSpace(frag.CanonicalPath)
		}
	}
	return c.publish(ctx, "horizon.graph.edge.upserted", tenantID, map[string]interface{}{
		"tenantId":            tenantID,
		"edgeId":              strings.TrimSpace(edge.ID),
		"sourceFragmentId":    strings.TrimSpace(edge.SourceFragmentID),
		"targetFragmentId":    strings.TrimSpace(edge.TargetFragmentID),
		"sourceCanonicalPath": src,
		"targetCanonicalPath": dst,
		"type":                string(edge.Type),
		"qualifiers":          cloneInterfaceMap(edge.Qualifiers),
		"properties":          edge.Properties,
	})
}

func artifactRealtimePayload(artifact *horizonmodels.MaterializedArtifact) map[string]interface{} {
	return map[string]interface{}{
		"id":               strings.TrimSpace(artifact.ID),
		"tenantId":         strings.TrimSpace(artifact.TenantID),
		"lensId":           strings.TrimSpace(artifact.LensID),
		"canonicalKey":     strings.TrimSpace(artifact.CanonicalKey),
		"displayName":      strings.TrimSpace(artifact.DisplayName),
		"anchorSupplier":   strings.TrimSpace(artifact.AnchorSupplier),
		"anchorCustomer":   strings.TrimSpace(artifact.AnchorCustomer),
		"productAxis":      strings.TrimSpace(artifact.ProductAxis),
		"nodeRefs":         append([]string(nil), artifact.NodeRefs...),
		"flowRefs":         append([]string(nil), artifact.FlowRefs...),
		"zoneRefs":         append([]string(nil), artifact.ZoneRefs...),
		"affectedZoneRefs": append([]string(nil), artifact.AffectedZoneRefs...),
		"pathStressScore":  artifact.PathStressScore,
		"legCount":         artifact.LegCount,
		"bindingScore":     artifact.BindingScore,
		"payload":          artifact.Payload,
		"riskOverlay":      artifact.RiskOverlay,
		"updatedAt":        time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func claimTitleForFinding(finding StressFinding) string {
	target := firstFindingCanonicalPath(finding)
	if target == "" {
		target = firstNonEmptyString(finding.SupplyLinkRef, finding.ArtifactKey, finding.ProductRef)
	}
	return strings.TrimSpace(fmt.Sprintf("%s pressure on %s", humanizeRefLabel(firstNonEmptyString(finding.StressorType, "stress")), humanizeRefLabel(target)))
}

func eventTitleForFinding(finding StressFinding) string {
	target := firstFindingCanonicalPath(finding)
	if target == "" {
		target = firstNonEmptyString(finding.SupplyLinkRef, finding.ArtifactKey, finding.ProductRef)
	}
	return strings.TrimSpace(fmt.Sprintf("%s affecting %s", humanizeRefLabel(firstNonEmptyString(finding.StressorType, "stress")), humanizeRefLabel(target)))
}

func stressFactTags(finding StressFinding, fingerprint, kind string) []string {
	return uniqueStrings([]string{
		"identify_stresses",
		"stress_kind:" + kind,
		"stress_fingerprint:" + fingerprint,
		"stress_mechanism:" + strings.TrimSpace(finding.Mechanism),
		"stress_stressor:" + strings.TrimSpace(finding.StressorType),
		"stress_target:" + strings.TrimSpace(firstFindingCanonicalPath(finding)),
	})
}

func claimCanonicalPaths(finding StressFinding) []string {
	paths := append([]string{}, finding.TargetRefs...)
	paths = append(paths, finding.SupplyLinkRef, finding.ProductRef)
	paths = append(paths, finding.AffectedZoneRefs...)
	return uniqueCanonicalRefs(paths)
}

func stressSpherePaths(finding StressFinding) []string {
	out := []string{"/sphere/logistics/supply_chain"}
	switch strings.TrimSpace(finding.Mechanism) {
	case "control_exposure":
		out = append(out, "/sphere/policy/sanctions")
	case "customs_friction":
		out = append(out, "/sphere/policy/customs")
	case "route_fragility":
		out = append(out, "/sphere/logistics/transport")
	case "inventory_vulnerability":
		out = append(out, "/sphere/economy/operations")
	case "service_dependency":
		out = append(out, "/sphere/technology/operations")
	}
	return uniqueCanonicalRefs(out)
}

func metricUpdatesFromFinding(finding StressFinding, now time.Time) []coremodels.FactMetricUpdate {
	out := make([]coremodels.FactMetricUpdate, 0, len(finding.MetricObservations))
	for _, obs := range finding.MetricObservations {
		key := strings.TrimSpace(obs.MetricKey)
		if key == "" {
			continue
		}
		out = append(out, coremodels.FactMetricUpdate{
			MetricName: key,
			Value:      obs.Value,
			Unit:       stringPtrIfNotEmpty(obs.Unit),
			Confidence: clamp01(firstNonZero(obs.Confidence, finding.Confidence, 0.64)),
			ObservedAt: &now,
		})
	}
	return out
}

func evidenceToSourceRefs(finding StressFinding, grounded []StressEvidence) []coremodels.SourceRef {
	sources := make([]coremodels.SourceRef, 0, len(finding.Evidence)+len(grounded))
	for _, item := range append(append([]StressEvidence{}, finding.Evidence...), grounded...) {
		urlValue := strings.TrimSpace(item.URL)
		if urlValue == "" {
			continue
		}
		sources = append(sources, coremodels.SourceRef{SourceID: urlValue, SourceType: coremodels.SourceTypeLLMSearchGrounding})
	}
	return dedupeSourceRefs(sources)
}

func dedupeSourceRefs(in []coremodels.SourceRef) []coremodels.SourceRef {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]coremodels.SourceRef, 0, len(in))
	for _, item := range in {
		key := strings.TrimSpace(item.SourceType) + "|" + strings.TrimSpace(item.SourceID)
		if key == "|" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func mergeRiskQualifierState(qualifiers map[string]interface{}, finding StressFinding, metrics map[string]horizonmodels.MetricValue) {
	if qualifiers == nil {
		return
	}
	mechanisms := uniqueStrings(append(interfaceToStrings(qualifiers["riskMechanisms"]), strings.TrimSpace(finding.Mechanism)))
	scores := cloneFloatMap(qualifiers["riskMechanismScores"])
	if scores == nil {
		scores = map[string]float64{}
	}
	score := findingPriorityScore(finding)
	if strings.TrimSpace(finding.Mechanism) != "" {
		if score > scores[finding.Mechanism] {
			scores[finding.Mechanism] = score
		}
	}
	summaries := cloneStringMap(qualifiers["riskMechanismSummaries"])
	if summaries == nil {
		summaries = map[string]string{}
	}
	if strings.TrimSpace(finding.Mechanism) != "" && strings.TrimSpace(finding.Summary) != "" {
		summaries[finding.Mechanism] = strings.TrimSpace(finding.Summary)
	}
	flags := uniqueStrings(append(interfaceToStrings(qualifiers["riskFlags"]), identifyFlagsForFinding(finding)...))
	primary := stringQualifier(qualifiers, "primaryRiskMechanism")
	if primary == "" || score > scores[primary] {
		primary = strings.TrimSpace(finding.Mechanism)
	}
	qualifiers["riskMechanisms"] = mechanisms
	qualifiers["riskMechanismScores"] = scores
	qualifiers["riskMechanismSummaries"] = summaries
	qualifiers["riskFlags"] = flags
	qualifiers["primaryRiskMechanism"] = primary
	qualifiers["primaryRiskLabel"] = humanizeRefLabel(primary)
	qualifiers["criticalSummary"] = strings.TrimSpace(finding.Summary)
	coverage := computeCoverageStatus(metrics)
	qualifiers["riskCoverageStatus"] = coverage
	qualifiers["riskGateStatus"] = computeGateStatus(scores, coverage)
	for _, obs := range finding.MetricObservations {
		key := strings.TrimSpace(obs.MetricKey)
		if key == "" {
			continue
		}
		qualifiers["primaryCriticalMetricKey"] = key
		qualifiers["primaryCriticalMetricLabel"] = humanizeRefLabel(key)
		qualifiers["primaryCriticalMetricValue"] = obs.Value
		if unit := strings.TrimSpace(obs.Unit); unit != "" {
			qualifiers["primaryCriticalMetricUnit"] = unit
		}
		break
	}
	for key := range identifyControlQualifierKeys {
		if value, ok := finding.QualifierDeltas[key]; ok {
			qualifiers[key] = value
		}
	}
	if routeFragility, ok := metrics["route_fragility"]; ok {
		qualifiers["routeFragility"] = routeFragility.Value
	}
	if routeRerouteability, ok := metrics["route_rerouteability"]; ok {
		qualifiers["routeRerouteability"] = routeRerouteability.Value
	}
}

func computeCoverageStatus(metrics map[string]horizonmodels.MetricValue) string {
	if len(metrics) == 0 {
		return "empty"
	}
	present := 0
	for _, key := range identifyCoverageRequiredMetricKeys {
		if metric, ok := metrics[key]; ok && (metric.Value != 0 || metric.Confidence > 0) {
			present++
		}
	}
	switch {
	case present >= 5:
		return "rich"
	case present >= 3:
		return "partial"
	case present >= 1:
		return "thin"
	default:
		return "empty"
	}
}

func computeGateStatus(scores map[string]float64, coverage string) string {
	maxScore := 0.0
	for _, value := range scores {
		if value > maxScore {
			maxScore = value
		}
	}
	switch {
	case maxScore >= 0.82 && (coverage == "rich" || coverage == "partial"):
		return "corroborated"
	case maxScore >= 0.55:
		return "sufficient"
	default:
		return "best_effort"
	}
}

func inferSpatialSemanticsFromFinding(finding StressFinding) []string {
	out := []string{}
	switch strings.TrimSpace(finding.Mechanism) {
	case "route_fragility":
		out = append(out, "chokepoint_exposed")
	case "customs_friction":
		out = append(out, "border_control_exposed")
	case "control_exposure":
		out = append(out, "regulatory_exposed")
	}
	if len(finding.AffectedZoneRefs) > 0 {
		out = append(out, "affected_zone_overlap")
	}
	return uniqueStrings(out)
}

func identifyFlagsForFinding(finding StressFinding) []string {
	flags := []string{}
	if strings.TrimSpace(finding.Mechanism) != "" {
		flags = append(flags, strings.TrimSpace(finding.Mechanism))
	}
	for key := range identifyControlQualifierKeys {
		if booleanQualifier(finding.QualifierDeltas, key) {
			flags = append(flags, key)
		}
	}
	if finding.TargetType == "path_leg" {
		flags = append(flags, "route_bottleneck")
	}
	return uniqueStrings(flags)
}

func collectSupplyLinkRefsFromArtifact(artifact *horizonmodels.MaterializedArtifact) []string {
	if artifact == nil {
		return nil
	}
	out := make([]string, 0, 8)
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return nil
	}
	for _, leg := range parsed.Legs {
		if strings.TrimSpace(leg.SupplyLinkCanonicalPath) != "" {
			out = append(out, strings.TrimSpace(leg.SupplyLinkCanonicalPath))
		}
	}
	for _, edge := range parsed.Edges {
		if strings.TrimSpace(edge.SupplyLinkCanonicalPath) != "" {
			out = append(out, strings.TrimSpace(edge.SupplyLinkCanonicalPath))
		}
	}
	return uniqueCanonicalRefs(out)
}

func artifactContainsLegEndpoints(artifact *horizonmodels.MaterializedArtifact, sourceRef, targetRef string) bool {
	if artifact == nil {
		return false
	}
	sourceRef = strings.TrimSpace(sourceRef)
	targetRef = strings.TrimSpace(targetRef)
	if sourceRef == "" || targetRef == "" {
		return false
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return false
	}
	for _, leg := range parsed.Legs {
		if strings.TrimSpace(leg.SupplierRef) == sourceRef && strings.TrimSpace(leg.CustomerRef) == targetRef {
			return true
		}
		if strings.TrimSpace(leg.SupplierRef) == targetRef && strings.TrimSpace(leg.CustomerRef) == sourceRef {
			return true
		}
	}
	return false
}

func collectArtifactNodeRefs(artifact *horizonmodels.MaterializedArtifact) []string {
	if artifact == nil {
		return nil
	}
	out := append([]string(nil), artifact.NodeRefs...)
	if strings.TrimSpace(artifact.AnchorSupplier) != "" {
		out = append(out, strings.TrimSpace(artifact.AnchorSupplier))
	}
	if strings.TrimSpace(artifact.AnchorCustomer) != "" {
		out = append(out, strings.TrimSpace(artifact.AnchorCustomer))
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return uniqueCanonicalRefs(out)
	}
	for _, node := range parsed.Nodes {
		out = append(out, strings.TrimSpace(node.Ref))
	}
	for _, leg := range parsed.Legs {
		out = append(out, strings.TrimSpace(leg.SupplierRef), strings.TrimSpace(leg.CustomerRef), strings.TrimSpace(leg.ProductRef))
	}
	return uniqueCanonicalRefs(out)
}

func collectArtifactProductRefs(artifact *horizonmodels.MaterializedArtifact) []string {
	if artifact == nil {
		return nil
	}
	out := []string{}
	if strings.TrimSpace(artifact.ProductAxis) != "" {
		out = append(out, strings.TrimSpace(artifact.ProductAxis))
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return uniqueCanonicalRefs(out)
	}
	for _, leg := range parsed.Legs {
		out = append(out, leg.ProductRef)
	}
	for _, edge := range parsed.Edges {
		out = append(out, edge.ProductRef)
	}
	return uniqueCanonicalRefs(out)
}

func setFromStrings(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}
	return set
}

func overlapCount(values []string, set map[string]struct{}) int {
	if len(values) == 0 || len(set) == 0 {
		return 0
	}
	count := 0
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		if _, ok := set[value]; ok {
			count++
		}
	}
	return count
}

func bestProductRefForArtifact(artifact *horizonmodels.MaterializedArtifact, finding StressFinding) string {
	productRefs := collectArtifactProductRefs(artifact)
	for _, targetRef := range finding.TargetRefs {
		if containsString(productRefs, targetRef) {
			return targetRef
		}
	}
	if finding.ProductRef != "" && containsString(productRefs, finding.ProductRef) {
		return finding.ProductRef
	}
	if strings.TrimSpace(artifact.ProductAxis) != "" {
		return strings.TrimSpace(artifact.ProductAxis)
	}
	if len(productRefs) > 0 {
		return productRefs[0]
	}
	return ""
}

func identifyScopeArtifactRef(artifact *horizonmodels.MaterializedArtifact) horizonmodels.ScopeArtifactRef {
	if artifact == nil {
		return horizonmodels.ScopeArtifactRef{}
	}
	horizonmodels.PopulateSupplyChainPathIndexes(artifact)
	ref := horizonmodels.ScopeArtifactRef{
		Kind:             string(artifact.Kind),
		DisplayName:      strings.TrimSpace(artifact.DisplayName),
		AnchorSupplier:   strings.TrimSpace(artifact.AnchorSupplier),
		AnchorCustomer:   strings.TrimSpace(artifact.AnchorCustomer),
		ProductAxis:      strings.TrimSpace(artifact.ProductAxis),
		NodeRefs:         uniqueCanonicalRefs(append([]string(nil), artifact.NodeRefs...)),
		Status:           strings.TrimSpace(artifact.PathStatus),
		GeoRefs:          uniqueCanonicalRefs(append([]string(nil), artifact.GeoRefs...)),
		FlowRefs:         uniqueCanonicalRefs(append([]string(nil), artifact.FlowRefs...)),
		ZoneRefs:         uniqueCanonicalRefs(append([]string(nil), artifact.ZoneRefs...)),
		AffectedZoneRefs: uniqueCanonicalRefs(append([]string(nil), artifact.AffectedZoneRefs...)),
	}
	if artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
		return ref
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return ref
	}
	summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
	ref.GeoRefs = summary.GeoRefs
	ref.FlowRefs = summary.FlowRefs
	ref.ZoneRefs = summary.ZoneRefs
	ref.AffectedZoneRefs = summary.AffectedZoneRefs
	ref.SpatialSemantics = summary.SpatialSemantics
	ref.LegCount = summary.LegCount
	ref.BindingConfidence = summary.BindingConfidence
	ref.PathStressScore = summary.PathStressScore
	ref.BottleneckLegIndex = summary.BottleneckLegIndex
	ref.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
	return ref
}

func isIdentifySyntheticArtifact(artifact *horizonmodels.MaterializedArtifact) bool {
	if artifact == nil {
		return false
	}
	if strings.TrimSpace(artifact.CreatedBy) == "identify_stresses:bootstrap_supply_link" || strings.TrimSpace(artifact.UpdatedBy) == "identify_stresses:bootstrap_supply_link" {
		return true
	}
	if raw, ok := artifact.Payload["notes"]; ok {
		if notes, ok := raw.(map[string]interface{}); ok {
			if flag, ok := notes["bootstrapSynthetic"].(bool); ok {
				return flag
			}
		}
	}
	return false
}

func buildSingleSupplyLinkArtifact(fragment *horizonmodels.HorizonFragment, tenantID, lensID string) *horizonmodels.MaterializedArtifact {
	if fragment == nil || fragment.SupplierRef == nil || fragment.CustomerRef == nil || fragment.ProductRef == nil {
		return nil
	}
	supplierRef := strings.TrimSpace(*fragment.SupplierRef)
	customerRef := strings.TrimSpace(*fragment.CustomerRef)
	productRef := strings.TrimSpace(*fragment.ProductRef)
	if supplierRef == "" || customerRef == "" || productRef == "" {
		return nil
	}
	confidence := metricValue(fragment.State, "confidence")
	if confidence <= 0 {
		confidence = 0.6
	}
	transportVia := uniqueCanonicalRefs(append([]string(nil), fragment.TransportViaRefs...))
	binder := pathbinding.NewEngine(nil, nil)
	binding := binder.BindSegment(context.Background(), pathbinding.SegmentInput{
		SupplierRef:       supplierRef,
		CustomerRef:       customerRef,
		ProductRef:        productRef,
		TransportViaRefs:  transportVia,
		SegmentConfidence: confidence,
	})
	leg := horizonmodels.SupplyChainPathLeg{
		Index:                   0,
		SupplierRef:             supplierRef,
		CustomerRef:             customerRef,
		ProductRef:              productRef,
		SupplyLinkCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
		SupplyLinkID:            strings.TrimSpace(fragment.ID),
		SupplyLinkTenantID:      firstNonEmptyString(strings.TrimSpace(fragment.TenantID), strings.TrimSpace(tenantID), "global"),
		TransportViaRefs:        transportVia,
		VesselType:              stringQualifier(fragment.Qualifiers, "vesselType"),
		Mode:                    binding.Mode,
		GeoRefs:                 append([]string(nil), binding.GeoRefs...),
		FlowRefs:                append([]string(nil), binding.FlowRefs...),
		AlternateFlowRefs:       append([]string(nil), binding.AlternateFlowRefs...),
		ZoneRefs:                append([]string(nil), binding.ZoneRefs...),
		AffectedZoneRefs:        append([]string(nil), binding.AffectedZoneRefs...),
		SpatialSemantics:        append([]string(nil), binding.SpatialSemantics...),
		BindingConfidence:       binding.BindingConfidence,
		SegmentConfidence:       confidence,
	}
	stressEval := pathstress.NewEvaluator()
	leg.Stress = stressEval.EvaluateLeg(leg)

	nodeRefs := []string{supplierRef, productRef}
	nodeRefs = append(nodeRefs, transportVia...)
	nodeRefs = append(nodeRefs, customerRef)
	orderedNodeRefs := uniqueCanonicalRefs(nodeRefs)
	nodes := make([]horizonmodels.SupplyChainPathNode, 0, len(orderedNodeRefs))
	nodeIndex := make(map[string]int, len(orderedNodeRefs))
	for _, ref := range orderedNodeRefs {
		nodeIndex[ref] = len(nodes)
		nodes = append(nodes, horizonmodels.SupplyChainPathNode{
			Type: supplyPathNodeType(ref),
			Ref:  ref,
		})
	}
	supplierIdx := nodeIndex[supplierRef]
	productIdx := nodeIndex[productRef]
	customerIdx := nodeIndex[customerRef]
	leg.SupplierNodeIndex = &supplierIdx
	leg.ProductNodeIndex = &productIdx
	leg.CustomerNodeIndex = &customerIdx
	for _, viaRef := range transportVia {
		if idx, ok := nodeIndex[viaRef]; ok {
			leg.TransportViaNodeIndices = append(leg.TransportViaNodeIndices, idx)
		}
	}

	metrics := cloneMetricValueMap(fragment.State)
	edges := []horizonmodels.SupplyChainPathEdge{
		{
			Type:                    "SUPPLY_LINK",
			From:                    supplierIdx,
			To:                      customerIdx,
			ViaProductIndex:         &productIdx,
			ProductNodeIndex:        &productIdx,
			ProductRef:              productRef,
			SupplyLinkID:            strings.TrimSpace(fragment.ID),
			SupplyLinkCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
			SupplyLinkTenantID:      firstNonEmptyString(strings.TrimSpace(fragment.TenantID), strings.TrimSpace(tenantID), "global"),
			SegmentIndex:            intPtr(0),
			CarriesProduct:          true,
			Role:                    "product_flow",
			Metrics:                 metrics,
			Qualifiers:              buildIdentifyPathEdgeQualifiers(metrics, leg, "product_flow", 0),
		},
	}
	if len(transportVia) > 0 {
		previousIdx := supplierIdx
		for viaOrder, viaRef := range transportVia {
			viaIdx, ok := nodeIndex[viaRef]
			if !ok {
				continue
			}
			edges = append(edges, horizonmodels.SupplyChainPathEdge{
				Type:                    "LOGISTICS",
				From:                    previousIdx,
				To:                      viaIdx,
				Mode:                    "required",
				ProductNodeIndex:        &productIdx,
				ProductRef:              productRef,
				SupplyLinkID:            strings.TrimSpace(fragment.ID),
				SupplyLinkCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
				SupplyLinkTenantID:      firstNonEmptyString(strings.TrimSpace(fragment.TenantID), strings.TrimSpace(tenantID), "global"),
				SegmentIndex:            intPtr(0),
				CarriesProduct:          true,
				Role:                    "logistics_leg",
				Metrics:                 metrics,
				Qualifiers:              buildIdentifyPathEdgeQualifiers(metrics, leg, "logistics_leg", viaOrder),
			})
			previousIdx = viaIdx
		}
		if previousIdx != customerIdx {
			edges = append(edges, horizonmodels.SupplyChainPathEdge{
				Type:                    "LOGISTICS",
				From:                    previousIdx,
				To:                      customerIdx,
				Mode:                    "required",
				ProductNodeIndex:        &productIdx,
				ProductRef:              productRef,
				SupplyLinkID:            strings.TrimSpace(fragment.ID),
				SupplyLinkCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
				SupplyLinkTenantID:      firstNonEmptyString(strings.TrimSpace(fragment.TenantID), strings.TrimSpace(tenantID), "global"),
				SegmentIndex:            intPtr(0),
				CarriesProduct:          true,
				Role:                    "logistics_leg",
				Metrics:                 metrics,
				Qualifiers:              buildIdentifyPathEdgeQualifiers(metrics, leg, "logistics_leg", len(transportVia)),
			})
		}
	}

	pathStress := stressEval.EvaluatePath([]horizonmodels.SupplyChainPathLeg{leg})
	canonicalKey := buildIdentifyStructuralPathKey(tenantID, supplierRef, productRef, customerRef)
	if canonicalKey == "" {
		return nil
	}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(
		nodes,
		edges,
		[]horizonmodels.SupplyChainPathLeg{leg},
		pathStress,
		nil,
		nil,
		map[string]interface{}{
			"buildMode":          "identify_stresses_supply_link_bootstrap",
			"sourceSupplyLink":   strings.TrimSpace(fragment.CanonicalPath),
			"bootstrapSynthetic": true,
		},
	)
	artifact := &horizonmodels.MaterializedArtifact{
		TenantID:       firstNonEmptyString(strings.TrimSpace(tenantID), "global"),
		LensID:         strings.TrimSpace(lensID),
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		CanonicalKey:   canonicalKey,
		Payload:        payload,
		PathStatus:     "active",
		DisplayName:    buildIdentifyArtifactDisplayName(supplierRef, productRef, customerRef),
		Labels:         buildIdentifyArtifactLabels(supplierRef, productRef, customerRef),
		NodeRefs:       uniqueCanonicalRefs([]string{supplierRef, productRef, customerRef}),
		AnchorSupplier: supplierRef,
		AnchorCustomer: customerRef,
		ProductAxis:    productRef,
		UpdatedBy:      "identify_stresses:bootstrap_supply_link",
		CreatedBy:      "identify_stresses:bootstrap_supply_link",
	}
	horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
	return artifact
}

func buildIdentifyStructuralPathKey(tenantID, supplierRef, productRef, customerRef string) string {
	supplierSlug := identifyCanonicalSlug(supplierRef)
	productSlug := identifyCanonicalSlug(productRef)
	customerSlug := identifyCanonicalSlug(customerRef)
	if supplierSlug == "" || productSlug == "" || customerSlug == "" {
		return ""
	}
	return fmt.Sprintf("/supply-chain/%s/%s/%s/%s", firstNonEmptyString(strings.TrimSpace(tenantID), "global"), supplierSlug, productSlug, customerSlug)
}

func identifyCanonicalSlug(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	return strings.Trim(strings.ReplaceAll(ref, "/", "_"), "_")
}

func buildIdentifyArtifactDisplayName(supplierRef, productRef, customerRef string) string {
	return strings.TrimSpace(fmt.Sprintf("%s -> %s -> %s", bestEffortRefLabel(supplierRef), bestEffortRefLabel(productRef), bestEffortRefLabel(customerRef)))
}

func buildIdentifyArtifactLabels(supplierRef, productRef, customerRef string) map[string]string {
	return map[string]string{
		"supplier": bestEffortRefLabel(supplierRef),
		"product":  bestEffortRefLabel(productRef),
		"customer": bestEffortRefLabel(customerRef),
	}
}

func supplyPathNodeType(ref string) string {
	switch {
	case strings.HasPrefix(ref, "/product/"):
		return "product"
	case strings.HasPrefix(ref, "/fac/"):
		return "fac"
	case strings.HasPrefix(ref, "/deposit/"):
		return "deposit"
	default:
		return "org"
	}
}

func bestEffortRefLabel(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) == 0 {
		return ref
	}
	slug := parts[len(parts)-1]
	slug = strings.ReplaceAll(slug, "-", " ")
	slug = strings.ReplaceAll(slug, "_", " ")
	words := strings.Fields(slug)
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func cloneMetricValueMap(in map[string]horizonmodels.MetricValue) map[string]horizonmodels.MetricValue {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]horizonmodels.MetricValue, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func buildIdentifyPathEdgeQualifiers(metrics map[string]horizonmodels.MetricValue, leg horizonmodels.SupplyChainPathLeg, role string, legOrder int) map[string]interface{} {
	qualifiers := map[string]interface{}{
		"role":              role,
		"segmentConfidence": leg.SegmentConfidence,
		"legOrder":          legOrder,
	}
	if leg.Mode != "" {
		qualifiers[horizonmodels.QualifierRouteMode] = leg.Mode
	}
	if len(leg.GeoRefs) > 0 {
		qualifiers[horizonmodels.QualifierGeoRefs] = append([]string(nil), leg.GeoRefs...)
	}
	if len(leg.FlowRefs) > 0 {
		qualifiers[horizonmodels.QualifierFlowRefs] = append([]string(nil), leg.FlowRefs...)
	}
	if len(leg.AlternateFlowRefs) > 0 {
		qualifiers[horizonmodels.QualifierAlternateFlowRefs] = append([]string(nil), leg.AlternateFlowRefs...)
	}
	if len(leg.ZoneRefs) > 0 {
		qualifiers[horizonmodels.QualifierZoneRefs] = append([]string(nil), leg.ZoneRefs...)
	}
	if len(leg.AffectedZoneRefs) > 0 {
		qualifiers[horizonmodels.QualifierAffectedZoneRefs] = append([]string(nil), leg.AffectedZoneRefs...)
	}
	if len(leg.SpatialSemantics) > 0 {
		qualifiers[horizonmodels.QualifierSpatialSemantics] = append([]string(nil), leg.SpatialSemantics...)
	}
	if leg.BindingConfidence > 0 {
		qualifiers[horizonmodels.QualifierBindingConfidence] = leg.BindingConfidence
	}
	if leg.Stress != nil {
		qualifiers[horizonmodels.QualifierPathStressScore] = leg.Stress.StressScore
		qualifiers[horizonmodels.QualifierPathFragility] = leg.Stress.Fragility
		qualifiers[horizonmodels.QualifierPathRerouteability] = leg.Stress.Rerouteability
		qualifiers[horizonmodels.QualifierPathZonePressure] = leg.Stress.ZonePressure
		qualifiers[horizonmodels.QualifierPathAffectedPressure] = leg.Stress.AffectedZonePressure
		qualifiers[horizonmodels.QualifierRecoveryLatencyHours] = leg.Stress.RecoveryLatencyHrs
		if len(leg.Stress.DominantFactors) > 0 {
			qualifiers["dominantFactors"] = append([]string(nil), leg.Stress.DominantFactors...)
		}
	}
	if len(leg.TransportViaRefs) > 0 {
		qualifiers[horizonmodels.QualifierTransportVia] = append([]string(nil), leg.TransportViaRefs...)
	}
	if leg.VesselType != "" {
		qualifiers["vesselType"] = leg.VesselType
	}
	if len(metrics) > 0 {
		qualifiers["metricKeys"] = sortedMetricKeys(metrics)
	}
	return qualifiers
}

func sortedMetricKeys(metrics map[string]horizonmodels.MetricValue) []string {
	if len(metrics) == 0 {
		return nil
	}
	keys := make([]string, 0, len(metrics))
	for key := range metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func bestArtifactBindingForFinding(artifacts []*horizonmodels.MaterializedArtifact, finding StressFinding) artifactBindingMatch {
	best := artifactBindingMatch{LegIndex: -1}
	for _, artifact := range artifacts {
		match := artifactBindingForFinding(artifact, finding)
		if match.Artifact == nil {
			continue
		}
		if match.Score > best.Score || (match.Score == best.Score && match.LegIndex >= 0 && (best.LegIndex < 0 || match.LegIndex < best.LegIndex)) {
			best = match
		}
	}
	return best
}

func artifactBindingForFinding(artifact *horizonmodels.MaterializedArtifact, finding StressFinding) artifactBindingMatch {
	match := artifactBindingMatch{Artifact: artifact, LegIndex: -1}
	if artifact == nil {
		return match
	}
	score := 0.0
	artifactKey := strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
	if finding.ArtifactKey != "" && artifactKey == strings.TrimPrefix(strings.TrimSpace(finding.ArtifactKey), "/") {
		score += 100
	}
	artifactProductRefs := collectArtifactProductRefs(artifact)
	if finding.ProductRef != "" {
		if containsString(artifactProductRefs, finding.ProductRef) {
			score += 90
			match.ProductMatched = true
			match.ProductRef = strings.TrimSpace(finding.ProductRef)
		} else {
			score -= 80
		}
	}
	artifactNodeRefs := collectArtifactNodeRefs(artifact)
	targetMatchCount := 0
	for _, targetRef := range finding.TargetRefs {
		if containsString(artifactNodeRefs, targetRef) {
			targetMatchCount++
		}
	}
	score += float64(targetMatchCount) * 22
	if strings.TrimSpace(artifact.AnchorSupplier) != "" && containsString(finding.TargetRefs, strings.TrimSpace(artifact.AnchorSupplier)) {
		score += 18
	}
	if strings.TrimSpace(artifact.AnchorCustomer) != "" && containsString(finding.TargetRefs, strings.TrimSpace(artifact.AnchorCustomer)) {
		score += 18
	}

	if finding.SupplyLinkRef != "" {
		if containsString(collectSupplyLinkRefsFromArtifact(artifact), finding.SupplyLinkRef) {
			score += 120
			match.LinkMatched = true
			match.LinkRef = strings.TrimSpace(finding.SupplyLinkRef)
		} else {
			score -= 60
		}
	}

	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err == nil && parsed != nil {
		for _, leg := range parsed.Legs {
			legScore := score
			legMatchedProduct := match.ProductMatched
			legMatchedLink := match.LinkMatched
			productRef := strings.TrimSpace(match.ProductRef)
			linkRef := strings.TrimSpace(match.LinkRef)
			if finding.SupplyLinkRef != "" {
				if strings.TrimSpace(leg.SupplyLinkCanonicalPath) == strings.TrimSpace(finding.SupplyLinkRef) {
					legScore += 140
					legMatchedLink = true
					linkRef = strings.TrimSpace(finding.SupplyLinkRef)
				} else {
					legScore -= 10
				}
			}
			if finding.ProductRef != "" {
				if strings.TrimSpace(leg.ProductRef) == strings.TrimSpace(finding.ProductRef) {
					legScore += 100
					legMatchedProduct = true
					productRef = strings.TrimSpace(finding.ProductRef)
				} else if strings.TrimSpace(leg.ProductRef) != "" {
					legScore -= 50
				}
			}
			for _, targetRef := range finding.TargetRefs {
				if targetRef == strings.TrimSpace(leg.SupplierRef) || targetRef == strings.TrimSpace(leg.CustomerRef) {
					legScore += 24
				}
			}
			if hasOverlap(finding.GeoRefs, leg.GeoRefs) {
				legScore += 8
			}
			if hasOverlap(finding.ZoneRefs, leg.ZoneRefs) || hasOverlap(finding.AffectedZoneRefs, leg.AffectedZoneRefs) {
				legScore += 8
			}
			if legScore > match.Score || (legScore == match.Score && leg.Index >= 0 && (match.LegIndex < 0 || leg.Index < match.LegIndex)) {
				match.Score = legScore
				match.LegIndex = leg.Index
				match.LinkRef = firstNonEmptyString(linkRef, strings.TrimSpace(leg.SupplyLinkCanonicalPath))
				match.ProductRef = firstNonEmptyString(productRef, strings.TrimSpace(leg.ProductRef), strings.TrimSpace(artifact.ProductAxis))
				match.ProductMatched = legMatchedProduct
				match.LinkMatched = legMatchedLink
			}
		}
	}

	if match.ProductRef == "" {
		match.ProductRef = bestProductRefForArtifact(artifact, finding)
	}
	if match.Score == 0 {
		match.Score = score
	}
	return match
}

func hasOverlap(left []string, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(left))
	for _, item := range left {
		item = strings.TrimSpace(item)
		if item != "" {
			set[item] = struct{}{}
		}
	}
	for _, item := range right {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := set[item]; ok {
			return true
		}
	}
	return false
}

func parsedEdgeEndpointRefs(parsed *horizonmodels.SupplyChainPathPayload, edge horizonmodels.SupplyChainPathEdge) (string, string) {
	if parsed == nil {
		return "", ""
	}
	supplierRef := ""
	customerRef := ""
	if edge.From >= 0 && edge.From < len(parsed.Nodes) {
		supplierRef = strings.TrimSpace(parsed.Nodes[edge.From].Ref)
	}
	if edge.To >= 0 && edge.To < len(parsed.Nodes) {
		customerRef = strings.TrimSpace(parsed.Nodes[edge.To].Ref)
	}
	return supplierRef, customerRef
}

func findingPriorityScore(finding StressFinding) float64 {
	severityBase := 0.45
	switch strings.TrimSpace(strings.ToLower(finding.Severity)) {
	case "critical":
		severityBase = 0.95
	case "high":
		severityBase = 0.78
	case "medium":
		severityBase = 0.58
	case "low":
		severityBase = 0.34
	}
	return clamp01(severityBase*0.7 + clamp01(finding.Confidence)*0.3)
}

func identifyStressFingerprint(finding StressFinding) string {
	target := firstFindingCanonicalPath(finding)
	if target == "" {
		target = firstNonEmptyString(finding.SupplyLinkRef, finding.ArtifactKey, finding.ProductRef)
	}
	primaryURL := ""
	if len(finding.Evidence) > 0 {
		primaryURL = firstNonEmptyString(strings.TrimSpace(finding.Evidence[0].URL), strings.TrimSpace(finding.Evidence[0].Domain))
	}
	dateToken := strings.TrimSpace(finding.EffectiveAt)
	if parsed := parseOptionalTime(finding.EffectiveAt); parsed != nil {
		dateToken = parsed.UTC().Format("2006-01-02")
	}
	return strings.ToLower(strings.Join([]string{
		target,
		strings.TrimSpace(finding.Mechanism),
		primaryURL,
		dateToken,
	}, "|"))
}

func factHasTag(fact *coremodels.OntologyFact, want string) bool {
	if fact == nil {
		return false
	}
	want = strings.TrimSpace(want)
	for _, tag := range fact.Tags {
		if strings.TrimSpace(tag) == want {
			return true
		}
	}
	return false
}

func firstFindingCanonicalPath(finding StressFinding) string {
	for _, ref := range finding.TargetRefs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			return ref
		}
	}
	return ""
}

func makeMetricValueFromObservation(obs StressMetricObservation, sourceIDs []string, observedAt time.Time) horizonmodels.MetricValue {
	return horizonmodels.MetricValue{
		Value:      obs.Value,
		Unit:       stringPtrIfNotEmpty(obs.Unit),
		Confidence: clamp01(firstNonZero(obs.Confidence, 0.64)),
		ObservedAt: observedAt,
		Provenance: horizonmodels.Provenance{
			Method:     "identify_stresses",
			SourceIDs:  append([]string(nil), sourceIDs...),
			ObservedAt: &observedAt,
		},
	}
}

func makeDerivedMetric(value float64, observedAt time.Time, sourceIDs []string) horizonmodels.MetricValue {
	return horizonmodels.MetricValue{
		Value:      value,
		Confidence: 0.72,
		ObservedAt: observedAt,
		Provenance: horizonmodels.Provenance{
			Method:     "identify_stresses",
			SourceIDs:  append([]string(nil), sourceIDs...),
			ObservedAt: &observedAt,
		},
	}
}

func cloneMetricMap(in map[string]horizonmodels.MetricValue) map[string]horizonmodels.MetricValue {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]horizonmodels.MetricValue, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneInterfaceMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func selectMetricKeys[V any](in map[string]V, allowed interface{}) map[string]V {
	if len(in) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	switch raw := allowed.(type) {
	case map[string]struct{}:
		for key := range raw {
			set[key] = struct{}{}
		}
	case []string:
		for _, key := range raw {
			set[strings.TrimSpace(key)] = struct{}{}
		}
	default:
		return nil
	}
	out := make(map[string]V)
	for key, value := range in {
		if _, ok := set[strings.TrimSpace(key)]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func evidenceDomain(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Hostname())
}

func interfaceToStrings(raw interface{}) []string {
	switch t := raw.(type) {
	case []string:
		return uniqueStrings(t)
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			value := strings.TrimSpace(fmt.Sprint(item))
			if value != "" && value != "<nil>" {
				out = append(out, value)
			}
		}
		return uniqueStrings(out)
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return []string{strings.TrimSpace(t)}
	default:
		return nil
	}
}

func numericQualifier(values map[string]interface{}, key string) float64 {
	if len(values) == 0 {
		return 0
	}
	raw, ok := values[strings.TrimSpace(key)]
	if !ok || raw == nil {
		return 0
	}
	switch t := raw.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		value, _ := t.Float64()
		return value
	case string:
		value, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return value
	default:
		return 0
	}
}

func booleanQualifier(values map[string]interface{}, key string) bool {
	if len(values) == 0 {
		return false
	}
	raw, ok := values[strings.TrimSpace(key)]
	if !ok || raw == nil {
		return false
	}
	switch t := raw.(type) {
	case bool:
		return t
	case string:
		value := strings.ToLower(strings.TrimSpace(t))
		return value == "true" || value == "1" || value == "yes"
	default:
		return false
	}
}

func stringPtrIfNotEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func parseOptionalTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, raw); err == nil {
			value := ts.UTC()
			return &value
		}
	}
	return nil
}

func humanizeRefLabel(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "/"))
	if value == "" {
		return ""
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '_' || r == '-' || r == '|'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, strings.ToUpper(part[:1])+part[1:])
	}
	return strings.Join(out, " ")
}

func uniqueCanonicalRefs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), " "))
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "/artifact/") {
			value = strings.TrimPrefix(value, "/artifact/")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func filterEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return uniqueStrings(out)
}

func sortedRefKeys(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		key = strings.TrimSpace(key)
		if key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func addRef(set map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	set[value] = struct{}{}
}

func cloneFloatMap(raw interface{}) map[string]float64 {
	src, ok := raw.(map[string]interface{})
	if !ok {
		if typed, ok := raw.(map[string]float64); ok {
			out := make(map[string]float64, len(typed))
			for key, value := range typed {
				out[key] = value
			}
			return out
		}
		return nil
	}
	out := make(map[string]float64, len(src))
	for key, value := range src {
		switch t := value.(type) {
		case float64:
			out[key] = t
		case int:
			out[key] = float64(t)
		case int64:
			out[key] = float64(t)
		case json.Number:
			if parsed, err := t.Float64(); err == nil {
				out[key] = parsed
			}
		}
	}
	return out
}

func cloneStringMap(raw interface{}) map[string]string {
	src, ok := raw.(map[string]interface{})
	if !ok {
		if typed, ok := raw.(map[string]string); ok {
			out := make(map[string]string, len(typed))
			for key, value := range typed {
				out[key] = value
			}
			return out
		}
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		valueStr := strings.TrimSpace(fmt.Sprint(value))
		if valueStr != "" && valueStr != "<nil>" {
			out[key] = valueStr
		}
	}
	return out
}

func findingCoordinates(finding StressFinding) (float64, float64, bool) {
	lat := numericQualifier(finding.QualifierDeltas, "lat")
	lon := numericQualifier(finding.QualifierDeltas, "lon")
	if horizonmodels.IsValidCoordinates(lat, lon) {
		return lat, lon, true
	}
	return 0, 0, false
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

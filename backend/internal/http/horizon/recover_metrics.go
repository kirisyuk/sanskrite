package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
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
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

const (
	defaultRecoverMetricsMaxRecoveries = 80
	recoverMetricsBatchSize            = 10
)

var (
	recoverMetricsPrimaryKeys = []string{
		"confidence",
		"volume",
		"share",
		"lead_time_days",
		"customs_clearance_days",
		"inventory_cover_days_at_destination",
		"switch_time_days",
		"technical_fit",
		"regulatory_fit",
		"fill_rate",
		"rejection_rate",
	}
	recoverMetricsAllowedMetricKeys = uniqueStrings(append(append([]string{}, recoverMetricsPrimaryKeys...), identifyCapabilityMetricKeys...))
	recoverMetricsAllowedMetricSet  = setFromStrings(recoverMetricsAllowedMetricKeys)
)

type RecoverMetricsRequest struct {
	ScopeMode             string                          `json:"scopeMode,omitempty"`
	SelectedCanonicalPath string                          `json:"selectedCanonicalPath,omitempty"`
	VisibleNodeRefs       []string                        `json:"visibleNodeRefs,omitempty"`
	VisibleArtifactKeys   []string                        `json:"visibleArtifactKeys,omitempty"`
	VisibleSupplyLinkRefs []string                        `json:"visibleSupplyLinkRefs,omitempty"`
	VisibleRelations      []IdentifyStressVisibleRelation `json:"visibleRelations,omitempty"`
	ClientViewID          string                          `json:"clientViewId,omitempty"`
	MaxNodes              int                             `json:"maxNodes,omitempty"`
	MaxArtifacts          int                             `json:"maxArtifacts,omitempty"`
	MaxRecoveries         int                             `json:"maxRecoveries,omitempty"`
	AsOfDate              string                          `json:"asOfDate,omitempty"`
	UseLLM                *bool                           `json:"useLlm,omitempty"`
}

type RecoverMetricsResponse struct {
	JobID         string                       `json:"jobId"`
	AcceptedScope IdentifyStressesScopeSummary `json:"acceptedScope"`
	StartedAt     string                       `json:"startedAt"`
	StreamChannel string                       `json:"streamChannel"`
}

type RecoverMetricsDebugResult struct {
	ScopeSummary      IdentifyStressesScopeSummary `json:"scopeSummary"`
	AcceptedNodeRefs  []string                     `json:"acceptedNodeRefs,omitempty"`
	AcceptedArtifacts []string                     `json:"acceptedArtifactKeys,omitempty"`
	AcceptedLinks     []string                     `json:"acceptedSupplyLinks,omitempty"`
	Gaps              []MetricRecoveryGap          `json:"gaps,omitempty"`
	GroundedDossier   string                       `json:"groundedDossier,omitempty"`
	GroundingEvidence []StressEvidence             `json:"groundingEvidence,omitempty"`
	Recoveries        []MetricRecoveryEstimate     `json:"recoveries,omitempty"`
}

type MetricRecoveryGap struct {
	SupplyLinkRef          string   `json:"supplyLinkRef"`
	SupplierRef            string   `json:"supplierRef,omitempty"`
	CustomerRef            string   `json:"customerRef,omitempty"`
	ProductRef             string   `json:"productRef,omitempty"`
	ArtifactKey            string   `json:"artifactKey,omitempty"`
	ContextualArtifactKey  string   `json:"contextualArtifactKey,omitempty"`
	LegIndex               *int     `json:"legIndex,omitempty"`
	MissingMetricKeys      []string `json:"missingMetricKeys,omitempty"`
	MissingOperationalKeys []string `json:"missingOperationalKeys,omitempty"`
	TransportViaRefs       []string `json:"transportViaRefs,omitempty"`
	ExistingMode           string   `json:"existingMode,omitempty"`
	ExistingVesselType     string   `json:"existingVesselType,omitempty"`
}

type MetricRecoveryEstimate struct {
	ID                 string                    `json:"id,omitempty"`
	Source             string                    `json:"source,omitempty"`
	SourceTier         string                    `json:"sourceTier,omitempty"`
	SupplyLinkRef      string                    `json:"supplyLinkRef"`
	ArtifactKey        string                    `json:"artifactKey,omitempty"`
	LegIndex           *int                      `json:"legIndex,omitempty"`
	TargetRefs         []string                  `json:"targetRefs,omitempty"`
	SupplierRef        string                    `json:"supplierRef,omitempty"`
	CustomerRef        string                    `json:"customerRef,omitempty"`
	ProductRef         string                    `json:"productRef,omitempty"`
	TransportViaRefs   []string                  `json:"transportViaRefs,omitempty"`
	Mode               string                    `json:"mode,omitempty"`
	VesselType         string                    `json:"vesselType,omitempty"`
	Confidence         float64                   `json:"confidence,omitempty"`
	AsOfDate           string                    `json:"asOfDate,omitempty"`
	Summary            string                    `json:"summary,omitempty"`
	MetricObservations []StressMetricObservation `json:"metricObservations,omitempty"`
	Evidence           []StressEvidence          `json:"evidence,omitempty"`
}

type RecoverMetricsCanvas struct {
	exec                     *ai.AITaskExecutor
	fragmentsRepo            *horizonrepo.HorizonFragmentsRepository
	edgesRepo                *horizonrepo.DependencyEdgesRepository
	materializedRepo         *horizonrepo.MaterializedArtifactsRepository
	lensesRepo               *horizonrepo.LensesRepository
	capabilityProfilesRepo   *horizonrepo.CapabilityProfilesRepository
	capabilityObsRepo        *horizonrepo.CapabilityObservationsRepository
	supplyLinkObsRepo        *horizonrepo.SupplyLinkObservationsRepository
	routeObsRepo             *horizonrepo.RouteObservationsRepository
	derivedRiskRepo          *horizonrepo.DerivedRiskProfilesRepository
	analyticalPrimitivesRepo *horizonrepo.AnalyticalPrimitivesRepository
	publisher                func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	logger                   *log.Logger
}

type RecoverMetricsCanvasConfig struct {
	Exec                     *ai.AITaskExecutor
	FragmentsRepo            *horizonrepo.HorizonFragmentsRepository
	EdgesRepo                *horizonrepo.DependencyEdgesRepository
	MaterializedRepo         *horizonrepo.MaterializedArtifactsRepository
	LensesRepo               *horizonrepo.LensesRepository
	CapabilityProfilesRepo   *horizonrepo.CapabilityProfilesRepository
	CapabilityObsRepo        *horizonrepo.CapabilityObservationsRepository
	SupplyLinkObsRepo        *horizonrepo.SupplyLinkObservationsRepository
	RouteObsRepo             *horizonrepo.RouteObservationsRepository
	DerivedRiskRepo          *horizonrepo.DerivedRiskProfilesRepository
	AnalyticalPrimitivesRepo *horizonrepo.AnalyticalPrimitivesRepository
	Publisher                func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	Logger                   *log.Logger
}

type recoverMetricsBatchResult struct {
	ArtifactsUpdated    int
	SupplyLinksUpdated  int
	TouchedArtifactKeys []string
	TouchedNodeRefs     []string
	RecoveredLinks      []string
}

func NewRecoverMetricsCanvas(cfg RecoverMetricsCanvasConfig) *RecoverMetricsCanvas {
	return &RecoverMetricsCanvas{
		exec:                     cfg.Exec,
		fragmentsRepo:            cfg.FragmentsRepo,
		edgesRepo:                cfg.EdgesRepo,
		materializedRepo:         cfg.MaterializedRepo,
		lensesRepo:               cfg.LensesRepo,
		capabilityProfilesRepo:   cfg.CapabilityProfilesRepo,
		capabilityObsRepo:        cfg.CapabilityObsRepo,
		supplyLinkObsRepo:        cfg.SupplyLinkObsRepo,
		routeObsRepo:             cfg.RouteObsRepo,
		derivedRiskRepo:          cfg.DerivedRiskRepo,
		analyticalPrimitivesRepo: cfg.AnalyticalPrimitivesRepo,
		publisher:                cfg.Publisher,
		logger:                   cfg.Logger,
	}
}

func (h *HorizonHandlers) RecoverMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.recoverMetricsCanvas == nil {
		http.Error(w, "Recover metrics canvas not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lensID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/"))
	if parts := strings.Split(lensID, "/"); len(parts) > 0 {
		lensID = strings.TrimSpace(parts[0])
	}
	if lensID == "" {
		http.Error(w, "lens id is required", http.StatusBadRequest)
		return
	}
	if h.service == nil || h.service.Lens() == nil {
		http.Error(w, "Lens service unavailable", http.StatusServiceUnavailable)
		return
	}
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		http.Error(w, "lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	var req RecoverMetricsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	jobID := "metrics_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	resp, err := h.recoverMetricsCanvas.Run(r.Context(), authInfo.TenantID, lens, req, jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

func (c *RecoverMetricsCanvas) DebugRecover(ctx context.Context, tenantID string, lens *horizonmodels.Lens, req RecoverMetricsRequest) (*RecoverMetricsDebugResult, error) {
	if c == nil {
		return nil, fmt.Errorf("recover-metrics canvas is nil")
	}
	if c.exec == nil {
		return nil, fmt.Errorf("recover-metrics: AI executor is not configured")
	}
	scope, err := c.resolveScope(ctx, lens, req)
	if err != nil {
		return nil, err
	}
	gaps, err := c.collectGaps(ctx, scope)
	if err != nil {
		return nil, err
	}
	grounded := ""
	evidence := []StressEvidence(nil)
	recoveries := []MetricRecoveryEstimate(nil)
	useLLM := req.UseLLM == nil || *req.UseLLM
	if useLLM && len(gaps) > 0 {
		if text, citations, err := c.runGroundedRecovery(ctx, tenantID, lens, scope, gaps, req); err == nil {
			grounded = text
			evidence = citations
			recoveries = c.filterRecoveries(scope, parseMetricRecoveryDossier(text, req.AsOfDate))
		} else if c.logger != nil {
			c.logger.Printf("recover-metrics: grounded recovery warning: %v", err)
		}
	}
	if c.logger != nil {
		c.logger.Printf(
			"recover-metrics: debug scope tenant=%s lens=%s selected=%s nodes=%d artifacts=%d supplyLinks=%d relations=%d gaps=%d recoveries=%d useLLM=%t",
			strings.TrimSpace(tenantID),
			strings.TrimSpace(lens.ID),
			strings.TrimSpace(scope.SelectedCanonicalPath),
			len(scope.NodeRefs),
			len(scope.Artifacts),
			len(scope.SupplyLinkRefs),
			len(scope.VisibleRelations),
			len(gaps),
			len(recoveries),
			useLLM,
		)
	}
	return &RecoverMetricsDebugResult{
		ScopeSummary: IdentifyStressesScopeSummary{
			ScopeMode:             firstNonEmptyString(strings.TrimSpace(req.ScopeMode), "visible_set"),
			SelectedCanonicalPath: strings.TrimSpace(scope.SelectedCanonicalPath),
			VisibleNodes:          len(uniqueCanonicalRefs(req.VisibleNodeRefs)),
			AcceptedNodes:         len(scope.NodeRefs),
			VisibleArtifacts:      len(uniqueCanonicalRefs(req.VisibleArtifactKeys)),
			AcceptedArtifacts:     len(scope.Artifacts),
			AcceptedSupplyLinks:   len(scope.SupplyLinkRefs),
			VisibleRelations:      len(uniqueCanonicalRefs(relationIDsFromSlice(req.VisibleRelations))),
			AcceptedRelations:     len(scope.VisibleRelations),
		},
		AcceptedNodeRefs:  append([]string(nil), scope.NodeRefs...),
		AcceptedArtifacts: append([]string(nil), scope.ArtifactKeys...),
		AcceptedLinks:     append([]string(nil), scope.SupplyLinkRefs...),
		Gaps:              append([]MetricRecoveryGap(nil), gaps...),
		GroundedDossier:   grounded,
		GroundingEvidence: append([]StressEvidence(nil), evidence...),
		Recoveries:        append([]MetricRecoveryEstimate(nil), recoveries...),
	}, nil
}

func (c *RecoverMetricsCanvas) Run(ctx context.Context, tenantID string, lens *horizonmodels.Lens, req RecoverMetricsRequest, jobID string) (*RecoverMetricsResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("recover-metrics canvas is nil")
	}
	debug, err := c.DebugRecover(ctx, tenantID, lens, req)
	if err != nil {
		return nil, err
	}
	recoveries := append([]MetricRecoveryEstimate(nil), debug.Recoveries...)
	if req.MaxRecoveries <= 0 {
		req.MaxRecoveries = defaultRecoverMetricsMaxRecoveries
	}
	if len(recoveries) > req.MaxRecoveries {
		recoveries = recoveries[:req.MaxRecoveries]
	}
	_ = c.publish(ctx, "horizon.recover_metrics.started", tenantID, map[string]interface{}{
		"tenantId":      tenantID,
		"lensId":        strings.TrimSpace(lens.ID),
		"projectId":     strings.TrimSpace(lens.ProjectID),
		"jobId":         jobID,
		"scopeSummary":  debug.ScopeSummary,
		"recoveries":    len(recoveries),
		"visibleNodes":  debug.ScopeSummary.AcceptedNodes,
		"visibleRoutes": debug.ScopeSummary.AcceptedArtifacts,
	})
	scope, err := c.resolveScope(ctx, lens, req)
	if err != nil {
		return nil, err
	}
	totalArtifacts := 0
	totalSupplyLinks := 0
	touchedArtifacts := map[string]struct{}{}
	touchedNodes := map[string]struct{}{}
	recoveredLinks := map[string]struct{}{}
	for start := 0; start < len(recoveries); start += recoverMetricsBatchSize {
		end := start + recoverMetricsBatchSize
		if end > len(recoveries) {
			end = len(recoveries)
		}
		batch, err := c.applyRecoveryBatch(ctx, tenantID, lens, scope, recoveries[start:end])
		if err != nil {
			if c.logger != nil {
				c.logger.Printf(
					"recover-metrics: run failed tenant=%s lens=%s job=%s batchStart=%d batchEnd=%d err=%v",
					strings.TrimSpace(tenantID),
					strings.TrimSpace(lens.ID),
					strings.TrimSpace(jobID),
					start,
					end,
					err,
				)
			}
			_ = c.publish(ctx, "horizon.recover_metrics.failed", tenantID, map[string]interface{}{
				"tenantId":  tenantID,
				"lensId":    strings.TrimSpace(lens.ID),
				"projectId": strings.TrimSpace(lens.ProjectID),
				"jobId":     jobID,
				"error":     err.Error(),
			})
			return nil, err
		}
		totalArtifacts += batch.ArtifactsUpdated
		totalSupplyLinks += batch.SupplyLinksUpdated
		for _, key := range batch.TouchedArtifactKeys {
			addRef(touchedArtifacts, key)
		}
		for _, ref := range batch.TouchedNodeRefs {
			addRef(touchedNodes, ref)
		}
		for _, ref := range batch.RecoveredLinks {
			addRef(recoveredLinks, ref)
		}
		if c.logger != nil {
			c.logger.Printf(
				"recover-metrics: batch applied tenant=%s lens=%s job=%s batchStart=%d batchEnd=%d artifactsUpdated=%d recoveredLinks=%v artifactKeys=%v",
				strings.TrimSpace(tenantID),
				strings.TrimSpace(lens.ID),
				strings.TrimSpace(jobID),
				start,
				end,
				batch.ArtifactsUpdated,
				batch.RecoveredLinks,
				batch.TouchedArtifactKeys,
			)
		}
		_ = c.publish(ctx, "horizon.recover_metrics.batch_applied", tenantID, map[string]interface{}{
			"tenantId":            tenantID,
			"lensId":              strings.TrimSpace(lens.ID),
			"projectId":           strings.TrimSpace(lens.ProjectID),
			"jobId":               jobID,
			"batchStart":          start,
			"batchEnd":            end,
			"artifactsUpdated":    batch.ArtifactsUpdated,
			"supplyLinksUpdated":  batch.SupplyLinksUpdated,
			"touchedArtifactKeys": batch.TouchedArtifactKeys,
			"touchedNodeRefs":     batch.TouchedNodeRefs,
			"recoveredLinks":      batch.RecoveredLinks,
			"requiresRefetch":     len(batch.TouchedArtifactKeys) > 50,
		})
	}
	if c.logger != nil {
		c.logger.Printf(
			"recover-metrics: run completed tenant=%s lens=%s job=%s artifactsUpdated=%d supplyLinksUpdated=%d recoveredLinks=%v artifactKeys=%v",
			strings.TrimSpace(tenantID),
			strings.TrimSpace(lens.ID),
			strings.TrimSpace(jobID),
			totalArtifacts,
			totalSupplyLinks,
			sortedRefKeys(recoveredLinks),
			sortedRefKeys(touchedArtifacts),
		)
	}
	_ = c.publish(ctx, "horizon.recover_metrics.completed", tenantID, map[string]interface{}{
		"tenantId":            tenantID,
		"lensId":              strings.TrimSpace(lens.ID),
		"projectId":           strings.TrimSpace(lens.ProjectID),
		"jobId":               jobID,
		"status":              "completed",
		"artifactsUpdated":    totalArtifacts,
		"supplyLinksUpdated":  totalSupplyLinks,
		"recoveredLinks":      sortedRefKeys(recoveredLinks),
		"touchedArtifactKeys": sortedRefKeys(touchedArtifacts),
		"touchedNodeRefs":     sortedRefKeys(touchedNodes),
	})
	return &RecoverMetricsResponse{
		JobID:         jobID,
		AcceptedScope: debug.ScopeSummary,
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		StreamChannel: "realtime",
	}, nil
}

func (c *RecoverMetricsCanvas) resolveScope(ctx context.Context, lens *horizonmodels.Lens, req RecoverMetricsRequest) (*identifyStressScope, error) {
	helper := &IdentifyStressesCanvas{
		fragmentsRepo:    c.fragmentsRepo,
		edgesRepo:        c.edgesRepo,
		materializedRepo: c.materializedRepo,
		lensesRepo:       c.lensesRepo,
		logger:           c.logger,
	}
	return helper.resolveScope(ctx, lens, IdentifyStressesRequest{
		ScopeMode:             req.ScopeMode,
		SelectedCanonicalPath: req.SelectedCanonicalPath,
		VisibleNodeRefs:       req.VisibleNodeRefs,
		VisibleArtifactKeys:   req.VisibleArtifactKeys,
		VisibleSupplyLinkRefs: req.VisibleSupplyLinkRefs,
		VisibleRelations:      req.VisibleRelations,
		ClientViewID:          req.ClientViewID,
		MaxNodes:              req.MaxNodes,
		MaxArtifacts:          req.MaxArtifacts,
		MaxFindings:           req.MaxRecoveries,
	})
}

func (c *RecoverMetricsCanvas) collectGaps(ctx context.Context, scope *identifyStressScope) ([]MetricRecoveryGap, error) {
	if scope == nil || c.fragmentsRepo == nil {
		return nil, nil
	}
	fragmentsByPath := map[string]*horizonmodels.HorizonFragment{}
	if len(scope.SupplyLinkRefs) > 0 {
		if fragments, err := c.fragmentsRepo.ListByCanonicalPaths(ctx, uniqueCanonicalRefs(scope.SupplyLinkRefs)); err == nil {
			for _, frag := range fragments {
				if frag == nil {
					continue
				}
				fragmentsByPath[strings.TrimSpace(frag.CanonicalPath)] = frag
			}
		}
	}
	out := make([]MetricRecoveryGap, 0, len(scope.SupplyLinkRefs))
	for _, ref := range scope.SupplyLinkRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		frag := fragmentsByPath[ref]
		if frag == nil {
			var err error
			frag, err = c.fragmentsRepo.GetByCanonicalPath(ctx, ref)
			if err == nil && frag != nil {
				fragmentsByPath[ref] = frag
			}
		}
		if frag == nil {
			continue
		}
		productRef := ""
		supplierRef := ""
		customerRef := ""
		if frag.ProductRef != nil {
			productRef = strings.TrimSpace(*frag.ProductRef)
		}
		if frag.SupplierRef != nil {
			supplierRef = strings.TrimSpace(*frag.SupplierRef)
		}
		if frag.CustomerRef != nil {
			customerRef = strings.TrimSpace(*frag.CustomerRef)
		}
		existing, contextual := exactArtifactGapForSupplyLink(scope.Artifacts, ref, supplierRef, customerRef, productRef)
		missingMetrics, missingOperational := recoverMetricMissingKeys(frag, existing, ref)
		if existing == nil && len(missingMetrics) == 0 && len(missingOperational) == 0 {
			missingMetrics = append(missingMetrics, "lead_time_days", "inventory_cover_days_at_destination", "share")
		}
		if len(missingMetrics) == 0 && len(missingOperational) == 0 {
			continue
		}
		gap := MetricRecoveryGap{
			SupplyLinkRef:          ref,
			SupplierRef:            supplierRef,
			CustomerRef:            customerRef,
			ProductRef:             productRef,
			MissingMetricKeys:      missingMetrics,
			MissingOperationalKeys: missingOperational,
			TransportViaRefs:       uniqueCanonicalRefs(append([]string(nil), frag.TransportViaRefs...)),
		}
		if existing != nil && existing.Artifact != nil {
			gap.ArtifactKey = strings.TrimSpace(existing.Artifact.CanonicalKey)
			gap.LegIndex = existing.LegIndex
			if parsed, err := horizonmodels.ParseSupplyChainPathPayload(existing.Artifact.Payload); err == nil && parsed != nil && existing.LegIndex != nil && *existing.LegIndex >= 0 && *existing.LegIndex < len(parsed.Legs) {
				leg := parsed.Legs[*existing.LegIndex]
				gap.ExistingMode = strings.TrimSpace(leg.Mode)
				gap.ExistingVesselType = strings.TrimSpace(leg.VesselType)
				gap.TransportViaRefs = uniqueCanonicalRefs(firstNonEmptySlice(gap.TransportViaRefs, append([]string(nil), leg.TransportViaRefs...)))
			}
		}
		if contextual != nil && contextual.Artifact != nil {
			gap.ContextualArtifactKey = strings.TrimSpace(contextual.Artifact.CanonicalKey)
		}
		out = append(out, gap)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].MissingMetricKeys) != len(out[j].MissingMetricKeys) {
			return len(out[i].MissingMetricKeys) > len(out[j].MissingMetricKeys)
		}
		if out[i].ArtifactKey == "" && out[j].ArtifactKey != "" {
			return true
		}
		if out[i].ArtifactKey != "" && out[j].ArtifactKey == "" {
			return false
		}
		return out[i].SupplyLinkRef < out[j].SupplyLinkRef
	})
	return out, nil
}

func recoverMetricMissingKeys(fragment *horizonmodels.HorizonFragment, match *substrateArtifactMatch, supplyLinkRef string) ([]string, []string) {
	present := map[string]struct{}{}
	if fragment != nil {
		for key, metric := range fragment.State {
			if (metric.Value != 0 || metric.Confidence > 0) && key != "" {
				present[strings.TrimSpace(key)] = struct{}{}
			}
		}
		if len(fragment.TransportViaRefs) > 0 {
			present["transportViaRefs"] = struct{}{}
		}
		if stringQualifier(fragment.Qualifiers, horizonmodels.QualifierRouteMode) != "" {
			present["mode"] = struct{}{}
		}
		if stringQualifier(fragment.Qualifiers, "vesselType") != "" {
			present["vesselType"] = struct{}{}
		}
	}
	if match != nil && match.Artifact != nil {
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(match.Artifact.Payload)
		if err == nil && parsed != nil {
			for _, edge := range parsed.Edges {
				if strings.TrimSpace(edge.SupplyLinkCanonicalPath) != strings.TrimSpace(supplyLinkRef) {
					continue
				}
				for key, metric := range edge.Metrics {
					if (metric.Value != 0 || metric.Confidence > 0) && key != "" {
						present[strings.TrimSpace(key)] = struct{}{}
					}
				}
				if routeMode := stringQualifier(edge.Qualifiers, horizonmodels.QualifierRouteMode); routeMode != "" {
					present["mode"] = struct{}{}
				}
				if stringQualifier(edge.Qualifiers, "vesselType") != "" {
					present["vesselType"] = struct{}{}
				}
				if len(interfaceToStrings(edge.Qualifiers[horizonmodels.QualifierTransportVia])) > 0 {
					present["transportViaRefs"] = struct{}{}
				}
			}
			if match.LegIndex != nil && *match.LegIndex >= 0 && *match.LegIndex < len(parsed.Legs) {
				leg := parsed.Legs[*match.LegIndex]
				if leg.Mode != "" {
					present["mode"] = struct{}{}
				}
				if leg.VesselType != "" {
					present["vesselType"] = struct{}{}
				}
				if len(leg.TransportViaRefs) > 0 {
					present["transportViaRefs"] = struct{}{}
				}
			}
		}
	}
	missingMetrics := make([]string, 0, len(recoverMetricsAllowedMetricKeys))
	for _, key := range recoverMetricsAllowedMetricKeys {
		if _, ok := present[key]; !ok {
			missingMetrics = append(missingMetrics, key)
		}
	}
	missingOperational := []string{}
	for _, key := range []string{"mode", "transportViaRefs", "vesselType"} {
		if _, ok := present[key]; !ok {
			missingOperational = append(missingOperational, key)
		}
	}
	return missingMetrics, missingOperational
}

func (c *RecoverMetricsCanvas) runGroundedRecovery(ctx context.Context, tenantID string, lens *horizonmodels.Lens, scope *identifyStressScope, gaps []MetricRecoveryGap, req RecoverMetricsRequest) (string, []StressEvidence, error) {
	prompt := c.buildGroundedRecoveryPrompt(lens, scope, gaps, req.AsOfDate)
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
		UseCase:   "horizon.recover_metrics",
		CascadeID: strings.TrimSpace(lens.ID),
	}
	res, err := c.exec.ExecuteTask(ctx, task)
	if err != nil {
		return "", nil, fmt.Errorf("recover-metrics grounded recovery: %w", err)
	}
	text := strings.TrimSpace(res.Content)
	evidence := make([]StressEvidence, 0, len(res.Citations))
	for _, citation := range res.Citations {
		rawURL := strings.TrimSpace(citation.URL)
		if rawURL == "" {
			continue
		}
		evidence = append(evidence, StressEvidence{
			URL:    rawURL,
			Title:  strings.TrimSpace(citation.Title),
			Domain: stressEvidenceDomain(rawURL),
		})
	}
	return text, evidence, nil
}

func (c *RecoverMetricsCanvas) buildGroundedRecoveryPrompt(lens *horizonmodels.Lens, scope *identifyStressScope, gaps []MetricRecoveryGap, asOfDate string) string {
	dateToken := normalizedAsOfDate(asOfDate)
	selected := firstNonEmptyString(strings.TrimSpace(scope.SelectedCanonicalPath), "(none)")
	allowedViaRefs := make([]string, 0, len(scope.NodeRefs))
	for _, ref := range scope.NodeRefs {
		if strings.HasPrefix(strings.TrimSpace(ref), "/fac/") {
			allowedViaRefs = append(allowedViaRefs, strings.TrimSpace(ref))
		}
	}
	allowedViaRefs = uniqueCanonicalRefs(allowedViaRefs)

	var b strings.Builder
	fmt.Fprintf(&b, "You recover operational supply-chain metrics for a Horizon lens as of %s.\n", dateToken)
	fmt.Fprintf(&b, "Lens: %s (%s)\n", strings.TrimSpace(lens.Name), strings.TrimSpace(lens.ID))
	fmt.Fprintf(&b, "Selected anchor: %s\n", selected)
	b.WriteString("You MUST NOT invent new objects or new supply links. You may only enrich the direct operational details and metrics for the supplyLinkRef values listed in the gap list below.\n")
	b.WriteString("You may use grounded web evidence from primary sources, official filings, company sites, customs/logistics sources, carrier and port advisories, trade press, operator reviews, practitioner forums, and industry communities.\n")
	b.WriteString("Forum and review evidence is allowed only as weak secondary evidence. If any metric relies materially on forums, community posts, or operator reviews, mark sourceTier=community and lower confidence accordingly.\n")
	b.WriteString("Only recover metrics and details that are known directly or can be bounded indirectly from grounded evidence as of the date above.\n")
	b.WriteString("Do NOT bridge entities through long indirect reasoning. Do NOT create new abstract graph relationships. Do NOT recover metrics for links that are not in the gap list.\n")
	b.WriteString("Use the graph slice below as the complete allowed substrate. Direct and side-context entities may be present, but you must only enrich the listed supply links.\n")
	b.WriteString("If an exact numeric value is unavailable, provide a conservative estimate only when grounded evidence supports that estimate indirectly. Lower confidence in that case.\n")
	b.WriteString("Return plain text only. No markdown tables. No code fences. Use exactly this format:\n")
	b.WriteString("RECOVERY <n>\n")
	b.WriteString("supplyLinkRef: <canonical supply link ref>\n")
	b.WriteString("artifactKey: <canonical artifact key or empty>\n")
	b.WriteString("legIndex: <integer or empty>\n")
	b.WriteString("supplierRef: <canonical ref or empty>\n")
	b.WriteString("customerRef: <canonical ref or empty>\n")
	b.WriteString("productRef: <canonical ref or empty>\n")
	b.WriteString("targetRefs: <comma separated canonical refs or empty>\n")
	b.WriteString("transportViaRefs: <comma separated canonical refs or empty>\n")
	b.WriteString("mode: <sea|rail|road|air|pipeline_gas|pipeline_oil|pipeline_h2|intermodal|internet_cable|unknown>\n")
	b.WriteString("vesselType: <short token or empty>\n")
	b.WriteString("confidence: <0..1 number>\n")
	b.WriteString("sourceTier: <primary|secondary|community>\n")
	b.WriteString("asOfDate: <YYYY-MM-DD>\n")
	b.WriteString("summary: <one sentence>\n")
	b.WriteString("metric.<key>: <value> | unit=<unit> | scope=<link|path|supplier_capability> | confidence=<0..1> | description=<short note>\n")
	b.WriteString("evidence: <url>\n\n")

	b.WriteString("Allowed metric keys:\n")
	for _, key := range recoverMetricsAllowedMetricKeys {
		fmt.Fprintf(&b, "- %s\n", key)
	}
	b.WriteString("\nAllowed facility refs for transportViaRefs:\n")
	for _, ref := range allowedViaRefs {
		fmt.Fprintf(&b, "- %s\n", ref)
	}
	b.WriteString("\nAllowed canonical node refs already present in the graph slice:\n")
	for _, ref := range scope.NodeRefs {
		fmt.Fprintf(&b, "- %s\n", ref)
	}
	b.WriteString("\nVisible materialized supply-path artifacts:\n")
	for i, artifact := range scope.Artifacts {
		if i >= 40 {
			break
		}
		if artifact == nil {
			continue
		}
		fmt.Fprintf(&b, "- artifactKey=%s supplier=%s customer=%s product=%s stress=%.2f legs=%d\n",
			strings.TrimSpace(artifact.CanonicalKey),
			firstNonEmptyString(strings.TrimSpace(artifact.AnchorSupplier), "(unknown)"),
			firstNonEmptyString(strings.TrimSpace(artifact.AnchorCustomer), "(unknown)"),
			firstNonEmptyString(strings.TrimSpace(artifact.ProductAxis), "(unknown)"),
			artifact.PathStressScore,
			artifact.LegCount,
		)
	}
	b.WriteString("\nMetric gaps to recover:\n")
	for i, gap := range gaps {
		fmt.Fprintf(&b, "%d. supplyLinkRef=%s supplier=%s customer=%s product=%s artifactKey=%s contextualArtifact=%s legIndex=%s missingMetrics=%s missingOperational=%s existingMode=%s existingVessel=%s knownTransportVia=%s\n",
			i+1,
			gap.SupplyLinkRef,
			firstNonEmptyString(gap.SupplierRef, "(unknown)"),
			firstNonEmptyString(gap.CustomerRef, "(unknown)"),
			firstNonEmptyString(gap.ProductRef, "(unknown)"),
			firstNonEmptyString(gap.ArtifactKey, "(none)"),
			firstNonEmptyString(gap.ContextualArtifactKey, "(none)"),
			intPtrString(gap.LegIndex),
			strings.Join(gap.MissingMetricKeys, ","),
			strings.Join(gap.MissingOperationalKeys, ","),
			firstNonEmptyString(gap.ExistingMode, "(none)"),
			firstNonEmptyString(gap.ExistingVesselType, "(none)"),
			strings.Join(gap.TransportViaRefs, ","),
		)
	}
	b.WriteString("\nVisible graph relations:\n")
	for i, rel := range scope.VisibleRelations {
		if i >= 100 {
			break
		}
		fmt.Fprintf(&b, "- %s -> %s type=%s family=%s role=%s criticality=%.2f meaning=%s\n",
			firstNonEmptyString(rel.SourceCanonicalPath, "(unknown)"),
			firstNonEmptyString(rel.TargetCanonicalPath, "(unknown)"),
			firstNonEmptyString(rel.RelationType, "(unknown)"),
			firstNonEmptyString(rel.RelationFamily, "(unknown)"),
			firstNonEmptyString(rel.RelationRole, "(unknown)"),
			rel.Criticality,
			strings.TrimSpace(rel.BusinessMeaning),
		)
	}
	return b.String()
}

func parseMetricRecoveryDossier(text string, asOfDate string) []MetricRecoveryEstimate {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	blocks := []string{}
	start := -1
	lines := strings.Split(text, "\n")
	for idx, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(strings.ToUpper(line), "RECOVERY ") {
			if start >= 0 {
				blocks = append(blocks, strings.Join(lines[start:idx], "\n"))
			}
			start = idx
		}
	}
	if start >= 0 {
		blocks = append(blocks, strings.Join(lines[start:], "\n"))
	} else {
		blocks = append(blocks, text)
	}
	out := make([]MetricRecoveryEstimate, 0, len(blocks))
	for _, block := range blocks {
		rec := MetricRecoveryEstimate{
			Source:   "llm_grounded",
			AsOfDate: normalizedAsOfDate(asOfDate),
		}
		lines := strings.Split(block, "\n")
		for _, rawLine := range lines {
			line := strings.TrimSpace(rawLine)
			if line == "" {
				continue
			}
			if strings.HasPrefix(strings.ToUpper(line), "RECOVERY ") {
				continue
			}
			switch {
			case strings.HasPrefix(line, "supplyLinkRef:"):
				rec.SupplyLinkRef = strings.TrimSpace(strings.TrimPrefix(line, "supplyLinkRef:"))
			case strings.HasPrefix(line, "artifactKey:"):
				rec.ArtifactKey = strings.TrimSpace(strings.TrimPrefix(line, "artifactKey:"))
			case strings.HasPrefix(line, "legIndex:"):
				if idx, ok := coerceJSONInt(strings.TrimSpace(strings.TrimPrefix(line, "legIndex:"))); ok {
					rec.LegIndex = &idx
				}
			case strings.HasPrefix(line, "supplierRef:"):
				rec.SupplierRef = strings.TrimSpace(strings.TrimPrefix(line, "supplierRef:"))
			case strings.HasPrefix(line, "customerRef:"):
				rec.CustomerRef = strings.TrimSpace(strings.TrimPrefix(line, "customerRef:"))
			case strings.HasPrefix(line, "productRef:"):
				rec.ProductRef = strings.TrimSpace(strings.TrimPrefix(line, "productRef:"))
			case strings.HasPrefix(line, "targetRefs:"):
				rec.TargetRefs = splitCommaRefs(strings.TrimSpace(strings.TrimPrefix(line, "targetRefs:")))
			case strings.HasPrefix(line, "transportViaRefs:"):
				rec.TransportViaRefs = splitCommaRefs(strings.TrimSpace(strings.TrimPrefix(line, "transportViaRefs:")))
			case strings.HasPrefix(line, "mode:"):
				rec.Mode = strings.TrimSpace(strings.TrimPrefix(line, "mode:"))
			case strings.HasPrefix(line, "vesselType:"):
				rec.VesselType = strings.TrimSpace(strings.TrimPrefix(line, "vesselType:"))
			case strings.HasPrefix(line, "confidence:"):
				rec.Confidence = clamp01(parseFloatLoose(strings.TrimSpace(strings.TrimPrefix(line, "confidence:"))))
			case strings.HasPrefix(line, "sourceTier:"):
				rec.SourceTier = strings.TrimSpace(strings.TrimPrefix(line, "sourceTier:"))
			case strings.HasPrefix(line, "asOfDate:"):
				rec.AsOfDate = normalizedAsOfDate(strings.TrimSpace(strings.TrimPrefix(line, "asOfDate:")))
			case strings.HasPrefix(line, "summary:"):
				rec.Summary = strings.TrimSpace(strings.TrimPrefix(line, "summary:"))
			case strings.HasPrefix(line, "metric."):
				if obs, ok := parseMetricRecoveryObservation(line); ok {
					rec.MetricObservations = append(rec.MetricObservations, obs)
				}
			case strings.HasPrefix(line, "evidence:"):
				rawURL := strings.TrimSpace(strings.TrimPrefix(line, "evidence:"))
				if rawURL != "" {
					rec.Evidence = append(rec.Evidence, StressEvidence{URL: rawURL, Domain: stressEvidenceDomain(rawURL)})
				}
			}
		}
		rec.SupplyLinkRef = strings.TrimSpace(rec.SupplyLinkRef)
		rec.ArtifactKey = strings.TrimPrefix(strings.TrimSpace(rec.ArtifactKey), "/")
		if rec.SupplyLinkRef == "" || len(rec.MetricObservations) == 0 {
			continue
		}
		if rec.SourceTier == "" {
			rec.SourceTier = "secondary"
		}
		if rec.Confidence == 0 {
			rec.Confidence = 0.62
		}
		if rec.ID == "" {
			rec.ID = "recovery:" + strings.Trim(rec.SupplyLinkRef, "/")
		}
		out = append(out, rec)
	}
	return out
}

func parseMetricRecoveryObservation(line string) (StressMetricObservation, bool) {
	keyLine := strings.TrimSpace(strings.TrimPrefix(line, "metric."))
	parts := strings.SplitN(keyLine, ":", 2)
	if len(parts) != 2 {
		return StressMetricObservation{}, false
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return StressMetricObservation{}, false
	}
	fields := strings.Split(parts[1], "|")
	if len(fields) == 0 {
		return StressMetricObservation{}, false
	}
	obs := StressMetricObservation{
		MetricKey: key,
		Scope:     "link",
	}
	obs.Value = parseFloatLoose(strings.TrimSpace(fields[0]))
	for _, raw := range fields[1:] {
		field := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(field, "unit="):
			obs.Unit = strings.TrimSpace(strings.TrimPrefix(field, "unit="))
		case strings.HasPrefix(field, "scope="):
			obs.Scope = strings.TrimSpace(strings.TrimPrefix(field, "scope="))
		case strings.HasPrefix(field, "confidence="):
			obs.Confidence = clamp01(parseFloatLoose(strings.TrimSpace(strings.TrimPrefix(field, "confidence="))))
		case strings.HasPrefix(field, "description="):
			obs.Description = strings.TrimSpace(strings.TrimPrefix(field, "description="))
		}
	}
	if _, ok := recoverMetricsAllowedMetricSet[obs.MetricKey]; !ok {
		return StressMetricObservation{}, false
	}
	return obs, true
}

func (c *RecoverMetricsCanvas) filterRecoveries(scope *identifyStressScope, recoveries []MetricRecoveryEstimate) []MetricRecoveryEstimate {
	linkSet := setFromStrings(scope.SupplyLinkRefs)
	artifactSet := setFromStrings(scope.ArtifactKeys)
	out := make([]MetricRecoveryEstimate, 0, len(recoveries))
	for _, rec := range recoveries {
		if _, ok := linkSet[strings.TrimSpace(rec.SupplyLinkRef)]; !ok {
			continue
		}
		if rec.ArtifactKey != "" {
			rec.ArtifactKey = strings.TrimPrefix(strings.TrimSpace(rec.ArtifactKey), "/")
			if _, ok := artifactSet[rec.ArtifactKey]; !ok {
				rec.ArtifactKey = ""
			}
		}
		if rec.SourceTier == "community" {
			rec.Confidence = math.Min(rec.Confidence, 0.55)
			for idx := range rec.MetricObservations {
				rec.MetricObservations[idx].Confidence = math.Min(firstNonZero(rec.MetricObservations[idx].Confidence, rec.Confidence), 0.55)
			}
		}
		out = append(out, rec)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			return out[i].SupplyLinkRef < out[j].SupplyLinkRef
		}
		return out[i].Confidence > out[j].Confidence
	})
	return out
}

func (c *RecoverMetricsCanvas) applyRecoveryBatch(ctx context.Context, tenantID string, lens *horizonmodels.Lens, scope *identifyStressScope, recoveries []MetricRecoveryEstimate) (*recoverMetricsBatchResult, error) {
	if len(recoveries) == 0 {
		return &recoverMetricsBatchResult{}, nil
	}
	now := time.Now().UTC()
	fragmentRefs := uniqueCanonicalRefs(extractRecoverySupplyLinks(recoveries))
	fragments := map[string]*horizonmodels.HorizonFragment{}
	if len(fragmentRefs) > 0 && c.fragmentsRepo != nil {
		loaded, err := c.fragmentsRepo.ListByCanonicalPaths(ctx, fragmentRefs)
		if err != nil {
			return nil, err
		}
		for _, frag := range loaded {
			if frag == nil {
				continue
			}
			fragments[strings.TrimSpace(frag.CanonicalPath)] = frag
		}
	}
	artifactByKey := map[string]*horizonmodels.MaterializedArtifact{}
	for _, artifact := range scope.Artifacts {
		if artifact == nil {
			continue
		}
		artifactByKey[strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")] = artifact
	}
	updatedFragments := map[string]*horizonmodels.HorizonFragment{}
	updatedArtifacts := map[string]*horizonmodels.MaterializedArtifact{}
	touchedNodes := map[string]struct{}{}
	recoveredLinks := map[string]struct{}{}
	for _, recovery := range recoveries {
		sourceIDs := recoveryEvidenceURLs(recovery)
		if frag := fragments[strings.TrimSpace(recovery.SupplyLinkRef)]; frag != nil {
			updated := cloneFragmentForMutation(frag)
			c.applyRecoveryToFragment(updated, recovery, sourceIDs, now)
			updatedFragments[strings.TrimSpace(updated.CanonicalPath)] = updated
			addRef(touchedNodes, strings.TrimSpace(updated.CanonicalPath))
			if updated.SupplierRef != nil {
				addRef(touchedNodes, strings.TrimSpace(*updated.SupplierRef))
			}
			if updated.CustomerRef != nil {
				addRef(touchedNodes, strings.TrimSpace(*updated.CustomerRef))
			}
		}
		artifact, ok := findMetricRecoveryArtifact(scope.Artifacts, artifactByKey, recovery)
		if ok && artifact != nil {
			updatedArtifact, err := c.applyRecoveryToArtifact(ctx, tenantID, artifact, recovery, sourceIDs, now)
			if err != nil {
				return nil, err
			}
			if updatedArtifact != nil {
				key := strings.TrimPrefix(strings.TrimSpace(updatedArtifact.CanonicalKey), "/")
				updatedArtifacts[key] = updatedArtifact
				addRef(recoveredLinks, recovery.SupplyLinkRef)
				for _, ref := range updatedArtifact.NodeRefs {
					addRef(touchedNodes, ref)
				}
			}
		} else {
			addRef(recoveredLinks, recovery.SupplyLinkRef)
		}
	}
	for _, frag := range updatedFragments {
		if frag == nil {
			continue
		}
		frag.Version++
		if err := c.fragmentsRepo.Update(ctx, frag); err != nil {
			return nil, fmt.Errorf("recover-metrics: update fragment %s: %w", frag.CanonicalPath, err)
		}
		_ = c.publishNodeUpsert(ctx, tenantID, frag, "recover_metrics")
	}
	updatedArtifactKeys := make([]string, 0, len(updatedArtifacts))
	for key, artifact := range updatedArtifacts {
		if artifact == nil {
			continue
		}
		annotateSupplyChainArtifactRiskOverlay(artifact)
		if err := c.materializedRepo.Update(ctx, artifact); err != nil {
			return nil, fmt.Errorf("recover-metrics: update artifact %s: %w", artifact.CanonicalKey, err)
		}
		updatedArtifactKeys = append(updatedArtifactKeys, key)
		helper := &IdentifyStressesCanvas{lensesRepo: c.lensesRepo, publisher: c.publisher}
		_ = helper.syncLensScopeArtifact(ctx, lens, artifact)
		_ = c.publish(ctx, "horizon.recover_metrics.artifact_upserted", tenantID, artifactRealtimePayload(artifact))
	}
	return &recoverMetricsBatchResult{
		ArtifactsUpdated:    len(updatedArtifacts),
		SupplyLinksUpdated:  len(updatedFragments),
		TouchedArtifactKeys: uniqueStrings(updatedArtifactKeys),
		TouchedNodeRefs:     sortedRefKeys(touchedNodes),
		RecoveredLinks:      sortedRefKeys(recoveredLinks),
	}, nil
}

func cloneFragmentForMutation(fragment *horizonmodels.HorizonFragment) *horizonmodels.HorizonFragment {
	if fragment == nil {
		return nil
	}
	copyFragment := *fragment
	copyFragment.State = cloneMetricMap(fragment.State)
	copyFragment.Qualifiers = cloneInterfaceMap(fragment.Qualifiers)
	copyFragment.TransportViaRefs = append([]string(nil), fragment.TransportViaRefs...)
	return &copyFragment
}

func (c *RecoverMetricsCanvas) applyRecoveryToFragment(fragment *horizonmodels.HorizonFragment, recovery MetricRecoveryEstimate, sourceIDs []string, now time.Time) {
	if fragment == nil {
		return
	}
	if fragment.State == nil {
		fragment.State = map[string]horizonmodels.MetricValue{}
	}
	if fragment.Qualifiers == nil {
		fragment.Qualifiers = map[string]interface{}{}
	}
	if fragment.ProductRef == nil && strings.TrimSpace(recovery.ProductRef) != "" {
		ref := strings.TrimSpace(recovery.ProductRef)
		fragment.ProductRef = &ref
	}
	if len(recovery.TransportViaRefs) > 0 {
		fragment.TransportViaRefs = uniqueCanonicalRefs(append(fragment.TransportViaRefs, recovery.TransportViaRefs...))
		fragment.Qualifiers[horizonmodels.QualifierTransportVia] = append([]string(nil), fragment.TransportViaRefs...)
	}
	if recovery.Mode != "" {
		fragment.Qualifiers[horizonmodels.QualifierRouteMode] = strings.TrimSpace(recovery.Mode)
	}
	if recovery.VesselType != "" {
		fragment.Qualifiers["vesselType"] = strings.TrimSpace(recovery.VesselType)
	}
	fragment.Qualifiers["metricRecoverySource"] = firstNonEmptyString(recovery.Source, "llm_grounded")
	fragment.Qualifiers["metricRecoverySourceTier"] = firstNonEmptyString(recovery.SourceTier, "secondary")
	fragment.Qualifiers["metricRecoveryAsOf"] = normalizedAsOfDate(recovery.AsOfDate)
	fragment.Qualifiers["metricRecoverySummary"] = strings.TrimSpace(recovery.Summary)
	for _, obs := range recovery.MetricObservations {
		fragment.State[strings.TrimSpace(obs.MetricKey)] = makeRecoveredMetricValue(obs, sourceIDs, now)
	}
	for key, metric := range deriveRecoveredRiskMetrics(fragment.State, sourceIDs, now) {
		if current, ok := fragment.State[key]; !ok || current.Value == 0 || metric.Value > current.Value {
			fragment.State[key] = metric
		}
	}
	applyRecoveredRiskQualifiers(fragment.Qualifiers, fragment.State, recovery)
	fragment.Metadata.LastUpdatedAt = now
}

func deriveRecoveredRiskMetrics(metrics map[string]horizonmodels.MetricValue, sourceIDs []string, now time.Time) map[string]horizonmodels.MetricValue {
	out := map[string]horizonmodels.MetricValue{}
	lead := metricValue(metrics, "lead_time_days")
	if lead == 0 {
		lead = metricValue(metrics, "lead_time_days_baseline")
	}
	customs := metricValue(metrics, "customs_clearance_days")
	inventoryCover := metricValue(metrics, "inventory_cover_days_at_destination")
	if inventoryCover == 0 {
		inventoryCover = metricValue(metrics, "inventory_days_baseline")
	}
	switchTime := metricValue(metrics, "switch_time_days")
	share := metricValue(metrics, "share")
	technicalFit := metricValue(metrics, "technical_fit")
	regulatoryFit := metricValue(metrics, "regulatory_fit")
	technicalSub := metricValue(metrics, "technical_substitutability")
	regulatorySub := metricValue(metrics, "regulatory_substitutability")
	serviceCriticality := metricValue(metrics, "service_criticality")
	fragility := clamp01(lead/45.0 + customs/20.0 + share*0.20)
	if inventoryCover > 0 && lead > 0 && inventoryCover < lead {
		fragility = clamp01(fragility + (lead-inventoryCover)/maxFloat(lead, 1)*0.45)
	}
	reroute := 0.55
	if switchTime > 0 {
		reroute = clamp01(1 - switchTime/90.0)
	}
	if technicalSub > 0 {
		reroute = clamp01((reroute + technicalSub) / 2)
	}
	if regulatorySub > 0 {
		reroute = clamp01((reroute + regulatorySub) / 2)
	}
	if technicalFit > 0 {
		reroute = clamp01((reroute + technicalFit) / 2)
	}
	if fragility > 0 {
		out["route_fragility"] = makeRecoveredDerivedMetric(fragility, sourceIDs, now)
	}
	out["route_rerouteability"] = makeRecoveredDerivedMetric(reroute, sourceIDs, now)
	criticality := clamp01(fragility*0.34 + (1-reroute)*0.21 + clamp01(share)*0.20 + clamp01(1-technicalFit)*0.10 + clamp01(1-regulatoryFit)*0.10 + clamp01(serviceCriticality)*0.05)
	if criticality > 0 {
		out["supply_chain_criticality"] = makeRecoveredDerivedMetric(criticality, sourceIDs, now)
	}
	return out
}

func applyRecoveredRiskQualifiers(qualifiers map[string]interface{}, metrics map[string]horizonmodels.MetricValue, recovery MetricRecoveryEstimate) {
	if qualifiers == nil {
		return
	}
	scores := map[string]float64{}
	if lead := metricValue(metrics, "lead_time_days"); lead > 0 {
		scores["inventory_vulnerability"] = clamp01(lead / 30.0)
	}
	if customs := metricValue(metrics, "customs_clearance_days"); customs > 0 {
		scores["customs_friction"] = clamp01(customs / 14.0)
	}
	if fragility := metricValue(metrics, "route_fragility"); fragility > 0 {
		scores["route_fragility"] = clamp01(fragility)
	}
	if service := metricValue(metrics, "service_criticality"); service > 0 {
		scores["service_dependency"] = clamp01(service)
	}
	if regFit := metricValue(metrics, "regulatory_fit"); regFit > 0 {
		scores["control_exposure"] = clamp01(1 - regFit)
	}
	if techSub := metricValue(metrics, "technical_substitutability"); techSub > 0 {
		scores["low_substitutability"] = clamp01(1 - techSub)
	}
	mechanisms := make([]string, 0, len(scores))
	for key := range scores {
		mechanisms = append(mechanisms, key)
	}
	sort.Strings(mechanisms)
	primary := ""
	primaryScore := 0.0
	for _, key := range mechanisms {
		if scores[key] > primaryScore {
			primary = key
			primaryScore = scores[key]
		}
	}
	qualifiers["riskMechanisms"] = mechanisms
	qualifiers["riskMechanismScores"] = scores
	qualifiers["primaryRiskMechanism"] = primary
	qualifiers["primaryRiskLabel"] = humanizeRefLabel(primary)
	qualifiers["criticalSummary"] = strings.TrimSpace(recovery.Summary)
	qualifiers["riskCoverageStatus"] = computeCoverageStatus(metrics)
	qualifiers["riskGateStatus"] = computeGateStatus(scores, stringQualifier(qualifiers, "riskCoverageStatus"))
	for _, obs := range recovery.MetricObservations {
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
	if routeFragility, ok := metrics["route_fragility"]; ok {
		qualifiers["routeFragility"] = routeFragility.Value
	}
	if routeRerouteability, ok := metrics["route_rerouteability"]; ok {
		qualifiers["routeRerouteability"] = routeRerouteability.Value
	}
}

func makeRecoveredMetricValue(obs StressMetricObservation, sourceIDs []string, observedAt time.Time) horizonmodels.MetricValue {
	return horizonmodels.MetricValue{
		Value:      obs.Value,
		Unit:       stringPtrIfNotEmpty(obs.Unit),
		Confidence: clamp01(firstNonZero(obs.Confidence, 0.62)),
		ObservedAt: observedAt,
		Provenance: horizonmodels.Provenance{
			Method:     "recover_metrics",
			SourceIDs:  append([]string(nil), sourceIDs...),
			ObservedAt: &observedAt,
		},
	}
}

func makeRecoveredDerivedMetric(value float64, sourceIDs []string, observedAt time.Time) horizonmodels.MetricValue {
	return horizonmodels.MetricValue{
		Value:      value,
		Confidence: 0.7,
		ObservedAt: observedAt,
		Provenance: horizonmodels.Provenance{
			Method:     "recover_metrics",
			SourceIDs:  append([]string(nil), sourceIDs...),
			ObservedAt: &observedAt,
		},
	}
}

func findMetricRecoveryArtifact(scopeArtifacts []*horizonmodels.MaterializedArtifact, artifactByKey map[string]*horizonmodels.MaterializedArtifact, recovery MetricRecoveryEstimate) (*horizonmodels.MaterializedArtifact, bool) {
	if key := strings.TrimPrefix(strings.TrimSpace(recovery.ArtifactKey), "/"); key != "" {
		if artifact, ok := artifactByKey[key]; ok && artifact != nil {
			return artifact, true
		}
	}
	match, _ := exactArtifactGapForSupplyLink(scopeArtifacts, recovery.SupplyLinkRef, recovery.SupplierRef, recovery.CustomerRef, recovery.ProductRef)
	if match != nil && match.Artifact != nil {
		return match.Artifact, true
	}
	for _, artifact := range scopeArtifacts {
		if artifact == nil {
			continue
		}
		if containsString(collectSupplyLinkRefsFromArtifact(artifact), recovery.SupplyLinkRef) {
			return artifact, true
		}
	}
	return nil, false
}

func (c *RecoverMetricsCanvas) applyRecoveryToArtifact(ctx context.Context, tenantID string, artifact *horizonmodels.MaterializedArtifact, recovery MetricRecoveryEstimate, sourceIDs []string, now time.Time) (*horizonmodels.MaterializedArtifact, error) {
	if artifact == nil {
		return nil, nil
	}
	copyArtifact := *artifact
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(copyArtifact.Payload)
	if err != nil || parsed == nil {
		return nil, nil
	}
	bindingEngine := pathbinding.NewEngine(c.fragmentsRepo, c.edgesRepo)
	stressEval := pathstress.NewEvaluator()
	matched := false
	for idx := range parsed.Legs {
		leg := &parsed.Legs[idx]
		if !metricRecoveryMatchesLeg(*leg, recovery, strings.TrimPrefix(strings.TrimSpace(copyArtifact.CanonicalKey), "/")) {
			continue
		}
		matched = true
		if recovery.ProductRef != "" {
			leg.ProductRef = firstNonEmptyString(strings.TrimSpace(recovery.ProductRef), leg.ProductRef)
		}
		if len(recovery.TransportViaRefs) > 0 {
			leg.TransportViaRefs = uniqueCanonicalRefs(append(leg.TransportViaRefs, recovery.TransportViaRefs...))
		}
		leg.Mode = firstNonEmptyString(strings.TrimSpace(recovery.Mode), leg.Mode)
		leg.VesselType = firstNonEmptyString(strings.TrimSpace(recovery.VesselType), leg.VesselType)
		leg.SegmentConfidence = maxFloat(leg.SegmentConfidence, recovery.Confidence)
		binding := bindingEngine.BindSegment(ctx, pathbinding.SegmentInput{
			SupplierRef:       leg.SupplierRef,
			CustomerRef:       leg.CustomerRef,
			ProductRef:        leg.ProductRef,
			TransportViaRefs:  leg.TransportViaRefs,
			SegmentConfidence: maxFloat(leg.SegmentConfidence, recovery.Confidence),
		})
		leg.Mode = firstNonEmptyString(leg.Mode, binding.Mode)
		leg.GeoRefs = uniqueCanonicalRefs(append(leg.GeoRefs, binding.GeoRefs...))
		leg.FlowRefs = uniqueCanonicalRefs(append(leg.FlowRefs, binding.FlowRefs...))
		leg.AlternateFlowRefs = uniqueCanonicalRefs(append(leg.AlternateFlowRefs, binding.AlternateFlowRefs...))
		leg.ZoneRefs = uniqueCanonicalRefs(append(leg.ZoneRefs, binding.ZoneRefs...))
		leg.AffectedZoneRefs = uniqueCanonicalRefs(append(leg.AffectedZoneRefs, binding.AffectedZoneRefs...))
		leg.SpatialSemantics = uniqueStrings(append(leg.SpatialSemantics, binding.SpatialSemantics...))
		leg.BindingConfidence = maxFloat(maxFloat(leg.BindingConfidence, binding.BindingConfidence), recovery.Confidence)
		leg.Stress = stressEval.EvaluateLeg(*leg)
	}
	for idx := range parsed.Edges {
		edge := &parsed.Edges[idx]
		if strings.TrimSpace(edge.SupplyLinkCanonicalPath) != strings.TrimSpace(recovery.SupplyLinkRef) {
			continue
		}
		matched = true
		if edge.Metrics == nil {
			edge.Metrics = map[string]horizonmodels.MetricValue{}
		}
		if edge.Qualifiers == nil {
			edge.Qualifiers = map[string]interface{}{}
		}
		if recovery.ProductRef != "" {
			edge.ProductRef = firstNonEmptyString(strings.TrimSpace(recovery.ProductRef), edge.ProductRef)
		}
		for _, obs := range recovery.MetricObservations {
			edge.Metrics[strings.TrimSpace(obs.MetricKey)] = makeRecoveredMetricValue(obs, sourceIDs, now)
		}
		if len(recovery.TransportViaRefs) > 0 {
			edge.Qualifiers[horizonmodels.QualifierTransportVia] = uniqueCanonicalRefs(recovery.TransportViaRefs)
		}
		if recovery.Mode != "" {
			edge.Qualifiers[horizonmodels.QualifierRouteMode] = strings.TrimSpace(recovery.Mode)
		}
		if recovery.VesselType != "" {
			edge.Qualifiers["vesselType"] = strings.TrimSpace(recovery.VesselType)
		}
		edge.Qualifiers["metricRecoverySource"] = firstNonEmptyString(recovery.Source, "llm_grounded")
		edge.Qualifiers["metricRecoverySourceTier"] = firstNonEmptyString(recovery.SourceTier, "secondary")
		edge.Qualifiers["metricRecoveryAsOf"] = normalizedAsOfDate(recovery.AsOfDate)
		edge.Qualifiers["metricRecoveryConfidence"] = recovery.Confidence
		edge.Qualifiers["metricRecoverySummary"] = strings.TrimSpace(recovery.Summary)
		applyRecoveredRiskQualifiers(edge.Qualifiers, edge.Metrics, recovery)
	}
	if !matched {
		return nil, nil
	}
	parsed.Stress = stressEval.EvaluatePath(parsed.Legs)
	if parsed.Notes == nil {
		parsed.Notes = map[string]interface{}{}
	}
	parsed.Notes["recoverMetricsUpdatedAt"] = now.Format(time.RFC3339Nano)
	copyArtifact.Payload = horizonmodels.BuildSupplyChainPathPayloadDetailed(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Family, parsed.Notes)
	horizonmodels.PopulateMaterializedArtifactIndexes(&copyArtifact)
	summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
	copyArtifact.LegCount = summary.LegCount
	copyArtifact.BindingScore = summary.BindingConfidence
	copyArtifact.PathStressScore = summary.PathStressScore
	copyArtifact.BottleneckLeg = summary.BottleneckLegIndex
	helper := &IdentifyStressesCanvas{
		analyticalPrimitivesRepo: c.analyticalPrimitivesRepo,
	}
	_, _ = helper.persistObservationArtifacts(ctx, tenantID, &copyArtifact, StressFinding{ProductRef: recovery.ProductRef}, sourceIDs, now)
	return &copyArtifact, nil
}

func metricRecoveryMatchesLeg(leg horizonmodels.SupplyChainPathLeg, recovery MetricRecoveryEstimate, artifactKey string) bool {
	if recovery.LegIndex != nil && leg.Index == *recovery.LegIndex {
		return true
	}
	if recovery.ArtifactKey != "" && strings.TrimPrefix(strings.TrimSpace(recovery.ArtifactKey), "/") == strings.TrimPrefix(strings.TrimSpace(artifactKey), "/") {
		if strings.TrimSpace(leg.SupplyLinkCanonicalPath) == strings.TrimSpace(recovery.SupplyLinkRef) {
			return true
		}
	}
	if strings.TrimSpace(leg.SupplyLinkCanonicalPath) == strings.TrimSpace(recovery.SupplyLinkRef) {
		return true
	}
	return false
}

func extractRecoverySupplyLinks(recoveries []MetricRecoveryEstimate) []string {
	out := make([]string, 0, len(recoveries))
	for _, recovery := range recoveries {
		if strings.TrimSpace(recovery.SupplyLinkRef) != "" {
			out = append(out, strings.TrimSpace(recovery.SupplyLinkRef))
		}
	}
	return uniqueCanonicalRefs(out)
}

func recoveryEvidenceURLs(recovery MetricRecoveryEstimate) []string {
	out := make([]string, 0, len(recovery.Evidence))
	for _, ev := range recovery.Evidence {
		if strings.TrimSpace(ev.URL) != "" {
			out = append(out, strings.TrimSpace(ev.URL))
		}
	}
	return uniqueStrings(out)
}

func relationIDsFromSlice(items []IdentifyStressVisibleRelation) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.EdgeID) != "" {
			out = append(out, strings.TrimSpace(item.EdgeID))
		}
	}
	return out
}

func splitCommaRefs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return uniqueCanonicalRefs(out)
}

func parseFloatLoose(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	fields := strings.Fields(raw)
	if len(fields) > 0 {
		raw = fields[0]
	}
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}

func intPtrString(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func (c *RecoverMetricsCanvas) publish(ctx context.Context, eventType string, tenantID string, payload interface{}) error {
	if c == nil || c.publisher == nil {
		return nil
	}
	return c.publisher(ctx, eventType, tenantID, payload)
}

func (c *RecoverMetricsCanvas) publishNodeUpsert(ctx context.Context, tenantID string, fragment *horizonmodels.HorizonFragment, operation string) error {
	helper := &IdentifyStressesCanvas{publisher: c.publisher}
	return helper.publishNodeUpsert(ctx, tenantID, fragment, operation)
}

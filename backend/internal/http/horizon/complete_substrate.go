package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbinding"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathstress"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

const (
	defaultCompleteSubstrateMaxCompletions = 80
	completeSubstrateBatchSize             = 10
)

type CompleteSubstrateRequest struct {
	ScopeMode             string                          `json:"scopeMode,omitempty"`
	SelectedCanonicalPath string                          `json:"selectedCanonicalPath,omitempty"`
	VisibleNodeRefs       []string                        `json:"visibleNodeRefs,omitempty"`
	VisibleArtifactKeys   []string                        `json:"visibleArtifactKeys,omitempty"`
	VisibleSupplyLinkRefs []string                        `json:"visibleSupplyLinkRefs,omitempty"`
	VisibleRelations      []IdentifyStressVisibleRelation `json:"visibleRelations,omitempty"`
	ClientViewID          string                          `json:"clientViewId,omitempty"`
	MaxNodes              int                             `json:"maxNodes,omitempty"`
	MaxArtifacts          int                             `json:"maxArtifacts,omitempty"`
	MaxCompletions        int                             `json:"maxCompletions,omitempty"`
	AsOfDate              string                          `json:"asOfDate,omitempty"`
	UseLLM                *bool                           `json:"useLlm,omitempty"`
}

type CompleteSubstrateResponse struct {
	JobID         string                       `json:"jobId"`
	AcceptedScope IdentifyStressesScopeSummary `json:"acceptedScope"`
	StartedAt     string                       `json:"startedAt"`
	StreamChannel string                       `json:"streamChannel"`
}

type CompleteSubstrateDebugResult struct {
	ScopeSummary        IdentifyStressesScopeSummary `json:"scopeSummary"`
	AcceptedNodeRefs    []string                     `json:"acceptedNodeRefs,omitempty"`
	AcceptedArtifacts   []string                     `json:"acceptedArtifactKeys,omitempty"`
	AcceptedSupplyLinks []string                     `json:"acceptedSupplyLinks,omitempty"`
	Gaps                []SubstrateGap               `json:"gaps,omitempty"`
	GroundedDossier     string                       `json:"groundedDossier,omitempty"`
	GroundingEvidence   []StressEvidence             `json:"groundingEvidence,omitempty"`
	Completions         []SubstrateCompletion        `json:"completions,omitempty"`
}

type CompleteSubstrateCanvas struct {
	exec             *ai.AITaskExecutor
	fragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	edgesRepo        *horizonrepo.DependencyEdgesRepository
	materializedRepo *horizonrepo.MaterializedArtifactsRepository
	lensesRepo       *horizonrepo.LensesRepository
	publisher        func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	logger           *log.Logger
}

type CompleteSubstrateCanvasConfig struct {
	Exec             *ai.AITaskExecutor
	FragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	EdgesRepo        *horizonrepo.DependencyEdgesRepository
	MaterializedRepo *horizonrepo.MaterializedArtifactsRepository
	LensesRepo       *horizonrepo.LensesRepository
	Publisher        func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	Logger           *log.Logger
}

type SubstrateGap struct {
	SupplyLinkRef      string   `json:"supplyLinkRef"`
	SupplierRef        string   `json:"supplierRef,omitempty"`
	CustomerRef        string   `json:"customerRef,omitempty"`
	ProductRef         string   `json:"productRef,omitempty"`
	GapType            string   `json:"gapType"`
	ExistingArtifact   string   `json:"existingArtifactKey,omitempty"`
	ExistingMatchKind  string   `json:"existingMatchKind,omitempty"`
	ContextualArtifact string   `json:"contextualArtifactKey,omitempty"`
	MissingFields      []string `json:"missingFields,omitempty"`
	TransportViaRefs   []string `json:"transportViaRefs,omitempty"`
	ExistingMode       string   `json:"existingMode,omitempty"`
	ExistingVessel     string   `json:"existingVesselType,omitempty"`
	SourceConfidence   float64  `json:"sourceConfidence,omitempty"`
	ExistingLegIndex   *int     `json:"existingLegIndex,omitempty"`
	ExistingPathScore  float64  `json:"existingPathScore,omitempty"`
}

type SubstrateCompletion struct {
	ID               string           `json:"id,omitempty"`
	Source           string           `json:"source,omitempty"`
	GapType          string           `json:"gapType,omitempty"`
	SupplyLinkRef    string           `json:"supplyLinkRef"`
	SupplierRef      string           `json:"supplierRef,omitempty"`
	CustomerRef      string           `json:"customerRef,omitempty"`
	ProductRef       string           `json:"productRef,omitempty"`
	TransportViaRefs []string         `json:"transportViaRefs,omitempty"`
	Mode             string           `json:"mode,omitempty"`
	VesselType       string           `json:"vesselType,omitempty"`
	Confidence       float64          `json:"confidence,omitempty"`
	AsOfDate         string           `json:"asOfDate,omitempty"`
	Summary          string           `json:"summary,omitempty"`
	Evidence         []StressEvidence `json:"evidence,omitempty"`
}

type completeSubstrateBatchResult struct {
	ArtifactsCreated   int
	ArtifactsUpdated   int
	ScopeArtifactsUped int
	CompletedLinks     []string
	ArtifactKeys       []string
}

const (
	substrateMatchExactSupplyLinkProduct = "exact_supply_link_product"
	substrateMatchExactEndpointProduct   = "exact_endpoint_product"
	substrateMatchContextualSurrogate    = "contextual_surrogate"
)

type substrateArtifactMatch struct {
	Artifact      *horizonmodels.MaterializedArtifact
	LegIndex      *int
	MissingFields []string
	MatchKind     string
	Score         int
}

func NewCompleteSubstrateCanvas(cfg CompleteSubstrateCanvasConfig) *CompleteSubstrateCanvas {
	return &CompleteSubstrateCanvas{
		exec:             cfg.Exec,
		fragmentsRepo:    cfg.FragmentsRepo,
		edgesRepo:        cfg.EdgesRepo,
		materializedRepo: cfg.MaterializedRepo,
		lensesRepo:       cfg.LensesRepo,
		publisher:        cfg.Publisher,
		logger:           cfg.Logger,
	}
}

func (h *HorizonHandlers) CompleteSubstrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.completeSubstrateCanvas == nil {
		http.Error(w, "Complete substrate canvas not configured", http.StatusServiceUnavailable)
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
	var req CompleteSubstrateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	jobID := "substrate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	resp, err := h.completeSubstrateCanvas.Run(r.Context(), authInfo.TenantID, lens, req, jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

func (c *CompleteSubstrateCanvas) DebugComplete(ctx context.Context, tenantID string, lens *horizonmodels.Lens, req CompleteSubstrateRequest) (*CompleteSubstrateDebugResult, error) {
	scope, err := c.resolveScope(ctx, lens, req)
	if err != nil {
		return nil, err
	}
	gaps, err := c.collectGaps(ctx, scope)
	if err != nil {
		return nil, err
	}
	deterministic := c.buildDeterministicCompletions(gaps, req.AsOfDate)
	grounded := ""
	evidence := []StressEvidence(nil)
	completions := append([]SubstrateCompletion(nil), deterministic...)
	useLLM := req.UseLLM == nil || *req.UseLLM
	if useLLM && len(gaps) > 0 && c.exec != nil {
		if text, citations, err := c.runGroundedCompletion(ctx, tenantID, lens, scope, gaps, req); err == nil {
			grounded = text
			evidence = citations
			completions = c.mergeSubstrateCompletions(deterministic, parseSubstrateCompletionDossier(text, req.AsOfDate))
		} else if c.logger != nil {
			c.logger.Printf("complete-substrate: grounded completion warning: %v", err)
		}
	}
	maxCompletions := req.MaxCompletions
	if maxCompletions <= 0 {
		maxCompletions = defaultCompleteSubstrateMaxCompletions
	}
	if len(completions) > maxCompletions {
		completions = completions[:maxCompletions]
	}
	if c.logger != nil {
		c.logger.Printf(
			"complete-substrate: debug scope tenant=%s lens=%s selected=%s nodes=%d artifacts=%d supplyLinks=%d relations=%d gaps=%d completions=%d useLLM=%t",
			strings.TrimSpace(tenantID),
			strings.TrimSpace(lens.ID),
			strings.TrimSpace(scope.SelectedCanonicalPath),
			len(scope.NodeRefs),
			len(scope.ArtifactKeys),
			len(scope.SupplyLinkRefs),
			len(scope.VisibleRelations),
			len(gaps),
			len(completions),
			useLLM,
		)
	}
	return &CompleteSubstrateDebugResult{
		ScopeSummary: IdentifyStressesScopeSummary{
			ScopeMode:             firstNonEmptyString(strings.TrimSpace(req.ScopeMode), "visible_set"),
			SelectedCanonicalPath: scope.SelectedCanonicalPath,
			VisibleNodes:          len(uniqueCanonicalRefs(req.VisibleNodeRefs)),
			AcceptedNodes:         len(scope.NodeRefs),
			VisibleArtifacts:      len(uniqueCanonicalRefs(req.VisibleArtifactKeys)),
			AcceptedArtifacts:     len(scope.ArtifactKeys),
			AcceptedSupplyLinks:   len(scope.SupplyLinkRefs),
			VisibleRelations:      len(req.VisibleRelations),
			AcceptedRelations:     len(scope.VisibleRelations),
		},
		AcceptedNodeRefs:    append([]string(nil), scope.NodeRefs...),
		AcceptedArtifacts:   append([]string(nil), scope.ArtifactKeys...),
		AcceptedSupplyLinks: append([]string(nil), scope.SupplyLinkRefs...),
		Gaps:                gaps,
		GroundedDossier:     grounded,
		GroundingEvidence:   evidence,
		Completions:         completions,
	}, nil
}

func (c *CompleteSubstrateCanvas) Run(ctx context.Context, tenantID string, lens *horizonmodels.Lens, req CompleteSubstrateRequest, jobID string) (*CompleteSubstrateResponse, error) {
	if lens == nil {
		return nil, fmt.Errorf("lens is required")
	}
	if jobID == "" {
		jobID = "substrate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	debug, err := c.DebugComplete(ctx, tenantID, lens, req)
	if err != nil {
		return nil, err
	}
	if c.logger != nil {
		c.logger.Printf(
			"complete-substrate: run started tenant=%s lens=%s job=%s acceptedNodes=%d acceptedArtifacts=%d acceptedSupplyLinks=%d acceptedRelations=%d completions=%d",
			strings.TrimSpace(tenantID),
			strings.TrimSpace(lens.ID),
			strings.TrimSpace(jobID),
			debug.ScopeSummary.AcceptedNodes,
			debug.ScopeSummary.AcceptedArtifacts,
			debug.ScopeSummary.AcceptedSupplyLinks,
			debug.ScopeSummary.AcceptedRelations,
			len(debug.Completions),
		)
	}
	if c.publisher != nil {
		_ = c.publisher(ctx, "horizon.complete_substrate.started", tenantID, map[string]interface{}{
			"jobId":         jobID,
			"lensId":        strings.TrimSpace(lens.ID),
			"acceptedScope": debug.ScopeSummary,
		})
	}
	batchResult := &completeSubstrateBatchResult{}
	for start := 0; start < len(debug.Completions); start += completeSubstrateBatchSize {
		end := minInt(start+completeSubstrateBatchSize, len(debug.Completions))
		res, err := c.applyCompletionBatch(ctx, lens, debug.Completions[start:end])
		if err != nil {
			if c.logger != nil {
				c.logger.Printf(
					"complete-substrate: run failed tenant=%s lens=%s job=%s batchStart=%d batchEnd=%d err=%v",
					strings.TrimSpace(tenantID),
					strings.TrimSpace(lens.ID),
					strings.TrimSpace(jobID),
					start,
					end,
					err,
				)
			}
			return nil, err
		}
		batchResult.ArtifactsCreated += res.ArtifactsCreated
		batchResult.ArtifactsUpdated += res.ArtifactsUpdated
		batchResult.ScopeArtifactsUped += res.ScopeArtifactsUped
		batchResult.CompletedLinks = append(batchResult.CompletedLinks, res.CompletedLinks...)
		batchResult.ArtifactKeys = append(batchResult.ArtifactKeys, res.ArtifactKeys...)
		if c.logger != nil {
			c.logger.Printf(
				"complete-substrate: batch applied tenant=%s lens=%s job=%s batchStart=%d batchEnd=%d artifactsCreated=%d artifactsUpdated=%d completedLinks=%v artifactKeys=%v",
				strings.TrimSpace(tenantID),
				strings.TrimSpace(lens.ID),
				strings.TrimSpace(jobID),
				start,
				end,
				res.ArtifactsCreated,
				res.ArtifactsUpdated,
				uniqueStrings(res.CompletedLinks),
				uniqueStrings(res.ArtifactKeys),
			)
		}
		if c.publisher != nil {
			_ = c.publisher(ctx, "horizon.complete_substrate.batch_applied", tenantID, map[string]interface{}{
				"jobId":            jobID,
				"lensId":           strings.TrimSpace(lens.ID),
				"artifactsCreated": res.ArtifactsCreated,
				"artifactsUpdated": res.ArtifactsUpdated,
				"completedLinks":   res.CompletedLinks,
				"artifactKeys":     res.ArtifactKeys,
			})
		}
	}
	if c.publisher != nil {
		_ = c.publisher(ctx, "horizon.complete_substrate.completed", tenantID, map[string]interface{}{
			"jobId":            jobID,
			"lensId":           strings.TrimSpace(lens.ID),
			"artifactsCreated": batchResult.ArtifactsCreated,
			"artifactsUpdated": batchResult.ArtifactsUpdated,
			"completedLinks":   uniqueStrings(batchResult.CompletedLinks),
			"artifactKeys":     uniqueStrings(batchResult.ArtifactKeys),
		})
	}
	if c.logger != nil {
		c.logger.Printf(
			"complete-substrate: run completed tenant=%s lens=%s job=%s artifactsCreated=%d artifactsUpdated=%d completedLinks=%v artifactKeys=%v",
			strings.TrimSpace(tenantID),
			strings.TrimSpace(lens.ID),
			strings.TrimSpace(jobID),
			batchResult.ArtifactsCreated,
			batchResult.ArtifactsUpdated,
			uniqueStrings(batchResult.CompletedLinks),
			uniqueStrings(batchResult.ArtifactKeys),
		)
	}
	return &CompleteSubstrateResponse{
		JobID:         jobID,
		AcceptedScope: debug.ScopeSummary,
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		StreamChannel: "realtime",
	}, nil
}

func (c *CompleteSubstrateCanvas) resolveScope(ctx context.Context, lens *horizonmodels.Lens, req CompleteSubstrateRequest) (*identifyStressScope, error) {
	scope := &identifyStressScope{
		SelectedCanonicalPath: strings.TrimSpace(req.SelectedCanonicalPath),
		VisibleRelations:      normalizeVisibleRelations(req.VisibleRelations),
	}

	nodeRefsSet := map[string]struct{}{}
	pushNodeRef := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "/artifact/") || strings.HasPrefix(ref, "/query/") || strings.HasPrefix(ref, "/domain/") {
			return
		}
		nodeRefsSet[ref] = struct{}{}
	}
	pushNodeRef(scope.SelectedCanonicalPath)
	for _, ref := range uniqueCanonicalRefs(req.VisibleNodeRefs) {
		pushNodeRef(ref)
	}
	for _, relation := range scope.VisibleRelations {
		pushNodeRef(relation.SourceCanonicalPath)
		pushNodeRef(relation.TargetCanonicalPath)
	}

	artifactKeys := uniqueCanonicalRefs(req.VisibleArtifactKeys)
	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(artifactKeys))
	seenArtifactKeys := map[string]struct{}{}
	for _, artKey := range artifactKeys {
		key := strings.TrimSpace(artKey)
		if key == "" || c.materializedRepo == nil {
			continue
		}
		var artifact *horizonmodels.MaterializedArtifact
		tenantCandidates := uniqueStrings([]string{
			strings.TrimSpace(firstNonEmptyString(lens.TenantID, "")),
			"global",
		})
		for _, tenantCandidate := range tenantCandidates {
			found, err := c.materializedRepo.GetByCanonicalKeyAnyKind(ctx, tenantCandidate, key)
			if err == nil && found != nil {
				artifact = found
				break
			}
		}
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		normKey := strings.TrimSpace(artifact.CanonicalKey)
		if _, ok := seenArtifactKeys[normKey]; ok {
			continue
		}
		seenArtifactKeys[normKey] = struct{}{}
		artifacts = append(artifacts, artifact)
		for _, ref := range collectArtifactNodeRefs(artifact) {
			pushNodeRef(ref)
		}
	}
	if req.MaxArtifacts > 0 && len(artifacts) > req.MaxArtifacts {
		artifacts = artifacts[:req.MaxArtifacts]
	}
	scope.Artifacts = artifacts
	scope.ArtifactKeys = make([]string, 0, len(artifacts))
	supplyLinkRefsSet := map[string]struct{}{}
	pushSupplyLinkRef := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, "/supply-link/") {
			return
		}
		supplyLinkRefsSet[ref] = struct{}{}
	}
	for _, ref := range uniqueCanonicalRefs(req.VisibleSupplyLinkRefs) {
		pushSupplyLinkRef(ref)
	}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		scope.ArtifactKeys = append(scope.ArtifactKeys, strings.TrimSpace(artifact.CanonicalKey))
		for _, ref := range artifact.GeoRefs {
			scope.GeoRefs = append(scope.GeoRefs, strings.TrimSpace(ref))
		}
		for _, ref := range artifact.ZoneRefs {
			scope.ZoneRefs = append(scope.ZoneRefs, strings.TrimSpace(ref))
		}
		for _, ref := range artifact.AffectedZoneRefs {
			scope.AffectedZoneRefs = append(scope.AffectedZoneRefs, strings.TrimSpace(ref))
		}
		for _, ref := range collectSupplyLinkRefsFromArtifact(artifact) {
			pushSupplyLinkRef(ref)
		}
	}

	nodeRefs := make([]string, 0, len(nodeRefsSet))
	for ref := range nodeRefsSet {
		nodeRefs = append(nodeRefs, ref)
	}
	sort.Strings(nodeRefs)
	if req.MaxNodes > 0 && len(nodeRefs) > req.MaxNodes {
		nodeRefs = nodeRefs[:req.MaxNodes]
	}
	scope.NodeRefs = nodeRefs
	scope.SupplyLinkRefs = make([]string, 0, len(supplyLinkRefsSet))
	for ref := range supplyLinkRefsSet {
		scope.SupplyLinkRefs = append(scope.SupplyLinkRefs, ref)
	}
	sort.Strings(scope.SupplyLinkRefs)
	scope.GeoRefs = uniqueCanonicalRefs(scope.GeoRefs)
	scope.ZoneRefs = uniqueCanonicalRefs(scope.ZoneRefs)
	scope.AffectedZoneRefs = uniqueCanonicalRefs(scope.AffectedZoneRefs)

	if c.fragmentsRepo != nil && len(scope.NodeRefs) > 0 {
		if fragments, err := c.fragmentsRepo.ListByCanonicalPaths(ctx, scope.NodeRefs); err == nil {
			scope.NodeFragments = fragments
		}
	}
	return scope, nil
}

func (c *CompleteSubstrateCanvas) collectGaps(ctx context.Context, scope *identifyStressScope) ([]SubstrateGap, error) {
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
	out := make([]SubstrateGap, 0, len(scope.SupplyLinkRefs))
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
		switch {
		case existing == nil:
			out = append(out, SubstrateGap{
				SupplyLinkRef:      ref,
				SupplierRef:        supplierRef,
				CustomerRef:        customerRef,
				ProductRef:         productRef,
				GapType:            "missing_materialized_path",
				ContextualArtifact: substrateArtifactKey(contextual),
				TransportViaRefs:   uniqueCanonicalRefs(append([]string(nil), frag.TransportViaRefs...)),
				SourceConfidence:   metricValue(frag.State, "confidence"),
			})
		case len(existing.MissingFields) > 0:
			out = append(out, SubstrateGap{
				SupplyLinkRef:      ref,
				SupplierRef:        supplierRef,
				CustomerRef:        customerRef,
				ProductRef:         productRef,
				GapType:            "missing_operational_detail",
				ExistingArtifact:   strings.TrimSpace(existing.Artifact.CanonicalKey),
				ExistingMatchKind:  existing.MatchKind,
				ContextualArtifact: substrateArtifactKey(contextual),
				MissingFields:      existing.MissingFields,
				TransportViaRefs:   uniqueCanonicalRefs(append([]string(nil), frag.TransportViaRefs...)),
				ExistingLegIndex:   existing.LegIndex,
				ExistingPathScore:  existing.Artifact.PathStressScore,
				SourceConfidence:   metricValue(frag.State, "confidence"),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].GapType != out[j].GapType {
			return out[i].GapType < out[j].GapType
		}
		return out[i].SupplyLinkRef < out[j].SupplyLinkRef
	})
	return out, nil
}

func (c *CompleteSubstrateCanvas) buildDeterministicCompletions(gaps []SubstrateGap, asOfDate string) []SubstrateCompletion {
	out := make([]SubstrateCompletion, 0, len(gaps))
	dateToken := normalizedAsOfDate(asOfDate)
	for _, gap := range gaps {
		out = append(out, SubstrateCompletion{
			ID:               "deterministic:" + strings.Trim(gap.SupplyLinkRef, "/"),
			Source:           "graph",
			GapType:          gap.GapType,
			SupplyLinkRef:    gap.SupplyLinkRef,
			SupplierRef:      gap.SupplierRef,
			CustomerRef:      gap.CustomerRef,
			ProductRef:       gap.ProductRef,
			TransportViaRefs: append([]string(nil), gap.TransportViaRefs...),
			Confidence:       clamp01(completeSubstrateFirstNonZero(gap.SourceConfidence, 0.72)),
			AsOfDate:         dateToken,
			Summary:          deterministicCompletionSummary(gap),
		})
	}
	return out
}

func (c *CompleteSubstrateCanvas) runGroundedCompletion(ctx context.Context, tenantID string, lens *horizonmodels.Lens, scope *identifyStressScope, gaps []SubstrateGap, req CompleteSubstrateRequest) (string, []StressEvidence, error) {
	prompt := c.buildGroundedCompletionPrompt(lens, scope, gaps, req.AsOfDate)
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
		UseCase:   "horizon.complete_substrate",
		CascadeID: strings.TrimSpace(lens.ID),
	}
	res, err := c.exec.ExecuteTask(ctx, task)
	if err != nil {
		return "", nil, fmt.Errorf("complete-substrate grounded completion: %w", err)
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

func (c *CompleteSubstrateCanvas) buildGroundedCompletionPrompt(lens *horizonmodels.Lens, scope *identifyStressScope, gaps []SubstrateGap, asOfDate string) string {
	selected := strings.TrimSpace(scope.SelectedCanonicalPath)
	if selected == "" {
		selected = "(none)"
	}
	dateToken := normalizedAsOfDate(asOfDate)
	allowedViaRefs := make([]string, 0, len(scope.NodeFragments))
	for _, frag := range scope.NodeFragments {
		if frag == nil {
			continue
		}
		cp := strings.TrimSpace(frag.CanonicalPath)
		if strings.HasPrefix(cp, "/fac/") {
			allowedViaRefs = append(allowedViaRefs, cp)
		}
	}
	allowedViaRefs = uniqueCanonicalRefs(allowedViaRefs)

	var b strings.Builder
	fmt.Fprintf(&b, "You complete operational supply-chain materialization for a Horizon lens as of %s.\n", dateToken)
	fmt.Fprintf(&b, "Lens: %s (%s)\n", strings.TrimSpace(lens.Name), strings.TrimSpace(lens.ID))
	fmt.Fprintf(&b, "Selected anchor: %s\n", selected)
	b.WriteString("You MUST NOT invent new objects. Only use canonical refs already present in the graph slice below.\n")
	b.WriteString("You MAY refine transport legs, ports, route mode, vessel type, and facility concretization for known supply links.\n")
	b.WriteString("Only materialize direct operational relationships already explicit in the provided objects, supply links, artifacts, and visible relations.\n")
	b.WriteString("Prefer exact supplyLinkRef + productRef first, then exact supplier/customer endpoint + productRef. Use contextual surrogates only as weak hints, never as direct bindings.\n")
	b.WriteString("You MUST ONLY return completions for supplyLinkRef values explicitly listed in the gap list below. Do NOT invent new supplyLinkRef values and do NOT create new abstract graph relationships.\n")
	b.WriteString("Do NOT build long indirect chains. Do NOT bridge entities that are merely co-present in the lens. Do NOT connect objects through multi-hop ecosystem reasoning.\n")
	b.WriteString("If a route detail, port, mode, vessel, or facility is not specifically supported by the provided graph slice or grounded evidence as of the date above, leave that field empty.\n")
	b.WriteString("Never output a completion for a relationship that is only probable, broad, ecosystem-level, or inferred through distant contextual associations.\n")
	b.WriteString("Treat the graph slice below as the complete allowed substrate for this pass: visible lens scope, visible supply-path artifacts, and distant map relations.\n")
	b.WriteString("Return plain text only. No markdown tables. No code fences. Use exactly this block format for each completion:\n")
	b.WriteString("COMPLETION <n>\n")
	b.WriteString("supplyLinkRef: <canonical supply link ref>\n")
	b.WriteString("supplierRef: <canonical ref or empty>\n")
	b.WriteString("customerRef: <canonical ref or empty>\n")
	b.WriteString("productRef: <canonical ref or empty>\n")
	b.WriteString("transportViaRefs: <comma separated canonical refs or empty>\n")
	b.WriteString("mode: <sea|rail|road|air|pipeline_gas|pipeline_oil|pipeline_h2|intermodal|unknown>\n")
	b.WriteString("vesselType: <short token or empty>\n")
	b.WriteString("confidence: <0..1 number>\n")
	b.WriteString("asOfDate: <YYYY-MM-DD>\n")
	b.WriteString("summary: <one sentence>\n")
	b.WriteString("evidence:\n")
	b.WriteString("- <url>\n\n")

	b.WriteString("Allowed facility refs for transportViaRefs:\n")
	for _, ref := range allowedViaRefs {
		fmt.Fprintf(&b, "- %s\n", ref)
	}
	b.WriteString("\nAllowed canonical node refs already present in the graph slice:\n")
	for _, ref := range scope.NodeRefs {
		fmt.Fprintf(&b, "- %s\n", ref)
	}
	b.WriteString("\nExisting materialized supply-path artifacts already visible on the map:\n")
	for i, artifact := range scope.Artifacts {
		if i >= 40 {
			break
		}
		if artifact == nil {
			continue
		}
		fmt.Fprintf(&b, "- artifactKey=%s supplier=%s customer=%s product=%s pathStress=%.2f legs=%d\n",
			strings.TrimSpace(artifact.CanonicalKey),
			firstNonEmptyString(strings.TrimSpace(artifact.AnchorSupplier), "(unknown)"),
			firstNonEmptyString(strings.TrimSpace(artifact.AnchorCustomer), "(unknown)"),
			firstNonEmptyString(strings.TrimSpace(artifact.ProductAxis), "(unknown)"),
			artifact.PathStressScore,
			artifact.LegCount,
		)
	}
	b.WriteString("\nKnown supply-link gaps to complete:\n")
	for i, gap := range gaps {
		fmt.Fprintf(&b, "%d. supplyLinkRef=%s gapType=%s supplier=%s customer=%s product=%s existingArtifact=%s existingMatchKind=%s contextualArtifact=%s missingFields=%s knownTransportVia=%s\n",
			i+1,
			gap.SupplyLinkRef,
			gap.GapType,
			firstNonEmptyString(gap.SupplierRef, "(unknown)"),
			firstNonEmptyString(gap.CustomerRef, "(unknown)"),
			firstNonEmptyString(gap.ProductRef, "(unknown)"),
			firstNonEmptyString(gap.ExistingArtifact, "(none)"),
			firstNonEmptyString(gap.ExistingMatchKind, "(none)"),
			firstNonEmptyString(gap.ContextualArtifact, "(none)"),
			strings.Join(gap.MissingFields, ","),
			strings.Join(gap.TransportViaRefs, ","),
		)
	}
	b.WriteString("\nVisible graph relations:\n")
	for i, rel := range scope.VisibleRelations {
		if i >= 80 {
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

func parseSubstrateCompletionDossier(text string, asOfDate string) []SubstrateCompletion {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	blocks := splitCompletionBlocks(text)
	out := make([]SubstrateCompletion, 0, len(blocks))
	for _, block := range blocks {
		completion := SubstrateCompletion{
			Source:   "llm_grounded",
			AsOfDate: normalizedAsOfDate(asOfDate),
		}
		evidence := []StressEvidence{}
		lines := strings.Split(block, "\n")
		inEvidence := false
		for _, rawLine := range lines {
			line := strings.TrimSpace(rawLine)
			if line == "" {
				continue
			}
			if strings.HasPrefix(strings.ToUpper(line), "COMPLETION ") {
				continue
			}
			if line == "evidence:" {
				inEvidence = true
				continue
			}
			if inEvidence {
				if strings.HasPrefix(line, "-") {
					urlText := strings.TrimSpace(strings.TrimPrefix(line, "-"))
					if urlText != "" {
						evidence = append(evidence, StressEvidence{URL: urlText, Domain: stressEvidenceDomain(urlText)})
					}
				}
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(strings.ToLower(key))
			value = strings.TrimSpace(value)
			switch key {
			case "supplylinkref":
				completion.SupplyLinkRef = value
			case "supplierref":
				completion.SupplierRef = value
			case "customerref":
				completion.CustomerRef = value
			case "productref":
				completion.ProductRef = value
			case "transportviarefs":
				completion.TransportViaRefs = uniqueCanonicalRefs(splitCSVRefs(value))
			case "mode":
				completion.Mode = value
			case "vesseltype":
				completion.VesselType = value
			case "confidence":
				if v, err := parseSubstrateFloat(value); err == nil {
					completion.Confidence = clamp01(v)
				}
			case "asofdate":
				completion.AsOfDate = normalizedAsOfDate(value)
			case "summary":
				completion.Summary = value
			}
		}
		completion.Evidence = evidence
		if strings.TrimSpace(completion.SupplyLinkRef) == "" {
			continue
		}
		if completion.Confidence <= 0 {
			completion.Confidence = 0.78
		}
		completion.ID = "llm:" + strings.Trim(completion.SupplyLinkRef, "/")
		out = append(out, completion)
	}
	return out
}

func (c *CompleteSubstrateCanvas) mergeSubstrateCompletions(base []SubstrateCompletion, inferred []SubstrateCompletion) []SubstrateCompletion {
	merged := map[string]SubstrateCompletion{}
	for _, item := range base {
		if strings.TrimSpace(item.SupplyLinkRef) == "" {
			continue
		}
		merged[item.SupplyLinkRef] = item
	}
	for _, item := range inferred {
		key := strings.TrimSpace(item.SupplyLinkRef)
		if key == "" {
			continue
		}
		current, ok := merged[key]
		if !ok {
			continue
		}
		current.Source = firstNonEmptyString(item.Source, current.Source)
		current.GapType = firstNonEmptyString(item.GapType, current.GapType)
		current.SupplierRef = firstNonEmptyString(item.SupplierRef, current.SupplierRef)
		current.CustomerRef = firstNonEmptyString(item.CustomerRef, current.CustomerRef)
		current.ProductRef = firstNonEmptyString(item.ProductRef, current.ProductRef)
		if len(item.TransportViaRefs) > 0 {
			current.TransportViaRefs = uniqueCanonicalRefs(item.TransportViaRefs)
		}
		current.Mode = firstNonEmptyString(item.Mode, current.Mode)
		current.VesselType = firstNonEmptyString(item.VesselType, current.VesselType)
		if item.Confidence > current.Confidence {
			current.Confidence = item.Confidence
		}
		current.AsOfDate = firstNonEmptyString(item.AsOfDate, current.AsOfDate)
		current.Summary = firstNonEmptyString(item.Summary, current.Summary)
		if len(item.Evidence) > 0 {
			current.Evidence = item.Evidence
		}
		merged[key] = current
	}
	out := make([]SubstrateCompletion, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SupplyLinkRef < out[j].SupplyLinkRef })
	return out
}

func (c *CompleteSubstrateCanvas) applyCompletionBatch(ctx context.Context, lens *horizonmodels.Lens, completions []SubstrateCompletion) (*completeSubstrateBatchResult, error) {
	result := &completeSubstrateBatchResult{}
	for _, completion := range completions {
		if strings.TrimSpace(completion.SupplyLinkRef) == "" {
			continue
		}
		frag, err := c.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(completion.SupplyLinkRef))
		if err != nil || frag == nil {
			continue
		}
		artifact, created, err := c.materializeCompletion(ctx, lens, frag, completion)
		if err != nil {
			return result, err
		}
		if artifact == nil {
			continue
		}
		if created {
			result.ArtifactsCreated++
		} else {
			result.ArtifactsUpdated++
		}
		result.ScopeArtifactsUped++
		result.CompletedLinks = append(result.CompletedLinks, strings.TrimSpace(completion.SupplyLinkRef))
		result.ArtifactKeys = append(result.ArtifactKeys, strings.TrimSpace(artifact.CanonicalKey))
	}
	result.CompletedLinks = uniqueStrings(result.CompletedLinks)
	result.ArtifactKeys = uniqueStrings(result.ArtifactKeys)
	return result, nil
}

func (c *CompleteSubstrateCanvas) materializeCompletion(ctx context.Context, lens *horizonmodels.Lens, fragment *horizonmodels.HorizonFragment, completion SubstrateCompletion) (*horizonmodels.MaterializedArtifact, bool, error) {
	if fragment == nil || c.materializedRepo == nil || lens == nil {
		return nil, false, nil
	}
	baseTenant := firstNonEmptyString(strings.TrimSpace(fragment.TenantID), strings.TrimSpace(lens.TenantID), "global")
	productRef := firstNonEmptyString(completion.ProductRef, completeSubstrateDerefString(fragment.ProductRef))
	supplierRef := firstNonEmptyString(completion.SupplierRef, completeSubstrateDerefString(fragment.SupplierRef))
	customerRef := firstNonEmptyString(completion.CustomerRef, completeSubstrateDerefString(fragment.CustomerRef))
	if supplierRef == "" || customerRef == "" || productRef == "" {
		return nil, false, nil
	}
	base := buildSingleSupplyLinkArtifact(fragment, baseTenant, "")
	if base == nil {
		return nil, false, nil
	}
	base.ProductAxis = productRef
	base.AnchorSupplier = supplierRef
	base.AnchorCustomer = customerRef
	base.CanonicalKey = buildIdentifyStructuralPathKey(baseTenant, supplierRef, productRef, customerRef)
	if strings.TrimSpace(base.CanonicalKey) == "" {
		return nil, false, nil
	}
	var artifact *horizonmodels.MaterializedArtifact
	existing, err := c.materializedRepo.GetByCanonicalKey(ctx, baseTenant, horizonmodels.MaterializedKindSupplyChainPath, strings.TrimSpace(base.CanonicalKey))
	if err == nil && existing != nil {
		artifact = existing
	} else {
		artifact = base
	}
	applySubstrateCompletionToArtifact(artifact, completion, fragment)
	horizonmodels.PopulateMaterializedArtifactIndexes(artifact)

	created := false
	if existing == nil {
		id, err := c.materializedRepo.Create(ctx, artifact)
		if err != nil {
			return nil, false, err
		}
		stored, err := c.materializedRepo.Get(ctx, id)
		if err != nil {
			return nil, false, err
		}
		artifact = stored
		created = true
	} else {
		artifact.UpdatedAt = time.Now().UTC()
		artifact.UpdatedBy = "complete_substrate:apply"
		if err := c.materializedRepo.Update(ctx, artifact); err != nil {
			return nil, false, err
		}
	}

	shadow := &IdentifyStressesCanvas{
		fragmentsRepo:    c.fragmentsRepo,
		edgesRepo:        c.edgesRepo,
		materializedRepo: c.materializedRepo,
		lensesRepo:       c.lensesRepo,
		logger:           c.logger,
	}
	lensScoped := shadow.ensureLensScopedArtifact(ctx, lens, artifact)
	if lensScoped != nil {
		artifact = lensScoped
	}
	return artifact, created, nil
}

func applySubstrateCompletionToArtifact(artifact *horizonmodels.MaterializedArtifact, completion SubstrateCompletion, fragment *horizonmodels.HorizonFragment) {
	if artifact == nil {
		return
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return
	}
	targetProductRef := firstNonEmptyString(completion.ProductRef, completeSubstrateDerefString(fragment.ProductRef), strings.TrimSpace(artifact.ProductAxis))
	targetLinkRef := strings.TrimSpace(completion.SupplyLinkRef)
	for i := range parsed.Legs {
		leg := &parsed.Legs[i]
		if targetLinkRef != "" && strings.TrimSpace(leg.SupplyLinkCanonicalPath) != targetLinkRef {
			continue
		}
		leg.SupplierRef = firstNonEmptyString(completion.SupplierRef, leg.SupplierRef, completeSubstrateDerefString(fragment.SupplierRef))
		leg.CustomerRef = firstNonEmptyString(completion.CustomerRef, leg.CustomerRef, completeSubstrateDerefString(fragment.CustomerRef))
		leg.ProductRef = firstNonEmptyString(targetProductRef, leg.ProductRef)
		if len(completion.TransportViaRefs) > 0 {
			leg.TransportViaRefs = uniqueCanonicalRefs(completion.TransportViaRefs)
		}
		leg.Mode = firstNonEmptyString(completion.Mode, leg.Mode)
		leg.VesselType = firstNonEmptyString(completion.VesselType, leg.VesselType)
		binding := pathbinding.NewEngine(nil, nil).BindSegment(context.Background(), pathbinding.SegmentInput{
			SupplierRef:       leg.SupplierRef,
			CustomerRef:       leg.CustomerRef,
			ProductRef:        leg.ProductRef,
			TransportViaRefs:  leg.TransportViaRefs,
			SegmentConfidence: completeSubstrateFirstNonZero(leg.SegmentConfidence, completion.Confidence, metricValue(fragment.State, "confidence")),
		})
		leg.Mode = firstNonEmptyString(leg.Mode, binding.Mode)
		if len(leg.TransportViaRefs) > 0 {
			leg.TransportViaRefs = uniqueCanonicalRefs(leg.TransportViaRefs)
		}
		leg.GeoRefs = uniqueCanonicalRefs(firstNonEmptySlice(leg.GeoRefs, binding.GeoRefs))
		leg.FlowRefs = uniqueCanonicalRefs(firstNonEmptySlice(leg.FlowRefs, binding.FlowRefs))
		leg.AlternateFlowRefs = uniqueCanonicalRefs(firstNonEmptySlice(leg.AlternateFlowRefs, binding.AlternateFlowRefs))
		leg.ZoneRefs = uniqueCanonicalRefs(firstNonEmptySlice(leg.ZoneRefs, binding.ZoneRefs))
		leg.AffectedZoneRefs = uniqueCanonicalRefs(firstNonEmptySlice(leg.AffectedZoneRefs, binding.AffectedZoneRefs))
		leg.SpatialSemantics = uniqueCanonicalRefs(firstNonEmptySlice(leg.SpatialSemantics, binding.SpatialSemantics))
		leg.BindingConfidence = completeSubstrateMaxFloat(leg.BindingConfidence, binding.BindingConfidence)
		leg.SegmentConfidence = completeSubstrateMaxFloat(leg.SegmentConfidence, completion.Confidence)
		leg.Stress = pathstress.NewEvaluator().EvaluateLeg(*leg)
	}
	for i := range parsed.Edges {
		edge := &parsed.Edges[i]
		if targetLinkRef != "" && strings.TrimSpace(edge.SupplyLinkCanonicalPath) != targetLinkRef {
			continue
		}
		edge.ProductRef = firstNonEmptyString(targetProductRef, edge.ProductRef)
		if edge.Qualifiers == nil {
			edge.Qualifiers = map[string]interface{}{}
		}
		if len(completion.TransportViaRefs) > 0 {
			edge.Qualifiers[QualifierTransportVia] = uniqueCanonicalRefs(completion.TransportViaRefs)
		}
		if completion.Mode != "" {
			edge.Qualifiers[QualifierRouteMode] = completion.Mode
		}
		if completion.VesselType != "" {
			edge.Qualifiers["vesselType"] = completion.VesselType
		}
		edge.Qualifiers["substrateCompletionSource"] = completion.Source
		edge.Qualifiers["substrateCompletionAsOf"] = normalizedAsOfDate(completion.AsOfDate)
		edge.Qualifiers["substrateCompletionConfidence"] = completion.Confidence
	}
	pathStress := pathstress.NewEvaluator().EvaluatePath(parsed.Legs)
	notes := map[string]interface{}{
		"substrateCompletion": map[string]interface{}{
			"source":      firstNonEmptyString(completion.Source, "graph"),
			"asOfDate":    normalizedAsOfDate(completion.AsOfDate),
			"confidence":  completion.Confidence,
			"summary":     strings.TrimSpace(completion.Summary),
			"evidence":    evidenceURLs(completion.Evidence),
			"provisional": true,
		},
	}
	artifact.Payload = horizonmodels.BuildSupplyChainPathPayloadDetailed(parsed.Nodes, parsed.Edges, parsed.Legs, pathStress, parsed.Sufficiency, parsed.Family, notes)
	artifact.PathStatus = firstNonEmptyString(artifact.PathStatus, "hypothesis")
	artifact.DisplayName = firstNonEmptyString(artifact.DisplayName, buildIdentifyArtifactDisplayName(artifact.AnchorSupplier, artifact.ProductAxis, artifact.AnchorCustomer))
	artifact.Labels = mergeStringMap(artifact.Labels, map[string]string{
		"supplier": bestEffortRefLabel(firstNonEmptyString(completion.SupplierRef, artifact.AnchorSupplier)),
		"product":  bestEffortRefLabel(firstNonEmptyString(targetProductRef, artifact.ProductAxis)),
		"customer": bestEffortRefLabel(firstNonEmptyString(completion.CustomerRef, artifact.AnchorCustomer)),
	})
	artifact.UpdatedBy = "complete_substrate:apply"
	if strings.TrimSpace(artifact.CreatedBy) == "" {
		artifact.CreatedBy = "complete_substrate:apply"
	}
}

func exactArtifactGapForSupplyLink(artifacts []*horizonmodels.MaterializedArtifact, supplyLinkRef, supplierRef, customerRef, productRef string) (*substrateArtifactMatch, *substrateArtifactMatch) {
	var primary *substrateArtifactMatch
	var contextual *substrateArtifactMatch
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		artifactSupplier := strings.TrimSpace(firstNonEmptyString(artifact.AnchorSupplier, inferPathEndpoint(parsed.Nodes, "supplier")))
		artifactCustomer := strings.TrimSpace(firstNonEmptyString(artifact.AnchorCustomer, inferPathEndpoint(parsed.Nodes, "customer")))
		artifactProduct := strings.TrimSpace(artifact.ProductAxis)
		for _, leg := range parsed.Legs {
			match := classifySubstrateArtifactMatch(artifact, leg, supplyLinkRef, supplierRef, customerRef, productRef, artifactSupplier, artifactCustomer, artifactProduct)
			if match == nil {
				continue
			}
			switch match.MatchKind {
			case substrateMatchExactSupplyLinkProduct, substrateMatchExactEndpointProduct:
				if primary == nil || match.Score > primary.Score {
					primary = match
				}
			case substrateMatchContextualSurrogate:
				if contextual == nil || match.Score > contextual.Score {
					contextual = match
				}
			}
		}
	}
	return primary, contextual
}

func classifySubstrateArtifactMatch(
	artifact *horizonmodels.MaterializedArtifact,
	leg horizonmodels.SupplyChainPathLeg,
	supplyLinkRef, supplierRef, customerRef, productRef, artifactSupplier, artifactCustomer, artifactProduct string,
) *substrateArtifactMatch {
	linkRef := strings.TrimSpace(supplyLinkRef)
	wantSupplier := strings.TrimSpace(supplierRef)
	wantCustomer := strings.TrimSpace(customerRef)
	wantProduct := strings.TrimSpace(productRef)

	legSupplyLink := strings.TrimSpace(leg.SupplyLinkCanonicalPath)
	legSupplier := strings.TrimSpace(firstNonEmptyString(leg.SupplierRef, artifactSupplier))
	legCustomer := strings.TrimSpace(firstNonEmptyString(leg.CustomerRef, artifactCustomer))
	legProduct := strings.TrimSpace(firstNonEmptyString(leg.ProductRef, artifactProduct))

	exactLink := linkRef != "" && legSupplyLink == linkRef
	exactProduct := wantProduct != "" && legProduct == wantProduct
	exactEndpoints := refsEqualOrEmpty(wantSupplier, legSupplier) && refsEqualOrEmpty(wantCustomer, legCustomer)
	sameFamily := sameProductFamily(wantProduct, legProduct)

	matchKind := ""
	score := 0
	switch {
	case exactLink && exactProduct:
		matchKind = substrateMatchExactSupplyLinkProduct
		score = 300
		if exactEndpoints {
			score += 30
		}
	case exactEndpoints && exactProduct:
		matchKind = substrateMatchExactEndpointProduct
		score = 200
		if exactLink {
			score += 10
		}
	case sameFamily && (exactEndpoints || exactLink):
		matchKind = substrateMatchContextualSurrogate
		score = 100
		if exactEndpoints {
			score += 20
		}
		if exactLink {
			score += 10
		}
	default:
		return nil
	}

	missing := []string{}
	if len(leg.TransportViaRefs) == 0 {
		missing = append(missing, "transportViaRefs")
	}
	if strings.TrimSpace(leg.Mode) == "" {
		missing = append(missing, "mode")
	}
	if strings.TrimSpace(leg.VesselType) == "" && strings.TrimSpace(leg.Mode) == "sea" {
		missing = append(missing, "vesselType")
	}
	legIndex := leg.Index
	score -= len(missing) * 2

	return &substrateArtifactMatch{
		Artifact:      artifact,
		LegIndex:      &legIndex,
		MissingFields: missing,
		MatchKind:     matchKind,
		Score:         score,
	}
}

func substrateArtifactKey(match *substrateArtifactMatch) string {
	if match == nil || match.Artifact == nil {
		return ""
	}
	return strings.TrimSpace(match.Artifact.CanonicalKey)
}

func refsEqualOrEmpty(want, got string) bool {
	want = strings.TrimSpace(want)
	got = strings.TrimSpace(got)
	return want == "" || got == "" || want == got
}

func sameProductFamily(left, right string) bool {
	left = productFamilyRef(left)
	right = productFamilyRef(right)
	return left != "" && right != "" && left == right
}

func productFamilyRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "/product/") {
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) < 3 || parts[0] != "product" {
		return ""
	}
	limit := minInt(len(parts), 3)
	return "/" + strings.Join(parts[:limit], "/")
}

func inferPathEndpoint(nodes []horizonmodels.SupplyChainPathNode, role string) string {
	if len(nodes) == 0 {
		return ""
	}
	switch role {
	case "supplier":
		for _, node := range nodes {
			if node.Class == ah.FragmentClassProduct {
				continue
			}
			if ref := strings.TrimSpace(node.Ref); ref != "" {
				return ref
			}
		}
	case "customer":
		for idx := len(nodes) - 1; idx >= 0; idx-- {
			node := nodes[idx]
			if node.Class == ah.FragmentClassProduct {
				continue
			}
			if ref := strings.TrimSpace(node.Ref); ref != "" {
				return ref
			}
		}
	}
	return ""
}

func splitCompletionBlocks(text string) []string {
	lines := strings.Split(text, "\n")
	out := []string{}
	current := []string{}
	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, strings.Join(current, "\n"))
		current = nil
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(strings.ToUpper(line), "COMPLETION ") {
			flush()
		}
		if line != "" {
			current = append(current, line)
		}
	}
	flush()
	return out
}

func parseSubstrateFloat(raw string) (float64, error) {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	return parseFloat(raw)
}

func normalizedAsOfDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC().Format("2006-01-02")
	}
	if parsed := parseOptionalTime(raw); parsed != nil {
		return parsed.UTC().Format("2006-01-02")
	}
	if len(raw) >= 10 {
		return raw[:10]
	}
	return raw
}

func deterministicCompletionSummary(gap SubstrateGap) string {
	switch gap.GapType {
	case "missing_materialized_path":
		return "Materialize a supply-chain path directly from the known supply-link graph substrate."
	case "missing_operational_detail":
		return "Complete operational fields on the known supply-chain path without changing the underlying graph relationship."
	default:
		return "Complete operational materialization for the known supply link."
	}
}

func splitCSVRefs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func evidenceURLs(items []StressEvidence) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if urlText := strings.TrimSpace(item.URL); urlText != "" {
			out = append(out, urlText)
		}
	}
	return uniqueStrings(out)
}

func mergeStringMap(left map[string]string, right map[string]string) map[string]string {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		if strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

func completeSubstrateDerefString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return strings.TrimSpace(*ptr)
}

func firstNonEmptySlice(left []string, right []string) []string {
	if len(left) > 0 {
		return append([]string(nil), left...)
	}
	return append([]string(nil), right...)
}

func completeSubstrateFirstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func completeSubstrateMaxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func stressEvidenceDomain(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Hostname())
}

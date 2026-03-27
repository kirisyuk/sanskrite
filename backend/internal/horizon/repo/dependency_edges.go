package repo

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/graphenrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/regulatorytransform"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/tracking"
)

var ErrEdgeNotFound = errors.New("dependency edge not found")

// =============================================================================
// Context hooks (best-effort)
// =============================================================================
//
// These hooks allow higher-level pipelines (e.g. Scout) to collect "affected" fragment IDs
// while creating edges, without extra Firestore reads or additional plumbing across many call sites.
// This is intentionally best-effort and must never change persistence semantics.

type edgeAffectCollectorKey struct{}

// WithEdgeAffectCollector attaches a collector to ctx. Repository methods may call it after successful writes.
// The collector is called with FINAL (possibly canonicalized/swapped) edge endpoints.
func WithEdgeAffectCollector(ctx context.Context, collector func(sourceFragmentID, targetFragmentID string)) context.Context {
	if ctx == nil || collector == nil {
		return ctx
	}
	return context.WithValue(ctx, edgeAffectCollectorKey{}, collector)
}

func getEdgeAffectCollector(ctx context.Context) func(string, string) {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(edgeAffectCollectorKey{}); v != nil {
		if fn, ok := v.(func(string, string)); ok {
			return fn
		}
	}
	return nil
}

// DependencyEdgesRepository репозиторий для работы с DependencyEdge
type DependencyEdgesRepository struct {
	db                          *firestore.Client
	col                         *firestore.CollectionRef
	validationPolicy            *policy.PolicyExecutor      // Optional: for edge validation
	fragmentsRepo               *HorizonFragmentsRepository // Optional: for fragment type lookup
	telemetryRepo               *HorizonTelemetryRepository // Optional: aggregated edge policy KPIs
	onUpsert                    func(ctx context.Context, edge *models.DependencyEdge)
	storage                     *gcp.GCSClient
	bucket                      string
	useGCS                      bool
	uploadLimiter               *rate.Limiter
	vertexSyncBatcher           *VertexSyncBatcher
	vertexSearchDelete          func(ctx context.Context, edgeID string) error
	productDictionary           *productdict.Runtime
	regulatoryTransformRegistry *regulatorytransform.Registry
}

type EdgeUpdateOptions struct {
	SkipOnUpsert bool
}

// NewDependencyEdgesRepository создает новый репозиторий
func NewDependencyEdgesRepository(db *firestore.Client) *DependencyEdgesRepository {
	return &DependencyEdgesRepository{
		db:  db,
		col: db.Collection("horizon_dependency_edges"),
	}
}

// WithValidationPolicy sets the validation policy executor.
func (r *DependencyEdgesRepository) WithValidationPolicy(executor *policy.PolicyExecutor) *DependencyEdgesRepository {
	r.validationPolicy = executor
	return r
}

// WithFragmentsRepo sets the fragments repository for type lookup.
func (r *DependencyEdgesRepository) WithFragmentsRepo(repo *HorizonFragmentsRepository) *DependencyEdgesRepository {
	r.fragmentsRepo = repo
	return r
}

// WithTelemetryRepo sets the unified telemetry sink used for edge policy KPI counters.
func (r *DependencyEdgesRepository) WithTelemetryRepo(repo *HorizonTelemetryRepository) *DependencyEdgesRepository {
	r.telemetryRepo = repo
	return r
}

// WithOnUpsert registers a best-effort callback after successful edge create/update writes.
func (r *DependencyEdgesRepository) WithOnUpsert(fn func(ctx context.Context, edge *models.DependencyEdge)) *DependencyEdgesRepository {
	if fn == nil {
		return r
	}
	if r.onUpsert == nil {
		r.onUpsert = fn
		return r
	}
	prev := r.onUpsert
	r.onUpsert = func(ctx context.Context, edge *models.DependencyEdge) {
		prev(ctx, edge)
		fn(ctx, edge)
	}
	return r
}

func (r *DependencyEdgesRepository) WithStorage(storage *gcp.GCSClient, bucket string) *DependencyEdgesRepository {
	r.storage = storage
	r.bucket = bucket
	r.useGCS = storage != nil && strings.TrimSpace(bucket) != ""
	if r.useGCS {
		r.uploadLimiter = rate.NewLimiter(rate.Limit(10), 10)
	}
	return r
}

func (r *DependencyEdgesRepository) WithVertexSyncBatcher(batcher *VertexSyncBatcher) *DependencyEdgesRepository {
	r.vertexSyncBatcher = batcher
	return r
}

func (r *DependencyEdgesRepository) WithVertexSearchDelete(fn func(ctx context.Context, edgeID string) error) *DependencyEdgesRepository {
	r.vertexSearchDelete = fn
	return r
}

func (r *DependencyEdgesRepository) WithProductDictionary(dict *productdict.Runtime) *DependencyEdgesRepository {
	r.productDictionary = dict
	return r
}

func (r *DependencyEdgesRepository) WithRegulatoryTransformRegistry(reg *regulatorytransform.Registry) *DependencyEdgesRepository {
	if r == nil {
		return r
	}
	r.regulatoryTransformRegistry = reg
	return r
}

func (r *DependencyEdgesRepository) resolveEndpointTypesAndPaths(ctx context.Context, edge *models.DependencyEdge) (sourceType, targetType, sourcePath, targetPath string) {
	if r == nil || r.fragmentsRepo == nil || edge == nil {
		return "", "", "", ""
	}
	if sourceFrag, err := r.fragmentsRepo.Get(ctx, edge.SourceFragmentID); err == nil && sourceFrag != nil {
		sourceType = string(sourceFrag.Type)
		sourcePath = sourceFrag.CanonicalPath
	}
	if targetFrag, err := r.fragmentsRepo.Get(ctx, edge.TargetFragmentID); err == nil && targetFrag != nil {
		targetType = string(targetFrag.Type)
		targetPath = targetFrag.CanonicalPath
	}
	srcTypeActual, _ := edgesemantics.ReconcileTypeWithCanonicalPath(models.FragmentType(sourceType), sourcePath)
	dstTypeActual, _ := edgesemantics.ReconcileTypeWithCanonicalPath(models.FragmentType(targetType), targetPath)
	return string(srcTypeActual), string(dstTypeActual), sourcePath, targetPath
}

func (r *DependencyEdgesRepository) applyProductRelationDictionary(
	ctx context.Context,
	edge *models.DependencyEdge,
	sourceType string,
	targetType string,
	sourcePath string,
	targetPath string,
) (string, string, bool) {
	if r == nil || r.productDictionary == nil || edge == nil {
		return sourcePath, targetPath, false
	}
	if models.FragmentType(strings.TrimSpace(sourceType)) != models.FragmentTypeProduct ||
		models.FragmentType(strings.TrimSpace(targetType)) != models.FragmentTypeProduct {
		return sourcePath, targetPath, false
	}
	resolution, err := r.productDictionary.ResolveProductRelationForTenant(
		ctx,
		firstNonEmptyString(strings.TrimSpace(edge.TenantID), "global"),
		sourcePath,
		targetPath,
	)
	if err != nil || !resolution.Known {
		if err != nil {
			log.Printf("[DependencyEdgesRepository] WARNING: product relation dictionary lookup failed edge=%s type=%s err=%v", strings.TrimSpace(edge.ID), string(edge.Type), err)
		}
		return sourcePath, targetPath, false
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	edge.Qualifiers["productRelationKind"] = resolution.RelationKind
	if resolution.Swapped {
		edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
		swapProductCanonicalPathQualifiers(edge.Qualifiers)
		edge.Qualifiers["normalizedDirection"] = true
	}
	if edge.Type != resolution.EdgeType {
		prevType := edge.Type
		edge.Type = resolution.EdgeType
		edge.Qualifiers["normalizedByProductRelationDictionary"] = true
		edge.Qualifiers["normalizedFromEdgeType"] = string(prevType)
		edge.Qualifiers["normalizedEdgeType"] = string(resolution.EdgeType)
	} else if resolution.Swapped {
		edge.Qualifiers["normalizedByProductRelationDictionary"] = true
	}
	return resolution.FromPath, resolution.ToPath, resolution.Swapped
}

func swapProductCanonicalPathQualifiers(qualifiers map[string]interface{}) {
	if qualifiers == nil {
		return
	}
	sourcePath, sourceOK := qualifiers["sourceCanonicalPath"]
	targetPath, targetOK := qualifiers["targetCanonicalPath"]
	if sourceOK || targetOK {
		qualifiers["sourceCanonicalPath"] = targetPath
		qualifiers["targetCanonicalPath"] = sourcePath
	}
}

func (r *DependencyEdgesRepository) applyRegulatoryTransformDictionary(edge *models.DependencyEdge, sourcePath, targetPath string) {
	if r == nil || r.regulatoryTransformRegistry == nil || edge == nil {
		return
	}
	sp := strings.TrimSpace(sourcePath)
	tp := strings.TrimSpace(targetPath)
	if sp == "" || tp == "" {
		return
	}
	normalized, changed := r.regulatoryTransformRegistry.NormalizeRegulatoryEdgeType(sp, tp, edge.Type)
	if !changed || normalized == "" || normalized == edge.Type {
		return
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	prev := edge.Type
	edge.Type = normalized
	edge.Qualifiers["normalizedByRegulatoryTransformDictionary"] = true
	edge.Qualifiers["normalizedFromEdgeType"] = string(prev)
	edge.Qualifiers["normalizedEdgeType"] = string(normalized)
}

func (r *DependencyEdgesRepository) recordUnknownProductRelationCandidateAsync(edge *models.DependencyEdge, sourceType, targetType, sourcePath, targetPath string) {
	if r == nil || r.productDictionary == nil || edge == nil {
		return
	}
	if models.FragmentType(strings.TrimSpace(sourceType)) != models.FragmentTypeProduct || models.FragmentType(strings.TrimSpace(targetType)) != models.FragmentTypeProduct {
		return
	}
	if !productdictSupportsEdgeType(edge.Type) {
		return
	}
	e := *edge
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[DependencyEdgesRepository] ERROR: transform candidate hook panic recovered: edgeId=%s panic=%v", strings.TrimSpace(e.ID), rec)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = r.productDictionary.RecordUnknownTransformCandidate(ctx, productdict.TransformObservation{
			TenantID:         firstNonEmptyString(strings.TrimSpace(e.TenantID), "global"),
			FromPath:         sourcePath,
			ToPath:           targetPath,
			ObservedEdgeType: e.Type,
			RelationHint:     relationHintForEdge(e.Type),
			SourceKind:       "edge_create",
			EdgeID:           strings.TrimSpace(e.ID),
			SourceFragmentID: strings.TrimSpace(e.SourceFragmentID),
			TargetFragmentID: strings.TrimSpace(e.TargetFragmentID),
		})
	}()
}

func relationHintForEdge(edgeType models.DependencyType) string {
	switch edgeType {
	case models.DependencyTypeAssemblesInto:
		return "assembly"
	case models.DependencyTypeUsedIn:
		return "usage"
	case models.DependencyTypeTransformsInto:
		return "transformation"
	default:
		return ""
	}
}

func productdictSupportsEdgeType(edgeType models.DependencyType) bool {
	switch edgeType {
	case models.DependencyTypeTransformsInto, models.DependencyTypeAssemblesInto, models.DependencyTypeUsedIn, models.DependencyTypeDependsOn:
		return true
	default:
		return false
	}
}

func (r *DependencyEdgesRepository) uploadToGCS(ctx context.Context, edge *models.DependencyEdge) error {
	if !r.useGCS || edge == nil {
		return nil
	}
	if r.uploadLimiter != nil {
		if err := r.uploadLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter wait failed: %w", err)
		}
	}

	sourceType, targetType, sourcePath, targetPath := r.resolveEndpointTypesAndPaths(ctx, edge)
	sourceLabel, targetLabel := r.edgeEndpointLabels(ctx, edge)
	content := edgeVectorText(edge, sourceType, targetType, sourcePath, targetPath, sourceLabel, targetLabel)

	objectPath := fmt.Sprintf("%s/horizon_edges/%s.json", strings.TrimSpace(edge.TenantID), strings.TrimSpace(edge.ID))
	gcsURI := fmt.Sprintf("gs://%s/%s", r.bucket, objectPath)

	doc := map[string]interface{}{
		"id": edge.ID,
		"content": map[string]interface{}{
			"mimeType": "text/plain",
			"rawBytes": base64.StdEncoding.EncodeToString([]byte(content)),
		},
		"structData": map[string]interface{}{
			"tenantId":         edge.TenantID,
			"type":             "horizon_edge",
			"edgeType":         string(edge.Type),
			"sourceFragmentId": edge.SourceFragmentID,
			"targetFragmentId": edge.TargetFragmentID,
			"sourceType":       sourceType,
			"targetType":       targetType,
			"sourcePath":       sourcePath,
			"targetPath":       targetPath,
			"sourceLabel":      sourceLabel,
			"targetLabel":      targetLabel,
			"edgeModality":     strings.TrimSpace(fmt.Sprint(edge.Qualifiers["edgeModality"])),
			"facet":            strings.TrimSpace(fmt.Sprint(edge.Qualifiers["facet"])),
			"policyStatus":     edge.GetPolicyStatus(),
			"criticality":      edge.Properties.Criticality,
			"amplification":    edge.Properties.Amplification,
			"qualifiers":       edge.Qualifiers,
			"metrics":          edgeMetricsToStructData(edge.Metrics),
			"updatedAt":        edge.Metadata.LastUpdatedAt.UTC().Format(time.RFC3339),
			"createdAt":        edge.Metadata.CreatedAt.UTC().Format(time.RFC3339),
			"validFrom":        edgeTimeString(edge.ValidFrom),
			"validTo":          edgeTimeString(edge.ValidTo),
			"uri":              gcsURI,
		},
	}

	jsonData, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal edge to json: %w", err)
	}

	metadata := map[string]string{
		"type":     "horizon_edge",
		"tenantId": edge.TenantID,
		"edgeType": string(edge.Type),
	}

	log.Printf("[DependencyEdgesRepository] Exporting edge %s (%s -> %s, %s) to GCS for Vertex Indexing...", edge.ID, sourcePath, targetPath, edge.Type)
	if err := r.storage.UploadBytes(ctx, r.bucket, objectPath, jsonData, "application/json", metadata); err != nil {
		return fmt.Errorf("failed to upload edge to GCS: %w", err)
	}
	if r.vertexSyncBatcher != nil {
		r.vertexSyncBatcher.Add(gcsURI)
	}
	return nil
}

func (r *DependencyEdgesRepository) edgeEndpointLabels(ctx context.Context, edge *models.DependencyEdge) (string, string) {
	if r == nil || r.fragmentsRepo == nil || edge == nil {
		return "", ""
	}
	sourceLabel := ""
	targetLabel := ""
	if sourceFrag, err := r.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.SourceFragmentID)); err == nil && sourceFrag != nil {
		sourceLabel = fragmentDisplayName(sourceFrag)
	}
	if targetFrag, err := r.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID)); err == nil && targetFrag != nil {
		targetLabel = fragmentDisplayName(targetFrag)
	}
	return sourceLabel, targetLabel
}

func edgeVectorText(edge *models.DependencyEdge, sourceType, targetType, sourcePath, targetPath, sourceLabel, targetLabel string) string {
	if edge == nil {
		return ""
	}
	parts := make([]string, 0, 16)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			parts = append(parts, v)
		}
	}
	add(strings.TrimSpace(sourceLabel))
	add(strings.TrimSpace(targetLabel))
	add(fmt.Sprintf("Dependency edge: %s", strings.TrimSpace(string(edge.Type))))
	if sourceLabel != "" || targetLabel != "" {
		add(fmt.Sprintf("Relation: %s -> %s", firstNonEmptyString(sourceLabel, sourcePath, edge.SourceFragmentID), firstNonEmptyString(targetLabel, targetPath, edge.TargetFragmentID)))
	}
	add(fmt.Sprintf("Source: %s", firstNonEmptyString(sourcePath, edge.SourceFragmentID)))
	add(fmt.Sprintf("Target: %s", firstNonEmptyString(targetPath, edge.TargetFragmentID)))
	if sourceType != "" || targetType != "" {
		add(fmt.Sprintf("Endpoint types: %s -> %s", firstNonEmptyString(sourceType, "unknown"), firstNonEmptyString(targetType, "unknown")))
	}
	if edge.Properties.Criticality > 0 || edge.Properties.Amplification > 0 {
		add(fmt.Sprintf("Edge properties: criticality %.3f, amplification %.3f", edge.Properties.Criticality, edge.Properties.Amplification))
	}
	if len(edge.Qualifiers) > 0 {
		add("Qualifiers: " + compactEdgeQualifierText(edge.Qualifiers, 16))
	}
	if len(edge.Metrics) > 0 {
		add("Metrics: " + compactEdgeMetricsText(edge.Metrics, 12))
	}
	if status := strings.TrimSpace(edge.GetPolicyStatus()); status != "" {
		add("Policy status: " + status)
	}
	return strings.Join(parts, "\n")
}

func compactEdgeQualifierText(values map[string]interface{}, limit int) string {
	if len(values) == 0 {
		return ""
	}
	if limit <= 0 {
		limit = 16
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, values[key]))
	}
	return strings.Join(parts, ", ")
}

func compactEdgeMetricsText(values map[string]models.MetricValue, limit int) string {
	if len(values) == 0 {
		return ""
	}
	if limit <= 0 {
		limit = 12
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		mv := values[key]
		if mv.Unit != nil && strings.TrimSpace(*mv.Unit) != "" {
			parts = append(parts, fmt.Sprintf("%s=%.4f %s", key, mv.Value, strings.TrimSpace(*mv.Unit)))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%.4f", key, mv.Value))
	}
	return strings.Join(parts, ", ")
}

func edgeMetricsToStructData(values map[string]models.MetricValue) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(values))
	for key, mv := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		unit := ""
		if mv.Unit != nil {
			unit = strings.TrimSpace(*mv.Unit)
		}
		out[key] = map[string]interface{}{
			"value":      mv.Value,
			"unit":       unit,
			"confidence": mv.Confidence,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func edgeTimeString(ts *time.Time) string {
	if ts == nil || ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
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

func edgeIdentityVariant(edge *models.DependencyEdge) string {
	if edge == nil {
		return ""
	}
	parts := make([]string, 0, 6)
	if assuranceType := strings.ToLower(strings.TrimSpace(fmt.Sprint(edge.Qualifiers["assurance_type"]))); assuranceType != "" {
		parts = append(parts, "assurance_type="+assuranceType)
	}
	if facet := strings.ToLower(strings.TrimSpace(fmt.Sprint(edge.Qualifiers["facet"]))); facet != "" {
		parts = append(parts, "facet="+facet)
	}
	if modality := strings.ToLower(strings.TrimSpace(fmt.Sprint(edge.Qualifiers["edgeModality"]))); modality != "" {
		parts = append(parts, "edge_modality="+modality)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func normalizedEdgeIdentity(tenantID, sourceID, targetID string, edgeType models.DependencyType, variant string) (string, string, models.DependencyType, string, string) {
	ns, nt, ntp, _ := models.NormalizeSymmetricEdge(strings.TrimSpace(sourceID), strings.TrimSpace(targetID), edgeType)
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	variant = strings.TrimSpace(variant)
	raw := tenantID + "|" + ns + "|" + nt + "|" + string(ntp) + "|" + variant
	sum := sha1.Sum([]byte(raw))
	return ns, nt, ntp, variant, "edge_" + hex.EncodeToString(sum[:10])
}

func mergeEdgeForIdempotentUpsert(dst, src *models.DependencyEdge) bool {
	if dst == nil || src == nil {
		return false
	}
	changed := false
	if dst.Qualifiers == nil && len(src.Qualifiers) > 0 {
		dst.Qualifiers = map[string]interface{}{}
	}
	for k, v := range src.Qualifiers {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		valueText := strings.TrimSpace(fmt.Sprint(v))
		if shouldOverwriteEdgeQualifierFromUpsert(key, valueText) {
			if current := strings.TrimSpace(fmt.Sprint(dst.Qualifiers[key])); current != valueText {
				dst.Qualifiers[key] = v
				changed = true
			}
			continue
		}
		if _, exists := dst.Qualifiers[key]; !exists && v != nil && valueText != "" {
			dst.Qualifiers[key] = v
			changed = true
		}
	}
	if dst.Metrics == nil && len(src.Metrics) > 0 {
		dst.Metrics = map[string]models.MetricValue{}
	}
	for key, mv := range src.Metrics {
		cur, ok := dst.Metrics[key]
		if !ok || mv.Confidence > cur.Confidence || (mv.Confidence == cur.Confidence && mv.Value != 0 && cur.Value == 0) {
			dst.Metrics[key] = mv
			changed = true
		}
	}
	if src.Properties.Criticality > dst.Properties.Criticality {
		dst.Properties.Criticality = src.Properties.Criticality
		changed = true
	}
	if src.Properties.Amplification > dst.Properties.Amplification {
		dst.Properties.Amplification = src.Properties.Amplification
		changed = true
	}
	if src.Properties.LatencyHours > 0 && (dst.Properties.LatencyHours == 0 || src.Properties.LatencyHours < dst.Properties.LatencyHours) {
		dst.Properties.LatencyHours = src.Properties.LatencyHours
		changed = true
	}
	if dst.Provenance.Method == "" && src.Provenance.Method != "" {
		dst.Provenance.Method = src.Provenance.Method
		changed = true
	}
	if len(src.Provenance.SourceIDs) > 0 {
		seen := make(map[string]struct{}, len(dst.Provenance.SourceIDs))
		for _, id := range dst.Provenance.SourceIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				seen[id] = struct{}{}
			}
		}
		for _, id := range src.Provenance.SourceIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			dst.Provenance.SourceIDs = append(dst.Provenance.SourceIDs, id)
			seen[id] = struct{}{}
			changed = true
		}
	}
	if dst.ValidFrom == nil && src.ValidFrom != nil {
		dst.ValidFrom = src.ValidFrom
		changed = true
	}
	if dst.ValidTo == nil && src.ValidTo != nil {
		dst.ValidTo = src.ValidTo
		changed = true
	}
	if changed {
		dst.Metadata.LastUpdatedAt = time.Now().UTC()
	}
	return changed
}

func shouldOverwriteEdgeQualifierFromUpsert(key string, value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	switch strings.TrimSpace(key) {
	case "sourceCanonicalPath",
		"targetCanonicalPath",
		"productRelationKind",
		"normalizedByProductRelationDictionary",
		"normalizedFromEdgeType",
		"normalizedEdgeType",
		"normalizedDirection",
		"repairSource",
		"repairSourceMode":
		return true
	default:
		return false
	}
}

func assignLoadedEdgeID(edge *models.DependencyEdge, doc *firestore.DocumentSnapshot) {
	if edge == nil || doc == nil || doc.Ref == nil {
		return
	}
	if stored := strings.TrimSpace(edge.ID); stored != "" && stored != strings.TrimSpace(doc.Ref.ID) {
		log.Printf("[DependencyEdgesRepository] WARNING: correcting stale edge id field stored=%s actual=%s type=%s", stored, strings.TrimSpace(doc.Ref.ID), strings.TrimSpace(string(edge.Type)))
	}
	edge.ID = strings.TrimSpace(doc.Ref.ID)
}

// Create создает новый DependencyEdge
func (r *DependencyEdgesRepository) Create(ctx context.Context, edge *models.DependencyEdge) (string, error) {
	logPrefix := "[DependencyEdgesRepository:Create]"

	if edge == nil {
		return "", ErrInvalidDocument
	}

	log.Printf("%s Creating edge: sourceID=%s, targetID=%s, type=%s",
		logPrefix, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
	sourceType, targetType, sourcePath, targetPath := r.resolveEndpointTypesAndPaths(ctx, edge)

	// Basic validation
	if err := edge.Validate(); err != nil {
		log.Printf("%s ERROR: basic validation failed: %v", logPrefix, err)
		return "", fmt.Errorf("validation error: %w", err)
	}
	if normalizedSourcePath, normalizedTargetPath, swapped := r.applyProductRelationDictionary(ctx, edge, sourceType, targetType, sourcePath, targetPath); normalizedSourcePath != "" || normalizedTargetPath != "" {
		sourcePath, targetPath = normalizedSourcePath, normalizedTargetPath
		if swapped {
			sourceType, targetType = targetType, sourceType
		}
	}

	// Policy-based validation if available
	if r.validationPolicy != nil {
		policyStatus := models.PolicyStatusClean
		policyReason := ""
		policyPenalty := 0.0
		policyEdgeModality := ""
		canonFromEdgeType := models.DependencyType("")
		decision := policy.PolicyDecision{Action: "allow"}
		var err error

		if sourceType != "" || sourcePath != "" {
			log.Printf("%s Source fragment type: %s, path: %s", logPrefix, sourceType, sourcePath)
		}
		if targetType != "" || targetPath != "" {
			log.Printf("%s Target fragment type: %s, path: %s", logPrefix, targetType, targetPath)
		}
		srcTypeActual, _ := edgesemantics.ReconcileTypeWithCanonicalPath(models.FragmentType(sourceType), sourcePath)
		dstTypeActual, _ := edgesemantics.ReconcileTypeWithCanonicalPath(models.FragmentType(targetType), targetPath)
		sourceType = string(srcTypeActual)
		targetType = string(dstTypeActual)

		// Canonicalize certain mis-modeled edges before running validation policy.
		// Example: PRODUCT OWNS ORGANIZATION -> ORGANIZATION PRODUCES PRODUCT (not ORG OWNS PRODUCT).
		// Use path-aware canonicalization to handle community subtypes (dynasty/tribe).
		if sourceType != "" && targetType != "" {
			reason, _ := edge.Qualifiers["reason"].(string)
			canon := policy.ApplyEdgeCanonicalizationPolicy(policy.EdgeCanonicalizationInput{
				EdgeType:          edge.Type,
				SourceType:        srcTypeActual,
				TargetType:        dstTypeActual,
				SourcePath:        sourcePath,
				TargetPath:        targetPath,
				RawRelationType:   string(edge.Type),
				Reason:            strings.TrimSpace(reason),
				Qualifiers:        edge.Qualifiers,
				OwnershipEvidence: models.HasOwnershipEvidence("", edge.Qualifiers, edge.Metrics),
			})
			if canon.Changed {
				oldType := edge.Type
				edge.Type = canon.EdgeType
				if canon.OriginalType != "" {
					canonFromEdgeType = canon.OriginalType
				} else {
					canonFromEdgeType = oldType
				}
				if canon.ShouldSwap {
					edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
					sourceType, targetType = targetType, sourceType
					sourcePath, targetPath = targetPath, sourcePath
					srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
				}
				if edge.Qualifiers == nil {
					edge.Qualifiers = map[string]interface{}{}
				}
				if edge.Type == models.DependencyTypeAssures && oldType == models.DependencyTypeAuditedBy {
					if _, ok := edge.Qualifiers["assurance_type"]; !ok {
						edge.Qualifiers["assurance_type"] = "audit"
					}
				}
				edge.Qualifiers["canonicalized"] = true
				edge.Qualifiers["canonicalizedFromEdgeType"] = string(canonFromEdgeType)
				edge.Qualifiers["canonicalizationReason"] = canon.Reason
				edge.Qualifiers["normalizedEdgeType"] = string(edge.Type)
				if canon.ShouldSwap {
					edge.Qualifiers["normalizedDirection"] = true
				}
				log.Printf("%s Canonicalized edge: %s -> %s (%s) [from %s] reason=%s",
					logPrefix, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, canonFromEdgeType, canon.Reason)
			}
			if canon.PolicyPenalty > 0 {
				policyStatus = policy.MergePolicyStatus(policyStatus, canon.PolicyStatus)
				if canon.PolicyPenalty > policyPenalty {
					policyPenalty = canon.PolicyPenalty
				}
				if strings.TrimSpace(canon.PolicyReason) != "" {
					policyReason = strings.TrimSpace(canon.PolicyReason)
				}
				if edge.Properties.Criticality > 0 && canon.ConfidenceMult > 0 && canon.ConfidenceMult < 1 {
					edge.Properties.Criticality *= canon.ConfidenceMult
				}
			}
			if canon.PolicyReason == "transition_unclassified" {
				log.Printf("%s transition_unclassified fallback applied: %s -> %s", logPrefix, canonFromEdgeType, edge.Type)
				policy.LogPolicyEvent("edge_canonicalization_policy", policy.PolicyLogStageFallback, map[string]interface{}{
					"trace_id":       "na",
					"tenant_id":      edge.TenantID,
					"job_id":         "na",
					"edge_id":        edge.ID,
					"source_type":    sourceType,
					"target_type":    targetType,
					"relation_type":  edge.Type,
					"action":         policy.ActionAllowWithPenalty,
					"policy_status":  policyStatus,
					"policy_reason":  canon.PolicyReason,
					"policy_penalty": policyPenalty,
					"decision_path":  "runtime_repo_edge_create",
					"confidence":     canon.ConfidenceMult,
				})
			}
		}

		decision, err = r.validationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "edge",
			Context: map[string]interface{}{
				"validationKind":      "edge",
				"sourceFragmentID":    edge.SourceFragmentID,
				"targetFragmentID":    edge.TargetFragmentID,
				"sourceType":          sourceType,
				"targetType":          targetType,
				"sourceCanonicalPath": sourcePath,
				"targetCanonicalPath": targetPath,
				"relationType":        string(edge.Type),
			},
			Edge: edge,
		})

		if err != nil {
			log.Printf("%s ERROR: policy validation failed: %v", logPrefix, err)
			policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageError, map[string]interface{}{
				"trace_id":       "na",
				"tenant_id":      edge.TenantID,
				"job_id":         "na",
				"edge_id":        edge.ID,
				"source_type":    sourceType,
				"target_type":    targetType,
				"relation_type":  edge.Type,
				"action":         "validate_edge",
				"policy_status":  "na",
				"policy_reason":  err.Error(),
				"policy_penalty": 0,
				"decision_path":  "runtime_repo_edge_create",
				"error_class":    "policy_execution_error",
				"confidence":     0,
			})
			return "", fmt.Errorf("policy validation error: %w", err)
		}

		log.Printf("%s Policy decision: action=%s, path=%s, confidence=%.2f",
			logPrefix, decision.Action, decision.DecisionPath, decision.Confidence)
		reason := ""
		if rv, _ := decision.Params["reason"].(string); strings.TrimSpace(rv) != "" {
			reason = strings.TrimSpace(rv)
		}
		policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageDecision, map[string]interface{}{
			"trace_id":       "na",
			"tenant_id":      edge.TenantID,
			"job_id":         "na",
			"edge_id":        edge.ID,
			"source_type":    sourceType,
			"target_type":    targetType,
			"relation_type":  edge.Type,
			"action":         decision.Action,
			"policy_status":  policyStatus,
			"policy_reason":  reason,
			"policy_penalty": policyPenalty,
			"decision_path":  decision.DecisionPath,
			"confidence":     decision.Confidence,
		})
		if modality, _ := decision.Params["edgeModality"].(string); strings.TrimSpace(modality) != "" {
			policyEdgeModality = strings.TrimSpace(modality)
		}

		policy.ApplyValidationDecisionBestEffort(logPrefix, decision, policy.ValidationDecisionHandlers{
			Reverse: func() {
				edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
				sourceType, targetType = targetType, sourceType
				sourcePath, targetPath = targetPath, sourcePath
				swapProductCanonicalPathQualifiers(edge.Qualifiers)
			},
			RewriteType: func(newType string, _ string) {
				newEdgeType := models.DependencyType(strings.TrimSpace(newType))
				if newEdgeType == "" || newEdgeType == edge.Type {
					return
				}
				canonFromEdgeType = edge.Type
				edge.Type = newEdgeType
			},
			ApplyPenalty: func(penalty float64, _ string) {
				if edge.Properties.Criticality > 0 {
					edge.Properties.Criticality *= (1.0 - penalty)
					log.Printf("%s Adjusted criticality: %.4f", logPrefix, edge.Properties.Criticality)
				}
			},
			SetPolicyStatus: func(status, reason string, penalty float64) {
				policyStatus = policy.MergePolicyStatus(policyStatus, status)
				if strings.TrimSpace(reason) != "" {
					policyReason = strings.TrimSpace(reason)
				}
				if penalty > policyPenalty {
					policyPenalty = penalty
				}
			},
		})
		if edge.Qualifiers == nil {
			edge.Qualifiers = map[string]interface{}{}
		}
		if policyEdgeModality != "" {
			if _, ok := edge.Qualifiers["edgeModality"]; !ok {
				edge.Qualifiers["edgeModality"] = policyEdgeModality
			}
		}
		edge.SetPolicyStatus(policyStatus, policyReason, policyPenalty)
		if canonFromEdgeType != "" {
			edge.SetPolicyOriginalType(canonFromEdgeType)
		}
		if edge.GetPolicyStatus() == models.PolicyStatusQuarantined {
			return "", fmt.Errorf("edge blocked by validation policy: %s", strings.TrimSpace(policyReason))
		}
		r.recordPolicyDecisionTelemetry(
			ctx,
			edge.TenantID,
			decision,
			sourceType,
			targetType,
			string(edge.Type),
			policyStatus,
			policyReason,
			canonFromEdgeType != "",
		)
	}
	r.applyRegulatoryTransformDictionary(edge, sourcePath, targetPath)
	graphenrich.ApplyEdgeHeuristics(edge, sourcePath, targetPath)

	now := time.Now().UTC()
	edge.Metadata.CreatedAt = now
	edge.Metadata.LastUpdatedAt = now

	variant := edgeIdentityVariant(edge)
	sourceID, targetID, edgeType, _, docID := normalizedEdgeIdentity(edge.TenantID, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, variant)
	edge.SourceFragmentID = sourceID
	edge.TargetFragmentID = targetID
	edge.Type = edgeType
	doc := r.col.Doc(docID)
	edge.ID = doc.ID

	log.Printf("%s Saving edge to Firestore: id=%s", logPrefix, edge.ID)
	if _, err := doc.Create(ctx, edge); err != nil {
		if status.Code(err) == codes.AlreadyExists {
			existing, gerr := r.Get(ctx, doc.ID)
			if gerr == nil && existing != nil {
				if mergeEdgeForIdempotentUpsert(existing, edge) {
					if uerr := r.Update(ctx, existing); uerr != nil {
						log.Printf("%s ERROR: failed to merge existing edge %s: %v", logPrefix, existing.ID, uerr)
						return "", uerr
					}
				}
				return strings.TrimSpace(existing.ID), nil
			}
		}
		log.Printf("%s ERROR: failed to save edge: %v", logPrefix, err)
		return "", err
	}
	tracking.RecordEdgeCreated(ctx, edge.ID)
	if r.useGCS {
		if err := r.uploadToGCS(ctx, edge); err != nil {
			log.Printf("%s WARNING: GCS upload failed for edge=%s: %v", logPrefix, edge.ID, err)
		}
	}

	// Best-effort: report affected endpoints to caller (if collector attached to ctx).
	if fn := getEdgeAffectCollector(ctx); fn != nil {
		fn(edge.SourceFragmentID, edge.TargetFragmentID)
	}
	r.recordUnknownProductRelationCandidateAsync(edge, sourceType, targetType, sourcePath, targetPath)
	r.runOnUpsertAsync(edge)

	log.Printf("%s Edge created successfully: id=%s", logPrefix, edge.ID)
	return edge.ID, nil
}

func (r *DependencyEdgesRepository) recordPolicyDecisionTelemetry(
	ctx context.Context,
	tenantID string,
	decision policy.PolicyDecision,
	sourceType string,
	targetType string,
	relationType string,
	finalStatus string,
	finalReason string,
	canonicalized bool,
) {
	if r == nil || r.telemetryRepo == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	snapshot := policy.BuildDecisionTelemetrySnapshot(policy.DecisionTelemetryInput{
		Kind:          "edge",
		Decision:      decision,
		SourceType:    sourceType,
		TargetType:    targetType,
		RelationType:  relationType,
		FinalStatus:   finalStatus,
		FinalReason:   finalReason,
		Canonicalized: canonicalized,
	})
	if len(snapshot.Counters) > 0 {
		_ = r.telemetryRepo.Increment(
			ctx,
			tenantID,
			"edge_policy",
			"kpi_rates",
			time.Hour,
			map[string]string{"kind": "edge"},
			snapshot.Counters,
		)
	}
	if len(snapshot.InvalidTripleDims) > 0 && len(snapshot.InvalidTripleCount) > 0 {
		_ = r.telemetryRepo.Increment(
			ctx,
			tenantID,
			"edge_policy",
			"top_invalid_triples",
			24*time.Hour,
			snapshot.InvalidTripleDims,
			snapshot.InvalidTripleCount,
		)
	}
}

// CreateMany batch-creates pre-validated edges in Firestore. Skips per-edge policy validation
// (assumes edges were already canonicalized and validated by the caller, e.g. edge_materializer).
// Respects Firestore's 500-write batch limit by chunking automatically.
func (r *DependencyEdgesRepository) CreateMany(ctx context.Context, edges []*models.DependencyEdge) ([]string, error) {
	if r == nil || r.db == nil || r.col == nil {
		return nil, ErrInvalidDocument
	}
	if len(edges) == 0 {
		return nil, nil
	}

	const batchLimit = 500
	ids := make([]string, 0, len(edges))
	now := time.Now().UTC()
	fn := getEdgeAffectCollector(ctx)

	for i := 0; i < len(edges); i += batchLimit {
		end := i + batchLimit
		if end > len(edges) {
			end = len(edges)
		}
		chunk := edges[i:end]

		batch := r.db.Batch()
		chunkByID := make(map[string]*models.DependencyEdge, len(chunk))
		for _, edge := range chunk {
			if edge == nil {
				continue
			}
			sourceType, targetType, sourcePath, targetPath := r.resolveEndpointTypesAndPaths(ctx, edge)
			if normalizedSourcePath, normalizedTargetPath, swapped := r.applyProductRelationDictionary(ctx, edge, sourceType, targetType, sourcePath, targetPath); normalizedSourcePath != "" || normalizedTargetPath != "" {
				sourcePath, targetPath = normalizedSourcePath, normalizedTargetPath
				if swapped {
					sourceType, targetType = targetType, sourceType
				}
			}
			graphenrich.ApplyEdgeHeuristics(edge, "", "")
			edge.Metadata.CreatedAt = now
			edge.Metadata.LastUpdatedAt = now
			sourceID, targetID, edgeType, _, docID := normalizedEdgeIdentity(edge.TenantID, edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, edgeIdentityVariant(edge))
			edge.SourceFragmentID = sourceID
			edge.TargetFragmentID = targetID
			edge.Type = edgeType
			edge.ID = docID
			if existing := chunkByID[docID]; existing != nil {
				mergeEdgeForIdempotentUpsert(existing, edge)
				continue
			}
			cpy := *edge
			chunkByID[docID] = &cpy
		}
		refs := make([]*firestore.DocumentRef, 0, len(chunkByID))
		keys := make([]string, 0, len(chunkByID))
		for docID := range chunkByID {
			keys = append(keys, docID)
			refs = append(refs, r.col.Doc(docID))
		}
		existingByID := map[string]*models.DependencyEdge{}
		if len(refs) > 0 {
			snaps, err := r.db.GetAll(ctx, refs)
			if err != nil {
				return ids, fmt.Errorf("batch get existing edges failed at offset %d: %w", i, err)
			}
			for _, snap := range snaps {
				if snap == nil || !snap.Exists() {
					continue
				}
				var existing models.DependencyEdge
				if err := snap.DataTo(&existing); err != nil {
					continue
				}
				if strings.TrimSpace(existing.ID) == "" {
					existing.ID = strings.TrimSpace(snap.Ref.ID)
				}
				existingByID[existing.ID] = &existing
			}
		}
		for _, docID := range keys {
			edge := chunkByID[docID]
			if edge == nil {
				continue
			}
			if existing := existingByID[docID]; existing != nil {
				mergeEdgeForIdempotentUpsert(existing, edge)
				edge = existing
			}
			ids = append(ids, docID)
			batch.Set(r.col.Doc(docID), edge)
		}
		if _, err := batch.Commit(ctx); err != nil {
			return ids, fmt.Errorf("batch commit failed at offset %d: %w", i, err)
		}
		for _, edge := range chunkByID {
			if edge != nil {
				sourceType, targetType, sourcePath, targetPath := r.resolveEndpointTypesAndPaths(ctx, edge)
				if _, ok := existingByID[edge.ID]; ok {
					tracking.RecordEdgeUpdated(ctx, edge.ID)
				} else {
					tracking.RecordEdgeCreated(ctx, edge.ID)
				}
				if r.useGCS {
					if err := r.uploadToGCS(ctx, edge); err != nil {
						log.Printf("[DependencyEdgesRepository:CreateMany] WARNING: GCS upload failed for edge=%s: %v", edge.ID, err)
					}
				}
				if fn != nil {
					fn(edge.SourceFragmentID, edge.TargetFragmentID)
				}
				if _, ok := existingByID[edge.ID]; !ok {
					r.recordUnknownProductRelationCandidateAsync(edge, sourceType, targetType, sourcePath, targetPath)
				}
				r.runOnUpsertAsync(edge)
			}
		}
	}

	return ids, nil
}

// GetMany batch-retrieves edges by IDs using Firestore's GetAll (single round-trip).
// Missing or deleted edges are silently skipped. Returns map[edgeID]*DependencyEdge.
func (r *DependencyEdgesRepository) GetMany(ctx context.Context, ids []string) (map[string]*models.DependencyEdge, error) {
	if r == nil || r.db == nil {
		return nil, ErrInvalidDocument
	}
	if len(ids) == 0 {
		return map[string]*models.DependencyEdge{}, nil
	}

	seen := map[string]struct{}{}
	refs := make([]*firestore.DocumentRef, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		refs = append(refs, r.col.Doc(id))
	}
	if len(refs) == 0 {
		return map[string]*models.DependencyEdge{}, nil
	}

	const batchSize = 100
	var snaps []*firestore.DocumentSnapshot
	for i := 0; i < len(refs); i += batchSize {
		end := i + batchSize
		if end > len(refs) {
			end = len(refs)
		}
		batch, err := r.db.GetAll(ctx, refs[i:end])
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, batch...)
	}
	out := make(map[string]*models.DependencyEdge, len(snaps))
	for _, snap := range snaps {
		if snap == nil || !snap.Exists() {
			continue
		}
		var edge models.DependencyEdge
		if err := snap.DataTo(&edge); err != nil {
			continue
		}
		assignLoadedEdgeID(&edge, snap)
		out[edge.ID] = &edge
	}
	return out, nil
}

// Get получает DependencyEdge по ID
func (r *DependencyEdgesRepository) Get(ctx context.Context, id string) (*models.DependencyEdge, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrEdgeNotFound
	}
	var edge models.DependencyEdge
	if err := snap.DataTo(&edge); err != nil {
		return nil, err
	}
	assignLoadedEdgeID(&edge, snap)
	return &edge, nil
}

// GetBySourceAndTarget получает DependencyEdge по source и target
func (r *DependencyEdgesRepository) GetBySourceAndTarget(ctx context.Context, tenantID, sourceFragmentID, targetFragmentID string) (*models.DependencyEdge, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceFragmentId", "==", sourceFragmentID).Where("targetFragmentId", "==", targetFragmentID).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrEdgeNotFound
	}
	var edge models.DependencyEdge
	if err := docs[0].DataTo(&edge); err != nil {
		return nil, err
	}
	assignLoadedEdgeID(&edge, docs[0])
	return &edge, nil
}

// GetBySourceTargetAndType получает DependencyEdge по source, target и типу
func (r *DependencyEdgesRepository) GetBySourceTargetAndType(ctx context.Context, tenantID, sourceFragmentID, targetFragmentID string, edgeType models.DependencyType) (*models.DependencyEdge, error) {
	q := r.col.Where("tenantId", "==", tenantID).
		Where("sourceFragmentId", "==", sourceFragmentID).
		Where("targetFragmentId", "==", targetFragmentID).
		Where("type", "==", edgeType).
		Limit(1)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrEdgeNotFound
	}
	var edge models.DependencyEdge
	if err := docs[0].DataTo(&edge); err != nil {
		return nil, err
	}
	assignLoadedEdgeID(&edge, docs[0])
	return &edge, nil
}

// ListBySourceTargetAndType returns all edges for exact source/target/type triple (best-effort capped).
func (r *DependencyEdgesRepository) ListBySourceTargetAndType(ctx context.Context, tenantID, sourceFragmentID, targetFragmentID string, edgeType models.DependencyType, limit int) ([]*models.DependencyEdge, error) {
	if limit <= 0 {
		limit = 50
	}
	q := r.col.Where("tenantId", "==", tenantID).
		Where("sourceFragmentId", "==", sourceFragmentID).
		Where("targetFragmentId", "==", targetFragmentID).
		Where("type", "==", edgeType).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.DependencyEdge, 0, len(docs))
	for _, d := range docs {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		assignLoadedEdgeID(&edge, d)
		out = append(out, &edge)
	}
	return out, nil
}

// ListBySource получает список исходящих DependencyEdge
func (r *DependencyEdgesRepository) ListBySource(ctx context.Context, tenantID, sourceFragmentID string, edgeType *models.DependencyType, limit int) ([]*models.DependencyEdge, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceFragmentId", "==", sourceFragmentID)
	if edgeType != nil {
		q = q.Where("type", "==", *edgeType)
	}
	q = q.OrderBy("properties.criticality", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var edges []*models.DependencyEdge
	for _, d := range docs {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		assignLoadedEdgeID(&edge, d)
		edges = append(edges, &edge)
	}
	return edges, nil
}

// ListBySourceIDsAndType returns edges for multiple exact source IDs with a single chunked Firestore preload.
// Missing sources are simply absent from the result. Intended for caller-side dedup/materialization batches.
func (r *DependencyEdgesRepository) ListBySourceIDsAndType(ctx context.Context, tenantID string, sourceFragmentIDs []string, edgeType models.DependencyType) ([]*models.DependencyEdge, error) {
	if r == nil || r.col == nil {
		return nil, ErrInvalidDocument
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(string(edgeType)) == "" || len(sourceFragmentIDs) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(sourceFragmentIDs))
	unique := make([]string, 0, len(sourceFragmentIDs))
	for _, raw := range sourceFragmentIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	const chunkSize = 30
	out := make([]*models.DependencyEdge, 0, len(unique)*2)
	for i := 0; i < len(unique); i += chunkSize {
		end := i + chunkSize
		if end > len(unique) {
			end = len(unique)
		}
		chunk := unique[i:end]
		docs, err := r.col.
			Where("tenantId", "==", tenantID).
			Where("type", "==", edgeType).
			Where("sourceFragmentId", "in", chunk).
			Documents(ctx).
			GetAll()
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			if d == nil || !d.Exists() {
				continue
			}
			var edge models.DependencyEdge
			if err := d.DataTo(&edge); err != nil {
				continue
			}
			assignLoadedEdgeID(&edge, d)
			out = append(out, &edge)
		}
	}
	return out, nil
}

// ListByTargetIDsAndType returns edges for multiple exact target IDs with a single chunked Firestore preload.
// Missing targets are simply absent from the result. Intended for caller-side reverse-adjacency batches.
func (r *DependencyEdgesRepository) ListByTargetIDsAndType(ctx context.Context, tenantID string, targetFragmentIDs []string, edgeType models.DependencyType) ([]*models.DependencyEdge, error) {
	if r == nil || r.col == nil {
		return nil, ErrInvalidDocument
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(string(edgeType)) == "" || len(targetFragmentIDs) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(targetFragmentIDs))
	unique := make([]string, 0, len(targetFragmentIDs))
	for _, raw := range targetFragmentIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	const chunkSize = 30
	out := make([]*models.DependencyEdge, 0, len(unique)*2)
	for i := 0; i < len(unique); i += chunkSize {
		end := i + chunkSize
		if end > len(unique) {
			end = len(unique)
		}
		chunk := unique[i:end]
		docs, err := r.col.
			Where("tenantId", "==", tenantID).
			Where("type", "==", edgeType).
			Where("targetFragmentId", "in", chunk).
			Documents(ctx).
			GetAll()
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			if d == nil || !d.Exists() {
				continue
			}
			var edge models.DependencyEdge
			if err := d.DataTo(&edge); err != nil {
				continue
			}
			assignLoadedEdgeID(&edge, d)
			out = append(out, &edge)
		}
	}
	return out, nil
}

// ListByTarget получает список входящих DependencyEdge
func (r *DependencyEdgesRepository) ListByTarget(ctx context.Context, tenantID, targetFragmentID string, edgeType *models.DependencyType, limit int) ([]*models.DependencyEdge, error) {
	if limit <= 0 {
		limit = 100
	}
	q := r.col.Where("tenantId", "==", tenantID).Where("targetFragmentId", "==", targetFragmentID)
	if edgeType != nil {
		q = q.Where("type", "==", *edgeType)
	}
	q = q.OrderBy("properties.criticality", firestore.Desc).Limit(limit)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var edges []*models.DependencyEdge
	for _, d := range docs {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		assignLoadedEdgeID(&edge, d)
		edges = append(edges, &edge)
	}
	return edges, nil
}

// CountDegreesForFragmentIDs scans edges and counts degrees for the provided fragment IDs.
// This avoids per-fragment queries by scanning edges once.
func (r *DependencyEdgesRepository) CountDegreesForFragmentIDs(ctx context.Context, tenantID string, fragmentIDs map[string]struct{}, pageSize int) (map[string]int, error) {
	if pageSize <= 0 {
		pageSize = 2000
	}
	if pageSize > 5000 {
		pageSize = 5000
	}

	counts := make(map[string]int)
	if len(fragmentIDs) == 0 {
		return counts, nil
	}

	q := r.col.Where("tenantId", "==", tenantID).OrderBy(firestore.DocumentID, firestore.Asc)
	var lastDoc *firestore.DocumentSnapshot

	for {
		paged := q.Limit(pageSize)
		if lastDoc != nil {
			paged = paged.StartAfter(lastDoc)
		}

		docs, err := paged.Documents(ctx).GetAll()
		if err != nil {
			return counts, err
		}
		if len(docs) == 0 {
			break
		}

		for _, doc := range docs {
			var edge models.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			assignLoadedEdgeID(&edge, doc)
			if _, ok := fragmentIDs[edge.SourceFragmentID]; ok {
				counts[edge.SourceFragmentID]++
			}
			if _, ok := fragmentIDs[edge.TargetFragmentID]; ok {
				counts[edge.TargetFragmentID]++
			}
		}

		lastDoc = docs[len(docs)-1]
	}

	return counts, nil
}

// EdgeQueryOptions provides optional filtering for edge queries.
type EdgeQueryOptions struct {
	IncludeHistorical bool // If false (default), filter out edges where ValidTo < now
	IncludeFuture     bool // If false (default), filter out edges where ValidFrom > now
}

// ListBySourceWithOptions gets outgoing edges with filtering options.
// When options.IncludeHistorical is false, historical edges (ValidTo < now) are filtered out.
// When options.IncludeFuture is false, future edges (ValidFrom > now) are filtered out.
func (r *DependencyEdgesRepository) ListBySourceWithOptions(ctx context.Context, tenantID, sourceFragmentID string, edgeType *models.DependencyType, limit int, opts *EdgeQueryOptions) ([]*models.DependencyEdge, error) {
	edges, err := r.ListBySource(ctx, tenantID, sourceFragmentID, edgeType, limit)
	if err != nil {
		return nil, err
	}
	return filterEdgesByOptions(edges, opts), nil
}

// ListByTargetWithOptions gets incoming edges with filtering options.
// When options.IncludeHistorical is false, historical edges (ValidTo < now) are filtered out.
// When options.IncludeFuture is false, future edges (ValidFrom > now) are filtered out.
func (r *DependencyEdgesRepository) ListByTargetWithOptions(ctx context.Context, tenantID, targetFragmentID string, edgeType *models.DependencyType, limit int, opts *EdgeQueryOptions) ([]*models.DependencyEdge, error) {
	edges, err := r.ListByTarget(ctx, tenantID, targetFragmentID, edgeType, limit)
	if err != nil {
		return nil, err
	}
	return filterEdgesByOptions(edges, opts), nil
}

// filterEdgesByOptions applies temporal filtering based on options.
// This is done in-memory because Firestore doesn't support null-field comparisons efficiently.
func filterEdgesByOptions(edges []*models.DependencyEdge, opts *EdgeQueryOptions) []*models.DependencyEdge {
	if opts == nil {
		opts = &EdgeQueryOptions{} // Default: exclude historical and future
	}

	// If including everything, return as-is
	if opts.IncludeHistorical && opts.IncludeFuture {
		return edges
	}

	result := make([]*models.DependencyEdge, 0, len(edges))
	for _, edge := range edges {
		if edge == nil {
			continue
		}
		// Filter historical edges
		if !opts.IncludeHistorical && edge.IsHistorical() {
			continue
		}
		// Filter future edges
		if !opts.IncludeFuture && edge.IsFuture() {
			continue
		}
		result = append(result, edge)
	}
	return result
}

// Update обновляет DependencyEdge
func (r *DependencyEdgesRepository) UpdateDerived(ctx context.Context, edge *models.DependencyEdge) error {
	return r.updateWithOptions(ctx, edge, EdgeUpdateOptions{SkipOnUpsert: true})
}

func (r *DependencyEdgesRepository) Update(ctx context.Context, edge *models.DependencyEdge) error {
	return r.updateWithOptions(ctx, edge, EdgeUpdateOptions{})
}

func (r *DependencyEdgesRepository) updateWithOptions(ctx context.Context, edge *models.DependencyEdge, opts EdgeUpdateOptions) error {
	if edge == nil || edge.ID == "" {
		return ErrInvalidDocument
	}
	sourceType, targetType, sourcePath, targetPath := r.resolveEndpointTypesAndPaths(ctx, edge)
	if normalizedSourcePath, normalizedTargetPath, swapped := r.applyProductRelationDictionary(ctx, edge, sourceType, targetType, sourcePath, targetPath); normalizedSourcePath != "" || normalizedTargetPath != "" {
		sourcePath, targetPath = normalizedSourcePath, normalizedTargetPath
		if swapped {
			sourceType, targetType = targetType, sourceType
		}
	}
	r.applyRegulatoryTransformDictionary(edge, sourcePath, targetPath)
	graphenrich.ApplyEdgeHeuristics(edge, sourcePath, targetPath)
	if err := edge.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	now := time.Now().UTC()
	edge.Metadata.LastUpdatedAt = now

	_, err := r.col.Doc(edge.ID).Set(ctx, edge)
	if err != nil {
		return err
	}
	tracking.RecordEdgeUpdated(ctx, edge.ID)
	if r.useGCS {
		if err := r.uploadToGCS(ctx, edge); err != nil {
			log.Printf("[DependencyEdgesRepository:Update] WARNING: GCS upload failed for edge=%s: %v", edge.ID, err)
		}
	}
	if !opts.SkipOnUpsert {
		r.runOnUpsertAsync(edge)
	}
	return nil
}

func (r *DependencyEdgesRepository) runOnUpsertAsync(edge *models.DependencyEdge) {
	if r == nil || r.onUpsert == nil || edge == nil {
		return
	}
	e := edge
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[DependencyEdgesRepository] ERROR: onUpsert hook panic recovered: edgeId=%s panic=%v", strings.TrimSpace(e.ID), rec)
			}
		}()
		hctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		r.onUpsert(hctx, e)
	}()
}

// Delete удаляет DependencyEdge
func (r *DependencyEdgesRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	if err != nil {
		return err
	}
	if r.vertexSearchDelete != nil {
		if derr := r.vertexSearchDelete(ctx, strings.TrimSpace(id)); derr != nil {
			log.Printf("[DependencyEdgesRepository:Delete] WARNING: vertex delete failed for edge=%s: %v", strings.TrimSpace(id), derr)
		}
	}
	return nil
}

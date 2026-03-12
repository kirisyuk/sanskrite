package scout

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
)

// chainReusePathBuilder abstracts the minimal interface needed for chain reuse evaluation.
type chainReusePathBuilder interface {
	GetChainByCanonicalKey(ctx context.Context, tenantID, canonicalKey string) (*models.MaterializedArtifact, error)
}

// ChainReuseLevel describes what to do when a supply chain already exists.
type ChainReuseLevel string

const (
	// ChainReuseFullSkip: chain is fresh, validated, high-confidence — skip processing entirely,
	// just add key nodes to lens scope.
	ChainReuseFullSkip ChainReuseLevel = "full_skip"

	// ChainReuseEnrichMerge: chain exists but is stale, hypothesis, or incomplete — run pipeline
	// and merge results, then add nodes to lens scope.
	ChainReuseEnrichMerge ChainReuseLevel = "enrich_merge"

	// ChainReuseNone: no existing chain found — full processing as normal.
	ChainReuseNone ChainReuseLevel = "no_reuse"
)

// ChainReuseDecision captures the result of evaluating whether a parsed supply chain
// can reuse an existing MaterializedArtifact instead of full re-processing.
type ChainReuseDecision struct {
	Level        ChainReuseLevel
	CanonicalKey string
	Artifact     *models.MaterializedArtifact
	Reason       string
}

// chainReuseFreshnessThreshold is how recently the artifact must have been updated
// for a full skip (7 days for standard, configurable per strategy later).
const chainReuseFreshnessThresholdDays = 7

// chainReuseMinConfidence is the minimum confidence to consider full skip.
const chainReuseMinConfidence = 0.7

// chainReuseMinSegments — single-segment chains are too sparse for full skip.
const chainReuseMinSegments = 2

// evaluateChainReuse checks whether a parsed supply chain already exists as a MaterializedArtifact
// and decides the reuse level.
//
// Called per-chain inside processSupplyChains, after parsing but before fragment/edge creation.
func evaluateChainReuse(
	ctx context.Context,
	builder chainReusePathBuilder,
	tenantID string,
	segments []pathbuilder.PathSegment,
) ChainReuseDecision {
	if builder == nil || len(segments) == 0 {
		return ChainReuseDecision{Level: ChainReuseNone, Reason: "no_builder_or_segments"}
	}

	canonicalKey := pathbuilder.BuildStructuralCanonicalKey(tenantID, segments)
	if canonicalKey == "" {
		return ChainReuseDecision{Level: ChainReuseNone, Reason: "empty_canonical_key"}
	}

	existing, err := builder.GetChainByCanonicalKey(ctx, tenantID, canonicalKey)
	if err != nil || existing == nil {
		return ChainReuseDecision{
			Level:        ChainReuseNone,
			CanonicalKey: canonicalKey,
			Reason:       "artifact_not_found",
		}
	}
	return evaluateChainReuseFromExisting(canonicalKey, existing)
}

func evaluateChainReuseFromExisting(canonicalKey string, existing *models.MaterializedArtifact) ChainReuseDecision {
	if existing == nil {
		return ChainReuseDecision{
			Level:        ChainReuseNone,
			CanonicalKey: canonicalKey,
			Reason:       "artifact_not_found",
		}
	}
	staleness := time.Since(existing.UpdatedAt)
	isFresh := staleness < time.Duration(chainReuseFreshnessThresholdDays)*24*time.Hour
	isValidated := existing.PathStatus == "validated"
	hasEnoughSegments := countPayloadNodes(existing.Payload) >= chainReuseMinSegments
	confidence := extractPayloadConfidence(existing)
	hasEnrichedBindings := artifactHasEnrichedPathBindings(existing)

	// Full skip: artifact is fresh, validated, high-confidence, complete enough.
	if isFresh && isValidated && confidence >= chainReuseMinConfidence && hasEnoughSegments && hasEnrichedBindings {
		return ChainReuseDecision{
			Level:        ChainReuseFullSkip,
			CanonicalKey: canonicalKey,
			Artifact:     existing,
			Reason: fmt.Sprintf("fresh(%.0fh) validated confidence=%.2f nodes=%d",
				staleness.Hours(), confidence, countPayloadNodes(existing.Payload)),
		}
	}

	// Enrich/merge: artifact exists but doesn't meet full-skip criteria.
	reasons := make([]string, 0, 4)
	if !isFresh {
		reasons = append(reasons, fmt.Sprintf("stale(%.0fh)", staleness.Hours()))
	}
	if !isValidated {
		reasons = append(reasons, "hypothesis")
	}
	if confidence < chainReuseMinConfidence {
		reasons = append(reasons, fmt.Sprintf("low_confidence(%.2f)", confidence))
	}
	if !hasEnoughSegments {
		reasons = append(reasons, fmt.Sprintf("sparse(%d_nodes)", countPayloadNodes(existing.Payload)))
	}
	if !hasEnrichedBindings {
		reasons = append(reasons, "path_bindings_missing")
	}

	return ChainReuseDecision{
		Level:        ChainReuseEnrichMerge,
		CanonicalKey: canonicalKey,
		Artifact:     existing,
		Reason:       strings.Join(reasons, ","),
	}
}

func artifactHasEnrichedPathBindings(artifact *models.MaterializedArtifact) bool {
	if artifact == nil || artifact.Kind != models.MaterializedKindSupplyChainPath || artifact.Payload == nil {
		return false
	}
	parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil || len(parsed.Legs) == 0 {
		return false
	}
	if parsed.Stress != nil && parsed.Stress.PathScore > 0 {
		return true
	}
	for _, leg := range parsed.Legs {
		if leg.BindingConfidence > 0 {
			return true
		}
		if len(leg.FlowRefs) > 0 || len(leg.ZoneRefs) > 0 || len(leg.AffectedZoneRefs) > 0 || len(leg.SpatialSemantics) > 0 {
			return true
		}
		if leg.Stress != nil && leg.Stress.StressScore > 0 {
			return true
		}
	}
	return false
}

// addArtifactToLensScope adds a MaterializedArtifact as a single /artifact/ scope entry
// to the lens (best-effort, up to MaxAutoScopeAddsPerLens). Returns true if the entry was added.
func addArtifactToLensScope(
	ctx context.Context,
	lensesRepo *repo.LensesRepository,
	lensID string,
	artifact *models.MaterializedArtifact,
) bool {
	if lensesRepo == nil || lensID == "" || artifact == nil || strings.TrimSpace(artifact.CanonicalKey) == "" {
		return false
	}
	artifactRef := "/artifact/" + strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
	res, err := lensesRepo.AddScopeCanonicalPathAutoLimited(ctx, lensID, artifactRef, models.MaxAutoScopeAddsPerLens)
	if err != nil || !res.Added {
		if res.Added {
			return true
		}
		return false
	}
	// Persist ScopeArtifacts metadata so frontend can render immediately.
	artKey := strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
	ref := models.ScopeArtifactRef{
		Kind:        string(artifact.Kind),
		DisplayName: artifact.DisplayName,
		NodeRefs:    artifact.NodeRefs,
		Status:      artifact.PathStatus,
	}
	if artifact.Kind == models.MaterializedKindSupplyChainPath {
		if parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
			summary := models.SummarizeSupplyChainPathBindings(parsed)
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
	}
	_ = lensesRepo.UpsertScopeArtifact(ctx, lensID, artKey, models.ScopeArtifactRef{
		Kind:               ref.Kind,
		DisplayName:        ref.DisplayName,
		NodeRefs:           ref.NodeRefs,
		Status:             ref.Status,
		GeoRefs:            ref.GeoRefs,
		FlowRefs:           ref.FlowRefs,
		ZoneRefs:           ref.ZoneRefs,
		AffectedZoneRefs:   ref.AffectedZoneRefs,
		SpatialSemantics:   ref.SpatialSemantics,
		LegCount:           ref.LegCount,
		BindingConfidence:  ref.BindingConfidence,
		PathStressScore:    ref.PathStressScore,
		BottleneckLegIndex: ref.BottleneckLegIndex,
		RecoveryLatencyHrs: ref.RecoveryLatencyHrs,
	})
	return true
}

// filterScopeWorthyRefs selects canonical paths suitable for lens scope from NodeRefs.
// Includes orgs, facilities, deposits, gov. Excludes products, locations, natural/end-consumer.
func filterScopeWorthyRefs(nodeRefs []string) []string {
	if len(nodeRefs) == 0 {
		return nil
	}
	out := make([]string, 0, len(nodeRefs))
	for _, ref := range nodeRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		switch {
		case strings.HasPrefix(ref, "/org/"),
			strings.HasPrefix(ref, "/fac/"),
			strings.HasPrefix(ref, "/deposit/"),
			strings.HasPrefix(ref, "/gov/"):
			out = append(out, ref)
		}
	}
	return out
}

// countPayloadNodes counts nodes in a MaterializedArtifact payload.
func countPayloadNodes(payload map[string]interface{}) int {
	if payload == nil {
		return 0
	}
	nodesRaw, ok := payload["nodes"]
	if !ok {
		return 0
	}
	switch ns := nodesRaw.(type) {
	case []interface{}:
		return len(ns)
	case []models.SupplyChainPathNode:
		return len(ns)
	default:
		return 0
	}
}

// extractPayloadConfidence extracts overall confidence from artifact payload or labels.
func extractPayloadConfidence(artifact *models.MaterializedArtifact) float64 {
	if artifact == nil {
		return 0
	}
	if artifact.Payload != nil {
		if notes, ok := artifact.Payload["notes"].(map[string]interface{}); ok {
			if conf, ok := notes["confidence"]; ok {
				switch v := conf.(type) {
				case float64:
					return v
				case int:
					return float64(v)
				}
			}
		}
	}
	// Fallback: if no explicit confidence, treat validated chains as having baseline confidence.
	if artifact.PathStatus == "validated" {
		return 0.8
	}
	return 0.5
}

// logChainReuseDecision emits structured trace for chain reuse decisions.
func logChainReuseDecision(ctx context.Context, job *models.ScoutingJob, decision ChainReuseDecision, chainIdx int) {
	lensID := ""
	if job != nil {
		lensID = strings.TrimSpace(job.LensID)
		if lensID == "" && job.State != nil {
			lensID = parseStateString(job.State["lensId"])
		}
	}
	trace.Log(ctx, "SupplyChain", "SupplyChain.chain_reuse.decision", map[string]any{
		"tenantId":     safeTenantID(job),
		"projectId":    safeProjectID(job),
		"lensId":       lensID,
		"jobId":        safeJobID(job),
		"chainIdx":     chainIdx,
		"level":        string(decision.Level),
		"canonicalKey": decision.CanonicalKey,
		"reason":       decision.Reason,
		"hasArtifact":  decision.Artifact != nil,
	})
}

func safeTenantID(job *models.ScoutingJob) string {
	if job == nil {
		return ""
	}
	return strings.TrimSpace(job.TenantID)
}

func safeProjectID(job *models.ScoutingJob) string {
	if job == nil {
		return ""
	}
	return strings.TrimSpace(job.ProjectID)
}

func safeJobID(job *models.ScoutingJob) string {
	if job == nil {
		return ""
	}
	return strings.TrimSpace(job.ID)
}

package scout

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/intra_graph"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// runIntraGraphInference runs intra-graph link inference for the lens scope,
// materializes inferred direct relations, and publishes WS events.
func (e *ScoutEngine) runIntraGraphInference(ctx context.Context, tenantID, projectID, lensID string) {
	if e == nil || e.aiExecutor == nil || e.fragmentsRepo == nil || e.edgesRepo == nil || e.lensesRepo == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return
	}

	lens, err := e.lensesRepo.Get(ctx, lensID)
	if err != nil || lens == nil {
		return
	}

	// Publish started event
	e.publish(ctx, realtime.EventType("horizon.intra_graph.started"), map[string]interface{}{
		"tenantId":  tenantID,
		"projectId": projectID,
		"lensId":    lensID,
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"humanReadable": map[string]interface{}{
			"title":       "Intra-graph link inference",
			"description": "Inferring direct relations between entities in scope",
		},
	})

	inf := intra_graph.NewInferrer(e.aiExecutor, e.fragmentsRepo, e.edgesRepo)
	result, err := inf.Infer(ctx, intra_graph.InferInput{
		TenantID:  tenantID,
		ProjectID: projectID,
		LensID:    lensID,
		Lens:      lens,
	})
	if err != nil {
		fmt.Printf("[ScoutEngine] intra-graph inference error: %v\n", err)
		e.publish(ctx, realtime.EventType("horizon.intra_graph.completed"), map[string]interface{}{
			"tenantId":  tenantID,
			"projectId": projectID,
			"lensId":    lensID,
			"error":     err.Error(),
			"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		})
		return
	}
	if result == nil || len(result.Relations) == 0 {
		e.publish(ctx, realtime.EventType("horizon.intra_graph.completed"), map[string]interface{}{
			"tenantId":  tenantID,
			"projectId": projectID,
			"lensId":    lensID,
			"added":     0,
			"pairStats": result.PairStats,
			"ts":        time.Now().UTC().Format(time.RFC3339Nano),
			"humanReadable": map[string]interface{}{
				"title":       "Intra-graph link inference",
				"description": "No direct relations inferred",
			},
		})
		return
	}

	// Materialize inferred relations
	syntheticJob := &models.ScoutingJob{LensID: lensID}
	syntheticJob.ID = "intra_graph_" + lensID
	syntheticJob.TenantID = tenantID
	syntheticJob.ProjectID = projectID
	syntheticJob.AgentID = "scout"
	syntheticJob.State = map[string]interface{}{"maxChildrenPerJob": 0}
	added := e.materializeIntraGraphRelations(ctx, syntheticJob, result.Relations)

	e.publish(ctx, realtime.EventType("horizon.intra_graph.completed"), map[string]interface{}{
		"tenantId":  tenantID,
		"projectId": projectID,
		"lensId":    lensID,
		"added":     added,
		"pairStats": result.PairStats,
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"humanReadable": map[string]interface{}{
			"title":       "Intra-graph link inference",
			"description": fmt.Sprintf("Added %d direct relations", added),
		},
	})
}

func (e *ScoutEngine) runScopedIntraGraphInference(ctx context.Context, tenantID, projectID, lensID string, scopeRefs []string) {
	if e == nil || e.aiExecutor == nil || e.fragmentsRepo == nil || e.edgesRepo == nil || e.lensesRepo == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	scopeRefs = uniqueSortedStrings(scopeRefs)
	if lensID == "" || len(scopeRefs) == 0 {
		return
	}

	lens, err := e.lensesRepo.Get(ctx, lensID)
	if err != nil || lens == nil {
		return
	}

	scopeSet := make(map[string]struct{}, len(scopeRefs))
	for _, ref := range scopeRefs {
		scopeSet[strings.TrimSpace(ref)] = struct{}{}
	}

	e.publish(ctx, realtime.EventType("horizon.intra_graph.started"), map[string]interface{}{
		"tenantId":  tenantID,
		"projectId": projectID,
		"lensId":    lensID,
		"scopeN":    len(scopeRefs),
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"humanReadable": map[string]interface{}{
			"title":       "Scoped intra-graph link inference",
			"description": "Inferring direct relations within assembly scope",
		},
	})

	inf := intra_graph.NewInferrer(e.aiExecutor, e.fragmentsRepo, e.edgesRepo)
	result, err := inf.Infer(ctx, intra_graph.InferInput{
		TenantID:  tenantID,
		ProjectID: projectID,
		LensID:    lensID,
		Lens:      lens,
	})
	if err != nil {
		e.publish(ctx, realtime.EventType("horizon.intra_graph.completed"), map[string]interface{}{
			"tenantId":  tenantID,
			"projectId": projectID,
			"lensId":    lensID,
			"scopeN":    len(scopeRefs),
			"error":     err.Error(),
			"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		})
		return
	}
	if result == nil || len(result.Relations) == 0 {
		return
	}

	filtered := make([]agents.DiscoveredRelation, 0, len(result.Relations))
	for _, rel := range result.Relations {
		src := strings.TrimSpace(rel.SourceCanonicalPath)
		tgt := strings.TrimSpace(rel.TargetCanonicalPath)
		if _, ok := scopeSet[src]; !ok {
			continue
		}
		if _, ok := scopeSet[tgt]; !ok {
			continue
		}
		filtered = append(filtered, rel)
	}
	if len(filtered) == 0 {
		return
	}

	syntheticJob := &models.ScoutingJob{LensID: lensID}
	syntheticJob.ID = "intra_graph_scoped_" + lensID
	syntheticJob.TenantID = tenantID
	syntheticJob.ProjectID = projectID
	syntheticJob.AgentID = "scout"
	syntheticJob.State = map[string]interface{}{"maxChildrenPerJob": 0}
	added := e.materializeIntraGraphRelations(ctx, syntheticJob, filtered)

	e.publish(ctx, realtime.EventType("horizon.intra_graph.completed"), map[string]interface{}{
		"tenantId":  tenantID,
		"projectId": projectID,
		"lensId":    lensID,
		"scopeN":    len(scopeRefs),
		"added":     added,
		"pairStats": result.PairStats,
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"humanReadable": map[string]interface{}{
			"title":       "Scoped intra-graph link inference",
			"description": fmt.Sprintf("Added %d scoped direct relations", added),
		},
	})
}

// materializeIntraGraphRelations materializes inferred relations (no child job scheduling).
func (e *ScoutEngine) materializeIntraGraphRelations(ctx context.Context, job *models.ScoutingJob, relations []agents.DiscoveredRelation) int {
	if e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil || job == nil {
		return 0
	}
	tenantID := strings.TrimSpace(job.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	seenEdgeKeys := make(map[string]struct{})
	added := 0
	for _, rel := range relations {
		srcPath := strings.TrimSpace(rel.SourceCanonicalPath)
		tgtPath := strings.TrimSpace(rel.TargetCanonicalPath)
		if srcPath == "" || tgtPath == "" || srcPath == tgtPath {
			continue
		}
		srcFrag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, srcPath)
		if err != nil || srcFrag == nil {
			continue
		}
		tgtFrag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, tgtPath)
		if err != nil || tgtFrag == nil {
			continue
		}
		targetType := e.normalizeFragmentType(ctx, rel.TargetType)
		if targetType == "" {
			targetType = InferFragmentTypeFromCanonicalPath(tgtPath)
		}
		rawRelationType := strings.TrimSpace(rel.RelationType)
		rel.RelationType = strings.TrimSpace(taxonomy.NormalizeRelationType(rel.RelationType))
		normalizedRelationType := strings.TrimSpace(rel.RelationType)
		if normalizedRelationType == "" {
			continue
		}

		// Canonicalize direction before materialization (aligned with unified policy).
		// Fixes ADMINISTERS/REGULATES /org/->/gov/, FACILITY->ORG, DEPOSIT->ORG, etc.
		canon := policy.ApplyEdgeCanonicalizationPolicy(policy.EdgeCanonicalizationInput{
			EdgeType:        DependencyType(normalizedRelationType),
			SourceType:      srcFrag.Class,
			TargetType:      tgtFrag.Class,
			SourcePath:      srcPath,
			TargetPath:      tgtPath,
			RawRelationType: rawRelationType,
			Reason:          strings.TrimSpace(rel.Reason),
			Qualifiers:      rel.Qualifiers,
		})
		if canon.Changed && canon.ShouldSwap {
			srcPath, tgtPath = tgtPath, srcPath
			srcFrag, tgtFrag = tgtFrag, srcFrag
			rel.SourceCanonicalPath, rel.TargetCanonicalPath = rel.TargetCanonicalPath, rel.SourceCanonicalPath
			rel.SourceType, rel.TargetType = rel.TargetType, rel.SourceType
			rel.SourceName, rel.TargetName = rel.TargetName, rel.SourceName
			targetType = tgtFrag.Class
		}
		if canon.Changed && string(canon.EdgeType) != "" {
			rel.RelationType = string(canon.EdgeType)
			normalizedRelationType = string(canon.EdgeType)
		}

		baseQualifiers := map[string]interface{}{"source": "intra_graph"}
		err = e.materializeRelationEdge(ctx, job, srcFrag, tgtFrag, tgtFrag.ID, targetType, tgtPath, rel, rawRelationType, normalizedRelationType, "intra_graph", baseQualifiers, false, seenEdgeKeys)
		if err == nil {
			added++
		}
	}
	return added
}

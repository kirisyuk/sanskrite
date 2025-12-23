package scout

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func parseStateInt(v interface{}, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return def
		}
		if i, err := strconv.Atoi(t); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return int(f)
		}
		return def
	default:
		return def
	}
}

func parseStateString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func normalizeEdgeConfidence(in float64) float64 {
	// LLMs sometimes violate the contract and return 0..100 or other ranges.
	// We normalize best-effort to keep graph materialization robust.
	c := in
	if c > 1.0 && c <= 100.0 {
		c = c / 100.0
	}
	if c < 0 {
		return 0
	}
	if c > 1.0 {
		return 1.0
	}
	return c
}

// processDiscoveredRelations обрабатывает найденные связи и запускает декомпозицию
func (e *ScoutEngine) processDiscoveredRelations(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
) error {
	return e.processRelationsInternal(ctx, job, sourceFragment, relations, "scout_discovery", map[string]interface{}{"source": "scout"})
}

// processHypothesisRelations materializes relations produced from intent decomposition.
// These are hypotheses (not grounded claims), so we mark provenance/qualifiers accordingly.
func (e *ScoutEngine) processHypothesisRelations(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
) error {
	return e.processRelationsInternal(ctx, job, sourceFragment, relations, "intent_decomposition", map[string]interface{}{"source": "intent_decomposition", "hypothesis": true})
}

func (e *ScoutEngine) processRelationsInternal(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
	provenanceMethod string,
	baseQualifiers map[string]interface{},
) error {
	if len(relations) == 0 {
		return nil
	}

	// Guardrails for recursive scouting:
	// - Depth is controlled by recursionDepth/maxRecursionDepth.
	// - Breadth is capped per parent job to avoid runaway fanout (LLM can emit many relations).
	// - We deduplicate targets within this job execution to avoid enqueueing the same target multiple times.
	maxChildren := 25
	if job != nil && job.State != nil {
		if v, ok := job.State["maxChildrenPerJob"]; ok {
			maxChildren = parseStateInt(v, maxChildren)
		}
	}
	if maxChildren < 0 {
		maxChildren = 0
	}
	scheduledChildren := map[string]struct{}{}
	scheduledCount := 0

	// Relation-level guardrails:
	// - Do not recurse on COMPETES_WITH (explodes breadth and is rarely needed for local intent).
	// - Do not recurse on INFLUENCES (spheres and vague influences create huge expansions).
	// - Require a moderate confidence for recursion to avoid "broad hallucinated" graphs.
	shouldRecurse := func(relType string, conf float64, targetPath string, targetType models.FragmentType) bool {
		rt := strings.TrimSpace(relType)
		if rt == "" {
			return false
		}
		if conf > 0 && conf < 0.55 {
			return false
		}
		if strings.HasPrefix(strings.TrimSpace(targetPath), "/sphere/") || targetType == models.FragmentTypeSphere {
			return false
		}
		switch rt {
		case string(models.DependencyTypeCompetesWith), string(models.DependencyTypeInfluences):
			return false
		default:
			return true
		}
	}

	for _, rel := range relations {
		if rel.TargetName == "" {
			continue
		}

		// Normalize relation type early (runtime taxonomy allowlist) to avoid creating edges with unsupported types.
		rel.RelationType = strings.TrimSpace(taxonomy.NormalizeRelationType(rel.RelationType))
		targetTypeHint := normalizeFragmentType(rel.TargetType)
		targetType := targetTypeHint

		// 1. Determine target canonical path
		targetPath, err := e.resolveCanonicalPath(ctx, rel)
		if err != nil {
			fmt.Printf("Canonicalization failed for relation target=%q: %v\n", rel.TargetName, err)
			continue
		}

		// 2. Find or Create Target Fragment
		targetFragment, err := e.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
		var targetID string

		if err != nil {
			// Fragment doesn't exist -> Create minimal fragment (low confidence) and schedule scouting to fill it.
			// We do NOT use /discovered paths; canonicalPath must already be stable here.
			ft := normalizeFragmentType(rel.TargetType)
			if ft == "" {
				ft = models.FragmentTypeOrganization
			}
			targetType = ft
			now := time.Now().UTC()
			legalName := rel.TargetName
			if rel.TargetLegalName != "" {
				legalName = rel.TargetLegalName
			}
			newFragment := &models.HorizonFragment{
				TenantID:      "global",
				CanonicalPath: targetPath,
				Type:          ft,
				Status:        "active",
				State:         make(map[string]models.MetricValue),
				Labels:        map[string]string{"en": legalName},
				Identifiers: map[string]string{
					"legalName": legalName,
				},
				Governance:  models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
				Description: fmt.Sprintf("Discovered via relation from %s", sourceFragment.CanonicalPath),
				Provenance: models.Provenance{
					Method:    provenanceMethod,
					SourceIDs: []string{job.ID},
				},
				Metadata: models.FragmentMetadata{
					CreatedAt:       now,
					LastUpdatedAt:   now,
					LastVerifiedAt:  time.Time{}, // IMPORTANT: keep zero so freshness check does not skip scouting
					EpistemicStatus: models.EpistemicStatusLowConfidence,
				},
			}
			if rel.TargetShortName != "" {
				newFragment.Identifiers["shortName"] = rel.TargetShortName
			}

			id, err := e.fragmentsRepo.Create(ctx, newFragment)
			if err != nil {
				fmt.Printf("Failed to create discovered fragment %s: %v\n", targetPath, err)
				continue
			}
			targetID = id

			// Create Recursive Scouting Job (Decomposition)
			// Проверяем глубину рекурсии, чтобы не уйти в бесконечность.
			// Читаем текущую глубину/лимит из job.State (это позволяет прод-ядру работать так же,
			// как тестовый сценарий).
			currentDepth := 0
			if job != nil && job.State != nil {
				// Firestore may decode numbers as int64; do not assume float64.
				currentDepth = parseStateInt(job.State["recursionDepth"], 0)
			}

			maxDepth := 2
			if job != nil && job.State != nil {
				if v, ok := job.State["maxRecursionDepth"]; ok {
					maxDepth = parseStateInt(v, 2)
				}
			}
			if maxDepth < 0 {
				maxDepth = 0
			}

			// Daily budget guardrails (depth limiting, not breadth):
			// If we've exceeded cascade or lens daily limits, stop expanding deeper.
			if e.budgetPolicy.Enabled && e.spendLedger != nil && job != nil {
				lensID := ""
				if job.State != nil {
					if v, ok := job.State["lensId"].(string); ok {
						lensID = strings.TrimSpace(v)
					}
				}
				cascadeID := ""
				if job.State != nil {
					if v, ok := job.State["cascadeRootJobId"].(string); ok {
						cascadeID = strings.TrimSpace(v)
					}
				}
				if cascadeID == "" {
					cascadeID = job.ID
				}
				if lensID != "" && cascadeID != "" {
					day := time.Now().UTC().Format("2006-01-02")
					if cSpend, _, cOut, err := e.spendLedger.GetCascadeSpendUSD(ctx, job.TenantID, cascadeID, day); err == nil {
						if e.budgetPolicy.CascadeDailyMaxUSD > 0 && cSpend >= e.budgetPolicy.CascadeDailyMaxUSD {
							maxDepth = currentDepth
						}
						if e.budgetPolicy.CascadeDailyMaxOutputTokens > 0 && cOut >= e.budgetPolicy.CascadeDailyMaxOutputTokens {
							maxDepth = currentDepth
						}
					}
					if dSpend, _, dOut, err := e.spendLedger.GetLensDaySpendUSD(ctx, job.TenantID, lensID, day); err == nil {
						if e.budgetPolicy.LensDailyMaxUSD > 0 && dSpend >= e.budgetPolicy.LensDailyMaxUSD {
							maxDepth = currentDepth
						}
						if e.budgetPolicy.LensDailyMaxOutputTokens > 0 && dOut >= e.budgetPolicy.LensDailyMaxOutputTokens {
							maxDepth = currentDepth
						}
					}
				}
			}

			// Ограничиваем глубину
			if currentDepth < maxDepth {
				// Breadth cap + per-parent dedup (in-memory).
				effectiveCap := maxChildren
				// Depth-aware cap: keep the graph tight for shallow (depth=2) lenses.
				if currentDepth <= 0 && effectiveCap > 12 {
					effectiveCap = 12
				} else if currentDepth >= 1 && effectiveCap > 6 {
					effectiveCap = 6
				}
				if effectiveCap == 0 || scheduledCount >= effectiveCap {
					// skip recursive expansion
				} else if _, ok := scheduledChildren[targetPath]; ok {
					// already scheduled from another relation in the same job
				} else if !shouldRecurse(rel.RelationType, rel.Confidence, targetPath, targetType) {
					// relation type / confidence not eligible for recursion
				} else {
					scheduledChildren[targetPath] = struct{}{}
					scheduledCount++

					agentID := job.AgentID
					if agentID == "" {
						agentID = "scout"
					}
					now := time.Now().UTC()
					childState := map[string]interface{}{
						"targetCanonicalPath": targetPath,
						"currentState":        string(models.ScoutingJobStatePending),
						"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
						"recursionDepth":      float64(currentDepth + 1),
						"maxRecursionDepth":   float64(maxDepth),
						"parentJobId":         job.ID,
					}
					// Propagate lens id (critical for assembly tracking, budgets, and UI filtering).
					if job != nil {
						if strings.TrimSpace(job.LensID) != "" {
							childState["lensId"] = strings.TrimSpace(job.LensID)
						} else if job.State != nil {
							if v, ok := job.State["lensId"]; ok {
								if s := parseStateString(v); s != "" {
									childState["lensId"] = s
								}
							}
						}
						// Propagate optional breadth cap downstream
						if maxChildren > 0 {
							childState["maxChildrenPerJob"] = float64(maxChildren)
						}
					}
					// Propagate cascade root id for budget attribution.
					if job != nil && job.State != nil {
						if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
							childState["cascadeRootJobId"] = strings.TrimSpace(v)
						} else {
							childState["cascadeRootJobId"] = job.ID
						}
					} else {
						childState["cascadeRootJobId"] = job.ID
					}
					// Propagate requestedMetrics, if present (ensures identical dynamic metrics behavior across prod/test).
					if job != nil && job.State != nil {
						if rm, ok := job.State["requestedMetrics"]; ok {
							childState["requestedMetrics"] = rm
						}
						// Propagate intent steering signals so recursive scouting stays aligned with the Lens focus.
						// These may be non-canonical, but are safe to pass through job state.
						if v, ok := job.State["focus"]; ok {
							childState["focus"] = v
						}
						if v, ok := job.State["keywords"]; ok {
							childState["keywords"] = v
						}
						if v, ok := job.State["spherePaths"]; ok {
							childState["spherePaths"] = v
						}
					}
					scoutingJob := &models.ScoutingJob{
						AgentJob: agentmodels.AgentJob{
							TenantID:  job.TenantID,
							ProjectID: job.ProjectID,
							Status:    agentmodels.AgentJobStatusPending,
							AgentID:   agentID, // inherit agentId to allow deterministic test workers and consistent routing
							CreatedAt: now,
							UpdatedAt: now,
							State:     childState,
						},
						LensID:              strings.TrimSpace(job.LensID),
						TargetCanonicalPath: targetPath,
						ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
						BillingContext: models.BillingContext{
							ChargeModel: string(models.ChargeModelBounty),
							ProjectID:   job.ProjectID,
						},
					}
					if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err != nil {
						fmt.Printf("Failed to create recursive job for %s: %v\n", targetPath, err)
					} else {
						fmt.Printf("Triggered recursive scouting for: %s\n", targetPath)
					}
				}
			}

		} else {
			targetID = targetFragment.ID
			if targetFragment != nil && targetFragment.Type != "" {
				targetType = targetFragment.Type
			}

			// If fragment exists but is not verified (or never verified), schedule scouting to enrich it.
			// This is what makes the graph "expand" beyond already-known nodes (e.g., Panasonic) when needed.
			currentDepth := 0
			if job != nil && job.State != nil {
				currentDepth = parseStateInt(job.State["recursionDepth"], 0)
			}
			shouldScout := targetFragment.Metadata.LastVerifiedAt.IsZero() || targetFragment.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence
			maxDepth := 2
			if job != nil && job.State != nil {
				if v, ok := job.State["maxRecursionDepth"]; ok {
					maxDepth = parseStateInt(v, 2)
				}
			}
			if maxDepth < 0 {
				maxDepth = 0
			}

			// Daily budget guardrails (depth limiting, not breadth)
			if e.budgetPolicy.Enabled && e.spendLedger != nil && job != nil {
				lensID := ""
				if job.State != nil {
					if v, ok := job.State["lensId"].(string); ok {
						lensID = strings.TrimSpace(v)
					}
				}
				cascadeID := ""
				if job.State != nil {
					if v, ok := job.State["cascadeRootJobId"].(string); ok {
						cascadeID = strings.TrimSpace(v)
					}
				}
				if cascadeID == "" {
					cascadeID = job.ID
				}
				if lensID != "" && cascadeID != "" {
					day := time.Now().UTC().Format("2006-01-02")
					if cSpend, _, cOut, err := e.spendLedger.GetCascadeSpendUSD(ctx, job.TenantID, cascadeID, day); err == nil {
						if e.budgetPolicy.CascadeDailyMaxUSD > 0 && cSpend >= e.budgetPolicy.CascadeDailyMaxUSD {
							maxDepth = currentDepth
						}
						if e.budgetPolicy.CascadeDailyMaxOutputTokens > 0 && cOut >= e.budgetPolicy.CascadeDailyMaxOutputTokens {
							maxDepth = currentDepth
						}
					}
					if dSpend, _, dOut, err := e.spendLedger.GetLensDaySpendUSD(ctx, job.TenantID, lensID, day); err == nil {
						if e.budgetPolicy.LensDailyMaxUSD > 0 && dSpend >= e.budgetPolicy.LensDailyMaxUSD {
							maxDepth = currentDepth
						}
						if e.budgetPolicy.LensDailyMaxOutputTokens > 0 && dOut >= e.budgetPolicy.LensDailyMaxOutputTokens {
							maxDepth = currentDepth
						}
					}
				}
			}

			if shouldScout && currentDepth < maxDepth {
				// Breadth cap + per-parent dedup (in-memory).
				effectiveCap := maxChildren
				if currentDepth <= 0 && effectiveCap > 12 {
					effectiveCap = 12
				} else if currentDepth >= 1 && effectiveCap > 6 {
					effectiveCap = 6
				}
				if effectiveCap == 0 || scheduledCount >= effectiveCap {
					// skip recursive expansion
				} else if _, ok := scheduledChildren[targetPath]; ok {
					// already scheduled from another relation in the same job
				} else if !shouldRecurse(rel.RelationType, rel.Confidence, targetPath, targetType) {
					// relation type / confidence not eligible for recursion
				} else {
					scheduledChildren[targetPath] = struct{}{}
					scheduledCount++

					agentID := job.AgentID
					if agentID == "" {
						agentID = "scout"
					}
					now := time.Now().UTC()
					childState := map[string]interface{}{
						"targetCanonicalPath": targetPath,
						"currentState":        string(models.ScoutingJobStatePending),
						"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
						"recursionDepth":      float64(currentDepth + 1),
						"maxRecursionDepth":   float64(maxDepth),
						"parentJobId":         job.ID,
					}
					if job != nil {
						if strings.TrimSpace(job.LensID) != "" {
							childState["lensId"] = strings.TrimSpace(job.LensID)
						} else if job.State != nil {
							if v, ok := job.State["lensId"]; ok {
								if s := parseStateString(v); s != "" {
									childState["lensId"] = s
								}
							}
						}
						if maxChildren > 0 {
							childState["maxChildrenPerJob"] = float64(maxChildren)
						}
					}
					// Propagate cascade root id for budget attribution.
					if job != nil && job.State != nil {
						if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
							childState["cascadeRootJobId"] = strings.TrimSpace(v)
						} else {
							childState["cascadeRootJobId"] = job.ID
						}
					} else {
						childState["cascadeRootJobId"] = job.ID
					}
					if job != nil && job.State != nil {
						if rm, ok := job.State["requestedMetrics"]; ok {
							childState["requestedMetrics"] = rm
						}
					}
					scoutingJob := &models.ScoutingJob{
						AgentJob: agentmodels.AgentJob{
							TenantID:  job.TenantID,
							ProjectID: job.ProjectID,
							Status:    agentmodels.AgentJobStatusPending,
							AgentID:   agentID,
							CreatedAt: now,
							UpdatedAt: now,
							State:     childState,
						},
						LensID:              strings.TrimSpace(job.LensID),
						TargetCanonicalPath: targetPath,
						ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
						BillingContext: models.BillingContext{
							ChargeModel: string(models.ChargeModelBounty),
							ProjectID:   job.ProjectID,
						},
					}
					if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err == nil {
						fmt.Printf("Triggered recursive scouting (existing fragment) for: %s\n", targetPath)
					}
				}
			}
		}

		// 3. Create Dependency Edge
		// Normalize and validate relation types against runtime taxonomy (Firestore-backed).
		edgeType := models.DependencyType(strings.TrimSpace(rel.RelationType))
		edgeSourceID := sourceFragment.ID
		edgeTargetID := targetID

		// Enforce directional semantics for common relations (best-effort).
		// This prevents obviously inverted edges like "/loc/astana SUPPLIES_TO /org/crrc".
		edgeSourceID, edgeTargetID, edgeType, directionChanged := normalizeRelationEdge(sourceFragment, edgeSourceID, edgeTargetID, edgeType, targetType)
		switch edgeType {
		case "SUBSIDIARY_OF":
			// Canonical representation: OWNS from parent -> subsidiary.
			edgeType = models.DependencyTypeOwns
			edgeSourceID, edgeTargetID = targetID, sourceFragment.ID
		}
		// Guard against self-edges (can happen when LLM accidentally points a relation back to the source).
		// Self-edges are almost always noise and can create cycles during propagation/aggregation.
		if strings.TrimSpace(edgeSourceID) != "" && edgeSourceID == edgeTargetID {
			fmt.Printf("Skipping self-edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			continue
		}

		// Проверка на дубликат ребра
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", edgeSourceID, edgeTargetID, edgeType); err == nil {
			// Ребро уже существует, пропускаем создание
			fmt.Printf("Skipping duplicate edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			continue
		}

		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: edgeSourceID,
			TargetFragmentID: edgeTargetID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality:   normalizeEdgeConfidence(rel.Confidence),
				Amplification: 1.0,
				LatencyHours:  24,
			},
			Qualifiers: map[string]interface{}{
				"reason":              strings.TrimSpace(rel.Reason),
				"rawRelationType":     strings.TrimSpace(rel.RelationType),
				"rawConfidence":       rel.Confidence,
				"targetName":          strings.TrimSpace(rel.TargetName),
				"targetCanonicalPath": strings.TrimSpace(targetPath),
				"sourceCanonicalPath": strings.TrimSpace(sourceFragment.CanonicalPath),
			},
			Provenance: models.Provenance{
				Method:    provenanceMethod,
				SourceIDs: []string{job.ID},
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     time.Now(),
				LastUpdatedAt: time.Now(),
			},
		}
		if len(baseQualifiers) > 0 {
			for k, v := range baseQualifiers {
				if _, exists := edge.Qualifiers[k]; !exists {
					edge.Qualifiers[k] = v
				}
			}
		}
		if directionChanged {
			edge.Qualifiers["normalizedDirection"] = true
			edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
		}

		// Используем Create, предполагая что репо обработает дубликаты или мы просто создаем новые
		// В реальной системе нужен UpsertEdge
		if id, err := e.edgesRepo.Create(ctx, edge); err != nil {
			// Historically this was swallowed, which can lead to "0 edges" while nodes exist.
			// Log loudly to make root cause visible (validation, Firestore indexes, permissions, etc.).
			msg := fmt.Sprintf("Failed to create edge %s -> %s (%s) confidence=%.4f rawConfidence=%.4f rawType=%q: %v",
				strings.TrimSpace(sourceFragment.CanonicalPath),
				strings.TrimSpace(targetPath),
				string(edgeType),
				edge.Properties.Criticality,
				rel.Confidence,
				strings.TrimSpace(rel.RelationType),
				err,
			)
			fmt.Printf("[ScoutEngine] %s\n", msg)
			e.logLine(ctx, job, "warn", msg)
		} else {
			edge.ID = id
			fmt.Printf("Created edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			// Realtime: edge materialized/updated for UI graph.
			e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
				"tenantId":         job.TenantID,
				"projectId":        job.ProjectID,
				"lensId":           strings.TrimSpace(job.LensID),
				"edgeId":           edge.ID,
				"sourceFragmentId": edge.SourceFragmentID,
				"targetFragmentId": edge.TargetFragmentID,
				"type":             string(edge.Type),
				"properties":       edge.Properties,
				"qualifiers":       edge.Qualifiers,
			})
		}
	}
	return nil
}

// NOTE: legacy sanitizePathName removed.

// normalizeRelationEdge enforces directional semantics for common relation types.
// It returns possibly swapped source/target IDs and/or adjusted edge type.
// This is intentionally conservative: if unsure, it leaves the relation as-is.
func normalizeRelationEdge(sourceFrag *models.HorizonFragment, sourceID, targetID string, edgeType models.DependencyType, targetType models.FragmentType) (string, string, models.DependencyType, bool) {
	if sourceFrag == nil {
		return sourceID, targetID, edgeType, false
	}
	srcType := sourceFrag.Type
	isGeo := func(t models.FragmentType) bool { return t == models.FragmentTypeGeoRegion }
	isOrg := func(t models.FragmentType) bool { return t == models.FragmentTypeOrganization }
	isFac := func(t models.FragmentType) bool { return t == models.FragmentTypeFacility }
	isProd := func(t models.FragmentType) bool { return t == models.FragmentTypeProduct }

	changed := false

	switch edgeType {
	case models.DependencyTypeOperatesIn:
		// "X operates in Y": X is typically ORG/FAC, Y is GEO.
		if isGeo(srcType) && (isOrg(targetType) || isFac(targetType)) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeLocatedIn:
		// "X located in Y": X is non-geo, Y is GEO. Geo->Geo can be used for containment.
		if isGeo(srcType) && !isGeo(targetType) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeRegulates:
		// GEO->GEO is almost certainly containment, not regulation.
		if isGeo(srcType) && isGeo(targetType) {
			edgeType = models.DependencyTypeLocatedIn
			changed = true
		}
		// GEO->ORG/FAC likely means the org/fac operates in the geo.
		if isGeo(srcType) && (isOrg(targetType) || isFac(targetType)) {
			sourceID, targetID = targetID, sourceID
			edgeType = models.DependencyTypeOperatesIn
			changed = true
		}
	case models.DependencyTypeOwns:
		// If it's clearly inverted, swap (e.g., FAC owns ORG).
		if !isOrg(srcType) && isOrg(targetType) && !isGeo(srcType) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeProduces:
		// "X produces Y": X is ORG/FAC, Y is PRODUCT.
		if isProd(srcType) && !isProd(targetType) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	case models.DependencyTypeSuppliesTo:
		// "X supplies to Y": X is ORG (supplier), Y can be ORG/FAC/PRODUCT/GEO (recipient).
		// If GEO -> ORG/FAC/PRODUCT, it's almost certainly inverted.
		if isGeo(srcType) && (isOrg(targetType) || isFac(targetType) || isProd(targetType)) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	}

	return sourceID, targetID, edgeType, changed
}

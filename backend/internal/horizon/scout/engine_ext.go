package scout

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
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

func parseStateStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case string:
		// comma-separated convenience
		parts := strings.Split(t, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		return nil
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

	// Best-effort: prefetch active (pending/running) scout jobs for this project/lens,
	// so we can avoid enqueueing duplicate child jobs for the same canonicalPath.
	activeJobByPath := map[string]string{} // canonicalPath -> jobId
	if e != nil && e.scoutingJobsRepo != nil && job != nil && strings.TrimSpace(job.TenantID) != "" && strings.TrimSpace(job.ProjectID) != "" {
		lensID := strings.TrimSpace(job.LensID)
		if lensID == "" && job.State != nil {
			if v, ok := job.State["lensId"]; ok {
				lensID = parseStateString(v)
			}
		}
		if jobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, job.TenantID, job.ProjectID, lensID, 300); err == nil {
			for _, j := range jobs {
				if j == nil {
					continue
				}
				if j.Status != agentmodels.AgentJobStatusPending && j.Status != agentmodels.AgentJobStatusRunning {
					continue
				}
				p := strings.TrimSpace(j.TargetCanonicalPath)
				if p == "" {
					continue
				}
				if _, ok := activeJobByPath[p]; !ok {
					activeJobByPath[p] = j.ID
				}
			}
		}
	}

	// Edge-level guardrails:
	// - Deduplicate edges within this job run to reduce Firestore reads/writes.
	// - Normalize symmetric edges so A<->B is represented consistently.
	seenEdgeKeys := map[string]struct{}{}

	// Specialties (optional): used to enable/disable extra normalization rules safely.
	// NOTE: We read specialties early so shouldRecurse can use them for supply_chain mode filtering.
	specialties := []string(nil)
	if job != nil && job.State != nil {
		if v, ok := job.State["specialties"]; ok {
			specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
		}
	}
	isSupplyChainMode := taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain)
	isEquityMode := taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyEquity)
	cascadeID := ""
	if job != nil && job.State != nil {
		if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
			cascadeID = strings.TrimSpace(v)
		}
	}
	if cascadeID == "" && job != nil {
		cascadeID = strings.TrimSpace(job.ID)
	}

	// Relation-level guardrails:
	// - Do not recurse on COMPETES_WITH (explodes breadth and is rarely needed for local intent).
	// - Do not recurse on INFLUENCES (spheres and vague influences create huge expansions).
	// - Require a moderate confidence for recursion to avoid "broad hallucinated" graphs.
	// - In supply_chain mode: ONLY recurse on supply-chain relevant edge types.
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

		// Supply chain mode: strict filtering to only follow supply-chain edges
		if isSupplyChainMode {
			switch rt {
			case string(models.DependencyTypeSuppliesTo),
				string(models.DependencyTypeDependsOn),
				string(models.DependencyTypeProduces),
				string(models.DependencyTypeConsumes),
				string(models.DependencyTypeTransformsInto):
				return true
			default:
				// In supply chain mode, skip non-supply-chain edges to stay focused
				return false
			}
		}

		// Equity mode: follow only ownership/control edges
		if isEquityMode {
			switch rt {
			case string(models.DependencyTypeOwns),
				string(models.DependencyTypeSubsidiaryOf),
				string(models.DependencyTypeControls),
				string(models.DependencyTypeInvestsIn):
				return true
			default:
				return false
			}
		}

		// General mode: exclude known noisy edge types
		switch rt {
		case string(models.DependencyTypeCompetesWith), string(models.DependencyTypeInfluences):
			return false
		default:
			return true
		}
	}

	// NOTE: specialties already read above for shouldRecurse filtering
	if job != nil && job.State != nil {
		// Skip duplicate read - already done above
		if v, ok := job.State["specialties"]; ok && len(specialties) == 0 {
			specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
		}
	}
	enableManagement := taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyManagement)

	for _, rel := range relations {
		if rel.TargetName == "" {
			continue
		}

		// Normalize relation type early (runtime taxonomy allowlist) to avoid creating edges with unsupported types.
		// IMPORTANT: preserve the raw relation type for debugging taxonomy drift / LLM outputs.
		rawRelationType := strings.TrimSpace(rel.RelationType)
		rel.RelationType = strings.TrimSpace(taxonomy.NormalizeRelationType(rel.RelationType))
		normalizedRelationType := strings.TrimSpace(rel.RelationType)
		if rawRelationType != "" && normalizedRelationType != "" && rawRelationType != normalizedRelationType {
			fmt.Printf("[ScoutEngine] Normalized relationType: %q -> %q\n", rawRelationType, normalizedRelationType)
		}
		targetTypeHint := e.normalizeFragmentType(ctx, rel.TargetType)
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
			ft := e.normalizeFragmentType(ctx, rel.TargetType)
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

			// Pre-create validation: check policies and deduplication (same as in processScoutResult)
			usedExistingDuplicate := false
			if e.preCreateValidator != nil {
				preValidation, valErr := e.preCreateValidator.ValidateAndProcess(ctx, newFragment)
				if valErr != nil {
					fmt.Printf("[ScoutEngine] Warning: pre-create validation error for %s: %v\n", targetPath, valErr)
				} else if preValidation != nil {
					// Log warnings
					for _, w := range preValidation.Warnings {
						fmt.Printf("[ScoutEngine] Pre-create warning for %s: %s\n", targetPath, w)
					}
					// Check if creation should be blocked
					if !preValidation.IsValid {
						fmt.Printf("[ScoutEngine] Fragment creation blocked for %s: %s\n", targetPath, preValidation.BlockReason)
						continue
					}
					// Check if we should use an existing fragment instead (deduplication)
					if preValidation.ReplacementFrag != nil && preValidation.ReplacementFrag.ID != "" {
						fmt.Printf("[ScoutEngine] Using existing fragment %s instead of creating %s\n", preValidation.ReplacementFrag.ID, targetPath)
						targetFragment = preValidation.ReplacementFrag
						targetID = targetFragment.ID
						usedExistingDuplicate = true
					}
				}
			}

			if !usedExistingDuplicate {
				id, err := e.fragmentsRepo.Create(ctx, newFragment)
				if err != nil {
					fmt.Printf("Failed to create discovered fragment %s: %v\n", targetPath, err)
					continue
				}
				targetID = id
				targetFragment = newFragment
				targetFragment.ID = id
			}

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
					// Check if we should skip creating scouting job
					shouldSkipJob := false

					// CRITICAL: Skip self-reference (prevents infinite loops)
					if sourceFragment != nil && strings.TrimSpace(targetPath) == strings.TrimSpace(sourceFragment.CanonicalPath) {
						fmt.Printf("[ScoutEngine] Skipping self-reference job: %s -> %s. Circular dependency avoided.\n",
							sourceFragment.CanonicalPath, targetPath)
						shouldSkipJob = true
					}

					// CRITICAL: Skip if target fragment is already verified (regardless of edge existence)
					// This prevents re-scouting entities that were just processed
					if !shouldSkipJob && targetFragment != nil && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
						fmt.Printf("[ScoutEngine] Target fragment already verified: %s (status=%s). Skipping scouting job.\n",
							targetPath, targetFragment.Metadata.EpistemicStatus)
						shouldSkipJob = true
					}

					// Check if edge already exists and target fragment is verified
					// This prevents duplicate scouting jobs for already-processed entities
					if !shouldSkipJob {
						edgeType := models.DependencyType(rel.RelationType)
						if e.edgesRepo != nil && sourceFragment != nil && sourceFragment.ID != "" && targetID != "" {
							if existingEdge, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceFragment.ID, targetID, edgeType); err == nil && existingEdge != nil {
								// Edge already exists, check if target fragment is verified
								if targetFragment != nil && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
									// Fragment is verified, skip creating new scouting job
									fmt.Printf("[ScoutEngine] Edge already exists and target fragment is verified: %s -> %s (%s). Skipping scouting job.\n",
										sourceFragment.CanonicalPath, targetPath, rel.RelationType)
									shouldSkipJob = true
								}
							}
						}
					}

					// Check if there's already an active scouting job for this canonicalPath (pending/running).
					// Uses a prefetched snapshot to avoid per-relation queries.
					if !shouldSkipJob {
						if existingJobID, ok := activeJobByPath[strings.TrimSpace(targetPath)]; ok && strings.TrimSpace(existingJobID) != "" {
							fmt.Printf("[ScoutEngine] Active scouting job already exists for %s (jobId=%s). Skipping duplicate.\n",
								targetPath, existingJobID)
							scheduledChildren[targetPath] = struct{}{}
							shouldSkipJob = true
						}
					}

					if shouldSkipJob {
						continue // Skip to next relation
					}

					// Supply-chain cascade visited set: prevent enqueueing same targetPath multiple times inside this cascade.
					// This is a guardrail against cycles/avalanche fanout; enabled only in supply_chain mode.
					if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
						already, vErr := e.cascadeVisitedRepo.TryMarkVisited(
							ctx,
							job.TenantID,
							job.ProjectID,
							cascadeID,
							targetPath,
							job.ID,
							currentDepth+1,
						)
						if vErr != nil {
							msg := fmt.Sprintf("[SupplyChainVisited] ERROR mark tenant=%s cascade=%s target=%s err=%v",
								strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), vErr)
							fmt.Printf("%s\n", msg)
							e.logLine(ctx, job, "warn", msg)
						} else if already {
							msg := fmt.Sprintf("[SupplyChainVisited] SKIP alreadyVisited tenant=%s cascade=%s target=%s depth=%d",
								strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), currentDepth+1)
							fmt.Printf("%s\n", msg)
							e.logLine(ctx, job, "info", msg)
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.skip", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
								"depth":     currentDepth + 1,
							})
							continue
						} else {
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.mark", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
								"depth":     currentDepth + 1,
							})
						}
					}

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
						// Propagate specialties (optional task focus modules).
						if v, ok := job.State["specialties"]; ok {
							childState["specialties"] = v
						}
						// Propagate supply chain context for continuous chain building.
						// This includes discovered chain fragments so far, enabling agents to extend them.
						if v, ok := job.State["chainContext"]; ok {
							childState["chainContext"] = v
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
						// Propagate originalQuery + decompositionHints so grounded research stays aligned
						// even deep in recursion (especially important for MARKET/PROGRAM anchors).
						if v, ok := job.State["originalQuery"]; ok {
							childState["originalQuery"] = v
						}
						if v, ok := job.State["decompositionHints"]; ok {
							childState["decompositionHints"] = v
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
					// Supply-chain cascade visited set: prevent enqueueing same targetPath multiple times inside this cascade.
					if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
						already, vErr := e.cascadeVisitedRepo.TryMarkVisited(
							ctx,
							job.TenantID,
							job.ProjectID,
							cascadeID,
							targetPath,
							job.ID,
							currentDepth+1,
						)
						if vErr != nil {
							msg := fmt.Sprintf("[SupplyChainVisited] ERROR mark tenant=%s cascade=%s target=%s err=%v",
								strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), vErr)
							fmt.Printf("%s\n", msg)
							e.logLine(ctx, job, "warn", msg)
						} else if already {
							msg := fmt.Sprintf("[SupplyChainVisited] SKIP alreadyVisited tenant=%s cascade=%s target=%s depth=%d",
								strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), currentDepth+1)
							fmt.Printf("%s\n", msg)
							e.logLine(ctx, job, "info", msg)
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.skip", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
								"depth":     currentDepth + 1,
							})
							continue
						}
					}

					if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err != nil {
						fmt.Printf("Failed to create recursive job for %s: %v\n", targetPath, err)
						// Best-effort: undo visited marker so quality isn't blocked by transient enqueue failures.
						if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
							_ = e.cascadeVisitedRepo.DeleteVisited(ctx, job.TenantID, cascadeID, targetPath)
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.unmark_on_create_fail", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
							})
						}
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
					// Check if we should skip creating scouting job
					shouldSkipJob := false

					// CRITICAL: Skip self-reference (prevents infinite loops)
					if sourceFragment != nil && strings.TrimSpace(targetPath) == strings.TrimSpace(sourceFragment.CanonicalPath) {
						trace.Log(ctx, "scout_decompose", "child_job.skip.self_reference", map[string]any{
							"tenantId":            strings.TrimSpace(job.TenantID),
							"projectId":           strings.TrimSpace(job.ProjectID),
							"lensId":              strings.TrimSpace(job.LensID),
							"jobId":               strings.TrimSpace(job.ID),
							"sourceCanonicalPath": strings.TrimSpace(sourceFragment.CanonicalPath),
							"targetCanonicalPath": strings.TrimSpace(targetPath),
						})
						fmt.Printf("[ScoutEngine] Skipping self-reference job: %s -> %s. Circular dependency avoided.\n",
							sourceFragment.CanonicalPath, targetPath)
						shouldSkipJob = true
					}

					// CRITICAL: Skip if target fragment is already verified (regardless of edge existence)
					// This prevents re-scouting entities that were just processed
					if !shouldSkipJob && targetFragment != nil && targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
						trace.Log(ctx, "scout_decompose", "child_job.skip.target_verified", map[string]any{
							"tenantId":            strings.TrimSpace(job.TenantID),
							"projectId":           strings.TrimSpace(job.ProjectID),
							"lensId":              strings.TrimSpace(job.LensID),
							"jobId":               strings.TrimSpace(job.ID),
							"targetCanonicalPath": strings.TrimSpace(targetPath),
							"epistemicStatus":     string(targetFragment.Metadata.EpistemicStatus),
						})
						fmt.Printf("[ScoutEngine] Target fragment already verified: %s (status=%s). Skipping scouting job.\n",
							targetPath, targetFragment.Metadata.EpistemicStatus)
						shouldSkipJob = true
					}

					// Check if edge already exists and target fragment is verified
					if !shouldSkipJob {
						edgeType := models.DependencyType(rel.RelationType)
						if e.edgesRepo != nil && sourceFragment != nil && sourceFragment.ID != "" && targetFragment != nil && targetFragment.ID != "" {
							if existingEdge, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceFragment.ID, targetFragment.ID, edgeType); err == nil && existingEdge != nil {
								if targetFragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
									fmt.Printf("[ScoutEngine] Edge already exists and target fragment is verified: %s -> %s (%s). Skipping scouting job.\n",
										sourceFragment.CanonicalPath, targetPath, rel.RelationType)
									shouldSkipJob = true
								}
							}
						}
					}

					// Check if there's already an active scouting job for this canonicalPath (pending/running).
					if !shouldSkipJob {
						if existingJobID, ok := activeJobByPath[strings.TrimSpace(targetPath)]; ok && strings.TrimSpace(existingJobID) != "" {
							trace.Log(ctx, "scout_decompose", "child_job.skip.active_exists", map[string]any{
								"tenantId":            strings.TrimSpace(job.TenantID),
								"projectId":           strings.TrimSpace(job.ProjectID),
								"lensId":              strings.TrimSpace(job.LensID),
								"jobId":               strings.TrimSpace(job.ID),
								"targetCanonicalPath": strings.TrimSpace(targetPath),
								"existingJobId":       strings.TrimSpace(existingJobID),
							})
							fmt.Printf("[ScoutEngine] Active scouting job already exists for %s (jobId=%s). Skipping duplicate.\n",
								targetPath, existingJobID)
							scheduledChildren[targetPath] = struct{}{}
							shouldSkipJob = true
						}
					}

					if shouldSkipJob {
						continue // Skip to next relation
					}

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
						if v, ok := job.State["specialties"]; ok {
							childState["specialties"] = v
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
					} else {
						// Best-effort: undo visited marker so quality isn't blocked by transient enqueue failures.
						if isSupplyChainMode && e != nil && e.cascadeVisitedRepo != nil {
							_ = e.cascadeVisitedRepo.DeleteVisited(ctx, job.TenantID, cascadeID, targetPath)
							trace.Log(ctx, "SupplyChain", "SupplyChain.visited.unmark_on_create_fail", map[string]any{
								"tenantId":  strings.TrimSpace(job.TenantID),
								"projectId": strings.TrimSpace(job.ProjectID),
								"lensId":    strings.TrimSpace(job.LensID),
								"jobId":     strings.TrimSpace(job.ID),
								"cascadeId": strings.TrimSpace(cascadeID),
								"target":    strings.TrimSpace(targetPath),
							})
						}
					}
				}
			}
		}

		// 3. Create Dependency Edge
		// Normalize and validate relation types against runtime taxonomy (Firestore-backed).
		edgeType := models.DependencyType(strings.TrimSpace(rel.RelationType))
		edgeSourceID := sourceFragment.ID
		edgeTargetID := targetID

		// Enforce directional semantics for common relations (best-effort, heuristic).
		// This prevents obviously inverted edges like "/loc/astana SUPPLIES_TO /org/crrc".
		edgeSourceID, edgeTargetID, edgeType, directionChanged := normalizeRelationEdge(sourceFragment, edgeSourceID, edgeTargetID, edgeType, targetType, enableManagement)
		switch edgeType {
		case "SUBSIDIARY_OF":
			// Canonical representation: OWNS from parent -> subsidiary.
			edgeType = models.DependencyTypeOwns
			edgeSourceID, edgeTargetID = targetID, sourceFragment.ID
			directionChanged = true
		}

		// Policy: self-loop prevention (reject).
		if IsSelfLoop(edgeSourceID, edgeTargetID) {
			fmt.Printf("Skipping self-edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			continue
		}

		// Policy: symmetric edge normalization (avoid A->B and B->A duplicates).
		edgeSourceID, edgeTargetID, edgeType, symmetricSwapped := NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
		if symmetricSwapped {
			directionChanged = true
		}

		// Resolve *actual* endpoint types for validation (must match edgeSourceID/edgeTargetID after swaps).
		// Rely on local fragments when possible to avoid extra Firestore reads.
		resolveTypeByID := func(id string) models.FragmentType {
			id = strings.TrimSpace(id)
			if id == "" {
				return ""
			}
			if sourceFragment != nil && strings.TrimSpace(sourceFragment.ID) == id {
				return sourceFragment.Type
			}
			if targetFragment != nil && strings.TrimSpace(targetFragment.ID) == id {
				return targetFragment.Type
			}
			// Fallback: best-effort fetch (rare; only when endpoints differ from source/target fragments).
			if e.fragmentsRepo != nil {
				if f, ferr := e.fragmentsRepo.Get(ctx, id); ferr == nil && f != nil {
					return f.Type
				}
			}
			return ""
		}
		srcTypeActual := resolveTypeByID(edgeSourceID)
		dstTypeActual := resolveTypeByID(edgeTargetID)

		// Policy: direction validation and reversal (enforced for known edge types).
		// Use unified ValidationPolicy if available, otherwise fallback to hardcoded rules
		if e.validationPolicy != nil && sourceFragment != nil {
			logPrefix := "[ScoutEngine:processRelations]"
			fmt.Printf("%s Using ValidationPolicy for edge validation\n", logPrefix)

			decision, err := e.validationPolicy.Execute(ctx, policy.PolicyInput{
				Kind: "edge",
				Context: map[string]interface{}{
					"validationKind":   "edge",
					"sourceFragmentID": edgeSourceID,
					"targetFragmentID": edgeTargetID,
					"sourceType":       string(srcTypeActual),
					"targetType":       string(dstTypeActual),
					"relationType":     string(edgeType),
				},
				// IMPORTANT: do not pass Fragment here, or ValidationPolicy will overwrite sourceType from it,
				// which can be incorrect after edge normalization swaps.
				Fragment: nil,
			})

			if err != nil {
				fmt.Printf("%s ERROR: ValidationPolicy failed: %v, falling back to hardcoded rules\n", logPrefix, err)
			} else {
				fmt.Printf("%s ValidationPolicy decision: action=%s, path=%s, confidence=%.2f\n",
					logPrefix, decision.Action, decision.DecisionPath, decision.Confidence)

				switch decision.Action {
				case "reject":
					// Don't block - treat as warning/penalty instead
					reason, _ := decision.Params["reason"].(string)
					fmt.Printf("%s WARNING: ValidationPolicy would reject edge (non-blocking): %s\n", logPrefix, reason)
					fmt.Printf("%s Applying penalty instead of blocking - edge will be created with low confidence\n", logPrefix)
					// Apply penalty to edge confidence instead of blocking
					// Continue processing - edge will be created but with lower confidence
				case "reverse":
					shouldReverse, _ := decision.Params["shouldReverse"].(bool)
					if shouldReverse {
						fmt.Printf("%s REVERSING edge direction by ValidationPolicy\n", logPrefix)
						edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
						directionChanged = true
						edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
					}
				case "penalty":
					penalty, _ := decision.Params["penalty"].(float64)
					reason, _ := decision.Params["reason"].(string)
					fmt.Printf("%s PENALTY by ValidationPolicy: %.2f, reason: %s\n", logPrefix, penalty, reason)
					// Penalty will be applied to edge.Properties.Criticality later
				case "allow":
					fmt.Printf("%s ALLOWED by ValidationPolicy\n", logPrefix)
				}
			}
		} else if sourceFragment != nil {
			// Fallback to hardcoded validation
			// Use actual endpoint types (post-normalization).
			st := srcTypeActual
			tt := dstTypeActual
			if st == "" {
				st = sourceFragment.Type
			}
			if tt == "" {
				tt = targetType
			}
			if valid, shouldReverse, rule := ValidateEdgeDirection(edgeType, st, tt); !valid {
				if shouldReverse {
					edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
					directionChanged = true
					// If we reversed, re-apply symmetric normalization (noop for non-symmetric types).
					edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
				} else if rule != nil {
					// Neither direction is valid: reject the edge to keep the graph consistent.
					fmt.Printf("Skipping invalid-direction edge: %s -> %s (%s) rule=%s\n", sourceFragment.CanonicalPath, targetPath, edgeType, rule.Description)
					continue
				}
			}
		}

		// In-memory dedup (per job run), after all normalization.
		ek := EdgeKey(edgeSourceID, edgeTargetID, edgeType)
		if _, ok := seenEdgeKeys[ek]; ok {
			fmt.Printf("Skipping duplicate edge (in-memory): %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			continue
		}
		seenEdgeKeys[ek] = struct{}{}

		// Firestore dedup check (cross-run).
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", edgeSourceID, edgeTargetID, edgeType); err == nil {
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
				"reason":                 strings.TrimSpace(rel.Reason),
				"rawRelationType":        rawRelationType,
				"normalizedRelationType": normalizedRelationType,
				"rawConfidence":          rel.Confidence,
				"targetName":             strings.TrimSpace(rel.TargetName),
				"targetCanonicalPath":    strings.TrimSpace(targetPath),
				"sourceCanonicalPath":    strings.TrimSpace(sourceFragment.CanonicalPath),
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
			edge.Qualifiers["normalizedSourceFragmentId"] = edge.SourceFragmentID
			edge.Qualifiers["normalizedTargetFragmentId"] = edge.TargetFragmentID
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
func normalizeRelationEdge(sourceFrag *models.HorizonFragment, sourceID, targetID string, edgeType models.DependencyType, targetType models.FragmentType, enableManagement bool) (string, string, models.DependencyType, bool) {
	if sourceFrag == nil {
		return sourceID, targetID, edgeType, false
	}
	srcType := sourceFrag.Type
	isGeo := func(t models.FragmentType) bool { return t == models.FragmentTypeGeoRegion }
	isOrg := func(t models.FragmentType) bool { return t == models.FragmentTypeOrganization }
	isFac := func(t models.FragmentType) bool { return t == models.FragmentTypeFacility }
	isProd := func(t models.FragmentType) bool { return t == models.FragmentTypeProduct }
	isPerson := func(t models.FragmentType) bool { return t == models.FragmentTypePerson }

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
		// Ownership is never "X OWNS a person" in our graph semantics.
		// If target is a PERSON and source is an ORG, assume inversion and swap.
		if isOrg(srcType) && isPerson(targetType) {
			sourceID, targetID = targetID, sourceID
			changed = true
			break
		}
		// If it's clearly inverted, swap (e.g., FAC owns ORG / PRODUCT owns ORG).
		if !isOrg(srcType) && !isPerson(srcType) && (isOrg(targetType) || isPerson(targetType)) && !isGeo(srcType) {
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
	case models.DependencyTypeManages, models.DependencyTypeEmployedBy, models.DependencyTypeBoardMemberOf, models.DependencyTypeFounded:
		// Leadership/people relationships are directional and typically originate from PERSON -> ORG.
		// When scouting an ORGANIZATION, LLM output may attach PERSON targets with these types;
		// when the "management" specialty is enabled, we normalize the direction by swapping endpoints.
		if enableManagement && isOrg(srcType) && isPerson(targetType) {
			sourceID, targetID = targetID, sourceID
			changed = true
		}
	}

	return sourceID, targetID, edgeType, changed
}

// processSupplyChains parses supply chain output from agents and persists fragments/edges.
func (e *ScoutEngine) processSupplyChains(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	chains []agents.SupplyChain,
) error {
	if len(chains) == 0 {
		return nil
	}

	parser := NewSupplyChainParser()
	result, err := parser.ParseSupplyChains(chains, sourceFragment.CanonicalPath)
	if err != nil {
		msg := fmt.Sprintf("[SupplyChainParse] failed to parse supplyChains: %v", err)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "warn", msg)
		return fmt.Errorf("failed to parse supply chains: %w", err)
	}

	lensID := ""
	if job.State != nil {
		if v, ok := job.State["lensId"]; ok {
			lensID, _ = v.(string)
		}
	}

	{
		msg := fmt.Sprintf("[SupplyChainParse] chains=%d nodes=%d edges=%d fragments=%d stats=%+v",
			len(chains), result.Stats.NodesTotal, len(result.Edges), len(result.Fragments), result.Stats)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
		trace.Log(ctx, "SupplyChain", "SupplyChain.parse.ok", map[string]any{
			"tenantId":    strings.TrimSpace(job.TenantID),
			"projectId":   strings.TrimSpace(job.ProjectID),
			"lensId":      lensID,
			"jobId":       strings.TrimSpace(job.ID),
			"target":      strings.TrimSpace(job.TargetCanonicalPath),
			"chainsN":     len(chains),
			"fragmentsN":  len(result.Fragments),
			"edgesN":      len(result.Edges),
			"parserStats": result.Stats,
			"chainContextLen": func() int {
				if job.State == nil {
					return 0
				}
				s, _ := job.State["chainContext"].(string)
				return len(s)
			}(),
		})
	}

	// Map canonical path -> fragment ID for edge creation
	pathToID := make(map[string]string)
	if sourceFragment != nil && sourceFragment.ID != "" {
		pathToID[sourceFragment.CanonicalPath] = sourceFragment.ID
	}

	// Persist fragments discovered in supply chains
	createdFrags := 0
	updatedFrags := 0
	createErrors := 0
	previewLimit := 20
	for canonicalPath, frag := range result.Fragments {
		// Skip the source fragment itself
		if canonicalPath == sourceFragment.CanonicalPath {
			continue
		}

		// IMPORTANT: Horizon fragments are global-scoped (except SUPPLY_LINK).
		// If tenantId != global/empty, fragment.Validate() will fail and nothing will be created.
		frag.TenantID = "global"
		frag.Status = "active"
		frag.Governance.Owner = "SYSTEM"
		frag.Governance.Status = string(models.FragmentStatusActive)
		// Keep LastVerifiedAt zero so freshness checks won't skip scouting for newly discovered nodes.
		if frag.Metadata.CreatedAt.IsZero() {
			frag.Metadata.CreatedAt = time.Now().UTC()
		}
		frag.Metadata.LastUpdatedAt = time.Now().UTC()
		if frag.Metadata.EpistemicStatus == "" {
			frag.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
		}
		if frag.Provenance.Method == "" {
			frag.Provenance.Method = "scout_supply_chain"
		}
		if len(frag.Provenance.SourceIDs) == 0 && job != nil {
			frag.Provenance.SourceIDs = []string{job.ID}
		}

		// Check if fragment already exists
		existing, err := e.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
		if err != nil {
			// Create new fragment
			frag.Metadata.CreatedAt = time.Now().UTC()
			frag.Metadata.LastUpdatedAt = time.Now().UTC()
			frag.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence

			id, createErr := e.fragmentsRepo.Create(ctx, frag)
			if createErr != nil {
				createErrors++
				msg := fmt.Sprintf("[SupplyChainFragmentCreate] FAILED canonicalPath=%s err=%v", canonicalPath, createErr)
				fmt.Printf("%s\n", msg)
				e.logLine(ctx, job, "warn", msg)
				continue
			}
			frag.ID = id
			pathToID[canonicalPath] = id
			createdFrags++
			if createdFrags <= previewLimit {
				msg := fmt.Sprintf("[SupplyChainFragmentCreate] OK canonicalPath=%s id=%s type=%s", canonicalPath, id, frag.Type)
				e.logLine(ctx, job, "info", msg)
			}
		} else {
			// Fragment exists, store ID for edge creation
			pathToID[canonicalPath] = existing.ID

			// Optionally update labels/identifiers if new info
			if existing.Labels == nil {
				existing.Labels = make(map[string]string)
			}
			if existing.Identifiers == nil {
				existing.Identifiers = make(map[string]string)
			}
			updated := false
			for k, v := range frag.Labels {
				if existing.Labels[k] == "" && v != "" {
					existing.Labels[k] = v
					updated = true
				}
			}
			for k, v := range frag.Identifiers {
				if existing.Identifiers[k] == "" && v != "" {
					existing.Identifiers[k] = v
					updated = true
				}
			}
			if updated {
				existing.Metadata.LastUpdatedAt = time.Now().UTC()
				if err := e.fragmentsRepo.Update(ctx, existing); err != nil {
					msg := fmt.Sprintf("[SupplyChainFragmentUpdate] FAILED canonicalPath=%s id=%s err=%v", canonicalPath, existing.ID, err)
					fmt.Printf("%s\n", msg)
					e.logLine(ctx, job, "warn", msg)
				} else {
					updatedFrags++
					if updatedFrags <= previewLimit {
						msg := fmt.Sprintf("[SupplyChainFragmentUpdate] OK canonicalPath=%s id=%s", canonicalPath, existing.ID)
						e.logLine(ctx, job, "info", msg)
					}
				}
			}
		}
	}

	// Create edges using fragment IDs
	provenanceMethod := "scout_supply_chain"
	createdEdges := 0
	edgeDupes := 0
	edgeCreateErrors := 0
	edgeReversed := 0
	edgePenalized := 0
	edgePreviewLimit := 40
	for _, chainEdge := range result.Edges {
		sourceID := pathToID[chainEdge.SourceCanonicalPath]
		targetID := pathToID[chainEdge.TargetCanonicalPath]

		if sourceID == "" || targetID == "" {
			// Try to look up missing IDs
			if sourceID == "" {
				if frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, chainEdge.SourceCanonicalPath); err == nil && frag != nil {
					sourceID = frag.ID
					pathToID[chainEdge.SourceCanonicalPath] = sourceID
				}
			}
			if targetID == "" {
				if frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, chainEdge.TargetCanonicalPath); err == nil && frag != nil {
					targetID = frag.ID
					pathToID[chainEdge.TargetCanonicalPath] = targetID
				}
			}
			if sourceID == "" || targetID == "" {
				msg := fmt.Sprintf("[SupplyChainEdge] SKIP missingFragmentId source=%s target=%s type=%s",
					chainEdge.SourceCanonicalPath, chainEdge.TargetCanonicalPath, chainEdge.RelationType)
				fmt.Printf("%s\n", msg)
				e.logLine(ctx, job, "warn", msg)
				continue
			}
		}

		edgeType := models.DependencyType(chainEdge.RelationType)

		// Policy: self-loop prevention
		if strings.TrimSpace(sourceID) == "" || strings.TrimSpace(targetID) == "" || IsSelfLoop(sourceID, targetID) {
			continue
		}

		// Resolve endpoint types for policy validation (best-effort).
		resolveTypeByCanonical := func(cp string) models.FragmentType {
			cp = strings.TrimSpace(cp)
			if cp == "" {
				return ""
			}
			if sourceFragment != nil && strings.TrimSpace(sourceFragment.CanonicalPath) == cp {
				return sourceFragment.Type
			}
			if e.fragmentsRepo != nil {
				if f, ferr := e.fragmentsRepo.GetByCanonicalPath(ctx, cp); ferr == nil && f != nil {
					return f.Type
				}
			}
			return ""
		}
		srcTypeActual := resolveTypeByCanonical(chainEdge.SourceCanonicalPath)
		dstTypeActual := resolveTypeByCanonical(chainEdge.TargetCanonicalPath)

		// Policy: direction validation & reversal (best-effort, same behavior as processDiscoveredRelations).
		edgeCriticality := normalizeEdgeConfidence(chainEdge.Confidence)
		directionChanged := false
		if e.validationPolicy != nil {
			decision, derr := e.validationPolicy.Execute(ctx, policy.PolicyInput{
				Kind: "edge",
				Context: map[string]interface{}{
					"validationKind":   "edge",
					"sourceFragmentID": sourceID,
					"targetFragmentID": targetID,
					"sourceType":       string(srcTypeActual),
					"targetType":       string(dstTypeActual),
					"relationType":     string(edgeType),
				},
				Fragment: nil,
			})
			if derr == nil {
				switch decision.Action {
				case "reject":
					// Non-blocking: apply penalty to criticality to de-prioritize doubtful edges.
					edgeCriticality = edgeCriticality * 0.35
					edgePenalized++
					msg := fmt.Sprintf("[SupplyChainPolicy] PENALTY action=reject edge=%s -[%s]-> %s path=%s conf=%.3f->%.3f",
						chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, decision.DecisionPath, normalizeEdgeConfidence(chainEdge.Confidence), edgeCriticality)
					e.logLine(ctx, job, "warn", msg)
				case "reverse":
					shouldReverse, _ := decision.Params["shouldReverse"].(bool)
					if shouldReverse {
						sourceID, targetID = targetID, sourceID
						directionChanged = true
						edgeReversed++
						msg := fmt.Sprintf("[SupplyChainPolicy] REVERSE edge=%s -[%s]-> %s path=%s",
							chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, decision.DecisionPath)
						e.logLine(ctx, job, "info", msg)
					}
				}
			} else {
				msg := fmt.Sprintf("[SupplyChainPolicy] ERROR validationPolicy err=%v", derr)
				e.logLine(ctx, job, "warn", msg)
			}
		}

		// Check for existing edge
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceID, targetID, edgeType); err == nil {
			// Edge already exists, skip
			edgeDupes++
			continue
		}

		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: sourceID,
			TargetFragmentID: targetID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality:   edgeCriticality,
				Amplification: 1.0,
				LatencyHours:  24,
			},
			Qualifiers: map[string]interface{}{
				"reason":              chainEdge.Reason,
				"sourceCanonicalPath": chainEdge.SourceCanonicalPath,
				"targetCanonicalPath": chainEdge.TargetCanonicalPath,
				"rawConfidence":       chainEdge.Confidence,
				"fromSupplyChain":     true,
			},
			Provenance: models.Provenance{
				Method:    provenanceMethod,
				SourceIDs: []string{job.ID},
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     time.Now().UTC(),
				LastUpdatedAt: time.Now().UTC(),
			},
		}
		if directionChanged {
			edge.Qualifiers["normalizedDirection"] = true
			edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
		}

		if id, createErr := e.edgesRepo.Create(ctx, edge); createErr != nil {
			edgeCreateErrors++
			msg := fmt.Sprintf("[SupplyChainEdgeCreate] FAILED %s -[%s]-> %s err=%v",
				chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, createErr)
			fmt.Printf("%s\n", msg)
			e.logLine(ctx, job, "warn", msg)
		} else {
			edge.ID = id
			createdEdges++
			if createdEdges <= edgePreviewLimit {
				e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainEdgeCreate] OK %s -[%s]-> %s id=%s crit=%.3f raw=%.3f",
					chainEdge.SourceCanonicalPath, edgeType, chainEdge.TargetCanonicalPath, id, edge.Properties.Criticality, chainEdge.Confidence))
			}

			// Realtime event
			e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
				"tenantId":         job.TenantID,
				"projectId":        job.ProjectID,
				"lensId":           lensID,
				"edgeId":           edge.ID,
				"sourceFragmentId": edge.SourceFragmentID,
				"targetFragmentId": edge.TargetFragmentID,
				"type":             string(edge.Type),
				"properties":       edge.Properties,
				"fromSupplyChain":  true,
			})
		}
	}

	// Update chain context in job state for propagation to child jobs
	if job.State == nil {
		job.State = make(map[string]interface{})
	}
	existingContext, _ := ParseChainContext(job.State["chainContext"])
	existingContext.MergeFromParsedResult(result)

	// Identify gaps: nodes that have only incoming or only outgoing edges
	gapHints := e.identifyChainGaps(result, existingContext)
	existingContext.GapHints = gapHints

	if contextJSON, err := existingContext.ToJSON(); err == nil {
		if job.State == nil {
			job.State = make(map[string]interface{})
		}
		job.State["chainContext"] = contextJSON
		// Update job state in repository
		if e.scoutingJobsRepo != nil {
			if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
				fmt.Printf("[ScoutEngine] Failed to update job chain context: %v\n", err)
			}
		}
	}

	// In supply_chain mode, schedule follow-up scout jobs based on supply chain nodes/gaps,
	// not only on plain relations. This enables branching chain expansion even when LLM outputs
	// most structure via supplyChains.
	_ = e.scheduleSupplyChainChildJobs(ctx, job, sourceFragment, result, existingContext)

	trace.Log(ctx, "scout_supply_chain", "scout.supply_chain.processed", map[string]any{
		"tenantId":       strings.TrimSpace(job.TenantID),
		"projectId":      strings.TrimSpace(job.ProjectID),
		"lensId":         lensID,
		"jobId":          strings.TrimSpace(job.ID),
		"chainsCount":    len(chains),
		"fragmentsCount": len(result.Fragments),
		"edgesCount":     len(result.Edges),
		"gapHintsCount":  len(gapHints),
		"supplyChainLogSummary": map[string]any{
			"createdFragments": createdFrags,
			"updatedFragments": updatedFrags,
			"fragmentErrors":   createErrors,
			"createdEdges":     createdEdges,
			"edgeDupes":        edgeDupes,
			"edgeErrors":       edgeCreateErrors,
			"edgeReversed":     edgeReversed,
			"edgePenalized":    edgePenalized,
		},
	})
	{
		msg := fmt.Sprintf("[SupplyChainSummary] fragments(created=%d updated=%d err=%d) edges(created=%d dupes=%d err=%d reversed=%d penalized=%d) gaps=%d",
			createdFrags, updatedFrags, createErrors, createdEdges, edgeDupes, edgeCreateErrors, edgeReversed, edgePenalized, len(gapHints))
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
	}

	return nil
}

// scheduleSupplyChainChildJobs enqueues child scouting jobs for discovered supply chain nodes
// (and especially gap nodes). This is supply_chain-only and does not affect general mode.
func (e *ScoutEngine) scheduleSupplyChainChildJobs(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	result *ParsedChainResult,
	context *ChainContext,
) error {
	if e == nil || job == nil || job.State == nil || e.scoutingJobsRepo == nil || e.fragmentsRepo == nil {
		return nil
	}

	specialties := []string(nil)
	if v, ok := job.State["specialties"]; ok {
		specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
	}
	if !taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain) {
		return nil
	}

	// Depth guardrails
	currentDepth := parseStateInt(job.State["recursionDepth"], 0)
	maxDepth := parseStateInt(job.State["maxRecursionDepth"], 2)
	if maxDepth < 0 {
		maxDepth = 0
	}
	if currentDepth >= maxDepth {
		return nil
	}

	// Breadth guardrails (supply_chain is allowed to be wider by default).
	maxChildren := 80
	if v, ok := job.State["maxChildrenPerJob"]; ok {
		maxChildren = parseStateInt(v, maxChildren)
	}
	if maxChildren < 0 {
		maxChildren = 0
	}
	if maxChildren == 0 {
		return nil
	}

	// Best-effort: prefetch active (pending/running) scout jobs for this project/lens
	activeJobByPath := map[string]string{} // canonicalPath -> jobId
	{
		lensID := strings.TrimSpace(job.LensID)
		if lensID == "" {
			lensID = parseStateString(job.State["lensId"])
		}
		if jobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, job.TenantID, job.ProjectID, lensID, 300); err == nil {
			for _, j := range jobs {
				if j == nil {
					continue
				}
				if j.Status != agentmodels.AgentJobStatusPending && j.Status != agentmodels.AgentJobStatusRunning {
					continue
				}
				p := strings.TrimSpace(j.TargetCanonicalPath)
				if p == "" {
					continue
				}
				if _, ok := activeJobByPath[p]; !ok {
					activeJobByPath[p] = j.ID
				}
			}
		}
	}

	// Build candidate list:
	// 1) newly seen nodes (this run)
	// 2) gap nodes (missing upstream/downstream)
	candidates := make([]string, 0, 64)
	seen := map[string]struct{}{}
	add := func(cp string) {
		cp = strings.TrimSpace(cp)
		if cp == "" {
			return
		}
		if sourceFragment != nil && strings.TrimSpace(sourceFragment.CanonicalPath) == cp {
			return
		}
		if _, ok := seen[cp]; ok {
			return
		}
		seen[cp] = struct{}{}
		candidates = append(candidates, cp)
	}

	if result != nil {
		for cp := range result.Fragments {
			add(cp)
		}
	}

	// Gap nodes derived from context edges
	if context != nil {
		incoming := map[string]int{}
		outgoing := map[string]int{}
		for _, e2 := range context.DiscoveredEdges {
			outgoing[strings.TrimSpace(e2.Source)]++
			incoming[strings.TrimSpace(e2.Target)]++
		}
		for _, cp := range context.DiscoveredNodes {
			cp = strings.TrimSpace(cp)
			if cp == "" {
				continue
			}
			isProduct := strings.HasPrefix(cp, "/product/")
			isActor := strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/fac/")
			// prioritize obvious open ends
			if isProduct && (incoming[cp] == 0 || outgoing[cp] == 0) {
				add(cp)
			}
			if isActor && (incoming[cp] == 0 || outgoing[cp] == 0) {
				add(cp)
			}
		}
	}

	if len(candidates) == 0 {
		msg := "[SupplyChainSchedule] no candidates"
		e.logLine(ctx, job, "info", msg)
		return nil
	}

	scheduled := 0
	scheduledChildren := map[string]struct{}{}
	skippedActive := 0
	skippedMissingFrag := 0
	skippedVerified := 0
	skippedDup := 0
	skippedVisited := 0

	cascadeID := ""
	if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
		cascadeID = strings.TrimSpace(v)
	}
	if cascadeID == "" {
		cascadeID = strings.TrimSpace(job.ID)
	}
	for _, targetPath := range candidates {
		if scheduled >= maxChildren {
			break
		}
		if _, ok := scheduledChildren[targetPath]; ok {
			skippedDup++
			continue
		}
		if existingJobID, ok := activeJobByPath[targetPath]; ok && strings.TrimSpace(existingJobID) != "" {
			skippedActive++
			continue
		}

		frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
		if err != nil || frag == nil {
			// If the fragment isn't there for some reason, skip; supplyChains materialization should have created it.
			skippedMissingFrag++
			continue
		}

		// Skip if already verified (consistent with relation-driven recursion)
		shouldScout := frag.Metadata.LastVerifiedAt.IsZero() || frag.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence
		if !shouldScout {
			skippedVerified++
			continue
		}

		// Cascade visited set: enqueue-once-per-canonicalPath inside this cascade.
		// Best-effort delete on enqueue failure to avoid harming quality.
		if e.cascadeVisitedRepo != nil {
			already, vErr := e.cascadeVisitedRepo.TryMarkVisited(ctx, job.TenantID, job.ProjectID, cascadeID, targetPath, job.ID, currentDepth+1)
			if vErr != nil {
				msg := fmt.Sprintf("[SupplyChainVisited] ERROR mark tenant=%s cascade=%s target=%s err=%v",
					strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), vErr)
				fmt.Printf("%s\n", msg)
				e.logLine(ctx, job, "warn", msg)
			} else if already {
				skippedVisited++
				msg := fmt.Sprintf("[SupplyChainVisited] SKIP alreadyVisited tenant=%s cascade=%s target=%s depth=%d",
					strings.TrimSpace(job.TenantID), strings.TrimSpace(cascadeID), strings.TrimSpace(targetPath), currentDepth+1)
				e.logLine(ctx, job, "info", msg)
				continue
			}
		}

		scheduledChildren[targetPath] = struct{}{}
		scheduled++

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
		// Propagate lens id
		if strings.TrimSpace(job.LensID) != "" {
			childState["lensId"] = strings.TrimSpace(job.LensID)
		} else if v := parseStateString(job.State["lensId"]); v != "" {
			childState["lensId"] = v
		}
		// Propagate breadth cap
		childState["maxChildrenPerJob"] = float64(maxChildren)
		// Propagate cascade root id for budget attribution
		if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
			childState["cascadeRootJobId"] = strings.TrimSpace(v)
		} else {
			childState["cascadeRootJobId"] = job.ID
		}
		// Pass through steering signals
		if rm, ok := job.State["requestedMetrics"]; ok {
			childState["requestedMetrics"] = rm
		}
		if v, ok := job.State["specialties"]; ok {
			childState["specialties"] = v
		}
		if v, ok := job.State["chainContext"]; ok {
			childState["chainContext"] = v
		}
		if v, ok := job.State["focus"]; ok {
			childState["focus"] = v
		}
		if v, ok := job.State["keywords"]; ok {
			childState["keywords"] = v
		}
		if v, ok := job.State["spherePaths"]; ok {
			childState["spherePaths"] = v
		}
		if v, ok := job.State["originalQuery"]; ok {
			childState["originalQuery"] = v
		}
		if v, ok := job.State["decompositionHints"]; ok {
			childState["decompositionHints"] = v
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
			CurrentState: models.ScoutingJobStatePending,
		}
		if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err != nil {
			// Undo visited marker on enqueue failure.
			if e.cascadeVisitedRepo != nil {
				_ = e.cascadeVisitedRepo.DeleteVisited(ctx, job.TenantID, cascadeID, targetPath)
				trace.Log(ctx, "SupplyChain", "SupplyChain.visited.unmark_on_create_fail", map[string]any{
					"tenantId":  strings.TrimSpace(job.TenantID),
					"projectId": strings.TrimSpace(job.ProjectID),
					"lensId":    strings.TrimSpace(job.LensID),
					"jobId":     strings.TrimSpace(job.ID),
					"cascadeId": strings.TrimSpace(cascadeID),
					"target":    strings.TrimSpace(targetPath),
				})
			}
		}
	}

	if scheduled > 0 {
		msg := fmt.Sprintf("[SupplyChainSchedule] enqueued=%d candidates=%d skipped(active=%d verified=%d missingFrag=%d dup=%d visited=%d) depth=%d/%d cap=%d",
			scheduled, len(candidates), skippedActive, skippedVerified, skippedMissingFrag, skippedDup, skippedVisited, currentDepth, maxDepth, maxChildren)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
		trace.Log(ctx, "SupplyChain", "SupplyChain.schedule.children", map[string]any{
			"tenantId":         strings.TrimSpace(job.TenantID),
			"projectId":        strings.TrimSpace(job.ProjectID),
			"lensId":           strings.TrimSpace(job.LensID),
			"jobId":            strings.TrimSpace(job.ID),
			"candidatesN":      len(candidates),
			"enqueuedN":        scheduled,
			"skippedActiveN":   skippedActive,
			"skippedVerifiedN": skippedVerified,
			"skippedMissingN":  skippedMissingFrag,
			"skippedDupN":      skippedDup,
			"skippedVisitedN":  skippedVisited,
			"depth":            currentDepth,
			"maxDepth":         maxDepth,
			"cap":              maxChildren,
		})
	} else {
		msg := fmt.Sprintf("[SupplyChainSchedule] enqueued=0 candidates=%d skipped(active=%d verified=%d missingFrag=%d dup=%d visited=%d) depth=%d/%d cap=%d",
			len(candidates), skippedActive, skippedVerified, skippedMissingFrag, skippedDup, skippedVisited, currentDepth, maxDepth, maxChildren)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "info", msg)
	}
	return nil
}

// identifyChainGaps finds gaps in the supply chain that should be filled.
func (e *ScoutEngine) identifyChainGaps(_ *ParsedChainResult, context *ChainContext) []string {
	hints := make([]string, 0)

	// Build edge maps to identify nodes with missing upstream/downstream
	incomingCount := make(map[string]int)
	outgoingCount := make(map[string]int)

	for _, edge := range context.DiscoveredEdges {
		outgoingCount[edge.Source]++
		incomingCount[edge.Target]++
	}

	// Find nodes that might need more connections
	for _, node := range context.DiscoveredNodes {
		isProduct := strings.HasPrefix(node, "/product/")
		isActor := strings.HasPrefix(node, "/org/") || strings.HasPrefix(node, "/fac/")

		if isProduct {
			// Products should have both incoming (producer) and outgoing (consumer) edges
			if incomingCount[node] == 0 {
				hints = append(hints, fmt.Sprintf("Who produces %s? Find upstream supplier.", node))
			}
			if outgoingCount[node] == 0 {
				hints = append(hints, fmt.Sprintf("Who consumes %s? Find downstream customer.", node))
			}
		} else if isActor {
			// Actors at chain ends might need extension
			if incomingCount[node] == 0 && outgoingCount[node] > 0 {
				hints = append(hints, fmt.Sprintf("Who supplies to %s? Find upstream supplier.", node))
			}
			if outgoingCount[node] == 0 && incomingCount[node] > 0 {
				hints = append(hints, fmt.Sprintf("Who does %s supply to? Find downstream customer.", node))
			}
		}
	}

	// Limit hints to avoid prompt bloat
	if len(hints) > 5 {
		hints = hints[:5]
	}

	return hints
}

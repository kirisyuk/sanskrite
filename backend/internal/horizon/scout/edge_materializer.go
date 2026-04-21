package scout

import (
	"context"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
	"time"
)

// materializeRelationEdge centralizes edge normalization/validation/persistence for discovered relations.
// It is used by processRelationsInternal so Phase 1 and strategic projection share one materialization path.
//
// IMPORTANT: this function is intentionally "fail-open" for non-critical issues:
// - it will skip invalid edges rather than failing the whole job
// - it will log create failures loudly (same behavior as before refactor)
func (e *ScoutEngine) materializeRelationEdge(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	targetFragment *models.HorizonFragment,
	targetID string,
	targetType FragmentType,
	targetPath string,
	rel agents.DiscoveredRelation,
	rawRelationType string,
	normalizedRelationType string,
	provenanceMethod string,
	baseQualifiers map[string]interface{},
	enableManagement bool,
	seenEdgeKeys map[string]struct{},
) error {
	if e == nil || job == nil || sourceFragment == nil || strings.TrimSpace(sourceFragment.ID) == "" {
		return nil
	}
	if e.edgesRepo == nil {
		return nil
	}
	if strings.TrimSpace(targetID) == "" || strings.TrimSpace(targetPath) == "" {
		return nil
	}

	edgeType := DependencyType(strings.TrimSpace(rel.RelationType))
	edgeSourceID := sourceFragment.ID
	edgeTargetID := targetID
	relConfidence := rel.Confidence
	policyStatus := models.PolicyStatusClean
	policyReason := ""
	policyPenalty := 0.0
	policyEdgeModality := ""

	// === EXPLICIT SOURCE HANDLING (Phase 2 strategic relations) ===
	explicitSourceFragment := sourceFragment
	if explicitSourcePath := strings.TrimSpace(rel.SourceCanonicalPath); explicitSourcePath != "" {
		// Fast-path: if the explicit source matches the current source fragment, don't do extra lookups.
		if strings.TrimSpace(sourceFragment.CanonicalPath) == explicitSourcePath {
			// no-op
		} else if e.fragmentsRepo == nil {
			// no-op
		} else {
			// Find or create the explicit source fragment
			if existingSrc, err := e.fragmentsRepo.GetByCanonicalPath(ctx, explicitSourcePath); err == nil && existingSrc != nil {
				explicitSourceFragment = existingSrc
				edgeSourceID = existingSrc.ID
				if sg := SessionGraphFromContext(ctx); sg != nil {
					sg.Add(existingSrc)
				}
				fmt.Printf("[ScoutEngine] Using explicit source for edge: %s -> %s [%s]\n", explicitSourcePath, targetPath, edgeType)
			} else {
				// Create minimal fragment for explicit source
				srcType := e.normalizeFragmentType(ctx, rel.SourceType)
				if srcType == "" {
					srcType = InferFragmentTypeFromCanonicalPath(explicitSourcePath)
				}
				if srcType == "" {
					srcType = ah.FragmentClassOrg
				}
				now := time.Now().UTC()
				srcName := strings.TrimSpace(rel.SourceName)
				if srcName == "" {
					parts := strings.Split(strings.Trim(explicitSourcePath, "/"), "/")
					if len(parts) > 0 {
						srcName = parts[len(parts)-1]
					}
				}
				newSrcFrag := &models.HorizonFragment{
					TenantID:      "global",
					CanonicalPath: explicitSourcePath,
					Class:         srcType,
					Labels:        map[string]string{"en": srcName},
					Status:        "active",
					Governance: models.Governance{
						Owner:  "SYSTEM",
						Status: string(models.FragmentStatusActive),
					},
					Metadata: models.FragmentMetadata{
						CreatedAt:       now,
						LastUpdatedAt:   now,
						SourceFrameID:   provenanceMethod,
						EpistemicStatus: models.EpistemicStatusLowConfidence,
					},
					Provenance: models.Provenance{
						SourceIDs: []string{"explicit_source_" + provenanceMethod},
						Method:    provenanceMethod,
					},
				}
				if createdID, createErr := e.fragmentsRepo.Create(ctx, newSrcFrag); createErr == nil {
					edgeSourceID = createdID
					explicitSourceFragment = newSrcFrag
					if sg := SessionGraphFromContext(ctx); sg != nil {
						sg.Add(newSrcFrag)
					}
					fmt.Printf("[ScoutEngine] Created explicit source fragment: %s (%s)\n", explicitSourcePath, srcType)
				} else {
					fmt.Printf("[ScoutEngine] Failed to create explicit source fragment %s: %v\n", explicitSourcePath, createErr)
					explicitSourceFragment = sourceFragment
					edgeSourceID = sourceFragment.ID
				}
			}
		}
		// When explicit source is used and target is THIS FRAGMENT, set target to sourceFragment.
		if strings.TrimSpace(rel.TargetCanonicalPath) == strings.TrimSpace(sourceFragment.CanonicalPath) {
			edgeTargetID = sourceFragment.ID
			targetType = sourceFragment.Class
		}
	}

	// Enforce directional semantics for common relations (best-effort, heuristic).
	// Pass qualifiers for role-based swap when LLM outputs inverted direction (e.g. subsidiary->owner for OWNS).
	edgeSourceID, edgeTargetID, edgeType, directionChanged := normalizeRelationEdge(explicitSourceFragment, edgeSourceID, edgeTargetID, edgeType, targetType, enableManagement, rel.Qualifiers)
	switch edgeType {
	case "SUBSIDIARY_OF":
		edgeType = ah.RelationNameBeneficiallyOwns
		edgeSourceID, edgeTargetID = targetID, sourceFragment.ID
		directionChanged = true
	}

	// Detect likely inverted ownership when no explicit source (e.g. "owned by" in Reason).
	if rel.SourceCanonicalPath == "" {
		if detectLikelyInvertedOwnershipEdge(edgeType, rel) {
			edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
			directionChanged = true
		}
	}

	// Policy: self-loop prevention (reject).
	if IsSelfLoop(edgeSourceID, edgeTargetID) {
		fmt.Printf("Skipping self-edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
		return nil
	}

	// Policy: symmetric edge normalization (avoid A->B and B->A duplicates).
	edgeSourceID, edgeTargetID, edgeType, symmetricSwapped := NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
	if symmetricSwapped {
		directionChanged = true
	}
	earlyDedupVariant := edgeDedupVariant(edgeType, rawRelationType, rel.Qualifiers, rel.Reason)
	earlyDedupKey := edgeDedupKey(edgeSourceID, edgeTargetID, edgeType, earlyDedupVariant)
	if e.seenEdgeDedupRecently(earlyDedupKey) {
		fmt.Printf("Skipping duplicate edge (cross-job recent cache): %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
		return nil
	}
	// Early in-memory dedup: skip canonicalization+validation for edges already seen in this job.
	if seenEdgeKeys != nil {
		if _, ok := seenEdgeKeys[earlyDedupKey]; ok {
			fmt.Printf("Skipping duplicate edge (early in-memory): %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			e.rememberEdgeDedupRecently(earlyDedupKey)
			return nil
		}
	}

	// Resolve *actual* endpoint types for validation (must match edgeSourceID/edgeTargetID after swaps).
	resolveTypeByID := func(id string) FragmentType {
		id = strings.TrimSpace(id)
		if id == "" {
			return ""
		}
		resolveFromPath := func(path string, current FragmentType) FragmentType {
			inferred := InferFragmentTypeFromCanonicalPath(strings.TrimSpace(path))
			if inferred == "" {
				return current
			}
			// /gov/* stays ORGANIZATION by design, so this reconciliation is safe.
			if current == "" || current != inferred {
				return inferred
			}
			return current
		}
		if explicitSourceFragment != nil && strings.TrimSpace(explicitSourceFragment.ID) == id {
			return resolveFromPath(explicitSourceFragment.CanonicalPath, explicitSourceFragment.Class)
		}
		if sourceFragment != nil && strings.TrimSpace(sourceFragment.ID) == id {
			return resolveFromPath(sourceFragment.CanonicalPath, sourceFragment.Class)
		}
		if targetFragment != nil && strings.TrimSpace(targetFragment.ID) == id {
			return resolveFromPath(targetFragment.CanonicalPath, targetFragment.Class)
		}
		if e.fragmentsRepo != nil {
			if f, ferr := e.fragmentsRepo.Get(ctx, id); ferr == nil && f != nil {
				return resolveFromPath(f.CanonicalPath, f.Class)
			}
		}
		return ""
	}
	resolvePathByID := func(id string) string {
		id = strings.TrimSpace(id)
		if id == "" {
			return ""
		}
		if explicitSourceFragment != nil && strings.TrimSpace(explicitSourceFragment.ID) == id {
			return strings.TrimSpace(explicitSourceFragment.CanonicalPath)
		}
		if sourceFragment != nil && strings.TrimSpace(sourceFragment.ID) == id {
			return strings.TrimSpace(sourceFragment.CanonicalPath)
		}
		if targetFragment != nil && strings.TrimSpace(targetFragment.ID) == id {
			return strings.TrimSpace(targetFragment.CanonicalPath)
		}
		if e.fragmentsRepo != nil {
			if f, ferr := e.fragmentsRepo.Get(ctx, id); ferr == nil && f != nil {
				return strings.TrimSpace(f.CanonicalPath)
			}
		}
		return ""
	}
	srcTypeActual := resolveTypeByID(edgeSourceID)
	dstTypeActual := resolveTypeByID(edgeTargetID)

	// Unified canonicalization via policy (includes REGULATES /gov swap, transition fallback, unknown type fallback).
	srcPathActual := resolvePathByID(edgeSourceID)
	dstPathActual := resolvePathByID(edgeTargetID)
	canonFromEdgeType := ""
	canonReason := ""
	isSupplyChainMode := taxonomy.HasDomainMode(getJobDomainModes(job), taxonomy.DomainModeSupplyChain) ||
		taxonomy.HasDomainMode(getJobDomainModes(job), taxonomy.DomainModeServiceChain)
	canon := policy.ApplyEdgeCanonicalizationPolicy(policy.EdgeCanonicalizationInput{
		EdgeType:          edgeType,
		SourceType:        srcTypeActual,
		TargetType:        dstTypeActual,
		SourcePath:        srcPathActual,
		TargetPath:        dstPathActual,
		RawRelationType:   rawRelationType,
		Reason:            strings.TrimSpace(rel.Reason),
		Qualifiers:        rel.Qualifiers,
		OwnershipEvidence: HasOwnershipEvidence(rel.Reason, rel.Qualifiers, rel.Metrics),
		SupplyChainMode:   isSupplyChainMode,
	})
	if canon.Changed {
		if canon.OriginalType != "" {
			canonFromEdgeType = string(canon.OriginalType)
		} else {
			canonFromEdgeType = string(edgeType)
		}
		canonReason = canon.Reason
		edgeType = canon.EdgeType
		directionChanged = true
		if canon.ShouldSwap {
			edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
			srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
			srcPathActual, dstPathActual = dstPathActual, srcPathActual
		}
		edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
	}
	if canon.PolicyPenalty > 0 {
		relConfidence = relConfidence * canon.ConfidenceMult
		if relConfidence < 0 {
			relConfidence = 0
		}
		if canon.PolicyPenalty > policyPenalty {
			policyPenalty = canon.PolicyPenalty
		}
		policyStatus = policy.MergePolicyStatus(policyStatus, canon.PolicyStatus)
		if strings.TrimSpace(canon.PolicyReason) != "" {
			policyReason = strings.TrimSpace(canon.PolicyReason)
		}
		if canon.PolicyReason == "transition_unclassified" || canon.PolicyReason == "unknown_edge_type" {
			tenantID := "global"
			if sourceFragment != nil && strings.TrimSpace(sourceFragment.TenantID) != "" {
				tenantID = strings.TrimSpace(sourceFragment.TenantID)
			}
			policy.LogPolicyEvent("edge_canonicalization_policy", policy.PolicyLogStageFallback, map[string]interface{}{
				"trace_id":       "na",
				"tenant_id":      tenantID,
				"job_id":         job.ID,
				"edge_id":        "",
				"source_type":    srcTypeActual,
				"target_type":    dstTypeActual,
				"relation_type":  edgeType,
				"action":         policy.ActionAllowWithPenalty,
				"policy_status":  policyStatus,
				"policy_reason":  policyReason,
				"policy_penalty": policyPenalty,
				"decision_path":  "runtime_scout_edge_materialization",
				"confidence":     canon.ConfidenceMult,
			})
		}
	}
	effectiveQualifiers, _ := withDeterministicDirectionalRoleQualifiers(edgeType, srcTypeActual, dstTypeActual, rel.Qualifiers)

	// Policy: direction validation and reversal (enforced for known edge types).
	if e.validationPolicy != nil {
		logPrefix := "[ScoutEngine:materializeEdge]"
		policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageInput, map[string]interface{}{
			"trace_id":       "na",
			"tenant_id":      job.TenantID,
			"job_id":         job.ID,
			"source_type":    srcTypeActual,
			"target_type":    dstTypeActual,
			"relation_type":  edgeType,
			"action":         "validate_edge",
			"policy_status":  "na",
			"policy_reason":  "",
			"policy_penalty": 0,
			"decision_path":  "runtime_scout_edge_materialization",
			"confidence":     0,
		})
		decision, err := e.validationPolicy.Execute(ctx, policy.PolicyInput{
			Kind: "edge",
			Context: map[string]interface{}{
				"validationKind":      "edge",
				"sourceFragmentID":    edgeSourceID,
				"targetFragmentID":    edgeTargetID,
				"sourceType":          string(srcTypeActual),
				"targetType":          string(dstTypeActual),
				"sourceCanonicalPath": srcPathActual,
				"targetCanonicalPath": dstPathActual,
				"relationType":        string(edgeType),
				"reason":              strings.TrimSpace(rel.Reason),
				"qualifiers":          effectiveQualifiers,
			},
			Fragment: nil,
		})
		if err == nil {
			reason := ""
			if rv, _ := decision.Params["reason"].(string); strings.TrimSpace(rv) != "" {
				reason = strings.TrimSpace(rv)
			}
			policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageDecision, map[string]interface{}{
				"trace_id":       "na",
				"tenant_id":      job.TenantID,
				"job_id":         job.ID,
				"source_type":    srcTypeActual,
				"target_type":    dstTypeActual,
				"relation_type":  edgeType,
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
					edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
					srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
					srcPathActual, dstPathActual = dstPathActual, srcPathActual
					directionChanged = true
					edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
				},
				RewriteType: func(newType string, _ string) {
					newEdgeType := DependencyType(strings.TrimSpace(newType))
					if newEdgeType == "" || newEdgeType == edgeType {
						return
					}
					canonFromEdgeType = string(edgeType)
					edgeType = newEdgeType
					directionChanged = true
					edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
				},
				ApplyPenalty: func(penalty float64, _ string) {
					relConfidence = relConfidence * (1.0 - penalty)
					if relConfidence < 0 {
						relConfidence = 0
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
		} else {
			policy.LogPolicyEvent("edge_validation_policy", policy.PolicyLogStageError, map[string]interface{}{
				"trace_id":       "na",
				"tenant_id":      job.TenantID,
				"job_id":         job.ID,
				"source_type":    srcTypeActual,
				"target_type":    dstTypeActual,
				"relation_type":  edgeType,
				"action":         "validate_edge",
				"policy_status":  "na",
				"policy_reason":  err.Error(),
				"policy_penalty": 0,
				"decision_path":  "runtime_scout_edge_materialization",
				"error_class":    "policy_execution_error",
				"confidence":     0,
			})
		}
	} else {
		// Policy-level direction validation (replaces direct edgesemantics call).
		st := srcTypeActual
		tt := dstTypeActual
		if st == "" {
			st = sourceFragment.Class
		}
		if tt == "" {
			tt = targetType
		}
		valid, shouldReverse, dirStatus, dirReason, dirPenalty := policy.ApplyEdgeDirectionValidation(edgeType, st, tt)
		if !valid {
			if shouldReverse {
				edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
				srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
				srcPathActual, dstPathActual = dstPathActual, srcPathActual
				directionChanged = true
				edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
			} else {
				policyStatus = policy.MergePolicyStatus(policyStatus, dirStatus)
				policyReason = dirReason
				if dirPenalty > policyPenalty {
					policyPenalty = dirPenalty
				}
				relConfidence = relConfidence * (1.0 - dirPenalty)
			}
		}
	}

	// Fail-closed for invalid edge semantics: quarantined edges must not be persisted.
	if policyStatus == models.PolicyStatusQuarantined {
		msg := fmt.Sprintf("[ScoutEngine] Skipping quarantined edge source=%s target=%s type=%s reason=%s",
			strings.TrimSpace(srcPathActual),
			strings.TrimSpace(dstPathActual),
			string(edgeType),
			strings.TrimSpace(policyReason),
		)
		fmt.Printf("%s\n", msg)
		e.logLine(ctx, job, "warn", msg)
		return nil
	}

	// In-memory dedup (per job run), after all normalization.
	dedupVariant := edgeDedupVariant(edgeType, rawRelationType, effectiveQualifiers, rel.Reason)
	if seenEdgeKeys != nil {
		ek := edgeDedupKey(edgeSourceID, edgeTargetID, edgeType, dedupVariant)
		if _, ok := seenEdgeKeys[ek]; ok {
			fmt.Printf("Skipping duplicate edge (in-memory): %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			e.rememberEdgeDedupRecently(ek)
			return nil
		}
		seenEdgeKeys[ek] = struct{}{}
	}

	// Firestore dedup check (cross-run).
	if e.hasDuplicateEdgeByVariant(ctx, "global", edgeSourceID, edgeTargetID, edgeType, dedupVariant) {
		fmt.Printf("Skipping duplicate edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
		e.rememberEdgeDedupRecently(edgeDedupKey(edgeSourceID, edgeTargetID, edgeType, dedupVariant))
		return nil
	}

	// Resolve actual canonical paths after all normalizations (sourceID/targetID may have been swapped).
	resolvedSourcePath := strings.TrimSpace(resolvePathByID(edgeSourceID))
	resolvedTargetPath := strings.TrimSpace(resolvePathByID(edgeTargetID))
	if resolvedSourcePath == "" {
		resolvedSourcePath = strings.TrimSpace(sourceFragment.CanonicalPath)
	}
	if resolvedTargetPath == "" {
		resolvedTargetPath = strings.TrimSpace(targetPath)
	}

	// POST-RESOLUTION SANITY CHECK: Penalize suspicious cross-jurisdiction edges.
	// For "local" relation types (REGULATES, CONTROLS_ACCESS, etc.), source and target
	// jurisdictions should typically match. Apply confidence penalty and policy status.
	if isCrossJurisdictionSuspicious(edgeType, resolvedSourcePath, resolvedTargetPath) {
		srcJurisdiction := extractJurisdiction(resolvedSourcePath)
		dstJurisdiction := extractJurisdiction(resolvedTargetPath)
		fmt.Printf("[CrossJurisdiction] PENALIZED edge: %s (%s) -> %s (%s) [%s]\n",
			resolvedSourcePath, srcJurisdiction, resolvedTargetPath, dstJurisdiction, edgeType)
		relConfidence = relConfidence * 0.7
		if 0.3 > policyPenalty {
			policyPenalty = 0.3
		}
		policyStatus = policy.MergePolicyStatus(policyStatus, models.PolicyStatusPenalized)
		policyReason = fmt.Sprintf("cross_jurisdiction_%s_%s", srcJurisdiction, dstJurisdiction)
	}

	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: edgeSourceID,
		TargetFragmentID: edgeTargetID,
		Relation:         edgeType,
		Properties: models.EdgeProperties{
			Criticality:   normalizeEdgeConfidence(relConfidence),
			Amplification: 1.0,
			LatencyHours:  24,
		},
		Qualifiers: map[string]interface{}{
			"reason":                 strings.TrimSpace(rel.Reason),
			"rawRelationType":        rawRelationType,
			"normalizedRelationType": normalizedRelationType,
			"rawConfidence":          relConfidence,
			"targetName":             strings.TrimSpace(rel.TargetName),
			"targetCanonicalPath":    resolvedTargetPath,
			"sourceCanonicalPath":    resolvedSourcePath,
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
	// Mark that this edge was processed by canonicalization+validation during materialization.
	// policy_checker.go uses this to skip redundant re-processing on already-persisted edges.
	edge.Qualifiers["pipelineCanonicalized"] = true
	edge.Qualifiers["pipelineValidated"] = true
	if canonFromEdgeType != "" {
		edge.Qualifiers["canonicalized"] = true
		edge.Qualifiers["canonicalizedFromEdgeType"] = canonFromEdgeType
		edge.Qualifiers["canonicalizationReason"] = canonReason
		edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
		if policyStatus == models.PolicyStatusClean {
			policyStatus = models.PolicyStatusAutoFixed
		}
		if policyReason == "" {
			policyReason = strings.TrimSpace(canonReason)
		}
	}
	edge.ValidFrom = parseOptionalISODate(rel.ValidFrom)
	edge.ValidTo = parseOptionalISODate(rel.ValidTo)
	if len(rel.Metrics) > 0 {
		edge.Metrics = rel.Metrics
	}
	if len(baseQualifiers) > 0 {
		for k, v := range baseQualifiers {
			if _, exists := edge.Qualifiers[k]; !exists {
				edge.Qualifiers[k] = v
			}
		}
	}
	if len(effectiveQualifiers) > 0 {
		edge.Qualifiers = mergeQualifiers(edge.Qualifiers, effectiveQualifiers)
	}
	// Canonicalize qualifier facility paths (at_facility, transport_via) — same rules as other fragments
	edge.Qualifiers = e.canonicalizeQualifierFacilityPaths(ctx, edge.Qualifiers, nil)
	if edge.Qualifiers == nil {
		edge.Qualifiers = make(map[string]interface{})
	}
	if strings.TrimSpace(policyEdgeModality) != "" {
		if _, exists := edge.Qualifiers["edgeModality"]; !exists {
			edge.Qualifiers["edgeModality"] = policyEdgeModality
		}
	}
	if directionChanged {
		edge.Qualifiers["normalizedDirection"] = true
		edge.Qualifiers["normalizedEdgeType"] = string(edgeType)
		edge.Qualifiers["normalizedSourceFragmentId"] = edge.SourceFragmentID
		edge.Qualifiers["normalizedTargetFragmentId"] = edge.TargetFragmentID
	}
	if canon.GovRegulatesSwap {
		edge.Qualifiers["canonicalizedGovRegulatesSwap"] = true
		edge.Qualifiers["govRegulatesSwapReason"] = canon.GovRegulatesSwapReason
	}
	if edge.Relation == ah.RelationNameAssures {
		if normalizeAssuranceTypeQualifier(edge.Qualifiers) == "" {
			if dedupVariant := edgeDedupVariant(edge.Relation, rawRelationType, edge.Qualifiers, rel.Reason); dedupVariant == "assurance_type=audit" {
				edge.Qualifiers["assurance_type"] = "audit"
			}
		}
	}
	edge.SetPolicyStatus(policyStatus, policyReason, policyPenalty)
	if canonFromEdgeType != "" {
		edge.SetPolicyOriginalType(DependencyType(canonFromEdgeType))
	}
	applyBestEffortEdgeEnrichment(edge, resolvedSourcePath, resolvedTargetPath)
	dedupVariant = edgeDedupVariant(edge.Relation, rawRelationType, edge.Qualifiers, rel.Reason)
	if e.hasDuplicateEdgeByVariant(ctx, "global", edge.SourceFragmentID, edge.TargetFragmentID, edge.Relation, dedupVariant) {
		e.rememberEdgeDedupRecently(edgeDedupKey(edge.SourceFragmentID, edge.TargetFragmentID, edge.Relation, dedupVariant))
		return nil
	}

	e.rememberEdgeDedupRecently(edgeDedupKey(edge.SourceFragmentID, edge.TargetFragmentID, edge.Relation, dedupVariant))

	// Final allowlist gate (supply-chain): never persist out-of-contract types.
	// Defense-in-depth: catches any edge that slipped through pre-materialization guard or canonicalization.
	if isSupplyChainMode {
		allowed := IsAllowedSupplyChainPersistRelation(resolvedSourcePath, resolvedTargetPath, edgeType)
		if !allowed {
			e.logLine(ctx, job, "info", fmt.Sprintf("> persist_final_gate: skipped type=%q source=%q target=%q reason=out_of_contract",
				edgeType, resolvedSourcePath, resolvedTargetPath))
			return nil
		}
	}

	// If a batch buffer is attached to the context, accumulate the edge for batch persist.
	if buf := EdgeBatchBufferFromContext(ctx); buf != nil {
		buf.Add(edge, edgeBatchMeta{
			sourcePath:  resolvedSourcePath,
			targetPath:  resolvedTargetPath,
			sourceLabel: strings.TrimSpace(sourceFragment.Labels["en"]),
			targetLabel: strings.TrimSpace(firstNonEmpty(rel.TargetShortName, rel.TargetName, rel.TargetLegalName)),
			job:         job,
		})
		fmt.Printf("Buffered edge: %s -> %s (%s)\n", resolvedSourcePath, resolvedTargetPath, edgeType)
		return nil
	}

	if id, err := e.edgesRepo.Create(ctx, edge); err != nil {
		msg := fmt.Sprintf("Failed to create edge %s -> %s (%s) confidence=%.4f rawConfidence=%.4f rawType=%q: %v",
			strings.TrimSpace(resolvedSourcePath),
			strings.TrimSpace(resolvedTargetPath),
			string(edgeType),
			edge.Properties.Criticality,
			relConfidence,
			strings.TrimSpace(rel.RelationType),
			err,
		)
		fmt.Printf("[ScoutEngine] %s\n", msg)
		e.logLine(ctx, job, "warn", msg)
		return nil
	} else {
		edge.ID = id
	}

	fmt.Printf("Created edge: %s -> %s (%s)\n", resolvedSourcePath, resolvedTargetPath, edgeType)
	e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
		"tenantId":            job.TenantID,
		"projectId":           job.ProjectID,
		"lensId":              strings.TrimSpace(job.LensID),
		"edgeId":              edge.ID,
		"sourceFragmentId":    edge.SourceFragmentID,
		"targetFragmentId":    edge.TargetFragmentID,
		"sourceCanonicalPath": resolvedSourcePath,
		"targetCanonicalPath": resolvedTargetPath,
		"sourceLabel":         strings.TrimSpace(sourceFragment.Labels["en"]),
		"targetLabel":         strings.TrimSpace(firstNonEmpty(rel.TargetShortName, rel.TargetName, rel.TargetLegalName)),
		"type":                string(edge.Relation),
		"properties":          edge.Properties,
		"qualifiers":          edge.Qualifiers,
	})
	return nil
}

func edgeDedupKey(sourceID, targetID string, edgeType DependencyType, variant string) string {
	base := EdgeKey(sourceID, targetID, edgeType)
	variant = strings.TrimSpace(variant)
	if variant == "" {
		return base
	}
	return base + "|" + variant
}

func edgeDedupVariant(
	edgeType DependencyType,
	rawRelationType string,
	qualifiers map[string]interface{},
	reason string,
) string {
	if edgeType != ah.RelationNameAssures {
		return ""
	}
	if t := normalizeAssuranceTypeQualifier(qualifiers); t != "" {
		return "assurance_type=" + t
	}
	raw := strings.ToUpper(strings.TrimSpace(rawRelationType))
	reasonLower := strings.ToLower(strings.TrimSpace(reason))
	if raw == "AUDITED_BY" ||
		strings.Contains(reasonLower, "audit") ||
		strings.Contains(reasonLower, "auditor") ||
		strings.Contains(reasonLower, "audited") {
		return "assurance_type=audit"
	}
	return ""
}

func normalizeAssuranceTypeQualifier(qualifiers map[string]interface{}) string {
	if qualifiers == nil {
		return ""
	}
	candidate := ""
	if v, ok := qualifiers["assurance_type"]; ok {
		if s, ok := v.(string); ok {
			candidate = s
		}
	}
	if candidate == "" {
		if v, ok := qualifiers["assuranceType"]; ok {
			if s, ok := v.(string); ok {
				candidate = s
			}
		}
	}
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	switch candidate {
	case "audit", "certification", "regulatory", "inspection":
		return candidate
	default:
		return ""
	}
}

func (e *ScoutEngine) hasDuplicateEdgeByVariant(
	ctx context.Context,
	tenantID, sourceID, targetID string,
	edgeType DependencyType,
	variant string,
) bool {
	if e == nil || e.edgesRepo == nil {
		return false
	}
	variant = strings.TrimSpace(variant)
	if variant == "" {
		_, err := e.edgesRepo.GetBySourceTargetAndType(ctx, tenantID, sourceID, targetID, edgeType)
		return err == nil
	}
	edges, err := e.edgesRepo.ListBySourceTargetAndType(ctx, tenantID, sourceID, targetID, edgeType, 50)
	if err != nil {
		return false
	}
	for _, ex := range edges {
		if ex == nil {
			continue
		}
		if edgeDedupVariant(ex.Relation, "", ex.Qualifiers, "") == variant {
			return true
		}
	}
	return false
}

// isCrossJurisdictionSuspicious checks if an edge between two jurisdictions is suspicious
// based on the edge type. For "local" relations like REGULATES, CONTROLS_ACCESS, etc.,
// cross-jurisdiction edges are likely errors (e.g., Indian regulator controlling Saudi company).
func isCrossJurisdictionSuspicious(edgeType DependencyType, srcPath, dstPath string) bool {
	srcJurisdiction := extractJurisdiction(srcPath)
	dstJurisdiction := extractJurisdiction(dstPath) // If either jurisdiction is empty/global, not suspicious
	if srcJurisdiction == "" || dstJurisdiction == "" {
		return false
	}
	if srcJurisdiction == "global" || dstJurisdiction == "global" {
		return false
	}

	// If jurisdictions match, not suspicious
	if srcJurisdiction == dstJurisdiction {
		return false
	}

	// Check if edge type is "local" (should typically be same jurisdiction)
	localEdgeTypes := map[DependencyType]bool{
		ah.RelationNameRegulates:      true,
		ah.RelationNameImposes: true,
		"GATEKEEPS":                         true,
		"GATEKEEPS_DECISION":                true,
		"LICENSED_BY":                       true,
		"APPROVED_BY":                       true,
		"CONTROLLED_BY":                     true,
	}

	return localEdgeTypes[edgeType]
}

type edgeBatchBufferKey struct{}

// WithEdgeBatchBuffer attaches an EdgeBatchBuffer to a context.
// When present, materializeRelationEdge will buffer edges instead of persisting them individually.
func WithEdgeBatchBuffer(ctx context.Context, buf *EdgeBatchBuffer) context.Context {
	return context.WithValue(ctx, edgeBatchBufferKey{}, buf)
}

// EdgeBatchBufferFromContext retrieves the EdgeBatchBuffer from context, or nil.
func EdgeBatchBufferFromContext(ctx context.Context) *EdgeBatchBuffer {
	if buf, ok := ctx.Value(edgeBatchBufferKey{}).(*EdgeBatchBuffer); ok {
		return buf
	}
	return nil
}

// EdgeBatchBuffer accumulates materialized edges and flushes them via CreateMany.
// Used as an optional optimization: when set on ScoutEngine, materializeRelationEdge
// buffers edges instead of persisting them individually.
type EdgeBatchBuffer struct {
	edges    []*models.DependencyEdge
	metadata []edgeBatchMeta
}

type edgeBatchMeta struct {
	sourcePath  string
	targetPath  string
	sourceLabel string
	targetLabel string
	job         *models.ScoutingJob
}

// NewEdgeBatchBuffer creates an empty batch buffer.
func NewEdgeBatchBuffer() *EdgeBatchBuffer {
	return &EdgeBatchBuffer{}
}

// Add appends an edge to the buffer.
func (b *EdgeBatchBuffer) Add(edge *models.DependencyEdge, meta edgeBatchMeta) {
	b.edges = append(b.edges, edge)
	b.metadata = append(b.metadata, meta)
}

// Len returns the number of buffered edges.
func (b *EdgeBatchBuffer) Len() int {
	if b == nil {
		return 0
	}
	return len(b.edges)
}

// Flush persists all buffered edges via CreateMany and publishes realtime events.
func (b *EdgeBatchBuffer) Flush(ctx context.Context, e *ScoutEngine) error {
	if b == nil || len(b.edges) == 0 {
		return nil
	}
	ids, err := e.edgesRepo.CreateMany(ctx, b.edges)
	if err != nil {
		fmt.Printf("[EdgeBatchBuffer] batch flush error: %v (created %d of %d)\n", err, len(ids), len(b.edges))
	}
	for i, edge := range b.edges {
		if edge == nil || edge.ID == "" {
			continue
		}
		meta := b.metadata[i]
		e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
			"tenantId":            edge.TenantID,
			"projectId":           meta.job.ProjectID,
			"lensId":              strings.TrimSpace(meta.job.LensID),
			"edgeId":              edge.ID,
			"sourceFragmentId":    edge.SourceFragmentID,
			"targetFragmentId":    edge.TargetFragmentID,
			"sourceCanonicalPath": meta.sourcePath,
			"targetCanonicalPath": meta.targetPath,
			"sourceLabel":         meta.sourceLabel,
			"targetLabel":         meta.targetLabel,
			"type":                string(edge.Relation),
			"properties":          edge.Properties,
			"qualifiers":          edge.Qualifiers,
		})
	}
	b.edges = nil
	b.metadata = nil
	return err
}

// detectLikelyInvertedOwnershipEdge returns true when Reason or qualifiers suggest the edge direction
// is inverted (e.g. "owned by" implies target->source but we have source->target).
func detectLikelyInvertedOwnershipEdge(edgeType DependencyType, rel agents.DiscoveredRelation) bool {
	switch edgeType {
	case ah.RelationNameBeneficiallyOwns, ah.RelationNameInvestedIn,
		ah.RelationNameControlsVia, ah.RelationNameAssures:
		// check
	default:
		return false
	}
	reason := strings.ToLower(strings.TrimSpace(rel.Reason))
	invertedPhrases := []string{"owned by", "invested in by", "controlled by", "subsidiary of", "audited by"}
	for _, phrase := range invertedPhrases {
		if strings.Contains(reason, phrase) {
			return true
		}
	}
	if q := rel.Qualifiers; q != nil {
		sr, _ := q["source_role"].(string)
		tr, _ := q["target_role"].(string)
		sr = strings.ToLower(strings.TrimSpace(sr))
		tr = strings.ToLower(strings.TrimSpace(tr))
		invertedSourceRoles := map[string]bool{"owned_entity": true, "investee": true, "controlled_entity": true}
		invertedTargetRoles := map[string]bool{"owner": true, "investor": true, "controller": true}
		if invertedSourceRoles[sr] || invertedTargetRoles[tr] {
			return true
		}
	}
	return false
}

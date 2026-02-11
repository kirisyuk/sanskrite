package scout

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
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
	targetType models.FragmentType,
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

	edgeType := models.DependencyType(strings.TrimSpace(rel.RelationType))
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
					srcType = models.FragmentTypeOrganization
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
					Type:          srcType,
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
			targetType = sourceFragment.Type
		}
	}

	// Enforce directional semantics for common relations (best-effort, heuristic).
	edgeSourceID, edgeTargetID, edgeType, directionChanged := normalizeRelationEdge(explicitSourceFragment, edgeSourceID, edgeTargetID, edgeType, targetType, enableManagement)
	switch edgeType {
	case "SUBSIDIARY_OF":
		edgeType = models.DependencyTypeOwns
		edgeSourceID, edgeTargetID = targetID, sourceFragment.ID
		directionChanged = true
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

	// Resolve *actual* endpoint types for validation (must match edgeSourceID/edgeTargetID after swaps).
	resolveTypeByID := func(id string) models.FragmentType {
		id = strings.TrimSpace(id)
		if id == "" {
			return ""
		}
		resolveFromPath := func(path string, current models.FragmentType) models.FragmentType {
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
			return resolveFromPath(explicitSourceFragment.CanonicalPath, explicitSourceFragment.Type)
		}
		if sourceFragment != nil && strings.TrimSpace(sourceFragment.ID) == id {
			return resolveFromPath(sourceFragment.CanonicalPath, sourceFragment.Type)
		}
		if targetFragment != nil && strings.TrimSpace(targetFragment.ID) == id {
			return resolveFromPath(targetFragment.CanonicalPath, targetFragment.Type)
		}
		if e.fragmentsRepo != nil {
			if f, ferr := e.fragmentsRepo.Get(ctx, id); ferr == nil && f != nil {
				return resolveFromPath(f.CanonicalPath, f.Type)
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

	// Special-case canonicalization: REGULATES involving /gov paths.
	govRegulatesSwapApplied := false
	govRegulatesSwapReason := ""
	if edgeType == models.DependencyTypeRegulates {
		srcPathActual := resolvePathByID(edgeSourceID)
		dstPathActual := resolvePathByID(edgeTargetID)
		if strings.HasPrefix(srcPathActual, "/org/") && strings.HasPrefix(dstPathActual, "/gov/") {
			edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
			srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
			directionChanged = true
			govRegulatesSwapApplied = true
			govRegulatesSwapReason = "canonicalize: REGULATES /org -> /gov swapped to /gov -> /org (gov bodies are regulators)"
			edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
		}
	}

	// Canonicalize certain mis-modeled edges where reversing is not sufficient.
	srcPathActual := resolvePathByID(edgeSourceID)
	dstPathActual := resolvePathByID(edgeTargetID)
	canonFromEdgeType := ""
	canonReason := ""
	// INVESTS_IN ORG->FACILITY can mean ownership (if evidence exists) or weaker operational exposure.
	if edgeType == models.DependencyTypeInvestsIn &&
		srcTypeActual == models.FragmentTypeOrganization &&
		dstTypeActual == models.FragmentTypeFacility &&
		models.HasOwnershipEvidence(rel.Reason, rel.Qualifiers, rel.Metrics) {
		canonFromEdgeType = string(edgeType)
		canonReason = "canonicalize: ORGANIZATION INVESTS_IN FACILITY with ownership evidence -> ORGANIZATION OWNS FACILITY"
		edgeType = models.DependencyTypeOwns
		directionChanged = true
		edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
	} else if newType, shouldSwap, changed, reason := CanonicalizeEdgeWithPaths(edgeType, srcTypeActual, dstTypeActual, srcPathActual, dstPathActual); changed {
		canonFromEdgeType = string(edgeType)
		canonReason = reason
		edgeType = newType
		directionChanged = true
		if shouldSwap {
			edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
			srcTypeActual, dstTypeActual = dstTypeActual, srcTypeActual
			srcPathActual, dstPathActual = dstPathActual, srcPathActual
		}
		edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
	}
	if canonFromEdgeType != "" {
		if penalty := models.CanonicalizationPenalty(models.DependencyType(canonFromEdgeType), edgeType, srcTypeActual, dstTypeActual); penalty > 0 {
			relConfidence = relConfidence * (1.0 - penalty)
			policyPenalty = penalty
			policyStatus = models.PolicyStatusPenalized
			if policyReason == "" {
				policyReason = strings.TrimSpace(canonReason)
			}
		}
	}

	// Policy: direction validation and reversal (enforced for known edge types).
	if e.validationPolicy != nil {
		logPrefix := "[ScoutEngine:materializeEdge]"
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
			},
			Fragment: nil,
		})
		if err == nil {
			if modality, _ := decision.Params["edgeModality"].(string); strings.TrimSpace(modality) != "" {
				policyEdgeModality = strings.TrimSpace(modality)
			}
			policy.ApplyValidationDecisionBestEffort(logPrefix, decision, policy.ValidationDecisionHandlers{
				Reverse: func() {
					edgeSourceID, edgeTargetID = edgeTargetID, edgeSourceID
					directionChanged = true
					edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
				},
				RewriteType: func(newType string, _ string) {
					newEdgeType := models.DependencyType(strings.TrimSpace(newType))
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
					if status == models.PolicyStatusClean && policyStatus != models.PolicyStatusClean {
						return
					}
					policyStatus = status
					policyReason = strings.TrimSpace(reason)
					policyPenalty = penalty
				},
			})
		}
	} else {
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
				edgeSourceID, edgeTargetID, edgeType, _ = NormalizeSymmetricEdge(edgeSourceID, edgeTargetID, edgeType)
			} else if rule != nil {
				policyStatus = models.PolicyStatusQuarantined
				policyReason = fmt.Sprintf("quarantine: %s", rule.Description)
				policyPenalty = 0.2
				relConfidence = relConfidence * (1.0 - policyPenalty)
			}
		}
	}

	// In-memory dedup (per job run), after all normalization.
	if seenEdgeKeys != nil {
		ek := EdgeKey(edgeSourceID, edgeTargetID, edgeType)
		if _, ok := seenEdgeKeys[ek]; ok {
			fmt.Printf("Skipping duplicate edge (in-memory): %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			return nil
		}
		seenEdgeKeys[ek] = struct{}{}
	}

	// Firestore dedup check (cross-run).
	if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", edgeSourceID, edgeTargetID, edgeType); err == nil {
		fmt.Printf("Skipping duplicate edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
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

	// POST-RESOLUTION SANITY CHECK: Flag suspicious cross-jurisdiction edges.
	// For "local" relation types (REGULATES, CONTROLS_ACCESS, etc.), source and target
	// jurisdictions should typically match. Log a warning if they differ.
	if isCrossJurisdictionSuspicious(edgeType, resolvedSourcePath, resolvedTargetPath) {
		srcJurisdiction := extractJurisdiction(resolvedSourcePath)
		dstJurisdiction := extractJurisdiction(resolvedTargetPath)
		fmt.Printf("[CrossJurisdiction] SUSPICIOUS edge: %s (%s) -> %s (%s) [%s]. Proceeding with caution.\n",
			resolvedSourcePath, srcJurisdiction, resolvedTargetPath, dstJurisdiction, edgeType)
		// We log but don't reject - let the edge be created, but it may need manual review.
	}

	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: edgeSourceID,
		TargetFragmentID: edgeTargetID,
		Type:             edgeType,
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
	if len(rel.Qualifiers) > 0 {
		edge.Qualifiers = mergeQualifiers(edge.Qualifiers, rel.Qualifiers)
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
	if govRegulatesSwapApplied {
		edge.Qualifiers["canonicalizedGovRegulatesSwap"] = true
		edge.Qualifiers["govRegulatesSwapReason"] = govRegulatesSwapReason
	}
	edge.SetPolicyStatus(policyStatus, policyReason, policyPenalty)
	if canonFromEdgeType != "" {
		edge.SetPolicyOriginalType(models.DependencyType(canonFromEdgeType))
	}
	applyBestEffortEdgeEnrichment(edge)

	if existing, _ := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", edge.SourceFragmentID, edge.TargetFragmentID, edge.Type); existing != nil {
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
		"type":                string(edge.Type),
		"properties":          edge.Properties,
		"qualifiers":          edge.Qualifiers,
	})
	return nil
}

// isCrossJurisdictionSuspicious checks if an edge between two jurisdictions is suspicious
// based on the edge type. For "local" relations like REGULATES, CONTROLS_ACCESS, etc.,
// cross-jurisdiction edges are likely errors (e.g., Indian regulator controlling Saudi company).
func isCrossJurisdictionSuspicious(edgeType models.DependencyType, srcPath, dstPath string) bool {
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
	localEdgeTypes := map[models.DependencyType]bool{
		models.DependencyTypeRegulates:      true,
		models.DependencyTypeControlsAccess: true,
		"GATEKEEPS":                         true,
		"GATEKEEPS_DECISION":                true,
		"LICENSED_BY":                       true,
		"APPROVED_BY":                       true,
		"CONTROLLED_BY":                     true,
	}

	if localEdgeTypes[edgeType] {
		return true
	}

	return false
}

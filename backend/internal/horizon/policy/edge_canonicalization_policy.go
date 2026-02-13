package policy

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type EdgeCanonicalizationInput struct {
	EdgeType          horizonmodels.DependencyType
	SourceType        horizonmodels.FragmentType
	TargetType        horizonmodels.FragmentType
	SourcePath        string
	TargetPath        string
	RawRelationType   string
	OwnershipEvidence bool
}

type EdgeCanonicalizationResult struct {
	EdgeType       horizonmodels.DependencyType
	OriginalType   horizonmodels.DependencyType
	ShouldSwap     bool
	Changed        bool
	Reason         string
	PolicyStatus   string
	PolicyReason   string
	PolicyPenalty  float64
	ConfidenceMult float64
	// GovRegulatesSwap is set when REGULATES /org→/gov was swapped to /gov→/org.
	GovRegulatesSwap       bool
	GovRegulatesSwapReason string
}

func ApplyEdgeCanonicalizationPolicy(in EdgeCanonicalizationInput) EdgeCanonicalizationResult {
	res := EdgeCanonicalizationResult{
		EdgeType:       in.EdgeType,
		PolicyStatus:   horizonmodels.PolicyStatusClean,
		ConfidenceMult: 1.0,
	}

	// --- REGULATES /org → /gov swap (gov bodies are regulators, not the regulated) ---
	// LLMs frequently output inverted direction as ORG -> GOV for REGULATES; fix best-effort.
	if in.EdgeType == horizonmodels.DependencyTypeRegulates &&
		strings.HasPrefix(in.SourcePath, "/org/") && strings.HasPrefix(in.TargetPath, "/gov/") {
		res.ShouldSwap = true
		res.Changed = true
		res.GovRegulatesSwap = true
		res.GovRegulatesSwapReason = "canonicalize: REGULATES /org -> /gov swapped to /gov -> /org (gov bodies are regulators)"
		// After swap, source/target types are effectively swapped for subsequent checks.
		in.SourceType, in.TargetType = in.TargetType, in.SourceType
		in.SourcePath, in.TargetPath = in.TargetPath, in.SourcePath
	}

	if in.EdgeType == horizonmodels.DependencyTypeInvestsIn &&
		in.SourceType == horizonmodels.FragmentTypeOrganization &&
		in.TargetType == horizonmodels.FragmentTypeFacility &&
		in.OwnershipEvidence {
		res.OriginalType = in.EdgeType
		res.EdgeType = horizonmodels.DependencyTypeOwns
		res.Changed = true
		res.Reason = "canonicalize: ORGANIZATION INVESTS_IN FACILITY with ownership evidence -> ORGANIZATION OWNS FACILITY"
	} else if newType, shouldSwap, changed, reason := horizonmodels.CanonicalizeEdgeWithPaths(
		in.EdgeType, in.SourceType, in.TargetType, in.SourcePath, in.TargetPath,
	); changed {
		res.OriginalType = in.EdgeType
		res.EdgeType = newType
		if shouldSwap {
			res.ShouldSwap = !res.ShouldSwap // compose with any prior swap
		}
		res.Changed = true
		res.Reason = strings.TrimSpace(reason)
	}

	if res.Changed && res.OriginalType != "" {
		penalty := horizonmodels.CanonicalizationPenalty(res.OriginalType, res.EdgeType, in.SourceType, in.TargetType)
		if penalty > 0 {
			res.PolicyStatus = horizonmodels.PolicyStatusPenalized
			res.PolicyPenalty = penalty
			res.PolicyReason = res.Reason
			res.ConfidenceMult = 1.0 - penalty
		}
	}

	rawRelationType := strings.TrimSpace(in.RawRelationType)
	if rawRelationType == "" {
		rawRelationType = string(in.EdgeType)
	}
	// --- Transition fallback: transition_unclassified → INFLUENCES ---
	if horizonmodels.IsTransitionLikeRelationType(rawRelationType) &&
		!horizonmodels.IsTransitionEdgeType(res.EdgeType) &&
		in.SourceType == horizonmodels.FragmentTypeOrganization &&
		in.TargetType == horizonmodels.FragmentTypeOrganization {
		if res.EdgeType != horizonmodels.DependencyTypeInfluences {
			if res.OriginalType == "" {
				res.OriginalType = res.EdgeType
			}
			res.EdgeType = horizonmodels.DependencyTypeInfluences
			res.Changed = true
			res.Reason = "canonicalize: transition_unclassified -> INFLUENCES"
		}
		res.PolicyStatus = horizonmodels.PolicyStatusPenalized
		res.PolicyReason = "transition_unclassified"
		if res.PolicyPenalty < 0.15 {
			res.PolicyPenalty = 0.15
			res.ConfidenceMult *= 0.85
		}
	}

	// --- Unknown edge type fallback: best-effort persist as INFLUENCES + penalized ---
	// If edge type has no direction policy, treat as unknown and persist with penalty.
	// This implements the "best effort first" principle (Section 7.1).
	if !res.Changed {
		_, exists := edgesemantics.EdgeDirectionPolicies[res.EdgeType]
		if !exists {
			res.OriginalType = res.EdgeType
			res.EdgeType = horizonmodels.DependencyTypeInfluences
			res.Changed = true
			res.Reason = "canonicalize: unknown_edge_type -> INFLUENCES (best-effort)"
			res.PolicyStatus = horizonmodels.PolicyStatusPenalized
			res.PolicyReason = "unknown_edge_type"
			if res.PolicyPenalty < 0.20 {
				res.PolicyPenalty = 0.20
				res.ConfidenceMult *= 0.80
			}
		}
	}

	return res
}

// ApplyEdgeDirectionValidation validates and optionally reverses edge direction using
// the unified edgesemantics direction policy. This is the policy-level entrypoint for
// edge direction validation, replacing direct calls to edgesemantics.ValidateEdgeDirection
// from scout code.
//
// Returns: (isValid, shouldReverse, policyStatus, policyReason, policyPenalty).
func ApplyEdgeDirectionValidation(
	edgeType horizonmodels.DependencyType,
	sourceType, targetType horizonmodels.FragmentType,
) (bool, bool, string, string, float64) {
	valid, shouldReverse, rule := edgesemantics.ValidateEdgeDirection(edgeType, sourceType, targetType)
	if valid {
		return true, false, horizonmodels.PolicyStatusClean, "", 0
	}
	if shouldReverse {
		return false, true, horizonmodels.PolicyStatusAutoFixed, "direction_reversed", 0
	}
	// Unknown or invalid direction: best-effort persist with quarantine.
	reason := "unknown_direction"
	if rule != nil {
		reason = "quarantine: " + rule.Description
	}
	return false, false, horizonmodels.PolicyStatusQuarantined, reason, 0.2
}

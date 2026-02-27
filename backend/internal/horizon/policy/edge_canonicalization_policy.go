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
	Reason            string
	Qualifiers        map[string]interface{}
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

	// --- Legacy AUDITED_BY canonicalization ---
	// Keep AUDITED_BY as input-compat only; canonical runtime type is ASSURES.
	if shouldCanonicalizeLegacyAuditedBy(in.EdgeType, in.RawRelationType) {
		if res.OriginalType == "" {
			res.OriginalType = in.EdgeType
		}
		res.EdgeType = horizonmodels.DependencyTypeAssures
		res.Changed = true
		res.ShouldSwap = true // AUDITED_BY: assuree -> assurer; ASSURES: assurer -> assuree.
		res.Reason = "canonicalize: legacy AUDITED_BY -> ASSURES (swap direction)"
		in.EdgeType = horizonmodels.DependencyTypeAssures
		in.SourceType, in.TargetType = in.TargetType, in.SourceType
		in.SourcePath, in.TargetPath = in.TargetPath, in.SourcePath
		if in.Qualifiers != nil {
			if _, ok := in.Qualifiers["assurance_type"]; !ok {
				in.Qualifiers["assurance_type"] = "audit"
			}
		}
	}

	// --- Assurance semantics rescue ---
	// If extraction emitted DEPENDS_ON/INFLUENCES for an audit/certification relation,
	// restore canonical ASSURES semantics (assurer -> assuree).
	if shouldRewrite, shouldSwap, assuranceType := shouldCanonicalizeToAssures(in.EdgeType, in.RawRelationType, in.Reason, in.Qualifiers); shouldRewrite {
		res.OriginalType = in.EdgeType
		res.EdgeType = horizonmodels.DependencyTypeAssures
		res.Changed = true
		res.Reason = "canonicalize: dependency/influence with assurance semantics -> ASSURES"
		if shouldSwap {
			res.ShouldSwap = true
			in.SourceType, in.TargetType = in.TargetType, in.SourceType
			in.SourcePath, in.TargetPath = in.TargetPath, in.SourcePath
		}
		if assuranceType != "" && in.Qualifiers != nil {
			if _, ok := in.Qualifiers["assurance_type"]; !ok {
				in.Qualifiers["assurance_type"] = assuranceType
			}
		}
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

	// --- ADMINISTERS /org → /gov swap (gov bodies administer, not companies) ---
	if in.EdgeType == horizonmodels.DependencyTypeAdministers &&
		strings.HasPrefix(in.SourcePath, "/org/") && strings.HasPrefix(in.TargetPath, "/gov/") {
		res.ShouldSwap = true
		res.Changed = true
		res.Reason = "canonicalize: ADMINISTERS /org -> /gov swapped to /gov -> /org (gov bodies administer)"
		in.SourceType, in.TargetType = in.TargetType, in.SourceType
		in.SourcePath, in.TargetPath = in.TargetPath, in.SourcePath
	}

	// --- OWNS /org → /gov swap (gov owns state-owned enterprises, not vice versa) ---
	if in.EdgeType == horizonmodels.DependencyTypeOwns &&
		strings.HasPrefix(in.SourcePath, "/org/") && strings.HasPrefix(in.TargetPath, "/gov/") {
		res.ShouldSwap = true
		res.Changed = true
		res.Reason = "canonicalize: OWNS /org -> /gov swapped to /gov -> /org (gov owns state-owned enterprises)"
		in.SourceType, in.TargetType = in.TargetType, in.SourceType
		in.SourcePath, in.TargetPath = in.TargetPath, in.SourcePath
	}

	// --- CONTROLS /product/ → /gov/ or /org/ swap (gov/org controls product, not vice versa) ---
	if in.EdgeType == horizonmodels.DependencyTypeControls &&
		strings.HasPrefix(in.SourcePath, "/product/") &&
		(strings.HasPrefix(in.TargetPath, "/gov/") || strings.HasPrefix(in.TargetPath, "/org/")) {
		res.ShouldSwap = true
		res.Changed = true
		res.Reason = "canonicalize: CONTROLS /product/ -> /gov/|/org/ swapped to /gov/|/org/ -> /product/ (gov/org controls product)"
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
	} else if fix, ok := ResolveEdgeDirectionAutoFix(in.EdgeType, in.SourceType, in.TargetType, in.SourcePath, in.TargetPath); ok {
		res.OriginalType = in.EdgeType
		res.EdgeType = fix.NewType
		res.ShouldSwap = fix.ShouldSwap
		res.Changed = true
		res.Reason = fix.Reason
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

func shouldCanonicalizeLegacyAuditedBy(
	edgeType horizonmodels.DependencyType,
	rawRelationType string,
) bool {
	if edgeType == horizonmodels.DependencyTypeAuditedBy {
		return true
	}
	raw := strings.ToUpper(strings.TrimSpace(rawRelationType))
	return raw == "AUDITED_BY"
}

func shouldCanonicalizeToAssures(
	edgeType horizonmodels.DependencyType,
	rawRelationType, reason string,
	qualifiers map[string]interface{},
) (shouldRewrite bool, shouldSwap bool, assuranceType string) {
	raw := strings.ToUpper(strings.TrimSpace(rawRelationType))
	reasonLower := strings.ToLower(strings.TrimSpace(reason))
	assuranceType = extractAssuranceType(qualifiers)
	if assuranceType == "" {
		assuranceType = inferAssuranceTypeFromReason(reasonLower)
	}

	if edgeType != horizonmodels.DependencyTypeDependsOn &&
		edgeType != horizonmodels.DependencyTypeInfluences &&
		edgeType != horizonmodels.DependencyTypeControls {
		return false, false, ""
	}
	hasAssuranceRaw := raw == "ASSURES" || raw == "AUDITED_BY" || strings.Contains(raw, "AUDIT") || strings.Contains(raw, "ASSUR") || strings.Contains(raw, "CERTIF")
	hasAssuranceCue := assuranceType != "" || hasAssuranceRaw || reasonHasAssuranceCue(reasonLower)
	if !hasAssuranceCue {
		return false, false, ""
	}

	// Direction inference:
	// - "audited by/certified by/assured by" => target assures source (swap)
	// - "audits/certifies/assures" => source assures target (no swap)
	// - DEPENDS_ON fallback => swap (dependent is usually assuree).
	switch {
	case raw == "ASSURES":
		shouldSwap = false
	case raw == "AUDITED_BY":
		shouldSwap = true
	case reasonSuggestsSourceAssuresTarget(reasonLower):
		shouldSwap = false
	case reasonSuggestsTargetAssuresSource(reasonLower):
		shouldSwap = true
	default:
		// DEPENDS_ON default is usually assuree -> assurer; invert to canonical ASSURES.
		// For CONTROLS/INFLUENCES keep emitted direction unless linguistic cues indicate inversion.
		shouldSwap = edgeType == horizonmodels.DependencyTypeDependsOn
	}

	return true, shouldSwap, assuranceType
}

func extractAssuranceType(qualifiers map[string]interface{}) string {
	if qualifiers == nil {
		return ""
	}
	if v, ok := qualifiers["assurance_type"]; ok {
		return normalizeAssuranceType(v)
	}
	if v, ok := qualifiers["assuranceType"]; ok {
		return normalizeAssuranceType(v)
	}
	return ""
}

func normalizeAssuranceType(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "audit", "certification", "regulatory", "inspection":
		return s
	default:
		return ""
	}
}

func inferAssuranceTypeFromReason(reason string) string {
	switch {
	case strings.Contains(reason, "audit"), strings.Contains(reason, "auditor"), strings.Contains(reason, "audited"):
		return "audit"
	case strings.Contains(reason, "certif"):
		return "certification"
	case strings.Contains(reason, "inspect"):
		return "inspection"
	default:
		return ""
	}
}

func reasonHasAssuranceCue(reason string) bool {
	return strings.Contains(reason, "audit") ||
		strings.Contains(reason, "auditor") ||
		strings.Contains(reason, "audited") ||
		strings.Contains(reason, "assurance") ||
		strings.Contains(reason, "assured") ||
		strings.Contains(reason, "certif") ||
		strings.Contains(reason, "inspection") ||
		strings.Contains(reason, "inspected")
}

func reasonSuggestsTargetAssuresSource(reason string) bool {
	return strings.Contains(reason, "audited by") ||
		strings.Contains(reason, "is audited by") ||
		strings.Contains(reason, "certified by") ||
		strings.Contains(reason, "is certified by") ||
		strings.Contains(reason, "assured by") ||
		strings.Contains(reason, "is assured by") ||
		strings.Contains(reason, "inspected by") ||
		strings.Contains(reason, "is inspected by")
}

func reasonSuggestsSourceAssuresTarget(reason string) bool {
	return strings.Contains(reason, "audits ") ||
		strings.Contains(reason, "certifies ") ||
		strings.Contains(reason, "assures ") ||
		strings.Contains(reason, "inspects ") ||
		strings.Contains(reason, "provides assurance to") ||
		strings.Contains(reason, "external auditor for")
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

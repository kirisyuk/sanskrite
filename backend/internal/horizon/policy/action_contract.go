package policy

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	ActionAllow            = "allow"
	ActionAllowWithPenalty = "allow_with_penalty"
	ActionAutoFix          = "auto_fix"
	ActionReverse          = "reverse"
	ActionRewriteType      = "rewrite_type"
	ActionQuarantineSoft   = "quarantine_soft"
)

var policyStatusOrder = map[string]int{
	horizonmodels.PolicyStatusClean:       0,
	horizonmodels.PolicyStatusAutoFixed:   1,
	horizonmodels.PolicyStatusPenalized:   2,
	horizonmodels.PolicyStatusQuarantined: 3,
}

// NormalizeValidationAction maps legacy validation actions to the runtime contract.
func NormalizeValidationAction(action string) string {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "", ActionAllow:
		return ActionAllow
	case "penalty":
		return ActionAllowWithPenalty
	case "reject", "quarantine":
		return ActionQuarantineSoft
	default:
		return strings.TrimSpace(strings.ToLower(action))
	}
}

// NormalizeValidationDecision rewrites legacy action names into the unified action contract.
func NormalizeValidationDecision(decision PolicyDecision) PolicyDecision {
	decision.Action = NormalizeValidationAction(decision.Action)
	return decision
}

// MergePolicyStatus enforces monotonic status transitions inside one pass.
func MergePolicyStatus(current, next string) string {
	current = normalizePolicyStatus(current)
	next = normalizePolicyStatus(next)
	if policyStatusOrder[next] > policyStatusOrder[current] {
		return next
	}
	return current
}

func normalizePolicyStatus(status string) string {
	s := strings.TrimSpace(strings.ToLower(status))
	if _, ok := policyStatusOrder[s]; ok {
		return s
	}
	return horizonmodels.PolicyStatusClean
}

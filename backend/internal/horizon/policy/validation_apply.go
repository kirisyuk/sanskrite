package policy

import (
	"log"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ValidationDecisionHandlers defines best-effort actions to apply a validation decision.
type ValidationDecisionHandlers struct {
	Warn            func(msg string)
	ApplyPenalty    func(penalty float64, reason string)
	Reverse         func()
	RewriteType     func(newType string, reason string)
	SetPolicyStatus func(status, reason string, penalty float64)
}

// ApplyValidationDecisionBestEffort applies validation policy decisions consistently.
// It never blocks execution; quarantine_soft remains non-blocking in best-effort runtime.
func ApplyValidationDecisionBestEffort(logPrefix string, decision PolicyDecision, handlers ValidationDecisionHandlers) {
	decision = NormalizeValidationDecision(decision)

	switch decision.Action {
	case ActionQuarantineSoft:
		reason, _ := decision.Params["reason"].(string)
		if reason == "" {
			reason = "policy quarantine_soft"
		}
		penalty := 0.2
		if p, ok := decision.Params["penalty"].(float64); ok && p > 0 {
			penalty = p
		}
		log.Printf("%s QUARANTINE_SOFT by ValidationPolicy (non-blocking): %.2f, reason: %s", logPrefix, penalty, reason)
		if handlers.Warn != nil {
			handlers.Warn(reason)
		}
		if handlers.ApplyPenalty != nil {
			handlers.ApplyPenalty(penalty, reason)
		}
		if handlers.SetPolicyStatus != nil {
			handlers.SetPolicyStatus(horizonmodels.PolicyStatusQuarantined, reason, penalty)
		}
	case ActionReverse:
		shouldReverse, _ := decision.Params["shouldReverse"].(bool)
		if shouldReverse {
			log.Printf("%s REVERSING by ValidationPolicy", logPrefix)
			if handlers.Reverse != nil {
				handlers.Reverse()
			}
			if handlers.SetPolicyStatus != nil {
				reason, _ := decision.Params["reason"].(string)
				handlers.SetPolicyStatus(horizonmodels.PolicyStatusAutoFixed, reason, 0)
			}
		}
	case ActionAutoFix:
		reason, _ := decision.Params["reason"].(string)
		if newType, ok := decision.Params["newType"].(string); ok && newType != "" && handlers.RewriteType != nil {
			handlers.RewriteType(newType, reason)
		}
		shouldReverse, _ := decision.Params["shouldReverse"].(bool)
		if shouldReverse && handlers.Reverse != nil {
			handlers.Reverse()
		}
		penalty, _ := decision.Params["penalty"].(float64)
		if penalty > 0 && handlers.ApplyPenalty != nil {
			handlers.ApplyPenalty(penalty, reason)
		}
		if handlers.SetPolicyStatus != nil {
			handlers.SetPolicyStatus(horizonmodels.PolicyStatusAutoFixed, reason, penalty)
		}
	case ActionRewriteType:
		reason, _ := decision.Params["reason"].(string)
		if newType, ok := decision.Params["newType"].(string); ok && strings.TrimSpace(newType) != "" && handlers.RewriteType != nil {
			handlers.RewriteType(newType, reason)
		}
		penalty, _ := decision.Params["penalty"].(float64)
		if penalty > 0 && handlers.ApplyPenalty != nil {
			handlers.ApplyPenalty(penalty, reason)
		}
		if handlers.SetPolicyStatus != nil {
			handlers.SetPolicyStatus(horizonmodels.PolicyStatusAutoFixed, reason, penalty)
		}
	case ActionAllowWithPenalty:
		penalty, _ := decision.Params["penalty"].(float64)
		reason, _ := decision.Params["reason"].(string)
		if penalty < 0 {
			penalty = 0
		}
		log.Printf("%s ALLOW_WITH_PENALTY by ValidationPolicy: %.2f, reason: %s", logPrefix, penalty, reason)
		if handlers.ApplyPenalty != nil && penalty > 0 {
			handlers.ApplyPenalty(penalty, reason)
		}
		if handlers.SetPolicyStatus != nil {
			handlers.SetPolicyStatus(horizonmodels.PolicyStatusPenalized, reason, penalty)
		}
	case ActionAllow:
		log.Printf("%s ALLOWED by ValidationPolicy", logPrefix)
		if handlers.SetPolicyStatus != nil {
			handlers.SetPolicyStatus(horizonmodels.PolicyStatusClean, "", 0)
		}
	default:
		log.Printf("%s WARNING: unknown policy action: %s, allowing", logPrefix, decision.Action)
	}
}

package policy

import "log"

// ValidationDecisionHandlers defines best-effort actions to apply a validation decision.
type ValidationDecisionHandlers struct {
	Warn         func(msg string)
	ApplyPenalty func(penalty float64, reason string)
	Reverse      func()
}

// ApplyValidationDecisionBestEffort applies validation policy decisions consistently.
// It never blocks execution; reject is treated as a warning.
func ApplyValidationDecisionBestEffort(logPrefix string, decision PolicyDecision, handlers ValidationDecisionHandlers) {
	switch decision.Action {
	case "reject":
		reason, _ := decision.Params["reason"].(string)
		if reason == "" {
			reason = "policy reject"
		}
		log.Printf("%s WARNING: ValidationPolicy would reject (non-blocking): %s", logPrefix, reason)
		if handlers.Warn != nil {
			handlers.Warn(reason)
		}
	case "reverse":
		shouldReverse, _ := decision.Params["shouldReverse"].(bool)
		if shouldReverse {
			log.Printf("%s REVERSING by ValidationPolicy", logPrefix)
			if handlers.Reverse != nil {
				handlers.Reverse()
			}
		}
	case "penalty":
		penalty, _ := decision.Params["penalty"].(float64)
		reason, _ := decision.Params["reason"].(string)
		if penalty < 0 {
			penalty = 0
		}
		log.Printf("%s PENALTY by ValidationPolicy: %.2f, reason: %s", logPrefix, penalty, reason)
		if handlers.ApplyPenalty != nil && penalty > 0 {
			handlers.ApplyPenalty(penalty, reason)
		}
	case "allow":
		log.Printf("%s ALLOWED by ValidationPolicy", logPrefix)
	default:
		log.Printf("%s WARNING: unknown policy action: %s, allowing", logPrefix, decision.Action)
	}
}

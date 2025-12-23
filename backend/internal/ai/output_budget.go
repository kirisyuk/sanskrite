package ai

import (
	"strings"
)

// computeMaxOutputTokens returns a safe max_tokens/maxOutputTokens value for a given model+task.
// This is important for reasoning models where "thinking"/reasoning can consume output budget,
// and for providers that otherwise default to small completion limits (causing truncated JSON).
func computeMaxOutputTokens(model string, caps *AIModelCapabilities, task *AITask, inputTokensEst int) int {
	// Default output targets if caller didn't specify.
	target := 0
	if task != nil {
		target = task.AIRequirements.MaxOutputSize
	}
	if target <= 0 {
		// Conservative default for structured JSON / typical tasks.
		target = 2048
	}

	// Add headroom for reasoning models where hidden reasoning can consume completion budget.
	headroom := 0
	name := strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(name, "reasoning") {
		// Add up to +4096 tokens, but don't explode for small targets.
		headroom = max(512, min(4096, target))
	}

	// Respect model constraints if known.
	maxByModel := 0
	maxByContext := 0
	if caps != nil {
		if caps.OutputTokens > 0 {
			maxByModel = caps.OutputTokens
		}
		if caps.ContextWindow > 0 {
			// Keep a reserve to avoid hitting hard limits / tool overhead.
			reserve := 1024
			if strings.Contains(name, "reasoning") {
				reserve = 4096
			}
			avail := caps.ContextWindow - inputTokensEst - reserve
			if avail > 0 {
				maxByContext = avail
			}
		}
	}

	desired := target + headroom
	// Clamp by model/context if present.
	if maxByModel > 0 {
		desired = min(desired, maxByModel)
	}
	if maxByContext > 0 {
		desired = min(desired, maxByContext)
	}

	// Never return absurdly small values; we'd rather allow some output than guarantee truncation.
	if desired < 256 {
		desired = 256
	}
	return desired
}




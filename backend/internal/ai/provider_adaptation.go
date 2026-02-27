package ai

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

type providerPressureState struct {
	ConsecutiveFailures int
	BlockedUntil        time.Time
	LastErrorClass      string
	LastError           string
	UpdatedAt           time.Time
}

const (
	providerPressureBaseBackoff = 3 * time.Second
	providerPressureMaxBackoff  = 90 * time.Second
)

func classifyProviderPressureError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "429"),
		strings.Contains(msg, "rate limit"),
		strings.Contains(msg, "too many requests"),
		strings.Contains(msg, "quota exceeded"),
		strings.Contains(msg, "resource exhausted"):
		return "rate_limit", true
	case strings.Contains(msg, "empty response"),
		strings.Contains(msg, "no content"),
		strings.Contains(msg, "empty text"),
		strings.Contains(msg, "null or empty response"):
		return "empty_response", true
	default:
		return "", false
	}
}

func providerPressureBackoff(consecutive int) time.Duration {
	if consecutive < 1 {
		consecutive = 1
	}
	backoff := providerPressureBaseBackoff
	for i := 1; i < consecutive; i++ {
		backoff *= 2
		if backoff >= providerPressureMaxBackoff {
			return providerPressureMaxBackoff
		}
	}
	if backoff > providerPressureMaxBackoff {
		return providerPressureMaxBackoff
	}
	return backoff
}

func (e *AITaskExecutor) recordProviderFailure(provider AIProvider, model string, err error) {
	if e == nil || err == nil {
		return
	}
	errorClass, isPressure := classifyProviderPressureError(err)
	if !isPressure {
		return
	}
	e.providerPressureMu.Lock()
	defer e.providerPressureMu.Unlock()
	if e.providerPressure == nil {
		e.providerPressure = make(map[AIProvider]providerPressureState)
	}
	st := e.providerPressure[provider]
	st.ConsecutiveFailures++
	st.LastErrorClass = errorClass
	st.LastError = strings.TrimSpace(err.Error())
	st.UpdatedAt = time.Now().UTC()
	backoff := providerPressureBackoff(st.ConsecutiveFailures)
	st.BlockedUntil = st.UpdatedAt.Add(backoff)
	e.providerPressure[provider] = st
	log.Printf("[AITaskExecutor] provider pressure: provider=%s model=%s class=%s consecutive=%d blocked_for=%s err=%s",
		provider, strings.TrimSpace(model), errorClass, st.ConsecutiveFailures, backoff.String(), st.LastError)
}

func (e *AITaskExecutor) recordProviderSuccess(provider AIProvider) {
	if e == nil {
		return
	}
	e.providerPressureMu.Lock()
	defer e.providerPressureMu.Unlock()
	if e.providerPressure == nil {
		return
	}
	st, ok := e.providerPressure[provider]
	if !ok {
		return
	}
	if st.ConsecutiveFailures == 0 && st.BlockedUntil.IsZero() {
		return
	}
	st.ConsecutiveFailures = 0
	st.BlockedUntil = time.Time{}
	st.LastErrorClass = ""
	st.LastError = ""
	st.UpdatedAt = time.Now().UTC()
	e.providerPressure[provider] = st
}

func (e *AITaskExecutor) blockedProviders(now time.Time) map[AIProvider]struct{} {
	out := map[AIProvider]struct{}{}
	if e == nil {
		return out
	}
	e.providerPressureMu.Lock()
	defer e.providerPressureMu.Unlock()
	if e.providerPressure == nil {
		return out
	}
	for provider, st := range e.providerPressure {
		if st.BlockedUntil.After(now) {
			out[provider] = struct{}{}
			continue
		}
		if !st.BlockedUntil.IsZero() {
			st.BlockedUntil = time.Time{}
			if st.ConsecutiveFailures > 1 {
				st.ConsecutiveFailures = st.ConsecutiveFailures / 2
			} else {
				st.ConsecutiveFailures = 0
			}
			e.providerPressure[provider] = st
		}
	}
	return out
}

func dedupeModelExcludes(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

func (e *AITaskExecutor) applyProviderPressureExclusions(excludeModels []string) ([]string, int) {
	if e == nil || e.modelManager == nil {
		return excludeModels, 0
	}
	blocked := e.blockedProviders(time.Now().UTC())
	if len(blocked) == 0 {
		return dedupeModelExcludes(excludeModels), 0
	}
	out := make([]string, 0, len(excludeModels)+8)
	out = append(out, excludeModels...)
	existing := make(map[string]struct{}, len(out))
	for _, m := range out {
		existing[strings.TrimSpace(m)] = struct{}{}
	}
	added := 0
	for modelName, caps := range e.modelManager.models {
		if _, isBlocked := blocked[caps.Provider]; !isBlocked {
			continue
		}
		if _, ok := existing[modelName]; ok {
			continue
		}
		out = append(out, modelName)
		existing[modelName] = struct{}{}
		added++
	}
	if added > 0 {
		providers := make([]string, 0, len(blocked))
		for p := range blocked {
			providers = append(providers, string(p))
		}
		log.Printf("[AITaskExecutor] adaptive provider exclusion active: blockedProviders=%v excludedModelsAdded=%d",
			providers, added)
	}
	return dedupeModelExcludes(out), added
}

func (e *AITaskExecutor) selectFallbackModelAdaptive(req AIRequirements, inputTokens int, triedModels []string) string {
	if e == nil || e.modelManager == nil {
		return ""
	}
	adaptedExcludes, blockedAdded := e.applyProviderPressureExclusions(triedModels)
	model := e.modelManager.SelectFallbackModel(req, inputTokens, adaptedExcludes)
	if model == "" && blockedAdded > 0 {
		// Fail-open: if all providers are temporarily blocked, allow one best-effort attempt.
		model = e.modelManager.SelectFallbackModel(req, inputTokens, triedModels)
		if model != "" {
			log.Printf("[AITaskExecutor] fail-open provider selection used model=%s despite pressure blocks", model)
		}
	}
	return model
}

func (e *AITaskExecutor) selectFallbackToolModelAdaptive(req AIRequirements, inputTokens int, triedModels []string) string {
	if e == nil || e.modelManager == nil {
		return ""
	}
	adaptedExcludes, blockedAdded := e.applyProviderPressureExclusions(triedModels)
	model := e.modelManager.SelectFallbackToolCallingModel(req, inputTokens, adaptedExcludes)
	if model == "" && blockedAdded > 0 {
		model = e.modelManager.SelectFallbackToolCallingModel(req, inputTokens, triedModels)
		if model != "" {
			log.Printf("[AITaskExecutor] fail-open tool-calling selection used model=%s despite pressure blocks", model)
		}
	}
	return model
}

func providerFailureSummary(provider AIProvider, model string, err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("provider=%s model=%s err=%s", provider, strings.TrimSpace(model), strings.TrimSpace(err.Error()))
}

package policy

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

const (
	PolicyLogStageInput    = "input"
	PolicyLogStageDecision = "decision"
	PolicyLogStageApply    = "apply"
	PolicyLogStageFallback = "fallback"
	PolicyLogStageResult   = "result"
	PolicyLogStageError    = "error"
)

// LogPolicyEvent emits a structured policy log line with unified prefix.
func LogPolicyEvent(policyName, stage string, fields map[string]interface{}) {
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		policyName = "unknown_policy"
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = PolicyLogStageResult
	}

	parts := make([]string, 0, len(fields))
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, fields[k]))
	}

	log.Printf("[HPolicy:%s:%s] %s", policyName, stage, strings.Join(parts, " "))
}

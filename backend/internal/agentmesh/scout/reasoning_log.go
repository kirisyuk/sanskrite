package scout

import (
	"log"
	"os"
	"strings"
)

// AGENTMESH_SCOUT_REASONING_LOG=1|true|yes|on enables operator-facing scout reasoning logs
// (beats, orchestrate decisions, research tool rounds, model text previews). Not raw Vertex HTTP.
func scoutReasoningLogEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AGENTMESH_SCOUT_REASONING_LOG")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func scoutReasoningLogf(format string, args ...any) {
	if !scoutReasoningLogEnabled() {
		return
	}
	log.Printf("[agentmesh.scout.reasoning] "+format, args...)
}

func scoutReasoningSingleLine(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if max > 0 && len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func logAppendedReasoningBeat(b ReasoningBeat) {
	if !scoutReasoningLogEnabled() {
		return
	}
	scoutReasoningLogf(
		"beat phase=%s turn=%d title=%s Q=%s decision=%s why=%s inference=%s looked=%v found=%v missing=%v branches=%v",
		b.Phase, b.Turn,
		scoutReasoningSingleLine(b.Title, 160),
		scoutReasoningSingleLine(b.QuestionWeAreAnswering, 320),
		b.Decision,
		scoutReasoningSingleLine(b.DecisionWhy, 240),
		scoutReasoningSingleLine(b.Inference, 240),
		b.WhatWeLookedAt, b.WhatWeFound, b.WhatWeDidNotFind, b.BranchIDs,
	)
}

package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	SupplyChainSufficiencyValidated   = "validated"
	SupplyChainSufficiencyHeuristic   = "heuristic"
	SupplyChainSufficiencyForensicGap = "forensic_gap"
	SupplyChainJoinEntityReuseOnly    = "reused_only"
	SupplyChainJoinEntityReuseBlocked = "blocked_new_entity"
	SupplyChainJoinEntityReuseUnknown = "unresolved"
)

type SupplyChainPathSufficiencySummary struct {
	Status              string
	Score               float64
	GapCount            int
	ValidatedJoinCount  int
	UnresolvedJoinCount int
	IsConfirmedPath     bool
}

func IsValidSupplyChainSufficiencyStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "", SupplyChainSufficiencyValidated, SupplyChainSufficiencyHeuristic, SupplyChainSufficiencyForensicGap:
		return true
	default:
		return false
	}
}

func decodeSupplyChainPathSufficiency(raw interface{}) (*SupplyChainPathSufficiency, error) {
	if raw == nil {
		return nil, nil
	}
	switch typed := raw.(type) {
	case *SupplyChainPathSufficiency:
		return typed, nil
	case SupplyChainPathSufficiency:
		copy := typed
		return &copy, nil
	case map[string]interface{}:
		buf, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("payload sufficiency marshal failed: %w", err)
		}
		var parsed SupplyChainPathSufficiency
		if err := json.Unmarshal(buf, &parsed); err != nil {
			return nil, fmt.Errorf("payload sufficiency unmarshal failed: %w", err)
		}
		if !IsValidSupplyChainSufficiencyStatus(parsed.Status) {
			return nil, fmt.Errorf("payload sufficiency has invalid status %q", parsed.Status)
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("payload 'sufficiency' must be an object")
	}
}

func EffectivePathStatusFromSufficiency(s *SupplyChainPathSufficiency) string {
	if s != nil && strings.TrimSpace(s.Status) == SupplyChainSufficiencyValidated {
		return "validated"
	}
	return "hypothesis"
}

func SummarizeSupplyChainPathSufficiency(parsed *SupplyChainPathPayload) SupplyChainPathSufficiencySummary {
	out := SupplyChainPathSufficiencySummary{}
	if parsed == nil || parsed.Sufficiency == nil {
		return out
	}
	s := parsed.Sufficiency
	out.Status = strings.TrimSpace(s.Status)
	out.Score = s.Score
	joinedGaps := mergeUniqueStrings(nil, append(append([]string(nil), s.HardFailures...), s.OpenGaps...))
	out.GapCount = len(joinedGaps)
	for _, join := range s.JoinAssessments {
		if strings.TrimSpace(join.Status) == SupplyChainSufficiencyValidated {
			out.ValidatedJoinCount++
		}
		if strings.TrimSpace(join.Status) != SupplyChainSufficiencyValidated &&
			(strings.TrimSpace(join.EntityReuse) == SupplyChainJoinEntityReuseUnknown ||
				strings.TrimSpace(join.ProductContinuity) == "unknown" ||
				strings.TrimSpace(join.TemporalContinuity) == "unknown") {
			out.UnresolvedJoinCount++
		}
	}
	out.IsConfirmedPath = EffectivePathStatusFromSufficiency(s) == "validated"
	return out
}

func PopulateSupplyChainPathSufficiency(artifact *MaterializedArtifact) {
	if artifact == nil || artifact.Kind != MaterializedKindSupplyChainPath {
		return
	}
	artifact.SufficiencyStatus = ""
	artifact.SufficiencyScore = 0
	artifact.GapCount = 0
	artifact.UnresolvedJoinCount = 0
	artifact.ValidatedJoinCount = 0
	artifact.IsConfirmedPath = false

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return
	}
	summary := SummarizeSupplyChainPathSufficiency(parsed)
	artifact.SufficiencyStatus = summary.Status
	artifact.SufficiencyScore = summary.Score
	artifact.GapCount = summary.GapCount
	artifact.UnresolvedJoinCount = summary.UnresolvedJoinCount
	artifact.ValidatedJoinCount = summary.ValidatedJoinCount
	artifact.IsConfirmedPath = summary.IsConfirmedPath
}

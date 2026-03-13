package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

func decodeSupplyChainPathFamily(raw interface{}) (*SupplyChainPathFamily, error) {
	if raw == nil {
		return nil, nil
	}
	switch typed := raw.(type) {
	case *SupplyChainPathFamily:
		return typed, nil
	case SupplyChainPathFamily:
		copyValue := typed
		return &copyValue, nil
	case map[string]interface{}:
		buf, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("payload family marshal failed: %w", err)
		}
		var parsed SupplyChainPathFamily
		if err := json.Unmarshal(buf, &parsed); err != nil {
			return nil, fmt.Errorf("payload family unmarshal failed: %w", err)
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("payload 'family' must be an object")
	}
}

func PopulateSupplyChainPathFamily(artifact *MaterializedArtifact) {
	if artifact == nil || artifact.Kind != MaterializedKindSupplyChainPath {
		return
	}
	artifact.FamilyID = ""
	artifact.FamilyPathCount = 0
	artifact.FamilyValidatedCount = 0
	artifact.FamilyGapCount = 0

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil || parsed.Family == nil {
		return
	}
	artifact.FamilyID = strings.TrimSpace(parsed.Family.ID)
	artifact.FamilyPathCount = parsed.Family.PathCount
	artifact.FamilyValidatedCount = parsed.Family.ValidatedPathCount
	artifact.FamilyGapCount = len(parsed.Family.OpenGaps)
	if parsed.Family.GapPathCount > artifact.FamilyGapCount {
		artifact.FamilyGapCount = parsed.Family.GapPathCount
	}
}

func mergePathFamily(existing, incoming *SupplyChainPathFamily) *SupplyChainPathFamily {
	if existing == nil && incoming == nil {
		return nil
	}
	if existing == nil {
		copyValue := *incoming
		copyValue.OpenGaps = mergeUniqueStrings(nil, incoming.OpenGaps)
		return &copyValue
	}
	if incoming == nil {
		copyValue := *existing
		copyValue.OpenGaps = mergeUniqueStrings(nil, existing.OpenGaps)
		return &copyValue
	}
	merged := &SupplyChainPathFamily{
		ID:                 firstNonEmptyTrimmed(incoming.ID, existing.ID),
		AnchorSupplier:     firstNonEmptyTrimmed(incoming.AnchorSupplier, existing.AnchorSupplier),
		AnchorCustomer:     firstNonEmptyTrimmed(incoming.AnchorCustomer, existing.AnchorCustomer),
		ProductAxis:        firstNonEmptyTrimmed(incoming.ProductAxis, existing.ProductAxis),
		PathCount:          maxInt(existing.PathCount, incoming.PathCount),
		ValidatedPathCount: maxInt(existing.ValidatedPathCount, incoming.ValidatedPathCount),
		HeuristicPathCount: maxInt(existing.HeuristicPathCount, incoming.HeuristicPathCount),
		GapPathCount:       maxInt(existing.GapPathCount, incoming.GapPathCount),
		OpenGaps:           mergeUniqueStrings(existing.OpenGaps, incoming.OpenGaps),
		CoResearchRound:    maxInt(existing.CoResearchRound, incoming.CoResearchRound),
	}
	return merged
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

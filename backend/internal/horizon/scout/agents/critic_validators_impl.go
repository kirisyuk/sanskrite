package agents

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// CoreValidator validates canonical path, lifecycle, and basic fields.
type CoreValidator struct {
	agent *CriticAgent
}

func (v *CoreValidator) DomainMode() string { return "core" }

func (v *CoreValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildCoreValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// SupplyChainValidator validates supply chains and related edges.
type SupplyChainValidator struct {
	agent *CriticAgent
}

func (v *SupplyChainValidator) DomainMode() string { return taxonomy.DomainModeSupplyChain }

func (v *SupplyChainValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildSupplyChainValidationPrompt(input)
	result, err := v.agent.runValidationPrompt(ctx, prompt)
	if err != nil || result == nil {
		return result, err
	}
	// Deterministic invariant checks: time overlap, unit norm, flow conservation, allocation
	invariantGaps := checkSupplyChainInvariants(input.SupplyChains, input.WorkingProductScope)
	if len(invariantGaps) > 0 {
		if result.MissingInfo == nil {
			result.MissingInfo = []string{}
		}
		result.MissingInfo = append(result.MissingInfo, invariantGaps...)
		result.IsValid = false
		if result.Confidence > 0.5 {
			result.Confidence = 0.5
		}
	}
	return result, nil
}

// checkSupplyChainInvariants runs deterministic checks: time overlap, unit normalization, flow conservation, allocation consistency (plan 8.1).
// Returns missingInfo items for failed invariants.
func checkSupplyChainInvariants(chains []SupplyChain, workingProductScope []string) []string {
	var gaps []string
	scopeSet := make(map[string]struct{}, len(workingProductScope))
	for _, productRef := range workingProductScope {
		if productRef = strings.TrimSpace(productRef); productRef != "" {
			scopeSet[productRef] = struct{}{}
		}
	}
	for _, ch := range chains {
		// Time overlap: adjacent segment metrics should have overlapping periods (plan 8.1)
		for i := 0; i < len(ch.SegmentMetrics)-1; i++ {
			m1, m2 := ch.SegmentMetrics[i], ch.SegmentMetrics[i+1]
			if !segmentPeriodsOverlap(&m1, &m2) {
				gaps = append(gaps, "supply_chain_invariant: adjacent segment periods do not overlap")
			}
		}
		// Unit normalization: detect incompatible unit families (not raw unit-token variety).
		seenFamilies := make(map[string]struct{})
		for _, m := range ch.SegmentMetrics {
			if m.Volume != nil && m.Volume.Unit != "" {
				if fam := volumeUnitFamily(m.Volume.Unit); fam != "" {
					seenFamilies[fam] = struct{}{}
				}
			}
		}
		if len(seenFamilies) > 2 {
			gaps = append(gaps, "supply_chain_invariant: unit mix in chain may violate flow conservation")
		}
		// Flow conservation: mass/energy balance — sum in ≈ sum out (with tolerance)
		if flowGap := checkFlowConservation(ch); flowGap != "" {
			gaps = append(gaps, flowGap)
		}
		// Allocation: shares for same customer should sum to ~100%
		if allocGap := checkAllocationConsistency(ch); allocGap != "" {
			gaps = append(gaps, allocGap)
		}
		gaps = append(gaps, checkScopedSupplyChainSemantics(ch, scopeSet)...)
	}
	return gaps
}

func checkScopedSupplyChainSemantics(ch SupplyChain, scopeSet map[string]struct{}) []string {
	var gaps []string
	chainProducts := make(map[string]struct{})
	for _, node := range ch.Nodes {
		cp := strings.TrimSpace(node.CanonicalPath)
		if strings.HasPrefix(cp, "/product/") {
			chainProducts[cp] = struct{}{}
		}
	}

	candidateSeen := make(map[string]struct{})
	for _, candidate := range ch.ProductCandidates {
		ref := strings.TrimSpace(candidate.CanonicalPath)
		if ref == "" {
			continue
		}
		if _, ok := candidateSeen[ref]; ok {
			gaps = append(gaps, "supply_chain_invariant: duplicate productCandidate canonicalPath in same chain")
			continue
		}
		candidateSeen[ref] = struct{}{}
		if _, ok := chainProducts[ref]; ok {
			gaps = append(gaps, "supply_chain_invariant: productCandidate duplicates an already materialized chain product")
		}
		if _, ok := scopeSet[ref]; ok {
			gaps = append(gaps, "supply_chain_invariant: productCandidate duplicates an existing working product scope product")
		}
	}

	for _, obs := range ch.CarrierProductObservations {
		if strings.TrimSpace(obs.CarrierRef) == "" || strings.TrimSpace(obs.ProductRef) == "" {
			gaps = append(gaps, "supply_chain_invariant: carrierProductObservation requires carrierRef and productRef")
			continue
		}
		if _, ok := chainProducts[strings.TrimSpace(obs.ProductRef)]; !ok {
			if _, ok := scopeSet[strings.TrimSpace(obs.ProductRef)]; !ok {
				gaps = append(gaps, "supply_chain_invariant: carrierProductObservation productRef must be present in chain or working product scope")
			}
		}
	}
	for _, obs := range ch.LinkObservations {
		if strings.TrimSpace(obs.ProductRef) == "" {
			gaps = append(gaps, "supply_chain_invariant: linkObservation requires productRef")
			continue
		}
		if _, ok := chainProducts[strings.TrimSpace(obs.ProductRef)]; !ok {
			if _, ok := scopeSet[strings.TrimSpace(obs.ProductRef)]; !ok {
				gaps = append(gaps, "supply_chain_invariant: linkObservation productRef must be present in chain or working product scope")
			}
		}
	}
	for _, obs := range ch.RouteObservations {
		if strings.TrimSpace(obs.ProductRef) == "" && len(obs.TransportVia) == 0 {
			gaps = append(gaps, "supply_chain_invariant: routeObservation requires productRef or transportVia")
			continue
		}
		if productRef := strings.TrimSpace(obs.ProductRef); productRef != "" {
			if _, ok := chainProducts[productRef]; !ok {
				if _, ok := scopeSet[productRef]; !ok {
					gaps = append(gaps, "supply_chain_invariant: routeObservation productRef must be present in chain or working product scope")
				}
			}
		}
	}

	return gaps
}

// segmentPeriodsOverlap returns true if m1 and m2 have overlapping time periods, or if we cannot determine (fail-open).
// Supports Period ("2023", "2023-Q1"), Start/End (ISO8601).
func segmentPeriodsOverlap(m1, m2 *SegmentMetrics) bool {
	s1, e1 := segmentBounds(m1)
	s2, e2 := segmentBounds(m2)
	if s1 == nil || e1 == nil || s2 == nil || e2 == nil {
		return true // fail-open: cannot determine, assume OK
	}
	// Overlap: start1 <= end2 && start2 <= end1
	return !s1.After(*e2) && !s2.After(*e1)
}

// segmentBounds returns (start, end) for a SegmentMetrics. Returns (nil, nil) if cannot parse.
var periodYearRe = regexp.MustCompile(`^(\d{4})$`)
var periodQuarterRe = regexp.MustCompile(`^(\d{4})-Q([1-4])$`)

func segmentBounds(m *SegmentMetrics) (*time.Time, *time.Time) {
	if m == nil {
		return nil, nil
	}
	// Prefer Start/End if both present
	if m.Start != "" && m.End != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01"} {
			s, err1 := time.Parse(layout, m.Start)
			e, err2 := time.Parse(layout, m.End)
			if err1 == nil && err2 == nil {
				return &s, &e
			}
		}
	}
	// Parse Period: "2023", "2023-Q1"
	if m.Period != "" {
		if sub := periodYearRe.FindStringSubmatch(m.Period); len(sub) == 2 {
			y, _ := strconv.Atoi(sub[1])
			s := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
			e := time.Date(y, 12, 31, 23, 59, 59, 999999999, time.UTC)
			return &s, &e
		}
		if sub := periodQuarterRe.FindStringSubmatch(m.Period); len(sub) == 3 {
			y, _ := strconv.Atoi(sub[1])
			q, _ := strconv.Atoi(sub[2])
			monthStart := (q-1)*3 + 1
			monthEnd := q * 3
			s := time.Date(y, time.Month(monthStart), 1, 0, 0, 0, 0, time.UTC)
			e := time.Date(y, time.Month(monthEnd), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, -1)
			return &s, &e
		}
	}
	return nil, nil
}

// checkFlowConservation: for nodes with both in/out edges, sum(volume in) ≈ sum(volume out).
func checkFlowConservation(ch SupplyChain) string {
	if len(ch.Nodes) < 3 || len(ch.SegmentMetrics) == 0 {
		return ""
	}
	// Build node index -> unit family -> total volume in/out (normalized to family base units).
	volIn := make(map[int]map[string]float64)
	volOut := make(map[int]map[string]float64)
	add := func(dst map[int]map[string]float64, nodeIdx int, family string, value float64) {
		if _, ok := dst[nodeIdx]; !ok {
			dst[nodeIdx] = make(map[string]float64)
		}
		dst[nodeIdx][family] += value
	}
	for _, m := range ch.SegmentMetrics {
		if m.Volume == nil {
			continue
		}
		family, normalized, ok := normalizeVolumeToFamilyBase(m.Volume.Value, m.Volume.Unit)
		if !ok || normalized <= 0 {
			continue
		}
		add(volOut, m.FromNodeIndex, family, normalized)
		add(volIn, m.ToNodeIndex, family, normalized)
	}
	for nodeIdx, inByFamily := range volIn {
		outByFamily, ok := volOut[nodeIdx]
		if !ok {
			continue
		}
		for family, in := range inByFamily {
			out := outByFamily[family]
			if out <= 0 {
				continue
			}
			ratio := in / out
			if ratio < 0.5 || ratio > 2.0 {
				return fmt.Sprintf("supply_chain_invariant: flow conservation violated for unit_family=%s (sum in / sum out ratio out of 0.5–2.0)", family)
			}
		}
	}
	return ""
}

// checkAllocationConsistency: shares for same customer (productNodeIndex) should sum to ~100%.
func checkAllocationConsistency(ch SupplyChain) string {
	if len(ch.SegmentMetrics) == 0 {
		return ""
	}
	type shareGroup struct {
		sumPercent float64
		count      int
		hasUnknown bool
	}
	// Group by (ToNodeIndex, ProductIndex) — same customer receiving same product from multiple suppliers.
	shareByCustomerProduct := make(map[string]shareGroup)
	for _, m := range ch.SegmentMetrics {
		if m.Share == nil {
			continue
		}
		unit := taxonomy.NormalizeSupplyChainMetricUnit(m.Share.Unit, false)
		v := m.Share.Value
		percentValue := 0.0
		switch unit {
		case "ratio":
			percentValue = v * 100.0
		case "percent":
			percentValue = v
		case "":
			// Best-effort: infer from value range when unit omitted.
			if v >= 0 && v <= 1 {
				percentValue = v * 100.0
			} else {
				percentValue = v
			}
		default:
			g := shareByCustomerProduct[fmt.Sprintf("%d-%d", m.ToNodeIndex, m.ProductIndex)]
			g.hasUnknown = true
			shareByCustomerProduct[fmt.Sprintf("%d-%d", m.ToNodeIndex, m.ProductIndex)] = g
			continue
		}
		key := fmt.Sprintf("%d-%d", m.ToNodeIndex, m.ProductIndex)
		g := shareByCustomerProduct[key]
		g.sumPercent += percentValue
		g.count++
		shareByCustomerProduct[key] = g
	}
	for key, g := range shareByCustomerProduct {
		if g.hasUnknown || g.count < 2 {
			continue
		}
		sum := g.sumPercent
		if sum < 80 || sum > 120 {
			_ = key
			return "supply_chain_invariant: allocation shares for same customer/product should sum to ~100%"
		}
	}
	return ""
}

func normalizeVolumeToFamilyBase(value float64, unit string) (family string, normalized float64, ok bool) {
	if value <= 0 {
		return "", 0, false
	}
	u := taxonomy.NormalizeSupplyChainMetricUnit(strings.TrimSpace(unit), false)
	switch u {
	case "gwh_per_year":
		return "energy", value, true
	case "mwh_per_year":
		return "energy", value * 0.001, true
	case "kwh_per_year":
		return "energy", value * 0.000001, true
	case "metric_tons_per_year":
		return "mass", value, true
	case "kt_per_year":
		return "mass", value * 1000.0, true
	case "oz_per_year":
		return "mass", value * 0.000028349523125, true
	case "bpd":
		return "liquid", value, true
	case "mbpd":
		return "liquid", value * 1_000_000.0, true
	case "mcf_per_day":
		return "gas", value, true
	case "mmcf_per_day":
		return "gas", value * 1_000_000.0, true
	case "units_per_year", "cells":
		return "units", value, true
	default:
		return "", 0, false
	}
}

func volumeUnitFamily(unit string) string {
	u := taxonomy.NormalizeSupplyChainMetricUnit(strings.TrimSpace(unit), false)
	switch u {
	case "gwh_per_year", "mwh_per_year", "kwh_per_year":
		return "energy"
	case "metric_tons_per_year", "kt_per_year", "oz_per_year":
		return "mass"
	case "bpd", "mbpd":
		return "liquid"
	case "mcf_per_day", "mmcf_per_day":
		return "gas"
	case "units_per_year", "cells":
		return "units"
	default:
		return ""
	}
}

// GovernanceValidator validates ownership, board, key persons.
type GovernanceValidator struct {
	agent *CriticAgent
}

func (v *GovernanceValidator) DomainMode() string { return taxonomy.DomainModeGovernance }

func (v *GovernanceValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildGovernanceValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// AssuranceValidator validates audit, certification, regulatory oversight.
type AssuranceValidator struct {
	agent *CriticAgent
}

func (v *AssuranceValidator) DomainMode() string { return taxonomy.DomainModeAssurance }

func (v *AssuranceValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildAssuranceValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// MarketValidator validates market participation and sizing.
type MarketValidator struct {
	agent *CriticAgent
}

func (v *MarketValidator) DomainMode() string { return taxonomy.DomainModeMarket }

func (v *MarketValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildMarketValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// StrategicValidator wraps ValidateStrategicOverlay for the strategic domain mode.
type StrategicValidator struct {
	agent *CriticAgent
}

func (v *StrategicValidator) DomainMode() string { return taxonomy.DomainModeStrategic }

func (v *StrategicValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil || input.Fragment == nil {
		return nil, nil
	}
	return v.agent.ValidateStrategicOverlay(ctx, input.Fragment, input.Relations, input.GraphContext)
}

// PolicyImpactValidator validates policy-impact specific relation quality and coverage.
type PolicyImpactValidator struct {
	agent *CriticAgent
}

func (v *PolicyImpactValidator) DomainMode() string { return taxonomy.DomainModePolicyImpact }

func (v *PolicyImpactValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildPolicyImpactValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

// EcosystemValidator validates ecosystem/stakeholder mapping relation quality and coverage.
type EcosystemValidator struct {
	agent *CriticAgent
}

func (v *EcosystemValidator) DomainMode() string { return taxonomy.DomainModeEcosystem }

func (v *EcosystemValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildEcosystemValidationPrompt(input)
	return v.agent.runValidationPrompt(ctx, prompt)
}

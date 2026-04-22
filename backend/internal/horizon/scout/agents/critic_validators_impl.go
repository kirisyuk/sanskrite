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

// ServiceChainValidator validates service delivery chains and related edges.
type ServiceChainValidator struct {
	agent *CriticAgent
}

func (v *ServiceChainValidator) DomainMode() string { return taxonomy.DomainModeServiceChain }

func (v *ServiceChainValidator) Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error) {
	if v.agent == nil {
		return nil, nil
	}
	prompt := v.agent.buildServiceChainValidationPrompt(input)
	result, err := v.agent.runValidationPrompt(ctx, prompt)
	if err != nil || result == nil {
		return result, err
	}
	invariantGaps := checkServiceChainInvariants(input.SupplyChains, input.WorkingProductScope)
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
	scopeSet := buildOperationalScopeSet(workingProductScope)
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
	return dedupInvariantGaps(gaps)
}

// checkServiceChainInvariants runs deterministic checks for service delivery chains.
// Service chains intentionally do not inherit supply-only mass/volume conservation rules.
func checkServiceChainInvariants(chains []SupplyChain, workingCapabilityScope []string) []string {
	var gaps []string
	scopeSet := buildOperationalScopeSet(workingCapabilityScope)
	for _, ch := range chains {
		hasProductNode := false
		hasServiceCapabilityNode := false
		for _, node := range ch.Nodes {
			cp := strings.TrimSpace(node.CanonicalPath)
			if strings.HasPrefix(cp, "/product/") {
				hasProductNode = true
			}
			if strings.HasPrefix(cp, "/product/service/") {
				hasServiceCapabilityNode = true
			}
		}
		if hasProductNode && !hasServiceCapabilityNode {
			gaps = append(gaps, "service_chain_invariant: chain lacks explicit /product/service capability path")
		}
		for i := 0; i < len(ch.SegmentMetrics)-1; i++ {
			m1, m2 := ch.SegmentMetrics[i], ch.SegmentMetrics[i+1]
			if !segmentPeriodsOverlap(&m1, &m2) {
				gaps = append(gaps, "service_chain_invariant: adjacent segment periods do not overlap")
			}
		}
		gaps = append(gaps, checkScopedServiceChainSemantics(ch, scopeSet)...)
		gaps = append(gaps, checkServiceChainObservationMetrics(ch)...)
	}
	return dedupInvariantGaps(gaps)
}

func checkGovernanceInvariants(relations []DiscoveredRelation) []string {
	var gaps []string
	if len(relations) == 0 {
		return gaps
	}

	type ownershipEdgeState struct {
		parents              int
		hasShareOrPercent    bool
		hasBeneficialOrHuman bool
	}

	ownershipByTarget := map[string]*ownershipEdgeState{}
	controlByPair := map[string]struct{}{}
	communitySupport := map[string]bool{}
	communitiesSeen := map[string]struct{}{}
	hasAnyOwnership := false
	hasBeneficialLayer := false

	for _, rel := range relations {
		rt := strings.ToUpper(strings.TrimSpace(rel.RelationType))
		src := strings.TrimSpace(rel.SourceCanonicalPath)
		tgt := strings.TrimSpace(rel.TargetCanonicalPath)
		pairKey := src + "->" + tgt

		switch rt {
		case "CONTROLS":
			controlByPair[pairKey] = struct{}{}
			if readQualifierString(rel.Qualifiers, "control_type", "controlType") == "" &&
				readQualifierString(rel.Qualifiers, "mechanism") == "" {
				gaps = append(gaps, "governance_invariant: CONTROLS edge missing control_type or mechanism")
			}
		case "OWNS", "SUBSIDIARY_OF":
			hasAnyOwnership = true
			state := ownershipByTarget[tgt]
			if state == nil {
				state = &ownershipEdgeState{}
				ownershipByTarget[tgt] = state
			}
			state.parents++
			shareType := strings.ToLower(readQualifierString(rel.Qualifiers, "share_type", "shareType"))
			ownershipPct, hasOwnershipPct := readMetricOrQualifierNumber(rel, "ownership_percent")
			votingPct, hasVotingPct := readMetricOrQualifierNumber(rel, "voting_rights_percent")
			if shareType != "" || hasOwnershipPct {
				state.hasShareOrPercent = true
			}
			if shareType == "beneficial" || shareType == "indirect" || strings.HasPrefix(src, "/person/") || strings.HasPrefix(src, "/community/") {
				state.hasBeneficialOrHuman = true
				hasBeneficialLayer = true
			}
			if !hasOwnershipPct && shareType == "" {
				gaps = append(gaps, "governance_invariant: ownership edge missing ownership_percent or share_type")
			}
			if hasOwnershipPct && hasVotingPct && absFloat(ownershipPct-votingPct) > 10.0 {
				if _, ok := controlByPair[pairKey]; !ok {
					gaps = append(gaps, "governance_invariant: voting-rights diverge from equity without matching CONTROLS edge")
				}
			}
		case "HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA":
			for _, p := range []string{src, tgt} {
				if strings.HasPrefix(p, "/community/") {
					communitySupport[p] = true
				}
			}
		}

		for _, p := range []string{src, tgt} {
			if strings.HasPrefix(p, "/community/") {
				communitiesSeen[p] = struct{}{}
			}
		}
	}

	for _, state := range ownershipByTarget {
		if state.parents > 1 && !state.hasShareOrPercent {
			gaps = append(gaps, "governance_invariant: multiple upstream owners without ownership percentages or share types")
		}
	}
	if hasAnyOwnership && !hasBeneficialLayer {
		gaps = append(gaps, "governance_invariant: ownership graph lacks beneficial-owner, person, or community lineage layer")
	}
	for cp := range communitiesSeen {
		if !communitySupport[cp] {
			gaps = append(gaps, "governance_invariant: community control node lacks lineage or affiliation support edges")
		}
	}

	return dedupInvariantGaps(gaps)
}

func checkPolicyImpactInvariants(relations []DiscoveredRelation) []string {
	var gaps []string
	if len(relations) == 0 {
		return gaps
	}

	hasSource := false
	hasDecision := false
	hasMechanism := false
	hasGeo := false
	hasAffected := false
	hasTransmission := false

	adj := map[string][]string{}
	seeds := map[string]struct{}{}
	policyRelevant := false

	for _, rel := range relations {
		rt := strings.ToUpper(strings.TrimSpace(rel.RelationType))
		src := strings.TrimSpace(rel.SourceCanonicalPath)
		tgt := strings.TrimSpace(rel.TargetCanonicalPath)
		if src == "" || tgt == "" {
			continue
		}
		switch rt {
		case "REGULATES", "CONTROLS_ACCESS", "IMPOSES_CONDITION", "GATEKEEPS_DECISION", "USES_MECHANISM", "APPLIES_TO", "IN_GEO_SCOPE", "AFFECTS", "ADMINISTERS", "OPERATES_VIA", "AFFILIATED_WITH", "HAS_SUBGROUP", "MEMBER_OF":
			policyRelevant = true
		default:
			continue
		}
		if strings.HasPrefix(src, "/gov/") || strings.HasPrefix(src, "/org/") || strings.HasPrefix(src, "/community/") || strings.HasPrefix(src, "/project/") {
			hasSource = true
			seeds[src] = struct{}{}
		}
		if strings.HasPrefix(src, "/decision/") {
			hasDecision = true
		}
		if strings.HasPrefix(src, "/mechanism/") {
			hasMechanism = true
		}
		if strings.HasPrefix(tgt, "/decision/") {
			hasDecision = true
		}
		if strings.HasPrefix(tgt, "/mechanism/") {
			hasMechanism = true
		}
		if strings.HasPrefix(tgt, "/loc/") {
			hasGeo = true
		}
		if strings.HasPrefix(src, "/loc/") {
			hasGeo = true
		}
		if hasPolicyAffectedPath(tgt) {
			hasAffected = true
		}
		if isPolicyTransmissionEdge(rt) || strings.HasPrefix(tgt, "/community/") || strings.HasPrefix(tgt, "/media/") || strings.HasPrefix(tgt, "/project/") {
			hasTransmission = true
		}
		adj[src] = append(adj[src], tgt)

		switch rt {
		case "CONTROLS_ACCESS":
			if readQualifierString(rel.Qualifiers, "type", "channel_type", "channelType") == "" {
				gaps = append(gaps, "policy_impact_invariant: CONTROLS_ACCESS edge missing type/channel qualifier")
			}
		case "IMPOSES_CONDITION":
			if readQualifierString(rel.Qualifiers, "type") == "" || readQualifierString(rel.Qualifiers, "impact") == "" {
				gaps = append(gaps, "policy_impact_invariant: IMPOSES_CONDITION edge missing type or impact qualifier")
			}
		case "IN_GEO_SCOPE":
			if readQualifierString(rel.Qualifiers, "scope_basis", "scopeBasis") == "" {
				gaps = append(gaps, "policy_impact_invariant: IN_GEO_SCOPE edge missing scope_basis")
			}
		}
	}

	if !policyRelevant {
		return dedupInvariantGaps(gaps)
	}
	if !hasSource {
		gaps = append(gaps, "policy_impact_invariant: graph lacks explicit policy source or gatekeeper layer")
	}
	if !hasDecision {
		gaps = append(gaps, "policy_impact_invariant: graph lacks decision-point layer")
	}
	if !hasMechanism {
		gaps = append(gaps, "policy_impact_invariant: graph lacks mechanism layer")
	}
	if !hasGeo {
		gaps = append(gaps, "policy_impact_invariant: graph lacks geo-scope layer")
	}
	if !hasAffected {
		gaps = append(gaps, "policy_impact_invariant: graph lacks affected exposure targets")
	}
	if !hasTransmission {
		gaps = append(gaps, "policy_impact_invariant: graph lacks transmission/response layer (community, program, or media tail)")
	}

	if len(seeds) > 0 {
		seen := map[string]struct{}{}
		queue := make([]string, 0, len(seeds))
		for seed := range seeds {
			seen[seed] = struct{}{}
			queue = append(queue, seed)
		}
		for len(queue) > 0 {
			u := queue[0]
			queue = queue[1:]
			for _, v := range adj[u] {
				if _, ok := seen[v]; ok {
					continue
				}
				seen[v] = struct{}{}
				queue = append(queue, v)
			}
		}
		for node := range collectPolicyCriticalNodes(relations) {
			if _, ok := seen[node]; !ok {
				gaps = append(gaps, "policy_impact_invariant: orphan policy node is not reachable from any source/gatekeeper layer")
				break
			}
		}
	}

	return dedupInvariantGaps(gaps)
}

func collectPolicyCriticalNodes(relations []DiscoveredRelation) map[string]struct{} {
	out := map[string]struct{}{}
	for _, rel := range relations {
		for _, p := range []string{strings.TrimSpace(rel.SourceCanonicalPath), strings.TrimSpace(rel.TargetCanonicalPath)} {
			switch {
			case strings.HasPrefix(p, "/decision/"),
				strings.HasPrefix(p, "/mechanism/"),
				strings.HasPrefix(p, "/community/"),
				strings.HasPrefix(p, "/project/"),
				strings.HasPrefix(p, "/media/"),
				strings.HasPrefix(p, "/loc/"):
				if p != "" {
					out[p] = struct{}{}
				}
			}
		}
	}
	return out
}

func hasPolicyAffectedPath(p string) bool {
	p = strings.TrimSpace(p)
	return strings.HasPrefix(p, "/org/") ||
		strings.HasPrefix(p, "/fac/") ||
		strings.HasPrefix(p, "/market/") ||
		strings.HasPrefix(p, "/product/") ||
		strings.HasPrefix(p, "/community/") ||
		strings.HasPrefix(p, "/project/") ||
		strings.HasPrefix(p, "/media/") ||
		strings.HasPrefix(p, "/loc/")
}

func isPolicyTransmissionEdge(rt string) bool {
	switch rt {
	case "AFFECTS", "OPERATES_VIA", "AFFILIATED_WITH", "HAS_SUBGROUP", "MEMBER_OF":
		return true
	default:
		return false
	}
}

func readQualifierString(qualifiers map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		v, ok := qualifiers[key]
		if !ok || v == nil {
			continue
		}
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func readMetricOrQualifierNumber(rel DiscoveredRelation, key string) (float64, bool) {
	if rel.Metrics != nil {
		if mv, ok := rel.Metrics[key]; ok {
			if mv.Value != 0 {
				return mv.Value, true
			}
		}
	}
	if rel.Qualifiers != nil {
		if raw, ok := rel.Qualifiers[key]; ok && raw != nil {
			switch v := raw.(type) {
			case float64:
				return v, true
			case float32:
				return float64(v), true
			case int:
				return float64(v), true
			case int64:
				return float64(v), true
			case string:
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "%")), 64); err == nil {
					return parsed, true
				}
			}
		}
	}
	return 0, false
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func dedupInvariantGaps(gaps []string) []string {
	if len(gaps) == 0 {
		return gaps
	}
	seen := make(map[string]struct{}, len(gaps))
	out := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		k := strings.TrimSpace(strings.ToLower(gap))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, gap)
	}
	return out
}

func buildOperationalScopeSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
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

func checkScopedServiceChainSemantics(ch SupplyChain, scopeSet map[string]struct{}) []string {
	var gaps []string
	chainCapabilities := make(map[string]struct{})
	for _, node := range ch.Nodes {
		cp := strings.TrimSpace(node.CanonicalPath)
		if strings.HasPrefix(cp, "/product/") {
			chainCapabilities[cp] = struct{}{}
		}
	}

	candidateSeen := make(map[string]struct{})
	for _, candidate := range ch.ProductCandidates {
		ref := strings.TrimSpace(candidate.CanonicalPath)
		if ref == "" {
			continue
		}
		if _, ok := candidateSeen[ref]; ok {
			gaps = append(gaps, "service_chain_invariant: duplicate productCandidate canonicalPath in same chain")
			continue
		}
		candidateSeen[ref] = struct{}{}
		if _, ok := chainCapabilities[ref]; ok {
			gaps = append(gaps, "service_chain_invariant: productCandidate duplicates an already materialized capability in the chain")
		}
		if _, ok := scopeSet[ref]; ok {
			gaps = append(gaps, "service_chain_invariant: productCandidate duplicates an existing working capability scope entry")
		}
	}

	for _, obs := range ch.CarrierProductObservations {
		if strings.TrimSpace(obs.CarrierRef) == "" || strings.TrimSpace(obs.ProductRef) == "" {
			gaps = append(gaps, "service_chain_invariant: carrierProductObservation requires carrierRef and productRef")
			continue
		}
		if !scopeOrChainContains(chainCapabilities, scopeSet, obs.ProductRef) {
			gaps = append(gaps, "service_chain_invariant: carrierProductObservation productRef must be present in chain or working capability scope")
		}
	}
	for _, obs := range ch.LinkObservations {
		if strings.TrimSpace(obs.ProductRef) == "" {
			gaps = append(gaps, "service_chain_invariant: linkObservation requires productRef")
			continue
		}
		if !scopeOrChainContains(chainCapabilities, scopeSet, obs.ProductRef) {
			gaps = append(gaps, "service_chain_invariant: linkObservation productRef must be present in chain or working capability scope")
		}
	}
	for _, obs := range ch.RouteObservations {
		if strings.TrimSpace(obs.ProductRef) == "" && len(obs.TransportVia) == 0 && !hasServiceControlQualifier(obs.Qualifiers) {
			gaps = append(gaps, "service_chain_invariant: routeObservation requires productRef, transportVia, or explicit control-path qualifier")
			continue
		}
		if productRef := strings.TrimSpace(obs.ProductRef); productRef != "" && !scopeOrChainContains(chainCapabilities, scopeSet, productRef) {
			gaps = append(gaps, "service_chain_invariant: routeObservation productRef must be present in chain or working capability scope")
		}
	}

	return gaps
}

func scopeOrChainContains(chainSet map[string]struct{}, scopeSet map[string]struct{}, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if _, ok := chainSet[ref]; ok {
		return true
	}
	_, ok := scopeSet[ref]
	return ok
}

func hasServiceControlQualifier(qualifiers map[string]interface{}) bool {
	for _, key := range []string{
		"auth_required",
		"approved_vendor_only",
		"service_contract_required",
		"region_lock_in",
		"export_license_required",
		"sanctions_touchpoint",
		"dual_use_flag",
	} {
		if _, ok := qualifiers[key]; ok {
			return true
		}
	}
	return false
}

func checkServiceChainObservationMetrics(ch SupplyChain) []string {
	var gaps []string
	for _, obs := range ch.CarrierProductObservations {
		gaps = append(gaps, validateServiceMetricMap(obs.Metrics)...)
		gaps = append(gaps, validateServiceQualifierMap(obs.Qualifiers)...)
	}
	for _, obs := range ch.LinkObservations {
		gaps = append(gaps, validateServiceMetricMap(obs.Metrics)...)
		gaps = append(gaps, validateServiceQualifierMap(obs.Qualifiers)...)
	}
	for _, obs := range ch.RouteObservations {
		gaps = append(gaps, validateServiceQualifierMap(obs.Qualifiers)...)
	}
	return gaps
}

func validateServiceMetricMap(metrics map[string]interface{}) []string {
	if len(metrics) == 0 {
		return nil
	}
	type metricRule struct {
		min float64
		max float64
	}
	rules := map[string]metricRule{
		"availability_slo":            {min: 0, max: 1},
		"vendor_lock_in_score":        {min: 0, max: 1},
		"p95_latency_ms":              {min: 0},
		"failover_time_minutes":       {min: 0},
		"recovery_time_hours":         {min: 0},
		"support_response_time_hours": {min: 0},
	}
	var gaps []string
	for key, rule := range rules {
		value, ok := extractObservationMetricNumber(metrics[key])
		if !ok {
			continue
		}
		if value < rule.min || (rule.max > 0 && value > rule.max) {
			gaps = append(gaps, fmt.Sprintf("service_chain_invariant: metric %s out of allowed range", key))
		}
	}
	return gaps
}

func validateServiceQualifierMap(qualifiers map[string]interface{}) []string {
	if len(qualifiers) == 0 {
		return nil
	}
	boolishKeys := []string{"auth_required", "approved_vendor_only", "service_contract_required", "export_license_required", "dual_use_flag"}
	var gaps []string
	for _, key := range boolishKeys {
		if raw, ok := qualifiers[key]; ok && raw != nil {
			if _, parsed := parseBoolishObservation(raw); !parsed {
				gaps = append(gaps, fmt.Sprintf("service_chain_invariant: qualifier %s must be boolean-like", key))
			}
		}
	}
	if raw, ok := qualifiers["region_lock_in"]; ok && raw != nil {
		switch v := raw.(type) {
		case bool:
		case string:
			if strings.TrimSpace(v) == "" {
				gaps = append(gaps, "service_chain_invariant: qualifier region_lock_in must not be empty")
			}
		default:
			gaps = append(gaps, "service_chain_invariant: qualifier region_lock_in must be boolean-like or a non-empty string")
		}
	}
	return gaps
}

func extractObservationMetricNumber(raw interface{}) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "%")), 64)
		return parsed, err == nil
	case map[string]interface{}:
		if inner, ok := value["value"]; ok {
			return extractObservationMetricNumber(inner)
		}
	}
	return 0, false
}

func parseBoolishObservation(raw interface{}) (bool, bool) {
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "1", "required":
			return true, true
		case "false", "no", "0", "not_required", "not required":
			return false, true
		}
	case int:
		if v == 0 || v == 1 {
			return v == 1, true
		}
	case float64:
		if v == 0 || v == 1 {
			return v == 1, true
		}
	}
	return false, false
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

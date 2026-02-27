package scout

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	ExecutionPlanStateKey = "executionPlan"
)

const (
	TerminalOutcomeCompleted                       = "completed"
	TerminalOutcomePartialCompleted                = "partial_completed"
	TerminalOutcomeFailed                          = "failed"
	TerminalOutcomeBudgetExhausted                 = "budget_exhausted"
	TerminalOutcomeCompletedWithPolicyPersistError = "completed_with_policy_persist_error"
)

// ExecutionPlanContract defines runtime planning limits applied before enqueue.
// It is persisted in job.State["executionPlan"] and propagated to child jobs.
type ExecutionPlanContract struct {
	Version string `json:"version"`
	Mode    string `json:"mode"` // "general" | "supply_chain"

	MaxAnchors        int `json:"maxAnchors"`
	MaxRootJobs       int `json:"maxRootJobs"`
	MaxRecursionDepth int `json:"maxRecursionDepth"`
	MaxChildrenPerJob int `json:"maxChildrenPerJob"`
	MaxCascadeJobs    int `json:"maxCascadeJobs"`

	MaxSupplyChainQueries int      `json:"maxSupplyChainQueries"`
	PriorityTiers         []string `json:"priorityTiers,omitempty"`
	StopConditions        []string `json:"stopConditions,omitempty"`

	BudgetCeilingUSD          float64 `json:"budgetCeilingUSD"`
	BudgetCeilingOutputTokens int     `json:"budgetCeilingOutputTokens"`
	StopOnBudgetExhaustion    bool    `json:"stopOnBudgetExhaustion"`
}

func defaultExecutionPlanContract(domainModes, specialties []string) ExecutionPlanContract {
	modes := taxonomy.NormalizeDomainModes(domainModes)
	specs := taxonomy.NormalizeSpecialties(specialties)
	supplyChainMode := taxonomy.HasDomainMode(modes, taxonomy.DomainModeSupplyChain) ||
		taxonomy.HasSpecialty(specs, taxonomy.SpecialtySupplyChain)

	p := ExecutionPlanContract{
		Version: "v1",
		Mode:    "general",

		MaxAnchors:        24,
		MaxRootJobs:       24,
		MaxRecursionDepth: 2,
		MaxChildrenPerJob: 12,
		MaxCascadeJobs:    180,

		MaxSupplyChainQueries: 0,
		PriorityTiers:         []string{"high", "normal", "low"},
		StopConditions:        []string{"max_cascade_jobs_reached"},

		BudgetCeilingUSD:          0,
		BudgetCeilingOutputTokens: 0,
		StopOnBudgetExhaustion:    false,
	}

	if supplyChainMode {
		p.Mode = "supply_chain"
		p.MaxAnchors = 12
		p.MaxRootJobs = 12
		p.MaxRecursionDepth = 3
		p.MaxChildrenPerJob = 18
		p.MaxCascadeJobs = 120
		p.MaxSupplyChainQueries = 6
		p.StopConditions = []string{"budget_exhausted", "max_cascade_jobs_reached"}
		p.StopOnBudgetExhaustion = true
	}
	return normalizeExecutionPlanContract(p)
}

func normalizeExecutionPlanContract(p ExecutionPlanContract) ExecutionPlanContract {
	p.Version = strings.TrimSpace(p.Version)
	if p.Version == "" {
		p.Version = "v1"
	}
	mode := strings.TrimSpace(strings.ToLower(p.Mode))
	if mode != "supply_chain" {
		mode = "general"
	}
	p.Mode = mode

	clampInt := func(v, def, minV, maxV int) int {
		if v <= 0 {
			v = def
		}
		if v < minV {
			v = minV
		}
		if maxV > 0 && v > maxV {
			v = maxV
		}
		return v
	}
	p.MaxAnchors = clampInt(p.MaxAnchors, 24, 1, 500)
	p.MaxRootJobs = clampInt(p.MaxRootJobs, 24, 1, 500)
	p.MaxRecursionDepth = clampInt(p.MaxRecursionDepth, 2, 0, 8)
	p.MaxChildrenPerJob = clampInt(p.MaxChildrenPerJob, 12, 0, 200)
	p.MaxCascadeJobs = clampInt(p.MaxCascadeJobs, 180, 1, 5000)
	if p.MaxCascadeJobs < p.MaxRootJobs {
		p.MaxCascadeJobs = p.MaxRootJobs
	}
	if p.MaxRootJobs > p.MaxAnchors {
		p.MaxRootJobs = p.MaxAnchors
	}

	if p.Mode == "supply_chain" {
		p.MaxSupplyChainQueries = clampInt(p.MaxSupplyChainQueries, 6, 1, 20)
	} else {
		if p.MaxSupplyChainQueries < 0 {
			p.MaxSupplyChainQueries = 0
		}
		if p.MaxSupplyChainQueries > 20 {
			p.MaxSupplyChainQueries = 20
		}
	}

	if p.BudgetCeilingUSD < 0 {
		p.BudgetCeilingUSD = 0
	}
	if p.BudgetCeilingUSD > 0 {
		p.BudgetCeilingUSD = math.Round(p.BudgetCeilingUSD*100) / 100
	}
	if p.BudgetCeilingOutputTokens < 0 {
		p.BudgetCeilingOutputTokens = 0
	}
	p.PriorityTiers = normalizeStringTokenList(p.PriorityTiers, []string{"high", "normal", "low"}, 8)
	p.StopConditions = normalizeStringTokenList(p.StopConditions, nil, 12)

	return p
}

func normalizeStringTokenList(in []string, fallback []string, maxN int) []string {
	if len(in) == 0 {
		if len(fallback) == 0 {
			return nil
		}
		out := append([]string(nil), fallback...)
		return out
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		token := strings.TrimSpace(strings.ToLower(raw))
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
		if maxN > 0 && len(out) >= maxN {
			break
		}
	}
	if len(out) == 0 && len(fallback) > 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func parseExecutionPlanFromRaw(raw interface{}) (ExecutionPlanContract, bool) {
	if raw == nil {
		return ExecutionPlanContract{}, false
	}
	switch t := raw.(type) {
	case ExecutionPlanContract:
		return normalizeExecutionPlanContract(t), true
	case *ExecutionPlanContract:
		if t == nil {
			return ExecutionPlanContract{}, false
		}
		return normalizeExecutionPlanContract(*t), true
	case map[string]interface{}:
		b, err := json.Marshal(t)
		if err != nil {
			return ExecutionPlanContract{}, false
		}
		var p ExecutionPlanContract
		if err := json.Unmarshal(b, &p); err != nil {
			return ExecutionPlanContract{}, false
		}
		return normalizeExecutionPlanContract(p), true
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return ExecutionPlanContract{}, false
		}
		var p ExecutionPlanContract
		if err := json.Unmarshal(b, &p); err != nil {
			return ExecutionPlanContract{}, false
		}
		return normalizeExecutionPlanContract(p), true
	}
}

func parseStateIntLoose(v interface{}, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float32:
		return int(t)
	case float64:
		return int(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return def
		}
		if i, ok := parseIntStringLoose(t); ok {
			return i
		}
		return def
	default:
		return def
	}
}

func parseIntStringLoose(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = strings.TrimPrefix(s, "-")
	}
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

func parseStateFloatLoose(v interface{}, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return def
		}
		if n, ok := parseIntStringLoose(t); ok {
			return float64(n)
		}
		return def
	default:
		return def
	}
}

func parseStateBoolLoose(v interface{}, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		x := strings.TrimSpace(strings.ToLower(t))
		if x == "" {
			return def
		}
		if x == "1" || x == "true" || x == "yes" {
			return true
		}
		if x == "0" || x == "false" || x == "no" {
			return false
		}
		return def
	default:
		return def
	}
}

func parseStateStringSliceLoose(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		parts := strings.Split(t, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		return nil
	}
}

// BuildExecutionPlanContract derives an execution contract from lens viewConfig + modes,
// then applies explicit overrides from viewConfig (if present).
func BuildExecutionPlanContract(viewConfig map[string]interface{}, domainModes, specialties []string) ExecutionPlanContract {
	p := defaultExecutionPlanContract(domainModes, specialties)
	if viewConfig == nil {
		return p
	}
	if raw, ok := viewConfig[ExecutionPlanStateKey]; ok {
		if fromRaw, ok2 := parseExecutionPlanFromRaw(raw); ok2 {
			p = fromRaw
		}
	}
	if v, ok := viewConfig["maxRootJobs"]; ok {
		p.MaxRootJobs = parseStateIntLoose(v, p.MaxRootJobs)
	}
	if v, ok := viewConfig["maxAnchors"]; ok {
		p.MaxAnchors = parseStateIntLoose(v, p.MaxAnchors)
	}
	if v, ok := viewConfig["maxRecursionDepth"]; ok {
		p.MaxRecursionDepth = parseStateIntLoose(v, p.MaxRecursionDepth)
	}
	if v, ok := viewConfig["maxChildrenPerJob"]; ok {
		p.MaxChildrenPerJob = parseStateIntLoose(v, p.MaxChildrenPerJob)
	}
	if v, ok := viewConfig["maxCascadeJobs"]; ok {
		p.MaxCascadeJobs = parseStateIntLoose(v, p.MaxCascadeJobs)
	}
	if v, ok := viewConfig["maxSupplyChainQueries"]; ok {
		p.MaxSupplyChainQueries = parseStateIntLoose(v, p.MaxSupplyChainQueries)
	}
	if v, ok := viewConfig["priorityTiers"]; ok {
		p.PriorityTiers = parseStateStringSliceLoose(v)
	}
	if v, ok := viewConfig["stopConditions"]; ok {
		p.StopConditions = parseStateStringSliceLoose(v)
	}
	if v, ok := viewConfig["budgetCeilingUSD"]; ok {
		p.BudgetCeilingUSD = parseStateFloatLoose(v, p.BudgetCeilingUSD)
	}
	if v, ok := viewConfig["budgetCeilingOutputTokens"]; ok {
		p.BudgetCeilingOutputTokens = parseStateIntLoose(v, p.BudgetCeilingOutputTokens)
	}
	if v, ok := viewConfig["stopOnBudgetExhaustion"]; ok {
		p.StopOnBudgetExhaustion = parseStateBoolLoose(v, p.StopOnBudgetExhaustion)
	}
	return normalizeExecutionPlanContract(p)
}

// ExecutionPlanContractFromState extracts plan from job.State, with compatibility fallback.
func ExecutionPlanContractFromState(state map[string]interface{}) ExecutionPlanContract {
	p := defaultExecutionPlanContract(nil, nil)
	if state == nil {
		return p
	}
	if raw, ok := state[ExecutionPlanStateKey]; ok {
		if fromRaw, ok2 := parseExecutionPlanFromRaw(raw); ok2 {
			p = fromRaw
		}
	}
	if v, ok := state["maxRootJobs"]; ok {
		p.MaxRootJobs = parseStateIntLoose(v, p.MaxRootJobs)
	}
	if v, ok := state["maxAnchors"]; ok {
		p.MaxAnchors = parseStateIntLoose(v, p.MaxAnchors)
	}
	if v, ok := state["maxRecursionDepth"]; ok {
		p.MaxRecursionDepth = parseStateIntLoose(v, p.MaxRecursionDepth)
	}
	if v, ok := state["maxChildrenPerJob"]; ok {
		p.MaxChildrenPerJob = parseStateIntLoose(v, p.MaxChildrenPerJob)
	}
	if v, ok := state["maxCascadeJobs"]; ok {
		p.MaxCascadeJobs = parseStateIntLoose(v, p.MaxCascadeJobs)
	}
	if v, ok := state["maxSupplyChainQueries"]; ok {
		p.MaxSupplyChainQueries = parseStateIntLoose(v, p.MaxSupplyChainQueries)
	}
	if v, ok := state["priorityTiers"]; ok {
		p.PriorityTiers = parseStateStringSliceLoose(v)
	}
	if v, ok := state["stopConditions"]; ok {
		p.StopConditions = parseStateStringSliceLoose(v)
	}
	if v, ok := state["budgetCeilingUSD"]; ok {
		p.BudgetCeilingUSD = parseStateFloatLoose(v, p.BudgetCeilingUSD)
	}
	if v, ok := state["budgetCeilingOutputTokens"]; ok {
		p.BudgetCeilingOutputTokens = parseStateIntLoose(v, p.BudgetCeilingOutputTokens)
	}
	if v, ok := state["stopOnBudgetExhaustion"]; ok {
		p.StopOnBudgetExhaustion = parseStateBoolLoose(v, p.StopOnBudgetExhaustion)
	}
	return normalizeExecutionPlanContract(p)
}

func (p ExecutionPlanContract) ToStateMap() map[string]interface{} {
	p = normalizeExecutionPlanContract(p)
	return map[string]interface{}{
		"version":                   p.Version,
		"mode":                      p.Mode,
		"maxAnchors":                p.MaxAnchors,
		"maxRootJobs":               p.MaxRootJobs,
		"maxRecursionDepth":         p.MaxRecursionDepth,
		"maxChildrenPerJob":         p.MaxChildrenPerJob,
		"maxCascadeJobs":            p.MaxCascadeJobs,
		"maxSupplyChainQueries":     p.MaxSupplyChainQueries,
		"priorityTiers":             append([]string(nil), p.PriorityTiers...),
		"stopConditions":            append([]string(nil), p.StopConditions...),
		"budgetCeilingUSD":          p.BudgetCeilingUSD,
		"budgetCeilingOutputTokens": p.BudgetCeilingOutputTokens,
		"stopOnBudgetExhaustion":    p.StopOnBudgetExhaustion,
	}
}

// ApplyExecutionPlanToState writes execution plan and compatibility keys into state.
func ApplyExecutionPlanToState(state map[string]interface{}, plan ExecutionPlanContract) {
	if state == nil {
		return
	}
	plan = normalizeExecutionPlanContract(plan)
	state[ExecutionPlanStateKey] = plan.ToStateMap()
	// Compatibility keys consumed by existing runtime paths.
	state["maxAnchors"] = float64(plan.MaxAnchors)
	state["maxRootJobs"] = float64(plan.MaxRootJobs)
	state["maxRecursionDepth"] = float64(plan.MaxRecursionDepth)
	state["maxChildrenPerJob"] = float64(plan.MaxChildrenPerJob)
	state["maxCascadeJobs"] = float64(plan.MaxCascadeJobs)
	state["maxSupplyChainQueries"] = float64(plan.MaxSupplyChainQueries)
	state["priorityTiers"] = append([]string(nil), plan.PriorityTiers...)
	state["stopConditions"] = append([]string(nil), plan.StopConditions...)
	state["budgetCeilingUSD"] = plan.BudgetCeilingUSD
	state["budgetCeilingOutputTokens"] = float64(plan.BudgetCeilingOutputTokens)
	state["stopOnBudgetExhaustion"] = plan.StopOnBudgetExhaustion
}

// HasExecutionPlanContractState reports whether state includes execution-plan fields
// (new nested object or legacy compatibility keys).
func HasExecutionPlanContractState(state map[string]interface{}) bool {
	if state == nil {
		return false
	}
	if _, ok := state[ExecutionPlanStateKey]; ok {
		return true
	}
	for _, k := range []string{
		"maxRootJobs",
		"maxAnchors",
		"maxRecursionDepth",
		"maxChildrenPerJob",
		"maxCascadeJobs",
		"maxSupplyChainQueries",
		"priorityTiers",
		"stopConditions",
		"budgetCeilingUSD",
		"budgetCeilingOutputTokens",
		"stopOnBudgetExhaustion",
	} {
		if _, ok := state[k]; ok {
			return true
		}
	}
	return false
}

// ValidateExecutionPlanContract validates contract semantics beyond normalization.
func ValidateExecutionPlanContract(p ExecutionPlanContract) error {
	p = normalizeExecutionPlanContract(p)
	if p.Version == "" {
		return fmt.Errorf("execution plan: missing version")
	}
	if p.Mode != "general" && p.Mode != "supply_chain" {
		return fmt.Errorf("execution plan: invalid mode %q", p.Mode)
	}
	if p.MaxAnchors <= 0 {
		return fmt.Errorf("execution plan: maxAnchors must be > 0")
	}
	if p.MaxRootJobs <= 0 {
		return fmt.Errorf("execution plan: maxRootJobs must be > 0")
	}
	if p.MaxRootJobs > p.MaxAnchors {
		return fmt.Errorf("execution plan: maxRootJobs(%d) exceeds maxAnchors(%d)", p.MaxRootJobs, p.MaxAnchors)
	}
	if p.MaxCascadeJobs < p.MaxRootJobs {
		return fmt.Errorf("execution plan: maxCascadeJobs(%d) < maxRootJobs(%d)", p.MaxCascadeJobs, p.MaxRootJobs)
	}
	if p.Mode == "supply_chain" && p.MaxSupplyChainQueries <= 0 {
		return fmt.Errorf("execution plan: supply_chain mode requires maxSupplyChainQueries > 0")
	}
	return nil
}

// ValidateDomainExecutionPlanState enforces strict plan contract for domain jobs.
// A domain job without a valid execution plan should not execute.
func ValidateDomainExecutionPlanState(state map[string]interface{}) (ExecutionPlanContract, error) {
	if !HasExecutionPlanContractState(state) {
		return ExecutionPlanContract{}, fmt.Errorf("execution plan missing")
	}
	p := ExecutionPlanContractFromState(state)
	if err := ValidateExecutionPlanContract(p); err != nil {
		return ExecutionPlanContract{}, err
	}
	return p, nil
}

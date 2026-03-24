package simulation

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

//go:embed scenarios/*.json
var scenarioDSLFS embed.FS

type scenarioDSL struct {
	Metadata           scenarioMetadata             `json:"metadata"`
	GraphRequirements  scenarioGraphRequirements    `json:"graphRequirements"`
	Interpretation     scenarioInterpretationSpec   `json:"interpretation"`
	Population         []scenarioPopulationTemplate `json:"population"`
	DirectInteractions []scenarioInteractionRule    `json:"directInteractions"`
}

type scenarioMetadata struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Version        string `json:"version"`
	Description    string `json:"description"`
	ActionOntology string `json:"actionOntology"`
}

type scenarioGraphRequirements struct {
	ScopeAnchors      []string                          `json:"scopeAnchors"`
	RequiredPaths     []string                          `json:"requiredPaths"`
	RequiredEdgeTypes []string                          `json:"requiredEdgeTypes"`
	RequiredBindings  []scenarioGraphBindingRequirement `json:"requiredBindings"`
}

type scenarioGraphBindingRequirement struct {
	AgentClass      string   `json:"agentClass"`
	RequiredOrigins []string `json:"requiredOrigins"`
	MinOriginRefs   int      `json:"minOriginRefs"`
}

type scenarioInterpretationSpec struct {
	Overlay            string   `json:"overlay"`
	ExecutiveAudience  string   `json:"executiveAudience"`
	PriorityDimensions []string `json:"priorityDimensions"`
}

type scenarioPopulationTemplate struct {
	AgentClass           string         `json:"agentClass"`
	DisplayNamePattern   string         `json:"displayNamePattern"`
	Count                int            `json:"count"`
	PopulationWeightBase float64        `json:"populationWeightBase"`
	PopulationWeightStep float64        `json:"populationWeightStep"`
	OriginRefs           []string       `json:"originRefs"`
	AllowedActions       []string       `json:"allowedActions"`
	Traits               map[string]any `json:"traits"`
	TraitsStep           map[string]any `json:"traitsStep"`
	BeliefState          map[string]any `json:"beliefState"`
	BeliefStateStep      map[string]any `json:"beliefStateStep"`
	ResourceState        map[string]any `json:"resourceState"`
	ResourceStateStep    map[string]any `json:"resourceStateStep"`
	MessageStyle         string         `json:"messageStyle"`
	PolicyHints          map[string]any `json:"policyHints"`
}

type scenarioInteractionRule struct {
	ID              string   `json:"id"`
	Axes            []string `json:"axes"`
	SourceClasses   []string `json:"sourceClasses"`
	ActionTypes     []string `json:"actionTypes"`
	MinStep         int      `json:"minStep"`
	MaxStep         int      `json:"maxStep"`
	VariantMin      int      `json:"variantMin"`
	VariantMax      int      `json:"variantMax"`
	TargetSelectors []string `json:"targetSelectors"`
	Limit           int      `json:"limit"`
	Effects         struct {
		StressDelta    float64 `json:"stressDelta"`
		TrustDelta     float64 `json:"trustDelta"`
		BudgetDelta    float64 `json:"budgetDelta"`
		AttentionDelta float64 `json:"attentionDelta"`
	} `json:"effects"`
}

type scenarioInteractionEffect struct {
	RuleID         string
	SourceActorID  string
	SourceClass    string
	TargetAgentIDs []string
	TargetClasses  []string
	StressDelta    float64
	TrustDelta     float64
	BudgetDelta    float64
	AttentionDelta float64
}

var (
	kzvatDSLOnce sync.Once
	kzvatDSL     *scenarioDSL
	kzvatDSLErr  error

	teslaBatteryDSLOnce sync.Once
	teslaBatteryDSL     *scenarioDSL
	teslaBatteryDSLErr  error

	spaceSatelliteDSLOnce sync.Once
	spaceSatelliteDSL     *scenarioDSL
	spaceSatelliteDSLErr  error

	slbRussiaDSLOnce sync.Once
	slbRussiaDSL     *scenarioDSL
	slbRussiaDSLErr  error
)

func loadScenarioDSL(name string) (*scenarioDSL, error) {
	switch strings.TrimSpace(name) {
	case "kzvat":
		kzvatDSLOnce.Do(func() {
			payload, err := scenarioDSLFS.ReadFile("scenarios/kzvat.json")
			if err != nil {
				kzvatDSLErr = err
				return
			}
			var dsl scenarioDSL
			if err := json.Unmarshal(payload, &dsl); err != nil {
				kzvatDSLErr = err
				return
			}
			if err := validateScenarioDSL(&dsl); err != nil {
				kzvatDSLErr = err
				return
			}
			kzvatDSL = &dsl
		})
		return kzvatDSL, kzvatDSLErr
	case "tesla_battery_logistics":
		teslaBatteryDSLOnce.Do(func() {
			payload, err := scenarioDSLFS.ReadFile("scenarios/tesla_battery_logistics.json")
			if err != nil {
				teslaBatteryDSLErr = err
				return
			}
			var dsl scenarioDSL
			if err := json.Unmarshal(payload, &dsl); err != nil {
				teslaBatteryDSLErr = err
				return
			}
			if err := validateScenarioDSL(&dsl); err != nil {
				teslaBatteryDSLErr = err
				return
			}
			teslaBatteryDSL = &dsl
		})
		return teslaBatteryDSL, teslaBatteryDSLErr
	case "space_satellite_sanctions":
		spaceSatelliteDSLOnce.Do(func() {
			payload, err := scenarioDSLFS.ReadFile("scenarios/space_satellite_sanctions.json")
			if err != nil {
				spaceSatelliteDSLErr = err
				return
			}
			var dsl scenarioDSL
			if err := json.Unmarshal(payload, &dsl); err != nil {
				spaceSatelliteDSLErr = err
				return
			}
			if err := validateScenarioDSL(&dsl); err != nil {
				spaceSatelliteDSLErr = err
				return
			}
			spaceSatelliteDSL = &dsl
		})
		return spaceSatelliteDSL, spaceSatelliteDSLErr
	case "slb_russia_sanctions":
		slbRussiaDSLOnce.Do(func() {
			payload, err := scenarioDSLFS.ReadFile("scenarios/slb_russia_sanctions.json")
			if err != nil {
				slbRussiaDSLErr = err
				return
			}
			var dsl scenarioDSL
			if err := json.Unmarshal(payload, &dsl); err != nil {
				slbRussiaDSLErr = err
				return
			}
			if err := validateScenarioDSL(&dsl); err != nil {
				slbRussiaDSLErr = err
				return
			}
			slbRussiaDSL = &dsl
		})
		return slbRussiaDSL, slbRussiaDSLErr
	default:
		return nil, fmt.Errorf("unknown scenario DSL %q", name)
	}
}

func validateScenarioDSL(dsl *scenarioDSL) error {
	if dsl == nil {
		return fmt.Errorf("scenario DSL is nil")
	}
	if strings.TrimSpace(dsl.Metadata.ID) == "" {
		return fmt.Errorf("scenario DSL metadata.id is required")
	}
	if strings.TrimSpace(dsl.Metadata.Title) == "" {
		return fmt.Errorf("scenario DSL metadata.title is required")
	}
	if strings.TrimSpace(dsl.Metadata.Version) == "" {
		return fmt.Errorf("scenario DSL metadata.version is required")
	}
	if strings.TrimSpace(dsl.Metadata.ActionOntology) == "" {
		dsl.Metadata.ActionOntology = "sim.action.v1"
	}
	dsl.GraphRequirements.ScopeAnchors = compactStrings(dsl.GraphRequirements.ScopeAnchors)
	dsl.GraphRequirements.RequiredPaths = compactStrings(dsl.GraphRequirements.RequiredPaths)
	dsl.GraphRequirements.RequiredEdgeTypes = compactStrings(dsl.GraphRequirements.RequiredEdgeTypes)
	dsl.Interpretation.PriorityDimensions = compactStrings(dsl.Interpretation.PriorityDimensions)
	if len(dsl.Population) == 0 {
		return fmt.Errorf("scenario DSL has empty population")
	}
	if len(dsl.GraphRequirements.RequiredPaths) == 0 {
		return fmt.Errorf("scenario DSL graphRequirements.requiredPaths must not be empty")
	}
	for idx := range dsl.Population {
		template := &dsl.Population[idx]
		template.AgentClass = strings.TrimSpace(template.AgentClass)
		template.DisplayNamePattern = strings.TrimSpace(template.DisplayNamePattern)
		template.OriginRefs = compactStrings(template.OriginRefs)
		template.AllowedActions = normalizeAllowedActions(template.AllowedActions)
		if template.AgentClass == "" {
			return fmt.Errorf("scenario DSL population[%d].agentClass is required", idx)
		}
		if template.Count <= 0 {
			return fmt.Errorf("scenario DSL population[%d].count must be positive", idx)
		}
		if template.DisplayNamePattern == "" {
			return fmt.Errorf("scenario DSL population[%d].displayNamePattern is required", idx)
		}
		if len(template.OriginRefs) == 0 {
			return fmt.Errorf("scenario DSL population[%d].originRefs must not be empty", idx)
		}
		if len(template.AllowedActions) == 0 {
			return fmt.Errorf("scenario DSL population[%d].allowedActions must not be empty", idx)
		}
	}
	for idx := range dsl.DirectInteractions {
		rule := &dsl.DirectInteractions[idx]
		rule.ID = strings.TrimSpace(rule.ID)
		rule.Axes = compactStrings(rule.Axes)
		rule.SourceClasses = compactStrings(rule.SourceClasses)
		rule.ActionTypes = normalizeAllowedActions(rule.ActionTypes)
		rule.TargetSelectors = compactStrings(rule.TargetSelectors)
		if rule.ID == "" {
			return fmt.Errorf("scenario DSL directInteractions[%d].id is required", idx)
		}
	}
	for idx := range dsl.GraphRequirements.RequiredBindings {
		binding := &dsl.GraphRequirements.RequiredBindings[idx]
		binding.AgentClass = strings.TrimSpace(binding.AgentClass)
		binding.RequiredOrigins = compactStrings(binding.RequiredOrigins)
		if binding.AgentClass == "" {
			return fmt.Errorf("scenario DSL graphRequirements.requiredBindings[%d].agentClass is required", idx)
		}
		if binding.MinOriginRefs <= 0 {
			binding.MinOriginRefs = 1
		}
	}
	return nil
}

func scenarioDSLDescriptor(run *horizonmodels.SimulationRun) (name, generatedBy, justification string, ok bool) {
	switch {
	case isKZVATHotPath(run):
		return "kzvat", "scenario.kzvat.population.dsl", "Kazakhstan VAT 16% hot-path archetype from scenario DSL", true
	case isTeslaBatteryHotPath(run):
		return "tesla_battery_logistics", "scenario.tesla_battery_logistics.population.dsl", "Tesla battery logistics trade-shock archetype from scenario DSL", true
	case isSpaceSatelliteHotPath(run):
		return "space_satellite_sanctions", "scenario.space_satellite_sanctions.population.dsl", "Space and satellite sanctions/compliance archetype from scenario DSL", true
	case isSLBRussiaHotPath(run):
		return "slb_russia_sanctions", "scenario.slb_russia_sanctions.population.dsl", "SLB Russia sanctions and transshipment archetype from scenario DSL", true
	default:
		return "", "", "", false
	}
}

func scenarioDSLByRun(run *horizonmodels.SimulationRun) (*scenarioDSL, string, bool) {
	name, _, _, ok := scenarioDSLDescriptor(run)
	if !ok {
		return nil, "", false
	}
	dsl, err := loadScenarioDSL(name)
	if err != nil || dsl == nil {
		return nil, "", false
	}
	return dsl, name, true
}

func scenarioDSLPopulation(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) ([]map[string]any, bool) {
	name, generatedBy, justification, ok := scenarioDSLDescriptor(run)
	if !ok {
		return nil, false
	}
	dsl, err := loadScenarioDSL(name)
	if err != nil || dsl == nil {
		return nil, false
	}
	out := make([]map[string]any, 0, 96)
	variant := worldVariant(world)
	for _, template := range dsl.Population {
		count := max(template.Count, 1)
		for idx := 0; idx < count; idx++ {
			traits := instantiateScenarioMap(template.Traits, template.TraitsStep, idx)
			traits["variant"] = variant
			beliefState := instantiateScenarioMap(template.BeliefState, template.BeliefStateStep, idx)
			resourceState := instantiateScenarioMap(template.ResourceState, template.ResourceStateStep, idx)
			out = append(out, map[string]any{
				"agentClass":       strings.TrimSpace(template.AgentClass),
				"displayName":      scenarioDisplayName(template.DisplayNamePattern, idx, count),
				"originKind":       "scenario_archetype",
				"originRefs":       compactStrings(template.OriginRefs),
				"generatedBy":      generatedBy,
				"justification":    justification,
				"populationWeight": round3(template.PopulationWeightBase + template.PopulationWeightStep*float64(idx)),
				"traits":           traits,
				"beliefState":      beliefState,
				"resourceState":    resourceState,
				"allowedActions":   normalizeAllowedActions(template.AllowedActions),
				"messageStyle":     strings.TrimSpace(template.MessageStyle),
				"policyHints":      cloneMap(template.PolicyHints),
			})
		}
	}
	return out, len(out) > 0
}

func scenarioDirectInteractions(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, actor *horizonmodels.SimulationAgentSpec, action horizonmodels.SimulationAction, step int) ([]scenarioInteractionEffect, bool) {
	name, _, _, ok := scenarioDSLDescriptor(run)
	if !ok || actor == nil {
		return nil, false
	}
	dsl, err := loadScenarioDSL(name)
	if err != nil || dsl == nil || len(dsl.DirectInteractions) == 0 {
		return nil, false
	}
	axis := runAxis(run)
	variant := worldVariant(world)
	out := make([]scenarioInteractionEffect, 0, 4)
	for _, rule := range dsl.DirectInteractions {
		if !scenarioInteractionRuleMatches(rule, axis, strings.TrimSpace(actor.AgentClass), strings.TrimSpace(action.Type), step, variant) {
			continue
		}
		targetAgents := resolveScenarioTargets(rule.TargetSelectors, agents, actor)
		if len(targetAgents) == 0 {
			continue
		}
		if rule.Limit > 0 && len(targetAgents) > rule.Limit {
			targetAgents = targetAgents[:rule.Limit]
		}
		effect := scenarioInteractionEffect{
			RuleID:         strings.TrimSpace(rule.ID),
			SourceActorID:  strings.TrimSpace(actor.ID),
			SourceClass:    strings.TrimSpace(actor.AgentClass),
			TargetAgentIDs: make([]string, 0, len(targetAgents)),
			TargetClasses:  make([]string, 0, len(targetAgents)),
			StressDelta:    rule.Effects.StressDelta,
			TrustDelta:     rule.Effects.TrustDelta,
			BudgetDelta:    rule.Effects.BudgetDelta,
			AttentionDelta: rule.Effects.AttentionDelta,
		}
		seenClasses := map[string]struct{}{}
		for _, target := range targetAgents {
			effect.TargetAgentIDs = append(effect.TargetAgentIDs, target.ID)
			if cls := strings.TrimSpace(target.AgentClass); cls != "" {
				if _, ok := seenClasses[cls]; !ok {
					seenClasses[cls] = struct{}{}
					effect.TargetClasses = append(effect.TargetClasses, cls)
				}
			}
		}
		sort.Strings(effect.TargetAgentIDs)
		sort.Strings(effect.TargetClasses)
		out = append(out, effect)
	}
	return out, len(out) > 0
}

func applyScenarioInteractionEffects(agents []*horizonmodels.SimulationAgentSpec, effects []scenarioInteractionEffect) {
	if len(effects) == 0 {
		return
	}
	byID := map[string]*horizonmodels.SimulationAgentSpec{}
	for _, agent := range agents {
		if agent != nil && strings.TrimSpace(agent.ID) != "" {
			byID[strings.TrimSpace(agent.ID)] = agent
		}
	}
	for _, effect := range effects {
		for _, targetID := range effect.TargetAgentIDs {
			target := byID[strings.TrimSpace(targetID)]
			if target == nil {
				continue
			}
			if target.BeliefState == nil {
				target.BeliefState = map[string]float64{}
			}
			if target.ResourceState == nil {
				target.ResourceState = map[string]float64{}
			}
			if target.Traits == nil {
				target.Traits = map[string]any{}
			}
			target.BeliefState["stress"] = round3(clamp01(mapFloat(target.BeliefState, "stress", 0.4) + effect.StressDelta))
			target.BeliefState["trust"] = round3(clamp01(mapFloat(target.BeliefState, "trust", 0.5) + effect.TrustDelta))
			target.ResourceState["budget"] = round3(clamp01(mapFloat(target.ResourceState, "budget", 0.5) + effect.BudgetDelta))
			target.Traits["attention"] = round3(clamp01(mapAnyFloat(target.Traits, "attention", 0.5) + effect.AttentionDelta))
		}
	}
}

func summarizeInteractionEffects(effects []scenarioInteractionEffect) []map[string]any {
	if len(effects) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(effects))
	for _, effect := range effects {
		out = append(out, map[string]any{
			"ruleId":         effect.RuleID,
			"sourceActorId":  effect.SourceActorID,
			"sourceClass":    effect.SourceClass,
			"targetAgentIds": compactStrings(effect.TargetAgentIDs),
			"targetClasses":  compactStrings(effect.TargetClasses),
			"stressDelta":    round3(effect.StressDelta),
			"trustDelta":     round3(effect.TrustDelta),
			"budgetDelta":    round3(effect.BudgetDelta),
			"attentionDelta": round3(effect.AttentionDelta),
		})
	}
	return out
}

func scenarioInteractionRuleMatches(rule scenarioInteractionRule, axis, sourceClass, actionType string, step, variant int) bool {
	if len(rule.Axes) > 0 && !containsFold(rule.Axes, axis) {
		return false
	}
	if len(rule.SourceClasses) > 0 && !containsFold(rule.SourceClasses, sourceClass) {
		return false
	}
	if len(rule.ActionTypes) > 0 && !containsFold(rule.ActionTypes, actionType) {
		return false
	}
	if step < rule.MinStep {
		return false
	}
	if rule.MaxStep > 0 && step > rule.MaxStep {
		return false
	}
	if variant < rule.VariantMin {
		return false
	}
	if rule.VariantMax > 0 && variant > rule.VariantMax {
		return false
	}
	return true
}

func resolveScenarioTargets(selectors []string, agents []*horizonmodels.SimulationAgentSpec, actor *horizonmodels.SimulationAgentSpec) []*horizonmodels.SimulationAgentSpec {
	if len(selectors) == 0 || len(agents) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]*horizonmodels.SimulationAgentSpec, 0, len(selectors))
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		prefix, value, hasPrefix := strings.Cut(selector, ":")
		for _, candidate := range agents {
			if candidate == nil || candidate == actor || strings.TrimSpace(candidate.ID) == "" {
				continue
			}
			if _, ok := seen[candidate.ID]; ok {
				continue
			}
			match := false
			if !hasPrefix {
				match = candidateMatchesOrigin(candidate, selector)
			} else {
				switch strings.TrimSpace(prefix) {
				case "class":
					match = strings.EqualFold(strings.TrimSpace(candidate.AgentClass), strings.TrimSpace(value))
				case "domain":
					match = strings.EqualFold(agentDomain(candidate), strings.TrimSpace(value))
				case "origin":
					match = candidateMatchesOrigin(candidate, value)
				case "agent":
					match = strings.EqualFold(strings.TrimSpace(candidate.ID), strings.TrimSpace(value))
				}
			}
			if match {
				seen[candidate.ID] = struct{}{}
				out = append(out, candidate)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgentClass != out[j].AgentClass {
			return out[i].AgentClass < out[j].AgentClass
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func candidateMatchesOrigin(agent *horizonmodels.SimulationAgentSpec, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" || agent == nil {
		return false
	}
	for _, ref := range agent.OriginRefs {
		if strings.EqualFold(strings.TrimSpace(ref), want) {
			return true
		}
	}
	return false
}

func instantiateScenarioMap(base, stepMap map[string]any, idx int) map[string]any {
	if len(base) == 0 && len(stepMap) == 0 {
		return nil
	}
	out := map[string]any{}
	for key, raw := range base {
		switch value := raw.(type) {
		case float64:
			out[key] = value
		case string:
			out[key] = value
		case bool:
			out[key] = value
		default:
			out[key] = value
		}
	}
	for key, raw := range stepMap {
		stepValue, ok := raw.(float64)
		if !ok {
			continue
		}
		baseValue, ok := out[key].(float64)
		if !ok {
			baseValue = 0
		}
		out[key] = round3(baseValue + stepValue*float64(idx))
	}
	return out
}

func scenarioDisplayName(pattern string, idx, count int) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return fmt.Sprintf("Scenario agent %d", idx+1)
	}
	if strings.Contains(pattern, "%d") {
		return fmt.Sprintf(pattern, idx+1)
	}
	if count > 1 {
		return fmt.Sprintf("%s %d", pattern, idx+1)
	}
	return pattern
}

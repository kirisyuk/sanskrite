package simulation

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func isSpaceSatelliteHotPath(run *horizonmodels.SimulationRun) bool {
	if run == nil {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(run.Request.Mode))
	question := strings.ToLower(strings.TrimSpace(run.Request.Question))
	if strings.Contains(mode, "satellite") || strings.Contains(mode, "space") {
		return true
	}
	if (strings.Contains(question, "satellite") || strings.Contains(question, "space") || strings.Contains(question, "launch")) &&
		(strings.Contains(question, "sanction") || strings.Contains(question, "export control") || strings.Contains(question, "compliance") || strings.Contains(question, "secondary sanction")) {
		return true
	}
	if runHasAnyScenarioPath(run,
		"/domain/v1/supply-chain/geo/brics-gcc/sector/space",
		"/market/brics/aerospace/satellite-services",
	) && runHasAnyScenarioPath(run,
		"/gov/cn/cnsa",
		"/org/ru/roscosmos",
		"/gov/in/isro",
		"/fac/in/satish-dhawan-space-centre",
	) {
		return true
	}
	return false
}

func spaceSatelliteFallbackPopulation(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) ([]map[string]any, bool) {
	if !isSpaceSatelliteHotPath(run) {
		return nil, false
	}
	variant := worldVariant(world)
	out := make([]map[string]any, 0, 24)
	add := func(agentClass, displayName string, weight float64, originRefs, allowedActions []string, attention, riskBias, trust, stress, budget float64, messageStyle string, policyHints map[string]any) {
		out = append(out, map[string]any{
			"agentClass":       agentClass,
			"displayName":      displayName,
			"originKind":       "scenario_archetype",
			"originRefs":       compactStrings(originRefs),
			"generatedBy":      "scenario.space_satellite_sanctions.population",
			"justification":    "Space and satellite sanctions/compliance fallback archetype",
			"populationWeight": weight,
			"traits": map[string]any{
				"attention": attention,
				"riskBias":  riskBias,
				"variant":   variant,
			},
			"beliefState": map[string]any{
				"trust":  trust,
				"stress": stress,
			},
			"resourceState": map[string]any{
				"budget": budget,
			},
			"allowedActions": allowedActions,
			"messageStyle":   messageStyle,
			"policyHints":    policyHints,
		})
	}

	add("export_control_officer", "Export controls desk", 1.42, []string{"/gov/cn/cnsa", "/domain/v1/supply-chain/geo/brics-gcc/sector/space"}, []string{
		string(horizonmodels.SimulationActionEnforceRule),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.92, 0.72, 0.58, 0.42, 0.74, "institutional", map[string]any{"role": "export-controls"})
	add("sanctions_compliance_officer", "Sanctions compliance desk", 1.36, []string{"/market/brics/aerospace/satellite-services", "/gov/cn/cnsa"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.88, 0.68, 0.56, 0.46, 0.68, "compliance", map[string]any{"role": "sanctions-screening"})
	add("launch_regulator", "Launch regulator desk", 1.34, []string{"/gov/in/isro", "/gov/ae/uae-space-agency"}, []string{
		string(horizonmodels.SimulationActionEnforceRule),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.84, 0.64, 0.60, 0.40, 0.70, "regulatory", map[string]any{"role": "launch-licensing"})
	add("launch_vehicle_manufacturer", "Launch vehicle program desk", 1.30, []string{"/fac/cn/beijing-rocket-manufacturing-plant", "/product/finished/mechanical/assembly/launch-vehicles"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionAllocateCapacity),
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.82, 0.62, 0.44, 0.50, 0.56, "industrial", map[string]any{"role": "launch-vehicle-oem"})
	add("rocket_engine_supplier", "Rocket engine supply desk", 1.24, []string{"/fac/cn/beijing-liquid-rocket-engine-facility", "/fac/ru/khimki-plant", "/product/finished/mechanical/assembly/rocket-engines"}, []string{
		string(horizonmodels.SimulationActionReserveInventory),
		string(horizonmodels.SimulationActionAllocateCapacity),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.78, 0.60, 0.42, 0.48, 0.54, "supply", map[string]any{"role": "engines"})
	add("satellite_manufacturer", "Satellite manufacturing desk", 1.28, []string{"/product/finished/electronic/assembly/small-satellites", "/fac/ru/polyot-omsk"}, []string{
		string(horizonmodels.SimulationActionReserveInventory),
		string(horizonmodels.SimulationActionReprioritizeFlow),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.80, 0.58, 0.46, 0.46, 0.58, "manufacturing", map[string]any{"role": "satellite-oem"})
	add("launch_manifest_planner", "Launch manifest planning desk", 1.38, []string{"/fac/cn/wenchang-port", "/fac/in/satish-dhawan-space-centre", "/market/brics/aerospace/satellite-services"}, []string{
		string(horizonmodels.SimulationActionReprioritizeFlow),
		string(horizonmodels.SimulationActionAllocateCapacity),
		string(horizonmodels.SimulationActionRerouteShipment),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.88, 0.66, 0.40, 0.50, 0.62, "planning", map[string]any{"role": "manifest"})
	add("cross_border_logistics_operator", "Cross-border aerospace logistics desk", 1.20, []string{"/market/brics/aerospace/satellite-services", "/fac/cn/wenchang-port"}, []string{
		string(horizonmodels.SimulationActionRerouteShipment),
		string(horizonmodels.SimulationActionExpediteShipment),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.76, 0.58, 0.42, 0.46, 0.52, "logistics", map[string]any{"role": "cross-border"})
	add("aerospace_settlement_bank", "Aerospace settlement bank", 1.18, []string{"/market/brics/aerospace/satellite-services"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionHedgeCost),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.74, 0.60, 0.46, 0.44, 0.70, "finance", map[string]any{"role": "settlement"})
	add("space_insurer", "Space insurance desk", 1.08, []string{"/market/brics/aerospace/satellite-services"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionAdjustGuidance),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.72, 0.58, 0.50, 0.40, 0.68, "risk", map[string]any{"role": "insurance"})
	add("procurement_compliance_lead", "Procurement compliance lead", 1.34, []string{"/market/brics/aerospace/satellite-services", "/gov/sa/saudi-space-commission"}, []string{
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionReprioritizeFlow),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.90, 0.68, 0.42, 0.52, 0.60, "compliance-ops", map[string]any{"role": "counterparty-screening"})
	add("satellite_services_customer", "Satellite-services customer desk", 1.10, []string{"/market/brics/aerospace/satellite-services", "/gov/ae/uae-space-agency"}, []string{
		string(horizonmodels.SimulationActionDelayPurchase),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.70, 0.52, 0.48, 0.42, 0.56, "customer", map[string]any{"role": "demand"})
	add("roscosmos_program_office", "Roscosmos program office", 1.26, []string{"/org/ru/roscosmos", "/market/brics/aerospace/satellite-services"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionReprioritizeFlow),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.82, 0.64, 0.44, 0.48, 0.62, "program", map[string]any{"role": "program-office"})
	add("isro_program_office", "ISRO program office", 1.24, []string{"/gov/in/isro", "/fac/in/satish-dhawan-space-centre"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionAllocateCapacity),
		string(horizonmodels.SimulationActionReprioritizeFlow),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.84, 0.60, 0.50, 0.42, 0.62, "program", map[string]any{"role": "program-office"})
	add("uae_space_procurement", "UAE space procurement", 1.12, []string{"/gov/ae/uae-space-agency", "/market/brics/aerospace/satellite-services"}, []string{
		string(horizonmodels.SimulationActionDelayPurchase),
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.74, 0.52, 0.52, 0.40, 0.58, "public-procurement", map[string]any{"role": "customer-procurement"})
	add("saudi_space_procurement", "Saudi space procurement", 1.12, []string{"/gov/sa/saudi-space-commission", "/market/brics/aerospace/satellite-services"}, []string{
		string(horizonmodels.SimulationActionDelayPurchase),
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.74, 0.52, 0.52, 0.40, 0.58, "public-procurement", map[string]any{"role": "customer-procurement"})
	add("market_intelligence_analyst", "Space market intelligence desk", 1.04, []string{"/market/brics/aerospace/satellite-services"}, []string{
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionAdjustGuidance),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.88, 0.56, 0.54, 0.32, 0.62, "analysis", map[string]any{"role": "market-intel"})
	add("pr_agent", "Space PR desk", 1.02, []string{"/market/brics/aerospace/satellite-services", "/org/ru/roscosmos"}, []string{
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionAdjustGuidance),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.90, 0.54, 0.56, 0.30, 0.64, "communications", map[string]any{"role": "external-comms"})
	return out, true
}

func spaceSatelliteFallbackActions(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) ([]horizonmodels.SimulationAction, bool) {
	if !isSpaceSatelliteHotPath(run) {
		return nil, false
	}
	axis := runAxis(run)
	variant := worldVariant(world)
	out := make([]horizonmodels.SimulationAction, 0, min(len(agents), 42))
	for idx, agent := range agents {
		if !spaceSatelliteAgentParticipates(agent, axis, variant, step, idx) {
			continue
		}
		actionType, delta, targets := spaceSatelliteActionPlan(agent, axis, variant, step, idx)
		if actionType == "" {
			continue
		}
		action := horizonmodels.SimulationAction{
			Type:        actionType,
			ActorID:     agent.ID,
			TargetRefs:  compactStrings(targets),
			GeneratedBy: "scenario.space_satellite_sanctions.fallback",
			Payload: map[string]any{
				"delta": round3(delta),
			},
		}
		if actionType == string(horizonmodels.SimulationActionDoNothing) {
			action.Payload = nil
		}
		if actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionBroadcastMessage) || actionType == string(horizonmodels.SimulationActionRequestExemption) {
			action.Justification = "scenario-aware response to sanctions, export controls, and launch compliance friction"
		}
		out = append(out, action)
		if len(out) >= 42 {
			break
		}
	}
	return out, true
}

func spaceSatelliteBootstrapAdjustments(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) (float64, float64, float64, float64, float64, bool) {
	if !isSpaceSatelliteHotPath(run) {
		return 0, 0, 0, 0, 0, false
	}
	axis := runAxis(run)
	variant := worldVariant(world)
	risk, price, narrative, policy, supply := 0.0, 0.0, 0.0, 0.0, 0.0
	switch axis {
	case horizonmodels.AxisLegalCompliance:
		risk += 0.08
		policy += 0.12
		supply += 0.06
	case horizonmodels.AxisFinancialSettlement:
		risk += 0.06
		price += 0.10
		supply += 0.07
	case horizonmodels.AxisPhysicalFlowDemand:
		risk += 0.06
		price += 0.08
		supply += 0.14
	case horizonmodels.AxisInfoUncertainty:
		risk += 0.08
		narrative += 0.14
		policy += 0.10
	}
	switch variant {
	case 1:
		risk += 0.04
		policy += 0.03
	case 2:
		price += 0.05
		supply += 0.06
	case 3:
		narrative += 0.05
		policy += 0.04
	case 4:
		risk += 0.05
		price += 0.04
	case 5:
		supply += 0.08
		price += 0.03
	case 6:
		risk += 0.06
		narrative += 0.06
	}
	return risk, price, narrative, policy, supply, true
}

func spaceSatelliteAgentParticipates(agent *horizonmodels.SimulationAgentSpec, axis string, variant, step, idx int) bool {
	if agent == nil {
		return false
	}
	cls := strings.TrimSpace(agent.AgentClass)
	switch axis {
	case horizonmodels.AxisLegalCompliance:
		switch cls {
		case "export_control_officer", "sanctions_compliance_officer", "launch_regulator", "procurement_compliance_lead", "launch_vehicle_manufacturer", "satellite_manufacturer", "aerospace_settlement_bank", "pr_agent":
			return true
		default:
			return step >= 2 && (idx+variant)%3 == 0
		}
	case horizonmodels.AxisFinancialSettlement:
		switch cls {
		case "aerospace_settlement_bank", "space_insurer", "procurement_compliance_lead", "launch_vehicle_manufacturer", "rocket_engine_supplier", "satellite_manufacturer", "cross_border_logistics_operator", "satellite_services_customer", "pr_agent":
			return true
		default:
			return step >= 2 && (idx+variant)%4 == 0
		}
	case horizonmodels.AxisPhysicalFlowDemand:
		switch cls {
		case "launch_manifest_planner", "launch_vehicle_manufacturer", "rocket_engine_supplier", "satellite_manufacturer", "cross_border_logistics_operator", "roscosmos_program_office", "isro_program_office", "satellite_services_customer", "uae_space_procurement", "saudi_space_procurement":
			return true
		default:
			return step >= 1 && (idx+variant)%4 == 0
		}
	case horizonmodels.AxisInfoUncertainty:
		switch cls {
		case "market_intelligence_analyst", "pr_agent", "export_control_officer", "sanctions_compliance_officer", "launch_regulator", "satellite_services_customer", "aerospace_settlement_bank", "procurement_compliance_lead":
			return true
		default:
			return step >= 2 && (idx+variant)%4 == 0
		}
	default:
		return true
	}
}

func spaceSatelliteActionPlan(agent *horizonmodels.SimulationAgentSpec, axis string, variant, step, idx int) (string, float64, []string) {
	if agent == nil {
		return "", 0, nil
	}
	cls := strings.TrimSpace(agent.AgentClass)
	switch axis {
	case horizonmodels.AxisLegalCompliance:
		switch cls {
		case "export_control_officer", "launch_regulator":
			if step >= 3 && variant%2 == 0 {
				return string(horizonmodels.SimulationActionIssueStatement), 0.08, nil
			}
			return string(horizonmodels.SimulationActionEnforceRule), 0.09, nil
		case "sanctions_compliance_officer", "procurement_compliance_lead":
			if step >= 2 {
				return string(horizonmodels.SimulationActionRequestExemption), 0.08, []string{"/market/brics/aerospace/satellite-services"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.06, nil
		case "launch_vehicle_manufacturer", "satellite_manufacturer":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.07, nil
		case "aerospace_settlement_bank":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06, nil
		case "pr_agent":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/market/brics/aerospace/satellite-services"}
		default:
			return string(horizonmodels.SimulationActionDoNothing), 0, nil
		}
	case horizonmodels.AxisFinancialSettlement:
		switch cls {
		case "aerospace_settlement_bank":
			if step >= 2 {
				return string(horizonmodels.SimulationActionHedgeCost), 0.08, nil
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.07, nil
		case "space_insurer":
			return string(horizonmodels.SimulationActionAdjustGuidance), 0.07, nil
		case "launch_vehicle_manufacturer", "rocket_engine_supplier", "satellite_manufacturer", "cross_border_logistics_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06 + 0.01*float64((idx+step)%3), nil
		case "procurement_compliance_lead":
			return string(horizonmodels.SimulationActionRequestExemption), 0.07, []string{"/market/brics/aerospace/satellite-services"}
		case "satellite_services_customer", "uae_space_procurement", "saudi_space_procurement":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.05, nil
		case "pr_agent":
			return string(horizonmodels.SimulationActionAdjustGuidance), 0.06, nil
		default:
			return string(horizonmodels.SimulationActionDoNothing), 0, nil
		}
	case horizonmodels.AxisPhysicalFlowDemand:
		switch cls {
		case "launch_manifest_planner":
			if step%2 == 0 {
				return string(horizonmodels.SimulationActionReprioritizeFlow), 0.08, []string{"/market/brics/aerospace/satellite-services"}
			}
			return string(horizonmodels.SimulationActionAllocateCapacity), 0.07, []string{"/product/finished/mechanical/assembly/launch-vehicles"}
		case "launch_vehicle_manufacturer", "rocket_engine_supplier", "satellite_manufacturer":
			if step >= 2 && variant%2 == 1 {
				return string(horizonmodels.SimulationActionReserveInventory), 0.08, nil
			}
			return string(horizonmodels.SimulationActionAllocateCapacity), 0.07, nil
		case "cross_border_logistics_operator":
			if step >= 2 {
				return string(horizonmodels.SimulationActionRerouteShipment), 0.08, nil
			}
			return string(horizonmodels.SimulationActionExpediteShipment), 0.07, nil
		case "roscosmos_program_office", "isro_program_office":
			return string(horizonmodels.SimulationActionReprioritizeFlow), 0.07, nil
		case "satellite_services_customer", "uae_space_procurement", "saudi_space_procurement":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.05, nil
		default:
			return string(horizonmodels.SimulationActionDoNothing), 0, nil
		}
	case horizonmodels.AxisInfoUncertainty:
		switch cls {
		case "market_intelligence_analyst":
			return string(horizonmodels.SimulationActionAdjustGuidance), 0.08, nil
		case "pr_agent":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.08, []string{"/market/brics/aerospace/satellite-services"}
		case "export_control_officer", "sanctions_compliance_officer", "launch_regulator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.07, nil
		case "satellite_services_customer":
			return string(horizonmodels.SimulationActionChangeBelief), 0.06, nil
		case "aerospace_settlement_bank":
			return string(horizonmodels.SimulationActionAdjustGuidance), 0.06, nil
		default:
			return string(horizonmodels.SimulationActionDoNothing), 0, nil
		}
	default:
		if step == 0 && idx == 0 {
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, nil
		}
		return string(horizonmodels.SimulationActionDoNothing), 0, nil
	}
}

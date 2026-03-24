package simulation

import (
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scenarios/teslabattery"
)

func teslaBatteryFallbackPopulation(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) ([]map[string]any, bool) {
	if !isTeslaBatteryHotPath(run) {
		return nil, false
	}
	variant := worldVariant(world)
	out := make([]map[string]any, 0, 18)
	add := func(agentClass, displayName string, weight float64, originRefs []string, allowedActions []string, attention, riskBias, trust, stress, budget float64, messageStyle string, policyHints map[string]any) {
		out = append(out, map[string]any{
			"agentClass":       agentClass,
			"displayName":      displayName,
			"originKind":       "scenario_archetype",
			"originRefs":       compactStrings(originRefs),
			"generatedBy":      "scenario.tesla_battery_logistics.population",
			"justification":    "Tesla battery logistics trade-shock fallback archetype",
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

	add("trade_official", "Trade policy desk", 1.44, []string{"/gov/us/department-of-commerce"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionEnforceRule),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.92, 0.68, 0.62, 0.34, 0.74, "institutional", map[string]any{"role": "trade-policy"})
	add("customs_official", "Customs clearance desk", 1.36, []string{"/gov/us/customs-and-border-protection"}, []string{
		string(horizonmodels.SimulationActionEnforceRule),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.88, 0.66, 0.60, 0.38, 0.72, "bureaucratic", map[string]any{"role": "customs"})
	add("facility_operator_nevada", "Nevada operations desk", 1.42, []string{"/fac/us/gigafactory-nevada"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.84, 0.60, 0.46, 0.42, 0.56, "operations", map[string]any{"facility": "nevada"})
	add("facility_operator_texas", "Texas operations desk", 1.48, []string{"/fac/us/gigafactory-texas"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.86, 0.62, 0.44, 0.44, 0.54, "operations", map[string]any{"facility": "texas"})
	add("panasonic_line_operator", "Panasonic battery lines", 1.24, []string{"/fac/jp/panasonic-giga-nevada-lines"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionSell),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.74, 0.58, 0.48, 0.40, 0.58, "supplier-ops", map[string]any{"role": "battery-cell-supplier"})
	add("catl_export_desk", "CATL export desk", 1.18, []string{"/fac/cn/catl-ningde"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionSell),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.78, 0.64, 0.42, 0.46, 0.52, "export", map[string]any{"role": "external-cell-optional"})
	add("material_supplier_lithium", "Lithium supplier desk", 1.14, []string{"/org/cl/sqm"}, []string{
		string(horizonmodels.SimulationActionSell),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.70, 0.56, 0.50, 0.38, 0.60, "upstream", map[string]any{"role": "materials"})
	add("lithium_refinery_operator", "Lithium refinery desk", 1.28, []string{"/fac/us/tesla-lithium-refinery-texas"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.78, 0.60, 0.46, 0.42, 0.58, "industrial", map[string]any{"role": "refining"})
	add("ocean_logistics_operator", "Ocean freight desk", 1.18, []string{"/market/us/transportation/freight-logistics"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.80, 0.62, 0.42, 0.46, 0.52, "freight", map[string]any{"mode": "ocean"})
	add("ground_logistics_operator", "Ground logistics desk", 1.16, []string{"/market/us/transportation/freight-logistics"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.78, 0.58, 0.44, 0.42, 0.54, "freight", map[string]any{"mode": "ground"})
	add("procurement_director", "Tesla procurement director", 1.54, []string{"/org/us/tesla", "/market/us/industrial/ev-battery-cells"}, []string{
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionLobby),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.94, 0.70, 0.38, 0.52, 0.62, "procurement", map[string]any{"role": "sourcing"})
	add("production_planner", "Production planning desk", 1.34, []string{"/fac/us/gigafactory-texas", "/fac/us/gigafactory-nevada"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionSell),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.86, 0.64, 0.42, 0.48, 0.58, "planning", map[string]any{"role": "throughput"})
	add("enterprise_treasury", "Enterprise treasury", 1.38, []string{"/org/us/tesla", "/market/us/finance/industrial-working-capital"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionLobby),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.88, 0.66, 0.46, 0.46, 0.66, "finance", map[string]any{"role": "treasury"})
	add("energy_storage_program_manager", "Energy storage program manager", 1.20, []string{"/market/us/energy/energy-storage-systems"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionSell),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDelayPurchase),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.80, 0.60, 0.44, 0.46, 0.52, "product", map[string]any{"segment": "energy-storage"})
	add("vehicle_program_manager", "Vehicle program manager", 1.22, []string{"/market/us/automotive/electric-vehicles"}, []string{
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionSell),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDelayPurchase),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.78, 0.58, 0.44, 0.44, 0.54, "product", map[string]any{"segment": "vehicles"})
	add("investor_relations", "Investor relations desk", 1.08, []string{"/market/us/capital/industrial-investor-confidence", "/org/us/tesla"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.94, 0.54, 0.50, 0.30, 0.62, "corporate-communications", map[string]any{"role": "ir"})
	add("equity_analyst", "Equity analyst", 1.00, []string{"/market/us/capital/industrial-investor-confidence"}, []string{
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.90, 0.62, 0.52, 0.34, 0.58, "analytical", map[string]any{"role": "street"})
	add("large_energy_customer", "Large storage buyer", 1.08, []string{"/market/us/energy/energy-storage-systems"}, []string{
		string(horizonmodels.SimulationActionDelayPurchase),
		string(horizonmodels.SimulationActionBuy),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.70, 0.56, 0.48, 0.38, 0.60, "customer", map[string]any{"role": "large-buyer"})
	add("competitor_demand_sink", "Competing demand sink", 1.04, []string{"/market/us/industrial/ev-battery-cells"}, []string{
		string(horizonmodels.SimulationActionBuy),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.66, 0.58, 0.42, 0.34, 0.64, "competitive", map[string]any{"role": "external-demand"})
	return out, true
}

func teslaBatteryFallbackActions(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) ([]horizonmodels.SimulationAction, bool) {
	if !isTeslaBatteryHotPath(run) {
		return nil, false
	}
	axis := runAxis(run)
	variant := worldVariant(world)
	out := make([]horizonmodels.SimulationAction, 0, min(len(agents), 40))
	for idx, agent := range agents {
		if !teslaBatteryAgentParticipates(agent, axis, variant, step, idx) {
			continue
		}
		actionType, delta, targets := teslaBatteryActionPlan(agent, axis, variant, step, idx)
		if actionType == "" {
			continue
		}
		action := horizonmodels.SimulationAction{
			Type:        actionType,
			ActorID:     agent.ID,
			TargetRefs:  compactStrings(targets),
			GeneratedBy: "scenario.tesla_battery_logistics.fallback",
			Payload: map[string]any{
				"delta": round3(delta),
			},
		}
		if actionType == string(horizonmodels.SimulationActionDoNothing) {
			action.Payload = nil
		}
		if actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionBroadcastMessage) {
			action.Justification = "scenario-aware response to Tesla battery logistics trade and delay shock"
		}
		out = append(out, action)
		if len(out) >= 40 {
			break
		}
	}
	return out, true
}

func teslaBatteryBootstrapAdjustments(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) (float64, float64, float64, float64, float64, bool) {
	if !isTeslaBatteryHotPath(run) {
		return 0, 0, 0, 0, 0, false
	}
	axis := runAxis(run)
	variant := worldVariant(world)
	risk, price, narrative, policy, supply := 0.0, 0.0, 0.0, 0.0, 0.0
	switch axis {
	case horizonmodels.AxisLegalCompliance:
		risk += 0.06
		policy += 0.10
		supply += 0.08
	case horizonmodels.AxisFinancialSettlement:
		risk += 0.05
		price += 0.13
		supply += 0.10
	case horizonmodels.AxisPhysicalFlowDemand:
		risk += 0.04
		price += 0.10
		supply += 0.14
	case horizonmodels.AxisInfoUncertainty:
		risk += 0.08
		narrative += 0.12
		policy += 0.08
		supply += 0.05
	}
	switch variant {
	case 1:
		risk += 0.05
		narrative += 0.04
	case 2:
		price += 0.05
		supply += 0.08
	case 3:
		risk += 0.03
		price += 0.04
		supply += 0.06
	case 4:
		narrative += 0.07
		policy += 0.05
	case 5:
		risk += 0.06
		price += 0.05
		narrative += 0.04
	case 6:
		risk += 0.08
		supply += 0.09
	}
	return risk, price, narrative, policy, supply, true
}

func isTeslaBatteryHotPath(run *horizonmodels.SimulationRun) bool {
	if run == nil {
		return false
	}
	if runQueryContainsAll(run, "tesla", "battery") &&
		(runQueryContainsAll(run, "logistics") || runQueryContainsAll(run, "procurement") || runQueryContainsAll(run, "supply")) {
		return true
	}
	if runHasAnyScenarioPath(run, "/org/us/tesla") &&
		runHasAnyScenarioPath(run,
			"/fac/us/gigafactory-nevada",
			"/fac/us/gigafactory-texas",
			"/program/us/tesla-battery-logistics-resilience-2026",
			"/decision/trade/restrict-battery-material-and-cell-flows-to-us-2026",
			"/mechanism/regulatory/export-controls-and-logistics-delays",
		) {
		return true
	}
	return runHasScenarioPathSet(run, teslabattery.SeedPaths(), 2)
}

func teslaBatteryAgentParticipates(agent *horizonmodels.SimulationAgentSpec, axis string, variant, step, idx int) bool {
	if agent == nil {
		return false
	}
	cls := strings.TrimSpace(agent.AgentClass)
	switch axis {
	case horizonmodels.AxisLegalCompliance:
		switch cls {
		case "trade_official", "customs_official", "procurement_director", "enterprise_treasury", "investor_relations", "pr_agent", "equity_analyst", "panasonic_line_operator", "catl_export_desk", "material_supplier_lithium", "lithium_refinery_operator", "facility_operator_texas", "facility_operator_nevada", "ocean_logistics_operator", "ground_logistics_operator", "port_operator":
			return true
		default:
			return step >= 2 && (idx+variant)%3 == 0
		}
	case horizonmodels.AxisFinancialSettlement:
		switch cls {
		case "enterprise_treasury", "procurement_director", "production_planner", "facility_operator_texas", "facility_operator_nevada", "panasonic_line_operator", "catl_export_desk", "material_supplier_lithium", "lithium_refinery_operator", "ocean_logistics_operator", "ground_logistics_operator", "rail_carrier", "warehouse_operator", "vehicle_program_manager", "energy_storage_program_manager", "pr_agent":
			return true
		default:
			return step >= 2 && (idx+variant)%4 == 0
		}
	case horizonmodels.AxisPhysicalFlowDemand:
		switch cls {
		case "facility_operator_texas", "facility_operator_nevada", "production_planner", "procurement_director", "panasonic_line_operator", "catl_export_desk", "material_supplier_lithium", "lithium_refinery_operator", "ocean_logistics_operator", "ground_logistics_operator", "rail_carrier", "warehouse_operator", "vehicle_program_manager", "energy_storage_program_manager", "large_energy_customer", "competitor_demand_sink":
			return true
		default:
			return step >= 1 && (idx+variant)%5 == 0
		}
	case horizonmodels.AxisInfoUncertainty:
		switch cls {
		case "trade_official", "customs_official", "investor_relations", "pr_agent", "equity_analyst", "facility_operator_texas", "facility_operator_nevada", "procurement_director", "enterprise_treasury", "panasonic_line_operator", "catl_export_desk", "ocean_logistics_operator", "port_operator":
			return true
		default:
			return step >= 2 && (idx+variant)%4 == 0
		}
	default:
		return true
	}
}

func teslaBatteryActionPlan(agent *horizonmodels.SimulationAgentSpec, axis string, variant, step, idx int) (string, float64, []string) {
	if agent == nil {
		return "", 0, nil
	}
	cls := strings.TrimSpace(agent.AgentClass)
	targets := compactStrings(agent.OriginRefs)
	switch axis {
	case horizonmodels.AxisLegalCompliance:
		switch cls {
		case "trade_official", "customs_official":
			if (step+variant+idx)%2 == 0 {
				return string(horizonmodels.SimulationActionEnforceRule), 0.07, []string{"/market/us/industrial/critical-battery-materials", "/market/us/industrial/ev-battery-cells", "/market/us/transportation/freight-logistics"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.06, []string{"/loc/us"}
		case "procurement_director":
			if step >= 1 {
				return string(horizonmodels.SimulationActionRequestExemption), 0.06, []string{"/gov/us/department-of-commerce", "/program/us/tesla-battery-logistics-resilience-2026"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/org/us/tesla"}
		case "enterprise_treasury":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06, []string{"/market/us/finance/industrial-working-capital"}
		case "panasonic_line_operator", "catl_export_desk", "material_supplier_lithium", "lithium_refinery_operator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, targets
		case "facility_operator_texas", "facility_operator_nevada":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, targets
		case "investor_relations", "equity_analyst":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, []string{"/market/us/capital/industrial-investor-confidence"}
		case "pr_agent":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/org/us/tesla", "/loc/us"}
		case "ocean_logistics_operator", "ground_logistics_operator", "port_operator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/us/transportation/freight-logistics"}
		}
	case horizonmodels.AxisFinancialSettlement:
		switch cls {
		case "enterprise_treasury":
			if step >= 2 {
				if (step+variant)%2 == 0 {
					return string(horizonmodels.SimulationActionHedgeCost), 0.05, []string{"/market/us/finance/industrial-working-capital"}
				}
				return string(horizonmodels.SimulationActionAdjustGuidance), 0.05, []string{"/market/us/capital/industrial-investor-confidence"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.09, []string{"/market/us/finance/industrial-working-capital"}
		case "procurement_director":
			switch (step + variant + idx) % 3 {
			case 0:
				return string(horizonmodels.SimulationActionSwitchSupplier), 0.06, []string{"/market/us/industrial/critical-battery-materials", "/market/us/industrial/ev-battery-cells"}
			case 1:
				return string(horizonmodels.SimulationActionAdjustBudget), 0.07, []string{"/org/us/tesla"}
			default:
				return string(horizonmodels.SimulationActionRequestExemption), 0.05, []string{"/gov/us/department-of-commerce"}
			}
		case "ocean_logistics_operator", "ground_logistics_operator", "rail_carrier":
			if (step+variant)%2 == 0 {
				return string(horizonmodels.SimulationActionExpediteShipment), 0.05, []string{"/market/us/transportation/freight-logistics", "/fac/us/port-of-long-beach"}
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06, []string{"/market/us/transportation/freight-logistics"}
		case "warehouse_operator":
			return string(horizonmodels.SimulationActionReserveInventory), 0.05, []string{"/fac/us/gigafactory-nevada", "/fac/us/gigafactory-texas"}
		case "port_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06, []string{"/market/us/transportation/freight-logistics"}
		case "panasonic_line_operator", "catl_export_desk", "material_supplier_lithium", "lithium_refinery_operator":
			if variant >= 2 {
				return string(horizonmodels.SimulationActionSell), 0.05, targets
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, targets
		case "facility_operator_texas", "facility_operator_nevada", "production_planner":
			if (step+idx)%2 == 0 {
				return string(horizonmodels.SimulationActionAllocateCapacity), 0.06, targets
			}
			return string(horizonmodels.SimulationActionAdjustBudget), 0.07, targets
		case "vehicle_program_manager", "energy_storage_program_manager":
			return string(horizonmodels.SimulationActionReprioritizeFlow), 0.05, targets
		case "large_energy_customer":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.04, []string{"/market/us/energy/energy-storage-systems"}
		case "equity_analyst", "investor_relations":
			return string(horizonmodels.SimulationActionAdjustGuidance), 0.04, []string{"/market/us/capital/industrial-investor-confidence"}
		case "pr_agent":
			return string(horizonmodels.SimulationActionAdjustGuidance), 0.04, []string{"/market/us/capital/industrial-investor-confidence", "/org/us/tesla"}
		}
	case horizonmodels.AxisPhysicalFlowDemand:
		switch cls {
		case "procurement_director":
			if (step+variant)%2 == 0 {
				return string(horizonmodels.SimulationActionRerouteShipment), 0.06, []string{"/fac/us/port-of-long-beach", "/market/us/transportation/freight-logistics"}
			}
			return string(horizonmodels.SimulationActionSwitchSupplier), 0.07, []string{"/market/us/industrial/critical-battery-materials", "/market/us/industrial/ev-battery-cells"}
		case "production_planner", "facility_operator_texas", "facility_operator_nevada":
			if (step+variant)%2 == 0 {
				return string(horizonmodels.SimulationActionAllocateCapacity), 0.06, targets
			}
			if cls == "production_planner" {
				return string(horizonmodels.SimulationActionReprioritizeFlow), 0.05, targets
			}
			return string(horizonmodels.SimulationActionSwitchSupplier), 0.06, targets
		case "panasonic_line_operator", "catl_export_desk", "material_supplier_lithium", "lithium_refinery_operator":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, targets
		case "ocean_logistics_operator", "ground_logistics_operator", "rail_carrier", "port_operator":
			if (step+variant+idx)%2 == 0 {
				return string(horizonmodels.SimulationActionRerouteShipment), 0.05, []string{"/market/us/transportation/freight-logistics", "/fac/us/port-of-long-beach"}
			}
			return string(horizonmodels.SimulationActionExpediteShipment), 0.05, []string{"/market/us/transportation/freight-logistics"}
		case "warehouse_operator":
			return string(horizonmodels.SimulationActionReserveInventory), 0.05, []string{"/fac/us/gigafactory-nevada", "/fac/us/gigafactory-texas"}
		case "vehicle_program_manager":
			return string(horizonmodels.SimulationActionReprioritizeFlow), 0.04, []string{"/market/us/automotive/electric-vehicles"}
		case "energy_storage_program_manager":
			if variant >= 3 {
				return string(horizonmodels.SimulationActionDelayPurchase), 0.05, []string{"/market/us/energy/energy-storage-systems"}
			}
			return string(horizonmodels.SimulationActionReprioritizeFlow), 0.05, []string{"/market/us/energy/energy-storage-systems"}
		case "large_energy_customer":
			if variant >= 3 {
				return string(horizonmodels.SimulationActionDelayPurchase), 0.05, []string{"/market/us/energy/energy-storage-systems"}
			}
			return string(horizonmodels.SimulationActionBuy), 0.04, []string{"/market/us/energy/energy-storage-systems"}
		case "competitor_demand_sink":
			return string(horizonmodels.SimulationActionBuy), 0.05, []string{"/market/us/industrial/ev-battery-cells"}
		}
	case horizonmodels.AxisInfoUncertainty:
		switch cls {
		case "trade_official", "customs_official":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/loc/us"}
		case "investor_relations", "equity_analyst":
			if (step+variant)%2 == 0 {
				return string(horizonmodels.SimulationActionAdjustGuidance), 0.05, []string{"/market/us/capital/industrial-investor-confidence"}
			}
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.06, []string{"/market/us/capital/industrial-investor-confidence"}
		case "pr_agent":
			if (step+variant)%2 == 0 {
				return string(horizonmodels.SimulationActionAdjustGuidance), 0.05, []string{"/market/us/capital/industrial-investor-confidence", "/org/us/tesla"}
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/org/us/tesla", "/loc/us"}
		case "facility_operator_texas", "facility_operator_nevada", "procurement_director", "enterprise_treasury":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/org/us/tesla"}
		case "panasonic_line_operator", "catl_export_desk":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, targets
		case "ocean_logistics_operator", "port_operator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, []string{"/market/us/transportation/freight-logistics"}
		}
	}
	if (step+variant+idx)%7 == 0 {
		return string(horizonmodels.SimulationActionDoNothing), 0, targets
	}
	return "", 0, nil
}

func teslaBatteryDisplay(reportID string) string {
	return fmt.Sprintf("tesla:%s", strings.TrimSpace(reportID))
}

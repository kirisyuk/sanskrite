package simulation

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func isSLBRussiaHotPath(run *horizonmodels.SimulationRun) bool {
	if run == nil {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(run.Request.Mode))
	question := strings.ToLower(strings.TrimSpace(run.Request.Question))
	if strings.Contains(mode, "slb-russia") || strings.Contains(mode, "schlumberger") {
		return true
	}
	if (strings.Contains(question, "slb") || strings.Contains(question, "schlumberger")) &&
		(strings.Contains(question, "russia") || strings.Contains(question, "russian")) &&
		(strings.Contains(question, "sanction") || strings.Contains(question, "spare part") || strings.Contains(question, "oilfield")) {
		return true
	}
	if runHasAnyScenarioPath(run,
		"/org/us/slb",
		"/domain/v1/supply-chain/geo/ru/subject/slb",
		"/org/ae/schlumberger-middle-east",
	) && runHasAnyScenarioPath(run,
		"/loc/ru",
		"/market/ru/energy/oilfield-services",
		"/domain/v1/policy-impact/geo/ru/topic/sanctions",
		"/fac/ae/jebel-ali-logistics-center",
		"/fac/ru/slb-tyumen-factory",
	) {
		return true
	}
	return false
}

func slbRussiaFallbackPopulation(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) ([]map[string]any, bool) {
	if !isSLBRussiaHotPath(run) {
		return nil, false
	}
	variant := worldVariant(world)
	out := make([]map[string]any, 0, 20)
	add := func(agentClass, displayName string, weight float64, originRefs, allowedActions []string, attention, riskBias, trust, stress, budget float64, messageStyle string, policyHints map[string]any) {
		out = append(out, map[string]any{
			"agentClass":       agentClass,
			"displayName":      displayName,
			"originKind":       "scenario_archetype",
			"originRefs":       compactStrings(originRefs),
			"generatedBy":      "scenario.slb_russia_sanctions.population",
			"justification":    "SLB Russia sanctions, transshipment, and oilfield-services fallback archetype",
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

	add("export_control_officer", "Export controls desk", 1.38, []string{"/domain/v1/policy-impact/geo/ru/topic/sanctions"}, []string{
		string(horizonmodels.SimulationActionEnforceRule),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.90, 0.72, 0.58, 0.40, 0.72, "institutional", map[string]any{"role": "export-controls"})
	add("sanctions_compliance_officer", "Sanctions compliance desk", 1.34, []string{"/org/us/slb", "/domain/v1/policy-impact/geo/ru/topic/sanctions"}, []string{
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.88, 0.68, 0.54, 0.46, 0.68, "compliance", map[string]any{"role": "sanctions-screening"})
	add("transshipment_compliance_officer", "Transshipment compliance desk", 1.28, []string{"/fac/ae/jebel-ali-logistics-center", "/org/ae/schlumberger-middle-east"}, []string{
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionReprioritizeFlow),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.86, 0.64, 0.52, 0.44, 0.64, "compliance-ops", map[string]any{"role": "transshipment-gate"})
	add("slb_russia_procurement_lead", "SLB Russia procurement lead", 1.48, []string{"/org/us/slb", "/market/ru/energy/oilfield-services"}, []string{
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionReprioritizeFlow),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.94, 0.70, 0.40, 0.54, 0.58, "procurement", map[string]any{"role": "russia-procurement"})
	add("jebel_ali_hub_operator", "Jebel Ali transshipment hub", 1.30, []string{"/fac/ae/jebel-ali-logistics-center", "/org/ae/schlumberger-middle-east"}, []string{
		string(horizonmodels.SimulationActionRerouteShipment),
		string(horizonmodels.SimulationActionReserveInventory),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.84, 0.60, 0.46, 0.44, 0.54, "logistics", map[string]any{"role": "hub-ops"})
	add("tyumen_pump_assembly_operator", "Tyumen REDA pump assembly", 1.24, []string{"/fac/ru/tyumen-reda-pump-plant", "/product/finished/mechanical/assembly/centrifugal-pumps"}, []string{
		string(horizonmodels.SimulationActionAllocateCapacity),
		string(horizonmodels.SimulationActionReserveInventory),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.78, 0.60, 0.44, 0.48, 0.52, "industrial", map[string]any{"role": "local-assembly"})
	add("tyumen_chemicals_operator", "Tyumen specialty chemicals plant", 1.22, []string{"/fac/ru/tyumen-specialty-chemicals-plant", "/product/finished/chemical/manufacturing/specialty-drilling-chemicals"}, []string{
		string(horizonmodels.SimulationActionAllocateCapacity),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.76, 0.58, 0.46, 0.46, 0.54, "industrial", map[string]any{"role": "local-chemicals"})
	add("china_equipment_supplier", "Chinese OFS equipment supplier", 1.20, []string{"/fac/cn/jereh-yantai-manufacturing-center", "/fac/cn/kerui-petroleum-dongying-factory"}, []string{
		string(horizonmodels.SimulationActionReserveInventory),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.72, 0.58, 0.48, 0.40, 0.56, "supplier", map[string]any{"role": "equipment"})
	add("kazakh_chemical_supplier", "Kazakh drilling chemicals supplier", 1.16, []string{"/fac/kz/atyrau-liquid-mud-plant", "/fac/kz/kazphosphate-taraz-plant"}, []string{
		string(horizonmodels.SimulationActionReserveInventory),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.70, 0.54, 0.50, 0.38, 0.58, "supplier", map[string]any{"role": "chemicals"})
	add("russian_oilfield_customer", "Russian oilfield customer procurement", 1.18, []string{"/fac/ru/rosneft-supply-base", "/market/ru/energy/oilfield-services"}, []string{
		string(horizonmodels.SimulationActionDelayPurchase),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.74, 0.52, 0.50, 0.42, 0.56, "customer", map[string]any{"role": "oilfield-customer"})
	add("ofs_settlement_bank", "OFS settlement bank", 1.18, []string{"/market/ru/energy/oilfield-services"}, []string{
		string(horizonmodels.SimulationActionHedgeCost),
		string(horizonmodels.SimulationActionAdjustBudget),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.74, 0.60, 0.46, 0.40, 0.70, "finance", map[string]any{"role": "settlement"})
	add("trade_credit_insurer", "Trade credit insurer", 1.10, []string{"/market/ru/energy/oilfield-services"}, []string{
		string(horizonmodels.SimulationActionAdjustGuidance),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.72, 0.56, 0.50, 0.36, 0.68, "risk", map[string]any{"role": "insurance"})
	add("parallel_import_broker", "Parallel-import routing broker", 1.12, []string{"/fac/ae/jebel-ali-logistics-center", "/loc/ru"}, []string{
		string(horizonmodels.SimulationActionRerouteShipment),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.80, 0.58, 0.42, 0.44, 0.50, "grey-trade", map[string]any{"role": "routing"})
	add("market_intelligence_analyst", "Oilfield market intelligence", 1.04, []string{"/market/ru/energy/oilfield-services"}, []string{
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionChangeBelief),
		string(horizonmodels.SimulationActionAdjustGuidance),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.88, 0.56, 0.54, 0.30, 0.60, "analysis", map[string]any{"role": "market-intel"})
	add("pr_agent", "SLB Russia PR desk", 1.02, []string{"/org/us/slb", "/market/ru/energy/oilfield-services"}, []string{
		string(horizonmodels.SimulationActionBroadcastMessage),
		string(horizonmodels.SimulationActionIssueStatement),
		string(horizonmodels.SimulationActionAdjustGuidance),
		string(horizonmodels.SimulationActionDoNothing),
	}, 0.90, 0.54, 0.56, 0.30, 0.62, "communications", map[string]any{"role": "external-comms"})
	out = append(out, slbRussiaDerivedPopulationExtras(run)...)
	return out, true
}

func slbRussiaFallbackActions(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) ([]horizonmodels.SimulationAction, bool) {
	if !isSLBRussiaHotPath(run) {
		return nil, false
	}
	axis := runAxis(run)
	variant := worldVariant(world)
	out := make([]horizonmodels.SimulationAction, 0, min(len(agents), 42))
	for idx, agent := range agents {
		if !slbRussiaAgentParticipates(agent, axis, variant, step, idx) {
			continue
		}
		actionType, delta, targets := slbRussiaActionPlan(agent, axis, variant, step, idx)
		if actionType == "" {
			continue
		}
		action := horizonmodels.SimulationAction{
			Type:        actionType,
			ActorID:     agent.ID,
			TargetRefs:  compactStrings(targets),
			GeneratedBy: "scenario.slb_russia_sanctions.fallback",
			Payload: map[string]any{
				"delta": round3(delta),
			},
		}
		if actionType == string(horizonmodels.SimulationActionDoNothing) {
			action.Payload = nil
		}
		if actionType == string(horizonmodels.SimulationActionIssueStatement) || actionType == string(horizonmodels.SimulationActionBroadcastMessage) || actionType == string(horizonmodels.SimulationActionRequestExemption) {
			action.Justification = "scenario-aware response to SLB Russia sanctions, transshipment, and spare-parts restrictions"
		}
		out = append(out, action)
		if len(out) >= 42 {
			break
		}
	}
	return out, true
}

func slbRussiaBootstrapAdjustments(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld) (float64, float64, float64, float64, float64, bool) {
	if !isSLBRussiaHotPath(run) {
		return 0, 0, 0, 0, 0, false
	}
	axis := runAxis(run)
	variant := worldVariant(world)
	risk, price, narrative, policy, supply := 0.0, 0.0, 0.0, 0.0, 0.0
	switch axis {
	case horizonmodels.AxisLegalCompliance, horizonmodels.AxisLegalAccess:
		risk += 0.08
		policy += 0.14
		supply += 0.06
	case horizonmodels.AxisFinancialSettlement:
		risk += 0.07
		price += 0.10
		policy += 0.05
		supply += 0.08
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		risk += 0.06
		price += 0.08
		supply += 0.16
	case horizonmodels.AxisInfoReputation, horizonmodels.AxisInfoUncertainty:
		risk += 0.08
		narrative += 0.14
		policy += 0.10
	}
	switch variant {
	case 1:
		risk += 0.03
		policy += 0.03
	case 2:
		price += 0.04
		supply += 0.04
	case 3:
		narrative += 0.04
	case 4:
		risk += 0.05
		supply += 0.03
	case 5:
		price += 0.03
		narrative += 0.02
	case 6:
		policy += 0.04
		supply += 0.05
	}
	return round3(risk), round3(price), round3(narrative), round3(policy), round3(supply), true
}

func slbRussiaAgentParticipates(agent *horizonmodels.SimulationAgentSpec, axis string, variant, step, idx int) bool {
	if agent == nil {
		return false
	}
	class := strings.TrimSpace(agent.AgentClass)
	switch axis {
	case horizonmodels.AxisLegalCompliance, horizonmodels.AxisLegalAccess:
		switch class {
		case "export_control_officer", "sanctions_compliance_officer", "transshipment_compliance_officer", "slb_russia_procurement_lead", "jebel_ali_hub_operator", "pr_agent":
			return true
		}
	case horizonmodels.AxisFinancialSettlement:
		switch class {
		case "sanctions_compliance_officer", "slb_russia_procurement_lead", "jebel_ali_hub_operator", "ofs_settlement_bank", "trade_credit_insurer", "parallel_import_broker", "pr_agent":
			return true
		}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		switch class {
		case "slb_russia_procurement_lead", "jebel_ali_hub_operator", "tyumen_pump_assembly_operator", "tyumen_chemicals_operator", "china_equipment_supplier", "kazakh_chemical_supplier", "russian_oilfield_customer", "parallel_import_broker":
			return true
		}
	case horizonmodels.AxisInfoReputation, horizonmodels.AxisInfoUncertainty:
		switch class {
		case "pr_agent", "market_intelligence_analyst", "sanctions_compliance_officer", "transshipment_compliance_officer", "export_control_officer", "russian_oilfield_customer", "trade_credit_insurer":
			return true
		}
	}
	if strings.HasPrefix(class, "regional_supply_shoulder_") {
		return axis == horizonmodels.AxisPhysicalFlowSupply || axis == horizonmodels.AxisLogisticsCapacity || axis == horizonmodels.AxisLogisticsLatency || axis == horizonmodels.AxisFinancialSettlement
	}
	if strings.HasPrefix(class, "product_risk_manager_") {
		return axis == horizonmodels.AxisPhysicalFlowSupply || axis == horizonmodels.AxisLogisticsCapacity || axis == horizonmodels.AxisFinancialSettlement || axis == horizonmodels.AxisLegalCompliance || axis == horizonmodels.AxisLegalAccess
	}
	return (idx+step+variant)%5 == 0
}

func slbRussiaActionPlan(agent *horizonmodels.SimulationAgentSpec, axis string, variant, step, idx int) (string, float64, []string) {
	if agent == nil {
		return "", 0, nil
	}
	class := strings.TrimSpace(agent.AgentClass)
	switch axis {
	case horizonmodels.AxisLegalCompliance, horizonmodels.AxisLegalAccess:
		switch class {
		case "export_control_officer":
			return string(horizonmodels.SimulationActionEnforceRule), 0.07, agent.OriginRefs
		case "sanctions_compliance_officer", "transshipment_compliance_officer", "slb_russia_procurement_lead":
			return string(horizonmodels.SimulationActionRequestExemption), 0.06, agent.OriginRefs
		case "jebel_ali_hub_operator":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, agent.OriginRefs
		case "pr_agent":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.05, agent.OriginRefs
		}
		if strings.HasPrefix(class, "product_risk_manager_") {
			if strings.Contains(strings.Join(agent.OriginRefs, " "), "telemetry-tools") {
				return string(horizonmodels.SimulationActionRequestExemption), 0.05, agent.OriginRefs
			}
			return string(horizonmodels.SimulationActionIssueStatement), 0.04, agent.OriginRefs
		}
	case horizonmodels.AxisFinancialSettlement:
		switch class {
		case "ofs_settlement_bank":
			return string(horizonmodels.SimulationActionHedgeCost), 0.07, agent.OriginRefs
		case "trade_credit_insurer":
			return string(horizonmodels.SimulationActionAdjustGuidance), 0.06, agent.OriginRefs
		case "slb_russia_procurement_lead", "sanctions_compliance_officer":
			return string(horizonmodels.SimulationActionAdjustBudget), 0.06, agent.OriginRefs
		case "jebel_ali_hub_operator", "parallel_import_broker":
			return string(horizonmodels.SimulationActionReserveInventory), 0.05, agent.OriginRefs
		case "pr_agent":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, agent.OriginRefs
		}
		if strings.HasPrefix(class, "regional_supply_shoulder_") {
			return string(horizonmodels.SimulationActionReserveInventory), 0.05, agent.OriginRefs
		}
		if strings.HasPrefix(class, "product_risk_manager_") {
			return string(horizonmodels.SimulationActionAdjustBudget), 0.05, agent.OriginRefs
		}
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		switch class {
		case "jebel_ali_hub_operator", "parallel_import_broker":
			return string(horizonmodels.SimulationActionRerouteShipment), 0.08, agent.OriginRefs
		case "slb_russia_procurement_lead":
			return string(horizonmodels.SimulationActionReprioritizeFlow), 0.07, agent.OriginRefs
		case "tyumen_pump_assembly_operator", "tyumen_chemicals_operator":
			return string(horizonmodels.SimulationActionAllocateCapacity), 0.07, agent.OriginRefs
		case "china_equipment_supplier", "kazakh_chemical_supplier":
			return string(horizonmodels.SimulationActionReserveInventory), 0.06, agent.OriginRefs
		case "russian_oilfield_customer":
			return string(horizonmodels.SimulationActionDelayPurchase), 0.05, agent.OriginRefs
		}
		if strings.HasPrefix(class, "regional_supply_shoulder_") {
			return string(horizonmodels.SimulationActionRerouteShipment), 0.06, agent.OriginRefs
		}
		if strings.HasPrefix(class, "product_risk_manager_") {
			if strings.Contains(strings.Join(agent.OriginRefs, " "), "/raw/chemical/") {
				return string(horizonmodels.SimulationActionReserveInventory), 0.06, agent.OriginRefs
			}
			return string(horizonmodels.SimulationActionReprioritizeFlow), 0.06, agent.OriginRefs
		}
	case horizonmodels.AxisInfoReputation, horizonmodels.AxisInfoUncertainty:
		switch class {
		case "pr_agent":
			return string(horizonmodels.SimulationActionBroadcastMessage), 0.07, agent.OriginRefs
		case "market_intelligence_analyst":
			return string(horizonmodels.SimulationActionChangeBelief), 0.06, agent.OriginRefs
		case "trade_credit_insurer", "export_control_officer", "sanctions_compliance_officer":
			return string(horizonmodels.SimulationActionIssueStatement), 0.05, agent.OriginRefs
		case "russian_oilfield_customer":
			return string(horizonmodels.SimulationActionChangeBelief), 0.04, agent.OriginRefs
		}
	}
	if (idx+step+variant)%3 == 0 {
		return string(horizonmodels.SimulationActionDoNothing), 0, nil
	}
	return "", 0, nil
}

type slbDerivedShoulder struct {
	AgentClass     string
	DisplayName    string
	CountryCode    string
	CountryName    string
	OriginRefs     []string
	AllowedActions []string
	Weight         float64
	Attention      float64
	RiskBias       float64
	Trust          float64
	Stress         float64
	Budget         float64
}

func slbRussiaDerivedPopulationExtras(run *horizonmodels.SimulationRun) []map[string]any {
	out := make([]map[string]any, 0, 10)
	for _, shoulder := range slbRussiaDerivedShoulders(run) {
		out = append(out, map[string]any{
			"agentClass":       shoulder.AgentClass,
			"displayName":      shoulder.DisplayName,
			"originKind":       "graph_scope_derivation",
			"originRefs":       compactStrings(shoulder.OriginRefs),
			"generatedBy":      "scenario.slb_russia_sanctions.population.derived",
			"justification":    "Graph-derived regional shoulder for SLB Russia scenario",
			"populationWeight": shoulder.Weight,
			"traits": map[string]any{
				"attention": shoulder.Attention,
				"riskBias":  shoulder.RiskBias,
			},
			"beliefState": map[string]any{
				"trust":  shoulder.Trust,
				"stress": shoulder.Stress,
			},
			"resourceState": map[string]any{
				"budget": shoulder.Budget,
			},
			"allowedActions": shoulder.AllowedActions,
			"messageStyle":   "regional-shoulder",
			"policyHints": map[string]any{
				"role":        "regional-shoulder",
				"countryCode": shoulder.CountryCode,
				"countryName": shoulder.CountryName,
			},
		})
	}
	for _, product := range slbRussiaDerivedProducts(run) {
		out = append(out, map[string]any{
			"agentClass":       product.AgentClass,
			"displayName":      product.DisplayName,
			"originKind":       "graph_scope_derivation",
			"originRefs":       compactStrings(product.OriginRefs),
			"generatedBy":      "scenario.slb_russia_sanctions.population.derived",
			"justification":    "Graph-derived product risk desk for SLB Russia scenario",
			"populationWeight": product.Weight,
			"traits": map[string]any{
				"attention": product.Attention,
				"riskBias":  product.RiskBias,
			},
			"beliefState": map[string]any{
				"trust":  product.Trust,
				"stress": product.Stress,
			},
			"resourceState": map[string]any{
				"budget": product.Budget,
			},
			"allowedActions": product.AllowedActions,
			"messageStyle":   "product-risk",
			"policyHints": map[string]any{
				"role":        "product-risk",
				"productPath": product.ProductPath,
				"productName": product.ProductName,
			},
		})
	}
	return out
}

type slbDerivedProduct struct {
	AgentClass     string
	DisplayName    string
	ProductPath    string
	ProductName    string
	OriginRefs     []string
	AllowedActions []string
	Weight         float64
	Attention      float64
	RiskBias       float64
	Trust          float64
	Stress         float64
	Budget         float64
}

func slbRussiaScopePaths(run *horizonmodels.SimulationRun) []string {
	if run == nil {
		return nil
	}
	return compactStrings(append(append([]string{}, run.BaselineRefs.ScopePaths...), run.Request.SeedPaths...))
}

func slbRussiaDerivedShoulders(run *horizonmodels.SimulationRun) []slbDerivedShoulder {
	paths := slbRussiaScopePaths(run)
	type countryBucket struct {
		code       string
		facilities []string
		score      float64
	}
	buckets := map[string]*countryBucket{}
	for _, path := range paths {
		if !strings.HasPrefix(path, "/fac/") {
			continue
		}
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 3 {
			continue
		}
		country := strings.ToLower(strings.TrimSpace(parts[1]))
		if country == "" || country == "ru" || country == "us" || country == "ae" {
			continue
		}
		bucket := buckets[country]
		if bucket == nil {
			bucket = &countryBucket{code: country}
			buckets[country] = bucket
		}
		bucket.facilities = append(bucket.facilities, path)
		bucket.score += slbShoulderFacilityScore(path)
	}
	items := make([]countryBucket, 0, len(buckets))
	for _, bucket := range buckets {
		if len(bucket.facilities) == 0 {
			continue
		}
		sort.Strings(bucket.facilities)
		items = append(items, *bucket)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].code < items[j].code
		}
		return items[i].score > items[j].score
	})
	out := make([]slbDerivedShoulder, 0, min(len(items), 4))
	for _, item := range items[:min(len(items), 4)] {
		countryName := slbCountryName(item.code)
		out = append(out, slbDerivedShoulder{
			AgentClass:     "regional_supply_shoulder_" + item.code,
			DisplayName:    fmt.Sprintf("%s supply shoulder", countryName),
			CountryCode:    item.code,
			CountryName:    countryName,
			OriginRefs:     item.facilities[:min(len(item.facilities), 3)],
			AllowedActions: []string{string(horizonmodels.SimulationActionRerouteShipment), string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionSwitchSupplier), string(horizonmodels.SimulationActionIssueStatement), string(horizonmodels.SimulationActionDoNothing)},
			Weight:         1.02 + 0.05*float64(min(len(item.facilities), 4)),
			Attention:      0.72,
			RiskBias:       0.58,
			Trust:          0.46,
			Stress:         0.42,
			Budget:         0.56,
		})
	}
	return out
}

func slbRussiaDerivedProducts(run *horizonmodels.SimulationRun) []slbDerivedProduct {
	paths := slbRussiaScopePaths(run)
	type productBucket struct {
		path  string
		score float64
	}
	items := make([]productBucket, 0, 8)
	seen := map[string]struct{}{}
	for _, path := range paths {
		if !strings.HasPrefix(path, "/product/") {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		score := slbProductRiskScore(path)
		if score <= 0 {
			continue
		}
		items = append(items, productBucket{path: path, score: score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].path < items[j].path
		}
		return items[i].score > items[j].score
	})
	out := make([]slbDerivedProduct, 0, min(len(items), 5))
	for _, item := range items[:min(len(items), 5)] {
		name := slbProductName(item.path)
		class := "product_risk_manager_" + slbSafeSlug(lastPathSegment(item.path))
		actions := []string{string(horizonmodels.SimulationActionReprioritizeFlow), string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionDoNothing)}
		if strings.Contains(item.path, "/raw/chemical/") || strings.Contains(item.path, "chemical") {
			actions = []string{string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionSwitchSupplier), string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionDoNothing)}
		}
		if strings.Contains(item.path, "telemetry-tools") {
			actions = []string{string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionReprioritizeFlow), string(horizonmodels.SimulationActionAdjustGuidance), string(horizonmodels.SimulationActionDoNothing)}
		}
		out = append(out, slbDerivedProduct{
			AgentClass:     class,
			DisplayName:    fmt.Sprintf("%s risk desk", name),
			ProductPath:    item.path,
			ProductName:    name,
			OriginRefs:     []string{item.path},
			AllowedActions: actions,
			Weight:         1.00 + 0.06*item.score,
			Attention:      0.76,
			RiskBias:       0.62,
			Trust:          0.44,
			Stress:         0.48,
			Budget:         0.52,
		})
	}
	return out
}

func slbShoulderFacilityScore(path string) float64 {
	score := 1.0
	switch {
	case strings.Contains(path, "port"), strings.Contains(path, "terminal"), strings.Contains(path, "logistics"), strings.Contains(path, "supply-base"):
		score += 1.3
	case strings.Contains(path, "assembly"), strings.Contains(path, "factory"), strings.Contains(path, "plant"), strings.Contains(path, "manufacturing"), strings.Contains(path, "facility"), strings.Contains(path, "center"):
		score += 0.8
	}
	return score
}

func slbProductRiskScore(path string) float64 {
	score := 0.0
	switch {
	case strings.Contains(path, "telemetry-tools"):
		score += 1.9
	case strings.Contains(path, "well-completion-systems"):
		score += 1.8
	case strings.Contains(path, "centrifugal-pumps"), strings.Contains(path, "hydraulic-pumps"):
		score += 1.7
	case strings.Contains(path, "specialty-drilling-chemicals"), strings.Contains(path, "oilfield-production-chemicals"):
		score += 1.7
	case strings.Contains(path, "drill-bits"):
		score += 1.55
	case strings.Contains(path, "oilfield-equipment"), strings.Contains(path, "oilfield-drilling-equipment"), strings.Contains(path, "oilfield-spare-parts"):
		score += 1.45
	}
	switch {
	case strings.Contains(path, "/finished/"):
		score += 0.25
	case strings.Contains(path, "/component/"):
		score += 0.15
	case strings.Contains(path, "/raw/"):
		score += 0.10
	}
	return score
}

func slbCountryName(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "cn":
		return "China"
	case "kz":
		return "Kazakhstan"
	case "tr":
		return "Turkey"
	case "in":
		return "India"
	case "sg":
		return "Singapore"
	case "sa":
		return "Saudi Arabia"
	default:
		return strings.ToUpper(strings.TrimSpace(code))
	}
}

func slbProductName(path string) string {
	return strings.ReplaceAll(lastPathSegment(path), "-", " ")
}

func lastPathSegment(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func slbSafeSlug(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, " ", "-")
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

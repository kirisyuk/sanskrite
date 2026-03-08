package propagation

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type PropagationNodeOverlay struct {
	PrimaryRole      string
	BusinessRole     string
	Roles            []string
	ExposureModes    []string
	OperationalScore float64
	BridgeScore      float64
	CriticalityScore float64
}

type PropagationEdgeOverlay struct {
	TransmissionProfile string
	Roles               []string
	WeightHint          float64
}

func BuildPropagationNodeOverlay(fragment *horizonmodels.HorizonFragment) PropagationNodeOverlay {
	if fragment == nil {
		return PropagationNodeOverlay{}
	}
	text := overlayFragmentText(fragment)
	overlay := PropagationNodeOverlay{
		OperationalScore: 0.20,
		BridgeScore:      0.10,
		CriticalityScore: 0.12,
	}
	addRole := func(role string) {
		overlay.Roles = overlayAppendUnique(overlay.Roles, role)
	}
	addExposure := func(mode string) {
		overlay.ExposureModes = overlayAppendUnique(overlay.ExposureModes, mode)
	}
	setPrimary := func(role string) {
		if overlay.PrimaryRole == "" {
			overlay.PrimaryRole = role
		}
	}
	setBusiness := func(role string) {
		if overlay.BusinessRole == "" {
			overlay.BusinessRole = role
		}
	}
	forceBusiness := func(role string) {
		overlay.BusinessRole = role
	}
	applyDomainSemantic := func(role, business string, operational, bridge, criticality float64, exposures ...string) {
		setPrimary(role)
		setBusiness(business)
		for _, exposure := range exposures {
			addExposure(exposure)
		}
		overlay.OperationalScore = maxOverlayFloat(overlay.OperationalScore, operational)
		overlay.BridgeScore = maxOverlayFloat(overlay.BridgeScore, bridge)
		overlay.CriticalityScore = maxOverlayFloat(overlay.CriticalityScore, criticality)
	}

	switch {
	case fragment.Type == horizonmodels.FragmentTypeAffectedZone:
		strategic := overlayFragmentMetricValue(fragment, horizonmodels.MetricPathImportanceStrategic)
		bridge := overlayFragmentMetricValue(fragment, horizonmodels.MetricPathImportanceBridge)
		businessBlast := overlayFragmentMetricValue(fragment, horizonmodels.MetricPathImportanceBusinessBlastRadius)
		conflictBlast := overlayFragmentMetricValue(fragment, horizonmodels.MetricPathImportanceConflictBlastRadius)
		setPrimary("zone_field")
		setBusiness("zone_operator")
		addRole("source")
		addExposure("direct_geo")
		if bridge >= 0.18 {
			addRole("bridge")
			addExposure("route_dependency")
		}
		if businessBlast >= 0.12 {
			addRole("amplifier")
			addExposure("market_demand")
		}
		if conflictBlast >= 0.18 {
			addRole("controller")
		}
		overlay.OperationalScore = overlayClamp(0.24+conflictBlast*0.30+businessBlast*0.30+strategic*0.12, 0.24, 0.92)
		overlay.BridgeScore = overlayClamp(0.10+bridge*0.62+businessBlast*0.08, 0.10, 0.95)
		overlay.CriticalityScore = overlayClamp(0.18+strategic*0.28+maxOverlayFloat(businessBlast, conflictBlast)*0.40, 0.18, 0.94)
	case IsChokepointFragment(fragment):
		setPrimary("chokepoint")
		setBusiness("transit_controller")
		addRole("bridge")
		addRole("amplifier")
		addExposure("route_dependency")
		addExposure("direct_geo")
		overlay.OperationalScore = 0.92
		overlay.BridgeScore = 0.95
		overlay.CriticalityScore = 0.94
	case overlayApplyMarketDomainSemantics(fragment, &overlay, addRole, addExposure, setPrimary, setBusiness, forceBusiness, applyDomainSemantic):
	case overlayHasAny(text, "naval base", "air base", "air-defense", "air defense", "air-defence", "arsenal", "missile", "fighter", "air force", "navy", "defense", "defence", "military"):
		setPrimary("defense_operator")
		setBusiness("operator")
		addRole("controller")
		addExposure("direct_geo")
		overlay.OperationalScore = 0.90
		overlay.BridgeScore = 0.44
		overlay.CriticalityScore = 0.76
	case overlayHasAny(text, "lng", "natural-gas", "gas", "oil", "refinery", "pipeline", "petrochem", "terminal", "power", "grid", "field") ||
		fragment.Type == horizonmodels.FragmentTypeDeposit:
		setPrimary("energy_asset")
		setBusiness("producer")
		addRole("source")
		addExposure("energy_input")
		if overlayHasAny(text, "terminal", "pipeline", "port") {
			addRole("bridge")
			addExposure("route_dependency")
			overlay.BridgeScore = 0.76
		}
		overlay.OperationalScore = 0.86
		overlay.CriticalityScore = 0.88
	case overlayHasAny(text, "port", "shipping", "maritime", "warehouse", "freight", "logistics", "terminal", "rail", "shipyard", "distribution center", "export distribution center"):
		setPrimary("logistics_hub")
		setBusiness("shipper")
		addRole("bridge")
		addExposure("route_dependency")
		addExposure("direct_geo")
		overlay.OperationalScore = 0.72
		overlay.BridgeScore = 0.82
		overlay.CriticalityScore = 0.70
		if overlayHasAny(text, "export distribution center", "distribution center", "warehouse") {
			forceBusiness("distributor")
			overlay.OperationalScore = 0.58
			overlay.CriticalityScore = 0.52
		}
	case overlayHasAny(text, "manufacturing", "plant", "factory", "assembly", "fab", "foundry", "steel complex", "production line"):
		setPrimary("manufacturing_operator")
		setBusiness("assembler")
		addRole("sink")
		addRole("source")
		addExposure("component_input")
		addExposure("energy_input")
		overlay.OperationalScore = 0.78
		overlay.BridgeScore = 0.34
		overlay.CriticalityScore = 0.72
	case fragment.Type == horizonmodels.FragmentTypeMarket:
		setPrimary("market_aggregator")
		setBusiness("market_operator")
		addRole("bridge")
		addRole("amplifier")
		addExposure("market_demand")
		overlay.OperationalScore = 0.56
		overlay.BridgeScore = 0.72
		overlay.CriticalityScore = 0.58
	case strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/gov/") || overlayHasAny(text, "regulator", "authority", "ministry", "agency", "commission", "department"):
		setPrimary("regulator")
		setBusiness("infrastructure_controller")
		addRole("regulator")
		addRole("controller")
		addExposure("regulatory")
		overlay.OperationalScore = 0.62
		overlay.BridgeScore = 0.38
		overlay.CriticalityScore = 0.64
	case fragment.Type == horizonmodels.FragmentTypeGeoRegion:
		setPrimary("geo_theater")
		setBusiness("geo_scope")
		addExposure("direct_geo")
		overlay.OperationalScore = 0.38
		overlay.BridgeScore = 0.18
		overlay.CriticalityScore = 0.30
	default:
		setPrimary("generic_node")
		setBusiness("generic")
		overlay.OperationalScore = 0.32
		overlay.BridgeScore = 0.16
		overlay.CriticalityScore = 0.28
	}

	if fragmentHasLowReplaceability(fragment) {
		overlay.CriticalityScore = overlayClamp01(overlay.CriticalityScore + 0.14)
		addRole("buffer")
	}
	if strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/fac/") {
		addRole("operator")
	}
	if fragment.Type == horizonmodels.FragmentTypeDeposit {
		addRole("source")
	}
	return overlay
}

func overlayApplyMarketDomainSemantics(
	fragment *horizonmodels.HorizonFragment,
	overlay *PropagationNodeOverlay,
	addRole func(string),
	addExposure func(string),
	setPrimary func(string),
	setBusiness func(string),
	forceBusiness func(string),
	applyDomainSemantic func(string, string, float64, float64, float64, ...string),
) bool {
	if fragment == nil || overlay == nil {
		return false
	}
	domain := strings.ToLower(strings.TrimSpace(fragment.MarketDomain))
	vertical := strings.ToLower(strings.TrimSpace(fragment.MarketVertical))
	if domain == "" {
		return false
	}
	switch domain {
	case "energy":
		applyDomainSemantic("energy_asset", "producer", 0.82, 0.34, 0.86, "energy_input")
		if overlayHasAny(vertical, "lng", "oil-and-gas", "chemicals-and-petrochemicals", "power-and-utilities") {
			addRole("source")
		}
		if overlayHasAny(vertical, "lng", "oil-and-gas", "power-and-utilities") {
			addExposure("route_dependency")
			overlay.BridgeScore = maxOverlayFloat(overlay.BridgeScore, 0.48)
		}
		return true
	case "materials":
		applyDomainSemantic("materials_operator", "producer", 0.74, 0.26, 0.78, "component_input")
		addRole("source")
		return true
	case "industrial":
		if overlayHasAny(vertical, "shipping", "logistics", "port-services", "aviation") {
			applyDomainSemantic("logistics_hub", "shipper", 0.72, 0.82, 0.70, "route_dependency", "direct_geo")
			addRole("bridge")
			return true
		}
		if overlayHasAny(vertical, "construction-and-building-materials", "automotive", "aerospace") {
			applyDomainSemantic("manufacturing_operator", "assembler", 0.76, 0.32, 0.70, "component_input", "energy_input")
			addRole("sink")
			addRole("source")
			return true
		}
	case "technology":
		if overlayHasAny(vertical, "semiconductors", "electronics-assembly") {
			applyDomainSemantic("manufacturing_operator", "assembler", 0.80, 0.28, 0.74, "component_input", "energy_input")
			addRole("sink")
			addRole("source")
			return true
		}
		if overlayHasAny(vertical, "cloud-and-software") {
			applyDomainSemantic("digital_operator", "operator", 0.62, 0.34, 0.56, "infrastructure", "communication")
			return true
		}
	case "telecom":
		applyDomainSemantic("network_operator", "operator", 0.68, 0.58, 0.64, "infrastructure", "communication")
		addRole("bridge")
		return true
	case "finance":
		if overlayHasAny(vertical, "payments", "market-infrastructure") {
			applyDomainSemantic("financial_infrastructure", "market_operator", 0.60, 0.62, 0.66, "payment", "regulatory")
			addRole("bridge")
			addRole("controller")
			return true
		}
		if overlayHasAny(vertical, "banking", "insurance") {
			applyDomainSemantic("financial_operator", "market_operator", 0.54, 0.36, 0.58, "payment", "market_demand")
			return true
		}
	case "consumer":
		if overlayHasAny(vertical, "retail", "ecommerce", "hospitality-and-leisure") {
			applyDomainSemantic("market_aggregator", "distributor", 0.56, 0.52, 0.50, "market_demand")
			addRole("amplifier")
			return true
		}
	case "agri-food":
		if overlayHasAny(vertical, "agriculture", "food-processing") {
			applyDomainSemantic("agri_operator", "producer", 0.66, 0.24, 0.62, "component_input", "market_demand")
			addRole("source")
			return true
		}
	case "healthcare":
		if overlayHasAny(vertical, "pharmaceuticals") {
			applyDomainSemantic("healthcare_operator", "producer", 0.72, 0.22, 0.70, "component_input", "regulatory")
			addRole("source")
			return true
		}
		if overlayHasAny(vertical, "health-services") {
			applyDomainSemantic("healthcare_provider", "operator", 0.58, 0.18, 0.64, "regulatory")
			return true
		}
	case "defense":
		applyDomainSemantic("defense_operator", "operator", 0.88, 0.40, 0.78, "direct_geo")
		addRole("controller")
		return true
	case "real-estate":
		applyDomainSemantic("property_operator", "operator", 0.48, 0.22, 0.46, "direct_geo")
		return true
	case "professional-services":
		applyDomainSemantic("service_operator", "operator", 0.44, 0.20, 0.40, "regulatory")
		return true
	case "public-sector":
		applyDomainSemantic("regulator", "infrastructure_controller", 0.60, 0.34, 0.60, "regulatory")
		addRole("controller")
		return true
	case "education":
		applyDomainSemantic("service_operator", "operator", 0.38, 0.14, 0.34, "direct_geo")
		return true
	case "media":
		applyDomainSemantic("media_operator", "operator", 0.42, 0.28, 0.38, "communication")
		return true
	}
	return false
}

func (o PropagationNodeOverlay) HasRole(role string) bool {
	role = strings.TrimSpace(role)
	for _, item := range o.Roles {
		if strings.TrimSpace(item) == role {
			return true
		}
	}
	return false
}

func (o PropagationNodeOverlay) HasExposure(mode string) bool {
	mode = strings.TrimSpace(mode)
	for _, item := range o.ExposureModes {
		if strings.TrimSpace(item) == mode {
			return true
		}
	}
	return false
}

func BuildPropagationEdgeOverlay(axis string, edge *horizonmodels.DependencyEdge, fromFrag *horizonmodels.HorizonFragment) PropagationEdgeOverlay {
	family := classifyEdgeFamily(axis, fromFrag, edge)
	if family == "" {
		family = "direct_dependency"
	}
	fromOverlay := BuildPropagationNodeOverlay(fromFrag)
	overlay := PropagationEdgeOverlay{
		TransmissionProfile: "generic_link",
		Roles:               []string{family},
		WeightHint:          1.0,
	}
	switch family {
	case "chokepoint_logistics":
		overlay.TransmissionProfile = "route_transit"
		overlay.WeightHint = 1.18
	case "energy_supply_chain":
		overlay.TransmissionProfile = "energy_input"
		overlay.WeightHint = 1.14
	case "market_exposure":
		overlay.TransmissionProfile = "market_transmission"
		overlay.WeightHint = 1.08
	case "regulatory_control":
		overlay.TransmissionProfile = "regulatory_gate"
		overlay.WeightHint = 0.96
	case "organizational_control":
		overlay.TransmissionProfile = "operational_control"
		overlay.WeightHint = 1.02
	case "facility_dependency":
		overlay.TransmissionProfile = "manufacturing_input"
		overlay.WeightHint = 1.06
	case "geo_operational_exposure":
		overlay.TransmissionProfile = "direct_geo"
		overlay.WeightHint = 1.03
	}
	if fromOverlay.HasRole("bridge") {
		overlay.WeightHint += 0.05
		overlay.Roles = overlayAppendUnique(overlay.Roles, "bridge_origin")
	}
	if fromOverlay.HasRole("regulator") {
		overlay.Roles = overlayAppendUnique(overlay.Roles, "regulator_origin")
	}
	if fromOverlay.HasExposure("route_dependency") && overlay.TransmissionProfile == "route_transit" {
		overlay.WeightHint += 0.04
	}
	overlay.WeightHint = overlayClamp(overlay.WeightHint, 0.84, 1.26)
	return overlay
}

func overlayFragmentText(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	parts := []string{
		strings.ToLower(strings.TrimSpace(fragment.CanonicalPath)),
		strings.ToLower(strings.TrimSpace(fragment.MarketDomain)),
		strings.ToLower(strings.TrimSpace(fragment.MarketVertical)),
		strings.ToLower(strings.TrimSpace(fragment.GeoFeatureType)),
		strings.ToLower(strings.TrimSpace(fragment.GovType)),
		strings.ToLower(strings.TrimSpace(fragment.CommunityType)),
		strings.ToLower(strings.TrimSpace(fragment.Description)),
	}
	for _, tag := range fragment.Tags {
		parts = append(parts, strings.ToLower(strings.TrimSpace(tag)))
	}
	for _, path := range fragment.IndexPaths {
		parts = append(parts, strings.ToLower(strings.TrimSpace(path)))
	}
	for _, v := range fragment.Labels {
		parts = append(parts, strings.ToLower(strings.TrimSpace(v)))
	}
	for _, v := range fragment.Identifiers {
		parts = append(parts, strings.ToLower(strings.TrimSpace(v)))
	}
	return strings.Join(parts, " ")
}

func overlayHasAny(text string, tokens ...string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "" && strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func overlayAppendUnique(in []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return in
	}
	for _, item := range in {
		if strings.TrimSpace(item) == value {
			return in
		}
	}
	return append(in, value)
}

func overlayClamp01(v float64) float64 {
	return overlayClamp(v, 0, 1)
}

func overlayClamp(v, minValue, maxValue float64) float64 {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}

func overlayFragmentMetricValue(fragment *horizonmodels.HorizonFragment, metric string) float64 {
	if fragment == nil || fragment.State == nil {
		return 0
	}
	mv, ok := fragment.State[strings.TrimSpace(metric)]
	if !ok {
		return 0
	}
	return overlayClamp01(mv.Value)
}

func maxOverlayFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	maxResearchChainContextChars = 4000
	maxResearchEntityLineChars   = 220
	maxResearchRawContentChars   = 7000
)

type researchPromptOptions struct {
	PreferredModel   string
	MinContextWindow int
	MaxOutputSize    int
	MinCapability    int
}

// geoAndSectorContext extracts geo and sector from fragment for prompt adaptation.
// Used to instruct LLM to adapt extraction to regional/industry-specific supply chain and governance patterns.
func geoAndSectorContext(frag *models.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	var geo, sector []string
	// Geo: from canonical path (org/gov/market 2nd segment), homeLocation, or marketSegments
	cp := strings.TrimSpace(frag.CanonicalPath)
	if cp != "" {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 2 {
			g := strings.ToLower(strings.TrimSpace(parts[1]))
			if g != "" && g != "na" && len(g) >= 2 {
				geo = append(geo, g)
			}
		}
	}
	if frag.HomeLocation != nil && frag.HomeLocation.ISO3166_1 != nil {
		g := strings.ToLower(strings.TrimSpace(*frag.HomeLocation.ISO3166_1))
		if g != "" && len(g) == 2 {
			geo = append(geo, g)
		}
	}
	for _, seg := range frag.MarketSegments {
		parts := strings.Split(strings.Trim(seg, "/"), "/")
		if len(parts) >= 3 {
			g := strings.ToLower(strings.TrimSpace(parts[2]))
			if g != "" && g != "global" {
				geo = append(geo, g)
			}
		}
	}
	// Sector: from EntityPath or marketSegments (domain/vertical)
	if ep := strings.TrimSpace(frag.EntityPath); ep != "" {
		sector = append(sector, ep)
	}
	for _, seg := range frag.MarketSegments {
		parts := strings.Split(strings.Trim(seg, "/"), "/")
		if len(parts) >= 2 {
			s := strings.Join(parts[:2], "/")
			if s != "" {
				sector = append(sector, s)
			}
		}
	}
	if len(geo) == 0 && len(sector) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("ENTITY CONTEXT (adapt extraction to this company and market):\n")
	if len(geo) > 0 {
		seen := make(map[string]struct{})
		var uniq []string
		for _, g := range geo {
			if _, ok := seen[g]; !ok {
				seen[g] = struct{}{}
				uniq = append(uniq, g)
			}
		}
		sb.WriteString(fmt.Sprintf("- Geo: %s\n", strings.Join(uniq, ", ")))
	}
	if len(sector) > 0 {
		seen := make(map[string]struct{})
		var uniq []string
		for _, s := range sector {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				uniq = append(uniq, s)
			}
		}
		sb.WriteString(fmt.Sprintf("- Sector: %s\n", strings.Join(uniq, ", ")))
	}
	sb.WriteString(`- ADAPT: Use your knowledge of the company, region, and industry to infer supply chain and governance patterns.
  Examples: Chinese EV/automotive (e.g., BYD) often use vertical integration; European OEMs often rely on partnership networks.
  When in doubt, infer from entity name, sector, and geo; output qualifiers.integration_type when the relationship nature is clear.
`)
	return sb.String()
}

// researchPreferExistingEntitiesBlock returns guidance to use existing entities from grounding instead of creating duplicates.
// Applied to all research prompts to reduce duplicates from minor naming differences.
func researchPreferExistingEntitiesBlock() string {
	return `PREFER EXISTING ENTITIES (avoid duplicates):
Your search has access to existing entities in the knowledge base (grounding/datastore results).
When you find an entity that matches an existing one — even with minor naming differences (e.g., "Tesla Inc" vs "Tesla Inc.", "JPMorgan" vs "JPMorgan Chase", "Ministry of Finance" vs "MOF", abbreviations vs full names) — use the EXISTING canonical path from the grounding context.
Do NOT create duplicate entities. Only create new entities when no suitable existing match is found in your search results.

DIRECTION ROLE CONTRACT (mandatory when applicable):
- For OWNS/SUBSIDIARY_OF/INVESTS_IN/ASSURES/CONTROLS (ORG↔ORG), DEPENDS_ON (actor↔actor), and SUPPLIES_TO (ORG/FACILITY↔ORG/FACILITY), ALWAYS include qualifiers.source_role and qualifiers.target_role.
- Use role pairs: owner->owned_entity, subsidiary->parent_owner, investor->investee, assurer->assuree, controller->controlled_entity, dependent->dependency, supplier->customer.
- Also include qualifiers.direction_basis when direction is explicit (owner_to_owned, investor_to_investee, dependent_to_dependency, supplier_to_customer).
- For policy-impact core edges, also keep directed-role qualifiers explicit:
  gatekeeper->decision_point for GATEKEEPS_DECISION,
  mechanism_user->mechanism for USES_MECHANISM,
  controller->access_subject for CONTROLS_ACCESS,
  imposer->constrained_entity for IMPOSES_CONDITION,
  mechanism->applicable_entity for APPLIES_TO,
  administrator->administered_entity for ADMINISTERS,
  scope_owner->geo_scope for IN_GEO_SCOPE,
  impact_source->affected_entity for AFFECTS.
- If direction is unclear, omit the relation instead of guessing.

ZONE GEO CONTRACT:
- In /zone/{geo}/{condition_type}/{slug}, geo: ` + taxonomy.ISO2GeoDescriptionForPrompts() + `
- Sea-area zones are valid and must use geo=sea (example: /zone/sea/political_tension/spratly-islands).
- Do not invent natural-feature geo tokens like ocean/gulf/strait for {geo}; keep specific place naming in slug.
`
}

func strategySupplyChainMetricsVocabularyBlock() string {
	return `SUPPLY CHAIN METRICS (use ONLY these units):
volume: gwh_per_year | metric_tons_per_year | kt_per_year | bpd | mbpd | mcf_per_day | units_per_year | cells
share: percent (0-100) | ratio (0-1)
contract_value: usd | eur | rub
lead_time_days: days
on_time_delivery: ratio (0-1)
inventory_cover_days_at_destination: days
customs_clearance_days: days
switch_time_days: days
technical_fit: ratio (0-1)
regulatory_fit: ratio (0-1)
fill_rate: ratio (0-1)
rejection_rate: ratio (0-1)

Format per segment: { "volume": { "value": 15, "unit": "gwh_per_year", "confidence": 0.8 }, "share": { "value": 40, "unit": "percent", "confidence": 0.7 } }
Choose unit by product type: batteries/energy → gwh; ores/chemicals → metric_tons; oil/gas → bbl/bcf; discrete → units/cells.
- When the flow is cross-border, sanctions-sensitive, export-control-sensitive, import-constrained, or customs-sensitive, prefer including customs_clearance_days, regulatory_fit, inventory_cover_days_at_destination, and switch_time_days when evidence exists.
- For linkObservations and routeObservations, also use qualifiers for control/access gates when evidence exists:
  - export_license_required
  - sanctions_touchpoint
  - approved_vendor_only
  - service_contract_required
  - dual_use_flag
`
}

func strategyCapabilityMetricWhitelistBlock() string {
	return `REUSABLE CAPABILITY METRICS (carrier × product baseline):
- When evidence exists, carrierProductObservations are EXPECTED, not optional.
- Use carrierProductObservations for reusable actor×product baseline facts that can apply across many customer links.
- Prefer these reusable metric keys when supported by evidence:
  - annual_capacity
  - annual_output
  - lead_time_days_baseline
  - lead_time_volatility_days
  - inventory_days_baseline
  - on_time_delivery_baseline
  - technical_substitutability
  - regulatory_substitutability
  - service_criticality
  - quality_score
  - purity_percent
  - utilization_rate
  - available_export_share
- Example:
  {
    "carrierRef": "/fac/qa/ras-laffan-lng-complex",
    "productRef": "/product/raw/hydrocarbon/liquefaction/lng",
    "metrics": {
      "annual_capacity": { "value": 77, "unit": "metric_tons_per_year", "confidence": 0.82 },
      "lead_time_days_baseline": { "value": 18, "unit": "days", "confidence": 0.63 },
      "on_time_delivery_baseline": { "value": 0.92, "unit": "ratio", "confidence": 0.58 }
    },
    "qualifiers": {
      "evidenceClass": "reported_capacity",
      "carrierProductRole": "export_supplier"
    },
    "confidence": 0.73
  }
- Do NOT place reusable baseline metrics in linkObservations unless they are explicitly customer-, destination-, or contract-specific.
- If evidence is only link-specific, put it in linkObservations. If evidence is only route-specific, put it in routeObservations.
`
}

func strategyServiceChainMetricsVocabularyBlock() string {
	return `SERVICE CHAIN METRICS (use ONLY concrete operational units when evidence exists):
capacity: requests_per_second | gbps | racks | mw | tenants | seats | sites | tickets_per_month | units_per_year
share: percent (0-100) | ratio (0-1)
latency: milliseconds | seconds | minutes | days
availability: ratio (0-1) | percent (0-100)
recovery: minutes | hours | days
throughput: requests_per_second | gbps | tickets_per_month | units_per_year
utilization: ratio (0-1) | percent (0-100)

Prefer service-specific metrics when supported by evidence:
- availability_slo
- p95_latency_ms
- provisioning_time_days
- failover_time_minutes
- recovery_time_hours
- support_response_time_hours
- incident_rate
- capacity_headroom
- tenant_concentration
- auth_success_ratio

Control / gate qualifiers to prefer when evidence exists:
- auth_required
- approved_vendor_only
- service_contract_required
- region_lock_in
- identity_provider_required
- export_license_required
- sanctions_touchpoint
- dual_use_flag
`
}

func strategyServiceCapabilityMetricWhitelistBlock() string {
	return `REUSABLE SERVICE CAPABILITY METRICS (provider × capability baseline):
- When evidence exists, carrierProductObservations are EXPECTED, not optional.
- Use carrierProductObservations for reusable provider×capability baseline facts that can apply across many customer links.
- Prefer these reusable metric keys when supported by evidence:
  - installed_capacity
  - available_capacity
  - capacity_headroom
  - p95_latency_ms
  - availability_slo
  - failover_time_minutes
  - recovery_time_hours
  - provisioning_time_days
  - support_response_time_hours
  - support_coverage_ratio
  - tenant_concentration
  - auth_success_ratio
  - region_lock_in
  - vendor_lock_in_score
  - service_criticality
- Example:
  {
    "carrierRef": "/fac/us/us-west4-data-center",
    "productRef": "/product/service/cloud/compute/virtual-machines",
    "metrics": {
      "available_capacity": { "value": 12000, "unit": "tenants", "confidence": 0.71 },
      "availability_slo": { "value": 0.999, "unit": "ratio", "confidence": 0.84 },
      "p95_latency_ms": { "value": 42, "unit": "milliseconds", "confidence": 0.58 }
    },
    "qualifiers": {
      "carrierProductRole": "regional_provider",
      "region_lock_in": true
    },
    "confidence": 0.74
  }
- Do NOT place reusable baseline metrics in linkObservations unless they are explicitly customer-, destination-, or contract-specific.
- If evidence is only link-specific, put it in linkObservations. If evidence is only route-specific or control-path-specific, put it in routeObservations.
`
}

func strategyMetricValueFormatBlock() string {
	return `METRIC VALUE FORMAT (STRICT):
- For fragment.state and relations[].metrics, each metric entry MUST be either:
  1) scalar number / numeric string (e.g., 0.065, "6.5%", "10M"), OR
  2) MetricValue object: { "value": <number>, "unit"?: "...", "confidence"?: 0..1, "observedAt"?: "YYYY-MM-DD|RFC3339" }.
- value MUST be numeric (JSON number after expansion). Never place narrative text in metric values.
- Put explanations in "reason" or "fragment.description", not in metrics.
- INVALID metric example:
  "market_share_percent": "Estimated ~6.5% share of total food retail ... 30%+ of modern retail segment."
- VALID metric example:
  "market_share_percent": { "value": 6.5, "unit": "percent", "confidence": 0.7 }
`
}

const supplyChainOperationalModelBlock = `CRITICAL — Supply chain operational model (org→fac→product→fac→org):
1. Full chain: ORGANIZATION → its FACILITIES → PRODUCTS they produce → receiving FACILITIES → their ORGANIZATIONS.
2. Chain nodes use their real types: /org/ for organizations, /fac/ for facilities, /product/ for products.
3. When a specific production facility is unknown or not separable (small org / org-level production only), use ORG → PRODUCES → PRODUCT directly.
4. Product = catalog entity. One product type, one node. Do not duplicate products for transport steps. Use transportVia array on the supplies edge. A product must NOT appear more than once in a single supply chain path — no pseudo-cycles.
5. Use only PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN, EXTRACTS_FROM.
6. For long-haul, cross-border, maritime, pipeline, rail, or chokepoint-sensitive supply legs, include transportVia with real facility/chokepoint canonical paths whenever evidence or geography makes the route reasonably inferable.
7. Prefer canonical ports, terminals, pipelines, straits, canals, warehouses, and intermodal nodes in transportVia over vague words like "shipping", "logistics", or "distribution".
8. Only product-carrying flow belongs in the chain. If an organization merely owns, controls, manages, or operates an adjacent facility, emit OWNS / CONTROLS / MANAGES separately. Do NOT encode that as FACILITY → PRODUCT → ORG.
9. Physical relay hubs (bonded warehouses, customs terminals, inland terminals, depots, service depots, seaport/airport terminals) may appear as operational facilities or in transportVia when they materially gate the flow.
10. Non-product-carrying control points (screening, licensing, settlement, insurance, service authorization) must NOT become fake product nodes. Capture them via observations/qualifiers or concrete actor relations when grounded.`

const supplyChainProductGranularityBlock = `PRODUCT GRANULARITY (what counts as a product node):
- Tech/consumer products: use MODEL LINE / product family level. NOT specific SKU or version.
  - Correct: iPhone, MacBook, Claude, Claude Code (model line for tech). Wrong: iPhone 17, MacBook Pro M4, Claude Opus.
  - One product node = one model line. Avoid version/year suffixes (iPhone 17, Tesla Model Y 2024).
- Natural resources, ores, chemicals: follow existing taxonomy (/product/raw/mineral/..., /product/finished/...). Keep appropriate granularity (lithium-carbonate, copper-anode) — do NOT over-generalize to "minerals" or "chemicals".
- When in doubt: prefer product family over specific SKU; prefer concrete commodity over vague category.`

const supplyChainSpecialScenariosBlock = `SPECIAL SCENARIOS:
- Contract manufacturing: org owns IP; facility (contract manufacturer) produces. Link: fac_produces_product.
- Toll manufacturing: raw from fac A, processing at fac B, result to A or C. Segments: fac_A→product→fac_B→product→fac_C.
- Distributors/resellers: fac with type distribution/sales; supplies without produces.
- Software vendors: prefer concrete production/delivery facilities when known; if production is org-level and facilities are not separable, model as org→product.
- Co-product/by-product: one process (fac)→multiple product. Multiple PRODUCES edges from one fac.
- Sanctions/customs/export-control flows: explicitly look for relay hubs, bonded/customs terminals, reroute corridors, and execution gates before concluding the route is single-hub.`

// holisticOutputSchemaBlock returns the full JSON schema for holistic/refine prompts (fragment partial merge + relations).
func holisticOutputSchemaBlock() string {
	return `Return JSON object (STRICT SCHEMA):
{
  "fragment": {
    "description"?: "string (max 500 chars)",
    "state"?: { "/metric/...": { "value": number, "confidence": 0..1, "observedAt": "RFC3339", "provenance": { "sourceIds": ["url"], "method": "llm_metric_inference" } } },
    "homeLocation"?: { "name": "string", "iso3166_1": "ISO2" },
    "qualifiers"?: { "key": "value" },
    "identifiers"?: { "legalName"?: "string", "shortName"?: "string" },
    "labels"?: { "en": "string", ... },
    "nameHistory"?: [ { "name": "string", "kind": "short|legal|brand|aka", "validFrom"?: "YYYY-MM-DD", "validTo"?: "YYYY-MM-DD" } ]
  },
  "relations": [
    {
      "targetName": "string (REQUIRED)",
      "targetType": "ORGANIZATION|FACILITY|PRODUCT|GEO_REGION|... (REQUIRED)",
      "targetCanonicalPath": "string (REQUIRED, canonical path)",
      "relationType": "string (REQUIRED)",
      "reason": "string",
      "confidence": 0.0,
      "sourceCanonicalPath"?: "string (when this fragment is semantic target)",
      "sourceType"?: "string",
      "sourceName"?: "string",
      "qualifiers"?: { "key": "value" }
    }
  ]
}`
}

const supplyChainMandatoryBlock = `MANDATORY — SUPPLY CHAIN RULES (NO EXCEPTIONS):
- Preferred chain model: org → fac → product → fac → org. Organizations at the ends, facilities in the middle, products between facilities.
- Fallback model (only when concrete production facility is unknown or not separable): org → product.
- Nodes: /org/ for organizations, /fac/ for facilities, /product/ for products, /deposit/ for deposits.
- Edge types: PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN, EXTRACTS_FROM.
- Product: one per type in path. No duplicates. No "Freight" or "delivery" as product unless that IS the facility's service.
- If you cannot satisfy these rules, omit the relation. Do NOT output invalid links.`

const serviceChainOperationalModelBlock = `CRITICAL — Service chain operational model (org→fac→service→fac→org):
1. Full chain: ORGANIZATION → delivery FACILITIES / service sites → SERVICE/CAPABILITY nodes → recipient FACILITIES / customer sites → their ORGANIZATIONS.
2. Service/capability nodes still use /product/service/... canonical product paths. Do not invent a parallel service identity namespace in prompts.
3. Chain nodes use their real types: /org/ for organizations, /fac/ for delivery sites / data centers / repair depots / network hubs, /product/service/... for delivered capabilities.
4. When a concrete delivery facility is unknown or not separable, ORG → PRODUCES → /product/service/... is allowed as a fallback for the provider-level capability.
5. Keep one capability per node. Do not duplicate the same service node for control-plane, support, or route details.
6. Physical delivery sites, network hubs, repair depots, bonded warehouses, terminals, ports, and customs relay points may appear as facilities or in transportVia when they materially gate delivery.
7. Non-service-carrying control points (authorization, identity/access, licensing, settlement, insurer approval, approved-vendor gate, service contract gate) must NOT become fake service nodes. Capture them via observations/qualifiers or concrete actor relations when grounded.
8. Prefer concrete operational facilities over abstract business-unit prose. A service chain should tell where capability is created, routed, controlled, or delivered.
9. Support / aftermarket / managed-service branches are valid when they materially affect the delivered capability path, but they must not crowd out the main delivery path.
10. Use only PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN for topology; use observations / qualifiers for service-control gates.`

const serviceChainProductGranularityBlock = `SERVICE / CAPABILITY GRANULARITY:
- Service nodes MUST use canonical /product/service/... paths. The service identity still lives inside the canonical product plane.
- Prefer capability families or operational service lines over marketing package names or contract nicknames.
- Good examples: cloud compute, colocation, managed SOC, spare-parts maintenance, field-service support, satellite connectivity, payment acquiring.
- Bad examples: generic "operations", vague "digital transformation", one-off contract titles, raw ticket numbers, or internal workstream names.
- When in doubt: prefer the concrete capability a downstream customer actually receives or depends on.`

const serviceChainSpecialScenariosBlock = `SPECIAL SERVICE SCENARIOS:
- Digital infrastructure / cloud: explicitly look for provider regions, data centers, colocation sites, power/network dependencies, landing stations, exchange points, and downstream enterprise/customer exposure.
- Field service / aftermarket: think in terms of parts manufacturing, authorized suppliers, repair depots, service hubs, outage support teams, and customer plant/site recipients.
- Managed service / reseller channel: separate core provider, managed-service operator, reseller/distributor, and final service-consuming customer when evidence supports it.
- Network / telecom / connectivity: include concrete backbone hubs, cable landing stations, IXPs, spectrum or access-control gates, and last-mile delivery sites when they materially gate delivery.
- Service authorization / approved-vendor cases: treat authorization, approved-vendor status, service contracts, or identity/access gates as control overlays or observations, not fake product nodes.`

const serviceChainMandatoryBlock = `MANDATORY — SERVICE CHAIN RULES (NO EXCEPTIONS):
- Preferred chain model: org → fac → /product/service/... → fac → org. Organizations at the ends, delivery sites in the middle, service/capability nodes between actors.
- Fallback model (only when concrete delivery facility is unknown or not separable): org → /product/service/...
- Nodes: /org/ for organizations, /fac/ for service delivery sites, /product/ for canonical capability/service paths.
- Edge types: PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN.
- One capability per node. Do not turn authorization, licensing, settlement, identity, insurance, or vendor-approval gates into service nodes.
- If you cannot satisfy these rules, omit the relation. Do NOT output invalid links.`

func strategySupplyChainsJSONContractBlock() string {
	return `SUPPLY CHAINS JSON CONTRACT (STRICT):
- "supplyChains" MUST be an array of objects.
- Each chain object MUST have:
  - "nodes": REQUIRED array of objects (NOT string and NOT shorthand)
  - "confidence": REQUIRED number in 0..1
  - "reason": REQUIRED short evidence string
  - "transportVia": OPTIONAL array of facility canonical paths (/fac/...)
  - "segmentMetrics": OPTIONAL array of { fromNodeIndex, toNodeIndex, productIndex, volume: { value, unit, confidence }, share, period, confidence }
  - "productCandidates": OPTIONAL array of { canonicalPath?, name, reason, confidence } for genuinely new job-scope products only
  - "carrierProductObservations": EXPECTED when evidence supports reusable actor×product facts; array of { carrierRef, productRef, metrics?, qualifiers?, confidence, observedAt? }
  - "linkObservations": OPTIONAL array of { supplierRef?, customerRef?, productRef, metrics?, qualifiers?, confidence, observedAt? }
  - "routeObservations": OPTIONAL array of { supplierRef?, customerRef?, productRef?, transportVia?, vesselType?, qualifiers?, confidence, observedAt? }
  - (no "alternatives" field — model is org→fac→product→fac→org trees, not alternative suppliers)
- Node object schema (each nodes[i]):
  - "canonicalPath": REQUIRED string
  - "name": REQUIRED string
  - "type": REQUIRED enum: ORGANIZATION|FACILITY|PRODUCT|DEPOSIT
  - "facilityType": OPTIONAL supply chain role: ` + taxonomy.FacilityRolesForPrompts() + `
  - "facilityKind": OPTIONAL physical type: ` + taxonomy.FacilityKindsForPrompts() + ` (seaport, factory, cable, pipeline, mine, etc.)
  - "position": OPTIONAL (upstream|midstream|downstream)
  - "role": OPTIONAL string
- Use /org/ for organizations, /fac/ for facilities, /product/ for products, /deposit/ for deposits. Do NOT invent synthetic /fac/*-general-production placeholders. If production is only known at org level, use org→product.
- transportVia is operationally important. When the leg is cross-border or route-sensitive and a plausible business route is known, do not leave transportVia empty.
- When multiple credible relay hubs exist, prefer returning parallel chains or routeObservations instead of collapsing everything into a single dominant hub.
- productCandidates are scope additions, not duplicates of nodes[]. Use them only when the current working product set needs extension.
- carrierProductObservations must be reusable actor×product facts and should be present when evidence supports reusable baseline metrics; linkObservations must be customer/destination specific; routeObservations must be transport/path specific.
- For sanctions-/export-control-/customs-sensitive cross-border flows, prefer linkObservations or routeObservations with:
  - metrics like customs_clearance_days, lead_time_days, inventory_cover_days_at_destination, regulatory_fit, switch_time_days
  - qualifiers like export_license_required, sanctions_touchpoint, approved_vendor_only, service_contract_required, dual_use_flag
- For gate-controlled flows, do not leave control points implicit. Capture them either as:
  - concrete relay facilities in transportVia / routeObservations when they are physical sites (bonded warehouse, seaport terminal, customs terminal, inland terminal, service depot), or
  - canonical relations / observations when they are organizations or authorities (screening office, licensing authority, settlement bank, trade-credit insurer, service-authorization gate).
- Good transportVia examples: /fac/sea/strait-of-hormuz-terminal, /fac/eg/suez-terminal, /fac/nl/port-of-rotterdam, /fac/ae/jebel-ali-terminal.
- Bad transportVia examples: generic words, regions, countries, products, or non-facility placeholders.
- INVALID shape example: "nodes": "A -> B -> C"
- VALID shape example (org→fac→product→fac→org):
{
  "supplyChains": [{
    "nodes": [
      { "canonicalPath": "/org/us/albemarle-corp", "name": "Albemarle Corporation", "type": "ORGANIZATION" },
      { "canonicalPath": "/fac/us/silver-peak", "name": "Silver Peak Lithium", "type": "FACILITY", "facilityType": "extraction", "facilityKind": "mine", "position": "upstream" },
      { "canonicalPath": "/product/raw/mineral/refining/lithium-carbonate", "name": "Lithium carbonate", "type": "PRODUCT" },
      { "canonicalPath": "/fac/cn/catl-ningde", "name": "CATL Ningde", "type": "FACILITY", "facilityType": "manufacturing", "facilityKind": "factory", "position": "downstream" },
      { "canonicalPath": "/org/cn/catl", "name": "CATL", "type": "ORGANIZATION" }
    ],
    "confidence": 0.85,
    "reason": "Industry reports and filings.",
    "transportVia": ["/fac/us/port-long-beach"],
    "segmentMetrics": [{ "fromNodeIndex": 1, "toNodeIndex": 3, "productIndex": 2, "volume": { "value": 10, "unit": "kt_per_year", "confidence": 0.7 }, "period": "2023" }]
  }]
}`
}

func strategyServiceChainsJSONContractBlock() string {
	return `SERVICE CHAINS JSON CONTRACT (STRICT):
- "supplyChains" remains the payload field for compatibility, but each chain represents a SERVICE / CAPABILITY path.
- Each chain object MUST have:
  - "nodes": REQUIRED array of objects (NOT string and NOT shorthand)
  - "confidence": REQUIRED number in 0..1
  - "reason": REQUIRED short evidence string
  - "transportVia": OPTIONAL array of facility canonical paths (/fac/...)
  - "segmentMetrics": OPTIONAL array of { fromNodeIndex, toNodeIndex, productIndex, volume: { value, unit, confidence }, share, period, confidence }
  - "productCandidates": OPTIONAL array of { canonicalPath?, name, reason, confidence } for genuinely new service/capability scope additions only
  - "carrierProductObservations": EXPECTED when evidence supports reusable provider×capability facts; array of { carrierRef, productRef, metrics?, qualifiers?, confidence, observedAt? }
  - "linkObservations": OPTIONAL array of { supplierRef?, customerRef?, productRef, metrics?, qualifiers?, confidence, observedAt? }
  - "routeObservations": OPTIONAL array of { supplierRef?, customerRef?, productRef?, transportVia?, vesselType?, qualifiers?, confidence, observedAt? }
- Node object schema (each nodes[i]):
  - "canonicalPath": REQUIRED string
  - "name": REQUIRED string
  - "type": REQUIRED enum: ORGANIZATION|FACILITY|PRODUCT|DEPOSIT
  - "facilityType": OPTIONAL service/delivery role: ` + taxonomy.FacilityRolesForPrompts() + `
  - "facilityKind": OPTIONAL physical type: ` + taxonomy.FacilityKindsForPrompts() + `
  - "position": OPTIONAL (upstream|midstream|downstream)
  - "role": OPTIONAL string
- Service/capability nodes MUST use canonical /product/service/... paths when the node is a service.
- transportVia is valid not only for freight but also for operational relay sites that materially gate service delivery.
- carrierProductObservations must be reusable provider×capability facts; linkObservations must be customer/destination specific; routeObservations must be transport/path/control-point specific.
- For gate-controlled service flows, do not leave control points implicit. Capture them either as:
  - concrete facilities in transportVia / routeObservations when they are physical sites (data center, landing station, depot, customs terminal, service hub), or
  - concrete actor relations / observations when they are organizations or authorities (identity provider, licensing authority, trade-credit insurer, service-authorization gate, approved-vendor gate).
- INVALID shape example: "nodes": "Provider -> Service -> Customer"
- VALID shape example (org→fac→service→fac→org):
{
  "supplyChains": [{
    "nodes": [
      { "canonicalPath": "/org/us/google-cloud", "name": "Google Cloud", "type": "ORGANIZATION" },
      { "canonicalPath": "/fac/us/us-west4-data-center", "name": "Bay Area Data Center", "type": "FACILITY", "facilityType": "operations", "facilityKind": "data_center", "position": "upstream" },
      { "canonicalPath": "/product/service/cloud/compute/virtual-machines", "name": "Cloud compute", "type": "PRODUCT" },
      { "canonicalPath": "/fac/us/customer-colo-bay-area", "name": "Customer colocation site", "type": "FACILITY", "facilityType": "operations", "facilityKind": "colocation", "position": "downstream" },
      { "canonicalPath": "/org/us/example-enterprise", "name": "Example Enterprise", "type": "ORGANIZATION" }
    ],
    "confidence": 0.85,
    "reason": "Provider-region and customer deployment evidence.",
    "transportVia": ["/fac/us/san-jose-network-hub"]
  }]
}`
}

// SupplyChainResearchStrategy enriches for supply chain only (PRODUCES, SUPPLIES_TO, etc.).
type SupplyChainResearchStrategy struct {
	agent *ResearchAgent
}

func (s *SupplyChainResearchStrategy) DomainMode() string { return taxonomy.DomainModeSupplyChain }

func (s *SupplyChainResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("supply-chain strategy is not configured")
	}
	prompt := s.agent.buildSupplyChainEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// ServiceChainResearchStrategy enriches service delivery lifecycle chains.
type ServiceChainResearchStrategy struct {
	agent *ResearchAgent
}

func (s *ServiceChainResearchStrategy) DomainMode() string { return taxonomy.DomainModeServiceChain }

func (s *ServiceChainResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("service-chain strategy is not configured")
	}
	prompt := s.agent.buildServiceChainEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// GovernanceResearchStrategy enriches for ownership, board, key persons only.
type GovernanceResearchStrategy struct {
	agent *ResearchAgent
}

func (s *GovernanceResearchStrategy) DomainMode() string { return taxonomy.DomainModeGovernance }

func (s *GovernanceResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("governance strategy is not configured")
	}
	prompt := s.agent.buildGovernanceEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// AssuranceResearchStrategy enriches for Third Line: audit, certification, regulatory oversight.
type AssuranceResearchStrategy struct {
	agent *ResearchAgent
}

func (s *AssuranceResearchStrategy) DomainMode() string { return taxonomy.DomainModeAssurance }

func (s *AssuranceResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("assurance strategy is not configured")
	}
	prompt := s.agent.buildAssuranceEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// MarketResearchStrategy enriches for market participation only (PARTICIPATES_IN, HAS_MARKET_SEGMENT).
type MarketResearchStrategy struct {
	agent *ResearchAgent
}

func (s *MarketResearchStrategy) DomainMode() string { return taxonomy.DomainModeMarket }

func (s *MarketResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("market strategy is not configured")
	}
	prompt := s.agent.buildMarketEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// StrategicResearchStrategy enriches for strategic layer (CONTROLS_ACCESS, USES_MECHANISM, etc.).
type StrategicResearchStrategy struct {
	agent *ResearchAgent
}

func (s *StrategicResearchStrategy) DomainMode() string { return taxonomy.DomainModeStrategic }

func (s *StrategicResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("strategic strategy is not configured")
	}
	prompt := s.agent.buildStrategicEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// PolicyImpactResearchStrategy enriches policy impact channels (regulatory instrument -> segment -> impact).
type PolicyImpactResearchStrategy struct {
	agent *ResearchAgent
}

func (s *PolicyImpactResearchStrategy) DomainMode() string { return taxonomy.DomainModePolicyImpact }

func (s *PolicyImpactResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("policy-impact strategy is not configured")
	}
	prompt := s.agent.buildPolicyImpactEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// EcosystemResearchStrategy enriches for ecosystem/stakeholder mapping (multi-actor relationships).
type EcosystemResearchStrategy struct {
	agent *ResearchAgent
}

func (s *EcosystemResearchStrategy) DomainMode() string { return taxonomy.DomainModeEcosystem }

func (s *EcosystemResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("ecosystem strategy is not configured")
	}
	prompt := s.agent.buildEcosystemEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// GeneralResearchStrategy enriches in broad mode with fragment-type specialization.
type GeneralResearchStrategy struct {
	agent *ResearchAgent
}

func (s *GeneralResearchStrategy) DomainMode() string { return taxonomy.DomainModeGeneral }

func (s *GeneralResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("general strategy is not configured")
	}
	prompt := s.agent.buildGeneralEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// runResearchPrompt runs a research prompt and parses result into ResearchStrategyOutput.
// Returns grounding evidence from Vertex AI Search when available.
// inputFragment is used as fallback when LLM omits fragment or returns invalid structure.
func (a *ResearchAgent) runResearchPrompt(ctx context.Context, prompt string, inputFragment *models.HorizonFragment) (*ResearchStrategyOutput, error) {
	return a.runResearchPromptWithOptions(ctx, prompt, inputFragment, researchPromptOptions{})
}

func (a *ResearchAgent) runResearchPromptWithOptions(ctx context.Context, prompt string, inputFragment *models.HorizonFragment, opts researchPromptOptions) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor is not configured")
	}
	minContextWindow := opts.MinContextWindow
	if minContextWindow <= 0 {
		minContextWindow = 16000
	}
	maxOutputSize := opts.MaxOutputSize
	if maxOutputSize <= 0 {
		maxOutputSize = 16000
	}
	minCapability := opts.MinCapability
	if minCapability <= 0 {
		minCapability = 7
	}
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:                  strings.TrimSpace(opts.PreferredModel),
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              minCapability,
			MinContextWindow:                minContextWindow,
			MaxOutputSize:                   maxOutputSize,
		},
		Verifier: verifier,
	}
	result, fullResult, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("research prompt: %w", err)
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	// Always sanitize before unmarshal: the sanitizer normalizes field names
	// (e.g. LLM uses "participants" instead of "nodes"), coerces types, and
	// filters broken entries. Without this, json.Unmarshal silently produces
	// zero-value SupplyChain structs when field names don't match tags.
	sanitized, sanitizeErr := sanitizeResearchResultForUnmarshal(jsonData)
	unmarshalPath := "sanitized"
	unmarshalInput := sanitized
	if sanitizeErr != nil {
		unmarshalPath = "direct"
		unmarshalInput = jsonData
	}
	var res researchResult
	if err := json.Unmarshal(unmarshalInput, &res); err != nil {
		if unmarshalPath == "sanitized" {
			// Sanitized input failed too — try raw as last resort
			if err2 := json.Unmarshal(jsonData, &res); err2 != nil {
				return nil, fmt.Errorf("unmarshal research result: %w (sanitize: %v)", err2, sanitizeErr)
			}
			unmarshalPath = "direct_fallback"
		} else {
			return nil, fmt.Errorf("unmarshal research result: %w", err)
		}
	}

	// Diagnostic: log chain quality after unmarshal (before repair)
	for i, sc := range res.SupplyChains {
		nodesWithCP := 0
		for _, n := range sc.Nodes {
			if strings.TrimSpace(n.CanonicalPath) != "" {
				nodesWithCP++
			}
		}
		log.Printf("[ResearchParse] chain[%d]: nodesTotal=%d nodesWithCP=%d confidence=%.2f unmarshalPath=%s jsonBytes=%d",
			i, len(sc.Nodes), nodesWithCP, sc.Confidence, unmarshalPath, len(jsonData))
	}
	if len(res.SupplyChains) == 0 {
		rawWhat := logRawSupplyChainsFromJSON(jsonData)
		log.Printf("[ResearchParse] 0 supply chains in response unmarshalPath=%s jsonBytes=%d raw=%s", unmarshalPath, len(jsonData), rawWhat)
	}
	repairSupplyChainNodePaths(res.SupplyChains)
	out := &ResearchStrategyOutput{
		Relations:         res.Relations,
		SupplyChains:      res.SupplyChains,
		GroundingEvidence: nil,
	}
	if fullResult != nil {
		out.GroundingEvidence = fullResult.GroundingEvidence
		out.RawContent = compactResearchRawContent(fullResult.Content)
	}
	if res.Fragment != nil {
		fragBytes, _ := json.Marshal(res.Fragment)
		var partial models.HorizonFragment
		if err := json.Unmarshal(fragBytes, &partial); err == nil {
			out.FragmentPartial = &partial
		}
	}
	// When LLM omits fragment or returns invalid structure, preserve input fragment so we don't lose data.
	if out.FragmentPartial == nil && inputFragment != nil {
		out.FragmentPartial = inputFragment
	}
	normalizeDiscoveredRelationsNames(out.Relations)
	return out, nil
}

func (a *ResearchAgent) runSupplyChainScopedObservationPrompt(ctx context.Context, prompt string, opts researchPromptOptions) (*SupplyChainScopedObservations, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor is not configured")
	}
	minContextWindow := opts.MinContextWindow
	if minContextWindow <= 0 {
		minContextWindow = 12000
	}
	maxOutputSize := opts.MaxOutputSize
	if maxOutputSize <= 0 {
		maxOutputSize = 12000
	}
	minCapability := opts.MinCapability
	if minCapability <= 0 {
		minCapability = 7
	}
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:                  strings.TrimSpace(opts.PreferredModel),
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              minCapability,
			MinContextWindow:                minContextWindow,
			MaxOutputSize:                   maxOutputSize,
		},
		Verifier: verifier,
	}
	result, _, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("supply-chain scoped observation prompt: %w", err)
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	sanitized, sanitizeErr := sanitizeSupplyChainScopedObservationBundle(jsonData)
	unmarshalInput := sanitized
	if sanitizeErr != nil {
		unmarshalInput = jsonData
	}
	var bundle SupplyChainScopedObservations
	if err := json.Unmarshal(unmarshalInput, &bundle); err != nil {
		return nil, err
	}
	if len(bundle.ProductCandidates) == 0 &&
		len(bundle.CarrierProductObservations) == 0 &&
		len(bundle.LinkObservations) == 0 &&
		len(bundle.RouteObservations) == 0 {
		return nil, nil
	}
	return &bundle, nil
}

func (a *ResearchAgent) runSupplyChainPreHotPathPrompt(ctx context.Context, prompt string, opts researchPromptOptions) (*SupplyChainPreHotPathPlan, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor is not configured")
	}
	minContextWindow := opts.MinContextWindow
	if minContextWindow <= 0 {
		minContextWindow = 8000
	}
	maxOutputSize := opts.MaxOutputSize
	if maxOutputSize <= 0 {
		maxOutputSize = 4000
	}
	minCapability := opts.MinCapability
	if minCapability <= 0 {
		minCapability = 7
	}
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:                  strings.TrimSpace(opts.PreferredModel),
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              minCapability,
			MinContextWindow:                minContextWindow,
			MaxOutputSize:                   maxOutputSize,
		},
		Verifier: verifier,
	}
	result, _, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("supply-chain pre-hot-path prompt: %w", err)
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	sanitized, sanitizeErr := sanitizeSupplyChainPreHotPathPlan(jsonData)
	unmarshalInput := sanitized
	if sanitizeErr != nil {
		unmarshalInput = jsonData
	}
	var plan SupplyChainPreHotPathPlan
	if err := json.Unmarshal(unmarshalInput, &plan); err != nil {
		return nil, err
	}
	plan.Strategy = strings.TrimSpace(plan.Strategy)
	plan.StrategyReason = strings.TrimSpace(plan.StrategyReason)
	plan.CandidateAnchorTerms = dedupStrings(plan.CandidateAnchorTerms, 6)
	plan.CandidateProductFamilies = dedupStrings(plan.CandidateProductFamilies, 6)
	plan.CandidateOperationalAnchorClasses = dedupStrings(plan.CandidateOperationalAnchorClasses, 8)
	plan.CandidateFlowArchetypes = dedupStrings(plan.CandidateFlowArchetypes, 8)
	plan.CandidateTransitHubTerms = dedupStrings(plan.CandidateTransitHubTerms, 8)
	plan.CandidateGateTerms = dedupStrings(plan.CandidateGateTerms, 8)
	plan.CandidateRouteGeographies = dedupStrings(plan.CandidateRouteGeographies, 8)
	plan.SearchFocus = dedupStrings(plan.SearchFocus, 8)
	plan.PreferredEvidence = dedupStrings(plan.PreferredEvidence, 8)
	plan.DerivedProductFilters = dedupStrings(plan.DerivedProductFilters, 4)
	if plan.Confidence < 0 {
		plan.Confidence = 0
	}
	if plan.Confidence > 1 {
		plan.Confidence = 1
	}
	if plan.Strategy == "" &&
		len(plan.CandidateAnchorTerms) == 0 &&
		len(plan.CandidateProductFamilies) == 0 &&
		len(plan.CandidateOperationalAnchorClasses) == 0 &&
		len(plan.CandidateFlowArchetypes) == 0 &&
		len(plan.CandidateTransitHubTerms) == 0 &&
		len(plan.CandidateGateTerms) == 0 &&
		len(plan.CandidateRouteGeographies) == 0 &&
		len(plan.SearchFocus) == 0 &&
		len(plan.DerivedProductFilters) == 0 {
		return nil, nil
	}
	return &plan, nil
}

func sanitizeSupplyChainPreHotPathPlan(data []byte) ([]byte, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	ensureStringField(root, "strategy")
	ensureStringField(root, "strategyReason")
	coerceFloatField(root, "confidence", 0.5)
	sanitizeStringSliceField(root, "candidateAnchorTerms")
	sanitizeStringSliceField(root, "candidateProductFamilies")
	sanitizeStringSliceField(root, "candidateOperationalAnchorClasses")
	sanitizeStringSliceField(root, "candidateFlowArchetypes")
	sanitizeStringSliceField(root, "candidateTransitHubTerms")
	sanitizeStringSliceField(root, "candidateGateTerms")
	sanitizeStringSliceField(root, "candidateRouteGeographies")
	sanitizeStringSliceField(root, "searchFocus")
	sanitizeStringSliceField(root, "preferredEvidence")
	sanitizeStringSliceField(root, "derivedProductFilters")
	return json.Marshal(root)
}

func sanitizeSupplyChainScopedObservationBundle(data []byte) ([]byte, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if cands := sanitizeSupplyChainProductCandidatesField(root["productCandidates"]); len(cands) > 0 {
		root["productCandidates"] = cands
	} else {
		delete(root, "productCandidates")
	}
	if obs := sanitizeCarrierProductObservationsField(root["carrierProductObservations"]); len(obs) > 0 {
		root["carrierProductObservations"] = obs
	} else {
		delete(root, "carrierProductObservations")
	}
	if obs := sanitizeSupplyLinkObservationsField(root["linkObservations"]); len(obs) > 0 {
		root["linkObservations"] = obs
	} else {
		delete(root, "linkObservations")
	}
	if obs := sanitizeRouteObservationsField(root["routeObservations"]); len(obs) > 0 {
		root["routeObservations"] = obs
	} else {
		delete(root, "routeObservations")
	}
	return json.Marshal(root)
}

// sanitizeResearchResultForUnmarshal repairs common shape/type drifts in LLM output so
// a single malformed field does not drop the whole research response.
func sanitizeResearchResultForUnmarshal(data []byte) ([]byte, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	if fragmentRaw, ok := root["fragment"]; ok {
		if fragmentMap, mapOK := fragmentRaw.(map[string]interface{}); mapOK {
			sanitizeMetricMapField(fragmentMap, "state")
		} else {
			// LLM returned fragment as null, string, array, etc. Replace with empty object
			// so we preserve structure instead of losing fragment entirely.
			root["fragment"] = map[string]interface{}{}
		}
	}

	sanitizeResearchRelations(root)
	sanitizeResearchSupplyChains(root)

	return json.Marshal(root)
}

func sanitizeResearchRelations(root map[string]interface{}) {
	relsRaw, exists := root["relations"]
	if !exists {
		return
	}
	rels, ok := relsRaw.([]interface{})
	if !ok {
		delete(root, "relations")
		return
	}
	clean := make([]interface{}, 0, len(rels))
	for _, relRaw := range rels {
		relMap, ok := relRaw.(map[string]interface{})
		if !ok {
			continue
		}
		sanitizeMetricMapField(relMap, "metrics")
		coerceFloatField(relMap, "confidence", 0.5)
		ensureStringField(relMap, "targetName")
		ensureStringField(relMap, "targetType")
		ensureStringField(relMap, "targetCanonicalPath")
		ensureStringField(relMap, "relationType")
		ensureStringField(relMap, "reason")
		ensureStringField(relMap, "sourceCanonicalPath")
		ensureStringField(relMap, "sourceType")
		ensureStringField(relMap, "sourceName")
		sanitizeMapFieldOrDelete(relMap, "qualifiers")
		clean = append(clean, relMap)
	}
	root["relations"] = clean
}

func sanitizeStringSliceField(root map[string]interface{}, field string) {
	vals := sanitizeStringSlice(root[field])
	if len(vals) == 0 {
		delete(root, field)
		return
	}
	out := make([]interface{}, 0, len(vals))
	for _, v := range vals {
		out = append(out, v)
	}
	root[field] = out
}

func sanitizeStringSlice(raw interface{}) []string {
	items := toInterfaceSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch t := item.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				out = append(out, s)
			}
		case map[string]interface{}:
			for _, key := range []string{"name", "value", "label", "canonicalPath"} {
				if s := strings.TrimSpace(anyToString(t[key])); s != "" {
					out = append(out, s)
					break
				}
			}
		default:
			if s := strings.TrimSpace(anyToString(t)); s != "" {
				out = append(out, s)
			}
		}
	}
	return dedupStrings(out, 16)
}

func anyToString(raw interface{}) string {
	switch t := raw.(type) {
	case nil:
		return ""
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", raw)
	}
}

func sanitizeResearchSupplyChains(root map[string]interface{}) {
	chainsRaw, exists := root["supplyChains"]
	if !exists {
		return
	}
	chains := toInterfaceSlice(chainsRaw)
	if chains == nil {
		delete(root, "supplyChains")
		return
	}

	cleanChains := make([]interface{}, 0, len(chains))
	for _, chainRaw := range chains {
		chainMap, ok := chainRaw.(map[string]interface{})
		if !ok {
			continue
		}
		nodes := extractNodesMultiKey(chainMap)
		if len(nodes) == 0 {
			logChainKeysOnEmpty(chainMap)
			continue
		}
		chainMap["nodes"] = nodes
		coerceFloatFieldMultiKey(chainMap, 0.5, "confidence", "conf", "reliability", "score")
		ensureStringFieldMultiKey(chainMap, "reason", "reason", "explanation", "description", "evidence")
		if via := sanitizeStringArrayField(chainMap["transportVia"]); len(via) > 0 {
			chainMap["transportVia"] = via
		} else {
			delete(chainMap, "transportVia")
		}
		if metrics := sanitizeSupplyChainSegmentMetricsField(chainMap["segmentMetrics"]); len(metrics) > 0 {
			chainMap["segmentMetrics"] = metrics
		} else {
			delete(chainMap, "segmentMetrics")
		}
		if alts := sanitizeSupplyChainAlternativesField(chainMap["alternatives"]); len(alts) > 0 {
			chainMap["alternatives"] = alts
		} else {
			delete(chainMap, "alternatives")
		}
		if cands := sanitizeSupplyChainProductCandidatesField(chainMap["productCandidates"]); len(cands) > 0 {
			chainMap["productCandidates"] = cands
		} else {
			delete(chainMap, "productCandidates")
		}
		if obs := sanitizeCarrierProductObservationsField(chainMap["carrierProductObservations"]); len(obs) > 0 {
			chainMap["carrierProductObservations"] = obs
		} else {
			delete(chainMap, "carrierProductObservations")
		}
		if obs := sanitizeSupplyLinkObservationsField(chainMap["linkObservations"]); len(obs) > 0 {
			chainMap["linkObservations"] = obs
		} else {
			delete(chainMap, "linkObservations")
		}
		if obs := sanitizeRouteObservationsField(chainMap["routeObservations"]); len(obs) > 0 {
			chainMap["routeObservations"] = obs
		} else {
			delete(chainMap, "routeObservations")
		}
		cleanChains = append(cleanChains, chainMap)
	}
	root["supplyChains"] = cleanChains
}

func sanitizeSupplyChainSegmentMetricsField(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if items == nil {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		fromIdx, okFrom := coerceIndexValue(firstValue(m, "fromNodeIndex", "from_node_index", "from"))
		toIdx, okTo := coerceIndexValue(firstValue(m, "toNodeIndex", "to_node_index", "to"))
		productIdx, okProduct := coerceIndexValue(firstValue(m, "productIndex", "product_index", "product"))
		if !okFrom || !okTo || !okProduct {
			continue
		}
		entry := map[string]interface{}{
			"fromNodeIndex": fromIdx,
			"toNodeIndex":   toIdx,
			"productIndex":  productIdx,
		}
		if v := sanitizeMetricWithConfidenceField(firstValue(m, "volume")); v != nil {
			entry["volume"] = v
		}
		if v := sanitizeMetricWithConfidenceField(firstValue(m, "share")); v != nil {
			entry["share"] = v
		}
		if s := strings.TrimSpace(firstStringValue(m, "period")); s != "" {
			entry["period"] = s
		}
		if s := strings.TrimSpace(firstStringValue(m, "start")); s != "" {
			entry["start"] = s
		}
		if s := strings.TrimSpace(firstStringValue(m, "end")); s != "" {
			entry["end"] = s
		}
		coerceFloatField(m, "confidence", 0.5)
		entry["confidence"] = m["confidence"]
		out = append(out, entry)
	}
	return out
}

func sanitizeSupplyChainAlternativesField(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if items == nil {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		customerIdx, okCustomer := coerceIndexValue(firstValue(m, "customerNodeIndex", "customer_node_index", "customerIndex"))
		productIdx, okProduct := coerceIndexValue(firstValue(m, "productNodeIndex", "product_node_index", "productIndex"))
		if !okCustomer || !okProduct {
			continue
		}
		suppliers := toInterfaceSlice(firstValue(m, "suppliers"))
		if len(suppliers) == 0 {
			continue
		}
		cleanSuppliers := make([]interface{}, 0, len(suppliers))
		for _, supplierRaw := range suppliers {
			supplierMap, ok := supplierRaw.(map[string]interface{})
			if !ok {
				continue
			}
			supplierRef := firstStringValue(supplierMap, "supplierRef", "supplier", "canonicalPath", "path")
			if strings.TrimSpace(supplierRef) == "" {
				continue
			}
			entry := map[string]interface{}{
				"supplierRef": strings.TrimSpace(supplierRef),
			}
			coerceFloatField(supplierMap, "technicalFit", 0.5)
			coerceFloatField(supplierMap, "regulatoryFit", 0.5)
			coerceFloatField(supplierMap, "confidence", 0.5)
			entry["technicalFit"] = supplierMap["technicalFit"]
			entry["regulatoryFit"] = supplierMap["regulatoryFit"]
			entry["confidence"] = supplierMap["confidence"]
			if v := sanitizeMetricWithConfidenceField(firstValue(supplierMap, "availableCapacity")); v != nil {
				entry["availableCapacity"] = v
			}
			if v := sanitizeMetricWithConfidenceField(firstValue(supplierMap, "leadTimeToSwitch")); v != nil {
				entry["leadTimeToSwitch"] = v
			}
			if v := sanitizeMetricWithConfidenceField(firstValue(supplierMap, "costToSwitch")); v != nil {
				entry["costToSwitch"] = v
			}
			cleanSuppliers = append(cleanSuppliers, entry)
		}
		if len(cleanSuppliers) == 0 {
			continue
		}
		out = append(out, map[string]interface{}{
			"customerNodeIndex": customerIdx,
			"productNodeIndex":  productIdx,
			"suppliers":         cleanSuppliers,
		})
	}
	return out
}

func sanitizeMetricWithConfidenceField(raw interface{}) map[string]interface{} {
	m, ok := raw.(map[string]interface{})
	if !ok || m == nil {
		return nil
	}
	out := map[string]interface{}{}
	coerceFloatField(m, "value", 0.0)
	out["value"] = m["value"]
	if u := strings.TrimSpace(firstStringValue(m, "unit")); u != "" {
		out["unit"] = u
	}
	coerceFloatField(m, "confidence", 0.5)
	out["confidence"] = m["confidence"]
	return out
}

func sanitizeSupplyChainProductCandidatesField(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if items == nil {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(firstStringValue(m, "name", "label", "rawName"))
		cp := strings.TrimSpace(firstStringValue(m, "canonicalPath", "path", "productRef"))
		if cp == "" && name == "" {
			continue
		}
		entry := map[string]interface{}{}
		if cp != "" {
			entry["canonicalPath"] = cp
		}
		if name != "" {
			entry["name"] = name
		}
		if reason := strings.TrimSpace(firstStringValue(m, "reason", "evidence", "summary")); reason != "" {
			entry["reason"] = reason
		}
		coerceFloatFieldMultiKey(m, 0.5, "confidence", "score")
		entry["confidence"] = m["confidence"]
		out = append(out, entry)
	}
	return out
}

func sanitizeCarrierProductObservationsField(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if items == nil {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		carrierRef := strings.TrimSpace(firstStringValue(m, "carrierRef", "carrier", "supplierRef"))
		productRef := strings.TrimSpace(firstStringValue(m, "productRef", "product", "canonicalPath"))
		if carrierRef == "" || productRef == "" {
			continue
		}
		entry := map[string]interface{}{
			"carrierRef": carrierRef,
			"productRef": productRef,
		}
		if metrics, ok := firstValue(m, "metrics").(map[string]interface{}); ok && len(metrics) > 0 {
			entry["metrics"] = metrics
		}
		if qualifiers, ok := firstValue(m, "qualifiers").(map[string]interface{}); ok && len(qualifiers) > 0 {
			entry["qualifiers"] = qualifiers
		}
		if observedAt := strings.TrimSpace(firstStringValue(m, "observedAt", "observed_at")); observedAt != "" {
			entry["observedAt"] = observedAt
		}
		coerceFloatFieldMultiKey(m, 0.5, "confidence", "score")
		entry["confidence"] = m["confidence"]
		out = append(out, entry)
	}
	return out
}

func sanitizeSupplyLinkObservationsField(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if items == nil {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		productRef := strings.TrimSpace(firstStringValue(m, "productRef", "product", "canonicalPath"))
		if productRef == "" {
			continue
		}
		entry := map[string]interface{}{
			"productRef": productRef,
		}
		if supplierRef := strings.TrimSpace(firstStringValue(m, "supplierRef", "supplier")); supplierRef != "" {
			entry["supplierRef"] = supplierRef
		}
		if customerRef := strings.TrimSpace(firstStringValue(m, "customerRef", "customer")); customerRef != "" {
			entry["customerRef"] = customerRef
		}
		if metrics, ok := firstValue(m, "metrics").(map[string]interface{}); ok && len(metrics) > 0 {
			entry["metrics"] = metrics
		}
		if qualifiers, ok := firstValue(m, "qualifiers").(map[string]interface{}); ok && len(qualifiers) > 0 {
			entry["qualifiers"] = qualifiers
		}
		if observedAt := strings.TrimSpace(firstStringValue(m, "observedAt", "observed_at")); observedAt != "" {
			entry["observedAt"] = observedAt
		}
		coerceFloatFieldMultiKey(m, 0.5, "confidence", "score")
		entry["confidence"] = m["confidence"]
		out = append(out, entry)
	}
	return out
}

func sanitizeRouteObservationsField(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if items == nil {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		productRef := strings.TrimSpace(firstStringValue(m, "productRef", "product", "canonicalPath"))
		transportVia := sanitizeStringArrayField(firstValue(m, "transportVia", "transport_via"))
		if productRef == "" && len(transportVia) == 0 {
			continue
		}
		entry := map[string]interface{}{}
		if productRef != "" {
			entry["productRef"] = productRef
		}
		if supplierRef := strings.TrimSpace(firstStringValue(m, "supplierRef", "supplier")); supplierRef != "" {
			entry["supplierRef"] = supplierRef
		}
		if customerRef := strings.TrimSpace(firstStringValue(m, "customerRef", "customer")); customerRef != "" {
			entry["customerRef"] = customerRef
		}
		if len(transportVia) > 0 {
			entry["transportVia"] = transportVia
		}
		if vesselType := strings.TrimSpace(firstStringValue(m, "vesselType", "vessel_type")); vesselType != "" {
			entry["vesselType"] = vesselType
		}
		if qualifiers, ok := firstValue(m, "qualifiers").(map[string]interface{}); ok && len(qualifiers) > 0 {
			entry["qualifiers"] = qualifiers
		}
		if observedAt := strings.TrimSpace(firstStringValue(m, "observedAt", "observed_at")); observedAt != "" {
			entry["observedAt"] = observedAt
		}
		coerceFloatFieldMultiKey(m, 0.5, "confidence", "score")
		entry["confidence"] = m["confidence"]
		out = append(out, entry)
	}
	return out
}

func firstValue(holder map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if v, ok := holder[key]; ok && v != nil {
			return v
		}
	}
	return nil
}

func coerceIndexValue(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		if v < 0 {
			return 0, false
		}
		return v, true
	case int32:
		if v < 0 {
			return 0, false
		}
		return int(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return int(v), true
	case float64:
		if v < 0 {
			return 0, false
		}
		return int(v), true
	case float32:
		if v < 0 {
			return 0, false
		}
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil && i >= 0 {
			return int(i), true
		}
		if f, err := v.Float64(); err == nil && f >= 0 {
			return int(f), true
		}
		return 0, false
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false
		}
		if i, err := strconv.Atoi(s); err == nil && i >= 0 {
			return i, true
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && f >= 0 {
			return int(f), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// extractNodesMultiKey looks for the nodes array under multiple possible field names
// that the LLM may use instead of the expected "nodes".
func extractNodesMultiKey(chainMap map[string]interface{}) []interface{} {
	for _, key := range []string{"nodes", "node_list", "chain_nodes", "participants", "steps", "stages", "chain", "entities", "members", "elements", "links"} {
		if raw, ok := chainMap[key]; ok && raw != nil {
			nodes := sanitizeSupplyChainNodesField(raw)
			if len(nodes) > 0 {
				return nodes
			}
		}
	}
	return nil
}

// coerceFloatFieldMultiKey looks for a float value under multiple keys and normalizes it
// into the first key name. Used because LLM may return "confidence" as "reliability", etc.
func coerceFloatFieldMultiKey(m map[string]interface{}, defaultVal float64, keys ...string) {
	if len(keys) == 0 {
		return
	}
	canonicalKey := keys[0]
	for _, key := range keys {
		if raw, ok := m[key]; ok && raw != nil {
			if key != canonicalKey {
				m[canonicalKey] = raw
				delete(m, key)
			}
			coerceFloatField(m, canonicalKey, defaultVal)
			return
		}
	}
	m[canonicalKey] = defaultVal
}

// ensureStringFieldMultiKey looks for a string value under multiple keys and normalizes it.
func ensureStringFieldMultiKey(m map[string]interface{}, canonicalKey string, keys ...string) {
	for _, key := range keys {
		if raw, ok := m[key]; ok && raw != nil {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				if key != canonicalKey {
					m[canonicalKey] = s
					delete(m, key)
				}
				return
			}
		}
	}
	ensureStringField(m, canonicalKey)
}

// logChainKeysOnEmpty logs the actual keys of a chain object when no nodes were found,
// to help diagnose what field names the LLM used.
func logChainKeysOnEmpty(chainMap map[string]interface{}) {
	keys := make([]string, 0, len(chainMap))
	for k := range chainMap {
		keys = append(keys, k)
	}
	log.Printf("[ResearchParse] WARN: chain with 0 nodes dropped. keys=%v", keys)
}

// logRawSupplyChainsFromJSON extracts supplyChains from raw LLM JSON and returns a short
// diagnostic string (missing|null|[]|len=N,sample=...) so logs show exactly what the LLM returned.
func logRawSupplyChainsFromJSON(jsonData []byte) string {
	var raw map[string]interface{}
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return "parse_err"
	}
	v, exists := raw["supplyChains"]
	if !exists {
		return "missing"
	}
	if v == nil {
		return "null"
	}
	arr, ok := v.([]interface{})
	if !ok {
		return fmt.Sprintf("type=%T", v)
	}
	if len(arr) == 0 {
		return "[]"
	}
	// Log structure of first chain: keys and len(nodes/steps/etc)
	sample := ""
	if len(arr) > 0 {
		if m, ok := arr[0].(map[string]interface{}); ok {
			keys := make([]string, 0, len(m))
			for k, val := range m {
				if s, ok := val.([]interface{}); ok {
					keys = append(keys, fmt.Sprintf("%s[len=%d]", k, len(s)))
				} else {
					keys = append(keys, k)
				}
			}
			sample = strings.Join(keys, ",")
			if len(sample) > 200 {
				sample = sample[:200] + "..."
			}
		}
	}
	return fmt.Sprintf("len=%d first=%s", len(arr), sample)
}

func sanitizeSupplyChainNodesField(raw interface{}) []interface{} {
	nodes := toInterfaceSlice(raw)
	if nodes == nil {
		return nil
	}
	clean := make([]interface{}, 0, len(nodes))
	for _, nodeRaw := range nodes {
		nodeMap := sanitizeSupplyChainNode(nodeRaw)
		if nodeMap == nil {
			continue
		}
		clean = append(clean, nodeMap)
	}
	return clean
}

func sanitizeSupplyChainNode(raw interface{}) map[string]interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		cp := firstStringValue(v, "canonicalPath", "canonical_path", "path")
		name := firstStringValue(v, "name", "step", "entity", "label", "id")
		nodeType := firstStringValue(v, "type")
		if cp == "" && name == "" {
			return nil
		}
		if cp != "" && name == "" {
			name = deriveNameFromCanonicalPath(cp)
		}
		if cp != "" && nodeType == "" {
			nodeType = inferSupplyChainNodeTypeFromPath(cp)
		}
		if nodeType == "" {
			nodeType = "ORGANIZATION"
		}
		nodeType = normalizeSupplyChainNodeType(nodeType)
		if cp == "" && name != "" {
			cp = inferCanonicalPathForSupplyChainActor(name, nodeType)
		}
		node := map[string]interface{}{
			"canonicalPath": cp,
			"name":          name,
			"type":          nodeType,
		}
		if s := firstStringValue(v, "role"); s != "" {
			node["role"] = s
		}
		if s := firstStringValue(v, "facilityType", "facility_type"); s != "" {
			node["facilityType"] = s
		}
		if s := firstStringValue(v, "facilityKind", "facility_kind"); s != "" {
			node["facilityKind"] = s
		}
		return node
	case string:
		return sanitizeSupplyChainNodeString(v)
	default:
		return nil
	}
}

func sanitizeSupplyChainNodeString(raw string) map[string]interface{} {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	parsedPath := normalizeNodePathToken(s)
	if strings.HasPrefix(parsedPath, "/") {
		return map[string]interface{}{
			"canonicalPath": parsedPath,
			"name":          deriveNameFromCanonicalPath(parsedPath),
			"type":          inferSupplyChainNodeTypeFromPath(parsedPath),
		}
	}
	nodeType := "ORGANIZATION"
	cp := inferCanonicalPathForSupplyChainActor(s, nodeType)
	return map[string]interface{}{
		"canonicalPath": cp,
		"name":          s,
		"type":          nodeType,
	}
}

func sanitizeMapFieldOrDelete(holder map[string]interface{}, field string) {
	raw, ok := holder[field]
	if !ok {
		return
	}
	if _, isMap := raw.(map[string]interface{}); !isMap {
		delete(holder, field)
	}
}

func ensureStringField(holder map[string]interface{}, field string) {
	raw, ok := holder[field]
	if !ok || raw == nil {
		return
	}
	switch v := raw.(type) {
	case string:
		holder[field] = strings.TrimSpace(v)
	default:
		holder[field] = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func firstStringValue(holder map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		raw, ok := holder[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		default:
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func coerceFloatField(holder map[string]interface{}, field string, fallback float64) {
	raw, ok := holder[field]
	if !ok || raw == nil {
		holder[field] = fallback
		return
	}
	switch v := raw.(type) {
	case float64:
		holder[field] = v
	case float32:
		holder[field] = float64(v)
	case int:
		holder[field] = float64(v)
	case int64:
		holder[field] = float64(v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			holder[field] = f
			return
		}
		holder[field] = fallback
	case string:
		s := strings.TrimSpace(v)
		s = strings.TrimSuffix(s, "%")
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			holder[field] = f
			return
		}
		holder[field] = fallback
	default:
		holder[field] = fallback
	}
}

func sanitizeStringArrayField(raw interface{}) []string {
	arr := toInterfaceSlice(raw)
	if arr == nil {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		switch v := item.(type) {
		case string:
			s := strings.TrimSpace(v)
			if s != "" {
				out = append(out, s)
			}
		default:
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func toInterfaceSlice(raw interface{}) []interface{} {
	switch v := raw.(type) {
	case []interface{}:
		return v
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		var arr []interface{}
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") && json.Unmarshal([]byte(s), &arr) == nil {
			return arr
		}
		for _, token := range splitSupplyChainNodeString(s) {
			arr = append(arr, token)
		}
		return arr
	default:
		return nil
	}
}

func splitSupplyChainNodeString(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	repl := strings.NewReplacer("→", "->", "=>", "->", "\n", "->", ";", "->")
	s = repl.Replace(s)
	parts := strings.Split(s, "->")
	if len(parts) == 1 {
		parts = strings.Split(s, ",")
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		t := strings.TrimSpace(part)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func normalizeNodePathToken(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "/") {
		return s
	}
	lowered := strings.ToLower(s)
	for _, prefix := range []string{"org/", "fac/", "product/", "market/", "program/", "loc/", "deposit/", "region/"} {
		if strings.HasPrefix(lowered, prefix) {
			return "/" + s
		}
	}
	return s
}

func deriveNameFromCanonicalPath(cp string) string {
	trimmed := strings.TrimSpace(cp)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" && len(parts) > 1 {
		last = strings.TrimSpace(parts[len(parts)-2])
	}
	last = strings.ReplaceAll(last, "-", " ")
	return last
}

// inferCanonicalPathFromNameAndType generates a best-effort canonical path
// when the LLM provides a name and type but omits canonicalPath.
// Paths use /discovered/ as the intermediate segment to signal auto-generation;
// the downstream canonicalizer resolves them to proper stable paths.
func inferCanonicalPathFromNameAndType(name, nodeType string) string {
	slug := slugifyNodeName(name)
	if slug == "" {
		return ""
	}
	prefix := nodeTypeToPathPrefix(strings.ToUpper(strings.TrimSpace(nodeType)))
	return prefix + slug
}

// inferCanonicalPathForSupplyChainActor is used when repairing supply chain nodes.
// For ORGANIZATION: returns /org/discovered/{slug} (org stays as org in supply chain).
// For other types: delegates to inferCanonicalPathFromNameAndType.
func inferCanonicalPathForSupplyChainActor(name, nodeType string) string {
	slug := slugifyNodeName(name)
	if slug == "" {
		return ""
	}
	nt := strings.ToUpper(strings.TrimSpace(nodeType))
	if nt == "ORGANIZATION" || nt == "ORG" {
		return "/org/discovered/" + slug
	}
	return inferCanonicalPathFromNameAndType(name, nodeType)
}

func nodeTypeToPathPrefix(nodeType string) string {
	switch nodeType {
	case "FACILITY":
		return "/fac/discovered/"
	case "PRODUCT":
		return "/product/discovered/"
	case "PROGRAM":
		return "/program/discovered/"
	case "MARKET":
		return "/market/discovered/"
	case "GEO_REGION":
		return "/loc/discovered/"
	case "DEPOSIT":
		return "/deposit/discovered/"
	case "COMMUNITY":
		return "/community/discovered/"
	default:
		return "/org/discovered/"
	}
}

func slugifyNodeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	var buf strings.Builder
	buf.Grow(len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			buf.WriteRune(r)
			prevDash = false
		case r == ' ', r == '_', r == '.', r == ',', r == '(', r == ')', r == '&':
			if !prevDash && buf.Len() > 0 {
				buf.WriteByte('-')
				prevDash = true
			}
		case r == '-':
			if !prevDash && buf.Len() > 0 {
				buf.WriteByte('-')
				prevDash = true
			}
		}
	}
	result := strings.TrimRight(buf.String(), "-")
	if len(result) > 80 {
		result = result[:80]
		result = strings.TrimRight(result, "-")
	}
	return result
}

// repairSupplyChainNodePaths fills in empty CanonicalPath fields on supply chain
// nodes that survived direct json.Unmarshal but were returned by the LLM without
// a canonical path. Without this repair they would be dropped by the parser.
func repairSupplyChainNodePaths(chains []SupplyChain) {
	for i := range chains {
		for j := range chains[i].Nodes {
			n := &chains[i].Nodes[j]
			if strings.TrimSpace(n.CanonicalPath) != "" {
				continue
			}
			name := strings.TrimSpace(n.Name)
			if name == "" {
				continue
			}
			nodeType := strings.TrimSpace(n.Type)
			if nodeType == "" {
				nodeType = "ORGANIZATION"
			}
			n.CanonicalPath = inferCanonicalPathForSupplyChainActor(name, normalizeSupplyChainNodeType(nodeType))
		}
	}
}

func inferSupplyChainNodeTypeFromPath(cp string) string {
	path := strings.TrimSpace(strings.ToLower(cp))
	switch {
	case strings.HasPrefix(path, "/org/"):
		return "ORGANIZATION"
	case strings.HasPrefix(path, "/fac/"):
		return "FACILITY"
	case strings.HasPrefix(path, "/product/"):
		return "PRODUCT"
	case strings.HasPrefix(path, "/program/"):
		return "PROGRAM"
	case strings.HasPrefix(path, "/market/"):
		return "MARKET"
	case strings.HasPrefix(path, "/loc/"), strings.HasPrefix(path, "/region/"):
		return "GEO_REGION"
	case strings.HasPrefix(path, "/deposit/"):
		return "DEPOSIT"
	case strings.HasPrefix(path, "/community/"):
		return "COMMUNITY"
	default:
		return "ORGANIZATION"
	}
}

// normalizeSupplyChainNodeType maps arbitrary LLM output to runtime-supported enum.
// Prevents contract drift when LLM returns "COMMUNITY", "network", "group" etc.
func normalizeSupplyChainNodeType(raw string) string {
	u := strings.ToUpper(strings.TrimSpace(raw))
	switch u {
	case "ORGANIZATION", "ORG":
		return "ORGANIZATION"
	case "FACILITY", "FAC":
		return "FACILITY"
	case "PRODUCT":
		return "PRODUCT"
	case "PROGRAM":
		return "PROGRAM"
	case "MARKET":
		return "MARKET"
	case "GEO_REGION", "GEO", "LOCATION", "LOC", "REGION":
		return "GEO_REGION"
	case "DEPOSIT":
		return "DEPOSIT"
	case "COMMUNITY", "NETWORK", "GROUP", "COLLECTIVE":
		return "COMMUNITY"
	default:
		return "ORGANIZATION"
	}
}

// sanitizeMetricMapField removes malformed metric maps from the holder field.
// This allows strategy output to remain usable when a single metric value is emitted as narrative text.
func sanitizeMetricMapField(holder map[string]interface{}, field string) {
	raw, ok := holder[field]
	if !ok {
		return
	}
	metricMap, ok := raw.(map[string]interface{})
	if !ok {
		delete(holder, field)
		return
	}

	clean := make(map[string]interface{}, len(metricMap))
	for key, value := range metricMap {
		if isValidMetricValueJSON(value) {
			clean[key] = value
		}
	}

	if len(clean) == 0 {
		delete(holder, field)
		return
	}
	holder[field] = clean
}

func buildResearchFocusContextBlock(focus string, spherePaths []string, keywords []string, originalQuery string) string {
	focus = strings.TrimSpace(focus)
	originalQuery = strings.TrimSpace(originalQuery)
	spheresLine := strings.Join(dedupNonEmptyStrings(spherePaths), ", ")
	keywordsLine := strings.Join(dedupNonEmptyStrings(keywords), ", ")

	if focus == "" && spheresLine == "" && keywordsLine == "" && originalQuery == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("FOCUS CONTEXT (use to steer grounded search):\n")
	if originalQuery != "" {
		b.WriteString(fmt.Sprintf("- originalQuery: %q\n", originalQuery))
	}
	if focus != "" {
		b.WriteString(fmt.Sprintf("- focus: %s\n", focus))
	}
	if spheresLine != "" {
		b.WriteString(fmt.Sprintf("- spherePaths: %s\n", spheresLine))
	}
	if keywordsLine != "" {
		b.WriteString(fmt.Sprintf("- keywords: %s\n", keywordsLine))
	}
	return b.String()
}

func buildResearchChainContextBlock(chainContext string) string {
	chainContext = strings.TrimSpace(chainContext)
	if chainContext == "" {
		return ""
	}
	chainContext = truncateTextHeadTail(chainContext, maxResearchChainContextChars)
	return "SUPPLY CHAIN CONTEXT:\n" + chainContext
}

func buildResearchExistingEntitiesBlock(existingEntities []string) string {
	if len(existingEntities) == 0 {
		return ""
	}
	ents := existingEntities
	if len(ents) > 50 {
		ents = ents[:50]
	}
	var b strings.Builder
	b.WriteString("EXISTING ENTITIES (reuse instead of creating duplicates):\n")
	for _, e := range ents {
		if strings.TrimSpace(e) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s\n", truncateTextHeadTail(e, maxResearchEntityLineChars)))
	}
	b.WriteString("- Prefer existing canonical paths when entity identity matches.\n")
	return b.String()
}

func truncateTextHeadTail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 32 {
		return text[:limit]
	}
	head := (limit * 2) / 3
	tail := limit - head - len("\n...[truncated]...\n")
	if tail < 0 {
		tail = 0
	}
	if head > len(text) {
		head = len(text)
	}
	if tail > len(text)-head {
		tail = len(text) - head
	}
	return text[:head] + "\n...[truncated]...\n" + text[len(text)-tail:]
}

func compactResearchRawContent(raw string) string {
	return truncateTextHeadTail(raw, maxResearchRawContentChars)
}

func isDomainFragment(fragment *models.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	cp := strings.TrimSpace(strings.ToLower(fragment.CanonicalPath))
	return fragment.Type == models.FragmentTypeDomain || strings.HasPrefix(cp, "/domain/")
}

func buildGeneralResearchDomainExpansionBlock(fragment *models.HorizonFragment) string {
	if !isDomainFragment(fragment) {
		return ""
	}
	return `DOMAIN EXPANSION GUIDANCE (CRITICAL OVERRIDE):
- This fragment is a DOMAIN contract. Your primary goal is to expand it into first-order entities.
- Use ONLY domain relation types: ` + domainScaffoldRelationTypesPromptString() + `.
- Target canonicalPath MUST match targetType:
  ORGANIZATION->/org or /gov, PERSON->/person, PRODUCT->/product, PROGRAM->/program, MARKET->/market, FACILITY->/fac, GEO_REGION->/loc, EVENT->/event, MEDIA_CHANNEL->/media, COMMUNITY->/community, DOMAIN->/domain, AFFECTED_ZONE->/zone
- If the user asks about a market, use /market/... as a TARGET (anchor), NOT as a DOMAIN.
- Do NOT output placeholder names like NA/N-A/Unknown. Use fewer entities instead.
- For AFFECTED_ZONE: slug MUST be descriptive place/region name (northern-rakhine, gaza-strip, donbas). NEVER "na".
- ZONE vs EVENT: ZONE = geographic area; EVENT = discrete occurrence. TRIGGERED_BY: zone→event.`
}

func generalResearchCanonicalSupplementBlock() string {
	return `- /segment/{domain}/{vertical}/{geo} for org marketSegments; /media/{platform}/{slug} for media channels
- Product: use a single canonical format /product/{category}/{type}/{stage}/{slug}[/{variant-slug}]
- For services, use category=service and map service domain/subdomain into {type}/{stage}
- Markets: prefer /market/{geo}/{domain}/{vertical}; for HAS_MARKET_SEGMENT child path = parent path + one segment
- Communities: /community/{geo}/{type}/{slug}; use HAS_SUBGROUP for parent->narrower subgroup
- Programs: prefer /program/{jurisdiction}/{slug} when jurisdiction is known
- AFFECTED_ZONE: /zone/{geo}/{condition_type}/{slug}. geo: ` + taxonomy.ISO2GeoDescriptionForPrompts() + `. condition_type: military_conflict, natural_disaster, political_tension, humanitarian_crisis, earthquake_affected, flood_zone. slug: descriptive place/region name (northern-syria, donbas, gaza-strip, northern-rakhine). NEVER use "na" as slug — use region name or condition-specific name (e.g. myanmar-crisis-area).
- NEVER use "/discovered/*" (not allowed)
- canonicalPath MUST match expected prefix for targetType; if unsure, still output your best stable canonicalPath`
}

func buildGeneralResearchRelationTaxonomyBlock(isDomain bool) string {
	if isDomain {
		return `ALLOWED RELATION TAXONOMY (DOMAIN OVERRIDE):
- Use ONLY: ` + domainScaffoldRelationTypesPromptString() + `.
- Do not emit non-domain relations for DOMAIN fragments.
- Domain is configuration, not a graph node for non-domain relation families.`
	}
	return "ALLOWED RELATION TAXONOMY:\n" + extractionGeneralRelationRulesBlock()
}

func generalResearchProductCanonicalizationBlock() string {
	return `Product Canonicalization:
- If you can resolve a more specific product name/path, update fragment.canonicalPath.
- Prefer /product/{category}/{type}/{stage}/{slug} consistently.
- For services, use category=service and map service domain/subdomain into {type}/{stage}.
- If the current path is flat or acronym-like, expand to full product name and align relation targets.
- Product naming rules:
  - Use SINGULAR nouns by default (e.g., "battery cell", "lithium carbonate"), not invented plurals.
  - Use plural only when plural is standard industry term.
  - Do NOT include company/brand prefix unless it is clearly official and widely used.`
}

func generalResearchNameHistoryBlock() string {
	return `Name History (ORG/PERSON - rebrand/rename/formerly-known-as):
- If former names are found, output fragment.nameHistory[] with:
  { "name": "...", "kind": "short|legal|brand|aka", "validFrom"?: "YYYY-MM-DD", "validTo"?: "YYYY-MM-DD" }
- Do NOT include current name in nameHistory.
- Search cues: "formerly known as", "renamed", "rebrand", "previous name".`
}

func generalResearchExplicitSourceRulesBlock() string {
	return generatedExplicitSourceRulesBlock()
}

func buildResearchDecompositionGuidanceBlock(fragment *models.HorizonFragment, hints []DecompositionHint, originalQuery string) string {
	if fragment == nil {
		return ""
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	isAnchor := fragment.Type == models.FragmentTypeMarket ||
		fragment.Type == models.FragmentTypeProgram ||
		strings.HasPrefix(cp, "/market/") ||
		strings.HasPrefix(cp, "/program/")
	if !isAnchor {
		return ""
	}

	for i := range hints {
		if strings.TrimSpace(hints[i].AnchorPath) != cp {
			continue
		}
		hint := hints[i]
		var b strings.Builder
		b.WriteString("MARKET/PROGRAM DECOMPOSITION GUIDANCE:\n")
		b.WriteString("- This fragment is an anchor. Prioritize concrete players and operating assets.\n")
		if len(hint.EntityTypes) > 0 {
			b.WriteString(fmt.Sprintf("- target entity types: %s\n", strings.Join(hint.EntityTypes, ", ")))
		}
		if len(hint.SubMarkets) > 0 {
			b.WriteString(fmt.Sprintf("- sub-markets: %s\n", strings.Join(hint.SubMarkets, ", ")))
		}
		if len(hint.PlayerCategories) > 0 {
			b.WriteString(fmt.Sprintf("- player categories: %s\n", strings.Join(hint.PlayerCategories, ", ")))
		}
		if len(hint.SearchQueries) > 0 {
			b.WriteString("- suggested search queries:\n")
			for _, q := range hint.SearchQueries {
				if strings.TrimSpace(q) == "" {
					continue
				}
				b.WriteString(fmt.Sprintf("  - %s\n", q))
			}
		}
		b.WriteString("- Prefer real-world entities and relations over abstract commentary.\n")
		return b.String()
	}

	originalQuery = strings.TrimSpace(originalQuery)
	if originalQuery == "" {
		return ""
	}
	return fmt.Sprintf(`MARKET/PROGRAM DECOMPOSITION GUIDANCE:
- This fragment is an anchor; prioritize concrete entities and business relations.
- originalQuery: %q
- Discover major players, products/services, regulators, and facilities.
- Emit these as relations with grounded evidence and stable canonical paths.`, originalQuery)
}

func joinPromptBlocks(blocks ...string) string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		out = append(out, block)
	}
	return strings.Join(out, "\n\n")
}

func (a *ResearchAgent) buildGeneralEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if strings.TrimSpace(questions) == "" {
		questions = "Resolve critical unknowns for fragment attributes, key dependencies, and supply chain links."
	}

	chainBlock := buildResearchChainContextBlock(input.ChainContext)
	focusBlock := buildResearchFocusContextBlock(input.Focus, input.SpherePaths, input.Keywords, input.OriginalQuery)
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	decompositionBlock := buildResearchDecompositionGuidanceBlock(frag, input.DecompositionHints, input.OriginalQuery)
	domainBlock := buildGeneralResearchDomainExpansionBlock(frag)
	specBlock := buildResearchSpecializationModulesBlock(input.Specialties)
	businessRulesBlock := taxonomy.BusinessTagRulesPrompt()
	profileBlock := entity_semantics.ResearchBlockForPath(frag.CanonicalPath, frag.Type, frag.CommunityType)
	objectTypeTaskBlock := buildObjectTypeTaskSpecializationBlockWithHints(frag.CanonicalPath, frag.Type, frag.CommunityType, frag.Qualifiers, frag.Identifiers, frag.SpherePaths)
	coreBusinessTaxonomyBlock := buildCoreBusinessTaxonomyBlock()
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	nameRulesBlock := naming.PromptEntityNameNormalizationRules()
	canonicalRulesBlock := taxonomy.ExtendedCanonicalPathRulesPrompt() + "\n" + generalResearchCanonicalSupplementBlock()
	relationTaxonomyBlock := buildGeneralResearchRelationTaxonomyBlock(isDomainFragment(frag))

	return joinPromptBlocks(
		`You are a research agent. Enrich the fragment with grounded facts and business-meaningful relations.`,
		`CURRENT FRAGMENT DATA:
`+string(fragmentJSON),
		chainBlock,
		focusBlock,
		existingEntitiesBlock,
		researchPreferExistingEntitiesBlock(),
		decompositionBlock,
		domainBlock,
		specBlock,
		businessRulesBlock,
		profileBlock,
		nameRulesBlock,
		objectTypeTaskBlock,
		coreBusinessTaxonomyBlock,
		objectQualifierContractBlock,
		generalResearchProductCanonicalizationBlock(),
		generalResearchNameHistoryBlock(),
		generalResearchExplicitSourceRulesBlock(),
		relationTaxonomyBlock,
		`CANONICAL PATH RULES:
`+canonicalRulesBlock,
		fmt.Sprintf(`QUESTIONS TO ANSWER (from Critic):
- %s`, questions),
		`Return JSON object:
{
  "fragment": { updated fields only },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?", "metrics"?, "qualifiers"? } ],
  "supplyChains": [ { "nodes": [ { "canonicalPath", "name", "type", "role"? } ], "confidence", "reason", "transportVia"?: ["/fac/..."] } ]
}`,
		strategySupplyChainsJSONContractBlock(),
		`OUTPUT BUDGET RULES (CRITICAL):
- Keep fragment.description short (max 500 chars); omit if it risks JSON truncation.
- If budget is tight, prioritize missingInfo answers and valid JSON with closed braces.`,
		strategyMetricValueFormatBlock(),
		`Return ONLY valid JSON. No markdown.`,
	)
}

func isValidMetricValueJSON(raw interface{}) bool {
	b, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var mv models.MetricValue
	return json.Unmarshal(b, &mv) == nil
}

// buildSupplyChainEnrichmentPrompt builds a prompt for supply-chain enrichment (value chains, logistics, extractive).
func (a *ResearchAgent) buildSupplyChainEnrichmentPrompt(input ResearchStrategyInput) string {
	return a.buildOperationalChainEnrichmentPrompt(taxonomy.DomainModeSupplyChain, input)
}

func (a *ResearchAgent) buildServiceChainEnrichmentPrompt(input ResearchStrategyInput) string {
	return a.buildOperationalChainEnrichmentPrompt(taxonomy.DomainModeServiceChain, input)
}

func (a *ResearchAgent) buildOperationalChainEnrichmentPrompt(mode string, input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	mode = normalizeOperationalChainMode(mode)
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	title := "SUPPLY CHAIN"
	mandatoryBlock := supplyChainMandatoryBlock
	operationalModelBlock := supplyChainOperationalModelBlock
	specialScenariosBlock := supplyChainSpecialScenariosBlock
	productGranularityBlock := supplyChainProductGranularityBlock
	edgeTypes := taxonomy.SupplyChainEdgeTypesString()
	entityTypes := taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeSupplyChain)
	jsonContractBlock := strategySupplyChainsJSONContractBlock()
	metricsBlock := strategySupplyChainMetricsVocabularyBlock()
	capabilityMetricsBlock := strategyCapabilityMetricWhitelistBlock()
	findBlock := `WHAT TO FIND (org→fac→product→fac→org trees, 1-to-many at each level):
1. From ORG: ALL child organizations (subsidiaries, controlled, partners) AND all its own facilities. One org → many orgs + many facs. Output /org/ and /fac/ paths.
2. From FAC: ALL products/services THIS facility creates directly. One fac → many products. NOT products of related or parent companies. Output /product/ paths.
3. From PRODUCT: ALL org-recipients (orgs that receive this product via their facilities). One product → many recipients. Output /org/ paths.
4. Chains: one chain per leaf path. Preferred pattern: org→fac→product→fac→org. Nodes alternate actor→product→actor; actor nodes use their real types (/org/, /fac/, /deposit/), not forced /fac/.
   transportVia is expected for cross-border, maritime, pipeline, chokepoint, rail, or otherwise route-sensitive legs when a plausible route is inferable.
5. Extractive: EXTRACTS_FROM (FACILITY→DEPOSIT). If CURRENT FRAGMENT is DEPOSIT: incoming edges with sourceCanonicalPath.
   Rare exception: if the extracting/processing facility is unknown but the downstream recipient is clear, a DEPOSIT→ORG/FAC SUPPLIES_TO shortcut is acceptable as lower-confidence placeholder evidence. Prefer explicit facility stages whenever known.`
	if mode == taxonomy.DomainModeServiceChain {
		title = "SERVICE CHAIN"
		mandatoryBlock = serviceChainMandatoryBlock
		operationalModelBlock = serviceChainOperationalModelBlock
		specialScenariosBlock = serviceChainSpecialScenariosBlock
		productGranularityBlock = serviceChainProductGranularityBlock
		edgeTypes = taxonomy.ServiceChainEdgeTypesString()
		entityTypes = taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeServiceChain)
		jsonContractBlock = strategyServiceChainsJSONContractBlock()
		metricsBlock = strategyServiceChainMetricsVocabularyBlock()
		capabilityMetricsBlock = strategyServiceCapabilityMetricWhitelistBlock()
		findBlock = `WHAT TO FIND (org->fac->service->fac->org trees, 1-to-many at each level):
1. From ORG: ALL child operators, managed-service units, resellers, channel partners, and service delivery facilities. One org -> many orgs + many facs. Output /org/ and /fac/ paths.
2. From FAC: ALL services/capabilities THIS facility delivers directly. Use canonical /product/service/... paths when the node is a service capability. One fac -> many service nodes.
3. From SERVICE/CAPABILITY: ALL recipient facilities, customer sites, tenants, enterprise users, or downstream operators that receive or depend on it. Output /fac/ and /org/ paths.
4. Chains: one chain per leaf path. Preferred pattern: org->fac->service->fac->org. Nodes still alternate actor->product(service)->actor; actor nodes use their real types (/org/, /fac/), not forced /fac/.
   transportVia is valid for operational relay sites, network hubs, landing stations, depots, terminals, or other concrete facilities that materially gate delivery.
5. Control gates: authorization, approved-vendor, service-contract, identity/access, licensing, settlement, or insurance gates must stay in observations/qualifiers or concrete actor relations, not fake service nodes.`
	}
	if questions == "" {
		switch {
		case mode == taxonomy.DomainModeServiceChain && frag != nil && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/org/"):
			questions = "Find ALL child operators, managed-service units, resellers, partners, and service delivery facilities. One org -> many orgs + many facs. Format: JSON."
		case mode == taxonomy.DomainModeServiceChain && frag != nil && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/"):
			questions = "Find ALL services/capabilities that THIS facility delivers directly. One fac -> many service capabilities. Do NOT list unrelated parent-company offerings. Format: JSON."
		case mode == taxonomy.DomainModeServiceChain:
			questions = "Build org->fac->service->fac->org trees (1-to-many at each level): (1) From org: ALL child operators + ALL delivery facilities. (2) From fac: ALL services/capabilities THIS site delivers. (3) From service: ALL recipient sites/orgs. Format: JSON."
		case frag != nil && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/org/"):
			questions = "Find ALL child organizations (subsidiaries, controlled, partners) AND ALL own facilities. One org → many child orgs + many facs. Format: JSON."
		case frag != nil && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/"):
			questions = "Find ALL products/services that THIS facility creates directly. One fac → many products. Do NOT list products of related or parent companies. Format: JSON."
		default:
			questions = "Build org→fac→product→fac→org trees (1-to-many at each level): (1) From org: ALL child orgs + ALL facilities. (2) From fac: ALL products THIS fac produces. (3) From product: ALL org-recipients. Format: JSON."
		}
	}
	geoContext := geoAndSectorContext(frag)
	chainBlock := buildResearchChainContextBlock(input.ChainContext)
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for %s context. Build coherent operational chains.

%s

%s

%s

%s

Use ONLY these relation types (canonical): %s.
Do NOT use RELATED_TO, AFFILIATED_WITH, OWNS, or other non-operational-chain types. COMPETES_WITH is allowed only for explicit direct competitor relationships. PARTNERS_WITH and OPERATES_VIA are allowed only for explicit operational/ecosystem overlays. INVESTS_IN and MEMBER_OF are allowed only for explicit institutional/structural overlays.
%s

ENTITY TYPES in %s: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath MUST follow; supplyChains nodes too):
%s

%s
%s
%s
%s
%s

%s

INTEGRATION_TYPE qualifier (for SUPPLIES_TO): When the relationship nature is clear, add qualifiers.integration_type:
- "vertical": supplier is owned/controlled by the same parent (e.g., BYD subsidiaries supplying BYD)
- "partnership": long-term partnership, JV, or strategic alliance (e.g., European OEM–tier-1 networks)
- "arm's-length": spot or short-term commercial relationship

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if found: homeLocation, state (e.g. /metric/reserves_volume, /metric/production_annual for extractive) },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?", "qualifiers": { "integration_type"?: "vertical|partnership|arm's-length", "transport_via"?: ["/fac/us/port-long-beach", "/fac/ae/jebel-ali-port"], "source_role"?: "supplier|dependent", "target_role"?: "customer|dependency" } } ],
  "supplyChains": [ { "nodes": [ { "canonicalPath", "name", "type", "role"? } ], "confidence", "reason", "transportVia"?: ["/fac/us/port-long-beach", "/fac/ae/jebel-ali-port"] } ]
}
If you update fragment.homeLocation, set homeLocation.name to a concrete city/site whenever the entity name, slug, or source contains a clear place clue (e.g. Wuxi, Huizhou, Eilat, Binh Duong, Riyadh). Set iso3166_1 only when the country is explicit or strongly corroborated by the place clue/source. If the place clue and ISO2 conflict, correct the ISO2 or omit it; never keep contradictory geo.
Nodes MUST alternate actor→product. Include FACILITY where transformation happens (product→facility→product). Keep ORGANIZATION endpoints when the chain meaningfully starts or ends at org level.
For SUPPLIES_TO: add qualifiers.transport_via when transport route is known or reasonably inferable for route-sensitive legs. transportVia in chains = facilities/chokepoints the product passes through.

%s

%s

%s

%s

Return ONLY valid JSON. No markdown.`, title, mandatoryBlock, operationalModelBlock, specialScenariosBlock, productGranularityBlock, edgeTypes, GovEntityRelationRulesBlock(), strings.ToLower(title), entityTypes, taxonomy.ExtendedCanonicalPathRulesPrompt(), chainBlock, geoContext, existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, findBlock, string(fragmentJSON), questions, metricsBlock, capabilityMetricsBlock, strategyMetricValueFormatBlock(), jsonContractBlock)
}

// EnrichSupplyChainFull runs anchor-centric full supply chain query (Plan mode).
// Input: SupplyChainQuery (anchor, direction, depth, filters). No fragment.
// Output: SupplyChains with nodes, segmentMetrics, alternatives. Called directly, not via ResearchPipeline.
func (a *ResearchAgent) EnrichSupplyChainFull(ctx context.Context, query SupplyChainQuery) ([]SupplyChain, error) {
	return a.enrichOperationalChainFull(ctx, query, taxonomy.DomainModeSupplyChain)
}

func (a *ResearchAgent) EnrichServiceChainFull(ctx context.Context, query SupplyChainQuery) ([]SupplyChain, error) {
	return a.enrichOperationalChainFull(ctx, query, taxonomy.DomainModeServiceChain)
}

func (a *ResearchAgent) enrichOperationalChainFull(ctx context.Context, query SupplyChainQuery, mode string) ([]SupplyChain, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildOperationalChainFullQueryPrompt(mode, query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredChains, _ := FilterDomainChains(ctx, normalizeOperationalChainMode(mode), out.SupplyChains)
	return filteredChains, nil
}

// EnrichSupplyChainPreHotPath returns a lightweight always-on steering plan that
// helps the hot-path query focus on the right operational branches without
// acting as a hard filter.
func (a *ResearchAgent) EnrichSupplyChainPreHotPath(ctx context.Context, query SupplyChainQuery) (*SupplyChainPreHotPathPlan, error) {
	return a.enrichOperationalChainPreHotPath(ctx, query, taxonomy.DomainModeSupplyChain)
}

func (a *ResearchAgent) EnrichServiceChainPreHotPath(ctx context.Context, query SupplyChainQuery) (*SupplyChainPreHotPathPlan, error) {
	return a.enrichOperationalChainPreHotPath(ctx, query, taxonomy.DomainModeServiceChain)
}

func (a *ResearchAgent) enrichOperationalChainPreHotPath(ctx context.Context, query SupplyChainQuery, mode string) (*SupplyChainPreHotPathPlan, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildOperationalChainPreHotPathPrompt(mode, query)
	out, err := a.runSupplyChainPreHotPathPrompt(ctx, prompt, researchPromptOptions{
		PreferredModel:   "vertex-gemini-3-flash-preview",
		MinContextWindow: 8000,
		MaxOutputSize:    4000,
		MinCapability:    7,
	})
	if err != nil || out == nil {
		return out, err
	}
	return out, nil
}

// EnrichSupplyChainHotPath runs a denser first-pass supply chain query for fast-first runtime.
// It prefers a stronger grounded model and asks for multiple concrete branches in one response.
func (a *ResearchAgent) EnrichSupplyChainHotPath(ctx context.Context, query SupplyChainQuery, prePlan *SupplyChainPreHotPathPlan) ([]SupplyChain, error) {
	return a.enrichOperationalChainHotPath(ctx, query, prePlan, taxonomy.DomainModeSupplyChain)
}

func (a *ResearchAgent) EnrichServiceChainHotPath(ctx context.Context, query SupplyChainQuery, prePlan *SupplyChainPreHotPathPlan) ([]SupplyChain, error) {
	return a.enrichOperationalChainHotPath(ctx, query, prePlan, taxonomy.DomainModeServiceChain)
}

func (a *ResearchAgent) enrichOperationalChainHotPath(ctx context.Context, query SupplyChainQuery, prePlan *SupplyChainPreHotPathPlan, mode string) ([]SupplyChain, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildOperationalChainHotPathPrompt(mode, query, prePlan)
	out, err := a.runResearchPromptWithOptions(ctx, prompt, nil, researchPromptOptions{
		PreferredModel:   "vertex-gemini-2.5-pro",
		MinContextWindow: 16000,
		MaxOutputSize:    32000,
		MinCapability:    8,
	})
	if err != nil || out == nil {
		return nil, err
	}
	filteredChains, _ := FilterDomainChains(ctx, normalizeOperationalChainMode(mode), out.SupplyChains)
	return filteredChains, nil
}

// EnrichSupplyChainHotPathCapabilities enriches already-discovered hot-path chains with reusable actor×product observations.
// It must not expand topology; it should only add scoped observations and candidates when evidence supports them.
func (a *ResearchAgent) EnrichSupplyChainHotPathCapabilities(ctx context.Context, query SupplyChainQuery, chains []SupplyChain, prePlan *SupplyChainPreHotPathPlan) (*SupplyChainScopedObservations, error) {
	return a.enrichOperationalChainHotPathCapabilities(ctx, query, chains, prePlan, taxonomy.DomainModeSupplyChain)
}

func (a *ResearchAgent) EnrichServiceChainHotPathCapabilities(ctx context.Context, query SupplyChainQuery, chains []SupplyChain, prePlan *SupplyChainPreHotPathPlan) (*SupplyChainScopedObservations, error) {
	return a.enrichOperationalChainHotPathCapabilities(ctx, query, chains, prePlan, taxonomy.DomainModeServiceChain)
}

func (a *ResearchAgent) enrichOperationalChainHotPathCapabilities(ctx context.Context, query SupplyChainQuery, chains []SupplyChain, prePlan *SupplyChainPreHotPathPlan, mode string) (*SupplyChainScopedObservations, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	if len(chains) == 0 {
		return nil, nil
	}
	prompt := a.buildOperationalChainHotPathCapabilityPrompt(mode, query, chains, prePlan)
	out, err := a.runSupplyChainScopedObservationPrompt(ctx, prompt, researchPromptOptions{
		PreferredModel:   "vertex-gemini-3-flash-preview",
		MinContextWindow: 8000,
		MaxOutputSize:    6000,
		MinCapability:    7,
	})
	if err != nil || out == nil {
		return nil, err
	}
	return out, nil
}

func (a *ResearchAgent) buildSupplyChainPreHotPathPrompt(query SupplyChainQuery) string {
	return a.buildOperationalChainPreHotPathPrompt(taxonomy.DomainModeSupplyChain, query)
}

func (a *ResearchAgent) buildServiceChainPreHotPathPrompt(query SupplyChainQuery) string {
	return a.buildOperationalChainPreHotPathPrompt(taxonomy.DomainModeServiceChain, query)
}

func (a *ResearchAgent) buildOperationalChainPreHotPathPrompt(mode string, query SupplyChainQuery) string {
	queryJSON, _ := json.MarshalIndent(query, "", "  ")
	mode = normalizeOperationalChainMode(mode)
	title := "supply-chain"
	outputStrategy := "commodity_corridor|oem_aftermarket|industrial_components|retail_distribution|generic_supply_chain"
	softRulesBlock := `- For OEM/service/spare-parts cases, think in terms of parts manufacturing, authorized suppliers, repair centers, service depots, installed-base support, customer plants, outage/maintenance support.
- For commodity corridor cases, think in terms of mines/wells/plants, terminals, ports, refineries, processors, chokepoints, and destination facilities.
- For sanctions-/customs-/export-control-sensitive cases, think separately about physical relay hubs, customs/bonded gates, screening/licensing/settlement/insurance gates, and alternative relay geographies.`
	if mode == taxonomy.DomainModeServiceChain {
		title = "service-chain"
		outputStrategy = "service_delivery_network|managed_service_access|field_service_aftermarket|digital_infrastructure|generic_service_chain"
		softRulesBlock = `- For digital infrastructure / cloud / network cases, think in terms of regions, data centers, landing stations, IXPs, network hubs, colocation, power/network dependencies, and downstream enterprise exposure.
- For field-service / aftermarket cases, think in terms of spare-parts manufacturing, authorized suppliers, repair centers, service depots, outage support, and customer plant/site recipients.
- For managed-service / reseller cases, think separately about core provider, managed-service operator, reseller/channel partner, downstream customer site, and authorization / approved-vendor / service-contract gates.`
	}
	chainLabel := "supply chains"
	if mode == taxonomy.DomainModeServiceChain {
		chainLabel = "service chains"
	}
	return fmt.Sprintf(`You are a %s pre-hot-path decomposition agent.

TASK:
- This stage runs BEFORE hot-path graph construction for every %s job.
- Do NOT build %s here.
- Do NOT hard-constrain the next search.
- Return ONLY a lightweight steering plan that helps the hot path ask better questions.
- If the query is already concrete and dense, keep the plan short and minimal.
- If the query is broad, service-heavy, route-sensitive, or otherwise ambiguous, decompose it into likely operational branches without forcing them.

WHAT TO RETURN:
- candidateAnchorTerms: concrete org/facility/brand/subsidiary aliases or operational anchor names worth retrying if the original anchor is too broad
- candidateProductFamilies: more concrete product families or service/capability families worth prioritizing
- candidateOperationalAnchorClasses: likely node classes to look for first
- candidateFlowArchetypes: likely topology shapes for this domain
- candidateTransitHubTerms: likely transshipment, customs, bonded, depot, inland-terminal, or relay hub terms worth retrying
- candidateGateTerms: likely non-product-carrying control points that gate execution (screening, licensing, settlement, insurance, service authorization)
- candidateRouteGeographies: likely relay geographies or corridor countries worth checking before concluding the route is single-hub
- searchFocus: concise retrieval foci for the next hot-path prompt
- preferredEvidence: source/evidence classes to trust first
- derivedProductFilters: up to 3 concrete product/service filter strings that MAY be used for a focused retry if the first hot-path pass is too broad

SOFT-STEERING RULES:
- This plan is guidance only. It must NEVER act as a hard filter.
- Do not suppress the original query intent.
- Prefer concrete operational decomposition over abstract business prose.
- For broad company anchors, include high-signal operational aliases, business units, country subsidiaries, depot/operator names, or common abbreviations when they materially improve retrieval.
%s

OUTPUT:
Return ONLY valid JSON:
{
  "strategy": "%s",
  "strategyReason": "short explanation",
  "candidateAnchorTerms": ["..."],
  "candidateProductFamilies": ["..."],
  "candidateOperationalAnchorClasses": ["..."],
  "candidateFlowArchetypes": ["..."],
  "candidateTransitHubTerms": ["..."],
  "candidateGateTerms": ["..."],
  "candidateRouteGeographies": ["..."],
  "searchFocus": ["..."],
  "preferredEvidence": ["..."],
  "derivedProductFilters": ["..."],
  "confidence": 0.0
}

QUERY:
%s

No markdown.`, title, title, chainLabel, softRulesBlock, outputStrategy, string(queryJSON))
}

// buildSupplyChainFullQueryPrompt builds anchor-centric prompt. Model: trees from org → fac → product → fac → org (recursion).
func (a *ResearchAgent) buildSupplyChainFullQueryPrompt(query SupplyChainQuery) string {
	return a.buildOperationalChainFullQueryPrompt(taxonomy.DomainModeSupplyChain, query)
}

func (a *ResearchAgent) buildOperationalChainFullQueryPrompt(mode string, query SupplyChainQuery) string {
	var anchorDesc string
	if len(query.Anchors) > 0 {
		trimmed := make([]string, 0, len(query.Anchors))
		for _, p := range query.Anchors {
			if p := strings.TrimSpace(p); p != "" {
				trimmed = append(trimmed, p)
			}
		}
		anchorDesc = "anchors=[" + strings.Join(trimmed, ", ") + "]"
	} else {
		anchorDesc = "anchor=" + strings.TrimSpace(query.Anchor)
	}
	depth := query.Depth
	if depth < 3 || depth > 6 {
		depth = 4
	}
	productFilter := strings.Join(query.ProductFilter, ", ")
	geoFilter := strings.Join(query.GeoFilter, ", ")
	filters := ""
	if productFilter != "" {
		filters += ", productFilter=[" + productFilter + "]"
	}
	if geoFilter != "" {
		filters += ", geoFilter=[" + geoFilter + "]"
	}
	mode = normalizeOperationalChainMode(mode)

	taskBlock := `TASK: Build supply chain trees (1-to-many at each level). Depth: ` + fmt.Sprintf("%d", depth) + ` hops. ` + anchorDesc + filters + `

ANCHOR HANDLING:
- If anchorType=org: start from the organization and expand its facilities, products, recipients, and relay facilities.
- If anchorType=fac: start from the facility and expand its direct products, upstream inputs, downstream recipients, and the organizations around it.
- If anchorType=product: start from the product and trace concrete manufacturers, recipient facilities, relay hubs, and destination organizations.
- If anchorType=deposit: start from the deposit and expand extraction/processing operators, downstream processors, and route-sensitive relay nodes.

TREE STRUCTURE (each step fans out 1→many):
Step 1 — ORG → [child orgs, facilities]: For each org anchor, find ALL organizations under it (subsidiaries, controlled, partners) AND its own production facilities. One org may have many child orgs and many facilities.
Step 2 — FAC → [products]: For each facility, find ALL products/services THIS facility produces directly. One facility may produce many products. Do NOT include products of related or parent companies.
Step 3 — PRODUCT → [recipient facs → orgs]: For each product, find ALL facilities that receive/consume it, and their parent organizations. One product may go to many recipients.
Step 4 — Recurse: From each recipient org (back to Step 1) until depth reached.

OUTPUT ONE CHAIN PER LEAF PATH. If org A has fac F1 and fac F2, and F1 produces P1 and P2 — output separate chains: A→F1→P1→..., A→F1→P2→..., A→F2→... etc.

ROUTE / GATE DISCIPLINE:
- For cross-border, rerouted, sanctions-sensitive, customs-sensitive, or import-controlled flows, actively look for more than one credible relay hub when the route is known to fork or reroute.
- Physical relay nodes belong in transportVia and routeObservations.
- Non-product-carrying gates (screening, licensing, settlement, trade-credit insurance, service authorization) must not be invented as product nodes; capture them in linkObservations / routeObservations qualifiers or canonical relations when they are concrete actors.`
	title := "supply chain"
	mandatoryBlock := supplyChainMandatoryBlock
	operationalModelBlock := supplyChainOperationalModelBlock
	specialScenariosBlock := supplyChainSpecialScenariosBlock
	productGranularityBlock := supplyChainProductGranularityBlock
	edgeTypes := taxonomy.SupplyChainEdgeTypesString()
	entityTypes := taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeSupplyChain)
	jsonContractBlock := strategySupplyChainsJSONContractBlock()
	metricsBlock := strategySupplyChainMetricsVocabularyBlock()
	capabilityMetricsBlock := strategyCapabilityMetricWhitelistBlock()
	if mode == taxonomy.DomainModeServiceChain {
		title = "service chain"
		mandatoryBlock = serviceChainMandatoryBlock
		operationalModelBlock = serviceChainOperationalModelBlock
		specialScenariosBlock = serviceChainSpecialScenariosBlock
		productGranularityBlock = serviceChainProductGranularityBlock
		edgeTypes = taxonomy.ServiceChainEdgeTypesString()
		entityTypes = taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeServiceChain)
		jsonContractBlock = strategyServiceChainsJSONContractBlock()
		metricsBlock = strategyServiceChainMetricsVocabularyBlock()
		capabilityMetricsBlock = strategyServiceCapabilityMetricWhitelistBlock()
		taskBlock = `TASK: Build service delivery / capability access trees (1-to-many at each level). Depth: ` + fmt.Sprintf("%d", depth) + ` hops. ` + anchorDesc + filters + `

ANCHOR HANDLING:
- If anchorType=org: start from the provider/operator organization and expand its delivery facilities, service capabilities, downstream customer sites, and recipient organizations.
- If anchorType=fac: start from the facility and expand its direct service/capability outputs, upstream support inputs, downstream recipient sites, and the organizations around it.
- If anchorType=product: start from the /product/service/... capability and trace concrete providers, delivery sites, customer sites, relay hubs, and downstream organizations.
- If anchorType=deposit: deposits are rare in service-chain mode; only use them when a concrete physical resource site materially gates the service delivery path.

TREE STRUCTURE (each step fans out 1→many):
Step 1 — ORG → [child operators, facilities]: For each org anchor, find ALL service operators under it (subsidiaries, controlled units, resellers, managed-service units) AND its delivery facilities. One org may have many child orgs and many facilities.
Step 2 — FAC → [service capabilities]: For each facility, find ALL services/capabilities THIS site delivers directly. Use canonical /product/service/... paths when the node is a service capability.
Step 3 — SERVICE/CAPABILITY → [recipient facs → orgs]: For each service node, find ALL facilities or customer sites that receive/consume it, and their parent organizations. One capability may go to many recipients.
Step 4 — Recurse: From each recipient org (back to Step 1) until depth reached.

OUTPUT ONE CHAIN PER LEAF PATH. If provider A has site F1 and F2, and F1 delivers service S1 and S2, output separate chains: A→F1→S1→..., A→F1→S2→..., A→F2→... etc.

ROUTE / GATE DISCIPLINE:
- For cross-border, regulated, authorization-sensitive, or route-sensitive service delivery, actively look for more than one credible relay hub or delivery/control path when the topology forks.
- Physical relay nodes belong in transportVia and routeObservations.
- Non-service-carrying gates (authorization, identity/access, licensing, settlement, insurer approval, approved-vendor status, service-contract gates) must not be invented as service nodes; capture them in linkObservations / routeObservations qualifiers or canonical relations when they are concrete actors.`
	}

	return fmt.Sprintf(`You are a %s research agent.

%s

%s

%s

%s

%s

%s

Use ONLY these relation types: %s.
ENTITY TYPES: %s.

CANONICAL PATH RULES:
%s

%s

%s

%s

OUTPUT: JSON with "supplyChains" array. Each chain:
- nodes: org→fac→product→fac→org chain. Actors: /org/, /fac/, or /deposit/.
- use node.role when it clarifies flow semantics (supplier, processor, distributor, customer); use owner/operator/manager/controller only for non-product-carrying adjacency that should become OWNS / MANAGES / CONTROLS outside the chain.
- segmentMetrics: volume, share, period per segment when known.
- transportVia: facility/chokepoint paths product passes through. For route-sensitive legs, include it unless evidence is genuinely absent.
- confidence, reason.

Return ONLY valid JSON: { "supplyChains": [ { "nodes": [...], "segmentMetrics": [...], "confidence", "reason" } ] }
No markdown.`, title, mandatoryBlock, operationalModelBlock, specialScenariosBlock, productGranularityBlock, taskBlock, edgeTypes, entityTypes, taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock(), metricsBlock, capabilityMetricsBlock, jsonContractBlock)
}

func (a *ResearchAgent) buildSupplyChainHotPathPrompt(query SupplyChainQuery, prePlan *SupplyChainPreHotPathPlan) string {
	return a.buildOperationalChainHotPathPrompt(taxonomy.DomainModeSupplyChain, query, prePlan)
}

func (a *ResearchAgent) buildServiceChainHotPathPrompt(query SupplyChainQuery, prePlan *SupplyChainPreHotPathPlan) string {
	return a.buildOperationalChainHotPathPrompt(taxonomy.DomainModeServiceChain, query, prePlan)
}

func (a *ResearchAgent) buildOperationalChainHotPathPrompt(mode string, query SupplyChainQuery, prePlan *SupplyChainPreHotPathPlan) string {
	base := a.buildOperationalChainFullQueryPrompt(mode, query)
	mode = normalizeOperationalChainMode(mode)
	capabilityMetricsBlock := strategyCapabilityMetricWhitelistBlock()
	chainTarget := "6-20"
	if strings.EqualFold(strings.TrimSpace(query.AnchorType), "product") {
		chainTarget = "3-8"
	}
	hotPathBlock := fmt.Sprintf(`HOT PATH OBJECTIVE:
- Produce the first high-value supply-chain graph quickly.
- Prefer a dense first pass over exhaustive recursion.
- Return multiple concrete branches in ONE response. Target %s chains when evidence supports it; do not stop after a single obvious chain.
- Mixed anchors are valid. Anchors may be organizations, facilities, deposits, products, or a mix.
- Prefer concrete operational nodes: factories, plants, mines, depots, bonded warehouses, customs terminals, ports, inland terminals, distribution centers, fulfillment hubs, and stores when they are operationally relevant.
- For each main branch, try to return a full operational slice: supplier/factory -> product family -> import/customs/port/DC -> recipient facility/store/e-commerce node.
- If there are multiple credible relay hubs or corridor variants, return them as distinct branches or distinct routeObservations instead of silently collapsing to one route.
- For retail / distribution anchors, include the main inbound product families, import/distribution branches, owned subsidiaries, publishing/import arms, local distribution centers, and major downstream store / e-commerce fulfillment branches when they are directly relevant.
- Explicitly separate main goods flow from component/material inputs and from support/logistics/infrastructure branches. Support branches are useful, but must not crowd out the main goods flows.
- Include transportVia when a route/chokepoint is known. Include alternatives when a customer/product pair has plausible substitute suppliers.
- For sanctions-sensitive, customs-sensitive, or export-control-sensitive flows, explicitly search not only for the main route but also for:
  - relay hubs,
  - bonded / customs / inland-terminal gates,
  - screening / licensing / settlement / insurance control points,
  - alternative relay geographies.
- When evidence supports reusable actor×product baseline facts, include carrierProductObservations in the same response. Do not leave reusable baseline metrics implicit.
- Prefer branches that maximize first-pass decision value: main products, main suppliers, main import/distribution nodes, main transformations, main customer/distribution branches.
- For sanctions-sensitive, cross-border inbound, import-constrained, or export-control-sensitive flows, explicitly seek customs terminals, rerouted corridors, alternative import nodes, export-license gates, and sanctioned/control-sensitive touchpoints.
- When the query is subject-scoped around a named company, asset, or operator, keep the graph centered on that subject. Do NOT return parallel industry chains unless they are direct upstream dependencies, substitute branches, or clearly route-supporting branches for the anchored subject path.
- When evidence points to a target geography or inbound destination, prioritize chains that enter, terminate in, or materially support that geography. De-prioritize branches that stay entirely outside the subject path and target geography.
- Do not stop at org -> product -> org if a more operational chain is available. Actor-only skeletons are incomplete unless the anchor itself is inherently actor-only.
- Keep taxonomy strict. Never invent invalid canonical path families or placeholder nodes.`, chainTarget)
	if mode == taxonomy.DomainModeServiceChain {
		capabilityMetricsBlock = strategyServiceCapabilityMetricWhitelistBlock()
		hotPathBlock = fmt.Sprintf(`HOT PATH OBJECTIVE:
- Produce the first high-value service-chain graph quickly.
- Prefer a dense first pass over exhaustive recursion.
- Return multiple concrete branches in ONE response. Target %s chains when evidence supports it; do not stop after a single obvious chain.
- Mixed anchors are valid. Anchors may be organizations, facilities, service capability paths, or a mix.
- Prefer concrete operational nodes: data centers, delivery sites, repair depots, landing stations, network hubs, bonded/customs terminals, ports, field-service hubs, customer sites, and colocation facilities when they materially gate delivery.
- For each main branch, try to return a full operational slice: provider/delivery site -> service capability -> relay/control site -> recipient facility/customer site -> recipient org.
- If there are multiple credible relay hubs, control paths, or delivery variants, return them as distinct branches or distinct routeObservations instead of silently collapsing to one route.
- Explicitly separate the main delivered capability path from support/logistics/infrastructure branches. Support branches are useful, but must not crowd out the main service-delivery path.
- Include transportVia when a relay site or chokepoint is known. Include alternatives when a customer/capability pair has plausible substitute providers or alternate delivery geographies.
- For regulated, sanctions-sensitive, customs-sensitive, vendor-approval-sensitive, or authorization-sensitive flows, explicitly search not only for the main route but also for:
  - relay hubs,
  - bonded / customs / inland-terminal gates,
  - identity / authorization / licensing / settlement / insurance / approved-vendor control points,
  - alternative delivery geographies.
- When evidence supports reusable provider×capability baseline facts, include carrierProductObservations in the same response. Do not leave reusable baseline metrics implicit.
- Prefer branches that maximize first-pass decision value: main capabilities, main providers, main relay/control nodes, main customer-site exposures, and main substitute branches.
- When the query is subject-scoped around a named provider, facility, tenant, or operator, keep the graph centered on that subject. Do NOT return parallel industry chains unless they are direct dependencies, substitute branches, or clearly route-supporting branches for the anchored subject path.
- When evidence points to a target geography, customer footprint, or delivery region, prioritize chains that enter, terminate in, or materially support that geography.
- Do not stop at org -> service -> org if a more operational chain is available. Actor-only skeletons are incomplete unless the anchor itself is inherently actor-only.
- Keep taxonomy strict. Never invent invalid canonical path families or placeholder nodes.`, chainTarget)
	}
	if strings.EqualFold(strings.TrimSpace(query.AnchorType), "product") {
		hotPathBlock += `
- Product-anchored hot path must stay selective. Prioritize the highest-confidence upstream manufacturers, specialized suppliers, and operational facilities first instead of broad industry sweeps.`
	}
	steeringBlock := ""
	if prePlan != nil {
		if b, err := json.MarshalIndent(prePlan, "", "  "); err == nil {
			steeringBlock = "\n\nPRE-HOT-PATH STEERING (soft guidance, not hard filters):\n" +
				"- Use this to decide which operational branches to prioritize first.\n" +
				"- Do NOT ignore relevant branches just because they are not named here.\n" +
				"- If strategy suggests service topology, explicitly seek service depots, repair centers, delivery facilities, network hubs, authorized suppliers, installed-base support nodes, and customer plant/site recipients.\n" +
				"- If strategy suggests commodity corridor or physical relay topology, explicitly seek production facilities, terminals, ports, chokepoints, processors, and destination facilities.\n" +
				"- If steering includes transit hubs, gate terms, or route geographies, treat them as high-signal retry terms for relay hubs and control points.\n" +
				string(b)
		}
	}
	return base + "\n\n" + hotPathBlock + steeringBlock + "\n\n" + capabilityMetricsBlock
}

func (a *ResearchAgent) buildSupplyChainHotPathCapabilityPrompt(query SupplyChainQuery, chains []SupplyChain, prePlan *SupplyChainPreHotPathPlan) string {
	return a.buildOperationalChainHotPathCapabilityPrompt(taxonomy.DomainModeSupplyChain, query, chains, prePlan)
}

func (a *ResearchAgent) buildOperationalChainHotPathCapabilityPrompt(mode string, query SupplyChainQuery, chains []SupplyChain, prePlan *SupplyChainPreHotPathPlan) string {
	type capabilityPair struct {
		CarrierRef         string   `json:"carrierRef"`
		CarrierName        string   `json:"carrierName,omitempty"`
		ProductRef         string   `json:"productRef"`
		ProductName        string   `json:"productName,omitempty"`
		SampleCustomerRefs []string `json:"sampleCustomerRefs,omitempty"`
		SampleTransportVia []string `json:"sampleTransportVia,omitempty"`
	}
	trimmedChains := chains
	if len(trimmedChains) > 10 {
		trimmedChains = trimmedChains[:10]
	}
	workingProducts := dedupStrings(append(append([]string{}, query.ProductFilter...), collectProductRefsFromSupplyChains(trimmedChains)...), 32)
	pairView := make([]capabilityPair, 0, 8)
	for _, pair := range extractCapabilityHotPathPairs(trimmedChains) {
		pairView = append(pairView, capabilityPair{
			CarrierRef:         pair.CarrierRef,
			CarrierName:        pair.CarrierName,
			ProductRef:         pair.ProductRef,
			ProductName:        pair.ProductName,
			SampleCustomerRefs: append([]string(nil), pair.SampleCustomerRefs...),
			SampleTransportVia: append([]string(nil), pair.SampleTransportVia...),
		})
		if len(pairView) >= 8 {
			break
		}
	}
	pairsJSON, _ := json.MarshalIndent(pairView, "", "  ")
	workingProductsJSON, _ := json.MarshalIndent(workingProducts, "", "  ")
	prePlanJSON := "{}"
	if prePlan != nil {
		if b, err := json.MarshalIndent(prePlan, "", "  "); err == nil && len(b) > 0 {
			prePlanJSON = string(b)
		}
	}
	mode = normalizeOperationalChainMode(mode)
	title := "supply chain capability enrichment"
	intro := `- The graph skeleton below is already accepted as the current hot-path supply chain result.`
	scopeLine := `- Your job is ONLY to add scoped supply-chain evidence, with emphasis on reusable carrier×product capability baseline facts.`
	controlLine := `- When the current chains are cross-border, sanctions-sensitive, import-constrained, export-control-sensitive, or customs-sensitive, prefer adding linkObservations / routeObservations for customs_clearance_days, inventory_cover_days_at_destination, regulatory_fit, switch_time_days, export_license_required, sanctions_touchpoint, approved_vendor_only, service_contract_required, and dual_use_flag whenever evidence exists.`
	whitelistBlock := strings.TrimSpace(strategyCapabilityMetricWhitelistBlock())
	if mode == taxonomy.DomainModeServiceChain {
		title = "service chain capability enrichment"
		intro = `- The graph skeleton below is already accepted as the current hot-path service-chain result.`
		scopeLine = `- Your job is ONLY to add scoped service-chain evidence, with emphasis on reusable provider×capability baseline facts.`
		controlLine = `- When the current chains are cross-border, regulated, authorization-sensitive, vendor-approval-sensitive, or customs-sensitive, prefer adding linkObservations / routeObservations for availability_slo, p95_latency_ms, failover_time_minutes, recovery_time_hours, support_response_time_hours, auth_required, approved_vendor_only, service_contract_required, region_lock_in, export_license_required, sanctions_touchpoint, and dual_use_flag whenever evidence exists.`
		whitelistBlock = strings.TrimSpace(strategyServiceCapabilityMetricWhitelistBlock())
	}
	return fmt.Sprintf(`You are a %s agent.

TASK:
%s
- Do NOT create new topology. Do NOT add or remove graph entities.
- %s
- Return ONLY scoped observation arrays:
  - carrierProductObservations
  - linkObservations when a fact is clearly customer/destination specific
  - routeObservations when a fact is clearly transport/path specific
  - productCandidates only if the existing working product scope is genuinely missing a material product/component
- If no reusable baseline evidence exists for a given carrier×product pair, omit the observation instead of guessing.

WORKING PRODUCT SCOPE:
%s

CURRENT HOT-PATH CARRIER×PRODUCT PAIRS:
%s

PRE-HOT-PATH STEERING:
%s

STRICT RULES:
- carrierProductObservations are practically required when evidence exists for reusable actor×product facts in the current chains.
- Keep one product per link/leg. Do NOT smear multi-product truth across a single link.
- Do NOT restate route-specific or customer-specific facts as reusable capability baseline.
- %s
- If relay hubs or control points are known but are not part of the current topology, surface them in routeObservations / linkObservations rather than dropping them.
- Prefer concrete reusable metric keys only from this whitelist:
%s
- Use ONLY valid canonical paths already present in the provided chains for carrierRef/productRef, unless you are returning a genuinely new productCandidate.

OUTPUT:
- Return ONLY valid JSON:
  {
    "productCandidates": [...],
    "carrierProductObservations": [...],
    "linkObservations": [...],
    "routeObservations": [...]
  }
- Do NOT return supplyChains or nodes in this pass.
- No markdown.`,
		title,
		intro,
		scopeLine,
		string(workingProductsJSON),
		string(pairsJSON),
		prePlanJSON,
		controlLine,
		whitelistBlock)
}

func normalizeOperationalChainMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case taxonomy.DomainModeServiceChain:
		return taxonomy.DomainModeServiceChain
	default:
		return taxonomy.DomainModeSupplyChain
	}
}

func collectProductRefsFromSupplyChains(chains []SupplyChain) []string {
	if len(chains) == 0 {
		return nil
	}
	out := make([]string, 0, len(chains)*2)
	for _, chain := range chains {
		for _, node := range chain.Nodes {
			cp := strings.TrimSpace(node.CanonicalPath)
			if strings.HasPrefix(cp, "/product/") {
				out = append(out, cp)
			}
		}
		for _, obs := range chain.CarrierProductObservations {
			if cp := strings.TrimSpace(obs.ProductRef); strings.HasPrefix(cp, "/product/") {
				out = append(out, cp)
			}
		}
	}
	return out
}

type capabilityHotPathPair struct {
	CarrierRef         string
	CarrierName        string
	ProductRef         string
	ProductName        string
	SampleCustomerRefs []string
	SampleTransportVia []string
}

func extractCapabilityHotPathPairs(chains []SupplyChain) []capabilityHotPathPair {
	if len(chains) == 0 {
		return nil
	}
	seen := make(map[string]int)
	out := make([]capabilityHotPathPair, 0, len(chains))
	for _, chain := range chains {
		for i := 1; i < len(chain.Nodes)-1; i++ {
			productNode := chain.Nodes[i]
			if !strings.HasPrefix(strings.TrimSpace(productNode.CanonicalPath), "/product/") {
				continue
			}
			carrierNode := chain.Nodes[i-1]
			carrierRef := strings.TrimSpace(carrierNode.CanonicalPath)
			productRef := strings.TrimSpace(productNode.CanonicalPath)
			if carrierRef == "" || productRef == "" {
				continue
			}
			customerRefs := []string{}
			if i+1 < len(chain.Nodes) {
				if customerRef := strings.TrimSpace(chain.Nodes[i+1].CanonicalPath); customerRef != "" {
					customerRefs = append(customerRefs, customerRef)
				}
			}
			key := strings.ToLower(carrierRef + "|" + productRef)
			if idx, ok := seen[key]; ok {
				out[idx].SampleCustomerRefs = dedupStrings(append(out[idx].SampleCustomerRefs, customerRefs...), 4)
				out[idx].SampleTransportVia = dedupStrings(append(out[idx].SampleTransportVia, chain.TransportVia...), 6)
				continue
			}
			seen[key] = len(out)
			out = append(out, capabilityHotPathPair{
				CarrierRef:         carrierRef,
				CarrierName:        strings.TrimSpace(carrierNode.Name),
				ProductRef:         productRef,
				ProductName:        strings.TrimSpace(productNode.Name),
				SampleCustomerRefs: dedupStrings(customerRefs, 4),
				SampleTransportVia: dedupStrings(chain.TransportVia, 6),
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// buildGovernanceEnrichmentPrompt builds a prompt for ownership, board, key persons, gov hierarchy (canonical types).
func (a *ResearchAgent) buildGovernanceEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify owners, subsidiaries, board members, key executives; ownership percentages; government hierarchy (ADMINISTERS)."
	}
	geoContext := geoAndSectorContext(frag)
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for GOVERNANCE context. Aim for COMPLETE ownership and control structure:

ENTITY TYPES in governance: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath MUST follow):
%s

%s
%s
%s

%s

Use ONLY these relation types (canonical): %s.
Do NOT use RELATED_TO, KEY_PERSON, HAS_KEY_PERSON, or other non-governance types. Community-specific canonical types are allowed when they materially explain control, lineage, mobilization, or informal governance.

DIRECTION RULES (CRITICAL):
- Use ROLE-FIRST direction for OWNS/SUBSIDIARY_OF/INVESTS_IN/ASSURES/CONTROLS (ORG<->ORG): source is semantic owner/investor/assurer/controller, not automatically CURRENT FRAGMENT.
- For OWNS, direction is ALWAYS owner -> owned entity. Example: "PIF owns Noon" → source=PIF (owner), target=Noon (owned_entity). NEVER the reverse (Noon -> PIF).
- If CURRENT FRAGMENT is owned by X, you MUST keep relationType=OWNS and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = CURRENT FRAGMENT.
- NEVER output reversed OWNS (CURRENT FRAGMENT -> its owner). If scouting Noon and PIF owns it, source=PIF, target=Noon.
- For INVESTS_IN, direction is ALWAYS investor -> investee.
- If CURRENT FRAGMENT is invested in by X, you MUST keep relationType=INVESTS_IN and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = CURRENT FRAGMENT.
- NEVER output reversed INVESTS_IN (CURRENT FRAGMENT -> its investor).
- For ASSURES, direction is ALWAYS assurer -> assuree.
- If CURRENT FRAGMENT is audited/certified/inspected by X, you MUST keep relationType=ASSURES and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = CURRENT FRAGMENT.
- SUBSIDIARY_OF remains child -> parent and is not a replacement for OWNS direction.
- For directed ORG->ORG edges above, include qualifiers.source_role and qualifiers.target_role.

WHAT TO FIND (prioritize completeness):
1. Ownership chain: Trace to ultimate parent or beneficial owner. OWNS (parent→subsidiary): ORG/PERSON/COMMUNITY→ORG. SUBSIDIARY_OF (subsidiary→parent). Include intermediate holding companies, SPVs, nominee vehicles, trustees, and indirect holding layers. metrics.ownership_percent when known. COMMUNITY (dynasty, tribe) can OWNS ORGANIZATION. UPSTREAM: when this entity is owned/controlled by X, emit OWNS/CONTROLS with explicit sourceCanonicalPath/sourceType/sourceName = X, target = CURRENT FRAGMENT.
2. Beneficial vs nominal ownership: explicitly separate direct legal owner, indirect owner, and beneficial owner. Use qualifiers.share_type=direct|indirect|beneficial when inferable. Capture voting rights separately from equity when they diverge via metrics/qualifiers.voting_rights_percent.
3. Control: CONTROLS (controller→controlled): ORG/PERSON/COMMUNITY→ORG/FACILITY. State control, voting rights, board control, contractual control, golden share, veto rights, trustee mandate. Who controls at each level? Do NOT model external audit/certification oversight as CONTROLS; use ASSURES with assurance_type.
4. Full board: BOARD_MEMBER_OF (PERSON→ORG). All board members for significant organizations, especially where board seats explain beneficial control or proxy ownership.
5. Management: MANAGES (manager→managed). CEO, director, operator — formal authority over org/subsidiary. Full C-suite: EMPLOYED_BY (PERSON→ORG) for employment. FOUNDED (PERSON→ORG) for founders.
6. Investment: INVESTS_IN (investor→target). Include metrics (investment_amount, round, valuation) when known.
7. Community influence layer: HAS_SUBGROUP for dynasty/tribe/family branches, MEMBER_OF for people whose lineage materially affects authority, AFFILIATED_WITH for community/person → formal organization ties, OPERATES_VIA for community/org → media channel when the channel is used for mobilization, coordination, or control signaling.
8. Government hierarchy: ADMINISTERS (GOV/ORG with /gov/ path → subordinate GOV or PROGRAM). Full hierarchy to leaf agencies.
9. External assurance in governance: ASSURES for auditors and certifiers linked to this org/facility/program (do not emit AUDITED_BY legacy inverse).
10. Return entities (parents, subsidiaries, board members, investors, gov bodies, auditors/certifiers, communities, media channels, programs) that should be explored further (child jobs will recurse).

INTEGRATION_TYPE qualifier (for OWNS): When the ownership fits a known pattern, add qualifiers.integration_type:
- "vertical": subsidiary is part of vertically integrated supply chain (e.g., BYD owning battery/cell producers)
- "partnership": JV, strategic stake, or alliance structure (e.g., European OEM–supplier equity ties)
- "arm's-length": pure financial investment, no operational integration

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if any },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?", "metrics": { "ownership_percent", "voting_rights_percent", "investment_amount", ... }, "qualifiers": { "integration_type"?: "vertical|partnership|arm's-length", "share_type"?: "direct|indirect|beneficial", "control_type"?: "equity|board|golden_share|veto|trustee|state|contractual", "mechanism"?: "board_majority|golden_share|veto_right|trustee_mandate|nominee_structure", "source_role"?: "owner|investor|assurer|controller|subsidiary", "target_role"?: "owned_entity|investee|assuree|controlled_entity|parent_owner" } } ],
  "supplyChains": []
}

%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeGovernance), taxonomy.ExtendedCanonicalPathRulesPrompt(), geoContext, existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

// buildAssuranceEnrichmentPrompt builds a prompt for Third Line: audit, certification, regulatory oversight.
func (a *ResearchAgent) buildAssuranceEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify external auditors, certifiers, regulators; audit firm, certifications, regulatory oversight."
	}
	geoContext := geoAndSectorContext(frag)
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for ASSURANCE context (Third Line: independent audit, certification, regulatory oversight). Aim for COMPLETE assurance picture:

ENTITY TYPES in assurance: %s. Assurers are audit firms, certifiers, regulators. Assurees are orgs, facilities, programs.

CANONICAL PATH RULES (targetCanonicalPath MUST follow):
%s

%s
%s
%s
%s

Use ONLY these relation types (canonical): %s.
For ASSURES: add qualifiers.assurance_type = "audit" | "certification" | "regulatory" | "inspection".
Direction: assurer (source) → assuree (target). E.g. audit firm ASSURES org, certifier ASSURES org.
If CURRENT FRAGMENT is audited/certified/inspected by X, set explicit sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = CURRENT FRAGMENT.
Do NOT emit AUDITED_BY (legacy inverse direction assuree → assurer).
REGULATES: regulator (source) → regulated entity (target). JURISDICTION RULE: use only for domestic regulatory oversight within the regulator's own jurisdiction. For sanctions, trade restrictions, or extra-territorial enforcement against foreign entities use CONTROLS_ACCESS or IMPOSES_CONDITION instead.

WHAT TO FIND (prioritize completeness):
1. External audit: Who audits this org/facility? ASSURES(assurance_type:audit) from auditor to org. Include audit firm legal name.
2. Certifications: ISO, sector-specific (e.g. FDA approval, mining license, banking license). ASSURES(assurance_type:certification). Certifier → org.
3. Regulatory oversight: Which domestic regulators oversee this entity within their jurisdiction? REGULATES (regulator→entity). Or ASSURES(assurance_type:regulatory) when regulator grants license/approval. For foreign sanctions or extra-territorial restrictions use CONTROLS_ACCESS.
4. Inspections: ASSURES(assurance_type:inspection) for inspection bodies.
5. Return assurers (audit firms, certifiers, regulators) that should be explored further (child jobs will recurse).

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if any },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?", "qualifiers": { "assurance_type"?: "audit|certification|regulatory|inspection", "source_role"?: "assurer", "target_role"?: "assuree" } } ],
  "supplyChains": []
}

%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeAssurance), taxonomy.ExtendedCanonicalPathRulesPrompt(), geoContext, existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeAssurance), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

// buildMarketEnrichmentPrompt builds a prompt for market participation, sizing, concentration, submarkets.
func (a *ResearchAgent) buildMarketEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify market participation, sub-markets, key players, market sizing (TAM, CAGR, HHI, CR4), geographic concentration."
	}
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for MARKET context. Aim for COMPLETE market picture:

ENTITY TYPES in market: %s. PARTICIPATES_IN source can be active market players; TRADED_IN source is PRODUCT. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s
%s
%s

Use ONLY these relation types: %s.
Do NOT use DOMINATES, COMPETES_WITH, or other non-market types.
STRICT DIRECTION/TYPING RULES (MANDATORY):
- PARTICIPATES_IN direction is participant -> market.
  - source MUST be ORGANIZATION/FACILITY/COMMUNITY/PROGRAM.
  - target MUST be MARKET and targetCanonicalPath MUST start with /market/.
  - targetCanonicalPath MUST NOT start with /domain/.
  - For ORGANIZATION ↔ MARKET pairs, use PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES (domestic oversight), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES.
- TRADED_IN direction is PRODUCT -> MARKET.
  - source MUST be PRODUCT.
  - target MUST be MARKET.
  - Use for product trading, benchmark pricing, or market venue context. Do NOT use PARTICIPATES_IN for PRODUCT -> MARKET.
- HAS_MARKET_SEGMENT direction is parent-market -> child-market (segment).
  - source and target MUST both be MARKET paths (/market/...).
  - Ideal rule: child path = parent path + exactly one segment (e.g. /market/sa/finance -> /market/sa/finance/banking; /market/global/technology -> /market/global/technology/semiconductors).
  - targetCanonicalPath MUST NOT start with /domain/.
- If CURRENT FRAGMENT is MARKET and you need player -> THIS MARKET, you MUST set explicit source:
  - sourceCanonicalPath/sourceType/sourceName = player
  - targetCanonicalPath = THIS FRAGMENT market path
  - relationType = PARTICIPATES_IN
- Never emit non-domain relations with targetCanonicalPath /domain/... in this strategy.

WHAT TO FIND (prioritize completeness):
1. Market participation: PARTICIPATES_IN (entity→market). Source: ORGANIZATION, FACILITY, COMMUNITY, PROGRAM, or PERSON. metrics.market_share (0..1 or percent). marketRole: participant, leader, dominant.
1b. Traded products: TRADED_IN (product→market). Use when the market explicitly trades, benchmarks, or price-discovers the product.
1c. Community influence and mobilization: use HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH, and OPERATES_VIA when community structure, affiliation networks, or communication channels materially shape market access, informal distribution, boycotts, demand coordination, or dominant-player behavior.
2. Full market hierarchy: HAS_MARKET_SEGMENT (MARKET→MARKET, parent→segment). Child path = parent path + one segment. Build 3 levels when possible (e.g. market→sub-market→segment).
3. Top 10 players: For MARKET entities, find top 10 player entities (orgs, facilities, communities, programs) by market share and emit PARTICIPATES_IN as player -> market using explicit sourceCanonicalPath.
4. Full sizing: fragment.state: /metric/market/tam_usd, /metric/market/cagr_percent, /metric/market/hhi, /metric/market/cr4_percent. Only if you find reliable data; do NOT fabricate.
5. Geographic split: fragment.state with geo distribution (e.g. /metric/market/geo_share_us, /metric/market/geo_share_eu) when relevant.
6. Return key players (orgs, facilities, communities, programs) that should be explored further (child jobs will recurse).

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields: state with /metric/market/tam_usd, cagr_percent, hhi, cr4_percent when found },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "metrics": { "market_share": ... }, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeMarket), taxonomy.ExtendedCanonicalPathRulesPrompt(), existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeMarket), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

// buildStrategicEnrichmentPrompt builds a prompt for strategic layer (decision points, mechanisms, gatekeepers).
func (a *ResearchAgent) buildStrategicEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify decision points, mechanisms, gatekeepers, control/access relations, export controls, sanctions, licensing."
	}
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for STRATEGIC context only.

ENTITY TYPES in strategic: %s. Gatekeepers can be org, gov, community, or person. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s
%s
%s

Use ONLY these relation types: %s.
Do NOT use generic relation types.

WHAT TO FIND (downstream impact is required):
1. Decision points: DECISION_POINT /decision/{domain}/{slug} — concrete decisions (Grant License, Block Export, Approve). MUST have CONTROLS_ACCESS or IMPOSES_CONDITION to concrete entities (what it affects). GATEKEEPS_DECISION (who decides). Do NOT create decision points without a clear affected target.
2. Mechanisms: MECHANISM /mechanism/{category}/{slug} — instruments (Entity List, Banking License, Export Control). MUST have APPLIES_TO to concrete entities (what/who it affects). USES_MECHANISM (who uses it). Do NOT create mechanisms without a clear affected target.
3. Control/access: CONTROLS_ACCESS — decision/org blocks/enables access to WHAT (target = org/facility/product/market). IMPOSES_CONDITION — who imposes rules/costs on WHOM.
4. Gatekeepers: GATEKEEPS_DECISION — who has authority over the decision.
5. Community gatekeeping and mobilization: use HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH, and OPERATES_VIA when tribe/dynasty/family/community structure or its media channels materially shape access decisions, sanctions evasion, distribution of permissions, or pressure campaigns.

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if any },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For GATEKEEPS_DECISION and USES_MECHANISM: set sourceCanonicalPath, sourceType, sourceName when the SOURCE is not the current fragment (e.g. gatekeeper org, mechanism user).
%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeStrategic), taxonomy.ExtendedCanonicalPathRulesPrompt(), existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeStrategic), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

// buildPolicyImpactEnrichmentPrompt builds a prompt for policy impact channels and exposure graph.
func (a *ResearchAgent) buildPolicyImpactEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify policy parameters, affected segments, impact channels, and regulator/program links."
	}
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for POLICY IMPACT context.

ENTITY TYPES in policy impact: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s
%s
%s

Use ONLY these relation types: %s.
Do NOT use generic relation types outside this list.
STRICT DIRECTION/TYPING RULES (MANDATORY):
- Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/.
- Keep the graph directed and role-first. For core policy-impact edges, ALWAYS include qualifiers.source_role, qualifiers.target_role, and qualifiers.direction_basis using these canonical pairs:
  GATEKEEPS_DECISION gatekeeper->decision_point,
  USES_MECHANISM mechanism_user->mechanism,
  CONTROLS_ACCESS controller->access_subject,
  IMPOSES_CONDITION imposer->constrained_entity,
  APPLIES_TO mechanism->applicable_entity,
  ADMINISTERS administrator->administered_entity,
  IN_GEO_SCOPE scope_owner->geo_scope,
  AFFECTS impact_source->affected_entity.
- PARTICIPATES_IN direction is participant -> market.
  - source MUST be ORGANIZATION/FACILITY/COMMUNITY/PROGRAM.
  - target MUST be MARKET and targetCanonicalPath MUST start with /market/.
  - For ORGANIZATION ↔ MARKET pairs, use PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES (domestic oversight), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES.
  - When CURRENT FRAGMENT is MARKET, use explicit sourceCanonicalPath/sourceType/sourceName for participant -> THIS MARKET.
- TRADED_IN direction is PRODUCT -> MARKET.
  - source MUST be PRODUCT, target MUST be MARKET.
  - Use for product trading/price-discovery context only.
- REGULATES/ADMINISTERS must originate from regulator/admin source (/gov/...), not from affected market/entity. REGULATES is for same-jurisdiction oversight only; for sanctions use CONTROLS_ACCESS.
- GATEKEEPS_DECISION is gatekeeper -> decision point. USES_MECHANISM is decision/org/gov/community/program -> mechanism.
- APPLIES_TO is mechanism -> covered entity. CONTROLS_ACCESS and IMPOSES_CONDITION are controller/imposer -> affected entity. IN_GEO_SCOPE is policy source/mechanism -> jurisdiction/location. AFFECTS is upstream impact source -> downstream affected entity.
- For any relation where semantic source is not CURRENT FRAGMENT, explicit sourceCanonicalPath/sourceType/sourceName is REQUIRED.

BUILD FIVE DIRECTED LAYERS:
1. Policy source layer: gatekeepers, regulators, administrators, programs, and decision points that set or authorize the move.
2. Mechanism layer: concrete mechanisms/instruments used to transmit the policy.
3. Scope and exposure layer: explicit IN_GEO_SCOPE, APPLIES_TO, REGULATES, CONTROLS_ACCESS, IMPOSES_CONDITION edges to markets, orgs, products, communities, programs, and geographies.
4. Transmission and response layer: downstream AFFECTS/INFLUENCES edges, including community-mediated and media-mediated pass-through when they materially change compliance, lobbying, evasion, adoption, or sentiment.
5. Observability layer: how the effect would actually be measured, with timing lags and confidence.

WHAT TO FIND:
1. Policy instruments and mechanisms (tax rate changes, exemptions, compliance controls, enforcement mechanisms).
2. Regulator/program authority chains (who sets/administers policy; what is in scope).
3. Geo scope and exposure links to affected segments (markets, organizations, products, communities, programs, locations).
4. Transmission channels suitable for propagation modeling (formal, market, community, and media channels).
4a. Community-mediated transmission: use HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH, and OPERATES_VIA when informal networks, clan/dynasty structures, or controlled channels materially change policy adoption, compliance, lobbying, evasion, or pass-through.
4b. Media/community tail: when a community, organization, or program transmits the policy via a specific channel, add OPERATES_VIA and continue the downstream path instead of stopping at the community node.
5. QUANTITATIVE QUALIFIERS (CRITICAL for scenario modeling — provide best estimates even when uncertain):
   - For REGULATES/CONTROLS_ACCESS/IMPOSES_CONDITION edges: qualifiers.rate_from, qualifiers.rate_to, qualifiers.effective_date when the instrument has numeric parameters.
   - For IN_GEO_SCOPE edges: qualifiers.scope_basis ("jurisdiction"|"market_rule"|"program_area"|"administrative_boundary") when inferable.
   - For exposure edges (to markets/orgs/communities): qualifiers.pass_through_estimate (0.0-1.0: share of cost passed downstream),
     qualifiers.elasticity_hint ("inelastic"|"unit_elastic"|"elastic"|"unknown"),
     qualifiers.tax_base_coverage ("full"|"partial"|"exempt"),
     qualifiers.sector_sensitivity ("low"|"medium"|"high").
   - For AFFECTS/INFLUENCES edges: qualifiers.channel_type ("formal_regulation"|"price_signal"|"behavioral"|"substitution"|"evasion"|"community_signal"|"media_signal"),
     qualifiers.lag_quarters (estimated delay in quarters before effect materializes),
     qualifiers.observability ("direct"|"proxy"|"modeled").
6. Observability metadata: for each major impact channel, note HOW the effect would be measured
   (e.g., national statistics agency data, customs declarations, tax collection reports).
   Add qualifiers.measurement_lag_months and qualifiers.measurement_confidence (0.0-1.0) when estimable.

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if any },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For relations where SOURCE is not the current fragment, set sourceCanonicalPath, sourceType, sourceName.
%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.ExtendedCanonicalPathRulesPrompt(), existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

func (a *ResearchAgent) buildEcosystemEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Map all actors in this ecosystem, their roles, mutual relationships, power dynamics, and dependencies."
	}
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for ECOSYSTEM/STAKEHOLDER MAPPING context.

ENTITY TYPES in ecosystem context: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s
%s
%s

Use ONLY these relation types: %s.
Do NOT use generic relation types outside this list.
STRICT DIRECTION/TYPING RULES (MANDATORY):
- Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/.
- PARTICIPATES_IN direction is participant -> market. source MUST be ORGANIZATION/FACILITY/COMMUNITY/PROGRAM. target MUST be MARKET.
- TRADED_IN direction is PRODUCT -> MARKET for traded commodity/product exposure.
- OWNS direction is owner -> owned.
- SUBSIDIARY_OF direction is subsidiary -> parent.
- REGULATES direction is regulator -> regulated (same-jurisdiction domestic oversight only; for sanctions/restrictions use CONTROLS_ACCESS or IMPOSES_CONDITION).
- INFLUENCES direction is influencer -> influenced.
- For any relation where semantic source is not CURRENT FRAGMENT, explicit sourceCanonicalPath/sourceType/sourceName is REQUIRED.

WHAT TO FIND:
1. All significant actors in this ecosystem: organizations, government bodies, communities, persons, facilities.
2. Their roles and positions: producers, regulators, investors, competitors, partners, gatekeepers.
3. Mutual relationships: supply links, ownership/control, partnerships, competition, regulatory oversight.
3a. Community structure and mobilization: capture HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH, and OPERATES_VIA when they explain how informal groups coordinate, influence, or connect to formal actors and channels.
4. Power dynamics: who influences whom, dependencies, bottlenecks, critical nodes.
5. Competitive positions: market shares, dominance, niche roles.
6. Geographic scope: where actors operate and interact.

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if any },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For relations where SOURCE is not the current fragment, set sourceCanonicalPath, sourceType, sourceName.
%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeEcosystem), taxonomy.ExtendedCanonicalPathRulesPrompt(), existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

// EnrichRegulatoryImpactFull performs holistic regulatory impact research in a single LLM call.
// Builds the full impact DAG: decision points, mechanisms, affected segments.
func (a *ResearchAgent) EnrichRegulatoryImpactFull(ctx context.Context, query RegulatoryImpactQuery) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildRegulatoryImpactFullPrompt(query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredRelations, _ := FilterDomainRelations(ctx, taxonomy.DomainModePolicyImpact, out.Relations)
	return filteredRelations, nil
}

// EnrichRegulatoryImpactRefinement asks for additional policy-impact edges given an existing graph.
// Intended for a second pass when job state sets regulatoryImpactRefinePass, or when the first
// pass returned a sparse relation set. New relations are filtered with the policy_impact allowlist.
func (a *ResearchAgent) EnrichRegulatoryImpactRefinement(ctx context.Context, domainCP string, existing []DiscoveredRelation) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	domainCP = strings.TrimSpace(domainCP)
	prompt := a.buildRegulatoryImpactRefinementPrompt(domainCP, existing)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredRelations, _ := FilterDomainRelations(ctx, taxonomy.DomainModePolicyImpact, out.Relations)
	return filteredRelations, nil
}

func (a *ResearchAgent) buildRegulatoryImpactRefinementPrompt(domainCP string, existing []DiscoveredRelation) string {
	seen := map[string]struct{}{}
	var nodeList []string
	addNode := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		nodeList = append(nodeList, path)
	}
	for _, r := range existing {
		addNode(r.TargetCanonicalPath)
		addNode(r.SourceCanonicalPath)
	}
	if domainCP != "" {
		addNode(domainCP)
	}
	maxNodes := 48
	if len(nodeList) > maxNodes {
		nodeList = nodeList[:maxNodes]
	}
	var edgeLines []string
	maxEdges := 64
	for i, r := range existing {
		if i >= maxEdges {
			break
		}
		s := strings.TrimSpace(r.SourceCanonicalPath)
		t := strings.TrimSpace(r.TargetCanonicalPath)
		if s == "" || t == "" {
			continue
		}
		edgeLines = append(edgeLines, fmt.Sprintf("%s --%s--> %s", s, strings.TrimSpace(taxonomy.NormalizeRelationType(r.RelationType)), t))
	}
	nodesBlock := strings.Join(nodeList, "\n")
	edgesBlock := strings.Join(edgeLines, "\n")
	if nodesBlock == "" {
		nodesBlock = "(none)"
	}
	if edgesBlock == "" {
		edgesBlock = "(none)"
	}
	return fmt.Sprintf(`You are a research agent. DENSIFY a regulatory / policy impact graph.

DOMAIN CONTRACT ANCHOR (may appear in edges): %s

KNOWN NODES (canonical paths; prefer linking these before inventing new entities):
%s

KNOWN EDGES (do not repeat these exact triples):
%s

TASK: Add NEW relations only. Connect missing layers:
- GOV / ORG -> DECISION_POINT (GATEKEEPS_DECISION, REGULATES where domestic, CONTROLS_ACCESS for sanctions)
- DECISION_POINT -> MECHANISM (USES_MECHANISM)
- MECHANISM -> MARKET / ORG / FACILITY (APPLIES_TO, IMPOSES_CONDITION, INFLUENCES)

Use ONLY these relation types: %s.
ENTITY TYPES: %s.

CANONICAL PATH RULES:
%s

Return JSON:
{
  "fragment": null,
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "qualifiers": {}, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

Set sourceCanonicalPath/sourceType/sourceName for every relation where source is not obvious.
Return ONLY valid JSON. No markdown.`,
		domainCP,
		nodesBlock,
		edgesBlock,
		relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModePolicyImpact),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
	)
}

func (a *ResearchAgent) buildRegulatoryImpactFullPrompt(query RegulatoryImpactQuery) string {
	instrument := strings.TrimSpace(query.Instrument)
	jurisdiction := strings.TrimSpace(query.Jurisdiction)
	if jurisdiction == "" {
		jurisdiction = "global"
	}
	depth := query.Depth
	if depth < 3 || depth > 5 {
		depth = 4
	}
	impactScope := strings.TrimSpace(query.ImpactScope)
	if impactScope == "" {
		impactScope = "economy-wide"
	}
	sectorFilter := strings.Join(query.SectorFilter, ", ")

	var paramBlock string
	if len(query.Parameters) > 0 {
		var parts []string
		for k, v := range query.Parameters {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		paramBlock = "Parameters: " + strings.Join(parts, ", ")
	}

	intent := fmt.Sprintf("instrument=%s, jurisdiction=%s, impactScope=%s, depth=%d", instrument, jurisdiction, impactScope, depth)
	if sectorFilter != "" {
		intent += ", sectorFilter=[" + sectorFilter + "]"
	}
	if paramBlock != "" {
		intent += ", " + paramBlock
	}

	return fmt.Sprintf(`You are a research agent. Build a FULL regulatory impact graph as a HOLISTIC structure.

TASK: Map the full impact graph for: %s
Reason: %s

Use ONLY these relation types: %s.
ENTITY TYPES: %s.

CANONICAL PATH RULES:
%s

%s

BUILD FIVE DIRECTED LAYERS:
1. POLICY SOURCE LAYER: Government entities, administrators, communities, and decision points linked by GATEKEEPS_DECISION / REGULATES / ADMINISTERS / CONTROLS_ACCESS.
2. MECHANISM LAYER: DECISION_POINT/ORG/GOV/COMMUNITY/PROGRAM --USES_MECHANISM--> MECHANISM.
3. SCOPE LAYER: source/mechanism --IN_GEO_SCOPE--> GEO and MECHANISM --APPLIES_TO--> MARKET/ORG/FACILITY/PRODUCT/COMMUNITY/PROGRAM.
4. IMPACT LAYER: source/mechanism --IMPOSES_CONDITION/AFFECTS/INFLUENCES--> affected segments, organizations, products, communities, media channels, and programs.
5. RESPONSE / OBSERVABILITY LAYER: continue through community and media channels when they transmit compliance, lobbying, evasion, sentiment, or adoption; include measurable lag/observability qualifiers.
Note: use REGULATES only for domestic jurisdictional authority. For sanctions, trade bans, or extra-territorial restrictions use CONTROLS_ACCESS or IMPOSES_CONDITION.
Keep the graph directed and role-first. For core policy-impact edges, ALWAYS include qualifiers.source_role, qualifiers.target_role, and qualifiers.direction_basis using canonical pairs:
- gatekeeper->decision_point for GATEKEEPS_DECISION
- mechanism_user->mechanism for USES_MECHANISM
- controller->access_subject for CONTROLS_ACCESS
- imposer->constrained_entity for IMPOSES_CONDITION
- mechanism->applicable_entity for APPLIES_TO
- administrator->administered_entity for ADMINISTERS
- scope_owner->geo_scope for IN_GEO_SCOPE
- impact_source->affected_entity for AFFECTS

QUANTITATIVE QUALIFIERS (CRITICAL):
- For REGULATES/CONTROLS_ACCESS/IMPOSES_CONDITION: qualifiers.rate_from, qualifiers.rate_to, qualifiers.effective_date
- For IN_GEO_SCOPE: qualifiers.scope_basis when inferable
- For exposure edges: qualifiers.pass_through_estimate (0.0-1.0), qualifiers.elasticity_hint, qualifiers.sector_sensitivity
- For AFFECTS/INFLUENCES: qualifiers.channel_type, qualifiers.lag_quarters, qualifiers.observability
%s

Return a JSON object:
{
  "fragment": null,
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "qualifiers": {}, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For each relation, set sourceCanonicalPath/sourceType/sourceName explicitly.
Chain depth target: %d hops from instrument to furthest affected segment.

Return ONLY valid JSON. No markdown.`, intent, strings.TrimSpace(query.Reason), relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.EntityTypesForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock(), strategyMetricValueFormatBlock(), depth)
}

// EnrichGovernanceFull performs holistic governance/ownership research in a single LLM call.
// Builds ownership trees, control layers, and board/people structures.
func (a *ResearchAgent) EnrichGovernanceFull(ctx context.Context, query GovernanceQuery) ([]DiscoveredRelation, error) {
	return a.EnrichGovernanceFullForContract(ctx, query, "")
}

func (a *ResearchAgent) EnrichGovernanceFullForContract(ctx context.Context, query GovernanceQuery, contractType string) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildGovernanceFullPromptForContract(query, contractType)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredRelations, _ := FilterDomainRelations(ctx, taxonomy.DomainModeGovernance, out.Relations)
	return filteredRelations, nil
}

func (a *ResearchAgent) buildGovernanceFullPrompt(query GovernanceQuery) string {
	return a.buildGovernanceFullPromptForContract(query, "")
}

func (a *ResearchAgent) buildGovernanceFullPromptForContract(query GovernanceQuery, contractType string) string {
	target := strings.TrimSpace(query.Target)
	targetType := strings.TrimSpace(query.TargetType)
	if targetType == "" {
		targetType = "org"
	}
	direction := strings.TrimSpace(query.Direction)
	if direction == "" {
		direction = "both"
	}
	depth := query.Depth
	if depth < 3 || depth > 6 {
		depth = 4
	}
	geoFilter := strings.Join(query.GeoFilter, ", ")

	intent := fmt.Sprintf("target=%s, targetType=%s, direction=%s, depth=%d, includeBoard=%v", target, targetType, direction, depth, query.IncludeBoard)
	if geoFilter != "" {
		intent += ", geoFilter=[" + geoFilter + "]"
	}
	contractType = strings.TrimSpace(strings.ToLower(contractType))
	governanceMode := "ownership"
	if contractType == "control-map" {
		governanceMode = "control"
	}

	boardBlock := ""
	if query.IncludeBoard {
		boardBlock = `
BOARD/PEOPLE LAYER:
- PERSON --BOARD_MEMBER_OF--> ORGANIZATION
- PERSON --EMPLOYED_BY--> ORGANIZATION (for C-suite / key executives)
- Include key persons: CEO, CFO, board chairman, major shareholders (individuals).`
	}
	subtypeBlock := `
OWNERSHIP MODE (default):
- Prioritize UBO, holding chains, direct vs indirect beneficial stakes, intermediate holding vehicles.
- When ownership and control diverge, preserve both: OWNS for equity stake, CONTROLS for non-equity or governance control.
- Capture nominee structures, trustees, proxy vehicles, and voting-rights divergence explicitly when evidence exists.`
	if governanceMode == "control" {
		subtypeBlock = `
CONTROL MODE (control-map):
- Prioritize operational, strategic, and administrative control structures over pure cap table mapping.
- Capture control mechanisms explicitly: board majority, golden share, veto rights, ministerial appointment, operating mandate, state administration.
- Use CONTROLS / MANAGES / ADMINISTERS as primary edges; keep OWNS / INVESTS_IN only as explanatory context when they clarify why control exists.`
	}

	return fmt.Sprintf(`You are a research agent. Build a FULL governance/ownership structure as a HOLISTIC graph.

TASK: Map the ownership and control structure for: %s
Reason: %s

Use ONLY these relation types: %s.
ENTITY TYPES: %s.

CANONICAL PATH RULES:
%s

%s

BUILD THESE LAYERS:
1. OWNERSHIP TREE (%s):
   - UBO (Ultimate Beneficial Owner: ORG, PERSON, or COMMUNITY dynasty/tribe) --OWNS--> Holding --OWNS--> Subsidiary
   - ORG --SUBSIDIARY_OF--> Parent
2. CONTROL LAYER:
   - ORG/GOV/COMMUNITY/PERSON --CONTROLS/MANAGES--> ORG
   - Preserve ownership vs control divergence when voting rights, board control, golden shares, veto rights, trustee mandates, or nominee structures are the real mechanism
3. INVESTMENT:
   - ORG --INVESTS_IN--> ORG
4. PEOPLE / GOVERNANCE LAYER:
   - PERSON --BOARD_MEMBER_OF--> ORGANIZATION
   - PERSON --EMPLOYED_BY--> ORGANIZATION
   - PERSON/ORG --FOUNDED--> ORGANIZATION|PROGRAM
5. COMMUNITY / LINEAGE LAYER:
   - COMMUNITY (dynasty/tribe/family network) --HAS_SUBGROUP--> COMMUNITY
   - PERSON --MEMBER_OF--> COMMUNITY when lineage materially affects ownership/control
   - COMMUNITY or PERSON --AFFILIATED_WITH--> ORGANIZATION when the tie explains beneficial ownership, patronage, or command structure
   - COMMUNITY or ORGANIZATION --OPERATES_VIA--> MEDIA_CHANNEL when the channel is a control, mobilization, or signaling instrument
%s
%s

QUALIFIERS on OWNS edges: qualifiers.share (ownership percentage 0.0-1.0), qualifiers.share_type (direct|indirect|beneficial).
QUALIFIERS on CONTROLS edges: qualifiers.control_type (operational|strategic|regulatory|board|trustee|state|contractual), qualifiers.mechanism (board_majority|golden_share|veto_right|trustee_mandate|nominee_structure|ministerial_appointment|operating_mandate).
When evidence exists, also capture metrics/qualifiers.voting_rights_percent separately from equity stake.

Return a JSON object:
{
  "fragment": null,
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "qualifiers": {}, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For each relation, set sourceCanonicalPath/sourceType/sourceName explicitly.
Chain depth target: %d levels from target entity.

Return ONLY valid JSON. No markdown.`, intent, strings.TrimSpace(query.Reason), relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance), taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeGovernance), taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock(), direction, boardBlock, subtypeBlock, depth)
}

// EnrichEcosystemFull performs holistic ecosystem mapping in a single LLM call.
// Discovers all significant actors and their multi-dimensional relationships.
func (a *ResearchAgent) EnrichEcosystemFull(ctx context.Context, query EcosystemQuery) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildEcosystemFullPrompt(query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredRelations, _ := FilterDomainRelations(ctx, taxonomy.DomainModeEcosystem, out.Relations)
	return filteredRelations, nil
}

func (a *ResearchAgent) buildEcosystemFullPrompt(query EcosystemQuery) string {
	focus := strings.TrimSpace(query.Focus)
	focusType := strings.TrimSpace(query.FocusType)
	if focusType == "" {
		focusType = "sector"
	}
	depth := query.Depth
	if depth < 3 || depth > 5 {
		depth = 4
	}
	actorTypes := strings.Join(query.ActorTypes, ", ")
	if actorTypes == "" {
		actorTypes = "ORGANIZATION, GOV, PERSON, FACILITY, COMMUNITY"
	}
	sectorFilter := strings.Join(query.SectorFilter, ", ")
	geoFilter := strings.Join(query.GeoFilter, ", ")

	intent := fmt.Sprintf("focus=%s, focusType=%s, depth=%d, actorTypes=[%s]", focus, focusType, depth, actorTypes)
	if sectorFilter != "" {
		intent += ", sectorFilter=[" + sectorFilter + "]"
	}
	if geoFilter != "" {
		intent += ", geoFilter=[" + geoFilter + "]"
	}

	return fmt.Sprintf(`You are a research agent. Map a FULL ecosystem as a HOLISTIC multi-actor relationship graph.

TASK: Map all actors and relationships in ecosystem: %s
Reason: %s

Use ONLY these relation types: %s.
ENTITY TYPES: %s.
Target actor types: %s.

CANONICAL PATH RULES:
%s

%s

DISCOVER AND MAP:
1. ALL significant actors: producers, consumers, regulators, investors, competitors, partners, gatekeepers.
2. MULTI-DIMENSIONAL relationships between actors:
   - Supply: SUPPLIES_TO, DEPENDS_ON
   - Ownership/Control: OWNS, SUBSIDIARY_OF, CONTROLS, INVESTS_IN
   - Competition: COMPETES_WITH
   - Cooperation: PARTNERS_WITH
   - Regulation: REGULATES (domestic jurisdiction), CONTROLS_ACCESS (sanctions/restrictions)
   - Influence: INFLUENCES
   - Market: PARTICIPATES_IN
   - Geographic: LOCATED_IN, OPERATES_IN
3. Power dynamics: who are the key influencers, gatekeepers, and bottleneck nodes?
4. Competitive positions: market shares, dominance, niche roles where known.

Return a JSON object:
{
  "fragment": null,
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "qualifiers": {}, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For each relation, set sourceCanonicalPath/sourceType/sourceName explicitly.
Aim for %d levels of relationship depth from the focal entity/area.

Return ONLY valid JSON. No markdown.`, intent, strings.TrimSpace(query.Reason), relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem), taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeEcosystem), actorTypes, taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock(), depth)
}

// EnrichAffectedZoneFull performs holistic affected-zone discovery in a single LLM call.
// Discovers zones across condition types (military_conflict, natural_disaster, etc.) for the given geo.
func (a *ResearchAgent) EnrichAffectedZoneFull(ctx context.Context, query AffectedZoneQuery) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildAffectedZoneFullPrompt(query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	// AFFECTS is considered generic/strategic. We can use Strategic constraints or leave it unfiltered.
	// We'll leave it unfiltered as it's a specialized edge type not bound to standard domains.
	return out.Relations, nil
}

func (a *ResearchAgent) buildAffectedZoneFullPrompt(query AffectedZoneQuery) string {
	geo := strings.TrimSpace(query.Geo)
	if geo == "" {
		geo = "global"
	}
	reason := strings.TrimSpace(query.Reason)
	if reason == "" {
		reason = "Discover affected zones for risk and exposure analysis"
	}
	title := strings.TrimSpace(query.Title)
	if title != "" {
		reason = title + ". " + reason
	}
	return fmt.Sprintf(`You are a research agent. Discover AFFECTED_ZONE entities and their FULL related graph in a HOLISTIC single query (like supply-chain: complete picture in one pass).

TASK: Find all significant affected zones in geo: %s and for EACH zone discover related entities.
Reason: %s

%s

PHASE 1 — Discover zones (5-15 across condition types):
- military_conflict (conflict zones, disputed territories)
- natural_disaster (earthquake_affected, flood_zone, wildfire, etc.)
- political_tension (unrest, sanctions-affected areas)
- humanitarian_crisis (displacement, famine, disease outbreak zones)

PHASE 2 — For EACH zone, discover related entities in the SAME holistic pass:
- LOCATED_IN: zone → GEO_REGION (parent geo where zone is located). Set sourceCanonicalPath=zone path, sourceType=AFFECTED_ZONE.
- AFFECTS: zone → FACILITY (affected plants, ports, warehouses), ORGANIZATION (affected companies, NGOs, gov bodies), PRODUCT (affected commodities), PROGRAM (humanitarian programs). Set sourceCanonicalPath=zone path.
- TRIGGERED_BY: zone → EVENT (earthquake, flood, conflict event that caused the zone). Set sourceCanonicalPath=zone path.

PHASE 2b — For military_conflict zones ONLY:
- Sides: AFFECTS zone → ORGANIZATION/GOV/COMMUNITY (combatants). qualifiers: conflictRole=combatant, conflictSide=side_a|side_b.
- Force strength: qualifiers.forceStrengthEstimate ("major"|"minor"|"paramilitary"|"state" when known).
- Resources used: AFFECTS zone → PRODUCT (weapons, equipment). qualifiers: conflictRole=resource_used.
- External support: AFFECTS zone → ORGANIZATION/GOV (backers). qualifiers: conflictRole=external_supporter, supportsSide=side_a|side_b.
Discover at least 2 sides, forces, key resources, 1-3 external supporters per side when evidence exists.

PHASE 2c — For natural_disaster zones (earthquake_affected, flood_zone, wildfire, etc.) ONLY:
- Damaged assets: AFFECTS zone → FACILITY (damaged plants, ports, hospitals, power), ORGANIZATION (affected companies). qualifiers: disasterRole=affected, damageScope ("partial"|"severe"|"destroyed" when known).
- Response actors: AFFECTS zone → ORGANIZATION/GOV/PROGRAM (relief agencies, first responders, gov disaster units). qualifiers: disasterRole=responder.
- Critical infrastructure at risk: AFFECTS zone → FACILITY (key sites). qualifiers: disasterRole=critical_infrastructure.
Discover 2-5 damaged/at-risk entities and 1-3 response actors per zone when evidence exists.

PHASE 2d — For political_tension zones (unrest, sanctions-affected) ONLY:
- Parties: AFFECTS zone → ORGANIZATION/GOV/COMMUNITY (sides to tension, protestors, opposition). qualifiers: tensionRole=party, tensionSide=side_a|side_b.
- Sanctions targets: AFFECTS zone → ORGANIZATION (entities under sanctions). qualifiers: tensionRole=sanctions_target.
- Mediators: AFFECTS zone → ORGANIZATION/GOV (mediators, observers). qualifiers: tensionRole=mediator.
- Disputed assets: AFFECTS zone → FACILITY/PRODUCT (disputed resources, assets). qualifiers: tensionRole=disputed.
Discover parties, sanctions targets when applicable, and mediators when evidence exists.

PHASE 2e — For humanitarian_crisis zones (displacement, famine, disease outbreak) ONLY:
- Affected populations: AFFECTS zone → GEO_REGION (displacement destinations, refugee host areas). qualifiers: crisisRole=displacement_flow.
- Relief actors: AFFECTS zone → PROGRAM/ORGANIZATION (NGOs, UN agencies, donors). qualifiers: crisisRole=relief_actor.
- Critical needs: AFFECTS zone → PRODUCT (food, medicine, shelter, water). qualifiers: crisisRole=critical_need.
- Access control: AFFECTS zone → ORGANIZATION/GOV (who controls access, blockades). qualifiers: crisisRole=access_controller.
Discover 1-3 relief actors, critical needs, and access bottlenecks when evidence exists.

RELATION TYPES: FOCUSES_ON (domain→zone), LOCATED_IN, AFFECTS, TRIGGERED_BY.

CANONICAL PATH RULES:
%s

ZONE PATH: /zone/{geo}/{condition_type}/{slug}. slug MUST be descriptive place/region name (northern-rakhine, gaza-strip, donbas). NEVER "na". When creating zone fragments, include radiusKm (approximate zone radius in km) ONLY when you have reliable, evidence-based data. Be conservative: prefer smaller, verifiable estimates; avoid overly broad interpretations. Do NOT extrapolate or guess—omit radiusKm if uncertain. Other types: /org/, /gov/, /fac/, /loc/, /product/, /program/, /event/ per rules.

%s

Return a JSON object:
{
  "fragment": null,
  "relations": [
    { "relationType": "FOCUSES_ON", "targetType": "AFFECTED_ZONE", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "radiusKm": 50 } },
    { "relationType": "LOCATED_IN", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "sourceName", "targetType": "GEO_REGION", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "radiusKm": 25 } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "sourceName", "targetType": "FACILITY|ORGANIZATION|PRODUCT|PROGRAM", "targetName", "targetCanonicalPath", "reason", "confidence" },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "ORGANIZATION", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "conflictRole": "combatant", "conflictSide": "side_a", "forceStrengthEstimate": "major" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "ORGANIZATION", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "conflictRole": "external_supporter", "supportsSide": "side_b" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "FACILITY", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "disasterRole": "affected", "damageScope": "severe" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "PROGRAM", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "disasterRole": "responder" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "ORGANIZATION", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "tensionRole": "sanctions_target" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "PRODUCT", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "crisisRole": "critical_need" } },
    { "relationType": "TRIGGERED_BY", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "sourceName", "targetType": "EVENT", "targetName", "targetCanonicalPath", "reason", "confidence" }
  ],
  "supplyChains": []
}

For zone relations (source or target is AFFECTED_ZONE): include qualifiers.radiusKm (numeric, km) ONLY when you have reliable, evidence-based data. Be conservative and verifiable—avoid overly broad interpretations. Omit radiusKm if uncertain.

CRITICAL: For zone→X relations, ALWAYS set sourceCanonicalPath, sourceType, sourceName to the zone. For FOCUSES_ON (domain→zone), source can be omitted.
Discover 5-15 zones and 2-8 related entities per zone (facilities, orgs, events, parent geo). Apply condition-type-specific blocks (2b–2e) per zone: military_conflict→sides/forces/support; natural_disaster→damage/responders; political_tension→parties/sanctions/mediators; humanitarian_crisis→relief/needs/access. Politically neutral, factual descriptions.

Return ONLY valid JSON. No markdown.`, geo, reason, taxonomy.ZoneVsEventDistinctionPrompt(), taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock())
}

// ---------------------------------------------------------------------------
// Holistic entity research methods (Phase 4 of Unified Post-Processing Pipeline)
// ---------------------------------------------------------------------------

// HolisticResearchInput holds context for holistic entity research.
type HolisticResearchInput struct {
	Fragment      *models.HorizonFragment
	OriginalQuery string
	DomainMode    string
	ChainContext  string
}

// EnrichHolisticSupplyChain performs domain-specific holistic research for an entity
// in the supply-chain domain context.
func (a *ResearchAgent) EnrichHolisticSupplyChain(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic supply-chain research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a supply chain analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

In the context of the analytical query above, provide a COMPREHENSIVE description of this entity's role in the supply chain:
- Upstream suppliers and raw material sources
- Downstream customers and distribution channels
- Key products/services in this context
- Logistics and infrastructure dependencies
- Bottleneck risks and single points of failure
- Market position and competitive alternatives

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

Return JSON object:
{
  "fragment": { partial fields to merge },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": [ { "nodes": [ { "canonicalPath", "name", "type", "role"? } ], "confidence", "reason", "transportVia"?: ["/fac/us/port-long-beach", "/fac/ae/jebel-ali-port"] } ]
}
Nodes MUST alternate actor→product. Include FACILITY where transformation happens (product→facility→product).

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		taxonomy.SupplyChainEdgeTypesString(),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeSupplyChain),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		strategySupplyChainsJSONContractBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticEcosystem performs domain-specific holistic research for ecosystem context.
func (a *ResearchAgent) EnrichHolisticEcosystem(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic ecosystem research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for an ecosystem analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's position in the ecosystem:
- Key partners, collaborators, and alliances
- Competitors and substitutes
- Dependencies on other actors (technology, resources, infrastructure)
- Influence and power dynamics
- Substitution risks and resilience factors

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeEcosystem),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticGovernance performs domain-specific holistic research for governance context.
func (a *ResearchAgent) EnrichHolisticGovernance(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic governance research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a governance/ownership analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's governance structure and ownership:
- Ownership structure (parent companies, UBOs, shareholders)
- Board composition and key personnel
- Control chains (subsidiaries, controlled entities)
- Beneficial owners and investment relationships
- Governance risks (concentration, opacity, cross-jurisdictional)

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeGovernance),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticRegulatory performs domain-specific holistic research for regulatory impact context.
func (a *ResearchAgent) EnrichHolisticRegulatory(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic regulatory research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a regulatory impact analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's relationship with the regulatory context:
- Regulatory instruments affecting this entity
- Decision points and mechanisms this entity participates in
- Compliance status and exposure
- Affected stakeholders and downstream impact
- Regulatory risks and opportunities

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModePolicyImpact),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticMarket performs domain-specific holistic research for market context.
func (a *ResearchAgent) EnrichHolisticMarket(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic market research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a market analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's market position:
- Market share and competitive position
- Pricing power and demand/supply drivers
- Key competitors and alternatives
- Growth trajectory and market outlook
- Barriers to entry and regulatory environment

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModeMarket),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeMarket),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticStrategic performs domain-specific holistic research for strategic context.
func (a *ResearchAgent) EnrichHolisticStrategic(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic strategic research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a strategic/geopolitical analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's strategic position:
- Power dynamics and leverage points
- Geopolitical dependencies and alliances
- Strategic assets and vulnerabilities
- Influence networks and key relationships
- Risk exposure and resilience factors

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModeStrategic),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeStrategic),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichQueryContextual performs query-contextualized research (Level 2).
func (a *ResearchAgent) EnrichQueryContextual(ctx context.Context, frag *models.HorizonFragment, originalQuery string) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || frag == nil {
		return nil, fmt.Errorf("query-contextual research input is incomplete")
	}
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

In the context of the analytical query above, provide all relevant information about this entity:
- Key facts, relationships, and dependencies relevant to the query
- Role and significance in the described context
- Connected entities and their relationships
- Risk factors and important data points

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		originalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, frag)
}

// EnrichEntityTypeHolistic performs entity-type-specific comprehensive research (Level 3).
func (a *ResearchAgent) EnrichEntityTypeHolistic(ctx context.Context, frag *models.HorizonFragment) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || frag == nil {
		return nil, fmt.Errorf("entity-type holistic research input is incomplete")
	}
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	focusAreas := entityTypeHolisticFocusAreas(frag.Type)
	prompt := fmt.Sprintf(`You are an analytical research agent building a comprehensive profile of an entity.

ENTITY: %s (type: %s, canonical path: %s)
%s

Provide a comprehensive profile covering:
%s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		focusAreas,
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, frag)
}

func entityTypeHolisticFocusAreas(ft models.FragmentType) string {
	switch ft {
	case models.FragmentTypeOrganization:
		return `- Ownership structure and parent/subsidiary relationships
- Products and services offered
- Key markets and geographic presence
- Financial overview (revenue, employees, market cap if public)
- Key personnel (CEO, board members)
- Principal facilities and operations`
	case models.FragmentTypeFacility:
		return `- Operator and owner
- Capacity and throughput
- Geographic location and coordinates
- Inputs and outputs (what it processes/produces)
- Connected infrastructure (pipelines, ports, grids)
- Operational status and commissioning date`
	case models.FragmentTypeProduct:
		return `- Major producers and suppliers
- Key consumers and end markets
- Alternative products and substitutes
- Price dynamics and benchmarks
- Position in supply chains
- Quality grades and specifications`
	case models.FragmentTypeGeoRegion:
		return `- Key industries and economic base
- Major organizations and employers
- Infrastructure and logistics
- Regulatory environment
- Demographics and population
- Political structure and governance`
	case models.FragmentTypeDeposit:
		return `- Resource type and classification
- Proven and probable reserves
- Current operators and concession holders
- Extraction methods and technology
- Grade and quality characteristics
- Infrastructure connectivity`
	case models.FragmentTypeCommunity:
		return `- Geographic scope and boundaries
- Demographics and population
- Economic base and key industries
- Political structure and governance
- Key institutions and organizations
- Infrastructure and services`
	case models.FragmentTypeMarket:
		return `- Market size and value
- Growth dynamics and trends
- Key players and market shares
- Barriers to entry
- Regulatory environment
- Demand and supply drivers`
	default:
		return `- Key relationships with other entities
- Role and significance in its domain
- Important data points and metrics
- Risk factors and dependencies
- Geographic presence and scope`
	}
}

func nameForPrompt(frag *models.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	if frag.Labels != nil {
		if en := strings.TrimSpace(frag.Labels["en"]); en != "" {
			return en
		}
	}
	if frag.Identifiers != nil {
		if ln := strings.TrimSpace(frag.Identifiers["legalName"]); ln != "" {
			return ln
		}
		if sn := strings.TrimSpace(frag.Identifiers["shortName"]); sn != "" {
			return sn
		}
	}
	parts := strings.Split(strings.Trim(frag.CanonicalPath, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return frag.CanonicalPath
}

func compactFragmentForPrompt(frag *models.HorizonFragment) map[string]interface{} {
	if frag == nil {
		return nil
	}
	m := map[string]interface{}{
		"canonicalPath": frag.CanonicalPath,
		"type":          frag.Type,
	}
	if len(frag.Labels) > 0 {
		m["labels"] = frag.Labels
	}
	if len(frag.Identifiers) > 0 {
		m["identifiers"] = frag.Identifiers
	}
	if frag.Description != "" {
		desc := frag.Description
		if len(desc) > 500 {
			desc = desc[:500] + "..."
		}
		m["description"] = desc
	}
	return m
}

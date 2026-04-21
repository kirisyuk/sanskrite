package taxonomy

import (
	"strings"
	"unicode"
)

// FacilityKind is the physical infrastructure type of a facility.
// Single source of truth for: heuristic slug detection, LLM extraction, Vertex categorization.
// Use facilityKind (identifiers or qualifiers) — never free-form.
const (
	FacilityKindUnknown    FacilityKind = ""
	FacilityKindSeaport    FacilityKind = "seaport"
	FacilityKindAirport    FacilityKind = "airport"
	FacilityKindFactory    FacilityKind = "factory"
	FacilityKindWarehouse  FacilityKind = "warehouse"
	FacilityKindPipeline   FacilityKind = "pipeline"
	FacilityKindCable      FacilityKind = "cable"
	FacilityKindMine       FacilityKind = "mine"
	FacilityKindRefinery   FacilityKind = "refinery"
	FacilityKindPowerPlant FacilityKind = "power_plant"
	FacilityKindDatacenter FacilityKind = "datacenter"
	FacilityKindTerminal   FacilityKind = "terminal"
	FacilityKindSubstation FacilityKind = "substation"
	FacilityKindOther      FacilityKind = "other"
)

type FacilityKind string

func normalizeFacilitySignalText(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	prevSpace := true
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
			continue
		}
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

func facilitySignalTokenSet(text string) map[string]struct{} {
	norm := normalizeFacilitySignalText(text)
	if norm == "" {
		return nil
	}
	parts := strings.Fields(norm)
	if len(parts) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		out[p] = struct{}{}
	}
	return out
}

func hasAnyFacilityToken(tokens map[string]struct{}, keys ...string) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, k := range keys {
		if _, ok := tokens[k]; ok {
			return true
		}
	}
	return false
}

func hasAnyFacilityPhrase(text string, phrases ...string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	needleBase := " " + normalizeFacilitySignalText(text) + " "
	for _, p := range phrases {
		needle := " " + normalizeFacilitySignalText(p) + " "
		if needle != "  " && strings.Contains(needleBase, needle) {
			return true
		}
	}
	return false
}

func inferFacilityKindFromText(text string) FacilityKind {
	norm := normalizeFacilitySignalText(text)
	if norm == "" {
		return FacilityKindUnknown
	}
	tokens := facilitySignalTokenSet(norm)

	// Hard infrastructure first.
	if hasAnyFacilityToken(tokens, "pipeline", "pipelines", "pipe", "midstream") ||
		hasAnyFacilityPhrase(norm, "gas pipeline", "oil pipeline", "export pipeline") {
		return FacilityKindPipeline
	}
	if hasAnyFacilityToken(tokens, "refinery", "refining", "smelter", "smelting") {
		return FacilityKindRefinery
	}
	if hasAnyFacilityToken(tokens, "mine", "mining", "quarry", "pit") ||
		hasAnyFacilityPhrase(norm, "open pit", "mine complex") {
		return FacilityKindMine
	}
	if hasAnyFacilityToken(tokens, "airport", "airfield", "aerodrome") {
		return FacilityKindAirport
	}
	if hasAnyFacilityToken(tokens, "seaport", "harbor", "harbour", "dock", "quay", "pier", "port") {
		return FacilityKindSeaport
	}
	if hasAnyFacilityToken(tokens, "terminal", "regas", "regasification") &&
		(hasAnyFacilityToken(tokens, "lng", "lpg", "gas", "oil", "export", "import", "storage", "port") ||
			hasAnyFacilityPhrase(norm, "lng terminal", "gas terminal", "oil terminal")) {
		return FacilityKindTerminal
	}
	if hasAnyFacilityToken(tokens, "substation", "grid") ||
		hasAnyFacilityPhrase(norm, "offshore substation", "transmission grid") {
		return FacilityKindSubstation
	}
	if hasAnyFacilityToken(tokens, "cable") ||
		(hasAnyFacilityToken(tokens, "fiber", "fibre", "optic") &&
			(hasAnyFacilityToken(tokens, "submarine", "subsea", "undersea", "power", "optic", "fiber", "fibre") ||
				hasAnyFacilityPhrase(norm, "submarine cable", "subsea cable", "power cable", "fiber cable"))) {
		return FacilityKindCable
	}
	if hasAnyFacilityToken(tokens, "datacenter") ||
		(hasAnyFacilityToken(tokens, "data", "center", "centre") &&
			(hasAnyFacilityPhrase(norm, "data center", "data centre", "hyperscale data center") || hasAnyFacilityToken(tokens, "server", "cloud"))) {
		return FacilityKindDatacenter
	}
	if hasAnyFacilityToken(tokens, "warehouse", "depot", "distribution", "logistics", "fulfillment", "fulfilment", "hub") &&
		(hasAnyFacilityToken(tokens, "warehouse", "depot", "distribution", "logistics", "fulfillment", "fulfilment") ||
			hasAnyFacilityPhrase(norm, "distribution center", "logistics hub", "general distribution")) {
		return FacilityKindWarehouse
	}

	// Power plant requires explicit power-generation signal; plain "plant" is not enough.
	if (hasAnyFacilityToken(tokens, "power", "generation", "generating", "thermal", "nuclear", "hydro", "geothermal", "biomass", "solar", "wind") &&
		hasAnyFacilityToken(tokens, "plant", "station", "farm")) ||
		hasAnyFacilityPhrase(norm, "power plant", "power station", "solar farm", "wind farm", "thermal power", "nuclear power") {
		return FacilityKindPowerPlant
	}

	// Generic manufacturing fallback.
	if hasAnyFacilityToken(tokens, "factory", "fab", "gigafactory", "foundry", "manufacturing", "assembly", "production", "processing", "mill", "works", "plant") ||
		hasAnyFacilityPhrase(norm, "processing plant", "manufacturing plant", "assembly plant", "production facility") {
		return FacilityKindFactory
	}

	return FacilityKindOther
}

// FacilityKinds returns all facility kinds in stable order (for prompts and validation).
func FacilityKinds() []FacilityKind {
	return []FacilityKind{
		FacilityKindSeaport,
		FacilityKindAirport,
		FacilityKindFactory,
		FacilityKindWarehouse,
		FacilityKindPipeline,
		FacilityKindCable,
		FacilityKindMine,
		FacilityKindRefinery,
		FacilityKindPowerPlant,
		FacilityKindDatacenter,
		FacilityKindTerminal,
		FacilityKindSubstation,
		FacilityKindOther,
	}
}

// FacilityKindsForPrompts returns the facility kind enum string for LLM prompts.
func FacilityKindsForPrompts() string {
	kinds := FacilityKinds()
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		if k != FacilityKindUnknown && k != FacilityKindOther {
			parts = append(parts, string(k))
		}
	}
	parts = append(parts, string(FacilityKindOther))
	return strings.Join(parts, "|")
}

// FacilityKindFromSlug infers FacilityKind from /fac/{geo}/{slug} facility slug.
// Used by: taxonomy classifier (Vertex mapping), pre-create detection, LLM fallback.
func FacilityKindFromSlug(slug string) FacilityKind {
	if strings.TrimSpace(slug) == "" {
		return FacilityKindUnknown
	}
	return inferFacilityKindFromText(slug)
}

// FacilityKindFromCanonicalPath extracts slug from /fac/{geo}/{slug} and runs FacilityKindFromSlug.
func FacilityKindFromCanonicalPath(canonicalPath string) FacilityKind {
	path := strings.TrimSpace(canonicalPath)
	if path == "" || !strings.HasPrefix(strings.ToLower(path), "/fac/") {
		return FacilityKindUnknown
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return FacilityKindUnknown
	}
	return FacilityKindFromSlug(parts[len(parts)-1])
}

// InferFacilityKind infers physical facility kind from canonical path plus optional textual signals
// (name, description, entity/sector path, facilityType, etc.).
// Returns concrete kind when possible, otherwise FacilityKindOther for /fac/* paths.
func InferFacilityKind(canonicalPath string, signals ...string) FacilityKind {
	path := strings.TrimSpace(canonicalPath)
	if path == "" || !strings.HasPrefix(strings.ToLower(path), "/fac/") {
		return FacilityKindUnknown
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return FacilityKindUnknown
	}

	candidates := make([]string, 0, len(signals)+2)
	candidates = append(candidates, parts[len(parts)-1])
	candidates = append(candidates, path)
	for _, s := range signals {
		if strings.TrimSpace(s) != "" {
			candidates = append(candidates, s)
		}
	}

	joined := strings.Join(candidates, " | ")
	kind := inferFacilityKindFromText(joined)
	if kind != FacilityKindUnknown {
		return kind
	}
	return FacilityKindOther
}

// NormalizeFacilityKind normalizes raw LLM/import string to FacilityKind.
func NormalizeFacilityKind(raw string) FacilityKind {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return FacilityKindUnknown
	}
	aliases := map[string]FacilityKind{
		"seaport": FacilityKindSeaport, "port": FacilityKindSeaport, "harbor": FacilityKindSeaport,
		"airport": FacilityKindAirport,
		"factory": FacilityKindFactory, "plant": FacilityKindFactory, "fab": FacilityKindFactory,
		"warehouse": FacilityKindWarehouse, "depot": FacilityKindWarehouse,
		"pipeline": FacilityKindPipeline, "pipe": FacilityKindPipeline,
		"cable": FacilityKindCable, "submarine-cable": FacilityKindCable, "subsea": FacilityKindCable,
		"mine": FacilityKindMine, "mining": FacilityKindMine,
		"refinery": FacilityKindRefinery, "refining": FacilityKindRefinery,
		"power_plant": FacilityKindPowerPlant, "powerplant": FacilityKindPowerPlant,
		"datacenter": FacilityKindDatacenter, "data-center": FacilityKindDatacenter,
		"terminal": FacilityKindTerminal, "lng-terminal": FacilityKindTerminal,
		"substation": FacilityKindSubstation, "grid": FacilityKindSubstation,
	}
	if k, ok := aliases[s]; ok {
		return k
	}
	for _, k := range FacilityKinds() {
		if string(k) == s {
			return k
		}
	}
	return FacilityKindOther
}

// FacilityKindToVertexEntityPath maps FacilityKind to Vertex AI entityPath (for search categorization).
func FacilityKindToVertexEntityPath(k FacilityKind) string {
	switch k {
	case FacilityKindPipeline:
		return "/Infrastructure/Energy/Pipeline/Gas"
	case FacilityKindRefinery:
		return "/Infrastructure/Manufacturing/Factory"
	case FacilityKindTerminal, FacilityKindWarehouse:
		return "/Infrastructure/Logistics/Warehouse"
	case FacilityKindMine:
		return "/Infrastructure/Manufacturing/Mine"
	case FacilityKindSeaport:
		return "/Infrastructure/Logistics/Seaport"
	case FacilityKindAirport:
		return "/Infrastructure/Logistics/Airport"
	case FacilityKindPowerPlant:
		return "/Infrastructure/Energy/PowerPlant/Thermal"
	case FacilityKindDatacenter:
		return "/Infrastructure/Digital/DataCenter"
	case FacilityKindFactory:
		return "/Infrastructure/Manufacturing/Factory"
	case FacilityKindSubstation:
		return "/Infrastructure/Energy/Grid/Substation"
	case FacilityKindCable:
		return "/Infrastructure/Telecom/Cable"
	default:
		return "/Infrastructure/Manufacturing/Factory"
	}
}

// =============================================================================
// FacilityRole — supply chain stage (separate from physical FacilityKind)
// =============================================================================
// Use for supply chain context: extraction, refining, manufacturing, etc.

const (
	FacilityRoleExtraction    FacilityRole = "extraction"
	FacilityRoleRefining      FacilityRole = "refining"
	FacilityRoleProcessing    FacilityRole = "processing"
	FacilityRoleManufacturing FacilityRole = "manufacturing"
	FacilityRoleDistribution  FacilityRole = "distribution"
	FacilityRoleStorage       FacilityRole = "storage"
	FacilityRoleLogistics     FacilityRole = "logistics"
)

type FacilityRole string

// FacilityRolesForPrompts returns the facility role enum for supply chain LLM prompts.
func FacilityRolesForPrompts() string {
	return "extraction|refining|processing|manufacturing|distribution|storage|logistics"
}

// NormalizeFacilityRole normalizes raw string to FacilityRole.
func NormalizeFacilityRole(raw string) FacilityRole {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "extraction", "extract", "mine":
		return FacilityRoleExtraction
	case "refining", "refinery", "refine":
		return FacilityRoleRefining
	case "processing", "process":
		return FacilityRoleProcessing
	case "manufacturing", "manufacture", "factory":
		return FacilityRoleManufacturing
	case "distribution", "distribute":
		return FacilityRoleDistribution
	case "storage", "store":
		return FacilityRoleStorage
	case "logistics", "logistic":
		return FacilityRoleLogistics
	}
	return ""
}

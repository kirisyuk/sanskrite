package taxonomy

import (
	"strings"
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

// facilityKindSlugHints maps FacilityKind to slug substrings (lowercase) for heuristic detection.
// Order matters: more specific hints first (e.g. "lng-terminal" before "terminal").
var facilityKindSlugHints = map[FacilityKind][]string{
	FacilityKindPipeline:   {"pipeline", "pipe"},
	FacilityKindRefinery:   {"refinery", "refining"},
	FacilityKindTerminal:   {"lng-terminal", "terminal", "storage", "tank"},
	FacilityKindMine:       {"mine", "mining"},
	FacilityKindSeaport:    {"port", "seaport", "harbor", "harbour"},
	FacilityKindAirport:    {"airport"},
	FacilityKindPowerPlant: {"power-plant", "powerplant", "plant", "solar", "wind", "renewable"},
	FacilityKindDatacenter: {"datacenter", "data-center"},
	FacilityKindFactory:    {"factory", "fab", "gigafactory", "foundry"},
	FacilityKindSubstation: {"substation", "grid"},
	FacilityKindCable:      {"cable", "submarine-cable", "subsea-cable", "fiber", "optic"},
	FacilityKindWarehouse:  {"warehouse", "depot", "distribution-center"},
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
	s := strings.ToLower(strings.TrimSpace(slug))
	if s == "" {
		return FacilityKindUnknown
	}
	// Check in deterministic order (more specific first; airport before seaport so "jfk-airport" -> airport not seaport)
	for _, k := range []FacilityKind{
		FacilityKindPipeline, FacilityKindRefinery, FacilityKindTerminal,
		FacilityKindMine, FacilityKindAirport, FacilityKindSeaport,
		FacilityKindPowerPlant, FacilityKindDatacenter, FacilityKindFactory,
		FacilityKindSubstation, FacilityKindCable, FacilityKindWarehouse,
	} {
		for _, hint := range facilityKindSlugHints[k] {
			if strings.Contains(s, hint) {
				return k
			}
		}
	}
	return FacilityKindOther
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

// FacilityKindToVertexSectorPath maps FacilityKind to Vertex AI sectorPath.
func FacilityKindToVertexSectorPath(k FacilityKind) string {
	switch k {
	case FacilityKindPipeline, FacilityKindTerminal:
		return "/Energy/Fossil_Fuels/Oil_Gas_Midstream"
	case FacilityKindRefinery, FacilityKindPowerPlant:
		return "/Energy/Fossil_Fuels/Oil_Gas_Downstream"
	case FacilityKindMine:
		return "/Materials/Mining/Industrial_Metals"
	case FacilityKindSeaport:
		return "/Industrials/Transportation/Marine"
	case FacilityKindAirport:
		return "/Industrials/Transportation/Air_Freight"
	case FacilityKindDatacenter:
		return "/Technology/Hardware/Consumer_Electronics"
	case FacilityKindFactory:
		return "/Technology/Semiconductors/Manufacturing"
	case FacilityKindSubstation:
		return "/Energy/Renewable/Wind"
	case FacilityKindCable:
		return "/Technology/Telecom/Infrastructure"
	default:
		return "/Industrials/Machinery/Industrial_Automation"
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

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// Seeds Horizon taxonomy into Firestore as a single source of truth.
// Usage:
//   go run ./cmd/tools/horizon_seed_taxonomy --project <gcp-project> --version v1
func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	version := flag.String("version", "v1", "taxonomy version doc id (default v1)")
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project is required (or set GCP_PROJECT_ID)")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	taxRepo := repo.NewHorizonTaxonomyRepository(fsClient.Client())

	now := time.Now().UTC()
	tax := &models.HorizonTaxonomy{
		Version:   *version,
		UpdatedAt: now,
		CanonicalPrefixes: []string{
			"/org/",
			"/fac/",
			"/loc/",
			"/person/",
			"/product/",
			"/program/",
			"/market/",
			"/event/",
			"/sphere/",
		},
		AllowedSpherePaths: []string{
			"/sphere/ai",
			"/sphere/ai/models",
			"/sphere/ai/policy",
			"/sphere/ai/research",
			"/sphere/ai/applications",
			"/sphere/geopolitics",
			"/sphere/policy",
			"/sphere/policy/regulation",
			"/sphere/policy/trade",
			"/sphere/policy/sanctions",
			"/sphere/social",
			"/sphere/social/information",
			"/sphere/cyber",
			"/sphere/digital",
			"/sphere/logistics",
			"/sphere/logistics/maritime",
			"/sphere/energy",
			"/sphere/energy/markets",
			"/sphere/finance",
			"/sphere/finance/crypto",
			"/sphere/finance/markets",
			"/sphere/other",
		},
		SphereSynonyms: map[string]string{
			"ai":            "/sphere/ai",
			"ml":            "/sphere/ai",
			"machinelearning": "/sphere/ai",
			"models":        "/sphere/ai/models",
			"llm":           "/sphere/ai/models",
			"policy":        "/sphere/policy",
			"regulation":    "/sphere/policy/regulation",
			"geo":           "/sphere/geopolitics",
			"geopolitical":  "/sphere/geopolitics",
			"geopolitics":   "/sphere/geopolitics",
			"society":       "/sphere/social",
			"socium":        "/sphere/social",
			"social":        "/sphere/social",
			"info":          "/sphere/social/information",
			"information":   "/sphere/social/information",
			"media":         "/sphere/social/information",
			"cyber":         "/sphere/cyber",
			"cybersecurity": "/sphere/cyber",
			"infosec":       "/sphere/cyber",
			"digital":       "/sphere/digital",
			"it":            "/sphere/digital",
			"tech":          "/sphere/digital",
			"logistics":     "/sphere/logistics",
			"supply_chain":  "/sphere/logistics",
			"shipping":      "/sphere/logistics/maritime",
			"maritime":      "/sphere/logistics/maritime",
			"energy":        "/sphere/energy",
			"oil":           "/sphere/energy",
			"gas":           "/sphere/energy",
			"finance":       "/sphere/finance",
			"markets":       "/sphere/finance",
			"crypto":        "/sphere/finance/crypto",
			"cryptocurrency": "/sphere/finance/crypto",
		},
		AllowedEventTypes: []string{
			"attack",
			"cyberattack",
			"sanction",
			"blockade",
			"policy_change",
			"license_change",
			"partnership",
			"acquisition",
			"release",
			"deprecation",
			"pricing_change",
			"availability_change",
			"capability_change",
			"protest",
			"strike",
			"election",
			"warning",
			"announcement",
			"incident",
			"other",
		},
		EventTypeSynonyms: map[string]string{
			"drone_attack":   "attack",
			"missile_attack": "attack",
			"strike":         "strike",
			"labor_strike":   "strike",
			"ransomware":     "cyberattack",
			"ddos":           "cyberattack",
			"sanctions":      "sanction",
			"regulation":     "policy_change",
			"policy":         "policy_change",
			"licensing":      "license_change",
			"license":        "license_change",
			"launch":         "release",
			"released":       "release",
			"deprecated":     "deprecation",
			"pricing":        "pricing_change",
			"price":          "pricing_change",
		},
		AllowedRelationTypes: []string{
			"SUPPLIES_TO",
			"LOCATED_IN",
			"OWNS",
			"PRODUCES",
			"OPERATES_IN",
			"REGULATES",
			"COMPETES_WITH",
			"INFLUENCES",
			"DEPENDS_ON",
			"SUBSIDIARY_OF",
			"TRANSFORMS_INTO",
		},
		AllowedMetricUnits: []string{"usd", "eur", "rub", "units", "units_per_day", "days", "hours", "ratio", "index_point", "code"},

		// Product taxonomy
		AllowedProductCategories: []string{"raw", "component", "finished", "service"},
		AllowedProductTypes: []string{
			"mineral",
			"agricultural",
			"chemical",
			"electronic",
			"mechanical",
			"energy",
		},
		AllowedProductStages: []string{
			"extraction",
			"refining",
			"processing",
			"manufacturing",
			"assembly",
		},
		ProductStageOrder: []string{
			"extraction",
			"refining",
			"processing",
			"manufacturing",
			"assembly",
		},
		KnownProducts: map[string]models.ProductSpecData{
			"lithium-ore": {
				Category: "raw",
				Type:     "mineral",
				Stage:    "extraction",
			},
			"lithium-carbonate": {
				Category: "raw",
				Type:     "chemical",
				Stage:    "refining",
			},
			"lithium-hydroxide": {
				Category: "raw",
				Type:     "chemical",
				Stage:    "refining",
			},
			"cobalt-ore": {
				Category: "raw",
				Type:     "mineral",
				Stage:    "extraction",
			},
			"nickel-ore": {
				Category: "raw",
				Type:     "mineral",
				Stage:    "extraction",
			},
			"cathode-material": {
				Category: "component",
				Type:     "electronic",
				Stage:    "processing",
			},
			"battery-cell": {
				Category: "component",
				Type:     "electronic",
				Stage:    "manufacturing",
			},
			"semiconductor-wafer": {
				Category: "component",
				Type:     "electronic",
				Stage:    "manufacturing",
			},
			"ev-battery-cell": {
				Category: "component",
				Type:     "electronic",
				Stage:    "manufacturing",
			},
			"electric-vehicle": {
				Category: "finished",
				Type:     "mechanical",
				Stage:    "assembly",
			},
		},
		ProductSynonyms: map[string]string{
			"li2co3":                    "lithium-carbonate",
			"lithium carbonate":         "lithium-carbonate",
			"battery grade lithium":     "lithium-carbonate",
			"lithium hydroxide":        "lithium-hydroxide",
			"lioh":                      "lithium-hydroxide",
			"lithium ore":               "lithium-ore",
			"spodumene":                 "lithium-ore",
			"cobalt ore":                "cobalt-ore",
			"nickel ore":                "nickel-ore",
			"ncm":                       "cathode-material",
			"nca":                       "cathode-material",
			"cathode":                   "cathode-material",
			"battery cell":              "battery-cell",
			"ev battery":                "ev-battery-cell",
			"electric vehicle battery":  "ev-battery-cell",
			"wafer":                     "semiconductor-wafer",
			"silicon wafer":             "semiconductor-wafer",
			"ev":                        "electric-vehicle",
			"electric car":              "electric-vehicle",
			"electric vehicle":          "electric-vehicle",
		},
	}

	if err := taxRepo.Upsert(ctx, tax); err != nil {
		log.Fatalf("taxonomy upsert failed: %v", err)
	}

	log.Printf("Seeded horizon taxonomy: project=%s version=%s updatedAt=%s", *projectID, *version, now.Format(time.RFC3339))
}


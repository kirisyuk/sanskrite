package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// TaxonomyClassificationResult результат классификации таксономии
type TaxonomyClassificationResult struct {
	EntityPath string `json:"entityPath"`
	SectorPath string `json:"sectorPath"`
	Reasoning  string `json:"reasoning,omitempty"` // Обоснование классификации
}

// TaxonomyClassifier классифицирует canonicalPath в entityPath и sectorPath через LLM
type TaxonomyClassifier struct {
	executor *ai.AITaskExecutor
}

// NewTaxonomyClassifier создает новый классификатор таксономии
func NewTaxonomyClassifier(executor *ai.AITaskExecutor) *TaxonomyClassifier {
	return &TaxonomyClassifier{
		executor: executor,
	}
}

// ClassifyTaxonomyDeterministic attempts rule-based classification without LLM.
// Returns ok=true when the canonical path prefix unambiguously maps to a known taxonomy.
func ClassifyTaxonomyDeterministic(canonicalPath string) (entityPath, sectorPath string, ok bool) {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	prefix := parts[0]

	switch prefix {
	case "gov":
		return classifyGovDeterministic(parts)
	case "org":
		return classifyOrgDeterministic(parts)
	case "fac":
		return classifyFacDeterministic(parts)
	case "product":
		return classifyProductDeterministic(parts)
	case "loc":
		return "/Geo/Region/Country", "/Geography", true
	case "market":
		return classifyMarketDeterministic(parts)
	case "person":
		return "/Person/Executive", "/Financials/Capital_Markets/Asset_Management", true
	case "event":
		return "/Event", "/Industrials/Transportation/Marine", true
	case "deposit":
		return "/Resource/RawMaterial/Mineral", "/Materials/Mining/Industrial_Metals", true
	case "mechanism":
		return "/Organization/Government/Agency", "/Public/Executive/Ministry", true
	case "decision":
		return "/Organization/Government/Agency", "/Public/Executive/Ministry", true
	case "program":
		return "/Organization/Government/Agency", "/Public/Executive/Ministry", true
	case "media":
		return "/Organization/Corporation/Private", "/Technology/Software/Enterprise", true
	case "sphere":
		return "", "", false
	case "domain":
		return "", "", false
	}
	return "", "", false
}

func classifyGovDeterministic(parts []string) (string, string, bool) {
	slug := ""
	if len(parts) >= 3 {
		slug = parts[2]
	}
	switch {
	case containsAny(slug, "military", "defense", "defence", "army", "navy"):
		return "/Organization/Government/Military", "/Industrials/Aerospace_Defense/Defense_Prime", true
	case containsAny(slug, "central-bank", "monetary", "reserve-bank"):
		return "/Organization/Government/Agency", "/Public/Central_Bank", true
	case containsAny(slug, "parliament", "congress", "assembly", "duma", "majlis"):
		return "/Organization/Government/Agency", "/Public/Legislative/Parliament", true
	case containsAny(slug, "court", "judiciary", "tribunal"):
		return "/Organization/Government/Agency", "/Public/Judicial/Court", true
	default:
		return "/Organization/Government/Agency", "/Public/Executive/Ministry", true
	}
}

var knownStateOwnedEnergy = map[string]bool{
	"socar": true, "aramco": true, "gazprom": true, "petrobras": true,
	"rosneft": true, "equinor": true, "petronas": true, "adnoc": true,
	"knpc": true, "nnpc": true, "sinopec": true, "cnooc": true,
	"cnpc": true, "botas": true, "saudi-aramco": true,
}

func classifyOrgDeterministic(parts []string) (string, string, bool) {
	slug := ""
	if len(parts) >= 3 {
		slug = parts[2]
	}
	ep := "/Organization/Corporation/Private"

	isSOE := containsAny(slug, "state", "national", "petroleum")
	if !isSOE {
		for known := range knownStateOwnedEnergy {
			if strings.Contains(slug, known) {
				isSOE = true
				break
			}
		}
	}
	if isSOE {
		ep = "/Organization/Corporation/StateOwned"
	}

	sp := inferSectorFromSlug(slug)
	if sp == "" && isSOE {
		return ep, "/Energy/Fossil_Fuels/Oil_Gas_Upstream", true
	}
	if sp == "" {
		return ep, "/Industrials/Machinery/Industrial_Automation", true
	}
	return ep, sp, true
}

func classifyFacDeterministic(parts []string) (string, string, bool) {
	slug := ""
	if len(parts) >= 3 {
		slug = parts[2]
	} else if len(parts) >= 2 {
		slug = parts[1]
	}
	kind := taxonomy.FacilityKindFromSlug(slug)
	if kind == taxonomy.FacilityKindPowerPlant && containsAny(slug, "solar", "wind", "renewable") {
		return "/Infrastructure/Energy/PowerPlant/Renewable", "/Energy/Renewable/Solar", true
	}
	return taxonomy.FacilityKindToVertexEntityPath(kind), taxonomy.FacilityKindToVertexSectorPath(kind), true
}

func classifyProductDeterministic(parts []string) (string, string, bool) {
	if len(parts) < 3 {
		return "/Resource/RawMaterial/Mineral", "/Materials/Mining/Industrial_Metals", true
	}
	cat := parts[1]
	typ := parts[2]

	switch cat {
	case "raw":
		switch typ {
		case "mineral":
			return "/Resource/RawMaterial/Mineral", "/Materials/Mining/Industrial_Metals", true
		case "agricultural":
			return "/Resource/RawMaterial/Agricultural", "/Materials/Chemicals/Fertilizers", true
		case "chemical":
			return "/Resource/RawMaterial/Mineral", "/Materials/Chemicals/Specialty", true
		case "energy":
			return "/Resource/Energy/Fossil", "/Energy/Fossil_Fuels/Oil_Gas_Upstream", true
		default:
			return "/Resource/RawMaterial/Mineral", "/Materials/Mining/Industrial_Metals", true
		}
	case "component":
		switch typ {
		case "electronic":
			return "/Resource/Component/Electronic", "/Technology/Semiconductors/Design", true
		case "mechanical":
			return "/Resource/Component/Mechanical", "/Industrials/Machinery/Industrial_Automation", true
		default:
			return "/Resource/Component/Electronic", "/Technology/Semiconductors/Design", true
		}
	case "finished":
		return "/Resource/Component/Electronic", "/Technology/Hardware/Consumer_Electronics", true
	case "service":
		return "/Organization/Corporation/Private", "/Technology/Software/Enterprise", true
	default:
		return "/Resource/RawMaterial/Mineral", "/Materials/Mining/Industrial_Metals", true
	}
}

func classifyMarketDeterministic(parts []string) (string, string, bool) {
	geo := ""
	domain := ""
	if len(parts) >= 2 {
		geo = strings.ToLower(strings.TrimSpace(parts[1]))
	}
	if len(parts) >= 3 {
		domain = parts[2]
	}

	// Market entityPath by geo scope (aligned with /market/{geo}/{domain}...)
	var entityPath string
	switch {
	case geo == "global" || geo == "no" || geo == "int":
		entityPath = "/Market/Geo/Global"
	case taxonomy.IsRegionalGeoToken(geo):
		entityPath = "/Market/Geo/Regional"
	case taxonomy.IsCompositeGeoToken(geo), len(geo) == 2 && geo[0] >= 'a' && geo[0] <= 'z' && geo[1] >= 'a' && geo[1] <= 'z':
		entityPath = "/Market/Geo/Country"
	default:
		entityPath = "/Market/Domain/Industry"
	}

	switch {
	case containsAny(domain, "energy", "oil", "gas", "lng", "power", "fuel"):
		return entityPath, "/Energy/Fossil_Fuels/Oil_Gas_Upstream", true
	case containsAny(domain, "mining", "metals", "mineral", "rare-earth"):
		return entityPath, "/Materials/Mining/Industrial_Metals", true
	case containsAny(domain, "chemical", "fertilizer", "pharma"):
		return entityPath, "/Materials/Chemicals/Specialty", true
	case containsAny(domain, "semiconductor", "chip", "electronics"):
		return entityPath, "/Technology/Semiconductors/Manufacturing", true
	case containsAny(domain, "software", "saas", "cloud", "cyber", "tech", "technology", "ai"):
		return entityPath, "/Technology/Software/Enterprise", true
	case containsAny(domain, "automotive", "ev", "vehicle"):
		return entityPath, "/Automotive/Vehicles/EV", true
	case containsAny(domain, "bank", "finance", "insurance", "fintech", "capital"):
		return entityPath, "/Financials/Banking/Systemic", true
	case containsAny(domain, "defense", "defence", "aerospace", "military"):
		return entityPath, "/Industrials/Aerospace_Defense/Defense_Prime", true
	case containsAny(domain, "transport", "logistics", "shipping", "freight"):
		return entityPath, "/Industrials/Transportation/Marine", true
	case containsAny(domain, "agriculture", "food", "crop", "agri"):
		return entityPath, "/Materials/Chemicals/Fertilizers", true
	case containsAny(domain, "health", "medical", "biotech"):
		return entityPath, "/Healthcare/Pharma/Biotech", true
	default:
		return entityPath, "/Industrials/Machinery/Industrial_Automation", true
	}
}

func inferSectorFromSlug(slug string) string {
	switch {
	case containsAny(slug, "oil", "gas", "lng", "petroleum", "petrol", "fuel",
		"energy", "pipeline", "refin"):
		return "/Energy/Fossil_Fuels/Oil_Gas_Upstream"
	case containsAny(slug, "solar", "wind", "renewable", "hydro"):
		return "/Energy/Renewable/Solar"
	case containsAny(slug, "mining", "mine", "metal", "cobalt", "lithium", "copper", "nickel"):
		return "/Materials/Mining/Industrial_Metals"
	case containsAny(slug, "chemical", "fertiliz"):
		return "/Materials/Chemicals/Specialty"
	case containsAny(slug, "semiconductor", "chip", "fab", "foundry", "tsmc"):
		return "/Technology/Semiconductors/Manufacturing"
	case containsAny(slug, "software", "saas", "cloud", "cyber", "ai"):
		return "/Technology/Software/Enterprise"
	case containsAny(slug, "bank", "finance", "capital", "insurance"):
		return "/Financials/Banking/Systemic"
	case containsAny(slug, "auto", "ev", "vehicle", "tesla", "bmw", "toyota"):
		return "/Automotive/Vehicles/EV"
	case containsAny(slug, "pharma", "biotech", "medical", "health"):
		return "/Healthcare/Pharma/Biotech"
	case containsAny(slug, "defense", "defence", "military", "weapon"):
		return "/Industrials/Aerospace_Defense/Defense_Prime"
	case containsAny(slug, "transport", "logistic", "shipping", "freight", "airline"):
		return "/Industrials/Transportation/Marine"
	case containsAny(slug, "telecom", "mobile", "wireless"):
		return "/Technology/Hardware/Consumer_Electronics"
	case containsAny(slug, "food", "agri", "crop", "grain"):
		return "/Materials/Chemicals/Fertilizers"
	}
	return ""
}

func containsAny(s string, tokens ...string) bool {
	for _, t := range tokens {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

// ClassifyTaxonomy determines entityPath and sectorPath from canonicalPath, using deterministic
// rules first and falling back to LLM only for ambiguous cases.
func (c *TaxonomyClassifier) ClassifyTaxonomy(ctx context.Context, canonicalPath string) (entityPath, sectorPath string, err error) {
	if ep, sp, ok := ClassifyTaxonomyDeterministic(canonicalPath); ok {
		return ep, sp, nil
	}

	prompt := c.buildClassificationPrompt(canonicalPath)

	// Создаем типизированный верификатор
	verifier := ai.NewGenericVerifier(func(result TaxonomyClassificationResult) error {
		if result.EntityPath == "" {
			return fmt.Errorf("entityPath is required")
		}
		if result.SectorPath == "" {
			return fmt.Errorf("sectorPath is required")
		}
		// Проверяем формат путей (должны начинаться с /)
		if result.EntityPath[0] != '/' {
			return fmt.Errorf("entityPath must start with /")
		}
		if result.SectorPath[0] != '/' {
			return fmt.Errorf("sectorPath must start with /")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeDomainClassification,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7, // Lowered to reduce cost while keeping quality
			MinContextWindow:         4000,
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}

	result, err := c.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return "", "", fmt.Errorf("failed to classify taxonomy: %w", err)
	}

	// Преобразуем результат в типизированную структуру
	jsonData, err := json.Marshal(result)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal result: %w", err)
	}

	var classification TaxonomyClassificationResult
	if err := json.Unmarshal(jsonData, &classification); err != nil {
		return "", "", fmt.Errorf("failed to unmarshal classification: %w", err)
	}

	return classification.EntityPath, classification.SectorPath, nil
}

// buildClassificationPrompt строит промпт для классификации таксономии.
// Uses taxonomy.EntitySectorPromptForLLM as single source of truth for entityPath/sectorPath taxonomy.
func (c *TaxonomyClassifier) buildClassificationPrompt(canonicalPath string) string {
	return taxonomy.EntitySectorPromptForLLM(canonicalPath)
}

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
	Reasoning  string `json:"reasoning,omitempty"` // Обоснование классификации
}

// TaxonomyClassifier классифицирует canonicalPath в entityPath через LLM
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
func ClassifyTaxonomyDeterministic(canonicalPath string) (entityPath string, ok bool) {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 {
		return "", false
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
		return "/Geo/Region/Country", true
	case "market":
		return classifyMarketDeterministic(parts)
	case "person":
		return "/Person/Executive", true
	case "event":
		return "/Event", true
	case "deposit":
		return "/Resource/RawMaterial/Mineral", true
	case "mechanism":
		return "/Organization/Government/Agency", true
	case "decision":
		return "/Organization/Government/Agency", true
	case "program":
		return "/Organization/Government/Agency", true
	case "media":
		return "/Organization/Corporation/Private", true
	case "sphere":
		return "", false
	case "domain":
		return "", false
	}
	return "", false
}

func classifyGovDeterministic(parts []string) (string, bool) {
	slug := ""
	if len(parts) >= 3 {
		slug = parts[2]
	}
	switch {
	case containsAny(slug, "military", "defense", "defence", "army", "navy"):
		return "/Organization/Government/Military", true
	case containsAny(slug, "central-bank", "monetary", "reserve-bank"):
		return "/Organization/Government/Agency", true
	case containsAny(slug, "parliament", "congress", "assembly", "duma", "majlis"):
		return "/Organization/Government/Agency", true
	case containsAny(slug, "court", "judiciary", "tribunal"):
		return "/Organization/Government/Agency", true
	default:
		return "/Organization/Government/Agency", true
	}
}

var knownStateOwnedEnergy = map[string]bool{
	"socar": true, "aramco": true, "gazprom": true, "petrobras": true,
	"rosneft": true, "equinor": true, "petronas": true, "adnoc": true,
	"knpc": true, "nnpc": true, "sinopec": true, "cnooc": true,
	"cnpc": true, "botas": true, "saudi-aramco": true,
}

func classifyOrgDeterministic(parts []string) (string, bool) {
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
	return ep, true
}

func classifyFacDeterministic(parts []string) (string, bool) {
	slug := ""
	if len(parts) >= 3 {
		slug = parts[2]
	} else if len(parts) >= 2 {
		slug = parts[1]
	}
	if containsAny(slug, "solar") {
		return "/Infrastructure/Energy/PowerPlant/Renewable", true
	}
	if containsAny(slug, "wind") {
		return "/Infrastructure/Energy/PowerPlant/Renewable", true
	}
	if containsAny(slug, "renewable") {
		return "/Infrastructure/Energy/PowerPlant/Renewable", true
	}
	kind := taxonomy.FacilityKindFromSlug(slug)
	return taxonomy.FacilityKindToVertexEntityPath(kind), true
}

func classifyProductDeterministic(parts []string) (string, bool) {
	if len(parts) < 3 {
		return "/Resource/RawMaterial/Mineral", true
	}
	cat := parts[1]
	typ := parts[2]

	switch cat {
	case "raw":
		switch typ {
		case "mineral":
			return "/Resource/RawMaterial/Mineral", true
		case "agricultural":
			return "/Resource/RawMaterial/Agricultural", true
		case "chemical":
			return "/Resource/RawMaterial/Mineral", true
		case "energy":
			return "/Resource/Energy/Fossil", true
		default:
			return "/Resource/RawMaterial/Mineral", true
		}
	case "component":
		switch typ {
		case "electronic":
			return "/Resource/Component/Electronic", true
		case "mechanical":
			return "/Resource/Component/Mechanical", true
		default:
			return "/Resource/Component/Electronic", true
		}
	case "finished":
		return "/Resource/Component/Electronic", true
	case "service":
		return "/Organization/Corporation/Private", true
	default:
		return "/Resource/RawMaterial/Mineral", true
	}
}

func classifyMarketDeterministic(parts []string) (string, bool) {
	geo := ""
	if len(parts) >= 2 {
		geo = strings.ToLower(strings.TrimSpace(parts[1]))
	}

	// Market entityPath by geo scope (aligned with /market/{geo}/{domain}...)
	switch {
	case geo == "global" || geo == "no" || geo == "int":
		return "/Market/Geo/Global", true
	case taxonomy.IsRegionalGeoToken(geo):
		return "/Market/Geo/Regional", true
	case taxonomy.IsCompositeGeoToken(geo), len(geo) == 2 && geo[0] >= 'a' && geo[0] <= 'z' && geo[1] >= 'a' && geo[1] <= 'z':
		return "/Market/Geo/Country", true
	default:
		return "/Market/Domain/Industry", true
	}
}

func containsAny(s string, tokens ...string) bool {
	for _, t := range tokens {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

// ClassifyTaxonomy determines entityPath from canonicalPath, using deterministic
// rules first and falling back to LLM only for ambiguous cases.
func (c *TaxonomyClassifier) ClassifyTaxonomy(ctx context.Context, canonicalPath string) (entityPath string, err error) {
	if ep, ok := ClassifyTaxonomyDeterministic(canonicalPath); ok {
		return ep, nil
	}

	prompt := c.buildClassificationPrompt(canonicalPath)

	// Создаем типизированный верификатор
	verifier := ai.NewGenericVerifier(func(result TaxonomyClassificationResult) error {
		if result.EntityPath == "" {
			return fmt.Errorf("entityPath is required")
		}
		if result.EntityPath[0] != '/' {
			return fmt.Errorf("entityPath must start with /")
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
		return "", fmt.Errorf("failed to classify taxonomy: %w", err)
	}

	// Преобразуем результат в типизированную структуру
	jsonData, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	var classification TaxonomyClassificationResult
	if err := json.Unmarshal(jsonData, &classification); err != nil {
		return "", fmt.Errorf("failed to unmarshal classification: %w", err)
	}

	return classification.EntityPath, nil
}

// buildClassificationPrompt строит промпт для классификации таксономии.
// Uses taxonomy.EntityPathPromptForLLM as single source of truth for entityPath taxonomy.
func (c *TaxonomyClassifier) buildClassificationPrompt(canonicalPath string) string {
	return taxonomy.EntityPathPromptForLLM(canonicalPath)
}

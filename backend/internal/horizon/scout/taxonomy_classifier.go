package scout

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
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

// ClassifyTaxonomy определяет entityPath и sectorPath из canonicalPath через LLM
func (c *TaxonomyClassifier) ClassifyTaxonomy(ctx context.Context, canonicalPath string) (entityPath, sectorPath string, err error) {
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

// buildClassificationPrompt строит промпт для классификации таксономии
func (c *TaxonomyClassifier) buildClassificationPrompt(canonicalPath string) string {
	return fmt.Sprintf(`You are a taxonomy classification agent for a risk analysis system. Your task is to classify a canonical path into entityPath and sectorPath according to Core Taxonomy v1.0.

CANONICAL PATH TO CLASSIFY:
%s

TASK: Determine the entityPath and sectorPath for this canonical path based on Core Taxonomy v1.0.

ENTITY TYPES TAXONOMY (entityPath):
The entityPath represents the physical nature of the entity. Format: /Category/Type/SubType

Available categories:
- /Organization/Corporation/Public - Public companies
- /Organization/Corporation/Private - Private companies
- /Organization/Corporation/StateOwned - State-owned enterprises (SOE)
- /Organization/Government/Agency - Regulatory agencies, ministries
- /Organization/Government/Military - Military organizations
- /Organization/NonProfit/NGO - Non-profit organizations
- /Organization/International/IGO - International organizations (UN, WTO, NATO)
- /Infrastructure/Logistics/Seaport - Seaports
- /Infrastructure/Logistics/Airport - Airports
- /Infrastructure/Logistics/RailHub - Railway hubs
- /Infrastructure/Logistics/Warehouse - Warehouses
- /Infrastructure/Energy/PowerPlant/Nuclear - Nuclear power plants
- /Infrastructure/Energy/PowerPlant/Thermal - Thermal power plants
- /Infrastructure/Energy/PowerPlant/Renewable - Renewable energy plants
- /Infrastructure/Energy/Grid/Substation - Electrical substations
- /Infrastructure/Energy/Pipeline/Oil - Oil pipelines
- /Infrastructure/Energy/Pipeline/Gas - Gas pipelines
- /Infrastructure/Manufacturing/Factory - Factories
- /Infrastructure/Manufacturing/Mine - Mines
- /Infrastructure/Digital/DataCenter - Data centers
- /Infrastructure/Digital/InternetExchange - Internet exchange points
- /Resource/RawMaterial/Mineral - Minerals (Lithium, Cobalt)
- /Resource/RawMaterial/Agricultural - Agricultural products (Wheat, Soy)
- /Resource/Energy/Fossil - Fossil fuels (Oil, LNG)
- /Resource/Energy/Electricity - Electricity
- /Resource/Component/Electronic - Electronic components (Semiconductors)
- /Resource/Component/Mechanical - Mechanical components
- /Person/PEP - Politically Exposed Person
- /Person/Executive - Executive (CEO, CFO)
- /Person/Shareholder - Shareholder (UBO)
- /Geo/Region/Country - Country (ISO 3166-1 alpha-2: US, KZ, RU)
- /Geo/Region/Subdivision - Administrative division (ISO 3166-2: US-CA, KZ-75, RU-MOW)
- /Geo/Region/City - City with ISO6709 coordinates
- /Geo/Region/Metropolitan - Metropolitan area / Agglomeration
- /Event - Events

INDUSTRY & SECTOR TAXONOMY (sectorPath):
The sectorPath represents the industry sector. Format: /Sector/IndustryGroup/Industry/SubIndustry

Available sectors:
- /Energy/Fossil_Fuels/Oil_Gas_Upstream - Exploration and extraction
- /Energy/Fossil_Fuels/Oil_Gas_Midstream - Pipelines and storage
- /Energy/Fossil_Fuels/Oil_Gas_Downstream - Refining
- /Energy/Renewable/Solar - Solar energy
- /Energy/Renewable/Wind - Wind energy
- /Materials/Mining/Precious_Metals - Precious metals
- /Materials/Mining/Industrial_Metals - Industrial metals (Copper, Aluminum)
- /Materials/Mining/Rare_Earths - Rare earth elements
- /Materials/Chemicals/Specialty - Specialty chemicals
- /Materials/Chemicals/Fertilizers - Fertilizers
- /Industrials/Aerospace_Defense/Defense_Prime - Defense contractors
- /Industrials/Aerospace_Defense/Components - Components
- /Industrials/Machinery/Industrial_Automation - Industrial automation
- /Industrials/Transportation/Marine - Marine transportation
- /Industrials/Transportation/Air_Freight - Air freight
- /Industrials/Transportation/Rail - Railway transportation
- /Automotive/Vehicles/EV - Electric vehicles
- /Automotive/Components/Batteries - Batteries
- /Technology/Semiconductors/Design - Design (Fabless)
- /Technology/Semiconductors/Manufacturing - Manufacturing (Foundry)
- /Technology/Semiconductors/Equipment - Equipment (ASML, etc.)
- /Technology/Hardware/Consumer_Electronics - Consumer electronics
- /Technology/Software/Enterprise - Enterprise software
- /Technology/Software/Cybersecurity - Cybersecurity
- /Healthcare/Pharma/Biotech - Biotechnology
- /Healthcare/Pharma/Generics - Generics
- /Healthcare/Equipment/Medical_Devices - Medical devices
- /Financials/Banking/Systemic - Systemically important banks (G-SIBs)
- /Financials/Banking/Regional - Regional banks
- /Financials/Insurance/Reinsurance - Reinsurance
- /Financials/Capital_Markets/Asset_Management - Asset management
- /Public/Executive/Ministry - Executive branch / Ministry
- /Public/Legislative/Parliament - Legislative branch / Parliament
- /Public/Judicial/Court - Judicial branch / Court
- /Public/Central_Bank - Central bank
- /Geography - Geographic regions

OUTPUT FORMAT:
Return a JSON object with the following structure:
{
  "entityPath": "/Category/Type/SubType",
  "sectorPath": "/Sector/IndustryGroup/Industry/SubIndustry",
  "reasoning": "Brief explanation of the classification"
}

RULES:
1. Analyze the canonical path segments to determine the entity type
2. Use context clues from the path (e.g., "org/tsmc" suggests Organization/Corporation/Public)
3. Infer sector from entity type and path context (e.g., "fab" suggests Technology/Semiconductors/Manufacturing)
4. If uncertain, choose the most specific matching path from the taxonomy
5. Both paths must start with "/"
6. Return ONLY valid JSON, no markdown code fences or additional text

Return the classification as JSON:`, canonicalPath)
}

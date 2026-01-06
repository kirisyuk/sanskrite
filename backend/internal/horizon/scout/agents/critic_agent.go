package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// CriticAgent проверяет извлеченные данные на соответствие источникам
type CriticAgent struct {
	executor *ai.AITaskExecutor
}

// NewCriticAgent создает новый Critic Agent
func NewCriticAgent(executor *ai.AITaskExecutor) *CriticAgent {
	return &CriticAgent{
		executor: executor,
	}
}

// ValidationResult результат валидации Critic Agent
type ValidationResult struct {
	HasConflicts bool                 `json:"hasConflicts"`
	Conflicts    []ValidationConflict `json:"conflicts,omitempty"`
	Confidence   float64              `json:"confidence"` // 0..1
	IsValid      bool                 `json:"isValid"`
	MissingInfo  []string             `json:"missingInfo,omitempty"` // Список вопросов для доизучения
}

// ValidationConflict конфликт между извлеченными данными и источниками
type ValidationConflict struct {
	Field          string      `json:"field"`
	ExtractedValue interface{} `json:"extractedValue"`
	SourceValue    interface{} `json:"sourceValue"`
	Reason         string      `json:"reason"`
	Severity       string      `json:"severity"` // "low", "medium", "high"
}

// Validate проверяет извлеченные данные на соответствие источникам
func (a *CriticAgent) Validate(ctx context.Context, extractedData *models.HorizonFragment, relations []DiscoveredRelation, supplyChains []SupplyChain, sourceContent string, specialties []string) (*ValidationResult, error) {
	prompt := a.buildValidationPrompt(extractedData, relations, supplyChains, sourceContent, specialties)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         16000, // Увеличиваем окно, так как добавляем relations
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to validate data: %w", err)
	}

	// Преобразуем результат в ValidationResult
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var validationResult ValidationResult
	if err := json.Unmarshal(jsonData, &validationResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal validation result: %w", err)
	}

	return &validationResult, nil
}

// buildValidationPrompt строит промпт для валидации данных
func (a *CriticAgent) buildValidationPrompt(extractedData *models.HorizonFragment, relations []DiscoveredRelation, supplyChains []SupplyChain, sourceContent string, specialties []string) string {
	extractedJSON, _ := json.MarshalIndent(extractedData, "", "  ")
	relationsJSON, _ := json.MarshalIndent(relations, "", "  ")
	supplyChainsJSON := "[]"
	if len(supplyChains) > 0 {
		if b, err := json.MarshalIndent(supplyChains, "", "  "); err == nil {
			supplyChainsJSON = string(b)
		}
	}
	specialties = taxonomy.NormalizeSpecialties(specialties)
	specBlock := ""
	if len(specialties) > 0 {
		specBlock = "REQUESTED SPECIALTIES (apply only those listed):\n"
		specBlock += fmt.Sprintf("- %s\n\n", strings.Join(specialties, ", "))
	}

	supplyChainBlock := ""
	if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain) {
		supplyChainBlock = fmt.Sprintf(`
EXTRACTED SUPPLY CHAINS (JSON):
%s

`, supplyChainsJSON)
	}

	return fmt.Sprintf(`You are a validation agent (Critic) for a risk analysis system. Your task is to verify that extracted data matches the source content and assess the completeness of the knowledge graph.

EXTRACTED DATA (JSON):
%s

EXTRACTED RELATIONS (JSON):
%s
%s
SOURCE CONTENT:
%s

%s
TASK: Compare the extracted data with the source content and identify any conflicts, inconsistencies, or missing information. Pay close attention to missing relationships.

	Return a JSON object with:
	- hasConflicts: boolean indicating if conflicts were found
	- conflicts: array of conflict objects (if any) with fields: field, extractedValue, sourceValue, reason, severity
	- confidence: float (0..1) indicating confidence in the validation
	- isValid: boolean indicating if data is valid overall
	- missingInfo: array of strings (questions). Identify CRITICAL missing information based on the entity type (e.g., missing location for a factory, missing capacity metrics). Formulate each item as a specific search query to find this data.

	A conflict should be reported if:
	1. Extracted value contradicts information in source
	2. Extracted value is not supported by source
	3. Metrics are outside valid ranges or inconsistent

	Missing Info logic:
	- If important fields (like location, key metrics for the sector) are empty or unknown, add a specific question to 'missingInfo'.
	- If the entity is an Organization or Facility, and NO supply chain relations (suppliers, customers, competitors) were found, you MUST add a question to find them. Example: "Who are the key suppliers for Tesla?" or "What are the major competitors of this entity?"
	- If "management" specialty is requested for an Organization/Facility, you MUST ensure leadership/board/founder coverage:
	  - If there are no PERSON relations relevant to leadership (MANAGES / EMPLOYED_BY / BOARD_MEMBER_OF / FOUNDED), add specific questions like:
	    - "Who is the CEO/CFO/COO/CTO/CIO/CMO/CHRO of <org>?" and "Who are the board members/chair of <org>?" and "Who founded <org>?"
	  - NOTE: PERSON relations may appear inverted due to extraction being anchored on the organization; still treat them as valid signals for completeness.
	- If "products_and_services" specialty is requested:
	  - For ORGANIZATION/FACILITY: if there are no PRODUCT relations for outputs/inputs (PRODUCES / CONSUMES), add questions:
	    - "What products and services does <org> produce/offer?" and "What key inputs/materials/components does <org> consume?"
	  - For PRODUCT: if there are no product-to-product transformation relations (TRANSFORMS_INTO), add a question:
	    - "What is this product produced from and what does it transform into? Provide upstream/downstream products."
	- If "supply_chain" specialty is requested:
	  - Validate that extracted supply chains are COMPLETE and COHERENT:
	    - Nodes must alternate: actor (org/fac) → product → actor → product → ...
	    - Each chain should have at least 3 nodes (actor → product → actor)
	    - Products should have valid /product/... canonical paths
	  - If chains are incomplete or missing, add specific questions:
	    - "Who are the upstream suppliers and what materials do they provide?"
	    - "Who are the downstream customers and what products do they receive?"
	    - "What intermediate products/materials flow through this supply chain?"
	  - Check for GAPS: if an actor produces a product but no downstream consumer is found, flag it.
	- If "equity" specialty is requested:
	  - Ensure ownership/control relations are present (OWNS / SUBSIDIARY_OF / CONTROLS / INVESTS_IN).
	  - If none are found for an ORGANIZATION, add questions like:
	    - "Who owns <org> (shareholders/parent company/UBO)?" and "What subsidiaries does <org> own/control?"
	- If relations seem sparse or inconsistent with the entity's scale, flag it.

	Return ONLY valid JSON. Do not include markdown code fences.`, string(extractedJSON), string(relationsJSON), supplyChainBlock, sourceContent, specBlock)
}

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ScoutAgent извлекает структурированные данные из сырого контента
type ScoutAgent struct {
	executor *ai.AITaskExecutor
}

// NewScoutAgent создает новый Scout Agent
func NewScoutAgent(executor *ai.AITaskExecutor) *ScoutAgent {
	return &ScoutAgent{
		executor: executor,
	}
}

// DiscoveredRelation описывает найденную зависимость
type DiscoveredRelation struct {
	// TargetName SHOULD be the full legal/official name where relevant (e.g., "Tesla, Inc.")
	TargetName string `json:"targetName"`
	// TargetLegalName is the legal/official name (with form of ownership where relevant).
	TargetLegalName string `json:"targetLegalName,omitempty"`
	// TargetShortName is the normalized "base name" without ownership suffix (e.g., "Tesla").
	TargetShortName string `json:"targetShortName,omitempty"`
	// IMPORTANT: must match Horizon FragmentType enum values (not human words):
	// ORGANIZATION | FACILITY | GEO_REGION | PRODUCT | EVENT | PERSON
	TargetType   string  `json:"targetType"`
	RelationType string  `json:"relationType"` // SUPPLIES_TO, LOCATED_IN, OWNS, COMPETES_WITH
	Reason       string  `json:"reason"`
	Confidence   float64 `json:"confidence"`
	// IMPORTANT: must be a stable canonical path. DO NOT use /discovered/*
	TargetCanonicalPath string `json:"targetCanonicalPath"`
}

// ScoutResult результат работы Scout Agent
type ScoutResult struct {
	Fragment  models.HorizonFragment `json:"fragment"`
	Relations []DiscoveredRelation   `json:"relations"`
}

// Вспомогательная структура для парсинга, чтобы поймать name
type extractedFragment struct {
	models.HorizonFragment
	// Backward compatibility: some models may return "name"
	Name string `json:"name"`
	// Preferred outputs:
	LegalName string `json:"legalName,omitempty"`
	ShortName string `json:"shortName,omitempty"`
}

type scoutResultInternal struct {
	Fragment  extractedFragment    `json:"fragment"`
	Relations []DiscoveredRelation `json:"relations"`
}

// ExtractFragmentData извлекает данные HorizonFragment и связи из контента источника
func (a *ScoutAgent) ExtractFragmentData(ctx context.Context, content string, targetCanonicalPath string, entityPath string, sectorPath string) (*ScoutResult, error) {
	prompt := a.buildExtractionPrompt(content, targetCanonicalPath, entityPath, sectorPath)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       8,
			MinContextWindow:         16000, // Увеличиваем окно для связей
			// IMPORTANT: For large inputs + reasoning models, output budgets can be consumed by hidden "thinking",
			// leading to truncated JSON. We intentionally allow larger outputs here.
			MaxOutputSize: 16384,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to extract fragment data: %w", err)
	}

	// Преобразуем результат
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	// Unmarshal to internal structure to catch extra fields like Name
	var internalResult scoutResultInternal
	var scoutResult *ScoutResult

	if err := json.Unmarshal(jsonData, &internalResult); err == nil {
		scoutResult = &ScoutResult{
			Fragment:  internalResult.Fragment.HorizonFragment,
			Relations: internalResult.Relations,
		}
		// Handle name fields from internal struct (prefer LegalName)
		legal := strings.TrimSpace(internalResult.Fragment.LegalName)
		if legal == "" {
			legal = strings.TrimSpace(internalResult.Fragment.Name)
		}
		short := strings.TrimSpace(internalResult.Fragment.ShortName)

		if legal != "" {
			if scoutResult.Fragment.Labels == nil {
				scoutResult.Fragment.Labels = make(map[string]string)
			}
			scoutResult.Fragment.Labels["en"] = legal
		}
		if scoutResult.Fragment.Identifiers == nil {
			scoutResult.Fragment.Identifiers = make(map[string]string)
		}
		if legal != "" && strings.TrimSpace(scoutResult.Fragment.Identifiers["legalName"]) == "" {
			scoutResult.Fragment.Identifiers["legalName"] = legal
		}
		if short != "" && strings.TrimSpace(scoutResult.Fragment.Identifiers["shortName"]) == "" {
			scoutResult.Fragment.Identifiers["shortName"] = short
		}
	} else {
		// Fallback: возможно, модель вернула только Fragment (старый формат)
		var fragment models.HorizonFragment
		if err2 := json.Unmarshal(jsonData, &fragment); err2 == nil {
			scoutResult = &ScoutResult{Fragment: fragment}
		} else {
			return nil, fmt.Errorf("failed to unmarshal scout result: %w", err)
		}
	}

	// Common Defaults & Validation
	if scoutResult.Fragment.CanonicalPath == "" {
		scoutResult.Fragment.CanonicalPath = targetCanonicalPath
	}

	// Валидация Fragment (повторная, если нужно)
	if scoutResult.Fragment.CanonicalPath == "" {
		var fragment models.HorizonFragment
		if err := json.Unmarshal(jsonData, &fragment); err == nil && fragment.CanonicalPath != "" {
			scoutResult.Fragment = fragment
		}
	}

	// Handle Name (Fallback)
	if scoutResult.Fragment.Labels == nil {
		scoutResult.Fragment.Labels = make(map[string]string)
	}
	if scoutResult.Fragment.Identifiers == nil {
		scoutResult.Fragment.Identifiers = make(map[string]string)
	}
	if _, ok := scoutResult.Fragment.Labels["en"]; !ok || scoutResult.Fragment.Labels["en"] == "" {
		// Fallback name from path
		parts := strings.Split(targetCanonicalPath, "/")
		if len(parts) > 0 {
			name := strings.Title(strings.ReplaceAll(parts[len(parts)-1], "_", " "))
			scoutResult.Fragment.Labels["en"] = name
			if scoutResult.Fragment.Identifiers["legalName"] == "" {
				scoutResult.Fragment.Identifiers["legalName"] = name
			}
		}
	}
	// If shortName is missing, keep it consistent (fallback to label)
	if scoutResult.Fragment.Identifiers["shortName"] == "" && scoutResult.Fragment.Labels["en"] != "" {
		scoutResult.Fragment.Identifiers["shortName"] = scoutResult.Fragment.Labels["en"]
	}

	// Ensure Type is set
	if scoutResult.Fragment.Type == "" {
		// Infer type from canonicalPath (stable namespaces) or default to Organization.
		switch {
		case strings.Contains(targetCanonicalPath, "/person/"):
			scoutResult.Fragment.Type = models.FragmentTypePerson
		case strings.Contains(targetCanonicalPath, "/org/"):
			scoutResult.Fragment.Type = models.FragmentTypeOrganization
		case strings.Contains(targetCanonicalPath, "/product/"):
			scoutResult.Fragment.Type = models.FragmentTypeProduct
		case strings.Contains(targetCanonicalPath, "/fac/"):
			scoutResult.Fragment.Type = models.FragmentTypeFacility
		case strings.Contains(targetCanonicalPath, "/loc/") || strings.Contains(targetCanonicalPath, "/region/"):
			scoutResult.Fragment.Type = models.FragmentTypeGeoRegion
		case strings.Contains(targetCanonicalPath, "/event/"):
			scoutResult.Fragment.Type = models.FragmentTypeEvent
		default:
			scoutResult.Fragment.Type = models.FragmentTypeOrganization
		}
	}

	if scoutResult.Fragment.TenantID == "" {
		scoutResult.Fragment.TenantID = "global"
	}

	if scoutResult.Fragment.Governance.Owner == "" {
		scoutResult.Fragment.Governance.Owner = "SYSTEM"
	}
	if scoutResult.Fragment.Governance.Status == "" {
		scoutResult.Fragment.Governance.Status = string(models.FragmentStatusActive)
	}
	if scoutResult.Fragment.Metadata.EpistemicStatus == "" {
		scoutResult.Fragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
	}
	if scoutResult.Fragment.Metadata.CreatedAt.IsZero() {
		scoutResult.Fragment.Metadata.CreatedAt = time.Now().UTC()
	}
	scoutResult.Fragment.Metadata.LastUpdatedAt = time.Now().UTC()

	return scoutResult, nil
}

// buildExtractionPrompt строит промпт для извлечения данных
func (a *ScoutAgent) buildExtractionPrompt(content string, targetCanonicalPath string, entityPath string, sectorPath string) string {
	return fmt.Sprintf(`You are a data extraction agent (Scout) for a risk analysis system. Extract structured information about an entity and its dependencies from the provided content.

TARGET ENTITY:
- Canonical Path: %s
- Entity Type Path: %s
- Sector Path: %s

CONTENT TO ANALYZE:
%s

TASK: Extract all relevant information about this entity and return it as a JSON object with two main fields: "fragment" and "relations".

1. "fragment" Field (Object):
   Must match the HorizonFragment schema:
   - canonicalPath: Must match the target canonical path
   - entityPath: Must match the provided entity path
   - sectorPath: Must match the provided sector path
   - legalName: The legal/official name of the entity, INCLUDING the form of ownership when relevant (Inc., Ltd., LLC, GmbH, AG, S.A., PLC, etc.)
   - shortName: The base name WITHOUT the form of ownership (used for canonicalPath slugging)
   - state: Map of metrics (operational_health, capacity_utilization, etc.) with MetricValue objects
   - description: Brief description of the entity
   - homeLocation: Location object with ISO standards if available
   - identifiers: Map of identifiers (ISIN, LEI, Ticker, etc.) if available
   - kbRefs: Array of KB references (Wikidata, DBpedia) if available
   - metrics: All metrics must be in range [0.0..1.0] (Magnitude) or [-1.0..1.0] (Directional). Each metric must include: value, confidence (0..1), observedAt (ISO 8601), provenance.

2. "relations" Field (Array of Objects):
   Identify key dependencies, suppliers, customers, subsidiaries, competitors, or locations mentioned in the text.
   Each object must include:
   - targetName: Full legal/official name where relevant (e.g., "Tesla, Inc.")
   - targetLegalName: Same as targetName (explicit field)
   - targetShortName: Base name without ownership suffix (e.g., "Tesla")
   - targetType: MUST be one of: "ORGANIZATION" | "FACILITY" | "GEO_REGION" | "PRODUCT" | "EVENT" | "PERSON"
   - relationType: One of: SUPPLIES_TO, LOCATED_IN, OWNS, COMPETES_WITH, DEPENDS_ON
   - reason: Brief evidence/reason for this relation
   - confidence: 0.0 to 1.0
   - targetCanonicalPath: REQUIRED. Suggest a STABLE canonical path.

CANONICAL PATH RULES:
- NEVER use "/discovered/*" (this is not allowed).
- Prefer existing stable namespaces:
  - Organizations: /org/{lowercase-slug}
  - People: /person/{lowercase-slug}
  - Products: /product/{lowercase-slug}
  - Facilities: /fac/{lowercase-slug}
  - Geo regions / locations: prefer /region/... or /loc/... when you can be specific; otherwise /region/{lowercase-slug}
- canonicalPath slug SHOULD be derived from shortName (without ownership suffix) to avoid duplicates when legal form changes.
- ONLY include ownership suffix in canonicalPath if needed to disambiguate between different entities with same shortName.
- If you are unsure, still output your best stable canonicalPath (do not leave it empty).

Return ONLY valid JSON. Do not include markdown code fences.`, targetCanonicalPath, entityPath, sectorPath, content)
}

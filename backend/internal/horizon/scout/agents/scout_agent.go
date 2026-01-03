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
			MinCapabilityScore:       7,
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
			seg := strings.TrimSpace(parts[len(parts)-1])
			seg = strings.ReplaceAll(seg, "-", " ")
			seg = strings.ReplaceAll(seg, "_", " ")
			seg = strings.TrimSpace(seg)
			if seg != "" {
				// Best-effort title casing for readability when segment is slug-like.
				name := seg
				if name == strings.ToLower(name) {
					name = strings.Title(name)
				}
				scoutResult.Fragment.Labels["en"] = name
				if scoutResult.Fragment.Identifiers["legalName"] == "" {
					scoutResult.Fragment.Identifiers["legalName"] = name
				}
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
		case strings.Contains(targetCanonicalPath, "/media/"):
			scoutResult.Fragment.Type = models.FragmentTypeMediaChannel
		case strings.Contains(targetCanonicalPath, "/person/"):
			scoutResult.Fragment.Type = models.FragmentTypePerson
		case strings.Contains(targetCanonicalPath, "/org/"):
			scoutResult.Fragment.Type = models.FragmentTypeOrganization
		case strings.Contains(targetCanonicalPath, "/product/"):
			scoutResult.Fragment.Type = models.FragmentTypeProduct
		case strings.Contains(targetCanonicalPath, "/program/"):
			scoutResult.Fragment.Type = models.FragmentTypeProgram
		case strings.Contains(targetCanonicalPath, "/market/"):
			scoutResult.Fragment.Type = models.FragmentTypeMarket
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
   Must match the fragment schema:
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
   IMPORTANT: Relation direction is STRICT.
   Each relation object describes a directed edge FROM THIS FRAGMENT (source) TO the relation's targetCanonicalPath (target).
   Choose relationType to match this direction:
   - DEPENDS_ON: source depends on target (use this for suppliers, contractors, infrastructure).
     Example: If Panasonic supplies Tesla battery cells, for Tesla (source) -> Panasonic (target) use DEPENDS_ON (NOT SUPPLIES_TO).
   - SUPPLIES_TO: source supplies goods/services to target (use this for customers/recipients).
   - LOCATED_IN: source is located in target (target MUST be GEO_REGION).
   - OPERATES_IN: source operates in target (source MUST be ORGANIZATION, target MUST be GEO_REGION or FACILITY).
   - REGULATES: source regulates target (source MUST be ORGANIZATION - only orgs can regulate).
   - OWNS: source owns target (source MUST be ORGANIZATION or PERSON - only orgs/persons can own).
     CRITICAL: NEVER output OWNS where targetType=PERSON.
     If THIS FRAGMENT is owned/controlled by a person or parent org, use SUBSIDIARY_OF with the owner as target (it will be normalized to OWNS).
     Use OWNS ONLY for equity ownership/control relationships (shareholder, stake, holding company, ultimate beneficial owner).
     Do NOT use OWNS for roles/employment/management like CEO, director, founder, chairman; use INFLUENCES if relevant, or omit the relation.
     Examples:
       - If "Company X is owned by John Doe": emit SUBSIDIARY_OF from Company X -> John Doe (NOT OWNS from Company X -> John Doe).
       - If THIS FRAGMENT is a PERSON and they own Company X: emit OWNS from Person -> Company X.
   - CONTROLS: source controls target (non-equity/indirect control: voting control, state control, contractual control). Use when control is explicit but ownership is unclear.
   - MANAGES: source manages target (formal authority: CEO/executive/director/operator). Do NOT use for informal influence.
   - EMPLOYED_BY: source (PERSON) is employed by target (ORGANIZATION). Do NOT use for board-only or founder-only relationships.
   - BOARD_MEMBER_OF: source (PERSON) is a board member of target (ORGANIZATION).
   - FOUNDED: source (PERSON or ORGANIZATION) founded target (ORGANIZATION or PROGRAM). Not ownership.
   - INVESTS_IN: source (ORGANIZATION or PERSON) invests in target (ORGANIZATION/PROGRAM/PRODUCT). Not supply-chain.
   - PARTNERS_WITH: partnership/collaboration (symmetric; direction is not important but still output as source->target).
   - PRODUCES: source produces target (source MUST be ORGANIZATION or FACILITY, target MUST be PRODUCT).
   - COMPETES_WITH: competitor relationship (direction is not important, but still output as source->target).
   - INFLUENCES: source influences target.
   - SUBSIDIARY_OF: source is owned/controlled by target (target can be ORGANIZATION or PERSON; will be normalized to OWNS in graph).
   
   EDGE DIRECTION VALIDATION RULES (CRITICAL - edges violating these will be REJECTED or REVERSED):
   - REGULATES: source MUST be ORGANIZATION, target can be ORGANIZATION/FACILITY/PRODUCT/GEO_REGION
   - OWNS: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/FACILITY/PRODUCT
   - CONTROLS: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/FACILITY
   - MANAGES: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/FACILITY/PROGRAM
   - EMPLOYED_BY: source MUST be PERSON, target MUST be ORGANIZATION
   - BOARD_MEMBER_OF: source MUST be PERSON, target MUST be ORGANIZATION
   - FOUNDED: source MUST be PERSON or ORGANIZATION, target MUST be ORGANIZATION/PROGRAM
   - INVESTS_IN: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/PROGRAM/PRODUCT
   - PARTNERS_WITH: source/target MUST be ORGANIZATION or FACILITY (symmetric)
   - PRODUCES: source MUST be ORGANIZATION or FACILITY, target MUST be PRODUCT
   - LOCATED_IN: target MUST be GEO_REGION
   - OPERATES_IN: source MUST be ORGANIZATION, target MUST be GEO_REGION or FACILITY
   - SUPPLIES_TO: source and target can be ORGANIZATION or FACILITY
   - DEPENDS_ON: source can be ORGANIZATION/FACILITY/PRODUCT, target can be ORGANIZATION/FACILITY/PRODUCT/GEO_REGION
   
   Each object must include:
   - targetName: Full legal/official name where relevant (e.g., "Tesla, Inc.")
   - targetLegalName: Same as targetName (explicit field)
   - targetShortName: Base name without ownership suffix (e.g., "Tesla")
   - targetType: MUST be one of: "ORGANIZATION" | "FACILITY" | "GEO_REGION" | "PRODUCT" | "PROGRAM" | "MARKET" | "EVENT" | "PERSON" | "MEDIA_CHANNEL"
   - relationType: One of: SUPPLIES_TO, DEPENDS_ON, LOCATED_IN, OPERATES_IN, REGULATES, PRODUCES, CONSUMES, TRANSFORMS_INTO, OWNS, SUBSIDIARY_OF, CONTROLS, MANAGES, EMPLOYED_BY, BOARD_MEMBER_OF, FOUNDED, INVESTS_IN, PARTNERS_WITH, COMPETES_WITH, INFLUENCES
   - reason: Brief evidence/reason for this relation
   - confidence: 0.0 to 1.0
   - targetCanonicalPath: REQUIRED. Suggest a STABLE canonical path.

CANONICAL PATH RULES:
- NEVER use "/discovered/*" (this is not allowed).
- Prefer existing stable namespaces:
  - Organizations: /org/{lowercase-slug}
  - Media channels (last-mile endpoints): /media/{platform}/{slug}
    - Example: Telegram channel "В казани поймут" -> /media/telegram/v-kazani-poymut
  - People: /person/{lowercase-slug}
  - Products: MUST use hierarchical structure /product/{category}/{type}/{stage}/{slug}
    - category: "raw" | "component" | "finished" | "service"
    - type: "mineral" | "agricultural" | "chemical" | "electronic" | "mechanical" | "energy"
    - stage: "extraction" | "refining" | "processing" | "manufacturing" | "assembly"
    - slug: lowercase-slug derived from product name
    - Examples:
      - "Lithium Ore" -> /product/raw/mineral/extraction/lithium-ore
      - "Lithium Carbonate" -> /product/raw/chemical/refining/lithium-carbonate
      - "Battery Cell" -> /product/component/electronic/manufacturing/battery-cell
      - "Electric Vehicle" -> /product/finished/mechanical/assembly/electric-vehicle
    - If category/type/stage cannot be determined, use flat /product/{slug} as fallback
  - Programs:
    - Prefer /program/{jurisdictionSlug}/{programSlug} when jurisdiction is known
    - Otherwise /program/{programSlug}
  - Markets:
    - Prefer /market/{domainSlug}/{marketSlug} when domain is known
    - Otherwise /market/{marketSlug}
  - Facilities:
    - Prefer /fac/{orgSlug}/{facilitySlug} when org can be inferred (e.g., "TSMC Fab 21" -> /fac/tsmc/fab-21)
    - Otherwise /fac/{facilitySlug}
  - GEO regions / locations: MUST be /loc/{country}/{admin1}/{city}
    - country: ISO3166-1 alpha2 lowercase, EXACTLY 2 characters (e.g., us, de, ru, kz, jp)
    - admin1: subdivision code/slug lowercase; use "na" if not applicable/unknown
    - city: lowercase slug; use "na" if not applicable/unknown
    - country-only locations: /loc/{country}/na/na
    - CRITICAL: country code MUST be valid ISO3166-1 alpha2 (exactly 2 lowercase letters). Invalid codes will be REJECTED.
- VALIDATION POLICY RULES (MUST comply or output will be REJECTED):
  - canonicalPath MUST NOT start with "/discovered/" (will be rejected)
  - canonicalPath MUST match expected prefix for targetType (see namespaces above)
  - GEO_REGION paths MUST have exactly 4 parts: /loc/{country}/{admin1}/{city} with valid country code
  - Facilities: prefer /fac/{orgSlug}/{facilitySlug} over flat /fac/{facilitySlug} (flat paths get penalty)
- canonicalPath slug SHOULD be derived from shortName (without ownership suffix) to avoid duplicates when legal form changes.
- ONLY include ownership suffix in canonicalPath if needed to disambiguate between different entities with same shortName.
- If you are unsure, still output your best stable canonicalPath (do not leave it empty).

Return ONLY valid JSON. Do not include markdown code fences.`, targetCanonicalPath, entityPath, sectorPath, content)
}

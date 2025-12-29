package scout

import (
	"context"
	"fmt"
	"strings"

	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// ProductWikidataData contains extracted Wikidata data for a product.
type ProductWikidataData struct {
	QID             string   // Wikidata Q-ID (e.g., "Q409857")
	Label           string   // English label
	Description     string   // English description
	CASNumber       string   // P231 - CAS Registry Number
	ChemicalFormula string   // P274 - Chemical formula
	InstanceOf      []string // P31 - Instance of (for entityPath)
	SubclassOf      []string // P279 - Subclass of (for entityPath hierarchy)
	Industry        []string // P452 - Industry (for sectorPath)
	Uses            []string // P366 - Has use (applications)
	ProducedFrom    []string // P10565 - Is produced from (upstream)
	MaterialUsed    []string // P186 - Material used (upstream)
	Manufacturers   []string // P176 - Manufacturer (organizations that produce this product)
}

// ProductWikidataExtractor extracts product-specific data from Wikidata.
type ProductWikidataExtractor struct {
	wikidataClient coreontology.WikidataClientInterface
}

// NewProductWikidataExtractor creates a new product Wikidata extractor.
func NewProductWikidataExtractor(client coreontology.WikidataClientInterface) *ProductWikidataExtractor {
	return &ProductWikidataExtractor{
		wikidataClient: client,
	}
}

// ExtractProductData extracts product data from Wikidata by Q-ID.
func (e *ProductWikidataExtractor) ExtractProductData(ctx context.Context, qID string) (*ProductWikidataData, error) {
	if e.wikidataClient == nil {
		return nil, fmt.Errorf("wikidata client not configured")
	}
	if qID == "" {
		return nil, fmt.Errorf("qID is required")
	}

	// Get entity data from Wikidata
	entityData, err := e.wikidataClient.GetEntityData(ctx, qID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity data: %w", err)
	}
	if entityData == nil {
		return nil, fmt.Errorf("entity not found: %s", qID)
	}

	result := &ProductWikidataData{
		QID:        qID,
		Label:      entityData.Label,
		Description: entityData.Description,
	}

	// Extract specific properties
	if props := entityData.PropertiesRaw; props != nil {
		// P231 - CAS Registry Number
		if cas, ok := e.extractStringProperty(props, "P231"); ok {
			result.CASNumber = cas
		}

		// P274 - Chemical formula
		if formula, ok := e.extractStringProperty(props, "P274"); ok {
			result.ChemicalFormula = formula
		}

		// P31 - Instance of (can be multiple)
		if instanceOf, ok := e.extractEntityListProperty(props, "P31"); ok {
			result.InstanceOf = instanceOf
		}

		// P279 - Subclass of (can be multiple)
		if subclassOf, ok := e.extractEntityListProperty(props, "P279"); ok {
			result.SubclassOf = subclassOf
		}

		// P452 - Industry (can be multiple)
		if industry, ok := e.extractEntityListProperty(props, "P452"); ok {
			result.Industry = industry
		}

		// P366 - Has use (can be multiple)
		if uses, ok := e.extractEntityListProperty(props, "P366"); ok {
			result.Uses = uses
		}

		// P10565 - Is produced from (upstream, can be multiple)
		if producedFrom, ok := e.extractEntityListProperty(props, "P10565"); ok {
			result.ProducedFrom = producedFrom
		}

		// P186 - Material used (upstream, can be multiple)
		if materialUsed, ok := e.extractEntityListProperty(props, "P186"); ok {
			result.MaterialUsed = materialUsed
		}

		// P176 - Manufacturer (organizations that produce this product)
		if manufacturers, ok := e.extractEntityListProperty(props, "P176"); ok {
			result.Manufacturers = manufacturers
		}
	}

	return result, nil
}

// extractStringProperty extracts a string value from Wikidata properties.
func (e *ProductWikidataExtractor) extractStringProperty(props map[string]interface{}, propID string) (string, bool) {
	if props == nil {
		return "", false
	}

	value, ok := props[propID]
	if !ok {
		return "", false
	}

	// Handle array (take first value) — some props are extracted as multi-value arrays.
	if arr, ok := value.([]interface{}); ok && len(arr) > 0 {
		value = arr[0]
	}

	// Handle different value formats
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v), v != ""
	case map[string]interface{}:
		// Wikidata string value format: {"value": "Li2CO3", "type": "string"}
		if val, ok := v["value"].(string); ok {
			return strings.TrimSpace(val), true
		}
		// Try direct value extraction
		if val, ok := v["id"].(string); ok {
			return strings.TrimSpace(val), true
		}
	}

	// Fallback: convert to string
	if value != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", value)), true
	}

	return "", false
}

// extractEntityListProperty extracts a list of entity Q-IDs from Wikidata properties.
func (e *ProductWikidataExtractor) extractEntityListProperty(props map[string]interface{}, propID string) ([]string, bool) {
	if props == nil {
		return nil, false
	}

	value, ok := props[propID]
	if !ok {
		return nil, false
	}

	var result []string

	// Handle array of values (from improved WikidataClient extraction)
	if arr, ok := value.([]interface{}); ok {
		for _, item := range arr {
			if entityID := e.extractEntityID(item); entityID != "" {
				result = append(result, entityID)
			} else if str, ok := item.(string); ok && strings.HasPrefix(str, "Q") {
				result = append(result, str)
			}
		}
		if len(result) > 0 {
			return result, true
		}
	}

	// Handle single entity value (fallback for backward compatibility)
	if entityID := e.extractEntityID(value); entityID != "" {
		return []string{entityID}, true
	}

	// Handle string value (Q-ID as string)
	if str, ok := value.(string); ok && strings.HasPrefix(str, "Q") {
		return []string{str}, true
	}

	return nil, false
}

// extractEntityID extracts Q-ID from various Wikidata value formats.
func (e *ProductWikidataExtractor) extractEntityID(value interface{}) string {
	if value == nil {
		return ""
	}

	// Handle map format: {"entity-type": "item", "id": "Q42"}
	if valueMap, ok := value.(map[string]interface{}); ok {
		if entityType, ok := valueMap["entity-type"].(string); ok && entityType == "item" {
			if id, ok := valueMap["id"].(string); ok && strings.HasPrefix(id, "Q") {
				return id
			}
		}
		// Alternative format
		if id, ok := valueMap["id"].(string); ok && strings.HasPrefix(id, "Q") {
			return id
		}
	}

	// Handle string format
	if idStr, ok := value.(string); ok && strings.HasPrefix(idStr, "Q") {
		return idStr
	}

	return ""
}

// SearchProduct searches for a product in Wikidata and returns the best match Q-ID.
func (e *ProductWikidataExtractor) SearchProduct(ctx context.Context, productName string) (string, error) {
	if e.wikidataClient == nil {
		return "", fmt.Errorf("wikidata client not configured")
	}
	if productName == "" {
		return "", fmt.Errorf("product name is required")
	}

	// Search Wikidata
	response, err := e.wikidataClient.Search(ctx, productName, 5)
	if err != nil {
		return "", fmt.Errorf("wikidata search failed: %w", err)
	}
	if response == nil || len(response.Results) == 0 {
		return "", fmt.Errorf("product not found in Wikidata: %s", productName)
	}

	// Return first result's Q-ID
	return response.Results[0].ID, nil
}


package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// BaseVerifier базовая реализация верификатора с общими методами
type BaseVerifier struct{}

// CleanAndFixJSON очищает и исправляет JSON перед парсингом
func (v *BaseVerifier) CleanAndFixJSON(rawJSON string) (string, error) {
	if strings.TrimSpace(rawJSON) == "" {
		return "", fmt.Errorf("JSON string is empty")
	}

	// Удаляем BOM если есть
	rawJSON = strings.TrimPrefix(rawJSON, "\uFEFF")

	// Удаляем trailing commas перед } или ]
	re := regexp.MustCompile(`,\s*([}\]])`)
	fixed := re.ReplaceAllString(rawJSON, "$1")

	// Удаляем однострочные комментарии
	re = regexp.MustCompile(`//.*?\n`)
	fixed = re.ReplaceAllString(fixed, "\n")

	// Удаляем многострочные комментарии
	re = regexp.MustCompile(`/\*.*?\*/`)
	fixed = re.ReplaceAllString(fixed, "")

	// Исправляем одиночные кавычки на двойные (только если они используются как кавычки для строк)
	// Это упрощенная версия, для полной реализации нужен более сложный парсер
	re = regexp.MustCompile(`'([^']*?)'`)
	fixed = re.ReplaceAllStringFunc(fixed, func(s string) string {
		// Проверяем, является ли это строковым значением JSON
		if strings.HasPrefix(strings.TrimSpace(s), "':") || strings.HasSuffix(strings.TrimSpace(s), ":'") {
			return s
		}
		return `"` + s[1:len(s)-1] + `"`
	})

	// Удаляем дублирующиеся запятые
	re = regexp.MustCompile(`,\s*,`)
	fixed = re.ReplaceAllString(fixed, ",")

	// Исправляем missing commas между объектами/массивами
	re = regexp.MustCompile(`}\s*{`)
	fixed = re.ReplaceAllString(fixed, "},{")
	re = regexp.MustCompile(`]\s*\[`)
	fixed = re.ReplaceAllString(fixed, "],[")

	return strings.TrimSpace(fixed), nil
}

// ValidateAndParse проверяет валидность JSON и парсит его
func (v *BaseVerifier) ValidateAndParse(rawJSON string, target interface{}) error {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return fmt.Errorf("failed to clean JSON: %w", err)
	}

	if !json.Valid([]byte(cleaned)) {
		return fmt.Errorf("invalid JSON after cleaning")
	}

	if err := json.Unmarshal([]byte(cleaned), target); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// GenericVerifier универсальный верификатор для любого типа
type GenericVerifier[T any] struct {
	BaseVerifier
	// ValidateFunc дополнительная функция валидации после парсинга
	ValidateFunc func(T) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *GenericVerifier[T]) VerifyAndParse(rawJSON string) (interface{}, error) {
	return v.VerifyAndParseTyped(rawJSON)
}

// VerifyAndParseTyped реализация типизированного верификатора
func (v *GenericVerifier[T]) VerifyAndParseTyped(rawJSON string) (T, error) {
	var zero T
	var result T

	if err := v.ValidateAndParse(rawJSON, &result); err != nil {
		return zero, err
	}

	// Применяем дополнительную валидацию если она задана
	if v.ValidateFunc != nil {
		if err := v.ValidateFunc(result); err != nil {
			return zero, fmt.Errorf("validation failed: %w", err)
		}
	}

	return result, nil
}

// NewGenericVerifier создает новый экземпляр GenericVerifier
func NewGenericVerifier[T any](validateFunc func(T) error) *GenericVerifier[T] {
	return &GenericVerifier[T]{
		ValidateFunc: validateFunc,
	}
}

// SimpleJSONVerifier простой верификатор, который только проверяет валидность JSON
type SimpleJSONVerifier struct {
	BaseVerifier
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *SimpleJSONVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return result, nil
}

// NewSimpleJSONVerifier создает новый экземпляр SimpleJSONVerifier
func NewSimpleJSONVerifier() *SimpleJSONVerifier {
	return &SimpleJSONVerifier{}
}

// MapVerifier верификатор для map[string]interface{}
type MapVerifier struct {
	BaseVerifier
	// RequiredFields список обязательных полей
	RequiredFields []string
	// ValidateFunc дополнительная функция валидации
	ValidateFunc func(map[string]interface{}) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *MapVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Проверяем обязательные поля
	for _, field := range v.RequiredFields {
		if _, ok := result[field]; !ok {
			return nil, fmt.Errorf("required field '%s' is missing", field)
		}
	}

	// Применяем дополнительную валидацию если она задана
	if v.ValidateFunc != nil {
		if err := v.ValidateFunc(result); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return result, nil
}

// NewMapVerifier создает новый экземпляр MapVerifier
func NewMapVerifier(requiredFields []string, validateFunc func(map[string]interface{}) error) *MapVerifier {
	return &MapVerifier{
		RequiredFields: requiredFields,
		ValidateFunc:   validateFunc,
	}
}

// ArrayVerifier верификатор для массивов
type ArrayVerifier struct {
	BaseVerifier
	// MinItems минимальное количество элементов
	MinItems int
	// MaxItems максимальное количество элементов (0 = без ограничения)
	MaxItems int
	// ValidateFunc дополнительная функция валидации
	ValidateFunc func([]interface{}) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *ArrayVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result []interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Проверяем количество элементов
	if len(result) < v.MinItems {
		return nil, fmt.Errorf("array has %d items, but minimum is %d", len(result), v.MinItems)
	}

	if v.MaxItems > 0 && len(result) > v.MaxItems {
		return nil, fmt.Errorf("array has %d items, but maximum is %d", len(result), v.MaxItems)
	}

	// Применяем дополнительную валидацию если она задана
	if v.ValidateFunc != nil {
		if err := v.ValidateFunc(result); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return result, nil
}

// NewArrayVerifier создает новый экземпляр ArrayVerifier
func NewArrayVerifier(minItems, maxItems int, validateFunc func([]interface{}) error) *ArrayVerifier {
	return &ArrayVerifier{
		MinItems:     minItems,
		MaxItems:     maxItems,
		ValidateFunc: validateFunc,
	}
}

// SchemaVerifier верификатор с проверкой по JSON Schema (упрощенная версия)
type SchemaVerifier struct {
	BaseVerifier
	// SchemaValidateFunc функция проверки соответствия схеме
	SchemaValidateFunc func(interface{}) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *SchemaVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Применяем валидацию по схеме
	if v.SchemaValidateFunc != nil {
		if err := v.SchemaValidateFunc(result); err != nil {
			return nil, fmt.Errorf("schema validation failed: %w", err)
		}
	}

	return result, nil
}

// NewSchemaVerifier создает новый экземпляр SchemaVerifier
func NewSchemaVerifier(schemaValidateFunc func(interface{}) error) *SchemaVerifier {
	return &SchemaVerifier{
		SchemaValidateFunc: schemaValidateFunc,
	}
}

// RepairableVerifier верификатор с возможностью автоматического исправления данных
type RepairableVerifier struct {
	BaseVerifier
	// RepairFunc функция для исправления данных
	RepairFunc func(map[string]interface{}) (map[string]interface{}, error)
	// ValidateFunc функция валидации после исправления
	ValidateFunc func(map[string]interface{}) error
}

// VerifyAndParse реализация интерфейса AITaskResultVerifier
func (v *RepairableVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	cleaned, err := v.CleanAndFixJSON(rawJSON)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Применяем исправление если функция задана
	if v.RepairFunc != nil {
		repaired, err := v.RepairFunc(result)
		if err != nil {
			return nil, fmt.Errorf("repair failed: %w", err)
		}
		result = repaired
	}

	// Валидируем после исправления
	if v.ValidateFunc != nil {
		if err := v.ValidateFunc(result); err != nil {
			return nil, fmt.Errorf("validation after repair failed: %w", err)
		}
	}

	return result, nil
}

// NewRepairableVerifier создает новый экземпляр RepairableVerifier
func NewRepairableVerifier(repairFunc func(map[string]interface{}) (map[string]interface{}, error), validateFunc func(map[string]interface{}) error) *RepairableVerifier {
	return &RepairableVerifier{
		RepairFunc:   repairFunc,
		ValidateFunc: validateFunc,
	}
}

// Примеры специализированных верификаторов

// EntityExtractionVerifier верификатор для извлечения сущностей
type EntityExtractionVerifier struct {
	GenericVerifier[EntityExtractionResult]
}

// EntityExtractionResult результат извлечения сущностей
type EntityExtractionResult struct {
	Entities []Entity `json:"entities"`
}

// Entity сущность
type Entity struct {
	Type       string  `json:"type"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
}

// NewEntityExtractionVerifier создает новый верификатор для извлечения сущностей
func NewEntityExtractionVerifier() *EntityExtractionVerifier {
	return &EntityExtractionVerifier{
		GenericVerifier: GenericVerifier[EntityExtractionResult]{
			ValidateFunc: func(result EntityExtractionResult) error {
				if len(result.Entities) == 0 {
					return fmt.Errorf("no entities found")
				}
				for i, entity := range result.Entities {
					if entity.Type == "" {
						return fmt.Errorf("entity %d has empty type", i)
					}
					if entity.Value == "" {
						return fmt.Errorf("entity %d has empty value", i)
					}
				}
				return nil
			},
		},
	}
}

// SummarizationVerifier верификатор для суммаризации
type SummarizationVerifier struct {
	GenericVerifier[SummarizationResult]
}

// SummarizationResult результат суммаризации
type SummarizationResult struct {
	Summary    string   `json:"summary"`
	KeyPoints  []string `json:"keyPoints,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
}

// NewSummarizationVerifier создает новый верификатор для суммаризации
func NewSummarizationVerifier() *SummarizationVerifier {
	return &SummarizationVerifier{
		GenericVerifier: GenericVerifier[SummarizationResult]{
			ValidateFunc: func(result SummarizationResult) error {
				if result.Summary == "" {
					return fmt.Errorf("summary is empty")
				}
				if len(result.Summary) < 10 {
					return fmt.Errorf("summary is too short (minimum 10 characters)")
				}
				return nil
			},
		},
	}
}


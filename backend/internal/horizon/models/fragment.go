package models

import (
	"fmt"
	"strings"
	"time"
)

// HorizonFragment представляет узел графа знаний (всегда глобальный)
type HorizonFragment struct {
	// BaseOntologyFields (общие с Entity)
	ID          string            `json:"id" firestore:"id"`
	TenantID    string            `json:"tenantId" firestore:"tenantId"` // Всегда "global"
	Version     int               `json:"version" firestore:"version"`
	CreatedAt   time.Time         `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt" firestore:"updatedAt"`
	CreatedBy   string            `json:"createdBy" firestore:"createdBy"`
	UpdatedBy   string            `json:"updatedBy" firestore:"updatedBy"`
	Labels      map[string]string `json:"labels,omitempty" firestore:"labels,omitempty"`
	Description string            `json:"description,omitempty" firestore:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty" firestore:"tags,omitempty"`
	Status      string            `json:"status" firestore:"status"` // active, deprecated, draft, archived

	// Основные поля
	CanonicalPath string       `json:"canonicalPath" firestore:"canonicalPath"`
	Type          FragmentType `json:"type" firestore:"type"`
	EntityPath    string       `json:"entityPath" firestore:"entityPath"` // /Organization/Corporation/Public
	SectorPath    string       `json:"sectorPath" firestore:"sectorPath"` // /Technology/Semiconductors/Manufacturing

	// Связь с Entity
	EntityID *string `json:"entityId,omitempty" firestore:"entityId,omitempty"`

	// Гибкие метрики
	State map[string]MetricValue `json:"state" firestore:"state"`

	// Обогащение данными
	KBRefs      []KBReference     `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
	Identifiers map[string]string `json:"identifiers,omitempty" firestore:"identifiers,omitempty"`

	// Временная валидность
	ValidFrom *time.Time `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo   *time.Time `json:"validTo,omitempty" firestore:"validTo,omitempty"`

	// Location с ISO стандартами
	HomeLocation *Location `json:"homeLocation,omitempty" firestore:"homeLocation,omitempty"`

	// Provenance
	Provenance Provenance `json:"provenance" firestore:"provenance"`

	// Метаданные и управление
	Metadata   FragmentMetadata   `json:"metadata" firestore:"metadata"`
	Governance Governance         `json:"governance" firestore:"governance"`
}

// FragmentMetadata метаданные жизненного цикла
type FragmentMetadata struct {
	CreatedAt      time.Time      `json:"createdAt" firestore:"createdAt"`
	LastUpdatedAt  time.Time      `json:"lastUpdatedAt" firestore:"lastUpdatedAt"`
	LastVerifiedAt time.Time      `json:"lastVerifiedAt" firestore:"lastVerifiedAt"`
	// LastFactsSeenAt is a watermark for incremental "facts slice" processing (watchdog/scout).
	// It is independent from LastVerifiedAt to allow cheap fact-driven refresh without full LLM runs.
	LastFactsSeenAt time.Time `json:"lastFactsSeenAt" firestore:"lastFactsSeenAt"`
	// LastFactsAppliedAt is updated when fast-path applies facts to the fragment without LLM.
	LastFactsAppliedAt time.Time `json:"lastFactsAppliedAt" firestore:"lastFactsAppliedAt"`
	SourceFrameID  string         `json:"sourceFrameId" firestore:"sourceFrameId"`
	EpistemicStatus EpistemicStatus `json:"epistemicStatus" firestore:"epistemicStatus"`
}

// Validate проверяет валидность HorizonFragment
func (f *HorizonFragment) Validate() error {
	if f.CanonicalPath == "" {
		return fmt.Errorf("canonicalPath is required")
	}
	if f.TenantID != "global" && f.TenantID != "" {
		return fmt.Errorf("tenantId must be 'global' or empty for HorizonFragment")
	}
	if f.Type == "" {
		return fmt.Errorf("type is required")
	}
	if f.Governance.Owner != "SYSTEM" {
		return fmt.Errorf("governance.owner must be 'SYSTEM'")
	}
	if f.State == nil {
		f.State = make(map[string]MetricValue)
	}
	return nil
}

// GenerateVectorText генерирует vectorText для семантического поиска
// См. docs/horizon/architecture/semantic-search.md
func (f *HorizonFragment) GenerateVectorText() string {
	var parts []string

	// Канонический путь и имя
	parts = append(parts, f.CanonicalPath)
	if name, ok := f.Labels["en"]; ok && name != "" {
		parts = append(parts, name)
	}
	// Добавляем статус для контекста
	parts = append(parts, fmt.Sprintf("Status: %s", f.Status))
	if f.Metadata.EpistemicStatus != "" {
		parts = append(parts, fmt.Sprintf("Epistemic Status: %s", f.Metadata.EpistemicStatus))
	}

	if f.Description != "" {
		parts = append(parts, f.Description)
	}

	// Алиасы из labels
	for lang, label := range f.Labels {
		if lang != "en" && label != "" {
			parts = append(parts, label)
		}
	}

	// Иерархические пути (дублируются в разных форматах)
	if f.EntityPath != "" {
		parts = append(parts, f.EntityPath)
		// Разбиваем путь и добавляем варианты
		pathParts := strings.Split(strings.Trim(f.EntityPath, "/"), "/")
		parts = append(parts, strings.Join(pathParts, " "))
		if len(pathParts) > 0 {
			parts = append(parts, pathParts[len(pathParts)-1]) // Последний элемент
		}
	}

	// Отраслевой путь
	if f.SectorPath != "" {
		parts = append(parts, f.SectorPath)
		sectorParts := strings.Split(strings.Trim(f.SectorPath, "/"), "/")
		parts = append(parts, strings.Join(sectorParts, " "))
		if len(sectorParts) > 0 {
			parts = append(parts, sectorParts[len(sectorParts)-1])
		}
	}

	// Локация
	if f.HomeLocation != nil {
		if f.HomeLocation.Name != "" {
			parts = append(parts, f.HomeLocation.Name)
		}
		if f.HomeLocation.ISO3166_1 != nil {
			parts = append(parts, fmt.Sprintf("ISO3166-1: %s", *f.HomeLocation.ISO3166_1))
			parts = append(parts, *f.HomeLocation.ISO3166_1)
		}
		if f.HomeLocation.ISO3166_2 != nil {
			parts = append(parts, fmt.Sprintf("ISO3166-2: %s", *f.HomeLocation.ISO3166_2))
			parts = append(parts, *f.HomeLocation.ISO3166_2)
		}
		if f.HomeLocation.ISO6709 != nil {
			parts = append(parts, fmt.Sprintf("ISO6709: %s", *f.HomeLocation.ISO6709))
		}
	}

	// Метрики (название и значение)
	for metricName, metricValue := range f.State {
		parts = append(parts, fmt.Sprintf("%s: %.2f", metricName, metricValue.Value))
		// Добавляем человекочитаемый вариант
		metricNameHuman := strings.ReplaceAll(metricName, "_", " ")
		parts = append(parts, fmt.Sprintf("%s %.0f%%", metricNameHuman, metricValue.Value*100))
		if metricValue.Unit != nil {
			parts = append(parts, fmt.Sprintf("%s %s", metricNameHuman, *metricValue.Unit))
		}
	}

	// Идентификаторы
	for key, value := range f.Identifiers {
		parts = append(parts, fmt.Sprintf("%s: %s", key, value))
		parts = append(parts, value) // Только значение тоже добавляем
	}

	// Теги и метки
	if len(f.Tags) > 0 {
		parts = append(parts, fmt.Sprintf("Tags: %s", strings.Join(f.Tags, " ")))
		for _, tag := range f.Tags {
			parts = append(parts, tag)
		}
	}

	// KBRefs
	for _, kbRef := range f.KBRefs {
		parts = append(parts, fmt.Sprintf("%s: %s", kbRef.KB, kbRef.ExternalID))
		parts = append(parts, kbRef.ExternalID)
	}

	return strings.Join(parts, "\n")
}


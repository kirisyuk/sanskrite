package models

import (
	"fmt"
	"time"
)

// Frame представляет неизменяемый артефакт результата одного цикла работы Scout Engine
type Frame struct {
	ID                  string                 `json:"id" firestore:"id"`
	TenantID            string                 `json:"tenantId" firestore:"tenantId"`
	ProjectID           string                 `json:"projectId" firestore:"projectId"`
	ScoutRequestID      string                 `json:"scoutRequestId" firestore:"scoutRequestId"`
	TargetCanonicalPath string                 `json:"targetCanonicalPath" firestore:"targetCanonicalPath"`
	SourceIDs           []string               `json:"sourceIds" firestore:"sourceIds"`           // DataSource IDs
	SourceDocumentID    *string                `json:"sourceDocumentId,omitempty" firestore:"sourceDocumentId,omitempty"` // Для обратной совместимости
	DocumentIDs         []string               `json:"documentIds,omitempty" firestore:"documentIds,omitempty"` // Document IDs
	ExtractedState      map[string]MetricValue `json:"extractedState" firestore:"extractedState"`
	Evidence            []Evidence             `json:"evidence" firestore:"evidence"`
	Provenance          ClaimProvenance        `json:"provenance" firestore:"provenance"`
	KBRefs              []KBReference          `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
	Confidence          float64                `json:"confidence" firestore:"confidence"` // 0..1
	ErrorMargin         *ErrorMargin           `json:"errorMargin,omitempty" firestore:"errorMargin,omitempty"`
	ValidationMeta      ValidationMeta         `json:"validationMeta" firestore:"validationMeta"`
	CreatedAt           time.Time              `json:"createdAt" firestore:"createdAt"`
}

// ClaimProvenance детальная прослеживаемость для Frame
type ClaimProvenance struct {
	SourceIDs       []string         `json:"sourceIds" firestore:"sourceIds"`   // Для обратной совместимости
	SourceRefs      []SourceRef      `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"`
	DataSourceIDs   []string         `json:"dataSourceIds,omitempty" firestore:"dataSourceIds,omitempty"` // DataSource IDs
	DocumentIDs     []string         `json:"documentIds,omitempty" firestore:"documentIds,omitempty"`     // Document IDs
	Method          ExtractionMethod `json:"method" firestore:"method"`
	ExtractedAt     time.Time        `json:"extractedAt" firestore:"extractedAt"`
	EvidenceAnchors []string         `json:"evidenceAnchors,omitempty" firestore:"evidenceAnchors,omitempty"`
	License         *string           `json:"license,omitempty" firestore:"license,omitempty"`
}

// Evidence представляет доказательство из источника
type Evidence struct {
	SourceURL       string  `json:"sourceUrl" firestore:"sourceUrl"`
	SourceHash      string  `json:"sourceHash" firestore:"sourceHash"`
	ReliabilityScore float64 `json:"reliabilityScore" firestore:"reliabilityScore"`
	Quote           string  `json:"quote" firestore:"quote"`
}

// ValidationMeta метаданные процесса валидации
type ValidationMeta struct {
	ScoutLLMVersion   string `json:"scoutLlmVersion" firestore:"scoutLlmVersion"`
	CriticLLMVersion  string `json:"criticLlmVersion" firestore:"criticLlmVersion"`
	ChainOfThought    string `json:"chainOfThought" firestore:"chainOfThought"`
	ValidationResult  string `json:"validationResult" firestore:"validationResult"`
}

// Validate проверяет валидность Frame
func (f *Frame) Validate() error {
	if f.ScoutRequestID == "" {
		return fmt.Errorf("scoutRequestId is required")
	}
	if f.TargetCanonicalPath == "" {
		return fmt.Errorf("targetCanonicalPath is required")
	}
	if f.ProjectID == "" {
		return fmt.Errorf("projectId is required")
	}
	if f.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if f.ExtractedState == nil {
		f.ExtractedState = make(map[string]MetricValue)
	}
	if f.Confidence < 0 || f.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}


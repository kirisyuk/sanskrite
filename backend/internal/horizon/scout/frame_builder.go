package scout

import (
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// FrameBuilder создает Frame документы из результатов Scout
type FrameBuilder struct{}

// NewFrameBuilder создает новый FrameBuilder
func NewFrameBuilder() *FrameBuilder {
	return &FrameBuilder{}
}

// BuildFrame создает Frame из извлеченных данных Scout
func (b *FrameBuilder) BuildFrame(
	tenantID, projectID, scoutRequestID, targetCanonicalPath string,
	extractedState map[string]models.MetricValue,
	evidence []models.Evidence,
	provenance models.ClaimProvenance,
	kbRefs []models.KBReference,
	confidence float64,
	sourceIDs, documentIDs []string,
) *models.Frame {
	now := time.Now().UTC()

	return &models.Frame{
		ID:                  "", // Будет установлен при сохранении
		TenantID:            tenantID,
		ProjectID:           projectID,
		ScoutRequestID:      scoutRequestID,
		TargetCanonicalPath: targetCanonicalPath,
		SourceIDs:           sourceIDs,
		DocumentIDs:          documentIDs,
		ExtractedState:       extractedState,
		Evidence:             evidence,
		Provenance:           provenance,
		KBRefs:               kbRefs,
		Confidence:           confidence,
		ValidationMeta: models.ValidationMeta{
			ScoutLLMVersion:  "unknown", // Будет установлено из результата AI
			CriticLLMVersion: "unknown",
			ChainOfThought:   "",
			ValidationResult: "",
		},
		CreatedAt: now,
	}
}


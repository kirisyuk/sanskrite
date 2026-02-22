package extraction

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
)

func docContextID(nd *prov.NormalizedDocument, sourceID string) string {
	if nd.ExternalID != "" {
		return nd.ExternalID
	}
	h := sha1.Sum([]byte(strings.TrimSpace(nd.URL + nd.Title + sourceID)))
	return "ext:" + hex.EncodeToString(h[:8])
}

// NormalizedDocumentExtractor extracts ontology facts from NormalizedDocument
// without creating Document or Chunks. Used for facts-only RSS/Horizon Monitor path.
type NormalizedDocumentExtractor struct {
	exec     *ai.AITaskExecutor
	extractor *ontology.OntologyExtractor
}

func NewNormalizedDocumentExtractor(exec *ai.AITaskExecutor, extractor *ontology.OntologyExtractor) *NormalizedDocumentExtractor {
	return &NormalizedDocumentExtractor{exec: exec, extractor: extractor}
}

// ExtractAndPersist runs LLM extraction and persists ontology facts to GCS/Vertex.
func (e *NormalizedDocumentExtractor) ExtractAndPersist(
	ctx context.Context,
	tenantID string,
	sourceID string,
	nd *prov.NormalizedDocument,
) error {
	if e.exec == nil || e.extractor == nil {
		return nil
	}
	if strings.TrimSpace(nd.FullText) == "" {
		return nil
	}

	result, err := RunDocumentExtraction(ctx, e.exec, tenantID, nd.FullText, nd.Language)
	if err != nil {
		return err
	}
	if result == nil || result.Ontology == nil {
		return nil
	}

	dc := &ontology.DocumentContext{
		ID:                docContextID(nd, sourceID),
		SourceID:          sourceID,
		URL:               nd.URL,
		Title:             nd.Title,
		DocumentType:      strings.ToLower(strings.TrimSpace(nd.DocumentType)),
		FullText:          nd.FullText,
		OriginPublishedAt:  nd.OriginPublishedAt,
	}
	if dc.DocumentType == "" {
		dc.DocumentType = "article"
	}

	return e.extractor.ExtractFromDocumentContext(ctx, tenantID, dc, result.Ontology)
}

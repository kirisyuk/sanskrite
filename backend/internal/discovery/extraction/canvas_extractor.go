package extraction

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
)

const (
	// MaxCharsPerChunk limits combined text per LLM call (~80k tokens safe for most models).
	MaxCharsPerChunk = 80000
	// MaxDocsPerChunk caps documents per batch to avoid overload.
	MaxDocsPerChunk = 25
)

// CanvasDocument pairs a NormalizedDocument with its DataSource ID for batch extraction.
type CanvasDocument struct {
	ND       *prov.NormalizedDocument
	SourceID string
}

// BatchExtractionResult holds merged extraction from multiple document chunks.
type BatchExtractionResult struct {
	Ontology *ontology.DocumentExtraction
	BatchID  string
}

func canvasBatchID() string {
	h := sha1.Sum([]byte(fmt.Sprintf("canvas_%d", time.Now().UnixNano())))
	return "canvas_batch_" + hex.EncodeToString(h[:8])
}

// RunBatchDocumentExtraction runs LLM extraction on a canvas (multiple documents) in chunks.
// Each chunk is processed as one LLM call; events from a chunk get all that chunk's SourceIDs.
// Returns merged DocumentExtraction with SourceIDs set on each event.
func RunBatchDocumentExtraction(
	ctx context.Context,
	exec *ai.AITaskExecutor,
	tenantID string,
	docs []CanvasDocument,
) (*BatchExtractionResult, error) {
	if exec == nil || len(docs) == 0 {
		return nil, nil
	}

	// Build chunks: group docs until we hit char limit or max docs
	var chunks [][]CanvasDocument
	var current []CanvasDocument
	var currentChars int
	for _, d := range docs {
		if d.ND == nil || strings.TrimSpace(d.ND.FullText) == "" {
			continue
		}
		textLen := len(d.ND.FullText) + len(d.ND.Title) + 200 // overhead for markers
		if len(current) > 0 && (currentChars+textLen > MaxCharsPerChunk || len(current) >= MaxDocsPerChunk) {
			chunks = append(chunks, current)
			current = nil
			currentChars = 0
		}
		current = append(current, d)
		currentChars += textLen
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	if len(chunks) == 0 {
		return nil, nil
	}

	// Extract per chunk and merge
	mergedEntities := make(map[string]ontology.ExtractedEntity)
	mergedEvents := make([]ontology.ExtractedEvent, 0)
	lang := "en"

	for _, chunk := range chunks {
		combined := buildChunkText(chunk)
		sourceIDs := make([]string, 0, len(chunk))
		for _, d := range chunk {
			sid := strings.TrimSpace(d.SourceID)
			if sid != "" {
				sourceIDs = append(sourceIDs, sid)
			}
		}
		if len(combined) == 0 {
			continue
		}
		result, err := RunDocumentExtraction(ctx, exec, tenantID, combined, lang)
		if err != nil || result == nil || result.Ontology == nil {
			continue
		}
		for _, e := range result.Ontology.Entities {
			key := strings.ToLower(strings.TrimSpace(e.Type + ":" + e.Value))
			if key != "" && key != ":" {
				mergedEntities[key] = e
			}
		}
		for _, ev := range result.Ontology.Events {
			ev.SourceIDs = sourceIDs
			mergedEvents = append(mergedEvents, ev)
		}
	}

	if len(mergedEvents) == 0 {
		return nil, nil
	}

	entities := make([]ontology.ExtractedEntity, 0, len(mergedEntities))
	for _, e := range mergedEntities {
		entities = append(entities, e)
	}

	return &BatchExtractionResult{
		Ontology: &ontology.DocumentExtraction{
			Entities: entities,
			Events:   mergedEvents,
		},
		BatchID: canvasBatchID(),
	}, nil
}

// ExtractBatchAndPersist runs batch extraction and persists ontology facts.
// Used by canvas pipeline. Returns created OntologyFact IDs for downstream SignalPipeline.
func ExtractBatchAndPersist(
	ctx context.Context,
	exec *ai.AITaskExecutor,
	extractor *ontology.OntologyExtractor,
	tenantID string,
	docs []CanvasDocument,
) (*BatchExtractionResult, []string, error) {
	if exec == nil || extractor == nil {
		return nil, nil, nil
	}
	result, err := RunBatchDocumentExtraction(ctx, exec, tenantID, docs)
	if err != nil || result == nil {
		return nil, nil, err
	}
	factIDs, err := extractor.ExtractFromBatchExtraction(ctx, tenantID, result.Ontology, result.BatchID)
	if err != nil {
		return nil, factIDs, err
	}
	return result, factIDs, nil
}

func buildChunkText(chunk []CanvasDocument) string {
	var b strings.Builder
	for i, d := range chunk {
		if d.ND == nil {
			continue
		}
		title := strings.TrimSpace(d.ND.Title)
		if title == "" {
			title = "(no title)"
		}
		fmt.Fprintf(&b, "--- Article %d (sourceId: %s, title: %s) ---\n\n%s\n\n",
			i+1, strings.TrimSpace(d.SourceID), title, strings.TrimSpace(d.ND.FullText))
	}
	return b.String()
}

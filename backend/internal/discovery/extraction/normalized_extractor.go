package extraction

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
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
	exec      *ai.AITaskExecutor
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
	ensureFallbackEventForHighSignalDocument(result.Ontology, nd)

	dc := &ontology.DocumentContext{
		ID:                docContextID(nd, sourceID),
		SourceID:          sourceID,
		URL:               nd.URL,
		Title:             nd.Title,
		DocumentType:      strings.ToLower(strings.TrimSpace(nd.DocumentType)),
		FullText:          nd.FullText,
		OriginPublishedAt: nd.OriginPublishedAt,
	}
	if dc.DocumentType == "" {
		dc.DocumentType = "article"
	}

	return e.extractor.ExtractFromDocumentContext(ctx, tenantID, dc, result.Ontology)
}

func ensureFallbackEventForHighSignalDocument(out *ontology.DocumentExtraction, nd *prov.NormalizedDocument) {
	if out == nil || nd == nil || len(out.Events) > 0 {
		return
	}
	title := strings.TrimSpace(nd.Title)
	if !shouldSynthesizeFallbackEvent(title, nd.FullText) {
		return
	}
	dateISO := time.Now().UTC().Format(time.RFC3339)
	if nd.OriginPublishedAt != nil && !nd.OriginPublishedAt.IsZero() {
		dateISO = nd.OriginPublishedAt.UTC().Format(time.RFC3339)
	}
	summary := fallbackEventSummary(nd.FullText, title)
	out.Events = append(out.Events, ontology.ExtractedEvent{
		Title:       title,
		EventType:   fallbackEventType(title, summary),
		DateISO:     dateISO,
		Summary:     strPtr(summary),
		Location:    fallbackEventLocation(title, summary),
		SpherePaths: fallbackEventSpherePaths(title, summary),
	})
}

func shouldSynthesizeFallbackEvent(title, body string) bool {
	text := normalizeFallbackText(strings.Join([]string{title, body}, " "))
	if text == "" {
		return false
	}
	if !hasAnyFallbackKeyword(text,
		"attack", "strike", "airstrike", "missile", "drone", "shelling", "under fire", "killed", "injured",
		"closure", "closed to shipping", "block shipping", "blockade", "port closure", "operations suspended",
		"shipping disruption", "gps jamming", "tanker", "vessel", "maritime", "red sea", "bab el mandeb", "hormuz",
	) {
		return false
	}
	if hasAnyFallbackKeyword(text,
		"what it is", "what is it", "forecast", "analysis", "opinion", "explainer", "why ", "how ",
		"at a glance", "by the numbers", "live blog", "live updates",
	) && !hasAnyFallbackKeyword(text, "closure", "closed to shipping", "block shipping", "blockade") {
		return false
	}
	return true
}

func fallbackEventSummary(fullText, title string) string {
	fullText = strings.TrimSpace(fullText)
	title = strings.TrimSpace(title)
	if fullText == "" {
		return title
	}
	text := strings.TrimSpace(strings.TrimPrefix(fullText, title))
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 320 {
		text = strings.TrimSpace(text[:320])
	}
	if text == "" {
		return title
	}
	return text
}

func fallbackEventType(title, summary string) string {
	text := normalizeFallbackText(strings.Join([]string{title, summary}, " "))
	switch {
	case hasAnyFallbackKeyword(text, "closure", "closed to shipping", "block shipping", "blockade", "shipping disruption", "port closure", "gps jamming"):
		return "disruption"
	case hasAnyFallbackKeyword(text, "attack", "strike", "airstrike", "missile", "drone", "shelling", "under fire"):
		return "conflict"
	default:
		return "other"
	}
}

func fallbackEventSpherePaths(title, summary string) []string {
	text := normalizeFallbackText(strings.Join([]string{title, summary}, " "))
	out := make([]string, 0, 2)
	if hasAnyFallbackKeyword(text, "shipping", "maritime", "port", "tanker", "vessel", "red sea", "bab el mandeb", "hormuz") {
		out = append(out, "/sphere/logistics")
	}
	if hasAnyFallbackKeyword(text, "oil", "crude", "lng", "energy", "terminal", "refinery") {
		out = append(out, "/sphere/energy")
	}
	return out
}

func fallbackEventLocation(title, summary string) *ontology.ExtractedLocation {
	text := normalizeFallbackText(strings.Join([]string{title, summary}, " "))
	switch {
	case hasAnyFallbackKeyword(text, "bab el mandeb", "bab al mandab"):
		return &ontology.ExtractedLocation{Name: strPtr("Bab el-Mandeb")}
	case hasAnyFallbackKeyword(text, "strait of hormuz", "hormuz"):
		return &ontology.ExtractedLocation{Name: strPtr("Strait of Hormuz")}
	case hasAnyFallbackKeyword(text, "red sea"):
		return &ontology.ExtractedLocation{Name: strPtr("Red Sea")}
	default:
		return nil
	}
}

func normalizeFallbackText(s string) string {
	replacer := strings.NewReplacer(",", " ", ";", " ", ":", " ", "(", " ", ")", " ", "-", " ", "_", " ", "/", " ")
	s = strings.ToLower(strings.TrimSpace(replacer.Replace(s)))
	return strings.Join(strings.Fields(s), " ")
}

func hasAnyFallbackKeyword(text string, keywords ...string) bool {
	text = normalizeFallbackText(text)
	if text == "" {
		return false
	}
	for _, keyword := range keywords {
		keyword = normalizeFallbackText(keyword)
		if keyword != "" && strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func strPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

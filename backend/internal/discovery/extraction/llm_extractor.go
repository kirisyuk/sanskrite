package extraction

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/prompts"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func clampRange(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// ExtractionResult contains ontology extraction and document metadata from LLM.
type ExtractionResult struct {
	Ontology *ontology.DocumentExtraction
	Metadata *ExtractionMetadata
}

// ExtractionMetadata is document-level metadata (categories, topics, etc.) for Document persistence.
type ExtractionMetadata struct {
	Language     string
	DocumentType string
	Categories   []map[string]any
	Topics       []map[string]any
	Sentiment    float64
	KeyPoints    []string
	Outlinks     []map[string]any
}

// RunDocumentExtraction runs LLM extraction on fullText and returns ExtractionResult.
// Used by both DocumentProcessor (Document path) and NormalizedDocumentExtractor (facts-only path).
func RunDocumentExtraction(
	ctx context.Context,
	exec *ai.AITaskExecutor,
	tenantID string,
	fullText string,
	language string,
) (*ExtractionResult, error) {
	if exec == nil || strings.TrimSpace(fullText) == "" {
		return nil, nil
	}

	type extCategory struct {
		Label      string  `json:"label"`
		Confidence float64 `json:"confidence"`
	}
	type extTopic struct {
		Name  string  `json:"name"`
		Share float64 `json:"share"`
	}
	type extEntity struct {
		Type       string  `json:"type"`
		Value      string  `json:"value"`
		Confidence float64 `json:"confidence"`
	}
	type extOutlink struct {
		URL  string `json:"url"`
		Kind string `json:"kind"`
	}
	type extEventParticipant struct {
		EntityName string `json:"entityName"`
		Role       string `json:"role"`
	}
	type extLocation struct {
		Name          *string `json:"name,omitempty"`
		ISO6709       *string `json:"iso6709,omitempty"`
		CountryAlpha2 *string `json:"countryAlpha2,omitempty"`
		Admin1        *string `json:"admin1,omitempty"`
		City          *string `json:"city,omitempty"`
	}
	type extEvent struct {
		Title        string                `json:"title"`
		EventType    string                `json:"eventType,omitempty"`
		DateISO      string                `json:"dateISO"`
		Context      *string               `json:"context,omitempty"`
		Summary      *string               `json:"summary,omitempty"`
		Location     *extLocation          `json:"location,omitempty"`
		Participants []extEventParticipant `json:"participants,omitempty"`
		SpherePaths  []string              `json:"spherePaths,omitempty"`
	}
	type extClaim struct {
		Text              string   `json:"text"`
		Summary           *string  `json:"summary,omitempty"`
		Confidence        float64  `json:"confidence"`
		EvidenceURLs      []string `json:"evidenceUrls,omitempty"`
		SubjectEntityName *string  `json:"subjectEntityName,omitempty"`
		SpherePaths       []string `json:"spherePaths,omitempty"`
	}
	type extMetricUpdate struct {
		MetricName        string  `json:"metricName"`
		Value             float64 `json:"value"`
		Unit              *string `json:"unit,omitempty"`
		Confidence        float64 `json:"confidence"`
		ObservedAt        *string `json:"observedAt,omitempty"`
		SubjectEntityName *string `json:"subjectEntityName,omitempty"`
	}
	type docExtraction struct {
		Language      string            `json:"language"`
		Sentiment     float64           `json:"sentiment"`
		Categories    []extCategory     `json:"categories"`
		Topics        []extTopic        `json:"topics"`
		Entities      []extEntity       `json:"entities"`
		KeyPoints     []string          `json:"keyPoints"`
		DocumentType  string            `json:"documentType"`
		Outlinks      []extOutlink      `json:"outlinks"`
		Events        []extEvent        `json:"events"`
		Claims        []extClaim        `json:"claims"`
		MetricUpdates []extMetricUpdate `json:"metricUpdates"`
	}

	validate := func(x docExtraction) error {
		if len(strings.TrimSpace(x.Language)) != 2 {
			return fmt.Errorf("language must be ISO 639-1 code")
		}
		if x.Sentiment < -1 || x.Sentiment > 1 {
			return fmt.Errorf("sentiment out of range [-1..1]")
		}
		for _, c := range x.Categories {
			wds := strings.Fields(c.Label)
			if len(wds) == 0 || len(wds) > 2 {
				return fmt.Errorf("category label must be 1-2 words")
			}
			if c.Confidence < 0 || c.Confidence > 1 {
				return fmt.Errorf("category confidence out of range")
			}
		}
		for _, t := range x.Topics {
			if t.Name == "" || t.Share < 0 || t.Share > 1 {
				return fmt.Errorf("invalid topic entry")
			}
		}
		for _, e := range x.Entities {
			if e.Type == "" || e.Value == "" || e.Confidence < 0 || e.Confidence > 1 {
				return fmt.Errorf("invalid entity entry")
			}
		}
		enums := map[string]bool{"article": true, "post": true, "report": true, "announcement": true, "interview": true, "transcript": true, "other": true}
		if !enums[strings.ToLower(strings.TrimSpace(x.DocumentType))] {
			x.DocumentType = "other"
		}
		for _, ol := range x.Outlinks {
			if strings.TrimSpace(ol.URL) == "" {
				return fmt.Errorf("outlink url empty")
			}
		}
		for _, ev := range x.Events {
			if ev.Title == "" || ev.DateISO == "" {
				return fmt.Errorf("invalid event entry")
			}
			if ev.Location != nil && ev.Location.CountryAlpha2 != nil {
				cc := strings.TrimSpace(strings.ToLower(*ev.Location.CountryAlpha2))
				if len(cc) != 2 {
					return fmt.Errorf("event.location.countryAlpha2 must be ISO3166-1 alpha2 lowercase")
				}
			}
			for _, p := range ev.Participants {
				if strings.TrimSpace(p.EntityName) == "" {
					return fmt.Errorf("invalid event participant")
				}
			}
		}
		for _, cl := range x.Claims {
			if cl.Text == "" || cl.Confidence < 0 || cl.Confidence > 1 {
				return fmt.Errorf("invalid claim entry")
			}
		}
		for _, mu := range x.MetricUpdates {
			if strings.TrimSpace(mu.MetricName) == "" {
				continue
			}
			if mu.Confidence < 0 || mu.Confidence > 1 {
				return fmt.Errorf("metric confidence out of range")
			}
		}
		return nil
	}

	text := fullText
	if len(text) > 120000 {
		text = text[:120000]
	}
	lang := strings.TrimSpace(language)
	if lang == "" {
		lang = "en"
	}
	prompt := prompts.Format(prompts.DocumentExtractionPrompt, map[string]string{
		"language":      strings.ToLower(lang),
		"text":          text,
		"taxonomyRules": taxonomy.DocumentExtractionTaxonomyRules(),
	})

	ver := ai.NewGenericVerifier(validate)
	t := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent:  prompt,
		TenantID:     tenantID,
		UserID:       "system",
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:           4000,
		},
		Verifier: ver,
	}

	parsed, err := exec.ExecuteStructuredTask(ctx, t)
	if err != nil {
		return nil, err
	}
	ex, ok := parsed.(docExtraction)
	if !ok {
		return nil, fmt.Errorf("unexpected extraction type")
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)

	meta := &ExtractionMetadata{
		Language:     strings.ToLower(strings.TrimSpace(ex.Language)),
		DocumentType: strings.ToLower(strings.TrimSpace(ex.DocumentType)),
		Sentiment:    clampRange(ex.Sentiment, -1, 1),
		KeyPoints:    ex.KeyPoints,
	}
	cats := make([]map[string]any, 0, len(ex.Categories))
	for _, c := range ex.Categories {
		cats = append(cats, map[string]any{"label": c.Label, "confidence": clamp01(c.Confidence), "method": "llm", "at": nowStr})
	}
	meta.Categories = cats
	var topics []map[string]any
	for _, t := range ex.Topics {
		topics = append(topics, map[string]any{"name": t.Name, "share": clamp01(t.Share)})
	}
	meta.Topics = topics
	var outs []map[string]any
	for _, o := range ex.Outlinks {
		outs = append(outs, map[string]any{"url": o.URL, "kind": strings.ToLower(o.Kind)})
	}
	meta.Outlinks = outs

	out := &ontology.DocumentExtraction{
		Entities: make([]ontology.ExtractedEntity, 0, len(ex.Entities)),
		Events:   make([]ontology.ExtractedEvent, 0, len(ex.Events)),
		Claims:   make([]ontology.ExtractedClaim, 0, len(ex.Claims)),
		Metrics:  make([]ontology.ExtractedMetricUpdate, 0, len(ex.MetricUpdates)),
	}

	allowedEntityTypes := map[string]bool{
		"organization": true, "person": true, "product": true, "facility": true,
		"program": true, "market": true, "governmentproject": true, "supplychainnode": true,
	}
	for _, e := range ex.Entities {
		t := strings.ToLower(strings.TrimSpace(e.Type))
		if !allowedEntityTypes[t] {
			continue
		}
		out.Entities = append(out.Entities, ontology.ExtractedEntity{
			Type:       e.Type,
			Value:      e.Value,
			Confidence: clamp01(e.Confidence),
		})
	}
	for _, ev := range ex.Events {
		out.Events = append(out.Events, ontology.ExtractedEvent{
			Title:       ev.Title,
			EventType:   strings.TrimSpace(ev.EventType),
			DateISO:     ev.DateISO,
			Context:     ev.Context,
			Summary:     ev.Summary,
			SpherePaths: ev.SpherePaths,
			Location: func() *ontology.ExtractedLocation {
				if ev.Location == nil {
					return nil
				}
				return &ontology.ExtractedLocation{
					Name:          ev.Location.Name,
					ISO6709:       ev.Location.ISO6709,
					CountryAlpha2: ev.Location.CountryAlpha2,
					Admin1:        ev.Location.Admin1,
					City:          ev.Location.City,
				}
			}(),
			Participants: func() []ontology.ExtractedEventParticipant {
				if len(ev.Participants) == 0 {
					return nil
				}
				p := make([]ontology.ExtractedEventParticipant, 0, len(ev.Participants))
				for _, x := range ev.Participants {
					p = append(p, ontology.ExtractedEventParticipant{EntityName: x.EntityName, Role: x.Role})
				}
				return p
			}(),
		})
	}
	for _, c := range ex.Claims {
		out.Claims = append(out.Claims, ontology.ExtractedClaim{
			Text:              c.Text,
			Summary:           c.Summary,
			Confidence:        clamp01(c.Confidence),
			EvidenceURLs:      c.EvidenceURLs,
			SubjectEntityName: c.SubjectEntityName,
			SpherePaths:       c.SpherePaths,
		})
	}
	for _, mu := range ex.MetricUpdates {
		if strings.TrimSpace(mu.MetricName) == "" {
			continue
		}
		out.Metrics = append(out.Metrics, ontology.ExtractedMetricUpdate{
			MetricName:        strings.TrimSpace(mu.MetricName),
			Value:             mu.Value,
			Unit:              mu.Unit,
			Confidence:        clamp01(mu.Confidence),
			ObservedAt:        mu.ObservedAt,
			SubjectEntityName: mu.SubjectEntityName,
		})
	}

	return &ExtractionResult{Ontology: out, Metadata: meta}, nil
}

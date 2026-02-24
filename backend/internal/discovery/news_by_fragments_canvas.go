package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// FragmentInput is a fragment with canonical path and optional display name for the Gemini prompt.
type FragmentInput struct {
	CanonicalPath string `json:"canonicalPath"`
	CanonicalName string `json:"canonicalName,omitempty"`
}

// FragmentLookup resolves canonical path to display label (optional, for enriching FragmentInput).
type FragmentLookup interface {
	GetByCanonicalPath(ctx context.Context, path string) (*FragmentInfo, error)
}

// FragmentInfo holds minimal fragment data.
type FragmentInfo struct {
	CanonicalPath string
	Label         string
	Type          string
}

// NewsByFragmentsCanvas fetches news via Gemini with search grounding, creates ontology facts
// with CanonicalNames, runs 12-axis + propagation, and publishes result via WS.
// Uses LLM-search quasi-source (DataSource) for provenance and Watchdog/Scout compatibility.
type NewsByFragmentsCanvas struct {
	exec           *ai.AITaskExecutor
	factsRepo      *repo.OntologyFactsRepository
	dataSourcesRepo *repo.DataSourcesRepository
	signalPipeline EventSignalProcessor
	fragmentLookup FragmentLookup
	publisher      func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	logger         *log.Logger
}

// NewsByFragmentsCanvasConfig holds dependencies.
type NewsByFragmentsCanvasConfig struct {
	Exec            *ai.AITaskExecutor
	FactsRepo       *repo.OntologyFactsRepository
	DataSourcesRepo *repo.DataSourcesRepository
	SignalPipeline  EventSignalProcessor
	FragmentLookup  FragmentLookup
	Publisher       func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	Logger          *log.Logger
}

// normalizeCanonicalPath trims and removes trailing slash for consistent key matching.
func normalizeCanonicalPath(cp string) string {
	cp = strings.TrimSpace(cp)
	for len(cp) > 1 && cp[len(cp)-1] == '/' {
		cp = cp[:len(cp)-1]
	}
	return cp
}

// NewNewsByFragmentsCanvas creates the service.
func NewNewsByFragmentsCanvas(cfg NewsByFragmentsCanvasConfig) *NewsByFragmentsCanvas {
	return &NewsByFragmentsCanvas{
		exec:            cfg.Exec,
		factsRepo:       cfg.FactsRepo,
		dataSourcesRepo: cfg.DataSourcesRepo,
		signalPipeline:  cfg.SignalPipeline,
		fragmentLookup:  cfg.FragmentLookup,
		publisher:       cfg.Publisher,
		logger:          cfg.Logger,
	}
}

// NewsByPathItem is a single news item for a canonical path.
type NewsByPathItem struct {
	FactID         string     `json:"factId"`
	Title          string     `json:"title"`
	Summary        string     `json:"summary,omitempty"`
	OccurredAt     *time.Time `json:"occurredAt,omitempty"`
	SourceURL      string     `json:"sourceUrl,omitempty"`
	CanonicalPath  string     `json:"canonicalPath"`
	CanonicalNames []string   `json:"canonicalNames,omitempty"`
	// Business semantics from LLM
	SpherePaths  []string `json:"spherePaths,omitempty"`  // e.g. /sphere/geopolitics, /sphere/logistics
	Polarity     string   `json:"polarity,omitempty"`     // risk | opportunity | neutral
	ImpactLevel  string   `json:"impactLevel,omitempty"` // low | medium | high | critical
	EventType    string   `json:"eventType,omitempty"`    // incident, regulatory_change, market_move, etc.
}

// NewsByFragmentsResult is the structured result sent via WS.
type NewsByFragmentsResult struct {
	JobID            string                    `json:"jobId"`
	TenantID         string                    `json:"tenantId"`
	NewsByPath       map[string][]NewsByPathItem `json:"newsByPath"`
	EventsCreated    int                       `json:"eventsCreated"`
	SignalsProcessed int                       `json:"signalsProcessed"`
}

// geminiNewsOutput is the JSON schema we ask Gemini to return.
type geminiNewsOutput struct {
	Fragments []struct {
		CanonicalPath string `json:"canonicalPath"`
		CanonicalName string `json:"canonicalName"`
		News          []struct {
			Title       string   `json:"title"`
			Summary     string   `json:"summary,omitempty"`
			OccurredAt  string   `json:"occurredAt,omitempty"` // ISO date
			SourceURL   string   `json:"sourceUrl,omitempty"`
			Location    *string  `json:"location,omitempty"`
			CountryCode string   `json:"countryCode,omitempty"`
			// Business semantics (LLM-inferred from content)
			SpherePaths  []string `json:"spherePaths,omitempty"`  // e.g. geopolitics, logistics, policy
			Polarity     string   `json:"polarity,omitempty"`    // risk | opportunity | neutral
			ImpactLevel  string   `json:"impactLevel,omitempty"` // low | medium | high | critical
			EventType    string   `json:"eventType,omitempty"`    // incident, regulatory_change, market_move, etc.
		} `json:"news"`
	} `json:"fragments"`
}

// Run calls Gemini with search grounding, parses structured JSON, creates ontology facts
// with CanonicalNames, runs 12-axis, and publishes via WS.
func (n *NewsByFragmentsCanvas) Run(ctx context.Context, tenantID string, fragments []FragmentInput, daysBack int, jobID string) (*NewsByFragmentsResult, error) {
	if strings.TrimSpace(tenantID) == "" {
		tenantID = "global"
	}
	if daysBack <= 0 {
		daysBack = 7
	}
	if jobID == "" {
		jobID = fmt.Sprintf("news_by_fragments_%d", time.Now().Unix())
	}

	// Ensure LLM-search quasi-source for provenance (Watchdog/Scout compatibility)
	quasiSourceID := ""
	if n.dataSourcesRepo != nil {
		if id, err := n.dataSourcesRepo.EnsureLLMSearchQuasiSource(ctx); err == nil {
			quasiSourceID = id
		} else if n.logger != nil {
			n.logger.Printf("news_by_fragments: EnsureLLMSearchQuasiSource failed: %v", err)
		}
	}

	refDate := time.Now().UTC()
	refDateStr := refDate.Format("2006-01-02")

	// Enrich fragments with labels from lookup if canonicalName is empty
	for i := range fragments {
		f := &fragments[i]
		f.CanonicalPath = strings.TrimSpace(f.CanonicalPath)
		if f.CanonicalPath == "" {
			continue
		}
		if f.CanonicalName == "" && n.fragmentLookup != nil {
			if info, err := n.fragmentLookup.GetByCanonicalPath(ctx, f.CanonicalPath); err == nil && info != nil && info.Label != "" {
				f.CanonicalName = strings.TrimSpace(info.Label)
			}
		}
		if f.CanonicalName == "" {
			f.CanonicalName = f.CanonicalPath
		}
	}

	// Fetch existing event facts per fragment to exclude from LLM output (avoid duplicates on repeated runs)
	existingByPath := n.fetchExistingEventTitles(ctx, tenantID, fragments)

	// Build prompt for Gemini with search grounding
	prompt := n.buildGeminiPrompt(fragments, daysBack, refDateStr, existingByPath)
	verifier := ai.NewGenericVerifier(func(x geminiNewsOutput) error {
		if len(x.Fragments) == 0 {
			return nil
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		TenantID:     tenantID,
		UserID:       "system",
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
			MinCapabilityScore:        6,
			MaxOutputSize:             16384, // 16K: search grounding + thinking can consume output budget
		},
		Verifier: verifier,
	}

	parsed, _, err := n.exec.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("gemini news search: %w", err)
	}
	out, ok := parsed.(geminiNewsOutput)
	if !ok {
		return nil, fmt.Errorf("gemini news search: unexpected response type %T", parsed)
	}

	// Create ontology facts from Gemini output
	now := time.Now().UTC()
	pathSet := make(map[string]struct{})
	for _, f := range fragments {
		p := strings.TrimSpace(f.CanonicalPath)
		if p != "" {
			pathSet[p] = struct{}{}
		}
	}

	var factIDs []string
	newsByPath := make(map[string][]NewsByPathItem)
	// Ensure all requested paths are present (even with empty array) so frontend can stop loading.
	for _, f := range fragments {
		cp := normalizeCanonicalPath(f.CanonicalPath)
		if cp != "" {
			newsByPath[cp] = []NewsByPathItem{}
		}
	}

	for _, frag := range out.Fragments {
		cp := normalizeCanonicalPath(frag.CanonicalPath)
		if cp == "" {
			continue
		}
		canonicalName := strings.TrimSpace(frag.CanonicalName)
		if canonicalName == "" {
			canonicalName = cp
		}
		canonicalNames := []string{canonicalName}

		for _, news := range frag.News {
			title := strings.TrimSpace(news.Title)
			if title == "" {
				continue
			}
			summary := strings.TrimSpace(news.Summary)
			var occurredAt *time.Time
			if news.OccurredAt != "" {
				if t, err := time.Parse("2006-01-02", news.OccurredAt); err == nil {
					occurredAt = &t
				} else if t, err := time.Parse(time.RFC3339, news.OccurredAt); err == nil {
					occurredAt = &t
				}
			}
			if occurredAt == nil {
				occurredAt = &now
			}

			sourceIDs := []string{}
			if quasiSourceID != "" {
				sourceIDs = []string{quasiSourceID}
			} else {
				sourceIDs = []string{"path:" + cp}
			}
			sourceRefs := []models.SourceRef{}
			if url := strings.TrimSpace(news.SourceURL); url != "" {
				sourceRefs = append(sourceRefs, models.SourceRef{SourceID: url, SourceType: models.SourceTypeWeb})
			}
			if quasiSourceID != "" {
				sourceRefs = append(sourceRefs, models.SourceRef{SourceID: quasiSourceID, SourceType: models.SourceTypeLLMSearchGrounding})
			}

			eventType := strings.TrimSpace(news.EventType)
			if eventType == "" {
				eventType = "incident"
			}
			eventType = taxonomy.NormalizeEventType(eventType)

			spherePaths := taxonomy.NormalizeSpherePaths(news.SpherePaths)
			polarity := strings.ToLower(strings.TrimSpace(news.Polarity))
			if polarity != "risk" && polarity != "opportunity" && polarity != "neutral" {
				polarity = "neutral"
			}
			impactLevel := strings.ToLower(strings.TrimSpace(news.ImpactLevel))
			if impactLevel != "low" && impactLevel != "medium" && impactLevel != "high" && impactLevel != "critical" {
				impactLevel = "medium"
			}

			tags := []string{"news-by-fragments"}
			if polarity != "" {
				tags = append(tags, "polarity:"+polarity)
			}
			if impactLevel != "" {
				tags = append(tags, "impact:"+impactLevel)
			}

			fact := &models.OntologyFact{
				BaseOntologyFields: models.BaseOntologyFields{
					TenantID:       tenantID,
					Version:        1,
					CreatedAt:      now,
					UpdatedAt:      now,
					CreatedBy:      "system",
					UpdatedBy:      "system",
					Labels:         map[string]string{"en": title},
					Status:         "active",
					CanonicalPaths: []string{cp},
					SpherePaths:    spherePaths,
					Tags:           tags,
				},
				Kind:           "event",
				Type:           eventType,
				Title:          title,
				Summary:        summary,
				OccurredAt:     occurredAt,
				ObservedAt:     &now,
				SourceIDs:      sourceIDs,
				SourceRefs:     sourceRefs,
				Confidence:     0.85,
				CanonicalNames: canonicalNames,
			}
			if news.Location != nil && strings.TrimSpace(*news.Location) != "" {
				place := strings.TrimSpace(*news.Location)
				fact.EventPlace = &place
			}
			if news.CountryCode != "" {
				locPath := taxonomy.GeoCanonicalPath(strings.ToLower(news.CountryCode), "na", "na")
				if locPath != "" {
					fact.LocationPath = &locPath
					fact.CanonicalPaths = append(fact.CanonicalPaths, locPath)
				}
			}
			fact.VectorText = fact.GenerateVectorText()

			factID, createErr := n.factsRepo.Create(ctx, fact)
			if createErr != nil {
				if n.logger != nil {
					n.logger.Printf("news_by_fragments: Create fact failed: %v", createErr)
				}
				continue
			}
			factIDs = append(factIDs, factID)

			// Run SignalPipeline (12-axis)
			if n.signalPipeline != nil {
				if err := n.signalPipeline.ProcessEvent(ctx, fact); err != nil && n.logger != nil {
					n.logger.Printf("news_by_fragments: ProcessEvent failed factId=%s: %v", factID, err)
				}
			}

			item := NewsByPathItem{
				FactID:         factID,
				Title:          title,
				Summary:        summary,
				OccurredAt:     occurredAt,
				SourceURL:      strings.TrimSpace(news.SourceURL),
				CanonicalPath:  cp,
				CanonicalNames: canonicalNames,
				SpherePaths:   spherePaths,
				Polarity:      polarity,
				ImpactLevel:   impactLevel,
				EventType:     eventType,
			}
			newsByPath[cp] = append(newsByPath[cp], item)
		}
	}

	signalsProcessed := len(factIDs)
	result := &NewsByFragmentsResult{
		JobID:            jobID,
		TenantID:         tenantID,
		NewsByPath:       newsByPath,
		EventsCreated:    len(factIDs),
		SignalsProcessed: signalsProcessed,
	}

	n.publishResult(ctx, tenantID, result)

	if n.logger != nil {
		n.logger.Printf("news_by_fragments: jobId=%s events=%d paths=%d",
			jobID, len(factIDs), len(newsByPath))
	}

	_ = pathSet
	return result, nil
}

// fetchExistingEventTitles returns event titles (and source URLs) per canonical path for deduplication.
func (n *NewsByFragmentsCanvas) fetchExistingEventTitles(ctx context.Context, tenantID string, fragments []FragmentInput) map[string][]string {
	if n.factsRepo == nil {
		return nil
	}
	out := make(map[string][]string)
	for _, f := range fragments {
		cp := strings.TrimSpace(f.CanonicalPath)
		if cp == "" {
			continue
		}
		facts, err := n.factsRepo.ListByCanonicalPath(ctx, tenantID, cp, "event", 50)
		if err != nil || len(facts) == 0 {
			continue
		}
		titles := make([]string, 0, len(facts))
		for _, fact := range facts {
			t := strings.TrimSpace(fact.Title)
			if t != "" {
				titles = append(titles, t)
			}
		}
		if len(titles) > 0 {
			out[cp] = titles
		}
	}
	return out
}

func (n *NewsByFragmentsCanvas) buildGeminiPrompt(fragments []FragmentInput, daysBack int, refDateStr string, existingByPath map[string][]string) string {
	var fragLines []string
	for _, f := range fragments {
		cp := strings.TrimSpace(f.CanonicalPath)
		name := strings.TrimSpace(f.CanonicalName)
		if cp == "" {
			continue
		}
		if name == "" {
			name = cp
		}
		fragLines = append(fragLines, fmt.Sprintf("  - canonicalPath: %q, canonicalName: %q", cp, name))
	}
	fragmentsJSON, _ := json.MarshalIndent(fragments, "  ", "  ")

	excludeBlock := ""
	if len(existingByPath) > 0 {
		var lines []string
		for cp, titles := range existingByPath {
			if len(titles) == 0 {
				continue
			}
			lines = append(lines, fmt.Sprintf("  %s:", cp))
			for _, t := range titles {
				lines = append(lines, fmt.Sprintf("    - %q", t))
			}
		}
		if len(lines) > 0 {
			excludeBlock = "\n\nEXCLUDE (already in our system — do NOT return these or very similar news):\n" + strings.Join(lines, "\n") + "\n"
		}
	}

	return fmt.Sprintf(`You have access to Google Search. Today's date is %s.

TASK: For each of the following fragments (entities/locations), search for and return 2-5 most recent news items (from the last %d days). Use search to find real, current news.%s

FRAGMENTS:
%s

Return a STRICT JSON object matching this schema. Do NOT include markdown or explanations. Output raw JSON only.

{
  "fragments": [
    {
      "canonicalPath": "string (exact path from input)",
      "canonicalName": "string (human-readable name)",
      "news": [
        {
          "title": "string (news headline)",
          "summary": "string (1-2 sentences)",
          "occurredAt": "YYYY-MM-DD or RFC3339",
          "sourceUrl": "string (URL of the source)",
          "location": "string (optional, place name)",
          "countryCode": "string (optional, ISO3166-1 alpha2 lowercase)",
          "spherePaths": ["string (1-3 domains: geopolitics, logistics, policy, finance, cyber, social, energy, legal, compliance, environment, etc.)"],
          "polarity": "risk | opportunity | neutral",
          "impactLevel": "low | medium | high | critical",
          "eventType": "string (incident, regulatory_change, market_move, conflict, protest, acquisition, partnership, etc.)"
        }
      ]
    }
  ]
}

Rules:
- Return 2-5 news items per fragment. If no recent news found, return empty "news" array.
- Do NOT return news that duplicates or is very similar to items in EXCLUDE list above.
- occurredAt must be within the last %d days from %s.
- Use real sources from your search. Include sourceUrl when available.
- countryCode: ISO3166-1 alpha2 lowercase (e.g. us, cn, de) when the news is location-specific.
- spherePaths: 1-3 domains the news affects (geopolitics, logistics, policy, finance, cyber, social, energy, legal, compliance, environment). Use lowercase slugs.
- polarity: risk (threat/negative), opportunity (positive), or neutral.
- impactLevel: business impact severity. critical = major disruption; high = significant; medium = notable; low = minor.
- eventType: specific event category from the news content.`,
		refDateStr, daysBack, excludeBlock, string(fragmentsJSON), daysBack, refDateStr)
}

func (n *NewsByFragmentsCanvas) publishResult(ctx context.Context, tenantID string, r *NewsByFragmentsResult) {
	if n.publisher != nil {
		_ = n.publisher(ctx, "horizon.canvas.newsByFragments.completed", tenantID, r)
	}
}

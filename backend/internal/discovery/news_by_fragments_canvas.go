package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	defaultNewsDaysBack          = 7
	maxNewsDaysBack              = 30
	maxFragmentsPerNewsRequest   = 64
	newsLLMBatchSize             = 8
	maxNewsItemsPerFragment      = 3
	maxNewsItemsPerL1Category    = 1
	defaultNewsMaxOutputTokens   = 16384
	maxExistingEventFactsPerPath = 50
)

var newsRiskL1Categories = []string{
	"geopolitics",
	"policy",
	"finance",
	"logistics",
}

var newsRiskL1CategorySet = func() map[string]struct{} {
	out := make(map[string]struct{}, len(newsRiskL1Categories))
	for _, c := range newsRiskL1Categories {
		out[c] = struct{}{}
	}
	return out
}()

var newsRiskL1CategoryAliases = map[string]string{
	"geopolitical":        "geopolitics",
	"economic":            "economy",
	"macro":               "economy",
	"financial":           "finance",
	"cybersecurity":       "cyber",
	"cyber-security":      "cyber",
	"supply-chain":        "logistics",
	"supply-chain-risk":   "logistics",
	"shipping":            "logistics",
	"regulatory":          "policy",
	"regulation":          "policy",
	"public-policy":       "policy",
	"sanctions":           "compliance",
	"sanctions-screening": "compliance",
	"aml":                 "compliance",
	"kyc":                 "compliance",
}

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
	exec            *ai.AITaskExecutor
	factsRepo       *repo.OntologyFactsRepository
	dataSourcesRepo *repo.DataSourcesRepository
	signalPipeline  EventSignalProcessor
	fragmentLookup  FragmentLookup
	publisher       func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	logger          *log.Logger
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
	FactID           string     `json:"factId"`
	Title            string     `json:"title"`
	Summary          string     `json:"summary,omitempty"`
	OccurredAt       *time.Time `json:"occurredAt,omitempty"`
	SourceURL        string     `json:"sourceUrl,omitempty"`
	SourceL1Category string     `json:"sourceL1Category,omitempty"`
	CanonicalPath    string     `json:"canonicalPath"`
	CanonicalNames   []string   `json:"canonicalNames,omitempty"`
	// Business semantics from LLM
	SpherePaths []string `json:"spherePaths,omitempty"` // e.g. /sphere/geopolitics, /sphere/logistics
	Polarity    string   `json:"polarity,omitempty"`    // risk | opportunity | neutral
	ImpactLevel string   `json:"impactLevel,omitempty"` // low | medium | high | critical
	EventType   string   `json:"eventType,omitempty"`   // incident, regulatory_change, market_move, etc.
}

// NewsByFragmentsResult is the structured result sent via WS.
type NewsByFragmentsResult struct {
	JobID            string                      `json:"jobId"`
	TenantID         string                      `json:"tenantId"`
	NewsByPath       map[string][]NewsByPathItem `json:"newsByPath"`
	EventsCreated    int                         `json:"eventsCreated"`
	SignalsProcessed int                         `json:"signalsProcessed"`
}

// geminiNewsOutput is the JSON schema we ask Gemini to return.
type geminiNewsOutput struct {
	Fragments []struct {
		CanonicalPath string `json:"canonicalPath"`
		CanonicalName string `json:"canonicalName"`
		News          []struct {
			Title            string  `json:"title"`
			Summary          string  `json:"summary,omitempty"`
			OccurredAt       string  `json:"occurredAt,omitempty"` // ISO date
			SourceURL        string  `json:"sourceUrl,omitempty"`
			SourceL1Category string  `json:"sourceL1Category,omitempty"`
			Location         *string `json:"location,omitempty"`
			CountryCode      string  `json:"countryCode,omitempty"`
			// Business semantics (LLM-inferred from content)
			SpherePaths []string `json:"spherePaths,omitempty"` // e.g. geopolitics, logistics, policy
			Polarity    string   `json:"polarity,omitempty"`    // risk | opportunity | neutral
			ImpactLevel string   `json:"impactLevel,omitempty"` // low | medium | high | critical
			EventType   string   `json:"eventType,omitempty"`   // incident, regulatory_change, market_move, etc.
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
		daysBack = defaultNewsDaysBack
	}
	if daysBack > maxNewsDaysBack {
		daysBack = maxNewsDaysBack
	}
	if jobID == "" {
		jobID = fmt.Sprintf("news_by_fragments_%d", time.Now().Unix())
	}
	if n.exec == nil {
		return nil, fmt.Errorf("news-by-fragments: AI executor is not configured")
	}
	if n.factsRepo == nil {
		return nil, fmt.Errorf("news-by-fragments: ontology facts repository is not configured")
	}

	preparedFragments, err := n.prepareInputFragments(ctx, fragments)
	if err != nil {
		return nil, err
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
	newsByPath := make(map[string][]NewsByPathItem)
	allowedPaths := make(map[string]struct{}, len(preparedFragments))
	nameByPath := make(map[string]string, len(preparedFragments))
	for _, f := range preparedFragments {
		cp := normalizeCanonicalPath(f.CanonicalPath)
		if cp != "" {
			allowedPaths[cp] = struct{}{}
			nameByPath[cp] = strings.TrimSpace(f.CanonicalName)
			newsByPath[cp] = []NewsByPathItem{}
		}
	}

	existingIndex := n.fetchExistingEventIndex(ctx, tenantID, preparedFragments)
	seenKeysByPath := copyPathDedupKeys(existingIndex.keysByPath, preparedFragments)

	type createdEvent struct {
		factID string
		fact   *models.OntologyFact
	}
	createdEvents := make([]createdEvent, 0, len(preparedFragments)*2)

	for start := 0; start < len(preparedFragments); start += newsLLMBatchSize {
		end := start + newsLLMBatchSize
		if end > len(preparedFragments) {
			end = len(preparedFragments)
		}
		batch := preparedFragments[start:end]
		batchTitles, batchURLs := existingIndex.forBatch(batch)

		out, batchErr := n.runGroundedNewsBatch(ctx, tenantID, batch, daysBack, refDateStr, batchTitles, batchURLs)
		if batchErr != nil {
			return nil, batchErr
		}

		for _, frag := range out.Fragments {
			cp := normalizeCanonicalPath(frag.CanonicalPath)
			if cp == "" {
				continue
			}
			if _, ok := allowedPaths[cp]; !ok {
				if n.logger != nil {
					n.logger.Printf("news_by_fragments: skip model output with non-requested canonicalPath=%q", cp)
				}
				continue
			}

			canonicalNames := mergeCanonicalNames(nameByPath[cp], frag.CanonicalName, cp)
			accepted := 0
			acceptedByCategory := map[string]int{}

			for _, news := range frag.News {
				if accepted >= maxNewsItemsPerFragment {
					break
				}

				title := strings.TrimSpace(news.Title)
				if title == "" {
					continue
				}
				sourceURL := normalizeSourceURL(news.SourceURL)
				occurredAt := parseOccurredAt(news.OccurredAt)
				if !isOccurredAtInWindow(occurredAt, refDate, daysBack) {
					continue
				}
				sourceL1Category := normalizeNewsSourceL1Category(news.SourceL1Category)
				if sourceL1Category == "" {
					sourceL1Category = inferNewsSourceL1FromSpherePaths(news.SpherePaths)
				}
				if sourceL1Category == "" {
					continue
				}
				if acceptedByCategory[sourceL1Category] >= maxNewsItemsPerL1Category {
					continue
				}

				dedupKeys := composeNewsDedupKeys(title, sourceURL, occurredAt)
				if containsAnyDedupKey(seenKeysByPath[cp], dedupKeys) {
					continue
				}

				now := time.Now().UTC()
				sourceIDs := []string{}
				if quasiSourceID != "" {
					sourceIDs = []string{quasiSourceID}
				} else {
					sourceIDs = []string{"path:" + cp}
				}
				sourceRefs := []models.SourceRef{}
				if sourceURL != "" {
					sourceRefs = append(sourceRefs, models.SourceRef{SourceID: sourceURL, SourceType: models.SourceTypeWeb})
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
				tags = append(tags, "source-l1:"+sourceL1Category)

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
					Summary:        strings.TrimSpace(news.Summary),
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
						n.logger.Printf("news_by_fragments: Create fact failed path=%s title=%q err=%v", cp, title, createErr)
					}
					continue
				}

				appendDedupKeys(seenKeysByPath[cp], dedupKeys)
				createdEvents = append(createdEvents, createdEvent{factID: factID, fact: fact})
				accepted++
				acceptedByCategory[sourceL1Category]++

				item := NewsByPathItem{
					FactID:           factID,
					Title:            title,
					Summary:          strings.TrimSpace(news.Summary),
					OccurredAt:       occurredAt,
					SourceURL:        sourceURL,
					SourceL1Category: sourceL1Category,
					CanonicalPath:    cp,
					CanonicalNames:   canonicalNames,
					SpherePaths:      spherePaths,
					Polarity:         polarity,
					ImpactLevel:      impactLevel,
					EventType:        eventType,
				}
				newsByPath[cp] = append(newsByPath[cp], item)
			}
		}
	}

	signalsProcessed := 0
	if n.signalPipeline != nil {
		for _, ev := range createdEvents {
			if err := n.signalPipeline.ProcessEvent(ctx, ev.fact); err != nil {
				if n.logger != nil {
					n.logger.Printf("news_by_fragments: ProcessEvent failed factId=%s: %v", ev.factID, err)
				}
				continue
			}
			signalsProcessed++
		}
	}
	result := &NewsByFragmentsResult{
		JobID:            jobID,
		TenantID:         tenantID,
		NewsByPath:       newsByPath,
		EventsCreated:    len(createdEvents),
		SignalsProcessed: signalsProcessed,
	}

	n.publishResult(ctx, tenantID, result)

	if n.logger != nil {
		n.logger.Printf("news_by_fragments: jobId=%s events=%d paths=%d",
			jobID, len(createdEvents), len(newsByPath))
	}

	return result, nil
}

// fetchExistingEventIndex returns event titles/URLs (for prompt exclusions) and dedup keys by canonical path.
func (n *NewsByFragmentsCanvas) fetchExistingEventIndex(ctx context.Context, tenantID string, fragments []FragmentInput) existingEventIndex {
	idx := existingEventIndex{
		titlesByPath: make(map[string][]string, len(fragments)),
		urlsByPath:   make(map[string][]string, len(fragments)),
		keysByPath:   make(map[string]map[string]struct{}, len(fragments)),
	}
	if n.factsRepo == nil {
		return idx
	}
	for _, f := range fragments {
		cp := normalizeCanonicalPath(f.CanonicalPath)
		if cp == "" {
			continue
		}
		facts, err := n.factsRepo.ListByCanonicalPath(ctx, tenantID, cp, "event", maxExistingEventFactsPerPath)
		if err != nil || len(facts) == 0 {
			continue
		}
		titleSet := map[string]struct{}{}
		urlSet := map[string]struct{}{}
		if _, ok := idx.keysByPath[cp]; !ok {
			idx.keysByPath[cp] = map[string]struct{}{}
		}
		for _, fact := range facts {
			t := strings.TrimSpace(fact.Title)
			if t != "" {
				titleSet[t] = struct{}{}
			}
			for _, srcURL := range extractWebSourceURLs(fact) {
				urlSet[srcURL] = struct{}{}
			}
			for _, key := range composeNewsDedupKeys(t, "", fact.OccurredAt) {
				idx.keysByPath[cp][key] = struct{}{}
			}
			for _, srcURL := range extractWebSourceURLs(fact) {
				for _, key := range composeNewsDedupKeys(t, srcURL, fact.OccurredAt) {
					idx.keysByPath[cp][key] = struct{}{}
				}
			}
		}
		if len(titleSet) > 0 {
			idx.titlesByPath[cp] = sortedSetValues(titleSet)
		}
		if len(urlSet) > 0 {
			idx.urlsByPath[cp] = sortedSetValues(urlSet)
		}
	}
	return idx
}

func (n *NewsByFragmentsCanvas) buildGeminiPrompt(
	fragments []FragmentInput,
	daysBack int,
	refDateStr string,
	existingTitlesByPath map[string][]string,
	existingURLsByPath map[string][]string,
) string {
	var fragLines []string
	for _, f := range fragments {
		cp := normalizeCanonicalPath(f.CanonicalPath)
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
	categoriesJSON, _ := json.Marshal(newsRiskL1Categories)
	categoriesList := strings.Join(newsRiskL1Categories, ", ")

	excludeBlock := ""
	if len(existingTitlesByPath) > 0 || len(existingURLsByPath) > 0 {
		var lines []string
		allPaths := make(map[string]struct{}, len(existingTitlesByPath)+len(existingURLsByPath))
		for cp := range existingTitlesByPath {
			allPaths[cp] = struct{}{}
		}
		for cp := range existingURLsByPath {
			allPaths[cp] = struct{}{}
		}
		paths := make([]string, 0, len(allPaths))
		for cp := range allPaths {
			paths = append(paths, cp)
		}
		sort.Strings(paths)
		for _, cp := range paths {
			lines = append(lines, fmt.Sprintf("  %s:", cp))
			if titles := existingTitlesByPath[cp]; len(titles) > 0 {
				lines = append(lines, "    titles:")
				for _, t := range titles {
					lines = append(lines, fmt.Sprintf("      - %q", t))
				}
			}
			if urls := existingURLsByPath[cp]; len(urls) > 0 {
				lines = append(lines, "    urls:")
				for _, u := range urls {
					lines = append(lines, fmt.Sprintf("      - %q", u))
				}
			}
		}
		if len(lines) > 0 {
			excludeBlock = "\n\nEXCLUDE (already in our system — do NOT return these or very similar news):\n" + strings.Join(lines, "\n") + "\n"
		}
	}

	return fmt.Sprintf(`You have access to Google Search. Today's date is %s.

TASK: For each of the following fragments (entities/locations), search for and return 1-3 most recent news items (from the last %d days). Use search to find real, current news in a SINGLE search pass across these L1 risk categories: %s.%s

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
          "sourceL1Category": "string (required L1 category, one of %s)",
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
- Return 1-3 news items per fragment. If no recent news found, return empty "news" array.
- Do NOT return news that duplicates or is very similar to items in EXCLUDE list above.
- occurredAt must be within the last %d days from %s.
- Use real sources from your search. Include sourceUrl when available.
- sourceL1Category is REQUIRED and must be one of: %s
- Ensure category diversity: no more than 1 item per sourceL1Category per fragment. Prefer highest-impact and most recent items.
- countryCode: ISO3166-1 alpha2 lowercase (e.g. us, cn, de) when the news is location-specific.
- spherePaths: 1-3 domains the news affects (geopolitics, logistics, policy, finance, cyber, social, energy, legal, compliance, environment). Use lowercase slugs.
- polarity: risk (threat/negative), opportunity (positive), or neutral.
- impactLevel: business impact severity. critical = major disruption; high = significant; medium = notable; low = minor.
- eventType: specific event category from the news content.`,
		refDateStr, daysBack, categoriesList, excludeBlock, string(fragmentsJSON), string(categoriesJSON), daysBack, refDateStr, categoriesList)
}

func (n *NewsByFragmentsCanvas) publishResult(ctx context.Context, tenantID string, r *NewsByFragmentsResult) {
	if n.publisher != nil {
		_ = n.publisher(ctx, "horizon.canvas.newsByFragments.completed", tenantID, r)
	}
}

type existingEventIndex struct {
	titlesByPath map[string][]string
	urlsByPath   map[string][]string
	keysByPath   map[string]map[string]struct{}
}

func (idx existingEventIndex) forBatch(fragments []FragmentInput) (map[string][]string, map[string][]string) {
	titles := make(map[string][]string, len(fragments))
	urls := make(map[string][]string, len(fragments))
	for _, f := range fragments {
		cp := normalizeCanonicalPath(f.CanonicalPath)
		if cp == "" {
			continue
		}
		if vals, ok := idx.titlesByPath[cp]; ok && len(vals) > 0 {
			titles[cp] = vals
		}
		if vals, ok := idx.urlsByPath[cp]; ok && len(vals) > 0 {
			urls[cp] = vals
		}
	}
	return titles, urls
}

func (n *NewsByFragmentsCanvas) prepareInputFragments(ctx context.Context, fragments []FragmentInput) ([]FragmentInput, error) {
	deduped := make([]FragmentInput, 0, len(fragments))
	indexByPath := make(map[string]int, len(fragments))
	for _, f := range fragments {
		cp := normalizeCanonicalPath(f.CanonicalPath)
		if cp == "" || !strings.HasPrefix(cp, "/") {
			continue
		}
		name := strings.TrimSpace(f.CanonicalName)
		if idx, ok := indexByPath[cp]; ok {
			if strings.TrimSpace(deduped[idx].CanonicalName) == "" && name != "" {
				deduped[idx].CanonicalName = name
			}
			continue
		}
		indexByPath[cp] = len(deduped)
		deduped = append(deduped, FragmentInput{
			CanonicalPath: cp,
			CanonicalName: name,
		})
	}
	if len(deduped) == 0 {
		return nil, fmt.Errorf("news-by-fragments: no valid canonical paths in request")
	}
	if len(deduped) > maxFragmentsPerNewsRequest {
		return nil, fmt.Errorf("news-by-fragments: too many fragments (%d > %d)", len(deduped), maxFragmentsPerNewsRequest)
	}
	for i := range deduped {
		f := &deduped[i]
		if f.CanonicalName == "" && n.fragmentLookup != nil {
			if info, err := n.fragmentLookup.GetByCanonicalPath(ctx, f.CanonicalPath); err == nil && info != nil && strings.TrimSpace(info.Label) != "" {
				f.CanonicalName = strings.TrimSpace(info.Label)
			}
		}
		if strings.TrimSpace(f.CanonicalName) == "" {
			f.CanonicalName = f.CanonicalPath
		}
	}
	return deduped, nil
}

func (n *NewsByFragmentsCanvas) runGroundedNewsBatch(
	ctx context.Context,
	tenantID string,
	fragments []FragmentInput,
	daysBack int,
	refDateStr string,
	existingTitlesByPath map[string][]string,
	existingURLsByPath map[string][]string,
) (*geminiNewsOutput, error) {
	prompt := n.buildGeminiPrompt(fragments, daysBack, refDateStr, existingTitlesByPath, existingURLsByPath)
	verifier := ai.NewGenericVerifier(func(x geminiNewsOutput) error { return nil })
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		TenantID:     tenantID,
		UserID:       "system",
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
			MinCapabilityScore:        6,
			MaxOutputSize:             defaultNewsMaxOutputTokens,
		},
		Verifier: verifier,
	}
	parsed, _, err := n.exec.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("gemini news search (batch=%d): %w", len(fragments), err)
	}
	out, ok := parsed.(geminiNewsOutput)
	if !ok {
		return nil, fmt.Errorf("gemini news search: unexpected response type %T", parsed)
	}
	return &out, nil
}

func mergeCanonicalNames(preferredName, llmName, canonicalPath string) []string {
	set := map[string]struct{}{}
	out := make([]string, 0, 2)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		key := strings.ToLower(v)
		if _, exists := set[key]; exists {
			return
		}
		set[key] = struct{}{}
		out = append(out, v)
	}
	add(preferredName)
	add(llmName)
	add(canonicalPath)
	if len(out) == 0 {
		return []string{canonicalPath}
	}
	return out
}

func parseOccurredAt(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			ut := t.UTC()
			return &ut
		}
	}
	return nil
}

func isOccurredAtInWindow(occurredAt *time.Time, refDate time.Time, daysBack int) bool {
	if occurredAt == nil {
		return true
	}
	start := refDate.AddDate(0, 0, -daysBack)
	upper := refDate.Add(24 * time.Hour)
	return !occurredAt.Before(start) && !occurredAt.After(upper)
}

func normalizeSourceURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Fragment = ""
	parsed.RawQuery = ""
	parsed.Host = strings.ToLower(parsed.Host)
	return strings.TrimSpace(parsed.String())
}

func composeNewsDedupKeys(title, sourceURL string, occurredAt *time.Time) []string {
	keys := make([]string, 0, 3)
	titleNorm := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(title))), " ")
	datePart := ""
	if occurredAt != nil {
		datePart = occurredAt.UTC().Format("2006-01-02")
	}
	if sourceURL = normalizeSourceURL(sourceURL); sourceURL != "" {
		keys = append(keys, "u:"+sourceURL)
	}
	if titleNorm != "" {
		keys = append(keys, "t:"+titleNorm)
		if datePart != "" {
			keys = append(keys, "td:"+titleNorm+"|"+datePart)
		}
	}
	return keys
}

func containsAnyDedupKey(existing map[string]struct{}, keys []string) bool {
	if len(keys) == 0 || len(existing) == 0 {
		return false
	}
	for _, k := range keys {
		if _, ok := existing[k]; ok {
			return true
		}
	}
	return false
}

func appendDedupKeys(dst map[string]struct{}, keys []string) {
	if dst == nil {
		return
	}
	for _, k := range keys {
		dst[k] = struct{}{}
	}
}

func copyPathDedupKeys(src map[string]map[string]struct{}, fragments []FragmentInput) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{}, len(fragments))
	for _, f := range fragments {
		cp := normalizeCanonicalPath(f.CanonicalPath)
		if cp == "" {
			continue
		}
		out[cp] = map[string]struct{}{}
		for k := range src[cp] {
			out[cp][k] = struct{}{}
		}
	}
	return out
}

func extractWebSourceURLs(fact *models.OntologyFact) []string {
	if fact == nil || len(fact.SourceRefs) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	for _, ref := range fact.SourceRefs {
		if ref.SourceType != models.SourceTypeWeb {
			continue
		}
		u := normalizeSourceURL(ref.SourceID)
		if u == "" {
			continue
		}
		set[u] = struct{}{}
	}
	return sortedSetValues(set)
}

func sortedSetValues(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func normalizeNewsSourceL1Category(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "_", "-")
	raw = strings.ReplaceAll(raw, " ", "-")

	parts := strings.Split(strings.Trim(raw, "/"), "/")
	switch {
	case len(parts) >= 2 && (parts[0] == "source" || parts[0] == "sphere"):
		raw = parts[1]
	case len(parts) > 1:
		raw = parts[0]
	case len(parts) == 1:
		raw = parts[0]
	}

	if alias, ok := newsRiskL1CategoryAliases[raw]; ok {
		raw = alias
	}
	if _, ok := newsRiskL1CategorySet[raw]; ok {
		return raw
	}
	return ""
}

func inferNewsSourceL1FromSpherePaths(spherePaths []string) string {
	for _, p := range spherePaths {
		if c := normalizeNewsSourceL1Category(p); c != "" {
			return c
		}
	}
	return ""
}

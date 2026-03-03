package qa

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	scoutpipeline "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// WebCitation is a simplified citation from a web source (URL + title).
type WebCitation struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// FilledGap records a successfully grounded signal gap.
type FilledGap struct {
	Gap          SignalGap     `json:"gap"`
	InjectedAxis string        `json:"injectedAxis"`
	Amplitude    float64       `json:"amplitude"`
	Confidence   float64       `json:"confidence"`
	Sources      []WebCitation `json:"sources"`
	DocumentID   string        `json:"documentId"`
	FactIDs      []string      `json:"factIds"`
}

// FailedGap records a gap that could not be filled.
type FailedGap struct {
	Gap    SignalGap `json:"gap"`
	Reason string    `json:"reason"`
}

// FillReport summarises a best-effort grounding run.
type FillReport struct {
	FilledGaps     []FilledGap   `json:"filledGaps"`
	FailedGaps     []FailedGap   `json:"failedGaps"`
	TotalQueries   int           `json:"totalQueries"`
	TotalSignals   int           `json:"totalSignals"`
	TotalCostUSD   float64       `json:"totalCostUsd"`
	CreatedDocIDs  []string      `json:"createdDocIds"`
	CreatedFactIDs []string      `json:"createdFactIds"`
	Sources        []WebCitation `json:"sources"`
}

// FragmentsRepoReader reads fragments by ID (subset of full repo).
type FragmentsRepoReader interface {
	Get(ctx context.Context, id string) (*horizonmodels.HorizonFragment, error)
}

// BestEffortFiller orchestrates grounding through the normal document pipeline.
// For each signal gap it runs a search-grounded AI call, persists results as
// documents + ontology facts, and feeds them through the signal pipeline.
// Failures are logged but never block the caller.
type BestEffortFiller struct {
	aiExecutor     *ai.AITaskExecutor
	documentsRepo  *corerepo.DocumentsRepository
	factsRepo      *corerepo.OntologyFactsRepository
	signalPipeline *scoutpipeline.SignalPipeline
	fragmentsRepo  FragmentsRepoReader
}

// NewBestEffortFiller creates a new BestEffortFiller.
func NewBestEffortFiller(
	aiExecutor *ai.AITaskExecutor,
	documentsRepo *corerepo.DocumentsRepository,
	factsRepo *corerepo.OntologyFactsRepository,
	signalPipeline *scoutpipeline.SignalPipeline,
	fragmentsRepo FragmentsRepoReader,
) *BestEffortFiller {
	return &BestEffortFiller{
		aiExecutor:     aiExecutor,
		documentsRepo:  documentsRepo,
		factsRepo:      factsRepo,
		signalPipeline: signalPipeline,
		fragmentsRepo:  fragmentsRepo,
	}
}

const fillConcurrency = 3

// Fill executes grounding queries and persists results through the normal document pipeline.
// Best-effort: individual query failures are recorded in FailedGaps, never propagated.
func (f *BestEffortFiller) Fill(
	ctx context.Context,
	gaps []SignalGap,
	queries []GroundingQuery,
	lensID string,
) (*FillReport, error) {
	report := &FillReport{
		TotalQueries: len(queries),
		TotalSignals: len(gaps),
	}
	log.Printf("[BestEffortFiller][lens=%s] start gaps=%d queries=%d ai=%t docsRepo=%t factsRepo=%t signalPipeline=%t",
		lensID,
		len(gaps),
		len(queries),
		f.aiExecutor != nil,
		f.documentsRepo != nil,
		f.factsRepo != nil,
		f.signalPipeline != nil,
	)

	if f.aiExecutor == nil {
		for _, gap := range gaps {
			report.FailedGaps = append(report.FailedGaps, FailedGap{
				Gap:    gap,
				Reason: "ai_executor_not_configured",
			})
		}
		log.Printf("[BestEffortFiller][lens=%s] skipped reason=ai_executor_not_configured failed=%d", lensID, len(report.FailedGaps))
		return report, nil
	}

	if len(queries) == 0 {
		log.Printf("[BestEffortFiller][lens=%s] no-queries", lensID)
		return report, nil
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, fillConcurrency)
		results []fillQueryResult
	)

	for _, q := range queries {
		wg.Add(1)
		go func(query GroundingQuery) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			qr := f.processQuery(ctx, query, lensID)
			mu.Lock()
			results = append(results, qr)
			mu.Unlock()
		}(q)
	}
	wg.Wait()

	allSources := make(map[string]WebCitation)
	for _, qr := range results {
		report.FilledGaps = append(report.FilledGaps, qr.filled...)
		report.FailedGaps = append(report.FailedGaps, qr.failed...)
		report.TotalCostUSD += qr.cost
		report.CreatedDocIDs = append(report.CreatedDocIDs, qr.docIDs...)
		report.CreatedFactIDs = append(report.CreatedFactIDs, qr.factIDs...)
		for _, src := range qr.sources {
			if src.URL != "" {
				allSources[src.URL] = src
			}
		}
	}
	for _, src := range allSources {
		report.Sources = append(report.Sources, src)
	}
	log.Printf("[BestEffortFiller][lens=%s] done filled=%d failed=%d docs=%d facts=%d sources=%d totalCostUsd=%.4f",
		lensID,
		len(report.FilledGaps),
		len(report.FailedGaps),
		len(report.CreatedDocIDs),
		len(report.CreatedFactIDs),
		len(report.Sources),
		report.TotalCostUSD,
	)

	return report, nil
}

func (f *BestEffortFiller) processQuery(ctx context.Context, query GroundingQuery, lensID string) fillQueryResult {
	var qr fillQueryResult

	entityName := ""
	if len(query.SourceGaps) > 0 {
		entityName = query.SourceGaps[0].EntityName
	}
	axisDescriptions := strings.Join(query.TargetAxes, ", ")
	log.Printf("[BestEffortFiller][lens=%s] query-start entity=%q axes=%d sourceGaps=%d query=%q",
		lensID,
		entityName,
		len(query.TargetAxes),
		len(query.SourceGaps),
		query.Query,
	)

	prompt := fmt.Sprintf(
		"Given real-world information about the following entity and topics, extract factual events.\n\n"+
			"Entity: %s\nTopics: %s\n\n"+
			"For each event return: title, description, date, relevance.",
		entityName, axisDescriptions,
	)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeSearch,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
			PreferredModel:            "gemini-2.5-flash",
			MaxOutputSize:             4096,
		},
		UseCase: "horizon.qa.grounding",
	}

	result, err := f.aiExecutor.ExecuteTask(ctx, task)
	if err != nil {
		log.Printf("[BestEffortFiller] AI call failed for query %q: %v", query.Query, err)
		for _, gap := range query.SourceGaps {
			qr.failed = append(qr.failed, FailedGap{Gap: gap, Reason: fmt.Sprintf("ai_call_failed: %v", err)})
		}
		return qr
	}
	if result.Status != ai.AITaskStatusCompleted || strings.TrimSpace(result.Content) == "" {
		reason := "empty_response"
		if result.Status == ai.AITaskStatusFailed {
			reason = "ai_task_failed"
		}
		for _, gap := range query.SourceGaps {
			qr.failed = append(qr.failed, FailedGap{Gap: gap, Reason: reason})
		}
		return qr
	}

	qr.cost = result.ProcessingCost

	citations := f.extractCitations(result)
	qr.sources = citations

	docID, err := f.persistDocument(ctx, lensID, query, result.Content, citations)
	if err != nil {
		log.Printf("[BestEffortFiller] document persist failed for query %q: %v", query.Query, err)
	} else if docID != "" {
		qr.docIDs = append(qr.docIDs, docID)
	}

	for _, gap := range query.SourceGaps {
		factID, fact, factErr := f.persistFact(ctx, lensID, gap, result.Content, docID)
		if factErr != nil {
			log.Printf("[BestEffortFiller] fact persist failed for gap %s/%s: %v", gap.CanonicalPath, gap.Axis, factErr)
			qr.failed = append(qr.failed, FailedGap{Gap: gap, Reason: fmt.Sprintf("fact_persist_failed: %v", factErr)})
			continue
		}

		if factID != "" {
			qr.factIDs = append(qr.factIDs, factID)

			if fact != nil && f.signalPipeline != nil {
				if pErr := f.signalPipeline.ProcessEvent(ctx, fact); pErr != nil {
					log.Printf("[BestEffortFiller] signal pipeline failed for fact %s: %v", factID, pErr)
				}
			}
		}

		qr.filled = append(qr.filled, FilledGap{
			Gap:          gap,
			InjectedAxis: gap.Axis,
			Amplitude:    0.5,
			Confidence:   0.6,
			Sources:      citations,
			DocumentID:   docID,
			FactIDs:      []string{factID},
		})
	}
	log.Printf("[BestEffortFiller][lens=%s] query-done entity=%q filled=%d failed=%d citations=%d doc=%t facts=%d costUsd=%.4f",
		lensID,
		entityName,
		len(qr.filled),
		len(qr.failed),
		len(qr.sources),
		docID != "",
		len(qr.factIDs),
		qr.cost,
	)

	return qr
}

func (f *BestEffortFiller) extractCitations(result *ai.AITaskResult) []WebCitation {
	var out []WebCitation
	seen := make(map[string]struct{})

	for _, c := range result.Citations {
		if c.URL == "" {
			continue
		}
		if _, ok := seen[c.URL]; ok {
			continue
		}
		seen[c.URL] = struct{}{}
		out = append(out, WebCitation{URL: c.URL, Title: c.Title})
	}

	if result.GroundingEvidence != nil {
		for _, ws := range result.GroundingEvidence.WebSources {
			if ws.URL == "" {
				continue
			}
			if _, ok := seen[ws.URL]; ok {
				continue
			}
			seen[ws.URL] = struct{}{}
			out = append(out, WebCitation{URL: ws.URL, Title: ws.Title})
		}
	}

	return out
}

func (f *BestEffortFiller) persistDocument(ctx context.Context, lensID string, query GroundingQuery, content string, citations []WebCitation) (string, error) {
	if f.documentsRepo == nil {
		return "", nil
	}
	now := time.Now().UTC()
	var url, title string
	if len(citations) > 0 {
		url = citations[0].URL
		title = citations[0].Title
	}
	if title == "" {
		title = "QA signal fill: " + query.Query
	}
	doc := &coremodels.Document{
		SourceID:     "qa_signal_fill",
		Title:        title,
		URL:          url,
		FullText:     content,
		Status:       "processed",
		ContentType:  "plainText",
		DocumentType: "article",
		IngestedAt:   now,
		CreatedAt:    now,
		UpdatedAt:    now,
		SourceMeta: map[string]any{
			"lensId":      lensID,
			"query":       query.Query,
			"fragmentIds": query.TargetFragmentIDs,
			"axes":        query.TargetAxes,
			"origin":      "best_effort_filler",
		},
	}
	return f.documentsRepo.Create(ctx, doc)
}

func (f *BestEffortFiller) persistFact(ctx context.Context, lensID string, gap SignalGap, content string, docID string) (string, *coremodels.OntologyFact, error) {
	if f.factsRepo == nil {
		return "", nil, nil
	}
	now := time.Now().UTC()
	fact := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			CreatedAt:      now,
			UpdatedAt:      now,
			CreatedBy:      "qa_signal_fill",
			Status:         "active",
			CanonicalPaths: []string{gap.CanonicalPath},
		},
		Kind:       "event",
		Type:       "signal_fill_" + strings.ReplaceAll(gap.Axis, "/", "_"),
		Title:      fmt.Sprintf("Signal fill for %s on %s", gap.EntityName, gap.Axis),
		Summary:    content,
		Confidence: 0.6,
		ObservedAt: &now,
	}
	if docID != "" {
		fact.SourceRefs = []coremodels.SourceRef{{
			SourceType: "document",
			SourceID:   docID,
		}}
	}
	id, err := f.factsRepo.Create(ctx, fact)
	return id, fact, err
}

// fillQueryResult bundles the outcome of a single GroundingQuery execution.
type fillQueryResult struct {
	filled  []FilledGap
	failed  []FailedGap
	cost    float64
	docIDs  []string
	factIDs []string
	sources []WebCitation
}

package discovery

import (
	"context"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery/extraction"
	"github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"golang.org/x/sync/errgroup"
)

// EventSignalProcessor processes ontology event facts through 12-axis extraction and propagation.
// Implemented by scout.SignalPipeline; interface avoids import cycle.
type EventSignalProcessor interface {
	ProcessEvent(ctx context.Context, event *models.OntologyFact) error
}

// CanvasIngester fetches from system sources, runs batch LLM extraction, persists ontology facts,
// and triggers 12-axis signal extraction + propagation.
type CanvasIngester struct {
	sources        *repo.DataSourcesRepository
	providersReg   *providers.Registry
	exec           *ai.AITaskExecutor
	ontologyExt    *ontology.OntologyExtractor
	factsRepo      *repo.OntologyFactsRepository
	signalPipeline EventSignalProcessor
	fetchHM        func(ctx context.Context, feedURL string) ([]providers.NormalizedDocument, error)
	logger         *log.Logger
}

// CanvasIngesterConfig holds dependencies for canvas ingest.
type CanvasIngesterConfig struct {
	Sources        *repo.DataSourcesRepository
	ProvidersReg   *providers.Registry
	Exec           *ai.AITaskExecutor
	OntologyExt    *ontology.OntologyExtractor
	FactsRepo      *repo.OntologyFactsRepository
	SignalPipeline EventSignalProcessor
	FetchHM        func(ctx context.Context, feedURL string) ([]providers.NormalizedDocument, error)
	Logger         *log.Logger
}

// NewCanvasIngester creates a canvas ingester.
func NewCanvasIngester(cfg CanvasIngesterConfig) *CanvasIngester {
	return &CanvasIngester{
		sources:        cfg.Sources,
		providersReg:   cfg.ProvidersReg,
		exec:           cfg.Exec,
		ontologyExt:    cfg.OntologyExt,
		factsRepo:      cfg.FactsRepo,
		signalPipeline: cfg.SignalPipeline,
		fetchHM:        cfg.FetchHM,
		logger:         cfg.Logger,
	}
}

// SystemCanvasKinds are source kinds included in the system canvas.
var SystemCanvasKinds = []string{
	models.SourceKindRSS,
	models.SourceKindHorizonFeed,
	models.SourceKindWorldMonitorLegacy,
}

// Ingest fetches from all enabled system sources, runs batch extraction, persists facts,
// and runs SignalPipeline for each created event fact.
func (c *CanvasIngester) Ingest(ctx context.Context, tenantID string) (docsFetched, eventsCreated, signalsProcessed int, err error) {
	if strings.TrimSpace(tenantID) == "" {
		tenantID = "global"
	}
	active := "active"
	sources, err := c.sources.ListByTenant(ctx, tenantID, SystemCanvasKinds, &active)
	if err != nil {
		return 0, 0, 0, err
	}
	eligible := make([]*models.DataSource, 0, len(sources))
	for _, ds := range sources {
		if !models.IsGlobalSystemSource(ds) {
			if c.logger != nil {
				c.logger.Printf("canvas: skip source=%s reason=not_global_system scope=%s", ds.ID, models.SourceScopeValue(ds))
			}
			continue
		}
		if !models.UsesCanvasLLMProcessing(ds) {
			if c.logger != nil {
				c.logger.Printf("canvas: skip source=%s reason=processing_mode_%s", ds.ID, models.SourceProcessingModeValue(ds))
			}
			continue
		}
		eligible = append(eligible, ds)
	}
	if len(eligible) == 0 {
		return 0, 0, 0, nil
	}

	// Fetch all sources in parallel (best-effort: collect what we get)
	type fetchResult struct {
		sourceID string
		docs     []providers.NormalizedDocument
		err      error
	}
	results := make([]fetchResult, len(eligible))
	g, gctx := errgroup.WithContext(ctx)
	for i, ds := range eligible {
		i, ds := i, ds
		g.Go(func() error {
			var docs []providers.NormalizedDocument
			var fetchErr error
			switch ds.Kind {
			case models.SourceKindRSS:
				feedURL := ""
				if ds.Details.RSSURL != nil {
					feedURL = *ds.Details.RSSURL
				}
				if feedURL == "" {
					feedURL = ds.ID
				}
				if p, ok := c.providersReg.Get("rss"); ok {
					docs, fetchErr = p.Ingest(gctx, tenantID, feedURL)
				}
			case models.SourceKindHorizonFeed, models.SourceKindWorldMonitorLegacy:
				feedURL := ""
				if ds.Details.URI != nil {
					feedURL = *ds.Details.URI
				} else if ds.Details.SourceURI != nil {
					feedURL = *ds.Details.SourceURI
				}
				if feedURL != "" && c.fetchHM != nil {
					docs, fetchErr = c.fetchHM(gctx, feedURL)
				}
			}
			results[i] = fetchResult{sourceID: ds.ID, docs: docs, err: fetchErr}
			return nil // never fail the group so we collect all results
		})
	}
	_ = g.Wait()

	// Build canvas documents
	var canvasDocs []extraction.CanvasDocument
	for _, r := range results {
		if r.err != nil && c.logger != nil {
			c.logger.Printf("canvas: fetch failed source=%s: %v", r.sourceID, r.err)
		}
		for _, nd := range r.docs {
			if strings.TrimSpace(nd.FullText) == "" {
				continue
			}
			canvasDocs = append(canvasDocs, extraction.CanvasDocument{
				ND:       &nd,
				SourceID: r.sourceID,
			})
		}
	}
	docsFetched = len(canvasDocs)
	if docsFetched == 0 {
		return docsFetched, 0, 0, nil
	}

	// Batch extract and persist
	_, factIDs, err := extraction.ExtractBatchAndPersist(ctx, c.exec, c.ontologyExt, tenantID, canvasDocs)
	if err != nil {
		return docsFetched, 0, 0, err
	}
	eventsCreated = len(factIDs)

	// Run SignalPipeline for each created event fact
	if c.signalPipeline != nil && c.factsRepo != nil {
		for _, factID := range factIDs {
			ev, getErr := c.factsRepo.Get(ctx, factID)
			if getErr != nil || ev == nil {
				continue
			}
			if ev.Kind != "event" {
				continue
			}
			if err := c.signalPipeline.ProcessEvent(ctx, ev); err != nil && c.logger != nil {
				c.logger.Printf("canvas: ProcessEvent failed factId=%s: %v", factID, err)
			} else {
				signalsProcessed++
			}
		}
	}

	if c.logger != nil {
		c.logger.Printf("canvas: ingested docs=%d events=%d signals=%d", docsFetched, eventsCreated, signalsProcessed)
	}
	return docsFetched, eventsCreated, signalsProcessed, nil
}

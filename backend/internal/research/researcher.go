package research

import (
	"context"
	"fmt"
	"log"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	g "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
)

// ProgressCallback is called to report research progress
type ProgressCallback func(stage string, data map[string]interface{})

// Researcher orchestrates the research process
type Researcher struct {
	query         string
	model         string
	tavilyClient  TavilySearchClient
	genaiClient   *gen_combined.Client
	planner       *Planner
	executor      *Executor
	reportGen     *ReportGenerator
	balancer      *Balancer
	synthesizer   *Synthesizer
	maxSources    int
	maxSubQueries int
	onProgress    ProgressCallback // Optional callback for progress events
}

// ResearchResult contains the final research report
type ResearchResult struct {
	Report          string
	CounterPosition string
	Synthesis       string
	Sources         []Source
	SubQueries      []string
	Usage           *g.Usage
	BalancingUsage  *g.Usage
	SynthesizingUsage *g.Usage
}

// Source represents a research source
type Source struct {
	Title   string
	URL     string
	Content string
	Score   float64
}

// Config holds research configuration
type Config struct {
	MaxSources    int
	MaxSubQueries int
	Model         string
}

// NewResearcher creates a new Researcher instance
func NewResearcher(
	query string,
	model string,
	tavilyClient TavilySearchClient,
	genaiClient *gen_combined.Client,
	cfg Config,
) *Researcher {
	if cfg.MaxSources <= 0 {
		cfg.MaxSources = 10
	}
	if cfg.MaxSubQueries <= 0 {
		cfg.MaxSubQueries = 5
	}

	return &Researcher{
		query:         query,
		model:         model,
		tavilyClient:  tavilyClient,
		genaiClient:   genaiClient,
		planner:       NewPlanner(genaiClient, model),
		executor:      NewExecutor(tavilyClient),
		reportGen:     NewReportGenerator(genaiClient, model),
		balancer:      NewBalancer(genaiClient, model),
		synthesizer:   NewSynthesizer(genaiClient, model),
		maxSources:    cfg.MaxSources,
		maxSubQueries: cfg.MaxSubQueries,
	}
}

// WithProgressCallback sets a callback for progress events
func (r *Researcher) WithProgressCallback(callback ProgressCallback) *Researcher {
	r.onProgress = callback
	return r
}

// ConductResearch performs the full research workflow
func (r *Researcher) ConductResearch(ctx context.Context) (*ResearchResult, error) {
	log.Printf("[Research] Starting research for query: %s", r.query)
	if r.onProgress != nil {
		r.onProgress("start", map[string]interface{}{
			"query": r.query,
		})
	}

	// Step 1: Plan - Generate sub-queries
	log.Printf("[Research] Step 1: Planning sub-queries...")
	if r.onProgress != nil {
		r.onProgress("planning", map[string]interface{}{
			"status": "in_progress",
		})
	}
	subQueries, err := r.planner.Plan(ctx, r.query, r.maxSubQueries)
	if err != nil {
		if r.onProgress != nil {
			r.onProgress("error", map[string]interface{}{
				"stage": "planning",
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("planning failed: %w", err)
	}
	log.Printf("[Research] Generated %d sub-queries", len(subQueries))
	if r.onProgress != nil {
		r.onProgress("planning", map[string]interface{}{
			"status":     "completed",
			"subQueries": subQueries,
			"count":      len(subQueries),
		})
	}

	// Step 2: Execute - Parallel search for each sub-query
	log.Printf("[Research] Step 2: Executing parallel searches...")
	if r.onProgress != nil {
		r.onProgress("searching", map[string]interface{}{
			"status":     "in_progress",
			"subQueries": len(subQueries),
		})
	}
	sources, err := r.executor.Execute(ctx, subQueries, r.maxSources)
	if err != nil {
		if r.onProgress != nil {
			r.onProgress("error", map[string]interface{}{
				"stage": "searching",
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("execution failed: %w", err)
	}
	log.Printf("[Research] Collected %d sources", len(sources))
	if r.onProgress != nil {
		r.onProgress("searching", map[string]interface{}{
			"status":  "completed",
			"sources": len(sources),
		})
	}

	// Step 3: Generate final report
	log.Printf("[Research] Step 3: Generating report...")
	if r.onProgress != nil {
		r.onProgress("generating", map[string]interface{}{
			"status": "in_progress",
		})
	}
	report, usage, err := r.reportGen.Generate(ctx, r.query, subQueries, sources)
	if err != nil {
		if r.onProgress != nil {
			r.onProgress("error", map[string]interface{}{
				"stage": "generating",
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("report generation failed: %w", err)
	}
	log.Printf("[Research] Report generated successfully")
	if r.onProgress != nil {
		r.onProgress("generating", map[string]interface{}{
			"status": "completed",
		})
	}

	// Step 4: Generate counter-position (balancing)
	log.Printf("[Research] Step 4: Generating counter-position...")
	if r.onProgress != nil {
		r.onProgress("balancing", map[string]interface{}{
			"status": "in_progress",
		})
	}
	counterPosition, balancingUsage, err := r.balancer.GenerateCounterPosition(ctx, r.query, report, subQueries, sources)
	if err != nil {
		if r.onProgress != nil {
			r.onProgress("error", map[string]interface{}{
				"stage": "balancing",
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("counter-position generation failed: %w", err)
	}
	log.Printf("[Research] Counter-position generated successfully")
	if r.onProgress != nil {
		r.onProgress("balancing", map[string]interface{}{
			"status": "completed",
		})
	}

	// Step 5: Synthesize both reports
	log.Printf("[Research] Step 5: Synthesizing reports...")
	if r.onProgress != nil {
		r.onProgress("synthesizing", map[string]interface{}{
			"status": "in_progress",
		})
	}
	synthesis, synthesizingUsage, err := r.synthesizer.Synthesize(ctx, r.query, report, counterPosition, subQueries, sources)
	if err != nil {
		if r.onProgress != nil {
			r.onProgress("error", map[string]interface{}{
				"stage": "synthesizing",
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("synthesis failed: %w", err)
	}
	log.Printf("[Research] Synthesis generated successfully")
	if r.onProgress != nil {
		r.onProgress("synthesizing", map[string]interface{}{
			"status": "completed",
		})
	}

	return &ResearchResult{
		Report:            report,
		CounterPosition:   counterPosition,
		Synthesis:         synthesis,
		Sources:           sources,
		SubQueries:        subQueries,
		Usage:             usage,
		BalancingUsage:    balancingUsage,
		SynthesizingUsage: synthesizingUsage,
	}, nil
}

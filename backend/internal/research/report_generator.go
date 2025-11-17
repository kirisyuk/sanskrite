package research

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	g "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
)

// ReportGenerator creates the final research report from collected sources
type ReportGenerator struct {
	genaiClient *gen_combined.Client
	model       string
}

// NewReportGenerator creates a new ReportGenerator instance
func NewReportGenerator(genaiClient *gen_combined.Client, model string) *ReportGenerator {
	return &ReportGenerator{
		genaiClient: genaiClient,
		model:       model,
	}
}

// Generate creates a comprehensive report from research sources
func (r *ReportGenerator) Generate(ctx context.Context, query string, subQueries []string, sources []Source) (string, *g.Usage, error) {
	// Build context from sources
	var sourceTexts []string
	var sourceURLs []string
	for i, source := range sources {
		if i >= 20 { // Limit to top 20 sources to avoid token limits
			break
		}
		sourceTexts = append(sourceTexts, fmt.Sprintf("Source %d: %s\nURL: %s\nContent: %s\n", i+1, source.Title, source.URL, source.Content))
		sourceURLs = append(sourceURLs, source.URL)
	}

	contextText := strings.Join(sourceTexts, "\n---\n\n")
	currentDate := time.Now().Format("January 2, 2006")

	prompt := fmt.Sprintf(`You are a research assistant. Create a comprehensive research report based on the following query and collected sources.

Current Date: %s

Research Query: %s

Sub-queries explored:
%s

Sources:
%s

Requirements:
- Write a comprehensive, well-structured report
- Synthesize information from multiple sources
- Prioritize the most current and up-to-date information available as of %s
- Focus on recent developments, latest trends, and current information relevant to the current date
- Include citations with source URLs in format [Source N]
- Be objective and factual
- Use professional, neutral tone throughout
- Avoid enthusiastic, exaggerated, or promotional language
- Avoid superlatives and hyperbolic expressions (e.g., "revolutionary", "groundbreaking", "unprecedented" unless factually accurate)
- Present information matter-of-factly without emotional embellishment
- Highlight key findings and insights in a measured, analytical manner
- Structure with clear sections and headings
- Use markdown formatting
- At the end, include a "Sources" section with a bulleted list (using "-" or "*") of all sources in format: "- [Source Title]. URL: [URL]"

Generate the research report:`, currentDate, query, formatSubQueries(subQueries), contextText, currentDate)

	req := gen_combined.GenAIRequest{
		Model:             r.model,
		Prompt:            prompt,
		MaxOutputTokens:   8192,
		Temperature:       0.7,
		SystemInstruction: "You are an expert research assistant. Create comprehensive, well-structured research reports with proper citations. Maintain a professional, neutral tone and avoid enthusiastic or exaggerated language.",
	}

	resp, err := r.genaiClient.GenAIGenerate(ctx, req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate report: %w", err)
	}

	report := resp.Content
	if report == "" {
		return "", nil, fmt.Errorf("empty report generated")
	}

	log.Printf("[ReportGenerator] Generated report (%d chars)", len(report))
	return report, &resp.Usage, nil
}

func formatSubQueries(queries []string) string {
	var formatted []string
	for i, q := range queries {
		formatted = append(formatted, fmt.Sprintf("%d. %s", i+1, q))
	}
	return strings.Join(formatted, "\n")
}

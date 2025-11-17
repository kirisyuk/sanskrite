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

// Synthesizer creates a synthesis of the original report and counter-position
type Synthesizer struct {
	genaiClient *gen_combined.Client
	model       string
}

// NewSynthesizer creates a new Synthesizer instance
func NewSynthesizer(genaiClient *gen_combined.Client, model string) *Synthesizer {
	return &Synthesizer{
		genaiClient: genaiClient,
		model:       model,
	}
}

// Synthesize creates a comprehensive synthesis that reinterprets both reports
func (s *Synthesizer) Synthesize(ctx context.Context, query string, originalReport string, counterPosition string, subQueries []string, sources []Source) (string, *g.Usage, error) {
	// Build context from sources (limit to top 20)
	var sourceTexts []string
	var sourceURLs []string
	for i, source := range sources {
		if i >= 20 {
			break
		}
		sourceTexts = append(sourceTexts, fmt.Sprintf("Source %d: %s\nURL: %s\nContent: %s\n", i+1, source.Title, source.URL, source.Content))
		sourceURLs = append(sourceURLs, source.URL)
	}
	contextText := strings.Join(sourceTexts, "\n---\n\n")
	currentDate := time.Now().Format("January 2, 2006")

	prompt := fmt.Sprintf(`You are preparing a comprehensive research report based on multiple sources of information.

Current Date: %s

Research Query: %s

Sub-queries explored:
%s

Sources:
%s

You have access to two research documents that contain relevant information:

Document 1:
%s

Document 2:
%s

TASK:
Create a single, unified research report that:
1. Presents a comprehensive analysis of the research query
2. Incorporates relevant information and insights from both documents
3. Provides a coherent, well-structured narrative
4. Uses only factual information and professional analysis
5. Maintains consistency throughout without referencing different perspectives or positions

REQUIREMENTS:
- Write as a single, unified report - do not mention that you are synthesizing or combining multiple documents
- Do not reference "Document 1", "Document 2", "original report", "counter-position", or any other document identifiers
- Do not mention different perspectives, positions, or viewpoints
- Do not explain how you resolved contradictions or differences
- Do not discuss your synthesis process or methodology
- Present information as if it comes from a single, authoritative analysis
- Focus on facts, evidence, and professional insights
- Structure with clear sections and headings
- Use markdown formatting
- Include citations with source URLs in format [Source N]
- At the end, include a "Sources" section with a bulleted list (using "-" or "*") of all sources in format: "- [Source Title]. URL: [URL]"

STYLE:
- Write clearly and accessibly, avoiding unnecessary jargon
- Use professional, neutral tone throughout
- Avoid enthusiastic, exaggerated, or promotional language
- Present information matter-of-factly without emotional embellishment
- Be objective and factual
- Write as a high-quality professional research report
- Keep in mind the current date (%s) for temporal context when presenting information

Generate the research report:`, currentDate, query, formatSubQueries(subQueries), contextText, originalReport, counterPosition, currentDate)

	req := gen_combined.GenAIRequest{
		Model:             s.model,
		Prompt:            prompt,
		MaxOutputTokens:   8192,
		Temperature:       0.7,
		SystemInstruction: "You are an expert research analyst preparing a comprehensive, professional research report. Your goal is to create a unified, authoritative report that presents information clearly and factually. Maintain a professional, neutral tone and avoid enthusiastic or exaggerated language. Do not mention synthesis, different perspectives, or your analysis process - simply present the information as a single, coherent report.",
	}

	resp, err := s.genaiClient.GenAIGenerate(ctx, req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to synthesize reports: %w", err)
	}

	synthesis := resp.Content
	if synthesis == "" {
		return "", nil, fmt.Errorf("empty synthesis generated")
	}

	log.Printf("[Synthesizer] Generated synthesis (%d chars)", len(synthesis))
	return synthesis, &resp.Usage, nil
}

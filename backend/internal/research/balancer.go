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

// Balancer generates a counter-position that challenges assumptions and deepens analysis
type Balancer struct {
	genaiClient *gen_combined.Client
	model       string
}

// NewBalancer creates a new Balancer instance
func NewBalancer(genaiClient *gen_combined.Client, model string) *Balancer {
	return &Balancer{
		genaiClient: genaiClient,
		model:       model,
	}
}

// GenerateCounterPosition creates a well-reasoned counter-position based on the original report
func (b *Balancer) GenerateCounterPosition(ctx context.Context, query string, originalReport string, subQueries []string, sources []Source) (string, *g.Usage, error) {
	// Build context from sources (limit to top 20)
	var sourceTexts []string
	for i, source := range sources {
		if i >= 20 {
			break
		}
		sourceTexts = append(sourceTexts, fmt.Sprintf("Source %d: %s\nURL: %s\nContent: %s\n", i+1, source.Title, source.URL, source.Content))
	}
	contextText := strings.Join(sourceTexts, "\n---\n\n")
	currentDate := time.Now().Format("January 2, 2006")

	prompt := fmt.Sprintf(`You are analyzing a research report and producing a well-reasoned counter-position that challenges assumptions and deepens analysis through rigorous evidence. Your goal is to strengthen thinking by identifying overlooked factors, alternative explanations, and measurement gaps.

Current Date: %s

Research Query: %s

Sub-queries explored:
%s

Sources:
%s

Original Research Report:
%s

PRINCIPLES:
- Formulate the strongest plausible counter-argument that an expert would take seriously.
- Evidence discipline: each major claim requires sources with dates. Prefer official statistics and primary data over opinions.
- Prioritize the most current and up-to-date information available as of %s
- Focus on recent developments, latest trends, and current information relevant to the current date
- Practical focus: emphasize what matters for decision-making, not abstract methodology.
- Clear distinctions: separate different types of risks, channels, and explanations that might be conflated.
- Measurable next steps: propose concrete data or tests that could resolve key uncertainties.

APPROACH:
1. Counter-position: Present the strongest alternative view that challenges the original conclusions.
2. What's overlooked: Identify critical factors, assumptions, or measurement gaps that weren't fully considered.
3. Alternative frameworks: Suggest different ways to measure or categorize the problem (e.g., separating compliance risk from shadow economy, distinguishing channels).
4. Practical triangulation: Propose specific data sources, metrics, or stress-tests that could validate or challenge the counter-position.
5. Working hypothesis: Offer a revised estimate or scenario that reflects the counter-arguments, explicitly marked as testable rather than final.

OUTPUT STRUCTURE:
- Counter-position: [concise alternative view]
- What's fundamentally overlooked: [key gaps in the original analysis]
- Alternative measurement framework: [different ways to approach the question, if relevant]
- Practical steps to test: [1-3 concrete measurements or data sources that could resolve uncertainty]
- Working hypothesis: [revised conclusion with explicit uncertainty, framed as testable]

STYLE:
- Write clearly and accessibly, avoiding unnecessary jargon. If technical terms are needed, explain them in context.
- Use natural structure based on content, not rigid templates.
- Prioritize actionable insights over abstract methodology.
- Be concise but thorough—cover the key counter-arguments without excessive expansion.
- Use professional, neutral tone throughout
- Avoid enthusiastic, exaggerated, or promotional language
- Include citations with source URLs in format [Source N]
- Use markdown formatting

CONSTRAINTS:
- Text only—no images, diagrams, or code blocks. Use plain text tables if needed.
- Focus on evidence and reasoning over rhetoric.
- When possible, distinguish between different types of risks or explanations rather than lumping them together.

Generate the counter-position analysis:`, currentDate, query, formatSubQueries(subQueries), contextText, originalReport, currentDate)

	req := gen_combined.GenAIRequest{
		Model:             b.model,
		Prompt:            prompt,
		MaxOutputTokens:   8192,
		Temperature:       0.3,
		SystemInstruction: "You are an expert analyst who produces well-reasoned counter-positions that challenge assumptions and deepen analysis through rigorous evidence. Your goal is to strengthen thinking by identifying overlooked factors, alternative explanations, and measurement gaps. Maintain a professional, neutral tone and focus on evidence and reasoning over rhetoric.",
	}

	resp, err := b.genaiClient.GenAIGenerate(ctx, req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate counter-position: %w", err)
	}

	counterPosition := resp.Content
	if counterPosition == "" {
		return "", nil, fmt.Errorf("empty counter-position generated")
	}

	log.Printf("[Balancer] Generated counter-position (%d chars)", len(counterPosition))
	return counterPosition, &resp.Usage, nil
}

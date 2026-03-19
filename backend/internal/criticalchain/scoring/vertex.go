package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
)

// VertexAIRiskScorer uses Gemini (Search Grounding) for batch risk scoring.
// API key from GEMINI_API_KEY env or Secret Manager (gemini_api_key).
type VertexAIRiskScorer struct {
	enabled bool
	client  *gen_gemini.Client
}

// NewVertexAIRiskScorer creates a new risk scorer.
// Uses Gemini with Search Grounding when GEMINI_API_KEY is available.
func NewVertexAIRiskScorer() *VertexAIRiskScorer {
	projectID := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	apiKey := gcp.GetEnvOrSecret(context.Background(), projectID, "GEMINI_API_KEY", "gemini_api_key")
	enabled := apiKey != ""
	if !enabled {
		log.Printf("[CriticalChain] Gemini risk scoring disabled (GEMINI_API_KEY or gemini_api_key secret not found)")
	}
	var client *gen_gemini.Client
	if apiKey != "" {
		client = gen_gemini.NewClient(apiKey, projectID, "us-central1", "gemini-3-flash-preview", "gemini-3-flash-preview")
	}
	return &VertexAIRiskScorer{enabled: enabled, client: client}
}

// ScoreFacility returns AI-generated risk score (1-5) for a facility.
// Falls back to rule-based when Gemini is disabled or fails.
func (s *VertexAIRiskScorer) ScoreFacility(ctx context.Context, f *models.Facility, signals SignalValues) float64 {
	if !s.enabled || s.client == nil {
		return ComputeNodeRisk(signals)
	}
	prompt := buildRiskPrompt(f, signals)
	resp, err := s.client.Generate(ctx, prompt, nil, gen_gemini.Options{
		Profile:          gen_gemini.ProfileFast,
		GroundingEnabled:  true,
		ResponseFormat:   "json",
		MaxOutputTokens:  512,
	})
	if err != nil {
		log.Printf("[CriticalChain] Gemini risk scoring failed: %v", err)
		return ComputeNodeRisk(signals)
	}
	score, err := parseRiskScore(resp.Content)
	if err != nil {
		log.Printf("[CriticalChain] Gemini risk parse failed: %v", err)
		return ComputeNodeRisk(signals)
	}
	return score
}

func buildRiskPrompt(f *models.Facility, signals SignalValues) string {
	var sb strings.Builder
	sb.WriteString("Assess supply chain risk (1-5) for this facility.\n\n")
	sb.WriteString("Facility: ")
	sb.WriteString(f.Name)
	sb.WriteString(" (")
	sb.WriteString(f.CanonicalPath)
	sb.WriteString(")\nCountry: ")
	sb.WriteString(f.Country)
	sb.WriteString("\nMaterial: ")
	sb.WriteString(f.MaterialID)
	sb.WriteString(", Stage: ")
	sb.WriteString(strconv.Itoa(f.Stage))
	sb.WriteString("\n\nSignal inputs (1-5 each): country_risk=")
	sb.WriteString(formatFloat(signals.CountryRisk))
	sb.WriteString(", conflict_events=")
	sb.WriteString(formatFloat(signals.ConflictEvents))
	sb.WriteString(", supply_concentration=")
	sb.WriteString(formatFloat(signals.SupplyConcentration))
	sb.WriteString("\n\nRespond with JSON only: {\"score\": <1-5>, \"explanation\": \"<brief reason>\"}")
	return sb.String()
}

func formatFloat(v float64) string {
	if v == 0 {
		return "N/A"
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

func parseRiskScore(content string) (float64, error) {
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "{"); idx >= 0 {
		content = content[idx:]
	}
	if idx := strings.LastIndex(content, "}"); idx >= 0 {
		content = content[:idx+1]
	}
	var out struct {
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return 0, err
	}
	if out.Score < 1 {
		out.Score = 1
	}
	if out.Score > 5 {
		out.Score = 5
	}
	return out.Score, nil
}

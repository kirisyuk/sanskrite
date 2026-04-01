package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	discoverynlp "github.com/igorkirisyuk/sanskrite/internal/discovery/nlp"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

type probeDoc struct {
	Name       string `json:"name"`
	SourceHost string `json:"sourceHost"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	URL        string `json:"url,omitempty"`
}

type llmAssessment struct {
	SchemaVersion     string  `json:"schemaVersion"`
	IsEvent           bool    `json:"isEvent"`
	EventClass        string  `json:"eventClass"`
	EventConfidence   float64 `json:"eventConfidence"`
	ShouldCreateZone  bool    `json:"shouldCreateZone"`
	ShouldWarmExisting bool   `json:"shouldWarmExisting"`
	ShouldOnlySignal  bool    `json:"shouldOnlySignal"`
	ConditionType     string  `json:"conditionType"`
	Place             string  `json:"place"`
	CountryCode       string  `json:"countryCode"`
	GeoConfidence     float64 `json:"geoConfidence"`
	Reason            string  `json:"reason"`
}

func main() {
	var (
		projectID    = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
		eventGateURL = flag.String("event-gate-url", strings.TrimSpace(os.Getenv("EVENT_GATE_SERVICE_URL")), "event gate base URL")
		useLLM       = flag.Bool("llm", true, "run episodic LLM adjudication for escalated docs")
		stdinJSONL   = flag.Bool("stdin-jsonl", false, "read probe docs as JSONL from stdin instead of built-in samples")
		modelName    = flag.String("model", "gemini-3.1-flash-lite-preview", "preferred LLM model for adjudication")
		timeout      = flag.Duration("timeout", 20*time.Second, "per-LLM timeout")
	)
	flag.Parse()

	docs := defaultProbeDocs()
	if *stdinJSONL {
		var err error
		docs, err = readProbeDocs(os.Stdin)
		if err != nil {
			log.Fatalf("read docs: %v", err)
		}
	}
	if len(docs) == 0 {
		log.Fatal("no probe docs")
	}

	var gate discoverynlp.EventGate
	if strings.TrimSpace(*eventGateURL) != "" {
		gate = discoverynlp.NewHTTPEventGate(strings.TrimSpace(*eventGateURL), 1500*time.Millisecond)
	}

	var exec *ai.AITaskExecutor
	if *useLLM {
		mm := ai.NewAIModelManager()
		apiKey := loadGeminiOrVertexAPIKey(context.Background(), strings.TrimSpace(*projectID))
		if apiKey == "" {
			log.Printf("warning: no gemini api key found; disabling LLM adjudication")
		} else {
			_ = os.Setenv("GEMINI_API_KEY", apiKey)
			exec = ai.NewAITaskExecutor([]ai.AIService{ai.NewGeminiService(apiKey, mm)}, mm)
		}
	}

	for _, doc := range docs {
		input := discoverynlp.DocumentInput{
			SourceHost: strings.TrimSpace(doc.SourceHost),
			SourceURL:  strings.TrimSpace(doc.URL),
			Title:      strings.TrimSpace(doc.Title),
			Summary:    strings.TrimSpace(doc.Summary),
			URL:        strings.TrimSpace(doc.URL),
		}
		rule := discovery.BuildHorizonMonitorWeakLabel(input)
		escalate, reason := shouldEscalateToLLM(input, rule)

		fmt.Printf("CASE %s\n", firstNonEmpty(doc.Name, trimForLog(doc.Title, 72)))
		fmt.Printf("  title: %s\n", strings.TrimSpace(doc.Title))
		fmt.Printf("  rule: event=%t class=%s conf=%.2f create=%t warm=%t signal=%t reject=%s\n",
			rule.IsEvent, rule.EventClass, rule.EventConfidence, rule.ShouldCreateZone, rule.ShouldWarmExisting, rule.ShouldOnlySignal, strings.TrimSpace(rule.RejectionReason))
		fmt.Printf("  escalate: %t (%s)\n", escalate, reason)

		if gate != nil {
			model, err := gate.Assess(input)
			if err != nil {
				fmt.Printf("  event_gate: error=%v\n", err)
			} else {
				fmt.Printf("  event_gate: event=%t class=%s conf=%.2f create=%t warm=%t signal=%t reject=%s\n",
					model.IsEvent, model.EventClass, model.EventConfidence, model.ShouldCreateZone, model.ShouldWarmExisting, model.ShouldOnlySignal, strings.TrimSpace(model.RejectionReason))
			}
		}

		if escalate && exec != nil {
			ctx, cancel := context.WithTimeout(context.Background(), *timeout)
			llm, usedModel, err := adjudicateWithLLM(ctx, exec, *modelName, input, rule)
			cancel()
			if err != nil {
				fmt.Printf("  llm: error=%v\n", err)
			} else {
				fmt.Printf("  llm: model=%s event=%t class=%s conf=%.2f create=%t warm=%t signal=%t cond=%s place=%s cc=%s geo=%.2f\n",
					usedModel, llm.IsEvent, llm.EventClass, llm.EventConfidence, llm.ShouldCreateZone, llm.ShouldWarmExisting, llm.ShouldOnlySignal,
					llm.ConditionType, llm.Place, strings.ToUpper(strings.TrimSpace(llm.CountryCode)), llm.GeoConfidence)
				fmt.Printf("  llm_reason: %s\n", strings.TrimSpace(llm.Reason))
			}
		}
		fmt.Println()
	}
}

func shouldEscalateToLLM(doc discoverynlp.DocumentInput, rule *discoverynlp.ArticleAssessment) (bool, string) {
	if rule == nil {
		return true, "missing_rule"
	}
	text := strings.ToLower(strings.TrimSpace(doc.Title + " " + doc.Summary))
	if hasAny(text,
		"blackout", "power outage", "grid outage", "telecom outage", "internet outage",
		"cyber attack", "cyberattack", "ransomware", "chemical spill", "toxic spill",
		"water shutdown", "dock strike", "workers strike", "walkout", "outbreak", "epidemic",
		"cholera", "measles", "dengue", "evacuations ordered", "cargo handling halted",
	) {
		return true, "supported_concrete_class"
	}
	if !rule.IsEvent && rule.AnalysisScore >= 0.90 && rule.EventConfidence >= 0.85 {
		return false, "high_confidence_rule_reject"
	}
	if rule.ShouldCreateZone && rule.EventConfidence >= 0.86 && rule.ConcreteOutageScore >= 0.80 && !rule.ShouldWarmExisting {
		return false, "high_confidence_rule_create"
	}
	if rule.ShouldWarmExisting {
		return true, "warm_existing_path"
	}
	if rule.ShouldOnlySignal {
		return true, "signal_only_path"
	}
	if rule.EventConfidence < 0.82 {
		return true, "low_rule_confidence"
	}
	if rule.ConcreteOutageScore >= 0.42 && !rule.ShouldCreateZone {
		return true, "latent_outage_signal"
	}
	return false, "stable_rule"
}

func adjudicateWithLLM(ctx context.Context, exec *ai.AITaskExecutor, preferredModel string, doc discoverynlp.DocumentInput, rule *discoverynlp.ArticleAssessment) (llmAssessment, string, error) {
	var zero llmAssessment
	if exec == nil {
		return zero, "", fmt.Errorf("llm executor is nil")
	}
	prompt := buildPrompt(doc, rule)
	verifier := ai.NewGenericVerifier(func(out llmAssessment) error {
		if strings.TrimSpace(out.SchemaVersion) == "" {
			return fmt.Errorf("schemaVersion is required")
		}
		switch strings.TrimSpace(out.EventClass) {
		case "real_event", "analysis", "advisory", "broad_scope", "noise":
		default:
			return fmt.Errorf("invalid eventClass %q", out.EventClass)
		}
		if out.EventConfidence < 0 || out.EventConfidence > 1 {
			return fmt.Errorf("eventConfidence out of range")
		}
		if out.GeoConfidence < 0 || out.GeoConfidence > 1 {
			return fmt.Errorf("geoConfidence out of range")
		}
		return nil
	})
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContextAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:           strings.TrimSpace(preferredModel),
			MinContextWindow:         2000,
			MaxOutputSize:            500,
			MinCapabilityScore:       7,
			TextGenerationCapability: true,
		},
		Verifier: verifier,
	}
	parsedAny, usedModel, err := exec.ExecuteStructuredTaskWithModel(ctx, task)
	if err != nil {
		return zero, usedModel, err
	}
	out, ok := parsedAny.(llmAssessment)
	if !ok {
		return zero, usedModel, fmt.Errorf("unexpected parsed type %T", parsedAny)
	}
	return out, usedModel, nil
}

func buildPrompt(doc discoverynlp.DocumentInput, rule *discoverynlp.ArticleAssessment) string {
	var b strings.Builder
	b.WriteString("You classify one news document for an affected-zone pipeline.\n")
	b.WriteString("Decide whether this is a real event worth creating or warming an affected zone, or only a signal, or should be rejected as analysis/advisory/broad_scope/noise.\n")
	b.WriteString("Prefer precision over recall, but do not miss concrete outages, strikes, outbreaks, environmental spills, blackouts, port closures, cyber outages, or direct operational disruptions.\n")
	b.WriteString("Return strict JSON only.\n\n")
	b.WriteString("Document:\n")
	b.WriteString("sourceHost: " + strings.TrimSpace(doc.SourceHost) + "\n")
	b.WriteString("title: " + strings.TrimSpace(doc.Title) + "\n")
	b.WriteString("summary: " + strings.TrimSpace(doc.Summary) + "\n")
	if rule != nil {
		b.WriteString("\nRule weak label:\n")
		b.WriteString(fmt.Sprintf("isEvent=%t eventClass=%s eventConfidence=%.2f shouldCreateZone=%t shouldWarmExisting=%t shouldOnlySignal=%t rejectionReason=%s\n",
			rule.IsEvent, rule.EventClass, rule.EventConfidence, rule.ShouldCreateZone, rule.ShouldWarmExisting, rule.ShouldOnlySignal, strings.TrimSpace(rule.RejectionReason)))
	}
	b.WriteString("\nSchema:\n")
	b.WriteString("{\"schemaVersion\":string,\"isEvent\":bool,\"eventClass\":\"real_event|analysis|advisory|broad_scope|noise\",\"eventConfidence\":number,\"shouldCreateZone\":bool,\"shouldWarmExisting\":bool,\"shouldOnlySignal\":bool,\"conditionType\":string,\"place\":string,\"countryCode\":string,\"geoConfidence\":number,\"reason\":string}\n")
	return b.String()
}

func defaultProbeDocs() []probeDoc {
	return []probeDoc{
		{
			Name:       "vancouver_dock_strike",
			SourceHost: "reuters.com",
			Title:      "Dockworkers strike shuts down Canada's Port of Vancouver",
			Summary:    "Port closures and halted cargo handling disrupt shipments across western Canada.",
		},
		{
			Name:       "sydney_port_cyber",
			SourceHost: "reuters.com",
			Title:      "Cyberattack forces major Australian port operator offline in Sydney",
			Summary:    "Port operations remain suspended after ransomware attack disrupted container handling.",
		},
		{
			Name:       "madrid_blackout",
			SourceHost: "reuters.com",
			Title:      "Spain declares emergency after blackout hits Madrid transit network",
			Summary:    "Grid failure causes metro shutdowns and widespread service disruption in the capital.",
		},
		{
			Name:       "port_sudan_cholera",
			SourceHost: "reuters.com",
			Title:      "Cholera outbreak spreads in camps near Port Sudan as cases surge",
			Summary:    "Health agencies report rising infections and emergency response in displacement camps.",
		},
		{
			Name:       "quito_toxic_spill",
			SourceHost: "reuters.com",
			Title:      "Toxic chemical spill contaminates river near Quito forcing water shutdown",
			Summary:    "Authorities order evacuations and suspend water service after the spill.",
		},
		{
			Name:       "red_sea_analysis",
			SourceHost: "foreignpolicy.com",
			Title:      "Why Red Sea shipping risks could reshape global trade",
			Summary:    "An analysis of how persistent maritime insecurity may alter logistics patterns.",
		},
	}
}

func readProbeDocs(r *os.File) ([]probeDoc, error) {
	scanner := bufio.NewScanner(r)
	out := make([]probeDoc, 0, 16)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var doc probeDoc
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func loadGeminiOrVertexAPIKey(ctx context.Context, projectID string) string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VERTEX_API_KEY")); v != "" {
		return v
	}
	candidates := []string{"gemini_api_key", "GEMINI_API_KEY", "vertex_api_key", "VERTEX_API_KEY"}
	for _, secret := range candidates {
		if v := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, projectID, secret, secret)); v != "" {
			return v
		}
	}
	return ""
}

func hasAny(text string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(kw))) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func trimForLog(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

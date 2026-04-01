package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/supplychainreport"
)

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	lensID := flag.String("lens-id", "", "lens id")
	outPath := flag.String("out", "", "optional path to write markdown report")
	limit := flag.Int("limit", 160, "max supply-chain paths to include")
	llmEnabled := flag.Bool("llm", true, "enable section-by-section LLM polish")
	model := flag.String("model", supplychainreport.DefaultPolishModel, "Gemini model for polishing")
	maxOutputTokens := flag.Int("max-output-tokens", supplychainreport.DefaultPolishMaxOutputTokens, "max output tokens per section")
	thinkingBudget := flag.Int("thinking-budget", supplychainreport.DefaultPolishThinkingBudget, "thinking budget per section")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if strings.TrimSpace(*lensID) == "" {
		log.Fatal("--lens-id is required")
	}

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	db := fs.Client()
	lensesRepo := hrepo.NewLensesRepository(db)
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := hrepo.NewDependencyEdgesRepository(db)
	materializedRepo := hrepo.NewMaterializedArtifactsRepository(db)

	lens, err := lensesRepo.Get(ctx, strings.TrimSpace(*lensID))
	if err != nil {
		log.Fatalf("load lens failed: %v", err)
	}
	input, err := supplychainreport.CollectInput(ctx, lens, strings.TrimSpace(firstNonEmpty(lens.TenantID, "global")), materializedRepo, fragmentsRepo, edgesRepo, *limit)
	if err != nil {
		log.Fatalf("collect report input failed: %v", err)
	}
	log.Printf("collected substrate: paths=%d scopePaths=%d", len(input.Chains), len(input.Resolved.ScopePaths))
	report := supplychainreport.Build(input)
	polished := map[string]supplychainreport.PolishResult(nil)
	if *llmEnabled {
		log.Printf("loading GEMINI_API_KEY from env/secret")
		geminiAPIKey := gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GEMINI_API_KEY", "gemini_api_key")
		if strings.TrimSpace(geminiAPIKey) == "" {
			log.Printf("WARN: GEMINI_API_KEY not found in env or Secret Manager, falling back to deterministic sections")
		} else {
			vertexLocation := firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1")
			log.Printf("starting LLM polish: model=%s maxOutputTokens=%d thinkingBudget=%d", strings.TrimSpace(*model), *maxOutputTokens, *thinkingBudget)
			client := gen_combined.NewClient(gen_gemini.NewClient(geminiAPIKey, strings.TrimSpace(*projectID), vertexLocation, "", ""), "")
			polished = supplychainreport.PolishPackages(ctx, report.Packages, toolSectionGenerator{client: client}, supplychainreport.PolishOptions{
				Enabled:         true,
				Model:           strings.TrimSpace(*model),
				MaxOutputTokens: *maxOutputTokens,
				ThinkingBudget:  thinkingBudget,
			})
			report.Sections = supplychainreport.BuildSectionsFromPackages(report.Packages, polished)
			report.Markdown = supplychainreport.AssembleMarkdown(report.Sections)
		}
	}

	if strings.TrimSpace(*outPath) != "" {
		if err := os.WriteFile(strings.TrimSpace(*outPath), []byte(report.Markdown+"\n"), 0o644); err != nil {
			log.Fatalf("write report failed: %v", err)
		}
		fmt.Printf("wrote %s\n", strings.TrimSpace(*outPath))
		return
	}

	fmt.Println(report.Markdown)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

type toolSectionGenerator struct {
	client *gen_combined.Client
}

func (g toolSectionGenerator) Generate(ctx context.Context, req supplychainreport.GenerateRequest) (*supplychainreport.GenerateResponse, error) {
	if g.client == nil {
		return nil, fmt.Errorf("genai client is not configured")
	}
	sectionID := "unknown"
	if idx := strings.Index(req.Prompt, "\n"); idx > 0 {
		sectionID = strings.TrimSpace(req.Prompt[:idx])
	}
	log.Printf("llm section start: %s model=%s", sectionID, strings.TrimSpace(req.Model))
	resp, err := g.client.GenAIGenerate(ctx, gen_combined.GenAIRequest{
		Model:             strings.TrimSpace(req.Model),
		Prompt:            req.Prompt,
		SystemInstruction: req.SystemInstruction,
		Temperature:       0.2,
		MaxOutputTokens:   req.MaxOutputTokens,
		ThinkingBudget:    req.ThinkingBudget,
	})
	if err != nil {
		log.Printf("llm section error: %s err=%v", sectionID, err)
		return nil, err
	}
	log.Printf("llm section done: %s model=%s tokens=%d", sectionID, strings.TrimSpace(resp.Model), resp.Usage.TotalTokens)
	return &supplychainreport.GenerateResponse{
		Content:    resp.Content,
		Model:      strings.TrimSpace(resp.Model),
		TokensUsed: resp.Usage.TotalTokens,
	}, nil
}

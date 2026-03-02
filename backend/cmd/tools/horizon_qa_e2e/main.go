// horizon_qa_e2e runs the full Lens Q&A pipeline E2E: question → decompose → simulate → leaf evidence → synthesize.
//
// Use to validate the QA flow on a real graph, find logic drift, and tune the core.
//
// Flow:
//  1. Load lens + fragments from Firestore
//  2. Decompose question (LLM)
//  3. Simulate signals (propagation, in-memory)
//  4. Collect leaf evidence (ontology facts, fragment state)
//  5. Synthesize answer (LLM)
//
// Usage:
//
//	go run ./cmd/tools/horizon_qa_e2e -project=PROJECT -lens=LENS_ID -question="How would a 5% VAT increase in Indonesia affect battery costs?"
//
// 12-axis mapping: see horizon_propagation_calibration (buildNewsAndSignalsPrompt, parseSignalsFromResponse).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizoninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonqa "github.com/igorkirisyuk/sanskrite/internal/horizon/qa"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required unless -demo)")
	lensID    = flag.String("lens", "", "Lens ID (required unless -demo)")
	question  = flag.String("question", "How would a 5% VAT increase in Indonesia affect battery costs?", "Question to ask")
	demo      = flag.Bool("demo", false, "Demo mode: no Firestore, stub EV battery supply chain data. Use for local run without GCP.")
	grounding = flag.String("grounding", "", "Reserved: 'search' for future fallback (news → 12-axis via horizon_propagation_calibration logic)")
	verbose   = flag.Bool("verbose", false, "Verbose logging")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	q := strings.TrimSpace(*question)
	if q == "" {
		log.Fatal("--question is required")
	}
	_ = grounding // reserved for future search fallback

	if *demo {
		runDemo(ctx, q)
		return
	}

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required (or use -demo for local run without Firestore)")
	}
	if strings.TrimSpace(*lensID) == "" {
		log.Fatal("--lens is required (or use -demo for local run without Firestore)")
	}

	proj := strings.TrimSpace(*projectID)
	fs, err := firestore.NewClient(ctx, proj)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	// Repos
	frRepo := horizonrepo.NewHorizonFragmentsRepository(fs)
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fs)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fs)
	lensesRepo := horizonrepo.NewLensesRepository(fs)
	artifactsRepo := horizonrepo.NewMaterializedArtifactsRepository(fs)
	ontologyFactsRepo := corerepo.NewOntologyFactsRepository(fs)
	profilesRepo := horizonrepo.NewPropagationProfilesRepository(fs)

	// AI executor (Gemini or Vertex)
	exec := initAIExecutor(ctx, proj)
	if exec == nil {
		log.Fatal("AI executor not configured (GEMINI_API_KEY or Vertex)")
	}

	// Propagation
	profileLookup := horizonprop.NewProfileLookup(profilesRepo)
	if err := profileLookup.Initialize(ctx); err != nil {
		log.Printf("ProfileLookup init warning: %v", err)
	}
	propEngine := horizonprop.NewPropagationEngine(frRepo, versionsRepo, edgesRepo, lensesRepo, nil).
		WithProfileLookup(profileLookup)

	// QA components
	decomposer := horizonqa.NewQuestionDecomposer(exec)
	seedResolver := horizonqa.NewSeedResolver()
	leafEvidence := horizonqa.NewLeafEvidenceCollector(horizoninterp.NewOntologyEventFactsResolver(ontologyFactsRepo))
	qaEngine := horizonqa.NewEngine(decomposer, seedResolver, leafEvidence, propEngine, profileLookup, frRepo, artifactsRepo)
	// Vertex AI Search: same config as horizon fragment indexing (GCS → Vertex). Required for QA alternatives.
	vertexDS := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"))
	if vertexDS == "" {
		vertexDS = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID"))
	}
	if vertexDS == "" {
		vertexDS = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"))
	}
	if vertexDS != "" {
		vertexLoc := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_LOCATION"))
		if vertexLoc == "" {
			vertexLoc = "global"
		}
		vertexClient := ai.NewVertexAISearchClient(proj, vertexLoc, vertexDS, "")
		qaEngine = qaEngine.WithVertexSearch(vertexClient)
		log.Printf("Vertex AI Search wired for QA alternatives (dataStore=%s)", vertexDS)
	}
	contextBuilder := horizonqa.NewAnswerContextBuilder()
	synthesizer := horizonqa.NewAnswerSynthesizer(exec)

	// Load lens
	lens, err := lensesRepo.Get(ctx, strings.TrimSpace(*lensID))
	if err != nil || lens == nil {
		log.Fatalf("lens not found: %v", err)
	}
	log.Printf("Lens: %s scope=%d paths", lens.ID, len(lens.Scope))

	// Load fragments
	fragments, err := frRepo.ListByCanonicalPaths(ctx, lens.Scope)
	if err != nil {
		log.Fatalf("ListByCanonicalPaths failed: %v", err)
	}
	log.Printf("Fragments: %d", len(fragments))

	scopeFragments := lens.ScopeFragments
	if scopeFragments == nil {
		scopeFragments = make(map[string]string)
	}
	for _, f := range fragments {
		if f != nil && f.CanonicalPath != "" && f.ID != "" {
			scopeFragments[f.CanonicalPath] = f.ID
		}
	}
	lens.ScopeFragments = scopeFragments

	// Artifact summaries
	var artifactSummaries []string
	arts, _ := artifactsRepo.ListByLens(ctx, *lensID, 20)
	for _, a := range arts {
		if a != nil && a.DisplayName != "" {
			artifactSummaries = append(artifactSummaries, a.DisplayName+": "+a.CanonicalKey)
		}
	}

	// Run QA
	req := &horizonqa.QARequest{
		Question:          q,
		Lens:              lens,
		Fragments:         fragments,
		ArtifactSummaries: artifactSummaries,
	}

	t0 := time.Now()
	simResult, plan, runErr := qaEngine.Run(ctx, req)
	elapsed := time.Since(t0)

	if runErr != nil {
		log.Fatalf("QA Run failed: %v", runErr)
	}

	if plan.AnswerMode == "out_of_scope" {
		fmt.Println("\n--- OUT OF SCOPE ---")
		fmt.Println("This question doesn't seem related to the lens scope.")
		return
	}

	// Humanization at finish: run Interpreter on top affected nodes (optional)
	var nodeInterpretations []*horizoninterp.RiskInterpretation
	if len(simResult.NodeImpacts) > 0 {
		interpreter := horizoninterp.NewInterpreter(exec, frRepo, horizoninterp.NewOntologySignalFactsRepo(ontologyFactsRepo))
		polarity := "risk"
		if len(plan.HypotheticalSignals) > 0 && plan.HypotheticalSignals[0].Polarity != "" {
			polarity = plan.HypotheticalSignals[0].Polarity
		}
		nodeInterpretations = horizonqa.RunFinishInterpretation(ctx, interpreter, simResult, fragments, polarity, 5)
		if *verbose && len(nodeInterpretations) > 0 {
			fmt.Println("\n--- NODE INTERPRETATIONS (humanization at finish) ---")
			for i, interp := range nodeInterpretations {
				fmt.Printf("[%d] %s: %s\n", i+1, interp.CanonicalPath, truncate(interp.Summary, 120))
			}
		}
	}

	// Synthesize
	contextPrompt, ctxErr := contextBuilder.BuildContext(ctx, plan, simResult, nil, artifactSummaries, nodeInterpretations, simResult.VertexAlternatives)
	if ctxErr != nil {
		log.Fatalf("BuildContext failed: %v", ctxErr)
	}

	if *verbose {
		fmt.Println("\n--- CONTEXT (first 2000 chars) ---")
		fmt.Println(truncate(contextPrompt, 2000))
	}

	answer, synthErr := synthesizer.Synthesize(ctx, contextPrompt, plan, simResult)
	if synthErr != nil {
		log.Fatalf("Synthesize failed: %v", synthErr)
	}

	// Business relevance evaluation
	if answer.Text != "" {
		evaluator := horizonqa.NewBusinessRelevanceEvaluator(exec)
		if score, err := evaluator.Evaluate(ctx, req.Question, answer); err == nil {
			answer.BusinessRelevanceScore = score
		}
	}

	printResult(req.Question, plan, simResult, answer, elapsed.Milliseconds())
}

func initAIExecutor(ctx context.Context, projectID string) *ai.AITaskExecutor {
	mm := ai.NewAIModelManager()
	var services []ai.AIService

	// Keys from Secret Manager (priority) or env
	apiKey := ""
	if projectID != "" {
		for _, name := range []string{"gemini_api_key", "GEMINI_API_KEY"} {
			if v, err := gcp.LoadSecret(ctx, projectID, name); err == nil && strings.TrimSpace(v) != "" {
				apiKey = strings.TrimSpace(v)
				break
			}
		}
	}
	if apiKey == "" {
		apiKey = gcp.GetEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "gemini_api_key")
	}
	if apiKey != "" {
		services = append(services, ai.NewGeminiService(apiKey, mm))
	}

	// Vertex AI (ADC) — всегда добавляем при наличии project для fallback
	if projectID != "" {
		loc := strings.TrimSpace(os.Getenv("VERTEX_LOCATION"))
		if loc == "" {
			loc = "us-central1"
		}
		if svc, err := ai.NewVertexAIService(projectID, loc, mm); err == nil {
			if ds := os.Getenv("VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID"); ds != "" {
				svc = svc.WithDataStore(ds)
			}
			services = append(services, svc)
		}
	}

	if len(services) == 0 {
		return nil
	}
	return ai.NewAITaskExecutor(services, mm)
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// runDemo runs the full QA pipeline with stub data (no Firestore). For local validation.
func runDemo(ctx context.Context, question string) {
	log.Printf("DEMO MODE: stub EV battery supply chain, no Firestore")

	proj := strings.TrimSpace(*projectID)
	if proj == "" {
		proj = os.Getenv("GCP_PROJECT_ID")
	}
	if proj == "" {
		proj = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if proj == "" {
		proj = "etigram"
	}
	exec := initAIExecutor(ctx, proj)
	if exec == nil {
		log.Fatalf("AI executor not configured. Use -project=YOUR_GCP_PROJECT (or GCP_PROJECT_ID) for Secret Manager, or gcloud auth application-default login for Vertex")
	}

	// Stub lens: Indonesia nickel → EV battery chain (Scope used by decomposer)
	lens := &horizonmodels.Lens{
		Scope: []string{
			"/org/pt-vale-indonesia",
			"/product/nickel-ore",
			"/org/catl",
			"/product/cathode-material",
			"/product/ev-battery-cell",
		},
	}

	// Stub fragments
	fragments := []*horizonmodels.HorizonFragment{
		{ID: "f1", CanonicalPath: "/org/pt-vale-indonesia", Labels: map[string]string{"en": "PT Vale Indonesia"}},
		{ID: "f2", CanonicalPath: "/product/nickel-ore", Labels: map[string]string{"en": "Nickel ore"}},
		{ID: "f3", CanonicalPath: "/org/catl", Labels: map[string]string{"en": "CATL"}},
		{ID: "f4", CanonicalPath: "/product/cathode-material", Labels: map[string]string{"en": "Cathode material"}},
		{ID: "f5", CanonicalPath: "/product/ev-battery-cell", Labels: map[string]string{"en": "EV battery cell"}},
	}

	// Decompose (LLM)
	decomposer := horizonqa.NewQuestionDecomposer(exec)
	lensCtx := &horizonqa.DecomposeContext{
		ScopePaths: lens.Scope,
		DomainModes: []string{"supply_chain"},
	}
	plan, err := decomposer.Decompose(ctx, question, lensCtx)
	if err != nil {
		log.Fatalf("Decompose failed: %v", err)
	}
	log.Printf("Decompose: answerMode=%s", plan.AnswerMode)

	if plan.AnswerMode == "out_of_scope" {
		fmt.Println("\n--- OUT OF SCOPE ---")
		fmt.Println("This question doesn't seem related to the lens scope.")
		return
	}

	// Stub SimulationResult: propagation output for Indonesia VAT → nickel → battery
	simResult := &horizonmodels.SimulationResult{
		NodeImpacts: []horizonmodels.NodeImpact{
			{FragmentID: "f1", CanonicalPath: "/org/pt-vale-indonesia", Name: "PT Vale Indonesia", AxisAmplitudes: map[string]float64{"physical_flow/supply": 0.72, "financial/settlement": 0.55}, TotalAmplitude: 0.72, Hops: 0},
			{FragmentID: "f2", CanonicalPath: "/product/nickel-ore", Name: "Nickel ore", AxisAmplitudes: map[string]float64{"physical_flow/supply": 0.58}, TotalAmplitude: 0.58, Hops: 1},
			{FragmentID: "f3", CanonicalPath: "/org/catl", Name: "CATL", AxisAmplitudes: map[string]float64{"physical_flow/supply": 0.35}, TotalAmplitude: 0.35, Hops: 2},
			{FragmentID: "f4", CanonicalPath: "/product/cathode-material", Name: "Cathode material", AxisAmplitudes: map[string]float64{"physical_flow/supply": 0.22}, TotalAmplitude: 0.22, Hops: 3},
			{FragmentID: "f5", CanonicalPath: "/product/ev-battery-cell", Name: "EV battery cell", AxisAmplitudes: map[string]float64{"physical_flow/supply": 0.12}, TotalAmplitude: 0.12, Hops: 4},
		},
		TracePaths: []horizonmodels.TracePath{
			{
				NodeCanonicalPaths: []string{"/org/pt-vale-indonesia", "/product/nickel-ore", "/org/catl", "/product/cathode-material", "/product/ev-battery-cell"},
				Axis:               "physical_flow/supply",
				Amplitudes:         []float64{0.72, 0.58, 0.35, 0.22, 0.12},
			},
		},
		LeafEvidence: []horizonmodels.LeafSignalEvidence{
			{FragmentID: "f1", CanonicalPath: "/org/pt-vale-indonesia", Axis: "physical_flow/supply", Amplitude: 0.45, Summary: "AIS: 3 bulk carriers delayed at Sulawesi port", Modality: "actual"},
		},
	}

	// Humanization at finish: Interpreter on top nodes
	interpreter := horizoninterp.NewInterpreter(exec, nil, nil)
	polarity := "risk"
	if len(plan.HypotheticalSignals) > 0 && plan.HypotheticalSignals[0].Polarity != "" {
		polarity = plan.HypotheticalSignals[0].Polarity
	}
	nodeInterpretations := horizonqa.RunFinishInterpretation(ctx, interpreter, simResult, fragments, polarity, 5)
	if *verbose && len(nodeInterpretations) > 0 {
		fmt.Println("\n--- NODE INTERPRETATIONS (humanization at finish) ---")
		for i, interp := range nodeInterpretations {
			fmt.Printf("[%d] %s: %s\n", i+1, interp.CanonicalPath, truncate(interp.Summary, 120))
		}
	}

	// Stub DomainSnapshot for demo: concentration, alternatives, data gaps
	cr4 := 0.72
	domainSnapshot := &horizonmodels.DomainAnswerSnapshot{
		Thesis: horizonmodels.DomainThesis{
			Text: "Indonesia nickel supply chain shows moderate concentration risk.",
		},
		ConcentrationEvidence: []horizonmodels.ConcentrationEvidence{
			{MarketCanonicalPath: "/product/nickel-ore", CR4: &cr4, DataGap: "Indonesia ~40% of global nickel ore"},
		},
		Alternatives: []horizonmodels.Alternative{
			{Title: "Australian nickel (BHP, Ravensthorpe)", CostDelta: "+8% logistics", Feasibility: "medium"},
			{Title: "Increase cathode buffer stock", CostDelta: "~6 weeks inventory", Feasibility: "high"},
		},
		DataGaps: []horizonmodels.DataGap{
			{GapType: "throughput", Hint: "CATL Morowali actual throughput — last reported Q2 2025"},
		},
	}

	// Synthesize
	contextBuilder := horizonqa.NewAnswerContextBuilder()
	artifactSummaries := []string{"Indonesia Nickel → EV Battery: /org/pt-vale-indonesia → /product/nickel-ore → /org/catl → /product/ev-battery-cell"}
	contextPrompt, ctxErr := contextBuilder.BuildContext(ctx, plan, simResult, domainSnapshot, artifactSummaries, nodeInterpretations, nil)
	if ctxErr != nil {
		log.Fatalf("BuildContext failed: %v", ctxErr)
	}

	if *verbose {
		fmt.Println("\n--- CONTEXT (first 2000 chars) ---")
		fmt.Println(truncate(contextPrompt, 2000))
	}

	synthesizer := horizonqa.NewAnswerSynthesizer(exec)
	answer, synthErr := synthesizer.Synthesize(ctx, contextPrompt, plan, simResult)
	if synthErr != nil {
		log.Fatalf("Synthesize failed: %v", synthErr)
	}

	// Business relevance
	if answer.Text != "" {
		evaluator := horizonqa.NewBusinessRelevanceEvaluator(exec)
		if score, err := evaluator.Evaluate(ctx, question, answer); err == nil {
			answer.BusinessRelevanceScore = score
		}
	}

	// Output
	printResult(question, plan, simResult, answer, 0)
}

func printResult(question string, plan *horizonmodels.QuestionPlan, simResult *horizonmodels.SimulationResult, answer *horizonmodels.QAAnswer, elapsedMs int64) {
	out := map[string]interface{}{
		"question":               question,
		"answerMode":             plan.AnswerMode,
		"answer":                 answer.Text,
		"confidence":             answer.Confidence,
		"businessRelevanceScore": answer.BusinessRelevanceScore,
		"affectedNodes":          len(simResult.NodeImpacts),
		"leafEvidence":           len(simResult.LeafEvidence),
		"tracePaths":             len(simResult.TracePaths),
		"highlightedIds":         answer.HighlightedNodeIDs,
		"elapsedMs":              elapsedMs,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println("\n--- RESULT ---")
	fmt.Println(string(b))

	if answer.Text != "" {
		fmt.Println("\n--- ANSWER ---")
		fmt.Println(answer.Text)
	}
}

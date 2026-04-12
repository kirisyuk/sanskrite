package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	agentmeshcontracts "github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/controlplane"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
	agentmeshrepo "github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	agentmeshruntime "github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	agentmeshscout "github.com/igorkirisyuk/sanskrite/internal/agentmesh/scout"
	meshtrace "github.com/igorkirisyuk/sanskrite/internal/agentmesh/trace"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/vertex"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// defaultSmokeQuery is English because retrieval and tool calls run in English internally.
const defaultSmokeQuery = "Trace the supply chain of lithium-ion cathode active materials into Germany: map inward flows (imports, cathode/cell value chain in Germany, OEM demand signals) upstream through CAM and precursors to primary raw materials (lithium salts, nickel, cobalt, manganese sources) and their countries of origin. Prefer concrete companies, sites, trade lanes, and mass-balance hints. Use English for all internal search queries and tool calls."

func main() {
	if strings.TrimSpace(os.Getenv("AGENTMESH_SCOUT_REASONING_LOG")) == "" {
		_ = os.Setenv("AGENTMESH_SCOUT_REASONING_LOG", "1")
	}
	var (
		projectID   = flag.String("project", envOr("GCP_PROJECT_ID", "nv-horizon"), "GCP project ID")
		location    = flag.String("location", envOr("AGENTMESH_VERTEX_LOCATION", "global"), "Vertex model location")
		query       = flag.String("query", defaultSmokeQuery, "Natural-language scouting query (English recommended; internal search/tools use English)")
		target      = flag.String("target", "", "Optional canonical target path")
		tenantID    = flag.String("tenant", "global", "Tenant ID")
		projectRef  = flag.String("project-ref", envOr("HORIZON_PROJECT_ID", envOr("GCP_PROJECT_ID", "nv-horizon")), "Logical Horizon project ID (defaults to HORIZON_PROJECT_ID, else GCP_PROJECT_ID, else nv-horizon)")
		lensID      = flag.String("lens", "", "Optional lens ID")
		allowWrites = flag.Bool("allow-writes", true, "Allow deterministic artifact writes")
		compareOnly = flag.Bool("compare-only", false, "Run in compare-only mode without graph landing semantics")
		dumpJSON    = flag.Bool("dump-json", false, "Also dump raw JSON output at the end")
	)
	flag.Parse()

	if strings.TrimSpace(*query) == "" && strings.TrimSpace(*target) == "" {
		log.Fatal("either --query or --target is required")
	}

	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()
	gcs, err := gcp.NewGCSClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("gcs client: %v", err)
	}

	logger := log.New(os.Stdout, "[agentic-scout-smoke] ", log.LstdFlags|log.Lshortfile)
	store := agentmeshrepo.NewStore(fs.Client(), gcs)
	cardSigner := registry.NewHMACSigner(envOr("AGENTMESH_SERVER_SIGNING_SECRET", "server-local-secret"), "server-local")
	registrySvc := registry.NewService(cardSigner)
	if err := registrySvc.BootstrapSystemAgents(ctx, "https://local.agentmesh.internal"); err != nil {
		log.Fatalf("bootstrap registry: %v", err)
	}
	controlSvc := controlplane.NewService(registrySvc, policy.NewService())
	vertexSvc, err := vertex.NewService(strings.TrimSpace(*projectID), strings.TrimSpace(*location), strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")))
	if err != nil {
		log.Fatalf("vertex service: %v", err)
	}
	{
		modelManager := ai.NewAIModelManager()
		aiServices := []ai.AIService{vertexSvc.AIService()}
		geminiKey := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GEMINI_API_KEY", "gemini_api_key"))
		if geminiKey != "" {
			if os.Getenv("GEMINI_API_KEY") == "" {
				_ = os.Setenv("GEMINI_API_KEY", geminiKey)
			}
			aiServices = append(aiServices, ai.NewGeminiService(geminiKey, modelManager))
		}
		if k := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "OPENAI_API_KEY", "openai_api_key")); k != "" {
			if os.Getenv("OPENAI_API_KEY") == "" {
				_ = os.Setenv("OPENAI_API_KEY", k)
			}
			aiServices = append(aiServices, ai.NewOpenAIService(k, modelManager))
		}
		if k := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GROK_API_KEY", "grok_api_key")); k != "" {
			if os.Getenv("GROK_API_KEY") == "" {
				_ = os.Setenv("GROK_API_KEY", k)
			}
			aiServices = append(aiServices, ai.NewGrokService(k, modelManager))
		}
		vertexSvc.WithAIExecutor(
			ai.NewAITaskExecutor(aiServices, modelManager).
				WithCircuitBreaker(ai.NewCircuitBreaker()),
		)
	}
	traceSvc := meshtrace.NewService(store, logger)
	vertexSearch := ai.NewVertexAISearchClient(strings.TrimSpace(*projectID), envOr("VERTEX_SEARCH_LOCATION", "global"), strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")), "")
	productDictionary := productdict.NewRuntime(fs.Client()).WithTenantID("global")
	agentJobsRepo := corerepo.NewAgentJobsRepository(fs.Client())
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	scoutProcessor := agentmeshscout.NewProcessor(
		agentJobsRepo,
		scoutingJobsRepo,
		horizonrepo.NewHorizonFragmentsRepository(fs.Client()),
		horizonrepo.NewDependencyEdgesRepository(fs.Client()),
		horizonrepo.NewMaterializedArtifactsRepository(fs.Client()),
		productDictionary,
		vertexSearch,
		logger,
	)
	runtimeSvc := agentmeshruntime.NewService(
		store,
		controlSvc,
		vertexSvc,
		traceSvc,
		nil,
		envOr("AGENTMESH_ARTIFACT_BUCKET", strings.TrimSpace(*projectID)+"-agentmesh-artifacts"),
		logger,
	).WithProcessor(scoutProcessor)

	task, err := runtimeSvc.CreateTask(ctx, &agentmeshcontracts.MeshTask{
		TenantID:   strings.TrimSpace(*tenantID),
		Capability: "scout.plan",
		Input: map[string]any{
			"tenantId":            strings.TrimSpace(*tenantID),
			"projectId":           strings.TrimSpace(*projectRef),
			"lensId":              strings.TrimSpace(*lensID),
			"query":               strings.TrimSpace(*query),
			"targetCanonicalPath": strings.TrimSpace(*target),
			"allowWrites":         *allowWrites,
			"compareOnly":         *compareOnly,
		},
	}, policy.RequestContext{CallerAgentID: "internal", DataZone: "internal"}, true)
	if err != nil {
		log.Fatalf("create task: %v", err)
	}
	if err := runtimeSvc.ExecuteTask(ctx, task.ID, "agentic-scout-smoke"); err != nil {
		log.Fatalf("execute task: %v", err)
	}
	finalTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		log.Fatalf("load task: %v", err)
	}
	if finalTask == nil {
		log.Fatal("task not found after execution")
	}

	printReasoningTrace(finalTask)

	if *dumpJSON {
		out := map[string]any{
			"taskId":         finalTask.ID,
			"status":         finalTask.Status,
			"reviewAccepted": finalTask.ReviewAccepted != nil && *finalTask.ReviewAccepted,
			"lastError":      strings.TrimSpace(finalTask.LastError),
			"output":         finalTask.Output,
		}
		raw, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(raw))
	}
}

func printReasoningTrace(task *agentmeshcontracts.MeshTask) {
	if task == nil {
		return
	}
	out := task.Output
	if out == nil {
		out = map[string]any{}
	}

	sep := strings.Repeat("=", 80)
	thin := strings.Repeat("-", 80)

	fmt.Println(sep)
	fmt.Printf("TASK: %s  STATUS: %s\n", task.ID, task.Status)
	if task.LastError != "" {
		fmt.Printf("ERROR: %s\n", task.LastError)
	}
	fmt.Printf("MODEL CALLS: %d  SPEND: $%.4f\n", task.Usage.ModelCalls, task.Usage.EstimatedSpendUSD)
	fmt.Println(sep)

	// Plan
	if plan := mapVal(out["plan"]); len(plan) > 0 {
		fmt.Println("\n--- PLAN ---")
		fmt.Printf("  Goal: %s\n", str(plan["goal"]))
		if role := mapVal(plan["missionRole"]); len(role) > 0 {
			fmt.Printf("  Role: %s (%s)\n", str(role["primaryRole"]), str(role["domain"]))
			fmt.Printf("  Posture: %s\n", str(role["investigationPosture"]))
		}
		if prods := anySlice(plan["focusProducts"]); len(prods) > 0 {
			fmt.Printf("  Focus Products: %s\n", strings.Join(prods, ", "))
		}
		if branches := sliceOfMaps(plan["branches"]); len(branches) > 0 {
			fmt.Printf("  Branches (%d):\n", len(branches))
			for _, b := range branches {
				marker := ""
				if boolVal(b["broadScan"]) {
					marker = " [BROAD SCAN]"
				}
				if boolVal(b["requiresPath"]) {
					marker += " [PATH]"
				}
				fmt.Printf("    [%s]%s %s\n", str(b["id"]), marker, str(b["question"]))
				if hints := anySlice(b["productHints"]); len(hints) > 0 {
					fmt.Printf("      products: %s\n", strings.Join(hints, ", "))
				}
			}
		}
	}

	// Decisions
	if state := mapVal(out["state"]); len(state) > 0 {
		if decisions := sliceOfMaps(state["decisions"]); len(decisions) > 0 {
			fmt.Println("\n--- DECISIONS ---")
			for i, d := range decisions {
				validation := str(d["validationStatus"])
				marker := "  "
				if validation == "fallback" {
					marker = "F>"
				}
				fmt.Printf("\n  %s Decision #%d: %s", marker, i+1, str(d["executedAction"]))
				if bid := str(d["executedBranchId"]); bid != "" {
					fmt.Printf(" branch=%s", bid)
				}
				if conf := floatVal(d["confidence"]); conf > 0 {
					fmt.Printf(" conf=%.2f", conf)
				}
				fmt.Printf(" [%s]\n", validation)
				if why := str(d["whyChosen"]); why != "" {
					fmt.Printf("     WHY: %s\n", truncate(why, 200))
				}
				if whyNotStop := str(d["whyNotStop"]); whyNotStop != "" && str(d["executedAction"]) != "stop" {
					fmt.Printf("     WHY NOT STOP: %s\n", truncate(whyNotStop, 160))
				}
				if eig := floatVal(d["expectedInformationGain"]); eig > 0 {
					fmt.Printf("     EXPECTED INFO GAIN: %.2f\n", eig)
				}
				if evidence := anySlice(d["evidenceUsed"]); len(evidence) > 0 {
					fmt.Printf("     EVIDENCE: %s\n", strings.Join(evidence, "; "))
				}
				if stops := sliceOfMaps(d["stopCriteriaEvaluation"]); len(stops) > 0 {
					for _, sc := range stops {
						mark := "[ ]"
						if boolVal(sc["satisfied"]) {
							mark = "[x]"
						}
						fmt.Printf("       %s %s\n", mark, str(sc["criterion"]))
					}
				}
			}
		}
	}

	// Reasoning Trace (beats)
	if trace := sliceOfMaps(out["reasoningTrace"]); len(trace) > 0 {
		fmt.Println("\n--- REASONING TRACE ---")
		for _, beat := range trace {
			turn := intVal(beat["turn"])
			phase := str(beat["phase"])
			title := str(beat["title"])
			fmt.Printf("\n  [Turn %d / %s] %s\n", turn, phase, title)
			if q := str(beat["questionWeAreAnswering"]); q != "" {
				fmt.Printf("    Q: %s\n", q)
			}
			if items := anySlice(beat["whatWeFound"]); len(items) > 0 {
				fmt.Printf("    FOUND: %s\n", strings.Join(items, "; "))
			}
			if items := anySlice(beat["whatWeDidNotFind"]); len(items) > 0 {
				fmt.Printf("    NOT FOUND: %s\n", strings.Join(items, "; "))
			}
			if inf := str(beat["inference"]); inf != "" {
				fmt.Printf("    INFERENCE: %s\n", truncate(inf, 200))
			}
			if dec := str(beat["decision"]); dec != "" {
				fmt.Printf("    DECISION: %s\n", dec)
				if why := str(beat["decisionWhy"]); why != "" {
					fmt.Printf("    BECAUSE: %s\n", truncate(why, 200))
				}
			}
			if diags := sliceOfMaps(beat["diagnostics"]); len(diags) > 0 {
				for _, d := range diags {
					fmt.Printf("    DIAG [%s]: %s\n", str(d["severity"]), str(d["message"]))
				}
			}
		}
	}

	// Graph Ops
	if state := mapVal(out["state"]); len(state) > 0 {
		if ops := sliceOfMaps(state["pendingGraphOps"]); len(ops) > 0 {
			fmt.Println("\n--- GRAPH OPS ---")
			for _, op := range ops {
				fmt.Printf("  [%s] %s -> %s (%s) conf=%.2f status=%s\n",
					str(op["kind"]), str(op["subjectPath"]), str(op["objectPath"]),
					str(op["edgeType"]), floatVal(op["confidence"]), str(op["status"]))
				if r := str(op["rationale"]); r != "" {
					fmt.Printf("    rationale: %s\n", truncate(r, 160))
				}
			}
		}
	}

	// Branches final state
	if state := mapVal(out["state"]); len(state) > 0 {
		if branches := mapVal(state["branches"]); len(branches) > 0 {
			fmt.Println("\n--- BRANCHES (final state) ---")
			ids := sortedKeys(branches)
			for _, id := range ids {
				b := mapVal(branches[id])
				if len(b) == 0 {
					continue
				}
				fmt.Printf("  [%s] status=%s rounds=%v conf=%.2f\n",
					id, str(b["status"]), b["researchRounds"], floatVal(b["lastConfidence"]))
			}
		}
		if hyps := mapVal(state["hypotheses"]); len(hyps) > 0 {
			fmt.Println("\n--- HYPOTHESES ---")
			ids := sortedKeys(hyps)
			for _, id := range ids {
				h := mapVal(hyps[id])
				if len(h) == 0 {
					continue
				}
				fmt.Printf("  [%s] %s conf=%.2f pursuit=%s matReady=%v\n",
					id, str(h["status"]), floatVal(h["confidence"]),
					str(h["pursuitDecision"]), boolVal(h["materializationReady"]))
				if stmt := str(h["statement"]); stmt != "" {
					fmt.Printf("    statement: %s\n", truncate(stmt, 160))
				}
			}
		}
	}

	// Critic
	if critic := mapVal(out["critic"]); len(critic) > 0 {
		fmt.Println("\n--- LATEST CRITIC ---")
		if items := anySlice(critic["acceptedFindings"]); len(items) > 0 {
			fmt.Printf("  ACCEPTED: %s\n", strings.Join(items, "; "))
		}
		if items := anySlice(critic["riskyClaims"]); len(items) > 0 {
			fmt.Printf("  RISKY: %s\n", strings.Join(items, "; "))
		}
		if items := anySlice(critic["openQuestions"]); len(items) > 0 {
			fmt.Printf("  OPEN: %s\n", strings.Join(items, "; "))
		}
		fmt.Printf("  materializePath=%v confidence=%.2f\n", boolVal(critic["materializePath"]), floatVal(critic["confidence"]))
		if ge := mapVal(critic["goodEnoughAssessment"]); len(ge) > 0 {
			fmt.Printf("  GOOD ENOUGH: status=%s stopNow=%v furtherWorkLowValue=%v\n",
				str(ge["status"]), boolVal(ge["stopNow"]), boolVal(ge["furtherWorkLowValue"]))
			if r := str(ge["reason"]); r != "" {
				fmt.Printf("    reason: %s\n", truncate(r, 200))
			}
		}
	}

	// Summary
	if summary := str(out["reasoningSummary"]); summary != "" {
		fmt.Println("\n--- REASONING SUMMARY ---")
		fmt.Println(summary)
	}

	// Review
	if review := mapVal(out["review"]); len(review) > 0 {
		fmt.Println("\n--- REVIEW ---")
		fmt.Printf("  accepted=%v quality=%s\n", boolVal(review["accepted"]), str(review["quality"]))
		if r := str(review["rationale"]); r != "" {
			fmt.Printf("  rationale: %s\n", truncate(r, 300))
		}
	}

	// Runtime Diagnostics
	if state := mapVal(out["state"]); len(state) > 0 {
		if diags := sliceOfMaps(state["runtimeDiagnosis"]); len(diags) > 0 {
			fmt.Println("\n--- RUNTIME DIAGNOSTICS ---")
			for _, d := range diags {
				fmt.Printf("  [%s/%s] %s\n", str(d["severity"]), str(d["code"]), str(d["message"]))
			}
		}
	}

	fmt.Printf("\n%s\n", sep)
	fmt.Printf("Stop reason: %s\n", str(mapVal(out["state"])["stopReason"]))
	fmt.Printf("Total turns: %v\n", mapVal(out["state"])["turn"])
	fmt.Println(thin)
}

func mapVal(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func str(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func anySlice(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := str(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	default:
		return nil
	}
}

func sliceOfMaps(v any) []map[string]any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if m := mapVal(item); m != nil {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return t
	default:
		return nil
	}
}

func floatVal(v any) float64 {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func intVal(v any) int {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		return 0
	}
}

func boolVal(v any) bool {
	if v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

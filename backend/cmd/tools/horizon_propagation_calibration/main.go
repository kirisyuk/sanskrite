// horizon_propagation_calibration is a calibration tool for propagation logic.
//
// Flow:
//  1. Take a node from the graph (org with many connections, via -path or -list-candidates)
//  2. Fetch news about that node via LLM (Gemini 3 Pro Preview with search grounding)
//  3. Transform news into 12-axis signals
//  4. Run propagation through the graph (simulation mode, no persistence)
//  5. Output human-readable results for manual evaluation and tuning
//
// Usage:
//
//	# Run calibration for a specific org
//	go run ./cmd/tools/horizon_propagation_calibration -project=PROJECT -path=/org/ae/flydubai-llc
//
//	# List high-degree org candidates
//	go run ./cmd/tools/horizon_propagation_calibration -project=PROJECT -list-candidates -limit=20
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
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/google/uuid"
)

const (
	modelGemini3ProPreview = "gemini-3-pro-preview"
	tenantID               = "global"
)

var (
	projectID  = flag.String("project", "", "GCP project ID (required)")
	path       = flag.String("path", "", "Canonical path of the org node (single-node mode)")
	paths      = flag.String("paths", "", "Comma-separated paths for multi-node mode with interference (e.g. /org/ae/g42-llc,/org/sa/public-investment-fund-sa)")
	listMode   = flag.Bool("list-candidates", false, "List orgs by edge count (high-degree first); use -limit to cap")
	limit      = flag.Int("limit", 200, "Max orgs to output when listing candidates (capped at 200)")
	maxEdges   = flag.Int("max-edges", 10000, "Max edges to scan in list-candidates mode (one pass, no per-org queries)")
	simulate   = flag.Bool("simulate", true, "Use simulation mode (no persistence); default true")
	verbose    = flag.Bool("verbose", false, "Verbose logging")
	aggMode    = flag.String("agg", "hop_weighted", "Aggregation for interference nodes: max | sum | hop_weighted")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	frRepo := repo.NewHorizonFragmentsRepository(fs)
	edgesRepo := repo.NewDependencyEdgesRepository(fs)

	if *listMode {
		runListCandidates(ctx, frRepo, edgesRepo, fs)
		return
	}

	// Resolve paths: -paths (comma-separated) or -path (single)
	var pathList []string
	if s := strings.TrimSpace(*paths); s != "" {
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				pathList = append(pathList, p)
			}
		}
	}
	if len(pathList) == 0 {
		if cp := strings.TrimSpace(*path); cp != "" {
			pathList = []string{cp}
		}
	}
	if len(pathList) == 0 {
		log.Fatal("--path or --paths required (or use -list-candidates to find orgs)")
	}

	if len(pathList) >= 2 {
		runMultiNodeCalibration(ctx, frRepo, edgesRepo, fs, pathList)
		return
	}

	// Single-node mode
	cp := pathList[0]
	frag, err := frRepo.GetByCanonicalPath(ctx, cp)
	if err != nil || frag == nil {
		log.Fatalf("fragment not found for path %q: %v", cp, err)
	}
	if frag.Type != models.FragmentTypeOrganization {
		log.Printf("WARNING: fragment type is %s, not ORGANIZATION; propagation may behave differently", frag.Type)
	}

	outEdges, _ := edgesRepo.ListBySource(ctx, tenantID, frag.ID, nil, 5000)
	inEdges, _ := edgesRepo.ListByTarget(ctx, tenantID, frag.ID, nil, 5000)
	log.Printf("Node: id=%s path=%s type=%s edges=%d", frag.ID, frag.CanonicalPath, frag.Type, len(outEdges)+len(inEdges))

	apiKey := loadGeminiAPIKey(ctx)
	gem := gen_gemini.NewClient(apiKey, strings.TrimSpace(*projectID), "us-central1", "", modelGemini3ProPreview)

	orgLabel := frag.Labels["en"]
	if orgLabel == "" {
		orgLabel = strings.TrimSpace(frag.Identifiers["legalName"])
	}
	if orgLabel == "" {
		orgLabel = strings.TrimSpace(frag.Identifiers["shortName"])
	}
	if orgLabel == "" {
		orgLabel = cp
	}

	log.Printf("Fetching news via Gemini (model=%s, grounding=on)...", modelGemini3ProPreview)
	prompt := buildNewsAndSignalsPrompt(orgLabel, cp)
	resp, err := gem.Generate(ctx, prompt, nil, gen_gemini.Options{
		ModelOverride:     modelGemini3ProPreview,
		GroundingEnabled:  true,
		ResponseFormat:    "json",
		MaxOutputTokens:   4096,
		Temperature:      0.3,
	})
	if err != nil {
		log.Fatalf("Gemini failed: %v", err)
	}

	newsSummary, signals := parseSignalsFromResponse(resp.Content, cp)
	if len(signals) == 0 {
		log.Printf("No valid signals extracted. Raw response (first 500 chars): %s", truncate(resp.Content, 500))
		log.Fatal("Cannot continue without signals")
	}
	log.Printf("Extracted %d signals", len(signals))
	if newsSummary != "" {
		log.Printf("News summary: %s", truncate(newsSummary, 200))
	}

	versionsRepo := repo.NewHorizonFragmentVersionsRepository(fs)
	lensesRepo := repo.NewLensesRepository(fs)
	profilesRepo := repo.NewPropagationProfilesRepository(fs)
	profileLookup := propagation.NewProfileLookup(profilesRepo)
	if err := profileLookup.Initialize(ctx); err != nil {
		log.Printf("ProfileLookup init warning: %v (using defaults)", err)
	}

	engine := propagation.NewPropagationEngine(frRepo, versionsRepo, edgesRepo, lensesRepo, nil).
		WithProfileLookup(profileLookup)

	traceCtx := propagation.WithSignalTraceConfig(ctx, propagation.SignalTraceConfig{
		Enabled:  true,
		MaxSteps: 2500,
	})

	seedIDs := []string{frag.ID}
	allResults := make([]*propagationRun, 0, len(signals))

	for i, sig := range signals {
		var result *propagation.SignalSimulationResult
		if *simulate {
			result, err = engine.SimulateSignalFromSeeds(traceCtx, sig, profileLookup, seedIDs)
		} else {
			var res *propagation.SignalPropagationResult
			res, err = engine.PropagateSignalFromSeeds(traceCtx, sig, profileLookup, seedIDs)
			if err == nil && res != nil {
				result = &propagation.SignalSimulationResult{
					AffectedFragments: res.AffectedFragments,
					PropagationPaths:  res.PropagationPaths,
					PropagationTime:   res.PropagationTime,
					Metrics:           res.Metrics,
					Trace:             res.Trace,
					TraceDropped:      res.TraceDropped,
				}
			}
		}
		if err != nil {
			log.Printf("Propagation failed for signal %d (axis=%s): %v", i+1, sig.Axis, err)
			continue
		}
		allResults = append(allResults, &propagationRun{Signal: sig, Result: result})
	}

	printReport(ctx, frRepo, orgLabel, cp, newsSummary, signals, allResults, *simulate)
}

type propagationRun struct {
	Signal   *models.SignalDescriptor
	Result   *propagation.SignalSimulationResult
	SeedPath string // for multi-node: which seed this run came from
}

// seedRunResult holds all propagation runs from a single seed node.
type seedRunResult struct {
	SeedPath     string
	SeedLabel    string
	NewsSummary  string
	Signals      []*models.SignalDescriptor
	Runs         []*propagationRun
}

func runMultiNodeCalibration(ctx context.Context, frRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository, fs *firestore.Client, pathList []string) {
	apiKey := loadGeminiAPIKey(ctx)
	gem := gen_gemini.NewClient(apiKey, strings.TrimSpace(*projectID), "us-central1", "", modelGemini3ProPreview)

	versionsRepo := repo.NewHorizonFragmentVersionsRepository(fs)
	lensesRepo := repo.NewLensesRepository(fs)
	profilesRepo := repo.NewPropagationProfilesRepository(fs)
	profileLookup := propagation.NewProfileLookup(profilesRepo)
	if err := profileLookup.Initialize(ctx); err != nil {
		log.Printf("ProfileLookup init warning: %v (using defaults)", err)
	}

	engine := propagation.NewPropagationEngine(frRepo, versionsRepo, edgesRepo, lensesRepo, nil).
		WithProfileLookup(profileLookup)

	traceCtx := propagation.WithSignalTraceConfig(ctx, propagation.SignalTraceConfig{
		Enabled:  true,
		MaxSteps: 2500,
	})

	var allSeedResults []seedRunResult

	for _, cp := range pathList {
		cp = strings.TrimSpace(cp)
		frag, err := frRepo.GetByCanonicalPath(ctx, cp)
		if err != nil || frag == nil {
			log.Printf("SKIP path %q: fragment not found: %v", cp, err)
			continue
		}

		orgLabel := frag.Labels["en"]
		if orgLabel == "" {
			orgLabel = strings.TrimSpace(frag.Identifiers["legalName"])
		}
		if orgLabel == "" {
			orgLabel = strings.TrimSpace(frag.Identifiers["shortName"])
		}
		if orgLabel == "" {
			orgLabel = cp
		}

		log.Printf("── Seed %s (%s) ──", cp, orgLabel)
		log.Printf("Fetching news via Gemini...")
		prompt := buildNewsAndSignalsPrompt(orgLabel, cp)
		resp, err := gem.Generate(ctx, prompt, nil, gen_gemini.Options{
			ModelOverride:     modelGemini3ProPreview,
			GroundingEnabled:  true,
			ResponseFormat:    "json",
			MaxOutputTokens:   4096,
			Temperature:      0.3,
		})
		if err != nil {
			log.Printf("Gemini failed for %s: %v", cp, err)
			continue
		}

		newsSummary, signals := parseSignalsFromResponse(resp.Content, cp)
		if len(signals) == 0 {
			log.Printf("No valid signals for %s", cp)
			continue
		}
		log.Printf("Extracted %d signals", len(signals))

		seedIDs := []string{frag.ID}
		runs := make([]*propagationRun, 0, len(signals))

		for i, sig := range signals {
			result, err := engine.SimulateSignalFromSeeds(traceCtx, sig, profileLookup, seedIDs)
			if err != nil {
				log.Printf("Propagation failed for signal %d (axis=%s): %v", i+1, sig.Axis, err)
				continue
			}
			runs = append(runs, &propagationRun{Signal: sig, Result: result, SeedPath: cp})
		}

		allSeedResults = append(allSeedResults, seedRunResult{
			SeedPath:    cp,
			SeedLabel:   orgLabel,
			NewsSummary: newsSummary,
			Signals:     signals,
			Runs:        runs,
		})
	}

	if len(allSeedResults) == 0 {
		log.Fatal("No seed results to report")
	}

	printMultiNodeReport(ctx, frRepo, allSeedResults, *simulate)
}

func buildNewsAndSignalsPrompt(orgLabel, canonicalPath string) string {
	return fmt.Sprintf(`You are a signal extraction agent for a risk analysis system. Use Google Search to find RECENT news (last 1-3 months) about this organization:

ORGANIZATION: %s
Canonical path: %s

TASK:
1. Search for and summarize recent news about this organization (risks, opportunities, disruptions, regulatory changes, supply chain, etc.)
2. Extract 1-4 signals that represent quasi-physical impulses from these events. Each signal MUST map to exactly one of the 12-axis basis.

12-AXIS BASIS:
1. legal/access - Legal permission to operate/transact (licenses, sanctions, bans)
2. legal/compliance - Secondary exposure risk from association with sanctioned/regulated entities
3. financial/settlement - Payment rails, banking access, clearing, FX (ability to move money)
4. financial/credit - Credit availability, counterparty risk, default exposure
5. physical_flow/supply - Physical availability of goods/materials (shortage or glut)
6. physical_flow/demand - Demand changes (surge or collapse)
7. logistics/capacity - Transportation/infrastructure capacity (routes, ports, airspace)
8. logistics/latency - Delays, lead time increases (not blockage, but slowdown)
9. operational/continuity - Ability to operate (strikes, failures, disasters)
10. info/uncertainty - Ambiguity of rules, timelines, exceptions (regulatory fog)
11. info/reputation - Public perception, ESG signals, brand impact
12. geo/conflict - Geopolitical instability, conflict zones, diplomatic friction

OUTPUT FORMAT (strict JSON only, no markdown):
{
  "newsSummary": "Brief 2-4 sentence summary of the most relevant news found",
  "signals": [
    {
      "axis": "one of the 12 axes above",
      "mechanism": "optional specific mechanism (e.g., 'sanctions', 'airspace_closure')",
      "amplitude": 0.0-1.0,
      "polarity": "risk" | "opportunity",
      "seedPaths": ["%s"],
      "confidence": 0.0-1.0
    }
  ]
}

RULES:
- seedPaths must be exactly ["%s"] (the org we are calibrating)
- Amplitude: 0.0 = weak, 1.0 = maximum
- Return ONLY valid JSON, no markdown code fences`,
		orgLabel, canonicalPath, canonicalPath, canonicalPath)
}

func parseSignalsFromResponse(content, seedPath string) (newsSummary string, signals []*models.SignalDescriptor) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var parsed struct {
		NewsSummary string `json:"newsSummary"`
		Signals     []struct {
			Axis       string   `json:"axis"`
			Mechanism  string   `json:"mechanism"`
			Amplitude  float64  `json:"amplitude"`
			Polarity   string   `json:"polarity"`
			SeedPaths  []string `json:"seedPaths"`
			Confidence float64  `json:"confidence"`
		} `json:"signals"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		log.Printf("Failed to parse LLM JSON: %v", err)
		return "", nil
	}

	newsSummary = strings.TrimSpace(parsed.NewsSummary)
	var out []*models.SignalDescriptor
	for _, s := range parsed.Signals {
		if !models.IsValidAxis(s.Axis) {
			continue
		}
		if s.Polarity != "risk" && s.Polarity != "opportunity" {
			s.Polarity = "risk"
		}
		if s.Amplitude < 0 || s.Amplitude > 1 {
			s.Amplitude = 0.5
		}
		sp := s.SeedPaths
		if len(sp) == 0 {
			sp = []string{seedPath}
		}
		sig := &models.SignalDescriptor{
			ID:         "cal-" + uuid.New().String()[:8],
			Axis:       s.Axis,
			Mechanism:  strings.TrimSpace(s.Mechanism),
			Amplitude:  s.Amplitude,
			Polarity:   s.Polarity,
			SeedPaths:  sp,
			Confidence: s.Confidence,
			CreatedAt:  time.Now().UTC(),
			ObservedAt: time.Now().UTC(),
		}
		if err := sig.Validate(); err != nil {
			if *verbose {
				log.Printf("Skip invalid signal: %v", err)
			}
			continue
		}
		out = append(out, sig)
	}
	return newsSummary, out
}

func printReport(ctx context.Context, frRepo *repo.HorizonFragmentsRepository, orgLabel, path, newsSummary string, _ []*models.SignalDescriptor, runs []*propagationRun, simulated bool) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Printf("  PROPAGATION CALIBRATION REPORT — %s\n", orgLabel)
	fmt.Printf("  Path: %s  |  Mode: %s\n", path, map[bool]string{true: "SIMULATION", false: "PERSIST"}[simulated])
	if newsSummary != "" {
		fmt.Printf("  News: %s\n", truncate(newsSummary, 120))
	}
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()

	// Accumulated view: which nodes got which metrics (deduped in core by node, seed, axis, polarity)
	pairs := make([]struct {
		Accumulated map[string][]propagation.AccumulatedSignal
		SeedPath    string
	}, 0, len(runs))
	for _, r := range runs {
		if r.Result == nil || r.Result.Accumulated == nil {
			continue
		}
		pairs = append(pairs, struct {
			Accumulated map[string][]propagation.AccumulatedSignal
			SeedPath    string
		}{Accumulated: r.Result.Accumulated, SeedPath: path})
	}
	merged := propagation.MergeAccumulatedWithSeedDedup(pairs)
	accByFragment := make(map[string][]accumEntry)
	for fragID, accs := range merged {
		for _, a := range accs {
			accByFragment[fragID] = append(accByFragment[fragID], accumEntry{
				Axis: a.Axis, Polarity: a.Polarity, Amplitude: a.Amplitude, HopCount: a.HopCount,
			})
		}
	}

	// Resolve fragment IDs to paths for readability
	pathCache := make(map[string]string)
	for fragID := range accByFragment {
		pathCache[fragID] = fragID
	}
	for fragID := range accByFragment {
		frag, err := frRepo.Get(ctx, fragID)
		if err == nil && frag != nil {
			pathCache[fragID] = frag.CanonicalPath
		}
	}

	// Sort by path for stable output
	var fragIDs []string
	for id := range accByFragment {
		fragIDs = append(fragIDs, id)
	}
	sort.Slice(fragIDs, func(i, j int) bool {
		return pathCache[fragIDs[i]] < pathCache[fragIDs[j]]
	})

	fmt.Println("── ACCUMULATED METRICS BY NODE (after propagation) ──")
	fmt.Println()
	for _, fragID := range fragIDs {
		entries := accByFragment[fragID]
		canonPath := pathCache[fragID]
		fmt.Printf("  %s\n", canonPath)
		for _, e := range entries {
			fmt.Printf("    %s %s amp=%.3f hops=%d\n", e.Polarity, e.Axis, e.Amplitude, e.HopCount)
		}
		fmt.Println()
	}

	fmt.Println("── PROPAGATION TRACE (first 30 events) ──")
	fmt.Println()
	traceCount := 0
	for _, r := range runs {
		if r.Result == nil || len(r.Result.Trace) == 0 {
			continue
		}
		for _, ev := range r.Result.Trace {
			if traceCount >= 30 {
				break
			}
			if ev.Outcome == "seed" {
				fmt.Printf("  [%s] SEED -> %s axis=%s amp=%.3f\n", r.Signal.Axis, ev.ToFragment, ev.AxisOut, ev.AmplitudeOut)
			} else if ev.Outcome == "propagated" {
				fmt.Printf("  [%s] %s -> %s edge=%s axis=%s->%s amp=%.3f->%.3f hops=%d\n",
					r.Signal.Axis, ev.FromFragment, ev.ToFragment, ev.EdgeType,
					ev.AxisIn, ev.AxisOut, ev.AmplitudeIn, ev.AmplitudeOut, ev.Hops)
			} else if ev.Outcome == "node_updated" {
				fmt.Printf("  [%s] UPDATED %s axis=%s amp=%.3f hops=%d\n", r.Signal.Axis, ev.ToFragment, ev.AxisOut, ev.AmplitudeOut, ev.Hops)
			} else if ev.Outcome == "dropped" && *verbose {
				fmt.Printf("  [%s] DROPPED %s reason=%s\n", r.Signal.Axis, ev.ToFragment, ev.Reason)
			}
			traceCount++
		}
	}
	if traceCount == 0 {
		fmt.Println("  (no trace events)")
	}
	fmt.Println()

	fmt.Println("── SUMMARY BY SIGNAL ──")
	fmt.Println()
	for i, r := range runs {
		if r.Result == nil {
			continue
		}
		sig := r.Signal
		fmt.Printf("  Signal %d: %s %s amp=%.3f\n", i+1, sig.Polarity, sig.Axis, sig.Amplitude)
		fmt.Printf("    Affected: %d nodes  Paths: %d  Time: %s  Dropped: %d\n",
			len(r.Result.AffectedFragments), len(r.Result.PropagationPaths),
			r.Result.PropagationTime.Round(time.Millisecond), r.Result.TraceDropped)
		fmt.Println()
	}

	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  Use this output to evaluate: where did signals go? where are obvious errors?")
	fmt.Println("  Tune propagation logic (damping, edge types, transformations) accordingly.")
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
}

type accumEntry struct {
	Axis      string
	Polarity  string
	Amplitude float64
	HopCount  int
}

type accumEntryWithSeed struct {
	accumEntry
	SeedPath string
}

type aggResult struct {
	Axis        string
	Polarity    string
	AggAmplitude float64
	MinHops     int
}

// findConflictNodes returns fragment IDs where same axis has risk from one seed and opportunity from another.
func findConflictNodes(accByFragment map[string][]accumEntryWithSeed) []string {
	var out []string
	for fragID, entries := range accByFragment {
		axisPolSeeds := make(map[string]map[string]map[string]bool) // axis -> polarity -> seed -> true
		for _, e := range entries {
			if axisPolSeeds[e.Axis] == nil {
				axisPolSeeds[e.Axis] = make(map[string]map[string]bool)
			}
			if axisPolSeeds[e.Axis][e.Polarity] == nil {
				axisPolSeeds[e.Axis][e.Polarity] = make(map[string]bool)
			}
			axisPolSeeds[e.Axis][e.Polarity][e.SeedPath] = true
		}
		for _, polSeeds := range axisPolSeeds {
			riskSeeds := polSeeds["risk"]
			oppSeeds := polSeeds["opportunity"]
			if len(riskSeeds) == 0 || len(oppSeeds) == 0 {
				continue
			}
			// Conflict only if from different seeds
			for rs := range riskSeeds {
				for os := range oppSeeds {
					if rs != os {
						out = append(out, fragID)
						goto nextFrag
					}
				}
			}
		nextFrag:
		}
	}
	sort.Strings(out)
	return out
}

// aggregateByAxisPolarity aggregates entries by (axis, polarity). Uses aggMode: max, sum, or hop_weighted.
// hop_weighted: weight = 1/(1+0.2*hops), take max of weighted amplitudes (direct signals count more).
func aggregateByAxisPolarity(entries []accumEntryWithSeed, aggMode string) []aggResult {
	keyed := make(map[string]*aggResult) // axis|polarity -> result
	for _, e := range entries {
		key := e.Axis + "|" + e.Polarity
		r, ok := keyed[key]
		if !ok {
			keyed[key] = &aggResult{Axis: e.Axis, Polarity: e.Polarity, MinHops: e.HopCount}
			r = keyed[key]
		}
		if e.HopCount < r.MinHops {
			r.MinHops = e.HopCount
		}
		switch aggMode {
		case "max":
			if e.Amplitude > r.AggAmplitude {
				r.AggAmplitude = e.Amplitude
			}
		case "sum":
			r.AggAmplitude += e.Amplitude
		case "hop_weighted":
			weight := 1.0 / (1.0 + float64(e.HopCount)*0.2)
			weighted := e.Amplitude * weight
			if weighted > r.AggAmplitude {
				r.AggAmplitude = weighted
			}
		default:
			if e.Amplitude > r.AggAmplitude {
				r.AggAmplitude = e.Amplitude
			}
		}
	}
	// Clamp sum to 1.0 for readability
	for _, r := range keyed {
		if r.AggAmplitude > 1.0 && aggMode == "sum" {
			r.AggAmplitude = 1.0
		}
	}
	var out []aggResult
	for _, r := range keyed {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Axis != out[j].Axis {
			return out[i].Axis < out[j].Axis
		}
		return out[i].Polarity < out[j].Polarity
	})
	return out
}

func keys(m map[string]bool) []string {
	var s []string
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}


func printMultiNodeReport(ctx context.Context, frRepo *repo.HorizonFragmentsRepository, seedResults []seedRunResult, simulated bool) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  MULTI-NODE CALIBRATION — INTERFERENCE REPORT")
	fmt.Printf("  Seeds: %d  |  Mode: %s  |  Agg: %s\n", len(seedResults), map[bool]string{true: "SIMULATION", false: "PERSIST"}[simulated], *aggMode)
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()

	// Build: fragmentID -> []accumEntryWithSeed (deduped in core by node, seed, axis, polarity)
	pairs := make([]struct {
		Accumulated map[string][]propagation.AccumulatedSignal
		SeedPath    string
	}, 0)
	for _, sr := range seedResults {
		for _, r := range sr.Runs {
			if r.Result == nil || r.Result.Accumulated == nil {
				continue
			}
			pairs = append(pairs, struct {
				Accumulated map[string][]propagation.AccumulatedSignal
				SeedPath    string
			}{Accumulated: r.Result.Accumulated, SeedPath: r.SeedPath})
		}
	}
	merged := propagation.MergeAccumulatedWithSeedDedup(pairs)
	accByFragment := make(map[string][]accumEntryWithSeed)
	for fragID, accs := range merged {
		for _, a := range accs {
			accByFragment[fragID] = append(accByFragment[fragID], accumEntryWithSeed{
				accumEntry: accumEntry{Axis: a.Axis, Polarity: a.Polarity, Amplitude: a.Amplitude, HopCount: a.HopCount},
				SeedPath:   a.SeedPath,
			})
		}
	}

	// Resolve paths
	pathCache := make(map[string]string)
	for fragID := range accByFragment {
		pathCache[fragID] = fragID
	}
	for fragID := range accByFragment {
		if frag, err := frRepo.Get(ctx, fragID); err == nil && frag != nil {
			pathCache[fragID] = frag.CanonicalPath
		}
	}

	// INTERFERENCE: nodes hit by 2+ different seeds
	seedsByFragment := make(map[string]map[string]bool)
	degreeByFragment := make(map[string]int)
	for fragID, entries := range accByFragment {
		seeds := make(map[string]bool)
		for _, e := range entries {
			seeds[e.SeedPath] = true
		}
		degreeByFragment[fragID] = len(seeds)
		if len(seeds) >= 2 {
			seedsByFragment[fragID] = seeds
		}
	}

	var interferenceIDs []string
	for id := range seedsByFragment {
		interferenceIDs = append(interferenceIDs, id)
	}
	sort.Slice(interferenceIDs, func(i, j int) bool {
		return pathCache[interferenceIDs[i]] < pathCache[interferenceIDs[j]]
	})

	// ── INTERFERENCE DEGREE SUMMARY ──
	degreeCount := make(map[int]int)
	for _, d := range degreeByFragment {
		degreeCount[d]++
	}
	var degrees []int
	for d := range degreeCount {
		degrees = append(degrees, d)
	}
	sort.Ints(degrees)
	fmt.Println("── INTERFERENCE DEGREE (seeds reaching each node) ──")
	for _, d := range degrees {
		fmt.Printf("  %d seeds → %d nodes\n", d, degreeCount[d])
	}
	fmt.Println()

	// ── CONFLICTS: same axis, risk + opportunity from different seeds ──
	conflictNodes := findConflictNodes(accByFragment)
	fmt.Println("── CONFLICTS (same axis, risk + opportunity from different seeds) ──")
	if len(conflictNodes) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, fragID := range conflictNodes {
			entries := accByFragment[fragID]
			canonPath := pathCache[fragID]
			axisPolSeeds := make(map[string]map[string]map[string]bool) // axis -> polarity -> seed -> true
			for _, e := range entries {
				if axisPolSeeds[e.Axis] == nil {
					axisPolSeeds[e.Axis] = make(map[string]map[string]bool)
				}
				if axisPolSeeds[e.Axis][e.Polarity] == nil {
					axisPolSeeds[e.Axis][e.Polarity] = make(map[string]bool)
				}
				axisPolSeeds[e.Axis][e.Polarity][e.SeedPath] = true
			}
			fmt.Printf("  %s\n", canonPath)
			for axis, polSeeds := range axisPolSeeds {
				riskSeeds := keys(polSeeds["risk"])
				oppSeeds := keys(polSeeds["opportunity"])
				if len(riskSeeds) > 0 && len(oppSeeds) > 0 {
					fmt.Printf("    %s: risk from %v, opportunity from %v\n", axis, riskSeeds, oppSeeds)
				}
			}
		}
	}
	fmt.Println()

	// ── AGGREGATED BY NODE (interference nodes, hop-weighted) ──
	fmt.Println("── AGGREGATED BY NODE (interference nodes, method=" + *aggMode + ") ──")
	fmt.Println()
	if len(interferenceIDs) == 0 {
		fmt.Println("  (no overlap)")
	} else {
		for _, fragID := range interferenceIDs {
			entries := accByFragment[fragID]
			canonPath := pathCache[fragID]
			agg := aggregateByAxisPolarity(entries, *aggMode)
			fmt.Printf("  %s\n", canonPath)
			fmt.Printf("    degree=%d\n", degreeByFragment[fragID])
			for _, a := range agg {
				direct := ""
				if a.MinHops <= 2 {
					direct = " [direct]"
				} else {
					direct = " [indirect]"
				}
				fmt.Printf("    %s %s agg=%.3f minHops=%d%s\n", a.Polarity, a.Axis, a.AggAmplitude, a.MinHops, direct)
			}
			fmt.Println()
		}
	}

	// ── RAW INTERFERENCE (per-seed breakdown) ──
	fmt.Println("── RAW: nodes receiving signals from 2+ seeds (per-seed breakdown) ──")
	fmt.Println()
	if len(interferenceIDs) == 0 {
		fmt.Println("  (no overlap)")
	} else {
		for _, fragID := range interferenceIDs {
			entries := accByFragment[fragID]
			canonPath := pathCache[fragID]
			seeds := seedsByFragment[fragID]
			seedList := make([]string, 0, len(seeds))
			for s := range seeds {
				seedList = append(seedList, s)
			}
			sort.Strings(seedList)
			fmt.Printf("  %s\n", canonPath)
			fmt.Printf("    Seeds: %s\n", strings.Join(seedList, ", "))
			bySeed := make(map[string][]accumEntryWithSeed)
			for _, e := range entries {
				bySeed[e.SeedPath] = append(bySeed[e.SeedPath], e)
			}
			for _, seed := range seedList {
				for _, e := range bySeed[seed] {
					fmt.Printf("    [%s] %s %s amp=%.3f hops=%d\n", truncate(seed, 35), e.Polarity, e.Axis, e.Amplitude, e.HopCount)
				}
			}
			fmt.Println()
		}
	}

	fmt.Println("── PER-SEED SUMMARY ──")
	fmt.Println()
	for _, sr := range seedResults {
		fmt.Printf("  %s (%s)\n", sr.SeedPath, sr.SeedLabel)
		if sr.NewsSummary != "" {
			fmt.Printf("    News: %s\n", truncate(sr.NewsSummary, 100))
		}
		totalAffected := make(map[string]bool)
		for _, r := range sr.Runs {
			if r.Result != nil {
				for _, fid := range r.Result.AffectedFragments {
					totalAffected[fid] = true
				}
			}
		}
		fmt.Printf("    Signals: %d  |  Unique affected nodes: %d\n", len(sr.Runs), len(totalAffected))
		fmt.Println()
	}

	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  Evaluate: do overlapping signals make sense? Any conflicts or noise?")
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
}

func runListCandidates(ctx context.Context, frRepo *repo.HorizonFragmentsRepository, _ *repo.DependencyEdgesRepository, fs *firestore.Client) {
	type orgDegree struct {
		ID       string
		Path     string
		Label    string
		OutEdges int
		InEdges  int
		Total    int
	}

	// Cap limit at 200
	outLimit := *limit
	if outLimit > 200 {
		outLimit = 200
	}
	if outLimit < 1 {
		outLimit = 200
	}

	// Step 1: Scan edges once, aggregate degree per fragment (no per-org queries)
	degree := make(map[string]int)
	edgesCol := fs.Collection("horizon_dependency_edges")
	q := edgesCol.Where("tenantId", "==", tenantID).
		OrderBy(firestore.DocumentID, firestore.Asc).
		Limit(*maxEdges)

	iter := q.Documents(ctx)
	edgesScanned := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("edge scan failed: %v", err)
		}
		edgesScanned++

		var edge models.DependencyEdge
		if err := doc.DataTo(&edge); err != nil {
			continue
		}
		if edge.SourceFragmentID != "" {
			degree[edge.SourceFragmentID]++
		}
		if edge.TargetFragmentID != "" {
			degree[edge.TargetFragmentID]++
		}
	}

	// Step 2: Sort fragment IDs by degree, take top candidates (buffer for non-orgs)
	type fragDeg struct {
		id   string
		deg  int
	}
	candidates := make([]fragDeg, 0, len(degree))
	for id, d := range degree {
		if d > 0 {
			candidates = append(candidates, fragDeg{id: id, deg: d})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].deg > candidates[j].deg })

	// Take top 2*outLimit to have buffer (many may not be orgs)
	fetchN := outLimit * 2
	if fetchN > len(candidates) {
		fetchN = len(candidates)
	}
	topIDs := make([]string, 0, fetchN)
	degByID := make(map[string]int, fetchN)
	for i := 0; i < fetchN; i++ {
		topIDs = append(topIDs, candidates[i].id)
		degByID[candidates[i].id] = candidates[i].deg
	}

	// Step 3: Batch fetch fragments, filter to orgs only
	orgs := make([]orgDegree, 0, outLimit)
	fragMap, err := frRepo.GetMany(ctx, topIDs)
	if err != nil {
		log.Fatalf("GetMany fragments failed: %v", err)
	}

	for _, id := range topIDs {
		f := fragMap[id]
		if f == nil || f.Type != models.FragmentTypeOrganization {
			continue
		}
		status := strings.TrimSpace(f.Governance.Status)
		if status == "" {
			status = strings.TrimSpace(f.Status)
		}
		if strings.ToLower(status) != "active" {
			continue
		}
		total := degByID[id]
		// We don't have out/in split from edge scan; use total for both display
		label := f.Labels["en"]
		if label == "" {
			label = f.Identifiers["legalName"]
		}
		if label == "" {
			label = f.Identifiers["shortName"]
		}
		if label == "" {
			label = f.CanonicalPath
		}
		orgs = append(orgs, orgDegree{
			ID:       f.ID,
			Path:     f.CanonicalPath,
			Label:    label,
			OutEdges: total / 2, // approximate
			InEdges:  total - total/2,
			Total:    total,
		})
		if len(orgs) >= outLimit {
			break
		}
	}

	fmt.Printf("Scanned %d edges (one pass). Top %d orgs by degree:\n\n", edgesScanned, len(orgs))
	fmt.Printf("%-50s %8s %8s %8s  %s\n", "Path", "Out~", "In~", "Total", "Label")
	fmt.Println(strings.Repeat("-", 120))
	for _, o := range orgs {
		fmt.Printf("%-50s %8d %8d %8d  %s\n", truncate(o.Path, 50), o.OutEdges, o.InEdges, o.Total, truncate(o.Label, 40))
	}
	fmt.Println()
	fmt.Println("Run calibration with: -path=/org/...")
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func loadGeminiAPIKey(ctx context.Context) string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	v := gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GEMINI_API_KEY", "gemini_api_key")
	if strings.TrimSpace(v) == "" {
		log.Fatal("GEMINI_API_KEY required (env or Secret Manager)")
	}
	return v
}

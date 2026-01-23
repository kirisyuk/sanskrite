package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")

	mode = flag.String("mode", "export", "Mode: export (write JSON batches) or apply (load JSON and update DB)")

	fragmentPageSize = flag.Int("fragment-page-size", 500, "Page size for fragment pagination")
	edgePageSize     = flag.Int("edge-page-size", 1000, "Page size for edge pagination")
	fragmentLimit    = flag.Int("fragment-limit", 0, "Limit number of fragments to process (0 = all)")
	edgeLimit        = flag.Int("edge-limit", 0, "Limit number of edges to load (0 = all)")
	startAfter       = flag.String("start-after", "", "Resume fragment pagination after this document ID")
	edgeStartAfter   = flag.String("edge-start-after", "", "Resume edge pagination after this document ID")

	dryRun = flag.Bool("dry-run", true, "Dry run mode (apply only): only report would-change counts (no writes)")
	force  = flag.Bool("force", false, "Force recompute even if highProfile exists")

	maxEvidenceK = flag.Int("max-evidence-k", 6, "Max evidence items stored per signal")

	updateQPS      = flag.Float64("update-qps", 15, "Max updates per second (0 = no throttle)")
	sleepPerPageMs = flag.Int("sleep-per-page-ms", 100, "Sleep between fragment pages (ms)")
	edgeSleepMs    = flag.Int("edge-sleep-per-page-ms", 50, "Sleep between edge pages (ms)")

	outDir    = flag.String("out-dir", "./tmp/highprofile_batches", "Output directory for JSON batches (export mode)")
	batchSize = flag.Int("batch-size", 500, "Records per JSON batch (export mode)")
	inDir     = flag.String("in-dir", "./tmp/highprofile_batches", "Input directory for JSON batches (apply mode)")
	applyStartBatch = flag.Int("apply-start-batch", 1, "Apply mode: start from batch number (1-based)")
)

type stats struct {
	FragmentsLoaded int
	EdgesLoaded     int
	Scanned         int
	WouldWrite      int
	Wrote           int
	Skipped         int
	Errors          int
}

type batchRecord struct {
	FragmentID    string              `json:"fragmentId"`
	CanonicalPath string              `json:"canonicalPath,omitempty"`
	HighProfile   *models.HighProfile `json:"highProfile"`
}

type batchFile struct {
	GeneratedAt string        `json:"generatedAt"`
	ProjectID   string        `json:"projectId"`
	Records     []batchRecord `json:"records"`
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *fragmentPageSize <= 0 {
		*fragmentPageSize = 500
	}
	if *edgePageSize <= 0 {
		*edgePageSize = 1000
	}

	modeVal := strings.ToLower(strings.TrimSpace(*mode))
	if modeVal != "export" && modeVal != "apply" {
		log.Fatalf("Invalid --mode=%q (use export or apply)", modeVal)
	}

	var st stats
	ctx := context.Background()

	fsClient, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer func() { _ = fsClient.Close() }()

	frRepo := hrepo.NewHorizonFragmentsRepository(fsClient)

	if modeVal == "apply" {
		log.Printf("Starting Horizon highProfile APPLY (project=%s dry-run=%v in-dir=%s)...",
			strings.TrimSpace(*projectID), *dryRun, strings.TrimSpace(*inDir))
		applyBatches(ctx, frRepo, &st)
		printSummary(&st, *dryRun)
		return
	}

	log.Printf("Starting Horizon highProfile EXPORT (project=%s out-dir=%s batch-size=%d force=%v)...",
		strings.TrimSpace(*projectID), strings.TrimSpace(*outDir), *batchSize, *force)

	// --- Phase 1: Load all edges into memory ---
	log.Println("Phase 1: Loading all edges into memory...")
	edgesBySource := make(map[string][]*models.DependencyEdge)
	edgesByTarget := make(map[string][]*models.DependencyEdge)
	{
		edgeCol := fsClient.Collection("horizon_dependency_edges")
		lastEdgeID := strings.TrimSpace(*edgeStartAfter)
		for {
			q := edgeCol.Where("tenantId", "==", "global").OrderBy(firestore.DocumentID, firestore.Asc).Limit(*edgePageSize)
			if lastEdgeID != "" {
				q = q.StartAfter(lastEdgeID)
			}
			iter := q.Documents(ctx)
			count := 0
			for {
				doc, err := iter.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					log.Printf("WARNING: edge iteration error: %v", err)
					break
				}
				var edge models.DependencyEdge
				if err := doc.DataTo(&edge); err != nil {
					continue
				}
				if strings.TrimSpace(edge.ID) == "" {
					edge.ID = doc.Ref.ID
				}
				src := strings.TrimSpace(edge.SourceFragmentID)
				tgt := strings.TrimSpace(edge.TargetFragmentID)
				if src != "" {
					edgesBySource[src] = append(edgesBySource[src], &edge)
				}
				if tgt != "" {
					edgesByTarget[tgt] = append(edgesByTarget[tgt], &edge)
				}
				st.EdgesLoaded++
				lastEdgeID = doc.Ref.ID
				count++

				if *edgeLimit > 0 && st.EdgesLoaded >= *edgeLimit {
					break
				}
			}
			iter.Stop()
			if count == 0 {
				break
			}
			if *edgeLimit > 0 && st.EdgesLoaded >= *edgeLimit {
				break
			}
			log.Printf("  Edges loaded: %d", st.EdgesLoaded)
			if *edgeSleepMs > 0 {
				time.Sleep(time.Duration(*edgeSleepMs) * time.Millisecond)
			}
		}
	}
	log.Printf("Phase 1 done: %d edges loaded, %d unique sources, %d unique targets",
		st.EdgesLoaded, len(edgesBySource), len(edgesByTarget))

	// --- Phase 2: Load all fragments into memory ---
	log.Println("Phase 2: Loading all fragments into memory...")
	fragmentByID := make(map[string]*models.HorizonFragment)
	var fragmentIDs []string
	{
		fragCol := fsClient.Collection("horizon_fragments")
		lastFragID := strings.TrimSpace(*startAfter)
		for {
			q := fragCol.Where("tenantId", "==", "global").OrderBy(firestore.DocumentID, firestore.Asc).Limit(*fragmentPageSize)
			if lastFragID != "" {
				q = q.StartAfter(lastFragID)
			}
			iter := q.Documents(ctx)
			count := 0
			for {
				doc, err := iter.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					log.Printf("WARNING: fragment iteration error: %v", err)
					break
				}
				var frag models.HorizonFragment
				if err := doc.DataTo(&frag); err != nil {
					continue
				}
				if strings.TrimSpace(frag.ID) == "" {
					frag.ID = doc.Ref.ID
				}
				fragmentByID[frag.ID] = &frag
				fragmentIDs = append(fragmentIDs, frag.ID)
				st.FragmentsLoaded++
				lastFragID = doc.Ref.ID
				count++

				if *fragmentLimit > 0 && st.FragmentsLoaded >= *fragmentLimit {
					break
				}
			}
			iter.Stop()
			if count == 0 || (*fragmentLimit > 0 && st.FragmentsLoaded >= *fragmentLimit) {
				break
			}
			log.Printf("  Fragments loaded: %d", st.FragmentsLoaded)
			if *sleepPerPageMs > 0 {
				time.Sleep(time.Duration(*sleepPerPageMs) * time.Millisecond)
			}
		}
	}
	log.Printf("Phase 2 done: %d fragments loaded", st.FragmentsLoaded)

	// --- Phase 3: Compute highProfile for each fragment using in-memory data ---
	log.Println("Phase 3: Computing highProfile signals...")

	if err := os.MkdirAll(strings.TrimSpace(*outDir), 0o755); err != nil {
		log.Fatalf("Failed to create out-dir: %v", err)
	}

	batchIdx := 1
	batchRecords := make([]batchRecord, 0, max(1, *batchSize))

	for i, fragID := range fragmentIDs {
		frag := fragmentByID[fragID]
		if frag == nil {
			continue
		}
		st.Scanned++

		// Skip if already has highProfile and not forcing.
		if !*force && frag.HighProfile != nil && len(frag.HighProfile.Signals) > 0 {
			st.Skipped++
			continue
		}

		// Compute using in-memory edges.
		outEdges := edgesBySource[frag.ID]
		inEdges := edgesByTarget[frag.ID]

		// Gather neighbor fragments for evidence (in-memory).
		neighborByID := make(map[string]*models.HorizonFragment)
		for _, e := range outEdges {
			if e != nil && strings.TrimSpace(e.TargetFragmentID) != "" {
				if n := fragmentByID[strings.TrimSpace(e.TargetFragmentID)]; n != nil {
					neighborByID[n.ID] = n
				}
			}
		}
		for _, e := range inEdges {
			if e != nil && strings.TrimSpace(e.SourceFragmentID) != "" {
				if n := fragmentByID[strings.TrimSpace(e.SourceFragmentID)]; n != nil {
					neighborByID[n.ID] = n
				}
			}
		}

		desired := computeHighProfile(frag, inEdges, outEdges, neighborByID, *maxEvidenceK)

		// Compare for dry-run accuracy.
		if reflect.DeepEqual(frag.HighProfile, desired) {
			st.Skipped++
			continue
		}

		st.WouldWrite++
		batchRecords = append(batchRecords, batchRecord{
			FragmentID:    frag.ID,
			CanonicalPath: strings.TrimSpace(frag.CanonicalPath),
			HighProfile:   desired,
		})

		if len(batchRecords) >= max(1, *batchSize) {
			if err := writeBatchFile(*outDir, batchIdx, *projectID, batchRecords); err != nil {
				st.Errors++
				log.Printf("ERROR: failed to write batch %d: %v", batchIdx, err)
			}
			batchIdx++
			batchRecords = make([]batchRecord, 0, max(1, *batchSize))
		}

		if (i+1)%500 == 0 {
			log.Printf("Progress: scanned=%d wouldWrite=%d wrote=%d skipped=%d errors=%d",
				st.Scanned, st.WouldWrite, st.Wrote, st.Skipped, st.Errors)
		}
	}

	if len(batchRecords) > 0 {
		if err := writeBatchFile(*outDir, batchIdx, *projectID, batchRecords); err != nil {
			st.Errors++
			log.Printf("ERROR: failed to write batch %d: %v", batchIdx, err)
		}
	}

	printSummary(&st, false)
}

// -----------------------------------------------------------------------------
// In-memory highProfile computation (mirrors highprofile.Calculator logic)
// -----------------------------------------------------------------------------

func computeHighProfile(
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
	maxEvidenceK int,
) *models.HighProfile {
	if frag == nil {
		return nil
	}

	now := time.Now().UTC()
	hp := &models.HighProfile{
		Version:   1,
		UpdatedAt: now,
		Signals:   map[string]models.HighProfileSignal{},
	}

	computeDynastySignals(hp, frag, inEdges, outEdges, neighborByID, maxEvidenceK)
	computeRegulatorySignals(hp, frag, inEdges, outEdges, neighborByID, maxEvidenceK)
	computeMarketSignals(hp, frag, inEdges, outEdges, neighborByID, maxEvidenceK)
	computeChokepointSignals(hp, frag, inEdges, outEdges, neighborByID, maxEvidenceK)

	if len(hp.Signals) == 0 {
		return nil
	}
	return hp
}

// --- Dynasty signals ---

func computeDynastySignals(
	hp *models.HighProfile,
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
	maxEvidenceK int,
) {
	if hp == nil || frag == nil {
		return
	}

	isDynastyFragment := frag.Type == models.FragmentTypeCommunity &&
		strings.EqualFold(strings.TrimSpace(frag.CommunityType), "dynasty")

	if isDynastyFragment {
		// dynasty_power: how much this dynasty controls/owns/regulates
		best := 0.0
		ev := make([]scoredEdge, 0)
		for _, e := range outEdges {
			if e == nil {
				continue
			}
			switch e.Type {
			case models.DependencyTypeControls, models.DependencyTypeOwns, models.DependencyTypeRegulates, models.DependencyTypeAdministers:
				// ok
			default:
				continue
			}
			tgt := neighborByID[strings.TrimSpace(e.TargetFragmentID)]
			tp := ""
			if tgt != nil {
				tp = strings.TrimSpace(tgt.CanonicalPath)
			}
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: tp})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, maxEvidenceK)
			hp.Signals["dynasty_power"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:       edgeIDs,
					NeighborPaths: neighborPaths,
					Note:          "Dynasty directly controls/owns/regulates other entities.",
				},
			}
		}
	}

	// dynasty_influence: fragment is controlled/owned by a dynasty
	{
		best := 0.0
		ev := make([]scoredEdge, 0)
		for _, e := range inEdges {
			if e == nil {
				continue
			}
			switch e.Type {
			case models.DependencyTypeControls, models.DependencyTypeOwns, models.DependencyTypeAdministers:
				// ok
			default:
				continue
			}
			src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
			if src == nil {
				continue
			}
			if src.Type != models.FragmentTypeCommunity {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(src.CommunityType), "dynasty") {
				continue
			}
			sp := strings.TrimSpace(src.CanonicalPath)
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: sp})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, maxEvidenceK)
			hp.Signals["dynasty_influence"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:       edgeIDs,
					NeighborPaths: neighborPaths,
					Note:          "Entity is controlled/owned by a dynasty community.",
				},
			}
		}
	}
}

// --- Regulatory signals ---

func computeRegulatorySignals(
	hp *models.HighProfile,
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
	maxEvidenceK int,
) {
	if hp == nil || frag == nil {
		return
	}
	_ = outEdges // kept for signature stability

	isGov := strings.HasPrefix(strings.ToLower(strings.TrimSpace(frag.CanonicalPath)), "/gov/")

	// is_regulator: /gov/* fragment that regulates others
	if isGov {
		best := 0.0
		ev := make([]scoredEdge, 0)
		for _, e := range outEdges {
			if e == nil || e.Type != models.DependencyTypeRegulates {
				continue
			}
			tgt := neighborByID[strings.TrimSpace(e.TargetFragmentID)]
			tp := ""
			if tgt != nil {
				tp = strings.TrimSpace(tgt.CanonicalPath)
			}
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: tp})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, maxEvidenceK)
			hp.Signals["is_regulator"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:       edgeIDs,
					NeighborPaths: neighborPaths,
					Note:          "Government entity that regulates other entities.",
				},
			}
		}
	}

	// regulated_exposure: fragment is regulated by gov entities
	{
		best := 0.0
		ev := make([]scoredEdge, 0)
		for _, e := range inEdges {
			if e == nil || e.Type != models.DependencyTypeRegulates {
				continue
			}
			src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
			sp := ""
			if src != nil {
				sp = strings.TrimSpace(src.CanonicalPath)
			}
			if src == nil || !strings.HasPrefix(strings.ToLower(sp), "/gov/") {
				continue
			}
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: sp})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, maxEvidenceK)
			hp.Signals["regulated_exposure"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:       edgeIDs,
					NeighborPaths: neighborPaths,
					Note:          "Entity is regulated by government bodies.",
				},
			}
		}
	}
}

// --- Market signals ---

func computeMarketSignals(
	hp *models.HighProfile,
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
	maxEvidenceK int,
) {
	if hp == nil || frag == nil {
		return
	}

	// market_dominance: org DOMINATES markets
	if frag.Type == models.FragmentTypeOrganization {
		best := 0.0
		ev := make([]scoredEdge, 0)
		for _, e := range outEdges {
			if e == nil || e.Type != models.DependencyTypeDominates {
				continue
			}
			tgt := neighborByID[strings.TrimSpace(e.TargetFragmentID)]
			if tgt == nil || tgt.Type != models.FragmentTypeMarket {
				continue
			}
			tp := strings.TrimSpace(tgt.CanonicalPath)
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: tp})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, maxEvidenceK)
			hp.Signals["market_dominance"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:       edgeIDs,
					NeighborPaths: neighborPaths,
					Note:          "Organization dominates one or more markets.",
				},
			}
		}
	}

	// has_dominant_players: market is dominated by orgs
	if frag.Type == models.FragmentTypeMarket {
		best := 0.0
		ev := make([]scoredEdge, 0)
		for _, e := range inEdges {
			if e == nil || e.Type != models.DependencyTypeDominates {
				continue
			}
			src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
			sp := ""
			if src != nil {
				sp = strings.TrimSpace(src.CanonicalPath)
			}
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: sp})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, maxEvidenceK)
			hp.Signals["has_dominant_players"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:       edgeIDs,
					NeighborPaths: neighborPaths,
					Note:          "Market has one or more dominant players (incoming DOMINATES).",
				},
			}
		}
	}
}

// --- Chokepoint signals ---

func computeChokepointSignals(
	hp *models.HighProfile,
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
	maxEvidenceK int,
) {
	if hp == nil || frag == nil {
		return
	}
	_ = outEdges // kept for signature stability

	// chokepoint_proxy: many high-criticality incoming supply/dependency edges
	count := 0
	bestCrit := 0.0
	ev := make([]scoredEdge, 0)
	for _, e := range inEdges {
		if e == nil {
			continue
		}
		switch e.Type {
		case models.DependencyTypeSuppliesTo, models.DependencyTypeDependsOn, models.DependencyTypeProduces,
			models.DependencyTypeTransformsInto, models.DependencyTypeExtractsFrom:
			// ok
		default:
			continue
		}
		if e.Properties.Criticality < 0.7 {
			continue
		}
		count++
		if e.Properties.Criticality > bestCrit {
			bestCrit = e.Properties.Criticality
		}
		src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
		sp := ""
		if src != nil {
			sp = strings.TrimSpace(src.CanonicalPath)
		}
		ev = append(ev, scoredEdge{edge: e, score: clamp01(e.Properties.Criticality), neighbor: sp})
	}

	if count > 0 {
		score := clamp01((float64(count) / 5.0) * clamp01(bestCrit))
		edgeIDs, neighborPaths := topKEvidence(ev, maxEvidenceK)
		hp.Signals["chokepoint_proxy"] = models.HighProfileSignal{
			Score:      score,
			Level:      levelFromScore(score),
			Confidence: clamp01(bestCrit),
			Evidence: &models.HighProfileEvidence{
				EdgeIDs:       edgeIDs,
				NeighborPaths: neighborPaths,
				Note:          "Proxy: many high-criticality incoming supply/dependency edges (potential chokepoint).",
			},
		}
	}

	// low_substitutability_dependency
	best := 0.0
	ev2 := make([]scoredEdge, 0)
	for _, e := range inEdges {
		if e == nil {
			continue
		}
		sub := e.GetSubstitutability()
		if sub <= 0 {
			continue
		}
		if sub > 0.2 {
			continue
		}
		if e.Properties.Criticality < 0.7 {
			continue
		}
		score := clamp01((1.0 - sub) * clamp01(e.Properties.Criticality))
		if score > best {
			best = score
		}
		src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
		sp := ""
		if src != nil {
			sp = strings.TrimSpace(src.CanonicalPath)
		}
		ev2 = append(ev2, scoredEdge{edge: e, score: score, neighbor: sp})
	}
	if best > 0 {
		edgeIDs, neighborPaths := topKEvidence(ev2, maxEvidenceK)
		hp.Signals["low_substitutability_dependency"] = models.HighProfileSignal{
			Score:      best,
			Level:      levelFromScore(best),
			Confidence: best,
			Evidence: &models.HighProfileEvidence{
				EdgeIDs:       edgeIDs,
				NeighborPaths: neighborPaths,
				Note:          "Has incoming dependencies with very low substitutability (hard to replace).",
			},
		}
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func levelFromScore(score float64) string {
	s := clamp01(score)
	switch {
	case s >= 0.85:
		return "critical"
	case s >= 0.70:
		return "high"
	case s >= 0.40:
		return "medium"
	case s > 0:
		return "low"
	default:
		return ""
	}
}

type scoredEdge struct {
	edge     *models.DependencyEdge
	score    float64
	neighbor string
}

func topKEvidence(in []scoredEdge, k int) (edgeIDs []string, neighborPaths []string) {
	if k <= 0 || len(in) == 0 {
		return nil, nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i].score > in[j].score })
	if len(in) > k {
		in = in[:k]
	}
	for _, se := range in {
		if se.edge != nil && strings.TrimSpace(se.edge.ID) != "" {
			edgeIDs = append(edgeIDs, strings.TrimSpace(se.edge.ID))
		}
		if strings.TrimSpace(se.neighbor) != "" {
			neighborPaths = append(neighborPaths, strings.TrimSpace(se.neighbor))
		}
	}
	return edgeIDs, neighborPaths
}

func writeBatchFile(outDir string, idx int, projectID string, records []batchRecord) error {
	if len(records) == 0 {
		return nil
	}
	payload := batchFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ProjectID:   strings.TrimSpace(projectID),
		Records:     records,
	}
	name := fmt.Sprintf("highprofile_batch_%04d.json", idx)
	path := filepath.Join(strings.TrimSpace(outDir), name)
	tmpPath := path + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func applyBatches(ctx context.Context, frRepo *hrepo.HorizonFragmentsRepository, st *stats) {
	if st == nil || frRepo == nil {
		return
	}
	files, err := filepath.Glob(filepath.Join(strings.TrimSpace(*inDir), "highprofile_batch_*.json"))
	if err != nil {
		log.Fatalf("Failed to list input files: %v", err)
	}
	if len(files) == 0 {
		log.Printf("No batch files found in %s", strings.TrimSpace(*inDir))
		return
	}
	sort.Strings(files)
	startBatch := *applyStartBatch
	if startBatch <= 0 {
		startBatch = 1
	}

	var tick <-chan time.Time
	if *updateQPS > 0 {
		interval := time.Duration(float64(time.Second) / *updateQPS)
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		tick = ticker.C
	}

	for _, path := range files {
		if batchIdx := parseBatchIndex(filepath.Base(path)); batchIdx > 0 && batchIdx < startBatch {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			st.Errors++
			log.Printf("ERROR: failed to read %s: %v", path, err)
			continue
		}
		var payload batchFile
		if err := json.Unmarshal(b, &payload); err != nil {
			st.Errors++
			log.Printf("ERROR: failed to parse %s: %v", path, err)
			continue
		}
		for _, rec := range payload.Records {
			if strings.TrimSpace(rec.FragmentID) == "" || rec.HighProfile == nil {
				st.Skipped++
				continue
			}
			st.Scanned++
			st.WouldWrite++
			if *dryRun {
				continue
			}
			if tick != nil {
				<-tick
			}
			wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := frRepo.UpdateHighProfile(wctx, strings.TrimSpace(rec.FragmentID), rec.HighProfile); err != nil {
				st.Errors++
				log.Printf("ERROR: update failed fragmentId=%s path=%s err=%v", rec.FragmentID, strings.TrimSpace(rec.CanonicalPath), err)
			} else {
				st.Wrote++
			}
			cancel()
		}
		log.Printf("Applied batch: %s (scanned=%d wrote=%d skipped=%d errors=%d)", filepath.Base(path), st.Scanned, st.Wrote, st.Skipped, st.Errors)
	}
}

func printSummary(st *stats, dry bool) {
	if st == nil {
		return
	}
	log.Println("\n=== Backfill Summary ===")
	log.Printf("Fragments loaded: %d", st.FragmentsLoaded)
	log.Printf("Edges loaded:     %d", st.EdgesLoaded)
	log.Printf("Scanned:          %d", st.Scanned)
	log.Printf("WouldWrite:       %d", st.WouldWrite)
	log.Printf("Wrote:            %d", st.Wrote)
	log.Printf("Skipped:          %d", st.Skipped)
	log.Printf("Errors:           %d", st.Errors)
	log.Printf("DryRun:           %v", dry)
	if dry {
		log.Println("\nRun with --dry-run=false to apply changes.")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseBatchIndex(name string) int {
	trim := strings.TrimPrefix(name, "highprofile_batch_")
	trim = strings.TrimSuffix(trim, ".json")
	if trim == name {
		return 0
	}
	val := 0
	for _, ch := range trim {
		if ch < '0' || ch > '9' {
			return 0
		}
		val = val*10 + int(ch-'0')
	}
	return val
}

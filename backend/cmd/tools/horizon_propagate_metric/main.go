package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizoncriticalpath "github.com/igorkirisyuk/sanskrite/internal/horizon/critical_path"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

func main() {
	var (
		projectID     = flag.String("project", "sanskrite", "GCP project id")
		canonicalPath = flag.String("path", "/org/byd", "canonical path to propagate from (e.g., /org/byd)")
		metricName    = flag.String("metric", "/metric/risk/supply_chain_criticality", "metric name to change")
		delta         = flag.Float64("delta", 0.1, "delta to apply to the source metric before propagation")
	)
	flag.Parse()

	ctx := context.Background()

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	// Real-time publisher (Pub/Sub) for propagation events
	pub, err := realtime.NewPublisher(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("realtime publisher init failed: %v", err)
	}

	frRepo := horizonrepo.NewHorizonFragmentsRepository(fs)
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fs)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fs)
	lensesRepo := horizonrepo.NewLensesRepository(fs)
	propagationRulesRepo := horizonrepo.NewPropagationPoliciesRepository(fs)

	// Policy infrastructure
	telemetryWriter := horizonpolicy.NewFirestoreTelemetryWriter(fs)
	propagationPolicy := horizonpolicy.NewPropagationPolicy(propagationRulesRepo)
	policyExecutor := horizonpolicy.NewPolicyExecutor(propagationPolicy, telemetryWriter, nil)

	// Critical path detector
	criticalPathDetector := horizoncriticalpath.NewDetector(frRepo, edgesRepo)

	// Resolve fragment
	cp := strings.TrimSpace(*canonicalPath)
	if cp == "" {
		log.Fatal("missing -path")
	}
	log.Printf("Looking up fragment: canonicalPath=%q", cp)
	frag, err := frRepo.GetByCanonicalPath(ctx, cp)
	if err != nil {
		log.Fatalf("GetByCanonicalPath failed: %v", err)
	}
	log.Printf("Found fragment: id=%s canonicalPath=%s", frag.ID, frag.CanonicalPath)

	mn := strings.TrimSpace(*metricName)
	if mn == "" {
		log.Fatal("missing -metric")
	}

	if frag.State == nil {
		frag.State = map[string]models.MetricValue{}
	}

	old := frag.State[mn] // zero value ok
	newVal := old
	newVal.Value = old.Value + *delta
	if math.Abs(newVal.Value) > 1e9 {
		log.Fatalf("refusing to set extreme metric value: %v", newVal.Value)
	}
	// keep existing confidence/unit; provenance will show update as manual tool
	newVal.Provenance.Method = "tool:horizon_propagate_metric"
	newVal.ObservedAt = time.Now().UTC()

	// Update source fragment first (so state is consistent in Firestore)
	updated := *frag
	updated.State = make(map[string]models.MetricValue, len(frag.State)+1)
	for k, v := range frag.State {
		updated.State[k] = v
	}
	updated.State[mn] = newVal

	log.Printf("Updating source fragment %s (%s): %s %.4f -> %.4f (delta=%.4f)", frag.ID, frag.CanonicalPath, mn, old.Value, newVal.Value, *delta)
	if err := frRepo.Update(ctx, &updated); err != nil {
		log.Fatalf("failed to update source fragment: %v", err)
	}

	// Run propagation
	engine := horizonprop.NewPropagationEngine(frRepo, versionsRepo, edgesRepo, lensesRepo, pub).
		WithPolicyExecutor(policyExecutor).
		WithCriticalPathDetector(criticalPathDetector)
	res, err := engine.PropagateChange(ctx, frag.ID, mn, old, newVal)
	if err != nil {
		log.Fatalf("PropagateChange failed: %v", err)
	}

	log.Printf("Propagation result: affectedFragments=%d affectedLenses=%d paths=%d time=%s metrics=%v",
		len(res.AffectedFragments), len(res.AffectedLenses), len(res.PropagationPaths), res.PropagationTime.String(), res.Metrics)
	if len(res.PropagationPaths) > 0 {
		limit := 5
		if len(res.PropagationPaths) < limit {
			limit = len(res.PropagationPaths)
		}
		for i := 0; i < limit; i++ {
			log.Printf("path[%d]=%v", i, res.PropagationPaths[i])
		}
	}

	fmt.Println("OK")
}




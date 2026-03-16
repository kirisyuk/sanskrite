package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbinding"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmptyString(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	zonesArg := flag.String("zones", "", "comma-separated canonical zone paths")
	supplyLimit := flag.Int("supply-limit", 4000, "max supply links to inspect for dynamic affectedZoneRefs")
	samples := flag.Int("samples", 5, "sample count per section")
	flag.Parse()

	if strings.TrimSpace(*zonesArg) == "" {
		fmt.Fprintln(os.Stderr, "--zones is required")
		os.Exit(2)
	}

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fs.Close()

	db := fs.Client()
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	binder := pathbinding.NewEngine(fragmentsRepo, edgesRepo)

	supplyLinks, err := fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(*tenantID), horizonmodels.FragmentTypeSupplyLink, *supplyLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list supply links failed: %v\n", err)
		os.Exit(1)
	}

	for _, zonePath := range parseZones(*zonesArg) {
		fmt.Printf("=== %s ===\n", zonePath)
		frag, err := fragmentsRepo.GetByCanonicalPath(ctx, zonePath)
		if err != nil || frag == nil {
			fmt.Printf("fragment: not found (err=%v)\n\n", err)
			continue
		}
		lat, lon, hasCoord := horizonrepo.ResolveFragmentCoordinates(frag)
		fmt.Printf("fragment: id=%s status=%s label=%q coords=%v", frag.ID, frag.Status, strings.TrimSpace(frag.Labels["en"]), hasCoord)
		if hasCoord {
			fmt.Printf(" %.4f,%.4f", lat, lon)
		}
		fmt.Printf(" radiusKm=%.1f hype=%.1f geoPrecision=%s\n", frag.RadiusKm, zoneHypeMetricValue(frag.State), qualifierString(frag.Qualifiers, "geoPrecision"))

		outEdges, _ := edgesRepo.ListBySource(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(frag.ID), nil, 128)
		affects := make([]string, 0, len(outEdges))
		locatedIn := make([]string, 0, len(outEdges))
		for _, edge := range outEdges {
			if edge == nil {
				continue
			}
			targetPath := fragmentPathByID(ctx, fragmentsRepo, strings.TrimSpace(edge.TargetFragmentID))
			switch edge.Type {
			case horizonmodels.DependencyTypeAffects:
				affects = append(affects, targetPath)
			case horizonmodels.DependencyTypeLocatedIn:
				locatedIn = append(locatedIn, targetPath)
			}
		}
		fmt.Printf("edges: affects=%d located_in=%d outgoing_total=%d\n", len(affects), len(locatedIn), len(outEdges))
		printSamples("affects_targets", affects, *samples)
		printSamples("located_in_targets", locatedIn, *samples)

		dynamicHits := make([]string, 0, *samples)
		dynamicCount := 0
		for _, link := range supplyLinks {
			if link == nil {
				continue
			}
			supplier := firstNonEmptyString(deref(link.SupplierFacilityRef), deref(link.SupplierRef))
			customer := firstNonEmptyString(deref(link.CustomerFacilityRef), deref(link.CustomerRef))
			product := deref(link.ProductRef)
			binding := binder.BindSegment(ctx, pathbinding.SegmentInput{
				SupplierRef:       supplier,
				CustomerRef:       customer,
				ProductRef:        product,
				TransportViaRefs:  append([]string(nil), link.TransportViaRefs...),
				SegmentConfidence: metricValue(link.State, "confidence"),
			})
			if !contains(binding.AffectedZoneRefs, zonePath) {
				continue
			}
			dynamicCount++
			if len(dynamicHits) < *samples {
				dynamicHits = append(dynamicHits, strings.TrimSpace(link.CanonicalPath))
			}
		}
		fmt.Printf("dynamic_binding_hits=%d/%d\n", dynamicCount, len(supplyLinks))
		printSamples("dynamic_supply_links", dynamicHits, *samples)

		materializedHits := make([]string, 0, *samples)
		materializedCount := 0
		iter := db.Collection("horizon_materialized_artifacts").
			Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Where("affectedZoneRefs", "array-contains", zonePath).
			Documents(ctx)
		for {
			doc, iterErr := iter.Next()
			if iterErr == iterator.Done {
				break
			}
			if iterErr != nil {
				fmt.Printf("materialized query error=%v\n", iterErr)
				break
			}
			materializedCount++
			if len(materializedHits) >= *samples {
				continue
			}
			var payload struct {
				CanonicalKey string `firestore:"canonicalKey"`
				DisplayName  string `firestore:"displayName"`
			}
			if err := doc.DataTo(&payload); err == nil {
				materializedHits = append(materializedHits, firstNonEmptyString(payload.CanonicalKey, payload.DisplayName, doc.Ref.ID))
			}
		}
		fmt.Printf("materialized_hits=%d\n", materializedCount)
		printSamples("materialized_artifacts", materializedHits, *samples)
		fmt.Println()
	}
}

func parseZones(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func fragmentPathByID(ctx context.Context, repo *horizonrepo.HorizonFragmentsRepository, id string) string {
	if repo == nil || strings.TrimSpace(id) == "" {
		return ""
	}
	frag, err := repo.Get(ctx, strings.TrimSpace(id))
	if err != nil || frag == nil {
		return ""
	}
	return strings.TrimSpace(frag.CanonicalPath)
}

func printSamples(label string, values []string, limit int) {
	if len(values) == 0 {
		fmt.Printf("%s: none\n", label)
		return
	}
	fmt.Printf("%s:\n", label)
	for i, value := range values {
		if i >= limit {
			break
		}
		fmt.Printf("  - %s\n", value)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func metricValue(state map[string]horizonmodels.MetricValue, key string) float64 {
	if state == nil {
		return 0
	}
	return state[key].Value
}

func zoneHypeMetricValue(state map[string]horizonmodels.MetricValue) float64 {
	if state == nil {
		return 0
	}
	if mv, ok := state["zone_hype_score"]; ok && mv.Value > 0 {
		return mv.Value
	}
	return state["hype_score"].Value
}

func qualifierString(qualifiers map[string]interface{}, key string) string {
	if qualifiers == nil {
		return ""
	}
	raw, ok := qualifiers[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbinding"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	projectID := flag.String("project", "nv-horizon", "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	zonesArg := flag.String("zones", "", "comma-separated canonical zone paths")
	limit := flag.Int("limit", 1500, "max supply links to inspect")
	samples := flag.Int("samples", 4, "sample hits per zone")
	flag.Parse()

	if strings.TrimSpace(*zonesArg) == "" {
		panic("--zones is required")
	}
	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fs.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fs.Client())
	binder := pathbinding.NewEngine(fragmentsRepo, edgesRepo)
	supplyLinks, err := fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(*tenantID), horizonmodels.FragmentTypeSupplyLink, *limit)
	if err != nil {
		panic(err)
	}

	zones := parseZones(*zonesArg)
	counts := make(map[string]int, len(zones))
	hits := make(map[string][]string, len(zones))
	for _, link := range supplyLinks {
		if link == nil {
			continue
		}
		supplier := firstNonEmpty(deref(link.SupplierFacilityRef), deref(link.SupplierRef))
		customer := firstNonEmpty(deref(link.CustomerFacilityRef), deref(link.CustomerRef))
		binding := binder.BindSegment(ctx, pathbinding.SegmentInput{
			SupplierRef:       supplier,
			CustomerRef:       customer,
			ProductRef:        deref(link.ProductRef),
			TransportViaRefs:  append([]string(nil), link.TransportViaRefs...),
			SegmentConfidence: metricValue(link.State, "confidence"),
		})
		for _, zone := range zones {
			if !contains(binding.AffectedZoneRefs, zone) {
				continue
			}
			counts[zone]++
			if len(hits[zone]) < *samples {
				hits[zone] = append(hits[zone], strings.TrimSpace(link.CanonicalPath))
			}
		}
	}

	fmt.Printf("scanned=%d\n", len(supplyLinks))
	for _, zone := range zones {
		fmt.Printf("%s count=%d\n", zone, counts[zone])
		for _, sample := range hits[zone] {
			fmt.Printf("  - %s\n", sample)
		}
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

func contains(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
}

func metricValue(state map[string]horizonmodels.MetricValue, key string) float64 {
	if state == nil {
		return 0
	}
	return state[key].Value
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

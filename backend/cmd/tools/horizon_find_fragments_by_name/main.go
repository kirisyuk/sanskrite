package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	query     = flag.String("query", "", "Exact fragment name, identifier, or canonical path")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")
	fragType  = flag.String("type", "", "Optional fragment type filter (e.g. ORGANIZATION, FACILITY)")
)

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if strings.TrimSpace(*query) == "" {
		log.Fatal("--query is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	fragmentsRepo := repo.NewHorizonFragmentsRepository(fsClient.Client())
	q := strings.TrimSpace(*query)
	t := models.FragmentType(strings.ToUpper(strings.TrimSpace(*fragType)))

	matches, err := findMatches(ctx, fragmentsRepo, q, t)
	if err != nil {
		log.Fatalf("Find fragments: %v", err)
	}
	if len(matches) == 0 {
		log.Printf("No fragments found for exact query: %q", q)
		return
	}

	log.Printf("Found %d exact matches for %q\n", len(matches), q)
	for _, f := range matches {
		if f == nil {
			continue
		}
		if strings.TrimSpace(*tenantID) != "" && strings.TrimSpace(f.TenantID) != "" && strings.TrimSpace(f.TenantID) != strings.TrimSpace(*tenantID) {
			continue
		}
		name := bestLabel(f)
		fmt.Printf("ID: %s\n", f.ID)
		fmt.Printf("Name: %s\n", name)
		fmt.Printf("CanonicalPath: %s\n", f.CanonicalPath)
		fmt.Printf("Type: %s\n", f.Type)
		fmt.Printf("HomeLocation: %s\n", homeLocationSummary(f))
		fmt.Printf("Coordinates: %s\n", coordinatesSummary(f))
		fmt.Printf("GeoProvider: %s\n", strings.TrimSpace(f.Identifiers["geoCoordinatesProvider"]))
		fmt.Printf("GeoQuery: %s\n", strings.TrimSpace(f.Identifiers["geoCoordinatesQuery"]))
		fmt.Printf("GeoFormattedAddress: %s\n", strings.TrimSpace(f.Identifiers["geoCoordinatesFormattedAddress"]))
		fmt.Printf("---\n")
	}
}

func findMatches(ctx context.Context, fragmentsRepo *repo.HorizonFragmentsRepository, query string, fragType models.FragmentType) ([]*models.HorizonFragment, error) {
	out := make([]*models.HorizonFragment, 0, 8)
	seen := map[string]struct{}{}
	add := func(items []*models.HorizonFragment) {
		for _, item := range items {
			if item == nil || strings.TrimSpace(item.ID) == "" {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			out = append(out, item)
		}
	}

	if strings.HasPrefix(query, "/") {
		if f, err := fragmentsRepo.GetByCanonicalPath(ctx, query); err == nil && f != nil {
			add([]*models.HorizonFragment{f})
		}
	}
	if items, err := fragmentsRepo.FindByLabel(ctx, "en", query, fragType); err == nil {
		add(items)
	}
	for _, key := range []string{"legalName", "shortName"} {
		if items, err := fragmentsRepo.FindByIdentifierAndType(ctx, key, query, fragType); err == nil {
			add(items)
		}
	}
	return out, nil
}

func bestLabel(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	for _, value := range []string{
		strings.TrimSpace(f.Labels["en"]),
		strings.TrimSpace(f.Identifiers["legalName"]),
		strings.TrimSpace(f.Identifiers["shortName"]),
	} {
		if value != "" {
			return value
		}
	}
	return ""
}

func homeLocationSummary(f *models.HorizonFragment) string {
	if f == nil || f.HomeLocation == nil {
		return ""
	}
	parts := []string{strings.TrimSpace(f.HomeLocation.Name)}
	if f.HomeLocation.ISO3166_1 != nil {
		parts = append(parts, strings.TrimSpace(*f.HomeLocation.ISO3166_1))
	}
	return strings.Join(compact(parts), ", ")
}

func coordinatesSummary(f *models.HorizonFragment) string {
	if f == nil || f.Coordinates == nil {
		return ""
	}
	return fmt.Sprintf("%.6f, %.6f", f.Coordinates.Lat, f.Coordinates.Lon)
}

func compact(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			out = append(out, s)
		}
	}
	return out
}

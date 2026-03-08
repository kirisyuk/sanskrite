package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

type sample struct {
	Path          string
	Type          string
	Label         string
	Place         string
	CurrentISO2   string
	ResolvedISO2  string
	FormattedAddr string
	Query         string
}

type kv struct {
	Key   string
	Count int
}

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	limit := flag.Int("limit", 300, "max fragments to probe")
	sampleN := flag.Int("samples", 12, "samples")
	flag.Parse()

	if key := gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")) == "" {
		_ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
	}

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()

	geoClient := googleGeoClient()
	if geoClient == nil {
		panic("google geocoding client is not configured")
	}

	scanned := 0
	conflicts := 0
	byType := map[string]int{}
	byResolved := map[string]int{}
	samples := make([]sample, 0, *sampleN)

	iter := fs.Client().Collection("horizon_fragments").Where("tenantId", "==", strings.TrimSpace(*tenantID)).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var frag h.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			continue
		}
		if frag.Type != h.FragmentTypeFacility && frag.Type != h.FragmentTypeOrganization {
			continue
		}
		if *limit > 0 && scanned >= *limit {
			break
		}
		scanned++

		currentISO2 := extractCurrentISO2(&frag)
		if currentISO2 == "" {
			continue
		}

		switch frag.Type {
		case h.FragmentTypeFacility:
			res, _ := scout.RevalidateFacilityGeo(ctx, cloneFragment(&frag), geoClient)
			if strings.TrimSpace(res.ResolvedISO2) != "" && !strings.EqualFold(currentISO2, res.ResolvedISO2) {
				conflicts++
				byType[string(frag.Type)]++
				byResolved[strings.ToLower(res.ResolvedISO2)]++
				if len(samples) < *sampleN {
					samples = append(samples, sample{
						Path:          frag.CanonicalPath,
						Type:          string(frag.Type),
						Label:         fragmentLabel(&frag),
						Place:         fragmentPlace(&frag),
						CurrentISO2:   currentISO2,
						ResolvedISO2:  res.ResolvedISO2,
						FormattedAddr: res.FormattedAddress,
						Query:         res.Query,
					})
				}
			}
		case h.FragmentTypeOrganization:
			res, _ := scout.RevalidateOrgGeo(ctx, cloneFragment(&frag), geoClient)
			if strings.TrimSpace(res.ResolvedISO2) != "" && !strings.EqualFold(currentISO2, res.ResolvedISO2) {
				conflicts++
				byType[string(frag.Type)]++
				byResolved[strings.ToLower(res.ResolvedISO2)]++
				if len(samples) < *sampleN {
					samples = append(samples, sample{
						Path:          frag.CanonicalPath,
						Type:          string(frag.Type),
						Label:         fragmentLabel(&frag),
						Place:         fragmentPlace(&frag),
						CurrentISO2:   currentISO2,
						ResolvedISO2:  res.ResolvedISO2,
						FormattedAddr: res.FormattedAddress,
						Query:         res.Query,
					})
				}
			}
		}
	}

	fmt.Printf("tenant=%s project=%s scanned=%d conflicts=%d\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*projectID), scanned, conflicts)
	printTop("conflicts_by_type", topCounts(byType))
	printTop("conflicts_by_resolved_iso2", topCounts(byResolved))
	if len(samples) > 0 {
		fmt.Println("samples:")
		for _, s := range samples {
			fmt.Printf("  - type=%s path=%s current=%s resolved=%s label=%q place=%q query=%q formatted=%q\n",
				s.Type, s.Path, s.CurrentISO2, s.ResolvedISO2, s.Label, s.Place, s.Query, s.FormattedAddr)
		}
	}
}

func googleGeoClient() coreontology.GeocodingClientInterface {
	if gc := httphandlers.GetGoogleGeocodingClientFromEnv(); gc != nil {
		return geocoding.NewAdapter(gc)
	}
	return nil
}

func cloneFragment(in *h.HorizonFragment) *h.HorizonFragment {
	if in == nil {
		return nil
	}
	cp := *in
	if in.HomeLocation != nil {
		loc := *in.HomeLocation
		cp.HomeLocation = &loc
	}
	if in.Coordinates != nil {
		coords := *in.Coordinates
		cp.Coordinates = &coords
	}
	if in.Labels != nil {
		cp.Labels = map[string]string{}
		for k, v := range in.Labels {
			cp.Labels[k] = v
		}
	}
	if in.Identifiers != nil {
		cp.Identifiers = map[string]string{}
		for k, v := range in.Identifiers {
			cp.Identifiers[k] = v
		}
	}
	return &cp
}

func extractCurrentISO2(fragment *h.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if v := strings.ToUpper(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1)); len(v) == 2 {
			return v
		}
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"countryIso2", "jurisdictionIso2", "country", "jurisdiction"} {
			if v := strings.ToUpper(strings.TrimSpace(fragment.Identifiers[key])); len(v) == 2 {
				return v
			}
		}
	}
	return ""
}

func fragmentLabel(fragment *h.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	return firstNonEmpty(
		strings.TrimSpace(fragment.Labels["en"]),
		strings.TrimSpace(fragment.Identifiers["legalName"]),
		strings.TrimSpace(fragment.Identifiers["shortName"]),
	)
}

func fragmentPlace(fragment *h.HorizonFragment) string {
	if fragment == nil || fragment.HomeLocation == nil {
		return ""
	}
	return strings.TrimSpace(fragment.HomeLocation.Name)
}

func topCounts(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{Key: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func printTop(title string, rows []kv) {
	fmt.Println(title + ":")
	if len(rows) == 0 {
		fmt.Println("  (none)")
		fmt.Println()
		return
	}
	for _, row := range rows {
		fmt.Printf("  %s: %d\n", row.Key, row.Count)
	}
	fmt.Println()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}

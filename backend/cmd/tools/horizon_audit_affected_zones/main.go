package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var countryNames = map[string]string{
	"us": "united states", "gb": "united kingdom", "ir": "iran", "il": "israel", "cn": "china", "ua": "ukraine", "ru": "russia", "fr": "france", "de": "germany", "br": "brazil",
	"iq": "iraq", "af": "afghanistan", "kp": "north korea", "pk": "pakistan", "tw": "taiwan", "ca": "canada", "au": "australia", "fi": "finland", "hu": "hungary", "ie": "ireland",
	"in": "india", "bh": "bahrain", "ae": "united arab emirates", "qa": "qatar", "sa": "saudi arabia", "ps": "palestine", "sy": "syria", "lb": "lebanon", "bd": "bangladesh",
	"ch": "switzerland", "jp": "japan", "mx": "mexico", "za": "south africa", "no": "norway", "ph": "philippines", "id": "indonesia", "sd": "sudan", "ma": "morocco", "ly": "libya",
	"es": "spain", "gr": "greece", "cl": "chile", "az": "azerbaijan", "tr": "turkey", "pe": "peru", "nl": "netherlands", "se": "sweden",
}

var countrySlugRE = regexp.MustCompile(`(?i)united-states|united-kingdom|iran|israel|china|ukraine|russia|france|germany|bahrain|qatar|saudi-arabia|pakistan|india|taiwan|canada|australia|finland|hungary|ireland|bangladesh|switzerland|japan|mexico|south-africa|norway|philippines|indonesia|sudan|morocco|libya|spain|greece|chile|azerbaijan|turkey|peru|netherlands|sweden|north-korea|united-arab-emirates|afghanistan|iraq|syria|lebanon`)

type example struct {
	Path      string
	URL       string
	UpdatedAt string
	LastMatch string
}

func metric(state map[string]horizonmodels.MetricValue, key string) float64 {
	if v, ok := state[key]; ok {
		return v.Value
	}
	return 0
}

func qstr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", v)))
}

func zoneSlug(cp string) string {
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func zoneGeo(cp string) string {
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func addExample(store map[string][]example, key string, ex example, limit int) {
	if len(store[key]) >= limit {
		return
	}
	store[key] = append(store[key], ex)
}

func main() {
	var (
		projectID  = flag.String("project", "nv-horizon", "GCP project id")
		tenantID   = flag.String("tenant", "global", "tenant id")
		limit      = flag.Int("limit", 5000, "max affected zones to scan")
		activeOnly = flag.Bool("active-only", false, "count only active zones")
	)
	flag.Parse()

	ctx := context.Background()
	client, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(client.Client())
	items, err := fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(*tenantID), horizonmodels.FragmentTypeAffectedZone, *limit)
	if err != nil {
		log.Fatal(err)
	}

	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)
	counts := map[string]int{}
	examples := map[string][]example{}
	var total, recent int
	var older30Touched, older180Touched, older365Touched int

	for _, f := range items {
		if f == nil || !strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/zone/") {
			continue
		}
		if *activeOnly && !strings.EqualFold(strings.TrimSpace(f.Status), string(horizonmodels.FragmentStatusActive)) {
			continue
		}
		total++
		g := zoneGeo(f.CanonicalPath)
		s := strings.ToLower(zoneSlug(f.CanonicalPath))
		gp := qstr(f.Qualifiers, "geoPrecision")
		cond := qstr(f.Qualifiers, "conditionType")
		lastMatch := metric(f.State, "zone_last_match_unix")
		ex := example{
			Path:      f.CanonicalPath,
			URL:       qstr(f.Qualifiers, "sourceUrl"),
			UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if lastMatch > 0 {
			ex.LastMatch = time.Unix(int64(lastMatch), 0).UTC().Format(time.RFC3339)
		}

		if f.UpdatedAt.After(cutoff) {
			recent++
			if lastMatch > 0 {
				age := now.Sub(time.Unix(int64(lastMatch), 0).UTC())
				if age > 30*24*time.Hour {
					older30Touched++
					addExample(examples, "older30d_recently_touched", ex, 20)
				}
				if age > 180*24*time.Hour {
					older180Touched++
				}
				if age > 365*24*time.Hour {
					older365Touched++
				}
			}
		}

		if g == "global" {
			counts["global_geo"]++
			addExample(examples, "global_geo", ex, 10)
		}
		if gp == "approximate_country" && s != "" && !strings.Contains(s, strings.ReplaceAll(countryNames[g], " ", "-")) && !strings.Contains(s, "region") && !strings.Contains(s, "country") {
			counts["local_slug_with_country_centroid"]++
			addExample(examples, "local_slug_with_country_centroid", ex, 15)
		}
		if m := countrySlugRE.FindString(s); m != "" && !looksLikeLegitCrossBorderOrSeaZone(g, s) {
			expected := ""
			for cc, name := range countryNames {
				if strings.ReplaceAll(name, " ", "-") == strings.ToLower(m) {
					expected = cc
					break
				}
			}
			if expected != "" && expected != g && g != "global" {
				counts["country_slug_mismatch"]++
				addExample(examples, "country_slug_mismatch", ex, 20)
			}
		}
		if (gp == "none" || gp == "") && !f.HasCoordinates && g != "global" {
			counts["no_geo_for_non_global"]++
			addExample(examples, "no_geo_for_non_global", ex, 12)
		}
		if strings.Contains(f.CanonicalPath, "/cyber_disruption/") && g == "global" {
			counts["global_cyber"]++
			addExample(examples, "global_cyber", ex, 8)
		}
		if cond == "" {
			counts["legacy_missing_qualifiers"]++
			addExample(examples, "legacy_missing_qualifiers", ex, 10)
		}
		if metric(f.State, "zone_hype_score") >= 90 && g == "global" {
			counts["high_hype_global"]++
			addExample(examples, "high_hype_global", ex, 10)
		}
	}

	fmt.Printf("total=%d recent24h=%d older30d_recently_touched=%d older180d_recently_touched=%d older365d_recently_touched=%d\n", total, recent, older30Touched, older180Touched, older365Touched)
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s=%d\n", k, counts[k])
		for _, ex := range examples[k] {
			if ex.LastMatch != "" {
				fmt.Printf("  - updated=%s lastMatch=%s %s\n    %s\n", ex.UpdatedAt, ex.LastMatch, ex.Path, ex.URL)
				continue
			}
			fmt.Printf("  - updated=%s %s\n    %s\n", ex.UpdatedAt, ex.Path, ex.URL)
		}
	}
	if touched := examples["older30d_recently_touched"]; len(touched) > 0 {
		fmt.Printf("older30d_recently_touched_examples=%d\n", len(touched))
		for _, ex := range touched {
			fmt.Printf("  - updated=%s lastMatch=%s %s\n    %s\n", ex.UpdatedAt, ex.LastMatch, ex.Path, ex.URL)
		}
	}
}

func looksLikeLegitCrossBorderOrSeaZone(geoToken, slug string) bool {
	geoToken = strings.TrimSpace(strings.ToLower(geoToken))
	slug = strings.TrimSpace(strings.ToLower(slug))
	if geoToken == "sea" || strings.Contains(geoToken, "-") {
		return true
	}
	if strings.Contains(slug, "sea") ||
		strings.Contains(slug, "strait") ||
		strings.Contains(slug, "channel") ||
		strings.Contains(slug, "gulf") ||
		strings.Contains(slug, "canal") ||
		strings.Contains(slug, "border") ||
		strings.Contains(slug, "exclusive-economic-zone") ||
		strings.Contains(slug, "economic-zone") {
		return true
	}
	return false
}

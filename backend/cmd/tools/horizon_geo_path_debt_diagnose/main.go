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
	h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	ah "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

type sample struct {
	Path   string
	Type   string
	ISO2   string
	Label  string
	Source string
}

type kv struct {
	Key   string
	Count int
}

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	topN := flag.Int("top", 20, "top N rows")
	sampleN := flag.Int("samples", 12, "sample rows to print")
	flag.Parse()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()

	total := 0
	facKnownIsoNa := 0
	orgKnownIsoNa := 0
	govKnownIsoNa := 0
	typeDebt := map[string]int{}
	isoDebt := map[string]int{}
	sourceDebt := map[string]int{}
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
		total++
		path := strings.TrimSpace(frag.CanonicalPath)
		iso2, source := inferISO2(&frag)
		if iso2 == "" {
			continue
		}
	switch {
	case frag.Class == ah.FragmentClassSite && strings.HasPrefix(path, "/fac/na/"):
		facKnownIsoNa++
	case frag.Class == ah.FragmentClassOrg && strings.HasPrefix(path, "/org/na/"):
		orgKnownIsoNa++
	case strings.HasPrefix(path, "/gov/na/"):
		govKnownIsoNa++
	default:
		continue
	}
	inc(typeDebt, string(frag.Class))
	inc(isoDebt, iso2)
	inc(sourceDebt, source)
	if len(samples) < *sampleN {
		samples = append(samples, sample{
			Path:   path,
			Type:   string(frag.Class),
				ISO2:   iso2,
				Label:  firstNonEmpty(strings.TrimSpace(frag.Labels["en"]), strings.TrimSpace(frag.Identifiers["legalName"]), strings.TrimSpace(frag.Identifiers["shortName"])),
				Source: source,
			})
		}
	}

	fmt.Printf("tenant=%s project=%s total_fragments=%d\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*projectID), total)
	fmt.Printf("facility_known_iso_but_na_path=%d\n", facKnownIsoNa)
	fmt.Printf("organization_known_iso_but_na_path=%d\n", orgKnownIsoNa)
	fmt.Printf("gov_known_iso_but_na_path=%d\n\n", govKnownIsoNa)

	printTop("debt_by_type", topCounts(typeDebt, *topN))
	printTop("debt_by_iso2", topCounts(isoDebt, *topN))
	printTop("debt_by_inference_source", topCounts(sourceDebt, *topN))

	if len(samples) > 0 {
		fmt.Println("samples:")
		for _, item := range samples {
			fmt.Printf("  - type=%s iso=%s source=%s path=%s label=%q\n", item.Type, item.ISO2, item.Source, item.Path, item.Label)
		}
	}
}

func inferISO2(fragment *h.HorizonFragment) (string, string) {
	if fragment == nil {
		return "", ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if v := normalizeISO2(*fragment.HomeLocation.ISO3166_1); v != "" {
			return v, "homeLocation.iso3166_1"
		}
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"countryIso2", "jurisdictionIso2", "country", "jurisdiction"} {
			if v := normalizeISO2(fragment.Identifiers[key]); v != "" {
				return v, "identifiers." + key
			}
		}
	}
	return "", ""
}

func normalizeISO2(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 2 {
		return ""
	}
	if s[0] < 'a' || s[0] > 'z' || s[1] < 'a' || s[1] > 'z' {
		return ""
	}
	return s
}

func inc(m map[string]int, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "(empty)"
	}
	m[key]++
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func topCounts(m map[string]int, limit int) []kv {
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
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
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

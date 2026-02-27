package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")

	apply  = flag.Bool("apply", false, "Apply changes (default: dry-run)")
	dryRun = flag.Bool("dry-run", false, "Force dry-run even if --apply")

	limit         = flag.Int("limit", 0, "Max fragments to process (0 = all)")
	maxScan       = flag.Int("max-scan", 100000, "Max fragments to scan")
	pageSize      = flag.Int("page-size", 200, "Firestore page size")
	startAfter    = flag.String("start-after", "", "Resume cursor: start after this document ID")
	pathPrefix    = flag.String("path-prefix", "", "Only process canonicalPath with this prefix")
	progressEvery = flag.Int("progress-every", 500, "Log progress every N processed (0=disable)")
)

type stats struct {
	Scanned    int64
	Processed  int64
	Changed    int64
	Applied    int64
	ISOFilled  int64
	NameFilled int64
	NoSignal   int64
	Errors     int64
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *tenantID != "" && *tenantID != "global" {
		*tenantID = "global"
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	log.Printf("Starting geo-location merge (project=%s apply=%v maxScan=%d prefix=%q)", *projectID, *apply && !*dryRun, *maxScan, strings.TrimSpace(*pathPrefix))

	var st stats
	lastSeen := strings.TrimSpace(*startAfter)
	col := db.Collection("horizon_fragments")
	prefix := strings.TrimSpace(*pathPrefix)

	for {
		if *limit > 0 && atomic.LoadInt64(&st.Processed) >= int64(*limit) {
			break
		}
		if atomic.LoadInt64(&st.Scanned) >= int64(*maxScan) {
			log.Printf("Reached maxScan=%d, stopping", *maxScan)
			break
		}

		q := col.Where("tenantId", "==", *tenantID).
			Where("governance.status", "==", string(hmodels.FragmentStatusActive)).
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if lastSeen != "" {
			q = q.StartAfter(lastSeen)
		}

		iter := q.Documents(ctx)
		pageDocs := make([]*firestore.DocumentSnapshot, 0, *pageSize)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				log.Fatalf("iterate: %v", err)
			}
			pageDocs = append(pageDocs, doc)
		}
		iter.Stop()

		if len(pageDocs) == 0 {
			break
		}

		for _, doc := range pageDocs {
			atomic.AddInt64(&st.Scanned, 1)
			lastSeen = doc.Ref.ID

			if *limit > 0 && atomic.LoadInt64(&st.Processed) >= int64(*limit) {
				break
			}

			var frag hmodels.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				atomic.AddInt64(&st.Errors, 1)
				continue
			}

			cp := strings.TrimSpace(frag.CanonicalPath)
			if prefix != "" && !strings.HasPrefix(cp, prefix) {
				continue
			}

			atomic.AddInt64(&st.Processed, 1)
			changed := false

			if frag.HomeLocation == nil {
				frag.HomeLocation = &hmodels.Location{}
			}

			if frag.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*frag.HomeLocation.ISO3166_1) == "" {
				if cc := deriveCountryFromFragment(&frag); cc != "" {
					v := cc
					frag.HomeLocation.ISO3166_1 = &v
					changed = true
					atomic.AddInt64(&st.ISOFilled, 1)
				}
			}

			if strings.TrimSpace(frag.HomeLocation.Name) == "" {
				if name := deriveLocationNameFromFragment(&frag); name != "" {
					frag.HomeLocation.Name = name
					changed = true
					atomic.AddInt64(&st.NameFilled, 1)
				}
			}

			if !changed {
				atomic.AddInt64(&st.NoSignal, 1)
				continue
			}
			atomic.AddInt64(&st.Changed, 1)

			if *apply && !*dryRun {
				patch := map[string]interface{}{
					"homeLocation":           frag.HomeLocation,
					"metadata.lastUpdatedAt": time.Now().UTC(),
				}
				if _, err := doc.Ref.Set(ctx, patch, firestore.MergeAll); err != nil {
					atomic.AddInt64(&st.Errors, 1)
					log.Printf("update %s: %v", doc.Ref.ID, err)
					continue
				}
				atomic.AddInt64(&st.Applied, 1)
			}

			if *progressEvery > 0 && atomic.LoadInt64(&st.Processed)%int64(*progressEvery) == 0 {
				log.Printf("Progress: scanned=%d processed=%d changed=%d applied=%d isoFilled=%d nameFilled=%d noSignal=%d errors=%d last=%s",
					atomic.LoadInt64(&st.Scanned), atomic.LoadInt64(&st.Processed),
					atomic.LoadInt64(&st.Changed), atomic.LoadInt64(&st.Applied),
					atomic.LoadInt64(&st.ISOFilled), atomic.LoadInt64(&st.NameFilled),
					atomic.LoadInt64(&st.NoSignal), atomic.LoadInt64(&st.Errors), lastSeen)
			}
		}

		if len(pageDocs) < *pageSize {
			break
		}
	}

	log.Printf("Done. scanned=%d processed=%d changed=%d applied=%d isoFilled=%d nameFilled=%d noSignal=%d errors=%d",
		atomic.LoadInt64(&st.Scanned), atomic.LoadInt64(&st.Processed),
		atomic.LoadInt64(&st.Changed), atomic.LoadInt64(&st.Applied),
		atomic.LoadInt64(&st.ISOFilled), atomic.LoadInt64(&st.NameFilled),
		atomic.LoadInt64(&st.NoSignal), atomic.LoadInt64(&st.Errors))
}

func deriveLocationNameFromFragment(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	if strings.HasPrefix(cp, "/loc/") {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 4 && parts[0] == "loc" {
			city := strings.TrimSpace(parts[3])
			if city != "" && !strings.EqualFold(city, "na") {
				return strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(city))
			}
		}
	}
	if f.Identifiers != nil {
		for _, k := range []string{"city", "locality", "town", "municipality", "settlement"} {
			if v := strings.TrimSpace(f.Identifiers[k]); v != "" {
				return v
			}
		}
	}
	return ""
}

func deriveCountryFromFragment(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if f.HomeLocation != nil && f.HomeLocation.ISO3166_1 != nil {
		if cc := normalizeISO2(*f.HomeLocation.ISO3166_1); cc != "" {
			return cc
		}
	}
	if f.Identifiers != nil {
		for _, k := range []string{"iso3166_1", "countryAlpha2", "countryCode", "country_code", "country", "iso2"} {
			if cc := normalizeISO2(f.Identifiers[k]); cc != "" {
				return cc
			}
		}
	}
	if cc := extractCountryFromCanonicalPath(f.CanonicalPath); cc != "" {
		return cc
	}
	for _, ctxPath := range []string{f.ParentPath, f.ParentGovPath, f.ParentMarketPath, f.AffiliatedOrgPath} {
		if cc := extractCountryFromCanonicalPath(ctxPath); cc != "" {
			return cc
		}
	}
	for _, seg := range f.MarketSegments {
		parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(seg)), "/"), "/")
		if len(parts) >= 4 && parts[0] == "segment" {
			if cc := normalizeISO2(parts[len(parts)-1]); cc != "" {
				return cc
			}
		}
	}
	return ""
}

func extractCountryFromCanonicalPath(rawPath string) string {
	cp := strings.TrimSpace(rawPath)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(strings.ToLower(cp), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "loc", "market", "community":
		return normalizeISO2(parts[1])
	default:
		return ""
	}
}

func normalizeISO2(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if len(s) != 2 || s == "NA" {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return ""
		}
	}
	return s
}

func init() {
	flag.Usage = func() {
		fmt.Printf("Usage: %s --project=<gcp-project> [--apply] [--dry-run] [--limit N] [--max-scan N]\\n", strings.TrimSpace(flag.CommandLine.Name()))
		flag.PrintDefaults()
	}
}

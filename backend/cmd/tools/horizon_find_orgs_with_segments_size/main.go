package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var (
	projectID  = flag.String("project", "", "GCP project ID (required)")
	tenantID   = flag.String("tenant", "global", "Tenant ID (default: global)")
	activeOnly = flag.Bool("active-only", true, "Only consider fragments with status=active")
	pageSize   = flag.Int("page-size", 500, "Firestore page size (default: 500)")
	maxScan    = flag.Int("max-scan", 200000, "Max scanned docs (safety cap)")
	limit      = flag.Int("limit", 100, "Max matching orgs to print (default: 100, 0 = unlimited)")
	format     = flag.String("format", "table", "Output format: table | jsonl")
	startAfter = flag.String("start-after", "", "Start after Firestore document ID (resume cursor)")
)

type hit struct {
	ID             string   `json:"id"`
	CanonicalPath  string   `json:"canonicalPath"`
	Label          string   `json:"label,omitempty"`
	OrgSize        string   `json:"orgSize,omitempty"`
	MarketSegments []string `json:"marketSegments,omitempty"`
	Status         string   `json:"status,omitempty"`
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}

	// Query by tenant and org type. This is fastest but may require a composite index depending on project.
	// If the query errors due to missing index, we fall back to scanning tenant docs and filtering in code.
	base := db.Collection("horizon_fragments").
		Where("tenantId", "==", tenant).
		Where("type", "==", string(hmodels.FragmentTypeOrganization)).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if *activeOnly {
		base = base.Where("status", "==", "active")
	}

	startDocID := strings.TrimSpace(*startAfter)

	var (
		scanned            = 0
		orgsWithOrgSize    = 0 // orgSize != "" && orgSize != "na"
		orgsWithOrgSizeNA  = 0 // orgSize == "na" (recognized but unknown)
		orgsWithOrgSizeAny = 0 // orgSize != "" (any non-empty value)
		orgsWithSegments   = 0
		orgsWithBoth       = 0
		matches            = 0
		printed            = 0
		lastDoc            *firestore.DocumentSnapshot
		fallbackToScanAll  = false
		outHits            []hit
	)

	startTime := time.Now()

	runPage := func(q firestore.Query) (nextLast *firestore.DocumentSnapshot, pageCount int, err error) {
		iter := q.Limit(*pageSize).Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				return nextLast, pageCount, nil
			}
			if err != nil {
				return nextLast, pageCount, err
			}
			nextLast = doc
			pageCount++
			scanned++
			if *maxScan > 0 && scanned >= *maxScan {
				return nextLast, pageCount, fmt.Errorf("max-scan reached (%d)", *maxScan)
			}

			var f hmodels.HorizonFragment
			if err := doc.DataTo(&f); err != nil {
				continue
			}
			// Safety: in fallback scan, enforce org type.
			if f.Type != hmodels.FragmentTypeOrganization {
				continue
			}
			if *activeOnly && strings.ToLower(strings.TrimSpace(f.Status)) != "active" {
				continue
			}

			orgSize := strings.ToLower(strings.TrimSpace(f.OrgSize))
			hasOrgSizeAny := orgSize != ""
			hasOrgSizeNA := orgSize == "na"
			hasOrgSize := hasOrgSizeAny && !hasOrgSizeNA
			hasSegments := len(f.MarketSegments) > 0
			if hasOrgSizeAny {
				orgsWithOrgSizeAny++
			}
			if hasOrgSizeNA {
				orgsWithOrgSizeNA++
			}
			if hasOrgSize {
				orgsWithOrgSize++
			}
			if hasSegments {
				orgsWithSegments++
			}
			if hasOrgSize && hasSegments {
				orgsWithBoth++
			}

			if !(hasOrgSize || hasSegments) {
				continue
			}
			matches++

			if *limit > 0 && printed >= *limit {
				continue
			}
			printed++

			lbl := strings.TrimSpace(f.Labels["en"])
			if lbl == "" {
				lbl = strings.TrimSpace(f.Identifiers["legalName"])
			}
			if lbl == "" {
				lbl = strings.TrimSpace(f.Identifiers["shortName"])
			}

			h := hit{
				ID:             f.ID,
				CanonicalPath:  strings.TrimSpace(f.CanonicalPath),
				Label:          lbl,
				OrgSize:        f.OrgSize,
				MarketSegments: f.MarketSegments,
				Status:         f.Status,
			}
			outHits = append(outHits, h)
		}
	}

	query := base
	if startDocID != "" {
		// StartAfter supports document snapshots, not IDs, so fetch the doc.
		snap, err := db.Collection("horizon_fragments").Doc(startDocID).Get(ctx)
		if err != nil || !snap.Exists() {
			log.Printf("WARNING: start-after doc %q not found; starting from beginning", startDocID)
		} else {
			query = query.StartAfter(snap)
		}
	}

	for {
		q := query
		if lastDoc != nil {
			q = query.StartAfter(lastDoc)
		}
		nextLast, pageCount, err := runPage(q)
		if err != nil {
			// Detect common Firestore missing-index error and fall back to scanning tenant docs.
			msg := strings.ToLower(err.Error())
			if !fallbackToScanAll && (strings.Contains(msg, "failed to execute query") || strings.Contains(msg, "requires an index") || strings.Contains(msg, "indexes")) {
				log.Printf("WARNING: indexed query failed (%v). Falling back to scan-by-tenant and filter in code (slower).", err)
				fallbackToScanAll = true
				// Reset counters and rerun with fallback query.
				scanned, orgsWithOrgSize, orgsWithOrgSizeNA, orgsWithOrgSizeAny, orgsWithSegments, orgsWithBoth, matches, printed = 0, 0, 0, 0, 0, 0, 0, 0
				outHits = nil
				lastDoc = nil
				base2 := db.Collection("horizon_fragments").Where("tenantId", "==", tenant).OrderBy(firestore.DocumentID, firestore.Asc)
				query = base2
				continue
			}
			// End on safety cap or other errors.
			log.Printf("ERROR: %v", err)
			break
		}
		if pageCount == 0 || nextLast == nil {
			break
		}
		lastDoc = nextLast
	}

	elapsed := time.Since(startTime)

	// Summary
	log.Printf("Scanned: %d (tenant=%s, type=ORGANIZATION, activeOnly=%v, fallbackScan=%v) in %s", scanned, tenant, *activeOnly, fallbackToScanAll, elapsed.Round(time.Millisecond))
	log.Printf("Matched (orgSize OR marketSegments): %d", matches)
	log.Printf("With orgSize (known, not 'na'): %d", orgsWithOrgSize)
	log.Printf("With orgSize='na' (recognized but unknown): %d", orgsWithOrgSizeNA)
	log.Printf("With orgSize (any non-empty value): %d", orgsWithOrgSizeAny)
	log.Printf("With marketSegments: %d", orgsWithSegments)
	log.Printf("With BOTH (orgSize known + segments): %d", orgsWithBoth)
	if lastDoc != nil {
		log.Printf("Resume cursor (start-after): %s", lastDoc.Ref.ID)
	}

	if strings.ToLower(strings.TrimSpace(*format)) == "jsonl" {
		enc := json.NewEncoder(os.Stdout)
		for _, h := range outHits {
			_ = enc.Encode(h)
		}
		return
	}

	// Table output
	fmt.Printf("\n%-28s  %-10s  %-7s  %-10s  %s\n", "ID", "orgSize", "segs", "status", "canonicalPath")
	fmt.Printf("%s\n", strings.Repeat("-", 92))
	for _, h := range outHits {
		fmt.Printf("%-28.28s  %-10.10s  %-7d  %-10.10s  %s\n",
			h.ID,
			strings.TrimSpace(h.OrgSize),
			len(h.MarketSegments),
			strings.TrimSpace(h.Status),
			strings.TrimSpace(h.CanonicalPath),
		)
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID for SupplyLinks (default: global)")

	apply         = flag.Bool("apply", false, "Apply changes (default: dry-run)")
	dryRun        = flag.Bool("dry-run", false, "Force dry-run even if --apply")
	limit         = flag.Int("limit", 0, "Max SupplyLinks to process (0 = all)")
	progressEvery = flag.Int("progress-every", 100, "Log progress every N processed (0=disable)")
)

type stats struct {
	SupplyLinksScanned int64
	SupplyLinksChanged int64
	SupplyLinksApplied int64
	FacilitiesCreated  int64
	Errors            int64
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore client: %v", err)
	}
	defer fsClient.Close()

	fragmentsRepo := repo.NewHorizonFragmentsRepository(fsClient.Client())

	log.Printf("Supply chain org→fac migration (project=%s apply=%v tenant=%s)",
		*projectID, *apply && !*dryRun, *tenantID)
	log.Fatal("deprecated: synthetic /fac/*-general-production migration is disabled; use org->product links and concrete facilities only")

	var st stats

	// 1. Migrate SupplyLinks with org refs
	links, err := fragmentsRepo.ListByTypeAndTenant(ctx, *tenantID, hmodels.FragmentTypeSupplyLink, 10000)
	if err != nil {
		log.Fatalf("List SupplyLinks: %v", err)
	}

	for i, link := range links {
		if *limit > 0 && i >= *limit {
			break
		}
		atomic.AddInt64(&st.SupplyLinksScanned, 1)

		changed := false
		supplier := ""
		if link.SupplierRef != nil {
			supplier = strings.TrimSpace(*link.SupplierRef)
		}
		customer := ""
		if link.CustomerRef != nil {
			customer = strings.TrimSpace(*link.CustomerRef)
		}

		newSupplier := supplier
		if strings.HasPrefix(supplier, "/org/") {
			if norm := hmodels.NormalizeOrgRefToFacilityFallback(supplier); norm != "" {
				newSupplier = norm
				changed = true
			}
		}
		newCustomer := customer
		if strings.HasPrefix(customer, "/org/") {
			if norm := hmodels.NormalizeOrgRefToFacilityFallback(customer); norm != "" {
				newCustomer = norm
				changed = true
			}
		}

		if !changed {
			continue
		}
		atomic.AddInt64(&st.SupplyLinksChanged, 1)

		// Ensure general-production facility fragments exist
		for _, path := range []string{newSupplier, newCustomer} {
			if path == "" || !strings.HasSuffix(path, "-general-production") {
				continue
			}
			existing, _ := fragmentsRepo.GetByCanonicalPath(ctx, path)
			if existing == nil || strings.TrimSpace(existing.ID) == "" {
				facFrag := &hmodels.HorizonFragment{
					CanonicalPath: path,
					Type:          hmodels.FragmentTypeFacility,
					Labels:        map[string]string{"en": "General production"},
					Identifiers:   map[string]string{},
					State:         make(map[string]hmodels.MetricValue),
					TenantID:      "global",
					Status:        "active",
					Governance:    hmodels.Governance{Owner: "SYSTEM", Status: string(hmodels.FragmentStatusActive)},
					Metadata:      hmodels.FragmentMetadata{CreatedAt: time.Now().UTC(), LastUpdatedAt: time.Now().UTC()},
					Provenance:    hmodels.Provenance{Method: "supply_chain_org_migration"},
				}
				if *apply && !*dryRun {
					if _, createErr := fragmentsRepo.Create(ctx, facFrag); createErr == nil {
						atomic.AddInt64(&st.FacilitiesCreated, 1)
						log.Printf("Created facility %s", path)
					}
				} else {
					atomic.AddInt64(&st.FacilitiesCreated, 1)
					log.Printf("Would create facility %s", path)
				}
			}
		}

		if *apply && !*dryRun {
			link.SupplierRef = &newSupplier
			link.CustomerRef = &newCustomer
			link.Metadata.LastUpdatedAt = time.Now().UTC()
			if err := fragmentsRepo.Update(ctx, link); err != nil {
				atomic.AddInt64(&st.Errors, 1)
				log.Printf("Update SupplyLink %s: %v", link.ID, err)
				continue
			}
			atomic.AddInt64(&st.SupplyLinksApplied, 1)
		}

		log.Printf("SupplyLink %s: %s → %s, %s → %s", link.ID, supplier, newSupplier, customer, newCustomer)

		if *progressEvery > 0 && (i+1)%*progressEvery == 0 {
			log.Printf("Progress: scanned=%d changed=%d applied=%d facilities=%d errors=%d",
				atomic.LoadInt64(&st.SupplyLinksScanned), atomic.LoadInt64(&st.SupplyLinksChanged),
				atomic.LoadInt64(&st.SupplyLinksApplied), atomic.LoadInt64(&st.FacilitiesCreated),
				atomic.LoadInt64(&st.Errors))
		}
	}

	log.Printf("Done. supplyLinksScanned=%d changed=%d applied=%d facilitiesCreated=%d errors=%d",
		st.SupplyLinksScanned, st.SupplyLinksChanged, st.SupplyLinksApplied, st.FacilitiesCreated, st.Errors)
}

func init() {
	flag.Usage = func() {
		fmt.Printf(`Usage: supply_chain_migrate_org_to_fac --project=<gcp-project>

Deprecated. Synthetic /fac/*-general-production migration is disabled.

`)
		flag.PrintDefaults()
	}
}

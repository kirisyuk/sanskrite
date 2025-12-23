package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

type lensLite struct {
	ID        string    `firestore:"id"`
	TenantID  string    `firestore:"tenantId"`
	ProjectID string    `firestore:"projectId"`
	Status    string    `firestore:"status"`
	Name      string    `firestore:"name"`
	CreatedAt time.Time `firestore:"createdAt"`
	UpdatedAt time.Time `firestore:"updatedAt"`
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	tenant := flag.String("tenant", "global", "tenantId (default: global)")
	limit := flag.Int("limit", 200, "max lenses to fetch (default 200)")
	freshness := flag.Duration("freshness", 30*24*time.Hour, "only include lenses updated within this window (default 30d)")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		fmt.Fprintln(os.Stderr, "--project is required (or set GCP_PROJECT_ID)")
		os.Exit(2)
	}
	tenantID := strings.TrimSpace(*tenant)
	if tenantID == "" {
		tenantID = "global"
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	// Primary query: tenant filter + updatedAt ordering. This may require a composite index.
	q := db.Collection("horizon_lenses").Query
	if strings.ToLower(tenantID) != "all" {
		q = q.Where("tenantId", "==", tenantID)
	}
	if *freshness > 0 {
		cutoff := time.Now().UTC().Add(-*freshness)
		q = q.Where("updatedAt", ">=", cutoff)
	}
	q = q.OrderBy("updatedAt", firestore.Desc)
	if *limit > 0 {
		q = q.Limit(*limit)
	}

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		// Fallback: if composite index is missing, retry without tenant filter and filter in-memory.
		// This still answers "any recent lens updates?" without requiring index creation.
		if strings.Contains(strings.ToLower(err.Error()), "requires an index") && strings.ToLower(tenantID) != "all" {
			q2 := db.Collection("horizon_lenses").Query
			if *freshness > 0 {
				cutoff := time.Now().UTC().Add(-*freshness)
				q2 = q2.Where("updatedAt", ">=", cutoff)
			}
			q2 = q2.OrderBy("updatedAt", firestore.Desc)
			if *limit > 0 {
				q2 = q2.Limit(*limit)
			}
			docs, err = q2.Documents(ctx).GetAll()
			if err != nil {
				fmt.Fprintf(os.Stderr, "query failed (fallback also failed): %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stderr, "query failed: %v\n", err)
			os.Exit(1)
		}
	}

	var items []lensLite
	for _, d := range docs {
		var l lensLite
		if err := d.DataTo(&l); err != nil {
			continue
		}
		l.ID = d.Ref.ID
		if strings.ToLower(tenantID) == "all" || strings.TrimSpace(l.TenantID) == tenantID {
			items = append(items, l)
		}
	}

	fmt.Printf("horizon_lenses sample: fetched=%d filter(tenant=%q freshness=%s)\n", len(items), tenantID, freshness.String())
	if len(items) == 0 {
		return
	}

	// Oldest/newest by updatedAt (on sample).
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.Before(items[j].UpdatedAt) })
	oldest := items[0]
	newest := items[len(items)-1]
	fmt.Printf("updatedAt(sample): oldest=%s newest=%s\n", oldest.UpdatedAt.Format(time.RFC3339), newest.UpdatedAt.Format(time.RFC3339))
	fmt.Printf("newest(sample): id=%s project=%s status=%s name=%q updatedAt=%s createdAt=%s\n",
		newest.ID, newest.ProjectID, newest.Status, newest.Name, newest.UpdatedAt.Format(time.RFC3339), newest.CreatedAt.Format(time.RFC3339))

	// Counts by status (on sample).
	counts := map[string]int{}
	for _, it := range items {
		counts[strings.ToLower(strings.TrimSpace(it.Status))]++
	}
	type kv struct {
		K string
		V int
	}
	var buckets []kv
	for k, v := range counts {
		buckets = append(buckets, kv{K: k, V: v})
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].V > buckets[j].V })
	fmt.Println("counts(sample) by status:")
	for _, b := range buckets {
		fmt.Printf("  - %s: %d\n", b.K, b.V)
	}
}

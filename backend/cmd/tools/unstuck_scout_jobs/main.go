package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
	staleAfter := flag.Duration("stale-after", 10*time.Minute, "Consider job stuck if no update for this long (default 10m)")
	jobID := flag.String("job-id", "", "specific agent_jobs document id to mark as failed")
	dryRun := flag.Bool("dry-run", true, "if true, only prints what would be changed")
	flag.Parse()

	if *projectID == "" {
		fmt.Fprintln(os.Stderr, "--project is required (or set GCP_PROJECT_ID)")
		os.Exit(2)
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	var docs []*firestore.DocumentSnapshot
	cutoff := time.Now().UTC().Add(-*staleAfter)
	if strings.TrimSpace(*jobID) != "" {
		snap, err := db.Collection("agent_jobs").Doc(strings.TrimSpace(*jobID)).Get(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "get job failed: %v\n", err)
			os.Exit(1)
		}
		docs = append(docs, snap)
	} else {
		q := db.Collection("agent_jobs").
			Where("agentId", "==", "scout").
			Where("status", "==", "running").
			Limit(100)

		allDocs, err := q.Documents(ctx).GetAll()
		if err != nil {
			fmt.Fprintf(os.Stderr, "query failed: %v\n", err)
			os.Exit(1)
		}
		for _, d := range allDocs {
			var m map[string]interface{}
			if err := d.DataTo(&m); err != nil {
				continue
			}
			if u, ok := m["updatedAt"].(time.Time); ok && u.Before(cutoff) {
				docs = append(docs, d)
			}
		}
	}

	if len(docs) == 0 {
		if strings.TrimSpace(*jobID) != "" {
			fmt.Printf("Job %s not found or not eligible\n", strings.TrimSpace(*jobID))
		} else {
			fmt.Println("No stuck scout jobs found (status=running, updatedAt <", cutoff.Format(time.RFC3339), ")")
		}
		return
	}

	if strings.TrimSpace(*jobID) != "" {
		fmt.Printf("Found %d targeted job(s):\n", len(docs))
	} else {
		fmt.Printf("Found %d stuck job(s) (status=running, no update since %s):\n", len(docs), cutoff.Format("15:04"))
	}
	for _, d := range docs {
		var m map[string]interface{}
		_ = d.DataTo(&m)
		target := ""
		if v, ok := m["targetCanonicalPath"].(string); ok {
			target = strings.TrimSpace(v)
		}
		if target == "" {
			if st, ok := m["state"].(map[string]interface{}); ok {
				if v, ok := st["targetCanonicalPath"].(string); ok {
					target = strings.TrimSpace(v)
				}
			}
		}
		updatedAt := ""
		if v, ok := m["updatedAt"].(time.Time); ok {
			updatedAt = v.Format("2006-01-02 15:04")
		}
		fmt.Printf("  - %s target=%s updatedAt=%s\n", d.Ref.ID, target, updatedAt)
	}

	if *dryRun {
		fmt.Println("\n[DRY-RUN] Run with -dry-run=false to mark as FAILED")
		return
	}

	now := time.Now().UTC()
	for _, d := range docs {
		_, err := d.Ref.Update(ctx, []firestore.Update{
			{Path: "status", Value: "failed"},
			{Path: "updatedAt", Value: now},
			{Path: "state.currentState", Value: "FAILED"},
			{Path: "state.stuckResolvedAt", Value: now.Format(time.RFC3339)},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "update %s failed: %v\n", d.Ref.ID, err)
			continue
		}
		fmt.Printf("Marked FAILED: %s\n", d.Ref.ID)
	}
	fmt.Printf("Done: %d job(s) marked as FAILED\n", len(docs))
}

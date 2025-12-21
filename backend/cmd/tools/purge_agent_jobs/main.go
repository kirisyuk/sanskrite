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

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	agentID := flag.String("agent", "scout", "agentId to purge (default scout)")
	status := flag.String("status", "pending", "status to purge (default pending)")
	limit := flag.Int("limit", 2000, "max docs to delete")
	dryRun := flag.Bool("dry-run", true, "if true, only prints how many would be deleted")
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

	aid := strings.TrimSpace(*agentID)
	st := strings.TrimSpace(strings.ToLower(*status))

	q := db.Collection("agent_jobs").
		Where("agentId", "==", aid).
		Where("status", "==", st).
		OrderBy("createdAt", firestore.Asc)

	toDelete := *limit
	if toDelete <= 0 {
		toDelete = 2000
	}

	total := 0
	start := time.Now()
	for total < toDelete {
		batchSize := 400
		if toDelete-total < batchSize {
			batchSize = toDelete - total
		}

		docs, err := q.Limit(batchSize).Documents(ctx).GetAll()
		if err != nil {
			fmt.Fprintf(os.Stderr, "query failed: %v\n", err)
			os.Exit(1)
		}
		if len(docs) == 0 {
			break
		}

		if *dryRun {
			total += len(docs)
			// Advance query cursor
			q = q.StartAfter(docs[len(docs)-1])
			continue
		}

		wb := db.Batch()
		for _, d := range docs {
			wb.Delete(d.Ref)
		}
		if _, err := wb.Commit(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "batch delete failed: %v\n", err)
			os.Exit(1)
		}
		total += len(docs)
		q = q.StartAfter(docs[len(docs)-1])
	}

	mode := "DRY_RUN"
	if !*dryRun {
		mode = "DELETED"
	}
	fmt.Printf("%s: agent_jobs %s/%s: count=%d duration=%s\n", mode, aid, st, total, time.Since(start).Truncate(time.Millisecond))
}



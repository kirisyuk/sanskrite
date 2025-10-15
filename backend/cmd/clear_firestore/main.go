package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

func getEnv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func main() {
	ctx := context.Background()

	projectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
	if strings.TrimSpace(projectID) == "" {
		log.Fatal("GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT not set")
	}

	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore init: %v", err)
	}
	defer client.Close()

	// Collections to skip
	skip := map[string]bool{
		"core_users": true,
	}

	it := client.Collections(ctx)
	totalDeleted := 0
	for {
		col, err := it.Next()
		if err != nil {
			if err.Error() == "iterator done" || strings.Contains(strings.ToLower(err.Error()), "no more") {
				break
			}
			log.Fatalf("list collections: %v", err)
		}
		name := col.ID
		if skip[name] {
			fmt.Printf("skip collection: %s\n", name)
			continue
		}
		if strings.HasPrefix(name, "__") {
			// internal collection
			continue
		}

		// Delete all documents in this collection (best-effort, small batches)
		fmt.Printf("clearing collection: %s...\n", name)
		docs, err := col.Documents(ctx).GetAll()
		if err != nil {
			log.Printf("warn: list docs for %s: %v", name, err)
			continue
		}
		deleted := 0
		batch := client.Batch()
		for i, d := range docs {
			batch.Delete(d.Ref)
			// Commit every 400 ops to avoid limits
			if (i+1)%400 == 0 {
				if _, err := batch.Commit(ctx); err != nil {
					log.Printf("warn: batch commit for %s: %v", name, err)
				}
				batch = client.Batch()
			}
			deleted++
		}
		if deleted%400 != 0 {
			if _, err := batch.Commit(ctx); err != nil {
				log.Printf("warn: batch commit for %s: %v", name, err)
			}
		}
		totalDeleted += deleted
		fmt.Printf("collection %s: deleted %d docs\n", name, deleted)
		// Small pause to be nice
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("done. total deleted docs: %d\n", totalDeleted)
}

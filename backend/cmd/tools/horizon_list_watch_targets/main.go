package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	projectID := flag.String("project", "", "GCP project id")
	tenantID := flag.String("tenant", "", "tenant id")
	packID := flag.String("pack", "", "watch target pack id")
	filter := flag.String("filter", "", "optional case-insensitive substring filter")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if strings.TrimSpace(*tenantID) == "" {
		log.Fatal("--tenant is required")
	}
	if strings.TrimSpace(*packID) == "" {
		log.Fatal("--pack is required")
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer client.Close()

	repo := horizonrepo.NewWatchTargetsRepository(client)
	targets, err := repo.ListByPack(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*packID))
	if err != nil {
		log.Fatalf("list pack: %v", err)
	}

	sort.SliceStable(targets, func(i, j int) bool {
		left := strings.TrimSpace(targets[i].CanonicalPath)
		right := strings.TrimSpace(targets[j].CanonicalPath)
		return left < right
	})

	needle := strings.ToLower(strings.TrimSpace(*filter))
	fmt.Printf("tenant=%s pack=%s targets=%d\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*packID), len(targets))
	for _, target := range targets {
		path := strings.TrimSpace(target.CanonicalPath)
		if needle != "" && !strings.Contains(strings.ToLower(path), needle) && !strings.Contains(strings.ToLower(strings.TrimSpace(target.Label)), needle) {
			continue
		}
		fmt.Printf("%s | kind=%s | subject=%s | label=%s\n",
			path,
			strings.TrimSpace(target.Kind),
			strings.TrimSpace(target.SubjectID),
			strings.TrimSpace(target.Label),
		)
	}
}

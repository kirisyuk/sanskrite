package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	query     = flag.String("query", "", "Search query (fragment name or canonical path)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")
)

func main() {
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project is required")
	}
	if *query == "" {
		log.Fatal("--query is required")
	}

	ctx := context.Background()

	fsClient, err := firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	q := strings.TrimSpace(*query)
	log.Printf("Searching for fragments matching: %q", q)

	// Search by labels.en (name)
	searchCol := fsClient.Collection("horizon_fragments")
	iter := searchCol.Where("tenantId", "==", *tenantID).Documents(ctx)
	defer iter.Stop()

	var matches []*models.HorizonFragment
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Failed to iterate: %v", err)
		}

		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			continue
		}

		// Check if matches query
		matchesQuery := false
		if strings.Contains(strings.ToLower(fragment.CanonicalPath), strings.ToLower(q)) {
			matchesQuery = true
		}
		if name, ok := fragment.Labels["en"]; ok && strings.Contains(strings.ToLower(name), strings.ToLower(q)) {
			matchesQuery = true
		}

		if matchesQuery {
			matches = append(matches, &fragment)
		}
	}

	if len(matches) == 0 {
		log.Printf("No fragments found matching: %q", q)
		return
	}

	log.Printf("\nFound %d fragments:\n", len(matches))
	for _, f := range matches {
		name := "N/A"
		if n, ok := f.Labels["en"]; ok {
			name = n
		}
		fmt.Printf("  ID: %s\n", f.ID)
		fmt.Printf("  Name: %s\n", name)
		fmt.Printf("  CanonicalPath: %s\n", f.CanonicalPath)
		fmt.Printf("  Type: %s\n", f.Type)
		fmt.Printf("  EntityPath: %s\n", f.EntityPath)
		fmt.Printf("  SectorPath: %s\n", f.SectorPath)
		fmt.Printf("  Status: %s\n", f.Governance.Status)
		fmt.Printf("  EpistemicStatus: %s\n", f.Metadata.EpistemicStatus)
		fmt.Printf("  ---\n")
	}
}


package main

import (
	"context"
	"fmt"
	"log"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func main() {
	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, "nv-horizon")
	if err != nil {
		log.Fatal(err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	// Find all edges between ORG and PERSON
	iter := db.Collection("horizon_dependency_edges").Documents(ctx)
	wrong := 0
	total := 0
	typeCounts := make(map[string]int)
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var edge models.DependencyEdge
		if err := doc.DataTo(&edge); err != nil {
			continue
		}
		total++

		// Get source and target types
		srcDoc, _ := db.Collection("horizon_fragments").Doc(edge.SourceFragmentID).Get(ctx)
		tgtDoc, _ := db.Collection("horizon_fragments").Doc(edge.TargetFragmentID).Get(ctx)

		var srcType, tgtType string
		if srcDoc != nil {
			if t, ok := srcDoc.Data()["type"].(string); ok {
				srcType = t
			}
		}
		if tgtDoc != nil {
			if t, ok := tgtDoc.Data()["type"].(string); ok {
				tgtType = t
			}
		}

		// Only count ORG -> PERSON edges
		if srcType == "ORGANIZATION" && tgtType == "PERSON" {
			key := string(edge.Type)
			typeCounts[key]++
			wrong++
			if wrong <= 20 {
				fmt.Printf("ORG->PERSON [%s]: %s --> %s\n", edge.Type, edge.SourceFragmentID, edge.TargetFragmentID)
			}
		}
	}
	fmt.Printf("\nTotal edges scanned: %d\n", total)
	fmt.Printf("ORG -> PERSON edges: %d\n", wrong)
	fmt.Println("\nEdge types for ORG -> PERSON:")
	for k, v := range typeCounts {
		fmt.Printf("  %s: %d\n", k, v)
	}
}

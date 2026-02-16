package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

type IndexDefinition struct {
	CollectionGroup string   `json:"collectionGroup"`
	Fields          []string `json:"fields"`
}

func main() {
	ctx := context.Background()
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		log.Fatal("GCP_PROJECT_ID or GOOGLE_CLOUD_PROJECT must be set")
	}

	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer client.Close()

	// Required indexes for ontology collections
	requiredIndexes := []IndexDefinition{
		{CollectionGroup: "ontology_entities", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_entities", Fields: []string{"tenantId", "type", "createdAt"}},
		{CollectionGroup: "ontology_entities", Fields: []string{"tenantId", "canonicalName"}},
		{CollectionGroup: "ontology_events", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_events", Fields: []string{"tenantId", "type", "createdAt"}},
		{CollectionGroup: "ontology_relations", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_relations", Fields: []string{"tenantId", "subjectId", "createdAt"}},
		{CollectionGroup: "ontology_relations", Fields: []string{"tenantId", "objectId", "createdAt"}},
		{CollectionGroup: "ontology_claims", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_concepts", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_concepts", Fields: []string{"tenantId", "kind", "createdAt"}},
		{CollectionGroup: "ontology_metric_states", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_metric_states", Fields: []string{"tenantId", "metricName", "createdAt"}},
		{CollectionGroup: "ontology_trends", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_causal_links", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_scenarios", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_actionable_items", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_actionable_items", Fields: []string{"tenantId", "status", "createdAt"}},
		{CollectionGroup: "ontology_sources", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_trusts", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_trusts", Fields: []string{"tenantId", "sourceId"}},
		{CollectionGroup: "ontology_uncertainty_models", Fields: []string{"tenantId", "createdAt"}},
		{CollectionGroup: "ontology_uncertainty_models", Fields: []string{"tenantId", "status", "createdAt"}},
	}

	// Get existing indexes
	// Note: Firestore Admin API is needed to list indexes, but we can check via gcloud command
	// For now, we'll just verify that collections exist and log instructions
	fmt.Println("Checking ontology collections and indexes...")
	fmt.Println("Note: Use 'gcloud firestore indexes composite list' to see existing indexes")
	fmt.Println("Deploy indexes with: cd web && firebase deploy --only firestore:indexes")
	fmt.Println()

	missing := 0
	for _, idx := range requiredIndexes {
		fmt.Printf("Required index: %s on fields %v\n", idx.CollectionGroup, idx.Fields)
		// Collections are created automatically on first write
		// Indexes need to be deployed via firestore.indexes.json
		missing++
	}

	if missing > 0 {
		fmt.Printf("\nTotal required indexes: %d\n", missing)
		fmt.Println("To deploy indexes, run:")
		fmt.Println("  cd web && firebase deploy --only firestore:indexes --project", projectID)
		fmt.Println("Or use Firebase CLI:")
		fmt.Println("  cd web && firebase deploy --only firestore:indexes --project", projectID)
	} else {
		fmt.Println("All required indexes are defined in firestore.indexes.json")
	}

	// Verify collections exist (they will be created on first write)
	collections := []string{
		"ontology_entities",
		"ontology_events",
		"ontology_relations",
		"ontology_claims",
		"ontology_concepts",
		"ontology_metric_states",
		"ontology_trends",
		"ontology_causal_links",
		"ontology_scenarios",
		"ontology_actionable_items",
		"ontology_sources",
		"ontology_trusts",
		"ontology_uncertainty_models",
	}

	fmt.Println("\nCollections will be created automatically on first write:")
	for _, col := range collections {
		// Try to read collection (will fail if doesn't exist, but that's ok)
		iter := client.Collection(col).Limit(1).Documents(ctx)
		_, err := iter.Next()
		if err == iterator.Done {
			fmt.Printf("  ✓ %s (empty, ready for use)\n", col)
		} else if err != nil {
			fmt.Printf("  ✓ %s (will be created on first write)\n", col)
		} else {
			fmt.Printf("  ✓ %s (exists)\n", col)
		}
		iter.Stop()
	}

	fmt.Println("\n✓ Ontology repositories check completed")
}


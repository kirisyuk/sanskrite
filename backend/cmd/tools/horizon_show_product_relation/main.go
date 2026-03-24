package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
)

var (
	projectID = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID  = flag.String("tenant", "global", "tenant id")
	fromPath  = flag.String("from", "", "from product canonical path")
	toPath    = flag.String("to", "", "to product canonical path")
)

func main() {
	flag.Parse()
	if strings.TrimSpace(*fromPath) == "" || strings.TrimSpace(*toPath) == "" {
		log.Fatal("--from and --to are required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	runtime := productdict.NewRuntime(db).WithTenantID(strings.TrimSpace(*tenantID))
	resolution, err := runtime.ResolveProductRelationForTenant(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*fromPath), strings.TrimSpace(*toPath))
	if err != nil {
		log.Fatalf("resolve product relation failed: %v", err)
	}

	fmt.Printf("resolution known=%t swapped=%t from=%s to=%s kind=%s edgeType=%s\n",
		resolution.Known,
		resolution.Swapped,
		resolution.FromPath,
		resolution.ToPath,
		resolution.RelationKind,
		resolution.EdgeType,
	)

	fmt.Printf("\nforward pair docs\n")
	showPairDocs(ctx, db, strings.TrimSpace(*fromPath), strings.TrimSpace(*toPath))
	fmt.Printf("\nreverse pair docs\n")
	showPairDocs(ctx, db, strings.TrimSpace(*toPath), strings.TrimSpace(*fromPath))
}

func showPairDocs(ctx context.Context, db *firestore.Client, fromPath, toPath string) {
	docs, err := db.Collection("horizon_product_transform_pairs").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("fromPath", "==", fromPath).
		Where("toPath", "==", toPath).
		Documents(ctx).
		GetAll()
	if err != nil {
		fmt.Printf("  query error: %v\n", err)
		return
	}
	if len(docs) == 0 {
		fmt.Printf("  none\n")
		return
	}
	for _, doc := range docs {
		data := doc.Data()
		fmt.Printf("  - id=%s active=%v relationKind=%v decisionSource=%v reason=%v from=%v to=%v\n",
			doc.Ref.ID,
			data["active"],
			data["relationKind"],
			data["decisionSource"],
			data["reason"],
			data["fromPath"],
			data["toPath"],
		)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

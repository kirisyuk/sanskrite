package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

var (
	projectID = flag.String("project", "etigram", "GCP project ID")
	pathsCSV  = flag.String("paths", "", "Comma-separated canonical paths to audit")
)

func main() {
	flag.Parse()
	paths := splitCSV(*pathsCSV)
	if len(paths) == 0 {
		log.Fatal("--paths is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	for _, path := range paths {
		fmt.Printf("PATH %s\n", path)
		reportCount(ctx, db, "redirects", db.Collection("horizon_canonical_path_redirects").Where("sourcePath", "==", path).Limit(20))
		reportCount(ctx, db, "frag.productRef", db.Collection("horizon_fragments").Where("productRef", "==", path).Limit(20))
		reportCount(ctx, db, "frag.supplierRef", db.Collection("horizon_fragments").Where("supplierRef", "==", path).Limit(20))
		reportCount(ctx, db, "frag.customerRef", db.Collection("horizon_fragments").Where("customerRef", "==", path).Limit(20))
		reportCount(ctx, db, "frag.transportViaRefs", db.Collection("horizon_fragments").Where("transportViaRefs", "array-contains", path).Limit(20))
		reportCount(ctx, db, "mat.nodeRefs", db.Collection("horizon_materialized_artifacts").Where("nodeRefs", "array-contains", path).Limit(20))
		reportCount(ctx, db, "mat.productAxis", db.Collection("horizon_materialized_artifacts").Where("productAxis", "==", path).Limit(20))
		fmt.Println()
	}
}

func reportCount(ctx context.Context, db *firestore.Client, label string, q firestore.Query) {
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		fmt.Printf("  %s ERROR %v\n", label, err)
		return
	}
	fmt.Printf("  %s %d\n", label, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		typ := strings.TrimSpace(fmt.Sprint(data["type"]))
		cp := strings.TrimSpace(fmt.Sprint(data["canonicalPath"]))
		if typ != "" || cp != "" {
			fmt.Printf("    - %s type=%s canonicalPath=%s\n", doc.Ref.ID, typ, cp)
			continue
		}
		fmt.Printf("    - %s\n", doc.Ref.ID)
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

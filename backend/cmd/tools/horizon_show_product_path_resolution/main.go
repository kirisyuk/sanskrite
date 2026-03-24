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
	path      = flag.String("path", "", "product canonical path to inspect")
)

func main() {
	flag.Parse()
	if strings.TrimSpace(*path) == "" {
		log.Fatal("--path is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	dict := productdict.NewRuntime(db).WithTenantID(strings.TrimSpace(*tenantID))
	canonical, matched, err := dict.CanonicalizeProductPathForTenant(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*path))
	if err != nil {
		log.Fatalf("canonicalize failed: %v", err)
	}
	known, err := dict.IsKnownProductPathForTenant(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*path))
	if err != nil {
		log.Fatalf("known-path lookup failed: %v", err)
	}

	fmt.Printf("path=%s\n", strings.TrimSpace(*path))
	fmt.Printf("known=%t canonical=%s matched=%t\n", known, canonical, matched)

	fmt.Printf("\nsynonym docs by synonymPath\n")
	showDocs(ctx, db.Collection("horizon_product_synonym_pairs").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("synonymPath", "==", strings.TrimSpace(*path)))
	fmt.Printf("\nsynonym docs by canonicalPath\n")
	showDocs(ctx, db.Collection("horizon_product_synonym_pairs").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("canonicalPath", "==", strings.TrimSpace(*path)))
}

func showDocs(ctx context.Context, q firestore.Query) {
	docs, err := q.Documents(ctx).GetAll()
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
		fmt.Printf("  - id=%s active=%v canonicalPath=%v synonymPath=%v decisionSource=%v reason=%v\n",
			doc.Ref.ID,
			data["active"],
			data["canonicalPath"],
			data["synonymPath"],
			data["decisionSource"],
			data["reason"],
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

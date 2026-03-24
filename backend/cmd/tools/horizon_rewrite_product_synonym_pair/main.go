package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
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
	projectID        = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID         = flag.String("tenant", "global", "tenant id")
	apply            = flag.Bool("apply", false, "apply writes")
	oldCanonicalPath = flag.String("old-canonical", "", "existing canonicalPath to deactivate")
	oldSynonymPath   = flag.String("old-synonym", "", "existing synonymPath to deactivate")
	newCanonicalPath = flag.String("new-canonical", "", "replacement canonicalPath to activate")
	newSynonymPath   = flag.String("new-synonym", "", "replacement synonymPath to activate")
	reason           = flag.String("reason", "manual_rewrite_after_supply_chain_relation_repair", "rewrite reason")
)

func main() {
	flag.Parse()

	if strings.TrimSpace(*oldCanonicalPath) == "" || strings.TrimSpace(*oldSynonymPath) == "" {
		log.Fatal("--old-canonical and --old-synonym are required")
	}
	if (strings.TrimSpace(*newCanonicalPath) == "") != (strings.TrimSpace(*newSynonymPath) == "") {
		log.Fatal("--new-canonical and --new-synonym must be provided together")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	dict := productdict.NewRuntime(db).WithTenantID(strings.TrimSpace(*tenantID))

	fmt.Printf("rewrite tenant=%s apply=%t\n", strings.TrimSpace(*tenantID), *apply)
	fmt.Printf("  deactivate: canonical=%s synonym=%s\n", strings.TrimSpace(*oldCanonicalPath), strings.TrimSpace(*oldSynonymPath))
	if strings.TrimSpace(*newCanonicalPath) != "" {
		fmt.Printf("  activate:   canonical=%s synonym=%s\n", strings.TrimSpace(*newCanonicalPath), strings.TrimSpace(*newSynonymPath))
	}
	if !*apply {
		return
	}

	if err := rewritePair(ctx, db, dict, strings.TrimSpace(*oldCanonicalPath), strings.TrimSpace(*oldSynonymPath), false, strings.TrimSpace(*reason)); err != nil {
		log.Fatalf("failed to deactivate old synonym pair: %v", err)
	}
	if strings.TrimSpace(*newCanonicalPath) != "" {
		if err := rewritePair(ctx, db, dict, strings.TrimSpace(*newCanonicalPath), strings.TrimSpace(*newSynonymPath), true, strings.TrimSpace(*reason)); err != nil {
			log.Fatalf("failed to activate new synonym pair: %v", err)
		}
	}
}

func rewritePair(ctx context.Context, db *firestore.Client, dict *productdict.Runtime, canonicalPath, synonymPath string, active bool, reason string) error {
	docID := stableDocID("product_synonym", strings.TrimSpace(*tenantID), canonicalPath, synonymPath)
	record := map[string]interface{}{
		"tenantId":       strings.TrimSpace(*tenantID),
		"canonicalPath":  canonicalPath,
		"synonymPath":    synonymPath,
		"active":         active,
		"decisionSource": "tool:horizon_rewrite_product_synonym_pair",
		"reason":         reason,
		"updatedAt":      firestore.ServerTimestamp,
	}
	if _, err := db.Collection("horizon_product_synonym_pairs").Doc(docID).Set(ctx, record, firestore.MergeAll); err != nil {
		return err
	}
	return dict.SyncSynonymPairWrite(ctx, strings.TrimSpace(*tenantID), canonicalPath, synonymPath, active)
}

func stableDocID(prefix string, values ...string) string {
	key := prefix + "|" + strings.Join(values, "|")
	sum := sha1String(key)
	if len(sum) > 20 {
		sum = sum[:20]
	}
	return prefix + "_" + sum
}

func sha1String(value string) string {
	h := sha1.Sum([]byte(value))
	return hex.EncodeToString(h[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

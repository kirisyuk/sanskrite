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
	projectID    = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID     = flag.String("tenant", "global", "tenant id")
	apply        = flag.Bool("apply", false, "apply writes")
	oldFromPath  = flag.String("old-from", "", "existing fromPath to deactivate")
	oldToPath    = flag.String("old-to", "", "existing toPath to deactivate")
	newFromPath  = flag.String("new-from", "", "replacement fromPath to activate")
	newToPath    = flag.String("new-to", "", "replacement toPath to activate")
	relationKind = flag.String("relation-kind", "transformation", "relation kind for replacement doc")
	reason       = flag.String("reason", "manual_rewrite_after_supply_chain_relation_repair", "rewrite reason")
)

func main() {
	flag.Parse()

	if strings.TrimSpace(*oldFromPath) == "" || strings.TrimSpace(*oldToPath) == "" {
		log.Fatal("--old-from and --old-to are required")
	}
	if (strings.TrimSpace(*newFromPath) == "") != (strings.TrimSpace(*newToPath) == "") {
		log.Fatal("--new-from and --new-to must be provided together")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	dict := productdict.NewRuntime(db).WithTenantID(strings.TrimSpace(*tenantID))

	oldDocID := pairDocID(strings.TrimSpace(*oldFromPath), strings.TrimSpace(*oldToPath))
	newDocID := ""
	if strings.TrimSpace(*newFromPath) != "" && strings.TrimSpace(*newToPath) != "" {
		newDocID = pairDocID(strings.TrimSpace(*newFromPath), strings.TrimSpace(*newToPath))
	}

	fmt.Printf("rewrite tenant=%s apply=%t\n", strings.TrimSpace(*tenantID), *apply)
	fmt.Printf("  deactivate: %s -> %s (doc=%s)\n", strings.TrimSpace(*oldFromPath), strings.TrimSpace(*oldToPath), oldDocID)
	if newDocID != "" {
		fmt.Printf("  activate:   %s -> %s (doc=%s) relation=%s\n", strings.TrimSpace(*newFromPath), strings.TrimSpace(*newToPath), newDocID, strings.TrimSpace(*relationKind))
	}

	if !*apply {
		return
	}

	oldUpdate := map[string]interface{}{
		"tenantId":        strings.TrimSpace(*tenantID),
		"fromPath":        strings.TrimSpace(*oldFromPath),
		"toPath":          strings.TrimSpace(*oldToPath),
		"active":          false,
		"decisionSource":  "tool:horizon_rewrite_product_transform_pair",
		"rewriteReason":   strings.TrimSpace(*reason),
		"updatedAt":       firestore.ServerTimestamp,
	}
	if _, err := db.Collection("horizon_product_transform_pairs").Doc(oldDocID).Set(ctx, oldUpdate, firestore.MergeAll); err != nil {
		log.Fatalf("failed to deactivate old pair doc: %v", err)
	}
	if err := dict.SyncTransformPairWrite(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*oldFromPath), strings.TrimSpace(*oldToPath), strings.TrimSpace(*relationKind), false); err != nil {
		log.Fatalf("failed to sync old pair deactivation: %v", err)
	}

	if newDocID == "" {
		return
	}

	record := map[string]interface{}{
		"tenantId":       strings.TrimSpace(*tenantID),
		"fromPath":       strings.TrimSpace(*newFromPath),
		"toPath":         strings.TrimSpace(*newToPath),
		"relationKind":   strings.TrimSpace(*relationKind),
		"decisionSource": "tool:horizon_rewrite_product_transform_pair",
		"reason":         strings.TrimSpace(*reason),
		"confidence":     1.0,
		"active":         true,
		"updatedAt":      firestore.ServerTimestamp,
	}
	if _, err := db.Collection("horizon_product_transform_pairs").Doc(newDocID).Set(ctx, record, firestore.MergeAll); err != nil {
		log.Fatalf("failed to upsert new pair doc: %v", err)
	}
	if err := dict.SyncTransformPairWrite(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*newFromPath), strings.TrimSpace(*newToPath), strings.TrimSpace(*relationKind), true); err != nil {
		log.Fatalf("failed to sync new pair activation: %v", err)
	}
}

func pairDocID(fromPath, toPath string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(fromPath) + "\n" + strings.TrimSpace(toPath)))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

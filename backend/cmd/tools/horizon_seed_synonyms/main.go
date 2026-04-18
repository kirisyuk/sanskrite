// horizon_seed_synonyms seeds the `horizon_synonym_pairs` Firestore
// collection from a JSON file. One document per alias→canonical pair.
//
// Usage:
//
//	go run ./cmd/tools/horizon_seed_synonyms \
//	  -project=$GOOGLE_CLOUD_PROJECT \
//	  -file=seeds/synonyms.json \
//	  [-dry-run]
//
// The JSON file must be an array of records. Each record shape:
//
//	{
//	  "aliasSurface": "TSMC",
//	  "canonicalPath": "/org/tw/taiwan-semiconductor-manufacturing-company",
//	  "tenantId": "global"   // optional, defaults to "global"
//	}
//
// Running the tool is idempotent: a deterministic doc id (sha1 of
// tenant+alias) means repeated runs overwrite rather than duplicate.
package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type seedRow struct {
	AliasSurface  string `json:"aliasSurface"`
	AliasPath     string `json:"aliasPath"`
	CanonicalPath string `json:"canonicalPath"`
	TenantID      string `json:"tenantId"`
}

func docID(tenantID, alias string) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(tenantID)) + "\x00" + strings.ToLower(strings.TrimSpace(alias))))
	return hex.EncodeToString(sum[:])
}

func main() {
	project := flag.String("project", os.Getenv("GOOGLE_CLOUD_PROJECT"), "GCP project id")
	path := flag.String("file", "", "path to seed JSON file")
	dryRun := flag.Bool("dry-run", false, "only validate, no writes")
	flag.Parse()

	if *path == "" {
		log.Fatal("-file is required")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		log.Fatalf("read %s: %v", *path, err)
	}
	var rows []seedRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		log.Fatalf("parse %s: %v", *path, err)
	}
	if len(rows) == 0 {
		log.Fatalf("%s: no rows", *path)
	}

	// Validate all rows up front.
	for i, r := range rows {
		if err := validateRow(r); err != nil {
			log.Fatalf("row %d invalid: %v", i, err)
		}
	}
	fmt.Printf("validated %d rows\n", len(rows))
	if *dryRun {
		return
	}
	if *project == "" {
		log.Fatal("-project or GOOGLE_CLOUD_PROJECT is required (or use -dry-run)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := firestore.NewClient(ctx, *project)
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer client.Close()

	col := client.Collection("horizon_synonym_pairs")
	now := firestore.ServerTimestamp
	written := 0
	for _, r := range rows {
		tenant := r.TenantID
		if tenant == "" {
			tenant = "global"
		}
		alias := firstNonEmpty(r.AliasSurface, r.AliasPath)
		id := docID(tenant, alias)
		update := map[string]interface{}{
			"tenantId":      tenant,
			"aliasSurface":  r.AliasSurface,
			"aliasPath":     r.AliasPath,
			"canonicalPath": strings.TrimSpace(r.CanonicalPath),
			"active":        true,
			"updatedAt":     now,
		}
		if _, err := col.Doc(id).Set(ctx, update, firestore.MergeAll); err != nil {
			log.Fatalf("set %s: %v", id, err)
		}
		written++
	}
	fmt.Printf("wrote %d synonym pairs to horizon_synonym_pairs\n", written)
}

func validateRow(r seedRow) error {
	canon := strings.TrimSpace(r.CanonicalPath)
	if canon == "" {
		return errors.New("canonicalPath is required")
	}
	if err := taxonomy.ValidateCanonicalPath(canon); err != nil {
		return fmt.Errorf("canonicalPath %q: %w", canon, err)
	}
	if strings.TrimSpace(r.AliasSurface) == "" && strings.TrimSpace(r.AliasPath) == "" {
		return errors.New("aliasSurface or aliasPath is required")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

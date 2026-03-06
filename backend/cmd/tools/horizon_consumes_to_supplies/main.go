package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// horizon_consumes_to_supplies — миграция CONSUMES → SUPPLIES_TO с разворотом направления.
// CONSUMES(org→product) → SUPPLIES_TO(product→org).
// CONSUMES удалён из таксономии; каноническая форма: product supplies to org.
//
// Использование:
//
//	go run ./cmd/tools/horizon_consumes_to_supplies --project=PROJECT [--apply] [--out=report.jsonl]

var (
	projectID   = flag.String("project", "", "GCP project ID (required)")
	tenantID    = flag.String("tenant", "global", "Tenant ID filter")
	apply       = flag.Bool("apply", false, "Apply changes to Firestore (default: dry-run)")
	outPath     = flag.String("out", "", "Optional output JSONL report path")
	pageSize    = flag.Int("page-size", 500, "Firestore page size")
	progressN   = flag.Int("progress-every", 100, "Print progress every N edges (0 disables)")
	startAfter  = flag.String("start-after", "", "Resume cursor: start after this document ID")
)

type ReportRow struct {
	EdgeID           string    `json:"edgeId"`
	SourceFragmentID string    `json:"sourceFragmentId"`
	TargetFragmentID string    `json:"targetFragmentId"`
	Action           string    `json:"action"` // "migrated" | "skipped" | "error"
	Reason           string    `json:"reason,omitempty"`
	NewEdgeID        string    `json:"newEdgeId,omitempty"`
	Applied          bool      `json:"applied,omitempty"`
	ScannedAt        time.Time `json:"scannedAt"`
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	var out *os.File
	if strings.TrimSpace(*outPath) != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("FATAL: failed to create output file: %v", err)
		}
		defer f.Close()
		out = f
	} else {
		out = os.Stdout
	}
	enc := json.NewEncoder(out)

	log.Printf("=== Horizon: CONSUMES → SUPPLIES_TO migration (swap direction) ===")
	log.Printf("project=%s tenant=%s apply=%v", *projectID, *tenantID, *apply)

	migrated, scanned, err := run(ctx, db, enc)
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}
	log.Printf("Done. scanned=%d migrated=%d", scanned, migrated)
}

func run(ctx context.Context, db *firestore.Client, enc *json.Encoder) (migrated, scanned int, err error) {
	col := db.Collection("horizon_dependency_edges")
	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}

	q := col.Where("tenantId", "==", tenant).
		Where("type", "==", "CONSUMES").
		OrderBy(firestore.DocumentID, firestore.Asc)

	var lastDoc *firestore.DocumentSnapshot
	if s := strings.TrimSpace(*startAfter); s != "" {
		snap, err := col.Doc(s).Get(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("start-after doc %q: %w", s, err)
		}
		lastDoc = snap
	}

	for {
		pageQ := q.Limit(*pageSize)
		if lastDoc != nil {
			pageQ = pageQ.StartAfter(lastDoc)
		}
		iter := pageQ.Documents(ctx)
		pageCount := 0

		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				return migrated, scanned, err
			}
			pageCount++
			scanned++
			lastDoc = doc

			var edge hmodels.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				_ = enc.Encode(&ReportRow{
					EdgeID: doc.Ref.ID,
					Action: "error",
					Reason: "DataTo: " + err.Error(),
					ScannedAt: time.Now().UTC(),
				})
				continue
			}
			if edge.ID == "" {
				edge.ID = doc.Ref.ID
			}

			row := processEdge(ctx, db, col, &edge)
			if row != nil {
				_ = enc.Encode(row)
				if row.Applied {
					migrated++
				}
			}

			if *progressN > 0 && scanned%*progressN == 0 {
				log.Printf("progress: scanned=%d migrated=%d lastDoc=%s", scanned, migrated, doc.Ref.ID)
			}
		}
		iter.Stop()
		if pageCount < *pageSize {
			break
		}
	}
	return migrated, scanned, nil
}

func processEdge(ctx context.Context, db *firestore.Client, col *firestore.CollectionRef, edge *hmodels.DependencyEdge) *ReportRow {
	now := time.Now().UTC()
	row := &ReportRow{
		EdgeID:           strings.TrimSpace(edge.ID),
		SourceFragmentID: strings.TrimSpace(edge.SourceFragmentID),
		TargetFragmentID: strings.TrimSpace(edge.TargetFragmentID),
		ScannedAt:        now,
	}

	if row.EdgeID == "" || row.SourceFragmentID == "" || row.TargetFragmentID == "" {
		row.Action = "skipped"
		row.Reason = "invalid edge ids"
		return row
	}

	// CONSUMES: source=org/fac/product, target=product
	// SUPPLIES_TO: new source=product (old target), new target=org/fac (old source)
	newSourceID := row.TargetFragmentID // product
	newTargetID := row.SourceFragmentID // org/fac

	// Check if SUPPLIES_TO already exists (product→org)
	existing := col.Where("tenantId", "==", strings.TrimSpace(edge.TenantID)).
		Where("type", "==", "SUPPLIES_TO").
		Where("sourceFragmentId", "==", newSourceID).
		Where("targetFragmentId", "==", newTargetID).
		Limit(1)
	iter := existing.Documents(ctx)
	existingDoc, err := iter.Next()
	iter.Stop()
	if err != nil && err != iterator.Done {
		row.Action = "error"
		row.Reason = "check existing: " + err.Error()
		return row
	}
	if existingDoc != nil {
		row.Action = "skipped"
		row.Reason = "SUPPLIES_TO already exists: " + existingDoc.Ref.ID
		return row
	}

	row.Action = "migrated"
	row.Reason = "CONSUMES -> SUPPLIES_TO (product supplies to org)"

	if !*apply {
		return row
	}

	// Create new SUPPLIES_TO edge
	newEdge := &hmodels.DependencyEdge{
		TenantID:         edge.TenantID,
		SourceFragmentID: newSourceID,
		TargetFragmentID: newTargetID,
		Type:             hmodels.DependencyTypeSuppliesTo,
		Properties:       edge.Properties,
		Qualifiers:       copyQualifiers(edge.Qualifiers),
		Provenance:       edge.Provenance,
		ValidFrom:        edge.ValidFrom,
		ValidTo:          edge.ValidTo,
		Metadata:         edge.Metadata,
		Metrics:          edge.Metrics,
	}
	if newEdge.Qualifiers == nil {
		newEdge.Qualifiers = map[string]interface{}{}
	}
	newEdge.Qualifiers["migratedFromConsumesAt"] = now.Format(time.RFC3339)
	newEdge.Qualifiers["migratedFromConsumesEdgeId"] = row.EdgeID
	newEdge.Qualifiers["migratedBy"] = "tool:horizon_consumes_to_supplies"
	newEdge.Metadata.CreatedAt = now
	newEdge.Metadata.LastUpdatedAt = now

	newDoc := col.NewDoc()
	newEdge.ID = newDoc.ID
	if _, err := newDoc.Set(ctx, newEdge); err != nil {
		row.Action = "error"
		row.Reason = "create SUPPLIES_TO: " + err.Error()
		return row
	}
	row.NewEdgeID = newEdge.ID

	// Delete old CONSUMES edge
	if _, err := col.Doc(row.EdgeID).Delete(ctx); err != nil {
		row.Action = "error"
		row.Reason = "delete CONSUMES: " + err.Error()
		return row
	}

	row.Applied = true
	return row
}

func copyQualifiers(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

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
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID  = flag.String("project", "", "GCP project ID (required)")
	tenantID   = flag.String("tenant", "global", "Tenant ID filter")
	apply      = flag.Bool("apply", false, "Apply changes to Firestore (default dry-run)")
	pageSize   = flag.Int("page-size", 500, "Firestore page size")
	progressN  = flag.Int("progress-every", 100, "Log progress every N rows")
	startAfter = flag.String("start-after", "", "Resume cursor: start after this edge document ID")
	outPath    = flag.String("out", "", "Optional output JSONL report path")
)

type reportRow struct {
	EdgeID       string    `json:"edgeId"`
	Action       string    `json:"action"`
	Reason       string    `json:"reason,omitempty"`
	OldType      string    `json:"oldType,omitempty"`
	NewType      string    `json:"newType,omitempty"`
	OldSourceID  string    `json:"oldSourceId,omitempty"`
	OldTargetID  string    `json:"oldTargetId,omitempty"`
	NewSourceID  string    `json:"newSourceId,omitempty"`
	NewTargetID  string    `json:"newTargetId,omitempty"`
	Applied      bool      `json:"applied,omitempty"`
	ScannedAt    time.Time `json:"scannedAt"`
	SourcePath   string    `json:"sourcePath,omitempty"`
	TargetPath   string    `json:"targetPath,omitempty"`
	ExistingEdge string    `json:"existingEdge,omitempty"`
}

type runStats struct {
	Scanned           int `json:"scanned"`
	NonProductMarket  int `json:"nonProductMarket"`
	AlreadyCanonical  int `json:"alreadyCanonical"`
	RewriteCandidates int `json:"rewriteCandidates"`
	DeleteDuplicate   int `json:"deleteDuplicate"`
	Errors            int `json:"errors"`
	Migrated          int `json:"migrated"`
	Deleted           int `json:"deleted"`
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("init firestore: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	fragmentsRepo := repo.NewHorizonFragmentsRepository(db)
	edgesRepo := repo.NewDependencyEdgesRepository(db)

	var out *os.File
	if strings.TrimSpace(*outPath) != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("create out file: %v", err)
		}
		defer f.Close()
		out = f
	} else {
		out = os.Stdout
	}
	enc := json.NewEncoder(out)

	stats, err := run(ctx, db, fragmentsRepo, edgesRepo, enc)
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	log.Printf("done scanned=%d migrated=%d deleted=%d rewriteCandidates=%d alreadyCanonical=%d nonProductMarket=%d errors=%d", stats.Scanned, stats.Migrated, stats.Deleted, stats.RewriteCandidates, stats.AlreadyCanonical, stats.NonProductMarket, stats.Errors)
}

func run(
	ctx context.Context,
	db *firestore.Client,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	enc *json.Encoder,
) (runStats, error) {
	stats := runStats{}
	col := db.Collection("horizon_dependency_edges")
	q := col.Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		OrderBy(firestore.DocumentID, firestore.Asc)

	var lastDoc *firestore.DocumentSnapshot
	if s := strings.TrimSpace(*startAfter); s != "" {
		snap, err := col.Doc(s).Get(ctx)
		if err != nil {
			return stats, fmt.Errorf("start-after doc %q: %w", s, err)
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
				return stats, err
			}
			pageCount++
			stats.Scanned++
			lastDoc = doc

			var edge hmodels.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				stats.Errors++
				_ = enc.Encode(reportRow{EdgeID: doc.Ref.ID, Action: "error", Reason: "DataTo: " + err.Error(), ScannedAt: time.Now().UTC()})
				continue
			}
			if strings.TrimSpace(edge.ID) == "" {
				edge.ID = doc.Ref.ID
			}

			row, didMigrate, didDelete := processEdge(ctx, col, fragmentsRepo, edgesRepo, &edge, &stats)
			if row != nil {
				_ = enc.Encode(row)
			}
			if didMigrate {
				stats.Migrated++
			}
			if didDelete {
				stats.Deleted++
			}
			if *progressN > 0 && stats.Scanned%*progressN == 0 {
				log.Printf("progress scanned=%d migrated=%d deleted=%d rewriteCandidates=%d alreadyCanonical=%d lastDoc=%s", stats.Scanned, stats.Migrated, stats.Deleted, stats.RewriteCandidates, stats.AlreadyCanonical, doc.Ref.ID)
			}
		}
		iter.Stop()
		if pageCount < *pageSize {
			break
		}
	}
	_ = enc.Encode(map[string]interface{}{
		"type":              "summary",
		"scanned":           stats.Scanned,
		"nonProductMarket":  stats.NonProductMarket,
		"alreadyCanonical":  stats.AlreadyCanonical,
		"rewriteCandidates": stats.RewriteCandidates,
		"deleteDuplicate":   stats.DeleteDuplicate,
		"errors":            stats.Errors,
		"migrated":          stats.Migrated,
		"deleted":           stats.Deleted,
	})
	return stats, nil
}

func processEdge(
	ctx context.Context,
	col *firestore.CollectionRef,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	edge *hmodels.DependencyEdge,
	stats *runStats,
) (*reportRow, bool, bool) {
	now := time.Now().UTC()
	row := &reportRow{
		EdgeID:      strings.TrimSpace(edge.ID),
		OldType:     string(edge.Type),
		OldSourceID: strings.TrimSpace(edge.SourceFragmentID),
		OldTargetID: strings.TrimSpace(edge.TargetFragmentID),
		ScannedAt:   now,
	}
	if row.EdgeID == "" || row.OldSourceID == "" || row.OldTargetID == "" {
		stats.Errors++
		row.Action = "error"
		row.Reason = "invalid edge ids"
		return row, false, false
	}

	sourceFrag, err := fragmentsRepo.Get(ctx, row.OldSourceID)
	if err != nil || sourceFrag == nil {
		stats.Errors++
		row.Action = "error"
		row.Reason = "missing source fragment"
		return row, false, false
	}
	targetFrag, err := fragmentsRepo.Get(ctx, row.OldTargetID)
	if err != nil || targetFrag == nil {
		stats.Errors++
		row.Action = "error"
		row.Reason = "missing target fragment"
		return row, false, false
	}
	row.SourcePath = strings.TrimSpace(sourceFrag.CanonicalPath)
	row.TargetPath = strings.TrimSpace(targetFrag.CanonicalPath)

	srcType := sourceFrag.Type
	dstType := targetFrag.Type
	isProductMarket := (srcType == hmodels.FragmentTypeProduct && dstType == hmodels.FragmentTypeMarket) ||
		(srcType == hmodels.FragmentTypeMarket && dstType == hmodels.FragmentTypeProduct)
	if !isProductMarket {
		stats.NonProductMarket++
		return nil, false, false
	}

	if edge.Type == hmodels.DependencyTypeTradedIn &&
		srcType == hmodels.FragmentTypeProduct &&
		dstType == hmodels.FragmentTypeMarket {
		stats.AlreadyCanonical++
		row.Action = "skipped"
		row.Reason = "already canonical traded_in"
		return row, false, false
	}

	newSourceID := row.OldSourceID
	newTargetID := row.OldTargetID
	if srcType == hmodels.FragmentTypeMarket && dstType == hmodels.FragmentTypeProduct {
		newSourceID = row.OldTargetID
		newTargetID = row.OldSourceID
	}

	existing, err := edgesRepo.GetBySourceTargetAndType(ctx, strings.TrimSpace(edge.TenantID), newSourceID, newTargetID, hmodels.DependencyTypeTradedIn)
	if err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" && existing.ID != row.EdgeID {
		stats.DeleteDuplicate++
		row.Action = "delete_duplicate"
		row.Reason = "canonical TRADED_IN already exists"
		row.ExistingEdge = existing.ID
		row.NewType = string(hmodels.DependencyTypeTradedIn)
		row.NewSourceID = newSourceID
		row.NewTargetID = newTargetID
		if !*apply {
			return row, false, false
		}
		if _, err := col.Doc(row.EdgeID).Delete(ctx); err != nil {
			stats.Errors++
			row.Action = "error"
			row.Reason = "delete duplicate: " + err.Error()
			return row, false, false
		}
		row.Applied = true
		return row, false, true
	}

	row.Action = "rewrite"
	row.Reason = "product-market edges must use TRADED_IN"
	stats.RewriteCandidates++
	row.NewType = string(hmodels.DependencyTypeTradedIn)
	row.NewSourceID = newSourceID
	row.NewTargetID = newTargetID

	if !*apply {
		return row, false, false
	}

	edge.Type = hmodels.DependencyTypeTradedIn
	edge.SourceFragmentID = newSourceID
	edge.TargetFragmentID = newTargetID
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	edge.Qualifiers["canonicalized"] = true
	edge.Qualifiers["canonicalizedFromEdgeType"] = row.OldType
	edge.Qualifiers["canonicalizationReason"] = row.Reason
	edge.Qualifiers["migratedBy"] = "tool:horizon_rewrite_product_market_edges"
	edge.Qualifiers["migratedAt"] = now.Format(time.RFC3339)
	edge.Metadata.LastUpdatedAt = now

	if _, err := col.Doc(row.EdgeID).Set(ctx, edge); err != nil {
		stats.Errors++
		row.Action = "error"
		row.Reason = "rewrite edge: " + err.Error()
		return row, false, false
	}
	row.Applied = true
	return row, true, false
}

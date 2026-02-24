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
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// horizon_fix_administers_facility_direction — исправляет исторические рёбра с неверным направлением:
// ADMINISTERS: FACILITY/DEPOSIT→ORG, /org/→/gov/
// CONTROLS: PRODUCT→ORG/GOV
//
// Использование:
//
//	go run ./cmd/tools/horizon_fix_administers_facility_direction --project=PROJECT [--apply] [--out=report.jsonl]

var (
	projectID   = flag.String("project", "", "GCP project ID (required)")
	tenantID    = flag.String("tenant", "global", "Tenant ID")
	apply       = flag.Bool("apply", false, "Apply changes to Firestore (default: dry-run)")
	outPath     = flag.String("out", "", "Optional output JSONL report path")
	pageSize    = flag.Int("page-size", 500, "Firestore page size")
	progressN   = flag.Int("progress-every", 100, "Print progress every N edges (0 disables)")
	startAfter  = flag.String("start-after", "", "Resume cursor: start after this document ID")
	edgeTypes   = flag.String("edge-types", "ADMINISTERS,CONTROLS", "Comma-separated: ADMINISTERS, CONTROLS")
)

type ReportRow struct {
	EdgeID           string    `json:"edgeId"`
	EdgeType         string    `json:"edgeType,omitempty"`
	SourceFragmentID string    `json:"sourceFragmentId"`
	TargetFragmentID string    `json:"targetFragmentId"`
	SourceType       string    `json:"sourceType"`
	TargetType       string    `json:"targetType"`
	SourcePath       string    `json:"sourcePath,omitempty"`
	TargetPath       string    `json:"targetPath,omitempty"`
	Action           string    `json:"action"` // "swapped" | "skipped" | "error"
	Reason           string    `json:"reason,omitempty"`
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

	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := hrepo.NewDependencyEdgesRepository(db).WithFragmentsRepo(fragmentsRepo)

	typesList := parseEdgeTypes(*edgeTypes)
	if len(typesList) == 0 {
		log.Fatal("--edge-types resulted in empty list")
	}

	log.Printf("=== Horizon: fix edge direction (ADMINISTERS, CONTROLS) ===")
	log.Printf("project=%s tenant=%s apply=%v edgeTypes=%v", *projectID, *tenantID, *apply, typesList)

	fixed, scanned, err := run(ctx, db, edgesRepo, fragmentsRepo, enc, typesList)
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}
	log.Printf("Done. scanned=%d fixed=%d", scanned, fixed)
}

func parseEdgeTypes(s string) []hmodels.DependencyType {
	parts := strings.Split(s, ",")
	out := make([]hmodels.DependencyType, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, hmodels.DependencyType(p))
	}
	return out
}

func run(
	ctx context.Context,
	db *firestore.Client,
	edgesRepo *hrepo.DependencyEdgesRepository,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	enc *json.Encoder,
	edgeTypesList []hmodels.DependencyType,
) (fixed, scanned int, err error) {
	col := db.Collection("horizon_dependency_edges")

	for _, edgeType := range edgeTypesList {
		q := col.Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Where("type", "==", string(edgeType)).
			OrderBy(firestore.DocumentID, firestore.Asc)

		var lastDoc *firestore.DocumentSnapshot
		if s := strings.TrimSpace(*startAfter); s != "" && scanned == 0 {
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
					return fixed, scanned, err
				}
				pageCount++
				scanned++
				lastDoc = doc

				var edge hmodels.DependencyEdge
				if err := doc.DataTo(&edge); err != nil {
					continue
				}
				if edge.ID == "" {
					edge.ID = doc.Ref.ID
				}

				row := processEdge(ctx, edgesRepo, fragmentsRepo, &edge)
				if row != nil {
					row.EdgeType = string(edge.Type)
					_ = enc.Encode(row)
					if row.Applied {
						fixed++
					}
				}

				if *progressN > 0 && scanned%*progressN == 0 {
					log.Printf("progress: scanned=%d fixed=%d lastDoc=%s", scanned, fixed, doc.Ref.ID)
				}
			}
			iter.Stop()
			if pageCount < *pageSize {
				break
			}
		}
	}
	return fixed, scanned, nil
}

func processEdge(
	ctx context.Context,
	edgesRepo *hrepo.DependencyEdgesRepository,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	edge *hmodels.DependencyEdge,
) *ReportRow {
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

	srcFrag, err1 := fragmentsRepo.Get(ctx, row.SourceFragmentID)
	tgtFrag, err2 := fragmentsRepo.Get(ctx, row.TargetFragmentID)
	if err1 != nil || err2 != nil || srcFrag == nil || tgtFrag == nil {
		row.Action = "skipped"
		row.Reason = "missing fragments"
		return row
	}

	row.SourceType = string(srcFrag.Type)
	row.TargetType = string(tgtFrag.Type)
	row.SourcePath = strings.TrimSpace(srcFrag.CanonicalPath)
	row.TargetPath = strings.TrimSpace(tgtFrag.CanonicalPath)

	tgtValidAdmin := tgtFrag.Type == hmodels.FragmentTypeOrganization ||
		tgtFrag.Type == hmodels.FragmentTypeProgram ||
		tgtFrag.Type == hmodels.FragmentTypeGeoRegion
	tgtValidControls := tgtFrag.Type == hmodels.FragmentTypeOrganization || tgtFrag.Type == hmodels.FragmentTypeFacility

	var needSwap bool
	var fixReason string

	switch edge.Type {
	case hmodels.DependencyTypeAdministers:
		// 1. FACILITY → ORG/PROGRAM/GEO
		if srcFrag.Type == hmodels.FragmentTypeFacility && tgtValidAdmin {
			needSwap = true
			fixReason = "FACILITY ADMINISTERS ORG -> ORG ADMINISTERS FACILITY (swap)"
		}
		// 2. DEPOSIT → ORG/PROGRAM/GEO
		if srcFrag.Type == hmodels.FragmentTypeDeposit && tgtValidAdmin {
			needSwap = true
			fixReason = "DEPOSIT ADMINISTERS ORG -> ORG ADMINISTERS DEPOSIT (swap)"
		}
		// 3. /org/ → /gov/ (both are ORGANIZATION type)
		if srcFrag.Type == hmodels.FragmentTypeOrganization && tgtFrag.Type == hmodels.FragmentTypeOrganization &&
			strings.HasPrefix(row.SourcePath, "/org/") && strings.HasPrefix(row.TargetPath, "/gov/") {
			needSwap = true
			fixReason = "ADMINISTERS /org/ -> /gov/ swapped to /gov/ -> /org/ (gov administers)"
		}
	case hmodels.DependencyTypeControls:
		// 4. PRODUCT → ORG/FACILITY (product cannot control; gov/org controls product)
		if srcFrag.Type == hmodels.FragmentTypeProduct && tgtValidControls {
			needSwap = true
			fixReason = "PRODUCT CONTROLS ORG -> ORG CONTROLS PRODUCT (swap)"
		}
	}

	if !needSwap {
		row.Action = "skipped"
		row.Reason = fmt.Sprintf("source=%s target=%s (no swap needed)", row.SourceType, row.TargetType)
		return row
	}

	// Swap source <-> target
	edge.SourceFragmentID = row.TargetFragmentID
	edge.TargetFragmentID = row.SourceFragmentID

	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	edge.Qualifiers["directionFixedAt"] = now.Format(time.RFC3339)
	edge.Qualifiers["directionFixedBy"] = "tool:horizon_fix_administers_facility_direction"
	edge.Qualifiers["directionFixedReason"] = fixReason

	row.Action = "swapped"
	row.Reason = fixReason

	if !*apply {
		return row
	}

	// Проверка: не создаём ли дубликат (уже есть ORG->FACILITY ADMINISTERS)?
	tenant := strings.TrimSpace(edge.TenantID)
	if tenant == "" {
		tenant = strings.TrimSpace(*tenantID)
	}
	existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenant, row.TargetFragmentID, row.SourceFragmentID, edge.Type)
	if err == nil && existing != nil && existing.ID != row.EdgeID {
		row.Action = "skipped"
		row.Reason = "edge already exists in correct direction (would create duplicate)"
		return row
	}

	if err := edgesRepo.Update(ctx, edge); err != nil {
		row.Action = "error"
		row.Reason = "update failed: " + err.Error()
		return row
	}
	row.Applied = true
	return row
}

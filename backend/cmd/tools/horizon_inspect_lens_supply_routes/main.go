package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type legRow struct {
	ArtifactID           string
	ArtifactDisplayName  string
	ArtifactCanonicalKey string
	LegIndex             int
	SupplierRef          string
	CustomerRef          string
	TransportViaRefs     []string
	FlowRefs             []string
	RouteGeometrySource  string
	RouteCoordinates     int
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	lensID := flag.String("lens-id", "", "lens id")
	lensName := flag.String("lens-name", "", "exact or partial lens name")
	nodeQuery := flag.String("node-query", "", "fragment prefix query; lists matching supply-chain artifacts and lens ids")
	limit := flag.Int("limit", 200, "max artifacts to inspect")
	apply := flag.Bool("apply", false, "rewrite route geometry for matching lens artifacts")
	onlyMissing := flag.Bool("only-missing", false, "when used with --apply, only rewrite legs that currently have no route coordinates")
	flag.Parse()

	if strings.TrimSpace(*lensID) == "" && strings.TrimSpace(*lensName) == "" && strings.TrimSpace(*nodeQuery) == "" {
		log.Fatal("one of --lens-id, --lens-name, or --node-query is required")
	}

	timeout := 2 * time.Minute
	if *apply {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer fs.Close()

	db := fs.Client()
	if query := strings.TrimSpace(*nodeQuery); query != "" {
		if err := inspectNodeQuery(ctx, db, query, *limit); err != nil {
			log.Fatalf("inspect node query failed: %v", err)
		}
		return
	}

	lens, err := resolveLens(ctx, db, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), strings.TrimSpace(*lensName))
	if err != nil {
		log.Fatalf("resolve lens failed: %v", err)
	}
	if lens == nil {
		log.Fatal("lens not found")
	}

	artifactsRepo := hrepo.NewMaterializedArtifactsRepository(db)
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	artifacts, err := artifactsRepo.ListByLens(ctx, lens.ID, *limit)
	if err != nil {
		log.Fatalf("list artifacts failed: %v", err)
	}

	rows := make([]legRow, 0, 256)
	sourceCounts := map[string]int{}
	changedArtifacts := 0
	appliedArtifacts := 0
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != models.MaterializedKindSupplyChainPath {
			continue
		}
		parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		artifactChanged := false
		for idx := range parsed.Legs {
			leg := &parsed.Legs[idx]
			if *apply && *onlyMissing && len(leg.RouteCoordinates) > 0 {
				continue
			}
			if !*apply {
				continue
			}
			orderedRefs := append(append([]string{strings.TrimSpace(leg.SupplierRef)}, leg.TransportViaRefs...), strings.TrimSpace(leg.CustomerRef))
			resolved := pathbuilder.ResolveSupplyChainRouteGeometryDetailed(ctx, fragmentsRepo, orderedRefs)
			nextSource := strings.TrimSpace(resolved.Source)
			nextFlowRefs := appendUniqueStrings(leg.FlowRefs, resolved.FlowRefs...)
			if len(resolved.Coordinates) >= 2 {
				if !routeCoordinatesEqual(leg.RouteCoordinates, resolved.Coordinates) || strings.TrimSpace(leg.RouteGeometrySource) != nextSource {
					leg.RouteCoordinates = append([]models.Coordinates(nil), resolved.Coordinates...)
					leg.RouteGeometrySource = nextSource
					artifactChanged = true
				}
			}
			if !stringSlicesEqual(leg.FlowRefs, nextFlowRefs) {
				leg.FlowRefs = nextFlowRefs
				artifactChanged = true
			}
		}
		if artifactChanged {
			changedArtifacts++
			artifact.Payload = models.BuildSupplyChainPathPayloadDetailed(
				parsed.Nodes,
				parsed.Edges,
				parsed.Legs,
				parsed.Stress,
				parsed.Sufficiency,
				parsed.Family,
				parsed.Notes,
			)
			if err := artifactsRepo.Update(ctx, artifact); err != nil {
				log.Printf("artifact update failed id=%s key=%s err=%v", artifact.ID, artifact.CanonicalKey, err)
			} else {
				appliedArtifacts++
			}
		}
		for idx := range parsed.Legs {
			leg := parsed.Legs[idx]
			source := strings.TrimSpace(leg.RouteGeometrySource)
			if source == "" {
				source = "unknown"
			}
			sourceCounts[source]++
			rows = append(rows, legRow{
				ArtifactID:           strings.TrimSpace(artifact.ID),
				ArtifactDisplayName:  strings.TrimSpace(artifact.DisplayName),
				ArtifactCanonicalKey: strings.TrimSpace(artifact.CanonicalKey),
				LegIndex:             idx,
				SupplierRef:          strings.TrimSpace(leg.SupplierRef),
				CustomerRef:          strings.TrimSpace(leg.CustomerRef),
				TransportViaRefs:     append([]string(nil), leg.TransportViaRefs...),
				FlowRefs:             append([]string(nil), leg.FlowRefs...),
				RouteGeometrySource:  source,
				RouteCoordinates:     len(leg.RouteCoordinates),
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RouteGeometrySource == rows[j].RouteGeometrySource {
			if rows[i].ArtifactDisplayName == rows[j].ArtifactDisplayName {
				return rows[i].LegIndex < rows[j].LegIndex
			}
			return rows[i].ArtifactDisplayName < rows[j].ArtifactDisplayName
		}
		return rows[i].RouteGeometrySource < rows[j].RouteGeometrySource
	})

	fmt.Printf("lens.id=%s\n", lens.ID)
	fmt.Printf("lens.name=%s\n", lens.Name)
	fmt.Printf("lens.projectId=%s\n", lens.ProjectID)
	fmt.Printf("artifacts.scanned=%d\n", len(artifacts))
	if *apply {
		fmt.Printf("artifacts.changed=%d\n", changedArtifacts)
		fmt.Printf("artifacts.applied=%d\n", appliedArtifacts)
	}
	fmt.Printf("legs.scanned=%d\n", len(rows))
	fmt.Printf("geometry.sources=")
	first := true
	for _, key := range sortedKeys(sourceCounts) {
		if !first {
			fmt.Printf(",")
		}
		first = false
		fmt.Printf("%s=%d", key, sourceCounts[key])
	}
	fmt.Printf("\n")
	fmt.Printf("\n")

	for _, row := range rows {
		fmt.Printf("[%s] %s leg=%d coords=%d\n", row.RouteGeometrySource, firstNonEmpty(row.ArtifactDisplayName, row.ArtifactCanonicalKey), row.LegIndex, row.RouteCoordinates)
		fmt.Printf("  %s -> %s\n", row.SupplierRef, row.CustomerRef)
		if len(row.TransportViaRefs) > 0 {
			fmt.Printf("  via: %s\n", strings.Join(row.TransportViaRefs, " -> "))
		}
		if len(row.FlowRefs) > 0 {
			fmt.Printf("  flows: %s\n", strings.Join(row.FlowRefs, ", "))
		}
		fmt.Printf("  artifact: %s\n", row.ArtifactCanonicalKey)
	}
}

func inspectNodeQuery(ctx context.Context, db *firestore.Client, query string, limit int) error {
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	artifactsRepo := hrepo.NewMaterializedArtifactsRepository(db)
	lensesRepo := hrepo.NewLensesRepository(db)

	fragments, err := fragmentsRepo.SearchPrefix(ctx, query, 20)
	if err != nil {
		return err
	}
	if len(fragments) == 0 {
		return fmt.Errorf("no fragments found for query %q", query)
	}
	for _, fragment := range fragments {
		if fragment == nil {
			continue
		}
		fmt.Printf("fragment: %s  %s\n", strings.TrimSpace(fragment.CanonicalPath), firstNonEmpty(fragment.Labels["en"], fragment.CanonicalPath))
		artifacts, err := artifactsRepo.FindByNodeRefAnyTenant(ctx, strings.TrimSpace(fragment.CanonicalPath), models.MaterializedKindSupplyChainPath, limit)
		if err != nil {
			return err
		}
		if len(artifacts) == 0 {
			fmt.Printf("  artifacts: none\n\n")
			continue
		}
		sort.Slice(artifacts, func(i, j int) bool {
			return strings.TrimSpace(artifacts[i].LensID) < strings.TrimSpace(artifacts[j].LensID)
		})
		for _, artifact := range artifacts {
			if artifact == nil {
				continue
			}
			lensName := ""
			if strings.TrimSpace(artifact.LensID) != "" {
				if lens, err := lensesRepo.Get(ctx, strings.TrimSpace(artifact.LensID)); err == nil && lens != nil {
					lensName = strings.TrimSpace(lens.Name)
				}
			}
			fmt.Printf("  artifact: lensId=%s lensName=%s canonicalKey=%s display=%s\n",
				strings.TrimSpace(artifact.LensID),
				lensName,
				strings.TrimSpace(artifact.CanonicalKey),
				strings.TrimSpace(artifact.DisplayName),
			)
		}
		fmt.Printf("\n")
	}
	return nil
}

func resolveLens(ctx context.Context, db *firestore.Client, tenantID, lensID, lensName string) (*models.Lens, error) {
	lensesRepo := hrepo.NewLensesRepository(db)
	if lensID != "" {
		return lensesRepo.Get(ctx, lensID)
	}

	docs, err := db.Collection("horizon_lenses").
		Where("tenantId", "==", tenantID).
		Limit(1000).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}

	needle := strings.ToLower(strings.TrimSpace(lensName))
	var exact *models.Lens
	var partial *models.Lens
	bestScore := 0
	tokens := make([]string, 0, 8)
	for _, token := range strings.FieldsFunc(needle, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == ':' || r == '/' || r == ','
	}) {
		token = strings.TrimSpace(token)
		if len(token) >= 3 {
			tokens = append(tokens, token)
		}
	}
	for _, doc := range docs {
		var lens models.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		name := strings.TrimSpace(lens.Name)
		lower := strings.ToLower(name)
		if lower == needle {
			exact = &lens
			break
		}
		if partial == nil && strings.Contains(lower, needle) {
			cp := lens
			partial = &cp
		}
		score := 0
		for _, token := range tokens {
			if strings.Contains(lower, token) {
				score++
			}
		}
		if score > bestScore {
			cp := lens
			partial = &cp
			bestScore = score
		}
	}
	if exact != nil {
		return exact, nil
	}
	return partial, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func appendUniqueStrings(base []string, extra ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, value := range base {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range extra {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if strings.TrimSpace(a[idx]) != strings.TrimSpace(b[idx]) {
			return false
		}
	}
	return true
}

func routeCoordinatesEqual(a, b []models.Coordinates) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return false
		}
	}
	return true
}

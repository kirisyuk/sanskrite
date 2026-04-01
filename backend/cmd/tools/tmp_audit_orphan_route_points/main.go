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

type finding struct {
	LensID         string
	ArtifactID     string
	CanonicalKey   string
	DisplayName    string
	LegIndex       int
	StoredSource   string
	ExpectedSource string
	StoredCoords   int
	ExpectedCoords int
	Reason         string
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID := flag.String("tenant", "", "optional tenant filter")
	lensID := flag.String("lens-id", "", "optional lens filter")
	limit := flag.Int("limit", 2000, "max supply-chain artifacts to scan")
	apply := flag.Bool("apply", false, "rewrite stale/orphan legs in place")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()

	db := fs.Client()
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	artifactsRepo := hrepo.NewMaterializedArtifactsRepository(db)

	artifacts, err := loadArtifacts(ctx, db, artifactsRepo, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), *limit)
	if err != nil {
		log.Fatalf("load artifacts: %v", err)
	}

	findings := make([]finding, 0, 256)
	artifactsScanned := 0
	legsScanned := 0
	artifactsChanged := 0
	artifactsApplied := 0

	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		artifactsScanned++
		artifactChanged := false
		for idx := range parsed.Legs {
			leg := &parsed.Legs[idx]
			legsScanned++
			orderedRefs := append(append([]string{strings.TrimSpace(leg.SupplierRef)}, leg.TransportViaRefs...), strings.TrimSpace(leg.CustomerRef))
			resolved := pathbuilder.ResolveSupplyChainRouteGeometryDetailed(ctx, fragmentsRepo, orderedRefs)
			expectedSource := strings.TrimSpace(resolved.Source)
			reason := staleReason(leg, resolved)
			if reason == "" {
				continue
			}
			findings = append(findings, finding{
				LensID:         strings.TrimSpace(artifact.LensID),
				ArtifactID:     strings.TrimSpace(artifact.ID),
				CanonicalKey:   strings.TrimSpace(artifact.CanonicalKey),
				DisplayName:    strings.TrimSpace(artifact.DisplayName),
				LegIndex:       idx,
				StoredSource:   strings.TrimSpace(leg.RouteGeometrySource),
				ExpectedSource: expectedSource,
				StoredCoords:   len(leg.RouteCoordinates),
				ExpectedCoords: len(resolved.Coordinates),
				Reason:         reason,
			})

			if !*apply {
				continue
			}
			nextFlowRefs, nextAlternateFlowRefs := rewriteLegFlowRefs(leg.FlowRefs, leg.AlternateFlowRefs, resolved.FlowRefs)
			if len(resolved.Coordinates) >= 2 {
				leg.RouteCoordinates = append([]models.Coordinates(nil), resolved.Coordinates...)
				leg.RouteGeometrySource = expectedSource
			} else {
				leg.RouteCoordinates = nil
				leg.RouteGeometrySource = ""
			}
			leg.FlowRefs = nextFlowRefs
			leg.AlternateFlowRefs = nextAlternateFlowRefs
			rewritePrimaryOverlays(leg, resolved, nextFlowRefs, nextAlternateFlowRefs, expectedSource)
			artifactChanged = true
		}
		if !artifactChanged {
			continue
		}
		artifactsChanged++
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
			continue
		}
		artifactsApplied++
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].LensID == findings[j].LensID {
			if findings[i].CanonicalKey == findings[j].CanonicalKey {
				return findings[i].LegIndex < findings[j].LegIndex
			}
			return findings[i].CanonicalKey < findings[j].CanonicalKey
		}
		return findings[i].LensID < findings[j].LensID
	})

	fmt.Printf("project=%s tenant=%s artifacts_scanned=%d legs_scanned=%d findings=%d artifacts_changed=%d artifacts_applied=%d\n",
		strings.TrimSpace(*projectID),
		strings.TrimSpace(*tenantID),
		artifactsScanned,
		legsScanned,
		len(findings),
		artifactsChanged,
		artifactsApplied,
	)
	for _, item := range findings {
		fmt.Printf("- lens=%s leg=%d stored=%s/%d expected=%s/%d reason=%s\n",
			item.LensID,
			item.LegIndex,
			firstNonEmpty(item.StoredSource, "unknown"),
			item.StoredCoords,
			firstNonEmpty(item.ExpectedSource, "none"),
			item.ExpectedCoords,
			item.Reason,
		)
		fmt.Printf("  artifact=%s\n", firstNonEmpty(item.CanonicalKey, item.ArtifactID))
		if item.DisplayName != "" {
			fmt.Printf("  display=%s\n", item.DisplayName)
		}
	}
}

func loadArtifacts(
	ctx context.Context,
	db *firestore.Client,
	artifactsRepo *hrepo.MaterializedArtifactsRepository,
	tenantID string,
	lensID string,
	limit int,
) ([]*models.MaterializedArtifact, error) {
	if strings.TrimSpace(lensID) != "" {
		return artifactsRepo.ListByLens(ctx, strings.TrimSpace(lensID), limit)
	}
	if strings.TrimSpace(tenantID) != "" {
		return artifactsRepo.ListByTenantAndKind(ctx, strings.TrimSpace(tenantID), models.MaterializedKindSupplyChainPath, limit)
	}
	docs, err := db.Collection("horizon_materialized_artifacts").
		Where("kind", "==", string(models.MaterializedKindSupplyChainPath)).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.MaterializedArtifact, 0, len(docs))
	for _, doc := range docs {
		var artifact models.MaterializedArtifact
		if err := doc.DataTo(&artifact); err != nil {
			continue
		}
		out = append(out, &artifact)
	}
	return out, nil
}

func staleReason(leg *models.SupplyChainPathLeg, resolved pathbuilder.ResolvedRouteGeometry) string {
	if leg == nil {
		return ""
	}
	storedCoords := len(leg.RouteCoordinates)
	expectedCoords := len(resolved.Coordinates)
	storedSource := strings.TrimSpace(leg.RouteGeometrySource)
	expectedSource := strings.TrimSpace(resolved.Source)
	switch {
	case expectedCoords < 2 && storedCoords > 0:
		return "stored_coords_without_current_route"
	case expectedCoords >= 2 && storedCoords == 0:
		return "missing_route_coordinates"
	case expectedCoords >= 2 && storedCoords > 0 && !routeCoordinatesEqual(leg.RouteCoordinates, resolved.Coordinates):
		return "route_coordinates_mismatch"
	case expectedCoords >= 2 && storedSource != expectedSource:
		return "route_source_mismatch"
	case expectedCoords < 2 && storedSource != "":
		return "stale_route_source_without_coords"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func rewriteLegFlowRefs(current []string, currentAlternate []string, resolved []string) ([]string, []string) {
	primary := appendUniqueStrings(nil, resolved...)
	if len(primary) == 0 {
		primary = appendUniqueStrings(nil, current...)
	}
	alternate := appendUniqueStrings(nil, currentAlternate...)
	for _, value := range current {
		value = strings.TrimSpace(value)
		if value == "" || stringSliceContains(primary, value) {
			continue
		}
		alternate = appendUniqueStrings(alternate, value)
	}
	for _, value := range resolved {
		value = strings.TrimSpace(value)
		if value == "" || stringSliceContains(primary, value) {
			continue
		}
		alternate = appendUniqueStrings(alternate, value)
	}
	return primary, removeStringSliceValues(alternate, primary...)
}

func rewritePrimaryOverlays(
	leg *models.SupplyChainPathLeg,
	resolved pathbuilder.ResolvedRouteGeometry,
	flowRefs []string,
	alternateFlowRefs []string,
	source string,
) bool {
	if leg == nil || len(leg.RouteOverlays) == 0 {
		return false
	}
	changed := false
	for idx := range leg.RouteOverlays {
		overlay := &leg.RouteOverlays[idx]
		if len(leg.RouteOverlays) > 1 && strings.TrimSpace(overlay.Status) != "primary" {
			continue
		}
		if len(resolved.Coordinates) >= 2 {
			if !routeCoordinatesEqual(overlay.RouteCoordinates, resolved.Coordinates) || strings.TrimSpace(overlay.RouteGeometrySource) != source {
				overlay.RouteCoordinates = append([]models.Coordinates(nil), resolved.Coordinates...)
				overlay.RouteGeometrySource = source
				changed = true
			}
		} else if len(overlay.RouteCoordinates) > 0 || strings.TrimSpace(overlay.RouteGeometrySource) != "" {
			overlay.RouteCoordinates = nil
			overlay.RouteGeometrySource = ""
			changed = true
		}
		if !stringSlicesEqual(overlay.FlowRefs, flowRefs) {
			overlay.FlowRefs = append([]string(nil), flowRefs...)
			changed = true
		}
		if !stringSlicesEqual(overlay.AlternateFlowRefs, alternateFlowRefs) {
			overlay.AlternateFlowRefs = append([]string(nil), alternateFlowRefs...)
			changed = true
		}
	}
	return changed
}

func stringSliceContains(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

func removeStringSliceValues(values []string, remove ...string) []string {
	if len(values) == 0 {
		return nil
	}
	blocked := make(map[string]struct{}, len(remove))
	for _, value := range remove {
		value = strings.TrimSpace(value)
		if value != "" {
			blocked[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := blocked[value]; ok {
			continue
		}
		if stringSliceContains(out, value) {
			continue
		}
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
		if a[idx].Lat != b[idx].Lat || a[idx].Lon != b[idx].Lon {
			return false
		}
	}
	return true
}

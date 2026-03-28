package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type supplyLinkSnapshot struct {
	transportViaRefs []string
	routeVariants    []models.SupplyLinkRouteVariant
}

type routeCandidateAggregate struct {
	ProfileKey              string   `json:"profileKey"`
	OrderedRefs             []string `json:"orderedRefs"`
	RouteVariantKey         string   `json:"routeVariantKey,omitempty"`
	SupplyLinkCanonicalPath string   `json:"supplyLinkCanonicalPath,omitempty"`
	PathArtifactKey         string   `json:"pathArtifactKey,omitempty"`
	Count                   int      `json:"count"`
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	pageSize := flag.Int("page-size", 100, "page size")
	limit := flag.Int("limit", 0, "max docs to process (0 = all)")
	startAfter := flag.String("start-after", "", "resume after document id")
	apply := flag.Bool("apply", false, "apply writes (default dry-run)")
	sleepPerPageMs := flag.Int("sleep-per-page-ms", 100, "sleep between pages")
	candidateReport := flag.String("candidate-report", "", "optional JSON report path for unresolved route aggregates")
	candidateReportLimit := flag.Int("candidate-report-limit", 1000, "max unresolved route aggregates to write into report")
	candidateReportEveryPages := flag.Int("candidate-report-every-pages", 0, "write candidate report checkpoint every N artifact pages (0 = final only)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer fs.Close()

	db := fs.Client()
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	routeObsRepo := hrepo.NewRouteObservationsRepository(db)
	routeCandidateRepo := hrepo.NewRouteGeometryCandidatesRepository(db)
	artifactsRepo := hrepo.NewMaterializedArtifactsRepository(db)

	linkScanned, linkChanged, linkApplied := 0, 0, 0
	artifactScanned, artifactChanged, artifactApplied := 0, 0, 0
	candidateDetected, candidateApplied := 0, 0
	errorsCount := 0
	lastDocID := strings.TrimSpace(*startAfter)
	sleepPerPage := time.Duration(*sleepPerPageMs) * time.Millisecond
	variantsBySupplyLink := make(map[string][]models.SupplyLinkRouteVariant)
	geometrySourceCounts := make(map[string]int)
	candidateSamples := make([]string, 0, 10)
	candidateAggregates := make(map[string]*routeCandidateAggregate)

	for {
		q := db.Collection("horizon_fragments").
			Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Where("type", "==", string(models.FragmentTypeSupplyLink)).
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if lastDocID != "" {
			q = q.StartAfter(lastDocID)
		}
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("supply-link page fetch failed: %v", err)
		}
		if len(docs) == 0 {
			break
		}
		lastDocID = docs[len(docs)-1].Ref.ID
		for _, doc := range docs {
			if *limit > 0 && linkScanned >= *limit {
				break
			}
			frag, err := fragmentsRepo.Get(ctx, doc.Ref.ID)
			if err != nil || frag == nil {
				errorsCount++
				continue
			}
			linkScanned++
			before := captureSupplyLinkSnapshot(frag)
			confidence := 0.0
			if frag.State != nil {
				if metric, ok := frag.State["confidence"]; ok {
					confidence = metric.Value
				}
			}
			variants := models.ExtractSupplyLinkRouteVariants(frag.Qualifiers, frag.TransportViaRefs, extractVesselType(frag.Qualifiers), confidence)
			observations, err := routeObsRepo.ListByTenantAndSupplyLinkCanonicalPath(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(frag.CanonicalPath), 200)
			if err != nil {
				errorsCount++
				log.Printf("route observation lookup failed path=%s err=%v", frag.CanonicalPath, err)
				continue
			}
			for _, obs := range observations {
				if obs == nil {
					continue
				}
				variants = models.MergeSupplyLinkRouteVariants(variants, []models.SupplyLinkRouteVariant{
					models.NewSupplyLinkRouteVariant(obs.TransportViaRefs, extractVesselType(obs.Qualifiers), routeObservationConfidence(obs)),
				})
			}
			if len(variants) > 0 {
				if frag.Qualifiers == nil {
					frag.Qualifiers = map[string]interface{}{}
				}
				frag.Qualifiers["routeVariants"] = variants
				if primary := models.PrimarySupplyLinkRouteVariant(variants); primary != nil {
					frag.TransportViaRefs = append([]string(nil), primary.TransportViaRefs...)
				}
			}
			after := captureSupplyLinkSnapshot(frag)
			variantsBySupplyLink[strings.TrimSpace(frag.CanonicalPath)] = after.routeVariants
			if reflect.DeepEqual(before, after) {
				continue
			}
			linkChanged++
			if *apply {
				frag.UpdatedAt = time.Now().UTC()
				frag.Metadata.LastUpdatedAt = time.Now().UTC()
				if err := fragmentsRepo.UpdateFast(ctx, frag); err != nil {
					errorsCount++
					log.Printf("supply-link update failed id=%s path=%s err=%v", frag.ID, frag.CanonicalPath, err)
					continue
				}
				linkApplied++
			}
		}
		log.Printf("supply-link page complete: scanned=%d changed=%d applied=%d lastDoc=%s", linkScanned, linkChanged, linkApplied, lastDocID)
		if *limit > 0 && linkScanned >= *limit {
			break
		}
		if sleepPerPage > 0 {
			time.Sleep(sleepPerPage)
		}
	}

	lastArtifactDocID := ""
	artifactPages := 0
	for {
		q := db.Collection("horizon_materialized_artifacts").
			Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Where("kind", "==", string(models.MaterializedKindSupplyChainPath)).
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if lastArtifactDocID != "" {
			q = q.StartAfter(lastArtifactDocID)
		}
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("artifact page fetch failed: %v", err)
		}
		if len(docs) == 0 {
			break
		}
		lastArtifactDocID = docs[len(docs)-1].Ref.ID
		artifactPages++
		for _, doc := range docs {
			if *limit > 0 && artifactScanned >= *limit {
				break
			}
			artifact, err := artifactsRepo.Get(ctx, doc.Ref.ID)
			if err != nil || artifact == nil {
				errorsCount++
				continue
			}
			artifactScanned++
			parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
			if err != nil || parsed == nil {
				continue
			}
			changed := false
			for idx := range parsed.Legs {
				leg := &parsed.Legs[idx]
				variantRefs := leg.TransportViaRefs
				if len(variantRefs) == 0 {
					if primary := models.PrimarySupplyLinkRouteVariant(variantsBySupplyLink[strings.TrimSpace(leg.SupplyLinkCanonicalPath)]); primary != nil {
						variantRefs = append([]string(nil), primary.TransportViaRefs...)
						leg.TransportViaRefs = append([]string(nil), primary.TransportViaRefs...)
						changed = true
					}
				}
				nextVariantKey := strings.TrimSpace(firstNonEmpty(leg.RouteVariantKey, models.BuildSupplyLinkRouteVariantKey(variantRefs)))
				if strings.TrimSpace(leg.RouteVariantKey) != nextVariantKey {
					leg.RouteVariantKey = nextVariantKey
					changed = true
				}
				orderedRefs := append(append([]string{strings.TrimSpace(leg.SupplierRef)}, variantRefs...), strings.TrimSpace(leg.CustomerRef))
				resolved := pathbuilder.ResolveSupplyChainRouteGeometryDetailed(
					ctx,
					fragmentsRepo,
					orderedRefs,
				)
				source := strings.TrimSpace(resolved.Source)
				if source != "" {
					geometrySourceCounts[source]++
				}
				if len(resolved.Coordinates) >= 2 && (!reflect.DeepEqual(leg.RouteCoordinates, resolved.Coordinates) || strings.TrimSpace(leg.RouteGeometrySource) != source) {
					leg.RouteCoordinates = append([]models.Coordinates(nil), resolved.Coordinates...)
					leg.RouteGeometrySource = source
					changed = true
				}
				nextFlowRefs := uniqueTrimmedStrings(append(append([]string(nil), leg.FlowRefs...), resolved.FlowRefs...))
				if !reflect.DeepEqual(leg.FlowRefs, nextFlowRefs) {
					leg.FlowRefs = nextFlowRefs
					changed = true
				}
				if source == "fragmentary_waypoint_sequence" {
					candidateDetected++
					profileKey := models.BuildRouteGeometryProfileKey(orderedRefs)
					if profileKey != "" {
						if agg := candidateAggregates[profileKey]; agg != nil {
							agg.Count++
						} else {
							candidateAggregates[profileKey] = &routeCandidateAggregate{
								ProfileKey:              profileKey,
								OrderedRefs:             models.NormalizeOrderedRouteRefs(orderedRefs),
								RouteVariantKey:         strings.TrimSpace(leg.RouteVariantKey),
								SupplyLinkCanonicalPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
								PathArtifactKey:         strings.TrimSpace(artifact.CanonicalKey),
								Count:                   1,
							}
						}
					}
					if len(candidateSamples) < 10 {
						candidateSamples = append(candidateSamples, strings.Join(models.NormalizeOrderedRouteRefs(orderedRefs), " -> "))
					}
					if *apply {
						candidate := &models.RouteGeometryCandidate{
							TenantID:                strings.TrimSpace(*tenantID),
							ProfileKey:              profileKey,
							RouteVariantKey:         strings.TrimSpace(leg.RouteVariantKey),
							OrderedRefs:             orderedRefs,
							SupplyLinkCanonicalPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
							PathArtifactKey:         strings.TrimSpace(artifact.CanonicalKey),
							Reason:                  "fragmentary waypoint-only route; requires curated real-world geometry",
						}
						if err := routeCandidateRepo.Upsert(ctx, candidate); err != nil {
							errorsCount++
							log.Printf("route candidate upsert failed profileKey=%s artifact=%s err=%v", candidate.ProfileKey, artifact.CanonicalKey, err)
						} else {
							candidateApplied++
						}
					}
				}
			}
			if !changed {
				continue
			}
			artifactChanged++
			artifact.Payload = models.BuildSupplyChainPathPayloadDetailed(
				parsed.Nodes,
				parsed.Edges,
				parsed.Legs,
				parsed.Stress,
				parsed.Sufficiency,
				parsed.Family,
				parsed.Notes,
			)
			if *apply {
				if err := artifactsRepo.Update(ctx, artifact); err != nil {
					errorsCount++
					log.Printf("artifact update failed id=%s key=%s err=%v", artifact.ID, artifact.CanonicalKey, err)
					continue
				}
				artifactApplied++
			}
		}
		log.Printf("artifact page complete: scanned=%d changed=%d applied=%d candidates=%d/%d lastDoc=%s", artifactScanned, artifactChanged, artifactApplied, candidateApplied, candidateDetected, lastArtifactDocID)
		if reportPath := strings.TrimSpace(*candidateReport); reportPath != "" && *candidateReportEveryPages > 0 && artifactPages%*candidateReportEveryPages == 0 {
			if err := writeCandidateReport(reportPath, candidateAggregates, *candidateReportLimit); err != nil {
				log.Printf("supply_link_route_backfill candidate_report_checkpoint_failed path=%s page=%d err=%v", reportPath, artifactPages, err)
			} else {
				log.Printf("supply_link_route_backfill candidate_report_checkpoint_written path=%s page=%d entries=%d", reportPath, artifactPages, minInt(len(candidateAggregates), *candidateReportLimit))
			}
		}
		if *limit > 0 && artifactScanned >= *limit {
			break
		}
		if sleepPerPage > 0 {
			time.Sleep(sleepPerPage)
		}
	}

	sourceKeys := make([]string, 0, len(geometrySourceCounts))
	for source := range geometrySourceCounts {
		sourceKeys = append(sourceKeys, source)
	}
	sort.Strings(sourceKeys)
	sourcePairs := make([]string, 0, len(sourceKeys))
	for _, source := range sourceKeys {
		sourcePairs = append(sourcePairs, source+"="+itoa(geometrySourceCounts[source]))
	}

	log.Printf(
		"supply_link_route_backfill summary: project=%s tenant=%s supply_links_scanned=%d supply_links_changed=%d supply_links_applied=%d artifacts_scanned=%d artifacts_changed=%d artifacts_applied=%d route_candidates_detected=%d route_candidates_applied=%d geometry_sources=%s errors=%d lastDoc=%s apply=%v",
		strings.TrimSpace(*projectID),
		strings.TrimSpace(*tenantID),
		linkScanned,
		linkChanged,
		linkApplied,
		artifactScanned,
		artifactChanged,
		artifactApplied,
		candidateDetected,
		candidateApplied,
		strings.Join(sourcePairs, ","),
		errorsCount,
		lastDocID,
		*apply,
	)
	if len(candidateSamples) > 0 {
		log.Printf("supply_link_route_backfill candidate_samples: %s", strings.Join(candidateSamples, " || "))
	}
	if reportPath := strings.TrimSpace(*candidateReport); reportPath != "" {
		if err := writeCandidateReport(reportPath, candidateAggregates, *candidateReportLimit); err != nil {
			log.Printf("supply_link_route_backfill candidate_report_write_failed path=%s err=%v", reportPath, err)
		} else {
			log.Printf("supply_link_route_backfill candidate_report_written path=%s entries=%d", reportPath, minInt(len(candidateAggregates), *candidateReportLimit))
		}
	}
}

func captureSupplyLinkSnapshot(frag *models.HorizonFragment) supplyLinkSnapshot {
	if frag == nil {
		return supplyLinkSnapshot{}
	}
	confidence := 0.0
	if frag.State != nil {
		if metric, ok := frag.State["confidence"]; ok {
			confidence = metric.Value
		}
	}
	return supplyLinkSnapshot{
		transportViaRefs: append([]string(nil), frag.TransportViaRefs...),
		routeVariants: models.ExtractSupplyLinkRouteVariants(
			frag.Qualifiers,
			frag.TransportViaRefs,
			extractVesselType(frag.Qualifiers),
			confidence,
		),
	}
}

func extractVesselType(qualifiers map[string]interface{}) string {
	if qualifiers == nil {
		return ""
	}
	for _, key := range []string{"vesselType", "vessel_type"} {
		if raw, ok := qualifiers[key].(string); ok {
			if value := strings.TrimSpace(raw); value != "" {
				return value
			}
		}
	}
	return ""
}

func routeObservationConfidence(obs *models.RouteObservation) float64 {
	if obs == nil {
		return 0
	}
	if obs.Metrics != nil {
		if metric, ok := obs.Metrics["confidence"]; ok {
			return metric.Value
		}
	}
	return 0.5
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueTrimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
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
	sort.Strings(out)
	return out
}

func itoa(v int) string {
	return strconv.FormatInt(int64(v), 10)
}

func writeCandidateReport(path string, aggregates map[string]*routeCandidateAggregate, limit int) error {
	if len(aggregates) == 0 {
		return os.WriteFile(path, []byte("[]\n"), 0o644)
	}
	items := make([]routeCandidateAggregate, 0, len(aggregates))
	for _, agg := range aggregates {
		if agg == nil {
			continue
		}
		items = append(items, *agg)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return strings.Join(items[i].OrderedRefs, "->") < strings.Join(items[j].OrderedRefs, "->")
		}
		return items[i].Count > items[j].Count
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o644)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

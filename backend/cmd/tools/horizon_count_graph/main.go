// horizon_count_graph counts edges and unique nodes (fragments) for a tenant,
// optionally filtered by creation time window. With -coords, reports how many
// fragments lack geo coordinates.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id for edge filter")
	fromStr := flag.String("from", "", "filter edges created at or after (RFC3339, e.g. 2026-03-05T11:25:00Z)")
	toStr := flag.String("to", "", "filter edges created at or before (RFC3339)")
	limit := flag.Int("limit", 10000, "max edges to scan")
	checkCoords := flag.Bool("coords", false, "fetch fragments and report how many lack geo coordinates")
	flag.Parse()

	if *projectID == "" {
		fmt.Fprintln(os.Stderr, "--project required or set GCP_PROJECT_ID")
		os.Exit(2)
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()

	col := fsClient.Client().Collection("horizon_dependency_edges")
	tenant := strings.TrimSpace(*tenantID)

	var q firestore.Query
	if *fromStr != "" || *toStr != "" {
		q = col.Where("tenantId", "==", tenant)
		if *fromStr != "" {
			from, err := time.Parse(time.RFC3339, *fromStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid -from: %v\n", err)
				os.Exit(2)
			}
			q = q.Where("metadata.createdAt", ">=", from)
		}
		if *toStr != "" {
			to, err := time.Parse(time.RFC3339, *toStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid -to: %v\n", err)
				os.Exit(2)
			}
			q = q.Where("metadata.createdAt", "<=", to)
		}
		q = q.OrderBy("metadata.createdAt", firestore.Asc).Limit(*limit)
	} else {
		q = col.Where("tenantId", "==", tenant).OrderBy(firestore.DocumentID, firestore.Asc).Limit(*limit)
	}

	iter := q.Documents(ctx)
	edgeCount := 0
	nodes := make(map[string]struct{})

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "iterate: %v\n", err)
			os.Exit(1)
		}
		var edge models.DependencyEdge
		if err := doc.DataTo(&edge); err != nil {
			continue
		}
		edgeCount++
		if edge.SourceFragmentID != "" {
			nodes[edge.SourceFragmentID] = struct{}{}
		}
		if edge.TargetFragmentID != "" {
			nodes[edge.TargetFragmentID] = struct{}{}
		}
	}

	fmt.Printf("edges: %d\n", edgeCount)
	fmt.Printf("unique_nodes: %d\n", len(nodes))

	if *checkCoords && len(nodes) > 0 {
		fragIDs := make([]string, 0, len(nodes))
		for id := range nodes {
			fragIDs = append(fragIDs, id)
		}
		frRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
		frags, err := frRepo.GetMany(ctx, fragIDs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fetch fragments: %v\n", err)
			os.Exit(1)
		}
		withCoords := 0
		withoutCoords := 0
		for _, f := range frags {
			if f == nil {
				withoutCoords++
				continue
			}
			if hasValidCoordinates(f) {
				withCoords++
			} else {
				withoutCoords++
			}
		}
		notFound := len(nodes) - len(frags)
		withoutCoords += notFound
		fmt.Printf("with_coordinates: %d\n", withCoords)
		fmt.Printf("without_coordinates: %d\n", withoutCoords)
	}
}

func hasValidCoordinates(f *models.HorizonFragment) bool {
	if f == nil {
		return false
	}
	if f.Coordinates != nil && (f.Coordinates.Lat != 0 || f.Coordinates.Lon != 0) {
		return true
	}
	if f.HomeLocation != nil && f.HomeLocation.ISO6709 != nil && strings.TrimSpace(*f.HomeLocation.ISO6709) != "" {
		return true
	}
	if f.HorizonMonitor != nil && f.HorizonMonitor.Coordinates != nil && (f.HorizonMonitor.Coordinates.Lat != 0 || f.HorizonMonitor.Coordinates.Lon != 0) {
		return true
	}
	if f.WorldMonitorLegacy != nil && f.WorldMonitorLegacy.Coordinates != nil && (f.WorldMonitorLegacy.Coordinates.Lat != 0 || f.WorldMonitorLegacy.Coordinates.Lon != 0) {
		return true
	}
	if f.Track != nil && (f.Track.Lat != 0 || f.Track.Lon != 0) {
		return true
	}
	return false
}

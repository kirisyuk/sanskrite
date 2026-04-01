package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

type fragmentRow struct {
	ID        string
	UpdatedAt time.Time
	CreatedAt time.Time
	Path      string
	Type      string
	Status    string
	Label     string
	Country   string
	Lat       float64
	Lon       float64
	HasCoords bool
}

type jobRow struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
	Status    string
	Title     string
	LensID    string
	Target    string
}

func main() {
	projectID := flag.String("project", "nv-horizon-test", "GCP project ID")
	tenantID := flag.String("tenant", "global", "Tenant ID")
	fragmentLimit := flag.Int("fragment-limit", 150, "Recent fragment count")
	jobLimit := flag.Int("job-limit", 80, "Recent scout job count")
	path := flag.String("path", "", "Show one fragment by canonical path")
	flag.Parse()

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	if strings.TrimSpace(*path) != "" {
		if err := showFragment(ctx, db, strings.TrimSpace(*tenantID), strings.TrimSpace(*path)); err != nil {
			panic(err)
		}
		return
	}

	fragments, err := recentFragments(ctx, db, strings.TrimSpace(*tenantID), *fragmentLimit)
	if err != nil {
		panic(err)
	}
	jobs, err := recentScoutJobs(ctx, db, strings.TrimSpace(*tenantID), *jobLimit)
	if err != nil {
		panic(err)
	}

	fmt.Printf("recent_fragments=%d recent_scout_jobs=%d project=%s tenant=%s\n", len(fragments), len(jobs), *projectID, *tenantID)
	fmt.Println("FRAGMENTS")
	for _, row := range fragments {
		fmt.Printf("%s | %s | %s | coords=%t %.4f,%.4f | cc=%s | %s | %s\n",
			row.UpdatedAt.UTC().Format(time.RFC3339),
			row.Type,
			row.Status,
			row.HasCoords,
			row.Lat,
			row.Lon,
			row.Country,
			row.Path,
			row.Label,
		)
	}

	fmt.Println("JOBS")
	for _, row := range jobs {
		fmt.Printf("%s | %s | lens=%s | target=%s | %s\n",
			row.CreatedAt.UTC().Format(time.RFC3339),
			row.Status,
			row.LensID,
			row.Target,
			row.Title,
		)
	}
}

func showFragment(ctx context.Context, db *firestore.Client, tenantID string, path string) error {
	docs, err := db.Collection("horizon_fragments").
		Where("tenantId", "==", tenantID).
		Where("canonicalPath", "==", path).
		Limit(1).
		Documents(ctx).
		GetAll()
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("fragment not found: %s", path)
	}
	var frag hmodels.HorizonFragment
	if err := docs[0].DataTo(&frag); err != nil {
		return err
	}
	frag.ID = docs[0].Ref.ID
	data, err := json.MarshalIndent(frag, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func recentFragments(ctx context.Context, db *firestore.Client, tenantID string, limit int) ([]fragmentRow, error) {
	docs, err := db.Collection("horizon_fragments").
		Where("tenantId", "==", tenantID).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	rows := make([]fragmentRow, 0, len(docs))
	for _, doc := range docs {
		var frag hmodels.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			continue
		}
		row := fragmentRow{
			ID:        doc.Ref.ID,
			UpdatedAt: frag.UpdatedAt,
			CreatedAt: frag.CreatedAt,
			Path:      strings.TrimSpace(frag.CanonicalPath),
			Type:      string(frag.Type),
			Status:    firstNonEmpty(string(frag.Governance.Status), frag.Status),
			Label:     firstNonEmpty(frag.Labels["en"], frag.Labels["legalName"], frag.Labels["shortName"], frag.Description),
		}
		if frag.HomeLocation != nil {
			if frag.HomeLocation.ISO3166_1 != nil {
				row.Country = strings.TrimSpace(*frag.HomeLocation.ISO3166_1)
			}
		}
		if frag.Coordinates != nil {
			row.HasCoords = true
			row.Lat = frag.Coordinates.Lat
			row.Lon = frag.Coordinates.Lon
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func recentScoutJobs(ctx context.Context, db *firestore.Client, tenantID string, limit int) ([]jobRow, error) {
	docs, err := db.Collection("agent_jobs").
		OrderBy("createdAt", firestore.Desc).
		Limit(limit * 8).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	rows := make([]jobRow, 0, len(docs))
	for _, doc := range docs {
		var job coremodels.AgentJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		if strings.TrimSpace(job.TenantID) != tenantID || strings.TrimSpace(job.AgentID) != "scout" {
			continue
		}
		rows = append(rows, jobRow{
			ID:        doc.Ref.ID,
			CreatedAt: job.CreatedAt,
			UpdatedAt: job.UpdatedAt,
			Status:    string(job.Status),
			Title:     strings.TrimSpace(job.Title),
			LensID:    stringFromMap(job.State, "lensId"),
			Target:    stringFromMap(job.State, "targetCanonicalPath"),
		})
		if len(rows) >= limit {
			break
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	return rows, nil
}

func stringFromMap(state map[string]interface{}, key string) string {
	if state == nil {
		return ""
	}
	v, ok := state[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

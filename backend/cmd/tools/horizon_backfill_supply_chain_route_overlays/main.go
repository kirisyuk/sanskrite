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

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID         = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID          = flag.String("tenant", "global", "tenant id")
	lensID            = flag.String("lens-id", "", "optional lens id filter")
	artifactKeyPrefix = flag.String("artifact-key-prefix", "", "optional artifact canonical key prefix filter")
	limit             = flag.Int("limit", 500, "max artifacts to scan")
	apply             = flag.Bool("apply", false, "persist normalized payloads")
	reportJSON        = flag.String("report-json", "", "optional report output path")
	timeout           = flag.Duration("timeout", 2*time.Hour, "overall execution timeout")
)

type report struct {
	TenantID          string   `json:"tenantId"`
	LensID            string   `json:"lensId,omitempty"`
	Scanned           int      `json:"scanned"`
	Normalized        int      `json:"normalized"`
	AlreadyNormalized int      `json:"alreadyNormalized"`
	Errors            int      `json:"errors"`
	Samples           []string `json:"samples,omitempty"`
}

const persistChunkSize = 50

func main() {
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	artifactsRepo := repo.NewMaterializedArtifactsRepository(fsClient.Client())
	items, err := loadArtifacts(ctx, artifactsRepo, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), strings.TrimSpace(*artifactKeyPrefix), *limit)
	if err != nil {
		log.Fatalf("load artifacts: %v", err)
	}

	out := report{
		TenantID: strings.TrimSpace(*tenantID),
		LensID:   strings.TrimSpace(*lensID),
		Samples:  make([]string, 0, 20),
	}
	toPersist := make([]*models.MaterializedArtifact, 0, len(items))
	for _, artifact := range items {
		if artifact == nil || artifact.Kind != models.MaterializedKindSupplyChainPath {
			continue
		}
		out.Scanned++
		changed, err := models.NormalizeSupplyChainPathRouteOverlays(artifact)
		if err != nil {
			out.Errors++
			appendSample(&out, fmt.Sprintf("artifact=%s err=%v", strings.TrimSpace(artifact.CanonicalKey), err))
			continue
		}
		if !changed {
			out.AlreadyNormalized++
			continue
		}
		out.Normalized++
		appendSample(&out, fmt.Sprintf("artifact=%s overlays-normalized", strings.TrimSpace(artifact.CanonicalKey)))
		if *apply {
			toPersist = append(toPersist, artifact)
		}
	}
	if *apply && len(toPersist) > 0 {
		persistArtifacts(ctx, artifactsRepo, toPersist, &out)
	}

	printSummary(out)
	if strings.TrimSpace(*reportJSON) != "" {
		if err := writeReportJSON(strings.TrimSpace(*reportJSON), out); err != nil {
			log.Fatalf("write report: %v", err)
		}
	}
}

func loadArtifacts(
	ctx context.Context,
	artifactsRepo *repo.MaterializedArtifactsRepository,
	tenantID string,
	lensID string,
	artifactKeyPrefix string,
	limit int,
) ([]*models.MaterializedArtifact, error) {
	items, err := artifactsRepo.ListByTenantAndKind(ctx, tenantID, models.MaterializedKindSupplyChainPath, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*models.MaterializedArtifact, 0, len(items))
	for _, item := range items {
		if item == nil || item.Kind != models.MaterializedKindSupplyChainPath {
			continue
		}
		if lensID != "" && strings.TrimSpace(item.LensID) != lensID {
			continue
		}
		if artifactKeyPrefix != "" && !strings.HasPrefix(strings.TrimSpace(item.CanonicalKey), artifactKeyPrefix) {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func persistArtifacts(
	ctx context.Context,
	artifactsRepo *repo.MaterializedArtifactsRepository,
	items []*models.MaterializedArtifact,
	out *report,
) {
	if artifactsRepo == nil || len(items) == 0 || out == nil {
		return
	}
	for start := 0; start < len(items); start += persistChunkSize {
		end := start + persistChunkSize
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]
		if _, err := artifactsRepo.BatchUpsert(ctx, chunk); err == nil {
			continue
		} else {
			out.Errors++
			appendSample(out, fmt.Sprintf("batchUpsertErr start=%d end=%d err=%v", start, end, err))
		}
		for _, artifact := range chunk {
			if artifact == nil {
				continue
			}
			if err := artifactsRepo.Update(ctx, artifact); err != nil {
				out.Errors++
				appendSample(out, fmt.Sprintf("artifact=%s updateErr=%v", strings.TrimSpace(artifact.CanonicalKey), err))
			}
		}
	}
}

func appendSample(out *report, sample string) {
	if out == nil {
		return
	}
	sample = strings.TrimSpace(sample)
	if sample == "" || len(out.Samples) >= 20 {
		return
	}
	out.Samples = append(out.Samples, sample)
}

func printSummary(out report) {
	log.Printf(
		"route-overlay-backfill summary tenant=%s lens=%s scanned=%d normalized=%d alreadyNormalized=%d errors=%d",
		out.TenantID,
		out.LensID,
		out.Scanned,
		out.Normalized,
		out.AlreadyNormalized,
		out.Errors,
	)
	for _, sample := range out.Samples {
		log.Printf("sample: %s", sample)
	}
}

func writeReportJSON(path string, out report) error {
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

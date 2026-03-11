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

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID         = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID          = flag.String("tenant", "global", "tenant id")
	lensID            = flag.String("lens-id", "", "optional lens id filter")
	artifactKeyPrefix = flag.String("artifact-key-prefix", "", "optional artifact canonical key prefix filter")
	limit             = flag.Int("limit", 500, "max artifacts to scan")
	dryRun            = flag.Bool("dry-run", true, "report only, no writes")
	repairWrite       = flag.Bool("repair-write", false, "persist repaired artifacts")
	reportJSON        = flag.String("report-json", "", "optional report output path")
)

type report struct {
	TenantID                  string   `json:"tenantId"`
	LensID                    string   `json:"lensId,omitempty"`
	Scanned                   int      `json:"scanned"`
	AlreadyComplete           int      `json:"alreadyComplete"`
	RepairedArtifacts         int      `json:"repairedArtifacts"`
	RepairedWithCreatedLinks  int      `json:"repairedWithCreatedLinks"`
	RepairedWithResolvedLinks int      `json:"repairedWithResolvedLinks"`
	Unresolved                int      `json:"unresolved"`
	Errors                    int      `json:"errors"`
	Samples                   []string `json:"samples,omitempty"`
}

func main() {
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	artifactsRepo := repo.NewMaterializedArtifactsRepository(db)
	fragmentsRepo := repo.NewHorizonFragmentsRepository(db)

	items, err := loadArtifacts(ctx, artifactsRepo, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), strings.TrimSpace(*artifactKeyPrefix), *limit)
	if err != nil {
		log.Fatalf("load artifacts: %v", err)
	}

	out := report{
		TenantID: strings.TrimSpace(*tenantID),
		LensID:   strings.TrimSpace(*lensID),
		Samples:  make([]string, 0, 20),
	}

	for _, artifact := range items {
		if artifact == nil || artifact.Kind != models.MaterializedKindSupplyChainPath {
			continue
		}
		out.Scanned++
		createdLinks := 0
		resolvedLinks := 0
		summary, err := models.RepairSupplyChainPathLinkage(artifact, func(canonicalPath string) (string, string, bool) {
			frag, ferr := fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
			if ferr == nil && frag != nil {
				resolvedLinks++
				return strings.TrimSpace(frag.ID), strings.TrimSpace(frag.TenantID), true
			}
			created, cerr := ensureSupplyLink(ctx, fragmentsRepo, artifact, canonicalPath)
			if cerr == nil && created != nil {
				createdLinks++
				return strings.TrimSpace(created.ID), strings.TrimSpace(created.TenantID), true
			}
			return "", "", false
		})
		if err != nil {
			out.Errors++
			appendSample(&out, fmt.Sprintf("artifact=%s error=%v", artifact.CanonicalKey, err))
			continue
		}
		if summary.FullyLinked && !summary.Changed {
			out.AlreadyComplete++
			continue
		}
		if !summary.FullyLinked {
			out.Unresolved++
			appendSample(&out, fmt.Sprintf("artifact=%s unresolved=%d", artifact.CanonicalKey, summary.Unresolved))
			continue
		}
		out.RepairedArtifacts++
		out.RepairedWithCreatedLinks += createdLinks
		out.RepairedWithResolvedLinks += resolvedLinks
		appendSample(&out, fmt.Sprintf("artifact=%s changed=%v resolvedIds=%d createdLinks=%d", artifact.CanonicalKey, summary.Changed, summary.ResolvedIDs, createdLinks))
		if *dryRun && !*repairWrite {
			continue
		}
		if summary.Changed {
			if err := artifactsRepo.Update(ctx, artifact); err != nil {
				out.Errors++
				appendSample(&out, fmt.Sprintf("artifact=%s updateErr=%v", artifact.CanonicalKey, err))
			}
		}
	}

	raw, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(raw))
	if strings.TrimSpace(*reportJSON) != "" {
		if err := os.WriteFile(strings.TrimSpace(*reportJSON), raw, 0o644); err != nil {
			log.Fatalf("write report: %v", err)
		}
	}
}

func loadArtifacts(ctx context.Context, artifactsRepo *repo.MaterializedArtifactsRepository, tenantID, lensID, artifactKeyPrefix string, limit int) ([]*models.MaterializedArtifact, error) {
	if strings.TrimSpace(lensID) != "" {
		items, err := artifactsRepo.ListByLens(ctx, lensID, limit)
		if err != nil {
			return nil, err
		}
		out := make([]*models.MaterializedArtifact, 0, len(items))
		for _, item := range items {
			if item == nil || item.Kind != models.MaterializedKindSupplyChainPath {
				continue
			}
			if tenantID != "" && tenantID != "global" && strings.TrimSpace(item.TenantID) != tenantID {
				continue
			}
			if artifactKeyPrefix != "" && !strings.HasPrefix(strings.TrimSpace(item.CanonicalKey), artifactKeyPrefix) {
				continue
			}
			out = append(out, item)
		}
		return out, nil
	}
	items, err := artifactsRepo.ListByTenantAndKind(ctx, tenantID, models.MaterializedKindSupplyChainPath, limit)
	if err != nil || artifactKeyPrefix == "" {
		return items, err
	}
	out := make([]*models.MaterializedArtifact, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(item.CanonicalKey), artifactKeyPrefix) {
			out = append(out, item)
		}
	}
	return out, nil
}

func ensureSupplyLink(ctx context.Context, fragmentsRepo *repo.HorizonFragmentsRepository, artifact *models.MaterializedArtifact, canonicalPath string) (*models.HorizonFragment, error) {
	parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		return nil, err
	}
	for _, leg := range parsed.Legs {
		expected, cerr := models.BuildSupplyLinkCanonicalPath(
			firstNonEmpty(strings.TrimSpace(artifact.TenantID), "global"),
			stringPtr(strings.TrimSpace(leg.SupplierRef)),
			stringPtr(strings.TrimSpace(leg.ProductRef)),
			stringPtr(strings.TrimSpace(leg.CustomerRef)),
		)
		if cerr != nil || strings.TrimSpace(expected) != strings.TrimSpace(canonicalPath) {
			continue
		}
		supplierRef := strings.TrimSpace(leg.SupplierRef)
		productRef := strings.TrimSpace(leg.ProductRef)
		customerRef := strings.TrimSpace(leg.CustomerRef)
		frag := &models.HorizonFragment{
			Type:             models.FragmentTypeSupplyLink,
			TenantID:         firstNonEmpty(strings.TrimSpace(artifact.TenantID), "global"),
			CanonicalPath:    strings.TrimSpace(canonicalPath),
			SupplierRef:      &supplierRef,
			ProductRef:       &productRef,
			CustomerRef:      &customerRef,
			TransportViaRefs: append([]string(nil), leg.TransportViaRefs...),
			Status:           "active",
			Governance:       models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
			Provenance:       models.Provenance{Method: "backfill_supply_chain_linkage"},
			Metadata:         models.FragmentMetadata{CreatedAt: time.Now().UTC(), LastUpdatedAt: time.Now().UTC(), EpistemicStatus: models.EpistemicStatusLowConfidence},
			State: map[string]models.MetricValue{
				"confidence": {Value: firstNonZero(leg.BindingConfidence, leg.SegmentConfidence, 0.5), Confidence: 0.5},
			},
		}
		if err := frag.Validate(); err != nil {
			return nil, err
		}
		id, err := fragmentsRepo.Create(ctx, frag)
		if err != nil {
			return nil, err
		}
		frag.ID = id
		return frag, nil
	}
	return nil, fmt.Errorf("no leg matched canonicalPath=%s", canonicalPath)
}

func appendSample(out *report, sample string) {
	if out == nil || len(out.Samples) >= 20 {
		return
	}
	out.Samples = append(out.Samples, sample)
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	v := value
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

var _ *firestore.Client

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	horizongcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type artifactMetricRow struct {
	Artifact  *horizonmodels.MaterializedArtifact
	Ref       horizonmodels.ScopeArtifactRef
	RiskScore float64
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	lensID := flag.String("lens", "l8dOd17uKYYTKD0CI9hy", "lens id")
	limit := flag.Int("limit", 0, "max artifacts to backfill (0 = all relevant)")
	minRisk := flag.Float64("min-risk", 0, "minimum derived risk score")
	excludeProductContains := flag.String("exclude-product-contains", "", "comma separated product substrings to exclude from manual backfill")
	dryRun := flag.Bool("dry-run", false, "print selected artifacts without writing")
	flag.Parse()

	ctx := context.Background()
	fs, err := horizongcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	lensesRepo := horizonrepo.NewLensesRepository(fs.Client())
	materializedRepo := horizonrepo.NewMaterializedArtifactsRepository(fs.Client())

	lens, err := lensesRepo.Get(ctx, strings.TrimSpace(*lensID))
	if err != nil {
		log.Fatalf("load lens failed: %v", err)
	}
	artifacts, err := materializedRepo.ListByLens(ctx, strings.TrimSpace(*lensID), 512)
	if err != nil {
		log.Fatalf("list artifacts failed: %v", err)
	}

	rows := make([]artifactMetricRow, 0, len(artifacts))
	excludedTerms := compactStrings(strings.Split(strings.ToLower(strings.TrimSpace(*excludeProductContains)), ","))
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		if shouldExcludeProductAxis(artifact.ProductAxis, excludedTerms) {
			continue
		}
		ref := scopeArtifactRefFromArtifact(artifact)
		riskScore := derivedArtifactRiskScore(artifact)
		if riskScore < *minRisk {
			continue
		}
		rows = append(rows, artifactMetricRow{
			Artifact:  artifact,
			Ref:       ref,
			RiskScore: riskScore,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RiskScore == rows[j].RiskScore {
			return strings.TrimSpace(rows[i].Artifact.CanonicalKey) < strings.TrimSpace(rows[j].Artifact.CanonicalKey)
		}
		return rows[i].RiskScore > rows[j].RiskScore
	})
	if *limit > 0 && len(rows) > *limit {
		rows = rows[:*limit]
	}

	fmt.Printf("lens=%s name=%q selected=%d dryRun=%v\n", lens.ID, lens.Name, len(rows), *dryRun)
	for idx, row := range rows {
		fmt.Printf(
			"%02d risk=%.3f stress=%.3f binding=%.3f recovery=%.1fh product=%s key=%s\n",
			idx+1,
			row.RiskScore,
			row.Ref.PathStressScore,
			row.Ref.BindingConfidence,
			row.Ref.RecoveryLatencyHrs,
			row.Ref.ProductAxis,
			row.Artifact.CanonicalKey,
		)
		if *dryRun {
			continue
		}
		if err := lensesRepo.UpsertScopeArtifact(ctx, lens.ID, strings.TrimPrefix(strings.TrimSpace(row.Artifact.CanonicalKey), "/"), row.Ref); err != nil {
			log.Fatalf("upsert scope artifact %s failed: %v", row.Artifact.CanonicalKey, err)
		}
	}
}

func scopeArtifactRefFromArtifact(artifact *horizonmodels.MaterializedArtifact) horizonmodels.ScopeArtifactRef {
	ref := horizonmodels.ScopeArtifactRef{
		Kind:               string(artifact.Kind),
		DisplayName:        artifact.DisplayName,
		AnchorSupplier:     artifact.AnchorSupplier,
		AnchorCustomer:     artifact.AnchorCustomer,
		ProductAxis:        artifact.ProductAxis,
		NodeRefs:           append([]string(nil), artifact.NodeRefs...),
		Status:             artifact.PathStatus,
		GeoRefs:            append([]string(nil), artifact.GeoRefs...),
		FlowRefs:           append([]string(nil), artifact.FlowRefs...),
		ZoneRefs:           append([]string(nil), artifact.ZoneRefs...),
		AffectedZoneRefs:   append([]string(nil), artifact.AffectedZoneRefs...),
		LegCount:           artifact.LegCount,
		BindingConfidence:  artifact.BindingScore,
		PathStressScore:    artifact.PathStressScore,
		BottleneckLegIndex: artifact.BottleneckLeg,
		RecoveryLatencyHrs: artifact.RecoveryLatencyHrs,
	}
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
		summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
		ref.GeoRefs = summary.GeoRefs
		ref.FlowRefs = summary.FlowRefs
		ref.ZoneRefs = summary.ZoneRefs
		ref.AffectedZoneRefs = summary.AffectedZoneRefs
		ref.SpatialSemantics = summary.SpatialSemantics
		ref.LegCount = summary.LegCount
		ref.BindingConfidence = summary.BindingConfidence
		ref.PathStressScore = summary.PathStressScore
		ref.BottleneckLegIndex = summary.BottleneckLegIndex
		ref.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
	}
	return ref
}

func derivedArtifactRiskScore(artifact *horizonmodels.MaterializedArtifact) float64 {
	bindingDeficit := 1 - clamp(artifact.BindingScore, 0, 1)
	stress := clamp(artifact.PathStressScore, 0, 1)
	recovery := clamp(artifact.RecoveryLatencyHrs/96.0, 0, 1)
	return round3(stress*1.4 + bindingDeficit*0.7 + recovery*0.5)
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func round3(value float64) float64 {
	return float64(int(value*1000+0.5)) / 1000
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
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
	return out
}

func shouldExcludeProductAxis(productAxis string, terms []string) bool {
	productAxis = strings.ToLower(strings.TrimSpace(productAxis))
	if productAxis == "" || len(terms) == 0 {
		return false
	}
	for _, term := range terms {
		if term != "" && strings.Contains(productAxis, term) {
			return true
		}
	}
	return false
}

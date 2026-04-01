package main

import (
	"context"
	"encoding/json"
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

type artifactView struct {
	ID             string   `json:"id"`
	DisplayName    string   `json:"displayName"`
	PathStatus     string   `json:"pathStatus,omitempty"`
	ProductAxis    string   `json:"productAxis,omitempty"`
	AnchorSupplier string   `json:"anchorSupplier,omitempty"`
	AnchorCustomer string   `json:"anchorCustomer,omitempty"`
	BindingScore   float64  `json:"bindingScore,omitempty"`
	PathStress     float64  `json:"pathStressScore,omitempty"`
	NodeRefs       []string `json:"nodeRefs,omitempty"`
	SourceIDs      []string `json:"candidateSourceArtifactIds,omitempty"`
	SourceKeys     []string `json:"candidateSourceArtifactKeys,omitempty"`
	Legs           []legView `json:"legs,omitempty"`
}

type legView struct {
	Index       int     `json:"index"`
	SupplierRef string  `json:"supplierRef,omitempty"`
	CustomerRef string  `json:"customerRef,omitempty"`
	ProductRef  string  `json:"productRef,omitempty"`
	Binding     float64 `json:"bindingConfidence,omitempty"`
	Segment     float64 `json:"segmentConfidence,omitempty"`
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	lensID := flag.String("lens", "l8dOd17uKYYTKD0CI9hy", "lens id")
	country := flag.String("country", "", "country code filter, e.g. kz")
	product := flag.String("product", "", "product slug substring, e.g. drill-bits")
	flag.Parse()

	ctx := context.Background()
	fs, err := horizongcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	repo := horizonrepo.NewMaterializedArtifactsRepository(fs.Client())
	artifacts, err := repo.ListByLens(ctx, strings.TrimSpace(*lensID), 1000)
	if err != nil {
		log.Fatalf("list artifacts failed: %v", err)
	}

	countryFilter := strings.ToLower(strings.TrimSpace(*country))
	productFilter := strings.ToLower(strings.TrimSpace(*product))
	out := make([]artifactView, 0)
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		productAxis := strings.TrimSpace(artifact.ProductAxis)
		if productFilter != "" && !strings.Contains(strings.ToLower(productAxis), productFilter) && !strings.Contains(strings.ToLower(artifact.DisplayName), productFilter) {
			continue
		}
		if countryFilter != "" && !containsCountry(artifact.NodeRefs, countryFilter) && !containsCountry(artifact.GeoRefs, countryFilter) {
			continue
		}
		sourceIDs, sourceKeys := extractCandidateSources(artifact.Payload)
		legs, _ := horizonmodels.GetSupplyChainPathLegs(artifact)
		legViews := make([]legView, 0, len(legs))
		for _, leg := range legs {
			legViews = append(legViews, legView{
				Index:       leg.Index,
				SupplierRef: strings.TrimSpace(leg.SupplierRef),
				CustomerRef: strings.TrimSpace(leg.CustomerRef),
				ProductRef:  strings.TrimSpace(leg.ProductRef),
				Binding:     leg.BindingConfidence,
				Segment:     leg.SegmentConfidence,
			})
		}
		out = append(out, artifactView{
			ID:             strings.TrimSpace(artifact.ID),
			DisplayName:    strings.TrimSpace(artifact.DisplayName),
			PathStatus:     strings.TrimSpace(artifact.PathStatus),
			ProductAxis:    productAxis,
			AnchorSupplier: strings.TrimSpace(artifact.AnchorSupplier),
			AnchorCustomer: strings.TrimSpace(artifact.AnchorCustomer),
			BindingScore:   artifact.BindingScore,
			PathStress:     artifact.PathStressScore,
			NodeRefs:       artifact.NodeRefs,
			SourceIDs:      sourceIDs,
			SourceKeys:     sourceKeys,
			Legs:           legViews,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ProductAxis == out[j].ProductAxis {
			if out[i].PathStatus == out[j].PathStatus {
				return out[i].DisplayName < out[j].DisplayName
			}
			return out[i].PathStatus < out[j].PathStatus
		}
		return out[i].ProductAxis < out[j].ProductAxis
	})

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatalf("encode failed: %v", err)
	}
}

func extractCandidateSources(payload map[string]any) ([]string, []string) {
	notes, _ := payload["notes"].(map[string]any)
	if notes == nil {
		notes, _ = payload["Notes"].(map[string]any)
	}
	return toStringSlice(notes["candidateSourceArtifactIds"]), toStringSlice(notes["candidateSourceArtifactKeys"])
}

func toStringSlice(value any) []string {
	raw, ok := value.([]any)
	if ok {
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if direct, ok := value.([]string); ok {
		return append([]string(nil), direct...)
	}
	return nil
}

func containsCountry(refs []string, country string) bool {
	for _, ref := range refs {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), "/fac/"+country+"/") ||
			strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), "/org/"+country+"/") ||
			strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), "/loc/"+country) {
			return true
		}
	}
	return false
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

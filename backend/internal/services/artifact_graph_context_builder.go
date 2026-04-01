package services

import (
	"context"
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/supplychainreport"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type artifactGraphLensGetter interface {
	GetLens(ctx context.Context, lensID string) (*horizonmodels.Lens, error)
}

type ArtifactGraphContextBuilderService struct {
	lenses       artifactGraphLensGetter
	materialized supplychainreport.MaterializedArtifactsPort
	fragments    supplychainreport.FragmentsPort
	edges        supplychainreport.EdgesPort
}

func NewArtifactGraphContextBuilder(
	lenses artifactGraphLensGetter,
	materialized supplychainreport.MaterializedArtifactsPort,
	fragments supplychainreport.FragmentsPort,
	edges supplychainreport.EdgesPort,
) *ArtifactGraphContextBuilderService {
	return &ArtifactGraphContextBuilderService{
		lenses:       lenses,
		materialized: materialized,
		fragments:    fragments,
		edges:        edges,
	}
}

func (s *ArtifactGraphContextBuilderService) Build(
	ctx context.Context,
	tenantID string,
	_ string,
	source models.ArtifactGraphContextSource,
) (*GraphContext, error) {
	kind := strings.TrimSpace(source.Kind)
	if kind == "" {
		return nil, nil
	}
	switch kind {
	case "lens_supply_chain_report":
		return s.buildLensSupplyChainReportContext(ctx, tenantID, source)
	default:
		return nil, nil
	}
}

func (s *ArtifactGraphContextBuilderService) buildLensSupplyChainReportContext(
	ctx context.Context,
	tenantID string,
	source models.ArtifactGraphContextSource,
) (*GraphContext, error) {
	if s == nil || s.lenses == nil || s.materialized == nil || s.fragments == nil || s.edges == nil {
		return nil, fmt.Errorf("graph context builder is not fully configured")
	}
	if source.LensID == nil || strings.TrimSpace(*source.LensID) == "" {
		return nil, fmt.Errorf("graph source lensId is required")
	}
	lens, err := s.lenses.GetLens(ctx, strings.TrimSpace(*source.LensID))
	if err != nil {
		return nil, err
	}
	if lens == nil {
		return nil, fmt.Errorf("lens not found")
	}
	if tenantID != "" && strings.TrimSpace(lens.TenantID) != "" && strings.TrimSpace(lens.TenantID) != strings.TrimSpace(tenantID) {
		return nil, fmt.Errorf("lens does not belong to tenant")
	}
	input, err := supplychainreport.CollectInput(ctx, lens, firstNonEmptyNonNil(strings.TrimSpace(tenantID), strings.TrimSpace(lens.TenantID)), s.materialized, s.fragments, s.edges, 160)
	if err != nil {
		return nil, err
	}
	packages := supplychainreport.BuildSectionPackages(input)
	sections := make(map[string]GraphSectionContext, len(packages))
	for _, pkg := range packages {
		sections[pkg.ID] = GraphSectionContext{
			ID:                 pkg.ID,
			Title:              pkg.Title,
			Description:        pkg.Description,
			Prompt:             pkg.Prompt,
			DeterministicDraft: pkg.DeterministicDraft,
			FactsJSON:          pkg.LLMReadyFactsJSON,
		}
	}
	visiblePaths := len(input.Chains)
	visibleChains := len(packages)
	visibleHubs := visibleHubCount(packages)
	summary := fmt.Sprintf(
		"Visible supply-chain evidence for lens %q includes %d documented routes rendered into %d consulting sections.",
		safeSubject(lens),
		visiblePaths,
		len(packages),
	)
	return &GraphContext{
		Kind:          source.Kind,
		LensID:        source.LensID,
		ReportType:    source.ReportType,
		Subject:       safeSubject(lens),
		Summary:       summary,
		VisiblePaths:  &visiblePaths,
		VisibleChains: &visibleChains,
		VisibleHubs:   &visibleHubs,
		Sections:      sections,
	}, nil
}

func visibleHubCount(packages []supplychainreport.SectionPackage) int {
	for _, pkg := range packages {
		if strings.TrimSpace(pkg.ID) == "industrial_roles_and_footprint" || strings.TrimSpace(pkg.ID) == "logistics_architecture_and_corridors" {
			text := pkg.LLMReadyFactsJSON
			if strings.TrimSpace(text) == "" {
				continue
			}
			return strings.Count(text, "\"name\":")
		}
	}
	return 0
}

func safeSubject(lens *horizonmodels.Lens) string {
	if lens == nil {
		return "Lens"
	}
	if value := strings.TrimSpace(lens.Name); value != "" {
		return value
	}
	if value := strings.TrimSpace(lens.ID); value != "" {
		return value
	}
	return "Lens"
}

func firstNonEmptyNonNil(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

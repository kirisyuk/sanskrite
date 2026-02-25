package scout

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ensureSegmentDerivedMarketLinks best-effort materializes segment-first market nodes and links a fragment into them.
//
// Segment-first principle:
// - ORG/FAC/GOV fragments carry primary market classification as MarketSegments (/segment/...).
// - We materialize /market/... nodes derived from segments for:
//   - hierarchy (HAS_MARKET_SEGMENT)
//   - market-level metrics (TAM/CAGR/etc.)
//   - stable traversal and aggregation
//
// This function is intentionally non-blocking:
// - It caps fanout to avoid runaway writes.
// - Any error is logged and swallowed.
func (e *ScoutEngine) ensureSegmentDerivedMarketLinks(ctx context.Context, fragment *models.HorizonFragment, provenanceMethod, sourceID string) {
	if e == nil || fragment == nil || strings.TrimSpace(fragment.ID) == "" || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return
	}
	// Only link concrete actors (ORG/FAC). GOV is stored as ORGANIZATION and is included.
	if fragment.Type != models.FragmentTypeOrganization && fragment.Type != models.FragmentTypeFacility {
		return
	}
	if len(fragment.MarketSegments) == 0 {
		return
	}

	// Cap to avoid runaway writes (LLM can output many segments).
	segs := fragment.MarketSegments
	if len(segs) > 12 {
		segs = segs[:12]
	}

	// Extract org jurisdiction for geo-validation.
	// An org should only be linked to markets in its own geo or "global"/"int".
	orgGeo := strings.ToLower(strings.TrimSpace(extractJurisdiction(fragment.CanonicalPath)))
	// Also accept the explicit jurisdictionIso2 identifier if canonical path has no geo.
	if orgGeo == "" || orgGeo == "na" {
		if iso2 := strings.ToLower(strings.TrimSpace(fragment.Identifiers["jurisdictionIso2"])); len(iso2) == 2 {
			orgGeo = iso2
		}
	}

	now := time.Now().UTC()
	for _, rawSeg := range segs {
		rawSeg = strings.TrimSpace(rawSeg)
		if rawSeg == "" {
			continue
		}
		seg, ok := taxonomy.NormalizeSegmentPath(rawSeg)
		if !ok || seg == "" {
			continue
		}

		marketPath, ok := taxonomy.MarketPathFromSegmentPath(seg)
		if !ok || strings.TrimSpace(marketPath) == "" {
			continue
		}

		// Geo-validation: org can only participate in markets matching its own geo or "global"/"int".
		// This prevents a Saudi org from being linked to /market/kz/... just because
		// the LLM generated a segment with geo=kz.
		// Skip for global/int/regional orgs — they can participate in any market.
		if orgGeo != "" && orgGeo != "na" && orgGeo != "global" && orgGeo != "int" &&
			orgGeo != "gcc" && orgGeo != "eu" && orgGeo != "eaeu" && orgGeo != "asean" &&
			orgGeo != "brics" && orgGeo != "opec" {
			marketGeo := extractMarketGeo(marketPath)
			if marketGeo != "" && marketGeo != "global" && marketGeo != "int" && marketGeo != orgGeo &&
				!taxonomy.IsCountryInRegion(marketGeo, orgGeo) {
				// Mismatch: rewrite market path to use org's geo instead.
				marketPath = rewriteMarketGeo(marketPath, orgGeo)
				if marketPath == "" {
					continue
				}
			}
		}

		// Ensure market fragment exists.
		marketFrag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, marketPath)
		if err != nil || marketFrag == nil || strings.TrimSpace(marketFrag.ID) == "" {
			if err != nil && !errors.Is(err, repo.ErrFragmentNotFound) {
				// Unknown error; skip (best-effort).
				continue
			}
			label := marketDisplayLabel(marketPath)
			if strings.TrimSpace(label) == "" {
				label = bestEffortDisplayLabel("", marketPath)
			}
			marketFrag = &models.HorizonFragment{
				TenantID:      "global",
				CanonicalPath: marketPath,
				Type:          models.FragmentTypeMarket,
				Labels:        map[string]string{"en": label},
				Status:        string(models.FragmentStatusActive),
				State:         map[string]models.MetricValue{},
				Governance:    models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
				Provenance: models.Provenance{
					Method: "segment_derived_market",
					SourceIDs: func() []string {
						if strings.TrimSpace(sourceID) == "" {
							return nil
						}
						return []string{strings.TrimSpace(sourceID)}
					}(),
				},
				Metadata: models.FragmentMetadata{
					CreatedAt:       now,
					LastUpdatedAt:   now,
					LastVerifiedAt:  now,
					EpistemicStatus: models.EpistemicStatusLowConfidence,
				},
			}

			// Best-effort: set marketDomain/marketVertical derived from segment mapping (non-blocking).
			if seg2, ok2 := taxonomy.SegmentPathFromMarketPath(marketPath); ok2 && seg2 != "" {
				parts := strings.Split(strings.Trim(seg2, "/"), "/")
				// segment/{domain}/{vertical}/{geo}
				if len(parts) == 4 && parts[0] == "segment" {
					marketFrag.MarketDomain = strings.TrimSpace(parts[1])
					marketFrag.MarketVertical = strings.TrimSpace(parts[2])
				}
			}

			id, cerr := e.fragmentsRepo.Create(ctx, marketFrag)
			if cerr != nil {
				continue
			}
			marketFrag.ID = id

			// Build market hierarchy for newly created market nodes (best-effort).
			e.enrichFragmentHierarchy(ctx, marketFrag, sourceID)
		}

		// Ensure PARTICIPATES_IN edge exists: fragment -> market
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", fragment.ID, marketFrag.ID, models.DependencyTypeParticipatesIn); err == nil {
			continue
		} else if !errors.Is(err, repo.ErrEdgeNotFound) {
			continue
		}

		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: fragment.ID,
			TargetFragmentID: marketFrag.ID,
			Type:             models.DependencyTypeParticipatesIn,
			Properties: models.EdgeProperties{
				Criticality:   0.4,
				Amplification: 1.0,
				LatencyHours:  0,
			},
			Qualifiers: map[string]interface{}{
				"derivedFrom": "segment",
				"segmentPath": seg,
				"inferred":    true,
			},
			Provenance: models.Provenance{
				Method: strings.TrimSpace(provenanceMethod),
				SourceIDs: func() []string {
					if strings.TrimSpace(sourceID) == "" {
						return nil
					}
					return []string{strings.TrimSpace(sourceID)}
				}(),
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     now,
				LastUpdatedAt: now,
			},
		}
		if _, err := e.edgesRepo.Create(ctx, edge); err != nil {
			// Best-effort
			_ = err
		}
	}
}

// extractMarketGeo returns the geo segment from a /market/{geo}/... path.
func extractMarketGeo(marketPath string) string {
	parts := strings.Split(strings.Trim(marketPath, "/"), "/")
	if len(parts) >= 2 && parts[0] == "market" {
		return strings.ToLower(strings.TrimSpace(parts[1]))
	}
	return ""
}

// rewriteMarketGeo replaces the geo segment in a /market/{geo}/{domain}/{vertical} path.
func rewriteMarketGeo(marketPath, newGeo string) string {
	parts := strings.Split(strings.Trim(marketPath, "/"), "/")
	if len(parts) < 3 || parts[0] != "market" {
		return ""
	}
	parts[1] = strings.ToLower(strings.TrimSpace(newGeo))
	return "/" + strings.Join(parts, "/")
}

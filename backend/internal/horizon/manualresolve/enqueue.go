// Package manualresolve enqueues human-in-the-loop work surfaced by pipelines (WorkScope, map, QA, etc.).
package manualresolve

import (
	"context"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

const (
	// SourceWorkscopeLensVolume is used when scanning fragments from a lens-derived work volume.
	SourceWorkscopeLensVolume = "workscope_lens_volume"
)

// FragmentLikelyNeedsGeoCoordinates mirrors product expectations: org/gov/fac paths and key fragment types
// should be mappable; absence of resolvable coordinates is an enrichment gap (not a map filter).
func FragmentLikelyNeedsGeoCoordinates(f *horizonmodels.HorizonFragment) bool {
	if f == nil {
		return false
	}
	p := strings.TrimSpace(f.CanonicalPath)
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "/org/") || strings.HasPrefix(p, "/gov/") || strings.HasPrefix(p, "/fac/") {
		return true
	}
	switch f.Class {
	case FragmentTypeOrganization, FragmentTypeFacility:
		return true
	default:
		return false
	}
}

// EnqueueMissingGeoForOrgGovFac upserts queue rows for fragments that should have coordinates but do not.
func EnqueueMissingGeoForOrgGovFac(
	ctx context.Context,
	q *horizonrepo.ManualResolveQueueRepository,
	tenantID, projectID, lensID, workScopeDigest, source string,
	fragments []*horizonmodels.HorizonFragment,
) (int, error) {
	if q == nil || len(fragments) == 0 {
		return 0, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return 0, nil
	}
	n := 0
	for _, f := range fragments {
		if f == nil {
			continue
		}
		if !FragmentLikelyNeedsGeoCoordinates(f) {
			continue
		}
		if _, _, ok := horizonrepo.ResolveFragmentCoordinates(f); ok {
			continue
		}
		path := strings.TrimSpace(f.CanonicalPath)
		if path == "" {
			continue
		}
		item := &horizonmodels.ManualResolveQueueItem{
			TenantID:        tenantID,
			ProjectID:       strings.TrimSpace(projectID),
			LensID:          strings.TrimSpace(lensID),
			FragmentID:      strings.TrimSpace(f.ID),
			CanonicalPath:   path,
			FragmentClass:   f.Class,
			EnrichmentKinds: []string{horizonmodels.EnrichmentKindGeoCoordinates},
			Status:          horizonmodels.ManualResolveQueueStatusOpen,
			Priority:        55,
			Source:          strings.TrimSpace(source),
			WorkScopeDigest: strings.TrimSpace(workScopeDigest),
		}
		if item.Source == "" {
			item.Source = SourceWorkscopeLensVolume
		}
		if err := q.Upsert(ctx, item); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

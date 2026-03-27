package workscope

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/manualresolve"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// RecordManualResolveGeoGaps enriches the manual resolve queue from a fragment set (e.g. lens work volume).
// Geo filtering for display remains optional elsewhere; this only records human follow-ups.
func RecordManualResolveGeoGaps(
	ctx context.Context,
	q *horizonrepo.ManualResolveQueueRepository,
	tenantID, projectID, lensID, workScopeDigest, source string,
	fragments []*horizonmodels.HorizonFragment,
) (int, error) {
	return manualresolve.EnqueueMissingGeoForOrgGovFac(ctx, q, tenantID, projectID, lensID, workScopeDigest, source, fragments)
}

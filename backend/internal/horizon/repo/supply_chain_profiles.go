package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type WorkingProductScopesRepository struct {
	col *firestore.CollectionRef
}

func NewWorkingProductScopesRepository(db *firestore.Client) *WorkingProductScopesRepository {
	return &WorkingProductScopesRepository{col: db.Collection("horizon_working_product_scopes")}
}

func (r *WorkingProductScopesRepository) GetByJobID(ctx context.Context, jobID string) (*models.WorkingProductScope, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("working product scopes repo not configured")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, ErrInvalidDocument
	}
	docs, err := r.col.Where("jobId", "==", jobID).Limit(1).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}
	var scope models.WorkingProductScope
	if err := docs[0].DataTo(&scope); err != nil {
		return nil, err
	}
	return &scope, nil
}

func (r *WorkingProductScopesRepository) Upsert(ctx context.Context, scope *models.WorkingProductScope) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("working product scopes repo not configured")
	}
	if scope == nil {
		return ErrInvalidDocument
	}
	scope.Normalize()
	docID := strings.TrimSpace(scope.JobID)
	if docID == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(docID).Set(ctx, scope)
	return err
}

type CapabilityProfilesRepository struct {
	col *firestore.CollectionRef
}

func NewCapabilityProfilesRepository(db *firestore.Client) *CapabilityProfilesRepository {
	return &CapabilityProfilesRepository{col: db.Collection("horizon_capability_profiles")}
}

func (r *CapabilityProfilesRepository) Get(ctx context.Context, tenantID, carrierRef, productRef string) (*models.CapabilityProfile, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("capability profiles repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	carrierRef = strings.TrimSpace(carrierRef)
	productRef = strings.TrimSpace(productRef)
	if tenantID == "" || carrierRef == "" || productRef == "" {
		return nil, ErrInvalidDocument
	}
	doc, err := r.col.Doc(capabilityProfileDocID(tenantID, carrierRef, productRef)).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var profile models.CapabilityProfile
	if err := doc.DataTo(&profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *CapabilityProfilesRepository) Upsert(ctx context.Context, profile *models.CapabilityProfile) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("capability profiles repo not configured")
	}
	if profile == nil {
		return ErrInvalidDocument
	}
	profile.CarrierRef = strings.TrimSpace(profile.CarrierRef)
	profile.ProductRef = strings.TrimSpace(profile.ProductRef)
	profile.TenantID = strings.TrimSpace(profile.TenantID)
	if profile.TenantID == "" || profile.CarrierRef == "" || profile.ProductRef == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	_, err := r.col.Doc(capabilityProfileDocID(profile.TenantID, profile.CarrierRef, profile.ProductRef)).Set(ctx, profile)
	return err
}

type CapabilityObservationsRepository struct {
	col *firestore.CollectionRef
}

func NewCapabilityObservationsRepository(db *firestore.Client) *CapabilityObservationsRepository {
	return &CapabilityObservationsRepository{col: db.Collection("horizon_capability_observations")}
}

func (r *CapabilityObservationsRepository) Create(ctx context.Context, obs *models.CapabilityObservation) (string, error) {
	if r == nil || r.col == nil {
		return "", fmt.Errorf("capability observations repo not configured")
	}
	if obs == nil {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	if obs.CreatedAt.IsZero() {
		obs.CreatedAt = now
	}
	obs.UpdatedAt = now
	doc := r.col.NewDoc()
	obs.ID = doc.ID
	_, err := doc.Set(ctx, obs)
	return obs.ID, err
}

type SupplyLinkObservationsRepository struct {
	col *firestore.CollectionRef
}

func NewSupplyLinkObservationsRepository(db *firestore.Client) *SupplyLinkObservationsRepository {
	return &SupplyLinkObservationsRepository{col: db.Collection("horizon_supply_link_observations")}
}

func (r *SupplyLinkObservationsRepository) Create(ctx context.Context, obs *models.SupplyLinkObservation) (string, error) {
	if r == nil || r.col == nil {
		return "", fmt.Errorf("supply link observations repo not configured")
	}
	if obs == nil {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	if obs.CreatedAt.IsZero() {
		obs.CreatedAt = now
	}
	obs.UpdatedAt = now
	doc := r.col.NewDoc()
	obs.ID = doc.ID
	_, err := doc.Set(ctx, obs)
	return obs.ID, err
}

type RouteObservationsRepository struct {
	col *firestore.CollectionRef
}

func NewRouteObservationsRepository(db *firestore.Client) *RouteObservationsRepository {
	return &RouteObservationsRepository{col: db.Collection("horizon_route_observations")}
}

func (r *RouteObservationsRepository) Create(ctx context.Context, obs *models.RouteObservation) (string, error) {
	if r == nil || r.col == nil {
		return "", fmt.Errorf("route observations repo not configured")
	}
	if obs == nil {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	if obs.CreatedAt.IsZero() {
		obs.CreatedAt = now
	}
	obs.UpdatedAt = now
	doc := r.col.NewDoc()
	obs.ID = doc.ID
	_, err := doc.Set(ctx, obs)
	return obs.ID, err
}

func (r *RouteObservationsRepository) ListByTenantAndSupplyLinkCanonicalPath(
	ctx context.Context,
	tenantID string,
	supplyLinkCanonicalPath string,
	limit int,
) ([]*models.RouteObservation, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("route observations repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	supplyLinkCanonicalPath = strings.TrimSpace(supplyLinkCanonicalPath)
	if tenantID == "" || supplyLinkCanonicalPath == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 200
	}
	docs, err := r.col.
		Where("tenantId", "==", tenantID).
		Where("supplyLinkCanonicalPath", "==", supplyLinkCanonicalPath).
		Limit(limit).
		Documents(ctx).
		GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.RouteObservation, 0, len(docs))
	for _, doc := range docs {
		var obs models.RouteObservation
		if err := doc.DataTo(&obs); err != nil {
			return nil, err
		}
		if obs.ID == "" {
			obs.ID = doc.Ref.ID
		}
		out = append(out, &obs)
	}
	return out, nil
}

type RouteGeometryProfilesRepository struct {
	col *firestore.CollectionRef
}

func NewRouteGeometryProfilesRepository(db *firestore.Client) *RouteGeometryProfilesRepository {
	return &RouteGeometryProfilesRepository{col: db.Collection("horizon_route_geometry_profiles")}
}

func (r *RouteGeometryProfilesRepository) GetByProfileKey(ctx context.Context, tenantID, profileKey string) (*models.RouteGeometryProfile, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("route geometry profiles repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	profileKey = strings.TrimSpace(profileKey)
	if tenantID == "" || profileKey == "" {
		return nil, ErrInvalidDocument
	}
	doc, err := r.col.Doc(routeGeometryDocID(tenantID, profileKey)).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	if doc == nil || !doc.Exists() {
		return nil, nil
	}
	var profile models.RouteGeometryProfile
	if err := doc.DataTo(&profile); err != nil {
		return nil, err
	}
	if profile.ID == "" {
		profile.ID = doc.Ref.ID
	}
	if len(profile.Coordinates) < 2 && strings.TrimSpace(profile.EncodedPolyline) != "" {
		profile.Coordinates = models.DecodeRoutePolyline(profile.EncodedPolyline)
	}
	return &profile, nil
}

func (r *RouteGeometryProfilesRepository) Upsert(ctx context.Context, profile *models.RouteGeometryProfile) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("route geometry profiles repo not configured")
	}
	if profile == nil {
		return ErrInvalidDocument
	}
	profile.Normalize()
	if profile.TenantID == "" || profile.ProfileKey == "" {
		return ErrInvalidDocument
	}
	docID := routeGeometryDocID(profile.TenantID, profile.ProfileKey)
	profile.ID = docID
	stored := *profile
	if len(stored.Coordinates) >= 2 && strings.TrimSpace(stored.EncodedPolyline) == "" {
		stored.EncodedPolyline = models.EncodeRoutePolyline(stored.Coordinates)
	}
	stored.Coordinates = nil
	_, err := r.col.Doc(docID).Set(ctx, &stored)
	return err
}

type RouteGeometryCandidatesRepository struct {
	col *firestore.CollectionRef
}

func NewRouteGeometryCandidatesRepository(db *firestore.Client) *RouteGeometryCandidatesRepository {
	return &RouteGeometryCandidatesRepository{col: db.Collection("horizon_route_geometry_candidates")}
}

func (r *RouteGeometryCandidatesRepository) Upsert(ctx context.Context, candidate *models.RouteGeometryCandidate) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("route geometry candidates repo not configured")
	}
	if candidate == nil {
		return ErrInvalidDocument
	}
	candidate.Normalize()
	if candidate.TenantID == "" || candidate.ProfileKey == "" {
		return ErrInvalidDocument
	}
	docRef := r.col.Doc(routeGeometryDocID(candidate.TenantID, candidate.ProfileKey))
	snap, err := docRef.Get(ctx)
	if err != nil && status.Code(err) != codes.NotFound {
		return err
	}
	if snap != nil && snap.Exists() {
		var existing models.RouteGeometryCandidate
		if err := snap.DataTo(&existing); err != nil {
			return err
		}
		existing.Normalize()
		existing.LastSeenAt = candidate.LastSeenAt
		existing.UpdatedAt = candidate.UpdatedAt
		existing.SeenCount++
		if existing.RouteVariantKey == "" {
			existing.RouteVariantKey = candidate.RouteVariantKey
		}
		if existing.SupplyLinkCanonicalPath == "" {
			existing.SupplyLinkCanonicalPath = candidate.SupplyLinkCanonicalPath
		}
		if existing.PathArtifactKey == "" {
			existing.PathArtifactKey = candidate.PathArtifactKey
		}
		if existing.Reason == "" {
			existing.Reason = candidate.Reason
		}
		existing.ID = docRef.ID
		_, err = docRef.Set(ctx, &existing)
		return err
	}
	candidate.ID = docRef.ID
	if candidate.SeenCount <= 0 {
		candidate.SeenCount = 1
	}
	_, err = docRef.Set(ctx, candidate)
	return err
}

type DerivedRiskProfilesRepository struct {
	col *firestore.CollectionRef
}

func NewDerivedRiskProfilesRepository(db *firestore.Client) *DerivedRiskProfilesRepository {
	return &DerivedRiskProfilesRepository{col: db.Collection("horizon_derived_risk_profiles")}
}

func (r *DerivedRiskProfilesRepository) Upsert(ctx context.Context, profile *models.DerivedRiskProfile) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("derived risk profiles repo not configured")
	}
	if profile == nil {
		return ErrInvalidDocument
	}
	profile.ScopeRef = strings.TrimSpace(profile.ScopeRef)
	profile.ProductRef = strings.TrimSpace(profile.ProductRef)
	profile.ProfileType = strings.TrimSpace(profile.ProfileType)
	if profile.ScopeRef == "" || profile.ProfileType == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	docID := strings.Join([]string{
		strings.ReplaceAll(strings.TrimSpace(profile.TenantID), "/", "_"),
		strings.ReplaceAll(profile.ScopeRef, "/", "_"),
		strings.ReplaceAll(profile.ProductRef, "/", "_"),
		profile.ProfileType,
	}, "__")
	_, err := r.col.Doc(docID).Set(ctx, profile)
	return err
}

func capabilityProfileDocID(tenantID, carrierRef, productRef string) string {
	return strings.Join([]string{
		strings.ReplaceAll(strings.TrimSpace(tenantID), "/", "_"),
		strings.ReplaceAll(strings.TrimSpace(carrierRef), "/", "_"),
		strings.ReplaceAll(strings.TrimSpace(productRef), "/", "_"),
	}, "__")
}

func routeGeometryDocID(tenantID, profileKey string) string {
	return strings.Join([]string{
		strings.ReplaceAll(strings.TrimSpace(tenantID), "/", "_"),
		strings.ReplaceAll(strings.TrimSpace(profileKey), "/", "_"),
	}, "__")
}

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

func (r *CapabilityProfilesRepository) Get(ctx context.Context, carrierRef, productRef string) (*models.CapabilityProfile, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("capability profiles repo not configured")
	}
	carrierRef = strings.TrimSpace(carrierRef)
	productRef = strings.TrimSpace(productRef)
	if carrierRef == "" || productRef == "" {
		return nil, ErrInvalidDocument
	}
	doc, err := r.col.Doc(capabilityProfileDocID(carrierRef, productRef)).Get(ctx)
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
	if profile.CarrierRef == "" || profile.ProductRef == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	_, err := r.col.Doc(capabilityProfileDocID(profile.CarrierRef, profile.ProductRef)).Set(ctx, profile)
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
		strings.ReplaceAll(profile.ScopeRef, "/", "_"),
		strings.ReplaceAll(profile.ProductRef, "/", "_"),
		profile.ProfileType,
	}, "__")
	_, err := r.col.Doc(docID).Set(ctx, profile)
	return err
}

func capabilityProfileDocID(carrierRef, productRef string) string {
	return strings.Join([]string{
		strings.ReplaceAll(strings.TrimSpace(carrierRef), "/", "_"),
		strings.ReplaceAll(strings.TrimSpace(productRef), "/", "_"),
	}, "__")
}

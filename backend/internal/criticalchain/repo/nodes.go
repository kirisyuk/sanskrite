package repo

import (
	"context"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
)

const nodesCollection = "criticalchain_nodes"

// NodesRepository manages criticalchain_nodes (Material, Facility).
type NodesRepository struct {
	client *firestore.Client
	col    *firestore.CollectionRef
}

// NewNodesRepository creates a new NodesRepository.
func NewNodesRepository(db *firestore.Client) *NodesRepository {
	return &NodesRepository{
		client: db,
		col:    db.Collection(nodesCollection),
	}
}

// GetMaterial returns a material by ID.
func (r *NodesRepository) GetMaterial(ctx context.Context, id string) (*models.Material, error) {
	doc, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var node models.NodeDocument
	if err := doc.DataTo(&node); err != nil {
		return nil, err
	}
	if node.Kind != models.NodeKindMaterial || node.Material == nil {
		return nil, errNotFound
	}
	return node.Material, nil
}

// GetFacility returns a facility by ID.
func (r *NodesRepository) GetFacility(ctx context.Context, id string) (*models.Facility, error) {
	doc, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var node models.NodeDocument
	if err := doc.DataTo(&node); err != nil {
		return nil, err
	}
	if node.Kind != models.NodeKindFacility || node.Facility == nil {
		return nil, errNotFound
	}
	return node.Facility, nil
}

// ListMaterials returns all materials.
func (r *NodesRepository) ListMaterials(ctx context.Context) ([]*models.Material, error) {
	iter := r.col.Where("kind", "==", string(models.NodeKindMaterial)).Documents(ctx)
	defer iter.Stop()
	var out []*models.Material
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var node models.NodeDocument
		if err := doc.DataTo(&node); err != nil {
			log.Printf("[CriticalChain] skip doc %s: %v", doc.Ref.ID, err)
			continue
		}
		if node.Material != nil {
			out = append(out, node.Material)
		}
	}
	return out, nil
}

// ListFacilities returns facilities with optional filters.
// Filters are applied client-side to avoid Firestore composite index requirements.
func (r *NodesRepository) ListFacilities(ctx context.Context, opts *ListFacilitiesOpts) ([]*models.Facility, error) {
	iter := r.col.Where("kind", "==", string(models.NodeKindFacility)).Documents(ctx)
	defer iter.Stop()
	var out []*models.Facility
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var node models.NodeDocument
		if err := doc.DataTo(&node); err != nil {
			log.Printf("[CriticalChain] skip doc %s: %v", doc.Ref.ID, err)
			continue
		}
		if node.Facility == nil {
			continue
		}
		if opts != nil {
			if opts.MaterialID != "" && node.Facility.MaterialID != opts.MaterialID {
				continue
			}
			if opts.Country != "" && node.Facility.Country != opts.Country {
				continue
			}
			if opts.MinRiskScore > 0 && node.Facility.RiskScore < opts.MinRiskScore {
				continue
			}
		}
		out = append(out, node.Facility)
	}
	return out, nil
}

// ListFacilitiesOpts filters for ListFacilities.
type ListFacilitiesOpts struct {
	MaterialID   string
	Country      string
	MinRiskScore float64
}

// SetMaterial upserts a material.
func (r *NodesRepository) SetMaterial(ctx context.Context, m *models.Material) error {
	if m.ID == "" {
		return errInvalidID
	}
	node := models.NodeDocument{
		ID:        m.ID,
		Kind:      models.NodeKindMaterial,
		Material:  m,
		UpdatedAt: m.UpdatedAt,
	}
	_, err := r.col.Doc(m.ID).Set(ctx, node)
	return err
}

// SetFacility upserts a facility.
func (r *NodesRepository) SetFacility(ctx context.Context, f *models.Facility) error {
	if f.ID == "" {
		return errInvalidID
	}
	node := models.NodeDocument{
		ID:        f.ID,
		Kind:      models.NodeKindFacility,
		Facility:  f,
		UpdatedAt: f.UpdatedAt,
	}
	_, err := r.col.Doc(f.ID).Set(ctx, node)
	return err
}

// UpdateMaterialRisk updates a material's risk score and appends to risk history.
func (r *NodesRepository) UpdateMaterialRisk(ctx context.Context, id string, score float64) error {
	return r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		docRef := r.col.Doc(id)
		doc, err := tx.Get(docRef)
		if err != nil {
			return err
		}
		var node models.NodeDocument
		if err := doc.DataTo(&node); err != nil {
			return err
		}
		if node.Kind != models.NodeKindMaterial || node.Material == nil {
			return errNotFound
		}

		m := node.Material
		m.RiskScore = score
		m.RiskHistory = append(m.RiskHistory, models.RiskPoint{
			Timestamp: time.Now(),
			Value:     score,
		})
		
		if len(m.RiskHistory) > 30 {
			m.RiskHistory = m.RiskHistory[len(m.RiskHistory)-30:]
		}
		m.UpdatedAt = time.Now()

		node.UpdatedAt = m.UpdatedAt
		return tx.Set(docRef, node)
	})
}

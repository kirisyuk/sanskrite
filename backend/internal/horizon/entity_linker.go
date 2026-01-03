package horizon

import (
	"context"
	"fmt"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	ontomodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

// Supported fragment types for auto Entity linking/enrichment (expandable).
// Per current requirement: Organization, Person, GeoRegion (+ Facility, Product).
var supportedFragmentTypes = map[horizonmodels.FragmentType]ontomodels.EntityType{
	horizonmodels.FragmentTypeOrganization: ontomodels.EntityTypeOrganization,
	horizonmodels.FragmentTypePerson:       ontomodels.EntityTypePerson,
	horizonmodels.FragmentTypeGeoRegion:    ontomodels.EntityTypeLocation,
	horizonmodels.FragmentTypeFacility:     ontomodels.EntityTypeFacility,
	horizonmodels.FragmentTypeProduct:      ontomodels.EntityTypeProduct,
}

// EnsureEntityForFragment ensures that a fragment has an Entity mapping:
// - finds existing entity (by fragment canonicalPath / resolver)
// - or creates a new entity
// - links fragment.EntityID
// - best-effort enriches entity via Wikidata (if configured)
//
// It is safe to call on every fragment access; it becomes a no-op once linked.
func (s *HorizonService) EnsureEntityForFragment(ctx context.Context, fragmentID string) (*ontomodels.Entity, bool, bool, error) {
	if s == nil {
		return nil, false, false, fmt.Errorf("horizon service is nil")
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return nil, false, false, fmt.Errorf("fragmentID is required")
	}

	frag, err := s.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return nil, false, false, err
	}
	return s.EnsureEntityForFragmentObject(ctx, frag)
}

// EnsureEntityForFragmentObject is the same as EnsureEntityForFragment but works on an already loaded fragment.
func (s *HorizonService) EnsureEntityForFragmentObject(ctx context.Context, frag *horizonmodels.HorizonFragment) (*ontomodels.Entity, bool, bool, error) {
	if s == nil {
		return nil, false, false, fmt.Errorf("horizon service is nil")
	}
	if frag == nil || strings.TrimSpace(frag.ID) == "" {
		return nil, false, false, fmt.Errorf("fragment is required")
	}

	entityType, ok := supportedFragmentTypes[frag.Type]
	if !ok {
		// Not a supported fragment type: nothing to do.
		return nil, false, false, nil
	}

	// If already linked, just return the entity if available.
	if frag.EntityID != nil && strings.TrimSpace(*frag.EntityID) != "" {
		e, err := s.deps.EntitiesRepo.Get(ctx, strings.TrimSpace(*frag.EntityID))
		if err != nil {
			// If linked entity not found, fall through and attempt to re-link.
		} else {
			// Optionally enrich on access (best-effort, rate-limited inside enricher).
			enriched := false
			if s.deps.EntityEnricher != nil {
				_ = s.deps.EntityEnricher.EnrichEntity(ctx, e.TenantID, e)
				enriched = true
			}
			return e, false, enriched, nil
		}
	}

	if s.deps.EntitiesRepo == nil {
		return nil, false, false, fmt.Errorf("EntitiesRepo is not configured")
	}

	tenantID := "global"

	// 1) Strongest: canonicalPath match (deterministic)
	var entity *ontomodels.Entity
	if strings.TrimSpace(frag.CanonicalPath) != "" {
		if e, err := s.deps.EntitiesRepo.FindByCanonicalPath(ctx, tenantID, strings.TrimSpace(frag.CanonicalPath)); err == nil && e != nil {
			// If entity type mismatches, we still allow linking when entity is Other (common during early extraction).
			if e.Type == entityType || e.Type == ontomodels.EntityTypeOther {
				entity = e
			}
		}
	}

	// 2) Resolver (dedupe across aliases / embeddings / LLM) if configured
	if entity == nil && s.deps.EntityResolver != nil {
		name := deriveCanonicalNameFromFragment(frag)
		if strings.TrimSpace(name) != "" {
			if e, err := s.deps.EntityResolver.ResolveEntity(ctx, tenantID, name, entityType); err == nil && e != nil {
				entity = e
			}
		}
	}

	created := false
	if entity == nil {
		// 3) Create entity
		now := time.Now().UTC()
		name := deriveCanonicalNameFromFragment(frag)
		if strings.TrimSpace(name) == "" {
			// If we cannot derive a stable name, do not create an entity.
			return nil, false, false, nil
		}

		newEntity := &ontomodels.Entity{
			BaseOntologyFields: ontomodels.BaseOntologyFields{
				TenantID:        tenantID,
				Version:         1,
				CreatedAt:       now,
				UpdatedAt:       now,
				CreatedBy:       "system",
				UpdatedBy:       "system",
				Labels:          map[string]string{"en": name},
				Status:          "active",
				CanonicalPaths:  []string{strings.TrimSpace(frag.CanonicalPath)},
				VectorText:      name,
				ActorPaths:      nil,
				SpherePaths:     nil,
				LocationPath:    nil,
			},
			CanonicalName: name,
			Type:          entityType,
			Aliases:       []string{name},
			Identifiers:   copyStringMap(frag.Identifiers),
			IndustryPaths: func() []string {
				if len(frag.IndustryPaths) == 0 {
					return nil
				}
				out := make([]string, 0, len(frag.IndustryPaths))
				seen := map[string]struct{}{}
				for _, p := range frag.IndustryPaths {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					if _, ok := seen[p]; ok {
						continue
					}
					seen[p] = struct{}{}
					out = append(out, p)
				}
				if len(out) == 0 {
					return nil
				}
				return out
			}(),
			Attributes: map[string]interface{}{
				"sourceType":          "horizon_fragment",
				"sourceFragmentId":    strings.TrimSpace(frag.ID),
				"sourceCanonicalPath": strings.TrimSpace(frag.CanonicalPath),
				"fragmentType":        string(frag.Type),
			},
		}

		id, err := s.deps.EntitiesRepo.Create(ctx, newEntity)
		if err != nil {
			return nil, false, false, err
		}
		newEntity.ID = id
		entity = newEntity
		created = true
	}

	// Link fragment -> entity
	eid := strings.TrimSpace(entity.ID)
	frag.EntityID = &eid
	if err := s.fragmentsRepo.Update(ctx, frag); err != nil {
		return nil, created, false, err
	}

	// Best-effort enrich
	enriched := false
	if s.deps.EntityEnricher != nil {
		_ = s.deps.EntityEnricher.EnrichEntity(ctx, entity.TenantID, entity)
		enriched = true
	}

	// Reload entity (ensures we return latest state after enrichment/upsert)
	if eid != "" {
		if e2, err := s.deps.EntitiesRepo.Get(ctx, eid); err == nil && e2 != nil {
			entity = e2
		}
	}

	return entity, created, enriched, nil
}

// EnrichEntityForFragment triggers Wikidata enrichment for the entity mapped to a fragment.
// If mapping doesn't exist, it will be created/linked first.
func (s *HorizonService) EnrichEntityForFragment(ctx context.Context, fragmentID string, force bool) (*ontomodels.Entity, error) {
	entity, _, _, err := s.EnsureEntityForFragment(ctx, fragmentID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, fmt.Errorf("entity not available for this fragment type")
	}
	if s.deps.EntityEnricher == nil {
		return nil, fmt.Errorf("entity enricher is not configured")
	}
	if force {
		if err := s.deps.EntityEnricher.EnrichEntityForce(ctx, entity.TenantID, entity); err != nil {
			return nil, err
		}
	} else {
		if err := s.deps.EntityEnricher.EnrichEntity(ctx, entity.TenantID, entity); err != nil {
			return nil, err
		}
	}
	// Reload latest
	if e2, err := s.deps.EntitiesRepo.Get(ctx, entity.ID); err == nil && e2 != nil {
		return e2, nil
	}
	return entity, nil
}

func deriveCanonicalNameFromFragment(f *horizonmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	// Prefer Labels.en
	if f.Labels != nil {
		if v := strings.TrimSpace(f.Labels["en"]); v != "" {
			return v
		}
	}
	// Prefer identifiers
	if f.Identifiers != nil {
		if v := strings.TrimSpace(f.Identifiers["legalName"]); v != "" {
			return v
		}
		if v := strings.TrimSpace(f.Identifiers["shortName"]); v != "" {
			return v
		}
		if v := strings.TrimSpace(f.Identifiers["name"]); v != "" {
			return v
		}
	}
	// Fallback: last segment of canonicalPath (slug -> words)
	cp := strings.TrimSpace(f.CanonicalPath)
	if cp != "" {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) > 0 {
			last := strings.TrimSpace(parts[len(parts)-1])
			last = strings.ReplaceAll(last, "-", " ")
			if last != "" {
				return last
			}
		}
	}
	return ""
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}



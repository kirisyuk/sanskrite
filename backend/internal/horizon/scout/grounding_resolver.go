package scout

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// GroundingEvidenceResolver resolves grounding evidence from Vertex AI into concrete
// Horizon fragments from the database.
//
// This is the system-level "source of truth" mechanism:
//   - LLM generates entity names, descriptions, relations (creative work)
//   - System uses grounding evidence to determine which entities ALREADY EXIST in the DB
//   - Fragment IDs from datastore citations are loaded from Firestore → exact canonical paths
//   - Pipeline uses DB data for existing entities, not LLM-generated paths
//
// Usage in pipeline:
//
//	evidence := result.GroundingEvidence
//	resolved := resolver.Resolve(ctx, evidence)
//	for _, entity := range llmEntities {
//	    if match := resolved.FindByName(entity.Name); match != nil {
//	        // Entity exists in DB → use match.CanonicalPath, skip dedup
//	    } else {
//	        // New entity → normal create + dedup flow
//	    }
//	}
type GroundingEvidenceResolver struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
}

// NewGroundingEvidenceResolver creates a new resolver.
func NewGroundingEvidenceResolver(fragmentsRepo *repo.HorizonFragmentsRepository) *GroundingEvidenceResolver {
	return &GroundingEvidenceResolver{
		fragmentsRepo: fragmentsRepo,
	}
}

// ResolvedGroundingContext contains the resolved fragments from grounding evidence,
// indexed for fast lookup by various keys (name, canonical path, identifiers).
type ResolvedGroundingContext struct {
	// Fragments — all successfully loaded fragments from datastore citations.
	Fragments []*models.HorizonFragment

	// byCanonicalPath — lookup by canonical path (exact).
	byCanonicalPath map[string]*models.HorizonFragment

	// byNameLower — lookup by lowercased label[en] or any label value.
	byNameLower map[string]*models.HorizonFragment

	// byIdentifier — lookup by identifier value (wikidataId, legalName, shortName, etc.).
	byIdentifier map[string]*models.HorizonFragment

	// HasDatastoreResults — were any fragments found in the datastore.
	HasDatastoreResults bool

	// HasWebResults — were any web sources in the evidence.
	HasWebResults bool
}

// Resolve loads fragments referenced by grounding evidence and builds a lookup context.
// This is the core operation: datastore citation URIs → fragment IDs → Firestore load → indexed context.
func (r *GroundingEvidenceResolver) Resolve(ctx context.Context, evidence *ai.GroundingEvidence) *ResolvedGroundingContext {
	result := &ResolvedGroundingContext{
		byCanonicalPath: map[string]*models.HorizonFragment{},
		byNameLower:     map[string]*models.HorizonFragment{},
		byIdentifier:    map[string]*models.HorizonFragment{},
	}

	if evidence == nil {
		return result
	}

	result.HasDatastoreResults = evidence.HasDatastoreResults
	result.HasWebResults = evidence.HasWebResults

	if len(evidence.DatastoreFragmentIDs) == 0 || r.fragmentsRepo == nil {
		return result
	}

	// Batch-load fragments from Firestore
	fragmentsMap, err := r.fragmentsRepo.GetMany(ctx, evidence.DatastoreFragmentIDs)
	if err != nil {
		log.Printf("[GroundingResolver] Warning: failed to batch-load %d fragments: %v", len(evidence.DatastoreFragmentIDs), err)
		return result
	}

	log.Printf("[GroundingResolver] Loaded %d/%d fragments from grounding evidence",
		len(fragmentsMap), len(evidence.DatastoreFragmentIDs))

	// Index fragments for fast lookup
	for _, frag := range fragmentsMap {
		if frag == nil {
			continue
		}
		result.Fragments = append(result.Fragments, frag)

		// Index by canonical path
		cp := strings.TrimSpace(frag.CanonicalPath)
		if cp != "" {
			result.byCanonicalPath[cp] = frag
		}

		// Index by all label values (lowercased)
		for _, label := range frag.Labels {
			label = strings.TrimSpace(label)
			if label != "" {
				key := strings.ToLower(label)
				// Don't overwrite if already set (first wins — avoids ambiguity)
				if _, exists := result.byNameLower[key]; !exists {
					result.byNameLower[key] = frag
				}
			}
		}

		// Index by identifier values
		for idKey, idVal := range frag.Identifiers {
			idVal = strings.TrimSpace(idVal)
			if idVal == "" {
				continue
			}
			// Index by "key:value" (e.g., "wikidataId:Q329458")
			compositeKey := strings.ToLower(fmt.Sprintf("%s:%s", idKey, idVal))
			if _, exists := result.byIdentifier[compositeKey]; !exists {
				result.byIdentifier[compositeKey] = frag
			}
			// Also index by value alone for common identifiers (wikidataId, legalName, shortName)
			switch idKey {
			case "wikidataId", "legalName", "shortName":
				valKey := strings.ToLower(idVal)
				if _, exists := result.byIdentifier[valKey]; !exists {
					result.byIdentifier[valKey] = frag
				}
			}
		}
	}

	return result
}

// FindByCanonicalPath returns a grounded fragment by exact canonical path, or nil.
func (c *ResolvedGroundingContext) FindByCanonicalPath(canonicalPath string) *models.HorizonFragment {
	if c == nil {
		return nil
	}
	return c.byCanonicalPath[strings.TrimSpace(canonicalPath)]
}

// FindByName returns a grounded fragment by name (case-insensitive label match), or nil.
func (c *ResolvedGroundingContext) FindByName(name string) *models.HorizonFragment {
	if c == nil || name == "" {
		return nil
	}
	return c.byNameLower[strings.ToLower(strings.TrimSpace(name))]
}

// FindByIdentifier returns a grounded fragment by identifier value (e.g., wikidataId, legalName), or nil.
func (c *ResolvedGroundingContext) FindByIdentifier(key, value string) *models.HorizonFragment {
	if c == nil || value == "" {
		return nil
	}
	value = strings.TrimSpace(value)
	if key != "" {
		compositeKey := strings.ToLower(fmt.Sprintf("%s:%s", key, value))
		if frag := c.byIdentifier[compositeKey]; frag != nil {
			return frag
		}
	}
	return c.byIdentifier[strings.ToLower(value)]
}

// FindBestMatch attempts to find a grounded fragment matching the given entity by trying
// multiple strategies in priority order:
//  1. Exact canonical path match
//  2. Identifier match (wikidataId, legalName, shortName)
//  3. Name/label match (case-insensitive)
//
// Returns nil if no match found.
func (c *ResolvedGroundingContext) FindBestMatch(canonicalPath, name string, identifiers map[string]string) *models.HorizonFragment {
	if c == nil || len(c.Fragments) == 0 {
		return nil
	}

	// 1. Exact canonical path
	if frag := c.FindByCanonicalPath(canonicalPath); frag != nil {
		return frag
	}

	// 2. Identifier match
	for key, val := range identifiers {
		if frag := c.FindByIdentifier(key, val); frag != nil {
			return frag
		}
	}

	// 3. Name match
	if frag := c.FindByName(name); frag != nil {
		return frag
	}

	return nil
}

// IsEmpty returns true if no fragments were resolved from grounding evidence.
func (c *ResolvedGroundingContext) IsEmpty() bool {
	return c == nil || len(c.Fragments) == 0
}

// Size returns the number of resolved fragments.
func (c *ResolvedGroundingContext) Size() int {
	if c == nil {
		return 0
	}
	return len(c.Fragments)
}

// CanonicalPaths returns all canonical paths of resolved fragments (for logging/debugging).
func (c *ResolvedGroundingContext) CanonicalPaths() []string {
	if c == nil {
		return nil
	}
	paths := make([]string, 0, len(c.Fragments))
	for _, f := range c.Fragments {
		if cp := strings.TrimSpace(f.CanonicalPath); cp != "" {
			paths = append(paths, cp)
		}
	}
	return paths
}

package policy

import (
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// SelectiveCanonizer applies a canonicalization fix without clobbering unrelated parts.
//
// Intended use:
//   - Multiple async fixers may target the same fragment; selective updates help avoid "last writer wins"
//     on canonical naming, by only mutating the semantic part(s) the fixer owns.
//
// NOTE: This does not replace the need for proper concurrency control for the full document write;
// it focuses specifically on making canonical naming fixes commutative where possible.
type SelectiveCanonizer interface {
	// Namespace returns the canonical namespace this canonizer owns (e.g., "org", "gov").
	Namespace() string
	// ApplyFromCanonicalPath applies a suggested canonical path into frag.CanonicalParts selectively.
	// It should NOT blindly overwrite all parts; only owned fields should be applied.
	//
	// Returns (changed, appliedFields, error).
	ApplyFromCanonicalPath(frag *horizonmodels.HorizonFragment, targetCanonicalPath string) (bool, []string, error)
}

// ensureParts best-effort ensures frag.CanonicalParts is present.
func ensureParts(frag *horizonmodels.HorizonFragment) {
	if frag == nil || frag.CanonicalParts != nil {
		return
	}
	if cp := strings.TrimSpace(frag.CanonicalPath); cp != "" {
		if parts, err := taxonomy.ParseCanonicalPathParts(cp); err == nil && parts != nil {
			frag.CanonicalParts = parts
		}
	}
}

type OrgSelectiveCanonizer struct{}

func (c *OrgSelectiveCanonizer) Namespace() string { return "org" }

func (c *OrgSelectiveCanonizer) ApplyFromCanonicalPath(frag *horizonmodels.HorizonFragment, targetCanonicalPath string) (bool, []string, error) {
	if frag == nil {
		return false, nil, fmt.Errorf("frag is nil")
	}
	targetCanonicalPath = strings.TrimSpace(targetCanonicalPath)
	if targetCanonicalPath == "" {
		return false, nil, fmt.Errorf("targetCanonicalPath is empty")
	}
	newParts, err := taxonomy.ParseCanonicalPathParts(targetCanonicalPath)
	if err != nil || newParts == nil || newParts.Org == nil {
		return false, nil, fmt.Errorf("failed to parse org parts from %q: %w", targetCanonicalPath, err)
	}

	ensureParts(frag)
	if frag.CanonicalParts == nil {
		frag.CanonicalParts = &taxonomy.CanonicalPathParts{Namespace: "org"}
	}
	frag.CanonicalParts.Namespace = "org"
	if frag.CanonicalParts.Org == nil {
		frag.CanonicalParts.Org = &taxonomy.OrgPathParts{}
	}

	applied := make([]string, 0, 3)
	changed := false

	// Owned fields for org:
	// - Geo (jurisdiction)
	// - LegalForm (ownership suffix)
	//
	// We intentionally do NOT overwrite BaseSlug unless it is currently empty, to avoid accidental identity drift.
	if geo := strings.TrimSpace(newParts.Org.Geo); geo != "" && geo != strings.TrimSpace(frag.CanonicalParts.Org.Geo) {
		frag.CanonicalParts.Org.Geo = geo
		changed = true
		applied = append(applied, "org.geo")
		// If the target is geo-scoped, legacy must be false.
		if geo != "" && frag.CanonicalParts.Org.Legacy {
			frag.CanonicalParts.Org.Legacy = false
			applied = append(applied, "org.legacy=false")
		}
	}
	if lf := strings.TrimSpace(newParts.Org.LegalForm); lf != "" && lf != strings.TrimSpace(frag.CanonicalParts.Org.LegalForm) {
		frag.CanonicalParts.Org.LegalForm = lf
		changed = true
		applied = append(applied, "org.legalForm")
	}
	if strings.TrimSpace(frag.CanonicalParts.Org.BaseSlug) == "" && strings.TrimSpace(newParts.Org.BaseSlug) != "" {
		frag.CanonicalParts.Org.BaseSlug = strings.TrimSpace(newParts.Org.BaseSlug)
		changed = true
		applied = append(applied, "org.baseSlug")
	}
	// Preserve legacy=true only when geo is absent.
	if strings.TrimSpace(frag.CanonicalParts.Org.Geo) == "" {
		frag.CanonicalParts.Org.Legacy = true
	}

	return changed, applied, nil
}

type GovSelectiveCanonizer struct{}

func (c *GovSelectiveCanonizer) Namespace() string { return "gov" }

func (c *GovSelectiveCanonizer) ApplyFromCanonicalPath(frag *horizonmodels.HorizonFragment, targetCanonicalPath string) (bool, []string, error) {
	if frag == nil {
		return false, nil, fmt.Errorf("frag is nil")
	}
	targetCanonicalPath = strings.TrimSpace(targetCanonicalPath)
	if targetCanonicalPath == "" {
		return false, nil, fmt.Errorf("targetCanonicalPath is empty")
	}
	newParts, err := taxonomy.ParseCanonicalPathParts(targetCanonicalPath)
	if err != nil || newParts == nil || newParts.Gov == nil {
		return false, nil, fmt.Errorf("failed to parse gov parts from %q: %w", targetCanonicalPath, err)
	}

	ensureParts(frag)
	if frag.CanonicalParts == nil {
		frag.CanonicalParts = &taxonomy.CanonicalPathParts{Namespace: "gov"}
	}
	frag.CanonicalParts.Namespace = "gov"
	if frag.CanonicalParts.Gov == nil {
		frag.CanonicalParts.Gov = &taxonomy.GovPathParts{}
	}

	applied := make([]string, 0, 2)
	changed := false

	// Owned fields for gov:
	// - Geo (jurisdiction)
	// - Slug (only fill if empty; avoid renaming agencies accidentally)
	if geo := strings.TrimSpace(newParts.Gov.Geo); geo != "" && geo != strings.TrimSpace(frag.CanonicalParts.Gov.Geo) {
		frag.CanonicalParts.Gov.Geo = geo
		changed = true
		applied = append(applied, "gov.geo")
	}
	if strings.TrimSpace(frag.CanonicalParts.Gov.Slug) == "" && strings.TrimSpace(newParts.Gov.Slug) != "" {
		frag.CanonicalParts.Gov.Slug = strings.TrimSpace(newParts.Gov.Slug)
		changed = true
		applied = append(applied, "gov.slug")
	}

	return changed, applied, nil
}

package pathbuilder

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Product canonical-path helpers for supply-chain path assembly.

// IsProductTransitionEdgeType reports whether the edge type is a
// product->product upstream/downstream step used in supply-chain
// continuity checks.
func IsProductTransitionEdgeType(edgeType DependencyType) bool {
	switch edgeType {
	case DependencyTypeTransformsInto, DependencyTypeAssemblesInto, DependencyTypeUsedIn:
		return true
	default:
		return false
	}
}

// ProductToProductEdgeTypeFromPaths selects the canonical product->product
// edge type. ASSEMBLES_INTO is reserved for nearest-step component /
// subassembly -> assembled product links; everything else stays
// TRANSFORMS_INTO.
func ProductToProductEdgeTypeFromPaths(sourcePath, targetPath string) DependencyType {
	if shouldPreferAssemblesIntoByProductPaths(sourcePath, targetPath) {
		return DependencyTypeAssemblesInto
	}
	return DependencyTypeTransformsInto
}

func shouldPreferAssemblesIntoByProductPaths(sourcePath, targetPath string) bool {
	src, srcOK := parseProductCanonicalParts(sourcePath)
	tgt, tgtOK := parseProductCanonicalParts(targetPath)
	if !srcOK || !tgtOK {
		return false
	}
	srcCategory := strings.ToLower(strings.TrimSpace(src.Category))
	tgtCategory := strings.ToLower(strings.TrimSpace(tgt.Category))
	srcStage := strings.ToLower(strings.TrimSpace(src.Stage))
	tgtStage := strings.ToLower(strings.TrimSpace(tgt.Stage))

	targetLooksAssembled := tgtCategory == "finished" || tgtStage == "assembly"
	sourceLooksSubassembly := srcCategory == "component" || srcStage == "assembly" || srcStage == "manufacturing" || srcCategory == "finished"
	if !targetLooksAssembled || !sourceLooksSubassembly {
		return false
	}
	if srcCategory == "raw" {
		return false
	}
	return true
}

func parseProductCanonicalParts(path string) (*taxonomy.ProductPathParts, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return nil, false
	}
	parts, err := taxonomy.ParseCanonicalPathParts(path)
	if err != nil || parts == nil || parts.Product == nil {
		return nil, false
	}
	return parts.Product, true
}

// ProductStageRankFromCanonicalPath returns the stage-order rank for a
// product canonical path. Used by supply-chain assembly to compare upstream
// vs downstream products.
func ProductStageRankFromCanonicalPath(path string) (int, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return 0, false
	}
	parts, err := taxonomy.ParseCanonicalPathParts(path)
	if err != nil || parts == nil || parts.Product == nil {
		return 0, false
	}
	stage := strings.ToLower(strings.TrimSpace(parts.Product.Stage))
	if stage == "" {
		return 0, false
	}
	order := taxonomy.GetProductStageOrder()
	for i, s := range order {
		if strings.ToLower(strings.TrimSpace(s)) == stage {
			return i, true
		}
	}
	return 0, false
}

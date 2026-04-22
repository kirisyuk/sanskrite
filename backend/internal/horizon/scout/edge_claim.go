package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

// EdgeClaim is the unified internal contract for "an edge we want to materialize".
//
// Today it is a type alias over agents.DiscoveredRelation because that struct already contains:
// - target fields (name/type/path)
// - relationType + confidence + reason
// - qualifiers/metrics/validFrom/validTo
// - (now) optional explicit source fields (sourceCanonicalPath/sourceType/sourceName)
//
// This allows us to converge Phase 1 (anchored) and strategic projection (explicit endpoints)
// without a breaking rewrite of the canonicalizer/materializer in one step.
type EdgeClaim = agents.DiscoveredRelation

func claimFromStrategicEdge(src string, srcType string, tgt string, tgtType string, relType string, reason string, confidence float64, qualifiers map[string]interface{}) EdgeClaim {
	src = strings.TrimSpace(src)
	tgt = strings.TrimSpace(tgt)
	relType = normalizeRelationToken(relType)

	c := EdgeClaim{
		SourceCanonicalPath: src,
		SourceType:          strings.TrimSpace(srcType),
		SourceName:          bestEffortDisplayLabel("", src),
		TargetName:          bestEffortDisplayLabel("", tgt),
		TargetType:          strings.TrimSpace(tgtType),
		RelationType:        relType,
		Reason:              strings.TrimSpace(reason),
		Confidence:          confidence,
		TargetCanonicalPath: tgt,
		Qualifiers:          qualifiers,
	}
	return c
}

package pathquery

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func filterArtifactsByAnchors(artifacts []*horizonmodels.MaterializedArtifact, anchors []string) []*horizonmodels.MaterializedArtifact {
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts))
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if _, ok := seen[artifact.ID]; ok {
			continue
		}
		if !artifactContainsAllAnchors(artifact, anchors) {
			continue
		}
		seen[artifact.ID] = struct{}{}
		out = append(out, artifact)
	}
	return out
}

func artifactContainsAllAnchors(artifact *horizonmodels.MaterializedArtifact, anchors []string) bool {
	if artifact == nil {
		return false
	}
	horizonmodels.PopulateSupplyChainPathIndexes(artifact)
	have := make(map[string]struct{}, len(artifact.NodeRefs))
	for _, ref := range artifact.NodeRefs {
		have[strings.TrimSpace(ref)] = struct{}{}
	}
	parsed, _ := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	for _, anchor := range anchors {
		anchor = strings.TrimSpace(anchor)
		if anchor == "" {
			continue
		}
		if isSupplyLinkAnchor(anchor) {
			if !payloadContainsSupplyLinkAnchor(parsed, anchor) {
				return false
			}
			continue
		}
		if _, ok := have[anchor]; ok {
			continue
		}
		if !payloadContainsObjectAnchor(parsed, anchor) {
			return false
		}
	}
	return true
}

func isSupplyLinkAnchor(anchor string) bool {
	return strings.HasPrefix(strings.TrimSpace(anchor), "/supply-link/")
}

func payloadContainsSupplyLinkAnchor(parsed *horizonmodels.SupplyChainPathPayload, anchor string) bool {
	if parsed == nil {
		return false
	}
	for _, leg := range parsed.Legs {
		if strings.TrimSpace(leg.SupplyLinkCanonicalPath) == anchor {
			return true
		}
	}
	return false
}

func payloadContainsObjectAnchor(parsed *horizonmodels.SupplyChainPathPayload, anchor string) bool {
	if parsed == nil {
		return false
	}
	for _, leg := range parsed.Legs {
		if legMatchesAnchor(leg, anchor) {
			return true
		}
	}
	return false
}

func legMatchesAnchor(leg horizonmodels.SupplyChainPathLeg, anchor string) bool {
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return false
	}
	return leg.SupplierRef == anchor || leg.CustomerRef == anchor || leg.ProductRef == anchor || leg.SupplyLinkCanonicalPath == anchor
}

func hasSupplyLinkAnchor(anchors []string) bool {
	for _, anchor := range anchors {
		if isSupplyLinkAnchor(anchor) {
			return true
		}
	}
	return false
}

func mergeUniqueStrings(dst []string, values []string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, s := range dst {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		seen[s] = struct{}{}
	}
	out := append([]string(nil), dst...)
	for _, s := range values {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func unionNodeRefsFromArtifacts(artifacts []*horizonmodels.MaterializedArtifact) []string {
	var out []string
	for _, a := range artifacts {
		if a == nil {
			continue
		}
		out = mergeUniqueStrings(out, a.NodeRefs)
		switch a.Kind {
		case horizonmodels.MaterializedKindSupplyChainPath, horizonmodels.MaterializedKindServiceChainPath:
			parsed, err := horizonmodels.ParseSupplyChainPathPayload(a.Payload)
			if err != nil || parsed == nil {
				continue
			}
			for _, node := range parsed.Nodes {
				out = mergeUniqueStrings(out, []string{node.Ref})
			}
			for _, leg := range parsed.Legs {
				out = mergeUniqueStrings(out, []string{
					leg.SupplierRef,
					leg.CustomerRef,
					leg.ProductRef,
					leg.SupplyLinkCanonicalPath,
				})
			}
		}
	}
	return out
}

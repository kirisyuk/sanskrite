package intra_graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// PairType defines allowed entity pair types for intra-graph inference.
type PairType string

const (
	PairTypeOrgOrg     PairType = "org_org"
	PairTypeOrgGov     PairType = "org_gov"
	PairTypeGovGov     PairType = "gov_gov"
	PairTypePersonOrg  PairType = "person_org"
	PairTypePersonGov  PairType = "person_gov"
	PairTypeOrgMarket  PairType = "org_market"
	PairTypeOrgProduct PairType = "org_product"
)

// AllowedRelationTypes returns canonical relation types for each pair type.
func (p PairType) AllowedRelationTypes() []string {
	switch p {
	case PairTypeOrgOrg, PairTypeOrgGov, PairTypeGovGov:
		return []string{"OWNS", "SUBSIDIARY_OF", "CONTROLS", "REGULATES", "ADMINISTERS", "SUPPLIES_TO", "DEPENDS_ON"}
	case PairTypePersonOrg, PairTypePersonGov:
		return []string{"EMPLOYED_BY", "BOARD_MEMBER_OF", "OWNS", "FOUNDED", "MANAGES"}
	case PairTypeOrgMarket:
		return []string{"PARTICIPATES_IN"}
	case PairTypeOrgProduct:
		return []string{"PRODUCES", "CONSUMES", "SUPPLIES_TO"}
	default:
		return nil
	}
}

// Inferrer runs intra-graph link inference: finds direct relations between
// entities in the same lens scope that were not linked during scouting.
type Inferrer struct {
	executor      *ai.AITaskExecutor
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
}

// NewInferrer creates a new intra-graph inferrer.
func NewInferrer(
	executor *ai.AITaskExecutor,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
) *Inferrer {
	return &Inferrer{
		executor:      executor,
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

// InferInput holds input for intra-graph inference.
type InferInput struct {
	TenantID  string
	ProjectID string
	LensID    string
	Lens      *models.Lens
}

// InferResult holds inferred relations (direct only; transitive filtered out).
type InferResult struct {
	Relations []agents.DiscoveredRelation
	PairStats map[string]int // pairType -> count
}

// Infer runs inference for all applicable pair types and returns direct relations.
func (inf *Inferrer) Infer(ctx context.Context, in InferInput) (*InferResult, error) {
	if inf == nil || inf.executor == nil || inf.fragmentsRepo == nil || inf.edgesRepo == nil {
		return nil, nil
	}
	if in.Lens == nil {
		return nil, nil
	}

	paths := collectLensScopePaths(in.Lens)
	if len(paths) < 2 {
		return &InferResult{Relations: nil, PairStats: map[string]int{}}, nil
	}

	fragments, err := inf.fragmentsRepo.ListByCanonicalPaths(ctx, paths)
	if err != nil || len(fragments) == 0 {
		return &InferResult{Relations: nil, PairStats: map[string]int{}}, nil
	}

	fragByPath := make(map[string]*models.HorizonFragment)
	for _, f := range fragments {
		cp := strings.TrimSpace(f.CanonicalPath)
		if cp != "" {
			fragByPath[cp] = f
		}
	}

	// Build existing edges (sourcePath -> targetPath set) for transitive filter
	existingEdges := inf.buildExistingEdges(ctx, fragments)

	var allRels []agents.DiscoveredRelation
	pairStats := make(map[string]int)

	pairTypes := []struct {
		pt         PairType
		srcFilter  func(*models.HorizonFragment) bool
		tgtFilter  func(*models.HorizonFragment) bool
	}{
		{PairTypeOrgOrg, fragOrgPath, fragOrgPath},
		{PairTypeOrgGov, fragOrgPath, fragGovPath},
		{PairTypeGovGov, fragGovPath, fragGovPath},
		{PairTypePersonOrg, fragPersonPath, fragOrgPath},
		{PairTypePersonGov, fragPersonPath, fragGovPath},
		{PairTypeOrgMarket, fragOrgPath, fragMarketPath},
		{PairTypeOrgProduct, fragOrgPath, fragProductPath},
	}

	for _, pt := range pairTypes {
		srcFrags := filterFragments(fragments, pt.srcFilter)
		tgtFrags := filterFragments(fragments, pt.tgtFilter)

		if len(srcFrags) == 0 || len(tgtFrags) == 0 {
			continue
		}

		rels, err := inf.inferForPairType(ctx, pt.pt, srcFrags, tgtFrags, existingEdges, fragByPath)
		if err != nil {
			fmt.Printf("[IntraGraph] pair %s inference error: %v\n", pt.pt, err)
			continue
		}
		filtered := filterTransitive(rels, existingEdges)
		allRels = append(allRels, filtered...)
		pairStats[string(pt.pt)] = len(filtered)
	}

	return &InferResult{Relations: allRels, PairStats: pairStats}, nil
}

func collectLensScopePaths(lens *models.Lens) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range lens.Scope {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "/query/") || strings.HasPrefix(s, "/domain/") {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(s); err != nil {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if lens.ResolvedQueries != nil {
		for _, paths := range lens.ResolvedQueries {
			for _, p := range paths {
				p = strings.TrimSpace(p)
				if p == "" || strings.HasPrefix(p, "/query/") || strings.HasPrefix(p, "/domain/") {
					continue
				}
				if err := taxonomy.ValidateCanonicalPath(p); err != nil {
					continue
				}
				if _, ok := seen[p]; ok {
					continue
				}
				seen[p] = struct{}{}
				out = append(out, p)
			}
		}
	}
	return out
}

func fragOrgPath(f *models.HorizonFragment) bool {
	return f != nil && strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/org/")
}
func fragGovPath(f *models.HorizonFragment) bool {
	return f != nil && strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/gov/")
}
func fragPersonPath(f *models.HorizonFragment) bool {
	return f != nil && f.Type == models.FragmentTypePerson && strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/person/")
}
func fragMarketPath(f *models.HorizonFragment) bool {
	return f != nil && strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/market/")
}
func fragProductPath(f *models.HorizonFragment) bool {
	return f != nil && strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/product/")
}

func filterFragments(frags []*models.HorizonFragment, pred func(*models.HorizonFragment) bool) []*models.HorizonFragment {
	var out []*models.HorizonFragment
	for _, f := range frags {
		if f != nil && pred(f) {
			out = append(out, f)
		}
	}
	return out
}

func (inf *Inferrer) buildExistingEdges(ctx context.Context, fragments []*models.HorizonFragment) map[string]map[string]struct{} {
	// sourcePath -> set of targetPath (direct edges)
	out := make(map[string]map[string]struct{})
	tenantID := "global"
	for _, f := range fragments {
		if f == nil || f.ID == "" {
			continue
		}
		srcPath := strings.TrimSpace(f.CanonicalPath)
		if srcPath == "" {
			continue
		}
		edges, err := inf.edgesRepo.ListBySource(ctx, tenantID, f.ID, nil, 500)
		if err != nil {
			continue
		}
		for _, e := range edges {
			if e == nil {
				continue
			}
			tgtID := strings.TrimSpace(e.TargetFragmentID)
			if tgtID == "" {
				continue
			}
			tgtFrag, err := inf.fragmentsRepo.Get(ctx, tgtID)
			if err != nil || tgtFrag == nil {
				continue
			}
			tgtPath := strings.TrimSpace(tgtFrag.CanonicalPath)
			if tgtPath == "" {
				continue
			}
			if out[srcPath] == nil {
				out[srcPath] = make(map[string]struct{})
			}
			out[srcPath][tgtPath] = struct{}{}
		}
	}
	return out
}

// filterTransitive drops relations A->B if a path A->...->B already exists (length > 1).
func filterTransitive(rels []agents.DiscoveredRelation, existingEdges map[string]map[string]struct{}) []agents.DiscoveredRelation {
	// Build adjacency for BFS: for each (src,tgt) we can check if path exists
	// Simplified: if A has edge to C and C has edge to B, then A->B is transitive
	// We use existingEdges: if A has any path to B through intermediates, skip A->B
	// For simplicity: if A has direct edge to B in existingEdges, skip (duplicate)
	// If A has path A->X->B (2 hops), skip A->B
	// General: if there exists any path A -> ... -> B with length >= 1 in existingEdges, and we're adding A->B, skip
	// Actually: existingEdges only has DIRECT edges. So if A->B is in existingEdges, it's a duplicate.
	// For transitive: we need to compute reachability. A->B is transitive if we can reach B from A in existing graph (path len >= 2).
	// Simpler heuristic: if A and B share a common "mediator" C such that A->C and C->B exist, then A->B is transitive.
	reachable := computeReachable(existingEdges)
	var out []agents.DiscoveredRelation
	for _, r := range rels {
		src := strings.TrimSpace(r.SourceCanonicalPath)
		tgt := strings.TrimSpace(r.TargetCanonicalPath)
		if src == "" {
			src = strings.TrimSpace(r.SourceCanonicalPath) // might be in different field for intra-graph
		}
		// For intra-graph, we always have explicit source and target
		src = strings.TrimSpace(r.SourceCanonicalPath)
		if src == "" {
			continue
		}
		if tgt == "" {
			tgt = strings.TrimSpace(r.TargetCanonicalPath)
		}
		if tgt == "" || src == tgt {
			continue
		}
		// Skip if already exists (direct duplicate)
		if existingEdges[src] != nil {
			if _, ok := existingEdges[src][tgt]; ok {
				continue
			}
		}
		// Skip if transitive: path src -> ... -> tgt exists with length >= 2
		if reachable[src] != nil {
			if _, ok := reachable[src][tgt]; ok {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func computeReachable(direct map[string]map[string]struct{}) map[string]map[string]struct{} {
	// reachable[src][tgt] = true if there exists path src -> ... -> tgt with >= 2 edges
	// I.e. tgt is reachable from src via at least one intermediate
	out := make(map[string]map[string]struct{})
	for src, tgts := range direct {
		for tgt := range tgts {
			// For each src->tgt, find tgt's successors
			if nexts, ok := direct[tgt]; ok {
				for tgt2 := range nexts {
					if tgt2 == src {
						continue
					}
					if out[src] == nil {
						out[src] = make(map[string]struct{})
					}
					out[src][tgt2] = struct{}{}
				}
			}
		}
	}
	return out
}

type intraGraphResult struct {
	Relations []agents.DiscoveredRelation `json:"relations"`
}

func (inf *Inferrer) inferForPairType(
	ctx context.Context,
	pt PairType,
	srcFrags, tgtFrags []*models.HorizonFragment,
	existingEdges map[string]map[string]struct{},
	fragByPath map[string]*models.HorizonFragment,
) ([]agents.DiscoveredRelation, error) {
	allowed := pt.AllowedRelationTypes()
	if len(allowed) == 0 {
		return nil, nil
	}

	srcPaths := make([]string, 0, len(srcFrags))
	for _, f := range srcFrags {
		p := strings.TrimSpace(f.CanonicalPath)
		if p != "" {
			srcPaths = append(srcPaths, p)
		}
	}
	tgtPaths := make([]string, 0, len(tgtFrags))
	for _, f := range tgtFrags {
		p := strings.TrimSpace(f.CanonicalPath)
		if p != "" {
			tgtPaths = append(tgtPaths, p)
		}
	}
	if len(srcPaths) == 0 || len(tgtPaths) == 0 {
		return nil, nil
	}

	// Build existing edges summary for prompt
	existingLines := []string{}
	for src, tgts := range existingEdges {
		for tgt := range tgts {
			existingLines = append(existingLines, fmt.Sprintf("  %s -> %s", src, tgt))
		}
	}
	existingBlock := "None"
	if len(existingLines) > 0 {
		if len(existingLines) > 50 {
			existingLines = existingLines[:50]
		}
		existingBlock = strings.Join(existingLines, "\n")
	}

	prompt := fmt.Sprintf(`You are an intra-graph link inference agent. Given a set of entities discovered in the same research job, infer ONLY DIRECT relations between them.

CRITICAL: Emit ONLY DIRECT relations. If A relates to B only through an intermediary C (e.g. Mubadala owns G42, G42 owns Core42 — do NOT add Mubadala->Core42), do NOT emit A->B.
Use the EXISTING EDGES below to avoid transitive links. If a path A -> ... -> B already exists, do NOT add A->B.

PAIR TYPE: %s
ALLOWED RELATION TYPES (use ONLY these): %s

SOURCE ENTITIES (canonical paths):
%s

TARGET ENTITIES (canonical paths):
%s

EXISTING DIRECT EDGES (do not duplicate; do not add transitive):
%s

For each DIRECT relation you infer, output:
- sourceCanonicalPath, sourceType, sourceName (from the source list)
- targetCanonicalPath, targetType, targetName (from the target list)
- relationType (must be one of the allowed)
- reason (brief evidence)
- confidence (0.6-0.9; only high-confidence direct links)

Return JSON:
{"relations": [{"sourceCanonicalPath":"/org/...","sourceType":"ORGANIZATION","sourceName":"...","targetCanonicalPath":"/org/...","targetType":"ORGANIZATION","targetName":"...","relationType":"OWNS","reason":"...","confidence":0.8}]}

Return ONLY valid JSON. No markdown.`,
		pt,
		strings.Join(allowed, ", "),
		strings.Join(srcPaths, "\n"),
		strings.Join(tgtPaths, "\n"),
		existingBlock,
	)

	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:      true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:               8000,
			MaxOutputSize:                  4000,
		},
		Verifier: verifier,
	}
	parsed, err := inf.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	jsonData, err := json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	var res intraGraphResult
	if err := json.Unmarshal(jsonData, &res); err != nil {
		return nil, err
	}

	// Validate and filter
	var out []agents.DiscoveredRelation
	allowedSet := make(map[string]struct{})
	for _, a := range allowed {
		allowedSet[strings.TrimSpace(a)] = struct{}{}
	}
	for _, r := range res.Relations {
		rt := strings.TrimSpace(taxonomy.NormalizeRelationType(r.RelationType))
		if _, ok := allowedSet[rt]; !ok {
			continue
		}
		src := strings.TrimSpace(r.SourceCanonicalPath)
		tgt := strings.TrimSpace(r.TargetCanonicalPath)
		if src == "" || tgt == "" || src == tgt {
			continue
		}
		if fragByPath[src] == nil || fragByPath[tgt] == nil {
			continue
		}
		if r.Confidence <= 0 || r.Confidence < 0.55 {
			r.Confidence = 0.65
		}
		if r.Confidence > 0.95 {
			r.Confidence = 0.9
		}
		out = append(out, r)
	}
	return out, nil
}

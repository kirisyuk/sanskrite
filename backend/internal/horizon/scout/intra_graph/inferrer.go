package intra_graph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
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
	PairTypeGovOrg     PairType = "gov_org" // gov as source, org as target (for ADMINISTERS, REGULATES)
	PairTypeGovGov     PairType = "gov_gov"
	PairTypePersonOrg  PairType = "person_org"
	PairTypePersonGov  PairType = "person_gov"
	PairTypeOrgMarket  PairType = "org_market"
	PairTypeOrgProduct PairType = "org_product"
)

// fragmentTypes maps pair type to (source, target) FragmentType using the same
// ontology as EdgeDirectionPolicies. /gov/ and /org/ entities share FragmentTypeOrganization.
func (p PairType) fragmentTypes() (FragmentType, FragmentType) {
	switch p {
	case PairTypeOrgOrg, PairTypeOrgGov, PairTypeGovOrg, PairTypeGovGov:
		return FragmentTypeOrganization, FragmentTypeOrganization
	case PairTypePersonOrg, PairTypePersonGov:
		return FragmentTypePerson, FragmentTypeOrganization
	case PairTypeOrgMarket:
		return FragmentTypeOrganization, FragmentTypeMarket
	case PairTypeOrgProduct:
		return FragmentTypeOrganization, FragmentTypeProduct
	default:
		return "", ""
	}
}

// AllowedRelationTypes derives allowed relation types from the unified
// EdgeDirectionPolicies source of truth. An edge type is allowed if
// (srcType, tgtType) or (tgtType, srcType) matches its direction policy
// (canonicalization fixes reversed directions downstream).
func (p PairType) AllowedRelationTypes() []string {
	srcType, tgtType := p.fragmentTypes()
	if srcType == "" || tgtType == "" {
		return nil
	}

	seen := make(map[string]struct{})
	for edgeType, rule := range edgesemantics.EdgeDirectionPolicies {
		fwd := containsFT(rule.ValidSourceClasses, srcType) && containsFT(rule.ValidTargetClasses, tgtType)
		rev := containsFT(rule.ValidSourceClasses, tgtType) && containsFT(rule.ValidTargetClasses, srcType)
		if fwd || rev {
			seen[string(edgeType)] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func containsFT(types []FragmentType, t FragmentType) bool {
	for _, vt := range types {
		if vt == t {
			return true
		}
	}
	return false
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

	existing := inf.buildExistingEdges(ctx, fragments)

	var allRels []agents.DiscoveredRelation
	pairStats := make(map[string]int)

	pairTypes := []struct {
		pt        PairType
		srcFilter func(*models.HorizonFragment) bool
		tgtFilter func(*models.HorizonFragment) bool
	}{
		{PairTypeOrgOrg, fragOrgPath, fragOrgPath},
		{PairTypeOrgGov, fragOrgPath, fragGovPath},
		{PairTypeGovOrg, fragGovPath, fragOrgPath},
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

		rels, err := inf.inferForPairType(ctx, pt.pt, srcFrags, tgtFrags, existing, fragByPath)
		if err != nil {
			fmt.Printf("[IntraGraph] pair %s inference error: %v\n", pt.pt, err)
			continue
		}
		filtered := filterDuplicatesAndTransitive(rels, existing)
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
	return f != nil && f.Class == FragmentTypePerson && strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/person/")
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

// typedEdgeSet stores existing edges with their relation types, enabling
// type-aware and bidirectional dedup.
type typedEdgeSet struct {
	// edges: srcPath → tgtPath → set of canonicalised edge types
	edges map[string]map[string]map[string]struct{}
}

func newTypedEdgeSet() *typedEdgeSet {
	return &typedEdgeSet{edges: make(map[string]map[string]map[string]struct{})}
}

func (s *typedEdgeSet) add(src, tgt, relType string) {
	if s.edges[src] == nil {
		s.edges[src] = make(map[string]map[string]struct{})
	}
	if s.edges[src][tgt] == nil {
		s.edges[src][tgt] = make(map[string]struct{})
	}
	s.edges[src][tgt][relType] = struct{}{}
}

// hasEdge checks whether a canonical relation already exists in either
// direction. Handles SUBSIDIARY_OF→OWNS alias and symmetric types.
func (s *typedEdgeSet) hasEdge(src, tgt, relType string) bool {
	canonical := canonicalType(relType)
	// Forward: src → tgt with same canonical type.
	if types, ok := s.edges[src][tgt]; ok {
		for t := range types {
			if canonicalType(t) == canonical {
				return true
			}
		}
	}
	// Reverse: tgt → src with same canonical type (covers reversed direction).
	if types, ok := s.edges[tgt][src]; ok {
		for t := range types {
			if canonicalType(t) == canonical {
				return true
			}
		}
	}
	return false
}

// adjacencyPaths returns the set of direct neighbours (ignoring types) for
// transitive‐path detection.
func (s *typedEdgeSet) adjacencyPaths() map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for src, tgts := range s.edges {
		for tgt := range tgts {
			if out[src] == nil {
				out[src] = make(map[string]struct{})
			}
			out[src][tgt] = struct{}{}
		}
	}
	return out
}

// promptLines returns human-readable lines "src -> tgt [TYPE1, TYPE2]" for LLM.
func (s *typedEdgeSet) promptLines(limit int) []string {
	var lines []string
	for src, tgts := range s.edges {
		for tgt, types := range tgts {
			ts := make([]string, 0, len(types))
			for t := range types {
				ts = append(ts, t)
			}
			sort.Strings(ts)
			lines = append(lines, fmt.Sprintf("  %s -> %s [%s]", src, tgt, strings.Join(ts, ", ")))
		}
	}
	sort.Strings(lines)
	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}
	return lines
}

func canonicalType(t string) string {
	t = strings.TrimSpace(strings.ToUpper(t))
	switch t {
	case "SUBSIDIARY_OF":
		return "OWNS"
	case "AUDITED_BY":
		return "ASSURES"
	case "HAS_SUBMARKET":
		return "HAS_MARKET_SEGMENT"
	}
	return t
}

func (inf *Inferrer) buildExistingEdges(ctx context.Context, fragments []*models.HorizonFragment) *typedEdgeSet {
	out := newTypedEdgeSet()
	tenantID := "global"

	// Phase 1: collect all edges via ListBySource.
	type edgeWithSrc struct {
		srcPath string
		edge    *models.DependencyEdge
	}
	var allEdges []edgeWithSrc
	var allTargetIDs []string
	targetIDSet := make(map[string]struct{})

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
			allEdges = append(allEdges, edgeWithSrc{srcPath: srcPath, edge: e})
			if _, ok := targetIDSet[tgtID]; !ok {
				targetIDSet[tgtID] = struct{}{}
				allTargetIDs = append(allTargetIDs, tgtID)
			}
		}
	}

	// Phase 2: batch-fetch all target fragments in one call.
	targetFrags, err := inf.fragmentsRepo.GetMany(ctx, allTargetIDs)
	if err != nil {
		return out
	}

	// Phase 3: build edge set from batch results.
	for _, ews := range allEdges {
		tgtID := strings.TrimSpace(ews.edge.TargetFragmentID)
		tgtFrag := targetFrags[tgtID]
		if tgtFrag == nil {
			continue
		}
		tgtPath := strings.TrimSpace(tgtFrag.CanonicalPath)
		if tgtPath == "" {
			continue
		}
		out.add(ews.srcPath, tgtPath, string(ews.edge.Relation))
	}
	return out
}

// filterDuplicatesAndTransitive removes relations that already exist (type-aware,
// bidirectional check) or are transitively implied (2-hop reachability).
func filterDuplicatesAndTransitive(rels []agents.DiscoveredRelation, existing *typedEdgeSet) []agents.DiscoveredRelation {
	adjacency := existing.adjacencyPaths()
	reachable := computeReachable(adjacency)
	var out []agents.DiscoveredRelation
	for _, r := range rels {
		src := strings.TrimSpace(r.SourceCanonicalPath)
		tgt := strings.TrimSpace(r.TargetCanonicalPath)
		rt := strings.TrimSpace(r.RelationType)
		if src == "" || tgt == "" || src == tgt {
			continue
		}
		// Type-aware bidirectional duplicate check.
		if existing.hasEdge(src, tgt, rt) {
			continue
		}
		// Transitive check (type-agnostic): if a 2-hop path src→…→tgt exists, skip.
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
	out := make(map[string]map[string]struct{})
	for src, tgts := range direct {
		for tgt := range tgts {
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
	existing *typedEdgeSet,
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

	existingLines := existing.promptLines(60)
	existingBlock := "None"
	if len(existingLines) > 0 {
		existingBlock = strings.Join(existingLines, "\n")
	}

	sphereBlock := buildSphereContextBlock(srcFrags, tgtFrags)

	directionRules := "\n" + edgesemantics.EdgeDirectionValidationRulesForPrompt()
	if pt == PairTypeOrgGov || pt == PairTypeOrgOrg || pt == PairTypeGovOrg || pt == PairTypeGovGov {
		directionRules += `
GOV/ORG PATH-SPECIFIC DIRECTION RULES:
- OWNS: gov owns state-owned enterprises. Direction MUST be /gov/ -> /org/. NEVER /org/ OWNS /gov/ (canonicalizer will swap).
- ADMINISTERS: source is the superior authority (typically /gov/...), target is subordinate. NEVER /org/ administers /gov/.
- REGULATES: source is the regulator, target is regulated. Regulators are typically /gov/ but private standards bodies (/org/) can regulate too.
- gov_org pair: natural for OWNS, ADMINISTERS, REGULATES, CONTROLS (gov owns/administers/regulates/controls org).
- org_gov pair: for OWNS/ADMINISTERS/REGULATES swap direction — source=TARGET (gov), target=SOURCE (org).
`
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

EXISTING DIRECT EDGES with relation types (do NOT duplicate these; do NOT add transitive paths):
%s
%s

For each DIRECT relation you infer, output:
- sourceCanonicalPath, sourceType, sourceName (from the source/target lists per direction rules)
- targetCanonicalPath, targetType, targetName (from the source/target lists per direction rules)
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
		directionRules+sphereBlock,
	)

	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:                8000,
			MaxOutputSize:                   4000,
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

// buildSphereContextBlock adds sphere-aware hints to the intra-graph inference prompt.
// Shared spheres -> sphere-specific relations (OWNS, PARTICIPATES_IN) more likely.
// Different spheres -> cross-sphere relations (INFLUENCES, DEPENDS_ON) more likely.
func buildSphereContextBlock(srcFrags, tgtFrags []*models.HorizonFragment) string {
	srcSpheres := collectSpherePaths(srcFrags)
	tgtSpheres := collectSpherePaths(tgtFrags)
	if len(srcSpheres) == 0 && len(tgtSpheres) == 0 {
		return ""
	}
	var shared []string
	sharedSet := make(map[string]struct{})
	for _, s := range srcSpheres {
		if _, ok := sharedSet[s]; ok {
			continue
		}
		for _, t := range tgtSpheres {
			if s == t {
				sharedSet[s] = struct{}{}
				shared = append(shared, s)
				break
			}
		}
	}
	sort.Strings(shared)
	var sb strings.Builder
	sb.WriteString("\n\nSPHERE CONTEXT:\n")
	if len(shared) > 0 {
		sb.WriteString(fmt.Sprintf("- Shared spheres: %s. Sphere-specific relations (OWNS, PARTICIPATES_IN, ADMINISTERS) are more likely.\n", strings.Join(shared, ", ")))
	}
	if len(srcSpheres) > 0 || len(tgtSpheres) > 0 {
		allSrc := strings.Join(srcSpheres, ", ")
		allTgt := strings.Join(tgtSpheres, ", ")
		if allSrc != "" && allTgt != "" && len(shared) == 0 {
			sb.WriteString(fmt.Sprintf("- Source spheres: %s. Target spheres: %s. Cross-sphere relations (INFLUENCES, DEPENDS_ON, REGULATES) are more likely.\n", allSrc, allTgt))
		} else if allSrc != "" || allTgt != "" {
			sb.WriteString(fmt.Sprintf("- Source spheres: %s. Target spheres: %s.\n", allSrc, allTgt))
		}
	}
	return sb.String()
}

func collectSpherePaths(frags []*models.HorizonFragment) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, f := range frags {
		if f == nil {
			continue
		}
		for _, s := range f.SpherePaths {
			s = strings.TrimSpace(s)
			if s != "" && s != "/sphere/other" {
				if _, ok := seen[s]; !ok {
					seen[s] = struct{}{}
					out = append(out, s)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

package horizon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// QueryControlCorridor builds an on-the-fly transitive control corridor:
// authority -> decision -> mechanism -> impact.
//
// POST /api/horizon/lenses/{lensId}/control-corridor
func (h *HorizonHandlers) QueryControlCorridor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	lensID := strings.TrimSpace(extractIDFromPath(r, "/api/horizon/lenses/"))
	if lensID == "" {
		http.Error(w, "lensId is required in path", http.StatusBadRequest)
		return
	}
	if h.service == nil || h.service.Lens() == nil || h.service.Fragments() == nil || h.service.Edges() == nil {
		http.Error(w, "Horizon service not available", http.StatusServiceUnavailable)
		return
	}

	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) == "" || lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var req controlCorridorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	opts := controlCorridorOptions{
		MaxAuthorityHops: clampInt(req.MaxAuthorityHops, 0, 4, 2),
		MaxImpactHops:    clampInt(req.MaxImpactHops, 1, 6, 3),
		PerNodeEdgeLimit: clampInt(req.PerNodeEdgeLimit, 40, 800, 180),
		MaxNodes:         clampInt(req.MaxNodes, 40, 1200, 260),
		MaxEdges:         clampInt(req.MaxEdges, 80, 2400, 520),
		MaxSeeds:         clampInt(req.MaxSeeds, 1, 64, 16),
	}

	seedIDs, seedPaths, seedTruncated, warnings, err := h.resolveControlCorridorSeeds(r.Context(), lens, req, opts.MaxSeeds)
	if err != nil {
		http.Error(w, "Failed to resolve seeds", http.StatusInternalServerError)
		return
	}
	if len(seedIDs) == 0 {
		http.Error(w, "No valid seeds found (provide seedCanonicalPaths/seedFragmentIds or ensure lens.scope is resolvable)", http.StatusBadRequest)
		return
	}

	res, err := h.buildControlCorridor(r.Context(), lensID, seedIDs, seedPaths, opts)
	if err != nil {
		http.Error(w, "Failed to build control corridor", http.StatusInternalServerError)
		return
	}
	res.Summary.SeedTruncated = seedTruncated
	res.Summary.Truncated = res.Summary.Truncated || seedTruncated
	if len(warnings) > 0 {
		res.Warnings = append(res.Warnings, warnings...)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

type controlCorridorRequest struct {
	SeedCanonicalPaths []string `json:"seedCanonicalPaths,omitempty"`
	SeedFragmentIDs    []string `json:"seedFragmentIds,omitempty"`
	MaxAuthorityHops   int      `json:"maxAuthorityHops,omitempty"`
	MaxImpactHops      int      `json:"maxImpactHops,omitempty"`
	PerNodeEdgeLimit   int      `json:"perNodeEdgeLimit,omitempty"`
	MaxNodes           int      `json:"maxNodes,omitempty"`
	MaxEdges           int      `json:"maxEdges,omitempty"`
	MaxSeeds           int      `json:"maxSeeds,omitempty"`
}

type controlCorridorOptions struct {
	MaxAuthorityHops int
	MaxImpactHops    int
	PerNodeEdgeLimit int
	MaxNodes         int
	MaxEdges         int
	MaxSeeds         int
}

type controlCorridorNode struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Type          string `json:"type,omitempty"`
	CanonicalPath string `json:"canonicalPath,omitempty"`
	Lane          string `json:"lane"`
	Depth         int    `json:"depth"`
	Seed          bool   `json:"seed,omitempty"`
}

type controlCorridorEdge struct {
	ID          string  `json:"id"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	Type        string  `json:"type"`
	Stage       string  `json:"stage"`
	Role        string  `json:"role,omitempty"`
	Criticality float64 `json:"criticality,omitempty"`
}

type controlCorridorSummary struct {
	SeedCount        int  `json:"seedCount"`
	SeedTruncated    bool `json:"seedTruncated,omitempty"`
	AuthorityCount   int  `json:"authorityCount"`
	DecisionCount    int  `json:"decisionCount"`
	MechanismCount   int  `json:"mechanismCount"`
	ImpactCount      int  `json:"impactCount"`
	ImpactRootCount  int  `json:"impactRootCount"`
	ControllerCount  int  `json:"controllerCount,omitempty"`
	MediatorCount    int  `json:"mediatorsCount,omitempty"`
	BundleCount      int  `json:"pathBundleCount,omitempty"`
	TotalNodes       int  `json:"totalNodes"`
	TotalEdges       int  `json:"totalEdges"`
	MaxAuthorityHops int  `json:"maxAuthorityHops"`
	MaxImpactHops    int  `json:"maxImpactHops"`
	Truncated        bool `json:"truncated"`
}

type controlCorridorResponse struct {
	LensID             string                 `json:"lensId"`
	SeedFragmentIDs    []string               `json:"seedFragmentIds"`
	SeedCanonicalPaths []string               `json:"seedCanonicalPaths,omitempty"`
	Nodes              []controlCorridorNode  `json:"nodes"`
	Edges              []controlCorridorEdge  `json:"edges"`
	Bundles            map[string]interface{} `json:"bundles,omitempty"`
	Summary            controlCorridorSummary `json:"summary"`
	Warnings           []string               `json:"warnings,omitempty"`
}

func (h *HorizonHandlers) resolveControlCorridorSeeds(
	ctx context.Context,
	lens *horizonmodels.Lens,
	req controlCorridorRequest,
	maxSeeds int,
) ([]string, []string, bool, []string, error) {
	fragmentsRepo := h.service.Fragments()
	if fragmentsRepo == nil {
		return nil, nil, false, nil, nil
	}

	seedIDs := make([]string, 0, maxSeeds)
	seedPaths := make([]string, 0, maxSeeds)
	seen := map[string]struct{}{}
	warnings := make([]string, 0, 8)
	truncated := false

	addSeed := func(f *horizonmodels.HorizonFragment) {
		if f == nil {
			return
		}
		id := strings.TrimSpace(f.ID)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		if len(seedIDs) >= maxSeeds {
			truncated = true
			return
		}
		seen[id] = struct{}{}
		seedIDs = append(seedIDs, id)
		if cp := strings.TrimSpace(f.CanonicalPath); cp != "" {
			seedPaths = append(seedPaths, cp)
		}
	}

	for _, idRaw := range req.SeedFragmentIDs {
		id := strings.TrimSpace(idRaw)
		if id == "" {
			continue
		}
		f, err := fragmentsRepo.Get(ctx, id)
		if err != nil || f == nil {
			warnings = append(warnings, "seedFragmentId not found: "+id)
			continue
		}
		addSeed(f)
	}
	for _, cpRaw := range req.SeedCanonicalPaths {
		cp := strings.TrimSpace(cpRaw)
		if cp == "" {
			continue
		}
		f, err := fragmentsRepo.GetByCanonicalPath(ctx, cp)
		if err != nil || f == nil {
			warnings = append(warnings, "seedCanonicalPath not found: "+cp)
			continue
		}
		addSeed(f)
	}

	if len(seedIDs) == 0 {
		scopeFrags, err := fragmentsRepo.ListByCanonicalPaths(ctx, lens.Scope)
		if err != nil {
			return nil, nil, false, warnings, err
		}
		sort.SliceStable(scopeFrags, func(i, j int) bool {
			ai := strings.TrimSpace(scopeFrags[i].CanonicalPath)
			aj := strings.TrimSpace(scopeFrags[j].CanonicalPath)
			if ai != aj {
				return ai < aj
			}
			return strings.TrimSpace(scopeFrags[i].ID) < strings.TrimSpace(scopeFrags[j].ID)
		})
		for _, f := range scopeFrags {
			addSeed(f)
		}
	}

	return seedIDs, seedPaths, truncated, warnings, nil
}

func (h *HorizonHandlers) buildControlCorridor(
	ctx context.Context,
	lensID string,
	seedIDs []string,
	seedPaths []string,
	opts controlCorridorOptions,
) (*controlCorridorResponse, error) {
	edgesRepo := h.service.Edges()
	fragmentsRepo := h.service.Fragments()

	const (
		laneAuthority    = "authority"
		laneDecision     = "decision"
		laneMechanism    = "mechanism"
		laneImpact       = "impact"
		laneIntermediary = "intermediary"
		roleAuthority    = "authority"
		roleExecution    = "execution"
		rolePropagation  = "propagation"
	)

	type bfsItem struct {
		id  string
		hop int
	}
	type corridorEdge struct {
		Key         string
		ID          string
		From        string
		To          string
		Type        string
		Role        string
		Criticality float64
	}
	type bundlePath struct {
		ID          string
		Controller  string
		Target      string
		Mediator    string
		NodeIDs     []string
		EdgeKeys    []string
		Coverage    float64
		PathType    string
		Description string
	}

	authorityEdgeSet := map[string]struct{}{
		string(ah.RelationNameBeneficiallyOwns): {},
		string(ah.RelationNameSubsidiaryOf):     {},
		"PARENT_OF":                             {},
		string(ah.RelationNameControlsVia):      {},
		string(ah.RelationNameManagedBy):        {},
		string(ah.RelationNameInvestedIn):       {},
		string(ah.RelationNameHoldsBoardSeat):   {},
		"EXECUTIVE_OF":                          {},
		string(ah.RelationNameMemberOf):         {},
		string(ah.RelationNameInfluences):       {},
		string(ah.RelationNameOperates):         {},
		string(ah.RelationNameAssures):          {},
	}
	executionEdgeSet := map[string]struct{}{
		string(ah.RelationNameGatekeeps): {},
		string(ah.RelationNameWields):    {},
		string(ah.RelationNameImposes):   {},
	}
	propagationEdgeSet := map[string]struct{}{
		string(ah.RelationNameInfluences):       {},
		string(ah.RelationNameDisrupts):         {},
		"IMPACTS":                               {},
		string(ah.RelationNameRegulates):        {},
		string(ah.RelationNameSupplies):         {},
		"REQUIRES":                              {},
		"NEEDS":                                 {},
		"USES":                                  {},
		string(ah.RelationNameProduces):         {},
		string(ah.RelationNameMemberOf):         {},
		string(ah.RelationNameWields):           {},
		string(ah.RelationNameJointVentureWith): {},
		"COOPERATES_WITH":                       {},
	}
	programGovBridgeEdgeSet := map[string]struct{}{
		string(ah.RelationNameOperates):    {},
		string(ah.RelationNameRegulates):   {},
		string(ah.RelationNameInfluences):  {},
		string(ah.RelationNameDisrupts):    {},
		"IMPACTS":                          {},
		string(ah.RelationNameControlsVia): {},
		string(ah.RelationNameManagedBy):   {},
		string(ah.RelationNameImposes):     {},
	}

	// AH relation names are canonical lowercase snake_case; preserve case
	// here so lookups in {authority,execution,propagation}EdgeSet (whose keys
	// come from string(ah.RelationName...)) match without uppercasing.
	// Pre-AH legacy uppercase tokens (e.g. "PARENT_OF") embedded in those
	// sets are intentionally preserved as dead entries: real persisted edges
	// no longer carry them post-migration.
	normalizeType := func(raw string) string {
		return strings.TrimSpace(raw)
	}
	roleForType := func(edgeType string) string {
		et := normalizeType(edgeType)
		if et == "" {
			return ""
		}
		if _, ok := authorityEdgeSet[et]; ok {
			return roleAuthority
		}
		if _, ok := executionEdgeSet[et]; ok {
			return roleExecution
		}
		if _, ok := propagationEdgeSet[et]; ok {
			return rolePropagation
		}
		return ""
	}
	stageFor := func(role, edgeType string) string {
		et := normalizeType(edgeType)
		switch role {
		case roleAuthority:
			return "authority"
		case roleExecution:
			if et == string(ah.RelationNameGatekeeps) || et == string(ah.RelationNameWields) {
				return "decision"
			}
			return "mechanism"
		case rolePropagation:
			return "impact"
		default:
			return "impact"
		}
	}

	fragCache := make(map[string]*horizonmodels.HorizonFragment, len(seedIDs)*8)
	missingFrag := make(map[string]struct{})
	getFragment := func(id string) *horizonmodels.HorizonFragment {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil
		}
		if f, ok := fragCache[id]; ok {
			return f
		}
		if _, miss := missingFrag[id]; miss {
			return nil
		}
		f, err := fragmentsRepo.Get(ctx, id)
		if err != nil || f == nil {
			missingFrag[id] = struct{}{}
			return nil
		}
		fragCache[id] = f
		return f
	}
	labelFor := func(f *horizonmodels.HorizonFragment, id string) string {
		if f != nil {
			if s := strings.TrimSpace(f.Labels["en"]); s != "" {
				return s
			}
			for _, lang := range []string{"ru", "de", "es", "kk"} {
				if s := strings.TrimSpace(f.Labels[lang]); s != "" {
					return s
				}
			}
			if cp := strings.TrimSpace(f.CanonicalPath); cp != "" {
				last := cp
				if parts := strings.Split(cp, "/"); len(parts) > 0 {
					last = parts[len(parts)-1]
				}
				last = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(last, "-", " "), "_", " "))
				if last != "" {
					return last
				}
				return cp
			}
		}
		return strings.TrimSpace(id)
	}
	isProgramNode := func(id string) bool {
		f := getFragment(id)
		if f == nil {
			return false
		}
		if horizonmodels.ClassSubtypeProgram.MatchesFragment(f) {
			return true
		}
		cp := strings.ToLower(strings.TrimSpace(f.CanonicalPath))
		return strings.HasPrefix(cp, "/program/")
	}
	isGovOrOrgNode := func(id string) bool {
		f := getFragment(id)
		if f == nil {
			return false
		}
		if f.Class != ah.FragmentClassOrg {
			return false
		}
		cp := strings.ToLower(strings.TrimSpace(f.CanonicalPath))
		return cp == "" || strings.HasPrefix(cp, "/gov/") || strings.HasPrefix(cp, "/org/")
	}

	adjCache := make(map[string][]*horizonmodels.DependencyEdge, len(seedIDs)*8)
	getAdjacent := func(nodeID string) []*horizonmodels.DependencyEdge {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			return nil
		}
		if cached, ok := adjCache[nodeID]; ok {
			return cached
		}
		outgoing, outErr := edgesRepo.ListBySource(ctx, "global", nodeID, nil, opts.PerNodeEdgeLimit)
		incoming, inErr := edgesRepo.ListByTarget(ctx, "global", nodeID, nil, opts.PerNodeEdgeLimit)
		if outErr != nil && inErr != nil {
			adjCache[nodeID] = []*horizonmodels.DependencyEdge{}
			return adjCache[nodeID]
		}
		seen := map[string]struct{}{}
		merged := make([]*horizonmodels.DependencyEdge, 0, len(outgoing)+len(incoming))
		appendUnique := func(list []*horizonmodels.DependencyEdge) {
			for _, e := range list {
				if e == nil {
					continue
				}
				s := strings.TrimSpace(e.SourceFragmentID)
				t := strings.TrimSpace(e.TargetFragmentID)
				et := normalizeType(string(e.Relation))
				if s == "" || t == "" || et == "" {
					continue
				}
				key := strings.TrimSpace(e.ID)
				if key == "" {
					key = s + "|" + t + "|" + et
				}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				merged = append(merged, e)
			}
		}
		appendUnique(outgoing)
		appendUnique(incoming)
		adjCache[nodeID] = merged
		return merged
	}

	maxExploreNodes := opts.MaxNodes*4 + 32
	maxExploreEdges := opts.MaxEdges*6 + 64
	maxExploreHops := opts.MaxAuthorityHops + opts.MaxImpactHops + 4
	if maxExploreHops < 3 {
		maxExploreHops = 3
	}
	truncated := false

	exploredNodes := make(map[string]struct{}, len(seedIDs)*16)
	addExploredNode := func(id string) bool {
		id = strings.TrimSpace(id)
		if id == "" {
			return false
		}
		if _, ok := exploredNodes[id]; ok {
			return true
		}
		if len(exploredNodes) >= maxExploreNodes {
			truncated = true
			return false
		}
		exploredNodes[id] = struct{}{}
		return true
	}
	edgePool := make(map[string]corridorEdge, maxExploreEdges)
	addEdgeToPool := func(e *horizonmodels.DependencyEdge, role string) bool {
		if e == nil {
			return false
		}
		s := strings.TrimSpace(e.SourceFragmentID)
		t := strings.TrimSpace(e.TargetFragmentID)
		et := normalizeType(string(e.Relation))
		if s == "" || t == "" || et == "" || role == "" {
			return false
		}
		key := s + "|" + t + "|" + et
		if _, ok := edgePool[key]; ok {
			return true
		}
		if len(edgePool) >= maxExploreEdges {
			truncated = true
			return false
		}
		eid := strings.TrimSpace(e.ID)
		if eid == "" {
			eid = key
		}
		edgePool[key] = corridorEdge{
			Key:         key,
			ID:          eid,
			From:        s,
			To:          t,
			Type:        et,
			Role:        role,
			Criticality: e.Properties.Criticality,
		}
		return true
	}

	exploreVisited := make(map[string]int, len(seedIDs)*8)
	exploreQueue := make([]bfsItem, 0, len(seedIDs))
	enqueueExplore := func(id string, hop int) {
		id = strings.TrimSpace(id)
		if id == "" || hop > maxExploreHops {
			return
		}
		if prev, ok := exploreVisited[id]; ok && hop >= prev {
			return
		}
		exploreVisited[id] = hop
		exploreQueue = append(exploreQueue, bfsItem{id: id, hop: hop})
	}
	for _, sid := range seedIDs {
		if addExploredNode(sid) {
			enqueueExplore(sid, 0)
		}
	}
	for qi := 0; qi < len(exploreQueue); qi++ {
		cur := exploreQueue[qi]
		if cur.hop > maxExploreHops {
			continue
		}
		for _, e := range getAdjacent(cur.id) {
			if e == nil {
				continue
			}
			role := roleForType(string(e.Relation))
			if role == "" {
				continue
			}
			if !addEdgeToPool(e, role) {
				continue
			}
			s := strings.TrimSpace(e.SourceFragmentID)
			t := strings.TrimSpace(e.TargetFragmentID)
			if s == "" || t == "" {
				continue
			}
			_ = addExploredNode(s)
			_ = addExploredNode(t)
			if s == cur.id {
				enqueueExplore(t, cur.hop+1)
			} else if t == cur.id {
				enqueueExplore(s, cur.hop+1)
			}
		}
	}

	edgeList := make([]corridorEdge, 0, len(edgePool))
	for _, e := range edgePool {
		edgeList = append(edgeList, e)
	}
	sort.SliceStable(edgeList, func(i, j int) bool {
		if edgeList[i].From != edgeList[j].From {
			return edgeList[i].From < edgeList[j].From
		}
		if edgeList[i].To != edgeList[j].To {
			return edgeList[i].To < edgeList[j].To
		}
		if edgeList[i].Type != edgeList[j].Type {
			return edgeList[i].Type < edgeList[j].Type
		}
		return edgeList[i].ID < edgeList[j].ID
	})

	outByRole := map[string]map[string][]corridorEdge{
		roleAuthority:   {},
		roleExecution:   {},
		rolePropagation: {},
	}
	inByRole := map[string]map[string][]corridorEdge{
		roleAuthority:   {},
		roleExecution:   {},
		rolePropagation: {},
	}
	for _, e := range edgeList {
		if _, ok := outByRole[e.Role][e.From]; !ok {
			outByRole[e.Role][e.From] = make([]corridorEdge, 0, 4)
		}
		outByRole[e.Role][e.From] = append(outByRole[e.Role][e.From], e)
		if _, ok := inByRole[e.Role][e.To]; !ok {
			inByRole[e.Role][e.To] = make([]corridorEdge, 0, 4)
		}
		inByRole[e.Role][e.To] = append(inByRole[e.Role][e.To], e)
	}

	upstreamLimit := opts.MaxAuthorityHops + 3
	upstreamVisited := make(map[string]int, len(seedIDs)*8)
	upstreamQueue := make([]bfsItem, 0, len(seedIDs))
	enqueueUpstream := func(id string, hop int) {
		id = strings.TrimSpace(id)
		if id == "" || hop > upstreamLimit {
			return
		}
		if prev, ok := upstreamVisited[id]; ok && hop >= prev {
			return
		}
		upstreamVisited[id] = hop
		upstreamQueue = append(upstreamQueue, bfsItem{id: id, hop: hop})
	}
	for _, sid := range seedIDs {
		enqueueUpstream(sid, 0)
	}
	for qi := 0; qi < len(upstreamQueue); qi++ {
		cur := upstreamQueue[qi]
		if cur.hop >= upstreamLimit {
			continue
		}
		for _, role := range []string{roleAuthority, roleExecution} {
			for _, e := range inByRole[role][cur.id] {
				enqueueUpstream(e.From, cur.hop+1)
			}
		}
		if isProgramNode(cur.id) {
			for _, e := range inByRole[rolePropagation][cur.id] {
				if _, ok := programGovBridgeEdgeSet[e.Type]; !ok {
					continue
				}
				if isGovOrOrgNode(e.From) {
					enqueueUpstream(e.From, cur.hop+1)
				}
			}
		}
	}

	isControllerLike := func(id string) bool {
		if len(outByRole[roleAuthority][id])+len(outByRole[roleExecution][id]) > 0 {
			return true
		}
		f := getFragment(id)
		if f == nil {
			return false
		}
		switch f.Class {
		case ah.FragmentClassOrg, ah.FragmentClassPerson,
			ah.FragmentClassDecision, ah.FragmentClassMechanism:
			return true
		}
		cp := strings.ToLower(strings.TrimSpace(f.CanonicalPath))
		return strings.HasPrefix(cp, "/gov/") || strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/program/") || strings.HasPrefix(cp, "/decision/") || strings.HasPrefix(cp, "/mechanism/")
	}

	controllers := make(map[string]struct{}, len(seedIDs))
	for id := range upstreamVisited {
		if isControllerLike(id) {
			controllers[id] = struct{}{}
		}
	}
	if len(controllers) == 0 {
		for _, sid := range seedIDs {
			controllers[sid] = struct{}{}
		}
	}

	forwardLimit := opts.MaxAuthorityHops + 4
	forwardDist := make(map[string]int, len(seedIDs)*16)
	forwardQueue := make([]bfsItem, 0, len(controllers)+len(seedIDs))
	enqueueForward := func(id string, hop int) {
		id = strings.TrimSpace(id)
		if id == "" || hop > forwardLimit {
			return
		}
		if old, ok := forwardDist[id]; ok && hop >= old {
			return
		}
		forwardDist[id] = hop
		forwardQueue = append(forwardQueue, bfsItem{id: id, hop: hop})
	}
	for cid := range controllers {
		enqueueForward(cid, 0)
	}
	for _, sid := range seedIDs {
		enqueueForward(sid, 0)
	}
	impactRoots := make(map[string]struct{}, 16)
	for qi := 0; qi < len(forwardQueue); qi++ {
		cur := forwardQueue[qi]
		if cur.hop >= forwardLimit {
			continue
		}
		for _, role := range []string{roleAuthority, roleExecution} {
			for _, e := range outByRole[role][cur.id] {
				enqueueForward(e.To, cur.hop+1)
				if e.Type == string(ah.RelationNameImposes) ||
					e.Type == string(ah.RelationNameImposes) ||
					e.Type == string(ah.RelationNameImposes) {
					impactRoots[e.To] = struct{}{}
				}
			}
		}
	}

	propStart := make(map[string]struct{}, len(seedIDs)+len(forwardDist))
	for _, sid := range seedIDs {
		propStart[sid] = struct{}{}
	}
	for id := range forwardDist {
		f := getFragment(id)
		var fc ah.FragmentClass
		if f != nil {
			fc = f.Class
		}
		if fc == ah.FragmentClassDecision || fc == ah.FragmentClassMechanism || len(outByRole[roleExecution][id]) > 0 {
			propStart[id] = struct{}{}
		}
	}

	impactLimit := opts.MaxImpactHops
	impactDist := make(map[string]int, len(seedIDs)*24)
	impactQueue := make([]bfsItem, 0, len(propStart))
	enqueueImpact := func(id string, hop int) {
		id = strings.TrimSpace(id)
		if id == "" || hop > impactLimit {
			return
		}
		if old, ok := impactDist[id]; ok && hop >= old {
			return
		}
		impactDist[id] = hop
		impactQueue = append(impactQueue, bfsItem{id: id, hop: hop})
	}
	for id := range propStart {
		enqueueImpact(id, 0)
	}
	for qi := 0; qi < len(impactQueue); qi++ {
		cur := impactQueue[qi]
		if cur.hop >= impactLimit {
			continue
		}
		for _, e := range outByRole[rolePropagation][cur.id] {
			enqueueImpact(e.To, cur.hop+1)
		}
	}

	isImpactTargetType := func(id string) bool {
		f := getFragment(id)
		if f == nil {
			return false
		}
		switch f.Class {
		case ah.FragmentClassMarket, ah.FragmentClassOrg,
			ah.FragmentClassProduct, ah.FragmentClassSite,
			ah.FragmentClassCommunity:
			return true
		}
		// Legacy DOMAIN folded to canonical-path /domain/* (no FragmentClass);
		// Legacy PROGRAM folded into ah.FragmentClassOrg above (subtype=program).
		cp := strings.ToLower(strings.TrimSpace(f.CanonicalPath))
		return strings.HasPrefix(cp, "/market/") || strings.HasPrefix(cp, "/domain/") || strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/program/")
	}

	impactedTargets := make(map[string]struct{}, 24)
	for id, d := range impactDist {
		if d <= 0 {
			continue
		}
		if _, ok := controllers[id]; ok {
			continue
		}
		if len(outByRole[rolePropagation][id]) == 0 || isImpactTargetType(id) {
			impactedTargets[id] = struct{}{}
		}
	}
	if len(impactedTargets) == 0 {
		type impactCandidate struct {
			id   string
			dist int
		}
		cands := make([]impactCandidate, 0, len(impactDist))
		for id, d := range impactDist {
			if d <= 0 {
				continue
			}
			cands = append(cands, impactCandidate{id: id, dist: d})
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].dist != cands[j].dist {
				return cands[i].dist > cands[j].dist
			}
			return cands[i].id < cands[j].id
		})
		for _, c := range cands {
			impactedTargets[c.id] = struct{}{}
			if len(impactedTargets) >= 8 {
				break
			}
		}
	}

	backwardLimit := opts.MaxImpactHops + 4
	backwardDist := make(map[string]int, len(seedIDs)*24)
	backwardQueue := make([]bfsItem, 0, len(impactedTargets))
	enqueueBackward := func(id string, hop int) {
		id = strings.TrimSpace(id)
		if id == "" || hop > backwardLimit {
			return
		}
		if old, ok := backwardDist[id]; ok && hop >= old {
			return
		}
		backwardDist[id] = hop
		backwardQueue = append(backwardQueue, bfsItem{id: id, hop: hop})
	}
	for id := range impactedTargets {
		enqueueBackward(id, 0)
	}
	for qi := 0; qi < len(backwardQueue); qi++ {
		cur := backwardQueue[qi]
		if cur.hop >= backwardLimit {
			continue
		}
		for _, e := range inByRole[rolePropagation][cur.id] {
			enqueueBackward(e.From, cur.hop+1)
		}
	}

	intersection := make(map[string]struct{}, len(forwardDist))
	for id := range forwardDist {
		if _, ok := backwardDist[id]; ok {
			intersection[id] = struct{}{}
		}
	}
	for _, sid := range seedIDs {
		intersection[sid] = struct{}{}
	}

	decisions := make(map[string]struct{}, 16)
	mechanisms := make(map[string]struct{}, 16)
	for id := range intersection {
		f := getFragment(id)
		if f == nil {
			continue
		}
		switch f.Class {
		case ah.FragmentClassDecision:
			decisions[id] = struct{}{}
		case ah.FragmentClassMechanism:
			mechanisms[id] = struct{}{}
		}
	}
	for id := range forwardDist {
		f := getFragment(id)
		if f == nil {
			continue
		}
		switch f.Class {
		case ah.FragmentClassDecision:
			decisions[id] = struct{}{}
		case ah.FragmentClassMechanism:
			mechanisms[id] = struct{}{}
		}
	}

	mediators := make(map[string]struct{}, len(intersection))
	for id := range intersection {
		if _, ok := controllers[id]; ok {
			continue
		}
		if _, ok := impactedTargets[id]; ok {
			continue
		}
		if _, ok := decisions[id]; ok {
			continue
		}
		if _, ok := mechanisms[id]; ok {
			continue
		}
		mediators[id] = struct{}{}
	}

	finalNodeSet := make(map[string]struct{}, len(intersection)+len(controllers)+len(impactedTargets)+32)
	finalEdgeSet := make(map[string]corridorEdge, len(edgePool))
	addFinalNode := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		finalNodeSet[id] = struct{}{}
	}
	addFinalEdge := func(e corridorEdge) {
		if e.Key == "" {
			return
		}
		finalEdgeSet[e.Key] = e
		addFinalNode(e.From)
		addFinalNode(e.To)
	}

	for _, sid := range seedIDs {
		addFinalNode(sid)
	}
	for id := range controllers {
		addFinalNode(id)
	}
	for id := range decisions {
		addFinalNode(id)
	}
	for id := range mechanisms {
		addFinalNode(id)
	}
	for id := range mediators {
		addFinalNode(id)
	}
	for id := range impactedTargets {
		addFinalNode(id)
	}

	type headPath struct {
		Controller string
		Edges      []corridorEdge
	}
	type meetPath struct {
		Meet string
		Tail []corridorEdge
		Dist int
	}

	rootsForHead := make(map[string]struct{}, len(controllers)+len(seedIDs))
	for id := range controllers {
		rootsForHead[id] = struct{}{}
	}
	for _, sid := range seedIDs {
		rootsForHead[sid] = struct{}{}
	}

	forwardPreds := make(map[string][]corridorEdge, len(forwardDist))
	for _, role := range []string{roleAuthority, roleExecution} {
		for fromID, edges := range outByRole[role] {
			fromDist, ok := forwardDist[fromID]
			if !ok {
				continue
			}
			for _, e := range edges {
				toDist, ok := forwardDist[e.To]
				if !ok || toDist != fromDist+1 {
					continue
				}
				forwardPreds[e.To] = append(forwardPreds[e.To], e)
			}
		}
	}
	for nodeID := range forwardPreds {
		sort.SliceStable(forwardPreds[nodeID], func(i, j int) bool {
			a := forwardPreds[nodeID][i]
			b := forwardPreds[nodeID][j]
			if a.From != b.From {
				return a.From < b.From
			}
			if a.Type != b.Type {
				return a.Type < b.Type
			}
			if a.To != b.To {
				return a.To < b.To
			}
			return a.Key < b.Key
		})
	}

	headPathsCache := make(map[string][]headPath, 64)
	collectHeadPaths := func(meetID string, maxPaths int) []headPath {
		meetID = strings.TrimSpace(meetID)
		if meetID == "" || maxPaths <= 0 {
			return nil
		}
		if cached, ok := headPathsCache[meetID]; ok {
			if len(cached) > maxPaths {
				return append([]headPath(nil), cached[:maxPaths]...)
			}
			return append([]headPath(nil), cached...)
		}

		paths := make([]headPath, 0, maxPaths)
		inStack := make(map[string]struct{}, 32)
		var walk func(nodeID string, reversePath []corridorEdge)
		walk = func(nodeID string, reversePath []corridorEdge) {
			if len(paths) >= maxPaths {
				return
			}
			nodeID = strings.TrimSpace(nodeID)
			if nodeID == "" {
				return
			}
			if _, cycle := inStack[nodeID]; cycle {
				return
			}
			if _, ok := rootsForHead[nodeID]; ok {
				forwardPath := make([]corridorEdge, len(reversePath))
				for i := 0; i < len(reversePath); i++ {
					forwardPath[i] = reversePath[len(reversePath)-1-i]
				}
				paths = append(paths, headPath{
					Controller: nodeID,
					Edges:      forwardPath,
				})
				return
			}

			preds := forwardPreds[nodeID]
			if len(preds) == 0 {
				return
			}

			inStack[nodeID] = struct{}{}
			for _, p := range preds {
				if len(paths) >= maxPaths {
					break
				}
				nextPath := append(append([]corridorEdge(nil), reversePath...), p)
				walk(p.From, nextPath)
			}
			delete(inStack, nodeID)
		}
		walk(meetID, nil)

		if len(paths) == 0 {
			if _, ok := forwardDist[meetID]; ok {
				paths = append(paths, headPath{Controller: meetID, Edges: nil})
			}
		}
		sort.SliceStable(paths, func(i, j int) bool {
			if paths[i].Controller != paths[j].Controller {
				return paths[i].Controller < paths[j].Controller
			}
			if len(paths[i].Edges) != len(paths[j].Edges) {
				return len(paths[i].Edges) < len(paths[j].Edges)
			}
			keyI := ""
			for _, e := range paths[i].Edges {
				keyI += "|" + e.Key
			}
			keyJ := ""
			for _, e := range paths[j].Edges {
				keyJ += "|" + e.Key
			}
			return keyI < keyJ
		})
		headPathsCache[meetID] = append([]headPath(nil), paths...)
		if len(paths) > maxPaths {
			return append([]headPath(nil), paths[:maxPaths]...)
		}
		return append([]headPath(nil), paths...)
	}

	findMeetPaths := func(targetID string, maxMeets int) []meetPath {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" || maxMeets <= 0 {
			return nil
		}
		dist := map[string]int{targetID: 0}
		nextToTarget := make(map[string]corridorEdge, 64)
		queue := []string{targetID}
		for qi := 0; qi < len(queue); qi++ {
			cur := queue[qi]
			curDist := dist[cur]
			if curDist >= backwardLimit {
				continue
			}
			for _, e := range inByRole[rolePropagation][cur] {
				pred := strings.TrimSpace(e.From)
				if pred == "" {
					continue
				}
				nextDist := curDist + 1
				oldDist, seen := dist[pred]
				if seen && nextDist >= oldDist {
					continue
				}
				dist[pred] = nextDist
				nextToTarget[pred] = e
				queue = append(queue, pred)
			}
		}

		meets := make([]meetPath, 0, 8)
		for meetID, d := range dist {
			if _, ok := forwardDist[meetID]; !ok {
				continue
			}
			tail := make([]corridorEdge, 0, d)
			cur := meetID
			guard := 0
			for cur != targetID && guard < 128 {
				guard++
				e, ok := nextToTarget[cur]
				if !ok {
					tail = nil
					break
				}
				tail = append(tail, e)
				cur = e.To
			}
			if cur != targetID {
				continue
			}
			meets = append(meets, meetPath{
				Meet: meetID,
				Tail: tail,
				Dist: d,
			})
		}
		sort.SliceStable(meets, func(i, j int) bool {
			iForward := forwardDist[meets[i].Meet]
			jForward := forwardDist[meets[j].Meet]
			iScore := iForward + meets[i].Dist
			jScore := jForward + meets[j].Dist
			if iScore != jScore {
				return iScore < jScore
			}
			if meets[i].Dist != meets[j].Dist {
				return meets[i].Dist < meets[j].Dist
			}
			if iForward != jForward {
				return iForward < jForward
			}
			return meets[i].Meet < meets[j].Meet
		})
		if len(meets) > maxMeets {
			meets = meets[:maxMeets]
		}
		return meets
	}

	nodeUsage := make(map[string]int, 64)
	edgeUsage := make(map[string]int, 64)
	usedPathKeys := map[string]struct{}{}
	bundles := make([]bundlePath, 0, 32)

	targetIDs := make([]string, 0, len(impactedTargets))
	for id := range impactedTargets {
		targetIDs = append(targetIDs, id)
	}
	sort.Strings(targetIDs)

	stopBundles := false
	for _, targetID := range targetIDs {
		if stopBundles {
			break
		}
		meetPaths := findMeetPaths(targetID, 6)
		for _, mt := range meetPaths {
			if stopBundles {
				break
			}
			headPaths := collectHeadPaths(mt.Meet, 8)
			if len(headPaths) == 0 {
				headPaths = []headPath{{Controller: mt.Meet, Edges: nil}}
			}
			for _, hp := range headPaths {
				controllerID := strings.TrimSpace(hp.Controller)
				if controllerID == "" {
					controllerID = mt.Meet
				}

				nodeSeq := make([]string, 0, len(hp.Edges)+len(mt.Tail)+2)
				edgeSeq := make([]string, 0, len(hp.Edges)+len(mt.Tail))
				nodeSeq = append(nodeSeq, controllerID)

				for _, e := range hp.Edges {
					addFinalEdge(e)
					edgeSeq = append(edgeSeq, e.Key)
					if len(nodeSeq) == 0 || nodeSeq[len(nodeSeq)-1] != e.To {
						nodeSeq = append(nodeSeq, e.To)
					}
				}
				if len(nodeSeq) == 0 || nodeSeq[len(nodeSeq)-1] != mt.Meet {
					nodeSeq = append(nodeSeq, mt.Meet)
				}
				for _, e := range mt.Tail {
					addFinalEdge(e)
					edgeSeq = append(edgeSeq, e.Key)
					if len(nodeSeq) == 0 || nodeSeq[len(nodeSeq)-1] != e.To {
						nodeSeq = append(nodeSeq, e.To)
					}
				}
				if len(nodeSeq) == 0 || nodeSeq[len(nodeSeq)-1] != targetID {
					nodeSeq = append(nodeSeq, targetID)
				}
				if len(nodeSeq) < 2 {
					continue
				}

				pathSig := controllerID + "|" + targetID + "|" + strings.Join(edgeSeq, ",")
				if _, ok := usedPathKeys[pathSig]; ok {
					continue
				}
				usedPathKeys[pathSig] = struct{}{}

				pathType := "transitive"
				hasExecution := false
				hasPropagation := false
				for _, ek := range edgeSeq {
					e, ok := edgePool[ek]
					if !ok {
						continue
					}
					if e.Role == roleExecution {
						hasExecution = true
					}
					if e.Role == rolePropagation {
						hasPropagation = true
					}
				}
				switch {
				case len(edgeSeq) == 1 && hasPropagation:
					pathType = "direct-impact"
				case hasExecution && hasPropagation:
					pathType = "decision-mechanism-impact"
				case hasExecution:
					pathType = "authority-execution"
				case hasPropagation:
					pathType = "propagation"
				}

				for _, nid := range nodeSeq {
					addFinalNode(nid)
					nodeUsage[nid]++
				}
				for _, ek := range edgeSeq {
					edgeUsage[ek]++
				}

				bundles = append(bundles, bundlePath{
					ID:          "bundle:" + pathSig,
					Controller:  controllerID,
					Target:      targetID,
					Mediator:    mt.Meet,
					NodeIDs:     nodeSeq,
					EdgeKeys:    edgeSeq,
					Coverage:    1.0 / float64(len(edgeSeq)+1),
					PathType:    pathType,
					Description: "controller-to-impact corridor",
				})
				if len(bundles) >= 96 {
					stopBundles = true
					break
				}
			}
		}
	}

	if len(finalEdgeSet) == 0 {
		for _, e := range edgeList {
			if e.Role == roleAuthority || e.Role == roleExecution {
				if _, ok := forwardDist[e.From]; ok {
					if _, ok2 := forwardDist[e.To]; ok2 {
						addFinalEdge(e)
					}
				}
			}
			if e.Role == rolePropagation {
				if _, ok := backwardDist[e.From]; ok {
					if _, ok2 := backwardDist[e.To]; ok2 {
						addFinalEdge(e)
					}
				}
			}
		}
	}

	type nodeRank struct {
		ID       string
		Priority int
		Depth    int
		Label    string
	}
	nodeRanks := make([]nodeRank, 0, len(finalNodeSet))
	for id := range finalNodeSet {
		depth := 999
		if d, ok := forwardDist[id]; ok && d < depth {
			depth = d
		}
		if d, ok := backwardDist[id]; ok && d < depth {
			depth = d
		}
		if depth == 999 {
			depth = 0
		}
		priority := 70
		if containsID(seedIDs, id) {
			priority = 0
		} else if _, ok := controllers[id]; ok {
			priority = 10
		} else if _, ok := decisions[id]; ok {
			priority = 20
		} else if _, ok := mechanisms[id]; ok {
			priority = 30
		} else if _, ok := mediators[id]; ok {
			priority = 40
		} else if _, ok := impactedTargets[id]; ok {
			priority = 50
		}
		nodeRanks = append(nodeRanks, nodeRank{
			ID:       id,
			Priority: priority,
			Depth:    depth,
			Label:    labelFor(getFragment(id), id),
		})
	}
	sort.SliceStable(nodeRanks, func(i, j int) bool {
		if nodeRanks[i].Priority != nodeRanks[j].Priority {
			return nodeRanks[i].Priority < nodeRanks[j].Priority
		}
		if nodeRanks[i].Depth != nodeRanks[j].Depth {
			return nodeRanks[i].Depth < nodeRanks[j].Depth
		}
		if nodeRanks[i].Label != nodeRanks[j].Label {
			return nodeRanks[i].Label < nodeRanks[j].Label
		}
		return nodeRanks[i].ID < nodeRanks[j].ID
	})

	keptNodes := make(map[string]struct{}, len(nodeRanks))
	nodeLimit := opts.MaxNodes
	if len(nodeRanks) > nodeLimit {
		truncated = true
		nodeRanks = nodeRanks[:nodeLimit]
	}
	for _, nr := range nodeRanks {
		keptNodes[nr.ID] = struct{}{}
	}

	keptEdges := make([]corridorEdge, 0, len(finalEdgeSet))
	for _, e := range finalEdgeSet {
		if _, ok := keptNodes[e.From]; !ok {
			continue
		}
		if _, ok := keptNodes[e.To]; !ok {
			continue
		}
		keptEdges = append(keptEdges, e)
	}
	roleRank := map[string]int{
		roleAuthority:   1,
		roleExecution:   2,
		rolePropagation: 3,
	}
	sort.SliceStable(keptEdges, func(i, j int) bool {
		if roleRank[keptEdges[i].Role] != roleRank[keptEdges[j].Role] {
			return roleRank[keptEdges[i].Role] < roleRank[keptEdges[j].Role]
		}
		if keptEdges[i].Type != keptEdges[j].Type {
			return keptEdges[i].Type < keptEdges[j].Type
		}
		if keptEdges[i].From != keptEdges[j].From {
			return keptEdges[i].From < keptEdges[j].From
		}
		if keptEdges[i].To != keptEdges[j].To {
			return keptEdges[i].To < keptEdges[j].To
		}
		return keptEdges[i].ID < keptEdges[j].ID
	})
	if len(keptEdges) > opts.MaxEdges {
		truncated = true
		keptEdges = keptEdges[:opts.MaxEdges]
	}

	nodeRoles := make(map[string]map[string]struct{}, len(keptNodes))
	for _, e := range keptEdges {
		if _, ok := nodeRoles[e.From]; !ok {
			nodeRoles[e.From] = map[string]struct{}{}
		}
		if _, ok := nodeRoles[e.To]; !ok {
			nodeRoles[e.To] = map[string]struct{}{}
		}
		nodeRoles[e.From][e.Role] = struct{}{}
		nodeRoles[e.To][e.Role] = struct{}{}
	}

	nodeLane := func(id string) string {
		if _, ok := decisions[id]; ok {
			return laneDecision
		}
		if _, ok := mechanisms[id]; ok {
			return laneMechanism
		}
		if _, ok := controllers[id]; ok {
			return laneAuthority
		}
		if _, ok := impactedTargets[id]; ok {
			return laneImpact
		}
		if _, ok := mediators[id]; ok {
			return laneIntermediary
		}
		if rs, ok := nodeRoles[id]; ok {
			if _, ok2 := rs[roleExecution]; ok2 {
				return laneMechanism
			}
			if _, ok2 := rs[roleAuthority]; ok2 {
				return laneAuthority
			}
			if _, ok2 := rs[rolePropagation]; ok2 {
				return laneImpact
			}
		}
		return laneIntermediary
	}

	nodesOut := make([]controlCorridorNode, 0, len(nodeRanks))
	for _, nr := range nodeRanks {
		id := nr.ID
		f := getFragment(id)
		n := controlCorridorNode{
			ID:    id,
			Label: labelFor(f, id),
			Lane:  nodeLane(id),
			Depth: nr.Depth,
			Seed:  containsID(seedIDs, id),
		}
		if f != nil {
			n.Type = strings.TrimSpace(string(f.Class))
			n.CanonicalPath = strings.TrimSpace(f.CanonicalPath)
		}
		nodesOut = append(nodesOut, n)
	}

	edgesOut := make([]controlCorridorEdge, 0, len(keptEdges))
	for _, e := range keptEdges {
		edgesOut = append(edgesOut, controlCorridorEdge{
			ID:          e.ID,
			From:        e.From,
			To:          e.To,
			Type:        e.Type,
			Stage:       stageFor(e.Role, e.Type),
			Role:        e.Role,
			Criticality: e.Criticality,
		})
	}

	keptEdgeKeys := make(map[string]struct{}, len(keptEdges))
	for _, e := range keptEdges {
		keptEdgeKeys[e.Key] = struct{}{}
	}

	type scoredNode struct {
		ID       string  `json:"id"`
		Label    string  `json:"label"`
		Type     string  `json:"type,omitempty"`
		Path     string  `json:"canonicalPath,omitempty"`
		Score    float64 `json:"score,omitempty"`
		Coverage int     `json:"coverage,omitempty"`
	}
	toScoredNode := func(id string, score float64, coverage int) scoredNode {
		f := getFragment(id)
		out := scoredNode{
			ID:       id,
			Label:    labelFor(f, id),
			Score:    score,
			Coverage: coverage,
		}
		if f != nil {
			out.Type = strings.TrimSpace(string(f.Class))
			out.Path = strings.TrimSpace(f.CanonicalPath)
		}
		return out
	}

	controllersOut := make([]scoredNode, 0, len(controllers))
	for id := range controllers {
		if _, ok := keptNodes[id]; !ok {
			continue
		}
		controllersOut = append(controllersOut, toScoredNode(id, 1.0/float64((forwardDist[id]+1)), nodeUsage[id]))
	}
	sort.SliceStable(controllersOut, func(i, j int) bool {
		if controllersOut[i].Coverage != controllersOut[j].Coverage {
			return controllersOut[i].Coverage > controllersOut[j].Coverage
		}
		if controllersOut[i].Score != controllersOut[j].Score {
			return controllersOut[i].Score > controllersOut[j].Score
		}
		return controllersOut[i].ID < controllersOut[j].ID
	})

	decisionsOut := make([]scoredNode, 0, len(decisions))
	for id := range decisions {
		if _, ok := keptNodes[id]; !ok {
			continue
		}
		decisionsOut = append(decisionsOut, toScoredNode(id, 1.0/float64((forwardDist[id]+1)), nodeUsage[id]))
	}
	sort.SliceStable(decisionsOut, func(i, j int) bool {
		if decisionsOut[i].Coverage != decisionsOut[j].Coverage {
			return decisionsOut[i].Coverage > decisionsOut[j].Coverage
		}
		return decisionsOut[i].ID < decisionsOut[j].ID
	})

	mechanismsOut := make([]scoredNode, 0, len(mechanisms))
	for id := range mechanisms {
		if _, ok := keptNodes[id]; !ok {
			continue
		}
		mechanismsOut = append(mechanismsOut, toScoredNode(id, 1.0/float64((forwardDist[id]+1)), nodeUsage[id]))
	}
	sort.SliceStable(mechanismsOut, func(i, j int) bool {
		if mechanismsOut[i].Coverage != mechanismsOut[j].Coverage {
			return mechanismsOut[i].Coverage > mechanismsOut[j].Coverage
		}
		return mechanismsOut[i].ID < mechanismsOut[j].ID
	})

	mediatorsOut := make([]scoredNode, 0, len(mediators))
	for id := range mediators {
		if _, ok := keptNodes[id]; !ok {
			continue
		}
		mediatorsOut = append(mediatorsOut, toScoredNode(id, 1.0/float64((forwardDist[id]+1)+(backwardDist[id]+1)), nodeUsage[id]))
	}
	sort.SliceStable(mediatorsOut, func(i, j int) bool {
		if mediatorsOut[i].Coverage != mediatorsOut[j].Coverage {
			return mediatorsOut[i].Coverage > mediatorsOut[j].Coverage
		}
		return mediatorsOut[i].ID < mediatorsOut[j].ID
	})

	targetsOut := make([]scoredNode, 0, len(impactedTargets))
	for id := range impactedTargets {
		if _, ok := keptNodes[id]; !ok {
			continue
		}
		targetsOut = append(targetsOut, toScoredNode(id, 1.0/float64((impactDist[id]+1)), nodeUsage[id]))
	}
	sort.SliceStable(targetsOut, func(i, j int) bool {
		if targetsOut[i].Coverage != targetsOut[j].Coverage {
			return targetsOut[i].Coverage > targetsOut[j].Coverage
		}
		return targetsOut[i].ID < targetsOut[j].ID
	})

	bundlesOut := make([]map[string]interface{}, 0, len(bundles))
	for _, b := range bundles {
		if _, ok := keptNodes[b.Controller]; !ok {
			continue
		}
		if _, ok := keptNodes[b.Target]; !ok {
			continue
		}
		nodeIDs := make([]string, 0, len(b.NodeIDs))
		for _, nid := range b.NodeIDs {
			if _, ok := keptNodes[nid]; ok {
				nodeIDs = append(nodeIDs, nid)
			}
		}
		edgeIDs := make([]string, 0, len(b.EdgeKeys))
		edgeKeys := make([]string, 0, len(b.EdgeKeys))
		for _, ek := range b.EdgeKeys {
			if _, ok := keptEdgeKeys[ek]; ok {
				edgeKeys = append(edgeKeys, ek)
				if edge, okEdge := edgePool[ek]; okEdge && strings.TrimSpace(edge.ID) != "" {
					edgeIDs = append(edgeIDs, strings.TrimSpace(edge.ID))
				} else {
					edgeIDs = append(edgeIDs, ek)
				}
			}
		}
		if len(nodeIDs) == 0 {
			continue
		}
		bundlesOut = append(bundlesOut, map[string]interface{}{
			"id":            b.ID,
			"controllerId":  b.Controller,
			"targetId":      b.Target,
			"mediatorId":    b.Mediator,
			"nodeIds":       nodeIDs,
			"edgeIds":       edgeIDs,
			"edgeKeys":      edgeKeys,
			"coverageScore": b.Coverage,
			"pathType":      b.PathType,
			"description":   b.Description,
		})
	}

	type usageNode struct {
		ID      string
		Count   int
		DistKey int
	}
	usageNodes := make([]usageNode, 0, len(nodeUsage))
	for id, c := range nodeUsage {
		if c <= 0 {
			continue
		}
		if _, ok := keptNodes[id]; !ok {
			continue
		}
		dist := 0
		if d, ok := forwardDist[id]; ok {
			dist += d
		}
		if d, ok := backwardDist[id]; ok {
			dist += d
		}
		usageNodes = append(usageNodes, usageNode{ID: id, Count: c, DistKey: dist})
	}
	sort.SliceStable(usageNodes, func(i, j int) bool {
		if usageNodes[i].Count != usageNodes[j].Count {
			return usageNodes[i].Count > usageNodes[j].Count
		}
		if usageNodes[i].DistKey != usageNodes[j].DistKey {
			return usageNodes[i].DistKey < usageNodes[j].DistKey
		}
		return usageNodes[i].ID < usageNodes[j].ID
	})
	if len(usageNodes) > 12 {
		usageNodes = usageNodes[:12]
	}
	bottleneckNodes := make([]map[string]interface{}, 0, len(usageNodes))
	for _, u := range usageNodes {
		n := toScoredNode(u.ID, float64(u.Count), u.Count)
		bottleneckNodes = append(bottleneckNodes, map[string]interface{}{
			"id":            n.ID,
			"label":         n.Label,
			"type":          n.Type,
			"canonicalPath": n.Path,
			"usageCount":    u.Count,
			"score":         n.Score,
		})
	}

	type usageEdge struct {
		Key   string
		Count int
	}
	usageEdges := make([]usageEdge, 0, len(edgeUsage))
	for key, c := range edgeUsage {
		if c <= 0 {
			continue
		}
		if _, ok := keptEdgeKeys[key]; !ok {
			continue
		}
		usageEdges = append(usageEdges, usageEdge{Key: key, Count: c})
	}
	sort.SliceStable(usageEdges, func(i, j int) bool {
		if usageEdges[i].Count != usageEdges[j].Count {
			return usageEdges[i].Count > usageEdges[j].Count
		}
		return usageEdges[i].Key < usageEdges[j].Key
	})
	if len(usageEdges) > 12 {
		usageEdges = usageEdges[:12]
	}
	bottleneckEdges := make([]map[string]interface{}, 0, len(usageEdges))
	for _, u := range usageEdges {
		e, ok := edgePool[u.Key]
		if !ok {
			continue
		}
		bottleneckEdges = append(bottleneckEdges, map[string]interface{}{
			"id":         e.ID,
			"from":       e.From,
			"to":         e.To,
			"type":       e.Type,
			"role":       e.Role,
			"usageCount": u.Count,
		})
	}

	bundlesPayload := map[string]interface{}{
		"controllers":     controllersOut,
		"decisionPoints":  decisionsOut,
		"mechanisms":      mechanismsOut,
		"mediators":       mediatorsOut,
		"impactedTargets": targetsOut,
		"paths":           bundlesOut,
		"bottlenecks": map[string]interface{}{
			"nodes": bottleneckNodes,
			"edges": bottleneckEdges,
		},
	}

	var authorityCount, decisionCount, mechanismCount, impactCount int
	for _, n := range nodesOut {
		switch strings.TrimSpace(n.Lane) {
		case laneAuthority:
			authorityCount++
		case laneDecision:
			decisionCount++
		case laneMechanism:
			mechanismCount++
		case laneImpact:
			impactCount++
		}
	}
	impactRootCount := 0
	for id := range impactRoots {
		if _, ok := keptNodes[id]; ok {
			impactRootCount++
		}
	}

	return &controlCorridorResponse{
		LensID:             lensID,
		SeedFragmentIDs:    append([]string(nil), seedIDs...),
		SeedCanonicalPaths: append([]string(nil), seedPaths...),
		Nodes:              nodesOut,
		Edges:              edgesOut,
		Bundles:            bundlesPayload,
		Summary: controlCorridorSummary{
			SeedCount:        len(seedIDs),
			AuthorityCount:   authorityCount,
			DecisionCount:    decisionCount,
			MechanismCount:   mechanismCount,
			ImpactCount:      impactCount,
			ImpactRootCount:  impactRootCount,
			TotalNodes:       len(nodesOut),
			TotalEdges:       len(edgesOut),
			MaxAuthorityHops: opts.MaxAuthorityHops,
			MaxImpactHops:    opts.MaxImpactHops,
			ControllerCount:  len(controllersOut),
			MediatorCount:    len(mediatorsOut),
			BundleCount:      len(bundlesOut),
			Truncated:        truncated,
		},
	}, nil
}

func containsID(items []string, id string) bool {
	id = strings.TrimSpace(id)
	for _, it := range items {
		if strings.TrimSpace(it) == id {
			return true
		}
	}
	return false
}

func clampInt(v, min, max, fallback int) int {
	if v == 0 {
		v = fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

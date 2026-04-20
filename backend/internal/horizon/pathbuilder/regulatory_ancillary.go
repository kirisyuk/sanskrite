package pathbuilder

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/regstress"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// UpsertRegulatoryTransmissionArtifact persists REGULATORY_IMPACT_TRANSMISSION_PATH from computed paths.
func (b *DomainArtifactBuilder) UpsertRegulatoryTransmissionArtifact(
	ctx context.Context,
	tenantID, projectID, lensID, rootPath string,
	paths []regstress.RegulatoryImpactPath,
) error {
	if b == nil || b.artifactsRepo == nil || len(paths) == 0 {
		return nil
	}
	nodes, edges, nodeRefs := relationsGraphFromRegPaths(paths)
	stress := make([]float64, 0, len(paths))
	obsSamples := make([]float64, 0, len(paths))
	for _, p := range paths {
		ps := regstress.EvaluatePathStress(p)
		stress = append(stress, ps.CumulativeExposure)
		obsSamples = append(obsSamples, ps.ObservabilityScore)
	}
	notes := map[string]interface{}{
		"rootEntity":           rootPath,
		"pathCount":            len(paths),
		"artifactClass":        "regulatory_transmission",
		"pathStressSamples":    stress,
		"observabilitySamples": obsSamples,
		"whatIfScenarioKind":   string(models.MaterializedKindRegulatoryImpactScenario),
	}
	payload := models.BuildRelationsGraphPayload(nodes, edges, taxonomy.DomainModePolicyImpact, rootPath, notes)
	key := buildRelationsGraphCanonicalKey(tenantID, "regulatory_transmission", rootPath, nodeRefs)
	art := &models.MaterializedArtifact{
		TenantID:     tenantID,
		ProjectID:    projectID,
		LensID:       lensID,
		Kind:         models.MaterializedKindRegulatoryImpactTransmissionPath,
		CanonicalKey: key,
		Payload:      payload,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "regulatory_ancillary_builder",
		UpdatedBy:    "regulatory_ancillary_builder",
		Status:       "active",
		DisplayName:  "Regulatory transmission paths",
		NodeRefs:     nodeRefs,
	}
	if err := art.Validate(); err != nil {
		return err
	}
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, art.Kind, key)
	if err == nil && existing != nil {
		art.ID = existing.ID
		art.CreatedAt = existing.CreatedAt
		art.CreatedBy = existing.CreatedBy
	}
	return b.artifactsRepo.Upsert(ctx, art)
}

// UpsertRegulatoryScenarioArtifact persists REGULATORY_IMPACT_SCENARIO: per-path what-if results for each scenario.
func (b *DomainArtifactBuilder) UpsertRegulatoryScenarioArtifact(
	ctx context.Context,
	tenantID, projectID, lensID, rootPath string,
	paths []regstress.RegulatoryImpactPath,
	scenarios []regstress.RegulatoryScenario,
) error {
	if b == nil || b.artifactsRepo == nil || len(paths) == 0 {
		return nil
	}
	if len(scenarios) == 0 {
		scenarios = []regstress.RegulatoryScenario{{
			ID:        "mechanism_rate_plus15pct",
			Overrides: map[string]float64{"/mechanism/": 1.15},
		}}
	}
	rootRef := strings.TrimSpace(rootPath)
	rootNode := models.SupplyChainPathNodeForRef(rootRef)
	nodes := []models.RelationsGraphNode{{
		Class: rootNode.Class, Subtype: rootNode.Subtype, Ref: rootRef, Name: rootRef,
	}}
	nodeRefs := []string{nodes[0].Ref}
	scenarioIDs := make([]string, 0, len(scenarios))
	var scenarioBlocks []map[string]interface{}
	for _, sc := range scenarios {
		sid := strings.TrimSpace(sc.ID)
		if sid == "" {
			sid = "unnamed_scenario"
		}
		scenarioIDs = append(scenarioIDs, sid)
		var perPath []map[string]interface{}
		for _, p := range paths {
			ps := regstress.EvaluatePathStress(p)
			sr := regstress.EvaluateScenario(p, sc)
			perPath = append(perPath, map[string]interface{}{
				"pathLegCount":     len(p.Legs),
				"baseStress":       sr.BaseStress,
				"scenarioStress":   sr.ScenarioStress,
				"delta":            sr.Delta,
				"observability":    ps.ObservabilityScore,
				"cumulativeExpose": ps.CumulativeExposure,
			})
		}
		scenarioBlocks = append(scenarioBlocks, map[string]interface{}{
			"scenarioId":  sid,
			"overrides":   sc.Overrides,
			"pathResults": perPath,
		})
	}
	sort.Strings(scenarioIDs)
	notes := map[string]interface{}{
		"rootEntity":    rootPath,
		"artifactClass": "regulatory_scenario",
		"pathCount":     len(paths),
		"scenarios":     scenarioBlocks,
	}
	payload := models.BuildRelationsGraphPayload(nodes, nil, taxonomy.DomainModePolicyImpact, rootPath, notes)
	key := buildRelationsGraphCanonicalKey(tenantID, "regulatory_scenario", rootPath, scenarioIDs)
	art := &models.MaterializedArtifact{
		TenantID:     tenantID,
		ProjectID:    projectID,
		LensID:       lensID,
		Kind:         models.MaterializedKindRegulatoryImpactScenario,
		CanonicalKey: key,
		Payload:      payload,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "regulatory_ancillary_builder",
		UpdatedBy:    "regulatory_ancillary_builder",
		Status:       "active",
		DisplayName:  "Regulatory what-if scenarios",
		NodeRefs:     nodeRefs,
	}
	if err := art.Validate(); err != nil {
		return err
	}
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, art.Kind, key)
	if err == nil && existing != nil {
		art.ID = existing.ID
		art.CreatedAt = existing.CreatedAt
		art.CreatedBy = existing.CreatedBy
	}
	return b.artifactsRepo.Upsert(ctx, art)
}

// UpsertRegulatoryTimelineArtifact persists REGULATORY_TIMELINE from relation qualifier dates.
func (b *DomainArtifactBuilder) UpsertRegulatoryTimelineArtifact(
	ctx context.Context,
	tenantID, projectID, lensID, rootPath string,
	rels []agents.DiscoveredRelation,
) error {
	if b == nil || b.artifactsRepo == nil || len(rels) == 0 {
		return nil
	}
	type ev struct {
		t time.Time
		s string
	}
	var events []ev
	for _, r := range rels {
		q := r.Qualifiers
		if q == nil {
			continue
		}
		if t := parseTimeQualifier(q, "effective_date", "effectiveDate"); !t.IsZero() {
			events = append(events, ev{t: t, s: fmt.Sprintf("%s -> %s effective", strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath))})
		}
		if t := parseTimeQualifier(q, "phase_in_start", "phaseInStart", "phase_in_begin"); !t.IsZero() {
			events = append(events, ev{t: t, s: fmt.Sprintf("%s -> %s phase_in_start", strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath))})
		}
		if t := parseTimeQualifier(q, "phase_in_end", "phaseInEnd", "phase_in_finish"); !t.IsZero() {
			events = append(events, ev{t: t, s: fmt.Sprintf("%s -> %s phase_in_end", strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath))})
		}
	}
	if len(events) == 0 {
		return nil
	}
	sort.Slice(events, func(i, j int) bool { return events[i].t.Before(events[j].t) })
	nodes := make([]models.RelationsGraphNode, 0, len(events))
	edges := make([]models.RelationsGraphEdge, 0, len(events)-1)
	nodeRefs := make([]string, 0, len(events))
	for i, e := range events {
		ref := fmt.Sprintf("/regulatory/timeline/%d", i)
		nodes = append(nodes, models.RelationsGraphNode{
			Class: ah.FragmentClassEvidence, Subtype: string(ah.EvidenceSubtypeOther), Ref: ref, Name: e.s,
		})
		nodeRefs = append(nodeRefs, ref)
		if i > 0 {
			edges = append(edges, models.RelationsGraphEdge{From: i - 1, To: i, Relation: ah.RelationNameInfluences})
		}
	}
	notes := map[string]interface{}{"rootEntity": rootPath, "artifactClass": "regulatory_timeline"}
	payload := models.BuildRelationsGraphPayload(nodes, edges, taxonomy.DomainModePolicyImpact, rootPath, notes)
	key := buildRelationsGraphCanonicalKey(tenantID, "regulatory_timeline", rootPath, nodeRefs)
	art := &models.MaterializedArtifact{
		TenantID:     tenantID,
		ProjectID:    projectID,
		LensID:       lensID,
		Kind:         models.MaterializedKindRegulatoryTimeline,
		CanonicalKey: key,
		Payload:      payload,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "regulatory_ancillary_builder",
		UpdatedBy:    "regulatory_ancillary_builder",
		Status:       "active",
		DisplayName:  "Regulatory timeline",
		NodeRefs:     nodeRefs,
	}
	if err := art.Validate(); err != nil {
		return err
	}
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, art.Kind, key)
	if err == nil && existing != nil {
		art.ID = existing.ID
		art.CreatedAt = existing.CreatedAt
		art.CreatedBy = existing.CreatedBy
	}
	return b.artifactsRepo.Upsert(ctx, art)
}

// UpsertRegulatoryCascadeArtifact persists REGULATORY_CASCADE_MAP: direct affected entities plus
// indirect downstream exposure via SUPPLIES_TO (org/fac/market) and optional productdict downstream products.
func (b *DomainArtifactBuilder) UpsertRegulatoryCascadeArtifact(
	ctx context.Context,
	tenantID, projectID, lensID, rootPath string,
	directAffected []string,
) error {
	if b == nil || b.artifactsRepo == nil || len(directAffected) == 0 {
		return nil
	}
	if b.fragmentsRepo == nil || b.edgesRepo == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	const maxHops = 3
	const maxIndirect = 240
	attenuationPerHop := 0.82

	type indRec struct {
		path   string
		att    float64
		origin string
		hops   int
	}
	indirect := map[string]indRec{}
	directSet := map[string]struct{}{}

	addIndirect := func(path, origin string, hops int, baseAtt float64) {
		path = strings.TrimSpace(path)
		origin = strings.TrimSpace(origin)
		if path == "" || origin == "" {
			return
		}
		att := baseAtt
		if att <= 0 || att > 1.01 {
			att = 0.82
		}
		prev, ok := indirect[path]
		if ok && prev.att >= att {
			return
		}
		if len(indirect) >= maxIndirect {
			return
		}
		indirect[path] = indRec{path: path, att: att, origin: origin, hops: hops}
	}

	supplies := DependencyTypeSuppliesTo
	for _, seed := range directAffected {
		seed = strings.TrimSpace(seed)
		if seed == "" {
			continue
		}
		directSet[seed] = struct{}{}
		frag, err := b.fragmentsRepo.GetByCanonicalPath(ctx, seed)
		if err != nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		// BFS downstream along SUPPLIES_TO (supplier -> customer).
		type qn struct {
			id     string
			hop    int
			att    float64
			origin string
		}
		queue := []qn{{id: frag.ID, hop: 0, att: 1.0, origin: seed}}
		seenHop := map[string]int{frag.ID: 0}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur.hop >= maxHops {
				continue
			}
			out, lerr := b.edgesRepo.ListBySource(ctx, tenantID, cur.id, &supplies, 40)
			if lerr != nil {
				continue
			}
			for _, e := range out {
				if e == nil {
					continue
				}
				tid := strings.TrimSpace(e.TargetFragmentID)
				if tid == "" {
					continue
				}
				nextHop := cur.hop + 1
				nextAtt := cur.att * attenuationPerHop
				if h, ok := seenHop[tid]; ok && h <= nextHop {
					continue
				}
				seenHop[tid] = nextHop
				tf, gerr := b.fragmentsRepo.Get(ctx, tid)
				if gerr != nil || tf == nil {
					continue
				}
				cp := strings.TrimSpace(tf.CanonicalPath)
				if cp == "" {
					continue
				}
				if _, isDirect := directSet[cp]; isDirect {
					queue = append(queue, qn{id: tid, hop: nextHop, att: nextAtt, origin: cur.origin})
					continue
				}
				if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/fac/") || strings.HasPrefix(cp, "/market/") {
					addIndirect(cp, cur.origin, nextHop, nextAtt)
				}
				queue = append(queue, qn{id: tid, hop: nextHop, att: nextAtt, origin: cur.origin})
			}
		}
		// Org/fac/market → linked products (PRODUCES, inbound SUPPLIES_TO) then optional productdict downstream.
		if strings.HasPrefix(seed, "/org/") || strings.HasPrefix(seed, "/fac/") || strings.HasPrefix(seed, "/market/") {
			for _, p := range b.linkedProductPathsForEntity(ctx, tenantID, frag.ID, 48) {
				addIndirect(p, seed, 1, attenuationPerHop)
				if b.productDictionary != nil {
					exp, xerr := b.productDictionary.ExpandProductStreamForTenant(ctx, tenantID, p, "downstream", 2, 24)
					if xerr == nil && exp != nil {
						for _, n := range exp.Nodes {
							pp := strings.TrimSpace(n.CanonicalPath)
							if pp == "" || pp == p {
								continue
							}
							hop := n.Hop
							if hop <= 0 {
								hop = 1
							}
							totalHop := 1 + hop
							if totalHop > maxHops {
								continue
							}
							att := attenuationPerHop
							for i := 1; i < totalHop && i < maxHops; i++ {
								att *= attenuationPerHop
							}
							addIndirect(pp, seed, totalHop, att)
						}
					}
				}
			}
		}
		// Product stream: downstream products for regulatory-affected product nodes.
		if b.productDictionary != nil && strings.HasPrefix(seed, "/product/") {
			exp, xerr := b.productDictionary.ExpandProductStreamForTenant(ctx, tenantID, seed, "downstream", 2, 24)
			if xerr == nil && exp != nil {
				for _, n := range exp.Nodes {
					p := strings.TrimSpace(n.CanonicalPath)
					if p == "" || p == seed {
						continue
					}
					hop := n.Hop
					if hop <= 0 {
						hop = 1
					}
					att := attenuationPerHop
					for i := 1; i < hop && i < maxHops; i++ {
						att *= attenuationPerHop
					}
					addIndirect(p, seed, hop, att)
				}
			}
		}
	}

	var nodeRefs []string
	var nodes []models.RelationsGraphNode
	var edges []models.RelationsGraphEdge
	idx := map[string]int{}

	addNode := func(ref, name string) int {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return -1
		}
		if i, ok := idx[ref]; ok {
			return i
		}
		i := len(nodes)
		base := models.SupplyChainPathNodeForRef(ref)
		display := strings.TrimSpace(name)
		if display == "" {
			display = ref
		}
		nodes = append(nodes, models.RelationsGraphNode{Class: base.Class, Subtype: base.Subtype, Ref: ref, Name: display})
		nodeRefs = append(nodeRefs, ref)
		idx[ref] = i
		return i
	}

	for p := range directSet {
		addNode(p, p)
	}
	for p, rec := range indirect {
		name := fmt.Sprintf("%s (indirect att=%.3f hops=%d from=%s)", p, rec.att, rec.hops, rec.origin)
		ip := addNode(p, name)
		oi := addNode(rec.origin, rec.origin)
		if ip >= 0 && oi >= 0 {
			edges = append(edges, models.RelationsGraphEdge{
				From:     oi,
				To:       ip,
				Relation: ah.RelationNameExposesTo,
			})
		}
	}

	if len(nodes) == 0 {
		return nil
	}
	sort.Strings(nodeRefs)
	notes := map[string]interface{}{
		"rootEntity":              rootPath,
		"artifactClass":           "regulatory_cascade",
		"directAffected":          directAffected,
		"indirectCount":           len(indirect),
		"supplyDownstreamMaxHops": maxHops,
		"attenuationPerHop":       attenuationPerHop,
		"productStreamDownstream": b.productDictionary != nil,
		"orgFacProductBridge":     true,
	}
	payload := models.BuildRelationsGraphPayload(nodes, edges, taxonomy.DomainModePolicyImpact, rootPath, notes)
	key := buildRelationsGraphCanonicalKey(tenantID, "regulatory_cascade", rootPath, nodeRefs)
	art := &models.MaterializedArtifact{
		TenantID:     tenantID,
		ProjectID:    projectID,
		LensID:       lensID,
		Kind:         models.MaterializedKindRegulatoryCascadeMap,
		CanonicalKey: key,
		Payload:      payload,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "regulatory_ancillary_builder",
		UpdatedBy:    "regulatory_ancillary_builder",
		Status:       "active",
		DisplayName:  "Regulatory cascade map",
		NodeRefs:     nodeRefs,
	}
	if err := art.Validate(); err != nil {
		return err
	}
	existing, err := b.artifactsRepo.GetByCanonicalKey(ctx, tenantID, art.Kind, key)
	if err == nil && existing != nil {
		art.ID = existing.ID
		art.CreatedAt = existing.CreatedAt
		art.CreatedBy = existing.CreatedBy
	}
	return b.artifactsRepo.Upsert(ctx, art)
}

func relationsGraphFromRegPaths(paths []regstress.RegulatoryImpactPath) ([]models.RelationsGraphNode, []models.RelationsGraphEdge, []string) {
	idx := map[string]int{}
	var nodes []models.RelationsGraphNode
	var nodeRefs []string
	addNode := func(ref string) int {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return -1
		}
		if i, ok := idx[ref]; ok {
			return i
		}
		i := len(nodes)
		idx[ref] = i
		base := models.SupplyChainPathNodeForRef(ref)
		nodes = append(nodes, models.RelationsGraphNode{Class: base.Class, Subtype: base.Subtype, Ref: ref, Name: ref})
		nodeRefs = append(nodeRefs, ref)
		return i
	}
	var edges []models.RelationsGraphEdge
	for _, p := range paths {
		for _, leg := range p.Legs {
			fi := addNode(leg.FromPath)
			ti := addNode(leg.ToPath)
			if fi < 0 || ti < 0 {
				continue
			}
			rel := ah.RelationNameInfluences
			if dec, ok := models.NormalizeRelationName(leg.RelationType); ok && !dec.Dropped && dec.Relation != "" {
				rel = dec.Relation
			}
			edges = append(edges, models.RelationsGraphEdge{
				From:     fi,
				To:       ti,
				Relation: rel,
			})
		}
	}
	return nodes, edges, nodeRefs
}

func (b *DomainArtifactBuilder) linkedProductPathsForEntity(ctx context.Context, tenantID, fragmentID string, limit int) []string {
	if b == nil || b.edgesRepo == nil || b.fragmentsRepo == nil {
		return nil
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return nil
	}
	if limit <= 0 {
		limit = 40
	}
	produces := DependencyTypeProduces
	supplies := DependencyTypeSuppliesTo
	seen := map[string]struct{}{}
	var out []string
	add := func(cp string) {
		cp = strings.TrimSpace(cp)
		if !strings.HasPrefix(cp, "/product/") {
			return
		}
		if _, ok := seen[cp]; ok {
			return
		}
		seen[cp] = struct{}{}
		out = append(out, cp)
	}
	if outE, err := b.edgesRepo.ListBySource(ctx, tenantID, fragmentID, &produces, limit); err == nil {
		for _, e := range outE {
			if e == nil {
				continue
			}
			tid := strings.TrimSpace(e.TargetFragmentID)
			if tid == "" {
				continue
			}
			if tf, gerr := b.fragmentsRepo.Get(ctx, tid); gerr == nil && tf != nil {
				add(tf.CanonicalPath)
			}
		}
	}
	if inE, err := b.edgesRepo.ListByTarget(ctx, tenantID, fragmentID, &supplies, limit); err == nil {
		for _, e := range inE {
			if e == nil {
				continue
			}
			sid := strings.TrimSpace(e.SourceFragmentID)
			if sid == "" {
				continue
			}
			if sf, gerr := b.fragmentsRepo.Get(ctx, sid); gerr == nil && sf != nil {
				add(sf.CanonicalPath)
			}
		}
	}
	return out
}

func parseTimeQualifier(q map[string]interface{}, keys ...string) time.Time {
	for _, k := range keys {
		if v, ok := q[k]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if t, err := time.Parse(time.RFC3339, s); err == nil {
					return t
				}
				if t, err := time.Parse("2006-01-02", s); err == nil {
					return t
				}
			}
		}
	}
	return time.Time{}
}

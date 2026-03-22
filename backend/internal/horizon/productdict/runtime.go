package productdict

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	redis "github.com/redis/go-redis/v9"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
)

const (
	defaultSynonymCollection            = "horizon_product_synonym_pairs"
	defaultTransformCollection          = "horizon_product_transform_pairs"
	defaultSynonymCandidateCollection   = "horizon_product_synonym_candidates"
	defaultTransformCandidateCollection = "horizon_product_transform_candidates"

	redisSynonymsSnapshotKey   = "horizon:productdict:synonyms:v1"
	redisTransformsSnapshotKey = "horizon:productdict:transforms:v1"
	redisTransformAdjPrefix    = "horizon:productdict:transform_adj:v1:"
)

type StreamDirection string

const (
	StreamDirectionDownstream StreamDirection = "downstream"
	StreamDirectionUpstream   StreamDirection = "upstream"
)

type ProductTransformPair struct {
	FromPath     string `json:"fromPath"`
	ToPath       string `json:"toPath"`
	RelationKind string `json:"relationKind"`
}

type ProductStreamNode struct {
	CanonicalPath string `json:"canonicalPath"`
	Hop           int    `json:"hop"`
}

type ProductStreamExpansion struct {
	SeedPath  string                 `json:"seedPath"`
	Direction string                 `json:"direction"`
	MaxHops   int                    `json:"maxHops"`
	Nodes     []ProductStreamNode    `json:"nodes"`
	Edges     []ProductTransformPair `json:"edges"`
	Truncated bool                   `json:"truncated"`
	UsedRedis bool                   `json:"usedRedis"`
	UsedFs    bool                   `json:"usedFirestore"`
}

type Runtime struct {
	db       *firestore.Client
	redis    *redis.Client
	tenantID string

	synonymCollection            string
	transformCollection          string
	synonymCandidateCollection   string
	transformCandidateCollection string
	refreshInterval              time.Duration

	mu             sync.RWMutex
	refreshMu      sync.Mutex
	loaded         bool
	lastRefresh    time.Time
	synonymToCanon map[string]map[string]string
	knownPaths     map[string]map[string]struct{}
	transformKinds map[string]map[string]string
}

type ProductObservation struct {
	TenantID     string
	ObservedPath string
	ObservedName string
	SourceKind   string
	FragmentID   string
}

type TransformObservation struct {
	TenantID         string
	FromPath         string
	ToPath           string
	ObservedEdgeType models.DependencyType
	RelationHint     string
	SourceKind       string
	EdgeID           string
	SourceFragmentID string
	TargetFragmentID string
}

func NewRuntime(db *firestore.Client) *Runtime {
	return &Runtime{
		db:                           db,
		tenantID:                     "global",
		synonymCollection:            defaultSynonymCollection,
		transformCollection:          defaultTransformCollection,
		synonymCandidateCollection:   defaultSynonymCandidateCollection,
		transformCandidateCollection: defaultTransformCandidateCollection,
		refreshInterval:              5 * time.Minute,
		synonymToCanon:               map[string]map[string]string{},
		knownPaths:                   map[string]map[string]struct{}{},
		transformKinds:               map[string]map[string]string{},
	}
}

func (r *Runtime) WithRedis(client *redis.Client) *Runtime {
	r.redis = client
	return r
}

func (r *Runtime) WithTenantID(tenantID string) *Runtime {
	if v := strings.TrimSpace(tenantID); v != "" {
		r.tenantID = v
	}
	return r
}

func (r *Runtime) WithRefreshInterval(interval time.Duration) *Runtime {
	if interval > 0 {
		r.refreshInterval = interval
	}
	return r
}

func (r *Runtime) WithSynonymCollection(name string) *Runtime {
	if v := strings.TrimSpace(name); v != "" {
		r.synonymCollection = v
	}
	return r
}

func (r *Runtime) WithTransformCollection(name string) *Runtime {
	if v := strings.TrimSpace(name); v != "" {
		r.transformCollection = v
	}
	return r
}

func (r *Runtime) EnsureFresh(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	loaded := r.loaded
	stale := !loaded || (r.refreshInterval > 0 && time.Since(r.lastRefresh) >= r.refreshInterval)
	r.mu.RUnlock()
	if !stale {
		return nil
	}

	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()

	r.mu.RLock()
	loaded = r.loaded
	stale = !loaded || (r.refreshInterval > 0 && time.Since(r.lastRefresh) >= r.refreshInterval)
	r.mu.RUnlock()
	if !stale {
		return nil
	}

	if err := r.refreshFromFirestore(ctx); err != nil {
		if loaded {
			log.Printf("[ProductDictionary] WARNING: refresh from Firestore failed, keeping stale snapshot: %v", err)
			return nil
		}
		if rerr := r.loadFromRedis(ctx); rerr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (r *Runtime) CanonicalizeProductPath(ctx context.Context, path string) (string, bool, error) {
	return r.CanonicalizeProductPathForTenant(ctx, "", path)
}

func (r *Runtime) CanonicalizeProductPathForTenant(ctx context.Context, tenantID string, path string) (string, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") || r == nil {
		return path, false, nil
	}
	if err := r.EnsureFresh(ctx); err != nil {
		return path, false, err
	}
	effectiveTenant := r.resolveTenantID(ctx, tenantID)
	canonical, ok := r.lookupCanonicalPath(effectiveTenant, path)
	if !ok || canonical == "" || canonical == path {
		return path, false, nil
	}
	return canonical, true, nil
}

func (r *Runtime) IsKnownProductPath(ctx context.Context, path string) (bool, error) {
	return r.IsKnownProductPathForTenant(ctx, "", path)
}

func (r *Runtime) IsKnownProductPathForTenant(ctx context.Context, tenantID string, path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") || r == nil {
		return false, nil
	}
	if err := r.EnsureFresh(ctx); err != nil {
		return false, err
	}
	effectiveTenant := r.resolveTenantID(ctx, tenantID)
	_, ok := r.lookupKnownPath(effectiveTenant, path)
	return ok, nil
}

func (r *Runtime) ExpandProductStream(ctx context.Context, seedPath string, direction string, maxHops int, maxPerHop int) (*ProductStreamExpansion, error) {
	return r.ExpandProductStreamForTenant(ctx, "", seedPath, direction, maxHops, maxPerHop)
}

func (r *Runtime) ExpandProductStreamForTenant(ctx context.Context, tenantID string, seedPath string, direction string, maxHops int, maxPerHop int) (*ProductStreamExpansion, error) {
	if r == nil {
		return &ProductStreamExpansion{}, nil
	}
	seedPath = strings.TrimSpace(seedPath)
	if seedPath == "" || !strings.HasPrefix(seedPath, "/product/") {
		return nil, fmt.Errorf("seedPath must be a /product/ canonical path")
	}
	if maxHops <= 0 {
		maxHops = 1
	}
	if maxHops > 3 {
		maxHops = 3
	}
	if maxPerHop <= 0 {
		maxPerHop = 25
	}
	if maxPerHop > 200 {
		maxPerHop = 200
	}
	effectiveTenant := r.resolveTenantID(ctx, tenantID)
	canonicalSeed, _, err := r.CanonicalizeProductPathForTenant(ctx, effectiveTenant, seedPath)
	if err != nil {
		return nil, err
	}
	dir, err := normalizeStreamDirection(direction)
	if err != nil {
		return nil, err
	}

	result := &ProductStreamExpansion{
		SeedPath:  canonicalSeed,
		Direction: string(dir),
		MaxHops:   maxHops,
		Nodes:     []ProductStreamNode{{CanonicalPath: canonicalSeed, Hop: 0}},
		Edges:     []ProductTransformPair{},
	}

	type queueItem struct {
		path string
		hop  int
	}
	queue := []queueItem{{path: canonicalSeed, hop: 0}}
	visitedNode := map[string]struct{}{canonicalSeed: {}}
	visitedEdge := map[string]struct{}{}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item.hop >= maxHops {
			continue
		}
		adj, usedRedis, usedFs, err := r.listTransformAdjacency(ctx, effectiveTenant, item.path, dir)
		if err != nil {
			return nil, err
		}
		result.UsedRedis = result.UsedRedis || usedRedis
		result.UsedFs = result.UsedFs || usedFs
		if len(adj) > maxPerHop {
			adj = adj[:maxPerHop]
			result.Truncated = true
		}
		for _, edge := range adj {
			edgeKey := transformKey(edge.FromPath, edge.ToPath)
			if _, ok := visitedEdge[edgeKey]; !ok {
				visitedEdge[edgeKey] = struct{}{}
				result.Edges = append(result.Edges, edge)
			}
			nextPath := strings.TrimSpace(edge.ToPath)
			if dir == StreamDirectionUpstream {
				nextPath = strings.TrimSpace(edge.FromPath)
			}
			if nextPath == "" {
				continue
			}
			if _, ok := visitedNode[nextPath]; ok {
				continue
			}
			visitedNode[nextPath] = struct{}{}
			nextHop := item.hop + 1
			result.Nodes = append(result.Nodes, ProductStreamNode{
				CanonicalPath: nextPath,
				Hop:           nextHop,
			})
			queue = append(queue, queueItem{path: nextPath, hop: nextHop})
		}
	}

	sort.Slice(result.Nodes, func(i, j int) bool {
		if result.Nodes[i].Hop != result.Nodes[j].Hop {
			return result.Nodes[i].Hop < result.Nodes[j].Hop
		}
		return result.Nodes[i].CanonicalPath < result.Nodes[j].CanonicalPath
	})
	sort.Slice(result.Edges, func(i, j int) bool {
		if result.Edges[i].FromPath != result.Edges[j].FromPath {
			return result.Edges[i].FromPath < result.Edges[j].FromPath
		}
		if result.Edges[i].ToPath != result.Edges[j].ToPath {
			return result.Edges[i].ToPath < result.Edges[j].ToPath
		}
		return result.Edges[i].RelationKind < result.Edges[j].RelationKind
	})
	return result, nil
}

func (r *Runtime) NormalizeProductEdgeType(
	ctx context.Context,
	edgeType models.DependencyType,
	sourceType models.FragmentType,
	targetType models.FragmentType,
	sourcePath string,
	targetPath string,
) (models.DependencyType, bool, string, bool, error) {
	return r.NormalizeProductEdgeTypeForTenant(ctx, "", edgeType, sourceType, targetType, sourcePath, targetPath)
}

func (r *Runtime) NormalizeProductEdgeTypeForTenant(
	ctx context.Context,
	tenantID string,
	edgeType models.DependencyType,
	sourceType models.FragmentType,
	targetType models.FragmentType,
	sourcePath string,
	targetPath string,
) (models.DependencyType, bool, string, bool, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	if r == nil || sourceType != models.FragmentTypeProduct || targetType != models.FragmentTypeProduct {
		return edgeType, false, "", false, nil
	}
	if !supportsProductRelationDictionary(edgeType) {
		return edgeType, false, "", false, nil
	}
	if err := r.EnsureFresh(ctx); err != nil {
		return edgeType, false, "", false, err
	}
	effectiveTenant := r.resolveTenantID(ctx, tenantID)
	fromPath, _, err := r.CanonicalizeProductPathForTenant(ctx, effectiveTenant, sourcePath)
	if err != nil {
		return edgeType, false, "", false, err
	}
	toPath, _, err := r.CanonicalizeProductPathForTenant(ctx, effectiveTenant, targetPath)
	if err != nil {
		return edgeType, false, "", false, err
	}
	relationKind, ok := r.lookupTransformKind(effectiveTenant, fromPath, toPath)
	if !ok {
		return edgeType, false, "", false, nil
	}
	normalized := desiredEdgeTypeForKind(relationKind)
	return normalized, true, relationKind, normalized != edgeType, nil
}

func (r *Runtime) listTransformAdjacency(ctx context.Context, tenantID string, path string, direction StreamDirection) ([]ProductTransformPair, bool, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false, false, nil
	}
	if err := r.EnsureFresh(ctx); err != nil {
		return nil, false, false, err
	}
	if edges, ok := r.getTransformAdjacencyFromRedis(ctx, tenantID, path, direction); ok {
		return edges, true, false, nil
	}
	edges, err := r.listTransformAdjacencyFromFirestore(ctx, tenantID, path, direction)
	if err == nil && r.db != nil {
		r.persistTransformAdjacencyToRedis(ctx, tenantID, path, direction, edges)
		return edges, false, true, nil
	}
	// Fail-open fallback to the in-memory snapshot already loaded by EnsureFresh.
	inMemory := r.listTransformAdjacencyFromSnapshot(tenantID, path, direction)
	if len(inMemory) > 0 {
		return inMemory, false, false, nil
	}
	return nil, false, true, err
}

func (r *Runtime) listTransformAdjacencyFromSnapshot(tenantID string, path string, direction StreamDirection) []ProductTransformPair {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProductTransformPair, 0, 8)
	merged := r.mergedTransformKindsLocked(tenantID)
	for rawKey, relationKind := range merged {
		parts := strings.SplitN(rawKey, "\n", 2)
		if len(parts) != 2 {
			continue
		}
		fromPath := strings.TrimSpace(parts[0])
		toPath := strings.TrimSpace(parts[1])
		switch direction {
		case StreamDirectionDownstream:
			if fromPath != path {
				continue
			}
		case StreamDirectionUpstream:
			if toPath != path {
				continue
			}
		}
		out = append(out, ProductTransformPair{
			FromPath:     fromPath,
			ToPath:       toPath,
			RelationKind: normalizeRelationKind(relationKind),
		})
	}
	sortProductTransformPairs(out)
	return out
}

func (r *Runtime) listTransformAdjacencyFromFirestore(ctx context.Context, tenantID string, path string, direction StreamDirection) ([]ProductTransformPair, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	field := "fromPath"
	if direction == StreamDirectionUpstream {
		field = "toPath"
	}
	tenantID = firstNonEmpty(strings.TrimSpace(tenantID), "global")
	tenantDocs, err := r.queryTransformAdjacencyDocs(ctx, tenantID, field, path)
	if err != nil {
		return nil, err
	}
	docs := tenantDocs
	if tenantID != "global" {
		globalDocs, gErr := r.queryTransformAdjacencyDocs(ctx, "global", field, path)
		if gErr != nil {
			return nil, gErr
		}
		docs = append(docs, globalDocs...)
	}
	out := make([]ProductTransformPair, 0, len(docs))
	seen := map[string]struct{}{}
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		data := doc.Data()
		fromPath := strings.TrimSpace(stringValue(data["fromPath"]))
		toPath := strings.TrimSpace(stringValue(data["toPath"]))
		if fromPath == "" || toPath == "" {
			continue
		}
		pair := ProductTransformPair{
			FromPath:     fromPath,
			ToPath:       toPath,
			RelationKind: normalizeRelationKind(stringValue(data["relationKind"])),
		}
		key := transformKey(pair.FromPath, pair.ToPath)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pair)
	}
	sortProductTransformPairs(out)
	return out, nil
}

func (r *Runtime) RecordUnknownProductCandidate(ctx context.Context, obs ProductObservation) error {
	if r == nil || r.db == nil {
		return nil
	}
	path := strings.TrimSpace(obs.ObservedPath)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return nil
	}
	if canonical, matched, err := r.CanonicalizeProductPathForTenant(ctx, strings.TrimSpace(obs.TenantID), path); err == nil {
		if matched || canonical != path {
			return nil
		}
	}
	if known, err := r.IsKnownProductPathForTenant(ctx, strings.TrimSpace(obs.TenantID), path); err == nil && known {
		return nil
	}
	tenantID := firstNonEmpty(strings.TrimSpace(obs.TenantID), r.resolveTenantID(ctx, ""), "global")
	docID := stableDocID("product_candidate", tenantID, path)
	doc := r.db.Collection(strings.TrimSpace(r.synonymCandidateCollection)).Doc(docID)
	now := firestore.ServerTimestamp
	aliasKey := normalizeAliasKey(firstNonEmpty(strings.TrimSpace(obs.ObservedName), lastPathSegment(path)))
	sourceKind := strings.TrimSpace(obs.SourceKind)
	fragmentID := strings.TrimSpace(obs.FragmentID)

	return r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(doc)
		exists := err == nil && snap != nil && snap.Exists()
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "notfound") {
			return err
		}
		update := map[string]interface{}{
			"tenantId":         tenantID,
			"observedPath":     path,
			"observedName":     strings.TrimSpace(obs.ObservedName),
			"observedAliasKey": aliasKey,
			"status":           "new",
			"active":           true,
			"evidenceCount":    firestore.Increment(1),
			"updatedAt":        now,
			"lastSeenAt":       now,
		}
		if sourceKind != "" {
			update["sourceKinds"] = firestore.ArrayUnion(sourceKind)
		}
		if fragmentID != "" {
			update["exampleFragmentIds"] = firestore.ArrayUnion(fragmentID)
		}
		if !exists {
			update["createdAt"] = now
			update["firstSeenAt"] = now
		}
		tx.Set(doc, update, firestore.MergeAll)
		return nil
	})
}

func (r *Runtime) RecordUnknownTransformCandidate(ctx context.Context, obs TransformObservation) error {
	if r == nil || r.db == nil {
		return nil
	}
	tenantID := firstNonEmpty(strings.TrimSpace(obs.TenantID), r.resolveTenantID(ctx, ""), "global")
	fromPath := strings.TrimSpace(obs.FromPath)
	toPath := strings.TrimSpace(obs.ToPath)
	if fromPath == "" || toPath == "" || fromPath == toPath {
		return nil
	}
	if !strings.HasPrefix(fromPath, "/product/") || !strings.HasPrefix(toPath, "/product/") {
		return nil
	}
	var err error
	fromPath, _, err = r.CanonicalizeProductPathForTenant(ctx, tenantID, fromPath)
	if err != nil {
		return err
	}
	toPath, _, err = r.CanonicalizeProductPathForTenant(ctx, tenantID, toPath)
	if err != nil {
		return err
	}
	if err := r.EnsureFresh(ctx); err != nil {
		return err
	}
	_, known := r.lookupTransformKind(tenantID, fromPath, toPath)
	if known {
		return nil
	}
	docID := stableDocID("transform_candidate", tenantID, fromPath, toPath)
	doc := r.db.Collection(strings.TrimSpace(r.transformCandidateCollection)).Doc(docID)
	now := firestore.ServerTimestamp
	sourceKind := strings.TrimSpace(obs.SourceKind)
	edgeID := strings.TrimSpace(obs.EdgeID)
	sourceFragmentID := strings.TrimSpace(obs.SourceFragmentID)
	targetFragmentID := strings.TrimSpace(obs.TargetFragmentID)
	relationHint := strings.TrimSpace(obs.RelationHint)
	if relationHint == "" {
		relationHint = relationHintForEdgeType(obs.ObservedEdgeType)
	}

	return r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(doc)
		exists := err == nil && snap != nil && snap.Exists()
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "notfound") {
			return err
		}
		update := map[string]interface{}{
			"tenantId":      tenantID,
			"fromPath":      fromPath,
			"toPath":        toPath,
			"status":        "new",
			"active":        true,
			"evidenceCount": firestore.Increment(1),
			"updatedAt":     now,
			"lastSeenAt":    now,
			"relationHint":  relationHint,
		}
		if sourceKind != "" {
			update["sourceKinds"] = firestore.ArrayUnion(sourceKind)
		}
		if edgeID != "" {
			update["exampleEdgeIds"] = firestore.ArrayUnion(edgeID)
		}
		if sourceFragmentID != "" {
			update["exampleSourceFragmentIds"] = firestore.ArrayUnion(sourceFragmentID)
		}
		if targetFragmentID != "" {
			update["exampleTargetFragmentIds"] = firestore.ArrayUnion(targetFragmentID)
		}
		if obs.ObservedEdgeType != "" {
			update["observedEdgeTypes"] = firestore.ArrayUnion(string(obs.ObservedEdgeType))
		}
		if !exists {
			update["createdAt"] = now
			update["firstSeenAt"] = now
		}
		tx.Set(doc, update, firestore.MergeAll)
		return nil
	})
}

func (r *Runtime) SyncSynonymPairWrite(ctx context.Context, tenantID string, canonicalPath string, synonymPath string, active bool) error {
	if r == nil {
		return nil
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	synonymPath = strings.TrimSpace(synonymPath)
	if canonicalPath == "" || synonymPath == "" || !strings.HasPrefix(canonicalPath, "/product/") || !strings.HasPrefix(synonymPath, "/product/") {
		return nil
	}
	if err := r.EnsureFresh(ctx); err != nil {
		return err
	}
	effectiveTenant := r.resolveTenantID(ctx, tenantID)

	r.mu.Lock()
	tenantSynonyms := ensureTenantStringMap(r.synonymToCanon, effectiveTenant)
	tenantKnown := ensureTenantSet(r.knownPaths, effectiveTenant)
	if active {
		tenantSynonyms[synonymPath] = canonicalPath
		tenantKnown[canonicalPath] = struct{}{}
		tenantKnown[synonymPath] = struct{}{}
	} else {
		delete(tenantSynonyms, synonymPath)
		if !r.pathReferencedInSynonymsLocked(effectiveTenant, synonymPath) {
			delete(tenantKnown, synonymPath)
		}
	}
	r.recanonicalizeTransformKindsLocked()
	synonymSnapshot := cloneTenantStringMap(r.synonymToCanon)
	transformSnapshot := cloneTenantStringMap(r.transformKinds)
	r.loaded = true
	r.lastRefresh = time.Now()
	r.mu.Unlock()

	r.persistToRedis(ctx, synonymSnapshot, transformSnapshot)
	r.refreshAdjacencyForPaths(ctx, effectiveTenant, canonicalPath, synonymPath)
	return nil
}

func (r *Runtime) SyncTransformPairWrite(ctx context.Context, tenantID string, fromPath string, toPath string, relationKind string, active bool) error {
	if r == nil {
		return nil
	}
	fromPath = strings.TrimSpace(fromPath)
	toPath = strings.TrimSpace(toPath)
	if fromPath == "" || toPath == "" || !strings.HasPrefix(fromPath, "/product/") || !strings.HasPrefix(toPath, "/product/") {
		return nil
	}
	if err := r.EnsureFresh(ctx); err != nil {
		return err
	}
	effectiveTenant := r.resolveTenantID(ctx, tenantID)

	r.mu.Lock()
	fromCanonical := r.lookupCanonicalPathLocked(effectiveTenant, fromPath)
	toCanonical := r.lookupCanonicalPathLocked(effectiveTenant, toPath)
	tenantKinds := ensureTenantStringMap(r.transformKinds, effectiveTenant)
	key := transformKey(firstNonEmpty(fromCanonical, fromPath), firstNonEmpty(toCanonical, toPath))
	if active {
		tenantKinds[key] = normalizeRelationKind(relationKind)
	} else {
		delete(tenantKinds, key)
	}
	synonymSnapshot := cloneTenantStringMap(r.synonymToCanon)
	transformSnapshot := cloneTenantStringMap(r.transformKinds)
	r.loaded = true
	r.lastRefresh = time.Now()
	r.mu.Unlock()

	r.persistToRedis(ctx, synonymSnapshot, transformSnapshot)
	r.refreshAdjacencyForPaths(ctx, effectiveTenant, firstNonEmpty(fromCanonical, fromPath), firstNonEmpty(toCanonical, toPath))
	return nil
}

func (r *Runtime) NormalizeArtifact(ctx context.Context, artifact *models.MaterializedArtifact) (bool, error) {
	if r == nil || artifact == nil {
		return false, nil
	}
	if err := r.EnsureFresh(ctx); err != nil {
		return false, err
	}
	replacements := map[string]string{}
	addReplacement := func(path string) error {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasPrefix(path, "/product/") {
			return nil
		}
		canonical, matched, err := r.CanonicalizeProductPathForTenant(ctx, strings.TrimSpace(artifact.TenantID), path)
		if err != nil {
			return err
		}
		if matched && canonical != "" && canonical != path {
			replacements[path] = canonical
		}
		return nil
	}
	for _, ref := range artifact.NodeRefs {
		if err := addReplacement(ref); err != nil {
			return false, err
		}
	}
	if err := addReplacement(artifact.ProductAxis); err != nil {
		return false, err
	}
	if artifact.Labels != nil {
		if err := addReplacement(artifact.Labels["productAxis"]); err != nil {
			return false, err
		}
		if err := addReplacement(artifact.Labels["anchorProduct"]); err != nil {
			return false, err
		}
	}
	if len(replacements) == 0 {
		return false, nil
	}
	keys := make([]string, 0, len(replacements))
	for oldPath := range replacements {
		keys = append(keys, oldPath)
	}
	sort.Strings(keys)
	changed := false
	for _, oldPath := range keys {
		if models.ReplaceCanonicalPathInArtifact(artifact, oldPath, replacements[oldPath]) {
			changed = true
		}
	}
	if changed {
		models.PopulateMaterializedArtifactIndexes(artifact)
	}
	return changed, nil
}

func (r *Runtime) refreshFromFirestore(ctx context.Context) error {
	if r == nil || r.db == nil {
		return nil
	}
	synonymToCanon := map[string]map[string]string{}
	knownPaths := map[string]map[string]struct{}{}
	transformKinds := map[string]map[string]string{}

	synIter := r.db.Collection(strings.TrimSpace(r.synonymCollection)).Documents(ctx)
	defer synIter.Stop()
	for {
		doc, err := synIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		tenantID := firstNonEmpty(strings.TrimSpace(stringValue(data["tenantId"])), "global")
		if !boolValue(data["active"], true) {
			continue
		}
		canonicalPath := strings.TrimSpace(stringValue(data["canonicalPath"]))
		synonymPath := strings.TrimSpace(stringValue(data["synonymPath"]))
		if canonicalPath == "" {
			canonicalPath = strings.TrimSpace(stringValue(data["canonicalCanonicalPath"]))
		}
		if canonicalPath == "" || synonymPath == "" {
			continue
		}
		ensureTenantStringMap(synonymToCanon, tenantID)[synonymPath] = canonicalPath
		ensureTenantSet(knownPaths, tenantID)[canonicalPath] = struct{}{}
		ensureTenantSet(knownPaths, tenantID)[synonymPath] = struct{}{}
	}

	trIter := r.db.Collection(strings.TrimSpace(r.transformCollection)).Documents(ctx)
	defer trIter.Stop()
	for {
		doc, err := trIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		tenantID := firstNonEmpty(strings.TrimSpace(stringValue(data["tenantId"])), "global")
		if !boolValue(data["active"], true) {
			continue
		}
		fromPath := strings.TrimSpace(stringValue(data["fromPath"]))
		toPath := strings.TrimSpace(stringValue(data["toPath"]))
		if fromPath == "" || toPath == "" {
			continue
		}
		synonyms := mergedTenantStringMap(synonymToCanon, tenantID)
		fromCanonical := firstNonEmpty(synonyms[fromPath], fromPath)
		toCanonical := firstNonEmpty(synonyms[toPath], toPath)
		ensureTenantStringMap(transformKinds, tenantID)[transformKey(fromCanonical, toCanonical)] = normalizeRelationKind(stringValue(data["relationKind"]))
	}

	r.mu.Lock()
	r.synonymToCanon = synonymToCanon
	r.knownPaths = knownPaths
	r.transformKinds = transformKinds
	r.loaded = true
	r.lastRefresh = time.Now()
	r.mu.Unlock()

	r.persistToRedis(ctx, synonymToCanon, transformKinds)
	return nil
}

func (r *Runtime) persistToRedis(ctx context.Context, synonymToCanon map[string]map[string]string, transformKinds map[string]map[string]string) {
	if r == nil || r.redis == nil {
		return
	}
	if blob, err := json.Marshal(synonymToCanon); err == nil {
		_ = r.redis.Set(ctx, redisSynonymsSnapshotKey, blob, 24*time.Hour).Err()
	}
	if blob, err := json.Marshal(transformKinds); err == nil {
		_ = r.redis.Set(ctx, redisTransformsSnapshotKey, blob, 24*time.Hour).Err()
	}
	adjacency := buildTransformAdjacency(transformKinds)
	for cacheKey, edges := range adjacency {
		if blob, err := json.Marshal(edges); err == nil {
			_ = r.redis.Set(ctx, cacheKey, blob, 30*time.Minute).Err()
		}
	}
}

func (r *Runtime) loadFromRedis(ctx context.Context) error {
	if r == nil || r.redis == nil {
		return fmt.Errorf("redis not configured")
	}
	synBlob, err := r.redis.Get(ctx, redisSynonymsSnapshotKey).Bytes()
	if err != nil {
		return err
	}
	trBlob, err := r.redis.Get(ctx, redisTransformsSnapshotKey).Bytes()
	if err != nil {
		return err
	}
	synonymToCanon := map[string]map[string]string{}
	if err := json.Unmarshal(synBlob, &synonymToCanon); err != nil {
		return err
	}
	transformKinds := map[string]map[string]string{}
	if err := json.Unmarshal(trBlob, &transformKinds); err != nil {
		return err
	}
	knownPaths := map[string]map[string]struct{}{}
	for tenantID, mappings := range synonymToCanon {
		for synonymPath, canonicalPath := range mappings {
			if synonymPath != "" {
				ensureTenantSet(knownPaths, tenantID)[synonymPath] = struct{}{}
			}
			if canonicalPath != "" {
				ensureTenantSet(knownPaths, tenantID)[canonicalPath] = struct{}{}
			}
		}
	}
	r.mu.Lock()
	r.synonymToCanon = synonymToCanon
	r.knownPaths = knownPaths
	r.transformKinds = transformKinds
	r.loaded = true
	r.lastRefresh = time.Now()
	r.mu.Unlock()
	return nil
}

func (r *Runtime) getTransformAdjacencyFromRedis(ctx context.Context, tenantID string, path string, direction StreamDirection) ([]ProductTransformPair, bool) {
	if r == nil || r.redis == nil {
		return nil, false
	}
	blob, err := r.redis.Get(ctx, transformAdjacencyRedisKey(tenantID, direction, path)).Bytes()
	if err != nil || len(blob) == 0 {
		return nil, false
	}
	var out []ProductTransformPair
	if err := json.Unmarshal(blob, &out); err != nil {
		return nil, false
	}
	sortProductTransformPairs(out)
	return out, true
}

func (r *Runtime) persistTransformAdjacencyToRedis(ctx context.Context, tenantID string, path string, direction StreamDirection, edges []ProductTransformPair) {
	if r == nil || r.redis == nil {
		return
	}
	blob, err := json.Marshal(edges)
	if err != nil {
		return
	}
	_ = r.redis.Set(ctx, transformAdjacencyRedisKey(tenantID, direction, path), blob, 30*time.Minute).Err()
}

func (r *Runtime) refreshAdjacencyForPaths(ctx context.Context, tenantID string, paths ...string) {
	if r == nil || r.redis == nil {
		return
	}
	seen := map[string]struct{}{}
	for _, rawPath := range paths {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		for _, direction := range []StreamDirection{StreamDirectionDownstream, StreamDirectionUpstream} {
			r.invalidateTransformAdjacencyKeys(ctx, tenantID, direction, path)
			edges := r.listTransformAdjacencyFromSnapshot(tenantID, path, direction)
			if len(edges) == 0 {
				continue
			}
			r.persistTransformAdjacencyToRedis(ctx, tenantID, path, direction, edges)
		}
	}
}

func (r *Runtime) invalidateTransformAdjacencyKeys(ctx context.Context, tenantID string, direction StreamDirection, path string) {
	if r == nil || r.redis == nil {
		return
	}
	hash := stableDocID("path", strings.TrimSpace(path))
	pattern := redisTransformAdjPrefix + "*:" + string(direction) + ":" + hash
	if strings.TrimSpace(tenantID) != "global" {
		pattern = transformAdjacencyRedisKey(tenantID, direction, path)
	}
	iter := r.redis.Scan(ctx, 0, pattern, 200)
	keys, _, err := iter.Result()
	if err != nil || len(keys) == 0 {
		return
	}
	_ = r.redis.Del(ctx, keys...).Err()
}

func (r *Runtime) resolveTenantID(ctx context.Context, tenantID string) string {
	if v := strings.TrimSpace(tenantID); v != "" {
		return v
	}
	if rtc, ok := rtctx.Get(ctx); ok {
		if v := strings.TrimSpace(rtc.TenantID); v != "" {
			return v
		}
	}
	return firstNonEmpty(r.tenantID, "global")
}

func (r *Runtime) lookupCanonicalPathLocked(tenantID, path string) string {
	merged := mergedTenantStringMap(r.synonymToCanon, tenantID)
	return merged[strings.TrimSpace(path)]
}

func (r *Runtime) lookupCanonicalPath(tenantID, path string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value := r.lookupCanonicalPathLocked(tenantID, path)
	_, ok := mergedTenantStringMap(r.synonymToCanon, tenantID)[strings.TrimSpace(path)]
	return value, ok
}

func (r *Runtime) lookupKnownPath(tenantID, path string) (struct{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	merged := mergedTenantSetMap(r.knownPaths, tenantID)
	value, ok := merged[strings.TrimSpace(path)]
	return value, ok
}

func (r *Runtime) lookupTransformKind(tenantID, fromPath, toPath string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	merged := r.mergedTransformKindsLocked(tenantID)
	value, ok := merged[transformKey(fromPath, toPath)]
	return value, ok
}

func (r *Runtime) mergedTransformKindsLocked(tenantID string) map[string]string {
	return mergedTenantStringMap(r.transformKinds, tenantID)
}

func (r *Runtime) recanonicalizeTransformKindsLocked() {
	rebuilt := map[string]map[string]string{}
	for tenantID, tenantKinds := range r.transformKinds {
		dst := ensureTenantStringMap(rebuilt, tenantID)
		for rawKey, relationKind := range tenantKinds {
			parts := strings.SplitN(rawKey, "\n", 2)
			if len(parts) != 2 {
				continue
			}
			fromPath := strings.TrimSpace(parts[0])
			toPath := strings.TrimSpace(parts[1])
			if fromCanonical := r.lookupCanonicalPathLocked(tenantID, fromPath); fromCanonical != "" {
				fromPath = fromCanonical
			}
			if toCanonical := r.lookupCanonicalPathLocked(tenantID, toPath); toCanonical != "" {
				toPath = toCanonical
			}
			if fromPath == "" || toPath == "" {
				continue
			}
			dst[transformKey(fromPath, toPath)] = normalizeRelationKind(relationKind)
		}
	}
	r.transformKinds = rebuilt
}

func (r *Runtime) pathReferencedInSynonymsLocked(tenantID string, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if mappings, ok := r.synonymToCanon[tenantID]; ok {
		for synonymPath, canonicalPath := range mappings {
			if strings.TrimSpace(synonymPath) == path || strings.TrimSpace(canonicalPath) == path {
				return true
			}
		}
	}
	return false
}

func (r *Runtime) queryTransformAdjacencyDocs(ctx context.Context, tenantID, field, path string) ([]*firestore.DocumentSnapshot, error) {
	return r.db.Collection(strings.TrimSpace(r.transformCollection)).
		Where("tenantId", "==", firstNonEmpty(strings.TrimSpace(tenantID), "global")).
		Where("active", "==", true).
		Where(strings.TrimSpace(field), "==", strings.TrimSpace(path)).
		Documents(ctx).
		GetAll()
}

func supportsProductRelationDictionary(edgeType models.DependencyType) bool {
	switch edgeType {
	case models.DependencyTypeTransformsInto, models.DependencyTypeAssemblesInto, models.DependencyTypeDependsOn:
		return true
	default:
		return false
	}
}

func desiredEdgeTypeForKind(kind string) models.DependencyType {
	if normalizeRelationKind(kind) == "assembly" {
		return models.DependencyTypeAssemblesInto
	}
	return models.DependencyTypeTransformsInto
}

func relationHintForEdgeType(edgeType models.DependencyType) string {
	switch edgeType {
	case models.DependencyTypeAssemblesInto:
		return "assembly"
	case models.DependencyTypeTransformsInto:
		return "transformation"
	default:
		return ""
	}
}

func normalizeRelationKind(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "assembly" {
		return "assembly"
	}
	return "transformation"
}

func normalizeStreamDirection(v string) (StreamDirection, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", string(StreamDirectionDownstream):
		return StreamDirectionDownstream, nil
	case string(StreamDirectionUpstream):
		return StreamDirectionUpstream, nil
	default:
		return StreamDirectionDownstream, fmt.Errorf("direction must be upstream or downstream")
	}
}

func buildTransformAdjacency(transformKinds map[string]map[string]string) map[string][]ProductTransformPair {
	out := map[string][]ProductTransformPair{}
	for tenantID, tenantKinds := range transformKinds {
		for rawKey, relationKind := range tenantKinds {
			parts := strings.SplitN(rawKey, "\n", 2)
			if len(parts) != 2 {
				continue
			}
			pair := ProductTransformPair{
				FromPath:     strings.TrimSpace(parts[0]),
				ToPath:       strings.TrimSpace(parts[1]),
				RelationKind: normalizeRelationKind(relationKind),
			}
			if pair.FromPath == "" || pair.ToPath == "" {
				continue
			}
			downKey := transformAdjacencyRedisKey(tenantID, StreamDirectionDownstream, pair.FromPath)
			upKey := transformAdjacencyRedisKey(tenantID, StreamDirectionUpstream, pair.ToPath)
			out[downKey] = append(out[downKey], pair)
			out[upKey] = append(out[upKey], pair)
		}
	}
	for cacheKey := range out {
		sortProductTransformPairs(out[cacheKey])
	}
	return out
}

func cloneTenantStringMap(src map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(src))
	for tenantID, tenantMap := range src {
		cp := make(map[string]string, len(tenantMap))
		for key, value := range tenantMap {
			cp[key] = value
		}
		out[tenantID] = cp
	}
	return out
}

func transformAdjacencyRedisKey(tenantID string, direction StreamDirection, path string) string {
	return redisTransformAdjPrefix + firstNonEmpty(strings.TrimSpace(tenantID), "global") + ":" + string(direction) + ":" + stableDocID("path", strings.TrimSpace(path))
}

func sortProductTransformPairs(edges []ProductTransformPair) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromPath != edges[j].FromPath {
			return edges[i].FromPath < edges[j].FromPath
		}
		if edges[i].ToPath != edges[j].ToPath {
			return edges[i].ToPath < edges[j].ToPath
		}
		return edges[i].RelationKind < edges[j].RelationKind
	})
}

func transformKey(fromPath, toPath string) string {
	return strings.TrimSpace(fromPath) + "\n" + strings.TrimSpace(toPath)
}

func stableDocID(prefix string, parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return prefix + "_" + hex.EncodeToString(sum[:10])
}

func stringValue(v interface{}) string {
	return strings.TrimSpace(fmt.Sprint(v))
}

func boolValue(v interface{}, defaultValue bool) bool {
	if v == nil {
		return defaultValue
	}
	if b, ok := v.(bool); ok {
		return b
	}
	s := strings.TrimSpace(strings.ToLower(fmt.Sprint(v)))
	switch s {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return defaultValue
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return ""
}

func lastPathSegment(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func normalizeAliasKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	prevSpace := false
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevSpace = false
		default:
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func ensureTenantStringMap(dst map[string]map[string]string, tenantID string) map[string]string {
	tenantID = firstNonEmpty(strings.TrimSpace(tenantID), "global")
	if dst[tenantID] == nil {
		dst[tenantID] = map[string]string{}
	}
	return dst[tenantID]
}

func ensureTenantSet(dst map[string]map[string]struct{}, tenantID string) map[string]struct{} {
	tenantID = firstNonEmpty(strings.TrimSpace(tenantID), "global")
	if dst[tenantID] == nil {
		dst[tenantID] = map[string]struct{}{}
	}
	return dst[tenantID]
}

func mergedTenantStringMap(src map[string]map[string]string, tenantID string) map[string]string {
	out := map[string]string{}
	for k, v := range src["global"] {
		out[k] = v
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" && tenantID != "global" {
		for k, v := range src[tenantID] {
			out[k] = v
		}
	}
	return out
}

func mergedTenantSetMap(src map[string]map[string]struct{}, tenantID string) map[string]struct{} {
	out := map[string]struct{}{}
	for k := range src["global"] {
		out[k] = struct{}{}
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" && tenantID != "global" {
		for k := range src[tenantID] {
			out[k] = struct{}{}
		}
	}
	return out
}

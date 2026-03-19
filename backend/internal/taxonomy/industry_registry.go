package taxonomy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

const (
	TaxonomyNamespaceIndustry    = "industry"
	TaxonomyAliasKeyKindLabel    = "label"
	TaxonomyAliasKeyKindAbbrev   = "abbreviation"
	TaxonomyAliasKeyKindWikidata = "wikidata_qid"
	industryNodeStatusActive     = "active"
	taxonomyAliasStatusActive    = "active"
)

var allowedIndustryFragmentTypes = setOf("ORGANIZATION", "FACILITY", "PRODUCT")

type IndustryNode struct {
	Path            string
	ParentPath      string
	Level           int
	Label           string
	Status          string
	Definition      string
	AllowedForTypes map[string]struct{}
}

type TaxonomyAlias struct {
	Namespace     string
	KeyKind       string
	KeyNorm       string
	CanonicalPath string
	Status        string
	Lang          string
	Source        string
	Comment       string
}

type IndustryRegistry struct {
	Version   string
	Nodes     map[string]IndustryNode
	Children  map[string][]string
	Aliases   map[string]map[string]string
	Ambiguous map[string]map[string]struct{}
}

var industryRegistryMu sync.RWMutex
var industryRegistry *IndustryRegistry

func SetIndustryRegistry(r *IndustryRegistry) {
	industryRegistryMu.Lock()
	defer industryRegistryMu.Unlock()
	industryRegistry = r
}

func GetIndustryRegistry() *IndustryRegistry {
	industryRegistryMu.RLock()
	defer industryRegistryMu.RUnlock()
	return industryRegistry
}

func normalizeIndustryKey(kind, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case TaxonomyAliasKeyKindWikidata:
		raw = strings.ToUpper(raw)
		if strings.HasPrefix(raw, "HTTP://") || strings.HasPrefix(raw, "HTTPS://") {
			if idx := strings.LastIndex(raw, "/"); idx >= 0 && idx+1 < len(raw) {
				raw = raw[idx+1:]
			}
		}
		if strings.HasPrefix(raw, "Q") {
			return raw
		}
		return ""
	default:
		raw = strings.ToLower(raw)
		raw = strings.ReplaceAll(raw, "&", " and ")
		raw = strings.ReplaceAll(raw, "_", " ")
		raw = strings.ReplaceAll(raw, "/", " ")
		raw = reProductNonSlugChars.ReplaceAllString(raw, " ")
		return strings.Join(strings.Fields(raw), " ")
	}
}

func normalizeIndustryPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/industry/") {
		return ""
	}
	path = strings.ToLower(path)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i == 0 {
			if part != TaxonomyNamespaceIndustry {
				return ""
			}
			out = append(out, part)
			continue
		}
		part = Slugify(part)
		if part == "" {
			return ""
		}
		out = append(out, part)
	}
	if len(out) < 2 {
		return ""
	}
	return "/" + strings.Join(out, "/")
}

func isIndustryFragmentTypeAllowed(fragmentType string) bool {
	fragmentType = strings.ToUpper(strings.TrimSpace(fragmentType))
	_, ok := allowedIndustryFragmentTypes[fragmentType]
	return ok
}

func ResolveIndustryKey(namespace, keyKind, raw string) (string, bool) {
	namespace = strings.ToLower(strings.TrimSpace(namespace))
	if namespace != TaxonomyNamespaceIndustry {
		return "", false
	}
	keyKind = strings.ToLower(strings.TrimSpace(keyKind))
	keyNorm := normalizeIndustryKey(keyKind, raw)
	if keyNorm == "" {
		return "", false
	}
	reg := GetIndustryRegistry()
	if reg == nil {
		return "", false
	}
	kindAliases := reg.Aliases[keyKind]
	if len(kindAliases) == 0 {
		return "", false
	}
	if ambiguous := reg.Ambiguous[keyKind]; len(ambiguous) > 0 {
		if _, ok := ambiguous[keyNorm]; ok {
			return "", false
		}
	}
	path, ok := kindAliases[keyNorm]
	if !ok {
		return "", false
	}
	return path, true
}

func ValidateIndustryPath(path string, fragmentType string) error {
	path = normalizeIndustryPath(path)
	if path == "" {
		return fmt.Errorf("invalid industry path")
	}
	reg := GetIndustryRegistry()
	if reg == nil {
		return fmt.Errorf("industry registry not loaded")
	}
	node, ok := reg.Nodes[path]
	if !ok {
		return fmt.Errorf("unknown industry path: %s", path)
	}
	if !isIndustryFragmentTypeAllowed(fragmentType) {
		return fmt.Errorf("fragment type %s cannot carry industryPaths", strings.TrimSpace(fragmentType))
	}
	if len(node.AllowedForTypes) > 0 {
		if _, ok := node.AllowedForTypes[strings.ToUpper(strings.TrimSpace(fragmentType))]; !ok {
			return fmt.Errorf("industry path %s is not allowed for fragment type %s", path, strings.TrimSpace(fragmentType))
		}
	}
	return nil
}

func ExpandIndustryAncestors(path string) []string {
	path = normalizeIndustryPath(path)
	if path == "" {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return nil
	}
	out := make([]string, 0, len(parts)-1)
	for i := 2; i <= len(parts); i++ {
		out = append(out, "/"+strings.Join(parts[:i], "/"))
	}
	return out
}

func IndustryDescendants(path string) []string {
	path = normalizeIndustryPath(path)
	if path == "" {
		return nil
	}
	reg := GetIndustryRegistry()
	if reg == nil {
		return nil
	}
	if _, ok := reg.Nodes[path]; !ok {
		return nil
	}
	seen := map[string]struct{}{path: {}}
	queue := []string{path}
	out := []string{path}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		children := append([]string(nil), reg.Children[cur]...)
		sort.Strings(children)
		for _, child := range children {
			if _, ok := seen[child]; ok {
				continue
			}
			seen[child] = struct{}{}
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}

func NormalizeIndustryPaths(paths []string, fragmentType string) []string {
	if !isIndustryFragmentTypeAllowed(fragmentType) {
		return nil
	}
	if len(paths) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := normalizeIndustryPath(raw)
		if path == "" {
			continue
		}
		if err := ValidateIndustryPath(path, fragmentType); err != nil {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func MapIndustryQIDsToPaths(qids []string) []string {
	if len(qids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(qids))
	for _, qid := range qids {
		path, ok := ResolveIndustryKey(TaxonomyNamespaceIndustry, TaxonomyAliasKeyKindWikidata, qid)
		if !ok {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

type FirestoreIndustryRegistryCache struct {
	db      *firestore.Client
	version string
	ttl     time.Duration

	mu       sync.Mutex
	cached   *IndustryRegistry
	loadedAt time.Time
}

func NewFirestoreIndustryRegistryCache(db *firestore.Client, version string, ttl time.Duration) *FirestoreIndustryRegistryCache {
	if strings.TrimSpace(version) == "" {
		version = "v1"
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &FirestoreIndustryRegistryCache{db: db, version: version, ttl: ttl}
}

func (c *FirestoreIndustryRegistryCache) isFreshLocked(now time.Time) bool {
	return c != nil && c.cached != nil && !c.loadedAt.IsZero() && now.Sub(c.loadedAt) < c.ttl
}

func (c *FirestoreIndustryRegistryCache) Get(ctx context.Context) (*IndustryRegistry, error) {
	if c == nil || c.db == nil {
		return nil, nil
	}
	now := time.Now().UTC()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isFreshLocked(now) {
		return c.cached, nil
	}

	nodeSnap, err := c.db.Collection("horizon_industry_nodes").Doc(c.version).Get(ctx)
	if err != nil || nodeSnap == nil || !nodeSnap.Exists() {
		if c.cached != nil {
			return c.cached, nil
		}
		return nil, err
	}
	aliasSnap, err := c.db.Collection("horizon_taxonomy_aliases").Doc(c.version).Get(ctx)
	if err != nil || aliasSnap == nil || !aliasSnap.Exists() {
		if c.cached != nil {
			return c.cached, nil
		}
		return nil, err
	}

	var nodeDoc models.IndustryNodeRegistry
	if err := nodeSnap.DataTo(&nodeDoc); err != nil {
		if c.cached != nil {
			return c.cached, nil
		}
		return nil, err
	}
	var aliasDoc models.TaxonomyAliasRegistry
	if err := aliasSnap.DataTo(&aliasDoc); err != nil {
		if c.cached != nil {
			return c.cached, nil
		}
		return nil, err
	}

	out := &IndustryRegistry{
		Version:   c.version,
		Nodes:     map[string]IndustryNode{},
		Children:  map[string][]string{},
		Aliases:   map[string]map[string]string{},
		Ambiguous: map[string]map[string]struct{}{},
	}

	for _, node := range nodeDoc.Nodes {
		path := normalizeIndustryPath(node.Path)
		if path == "" {
			continue
		}
		parentPath := normalizeIndustryPath(node.ParentPath)
		allowedForTypes := map[string]struct{}{}
		for _, item := range node.AllowedForTypes {
			item = strings.ToUpper(strings.TrimSpace(item))
			if item == "" {
				continue
			}
			if !isIndustryFragmentTypeAllowed(item) {
				continue
			}
			allowedForTypes[item] = struct{}{}
		}
		if len(allowedForTypes) == 0 {
			for item := range allowedIndustryFragmentTypes {
				allowedForTypes[item] = struct{}{}
			}
		}
		status := strings.ToLower(strings.TrimSpace(node.Status))
		if status == "" {
			status = industryNodeStatusActive
		}
		out.Nodes[path] = IndustryNode{
			Path:            path,
			ParentPath:      parentPath,
			Level:           node.Level,
			Label:           strings.TrimSpace(node.Label),
			Status:          status,
			Definition:      strings.TrimSpace(node.Definition),
			AllowedForTypes: allowedForTypes,
		}
		if parentPath != "" {
			out.Children[parentPath] = append(out.Children[parentPath], path)
		}
	}

	for _, alias := range aliasDoc.Aliases {
		namespace := strings.ToLower(strings.TrimSpace(alias.Namespace))
		if namespace != TaxonomyNamespaceIndustry {
			continue
		}
		keyKind := strings.ToLower(strings.TrimSpace(alias.KeyKind))
		if keyKind == "" {
			continue
		}
		keyNorm := normalizeIndustryKey(keyKind, alias.KeyNorm)
		if keyNorm == "" {
			continue
		}
		canonicalPath := normalizeIndustryPath(alias.CanonicalPath)
		if canonicalPath == "" {
			continue
		}
		node, ok := out.Nodes[canonicalPath]
		if !ok || node.Status != industryNodeStatusActive {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(alias.Status))
		if status == "" {
			status = taxonomyAliasStatusActive
		}
		if status != taxonomyAliasStatusActive {
			continue
		}
		if out.Aliases[keyKind] == nil {
			out.Aliases[keyKind] = map[string]string{}
		}
		if out.Ambiguous[keyKind] == nil {
			out.Ambiguous[keyKind] = map[string]struct{}{}
		}
		if existing, exists := out.Aliases[keyKind][keyNorm]; exists && existing != canonicalPath {
			delete(out.Aliases[keyKind], keyNorm)
			out.Ambiguous[keyKind][keyNorm] = struct{}{}
			continue
		}
		if _, ambiguous := out.Ambiguous[keyKind][keyNorm]; ambiguous {
			continue
		}
		out.Aliases[keyKind][keyNorm] = canonicalPath
	}

	c.cached = out
	c.loadedAt = now
	return out, nil
}

func (c *FirestoreIndustryRegistryCache) StartAutoRefresh(ctx context.Context, interval time.Duration, onError func(error)) {
	if c == nil || c.db == nil {
		return
	}
	if interval <= 0 {
		interval = c.ttl / 2
		if interval < 30*time.Second {
			interval = 30 * time.Second
		}
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reg, err := c.Get(context.Background())
				if err != nil {
					if onError != nil {
						onError(err)
					}
					continue
				}
				if reg != nil {
					SetIndustryRegistry(reg)
				}
			}
		}
	}()
}

var industryCacheMu sync.Mutex
var industryCache *FirestoreIndustryRegistryCache

func InitIndustryRegistryCacheFromFirestore(db *firestore.Client, version string, ttl time.Duration) {
	if db == nil {
		return
	}
	industryCacheMu.Lock()
	defer industryCacheMu.Unlock()
	industryCache = NewFirestoreIndustryRegistryCache(db, version, ttl)
}

func RefreshIndustryRegistry(ctx context.Context) error {
	industryCacheMu.Lock()
	c := industryCache
	industryCacheMu.Unlock()
	if c == nil {
		return nil
	}
	reg, err := c.Get(ctx)
	if err != nil {
		return err
	}
	if reg != nil {
		SetIndustryRegistry(reg)
	}
	return nil
}

func StartIndustryRegistryAutoRefresh(ctx context.Context, interval time.Duration, onError func(error)) {
	industryCacheMu.Lock()
	c := industryCache
	industryCacheMu.Unlock()
	if c == nil {
		return
	}
	c.StartAutoRefresh(ctx, interval, onError)
}

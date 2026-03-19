package lens

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// QueryResolver resolves QueryAddress to a list of canonical paths
type QueryResolver struct {
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository
	ontologyFactsRepo *corerepo.OntologyFactsRepository
}

// NewQueryResolver creates a new QueryResolver
func NewQueryResolver(fragmentsRepo *horizonrepo.HorizonFragmentsRepository, ontologyFactsRepo *corerepo.OntologyFactsRepository) *QueryResolver {
	return &QueryResolver{
		fragmentsRepo:      fragmentsRepo,
		ontologyFactsRepo:  ontologyFactsRepo,
	}
}

// Resolve resolves a QueryAddress to a list of canonical paths
// Returns (canonicalPaths, error)
func (r *QueryResolver) Resolve(ctx context.Context, tenantID string, queryAddress *QueryAddress) ([]string, error) {
	startTime := time.Now()
	
	if queryAddress == nil {
		return nil, fmt.Errorf("nil query address")
	}

	if err := queryAddress.Validate(); err != nil {
		return nil, fmt.Errorf("invalid query address: %w", err)
	}

	if tenantID == "" {
		tenantID = "global" // HorizonFragments are always global
	}

	// Build evaluation context (expensive predicates precomputed once).
	evalCtx, err := r.buildEvalContext(ctx, tenantID, queryAddress.Expr)
	if err != nil {
		return nil, err
	}

	// Convert expression to DNF (OR of AND clauses) to be able to fetch candidates correctly for OR.
	clauses, err := exprToDNF(queryAddress.Expr, 32 /* maxClauses */)
	if err != nil {
		return nil, err
	}

	// Fetch candidate fragments (union across OR branches), then filter by full expression in-memory.
	candidates, fetchedCount, err := r.fetchCandidates(ctx, tenantID, clauses)
	if err != nil {
		return nil, err
	}

	canonicalPaths := make([]string, 0, len(candidates))
	matchedCount := 0
	for _, v := range candidates {
		if queryAddress.Expr.Eval(v, evalCtx) {
			canonicalPaths = append(canonicalPaths, v.CanonicalPath)
			matchedCount++
		}
	}
	skippedCount := fetchedCount - matchedCount

	duration := time.Since(startTime)
	log.Printf("[QueryResolver] Resolved query %q: %d paths found (matched %d, skipped %d) in %v", 
		queryAddress.Raw, len(canonicalPaths), matchedCount, skippedCount, duration)

	return canonicalPaths, nil
}

// --- "adult" query execution helpers ---

type lit struct {
	term Term
	neg  bool
}

type conj []lit

func (r *QueryResolver) baseFragmentsQuery(tenantID string) firestore.Query {
	return r.fragmentsRepo.Query().
		Where("tenantId", "==", tenantID).
		Where("governance.status", "==", "ACTIVE")
}

func (r *QueryResolver) buildEvalContext(ctx context.Context, tenantID string, expr Expr) (*EvalContext, error) {
	// Collect all sphere terms used in expression.
	terms := []Term{}
	expr.collectTerms(&terms)

	sphereExact := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, t := range terms {
		if t.Field != FieldSphere {
			continue
		}
		p := strings.TrimSpace(t.Pattern)
		if p == "" {
			continue
		}
		// "adult" policy: we only support exact sphere paths (no wildcards) because facts store spheres as arrays.
		if strings.Contains(p, "*") {
			return nil, fmt.Errorf("sphere criteria does not support wildcards yet: %q", p)
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		sphereExact = append(sphereExact, p)
	}

	if len(sphereExact) == 0 {
		return &EvalContext{}, nil
	}
	if r.ontologyFactsRepo == nil {
		return nil, fmt.Errorf("sphere criteria requires ontologyFactsRepo (not configured)")
	}

	out := &EvalContext{SphereCanonicalPathsByExact: map[string]map[string]struct{}{}}
	for _, sp := range sphereExact {
		paths, err := r.ontologyFactsRepo.ListCanonicalPathsBySpherePath(ctx, tenantID, sp, 2000)
		if err != nil {
			return nil, fmt.Errorf("sphere lookup failed for %q: %w", sp, err)
		}
		set := map[string]struct{}{}
		for _, cp := range paths {
			set[cp] = struct{}{}
		}
		out.SphereCanonicalPathsByExact[sp] = set
	}
	return out, nil
}

func exprToDNF(expr Expr, maxClauses int) ([]conj, error) {
	// First, push NOT down to terms (NNF).
	nnf, err := toNNF(expr, 64)
	if err != nil {
		return nil, err
	}
	clauses, err := toDNF(nnf, maxClauses)
	if err != nil {
		return nil, err
	}
	if len(clauses) == 0 {
		return nil, fmt.Errorf("empty expression (no clauses)")
	}
	return clauses, nil
}

func toNNF(expr Expr, maxNodes int) (Expr, error) {
	// Convert to negation normal form: NOT only applies to terms.
	var rec func(e Expr, budget *int) (Expr, error)
	rec = func(e Expr, budget *int) (Expr, error) {
		*budget--
		if *budget <= 0 {
			return nil, fmt.Errorf("query expression too complex")
		}
		switch x := e.(type) {
		case termExpr:
			return x, nil
		case andExpr:
			a, err := rec(x.a, budget)
			if err != nil {
				return nil, err
			}
			b, err := rec(x.b, budget)
			if err != nil {
				return nil, err
			}
			return andExpr{a: a, b: b}, nil
		case orExpr:
			a, err := rec(x.a, budget)
			if err != nil {
				return nil, err
			}
			b, err := rec(x.b, budget)
			if err != nil {
				return nil, err
			}
			return orExpr{a: a, b: b}, nil
		case notExpr:
			// Push negation down.
			switch y := x.x.(type) {
			case termExpr:
				return notExpr{x: y}, nil
			case notExpr:
				return rec(y.x, budget)
			case andExpr:
				// NOT (A AND B) -> (NOT A) OR (NOT B)
				na, err := rec(notExpr{x: y.a}, budget)
				if err != nil {
					return nil, err
				}
				nb, err := rec(notExpr{x: y.b}, budget)
				if err != nil {
					return nil, err
				}
				return orExpr{a: na, b: nb}, nil
			case orExpr:
				// NOT (A OR B) -> (NOT A) AND (NOT B)
				na, err := rec(notExpr{x: y.a}, budget)
				if err != nil {
					return nil, err
				}
				nb, err := rec(notExpr{x: y.b}, budget)
				if err != nil {
					return nil, err
				}
				return andExpr{a: na, b: nb}, nil
			default:
				return nil, fmt.Errorf("unsupported expression node")
			}
		default:
			return nil, fmt.Errorf("unsupported expression node")
		}
	}
	b := maxNodes
	return rec(expr, &b)
}

func toDNF(expr Expr, maxClauses int) ([]conj, error) {
	switch x := expr.(type) {
	case termExpr:
		return []conj{{{term: x.t, neg: false}}}, nil
	case notExpr:
		if t, ok := x.x.(termExpr); ok {
			return []conj{{{term: t.t, neg: true}}}, nil
		}
		return nil, fmt.Errorf("NOT must apply to a term after NNF")
	case orExpr:
		a, err := toDNF(x.a, maxClauses)
		if err != nil {
			return nil, err
		}
		b, err := toDNF(x.b, maxClauses)
		if err != nil {
			return nil, err
		}
		out := append(a, b...)
		if len(out) > maxClauses {
			return nil, fmt.Errorf("query expands to too many OR clauses (%d > %d)", len(out), maxClauses)
		}
		return out, nil
	case andExpr:
		a, err := toDNF(x.a, maxClauses)
		if err != nil {
			return nil, err
		}
		b, err := toDNF(x.b, maxClauses)
		if err != nil {
			return nil, err
		}
		out := make([]conj, 0, len(a)*len(b))
		for _, ca := range a {
			for _, cb := range b {
				merged, ok := mergeConj(ca, cb)
				if !ok {
					continue // contradiction
				}
				out = append(out, merged)
				if len(out) > maxClauses {
					return nil, fmt.Errorf("query expands to too many OR clauses (%d > %d)", len(out), maxClauses)
				}
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported expression node in DNF")
	}
}

func mergeConj(a, b conj) (conj, bool) {
	out := make(conj, 0, len(a)+len(b))
	seen := map[string]bool{} // key -> neg?
	for _, l := range a {
		key := string(l.term.Field) + ":" + l.term.Pattern
		seen[key] = l.neg
		out = append(out, l)
	}
	for _, l := range b {
		key := string(l.term.Field) + ":" + l.term.Pattern
		if neg, ok := seen[key]; ok {
			if neg != l.neg {
				return nil, false // contradiction
			}
			continue
		}
		seen[key] = l.neg
		out = append(out, l)
	}
	return out, true
}

func (r *QueryResolver) fetchCandidates(ctx context.Context, tenantID string, clauses []conj) ([]*HorizonFragmentView, int, error) {
	// De-duplicate by canonicalPath across clauses.
	seen := map[string]struct{}{}
	out := make([]*HorizonFragmentView, 0, 128)
	totalFetched := 0

	for _, c := range clauses {
		q := r.pickSeedQuery(tenantID, c)
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return nil, totalFetched, err
		}
		totalFetched += len(docs)
		for _, d := range docs {
			var f models.HorizonFragment
			if err := d.DataTo(&f); err != nil {
				continue
			}
			view := fragmentToView(&f)
			if view == nil || strings.TrimSpace(view.CanonicalPath) == "" {
				continue
			}
			if _, ok := seen[view.CanonicalPath]; ok {
				continue
			}
			seen[view.CanonicalPath] = struct{}{}
			out = append(out, view)
		}
	}
	return out, totalFetched, nil
}

func (r *QueryResolver) pickSeedQuery(tenantID string, c conj) firestore.Query {
	base := r.baseFragmentsQuery(tenantID)

	// Choose a single positive literal for Firestore seeding (must be correct subset, then full expr filters).
	// Priority: canonicalPath > industry > entityType > location
	bestRank := 999
	var best *lit
	for i := range c {
		if c[i].neg {
			continue
		}
		rank := literalSeedRank(c[i])
		if rank < bestRank && rank < 999 {
			bestRank = rank
			best = &c[i]
		}
	}
	if best == nil {
		return base
	}
	return applySeed(base, best.term)
}

func literalSeedRank(l lit) int {
	switch l.term.Field {
	case FieldCanonicalPath:
		return 1
	case FieldIndustry:
		return 2
	case FieldEntityType:
		return 3
	case FieldLocation:
		return 4
	default:
		return 999
	}
}

func applySeed(q firestore.Query, t Term) firestore.Query {
	p := strings.TrimSpace(t.Pattern)
	switch t.Field {
	case FieldCanonicalPath:
		return applyPathSeed(q, "canonicalPath", p)
	case FieldIndustry:
		return applyIndexPathSeed(q, p)
	case FieldEntityType:
		return applyPathSeed(q, "entityPath", p)
	case FieldLocation:
		// /loc/{cc}[/admin1][/**]
		cc, admin1 := parseLocPattern(p)
		if cc != "" && admin1 != "" {
			return q.Where("homeLocation.iso3166_2", "==", strings.ToUpper(cc)+"-"+strings.ToUpper(admin1))
		}
		if cc != "" {
			return q.Where("homeLocation.iso3166_1", "==", strings.ToLower(cc))
		}
		return q
	default:
		return q
	}
}

func applyPathSeed(q firestore.Query, field string, pattern string) firestore.Query {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return q.Where(field, ">=", prefix).Where(field, "<", prefix+"\ufffd")
	}
	// exact only (no wildcard)
	if !strings.Contains(pattern, "*") {
		return q.Where(field, "==", pattern)
	}
	return q
}

func applyIndexPathSeed(q firestore.Query, pattern string) firestore.Query {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return q.Where("indexPaths", "array-contains", prefix)
	}
	if !strings.Contains(pattern, "*") {
		return q.Where("indexPaths", "array-contains", pattern)
	}
	return q
}

func parseLocPattern(pattern string) (country string, admin1 string) {
	// Accepts: /loc/{country}, /loc/{country}/** , /loc/{country}/{admin1}, /loc/{country}/{admin1}/**
	parts := strings.Split(strings.Trim(pattern, "/"), "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return "", ""
	}
	country = strings.ToLower(strings.TrimSpace(parts[1]))
	if len(parts) >= 3 && parts[2] != "**" && parts[2] != "" {
		admin1 = strings.ToLower(strings.TrimSpace(parts[2]))
	}
	return country, admin1
}

func fragmentToView(f *models.HorizonFragment) *HorizonFragmentView {
	if f == nil {
		return nil
	}
	return &HorizonFragmentView{
		CanonicalPath: f.CanonicalPath,
		EntityPath:    f.EntityPath,
		IndustryPaths: append([]string(nil), f.IndustryPaths...),
		LocationPath:  buildLocationPath(f.HomeLocation),
	}
}

// buildLocationPath builds a best-effort canonicalized location path for QueryAddress matching.
// Canonical format: /loc/{country}[/{level1}[/{level2}]]
// HorizonFragment stores only ISO3166-1 + ISO3166-2; we map to /loc/{country}/{level1}.
func buildLocationPath(homeLocation *models.Location) string {
	if homeLocation == nil || homeLocation.ISO3166_1 == nil || strings.TrimSpace(*homeLocation.ISO3166_1) == "" {
		return ""
	}
	country := strings.ToLower(strings.TrimSpace(*homeLocation.ISO3166_1))
	admin1 := "na"
	if homeLocation.ISO3166_2 != nil && strings.TrimSpace(*homeLocation.ISO3166_2) != "" {
		parts := strings.Split(strings.TrimSpace(*homeLocation.ISO3166_2), "-")
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			admin1 = strings.ToLower(strings.TrimSpace(parts[1]))
		}
	}
	return "/loc/" + country + "/" + admin1 + "/na"
}

package lens

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Expr is a boolean expression over fragment criteria.
type Expr interface {
	// Eval evaluates expression for a fragment using an evaluation context.
	Eval(f *HorizonFragmentView, ctx *EvalContext) bool
	// collectTerms returns all criterion terms inside expression.
	collectTerms(out *[]Term)
}

// TermField represents supported criterion fields.
type TermField string

const (
	FieldSphere        TermField = "sphere"
	FieldIndustry      TermField = "industry"
	FieldLocation      TermField = "location"
	FieldEntityType    TermField = "entityType"
	FieldCanonicalPath TermField = "canonicalPath"
)

// Term is an atomic predicate (field:value).
type Term struct {
	Field   TermField
	Pattern string
}

type termExpr struct{ t Term }
type andExpr struct{ a, b Expr }
type orExpr struct{ a, b Expr }
type notExpr struct{ x Expr }

func (e termExpr) Eval(f *HorizonFragmentView, ctx *EvalContext) bool {
	if f == nil {
		return false
	}
	switch e.t.Field {
	case FieldIndustry:
		for _, path := range f.IndustryPaths {
			if MatchesPattern(path, e.t.Pattern) {
				return true
			}
		}
		return false
	case FieldEntityType:
		return MatchesPattern(f.EntityPath, e.t.Pattern)
	case FieldCanonicalPath:
		return MatchesPattern(f.CanonicalPath, e.t.Pattern)
	case FieldLocation:
		lp := f.LocationPath
		if lp == "" {
			return false
		}
		return MatchesPattern(lp, e.t.Pattern)
	case FieldSphere:
		if ctx == nil || ctx.SphereCanonicalPathsByExact == nil {
			return false
		}
		// Sphere patterns are enforced as exact paths (no wildcards) in the resolver.
		set := ctx.SphereCanonicalPathsByExact[e.t.Pattern]
		if set == nil {
			return false
		}
		_, ok := set[f.CanonicalPath]
		return ok
	default:
		return false
	}
}

func (e termExpr) collectTerms(out *[]Term) { *out = append(*out, e.t) }
func (e andExpr) collectTerms(out *[]Term)  { e.a.collectTerms(out); e.b.collectTerms(out) }
func (e orExpr) collectTerms(out *[]Term)   { e.a.collectTerms(out); e.b.collectTerms(out) }
func (e notExpr) collectTerms(out *[]Term)  { e.x.collectTerms(out) }

func (e andExpr) Eval(f *HorizonFragmentView, ctx *EvalContext) bool {
	return e.a.Eval(f, ctx) && e.b.Eval(f, ctx)
}
func (e orExpr) Eval(f *HorizonFragmentView, ctx *EvalContext) bool {
	return e.a.Eval(f, ctx) || e.b.Eval(f, ctx)
}
func (e notExpr) Eval(f *HorizonFragmentView, ctx *EvalContext) bool { return !e.x.Eval(f, ctx) }

// HorizonFragmentView is a minimal projection used by QueryResolver.
// We keep it decoupled from the Firestore model to make it testable.
type HorizonFragmentView struct {
	CanonicalPath string
	EntityPath    string
	IndustryPaths []string
	LocationPath  string // derived from geoSlug/homeLocation (best-effort)
}

// EvalContext contains precomputed sets for expensive predicates.
type EvalContext struct {
	// For each exact sphere path, a set of canonicalPaths that have facts in that sphere.
	SphereCanonicalPathsByExact map[string]map[string]struct{}
}

// --- Pattern helpers ---

var reDoubleStarCache sync.Map // pattern -> *regexp.Regexp

func toDoubleStarRegex(pattern string) *regexp.Regexp {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return regexp.MustCompile("^$") // never matches non-empty values
	}
	if v, ok := reDoubleStarCache.Load(pattern); ok {
		if r, ok2 := v.(*regexp.Regexp); ok2 && r != nil {
			return r
		}
	}
	// Escape everything, then turn "**" into ".*"
	rx := regexp.QuoteMeta(pattern)
	rx = strings.ReplaceAll(rx, "\\*\\*", ".*")
	rx = "^" + rx + "$"
	compiled := regexp.MustCompile(rx)
	reDoubleStarCache.Store(pattern, compiled)
	return compiled
}

// --- Parser ---

type tokKind int

const (
	tokEOF tokKind = iota
	tokLParen
	tokRParen
	tokAND
	tokOR
	tokNOT
	tokTerm
)

type token struct {
	kind tokKind
	lit  string
	term Term
}

func parseQueryExpr(input string) (Expr, error) {
	toks, err := tokenizeQueryExpr(input)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	expr, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q", p.peek().lit)
	}
	return expr, nil
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token {
	if p.i >= len(p.toks) {
		return token{kind: tokEOF, lit: ""}
	}
	return p.toks[p.i]
}

func (p *parser) next() token {
	t := p.peek()
	if p.i < len(p.toks) {
		p.i++
	}
	return t
}

// OR lowest precedence
func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		if p.peek().kind != tokOR {
			break
		}
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orExpr{a: left, b: right}
	}
	return left, nil
}

// AND middle precedence
func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		if p.peek().kind != tokAND {
			break
		}
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = andExpr{a: left, b: right}
	}
	return left, nil
}

// NOT highest precedence
func (p *parser) parseUnary() (Expr, error) {
	if p.peek().kind == tokNOT {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return notExpr{x: x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Expr, error) {
	t := p.peek()
	switch t.kind {
	case tokLParen:
		p.next()
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("expected ')'")
		}
		p.next()
		return e, nil
	case tokTerm:
		p.next()
		if strings.TrimSpace(t.term.Pattern) == "" {
			return nil, fmt.Errorf("empty pattern for %s", t.term.Field)
		}
		return termExpr{t: t.term}, nil
	default:
		return nil, fmt.Errorf("unexpected token %q", t.lit)
	}
}

func tokenizeQueryExpr(input string) ([]token, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, fmt.Errorf("empty expression")
	}
	out := make([]token, 0, 16)
	i := 0
	readWord := func() string {
		start := i
		for i < len(s) {
			ch := s[i]
			if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '(' || ch == ')' {
				break
			}
			i++
		}
		return s[start:i]
	}
	skipWS := func() {
		for i < len(s) {
			if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
				i++
				continue
			}
			break
		}
	}
	for {
		skipWS()
		if i >= len(s) {
			out = append(out, token{kind: tokEOF, lit: ""})
			break
		}
		switch s[i] {
		case '(':
			out = append(out, token{kind: tokLParen, lit: "("})
			i++
			continue
		case ')':
			out = append(out, token{kind: tokRParen, lit: ")"})
			i++
			continue
		}

		word := readWord()
		if word == "" {
			return nil, fmt.Errorf("unexpected character at %d", i)
		}
		upper := strings.ToUpper(word)
		switch upper {
		case "AND":
			out = append(out, token{kind: tokAND, lit: word})
			continue
		case "OR":
			out = append(out, token{kind: tokOR, lit: word})
			continue
		case "NOT":
			out = append(out, token{kind: tokNOT, lit: word})
			continue
		}

		// criterion: field:value (value can be quoted to include spaces, but then it must be the last thing readWord saw)
		colon := strings.IndexByte(word, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("expected criterion field:value, got %q", word)
		}
		fieldRaw := strings.ToLower(strings.TrimSpace(word[:colon]))
		valueRaw := strings.TrimSpace(word[colon+1:])
		// If value begins with quote, parse quoted string from the original buffer.
		if strings.HasPrefix(valueRaw, "\"") {
			// Rewind to start of value in s: current token started at (i-len(word)), value starts after colon within word.
			tokenStart := i - len(word)
			valStart := tokenStart + colon + 1
			if valStart >= len(s) || s[valStart] != '"' {
				return nil, fmt.Errorf("invalid quoted value")
			}
			j := valStart + 1
			for j < len(s) && s[j] != '"' {
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated quoted value")
			}
			valueRaw = s[valStart+1 : j]
			// advance i to after closing quote
			i = j + 1
		}

		term, err := normalizeTerm(fieldRaw, valueRaw)
		if err != nil {
			return nil, err
		}
		out = append(out, token{kind: tokTerm, lit: word, term: term})
	}
	return out, nil
}

func normalizeTerm(fieldRaw string, value string) (Term, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Term{}, fmt.Errorf("empty value for %q", fieldRaw)
	}
	switch fieldRaw {
	case "sphere", "spherepath", "spherepaths":
		return Term{Field: FieldSphere, Pattern: value}, nil
	case "industry", "industrypath", "industrypaths":
		return Term{Field: FieldIndustry, Pattern: value}, nil
	case "location", "locationpath":
		return Term{Field: FieldLocation, Pattern: value}, nil
	case "entitytype", "entity_type":
		return Term{Field: FieldEntityType, Pattern: value}, nil
	case "canonicalpath", "canonical_path", "path":
		return Term{Field: FieldCanonicalPath, Pattern: value}, nil
	default:
		return Term{}, fmt.Errorf("unknown criteria field: %s", fieldRaw)
	}
}

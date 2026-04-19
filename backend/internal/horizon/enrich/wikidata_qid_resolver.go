package enrich

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"golang.org/x/time/rate"
)

// QIDResolutionCandidate is a scored Wikidata candidate.
type QIDResolutionCandidate struct {
	QID   string
	Label string
	Desc  string
	URL   string
	Score int
}

// QIDResolutionResult is the output of ResolveWikidataQIDForFragment.
type QIDResolutionResult struct {
	Query      string
	QID        string
	Score      int
	Candidates []QIDResolutionCandidate
}

type QIDResolveOptions struct {
	Enabled       bool
	MinScore      int
	MaxCandidates int
	// RateLimit is a global limiter for outbound Wikidata calls (Search/GetEntityData).
	// If nil, a default limiter is used.
	RateLimit *rate.Limiter
}

func (o QIDResolveOptions) withDefaults() QIDResolveOptions {
	out := o
	if out.MaxCandidates <= 0 {
		out.MaxCandidates = 3
	}
	if out.MaxCandidates > 10 {
		out.MaxCandidates = 10
	}
	if out.MinScore <= 0 {
		out.MinScore = 130
	}
	if out.RateLimit == nil {
		out.RateLimit = defaultWikidataResolveLimiter()
	}
	return out
}

// Default env toggles for core QID resolution (scout-time).
//
// - HORIZON_ENABLE_WIKIDATA_QID_RESOLUTION=true|1
// - HORIZON_WIKIDATA_QID_SCORE_MIN=130
// - HORIZON_WIKIDATA_QID_MAX_CANDIDATES=3
// - HORIZON_WIKIDATA_QID_RATE_LIMIT_MS=120
func QIDResolveOptionsFromEnv() QIDResolveOptions {
	enabled := strings.ToLower(strings.TrimSpace(os.Getenv("HORIZON_ENABLE_WIKIDATA_QID_RESOLUTION")))
	minScore, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("HORIZON_WIKIDATA_QID_SCORE_MIN")))
	maxCand, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("HORIZON_WIKIDATA_QID_MAX_CANDIDATES")))
	return QIDResolveOptions{
		Enabled:       enabled == "1" || enabled == "true" || enabled == "yes" || enabled == "y" || enabled == "on",
		MinScore:      minScore,
		MaxCandidates: maxCand,
	}
}

var (
	wdLimiterOnce sync.Once
	wdLimiter     *rate.Limiter
)

func defaultWikidataResolveLimiter() *rate.Limiter {
	wdLimiterOnce.Do(func() {
		ms, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("HORIZON_WIKIDATA_QID_RATE_LIMIT_MS")))
		if ms <= 0 {
			ms = 120
		}
		d := time.Duration(ms) * time.Millisecond
		wdLimiter = rate.NewLimiter(rate.Every(d), 1)
	})
	return wdLimiter
}

// ResolveAndAttachWikidataQIDIfMissing attempts to resolve a Wikidata QID for a fragment (ORG/PERSON),
// and, if confident, attaches it to the fragment in-place (identifiers.wikidataId + KBRef).
//
// Safety:
// - Never runs unless opts.Enabled is true.
// - Only resolves if fragment has no Wikidata QID.
// - Applies only if score >= opts.MinScore.
func ResolveAndAttachWikidataQIDIfMissing(
	ctx context.Context,
	client coreontology.WikidataClientInterface,
	fragment *hmodels.HorizonFragment,
	opts QIDResolveOptions,
) (*QIDResolutionResult, *coreontology.WikidataEntityData, bool, error) {
	if client == nil || fragment == nil {
		return nil, nil, false, nil
	}
	opts = opts.withDefaults()
	if !opts.Enabled {
		return nil, nil, false, nil
	}
	if fragment.Class != FragmentTypeOrganization && fragment.Class != FragmentTypePerson {
		return nil, nil, false, nil
	}
	if strings.TrimSpace(extractWikidataIDFromFragment(fragment)) != "" {
		return nil, nil, false, nil
	}

	best, err := ResolveWikidataQIDForFragment(ctx, client, fragment, opts)
	if err != nil || best == nil || strings.TrimSpace(best.QID) == "" {
		return best, nil, false, err
	}
	if best.Score < opts.MinScore {
		return best, nil, false, nil
	}

	// Fetch entity data for type-gating and enrichment.
	if opts.RateLimit != nil {
		_ = opts.RateLimit.Wait(ctx)
	}
	ed, err := client.GetEntityData(ctx, strings.TrimSpace(best.QID))
	if err != nil || ed == nil {
		return best, nil, false, fmt.Errorf("GetEntityData(%s) failed: %w", best.QID, err)
	}

	// Type gating using P31/Q5.
	isHuman := looksHuman(ed)
	if fragment.Class == FragmentTypePerson && !isHuman {
		return best, ed, false, fmt.Errorf("qid %s is not human (P31=%v)", best.QID, extractEntityIDs(ed.PropertiesRaw, "P31"))
	}
	if fragment.Class == FragmentTypeOrganization && isHuman {
		return best, ed, false, fmt.Errorf("qid %s is human (unexpected for org)", best.QID)
	}

	// Attach QID to fragment (best-effort; Validate() will mirror).
	if fragment.Identifiers == nil {
		fragment.Identifiers = map[string]string{}
	}
	fragment.Identifiers["wikidataId"] = strings.TrimSpace(best.QID)
	// Ensure KBRef exists (Validate() will also add if missing).
	has := false
	for _, r := range fragment.KBRefs {
		if strings.EqualFold(strings.TrimSpace(r.KB), "Wikidata") {
			has = true
			break
		}
	}
	if !has {
		fragment.KBRefs = append(fragment.KBRefs, hmodels.KBReference{KB: "Wikidata", ExternalID: strings.TrimSpace(best.QID), Confidence: 1.0})
	}
	_ = fragment.Validate()
	return best, ed, true, nil
}

// ResolveWikidataQIDForFragment searches Wikidata and returns the best-scored candidate (plus top alternatives).
// This does NOT mutate the fragment.
func ResolveWikidataQIDForFragment(
	ctx context.Context,
	client coreontology.WikidataClientInterface,
	fragment *hmodels.HorizonFragment,
	opts QIDResolveOptions,
) (*QIDResolutionResult, error) {
	if client == nil || fragment == nil {
		return nil, nil
	}
	opts = opts.withDefaults()
	queries := pickQueries(fragment)
	if len(queries) == 0 {
		return nil, nil
	}

	bestScore := -1
	bestQID := ""
	bestQuery := queries[0]
	all := make([]QIDResolutionCandidate, 0, opts.MaxCandidates*len(queries))

	for qi, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if opts.RateLimit != nil {
			_ = opts.RateLimit.Wait(ctx)
		}
		resp, err := client.Search(ctx, q, opts.MaxCandidates)
		if err != nil || resp == nil || len(resp.Results) == 0 {
			continue
		}
		for i, r := range resp.Results {
			qid := strings.TrimSpace(r.ID)
			if qid == "" {
				continue
			}
			label := strings.TrimSpace(r.Label)
			desc := strings.TrimSpace(r.Description)
			url := strings.TrimSpace(r.URL)

			score := scoreCandidate(fragment, q, label, desc)
			// penalize fallback queries / later results slightly
			score -= qi * 3
			score -= i * 1

			c := QIDResolutionCandidate{QID: qid, Label: label, Desc: desc, URL: url, Score: score}
			all = append(all, c)
			if score > bestScore {
				bestScore = score
				bestQID = qid
				bestQuery = q
			}
		}
	}
	if len(all) == 0 {
		return nil, nil
	}
	// sort desc by score (small N)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].Score > all[i].Score {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if len(all) > opts.MaxCandidates {
		all = all[:opts.MaxCandidates]
	}
	return &QIDResolutionResult{Query: bestQuery, QID: bestQID, Score: bestScore, Candidates: all}, nil
}

func pickQueries(f *hmodels.HorizonFragment) []string {
	if f == nil {
		return nil
	}
	legal := strings.TrimSpace(f.Identifiers["legalName"])
	label := strings.TrimSpace(f.Labels["en"])
	short := strings.TrimSpace(f.Identifiers["shortName"])

	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	// primary
	if f.Class == FragmentTypePerson {
		add(label)
		add(legal)
		add(short)
	} else {
		add(legal)
		add(label)
		add(short)
	}
	// normalized fallbacks
	if legal != "" {
		add(normalizeLegalName(legal))
	}
	if label != "" {
		add(normalizeLegalName(label))
	}
	// canonicalPath last segment (rough)
	if cp := strings.TrimSpace(f.CanonicalPath); strings.HasPrefix(cp, "/") {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 2 {
			last := parts[len(parts)-1]
			last = strings.ReplaceAll(last, "-", " ")
			last = strings.ReplaceAll(last, "_", " ")
			add(last)
		}
	}
	return out
}

func scoreCandidate(f *hmodels.HorizonFragment, query, candLabel, candDesc string) int {
	score := 0
	qn := normalizeNameForMatch(query)
	ln := normalizeNameForMatch(candLabel)
	if ln != "" && qn != "" {
		if ln == qn {
			score += 80
		} else if strings.Contains(ln, qn) || strings.Contains(qn, ln) {
			score += 55
		}
	}
	// Org-specific suffix normalization bonus.
	if f != nil && f.Class == FragmentTypeOrganization {
		q2 := normalizeLegalName(query)
		l2 := normalizeLegalName(candLabel)
		if q2 != "" && l2 != "" && q2 == l2 {
			score += 35
		}
	}

	// Hard penalties for known bad candidate kinds (works even without entity data).
	txt := strings.ToLower(strings.TrimSpace(candLabel + " " + candDesc))
	for _, bad := range []string{"genus", "family of", "scientific article", "album", "song", "surname", "history of"} {
		if strings.Contains(txt, bad) {
			score -= 80
			break
		}
	}

	// Soft org-like boost for orgs.
	if f != nil && f.Class == FragmentTypeOrganization {
		if strings.Contains(txt, "company") || strings.Contains(txt, "organization") || strings.Contains(txt, "bank") || strings.Contains(txt, "airline") || strings.Contains(txt, "government") {
			score += 10
		}
	}
	return score
}

func normalizeNameForMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// lightweight normalization
	r := strings.NewReplacer("&", "and", ".", "", ",", "", "-", " ", "_", " ")
	s = r.Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func normalizeLegalName(name string) string {
	name = normalizeNameForMatch(name)
	if name == "" {
		return ""
	}
	suffixes := []string{
		" jsc", " llc", " inc", " ltd", " co", " corp", " corporation",
		" group", " holdings", " limited", " company", " pty ltd",
		" sa", " ag", " se", " gmbh", " ab", " oy", " as", " spa",
		" nv", " bv", " sarl", " plc", " lp", " llp",
		" s a", " a g", " s p a", " n v", " b v", " p l c",
		" co ltd", " co ltd", " co ltd",
	}
	for _, suf := range suffixes {
		if strings.HasSuffix(name, suf) {
			name = strings.TrimSpace(strings.TrimSuffix(name, suf))
			break
		}
	}
	return strings.Join(strings.Fields(name), " ")
}

func looksHuman(ed *coreontology.WikidataEntityData) bool {
	if ed == nil {
		return false
	}
	p31 := extractEntityIDs(ed.PropertiesRaw, "P31")
	for _, x := range p31 {
		if strings.TrimSpace(x) == "Q5" {
			return true
		}
	}
	return false
}



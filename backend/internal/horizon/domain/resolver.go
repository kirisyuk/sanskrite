package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type ResolveInput struct {
	Query               string
	ExistingDomains     []string
	DomainCanonicalPath string
}

type ResolveResult struct {
	Intent    *agents.IntentResolution
	Contracts []Contract
}

// Resolve converts a natural-language query into typed domain contracts without executing scouting.
// It is used by preview-style flows and by thin HTTP wrappers that enqueue domain runs.
func (e *Engine) Resolve(ctx context.Context, in ResolveInput) (*ResolveResult, error) {
	if e == nil {
		return nil, fmt.Errorf("domain engine is nil")
	}
	if e.aiExecutor == nil {
		return nil, fmt.Errorf("domain resolve requires ai executor")
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	existingDomains := normalizeDomainPathList(in.ExistingDomains, 32)
	logDomainContract(ctx, "resolve.start", nil, nil, map[string]any{
		"queryLength":           len(query),
		"existingDomainsInput":  len(in.ExistingDomains),
		"existingDomainsScoped": len(existingDomains),
		"preferredPath":         strings.TrimSpace(in.DomainCanonicalPath),
	})

	intentAgent := agents.NewIntentResolverAgent(e.aiExecutor)
	intent, err := intentAgent.ResolveWithContext(ctx, query, existingDomains)
	if err != nil {
		logDomainContract(ctx, "resolve.intent.error", nil, nil, map[string]any{
			"error": err.Error(),
		})
		return nil, err
	}
	if intent == nil {
		logDomainContract(ctx, "resolve.intent.nil", nil, nil, nil)
		return nil, fmt.Errorf("intent resolve returned nil result")
	}

	out := &ResolveResult{
		Intent:    intent,
		Contracts: make([]Contract, 0, len(intent.Domains)),
	}
	for idx, d := range intent.Domains {
		c := contractFromIntentResolution(d)
		if c.CanonicalPath == "" {
			logDomainContract(ctx, "resolve.contract.drop", nil, nil, map[string]any{
				"index":         idx,
				"canonicalPath": strings.TrimSpace(d.CanonicalPath),
				"contractType":  strings.TrimSpace(d.ContractType),
				"reason":        "invalid_or_empty_canonical_path",
			})
			continue
		}
		out.Contracts = append(out.Contracts, c)
	}

	preferredPath := strings.TrimSpace(in.DomainCanonicalPath)
	if preferredPath != "" {
		if err := taxonomy.ValidateCanonicalPath(preferredPath); err == nil && strings.HasPrefix(preferredPath, "/domain/") {
			out.Contracts = prioritizeContractPath(out.Contracts, preferredPath)
			if len(out.Contracts) == 0 {
				out.Contracts = append(out.Contracts, contractFromCanonicalPath(preferredPath))
				logDomainContract(ctx, "resolve.contract.injected_preferred", nil, &out.Contracts[0], map[string]any{
					"preferredPath": preferredPath,
				})
			}
		} else {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			logDomainContract(ctx, "resolve.preferred.invalid", nil, nil, map[string]any{
				"preferredPath": preferredPath,
				"error":         errMsg,
			})
		}
	}

	if len(out.Contracts) == 0 {
		logDomainContract(ctx, "resolve.error.no_contracts", nil, nil, map[string]any{
			"intentDomains": len(intent.Domains),
		})
		return nil, fmt.Errorf("resolve produced no domain contracts")
	}
	top := out.Contracts[0]
	logDomainContract(ctx, "resolve.completed", nil, &top, map[string]any{
		"contractCount": len(out.Contracts),
		"focus":         strings.TrimSpace(intent.Focus),
	})
	return out, nil
}

func contractFromIntentResolution(in agents.DomainResolution) Contract {
	out := contractFromCanonicalPath(strings.TrimSpace(in.CanonicalPath))
	if out.CanonicalPath == "" {
		return Contract{}
	}
	out.ContractType = strings.TrimSpace(strings.ToLower(in.ContractType))
	if out.ContractType == "" {
		parts := strings.Split(strings.Trim(out.CanonicalPath, "/"), "/")
		if len(parts) >= 3 && strings.EqualFold(parts[0], "domain") && strings.EqualFold(parts[1], "v1") {
			out.ContractType = strings.TrimSpace(strings.ToLower(parts[2]))
		}
	}
	out.Title = strings.TrimSpace(in.Title)
	out.Description = strings.TrimSpace(in.Description)
	out.Facets = normalizeFacetMap(in.Facets)
	out.FirstOrderEntities = normalizeDomainPathList(in.FirstOrderEntities, 64)
	out.RelatedDomains = normalizeDomainPathList(in.RelatedDomains, 32)
	out.AffectedDomains = normalizeDomainPathList(in.AffectedDomains, 32)
	return out
}

func contractFromCanonicalPath(canonicalPath string) Contract {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" || !strings.HasPrefix(cp, "/domain/") {
		return Contract{}
	}
	if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
		return Contract{}
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	ct := ""
	if len(parts) >= 3 && strings.EqualFold(parts[0], "domain") && strings.EqualFold(parts[1], "v1") {
		ct = strings.TrimSpace(strings.ToLower(parts[2]))
	}
	return Contract{
		CanonicalPath: cp,
		ContractType:  ct,
		Facets:        parseFacetsFromDomainPath(cp),
		Title:         cp,
	}
}

func prioritizeContractPath(in []Contract, preferred string) []Contract {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		return in
	}
	out := make([]Contract, 0, len(in))
	for _, c := range in {
		if strings.TrimSpace(c.CanonicalPath) == preferred {
			out = append([]Contract{c}, out...)
		} else {
			out = append(out, c)
		}
	}
	return out
}

func normalizeFacetMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(in))
	for key, vals := range in {
		k := strings.TrimSpace(strings.ToLower(key))
		if k == "" {
			continue
		}
		seen := map[string]struct{}{}
		clean := make([]string, 0, len(vals))
		for _, raw := range vals {
			v := strings.TrimSpace(strings.ToLower(raw))
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			clean = append(clean, v)
		}
		out[k] = clean
	}
	return out
}

func normalizeDomainPathList(in []string, limit int) []string {
	if limit <= 0 {
		limit = len(in)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, minInt(limit, len(in)))
	for _, raw := range in {
		cp := strings.TrimSpace(raw)
		if cp == "" || !strings.HasPrefix(cp, "/domain/") {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		seen[cp] = struct{}{}
		out = append(out, cp)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

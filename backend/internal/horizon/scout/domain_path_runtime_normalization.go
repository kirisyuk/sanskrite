package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// NormalizeRuntimeDomainCanonicalPath repairs common malformed /domain/v1/* paths
// before runtime matching, fragment upsert, or queue creation.
func NormalizeRuntimeDomainCanonicalPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" || !strings.HasPrefix(strings.ToLower(p), "/domain/") {
		return p
	}
	p = repairRuntimeSupplyChainDomainNamespacePath(p)
	p = trimRuntimeDomainPathTrailingIncompleteFacet(p)
	if norm, err := taxonomy.NormalizeDomainCanonicalPath(p); err == nil && strings.TrimSpace(norm) != "" {
		return strings.TrimSpace(norm)
	}
	return p
}

func trimRuntimeDomainPathTrailingIncompleteFacet(path string) string {
	p := strings.TrimSpace(path)
	if p == "" || !strings.HasPrefix(strings.ToLower(p), "/domain/") {
		return p
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 5 {
		return p
	}
	rest := parts[3:]
	if len(rest)%2 == 0 {
		return p
	}
	return "/" + strings.Join(parts[:len(parts)-1], "/")
}

func repairRuntimeSupplyChainDomainNamespacePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" || !strings.HasPrefix(strings.ToLower(p), "/domain/v1/") {
		return p
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 6 {
		return p
	}
	contractType := strings.ToLower(strings.TrimSpace(parts[2]))
	if contractType != "supply-chain" && contractType != "value-chain" {
		return p
	}
	rest := parts[3:]
	if len(rest) < 3 || strings.ToLower(strings.TrimSpace(rest[0])) != "org" {
		return p
	}
	geo := taxonomy.Slugify(rest[1])
	subject := taxonomy.Slugify(rest[2])
	if geo == "" || subject == "" {
		return p
	}
	rebuilt := []string{"domain", "v1", contractType, "geo", geo, "subject", subject}
	rebuilt = append(rebuilt, rest[3:]...)
	return "/" + strings.Join(rebuilt, "/")
}

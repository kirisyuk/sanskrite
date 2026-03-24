package productsem

import (
	"sort"
	"strings"
	"unicode"
)

type PathCandidate struct {
	Path    string
	Name    string
	Quality int
}

type Group struct {
	Key     string
	Winner  PathCandidate
	Members []PathCandidate
}

func SemanticKey(path string, name string) string {
	labelKey := normalizeKey(name)
	slugKey := normalizeKey(lastSegment(path))
	switch {
	case labelKey != "" && slugKey != "" && (labelKey == slugKey || strings.Contains(labelKey, slugKey) || strings.Contains(slugKey, labelKey)):
		if len(labelKey) >= len(slugKey) {
			return "label:" + labelKey
		}
		return "label:" + slugKey
	case labelKey != "":
		return "label:" + labelKey
	case slugKey != "":
		return "slug:" + slugKey
	default:
		return ""
	}
}

func BuildGroups(candidates []PathCandidate) []Group {
	byKey := map[string]map[string]PathCandidate{}
	for _, candidate := range candidates {
		path := strings.TrimSpace(candidate.Path)
		if path == "" || !strings.HasPrefix(path, "/product/") {
			continue
		}
		candidate.Path = path
		key := SemanticKey(candidate.Path, candidate.Name)
		if key == "" {
			continue
		}
		if byKey[key] == nil {
			byKey[key] = map[string]PathCandidate{}
		}
		current, ok := byKey[key][candidate.Path]
		if !ok || candidateScore(candidate) > candidateScore(current) {
			byKey[key][candidate.Path] = candidate
		}
	}
	groups := make([]Group, 0, len(byKey))
	for key, membersByPath := range byKey {
		members := make([]PathCandidate, 0, len(membersByPath))
		for _, member := range membersByPath {
			members = append(members, member)
		}
		if len(members) == 0 {
			continue
		}
		sort.Slice(members, func(i, j int) bool {
			si := candidateScore(members[i])
			sj := candidateScore(members[j])
			if si != sj {
				return si > sj
			}
			return members[i].Path < members[j].Path
		})
		groups = append(groups, Group{
			Key:     key,
			Winner:  members[0],
			Members: members,
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Members) != len(groups[j].Members) {
			return len(groups[i].Members) > len(groups[j].Members)
		}
		return groups[i].Key < groups[j].Key
	})
	return groups
}

func BuildWinnerMap(candidates []PathCandidate) map[string]string {
	groups := BuildGroups(candidates)
	out := map[string]string{}
	for _, group := range groups {
		winner := strings.TrimSpace(group.Winner.Path)
		if winner == "" {
			continue
		}
		for _, member := range group.Members {
			path := strings.TrimSpace(member.Path)
			if path == "" {
				continue
			}
			out[path] = winner
		}
	}
	return out
}

func candidateScore(candidate PathCandidate) int {
	score := candidate.Quality * 100
	path := strings.TrimSpace(candidate.Path)
	name := normalizeKey(candidate.Name)
	slug := normalizeKey(lastSegment(path))
	if slug != "" && name != "" && slug == name {
		score += 300
	}
	if name != "" && slug != "" && (strings.Contains(name, slug) || strings.Contains(slug, name)) {
		score += 160
	}
	segments := 0
	if path != "" {
		segments = len(strings.Split(strings.Trim(path, "/"), "/"))
	}
	if segments > 0 {
		score -= segments * 4
	}
	score -= len(path) / 6
	if strings.Contains(path, "/assembly/") {
		score -= 20
	}
	if strings.Contains(path, "/mechanical/") {
		score -= 10
	}
	return score
}

func normalizeKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	lastSpace := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	parts := strings.Fields(b.String())
	for i, part := range parts {
		parts[i] = singularize(part)
	}
	return strings.Join(parts, " ")
}

func lastSegment(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func singularize(value string) string {
	switch {
	case len(value) > 4 && strings.HasSuffix(value, "ies"):
		return strings.TrimSuffix(value, "ies") + "y"
	case len(value) > 4 && strings.HasSuffix(value, "sses"):
		return strings.TrimSuffix(value, "es")
	case len(value) > 3 && strings.HasSuffix(value, "xes"):
		return strings.TrimSuffix(value, "es")
	case len(value) > 3 && strings.HasSuffix(value, "ses"):
		return strings.TrimSuffix(value, "s")
	case len(value) > 3 && strings.HasSuffix(value, "s") && !strings.HasSuffix(value, "ss"):
		return strings.TrimSuffix(value, "s")
	default:
		return value
	}
}

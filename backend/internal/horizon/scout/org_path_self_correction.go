package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type orgPathUpgrade struct {
	OldPath string
	NewPath string
	Reason  string
}

type orgEdgeCandidate struct {
	Index              int
	EdgeID             string
	RelationType       string
	Direction          string
	OtherFragmentID    string
	OtherCanonicalPath string
	OtherLabel         string
	CreatedAt          time.Time
	Criticality        float64
}

type orgEdgeCleanupDecision struct {
	DeleteIndices []int  `json:"delete_indices"`
	Rationale     string `json:"rationale,omitempty"`
}

func normalizeISO2(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 2 {
		return ""
	}
	if s[0] < 'a' || s[0] > 'z' || s[1] < 'a' || s[1] > 'z' {
		return ""
	}
	return s
}

func inferOrgISO2(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if v := normalizeISO2(*fragment.HomeLocation.ISO3166_1); v != "" {
			return v
		}
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"countryIso2", "jurisdictionIso2", "country", "jurisdiction"} {
			if v := normalizeISO2(fragment.Identifiers[key]); v != "" {
				return v
			}
		}
	}
	return ""
}

func orgCanonicalizationLabel(fragment *models.HorizonFragment, oldPath string) string {
	if fragment == nil {
		return ""
	}
	if fragment.Identifiers != nil {
		if v := strings.TrimSpace(fragment.Identifiers["legalName"]); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
		return v
	}
	if fragment.Identifiers != nil {
		if v := strings.TrimSpace(fragment.Identifiers["shortName"]); v != "" {
			return v
		}
	}
	oldPath = strings.TrimSpace(oldPath)
	if oldPath == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(oldPath, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	slug := strings.TrimSpace(parts[len(parts)-1])
	if slug == "" {
		return ""
	}
	return strings.ReplaceAll(slug, "-", " ")
}

func (e *ScoutEngine) maybeUpgradeLegacyOrgPath(_ context.Context, fragment *models.HorizonFragment, effectiveCanonicalPath string) *orgPathUpgrade {
	if fragment == nil || fragment.Class != ah.FragmentClassOrg {
		return nil
	}
	oldPath := strings.TrimSpace(effectiveCanonicalPath)
	if oldPath == "" {
		oldPath = strings.TrimSpace(fragment.CanonicalPath)
	}
	if oldPath == "" {
		return nil
	}
	if strings.HasPrefix(oldPath, "/gov/") {
		return nil
	}
	isLegacy := isLegacyOrgPath(oldPath)
	isOrgNa := strings.HasPrefix(oldPath, "/org/na/")
	if !isLegacy && !isOrgNa {
		return nil
	}

	label := orgCanonicalizationLabel(fragment, oldPath)
	if label == "" {
		return nil
	}
	if looksLikeGovLabel(label) {
		iso2 := inferISO2FromLabel(label, fragment)
		if iso2 == "" {
			iso2 = "na"
		}
		newPath := taxonomy.GovCanonicalPath(iso2, label)
		if newPath != "" && strings.TrimSpace(newPath) != oldPath {
			if err := taxonomy.ValidateCanonicalPath(newPath); err == nil {
				return &orgPathUpgrade{
					OldPath: oldPath,
					NewPath: newPath,
					Reason:  "legacy_gov_path_upgrade",
				}
			}
		}
	}
	iso2 := inferOrgISO2(fragment)
	// Fallback: try to infer ISO2 from organization label (e.g., "Taiwan Semiconductor..." -> "tw")
	if iso2 == "" {
		iso2 = inferISO2FromLabel(label, fragment)
	}
	if iso2 == "" {
		iso2 = "na"
	}
	newPath := taxonomy.OrgCanonicalPath(iso2, label)
	if newPath == "" {
		return nil
	}
	if strings.TrimSpace(newPath) == oldPath {
		return nil
	}
	if err := taxonomy.ValidateCanonicalPath(newPath); err != nil {
		return nil
	}
	return &orgPathUpgrade{
		OldPath: oldPath,
		NewPath: newPath,
		Reason:  "legacy_org_path_upgrade",
	}
}

func (e *ScoutEngine) orgEdgeCleanupWindowDays(job *models.ScoutingJob) int {
	if job != nil && job.State != nil {
		if v, ok := job.State["orgEdgeCleanupDays"]; ok {
			switch t := v.(type) {
			case int:
				if t > 0 {
					return t
				}
			case int64:
				if t > 0 {
					return int(t)
				}
			case float64:
				if t > 0 {
					return int(t)
				}
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && n > 0 {
					return n
				}
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("HORIZON_ORG_EDGE_CLEANUP_DAYS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 7
}

func (e *ScoutEngine) orgEdgeCleanupLimit(job *models.ScoutingJob) int {
	if job != nil && job.State != nil {
		if v, ok := job.State["orgEdgeCleanupLimit"]; ok {
			switch t := v.(type) {
			case int:
				if t > 0 {
					return t
				}
			case int64:
				if t > 0 {
					return int(t)
				}
			case float64:
				if t > 0 {
					return int(t)
				}
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && n > 0 {
					return n
				}
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("HORIZON_ORG_EDGE_CLEANUP_LIMIT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 120
}

func (e *ScoutEngine) collectOrgEdgeCleanupCandidates(ctx context.Context, fragmentID string, cutoff time.Time, limit int) []orgEdgeCandidate {
	if e == nil || e.edgesRepo == nil || e.fragmentsRepo == nil || fragmentID == "" {
		return nil
	}
	if limit <= 0 {
		limit = 120
	}
	edges := make([]*models.DependencyEdge, 0, limit*2)
	if out, err := e.edgesRepo.ListBySource(ctx, "global", fragmentID, nil, limit); err == nil && len(out) > 0 {
		edges = append(edges, out...)
	}
	if in, err := e.edgesRepo.ListByTarget(ctx, "global", fragmentID, nil, limit); err == nil && len(in) > 0 {
		edges = append(edges, in...)
	}
	if len(edges) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	filtered := make([]*models.DependencyEdge, 0, len(edges))
	for _, edge := range edges {
		if edge == nil || edge.ID == "" {
			continue
		}
		if _, ok := seen[edge.ID]; ok {
			continue
		}
		seen[edge.ID] = struct{}{}
		if !edge.Metadata.CreatedAt.IsZero() && edge.Metadata.CreatedAt.Before(cutoff) {
			continue
		}
		filtered = append(filtered, edge)
	}
	if len(filtered) == 0 {
		return nil
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Metadata.CreatedAt.After(filtered[j].Metadata.CreatedAt)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	fragCache := map[string]*models.HorizonFragment{}
	getFrag := func(id string) *models.HorizonFragment {
		if id == "" {
			return nil
		}
		if f, ok := fragCache[id]; ok {
			return f
		}
		f, err := e.fragmentsRepo.Get(ctx, id)
		if err != nil {
			fragCache[id] = nil
			return nil
		}
		fragCache[id] = f
		return f
	}

	out := make([]orgEdgeCandidate, 0, len(filtered))
	for _, edge := range filtered {
		var otherID string
		direction := "incoming"
		if edge.SourceFragmentID == fragmentID {
			otherID = edge.TargetFragmentID
			direction = "outgoing"
		} else {
			otherID = edge.SourceFragmentID
		}
		other := getFrag(otherID)
		candidate := orgEdgeCandidate{
			EdgeID:             edge.ID,
			RelationType:       string(edge.Relation),
			Direction:          direction,
			OtherFragmentID:    otherID,
			OtherCanonicalPath: "",
			OtherLabel:         "",
			CreatedAt:          edge.Metadata.CreatedAt,
			Criticality:        edge.Properties.Criticality,
		}
		if other != nil {
			candidate.OtherCanonicalPath = strings.TrimSpace(other.CanonicalPath)
			candidate.OtherLabel = strings.TrimSpace(other.Labels["en"])
		}
		out = append(out, candidate)
	}
	for i := range out {
		out[i].Index = i + 1
	}
	return out
}

func (e *ScoutEngine) runLegacyOrgEdgeCleanup(ctx context.Context, job *models.ScoutingJob, fragment *models.HorizonFragment, oldPath, newPath string) {
	if e == nil || job == nil || fragment == nil {
		return
	}
	if e.aiExecutor == nil || e.edgesRepo == nil || e.fragmentsRepo == nil {
		return
	}
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if oldPath == "" || newPath == "" || fragment.ID == "" {
		return
	}
	windowDays := e.orgEdgeCleanupWindowDays(job)
	if windowDays <= 0 {
		return
	}
	cutoff := time.Now().UTC().Add(-time.Duration(windowDays) * 24 * time.Hour)
	limit := e.orgEdgeCleanupLimit(job)
	candidates := e.collectOrgEdgeCleanupCandidates(ctx, fragment.ID, cutoff, limit)
	if len(candidates) == 0 {
		e.logLine(ctx, job, "info", fmt.Sprintf("> org edge cleanup skipped: no recent edges for %s", oldPath))
		return
	}

	prompt := buildOrgEdgeCleanupPrompt(fragment, oldPath, newPath, candidates, windowDays)
	verifier := ai.NewGenericVerifier(func(result orgEdgeCleanupDecision) error {
		for _, idx := range result.DeleteIndices {
			if idx < 1 || idx > len(candidates) {
				return fmt.Errorf("delete_indices contains out-of-range index %d", idx)
			}
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         12000,
			MaxOutputSize:            2048,
		},
		Verifier: verifier,
	}
	result, err := e.aiExecutor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		e.logLine(ctx, job, "warn", fmt.Sprintf("> org edge cleanup failed: %v", err))
		return
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		e.logLine(ctx, job, "warn", fmt.Sprintf("> org edge cleanup failed to parse response: %v", err))
		return
	}
	var decision orgEdgeCleanupDecision
	if err := json.Unmarshal(jsonData, &decision); err != nil {
		e.logLine(ctx, job, "warn", fmt.Sprintf("> org edge cleanup failed to unmarshal response: %v", err))
		return
	}

	if len(decision.DeleteIndices) == 0 {
		e.logLine(ctx, job, "info", fmt.Sprintf("> org edge cleanup: no deletions for %s (reviewed %d edges)", oldPath, len(candidates)))
		return
	}

	indices := map[int]struct{}{}
	for _, idx := range decision.DeleteIndices {
		indices[idx] = struct{}{}
	}
	deleted := 0
	for _, cand := range candidates {
		if _, ok := indices[cand.Index]; !ok {
			continue
		}
		if err := e.edgesRepo.Delete(ctx, cand.EdgeID); err != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("> org edge cleanup failed to delete edge %d (%s): %v", cand.Index, cand.EdgeID, err))
			continue
		}
		deleted++
	}
	if deleted > 0 {
		e.logLine(ctx, job, "info", fmt.Sprintf("> org edge cleanup: deleted %d/%d edges for legacy %s", deleted, len(candidates), oldPath))
	}
}

func buildOrgEdgeCleanupPrompt(fragment *models.HorizonFragment, oldPath, newPath string, candidates []orgEdgeCandidate, windowDays int) string {
	label := ""
	legal := ""
	short := ""
	desc := ""
	iso2 := ""
	if fragment != nil {
		label = strings.TrimSpace(fragment.Labels["en"])
		if fragment.Identifiers != nil {
			legal = strings.TrimSpace(fragment.Identifiers["legalName"])
			short = strings.TrimSpace(fragment.Identifiers["shortName"])
		}
		desc = strings.TrimSpace(fragment.Description)
		iso2 = inferOrgISO2(fragment)
	}

	var sb strings.Builder
	sb.WriteString("You are cleaning up edges after canonical path upgrades.\n")
	sb.WriteString("A legacy organization path was upgraded to a geo-scoped canonical path.\n")
	sb.WriteString("Some recent edges may be incorrect due to collisions/abbreviations (e.g., MIT linking to wrong entities).\n")
	sb.WriteString("Your task: choose which edges should be deleted.\n\n")
	sb.WriteString("ORG CONTEXT:\n")
	sb.WriteString(fmt.Sprintf("- oldCanonicalPath: %s\n", oldPath))
	sb.WriteString(fmt.Sprintf("- newCanonicalPath: %s\n", newPath))
	if label != "" {
		sb.WriteString(fmt.Sprintf("- label: %s\n", label))
	}
	if legal != "" {
		sb.WriteString(fmt.Sprintf("- legalName: %s\n", legal))
	}
	if short != "" {
		sb.WriteString(fmt.Sprintf("- shortName: %s\n", short))
	}
	if iso2 != "" {
		sb.WriteString(fmt.Sprintf("- inferredISO2: %s\n", iso2))
	}
	if desc != "" {
		sb.WriteString(fmt.Sprintf("- description: %s\n", desc))
	}
	sb.WriteString(fmt.Sprintf("- edgeWindowDays: %d\n\n", windowDays))

	sb.WriteString("EDGES (indexed list):\n")
	for _, c := range candidates {
		when := ""
		if !c.CreatedAt.IsZero() {
			when = c.CreatedAt.UTC().Format(time.RFC3339)
		}
		other := c.OtherCanonicalPath
		if other == "" {
			other = c.OtherFragmentID
		}
		labelPart := ""
		if c.OtherLabel != "" {
			labelPart = fmt.Sprintf(" label=%s", c.OtherLabel)
		}
		sb.WriteString(fmt.Sprintf("[%d] %s %s -> %s%s | createdAt=%s | criticality=%.2f\n",
			c.Index, c.Direction, c.RelationType, other, labelPart, when, c.Criticality))
	}

	sb.WriteString("\nDECISION RULES:\n")
	sb.WriteString("- Delete edges that are likely incorrect due to legacy path collisions or name ambiguity.\n")
	sb.WriteString("- Keep edges that clearly match the organization identity.\n")
	sb.WriteString("- If unsure, keep the edge (do not delete).\n\n")
	sb.WriteString("OUTPUT FORMAT (JSON only, no markdown):\n")
	sb.WriteString(`{
  "delete_indices": [1, 4],
  "rationale": "short explanation"
}
`)
	sb.WriteString("\nReturn ONLY valid JSON.\n")
	return sb.String()
}

func recordLegacyOrgPath(fragment *models.HorizonFragment, legacyPath string) {
	if fragment == nil {
		return
	}
	legacyPath = strings.TrimSpace(legacyPath)
	if legacyPath == "" {
		return
	}
	if fragment.Identifiers == nil {
		fragment.Identifiers = make(map[string]string)
	}
	if fragment.Identifiers["legacy_org_canonical_path"] == "" {
		fragment.Identifiers["legacy_org_canonical_path"] = legacyPath
	}
	existing := fragment.Identifiers["merged_paths"]
	if existing == "" {
		fragment.Identifiers["merged_paths"] = legacyPath
		return
	}
	for _, part := range strings.Split(existing, ",") {
		if strings.TrimSpace(part) == legacyPath {
			return
		}
	}
	fragment.Identifiers["merged_paths"] = existing + "," + legacyPath
}

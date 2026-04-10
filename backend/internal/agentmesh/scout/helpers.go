package scout

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

func parseContentObject(body map[string]any) map[string]any {
	content, _ := body["content"].(string)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return parseJSONObject(content)
}

func parseJSONObject(raw string) map[string]any {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err == nil {
		return out
	}
	return nil
}

func stringValue(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func floatValue(input map[string]any, key string, fallback float64) float64 {
	if input == nil {
		return fallback
	}
	switch value := input[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return fallback
	}
}

func boolValue(input map[string]any, key string, fallback bool) bool {
	if input == nil {
		return fallback
	}
	value, ok := input[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func stringSliceValue(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return compactStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return compactStrings(out)
	default:
		return nil
	}
}

func mapSlice(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			out = append(out, object)
		}
	}
	return out
}

func mergeStringMaps(base map[string]string, patch map[string]string) map[string]string {
	if len(patch) == 0 {
		return base
	}
	if base == nil {
		base = map[string]string{}
	}
	for k, v := range patch {
		if strings.TrimSpace(k) == "" {
			continue
		}
		base[k] = v
	}
	return base
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeRequest(req InvestigationRequest) InvestigationRequest {
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.LensID = strings.TrimSpace(req.LensID)
	req.ScoutingJobID = strings.TrimSpace(req.ScoutingJobID)
	req.Query = strings.TrimSpace(req.Query)
	req.TargetCanonicalPath = strings.TrimSpace(req.TargetCanonicalPath)
	req.Goal = normalizeGoalFrame(req.Goal)
	req.Environment = normalizeEnvironmentFrame(req.Environment)
	req.Role = normalizeRoleFrame(req.Role)
	req.Completeness = normalizeCompletenessFrame(req.Completeness)
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	return req
}

func normalizeGoalFrame(goal GoalFrame) GoalFrame {
	goal.Objective = strings.TrimSpace(goal.Objective)
	goal.PrimaryQuestion = strings.TrimSpace(goal.PrimaryQuestion)
	goal.UserIntent = strings.TrimSpace(goal.UserIntent)
	goal.TargetCanonicalPath = strings.TrimSpace(goal.TargetCanonicalPath)
	goal.WriteDisposition = strings.TrimSpace(goal.WriteDisposition)
	goal.OutputContract = compactStrings(goal.OutputContract)
	goal.SuccessCriteria = compactStrings(goal.SuccessCriteria)
	goal.StopConditions = compactStrings(goal.StopConditions)
	return goal
}

func normalizeEnvironmentFrame(env EnvironmentFrame) EnvironmentFrame {
	env.WorkspaceKind = strings.TrimSpace(env.WorkspaceKind)
	env.UnitOfWork = strings.TrimSpace(env.UnitOfWork)
	env.PrimaryObjects = compactStrings(env.PrimaryObjects)
	env.EntitySemantics = compactStrings(env.EntitySemantics)
	env.EdgeSemantics = compactStrings(env.EdgeSemantics)
	env.ArtifactSemantics = compactStrings(env.ArtifactSemantics)
	env.AvailableActions = compactStrings(env.AvailableActions)
	env.KnownContextRefs = compactStrings(env.KnownContextRefs)
	env.MetadataFields = compactStrings(env.MetadataFields)
	if env.TargetEntity == nil {
		env.TargetEntity = map[string]any{}
	}
	if len(env.ObservedRelations) == 0 {
		env.ObservedRelations = nil
	}
	return env
}

func normalizeRoleFrame(role RoleFrame) RoleFrame {
	role.Domain = strings.TrimSpace(role.Domain)
	role.PrimaryRole = strings.TrimSpace(role.PrimaryRole)
	role.SupportingRoles = compactStrings(role.SupportingRoles)
	role.ProfessionalQualityBar = strings.TrimSpace(role.ProfessionalQualityBar)
	role.InvestigationPosture = strings.TrimSpace(role.InvestigationPosture)
	role.RoleRationale = strings.TrimSpace(role.RoleRationale)
	return role
}

func normalizeCompletenessFrame(frame CompletenessFrame) CompletenessFrame {
	frame.BroadScanQuestion = strings.TrimSpace(frame.BroadScanQuestion)
	frame.ExplorationStatus = strings.TrimSpace(frame.ExplorationStatus)
	frame.KnownAlternativeRoutes = compactStrings(frame.KnownAlternativeRoutes)
	frame.MissingAngles = compactStrings(frame.MissingAngles)
	frame.CompletionRationale = strings.TrimSpace(frame.CompletionRationale)
	return frame
}

func modelTaskInput(task *contracts.MeshTask) map[string]any {
	if task == nil || task.Input == nil {
		return map[string]any{}
	}
	return task.Input
}

// enrichScoutModelInput ensures Vertex ExecuteTask sees a stable parent investigation session:
// copies input, sets parentSessionRef from task.SessionRef or existing input, never drops it.
func enrichScoutModelInput(input map[string]any, task *contracts.MeshTask) map[string]any {
	out := map[string]any{}
	for k, v := range input {
		out[k] = v
	}
	ref := ""
	if task != nil {
		ref = strings.TrimSpace(task.SessionRef)
	}
	if ref == "" {
		ref = strings.TrimSpace(stringValue(out, "parentSessionRef"))
	}
	if ref != "" {
		out["parentSessionRef"] = ref
	}
	return out
}

func stringSliceContains(slice []string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, item := range slice {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

func makeDeterministicKey(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			normalized = append(normalized, part)
		}
	}
	sum := sha1.Sum([]byte(strings.Join(normalized, "|")))
	return hex.EncodeToString(sum[:12])
}

func childLeaseOwner(parentTaskID, capability string) string {
	base := strings.TrimSpace(parentTaskID)
	if base == "" {
		base = "agentic-scout"
	}
	return base + ":" + strings.TrimSpace(capability)
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func marshalForPrompt(v any) string {
	raw, _ := json.MarshalIndent(v, "", "  ")
	return string(raw)
}

func maybeSearchKnowledgeBase(ctx context.Context, vertexSearch knowledgeBaseSearcher, tenantID, query string, pageSize int) []map[string]any {
	if vertexSearch == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	if pageSize <= 0 {
		pageSize = 6
	}
	resp, err := vertexSearch.Search(ctx, tenantID, query, pageSize)
	if err != nil || len(resp) == 0 {
		return nil
	}
	return resp
}

type knowledgeBaseSearcher interface {
	Search(ctx context.Context, tenantID, query string, pageSize int) ([]map[string]any, error)
}

// humanizeSearchQuery strips canonical paths and system-internal syntax from a
// query so that it reads like a question a human analyst would type into Google.
func humanizeSearchQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Replace canonical path fragments (e.g. /product/component/chemical/refining/lithium-carbonate)
	// with the human-readable tail token.
	parts := strings.Fields(raw)
	var out []string
	for _, part := range parts {
		if strings.HasPrefix(part, "/") && strings.Count(part, "/") >= 2 {
			segments := strings.Split(strings.Trim(part, "/"), "/")
			tail := segments[len(segments)-1]
			tail = strings.ReplaceAll(tail, "-", " ")
			tail = strings.ReplaceAll(tail, "_", " ")
			out = append(out, tail)
		} else {
			out = append(out, part)
		}
	}
	result := strings.Join(out, " ")
	result = strings.TrimSpace(result)
	if result == "" {
		return raw
	}
	return result
}

func summarizeTraceDigest(path map[string]any) string {
	product, _ := path["productRef"].(string)
	suppliers := stringSliceValue(path["suppliers"])
	customers := stringSliceValue(path["customers"])
	legs := int(floatValue(path, "pathCount", 0))
	return fmt.Sprintf("product=%s suppliers=%d customers=%d paths=%d", strings.TrimSpace(product), len(suppliers), len(customers), legs)
}

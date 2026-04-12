package vertex

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

const defaultMemoryFetchLimit = 8

type MemoryRecord struct {
	Name       string            `json:"name,omitempty"`
	Fact       string            `json:"fact,omitempty"`
	Scope      map[string]string `json:"scope,omitempty"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
	Confidence float64           `json:"confidence,omitempty"`
}

func (s *Service) EnsureMemoryBank(ctx context.Context, task *contracts.MeshTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is nil")
	}
	if strings.TrimSpace(task.MemoryBankRef) != "" {
		return task.MemoryBankRef, nil
	}
	ref := strings.TrimRight(s.reasoningEngine, "/") + "/memories"
	if _, err := s.doVertexJSON(ctx, http.MethodGet, resourceURL(ref), nil); err != nil {
		return "", fmt.Errorf("vertex memory bank is not available: %w", err)
	}
	return ref, nil
}

func (s *Service) FetchMemories(ctx context.Context, task *contracts.MeshTask, query string) ([]MemoryRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("vertex service is not configured")
	}
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	scope := memoryScope(task)
	payload := map[string]any{
		"scope": scope,
	}
	if trimmedQuery := strings.TrimSpace(query); trimmedQuery != "" {
		payload["similaritySearchParams"] = map[string]any{
			"searchQuery": trimmedQuery,
			"topK":        defaultMemoryFetchLimit,
		}
	} else {
		payload["simpleRetrievalParams"] = map[string]any{
			"pageSize": defaultMemoryFetchLimit,
		}
	}
	response, err := s.doVertexJSON(ctx, http.MethodPost, resourceURL(s.reasoningEngine)+"/memories:retrieve", payload)
	if err != nil {
		return nil, err
	}
	return trustedMemoriesFromResponse(response, task), nil
}

func (s *Service) GenerateMemoriesFromSession(ctx context.Context, task *contracts.MeshTask) error {
	if task == nil || strings.TrimSpace(task.SessionRef) == "" {
		return fmt.Errorf("task with sessionRef is required")
	}
	_, err := s.doVertexJSON(ctx, http.MethodPost, resourceURL(s.reasoningEngine)+"/memories:generate", map[string]any{
		"vertexSessionSource": map[string]any{
			"session": task.SessionRef,
		},
		"scope": memoryScope(task),
		"config": map[string]any{
			"waitForCompletion": false,
			"metadata": map[string]string{
				"capability": task.Capability,
				"taskId":     task.ID,
				"tenant":     task.TenantID,
				"sessionRef": task.SessionRef,
			},
		},
	})
	return err
}

func (s *Service) GenerateAndFetchMemoriesFromSession(ctx context.Context, task *contracts.MeshTask, query string) ([]MemoryRecord, error) {
	if err := s.GenerateMemoriesFromSession(ctx, task); err != nil {
		return nil, err
	}
	memories, err := s.FetchMemories(ctx, task, query)
	if err != nil {
		return nil, err
	}
	return memories, nil
}

func memoryScope(task *contracts.MeshTask) map[string]string {
	scope := map[string]string{
		"user_id":    firstNonEmpty(task.TenantID, "global"),
		"capability": strings.TrimSpace(task.Capability),
	}
	if strings.TrimSpace(task.TenantID) != "" {
		scope["tenant"] = strings.TrimSpace(task.TenantID)
	}
	return scope
}

func trustedMemoriesFromResponse(response map[string]any, task *contracts.MeshTask) []MemoryRecord {
	items := firstArray(response, "retrievedMemories", "memories", "results")
	out := make([]MemoryRecord, 0, len(items))
	for _, item := range items {
		memoryMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if nested, ok := memoryMap["memory"].(map[string]any); ok {
			memoryMap = mergeMaps(nested, memoryMap)
		}
		record, ok := trustedMemoryFromMap(memoryMap, task)
		if !ok {
			continue
		}
		out = append(out, record)
	}
	return out
}

func trustedMemoryFromMap(raw map[string]any, task *contracts.MeshTask) (MemoryRecord, bool) {
	scope := stringMapFromAny(raw["scope"])
	metadata := mapFromAny(raw["metadata"])
	if len(metadata) == 0 {
		return MemoryRecord{}, false
	}
	expectedUser := firstNonEmpty(task.TenantID, "global")
	if got := firstNonEmpty(scope["user_id"], scope["userId"], scope["tenant"]); got != "" && !strings.EqualFold(got, expectedUser) {
		return MemoryRecord{}, false
	}
	if got := firstNonEmpty(scope["capability"], stringFromAny(metadata["capability"])); got != "" && !strings.EqualFold(got, task.Capability) {
		return MemoryRecord{}, false
	}
	if status := strings.ToLower(firstNonEmpty(stringFromAny(metadata["reviewStatus"]), stringFromAny(raw["reviewStatus"]))); status == "rejected" {
		return MemoryRecord{}, false
	}
	if firstNonEmpty(stringFromAny(metadata["taskId"]), stringFromAny(metadata["sessionRef"]), stringFromAny(metadata["source"])) == "" {
		return MemoryRecord{}, false
	}
	fact := firstNonEmpty(stringFromAny(raw["fact"]), stringFromAny(raw["text"]), stringFromAny(raw["content"]))
	if fact == "" {
		return MemoryRecord{}, false
	}
	return MemoryRecord{
		Name:       firstNonEmpty(stringFromAny(raw["name"]), stringFromAny(raw["id"])),
		Fact:       fact,
		Scope:      scope,
		Metadata:   metadata,
		Confidence: floatFromAny(raw["confidence"]),
	}, true
}

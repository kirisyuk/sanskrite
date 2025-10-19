package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

// AIExecHandler exposes universal structured-task execution via ModelManager + TaskExecutor
type AIExecHandler struct {
	exec *ai.AITaskExecutor
}

func NewAIExecHandler(exec *ai.AITaskExecutor) *AIExecHandler { return &AIExecHandler{exec: exec} }

type structuredRequest struct {
	Prompt             string `json:"prompt"`
	MinCapabilityScore int    `json:"minCapabilityScore"`
	MaxOutputSize      int    `json:"maxOutputSize"`
}

type structuredResponse struct {
	OK     bool        `json:"ok"`
	Result interface{} `json:"result"`
}

// Structured executes a prompt through the standard executor with JSON-only verification
// POST /api/v1/ai/structured
func (h *AIExecHandler) Structured(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	// Auth required
	if GetAuthInfo(r.Context()) == nil {
		_ = WriteUnauthorized(w, "Authentication required")
		return
	}
	var req structuredRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Prompt) == 0 {
		_ = WriteBadRequest(w, "INVALID_JSON", "prompt is required")
		return
	}

	minScore := req.MinCapabilityScore
	if minScore <= 0 {
		minScore = 6
	}
	maxOutput := req.MaxOutputSize
	if maxOutput <= 0 {
		maxOutput = 1024
	}

	// Compose instruction to force JSON-only output
	composed := req.Prompt + "\n\nReturn strictly JSON only. Do not include markdown code fences or extra text."

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: composed,
		AIRequirements: ai.AIRequirements{
			MinContextWindow:         4000,
			MaxOutputSize:            maxOutput,
			MinCapabilityScore:       minScore,
			TextGenerationCapability: true,
		},
		Verifier:        ai.NewSimpleJSONVerifier(),
		EnableTelemetry: true,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	parsed, err := h.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		_ = WriteError(w, http.StatusBadGateway, "STRUCTURED_TASK_ERROR", err.Error(), nil)
		return
	}
	_ = WriteJSON(w, http.StatusOK, structuredResponse{OK: true, Result: parsed})
}

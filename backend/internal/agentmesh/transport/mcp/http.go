package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
)

type Tool interface {
	Name() string
	Description() string
	Call(ctx context.Context, caller contracts.AgentSpec, params map[string]any) (map[string]any, error)
}

type Handler struct {
	tools      map[string]Tool
	registry   *registry.Service
	policy     *policy.Service
	runtime    *runtime.Service
	store      *repo.Store
	leaseOwner string
}

func NewHandler(registrySvc *registry.Service, policySvc *policy.Service, runtimeSvc *runtime.Service, store *repo.Store, leaseOwner string) *Handler {
	h := &Handler{
		tools:      map[string]Tool{},
		registry:   registrySvc,
		policy:     policySvc,
		runtime:    runtimeSvc,
		store:      store,
		leaseOwner: strings.TrimSpace(leaseOwner),
	}
	h.registerTool(taskGetTool{store: store})
	h.registerTool(vertexGenerateTool{runtime: runtimeSvc, store: store, leaseOwner: h.leaseOwner})
	return h
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/mcp", h.handle)
}

func (h *Handler) registerTool(tool Tool) {
	h.tools[tool.Name()] = tool
}

func (h *Handler) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMCPError(w, nil, -32601, "method not allowed")
		return
	}
	var req struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      any            `json:"id"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPError(w, nil, -32700, "invalid json")
		return
	}
	if req.Method == "initialize" {
		writeMCPResult(w, req.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"serverInfo": map[string]any{
				"name":    "AgentMesh MCP",
				"version": "v1",
			},
		})
		return
	}

	caller, err := h.parseSignedCard(r)
	if err != nil {
		writeMCPError(w, req.ID, -32001, err.Error())
		return
	}

	switch req.Method {
	case "tools/list":
		tools := make([]map[string]any, 0, len(h.tools))
		for _, tool := range h.tools {
			if err := h.policy.EvaluateToolInvocation(*caller, tool.Name()); err == nil {
				tools = append(tools, map[string]any{
					"name":        tool.Name(),
					"description": tool.Description(),
				})
			}
		}
		writeMCPResult(w, req.ID, map[string]any{"tools": tools})
	case "tools/call":
		name, _ := req.Params["name"].(string)
		tool, ok := h.tools[strings.TrimSpace(name)]
		if !ok {
			writeMCPError(w, req.ID, -32601, "tool not found")
			return
		}
		if err := h.policy.EvaluateToolInvocation(*caller, tool.Name()); err != nil {
			writeMCPError(w, req.ID, -32003, err.Error())
			return
		}
		args, _ := req.Params["arguments"].(map[string]any)
		out, err := tool.Call(r.Context(), *caller, args)
		if err != nil {
			writeMCPError(w, req.ID, -32000, err.Error())
			return
		}
		writeMCPResult(w, req.ID, out)
	default:
		writeMCPError(w, req.ID, -32601, "method not found")
	}
}

func (h *Handler) parseSignedCard(r *http.Request) (*contracts.AgentSpec, error) {
	encoded := strings.TrimSpace(r.Header.Get("X-Agentmesh-Agent-Card"))
	if encoded == "" {
		return nil, httpError("missing X-Agentmesh-Agent-Card header")
	}
	return h.registry.ParseAndValidateEncodedCard(r.Context(), encoded)
}

type taskGetTool struct{ store *repo.Store }

func (t taskGetTool) Name() string        { return "agentmesh.task.get" }
func (t taskGetTool) Description() string { return "Fetch an AgentMesh task by ID." }
func (t taskGetTool) Call(ctx context.Context, _ contracts.AgentSpec, params map[string]any) (map[string]any, error) {
	taskID, _ := params["taskId"].(string)
	task, err := t.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":         task.ID,
		"tenantId":   task.TenantID,
		"status":     task.Status,
		"capability": task.Capability,
		"output":     task.Output,
		"review":     task.ReviewOutput,
		"usage":      task.Usage,
	}, nil
}

type vertexGenerateTool struct {
	runtime    *runtime.Service
	store      *repo.Store
	leaseOwner string
}

func (t vertexGenerateTool) Name() string { return "vertex.generate" }
func (t vertexGenerateTool) Description() string {
	return "Generate structured output using Vertex-hosted Gemini through AgentMesh governance."
}

func (t vertexGenerateTool) Call(ctx context.Context, caller contracts.AgentSpec, params map[string]any) (map[string]any, error) {
	input, _ := params["input"].(map[string]any)
	capability, _ := params["capability"].(string)
	tenantID, _ := params["tenantId"].(string)
	dedupKey, _ := params["dedupKey"].(string)
	approvalRequired, _ := params["approvalRequired"].(bool)
	task, err := t.runtime.CreateTask(ctx, &contracts.MeshTask{
		TenantID:         strings.TrimSpace(tenantID),
		Capability:       strings.TrimSpace(capability),
		ApprovalRequired: approvalRequired,
		Input:            input,
		Lease: contracts.Lease{
			DedupKey: strings.TrimSpace(dedupKey),
		},
	}, policy.RequestContext{
		CallerAgentID: caller.ID,
		DataZone:      string(caller.TrustZone),
		ModelAlias:    "flash",
		ToolName:      "vertex.generate",
	}, true)
	if err != nil {
		return nil, err
	}
	if err := t.runtime.ExecuteTask(ctx, task.ID, fallbackLeaseOwner(t.leaseOwner)); err != nil {
		return nil, err
	}
	updated, err := t.store.GetTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"taskId":           updated.ID,
		"status":           updated.Status,
		"output":           updated.Output,
		"artifacts":        updated.Artifacts,
		"requiresApproval": updated.Status == contracts.TaskStatusAwaitingReview,
	}, nil
}

func fallbackLeaseOwner(in string) string {
	if strings.TrimSpace(in) != "" {
		return strings.TrimSpace(in)
	}
	return "agentmesh-mcp"
}

type httpError string

func (e httpError) Error() string { return string(e) }

func writeMCPResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func writeMCPError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

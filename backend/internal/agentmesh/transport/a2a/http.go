package a2a

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/policy"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/repo"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
)

type Handler struct {
	registry *registry.Service
	runtime  *runtime.Service
	store    *repo.Store
}

func NewHandler(registrySvc *registry.Service, runtimeSvc *runtime.Service, store *repo.Store) *Handler {
	return &Handler{registry: registrySvc, runtime: runtimeSvc, store: store}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/agent-card.json", h.handleAgentCard)
	mux.HandleFunc("/a2a/card", h.handleAgentCard)
	mux.HandleFunc("/a2a/tasks", h.handleTasks)
	mux.HandleFunc("/a2a/tasks/", h.handleTaskByID)
}

func (h *Handler) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cards := h.registry.ListAgents()
	writeJSON(w, http.StatusOK, map[string]any{
		"version": "1.0.0",
		"cards":   cards,
	})
}

func (h *Handler) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	reqCard, err := h.parseSignedCard(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var body struct {
		TenantID         string         `json:"tenantId"`
		Capability       string         `json:"capability"`
		RequestedAgentID string         `json:"requestedAgentId,omitempty"`
		DedupKey         string         `json:"dedupKey,omitempty"`
		ApprovalRequired bool           `json:"approvalRequired,omitempty"`
		Input            map[string]any `json:"input,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	task := &contracts.MeshTask{
		TenantID:         strings.TrimSpace(body.TenantID),
		Capability:       strings.TrimSpace(body.Capability),
		RequestedAgentID: strings.TrimSpace(body.RequestedAgentID),
		ApprovalRequired: body.ApprovalRequired,
		Input:            body.Input,
		Lease: contracts.Lease{
			DedupKey: strings.TrimSpace(body.DedupKey),
		},
	}
	created, err := h.runtime.CreateTask(r.Context(), task, policy.RequestContext{
		CallerAgentID: reqCard.ID,
		DataZone:      "internal",
		ModelAlias:    "flash",
	}, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

func (h *Handler) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, err := h.parseSignedCard(r); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/a2a/tasks/")
	action := ""
	switch {
	case strings.HasSuffix(path, "/result"):
		action = "result"
		path = strings.TrimSuffix(path, "/result")
	case strings.HasSuffix(path, ":approve"):
		action = "approve"
		path = strings.TrimSuffix(path, ":approve")
	case strings.HasSuffix(path, ":reject"):
		action = "reject"
		path = strings.TrimSuffix(path, ":reject")
	case strings.HasSuffix(path, ":cancel"):
		action = "cancel"
		path = strings.TrimSuffix(path, ":cancel")
	}
	taskID := strings.TrimSpace(path)
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}
	if action == "approve" || action == "reject" || action == "cancel" {
		h.handleTaskAction(w, r, taskID, action)
		return
	}
	task, err := h.store.GetTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if action == "result" {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":        task.ID,
			"status":    task.Status,
			"output":    task.Output,
			"artifacts": task.Artifacts,
		})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) handleTaskAction(w http.ResponseWriter, r *http.Request, taskID, action string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Actor     string `json:"actor,omitempty"`
		Rationale string `json:"rationale,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	actor := strings.TrimSpace(body.Actor)
	if actor == "" {
		actor = strings.TrimSpace(r.Header.Get("X-Agentmesh-Actor"))
	}
	if actor == "" {
		actor = "internal"
	}
	rationale := strings.TrimSpace(body.Rationale)
	var err error
	switch action {
	case "approve":
		if rationale == "" {
			rationale = "approved"
		}
		err = h.runtime.ResolveReview(r.Context(), taskID, actor, rationale, true)
	case "reject":
		if rationale == "" {
			rationale = "rejected"
		}
		err = h.runtime.ResolveReview(r.Context(), taskID, actor, rationale, false)
	case "cancel":
		if rationale == "" {
			rationale = "cancelled"
		}
		err = h.runtime.CancelTask(r.Context(), taskID, actor, rationale)
	default:
		err = httpError("unsupported action")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	task, getErr := h.store.GetTask(r.Context(), taskID)
	if getErr != nil {
		writeError(w, http.StatusNotFound, getErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) parseSignedCard(r *http.Request) (*contracts.AgentSpec, error) {
	encoded := strings.TrimSpace(r.Header.Get("X-Agentmesh-Agent-Card"))
	if encoded == "" {
		return nil, httpError("missing X-Agentmesh-Agent-Card header")
	}
	card, err := h.registry.ParseAndValidateEncodedCard(r.Context(), encoded)
	if err != nil {
		return nil, httpError(err.Error())
	}
	return card, nil
}

func EncodeAgentCard(spec contracts.AgentSpec) (string, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

type httpError string

func (e httpError) Error() string { return string(e) }

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

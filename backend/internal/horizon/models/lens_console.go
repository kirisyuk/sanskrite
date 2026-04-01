package models

import "time"

const (
	LensConsoleToolObserveGraph   = "observe_graph"
	LensConsoleToolReadObject     = "read_object"
	LensConsoleToolFindObjects    = "find_objects"
	LensConsoleToolAnswer         = "answer"
	LensConsoleToolResearch       = "research"
	LensConsoleToolWebSearch      = "web_search"
	LensConsoleToolCompose        = "compose"
	LensConsoleToolUpdate         = "update"
	LensConsoleToolUpdateMetrics  = "update_metrics"
	LensConsoleToolPreviewChanges = "preview_changes"
	LensConsoleToolApplyChanges   = "apply_changes"
)

const (
	LensConsoleStatusCompleted            = "completed"
	LensConsoleStatusFailed               = "failed"
	LensConsoleStatusRequiresConfirmation = "requires_confirmation"
)

type LensConsoleToolRunRequest struct {
	Tool           string         `json:"tool"`
	Input          map[string]any `json:"input,omitempty"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	Confirm        bool           `json:"confirm,omitempty"`
	PublishEvents  *bool          `json:"publishEvents,omitempty"`
}

type LensConsoleToolRunResponse struct {
	RunID                string         `json:"runId"`
	Tool                 string         `json:"tool"`
	Status               string         `json:"status"`
	RequiresConfirmation bool           `json:"requiresConfirmation,omitempty"`
	Preview              map[string]any `json:"preview,omitempty"`
	Result               map[string]any `json:"result,omitempty"`
	Warnings             []string       `json:"warnings,omitempty"`
	LensID               string         `json:"lensId"`
	ProjectID            string         `json:"projectId"`
	StartedAt            time.Time      `json:"startedAt"`
	CompletedAt          *time.Time     `json:"completedAt,omitempty"`
}

type LensConsoleGraphSummary struct {
	NodeCount          int            `json:"nodeCount"`
	EdgeCount          int            `json:"edgeCount"`
	ObjectCountsByType map[string]int `json:"objectCountsByType,omitempty"`
	SelectedNodeIDs    []string       `json:"selectedNodeIds,omitempty"`
	VisibleNodeIDs     []string       `json:"visibleNodeIds,omitempty"`
}

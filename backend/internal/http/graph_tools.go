package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// GraphToolExecutor executes graph-related tools for AI
type GraphToolExecutor struct {
	interactions *repo.InteractionsRepository
	publisher    Publisher
	tenantID     string
	projectID    string
	streamID     string
	logger       *log.Logger
}

// GraphData represents the structure of graph data
type GraphData struct {
	ChartTitle string          `json:"chartTitle"`
	ChartType  string          `json:"chartType,omitempty"`
	Table      [][]interface{} `json:"table"`
	Sources    []string        `json:"sources"`
}

// NewGraphToolExecutor creates a new executor for graph tools
func NewGraphToolExecutor(
	interactions *repo.InteractionsRepository,
	publisher Publisher,
	tenantID, projectID, streamID string,
	logger *log.Logger,
) *GraphToolExecutor {
	return &GraphToolExecutor{
		interactions: interactions,
		publisher:    publisher,
		tenantID:     tenantID,
		projectID:    projectID,
		streamID:     streamID,
		logger:       logger,
	}
}

// IsAllowed checks if a tool is allowed
func (e *GraphToolExecutor) IsAllowed(name string) bool {
	allowed := map[string]bool{
		"create_graph": true,
		"update_graph": true,
	}
	return allowed[name]
}

// Execute executes a tool call
func (e *GraphToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "create_graph":
		return e.createGraph(ctx, args)
	case "update_graph":
		return e.updateGraph(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// validateGraphData validates the structure of graph data
func (e *GraphToolExecutor) validateGraphData(gd *GraphData) error {
	if gd.ChartTitle == "" {
		return fmt.Errorf("chartTitle is required")
	}
	if len(gd.Table) < 2 {
		return fmt.Errorf("table must have at least 2 rows (header + at least one data row)")
	}
	if len(gd.Table[0]) < 2 {
		return fmt.Errorf("table header row must have at least 2 columns")
	}
	// Validate first cell of header is empty string
	if gd.Table[0][0] != "" && gd.Table[0][0] != nil {
		return fmt.Errorf("first cell of table header must be empty string")
	}
	// Validate chartType
	allowedTypes := []string{"line", "bar", "area", "stackedBar", "stackedArea", "composed"}
	if gd.ChartType == "" {
		gd.ChartType = "line" // Default
	} else {
		valid := false
		for _, t := range allowedTypes {
			if gd.ChartType == t {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid chartType: %s, must be one of: %v", gd.ChartType, allowedTypes)
		}
	}
	// Validate sources
	if len(gd.Sources) == 0 {
		return fmt.Errorf("sources must be a non-empty array")
	}
	// Validate and normalize table structure: all rows should have same length
	headerLen := len(gd.Table[0])
	for i, row := range gd.Table[1:] {
		rowIdx := i + 1
		// Normalize row length: pad with empty strings if too short, trim if too long
		if len(row) < headerLen {
			// Pad row with empty strings to match header length
			for len(row) < headerLen {
				row = append(row, "")
			}
			gd.Table[rowIdx] = row
			if e.logger != nil {
				e.logger.Printf("[GraphToolExecutor] Normalized row %d: padded from %d to %d columns", rowIdx, len(row)-headerLen+len(row), headerLen)
			}
		} else if len(row) > headerLen {
			// Trim row to match header length
			gd.Table[rowIdx] = row[:headerLen]
			if e.logger != nil {
				e.logger.Printf("[GraphToolExecutor] Normalized row %d: trimmed from %d to %d columns", rowIdx, len(row), headerLen)
			}
		}
		// First column should be string (label), rest should be numbers
		if row[0] == nil {
			return fmt.Errorf("table row %d: first column (label) cannot be null", rowIdx)
		}
	}
	return nil
}

// createGraph creates a new graph interaction
func (e *GraphToolExecutor) createGraph(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		ChartTitle string          `json:"chartTitle"`
		ChartType  string          `json:"chartType,omitempty"`
		Table      [][]interface{} `json:"table"`
		Sources    []string        `json:"sources"`
		ReplyTo    *string         `json:"replyTo,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// Build GraphData
	gd := &GraphData{
		ChartTitle: params.ChartTitle,
		ChartType:  params.ChartType,
		Table:      params.Table,
		Sources:    params.Sources,
	}

	// Validate
	if err := e.validateGraphData(gd); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Serialize to JSON for content
	contentJSON, err := json.Marshal(gd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal graph data: %w", err)
	}
	contentStr := string(contentJSON)

	// Create interaction
	subkind := "graph"
	interaction := &repo.Interaction{
		TenantID:         e.tenantID,
		ProjectID:        e.projectID,
		Kind:             "assistant",
		Subkind:          &subkind,
		Content:          contentStr,
		ContentSanitized: &contentStr,
		Sources:          gd.Sources, // Also store at interaction level for compatibility
	}
	if params.ReplyTo != nil && *params.ReplyTo != "" {
		interaction.ReplyTo = params.ReplyTo
		interaction.Relations = []struct {
			Type     string `json:"type" firestore:"type"`
			TargetID string `json:"targetId" firestore:"targetId"`
		}{
			{Type: "answers", TargetID: *params.ReplyTo},
		}
	}

	interactionID, err := e.interactions.Create(ctx, interaction)
	if err != nil {
		return nil, fmt.Errorf("failed to create interaction: %w", err)
	}

	// Publish WebSocket event
	if e.publisher != nil {
		_ = e.publisher.PublishJSON(ctx, realtime.EventInteractionCreated, e.tenantID, map[string]any{
			"projectId":     e.projectID,
			"interactionId": interactionID,
			"kind":          "assistant",
			"subkind":       "graph",
			"ts":            time.Now().UnixMilli(),
		})
	}

	// Return full graph data so LLM can see what was created
	response := map[string]interface{}{
		"interactionId": interactionID,
		"chartTitle":    gd.ChartTitle,
		"chartType":     gd.ChartType,
		"table":         gd.Table,
		"sources":       gd.Sources,
		"status":        "created",
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	// Log tool result for debugging
	if e.logger != nil {
		e.logger.Printf("[GraphToolExecutor] create_graph result: interactionId=%s, chartTitle=%s, chartType=%s, tableRows=%d, sourcesCount=%d, responseJSON=%s", interactionID, gd.ChartTitle, gd.ChartType, len(gd.Table), len(gd.Sources), string(b))
	}

	return b, nil
}

// updateGraph updates an existing graph interaction
func (e *GraphToolExecutor) updateGraph(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		InteractionID string          `json:"interactionId"`
		ChartTitle    *string         `json:"chartTitle,omitempty"`
		ChartType     *string         `json:"chartType,omitempty"`
		Table         [][]interface{} `json:"table,omitempty"`
		Sources       []string        `json:"sources,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.InteractionID == "" {
		return nil, fmt.Errorf("interactionId is required")
	}

	// Get existing interaction
	existing, err := e.interactions.Get(ctx, params.InteractionID)
	if err != nil {
		return nil, fmt.Errorf("interaction not found: %w", err)
	}
	if existing.ProjectID != e.projectID || existing.TenantID != e.tenantID {
		return nil, fmt.Errorf("access denied")
	}
	if existing.Subkind == nil || *existing.Subkind != "graph" {
		return nil, fmt.Errorf("interaction is not a graph (subkind: %v)", existing.Subkind)
	}

	// Parse existing graph data
	var existingGD GraphData
	if existing.Content != "" {
		if err := json.Unmarshal([]byte(existing.Content), &existingGD); err != nil {
			// If parsing fails, create new structure
			existingGD = GraphData{
				ChartTitle: "",
				ChartType:  "line",
				Table:      [][]interface{}{},
				Sources:    []string{},
			}
		}
	}

	// Apply updates
	if params.ChartTitle != nil {
		existingGD.ChartTitle = *params.ChartTitle
	}
	if params.ChartType != nil {
		existingGD.ChartType = *params.ChartType
	}
	if len(params.Table) > 0 {
		existingGD.Table = params.Table
	}
	if len(params.Sources) > 0 {
		existingGD.Sources = params.Sources
	}

	// Validate updated data
	if err := e.validateGraphData(&existingGD); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Serialize updated data
	contentJSON, err := json.Marshal(existingGD)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal graph data: %w", err)
	}
	contentStr := string(contentJSON)

	// Update interaction
	updates := map[string]interface{}{
		"content":          contentStr,
		"contentSanitized": contentStr,
		"sources":          existingGD.Sources,
	}

	if err := e.interactions.Update(ctx, params.InteractionID, updates); err != nil {
		return nil, fmt.Errorf("failed to update interaction: %w", err)
	}

	// Publish WebSocket event
	if e.publisher != nil {
		_ = e.publisher.PublishJSON(ctx, realtime.EventInteractionUpdated, e.tenantID, map[string]any{
			"projectId":     e.projectID,
			"interactionId": params.InteractionID,
			"kind":          "assistant",
			"subkind":       "graph",
			"ts":            time.Now().UnixMilli(),
		})
	}

	// Return full graph data so LLM can see what was updated
	response := map[string]interface{}{
		"interactionId": params.InteractionID,
		"chartTitle":    existingGD.ChartTitle,
		"chartType":     existingGD.ChartType,
		"table":         existingGD.Table,
		"sources":       existingGD.Sources,
		"status":        "updated",
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return b, nil
}

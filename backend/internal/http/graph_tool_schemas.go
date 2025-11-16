package http

import gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"

// getGraphToolSchemas returns JSON Schema definitions for graph tools
func getGraphToolSchemas() []gen_gemini.Tool {
	return []gen_gemini.Tool{
		{
			Name:        "create_graph",
			Description: "Create a new graph/chart interaction. Use this when the user wants to visualize data as a chart. The graph will be displayed in the chat.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"chartTitle": map[string]interface{}{
						"type":        "string",
						"description": "Title of the chart (e.g., 'R&D Investment Trends')",
					},
					"chartType": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"line", "bar", "area", "stackedBar", "stackedArea", "composed"},
						"description": "Type of chart: 'line' (trends over time), 'bar' (discrete categories), 'area' (cumulative values), 'stackedBar' (composition), 'stackedArea' (cumulative contributions), 'composed' (mixed types). Default: 'line'",
					},
					"table": map[string]interface{}{
						"type":        "array",
						"description": "2D array representing chart data. First row is header: [\"\", \"X1\", \"X2\", ...] where first cell is empty and rest are X-axis labels. Subsequent rows are data series: [\"Label\", num1, num2, ...] where first cell is series label and rest are numeric values.",
						"items": map[string]interface{}{
							"type":        "array",
							"description": "Row array. First cell is the series label (or empty string for the header row). Remaining cells are X-axis labels (strings) or numeric values.",
							"minItems":    2,
							"items": map[string]interface{}{
								"description": "Header cells should be strings; data cells should be JSON numbers whenever possible (strings are accepted if numbers are not available).",
							},
						},
					},
					"sources": map[string]interface{}{
						"type":        "array",
						"description": "Array of source URLs where the data comes from (required, non-empty)",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"replyTo": map[string]interface{}{
						"type":        "string",
						"description": "Optional ID of the interaction this graph responds to",
					},
				},
				"required": []string{"chartTitle", "table", "sources"},
			},
		},
		{
			Name:        "update_graph",
			Description: "Update an existing graph/chart interaction. Use this to modify chart data, title, type, or sources of an existing graph.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"interactionId": map[string]interface{}{
						"type":        "string",
						"description": "ID of the graph interaction to update",
					},
					"chartTitle": map[string]interface{}{
						"type":        "string",
						"description": "New chart title (optional)",
					},
					"chartType": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"line", "bar", "area", "stackedBar", "stackedArea", "composed"},
						"description": "New chart type (optional)",
					},
					"table": map[string]interface{}{
						"type":        "array",
						"description": "New table data (optional, must match the structure described in create_graph). Provide header row followed by data rows. Cells can contain strings (for labels) or numbers.",
						"items": map[string]interface{}{
							"type":        "array",
							"description": "Row array with a label in the first cell. Header row should start with an empty string, subsequent rows start with the series label.",
							"minItems":    2,
							"items": map[string]interface{}{
								"description": "Header cells should be strings; data cells should be numeric values encoded as JSON numbers whenever possible (strings are accepted if needed).",
							},
						},
					},
					"sources": map[string]interface{}{
						"type":        "array",
						"description": "New sources array (optional)",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
				"required": []string{"interactionId"},
			},
		},
	}
}

package http

import gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"

// getCoreDocsToolSchemas returns JSON Schema definitions for CoreDocs tools
func getCoreDocsToolSchemas() []gen_gemini.Tool {
	return []gen_gemini.Tool{
		{
			Name:        "search_coredocs",
			Description: "Search CoreDocs using semantic vector search. Use this to find relevant documents when the user asks about content in documents.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query in natural language (e.g., 'In the document where we do exec research...')",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of documents to return (1-20, default: 5)",
						"minimum":     1,
						"maximum":     20,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "create_coredoc",
			Description: "Create a new CoreDoc document. Use this when the user wants to create or save a document.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filename": map[string]interface{}{
						"type":        "string",
						"description": "Filename for the document (e.g., 'research-plan.md')",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Full text content of the document",
					},
					"contentType": map[string]interface{}{
						"type":        "string",
						"description": "MIME type (e.g., 'text/markdown', 'text/plain')",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Optional description of the document",
					},
				},
				"required": []string{"filename", "content", "contentType"},
			},
		},
		{
			Name:        "update_coredoc",
			Description: "Update an entire CoreDoc document. Use this when the user wants to replace the entire content of a document.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"docId": map[string]interface{}{
						"type":        "string",
						"description": "ID of the document to update",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "New full text content (optional, if not provided other fields can still be updated)",
					},
					"contentType": map[string]interface{}{
						"type":        "string",
						"description": "New MIME type (optional)",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "New description (optional)",
					},
				},
				"required": []string{"docId"},
			},
		},
		{
			Name:        "update_coredoc_fragment",
			Description: "Perform fragmentary updates to a CoreDoc (replace, insert, append, prepend). Use this for partial edits when the user wants to modify only part of a document.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"docId": map[string]interface{}{
						"type":        "string",
						"description": "ID of the document to update",
					},
					"operation": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"replace", "insert", "append", "prepend"},
						"description": "Operation type: 'replace' (replace searchText with newText), 'insert' (insert newText at position), 'append' (add newText at end), 'prepend' (add newText at start)",
					},
					"searchText": map[string]interface{}{
						"type":        "string",
						"description": "Text to search for (required for 'replace' operation)",
					},
					"newText": map[string]interface{}{
						"type":        "string",
						"description": "New text to insert/replace/append/prepend",
					},
					"position": map[string]interface{}{
						"type":        "integer",
						"description": "Character position for 'insert' operation (0-based)",
					},
				},
				"required": []string{"docId", "operation", "newText"},
			},
		},
		{
			Name:        "list_coredocs",
			Description: "List all CoreDocs in the project. Use this to see what documents exist.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of documents to return (1-100, default: 50)",
						"minimum":     1,
						"maximum":     100,
					},
				},
			},
		},
		{
			Name:        "get_coredoc",
			Description: "Get the full content of a CoreDoc by its ID. Use this to read a document before editing it, or to see the current state of a document.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"docId": map[string]interface{}{
						"type":        "string",
						"description": "ID of the document to retrieve",
					},
				},
				"required": []string{"docId"},
			},
		},
	}
}


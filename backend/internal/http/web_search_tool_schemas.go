package http

import gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"

// getWebSearchToolSchemas returns JSON Schema definitions for web search tools
func getWebSearchToolSchemas() []gen_gemini.Tool {
	return []gen_gemini.Tool{
		getWebSearchToolSchema(),
	}
}


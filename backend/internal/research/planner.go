package research

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
)

// Planner generates sub-queries from the main research query
type Planner struct {
	genaiClient *gen_combined.Client
	model       string
}

// NewPlanner creates a new Planner instance
func NewPlanner(genaiClient *gen_combined.Client, model string) *Planner {
	return &Planner{
		genaiClient: genaiClient,
		model:       model,
	}
}

// Plan generates sub-queries to break down the research task
func (p *Planner) Plan(ctx context.Context, query string, maxQueries int) ([]string, error) {
	// Define JSON schema for array of strings
	jsonSchema := map[string]interface{}{
		"type": "array",
		"items": map[string]interface{}{
			"type": "string",
		},
		"minItems": 1,
		"maxItems": maxQueries,
	}

	currentDate := time.Now().Format("January 2, 2006")

	prompt := fmt.Sprintf(`You are a research assistant. Break down the following research query into %d focused sub-queries that will help gather comprehensive information.

Current Date: %s

Research Query: %s

Requirements:
- Each sub-query should be specific and focused on one aspect
- Sub-queries should cover different angles and perspectives
- Avoid redundancy between sub-queries
- Prioritize the most current and up-to-date materials available as of %s
- When formulating sub-queries, focus on recent developments, latest trends, and current information relevant to the current date
- Language requirements:
  * The PRIMARY language for sub-queries is English (most sub-queries should be in English)
  * If the original query is NOT in English, include up to 3 sub-queries in the original query's language
  * All other sub-queries must be in English
- You MUST return a strict JSON array of strings matching this exact schema:
  {
    "type": "array",
    "items": {
      "type": "string"
    },
    "minItems": 1,
    "maxItems": %d
  }
- Return ONLY the JSON array, no other text, no markdown, no code fences
- The response must be a valid JSON array of strings

Example format (if original query is in English):
["What is X?", "How does X work?", "What are the benefits of X?"]

Example format (if original query is in another language, e.g., Russian):
["What is X?", "How does X work?", "Какие преимущества у X?", "What are the challenges of X?", "What is the future of X?"]

Return the sub-queries as a strict JSON array of strings:`, maxQueries, currentDate, query, currentDate, maxQueries)

	req := gen_combined.GenAIRequest{
		Model:             p.model,
		Prompt:            prompt,
		StrictJSONEnabled: true,
		JSONSchema:        jsonSchema,
		MaxOutputTokens:   8192,
		Temperature:       0.3,
	}

	resp, err := p.genaiClient.GenAIGenerate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	// Parse JSON response
	var subQueries []string
	content := resp.Content

	// Log raw response for debugging
	log.Printf("[Planner] Raw response Content length: %d, RawJSON length: %d, ToolCalls: %d", len(content), len(resp.RawJSON), len(resp.ToolCalls))
	if len(content) > 0 && len(content) < 500 {
		log.Printf("[Planner] Content preview: %s", content)
	}

	// Check ToolCalls first (strict JSON mode may return data in tool calls)
	if len(resp.ToolCalls) > 0 {
		for _, tc := range resp.ToolCalls {
			if tc.Name == "return_structured" && tc.Arguments != "" {
				log.Printf("[Planner] Found return_structured tool call, using arguments")
				content = tc.Arguments
				break
			}
		}
	}

	// Prefer RawJSON if available (contains unmodified response)
	if resp.RawJSON != "" {
		// Try to extract JSON from RawJSON response
		var rawResp map[string]interface{}
		if err := json.Unmarshal([]byte(resp.RawJSON), &rawResp); err == nil {
			// Try to find the actual JSON array in the response structure
			if candidates, ok := rawResp["candidates"].([]interface{}); ok && len(candidates) > 0 {
				if candidate, ok := candidates[0].(map[string]interface{}); ok {
					if contentParts, ok := candidate["content"].(map[string]interface{}); ok {
						if parts, ok := contentParts["parts"].([]interface{}); ok && len(parts) > 0 {
							if part, ok := parts[0].(map[string]interface{}); ok {
								if text, ok := part["text"].(string); ok && text != "" {
									content = text
								}
								// Check for function call response
								if fc, ok := part["functionCall"].(map[string]interface{}); ok {
									if args, ok := fc["args"].(map[string]interface{}); ok {
										// Try to find array in args
										for _, v := range args {
											if arr, ok := v.([]interface{}); ok {
												// Convert to []string
												for _, item := range arr {
													if str, ok := item.(string); ok {
														subQueries = append(subQueries, str)
													}
												}
												if len(subQueries) > 0 {
													log.Printf("[Planner] Extracted %d sub-queries from function call args", len(subQueries))
													goto validate
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Try to extract JSON array from response
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	// Try direct JSON parsing
	if err := json.Unmarshal([]byte(content), &subQueries); err != nil {
		log.Printf("[Planner] Direct JSON parse failed: %v, content: %s", err, content[:min(200, len(content))])
		// Fallback: try to extract array from text
		start := strings.Index(content, "[")
		end := strings.LastIndex(content, "]")
		if start >= 0 && end > start {
			arrayStr := content[start : end+1]
			if err := json.Unmarshal([]byte(arrayStr), &subQueries); err != nil {
				log.Printf("[Planner] Extracted array parse failed: %v, array: %s", err, arrayStr)
				// Last resort: return original query as single sub-query
				log.Printf("[Planner] All parsing attempts failed, using original query")
				return []string{query}, nil
			}
		} else {
			// Last resort: return original query as single sub-query
			log.Printf("[Planner] No array brackets found, using original query")
			return []string{query}, nil
		}
	}

validate:
	// Validate and limit sub-queries
	if len(subQueries) == 0 {
		log.Printf("[Planner] No sub-queries extracted, using original query")
		return []string{query}, nil
	}

	// Filter out empty strings
	var validQueries []string
	for _, q := range subQueries {
		q = strings.TrimSpace(q)
		if q != "" && q != query {
			validQueries = append(validQueries, q)
		}
	}

	if len(validQueries) == 0 {
		log.Printf("[Planner] All sub-queries were empty or same as original, using original query")
		return []string{query}, nil
	}

	subQueries = validQueries
	// Don't limit to maxQueries yet - we'll add frame expansions first

	log.Printf("[Planner] Generated %d initial sub-queries: %v", len(subQueries), subQueries)

	// Always generate 3-5 additional frame expansion sub-queries
	// Target: if we have 5 sub-queries, add 3-5 more to get 8-10 total
	additionalCount := 5 // Default to 5 additional queries
	if len(subQueries) >= 5 {
		// If we already have 5+, generate 3-5 more
		additionalCount = 5
	} else if len(subQueries) >= 3 {
		// If we have 3-4, generate enough to reach 8-10
		additionalCount = 8 - len(subQueries)
		if additionalCount < 3 {
			additionalCount = 3
		}
		if additionalCount > 5 {
			additionalCount = 5
		}
	} else {
		// If we have less than 3, generate 3-5 more
		additionalCount = 5
	}

	additionalQueries, err := p.generateFrameExpansions(ctx, query, subQueries, additionalCount)
	if err != nil {
		log.Printf("[Planner] Failed to generate frame expansions: %v", err)
	} else if len(additionalQueries) > 0 {
		// Combine with existing sub-queries
		subQueries = append(subQueries, additionalQueries...)
		log.Printf("[Planner] Added %d frame expansion queries, total: %d", len(additionalQueries), len(subQueries))
	}

	// Apply reasonable maximum limit (20) to prevent excessive queries
	const maxTotalQueries = 20
	if len(subQueries) > maxTotalQueries {
		subQueries = subQueries[:maxTotalQueries]
		log.Printf("[Planner] Limited to %d total queries", maxTotalQueries)
	}

	log.Printf("[Planner] Final sub-queries count: %d", len(subQueries))
	return subQueries, nil
}

// generateFrameExpansions generates additional sub-queries using frame expansion logic
func (p *Planner) generateFrameExpansions(ctx context.Context, originalQuery string, existingQueries []string, count int) ([]string, error) {
	// Build context from existing queries
	existingQueriesText := ""
	if len(existingQueries) > 0 {
		for i, q := range existingQueries {
			existingQueriesText += fmt.Sprintf("%d. %s\n", i+1, q)
		}
	}

	// Determine language from original query (simple heuristic)
	queryLang := "English"
	if len(originalQuery) > 0 {
		// Check if query contains non-ASCII characters (likely not English)
		hasNonASCII := false
		for _, r := range originalQuery {
			if r > 127 {
				hasNonASCII = true
				break
			}
		}
		if hasNonASCII {
			queryLang = "the original query's language"
		}
	}

	jsonSchema := map[string]interface{}{
		"type": "array",
		"items": map[string]interface{}{
			"type": "string",
		},
		"minItems": count,
		"maxItems": count,
	}

	prompt := fmt.Sprintf(`You are an expert at analyzing research queries and identifying ways to expand their frame or view them from alternative perspectives.

Task:
1. Look at the original research query and the sub-queries already generated
2. Generate %d additional concise, focused research sub-queries that suggest alternative angles or expansions
3. These should complement the existing sub-queries by offering different perspectives or deeper angles

Original Research Query: %s

Existing Sub-queries:
%s

STYLE REQUIREMENTS:
- Maximum 15-20 words per question
- Use natural, research-oriented language
- One clear idea per question
- Avoid complex nested clauses or long lists
- Don't use meta-phrases like "expanding the frame" - just suggest the alternative directly
- Focus on research questions that would gather complementary information

QUESTION FORMAT:
- Use simple question structures: "What about...?", "How does...?", "What are the...?", "What is the impact of...?"
- Keep it brief and direct
- If mentioning examples, limit to 1-2 items maximum
- Write as research questions, not conversational

AVOID:
- Redundancy with existing sub-queries
- Long sentences with multiple clauses
- Explaining reasons or conditions within the question
- Questions longer than 20 words
- Complex nested structures

CRITICAL REQUIREMENTS:
- Avoid superficial suggestions that are generic or obvious
- Focus on meaningful, context-specific expansions that genuinely add depth
- Each suggestion should be substantive and relevant to the original query
- Do not duplicate angles already covered in existing sub-queries

Language requirements:
- The PRIMARY language for additional sub-queries is English (most should be in English)
- If the original query is NOT in English, include up to 2 sub-queries in %s
- All other sub-queries must be in English

You MUST return a strict JSON array of strings matching this exact schema:
{
  "type": "array",
  "items": {
    "type": "string"
  },
  "minItems": %d,
  "maxItems": %d
}
- Return ONLY the JSON array, no other text, no markdown, no code fences
- The response must be a valid JSON array of strings

Return the additional sub-queries as a strict JSON array of strings:`, count, originalQuery, existingQueriesText, queryLang, count, count)

	req := gen_combined.GenAIRequest{
		Model:             p.model,
		Prompt:            prompt,
		StrictJSONEnabled: true,
		JSONSchema:        jsonSchema,
		MaxOutputTokens:   2048,
		Temperature:       0.5, // Slightly higher temperature for more creative expansions
	}

	resp, err := p.genaiClient.GenAIGenerate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate frame expansions: %w", err)
	}

	// Parse JSON response (similar to Plan method)
	var additionalQueries []string
	content := resp.Content

	// Check ToolCalls first
	if len(resp.ToolCalls) > 0 {
		for _, tc := range resp.ToolCalls {
			if tc.Name == "return_structured" && tc.Arguments != "" {
				content = tc.Arguments
				break
			}
		}
	}

	// Prefer RawJSON if available
	if resp.RawJSON != "" {
		var rawResp map[string]interface{}
		if err := json.Unmarshal([]byte(resp.RawJSON), &rawResp); err == nil {
			if candidates, ok := rawResp["candidates"].([]interface{}); ok && len(candidates) > 0 {
				if candidate, ok := candidates[0].(map[string]interface{}); ok {
					if contentParts, ok := candidate["content"].(map[string]interface{}); ok {
						if parts, ok := contentParts["parts"].([]interface{}); ok && len(parts) > 0 {
							if part, ok := parts[0].(map[string]interface{}); ok {
								if text, ok := part["text"].(string); ok && text != "" {
									content = text
								}
								if fc, ok := part["functionCall"].(map[string]interface{}); ok {
									if args, ok := fc["args"].(map[string]interface{}); ok {
										for _, v := range args {
											if arr, ok := v.([]interface{}); ok {
												for _, item := range arr {
													if str, ok := item.(string); ok {
														additionalQueries = append(additionalQueries, str)
													}
												}
												if len(additionalQueries) > 0 {
													goto validateExpansions
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Try to extract JSON array from response
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	// Try direct JSON parsing
	if err := json.Unmarshal([]byte(content), &additionalQueries); err != nil {
		// Fallback: try to extract array from text
		start := strings.Index(content, "[")
		end := strings.LastIndex(content, "]")
		if start >= 0 && end > start {
			arrayStr := content[start : end+1]
			if err := json.Unmarshal([]byte(arrayStr), &additionalQueries); err != nil {
				return nil, fmt.Errorf("failed to parse frame expansions JSON: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no array found in frame expansions response")
		}
	}

validateExpansions:
	// Filter out empty strings and duplicates with existing queries
	var validExpansions []string
	existingSet := make(map[string]bool)
	for _, q := range existingQueries {
		existingSet[strings.ToLower(strings.TrimSpace(q))] = true
	}

	for _, q := range additionalQueries {
		q = strings.TrimSpace(q)
		if q != "" && q != originalQuery {
			qLower := strings.ToLower(q)
			if !existingSet[qLower] {
				validExpansions = append(validExpansions, q)
				existingSet[qLower] = true
			}
		}
	}

	if len(validExpansions) == 0 {
		return nil, fmt.Errorf("no valid frame expansions generated")
	}

	// Limit to requested count
	if len(validExpansions) > count {
		validExpansions = validExpansions[:count]
	}

	return validExpansions, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

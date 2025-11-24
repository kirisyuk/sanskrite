package genai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	g "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
)

// ToolCallErrorType represents the type of error that occurred during tool execution
type ToolCallErrorType string

const (
	ErrorTypeValidation   ToolCallErrorType = "validation"   // Data validation error (e.g., table structure)
	ErrorTypeParsing      ToolCallErrorType = "parsing"      // JSON parsing or data format error
	ErrorTypePermission   ToolCallErrorType = "permission"    // Permission/authorization error
	ErrorTypeNotFound     ToolCallErrorType = "not_found"     // Resource not found
	ErrorTypeRateLimit    ToolCallErrorType = "rate_limit"    // Rate limit exceeded
	ErrorTypeService      ToolCallErrorType = "service"       // External service error
	ErrorTypeUnknown      ToolCallErrorType = "unknown"       // Unknown error type
	ErrorTypeUnrecoverable ToolCallErrorType = "unrecoverable" // Error that cannot be fixed by retry
)

// ToolCallError represents a structured error from tool execution
type ToolCallError struct {
	Type        ToolCallErrorType `json:"type"`
	Message     string            `json:"message"`
	ToolName    string            `json:"toolName"`
	Arguments   json.RawMessage   `json:"arguments"`
	Context     map[string]any    `json:"context,omitempty"`
	OriginalErr error             `json:"-"`
}

func (e *ToolCallError) Error() string {
	return e.Message
}

// ClassifyError analyzes an error and determines its type and whether it's recoverable
func ClassifyError(err error, toolName string, args json.RawMessage) *ToolCallError {
	if err == nil {
		return nil
	}

	errStr := err.Error()
	tcErr := &ToolCallError{
		ToolName:  toolName,
		Arguments: args,
		Message:   errStr,
		OriginalErr: err,
		Context:   make(map[string]any),
	}

	// Extract context from error message
	errLower := strings.ToLower(errStr)

	// Validation errors
	if strings.Contains(errLower, "validation failed") ||
		strings.Contains(errLower, "invalid") ||
		strings.Contains(errLower, "expected") ||
		strings.Contains(errLower, "must match") ||
		strings.Contains(errLower, "required") ||
		strings.Contains(errLower, "columns") ||
		strings.Contains(errLower, "rows") {
		tcErr.Type = ErrorTypeValidation
		tcErr.Context["recoverable"] = true
		return tcErr
	}

	// Parsing errors
	if strings.Contains(errLower, "parse") ||
		strings.Contains(errLower, "json") ||
		strings.Contains(errLower, "unmarshal") ||
		strings.Contains(errLower, "decode") {
		tcErr.Type = ErrorTypeParsing
		tcErr.Context["recoverable"] = true
		return tcErr
	}

	// Permission errors
	if strings.Contains(errLower, "permission") ||
		strings.Contains(errLower, "unauthorized") ||
		strings.Contains(errLower, "access denied") ||
		strings.Contains(errLower, "forbidden") {
		tcErr.Type = ErrorTypePermission
		tcErr.Context["recoverable"] = false
		return tcErr
	}

	// Not found errors
	if strings.Contains(errLower, "not found") ||
		strings.Contains(errLower, "does not exist") {
		tcErr.Type = ErrorTypeNotFound
		tcErr.Context["recoverable"] = false
		return tcErr
	}

	// Rate limit errors
	if strings.Contains(errLower, "rate limit") ||
		strings.Contains(errLower, "too many requests") {
		tcErr.Type = ErrorTypeRateLimit
		tcErr.Context["recoverable"] = true
		tcErr.Context["retryAfter"] = 5 // seconds
		return tcErr
	}

	// Service errors
	if strings.Contains(errLower, "service unavailable") ||
		strings.Contains(errLower, "timeout") ||
		strings.Contains(errLower, "connection") {
		tcErr.Type = ErrorTypeService
		tcErr.Context["recoverable"] = true
		return tcErr
	}

	// Default to unknown, assume recoverable for tool calls
	tcErr.Type = ErrorTypeUnknown
	tcErr.Context["recoverable"] = true
	return tcErr
}

// RetryableToolExecutor wraps a ToolExecutor with retry logic and self-correction
type RetryableToolExecutor struct {
	executor     g.ToolExecutor
	client       ClientInterface
	maxRetries   int
	onRetry      func(toolName string, attempt int, error *ToolCallError)
	onCorrection func(toolName string, attempt int, correctedArgs json.RawMessage)
}

// ClientInterface defines the interface needed for LLM-based correction
type ClientInterface interface {
	Generate(ctx context.Context, prompt string, tools []gen_combined.Tool, opts gen_combined.Options) (*gen_combined.GenerateResponse, error)
	GenerateJSONStrict(ctx context.Context, prompt string, tools []gen_combined.Tool, schema map[string]interface{}, opts gen_combined.Options) (*gen_combined.GenerateResponse, error)
}

// NewRetryableToolExecutor creates a new retryable executor wrapper
func NewRetryableToolExecutor(executor g.ToolExecutor, client ClientInterface) *RetryableToolExecutor {
	return &RetryableToolExecutor{
		executor:   executor,
		client:    client,
		maxRetries: 3,
	}
}

// SetCallbacks sets callbacks for retry and correction events
func (r *RetryableToolExecutor) SetCallbacks(
	onRetry func(toolName string, attempt int, error *ToolCallError),
	onCorrection func(toolName string, attempt int, correctedArgs json.RawMessage),
) {
	r.onRetry = onRetry
	r.onCorrection = onCorrection
}

// Execute executes a tool call with retry and self-correction logic
func (r *RetryableToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	attempt := 0
	var lastError *ToolCallError
	var lastArgs json.RawMessage = args

	for attempt <= r.maxRetries {
		if attempt > 0 {
			// Notify about retry
			if r.onRetry != nil {
				r.onRetry(name, attempt, lastError)
			}

			// Attempt self-correction
			correctedArgs, correctionErr := r.attemptCorrection(ctx, name, lastArgs, lastError)
			if correctionErr != nil {
				log.Printf("[RetryableToolExecutor] Self-correction failed: %v", correctionErr)
				// Continue with original args if correction fails
				correctedArgs = lastArgs
			} else {
				lastArgs = correctedArgs
				if r.onCorrection != nil {
					r.onCorrection(name, attempt, correctedArgs)
				}
			}
		}

		// Execute tool call
		result, err := r.executor.Execute(ctx, name, lastArgs)
		if err == nil {
			// Success
			return result, nil
		}

		// Classify error
		tcErr := ClassifyError(err, name, lastArgs)
		if tcErr == nil {
			// If we can't classify, return original error
			return nil, err
		}

		lastError = tcErr

		// Check if error is recoverable
		recoverable, ok := tcErr.Context["recoverable"].(bool)
		if !ok || !recoverable {
			log.Printf("[RetryableToolExecutor] Non-recoverable error: %s", tcErr.Message)
			return nil, err
		}

		// Check if we've exceeded max retries
		if attempt >= r.maxRetries {
			log.Printf("[RetryableToolExecutor] Max retries exceeded for tool %s: %s", name, tcErr.Message)
			return nil, fmt.Errorf("tool execution failed after %d attempts: %w", r.maxRetries+1, err)
		}

		attempt++

		// If rate limit, wait before retry
		if tcErr.Type == ErrorTypeRateLimit {
			retryAfter := 5
			if ra, ok := tcErr.Context["retryAfter"].(int); ok {
				retryAfter = ra
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(retryAfter) * time.Second):
			}
		}
	}

	return nil, fmt.Errorf("tool execution failed: %w", lastError.OriginalErr)
}

// attemptCorrection attempts to correct the tool call arguments using LLM
func (r *RetryableToolExecutor) attemptCorrection(ctx context.Context, toolName string, originalArgs json.RawMessage, toolErr *ToolCallError) (json.RawMessage, error) {
	if r.client == nil {
		return nil, fmt.Errorf("client not available for correction")
	}

	// Build correction prompt
	correctionPrompt := r.buildCorrectionPrompt(toolName, originalArgs, toolErr)

	// Get tool schema for context (if available)
	toolSchema := r.getToolSchema(toolName)

	// Call LLM for correction
	opts := gen_combined.Options{
		Temperature:     0.3, // Lower temperature for more deterministic corrections
		MaxOutputTokens: 2048,
		ModelOverride:   "gemini_fast", // Use fast model for corrections
	}

	// Use GenerateJSONStrict if we have a schema, otherwise regular Generate
	var resp *gen_combined.GenerateResponse
	var genErr error

	if toolSchema != nil && toolSchema.Parameters != nil {
		// Use tool schema parameters for strict JSON generation
		resp, genErr = r.client.GenerateJSONStrict(ctx, correctionPrompt, nil, toolSchema.Parameters, opts)
	} else {
		resp, genErr = r.client.Generate(ctx, correctionPrompt, nil, opts)
	}

	if genErr != nil {
		return nil, fmt.Errorf("correction request failed: %w", genErr)
	}

	// Extract corrected arguments from response
	correctedArgs, err := r.extractCorrectedArgs(resp, originalArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to extract corrected arguments: %w", err)
	}

	log.Printf("[RetryableToolExecutor] Correction successful for tool %s", toolName)
	return correctedArgs, nil
}

// buildCorrectionPrompt builds a prompt for LLM to correct the tool call
func (r *RetryableToolExecutor) buildCorrectionPrompt(toolName string, originalArgs json.RawMessage, toolErr *ToolCallError) string {
	var argsStr string
	if len(originalArgs) > 0 {
		argsStr = string(originalArgs)
	} else {
		argsStr = "{}"
	}

	var contextInfo strings.Builder
	if len(toolErr.Context) > 0 {
		contextInfo.WriteString("\nAdditional context:\n")
		for k, v := range toolErr.Context {
			contextInfo.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
		}
	}

	return fmt.Sprintf(`You are helping to correct a tool call that failed with an error.

Tool name: %s
Error type: %s
Error message: %s
Original arguments: %s%s

Your task:
1. Analyze the error and understand what went wrong
2. Correct the arguments to fix the issue
3. Return ONLY the corrected arguments as valid JSON
4. Ensure the corrected arguments match the tool's schema exactly
5. Do not add any explanation or markdown - just return the JSON object

Important:
- Preserve all valid fields from the original arguments
- Only fix the fields that caused the error
- Ensure data types match the schema requirements
- For table/array structures, ensure all rows have the same number of columns as the header

Return the corrected arguments as JSON:`, toolName, toolErr.Type, toolErr.Message, argsStr, contextInfo.String())
}

// getToolSchema retrieves the schema for a tool (if available)
func (r *RetryableToolExecutor) getToolSchema(toolName string) *gen_combined.Tool {
	// This would need to be passed in or retrieved from a registry
	// For now, return nil - schema will be extracted from error context if available
	return nil
}

// extractCorrectedArgs extracts corrected arguments from LLM response
func (r *RetryableToolExecutor) extractCorrectedArgs(resp *gen_combined.GenerateResponse, originalArgs json.RawMessage) (json.RawMessage, error) {
	// Try to parse content as JSON
	content := strings.TrimSpace(resp.Content)
	
	// Remove markdown code fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	// Try to extract JSON from content
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// Try to find JSON object in content using regex-like approach
		startIdx := strings.Index(content, "{")
		endIdx := strings.LastIndex(content, "}")
		if startIdx >= 0 && endIdx > startIdx {
			jsonStr := content[startIdx : endIdx+1]
			if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
				return nil, fmt.Errorf("failed to parse corrected arguments: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no valid JSON found in response")
		}
	}

	// Marshal back to JSON
	correctedJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal corrected arguments: %w", err)
	}

	return json.RawMessage(correctedJSON), nil
}

// IsAllowed delegates to underlying executor
func (r *RetryableToolExecutor) IsAllowed(name string) bool {
	return r.executor.IsAllowed(name)
}


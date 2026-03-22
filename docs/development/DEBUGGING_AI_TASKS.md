# Debugging AI Tasks - Context Timeout and Logging

## Problem Analysis

### Original Issue
When executing large AI structured tasks (e.g., 269KB+ input), all AI models were failing with "context canceled" error, causing a cascade of fallback attempts until exhausting all available models.

### Root Cause
1. **Missing Timeout Configuration**: In `meta.go`, HTTP request context was passed directly to AI tasks without explicit timeout
2. **Cloud Run Default Timeout**: Standard HTTP timeout (~30-60 seconds) was insufficient for large AI requests
3. **Insufficient Logging**: Error messages didn't provide enough context about request size and which step failed

### Example Log Output (Before Fix)
```
DEFAULT 2025-10-25T12:57:16.923128Z ExecuteStructuredTask called with task type: TextProcessing
DEFAULT 2025-10-25T12:57:16.923145Z AIModelManager SelectModel called with requirements: MinContextWindow=67398, InputTokens=269592
DEFAULT 2025-10-25T12:57:38.099283Z Error executing task with model gpt-5-nano. Message: failed to execute request: Post "https://api.openai.com/v1/chat/completions": context canceled. Attempting fallback.
DEFAULT 2025-10-25T12:57:38.103556Z All models tried or unavailable for structured task 'TextProcessing'.
```

## Changes Made

### 1. Added Timeout to AI Task Execution (`meta.go`)

All AI task execution methods now create an independent context with 120-second timeout:

```go
func (h *MetaHandler) runThinking(ctx context.Context, window string) (map[string]any, error) {
    // Create a context with timeout for AI task execution
    taskCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
    defer cancel()
    
    // ... task setup ...
    
    res, err := h.exec.ExecuteStructuredTask(taskCtx, task)
    // ... error handling ...
}
```

**Modified methods:**
- `classifyDialogControl()`
- `runThinking()`
- `runBiases()`
- `runDecisions()`
- `runComm()`
- `runLearning()`

### 2. Enhanced Logging in Task Executor (`task_executor.go`)

Added detailed logging at multiple levels with proper log severity:

**Before task execution:**
```go
log.Printf("[%s] Executing structured task with model %s (input: %d chars, %d tokens est)", 
    modelCapabilities.Provider, currentModel, inputChars, inputTokens)
```

**On task failure (WARNING level - fallback attempt):**
```go
log.Printf("WARNING: Task failed with model %s. Error: %v", currentModel, err)
log.Printf("WARNING: Request details: TaskType=%s, InputSize=%d chars (%d tokens est), Provider=%s", 
    task.TaskType, inputChars, inputTokens, modelCapabilities.Provider)

// If context was canceled, log a specific message
if ctx.Err() != nil {
    log.Printf("WARNING: Context error detected: %v. This usually means timeout or cancellation.", ctx.Err())
}
```

**When all models exhausted (ERROR level - final failure):**
```go
log.Printf("ERROR: All models tried or unavailable for structured task '%s'. Total models tried: %d, Input size: %d chars (%d tokens est)", 
    task.TaskType, len(triedModels), inputChars, inputTokens)
```

**Fixed token calculation:**
- Before: `inputTokens := len(task.InputContent)` (incorrect)
- After: `inputTokens := len(task.InputContent) / 4` (correct approximation)

### 3. Added Method-Level Logging (`meta.go`)

Each analyzer method now logs start and failure events with token estimation:

```go
if h.logger != nil {
    h.logger.Printf("meta: runThinking starting (windowChars=%d, promptChars=%d, tokensEst=%d)", 
        len(window), len(prompt), len(prompt)/4)
}
res, err := h.exec.ExecuteStructuredTask(taskCtx, task)
if err != nil {
    if h.logger != nil {
        h.logger.Printf("ERROR: meta: runThinking failed: %v", err)
    }
    return nil, err
}
```

**Added window size monitoring:**
```go
// Log window size for monitoring
if h.logger != nil {
    h.logger.Printf("meta: analyze request (messageCount=%d, windowChars=%d, windowTokensEst=%d)", 
        len(msgs), len(window), len(window)/4)
    if len(window) > 100000 {
        h.logger.Printf("WARNING: meta: large window detected (>100KB). Consider limiting message count.")
    }
}
```

## Expected Log Output (After Fix)

```
DEFAULT 2025-10-25T12:57:16.923128Z meta: analyze request (messageCount=50, windowChars=267943, windowTokensEst=66986)
DEFAULT 2025-10-25T12:57:16.923140Z WARNING: meta: large window detected (>100KB). Consider limiting message count.
DEFAULT 2025-10-25T12:57:16.923145Z meta: runThinking starting (windowChars=267943, promptChars=269592, tokensEst=67398)
DEFAULT 2025-10-25T12:57:16.923150Z ExecuteStructuredTask called with task type: TextProcessing
DEFAULT 2025-10-25T12:57:16.923172Z [OpenAI] Executing structured task with model gpt-5-nano (input: 269592 chars, 67398 tokens est)
DEFAULT 2025-10-25T12:57:38.099283Z WARNING: Task failed with model gpt-5-nano. Error: failed to execute request: Post "https://api.openai.com/v1/chat/completions": context canceled
DEFAULT 2025-10-25T12:57:38.099310Z WARNING: Request details: TaskType=TextProcessing, InputSize=269592 chars (67398 tokens est), Provider=OpenAI
DEFAULT 2025-10-25T12:57:38.099315Z WARNING: Context error detected: context deadline exceeded. This usually means timeout or cancellation.
DEFAULT 2025-10-25T12:57:38.099320Z WARNING: Error executing task with model gpt-5-nano. Message: context canceled. Attempting fallback.
DEFAULT 2025-10-25T12:57:39.100000Z ERROR: All models tried or unavailable for structured task 'TextProcessing'. Total models tried: 4, Input size: 269592 chars (67398 tokens est)
DEFAULT 2025-10-25T12:57:39.100010Z ERROR: meta: runThinking failed: unable to complete structured task 'TextProcessing': No suitable AI models available after all fallbacks (windowChars=267943)
```

## Benefits

1. **Independent Timeouts**: AI tasks now have their own 120-second timeout, independent of HTTP request lifecycle
2. **Better Debugging**: Clear visibility into:
   - Input size (chars/tokens) with correct estimation
   - Message count and window size
   - Selected model and provider
   - Exact point of failure
   - Context cancellation detection
3. **Proper Log Levels**:
   - **WARNING**: Fallback attempts (expected behavior)
   - **ERROR**: Final failures (requires attention)
4. **Failure Analysis**: Can now identify:
   - Whether input is too large (>100KB warning)
   - Which provider failed
   - If timeout is the issue
   - How many messages were analyzed

## Recommendations

### For Production Use

1. **Monitor Large Requests**: Set up alerts for requests > 200KB
2. **Consider Chunking**: For very large windows, consider splitting into multiple smaller requests
3. **Adjust Timeouts**: If still seeing timeouts, increase from 120s to 180s or 240s
4. **Model Selection**: Ensure model capabilities match input size requirements

### For Development

1. **Test with Various Sizes**: Test with inputs of different sizes (1KB, 10KB, 100KB, 500KB)
2. **Monitor Logs**: Watch for patterns in which models/providers fail more often
3. **Benchmark Models**: Track which models handle large inputs better

## Configuration Options

### Cloud Run Timeout (if needed)
If you need to increase Cloud Run's request timeout beyond default:

```yaml
# cloudbuild.yaml or cloud run config
apiVersion: serving.knative.dev/v1
kind: Service
spec:
  template:
    spec:
      timeoutSeconds: 300  # 5 minutes
```

### AI Task Timeout
Currently hardcoded to 120s, but can be made configurable:

```go
// In meta.go, could make this configurable
taskTimeout := 120 * time.Second
if h.config != nil && h.config.AITaskTimeout > 0 {
    taskTimeout = h.config.AITaskTimeout
}
taskCtx, cancel := context.WithTimeout(context.Background(), taskTimeout)
```

## Related Files

- `/Users/kirisyuk/cursor/sanskrite/backend/internal/ai/task_executor.go` - Core task execution logic
- `/Users/kirisyuk/cursor/sanskrite/backend/internal/http/meta.go` - Meta analysis HTTP handlers
- `/Users/kirisyuk/cursor/sanskrite/backend/internal/ai/verifiers.go` - Response verification logic
- [META_WINDOW_SIZE_ANALYSIS.md](../notes/META_WINDOW_SIZE_ANALYSIS.md) — размер контекстного окна и стратегии оптимизации


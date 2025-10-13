## AI Module

### Scope
- Abstraction over providers with model manager, task executor, verifiers, and service implementations.

### Key Code
- Interfaces: `backend/internal/ai/interfaces.go`
- Services: `backend/internal/ai/openai_service.go`, `claude_service.go`, `grok_service.go`, `vertex_service.go`
- Model manager: `backend/internal/ai/model_manager.go`, `models.go`
- Task executor: `backend/internal/ai/task_executor.go`
- Prompts: `backend/internal/prompts/prompts.go`
- Verifiers: `backend/internal/ai/verifiers.go`

### Flow
1. Caller requests AI operation via model manager or executor
2. Service selected (capabilities, config)
3. Prompt built and sent
4. Response validated by verifiers



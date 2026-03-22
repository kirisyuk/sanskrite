# Архитектура динамического планирования агентов

## Концепция

Агенты должны быть способны:
1. **Принимать естественный язык** и формировать план выполнения
2. **Репланировать** на основе результатов выполнения шагов
3. **Пробовать альтернативные подходы** при неудачах
4. **Отслеживать прогресс** относительно целевого результата
5. **Работать в двух режимах**: автономное планирование или явные шаги

## Изменения в моделях

### AgentJob - расширения

```go
type AgentJob struct {
    // ... существующие поля ...
    
    // Планирование
    UserQuery      string                 `json:"userQuery" firestore:"userQuery"`           // Исходный запрос на естественном языке
    Intent         map[string]interface{} `json:"intent,omitempty" firestore:"intent"`      // Понимание намерения (LLM)
    Goal           map[string]interface{} `json:"goal,omitempty" firestore:"goal"`           // Целевой результат (LLM)
    PlanMode       PlanMode               `json:"planMode" firestore:"planMode"`           // "auto" | "manual" | "hybrid"
    
    // Репланирование
    ReplanTriggered bool      `json:"replanTriggered" firestore:"replanTriggered"`          // Был ли запущен репланинг
    ReplanCount     int       `json:"replanCount" firestore:"replanCount"`                  // Количество репланов
    LastReplanAt    *time.Time `json:"lastReplanAt,omitempty" firestore:"lastReplanAt"`     // Время последнего реплана
}

type PlanMode string

const (
    PlanModeAuto   PlanMode = "auto"   // Полностью автономное планирование
    PlanModeManual PlanMode = "manual" // Явно заданные шаги (текущий режим)
    PlanModeHybrid PlanMode = "hybrid" // Начальный план + репланирование
)
```

### AgentStep - расширения

```go
type AgentStep struct {
    // ... существующие поля ...
    
    // Статус выполнения (детализированный)
    ExecutionStatus ExecutionStatus `json:"executionStatus" firestore:"executionStatus"` // "success" | "partial" | "failed" | "retry_needed"
    Confidence      float64         `json:"confidence,omitempty" firestore:"confidence"`  // Уверенность в результате (0.0-1.0)
    
    // Метаданные для репланирования
    Retryable       bool   `json:"retryable" firestore:"retryable"`                       // Можно ли повторить шаг
    AlternativeCapabilities []string `json:"alternativeCapabilities,omitempty" firestore:"alternativeCapabilities"` // Альтернативные capabilities
    Reason          string `json:"reason,omitempty" firestore:"reason"`                  // Причина успеха/неудачи
}

type ExecutionStatus string

const (
    ExecutionStatusSuccess    ExecutionStatus = "success"     // Полный успех
    ExecutionStatusPartial    ExecutionStatus = "partial"    // Частичный успех (нужны дополнительные шаги)
    ExecutionStatusFailed     ExecutionStatus = "failed"     // Провал
    ExecutionStatusRetryNeeded ExecutionStatus = "retry_needed" // Требуется повторная попытка
)
```

### CapabilityResult - расширения

```go
type CapabilityResult struct {
    // ... существующие поля ...
    
    // Детализированный статус
    ExecutionStatus ExecutionStatus `json:"executionStatus"` // Детальный статус выполнения
    Confidence      float64         `json:"confidence,omitempty"` // Уверенность в результате (0.0-1.0)
    Retryable       bool            `json:"retryable"` // Можно ли повторить
    Reason          string          `json:"reason,omitempty"` // Причина успеха/неудачи
    
    // Для репланирования
    SuggestsReplan  bool     `json:"suggestsReplan,omitempty"` // Рекомендует ли репланирование
    ReplanReason    string   `json:"replanReason,omitempty"`  // Причина репланирования
    AlternativeCapabilities []string `json:"alternativeCapabilities,omitempty"` // Альтернативные подходы
    
    // Динамические шаги
    SuggestedSteps  []SuggestedStep `json:"suggestedSteps,omitempty"` // Предложенные следующие шаги
}

type SuggestedStep struct {
    Capability string                 `json:"capability"`
    Input      map[string]interface{} `json:"input"`
    Reason     string                 `json:"reason"` // Почему этот шаг предложен
    Priority   int                    `json:"priority"` // Приоритет (меньше = выше)
}
```

## Новые Capabilities

### 1. Plan Generation (`plan_generate`)

Генерирует план выполнения из естественного языка.

**Input:**
```json
{
  "userQuery": "Проанализируй документ и предложи улучшения",
  "availableCapabilities": ["document_read", "llm_generate", "document_write", ...],
  "context": {...} // Контекст проекта, доступные документы и т.д.
}
```

**Output:**
```json
{
  "intent": {
    "action": "analyze_and_improve",
    "target": "document",
    "expectedOutcome": "improved_document_with_suggestions"
  },
  "goal": {
    "description": "Документ должен быть улучшен на основе анализа",
    "successCriteria": ["document_updated", "improvements_applied"]
  },
  "steps": [
    {
      "id": "step1",
      "capability": "document_read",
      "input": {"documentIds": ["doc-123"]},
      "reason": "Нужно прочитать документ для анализа"
    },
    {
      "id": "step2",
      "capability": "llm_generate",
      "input": {"prompt": "Проанализируй документ и предложи улучшения"},
      "dependsOn": ["step1"],
      "reason": "Анализ документа через LLM"
    },
    {
      "id": "step3",
      "capability": "document_write",
      "input": {"documentId": "doc-123", "content": "{{step2.content}}"},
      "dependsOn": ["step2"],
      "reason": "Применить улучшения к документу"
    }
  ]
}
```

### 2. Replanning (`replan`)

Корректирует план на основе результатов выполнения.

**Input:**
```json
{
  "currentPlan": {...}, // Текущий план
  "completedSteps": [...], // Выполненные шаги с результатами
  "failedSteps": [...], // Провалившиеся шаги
  "currentGoal": {...}, // Целевой результат
  "availableCapabilities": [...]
}
```

**Output:**
```json
{
  "updatedPlan": {
    "steps": [...], // Обновленный список шагов
    "reason": "Шаг X провалился, добавлен альтернативный подход Y"
  },
  "goalAchieved": false, // Достигнута ли цель
  "nextSteps": [...] // Следующие шаги для выполнения
}
```

### 3. Reflection (`reflect`)

Оценивает прогресс относительно цели.

**Input:**
```json
{
  "goal": {...}, // Целевой результат
  "completedSteps": [...], // Выполненные шаги
  "currentState": {...} // Текущее состояние системы
}
```

**Output:**
```json
{
  "progress": 0.75, // Прогресс (0.0-1.0)
  "goalAchieved": false,
  "missing": ["improvements_applied"], // Что еще нужно сделать
  "suggestions": [
    {
      "action": "add_step",
      "step": {...},
      "reason": "Нужно применить улучшения к документу"
    }
  ]
}
```

## Изменения в Orchestrator

### 1. Поддержка автономного планирования

```go
func (o *AgentOrchestrator) ExecuteJob(ctx context.Context, jobID string, onUpdate func(Update)) error {
    job, err := o.jobsRepo.Get(ctx, jobID)
    if err != nil {
        return err
    }
    
    // Если режим автономного планирования и план не создан
    if job.PlanMode == PlanModeAuto && len(job.Steps) == 0 {
        plan, err := o.generatePlan(ctx, job)
        if err != nil {
            return err
        }
        job.Steps = plan.Steps
        job.Intent = plan.Intent
        job.Goal = plan.Goal
        o.jobsRepo.Update(ctx, job)
    }
    
    // Выполнение с поддержкой репланирования
    return o.executeWithReplanning(ctx, job, onUpdate)
}
```

### 2. Репланирование на основе результатов

```go
func (o *AgentOrchestrator) executeWithReplanning(
    ctx context.Context,
    job *models.AgentJob,
    onUpdate func(Update),
) error {
    for {
        // Выполнить текущие шаги
        err := o.executeSteps(ctx, job, onUpdate)
        
        // Проверить, нужен ли репланинг
        if shouldReplan(job) {
            newPlan, err := o.replan(ctx, job)
            if err != nil {
                return err
            }
            
            // Добавить новые шаги к существующим
            job.Steps = append(job.Steps, newPlan.Steps...)
            job.ReplanCount++
            now := time.Now().UTC()
            job.LastReplanAt = &now
            o.jobsRepo.Update(ctx, job)
            
            continue // Повторить выполнение
        }
        
        // Проверить достижение цели
        if o.checkGoalAchieved(ctx, job) {
            break
        }
        
        // Если все шаги выполнены, но цель не достигнута - репланинг
        if allStepsCompleted(job) {
            newPlan, err := o.replan(ctx, job)
            if err != nil {
                return err
            }
            job.Steps = append(job.Steps, newPlan.Steps...)
            continue
        }
        
        break
    }
    
    return nil
}

func shouldReplan(job *models.AgentJob) bool {
    // Репланинг нужен если:
    // 1. Есть шаги с ExecutionStatusRetryNeeded
    // 2. Есть шаги с SuggestsReplan = true
    // 3. Частичный успех требует дополнительных шагов
    
    for _, step := range job.Steps {
        if step.ExecutionStatus == ExecutionStatusRetryNeeded {
            return true
        }
        // Проверка через CapabilityResult будет в executeStep
    }
    return false
}
```

### 3. Обработка детализированного статуса

```go
func (o *AgentOrchestrator) executeStep(...) error {
    // ... выполнение ...
    
    result, err := executor.Execute(ctx, stepCtx)
    if err != nil {
        // Обработка ошибки с учетом retryable
        if result != nil && result.Retryable {
            step.ExecutionStatus = ExecutionStatusRetryNeeded
            step.RetryCount++
            // Можно повторить позже
        } else {
            step.ExecutionStatus = ExecutionStatusFailed
        }
        // ...
    } else {
        // Успешное выполнение
        step.ExecutionStatus = result.ExecutionStatus // success | partial
        step.Confidence = result.Confidence
        step.Reason = result.Reason
        
        // Если результат частичный - может потребоваться репланинг
        if result.ExecutionStatus == ExecutionStatusPartial {
            // Добавить предложенные шаги
            if len(result.SuggestedSteps) > 0 {
                // Создать новые шаги из SuggestedSteps
            }
        }
        
        // Если предлагается репланинг
        if result.SuggestsReplan {
            // Пометить job для репланирования
        }
    }
    
    // ...
}
```

## Примеры использования

### 1. Автономное планирование

```go
job := &models.AgentJob{
    UserQuery: "Проанализируй документ и предложи улучшения",
    PlanMode:  PlanModeAuto,
    AutoMode:   true,
}

// Orchestrator автоматически:
// 1. Вызовет plan_generate для создания плана
// 2. Выполнит шаги
// 3. При необходимости репланирует
```

### 2. Ручное планирование (текущий режим)

```go
job := &models.AgentJob{
    UserQuery: "Проанализируй документ",
    PlanMode:  PlanModeManual,
    Steps: []models.AgentStep{
        {ID: "step1", Capability: "document_read", ...},
        {ID: "step2", Capability: "llm_generate", ...},
    },
}

// Orchestrator выполняет только указанные шаги
```

### 3. Гибридный режим

```go
job := &models.AgentJob{
    UserQuery: "Проанализируй документ и предложи улучшения",
    PlanMode:  PlanModeHybrid,
    Steps: []models.AgentStep{
        {ID: "step1", Capability: "document_read", ...},
    },
}

// Orchestrator:
// 1. Выполнит начальные шаги
// 2. На основе результатов сгенерирует дополнительные шаги
// 3. Может репланировать при необходимости
```

## Требования к Capability Executors

Все executors должны:

1. **Точно сообщать статус выполнения**:
   - `ExecutionStatus`: success | partial | failed | retry_needed
   - `Confidence`: уверенность в результате (0.0-1.0)
   - `Reason`: причина успеха/неудачи

2. **Указывать возможность повторной попытки**:
   - `Retryable`: можно ли повторить шаг
   - `AlternativeCapabilities`: альтернативные подходы

3. **Предлагать следующие шаги** (опционально):
   - `SuggestedSteps`: предложенные следующие шаги
   - `SuggestsReplan`: рекомендует ли репланирование

## Миграция

1. **Постепенное внедрение**: Сначала добавить новые поля как опциональные
2. **Обратная совместимость**: Старые jobs работают в `PlanModeManual`
3. **Новые capabilities**: `plan_generate`, `replan`, `reflect` добавляются постепенно


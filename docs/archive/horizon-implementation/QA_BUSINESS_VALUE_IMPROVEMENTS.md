# Улучшение бизнес-значения ответов Lens Q&A

> План системных изменений для устранения минусов: отсутствие количественных оценок, доли экспозиции, альтернатив и низкий business relevance.

---

## Текущие проблемы

| Проблема | Причина | Где исправлять |
|----------|---------|----------------|
| Нет количественных оценок | Amplitude 0–1 не маппится в %; инструкции не требуют диапазонов | ContextBuilder, Synthesizer, Decomposer |
| Доля Индонезии неясна | Нет ConcentrationEvidence в контексте | ContextBuilder, DomainSnapshot |
| Нет альтернатив | DomainSnapshot.Alternatives не передаётся; граф demo — одна цепочка | ContextBuilder, stub data |
| Business relevance 0.7 | Оценщик не штрафует vague language | RelevanceEvaluator |

---

## 1. Количественные оценки (Quantification)

### 1.1 Парсинг числовых параметров в Decomposer

**Цель:** "5% VAT increase" → `mechanismParams: {vat_delta_pct: 5}` в HypotheticalSignal.

**Изменения:**
- `QuestionPlan.HypotheticalSignals[].MechanismParams map[string]float64` — опционально
- Промпт Decomposer: "If question mentions numeric values (e.g. 5% VAT, 10% tariff), add mechanismParams: {\"vat_delta_pct\": 5} etc."
- Используется в ContextBuilder для quantitative hint

### 1.2 Quantitative hint в контексте

**Файл:** `backend/internal/horizon/qa/context_builder.go`

Добавить секцию после IMPACT BREAKDOWN:

```
=== QUANTITATIVE HINT (use for cost/tariff questions) ===
- Input: 5% VAT increase (from question / mechanismParams)
- Pass-through heuristic: amplitude at node ≈ cost sensitivity. High amplitude (0.6+) → 60–80% pass-through; moderate (0.3–0.6) → 30–50%; low (<0.3) → 10–30%
- For VAT 5% at PT Vale (amplitude 0.72): ~3.5–4% cost increase at nickel ore stage
- For EV battery cell (amplitude 0.12): ~0.5–1.5% at final product
- ALWAYS provide numeric ranges when mechanism permits. Avoid "likely", "potentially" without numbers.
```

Генерировать hint только когда:
- `plan.AnswerMode == "impact_assessment"`
- Есть `mechanismParams` с vat_delta_pct/tariff_pct или mechanism содержит vat/tariff
- Есть NodeImpacts с amplitude

### 1.3 Инструкции Synthesizer

**Файл:** `backend/internal/horizon/qa/context_builder.go` (INSTRUCTIONS)

Добавить:
```
- QUANTIFICATION: When the question asks about cost/price impact and QUANTITATIVE HINT is present, include specific ranges (e.g. "2–4% cost increase"). Do NOT use only "likely" or "potentially" — provide numbers.
```

---

## 2. Доля экспозиции (Concentration / Share)

### 2.1 Передача ConcentrationEvidence в контекст

**Файл:** `backend/internal/horizon/qa/context_builder.go`

В секции EXISTING ANALYSIS, если `domainSnapshot.ConcentrationEvidence` не пусто:

```
=== CONCENTRATION / MARKET SHARE ===
- Indonesia nickel: CR4=0.72, Indonesia share ~40% (from ConcentrationEvidence)
- Use to explain sensitivity: "Indonesia supplies ~40% of nickel ore, so VAT change has significant pass-through."
```

### 2.2 Stub / Demo

**Файл:** `backend/cmd/tools/horizon_qa_e2e/main.go`

В demo передавать не `nil`, а stub DomainAnswerSnapshot:

```go
domainSnapshot := &horizonmodels.DomainAnswerSnapshot{
    Thesis: horizonmodels.DomainThesis{
        Text: "Indonesia nickel supply chain shows moderate concentration risk.",
    },
    ConcentrationEvidence: []horizonmodels.ConcentrationEvidence{
        {MarketCanonicalPath: "/product/nickel-ore", CR4: ptr(0.72), Hint: "Indonesia ~40% of global nickel ore"},
    },
    Alternatives: []horizonmodels.Alternative{
        {Title: "Australian nickel (BHP, Ravensthorpe)", CostDelta: "+8% logistics", Feasibility: "medium"},
    },
    DataGaps: []horizonmodels.DataGap{
        {GapType: "throughput", Hint: "CATL Morowali actual throughput — last reported Q2 2025"},
    },
}
```

---

## 3. Альтернативы (Alternatives)

### 3.1 ContextBuilder: Alternatives из DomainSnapshot

**Файл:** `backend/internal/horizon/qa/context_builder.go`

В EXISTING ANALYSIS добавить:

```go
if len(domainSnapshot.Alternatives) > 0 {
    sb.WriteString("Alternatives: ")
    for i, alt := range domainSnapshot.Alternatives {
        if i > 0 { sb.WriteString("; ") }
        sb.WriteString(fmt.Sprintf("[%s: %s, cost %s]", alt.Title, alt.Feasibility, alt.CostDelta))
    }
    sb.WriteString("\n")
}
```

### 3.2 Инструкции: всегда предлагать альтернативы

В INSTRUCTIONS:
```
- ALTERNATIVES: If answer mode is impact_assessment or alternative_sourcing, suggest 1–3 actionable alternatives (e.g. Australian nickel, hedging, buffer stock). Use ALTERNATIVES from context if present; otherwise infer from graph structure (other deposits, other suppliers).
```

### 3.3 Обнаружение альтернатив из графа

**Долгосрочно:** Scout / MaterializedArtifacts могут содержать альтернативные пути (`/deposit/nickel/australia`). Если в scope есть несколько путей к одному продукту — это альтернативы. Можно добавить в ContextBuilder: "Alternative paths in scope: ..." на основе `observePathPatterns` и дублирующихся target nodes.

---

## 4. Business Relevance Evaluator

**Файл:** `backend/internal/horizon/qa/relevance_evaluator.go`

Усилить промпт:

```go
prompt := fmt.Sprintf(`Score how well this answer addresses the business question. Return ONLY a number 0.0-1.0.

QUESTION: %s

ANSWER: %s

Scoring criteria (penalize if missing):
- Direct answer to the question: does it address what was asked?
- Quantification: specific numbers/ranges (e.g. "2-4%%") > vague ("likely", "potentially")
- Alternatives: actionable options mentioned > none
- Evidence: grounded in data > generic statements
- Actionability: concrete next steps > abstract

Return a single number 0.0-1.0. No explanation.`, question, answerText)
```

---

## 5. Порядок внедрения

| Шаг | Компонент | Effort | Зависимости |
|-----|-----------|--------|-------------|
| 1 | ContextBuilder: quantitative hint (hardcoded для VAT 5% пока) | S | — |
| 2 | ContextBuilder: инструкции по quantification | S | 1 |
| 3 | Demo: stub DomainSnapshot с Alternatives, Concentration, DataGaps | S | — |
| 4 | ContextBuilder: Alternatives из DomainSnapshot | S | 3 |
| 5 | RelevanceEvaluator: усиленный промпт | S | — |
| 6 | Decomposer: mechanismParams (vat_delta_pct) | M | — |
| 7 | ContextBuilder: динамический quantitative hint из mechanismParams | M | 6 |
| 8 | ConcentrationEvidence в контекст (когда есть) | S | 3 |

Шаги 1–5 дают быстрый эффект без изменения Decomposer. Шаги 6–8 — полноценная количественная поддержка.

---

## 6. Ожидаемый результат

**До:**
> "A 5% VAT increase in Indonesia would likely lead to an increase in battery costs... potentially impacting... The propagation chain indicates..."

**После:**
> "A 5% VAT increase in Indonesia would raise EV battery cell costs by an estimated 2–4%, mainly through nickel ore (PT Vale Indonesia, ~3.5% at that stage). Indonesia supplies ~40% of global nickel ore, so the pass-through is significant. Alternatives: consider Australian nickel (BHP) — +8% logistics but lower policy risk; or increase cathode buffer stock (~6 weeks)."

# Horizon Risk/Opportunity/Trend Pipeline Technical Design

## 1. Problem Statement

Current risk semantics (`event -> signal -> applicability -> propagation -> rollup -> interpretation`) are mostly embedded inside Scout execution and auxiliary handlers.
This limits observability, deterministic replay, retry semantics, and stage-level explainability.

Target: introduce a dedicated asynchronous risk pipeline with the same operational maturity as `scout-research-critic-policy`, while reusing existing WS transport and deterministic core.

Key product constraints:

1. Cloud Tasks queue is separate from classic Scout queue.
2. WS channel remains the same transport, with dedicated risk event types.
3. End-to-end stage visibility (logs, state transitions, reasons).
4. Strong testability and post-mortem explainability.

## 2. Goals / Non-Goals

### Goals

1. Dedicated `RiskJob` lifecycle with explicit stages.
2. Idempotent execution with dedup keys and poison semantics.
3. Stage outputs are structured and machine-readable.
4. Shared correlation/session flow across queue worker and WS.
5. Incremental rollout with shadow mode.

### Non-Goals (v1)

1. Replacing Scout pipeline.
2. Replacing signal taxonomy or risk math primitives.
3. New WS transport layer.
4. LLM-first decision engine.

## 3. Semantic Canonicalization

## 3.1 Single semantic axis for scenario realism

To avoid dual-field ambiguity, runtime semantics use:

1. `modality` as the only realization axis:
  `rhetoric | potential | projected | escalation | disruption | confirmed | actual`
2. `likelihood` and `impact` as orthogonal numeric dimensions.
3. `timeHorizon` as temporal dimension.

`scenarioType` is not used in runtime contracts.

## 3.2 Backward compatibility mapping

If upstream payload contains `scenarioType`, convert at ingestion:

1. `rhetoric -> modality=rhetoric`
2. `escalation -> modality=escalation`
3. `disruption -> modality=disruption`
4. `confirmed -> modality=confirmed`

Unknown value:

1. `reasonCode=E_SEMANTIC_SCENARIO_UNKNOWN`
2. fallback `modality=potential`

## 4. Runtime Architecture

## 4.1 High-level flow

`RiskTrigger -> RiskJob.Create -> CloudTasks(risk-queue) -> RiskWorker -> StageExecutor[] -> Persist -> WS events -> Complete/Failed`

RiskTrigger sources:

1. New/updated `ontology_facts` (`event|signal`) in monitored scope.
2. Watchdog semantic threshold pass.
3. Feedback events (`re_evaluate`, `attention_escalation`).
4. Manual operator/API trigger.

## 4.2 Stage graph (v1)

1. `IDENTIFY`
2. `ESCALATE`
3. `APPLICABILITY`
4. `SEED`
5. `PROPAGATE`
6. `ROLLUP`
7. `INTERPRET`
8. `ALERT`

Terminal states:

`COMPLETED | FAILED | POISONED | CANCELLED`

## 4.3 Stage input/output contracts

Every stage receives `StageInput` and emits `StageOutput`.

`StageInput` minimum:

1. `jobId`
2. `tenantId`
3. `projectId`
4. `targetCanonicalPath`
5. `polarity`
6. `traceRef`
7. `attempt`
8. `state` (accumulated context)

`StageOutput` minimum:

1. `nextStatePatch` (delta-only)
2. `decision` (`pass|skip|fail`)
3. `reasonCodes[]`
4. `metrics` (durations/counters)
5. `artifacts` (optional ids/refs)

Stage-specific required outputs:

1. `IDENTIFY`: `signals[]`, `signalCount`, `signalQualityScore`
2. `ESCALATE`: `escalationLevel`, `escalationConfidence`, `evidenceBuckets`
3. `APPLICABILITY`: `applicableTargets[]`, `rejectedTargets[]`, `confidencePenalty`
4. `SEED`: `seedFragmentIds[]`, `seedCount`
5. `PROPAGATE`: `affectedFragmentIds[]`, `propagationPaths[]`
6. `ROLLUP`: `rollupValue`, `tailMode`, `tailLikelihood`, `tailImpact`, `topContributors[]`
7. `INTERPRET`: `interpretationId`, `narrativeQuality`, `anomalyFlags[]`
8. `ALERT`: `appetiteBreach`, `alertLevel`, `alertRecipients[]`, `suppressionReason`

## 4.4 Canonical reasonCode enum (v1)

Prefix convention:

1. `E_*` errors
2. `W_*` warnings
3. `I_*` informational decisions

Initial set:

1. `E_SEMANTIC_SCENARIO_UNKNOWN`
2. `E_STAGE_INPUT_INVALID`
3. `E_STAGE_TIMEOUT`
4. `E_DEPENDENCY_UNAVAILABLE`
5. `E_APPLICABILITY_NO_EXPOSURE_PATH`
6. `E_PROPAGATION_POLICY_MISSING`
7. `E_ALERT_POLICY_EVAL_FAILED`
8. `W_SPARSE_GRAPH_PENALTY_APPLIED`
9. `W_ESCALATION_LLM_SKIPPED`
10. `W_ROLLUP_FALLBACK_USED`
11. `I_STAGE_SKIPPED_NO_SIGNALS`
12. `I_ALERT_SUPPRESSED_BY_APPETITE`

## 5. Data Model

## 5.1 `RiskJob` (new)

Storage: Firestore collection `horizon_risk_jobs` (dedicated, not AgentJobs).

Core fields:

1. `id`
2. `tenantId`
3. `projectId`
4. `lensId` (optional)
5. `triggerType` (`watchdog|event|feedback|manual`)
6. `triggerRef`
7. `targetCanonicalPath`
8. `polarity` (`risk|opportunity`)
9. `currentStage`
10. `currentState`
11. `attempt`
12. `state` (extensible stage data)
13. `traceRef`
14. `createdAt`
15. `updatedAt`
16. `terminalReason` (for `FAILED|POISONED|CANCELLED`)

## 5.2 `RiskStageResult` (embedded history)

1. `stage`
2. `startedAt`
3. `finishedAt`
4. `decision`
5. `reasonCodes[]`
6. `attempt`
7. `inputHash`
8. `outputHash`
9. `workerVersion`

## 5.3 Idempotency keys

Base key:

`risk_job_dedup|tenant|project|lens|target|triggerRef|polarity`

Wave dedup guard:

`eventId|signalKey|stage`

Rules:

1. Same event + same signalKey + same stage -> throttle/drop.
2. Different event with same signal shape -> allowed overlay.

## 5.4 State transition table

Allowed transitions:

1. `PENDING -> RUNNING`
2. `RUNNING -> COMPLETED`
3. `RUNNING -> FAILED`
4. `RUNNING -> POISONED`
5. `RUNNING -> CANCELLED`
6. `FAILED -> RUNNING` (retry only)
7. `FAILED -> POISONED` (max attempts reached)

Forbidden:

1. Any terminal -> non-terminal (except explicit operator replay path creating new job id)

## 6. Cloud Tasks Topology

## 6.1 Queue

Dedicated queue:

`CLOUD_TASKS_RISK_QUEUE` (default `h1horizon-risk-v1`)

Initial config:

1. `max-attempts=5`
2. `min-backoff=10s`
3. `max-backoff=600s`
4. `max-retry-duration=3600s`
5. `max-dispatches-per-second=20`
6. `max-concurrent-dispatches=100`

## 6.2 Dispatcher

New dispatcher:

`backend/internal/horizon/tasks/risk_cloudtasks.go`

Payload v1:

```json
{
  "jobId": "..."
}
```

Target endpoint:

`POST /api/v1/internal/tasks/risk/run`

Security:

1. OIDC audience validation.
2. Optional strict service account email validation.
3. Reject non-POST and malformed payload.

## 6.3 Retry matrix

1. `4xx` validation/auth errors -> no retry.
2. `5xx` transient stage/dependency errors -> retry.
3. Max attempts reached -> mark `POISONED`, return `200` (stop retries).

## 7. Internal API Contracts

## 7.1 `POST /api/v1/internal/tasks/risk/run`

Request body:

```json
{
  "jobId": "string"
}
```

Success response:

1. `200` with `{ "status": "ok", "jobId": "...", "state": "COMPLETED|POISONED|FAILED" }`

Client errors:

1. `400` invalid payload
2. `401` token missing/invalid
3. `403` unexpected token identity
4. `404` job not found
5. `409` illegal state transition (if applicable)

Server errors:

1. `500` retryable execution failure

Idempotency:

1. If job is already terminal, handler returns `200` and no-op.
2. Duplicate Cloud Task delivery must not re-apply stage side effects.

## 7.2 Optional operator APIs (v1.1)

1. `POST /api/horizon/risk/jobs` (manual trigger)
2. `GET /api/horizon/risk/jobs/{id}`
3. `POST /api/horizon/risk/jobs/{id}:cancel`
4. `POST /api/horizon/risk/jobs/{id}:replay` (creates new job id)

## 8. WS Event Contract (same channel, dedicated types)

Event types:

1. `horizon.risk.job.created`
2. `horizon.risk.job.state`
3. `horizon.risk.job.log`
4. `horizon.risk.stage.started`
5. `horizon.risk.stage.completed`
6. `horizon.risk.stage.failed`
7. `horizon.risk.job.completed`
8. `horizon.risk.job.failed`

## 8.1 Versioned envelope

All risk WS messages use:

```json
{
  "schemaVersion": "risk.ws.v1",
  "tenantId": "...",
  "projectId": "...",
  "lensId": "...",
  "jobId": "...",
  "correlationId": "...",
  "sessionId": "...",
  "targetCanonicalPath": "...",
  "polarity": "risk|opportunity",
  "ts": "RFC3339Nano",
  "payload": {}
}
```

Compatibility rule:

1. Additive fields allowed in `payload`.
2. Breaking changes require new `schemaVersion`.

## 9. Control Plane / Kill Switches

Required flags:

1. `ENABLE_CLOUD_TASKS_RISK`
2. `ENABLE_RISK_PIPELINE`
3. `ENABLE_RISK_STAGE_IDENTIFY`
4. `ENABLE_RISK_STAGE_ESCALATE`
5. `ENABLE_RISK_STAGE_APPLICABILITY`
6. `ENABLE_RISK_STAGE_SEED`
7. `ENABLE_RISK_STAGE_PROPAGATE`
8. `ENABLE_RISK_STAGE_ROLLUP`
9. `ENABLE_RISK_STAGE_INTERPRET`
10. `ENABLE_RISK_STAGE_ALERT`

Operational commands:

1. Pause dispatch (queue rate -> 0).
2. Drain queue safely.
3. Force drop new job creation while allowing in-flight completion.
4. Tenant-level denylist.

## 10. Logging and Tracing

Canonical prefixes:

1. `[horizon.risk.pipeline]`
2. `[horizon.risk.identify]`
3. `[horizon.risk.escalate]`
4. `[horizon.risk.applicability]`
5. `[horizon.risk.seed]`
6. `[horizon.risk.propagate]`
7. `[horizon.risk.rollup]`
8. `[horizon.risk.interpret]`
9. `[horizon.risk.alert]`
10. `[horizon.risk.queue]`

Structured trace (`trace.Log`) fields:

1. `stage`
2. `event`
3. `decision`
4. `reasonCodes[]`
5. `durMs`
6. `attempt`
7. `inputHash`
8. `outputHash`
9. `workerVersion`

## 11. SLO/SLI and Alerting

Primary SLI:

1. `event_to_alert_latency_p95`
2. `job_success_rate`
3. `stage_failure_rate{stage}`
4. `poison_rate`
5. `dedup_drop_rate`
6. `false_spread_rate`
7. `applicability_precision`

Initial SLO targets:

1. `job_success_rate >= 99%` (rolling 7d)
2. `poison_rate < 0.5%`
3. `event_to_alert_latency_p95 < 5m` (high-priority classes)

Operational alerts:

1. Queue backlog > threshold for 15m.
2. Poison jobs spike.
3. Stage timeout spike.
4. False spread KPI regression.

## 12. Integration with Existing Components

V1 reuses:

1. `SignalExtractor`
2. `EscalationClassifier`
3. `ExternalApplicabilityEvaluator`
4. `SeedInjector`
5. `PropagationEngine`
6. `HorizonService.GetFragmentRollup`
7. `Interpreter` and `ConcentrationAnalyzer` feedback emitters

This preserves single-source semantics and avoids duplicated risk math.

## 13. Failure Model

Transient failures:

1. Retry via `5xx`.

Terminal failures:

1. Mark `POISONED`.
2. Emit terminal WS event.
3. Persist final stage/reason codes.

Partial stage failures:

1. Persist stage checkpoint.
2. Resume from last non-terminal stage on retry.

## 14. Testing Strategy

Unit:

1. Stage determinism.
2. Transition legality.
3. Reason code mapping.
4. Dedup logic.
5. WS schema validation.

Integration:

1. OIDC auth path.
2. Queue dispatch to terminal state.
3. Retry/poison behavior.
4. Feedback-triggered re-evaluate.

Replay:

1. Historical scenarios (Hormuz, sanctions, chokepoints, etc.).
2. Golden snapshot comparison for stage outputs.

## 15. Rollout Plan

Phase A:

1. Queue + env + dispatcher + worker endpoint.
2. `RiskJob` model/repo.
3. WS/log skeleton.

Phase B:

1. Wire deterministic stage executors.
2. Persist stage outputs and reason codes.
3. Enable appetite/tail/alert decisions.

Phase C (shadow):

1. Run parallel with current path.
2. Compare KPI deltas.
3. Cutover after SLO gates pass.

## 16. Config Surface

New:

1. `ENABLE_CLOUD_TASKS_RISK`
2. `CLOUD_TASKS_RISK_QUEUE`
3. `ENABLE_RISK_PIPELINE`
4. `ENABLE_RISK_STAGE_*`
5. `RISK_PIPELINE_STAGE_TIMEOUT_SEC`
6. `RISK_PIPELINE_MAX_STAGE_RETRIES`

Shared:

1. `CLOUD_TASKS_LOCATION`
2. `CLOUD_TASKS_TARGET_BASE_URL`
3. `CLOUD_TASKS_OIDC_SERVICE_ACCOUNT`
4. `CLOUD_TASKS_OIDC_AUDIENCE`

## 17. Runbook (v1)

## 17.1 Queue backlog incident

1. Check queue depth and dispatch rates.
2. Verify worker health and OIDC auth failures.
3. Temporarily raise dispatch/concurrency if workers healthy.
4. If workers unhealthy, pause queue and recover service first.

## 17.2 Poison spike incident

1. Filter poison jobs by stage and reasonCode.
2. Identify dominant dependency/validator error.
3. Enable stage-level kill switch for failing stage.
4. Patch and replay poisoned jobs as new ids.

## 17.3 False-alert spike

1. Inspect `APPLICABILITY` and `ALERT` reason codes.
2. Increase strictness for exposure-path gate where needed.
3. Apply temporary suppression policy for noisy class.
4. Run replay benchmark before unfreezing.

## 17.4 Emergency stop

1. Disable new risk job creation (`ENABLE_RISK_PIPELINE=0`).
2. Keep read-only APIs and WS online.
3. Preserve trace artifacts for postmortem.
4. Re-enable by staged flags (`IDENTIFY -> ESCALATE -> ...`).

## 18. Open Decisions

1. Single queue vs multi-queue by stage.
2. Trend handling as `intent=trend` vs separate job type.
3. Priority lanes for urgent geopolitical shocks.
4. Retention windows for stage history and WS audit payloads.

## 19. Immediate Next Steps

1. Implement Phase A skeleton in code.
2. Add strict stage contract validation in worker.
3. Add WS schema tests and transition-table tests.
4. Start shadow mode on selected event classes.


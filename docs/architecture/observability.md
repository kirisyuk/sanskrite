## Observability

### Logging
- Backend emits structured logs; aggregated during profiling and discovery.
- Example local artifacts: `logs.json`, `logs_full.json`, `logs_all.json`, `logs_api.txt`, `profile_errors.txt`.

### Metrics/Tracing
- Current: no dedicated metrics/tracing pipeline documented. Add exporters in future (OpenTelemetry recommended).

### Error Handling
- Prefer explicit error returns; avoid swallowing errors. Surface errors to clients with consistent responses.



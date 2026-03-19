# Trace

The trace package records mesh-level lineage.

Files:

- [`service.go`](./service.go): span start and finish helpers

## Responsibilities

- record execution spans for worker and reviewer stages
- persist lineage snapshots into Firestore
- emit log entries for operational visibility
- attach cost attribution and terminal status

## Stored Span Data

- `taskId`
- `agentId`
- `capability`
- `kind`
- `status`
- timestamps
- free-form attributes
- optional `CostAttribution`

## Design Intent

This is a mesh trace layer, not a replacement for Cloud Trace. It captures business-level lineage:

- which agent handled which task
- which stage failed
- how much the model execution cost
- why a review was accepted or rejected

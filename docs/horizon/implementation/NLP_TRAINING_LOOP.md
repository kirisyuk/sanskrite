# NLP Training Loop

## First production target

Train only the `event-gate` classifier first.

Why:
- highest ROI
- cheapest runtime
- directly reduces false-positive `affected zones`

## Dataset sources

1. weak labels from current deterministic core
2. hard negatives from repaired/archived affected zones
3. manual gold set

## Dataset builder

Use:

```bash
cd backend
go run ./cmd/tools/horizon_build_nlp_dataset --project=<gcp-project> --tenant=global --per-source=100 --output=../tmp/event_gate_dataset.jsonl
```

## Training

Current baseline trainer:
- `python/event_gate_service/train.py`
- backend: TF-IDF + logistic regression

This is intentionally cheap and fast.

## Evaluation

Use:

```bash
python python/event_gate_service/evaluate.py --input tmp/event_gate_dataset.jsonl --model path/to/model.joblib
```

Key metrics:
- precision on `real_event`
- recall on `real_event`
- false positives on `analysis/advisory/broad_scope`
- downstream reduction in false-positive `affected zones`

## Promote criteria

Do not promote a new model unless:
- `real_event` precision beats current rules baseline
- false-positive advisory/analysis rate drops materially
- fallback remains safe
- shadow divergence review looks sane

## Runtime modes

- `rules`
- `shadow`
- `hybrid`
- `model`

Recommended order:
- `rules -> shadow -> hybrid -> model`

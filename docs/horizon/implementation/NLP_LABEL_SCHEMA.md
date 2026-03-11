# NLP Label Schema

## Event classes

- `real_event`
- `analysis`
- `advisory`
- `roundup`
- `broad_scope`
- `noise`

## Entity types

- `country`
- `city`
- `region`
- `facility`
- `company`
- `government body`
- `port`
- `terminal`
- `pipeline`
- `refinery`
- `fab`
- `chokepoint`
- `route`
- `market`
- `product`
- `program`

## Entity roles

- `actor`
- `target`
- `place`
- `facility`
- `market`
- `route`
- `chokepoint`
- `trigger`
- `program`
- `product`

## Operational rule

Entity extraction is not sufficient on its own.

The pipeline should prefer:
1. extract
2. link to graph
3. geo resolve
4. infer affected zone / signal

If linking or geo confidence is weak:
- prefer `signal-only`
- or `warm-only`
- or abstain

Do not aggressively create zones from unresolved entity text alone.

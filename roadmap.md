## Roadmap (High-level)

### Markets (MARKET)

- **Market↔Market relations (future, not MVP)**:
  - **Goal**: model value-chain / dependency structure between markets (e.g., "EV batteries market depends on lithium chemicals market").
  - **Default in MVP**: use `DEPENDS_ON` if needed (MARKET → MARKET) as a generic relationship.
  - **Possible dedicated edge types (later)**:
    - `UPSTREAM_OF` / `DOWNSTREAM_OF` (explicit value chain semantics)
    - `SUBMARKET_OF` (taxonomy hierarchy / market decomposition)
  - **Reason to defer**: requires clear semantics + direction rules + UI/analytics decisions; MVP focus is market metrics + participants/share.


## Data Model

Primary store: Firestore

### Entities
- See `backend/internal/models/models.go` for canonical Go types.
- Firestore rules: `backend/firestore.rules`
- Firestore indexes: `web/firestore.indexes.json`

### Collection Design
- Collections and subcollections are defined implicitly by repositories under `backend/internal/repo/*` and data access in `backend/internal/gcp/firestore.go`.
- **Horizon:** отдельные репозитории и имена коллекций — `backend/internal/horizon/repo/*.go` и модели в `backend/internal/horizon/models/`. Обзорный документ по домену: [docs/horizon/architecture/data-model.md](../horizon/architecture/data-model.md) (проверять согласованность с кодом).

### Mapping
- Repositories serialize/deserialize Go models to Firestore documents.
- Ensure index definitions cover high-frequency queries noted in repo methods.



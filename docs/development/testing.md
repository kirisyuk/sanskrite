## Backend testing (local)

This project ships with unit tests that do not require deployment. They mock external services and avoid network calls (except a local httptest server for RSS parsing).

Quick start:

1) Optional: point Google SDK to local credentials to avoid any remote calls

- macOS default ADC (no service account needed):

```bash
export GOOGLE_APPLICATION_CREDENTIALS="$HOME/.config/gcloud/application_default_credentials.json"
```

- Or place a service-account JSON locally and point to it:

```bash
export GOOGLE_APPLICATION_CREDENTIALS="$HOME/.secrets/sanskrite-sa.json"
```

No tests currently construct real Firestore/PubSub clients, so credentials are not required; the env setup above just ensures any accidental client construction stays local.

2) Run tests from your machine

```bash
cd backend
go test ./...
```

Notes:
- Discovery pipeline tests cover: chunking, rules matching, RSS provider ingest, crawler URL canonicalization, scheduler cadence, and LLM title generation via a fake AI service.
- Telegram and Firestore-backed integration paths are intentionally not hitting the network in unit tests.
- If you add integration tests that instantiate `firestore.Client` or `pubsub.Client`, prefer running against emulators:

```bash
# Firestore emulator
gcloud beta emulators firestore start --host-port=localhost:8080 &
export FIRESTORE_EMULATOR_HOST=localhost:8080

# Pub/Sub emulator
gcloud beta emulators pubsub start --host-port=localhost:8085 &
export PUBSUB_EMULATOR_HOST=localhost:8085
```

Then run `go test` again.



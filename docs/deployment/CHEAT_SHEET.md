# Deployment Cheat Sheet

Одностраничная справка по deployment для Sanskrite Discovery Platform.

---

## 🚀 Deployment Commands

```bash
# Full deploy (all services)
bash scripts/deploy

# Partial deploy
bash scripts/deploy --api    # Backend only
bash scripts/deploy --web    # Frontend only
bash scripts/deploy --py     # Telegram service only
```

---

## 🔍 Quick Diagnostics

```bash
# Health check
curl $(gcloud run services describe sanskrite-api --region us-central1 --format='value(status.url)')/health

# Logs (last 20 lines)
gcloud run services logs read sanskrite-api --region us-central1 --limit 20

# Current revision
gcloud run services describe sanskrite-api --region us-central1 --format="value(status.traffic[0].revisionName)"

# All revisions
gcloud run revisions list --service sanskrite-api --region us-central1 --limit 5
```

---

## 🐛 Common Issues & Quick Fixes

| Symptom | Quick Fix |
|---------|-----------|
| `Permission denied on secret` | `gcloud projects add-iam-policy-binding $(gcloud config get-value project) --member="serviceAccount:backend@$(gcloud config get-value project).iam.gserviceaccount.com" --role=roles/secretmanager.secretAccessor` |
| `Topic not found` | `gcloud pubsub topics create discovery.status discovery.profile discovery.answer discovery.project` |
| `projectID was empty` | `gcloud run services update sanskrite-api --region us-central1 --set-env-vars "GCP_PROJECT_ID=$(gcloud config get-value project),GOOGLE_CLOUD_PROJECT=$(gcloud config get-value project)"` |
| Old code after deploy | `gcloud run services update-traffic sanskrite-api --region us-central1 --to-latest` |
| CORS error | `gcloud run services update sanskrite-api --region us-central1 --update-env-vars "FRONTEND_ORIGIN=http://localhost:5173"` |

---

## 🔧 Useful One-Liners

```bash
# Get all service URLs
echo "API: $(gcloud run services describe sanskrite-api --region us-central1 --format='value(status.url)')"; echo "TG: $(gcloud run services describe sanskrite-telegram --region us-central1 --format='value(status.url)')"

# Check all secrets exist
for s in vertex_api_key openai_api_key anthropic_api_key grok_api_key; do gcloud secrets describe $s >/dev/null 2>&1 && echo "✓ $s" || echo "✗ $s"; done

# Check all Pub/Sub topics exist
for t in discovery.status discovery.profile discovery.answer discovery.project; do gcloud pubsub topics describe $t >/dev/null 2>&1 && echo "✓ $t" || echo "✗ $t"; done

# Follow logs in real-time
gcloud run services logs tail sanskrite-api --region us-central1

# View env vars
gcloud run services describe sanskrite-api --region us-central1 --format="value(spec.template.spec.containers[0].env)"
```

---

## 🔑 Naming Conventions

| Component | Format | Example |
|-----------|--------|---------|
| GCP Secrets | `lowercase_underscore` | `vertex_api_key` |
| Env Vars | `UPPERCASE_UNDERSCORE` | `VERTEX_API_KEY` |
| Cloud Run | `lowercase-dash` | `sanskrite-api` |
| Pub/Sub Topics | `lowercase.dot` | `discovery.status` |

---

## 🚨 Emergency Rollback

```bash
# 1. List revisions
gcloud run revisions list --service sanskrite-api --region us-central1 --limit 5

# 2. Rollback to previous (e.g., sanskrite-api-00006)
gcloud run services update-traffic sanskrite-api --region us-central1 --to-revisions sanskrite-api-00006=100

# 3. Verify
curl $(gcloud run services describe sanskrite-api --region us-central1 --format='value(status.url)')/health
```

---

## 📚 Full Docs

- **Deployment Checklist**: [docs/deployment/deployment-checklist.md](deployment-checklist.md)
- **Troubleshooting**: [docs/deployment/TROUBLESHOOTING.md](TROUBLESHOOTING.md)
- **Lessons Learned**: [docs/lessons-learned/deployment-pitfalls.md](../lessons-learned/deployment-pitfalls.md)
- **Quick Start**: [DEPLOY_QUICK_START.md](../../DEPLOY_QUICK_START.md)

---

**Last Updated**: 2025-10-11


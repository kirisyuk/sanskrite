# 🚀 Deploy Quick Start

## Первый деплой (новый проект)

```bash
# 1. Setup infrastructure (ОДИН РАЗ)
wsl bash scripts/deploy
# Скрипт автоматически создаст: SA, роли, секреты (placeholders), Pub/Sub топики

# 2. Обновить секреты реальными ключами
echo "YOUR_REAL_KEY" | gcloud secrets versions add vertex_api_key --data-file=-
echo "YOUR_REAL_KEY" | gcloud secrets versions add openai_api_key --data-file=-
echo "YOUR_REAL_KEY" | gcloud secrets versions add anthropic_api_key --data-file=-
echo "YOUR_REAL_KEY" | gcloud secrets versions add grok_api_key --data-file=-

# 3. Проверить что всё работает
curl "$(gcloud run services describe nv-horizon-api --region us-central1 --format='value(status.url)')/healthz"
```

---

## Регулярный деплой

```bash
# Полный деплой (все сервисы)
wsl bash scripts/deploy

# Только backend
wsl bash scripts/deploy --api

# Только frontend
wsl bash scripts/deploy --web

# Только Python Telegram
wsl bash scripts/deploy --py
```

---

## Проверка после деплоя

```bash
# ✓ Трафик на последней ревизии?
gcloud run services describe nv-horizon-api --region us-central1 --format="value(status.traffic[0].revisionName)"

# ✓ Есть ошибки в логах?
gcloud run services logs read nv-horizon-api --region us-central1 --limit 20

# ✓ Health check OK?
curl "$(gcloud run services describe nv-horizon-api --region us-central1 --format='value(status.url)')/healthz"
```

---

## Типовые проблемы

| Симптом | Причина | Решение |
|---------|---------|---------|
| `Permission denied on secret` | Нет роли `secretAccessor` | См. docs/deployment/deployment-checklist.md |
| `Topic not found` | Pub/Sub топики не созданы | `gcloud pubsub topics create discovery.status ...` |
| `projectID was empty` | Env var потерялся | Скрипт теперь передаёт `GCP_PROJECT_ID` + `GOOGLE_CLOUD_PROJECT` |
| Старый код работает | Трафик не переключён | `gcloud run services update-traffic ... --to-latest` |

**Полная документация**: [docs/deployment/deployment-checklist.md](docs/deployment/deployment-checklist.md)

---

## Emergency Rollback

```bash
# Откат на предыдущую ревизию
gcloud run revisions list --service nv-horizon-api --region us-central1 --limit 5
gcloud run services update-traffic nv-horizon-api --region us-central1 --to-revisions REVISION_NAME=100
```


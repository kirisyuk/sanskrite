# Deployment Pitfalls & Lessons Learned

Этот документ собирает типовые ошибки и anti-patterns, на которые мы потратили время в процессе разработки и деплоя Sanskrite Discovery Platform.

**Дата**: 2025-10-11  
**Контекст**: Миграция с Azure (C#) на GCP (Go), первый деплой нового проекта

---

## 🔴 Категория: IAM & Permissions

### ❌ Anti-pattern: Добавляем права по мере необходимости
**Что делали**: Деплоим → получаем Permission Denied → добавляем роль → деплоим снова → новая Permission Denied

**Итог**: 4-5 итераций деплоя (по ~5 минут каждый) = 20-25 минут потрачено впустую

**✅ Best practice**: 
```bash
# Создать чеклист прав ДО первого деплоя и добавить ВСЕ сразу:
for role in roles/datastore.user roles/secretmanager.secretAccessor roles/pubsub.admin roles/logging.logWriter; do
  gcloud projects add-iam-policy-binding $PROJECT \
    --member="serviceAccount:$SA" --role="$role"
done
```

**Почему важно**: Cloud Run deployment занимает 3-5 минут. Каждая итерация - дорого.

---

## 🔴 Категория: Secrets Management

### ❌ Anti-pattern: Несогласованность naming в секретах
**Что делали**: 
- Создали секреты: `GEMINI_API_KEY` (uppercase)
- В Cloud Run binding: `GEMINI_API_KEY=GEMINI_API_KEY:latest`
- Ошибка: `Secret "GEMINI_API_KEY" was not found`

**Итог**: 2-3 итерации пока не поняли что GCP требует lowercase имена

**✅ Best practice**:
```bash
# В GCP Secret Manager - ВСЕГДА lowercase_underscore
gcloud secrets create gemini_api_key

# В Cloud Run binding - mapping к UPPERCASE
--set-secrets "GEMINI_API_KEY=gemini_api_key:latest"
```

**Документация**: Naming convention в [deployment-checklist.md](../deployment/deployment-checklist.md)

---

### ❌ Anti-pattern: Создание секретов через echo без pipe
**Что делали**:
```bash
gcloud secrets versions add vertex_api_key --data-file="$KEY"  # ❌
```

**Ошибка**: `No such file or directory` или `Secret Payload cannot be empty`

**✅ Best practice**:
```bash
# Option 1: Pipe через stdin
echo "$KEY" | gcloud secrets versions add vertex_api_key --data-file=-

# Option 2: Temporary file (если ключ длинный или multiline)
echo "$KEY" > /tmp/key.txt
gcloud secrets versions add vertex_api_key --data-file=/tmp/key.txt
rm /tmp/key.txt
```

---

## 🔴 Категория: Pub/Sub Infrastructure

### ❌ Anti-pattern: Ожидаем что топики создадутся автоматически
**Что делали**: Код пытается `pubsub.CreateTopic()` → падает с PermissionDenied

**Итог**: 
1. Добавляем `pubsub.admin` роль → 5 минут
2. Деплоим снова → код пытается создать, но падает по таймауту → 5 минут
3. Создаём топики вручную → работает

**✅ Best practice**: Топики - это инфраструктура, создавать ДО деплоя:
```bash
# В setup_infrastructure() в deploy script
for topic in discovery.status discovery.profile discovery.answer discovery.project; do
  gcloud pubsub topics create "$topic" --quiet || true  # идемпотентно
done
```

**Lesson**: Разделяем infrastructure provisioning и application deployment

---

## 🔴 Категория: Environment Variables

### ❌ Anti-pattern: Полагаемся что `gcloud run services update` сохраняет старые env vars
**Что делали**:
```bash
# Deploy #1
gcloud run deploy --set-env-vars "GCP_PROJECT_ID=sanskrite,VAR1=val1"

# Deploy #2 (добавляем VAR2)
gcloud run services update --update-env-vars "VAR2=val2"

# Результат: GCP_PROJECT_ID потерян! 😱
```

**Итог**: Backend падает с "projectID was empty", 10 минут на debugging

**✅ Best practice**:
```bash
# ВСЕГДА передавать ВСЕ env vars при update:
gcloud run services update \
  --set-env-vars "GCP_PROJECT_ID=$PROJECT,GOOGLE_CLOUD_PROJECT=$PROJECT,VAR1=val1,VAR2=val2"

# Или использовать deploy (не update) - он сохраняет старые env vars
gcloud run deploy --set-env-vars "GCP_PROJECT_ID=$PROJECT,VAR2=val2"
```

**Root cause**: `--update-env-vars` делает PATCH, не GET-and-merge

---

### ❌ Anti-pattern: Передаём только GCP_PROJECT_ID
**Что делали**: `--set-env-vars "GCP_PROJECT_ID=$PROJECT"`

**Проблема**: Некоторые GCP SDK ищут `GOOGLE_CLOUD_PROJECT`, некоторые `GCP_PROJECT_ID`

**✅ Best practice**: Передавать оба:
```bash
--set-env-vars "GCP_PROJECT_ID=$PROJECT,GOOGLE_CLOUD_PROJECT=$PROJECT"
```

---

## 🔴 Категория: Traffic Management

### ❌ Anti-pattern: Не проверяем на какой ревизии трафик после deploy
**Что делали**: Deploy успешен → тестируем → код не работает → "Почему мои изменения не применились?"

**Итог**: 15 минут debugging, пока не поняли что трафик на старой ревизии

**✅ Best practice**:
```bash
# В deploy script - ВСЕГДА after deploy:
gcloud run services update-traffic $SERVICE --region $REGION --to-latest

# И проверяем:
REVISION=$(gcloud run services describe $SERVICE --format="value(status.traffic[0].revisionName)")
echo "Traffic on: $REVISION"
```

**Почему бывает**: По умолчанию Cloud Run может оставить трафик на старой ревизии при ошибках health check

---

## 🔴 Категория: Development Environment

### ❌ Anti-pattern: PowerShell синтаксис вместо bash
**Что делали**: `cd backend && go build` в PowerShell → `The term '&&' is not recognized`

**Итог**: 5 минут на поиск "почему команда не работает"

**✅ Best practice**: На Windows **всегда** использовать WSL или Git Bash для bash скриптов:
```bash
wsl bash scripts/deploy
# или
"C:\Program Files\Git\bin\bash.exe" scripts/deploy
```

**Alternative**: В PowerShell использовать `;` вместо `&&`:
```powershell
cd backend; go build ./cmd/server
```

---

### ❌ Anti-pattern: Не проверяем компиляцию локально перед deploy
**Что делали**: Пишем код → деплоим → Cloud Build падает через 5 минут → "syntax error"

**Итог**: 5 минут на build, 2 минуты на фикс, снова 5 минут на build

**✅ Best practice**:
```bash
# В deploy script - ПЕРЕД gcloud run deploy:
(cd "$API_DIR" && go build -o /dev/null ./cmd/server) || {
  echo "Go build failed! Fix errors before deploying."
  exit 1
}
```

**Benefit**: Ловим ошибки за 10 секунд локально, не за 5 минут в Cloud Build

---

## 🔴 Категория: Frontend Development

### ❌ Anti-pattern: Не перезагружаем dev server при HMR issues
**Что делали**: Видим ошибку в Vite HMR → фиксим → ошибка остаётся → думаем что код неправильный

**Итог**: 10 минут debugging "ghost errors"

**✅ Best practice**: При странных HMR ошибках - **сразу restart**:
```bash
# Kill
taskkill /F /IM node.exe

# Restart
npm run dev
```

**Why**: Vite HMR cache может "залипнуть" на старой ошибке

---

### ❌ Anti-pattern: `await` в non-async callback
**Что делали**:
```typescript
ws.onmessage = (evt) => {
  const data = await fetch(...);  // ❌ ERROR: await only in async
}
```

**✅ Best practice**:
```typescript
// Option 1: Make callback async
ws.onmessage = async (evt) => {
  const data = await fetch(...);
}

// Option 2: Use .then()
ws.onmessage = (evt) => {
  fetch(...).then(data => { ... });
}

// Option 3: Dynamic import without await
ws.onmessage = (evt) => {
  import('./store').then(({ useStore }) => { ... });
}
```

---

## 🔴 Категория: Code Migration

### ❌ Anti-pattern: Делаем assumptions о логике вместо чтения кода
**Что делали**: "Наверное, источники создаются через POST endpoint" → пишем код → не работает

**Реальность**: В старом backend источники создавались on-demand в других эндпоинтах

**Итог**: 30 минут на переделку логики

**✅ Best practice**:
```
1. Прочитать ВЕСЬ релевантный код старого backend (не grep)
2. Понять flow целиком
3. Написать новый код
```

**Tool**: `grep -r "DataSource" --include="*.cs"` для поиска всех упоминаний

---

## 🔴 Категория: Firestore Indexes

### ❌ Anti-pattern: Неправильный синтаксис --field-config
**Что делали**: `--field-config fieldPath=tenantId,order=ASCENDING`

**Ошибка**: `valid keys are [field-path, ...]; received: fieldPath`

**✅ Best practice**:
```bash
gcloud firestore indexes composite create \
  --field-config field-path=tenantId,order=ASCENDING \
  --field-config field-path=view,order=ASCENDING
```

**Note**: kebab-case, not camelCase!

---

## 📊 Статистика потерянного времени

| Проблема | Итераций | Время на итерацию | Всего потрачено |
|----------|----------|-------------------|-----------------|
| IAM права по мере необходимости | 4 | 5 мин | 20 мин |
| Secret naming (uppercase/lowercase) | 3 | 5 мин | 15 мин |
| Pub/Sub топики не созданы | 2 | 5 мин | 10 мин |
| GCP_PROJECT_ID потерян | 2 | 10 мин | 20 мин |
| Трафик на старой ревизии | 1 | 15 мин | 15 мин |
| PowerShell vs bash | 1 | 5 мин | 5 мин |
| HMR cache issues | 2 | 10 мин | 20 мин |
| Code migration assumptions | 1 | 30 мин | 30 мин |

**ИТОГО**: ~135 минут = 2.25 часа потрачено на типовые ловушки

**С чеклистом**: ~15 минут (только первый setup)

**Экономия**: 2+ часа на КАЖДЫЙ новый проект! 🎯

---

## 🎓 Key Takeaways

### 1. Infrastructure First, Code Second
- Service accounts + IAM roles
- Secrets (с placeholders OK)
- Pub/Sub topics
- Firestore indexes

### 2. Naming Conventions - согласовать заранее
- GCP Secrets: `lowercase_underscore`
- Env Vars: `UPPERCASE_UNDERSCORE`
- Cloud Run: `lowercase-dash`
- Pub/Sub: `lowercase.dot`

### 3. Always Verify After Deploy
- ✅ Traffic on latest revision?
- ✅ Logs have no errors?
- ✅ Health check passes?
- ✅ Env vars preserved?

### 4. Local Validation Before Cloud Build
- Go: `go build`
- TS: `npm run type-check`
- Saves 5+ minutes per failed deploy

### 5. Read First, Code Second
- Не делать assumptions о старом коде
- Читать исходники, не только grep
- Понимать flow целиком

---

## 📚 Related Documentation

- [Deployment Checklist](../deployment/deployment-checklist.md) - Полный чеклист для деплоя
- [DEPLOY_QUICK_START.md](../../DEPLOY_QUICK_START.md) - Быстрая памятка
- [scripts/deploy](../../scripts/deploy) - Automated deployment script

---

**Вывод**: С правильным чеклистом и automation, полный first-time deploy занимает ~15 минут вместо 2+ часов debugging типовых проблем.


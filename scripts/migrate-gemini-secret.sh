#!/usr/bin/env bash
set -euo pipefail

# Миграция секрета VERTEX_API_KEY -> GEMINI_API_KEY
# Использование: ./scripts/migrate-gemini-secret.sh [-p|--project nv-horizon]

PROJECT="nv-horizon"
while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--project) PROJECT="$2"; shift 2;;
    *) echo "Unknown arg: $1" >&2; exit 2;;
  esac
done

echo "Migrating secrets for project: $PROJECT"
gcloud config set project "$PROJECT" >/dev/null

# Функция для проверки существования секрета
secret_exists() {
  local name="$1"
  gcloud secrets describe "$name" --project="$PROJECT" >/dev/null 2>&1
}

# Функция для получения значения секрета
get_secret_value() {
  local name="$1"
  gcloud secrets versions access latest --secret="$name" --project="$PROJECT" 2>/dev/null || echo ""
}

# Функция для создания секрета
create_secret() {
  local name="$1"
  local value="$2"
  if secret_exists "$name"; then
    echo "  ⚠️  Secret $name already exists, adding new version..."
    echo -n "$value" | gcloud secrets versions add "$name" --data-file=- --project="$PROJECT" >/dev/null
    echo "  ✅ Updated secret: $name"
  else
    echo -n "$value" | gcloud secrets create "$name" --data-file=- --project="$PROJECT" >/dev/null
    echo "  ✅ Created secret: $name"
  fi
}

echo ""
echo "1. Migrating gemini_api_key (production)..."
if secret_exists "vertex_api_key"; then
  echo "  📋 Found existing vertex_api_key, copying value..."
  VALUE=$(get_secret_value "vertex_api_key")
  if [ -z "$VALUE" ]; then
    echo "  ❌ Failed to retrieve vertex_api_key value"
    exit 1
  fi
  create_secret "gemini_api_key" "$VALUE"
else
  echo "  ⚠️  vertex_api_key not found"
  echo "  💡 You can create gemini_api_key manually:"
  echo "     echo 'YOUR_API_KEY' | gcloud secrets create gemini_api_key --data-file=- --project=$PROJECT"
fi

echo ""
echo "2. Migrating gemini_api_key_test (test)..."
if secret_exists "vertex_api_key_test"; then
  echo "  📋 Found existing vertex_api_key_test, copying value..."
  TEST_VALUE=$(get_secret_value "vertex_api_key_test")
  if [ -z "$TEST_VALUE" ]; then
    echo "  ❌ Failed to retrieve vertex_api_key_test value"
    exit 1
  fi
  create_secret "gemini_api_key_test" "$TEST_VALUE"
else
  echo "  ⚠️  vertex_api_key_test not found (optional, skipping)"
fi

echo ""
echo "3. Verifying secrets..."
if secret_exists "gemini_api_key"; then
  echo "  ✅ gemini_api_key exists"
  # Проверяем что значение не пустое (первые 10 символов)
  SAMPLE=$(get_secret_value "gemini_api_key" | head -c 10)
  if [ -n "$SAMPLE" ]; then
    echo "  ✅ gemini_api_key has value (sample: ${SAMPLE}...)"
  else
    echo "  ⚠️  gemini_api_key exists but appears to be empty"
  fi
else
  echo "  ❌ gemini_api_key not found"
fi

if secret_exists "gemini_api_key_test"; then
  echo "  ✅ gemini_api_key_test exists"
else
  echo "  ⚠️  gemini_api_key_test not found (optional)"
fi

echo ""
echo "✅ Migration complete!"
echo ""
echo "Next steps:"
echo "1. Update Cloud Run service to use GEMINI_API_KEY=gemini_api_key:latest"
echo "2. Test the application with new secret"
echo "3. (Optional) Delete old vertex_api_key secrets after verification"


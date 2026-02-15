#!/bin/bash
# Скрипт для создания секретов в GCP Secret Manager
# Использование: ./scripts/create-secrets.sh [PROJECT_ID]
#
# ВАЖНО: Этот скрипт содержит реальные значения секретов!

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Get project ID from argument or gcloud config
PROJECT_ID="${1:-$(gcloud config get-value project 2>/dev/null)}"

if [ -z "$PROJECT_ID" ]; then
  echo -e "${RED}Error: PROJECT_ID is required${NC}"
  echo "Usage: $0 [PROJECT_ID]"
  echo "Or set default project: gcloud config set project PROJECT_ID"
  exit 1
fi

echo -e "${BLUE}Creating secrets for project:${NC} $PROJECT_ID"
echo ""

# Function to create or update secret
create_secret() {
  local secret_name=$1
  local secret_value=$2
  local description="${3:-}"
  local label_arg=()
  
  if [ -n "$description" ]; then
    # Labels must be lowercase and contain only [a-z0-9_-]
    local label_value
    label_value=$(echo "$description" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]+/-/g; s/^-+//; s/-+$//')
    if [ -n "$label_value" ]; then
      label_arg=(--labels="description=$label_value")
    fi
  fi
  
  if [ -z "$secret_value" ] || [ "$secret_value" = "null" ]; then
    echo -e "${YELLOW}⚠ Skipping ${secret_name} - value is empty${NC}"
    return
  fi
  
  if gcloud secrets describe "$secret_name" --project="$PROJECT_ID" >/dev/null 2>&1; then
    echo -e "${YELLOW}Secret ${secret_name} already exists, adding new version...${NC}"
    echo -n "$secret_value" | gcloud secrets versions add "$secret_name" \
      --project="$PROJECT_ID" \
      --data-file=- \
      --quiet || true
    echo -e "${GREEN}  ✓${NC} Updated ${secret_name}"
  else
    echo -n "$secret_value" | gcloud secrets create "$secret_name" \
      --project="$PROJECT_ID" \
      --data-file=- \
      --replication-policy="automatic" \
      "${label_arg[@]}" \
      --quiet || true
    echo -e "${GREEN}  ✓${NC} Created ${secret_name}"
  fi
}

# API Keys (UPPERCASE versions)
create_secret "ANTHROPIC_API_KEY" "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" "Anthropic API key"
create_secret "ANTHROPIC_API_KEY_TEST" "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" "Anthropic API key for testing"
create_secret "DEEPSEEK_API_KEY" "sk-1f5a3c35e08242318626291d6f159407" "DeepSeek API key"
create_secret "DEEPSEEK_API_KEY_TEST" "sk-2d113e3584894dcc8ee697aa8879449f" "DeepSeek API key for testing"
create_secret "GROK_API_KEY" "xai-hWLAtWFqCn2j8YSynauB2i7P3NLxkwZmeyLKNltXVjzmprmXYIZsAW46fsO45e3300Lszbx7LO1Eh4W3" "Grok API key"
create_secret "GROK_API_KEY_TEST" "xai-cHuyaGAjicYfxVWsm3kzpPcs0EA798HHFyoqb608yjVFUNGcf7dy1IVmwiEfR08hlfwdetsdl8P5UVuq" "Grok API key for testing"
create_secret "OPENAI_API_KEY_TEST" "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" "OpenAI API key for testing"
create_secret "VERTEX_API_KEY" "AIzaSyBRzAlY4OnhdsUUqMkA_ri26RyYIs_rRJo" "Vertex AI API key"
create_secret "VERTEX_API_KEY_TEST" "AIzaSyCUYOjd9G0Wr82i1uPQ-WO2IFzkb4EkA1U" "Vertex AI API key for testing"

# API Keys (lowercase versions)
create_secret "anthropic_api_key" "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" "Anthropic API key (lowercase)"
create_secret "deepseek_api_key" "sk-1f5a3c35e08242318626291d6f159407" "DeepSeek API key (lowercase)"
create_secret "grok_api_key" "xai-m5h9agnP6VoZ5OHyNfieWVzTt7OoBg74VpelcYt7EILkwxMSVfGrKFnS9piGqQeIo4iXLfgP4OgSgisj" "Grok API key (lowercase)"
create_secret "openai_api_key" "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" "OpenAI API key (lowercase)"
create_secret "vertex_api_key" "AIzaSyD5fKVHm_AGY6bms4XermWRRxX6d6TDsAc" "Vertex AI API key (lowercase)"
create_secret "gemini_api_key" "AIzaSyD5fKVHm_AGY6bms4XermWRRxX6d6TDsAc" "Gemini API key"
create_secret "tavily_api_key" "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" "Tavily API key"

# Telegram secrets
create_secret "TELEGRAM_API_ID" "26909019" "Telegram API ID"
create_secret "TELEGRAM_API_HASH" "2ffc3249536bbac0c5d4c35c32453d59" "Telegram API Hash"
create_secret "TELEGRAM_BOT_TOKEN" "7810167227:AAFb8iOOZSFf_ZkRPH0OXCBMP9SnzDRrI0M" "Telegram Bot Token"
create_secret "TELEGRAM_SESSION_STRING" "1ApWapzMBuzgf9rQq4AEuPWHUDUpPWOQiKcVLaWxejytt4lYRB36VHWPsgnieKHmiNzZBiyDqZQ3U18PPKr8VcxUixn6daAhoHWBxZjEJ3hfFq9YUhmj7yzh5yIs7DmeaoUMxbqTsi3PZ9ic3FRAfU96hHncFBREt4_Hz_1ob10n3NuhX5OOQOGGQEC7NwDZupEA3UG1Yu1un0BvrlD9Se2uc2EyGuj8n5N7OqcjBIVut0kgq6E3GSuqeUU_0LiQWbvi_kGDbxpma_85kBWPbtz2jaonrAEsuRLXLJB2oPSXZMyT7bGhbNf_HgziHwqqDe668cufdSBbclpdiDfcz6SdsBiroSiQ=" "Telegram Session String"

# PayPal secrets
create_secret "paypal_client_id" "AceUI-g9OcczbMMDaW_dpetb_49VV6WZ7XtweteU4pg1-jqvd4s1TPbqfm6_hKsr0me8zltwVRUdxs7o" "PayPal Client ID"
create_secret "paypal_secret" "EH3WRFyGfike82icgSvv21nG_lIID-WWmvAtqADD5EiH83Pjfi3lFcBrY_lfQ3c6IZVpm6Rm-fnStQ_W" "PayPal Secret"
create_secret "paypal_webhook_id" "8UY853924Y084640C" "PayPal Webhook ID"

# Google services
create_secret "google-custom-search-api-key" "AIzaSyDCI4Bem_dS_wVhUI1XhhoOdfC892-sZMY" "Google Custom Search API Key"
# Note: google-custom-search-cx secret doesn't exist in GCP, skipping

# Other services
create_secret "GEONAMES_USERNAME" "kirisyuk" "Geonames Username"
create_secret "admin_job_token" "0221989ca2185bde0290361f" "Admin Job Token for Cloud Scheduler"
create_secret "functions-key" "PLACEHOLDER_UPDATE_ME" "Functions Key"

# External Data Providers
create_secret "ACLED_ACCESS_TOKEN" "PLACEHOLDER_UPDATE_ME" "ACLED API Access Token"
create_secret "NASA_FIRMS_API_KEY" "PLACEHOLDER_UPDATE_ME" "NASA FIRMS API Key"
create_secret "FRED_API_KEY" "PLACEHOLDER_UPDATE_ME" "FRED API Key"
create_secret "EIA_API_KEY" "PLACEHOLDER_UPDATE_ME" "EIA API Key"
create_secret "FINNHUB_API_KEY" "PLACEHOLDER_UPDATE_ME" "Finnhub API Key"
create_secret "CLOUDFLARE_API_TOKEN" "PLACEHOLDER_UPDATE_ME" "Cloudflare API Token"
create_secret "OPENSKY_USERNAME" "PLACEHOLDER_UPDATE_ME" "OpenSky Network Username"
create_secret "OPENSKY_PASSWORD" "PLACEHOLDER_UPDATE_ME" "OpenSky Network Password"

echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ Secrets creation completed${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${YELLOW}Note:${NC} This script is idempotent - it will update existing secrets with new versions"
echo -e "${YELLOW}Note:${NC} functions-key contains placeholder - update it manually if needed"

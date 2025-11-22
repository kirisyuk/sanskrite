#!/usr/bin/env bash
set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROJECT_ID=$(gcloud config get-value project 2>/dev/null || true)
if [[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]]; then
  echo -e "${RED}GCP project is not set. Run: gcloud config set project <ID>${NC}" >&2
  exit 1
fi

echo -e "${BLUE}Project:${NC} $PROJECT_ID"
echo -e "${BLUE}Checking available Gemini models...${NC}\n"

# Get API key from Secret Manager
echo -e "${BLUE}Fetching GEMINI_API_KEY from Secret Manager...${NC}"
API_KEY=$(gcloud secrets versions access latest --secret="gemini_api_key" --project="$PROJECT_ID" 2>/dev/null || echo "")

if [[ -z "$API_KEY" ]]; then
  echo -e "${YELLOW}Warning: Could not fetch API key from Secret Manager. Trying environment variable...${NC}"
  API_KEY="${GEMINI_API_KEY:-}"
fi

if [[ -z "$API_KEY" ]]; then
  echo -e "${RED}Error: GEMINI_API_KEY not found in Secret Manager or environment${NC}" >&2
  echo -e "${YELLOW}Please set GEMINI_API_KEY environment variable or ensure secret exists${NC}" >&2
  exit 1
fi

# Check available models using v1beta API
echo -e "${BLUE}Fetching models from Google Generative AI API (v1beta)...${NC}\n"

RESPONSE=$(curl -s -w "\n%{http_code}" \
  -H "x-goog-api-key: ${API_KEY}" \
  "https://generativelanguage.googleapis.com/v1beta/models")

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [[ "$HTTP_CODE" != "200" ]]; then
  echo -e "${RED}Error: API returned HTTP $HTTP_CODE${NC}" >&2
  echo -e "${YELLOW}Response:${NC}"
  echo "$BODY" | jq '.' 2>/dev/null || echo "$BODY"
  exit 1
fi

# Parse and display models
echo -e "${GREEN}Available Gemini models:${NC}\n"

# Extract model names and filter for Gemini models
MODELS=$(echo "$BODY" | jq -r '.models[] | select(.name | startswith("models/gemini")) | .name' 2>/dev/null || echo "")

if [[ -z "$MODELS" ]]; then
  echo -e "${YELLOW}No Gemini models found in response. Full response:${NC}"
  echo "$BODY" | jq '.' 2>/dev/null || echo "$BODY"
  exit 1
fi

# Display models in a formatted way
echo "$MODELS" | while IFS= read -r model; do
  # Extract model name without "models/" prefix
  model_name=$(echo "$model" | sed 's|models/||')
  
  # Get detailed info about the model
  MODEL_INFO=$(curl -s \
    -H "x-goog-api-key: ${API_KEY}" \
    "https://generativelanguage.googleapis.com/v1beta/${model}" 2>/dev/null || echo "")
  
  if [[ -n "$MODEL_INFO" ]]; then
    # Extract supported methods
    SUPPORTED_METHODS=$(echo "$MODEL_INFO" | jq -r '.supportedGenerationMethods[]?' 2>/dev/null | tr '\n' ', ' | sed 's/,$//' || echo "unknown")
    
    # Extract display name
    DISPLAY_NAME=$(echo "$MODEL_INFO" | jq -r '.displayName // empty' 2>/dev/null || echo "")
    
    # Extract description
    DESCRIPTION=$(echo "$MODEL_INFO" | jq -r '.description // empty' 2>/dev/null || echo "")
    
    echo -e "${GREEN}✓${NC} ${BLUE}$model_name${NC}"
    if [[ -n "$DISPLAY_NAME" ]]; then
      echo -e "  Display Name: $DISPLAY_NAME"
    fi
    if [[ -n "$DESCRIPTION" ]]; then
      echo -e "  Description: $DESCRIPTION"
    fi
    echo -e "  Supported Methods: $SUPPORTED_METHODS"
    echo ""
  else
    echo -e "${GREEN}✓${NC} ${BLUE}$model_name${NC}"
    echo ""
  fi
done

# Check specifically for gemini-3.0-pro
echo -e "${BLUE}Checking specifically for gemini-3.0-pro...${NC}"
GEMINI_3_PRO=$(echo "$MODELS" | grep -i "gemini-3.0-pro" || echo "")

if [[ -n "$GEMINI_3_PRO" ]]; then
  echo -e "${GREEN}✓ Found:${NC} $GEMINI_3_PRO"
  
  # Check if it supports generateContent
  MODEL_NAME=$(echo "$GEMINI_3_PRO" | sed 's|models/||')
  MODEL_INFO=$(curl -s \
    -H "x-goog-api-key: ${API_KEY}" \
    "https://generativelanguage.googleapis.com/v1beta/models/${MODEL_NAME}" 2>/dev/null || echo "")
  
  if [[ -n "$MODEL_INFO" ]]; then
    SUPPORTS_GENERATE=$(echo "$MODEL_INFO" | jq -r '.supportedGenerationMethods[]?' 2>/dev/null | grep -q "generateContent" && echo "yes" || echo "no")
    if [[ "$SUPPORTS_GENERATE" == "yes" ]]; then
      echo -e "${GREEN}  ✓ Supports generateContent${NC}"
    else
      echo -e "${RED}  ✗ Does NOT support generateContent${NC}"
      echo -e "${YELLOW}  Supported methods:${NC}"
      echo "$MODEL_INFO" | jq -r '.supportedGenerationMethods[]?' 2>/dev/null || echo "unknown"
    fi
  fi
else
  echo -e "${RED}✗ gemini-3.0-pro NOT found${NC}"
  echo -e "${YELLOW}Available Gemini 3.x models:${NC}"
  echo "$MODELS" | grep -i "gemini-3" || echo "  (none)"
fi

echo ""
echo -e "${BLUE}Summary:${NC}"
echo "  Total Gemini models found: $(echo "$MODELS" | wc -l | tr -d ' ')"
echo "  API Version: v1beta"
echo "  Endpoint: https://generativelanguage.googleapis.com/v1beta/models"


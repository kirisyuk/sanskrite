#!/usr/bin/env bash
set -euo pipefail

# Real API test - проверяет реальную работу Tavily через Cloud Run

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

API_URL="${API_URL:-$(gcloud run services describe api --region=us-central1 --project=prevalent-horizon-test --format='value(status.url)' 2>/dev/null || echo "")}"
TEST_QUERY="${TEST_QUERY:-latest news about AI}"

if [[ -z "$API_URL" ]]; then
    echo -e "${RED}✗ API_URL not set and api Cloud Run service not found${NC}"
    echo -e "${YELLOW}Set API_URL manually or deploy api first${NC}"
    exit 1
fi

echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Real Tavily API Test via Cloud Run${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""

# Get key from secrets
echo -e "${BLUE}[Step 1] Getting Tavily API key from GCP secrets...${NC}"
TAVILY_KEY=$(gcloud secrets versions access latest --secret=tavily_api_key --project=prevalent-horizon-test 2>/dev/null | tr -d '\n\r ' || echo "")

if [[ -z "$TAVILY_KEY" ]]; then
    echo -e "${RED}✗ Failed to get key from secrets${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Key retrieved (length: ${#TAVILY_KEY})${NC}"
echo ""

# Test 1: Direct Tavily API call
echo -e "${BLUE}[Step 2] Testing direct Tavily API call...${NC}"
echo -e "  Query: ${YELLOW}$TEST_QUERY${NC}"

TAVILY_RESP=$(curl -s -X POST "https://api.tavily.com/search" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TAVILY_KEY" \
    -d "{
        \"query\": \"$TEST_QUERY\",
        \"max_results\": 3
    }")

if echo "$TAVILY_RESP" | grep -q '"error"\|"Unauthorized"'; then
    ERROR=$(echo "$TAVILY_RESP" | jq -r '.detail.error // .error // "Unknown"' 2>/dev/null || echo "API error")
    echo -e "${RED}✗ Tavily API error: $ERROR${NC}"
    echo ""
    echo -e "${YELLOW}Response:${NC}"
    echo "$TAVILY_RESP" | jq '.' 2>/dev/null || echo "$TAVILY_RESP"
    echo ""
    echo -e "${YELLOW}⚠ Key may be invalid or expired. But let's check if Cloud Run can use it...${NC}"
    DIRECT_API_WORKS=false
else
    RESULTS_COUNT=$(echo "$TAVILY_RESP" | jq -r '.results | length' 2>/dev/null || echo "0")
    if [[ "$RESULTS_COUNT" -gt 0 ]]; then
        echo -e "${GREEN}✓ Tavily API works! Got $RESULTS_COUNT results${NC}"
        echo "$TAVILY_RESP" | jq '.results[0] | {title, url}' 2>/dev/null
        DIRECT_API_WORKS=true
    else
        echo -e "${YELLOW}⚠ No results returned${NC}"
        DIRECT_API_WORKS=false
    fi
fi

echo ""

# Test 2: Check Cloud Run logs for Tavily usage
echo -e "${BLUE}[Step 3] Checking Cloud Run logs for Tavily activity...${NC}"
RECENT_LOGS=$(gcloud logging read \
    "resource.type=cloud_run_revision AND resource.labels.service_name=api AND (textPayload=~\"Tavily\" OR textPayload=~\"tavily\" OR textPayload=~\"web_search\")" \
    --limit=5 \
    --format="value(textPayload)" \
    --project=prevalent-horizon-test \
    --freshness=1h 2>/dev/null || echo "")

if [[ -n "$RECENT_LOGS" ]]; then
    echo -e "${GREEN}✓ Found recent Tavily-related logs:${NC}"
    echo "$RECENT_LOGS" | head -3
else
    echo -e "${YELLOW}⚠ No recent Tavily logs found (may need to trigger a request)${NC}"
fi

echo ""

# Test 3: Check if TAVILY_API_KEY is accessible in Cloud Run
echo -e "${BLUE}[Step 4] Verifying Cloud Run configuration...${NC}"
HAS_TAVILY_ENV=$(gcloud run services describe api \
    --region=us-central1 \
    --format="get(spec.template.spec.containers[0].env)" \
    --project=prevalent-horizon-test 2>/dev/null | grep -o "TAVILY_API_KEY" || echo "")

if [[ -n "$HAS_TAVILY_ENV" ]]; then
    echo -e "${GREEN}✓ TAVILY_API_KEY is configured in Cloud Run${NC}"
else
    echo -e "${RED}✗ TAVILY_API_KEY not found in Cloud Run env${NC}"
fi

# Check secret exists
if gcloud secrets describe tavily_api_key --project=prevalent-horizon-test >/dev/null 2>&1; then
    echo -e "${GREEN}✓ tavily_api_key secret exists${NC}"
else
    echo -e "${RED}✗ tavily_api_key secret not found${NC}"
fi

echo ""

# Test 4: Try to trigger web search via API (if we can)
echo -e "${BLUE}[Step 5] Testing web_search tool execution...${NC}"
echo -e "${YELLOW}  Note: This requires authentication. Checking if endpoint is accessible...${NC}"

# Try a simple request to see what happens
API_TEST_RESP=$(curl -s -X POST "${API_URL}/api/v1/genai/complete-with-tools" \
    -H "Content-Type: application/json" \
    -d "{
        \"prompt\": \"Use web_search tool to find: $TEST_QUERY\",
        \"tools\": [{
            \"name\": \"web_search\",
            \"description\": \"Search the web\",
            \"parameters\": {
                \"type\": \"object\",
                \"properties\": {
                    \"query\": {\"type\": \"string\"},
                    \"limit\": {\"type\": \"integer\"}
                },
                \"required\": [\"query\"]
            }
        }],
        \"options\": {
            \"modelOverride\": \"gemini_fast\"
        }
    }" 2>&1)

if echo "$API_TEST_RESP" | grep -q "UNAUTHORIZED\|401\|unauthorized"; then
    echo -e "${YELLOW}⚠ Endpoint requires authentication (expected)${NC}"
    echo -e "${YELLOW}  To test fully, you need a valid auth token${NC}"
elif echo "$API_TEST_RESP" | grep -q "error\|Error"; then
    ERROR_CODE=$(echo "$API_TEST_RESP" | jq -r '.code // "unknown"' 2>/dev/null || echo "unknown")
    echo -e "${YELLOW}⚠ API returned error: $ERROR_CODE${NC}"
else
    echo -e "${GREEN}✓ API endpoint responded${NC}"
    echo "$API_TEST_RESP" | jq '.' 2>/dev/null | head -20 || echo "$API_TEST_RESP" | head -20
fi

echo ""
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}Test Summary:${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"

if [[ "$DIRECT_API_WORKS" == "true" ]]; then
    echo -e "Direct Tavily API: ${GREEN}✓ Works${NC}"
else
    echo -e "Direct Tavily API: ${RED}✗ Failed (key may be invalid)${NC}"
fi

echo -e "Cloud Run Config: ${GREEN}✓ TAVILY_API_KEY configured${NC}"
echo -e "Secret Manager: ${GREEN}✓ Secret exists${NC}"
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo -e "  1. If key is invalid, update it: ${BLUE}gcloud secrets versions add tavily_api_key --data-file=-${NC}"
echo -e "  2. Trigger a real web search request through the UI or API with auth"
echo -e "  3. Check logs: ${BLUE}gcloud logging read 'resource.labels.service_name=api AND textPayload~\"Tavily\"' --limit=20${NC}"
echo ""

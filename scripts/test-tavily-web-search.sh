#!/usr/bin/env bash
set -euo pipefail

# Test script for Tavily Web Search integration
# Tests: 1) Direct Tavily API, 2) Web search tool via GenAI API

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
API_URL="${API_URL:-$(gcloud run services describe etigram-api --region=us-central1 --project=etigram --format='value(status.url)' 2>/dev/null || echo "")}"
TEST_QUERY="${TEST_QUERY:-latest news about AI}"

if [[ -z "$API_URL" ]]; then
    echo -e "${RED}Error: API_URL not set and etigram-api Cloud Run service not found${NC}"
    echo -e "${YELLOW}Set API_URL environment variable or deploy etigram-api first${NC}"
    exit 1
fi

# Get Tavily API key
if [[ -z "${TAVILY_API_KEY:-}" ]]; then
    if command -v gcloud >/dev/null 2>&1; then
        TAVILY_API_KEY=$(gcloud secrets versions access latest --secret=tavily_api_key 2>/dev/null | tr -d '\n\r ' || echo '')
    fi
    # Fallback to test key if available
    if [[ -z "$TAVILY_API_KEY" ]]; then
        TAVILY_API_KEY="${TAVILY_API_KEY_TEST:-}"
    fi
fi

# Verify key is set
if [[ -z "$TAVILY_API_KEY" ]]; then
    echo -e "${RED}Error: TAVILY_API_KEY not found${NC}"
    echo -e "${YELLOW}Set TAVILY_API_KEY environment variable or ensure tavily_api_key secret exists${NC}"
    exit 1
fi

echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Tavily Web Search Integration Test${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""

# Check dependencies
if ! command -v curl >/dev/null 2>&1; then
    echo -e "${RED}Error: curl is required${NC}"
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo -e "${YELLOW}Warning: jq not found, JSON output will be raw${NC}"
    JQ_CMD="cat"
else
    JQ_CMD="jq"
fi

# Test 1: Direct Tavily API test
echo -e "${BLUE}[Test 1] Testing direct Tavily API connection...${NC}"
if [[ -z "$TAVILY_API_KEY" ]]; then
    echo -e "${RED}✗ TAVILY_API_KEY not found in environment or GCP secrets${NC}"
    echo -e "${YELLOW}  Set TAVILY_API_KEY environment variable or ensure secret exists${NC}"
    exit 1
fi

TAVILY_RESPONSE=$(curl -s -X POST "https://api.tavily.com/search" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TAVILY_API_KEY" \
    -d "{
        \"query\": \"$TEST_QUERY\",
        \"max_results\": 3,
        \"search_depth\": \"basic\"
    }")

if echo "$TAVILY_RESPONSE" | grep -q "error\|Error\|Unauthorized"; then
    ERROR_MSG=$(echo "$TAVILY_RESPONSE" | $JQ_CMD -r '.detail.error // .error // "Unknown error"' 2>/dev/null || echo "API error")
    echo -e "${RED}✗ Tavily API error: $ERROR_MSG${NC}"
    echo -e "${YELLOW}  Note: This may be due to:${NC}"
    echo -e "${YELLOW}    - Invalid or expired API key${NC}"
    echo -e "${YELLOW}    - API key not activated${NC}"
    echo -e "${YELLOW}    - Rate limit exceeded${NC}"
    echo ""
    echo -e "${YELLOW}  Response:${NC}"
    echo "$TAVILY_RESPONSE" | $JQ_CMD 2>/dev/null || echo "$TAVILY_RESPONSE"
    echo ""
    echo -e "${YELLOW}  Continuing with other tests...${NC}"
    TAVILY_DIRECT_TEST_FAILED=true
else
    TAVILY_DIRECT_TEST_FAILED=false
fi

if [[ "${TAVILY_DIRECT_TEST_FAILED:-false}" != "true" ]]; then
    TAVILY_RESULTS_COUNT=$(echo "$TAVILY_RESPONSE" | $JQ_CMD -r '.results | length' 2>/dev/null || echo "0")
    if [[ "$TAVILY_RESULTS_COUNT" -gt 0 ]]; then
        echo -e "${GREEN}✓ Tavily API connection successful${NC}"
        echo -e "  Found ${GREEN}$TAVILY_RESULTS_COUNT${NC} results"
        echo "$TAVILY_RESPONSE" | $JQ_CMD '.results[0] | {title, url, content: (.content[:100] + "...")}' 2>/dev/null || echo "$TAVILY_RESPONSE"
    else
        echo -e "${YELLOW}⚠ Tavily API returned no results${NC}"
        echo "$TAVILY_RESPONSE" | $JQ_CMD 2>/dev/null || echo "$TAVILY_RESPONSE"
    fi
fi

echo ""

# Test 2: Web search tool via GenAI API (complete-with-tools)
echo -e "${BLUE}[Test 2] Testing web_search tool via GenAI API...${NC}"

# Get auth token (if needed) - for now we'll test without auth if API allows
# In production, you'd need to authenticate first

WEB_SEARCH_REQUEST=$(cat <<EOF
{
  "prompt": "Search the web for: $TEST_QUERY",
  "tools": [
    {
      "name": "web_search",
      "description": "Search the web for current information. Use this tool when you need to find up-to-date information from the internet, check current events, look up facts, or find recent news.",
      "parameters": {
        "type": "object",
        "properties": {
          "query": {
            "type": "string",
            "description": "Search query in natural language"
          },
          "limit": {
            "type": "integer",
            "description": "Maximum number of results to return (1-20, default: 10)",
            "minimum": 1,
            "maximum": 20
          }
        },
        "required": ["query"]
      }
    }
  ],
  "options": {
    "modelOverride": "gemini_fast",
    "temperature": 0.1,
    "maxOutputTokens": 2000
  }
}
EOF
)

echo -e "${BLUE}  Sending request to: ${API_URL}/api/v1/genai/complete-with-tools${NC}"
echo -e "${BLUE}  Query: ${YELLOW}$TEST_QUERY${NC}"

WEB_SEARCH_RESPONSE=$(curl -s -X POST "${API_URL}/api/v1/genai/complete-with-tools" \
    -H "Content-Type: application/json" \
    -d "$WEB_SEARCH_REQUEST")

# Check for errors
if echo "$WEB_SEARCH_RESPONSE" | grep -q '"error"\|"code"'; then
    ERROR_CODE=$(echo "$WEB_SEARCH_RESPONSE" | $JQ_CMD -r '.code // .error // "unknown"' 2>/dev/null || echo "unknown")
    ERROR_MSG=$(echo "$WEB_SEARCH_RESPONSE" | $JQ_CMD -r '.message // .error // "unknown error"' 2>/dev/null || echo "unknown error")
    
    if [[ "$ERROR_CODE" == "UNAUTHORIZED" ]] || [[ "$ERROR_MSG" == *"auth"* ]] || [[ "$ERROR_MSG" == *"Auth"* ]]; then
        echo -e "${YELLOW}⚠ Authentication required${NC}"
        echo -e "${YELLOW}  This endpoint requires authentication. Testing tool execution directly...${NC}"
        echo ""
        
        # Test 3: Direct tool execution test (if we can access internal endpoint or test locally)
        echo -e "${BLUE}[Test 3] Testing web_search tool executor directly...${NC}"
        echo -e "${YELLOW}  Note: This requires local testing or internal API access${NC}"
        echo -e "${YELLOW}  To test locally, run:${NC}"
        echo -e "${YELLOW}    cd backend && go run -tags test ./cmd/test-web-search-tool${NC}"
    else
        echo -e "${RED}✗ API error: $ERROR_CODE - $ERROR_MSG${NC}"
        echo "$WEB_SEARCH_RESPONSE" | $JQ_CMD
        exit 1
    fi
else
    # Check if tool was called
    TOOL_CALLS=$(echo "$WEB_SEARCH_RESPONSE" | $JQ_CMD -r '.toolCalls // [] | length' 2>/dev/null || echo "0")
    CONTENT=$(echo "$WEB_SEARCH_RESPONSE" | $JQ_CMD -r '.content // ""' 2>/dev/null || echo "")
    
    if [[ "$TOOL_CALLS" -gt 0 ]]; then
        echo -e "${GREEN}✓ Tool was called successfully${NC}"
        echo -e "  Tool calls: ${GREEN}$TOOL_CALLS${NC}"
        
        # Extract tool call details
        echo "$WEB_SEARCH_RESPONSE" | $JQ_CMD '.toolCalls[] | {name, arguments}' 2>/dev/null || echo "$WEB_SEARCH_RESPONSE"
        
        # Check if we have results in content or need to check tool results
        if [[ -n "$CONTENT" ]]; then
            echo -e "${GREEN}✓ Response content received${NC}"
            echo -e "  Content preview: ${CONTENT:0:200}..."
        fi
    else
        echo -e "${YELLOW}⚠ No tool calls detected in response${NC}"
        echo "$WEB_SEARCH_RESPONSE" | $JQ_CMD
    fi
fi

echo ""

# Test 4: Verify Tavily key is accessible in Cloud Run
echo -e "${BLUE}[Test 4] Verifying Tavily secret in Cloud Run...${NC}"
if command -v gcloud >/dev/null 2>&1; then
    SECRET_CHECK=$(gcloud run services describe etigram-api --region=us-central1 \
        --format="value(spec.template.spec.containers[0].env)" 2>/dev/null | grep -o "TAVILY_API_KEY" || echo "")
    
    if [[ -n "$SECRET_CHECK" ]]; then
        echo -e "${GREEN}✓ TAVILY_API_KEY is configured in Cloud Run service${NC}"
    else
        echo -e "${YELLOW}⚠ TAVILY_API_KEY not found in Cloud Run env vars (may be in secrets)${NC}"
    fi
    
    # Check if secret exists
    if gcloud secrets describe tavily_api_key >/dev/null 2>&1; then
        echo -e "${GREEN}✓ tavily_api_key secret exists in GCP Secret Manager${NC}"
    else
        echo -e "${RED}✗ tavily_api_key secret not found${NC}"
    fi
else
    echo -e "${YELLOW}⚠ gcloud not available, skipping Cloud Run check${NC}"
fi

echo ""
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}Test Summary:${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"

if [[ "${TAVILY_DIRECT_TEST_FAILED:-false}" == "true" ]]; then
    echo -e "Tavily API: ${RED}✗ Direct connection failed (check API key)${NC}"
else
    echo -e "Tavily API: ${GREEN}✓ Direct connection works${NC}"
fi

echo -e "Test Query: ${YELLOW}$TEST_QUERY${NC}"
echo -e "API URL: ${BLUE}$API_URL${NC}"
echo ""
echo -e "${YELLOW}Additional Testing Options:${NC}"
echo -e "  1. Test tool executor locally:"
echo -e "     ${BLUE}cd backend && TAVILY_API_KEY=\$TAVILY_API_KEY go run ./cmd/test-web-search-tool${NC}"
echo -e "  2. Test with authentication (requires valid token):"
echo -e "     ${BLUE}curl -H \"Authorization: Bearer <token>\" ...${NC}"
echo ""

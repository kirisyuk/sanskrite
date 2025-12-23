#!/bin/bash

# Test script for Tesla Battery Logistics Lens
# Tenant: kirisyuk@gmail.com
# Usage: ./test-tesla-batteries-logistics.sh <PROJECT_ID> <API_BASE_URL>

set -e

PROJECT_ID="${1:-}"
API_BASE_URL="${2:-http://localhost:8080}"

if [ -z "$PROJECT_ID" ]; then
  echo "Usage: $0 <PROJECT_ID> [API_BASE_URL]"
  echo ""
  echo "First, get your project ID:"
  echo "  curl -H 'Authorization: Bearer YOUR_TOKEN' $API_BASE_URL/api/v1/projects"
  exit 1
fi

# Update JSON with project ID
sed "s/REPLACE_WITH_PROJECT_ID/$PROJECT_ID/g" test-tesla-batteries-logistics.json > /tmp/lens-request.json

echo "Creating Lens with:"
cat /tmp/lens-request.json | jq '.'

echo ""
echo "Sending POST request to $API_BASE_URL/api/horizon/lenses..."
echo ""

# Note: You need to set AUTH_TOKEN environment variable or use gcloud auth
if [ -z "$AUTH_TOKEN" ]; then
  echo "Warning: AUTH_TOKEN not set. Using gcloud auth token..."
  AUTH_TOKEN=$(gcloud auth print-access-token)
fi

RESPONSE=$(curl -s -w "\n%{http_code}" \
  -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -d @/tmp/lens-request.json \
  "$API_BASE_URL/api/horizon/lenses")

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | sed '$d')

echo "HTTP Status: $HTTP_CODE"
echo "Response:"
echo "$BODY" | jq '.' 2>/dev/null || echo "$BODY"

if [ "$HTTP_CODE" -eq 200 ] || [ "$HTTP_CODE" -eq 201 ]; then
  LENS_ID=$(echo "$BODY" | jq -r '.id // empty')
  if [ -n "$LENS_ID" ]; then
    echo ""
    echo "✅ Lens created successfully!"
    echo "Lens ID: $LENS_ID"
    echo ""
    echo "Monitor assembly status:"
    echo "  curl -H 'Authorization: Bearer $AUTH_TOKEN' $API_BASE_URL/api/horizon/lenses/$LENS_ID/assembly-status"
    echo ""
    echo "Check lens details:"
    echo "  curl -H 'Authorization: Bearer $AUTH_TOKEN' $API_BASE_URL/api/horizon/lenses/$LENS_ID"
  fi
else
  echo ""
  echo "❌ Failed to create lens"
  exit 1
fi


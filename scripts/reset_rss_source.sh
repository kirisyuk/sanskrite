#!/bin/bash
set -e

SOURCE_ID="0juOUd5xxhFMZ4GydpHz"

echo "Resetting status and errorCount for source $SOURCE_ID..."

# Using gcloud firestore documents update
# Requires alpha/beta component or recent gcloud
# Alternative: use curl to API if we had an admin endpoint, or firestore update via patch

# Construct the resource path
# Assuming default database
RESOURCE_PATH="projects/etigram/databases/(default)/documents/core_datasources/$SOURCE_ID"

echo "Updating $RESOURCE_PATH..."

# We need to use curl because gcloud firestore documents update is limited
# Get access token
TOKEN=$(gcloud auth print-access-token)

# Patch request to reset status and errorCount
curl -X PATCH \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  "https://firestore.googleapis.com/v1/$RESOURCE_PATH?updateMask.fieldPaths=status&updateMask.fieldPaths=errorCount" \
  -d '{
    "fields": {
      "status": { "stringValue": "active" },
      "errorCount": { "integerValue": 0 }
    }
  }'

echo ""
echo "Reset complete. The source should be picked up by the next poll cycle."


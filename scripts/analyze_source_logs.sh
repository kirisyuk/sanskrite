#!/bin/bash
set -e

SOURCE_ID="0juOUd5xxhFMZ4GydpHz"
PROJECT_ID="etigram"

echo "Fetching recent logs for source $SOURCE_ID in project $PROJECT_ID..."

# Fetch logs from the last 20 minutes
# We look for the source ID in textPayload to catch poller logs
gcloud logging read "resource.type=cloud_run_revision AND textPayload:\"$SOURCE_ID\"" \
  --project="$PROJECT_ID" \
  --limit=50 \
  --freshness=20m \
  --format="table(timestamp, textPayload)"

echo "---------------------------------------------------"
echo "Checking for general poller activity (PublishDue)..."
# Check if the poller triggered recently (looking for "source poller: checking")
gcloud logging read "resource.type=cloud_run_revision AND textPayload:\"source poller: checking\"" \
  --project="$PROJECT_ID" \
  --limit=10 \
  --freshness=20m \
  --format="table(timestamp, textPayload)"


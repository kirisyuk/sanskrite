#!/bin/bash
set -e

# RSS Source ID from user query
SOURCE_ID="0juOUd5xxhFMZ4GydpHz"
KIND="rss"
# Tenant ID might be empty for public source, or specific.
# We'll send empty tenantId and let poller resolve it from active projects like it does for scheduled polls.
PAYLOAD="{\"tenantId\": \"\", \"sourceId\": \"$SOURCE_ID\", \"kind\": \"$KIND\"}"

echo "Project ID: $GCP_PROJECT_ID"
if [ -z "$GCP_PROJECT_ID" ]; then
    echo "GCP_PROJECT_ID not set. Trying to get from gcloud config..."
    GCP_PROJECT_ID=$(gcloud config get-value project)
    echo "Using Project ID: $GCP_PROJECT_ID"
fi

echo "Publishing manual poll event for RSS source $SOURCE_ID..."
gcloud pubsub topics publish source.poll --message="$PAYLOAD" --project="$GCP_PROJECT_ID"

echo "Event published. Waiting 15 seconds for processing..."
sleep 15

echo "Fetching recent logs for this source..."
# Filter logs for the last 5 minutes, looking for the source ID
gcloud logging read "resource.type=cloud_run_revision AND textPayload:\"$SOURCE_ID\"" --limit=20 --format="table(timestamp, textPayload)" --project="$GCP_PROJECT_ID" --freshness=5m

echo "Done."


#!/bin/bash
set -e

SOURCE_ID="0juOUd5xxhFMZ4GydpHz"
PROJECT_ID="h1horizon-488615"

echo "Fetching logs for source $SOURCE_ID covering the 09:13 poll window..."

# Fetch logs from 09:10 to 09:30 UTC to catch the poll attempt and potential timeout
# Assuming current time is past 10:00 UTC based on previous logs
# We look specifically for the processing start and result
gcloud logging read "resource.type=cloud_run_revision AND textPayload:\"$SOURCE_ID\" AND timestamp>=\"2025-12-02T09:10:00Z\" AND timestamp<=\"2025-12-02T09:30:00Z\"" \
  --project="$PROJECT_ID" \
  --limit=50 \
  --format="table(timestamp, textPayload)"


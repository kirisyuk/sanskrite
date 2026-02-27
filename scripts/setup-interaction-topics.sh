#!/bin/bash

# DEPRECATED: Interaction events now go through hatlone.interaction.events (hatlone-ws).
# This script created subscriptions for legacy ws-broker; use deploy or setup-horizon-pubsub.sh instead.
#
# Script to create Pub/Sub topics and subscriptions for interaction events
# Usage: ./scripts/setup-interaction-topics.sh [PROJECT_ID]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get project ID from argument or gcloud config
PROJECT_ID="${1:-$(gcloud config get-value project 2>/dev/null)}"

if [ -z "$PROJECT_ID" ]; then
  echo -e "${RED}Error: PROJECT_ID is required${NC}"
  echo "Usage: $0 [PROJECT_ID]"
  echo "Or set default project: gcloud config set project PROJECT_ID"
  exit 1
fi

echo -e "${BLUE}Setting up interaction topics for project:${NC} $PROJECT_ID"

# Topics to create
TOPICS=(
  "interaction.created"
  "interaction.updated"
)

# Create topics
for topic in "${TOPICS[@]}"; do
  echo -e "${BLUE}Creating topic:${NC} $topic"
  
  if gcloud pubsub topics describe "$topic" --project="$PROJECT_ID" >/dev/null 2>&1; then
    echo -e "${YELLOW}Topic $topic already exists, skipping${NC}"
  else
    if gcloud pubsub topics create "$topic" --project="$PROJECT_ID" 2>&1; then
      echo -e "${GREEN}✓ Created topic:${NC} $topic"
    else
      echo -e "${RED}✗ Failed to create topic:${NC} $topic"
      exit 1
    fi
  fi
  
  # Enable message ordering for the topic (if supported)
  # Note: Message ordering is typically enabled at subscription level, not topic level
  echo -e "${BLUE}Topic $topic is ready${NC}"
done

# Create subscriptions for ws-broker
for topic in "${TOPICS[@]}"; do
  sub_id="${topic}-ws-broker"
  echo -e "${BLUE}Creating subscription:${NC} $sub_id"
  
  if gcloud pubsub subscriptions describe "$sub_id" --project="$PROJECT_ID" >/dev/null 2>&1; then
    echo -e "${YELLOW}Subscription $sub_id already exists${NC}"
    
    # Check if ordering is enabled
    ordering=$(gcloud pubsub subscriptions describe "$sub_id" --project="$PROJECT_ID" --format="value(enableMessageOrdering)" 2>/dev/null || echo "false")
    if [ "$ordering" != "True" ]; then
      echo -e "${YELLOW}Recreating subscription with message ordering enabled${NC}"
      gcloud pubsub subscriptions delete "$sub_id" --project="$PROJECT_ID" --quiet || true
      gcloud pubsub subscriptions create "$sub_id" \
        --topic="$topic" \
        --project="$PROJECT_ID" \
        --enable-message-ordering \
        2>&1 && echo -e "${GREEN}✓ Recreated subscription with ordering:${NC} $sub_id" || {
        echo -e "${RED}✗ Failed to recreate subscription:${NC} $sub_id"
        exit 1
      }
    else
      echo -e "${GREEN}✓ Subscription $sub_id already has ordering enabled${NC}"
    fi
  else
    if gcloud pubsub subscriptions create "$sub_id" \
      --topic="$topic" \
      --project="$PROJECT_ID" \
      --enable-message-ordering \
      2>&1; then
      echo -e "${GREEN}✓ Created subscription:${NC} $sub_id"
    else
      echo -e "${RED}✗ Failed to create subscription:${NC} $sub_id"
      exit 1
    fi
  fi
done

echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ Setup Complete${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${BLUE}Created topics:${NC}"
for topic in "${TOPICS[@]}"; do
  echo "  - $topic"
done
echo ""
echo -e "${BLUE}Created subscriptions:${NC}"
for topic in "${TOPICS[@]}"; do
  echo "  - ${topic}-ws-broker"
done
echo ""
echo -e "${YELLOW}Note:${NC} (Deprecated) Interaction events use hatlone.interaction.events; restart hatlone-ws if needed"
echo ""


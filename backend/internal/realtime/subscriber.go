package realtime

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
)

// Subscriber binds Pub/Sub topics to WS hub broadcasts per tenant
type Subscriber struct {
	client *pubsub.Client
	hub    *Hub
	logger *log.Logger
}

func NewSubscriber(client *pubsub.Client, hub *Hub, logger *log.Logger) *Subscriber {
	return &Subscriber{client: client, hub: hub, logger: logger}
}

func (s *Subscriber) Start(ctx context.Context, topicNames ...string) error {
	if s.logger != nil {
		s.logger.Printf("subscriber: starting for %d topics", len(topicNames))
	}
	for _, tname := range topicNames {
		subID := tname + "-ws-broker"
		sub := s.client.Subscription(subID)
		exists, err := sub.Exists(ctx)
		if err != nil {
			if s.logger != nil {
				s.logger.Printf("subscriber: error checking subscription %s: %v", subID, err)
			}
			return err
		}
		if !exists {
			t := s.client.Topic(tname)
			if ok, _ := t.Exists(ctx); !ok {
				if s.logger != nil {
					s.logger.Printf("subscriber: topic %s not found; skipping subscription", tname)
				}
				continue
			}
			created, err := s.client.CreateSubscription(ctx, subID, pubsub.SubscriptionConfig{Topic: t, EnableMessageOrdering: true})
			if err != nil {
				return err
			}
			sub = created
		} else {
			// If subscription exists but ordering is not enabled, recreate for genai.stream, verified.stream and artifact topics
			if cfg, err := sub.Config(ctx); err == nil {
				if !cfg.EnableMessageOrdering && (tname == TopicGenAIStreams || tname == TopicVerifiedStreams || tname == TopicArtifactEvents) {
					_ = sub.Delete(ctx)
					t := s.client.Topic(tname)
					created, err := s.client.CreateSubscription(ctx, subID, pubsub.SubscriptionConfig{Topic: t, EnableMessageOrdering: true})
					if err == nil {
						sub = created
					} else {
						s.logger.Printf("subscriber: recreate with ordering failed for %s: %v", subID, err)
					}
				}
			}
		}
		if s.logger != nil {
			s.logger.Printf("subscriber: subscribing to %s (subscription: %s)", tname, subID)
		}
		go s.runWithRetry(ctx, sub, tname)
	}
	if s.logger != nil {
		s.logger.Printf("subscriber: started successfully for %d topics", len(topicNames))
	}
	return nil
}

// runWithRetry runs subscription with automatic retry on errors
func (s *Subscriber) runWithRetry(ctx context.Context, sub *pubsub.Subscription, topicName string) {
	retryDelay := time.Second
	maxRetryDelay := 30 * time.Second

	for {
		// Check if context is cancelled (for graceful shutdown)
		select {
		case <-ctx.Done():
			if s.logger != nil {
				s.logger.Printf("subscriber: stopping subscription for %s (context cancelled)", topicName)
			}
			return
		default:
		}

		// Run subscription
		err := sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
			// CRITICAL: Wrap handleMessage in panic recovery to ensure message is always ack'd
			// This prevents Pub/Sub ordering key blocking if handleMessage panics
			defer func() {
				if r := recover(); r != nil {
					if s.logger != nil {
						s.logger.Printf("subscriber: PANIC in handleMessage for topic=%s: %v, acking message to prevent ordering key block", topicName, r)
					}
					// Always ack even on panic to prevent ordering key blocking
					m.Ack()
				}
			}()
			s.handleMessage(ctx, m)
		})

		// If Receive returns, it means either context was cancelled or there was an error
		if err != nil {
			if err == context.Canceled || err == context.DeadlineExceeded {
				// Context cancelled - graceful shutdown
				if s.logger != nil {
					s.logger.Printf("subscriber: subscription for %s stopped (context cancelled)", topicName)
				}
				return
			}

			// Error occurred - log and retry
			if s.logger != nil {
				s.logger.Printf("subscriber: error receiving messages for %s: %v, retrying in %v", topicName, err, retryDelay)
			}

			// Wait before retry with exponential backoff
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryDelay):
				// Increase retry delay exponentially, but cap at maxRetryDelay
				retryDelay *= 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
			}
		} else {
			// Receive returned without error (shouldn't happen normally, but reset delay if it does)
			if s.logger != nil {
				s.logger.Printf("subscriber: subscription for %s returned without error, resetting retry delay", topicName)
			}
			retryDelay = time.Second
		}

		// Log successful reconnection
		if s.logger != nil && retryDelay > time.Second {
			s.logger.Printf("subscriber: successfully reconnected to %s, resetting retry delay", topicName)
			retryDelay = time.Second
		}
	}
}

func (s *Subscriber) handleMessage(_ context.Context, m *pubsub.Message) {
	tenant := m.Attributes["tenantId"]
	et := m.Attributes["eventType"]
	projectID := m.Attributes["projectId"]
	streamIDForLog := m.Attributes["streamId"]
	msgID := m.ID

	// Log message receipt for streaming events
	if s.logger != nil && (strings.HasPrefix(et, "genai.stream") || strings.HasPrefix(et, "artifact.") || strings.HasPrefix(et, "verified.stream")) {
		s.logger.Printf("subscriber.handleMessage: RECEIVED et=%s tenant=%s project=%s streamId=%s msgId=%s", et, tenant, projectID, streamIDForLog, msgID)
	}

	// Ack after processing to keep ordering guarantees; guard against panics to avoid lockups
	defer func() {
		if r := recover(); r != nil && s.logger != nil {
			s.logger.Printf("subscriber: PANIC while handling message et=%s tenant=%s project=%s streamId=%s msgId=%s err=%v", et, tenant, projectID, streamIDForLog, msgID, r)
		}
		m.Ack()
		if s.logger != nil {
			s.logger.Printf("subscriber: acked message et=%s tenant=%s project=%s streamId=%s msgId=%s", et, tenant, projectID, streamIDForLog, msgID)
		}
	}()

	logDelivery := func(attrs map[string]string) {
		if s.logger == nil {
			return
		}
		if strings.HasPrefix(et, "genai.stream") || strings.HasPrefix(et, "artifact.") || strings.HasPrefix(et, "verified.stream") {
			connCount := 0
			if projectID != "" {
				connCount = s.hub.GetProjectConnCount(tenant, projectID)
			}
			s.logger.Printf("subscriber: deliver et=%s tenant=%s project=%s streamId=%s connCount=%d attrs=%v", et, tenant, projectID, streamIDForLog, connCount, attrs)
		}
		if et == "analysis.status" {
			s.logger.Printf("subscriber: received analysis.status for tenantId=%s, payload=%s", tenant, string(m.Data))
		}
		if et == "agent.update" {
			connCount := 0
			if projectID != "" {
				connCount = s.hub.GetProjectConnCount(tenant, projectID)
			}
			s.logger.Printf("subscriber: received agent.update for tenantId=%s, projectId=%s, connCount=%d, payload=%s", tenant, projectID, connCount, string(m.Data))
		}
		if et == "interaction.created" || et == "interaction.updated" {
			connCount := 0
			if projectID != "" {
				connCount = s.hub.GetProjectConnCount(tenant, projectID)
			}
			s.logger.Printf("subscriber: received %s for tenantId=%s, projectId=%s, connCount=%d, payload=%s", et, tenant, projectID, connCount, string(m.Data))
		}
	}

	logDelivery(m.Attributes)

	dispatchEnvelope := func(obj map[string]interface{}) {
		envelope := map[string]interface{}{
			"type": et,
			"data": obj,
		}
		if strings.HasPrefix(et, "verified.stream.") {
			if code, ok := obj["code"].(string); ok && code != "" {
				s.hub.BroadcastVerified(code, envelope)
			}
			return
		}
		if projectID != "" {
			obj["projectId"] = projectID
			s.hub.BroadcastJSONProject(tenant, projectID, envelope)
			return
		}
		if strings.HasPrefix(et, "genai.stream") && s.logger != nil {
			if payloadProjectID, ok := obj["projectId"].(string); ok && payloadProjectID != "" {
				s.logger.Printf("subscriber: genai.stream event missing projectId in attrs, using payload projectId=%s tenant=%s", payloadProjectID, tenant)
				obj["projectId"] = payloadProjectID
				s.hub.BroadcastJSONProject(tenant, payloadProjectID, envelope)
				return
			}
			s.logger.Printf("subscriber: genai.stream event missing projectId, broadcasting to all tenant connections tenant=%s", tenant)
		}
		s.hub.BroadcastJSON(tenant, envelope)
	}

	// Try object payload first
	var obj map[string]interface{}
	if err := json.Unmarshal(m.Data, &obj); err == nil {
		dispatchEnvelope(obj)
		return
	}

	// If array payload, broadcast each element (object only)
	var arr []interface{}
	if err := json.Unmarshal(m.Data, &arr); err == nil {
		for _, el := range arr {
			if o, ok := el.(map[string]interface{}); ok {
				dispatchEnvelope(o)
			}
		}
		return
	}

	s.logger.Printf("subscriber: json error: %s", string(m.Data))
}

package realtime

import (
	"context"
	"encoding/json"
	"log"
	"strings"

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
			// If subscription exists but ordering is not enabled, recreate for genai.stream topic
			if cfg, err := sub.Config(ctx); err == nil {
				if !cfg.EnableMessageOrdering && (tname == string(EventGenAIStream) || strings.HasPrefix(tname, "artifact.")) {
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
		go s.run(ctx, sub)
	}
	if s.logger != nil {
		s.logger.Printf("subscriber: started successfully for %d topics", len(topicNames))
	}
	return nil
}

func (s *Subscriber) run(ctx context.Context, sub *pubsub.Subscription) {
	_ = sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		tenant := m.Attributes["tenantId"]
		et := m.Attributes["eventType"]
		projectID := m.Attributes["projectId"]

		// Diagnostics: log genai/artifact deliveries with project routing and conn counts
		if s.logger != nil && (strings.HasPrefix(et, "genai.stream") || strings.HasPrefix(et, "artifact.")) {
			connCount := 0
			if projectID != "" {
				connCount = s.hub.GetProjectConnCount(tenant, projectID)
			}
			s.logger.Printf("subscriber: deliver et=%s tenant=%s project=%s connCount=%d attrs=%v", et, tenant, projectID, connCount, m.Attributes)
		}

		// DEBUG: log analysis.status events
		if et == "analysis.status" && s.logger != nil {
			s.logger.Printf("subscriber: received analysis.status for tenantId=%s, payload=%s", tenant, string(m.Data))
		}

		// DEBUG: log agent.update events
		if et == "agent.update" && s.logger != nil {
			connCount := 0
			if projectID != "" {
				connCount = s.hub.GetProjectConnCount(tenant, projectID)
			}
			s.logger.Printf("subscriber: received agent.update for tenantId=%s, projectId=%s, connCount=%d, payload=%s", tenant, projectID, connCount, string(m.Data))
		}

		// Try object payload first
		var obj map[string]interface{}
		if err := json.Unmarshal(m.Data, &obj); err == nil {
			envelope := map[string]interface{}{
				"type": et,
				"data": obj,
			}
			// Prefer routing by project if present
			if projectID != "" {
				// Pass-through projectId inside envelope for FE-side checks too
				obj["projectId"] = projectID
				s.hub.BroadcastJSONProject(tenant, projectID, envelope)
			} else {
				s.hub.BroadcastJSON(tenant, envelope)
			}
			m.Ack()
			return
		}

		// If array payload, broadcast each element (object only)
		var arr []interface{}
		if err := json.Unmarshal(m.Data, &arr); err == nil {
			for _, el := range arr {
				if o, ok := el.(map[string]interface{}); ok {
					envelope := map[string]interface{}{
						"type": et,
						"data": o,
					}
					if projectID != "" {
						o["projectId"] = projectID
						s.hub.BroadcastJSONProject(tenant, projectID, envelope)
					} else {
						s.hub.BroadcastJSON(tenant, envelope)
					}
				}
			}
			m.Ack()
			return
		}

		s.logger.Printf("subscriber: json error: %s", string(m.Data))
		m.Ack()
	})
}

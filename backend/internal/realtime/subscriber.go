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
	for _, tname := range topicNames {
		subID := tname + "-ws"
		sub := s.client.Subscription(subID)
		exists, err := sub.Exists(ctx)
		if err != nil {
			return err
		}
		if !exists {
			t := s.client.Topic(tname)
			if ok, _ := t.Exists(ctx); !ok {
				return nil
			}
			created, err := s.client.CreateSubscription(ctx, subID, pubsub.SubscriptionConfig{Topic: t, EnableMessageOrdering: true})
			if err != nil {
				return err
			}
			sub = created
		} else {
			// If subscription exists but ordering is not enabled, recreate for genai.stream.* topics
			if cfg, err := sub.Config(ctx); err == nil {
				if !cfg.EnableMessageOrdering && strings.HasPrefix(tname, "genai.stream") {
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
		go s.run(ctx, sub)
	}
	return nil
}

func (s *Subscriber) run(ctx context.Context, sub *pubsub.Subscription) {
	_ = sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		tenant := m.Attributes["tenantId"]
		et := m.Attributes["eventType"]

		// Try object payload first
		var obj map[string]interface{}
		if err := json.Unmarshal(m.Data, &obj); err == nil {
			envelope := map[string]interface{}{
				"type": et,
				"data": obj,
			}
			s.hub.BroadcastJSON(tenant, envelope)
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
					s.hub.BroadcastJSON(tenant, envelope)
				}
			}
			m.Ack()
			return
		}

		s.logger.Printf("subscriber: json error: %s", string(m.Data))
		m.Ack()
	})
}

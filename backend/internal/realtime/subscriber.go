package realtime

import (
	"context"
	"encoding/json"
	"log"

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
			created, err := s.client.CreateSubscription(ctx, subID, pubsub.SubscriptionConfig{Topic: t})
			if err != nil {
				return err
			}
			sub = created
		}
		go s.run(ctx, sub)
	}
	return nil
}

func (s *Subscriber) run(ctx context.Context, sub *pubsub.Subscription) {
	_ = sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		tenant := m.Attributes["tenantId"]
		et := m.Attributes["eventType"]
		var payload map[string]interface{}
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			s.logger.Printf("subscriber: json error: %v", err)
			m.Ack()
			return
		}
		envelope := map[string]interface{}{
			"type": et,
			"data": payload,
		}
		s.hub.BroadcastJSON(tenant, envelope)
		m.Ack()
	})
}

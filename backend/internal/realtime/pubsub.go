package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/pubsub"
)

type EventType string

const (
	EventDiscoveryStatus  EventType = "discovery.status"
	EventDiscoveryProfile EventType = "discovery.profile"
	EventDiscoveryAnswer  EventType = "discovery.answer"
)

type Publisher struct {
	client *pubsub.Client
	topics map[EventType]*pubsub.Topic
}

func NewPublisher(ctx context.Context, projectID string) (*Publisher, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, err
	}

	p := &Publisher{client: client, topics: map[EventType]*pubsub.Topic{}}
	// Ensure topics exist
	for _, et := range []EventType{EventDiscoveryStatus, EventDiscoveryProfile, EventDiscoveryAnswer} {
		name := string(et)
		t := client.Topic(name)
		exists, err := t.Exists(ctx)
		if err != nil {
			return nil, err
		}
		if !exists {
			t, err = client.CreateTopic(ctx, name)
			if err != nil {
				log.Printf("realtime: create topic %s failed: %v", name, err)
				// continue with non-fatal; attempt to publish may fail later
			}
		}
		p.topics[et] = t
	}
	return p, nil
}

func (p *Publisher) PublishJSON(ctx context.Context, et EventType, tenantID string, payload interface{}) error {
	t, ok := p.topics[et]
	if !ok || t == nil {
		return fmt.Errorf("topic not initialized: %s", et)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	res := t.Publish(ctx, &pubsub.Message{
		Data: b,
		Attributes: map[string]string{
			"tenantId":  tenantID,
			"eventType": string(et),
			"ts":        fmt.Sprintf("%d", time.Now().UnixMilli()),
		},
	})
	_, err = res.Get(ctx)
	return err
}

// Client exposes the underlying pubsub.Client for subscriber wiring
func (p *Publisher) Client() *pubsub.Client { return p.client }

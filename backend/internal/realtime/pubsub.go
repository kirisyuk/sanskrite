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
	EventDiscoveryProject EventType = "discovery.project"
	EventDocProcess       EventType = "document.process"
	EventGroundRefresh    EventType = "ground.refresh"
	EventSourcePoll       EventType = "source.poll"
	EventSourcePollDue    EventType = "source.poll.due"
	// GenAI streaming and tool events
	EventGenAIStreamStart   EventType = "genai.stream.start"
	EventGenAIStreamChunk   EventType = "genai.stream.chunk"
	EventGenAIStreamTool    EventType = "genai.stream.tool_call"
	EventGenAIStreamToolRes EventType = "genai.stream.tool_result"
	EventGenAIStreamDone    EventType = "genai.stream.done"
	EventGenAIStreamError   EventType = "genai.stream.error"
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
	for _, et := range []EventType{
		EventDiscoveryStatus,
		EventDiscoveryProfile,
		EventDiscoveryAnswer,
		EventDiscoveryProject,
		EventDocProcess,
		EventGroundRefresh,
		EventSourcePoll,
		EventSourcePollDue,
		// GenAI topics
		EventGenAIStreamStart,
		EventGenAIStreamChunk,
		EventGenAIStreamTool,
		EventGenAIStreamToolRes,
		EventGenAIStreamDone,
		EventGenAIStreamError,
	} {
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

	// Route all GenAI stream event types to a single physical topic to preserve order across events
	// We reuse genai.stream.chunk topic for all and enable ordering
	if tChunk, ok := p.topics[EventGenAIStreamChunk]; ok && tChunk != nil {
		tChunk.EnableMessageOrdering = true
		for _, et := range []EventType{
			EventGenAIStreamStart,
			EventGenAIStreamChunk,
			EventGenAIStreamTool,
			EventGenAIStreamToolRes,
			EventGenAIStreamDone,
			EventGenAIStreamError,
		} {
			p.topics[et] = tChunk
		}
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
		OrderingKey: tenantID,
	})
	_, err = res.Get(ctx)
	return err
}

// Client exposes the underlying pubsub.Client for subscriber wiring
func (p *Publisher) Client() *pubsub.Client { return p.client }

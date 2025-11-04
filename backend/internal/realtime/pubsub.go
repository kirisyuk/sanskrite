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
	// GenAI streaming: all events go to a single topic for ordering
	EventGenAIStream EventType = "genai.stream"
	// Artifact generation events
	EventArtifactGenerationStarted   EventType = "artifact.generation.started"
	EventArtifactSectionGenerating   EventType = "artifact.section.generating"
	EventArtifactSectionChunk        EventType = "artifact.section.chunk"
	EventArtifactSectionCompleted    EventType = "artifact.section.completed"
	EventArtifactAssemblyStarted     EventType = "artifact.assembly.started"
	EventArtifactAssemblyChunk       EventType = "artifact.assembly.chunk"
	EventArtifactAssemblyCompleted   EventType = "artifact.assembly.completed"
	EventArtifactGenerationCompleted EventType = "artifact.generation.completed"
	EventArtifactGenerationError     EventType = "artifact.generation.error"
	// Unified analysis status (entities/facts/events/metrics/decisions)
	EventAnalysisStatus EventType = "analysis.status"
	// Agent execution updates
	EventAgentUpdate EventType = "agent.update"
	// Verified URL analysis streaming: all events go to a single topic for ordering
	EventVerifiedStream EventType = "verified.stream"
)

var genaiStreamEventTypes = map[EventType]bool{
	"genai.stream.start":       true,
	"genai.stream.chunk":       true,
	"genai.stream.tool_call":   true,
	"genai.stream.tool_result":  true,
	"genai.stream.done":         true,
	"genai.stream.error":        true,
}

var verifiedStreamEventTypes = map[EventType]bool{
	"verified.stream.fetch_start":      true,
	"verified.stream.fetch_success":    true,
	"verified.stream.fetch_failed":     true,
	"verified.stream.analysis_start":   true,
	"verified.stream.analysis_complete": true,
	"verified.stream.analysis_failed":  true,
}

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
		// GenAI stream topic
		EventGenAIStream,
		// Artifact generation topics
		EventArtifactGenerationStarted,
		EventArtifactSectionGenerating,
		EventArtifactSectionChunk,
		EventArtifactSectionCompleted,
		EventArtifactAssemblyStarted,
		EventArtifactAssemblyChunk,
		EventArtifactAssemblyCompleted,
		EventArtifactGenerationCompleted,
		EventArtifactGenerationError,
		EventAnalysisStatus,
		EventAgentUpdate,
		// Verified stream topic
		EventVerifiedStream,
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

	// Ensure ordering is enabled per-topic for GenAI stream topics
	if t, ok := p.topics[EventGenAIStream]; ok && t != nil {
		t.EnableMessageOrdering = true
	}

	// Also enable ordering for artifact topics (stream-like)
	if t, ok := p.topics[EventArtifactGenerationStarted]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	if t, ok := p.topics[EventArtifactSectionGenerating]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	if t, ok := p.topics[EventArtifactSectionChunk]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	if t, ok := p.topics[EventArtifactSectionCompleted]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	if t, ok := p.topics[EventArtifactAssemblyStarted]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	if t, ok := p.topics[EventArtifactAssemblyChunk]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	if t, ok := p.topics[EventArtifactAssemblyCompleted]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	if t, ok := p.topics[EventArtifactGenerationCompleted]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	if t, ok := p.topics[EventArtifactGenerationError]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	// Enable ordering for verified stream topic
	if t, ok := p.topics[EventVerifiedStream]; ok && t != nil {
		t.EnableMessageOrdering = true
	}
	return p, nil
}

func (p *Publisher) PublishJSON(ctx context.Context, et EventType, tenantID string, payload interface{}) error {
	var topicToUse EventType
	// Reroute all genai.stream.* events to the single EventGenAIStream topic for ordering
	if _, isGenaiStream := genaiStreamEventTypes[et]; isGenaiStream {
		topicToUse = EventGenAIStream
	} else if _, isVerifiedStream := verifiedStreamEventTypes[et]; isVerifiedStream {
		// Reroute all verified.stream.* events to the single EventVerifiedStream topic for ordering
		topicToUse = EventVerifiedStream
	} else {
		topicToUse = et
	}

	t, ok := p.topics[topicToUse]
	if !ok || t == nil {
		return fmt.Errorf("topic not initialized: %s (routed from %s)", topicToUse, et)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// DEBUG: log analysis.status publications
	if et == EventAnalysisStatus {
		log.Printf("publisher: publishing %s for tenantId=%s, payload=%s", et, tenantID, string(b))
	}
	// DEBUG: log agent.update publications
	if et == EventAgentUpdate {
		log.Printf("publisher: publishing %s for tenantId=%s, payload=%s", et, tenantID, string(b))
	}
	// Determine whether to use ordering key for this topic (only for stream-like topics)
	requireOrdering := (topicToUse == EventGenAIStream ||
		topicToUse == EventArtifactGenerationStarted ||
		topicToUse == EventArtifactSectionGenerating ||
		topicToUse == EventArtifactSectionChunk ||
		topicToUse == EventArtifactSectionCompleted ||
		topicToUse == EventArtifactAssemblyStarted ||
		topicToUse == EventArtifactAssemblyChunk ||
		topicToUse == EventArtifactAssemblyCompleted ||
		topicToUse == EventArtifactGenerationCompleted ||
		topicToUse == EventArtifactGenerationError ||
		topicToUse == EventVerifiedStream)

	// Derive optional routing context from payload if present
	orderingKey := ""
	if requireOrdering {
		orderingKey = tenantID
	}
	attrs := map[string]string{
		"tenantId":  tenantID,
		"eventType": string(et),
		"ts":        fmt.Sprintf("%d", time.Now().UnixMilli()),
	}
	// If payload is an object, mirror projectId/streamId/code into attributes and ordering key
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err == nil {
		if v, ok := obj["projectId"].(string); ok && v != "" {
			attrs["projectId"] = v
			if requireOrdering {
				orderingKey = fmt.Sprintf("%s:%s", orderingKey, v)
			}
		}
		if v, ok := obj["streamId"].(string); ok && v != "" {
			attrs["streamId"] = v
			if requireOrdering {
				orderingKey = fmt.Sprintf("%s:%s", orderingKey, v)
			}
		}
		// For verified.stream events, use code for ordering
		if _, isVerifiedStream := verifiedStreamEventTypes[et]; isVerifiedStream {
			if v, ok := obj["code"].(string); ok && v != "" {
				attrs["code"] = v
				if requireOrdering {
					orderingKey = fmt.Sprintf("%s:%s", orderingKey, v)
				}
			}
		}
	}
	res := t.Publish(ctx, &pubsub.Message{
		Data:        b,
		Attributes:  attrs,
		OrderingKey: orderingKey,
	})
	_, err = res.Get(ctx)
	if err != nil && et == EventAnalysisStatus {
		log.Printf("publisher: ERROR publishing %s: %v", et, err)
	}
	return err
}

// Client exposes the underlying pubsub.Client for subscriber wiring
func (p *Publisher) Client() *pubsub.Client { return p.client }

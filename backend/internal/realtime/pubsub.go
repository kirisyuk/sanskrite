package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
	EventDocExtract       EventType = "document.extract" // Facts-only: NormalizedDocument → OntologyFacts (no Document/Chunks)
	EventGroundRefresh    EventType = "ground.refresh"
	EventSourcePoll       EventType = "source.poll"
	EventSourcePollDue    EventType = "source.poll.due"
	EventSourceCreated    EventType = "source.created" // Создание нового источника
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
	EventAgentUpdate     EventType = "agent.update"      // Общие обновления job (статус, прогресс)
	EventAgentStepUpdate EventType = "agent.step.update" // Детальные обновления шагов (статус, output, ошибки)
	// Interaction events (created/updated via tools)
	EventInteractionCreated EventType = "interaction.created"
	EventInteractionUpdated EventType = "interaction.updated"
	// Chat ontology processing (async extraction from chat messages)
	EventChatOntologyProcess EventType = "chat.ontology.process"
	// Verified URL analysis streaming: all events go to a single topic for ordering
	EventVerifiedStream EventType = "verified.stream"
	// Horizon events namespace (graph/lens/scout/propagation assembly)
	// NOTE: Horizon uses "horizon.*" event types for routing to a dedicated topic.
)

// Aggregated Pub/Sub topic names for the hatlone broker
const (
	TopicGenAIStreams      = "hatlone.genai.streams"
	TopicDiscoveryEvents   = "hatlone.discovery.events"
	TopicArtifactEvents    = "hatlone.artifact.events"
	TopicInteractionEvents = "hatlone.interaction.events"
	TopicAnalysisEvents    = "hatlone.analysis.events"
	TopicVerifiedStreams   = "hatlone.verified.streams"
	TopicHorizonEvents     = "hatlone.horizon.events"
	TopicOperationsEvents  = "hatlone.operations.events"
	TopicSourcePoll        = "source.poll"
)

// AggregatedTopics enumerates topics the new broker listens to (exported for cmds)
var AggregatedTopics = []string{
	TopicGenAIStreams,
	TopicDiscoveryEvents,
	TopicArtifactEvents,
	TopicInteractionEvents,
	TopicAnalysisEvents,
	TopicVerifiedStreams,
	TopicHorizonEvents,
	TopicOperationsEvents,
	TopicSourcePoll,
}

var genaiStreamEventTypes = map[EventType]bool{
	"genai.stream.start":       true,
	"genai.stream.chunk":       true,
	"genai.stream.tool_call":   true,
	"genai.stream.tool_result": true,
	"genai.stream.done":        true,
	"genai.stream.error":       true,
}

var verifiedStreamEventTypes = map[EventType]bool{
	"verified.stream.fetch_start":       true,
	"verified.stream.fetch_success":     true,
	"verified.stream.fetch_failed":      true,
	"verified.stream.analysis_start":    true,
	"verified.stream.analysis_complete": true,
	"verified.stream.analysis_failed":   true,
}

type Publisher struct {
	client *pubsub.Client
	topics map[string]*pubsub.Topic
}

func NewPublisher(ctx context.Context, projectID string) (*Publisher, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, err
	}

	p := &Publisher{client: client, topics: map[string]*pubsub.Topic{}}
	for _, name := range AggregatedTopics {
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
		p.topics[name] = t
	}

	enableOrdering := []string{
		TopicGenAIStreams,
		TopicArtifactEvents,
		TopicVerifiedStreams,
	}
	for _, name := range enableOrdering {
		if t, ok := p.topics[name]; ok && t != nil {
			t.EnableMessageOrdering = true
		}
	}
	return p, nil
}

func (p *Publisher) PublishJSON(ctx context.Context, et EventType, tenantID string, payload interface{}) error {
	topicToUse := routeTopicName(et)
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
	// DEBUG: log agent.update and agent.step.update publications
	if et == EventAgentUpdate || et == EventAgentStepUpdate {
		log.Printf("publisher: publishing %s for tenantId=%s, payload=%s", et, tenantID, string(b))
	}
	// Determine whether to use ordering key for this topic (only for stream-like topics)
	requireOrdering := topicToUse == TopicGenAIStreams ||
		topicToUse == TopicArtifactEvents ||
		topicToUse == TopicVerifiedStreams

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
	// Retry mechanism for critical events (genai.stream, analysis.status)
	maxRetries := 1
	if et == EventAnalysisStatus || strings.HasPrefix(string(et), "genai.stream") {
		maxRetries = 3
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		res := t.Publish(ctx, &pubsub.Message{
			Data:        b,
			Attributes:  attrs,
			OrderingKey: orderingKey,
		})
		_, err = res.Get(ctx)
		if err == nil {
			// Success - log for genai.stream events
			if strings.HasPrefix(string(et), "genai.stream") {
				log.Printf("publisher: published %s tenant=%s project=%s stream=%s (attempt %d)", et, tenantID, attrs["projectId"], attrs["streamId"], attempt+1)
			}
			return nil
		}
		lastErr = err

		// Log error for important event types
		if et == EventAnalysisStatus || strings.HasPrefix(string(et), "genai.stream") {
			log.Printf("publisher: ERROR publishing %s tenant=%s project=%s stream=%s (attempt %d/%d): %v", et, tenantID, attrs["projectId"], attrs["streamId"], attempt+1, maxRetries+1, err)
		}

		// Retry with exponential backoff (only if not last attempt)
		if attempt < maxRetries {
			backoff := time.Duration(attempt+1) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				// Continue retry
			}
		}
	}

	// All retries failed - critical error for genai.stream events
	if strings.HasPrefix(string(et), "genai.stream") {
		log.Printf("publisher: CRITICAL ERROR - failed to publish %s after %d attempts tenant=%s project=%s stream=%s: %v", et, maxRetries+1, tenantID, attrs["projectId"], attrs["streamId"], lastErr)
	}
	return lastErr
}

// Client exposes the underlying pubsub.Client for subscriber wiring
func (p *Publisher) Client() *pubsub.Client { return p.client }

func routeTopicName(et EventType) string {
	switch {
	case strings.HasPrefix(string(et), "horizon."):
		return TopicHorizonEvents
	case genaiStreamEventTypes[et] || strings.HasPrefix(string(et), "genai.stream."):
		return TopicGenAIStreams
	case strings.HasPrefix(string(et), "discovery."):
		return TopicDiscoveryEvents
	case strings.HasPrefix(string(et), "artifact.") || et == EventDocProcess || et == EventDocExtract:
		return TopicArtifactEvents
	case strings.HasPrefix(string(et), "interaction.") || et == EventAgentUpdate || et == EventAgentStepUpdate || et == EventChatOntologyProcess:
		return TopicInteractionEvents
	case et == EventAnalysisStatus:
		return TopicAnalysisEvents
	case verifiedStreamEventTypes[et] || strings.HasPrefix(string(et), "verified.stream."):
		return TopicVerifiedStreams
	case et == EventSourcePoll || et == EventSourcePollDue:
		return TopicSourcePoll
	default:
		return TopicOperationsEvents
	}
}

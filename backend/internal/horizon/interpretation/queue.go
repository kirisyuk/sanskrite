package interpretation

import (
	"context"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// InterpretationRequest represents a request for interpretation.
type InterpretationRequest struct {
	ID           string         `json:"id" firestore:"id"`
	FragmentID   string         `json:"fragmentId" firestore:"fragmentId"`
	CanonicalPath string        `json:"canonicalPath" firestore:"canonicalPath"`
	NodeLabel    string         `json:"nodeLabel" firestore:"nodeLabel"`
	Polarity     string         `json:"polarity" firestore:"polarity"` // "risk" | "opportunity"
	Priority     TriggerPriority `json:"priority" firestore:"priority"`
	TriggerType  TriggerType    `json:"triggerType" firestore:"triggerType"`
	TriggerValue float64        `json:"triggerValue" firestore:"triggerValue"`
	CreatedAt    time.Time      `json:"createdAt" firestore:"createdAt"`
	Status       string         `json:"status" firestore:"status"` // "pending" | "processing" | "completed" | "failed"
}

// InterpretationQueue manages the queue of interpretation requests.
type InterpretationQueue struct {
	col *firestore.CollectionRef
}

// NewInterpretationQueue creates a new InterpretationQueue.
func NewInterpretationQueue(client *firestore.Client) *InterpretationQueue {
	return &InterpretationQueue{
		col: client.Collection("horizon_interpretation_queue"),
	}
}

// Enqueue adds an interpretation request to the queue.
func (q *InterpretationQueue) Enqueue(ctx context.Context, request *InterpretationRequest) (string, error) {
	if request == nil {
		return "", nil
	}

	now := time.Now()
	if request.CreatedAt.IsZero() {
		request.CreatedAt = now
	}
	if request.Status == "" {
		request.Status = "pending"
	}

	doc := q.col.NewDoc()
	request.ID = doc.ID

	_, err := doc.Set(ctx, request)
	if err != nil {
		return "", err
	}

	log.Printf("%s enqueued id=%s fragmentId=%s polarity=%s priority=%s trigger=%s",
		signalsys.LogQueue,
		strings.TrimSpace(request.ID),
		strings.TrimSpace(request.FragmentID),
		strings.TrimSpace(request.Polarity),
		string(request.Priority),
		string(request.TriggerType),
	)
	return request.ID, nil
}

// Dequeue retrieves the next pending interpretation request (priority order).
func (q *InterpretationQueue) Dequeue(ctx context.Context, limit int) ([]*InterpretationRequest, error) {
	if limit <= 0 {
		limit = 10
	}

	// Query pending requests ordered by priority and createdAt
	qry := q.col.Where("status", "==", "pending").
		OrderBy("priority", firestore.Desc).
		OrderBy("createdAt", firestore.Asc).
		Limit(limit)

	docs, err := qry.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	requests := make([]*InterpretationRequest, 0, len(docs))
	for _, doc := range docs {
		var req InterpretationRequest
		if err := doc.DataTo(&req); err != nil {
			continue
		}
		if doc.Ref != nil {
			req.ID = doc.Ref.ID
		}
		requests = append(requests, &req)
	}

	if len(requests) > 0 {
		log.Printf("%s dequeued n=%d", signalsys.LogQueue, len(requests))
	}
	return requests, nil
}

// MarkProcessing marks a request as processing.
func (q *InterpretationQueue) MarkProcessing(ctx context.Context, requestID string) error {
	_, err := q.col.Doc(requestID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "processing"},
	})
	return err
}

// MarkCompleted marks a request as completed.
func (q *InterpretationQueue) MarkCompleted(ctx context.Context, requestID string) error {
	_, err := q.col.Doc(requestID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "completed"},
	})
	return err
}

// MarkFailed marks a request as failed.
func (q *InterpretationQueue) MarkFailed(ctx context.Context, requestID string, errorMsg string) error {
	_, err := q.col.Doc(requestID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "failed"},
		{Path: "error", Value: errorMsg},
	})
	return err
}

// HasPending checks whether a pending request already exists for the same fragment+polarity.
func (q *InterpretationQueue) HasPending(ctx context.Context, fragmentID, polarity string) (bool, error) {
	fragmentID = strings.TrimSpace(fragmentID)
	polarity = strings.TrimSpace(polarity)
	if fragmentID == "" || polarity == "" {
		return false, nil
	}

	qry := q.col.
		Where("fragmentId", "==", fragmentID).
		Where("polarity", "==", polarity).
		Where("status", "==", "pending").
		Limit(1)

	docs, err := qry.Documents(ctx).GetAll()
	if err != nil {
		return false, err
	}
	return len(docs) > 0, nil
}

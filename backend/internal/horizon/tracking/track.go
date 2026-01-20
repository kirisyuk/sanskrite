package tracking

import (
	"context"
	"sort"
	"strings"
	"sync"
)

type trackerKey struct{}

// Tracker collects created/modified entity IDs during a job.
type Tracker struct {
	mu               sync.Mutex
	createdFragments map[string]struct{}
	modifiedFragments map[string]struct{}
	createdEdges     map[string]struct{}
	modifiedEdges    map[string]struct{}
}

// NewTracker creates a new Tracker.
func NewTracker() *Tracker {
	return &Tracker{
		createdFragments:  map[string]struct{}{},
		modifiedFragments: map[string]struct{}{},
		createdEdges:      map[string]struct{}{},
		modifiedEdges:     map[string]struct{}{},
	}
}

// WithTracker attaches a tracker to context.
func WithTracker(ctx context.Context, tracker *Tracker) context.Context {
	if tracker == nil {
		return ctx
	}
	return context.WithValue(ctx, trackerKey{}, tracker)
}

// FromContext retrieves a tracker from context.
func FromContext(ctx context.Context) *Tracker {
	if ctx == nil {
		return nil
	}
	if tracker, ok := ctx.Value(trackerKey{}).(*Tracker); ok {
		return tracker
	}
	return nil
}

// RecordFragmentCreated records a newly created fragment ID.
func RecordFragmentCreated(ctx context.Context, id string) {
	if tracker := FromContext(ctx); tracker != nil {
		tracker.RecordFragmentCreated(id)
	}
}

// RecordFragmentUpdated records a modified fragment ID.
func RecordFragmentUpdated(ctx context.Context, id string) {
	if tracker := FromContext(ctx); tracker != nil {
		tracker.RecordFragmentUpdated(id)
	}
}

// RecordEdgeCreated records a newly created edge ID.
func RecordEdgeCreated(ctx context.Context, id string) {
	if tracker := FromContext(ctx); tracker != nil {
		tracker.RecordEdgeCreated(id)
	}
}

// RecordEdgeUpdated records a modified edge ID.
func RecordEdgeUpdated(ctx context.Context, id string) {
	if tracker := FromContext(ctx); tracker != nil {
		tracker.RecordEdgeUpdated(id)
	}
}

// ApplyToState merges tracked IDs into job state.
func (t *Tracker) ApplyToState(state map[string]interface{}) map[string]interface{} {
	if t == nil {
		return state
	}
	if state == nil {
		state = map[string]interface{}{}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state["createdFragmentIds"] = mergeIDSlice(state["createdFragmentIds"], t.createdFragments)
	state["modifiedFragmentIds"] = mergeIDSlice(state["modifiedFragmentIds"], t.modifiedFragments)
	state["createdEdgeIds"] = mergeIDSlice(state["createdEdgeIds"], t.createdEdges)
	state["modifiedEdgeIds"] = mergeIDSlice(state["modifiedEdgeIds"], t.modifiedEdges)
	return state
}

// RecordFragmentCreated records a created fragment ID on the tracker.
func (t *Tracker) RecordFragmentCreated(id string) {
	t.recordCreated(id, t.createdFragments, t.modifiedFragments)
}

// RecordFragmentUpdated records a modified fragment ID on the tracker.
func (t *Tracker) RecordFragmentUpdated(id string) {
	t.recordModified(id, t.createdFragments, t.modifiedFragments)
}

// RecordEdgeCreated records a created edge ID on the tracker.
func (t *Tracker) RecordEdgeCreated(id string) {
	t.recordCreated(id, t.createdEdges, t.modifiedEdges)
}

// RecordEdgeUpdated records a modified edge ID on the tracker.
func (t *Tracker) RecordEdgeUpdated(id string) {
	t.recordModified(id, t.createdEdges, t.modifiedEdges)
}

func (t *Tracker) recordCreated(id string, created map[string]struct{}, modified map[string]struct{}) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	created[id] = struct{}{}
	delete(modified, id)
}

func (t *Tracker) recordModified(id string, created map[string]struct{}, modified map[string]struct{}) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := created[id]; exists {
		return
	}
	modified[id] = struct{}{}
}

func mergeIDSlice(existing interface{}, added map[string]struct{}) []string {
	set := map[string]struct{}{}
	for _, item := range readStringSlice(existing) {
		item = strings.TrimSpace(item)
		if item != "" {
			set[item] = struct{}{}
		}
	}
	for id := range added {
		id = strings.TrimSpace(id)
		if id != "" {
			set[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func readStringSlice(raw interface{}) []string {
	switch t := raw.(type) {
	case []string:
		return t
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

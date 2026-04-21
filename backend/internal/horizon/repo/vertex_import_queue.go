package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

const (
	DefaultVertexImportIntentTopic        = "horizon.vertex.imports"
	DefaultVertexImportIntentSubscription = "horizon.vertex.imports.worker"
	DefaultVertexImportIntentDLQTopic     = "horizon.vertex.imports.dlq"
)

type VertexImportIntent struct {
	TenantID           string `json:"tenantId,omitempty"`
	Source             string `json:"source,omitempty"`
	URI                string `json:"uri"`
	DataStoreID        string `json:"dataStoreId"`
	DataSchema         string `json:"dataSchema"`
	ReconciliationMode string `json:"reconciliationMode"`
	EnqueuedAt         string `json:"enqueuedAt,omitempty"`
}

func (i VertexImportIntent) Normalized() VertexImportIntent {
	i.TenantID = strings.TrimSpace(i.TenantID)
	i.Source = strings.TrimSpace(i.Source)
	i.URI = strings.TrimSpace(i.URI)
	i.DataStoreID = strings.TrimSpace(i.DataStoreID)
	i.DataSchema = strings.TrimSpace(i.DataSchema)
	if i.DataSchema == "" {
		i.DataSchema = "document"
	}
	i.ReconciliationMode = strings.TrimSpace(i.ReconciliationMode)
	if i.ReconciliationMode == "" {
		i.ReconciliationMode = "INCREMENTAL"
	}
	if strings.TrimSpace(i.EnqueuedAt) == "" {
		i.EnqueuedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return i
}

func (i VertexImportIntent) Valid() bool {
	return strings.TrimSpace(i.URI) != "" && strings.TrimSpace(i.DataStoreID) != ""
}

func (i VertexImportIntent) RouteKey() string {
	return fmt.Sprintf("%s|%s|%s", strings.TrimSpace(i.DataStoreID), strings.TrimSpace(i.DataSchema), strings.TrimSpace(i.ReconciliationMode))
}

type VertexImportPublisher struct {
	topic  *pubsub.Topic
	logger *log.Logger
}

func NewVertexImportPublisher(ctx context.Context, client *pubsub.Client, topicName string, logger *log.Logger) (*VertexImportPublisher, error) {
	if client == nil {
		return nil, fmt.Errorf("pubsub client is nil")
	}
	topicName = strings.TrimSpace(topicName)
	if topicName == "" {
		topicName = DefaultVertexImportIntentTopic
	}
	topic := client.Topic(topicName)
	exists, err := topic.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("check topic %s: %w", topicName, err)
	}
	if !exists {
		topic, err = client.CreateTopic(ctx, topicName)
		if err != nil {
			return nil, fmt.Errorf("create topic %s: %w", topicName, err)
		}
	}
	return &VertexImportPublisher{topic: topic, logger: logger}, nil
}

func (p *VertexImportPublisher) PublishIntent(ctx context.Context, intent VertexImportIntent) error {
	if p == nil || p.topic == nil {
		return fmt.Errorf("vertex import publisher is not configured")
	}
	intent = intent.Normalized()
	if !intent.Valid() {
		return fmt.Errorf("invalid vertex import intent")
	}
	body, err := json.Marshal(intent)
	if err != nil {
		return fmt.Errorf("marshal import intent: %w", err)
	}
	msg := &pubsub.Message{
		Data: body,
		Attributes: map[string]string{
			"tenantId":           intent.TenantID,
			"source":             intent.Source,
			"uri":                intent.URI,
			"dataStoreId":        intent.DataStoreID,
			"dataSchema":         intent.DataSchema,
			"reconciliationMode": intent.ReconciliationMode,
		},
	}
	res := p.topic.Publish(ctx, msg)
	if _, err := res.Get(ctx); err != nil {
		return fmt.Errorf("publish import intent: %w", err)
	}
	if p.logger != nil {
		p.logger.Printf("[VertexImportPublisher] queued source=%s store=%s schema=%s uri=%s",
			intent.Source, intent.DataStoreID, intent.DataSchema, intent.URI)
	}
	return nil
}

type VertexImportBudgetConfig struct {
	BaseImportsPerMinute int
	MaxImportsPerMinute  int
	BaseInflightImports  int
	MaxInflightImports   int
	TargetPendingLRO     int
	Adaptive             bool
	OperationTimeout     time.Duration
	LogInterval          time.Duration
}

func DefaultVertexImportBudgetConfig() VertexImportBudgetConfig {
	return VertexImportBudgetConfig{
		BaseImportsPerMinute: 6,
		MaxImportsPerMinute:  12,
		BaseInflightImports:  2,
		MaxInflightImports:   6,
		TargetPendingLRO:     3,
		Adaptive:             true,
		OperationTimeout:     45 * time.Minute,
		LogInterval:          1 * time.Minute,
	}
}

type VertexImportBudgetStats struct {
	CurrentImportsPerMinute float64
	CurrentInflightCap      int
	InflightOperations      int
	TargetPendingLRO        int
	TotalAccepted           int
	TotalCompleted          int
	TotalRejected           int
	TotalResourceExhausted  int
	AverageOperationLatency time.Duration
	LastError               string
}

type VertexOperationChecker func(ctx context.Context, operation string) (*ai.OperationStatus, error)
type VertexImportObjectExists func(ctx context.Context, uri string) (bool, error)

type VertexImportBudget struct {
	mu sync.Mutex

	config             VertexImportBudgetConfig
	currentRate        float64
	currentInflightCap int
	inflight           int
	nextImportAllowed  time.Time
	successStreak      int
	lastError          string
	totalAccepted      int
	totalCompleted     int
	totalRejected      int
	total429           int
	avgLatency         time.Duration
}

func NewVertexImportBudget(config VertexImportBudgetConfig) *VertexImportBudget {
	if config.BaseImportsPerMinute <= 0 {
		config.BaseImportsPerMinute = 1
	}
	if config.MaxImportsPerMinute < config.BaseImportsPerMinute {
		config.MaxImportsPerMinute = config.BaseImportsPerMinute
	}
	if config.BaseInflightImports <= 0 {
		config.BaseInflightImports = 1
	}
	if config.MaxInflightImports < config.BaseInflightImports {
		config.MaxInflightImports = config.BaseInflightImports
	}
	if config.TargetPendingLRO <= 0 {
		config.TargetPendingLRO = config.BaseInflightImports
	}
	if config.OperationTimeout <= 0 {
		config.OperationTimeout = 45 * time.Minute
	}
	if config.LogInterval <= 0 {
		config.LogInterval = time.Minute
	}
	return &VertexImportBudget{
		config:             config,
		currentRate:        float64(config.BaseImportsPerMinute),
		currentInflightCap: config.BaseInflightImports,
	}
}

func (b *VertexImportBudget) AcquireImportPermit(ctx context.Context, routeKey string) (*VertexImportPermit, error) {
	if b == nil {
		return &VertexImportPermit{}, nil
	}
	for {
		waitFor := time.Duration(0)
		b.mu.Lock()
		now := time.Now()
		effectiveCap := b.currentInflightCap
		if b.config.TargetPendingLRO > 0 && b.config.TargetPendingLRO < effectiveCap {
			effectiveCap = b.config.TargetPendingLRO
		}
		if effectiveCap <= 0 {
			effectiveCap = 1
		}
		switch {
		case b.inflight >= effectiveCap:
			waitFor = 2 * time.Second
		case !b.nextImportAllowed.IsZero() && now.Before(b.nextImportAllowed):
			waitFor = b.nextImportAllowed.Sub(now)
		default:
			interval := time.Minute / time.Duration(math.Max(b.currentRate, 1))
			if interval < 500*time.Millisecond {
				interval = 500 * time.Millisecond
			}
			b.inflight++
			b.nextImportAllowed = now.Add(interval)
			b.mu.Unlock()
			return &VertexImportPermit{
				budget:    b,
				routeKey:  strings.TrimSpace(routeKey),
				acquiredAt: now,
			}, nil
		}
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitFor):
		}
	}
}

func (b *VertexImportBudget) StartLogging(ctx context.Context, label string, logger *log.Logger) {
	if b == nil || logger == nil {
		return
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "vertex"
	}
	go func() {
		ticker := time.NewTicker(b.config.LogInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats := b.Stats()
				logger.Printf("[VertexImportBudget] label=%s rate=%.2f inflight=%d inflightCap=%d targetPendingLRO=%d accepted=%d completed=%d rejected=%d resourceExhausted=%d avgLatency=%s lastError=%q",
					label,
					stats.CurrentImportsPerMinute,
					stats.InflightOperations,
					stats.CurrentInflightCap,
					stats.TargetPendingLRO,
					stats.TotalAccepted,
					stats.TotalCompleted,
					stats.TotalRejected,
					stats.TotalResourceExhausted,
					stats.AverageOperationLatency,
					stats.LastError,
				)
			}
		}
	}()
}

func (b *VertexImportBudget) Stats() VertexImportBudgetStats {
	if b == nil {
		return VertexImportBudgetStats{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return VertexImportBudgetStats{
		CurrentImportsPerMinute: b.currentRate,
		CurrentInflightCap:      b.currentInflightCap,
		InflightOperations:      b.inflight,
		TargetPendingLRO:        b.config.TargetPendingLRO,
		TotalAccepted:           b.totalAccepted,
		TotalCompleted:          b.totalCompleted,
		TotalRejected:           b.totalRejected,
		TotalResourceExhausted:  b.total429,
		AverageOperationLatency: b.avgLatency,
		LastError:               b.lastError,
	}
}

type VertexImportPermit struct {
	budget     *VertexImportBudget
	routeKey   string
	acquiredAt time.Time
	released   bool
}

func (p *VertexImportPermit) Reject(err error) {
	if p == nil || p.released || p.budget == nil {
		return
	}
	p.released = true
	b := p.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inflight > 0 {
		b.inflight--
	}
	b.totalRejected++
	b.successStreak = 0
	b.lastError = strings.TrimSpace(errString(err))
	if isRateLimit429(err) {
		b.total429++
		if b.currentRate > 1 {
			b.currentRate = math.Max(1, math.Floor(b.currentRate/2))
		}
		if b.currentInflightCap > 1 {
			b.currentInflightCap--
		}
		backoff := 15 * time.Second
		if b.nextImportAllowed.Before(time.Now().Add(backoff)) {
			b.nextImportAllowed = time.Now().Add(backoff)
		}
		return
	}
	if b.config.Adaptive && b.currentRate > 1 {
		b.currentRate = math.Max(1, b.currentRate-1)
	}
}

func (p *VertexImportPermit) Complete(err error, latency time.Duration) {
	if p == nil || p.released || p.budget == nil {
		return
	}
	p.released = true
	b := p.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inflight > 0 {
		b.inflight--
	}
	if err != nil {
		b.totalRejected++
		b.successStreak = 0
		b.lastError = strings.TrimSpace(errString(err))
		if isRateLimit429(err) {
			b.total429++
			if b.currentRate > 1 {
				b.currentRate = math.Max(1, math.Floor(b.currentRate/2))
			}
			if b.currentInflightCap > 1 {
				b.currentInflightCap--
			}
		}
		return
	}
	b.recordSuccessLocked(latency)
}

func (p *VertexImportPermit) Accept(operationName string, checker VertexOperationChecker) {
	if p == nil || p.released || p.budget == nil {
		return
	}
	p.released = true
	b := p.budget
	b.mu.Lock()
	b.totalAccepted++
	b.mu.Unlock()

	if strings.TrimSpace(operationName) == "" || checker == nil {
		b.mu.Lock()
		if b.inflight > 0 {
			b.inflight--
		}
		b.recordSuccessLocked(time.Since(p.acquiredAt))
		b.mu.Unlock()
		return
	}

	go func(op string, started time.Time) {
		timeout := b.config.OperationTimeout
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			status, err := checker(ctx, op)
			if err == nil && status != nil && status.Done {
				b.mu.Lock()
				if b.inflight > 0 {
					b.inflight--
				}
				if status.Error != nil && status.Error.Code != 0 {
					b.totalRejected++
					b.successStreak = 0
					b.lastError = fmt.Sprintf("op=%s code=%d message=%s", op, status.Error.Code, strings.TrimSpace(status.Error.Message))
					if status.Error.Code == 429 {
						b.total429++
						if b.currentRate > 1 {
							b.currentRate = math.Max(1, math.Floor(b.currentRate/2))
						}
						if b.currentInflightCap > 1 {
							b.currentInflightCap--
						}
					}
					b.mu.Unlock()
					return
				}
				b.recordSuccessLocked(time.Since(started))
				b.mu.Unlock()
				return
			}
			select {
			case <-ctx.Done():
				b.mu.Lock()
				if b.inflight > 0 {
					b.inflight--
				}
				b.totalRejected++
				b.successStreak = 0
				b.lastError = fmt.Sprintf("operation timeout: %s", op)
				b.mu.Unlock()
				return
			case <-ticker.C:
			}
		}
	}(strings.TrimSpace(operationName), p.acquiredAt)
}

func (b *VertexImportBudget) recordSuccessLocked(latency time.Duration) {
	b.totalCompleted++
	b.successStreak++
	b.lastError = ""
	if latency > 0 {
		if b.avgLatency <= 0 {
			b.avgLatency = latency
		} else {
			b.avgLatency = time.Duration((float64(b.avgLatency) * 0.8) + (float64(latency) * 0.2))
		}
	}
	if !b.config.Adaptive {
		return
	}
	if b.successStreak%5 == 0 && b.currentRate < float64(b.config.MaxImportsPerMinute) {
		b.currentRate++
	}
	if b.successStreak%8 == 0 && b.currentInflightCap < b.config.MaxInflightImports {
		b.currentInflightCap++
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type VertexImportWorker struct {
	sub         *pubsub.Subscription
	vertex      *ai.VertexAISearchClient
	budget      *VertexImportBudget
	batchConfig VertexSyncBatcherConfig
	logger      *log.Logger
	objectExists VertexImportObjectExists

	controllersMu sync.Mutex
	controllers   map[string]*vertexImportController
}

func NewVertexImportWorker(
	ctx context.Context,
	client *pubsub.Client,
	topicName string,
	subscriptionName string,
	vertex *ai.VertexAISearchClient,
	budget *VertexImportBudget,
	batchConfig VertexSyncBatcherConfig,
	logger *log.Logger,
	objectExists VertexImportObjectExists,
) (*VertexImportWorker, error) {
	if client == nil {
		return nil, fmt.Errorf("pubsub client is nil")
	}
	if vertex == nil {
		return nil, fmt.Errorf("vertex client is nil")
	}
	topicName = strings.TrimSpace(topicName)
	if topicName == "" {
		topicName = DefaultVertexImportIntentTopic
	}
	subscriptionName = strings.TrimSpace(subscriptionName)
	if subscriptionName == "" {
		subscriptionName = DefaultVertexImportIntentSubscription
	}
	topic := client.Topic(topicName)
	exists, err := topic.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("check topic %s: %w", topicName, err)
	}
	if !exists {
		topic, err = client.CreateTopic(ctx, topicName)
		if err != nil {
			return nil, fmt.Errorf("create topic %s: %w", topicName, err)
		}
	}
	sub := client.Subscription(subscriptionName)
	subExists, err := sub.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("check subscription %s: %w", subscriptionName, err)
	}
	if !subExists {
		sub, err = client.CreateSubscription(ctx, subscriptionName, pubsub.SubscriptionConfig{
			Topic:               topic,
			AckDeadline:         60 * time.Second,
			RetentionDuration:   7 * 24 * time.Hour,
			ExpirationPolicy:    24 * time.Hour,
			EnableMessageOrdering: false,
		})
		if err != nil {
			return nil, fmt.Errorf("create subscription %s: %w", subscriptionName, err)
		}
	}
	return &VertexImportWorker{
		sub:         sub,
		vertex:      vertex,
		budget:      budget,
		batchConfig: batchConfig,
		logger:      logger,
		objectExists: objectExists,
		controllers: map[string]*vertexImportController{},
	}, nil
}

func (w *VertexImportWorker) Start(ctx context.Context) error {
	if w == nil || w.sub == nil {
		return nil
	}
	w.sub.ReceiveSettings.Synchronous = false
	w.sub.ReceiveSettings.MaxOutstandingMessages = 256
	w.sub.ReceiveSettings.MaxOutstandingBytes = 32 << 20
	return w.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		var intent VertexImportIntent
		if err := json.Unmarshal(m.Data, &intent); err != nil {
			if w.logger != nil {
				w.logger.Printf("[VertexImportWorker] bad payload: %v", err)
			}
			m.Ack()
			return
		}
		intent = intent.Normalized()
		if !intent.Valid() {
			m.Ack()
			return
		}
		controller := w.controllerFor(intent)
		if err := controller.handle(ctx, m, intent); err != nil {
			m.Nack()
			return
		}
		m.Ack()
	})
}

func (w *VertexImportWorker) controllerFor(intent VertexImportIntent) *vertexImportController {
	key := intent.RouteKey()
	w.controllersMu.Lock()
	defer w.controllersMu.Unlock()
	if c, ok := w.controllers[key]; ok {
		return c
	}
	c := &vertexImportController{
		worker:        w,
		key:           key,
		dataStoreID:   intent.DataStoreID,
		dataSchema:    intent.DataSchema,
		reconcileMode: intent.ReconciliationMode,
		pending:       map[string]*vertexImportPending{},
		order:         make([]string, 0, w.batchConfig.MaxBatchSize),
	}
	w.controllers[key] = c
	return c
}

type vertexImportTask struct {
	result chan error
}

type vertexImportPending struct {
	intent    VertexImportIntent
	waiters   []*vertexImportTask
	firstSeen time.Time
}

type vertexImportController struct {
	worker        *VertexImportWorker
	key           string
	dataStoreID   string
	dataSchema    string
	reconcileMode string

	mu         sync.Mutex
	pending    map[string]*vertexImportPending
	order      []string
	flushTimer *time.Timer
	flushing   bool
}

func (c *vertexImportController) handle(ctx context.Context, _ *pubsub.Message, intent VertexImportIntent) error {
	task := &vertexImportTask{result: make(chan error, 1)}
	c.enqueue(intent, task)
	select {
	case err := <-task.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *vertexImportController) enqueue(intent VertexImportIntent, task *vertexImportTask) {
	intent = intent.Normalized()
	c.mu.Lock()
	if existing, ok := c.pending[intent.URI]; ok {
		existing.waiters = append(existing.waiters, task)
		c.mu.Unlock()
		return
	}
	c.pending[intent.URI] = &vertexImportPending{
		intent:    intent,
		waiters:   []*vertexImportTask{task},
		firstSeen: time.Now(),
	}
	c.order = append(c.order, intent.URI)
	shouldFlush := len(c.order) >= c.worker.batchConfig.MaxBatchSize
	if c.flushTimer == nil {
		c.flushTimer = time.AfterFunc(c.worker.batchConfig.FlushInterval, func() {
			c.flush(true)
		})
	}
	c.mu.Unlock()
	if shouldFlush {
		go c.flush(false)
	}
}

func (c *vertexImportController) flush(force bool) {
	c.mu.Lock()
	if c.flushing {
		c.mu.Unlock()
		return
	}
	if len(c.order) == 0 {
		c.stopTimerLocked()
		c.mu.Unlock()
		return
	}
	if !force && len(c.order) < c.worker.batchConfig.MinBatchSize {
		c.mu.Unlock()
		return
	}
	c.flushing = true
	c.stopTimerLocked()
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.flushing = false
		if len(c.order) > 0 && c.flushTimer == nil {
			c.flushTimer = time.AfterFunc(c.worker.batchConfig.FlushInterval, func() {
				c.flush(true)
			})
		}
		c.mu.Unlock()
	}()

	for {
		entries := c.takeBatch()
		if len(entries) == 0 {
			return
		}
		batch, waiters, skippedWaiters, err := c.filterExistingEntries(entries)
		if err != nil {
			notifyWaiters(vertexImportEntriesWaiters(entries), err)
			return
		}
		if len(skippedWaiters) > 0 {
			if c.worker.logger != nil {
				c.worker.logger.Printf("[VertexImportWorker] skipped stale URIs route=%s count=%d", c.key, len(skippedWaiters))
			}
			notifyWaiters(skippedWaiters, nil)
		}
		if len(batch) == 0 {
			continue
		}
		permit, err := c.worker.budget.AcquireImportPermit(context.Background(), c.key)
		if err != nil {
			notifyWaiters(waiters, err)
			return
		}
		importCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		startedAt := time.Now()
		opName, importErr := c.worker.vertex.WithDataStore(c.dataStoreID).ImportDocumentsFromURIs(importCtx, batch, c.reconcileMode, c.dataSchema)
		cancel()
		if importErr != nil {
			permit.Reject(importErr)
			if c.worker.logger != nil {
				c.worker.logger.Printf("[VertexImportWorker] import failed route=%s count=%d err=%v", c.key, len(batch), importErr)
			}
			notifyWaiters(waiters, importErr)
			return
		}
		permit.Accept(opName, func(ctx context.Context, operation string) (*ai.OperationStatus, error) {
			return c.worker.vertex.WithDataStore(c.dataStoreID).CheckOperationStatus(ctx, operation)
		})
		if c.worker.logger != nil {
			oldestAge := time.Since(startedAt)
			c.worker.logger.Printf("[VertexImportWorker] import accepted route=%s count=%d operation=%s queueLatency=%s",
				c.key, len(batch), opName, oldestAge)
		}
		notifyWaiters(waiters, nil)
	}
}

func (c *vertexImportController) takeBatch() []*vertexImportPending {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.order) == 0 {
		return nil
	}
	n := c.worker.batchConfig.MaxBatchSize
	if n <= 0 || n > len(c.order) {
		n = len(c.order)
	}
	entries := make([]*vertexImportPending, 0, n)
	remaining := make([]string, 0, len(c.order)-n)
	for idx, uri := range c.order {
		if idx < n {
			entry := c.pending[uri]
			delete(c.pending, uri)
			if entry == nil {
				continue
			}
			entries = append(entries, entry)
			continue
		}
		remaining = append(remaining, uri)
	}
	c.order = remaining
	return entries
}

func (c *vertexImportController) filterExistingEntries(entries []*vertexImportPending) ([]string, []*vertexImportTask, []*vertexImportTask, error) {
	if len(entries) == 0 {
		return nil, nil, nil, nil
	}
	if c.worker == nil || c.worker.objectExists == nil {
		return vertexImportEntriesURIs(entries), vertexImportEntriesWaiters(entries), nil, nil
	}

	checkCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	uris := make([]string, 0, len(entries))
	waiters := make([]*vertexImportTask, 0, len(entries))
	skippedWaiters := make([]*vertexImportTask, 0)
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		exists, err := c.worker.objectExists(checkCtx, entry.intent.URI)
		if err != nil {
			return nil, nil, nil, err
		}
		if !exists {
			skippedWaiters = append(skippedWaiters, entry.waiters...)
			continue
		}
		uris = append(uris, entry.intent.URI)
		waiters = append(waiters, entry.waiters...)
	}
	return uris, waiters, skippedWaiters, nil
}

func vertexImportEntriesURIs(entries []*vertexImportPending) []string {
	uris := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		uris = append(uris, entry.intent.URI)
	}
	return uris
}

func vertexImportEntriesWaiters(entries []*vertexImportPending) []*vertexImportTask {
	waiters := make([]*vertexImportTask, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		waiters = append(waiters, entry.waiters...)
	}
	return waiters
}

func (c *vertexImportController) stopTimerLocked() {
	if c.flushTimer != nil {
		c.flushTimer.Stop()
		c.flushTimer = nil
	}
}

func notifyWaiters(waiters []*vertexImportTask, err error) {
	for _, waiter := range waiters {
		if waiter == nil {
			continue
		}
		waiter.result <- err
		close(waiter.result)
	}
}

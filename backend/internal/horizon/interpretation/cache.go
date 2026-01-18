package interpretation

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// InterpretationCache provides TTL-based caching for interpretations.
type InterpretationCache struct {
	col *firestore.CollectionRef
}

// NewInterpretationCache creates a new InterpretationCache.
func NewInterpretationCache(client *firestore.Client) *InterpretationCache {
	return &InterpretationCache{
		col: client.Collection("horizon_interpretations"),
	}
}

// Get retrieves a cached interpretation.
// Returns nil if not found or expired.
func (c *InterpretationCache) Get(ctx context.Context, nodeID, polarity string) (*RiskInterpretation, error) {
	docID := c.buildCacheKey(nodeID, polarity)
	snap, err := c.col.Doc(docID).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		log.Printf("%s miss key=%s", signalsys.LogCache, strings.TrimSpace(docID))
		return nil, nil
	}

	var interpretation RiskInterpretation
	if err := snap.DataTo(&interpretation); err != nil {
		return nil, err
	}

	// Check if expired
	if time.Now().After(interpretation.ExpiresAt) {
		// Delete expired entry
		_, _ = c.col.Doc(docID).Delete(ctx)
		log.Printf("%s expired key=%s", signalsys.LogCache, strings.TrimSpace(docID))
		return nil, nil
	}

	log.Printf("%s hit key=%s", signalsys.LogCache, strings.TrimSpace(docID))
	return &interpretation, nil
}

// Set stores an interpretation in cache.
func (c *InterpretationCache) Set(ctx context.Context, interpretation *RiskInterpretation) error {
	if interpretation == nil {
		return fmt.Errorf("interpretation is nil")
	}

	docID := c.buildCacheKey(interpretation.NodeID, interpretation.Polarity)
	_, err := c.col.Doc(docID).Set(ctx, interpretation)
	if err == nil {
		log.Printf("%s set key=%s ttl=%s", signalsys.LogCache, strings.TrimSpace(docID), time.Until(interpretation.ExpiresAt).String())
	}
	return err
}

// Invalidate invalidates cached interpretation for a node.
func (c *InterpretationCache) Invalidate(ctx context.Context, nodeID string) error {
	// Invalidate both risk and opportunity
	riskKey := c.buildCacheKey(nodeID, "risk")
	oppKey := c.buildCacheKey(nodeID, "opportunity")

	_, err1 := c.col.Doc(riskKey).Delete(ctx)
	_, err2 := c.col.Doc(oppKey).Delete(ctx)

	if err1 != nil {
		return err1
	}
	return err2
}

// buildCacheKey builds a cache key from nodeID and polarity.
func (c *InterpretationCache) buildCacheKey(nodeID, polarity string) string {
	return fmt.Sprintf("%s:%s", nodeID, polarity)
}

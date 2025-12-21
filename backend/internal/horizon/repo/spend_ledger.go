package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// HorizonSpendLedger persists actual (observed) GenAI spend attribution for Horizon.
// It is updated incrementally from AITaskExecutor using the real per-call costUSD.
//
// Collections:
// - horizon_lens_spend: per lens per UTC day
// - horizon_cascade_spend: per cascade per UTC day
type HorizonSpendLedger struct {
	lensCol   *firestore.CollectionRef
	cascadeCol *firestore.CollectionRef
}

func NewHorizonSpendLedger(db *firestore.Client) *HorizonSpendLedger {
	return &HorizonSpendLedger{
		lensCol:   db.Collection("horizon_lens_spend"),
		cascadeCol: db.Collection("horizon_cascade_spend"),
	}
}

type LensSpendDoc struct {
	ID        string    `firestore:"id"`
	TenantID  string    `firestore:"tenantId"`
	LensID    string    `firestore:"lensId"`
	DayUTC    string    `firestore:"dayUtc"` // YYYY-MM-DD
	SpendUSD  float64   `firestore:"spendUSD"`
	InputTokens  int    `firestore:"inputTokens"`
	OutputTokens int    `firestore:"outputTokens"`
	UpdatedAt time.Time `firestore:"updatedAt"`
	CreatedAt time.Time `firestore:"createdAt"`
}

type CascadeSpendDoc struct {
	ID        string    `firestore:"id"`
	TenantID  string    `firestore:"tenantId"`
	LensID    string    `firestore:"lensId"`
	CascadeID string    `firestore:"cascadeId"`
	DayUTC    string    `firestore:"dayUtc"` // YYYY-MM-DD
	SpendUSD  float64   `firestore:"spendUSD"`
	InputTokens  int    `firestore:"inputTokens"`
	OutputTokens int    `firestore:"outputTokens"`
	UpdatedAt time.Time `firestore:"updatedAt"`
	CreatedAt time.Time `firestore:"createdAt"`
}

func dayUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func (r *HorizonSpendLedger) AddHorizonSpend(ctx context.Context, tenantID, lensID, cascadeID string, amountUSD float64, inputTokens, outputTokens int) error {
	tenantID = strings.TrimSpace(tenantID)
	lensID = strings.TrimSpace(lensID)
	cascadeID = strings.TrimSpace(cascadeID)
	if tenantID == "" {
		return nil
	}
	now := time.Now().UTC()
	if amountUSD < 0 {
		amountUSD = 0
	}
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}

	// Per-lens daily spend (if lensID is present)
	if lensID != "" {
		docID := fmt.Sprintf("%s__%s__%s", tenantID, lensID, dayUTC(now))
		_, err := r.lensCol.Doc(docID).Set(ctx, map[string]interface{}{
			"id":        docID,
			"tenantId":  tenantID,
			"lensId":    lensID,
			"dayUtc":    dayUTC(now),
			"spendUSD":  firestore.Increment(amountUSD),
			"inputTokens":  firestore.Increment(inputTokens),
			"outputTokens": firestore.Increment(outputTokens),
			"updatedAt": now,
			"createdAt": now,
		}, firestore.MergeAll)
		if err != nil {
			return err
		}
	}

	// Per-cascade daily spend (if cascadeID is present)
	if cascadeID != "" {
		docID := fmt.Sprintf("%s__%s__%s", tenantID, cascadeID, dayUTC(now))
		_, err := r.cascadeCol.Doc(docID).Set(ctx, map[string]interface{}{
			"id":        docID,
			"tenantId":  tenantID,
			"lensId":    lensID,
			"cascadeId": cascadeID,
			"dayUtc":    dayUTC(now),
			"spendUSD":  firestore.Increment(amountUSD),
			"inputTokens":  firestore.Increment(inputTokens),
			"outputTokens": firestore.Increment(outputTokens),
			"updatedAt": now,
			"createdAt": now,
		}, firestore.MergeAll)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *HorizonSpendLedger) GetCascadeSpendUSD(ctx context.Context, tenantID, cascadeID string, day string) (float64, int, int, error) {
	tenantID = strings.TrimSpace(tenantID)
	cascadeID = strings.TrimSpace(cascadeID)
	day = strings.TrimSpace(day)
	if day == "" {
		day = dayUTC(time.Now())
	}
	if tenantID == "" || cascadeID == "" {
		return 0, 0, 0, nil
	}
	docID := fmt.Sprintf("%s__%s__%s", tenantID, cascadeID, day)
	snap, err := r.cascadeCol.Doc(docID).Get(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	var doc CascadeSpendDoc
	if err := snap.DataTo(&doc); err != nil {
		return 0, 0, 0, err
	}
	return doc.SpendUSD, doc.InputTokens, doc.OutputTokens, nil
}

func (r *HorizonSpendLedger) GetLensDaySpendUSD(ctx context.Context, tenantID, lensID string, day string) (float64, int, int, error) {
	tenantID = strings.TrimSpace(tenantID)
	lensID = strings.TrimSpace(lensID)
	day = strings.TrimSpace(day)
	if tenantID == "" || lensID == "" || day == "" {
		return 0, 0, 0, nil
	}
	docID := fmt.Sprintf("%s__%s__%s", tenantID, lensID, day)
	snap, err := r.lensCol.Doc(docID).Get(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	var doc LensSpendDoc
	if err := snap.DataTo(&doc); err != nil {
		return 0, 0, 0, err
	}
	return doc.SpendUSD, doc.InputTokens, doc.OutputTokens, nil
}



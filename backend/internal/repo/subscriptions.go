package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

var ErrSubscriptionNotFound = errors.New("subscription not found")

type SubscriptionsRepository struct {
	col *firestore.CollectionRef
}

func NewSubscriptionsRepository(db *firestore.Client) *SubscriptionsRepository {
	return &SubscriptionsRepository{col: db.Collection("subscriptions")}
}

// Upsert creates or updates a subscription
func (r *SubscriptionsRepository) Upsert(ctx context.Context, sub *models.Subscription) error {
	if sub == nil || sub.UserID == "" || sub.TenantID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = now
	}
	sub.UpdatedAt = now
	// Use composite key: tenantId_userId
	docID := sub.TenantID + "_" + sub.UserID
	sub.ID = docID
	_, err := r.col.Doc(docID).Set(ctx, sub)
	return err
}

// Get retrieves a subscription by tenant and user ID
func (r *SubscriptionsRepository) Get(ctx context.Context, tenantID, userID string) (*models.Subscription, error) {
	docID := tenantID + "_" + userID
	snap, err := r.col.Doc(docID).Get(ctx)
	if err != nil {
		// Check if error is NotFound from Firestore
		if status.Code(err) == codes.NotFound {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrSubscriptionNotFound
	}
	var sub models.Subscription
	if err := snap.DataTo(&sub); err != nil {
		return nil, err
	}
	return &sub, nil
}

// ListActive returns all active subscriptions (for billing processing)
func (r *SubscriptionsRepository) ListActive(ctx context.Context) ([]*models.Subscription, error) {
	q := r.col.Where("status", "==", "active")
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var subs []*models.Subscription
	for _, d := range docs {
		var sub models.Subscription
		if err := d.DataTo(&sub); err != nil {
			continue
		}
		subs = append(subs, &sub)
	}
	return subs, nil
}

// ListExpiringSoon returns subscriptions expiring within the specified duration
func (r *SubscriptionsRepository) ListExpiringSoon(ctx context.Context, within time.Duration) ([]*models.Subscription, error) {
	threshold := time.Now().UTC().Add(within)
	q := r.col.Where("status", "==", "active").
		Where("currentPeriodEnd", "<=", threshold).
		OrderBy("currentPeriodEnd", firestore.Asc)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var subs []*models.Subscription
	for _, d := range docs {
		var sub models.Subscription
		if err := d.DataTo(&sub); err != nil {
			continue
		}
		subs = append(subs, &sub)
	}
	return subs, nil
}

// UpdateStatus updates subscription status
func (r *SubscriptionsRepository) UpdateStatus(ctx context.Context, tenantID, userID, status string) error {
	docID := tenantID + "_" + userID
	_, err := r.col.Doc(docID).Update(ctx, []firestore.Update{
		{Path: "status", Value: status},
		{Path: "updatedAt", Value: time.Now().UTC()},
	})
	return err
}

// UpdateBillingPeriod updates billing period dates
func (r *SubscriptionsRepository) UpdateBillingPeriod(ctx context.Context, tenantID, userID string, periodStart, periodEnd time.Time, nextBilling *time.Time) error {
	docID := tenantID + "_" + userID
	updates := []firestore.Update{
		{Path: "currentPeriodStart", Value: periodStart},
		{Path: "currentPeriodEnd", Value: periodEnd},
		{Path: "updatedAt", Value: time.Now().UTC()},
	}
	if nextBilling != nil {
		updates = append(updates, firestore.Update{Path: "nextBillingDate", Value: nextBilling})
	} else {
		updates = append(updates, firestore.Update{Path: "nextBillingDate", Value: firestore.Delete})
	}
	_, err := r.col.Doc(docID).Update(ctx, updates)
	return err
}

// ListByPayPalAgreementID finds subscriptions by PayPal Agreement ID (legacy)
func (r *SubscriptionsRepository) ListByPayPalAgreementID(ctx context.Context, agreementID string) ([]*models.Subscription, error) {
	q := r.col.Where("paypalAgreementId", "==", agreementID)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var subs []*models.Subscription
	for _, d := range docs {
		var sub models.Subscription
		if err := d.DataTo(&sub); err != nil {
			continue
		}
		subs = append(subs, &sub)
	}
	return subs, nil
}

// ListByPayPalSubscriptionID finds subscriptions by PayPal Subscription ID
func (r *SubscriptionsRepository) ListByPayPalSubscriptionID(ctx context.Context, subscriptionID string) ([]*models.Subscription, error) {
	q := r.col.Where("paypalSubscriptionId", "==", subscriptionID)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var subs []*models.Subscription
	for _, d := range docs {
		var sub models.Subscription
		if err := d.DataTo(&sub); err != nil {
			continue
		}
		subs = append(subs, &sub)
	}
	return subs, nil
}


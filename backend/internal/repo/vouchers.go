package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

var ErrVoucherNotFound = errors.New("voucher not found")
var ErrVoucherExpired = errors.New("voucher expired")
var ErrVoucherExhausted = errors.New("voucher usage limit exceeded")
var ErrVoucherInactive = errors.New("voucher is not active")

type VouchersRepository struct {
	col *firestore.CollectionRef
}

func NewVouchersRepository(db *firestore.Client) *VouchersRepository {
	return &VouchersRepository{col: db.Collection("vouchers")}
}

// Create creates a new voucher
func (r *VouchersRepository) Create(ctx context.Context, voucher *models.Voucher) error {
	if voucher == nil || voucher.Code == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if voucher.CreatedAt.IsZero() {
		voucher.CreatedAt = now
	}
	voucher.UpdatedAt = now
	if voucher.ID == "" {
		doc := r.col.NewDoc()
		voucher.ID = doc.ID
	} else {
		// Check if code already exists
		existing, _ := r.GetByCode(ctx, voucher.Code)
		if existing != nil {
			return errors.New("voucher code already exists")
		}
	}
	_, err := r.col.Doc(voucher.ID).Set(ctx, voucher)
	return err
}

// Get retrieves a voucher by ID
func (r *VouchersRepository) Get(ctx context.Context, id string) (*models.Voucher, error) {
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrVoucherNotFound
	}
	var voucher models.Voucher
	if err := snap.DataTo(&voucher); err != nil {
		return nil, err
	}
	return &voucher, nil
}

// GetByCode retrieves a voucher by code
func (r *VouchersRepository) GetByCode(ctx context.Context, code string) (*models.Voucher, error) {
	q := r.col.Where("code", "==", code).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrVoucherNotFound
	}
	var voucher models.Voucher
	if err := docs[0].DataTo(&voucher); err != nil {
		return nil, err
	}
	return &voucher, nil
}

// ValidateAndUse validates a voucher and increments usage count
func (r *VouchersRepository) ValidateAndUse(ctx context.Context, code string) (*models.Voucher, error) {
	voucher, err := r.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// Check if voucher is active
	if !voucher.IsActive {
		return nil, ErrVoucherInactive
	}

	// Check validity period
	if now.Before(voucher.ValidFrom) {
		return nil, errors.New("voucher not yet valid")
	}
	if voucher.ValidTo != nil && now.After(*voucher.ValidTo) {
		return nil, ErrVoucherExpired
	}

	// Check usage limit
	if voucher.MaxUses != nil && voucher.UsedCount >= *voucher.MaxUses {
		return nil, ErrVoucherExhausted
	}

	// Increment usage count
	voucher.UsedCount++
	voucher.UpdatedAt = now
	_, err = r.col.Doc(voucher.ID).Set(ctx, voucher)
	if err != nil {
		return nil, err
	}

	return voucher, nil
}

// List returns all vouchers (optionally filtered by tier and active status)
// activeOnly: nil = show all, true = active only, false = inactive only
func (r *VouchersRepository) List(ctx context.Context, tier *models.SubscriptionTier, activeOnly *bool) ([]*models.Voucher, error) {
	q := r.col.Query
	if tier != nil {
		q = q.Where("tier", "==", string(*tier))
	}
	if activeOnly != nil {
		q = q.Where("isActive", "==", *activeOnly)
	}
	q = q.OrderBy("createdAt", firestore.Desc).Limit(500)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	var vouchers []*models.Voucher
	for _, d := range docs {
		var v models.Voucher
		if err := d.DataTo(&v); err != nil {
			continue
		}
		vouchers = append(vouchers, &v)
	}
	return vouchers, nil
}

// Update updates a voucher
func (r *VouchersRepository) Update(ctx context.Context, voucher *models.Voucher) error {
	if voucher == nil || voucher.ID == "" {
		return ErrInvalidDocument
	}
	voucher.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(voucher.ID).Set(ctx, voucher)
	return err
}

// Delete deletes a voucher
func (r *VouchersRepository) Delete(ctx context.Context, id string) error {
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}


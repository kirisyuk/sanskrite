package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// SubscriptionService handles subscription business logic
type SubscriptionService struct {
	subsRepo   *repo.SubscriptionsRepository
	vouchersRepo *repo.VouchersRepository
	usersRepo  *repo.UsersRepository
	logger     *log.Logger
}

// NewSubscriptionService creates a new subscription service
func NewSubscriptionService(
	subsRepo *repo.SubscriptionsRepository,
	vouchersRepo *repo.VouchersRepository,
	usersRepo *repo.UsersRepository,
	logger *log.Logger,
) *SubscriptionService {
	return &SubscriptionService{
		subsRepo:     subsRepo,
		vouchersRepo: vouchersRepo,
		usersRepo:    usersRepo,
		logger:       logger,
	}
}

// GetOrCreateSubscription gets existing subscription or creates a free one (or Max for admins)
func (s *SubscriptionService) GetOrCreateSubscription(ctx context.Context, tenantID, userID string) (*models.Subscription, error) {
	// Try to get existing subscription
	sub, err := s.subsRepo.Get(ctx, tenantID, userID)
	if err == nil {
		return sub, nil
	}
	if err != repo.ErrSubscriptionNotFound {
		return nil, err
	}

	// Check if user is admin - admins get Max tier automatically
	isAdmin := false
	if user, err := s.usersRepo.GetByIDAndTenant(ctx, userID, tenantID); err == nil && user != nil && user.Role != nil {
		isAdmin = strings.ToLower(*user.Role) == "admin"
	}

	// Create subscription for new user
	now := time.Now().UTC()
	// Set period end far in the future (effectively never expires)
	periodEnd := now.AddDate(100, 0, 0) // 100 years in the future

	var tier models.SubscriptionTier
	if isAdmin {
		tier = models.SubscriptionTierMax
	} else {
		tier = models.SubscriptionTierFree
	}

	sub = &models.Subscription{
		TenantID:          tenantID,
		UserID:            userID,
		Tier:              tier,
		Status:             "active",
		StartedAt:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   periodEnd,
		NextBillingDate:   nil, // No billing for free or admin Max tier
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.subsRepo.Upsert(ctx, sub); err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	if s.logger != nil {
		if isAdmin {
			s.logger.Printf("Created Max subscription for admin user %s in tenant %s", userID, tenantID)
		} else {
			s.logger.Printf("Created free subscription for user %s in tenant %s", userID, tenantID)
		}
	}

	return sub, nil
}

// ApplyVoucher applies a voucher code to a user's subscription
func (s *SubscriptionService) ApplyVoucher(ctx context.Context, tenantID, userID, voucherCode string) (*models.Subscription, error) {
	// Validate and use voucher
	voucher, err := s.vouchersRepo.ValidateAndUse(ctx, voucherCode)
	if err != nil {
		return nil, fmt.Errorf("invalid voucher: %w", err)
	}

	// Get or create subscription
	sub, err := s.GetOrCreateSubscription(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// Calculate new period end based on voucher duration
	var periodStart time.Time
	var periodEnd time.Time
	var nextBilling *time.Time

	if sub.Status == "active" && sub.CurrentPeriodEnd.After(now) {
		// Extend existing subscription from current period end
		periodStart = sub.CurrentPeriodEnd
		periodEnd = sub.CurrentPeriodEnd.AddDate(0, voucher.DurationMonths, 0)
	} else {
		// Start new period from now
		periodStart = now
		periodEnd = now.AddDate(0, voucher.DurationMonths, 0)
	}

	// Set next billing date (only for paid tiers)
	if voucher.Tier != models.SubscriptionTierFree {
		nextBilling = &periodEnd
	}

	// Update subscription
	sub.Tier = voucher.Tier
	sub.Status = "active"
	sub.CurrentPeriodStart = periodStart
	sub.CurrentPeriodEnd = periodEnd
	sub.NextBillingDate = nextBilling
	sub.VoucherID = &voucher.ID
	sub.UpdatedAt = now

	if err := s.subsRepo.Upsert(ctx, sub); err != nil {
		return nil, fmt.Errorf("failed to update subscription: %w", err)
	}

	if s.logger != nil {
		s.logger.Printf("Applied voucher %s (tier: %s, duration: %d months) to user %s in tenant %s",
			voucherCode, voucher.Tier, voucher.DurationMonths, userID, tenantID)
	}

	return sub, nil
}

// CheckSubscription checks if user has active subscription of required tier
// Returns subscription and whether it's valid
func (s *SubscriptionService) CheckSubscription(ctx context.Context, tenantID, userID string, requiredTier models.SubscriptionTier) (*models.Subscription, bool, error) {
	// Check if user is admin (admins bypass subscription checks)
	user, err := s.usersRepo.GetByIDAndTenant(ctx, userID, tenantID)
	if err == nil && user != nil && user.Role != nil && *user.Role == "admin" {
		// Admin bypass - return a mock subscription
		now := time.Now().UTC()
		adminSub := &models.Subscription{
			TenantID:          tenantID,
			UserID:            userID,
			Tier:              models.SubscriptionTierMax, // Admins get max tier
			Status:             "active",
			CurrentPeriodStart: now,
			CurrentPeriodEnd:   now.AddDate(100, 0, 0),
		}
		return adminSub, true, nil
	}

	// Get subscription
	sub, err := s.GetOrCreateSubscription(ctx, tenantID, userID)
	if err != nil {
		return nil, false, err
	}

	// Check if subscription is active
	if sub.Status != "active" {
		return sub, false, nil
	}

	// Check if subscription period has expired
	now := time.Now().UTC()
	if now.After(sub.CurrentPeriodEnd) {
		// Expired - downgrade to free
		if err := s.downgradeToFree(ctx, tenantID, userID); err != nil {
			s.logger.Printf("Failed to downgrade expired subscription: %v", err)
		}
		return sub, false, nil
	}

	// Check tier hierarchy: free < pro < max
	tierLevel := map[models.SubscriptionTier]int{
		models.SubscriptionTierFree: 0,
		models.SubscriptionTierPro: 1,
		models.SubscriptionTierMax: 2,
	}

	userLevel := tierLevel[sub.Tier]
	requiredLevel := tierLevel[requiredTier]

	return sub, userLevel >= requiredLevel, nil
}

// downgradeToFree downgrades subscription to free tier
func (s *SubscriptionService) downgradeToFree(ctx context.Context, tenantID, userID string) error {
	now := time.Now().UTC()
	periodEnd := now.AddDate(100, 0, 0) // 100 years

	sub := &models.Subscription{
		TenantID:          tenantID,
		UserID:            userID,
		Tier:              models.SubscriptionTierFree,
		Status:             "active",
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   periodEnd,
		NextBillingDate:   nil,
		UpdatedAt:         now,
	}

	return s.subsRepo.Upsert(ctx, sub)
}

// ProcessBillingCycle processes monthly billing cycle
// This should be called periodically (e.g., daily via cron) to update subscriptions
func (s *SubscriptionService) ProcessBillingCycle(ctx context.Context) error {
	now := time.Now().UTC()

	// Find subscriptions that need billing processing
	// This includes:
	// 1. Subscriptions where currentPeriodEnd has passed (expired)
	// 2. Subscriptions where nextBillingDate is today or passed

	activeSubs, err := s.subsRepo.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("failed to list active subscriptions: %w", err)
	}

	processed := 0
	expired := 0

	for _, sub := range activeSubs {
		// Check if subscription period has expired
		if now.After(sub.CurrentPeriodEnd) {
			// Expired subscription - downgrade to free
			if err := s.downgradeToFree(ctx, sub.TenantID, sub.UserID); err != nil {
				if s.logger != nil {
					s.logger.Printf("Failed to downgrade expired subscription %s: %v", sub.ID, err)
				}
				continue
			}
			expired++
			if s.logger != nil {
				s.logger.Printf("Downgraded expired subscription for user %s in tenant %s", sub.UserID, sub.TenantID)
			}
			continue
		}

		// Check if billing date has arrived (for paid tiers)
		if sub.NextBillingDate != nil && !now.Before(*sub.NextBillingDate) {
			// Renew billing period (for paid subscriptions)
			// For now, if no payment is set up, we extend by another month
			// In production, this would trigger payment processing
			if sub.PaymentID == nil {
				// No payment set up - extend period by 1 month (grace period)
				newPeriodStart := sub.CurrentPeriodEnd
				newPeriodEnd := newPeriodStart.AddDate(0, 1, 0)
				nextBilling := newPeriodEnd

				if err := s.subsRepo.UpdateBillingPeriod(ctx, sub.TenantID, sub.UserID, newPeriodStart, newPeriodEnd, &nextBilling); err != nil {
					if s.logger != nil {
						s.logger.Printf("Failed to renew billing period for subscription %s: %v", sub.ID, err)
					}
					continue
				}
				processed++
				if s.logger != nil {
					s.logger.Printf("Renewed billing period for subscription %s (user %s, tenant %s)", sub.ID, sub.UserID, sub.TenantID)
				}
			}
			// TODO: Integrate with payment provider (Stripe, PayPal, etc.)
		}
	}

	if s.logger != nil {
		s.logger.Printf("Billing cycle processed: %d renewed, %d expired", processed, expired)
	}

	return nil
}

// GetSubscriptionTier returns the effective subscription tier for a user
func (s *SubscriptionService) GetSubscriptionTier(ctx context.Context, tenantID, userID string) (models.SubscriptionTier, error) {
	sub, valid, err := s.CheckSubscription(ctx, tenantID, userID, models.SubscriptionTierFree)
	if err != nil {
		return models.SubscriptionTierFree, err
	}
	if !valid {
		return models.SubscriptionTierFree, nil
	}
	return sub.Tier, nil
}

// EnsureAdminMaxSubscription ensures admin users have Max tier subscription
// This is called automatically when an admin makes their first request
func (s *SubscriptionService) EnsureAdminMaxSubscription(ctx context.Context, tenantID, userID string) error {
	// Get or create subscription
	sub, err := s.GetOrCreateSubscription(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	
	// If already Max tier, nothing to do
	if sub.Tier == models.SubscriptionTierMax {
		return nil
	}
	
	// Upgrade to Max tier
	now := time.Now().UTC()
	sub.Tier = models.SubscriptionTierMax
	sub.Status = "active"
	sub.CurrentPeriodStart = now
	sub.CurrentPeriodEnd = now.AddDate(100, 0, 0) // Effectively never expires
	sub.NextBillingDate = nil
	sub.UpdatedAt = now
	
	if err := s.subsRepo.Upsert(ctx, sub); err != nil {
		return fmt.Errorf("failed to upgrade admin subscription: %w", err)
	}
	
	if s.logger != nil {
		s.logger.Printf("Auto-upgraded admin %s to Max subscription", userID)
	}
	
	return nil
}


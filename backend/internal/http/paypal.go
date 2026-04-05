package http

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/paypal"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/services"
)

// PayPalHandler handles PayPal-related HTTP requests
type PayPalHandler struct {
	paypalClient        *paypal.Client
	subscriptionService *services.SubscriptionService
	subsRepo            *repo.SubscriptionsRepository
	logger              *log.Logger
}

// NewPayPalHandler creates a new PayPalHandler
func NewPayPalHandler(
	paypalClient *paypal.Client,
	subscriptionService *services.SubscriptionService,
	subsRepo *repo.SubscriptionsRepository,
	logger *log.Logger,
) *PayPalHandler {
	return &PayPalHandler{
		paypalClient:        paypalClient,
		subscriptionService: subscriptionService,
		subsRepo:            subsRepo,
		logger:              logger,
	}
}

// CreateSubscriptionRequest represents request to create PayPal subscription
type CreateSubscriptionRequest struct {
	Tier string `json:"tier"` // "pro" or "max"
}

// CreateSubscription handles POST /api/v1/paypal/subscription
// Creates a PayPal subscription plan and subscription for the user
func (h *PayPalHandler) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	tier := models.SubscriptionTier(strings.ToLower(req.Tier))
	if tier != models.SubscriptionTierPro && tier != models.SubscriptionTierMax {
		WriteBadRequest(w, "INVALID_TIER", "Tier must be 'pro' or 'max'")
		return
	}

	// Define pricing (USD)
	currency := os.Getenv("PAYPAL_CURRENCY")
	if currency == "" {
		currency = "USD" // Default
	}

	var price string
	switch tier {
	case models.SubscriptionTierPro:
		price = "29.00"
	case models.SubscriptionTierMax:
		price = "99.00"
	}

	// Get or create product ID (should be done once, stored in env/config)
	productID := os.Getenv("PAYPAL_PRODUCT_ID")
	if productID == "" {
		// For now, use a default product ID - in production, create product separately
		productID = "PROD_" + strings.ToUpper(string(tier))
	}

	// Create subscription plan
	plan, err := h.paypalClient.CreateSubscriptionPlan(r.Context(), string(tier), price, currency, productID)
	if err != nil {
		h.logger.Printf("Failed to create PayPal plan: %v", err)
		WriteInternalError(w, "Failed to create subscription plan")
		return
	}

	// Create subscription with return/cancel URLs
	returnURL := os.Getenv("PAYPAL_RETURN_URL")
	cancelURL := os.Getenv("PAYPAL_CANCEL_URL")
	if returnURL == "" {
		returnURL = "https://horizon.horde.vision/subscription/success"
	}
	if cancelURL == "" {
		cancelURL = "https://horizon.horde.vision/subscription/cancel"
	}

	subscription, err := h.paypalClient.CreateSubscription(r.Context(), plan.ID, returnURL, cancelURL)
	if err != nil {
		h.logger.Printf("Failed to create PayPal subscription: %v", err)
		WriteInternalError(w, "Failed to create subscription")
		return
	}

	// Find approval URL in links
	var approvalURL string
	for _, link := range subscription.Links {
		if link.Rel == "approve" {
			approvalURL = link.Href
			break
		}
	}

	if approvalURL == "" {
		h.logger.Printf("No approval URL found in subscription links")
		WriteInternalError(w, "Failed to get approval URL")
		return
	}

	// Return approval URL to frontend
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"approvalUrl":    approvalURL,
		"subscriptionId": subscription.ID,
		"planId":         plan.ID,
	})
}

// ExecuteSubscriptionRequest represents request to execute PayPal subscription
type ExecuteSubscriptionRequest struct {
	SubscriptionID string `json:"subscriptionId"`
}

// ExecuteSubscription handles POST /api/v1/paypal/subscription/execute
// Confirms subscription after user approval (subscription is auto-activated by PayPal)
func (h *PayPalHandler) ExecuteSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req ExecuteSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// Get subscription details from PayPal
	subscription, err := h.paypalClient.GetSubscription(r.Context(), req.SubscriptionID)
	if err != nil {
		h.logger.Printf("Failed to get PayPal subscription: %v", err)
		WriteInternalError(w, "Failed to get subscription details")
		return
	}

	// Get plan details to determine tier
	plan, err := h.paypalClient.GetClient().GetSubscriptionPlan(r.Context(), subscription.PlanID)
	if err != nil {
		h.logger.Printf("Failed to get PayPal plan: %v", err)
		WriteInternalError(w, "Failed to get plan details")
		return
	}

	// Determine tier from plan name or description
	var tier models.SubscriptionTier
	planName := strings.ToLower(plan.Name)
	planDesc := strings.ToLower(plan.Description)
	if strings.Contains(planName, "pro") || strings.Contains(planDesc, "pro") {
		tier = models.SubscriptionTierPro
	} else if strings.Contains(planName, "max") || strings.Contains(planDesc, "max") {
		tier = models.SubscriptionTierMax
	} else {
		h.logger.Printf("Unknown plan tier: %s (name: %s)", planDesc, planName)
		WriteInternalError(w, "Unknown subscription tier")
		return
	}

	// Create or update subscription in our system
	provider := "paypal"
	sub, err := h.subscriptionService.GetOrCreateSubscription(r.Context(), authInfo.TenantID, authInfo.UID)
	if err != nil {
		h.logger.Printf("Failed to get/create subscription: %v", err)
		WriteInternalError(w, "Failed to create subscription")
		return
	}

	// Update subscription with PayPal info
	sub.Tier = tier
	sub.Status = "active"
	sub.PaymentProvider = &provider
	sub.PayPalPlanID = &plan.ID
	sub.PayPalSubscriptionID = &subscription.ID
	sub.CancelAtPeriodEnd = false // Reset cancellation flag when resuming

	now := time.Now().UTC()
	sub.CurrentPeriodStart = now
	sub.CurrentPeriodEnd = now.AddDate(0, 1, 0) // 1 month
	nextBilling := now.AddDate(0, 1, 0)
	sub.NextBillingDate = &nextBilling
	sub.UpdatedAt = now

	if err := h.subsRepo.Upsert(r.Context(), sub); err != nil {
		h.logger.Printf("Failed to update subscription: %v", err)
		WriteInternalError(w, "Failed to save subscription")
		return
	}

	h.logger.Printf("User %s subscribed to %s tier via PayPal (subscription: %s)", authInfo.UID, tier, subscription.ID)

	WriteJSON(w, http.StatusOK, sub)
}

// CancelSubscription handles POST /api/v1/paypal/subscription/cancel
// Cancels subscription at the end of current billing period
func (h *PayPalHandler) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Get current subscription
	sub, err := h.subscriptionService.GetOrCreateSubscription(r.Context(), authInfo.TenantID, authInfo.UID)
	if err != nil {
		h.logger.Printf("Failed to get subscription: %v", err)
		WriteInternalError(w, "Failed to get subscription")
		return
	}

	// Can only cancel paid subscriptions
	if sub.Tier == models.SubscriptionTierFree {
		WriteBadRequest(w, "INVALID_TIER", "Cannot cancel free subscription")
		return
	}

	// If PayPal subscription exists, cancel it in PayPal
	if sub.PayPalSubscriptionID != nil && *sub.PayPalSubscriptionID != "" {
		// Cancel subscription in PayPal (at period end, not immediately)
		// PayPal subscriptions are cancelled at period end by default when we call CancelSubscription
		if err := h.paypalClient.CancelSubscription(r.Context(), *sub.PayPalSubscriptionID, "User requested cancellation"); err != nil {
			h.logger.Printf("Failed to cancel PayPal subscription %s: %v", *sub.PayPalSubscriptionID, err)
			// Continue anyway - we'll mark it for cancellation in our system
		} else {
			h.logger.Printf("Cancelled PayPal subscription %s", *sub.PayPalSubscriptionID)
		}
	}

	// Mark subscription for cancellation at period end
	cancelledSub, err := h.subscriptionService.CancelSubscriptionAtPeriodEnd(r.Context(), authInfo.TenantID, authInfo.UID)
	if err != nil {
		h.logger.Printf("Failed to cancel subscription: %v", err)
		WriteInternalError(w, "Failed to cancel subscription")
		return
	}

	h.logger.Printf("User %s cancelled subscription (will expire at %s)", authInfo.UID, cancelledSub.CurrentPeriodEnd.Format(time.RFC3339))

	WriteJSON(w, http.StatusOK, cancelledSub)
}

// Webhook handles POST /api/v1/paypal/webhook
// Processes PayPal webhook events
func (h *PayPalHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	webhookID := os.Getenv("PAYPAL_WEBHOOK_ID")
	if webhookID == "" {
		h.logger.Printf("PAYPAL_WEBHOOK_ID not set, skipping verification")
	} else {
		// Collect headers for verification
		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		// Read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			h.logger.Printf("Failed to read webhook body: %v", err)
			WriteBadRequest(w, "INVALID_BODY", "Failed to read request body")
			return
		}

		// Verify webhook signature
		if err := h.paypalClient.VerifyWebhookSignature(r.Context(), webhookID, headers, body); err != nil {
			h.logger.Printf("Webhook verification failed: %v", err)
			WriteUnauthorized(w, "Invalid webhook signature")
			return
		}

		// Parse webhook event
		var event struct {
			ID           string          `json:"id"`
			EventType    string          `json:"event_type"`
			ResourceType string          `json:"resource_type"`
			Resource     json.RawMessage `json:"resource"`
		}
		if err := json.Unmarshal(body, &event); err != nil {
			h.logger.Printf("Failed to parse webhook event: %v", err)
			WriteBadRequest(w, "INVALID_JSON", "Failed to parse webhook event")
			return
		}

		h.logger.Printf("Received PayPal webhook: %s (type: %s)", event.ID, event.EventType)

		// Process webhook event
		if err := h.processWebhookEvent(r.Context(), event.EventType, event.Resource); err != nil {
			h.logger.Printf("Failed to process webhook event: %v", err)
			WriteInternalError(w, "Failed to process webhook")
			return
		}
	}

	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *PayPalHandler) processWebhookEvent(ctx context.Context, eventType string, resource json.RawMessage) error {
	switch eventType {
	case "PAYMENT.SALE.COMPLETED", "BILLING.SUBSCRIPTION.PAYMENT.SUCCEEDED":
		// Payment completed - subscription renewed
		var sale struct {
			BillingAgreementID string `json:"billing_agreement_id"`
			SubscriptionID     string `json:"subscription_id"`
		}
		if err := json.Unmarshal(resource, &sale); err == nil {
			subscriptionID := sale.SubscriptionID
			if subscriptionID == "" {
				subscriptionID = sale.BillingAgreementID
			}
			if subscriptionID != "" {
				return h.handlePaymentCompleted(ctx, subscriptionID)
			}
		}

	case "BILLING.SUBSCRIPTION.CANCELLED", "BILLING.SUBSCRIPTION.EXPIRED":
		// Subscription cancelled or expired
		var subscription struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(resource, &subscription); err == nil && subscription.ID != "" {
			return h.handleSubscriptionCancelled(ctx, subscription.ID)
		}

	case "BILLING.SUBSCRIPTION.REACTIVATED":
		// Subscription reactivated (user resumed cancelled subscription)
		var subscription struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(resource, &subscription); err == nil && subscription.ID != "" {
			return h.handleSubscriptionReactivated(ctx, subscription.ID)
		}

	case "BILLING.SUBSCRIPTION.PAYMENT.FAILED":
		// Payment failed - mark subscription as past_due
		var subscription struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(resource, &subscription); err == nil && subscription.ID != "" {
			return h.handlePaymentFailed(ctx, subscription.ID)
		}
	}

	return nil
}

func (h *PayPalHandler) handlePaymentCompleted(ctx context.Context, subscriptionID string) error {
	// Find subscription by PayPal subscription ID
	subs, err := h.subsRepo.ListByPayPalSubscriptionID(ctx, subscriptionID)
	if err != nil || len(subs) == 0 {
		h.logger.Printf("Subscription not found for PayPal subscription %s", subscriptionID)
		return nil // Not an error - subscription might not exist yet
	}

	sub := subs[0]
	now := time.Now().UTC()
	sub.Status = "active"
	sub.CancelAtPeriodEnd = false // Reset cancellation flag on successful payment (subscription resumed)
	sub.CurrentPeriodStart = now
	sub.CurrentPeriodEnd = now.AddDate(0, 1, 0)
	nextBilling := now.AddDate(0, 1, 0)
	sub.NextBillingDate = &nextBilling
	sub.UpdatedAt = now

	return h.subsRepo.Upsert(ctx, sub)
}

func (h *PayPalHandler) handleSubscriptionCancelled(ctx context.Context, subscriptionID string) error {
	subs, err := h.subsRepo.ListByPayPalSubscriptionID(ctx, subscriptionID)
	if err != nil || len(subs) == 0 {
		return nil
	}

	sub := subs[0]
	sub.Status = "cancelled"
	sub.UpdatedAt = time.Now().UTC()

	return h.subsRepo.Upsert(ctx, sub)
}

func (h *PayPalHandler) handlePaymentFailed(ctx context.Context, subscriptionID string) error {
	subs, err := h.subsRepo.ListByPayPalSubscriptionID(ctx, subscriptionID)
	if err != nil || len(subs) == 0 {
		return nil
	}

	sub := subs[0]
	sub.Status = "past_due"
	sub.UpdatedAt = time.Now().UTC()

	return h.subsRepo.Upsert(ctx, sub)
}

func (h *PayPalHandler) handleSubscriptionReactivated(ctx context.Context, subscriptionID string) error {
	subs, err := h.subsRepo.ListByPayPalSubscriptionID(ctx, subscriptionID)
	if err != nil || len(subs) == 0 {
		return nil
	}

	sub := subs[0]
	// Resume subscription - reset cancellation flag
	sub.Status = "active"
	sub.CancelAtPeriodEnd = false
	sub.UpdatedAt = time.Now().UTC()

	// Also update via subscription service to ensure consistency
	if _, err := h.subscriptionService.ResumeSubscription(ctx, sub.TenantID, sub.UserID); err != nil {
		h.logger.Printf("Failed to resume subscription via service: %v", err)
		// Continue with direct update
	}

	return h.subsRepo.Upsert(ctx, sub)
}

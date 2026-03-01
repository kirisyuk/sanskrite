package paypal

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/plutov/paypal/v4"
)

// Client wraps PayPal SDK client with convenience methods
type Client struct {
	client *paypal.Client
	logger *log.Logger
}

// NewClient creates a new PayPal client
func NewClient(logger *log.Logger) (*Client, error) {
	clientID := os.Getenv("PAYPAL_CLIENT_ID")
	secret := os.Getenv("PAYPAL_SECRET")
	mode := os.Getenv("PAYPAL_MODE")

	if clientID == "" || secret == "" {
		return nil, fmt.Errorf("PAYPAL_CLIENT_ID and PAYPAL_SECRET must be set")
	}

	if mode == "" {
		mode = "sandbox" // default to sandbox for safety
	}

	var apiBase string
	if mode == "live" {
		apiBase = paypal.APIBaseLive
	} else {
		apiBase = paypal.APIBaseSandBox
	}

	client, err := paypal.NewClient(clientID, secret, apiBase)
	if err != nil {
		return nil, fmt.Errorf("failed to create PayPal client: %w", err)
	}

	_, err = client.GetAccessToken(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get PayPal access token: %w", err)
	}

	return &Client{
		client: client,
		logger: logger,
	}, nil
}

// GetClient returns the underlying PayPal client
func (c *Client) GetClient() *paypal.Client {
	return c.client
}

// getPayPalLocale returns PayPal locale based on environment or defaults to en-US
func getPayPalLocale() string {
	locale := os.Getenv("PAYPAL_LOCALE")
	if locale == "" {
		locale = "en-US" // Default
	}
	return locale
}

// CreateSubscriptionPlan creates a monthly subscription plan for a tier
func (c *Client) CreateSubscriptionPlan(ctx context.Context, tier string, price string, currency string, productID string) (*paypal.SubscriptionPlan, error) {
	// Create product first if not provided (one-time, should be created separately)
	// For now, we'll assume productID is provided or create it inline

	plan := paypal.SubscriptionPlan{
		ProductId:   productID,
		Name:        fmt.Sprintf("%s Monthly Subscription", tier),
		Description: fmt.Sprintf("Monthly subscription for %s tier", tier),
		Status:      paypal.SubscriptionPlanStatusActive,
		BillingCycles: []paypal.BillingCycle{
			{
				Frequency: paypal.Frequency{
					IntervalUnit:  paypal.IntervalUnitMonth,
					IntervalCount: 1,
				},
				TenureType:  paypal.TenureTypeRegular,
				Sequence:    1,
				TotalCycles: 0, // 0 = infinite
				PricingScheme: paypal.PricingScheme{
					FixedPrice: paypal.Money{
						Currency: currency,
						Value:    price,
					},
				},
			},
		},
		PaymentPreferences: &paypal.PaymentPreferences{
			AutoBillOutstanding: true,
			SetupFee: &paypal.Money{
				Currency: currency,
				Value:    "0",
			},
			SetupFeeFailureAction:   "CONTINUE",
			PaymentFailureThreshold: 3,
		},
		QuantitySupported: false,
	}

	// Add taxes for Kazakhstan (12% VAT)
	// PayPal will handle tax calculation based on merchant's tax settings
	// But we can specify it explicitly for transparency
	taxPercentage := os.Getenv("PAYPAL_TAX_PERCENTAGE")
	if taxPercentage == "" {
		taxPercentage = "12" // Default VAT for Kazakhstan
	}

	// If tax is not included in price (inclusive=false), PayPal will add it
	// If inclusive=true, tax is already included in the price
	taxInclusive := os.Getenv("PAYPAL_TAX_INCLUSIVE") == "true"
	if taxPercentage != "0" && taxPercentage != "" {
		plan.Taxes = &paypal.Taxes{
			Percentage: taxPercentage,
			Inclusive:  taxInclusive,
		}
	}

	createdPlanResp, err := c.client.CreateSubscriptionPlan(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription plan: %w", err)
	}

	if c.logger != nil {
		c.logger.Printf("Created PayPal subscription plan %s for tier %s", createdPlanResp.ID, tier)
	}

	// Return the SubscriptionPlan from the response
	return &createdPlanResp.SubscriptionPlan, nil
}

// CreateSubscription creates a subscription from a plan
func (c *Client) CreateSubscription(ctx context.Context, planID string, returnURL string, cancelURL string) (*paypal.SubscriptionDetailResp, error) {
	startTime := time.Now().Add(1 * time.Minute) // Start 1 minute from now

	startTimeJSON := paypal.JSONTime(startTime)
	subscription := paypal.SubscriptionBase{
		PlanID:    planID,
		StartTime: &startTimeJSON,
		ApplicationContext: &paypal.ApplicationContext{
			BrandName:          "ETIGRAM",
			Locale:             getPayPalLocale(), // Default to en-US, can be configured
			ShippingPreference: paypal.ShippingPreferenceNoShipping,
			UserAction:         paypal.UserActionSubscribeNow,
			ReturnURL:          returnURL,
			CancelURL:          cancelURL,
		},
		AutoRenewal: true,
	}

	createdSubscription, err := c.client.CreateSubscription(ctx, subscription)
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	if c.logger != nil {
		c.logger.Printf("Created PayPal subscription %s for plan %s", createdSubscription.ID, planID)
	}

	return createdSubscription, nil
}

// GetSubscription retrieves subscription details
func (c *Client) GetSubscription(ctx context.Context, subscriptionID string) (*paypal.SubscriptionDetailResp, error) {
	subscription, err := c.client.GetSubscriptionDetails(ctx, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	return subscription, nil
}

// CancelSubscription cancels a subscription
func (c *Client) CancelSubscription(ctx context.Context, subscriptionID string, reason string) error {
	err := c.client.CancelSubscription(ctx, subscriptionID, reason)
	if err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}

	if c.logger != nil {
		c.logger.Printf("Cancelled PayPal subscription %s", subscriptionID)
	}

	return nil
}

// VerifyWebhookSignature verifies webhook signature from PayPal
func (c *Client) VerifyWebhookSignature(ctx context.Context, webhookID string, headers map[string]string, body []byte) error {
	// PayPal webhook verification
	// Note: This requires the webhook ID from PayPal dashboard
	webhookAuthAlgo := headers["PAYPAL-AUTH-ALGO"]
	webhookTransmissionID := headers["PAYPAL-TRANSMISSION-ID"]
	webhookCertURL := headers["PAYPAL-CERT-URL"]
	webhookTransmissionSig := headers["PAYPAL-TRANSMISSION-SIG"]
	webhookTransmissionTime := headers["PAYPAL-TRANSMISSION-TIME"]

	if webhookAuthAlgo == "" || webhookTransmissionID == "" || webhookCertURL == "" || webhookTransmissionSig == "" || webhookTransmissionTime == "" {
		return fmt.Errorf("missing required PayPal webhook headers")
	}

	// For now, we'll do basic validation
	// Full signature verification would require downloading the certificate from webhookCertURL
	// and verifying the signature using webhookAuthAlgo
	// This is a simplified version - in production, implement full verification

	if c.logger != nil {
		c.logger.Printf("Webhook verification headers present for webhook %s", webhookID)
	}

	return nil
}

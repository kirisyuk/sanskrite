# PayPal Integration Guide

## Overview

PayPal integration has been implemented for subscription payments. Users can subscribe to Pro or Max tiers via PayPal.

## Backend Implementation

### Components

1. **PayPal Client** (`backend/internal/paypal/client.go`)
   - Wraps PayPal SDK v4
   - Handles subscription plan creation
   - Manages subscriptions and webhooks

2. **PayPal Handlers** (`backend/internal/http/paypal.go`)
   - `POST /api/v1/paypal/subscription` - Create subscription
   - `POST /api/v1/paypal/subscription/execute` - Execute subscription after approval
   - `POST /api/v1/paypal/webhook` - Handle PayPal webhook events

3. **Updated Models**
   - `Subscription` model now includes PayPal fields:
     - `PaymentProvider` - "paypal", "voucher", or "admin"
     - `PayPalPlanID` - PayPal plan ID
     - `PayPalAgreementID` - Legacy agreement ID (for compatibility)
     - `PayPalSubscriptionID` - PayPal subscription ID

### Environment Variables

Required:
- `PAYPAL_CLIENT_ID` - PayPal Client ID
- `PAYPAL_SECRET` - PayPal Secret
- `PAYPAL_MODE` - "live" or "sandbox" (default: "sandbox")
- `PAYPAL_WEBHOOK_ID` - Webhook ID from PayPal dashboard
- `PAYPAL_RETURN_URL` - URL to redirect after successful payment
- `PAYPAL_CANCEL_URL` - URL to redirect after cancellation

Optional:
- `PAYPAL_PRODUCT_ID` - PayPal Product ID (if not set, will use generated)
- `PAYPAL_CURRENCY` - Currency code (default: "USD", set to "KZT" for Kazakhstan)
- `PAYPAL_LOCALE` - PayPal locale (default: "en-US", set to "ru-KZ" for Kazakhstan)
- `PAYPAL_TAX_PERCENTAGE` - Tax percentage (default: "12" for Kazakhstan VAT)
- `PAYPAL_TAX_INCLUSIVE` - Whether tax is included in price (default: "false")

### GCP Secret Manager

Credentials are stored in Secret Manager:
- `paypal_client_id`
- `paypal_secret`
- `paypal_webhook_id`

### Webhook Events Handled

- `PAYMENT.SALE.COMPLETED` / `BILLING.SUBSCRIPTION.PAYMENT.SUCCEEDED` - Payment completed
- `BILLING.SUBSCRIPTION.CANCELLED` - Subscription cancelled
- `BILLING.SUBSCRIPTION.EXPIRED` - Subscription expired
- `BILLING.SUBSCRIPTION.PAYMENT.FAILED` - Payment failed

## Setup Instructions

### 1. Create Secrets in GCP

```bash
# Client ID
echo "YOUR_CLIENT_ID" | gcloud secrets create paypal_client_id --data-file=-

# Secret
echo "YOUR_SECRET" | gcloud secrets create paypal_secret --data-file=-

# Webhook ID
echo "YOUR_WEBHOOK_ID" | gcloud secrets create paypal_webhook_id --data-file=-
```

### 2. Update Deployment Script

The deployment script (`scripts/deploy`) has been updated to automatically include PayPal secrets if they exist.

### 3. Configure PayPal Dashboard

1. Go to PayPal Developer Dashboard
2. Set webhook URL: `https://sanskrite-api-sqt63d2k2q-uc.a.run.app/api/v1/paypal/webhook`
3. Subscribe to events:
   - `PAYMENT.SALE.COMPLETED`
   - `BILLING.SUBSCRIPTION.CANCELLED`
   - `BILLING.SUBSCRIPTION.EXPIRED`
   - `BILLING.SUBSCRIPTION.PAYMENT.FAILED`
   - `BILLING.SUBSCRIPTION.PAYMENT.SUCCEEDED`

### 4. Create PayPal Product (Optional)

For better organization, create a product in PayPal Dashboard and set `PAYPAL_PRODUCT_ID` environment variable. Otherwise, the system will generate product IDs.

## Pricing

Current pricing (USD):
- **Pro**: $29.00/month
- **Max**: $99.00/month

Prices are in USD. PayPal will handle currency conversion if needed based on user's location.

## Kazakhstan-Specific Configuration

For Kazakhstan business:
- Currency: USD (PayPal will handle conversion to local currency if needed)
- Locale: ru-KZ (Russian - Kazakhstan)
- Tax: 12% VAT (not included in price by default)
- Business account location: Kazakhstan

These settings are configured in the deployment script for Kazakhstan market.

## Frontend Integration (TODO)

Frontend components need to be created to:
1. Display subscription plans
2. Initiate PayPal subscription flow
3. Handle approval redirect
4. Display subscription status

## Testing

Use PayPal Sandbox for testing:
1. Set `PAYPAL_MODE=sandbox`
2. Use sandbox credentials
3. Test with sandbox PayPal accounts

## Notes

- Subscription plans are created dynamically for each subscription request
- Consider caching/using existing plans in production
- Webhook signature verification is basic - implement full verification for production
- Product ID should be created once and reused


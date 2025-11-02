import React from 'react';
import { api } from '../lib/api';
import { useAuth } from '../contexts/AuthContext';

type SubscriptionTier = 'free' | 'pro' | 'max';
type SubscriptionStatus = 'active' | 'expired' | 'cancelled';

interface Subscription {
  id: string;
  tenantId: string;
  userId: string;
  tier: SubscriptionTier;
  status: SubscriptionStatus;
  startedAt: string;
  currentPeriodStart: string;
  currentPeriodEnd: string;
  nextBillingDate?: string;
  voucherId?: string;
  paymentProvider?: string;
  paymentId?: string;
  paypalPlanId?: string;
  paypalAgreementId?: string;
  paypalSubscriptionId?: string;
  metadata?: Record<string, any>;
  createdAt: string;
  updatedAt: string;
}

export const SubscriptionTab: React.FC = () => {
  const { currentUser } = useAuth();
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [subscription, setSubscription] = React.useState<Subscription | null>(null);
  const [voucherCode, setVoucherCode] = React.useState('');
  const [applyingVoucher, setApplyingVoucher] = React.useState(false);
  const [voucherError, setVoucherError] = React.useState<string | null>(null);
  const [upgrading, setUpgrading] = React.useState<SubscriptionTier | null>(null);

  React.useEffect(() => {
    if (!currentUser?.uid) return;
    
    // Handle PayPal redirect after payment approval
    const urlParams = new URLSearchParams(window.location.search);
    const token = urlParams.get('token');
    const payerId = urlParams.get('PayerID');
    const subscriptionId = sessionStorage.getItem('paypal_subscription_id');
    
    if (token && payerId && subscriptionId) {
      // Clean up URL and sessionStorage
      window.history.replaceState({}, '', window.location.pathname);
      sessionStorage.removeItem('paypal_subscription_id');
      
      // Execute the subscription
      (async () => {
        try {
          setLoading(true);
          const updated = await api.executePayPalSubscription(subscriptionId);
          setSubscription(updated);
          setLoading(false);
        } catch (err: any) {
          console.error('[SubscriptionTab] Error executing PayPal subscription:', err);
          setError(err?.response?.data?.message || err?.message || 'Failed to complete subscription');
          setLoading(false);
        }
      })();
      return;
    }
    
    loadSubscription();
  }, [currentUser?.uid]);

  const loadSubscription = async () => {
    setLoading(true);
    setError(null);
    try {
      const sub = await api.getSubscription();
      setSubscription(sub);
    } catch (err: any) {
      setError(err?.message || 'Failed to load subscription');
      console.error('[SubscriptionTab] Error loading subscription:', err);
    } finally {
      setLoading(false);
    }
  };

  const handleApplyVoucher = async () => {
    if (!voucherCode.trim()) {
      setVoucherError('Please enter a voucher code');
      return;
    }

    setApplyingVoucher(true);
    setVoucherError(null);
    try {
      const updated = await api.applyVoucher(voucherCode.trim().toUpperCase());
      setSubscription(updated);
      setVoucherCode('');
    } catch (err: any) {
      setVoucherError(err?.response?.data?.message || err?.message || 'Failed to apply voucher');
      console.error('[SubscriptionTab] Error applying voucher:', err);
    } finally {
      setApplyingVoucher(false);
    }
  };

  const handleUpgrade = async (tier: 'pro' | 'max') => {
    setUpgrading(tier);
    try {
      const { approvalUrl, subscriptionId } = await api.createPayPalSubscription(tier);
      // Store subscriptionId for after PayPal redirect
      sessionStorage.setItem('paypal_subscription_id', subscriptionId);
      // Redirect to PayPal
      window.location.href = approvalUrl;
    } catch (err: any) {
      console.error('[SubscriptionTab] Error creating PayPal subscription:', err);
      alert(err?.response?.data?.message || err?.message || 'Failed to start upgrade process');
      setUpgrading(null);
    }
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleDateString('en-US', { 
      year: 'numeric', 
      month: 'long', 
      day: 'numeric' 
    });
  };

  const getTierDisplayName = (tier: SubscriptionTier) => {
    return tier.charAt(0).toUpperCase() + tier.slice(1);
  };

  const getTierPrice = (tier: SubscriptionTier) => {
    if (tier === 'pro') return '$29';
    if (tier === 'max') return '$99';
    return 'Free';
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>Loading subscription...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-error)' }}>{error}</div>
      </div>
    );
  }

  if (!subscription) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>No subscription found</div>
      </div>
    );
  }

  const isActive = subscription.status === 'active';
  const isPaidTier = subscription.tier === 'pro' || subscription.tier === 'max';

  return (
    <div className="space-y-6">
      {/* Current Subscription */}
      <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
        <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>Current Subscription</div>
        
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Tier:</span>
            <span className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
              {getTierDisplayName(subscription.tier)}
            </span>
          </div>
          
          <div className="flex items-center justify-between">
            <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Status:</span>
            <span 
              className="text-13 px-2 py-1 rounded"
              style={{
                backgroundColor: isActive ? 'rgba(34, 197, 94, 0.1)' : 'rgba(239, 68, 68, 0.1)',
                color: isActive ? 'rgb(34, 197, 94)' : 'rgb(239, 68, 68)',
              }}
            >
              {subscription.status.charAt(0).toUpperCase() + subscription.status.slice(1)}
            </span>
          </div>

          {isPaidTier && (
            <>
              <div className="flex items-center justify-between">
                <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Current Period:</span>
                <span className="text-13" style={{ color: 'var(--text-heading)' }}>
                  {formatDate(subscription.currentPeriodStart)} - {formatDate(subscription.currentPeriodEnd)}
                </span>
              </div>

              {subscription.nextBillingDate && (
                <div className="flex items-center justify-between">
                  <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Next Billing:</span>
                  <span className="text-13" style={{ color: 'var(--text-heading)' }}>
                    {formatDate(subscription.nextBillingDate)}
                  </span>
                </div>
              )}

              {subscription.paymentProvider && (
                <div className="flex items-center justify-between">
                  <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Payment Method:</span>
                  <span className="text-13" style={{ color: 'var(--text-heading)' }}>
                    {subscription.paymentProvider.charAt(0).toUpperCase() + subscription.paymentProvider.slice(1)}
                  </span>
                </div>
              )}
            </>
          )}

          <div className="flex items-center justify-between">
            <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Started:</span>
            <span className="text-13" style={{ color: 'var(--text-heading)' }}>
              {formatDate(subscription.startedAt)}
            </span>
          </div>
        </div>
      </div>

      {/* Voucher Code */}
      {subscription.tier === 'free' && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-13 mb-3" style={{ color: 'var(--text-heading)' }}>Apply Voucher Code</div>
          <div className="flex gap-2">
            <input
              type="text"
              value={voucherCode}
              onChange={(e) => setVoucherCode(e.target.value.toUpperCase())}
              placeholder="Enter voucher code"
              className="flex-1 px-3 py-2 rounded text-13"
              style={{
                backgroundColor: 'var(--white-05)',
                border: '1px solid var(--overlay-line-90)',
                color: 'var(--text-heading)',
              }}
              onKeyPress={(e) => {
                if (e.key === 'Enter') {
                  handleApplyVoucher();
                }
              }}
            />
            <button
              onClick={handleApplyVoucher}
              disabled={applyingVoucher || !voucherCode.trim()}
              className="px-4 py-2 rounded text-13 font-medium transition-colors"
              style={{
                backgroundColor: applyingVoucher || !voucherCode.trim() ? 'var(--white-05)' : 'var(--accent-primary)',
                color: applyingVoucher || !voucherCode.trim() ? 'var(--text-tertiary-weak)' : 'white',
                cursor: applyingVoucher || !voucherCode.trim() ? 'not-allowed' : 'pointer',
              }}
            >
              {applyingVoucher ? 'Applying...' : 'Apply'}
            </button>
          </div>
          {voucherError && (
            <div className="mt-2 text-12" style={{ color: 'var(--text-error)' }}>
              {voucherError}
            </div>
          )}
        </div>
      )}

      {/* Upgrade Options */}
      {subscription.tier !== 'max' && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>Upgrade Subscription</div>
          
          <div className="space-y-3">
            {subscription.tier === 'free' && (
              <div className="p-3 rounded" style={{ backgroundColor: 'var(--white-10)', border: '1px solid var(--overlay-line-90)' }}>
                <div className="flex items-center justify-between mb-2">
                  <div>
                    <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>Pro</div>
                    <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
                      Enhanced features and higher limits
                    </div>
                  </div>
                  <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
                    {getTierPrice('pro')}/month
                  </div>
                </div>
                <button
                  onClick={() => handleUpgrade('pro')}
                  disabled={upgrading === 'pro'}
                  className="w-full px-4 py-2 rounded text-13 font-medium transition-colors"
                  style={{
                    backgroundColor: upgrading === 'pro' ? 'var(--white-05)' : 'var(--accent-primary)',
                    color: upgrading === 'pro' ? 'var(--text-tertiary-weak)' : 'white',
                    cursor: upgrading === 'pro' ? 'not-allowed' : 'pointer',
                  }}
                >
                  {upgrading === 'pro' ? 'Processing...' : 'Upgrade to Pro'}
                </button>
              </div>
            )}

            {subscription.tier !== 'max' && (
              <div className="p-3 rounded" style={{ backgroundColor: 'var(--white-10)', border: '1px solid var(--overlay-line-90)' }}>
                <div className="flex items-center justify-between mb-2">
                  <div>
                    <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>Max</div>
                    <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
                      Maximum features and unlimited access
                    </div>
                  </div>
                  <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
                    {getTierPrice('max')}/month
                  </div>
                </div>
                <button
                  onClick={() => handleUpgrade('max')}
                  disabled={upgrading === 'max'}
                  className="w-full px-4 py-2 rounded text-13 font-medium transition-colors"
                  style={{
                    backgroundColor: upgrading === 'max' ? 'var(--white-05)' : 'var(--accent-primary)',
                    color: upgrading === 'max' ? 'var(--text-tertiary-weak)' : 'white',
                    cursor: upgrading === 'max' ? 'not-allowed' : 'pointer',
                  }}
                >
                  {upgrading === 'max' ? 'Processing...' : subscription.tier === 'free' ? 'Upgrade to Max' : 'Upgrade to Max'}
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};


import React from 'react';
import { useTranslation } from 'react-i18next';
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
  cancelAtPeriodEnd?: boolean;
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
  const { t } = useTranslation();
  const { currentUser, refreshSubscription } = useAuth();
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [subscription, setSubscription] = React.useState<Subscription | null>(null);
  const [voucherCode, setVoucherCode] = React.useState('');
  const [applyingVoucher, setApplyingVoucher] = React.useState(false);
  const [voucherError, setVoucherError] = React.useState<string | null>(null);
  const [voucherSuccess, setVoucherSuccess] = React.useState<string | null>(null);
  const [upgrading, setUpgrading] = React.useState<SubscriptionTier | null>(null);
  const [cancelling, setCancelling] = React.useState(false);
  const [showCancelConfirm, setShowCancelConfirm] = React.useState(false);
  const [cancelError, setCancelError] = React.useState<string | null>(null);

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
      setVoucherError(t('account.subscription.enterVoucherCode'));
      setVoucherSuccess(null);
      return;
    }

    setApplyingVoucher(true);
    setVoucherError(null);
    setVoucherSuccess(null);
    try {
      const updated = await api.applyVoucher(voucherCode.trim().toUpperCase());
      setSubscription(updated);
      setVoucherCode('');
      setVoucherSuccess(t('account.subscription.voucherApplied', { tier: getTierDisplayName(updated.tier) }));
      // Refresh subscription in AuthContext
      await refreshSubscription();
      // Clear success message after 5 seconds
      setTimeout(() => setVoucherSuccess(null), 5000);
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

  const handleCancelSubscription = async () => {
    setCancelling(true);
    setCancelError(null);
    try {
      const updated = await api.cancelPayPalSubscription();
      setSubscription(updated);
      setShowCancelConfirm(false);
      await refreshSubscription();
    } catch (err: any) {
      console.error('[SubscriptionTab] Error cancelling subscription:', err);
      setCancelError(err?.response?.data?.message || err?.message || 'Failed to cancel subscription');
    } finally {
      setCancelling(false);
    }
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleDateString('en-US', { 
      year: 'numeric', 
      month: 'long', 
      day: 'numeric' 
    });
  };

  const getTierDisplayName = (tier: SubscriptionTier): string => {
    if (tier === 'free') return t('account.subscription.free');
    if (tier === 'pro') return t('account.subscription.pro');
    if (tier === 'max') return t('account.subscription.max');
    // This should never happen, but TypeScript requires a return
    return String(tier).charAt(0).toUpperCase() + String(tier).slice(1);
  };

  const getTierPrice = (tier: SubscriptionTier) => {
    if (tier === 'pro') return '$29';
    if (tier === 'max') return '$99';
    return 'Free';
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.subscription.loading')}</div>
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
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.subscription.notFound')}</div>
      </div>
    );
  }

  const isActive = subscription.status === 'active';
  const isPaidTier = subscription.tier === 'pro' || subscription.tier === 'max';

  return (
    <div className="space-y-6">
      {/* Current Subscription */}
      <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
        <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>{t('account.subscription.currentSubscription')}</div>
        
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.subscription.tier')}:</span>
            <span className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
              {getTierDisplayName(subscription.tier)}
            </span>
          </div>
          
          <div className="flex items-center justify-between">
            <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.subscription.status')}:</span>
            <span 
              className="text-13 px-2 py-1 rounded"
              style={{
                backgroundColor: isActive ? 'rgba(34, 197, 94, 0.1)' : 'rgba(239, 68, 68, 0.1)',
                color: isActive ? 'rgb(34, 197, 94)' : 'rgb(239, 68, 68)',
              }}
            >
              {subscription.status === 'active' ? t('account.subscription.active') : 
               subscription.status === 'expired' ? t('account.subscription.expired') : 
               t('account.subscription.cancelled')}
            </span>
          </div>

          {isPaidTier && (
            <>
              <div className="flex items-center justify-between">
                <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.subscription.currentPeriod')}:</span>
                <span className="text-13" style={{ color: 'var(--text-heading)' }}>
                  {formatDate(subscription.currentPeriodStart)} - {formatDate(subscription.currentPeriodEnd)}
                </span>
              </div>

              {subscription.nextBillingDate && (
                <div className="flex items-center justify-between">
                  <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.subscription.nextBilling')}:</span>
                  <span className="text-13" style={{ color: 'var(--text-heading)' }}>
                    {formatDate(subscription.nextBillingDate)}
                  </span>
                </div>
              )}

              {subscription.paymentProvider && (
                <div className="flex items-center justify-between">
                  <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.subscription.paymentMethod')}:</span>
                  <span className="text-13" style={{ color: 'var(--text-heading)' }}>
                    {subscription.paymentProvider.charAt(0).toUpperCase() + subscription.paymentProvider.slice(1)}
                  </span>
                </div>
              )}

              {subscription.cancelAtPeriodEnd && (
                <div className="mt-3 p-3 rounded" style={{ backgroundColor: 'rgba(251, 191, 36, 0.1)', border: '1px solid rgba(251, 191, 36, 0.3)' }}>
                  <div className="text-12 font-medium" style={{ color: 'rgb(251, 191, 36)' }}>
                    {t('account.subscription.cancelledAtPeriodEnd')}
                  </div>
                  <div className="text-11 mt-1" style={{ color: 'var(--text-secondary)' }}>
                    {t('account.subscription.cancelledAtPeriodEndDescription', { date: formatDate(subscription.currentPeriodEnd) })}
                  </div>
                </div>
              )}
            </>
          )}

          <div className="flex items-center justify-between">
            <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.subscription.started')}:</span>
            <span className="text-13" style={{ color: 'var(--text-heading)' }}>
              {formatDate(subscription.startedAt)}
            </span>
          </div>
        </div>
      </div>

      {/* Voucher Code */}
      <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
        <div className="text-13 mb-3" style={{ color: 'var(--text-heading)' }}>{t('account.subscription.applyVoucher')}</div>
        <div className="text-12 mb-3" style={{ color: 'var(--text-secondary)' }}>
          {t('account.subscription.voucherDescription')}
        </div>
        <div className="flex gap-2">
          <input
            type="text"
            value={voucherCode}
            onChange={(e) => {
              setVoucherCode(e.target.value.toUpperCase());
              setVoucherError(null);
              setVoucherSuccess(null);
            }}
            placeholder={t('account.subscription.enterVoucherCode')}
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
            {applyingVoucher ? t('account.subscription.applying') : t('account.subscription.apply')}
          </button>
        </div>
        {voucherError && (
          <div className="mt-2 text-12" style={{ color: 'var(--text-error)' }}>
            {voucherError}
          </div>
        )}
        {voucherSuccess && (
          <div className="mt-2 text-12" style={{ color: 'rgb(34, 197, 94)' }}>
            {voucherSuccess}
          </div>
        )}
      </div>

      {/* Upgrade Options */}
      {subscription.tier !== 'max' && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>{t('account.subscription.upgradeSubscription')}</div>
          
          <div className="space-y-3">
            {subscription.tier === 'free' && (
              <div className="p-3 rounded" style={{ backgroundColor: 'var(--white-10)', border: '1px solid var(--overlay-line-90)' }}>
                <div className="flex items-center justify-between mb-2">
                  <div>
                    <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>{t('account.subscription.pro')}</div>
                    <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
                      {t('account.subscription.proDescription')}
                    </div>
                  </div>
                  <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
                    {getTierPrice('pro')}{t('account.subscription.perMonth')}
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
                  {upgrading === 'pro' ? t('account.subscription.processing') : t('account.subscription.upgradeToPro')}
                </button>
              </div>
            )}

            {(subscription.tier === 'free' || subscription.tier === 'pro') && (
              <div className="p-3 rounded" style={{ backgroundColor: 'var(--white-10)', border: '1px solid var(--overlay-line-90)' }}>
                <div className="flex items-center justify-between mb-2">
                  <div>
                    <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>{t('account.subscription.max')}</div>
                    <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
                      {t('account.subscription.maxDescription')}
                    </div>
                  </div>
                  <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
                    {getTierPrice('max')}{t('account.subscription.perMonth')}
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
                  {upgrading === 'max' ? t('account.subscription.processing') : t('account.subscription.upgradeToMax')}
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Cancel Subscription */}
      {isPaidTier && isActive && !subscription.cancelAtPeriodEnd && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-13 mb-3" style={{ color: 'var(--text-heading)' }}>{t('account.subscription.cancelSubscription')}</div>
          <div className="text-12 mb-4" style={{ color: 'var(--text-secondary)' }}>
            {t('account.subscription.cancelDescription')}
          </div>
          <button
            onClick={() => setShowCancelConfirm(true)}
            className="px-4 py-2 rounded text-13 font-medium transition-colors"
            style={{
              backgroundColor: 'rgba(239, 68, 68, 0.1)',
              color: 'rgb(239, 68, 68)',
              border: '1px solid rgba(239, 68, 68, 0.3)',
              cursor: 'pointer',
            }}
          >
            {t('account.subscription.cancelButton')}
          </button>
        </div>
      )}

      {/* Cancel Confirmation Modal */}
      {showCancelConfirm && (
        <div 
          className="fixed inset-0 flex items-center justify-center z-50"
          style={{ backgroundColor: 'rgba(0, 0, 0, 0.5)' }}
          onClick={() => !cancelling && setShowCancelConfirm(false)}
        >
          <div 
            className="p-6 rounded-lg max-w-md"
            style={{ 
              backgroundColor: 'var(--background-primary)',
              border: '1px solid var(--overlay-line-90)',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="text-15 font-semibold mb-4" style={{ color: 'var(--text-heading)' }}>
              {t('account.subscription.confirmCancel')}
            </div>
            <div className="text-13 mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
              {t('account.subscription.cancelWarning')}
            </div>
            <div className="text-12 mb-4" style={{ color: 'var(--text-secondary)' }}>
              {t('account.subscription.cancelDetails', { 
                date: formatDate(subscription.currentPeriodEnd),
                tier: getTierDisplayName(subscription.tier)
              })}
            </div>
            {cancelError && (
              <div className="mb-4 p-2 rounded text-12" style={{ backgroundColor: 'rgba(239, 68, 68, 0.1)', color: 'rgb(239, 68, 68)' }}>
                {cancelError}
              </div>
            )}
            <div className="flex gap-3">
              <button
                onClick={() => setShowCancelConfirm(false)}
                disabled={cancelling}
                className="flex-1 px-4 py-2 rounded text-13 font-medium transition-colors"
                style={{
                  backgroundColor: 'var(--white-05)',
                  color: 'var(--text-heading)',
                  border: '1px solid var(--overlay-line-90)',
                  cursor: cancelling ? 'not-allowed' : 'pointer',
                }}
              >
                {t('account.subscription.keepSubscription')}
              </button>
              <button
                onClick={handleCancelSubscription}
                disabled={cancelling}
                className="flex-1 px-4 py-2 rounded text-13 font-medium transition-colors"
                style={{
                  backgroundColor: cancelling ? 'var(--white-05)' : 'rgb(239, 68, 68)',
                  color: cancelling ? 'var(--text-tertiary-weak)' : 'white',
                  cursor: cancelling ? 'not-allowed' : 'pointer',
                }}
              >
                {cancelling ? t('account.subscription.cancelling') : t('account.subscription.confirmCancelButton')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};


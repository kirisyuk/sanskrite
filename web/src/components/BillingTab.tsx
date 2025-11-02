import React from 'react';
import { api } from '../lib/api';
import { useAuth } from '../contexts/AuthContext';

export const BillingTab: React.FC = () => {
  const { currentUser } = useAuth();
  const [loading, setLoading] = React.useState(true);
  const [subscription, setSubscription] = React.useState<any>(null);

  React.useEffect(() => {
    if (!currentUser?.uid) return;
    loadSubscription();
  }, [currentUser?.uid]);

  const loadSubscription = async () => {
    setLoading(true);
    try {
      const sub = await api.getSubscription();
      setSubscription(sub);
    } catch (err: any) {
      console.error('[BillingTab] Error loading subscription:', err);
    } finally {
      setLoading(false);
    }
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleDateString('en-US', { 
      year: 'numeric', 
      month: 'long', 
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  const formatCurrency = (amount: number, currency: string = 'USD') => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currency,
    }).format(amount);
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>Loading billing information...</div>
      </div>
    );
  }

  if (!subscription) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>No billing information available</div>
      </div>
    );
  }

  const isPaidTier = subscription.tier === 'pro' || subscription.tier === 'max';
  const billingHistory: Array<{
    date: string;
    amount: number;
    currency: string;
    description: string;
    status: string;
  }> = [];

  // Add current period billing if paid tier
  if (isPaidTier && subscription.currentPeriodStart) {
    const tierPrice = subscription.tier === 'pro' ? 29 : 99;
    billingHistory.push({
      date: subscription.currentPeriodStart,
      amount: tierPrice,
      currency: 'USD',
      description: `${subscription.tier.charAt(0).toUpperCase() + subscription.tier.slice(1)} Subscription - ${new Date(subscription.currentPeriodStart).toLocaleDateString('en-US', { month: 'long', year: 'numeric' })}`,
      status: subscription.status === 'active' ? 'paid' : 'pending',
    });
  }

  return (
    <div className="space-y-6">
      {/* Billing Summary */}
      {isPaidTier && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>Billing Summary</div>
          
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Current Plan:</span>
              <span className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
                {subscription.tier.charAt(0).toUpperCase() + subscription.tier.slice(1)}
              </span>
            </div>
            
            <div className="flex items-center justify-between">
              <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Monthly Cost:</span>
              <span className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
                {subscription.tier === 'pro' ? '$29.00' : '$99.00'}
              </span>
            </div>

            {subscription.nextBillingDate && (
              <div className="flex items-center justify-between">
                <span className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Next Billing Date:</span>
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
          </div>
        </div>
      )}

      {/* Billing History */}
      <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
        <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>Billing History</div>
        
        {billingHistory.length === 0 ? (
          <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
            {isPaidTier 
              ? 'No billing history available yet. Your first invoice will appear here after your first billing cycle.'
              : 'You are on a free plan. Upgrade to Pro or Max to see billing history.'}
          </div>
        ) : (
          <div className="space-y-2">
            {billingHistory.map((item, index) => (
              <div
                key={index}
                className="flex items-center justify-between p-3 rounded"
                style={{ backgroundColor: 'var(--white-10)', border: '1px solid var(--overlay-line-90)' }}
              >
                <div className="flex-1">
                  <div className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>
                    {item.description}
                  </div>
                  <div className="text-12 mt-1" style={{ color: 'var(--text-secondary)' }}>
                    {formatDate(item.date)}
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-13 font-semibold" style={{ color: 'var(--text-heading)' }}>
                    {formatCurrency(item.amount, item.currency)}
                  </div>
                  <div 
                    className="text-12 mt-1"
                    style={{
                      color: item.status === 'paid' ? 'rgb(34, 197, 94)' : 'rgb(251, 191, 36)',
                    }}
                  >
                    {item.status.charAt(0).toUpperCase() + item.status.slice(1)}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Payment Information */}
      {isPaidTier && subscription.paymentProvider === 'paypal' && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-13 mb-3" style={{ color: 'var(--text-heading)' }}>Payment Information</div>
          <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
            Your subscription is managed through PayPal. To update your payment method or cancel your subscription, 
            please visit your PayPal account or contact support.
          </div>
        </div>
      )}
    </div>
  );
};


import React from 'react';
import { SubscriptionTab } from './SubscriptionTab';
import { BillingTab } from './BillingTab';
import { UsageTab } from './UsageTab';

type TabKey = 'subscription' | 'billing' | 'usage';

const tabs: { key: TabKey; label: string }[] = [
  { key: 'subscription', label: 'Subscription' },
  { key: 'billing', label: 'Billing' },
  { key: 'usage', label: 'Usage' },
];

export const Account: React.FC = () => {
  const [active, setActive] = React.useState<TabKey>('subscription');

  return (
    <div className="rounded-xl" style={{ backgroundColor: 'transparent', padding: '0' }}>
      <div className="flex" style={{ gap: '16px' }}>
        {/* Tabs column */}
        <div className="flex-shrink-0" style={{ width: '220px' }}>
          <div className="flex flex-col" style={{ gap: '6px' }}>
            {tabs.map(t => (
              <button
                key={t.key}
                className="text-13 text-left rounded-lg px-3 py-2 transition-colors"
                style={{
                  backgroundColor: active === t.key ? 'var(--white-10)' : 'transparent',
                  color: 'var(--text-heading)'
                }}
                onMouseEnter={(e) => { if (active !== t.key) (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { if (active !== t.key) (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setActive(t.key)}
              >
                {t.label}
              </button>
            ))}
          </div>
        </div>

        {/* Vertical divider */}
        <div style={{ width: '1px', backgroundColor: 'var(--overlay-line-90)' }} />

        {/* Content full width */}
        <div className="flex-1" style={{ paddingLeft: '16px' }}>
          {active === 'subscription' && <SubscriptionTab />}
          {active === 'billing' && <BillingTab />}
          {active === 'usage' && <UsageTab />}
        </div>
      </div>
    </div>
  );
};

export default Account;


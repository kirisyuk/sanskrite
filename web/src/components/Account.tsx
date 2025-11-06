import React from 'react';
import { useTranslation } from 'react-i18next';
import { SubscriptionTab } from './SubscriptionTab';
import { BillingTab } from './BillingTab';
import { UsageTab } from './UsageTab';

type TabKey = 'subscription' | 'billing' | 'usage';

export const Account: React.FC = () => {
  const { t } = useTranslation();
  const [active, setActive] = React.useState<TabKey>('subscription');
  const [isMobile, setIsMobile] = React.useState(false);

  React.useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };
    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  const tabs: { key: TabKey; label: string; icon: React.ReactNode }[] = [
    { 
      key: 'subscription', 
      label: t('account.tabs.subscription'),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M20 6L9 17l-5-5" />
        </svg>
      )
    },
    { 
      key: 'billing', 
      label: t('account.tabs.billing'),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="1" y="4" width="22" height="16" rx="2" ry="2" />
          <line x1="1" y1="10" x2="23" y2="10" />
        </svg>
      )
    },
    { 
      key: 'usage', 
      label: t('account.tabs.usage'),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
        </svg>
      )
    },
  ];

  return (
    <div className="rounded-xl" style={{ backgroundColor: 'transparent', padding: '0' }}>
      <div className={isMobile ? 'flex flex-col' : 'flex'} style={{ gap: '16px' }}>
        {/* Tabs column/row */}
        <div className={isMobile ? 'w-full' : 'flex-shrink-0'} style={{ width: isMobile ? '100%' : '220px' }}>
          <div className={isMobile ? 'flex flex-row' : 'flex flex-col'} style={{ gap: '6px', overflowX: isMobile ? 'auto' : 'visible' }}>
            {tabs.map(t => (
              <button
                key={t.key}
                className={`text-13 ${isMobile ? 'text-center' : 'text-left'} rounded-lg px-3 py-2 transition-colors flex items-center gap-2`}
                style={{
                  backgroundColor: active === t.key ? 'var(--white-10)' : 'transparent',
                  color: 'var(--text-heading)',
                  ...(isMobile ? { flex: '1 1 0', minWidth: 'fit-content' } : {})
                }}
                onMouseEnter={(e) => { if (active !== t.key) (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { if (active !== t.key) (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setActive(t.key)}
              >
                <span style={{ color: 'var(--text-heading)', display: 'flex', alignItems: 'center' }}>
                  {t.icon}
                </span>
                {t.label}
              </button>
            ))}
          </div>
        </div>

        {/* Divider */}
        {isMobile ? (
          <div style={{ width: '100%', height: '1px', backgroundColor: 'var(--overlay-line-90)' }} />
        ) : (
          <div style={{ width: '1px', backgroundColor: 'var(--overlay-line-90)' }} />
        )}

        {/* Content full width */}
        <div className="flex-1" style={{ paddingLeft: isMobile ? '0' : '16px' }}>
          {active === 'subscription' && <SubscriptionTab />}
          {active === 'billing' && <BillingTab />}
          {active === 'usage' && <UsageTab />}
        </div>
      </div>
    </div>
  );
};

export default Account;


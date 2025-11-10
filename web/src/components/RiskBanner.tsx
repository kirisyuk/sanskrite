import React from 'react';

interface RiskBannerProps {
  riskBanner: {
    visible: boolean;
    level: 'High' | 'Medium' | 'Low';
    rationale: string;
  } | null;
  onDismiss: () => void;
}

export const RiskBanner: React.FC<RiskBannerProps> = ({
  riskBanner,
  onDismiss,
}) => {
  const isHigh = riskBanner?.visible && riskBanner?.level === 'High';
  
  if (!isHigh) return null;

  return (
    <div
      className="rounded-lg border p-2 mb-2"
      style={{
        borderColor: 'rgba(220,20,60,0.5)',
        background: 'rgba(255,0,0,0.06)',
        color: 'var(--text-heading)',
        display: 'flex',
        alignItems: 'flex-start',
        gap: 8,
      }}
    >
      <div style={{ flex: 1 }}>
        <div
          className="text-12"
          style={{ color: '#cc3344', fontWeight: 600 }}
        >
          High-risk conversation
        </div>
        {riskBanner?.rationale && (
          <div
            className="text-[11px]"
            style={{
              color: 'var(--text-secondary-strong)',
            }}
          >
            {riskBanner.rationale}
          </div>
        )}
      </div>
      <button
        aria-label="Dismiss high-risk banner"
        title="Dismiss"
        className="rounded"
        onClick={onDismiss}
        style={{
          background: 'transparent',
          color: 'var(--text-secondary-strong)',
        }}
      >
        <svg className="w-4 h-4" viewBox="0 0 24 24">
          <path
            d="M6 6l12 12M18 6L6 18"
            stroke="currentColor"
            strokeWidth="2"
          />
        </svg>
      </button>
    </div>
  );
};


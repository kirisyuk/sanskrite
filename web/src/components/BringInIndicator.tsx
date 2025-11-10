import React from 'react';

interface BringInIndicatorProps {
  interaction: any;
}

export const BringInIndicator: React.FC<BringInIndicatorProps> = ({
  interaction,
}) => {
  return (
    <div
      key={interaction.id}
      className="my-1.5 flex justify-end"
    >
      <div
        className="px-3 py-1.5 rounded-lg text-[12px] inline-flex items-center gap-2"
        style={{
          background: 'var(--accent-research-10)',
          color: 'var(--accent-research)',
          border: '1px solid var(--accent-research-20)',
          maxWidth: '80%',
        }}
      >
        <svg
          className="w-3.5 h-3.5 flex-shrink-0"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
        >
          <path
            d="M12 7a3 3 0 1 1 0 6a3 3 0 0 1 0-6"
            strokeWidth="2"
          />
          <path
            d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6"
            strokeWidth="2"
            strokeLinecap="round"
          />
          <path
            d="M18 7v4M16 9h4"
            strokeWidth="2"
            strokeLinecap="round"
          />
        </svg>
        <span>{interaction.content}</span>
      </div>
    </div>
  );
};


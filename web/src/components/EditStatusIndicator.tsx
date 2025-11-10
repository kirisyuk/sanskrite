import React from 'react';

interface EditStatusIndicatorProps {
  editSeconds: number;
}

export const EditStatusIndicator: React.FC<EditStatusIndicatorProps> = ({
  editSeconds,
}) => {
  return (
    <div className="px-1 pb-2">
      <div
        className="text-xs flex items-center gap-2"
        style={{
          textAlign: 'left',
          color: 'var(--text-secondary-strong)',
        }}
      >
        <span
          className="animate-pulse"
          style={{
            width: '8px',
            height: '8px',
            borderRadius: '9999px',
            backgroundColor: 'var(--text-secondary-strong)',
          }}
        />
        <span>Applying edit… {editSeconds}s</span>
      </div>
    </div>
  );
};


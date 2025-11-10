import React from 'react';

interface FloatingActionButtonsProps {
  sideOpen: boolean;
  showSettings: boolean;
  showAccount: boolean;
  onOpenAccount: () => void;
  onOpenSettings: () => void;
}

export const FloatingActionButtons: React.FC<FloatingActionButtonsProps> = ({
  sideOpen,
  showSettings,
  showAccount,
  onOpenAccount,
  onOpenSettings,
}) => {
  if (sideOpen || showSettings || showAccount) {
    return null;
  }

  return (
    <>
      {/* Account button */}
      <button
        type="button"
        aria-label="Account"
        onClick={onOpenAccount}
        className="rounded-lg transition-colors flex items-center justify-center"
        style={{
          position: 'fixed',
          left: 8,
          bottom: 88,
          width: 32,
          height: 32,
          backgroundColor: 'transparent',
          color: 'var(--text-heading)',
          border: 'none',
          zIndex: 40,
        }}
        onMouseEnter={(e) => {
          (e.currentTarget as HTMLButtonElement).style.backgroundColor =
            'var(--white-10)';
        }}
        onMouseLeave={(e) => {
          (e.currentTarget as HTMLButtonElement).style.backgroundColor =
            'transparent';
        }}
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
          <circle cx="12" cy="7" r="4" />
        </svg>
      </button>
      {/* Settings button */}
      <button
        type="button"
        aria-label="Settings"
        onClick={onOpenSettings}
        className="rounded-lg transition-colors flex items-center justify-center"
        style={{
          position: 'fixed',
          left: 8,
          bottom: 48,
          width: 32,
          height: 32,
          backgroundColor: 'transparent',
          color: 'var(--text-heading)',
          border: 'none',
          zIndex: 40,
        }}
        onMouseEnter={(e) => {
          (e.currentTarget as HTMLButtonElement).style.backgroundColor =
            'var(--white-10)';
        }}
        onMouseLeave={(e) => {
          (e.currentTarget as HTMLButtonElement).style.backgroundColor =
            'transparent';
        }}
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      </button>
    </>
  );
};


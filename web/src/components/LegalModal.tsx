import React, { useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import TermsOfUsage from '../pages/TermsOfUsage';
import PrivacyNotice from '../pages/PrivacyNotice';

interface LegalModalProps {
  type: 'terms' | 'privacy';
  onClose: () => void;
}

export default function LegalModal({ type, onClose }: LegalModalProps) {
  // Close on Escape key
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    window.addEventListener('keydown', handleEscape);
    return () => window.removeEventListener('keydown', handleEscape);
  }, [onClose]);

  // Prevent body scroll when modal is open
  useEffect(() => {
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = '';
    };
  }, []);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ backgroundColor: 'rgba(0, 0, 0, 0.7)' }}
      onClick={onClose}
    >
      <div
        className="relative rounded-xl overflow-hidden"
        style={{
          width: '70%',
          maxWidth: '1200px',
          height: '85vh',
          backgroundColor: 'var(--bg-elevated)',
          border: '1px solid var(--overlay-line-90)',
          boxShadow: '0 12px 48px rgba(0, 0, 0, 0.4)'  /* Enhanced shadow for depth */
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div
          className="sticky top-0 z-10 flex items-center justify-between px-6 py-4"
          style={{
            backgroundColor: 'var(--bg-elevated)',
            borderBottom: '1px solid var(--overlay-line-90)',
          }}
        >
          <h2 className="text-18 font-semibold" style={{ color: 'var(--text-heading)' }}>
            {type === 'privacy' ? 'Privacy Notice' : 'Terms of Usage'}
          </h2>
          <button
            onClick={onClose}
            className="p-2 rounded-lg transition-colors"
            style={{ color: 'var(--text-secondary-strong)' }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
            }}
            aria-label="Close"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Content */}
        <div
          className="overflow-y-auto"
          style={{
            height: 'calc(85vh - 64px)',
          }}
        >
          {type === 'privacy' ? <PrivacyNotice /> : <TermsOfUsage />}
        </div>
      </div>
    </div>
  );
}


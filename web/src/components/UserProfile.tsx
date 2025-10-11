import React, { useState, useRef, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';

export const UserProfile: React.FC = () => {
  const { currentUser, signOut } = useAuth();
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const handleSignOut = async () => {
    try {
      await signOut();
    } catch (error) {
      console.error('Sign out error:', error);
    }
  };

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, []);

  if (!currentUser) return null;

  return (
    <div className="relative" ref={dropdownRef}>
      {/* User avatar */}
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="user-icon hover:opacity-80 focus:outline-none transition-opacity"
        title={currentUser.email || 'User Profile'}
      >
        {currentUser.email?.charAt(0).toUpperCase() || 'U'}
      </button>

      {/* Dropdown menu */}
      {isOpen && (
        <div
          className="absolute right-0 mt-2 w-64 rounded-lg shadow-lg z-50"
          style={{
            backgroundColor: 'var(--overlay-deep-95)',
            borderColor: 'var(--overlay-line-90)',
            border: '1px solid',
            boxShadow: '0 12px 28px rgba(0,0,0,0.35)',
          }}
        >
          <div className="p-3 border-b border-solid" style={{ borderColor: 'var(--overlay-line-90)' }}>
            <div className="flex items-center gap-2">
              <div className="user-icon w-8 h-8">{currentUser.email?.charAt(0).toUpperCase() || 'U'}</div>
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>
                  {currentUser.email || 'Unknown User'}
                </p>
              </div>
            </div>
          </div>

          <div className="p-3 border-b border-solid" style={{ borderColor: 'var(--overlay-line-90)' }}>
            <div className="space-y-2 text-xs">
              <div className="flex justify-between">
                <span style={{ color: 'var(--text-secondary)' }}>ID:</span>
                <span className="font-mono truncate ml-2" style={{ color: 'var(--text-primary)' }}>
                  {currentUser.uid.slice(-8)}
                </span>
              </div>
              <div className="flex justify-between">
                <span style={{ color: 'var(--text-secondary)' }}>Verified:</span>
                <span style={{ color: currentUser.emailVerified ? 'var(--accent-success)' : 'var(--accent-danger)' }}>
                  {currentUser.emailVerified ? 'Yes' : 'No'}
                </span>
              </div>
            </div>
          </div>

          <div className="p-2">
            <button onClick={handleSignOut} className="btn btn-secondary w-full text-xs justify-start">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"
                />
              </svg>
              Sign Out
            </button>
          </div>
        </div>
      )}
    </div>
  );
};


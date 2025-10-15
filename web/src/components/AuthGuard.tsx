import React, { useState } from 'react';
import { useAuth } from '../contexts/AuthContext';
import darkLogo from '../assets/sanskrite_logo_dark_theme.png';

interface AuthGuardProps {
  children: React.ReactNode;
}

type AuthView = 'login' | 'signup' | 'reset';

const PUBLIC_DOMAINS = [
  'gmail.com', 'yahoo.com', 'hotmail.com', 'outlook.com', 'aol.com',
  'icloud.com', 'mail.com', 'protonmail.com', 'yandex.ru', 'mail.ru',
  'live.com', 'msn.com', 'me.com', 'mac.com'
];

const AuthGuard: React.FC<AuthGuardProps> = ({ children }) => {
  const { user, loading, signIn, signUp, resetPassword } = useAuth();
  const [currentView, setCurrentView] = useState<AuthView>('login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [accountType, setAccountType] = useState<'personal' | 'corporate'>('personal');
  const [formLoading, setFormLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const validateEmail = (email: string): { isValid: boolean; error?: string } => {
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    if (!emailRegex.test(email)) {
      return { isValid: false, error: 'Please enter a valid email address' };
    }

    if (currentView === 'signup' && accountType === 'corporate') {
      const domain = email.split('@')[1]?.toLowerCase();
      if (PUBLIC_DOMAINS.includes(domain)) {
        return { 
          isValid: false, 
          error: 'Corporate accounts cannot use public email domains like Gmail, Yahoo, etc. Please use your company email address.' 
        };
      }
    }

    return { isValid: true };
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (formLoading) return;

    setError(null);
    setFormLoading(true);

    try {
      if (currentView === 'login') {
        await signIn(email, password);
      } else if (currentView === 'signup') {
        if (password !== confirmPassword) {
          throw new Error('Passwords do not match');
        }
        if (password.length < 6) {
          throw new Error('Password must be at least 6 characters long');
        }
        const emailValidation = validateEmail(email);
        if (!emailValidation.isValid) {
          throw new Error(emailValidation.error || 'Invalid email');
        }
        await signUp(email, password, accountType, displayName || undefined);
      } else if (currentView === 'reset') {
        const emailValidation = validateEmail(email);
        if (!emailValidation.isValid) {
          throw new Error(emailValidation.error || 'Invalid email');
        }
        await resetPassword(email);
        setError('Password reset email sent. Check your inbox.');
        setTimeout(() => setCurrentView('login'), 2000);
        return;
      }
    } catch (err: any) {
      setError(err.message || 'An error occurred');
    } finally {
      setFormLoading(false);
    }
  };

  const getTitle = () => {
    switch (currentView) {
      case 'login': return 'Sign In';
      case 'signup': return 'Create Account';
      case 'reset': return 'Reset Password';
    }
  };

  const getSubmitText = () => {
    if (formLoading) return 'Please wait...';
    switch (currentView) {
      case 'login': return 'Sign In';
      case 'signup': return 'Create Account';
      case 'reset': return 'Send Reset Email';
    }
  };

  // Loading state with splash
  if (loading) {
    return (
      <div className="auth-container">
        <div className="flex flex-col items-center">
          <img src={darkLogo} alt="Sanskrite" className="h-8 w-auto mb-4" />
          <div className="text-sm mb-4" style={{ color: 'var(--text-secondary)' }}>Signing you in…</div>
          <div className="w-48 h-1 rounded-full overflow-hidden" style={{ backgroundColor: 'var(--bg-elevated)' }}>
            <div className="h-full w-2/5 animate-pulse" style={{ backgroundColor: 'var(--accent-brand)' }}></div>
          </div>
        </div>
      </div>
    );
  }

  // Auth form
  if (!user) {
    return (
      <div className="auth-container">
        <div className="auth-card">
          <div className="text-center mb-8">
            <img src={darkLogo} alt="Sanskrite" className="auth-logo" />
            <h1 className="auth-title">{getTitle()}</h1>
            <p className="auth-subtitle">
              {currentView === 'login' && 'Welcome back to Sanskrite'}
              {currentView === 'signup' && 'Join Sanskrite to make informed decisions'}
              {currentView === 'reset' && 'Enter your email to reset your password'}
            </p>
          </div>

          {/* Tabs for switching between login/signup */}
          {currentView !== 'reset' && (
            <div className="auth-tabs">
              <button
                type="button"
                className={`auth-tab ${currentView === 'login' ? 'active' : ''}`}
                onClick={() => setCurrentView('login')}
              >
                Sign In
              </button>
              <button
                type="button"
                className={`auth-tab ${currentView === 'signup' ? 'active' : ''}`}
                onClick={() => setCurrentView('signup')}
              >
                Create Account
              </button>
            </div>
          )}

          {error && (
            <div className={`auth-error ${error.includes('sent') ? 'success' : ''}`}>
              {error}
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label htmlFor="email" className="block text-sm font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
                Email address
              </label>
              <input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                disabled={formLoading}
                className="auth-input"
                placeholder="Enter your email"
              />
            </div>

            {currentView === 'signup' && (
              <>
                <div>
                  <label htmlFor="displayName" className="block text-sm font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
                    Display Name (Optional)
                  </label>
                  <input
                    id="displayName"
                    type="text"
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    disabled={formLoading}
                    className="auth-input"
                    placeholder="Enter your display name"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
                    Account Type
                  </label>
                  <div className="flex space-x-4">
                    <label className="flex items-center cursor-pointer">
                      <input 
                        type="radio" 
                        name="accountType" 
                        value="personal" 
                        checked={accountType === 'personal'} 
                        onChange={(e) => setAccountType(e.target.value as 'personal' | 'corporate')} 
                        disabled={formLoading} 
                        className="h-4 w-4"
                        style={{ accentColor: 'var(--accent-brand)' }}
                      />
                      <span className="ml-2 text-sm" style={{ color: 'var(--text-primary)' }}>Personal</span>
                    </label>
                    <label className="flex items-center cursor-pointer">
                      <input 
                        type="radio" 
                        name="accountType" 
                        value="corporate" 
                        checked={accountType === 'corporate'} 
                        onChange={(e) => setAccountType(e.target.value as 'personal' | 'corporate')} 
                        disabled={formLoading} 
                        className="h-4 w-4"
                        style={{ accentColor: 'var(--accent-brand)' }}
                      />
                      <span className="ml-2 text-sm" style={{ color: 'var(--text-primary)' }}>Corporate</span>
                    </label>
                  </div>
                  <div className="mt-2 text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                    {accountType === 'corporate' ? (
                      <div>
                        <p>Use your company email address (public domains like Gmail are not allowed)</p>
                        <p className="mt-1 font-medium">
                          Corporate accounts share data within the domain:{' '}
                          <span style={{ color: 'var(--accent-brand)' }}>
                            {email.includes('@') ? email.split('@')[1] : '[domain]'}
                          </span>
                        </p>
                      </div>
                    ) : (
                      <p>Personal accounts use your email as a unique workspace</p>
                    )}
                  </div>
                </div>
              </>
            )}

            {currentView !== 'reset' && (
              <div>
                <label htmlFor="password" className="block text-sm font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
                  Password
                </label>
                <input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  disabled={formLoading}
                  className="auth-input"
                  placeholder="Enter your password"
                />
              </div>
            )}

            {currentView === 'signup' && (
              <div>
                <label htmlFor="confirmPassword" className="block text-sm font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
                  Confirm Password
                </label>
                <input
                  id="confirmPassword"
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  required
                  disabled={formLoading}
                  className="auth-input"
                  placeholder="Confirm your password"
                />
              </div>
            )}

            <button
              type="submit"
              disabled={formLoading}
              className="auth-button"
            >
              {getSubmitText()}
            </button>

            <div className="auth-divider">or</div>

            <div className="text-center text-sm">
              {currentView === 'login' ? (
                <button
                  type="button"
                  onClick={() => setCurrentView('reset')}
                  className="auth-link"
                  disabled={formLoading}
                >
                  Forgot your password?
                </button>
              ) : currentView === 'reset' ? (
                <button
                  type="button"
                  onClick={() => setCurrentView('login')}
                  className="auth-link"
                  disabled={formLoading}
                >
                  Back to Sign In
                </button>
              ) : null}
            </div>
          </form>
        </div>
      </div>
    );
  }

  // Authenticated, render children
  return <>{children}</>;
};

export default AuthGuard;

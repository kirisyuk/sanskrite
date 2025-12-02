import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../contexts/AuthContext';
import { AuthLoading } from './AuthLoading';
import lightLogo from '../assets/hatl-light-theme.png';
import darkLogo from '../assets/hatl-dark-theme.png';
import { ThemeContext } from './ThemeProvider';

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
  const { t } = useTranslation();
  const { user, loading, signIn, signUp, resetPassword, signInWithGoogle } = useAuth();
  const { theme } = React.useContext(ThemeContext);
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
      return { isValid: false, error: t('unfold.auth.enterValidEmail') };
    }

    if (currentView === 'signup' && accountType === 'corporate') {
      const domain = email.split('@')[1]?.toLowerCase();
      if (PUBLIC_DOMAINS.includes(domain)) {
        return { 
          isValid: false, 
          error: t('unfold.auth.corporateEmailRestriction')
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
          throw new Error(t('unfold.auth.passwordsDoNotMatch'));
        }
        if (password.length < 6) {
          throw new Error(t('unfold.auth.passwordMinLength'));
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
        setError(t('unfold.auth.passwordResetSent'));
        setTimeout(() => setCurrentView('login'), 2000);
        return;
      }
    } catch (err: any) {
      setError(err.message || t('unfold.auth.anErrorOccurred'));
    } finally {
      setFormLoading(false);
    }
  };

  const getTitle = () => {
    switch (currentView) {
      case 'login': return t('unfold.auth.signIn');
      case 'signup': return t('unfold.auth.createAccount');
      case 'reset': return t('unfold.auth.resetPassword');
    }
  };

  const getSubmitText = () => {
    if (formLoading) return t('unfold.auth.pleaseWait');
    switch (currentView) {
      case 'login': return t('unfold.auth.signIn');
      case 'signup': return t('unfold.auth.createAccount');
      case 'reset': return t('unfold.auth.sendResetEmail');
    }
  };

  // Loading state with splash
  if (loading) {
    return <AuthLoading />;
  }

  // Auth form
  if (!user) {
  return (
      <>
        <div className="auth-container">
        <div className="auth-card">
          <div className="text-center mb-8">
            <img src={theme === 'dark' ? darkLogo : lightLogo} alt="hatl.one" className="auth-logo" />
            <h1 className="auth-title">{getTitle()}</h1>
            <p className="auth-subtitle">
              {currentView === 'login' && ''}
              {currentView === 'signup' && ''}
              {currentView === 'reset' && t('unfold.auth.resetPasswordMessage')}
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
                {t('unfold.auth.signIn')}
              </button>
              <button
                type="button"
                className={`auth-tab ${currentView === 'signup' ? 'active' : ''}`}
                onClick={() => setCurrentView('signup')}
              >
                {t('unfold.auth.createAccount')}
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
                {t('unfold.auth.emailAddress')}
              </label>
              <input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                disabled={formLoading}
                className="auth-input"
                placeholder={t('unfold.auth.enterEmail')}
              />
            </div>

            {currentView === 'signup' && (
              <>
                <div>
                  <label htmlFor="displayName" className="block text-sm font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
                    {t('unfold.auth.displayName')}
                  </label>
                  <input
                    id="displayName"
                    type="text"
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    disabled={formLoading}
                    className="auth-input"
                    placeholder={t('unfold.auth.enterDisplayName')}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
                    {t('unfold.auth.accountType')}
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
                      <span className="ml-2 text-sm" style={{ color: 'var(--text-primary)' }}>{t('unfold.auth.personal')}</span>
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
                      <span className="ml-2 text-sm" style={{ color: 'var(--text-primary)' }}>{t('unfold.auth.corporate')}</span>
                    </label>
                  </div>
                  <div className="mt-2 text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                    {accountType === 'corporate' ? (
                      <div>
                        <p>{t('unfold.auth.useCompanyEmail')}</p>
                        <p className="mt-1 font-medium">
                          {t('unfold.auth.corporateAccountsShare')}{' '}
                          <span style={{ color: 'var(--accent-brand)' }}>
                            {email.includes('@') ? email.split('@')[1] : '[domain]'}
                          </span>
                        </p>
                      </div>
                    ) : (
                      <p>{t('unfold.auth.personalAccountsWorkspace')}</p>
                    )}
                  </div>
                </div>
              </>
            )}

            {currentView !== 'reset' && (
              <div>
                <label htmlFor="password" className="block text-sm font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
                  {t('unfold.auth.password')}
                </label>
                <input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  disabled={formLoading}
                  className="auth-input"
                  placeholder={t('unfold.auth.enterPassword')}
                />
              </div>
            )}

            {currentView === 'signup' && (
              <div>
                <label htmlFor="confirmPassword" className="block text-sm font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
                  {t('unfold.auth.confirmPassword')}
                </label>
                <input
                  id="confirmPassword"
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  required
                  disabled={formLoading}
                  className="auth-input"
                  placeholder={t('unfold.auth.confirmPasswordPlaceholder')}
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

            {currentView !== 'reset' && (
              <>
                <div className="auth-divider">{t('unfold.auth.or')}</div>

                <button
                  type="button"
                  onClick={async () => {
                    setError(null);
                    setFormLoading(true);
                    try {
                      await signInWithGoogle();
                    } catch (err: any) {
                      setError(err.message || t('unfold.auth.failedToSignInWithGoogle'));
                    } finally {
                      setFormLoading(false);
                    }
                  }}
                  disabled={formLoading}
                  className="auth-button-google"
                >
                  <svg className="w-5 h-5 mr-2" viewBox="0 0 24 24">
                    <path
                      fill="currentColor"
                      d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"
                    />
                    <path
                      fill="currentColor"
                      d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
                    />
                    <path
                      fill="currentColor"
                      d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
                    />
                    <path
                      fill="currentColor"
                      d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
                    />
                  </svg>
                  {t('unfold.auth.signInWithGoogle')}
                </button>
              </>
            )}

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
      </>
    );
  }

  // Authenticated, render children
  return <>{children}</>;
};

export default AuthGuard;

import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ThemeContext } from '../components/ThemeProvider';

export default function Homepage() {
  const navigate = useNavigate();
  const { t } = useTranslation();
  const { theme } = React.useContext(ThemeContext);
  const [showJoin, setShowJoin] = React.useState(false);
  const [showSuccess, setShowSuccess] = React.useState(false);
  const [email, setEmail] = React.useState('');
  const [note, setNote] = React.useState('');
  const [sending, setSending] = React.useState(false);

  // Handle ESC key to close modals
  React.useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (showSuccess) {
          setShowSuccess(false);
        } else if (showJoin) {
          setShowJoin(false);
        }
      }
    };
    window.addEventListener('keydown', handleEsc);
    return () => window.removeEventListener('keydown', handleEsc);
  }, [showJoin, showSuccess]);

  return (
    <div
      className="min-h-screen"
      style={{
        backgroundColor: 'var(--bg-body)',
      }}
    >
      <div className="max-w-6xl mx-auto px-4 min-h-screen flex items-center">
        <div className="w-full flex gap-8" style={{ alignItems: 'flex-start' }}>
          {/* Left column */}
          <div style={{ flex: '0 0 35%', marginTop: '24px', display: 'flex', justifyContent: 'flex-end' }}>
            <div
              style={{
                backgroundColor: theme === 'dark' ? '#FFFFFF' : '#0A0A0F',
                color: theme === 'dark' ? '#0B0D12' : '#FFFFFF',
                padding: '8px 12px',
                fontSize: 'clamp(16px, 2vw, 22px)',
                fontWeight: 500,
                display: 'inline-block',
              }}
            >
              Human Above The Loop
            </div>
          </div>

          {/* Right column */}
          <div style={{ flex: '0 0 65%', width: 'fit-content', maxWidth: '100%' }}>
            <div
              className="font-semibold"
              style={{ color: 'var(--text-primary)', fontSize: 'clamp(52px, 8vw, 96px)', lineHeight: 1.02, marginBottom: '32px' }}
            >
              <span style={{ display: 'block' }}>A Whole</span>
              <span style={{ display: 'block' }}>Different</span>
              <span style={{ display: 'block' }}>Beast</span>
            </div>
            {/* Copy block */}
            <div className="mt-4" style={{ backgroundColor: 'var(--bg-elevated)', padding: '14px 16px', borderRadius: 8, marginBottom: '32px', width: 'fit-content' }}>
              <div className="space-y-2">
                <div className="text-13" style={{ color: 'var(--text-primary)', fontWeight: 600 }}>{t('homepage.reclaimMind')}</div>
                <div className="text-13" style={{ color: 'var(--text-secondary)' }}>{t('homepage.tagline')}</div>
                <div className="text-13" style={{ color: 'var(--text-secondary)' }}>{t('homepage.description')}</div>
                <div className="text-13" style={{ color: 'var(--text-secondary)' }}>{t('homepage.description2')}</div>
                <div className="text-13" style={{ color: 'var(--text-primary)', fontWeight: 600 }}>{t('homepage.beta')}</div>
              </div>
            </div>

            {/* CTA Row */}
            <div className="mt-4 flex items-center gap-3">
              <button
                className="rounded text-13"
                style={{ padding: '10px 16px', backgroundColor: 'transparent', color: 'var(--text-primary)', border: '1px solid var(--border-subtle)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setShowJoin(true)}
              >
                {t('homepage.requestAccess')}
              </button>
              <button
                className="rounded text-13"
                style={{ padding: '10px 16px', backgroundColor: 'transparent', color: 'var(--text-primary)', border: '1px solid var(--border-subtle)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => navigate('/?app=unfold')}
              >
                {t('homepage.signIn')}
              </button>
            </div>
          </div>
        </div>
      </div>
      {showJoin && (
        <div className="fixed inset-0 z-50 flex items-center justify-center" role="dialog" aria-modal="true">
          <div className="absolute inset-0" style={{ backgroundColor: 'var(--overlay-40)' }} onClick={() => setShowJoin(false)} />
          <div className="relative auth-card" style={{ width: '100%', maxWidth: 440 }}>
            <div>
              <div className="auth-title" style={{ marginBottom: 16 }}>{t('homepage.joinWaitlist')}</div>
              <input value={email} onChange={(e) => setEmail(e.target.value)} className="auth-input" placeholder="you@example.com" />
              <div className="text-12" style={{ color: 'var(--text-secondary)', marginTop: '4px', marginBottom: '16px' }}>{t('homepage.emailPrivacyNote')}</div>
              <textarea value={note} onChange={(e) => setNote(e.target.value)} rows={3} className="auth-input" placeholder={t('homepage.contextPlaceholder')} />
              <div className="mt-2 flex items-center justify-end gap-2">
                <button className="btn btn-secondary" onClick={() => setShowJoin(false)}>{t('homepage.cancel')}</button>
                <button className="btn btn-primary" disabled={sending || !email} onClick={async () => {
                  try {
                    setSending(true);
                    await (await import('../lib/api')).api.waitlistJoin({ email, note: note.trim() || undefined });
                    setShowJoin(false);
                    setShowSuccess(true);
                    setEmail(''); setNote('');
                  } catch (e) {
                    // Error handling can be added here if needed
                  } finally { setSending(false); }
                }}>{sending ? t('homepage.sending') : t('homepage.send')}</button>
              </div>
            </div>
          </div>
        </div>
      )}
      {showSuccess && (
        <div className="fixed inset-0 z-50 flex items-center justify-center" role="dialog" aria-modal="true">
          <div className="absolute inset-0" style={{ backgroundColor: 'var(--overlay-40)' }} onClick={() => setShowSuccess(false)} />
          <div className="relative auth-card" style={{ width: '100%', maxWidth: 440 }}>
            <div>
              <div className="auth-title" style={{ marginBottom: 16, whiteSpace: 'pre-line' }}>{t('homepage.submitted')}</div>
              <div className="mt-2 flex items-center justify-end">
                <button className="btn btn-primary" onClick={() => setShowSuccess(false)}>{t('homepage.close')}</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}


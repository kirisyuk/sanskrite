import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

export default function Homepage() {
  const navigate = useNavigate();
  const { t } = useTranslation();
  const [showJoin, setShowJoin] = React.useState(false);
  const [email, setEmail] = React.useState('');
  const [note, setNote] = React.useState('');
  const [sending, setSending] = React.useState(false);
  const [ok, setOk] = React.useState<string | null>(null);
  return (
    <div
      className="min-h-screen"
      style={{
        backgroundColor: 'var(--bg-body)',
      }}
    >
      <div className="max-w-6xl mx-auto px-4 min-h-screen flex items-center">
        <div className="w-full" style={{ width: '40%', maxWidth: '40%', flexBasis: '40%', overflow: 'hidden' }}>
          <div
            className="font-semibold"
            style={{ color: 'var(--text-primary)', fontSize: 'clamp(52px, 8vw, 96px)', lineHeight: 1.02, marginBottom: '32px' }}
          >
            <span style={{ display: 'block' }}>Human</span>
            <span style={{ display: 'block' }}>Above</span>
            <span style={{ display: 'block' }}>The Loop</span>
          </div>
          {/* Copy block */}
          <div className="mt-4" style={{ backgroundColor: 'var(--bg-elevated)', padding: '14px 16px', borderRadius: 8, marginBottom: '32px' }}>
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
      {showJoin && (
        <div className="fixed inset-0 z-50 flex items-center justify-center" role="dialog" aria-modal="true">
          <div className="absolute inset-0" style={{ backgroundColor: 'var(--overlay-40)' }} onClick={() => setShowJoin(false)} />
          <div className="relative auth-card" style={{ width: '100%', maxWidth: 440 }}>
            <div>
              <div className="auth-title" style={{ marginBottom: 16 }}>{t('homepage.joinWaitlist')}</div>
              <input value={email} onChange={(e) => setEmail(e.target.value)} className="auth-input" placeholder="you@example.com" />
              <textarea value={note} onChange={(e) => setNote(e.target.value)} rows={3} className="auth-input" placeholder={t('homepage.contextPlaceholder')} />
              {ok && <div className="auth-subtitle" style={{ marginTop: 0 }}>{ok}</div>}
              <div className="mt-2 flex items-center justify-end gap-2">
                <button className="btn btn-secondary" onClick={() => setShowJoin(false)}>{t('homepage.cancel')}</button>
                <button className="btn btn-primary" disabled={sending || !email} onClick={async () => {
                  try {
                    setSending(true);
                    await (await import('../lib/api')).api.waitlistJoin({ email, note: note.trim() || undefined });
                    setOk(t('homepage.submitted'));
                    setEmail(''); setNote('');
                  } catch (e) {
                    setOk(t('homepage.failed'));
                  } finally { setSending(false); }
                }}>{sending ? t('homepage.sending') : t('homepage.send')}</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}


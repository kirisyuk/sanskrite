import React from 'react';
import { useNavigate } from 'react-router-dom';
import bg from '../assets/hatl-background.webp';

export default function Homepage() {
  const navigate = useNavigate();
  const [showJoin, setShowJoin] = React.useState(false);
  const [email, setEmail] = React.useState('');
  const [note, setNote] = React.useState('');
  const [sending, setSending] = React.useState(false);
  const [ok, setOk] = React.useState<string | null>(null);
  return (
    <div
      className="min-h-screen"
      style={{
        backgroundColor: 'var(--bg-base)',
        backgroundImage: `linear-gradient(90deg, rgba(0,0,0,0.75) 0%, rgba(0,0,0,0.55) 35%, rgba(0,0,0,0.25) 58%, rgba(0,0,0,0.05) 72%, rgba(0,0,0,0) 82%), url(${bg})`,
        backgroundSize: 'cover',
        backgroundRepeat: 'no-repeat',
        backgroundPosition: '85% center',
      }}
    >
      <div className="max-w-6xl mx-auto px-4 min-h-screen flex items-center">
        <div className="w-full" style={{ width: '40%', maxWidth: '40%', flexBasis: '40%', overflow: 'hidden' }}>
          <div
            className="font-semibold"
            style={{ color: '#ffffff', fontSize: 'clamp(52px, 8vw, 96px)', lineHeight: 1.02, textShadow: '0 6px 24px rgba(0,0,0,0.45)', marginBottom: '32px' }}
          >
            <span style={{ display: 'block' }}>Human</span>
            <span style={{ display: 'block' }}>Above</span>
            <span style={{ display: 'block' }}>The Loop</span>
          </div>
          {/* Copy block on dark overlay */}
          <div className="mt-4" style={{ backgroundColor: 'rgba(0,0,0,0.3)', padding: '14px 16px', borderRadius: 8, marginBottom: '32px' }}>
            <div className="space-y-2">
              <div className="text-13" style={{ color: 'rgba(255,255,255,0.95)', fontWeight: 600 }}>Reclaim your mind.</div>
              <div className="text-13" style={{ color: 'rgba(255,255,255,0.92)' }}>HATL is not another AI tool — it’s a mirror for your thinking.</div>
              <div className="text-13" style={{ color: 'rgba(255,255,255,0.9)' }}>A place where reflection, structure, and intuition coexist.</div>
              <div className="text-13" style={{ color: 'rgba(255,255,255,0.88)' }}>Enter the space above the loop — where human insight leads, and machines assist.</div>
              <div className="text-13" style={{ color: 'rgba(255,255,255,0.9)', fontWeight: 600 }}>Private beta. Invitation only.</div>
            </div>
          </div>

          {/* CTA Row */}
          <div className="mt-4 flex items-center gap-3">
            <button
              className="rounded text-13"
              style={{ padding: '10px 16px', backgroundColor: 'transparent', color: '#ffffff' }}
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'rgba(255,255,255,0.12)'; }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
              onClick={() => setShowJoin(true)}
            >
              Request access
            </button>
            <button
              className="rounded text-13"
              style={{ padding: '10px 16px', backgroundColor: 'transparent', color: '#ffffff' }}
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'rgba(255,255,255,0.12)'; }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
              onClick={() => navigate('/?app=unfold')}
            >
              Sign in
            </button>
          </div>
        </div>
      </div>
      {showJoin && (
        <div className="fixed inset-0 z-50 flex items-center justify-center" role="dialog" aria-modal="true">
          <div className="absolute inset-0" style={{ backgroundColor: 'rgba(0,0,0,0.4)' }} onClick={() => setShowJoin(false)} />
          <div className="relative auth-card" style={{ width: '100%', maxWidth: 440 }}>
            <div>
              <div className="auth-title" style={{ marginBottom: 16 }}>Request access</div>
              <input value={email} onChange={(e) => setEmail(e.target.value)} className="auth-input" placeholder="you@example.com" />
              <textarea value={note} onChange={(e) => setNote(e.target.value)} rows={3} className="auth-input" placeholder="Context or affiliation" />
              {ok && <div className="auth-subtitle" style={{ marginTop: 0 }}>{ok}</div>}
              <div className="mt-2 flex items-center justify-end gap-2">
                <button className="btn btn-secondary" onClick={() => setShowJoin(false)}>Cancel</button>
                <button className="btn btn-primary" disabled={sending || !email} onClick={async () => {
                  try {
                    setSending(true);
                    await (await import('../lib/api')).api.waitlistJoin({ email, note: note.trim() || undefined });
                    setOk('Submitted. We\'ll notify you.');
                    setEmail(''); setNote('');
                  } catch (e) {
                    setOk('Failed. Try later.');
                  } finally { setSending(false); }
                }}>{sending ? 'Sending…' : 'Send'}</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}


import React from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { api } from '../lib/api';
import type { FragmentProfile } from '../components/FragmentProfileModal';
import { ProgramProfileDashboard } from '../components/ProgramProfileDashboard';

export default function HorizonProgramProfilePrintPage() {
  const { fragmentId } = useParams<{ fragmentId: string }>();
  const location = useLocation();
  const navigate = useNavigate();

  const search = React.useMemo(() => new URLSearchParams(location.search), [location.search]);
  const label = String(search.get('label') || '').trim();
  const autoPrint = search.get('autoprint') === '1';

  const [profile, setProfile] = React.useState<FragmentProfile | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [creating, setCreating] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // Force light theme for PDF readability (restore on unmount).
  React.useEffect(() => {
    const prev = document.documentElement.getAttribute('data-theme');
    document.documentElement.setAttribute('data-theme', 'light');
    return () => {
      if (prev) document.documentElement.setAttribute('data-theme', prev);
      else document.documentElement.removeAttribute('data-theme');
    };
  }, []);

  React.useEffect(() => {
    let cancelled = false;
    async function load() {
      if (!fragmentId) {
        setError('Missing fragmentId');
        setLoading(false);
        return;
      }
      try {
        setLoading(true);
        setError(null);
        const existing = (await api.getFragmentProfile(fragmentId)) as FragmentProfile | null;
        if (cancelled) return;
        if (existing) {
          setProfile(existing);
          setLoading(false);
          return;
        }
        setCreating(true);
        await api.refreshFragmentProfile(fragmentId, true);
        const createdProfile = (await api.getFragmentProfile(fragmentId)) as FragmentProfile | null;
        if (cancelled) return;
        setProfile(createdProfile);
        setLoading(false);
      } catch (e: any) {
        if (cancelled) return;
        const msg = e?.response?.data?.error || e?.message || 'Failed to load profile';
        setError(String(msg));
        setLoading(false);
      } finally {
        if (!cancelled) setCreating(false);
      }
    }
    load();
    return () => {
      cancelled = true;
    };
  }, [fragmentId]);

  React.useEffect(() => {
    if (!autoPrint) return;
    if (loading) return;
    if (!profile) return;
    // Give the layout a moment to paint.
    const t = window.setTimeout(() => window.print(), 350);
    return () => window.clearTimeout(t);
  }, [autoPrint, loading, profile]);

  const title = label || (fragmentId ? `Fragment ${fragmentId.slice(0, 8)}` : 'Program profile');

  return (
    <div className="hzn-print">
      <div
        className="no-print"
        style={{
          padding: '14px 18px',
          borderBottom: '1px solid rgba(15,23,42,0.08)',
          background: 'rgba(255,255,255,0.85)',
          backdropFilter: 'blur(6px)',
          position: 'sticky',
          top: 0,
          zIndex: 50,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
          <div style={{ fontSize: 13, fontWeight: 900, color: 'var(--text-heading)' }}>
            Export PDF · {title}
          </div>
          <div style={{ display: 'flex', gap: 10 }}>
            <button
              type="button"
              onClick={() => navigate(-1)}
              style={{
                height: 34,
                padding: '0 12px',
                borderRadius: 10,
                border: '1px solid rgba(15,23,42,0.14)',
                background: 'rgba(255,255,255,0.9)',
                color: 'rgba(15,23,42,0.86)',
                fontSize: 12,
                fontWeight: 800,
                cursor: 'pointer',
              }}
            >
              Back
            </button>
            <button
              type="button"
              onClick={() => window.print()}
              style={{
                height: 34,
                padding: '0 12px',
                borderRadius: 10,
                border: '1px solid rgba(15,23,42,0.14)',
                background: 'rgba(15,23,42,0.92)',
                color: 'white',
                fontSize: 12,
                fontWeight: 900,
                cursor: 'pointer',
              }}
            >
              Print / Save PDF
            </button>
          </div>
        </div>
      </div>

      <div style={{ maxWidth: 980, margin: '0 auto', padding: '18px 18px 24px' }}>
        <div className="print-only" style={{ display: 'none' }}>
          <div style={{ fontSize: 18, fontWeight: 950 as any, color: 'rgba(15,23,42,0.92)', marginBottom: 6 }}>{title}</div>
          {fragmentId && <div style={{ fontSize: 12, color: 'rgba(15,23,42,0.55)' }}>Fragment: {fragmentId}</div>}
        </div>

        {loading && (
          <div style={{ padding: '80px 20px', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 16 }}>
            <style>{`
              @keyframes horizon-profile-loading-dot {
                0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
                40% { transform: translateY(-8px); opacity: 1; }
              }
            `}</style>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              {[0, 1, 2].map((i) => (
                <div
                  key={i}
                  style={{
                    width: 12,
                    height: 12,
                    borderRadius: '50%',
                    background: '#3B82F6',
                    animation: 'horizon-profile-loading-dot 1.1s infinite ease-in-out',
                    animationDelay: `${i * 0.14}s`,
                  }}
                />
              ))}
            </div>
            <div style={{ fontSize: 13, fontWeight: 700, color: 'rgba(15,23,42,0.72)' }}>
              {creating ? 'Generating profile…' : 'Loading profile…'}
            </div>
          </div>
        )}

        {error && !loading && (
          <div style={{ padding: '60px 20px', textAlign: 'center', color: '#ef4444', fontSize: 13 }}>
            {error}
          </div>
        )}

        {!loading && !error && (
          <ProgramProfileDashboard profile={profile} />
        )}
      </div>
    </div>
  );
}

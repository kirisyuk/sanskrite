import React from 'react';
import { ThemeContext } from './ThemeProvider';

export interface FragmentProfile {
  id: string;
  fragmentId: string;
  fragmentType: string;
  canonicalPath: string;
  version: number;
  capturedAt: string;
  sections: Record<string, {
    key: string;
    title: string;
    content: string;
    structured?: Record<string, any>;
    datasets?: Array<{
      id: string;
      title: string;
      type: 'timeseries' | 'bar' | 'pie' | 'table';
      description?: string;
      unit?: string;
      currency?: string;
      series?: Array<{
        name?: string;
        points: Array<{
          x: string;
          y: number;
          meta?: Record<string, any>;
        }>;
      }>;
      table?: {
        columns: Array<{ id: string; title: string; format?: string }>;
        rows: Array<Record<string, any>>;
      } | null;
      sources?: string[];
      confidence?: number;
    }>;
    sources?: string[];
    confidence: number;
    order: number;
  }>;
  dashboard?: {
    thesis?: string;
    kpis?: Array<{
      id: string;
      label: string;
      value: string;
      subtext?: string;
      trendValue?: string;
      severity?: string;
      confidence?: number;
    }>;
    scenarios?: Array<{
      id: string;
      name: string;
      probability: number;
      assumptions?: string[];
      metrics?: Record<string, string>;
      triggers?: string[];
    }>;
    watchlist?: Array<{
      id: string;
      indicator: string;
      current?: string;
      threshold?: string;
      signal?: string;
      frequency?: string;
      sources?: string[];
      confidence?: number;
    }>;
    charts?: FragmentProfile['sections'][string]['datasets'];
    tables?: FragmentProfile['sections'][string]['datasets'];
    notes?: string;
    confidence?: number;
  };
  generation: {
    model?: string;
    groundingUsed: boolean;
    promptTokens?: number;
    outputTokens?: number;
    durationMs?: number;
    graphContext?: {
      neighborCount: number;
      edgeCount: number;
      edgeTypes?: string[];
      topNeighbors?: string[];
      fragmentLabels?: string[];
    };
    generatedAt: string;
    schemaVersion: string;
    generationNotes?: string;
  };
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface FragmentProfileModalProps {
  open: boolean;
  onClose: () => void;
  fragmentId: string;
  fragmentType: string;
  canonicalPath?: string;
  label?: string;
  children: (profile: FragmentProfile | null, loading: boolean, error: string | null) => React.ReactNode;
}

/**
 * Base modal component for displaying fragment profiles.
 * Handles loading, error states, and modal UI structure.
 * Children component receives profile data and renders type-specific content.
 */
export const FragmentProfileModal: React.FC<FragmentProfileModalProps> = ({
  open,
  onClose,
  fragmentId,
  fragmentType,
  canonicalPath,
  label,
  children,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [profile, setProfile] = React.useState<FragmentProfile | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [creating, setCreating] = React.useState(false);
  const [refreshing, setRefreshing] = React.useState(false);

  // Load or create profile when modal opens
  React.useEffect(() => {
    if (!open || !fragmentId) {
      setProfile(null);
      setError(null);
      return;
    }

    const loadProfile = async () => {
      setLoading(true);
      setError(null);

      try {
        const { api } = await import('../lib/api');
        
        // Try to get existing profile
        const existingProfile = await api.getFragmentProfile(fragmentId);
        
        if (existingProfile) {
          setProfile(existingProfile);
          setLoading(false);
          return;
        }

        // Profile doesn't exist - create it
        setCreating(true);
        setLoading(false); // Switch to creating state
        
        const result = await api.refreshFragmentProfile(fragmentId, false);
        
        // Reload the profile after creation
        const newProfile = await api.getFragmentProfile(fragmentId);
        setProfile(newProfile);
        setCreating(false);
      } catch (err: any) {
        const respData = err?.response?.data;
        const errorMessage =
          (respData && typeof respData === 'object' && respData.error) ? String(respData.error) :
          (typeof respData === 'string' && respData.trim() ? respData : '') ||
          err?.message ||
          'Failed to load profile';
        setError(errorMessage);
        setLoading(false);
        setCreating(false);
      }
    };

    loadProfile();
  }, [open, fragmentId]);

  const handleRefresh = React.useCallback(async () => {
    if (!fragmentId) return;
    setError(null);
    setRefreshing(true);
    setProfile(null);
    try {
      const { api } = await import('../lib/api');
      // Best-effort: delete existing profile(s) first.
      try {
        await api.deleteFragmentProfile(fragmentId);
      } catch (e: any) {
        // If deletion fails due to auth or transient issues, surface it; otherwise proceed.
        // (We want explicit visibility rather than silently hiding problems.)
        const status = e?.response?.status;
        if (status && status !== 404) {
          throw e;
        }
      }
      // Regenerate (force=true) and reload.
      await api.refreshFragmentProfile(fragmentId, true);
      const newProfile = await api.getFragmentProfile(fragmentId);
      setProfile(newProfile);
    } catch (err: any) {
      const respData = err?.response?.data;
      const errorMessage =
        (respData && typeof respData === 'object' && respData.error) ? String(respData.error) :
        (typeof respData === 'string' && respData.trim() ? respData : '') ||
        err?.message ||
        'Failed to refresh profile';
      setError(errorMessage);
    } finally {
      setRefreshing(false);
    }
  }, [fragmentId]);

  // Close on Escape key
  React.useEffect(() => {
    if (!open) return;

    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !loading && !creating && !refreshing) {
        onClose();
      }
    };

    window.addEventListener('keydown', handleEscape);
    return () => window.removeEventListener('keydown', handleEscape);
  }, [open, loading, creating, refreshing, onClose]);

  if (!open) return null;

  const isLoading = loading || creating || refreshing;

  return (
    <>
      {/* Backdrop */}
      <div
        onClick={(e) => {
          if (e.target === e.currentTarget && !isLoading) {
            onClose();
          }
        }}
        style={{
          position: 'fixed',
          inset: 0,
          zIndex: 1950,
          background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(15,23,42,0.12)',
          backdropFilter: 'blur(10px)',
        }}
      />
      
      {/* Modal */}
      <div
        style={{
          position: 'fixed',
          left: '50%',
          top: '50%',
          transform: 'translate(-50%, -50%)',
          width: 720,
          maxWidth: 'calc(100vw - 24px)',
          // IMPORTANT: give the modal a definite height so the content area doesn't collapse
          // when children renders null (e.g., during loading/refresh overlays).
          height: 'min(720px, calc(100vh - 24px))',
          maxHeight: 'calc(100vh - 24px)',
          minHeight: 360,
          zIndex: 2000,
          borderRadius: 16,
          border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.12)' : 'rgba(15,23,42,0.10)'}`,
          background: theme === 'dark' ? 'rgba(10,10,12,0.70)' : 'rgba(255,255,255,0.72)',
          backdropFilter: 'blur(12px)',
          boxShadow: theme === 'dark' ? '0 22px 70px rgba(0,0,0,0.60)' : '0 22px 70px rgba(0,0,0,0.18)',
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
        }}
        onMouseDown={(e) => {
          e.stopPropagation();
        }}
      >
        {/* Header with close button */}
        <div
          style={{
            position: 'relative',
            padding: '16px 20px',
            borderBottom: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            flexShrink: 0,
          }}
        >
          <div style={{ minWidth: 0, flex: 1 }}>
            <div
              style={{
                fontSize: 16,
                fontWeight: 800,
                color: 'var(--text-heading)',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {label || canonicalPath || `Fragment ${fragmentId.slice(0, 8)}`}
            </div>
            {canonicalPath && (
              <div
                style={{
                  marginTop: 4,
                  fontSize: 12,
                  color: 'var(--text-secondary-strong)',
                  fontFamily: 'monospace',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {canonicalPath}
              </div>
            )}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <button
              type="button"
              onClick={handleRefresh}
              disabled={isLoading}
              title="Refresh profile"
              style={{
                height: 32,
                padding: '0 12px',
                borderRadius: 10,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}`,
                background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.02)',
                color: 'var(--text-heading)',
                cursor: isLoading ? 'not-allowed' : 'pointer',
                opacity: isLoading ? 0.4 : 0.85,
                fontSize: 12,
                fontWeight: 800,
                lineHeight: 1,
                flexShrink: 0,
              }}
              onMouseEnter={(e) => {
                if (!isLoading) e.currentTarget.style.opacity = '1';
              }}
              onMouseLeave={(e) => {
                if (!isLoading) e.currentTarget.style.opacity = '0.85';
              }}
            >
              Refresh
            </button>

            <button
              type="button"
              onClick={onClose}
              disabled={isLoading}
              aria-label="Close"
              title="Close"
              style={{
                width: 32,
                height: 32,
                borderRadius: 10,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}`,
                background: 'transparent',
                color: 'var(--text-heading)',
                cursor: isLoading ? 'not-allowed' : 'pointer',
                opacity: isLoading ? 0.4 : 0.7,
                fontSize: 18,
                fontWeight: 900,
                lineHeight: 1,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                flexShrink: 0,
                transition: 'opacity 0.2s',
              }}
              onMouseEnter={(e) => {
                if (!isLoading) {
                  e.currentTarget.style.opacity = '1';
                }
              }}
              onMouseLeave={(e) => {
                if (!isLoading) {
                  e.currentTarget.style.opacity = '0.7';
                }
              }}
            >
              ×
            </button>
          </div>
        </div>

        {/* Content area */}
        <div
          style={{
            flex: 1,
            minHeight: 0,
            overflow: 'auto',
            position: 'relative',
          }}
        >
          {/* Loading state - orange dots */}
          {isLoading && (
            <div
              style={{
                position: 'absolute',
                inset: 0,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                background: theme === 'dark' ? 'rgba(0,0,0,0.2)' : 'rgba(255,255,255,0.4)',
                backdropFilter: 'blur(3px)',
                zIndex: 10,
              }}
            >
              <div
                style={{
                  display: 'flex',
                  flexDirection: 'column',
                  alignItems: 'center',
                  gap: 16,
                }}
              >
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
                        background: '#F97316', // orange-500
                        animation: 'horizon-profile-loading-dot 1.1s infinite ease-in-out',
                        animationDelay: `${i * 0.14}s`,
                      }}
                    />
                  ))}
                </div>
                <div
                  style={{
                    fontSize: 13,
                    fontWeight: 600,
                    color: 'var(--text-heading)',
                  }}
                >
                  {refreshing ? 'Refreshing profile...' : creating ? 'Generating profile...' : 'Loading profile...'}
                </div>
              </div>
            </div>
          )}

          {/* Error state */}
          {error && !isLoading && (
            <div
              style={{
                padding: '40px 20px',
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                gap: 12,
                color: '#f87171',
                fontSize: 13,
                textAlign: 'center',
              }}
            >
              <div style={{ fontWeight: 700 }}>Failed to load profile</div>
              <div style={{ fontSize: 12, opacity: 0.8 }}>{error}</div>
            </div>
          )}

          {/* Profile content */}
          {!isLoading && !error && children(profile, false, null)}
        </div>
      </div>
    </>
  );
};

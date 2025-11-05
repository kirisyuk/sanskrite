import React from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { useTranslation } from 'react-i18next';
import { api } from '../lib/api';
import { useAuth } from '../contexts/AuthContext';
import { useDiscoveryStore } from '../stores/discoveryStore';

export default function VerifiedViewPage() {
  const { code } = useParams<{ code: string }>();
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { user } = useAuth();
  const connectWs = useDiscoveryStore((state) => state.connectWs);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [data, setData] = React.useState<any | null>(null);
  const pollIntervalRef = React.useRef<NodeJS.Timeout | null>(null);
  const wsListenerRef = React.useRef<((msg: any) => void) | null>(null);

  // WebSocket listener for verified.stream events
  React.useEffect(() => {
    if (!code || !user) return;

    const subscribeToCode = () => {
      const ws = (window as any).__wsRef;
      if (ws && ws.readyState === WebSocket.OPEN) {
        try {
          ws.send(JSON.stringify({ type: 'subscribe', verifiedCode: code }));
        } catch (e) {
          console.error('Failed to subscribe to verified code:', e);
        }
      }
    };

    // Create listener function
    const listener = (evt: MessageEvent) => {
      try {
        const msg = JSON.parse(evt.data);
        // Only process verified.stream events for this code
        if (msg?.type?.startsWith('verified.stream.') && msg?.data?.code === code) {
          const eventType = msg.type;
          
          if (eventType === 'verified.stream.fetch_start') {
            setData((prev: any) => ({ ...prev, status: 'processing' }));
          } else if (eventType === 'verified.stream.fetch_success') {
            // Fetch updated data when fetch succeeds
            api.verifiedGetPublic(code).then((res) => {
              setData(res);
            }).catch(() => {});
          } else if (eventType === 'verified.stream.fetch_failed') {
            setData((prev: any) => ({
              ...prev,
              status: 'failed',
              error: msg.data?.error,
            }));
            if (pollIntervalRef.current) {
              clearInterval(pollIntervalRef.current);
              pollIntervalRef.current = null;
            }
          } else if (eventType === 'verified.stream.analysis_start') {
            setData((prev: any) => ({ ...prev, status: 'processing' }));
          } else if (eventType === 'verified.stream.analysis_complete') {
            // Fetch final data when analysis completes
            api.verifiedGetPublic(code).then((res) => {
              setData(res);
              if (pollIntervalRef.current) {
                clearInterval(pollIntervalRef.current);
                pollIntervalRef.current = null;
              }
            }).catch(() => {});
          } else if (eventType === 'verified.stream.analysis_failed') {
            setData((prev: any) => ({
              ...prev,
              status: 'failed',
              error: msg.data?.error,
            }));
            if (pollIntervalRef.current) {
              clearInterval(pollIntervalRef.current);
              pollIntervalRef.current = null;
            }
          }
        }
      } catch (e) {
        // Ignore parse errors
      }
    };

    // Access WebSocket from discoveryStore's module scope
    // We'll attach listener via window event delegation
    const wsMessageHandler = (evt: MessageEvent) => {
      listener(evt);
    };

    // Store reference for cleanup
    wsListenerRef.current = wsMessageHandler;

    // Attach listener to window (works because discoveryStore's wsRef dispatches events)
    // Actually, we need to hook into the existing wsRef.onmessage
    // Since we can't directly access wsRef, we'll use a custom event emitter pattern
    // or check the existing wsStore for messages
    
    // For now, let's use polling as fallback and WS will update when connected
    // The WebSocket handler in discoveryStore already processes all messages
    // We need to intercept them - but that's complex. Let's use a simpler approach:
    // Check wsStore for recent messages of type verified.stream.*
    
    // Actually, the simplest is to extend the wsStore or create a custom event system
    // But for MVP, polling + WS updates is fine. The WS will update the store,
    // and we can check it periodically or use a custom event
    
    // Since discoveryStore already handles all WS messages globally,
    // we'll use a custom event emitter on window
    (window as any).__verifiedWsListeners = (window as any).__verifiedWsListeners || [];
    (window as any).__verifiedWsListeners.push(listener);

    // Try to subscribe immediately if WS is already open
    subscribeToCode();

    // Ensure WebSocket is connected and subscribe when it opens
    if (user?.tenantId) {
      connectWs(user.tenantId).then(() => {
        subscribeToCode();
      }).catch(() => {});
    }

    // Track WS open listener to prevent accumulation
    let wsOpenListener: (() => void) | null = null;

    // Also listen for WS open events (in case WS reconnects)
    const checkAndSubscribe = () => {
      const ws = (window as any).__wsRef;
      if (ws) {
        if (ws.readyState === WebSocket.OPEN) {
          subscribeToCode();
        } else {
          // Remove previous listener if exists to prevent accumulation
          if (wsOpenListener) {
            try {
              ws.removeEventListener('open', wsOpenListener);
            } catch {}
          }
          // Wait for open
          const onOpen = () => {
            subscribeToCode();
            if (wsOpenListener) {
              try {
                ws.removeEventListener('open', wsOpenListener);
              } catch {}
              wsOpenListener = null;
            }
          };
          ws.addEventListener('open', onOpen);
          wsOpenListener = onOpen;
        }
      }
    };

    // Check periodically if WS opened (for reconnect scenarios)
    const checkInterval = setInterval(() => {
      checkAndSubscribe();
    }, 1000);

    return () => {
      clearInterval(checkInterval);
      // Clean up WS open listener
      if (wsOpenListener) {
        try {
          const ws = (window as any).__wsRef;
          if (ws) {
            ws.removeEventListener('open', wsOpenListener);
          }
        } catch {}
        wsOpenListener = null;
      }
      if ((window as any).__verifiedWsListeners) {
        (window as any).__verifiedWsListeners = (window as any).__verifiedWsListeners.filter(
          (l: any) => l !== listener
        );
      }
      wsListenerRef.current = null;
    };
  }, [code, user, connectWs]);

  React.useEffect(() => {
    let mounted = true;

    const fetchData = async () => {
      try {
        if (!code) {
          setError(t('verified.view.missingCode'));
          setLoading(false);
          return;
        }
        const res = await api.verifiedGetPublic(code);
        if (mounted) {
          setData(res);
          setLoading(false);

          // If analysis is still processing, poll for updates (as fallback)
          if (res.status === 'pending' || res.status === 'processing') {
            // Clear existing interval
            if (pollIntervalRef.current) {
              clearInterval(pollIntervalRef.current);
            }
            // Poll every 5 seconds (less frequent since WS handles updates)
            pollIntervalRef.current = setInterval(async () => {
              if (!mounted) return;
              try {
                const updated = await api.verifiedGetPublic(code!);
                if (mounted) {
                  setData(updated);
                  // Stop polling when completed or failed
                  if (updated.status === 'completed' || updated.status === 'failed') {
                    if (pollIntervalRef.current) {
                      clearInterval(pollIntervalRef.current);
                      pollIntervalRef.current = null;
                    }
                  }
                }
              } catch (e) {
                // Silent fail on polling errors
                console.error('Polling error:', e);
              }
            }, 5000);
          }
        }
      } catch (e: any) {
        if (mounted) {
          setError(e?.response?.data?.message || t('verified.view.error'));
          setLoading(false);
        }
      }
    };

    fetchData();

    return () => {
      mounted = false;
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
        pollIntervalRef.current = null;
      }
    };
  }, [code, t]);

  const handleCopyUrl = async () => {
    const fullUrl = `${window.location.origin}/verified/${code}`;
    try {
      await navigator.clipboard.writeText(fullUrl);
      // Could show toast notification here
    } catch (err) {
      // Fallback
      const textArea = document.createElement('textarea');
      textArea.value = fullUrl;
      document.body.appendChild(textArea);
      textArea.select();
      document.execCommand('copy');
      document.body.removeChild(textArea);
    }
  };

  const handleShare = () => {
    if (navigator.share && code) {
      navigator.share({
        title: t('verified.view.shareTitle'),
        text: t('verified.view.shareText'),
        url: `${window.location.origin}/verified/${code}`,
      }).catch(() => {
        // User cancelled or error
      });
    } else {
      handleCopyUrl();
    }
  };

  if (loading) {
    return (
      <div className="content-area flex items-center justify-center" style={{ minHeight: 'calc(100vh - 88px)' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary)' }}>
          {t('verified.view.loading')}
        </div>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="content-area flex flex-col items-center justify-center p-6" style={{ minHeight: 'calc(100vh - 88px)' }}>
        <div className="w-full max-w-lg text-center">
          <h1 className="text-2xl font-semibold mb-3" style={{ color: 'var(--text-heading)' }}>
            {t('verified.view.errorTitle')}
          </h1>
          <p className="text-base mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            {error || t('verified.view.errorDescription')}
          </p>
          <button
            onClick={() => navigate('/verified')}
            className="mt-4 px-4 py-2 rounded-lg text-13 font-medium"
            style={{
              backgroundColor: 'var(--accent-primary)',
              color: 'var(--text-on-accent)',
              border: 'none',
              cursor: 'pointer',
            }}
          >
            {t('verified.view.backToForm')}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="content-area">
      <div className="max-w-3xl mx-auto px-4 py-4">
        {/* Header */}
        <div className="mb-4 flex items-start justify-between">
          <div className="flex-1">
            <div className="text-[13px] font-semibold mb-1" style={{ color: 'var(--text-heading)' }}>
              {t('verified.view.title')}
            </div>
            {data.url && (
              <a
                href={data.url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-12 inline-flex items-center gap-1 hover:underline"
                style={{ color: 'var(--accent-primary)' }}
              >
                {data.url}
                <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6M15 3h6v6M10 14L21 3" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
                </svg>
              </a>
            )}
            {data.createdAt && (
              <div className="text-11 mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
                {t('verified.view.createdAt')} {new Date(data.createdAt).toLocaleString()}
              </div>
            )}
          </div>
          <div className="flex items-center gap-2 ml-4">
            <button
              onClick={handleCopyUrl}
              className="px-3 py-1.5 rounded-lg text-12 font-medium"
              style={{
                backgroundColor: 'var(--bg-elevated)',
                border: '1px solid var(--border-subtle)',
                color: 'var(--text-primary)',
                cursor: 'pointer',
              }}
              title={t('verified.view.copyUrl')}
            >
              {t('verified.view.copy')}
            </button>
            <button
              onClick={handleShare}
              className="px-3 py-1.5 rounded-lg text-12 font-medium"
              style={{
                backgroundColor: 'var(--bg-elevated)',
                border: '1px solid var(--border-subtle)',
                color: 'var(--text-primary)',
                cursor: 'pointer',
              }}
              title={t('verified.view.share')}
            >
              {t('verified.view.share')}
            </button>
          </div>
        </div>

        {/* Status indicator */}
        {data.status && (
          <div className="mt-4 mb-4">
            {data.status === 'pending' || data.status === 'processing' ? (
              <div className="p-3 rounded-lg flex items-center gap-3" style={{ backgroundColor: 'var(--accent-research-10)', border: '1px solid var(--accent-research-20)' }}>
                <div className="animate-spin rounded-full h-4 w-4 border-2 border-current border-t-transparent" style={{ color: 'var(--accent-research)' }}></div>
                <div className="text-12" style={{ color: 'var(--accent-research)' }}>
                  {data.status === 'pending' ? t('verified.view.status.pending') : t('verified.view.status.processing')}
                </div>
              </div>
            ) : data.status === 'failed' ? (
              <div className="p-3 rounded-lg" style={{ backgroundColor: 'var(--accent-danger-10)', border: '1px solid var(--accent-danger-20)' }}>
                <div className="text-12 font-semibold mb-1" style={{ color: 'var(--accent-danger)' }}>
                  {t('verified.view.status.failed')}
                </div>
                {data.error && (
                  <div className="text-11" style={{ color: 'var(--text-secondary)' }}>
                    {data.error}
                  </div>
                )}
              </div>
            ) : null}
          </div>
        )}

        {/* Analysis Content */}
        {data.analysis && data.status === 'completed' && (
          <div className="mt-4 p-4 rounded-lg" style={{ backgroundColor: 'var(--bg-elevated)', border: '1px solid var(--border-subtle)' }}>
            <div className="markdown prose prose-sm max-w-none">
              <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>
                {data.analysis}
              </ReactMarkdown>
            </div>
          </div>
        )}

        {!data.analysis && data.status === 'completed' && (
          <div className="mt-4 p-4 rounded-lg text-center" style={{ backgroundColor: 'var(--bg-elevated)', border: '1px solid var(--border-subtle)' }}>
            <div className="text-13" style={{ color: 'var(--text-secondary)' }}>
              {t('verified.view.noAnalysis')}
            </div>
          </div>
        )}

        {/* Metadata */}
        {data.model && (
          <div className="mt-4 text-11" style={{ color: 'var(--text-tertiary-weak)' }}>
            {t('verified.view.model')}: {data.model}
          </div>
        )}
      </div>
    </div>
  );
}


import React from 'react';
import { useTranslation } from 'react-i18next';
import { DataSource } from '../types/generated';
import { api } from '../lib/api';
import { MarkdownRenderer } from '../lib/markdownComponents';

type SourceWithProfile = DataSource & { profile?: any; description?: string };

interface SourcePreviewProps {
  source: SourceWithProfile;
  onClose: () => void;
  style?: React.CSSProperties;
}

const headerStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  padding: '10px 12px',
  borderBottom: '1px solid var(--overlay-line-90)',
};

const titleStyle: React.CSSProperties = {
  fontSize: '0.8125rem',
  lineHeight: '1.25rem',
  fontWeight: 600,
  color: 'var(--text-heading)',
};

const closeBtnStyle: React.CSSProperties = {
  padding: 6,
  borderRadius: 8,
  backgroundColor: 'transparent',
  color: 'var(--text-heading)',
};

export const SourcePreview: React.FC<SourcePreviewProps> = ({ source, onClose, style }) => {
  const { t } = useTranslation();
  const containerRef = React.useRef<HTMLDivElement | null>(null);
  const [documents, setDocuments] = React.useState<any[]>([]);
  const [loadingDocs, setLoadingDocs] = React.useState(false);
  const [documentsError, setDocumentsError] = React.useState<string | null>(null);
  const [ingestHistory, setIngestHistory] = React.useState<any[]>([]);
  const [loadingHistory, setLoadingHistory] = React.useState(false);
  const [isHistoryExpanded, setIsHistoryExpanded] = React.useState(true);
  const [isDocumentsExpanded, setIsDocumentsExpanded] = React.useState(true);

  React.useEffect(() => {
    function handleDown(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onClose();
      }
    }
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('mousedown', handleDown);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('mousedown', handleDown);
      document.removeEventListener('keydown', handleKey);
    };
  }, [onClose]);

  // Load documents when source changes
  React.useEffect(() => {
    if (!source?.id) return;
    setLoadingDocs(true);
    setDocumentsError(null);
    
    // For Telegram sources, use preview endpoint
    if (source.kind === 'telegram_channel' && source.sourceDetails?.channelUsername) {
      const channel = source.sourceDetails.channelUsername;
      api.previewTelegramChannel(channel)
        .then((result) => {
          // Convert Telegram messages to document format
          // TelegramMessage format: { id: number, text: string, date: string, link: string }
          // Telegram service (Telethon iter_messages) returns messages in reverse chronological order (newest first),
          // so messages are already in the correct order
          const docs = (result.messages || []).map((msg: any) => ({
              id: msg.id?.toString() || '',
              title: msg.link || '',
              fullText: msg.text || '', // Use fullText to match document display format
              text: msg.text || '',
              url: msg.link || '',
              date: msg.date || '',
              link: msg.link || '',
              originPublishedAt: msg.date || null,
            }));
          setDocuments(docs);
          setLoadingDocs(false);
        })
        .catch((err) => {
          console.error('[SourcePreview] Failed to load Telegram preview', err);
          setDocumentsError('Failed to load messages');
          setLoadingDocs(false);
        });
    } else {
      // For other sources, use documents endpoint
      api.getSourceDocuments(source.id, 20)
        .then((docs) => {
          setDocuments(docs || []);
          setLoadingDocs(false);
        })
        .catch((err) => {
          console.error('[SourcePreview] Failed to load documents', err);
          setDocumentsError('Failed to load messages');
          setLoadingDocs(false);
        });
    }
  }, [source?.id, source?.kind, source?.sourceDetails?.channelUsername]);

  // Load ingest history
  React.useEffect(() => {
    if (!source?.id) return;
    setLoadingHistory(true);
    api.getSourceIngestHistory(source.id, 20)
      .then((result) => {
        setIngestHistory(result.history || []);
        setLoadingHistory(false);
      })
      .catch((err) => {
        console.error('[SourcePreview] Failed to load ingest history', err);
        setLoadingHistory(false);
      });
  }, [source?.id]);

  const badgeStyle: React.CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    padding: '2px 8px',
    borderRadius: 999,
    backgroundColor: 'var(--white-10)',
    color: 'var(--text-secondary-strong)',
    fontSize: 11,
  };

  return (
    <div
      ref={containerRef}
      style={{
        position: 'absolute',
        top: 8,
        left: 8,
        bottom: 140,
        width: 380,
        maxWidth: 'calc(100% - 16px)',
        backgroundColor: 'var(--bg-elevated)',
        border: '1px solid var(--overlay-line-90)',
        borderRadius: 12,
        boxShadow: '0 8px 24px rgba(0,0,0,0.35)',
        zIndex: 30,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        ...style,
      }}
      role="dialog"
      aria-label="Source preview"
    >
      <div style={headerStyle}>
        <div style={titleStyle}>
          {source.kind ? `${source.kind}` : 'source'}
          <span style={{ fontWeight: 400, color: 'var(--text-secondary-strong)' }}> — {source.displayName || source.id}</span>
        </div>
        <button
          onClick={onClose}
          aria-label="Close preview"
          style={closeBtnStyle}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>

      <div style={{ padding: 12, overflowY: 'auto' }}>
        {/* Telegram inline preview (under header) */}
        {(() => {
          const isTelegram = (source.kind === 'telegram') ||
            (!!source.sourceDetails?.channelUsername) ||
            (typeof source.id === 'string' && source.id.startsWith('@'));
          if (!isTelegram) return null;

          const username = (source.sourceDetails?.channelUsername) ||
            (typeof source.id === 'string' && source.id.startsWith('@') ? source.id.slice(1) : undefined);
          const href = username ? `https://t.me/${username}` : (source.sourceDetails?.uri || undefined);
          const display = source.displayName || (username ? `@${username}` : (source.id as string));

          return (
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: 10,
                border: '1px solid var(--overlay-line-90)',
                borderRadius: 10,
                backgroundColor: 'var(--bg-sidebar)',
                marginBottom: 10,
              }}
              aria-label="Telegram channel preview"
            >
              <div
                style={{
                  width: 36,
                  height: 36,
                  borderRadius: 8,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  background: 'linear-gradient(135deg, #2AABEE 0%, #229ED9 100%)',
                  color: 'white',
                  flexShrink: 0,
                }}
              >
                <svg viewBox="0 0 24 24" width={18} height={18} fill="none" stroke="currentColor" strokeWidth={2}>
                  <path d="M3 11l18-7-7 18-4-8-7-3z" stroke="currentColor" />
                </svg>
              </div>
              <div style={{ minWidth: 0, flex: 1 }}>
                <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 600 }}>{display}</div>
                {username && (
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>@{username}</div>
                )}
              </div>
              {href && (
                <a
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-xs"
                  style={{
                    padding: '6px 10px',
                    borderRadius: 8,
                    border: '1px solid var(--overlay-line-90)',
                    color: 'var(--text-heading)',
                    backgroundColor: 'transparent',
                    textDecoration: 'none',
                    whiteSpace: 'nowrap',
                  }}
                >
                  Open
                </a>
              )}
            </div>
          );
        })()}

        {/* Basic info */}
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
          <span style={{
            ...badgeStyle,
            color: source.status === 'active' 
              ? 'var(--accent-success)' 
              : source.status === 'paused'
              ? 'var(--accent-primary)'
              : 'var(--text-secondary-strong)',
            backgroundColor: source.status === 'active'
              ? 'var(--success-20)'
              : source.status === 'paused'
              ? 'rgba(255, 169, 77, 0.20)'
              : 'var(--white-10)',
          }}>{source.status || 'unknown'}</span>
          {source.trustLevel !== undefined && (
            <span style={badgeStyle}>trust: {source.trustLevel}</span>
          )}
        </div>

        {/* Description / Profile summary */}
        {source.profile?.motivation && (
          <div style={{ marginBottom: 8, color: 'var(--text-secondary-strong)' }}>{source.profile.motivation}</div>
        )}
        {!source.profile?.motivation && source.profile?.agenda?.description && (
          <div style={{ marginBottom: 8, color: 'var(--text-secondary-strong)' }}>{source.profile.agenda.description}</div>
        )}
        {!source.profile && source.description && (
          <div style={{ marginBottom: 8, color: 'var(--text-secondary-strong)' }}>{source.description}</div>
        )}

        {/* Primary goal */}
        {source.profile?.agenda?.primaryGoal && (
          <div style={{ margin: '8px 0', color: 'var(--text-heading)' }}>
            <span style={{ color: 'var(--text-secondary-strong)' }}>primaryGoal:</span> {source.profile.agenda.primaryGoal}
          </div>
        )}

        {/* Topics */}
        {Array.isArray(source.profile?.topics) && source.profile.topics.length > 0 && (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 8 }}>
            {source.profile.topics.slice(0, 10).map((t: any, i: number) => (
              <span
                key={i}
                style={{
                  padding: '2px 8px',
                  borderRadius: 999,
                  backgroundColor: 'var(--white-10)',
                  color: 'var(--text-secondary-strong)',
                  fontSize: 11,
                }}
              >
                {t.name}{typeof t.share === 'number' ? ` ${Math.round(t.share * 100)}%` : ''}
              </span>
            ))}
          </div>
        )}

        {/* Alignment metric */}
        {typeof source.profile?.goalAlignmentScore === 'number' && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 10, color: 'var(--text-secondary-strong)', fontSize: 12 }}>
            <span>alignment</span>
            <div style={{ width: 120, height: 6, borderRadius: 4, backgroundColor: 'var(--overlay-line-90)' }}>
              <div
                style={{
                  width: `${Math.max(0, Math.min(1, source.profile.goalAlignmentScore)) * 100}%`,
                  height: 6,
                  borderRadius: 4,
                  backgroundColor: 'var(--accent-success)'
                }}
              />
            </div>
            <span>{Math.round(Math.max(0, Math.min(1, source.profile.goalAlignmentScore)) * 100)}%</span>
          </div>
        )}

        {/* Details list */}
        <div style={{ marginTop: 12, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          <div><span style={{ color: 'var(--text-tertiary-weak)' }}>id:</span> {source.id}</div>
          {source.sourceDetails?.uri && (
            <div><span style={{ color: 'var(--text-tertiary-weak)' }}>uri:</span> {source.sourceDetails.uri}</div>
          )}
          {source.sourceDetails?.channelUsername && (
            <div><span style={{ color: 'var(--text-tertiary-weak)' }}>telegram:</span> @{source.sourceDetails.channelUsername}</div>
          )}
        </div>

        {/* Polling Schedule section - only show if source is not paused */}
        {source.status !== 'paused' && (
          <div style={{ marginTop: 16 }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-heading)', marginBottom: 8 }}>
              Polling Schedule
            </div>
            <div style={{ 
              padding: 10, 
              borderRadius: 8, 
              backgroundColor: 'var(--bg-sidebar)', 
              border: '1px solid var(--overlay-line-90)',
              fontSize: 11,
            }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ color: 'var(--text-tertiary-weak)' }}>Last Polled:</span>
                  <span style={{ color: 'var(--text-secondary-strong)' }}>
                    {source.lastPolledAt 
                      ? new Date(source.lastPolledAt).toLocaleString('ru-RU', {
                          year: 'numeric',
                          month: '2-digit',
                          day: '2-digit',
                          hour: '2-digit',
                          minute: '2-digit',
                        })
                      : 'Never'}
                  </span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ color: 'var(--text-tertiary-weak)' }}>Next Poll:</span>
                  <span style={{ 
                    color: source.nextPollAt && new Date(source.nextPollAt) > new Date()
                      ? 'var(--accent-success)'
                      : source.nextPollAt
                      ? 'var(--accent-primary)'
                      : 'var(--text-tertiary-weak)'
                  }}>
                    {source.nextPollAt 
                      ? new Date(source.nextPollAt).toLocaleString('ru-RU', {
                          year: 'numeric',
                          month: '2-digit',
                          day: '2-digit',
                          hour: '2-digit',
                          minute: '2-digit',
                        })
                      : 'Not scheduled'}
                  </span>
                </div>
                {source.lastPolledAt && source.nextPollAt && (
                  <div style={{ 
                    fontSize: 10, 
                    color: 'var(--text-tertiary-weak)', 
                    marginTop: 4,
                    paddingTop: 6,
                    borderTop: '1px solid var(--overlay-line-90)'
                  }}>
                    {(() => {
                      const now = new Date();
                      const nextPoll = new Date(source.nextPollAt);
                      const diffMs = nextPoll.getTime() - now.getTime();
                      if (diffMs < 0) {
                        const overdue = Math.abs(diffMs);
                        const overdueHours = Math.floor(overdue / (1000 * 60 * 60));
                        const overdueMinutes = Math.floor((overdue % (1000 * 60 * 60)) / (1000 * 60));
                        return `Overdue by ${overdueHours}h ${overdueMinutes}m`;
                      } else {
                        const hours = Math.floor(diffMs / (1000 * 60 * 60));
                        const minutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
                        return `In ${hours}h ${minutes}m`;
                      }
                    })()}
                  </div>
                )}
              </div>
            </div>
          </div>
        )}

        {/* Ingest History section */}
        <div style={{ marginTop: 16 }}>
          <button
            onClick={() => setIsHistoryExpanded(!isHistoryExpanded)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              fontSize: 12,
              fontWeight: 600,
              color: 'var(--text-heading)',
              marginBottom: 8,
              backgroundColor: 'transparent',
              border: 'none',
              padding: 0,
              cursor: 'pointer',
            }}
          >
            <svg
              width="12"
              height="12"
              viewBox="0 0 12 12"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              style={{
                transform: isHistoryExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                transition: 'transform 0.2s',
              }}
            >
              <path d="M4 2L8 6L4 10" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            Ingest History ({ingestHistory.length})
          </button>
          {isHistoryExpanded && (
            <>
              {loadingHistory ? (
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', padding: '8px 0' }}>
                  Loading history...
                </div>
              ) : ingestHistory.length === 0 ? (
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', padding: '8px 0' }}>
                  No ingest history yet
                </div>
              ) : (
                <div style={{ maxHeight: 200, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 6 }}>
                  {ingestHistory.map((h: any) => (
                    <div
                      key={h.id}
                      style={{
                        padding: 8,
                        borderRadius: 8,
                        backgroundColor: 'var(--bg-sidebar)',
                        border: '1px solid var(--overlay-line-90)',
                        fontSize: 11,
                      }}
                    >
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
                        {(h.status === 'empty' || (h.status === 'partial' && (h.documentsProcessed || 0) === 0)) ? (
                          <span style={{ 
                            padding: '2px 6px', 
                            borderRadius: 4, 
                            backgroundColor: 'rgba(156, 163, 175, 0.1)',
                            border: '1px solid rgba(156, 163, 175, 0.3)',
                            color: 'rgb(156, 163, 175)',
                            fontSize: 10,
                            fontWeight: 600,
                          }}>
                            {h.status === 'empty' ? t('unfold.source.noUpdates') : t('unfold.source.noNewMessages')}
                          </span>
                        ) : (
                          <span style={{ 
                            padding: '2px 6px', 
                            borderRadius: 4, 
                            backgroundColor: h.status === 'success' ? 'var(--accent-success)' : h.status === 'error' ? 'var(--accent-error)' : 'var(--accent-warning)',
                            color: 'white',
                            fontSize: 10,
                            fontWeight: 600,
                          }}>
                            {h.status}
                          </span>
                        )}
                        <span style={{ color: 'var(--text-tertiary-weak)', fontSize: 10 }}>
                          {h.completedAt ? new Date(h.completedAt).toLocaleString() : new Date(h.startedAt).toLocaleString()}
                        </span>
                      </div>
                      <div style={{ color: 'var(--text-secondary-strong)', marginTop: 4 }}>
                        <div>Processed: {h.documentsProcessed || 0} • Created: {h.documentsCreated || 0}</div>
                        {h.durationMs && (
                          <div style={{ fontSize: 10, color: 'var(--text-tertiary-weak)', marginTop: 2 }}>
                            Duration: {(h.durationMs / 1000).toFixed(1)}s
                          </div>
                        )}
                        {h.error && (
                          <div style={{ fontSize: 10, color: 'var(--accent-error)', marginTop: 2 }}>
                            Error: {h.error}
                          </div>
                        )}
                        {h.params && Object.keys(h.params).length > 0 && (
                          <div style={{ fontSize: 10, color: 'var(--text-tertiary-weak)', marginTop: 2 }}>
                            Params: {JSON.stringify(h.params)}
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </>
          )}
        </div>

        {/* Messages/Documents section */}
        <div style={{ marginTop: 16 }}>
          <button
            onClick={() => setIsDocumentsExpanded(!isDocumentsExpanded)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              fontSize: 12,
              fontWeight: 600,
              color: 'var(--text-heading)',
              marginBottom: 8,
              backgroundColor: 'transparent',
              border: 'none',
              padding: 0,
              cursor: 'pointer',
            }}
          >
            <svg
              width="12"
              height="12"
              viewBox="0 0 12 12"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              style={{
                transform: isDocumentsExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                transition: 'transform 0.2s',
              }}
            >
              <path d="M4 2L8 6L4 10" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            {source.kind === 'telegram_channel' ? 'Messages' : 'Documents'} ({documents.length})
          </button>
          {isDocumentsExpanded && (
            <>
              {loadingDocs ? (
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', padding: '8px 0' }}>
                  Loading messages...
                </div>
              ) : documentsError ? (
                <div style={{ fontSize: 11, color: 'var(--accent-error)', padding: '8px 0' }}>
                  {documentsError}
                </div>
              ) : documents.length === 0 ? (
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', padding: '8px 0' }}>
                  No messages yet
                </div>
              ) : (
                <div style={{ maxHeight: 300, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {documents.map((doc: any) => (
                    <div
                      key={doc.id}
                      style={{
                        padding: 8,
                        borderRadius: 8,
                        backgroundColor: 'var(--bg-sidebar)',
                        border: '1px solid var(--overlay-line-90)',
                      }}
                    >
                      {(doc.fullText || doc.text) && (
                        <div
                          className="markdown"
                          style={{
                            fontSize: 11,
                            color: 'var(--text-secondary-strong)',
                            lineHeight: 1.4,
                            marginBottom: doc.url ? 4 : 0,
                          }}
                        >
                          <MarkdownRenderer 
                            content={doc.fullText || doc.text || ''} 
                            skipWrapper={true}
                            className=""
                          />
                        </div>
                      )}
                      {doc.title && doc.title !== doc.url && (
                        <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-heading)', marginBottom: 4 }}>
                          {doc.title}
                        </div>
                      )}
                      {doc.originPublishedAt && (
                        <div style={{ fontSize: 10, color: 'var(--text-tertiary-weak)', marginTop: 4 }}>
                          {new Date(doc.originPublishedAt).toLocaleDateString()}
                        </div>
                      )}
                      {doc.url && (
                        <a
                          href={doc.url}
                          target="_blank"
                          rel="noopener noreferrer"
                          style={{
                            fontSize: 10,
                            color: 'var(--accent-primary)',
                            textDecoration: 'none',
                            marginTop: 4,
                            display: 'inline-block',
                          }}
                        >
                          Open →
                        </a>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
};

export default SourcePreview;



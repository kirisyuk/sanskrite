import React from 'react';
import { DataSource } from '../types/generated';
import { api } from '../lib/api';
import { MarkdownRenderer } from '../lib/markdownComponents';

interface SourceDocumentsViewerProps {
  source: DataSource;
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

export const SourceDocumentsViewer: React.FC<SourceDocumentsViewerProps> = ({ source, onClose, style }) => {
  const containerRef = React.useRef<HTMLDivElement | null>(null);
  const [documents, setDocuments] = React.useState<any[]>([]);
  const [loadingDocs, setLoadingDocs] = React.useState(false);
  const [documentsError, setDocumentsError] = React.useState<string | null>(null);
  const [reprocessing, setReprocessing] = React.useState(false);
  const [reprocessMessage, setReprocessMessage] = React.useState<string | null>(null);

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

  // Load documents from database (created by ingest)
  React.useEffect(() => {
    if (!source?.id) return;
    setLoadingDocs(true);
    setDocumentsError(null);
    
    api.getSourceDocuments(source.id, 100)
      .then((docs) => {
        setDocuments(docs || []);
        setLoadingDocs(false);
      })
      .catch((err) => {
        console.error('[SourceDocumentsViewer] Failed to load documents', err);
        setDocumentsError('Failed to load documents');
        setLoadingDocs(false);
      });
  }, [source?.id]);

  const displayName = source.displayName || 
    (source.sourceDetails?.channelUsername ? `@${source.sourceDetails.channelUsername}` : source.id);

  const handleReprocess = async () => {
    if (!source?.id) return;
    setReprocessing(true);
    setReprocessMessage(null);
    try {
      const result = await api.reprocessSource(source.id);
      setReprocessMessage(`Reprocessing queued for ${result.count} documents`);
      // Reload documents after a delay to see updated statuses
      setTimeout(() => {
        api.getSourceDocuments(source.id, 100)
          .then((docs) => {
            setDocuments(docs || []);
          })
          .catch((err) => {
            console.error('[SourceDocumentsViewer] Failed to reload documents', err);
          });
      }, 2000);
    } catch (err) {
      console.error('[SourceDocumentsViewer] Failed to reprocess', err);
      setReprocessMessage('Failed to reprocess documents');
    } finally {
      setReprocessing(false);
    }
  };

  const isTelegramSource = source.kind === 'telegram_channel';

  return (
    <div
      ref={containerRef}
      style={{
        position: 'fixed',
        top: '50%',
        left: '50%',
        transform: 'translate(-50%, -50%)',
        width: '90%',
        maxWidth: '800px',
        maxHeight: '90vh',
        backgroundColor: 'var(--bg-sidebar)',
        border: '1px solid var(--overlay-line-90)',
        borderRadius: 12,
        boxShadow: '0 8px 32px rgba(0, 0, 0, 0.3)',
        zIndex: 1000,
        display: 'flex',
        flexDirection: 'column',
        ...style,
      }}
    >
      {/* Header */}
      <div style={headerStyle}>
        <div style={titleStyle}>
          Documents from {displayName}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {isTelegramSource && (
            <button
              type="button"
              onClick={handleReprocess}
              disabled={reprocessing}
              style={{
                padding: '6px 12px',
                borderRadius: 8,
                backgroundColor: 'var(--accent-primary)',
                color: 'white',
                fontSize: 11,
                fontWeight: 600,
                border: 'none',
                cursor: reprocessing ? 'not-allowed' : 'pointer',
                opacity: reprocessing ? 0.6 : 1,
                display: 'flex',
                alignItems: 'center',
                gap: 6,
              }}
              onMouseEnter={(e) => {
                if (!reprocessing) {
                  (e.currentTarget as HTMLButtonElement).style.opacity = '0.9';
                }
              }}
              onMouseLeave={(e) => {
                if (!reprocessing) {
                  (e.currentTarget as HTMLButtonElement).style.opacity = '1';
                }
              }}
            >
              {reprocessing ? (
                <>
                  <svg className="w-3 h-3 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" strokeWidth="2" strokeLinecap="round" />
                  </svg>
                  Processing...
                </>
              ) : (
                <>
                  <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <path d="M1 4v6h6M23 20v-6h-6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  Reprocess
                </>
              )}
            </button>
          )}
          <button
            type="button"
            onClick={onClose}
            style={closeBtnStyle}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
      </div>

      {/* Content */}
      <div style={{ padding: 12, overflowY: 'auto', flex: 1 }}>
        {reprocessMessage && (
          <div style={{
            fontSize: 11,
            color: reprocessMessage.includes('Failed') ? 'var(--accent-error)' : 'var(--accent-primary)',
            padding: '8px 12px',
            borderRadius: 8,
            backgroundColor: reprocessMessage.includes('Failed') ? 'var(--accent-error-10)' : 'var(--accent-primary-10)',
            marginBottom: 12,
          }}>
            {reprocessMessage}
          </div>
        )}
        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-heading)', marginBottom: 8 }}>
          Documents ({documents.length})
        </div>
        {loadingDocs ? (
          <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', padding: '8px 0' }}>
            Loading documents...
          </div>
        ) : documentsError ? (
          <div style={{ fontSize: 11, color: 'var(--accent-error)', padding: '8px 0' }}>
            {documentsError}
          </div>
        ) : documents.length === 0 ? (
          <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', padding: '8px 0' }}>
            No documents found. Run ingest to create documents from this source.
          </div>
        ) : (
          <div style={{ maxHeight: 'calc(90vh - 120px)', overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 8 }}>
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
                {doc.title && doc.title !== doc.url && (
                  <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-heading)', marginBottom: 4 }}>
                    {doc.title}
                  </div>
                )}
                {(doc.fullText || doc.text) && (
                  <div
                    className="markdown"
                    style={{
                      fontSize: 11,
                      color: 'var(--text-secondary-strong)',
                      lineHeight: 1.4,
                      marginBottom: doc.url ? 4 : 0,
                      whiteSpace: 'pre-wrap',
                    }}
                  >
                    <MarkdownRenderer 
                      content={doc.fullText || doc.text || ''} 
                      skipWrapper={true}
                      className=""
                    />
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
                {doc.status && (
                  <div style={{ fontSize: 10, color: 'var(--text-tertiary-weak)', marginTop: 4 }}>
                    Status: {doc.status}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default SourceDocumentsViewer;


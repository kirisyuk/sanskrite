import React from 'react';
import { api } from '../lib/api';

export interface CoreDocMeta {
  id: string;
  filename: string;
  contentType?: string;
  sizeBytes?: number;
  parseStatus?: string;
}

interface DocumentViewerProps {
  projectId: string;
  doc: CoreDocMeta;
  onClose: () => void;
}

const DocumentViewer: React.FC<DocumentViewerProps> = ({ projectId, doc, onClose }) => {
  const [blobUrl, setBlobUrl] = React.useState<string | null>(null);
  const [textContent, setTextContent] = React.useState<string | null>(null);
  const [jsonObject, setJsonObject] = React.useState<any>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const isEntities = (doc.filename || '').toLowerCase() === 'entities.json';

  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  React.useEffect(() => {
    let revoked: string | null = null;
    (async () => {
      setLoading(true); setError(null);
      try {
        const blob = await api.docsDownload(projectId, doc.id);
        const ct = (blob.type || doc.contentType || '').toLowerCase();
        // Try parse text for text-like types
        if (isEntities || ct.includes('json') || ct.startsWith('text/') || /\.(md|txt|csv|tsv)$/i.test(doc.filename)) {
          const text = await new Response(blob).text();
          setTextContent(text);
          if (isEntities) {
            try { setJsonObject(JSON.parse(text || '{}')); } catch { setJsonObject(null); }
          } else if (ct.includes('json')) {
            try { setJsonObject(JSON.parse(text || '{}')); } catch { /* ignore */ }
          }
        } else {
          const url = URL.createObjectURL(blob);
          setBlobUrl(url);
          revoked = url;
        }
      } catch (e: any) {
        setError('Failed to load document');
      } finally {
        setLoading(false);
      }
    })();
    return () => { if (revoked) { try { URL.revokeObjectURL(revoked); } catch {} } };
  }, [projectId, doc.id]);

  const headerTitle = isEntities ? 'Entities container' : (doc.filename || 'document');

  const download = async () => {
    try {
      const blob = await api.docsDownload(projectId, doc.id);
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = doc.filename || 'download';
      document.body.appendChild(a);
      a.click();
      setTimeout(() => { try { URL.revokeObjectURL(a.href); document.body.removeChild(a); } catch {} }, 0);
    } catch {}
  };

  const renderEntities = () => {
    const arr = Array.isArray(jsonObject?.entities) ? jsonObject.entities : [];
    if (loading) return <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Loading…</div>;
    if (error) return <div className="text-12" style={{ color: 'var(--accent-danger)' }}>{error}</div>;
    return (
      <div className="p-3 space-y-2">
        {arr.length === 0 ? (
          <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>No entities yet.</div>
        ) : (
          arr.map((e: any, idx: number) => (
            <div key={idx} className="p-2 rounded" style={{ background: 'var(--white-06)', border: '1px solid var(--overlay-line-90)' }}>
              <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{String(e?.name || '')}</div>
              <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>{String(e?.type || '')}</div>
              {e?.description && (
                <div className="mt-1 text-12" style={{ color: 'var(--text-secondary-strong)', whiteSpace: 'pre-wrap' }}>{String(e.description)}</div>
              )}
            </div>
          ))
        )}
      </div>
    );
  };

  const renderText = () => {
    if (loading) return <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Loading…</div>;
    if (error) return <div className="text-12" style={{ color: 'var(--accent-danger)' }}>{error}</div>;
    if (jsonObject && !isEntities) {
      return (
        <pre className="p-3 text-12" style={{ color: 'var(--text-heading)', background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', borderRadius: 8, overflow: 'auto' }}>
          {JSON.stringify(jsonObject, null, 2)}
        </pre>
      );
    }
    return (
      <pre className="p-3 text-12" style={{ color: 'var(--text-heading)', background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', borderRadius: 8, overflow: 'auto' }}>
        {textContent || ''}
      </pre>
    );
  };

  const ext = (doc.filename.split('.').pop() || '').toLowerCase();
  const ct = (doc.contentType || '').toLowerCase();
  const isImage = /^(png|jpg|jpeg|gif|webp|bmp|svg)$/.test(ext) || ct.startsWith('image/');
  const isPdf = ext === 'pdf' || ct.includes('pdf');
  const isMarkdown = ext === 'md' || ct.includes('markdown');
  const isTextLike = isMarkdown || ct.startsWith('text/') || /(json|csv|tsv)/.test(ct) || /(md|txt|csv|tsv|json)$/i.test(ext);

  const renderBody = () => {
    if (isEntities) return renderEntities();
    if (isImage && blobUrl) return <img src={blobUrl} alt={doc.filename} style={{ maxWidth: '100%', height: 'auto' }} />;
    if (isPdf && blobUrl) return (
      <iframe title={doc.filename} src={blobUrl} style={{ width: '100%', height: '100%', border: 'none' }} />
    );
    if (isTextLike) return renderText();
    if (loading) return <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Loading…</div>;
    if (error) return <div className="text-12" style={{ color: 'var(--accent-danger)' }}>{error}</div>;
    return (
      <div className="p-3 text-12" style={{ color: 'var(--text-secondary-strong)' }}>
        Preview is not available for this file type. Use Download to open.
      </div>
    );
  };

  return (
    <div className="absolute inset-0 z-[70] rounded-xl" style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', overflow: 'hidden' }}>
      <div className="flex items-center justify-between px-3 py-2" style={{ borderBottom: '1px solid var(--overlay-line-90)' }}>
        <div className="text-13" style={{ color: 'var(--text-heading)' }} title={doc.filename}>{headerTitle}</div>
        <div className="flex items-center gap-2">
          <button type="button" className="px-2 py-1 rounded text-11" onClick={download} title="Download" aria-label="Download" style={{ background: 'transparent', color: 'var(--text-secondary-strong)' }} onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }} onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}>
            Download
          </button>
          <button type="button" className="p-1 rounded-lg transition-colors" aria-label="Close" title="Close" onClick={onClose} style={{ backgroundColor: 'transparent', color: 'var(--text-heading)', cursor: 'pointer', transition: 'background-color 120ms ease-in-out' }} onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)'; }} onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}>
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ pointerEvents: 'none' }}><path d="M6 6l12 12M18 6L6 18" strokeWidth={2} /></svg>
          </button>
        </div>
      </div>
      <div className="absolute inset-x-0 bottom-0 top-[38px]">
        <div className="w-full h-full overflow-hidden">
          <div className="w-full h-full overflow-auto px-3 py-2">
            {renderBody()}
          </div>
        </div>
      </div>
    </div>
  );
};

export default DocumentViewer;

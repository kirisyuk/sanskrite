import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { api } from '../lib/api';
import { isArtifact, type Artifact } from '../types/artifact';
import type { CoreDoc } from '../types/generated';

export interface CoreDocMeta {
  id: string;
  filename: string;
  contentType?: string;
  sizeBytes?: number;
  parseStatus?: string;
  docType?: string;
  artifactConfig?: any;
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
  const [exporting, setExporting] = React.useState(false);
  const [fullDoc, setFullDoc] = React.useState<CoreDoc | null>(null);

  const isEntities = (doc.filename || '').toLowerCase() === 'entities.json';
  const isArtifactDoc = doc.docType === 'dynamic_artifact' && doc.artifactConfig;

  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  // Load full document if it's an artifact
  React.useEffect(() => {
    if (isArtifactDoc) {
      (async () => {
        try {
          const docs = await api.docsList(projectId);
          const foundDoc = docs.find((d: CoreDoc) => d.id === doc.id);
          if (foundDoc && isArtifact(foundDoc)) {
            setFullDoc(foundDoc);
          }
        } catch (e) {
          console.error('[DocumentViewer] Failed to load full document', e);
        }
      })();
    }
  }, [projectId, doc.id, isArtifactDoc]);

  React.useEffect(() => {
    // Skip loading blob for artifacts - we'll render structured view
    if (isArtifactDoc) {
      setLoading(false);
      return;
    }

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
  }, [projectId, doc.id, isArtifactDoc]);

  const headerTitle = isEntities ? 'Entities container' : (doc.filename || 'document');
  const fileExt = (doc.filename.split('.').pop() || '').toLowerCase();
  const isMarkdown = fileExt === 'md' || (doc.contentType || '').includes('markdown');

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

  const exportToDocx = async () => {
    try {
      setExporting(true);
      const blob = await api.docsExportToDocx(projectId, doc.id);
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = doc.filename.replace(/\.(md|txt)$/i, '.docx') || 'export.docx';
      document.body.appendChild(a);
      a.click();
      setTimeout(() => { try { URL.revokeObjectURL(a.href); document.body.removeChild(a); } catch {} }, 0);
    } catch (error) {
      console.error('Failed to export to DOCX:', error);
    } finally {
      setExporting(false);
    }
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

  const renderArtifactStructure = () => {
    if (!fullDoc || !isArtifact(fullDoc)) {
      return <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Loading artifact structure...</div>;
    }

    const artifact = fullDoc as Artifact;
    const sections = [...(artifact.artifactConfig.structure || [])].sort((a, b) => a.order - b.order);

    return (
      <div className="p-3 space-y-4">
        {sections.length === 0 ? (
          <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>No sections yet.</div>
        ) : (
          sections.map((section) => (
            <div
              key={section.id}
              className="rounded-lg"
              style={{
                backgroundColor: 'var(--toolbar-bg)',
                border: '1px solid var(--overlay-line-90)',
              }}
            >
              <div
                className="px-4 py-3 border-b"
                style={{ borderColor: 'var(--overlay-line-90)' }}
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="flex-1">
                    <h3
                      className="text-sm font-medium"
                      style={{ color: 'var(--text-heading)' }}
                    >
                      {section.title}
                      {section.required && (
                        <span
                          className="ml-2 text-xs"
                          style={{ color: 'var(--text-tertiary-weak)' }}
                        >
                          (required)
                        </span>
                      )}
                    </h3>
                    {section.description && (
                      <p
                        className="text-xs mt-1"
                        style={{ color: 'var(--text-secondary-strong)' }}
                      >
                        {section.description}
                      </p>
                    )}
                  </div>
                </div>
              </div>
              <div className="px-4 py-3">
                {section.content ? (
                  <div className="markdown text-xs" style={{ color: 'var(--text-heading)' }}>
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {section.content}
                    </ReactMarkdown>
                  </div>
                ) : (
                  <div
                    className="text-xs py-2"
                    style={{ color: 'var(--text-tertiary-weak)' }}
                  >
                    *[Section not generated yet]*
                  </div>
                )}
              </div>
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
    // Render markdown with formatting
    if (isMarkdown && textContent) {
      return (
        <div className="p-3 markdown text-13" style={{ color: 'var(--text-heading)' }}>
          <ReactMarkdown remarkPlugins={[remarkGfm]}>
            {textContent}
          </ReactMarkdown>
        </div>
      );
    }
    return (
      <pre className="p-3 text-12" style={{ color: 'var(--text-heading)', background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', borderRadius: 8, overflow: 'auto' }}>
        {textContent || ''}
      </pre>
    );
  };

  const ct = (doc.contentType || '').toLowerCase();
  const isImage = /^(png|jpg|jpeg|gif|webp|bmp|svg)$/.test(fileExt) || ct.startsWith('image/');
  const isPdf = fileExt === 'pdf' || ct.includes('pdf');
  const isTextLike = isMarkdown || ct.startsWith('text/') || /(json|csv|tsv)/.test(ct) || /(md|txt|csv|tsv|json)$/i.test(fileExt);

  const renderBody = () => {
    // Render structured view for artifacts
    if (isArtifactDoc) return renderArtifactStructure();
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
    <div className="absolute inset-0 z-[70] rounded-xl" style={{ background: 'var(--bg-input)', border: '1px solid var(--overlay-line-90)', overflow: 'hidden' }}>
      <div className="flex items-center justify-between px-3 py-2" style={{ borderBottom: '1px solid var(--overlay-line-90)' }}>
        <div className="text-13" style={{ color: 'var(--text-heading)' }} title={doc.filename}>{headerTitle}</div>
        <div className="flex items-center gap-2">
          {(isMarkdown || isArtifactDoc) && (
            <button 
              type="button" 
              className="px-2 py-0.5 rounded text-[11px] font-medium transition-colors shrink-0 flex items-center gap-1"
              onClick={exportToDocx} 
              disabled={exporting}
              title="Export to Word" 
              aria-label="Export to Word" 
              style={{
                background: exporting ? 'var(--white-06)' : 'var(--white-10)',
                color: exporting ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
                border: '1px solid var(--overlay-line-90)',
                cursor: exporting ? 'not-allowed' : 'pointer',
                opacity: exporting ? 0.6 : 1
              }}
              onMouseEnter={(e) => {
                if (!exporting) {
                  (e.currentTarget as HTMLButtonElement).style.opacity = '0.85';
                }
              }}
              onMouseLeave={(e) => {
                if (!exporting) {
                  (e.currentTarget as HTMLButtonElement).style.opacity = '1';
                }
              }}
            >
              {exporting ? 'Exporting...' : 'Export to Word'}
            </button>
          )}
          <button 
            type="button" 
            className="px-2 py-0.5 rounded text-[11px] font-medium transition-colors shrink-0 flex items-center gap-1"
            onClick={download} 
            title="Download" 
            aria-label="Download" 
            style={{
              background: 'var(--white-10)',
              color: 'var(--text-heading)',
              border: '1px solid var(--overlay-line-90)',
              cursor: 'pointer',
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLButtonElement).style.opacity = '0.85';
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.opacity = '1';
            }}
          >
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

import React from 'react';
import { api } from '../lib/api';

interface Document {
  id: string;
  filename: string;
  contentType?: string;
  sizeBytes?: number;
  parseStatus?: string;
  createdAt?: string;
}

interface Source {
  id: string;
  displayName: string;
  kind: string;
  status?: string;
}

interface DocumentsAndSourcesProps {
  projectId: string | null;
  onDocumentDelete?: (docId: string) => void;
  onSourceDelete?: (sourceId: string) => void;
  onOpenDocument?: (doc: Document) => void;
}

const DocumentsAndSources: React.FC<DocumentsAndSourcesProps> = ({ 
  projectId, 
  onDocumentDelete,
  onSourceDelete,
  onOpenDocument,
}) => {
  const [documents, setDocuments] = React.useState<Document[]>([]);
  const [sources, setSources] = React.useState<Source[]>([]);
  const [loading, setLoading] = React.useState(false);

  // Load documents when projectId changes
  React.useEffect(() => {
    if (!projectId || projectId === 'local-unfold') return;
    
    const loadDocuments = async () => {
      setLoading(true);
      try {
        const docs = await api.docsList(projectId);
        setDocuments(docs || []);
      } catch (err) {
        console.error('[DocumentsAndSources] Failed to load documents', err);
      } finally {
        setLoading(false);
      }
    };

    loadDocuments();
  }, [projectId]);

  const getFileIcon = (filename: string, contentType?: string) => {
    const ext = filename.split('.').pop()?.toLowerCase();
    const type = contentType?.toLowerCase() || '';

    // PDF
    if (ext === 'pdf' || type.includes('pdf')) {
      return (
        <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M14 2v6h6M10 14h4M10 18h4M10 10h1" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      );
    }

    // Word documents
    if (ext === 'doc' || ext === 'docx' || type.includes('word') || type.includes('document')) {
      return (
        <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M14 2v6h6M9 15l2 2 4-4" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      );
    }

    // CSV / TSV
    if (ext === 'csv' || ext === 'tsv' || type.includes('csv')) {
      return (
        <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M14 2v6h6M8 13h8M8 17h8M8 9h2" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      );
    }

    // Markdown
    if (ext === 'md' || type.includes('markdown')) {
      return (
        <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M14 2v6h6M11 14l-2-2v4M13 14l2-2v4" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      );
    }

    // Text files (default)
    return (
      <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
        <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  };

  const getSourceIcon = (kind: string) => {
    switch (kind.toLowerCase()) {
      case 'telegram':
        return (
          <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm4.64 6.8c-.15 1.58-.8 5.42-1.13 7.19-.14.75-.42 1-.68 1.03-.58.05-1.02-.38-1.58-.75-.88-.58-1.38-.94-2.23-1.5-.99-.65-.35-1.01.22-1.59.15-.15 2.71-2.48 2.76-2.69a.2.2 0 00-.05-.18c-.06-.05-.14-.03-.21-.02-.09.02-1.49.95-4.22 2.79-.4.27-.76.41-1.08.4-.36-.01-1.04-.2-1.55-.37-.63-.2-1.12-.31-1.08-.66.02-.18.27-.36.74-.55 2.92-1.27 4.86-2.11 5.83-2.51 2.78-1.16 3.35-1.36 3.73-1.36.08 0 .27.02.39.12.1.08.13.19.14.27-.01.06.01.24 0 .38z"/>
          </svg>
        );
      case 'web':
      case 'url':
        return (
          <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <circle cx="12" cy="12" r="10" strokeWidth="2" />
            <path d="M2 12h20M12 2a15.3 15.3 0 014 10 15.3 15.3 0 01-4 10 15.3 15.3 0 01-4-10 15.3 15.3 0 014-10z" strokeWidth="2" />
          </svg>
        );
      default:
        return (
          <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path d="M3 3h7l7 7v11a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        );
    }
  };

  const formatSize = (bytes?: number): string => {
    if (!bytes) return '';
    if (bytes < 1024) return `${bytes}B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
  };

  const handleDocumentDelete = async (doc: Document, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    if (!projectId || projectId === 'local-unfold') return;
    
    try {
      await api.docsDelete(projectId, doc.id);
      setDocuments(prev => prev.filter(d => d.id !== doc.id));
      onDocumentDelete?.(doc.id);
      console.log('[DocumentsAndSources] Document deleted', { docId: doc.id });
    } catch (err) {
      console.error('[DocumentsAndSources] Failed to delete document', err);
    }
  };

  const handleSourceDelete = async (source: Source) => {
    // TODO: Implement source deletion when API is ready
    console.log('[DocumentsAndSources] Source delete requested', { sourceId: source.id });
    onSourceDelete?.(source.id);
  };

  const getDocumentDisplayName = (filename: string) => {
    try {
      const f = (filename || '').toLowerCase();
      if (f === 'entities.json') return 'Entities container';
      if (f === 'facts.json') return 'Facts container';
      if (f === 'events.json') return 'Events container';
      if (f === 'metrics.json') return 'Metrics container';
      if (f === 'decisions.json') return 'Decisions container';
      return filename;
    } catch {
      return filename;
    }
  };

  if (!projectId || projectId === 'local-unfold') {
    return (
      <div className="flex items-center justify-center py-8" style={{ color: 'var(--text-secondary)' }}>
        <span className="text-xs">This chat has no documents</span>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8" style={{ color: 'var(--text-secondary)' }}>
        <div className="animate-pulse text-xs">Loading...</div>
      </div>
    );
  }

  const isEmpty = documents.length === 0 && sources.length === 0;

  if (isEmpty) {
    return (
      <div className="flex flex-col items-center justify-center py-8 px-4" style={{ color: 'var(--text-secondary)' }}>
        <svg className="w-12 h-12 mb-2 opacity-30" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span className="text-xs text-center">No documents or sources yet</span>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Documents section */}
      {documents.length > 0 && (
        <div>
          <h4 className="text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            Documents ({documents.length})
          </h4>
          <div className="space-y-1">
            {documents.map((doc) => (
              <div
                key={doc.id}
                className="flex items-center gap-2 px-2 py-1.5 rounded group hover:bg-white/5 transition-colors cursor-pointer"
                onClick={() => onOpenDocument?.(doc)}
              >
                {/* Icon */}
                <div className="flex-shrink-0" style={{ color: 'var(--text-secondary)' }}>
                  {getFileIcon(doc.filename, doc.contentType)}
                </div>

                {/* File info */}
                <div className="flex-1 min-w-0">
                  <div className="text-xs font-medium truncate" style={{ color: 'var(--text-heading)' }} title={doc.filename}>
                    {getDocumentDisplayName(doc.filename)}
                  </div>
                  <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                    {formatSize(doc.sizeBytes)}
                    {doc.parseStatus && ` • ${doc.parseStatus}`}
                  </div>
                </div>

                {/* Delete button */}
                <button
                  type="button"
                  onClick={(e) => handleDocumentDelete(doc, e)}
                  className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full opacity-0 group-hover:opacity-100 hover:bg-white/10 transition-all"
                  style={{ color: 'var(--text-secondary)' }}
                  aria-label="Delete document"
                  title="Delete document"
                >
                  <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Sources section (future) */}
      {sources.length > 0 && (
        <div>
          <h4 className="text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            Sources ({sources.length})
          </h4>
          <div className="space-y-1">
            {sources.map((source) => (
              <div
                key={source.id}
                className="flex items-center gap-2 px-2 py-1.5 rounded group hover:bg-white/5 transition-colors"
              >
                {/* Icon */}
                <div className="flex-shrink-0" style={{ color: 'var(--text-secondary)' }}>
                  {getSourceIcon(source.kind)}
                </div>

                {/* Source info */}
                <div className="flex-1 min-w-0">
                  <div className="text-xs font-medium truncate" style={{ color: 'var(--text-heading)' }} title={source.displayName}>
                    {source.displayName}
                  </div>
                  <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                    {source.kind}
                    {source.status && ` • ${source.status}`}
                  </div>
                </div>

                {/* Delete button */}
                <button
                  type="button"
                  onClick={() => handleSourceDelete(source)}
                  className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full opacity-0 group-hover:opacity-100 hover:bg-white/10 transition-all"
                  style={{ color: 'var(--text-secondary)' }}
                  aria-label="Delete source"
                  title="Delete source"
                >
                  <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </button>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};

export default DocumentsAndSources;


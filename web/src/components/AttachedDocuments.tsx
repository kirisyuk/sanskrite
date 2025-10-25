import React from 'react';

interface AttachedDoc {
  id: string;
  filename: string;
  sizeBytes?: number;
  status: 'uploading' | 'parsing' | 'parsed' | 'failed';
  error?: string;
}

interface AttachedDocumentsProps {
  docs: AttachedDoc[];
  onRemove: (docId: string) => void;
}

const AttachedDocuments: React.FC<AttachedDocumentsProps> = ({ docs, onRemove }) => {
  if (docs.length === 0) return null;

  const formatSize = (bytes?: number): string => {
    if (!bytes) return '';
    if (bytes < 1024) return `${bytes}B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
  };

  const getStatusIcon = (status: AttachedDoc['status']) => {
    switch (status) {
      case 'uploading':
        return (
          <div className="w-4 h-4 animate-spin" style={{ color: 'var(--text-secondary)' }}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <circle cx="12" cy="12" r="10" strokeWidth="2" strokeDasharray="60" strokeDashoffset="15" opacity="0.3" />
              <circle cx="12" cy="12" r="10" strokeWidth="2" strokeDasharray="60" strokeDashoffset="45" strokeLinecap="round" />
            </svg>
          </div>
        );
      case 'parsing':
        return (
          <div className="w-4 h-4 animate-pulse" style={{ color: 'var(--text-secondary)' }}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <path d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </div>
        );
      case 'parsed':
        return (
          <div className="w-4 h-4" style={{ color: '#4ade80' }}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <path d="M5 13l4 4L19 7" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </div>
        );
      case 'failed':
        return (
          <div className="w-4 h-4" style={{ color: '#f87171' }}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <circle cx="12" cy="12" r="10" strokeWidth="2" />
              <path d="M15 9l-6 6m0-6l6 6" strokeWidth="2" strokeLinecap="round" />
            </svg>
          </div>
        );
    }
  };

  const getStatusText = (status: AttachedDoc['status']) => {
    switch (status) {
      case 'uploading': return 'Uploading...';
      case 'parsing': return 'Processing...';
      case 'parsed': return 'Ready';
      case 'failed': return 'Failed';
    }
  };

  return (
    <div className="mb-2" style={{ backgroundColor: 'transparent' }}>
      <div className="space-y-1">
        {docs.map((doc) => (
          <div
            key={doc.id}
            className="flex items-center gap-2 px-2 py-1.5 rounded"
            style={{
              backgroundColor: doc.status === 'failed' ? 'rgba(248, 113, 113, 0.1)' : 'var(--white-06)',
            }}
          >
            {/* Status icon */}
            <div className="flex-shrink-0">
              {getStatusIcon(doc.status)}
            </div>

            {/* File info */}
            <div className="flex-1 min-w-0">
              <div className="flex items-baseline gap-2">
                <span
                  className="text-xs font-medium truncate"
                  style={{ 
                    color: doc.status === 'failed' ? '#f87171' : 'var(--text-heading)',
                  }}
                  title={doc.filename}
                >
                  {doc.filename}
                </span>
                {doc.sizeBytes && (
                  <span className="text-[10px] flex-shrink-0" style={{ color: 'var(--text-tertiary-weak)' }}>
                    {formatSize(doc.sizeBytes)}
                  </span>
                )}
              </div>
              <div className="text-[10px]" style={{ color: 'var(--text-secondary)' }}>
                {doc.status === 'failed' && doc.error ? doc.error : getStatusText(doc.status)}
              </div>
            </div>

            {/* Remove button */}
            <button
              type="button"
              onClick={() => onRemove(doc.id)}
              className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full hover:bg-white/10 transition-colors"
              style={{ color: 'var(--text-secondary)' }}
              aria-label="Remove document"
              title="Remove document"
            >
              <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
          </div>
        ))}
      </div>
    </div>
  );
};

export default AttachedDocuments;


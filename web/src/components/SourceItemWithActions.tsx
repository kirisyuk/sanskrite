import React from 'react';
import { api } from '../lib/api';

interface Source {
  id: string;
  displayName: string;
  kind: string;
  status?: string;
  lastPolledAt?: string;
  nextPollAt?: string;
}

interface SourceItemWithActionsProps {
  source: Source;
  projectId: string | null;
  onStatusChange?: (sourceId: string, newStatus: 'active' | 'paused') => void;
  onDelete?: (sourceId: string) => void;
  onPreview?: (source: Source) => void;
  onViewDocuments?: (sourceId: string) => void;
}

const getSourceIcon = (kind: string) => {
  switch (kind.toLowerCase()) {
    case 'telegram':
    case 'telegram_channel':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
          <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm4.64 6.8c-.15 1.58-.8 5.42-1.13 7.19-.14.75-.42 1-.68 1.03-.58.05-1.02-.38-1.58-.75-.88-.58-1.38-.94-2.23-1.5-.99-.65-.35-1.01.22-1.59.15-.15 2.71-2.48 2.76-2.69a.2.2 0 00-.05-.18c-.06-.05-.14-.03-.21-.02-.09.02-1.49.95-4.22 2.79-.4.27-.76.41-1.08.4-.36-.01-1.04-.2-1.55-.37-.63-.2-1.12-.31-1.08-.66.02-.18.27-.36.74-.55 2.92-1.27 4.86-2.11 5.83-2.51 2.78-1.16 3.35-1.36 3.73-1.36.08 0 .27.02.39.12.1.08.13.19.14.27-.01.06.01.24 0 .38z"/>
        </svg>
      );
    case 'rss':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <circle cx="6" cy="18" r="3" strokeWidth="2" />
          <path d="M6 9a9 9 0 0 1 9 9M6 3a15 15 0 0 1 15 15" strokeWidth="2" strokeLinecap="round" />
        </svg>
      );
    case 'web':
    case 'url':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <circle cx="12" cy="12" r="10" strokeWidth="2" />
          <path d="M2 12h20M12 2a15.3 15.3 0 014 10 15.3 15.3 0 01-4 10 15.3 15.3 0 01-4-10 15.3 15.3 0 014-10z" strokeWidth="2" />
        </svg>
      );
    default:
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M3 3h7l7 7v11a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      );
  }
};

export const SourceItemWithActions: React.FC<SourceItemWithActionsProps> = ({
  source,
  projectId,
  onStatusChange,
  onDelete,
  onPreview,
  onViewDocuments,
}) => {
  const [isIngesting, setIsIngesting] = React.useState(false);

  const handleStatusChange = async (newStatus: 'active' | 'paused') => {
    if (!projectId || projectId === 'local-unfold') return;
    if (source.status === newStatus) return; // No change needed
    
    try {
      await api.updateSourceProjectStatus(projectId, source.id, newStatus);
      onStatusChange?.(source.id, newStatus);
    } catch (err) {
      console.error('[SourceItemWithActions] Failed to update source status', err);
      alert('Failed to update source status');
    }
  };

  const handleDelete = async () => {
    if (!projectId || projectId === 'local-unfold') return;
    
    try {
      await api.detachSource(projectId, source.id);
      onDelete?.(source.id);
    } catch (err) {
      console.error('[SourceItemWithActions] Failed to detach source', err);
      alert('Failed to detach source from project');
    }
  };

  const handleIngest = async () => {
    if (!projectId || projectId === 'local-unfold') return;
    
    setIsIngesting(true);
    try {
      await api.ingestSource(source.id);
      alert('Ingest started successfully');
    } catch (err) {
      console.error('[SourceItemWithActions] Failed to start ingest', err);
      alert('Failed to start ingest');
    } finally {
      setIsIngesting(false);
    }
  };

  const handleViewDocuments = () => {
    onViewDocuments?.(source.id);
  };

  return (
    <div
      className="px-2 py-1.5 rounded text-xs flex items-center gap-2 group"
      style={{
        backgroundColor: 'transparent',
        cursor: onPreview ? 'pointer' : 'default',
      }}
      onClick={(e) => {
        // Don't trigger preview if clicking on buttons
        if ((e.target as HTMLElement).closest('button')) {
          return;
        }
        onPreview?.(source);
      }}
      onMouseEnter={(e) => {
        (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)';
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent';
      }}
    >
      {/* Icon */}
      <div className="flex-shrink-0" style={{ color: 'var(--text-secondary)' }}>
        {getSourceIcon(source.kind)}
      </div>

      {/* Source info */}
      <div className="flex-1 min-w-0">
        <div className="font-medium truncate" style={{ color: 'var(--text-heading)' }} title={source.displayName}>
          {source.displayName}
        </div>
        <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
          {source.kind}
          {source.status && ` • ${source.status}`}
        </div>
        <div className="text-[9px] mt-0.5" style={{ color: 'var(--text-tertiary-weak)' }}>
          {(source.lastPolledAt || source.nextPollAt) ? (
            <>
              {source.lastPolledAt && (
                <span>Last: {new Date(source.lastPolledAt).toLocaleString('ru-RU', { 
                  year: 'numeric', 
                  month: '2-digit', 
                  day: '2-digit', 
                  hour: '2-digit', 
                  minute: '2-digit' 
                })}</span>
              )}
              {source.lastPolledAt && source.nextPollAt && source.status !== 'paused' && ' • '}
              {source.nextPollAt && source.status !== 'paused' && (
                <span>Next: {new Date(source.nextPollAt).toLocaleString('ru-RU', { 
                  year: 'numeric', 
                  month: '2-digit', 
                  day: '2-digit', 
                  hour: '2-digit', 
                  minute: '2-digit' 
                })}</span>
              )}
              {source.status === 'paused' && source.lastPolledAt && (
                <span className="ml-1" style={{ fontStyle: 'italic' }}>(paused)</span>
              )}
              {source.status === 'paused' && !source.lastPolledAt && source.nextPollAt && (
                <span style={{ fontStyle: 'italic' }}>Next: {new Date(source.nextPollAt).toLocaleString('ru-RU', { 
                  year: 'numeric', 
                  month: '2-digit', 
                  day: '2-digit', 
                  hour: '2-digit', 
                  minute: '2-digit' 
                })} (paused)</span>
              )}
            </>
          ) : (
            <span style={{ fontStyle: 'italic', opacity: 0.5 }}>
              Last: — • Next: — (здесь появится время последнего и следующего ingest)
            </span>
          )}
        </div>
      </div>

      {/* Action buttons */}
      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
        {/* Ingest button */}
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            handleIngest();
          }}
          disabled={isIngesting}
          className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full hover:bg-white/10 transition-all disabled:opacity-50"
          style={{ color: 'var(--accent-primary)' }}
          aria-label="Start ingest"
          title="Start ingest"
        >
          {isIngesting ? (
            <svg className="w-3 h-3 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" strokeWidth="2" strokeLinecap="round" />
            </svg>
          ) : (
            <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          )}
        </button>

        {/* View documents button */}
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            handleViewDocuments();
          }}
          className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
          style={{ color: 'var(--text-secondary-strong)' }}
          aria-label="View documents"
          title="View documents"
        >
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>

        {/* Play/Pause button - показывает противоположное действие */}
        {source.status === 'paused' ? (
          // Если paused - показываем ▶ (play) для активации
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              handleStatusChange('active');
            }}
            className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
            style={{ color: 'var(--accent-success)' }}
            aria-label="Activate source"
            title="Activate source"
          >
            <svg className="w-3 h-3" viewBox="0 0 24 24" fill="currentColor">
              <path d="M8 5v14l11-7z" />
            </svg>
          </button>
        ) : (
          // Если active (или нет статуса) - показываем ⏸ (pause) для паузы
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              handleStatusChange('paused');
            }}
            className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
            style={{ color: 'var(--text-secondary-strong)' }}
            aria-label="Pause source"
            title="Pause source"
          >
            <svg className="w-3 h-3" viewBox="0 0 24 24" fill="currentColor">
              <path d="M6 4h4v16H6V4zm8 0h4v16h-4V4z" />
            </svg>
          </button>
        )}
        
        {/* Delete button */}
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            handleDelete();
          }}
          className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
          style={{ color: 'var(--text-secondary)' }}
          aria-label="Delete source"
          title="Delete source"
        >
          <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
      </div>
    </div>
  );
};


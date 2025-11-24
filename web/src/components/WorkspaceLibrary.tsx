import React from 'react';
import { api } from '../lib/api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useDiscoveryStore } from '../stores/discoveryStore';
import { SourceItemWithActions } from './SourceItemWithActions';
import SourcePreview from './SourcePreview';
import { SourceDocumentsViewer } from './SourceDocumentsViewer';
import type { DataSource } from '../types/generated';

interface Document {
  id: string;
  filename: string;
  contentType?: string;
  sizeBytes?: number;
  parseStatus?: string;
  createdAt?: string;
  extractedText?: string;
  docType?: string;
  artifactConfig?: any;
}

interface Source {
  id: string;
  displayName: string;
  kind: string;
  status?: string;
  lastPolledAt?: string;
  nextPollAt?: string;
}

interface WorkspaceLibraryProps {
  projectId: string | null;
  onDocumentDelete?: (docId: string) => void;
  onSourceDelete?: (sourceId: string) => void;
  onOpenDocument?: (doc: Document) => void;
  onExpand?: (projectId: string | null) => void;
}

const WorkspaceLibrary: React.FC<WorkspaceLibraryProps> = ({ 
  projectId, 
  onDocumentDelete,
  onSourceDelete,
  onOpenDocument,
  onExpand,
}) => {
  const [documents, setDocuments] = React.useState<Document[]>([]);
  const [sources, setSources] = React.useState<Source[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [editingDocId, setEditingDocId] = React.useState<string | null>(null);
  const [editingDocName, setEditingDocName] = React.useState<string>('');
  const editingDocInputRef = React.useRef<HTMLInputElement>(null);
  const pendingAttachedDocIds = useGenaiChatStore((state) => state.pendingAttachedDocIds);
  const setPendingAttachedDocIds = useGenaiChatStore((state) => state.setPendingAttachedDocIds);
  const [previewSource, setPreviewSource] = React.useState<DataSource | null>(null);
  const [documentsViewSource, setDocumentsViewSource] = React.useState<DataSource | null>(null);
  
  // Subscribe to WebSocket document events
  const lastDocEvent = useDiscoveryStore((state: any) => state.lastDocEvent);
  const lastProcessedEventRef = React.useRef<{ docId: string; status: string; ts: number } | null>(null);
  
  // Sync sources with workspace store
  const setProjectSources = useWorkspaceStore((s) => s.setProjectSources);
  const updateSourceStatus = useWorkspaceStore((s) => s.updateSourceStatus);
  const storeProjectSources = useWorkspaceStore((s) => projectId ? s.projectSources[projectId] : undefined);
  const sourceStatusUpdates = useWorkspaceStore((s) => s.sourceStatusUpdates);

  // PERF: Track mount time
  React.useEffect(() => {
    const mountTime = performance.now();
    console.log('[WorkspaceLibrary] Component mounted at', mountTime);
    return () => {
      const unmountTime = performance.now();
      console.log('[WorkspaceLibrary] Component unmounted at', unmountTime, 'lifetime:', unmountTime - mountTime, 'ms');
    };
  }, []);

  // Focus input when editing starts
  React.useEffect(() => {
    if (editingDocId && editingDocInputRef.current) {
      editingDocInputRef.current.focus();
      editingDocInputRef.current.select();
    }
  }, [editingDocId]);

  // Load documents and sources when projectId changes
  React.useEffect(() => {
    if (!projectId || projectId === 'local-unfold') return;
    
    const loadData = async () => {
      const startTime = performance.now();
      console.log('[WorkspaceLibrary] Starting data load at', startTime);
      setLoading(true);
      try {
        // Load documents, excluding automatic post documents - they will be shown separately
        const docs = await api.docsList(projectId, 'post');
        const docsTime = performance.now();
        console.log('[WorkspaceLibrary] Document load completed at', docsTime, 'took', docsTime - startTime, 'ms');
        setDocuments(docs || []);

        // Load sources
        try {
          const srcs = await api.getProjectSources(projectId);
          const sourcesTime = performance.now();
          console.log('[WorkspaceLibrary] Sources load completed at', sourcesTime, 'took', sourcesTime - docsTime, 'ms');
          // Transform DataSource to Source format
          const transformedSources: Source[] = (srcs || []).map((s: any) => {
            // Extract displayName from various possible fields
            let displayName = s.displayName || s.name;
            if (!displayName && s.details) {
              displayName = s.details.uri || s.details.channelUsername || s.details.url;
            }
            if (!displayName) {
              displayName = s.kind || 'Unknown';
            }
            return {
              id: s.id,
              displayName,
              kind: s.kind || 'unknown',
              status: s.status || 'active',
              lastPolledAt: s.lastPolledAt,
              nextPollAt: s.nextPollAt,
            };
          });
          setSources(transformedSources);
        } catch (err) {
          console.error('[WorkspaceLibrary] Failed to load sources', err);
          setSources([]);
        }
      } catch (err) {
        console.error('[WorkspaceLibrary] Failed to load documents', err);
      } finally {
        setLoading(false);
      }
    };

    loadData();
  }, [projectId, setProjectSources]);
  
  // Sync sources from store when they change (e.g., from WorkspaceCanvas)
  React.useEffect(() => {
    if (!projectId || !storeProjectSources) return;
    
    const transformedSources: Source[] = storeProjectSources.map((s: DataSource) => {
      let displayName: string = s.displayName || '';
      if (!displayName && s.sourceDetails) {
        displayName = s.sourceDetails.uri || s.sourceDetails.channelUsername || s.sourceDetails.sourceUri || '';
      }
      if (!displayName) {
        displayName = s.kind || 'Unknown';
      }
      return {
        id: s.id,
        displayName,
        kind: s.kind || 'unknown',
        status: s.status || 'active',
        lastPolledAt: s.lastPolledAt,
        nextPollAt: s.nextPollAt,
      };
    });
    
    // Use a ref to track previous sources to avoid infinite loops
    setSources(prevSources => {
      const currentIds = prevSources.map(s => s.id).sort().join(',');
      const newIds = transformedSources.map(s => s.id).sort().join(',');
      if (currentIds !== newIds) {
        return transformedSources;
      }
      // Check if status changed
      const statusChanged = transformedSources.some(newSource => {
        const oldSource = prevSources.find(s => s.id === newSource.id);
        return oldSource && oldSource.status !== newSource.status;
      });
      return statusChanged ? transformedSources : prevSources;
    });
  }, [projectId, storeProjectSources, sourceStatusUpdates]);

  // Handle WebSocket document events - update documents list in real-time
  React.useEffect(() => {
    if (!lastDocEvent || !projectId || projectId === 'local-unfold') return;
    
    const eventDocId = lastDocEvent.docId;
    const eventProjectId = lastDocEvent.projectId;
    const eventStatus = lastDocEvent.status;
    const eventTs = lastDocEvent.ts || 0;
    
    // Only process events for current project
    if (eventProjectId !== projectId) return;
    
    // Deduplicate events - skip if we already processed this exact event
    const eventKey = `${eventDocId}:${eventStatus}:${eventTs}`;
    const lastProcessed = lastProcessedEventRef.current;
    if (lastProcessed && 
        lastProcessed.docId === eventDocId && 
        lastProcessed.status === eventStatus && 
        lastProcessed.ts === eventTs) {
      console.log('[WorkspaceLibrary] Skipping duplicate event:', { eventDocId, eventStatus, eventTs });
      return;
    }
    
    // Mark as processed
    lastProcessedEventRef.current = { docId: eventDocId, status: eventStatus, ts: eventTs };
    
    console.log('[WorkspaceLibrary] Processing document event:', { eventDocId, eventStatus, eventProjectId });
    
    // Handle document deletion
    if (eventStatus === 'deleted') {
      setDocuments(prev => prev.filter(d => d.id !== eventDocId));
      return;
    }
    
    // For uploaded/parsing/parsed statuses, fetch or update document
    if (eventStatus === 'uploaded' || eventStatus === 'parsing' || eventStatus === 'parsed') {
      // Check if document already exists in list
      setDocuments(prev => {
        const existingDoc = prev.find(d => d.id === eventDocId);
        
        if (existingDoc) {
          // Update existing document status
          return prev.map(doc => 
            doc.id === eventDocId 
              ? { ...doc, parseStatus: eventStatus }
              : doc
          );
        } else {
          // Document doesn't exist yet - fetch it from API
          api.docsList(projectId, 'post').then(docs => {
            const newDoc = docs?.find((d: Document) => d.id === eventDocId);
            if (newDoc) {
              setDocuments(prevDocs => {
                // Avoid duplicates
                if (prevDocs.find(d => d.id === eventDocId)) {
                  return prevDocs.map(doc => 
                    doc.id === eventDocId 
                      ? { ...doc, parseStatus: eventStatus }
                      : doc
                  );
                }
                return [...prevDocs, newDoc];
              });
            }
          }).catch(err => {
            console.error('[WorkspaceLibrary] Failed to fetch new document:', err);
          });
          return prev; // Return unchanged while fetching
        }
      });
    }
  }, [lastDocEvent, projectId]);

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
      console.log('[WorkspaceLibrary] Document deleted', { docId: doc.id });
    } catch (err) {
      console.error('[WorkspaceLibrary] Failed to delete document', err);
    }
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

  const handleDocumentToggle = (docId: string, e: React.MouseEvent) => {
    e.stopPropagation();
    const isAttached = pendingAttachedDocIds.includes(docId);
    if (isAttached) {
      setPendingAttachedDocIds(pendingAttachedDocIds.filter(id => id !== docId));
    } else {
      setPendingAttachedDocIds([...pendingAttachedDocIds, docId]);
    }
  };

  if (!projectId || projectId === 'local-unfold') {
    return (
      <div className="flex items-center justify-center py-8 text-center" style={{ color: 'var(--text-secondary)' }}>
        <span className="text-xs">No documents, no sources. Nothing.<br />But you may add or create.</span>
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
        <span className="text-xs text-center">Your workspace library is empty</span>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Header with expand button - always visible if onExpand is provided */}
      {onExpand && (
        <div className="flex items-center justify-between mb-2">
          <h4 className="text-xs font-medium" style={{ color: 'var(--text-secondary-strong)' }}>
            Workspace Library
          </h4>
          <button
            type="button"
            onClick={() => {
              console.log('[WorkspaceLibrary] Expand clicked', { projectId });
              onExpand?.(projectId);
            }}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded text-[11px] font-medium transition-colors"
            style={{
              color: 'var(--text-heading)',
              backgroundColor: 'var(--white-06)',
              border: '1px solid var(--overlay-line-90)',
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.backgroundColor = 'var(--white-10)';
              e.currentTarget.style.borderColor = 'var(--overlay-line-80)';
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.backgroundColor = 'var(--white-06)';
              e.currentTarget.style.borderColor = 'var(--overlay-line-90)';
            }}
            title="Expand to full screen"
            aria-label="Expand workspace library to full screen"
          >
            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <span>Expand</span>
          </button>
        </div>
      )}

      {/* Documents section */}
      {documents.length > 0 && (
        <div>
          <h4 className="text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            Documents ({documents.length})
          </h4>
          <div className="space-y-1">
            {documents.map((doc) => {
              const isAttached = pendingAttachedDocIds.includes(doc.id);
              return (
                <div
                  key={doc.id}
                  className="flex items-center gap-2 px-2 py-1.5 rounded group hover:bg-white/5 transition-colors"
                >
                  {/* Checkbox for attaching to context */}
                  <input
                    type="checkbox"
                    checked={isAttached}
                    onChange={() => {}}
                    onClick={(e) => handleDocumentToggle(doc.id, e)}
                    className="flex-shrink-0 w-4 h-4 rounded cursor-pointer"
                    style={{ 
                      accentColor: 'var(--accent-primary)',
                      cursor: 'pointer'
                    }}
                    aria-label="Attach to context"
                    title="Attach to context for next message"
                  />

                  {/* Icon and file info - clickable to open */}
                  <div
                    className="flex items-center gap-2 flex-1 min-w-0 cursor-pointer"
                    onClick={() => {
                      if (editingDocId !== doc.id) {
                        onOpenDocument?.(doc);
                      }
                    }}
                  >
                    {/* Icon */}
                    <div className="flex-shrink-0" style={{ color: 'var(--text-secondary)' }}>
                      {getFileIcon(doc.filename, doc.contentType)}
                    </div>

                    {/* File info */}
                    <div className="flex-1 min-w-0">
                      {editingDocId === doc.id ? (
                        <input
                          ref={editingDocInputRef}
                          type="text"
                          value={editingDocName}
                          onChange={(e) => setEditingDocName(e.target.value)}
                          onKeyDown={async (e) => {
                            if (e.key === 'Enter') {
                              e.stopPropagation();
                              if (!projectId || projectId === 'local-unfold') return;
                              const newName = editingDocName.trim();
                              if (newName && newName !== doc.filename) {
                                try {
                                  await api.docsUpdate(projectId, doc.id, { filename: newName });
                                  const docs = await api.docsList(projectId, 'post');
                                  setDocuments(docs || []);
                                } catch (err) {
                                  console.error('[WorkspaceLibrary] Failed to rename document', err);
                                  alert('Failed to rename document');
                                }
                              }
                              setEditingDocId(null);
                              setEditingDocName('');
                            } else if (e.key === 'Escape') {
                              e.stopPropagation();
                              setEditingDocId(null);
                              setEditingDocName('');
                            }
                          }}
                          onBlur={async () => {
                            if (!projectId || projectId === 'local-unfold') return;
                            const newName = editingDocName.trim();
                            if (newName && newName !== doc.filename) {
                              try {
                                await api.docsUpdate(projectId, doc.id, { filename: newName });
                                const docs = await api.docsList(projectId, 'post');
                                setDocuments(docs || []);
                              } catch (err) {
                                console.error('[WorkspaceLibrary] Failed to rename document', err);
                                alert('Failed to rename document');
                              }
                            }
                            setEditingDocId(null);
                            setEditingDocName('');
                          }}
                          className="text-xs font-medium px-1 py-0.5 rounded"
                          style={{
                            backgroundColor: 'var(--bg-input, #1A1A1A)',
                            color: 'var(--text-heading)',
                            border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                            width: '100%',
                          }}
                          onClick={(e) => e.stopPropagation()}
                        />
                      ) : (
                        <div className="text-xs font-medium truncate" style={{ color: 'var(--text-heading)' }} title={doc.filename}>
                          {getDocumentDisplayName(doc.filename)}
                        </div>
                      )}
                      {editingDocId !== doc.id && (
                        <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                          {formatSize(doc.sizeBytes)}
                          {doc.parseStatus && ` • ${doc.parseStatus}`}
                        </div>
                      )}
                    </div>
                  </div>

                  {/* Action buttons */}
                  <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        setEditingDocId(doc.id);
                        setEditingDocName(doc.filename);
                        setTimeout(() => editingDocInputRef.current?.focus(), 0);
                      }}
                      className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
                      style={{ color: 'var(--text-secondary)' }}
                      aria-label="Rename document"
                      title="Rename document"
                    >
                      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      onClick={(e) => handleDocumentDelete(doc, e)}
                      className="flex-shrink-0 w-5 h-5 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
                      style={{ color: 'var(--text-secondary)' }}
                      aria-label="Delete document"
                      title="Delete document"
                    >
                      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                      </svg>
                    </button>
                  </div>
                </div>
              );
            })}
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
              <SourceItemWithActions
                key={source.id}
                source={source}
                projectId={projectId}
                onStatusChange={async (sourceId, newStatus) => {
                  // Update local state immediately
                  setSources(prev => prev.map(s => 
                    s.id === sourceId ? { ...s, status: newStatus } : s
                  ));
                  // Update store for synchronization with WorkspaceCanvas
                  updateSourceStatus(sourceId, newStatus);
                  // Also update store sources for this project
                  if (projectId && storeProjectSources) {
                    const updated = storeProjectSources.map(s =>
                      s.id === sourceId ? { ...s, status: newStatus } : s
                    );
                    setProjectSources(projectId, updated);
                  }
                }}
                onDelete={(sourceId) => {
                  // Update local state immediately
                  setSources(prev => prev.filter(s => s.id !== sourceId));
                  // Update store for synchronization with WorkspaceCanvas
                  if (projectId && storeProjectSources) {
                    const updated = storeProjectSources.filter(s => s.id !== sourceId);
                    setProjectSources(projectId, updated);
                  }
                  onSourceDelete?.(sourceId);
                }}
                onPreview={async (source) => {
                  if (!projectId || projectId === 'local-unfold') return;
                  try {
                    // Load full source data with profile
                    const allSources = await api.getProjectSources(projectId);
                    const fullSource = allSources.find((s: DataSource) => s.id === source.id);
                    if (fullSource) {
                      setPreviewSource(fullSource);
                    }
                  } catch (err) {
                    console.error('[WorkspaceLibrary] Failed to load source for preview', err);
                  }
                }}
                onViewDocuments={async (sourceId) => {
                  if (!projectId || projectId === 'local-unfold') return;
                  try {
                    // Load full source data
                    const allSources = await api.getProjectSources(projectId);
                    const fullSource = allSources.find((s: DataSource) => s.id === sourceId);
                    if (fullSource) {
                      setDocumentsViewSource(fullSource);
                    }
                  } catch (err) {
                    console.error('[WorkspaceLibrary] Failed to load source for documents view', err);
                  }
                }}
              />
            ))}
          </div>
        </div>
      )}

      {/* Source Preview */}
      {previewSource && (
        <SourcePreview
          source={previewSource}
          onClose={() => setPreviewSource(null)}
        />
      )}

      {/* Source Documents Viewer */}
      {documentsViewSource && (
        <SourceDocumentsViewer
          source={documentsViewSource}
          onClose={() => setDocumentsViewSource(null)}
        />
      )}
    </div>
  );
};

export default WorkspaceLibrary;


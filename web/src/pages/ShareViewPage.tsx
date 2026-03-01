import React from 'react';
import { useParams } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import ChatGraphMessage from '../components/ChatGraphMessage';
import ChatMessage from '../components/ChatMessage';
import { api } from '../lib/api';
import { useTranslation } from 'react-i18next';
import { ThemeContext } from '../components/ThemeProvider';
import logoDarkTheme from '../assets/etigram-dark-theme.png';
import logoLightTheme from '../assets/etigram-light-theme.png';

export default function ShareViewPage() {
  const { token } = useParams<{ token: string }>();
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [data, setData] = React.useState<any | null>(null);
  const [documents, setDocuments] = React.useState<any[]>([]);
  const [documentsLoading, setDocumentsLoading] = React.useState(false);
  const [viewingDoc, setViewingDoc] = React.useState<string | null>(null);
  const [docContent, setDocContent] = React.useState<string | null>(null);
  const { theme } = React.useContext(ThemeContext);
  const { t } = useTranslation();

  React.useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        if (!token) { setError('Missing token'); setLoading(false); return; }
        const res = await api.shareGetPublic(token);
        if (mounted) { setData(res); setLoading(false); }
      } catch (e: any) {
        if (mounted) { setError('Unavailable or expired'); setLoading(false); }
      }
    })();
    return () => { mounted = false; };
  }, [token]);

  // Load documents when token is available
  React.useEffect(() => {
    if (!token || !data) return;
    let mounted = true;
    setDocumentsLoading(true);
    (async () => {
      try {
        const docsRes = await api.shareListDocs(token);
        if (mounted) {
          setDocuments(docsRes.documents || []);
          setDocumentsLoading(false);
        }
      } catch (e: any) {
        if (mounted) {
          setDocuments([]);
          setDocumentsLoading(false);
        }
      }
    })();
    return () => { mounted = false; };
  }, [token, data]);

  // Build map of compliance warnings by assistant message ID (via replyTo or relations)
  // Must be called before early returns to follow React hooks rules
  const items = React.useMemo(() => {
    return Array.isArray(data?.interactions) ? data.interactions : [];
  }, [data]);

  // Separate perspective analysis interactions from regular messages
  const { regularItems, perspectiveAnalyses } = React.useMemo(() => {
    const regular: any[] = [];
    const analyses: any[] = [];
    
    items.forEach((it: any) => {
      // Check if this is a perspective analysis interaction
      if (it.subkind && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
        analyses.push(it);
      } else {
        regular.push(it);
      }
    });
    
    return { regularItems: regular, perspectiveAnalyses: analyses };
  }, [items]);

  const complianceWarnings = React.useMemo(() => {
    const map: Record<string, { confidence: number; explanation: string }> = {};
    items.forEach((it: any) => {
      // Check by subkind 'eval.frame_compliance' (ignore kind to be safe)
      if (it.subkind === 'eval.frame_compliance') {
        try {
          const content = it.contentSanitized || it.content || '{}';
          const parsed = JSON.parse(content);
          if (parsed.confidence >= 0.7 && parsed.explanation) {
            const warning = { confidence: parsed.confidence, explanation: parsed.explanation };
            // Collect all target IDs (replyTo and relations.targetId) to avoid duplicates
            const targetIds = new Set<string>();
            if (it.replyTo) {
              targetIds.add(it.replyTo);
            }
            if (it.relations && Array.isArray(it.relations)) {
              it.relations.forEach((rel: any) => {
                if (rel.targetId && (rel.type === 'compliance_check' || rel.type === 'evaluates')) {
                  targetIds.add(rel.targetId);
                }
              });
            }
            // Add warning once per target ID (avoid duplicates if replyTo == targetId)
            targetIds.forEach(targetId => {
              map[targetId] = warning;
            });
          }
        } catch (e) {
          // Ignore parse errors
        }
      }
    });
    return map;
  }, [items]);

  // Extract Discussion Frame from interaction with discussion.flow.analysis subkind
  // Check by subkind (ignore kind to be safe)
  const discussionFrame = React.useMemo(() => {
    const analysisInteraction = items.find(
      (it: any) => it.subkind === 'discussion.flow.analysis'
    );
    if (analysisInteraction?.content || analysisInteraction?.contentSanitized) {
      try {
        const content = analysisInteraction.contentSanitized || analysisInteraction.content || '{}';
        const parsed = JSON.parse(content);
        if (parsed.frame && typeof parsed.frame === 'string' && parsed.frame.trim()) {
          return parsed.frame.trim();
        }
      } catch (e) {
        // Ignore parse errors
      }
    }
    return null;
  }, [items]);

  // State for Discussion Frame collapse
  const [isFrameExpanded, setIsFrameExpanded] = React.useState(true);
  const [isDocumentsExpanded, setIsDocumentsExpanded] = React.useState(true);

  const handleViewDoc = async (docId: string) => {
    if (!token) return;
    try {
      const doc = await api.shareGetDoc(token, docId);
      setDocContent(doc.content);
      setViewingDoc(docId);
    } catch (e: any) {
      console.error('Failed to load document:', e);
    }
  };

  const handleDownloadDoc = async (docId: string, filename: string) => {
    if (!token) return;
    try {
      const blob = await api.shareDownloadDoc(token, docId);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (e: any) {
      console.error('Failed to download document:', e);
    }
  };

  const handleExportToWord = async (docId: string, filename: string) => {
    if (!token) return;
    try {
      const blob = await api.shareExportDocToWord(token, docId);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      const baseName = filename.replace(/\.[^/.]+$/, '');
      a.download = `${baseName}.docx`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (e: any) {
      console.error('Failed to export document:', e);
    }
  };

  const formatFileSize = (bytes?: number): string => {
    if (!bytes) return '';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  const getDocumentIcon = (filename: string, contentType?: string, docType?: string) => {
    const ext = filename.split('.').pop()?.toLowerCase() || '';
    const ct = contentType?.toLowerCase() || '';

    // Check for dynamic artifacts
    if (docType === 'dynamic_artifact') {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <line x1="16" y1="13" x2="8" y2="13" />
          <line x1="16" y1="17" x2="8" y2="17" />
          <polyline points="10 9 9 9 8 9" />
        </svg>
      );
    }

    // PDF
    if (ext === 'pdf' || ct.includes('pdf')) {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="currentColor">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <line x1="16" y1="13" x2="8" y2="13" />
          <line x1="16" y1="17" x2="8" y2="17" />
          <polyline points="10 9 9 9 8 9" />
        </svg>
      );
    }

    // Word / DOCX
    if (ext === 'doc' || ext === 'docx' || ct.includes('word') || ct.includes('document')) {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="currentColor">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <line x1="16" y1="13" x2="8" y2="13" />
          <line x1="16" y1="17" x2="8" y2="17" />
          <polyline points="10 9 9 9 8 9" />
        </svg>
      );
    }

    // Excel / XLSX
    if (ext === 'xls' || ext === 'xlsx' || ct.includes('spreadsheet') || ct.includes('excel')) {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <rect x="3" y="3" width="18" height="18" rx="2" />
          <line x1="3" y1="9" x2="21" y2="9" />
          <line x1="9" y1="3" x2="9" y2="21" />
        </svg>
      );
    }

    // Markdown / MD
    if (ext === 'md' || ext === 'markdown' || ct.includes('markdown')) {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <line x1="16" y1="13" x2="8" y2="13" />
          <line x1="16" y1="17" x2="8" y2="17" />
          <polyline points="10 9 9 9 8 9" />
        </svg>
      );
    }

    // Text / TXT
    if (ext === 'txt' || ct.includes('text/plain')) {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <line x1="16" y1="13" x2="8" y2="13" />
          <line x1="16" y1="17" x2="8" y2="17" />
          <polyline points="10 9 9 9 8 9" />
        </svg>
      );
    }

    // JSON
    if (ext === 'json' || ct.includes('json')) {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <path d="M10 12h4M10 16h4M10 8h1" />
        </svg>
      );
    }

    // CSV
    if (ext === 'csv' || ct.includes('csv')) {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <rect x="3" y="3" width="18" height="18" rx="2" />
          <line x1="3" y1="9" x2="21" y2="9" />
          <line x1="9" y1="3" x2="9" y2="21" />
        </svg>
      );
    }

    // Image
    if (['jpg', 'jpeg', 'png', 'gif', 'svg', 'webp'].includes(ext) || ct.includes('image')) {
      return (
        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <rect x="3" y="3" width="18" height="18" rx="2" />
          <circle cx="8.5" cy="8.5" r="1.5" />
          <polyline points="21 15 16 10 5 21" />
        </svg>
      );
    }

    // Default document icon
    return (
      <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
        <polyline points="14 2 14 8 20 8" />
        <line x1="16" y1="13" x2="8" y2="13" />
        <line x1="16" y1="17" x2="8" y2="17" />
        <polyline points="10 9 9 9 8 9" />
      </svg>
    );
  };

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center" style={{ background: 'var(--bg-body)', color: 'var(--text-secondary-strong)' }}>
        {t('unfold.share.loading')}
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="min-h-screen flex flex-col items-center justify-center p-6" style={{ background: 'var(--bg-body)' }}>
        <div className="w-full max-w-lg text-center">
          <h1 className="text-2xl font-semibold mb-3" style={{ color: 'var(--text-heading)' }}>
            {t('unfold.share.linkUnavailable')}
          </h1>
          <p className="text-base mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            {t('unfold.share.linkExpired')}
          </p>
          <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
            {t('unfold.share.contactOwner')}
          </p>
        </div>
      </div>
    );
  }

  const proj = data.project || {};
  const selectedDoc = documents.find((d: any) => d.id === viewingDoc);

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg-body)' }}>
      {/* Header - centered, matches central column width, sticky on scroll */}
      <div
        className="flex justify-center sticky top-0 z-50 w-full"
        style={{ background: 'var(--bg-body)' }}
      >
        <div
          className="w-full max-w-3xl px-4 py-3 z-50"
          style={{ background: 'var(--bg-body)' }}
        >
          <div>
            <div className="text-[13px] font-semibold" style={{ color: 'var(--text-heading)' }}>
              {proj.displayName || t('unfold.share.sharedChat')}
            </div>
            {proj.description && (
              <div className="text-[12px]" style={{ color: 'var(--text-secondary-strong)' }}>
                {proj.description}
              </div>
            )}
            <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              {t('unfold.share.publicReadOnly')} {new Date(data.share.expiresAt).toLocaleString()}
            </div>
          </div>
          <div className="w-full border-b mt-3 pb-3" style={{ borderColor: 'var(--overlay-line-90)' }}></div>
        </div>
      </div>

      {/* Three-column layout */}
      <div
        className="flex justify-center"
        style={{
          minHeight: 'calc(100vh - 100px)',
          overflow: 'visible',
          position: 'relative',
        }}
      >
        {/* Center column - Chat or Document Preview */}
        <div className="relative flex-shrink-0 px-4 py-4">
          <div className="max-w-3xl w-full">
          {viewingDoc && docContent ? (
            /* Document Preview */
              <div>
                <div className="mb-4 flex items-center justify-between">
                  <div>
                    <h2 className="text-[14px] font-semibold" style={{ color: 'var(--text-heading)' }}>
                      {selectedDoc?.filename || t('unfold.share.document')}
                    </h2>
                    {selectedDoc?.description && (
                      <div className="text-[12px] mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
                        {selectedDoc.description}
                      </div>
                    )}
                  </div>
                  <button
                    onClick={() => {
                      setViewingDoc(null);
                      setDocContent(null);
                    }}
                    className="px-3 py-1 rounded text-[12px]"
                    style={{ 
                      backgroundColor: 'transparent',
                      color: 'var(--text-secondary-strong)',
                      border: '1px solid var(--overlay-line-90)',
                      cursor: 'pointer'
                    }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                    }}
                  >
                    {t('unfold.share.backToChat')}
                  </button>
                </div>
              <div className="markdown" style={{ color: 'var(--text-secondary-strong)' }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>
                  {docContent}
                </ReactMarkdown>
              </div>
            </div>
          ) : (
            /* Chat Messages */
            <div>
              {/* Discussion Frame (collapsible) */}
              {discussionFrame && (
                <div className="mb-4">
                  <button
                    onClick={() => setIsFrameExpanded(!isFrameExpanded)}
                    className="w-full flex items-center justify-between p-3 text-left"
                    style={{ 
                      color: 'var(--text-heading)',
                      cursor: 'pointer',
                      backgroundColor: 'transparent',
                      border: 'none'
                    }}
                  >
                    <span className="text-[13px] font-semibold" style={{ color: '#669999' }}>
                      {t('unfold.share.discussionFrame')}
                    </span>
                    <svg 
                      className="w-4 h-4 transition-transform" 
                      style={{ 
                        transform: isFrameExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                        color: 'var(--text-secondary-strong)'
                      }}
                      viewBox="0 0 24 24" 
                      fill="none" 
                      stroke="currentColor" 
                      strokeWidth="2"
                    >
                      <path d="M6 9l6 6 6-6" strokeLinecap="round" strokeLinejoin="round"/>
                    </svg>
                  </button>
                  {isFrameExpanded && (
                    <div className="px-3 pb-3" style={{ color: 'var(--text-secondary-strong)' }}>
                      <div className="text-[12px] leading-relaxed" style={{ 
                        fontStyle: 'italic',
                        color: 'var(--text-secondary-strong)'
                      }}>
                        {discussionFrame}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {/* Divider after Discussion Frame */}
              {discussionFrame && (
                <div className="mt-4 mb-4" style={{ borderTop: '1px solid var(--overlay-line-90)' }} />
              )}

              {/* Messages */}
              <div className="space-y-2">
                {regularItems.map((it: any) => {
                  const isUser = it.kind === 'user';
                  const isSystem = it.kind === 'system';
                  const content = (it.contentSanitized || it.content || '').toString();
                  const gd = it.graphData;

                  // Skip interactions with discussion.flow.analysis (shown separately as Discussion Frame)
                  if (it.subkind === 'discussion.flow.analysis') {
                    return null;
                  }

                  // Skip perspective analysis interactions (shown separately at the end)
                  if (it.subkind && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
                    return null;
                  }

                  // Skip all system interactions with eval.* subkind (except eval.frame_compliance which is shown as compliance warning)
                  // These are metadata evaluations (bias, stance, etc.) and should not be displayed as messages
                  if (isSystem && it.subkind && it.subkind.startsWith('eval.')) {
                    if (it.subkind === 'eval.frame_compliance') {
                      // This will be shown as compliance warning after assistant message
                      return null;
                    }
                    // Skip all other eval.* system interactions (eval.bias, eval.stance, etc.)
                    return null;
                  }

                  // Skip assistant interactions that are pure metadata (empty content or only JSON evaluations)
                  // These might be evaluation metadata stored as assistant interactions
                  if (!isUser && !isSystem && (!content || content.trim() === '' || content.trim().startsWith('{'))) {
                    // Check if this looks like a JSON-only interaction (evaluation metadata)
                    try {
                      const parsed = JSON.parse(content);
                      // If it's a valid JSON object with evaluation-like structure, skip it
                      if (parsed && typeof parsed === 'object' && (parsed.biasLevel !== undefined || parsed.thoughtfulnessLevel !== undefined || parsed.expansions !== undefined)) {
                        return null;
                      }
                    } catch {
                      // Not JSON, continue rendering
                    }
                  }

                  // Special rendering for action.bring_in indicator messages
                  if (isUser && it.subkind === 'action.bring_in') {
                    return (
                      <div key={it.id} className="my-1.5 flex justify-end">
                        <div className="px-3 py-1.5 rounded-lg text-[12px] inline-flex items-center gap-2" style={{ 
                          background: 'var(--accent-research-10)', 
                          color: 'var(--accent-research)',
                          border: '1px solid var(--accent-research-20)',
                          maxWidth: '80%'
                        }}>
                          <svg className="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                            <path d="M12 7a3 3 0 1 1 0 6a3 3 0 0 1 0-6" strokeWidth="2" />
                            <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth="2" strokeLinecap="round" />
                            <path d="M18 7v4M16 9h4" strokeWidth="2" strokeLinecap="round" />
                          </svg>
                          <span>{content}</span>
                        </div>
                      </div>
                    );
                  }

                  // Determine display name
                  let displayName = isUser ? t('unfold.share.you') : t('unfold.share.assistant');
                  if (it.author?.displayName) {
                    displayName = it.author.displayName;
                  } else if (it.subkind === 'agent.bring_in' || it.subkind === 'agent.ask') {
                    displayName = t('unfold.share.agent');
                  }

                  // Agent accent (if agent contribution)
                  const isAgent = it.subkind?.startsWith('agent.');
                  const isAssistant = !isUser && !isSystem;

                  // Check for compliance warning for this assistant message
                  // Look up by message ID only (warnings are already mapped to message IDs in complianceWarnings)
                  const complianceWarning = isAssistant ? complianceWarnings[it.id] : null;

                  return (
                    <React.Fragment key={it.id}>
                      <ChatMessage
                        role={isUser ? 'user' : 'assistant'}
                        align={isUser ? 'right' : 'left'}
                        headerText={displayName}
                        leftAccent={isAgent}
                      >
                        {gd ? (
                          <ChatGraphMessage
                            chartTitle={gd.chartTitle}
                            chartType={gd.chartType || 'line'}
                            table={gd.table}
                            sources={gd.sources}
                          />
                        ) : (
                          <div className="markdown">
                            <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>
                              {content}
                            </ReactMarkdown>
                          </div>
                        )}
                      </ChatMessage>
                      {/* Show compliance warning after assistant message */}
                      {isAssistant && complianceWarning && (
                        <div className="mt-2" style={{ width: '100%' }}>
                          <div className="p-2" style={{ backgroundColor: 'var(--warning-10)', borderLeft: '2px solid var(--accent-warning)', borderRadius: '4px' }}>
                            <div className="text-[11px]" style={{ color: 'var(--text-heading)', lineHeight: '1.4' }}>
                              {complianceWarning.explanation}
                            </div>
                          </div>
                        </div>
                      )}
                    </React.Fragment>
                  );
                })}
              </div>

              {/* Perspective Analyses at the end - Combined */}
              {perspectiveAnalyses.length > 0 && (() => {
                // Collect all keyPoints and blindSpots from all analyses
                const allKeyPoints: string[] = [];
                const allBlindSpots: string[] = [];
                
                perspectiveAnalyses.forEach((it: any) => {
                  try {
                    const content = it.contentSanitized || it.content || '{}';
                    const parsed = JSON.parse(content);
                    
                    if (Array.isArray(parsed.keyPoints)) {
                      parsed.keyPoints.forEach((point: string) => {
                        if (point && typeof point === 'string' && point.trim()) {
                          allKeyPoints.push(point.trim());
                        }
                      });
                    }
                    
                    if (Array.isArray(parsed.blindSpots)) {
                      parsed.blindSpots.forEach((spot: string | { text?: string; textValue?: string; value?: string; description?: string; content?: string }) => {
                        const spotText = typeof spot === 'string' ? spot : (spot.text || spot.textValue || spot.value || spot.description || spot.content || String(spot));
                        if (spotText && typeof spotText === 'string' && spotText.trim()) {
                          allBlindSpots.push(spotText.trim());
                        }
                      });
                    }
                  } catch (e) {
                    // Skip invalid analyses
                  }
                });

                // Remove duplicates
                const uniqueKeyPoints = Array.from(new Set(allKeyPoints));
                const uniqueBlindSpots = Array.from(new Set(allBlindSpots));

                // Only render if there's data to show
                if (uniqueKeyPoints.length === 0 && uniqueBlindSpots.length === 0) {
                  return null;
                }

                return (
                  <>
                    <div className="mt-8 mb-4" style={{ borderTop: '1px solid var(--overlay-line-90)' }} />
                    <div 
                      className="rounded-lg p-4"
                      style={{ 
                        backgroundColor: 'var(--bg-elevated)',
                        border: '1px solid var(--overlay-line-90)'
                      }}
                    >
                      <div className="text-[13px] font-semibold mb-4" style={{ color: 'var(--text-heading)' }}>
                        {t('unfold.share.analysisTitle')}
                      </div>
                      
                      {uniqueKeyPoints.length > 0 && (
                        <div className="mb-4">
                          <div className="text-[11px] font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
                            {t('unfold.share.keyPoints')}
                          </div>
                          <ul className="space-y-1">
                            {uniqueKeyPoints.map((point: string, idx: number) => (
                              <li key={idx} className="text-[12px] flex items-start gap-2" style={{ color: 'var(--text-secondary-strong)' }}>
                                <span style={{ color: 'var(--text-tertiary-weak)' }}>•</span>
                                <span>{point}</span>
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                      
                      {uniqueBlindSpots.length > 0 && (
                        <div>
                          <div className="text-[11px] font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
                            {t('unfold.share.blindSpots')}
                          </div>
                          <ul className="space-y-1">
                            {uniqueBlindSpots.map((spot: string, idx: number) => (
                              <li key={idx} className="text-[12px] flex items-start gap-2" style={{ color: 'var(--text-secondary-strong)' }}>
                                <span style={{ color: 'var(--text-tertiary-weak)' }}>•</span>
                                <span>{spot}</span>
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                    </div>
                  </>
                );
              })()}

              {/* Footer with logo and tagline */}
              <div className="mt-8 pt-6" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
                <div className="flex flex-col items-center gap-3">
                  {/* Dark theme logo */}
                  <img
                    src={logoDarkTheme}
                    alt="etigram.one"
                    className="h-4 w-auto"
                    style={{
                      display: theme === 'dark' ? 'block' : 'none',
                      filter: 'drop-shadow(0 0 2px rgba(0,0,0,0.6))',
                      opacity: 0.7,
                    }}
                  />
                  {/* Light theme logo */}
                  <img
                    src={logoLightTheme}
                    alt="etigram.one"
                    className="h-4 w-auto"
                    style={{
                      display: theme === 'light' ? 'block' : 'none',
                      opacity: 0.7,
                    }}
                  />
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary)', textAlign: 'center' }}>
                    {t('unfold.share.footer.tagline')}
                  </div>
                </div>
              </div>
            </div>
          )}
          </div>
          {/* Right column - Documents, attached to central column and sticky */}
          {(documents.length > 0 || documentsLoading) && (
          <div className="absolute top-0 left-full w-80 h-full pointer-events-auto">
            <div
              className="sticky top-32 px-4 py-4"
            >
            {documents.length > 0 ? (
              <div>
                <div className="mb-3 flex items-center justify-between">
                  <span className="text-[13px] font-semibold" style={{ color: 'var(--text-heading)' }}>
                    {t('unfold.share.documents')} ({documents.length})
                  </span>
                  <button
                    onClick={() => setIsDocumentsExpanded(!isDocumentsExpanded)}
                    className="p-1 rounded"
                    style={{ 
                      backgroundColor: 'transparent',
                      color: 'var(--text-secondary-strong)',
                      border: 'none',
                      cursor: 'pointer'
                    }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                    }}
                  >
                    <svg 
                      className="w-4 h-4 transition-transform" 
                      style={{ 
                        transform: isDocumentsExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                        color: 'var(--text-secondary-strong)'
                      }}
                      viewBox="0 0 24 24" 
                      fill="none" 
                      stroke="currentColor" 
                      strokeWidth="2"
                    >
                      <path d="M6 9l6 6 6-6" strokeLinecap="round" strokeLinejoin="round"/>
                    </svg>
                  </button>
                </div>
                {isDocumentsExpanded && (
                  <div className="space-y-2">
                    {documents.map((doc: any) => (
                      <div 
                        key={doc.id} 
                        className="inline-flex items-center gap-2 p-2 rounded cursor-pointer"
                        style={{ 
                          backgroundColor: viewingDoc === doc.id ? 'var(--accent-research-10)' : 'transparent',
                          transition: 'all 0.2s'
                        }}
                        onClick={() => handleViewDoc(doc.id)}
                        onMouseEnter={(e) => {
                          if (viewingDoc !== doc.id) {
                            (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)';
                          }
                        }}
                        onMouseLeave={(e) => {
                          if (viewingDoc !== doc.id) {
                            (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent';
                          }
                        }}
                      >
                        <div style={{ color: 'var(--text-secondary-strong)' }}>
                          {getDocumentIcon(doc.filename, doc.contentType, doc.docType)}
                        </div>
                        <div className="text-[12px] font-medium" style={{ color: 'var(--text-heading)' }}>
                          {doc.filename}
                        </div>
                        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                          <button
                            onClick={() => handleDownloadDoc(doc.id, doc.filename)}
                            className="px-2 py-1 rounded text-[11px] flex items-center gap-1"
                            style={{ 
                              backgroundColor: 'transparent',
                              color: 'var(--text-secondary-strong)',
                              border: '1px solid var(--overlay-line-90)',
                              cursor: 'pointer'
                            }}
                            onMouseEnter={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                            }}
                            onMouseLeave={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                            }}
                          >
                            <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                              <polyline points="7 10 12 15 17 10" />
                              <line x1="12" y1="15" x2="12" y2="3" />
                            </svg>
                            {t('unfold.share.download')}
                          </button>
                          <button
                            onClick={() => handleExportToWord(doc.id, doc.filename)}
                            className="px-2 py-1 rounded text-[11px] flex items-center gap-1"
                            style={{ 
                              backgroundColor: 'transparent',
                              color: 'var(--text-secondary-strong)',
                              border: '1px solid var(--overlay-line-90)',
                              cursor: 'pointer'
                            }}
                            onMouseEnter={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                            }}
                            onMouseLeave={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                            }}
                            title={t('unfold.share.exportToWord')}
                          >
                            <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
                              <polyline points="14 2 14 8 20 8" />
                              <line x1="16" y1="13" x2="8" y2="13" />
                              <line x1="16" y1="17" x2="8" y2="17" />
                              <polyline points="10 9 9 9 8 9" />
                            </svg>
                            {t('unfold.share.exportToWord')}
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ) : documentsLoading ? (
              <div className="text-[12px]" style={{ color: 'var(--text-secondary-strong)' }}>
                {t('unfold.share.documentsLoading')}
              </div>
            ) : null}
            </div>
          </div>
          )}
          {/* Close relative container */}
        </div>
      </div>
    </div>
  );
}

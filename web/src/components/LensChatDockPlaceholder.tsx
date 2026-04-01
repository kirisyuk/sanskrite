import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { api, type HorizonLensChatResponse } from '../lib/api';
import type { Interaction } from '../types/interaction';

export interface LensChatMapFocus {
  sourceMessageId: string;
  canonicalPaths: string[];
}

interface LensChatDockPlaceholderProps {
  isOpen: boolean;
  onOpen: () => void;
  onClose: () => void;
  isMobile: boolean;
  sidebarOpen?: boolean;
  lensId: string;
  projectId: string | null;
  lensName?: string | null;
  activeMapFocusSourceId?: string | null;
  onShowOnMap?: (focus: LensChatMapFocus) => void;
  onClearMapFocus?: () => void;
}

const TOGGLE_BUTTON_STYLE: React.CSSProperties = {
  width: 32,
  minWidth: 32,
  maxWidth: 32,
  height: 32,
  minHeight: 32,
  maxHeight: 32,
  flexShrink: 0,
  borderRadius: 10,
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  border: 'none',
  backgroundColor: '#669999',
  color: '#fff',
  cursor: 'pointer',
  boxShadow: '0 10px 22px rgba(0, 0, 0, 0.28)',
};

function MessageIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M7 10h10" />
      <path d="M7 14h6" />
      <path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5c-1.4 0-2.73-.33-3.9-.92L3 21l1.98-5.15A8.47 8.47 0 0 1 3.5 11.5 8.5 8.5 0 0 1 12 3a8.5 8.5 0 0 1 9 8.5Z" />
    </svg>
  );
}

function ChevronLeftIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M15 5 8 12l7 7" />
    </svg>
  );
}

function SendIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M22 2 11 13" />
      <path d="M22 2 15 22l-4-9-9-4Z" />
    </svg>
  );
}

function formatMessageTime(value?: string) {
  if (!value) return '';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return '';
  return parsed.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function humanizeCanonicalPath(path: string) {
  const normalized = String(path || '').trim().toLowerCase();
  if (!normalized.startsWith('/')) return normalized;
  const segments = normalized.split('/').filter(Boolean);
  if (segments.length === 0) return normalized;
  let start = 1;
  if (segments.length > 1 && segments[1].length === 2) {
    start = 2;
  }
  if (start >= segments.length) {
    start = segments.length - 1;
  }
  return segments
    .slice(start)
    .flatMap((segment) => segment.split('-'))
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

function normalizeDisplayValue(value: string) {
  return String(value || '').trim().toLowerCase();
}

function isGenericCapsuleHit(response: HorizonLensChatResponse | null, path: string, title: string) {
  if (!response?.hits?.length || !path || !title) return false;
  const normalizedPath = String(path).trim();
  const normalizedTitle = normalizeDisplayValue(title);
  return response.hits.some((hit) => {
    if (!hit?.canonicalPaths?.includes(normalizedPath)) return false;
    if (normalizeDisplayValue(hit.title) !== normalizedTitle) return false;
    return (hit.canonicalPaths?.length || 0) > 1;
  });
}

function resolvePathLabel(path: string, response: HorizonLensChatResponse | null) {
  const normalized = String(path || '').trim();
  if (!normalized) return '';
  const match = response?.hits?.find((hit) => hit.canonicalPaths?.includes(normalized));
  const matchedTitle = match?.title?.trim() || '';
  if (matchedTitle && !isGenericCapsuleHit(response, normalized, matchedTitle)) {
    return matchedTitle;
  }
  return humanizeCanonicalPath(normalized) || matchedTitle || normalized;
}

function buildHighlightedChipItems(response: HorizonLensChatResponse | null) {
  const items = response?.highlightedPaths || [];
  const seen = new Set<string>();
  return items
    .map((path) => {
      const label = resolvePathLabel(path, response);
      return { path, label };
    })
    .filter(({ path, label }) => {
      const key = normalizeDisplayValue(label || path);
      if (!key || seen.has(key)) return false;
      seen.add(key);
      return true;
    });
}

function buildEvidenceItems(response: HorizonLensChatResponse | null, highlightedLabels: string[]) {
  const hits = response?.hits || [];
  const highlightedSet = new Set(highlightedLabels.map((value) => normalizeDisplayValue(value)));
  const seen = new Set<string>();
  return hits.filter((hit) => {
    const title = String(hit?.title || hit?.id || '').trim();
    const key = `${normalizeDisplayValue(title)}::${normalizeDisplayValue(hit?.kind || '')}`;
    if (!title || seen.has(key)) return false;
    seen.add(key);
    return !highlightedSet.has(normalizeDisplayValue(title));
  });
}

function buildMapFocus(response: HorizonLensChatResponse | null, assistantMessageId?: string | null): LensChatMapFocus | null {
  const sourceMessageId = String(assistantMessageId || '').trim();
  if (!sourceMessageId || !response) return null;
  const pathSet = new Set<string>();
  for (const path of response.highlightedPaths || []) {
    const value = String(path || '').trim();
    if (value.startsWith('/')) pathSet.add(value);
  }
  for (const hit of response.hits || []) {
    for (const path of hit.canonicalPaths || []) {
      const value = String(path || '').trim();
      if (value.startsWith('/')) pathSet.add(value);
    }
  }
  const canonicalPaths = Array.from(pathSet);
  if (canonicalPaths.length === 0) return null;
  return { sourceMessageId, canonicalPaths };
}

function upsertMessages(current: Interaction[], additions: Array<Interaction | undefined>): Interaction[] {
  const merged = [...current];
  for (const item of additions) {
    if (!item?.id) continue;
    const index = merged.findIndex((existing) => existing.id === item.id);
    if (index >= 0) {
      merged[index] = item;
      continue;
    }
    merged.push(item);
  }
  merged.sort((a, b) => {
    const left = new Date(a.createdAt || '').getTime();
    const right = new Date(b.createdAt || '').getTime();
    return left - right;
  });
  return merged;
}

export const LensChatDockPlaceholder: React.FC<LensChatDockPlaceholderProps> = ({
  isOpen,
  onOpen,
  onClose,
  isMobile,
  sidebarOpen = true,
  lensId,
  projectId,
  lensName,
  activeMapFocusSourceId = null,
  onShowOnMap,
  onClearMapFocus,
}) => {
  const shellLeft = isMobile ? 12 : 16;
  const shellBottom = isMobile ? 12 : 16;
  const shellTop = isMobile ? 72 : 78;
  const shellWidth = isMobile ? 'min(calc(100vw - 24px), 420px)' : 360;
  const collapsedSidebarButtonInset = 8;
  const hiddenTogglePinnedToRail = !isMobile && !sidebarOpen;
  const hiddenToggleLeft = hiddenTogglePinnedToRail ? collapsedSidebarButtonInset : shellLeft;
  const hiddenToggleBottom = shellBottom + 48;
  const threadId = React.useMemo(() => `lens-chat:${lensId}`, [lensId]);
  const [messages, setMessages] = React.useState<Interaction[]>([]);
  const [draft, setDraft] = React.useState('');
  const [isLoadingHistory, setIsLoadingHistory] = React.useState(false);
  const [isSubmitting, setIsSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [latestResponse, setLatestResponse] = React.useState<HorizonLensChatResponse | null>(null);
  const scrollRef = React.useRef<HTMLDivElement | null>(null);

  const loadMessages = React.useCallback(async () => {
    if (!projectId) {
      setMessages([]);
      return;
    }
    setIsLoadingHistory(true);
    setError(null);
    try {
      const items = await api.getProjectInteractions(projectId);
      const safeItems = Array.isArray(items) ? items : [];
      const threadMessages = safeItems.filter((item) => item.sectionId === threadId && item.subkind === 'lens.chat') as Interaction[];
      setMessages(threadMessages);
    } catch (err: any) {
      console.error('[LensChat] Failed to load messages', err);
      setError(err?.response?.data?.error?.message || err?.message || 'Failed to load chat history');
    } finally {
      setIsLoadingHistory(false);
    }
  }, [projectId, threadId]);

  React.useEffect(() => {
    setMessages([]);
    setDraft('');
    setError(null);
    setLatestResponse(null);
  }, [lensId, projectId]);

  React.useEffect(() => {
    if (!isOpen) return;
    void loadMessages();
  }, [isOpen, loadMessages]);

  React.useEffect(() => {
    if (!isOpen || !scrollRef.current) return;
    scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [isOpen, messages, isSubmitting, latestResponse]);

  const handleSubmit = React.useCallback(async () => {
    const message = draft.trim();
    if (!message || isSubmitting || !lensId) return;
    setIsSubmitting(true);
    setError(null);
    try {
      const response = await api.postHorizonLensChat(lensId, message);
      setLatestResponse(response);
      setMessages((current) =>
        upsertMessages(current, [response.userMessage as Interaction | undefined, response.assistantMessage as Interaction | undefined]),
      );
      setDraft('');
    } catch (err: any) {
      console.error('[LensChat] Failed to send message', err);
      setError(err?.response?.data?.error?.message || err?.message || 'Failed to send message');
    } finally {
      setIsSubmitting(false);
    }
  }, [draft, isSubmitting, lensId]);

  const handleComposerKeyDown = React.useCallback(
    (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        void handleSubmit();
      }
    },
    [handleSubmit],
  );

  const latestAssistantId = latestResponse?.assistantMessage?.id || null;

  return (
    <>
      {!isOpen && (
        <div
          style={{
            position: hiddenTogglePinnedToRail ? 'fixed' : 'absolute',
            left: hiddenToggleLeft,
            bottom: hiddenToggleBottom,
            zIndex: 85,
            pointerEvents: 'auto',
          }}
        >
          <button
            type="button"
            onClick={onOpen}
            aria-label="Open lens console"
            title="Open lens console"
            style={TOGGLE_BUTTON_STYLE}
          >
            <MessageIcon />
          </button>
        </div>
      )}

      <div
        aria-hidden={!isOpen}
        style={{
          position: 'absolute',
          top: shellTop,
          left: shellLeft,
          bottom: shellBottom,
          width: shellWidth,
          maxWidth: 'calc(100vw - 24px)',
          zIndex: 84,
          display: 'flex',
          pointerEvents: isOpen ? 'auto' : 'none',
          transform: isOpen ? 'translateX(0)' : 'translateX(-24px)',
          opacity: isOpen ? 1 : 0,
          transition: 'transform 200ms ease, opacity 200ms ease',
        }}
      >
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            width: '100%',
            minHeight: 0,
            borderRadius: 20,
            border: '1px solid var(--border-subtle)',
            background: 'color-mix(in srgb, var(--bg-elevated) 92%, black 8%)',
            boxShadow: '0 24px 60px rgba(0, 0, 0, 0.28)',
            backdropFilter: 'blur(14px)',
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 12,
              padding: '14px 14px 12px 16px',
              borderBottom: '1px solid var(--border-subtle)',
            }}
          >
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)', letterSpacing: '0.04em', textTransform: 'uppercase' }}>
                Lens Console
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginTop: 3, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {lensName ? `Attached to ${lensName}` : 'Attached to current lens'}
              </div>
            </div>
            <button
              type="button"
              onClick={onClose}
              aria-label="Close lens console"
              title="Close lens console"
              style={TOGGLE_BUTTON_STYLE}
            >
              <ChevronLeftIcon />
            </button>
          </div>

          <div
            ref={scrollRef}
            style={{
              flex: 1,
              minHeight: 0,
              overflow: 'auto',
              padding: '16px 14px 14px 16px',
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
              background:
                'radial-gradient(circle at top left, rgba(102, 153, 153, 0.12), transparent 38%), linear-gradient(180deg, rgba(255,255,255,0.02), rgba(255,255,255,0))',
            }}
          >
            {messages.length === 0 && !isLoadingHistory ? (
              <>
                <div
                  style={{
                    alignSelf: 'flex-start',
                    maxWidth: '88%',
                    padding: '12px 14px',
                    borderRadius: '16px 16px 16px 6px',
                    background: 'var(--bg-sidebar)',
                    border: '1px solid var(--border-subtle)',
                    color: 'var(--text-secondary-strong)',
                    fontSize: 13,
                    lineHeight: 1.45,
                  }}
                >
                  Ask this lens a question. The answer will be grounded in warmed internal knowledge for the current lens.
                </div>
                <div
                  style={{
                    alignSelf: 'flex-start',
                    maxWidth: '92%',
                    padding: '12px 14px',
                    borderRadius: '16px 16px 16px 6px',
                    background: 'rgba(102, 153, 153, 0.12)',
                    border: '1px solid rgba(102, 153, 153, 0.25)',
                    color: 'var(--text-heading)',
                    fontSize: 13,
                    lineHeight: 1.45,
                  }}
                >
                  Example: Who participates in the supply of lithium batteries for Mercedes?
                </div>
              </>
            ) : null}

            {isLoadingHistory ? (
              <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>Loading lens console…</div>
            ) : null}

            {messages.map((message) => {
              const isUser = message.kind === 'user';
              const isLatestAssistant = !isUser && message.id === latestAssistantId;
              const highlightedChipItems = isLatestAssistant ? buildHighlightedChipItems(latestResponse) : [];
              const evidenceItems = isLatestAssistant ? buildEvidenceItems(latestResponse, highlightedChipItems.map((item) => item.label)) : [];
              const mapFocus = isLatestAssistant ? buildMapFocus(latestResponse, message.id) : null;
              const mapFocusActive = Boolean(mapFocus && activeMapFocusSourceId && mapFocus.sourceMessageId === activeMapFocusSourceId);
              return (
                <div
                  key={message.id}
                  style={{
                    alignSelf: isUser ? 'flex-end' : 'flex-start',
                    maxWidth: '92%',
                    padding: '12px 14px',
                    borderRadius: isUser ? '16px 16px 6px 16px' : '16px 16px 16px 6px',
                    background: isUser ? 'rgba(102, 153, 153, 0.18)' : 'var(--bg-sidebar)',
                    border: isUser ? '1px solid rgba(102, 153, 153, 0.25)' : '1px solid var(--border-subtle)',
                    color: isUser ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                    fontSize: 13,
                    lineHeight: 1.5,
                    overflowWrap: 'anywhere',
                    wordBreak: 'break-word',
                  }}
                >
                  <div style={{ whiteSpace: 'normal' }}>
                    {isUser ? (
                      <div style={{ whiteSpace: 'pre-wrap' }}>{message.content}</div>
                    ) : (
                      <ReactMarkdown
                        remarkPlugins={[remarkGfm]}
                        components={{
                          p: ({ children }) => <p style={{ margin: 0, marginBottom: 10 }}>{children}</p>,
                          ul: ({ children }) => <ul style={{ margin: '0 0 10px 18px', padding: 0 }}>{children}</ul>,
                          ol: ({ children }) => <ol style={{ margin: '0 0 10px 18px', padding: 0 }}>{children}</ol>,
                          li: ({ children }) => <li style={{ marginBottom: 4 }}>{children}</li>,
                          strong: ({ children }) => <strong style={{ color: 'var(--text-heading)' }}>{children}</strong>,
                          code: ({ children }) => (
                            <code
                              style={{
                                fontSize: 12,
                                padding: '1px 4px',
                                borderRadius: 6,
                                background: 'rgba(255,255,255,0.06)',
                                overflowWrap: 'anywhere',
                                wordBreak: 'break-word',
                              }}
                            >
                              {children}
                            </code>
                          ),
                        }}
                      >
                        {message.content}
                      </ReactMarkdown>
                    )}
                  </div>
                  <div style={{ marginTop: 8, display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                    <span style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{formatMessageTime(message.createdAt)}</span>
                    {!isUser && message.model ? (
                      <span style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{message.model}</span>
                    ) : null}
                  </div>
                  {isLatestAssistant && latestResponse ? (
                    <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 8 }}>
                      {mapFocus ? (
                        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                          <button
                            type="button"
                            onClick={() => {
                              if (mapFocusActive) {
                                onClearMapFocus?.();
                                return;
                              }
                              onShowOnMap?.(mapFocus);
                            }}
                            style={{
                              borderRadius: 999,
                              border: mapFocusActive ? '1px solid rgba(102, 153, 153, 0.45)' : '1px solid var(--border-subtle)',
                              background: mapFocusActive ? 'rgba(102, 153, 153, 0.18)' : 'rgba(255,255,255,0.03)',
                              color: mapFocusActive ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                              padding: '7px 10px',
                              fontSize: 11,
                              fontWeight: 700,
                              cursor: 'pointer',
                            }}
                          >
                            {mapFocusActive ? 'Showing on map' : 'Show me on map'}
                          </button>
                          {mapFocusActive ? (
                            <button
                              type="button"
                              onClick={() => onClearMapFocus?.()}
                              style={{
                                borderRadius: 999,
                                border: '1px solid var(--border-subtle)',
                                background: 'transparent',
                                color: 'var(--text-secondary-strong)',
                                padding: '7px 10px',
                                fontSize: 11,
                                fontWeight: 700,
                                cursor: 'pointer',
                              }}
                            >
                              Clear
                            </button>
                          ) : null}
                        </div>
                      ) : null}
                      {highlightedChipItems.length > 0 ? (
                        <details
                          style={{
                            borderRadius: 12,
                            border: '1px solid var(--border-subtle)',
                            background: 'rgba(255,255,255,0.03)',
                            overflow: 'hidden',
                          }}
                        >
                          <summary
                            style={{
                              cursor: 'pointer',
                              listStyle: 'none',
                              padding: '8px 10px',
                              fontSize: 11,
                              fontWeight: 600,
                              color: 'var(--text-secondary-strong)',
                              userSelect: 'none',
                            }}
                          >
                            Sources
                            <span style={{ marginLeft: 6, color: 'var(--text-secondary)', fontWeight: 500 }}>
                              {highlightedChipItems.length + evidenceItems.length}
                            </span>
                          </summary>
                          <div style={{ padding: '0 10px 10px 10px', display: 'flex', flexDirection: 'column', gap: 8 }}>
                            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                              {highlightedChipItems.slice(0, 6).map(({ path, label }) => (
                                <span
                                  key={path}
                                  title={path}
                                  style={{
                                    fontSize: 10,
                                    fontWeight: 600,
                                    padding: '5px 8px',
                                    borderRadius: 999,
                                    border: '1px solid rgba(102, 153, 153, 0.28)',
                                    background: 'rgba(102, 153, 153, 0.1)',
                                    color: 'var(--text-heading)',
                                    overflowWrap: 'anywhere',
                                    wordBreak: 'break-word',
                                  }}
                                >
                                  {label}
                                </span>
                              ))}
                            </div>
                            {evidenceItems.length > 0 ? (
                              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                                {evidenceItems.slice(0, 3).map((hit) => (
                                  <div key={hit.id} style={{ fontSize: 11, color: 'var(--text-secondary)' }}>
                                    <strong style={{ color: 'var(--text-secondary-strong)' }}>{hit.title || hit.id}</strong>
                                    {hit.kind ? ` • ${hit.kind}` : ''}
                                  </div>
                                ))}
                              </div>
                            ) : null}
                          </div>
                        </details>
                      ) : null}
                      {!highlightedChipItems.length && evidenceItems.length > 0 ? (
                        <details
                          style={{
                            borderRadius: 12,
                            border: '1px solid var(--border-subtle)',
                            background: 'rgba(255,255,255,0.03)',
                            overflow: 'hidden',
                          }}
                        >
                          <summary
                            style={{
                              cursor: 'pointer',
                              listStyle: 'none',
                              padding: '8px 10px',
                              fontSize: 11,
                              fontWeight: 600,
                              color: 'var(--text-secondary-strong)',
                              userSelect: 'none',
                            }}
                          >
                            Sources
                            <span style={{ marginLeft: 6, color: 'var(--text-secondary)', fontWeight: 500 }}>
                              {evidenceItems.length}
                            </span>
                          </summary>
                          <div style={{ padding: '0 10px 10px 10px', display: 'flex', flexDirection: 'column', gap: 6 }}>
                            {evidenceItems.slice(0, 3).map((hit) => (
                              <div key={hit.id} style={{ fontSize: 11, color: 'var(--text-secondary)' }}>
                                <strong style={{ color: 'var(--text-secondary-strong)' }}>{hit.title || hit.id}</strong>
                                {hit.kind ? ` • ${hit.kind}` : ''}
                              </div>
                            ))}
                          </div>
                        </details>
                      ) : null}
                      {latestResponse.warnings && latestResponse.warnings.length > 0 ? (
                        <div style={{ fontSize: 11, color: 'var(--warning-strong, #d9a441)' }}>
                          {latestResponse.warnings[0]}
                        </div>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              );
            })}

            {isSubmitting ? (
              <div
                style={{
                  alignSelf: 'flex-start',
                  maxWidth: '88%',
                  padding: '12px 14px',
                  borderRadius: '16px 16px 16px 6px',
                  background: 'var(--bg-sidebar)',
                  border: '1px solid var(--border-subtle)',
                  color: 'var(--text-secondary)',
                  fontSize: 13,
                }}
              >
                Thinking over the current lens…
              </div>
            ) : null}
          </div>

          <div
            style={{
              padding: 14,
              borderTop: '1px solid var(--border-subtle)',
              background: 'rgba(0, 0, 0, 0.08)',
            }}
          >
            {error ? (
              <div style={{ marginBottom: 10, fontSize: 12, color: 'var(--danger-strong, #e37d7d)' }}>
                {error}
              </div>
            ) : null}
            <div
              style={{
                borderRadius: 16,
                border: '1px solid var(--border-subtle)',
                background: 'var(--bg-input)',
                padding: 10,
                display: 'flex',
                alignItems: 'flex-end',
                gap: 10,
              }}
            >
              <textarea
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                onKeyDown={handleComposerKeyDown}
                aria-label="Lens chat input"
                placeholder="Ask this lens..."
                disabled={!projectId || isSubmitting}
                style={{
                  flex: 1,
                  minHeight: 72,
                  resize: 'none',
                  border: 'none',
                  outline: 'none',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  fontSize: 13,
                  lineHeight: 1.45,
                }}
              />
              <button
                type="button"
                onClick={() => void handleSubmit()}
                disabled={!projectId || isSubmitting || !draft.trim()}
                aria-label="Send lens chat message"
                style={{
                  width: 36,
                  height: 36,
                  borderRadius: 12,
                  border: '1px solid var(--border-subtle)',
                  background: !projectId || isSubmitting || !draft.trim() ? 'rgba(102, 153, 153, 0.12)' : 'rgba(102, 153, 153, 0.28)',
                  color: 'var(--text-heading)',
                  opacity: !projectId || isSubmitting || !draft.trim() ? 0.7 : 1,
                  cursor: !projectId || isSubmitting || !draft.trim() ? 'not-allowed' : 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <SendIcon />
              </button>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

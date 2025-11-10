import React from 'react';
import { findFirstRange } from '../lib/textHighlight';

interface ContextPanelProps {
  sectionStarts: any[];
  pinnedInteractions: any[];
  messagesRef: React.RefObject<HTMLDivElement>;
  scrollToInteraction: (id: string) => void;
  truncateCenterWords: (s: string, headWords?: number, tailWords?: number) => string;
  onRemoveNote: (interactionId: string, noteIndex: number) => Promise<void>;
}

export const ContextPanel: React.FC<ContextPanelProps> = ({
  sectionStarts,
  pinnedInteractions,
  messagesRef,
  scrollToInteraction,
  truncateCenterWords,
  onRemoveNote,
}) => {
  return (
    <div className="space-y-2">
      {/* Sections list */}
      {sectionStarts.length > 0 && (
        <div className="mb-2">
          <div
            className="text-[11px] mb-1 flex items-center gap-1"
            style={{
              color: 'var(--text-secondary-strong)',
            }}
          >
            <svg
              className="w-3.5 h-3.5"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
            >
              <rect
                x="4"
                y="5"
                width="16"
                height="4"
                rx="1"
                strokeWidth="2"
              />
              <rect
                x="4"
                y="11"
                width="16"
                height="4"
                rx="1"
                strokeWidth="2"
              />
              <rect
                x="4"
                y="17"
                width="16"
                height="2"
                rx="1"
                strokeWidth="2"
              />
            </svg>
            <span>Sections</span>
          </div>
          <div className="space-y-1">
            {sectionStarts.map((s) => (
              <button
                key={s.id}
                type="button"
                className="w-full text-left p-2 rounded transition-colors"
                style={{
                  background: 'var(--white-06)',
                  border: '1px solid var(--overlay-line-90)',
                  color: 'var(--text-heading)',
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                    'var(--white-12)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                    'var(--white-06)';
                }}
                onClick={() => scrollToInteraction(s.id)}
              >
                <div className="text-12 truncate">
                  {(s as any).content || 'Section'}
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Pinned messages */}
      <div
        className="text-[11px] mb-1 mt-3 flex items-center gap-1"
        style={{ color: 'var(--text-tertiary-weak)' }}
      >
        <svg
          className="w-3.5 h-3.5"
          viewBox="0 0 24 24"
          fill="currentColor"
          style={{ color: 'var(--accent-research)' }}
        >
          <path d="M5 5a2 2 0 012-2h10a2 2 0 012 2v16l-7-3.5L5 21V5z" />
        </svg>
        <span>Pinned messages</span>
      </div>
      {pinnedInteractions.length === 0 ? (
        <div
          className="text-12"
          style={{
            color: 'var(--text-secondary-strong)',
          }}
        >
          No pinned messages yet.
        </div>
      ) : (
        pinnedInteractions.map((it) => (
          <div
            key={it.id}
            className="p-2 rounded text-left w-full transition-colors"
            style={{
              background: 'var(--white-06)',
              border: '1px solid var(--overlay-line-90)',
            }}
          >
            <div className="flex items-start justify-between gap-2">
              <button
                type="button"
                onClick={() => scrollToInteraction(it.id)}
                className="text-left"
                style={{ flex: 1 }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLButtonElement).parentElement!.parentElement!.style.backgroundColor =
                    'var(--white-12)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLButtonElement).parentElement!.parentElement!.style.backgroundColor =
                    'var(--white-06)';
                }}
              >
                <div
                  className="text-[11px] mb-1"
                  style={{
                    color: 'var(--text-tertiary-weak)',
                  }}
                >
                  {it.kind === 'assistant'
                    ? 'Assistant'
                    : it.kind === 'user'
                      ? 'User'
                      : it.agentProfile
                        ? 'Agent'
                        : it.kind || ''}
                </div>
                <div
                  className="text-12"
                  style={{
                    color: 'var(--text-heading)',
                    whiteSpace: 'pre-wrap',
                  }}
                >
                  {truncateCenterWords(
                    it.contentSanitized || it.content || '',
                  )}
                </div>
              </button>
            </div>
            {Array.isArray(it?.metadata?.notes) &&
              it.metadata!.notes!.length > 0 && (
                <div className="mt-2 space-y-1">
                  {it.metadata!.notes!.map((note, idx) => (
                    <div
                      key={idx}
                      className="flex items-start justify-between gap-2 p-1 rounded"
                      style={{
                        background: 'var(--white-10)',
                      }}
                    >
                      <button
                        type="button"
                        className="text-left flex-1"
                        style={{
                          color: 'var(--text-secondary-strong)',
                          whiteSpace: 'pre-wrap',
                        }}
                        title="Scroll to note in message"
                        onClick={(e) => {
                          e.stopPropagation();
                          try {
                            const container = document.querySelector(
                              `.markdown[data-interaction-id="${it.id}"]`,
                            ) as HTMLElement | null;
                            if (container) {
                              const noteText = String(note).trim();
                              const r = noteText
                                ? findFirstRange(container, noteText)
                                : null;
                              if (r) {
                                const rect = r.getBoundingClientRect();
                                const host = messagesRef.current as HTMLElement | null;
                                if (host) {
                                  const hostRect = host.getBoundingClientRect();
                                  const offset =
                                    rect.top -
                                    hostRect.top +
                                    host.scrollTop -
                                    host.clientHeight / 2;
                                  host.scrollTo({
                                    top: Math.max(0, offset),
                                    behavior: 'smooth',
                                  });
                                } else {
                                  container.scrollIntoView({
                                    behavior: 'smooth',
                                    block: 'center',
                                  });
                                }
                              } else {
                                if (messagesRef.current) {
                                  const rect = container.getBoundingClientRect();
                                  const host = messagesRef.current;
                                  const hostRect = host.getBoundingClientRect();
                                  const offset =
                                    rect.top -
                                    hostRect.top +
                                    host.scrollTop -
                                    host.clientHeight / 2;
                                  host.scrollTo({
                                    top: Math.max(0, offset),
                                    behavior: 'smooth',
                                  });
                                } else {
                                  container.scrollIntoView({
                                    behavior: 'smooth',
                                    block: 'center',
                                  });
                                }
                              }
                            } else {
                              scrollToInteraction(it.id);
                            }
                          } catch (err) {
                            console.warn('[ScrollToNote] Failed', err);
                            try {
                              scrollToInteraction(it.id);
                            } catch {}
                          }
                        }}
                      >
                        <span className="text-[12px]">{note}</span>
                      </button>
                      <button
                        type="button"
                        aria-label="Remove note"
                        className="p-1 rounded"
                        style={{
                          background: 'transparent',
                          color: 'var(--text-secondary-strong)',
                        }}
                        onClick={async (ev) => {
                          ev.stopPropagation();
                          await onRemoveNote(it.id, idx);
                        }}
                        onMouseEnter={(ev) => {
                          (ev.currentTarget as HTMLButtonElement).style.backgroundColor =
                            'var(--white-12)';
                        }}
                        onMouseLeave={(ev) => {
                          (ev.currentTarget as HTMLButtonElement).style.backgroundColor =
                            'transparent';
                        }}
                      >
                        <svg
                          className="w-3 h-3"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                        >
                          <path
                            strokeLinecap="round"
                            strokeWidth={2}
                            d="M6 6l12 12M18 6L6 18"
                          />
                        </svg>
                      </button>
                    </div>
                  ))}
                </div>
              )}
          </div>
        ))
      )}
    </div>
  );
};


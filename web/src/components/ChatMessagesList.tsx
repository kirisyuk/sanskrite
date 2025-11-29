import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { handleEditTextDirect } from '../lib/textEditHandler';
import { findEvalBiasInteraction } from '../lib/interactionHelpers';
import { parseGraphData } from '../lib/graphDataParser';
import { applyEditHighlight } from '../lib/editHighlight';
import ChatMessage from './ChatMessage';
import { MessageItem } from './MessageItem';
import { SectionMarker } from './SectionMarker';
import { ActionInteraction } from './ActionInteraction';
import { BringInIndicator } from './BringInIndicator';
import { ResearchInteraction } from './ResearchInteraction';
import { AgentExecutionInteraction } from './AgentExecutionInteraction';
import { MessageActions } from './MessageActions';
import { CollapsibleText } from './CollapsibleText';
import { MarkdownRenderer } from '../lib/markdownComponents';
import { EvaluationDetails } from './EvaluationDetails';
import ChatGraphMessage from './ChatGraphMessage';
import { ActionRow } from './ActionRow';
import { ThinkingPlaceholder } from './ThinkingPlaceholder';

interface ChatMessagesListProps {
  mergedMessages: Array<{ type: 'message' | 'interaction'; data: any }>;
  genaiChat: {
    streaming: boolean;
    error: any;
    messageMeta: Record<string, any>;
    preStanceLatency: Record<string, number>;
    evaluations: Record<string, any>;
    interactions: any[];
    messages: any[];
    persistedByLocalId?: Record<string, string>;
  };
  genaiThinkingSeconds: number;
  averageTimingSeconds: number | null;
  genaiSettings: {
    strength: string;
    model: string;
    groundingEnabled: boolean;
  };
  expandedChip: string | null;
  onExpandChip: (chipId: string | null) => void;
  collapsedSections: Record<string, boolean>;
  onToggleCollapse: (sectionId: string) => void;
  frozenEdits: Record<string, boolean>;
  editErrors: Record<string, string | null>;
  editLast: Record<string, any>;
  editHighlight: Record<string, any>;
  onEditStart: (id: string) => void;
  onEditFinish: (id: string) => void;
  onEditError: (id: string, error: string) => void;
  onEditUpdate: (id: string, content: string) => void;
}

export const ChatMessagesList: React.FC<ChatMessagesListProps> = ({
  mergedMessages,
  genaiChat,
  genaiThinkingSeconds,
  averageTimingSeconds,
  genaiSettings,
  expandedChip,
  onExpandChip,
  collapsedSections,
  onToggleCollapse,
  frozenEdits,
  editErrors,
  editLast,
  editHighlight,
  onEditStart,
  onEditFinish,
  onEditError,
  onEditUpdate,
}) => {
  return (
    <>
      {(() => {
        // Merge messages and interactions, sort by timestamp to ensure correct order
        return mergedMessages.map((item) => {
          if (item.type === 'message') {
            const m = item.data;
            return (
              <MessageItem
                key={m.id}
                message={m}
                genaiChat={{
                  streaming: genaiChat.streaming,
                  error: genaiChat.error,
                  messageMeta: genaiChat.messageMeta,
                  preStanceLatency: genaiChat.preStanceLatency,
                  evaluations: genaiChat.evaluations,
                }}
                genaiThinkingSeconds={genaiThinkingSeconds}
                averageTimingSeconds={averageTimingSeconds}
                genaiSettings={{
                  strength: genaiSettings.strength,
                  model: genaiSettings.model,
                  groundingEnabled: genaiSettings.groundingEnabled,
                }}
                expandedChip={expandedChip}
                onExpandChip={onExpandChip}
              />
            );
          } else {
            // item.type === 'interaction'
            const it = item.data;
            // Render section markers
            if (
              it.kind === 'section_start' ||
              it.kind === 'section_end'
            ) {
              return (
                <SectionMarker
                  key={it.id}
                  interaction={it}
                  collapsedSections={collapsedSections}
                  onToggleCollapse={onToggleCollapse}
                />
              );
            }
            
            // Special rendering for action interactions
            if (it.kind === 'action') {
              return (
                <ActionInteraction
                  key={it.id}
                  interaction={it}
                  collapsedSections={collapsedSections}
                />
              );
            }
            
            // Special rendering for research interactions
            if (it.kind === 'research') {
              return (
                <ResearchInteraction
                  key={it.id}
                  interaction={it}
                  collapsedSections={collapsedSections}
                />
              );
            }
            
            // Special rendering for agent execution interactions
            if (it.kind === 'agent.execution') {
              return (
                <AgentExecutionInteraction
                  key={it.id}
                  interaction={it}
                />
              );
            }
            
            // Skip ontology interactions (they are shown in Model tab, not in chat)
            if (it.kind === 'ontology') {
              return null;
            }
            
            const isUser = it.kind === 'user';
            // Skip rendering if inside a collapsed section
            try {
              const sid = (it as any).sectionId as
                | string
                | undefined;
              if (sid && collapsedSections[sid])
                return null;
            } catch {}
            
            // Special rendering for action.bring_in indicator messages
            if (
              isUser &&
              it.subkind === 'action.bring_in'
            ) {
              return (
                <BringInIndicator
                  key={it.id}
                  interaction={it}
                />
              );
            }
            
            const meta = genaiChat.messageMeta[it.id];
            const header = !isUser
              ? meta?.headerText ||
                it.author?.displayName ||
                (it.agentProfile ? 'agent' : 'assistant')
              : undefined;
            const leftAccent = !isUser
              ? (meta?.leftAccent ?? !!it.agentProfile)
              : false;
            
            // Check if this is an error interaction
            const isError = !isUser && it.subkind === 'error';
            const errorMessage = isError ? (it.content || genaiChat.error) : null;
            
            return (
              <div key={it.id} data-interaction-id={it.id} className="group relative">
                <MessageActions
                  interactionId={it.id}
                  isUser={isUser}
                  interaction={it}
                />
                <ChatMessage
                  role={isUser ? 'user' : 'assistant'}
                  align={isUser ? 'right' : 'left'}
                  isThinking={false}
                  thinkingSeconds={0}
                  isError={isError}
                  errorMessage={errorMessage}
                  headerText={header}
                  leftAccent={leftAccent}
                  footer={undefined}
                >
                  {isUser ? (
                    <>
                      <CollapsibleText maxLines={7}>
                        <MarkdownRenderer
                          content={it.content}
                          className="text-13 markdown"
                          style={{
                            color: 'var(--text-heading)',
                            whiteSpace: 'pre-wrap',
                          }}
                        />
                      </CollapsibleText>
                      {/* Lightweight action row for USER: only evaluation, no buttons */}
                      {(() => {
                        try {
                          const evalNode = findEvalBiasInteraction(
                            it.id,
                            genaiChat.interactions,
                            genaiChat.persistedByLocalId || {},
                          );
                          if (!evalNode) return null;
                          return (
                            <div className="mt-1">
                              <EvaluationDetails messageId={it.id} />
                            </div>
                          );
                        } catch {
                          return null;
                        }
                      })()}
                    </>
                  ) : (
                    <>
                      {/* For error interactions, content is already shown via ChatMessage errorMessage prop */}
                      {!isError && (
                    <>
                      {/* Graph message rendering */}
                      {(() => {
                        const gd = parseGraphData(it);
                        if (gd) {
                          return (
                            <div data-interaction-id={it.id}>
                              <ChatGraphMessage
                                chartTitle={gd.chartTitle}
                                chartType={gd.chartType || 'line'}
                                table={gd.table}
                                sources={gd.sources}
                              />
                            </div>
                          );
                        }
                        const contentWithHighlight = applyEditHighlight(
                          (it.contentSanitized || it.content) as string,
                          editHighlight[it.id],
                        );
                        
                        return (
                          <div
                            className="text-13 markdown"
                            data-interaction-id={it.id}
                            style={{
                              color: 'var(--text-heading)',
                              position: 'relative',
                              opacity: frozenEdits[it.id] ? 0.6 : 1,
                              pointerEvents: frozenEdits[it.id]
                                ? ('none' as any)
                                : 'auto',
                            }}
                          >
                            <MarkdownRenderer
                              content={contentWithHighlight}
                              useRaw={true}
                              skipWrapper={true}
                            />
                            {/* Top-right status chip */}
                            {(frozenEdits[it.id] ||
                              editErrors[it.id]) && (
                              <div
                                className="absolute"
                                style={{
                                  top: 4,
                                  right: 4,
                                }}
                              >
                                {frozenEdits[it.id] && (
                                  <span
                                    className="px-2 py-0.5 rounded text-[10px]"
                                    style={{
                                      background:
                                        'var(--white-10)',
                                      border:
                                        '1px solid var(--overlay-line-90)',
                                      color:
                                        'var(--text-secondary-strong)',
                                    }}
                                  >
                                    <span
                                      className="inline-block mr-1"
                                      style={{
                                        width: 8,
                                        height: 8,
                                        borderRadius: 9999,
                                        background:
                                          'var(--text-secondary-strong)',
                                        opacity: 0.6,
                                        animation:
                                          'pulse 1.2s infinite',
                                      }}
                                    />
                                    Updating…
                                  </span>
                                )}
                                {editErrors[it.id] &&
                                  !frozenEdits[it.id] && (
                                    <span
                                      className="px-2 py-0.5 rounded text-[10px]"
                                      style={{
                                        background:
                                          'var(--bg-elevated)',
                                        border:
                                          '1px solid var(--overlay-line-90)',
                                        color:
                                          'var(--accent-danger)',
                                      }}
                                    >
                                      Failed
                                      <button
                                        className="ml-2 underline"
                                        style={{
                                          color:
                                            'var(--text-secondary-strong)',
                                        }}
                                        onClick={() => {
                                          const last =
                                            editLast[
                                              it.id
                                            ];
                                          if (
                                            last &&
                                            last.request
                                          ) {
                                            // re-run with saved request
                                            (async () => {
                                              const req =
                                                last.request;
                                              const selText =
                                                last.selected;
                                              const markdownRoot =
                                                document.querySelector(
                                                  `[data-interaction-id="${it.id}"]`,
                                                ) as HTMLElement | null;
                                              if (
                                                !markdownRoot
                                              )
                                                return;
                                              const inter =
                                                useGenaiChatStore
                                                  .getState()
                                                  .interactions.find(
                                                    (x) =>
                                                      x.id ===
                                                      it.id,
                                                  );
                                              if (!inter) return;
                                              const full =
                                                (
                                                  inter?.contentSanitized ||
                                                  inter?.content ||
                                                  ''
                                                ).toString();
                                              const displayed =
                                                (markdownRoot.innerText || '').toString();
                                              const meta =
                                                useGenaiChatStore.getState()
                                                  .messageMeta[
                                                  it.id
                                                ];
                                              const agentName =
                                                meta?.headerText ||
                                                'agent';
                                              const prof =
                                                meta?.agentProfile;
                                              await handleEditTextDirect({
                                                interactionId: it.id,
                                                selectedText: selText,
                                                fullText: full,
                                                displayedText: displayed,
                                                agentName,
                                                agentProfile: prof,
                                                request: req,
                                                onUpdate: (id, newContent) => {
                                                  onEditUpdate(id, newContent);
                                                },
                                                onError: (id, error) => {
                                                  onEditError(id, error);
                                                },
                                                onStart: (id) => {
                                                  onEditStart(id);
                                                },
                                                onFinish: (id) => {
                                                  onEditFinish(id);
                                                },
                                              });
                                            })();
                                          }
                                        }}
                                      >
                                        Retry
                                      </button>
                                      <button
                                        className="ml-1 underline"
                                        style={{
                                          color:
                                            'var(--text-secondary-strong)',
                                        }}
                                        onClick={() =>
                                          onEditError(it.id, null as any)
                                        }
                                      >
                                        Cancel
                                      </button>
                                    </span>
                                  )}
                              </div>
                            )}
                            {frozenEdits[it.id] && (
                              <div
                                className="absolute inset-0"
                                style={{
                                  pointerEvents: 'none',
                                }}
                              >
                                <div
                                  className="absolute inset-0"
                                  style={{
                                    background:
                                      'linear-gradient(90deg, transparent, var(--white-06), transparent)',
                                    animation:
                                      'etg-shimmer 2s infinite',
                                  }}
                                />
                              </div>
                            )}
                            {editErrors[it.id] && (
                              <div
                                className="mt-1 px-2 py-1 rounded"
                                style={{
                                  border:
                                    '1px solid var(--overlay-line-90)',
                                  background:
                                    'var(--bg-elevated)',
                                }}
                              >
                                <div
                                  className="text-[11px]"
                                  style={{
                                    color:
                                      'var(--accent-danger)',
                                  }}
                                >
                                  Edit failed:{' '}
                                  {editErrors[it.id]}
                                </div>
                              </div>
                            )}
                          </div>
                        );
                      })()}
                      {/* End of graph/markdown conditional rendering */}
                        </>
                      )}
                      {/* Tags row: per-interaction chips for assistant - BEFORE ActionRow/EvaluationDetails */}
                      {!isUser && !isError &&
                        (() => {
                          try {
                            const perQ = Array.isArray(
                              (it as any).searchQueries,
                            )
                              ? (
                                  it as any
                                ).searchQueries.filter(
                                  Boolean,
                                )
                              : [];
                            const perS = Array.isArray(
                              (it as any).sources,
                            )
                              ? (
                                  it as any
                                ).sources.filter(Boolean)
                              : [];
                            const qs = perQ.slice(0, 4);
                            const attrs =
                              perS.length > 0
                                ? perS
                                    .map(
                                      (url: string) => ({
                                        url,
                                      }),
                                    )
                                    .slice(0, 4)
                                : [];
                            if (
                              qs.length === 0 &&
                              attrs.length === 0
                            )
                              return null;
                            return (
                              <div className="mt-1">
                                <div
                                  className="text-[11px] mb-1"
                                  style={{
                                    color:
                                      'var(--text-tertiary-weak)',
                                  }}
                                >
                                  Sources:
                                </div>
                                <div className="flex flex-wrap gap-1">
                                  {qs.map(
                                    (
                                      q: string,
                                      i: number,
                                    ) => {
                                      const chipId = `${it.id}-q-${i}`;
                                      const isExpanded =
                                        expandedChip ===
                                        chipId;
                                      const words =
                                        String(q || '')
                                          .trim()
                                          .split(/\s+/);
                                      const short =
                                        words.length > 3
                                          ? words
                                              .slice(0, 3)
                                              .join(' ') +
                                            '…'
                                          : String(
                                              q || '',
                                            );
                                      const needsExpand =
                                        words.length > 3;
                                      return (
                                        <button
                                          key={`q-${i}`}
                                          type="button"
                                          className="px-2 py-0.5 rounded-full text-[10px] transition-colors"
                                          style={{
                                            backgroundColor:
                                              isExpanded
                                                ? 'var(--white-20)'
                                                : 'var(--white-10)',
                                            color:
                                              'var(--text-secondary-strong)',
                                            border:
                                              'none',
                                            cursor:
                                              needsExpand
                                                ? 'pointer'
                                                : 'default',
                                          }}
                                          onClick={() =>
                                            needsExpand &&
                                            onExpandChip(
                                              isExpanded
                                                ? null
                                                : chipId,
                                            )
                                          }
                                          onMouseEnter={(
                                            e,
                                          ) => {
                                            if (
                                              needsExpand
                                            )
                                              (
                                                e.currentTarget as HTMLButtonElement
                                              ).style.backgroundColor =
                                                isExpanded
                                                  ? 'var(--white-25)'
                                                  : 'var(--white-15)';
                                          }}
                                          onMouseLeave={(
                                            e,
                                          ) => {
                                            if (
                                              needsExpand
                                            )
                                              (
                                                e.currentTarget as HTMLButtonElement
                                              ).style.backgroundColor =
                                                isExpanded
                                                  ? 'var(--white-20)'
                                                  : 'var(--white-10)';
                                          }}
                                        >
                                          {isExpanded
                                            ? String(
                                                q || '',
                                              )
                                            : short}
                                        </button>
                                      );
                                    },
                                  )}
                                  {attrs.map(
                                    (
                                      a: any,
                                      i: number,
                                    ) => {
                                      const chipId = `${it.id}-src-${i}`;
                                      const isExpanded =
                                        expandedChip ===
                                        chipId;
                                      const raw = String(
                                        (a as any).url ||
                                          '',
                                      ).replace(
                                        /^https?:\/\//i,
                                        '',
                                      );
                                      const parts =
                                        raw.split('/');
                                      const host =
                                        parts[0] || '';
                                      const tail =
                                        parts[1]
                                          ? `/${parts[1]}`
                                          : '';
                                      const label =
                                        host + tail ||
                                        (a as any)
                                          .title ||
                                        'link';
                                      const needsExpand =
                                        raw.length >
                                        (host + tail)
                                          .length;
                                      return (
                                        <a
                                          key={`src-${i}`}
                                          href={
                                            (a as any).url
                                          }
                                          target="_blank"
                                          rel="noreferrer"
                                          className="px-2 py-0.5 rounded-full text-[10px] underline transition-colors"
                                          style={{
                                            backgroundColor:
                                              isExpanded
                                                ? 'var(--white-20)'
                                                : 'var(--white-10)',
                                            color:
                                              'var(--text-secondary-strong)',
                                          }}
                                          onClick={(
                                            e,
                                          ) => {
                                            if (
                                              needsExpand
                                            ) {
                                              e.preventDefault();
                                              onExpandChip(
                                                isExpanded
                                                  ? null
                                                  : chipId,
                                              );
                                            }
                                          }}
                                          onMouseEnter={(
                                            e,
                                          ) => {
                                            (
                                              e.currentTarget as HTMLAnchorElement
                                            ).style.backgroundColor =
                                              isExpanded
                                                ? 'var(--white-25)'
                                                : 'var(--white-15)';
                                          }}
                                          onMouseLeave={(
                                            e,
                                          ) => {
                                            (
                                              e.currentTarget as HTMLAnchorElement
                                            ).style.backgroundColor =
                                              isExpanded
                                                ? 'var(--white-20)'
                                                : 'var(--white-10)';
                                          }}
                                        >
                                          {isExpanded
                                            ? raw
                                            : label}
                                        </a>
                                      );
                                    },
                                  )}
                                </div>
                              </div>
                            );
                          } catch {
                            return null;
                          }
                        })()}
                      {!isUser && !isError && (
                        <ActionRow
                          messageId={it.id}
                          isAssistant={!isUser}
                          content={
                            it.contentSanitized ||
                            it.content
                          }
                        />
                      )}
                    </>
                  )}
                </ChatMessage>
              </div>
            );
          }
        });
      })()}
      {genaiChat.streaming &&
        (() => {
          try {
            // Find the last assistant message (check from end of array)
            const messages = genaiChat.messages || [];
            let m = null;
            for (let i = messages.length - 1; i >= 0; i--) {
              if (messages[i]?.role === 'assistant') {
                m = messages[i];
                break;
              }
            }
            
            if (!m) return null;
            
            // Don't show placeholder if message has content (streaming has started producing content)
            if (m.content && m.content.trim())
              return null;
            
            // CRITICAL: Don't show placeholder if research mode is active (research.progress interaction exists)
            // This check ensures thinking placeholder is hidden during research mode
            const hasActiveResearch = genaiChat.interactions.some(
              (it: any) => it.kind === 'research' && it.subkind === 'research.progress'
            );
            if (hasActiveResearch) {
              return null;
            }
            
            // Show thinking placeholder for all other modes (regular chat, agents, etc.)
            return (
              <ThinkingPlaceholder
                key={m.id}
                messageId={m.id}
                genaiThinkingSeconds={genaiThinkingSeconds}
                averageTimingSeconds={averageTimingSeconds}
              />
            );
          } catch {
            return null;
          }
        })()}
    </>
  );
};


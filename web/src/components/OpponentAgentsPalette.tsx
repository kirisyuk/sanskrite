import React, { useState, useRef, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { AGENT_PRESETS } from '../lib/agentPresets';
import { buildContext } from '../lib/contextBuilder';
import { mapAliasToModel } from '../lib/modelMapping';
import { TelegramFetchUpdatesTool } from '../lib/genaiTools';
import { getPreferredLanguage, getLanguageName } from '../lib/uiUtils';

interface OpponentAgentsPaletteProps {
  unfoldSubView: 'chat' | 'perspectives';
  genaiChatInteractionsLength: number;
  genaiChatMessagesLength: number;
}

export const OpponentAgentsPalette: React.FC<OpponentAgentsPaletteProps> = ({
  unfoldSubView,
  genaiChatInteractionsLength,
  genaiChatMessagesLength,
}) => {
  const { t } = useTranslation();
  const genaiChat = useGenaiChatStore();
  const genaiSettings = useGenAISettings();
  
  const [showOpponentAgents, setShowOpponentAgents] = useState(false);
  const [expandedAgentId, setExpandedAgentId] = useState<string | null>(null);
  const [opponentButtonBottom, setOpponentButtonBottom] = useState<number>(120);
  
  const opponentAgentsRef = useRef<HTMLDivElement | null>(null);
  const opponentButtonRef = useRef<HTMLDivElement | null>(null);

  // Close opponent agents list on outside click
  useEffect(() => {
    if (!showOpponentAgents) return;
    
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        opponentButtonRef.current && 
        opponentButtonRef.current.contains(target)
      ) {
        return; // Click on button itself - handled by onClick
      }
      if (
        opponentAgentsRef.current && 
        opponentAgentsRef.current.contains(target)
      ) {
        return; // Click inside agents list
      }
      setShowOpponentAgents(false);
    };
    
    document.addEventListener('mousedown', handleClickOutside, {
      capture: true,
    });
    return () => {
      document.removeEventListener('mousedown', handleClickOutside, {
        capture: true,
      } as any);
    };
  }, [showOpponentAgents]);

  // Track UnfoldRequest container position for opponent button positioning
  useEffect(() => {
    if (unfoldSubView === 'perspectives') return;
    
    const updatePosition = () => {
      const container = document.getElementById('unfold-request-container');
      if (!container) return;
      
      // Find parent container with position: relative (the chat container)
      const parentContainer = container.closest(
        '[style*="position: relative"]',
      ) as HTMLElement;
      if (!parentContainer) return;
      
      // Calculate distance from bottom of parent container to top of UnfoldRequest
      const parentHeight = parentContainer.offsetHeight;
      const containerTop = container.offsetTop;
      const distanceFromBottom = parentHeight - containerTop;
      
      // Position button 0.5rem (8px) above UnfoldRequest
      setOpponentButtonBottom(distanceFromBottom + 8);
    };
    
    // Initial position with small delay to ensure DOM is ready
    const timeoutId = setTimeout(updatePosition, 0);
    
    // Use ResizeObserver to track size changes of UnfoldRequest
    const container = document.getElementById('unfold-request-container');
    if (!container) {
      clearTimeout(timeoutId);
      return;
    }
    
    const resizeObserver = new ResizeObserver(() => {
      updatePosition();
    });
    
    resizeObserver.observe(container);
    
    // Also observe parent container for size changes
    const parentContainer = container.closest(
      '[style*="position: relative"]',
    ) as HTMLElement;
    if (parentContainer) {
      resizeObserver.observe(parentContainer);
      
      // Update on scroll (in case parent scrolls)
      parentContainer.addEventListener('scroll', updatePosition, {
        passive: true,
      });
    }
    
    // Update on window resize
    window.addEventListener('resize', updatePosition, { passive: true });
    
    return () => {
      clearTimeout(timeoutId);
      resizeObserver.disconnect();
      if (parentContainer) {
        parentContainer.removeEventListener('scroll', updatePosition);
      }
      window.removeEventListener('resize', updatePosition);
    };
  }, [unfoldSubView, genaiChatInteractionsLength, genaiChatMessagesLength]);

  // Don't render if in perspectives view or no messages
  if (
    unfoldSubView === 'perspectives' ||
    (genaiChatInteractionsLength === 0 && genaiChatMessagesLength === 0)
  ) {
    return null;
  }

  return (
    <div
      ref={opponentButtonRef}
      className="absolute"
      style={{
        bottom: `${opponentButtonBottom}px`, // Dynamically calculated from UnfoldRequest top
        right: '1rem', // Align to right edge (same padding as px-4 = 1rem)
        zIndex: 10,
        pointerEvents: 'auto',
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
      }}
    >
      {/* Agents list - appears to the left */}
      {showOpponentAgents && (
        <div
          ref={opponentAgentsRef}
          className="flex items-center gap-2"
          style={{
            animation: 'fadeInScale 0.2s ease-out',
            transformOrigin: 'right center',
          }}
        >
          {AGENT_PRESETS.map((preset) => {
            const isExpanded = expandedAgentId === preset.id;
            
            return (
              <div
                key={preset.id}
                className="flex items-center gap-2"
                style={{
                  transition: 'all 0.2s ease',
                }}
              >
                <button
                  type="button"
                  aria-label={preset.displayName}
                  title={preset.description}
                  onClick={async () => {
                    // If already expanded, launch the agent
                    if (isExpanded) {
                      setShowOpponentAgents(false);
                      setExpandedAgentId(null);
                      
                      try {
                        const pid = await genaiChat.ensureProject();
                        const preferredLang = getPreferredLanguage();
                        
                        // Build context using unified function (70% of context window)
                        const context = buildContext({
                          modelAlias: preset.modelAlias,
                          contextPercentage: 0.7,
                          includeAgents: true,
                        });
                        
                        console.log(
                          '[Opponent] Context built:',
                          {
                            messagesCount: context.messages.length,
                            tokenCount: context.tokenCount,
                            maxTokens: context.maxTokens,
                            charCount: context.charCount,
                          },
                        );
                        
                        // Build prompt for agent discussion
                        // Use agent's system instruction if available, otherwise use default prompt
                        const basePrompt = preset.params?.systemInstruction
                          ? `${preset.params.systemInstruction}\n\nContext from the discussion:\n${context.transcript}\n\nRespond in ${getLanguageName(preferredLang)} language.`
                          : `You are participating in a discussion as an opponent, providing a counterpoint or alternative perspective.

Your role:
- Challenge assumptions and provide alternative viewpoints
- Identify potential weaknesses or gaps in the arguments presented
- Offer constructive criticism and different perspectives
- Maintain respectful and productive discourse
- Focus on substantive arguments rather than personal attacks

Context from the discussion:
${context.transcript}

Your response should:
1. Acknowledge the key points made in the discussion
2. Present alternative perspectives or counterarguments
3. Identify any assumptions, gaps, or potential weaknesses
4. Contribute constructively to the discussion

Respond in ${getLanguageName(preferredLang)} language.`;
                        
                        const prompt = basePrompt;

                        // Get model and options
                        const modelMapping = mapAliasToModel(preset.modelAlias);
                        if (!modelMapping) {
                          console.error(
                            '[Opponent] Failed to map model alias:',
                            preset.modelAlias,
                          );
                          return;
                        }
                        const tools = preset.capabilities?.tools
                          ? genaiSettings.enableTelegramTool
                            ? [TelegramFetchUpdatesTool]
                            : undefined
                          : undefined;
                        const options: any = {
                          profile: preset.modelAlias.includes('quality')
                            ? 'quality'
                            : 'fast',
                          temperature: preset.params?.temperature ?? 0.7,
                          systemInstruction: preset.params?.systemInstruction,
                          __lang: preferredLang,
                        };

                        // Create indicator message
                        const indicatorContent = `${t('unfold.actions.opponentInvoked')} ${preset.displayName}`;
                        await genaiChat.createIndicatorMessage(
                          indicatorContent,
                          'action.opponent',
                          undefined,
                          {
                            agentProfile: {
                              provider: modelMapping.provider,
                              model: modelMapping.model,
                              capabilities: {
                                tools: !!preset.capabilities?.tools,
                                webSearch: !!preset.capabilities?.webSearch,
                                reasoning: !!preset.capabilities?.reasoning,
                              },
                              params: preset.params,
                            },
                          },
                        );

                        // Start stream with context
                        await genaiChat.startStream({
                          prompt,
                          tools,
                          options,
                          agentMeta: {
                            displayName: preset.displayName,
                            agentProfile: {
                              provider: modelMapping.provider,
                              model: modelMapping.model,
                              capabilities: {
                                tools: !!preset.capabilities?.tools,
                                webSearch: !!preset.capabilities?.webSearch,
                                reasoning: !!preset.capabilities?.reasoning,
                              },
                              params: preset.params,
                            },
                            subkind: 'agent.opponent',
                          },
                          suppressUserEcho: true,
                        });
                      } catch (err) {
                        console.error(
                          '[Opponent] Failed to start opponent discussion',
                          err,
                        );
                      }
                    } else {
                      // Expand this agent (collapse others)
                      setExpandedAgentId(preset.id);
                    }
                  }}
                  className="flex items-center justify-center rounded-full transition-all shadow-lg"
                  style={{
                    width: isExpanded ? 'auto' : '32px',
                    height: '32px',
                    backgroundColor: isExpanded
                      ? 'var(--white-15)'
                      : 'var(--bg-elevated)',
                    color: 'var(--text-heading)',
                    border: '1px solid var(--overlay-line-90)',
                    backdropFilter: 'blur(8px)',
                    opacity: 0.95,
                    padding: isExpanded ? '0 12px' : '0',
                    gap: isExpanded ? '8px' : '0',
                  }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.opacity = '1';
                    if (!isExpanded) {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                        'var(--white-15)';
                    }
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.opacity = '0.95';
                    if (!isExpanded) {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                        'var(--bg-elevated)';
                    }
                  }}
                >
                  {preset.icon || (
                    <svg
                      className="w-4 h-4"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <circle cx="12" cy="12" r="10" />
                      <path d="M12 16v-4M12 8h.01" />
                    </svg>
                  )}
                  {isExpanded && (
                    <span className="text-xs font-medium whitespace-nowrap">
                      {preset.displayName}
                    </span>
                  )}
                </button>
              </div>
            );
          })}
        </div>
      )}
      
      {/* Main opponent button - scales */}
      <button
        type="button"
        aria-label={t('unfold.request.opponent')}
        title={t('unfold.request.opponentExplain')}
        onClick={() => {
          setShowOpponentAgents(!showOpponentAgents);
          if (showOpponentAgents) {
            setExpandedAgentId(null); // Reset expanded state when closing
          }
        }}
        className="flex items-center justify-center rounded-full transition-all shadow-lg"
        style={{
          width: '32px',
          height: '32px',
          backgroundColor: showOpponentAgents
            ? 'var(--white-15)'
            : 'var(--bg-elevated)',
          color: 'var(--text-heading)',
          border: '1px solid var(--overlay-line-90)',
          backdropFilter: 'blur(8px)',
          opacity: showOpponentAgents ? 1 : 0.95,
        }}
        onMouseEnter={(e) => {
          if (!showOpponentAgents) {
            (e.currentTarget as HTMLButtonElement).style.opacity = '1';
            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)';
          }
        }}
        onMouseLeave={(e) => {
          if (!showOpponentAgents) {
            (e.currentTarget as HTMLButtonElement).style.opacity = '0.95';
            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--bg-elevated)';
          }
        }}
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          {/* Flag - marking position/argument */}
          <path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z" />
          <line x1="4" y1="22" x2="4" y2="15" />
        </svg>
      </button>
    </div>
  );
};


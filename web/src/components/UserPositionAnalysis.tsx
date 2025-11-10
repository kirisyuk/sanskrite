import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useAuth } from '../contexts/AuthContext';
import { isGeneratingUserStanceAnalysis } from '../lib/userStanceAnalysis';

// Component to analyze user's position based on their interactions
export const UserPositionAnalysis: React.FC<{ onFirstResult?: () => void; onRiskUpdate?: (r: { level: string; score: number; rationale?: string } | null) => void; hideTitle?: boolean }> = ({ onFirstResult, onRiskUpdate, hideTitle = false }) => {
  const [showRiskRationale, setShowRiskRationale] = React.useState(false);
  // PERFORMANCE FIX: Subscribe only to interactions.length, not entire array
  const interactionsLength = useGenaiChatStore((s) => s.interactions.length);
  const projectId = useGenaiChatStore((s) => s.projectId);
  const { currentUser } = useAuth();
  
  // Get interactions only when needed via getState()
  const getInteractions = React.useCallback(() => useGenaiChatStore.getState().interactions, []);
  
  // Find the project-level eval.stance interaction (not tied to a specific message)
  const stanceAnalysis = React.useMemo(() => {
    try {
      const interactions = getInteractions();
      const currentProjectId = useGenaiChatStore.getState().projectId;
      return interactions.find((it) => 
        it.subkind === 'eval.stance' && 
        it.projectId === currentProjectId && 
        !it.replyTo // Project-level analysis, not tied to specific message
      ) || null;
    } catch { return null; }
  }, [getInteractions, interactionsLength, projectId]);

  const notifiedRef = React.useRef(false);
  
  // Notify on first result
  React.useEffect(() => {
    if (stanceAnalysis && !notifiedRef.current) {
      notifiedRef.current = true;
      onFirstResult && onFirstResult();
      // Notify about risk if present
      if (stanceAnalysis.highRisk && onRiskUpdate) {
        try {
          onRiskUpdate({
            level: stanceAnalysis.highRisk.level,
            score: stanceAnalysis.highRisk.score,
            rationale: stanceAnalysis.highRisk.rationale,
          });
        } catch {}
      }
    }
  }, [stanceAnalysis, onFirstResult, onRiskUpdate]);

  // Check if generation is in progress
  const isLoading = React.useMemo(() => {
    if (!projectId) return false;
    const chatMessages = getInteractions().filter((it) => 
      it.kind === 'user' || it.kind === 'assistant'
    );
    return isGeneratingUserStanceAnalysis(projectId, chatMessages.length);
  }, [projectId, getInteractions, interactionsLength]);

  // Do not render until there's at least one user message
  const hasUserMessages = React.useMemo(() => {
    const interactions = getInteractions();
    return interactions.some((it) => it.kind === 'user');
  }, [getInteractions, interactionsLength]);

  if (!hasUserMessages) return null;
  
  const sanitizeSx = (s: string) => {
    try { return (s || '').replace(/<!--\s*@sx:[\s\S]*?-->/g, '').trim(); } catch { return s; }
      };
  
  // Display content from stanceAnalysis
  const displayContent = stanceAnalysis 
    ? (stanceAnalysis.contentSanitized || stanceAnalysis.content)
    : null;
  
  // Display risk from stanceAnalysis
  const displayRisk = stanceAnalysis?.highRisk;
  
  // Display roles and industries from stanceAnalysis
  const displayRoles = stanceAnalysis?.roles || [];
  const displayIndustries = stanceAnalysis?.industries || [];

  return (
    <div className="mt-3 p-3">
      {!hideTitle && (<div className="text-13 mb-2" style={{ color: '#669999' }}>How you engage</div>)}
      {isLoading ? (
        <div className="text-12 p-3 rounded-lg" style={{ color: 'var(--text-secondary)', backgroundColor: 'var(--toolbar-bg)' }}>Analyzing...</div>
      ) : displayContent ? (
        <>
          {/* Stance section */}
          <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
            <div className="text-12 fade-in" style={{ color: 'var(--text-secondary-strong)', lineHeight: '1.6', transition: 'opacity 0.2s', whiteSpace: 'pre-wrap' }}>
              {sanitizeSx(displayContent as string)}
            </div>
          </div>
          
          {/* Risk section */}
          {displayRisk && (
            <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
              <div className="flex items-center gap-2">
                {(() => {
                  const hr = displayRisk;
                  if (!hr) return null as any;
                  
                  const level = hr.level || 'Low';
                  
                  // Determine badge text and colors based on risk level
                  let badgeText = 'This discussion is safe';
                  let bgColor = 'rgba(34, 197, 94, 0.1)'; // green
                  let borderColor = 'rgba(34, 197, 94, 0.3)';
                  let textColor = 'rgb(34, 197, 94)';
                  
                  if (level === 'High') {
                    badgeText = 'High risk detected';
                    bgColor = 'rgba(239, 68, 68, 0.1)'; // red
                    borderColor = 'rgba(239, 68, 68, 0.3)';
                    textColor = 'rgb(239, 68, 68)';
                  } else if (level === 'Medium') {
                    badgeText = 'Moderate risk detected';
                    bgColor = 'rgba(251, 191, 36, 0.1)'; // yellow/orange
                    borderColor = 'rgba(251, 191, 36, 0.3)';
                    textColor = 'rgb(251, 191, 36)';
                  }
                  
                  const hasRationale = hr.rationale && hr.rationale.trim().length > 0;
                  
                  return (
                    <button
                      type="button"
                      onClick={() => hasRationale && setShowRiskRationale(!showRiskRationale)}
                      className={`inline-flex items-center gap-1 px-2 py-1 rounded text-[11px] font-medium ${hasRationale ? 'cursor-pointer hover:opacity-80 transition-opacity' : ''}`}
                      style={{ 
                        backgroundColor: bgColor,
                        border: `1px solid ${borderColor}`,
                        color: textColor
                      }}
                      disabled={!hasRationale}
                    >
                      {badgeText}
                    </button>
                  );
                })()}
              </div>
              {showRiskRationale && displayRisk?.rationale && (
                <div className="text-[11px] mt-2" style={{ color: 'var(--text-secondary-strong)' }}>
                  {displayRisk.rationale}
                </div>
              )}
            </div>
          )}
          
          {/* Roles and Industries section */}
          {(displayRoles.length > 0 || displayIndustries.length > 0) && (
            <div className="mb-4 p-3 rounded-lg" style={{ backgroundColor: 'var(--toolbar-bg)' }}>
              {displayRoles.length > 0 && (
                <div className="mb-2">
                  <div className="text-[11px] mb-1.5" style={{ color: 'var(--text-tertiary-weak)', fontWeight: 500 }}>
                    Professional roles
                  </div>
                  <div className="space-y-1.5">
                    {displayRoles.map((r: any, idx: number) => {
                      const confidencePercent = Math.round((r.confidence || 0) * 100);
                      return (
                        <div key={idx} className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                          <span style={{ fontWeight: 500 }}>{r.role}</span>
                          <span className="ml-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                            ({confidencePercent}%)
                          </span>
                          {r.rationale && (
                            <div className="mt-0.5 ml-0" style={{ color: 'var(--text-tertiary-weak)', fontSize: '10px' }}>
                              {r.rationale}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
              {displayIndustries.length > 0 && (
                <div>
                  <div className="text-[11px] mb-1.5" style={{ color: 'var(--text-tertiary-weak)', fontWeight: 500 }}>
                    Industries
                  </div>
                  <div className="space-y-1.5">
                    {displayIndustries.map((i: any, idx: number) => {
                      const confidencePercent = Math.round((i.confidence || 0) * 100);
                      return (
                        <div key={idx} className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                          <span style={{ fontWeight: 500 }}>{i.industry}</span>
                          <span className="ml-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                            ({confidencePercent}%)
                          </span>
                          {i.rationale && (
                            <div className="mt-0.5 ml-0" style={{ color: 'var(--text-tertiary-weak)', fontSize: '10px' }}>
                              {i.rationale}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          )}
        </>
      ) : (
        <div className="text-12 p-3 rounded-lg" style={{ color: 'var(--text-secondary)', backgroundColor: 'var(--toolbar-bg)' }}>No clear position identified yet.</div>
      )}
    </div>
  );
};


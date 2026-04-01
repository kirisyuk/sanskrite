import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  getRightPanelButtonStyle,
  getRightPanelButtonHoverStyle,
} from '../lib/buttonStyles';
import { RightPanelType } from '../hooks/usePanelsManagement';

interface RightPanelButtonsProps {
  rightPanelType: RightPanelType;
  onPanelTypeChange: (type: RightPanelType) => void;
  hasPinned: boolean;
  hasSections: boolean;
  availUserAnalysis: boolean;
  availDiscussionFlow: boolean;
  hasQueuedTasks?: boolean; // Show button if there are queued tasks
  unfoldSubView?: 'chat' | 'perspectives' | 'models' | 'pages' | 'scenarios'; // Show model-context button only in graph subview
  onScrollToTop?: () => void; // Scroll to top of chat
  onScrollToBottom?: () => void; // Scroll to bottom of chat
}

export const RightPanelButtons: React.FC<RightPanelButtonsProps> = ({
  rightPanelType,
  onPanelTypeChange,
  hasPinned,
  hasSections,
  availUserAnalysis,
  availDiscussionFlow,
  hasQueuedTasks = false,
  unfoldSubView,
  onScrollToTop,
  onScrollToBottom,
}) => {
  const { t } = useTranslation();

  const handleButtonClick = (type: RightPanelType) => {
    if (type === 'artifacts') {
      // Special handling for artifacts button
      try {
        const btn = document.getElementById('btn-artifacts');
        if (btn) btn.classList.remove('nv-glow-blue');
      } catch {}
    }
    
    if (type === 'workspace') {
      // Performance logging for workspace button
      const startTime = performance.now();
      console.log('[RightPanel] Click at', startTime, 'toggle', rightPanelType !== 'workspace');
      onPanelTypeChange(rightPanelType === 'workspace' ? null : 'workspace');
      const endTime = performance.now();
      console.log('[RightPanel] setState called at', endTime, 'delay:', endTime - startTime, 'ms');
    } else {
      onPanelTypeChange(rightPanelType === type ? null : type);
    }
  };

  const handleMouseEnter = (
    e: React.MouseEvent<HTMLButtonElement>,
    isActive: boolean,
  ) => {
    if (!isActive) {
      e.currentTarget.style.backgroundColor = getRightPanelButtonHoverStyle(false);
    }
  };

  const handleMouseLeave = (
    e: React.MouseEvent<HTMLButtonElement>,
    isActive: boolean,
  ) => {
    if (!isActive) {
      e.currentTarget.style.backgroundColor = getRightPanelButtonStyle(false).backgroundColor;
    }
  };

  return (
    <div
      className="flex flex-col items-center gap-2"
      style={{
        width: '48px',
        minWidth: '48px',
        flexShrink: 0,
        paddingTop: '76px',
      }}
    >
      {/* Workspace Library button */}
      <button
        type="button"
        aria-label={t('unfold.sections.toggleWorkspaceLibrary')}
        title={t('unfold.sections.toggleWorkspaceLibrary')}
        className="mirror-doc-btn rounded-lg flex items-center justify-center transition-all duration-300"
        style={{
          ...getRightPanelButtonStyle(rightPanelType === 'workspace'),
          backgroundColor: rightPanelType === 'workspace' ? '#669999' : '#669999',
          color: 'white',
        }}
        onClick={() => handleButtonClick('workspace')}
        onMouseEnter={(e) => {
          if (rightPanelType !== 'workspace') {
            e.currentTarget.style.backgroundColor = '#7aa3a3'; // Slightly lighter on hover
          }
        }}
        onMouseLeave={(e) => {
          if (rightPanelType !== 'workspace') {
            e.currentTarget.style.backgroundColor = '#669999';
          }
        }}
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          aria-hidden="true"
        >
          <path
            d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.5L13 4H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2z"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <path
            d="M8 10h8M8 14h6"
            strokeWidth={2}
            strokeLinecap="round"
          />
        </svg>
      </button>

      {/* Context Navigation button */}
      {(hasPinned || hasSections) && (
        <button
          type="button"
          aria-label={t('unfold.sections.contextNavigation')}
          title={t('unfold.sections.contextNavigation')}
          onClick={() => handleButtonClick('context')}
          className="rounded-lg flex items-center justify-center transition-all duration-300"
          style={getRightPanelButtonStyle(rightPanelType === 'context')}
          onMouseEnter={(e) => handleMouseEnter(e, rightPanelType === 'context')}
          onMouseLeave={(e) => handleMouseLeave(e, rightPanelType === 'context')}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M5 5a2 2 0 012-2h10a2 2 0 012 2v16l-7-3.5L5 21V5z" />
          </svg>
        </button>
      )}

      {/* Workspace Artifacts button */}
      <button
        id="btn-artifacts"
        type="button"
        aria-label={t('unfold.sections.workspaceArtifacts')}
        title={t('unfold.sections.workspaceArtifacts')}
        onClick={() => handleButtonClick('artifacts')}
        className="rounded-lg flex items-center justify-center transition-all duration-300"
        style={getRightPanelButtonStyle(rightPanelType === 'artifacts')}
        onMouseEnter={(e) => handleMouseEnter(e, rightPanelType === 'artifacts')}
        onMouseLeave={(e) => handleMouseLeave(e, rightPanelType === 'artifacts')}
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
          <line x1="16" y1="13" x2="8" y2="13" />
          <line x1="16" y1="17" x2="8" y2="17" />
          <polyline points="10 9 9 9 8 9" />
        </svg>
      </button>

      {/* Analysis button - shows if User Analysis or Discussion Flow is available */}
      {(availUserAnalysis || availDiscussionFlow) && (
        <button
          type="button"
          aria-label={t('unfold.sections.analysis', { defaultValue: 'Analysis' })}
          title={t('unfold.sections.analysis', { defaultValue: 'Analysis' })}
          onClick={() => handleButtonClick('analysis')}
          className="rounded-lg flex items-center justify-center transition-all duration-300"
          style={getRightPanelButtonStyle(rightPanelType === 'analysis')}
          onMouseEnter={(e) => handleMouseEnter(e, rightPanelType === 'analysis')}
          onMouseLeave={(e) => handleMouseLeave(e, rightPanelType === 'analysis')}
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
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="16" x2="12" y2="12" />
            <line x1="12" y1="8" x2="12.01" y2="8" />
          </svg>
        </button>
      )}

      {/* Pending Actions button - shows if there are queued tasks */}
      {hasQueuedTasks && (
        <button
          type="button"
          aria-label={t('unfold.sections.pendingActions', { defaultValue: 'Pending Actions' })}
          title={t('unfold.sections.pendingActions', { defaultValue: 'Pending Actions' })}
          onClick={() => handleButtonClick('pending-actions')}
          className="rounded-lg flex items-center justify-center transition-all duration-300"
          style={getRightPanelButtonStyle(rightPanelType === 'pending-actions')}
          onMouseEnter={(e) => handleMouseEnter(e, rightPanelType === 'pending-actions')}
          onMouseLeave={(e) => handleMouseLeave(e, rightPanelType === 'pending-actions')}
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
            <path d="M9 11l3 3L22 4" />
            <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11" />
          </svg>
        </button>
      )}

      {/* Model Context button - shows only in graph subview */}
      {unfoldSubView === 'models' && (
        <button
          type="button"
          aria-label="Graph Context"
          title="Graph Context"
          onClick={() => handleButtonClick('model-context')}
          className="rounded-lg flex items-center justify-center transition-all duration-300"
          style={getRightPanelButtonStyle(rightPanelType === 'model-context')}
          onMouseEnter={(e) => handleMouseEnter(e, rightPanelType === 'model-context')}
          onMouseLeave={(e) => handleMouseLeave(e, rightPanelType === 'model-context')}
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
            <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z" />
            <circle cx="12" cy="10" r="3" />
          </svg>
        </button>
      )}

      {/* Scroll buttons - only in Discuss (chat) mode */}
      {unfoldSubView === 'chat' && onScrollToTop && onScrollToBottom && (
        <>
          {/* Large gap before scroll buttons */}
          <div style={{ marginTop: '32px' }} />
          
          {/* Scroll to top button */}
          <button
            type="button"
            aria-label="Scroll to top"
            title="Scroll to top"
            onClick={onScrollToTop}
            className="rounded-lg flex items-center justify-center transition-all duration-300"
            style={getRightPanelButtonStyle(false)}
            onMouseEnter={(e) => handleMouseEnter(e, false)}
            onMouseLeave={(e) => handleMouseLeave(e, false)}
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
              <path d="M18 15l-6-6-6 6" />
            </svg>
          </button>

          {/* Scroll to bottom button */}
          <button
            type="button"
            aria-label="Scroll to bottom"
            title="Scroll to bottom"
            onClick={onScrollToBottom}
            className="rounded-lg flex items-center justify-center transition-all duration-300"
            style={getRightPanelButtonStyle(false)}
            onMouseEnter={(e) => handleMouseEnter(e, false)}
            onMouseLeave={(e) => handleMouseLeave(e, false)}
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
              <path d="M6 9l6 6 6-6" />
            </svg>
          </button>
        </>
      )}
    </div>
  );
};


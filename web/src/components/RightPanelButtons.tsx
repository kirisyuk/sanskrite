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
  unfoldSubView?: 'chat' | 'perspectives' | 'models' | 'pages' | 'scenarios'; // Show model-context button only in models subview
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
}) => {
  const { t } = useTranslation();

  const handleButtonClick = (type: RightPanelType) => {
    if (type === 'artifacts') {
      // Special handling for artifacts button
      try {
        const btn = document.getElementById('btn-artifacts');
        if (btn) btn.classList.remove('etg-glow-blue');
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
        style={getRightPanelButtonStyle(rightPanelType === 'workspace')}
        onClick={() => handleButtonClick('workspace')}
        onMouseEnter={(e) => handleMouseEnter(e, rightPanelType === 'workspace')}
        onMouseLeave={(e) => handleMouseLeave(e, rightPanelType === 'workspace')}
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
          >
            <path
              d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z"
              strokeWidth={2}
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            <circle cx="12" cy="10" r="3" strokeWidth={2} />
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
        >
          <path
            d="M9 21h6M12 3a6 6 0 0 1 6 6c0 2.5-1.5 4.5-3 6l-1 1H9l-1-1c-1.5-1.5-3-3.5-3-6a6 6 0 0 1 6-6z"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <path
            d="M9 8h6M9 12h4"
            strokeWidth={2}
            strokeLinecap="round"
          />
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
            <line x1="12" y1="20" x2="12" y2="10" />
            <line x1="18" y1="20" x2="18" y2="4" />
            <line x1="6" y1="20" x2="6" y2="16" />
          </svg>
        </button>
      )}

      {/* Experimental Response button */}
      <button
        type="button"
        aria-label="Experimental Response"
        title="Experimental Response"
        onClick={() => handleButtonClick('experimental')}
        className="rounded-lg flex items-center justify-center transition-all duration-300"
        style={getRightPanelButtonStyle(rightPanelType === 'experimental')}
        onMouseEnter={(e) => handleMouseEnter(e, rightPanelType === 'experimental')}
        onMouseLeave={(e) => handleMouseLeave(e, rightPanelType === 'experimental')}
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
        >
          <path
            d="M9 2v6l-3 3v8a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2v-8l-3-3V2"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <path
            d="M9 2h6M12 10v4"
            strokeWidth={2}
            strokeLinecap="round"
          />
        </svg>
      </button>

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

      {/* Model Context button - shows only in models subview */}
      {unfoldSubView === 'models' && (
        <button
          type="button"
          aria-label="Model Context"
          title="Model Context"
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
    </div>
  );
};


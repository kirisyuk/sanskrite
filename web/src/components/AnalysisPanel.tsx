import React from 'react';
import { useTranslation } from 'react-i18next';
import { UserPositionAnalysis } from './UserPositionAnalysis';
import { DiscussionFlow } from './DiscussionFlow';
import { TextTask } from './TextTasksPanel';

interface AnalysisPanelProps {
  analysisTab: 'user' | 'flow';
  onAnalysisTabChange: (tab: 'user' | 'flow') => void;
  onFirstResultUser: () => void;
  onRiskUpdate: (r: any) => void;
  onFirstResultDiscussionFlow: () => void;
  onAddTextTask: (task: TextTask) => void;
  onRemoveTextTask: (index: number) => void;
  textTasks: TextTask[];
}

export const AnalysisPanel: React.FC<AnalysisPanelProps> = ({
  analysisTab,
  onAnalysisTabChange,
  onFirstResultUser,
  onRiskUpdate,
  onFirstResultDiscussionFlow,
  onAddTextTask,
  onRemoveTextTask,
  textTasks,
}) => {
  const { t } = useTranslation();

  return (
    <>
      {/* Toolbar with tabs */}
      <div
        className="mb-3 flex items-center gap-1"
        role="tablist"
      >
        <button
          type="button"
          role="tab"
          aria-selected={analysisTab === 'user'}
          onClick={() => onAnalysisTabChange('user')}
          className="px-3 py-1.5 text-12 rounded transition-colors"
          style={{
            backgroundColor:
              analysisTab === 'user'
                ? 'var(--accent-primary)'
                : 'transparent',
            color:
              analysisTab === 'user'
                ? 'var(--text-on-accent)'
                : 'var(--text-secondary-strong)',
          }}
          onMouseEnter={(e) => {
            if (analysisTab !== 'user') {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                'var(--white-10)';
            }
          }}
          onMouseLeave={(e) => {
            if (analysisTab !== 'user') {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                'transparent';
            }
          }}
        >
          {t('unfold.sections.howYouEngage', {
            defaultValue: 'User Analysis',
          })}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={analysisTab === 'flow'}
          onClick={() => onAnalysisTabChange('flow')}
          className="px-3 py-1.5 text-12 rounded transition-colors"
          style={{
            backgroundColor:
              analysisTab === 'flow'
                ? 'var(--accent-primary)'
                : 'transparent',
            color:
              analysisTab === 'flow'
                ? 'var(--text-on-accent)'
                : 'var(--text-secondary-strong)',
          }}
          onMouseEnter={(e) => {
            if (analysisTab !== 'flow') {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                'var(--white-10)';
            }
          }}
          onMouseLeave={(e) => {
            if (analysisTab !== 'flow') {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                'transparent';
            }
          }}
        >
          {t('unfold.sections.discussionFlow', {
            defaultValue: 'Discussion Flow',
          })}
        </button>
      </div>

      {/* Content */}
      {analysisTab === 'user' && (
        <UserPositionAnalysis
          hideTitle
          onFirstResult={onFirstResultUser}
          onRiskUpdate={onRiskUpdate}
        />
      )}

      {analysisTab === 'flow' && (
        <DiscussionFlow
          onFirstResult={onFirstResultDiscussionFlow}
          onAddTextTask={onAddTextTask}
          onRemoveTextTask={onRemoveTextTask}
          textTasks={textTasks}
        />
      )}
    </>
  );
};


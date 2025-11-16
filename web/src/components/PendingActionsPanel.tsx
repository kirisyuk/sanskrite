import React from 'react';
import { useTranslation } from 'react-i18next';
import type { QueuedTask } from '../types/queuedTasks';
import { QueuedTasksList } from './QueuedTasksList';

interface PendingActionsPanelProps {
  tasks: QueuedTask[];
  onRemove: (taskId: string) => void;
  onApply: (taskIds: string[]) => void;
  isApplying?: boolean;
}

export const PendingActionsPanel: React.FC<PendingActionsPanelProps> = ({
  tasks,
  onRemove,
  onApply,
  isApplying = false,
}) => {
  const { t } = useTranslation();

  // Group tasks by type for better organization
  const tasksByType = tasks.reduce((acc, task) => {
    if (!acc[task.type]) {
      acc[task.type] = [];
    }
    acc[task.type].push(task);
    return acc;
  }, {} as Record<string, QueuedTask[]>);

  const typeLabels: Record<string, string> = {
    text: t('unfold.discussionFlow.textTasks', { defaultValue: 'Text Tasks' }),
  };

  const unappliedTasks = tasks.filter((t) => !t.applied);
  const hasUnapplied = unappliedTasks.length > 0;

  return (
    <div>
      {tasks.length === 0 ? (
        <div className="text-12 text-center" style={{ color: 'var(--text-secondary-strong)', padding: '20px 0' }}>
          {t('unfold.pendingActions.empty', { defaultValue: 'No pending actions' })}
        </div>
      ) : (
        <div className="space-y-4">
          {/* Apply All button at the top */}
          {hasUnapplied && (
            <div className="flex items-center justify-end">
              <button
                type="button"
                onClick={() => onApply(unappliedTasks.map((t) => t.id))}
                disabled={isApplying}
                className="px-3 py-1.5 rounded text-12 font-medium transition-colors flex items-center gap-2"
                style={{
                  background: isApplying ? 'var(--white-06)' : 'var(--white-10)',
                  color: isApplying ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
                  border: '1px solid var(--overlay-line-90)',
                  cursor: isApplying ? 'not-allowed' : 'pointer',
                  opacity: isApplying ? 0.6 : 1,
                }}
                title={t('unfold.pendingActions.applyAll', { defaultValue: 'Apply all unapplied tasks' })}
                onMouseEnter={(e) => {
                  if (!isApplying) {
                    (e.currentTarget as HTMLButtonElement).style.opacity = '0.85';
                  }
                }}
                onMouseLeave={(e) => {
                  if (!isApplying) {
                    (e.currentTarget as HTMLButtonElement).style.opacity = '1';
                  }
                }}
              >
                {isApplying ? (
                  <>
                    <svg
                      className="w-4 h-4 animate-spin"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                    >
                      <circle cx="12" cy="12" r="9" strokeWidth="2" opacity="0.25" />
                      <path d="M21 12a9 9 0 0 0-9-9" strokeWidth="2" strokeLinecap="round" />
                    </svg>
                    <span>{t('unfold.discussionFlow.applying', { defaultValue: 'Applying...' })}</span>
                  </>
                ) : (
                  <span>{t('unfold.pendingActions.applyAll', { defaultValue: 'Apply All' })}</span>
                )}
              </button>
            </div>
          )}

          {/* Tasks grouped by type */}
          {Object.entries(tasksByType).map(([type, typeTasks]) => (
            <div key={type} className="space-y-2">
              <div className="text-[11px] uppercase tracking-wide" style={{ color: 'var(--text-tertiary-weak)' }}>
                {typeLabels[type] || type}
              </div>
              <QueuedTasksList
                tasks={typeTasks}
                onRemove={onRemove}
                filterType={null} // Already filtered by type
                showOnlyUnapplied={false} // Show all including applied
              />
            </div>
          ))}
        </div>
      )}
    </div>
  );
};


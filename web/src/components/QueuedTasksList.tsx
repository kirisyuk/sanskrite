import React from 'react';
import { useTranslation } from 'react-i18next';
import type { QueuedTask } from '../types/queuedTasks';

interface QueuedTasksListProps {
  tasks: QueuedTask[];
  onRemove?: (taskId: string) => void;
  onApply?: (taskIds: string[]) => void;
  isApplying?: boolean;
  showApplyButton?: boolean;
  filterType?: QueuedTask['type'] | null; // Filter by type, null = show all
  showOnlyUnapplied?: boolean; // Show only unapplied tasks
}

export const QueuedTasksList: React.FC<QueuedTasksListProps> = ({
  tasks,
  onRemove,
  onApply,
  isApplying = false,
  showApplyButton = false,
  filterType = null,
  showOnlyUnapplied = false,
}) => {
  const { t } = useTranslation();

  // Filter tasks
  const filteredTasks = tasks.filter((task) => {
    if (filterType !== null && task.type !== filterType) return false;
    if (showOnlyUnapplied && task.applied) return false;
    return true;
  });

  if (filteredTasks.length === 0) return null;

  const unappliedTasks = filteredTasks.filter((t) => !t.applied);
  const hasUnapplied = unappliedTasks.length > 0;

  return (
    <div className="space-y-2">
      {showApplyButton && hasUnapplied && onApply && (
        <div className="flex items-center justify-end mb-2">
          <button
            type="button"
            onClick={() => onApply(unappliedTasks.map((t) => t.id))}
            disabled={isApplying}
            className="px-2 py-0.5 rounded text-[11px] font-medium transition-colors shrink-0 flex items-center gap-1"
            style={{
              background: isApplying ? 'var(--white-06)' : 'var(--white-10)',
              color: isApplying ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
              border: '1px solid var(--overlay-line-90)',
              cursor: isApplying ? 'not-allowed' : 'pointer',
              opacity: isApplying ? 0.6 : 1,
            }}
            title={t('unfold.discussionFlow.applyTasks', { defaultValue: 'Apply all tasks to chat' })}
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
                  className="w-3 h-3 animate-spin"
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
              <span>{t('unfold.discussionFlow.applyTasks', { defaultValue: 'Apply All' })}</span>
            )}
          </button>
        </div>
      )}
      <ul className="space-y-1.5" style={{ listStyle: 'none', padding: 0, margin: 0 }}>
        {filteredTasks.map((task, idx) => {
          const isApplied = task.applied || false;
          return (
            <li key={task.id} className="flex items-center gap-2">
              <span
                className="text-12 shrink-0"
                style={{
                  color: 'var(--text-secondary-strong)',
                  width: '16px',
                  textDecoration: isApplied ? 'line-through' : 'none',
                  opacity: isApplied ? 0.6 : 1,
                }}
              >
                {idx + 1}.
              </span>
              <span
                className="text-12 flex-1"
                style={{
                  color: isApplied ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
                  lineHeight: '1.5',
                  textDecoration: isApplied ? 'line-through' : 'none',
                  opacity: isApplied ? 0.6 : 1,
                }}
              >
                {task.prompt}
              </span>
              {onRemove && (
                <button
                  type="button"
                  onClick={() => onRemove(task.id)}
                  className="p-1 rounded shrink-0"
                  style={{
                    background: 'transparent',
                    color: 'var(--text-secondary-strong)',
                    cursor: 'pointer',
                    border: 'none',
                    opacity: isApplied ? 0.5 : 1,
                  }}
                  title={t('unfold.discussionFlow.removeTask', { defaultValue: 'Remove task' })}
                  onMouseEnter={(e) => {
                    if (!isApplied) {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                    }
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                  }}
                >
                  <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <line x1="18" y1="6" x2="6" y2="18" />
                    <line x1="6" y1="6" x2="18" y2="18" />
                  </svg>
                </button>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
};


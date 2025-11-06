import React from 'react';
import { useTranslation } from 'react-i18next';

export interface TextTask {
  id: string;
  title: string;
  prompt: string;
}

interface TextTasksPanelProps {
  tasks: TextTask[];
  onRemove: (taskId: string) => void;
  onApply: () => void;
  isApplying?: boolean;
}

export const TextTasksPanel: React.FC<TextTasksPanelProps> = ({ tasks, onRemove, onApply, isApplying = false }) => {
  const { t } = useTranslation();

  if (tasks.length === 0) return null;

  return (
    <div className="mb-3 rounded-lg border p-3" style={{ backgroundColor: 'var(--bg-sidebar)', borderColor: 'var(--overlay-line-90)' }}>
      <div className="flex items-center justify-between mb-2">
        <div className="text-[11px] uppercase tracking-wide" style={{ color: 'var(--text-tertiary-weak)' }}>
          {t('unfold.discussionFlow.textTasks', { defaultValue: 'Text Tasks' })}
        </div>
        <button
          type="button"
          onClick={onApply}
          disabled={isApplying}
          className="px-2 py-0.5 rounded text-[11px] font-medium transition-colors shrink-0 flex items-center gap-1"
          style={{
            background: isApplying ? 'var(--white-06)' : 'var(--white-10)',
            color: isApplying ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
            border: '1px solid var(--overlay-line-90)',
            cursor: isApplying ? 'not-allowed' : 'pointer',
            opacity: isApplying ? 0.6 : 1
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
      <ul className="space-y-1.5" style={{ listStyle: 'none', padding: 0, margin: 0 }}>
        {tasks.map((task, idx) => (
          <li key={task.id} className="flex items-center gap-2">
            <span className="text-12 shrink-0" style={{ color: 'var(--text-secondary-strong)', width: '16px' }}>
              {idx + 1}.
            </span>
            <span className="text-12 flex-1" style={{ color: 'var(--text-heading)', lineHeight: '1.5' }}>
              {task.prompt}
            </span>
            <button
              type="button"
              onClick={() => onRemove(task.id)}
              className="p-1 rounded shrink-0"
              style={{
                background: 'transparent',
                color: 'var(--text-secondary-strong)',
                cursor: 'pointer',
                border: 'none'
              }}
              title={t('unfold.discussionFlow.removeTask', { defaultValue: 'Remove task' })}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
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
          </li>
        ))}
      </ul>
    </div>
  );
};


import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

interface BringInIndicatorProps {
  interaction: any;
}

export const BringInIndicator: React.FC<BringInIndicatorProps> = ({
  interaction,
}) => {
  const genaiChat = useGenaiChatStore();

  const handleDelete = async () => {
    if (window.confirm('Delete this indicator message?')) {
      try {
        await genaiChat.deleteInteraction(interaction.id);
      } catch (e) {
        alert('Failed to delete message. Please try again.');
        console.error('[Delete] Error:', e);
      }
    }
  };

  return (
    <div
      key={interaction.id}
      className="my-1.5 flex justify-end group relative"
    >
      {/* Delete button - appears on hover */}
      <button
        type="button"
        aria-label="Delete indicator message"
        className="absolute top-2 right-2 p-1.5 rounded opacity-0 group-hover:opacity-100 transition-opacity z-10"
        style={{ backgroundColor: 'transparent' }}
        onClick={handleDelete}
        onMouseEnter={(e) => {
          (e.currentTarget as HTMLButtonElement).style.backgroundColor =
            'var(--white-10)';
        }}
        onMouseLeave={(e) => {
          (e.currentTarget as HTMLButtonElement).style.backgroundColor =
            'transparent';
        }}
      >
        <svg
          className="w-3.5 h-3.5"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          style={{ color: 'var(--text-secondary-strong)' }}
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
          />
        </svg>
      </button>
      <div
        className="px-3 py-1.5 rounded-lg text-[12px] inline-flex items-center gap-2"
        style={{
          background: 'var(--accent-research-10)',
          color: 'var(--accent-research)',
          border: '1px solid var(--accent-research-20)',
          maxWidth: '80%',
        }}
      >
        <svg
          className="w-3.5 h-3.5 flex-shrink-0"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
        >
          <path
            d="M12 7a3 3 0 1 1 0 6a3 3 0 0 1 0-6"
            strokeWidth="2"
          />
          <path
            d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6"
            strokeWidth="2"
            strokeLinecap="round"
          />
          <path
            d="M18 7v4M16 9h4"
            strokeWidth="2"
            strokeLinecap="round"
          />
        </svg>
        <span>{interaction.content}</span>
      </div>
    </div>
  );
};


import React from 'react';
import { toggleInteractionPin, isInteractionPinned } from '../lib/interactionHelpers';
import { useGenaiChatStore } from '../stores/genaiChatStore';

interface MessageActionsProps {
  interactionId: string;
  isUser: boolean;
  interaction: any;
}

export const MessageActions: React.FC<MessageActionsProps> = ({
  interactionId,
  isUser,
  interaction,
}) => {
  const genaiChat = useGenaiChatStore();
  const isPinned = !isUser && isInteractionPinned(interaction);

  const handlePinToggle = async () => {
    if (isUser) return; // Only for assistant messages
    try {
      await toggleInteractionPin(
        genaiChat.projectId,
        interactionId,
        isPinned,
      );
      // Refresh interactions to update pin state
      await genaiChat.hydrateInteractions();
    } catch (e) {
      alert('Failed to toggle pin. Please try again.');
      console.error('[Pin] Error:', e);
    }
  };

  const handleDelete = async () => {
    const targetName = isUser ? 'user message' : 'assistant response';
    if (window.confirm(`Delete this ${targetName}?`)) {
      try {
        await genaiChat.deleteInteraction(interactionId);
      } catch (e) {
        alert('Failed to delete message. Please try again.');
        console.error('[Delete] Error:', e);
      }
    }
  };

  return (
    <>
      {/* Pin button - appears on hover for assistant messages only */}
      {!isUser && (
        <button
          type="button"
          aria-label={isPinned ? 'Unpin message' : 'Pin message'}
          className="absolute top-2 right-10 p-1.5 rounded opacity-0 group-hover:opacity-100 transition-opacity z-10"
          style={{ backgroundColor: 'transparent' }}
          onClick={handlePinToggle}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
        >
          {isPinned ? (
            <svg
              className="w-3.5 h-3.5"
              viewBox="0 0 24 24"
              fill="currentColor"
              style={{ color: 'var(--accent-research)' }}
            >
              <path d="M5 5a2 2 0 012-2h10a2 2 0 012 2v16l-7-3.5L5 21V5z" />
            </svg>
          ) : (
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
                d="M5 5a2 2 0 012-2h10a2 2 0 012 2v16l-7-3.5L5 21V5z"
              />
            </svg>
          )}
        </button>
      )}

      {/* Delete button - appears on hover */}
      <button
        type="button"
        aria-label="Delete message"
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
    </>
  );
};


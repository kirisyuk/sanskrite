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
  const [isDeleteHovered, setIsDeleteHovered] = React.useState(false);
  const [isPinning, setIsPinning] = React.useState(false);
  const [optimisticPinned, setOptimisticPinned] = React.useState<boolean | null>(null);
  const containerRef = React.useRef<HTMLDivElement | null>(null);
  
  // Recalculate isPinned whenever interaction changes
  const isPinned = React.useMemo(() => {
    return !isUser && isInteractionPinned(interaction);
  }, [isUser, interaction?.metadata?.pinned]);
  
  // Use optimistic state if available, otherwise use actual state
  const displayPinned = optimisticPinned !== null ? optimisticPinned : isPinned;

  // Update data attribute on parent container when delete button is hovered
  React.useEffect(() => {
    const container = containerRef.current?.closest('.group');
    if (container) {
      if (isDeleteHovered && !isUser) {
        container.setAttribute('data-delete-hovered', 'true');
      } else {
        container.removeAttribute('data-delete-hovered');
      }
    }
  }, [isDeleteHovered, isUser]);

  const handlePinToggle = async () => {
    if (isUser || isPinning) return; // Only for assistant messages, prevent double-click
    setIsPinning(true);
    const newPinnedState = !isPinned;
    // Optimistic update: show new state immediately
    setOptimisticPinned(newPinnedState);
    
    // Send request in background, don't wait for it
    toggleInteractionPin(
      genaiChat.projectId,
      interactionId,
      isPinned,
    ).then(() => {
      setIsPinning(false);
      // Don't clear optimistic state immediately - let it persist until store updates
      // The store will be updated via WebSocket or next hydrateInteractions
      // We'll clear optimistic state when real state matches (via useEffect below)
    }).catch((e) => {
      // Revert optimistic update on error
      setOptimisticPinned(null);
      setIsPinning(false);
      alert('Failed to toggle pin. Please try again.');
      console.error('[Pin] Error:', e);
    });
  };

  // Clear optimistic state when real state matches optimistic state
  React.useEffect(() => {
    if (optimisticPinned !== null && optimisticPinned === isPinned) {
      // Real state now matches optimistic state, clear optimistic
      setOptimisticPinned(null);
    }
  }, [optimisticPinned, isPinned]);

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
    <div ref={containerRef}>
      {/* Pin button - appears on hover for assistant messages only */}
      {!isUser && (
        <button
          type="button"
          aria-label={displayPinned ? 'Unpin message' : 'Pin message'}
          disabled={isPinning}
          className={`absolute top-2 right-10 p-1.5 rounded transition-opacity z-10 disabled:cursor-wait ${
            isPinning ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'
          }`}
          style={{ backgroundColor: 'transparent' }}
          onClick={handlePinToggle}
          onMouseEnter={(e) => {
            if (!isPinning) {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                'var(--white-10)';
            }
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
        >
          {isPinning ? (
            <svg
              className="w-3.5 h-3.5 animate-spin"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              style={{ color: 'var(--accent-research)' }}
            >
              <circle
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                strokeWidth="2"
                strokeDasharray="32"
                strokeDashoffset="24"
                strokeLinecap="round"
                opacity="0.3"
              />
            </svg>
          ) : displayPinned ? (
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
          setIsDeleteHovered(true);
          (e.currentTarget as HTMLButtonElement).style.backgroundColor =
            'var(--white-10)';
        }}
        onMouseLeave={(e) => {
          setIsDeleteHovered(false);
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
    </div>
  );
};


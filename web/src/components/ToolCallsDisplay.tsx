import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

export const ToolCallsDisplay: React.FC = () => {
  const genaiChat = useGenaiChatStore();
  
  if (genaiChat.toolCalls.length === 0 && genaiChat.toolResults.length === 0) {
    return null;
  }

  return (
    <div className="px-1 pb-2">
      {genaiChat.toolCalls.length > 0 && (
        <div
          className="text-[11px] mb-1"
          style={{ color: 'var(--text-secondary-strong)' }}
        >
          Tool calls:
          <ul className="list-disc ml-4">
            {genaiChat.toolCalls.map((c, i) => (
              <li key={`tc-${i}`}>{c.name}</li>
            ))}
          </ul>
        </div>
      )}
      {genaiChat.toolResults.length > 0 && (
        <div
          className="text-[11px]"
          style={{ color: 'var(--text-secondary-strong)' }}
        >
          Tool results:
          <ul className="list-disc ml-4">
            {genaiChat.toolResults.map((c, i) => (
              <li key={`tr-${i}`}>
                <span className="font-mono">{c.name}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};


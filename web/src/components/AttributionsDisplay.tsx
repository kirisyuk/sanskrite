import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

export const AttributionsDisplay: React.FC = () => {
  const genaiChat = useGenaiChatStore();
  
  if (genaiChat.streaming || genaiChat.attributions.length === 0) {
    return null;
  }

  return (
    <div className="px-1 pb-2 space-y-1">
      <div
        className="text-[11px]"
        style={{ color: 'var(--text-secondary-strong)' }}
      >
        Sources:
        <ul className="list-disc ml-4">
          {genaiChat.attributions.map((a, i) => (
            <li key={`att-${i}`}>
              <a
                href={a.url}
                target="_blank"
                rel="noreferrer"
                className="underline"
              >
                {a.title || a.url}
              </a>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
};


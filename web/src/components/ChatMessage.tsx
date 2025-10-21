import React from 'react';

interface ChatMessageProps {
  role: 'user' | 'assistant';
  children: React.ReactNode;
  footer?: React.ReactNode;
  align?: 'left' | 'right';
  headerText?: string;
  thinkingSeconds?: number;
  isThinking?: boolean;
  isError?: boolean;
  bgOverride?: string;
  leftAccent?: boolean;
}

const ChatMessage: React.FC<ChatMessageProps> = ({
  role,
  children,
  footer,
  align = 'left',
  headerText,
  thinkingSeconds,
  isThinking,
  isError,
  bgOverride,
  leftAccent,
}) => {
  const header = headerText || (role === 'user' ? 'you' : 'assistant');
  const containerStyle: React.CSSProperties = {
    maxWidth: align === 'right' ? '75%' : '100%',
    width: align === 'right' ? 'fit-content' : '100%',
    // For user messages use theme-aware adjusted background
    backgroundColor: bgOverride ? bgOverride : (role === 'user' ? 'var(--user-msg-bg)' : 'transparent'),
    border: 'none',
    borderLeft: leftAccent ? '2px solid var(--overlay-line-90)' : 'none',
  };
  // Transparent, no borders; color handled by parent theme
  return (
    <div className={`rounded-2xl px-4 py-3 ${align === 'right' ? 'ml-auto' : ''}`} style={containerStyle}>
      <div className="text-[10px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
        {isThinking && role === 'assistant' ? `${header} (thinking ${thinkingSeconds ?? 0} sec)` : header}
      </div>
      <div className="text-13" style={{ color: 'var(--text-heading)' }}>
        {isError ? (
          <div>
            К сожалению, не удалось обработать запрос. Мы зафиксировали техническую неполадку и уже анализируем ситуацию. Пожалуйста, повторите попытку чуть позже.
          </div>
        ) : (
          children
        )}
      </div>
      {footer && (
        <div className="mt-1 flex justify-end">
          {footer}
        </div>
      )}
    </div>
  );
};

export default ChatMessage;



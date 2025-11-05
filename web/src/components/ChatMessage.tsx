import React from 'react';
import { useTranslation } from 'react-i18next';

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
  averageTimingSeconds?: number | null; // Average response time estimate in seconds
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
  averageTimingSeconds,
}) => {
  const { t } = useTranslation();
  const header = headerText || (role === 'user' ? t('chatMessage.userRole', { defaultValue: 'you' }) : t('chatMessage.assistantRole', { defaultValue: 'assistant' }));
  const containerStyle: React.CSSProperties = {
    maxWidth: align === 'right' ? '75%' : '100%',
    width: align === 'right' ? 'fit-content' : '100%',
    // For user messages use theme-aware adjusted background
    backgroundColor: bgOverride ? bgOverride : (role === 'user' ? 'var(--user-msg-bg)' : 'transparent'),
    border: 'none',
    borderLeft: leftAccent ? '2px solid var(--overlay-line-90)' : 'none',
  };
  
  // Build header text with timing info
  let headerTextContent = header;
  if (isThinking && role === 'assistant') {
    const thinkingText = t('chatMessage.thinking', { seconds: thinkingSeconds ?? 0, defaultValue: `thinking ${thinkingSeconds ?? 0} sec` });
    headerTextContent = `${header} (${thinkingText})`;
    if (averageTimingSeconds !== null && averageTimingSeconds !== undefined) {
      const averageText = t('chatMessage.averageResponseTime', { seconds: averageTimingSeconds, defaultValue: `average response time ${averageTimingSeconds} sec` });
      headerTextContent += ` • ${averageText}`;
    }
  }
  
  // Transparent, no borders; color handled by parent theme
  return (
    <div className={`rounded-2xl px-4 py-3 ${align === 'right' ? 'ml-auto' : ''}`} style={containerStyle}>
      <div className="text-[10px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
        {headerTextContent}
      </div>
      <div className="text-13" style={{ color: 'var(--text-heading)' }}>
        {isError ? (
          <div>
            {t('chatMessage.errorMessage', { 
              defaultValue: 'К сожалению, не удалось обработать запрос. Мы зафиксировали техническую неполадку и уже анализируем ситуацию. Пожалуйста, повторите попытку чуть позже.' 
            })}
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



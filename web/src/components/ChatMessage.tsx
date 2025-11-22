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
  errorMessage?: string | null;
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
  errorMessage,
  bgOverride,
  leftAccent,
  averageTimingSeconds,
}) => {
  const { t } = useTranslation();
  const messageRef = React.useRef<HTMLDivElement | null>(null);
  const [isHighlighted, setIsHighlighted] = React.useState(false);

  // Check parent element for data-delete-hovered attribute
  React.useEffect(() => {
    if (role !== 'assistant') return;
    
    const checkParent = () => {
      const parent = messageRef.current?.closest('.group');
      if (parent) {
        const hasAttribute = parent.hasAttribute('data-delete-hovered');
        setIsHighlighted(hasAttribute);
      }
    };

    // Check initially
    checkParent();

    // Use MutationObserver to watch for attribute changes
    const parent = messageRef.current?.closest('.group');
    if (parent) {
      const observer = new MutationObserver(checkParent);
      observer.observe(parent, {
        attributes: true,
        attributeFilter: ['data-delete-hovered'],
      });
      return () => observer.disconnect();
    }
  }, [role]);
  
  // Icon components for roles
  const UserIcon = () => (
    <div className="flex items-center justify-center rounded-full" style={{ 
      width: 20, 
      height: 20, 
      backgroundColor: 'var(--text-tertiary-weak)',
      color: 'var(--bg-body)',
      flexShrink: 0
    }}>
      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="8" r="3"/>
        <path d="M4 20c0-3.314 3.134-6 7-6s7 2.686 7 6"/>
      </svg>
    </div>
  );

  const RobotIcon = () => (
    <div className="flex items-center justify-center rounded-full" style={{ 
      width: 20, 
      height: 20, 
      backgroundColor: 'var(--text-tertiary-weak)',
      color: 'var(--bg-body)',
      flexShrink: 0
    }}>
      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <rect x="5" y="2" width="14" height="20" rx="2"/>
        <circle cx="9" cy="9" r="1.5" fill="currentColor"/>
        <circle cx="15" cy="9" r="1.5" fill="currentColor"/>
        <path d="M9 14h6M9 17h6"/>
      </svg>
    </div>
  );

  const header = headerText || (role === 'user' ? t('chatMessage.userRole', { defaultValue: 'you' }) : t('chatMessage.assistantRole', { defaultValue: 'assistant' }));
  const containerStyle: React.CSSProperties = {
    maxWidth: align === 'right' ? '75%' : '100%',
    width: align === 'right' ? 'fit-content' : '100%',
    // For user messages use theme-aware adjusted background
    backgroundColor: bgOverride 
      ? bgOverride 
      : (role === 'user' 
          ? 'var(--user-msg-bg)' 
          : (isHighlighted ? 'var(--white-10)' : 'transparent')),
    border: 'none',
    borderLeft: leftAccent ? '2px solid var(--overlay-line-90)' : 'none',
    boxSizing: 'border-box', // Ensure border-left doesn't add to width
    minWidth: 0, // Allow flex items to shrink below content size
    // Add smooth transition for highlight effect
    transition: role === 'assistant' ? 'background-color 0.2s ease' : undefined,
  };
  
  // Build header with icon and optional timing info
  let headerContent: React.ReactNode = (
    <div className="flex items-center gap-2">
      {role === 'user' ? <UserIcon /> : <RobotIcon />}
      {headerText && (
        <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
          {headerText}
        </span>
      )}
    </div>
  );

  if (isThinking && role === 'assistant') {
    const thinkingText = t('chatMessage.thinking', { seconds: thinkingSeconds ?? 0, defaultValue: `thinking ${thinkingSeconds ?? 0} sec` });
    headerContent = (
      <div className="flex items-center gap-2">
        <RobotIcon />
        {headerText ? (
          <>
            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              {headerText}
            </span>
            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>•</span>
            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              {thinkingText}
            </span>
          </>
        ) : (
          <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
            {thinkingText}
          </span>
        )}
        {averageTimingSeconds !== null && averageTimingSeconds !== undefined && (
          <>
            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>•</span>
            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              {t('chatMessage.averageResponseTime', { seconds: averageTimingSeconds, defaultValue: `average response time ${averageTimingSeconds} sec` })}
            </span>
          </>
        )}
      </div>
    );
  }
  
  // Transparent, no borders; color handled by parent theme
  return (
    <div ref={messageRef} className={`rounded-2xl px-4 py-3 ${align === 'right' ? 'ml-auto' : ''}`} style={containerStyle}>
      <div className="mb-1">
        {headerContent}
      </div>
      <div className="text-13" style={{ color: 'var(--text-heading)' }}>
        {isError ? (
          <div style={{ color: 'var(--accent-danger)' }}>
            {errorMessage || t('chatMessage.errorMessage', { 
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



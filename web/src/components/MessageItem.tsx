import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import ChatMessage from './ChatMessage';
import { CollapsibleText } from './CollapsibleText';
import { ActionRow } from './ActionRow';
import { EvaluationDetails } from './EvaluationDetails';
import { markdownComponents } from '../lib/markdownComponents';

interface MessageItemProps {
  message: {
    id: string;
    role: 'user' | 'assistant';
    content: string;
  };
  genaiChat: {
    streaming: boolean;
    error: any;
    messageMeta?: Record<string, { headerText?: string; leftAccent?: boolean }>;
    preStanceLatency?: Record<string, number>;
    evaluations?: Record<string, any>;
  };
  genaiThinkingSeconds: number;
  averageTimingSeconds: number | null;
  genaiSettings: {
    strength?: string;
    model?: string;
    groundingEnabled?: boolean;
  };
  expandedChip: string | null;
  onExpandChip: (chipId: string | null) => void;
}

/**
 * Мемоизированный компонент для рендеринга отдельного сообщения
 * Оптимизирован для предотвращения ненужных ре-рендеров
 */
export const MessageItem = React.memo<MessageItemProps>(({
  message: m,
  genaiChat,
  genaiThinkingSeconds,
  averageTimingSeconds,
  genaiSettings,
  expandedChip,
  onExpandChip,
}) => {
  const { t } = useTranslation();
  const isUser = m.role === 'user';
  
  // Вычисляем состояние сообщения
  const isAssistantPlaceholder = useMemo(
    () => !isUser && genaiChat.streaming && !m.content,
    [isUser, genaiChat.streaming, m.content]
  );
  
  const isAssistantError = useMemo(
    () => !isUser && !genaiChat.streaming && !m.content && !!genaiChat.error,
    [isUser, genaiChat.streaming, m.content, genaiChat.error]
  );

  // Получаем contentSanitized для assistant сообщений
  const assistantContent = useMemo(() => {
    if (isUser) return m.content;
    
    try {
      const store = useGenaiChatStore.getState();
      const inter = store.interactions.find(
        (it) =>
          it.kind === 'assistant' &&
          (it as any).originalMessageId === m.id
      );
      if (
        inter &&
        typeof inter.contentSanitized === 'string' &&
        inter.contentSanitized.length > 0
      ) {
        return inter.contentSanitized;
      }
      return m.content;
    } catch {
      return m.content;
    }
  }, [isUser, m.id, m.content]);

  // Получаем метаданные для тегов/chips
  const messageTags = useMemo(() => {
    if (isUser) return null;
    
    try {
      const store = useGenaiChatStore.getState() as any;
      let inter = store.interactions.find(
        (it: any) =>
          it.kind === 'assistant' &&
          (it as any).originalMessageId === m.id
      ) as any;
      
      if (!inter) {
        const map: Record<string, string> = store.persistedByLocalId || {};
        const serverId = map[m.id];
        if (serverId) {
          inter = store.interactions.find((it: any) => it.id === serverId);
        }
      }
      
      const perQ = Array.isArray(inter?.searchQueries)
        ? inter.searchQueries.filter(Boolean)
        : [];
      const perS = Array.isArray(inter?.sources)
        ? inter.sources.filter(Boolean)
        : [];
      
      const qs = perQ.slice(0, 4);
      const attrs =
        perS.length > 0
          ? perS.map((url: string) => ({ url })).slice(0, 4)
          : [];
      
      if (qs.length === 0 && attrs.length === 0) return null;
      
      return { queries: qs, sources: attrs };
    } catch {
      return null;
    }
  }, [isUser, m.id]);

  if (isAssistantPlaceholder) {
    return null;
  }

  const headerText = !isUser
    ? genaiChat.messageMeta?.[m.id]?.headerText || 'assistant'
    : undefined;
  
  const leftAccent = !isUser
    ? !!genaiChat.messageMeta?.[m.id]?.leftAccent
    : false;

  const footer = isUser &&
    genaiChat.preStanceLatency &&
    genaiChat.preStanceLatency[m.id] ? (
      <span
        className="text-[10px]"
        style={{
          color: 'var(--text-tertiary-weak)',
        }}
      >
        stance:{' '}
        {(genaiChat.preStanceLatency[m.id] / 1000).toFixed(2)}s
      </span>
    ) : undefined;

  return (
    <ChatMessage
      key={m.id}
      role={isUser ? 'user' : 'assistant'}
      align={isUser ? 'right' : 'left'}
      isThinking={isAssistantPlaceholder}
      thinkingSeconds={genaiThinkingSeconds}
      averageTimingSeconds={averageTimingSeconds}
      isError={isAssistantError}
      headerText={headerText}
      leftAccent={leftAccent}
      footer={footer}
    >
      {isAssistantPlaceholder ? (
        <ThinkingPlaceholder
          genaiSettings={genaiSettings}
          genaiThinkingSeconds={genaiThinkingSeconds}
          t={t}
        />
      ) : (
        <>
          {isUser ? (
            <CollapsibleText maxLines={7}>
              <div
                className="text-13 markdown"
                style={{
                  color: 'var(--text-heading)',
                }}
              >
                <ReactMarkdown
                  remarkPlugins={[remarkGfm]}
                  components={markdownComponents}
                >
                  {m.content}
                </ReactMarkdown>
              </div>
            </CollapsibleText>
          ) : (
            <div
              className="text-13 markdown"
              style={{
                color: 'var(--text-heading)',
              }}
            >
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={markdownComponents}
              >
                {assistantContent}
              </ReactMarkdown>
            </div>
          )}
          
          {/* Action row: shows bias if available, else only buttons (assistant only) */}
          {!isUser && (
            <ActionRow
              messageId={m.id}
              isAssistant={!isUser}
              content={m.content}
            />
          )}
          
          {/* Show bias for user messages if available */}
          {isUser && genaiChat.evaluations?.[m.id] && (
            <EvaluationDetails messageId={m.id} />
          )}
          
          {/* Tags row: per-message chips */}
          {messageTags && (
            <MessageTags
              messageId={m.id}
              queries={messageTags.queries}
              sources={messageTags.sources}
              expandedChip={expandedChip}
              onExpandChip={onExpandChip}
            />
          )}
        </>
      )}
    </ChatMessage>
  );
}, (prevProps, nextProps) => {
  // Кастомная функция сравнения для оптимизации
  if (prevProps.message.id !== nextProps.message.id) return false;
  if (prevProps.message.content !== nextProps.message.content) return false;
  if (prevProps.message.role !== nextProps.message.role) return false;
  if (prevProps.genaiThinkingSeconds !== nextProps.genaiThinkingSeconds) return false;
  if (prevProps.averageTimingSeconds !== nextProps.averageTimingSeconds) return false;
  if (prevProps.expandedChip !== nextProps.expandedChip) return false;
  
  // Проверяем изменения в genaiChat
  if (prevProps.genaiChat.streaming !== nextProps.genaiChat.streaming) return false;
  if (prevProps.genaiChat.error !== nextProps.genaiChat.error) return false;
  if (prevProps.genaiChat.messageMeta?.[prevProps.message.id]?.headerText !== 
      nextProps.genaiChat.messageMeta?.[nextProps.message.id]?.headerText) return false;
  if (prevProps.genaiChat.messageMeta?.[prevProps.message.id]?.leftAccent !== 
      nextProps.genaiChat.messageMeta?.[nextProps.message.id]?.leftAccent) return false;
  if (prevProps.genaiChat.preStanceLatency?.[prevProps.message.id] !== 
      nextProps.genaiChat.preStanceLatency?.[nextProps.message.id]) return false;
  if (prevProps.genaiChat.evaluations?.[prevProps.message.id] !== 
      nextProps.genaiChat.evaluations?.[nextProps.message.id]) return false;
  
  // Проверяем изменения в genaiSettings
  if (prevProps.genaiSettings.strength !== nextProps.genaiSettings.strength) return false;
  if (prevProps.genaiSettings.model !== nextProps.genaiSettings.model) return false;
  if (prevProps.genaiSettings.groundingEnabled !== nextProps.genaiSettings.groundingEnabled) return false;
  
  return true; // Пропсы не изменились, пропускаем ре-рендер
});

MessageItem.displayName = 'MessageItem';

// Компонент для placeholder состояния "thinking"
const ThinkingPlaceholder = React.memo<{
  genaiSettings: {
    strength?: string;
    model?: string;
    groundingEnabled?: boolean;
  };
  genaiThinkingSeconds: number;
  t: (key: string, options?: any) => string;
}>(({ genaiSettings, genaiThinkingSeconds, t }) => {
  const isQuality =
    genaiSettings.strength === 'quality' ||
    /_quality$/.test(genaiSettings.model || '');
  
  const threshold = genaiSettings.groundingEnabled ? 35 : 20;
  const showComplexReasoning = isQuality && (genaiThinkingSeconds || 0) >= threshold;
  
  return (
    <div className="space-y-1">
      <div
        style={{
          width: 160,
          height: 10,
          borderRadius: 4,
          background: 'var(--white-06)',
          position: 'relative',
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            position: 'absolute',
            inset: 0,
            background:
              'linear-gradient(90deg, transparent, var(--white-20), transparent)',
            animation: 'etg-shimmer 2s infinite',
          }}
        />
      </div>
      <div
        style={{
          width: 220,
          height: 10,
          borderRadius: 4,
          background: 'var(--white-06)',
          position: 'relative',
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            position: 'absolute',
            inset: 0,
            background:
              'linear-gradient(90deg, transparent, var(--white-20), transparent)',
            animation: 'etg-shimmer 2s infinite',
          }}
        />
      </div>
      {showComplexReasoning && (
        <div
          className="text-[11px]"
          style={{
            color: 'var(--text-secondary-strong)',
          }}
        >
          {genaiSettings.groundingEnabled
            ? t('unfold.status.modelUpdatingAnalysis')
            : t('unfold.status.modelComplexReasoning')}
        </div>
      )}
    </div>
  );
});

ThinkingPlaceholder.displayName = 'ThinkingPlaceholder';

// Компонент для тегов/chips сообщения
const MessageTags = React.memo<{
  messageId: string;
  queries: string[];
  sources: Array<{ url: string }>;
  expandedChip: string | null;
  onExpandChip: (chipId: string | null) => void;
}>(({ messageId, queries, sources, expandedChip, onExpandChip }) => {
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {queries.map((q: string, i: number) => {
        const chipId = `${messageId}-q-${i}`;
        const isExpanded = expandedChip === chipId;
        const words = String(q || '').trim().split(/\s+/);
        const short =
          words.length > 3
            ? words.slice(0, 3).join(' ') + '…'
            : String(q || '');
        const needsExpand = words.length > 3;
        
        return (
          <button
            key={`q-${i}`}
            type="button"
            className="px-2 py-0.5 rounded-full text-[10px] transition-colors"
            style={{
              backgroundColor: isExpanded
                ? 'var(--white-20)'
                : 'var(--white-10)',
              color: 'var(--text-secondary-strong)',
              border: 'none',
              cursor: needsExpand ? 'pointer' : 'default',
            }}
            onClick={() =>
              needsExpand && onExpandChip(isExpanded ? null : chipId)
            }
            onMouseEnter={(e) => {
              if (needsExpand)
                (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                  isExpanded ? 'var(--white-25)' : 'var(--white-15)';
            }}
            onMouseLeave={(e) => {
              if (needsExpand)
                (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                  isExpanded ? 'var(--white-20)' : 'var(--white-10)';
            }}
          >
            {isExpanded ? String(q || '') : short}
          </button>
        );
      })}
      {sources.map((a: any, i: number) => {
        const chipId = `${messageId}-src-${i}`;
        const isExpanded = expandedChip === chipId;
        const raw = String((a as any).url || '').replace(/^https?:\/\//i, '');
        const parts = raw.split('/');
        const host = parts[0] || '';
        const tail = parts[1] ? `/${parts[1]}` : '';
        const label = host + tail || (a as any).title || 'link';
        const needsExpand = raw.length > (host + tail).length;
        
        return (
          <a
            key={`src-${i}`}
            href={(a as any).url}
            target="_blank"
            rel="noreferrer"
            className="px-2 py-0.5 rounded-full text-[10px] underline transition-colors"
            style={{
              backgroundColor: isExpanded
                ? 'var(--white-20)'
                : 'var(--white-10)',
              color: 'var(--text-secondary-strong)',
            }}
            onClick={(e) => {
              if (needsExpand) {
                e.preventDefault();
                onExpandChip(isExpanded ? null : chipId);
              }
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLAnchorElement).style.backgroundColor =
                isExpanded ? 'var(--white-25)' : 'var(--white-15)';
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLAnchorElement).style.backgroundColor =
                isExpanded ? 'var(--white-20)' : 'var(--white-10)';
            }}
          >
            {isExpanded ? raw : label}
          </a>
        );
      })}
    </div>
  );
});

MessageTags.displayName = 'MessageTags';


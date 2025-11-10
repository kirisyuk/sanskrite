import React from 'react';
import ChatMessage from './ChatMessage';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { useTranslation } from 'react-i18next';

interface ThinkingPlaceholderProps {
  messageId: string;
  genaiThinkingSeconds: number;
  averageTimingSeconds: number | null;
}

export const ThinkingPlaceholder: React.FC<ThinkingPlaceholderProps> = ({
  messageId,
  genaiThinkingSeconds,
  averageTimingSeconds,
}) => {
  const { t } = useTranslation();
  const genaiChat = useGenaiChatStore();
  const genaiSettings = useGenAISettings();

  // Don't show placeholder if this message is already in interactions (streaming completed)
  const alreadyInInteractions = genaiChat.interactions.some(
    (it) =>
      it.kind === 'assistant' &&
      (it.id === messageId || (it as any).originalMessageId === messageId),
  );

  if (alreadyInInteractions) {
    console.log(
      '[ThinkingPlaceholder] Hiding placeholder - already in interactions',
      { messageId },
    );
    return null;
  }

  const header =
    genaiChat.messageMeta?.[messageId]?.headerText || 'assistant';
  const leftAccent = !!genaiChat.messageMeta?.[messageId]?.leftAccent;
  const isQuality =
    genaiSettings.strength === 'quality' ||
    /_quality$/.test(genaiSettings.model || '');
  const threshold = genaiSettings.groundingEnabled ? 35 : 20;
  const showInfo = (genaiThinkingSeconds || 0) >= threshold && isQuality;
  const infoMsg = genaiSettings.groundingEnabled
    ? t('unfold.status.modelUpdatingAnalysis')
    : t('unfold.status.modelComplexReasoning');

  return (
    <ChatMessage
      key={messageId}
      role="assistant"
      align="left"
      isThinking={true}
      thinkingSeconds={genaiThinkingSeconds}
      isError={false}
      headerText={header}
      leftAccent={leftAccent}
      footer={undefined}
      averageTimingSeconds={averageTimingSeconds}
    >
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
        {showInfo && (
          <div
            className="text-[11px]"
            style={{
              color: 'var(--text-secondary-strong)',
            }}
          >
            {infoMsg}
          </div>
        )}
      </div>
    </ChatMessage>
  );
};


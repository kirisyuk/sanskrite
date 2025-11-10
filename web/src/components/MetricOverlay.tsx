import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { useTranslation } from 'react-i18next';
import { useGenaiChatStore } from '../stores/genaiChatStore';

export const MetricOverlay: React.FC = () => {
  const { t } = useTranslation();
  const genaiChat = useGenaiChatStore();
  
  const openKey = genaiChat.metricOverlayOpenKey || null;
  if (!openKey) return null;
  
  const cache = genaiChat.metricOverlayCache[openKey];
  const loading = !!genaiChat.metricOverlayLoading && !cache?.content;
  const content = cache?.content || '';

  const handleClose = () => {
    try {
      genaiChat.closeMetricOverlay();
    } catch (e) {
      console.error('[MetricOverlay] Error closing overlay:', e);
    }
  };

  return (
    <div
      className="absolute inset-0 z-30 rounded-xl"
      style={{
        background: 'var(--bg-elevated)',
        border: '1px solid var(--overlay-line-90)',
        overflow: 'hidden',
      }}
    >
      <div
        className="flex items-center justify-between px-3 py-2"
        style={{
          borderBottom: '1px solid var(--overlay-line-90)',
        }}
      >
        <div
          className="text-13"
          style={{ color: 'var(--text-heading)' }}
        >
          {cache?.metric?.metricName ||
            (cache?.metric as any)?.name ||
            cache?.metric?.id ||
            t('unfold.sections.metric')}
        </div>
        <button
          type="button"
          className="rounded-lg transition-colors"
          aria-label={t('unfold.sections.close')}
          title={t('unfold.sections.close')}
          onClick={handleClose}
          style={{
            backgroundColor: 'transparent',
            color: 'var(--text-secondary-strong)',
          }}
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
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M6 6l12 12M18 6L6 18"
            />
          </svg>
        </button>
      </div>
      <div className="absolute inset-0 pt-[38px]">
        <div className="w-full h-full overflow-y-auto px-4 py-3">
        {loading ? (
          <div
            className="text-13"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            {t('unfold.status.loading', { defaultValue: 'Loading...' })}
          </div>
        ) : content ? (
          <div
            className="markdown text-13"
            style={{ color: 'var(--text-heading)' }}
          >
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[rehypeRaw]}
            >
              {content}
            </ReactMarkdown>
          </div>
        ) : (
          <div
            className="text-13"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            {t('unfold.status.noContent', { defaultValue: 'No content available' })}
          </div>
        )}
        </div>
      </div>
    </div>
  );
};


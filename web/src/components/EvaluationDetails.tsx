import React from 'react';
import { useTranslation } from 'react-i18next';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { ThemeContext } from './ThemeProvider';

// Inline evaluation details renderer (reads eval.bias interactions)
export const EvaluationDetails: React.FC<{ messageId: string }> = ({ messageId }) => {
  // PERFORMANCE FIX: Subscribe only to specific values, not entire store
  const projectId = useGenaiChatStore((s) => s.projectId);
  const interactionsLength = useGenaiChatStore((s) => s.interactions.length);
  const messagesLength = useGenaiChatStore((s) => s.messages.length);
  const persistedByLocalId = useGenaiChatStore((s) => (s as any).persistedByLocalId);
  
  // Get interactions/messages only when needed via getState()
  const getInteractions = React.useCallback(() => useGenaiChatStore.getState().interactions, []);
  const getMessages = React.useCallback(() => useGenaiChatStore.getState().messages, []);
  
  // NOTE: hydrateInteractions вызывается в DiscoveryPage, не нужно дублировать здесь
  const { t, i18n } = useTranslation();
  const ev = React.useMemo(() => {
    try {
      const map = persistedByLocalId || {};
      const serverId = map[messageId];
      // Build reverse map for bidirectional lookup
      const reverseMap: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      const localIds = reverseMap[messageId] || [];
      const interactions = getInteractions();
      const found = [...interactions].reverse().find((it: any) => 
        it.subkind === 'eval.bias' && 
        (it.replyTo === messageId || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
      );
      if (!found) return null;
      try { return JSON.parse((found as any).content || '{}'); } catch { try { return JSON.parse((found as any).contentSanitized || '{}'); } catch { return null; } }
    } catch { return null; }
  }, [getInteractions, interactionsLength, persistedByLocalId, messageId]);
  const [open, setOpen] = React.useState(false);
  const [selectedTag, setSelectedTag] = React.useState<string | null>(null);
  const msg = React.useMemo(() => {
    const messages = getMessages();
    return messages.find((m) => m.id === messageId);
  }, [getMessages, messagesLength, messageId]);
  const { theme } = React.useContext(ThemeContext);
  if (!ev) return null;
  const level = Math.max(0, Math.min(1, Number(ev.biasLevel || 0)));
  const hue = Math.round(120 * (1 - level));
  const fill = `hsl(${hue}, 60%, 70%)`;
  const labelLevel = Math.round(level * 10) / 10;
  
  // 5 градаций: 0-0.2, 0.2-0.4, 0.4-0.6, 0.6-0.8, 0.8-1.0
  const bucketLabel = level <= 0.2 
    ? t('unfold.bias.neutral')
    : level <= 0.4
    ? t('unfold.bias.balanced')
    : level <= 0.6
    ? t('unfold.bias.subjective')
    : level <= 0.8
    ? t('unfold.bias.oneSided')
    : t('unfold.bias.biased');

  const tLevel = (() => {
    try {
      const v = (typeof (ev as any).thoughtfulnessLevel === 'number') ? (ev as any).thoughtfulnessLevel : (ev as any).thoughtfulness;
      return Math.max(0, Math.min(1, Number(v || 0)));
    } catch { return 0; }
  })();
  const tHue = Math.round(120 * (tLevel));
  const tFill = `hsl(${tHue}, 60%, 70%)`;
  const tLabelLevel = Math.round(tLevel * 10) / 10;
  
  // 5 градаций: 0-0.2, 0.2-0.4, 0.4-0.6, 0.6-0.8, 0.8-1.0
  const tLabel = tLevel <= 0.2
    ? t('unfold.thoughtfulness.shallow')
    : tLevel <= 0.4
    ? t('unfold.thoughtfulness.basic')
    : tLevel <= 0.6
    ? t('unfold.thoughtfulness.moderate')
    : tLevel <= 0.8
    ? t('unfold.thoughtfulness.deep')
    : t('unfold.thoughtfulness.comprehensive');

  return (
    <div className="mt-1" style={{ width: '100%', position: 'relative' }}>
      <button
        type="button"
        className="flex items-center gap-2 w-full text-left"
        onClick={() => setOpen(!open)}
        style={{ background: 'transparent', border: 'none', padding: 0, cursor: 'pointer' }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', width: 16, height: 16 }}>
          <div style={{ width: 8, height: 8, borderRadius: '50%', backgroundColor: fill }} />
        </div>
        <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>{labelLevel.toFixed(1)} {bucketLabel}</div>
      </button>
      {open && (
        <div className="mt-2 space-y-2">
          {Array.isArray((ev as any).categories) && (ev as any).categories.length > 0 && (
            <>
              <div className="flex flex-wrap gap-1">
                {(ev as any).categories.map((c: any, i: number) => {
                  const code = String(c?.code || '');
                  return (
                  <button
                    key={i}
                    type="button"
                    onClick={() => setSelectedTag(selectedTag === code ? null : code)}
                    className="px-2 py-0.5 rounded-full text-[10px] transition-colors cursor-pointer"
                    style={{ 
                      backgroundColor: selectedTag === code ? 'var(--white-20)' : 'var(--white-10)', 
                      color: 'var(--text-secondary-strong)',
                      border: 'none',
                    }}
                    onMouseEnter={(e) => {
                      if (selectedTag !== code) {
                        (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)';
                      }
                    }}
                    onMouseLeave={(e) => {
                      if (selectedTag !== code) {
                        (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                      }
                    }}
                  >
                    {t(`unfold.bias.tags.${code}`, { defaultValue: code })}
                  </button>
                );})}
              </div>
              {selectedTag && (() => {
                const found = ((ev as any).categories || []).find((c: any) => String(c?.code || '') === selectedTag);
                const expl = typeof found?.explanation === 'string' ? found.explanation : t(`unfold.bias.descriptions.${selectedTag}`, { defaultValue: '' });
                return (
                  <div className="mt-2 p-3 rounded-lg" style={{ backgroundColor: 'var(--surface-700)', border: '1px solid var(--overlay-line-90)' }}>
                    <div className="text-[12px]" style={{ color: '#FFFFFF', lineHeight: '1.5' }}>
                      {expl}
                    </div>
                  </div>
                );
              })()}
            </>
          )}
          <div className="space-y-1">
            <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>{t('unfold.thoughtfulness.label')}</div>
            <div className="flex items-center gap-2">
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', width: 16, height: 16 }}>
                <div style={{ width: 8, height: 8, borderRadius: '50%', backgroundColor: tFill }} />
              </div>
              <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>{tLabelLevel.toFixed(1)} {tLabel}</div>
            </div>
            {/* Detailed dimension rows hidden per request */}
          </div>
        </div>
      )}
    </div>
  );
};


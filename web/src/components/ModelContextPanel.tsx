import React, { useMemo } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { ThemeContext } from './ThemeProvider';
import type { LocalInteraction } from '../stores/genaiChatStore';

// Цвета для разных типов interactions
const getInteractionColor = (kind: string, theme: 'light' | 'dark'): string => {
  const colors: Record<string, { light: string; dark: string }> = {
    user: { light: '#3B82F6', dark: '#60A5FA' }, // blue
    assistant: { light: '#10B981', dark: '#34D399' }, // green
    system: { light: '#8B5CF6', dark: '#A78BFA' }, // purple
    ontology: { light: '#F59E0B', dark: '#FBBF24' }, // yellow/orange
    research: { light: '#06B6D4', dark: '#22D3EE' }, // cyan
    note: { light: '#EC4899', dark: '#F472B6' }, // pink
  };
  return colors[kind]?.[theme] || (theme === 'dark' ? '#9CA3AF' : '#6B7280');
};

export const ModelContextPanel: React.FC = () => {
  const { theme } = React.useContext(ThemeContext);
  const interactions = useGenaiChatStore((s) => s.interactions);

  // Фильтруем interactions так же, как в genaiChatStore для LLM context
  // Это те interactions, которые попадают в контекст для Model режима
  const contextInteractions = useMemo(() => {
    const filtered = interactions.filter((it) => {
      // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
      if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
        return false;
      }
      // Exclude queued tasks interactions (they are metadata, not chat content)
      if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind === 'queued.tasks') {
        return false;
      }
      // Exclude perspective analysis interactions (kind='system' with subkind starting with 'perspective.' and ending with '.analysis')
      if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
        return false;
      }
      // Exclude eval.* system messages (bias, stance, user_request, review evaluations)
      if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('eval.')) {
        return false;
      }
      // Exclude action, section markers (these are UI markers, not chat content)
      if (it.kind === 'action' || it.kind === 'section_start' || it.kind === 'section_end') {
        return false;
      }
      // Exclude agent execution interactions (they are execution status, not chat content)
      if (it.kind === 'agent.execution') {
        return false;
      }
      return true;
    });

    // Сортируем по времени создания (старые сначала)
    return filtered.sort((a, b) => {
      const timeA = new Date(a.createdAt || 0).getTime();
      const timeB = new Date(b.createdAt || 0).getTime();
      return timeA - timeB;
    });
  }, [interactions]);

  if (contextInteractions.length === 0) {
    return (
      <div
        style={{
          width: '100%',
          height: '100%',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '24px',
          color: 'var(--text-secondary-strong)',
          fontSize: '14px',
        }}
      >
        No context interactions
      </div>
    );
  }

  return (
    <div
      style={{
        width: '100%',
        height: '100%',
        overflowY: 'auto',
        padding: '16px',
        background: 'var(--bg-elevated)',
      }}
    >
      <div
        style={{
          fontSize: '12px',
          fontWeight: 600,
          color: 'var(--text-heading)',
          marginBottom: '16px',
          textTransform: 'uppercase',
          letterSpacing: '0.5px',
        }}
      >
        Context ({contextInteractions.length})
      </div>
      
      <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        {contextInteractions.map((interaction) => {
          const color = getInteractionColor(interaction.kind || 'unknown', theme);
          
          // Формируем заголовок
          let title: string = interaction.kind || 'unknown';
          if (interaction.subkind) {
            title += ` / ${interaction.subkind}`;
          }
          
          // Для ontology interactions показываем имя сущности
          if (interaction.kind === 'ontology' && interaction.content) {
            try {
              const entityData = JSON.parse(interaction.content);
              const name = entityData.canonicalName || entityData.name || entityData.title || entityData.label || interaction.id;
              title = `${title}: ${name}`;
            } catch {
              // Ignore parse errors
            }
          }
          
          // Для user/assistant показываем превью контента
          let preview = '';
          if (interaction.content) {
            preview = interaction.content.length > 100 
              ? interaction.content.substring(0, 100) + '...'
              : interaction.content;
          } else if (interaction.summary) {
            preview = interaction.summary.length > 100
              ? interaction.summary.substring(0, 100) + '...'
              : interaction.summary;
          }
          
          return (
            <div
              key={interaction.id}
              style={{
                padding: '12px',
                borderRadius: '8px',
                border: `1px solid var(--border-subtle)`,
                background: 'var(--bg-body)',
              }}
            >
              {/* Заголовок с типом */}
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px' }}>
                <div
                  style={{
                    width: '8px',
                    height: '8px',
                    borderRadius: '50%',
                    backgroundColor: color,
                    flexShrink: 0,
                  }}
                />
                <div
                  style={{
                    fontSize: '10px',
                    fontWeight: 600,
                    color: color,
                    textTransform: 'uppercase',
                    letterSpacing: '0.5px',
                  }}
                >
                  {title}
                </div>
              </div>

              {/* ID */}
              <div
                style={{
                  fontSize: '10px',
                  color: 'var(--text-tertiary-weak)',
                  fontFamily: 'monospace',
                  marginBottom: '8px',
                  wordBreak: 'break-all',
                }}
              >
                {interaction.id}
              </div>

              {/* Превью контента */}
              {preview && (
                <div
                  style={{
                    fontSize: '12px',
                    color: 'var(--text-secondary-strong)',
                    marginBottom: '8px',
                    lineHeight: '1.4',
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                  }}
                >
                  {preview}
                </div>
              )}

              {/* Полный объект interaction (свернутый) */}
              <details
                style={{
                  fontSize: '10px',
                  color: 'var(--text-tertiary-weak)',
                }}
              >
                <summary
                  style={{
                    cursor: 'pointer',
                    color: 'var(--text-secondary)',
                    marginBottom: '4px',
                  }}
                >
                  View Full Interaction
                </summary>
                <pre
                  style={{
                    marginTop: '8px',
                    padding: '8px',
                    background: 'var(--bg-elevated)',
                    borderRadius: '4px',
                    overflow: 'auto',
                    fontSize: '10px',
                    fontFamily: 'monospace',
                    color: 'var(--text-secondary)',
                    maxHeight: '400px',
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                  }}
                >
                  {JSON.stringify(interaction, null, 2)}
                </pre>
              </details>
            </div>
          );
        })}
      </div>
    </div>
  );
};


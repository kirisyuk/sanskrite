import React from 'react';
import { NodeTypes, Handle, Position } from 'reactflow';
import { ThemeContext } from './ThemeProvider';

// Типы нод для Horizon графа (на основе LensItem)
export interface HorizonNodeData {
  id: string;
  lensItemId: string; // ID LensItem
  fragmentId?: string;
  parentFragmentId?: string;
  rootFragmentId?: string;
  edgeTypeFromParent?: string; // Тип связи с родителем (для сортировки при Arrange)
  directionFromParent?: 'incoming' | 'outgoing'; // Направление связи: incoming (сверху) или outgoing (снизу)
  depth?: number;
  canonicalPath?: string;
  indexPaths?: string[];
  label?: string;
  type?: string; // FragmentType
  state?: Record<string, number>; // Метрики
  status?: string;
  expanded?: boolean;
  collapsed?: boolean;
  critical?: boolean;
  onLoadRelated?: (fragmentId: string) => void; // Функция для загрузки связанных элементов
  isLoadingRelated?: boolean; // Флаг загрузки связанных элементов
  [key: string]: any;
}

// Цветовая схема по типам сущностей
interface NodeColorScheme {
  bg: string;
  bgDark: string;
  border: string;
  borderDark: string;
  text: string;
  textDark: string;
  accent: string;
  accentDark: string;
}

const NODE_COLORS: Record<string, NodeColorScheme> = {
  'ORGANIZATION': {
    bg: '#DBEAFE', bgDark: '#1E3A5F',
    border: '#3B82F6', borderDark: '#60A5FA',
    text: '#1E40AF', textDark: '#BFDBFE',
    accent: '#3B82F6', accentDark: '#60A5FA',
  },
  'GEO_REGION': {
    bg: '#D1FAE5', bgDark: '#064E3B',
    border: '#10B981', borderDark: '#34D399',
    text: '#065F46', textDark: '#A7F3D0',
    accent: '#10B981', accentDark: '#34D399',
  },
  'FACILITY': {
    bg: '#FFEDD5', bgDark: '#7C2D12',
    border: '#F97316', borderDark: '#FB923C',
    text: '#9A3412', textDark: '#FED7AA',
    accent: '#F97316', accentDark: '#FB923C',
  },
  'PERSON': {
    bg: '#CFFAFE', bgDark: '#164E63',
    border: '#06B6D4', borderDark: '#22D3EE',
    text: '#155E75', textDark: '#A5F3FC',
    accent: '#06B6D4', accentDark: '#22D3EE',
  },
  'TECHNOLOGY': {
    bg: '#EDE9FE', bgDark: '#4C1D95',
    border: '#8B5CF6', borderDark: '#A78BFA',
    text: '#5B21B6', textDark: '#DDD6FE',
    accent: '#8B5CF6', accentDark: '#A78BFA',
  },
  'PRODUCT': {
    bg: '#FEF3C7', bgDark: '#78350F',
    border: '#F59E0B', borderDark: '#FBBF24',
    text: '#92400E', textDark: '#FDE68A',
    accent: '#F59E0B', accentDark: '#FBBF24',
  },
  'SERVICE': {
    bg: '#FCE7F3', bgDark: '#831843',
    border: '#EC4899', borderDark: '#F472B6',
    text: '#9D174D', textDark: '#FBCFE8',
    accent: '#EC4899', accentDark: '#F472B6',
  },
  'EVENT': {
    bg: '#FEE2E2', bgDark: '#7F1D1D',
    border: '#EF4444', borderDark: '#F87171',
    text: '#991B1B', textDark: '#FECACA',
    accent: '#EF4444', accentDark: '#F87171',
  },
  'RISK': {
    bg: '#FEE2E2', bgDark: '#450A0A',
    border: '#DC2626', borderDark: '#EF4444',
    text: '#7F1D1D', textDark: '#FECACA',
    accent: '#DC2626', accentDark: '#EF4444',
  },
  'default': {
    bg: '#F1F5F9', bgDark: '#1E293B',
    border: '#64748B', borderDark: '#94A3B8',
    text: '#334155', textDark: '#CBD5E1',
    accent: '#64748B', accentDark: '#94A3B8',
  },
};

// Получить цветовую схему для типа
const getNodeColors = (type: string | undefined, theme: 'light' | 'dark'): {
  bg: string;
  border: string;
  text: string;
  accent: string;
} => {
  const normalizedType = (type || '').toUpperCase().replace(/\s+/g, '_');
  const scheme = NODE_COLORS[normalizedType] || NODE_COLORS['default'];
  
  return {
    bg: theme === 'dark' ? scheme.bgDark : scheme.bg,
    border: theme === 'dark' ? scheme.borderDark : scheme.border,
    text: theme === 'dark' ? scheme.textDark : scheme.text,
    accent: theme === 'dark' ? scheme.accentDark : scheme.accent,
  };
};

// SVG иконки для разных типов
const NodeIcon: React.FC<{ type: string | undefined; color: string; size?: number }> = ({ type, color, size = 16 }) => {
  const normalizedType = (type || '').toUpperCase().replace(/\s+/g, '_');
  const style: React.CSSProperties = { width: size, height: size, flexShrink: 0 };
  
  switch (normalizedType) {
    case 'ORGANIZATION':
      // Building icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 21h18" />
          <path d="M5 21V7l8-4v18" />
          <path d="M19 21V11l-6-4" />
          <path d="M9 9v.01" />
          <path d="M9 12v.01" />
          <path d="M9 15v.01" />
          <path d="M9 18v.01" />
        </svg>
      );
    
    case 'GEO_REGION':
    case 'LOCATION':
      // Globe icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <circle cx="12" cy="12" r="10" />
          <path d="M2 12h20" />
          <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
        </svg>
      );
    
    case 'FACILITY':
      // Factory icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M2 20a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8l-7 5V8l-7 5V4a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2v16z" />
          <path d="M17 18h1" />
          <path d="M12 18h1" />
          <path d="M7 18h1" />
        </svg>
      );
    
    case 'PERSON':
      // User icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <circle cx="12" cy="8" r="5" />
          <path d="M20 21a8 8 0 0 0-16 0" />
        </svg>
      );
    
    case 'TECHNOLOGY':
      // Cpu icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <rect x="4" y="4" width="16" height="16" rx="2" />
          <rect x="9" y="9" width="6" height="6" />
          <path d="M9 1v3" />
          <path d="M15 1v3" />
          <path d="M9 20v3" />
          <path d="M15 20v3" />
          <path d="M20 9h3" />
          <path d="M20 14h3" />
          <path d="M1 9h3" />
          <path d="M1 14h3" />
        </svg>
      );
    
    case 'PRODUCT':
      // Package icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M16.5 9.4 7.55 4.24" />
          <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
          <polyline points="3.29 7 12 12 20.71 7" />
          <line x1="12" y1="22" x2="12" y2="12" />
        </svg>
      );
    
    case 'SERVICE':
      // Settings icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      );
    
    case 'EVENT':
      // Calendar icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <rect x="3" y="4" width="18" height="18" rx="2" ry="2" />
          <line x1="16" y1="2" x2="16" y2="6" />
          <line x1="8" y1="2" x2="8" y2="6" />
          <line x1="3" y1="10" x2="21" y2="10" />
          <path d="M8 14h.01" />
          <path d="M12 14h.01" />
          <path d="M16 14h.01" />
        </svg>
      );
    
    case 'RISK':
      // Alert triangle icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
      );
    
    default:
      // Default circle icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <circle cx="12" cy="12" r="10" />
          <circle cx="12" cy="12" r="3" fill={color} />
        </svg>
      );
  }
};

// Получить цвет для значения метрики (красный=плохо, жёлтый=средне, зелёный=хорошо)
const getMetricColor = (key: string, value: number): string => {
  // Метрики риска/уязвимости — инвертированные (высокое = плохо)
  const isRiskMetric = key.toLowerCase().includes('risk') || 
                       key.toLowerCase().includes('vulnerability') ||
                       key.toLowerCase().includes('exposure');
  
  // Метрики здоровья/производительности — прямые (высокое = хорошо)
  const isHealthMetric = key.toLowerCase().includes('health') ||
                         key.toLowerCase().includes('alignment') ||
                         key.toLowerCase().includes('utilization');
  
  let normalizedValue = value;
  if (isRiskMetric) {
    normalizedValue = 1 - value; // Инвертируем для риска
  }
  
  if (normalizedValue >= 0.7) return '#22C55E'; // green
  if (normalizedValue >= 0.4) return '#F59E0B'; // amber
  return '#EF4444'; // red
};

// Базовый узел для Horizon фрагмента
const HorizonFragmentNode: React.FC<{ data: HorizonNodeData }> = ({ data }) => {
  const { theme } = React.useContext(ThemeContext);
  const colors = getNodeColors(data.type, theme);
  
  const isCritical = data.critical;
  const isCollapsed = data.collapsed;
  const isExpanded = data.expanded;
  
  // Цвета с учётом состояния
  const bgColor = colors.bg;
  const borderColor = isCritical ? '#EF4444' : colors.border;
  const borderWidth = isCritical ? 3 : 2;
  const textColor = colors.text;
  const accentColor = isCritical ? '#EF4444' : colors.accent;

  const displayLabel = data.label || data.canonicalPath?.split('/').pop() || 'Fragment';
  const displayType = data.type || 'Unknown';

  // Форматирование типа для отображения
  const formatType = (type: string) => {
    return type.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
  };

  return (
    <div
      style={{
        background: bgColor,
        border: `${borderWidth}px solid ${borderColor}`,
        borderRadius: '10px',
        padding: isCollapsed ? '8px 12px' : '10px 14px',
        minWidth: isCollapsed ? '100px' : '160px',
        maxWidth: '280px',
        boxShadow: isCritical
          ? `0 0 0 2px rgba(239, 68, 68, 0.3), 0 4px 12px rgba(239, 68, 68, 0.2)`
          : theme === 'dark' 
            ? `0 4px 12px rgba(0, 0, 0, 0.4)`
            : `0 4px 12px rgba(0, 0, 0, 0.1)`,
        position: 'relative',
        opacity: isCollapsed ? 0.7 : 1,
        transition: 'all 0.2s ease',
      }}
    >
      {/* Source handle снизу (для исходящих связей - от кнопки +) */}
      <Handle
        id="source-bottom"
        type="source"
        position={Position.Bottom}
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${bgColor}`,
          bottom: '-5px',
        }}
      />
      
      {/* Target handle сверху (для входящих связей) */}
      <Handle
        id="target-top"
        type="target"
        position={Position.Top}
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${bgColor}`,
          top: '-5px',
        }}
      />
      
      {/* Старые handle'ы для обратной совместимости (лево/право) */}
      <Handle
        id="source-right"
        type="source"
        position={Position.Right}
        style={{
          background: accentColor,
          width: '8px',
          height: '8px',
          border: `2px solid ${bgColor}`,
          opacity: 0.5,
        }}
      />
      
      <Handle
        id="target-left"
        type="target"
        position={Position.Left}
        style={{
          background: accentColor,
          width: '8px',
          height: '8px',
          border: `2px solid ${bgColor}`,
          opacity: 0.5,
        }}
      />

      {/* Заголовок с иконкой */}
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: '8px' }}>
        <div
          style={{
            padding: '4px',
            borderRadius: '6px',
            background: theme === 'dark' ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.05)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <NodeIcon type={data.type} color={accentColor} size={18} />
        </div>
        
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: '13px',
              fontWeight: 600,
              color: textColor,
              lineHeight: 1.3,
              wordBreak: 'break-word',
            }}
            title={displayLabel}
          >
            {displayLabel}
          </div>
          
          {!isCollapsed && (
            <div
              style={{
                fontSize: '10px',
                color: accentColor,
                marginTop: '2px',
                fontWeight: 500,
                textTransform: 'uppercase',
                letterSpacing: '0.5px',
              }}
            >
              {formatType(displayType)}
            </div>
          )}
        </div>
        
        {/* Индикатор критичности */}
        {isCritical && (
          <div
            style={{
              width: '10px',
              height: '10px',
              borderRadius: '50%',
              background: '#EF4444',
              flexShrink: 0,
              boxShadow: '0 0 6px rgba(239, 68, 68, 0.6)',
              animation: 'pulse 2s infinite',
            }}
            title="Critical"
          />
        )}
      </div>

      {/* Метрики (если не collapsed) */}
      {!isCollapsed && data.state && Object.keys(data.state).length > 0 && (
        <div
          style={{
            marginTop: '8px',
            paddingTop: '8px',
            borderTop: `1px solid ${theme === 'dark' ? 'rgba(255, 255, 255, 0.15)' : 'rgba(0, 0, 0, 0.1)'}`,
            fontSize: '11px',
          }}
        >
          {Object.entries(data.state).slice(0, 3).map(([key, value]) => {
            const metricColor = getMetricColor(key, value);
            // Сокращаем длинные ключи
            const shortKey = key.split('/').pop() || key;
            
            return (
              <div 
                key={key} 
                style={{ 
                  display: 'flex', 
                  alignItems: 'center',
                  justifyContent: 'space-between', 
                  gap: '8px',
                  marginBottom: '3px',
                }}
              >
                <span 
                  style={{ 
                    color: theme === 'dark' ? 'rgba(255,255,255,0.7)' : 'rgba(0,0,0,0.6)',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                    flex: 1,
                  }}
                  title={key}
                >
                  {shortKey}:
                </span>
                <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
                  <span 
                    style={{ 
                      fontWeight: 600,
                      fontFamily: 'monospace',
                      color: textColor,
                    }}
                  >
                    {typeof value === 'number' ? value.toFixed(2) : value}
                  </span>
                  <div
                    style={{
                      width: '8px',
                      height: '8px',
                      borderRadius: '50%',
                      background: metricColor,
                      flexShrink: 0,
                    }}
                    title={value >= 0.7 ? 'Good' : value >= 0.4 ? 'Medium' : 'Low'}
                  />
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Кнопка "+" для загрузки связанных элементов */}
      {data.fragmentId && data.onLoadRelated && (
        <div
          style={{
            position: 'absolute',
            bottom: '-16px',
            left: '50%',
            transform: 'translateX(-50%)',
            zIndex: 10,
          }}
        >
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              if (data.fragmentId && data.onLoadRelated) {
                data.onLoadRelated(data.fragmentId);
              }
            }}
            disabled={data.isLoadingRelated}
            style={{
              width: '26px',
              height: '26px',
              borderRadius: '50%',
              border: `2px solid ${accentColor}`,
              background: bgColor,
              color: accentColor,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              cursor: data.isLoadingRelated ? 'wait' : 'pointer',
              fontSize: '16px',
              fontWeight: 'bold',
              lineHeight: 1,
              padding: 0,
              boxShadow: theme === 'dark' 
                ? `0 2px 8px rgba(0, 0, 0, 0.4)`
                : `0 2px 8px rgba(0, 0, 0, 0.15)`,
              opacity: data.isLoadingRelated ? 0.6 : 1,
              transition: 'all 0.2s ease',
            }}
            title="Load related fragments"
          >
            {data.isLoadingRelated ? (
              <span style={{ fontSize: '12px' }}>⏳</span>
            ) : '+'}
          </button>
        </div>
      )}

      {/* CSS для анимации пульса */}
      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; transform: scale(1); }
          50% { opacity: 0.7; transform: scale(1.1); }
        }
      `}</style>
    </div>
  );
};

// Экспортируем типы нод для Horizon графа
export const HORIZON_NODE_TYPES: NodeTypes = Object.freeze({
  horizonFragment: HorizonFragmentNode,
});

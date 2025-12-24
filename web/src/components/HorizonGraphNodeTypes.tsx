import React from 'react';
import { NodeTypes, Handle, Position } from 'reactflow';
import { ThemeContext } from './ThemeProvider';

// Типы нод для Horizon графа (на основе LensItem)
export interface HorizonNodeData {
  id: string;
  lensItemId: string; // ID LensItem
  fragmentId?: string;
  canonicalPath?: string;
  label?: string;
  type?: string; // FragmentType
  state?: Record<string, number>; // Метрики
  status?: string;
  expanded?: boolean;
  collapsed?: boolean;
  critical?: boolean;
  [key: string]: any;
}

// Цвета для разных типов фрагментов
const getFragmentColor = (type: string | undefined, critical: boolean | undefined, theme: 'light' | 'dark'): string => {
  const colors: Record<string, { light: string; dark: string }> = {
    'Organization': { light: '#3B82F6', dark: '#60A5FA' }, // blue
    'Location': { light: '#10B981', dark: '#34D399' }, // green
    'Technology': { light: '#8B5CF6', dark: '#A78BFA' }, // purple
    'Product': { light: '#F59E0B', dark: '#FBBF24' }, // yellow/orange
    'Service': { light: '#EC4899', dark: '#F472B6' }, // pink
    'Person': { light: '#06B6D4', dark: '#22D3EE' }, // cyan
    'Event': { light: '#F97316', dark: '#FB923C' }, // orange
    'Risk': { light: '#EF4444', dark: '#F87171' }, // red
  };
  
  const baseColor = colors[type || '']?.[theme] || (theme === 'dark' ? '#60A5FA' : '#3B82F6');
  
  // Если critical, делаем цвет ярче/насыщеннее
  if (critical) {
    return theme === 'dark' ? baseColor : baseColor;
  }
  
  return baseColor;
};

// Иконка для фрагмента
const getFragmentIcon = (type: string | undefined): React.ReactNode => {
  const iconSize = 14;
  const iconStyle: React.CSSProperties = {
    width: iconSize,
    height: iconSize,
    flexShrink: 0,
  };

  // Простая иконка узла (кружок с точкой)
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
      <circle cx="12" cy="12" r="10" />
      <circle cx="12" cy="12" r="3" fill="currentColor" />
    </svg>
  );
};

// Базовый узел для Horizon фрагмента
const HorizonFragmentNode: React.FC<{ data: HorizonNodeData }> = ({ data }) => {
  const { theme } = React.useContext(ThemeContext);
  const color = getFragmentColor(data.type, data.critical, theme);
  const bgColor = theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)';
  const textColor = theme === 'dark' ? 'var(--text-heading)' : 'var(--text-heading)';
  const borderColor = data.critical ? '#EF4444' : color;
  const borderWidth = data.critical ? 3 : 2;

  const displayLabel = data.label || data.canonicalPath?.split('/').pop() || 'Fragment';
  const isCollapsed = data.collapsed;

  return (
    <div
      style={{
        background: bgColor,
        border: `${borderWidth}px solid ${borderColor}`,
        borderRadius: '8px',
        padding: isCollapsed ? '6px 10px' : '8px 12px',
        minWidth: isCollapsed ? '80px' : '120px',
        boxShadow: theme === 'dark' 
          ? `0 2px 8px rgba(0, 0, 0, 0.3), 0 0 0 1px ${borderColor}40`
          : `0 2px 8px rgba(0, 0, 0, 0.1), 0 0 0 1px ${borderColor}40`,
        position: 'relative',
        opacity: isCollapsed ? 0.6 : 1,
      }}
    >
      {/* Source handle (для исходящих связей) */}
      <Handle
        type="source"
        position={Position.Right}
        style={{
          background: color,
          width: '8px',
          height: '8px',
          border: `2px solid ${bgColor}`,
        }}
      />
      
      {/* Target handle (для входящих связей) */}
      <Handle
        type="target"
        position={Position.Left}
        style={{
          background: color,
          width: '8px',
          height: '8px',
          border: `2px solid ${bgColor}`,
        }}
      />

      <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
        {getFragmentIcon(data.type)}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: '12px',
              fontWeight: data.critical ? 600 : 500,
              color: textColor,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
          >
            {displayLabel}
          </div>
          {!isCollapsed && data.type && (
            <div
              style={{
                fontSize: '10px',
                color: 'var(--text-secondary-strong)',
                marginTop: '2px',
              }}
            >
              {data.type}
            </div>
          )}
        </div>
        {data.critical && (
          <div
            style={{
              width: '8px',
              height: '8px',
              borderRadius: '50%',
              background: '#EF4444',
              flexShrink: 0,
            }}
          />
        )}
      </div>

      {/* Метрики (если не collapsed) */}
      {!isCollapsed && data.state && Object.keys(data.state).length > 0 && (
        <div
          style={{
            marginTop: '6px',
            paddingTop: '6px',
            borderTop: `1px solid ${theme === 'dark' ? 'rgba(255, 255, 255, 0.1)' : 'rgba(0, 0, 0, 0.1)'}`,
            fontSize: '10px',
            color: 'var(--text-secondary-strong)',
          }}
        >
          {Object.entries(data.state).slice(0, 2).map(([key, value]) => (
            <div key={key} style={{ display: 'flex', justifyContent: 'space-between', gap: '8px' }}>
              <span>{key}:</span>
              <span style={{ fontWeight: 500 }}>{typeof value === 'number' ? value.toFixed(2) : value}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

// Экспортируем типы нод для Horizon графа
export const HORIZON_NODE_TYPES: NodeTypes = Object.freeze({
  horizonFragment: HorizonFragmentNode,
});


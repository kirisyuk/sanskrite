import React from 'react';
import { NodeTypes, Handle, Position } from 'reactflow';
import { ThemeContext } from './ThemeProvider';

// Типы для онтологических сущностей
type OntologyEntityType = 'entity' | 'event' | 'claim' | 'relation' | 'concept' | 'metricState' | 'trend' | 'causalLink' | 'scenario' | 'actionableItem';

interface OntologyNodeData {
  id: string;
  type: OntologyEntityType;
  label: string;
  canonicalName?: string;
  description?: string;
  [key: string]: any;
}

// Цвета для разных типов сущностей (в соответствии с цветовой схемой)
const getEntityColor = (type: OntologyEntityType, theme: 'light' | 'dark'): string => {
  const colors: Record<OntologyEntityType, { light: string; dark: string }> = {
    entity: { light: '#3B82F6', dark: '#60A5FA' }, // blue
    event: { light: '#10B981', dark: '#34D399' }, // green
    claim: { light: '#F59E0B', dark: '#FBBF24' }, // yellow/orange
    relation: { light: '#8B5CF6', dark: '#A78BFA' }, // purple
    concept: { light: '#EC4899', dark: '#F472B6' }, // pink
    metricState: { light: '#06B6D4', dark: '#22D3EE' }, // cyan
    trend: { light: '#F97316', dark: '#FB923C' }, // orange
    causalLink: { light: '#EF4444', dark: '#F87171' }, // red
    scenario: { light: '#6366F1', dark: '#818CF8' }, // indigo
    actionableItem: { light: '#14B8A6', dark: '#2DD4BF' }, // teal
  };
  return colors[type]?.[theme] || (theme === 'dark' ? '#60A5FA' : '#3B82F6');
};

// Иконки для разных типов сущностей (строгий минималистичный стиль)
const getEntityIcon = (type: OntologyEntityType): React.ReactNode => {
  const iconSize = 14;
  const iconStyle: React.CSSProperties = {
    width: iconSize,
    height: iconSize,
    flexShrink: 0,
  };

  switch (type) {
    case 'entity':
      // Кубик - объект/сущность
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
          <path d="M3.27 6.96L12 12.01l8.73-5.05" />
          <path d="M12 22.08V12" />
        </svg>
      );
    case 'event':
      // Молния - событие
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" />
        </svg>
      );
    case 'claim':
      // Кавычки - утверждение
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M3 21c3 0 7-1 7-8V5c0-1.25-.756-2.017-2-2H4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2 1 0 1 0 1 1v1c0 1-1 2-2 2s-1 .008-1 1.031V20c0 1 0 1 1 1z" />
          <path d="M15 21c3 0 7-1 7-8V5c0-1.25-.757-2.017-2-2h-4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2h.75c0 2.25.25 4-2.75 4v3c0 1 0 1 1 1z" />
        </svg>
      );
    case 'relation':
      // Две связанные точки - связь
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <circle cx="9" cy="12" r="3" />
          <circle cx="15" cy="12" r="3" />
          <path d="M12 12h0" />
        </svg>
      );
    case 'concept':
      // Звезда - концепция
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
        </svg>
      );
    case 'metricState':
      // График - метрика
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M3 3v18h18" />
          <path d="M18 17V9M12 17V5M6 17v-3" />
        </svg>
      );
    case 'trend':
      // Стрелка вверх - тренд
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M12 19V5M5 12l7-7 7 7" />
        </svg>
      );
    case 'causalLink':
      // Стрелка - причинная связь
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M5 12h14M12 5l7 7-7 7" />
        </svg>
      );
    case 'scenario':
      // Файл - сценарий
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" />
        </svg>
      );
    case 'actionableItem':
      // Галочка в круге - действие
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
          <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
          <path d="M22 4L12 14.01l-3-3" />
        </svg>
      );
    default:
      return null;
  }
};

// Кастомный узел для онтологических сущностей
const OntologyNode: React.FC<{ data: OntologyNodeData }> = ({ data }) => {
  const { theme } = React.useContext(ThemeContext);
  const color = getEntityColor(data.type, theme);
  const bgColor = theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)';
  const textColor = theme === 'dark' ? 'var(--text-heading)' : 'var(--text-heading)';
  const borderColor = color;

  return (
    <div
      style={{
        background: bgColor,
        border: `2px solid ${borderColor}`,
        borderRadius: '8px',
        padding: '8px 12px',
        minWidth: '120px',
        boxShadow: theme === 'dark' 
          ? `0 2px 8px rgba(0, 0, 0, 0.3), 0 0 0 1px ${borderColor}40`
          : `0 2px 8px rgba(0, 0, 0, 0.1), 0 0 0 1px ${borderColor}40`,
        position: 'relative',
      }}
    >
      {/* Source handle (для исходящих связей) */}
      <Handle
        type="source"
        position={Position.Right}
        id="source"
        style={{
          background: borderColor,
          width: '8px',
          height: '8px',
        }}
      />
      {/* Target handle (для входящих связей) */}
      <Handle
        type="target"
        position={Position.Left}
        id="target"
        style={{
          background: borderColor,
          width: '8px',
          height: '8px',
        }}
      />
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          fontSize: '10px',
          fontWeight: 600,
          color: borderColor,
          textTransform: 'uppercase',
          letterSpacing: '0.5px',
          marginBottom: '4px',
        }}
      >
        <span style={{ display: 'flex', alignItems: 'center', color: borderColor }}>
          {getEntityIcon(data.type)}
        </span>
        <span>
          {data.type.charAt(0).toUpperCase() + data.type.slice(1).replace(/([A-Z])/g, ' $1').trim()}
        </span>
      </div>
      <div
        style={{
          fontSize: '13px',
          fontWeight: 500,
          color: textColor,
          lineHeight: '1.4',
          wordBreak: 'break-word',
        }}
      >
        {data.label || data.canonicalName || data.id}
      </div>
      {data.description && (
        <div
          style={{
            fontSize: '11px',
            color: 'var(--text-secondary)',
            marginTop: '4px',
            lineHeight: '1.3',
            maxWidth: '200px',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {data.description}
        </div>
      )}
    </div>
  );
};

// Экспортируем nodeTypes как константу вне компонента
export const nodeTypes: NodeTypes = {
  ontology: OntologyNode,
};


import React from 'react';
import { NodeTypes } from 'reactflow';
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
      }}
    >
      <div
        style={{
          fontSize: '10px',
          fontWeight: 600,
          color: borderColor,
          textTransform: 'uppercase',
          letterSpacing: '0.5px',
          marginBottom: '4px',
        }}
      >
        {data.type.charAt(0).toUpperCase() + data.type.slice(1).replace(/([A-Z])/g, ' $1').trim()}
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


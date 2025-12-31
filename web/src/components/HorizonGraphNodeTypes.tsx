import React from 'react';
import { NodeTypes, Handle, Position, NodeProps } from 'reactflow';
import { useTranslation } from 'react-i18next';
import { ThemeContext } from './ThemeProvider';
import type { SemanticGroup, LinearSection } from '../lib/elkLayout';

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
  onCollapse?: (fragmentId: string) => void; // Функция для сворачивания (undo expand)
  isLoadingRelated?: boolean; // Флаг загрузки связанных элементов
  loadRelatedAttempted?: boolean; // UI: user tried loading related nodes at least once
  onScopeScout?: (fragmentId: string, canonicalPath?: string) => void; // Add to scope + run scouting depth=1
  isScopeScouting?: boolean; // UI: scope-scout request in-flight
  scopeScoutDone?: boolean; // UI: scope-scout already launched (show "+" again)
  // Group/container nodes (UI-only, not persisted)
  isGroup?: boolean;
  groupParentId?: string;
  groupSemanticGroup?: SemanticGroup;  // New: semantic category
  groupEdgeTypes?: string[];  // New: all edge types in this group
  groupDirection?: 'incoming' | 'outgoing' | 'both';  // Legacy, derived from semantic group
  groupEdgeType?: string;  // Legacy, first edge type
  groupCount?: number;
  /** Optional override for group title (used by Linear layout section containers) */
  groupTitle?: string;
  // Pagination for groups
  groupCurrentPage?: number;  // Current page (0-indexed)
  groupTotalPages?: number;   // Total number of pages
  groupPageSize?: number;     // Items per page (default 12)
  groupAllChildIds?: string[]; // All child IDs for edge filtering
  onGroupPageChange?: (groupId: string, page: number) => void; // Callback for page change
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

const hexToRgba = (hex: string, alpha: number) => {
  const h = String(hex || '').trim();
  if (!h.startsWith('#')) return `rgba(0,0,0,${alpha})`;
  const v = h.slice(1);
  if (v.length !== 6) return `rgba(0,0,0,${alpha})`;
  const r = parseInt(v.slice(0, 2), 16);
  const g = parseInt(v.slice(2, 4), 16);
  const b = parseInt(v.slice(4, 6), 16);
  if (!Number.isFinite(r) || !Number.isFinite(g) || !Number.isFinite(b)) return `rgba(0,0,0,${alpha})`;
  const a = Math.max(0, Math.min(1, alpha));
  return `rgba(${r},${g},${b},${a})`;
};

export type HorizonNodeVisualQuality = 'max' | 'balanced' | 'minimal';

export const setHorizonNodeVisualQuality = (q: HorizonNodeVisualQuality) => {
  if (typeof document === 'undefined') return;
  const next = q === 'max' ? 'max' : q === 'minimal' ? 'minimal' : 'balanced';
  document.documentElement.dataset.hznNodeVisuals = next;
  ensureHorizonNode2026Styles();
};

const getHorizonNodeVisualQuality = (): HorizonNodeVisualQuality => {
  if (typeof document === 'undefined') return 'balanced';
  const raw = String(document.documentElement.dataset.hznNodeVisuals || '').trim();
  if (raw === 'max' || raw === 'minimal') return raw;
  return 'balanced';
};

const ensureHorizonNode2026Styles = () => {
  if (typeof document === 'undefined') return;
  // Default to balanced if unset
  if (!document.documentElement.dataset.hznNodeVisuals) {
    document.documentElement.dataset.hznNodeVisuals = 'balanced';
  }
  const id = 'horizon-node-2026-styles';
  let style = document.getElementById(id) as HTMLStyleElement | null;
  if (!style) {
    style = document.createElement('style');
    style.id = id;
    document.head.appendChild(style);
  }
  // Always refresh content so HMR / incremental builds don't leave stale CSS around.
  style.textContent = `
    .hzn-node-2026 {
      position: relative;
      overflow: hidden;
      border-radius: 14px;
      background: var(--hzn-surface);
      border: 1px solid var(--hzn-border);
      box-shadow: var(--hzn-shadow);
      transition: box-shadow 140ms ease, border-color 140ms ease, opacity 140ms ease;
    }
    :root[data-hzn-node-visuals="minimal"] .hzn-node-2026 {
      box-shadow: none !important;
      transition: opacity 120ms ease;
    }
    :root[data-hzn-node-visuals="minimal"] .hzn-node-2026::before,
    :root[data-hzn-node-visuals="minimal"] .hzn-node-2026::after {
      display: none !important;
    }
    :root[data-hzn-node-visuals="minimal"] .hzn-node-2026 .hzn-metrics {
      display: none !important;
    }

    /* Max: a bit richer (still lightweight — no blur, no transforms). */
    :root[data-hzn-node-visuals="max"] .hzn-node-2026::before {
      content: "";
      position: absolute;
      inset: 0;
      border-radius: inherit;
      pointer-events: none;
      background:
        radial-gradient(140px 80px at 18% 6%, var(--hzn-accent-soft) 0%, rgba(0,0,0,0) 70%),
        linear-gradient(180deg, rgba(255,255,255,0.08) 0%, rgba(255,255,255,0.00) 50%);
      opacity: 0.9;
    }
    :root[data-hzn-node-visuals="max"] .hzn-node-2026::after {
      content: "";
      position: absolute;
      left: 0;
      right: 0;
      top: 0;
      height: 2px;
      pointer-events: none;
      background: linear-gradient(90deg, var(--hzn-accent) 0%, rgba(0,0,0,0) 60%);
      opacity: 0.9;
    }
    .hzn-node-2026.is-selected {
      box-shadow: 0 0 0 2px var(--hzn-accent-ring), 0 10px 28px rgba(0,0,0,0.18);
      border-color: var(--hzn-accent-ring);
    }
    .hzn-node-2026 .hzn-handle {
      opacity: 0;
      transition: opacity 120ms ease;
      pointer-events: none;
    }
    .hzn-node-2026:hover .hzn-handle {
      opacity: 1;
      pointer-events: auto;
    }
    .hzn-node-2026.is-selected .hzn-handle {
      opacity: 1;
      pointer-events: auto;
    }
    .hzn-node-2026 .hzn-plus {
      transition: transform 140ms ease, opacity 140ms ease, box-shadow 140ms ease;
      transform-origin: 50% 50%;
    }
    .hzn-node-2026:hover .hzn-plus,
    .hzn-node-2026.is-selected .hzn-plus {
      transform: none; /* keep stable position */
      box-shadow: 0 10px 22px rgba(0,0,0,0.18);
    }
  `;
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

const EDGE_GROUP_COLORS: Record<
  string,
  { light: string; dark: string; borderLight: string; borderDark: string }
> = {
  // Competition / conflict
  COMPETES_WITH: {
    light: 'rgba(239, 68, 68, 0.10)',
    dark: 'rgba(239, 68, 68, 0.12)',
    borderLight: 'rgba(239, 68, 68, 0.50)',
    borderDark: 'rgba(248, 113, 113, 0.55)',
  },
  CONFLICTS_WITH: {
    light: 'rgba(239, 68, 68, 0.10)',
    dark: 'rgba(239, 68, 68, 0.12)',
    borderLight: 'rgba(239, 68, 68, 0.50)',
    borderDark: 'rgba(248, 113, 113, 0.55)',
  },
  OPPOSES: {
    light: 'rgba(239, 68, 68, 0.10)',
    dark: 'rgba(239, 68, 68, 0.12)',
    borderLight: 'rgba(239, 68, 68, 0.50)',
    borderDark: 'rgba(248, 113, 113, 0.55)',
  },
  // Influence
  INFLUENCES: {
    light: 'rgba(249, 115, 22, 0.10)',
    dark: 'rgba(249, 115, 22, 0.12)',
    borderLight: 'rgba(249, 115, 22, 0.45)',
    borderDark: 'rgba(251, 146, 60, 0.55)',
  },
  AFFECTS: {
    light: 'rgba(249, 115, 22, 0.10)',
    dark: 'rgba(249, 115, 22, 0.12)',
    borderLight: 'rgba(249, 115, 22, 0.45)',
    borderDark: 'rgba(251, 146, 60, 0.55)',
  },
  REGULATES: {
    light: 'rgba(249, 115, 22, 0.10)',
    dark: 'rgba(249, 115, 22, 0.12)',
    borderLight: 'rgba(249, 115, 22, 0.45)',
    borderDark: 'rgba(251, 146, 60, 0.55)',
  },
  IMPACTS: {
    light: 'rgba(249, 115, 22, 0.10)',
    dark: 'rgba(249, 115, 22, 0.12)',
    borderLight: 'rgba(249, 115, 22, 0.45)',
    borderDark: 'rgba(251, 146, 60, 0.55)',
  },
  // Ownership / hierarchy
  OWNS: {
    light: 'rgba(59, 130, 246, 0.10)',
    dark: 'rgba(59, 130, 246, 0.12)',
    borderLight: 'rgba(59, 130, 246, 0.45)',
    borderDark: 'rgba(96, 165, 250, 0.55)',
  },
  MANAGES: {
    light: 'rgba(59, 130, 246, 0.10)',
    dark: 'rgba(59, 130, 246, 0.12)',
    borderLight: 'rgba(59, 130, 246, 0.45)',
    borderDark: 'rgba(96, 165, 250, 0.55)',
  },
  CONTROLS: {
    light: 'rgba(59, 130, 246, 0.10)',
    dark: 'rgba(59, 130, 246, 0.12)',
    borderLight: 'rgba(59, 130, 246, 0.45)',
    borderDark: 'rgba(96, 165, 250, 0.55)',
  },
  PARENT_OF: {
    light: 'rgba(59, 130, 246, 0.10)',
    dark: 'rgba(59, 130, 246, 0.12)',
    borderLight: 'rgba(59, 130, 246, 0.45)',
    borderDark: 'rgba(96, 165, 250, 0.55)',
  },
  SUBSIDIARY_OF: {
    light: 'rgba(59, 130, 246, 0.10)',
    dark: 'rgba(59, 130, 246, 0.12)',
    borderLight: 'rgba(59, 130, 246, 0.45)',
    borderDark: 'rgba(96, 165, 250, 0.55)',
  },
  // Default / other
  OTHER: {
    light: 'rgba(100, 116, 139, 0.08)',
    dark: 'rgba(148, 163, 184, 0.10)',
    borderLight: 'rgba(100, 116, 139, 0.35)',
    borderDark: 'rgba(148, 163, 184, 0.40)',
  },
};

const normalizeEdgeType = (t: string | undefined) => (t || '').toUpperCase().trim() || 'OTHER';

// Color scheme for semantic groups (business-oriented)
const SEMANTIC_GROUP_COLORS: Record<
  SemanticGroup,
  { light: string; dark: string; borderLight: string; borderDark: string; icon: string }
> = {
  'supply-chain': {
    light: 'rgba(59, 130, 246, 0.10)',
    dark: 'rgba(59, 130, 246, 0.12)',
    borderLight: 'rgba(59, 130, 246, 0.45)',
    borderDark: 'rgba(96, 165, 250, 0.55)',
    icon: '⬆️',  // Upstream
  },
  'customers': {
    light: 'rgba(34, 197, 94, 0.10)',
    dark: 'rgba(34, 197, 94, 0.12)',
    borderLight: 'rgba(34, 197, 94, 0.45)',
    borderDark: 'rgba(74, 222, 128, 0.55)',
    icon: '⬇️',  // Downstream
  },
  'market': {
    light: 'rgba(239, 68, 68, 0.10)',
    dark: 'rgba(239, 68, 68, 0.12)',
    borderLight: 'rgba(239, 68, 68, 0.45)',
    borderDark: 'rgba(248, 113, 113, 0.55)',
    icon: '⚔️',  // Competition/Partnership
  },
  'context': {
    light: 'rgba(168, 85, 247, 0.10)',
    dark: 'rgba(168, 85, 247, 0.12)',
    borderLight: 'rgba(168, 85, 247, 0.45)',
    borderDark: 'rgba(192, 132, 252, 0.55)',
    icon: '🌍',  // Geography/Context
  },
};

// Format edge types for subtitle (e.g., "OWNS, SUPPLIES_TO")
const formatEdgeTypes = (edgeTypes: string[] | undefined): string => {
  if (!edgeTypes || edgeTypes.length === 0) return '';
  // Dedupe and limit to 3 for display
  const unique = [...new Set(edgeTypes.map(t => normalizeEdgeType(t)))];
  if (unique.length <= 3) return unique.join(', ');
  return `${unique.slice(0, 2).join(', ')} +${unique.length - 2}`;
};

const HorizonGroupNode: React.FC<NodeProps<HorizonNodeData>> = React.memo(({ data, selected, id }) => {
  const { theme } = React.useContext(ThemeContext);
  const { t } = useTranslation();

  // Use semantic group if available, fall back to edge type
  const semanticGroup = data.groupSemanticGroup as SemanticGroup | undefined;
  const linearSection = (data as any).groupLinearSection as LinearSection | undefined;
  const edgeTypes = data.groupEdgeTypes || (data.groupEdgeType ? [data.groupEdgeType] : []);

  // Pagination data
  const currentPage = data.groupCurrentPage ?? 0;
  const totalPages = data.groupTotalPages ?? 1;
  const hasPagination = totalPages > 1;
  const onGroupPageChange = data.onGroupPageChange;

  // Get colors based on semantic group or fall back to edge type colors
  let bg: string;
  let border: string;
  let icon: string;

  if (semanticGroup && SEMANTIC_GROUP_COLORS[semanticGroup]) {
    const scheme = SEMANTIC_GROUP_COLORS[semanticGroup];
    bg = theme === 'dark' ? scheme.dark : scheme.light;
    border = theme === 'dark' ? scheme.borderDark : scheme.borderLight;
    icon = scheme.icon;
  } else {
    // Fallback to edge type colors
    const edgeType = normalizeEdgeType(data.groupEdgeType);
    const scheme = EDGE_GROUP_COLORS[edgeType] || EDGE_GROUP_COLORS.OTHER;
    bg = theme === 'dark' ? scheme.dark : scheme.light;
    border = theme === 'dark' ? scheme.borderDark : scheme.borderLight;
    icon = '';
  }

  const titleColor = theme === 'dark' ? 'rgba(255,255,255,0.85)' : 'rgba(15,23,42,0.85)';
  const subtitleColor = theme === 'dark' ? 'rgba(255,255,255,0.55)' : 'rgba(15,23,42,0.55)';
  const count = typeof data.groupCount === 'number' ? data.groupCount : undefined;
  const ring = selected ? (theme === 'dark' ? 'rgba(255,255,255,0.35)' : 'rgba(15,23,42,0.20)') : undefined;

  const semanticFallback: Record<SemanticGroup, string> = {
    'supply-chain': 'Supply Chain',
    'customers': 'Customers & Outputs',
    'market': 'Market Position',
    'context': 'Operating Context',
  };

  const linearFallback: Record<LinearSection, string> = {
    'top-left': 'Suppliers and Materials',
    'top-right': 'Regulators and Context',
    'bottom-left': 'Products and Ownership',
    'bottom-right': 'Market and Partners',
    'center': 'Central Nodes',
  };

  // Title priority:
  // 1) explicit override (rare; legacy)
  // 2) linear section id (translated via i18n)
  // 3) semantic group (translated via i18n)
  // 4) edge type fallback
  const title =
    data.groupTitle ||
    (linearSection
      ? t(`horizon.groups.linear.${linearSection}`, { defaultValue: linearFallback[linearSection] })
      : semanticGroup
        ? t(`horizon.groups.semantic.${semanticGroup}`, { defaultValue: semanticFallback[semanticGroup] })
        : normalizeEdgeType(data.groupEdgeType));
  // Subtitle: edge types in this group
  const subtitle = formatEdgeTypes(edgeTypes);

  // Arrow button styles
  const arrowButtonStyle: React.CSSProperties = {
    position: 'absolute',
    top: '50%',
    transform: 'translateY(-50%)',
    width: 24,
    height: 24,
    borderRadius: '50%',
    border: `1px solid ${border}`,
    background: theme === 'dark' ? 'rgba(0,0,0,0.4)' : 'rgba(255,255,255,0.9)',
    color: titleColor,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    fontSize: 14,
    fontWeight: 'bold',
    zIndex: 10,
    transition: 'all 0.15s ease',
  };

  const disabledArrowStyle: React.CSSProperties = {
    ...arrowButtonStyle,
    opacity: 0.3,
    cursor: 'not-allowed',
  };

  const handlePrevPage = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (currentPage > 0 && onGroupPageChange) {
      onGroupPageChange(id, currentPage - 1);
    }
  };

  const handleNextPage = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (currentPage < totalPages - 1 && onGroupPageChange) {
      onGroupPageChange(id, currentPage + 1);
    }
  };

  return (
    <div
      style={{
        width: '100%',
        height: '100%',
        background: bg,
        border: `1px dashed ${border}`,
        borderRadius: 12,
        boxShadow:
          (theme === 'dark' ? '0 2px 10px rgba(0,0,0,0.25)' : '0 2px 10px rgba(0,0,0,0.08)') +
          (ring ? `, 0 0 0 3px ${ring}` : ''),
        pointerEvents: 'auto',
        position: 'relative',
        overflow: 'hidden',
        cursor: 'grab',
      }}
    >
      {/* Header */}
      <div
        style={{
          position: 'absolute',
          left: 12,
          top: 10,
          right: 12,
          display: 'flex',
          alignItems: 'baseline',
          justifyContent: 'space-between',
          gap: 10,
          whiteSpace: 'nowrap',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0 }}>
          {icon && <span style={{ fontSize: 12 }}>{icon}</span>}
          <div style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.5px', color: titleColor }}>
            {title}
          </div>
          {count !== undefined && (
            <div style={{ fontSize: 11, fontWeight: 600, color: subtitleColor }}>
              ({count})
            </div>
          )}
        </div>
        {subtitle && (
          <div
            style={{
              fontSize: 9,
              fontWeight: 500,
              color: subtitleColor,
              textTransform: 'uppercase',
              letterSpacing: '0.3px',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              maxWidth: '50%',
            }}
            title={edgeTypes.join(', ')}
          >
            {subtitle}
          </div>
        )}
      </div>

      {/* Pagination Controls (Bottom) */}
      {hasPagination && (
        <div
          style={{
            position: 'absolute',
            bottom: 8,
            left: 0,
            right: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 12,
            pointerEvents: 'none', // Allow clicking through empty space
          }}
        >
          <button
            type="button"
            onClick={handlePrevPage}
            disabled={currentPage === 0}
            style={{
              ...arrowButtonStyle,
              position: 'static', // Override absolute from base style
              transform: 'none',
              pointerEvents: 'auto',
              width: 26,
              height: 26,
              opacity: currentPage === 0 ? 0.3 : 1,
              cursor: currentPage === 0 ? 'not-allowed' : 'pointer',
              background: theme === 'dark' ? 'rgba(0,0,0,0.6)' : 'rgba(255,255,255,0.95)',
              boxShadow: theme === 'dark' ? '0 2px 4px rgba(0,0,0,0.3)' : '0 1px 3px rgba(0,0,0,0.1)',
            }}
            title="Previous page"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </button>

          <span
            style={{
              fontSize: 11,
              fontWeight: 600,
              color: subtitleColor,
              letterSpacing: '0.3px',
              fontVariantNumeric: 'tabular-nums',
            }}
          >
            {currentPage + 1} / {totalPages}
          </span>

          <button
            type="button"
            onClick={handleNextPage}
            disabled={currentPage >= totalPages - 1}
            style={{
              ...arrowButtonStyle,
              position: 'static',
              transform: 'none',
              pointerEvents: 'auto',
              width: 26,
              height: 26,
              opacity: currentPage >= totalPages - 1 ? 0.3 : 1,
              cursor: currentPage >= totalPages - 1 ? 'not-allowed' : 'pointer',
              background: theme === 'dark' ? 'rgba(0,0,0,0.6)' : 'rgba(255,255,255,0.95)',
              boxShadow: theme === 'dark' ? '0 2px 4px rgba(0,0,0,0.3)' : '0 1px 3px rgba(0,0,0,0.1)',
            }}
            title="Next page"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M9 18l6-6-6-6" />
            </svg>
          </button>
        </div>
      )}
    </div>
  );
});

// Базовый узел для Horizon фрагмента
const HorizonFragmentNode: React.FC<NodeProps<HorizonNodeData>> = React.memo(({ data, selected }) => {
  ensureHorizonNode2026Styles();
  const { theme } = React.useContext(ThemeContext);
  const colors = getNodeColors(data.type, theme);
  
  const isCritical = data.critical;
  const isCollapsed = data.collapsed;
  const isExpanded = data.expanded;
  // Allow Research even if the first "load related" attempt returned empty: in that case the node isn't expanded,
  // but the user should still be able to run scouting from it.
  const canScopeScout = Boolean((isExpanded || data.loadRelatedAttempted) && data.onScopeScout && !data.scopeScoutDone);
  const isSelected = Boolean(selected);
  const canCollapse = Boolean(data.fragmentId && data.onCollapse && isExpanded && !data.isLoadingRelated);
  
  // 2026 visual style: neutral surface with tinted accent (less "dashboardy", more product-like)
  const accentColor = isCritical ? '#EF4444' : colors.accent;
  const textColor = theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.88)';
  const mutedTextColor = theme === 'dark' ? 'rgba(255,255,255,0.62)' : 'rgba(15,23,42,0.56)';
  const surfaceBg = theme === 'dark' ? 'rgba(17,24,39,0.90)' : 'rgba(255,255,255,0.96)';
  const surfaceBorder = theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)';
  const accentSoft = hexToRgba(accentColor, theme === 'dark' ? 0.22 : 0.16);
  const accentRing = hexToRgba(accentColor, theme === 'dark' ? 0.55 : 0.34);
  const shadowBase = theme === 'dark' ? '0 8px 22px rgba(0,0,0,0.30)' : '0 8px 22px rgba(0,0,0,0.10)';
  const criticalRing = hexToRgba('#EF4444', theme === 'dark' ? 0.55 : 0.35);
  const q = getHorizonNodeVisualQuality();

  const displayLabel = data.label || data.canonicalPath?.split('/').pop() || 'Fragment';
  const displayType = data.type || 'Unknown';
  // Keep node size consistent with ELK layout defaults used in the canvas (see LensCanvasCockpit)
  const NODE_WIDTH_PX = 180;
  const NODE_CARD_HEIGHT_PX = 148;
  const PLUS_BUTTON_SIZE_PX = 26;

  // Форматирование типа для отображения
  const formatType = (type: string) => {
    return type.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
  };

  return (
    <div
      className={`hzn-node-2026 ${isSelected ? 'is-selected' : ''}`}
      style={{
        // CSS variables for 2026 styles
        ['--hzn-surface' as any]: surfaceBg,
        ['--hzn-border' as any]: surfaceBorder,
        ['--hzn-accent' as any]: accentColor,
        ['--hzn-accent-soft' as any]: accentSoft,
        ['--hzn-accent-ring' as any]: isCritical ? criticalRing : accentRing,
        ['--hzn-shadow' as any]: isCritical
          ? `0 0 0 2px ${criticalRing}, ${shadowBase}`
          : (q === 'max' ? `${shadowBase}, 0 18px 60px rgba(0,0,0,0.18)` : shadowBase),
        // Reserve room at the bottom so the "+" button never overlaps metrics
        padding: isCollapsed ? '8px 12px 32px' : '10px 14px 40px',
        width: `${NODE_WIDTH_PX}px`,
        height: `${NODE_CARD_HEIGHT_PX}px`,
        boxSizing: 'border-box',
        position: 'relative',
        opacity: isCollapsed ? 0.7 : 1,
        cursor: 'default',
      }}
    >
      {/* Top handles: 2 handles для верхней группы (supply-chain) */}
      <Handle
        id="source-top"
        type="source"
        position={Position.Top}
        className="hzn-handle"
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${surfaceBg}`,
          top: '-5px',
          left: '40%',
        }}
      />
      <Handle
        id="target-top"
        type="target"
        position={Position.Top}
        className="hzn-handle"
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${surfaceBg}`,
          top: '-5px',
          right: '40%',
        }}
      />
      
      {/* Bottom handles: 2 handles для нижней группы (customers) */}
      <Handle
        id="source-bottom"
        type="source"
        position={Position.Bottom}
        className="hzn-handle"
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${surfaceBg}`,
          bottom: '-5px',
          left: '40%',
        }}
      />
      <Handle
        id="target-bottom"
        type="target"
        position={Position.Bottom}
        className="hzn-handle"
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${surfaceBg}`,
          bottom: '-5px',
          right: '40%',
        }}
      />
      
      {/* Left handles: 2 handles для левой группы (market) */}
      <Handle
        id="source-left"
        type="source"
        position={Position.Left}
        className="hzn-handle"
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${surfaceBg}`,
          left: '-5px',
          top: '40%',
        }}
      />
      <Handle
        id="target-left"
        type="target"
        position={Position.Left}
        className="hzn-handle"
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${surfaceBg}`,
          left: '-5px',
          top: '60%',
        }}
      />
      
      {/* Right handles: 2 handles для правой группы (context) */}
      <Handle
        id="source-right"
        type="source"
        position={Position.Right}
        className="hzn-handle"
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${surfaceBg}`,
          right: '-5px',
          top: '40%',
        }}
      />
      <Handle
        id="target-right"
        type="target"
        position={Position.Right}
        className="hzn-handle"
        style={{
          background: accentColor,
          width: '10px',
          height: '10px',
          border: `2px solid ${surfaceBg}`,
          right: '-5px',
          top: '60%',
        }}
      />

      {/* Заголовок с иконкой */}
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: '8px' }}>
        <div
          style={{
            padding: '4px',
            borderRadius: '6px',
            background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.08)'}`,
          }}
        >
          <NodeIcon type={data.type} color={accentColor} size={18} />
        </div>
        
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 13,
              fontWeight: 700,
              color: textColor,
              lineHeight: 1.3,
              overflow: 'hidden',
              display: '-webkit-box',
              WebkitBoxOrient: 'vertical' as any,
              WebkitLineClamp: 2 as any,
            }}
            title={displayLabel}
          >
            {displayLabel}
          </div>
          
          {!isCollapsed && (
            <div
              style={{
                fontSize: 10,
                color: mutedTextColor,
                marginTop: '2px',
                fontWeight: 650,
                textTransform: 'uppercase',
                letterSpacing: '0.5px',
              }}
            >
              {formatType(displayType)}
            </div>
          )}
        </div>
        
        {/* Right-side actions / indicators */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
          {canCollapse && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                if (data.fragmentId && data.onCollapse) {
                  data.onCollapse(data.fragmentId);
                }
              }}
              aria-label="Collapse"
              title="Collapse"
              style={{
                width: 22,
                height: 22,
                borderRadius: 8,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                background: theme === 'dark' ? 'rgba(0,0,0,0.25)' : 'rgba(255,255,255,0.65)',
                color: theme === 'dark' ? 'rgba(255,255,255,0.85)' : 'rgba(15,23,42,0.75)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                boxShadow: theme === 'dark' ? '0 1px 6px rgba(0,0,0,0.35)' : '0 1px 6px rgba(0,0,0,0.10)',
              }}
            >
              {/* Chevron-up icon (collapse) */}
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M18 15l-6-6-6 6" />
              </svg>
            </button>
          )}

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
                animation: 'horizon-pulse 2s infinite',
              }}
              title="Critical"
            />
          )}
        </div>
      </div>

      {/* Метрики (если не collapsed) */}
      {!isCollapsed && data.state && Object.keys(data.state).length > 0 && (
        <div
          className="hzn-metrics"
          style={{
            marginTop: '8px',
            paddingTop: '8px',
            borderTop: `1px solid ${theme === 'dark' ? 'rgba(255, 255, 255, 0.12)' : 'rgba(15, 23, 42, 0.10)'}`,
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
                    color: mutedTextColor,
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
                      boxShadow: `0 0 0 2px ${theme === 'dark' ? 'rgba(0,0,0,0.22)' : 'rgba(255,255,255,0.75)'}`,
                    }}
                    title={value >= 0.7 ? 'Good' : value >= 0.4 ? 'Medium' : 'Low'}
                  />
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Кнопка для расширения ( + ) и доп. Research (🔎) */}
      {data.fragmentId && data.onLoadRelated && (
        <div
          style={{
            position: 'absolute',
            bottom: '0px',
            left: '50%',
            // Place the CENTER of the circle exactly on the bottom border line of the card.
            transform: 'translate(-50%, 50%)',
            zIndex: 20,
          }}
        >
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              if (!data.fragmentId) return;
              if (data.isLoadingRelated || data.isScopeScouting) return;

              // After the node has been expanded, the same button becomes "additional research" (scope+scout depth=1)
              if (canScopeScout && data.onScopeScout) {
                data.onScopeScout(data.fragmentId, data.canonicalPath);
                return;
              }

              if (data.onLoadRelated) {
                data.onLoadRelated(data.fragmentId);
              }
            }}
            disabled={data.isLoadingRelated || data.isScopeScouting}
            className="hzn-plus"
            style={{
              width: `${PLUS_BUTTON_SIZE_PX}px`,
              height: `${PLUS_BUTTON_SIZE_PX}px`,
              borderRadius: '50%',
              border: `2px solid ${accentColor}`,
              background: surfaceBg,
              color: accentColor,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              cursor: (data.isLoadingRelated || data.isScopeScouting) ? 'wait' : 'pointer',
              fontSize: '16px',
              fontWeight: 'bold',
              lineHeight: 1,
              padding: 0,
              boxShadow: theme === 'dark' ? '0 8px 22px rgba(0,0,0,0.35)' : '0 8px 22px rgba(0,0,0,0.14)',
              opacity: (data.isLoadingRelated || data.isScopeScouting) ? 0.6 : 1,
              transition: 'all 0.2s ease',
            }}
            title={canScopeScout ? 'Research (add to scope + scout)' : 'Load related fragments'}
          >
            {(data.isLoadingRelated || data.isScopeScouting) ? (
              <span style={{ fontSize: '12px' }}>⏳</span>
            ) : canScopeScout ? (
              // Research icon (magnifier)
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <circle cx="11" cy="11" r="7" />
                <path d="M21 21l-4.3-4.3" />
              </svg>
            ) : '+'}
          </button>
        </div>
      )}

      {/* CSS для анимации пульса */}
    </div>
  );
});

// Экспортируем типы нод для Horizon графа
export const HORIZON_NODE_TYPES: NodeTypes = Object.freeze({
  horizonFragment: HorizonFragmentNode,
  horizonGroup: HorizonGroupNode,
});

import React from 'react';
import { NodeTypes, Handle, Position, NodeProps, NodeToolbar } from 'reactflow';
import { useTranslation } from 'react-i18next';
import { ThemeContext } from './ThemeProvider';
import type { SemanticGroup, LinearSection } from '../lib/elkLayout';
import { humanizeCanonicalPath } from '../lib/uiUtils';

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
  /** UI: whether to render metrics inside the node (disabled by default for performance). */
  showMetrics?: boolean;
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
  // Collapsible subcategories (Left pane: subcategories by relation type)
  // Each subcategory (relBlock) can be collapsed into a compact stack representation.
  // Key format: `${groupId}:${edgeType}` or just edgeType within the group.
  groupCollapsedSubcategories?: Set<string> | string[];  // Edge types that are collapsed in this group
  onToggleSubcategoryCollapse?: (groupId: string, edgeType: string, nextCollapsed: boolean) => void;
  // Right-side temporary stacking (Levels/Hierarchical UX)
  // When many leaf right-nodes form a tall vertical column, we can collapse them into a single "stack"
  // and expand on demand. These fields are view-only (not persisted).
  stackGroupKey?: string; // stable key for the stack group
  stackCount?: number; // total nodes in the stack
  stackExpanded?: boolean; // whether the stack is currently expanded
  // Named stack controllers (Levels right pane): semantic stacks (Persons/Products/Control/Geolocation)
  stackTitle?: string;
  /** Stack subtitle (context label): e.g. "Dubai Future Foundation" under "Control" */
  stackSubtitle?: string;
  stackKind?: 'persons' | 'staff' | 'products' | 'control' | 'geolocation' | 'suppliers' | 'dependencies';
  /** Visual type override for the stack controller node (drives color/icon); e.g. PERSON/PRODUCT/ORGANIZATION/GEO_REGION */
  stackVisualType?: string;
  onToggleStack?: (stackKey: string, nextExpanded: boolean) => void;
  // Levels two-pane metadata (view-only)
  levelsPane?: 'right' | 'left' | 'dock';
  hiddenByStack?: boolean;
  // Right-side contextual filters (Levels layout; view-only)
  rightFiltersOpen?: boolean;
  rightFilterHideLoneParents?: boolean;
  rightFilterHideLoneChildren?: boolean;
  rightFilterHideLoneGovernancePeople?: boolean;
  onOpenRightFilters?: (nodeId: string, nextOpen: boolean) => void;
  onToggleRightFilter?: (nodeId: string, key: 'loneParents' | 'loneChildren' | 'loneGovernancePeople', nextOn: boolean) => void;
  // Right-side node actions (Levels layout; view-only)
  onTrace?: (nodeId: string) => void;
  onUndoTrace?: () => void;
  canUndoTrace?: boolean;
  // Local relation filters (Levels layout; per-node toggle to show/hide neighbors by edge type)
  localFiltersOpen?: boolean;
  /** Map of edge type → visible (true = show, false = hide neighbors with this edge type). */
  localFilterState?: Record<string, boolean>;
  /** List of edge types available for this node (derived from current levelsExpandMode). */
  localFilterEdgeTypes?: string[];
  onOpenLocalFilters?: (nodeId: string, nextOpen: boolean) => void;
  onToggleLocalFilter?: (nodeId: string, edgeType: string, nextVisible: boolean) => void;
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
  'SUPPLIER': {
    bg: '#ECFDF5', bgDark: '#022C22',
    border: '#22C55E', borderDark: '#4ADE80',
    text: '#166534', textDark: '#BBF7D0',
    accent: '#22C55E', accentDark: '#4ADE80',
  },
  'DEPENDENCY': {
    // Neutral grey (distinct from default, but still consistent with platform palette)
    bg: '#F1F5F9', bgDark: '#0F172A',
    border: '#94A3B8', borderDark: '#94A3B8',
    text: '#334155', textDark: '#CBD5E1',
    accent: '#94A3B8', accentDark: '#CBD5E1',
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
  'MEDIA_CHANNEL': {
    bg: '#E0E7FF', bgDark: '#1E1B4B',
    border: '#6366F1', borderDark: '#818CF8',
    text: '#3730A3', textDark: '#C7D2FE',
    accent: '#6366F1', accentDark: '#818CF8',
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
export const getNodeColors = (type: string | undefined, theme: 'light' | 'dark'): {
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

// Track whether styles have been injected (avoids DOM thrashing on every node render)
const HZN_NODE_2026_CSS_VERSION = '2026-10';

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
  // Skip if already up-to-date (allows hot-reload updates without per-render thrash)
  if ((style as any).dataset?.hznNode2026V === HZN_NODE_2026_CSS_VERSION) return;
  (style as any).dataset.hznNode2026V = HZN_NODE_2026_CSS_VERSION;
  // Inject styles once
  style.textContent = `
    @keyframes hzn-spin {
      from { transform: rotate(0deg); }
      to { transform: rotate(360deg); }
    }

    .hzn-node-2026 {
      position: relative;
      overflow: hidden;
      border-radius: 14px;
      background: var(--hzn-surface);
      border: 1px solid var(--hzn-border);
      box-shadow: var(--hzn-shadow);
      transition: box-shadow 140ms ease, border-color 140ms ease, opacity 140ms ease;
      /* CSS containment for better performance.
         IMPORTANT: do NOT use paint here — nodes intentionally render UI that can extend outside
         the card bounds (stack previews, bottom controls). paint would clip them. */
      contain: layout style;
    }

    /* Stack controllers (containers): visually distinct from entity nodes.
       Goal: fast role recognition without adding noisy chrome. */
    .hzn-node-2026.is-stack {
      /* Stack controllers (group-like nodes): calm, single-surface card.
         No backplates, no inner chrome. Border width matches selected edge weight. */
      border-radius: 14px;
      border: 2px solid var(--hzn-stack-border, rgba(148,163,184,0.26));
    }
    .hzn-node-2026.is-stack::after { display: none; }
    [data-theme="light"] .hzn-node-2026.is-stack {
      border-color: rgba(100,116,139,0.26);
    }
    [data-theme="light"] .hzn-node-2026.is-stack::after { display: none; }
    /* Perf mode: disable all transitions and heavy effects for smoother pan/zoom on large graphs */
    :root[data-hzn-perf-mode="true"] .hzn-node-2026 {
      transition: none !important;
      box-shadow: none !important;
    }
    :root[data-hzn-perf-mode="true"] .hzn-node-2026.is-selected {
      /* Keep minimal selection indicator */
      box-shadow: 0 0 0 2px var(--hzn-accent-ring) !important;
    }
    :root[data-hzn-perf-mode="true"] .hzn-node-2026 .hzn-handle,
    :root[data-hzn-perf-mode="true"] .hzn-node-2026 .hzn-plus {
      transition: none !important;
      box-shadow: none !important;
    }
    :root[data-hzn-perf-mode="true"] .react-flow__edge path {
      transition: none !important;
    }
    :root[data-hzn-perf-mode="true"] .hzn-node-2026::before {
      /* Disable gradient overlay in perf mode */
      display: none !important;
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
    /* Stack/group nodes must stay calm: solid fill, no top overlay gradients. */
    :root[data-hzn-node-visuals="max"] .hzn-node-2026.is-stack::before {
      display: none !important;
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

// Initialize styles immediately on module load (avoids per-render checks)
if (typeof document !== 'undefined') {
  ensureHorizonNode2026Styles();
}

export const formatHorizonFragmentType = (type: string | undefined): string => {
  const t = String(type || '').trim();
  if (!t) return 'Unknown';
  return t.replace(/_/g, ' ').toLowerCase().replace(/\b\w/g, (c) => c.toUpperCase());
};

// SVG иконки для разных типов
export const HorizonNodeIcon: React.FC<{ type: string | undefined; color: string; size?: number }> = ({ type, color, size = 16 }) => {
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
    
    case 'MEDIA_CHANNEL':
      // Broadcast / megaphone icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 11v2a2 2 0 0 0 2 2h2l7 4V3L7 7H5a2 2 0 0 0-2 2z" />
          <path d="M16 8.5a4 4 0 0 1 0 7" />
          <path d="M19 6a8 8 0 0 1 0 12" />
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
    
    case 'SUPPLIER':
      // Truck icon (delivery/supply)
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M14 18V6a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2v11a1 1 0 0 0 1 1h2" />
          <path d="M15 18h2a1 1 0 0 0 1-1v-3.28a1 1 0 0 0-.684-.948l-1.923-.641a1 1 0 0 1-.684-.949V8a1 1 0 0 1 1-1h1.382a1 1 0 0 1 .894.553l1.448 2.894A1 1 0 0 0 20.382 11H22" />
          <circle cx="7" cy="18" r="2" />
          <path d="M22 18h-4" />
          <circle cx="19" cy="18" r="2" />
        </svg>
      );
    
    case 'DEPENDENCY':
      // Link / dependency icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M10 13a5 5 0 0 1 0-7l1-1a5 5 0 0 1 7 7l-1 1" />
          <path d="M14 11a5 5 0 0 1 0 7l-1 1a5 5 0 0 1-7-7l1-1" />
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
export const getMetricColor = (key: string, value: number): string => {
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

/** Get edge stroke color by edge type (matches getEdgeStyle in LensCanvasCockpit) */
export const getEdgeColor = (edgeType: string | undefined): string => {
  const type = (edgeType || '').toUpperCase();
  // Competition/Conflict — Red
  if (['COMPETES_WITH', 'CONFLICTS_WITH', 'OPPOSES'].includes(type)) return '#EF4444';
  // Partnership/Cooperation — Green
  if (['PARTNERS_WITH', 'COOPERATES_WITH', 'SUPPLIES_TO', 'SUPPORTS'].includes(type)) return '#22C55E';
  // Influence — Orange
  if (['INFLUENCES', 'AFFECTS', 'REGULATES', 'IMPACTS'].includes(type)) return '#F97316';
  // Ownership — Blue
  if (['OWNS', 'INVESTS_IN', 'PARENT_OF', 'SUBSIDIARY_OF'].includes(type)) return '#3B82F6';
  // Control/Management — Indigo
  if (['CONTROLS', 'MANAGES'].includes(type)) return '#6366F1';
  // Governance — Indigo (lighter)
  if (['BOARD_MEMBER_OF', 'EMPLOYED_BY', 'FOUNDED'].includes(type)) return '#6366F1';
  // Dependency — Gray
  if (['DEPENDS_ON', 'REQUIRES', 'USES', 'NEEDS'].includes(type)) return '#9CA3AF';
  // Location — Purple
  if (['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'AUDITED_BY'].includes(type)) return '#A855F7';
  // Social graph (people-people) — Teal
  if (['RELATED_TO', 'RELATIVE_OF'].includes(type)) return '#06B6D4';
  // Default — Slate
  return '#64748B';
};

const EdgeTypeIcon = ({ edgeType, color, size = 16 }: { edgeType: string; color: string; size?: number }) => {
  const et = (edgeType || '').toUpperCase().trim();
  const common = {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: color,
    strokeWidth: 2.5,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
  };

  // IMPORTANT: icons must be unique per edge type (no shared SVGs), otherwise toggles are ambiguous.
  switch (et) {
    // Context / location
    case 'LOCATED_IN':
      return (
        <svg {...common}>
          <path d="M12 21s7-4.5 7-11a7 7 0 1 0-14 0c0 6.5 7 11 7 11z" />
          <circle cx="12" cy="10" r="2.5" />
        </svg>
      );
    case 'OPERATES_IN':
      return (
        <svg {...common}>
          <path d="M12 21s7-4.5 7-11a7 7 0 1 0-14 0c0 6.5 7 11 7 11z" />
          <rect x="10.5" y="8.5" width="3" height="3" rx="0.6" />
        </svg>
      );
    case 'BASED_IN':
      return (
        <svg {...common}>
          <path d="M12 21s7-4.5 7-11a7 7 0 1 0-14 0c0 6.5 7 11 7 11z" />
          <path d="M9.5 11.5h5" />
        </svg>
      );
    case 'HEADQUARTERED_IN':
      return (
        <svg {...common}>
          <path d="M12 21s7-4.5 7-11a7 7 0 1 0-14 0c0 6.5 7 11 7 11z" />
          <path d="M10 8v6" />
          <path d="M10 8h4l-1 1 1 1h-4" />
        </svg>
      );
    case 'REGULATES':
      return (
        <svg {...common}>
          <path d="M14 4l6 6" />
          <path d="M13 5l-2 2 6 6 2-2" />
          <path d="M9 9l-5 5" />
          <path d="M8 10l6 6" />
          <path d="M4 20h8" />
        </svg>
      );

    // Dependencies / usage
    case 'DEPENDS_ON':
      return (
        <svg {...common}>
          <path d="M10 13a5 5 0 0 1 0-7l1.5-1.5a5 5 0 0 1 7 7L17 13" />
          <path d="M14 11a5 5 0 0 1 0 7L12.5 19.5a5 5 0 0 1-7-7L7 11" />
        </svg>
      );
    case 'REQUIRES':
      return (
        <svg {...common}>
          <rect x="7" y="11" width="10" height="9" rx="2" />
          <path d="M9 11V8a3 3 0 0 1 6 0v3" />
        </svg>
      );
    case 'NEEDS':
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="9" />
          <path d="M12 7v6" />
          <path d="M12 17h.01" />
        </svg>
      );
    case 'USES':
      return (
        <svg {...common}>
          <path d="M14 7a4 4 0 0 0-5 5l-5 5 2 2 5-5a4 4 0 0 0 5-5l-2 2-2-2z" />
        </svg>
      );
    case 'CONSUMES':
      return (
        <svg {...common}>
          <path d="M12 3v10" />
          <path d="M8 9l4 4 4-4" />
          <path d="M7 21v-4h10v4" />
          <path d="M5 21h14" />
        </svg>
      );
    case 'TRANSFORMS_INTO':
      return (
        <svg {...common}>
          <path d="M16 3h4v4" />
          <path d="M20 3l-5 5" />
          <path d="M8 21H4v-4" />
          <path d="M4 21l5-5" />
          <path d="M7 7h6" />
          <path d="M11 17h6" />
        </svg>
      );

    // Ownership / control
    case 'OWNS':
      return (
        <svg {...common}>
          <circle cx="8.5" cy="12" r="3" />
          <path d="M11.5 12H21" />
          <path d="M18 12v3" />
          <path d="M16 12v2" />
        </svg>
      );
    case 'PARENT_OF':
      return (
        <svg {...common}>
          <path d="M12 5v6" />
          <path d="M6 9h12" />
          <path d="M12 11l-4 4" />
          <path d="M12 11l4 4" />
          <path d="M8 21v-3" />
          <path d="M16 21v-3" />
        </svg>
      );
    case 'SUBSIDIARY_OF':
      return (
        <svg {...common}>
          <rect x="6" y="3" width="12" height="6" rx="1.5" />
          <path d="M12 21v-6" />
          <path d="M8 15l4-4 4 4" />
        </svg>
      );
    case 'INVESTS_IN':
      return (
        <svg {...common}>
          <path d="M3 17l6-6 4 4 7-7" />
          <path d="M14 8h6v6" />
        </svg>
      );
    case 'CONTROLS':
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="7" />
          <circle cx="12" cy="12" r="2" />
          <path d="M12 2v3" />
          <path d="M12 19v3" />
          <path d="M2 12h3" />
          <path d="M19 12h3" />
        </svg>
      );
    case 'MANAGES':
      return (
        <svg {...common}>
          <rect x="7" y="4" width="10" height="16" rx="2" />
          <path d="M10 4v2h4V4" />
          <path d="M9.5 9h5" />
          <path d="M9.5 13h5" />
          <path d="M9.5 17h5" />
        </svg>
      );

    // Governance / people
    case 'BOARD_MEMBER_OF':
      return (
        <svg {...common}>
          <path d="M15 7a3 3 0 1 1-6 0a3 3 0 0 1 6 0z" />
          <path d="M5 21a7 7 0 0 1 14 0" />
          <path d="M19 8v4" />
          <path d="M17 10h4" />
        </svg>
      );
    case 'EXECUTIVE_OF':
      return (
        <svg {...common}>
          <path d="M15 7a3 3 0 1 1-6 0a3 3 0 0 1 6 0z" />
          <path d="M5 21a7 7 0 0 1 14 0" />
          <path d="M18 6l.7 1.4 1.6.2-1.15 1.1.27 1.6L18 9.6l-1.42.75.27-1.6-1.15-1.1 1.6-.2z" />
        </svg>
      );
    case 'EMPLOYED_BY':
      return (
        <svg {...common}>
          <rect x="6" y="7" width="12" height="10" rx="2" />
          <path d="M9 7V5h6v2" />
          <path d="M6 12h12" />
        </svg>
      );
    case 'FOUNDED':
      return (
        <svg {...common}>
          <path d="M6 3v18" />
          <path d="M6 4h11l-2 3 2 3H6" />
        </svg>
      );

    // Supply chain
    case 'PRODUCES':
      return (
        <svg {...common}>
          <path d="M3 21V11l6-3v3l6-3v5h6v8H3z" />
          <path d="M7 21v-4" />
          <path d="M11 21v-4" />
        </svg>
      );
    case 'SUPPLIES_TO':
      return (
        <svg {...common}>
          <path d="M3 7h11v10H3z" />
          <path d="M14 10h4l3 3v4h-7" />
          <circle cx="7" cy="19" r="1.5" />
          <circle cx="18" cy="19" r="1.5" />
        </svg>
      );
    case 'SUPPORTS':
      return (
        <svg {...common}>
          <path d="M12 21s-7-4.5-9-9a5 5 0 0 1 9-3a5 5 0 0 1 9 3c-2 4.5-9 9-9 9z" />
        </svg>
      );

    // Market relations
    case 'PARTNERS_WITH':
      return (
        <svg {...common}>
          <path d="M8 12l2 2 6-6" />
          <path d="M7 7h10" />
          <path d="M7 17h10" />
          <path d="M6 12h2" />
          <path d="M16 12h2" />
        </svg>
      );
    case 'COOPERATES_WITH':
      return (
        <svg {...common}>
          <path d="M15 7a3 3 0 1 1-6 0a3 3 0 0 1 6 0z" />
          <path d="M5 21a7 7 0 0 1 14 0" />
          <path d="M6 10a2.5 2.5 0 0 1 3-2" />
          <path d="M18 10a2.5 2.5 0 0 0-3-2" />
        </svg>
      );
    case 'RELATED_TO':
      return (
        <svg {...common}>
          <path d="M10 10a2.5 2.5 0 1 1-5 0a2.5 2.5 0 0 1 5 0z" />
          <path d="M19 10a2.5 2.5 0 1 1-5 0a2.5 2.5 0 0 1 5 0z" />
          <path d="M2.8 21a5.8 5.8 0 0 1 10.4 0" />
          <path d="M10.8 21a5.8 5.8 0 0 1 10.4 0" />
        </svg>
      );
    case 'RELATIVE_OF':
      return (
        <svg {...common}>
          <path d="M10 10a2.5 2.5 0 1 1-5 0a2.5 2.5 0 0 1 5 0z" />
          <path d="M19 10a2.5 2.5 0 1 1-5 0a2.5 2.5 0 0 1 5 0z" />
          <path d="M2.8 21a5.8 5.8 0 0 1 10.4 0" />
          <path d="M10.8 21a5.8 5.8 0 0 1 10.4 0" />
        </svg>
      );
    case 'SIMILAR_TO':
      return (
        <svg {...common}>
          <path d="M7 10h10" />
          <path d="M7 14h10" />
        </svg>
      );
    case 'LINKED_TO':
      return (
        <svg {...common}>
          <path d="M14 3h7v7" />
          <path d="M10 14L21 3" />
          <path d="M5 7v14h14v-5" />
        </svg>
      );
    case 'COMPETES_WITH':
      return (
        <svg {...common}>
          <path d="M7 8h10" />
          <path d="M7 8l2-2" />
          <path d="M7 8l2 2" />
          <path d="M17 16H7" />
          <path d="M17 16l-2-2" />
          <path d="M17 16l-2 2" />
        </svg>
      );
    case 'CONFLICTS_WITH':
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="7" />
          <path d="M8 16l8-8" />
        </svg>
      );
    case 'OPPOSES':
      return (
        <svg {...common}>
          <path d="M7 12h10" />
          <path d="M7 12l2-2" />
          <path d="M7 12l2 2" />
          <path d="M17 12l-2-2" />
          <path d="M17 12l-2 2" />
        </svg>
      );

    // Influence
    case 'INFLUENCES':
      return (
        <svg {...common}>
          <path d="M4 12c2-4 4-4 6 0s4 4 10 0" />
          <path d="M18 8l2 4-2 4" />
        </svg>
      );
    case 'AFFECTS':
      return (
        <svg {...common}>
          <path d="M13 2L4 14h7l-1 8 9-12h-7z" />
        </svg>
      );
    case 'IMPACTS':
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="7" />
          <path d="M12 5v4" />
          <path d="M16 8l-4 4" />
        </svg>
      );
    default:
      break;
  }

  // Default: dot
  return (
    <svg {...common}>
      <circle cx="12" cy="12" r="4" />
    </svg>
  );
};

const edgeCaptionShort = (raw: any): string => {
  const t = String(raw || '').trim();
  if (!t) return 'Relation';
  const u = t.toUpperCase();
  const MAP: Record<string, string> = {
    // Dependency / usage
    DEPENDS_ON: 'Depends',
    REQUIRES: 'Requires',
    NEEDS: 'Needs',
    USES: 'Uses',
    CONSUMES: 'Consumes',
    TRANSFORMS_INTO: 'Transforms',
    // Ownership / control
    OWNS: 'Owns',
    MANAGES: 'Manages',
    CONTROLS: 'Controls',
    INVESTS_IN: 'Invests',
    PARENT_OF: 'Parent',
    SUBSIDIARY_OF: 'Subsidiary',
    EMPLOYED_BY: 'Employed',
    BOARD_MEMBER_OF: 'Board',
    EXECUTIVE_OF: 'Executive',
    FOUNDED: 'Founded',
    // Cooperation / relationships
    PARTNERS_WITH: 'Partners',
    COOPERATES_WITH: 'Cooperates',
    SUPPORTS: 'Supports',
    SUPPLIES_TO: 'Supplies',
    PRODUCES: 'Produces',
    AUDITED_BY: 'Audited',
    RELATED_TO: 'Related',
    RELATIVE_OF: 'Relatives',
    SIMILAR_TO: 'Similar',
    LINKED_TO: 'Linked',
    // Location / context
    LOCATED_IN: 'Located',
    OPERATES_IN: 'Operates',
    BASED_IN: 'Based',
    HEADQUARTERED_IN: 'HQ',
    // Competition / conflict
    COMPETES_WITH: 'Competes',
    CONFLICTS_WITH: 'Conflicts',
    OPPOSES: 'Opposes',
    // Influence / regulation
    INFLUENCES: 'Influences',
    AFFECTS: 'Affects',
    REGULATES: 'Regulates',
    IMPACTS: 'Impacts',
  };
  if (MAP[u]) return MAP[u];
  const human = u
    .replace(/_WITH$/g, '')
    .replace(/_TO$/g, '')
    .replace(/_OF$/g, '')
    .replace(/_/g, ' ')
    .toLowerCase()
    .replace(/\b\w/g, (c) => c.toUpperCase());
  return human.length > 18 ? `${human.slice(0, 18)}…` : human;
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
  // Groups are primarily visual containers; they must NOT block clicks on child nodes.
  // We only treat a group as interactive if explicitly requested (rare).
  const isBackdrop = Boolean((data as any)?.groupIsBackdrop);
  const interactive = Boolean((data as any)?.groupInteractive);

  // Use semantic group if available, fall back to edge type
  const semanticGroup = data.groupSemanticGroup as SemanticGroup | undefined;
  const linearSection = (data as any).groupLinearSection as LinearSection | undefined;
  const edgeTypes = data.groupEdgeTypes || (data.groupEdgeType ? [data.groupEdgeType] : []);

  // Pagination data
  const currentPage = data.groupCurrentPage ?? 0;
  const totalPages = data.groupTotalPages ?? 1;
  const hasPagination = totalPages > 1;
  const onGroupPageChange = data.onGroupPageChange;
  const hideHeader = Boolean((data as any)?.groupHideHeader);
  const noFill = Boolean((data as any)?.groupNoFill);

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

  // In some layouts (e.g. Levels Two-Pane), groups are purely structural containers.
  // Best practice: no semantic fill, only a subtle outline.
  if (noFill) {
    bg = 'transparent';
    border = theme === 'dark' ? 'rgba(148, 163, 184, 0.28)' : 'rgba(100, 116, 139, 0.22)';
  }

  const titleColor = theme === 'dark' ? 'rgba(255,255,255,0.85)' : 'rgba(15,23,42,0.85)';
  const subtitleColor = theme === 'dark' ? 'rgba(255,255,255,0.55)' : 'rgba(15,23,42,0.55)';
  const count = typeof data.groupCount === 'number' ? data.groupCount : undefined;
  const ring = selected ? (theme === 'dark' ? 'rgba(255,255,255,0.35)' : 'rgba(15,23,42,0.20)') : undefined;

  // Optional: intra-group subsections (used by Levels Two-Pane left groups to separate children by fragment type).
  const typeBlocks = (data as any)?.groupTypeBlocks as
    | Array<{
        typeKey: string;
        label: string;
        startX: number;
        dividerX?: number;
        blockW?: number;
        relationBlocks?: Array<{
          label: string;
          startX: number;
          blockW: number;
          dividerX?: number;
          edgeTypeKey?: string;
          nodeCount?: number;
          collapsed?: boolean;
          firstNodeId?: string;
        }>;
        relationLabelY?: number;
      }>
    | undefined;
  const showTypeBlocks = Array.isArray(typeBlocks) && typeBlocks.length > 1;
  const showRelationBlocks =
    Array.isArray(typeBlocks) &&
    typeBlocks.some((b) => Array.isArray((b as any)?.relationBlocks) && ((b as any).relationBlocks as any[]).length > 1);

  // Levels Two-Pane: ports for aggregated edges (one per edge type).
  const edgePorts = (data as any)?.groupEdgePorts as
    | Array<{ edgeType: string; targetHandleId: string; sourceHandleId: string; y: number }>
    | undefined;
  const showEdgePorts = Array.isArray(edgePorts) && edgePorts.length > 0;

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
        boxShadow: (noFill && !ring)
          ? 'none'
          : ((theme === 'dark' ? '0 2px 10px rgba(0,0,0,0.25)' : '0 2px 10px rgba(0,0,0,0.08)') +
            (ring ? `, 0 0 0 3px ${ring}` : '')),
        // Groups are real containers in Freeform/Linear pagination modes; they must allow interaction
        // (pagination buttons). Backdrop-only groups should not intercept clicks.
        pointerEvents: isBackdrop ? 'none' : 'auto',
        position: 'relative',
        overflow: 'hidden',
        cursor: interactive ? 'grab' : 'default',
      }}
    >
      {/* Optional: Type subsections (visual separators + labels). Keep pointerEvents off. */}
      {showTypeBlocks && (
        <>
          {typeBlocks!.map((b, idx) => {
            if (typeof b?.dividerX !== 'number') return null;
            return (
              <div
                key={`type-divider-${idx}`}
                style={{
                  position: 'absolute',
                  left: b.dividerX,
                  top: 6,
                  bottom: 6,
                  width: 1,
                  borderLeft: `1px dashed ${border}`,
                  opacity: theme === 'dark' ? 0.28 : 0.22,
                  pointerEvents: 'none',
                }}
              />
            );
          })}
          {typeBlocks!.map((b, idx) => {
            const left = typeof b?.startX === 'number' ? b.startX : 12;
            const maxWidth = typeof b?.blockW === 'number' ? b.blockW : 240;
            return (
              <div
                key={`type-label-${idx}`}
                style={{
                  position: 'absolute',
                  left,
                  top: 4,
                  maxWidth,
                  fontSize: 10,
                  fontWeight: 700,
                  color: subtitleColor,
                  textTransform: 'uppercase',
                  letterSpacing: '0.3px',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                  pointerEvents: 'none',
                }}
                title={String(b?.label || b?.typeKey || '')}
              >
                {String(b?.label || b?.typeKey || '')}
              </div>
            );
          })}
        </>
      )}

      {/* Optional: Relation subsections (within a type block).
          IMPORTANT: render even when there is only ONE type block (typeBlocks.length === 1). */}
      {showRelationBlocks && Array.isArray(typeBlocks) && (
        <>
          {/* Subcategory dividers (between relation blocks): subtle, like type dividers but quieter (2025 UX). */}
          {typeBlocks.flatMap((b, bIdx) => {
            const rels = Array.isArray(b?.relationBlocks) ? (b.relationBlocks as any[]) : [];
            if (!rels || rels.length <= 1) return [];
            return rels
              .map((rb: any, rIdx: number) => ({ rb, rIdx }))
              .filter(({ rb }) => typeof rb?.dividerX === 'number')
              .map(({ rb, rIdx }) => (
                <div
                  key={`rel-divider-${bIdx}-${rIdx}`}
                  style={{
                    position: 'absolute',
                    left: rb.dividerX,
                    top: 6,
                    bottom: 6,
                    width: 1,
                    borderLeft: `1px dashed ${border}`,
                    opacity: theme === 'dark' ? 0.18 : 0.14,
                    pointerEvents: 'none',
                  }}
                />
              ));
          })}

          {typeBlocks.flatMap((b, bIdx) => {
            const rels = Array.isArray(b?.relationBlocks) ? b.relationBlocks : [];
            const y = typeof b?.relationLabelY === 'number' ? b.relationLabelY : undefined;
            if (!rels || rels.length <= 1 || typeof y !== 'number') return [];
            return rels.map((rb, rIdx) => {
              const left = typeof rb?.startX === 'number' ? rb.startX : (typeof b?.startX === 'number' ? b.startX : 12);
              const w = typeof rb?.blockW === 'number' ? rb.blockW : 80;
              const label = String(rb?.label || '').trim();
              const edgeTypeKey = String((rb as any)?.edgeTypeKey || '').trim();
              const firstNodeId = String((rb as any)?.firstNodeId || '').trim();
              const groupIdForToggle = String((data as any)?.groupId || id).trim();
              const canToggle = Boolean(edgeTypeKey && (data as any)?.onToggleSubcategoryCollapse);
              const isCollapsed = Boolean((rb as any)?.collapsed);
              const nodeCount = typeof (rb as any)?.nodeCount === 'number' ? (rb as any).nodeCount : undefined;
              const showCollapsePill = Boolean(canToggle && !isCollapsed && typeof nodeCount === 'number' && nodeCount > 1);
              return (
                <div
                  key={`rel-label-${bIdx}-${rIdx}`}
                  style={{
                    position: 'absolute',
                    left,
                    top: y,
                    width: w,
                    height: 16,
                    fontSize: 10,
                    fontWeight: 700,
                    color: subtitleColor,
                    textTransform: 'uppercase',
                    letterSpacing: '0.25px',
                    opacity: theme === 'dark' ? 0.55 : 0.60,
                    pointerEvents: 'none',
                    lineHeight: '16px',
                  }}
                  title={label}
                >
                  <span
                    style={{
                      position: 'absolute',
                      inset: 0,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      textAlign: 'center',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                      paddingRight: showCollapsePill ? 34 : 0,
                    }}
                  >
                    {label}
                  </span>

                  {/* Collapse control (only when expanded). Expand stays on the stack-controller node. */}
                  {showCollapsePill && (
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        (data as any)?.onToggleSubcategoryCollapse?.(groupIdForToggle, edgeTypeKey, true, firstNodeId || undefined);
                      }}
                      aria-label={`Collapse ${label} (${nodeCount})`}
                      title={`Collapse ${label} (${nodeCount})`}
                      style={{
                        pointerEvents: 'auto',
                        position: 'absolute',
                        right: 0,
                        top: 0,
                        height: 16,
                        padding: '0 6px',
                        borderRadius: 999,
                        border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.18)' : 'rgba(15,23,42,0.14)'}`,
                        background: theme === 'dark' ? 'rgba(0,0,0,0.26)' : 'rgba(255,255,255,0.78)',
                        color: subtitleColor,
                        cursor: 'pointer',
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 6,
                        fontSize: 10,
                        fontWeight: 850,
                        lineHeight: 1,
                      }}
                    >
                      <span>{nodeCount}</span>
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M18 15l-6-6-6 6" />
                      </svg>
                    </button>
                  )}
                </div>
              );
            });
          })}
        </>
      )}

      {/* Aggregation ports (edge types): always on the RIGHT edge, spaced vertically. */}
      {showEdgePorts && (
        <>
          {edgePorts!.map((p, idx) => {
            const y = typeof p?.y === 'number' ? p.y : undefined;
            if (typeof y !== 'number') return null;
            const targetId = String(p?.targetHandleId || '').trim() || `edge:${String(p?.edgeType || 'OTHER').trim()}:target`;
            const sourceId = String(p?.sourceHandleId || '').trim() || `edge:${String(p?.edgeType || 'OTHER').trim()}:source`;
            return (
              <React.Fragment key={`edge-port-${idx}-${targetId}-${sourceId}`}>
                <Handle
                  id={targetId}
                  type="target"
                  position={Position.Right}
                  className="hzn-handle"
                  style={{
                    background: border,
                    width: '10px',
                    height: '10px',
                    border: `2px solid ${theme === 'dark' ? 'rgba(15,23,42,0.75)' : 'rgba(255,255,255,0.95)'}`,
                    right: '-5px',
                    top: `${y}px`,
                    opacity: 0.85,
                    pointerEvents: 'none',
                  }}
                />
                <Handle
                  id={sourceId}
                  type="source"
                  position={Position.Right}
                  className="hzn-handle"
                  style={{
                    background: border,
                    width: '10px',
                    height: '10px',
                    border: `2px solid ${theme === 'dark' ? 'rgba(15,23,42,0.75)' : 'rgba(255,255,255,0.95)'}`,
                    right: '-5px',
                    top: `${y}px`,
                    opacity: 0, // hidden but present for correct directionality
                    pointerEvents: 'none',
                  }}
                />
              </React.Fragment>
            );
          })}
        </>
      )}

      {/* Header (optional) */}
      {!hideHeader && (
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
      )}

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
  // Note: ensureHorizonNode2026Styles() is called once on module load (not per render)
  const { theme } = React.useContext(ThemeContext);
  const colors = getNodeColors(data.type, theme);
  const [isHovering, setIsHovering] = React.useState(false);
  const rightFiltersRef = React.useRef<HTMLDivElement | null>(null);
  const rightFiltersBtnRef = React.useRef<HTMLButtonElement | null>(null);
  
  const isCritical = data.critical;
  const isCollapsed = data.collapsed;
  const isExpanded = data.expanded;
  const isLoadingRelated = Boolean(data.isLoadingRelated);
  const isHub = Boolean((data as any)?.hubHighlight);
  const isUltimateBeneficiary = Boolean((data as any)?.ultimateBeneficiaryHighlight);
  // Allow Research even if the first "load related" attempt returned empty: in that case the node isn't expanded,
  // but the user should still be able to run scouting from it.
  const isSelected = Boolean(selected);
  // Node expand/collapse (load related fragments). NOTE: stacks should not show this control:
  // the stack "N" pill controls stack expansion; node expansion is misleading on stack controllers.
  const canCollapse0 = Boolean(data.fragmentId && data.onCollapse && isExpanded && !data.isLoadingRelated);
  const showExpand0 = Boolean(data.fragmentId && data.onLoadRelated && !isExpanded);
  const isPinnedToScope = Boolean((data as any)?.pinnedToScope);
  const canTogglePin = Boolean((data as any)?.onTogglePin && data.canonicalPath);
  const stackKey = String((data as any)?.stackGroupKey || '').trim();
  const stackCount = typeof (data as any)?.stackCount === 'number' ? (data as any).stackCount : undefined;
  const stackExpanded = Boolean((data as any)?.stackExpanded);
  const canToggleStack = Boolean(stackKey && stackCount && stackCount > 1 && typeof (data as any)?.onToggleStack === 'function');
  const stackTitle = String((data as any)?.stackTitle || '').trim();
  const stackSubtitle = String((data as any)?.stackSubtitle || '').trim();
  const stackVisualType = String((data as any)?.stackVisualType || '').trim();
  const controllerId = String((data as any)?.stackControllerId || '').trim();
  const isStackController = Boolean(canToggleStack && controllerId && controllerId === String(data.id || '').trim());
  // Scout CTA: normally shown only when the node is expanded (or user already attempted loading related).
  // Exception: for RIGHT-PANE stacks, when the stack is expanded the controller node should still allow Scout,
  // even if the node itself isn't expanded yet (otherwise the first visible item in an expanded stack feels "broken").
  const allowScoutViaExpandedStack = Boolean(isStackController && stackExpanded && String((data as any)?.levelsPane || '') === 'right');
  const canScopeScout = Boolean((isExpanded || data.loadRelatedAttempted || allowScoutViaExpandedStack) && data.onScopeScout && !data.scopeScoutDone);
  const isCollapsedStackController = Boolean(isStackController && !stackExpanded && (stackCount || 0) > 1);
  const isSubcategoryStackController = Boolean(isCollapsedStackController && (data as any)?.subcategoryStack);
  const subcategoryEdgeType =
    isSubcategoryStackController
      ? (
          String((data as any)?.subcategoryEdgeType || '').trim() ||
          // Fallback: parse from stackGroupKey like `levels:leftsubcat:${groupId}:${typeKey}:${relKey}`
          (() => {
            const raw = String((data as any)?.stackGroupKey || '').trim();
            const parts = raw ? raw.split(':') : [];
            const last = parts.length > 0 ? parts[parts.length - 1] : '';
            return String(last || '').trim();
          })()
        )
      : '';
  const canCollapse = canCollapse0 && !isStackController;
  const showExpand = showExpand0 && !isStackController;
  const canExpandClick = Boolean(showExpand && !isLoadingRelated);
  const stackPreviewOpen = Boolean((data as any)?.stackPreviewOpen);
  const stackPreviewItems = Array.isArray((data as any)?.stackPreviewItems) ? ((data as any)?.stackPreviewItems as any[]) : [];
  const stackPreviewMore = typeof (data as any)?.stackPreviewMore === 'number' ? (data as any).stackPreviewMore : 0;
  const onStackPreviewSelect = (data as any)?.onStackPreviewSelect as ((nodeId: string) => void) | undefined;
  const onStackPreviewMore = (data as any)?.onStackPreviewMore as (() => void) | undefined;
  // Collapsed stacked view: show 2–3 subtle "cards" peeking from underneath.
  const stackLayers = canToggleStack && !stackExpanded
    ? Math.min(3, Math.max(2, Math.ceil((stackCount || 0) / 8)))
    : 0;

  const isLevelsRight = String((data as any)?.levelsPane || '') === 'right';
  const rightFiltersOpen = Boolean((data as any)?.rightFiltersOpen);
  const canRightFilters = isLevelsRight && typeof (data as any)?.onOpenRightFilters === 'function' && typeof (data as any)?.onToggleRightFilter === 'function';
  const rfHideParents = Boolean((data as any)?.rightFilterHideLoneParents);
  const rfHideChildren = Boolean((data as any)?.rightFilterHideLoneChildren);
  const rfHideGov = Boolean((data as any)?.rightFilterHideLoneGovernancePeople);
  const canTrace = typeof (data as any)?.onTrace === 'function';
  const canUndoTrace = Boolean((data as any)?.canUndoTrace) && typeof (data as any)?.onUndoTrace === 'function';

  // Local relation filters (edge-type toggles shown above node on click)
  const localFiltersOpen = Boolean((data as any)?.localFiltersOpen);
  const localFilterState = ((data as any)?.localFilterState || {}) as Record<string, boolean>;
  const localFilterEdgeTypes = Array.isArray((data as any)?.localFilterEdgeTypes) ? ((data as any)?.localFilterEdgeTypes as string[]) : [];
  const canLocalFilters = localFilterEdgeTypes.length > 0 && typeof (data as any)?.onToggleLocalFilter === 'function';
  const localFiltersRef = React.useRef<HTMLDivElement | null>(null);

  React.useEffect(() => {
    if (!canRightFilters || !rightFiltersOpen) return;
    const onMouseDown = (e: MouseEvent) => {
      const t = e.target as any;
      if (rightFiltersRef.current && rightFiltersRef.current.contains(t)) return;
      if (rightFiltersBtnRef.current && rightFiltersBtnRef.current.contains(t)) return;
      (data as any)?.onOpenRightFilters?.(data.id, false);
    };
    window.addEventListener('mousedown', onMouseDown, true);
    return () => window.removeEventListener('mousedown', onMouseDown, true);
  }, [canRightFilters, rightFiltersOpen, data]);

  React.useEffect(() => {
    if (!canLocalFilters || !localFiltersOpen) return;
    const onMouseDown = (e: MouseEvent) => {
      const t = e.target as any;
      if (localFiltersRef.current && localFiltersRef.current.contains(t)) return;
      // Close on any click outside the filters
      (data as any)?.onOpenLocalFilters?.(data.id, false);
    };
    window.addEventListener('mousedown', onMouseDown, true);
    return () => window.removeEventListener('mousedown', onMouseDown, true);
  }, [canLocalFilters, localFiltersOpen, data]);
  
  // Stack controllers can have a separate visual type (Persons/Products/Control/Geolocation).
  const visualType = isCollapsedStackController && stackVisualType ? stackVisualType : (data.type || '');
  const colorsForVisual = visualType ? getNodeColors(visualType, theme) : colors;

  // 2026 visual style: neutral surface with tinted accent (less "dashboardy", more product-like)
  const accentColor = isCritical ? '#EF4444' : (colorsForVisual?.accent || colors.accent);
  const textColor = theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.88)';
  const mutedTextColor = theme === 'dark' ? 'rgba(255,255,255,0.62)' : 'rgba(15,23,42,0.56)';
  // Stack controllers: calm single-surface fill (no gradients).
  const surfaceBg = isCollapsedStackController
    ? (theme === 'dark' ? 'rgba(17,24,39,0.94)' : 'rgba(255,255,255,0.98)')
    : (theme === 'dark' ? 'rgba(17,24,39,0.90)' : 'rgba(255,255,255,0.96)');
  const surfaceBorder = isCollapsedStackController
    ? (theme === 'dark' ? 'rgba(148,163,184,0.28)' : 'rgba(100,116,139,0.24)') // light graphite
    : (theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)');
  const stackBorderColor =
    isCollapsedStackController
      ? (isSubcategoryStackController
          ? getEdgeColor(subcategoryEdgeType || 'RELATED_TO')
          : accentColor)
      : surfaceBorder;
  const accentSoft = hexToRgba(accentColor, theme === 'dark' ? 0.22 : 0.16);
  const accentRing = hexToRgba(accentColor, theme === 'dark' ? 0.55 : 0.34);
  const shadowBase = theme === 'dark' ? '0 8px 22px rgba(0,0,0,0.30)' : '0 8px 22px rgba(0,0,0,0.10)';
  const criticalRing = hexToRgba('#EF4444', theme === 'dark' ? 0.55 : 0.35);
  const q = getHorizonNodeVisualQuality();

  // Additional semantic highlights:
  // - Ultimate beneficiaries (PERSON) on upward control traversal: greenish ring
  // - Hubs (in>=5 or out>=5): amber ring
  // Priority: critical > beneficiary > hub
  const hubRing = theme === 'dark' ? 'rgba(251, 191, 36, 0.60)' : 'rgba(245, 158, 11, 0.55)';
  const beneficiaryRing = theme === 'dark' ? 'rgba(52, 211, 153, 0.56)' : 'rgba(16, 185, 129, 0.46)';
  const highlightRing = !isCritical ? (isUltimateBeneficiary ? beneficiaryRing : (isHub ? hubRing : null)) : null;

  const displayLabel =
    (isCollapsedStackController && stackTitle)
      ? stackTitle
      : (data.label || humanizeCanonicalPath(data.canonicalPath) || 'Fragment');
  const titleLineClamp = isCollapsed ? 2 : (data.showMetrics ? 2 : 6);
  const displayType = data.type || 'Unknown';
  // Keep node size consistent with ELK layout defaults used in the canvas (see LensCanvasCockpit)
  const NODE_WIDTH_PX = 180;
  const NODE_CARD_HEIGHT_PX = 148;

  // Форматирование типа для отображения
  const formatType = (type: string) => {
    return type.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
  };

  return (
    <>
      {/* Selected-only contextual CTA (no hover): "Scout it" */}
      {canScopeScout && data.fragmentId && data.onScopeScout && (
        <NodeToolbar
          isVisible={isSelected}
          position={Position.Bottom}
          offset={12}
        >
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              if (!data.fragmentId) return;
              if (data.isScopeScouting) return;
              data.onScopeScout?.(data.fragmentId, data.canonicalPath);
            }}
            disabled={data.isScopeScouting}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 8,
              padding: '8px 10px',
              borderRadius: 12,
              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.18)' : 'rgba(15,23,42,0.14)'}`,
              background: theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.82)',
              color: theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.90)',
              boxShadow: theme === 'dark' ? '0 10px 24px rgba(0,0,0,0.38)' : '0 10px 24px rgba(0,0,0,0.14)',
              cursor: data.isScopeScouting ? 'wait' : 'pointer',
              whiteSpace: 'nowrap',
              pointerEvents: 'auto',
            }}
            title="Scout this fragment (add to scope + scout)"
            aria-label="Scout it"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke={accentColor}
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <circle cx="11" cy="11" r="7" />
              <path d="M21 21l-4.3-4.3" />
            </svg>
            <span style={{ fontSize: 12, fontWeight: 700 }}>
              {data.isScopeScouting ? 'Scouting…' : 'Scout it'}
            </span>
          </button>
        </NodeToolbar>
      )}

      {/* Stack preview chips (collapsed stack): show up to 5 hidden members + more.
          IMPORTANT: render INSIDE the node DOM (not NodeToolbar), so it scales with graph zoom. */}
      {canToggleStack && !stackExpanded && stackPreviewOpen && (
        (() => {
          return (
        <div
          style={{
            position: 'absolute',
            left: '100%',
            top: 12,
            marginLeft: 12,
            display: 'flex',
            flexDirection: 'column',
            gap: 6,
            padding: 0,
            borderRadius: 0,
            border: 'none',
            background: 'transparent',
            boxShadow: 'none',
            pointerEvents: 'auto',
            maxWidth: 240,
            zIndex: 60,
          }}
          onMouseDown={(e) => e.stopPropagation()}
          onClick={(e) => e.stopPropagation()}
        >
          {stackPreviewItems.slice(0, 5).map((it: any) => {
            const id = String(it?.nodeId || '').trim();
            const label = String(it?.label || '').trim() || 'Fragment';
            const t = String(it?.type || '').trim() || 'UNKNOWN';
            const c = getNodeColors(t, theme);
            return (
              <button
                key={`stack-prev-${id}`}
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onStackPreviewSelect?.(id);
                }}
                title={label}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 8,
                  padding: '2px 0',
                  borderRadius: 0,
                  border: 'none',
                  background: 'transparent',
                  color: textColor,
                  cursor: 'pointer',
                  width: '100%',
                  textAlign: 'left',
                }}
              >
                <HorizonNodeIcon type={t} color={c.accent} size={14} />
                <span
                  style={{
                    fontSize: 12,
                    fontWeight: 700,
                    minWidth: 0,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                    flex: 1,
                  }}
                >
                  {label}
                </span>
              </button>
            );
          })}

          {stackPreviewMore > 0 && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onStackPreviewMore?.();
              }}
              title={`Show ${stackPreviewMore} more`}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'flex-start',
                gap: 8,
                padding: '2px 0',
                borderRadius: 0,
                border: 'none',
                background: 'transparent',
                color: mutedTextColor,
                cursor: 'pointer',
                width: '100%',
                fontSize: 12,
                fontWeight: 800,
              }}
            >
              +{stackPreviewMore} more
            </button>
          )}
        </div>
          );
        })()
      )}

      {/* Local relation filters (edge-type toggles above the node, shown on click).
          Renders INSIDE the node DOM so it scales with graph zoom. */}
      {canLocalFilters && localFiltersOpen && (
        <div
          ref={localFiltersRef}
          style={{
            position: 'absolute',
            bottom: '100%',
            left: 0,
            marginBottom: 8,
            display: 'flex',
            flexDirection: 'row',
            gap: 4,
            padding: 0,
            background: 'transparent',
            pointerEvents: 'auto',
            zIndex: 60,
          }}
          onMouseDown={(e) => e.stopPropagation()}
          onClick={(e) => e.stopPropagation()}
        >
          {localFilterEdgeTypes.map((et) => {
            const isVisible = localFilterState[et] !== false; // default true
            const color = getEdgeColor(et);
            return (
              <button
                key={`lf-${et}`}
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  (data as any)?.onToggleLocalFilter?.(data.id, et, !isVisible);
                }}
                title={`${isVisible ? 'Hide' : 'Show'} ${edgeCaptionShort(et)}`}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  width: 22,
                  height: 22,
                  borderRadius: 6,
                  border: 'none',
                  background: 'transparent',
                  cursor: 'pointer',
                  opacity: isVisible ? 1 : 0.35,
                  transition: 'opacity 0.15s ease',
                }}
              >
                <EdgeTypeIcon edgeType={et} color={color} />
              </button>
            );
          })}
        </div>
      )}

      <div
        className={`hzn-node-2026 ${isSelected ? 'is-selected' : ''} ${isCollapsedStackController ? 'is-stack' : ''}`}
        onMouseEnter={() => setIsHovering(true)}
        onMouseLeave={() => setIsHovering(false)}
        style={{
          // CSS variables for 2026 styles
          ['--hzn-surface' as any]: surfaceBg,
          ['--hzn-border' as any]: surfaceBorder,
          ['--hzn-stack-border' as any]: stackBorderColor,
          ['--hzn-accent' as any]: accentColor,
          ['--hzn-accent-soft' as any]: accentSoft,
          ['--hzn-accent-ring' as any]: isCritical ? criticalRing : accentRing,
          ['--hzn-shadow' as any]: (() => {
            const baseShadow = isCritical
              ? `0 0 0 2px ${criticalRing}, ${shadowBase}`
              : (q === 'max' ? `${shadowBase}, 0 18px 60px rgba(0,0,0,0.18)` : shadowBase);
            return highlightRing ? `0 0 0 2px ${highlightRing}, ${baseShadow}` : baseShadow;
          })(),
          padding: isCollapsed ? '8px 12px 12px' : '10px 14px 14px',
          width: `${NODE_WIDTH_PX}px`,
          height: `${NODE_CARD_HEIGHT_PX}px`,
          boxSizing: 'border-box',
          position: 'relative',
          opacity: isCollapsed ? 0.7 : 1,
          cursor: 'default',
          overflow: 'visible',
        }}
      >
      {/* Stack controllers are now rendered as a single calm surface (no backplates). */}

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

      {/* Header */}
      {isCollapsedStackController ? (
        <>
          {/* Stack controller: centered premium layout (icon above text) */}
          <div
            style={{
              position: 'absolute',
              // Align the main icon top with the stack badge top-right.
              inset: '10px 16px 30px 16px',
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'flex-start',
              textAlign: 'center',
              gap: 8,
              pointerEvents: 'none',
            }}
          >
            <div
              style={{
                width: 44,
                height: 44,
                borderRadius: 14,
                background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)',
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.12)' : 'rgba(15,23,42,0.10)'}`,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                // Keep the badge away from the card's rounded border (stack controllers have stacked backplates).
                marginTop: 0,
                boxShadow: theme === 'dark'
                  ? `0 14px 30px rgba(0,0,0,0.22), inset 0 0 0 1px ${hexToRgba(accentColor, 0.18)}`
                  : `0 14px 30px rgba(0,0,0,0.10), inset 0 0 0 1px ${hexToRgba(accentColor, 0.12)}`,
              }}
            >
              {isSubcategoryStackController ? (
                <div style={{ transform: 'translateY(-1px)' }}>
                  <EdgeTypeIcon
                    edgeType={subcategoryEdgeType || 'RELATED_TO'}
                    color={getEdgeColor(subcategoryEdgeType || 'RELATED_TO')}
                    size={16}
                  />
                </div>
              ) : (
                <HorizonNodeIcon type={visualType || data.type} color={accentColor} size={18} />
              )}
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0, width: '100%' }}>
              <div
                style={{
                  fontSize: 15,
                  fontWeight: 850,
                  color: textColor,
                  letterSpacing: '-0.01em',
                  lineHeight: 1.15,
                  width: '100%',
                  maxWidth: '100%',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
                title={stackTitle || displayLabel}
              >
                {stackTitle || displayLabel}
              </div>
              {stackSubtitle && (
                <div
                  style={{
                    fontSize: 12,
                    fontWeight: 520,
                    color: mutedTextColor,
                    lineHeight: 1.2,
                    width: '100%',
                    maxWidth: '100%',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                  title={stackSubtitle}
                >
                  {stackSubtitle}
                </div>
              )}
            </div>
          </div>

          {/* Right-side actions / indicators: keep clickable, don't disturb centered stack layout */}
          <div
            style={{
              position: 'absolute',
              right: 10,
              top: 10,
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              zIndex: 3,
            }}
          >
            {/* Stack indicator: strict top-right position (same slot as Pin on regular nodes). */}
            {isCollapsedStackController && (
              <div
                style={{
                  width: 22,
                  height: 22,
                  borderRadius: 8,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.25)' : 'rgba(255,255,255,0.65)',
                  color: theme === 'dark' ? 'rgba(255,255,255,0.75)' : 'rgba(15,23,42,0.70)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  boxShadow: theme === 'dark' ? '0 1px 6px rgba(0,0,0,0.35)' : '0 1px 6px rgba(0,0,0,0.10)',
                  pointerEvents: 'none',
                }}
                title="Stack"
                aria-hidden="true"
              >
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M12 2 2 7l10 5 10-5-10-5z" />
                  <path d="M2 17l10 5 10-5" />
                  <path d="M2 12l10 5 10-5" />
                </svg>
              </div>
            )}

            {/* Pin is not shown for subcategory stacks */}
            {canTogglePin && !isCollapsedStackController && !isSubcategoryStackController && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  const cb = (data as any)?.onTogglePin as
                    | ((args: { fragmentId?: string; canonicalPath?: string; nextPinned: boolean }) => void)
                    | undefined;
                  cb?.({ fragmentId: data.fragmentId, canonicalPath: data.canonicalPath, nextPinned: !isPinnedToScope });
                }}
                aria-label={isPinnedToScope ? 'Unpin from scope' : 'Pin to scope'}
                title={isPinnedToScope ? 'Pinned to scope (click to unpin)' : 'Pin to scope'}
                style={{
                  width: 22,
                  height: 22,
                  borderRadius: 8,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.25)' : 'rgba(255,255,255,0.65)',
                  color: isPinnedToScope ? accentColor : (theme === 'dark' ? 'rgba(255,255,255,0.85)' : 'rgba(15,23,42,0.75)'),
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  boxShadow: theme === 'dark' ? '0 1px 6px rgba(0,0,0,0.35)' : '0 1px 6px rgba(0,0,0,0.10)',
                  pointerEvents: 'auto',
                }}
              >
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill={isPinnedToScope ? 'currentColor' : 'none'}
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <line x1="12" y1="17" x2="12" y2="22" />
                  <path d="M5 17h14v-1.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1v4.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24Z" />
                </svg>
              </button>
            )}
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
        </>
      ) : (
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
            <HorizonNodeIcon type={visualType || data.type} color={accentColor} size={14} />
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
                WebkitLineClamp: titleLineClamp as any,
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
            {/* Pin is not shown for subcategory stacks */}
            {canTogglePin && !isSubcategoryStackController && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  const cb = (data as any)?.onTogglePin as
                    | ((args: { fragmentId?: string; canonicalPath?: string; nextPinned: boolean }) => void)
                    | undefined;
                  cb?.({ fragmentId: data.fragmentId, canonicalPath: data.canonicalPath, nextPinned: !isPinnedToScope });
                }}
                aria-label={isPinnedToScope ? 'Unpin from scope' : 'Pin to scope'}
                title={isPinnedToScope ? 'Pinned to scope (click to unpin)' : 'Pin to scope'}
                style={{
                  width: 22,
                  height: 22,
                  borderRadius: 8,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.25)' : 'rgba(255,255,255,0.65)',
                  color: isPinnedToScope ? accentColor : (theme === 'dark' ? 'rgba(255,255,255,0.85)' : 'rgba(15,23,42,0.75)'),
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  boxShadow: theme === 'dark' ? '0 1px 6px rgba(0,0,0,0.35)' : '0 1px 6px rgba(0,0,0,0.10)',
                }}
              >
                {/* Lucide "pin" (thumbtack) */}
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill={isPinnedToScope ? 'currentColor' : 'none'}
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <line x1="12" y1="17" x2="12" y2="22" />
                  <path d="M5 17h14v-1.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1v4.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24Z" />
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
      )}

      {/* Expand / collapse moved to bottom-right (less intrusive; more discoverable near content). */}
      {(showExpand || canCollapse || (isSubcategoryStackController && canToggleStack)) && (
        <div
          style={{
            position: 'absolute',
            right: 12,
            bottom: 12,
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            zIndex: 2,
          }}
        >
          {isSubcategoryStackController && canToggleStack && typeof stackCount === 'number' && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                const cb = (data as any)?.onToggleStack as ((k: string, next: boolean) => void) | undefined;
                cb?.(stackKey, true);
              }}
              aria-label={`Expand ${stackTitle || displayLabel} (${stackCount})`}
              title={`Expand ${stackTitle || displayLabel} (${stackCount})`}
              style={{
                minWidth: 28,
                height: 22,
                padding: '0 8px',
                borderRadius: 11,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.22)' : 'rgba(15,23,42,0.16)'}`,
                background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.82)',
                color: theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.82)',
                cursor: 'pointer',
                fontSize: 12,
                fontWeight: 850,
                lineHeight: 1,
                boxShadow: theme === 'dark' ? '0 8px 18px rgba(0,0,0,0.25)' : '0 8px 18px rgba(0,0,0,0.12)',
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
              }}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
                <path d="M6 9l6 6 6-6" />
              </svg>
              <span>{stackCount}</span>
            </button>
          )}
          {showExpand && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                if (!canExpandClick) return;
                if (data.fragmentId && data.onLoadRelated) {
                  data.onLoadRelated(data.fragmentId);
                }
              }}
              aria-label="Expand"
              title={isLoadingRelated ? 'Loading related fragments…' : 'Expand (load related fragments)'}
              disabled={!canExpandClick}
              style={{
                width: 24,
                height: 24,
                borderRadius: 9,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.78)',
                color: theme === 'dark' ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.78)',
                cursor: isLoadingRelated ? 'wait' : 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                boxShadow: theme === 'dark' ? '0 8px 18px rgba(0,0,0,0.42)' : '0 8px 18px rgba(0,0,0,0.12)',
                opacity: isLoadingRelated ? 0.7 : 1,
              }}
            >
              {isLoadingRelated ? (
                <span
                  style={{
                    width: 14,
                    height: 14,
                    borderRadius: '50%',
                    border: `2px solid ${theme === 'dark' ? 'rgba(255,255,255,0.22)' : 'rgba(15,23,42,0.16)'}`,
                    borderTopColor: accentColor,
                    boxSizing: 'border-box',
                    animation: 'hzn-spin 0.9s linear infinite',
                  }}
                />
              ) : (
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
                  <path d="M6 9l6 6 6-6" />
                </svg>
              )}
            </button>
          )}
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
                width: 24,
                height: 24,
                borderRadius: 9,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                // Expanded state: same control, but with an amber fill for better state recognition.
                background: theme === 'dark' ? 'rgba(245, 158, 11, 0.28)' : 'rgba(245, 158, 11, 0.18)',
                color: theme === 'dark' ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.78)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                boxShadow: theme === 'dark' ? '0 8px 18px rgba(0,0,0,0.42)' : '0 8px 18px rgba(0,0,0,0.12)',
              }}
            >
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
        </div>
      )}

      {/* Stack toggle: sits just below the node, pairs with the stacked-card visual cue */}
      {canToggleStack && !isSubcategoryStackController && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            const cb = (data as any)?.onToggleStack as ((k: string, next: boolean) => void) | undefined;
            cb?.(stackKey, !stackExpanded);
          }}
          aria-label={stackExpanded ? 'Collapse stack' : 'Expand stack'}
          title={stackExpanded ? 'Collapse stacked nodes' : `Show ${(stackCount || 0)} items`}
          style={{
            position: 'absolute',
            left: '50%',
            bottom: -14,
            transform: 'translateX(-50%)',
            height: 26,
            padding: '0 10px',
            borderRadius: 999,
            border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.18)' : 'rgba(15,23,42,0.14)'}`,
            background: theme === 'dark' ? 'rgba(0,0,0,0.45)' : 'rgba(255,255,255,0.82)',
            color: theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.86)',
            cursor: 'pointer',
            display: 'inline-flex',
            alignItems: 'center',
            gap: 8,
            boxShadow: theme === 'dark' ? '0 10px 24px rgba(0,0,0,0.28)' : '0 10px 24px rgba(0,0,0,0.12)',
            fontSize: 12,
            fontWeight: 800,
            lineHeight: 1,
            pointerEvents: 'auto',
            zIndex: 3,
          }}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 2 3 7l9 5 9-5-9-5Z" />
            <path d="M3 12l9 5 9-5" />
            <path d="M3 17l9 5 9-5" />
          </svg>
          <span>
            {stackExpanded ? 'Collapse' : `${(stackCount || 0)}`}
          </span>
        </button>
      )}

      {/* Right-side hover circle + actions menu (Levels right pane) */}
      {canRightFilters && (
        <>
          <button
            ref={rightFiltersBtnRef}
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              (data as any)?.onOpenRightFilters?.(data.id, !rightFiltersOpen);
            }}
            title="Actions"
            aria-label="Actions"
            style={{
              position: 'absolute',
              right: -16,
              top: '50%',
              transform: 'translateY(-50%)',
              width: 28,
              height: 28,
              borderRadius: 999,
              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.18)' : 'rgba(15,23,42,0.14)'}`,
              background: theme === 'dark' ? 'rgba(0,0,0,0.40)' : 'rgba(255,255,255,0.82)',
              color: theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.86)',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              boxShadow: theme === 'dark' ? '0 10px 24px rgba(0,0,0,0.24)' : '0 10px 24px rgba(0,0,0,0.10)',
              opacity: (isHovering || rightFiltersOpen) ? 1 : 0,
              pointerEvents: (isHovering || rightFiltersOpen) ? 'auto' : 'none',
              transition: 'opacity 120ms ease',
              zIndex: 4,
            }}
          >
            {/* actions / more icon */}
            <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <circle cx="12" cy="5" r="2.1" />
              <circle cx="12" cy="12" r="2.1" />
              <circle cx="12" cy="19" r="2.1" />
            </svg>
          </button>

          {rightFiltersOpen && (
            <div
              ref={rightFiltersRef}
              onClick={(e) => e.stopPropagation()}
              style={{
                position: 'absolute',
                top: '50%',
                left: `calc(100% + 18px)`,
                transform: 'translateY(-50%)',
                minWidth: 220,
                padding: 10,
                borderRadius: 14,
                // Match the "Actions" button (dark, inky surface) in dark theme.
                border: theme === 'dark' ? '1px solid rgba(255,255,255,0.18)' : '1px solid var(--border-subtle)',
                background: theme === 'dark' ? 'rgba(0,0,0,0.62)' : 'rgba(255,255,255,0.96)',
                boxShadow: theme === 'dark' ? '0 18px 60px rgba(0,0,0,0.72)' : '0 18px 60px rgba(0,0,0,0.18)',
                zIndex: 5,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10, marginBottom: 8 }}>
                <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>
                  Actions
                </div>
                <button
                  type="button"
                  onClick={() => (data as any)?.onOpenRightFilters?.(data.id, false)}
                  style={{
                    width: 28,
                    height: 28,
                    borderRadius: 10,
                    border: theme === 'dark' ? '1px solid rgba(255,255,255,0.16)' : '1px solid var(--border-subtle)',
                    background: theme === 'dark' ? 'rgba(0,0,0,0.25)' : 'transparent',
                    color: theme === 'dark' ? 'rgba(255,255,255,0.82)' : 'var(--text-secondary-strong)',
                    cursor: 'pointer',
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    lineHeight: 1,
                  }}
                  title="Close"
                  aria-label="Close"
                >
                  <span style={{ fontSize: 14, fontWeight: 900 }}>×</span>
                </button>
              </div>

              <div style={{ fontSize: 11, fontWeight: 800, color: theme === 'dark' ? 'rgba(255,255,255,0.78)' : 'rgba(15,23,42,0.78)', marginBottom: 8 }}>
                Hide (context)
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {([
                  { key: 'loneParents', label: 'Lone parents', active: rfHideParents },
                  { key: 'loneChildren', label: 'Lone children', active: rfHideChildren },
                  { key: 'loneGovernancePeople', label: 'Lone board/exec', active: rfHideGov },
                ] as const).map((chip) => (
                  <button
                    key={chip.key}
                    type="button"
                    onClick={() => {
                      (data as any)?.onToggleRightFilter?.(data.id, chip.key, !chip.active);
                    }}
                    style={{
                      padding: '6px 10px',
                      borderRadius: 999,
                      border: `1px solid ${chip.active ? (theme === 'dark' ? 'rgba(59,130,246,0.55)' : 'rgba(59,130,246,0.45)') : 'var(--border-subtle)'}`,
                      background: chip.active
                        ? (theme === 'dark' ? 'rgba(59,130,246,0.22)' : 'rgba(59,130,246,0.12)')
                        : (theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.04)'),
                      color: 'var(--text-heading)',
                      cursor: 'pointer',
                      fontSize: 12,
                      fontWeight: 750,
                    }}
                    title={chip.label}
                  >
                    {chip.label}
                  </button>
                ))}
              </div>
              <div style={{ marginTop: 10, fontSize: 11, color: theme === 'dark' ? 'rgba(255,255,255,0.58)' : 'rgba(15,23,42,0.56)', lineHeight: 1.35 }}>
                Lone = nodes that only connect to this node.
              </div>

              <div style={{ height: 1, background: 'var(--border-subtle)', margin: '10px 0' }} />

              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <button
                  type="button"
                  disabled={!canTrace}
                  onClick={() => {
                    (data as any)?.onTrace?.(data.id);
                    (data as any)?.onOpenRightFilters?.(data.id, false);
                  }}
                  style={{
                    width: '100%',
                    padding: '8px 10px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.04)',
                    color: 'var(--text-heading)',
                    cursor: canTrace ? 'pointer' : 'not-allowed',
                    fontSize: 12,
                    fontWeight: 850,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 10,
                    opacity: canTrace ? 1 : 0.45,
                  }}
                  title={canTrace ? 'Trace…' : 'Trace is unavailable'}
                >
                  <span>Trace…</span>
                  <span style={{ color: 'var(--text-secondary-strong)', fontWeight: 900 }}>↗</span>
                </button>

                <button
                  type="button"
                  disabled={!canUndoTrace}
                  onClick={() => {
                    (data as any)?.onUndoTrace?.();
                    (data as any)?.onOpenRightFilters?.(data.id, false);
                  }}
                  style={{
                    width: '100%',
                    padding: '8px 10px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'transparent',
                    color: 'var(--text-heading)',
                    cursor: canUndoTrace ? 'pointer' : 'not-allowed',
                    fontSize: 12,
                    fontWeight: 850,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 10,
                    opacity: canUndoTrace ? 1 : 0.45,
                  }}
                  title={canUndoTrace ? 'Undo last trace' : 'No trace to undo'}
                >
                  <span>Undo last trace</span>
                  <span style={{ color: 'var(--text-secondary-strong)', fontWeight: 900 }}>↶</span>
                </button>
              </div>
            </div>
          )}
        </>
      )}

      {/* Метрики (если не collapsed) */}
      {!isCollapsed && data.showMetrics && data.state && Object.keys(data.state).length > 0 && (
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

      {/* CSS для анимации пульса */}
    </div>
    </>
  );
});

// Экспортируем типы нод для Horizon графа
export const HORIZON_NODE_TYPES: NodeTypes = Object.freeze({
  horizonFragment: HorizonFragmentNode,
  horizonGroup: HorizonGroupNode,
});

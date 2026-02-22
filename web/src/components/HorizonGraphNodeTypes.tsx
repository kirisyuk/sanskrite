import React from 'react';
import { NodeTypes, Handle, Position, NodeProps, NodeToolbar, useReactFlow } from 'reactflow';
import { useTranslation } from 'react-i18next';
import { ThemeContext } from './ThemeProvider';
import type { SemanticGroup, LinearSection } from '../lib/elkLayout';
import { humanizeCanonicalPath, preferDisplayLabel } from '../lib/uiUtils';
import { RiskSegmentIcon, computeRiskSegments, computeRiskSegmentSource, formatRiskLevel, clamp01, type RiskSegmentId } from '../lib/riskShield';
import * as CountryFlags from 'country-flag-icons/react/1x1';

// Extract ISO 2-letter country code from canonical path
// Supports: /org/{cc}/..., /gov/{cc}/..., /market/{cc}/..., /loc/{cc}/...
const extractCountryCode = (canonicalPath?: string): string | null => {
  if (!canonicalPath) return null;
  const parts = canonicalPath.toLowerCase().split('/').filter(Boolean);
  if (parts.length < 2) return null;
  
  const namespace = parts[0];
  // These namespaces have country code as second segment
  if (['org', 'gov', 'market', 'loc', 'fac', 'deposit', 'community'].includes(namespace)) {
    const cc = parts[1];
    // Must be exactly 2 letters and not 'na' (not applicable)
    if (cc && cc.length === 2 && /^[a-z]{2}$/.test(cc) && cc !== 'na') {
      return cc.toUpperCase();
    }
  }
  return null;
};

// Country flag component - renders flag icon from ISO 2-letter code
const CountryFlag: React.FC<{ countryCode: string | null; size?: number }> = ({ countryCode, size = 14 }) => {
  if (!countryCode) return null;
  
  const FlagComponent = (CountryFlags as Record<string, React.FC<React.SVGProps<SVGSVGElement>>>)[countryCode];
  if (!FlagComponent) return null;
  
  return (
    <FlagComponent
      style={{
        width: size,
        height: size,
        borderRadius: 2,
        flexShrink: 0,
      }}
    />
  );
};

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
  /** Canonical market segment paths (/segment/...) for this fragment (if present). */
  marketSegments?: string[];
  /** Derived normalization of market segments (computed on backend). */
  marketSegmentNorms?: Array<{
    segmentPath: string;
    normDomain: string;
    normVertical?: string;
    normPair: string;
    channel?: string;
  }>;
  /** Deduped list of normalized market domains (for future filtering). */
  marketSegmentNormDomains?: string[];
  /** Deduped list of normalized market verticals (for future filtering). */
  marketSegmentNormVerticals?: string[];
  /** Deduped list of normalized domain/vertical pairs (for future filtering). */
  marketSegmentNormPairs?: string[];
  label?: string;
  type?: string; // FragmentType
  state?: Record<string, number>; // Метрики
  /** UI: whether to render metrics inside the node (disabled by default for performance). */
  showMetrics?: boolean;
  status?: string;
  expanded?: boolean;
  collapsed?: boolean;
  critical?: boolean;
  /** UI: risk shield mode for this node (computed in LensCanvasCockpit displayNodes). */
  riskShieldOpen?: boolean;
  /** Global, precomputed "high-profile" business signals for this fragment (backend-provided). */
  highProfile?: {
    version: number;
    updatedAt: string;
    signals?: Record<string, {
      score: number;
      level?: 'low' | 'medium' | 'high' | 'critical' | string;
      confidence?: number;
      evidence?: {
        edgeIds?: string[];
        neighborPaths?: string[];
        note?: string;
      } | null;
    }>;
  } | null;
  /** UI: toggle risk shield for a fragment (wired by LensCanvasCockpit). */
  onToggleRiskShield?: (fragmentId: string) => void;
  /** UI: rounded highlight ring for graph query match (wired by LensCanvasCockpit). */
  queryHighlight?: boolean;
  /** UI: rounded highlight ring for selected domain overlay (wired by LensCanvasCockpit). */
  domainHighlight?: boolean;
  /** UI: stronger rounded ring for top-dependency focus nodes (wired by LensCanvasCockpit). */
  topDependencyHighlight?: boolean;
  /** UI: rounded red ring for propagation trace preview (wired by LensCanvasCockpit). */
  signalPropagationHighlight?: boolean;
  /** UI: temporary glow after expand (wired by LensCanvasCockpit). */
  expandGlow?: boolean;
  onLoadRelated?: (fragmentId: string) => void; // Функция для загрузки связанных элементов
  onCollapse?: (fragmentId: string) => void; // Функция для сворачивания (undo expand)
  isLoadingRelated?: boolean; // Флаг загрузки связанных элементов
  loadRelatedAttempted?: boolean; // UI: user tried loading related nodes at least once
  onScopeScout?: (fragmentId: string, canonicalPath?: string) => void; // Add to scope + run scouting depth=1
  isScopeScouting?: boolean; // UI: scope-scout request in-flight
  scopeScoutDone?: boolean; // UI: scope-scout already launched (show "+" again)
  /** UI: open a concise entity summary modal (wired by LensCanvasCockpit). */
  onExplainNode?: (args: { nodeId: string; fragmentId?: string; canonicalPath?: string; label?: string; type?: string }) => void;
  // Group/container nodes (UI-only, not persisted)
  isGroup?: boolean;
  groupParentId?: string;
  /** Owner of this semantic group container (single parent node in the right pane). */
  groupOwnerFragmentId?: string;
  groupOwnerLabel?: string;
  groupOwnerType?: string;
  groupOwnerCanonicalPath?: string;
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
  // Role overflow (left pane compact categories)
  groupOverflowCount?: number;
  groupOverflowKey?: string;
  groupOverflowExpanded?: boolean;
  onToggleRoleOverflow?: (groupKey: string, nextExpanded: boolean) => void;
  // Collapsible subcategories (Left pane: subcategories by relation type)
  // Each subcategory (relBlock) can be collapsed into a compact stack representation.
  // Key format: `${groupId}:${edgeType}` or just edgeType within the group.
  groupCollapsedSubcategories?: Set<string> | string[];  // Edge types that are collapsed in this group
  onToggleSubcategoryCollapse?: (groupId: string, edgeType: string, nextCollapsed: boolean) => void;
  // Business block context cards (left pane compact summary)
  contextCard?: boolean;
  contextTitle?: string;
  contextCount?: number;
  contextLines?: string[];
  contextBlockKey?: string;
  contextGroupId?: string;
  onToggleContextDetails?: () => void;
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
  stackKind?: 'persons' | 'staff' | 'products' | 'control' | 'geolocation' | 'suppliers' | 'dependencies' | 'dominates';
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
  // Right dock (Levels layout; view-only)
  rightDockEnabled?: Array<'governance' | 'supply' | 'dependencies' | 'structure'>;
  onToggleRightDock?: (nodeId: string, key: 'governance' | 'supply' | 'dependencies' | 'structure', nextEnabled: boolean) => void;
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
  /** Node display size: 'default' (148px height) or 'compact' (48px height). */
  nodeSize?: 'default' | 'compact';
  [key: string]: any;
}

// Node dimensions (default / full size)
export const DEFAULT_NODE_WIDTH = 180;
export const DEFAULT_NODE_HEIGHT = 148;
export const DEFAULT_NODE_LAYOUT_HEIGHT = 161; // includes expand button protruding

// Compact node dimensions (used in left pane groups)
export const COMPACT_NODE_WIDTH = 180;
// Goal: 3 compact nodes fit into one full (layout) slot: 3*49 + 2*7 = 161
export const COMPACT_NODE_HEIGHT = 49;
export const COMPACT_NODE_LAYOUT_HEIGHT = 49;

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
  // ORGANIZATION nodes that live under /gov/* namespace (same behavior as ORG, different visual).
  // Goal: make ingestion artifacts / government bodies visually distinct without changing semantics.
  'GOV_ORG': {
    // Teal palette (institutional / public sector; distinct from ORG blue and GEO green).
    bg: '#CCFBF1', bgDark: '#042F2E',
    border: '#14B8A6', borderDark: '#2DD4BF',
    text: '#0F766E', textDark: '#99F6E4',
    accent: '#14B8A6', accentDark: '#2DD4BF',
  },
  // Market nodes (/market/*). Keep distinct from PRODUCT (amber) and ORG (blue).
  'MARKET': {
    // Amber / gold (market = money)
    bg: '#FEF3C7', bgDark: '#78350F',
    border: '#F59E0B', borderDark: '#FBBF24',
    text: '#92400E', textDark: '#FDE68A',
    accent: '#F59E0B', accentDark: '#FBBF24',
  },
  // DOMAIN: "Foundation plate" — warm stone/paper palette with stronger accent for left spine
  'DOMAIN': {
    bg: '#FAF7F4', bgDark: '#1F1B18',
    border: '#D4C8BE', borderDark: '#6B5F55',
    text: '#5C4F44', textDark: '#F0EAE5',
    accent: '#9C8574', accentDark: '#A89484', // warmer brown for left spine
  },
  'DECISION_POINT': {
    bg: '#FEF9C3', bgDark: '#422006',
    border: '#EAB308', borderDark: '#FACC15',
    text: '#854D0E', textDark: '#FEF08A',
    accent: '#EAB308', accentDark: '#FACC15',
  },
  'MECHANISM': {
    bg: '#E0F2FE', bgDark: '#0C4A6E',
    border: '#38BDF8', borderDark: '#7DD3FC',
    text: '#075985', textDark: '#BAE6FD',
    accent: '#38BDF8', accentDark: '#7DD3FC',
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
  'DEPOSIT': {
    // Earth / deposit: deeper soil/ore brown (avoid amber used by PRODUCT)
    bg: '#FBF2E8', bgDark: '#1A120C',
    border: '#6B3F2A', borderDark: '#B7791F',
    text: '#5A2E0D', textDark: '#F2D3B8',
    accent: '#8B4513', accentDark: '#C6903D',
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
    // Steel / slate palette to avoid clashing with MARKET (gold).
    bg: '#E2E8F0', bgDark: '#0F172A',
    border: '#64748B', borderDark: '#94A3B8',
    text: '#334155', textDark: '#E2E8F0',
    accent: '#64748B', accentDark: '#CBD5E1',
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
  // COMMUNITY: informal social groups (networks, communities, collectives)
  // Distinct from ORGANIZATION (blue) and PERSON (cyan) — use warm violet/grape palette
  'COMMUNITY': {
    bg: '#F3E8FF', bgDark: '#3B0764',
    border: '#A855F7', borderDark: '#C084FC',
    text: '#6B21A8', textDark: '#E9D5FF',
    accent: '#A855F7', accentDark: '#C084FC',
  },
  // COMMUNITY_DYNASTY: visual variant for dynasty communities (same palette, different icon)
  'COMMUNITY_DYNASTY': {
    bg: '#F3E8FF', bgDark: '#3B0764',
    border: '#A855F7', borderDark: '#C084FC',
    text: '#6B21A8', textDark: '#E9D5FF',
    accent: '#A855F7', accentDark: '#C084FC',
  },
  // COMMUNITY_TRIBE: visual variant for tribe communities (same palette, different icon)
  'COMMUNITY_TRIBE': {
    bg: '#F3E8FF', bgDark: '#3B0764',
    border: '#A855F7', borderDark: '#C084FC',
    text: '#6B21A8', textDark: '#E9D5FF',
    accent: '#A855F7', accentDark: '#C084FC',
  },
  // SACRED_TEXT_QURAN: visual variant for Quran (respectful icon)
  'SACRED_TEXT_QURAN': {
    bg: '#FEF3C7', bgDark: '#78350F',
    border: '#F59E0B', borderDark: '#FBBF24',
    text: '#92400E', textDark: '#FDE68A',
    accent: '#F59E0B', accentDark: '#FBBF24',
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
  const cacheKey = `${normalizedType}:${theme}`;
  const cached = (getNodeColors as any)._cache?.get(cacheKey);
  if (cached) return cached;
  const scheme = NODE_COLORS[normalizedType] || NODE_COLORS['default'];
  
  const out = {
    bg: theme === 'dark' ? scheme.bgDark : scheme.bg,
    border: theme === 'dark' ? scheme.borderDark : scheme.border,
    text: theme === 'dark' ? scheme.textDark : scheme.text,
    accent: theme === 'dark' ? scheme.accentDark : scheme.accent,
  };
  const m = (getNodeColors as any)._cache || new Map<string, any>();
  m.set(cacheKey, out);
  (getNodeColors as any)._cache = m;
  return out;
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
    @keyframes spin {
      from { transform: rotate(0deg); }
      to { transform: rotate(360deg); }
    }

    /* Compact node styles (for left pane groups in Levels layout) */
    .hzn-node-compact {
      transition: box-shadow 0.15s ease, border-color 0.15s ease;
    }
    .hzn-node-compact.is-selected {
      border-color: var(--hzn-accent, #3B82F6);
    }
    :root[data-hzn-perf-mode="true"] .hzn-node-compact {
      transition: none !important;
      box-shadow: none !important;
    }
    :root[data-hzn-perf-mode="true"] .hzn-node-compact.is-selected {
      box-shadow: 0 0 0 2px var(--hzn-accent-ring) !important;
    }

    .hzn-node-2026 {
      position: relative;
      overflow: hidden;
      border-radius: 14px;
      background: var(--hzn-surface);
      border: var(--hzn-border-width, 1px) solid var(--hzn-border);
      box-shadow: var(--hzn-shadow);
      transition: box-shadow 140ms ease, border-color 140ms ease, opacity 140ms ease;
      /* CSS containment for better performance.
         IMPORTANT: do NOT use paint here — nodes intentionally render UI that can extend outside
         the card bounds (stack previews, bottom controls). paint would clip them. */
      contain: layout style;
    }
    /* Domain nodes: "Foundation plate" style - double border + thick left spine */
    .hzn-node-2026.is-domain {
      border: 2px double var(--hzn-border);
      border-left: 4px solid var(--hzn-accent, var(--hzn-border));
      border-radius: 12px 14px 14px 12px;
      /* Ensure domains don't get the sharp corners if they overlap classes */
    }

    /* Strategic layer: Technical/Schematic look (Decision Points & Mechanisms).
       Goal: Look like a "logic layer" or "annotation" on top of the entity graph.
       Flat, sharp corners, high-contrast border, no shadow. */
    .hzn-node-2026.is-decision:not(.is-stack),
    .hzn-node-2026.is-mechanism:not(.is-stack),
    .hzn-node-compact.is-decision,
    .hzn-node-compact.is-mechanism {
      border-radius: 1px; /* Sharp, technical corners */
      box-shadow: none !important; /* Flat */
      /* Override the semantic colored border with a "ink" border */
      border: 1px solid var(--text-primary) !important;
      /* Ensure text is distinct */
      font-family: 'JetBrains Mono', monospace, ui-monospace; /* Optional: adds to technical feel */
    }

    /* Specific nuances if needed */
    .hzn-node-2026.is-decision {
      /* Decision: Keep the warm background but make it flat */
    }
    .hzn-node-2026.is-mechanism {
      /* Mechanism: Keep the cool background but make it flat */
    }

    /* Stack controllers (containers): visually distinct from entity nodes.
       Goal: fast role recognition without adding noisy chrome. */
    .hzn-node-2026.is-stack {
      /* Quiet aggregate card: no accent spine, no heavy border treatment. */
      border-radius: 14px;
      border: 1px solid var(--hzn-stack-border, rgba(148,163,184,0.18));
      box-shadow: var(--hzn-shadow);
    }
    .hzn-node-2026.is-stack::after { display: none; }
    [data-theme="light"] .hzn-node-2026.is-stack { border-color: rgba(100,116,139,0.20); }
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
    :root[data-hzn-node-visuals="minimal"] .hzn-node-2026.is-domain {
      border: 2px double var(--hzn-border);
      border-left: 4px solid var(--hzn-accent, var(--hzn-border));
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

const normalizeQuranKey = (value: string | null | undefined): { compact: string; words: string[] } => {
  const raw = String(value || '').toLowerCase();
  const normalized = raw.replace(/[^a-z]+/g, ' ').trim();
  const compact = normalized.replace(/\s+/g, '');
  const words = normalized ? normalized.split(/\s+/g) : [];
  return { compact, words };
};

const isQuranReference = (label?: string | null, canonicalPath?: string | null): boolean => {
  const cp = String(canonicalPath || '').toLowerCase();
  if (cp.includes('/quran') || cp.includes('/koran')) return true;
  const { compact, words } = normalizeQuranKey(label);
  if (compact.includes('quran') || compact.includes('koran')) return true;
  return words.includes('quran') || words.includes('koran');
};

export const formatHorizonFragmentType = (
  type: string | undefined,
  canonicalPath?: string | null,
  label?: string | null,
): string => {
  const t = String(type || '').trim();
  if (!t) return 'Unknown';
  if (t.toUpperCase() === 'PRODUCT' && isQuranReference(label, canonicalPath)) {
    return 'Sacred Text';
  }
  return t.replace(/_/g, ' ').toLowerCase().replace(/\b\w/g, (c) => c.toUpperCase());
};

// Visual-only type overrides (do NOT change semantics/behavior).
// Currently used to render /gov/* organizations differently while keeping them as ORGANIZATION logically.
export const getHorizonNodeVisualType = (
  type: string | undefined,
  canonicalPath?: string | null,
  label?: string | null,
): string => {
  const t = String(type || '').trim();
  const cp = String(canonicalPath || '').trim();
  // Any /gov/* fragment should look "government-ish", regardless of declared type.
  if (cp.startsWith('/gov/')) {
    return 'GOV_ORG';
  }
  // Any /market/* fragment should look like a market node, regardless of declared type.
  if (cp.startsWith('/market/')) {
    return 'MARKET';
  }
  // Community subtypes can have distinct visuals (e.g., dynasty).
  if (cp.startsWith('/community/')) {
    const parts = cp.split('/').filter(Boolean);
    const communityType = parts[2] || '';
    if (communityType === 'dynasty') return 'COMMUNITY_DYNASTY';
    if (communityType === 'tribe') return 'COMMUNITY_TRIBE';
  }
  if (String(t).toUpperCase() === 'PRODUCT' && isQuranReference(label, cp)) {
    return 'SACRED_TEXT_QURAN';
  }
  return t;
};

const getHorizonNodeDisplayType = (
  type: string | undefined,
  canonicalPath?: string | null,
  label?: string | null,
): string => {
  const t = String(type || '').trim();
  const cp = String(canonicalPath || '').trim().toLowerCase();
  if (!t) return 'Unknown';
  if (cp.startsWith('/community/')) {
    const parts = cp.split('/').filter(Boolean);
    const communityType = parts[2] || '';
    if (communityType === 'dynasty') return 'DYNASTY';
    if (communityType === 'tribe') return 'TRIBE';
  }
  if (t.toUpperCase() === 'PRODUCT' && isQuranReference(label, canonicalPath)) {
    return 'SACRED_TEXT';
  }
  return t;
};

// SVG иконки для разных типов
export const HorizonNodeIcon: React.FC<{ type: string | undefined; color: string; size?: number }> = ({ type, color, size = 16 }) => {
  const normalizedType = (type || '').toUpperCase().replace(/\s+/g, '_');
  const style: React.CSSProperties = { width: size, height: size, flexShrink: 0 };
  
  switch (normalizedType) {
    case 'MARKET':
      // Market: trend line (generic market icon)
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 17l6-6 4 4 7-7" />
          <path d="M14 8h6v6" />
        </svg>
      );

    case 'MARKET_RENEWABLES':
      // Renewables / clean energy: leaf + bolt
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M20 4c-7 0-12 5-12 12 0 2.5 1.2 4 3.2 4 5.8 0 8.8-7.2 8.8-16z" />
          <path d="M10 14l4-6-1 5h3l-5 7 1-6h-2z" />
        </svg>
      );

    case 'MARKET_ACCOUNTING':
      // Accounting: calculator
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <rect x="6" y="3" width="12" height="18" rx="2" />
          <path d="M8.5 7h7" />
          <path d="M9 11h.01" />
          <path d="M12 11h.01" />
          <path d="M15 11h.01" />
          <path d="M9 14h.01" />
          <path d="M12 14h.01" />
          <path d="M15 14h.01" />
          <path d="M9 17h6" />
        </svg>
      );

    case 'MARKET_CONSULTING':
      // Consulting: briefcase
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M10 6V5a2 2 0 0 1 2-2h0a2 2 0 0 1 2 2v1" />
          <rect x="3" y="6" width="18" height="14" rx="2" />
          <path d="M3 12h18" />
          <path d="M10 12v2h4v-2" />
        </svg>
      );

    case 'MARKET_AUTOMOTIVE':
      // Automotive / vehicles: compact car
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M5 16l1.2-4a3 3 0 0 1 2.9-2h6.8a3 3 0 0 1 2.9 2L22 16" />
          <path d="M3 16h18" />
          <path d="M6 16v2" />
          <path d="M18 16v2" />
          <circle cx="7.5" cy="18.5" r="1.5" />
          <circle cx="16.5" cy="18.5" r="1.5" />
          <path d="M8 10.5h8" />
        </svg>
      );

    case 'MARKET_MANUFACTURING':
      // Manufacturing / industry: gear
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M12 8.25a3.75 3.75 0 1 0 0 7.5 3.75 3.75 0 0 0 0-7.5z" />
          <path d="M19.4 15.2l1.1-1.9-1.7-3-2.2.3a7.9 7.9 0 0 0-1.1-1.1l.3-2.2-3-1.7-1.9 1.1a8.2 8.2 0 0 0-1.6 0L6.4 4.6l-3 1.7.3 2.2c-.4.35-.8.7-1.1 1.1l-2.2-.3-1.7 3 1.1 1.9a8.2 8.2 0 0 0 0 1.6l-1.1 1.9 1.7 3 2.2-.3c.35.4.7.8 1.1 1.1l-.3 2.2 3 1.7 1.9-1.1a8.2 8.2 0 0 0 1.6 0l1.9 1.1 3-1.7-.3-2.2c.4-.35.8-.7 1.1-1.1l2.2.3 1.7-3-1.1-1.9c.12-.52.12-1.08 0-1.6z" />
        </svg>
      );

    case 'MARKET_CONSUMER_ELECTRONICS':
      // Consumer electronics: phone + spark (device)
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <rect x="7" y="2.5" width="10" height="19" rx="2.2" />
          <path d="M11 19h2" />
          <path d="M4 9l1-1 1 1-1 1-1-1z" />
          <path d="M19 10l1-1 1 1-1 1-1-1z" />
        </svg>
      );

    case 'MARKET_LEGAL':
      // Legal: balance scales
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M12 3v18" />
          <path d="M7 6h10" />
          <path d="M7 6l-3 5h6l-3-5z" />
          <path d="M17 6l-3 5h6l-3-5z" />
          <path d="M6 21h12" />
        </svg>
      );

    case 'MARKET_TRADING':
      // Trading: bidirectional arrows
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M7 7h11" />
          <path d="M16 4l3 3-3 3" />
          <path d="M17 17H6" />
          <path d="M8 14l-3 3 3 3" />
        </svg>
      );

    case 'MARKET_ENERGY':
      // Oil & gas / energy: droplet + flame-ish mark
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M12 2s5 6 5 10a5 5 0 0 1-10 0c0-4 5-10 5-10z" />
          <path d="M9.5 14.5c.6 1.6 2 2.5 3.5 2.5" />
          <path d="M14.5 14.5c-.4 1.1-1.2 1.9-2.2 2.3" />
        </svg>
      );

    case 'MARKET_LOGISTICS':
      // Logistics / freight: truck + box
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 17V7a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v10" />
          <path d="M17 17h2a2 2 0 0 0 2-2v-4l-2-3h-4" />
          <path d="M7 17a2 2 0 1 0 0 4 2 2 0 0 0 0-4z" />
          <path d="M18 17a2 2 0 1 0 0 4 2 2 0 0 0 0-4z" />
          <path d="M7 9h6" />
          <path d="M7 12h6" />
        </svg>
      );

    case 'MARKET_FINANCE':
      // Credit card / finance icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <rect x="3" y="5" width="18" height="14" rx="2" />
          <path d="M3 10h18" />
          <path d="M7 15h4" />
        </svg>
      );

    case 'MARKET_CRYPTO':
      // Coin icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <circle cx="12" cy="12" r="8" />
          <path d="M12 7v10" />
          <path d="M9.5 9.5h4a2 2 0 0 1 0 4h-4" />
          <path d="M9.5 13.5h4a2 2 0 0 1 0 4h-4" />
        </svg>
      );

    case 'MARKET_REAL_ESTATE':
      // Home icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 11l9-7 9 7" />
          <path d="M5 10v10h14V10" />
          <path d="M10 20v-6h4v6" />
        </svg>
      );

    case 'MARKET_HEALTHCARE':
      // Medical cross
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M12 3v18" />
          <path d="M3 12h18" />
        </svg>
      );

    case 'MARKET_CONSTRUCTION':
      // Hammer icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M14 7l3 3" />
          <path d="M6 9l7-7 5 5-7 7" />
          <path d="M2 22l7-7" />
        </svg>
      );

    case 'MARKET_ENTERTAINMENT':
      // Film icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <rect x="3" y="6" width="18" height="12" rx="2" />
          <path d="M7 6v12" />
          <path d="M17 6v12" />
          <path d="M3 10h4" />
          <path d="M17 10h4" />
        </svg>
      );

    case 'MARKET_GAMBLING':
      // Dice icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <rect x="5" y="5" width="14" height="14" rx="3" />
          <path d="M9 9h.01" />
          <path d="M15 15h.01" />
          <path d="M15 9h.01" />
          <path d="M9 15h.01" />
          <path d="M12 12h.01" />
        </svg>
      );

    case 'MARKET_RETAIL':
      // Shopping bag icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M6 7h12l-1 14H7L6 7z" />
          <path d="M9 7a3 3 0 0 1 6 0" />
        </svg>
      );

    case 'MARKET_GOVERNMENT':
      // Landmark / building icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 10l9-5 9 5" />
          <path d="M4 10v10" />
          <path d="M20 10v10" />
          <path d="M8 20V11" />
          <path d="M12 20V11" />
          <path d="M16 20V11" />
          <path d="M2 20h20" />
        </svg>
      );

    case 'MARKET_EDUCATION':
      // Education / higher ed: book + bookmark
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M5 4h10a3 3 0 0 1 3 3v13l-3-2-3 2V7a3 3 0 0 0-3-3H5z" />
          <path d="M5 4v16a2 2 0 0 0 2 2h11" />
        </svg>
      );

    case 'GOV_ORG':
      // Government-like organization (same semantics as ORGANIZATION, different visual).
      // Use a landmark / courthouse icon to distinguish from regular corporate org nodes.
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 10l9-5 9 5" />
          <path d="M5 10v10" />
          <path d="M19 10v10" />
          <path d="M8 20V11" />
          <path d="M12 20V11" />
          <path d="M16 20V11" />
          <path d="M2 20h20" />
        </svg>
      );

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

    case 'COMMUNITY':
      // Community / network: group of connected people
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          {/* Central person */}
          <circle cx="12" cy="8" r="3" />
          <path d="M12 11v2" />
          {/* Left person */}
          <circle cx="5" cy="14" r="2.5" />
          <path d="M5 16.5v1.5" />
          {/* Right person */}
          <circle cx="19" cy="14" r="2.5" />
          <path d="M19 16.5v1.5" />
          {/* Connection lines */}
          <path d="M8 10l-3 4" />
          <path d="M16 10l3 4" />
          <path d="M7.5 15.5h9" />
        </svg>
      );

    case 'COMMUNITY_DYNASTY': {
      // Dynasty: crown icon (gold)
      const crownColor = '#D4AF37';
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={crownColor} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 6l4 4 5-6 5 6 4-4" />
          <path d="M4 10l2 8h12l2-8" />
          <path d="M6 18h12" />
        </svg>
      );
    }

    case 'COMMUNITY_TRIBE':
      // Tribe: shield icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M12 3l7 3v6c0 5-3.5 8-7 9-3.5-1-7-4-7-9V6l7-3z" />
          <path d="M8 9h8" />
          <path d="M12 9v9" />
        </svg>
      );

    case 'SACRED_TEXT_QURAN':
      // Quran: crescent moon icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M15 4a8 8 0 1 0 0 16a6 6 0 1 1 0-16z" />
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
    
    case 'DEPOSIT':
      // Deposit / resource: layered rock icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M3 16l4-6 5 3 5-6 4 9" />
          <path d="M4 20h16" />
          <path d="M6 18h12" />
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
    
    case 'PROGRAM':
      // Program / initiative: clipboard icon
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <rect x="6" y="4" width="12" height="16" rx="2" />
          <path d="M9 4a3 3 0 0 1 6 0" />
          <path d="M9 10h6" />
          <path d="M9 14h6" />
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
    
    case 'DECISION_POINT':
      // Decision point: compass/target
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <circle cx="12" cy="12" r="8" />
          <circle cx="12" cy="12" r="2.5" fill={color} />
          <path d="M12 4v2" />
          <path d="M12 18v2" />
          <path d="M4 12h2" />
          <path d="M18 12h2" />
        </svg>
      );

    case 'MECHANISM':
      // Mechanism: wrench
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={style}>
          <path d="M22 19l-6.5-6.5" />
          <path d="M16 10a5 5 0 1 1-2-4l-3 3 3 3 3-3z" />
          <path d="M2 22l4-4" />
        </svg>
      );

    case 'DOMAIN':
      // Domain: Capitol/Columns (foundation/institution)
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" style={style}>
          {/* Pediment (roof triangle) */}
          <path d="M3 10L12 4L21 10" />
          {/* Entablature (horizontal beam) */}
          <path d="M4 10h16v1H4z" fill={color} stroke="none" />
          {/* Columns */}
          <path d="M6 11v8" />
          <path d="M10 11v8" />
          <path d="M14 11v8" />
          <path d="M18 11v8" />
          {/* Base/Stylobate */}
          <path d="M3 19h18" strokeWidth="2" />
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

const segmentDomainKey = (segmentPath: string): string => {
  // Canonical: /segment/{domain}/{vertical}/{geo}
  const parts = String(segmentPath || '').trim().split('/').filter(Boolean);
  const domain = String(parts[1] || '').toLowerCase().trim();
  return domain || String(segmentPath || '').trim();
};

const topMarketSegmentNorms = (
  data: any,
  limit: number = 3,
): Array<{ segmentPath: string; normDomain: string; normVertical?: string; normPair: string; channel?: string }> => {
  const norms = Array.isArray(data?.marketSegmentNorms) ? (data.marketSegmentNorms as any[]) : [];
  const out: Array<{ segmentPath: string; normDomain: string; normVertical?: string; normPair: string; channel?: string }> = [];
  const seen = new Set<string>();
  for (const n of norms) {
    const normPair = String(n?.normPair || '').trim();
    const normDomain = String(n?.normDomain || '').trim();
    if (!normPair || !normDomain) continue;
    if (seen.has(normPair)) continue;
    seen.add(normPair);
    out.push({
      segmentPath: String(n?.segmentPath || '').trim(),
      normDomain,
      normVertical: String(n?.normVertical || '').trim() || undefined,
      normPair,
      channel: String(n?.channel || '').trim() || undefined,
    });
    if (out.length >= limit) break;
  }
  return out;
};

const topMarketSegments = (data: any, limit: number = 3): string[] => {
  const fromMarket = Array.isArray(data?.marketSegments) ? (data.marketSegments as any[]) : [];
  const fromIndex = Array.isArray(data?.indexPaths) ? (data.indexPaths as any[]) : [];
  const raw = (fromMarket.length > 0 ? fromMarket : fromIndex).map((x) => String(x || '').trim()).filter(Boolean);
  const segs = raw.filter((p) => p.startsWith('/segment/'));
  // Dedup by top-level domain so sub-segments don't show as duplicate icons (e.g., /segment/automotive/...).
  const seen = new Set<string>();
  const out: string[] = [];
  for (const s of segs) {
    const k = segmentDomainKey(s);
    if (!k || seen.has(k)) continue;
    seen.add(k);
    out.push(s);
    if (out.length >= limit) break;
  }
  return out;
};

const segmentIconType = (normDomain: string, normVertical?: string): string | null => {
  const d = String(normDomain || '').toLowerCase().trim();
  const v = String(normVertical || '').toLowerCase().trim();

  if (!d) return null;

  // Normalize common aliases for segment domain (we see both "finance" and "financial" in the wild).
  const d0 = d === 'financial' ? 'finance' : d;

  // Manufacturing / industrial production (very common in /segment/b2b/*)
  if (
    d0 === 'manufacturing' ||
    d0.includes('manufactur') ||
    d0 === 'machinery' ||
    d0.includes('machinery') ||
    v === 'manufacturing' ||
    v.includes('manufactur') ||
    v === 'machinery' ||
    v.includes('machinery')
  ) {
    return 'MARKET_MANUFACTURING';
  }

  // Consumer electronics (common in /segment/b2c/consumer-electronics/*)
  if (
    d0 === 'consumer-electronics' ||
    d0 === 'consumer-electronic' ||
    d0.includes('consumer-electronic') ||
    v === 'consumer-electronics' ||
    v.includes('consumer-electronic')
  ) {
    return 'MARKET_CONSUMER_ELECTRONICS';
  }

  // Renewables / storage: often encoded as vertical under energy domain.
  if (
    d0 === 'energy' &&
    (v === 'renewable-energy' ||
      v === 'renewable-energy-storage' ||
      v === 'renewables' ||
      v.includes('renewable') ||
      v.includes('solar') ||
      v.includes('wind') ||
      v.includes('storage') ||
      v.includes('battery'))
  ) {
    return 'MARKET_RENEWABLES';
  }

  // Professional services are often encoded in VERTICAL under broad domains like /segment/financial/* or /segment/b2b/*.
  // Prefer vertical-based mapping first (more specific), but also accept domain-based encoding.
  if (
    d0 === 'accounting' ||
    d0 === 'bookkeeping' ||
    d0 === 'audit' ||
    d0 === 'auditing' ||
    d0.includes('account') ||
    v === 'accounting' ||
    v === 'bookkeeping' ||
    v === 'audit' ||
    v === 'auditing' ||
    v === 'tax' ||
    v.includes('account') ||
    v.includes('audit') ||
    v.includes('tax')
  ) {
    return 'MARKET_ACCOUNTING';
  }
  if (
    d0 === 'consulting' ||
    d0 === 'advisory' ||
    d0 === 'management-consulting' ||
    d0.includes('consult') ||
    v === 'consulting' ||
    v === 'advisory' ||
    v === 'management-consulting' ||
    v === 'professional-services' ||
    v.includes('consult') ||
    v.includes('advis')
  ) {
    return 'MARKET_CONSULTING';
  }

  // Automotive / EV
  if (
    d0 === 'electric-vehicles' ||
    d0 === 'electric-vehicle' ||
    d0 === 'ev' ||
    d0 === 'automotive' ||
    d0 === 'vehicles' ||
    d0 === 'vehicle' ||
    d0 === 'auto' ||
    d0.includes('vehicle') ||
    d0.includes('automotive') ||
    d0.includes('electric-vehicle') ||
    // EV can be encoded as vertical under other domains (e.g. manufacturing/electric-vehicles).
    v === 'electric-vehicles' ||
    v === 'electric-vehicle' ||
    v === 'ev' ||
    v.includes('electric-vehicle')
  ) {
    return 'MARKET_AUTOMOTIVE';
  }

  // Legal / compliance / law
  if (
    d === 'legal' ||
    d === 'law' ||
    d === 'legal-services' ||
    d === 'compliance' ||
    d.includes('legal') ||
    d.includes('compliance')
  ) {
    return 'MARKET_LEGAL';
  }

  // Trading / import-export / wholesale
  if (
    d === 'trading' ||
    d === 'import-export' ||
    d === 'wholesale' ||
    d === 'wholesaling' ||
    d.includes('trading') ||
    d.includes('import') ||
    d.includes('export')
  ) {
    return 'MARKET_TRADING';
  }

  // "Commercial" is frequently used as a retail/commerce proxy.
  if (d === 'commercial' || d === 'commerce' || d === 'ecommerce' || d === 'e-commerce') {
    return 'MARKET_RETAIL';
  }

  // Energy / oil & gas (incl. services)
  if (
    d === 'energy' ||
    d === 'oil-and-gas' ||
    d === 'oil-and-gas-services' ||
    d.includes('oil') ||
    d.includes('gas')
  ) {
    return 'MARKET_ENERGY';
  }

  // Logistics / freight / shipping
  if (
    d0 === 'logistics' ||
    d0 === 'freight-forwarding' ||
    d0 === 'contract-logistics' ||
    d0 === 'supply-chain-management' ||
    d0 === 'shipping' ||
    d0 === 'transportation' ||
    d0 === 'supply-chain' ||
    d0.includes('freight') ||
    d0.includes('logistic')
  ) {
    return 'MARKET_LOGISTICS';
  }

  // Education (incl. higher-ed / universities / schools)
  if (
    d0 === 'education' ||
    d0 === 'higher-education' ||
    d0.includes('education') ||
    v === 'education' ||
    v === 'higher-education' ||
    v.includes('education') ||
    v.includes('university') ||
    v.includes('college') ||
    v.includes('school') ||
    v.includes('academy')
  ) {
    return 'MARKET_EDUCATION';
  }

  if (d0 === 'finance') {
    if (v === 'crypto') return 'MARKET_CRYPTO';
    return 'MARKET_FINANCE';
  }
  if (d === 'real-estate') return 'MARKET_REAL_ESTATE';
  if (d === 'healthcare' || v.includes('hospital') || v.includes('clinic') || v.includes('healthcare') || v.includes('medical'))
    return 'MARKET_HEALTHCARE';
  if (d === 'construction') return 'MARKET_CONSTRUCTION';
  if (d === 'retail') return 'MARKET_RETAIL';
  if (d === 'government' || d === 'public-administration' || d === 'defense') return 'MARKET_GOVERNMENT';
  if (d === 'entertainment') {
    if (v === 'casinos' || v === 'gambling') return 'MARKET_GAMBLING';
    return 'MARKET_ENTERTAINMENT';
  }
  if (d === 'gambling' || d === 'casinos') return 'MARKET_GAMBLING';
  if (d === 'technology') return 'TECHNOLOGY';
  return null;
};

/** Get edge stroke color by edge type (matches getEdgeStyle in LensCanvasCockpit) */
export const getEdgeColor = (edgeType: string | undefined): string => {
  const type = (edgeType || '').toUpperCase();
  // Competition/Conflict — Red
  if (['COMPETES_WITH', 'CONFLICTS_WITH', 'OPPOSES'].includes(type)) return '#EF4444';
  // Participation (org in market) — Teal (neutral, distinct from red competes)
  if (type === 'PARTICIPATES_IN') return '#0D9488';
  // Partnership/Cooperation — Green
  if (['PARTNERS_WITH', 'COOPERATES_WITH', 'SUPPLIES_TO', 'SUPPORTS'].includes(type)) return '#22C55E';
  // Products/Output — Cyan
  if (['PRODUCES', 'TRANSFORMS_INTO', 'EXTRACTS_FROM'].includes(type)) return '#06B6D4';
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
  // Market hierarchy (segment) — Gray dashed
  if (['HAS_MARKET_SEGMENT', 'HAS_SUBMARKET'].includes(type)) return '#9CA3AF';
  // Location — Purple
  if (['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'ASSURES', 'AUDITED_BY'].includes(type)) return '#A855F7';
  // Social graph (people-people) — Teal
  if (['RELATED_TO', 'RELATIVE_OF'].includes(type)) return '#06B6D4';
  // Default — Slate
  return '#64748B';
};

export const EdgeTypeIcon = ({ edgeType, color, size = 16 }: { edgeType: string; color: string; size?: number }) => {
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
    ASSURES: 'Assures',
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
    // Market / hierarchy
    PARTICIPATES_IN: 'Participates',
    DOMINATES: 'Dominates',
    HAS_MARKET_SEGMENT: 'Segment',
    HAS_SUBMARKET: 'Segment', // deprecated
    HAS_SUBGROUP: 'Subgroup',
    ADMINISTERS: 'Administers',
    // Community relationships
    OPERATES_VIA: 'Via',
    MEMBER_OF: 'Member',
    AFFILIATED_WITH: 'Affiliated',
    // Extractive
    EXTRACTS_FROM: 'Extracts',
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
  // Strategic overlay relations
  CONTROLS_ACCESS: {
    light: 'rgba(168, 85, 247, 0.10)',
    dark: 'rgba(168, 85, 247, 0.12)',
    borderLight: 'rgba(168, 85, 247, 0.50)',
    borderDark: 'rgba(192, 132, 252, 0.55)',
  },
  IMPOSES_CONDITION: {
    light: 'rgba(168, 85, 247, 0.10)',
    dark: 'rgba(168, 85, 247, 0.12)',
    borderLight: 'rgba(168, 85, 247, 0.50)',
    borderDark: 'rgba(192, 132, 252, 0.55)',
  },
  GATEKEEPS_DECISION: {
    light: 'rgba(168, 85, 247, 0.10)',
    dark: 'rgba(168, 85, 247, 0.12)',
    borderLight: 'rgba(168, 85, 247, 0.50)',
    borderDark: 'rgba(192, 132, 252, 0.55)',
  },
  USES_MECHANISM: {
    light: 'rgba(168, 85, 247, 0.10)',
    dark: 'rgba(168, 85, 247, 0.12)',
    borderLight: 'rgba(168, 85, 247, 0.50)',
    borderDark: 'rgba(192, 132, 252, 0.55)',
  },
  APPLIES_TO: {
    light: 'rgba(168, 85, 247, 0.10)',
    dark: 'rgba(168, 85, 247, 0.12)',
    borderLight: 'rgba(168, 85, 247, 0.50)',
    borderDark: 'rgba(192, 132, 252, 0.55)',
  },
  SURFACES: {
    light: 'rgba(234, 179, 8, 0.10)',
    dark: 'rgba(234, 179, 8, 0.12)',
    borderLight: 'rgba(234, 179, 8, 0.50)',
    borderDark: 'rgba(250, 204, 21, 0.55)',
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
  const rf = useReactFlow();
  const [hoveredRelKey, setHoveredRelKey] = React.useState<string | null>(null);
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

  // Stack group (expanded right-side stacks)
  const isStackGroup = Boolean((data as any)?.isStackGroup);
  const stackGroupKey = String((data as any)?.stackGroupKey || '').trim();
  const stackGroupExpanded = Boolean((data as any)?.stackGroupExpanded);
  const onToggleStack = (data as any)?.onToggleStack as ((key: string, next: boolean) => void) | undefined;

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
    // Keep groups mostly "no-fill" to avoid fighting the palette.
    // If we ever want a fill here, it should be an extremely subtle cold graphite.
    bg = 'transparent';
    border = theme === 'dark' ? 'rgba(148, 163, 184, 0.42)' : 'rgba(100, 116, 139, 0.34)';
  }

  // Stack groups should be visually lightweight (closer to left-pane groups).
  const stackGroupVisualType = isStackGroup ? String((data as any)?.stackVisualType || '').trim() : '';
  const stackGroupAccent = stackGroupVisualType
    ? getNodeColors(stackGroupVisualType, theme).accent
    : (theme === 'dark' ? '#94A3B8' : '#64748B');
  if (isStackGroup) {
    bg = theme === 'dark' ? 'rgba(148, 163, 184, 0.07)' : 'rgba(15, 23, 42, 0.035)';
    border = theme === 'dark' ? 'rgba(148, 163, 184, 0.28)' : 'rgba(100, 116, 139, 0.22)';
  }

  const titleColor = isStackGroup
    ? (theme === 'dark' ? 'rgba(255,255,255,0.72)' : 'rgba(15,23,42,0.72)')
    : (theme === 'dark' ? 'rgba(255,255,255,0.9)' : 'rgba(15,23,42,0.9)');
  const subtitleColor = isStackGroup
    ? (theme === 'dark' ? 'rgba(255,255,255,0.48)' : 'rgba(15,23,42,0.5)')
    : (theme === 'dark' ? 'rgba(255,255,255,0.62)' : 'rgba(15,23,42,0.62)');
  const count = typeof data.groupCount === 'number' ? data.groupCount : undefined;
  const ring = selected ? (theme === 'dark' ? 'rgba(255,255,255,0.35)' : 'rgba(15,23,42,0.20)') : undefined;
  const borderWidth = isStackGroup ? 0 : (noFill ? 2 : 1.5);
  const borderStyle = isStackGroup ? 'solid' : 'dashed';
  // Border mode for group containers:
  // - outline (default): full rectangle
  // - none: no border
  // - divider-left / divider-right: only a single vertical dashed divider (used for Levels left-pane columns)
  const borderMode = String((data as any)?.groupBorderMode || 'outline').trim().toLowerCase();
  const fullBorderCss = `${borderWidth}px ${borderStyle} ${border}`;
  const dividerBorderCss = `1px dashed ${border}`;
  const showExpandBracket = Boolean((data as any)?.groupShowExpandBracket);

  // Optional subtle background tint (used for Levels root group instead of an outline).
  const bgTint = String((data as any)?.groupBgTint || '').trim().toLowerCase();
  if (bgTint === 'subtle') {
    // Keep *very* subtle — just enough to separate from pure black.
    bg = theme === 'dark' ? 'rgba(255,255,255,0.015)' : 'rgba(15,23,42,0.03)';
  }

  // Category tint: subtle background per edge type (left pane relation blocks)
  const categoryTint = String((data as any)?.groupCategoryTint || '').trim();
  if (categoryTint && noFill) {
    const edgeColor = getEdgeColor(categoryTint);
    bg = hexToRgba(edgeColor, theme === 'dark' ? 0.06 : 0.05);
  }

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
  const edgePortsRaw = (data as any)?.groupEdgePorts as
    | Array<{ edgeType: string; targetHandleId: string; sourceHandleId: string; y: number }>
    | undefined;
  const hasEdgePorts = Array.isArray(edgePortsRaw) && edgePortsRaw.length > 0;
  // Stack groups (right pane) may omit groupEdgePorts; add a default port so ReactFlow edges always find a handle.
  const fallbackEdgePortsFromTypes =
    !hasEdgePorts && edgeTypes.length > 0
      ? edgeTypes.map((et, idx) => {
          const edgeType = normalizeEdgeType(et);
          return {
            edgeType,
            targetHandleId: `edge:${edgeType}:target`,
            sourceHandleId: `edge:${edgeType}:source`,
            // Approximate vertical placement; exact position is less critical than having a stable handle.
            y: 40 + idx * 18,
          };
        })
      : [];
  const fallbackEdgePorts =
    !hasEdgePorts && isStackGroup
      ? [{ edgeType: 'OTHER', targetHandleId: 'target-left', sourceHandleId: 'source-left', y: 60 }]
      : fallbackEdgePortsFromTypes;
  const edgePorts: Array<{ edgeType: string; targetHandleId: string; sourceHandleId: string; y: number }> =
    hasEdgePorts ? edgePortsRaw! : fallbackEdgePorts;
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
  const subtitle = isStackGroup ? '' : formatEdgeTypes(edgeTypes);

  // Owner chip (single owner by product definition).
  const ownerId = String((data as any)?.groupOwnerFragmentId || data.groupParentId || '').trim();
  const ownerLabel = String((data as any)?.groupOwnerLabel || '').trim();
  const ownerType = String((data as any)?.groupOwnerType || '').trim();
  const ownerAccent = ownerType ? getNodeColors(ownerType, theme === 'dark' ? 'dark' : 'light').accent : border;
  const showOwnerChip = Boolean(ownerId && ownerLabel);

  const focusOwner = React.useCallback(() => {
    if (!ownerId) return;
    try {
      const node: any = rf.getNode?.(ownerId);
      if (!node) return;
      const x0 = (node.positionAbsolute?.x ?? node.position?.x ?? 0);
      const y0 = (node.positionAbsolute?.y ?? node.position?.y ?? 0);
      const w = typeof node.width === 'number' ? node.width : 180;
      const h = typeof node.height === 'number' ? node.height : 160;
      const cx = x0 + w / 2;
      const cy = y0 + h / 2;
      const z = rf.getZoom?.();
      rf.setCenter?.(cx, cy, { zoom: typeof z === 'number' ? z : undefined, duration: 360 });
    } catch {}
  }, [ownerId, rf]);

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

  // Optional: footer label (used by nested relation containers in Levels left pane)
  const footerTitle = String((data as any)?.groupFooterTitle || '').trim();
  const footerEdgeTypeKey = String((data as any)?.groupFooterEdgeTypeKey || '').trim();
  const footerNodeCount = typeof (data as any)?.groupFooterNodeCount === 'number' ? (data as any).groupFooterNodeCount : undefined;
  const footerCollapsed = Boolean((data as any)?.groupFooterCollapsed);
  const footerFirstNodeId = String((data as any)?.groupFooterFirstNodeId || '').trim();
  const footerGroupIdForToggle = String((data as any)?.groupId || id).trim();
  const footerCanToggle = Boolean(footerEdgeTypeKey && typeof (data as any)?.onToggleSubcategoryCollapse === 'function');
  const showFooterCollapsePill = Boolean(footerCanToggle && !footerCollapsed && typeof footerNodeCount === 'number' && footerNodeCount > 1);
  const overflowCount = typeof (data as any)?.groupOverflowCount === 'number' ? (data as any).groupOverflowCount : 0;
  const overflowKey = String((data as any)?.groupOverflowKey || '').trim();
  const overflowExpanded = Boolean((data as any)?.groupOverflowExpanded);
  const canToggleOverflow = Boolean(overflowKey && typeof (data as any)?.onToggleRoleOverflow === 'function');

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
        border: isStackGroup
          ? 'none'
          : (borderMode === 'outline' ? fullBorderCss : 'none'),
        borderLeft: isStackGroup
          ? 'none'
          : (borderMode === 'divider-left' ? dividerBorderCss : undefined),
        borderRight: borderMode === 'divider-right' ? dividerBorderCss : undefined,
        borderRadius: (borderMode === 'outline' || isStackGroup || bgTint === 'subtle') ? 12 : 0,
        boxShadow: isStackGroup
          ? (theme === 'dark' ? '0 2px 8px rgba(0,0,0,0.12)' : '0 2px 8px rgba(0,0,0,0.05)')
          : ((noFill && !ring)
            ? 'none'
            : ((theme === 'dark' ? '0 2px 10px rgba(0,0,0,0.25)' : '0 2px 10px rgba(0,0,0,0.08)') +
              (ring ? `, 0 0 0 3px ${ring}` : ''))),
        // Groups are real containers in Freeform/Linear pagination modes; they must allow interaction
        // (pagination buttons). Backdrop-only groups should not intercept clicks unless they contain
        // an interactive overflow pill.
        pointerEvents: (isBackdrop && !canToggleOverflow) ? 'none' : 'auto',
        position: 'relative',
        // Allow the owner chip to sit on the border without being clipped.
        overflow: 'visible',
        cursor: interactive ? 'grab' : 'default',
      }}
    >
      {showOwnerChip && (
        <button
          type="button"
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            focusOwner();
          }}
          title={ownerType ? `${ownerLabel} · ${formatHorizonFragmentType(ownerType)}` : ownerLabel}
          style={{
            position: 'absolute',
            top: -12,
            left: 12,
            zIndex: 20,
            display: 'inline-flex',
            alignItems: 'center',
            gap: 8,
            maxWidth: 'min(360px, calc(100% - 24px))',
            padding: '6px 10px',
            borderRadius: 999,
            border: `1px solid ${ownerAccent}`,
            background: theme === 'dark' ? 'rgba(2, 6, 23, 0.88)' : 'rgba(255,255,255,0.92)',
            color: titleColor,
            cursor: 'pointer',
            boxShadow: theme === 'dark' ? '0 10px 30px rgba(0,0,0,0.35)' : '0 10px 30px rgba(0,0,0,0.15)',
          }}
        >
          {ownerType && <HorizonNodeIcon type={ownerType} color={ownerAccent} size={14} />}
          <span
            style={{
              fontSize: 12,
              fontWeight: 850,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              minWidth: 0,
            }}
          >
            {ownerLabel}
          </span>
        </button>
      )}
      {/* Bracket "[" embracing expanded nodes on the left (context card expansion). */}
      {showExpandBracket && (
        <div
          style={{
            position: 'absolute',
            left: 0,
            top: 12,
            bottom: 12,
            width: 8,
            pointerEvents: 'none',
            borderLeft: `2px solid ${border}`,
            borderTopLeftRadius: 6,
            borderBottomLeftRadius: 6,
            opacity: theme === 'dark' ? 0.5 : 0.45,
          }}
        />
      )}
      {/* Optional: Type subsections (visual separators + labels). Keep pointerEvents off. */}
      {showTypeBlocks && (
        <>
          {/* Type dividers: barely visible (category tint used for identification) */}
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
                  opacity: theme === 'dark' ? 0.14 : 0.12,
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
                  color: theme === 'dark' ? 'rgba(255,255,255,0.66)' : 'rgba(15,23,42,0.66)',
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
          {/* Subcategory dividers: barely visible (category tint used for identification) */}
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
                    opacity: theme === 'dark' ? 0.12 : 0.10,
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
              const relHoverKey = `${groupIdForToggle}:${edgeTypeKey || label || `${bIdx}-${rIdx}`}`;
              const isRelHovered = hoveredRelKey === relHoverKey;
              const relTitleColor =
                theme === 'dark' ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.88)';
              const relLabelColor = isRelHovered ? relTitleColor : subtitleColor;
              const relLabelOpacity = isRelHovered ? 0.96 : (theme === 'dark' ? 0.72 : 0.72);
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
                    fontWeight: 800,
                    color: relLabelColor,
                    textTransform: 'uppercase',
                    letterSpacing: '0.25px',
                    opacity: relLabelOpacity,
                    pointerEvents: 'auto',
                    lineHeight: '16px',
                    transition: 'opacity 120ms ease, color 120ms ease',
                  }}
                  title={label}
                  onMouseEnter={() => setHoveredRelKey(relHoverKey)}
                  onMouseLeave={() => setHoveredRelKey((cur) => (cur === relHoverKey ? null : cur))}
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
                      onMouseEnter={() => setHoveredRelKey(relHoverKey)}
                      onMouseLeave={() => setHoveredRelKey((cur) => (cur === relHoverKey ? null : cur))}
                      style={{
                        pointerEvents: 'auto',
                        position: 'absolute',
                        right: 0,
                        top: 0,
                        height: 16,
                        padding: '0 6px',
                        borderRadius: 999,
                        border: `1px solid ${
                          theme === 'dark'
                            ? (isRelHovered ? 'rgba(255,255,255,0.32)' : 'rgba(255,255,255,0.18)')
                            : (isRelHovered ? 'rgba(15,23,42,0.22)' : 'rgba(15,23,42,0.14)')
                        }`,
                        background: theme === 'dark'
                          ? (isRelHovered ? 'rgba(0,0,0,0.40)' : 'rgba(0,0,0,0.26)')
                          : (isRelHovered ? 'rgba(255,255,255,0.90)' : 'rgba(255,255,255,0.78)'),
                        color: isRelHovered ? relTitleColor : subtitleColor,
                        cursor: 'pointer',
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 6,
                        fontSize: 10,
                        fontWeight: 850,
                        lineHeight: 1,
                        transition: 'all 120ms ease',
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

      {/* Aggregation ports (edge types): on the RIGHT edge for left groups, spaced vertically. */}
      {showEdgePorts && !isStackGroup && (
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

      {/* Stack group visible ports are rendered on the LEFT edge. */}
      {showEdgePorts && isStackGroup && (
        <>
          {edgePorts!.map((p, idx) => {
            const y = typeof p?.y === 'number' ? p.y : undefined;
            if (typeof y !== 'number') return null;
            const targetId = String(p?.targetHandleId || '').trim() || `edge:${String(p?.edgeType || 'OTHER').trim()}:target`;
            const sourceId = String(p?.sourceHandleId || '').trim() || `edge:${String(p?.edgeType || 'OTHER').trim()}:source`;
            return (
              <React.Fragment key={`stack-port-${idx}-${targetId}-${sourceId}`}>
                <Handle
                  id={targetId}
                  type="target"
                  position={Position.Left}
                  className="hzn-handle"
                  style={{
                    background: border,
                    width: '10px',
                    height: '10px',
                    border: `2px solid ${theme === 'dark' ? 'rgba(15,23,42,0.75)' : 'rgba(255,255,255,0.95)'}`,
                    left: '-5px',
                    top: `${y}px`,
                    opacity: 0.85,
                    pointerEvents: 'none',
                  }}
                />
                <Handle
                  id={sourceId}
                  type="source"
                  position={Position.Left}
                  className="hzn-handle"
                  style={{
                    background: border,
                    width: '10px',
                    height: '10px',
                    border: `2px solid ${theme === 'dark' ? 'rgba(15,23,42,0.75)' : 'rgba(255,255,255,0.95)'}`,
                    left: '-5px',
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
      {/* Fallback lateral handles for stack groups.
          Proxy edges may resolve to left or right side depending on geometry.
          Always render them so React Flow can resolve the handle reference. */}
      {isStackGroup && (() => {
        const hasSourceLeft = edgePorts.some((p) => p?.sourceHandleId === 'source-left');
        const hasTargetLeft = edgePorts.some((p) => p?.targetHandleId === 'target-left');
        const hasSourceRight = edgePorts.some((p) => p?.sourceHandleId === 'source-right');
        const hasTargetRight = edgePorts.some((p) => p?.targetHandleId === 'target-right');
        if (hasSourceLeft && hasTargetLeft && hasSourceRight && hasTargetRight) return null;
        return (
          <>
            {!hasTargetLeft && (
              <Handle
                id="target-left"
                type="target"
                position={Position.Left}
                className="hzn-handle"
                style={{ background: border, width: '10px', height: '10px',
                  border: `2px solid ${theme === 'dark' ? 'rgba(15,23,42,0.75)' : 'rgba(255,255,255,0.95)'}`,
                  left: '-5px', top: '50%', opacity: 0, pointerEvents: 'none' }}
              />
            )}
            {!hasSourceLeft && (
              <Handle
                id="source-left"
                type="source"
                position={Position.Left}
                className="hzn-handle"
                style={{ background: border, width: '10px', height: '10px',
                  border: `2px solid ${theme === 'dark' ? 'rgba(15,23,42,0.75)' : 'rgba(255,255,255,0.95)'}`,
                  left: '-5px', top: '50%', opacity: 0, pointerEvents: 'none' }}
              />
            )}
            {!hasTargetRight && (
              <Handle
                id="target-right"
                type="target"
                position={Position.Right}
                className="hzn-handle"
                style={{ background: border, width: '10px', height: '10px',
                  border: `2px solid ${theme === 'dark' ? 'rgba(15,23,42,0.75)' : 'rgba(255,255,255,0.95)'}`,
                  right: '-5px', top: '50%', opacity: 0, pointerEvents: 'none' }}
              />
            )}
            {!hasSourceRight && (
              <Handle
                id="source-right"
                type="source"
                position={Position.Right}
                className="hzn-handle"
                style={{ background: border, width: '10px', height: '10px',
                  border: `2px solid ${theme === 'dark' ? 'rgba(15,23,42,0.75)' : 'rgba(255,255,255,0.95)'}`,
                  right: '-5px', top: '50%', opacity: 0, pointerEvents: 'none' }}
              />
            )}
          </>
        );
      })()}

      {/* Header (optional) */}
      {!hideHeader && (
        <div
          style={{
            position: 'absolute',
            left: 12,
            top: 10,
            right: 12,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 10,
            whiteSpace: 'nowrap',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, maxWidth: isStackGroup ? '85%' : '70%' }}>
            {!isStackGroup && icon && <span style={{ fontSize: 12 }}>{icon}</span>}
            <div style={{
              fontSize: 11,
              fontWeight: isStackGroup ? 600 : 700,
              letterSpacing: isStackGroup ? '0.15px' : '0.5px',
              textTransform: isStackGroup ? 'none' : 'uppercase',
              color: titleColor,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              minWidth: 0,
            }}>
              {title}
            </div>
            {!isStackGroup && count !== undefined && (
              <div style={{ fontSize: 11, fontWeight: 600, color: subtitleColor }}>
                ({count})
              </div>
            )}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {!isStackGroup && subtitle && (
              <div
                style={{
                  fontSize: 9,
                  fontWeight: 500,
                  color: subtitleColor,
                  textTransform: 'uppercase',
                  letterSpacing: '0.3px',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  maxWidth: 110,
                }}
                title={edgeTypes.join(', ')}
              >
                {subtitle}
              </div>
            )}
            {/* Stack group collapse button */}
            {isStackGroup && stackGroupExpanded && onToggleStack && stackGroupKey && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onToggleStack(stackGroupKey, false);
                }}
                aria-label="Collapse stack"
                title="Collapse stacked nodes"
                style={{
                  width: 24,
                  height: 24,
                  borderRadius: 9,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.16)' : 'rgba(15,23,42,0.12)'}`,
                  background: theme === 'dark' ? 'rgba(2, 6, 23, 0.28)' : 'rgba(255, 255, 255, 0.74)',
                  color: theme === 'dark' ? 'rgba(255,255,255,0.68)' : 'rgba(15,23,42,0.58)',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  boxShadow: 'none',
                  pointerEvents: 'auto',
                }}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M18 15l-6-6-6 6" />
                </svg>
              </button>
            )}
            {/* Expanded business block: collapse-back to summary card */}
            {(() => {
              const blockCollapseKey = String((data as any)?.groupBlockCollapseKey || '').trim();
              const blockCollapseGroupId = String((data as any)?.groupBlockCollapseGroupId || '').trim();
              const toggleFn = (data as any)?.onToggleSubcategoryCollapse as ((gid: string, et: string, collapsed: boolean) => void) | undefined;
              if (!blockCollapseKey || !blockCollapseGroupId || typeof toggleFn !== 'function') return null;
              return (
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    e.preventDefault();
                    toggleFn(blockCollapseGroupId, blockCollapseKey, true);
                  }}
                  onPointerDown={(e) => e.stopPropagation()}
                  aria-label="Collapse back"
                  title="Collapse back"
                  style={{
                    width: 24,
                    height: 24,
                    position: 'absolute',
                    right: 12,
                    bottom: 12,
                    borderRadius: 9,
                    border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                    background: theme === 'dark' ? 'rgba(245, 158, 11, 0.28)' : 'rgba(245, 158, 11, 0.18)',
                    color: theme === 'dark' ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.78)',
                    cursor: 'pointer',
                    pointerEvents: 'auto',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    boxShadow: theme === 'dark' ? '0 8px 18px rgba(0,0,0,0.42)' : '0 8px 18px rgba(0,0,0,0.12)',
                    zIndex: 3,
                  }}
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M15 18l-6-6 6-6" />
                  </svg>
                </button>
              );
            })()}
          </div>
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

      {/* Footer label (Bottom): used for relation subcategory containers in Levels left pane */}
      {footerTitle && (
        <div
          style={{
            position: 'absolute',
            bottom: 6,
            left: 0,
            right: 0,
            height: 16,
            fontSize: 10,
            fontWeight: 800,
            color: subtitleColor,
            textTransform: 'uppercase',
            letterSpacing: '0.25px',
            opacity: theme === 'dark' ? 0.72 : 0.72,
            pointerEvents: 'auto',
            lineHeight: '16px',
          }}
          title={footerTitle}
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
              paddingRight: showFooterCollapsePill ? 34 : 0,
            }}
          >
            {footerTitle}
          </span>

          {showFooterCollapsePill && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                (data as any)?.onToggleSubcategoryCollapse?.(
                  footerGroupIdForToggle,
                  footerEdgeTypeKey,
                  true,
                  footerFirstNodeId || undefined,
                );
              }}
              aria-label={`Collapse ${footerTitle} (${footerNodeCount})`}
              title={`Collapse ${footerTitle} (${footerNodeCount})`}
              style={{
                pointerEvents: 'auto',
                position: 'absolute',
                right: 10,
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
              <span>{footerNodeCount}</span>
              <svg
                width="10"
                height="10"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.4"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M18 15l-6-6-6 6" />
              </svg>
            </button>
          )}
        </div>
      )}

      {/* "+N more" overflow pill: shown when a semantic category has been capped */}
      {(overflowCount > 0 || overflowExpanded) && (
        <div
          style={{
            position: 'absolute',
            bottom: 4,
            left: 0,
            right: 0,
            display: 'flex',
            justifyContent: 'center',
            pointerEvents: canToggleOverflow ? 'auto' : 'none',
          }}
        >
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              if (!canToggleOverflow) return;
              (data as any)?.onToggleRoleOverflow?.(overflowKey, !overflowExpanded);
            }}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 4,
              padding: '2px 8px',
              borderRadius: 999,
              fontSize: 10,
              fontWeight: 700,
              letterSpacing: '0.2px',
              color: theme === 'dark' ? 'rgba(255,255,255,0.78)' : 'rgba(15,23,42,0.68)',
              background: theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.08)',
              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.18)' : 'rgba(15,23,42,0.14)'}`,
              cursor: canToggleOverflow ? 'pointer' : 'default',
              pointerEvents: canToggleOverflow ? 'auto' : 'none',
            }}
            title={overflowExpanded ? 'Show less' : `Show ${overflowCount} more`}
          >
            {overflowExpanded ? 'Show less' : `+${overflowCount} more`}
          </button>
        </div>
      )}
    </div>
  );
});

// Business block context card: rich-text summary for left pane (Market & Regulation, Products & Supply, etc.)
const HorizonContextCardNode: React.FC<NodeProps<HorizonNodeData>> = React.memo(({ data, id }) => {
  const { theme } = React.useContext(ThemeContext);
  const title = String((data as any)?.contextTitle || 'Context').trim();
  const contextLines = (data as any)?.contextLines;
  // Support both old format (string[]) and new format (structured data)
  const isOldFormat = Array.isArray(contextLines) && contextLines.length > 0 && typeof contextLines[0] === 'string';
  const facts: Array<{ label: string; items: Array<{ id: string; label: string; type: string }> }> = isOldFormat
    ? []
    : (Array.isArray(contextLines) ? contextLines : []);
  const oldLines: string[] = isOldFormat ? (contextLines as string[]) : [];
  const count = typeof (data as any)?.contextCount === 'number' ? (data as any).contextCount : 0;
  const onShowDetails = (data as any)?.onToggleContextDetails as (() => void) | undefined;
  const contextExpanded = Boolean((data as any)?.contextExpanded);
  const isRightStackContextCard = Boolean((data as any)?.rightStackContextCard);

  const isDark = theme === 'dark';
  // Graphite background: one tone above main canvas (#121212 → #1A1A1A), subtle, not loud
  const bg = isDark ? 'rgba(26,26,26,0.98)' : 'rgba(248,250,252,0.97)';
  const border = isDark ? 'rgba(148,163,184,0.12)' : 'rgba(148,163,184,0.18)';
  const titleColor = isDark ? 'rgba(248,250,252,0.88)' : 'rgba(15,23,42,0.82)';
  const labelColor = isDark ? 'rgba(148,163,184,0.65)' : 'rgba(71,85,105,0.65)';
  const lineColor = isDark ? 'rgba(148,163,184,0.82)' : 'rgba(71,85,105,0.78)';
  // CTA styling: match node expand/collapse affordance (same border, shadow, amber active state).
  const ctaBorder = isDark ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)';
  const ctaBgIdle = isDark ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.78)';
  const ctaBgActive = isDark ? 'rgba(245, 158, 11, 0.28)' : 'rgba(245, 158, 11, 0.18)';
  const ctaText = isDark ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.78)';
  const ctaShadow = isDark ? '0 8px 18px rgba(0,0,0,0.42)' : '0 8px 18px rgba(0,0,0,0.12)';

  const fadeHeight = 40;
  const gradientOverlay = `linear-gradient(to top, ${bg} 0%, transparent 100%)`;

  return (
    <div
      style={{
        width: '100%',
        height: '100%',
        borderRadius: 10,
        border: `1px solid ${border}`,
        background: bg,
        padding: '10px 12px 8px',
        display: 'flex',
        flexDirection: 'column',
        boxShadow: isDark ? '0 2px 8px rgba(0,0,0,0.25)' : '0 1px 4px rgba(0,0,0,0.06)',
        overflow: 'hidden',
        pointerEvents: 'auto',
        position: 'relative',
      }}
    >
      {/* Title row */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, paddingBottom: 8, marginBottom: 8, flexShrink: 0 }}>
        <div
          style={{
            fontSize: 11,
            fontWeight: 700,
            color: titleColor,
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
            lineHeight: 1.2,
            flex: 1,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {title}
        </div>
        {count > 0 && (
          <div
            style={{
              fontSize: 9,
              fontWeight: 600,
              color: lineColor,
              opacity: 0.7,
              flexShrink: 0,
            }}
          >
            {count}
          </div>
        )}
      </div>
      {/* Fact lines / Structured facts — extends into button area with bottom fade */}
      <div
        className="nowheel nodrag nopan"
        onWheelCapture={(e) => e.stopPropagation()}
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 8,
          flex: 1,
          minHeight: 0,
          overflowY: 'auto',
          paddingRight: 2,
          paddingBottom: fadeHeight,
        }}
      >
        {isOldFormat ? (
          // Legacy format: plain text lines
          <>
            {oldLines.map((line: string, idx: number) => (
              <div
                key={idx}
                style={{
                  fontSize: 11,
                  color: lineColor,
                  lineHeight: 1.35,
                  whiteSpace: 'normal',
                  wordBreak: 'break-word',
                }}
                title={line}
              >
                {line}
              </div>
            ))}
            {oldLines.length === 0 && count > 0 && (
              <div style={{ fontSize: 11, color: lineColor, opacity: 0.6 }}>
                {count} related items
              </div>
            )}
          </>
        ) : (
          // New format: structured facts with badges
          <>
            {facts.map((fact, factIdx, arr) => (
              <div
                key={factIdx}
                style={{
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 4,
                  paddingBottom: factIdx < arr.length - 1 ? 6 : 0,
                }}
              >
                {/* Label row */}
                <div
                  style={{
                    fontSize: 10.5,
                    fontWeight: 600,
                    color: labelColor,
                    letterSpacing: '0.2px',
                    lineHeight: 1.2,
                  }}
                >
                  {fact.label}
                </div>
                {/* Badges row */}
                <div
                  style={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 4,
                    alignItems: 'center',
                  }}
                >
                  {fact.items.map((item) => {
                    const isOverflow = item.id.startsWith('__overflow__');
                    const nodeType = item.type || 'UNKNOWN';
                    const visualType = getHorizonNodeVisualType(nodeType, '', item.label);
                    const colors = getNodeColors(visualType, theme);
                    const badgeAccent = colors.accent;
                    // Mute the accent color for border (reduce opacity)
                    const badgeAccentMuted = hexToRgba(badgeAccent, isDark ? 0.4 : 0.5);
                    // Very low opacity background using accent color
                    const badgeBgSubtle = hexToRgba(badgeAccent, 0.05);
                    const badgeTextColorBright = isDark ? 'rgba(255,255,255,0.95)' : 'rgba(15,23,42,0.95)';

                    return (
                      <div
                        key={item.id}
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: 4,
                          padding: '3px 6px',
                          borderRadius: 6,
                          border: `1px solid ${badgeAccentMuted}`,
                          background: badgeBgSubtle,
                          fontSize: 10,
                          color: isOverflow ? labelColor : badgeTextColorBright,
                          fontWeight: isOverflow ? 600 : 500,
                          lineHeight: 1.2,
                          whiteSpace: 'nowrap',
                          maxWidth: '100%',
                        }}
                        title={isOverflow ? undefined : `${item.label} (${nodeType})`}
                      >
                        {!isOverflow && (
                          <HorizonNodeIcon type={visualType || nodeType} color={badgeAccent} size={10} />
                        )}
                        <span
                          style={{
                            maxWidth: '100%',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {item.label}
                        </span>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}
            {facts.length === 0 && count > 0 && (
              <div style={{ fontSize: 11, color: lineColor, opacity: 0.6 }}>
                {count} related items
              </div>
            )}
          </>
        )}
      </div>
      {/* Gradient fade so content smoothly disappears into the button area */}
      <div
        style={{
          position: 'absolute',
          bottom: 0,
          left: 0,
          right: 0,
          height: fadeHeight,
          background: gradientOverlay,
          pointerEvents: 'none',
          zIndex: 1,
        }}
      />
      {/* Fixed bottom action row so the contextual toggle is always visible. */}
      <div
        style={{
          position: 'absolute',
          bottom: 0,
          left: 0,
          right: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-end',
          minHeight: 28,
          paddingTop: 6,
          paddingRight: 12,
          paddingBottom: 8,
          zIndex: 2,
        }}
      >
        {typeof onShowDetails === 'function' && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              e.preventDefault();
              onShowDetails();
            }}
            onPointerDown={(e) => e.stopPropagation()}
            aria-label={contextExpanded ? 'Collapse back' : 'Show details'}
            title={contextExpanded ? 'Collapse back' : 'Show details'}
            style={{
              width: 24,
              height: 24,
              borderRadius: 9,
              border: `1px solid ${ctaBorder}`,
              background: contextExpanded ? ctaBgActive : ctaBgIdle,
              color: ctaText,
              cursor: 'pointer',
              boxShadow: ctaShadow,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              {isRightStackContextCard ? <path d="M6 9l6 6 6-6" /> : (contextExpanded ? <path d="M15 18l-6-6 6-6" /> : <path d="M9 18l6-6-6-6" />)}
            </svg>
          </button>
        )}
      </div>
      {/* Hidden handles for edge routing (context card acts as a proxy for the first child node) */}
      <Handle type="source" position={Position.Right} id="source-right" style={{ opacity: 0, width: 1, height: 1 }} />
      <Handle type="target" position={Position.Left} id="target-left" style={{ opacity: 0, width: 1, height: 1 }} />
      <Handle type="source" position={Position.Left} id="source-left" style={{ opacity: 0, width: 1, height: 1 }} />
      <Handle type="target" position={Position.Right} id="target-right" style={{ opacity: 0, width: 1, height: 1 }} />
    </div>
  );
});

// Базовый узел для Horizon фрагмента
const HorizonFragmentNode: React.FC<NodeProps<HorizonNodeData>> = React.memo(({ data, selected, id }) => {
  // Note: ensureHorizonNode2026Styles() is called once on module load (not per render)
  const { theme } = React.useContext(ThemeContext);
  const baseVisualType = getHorizonNodeVisualType(data.type, data.canonicalPath, data.label);
  const colors = getNodeColors(baseVisualType, theme);
  const [isHovering, setIsHovering] = React.useState(false);
  // Risk panel (below node): active tab + compact/full toggle.
  const [riskTab, setRiskTab] = React.useState<RiskSegmentId>('geopolitical');
  const [riskExpanded, setRiskExpanded] = React.useState(false);
  const rightFiltersRef = React.useRef<HTMLDivElement | null>(null);
  const rightFiltersBtnRef = React.useRef<HTMLButtonElement | null>(null);
  
  const isCritical = data.critical;
  const isCollapsed = data.collapsed;
  const isExpanded = data.expanded;
  const isLoadingRelated = Boolean(data.isLoadingRelated);
  const isHub = Boolean((data as any)?.hubHighlight);
  const isUltimateBeneficiary = Boolean((data as any)?.ultimateBeneficiaryHighlight);
  const isQueryHighlight = Boolean((data as any)?.queryHighlight);
  const isDomainHighlight = Boolean((data as any)?.domainHighlight);
  const isTopDependencyHighlight = Boolean((data as any)?.topDependencyHighlight);
  const isSignalPropagationHighlight = Boolean((data as any)?.signalPropagationHighlight);
  const isExpandGlow = Boolean((data as any)?.expandGlow);
  const isBottleneckHighlight = Boolean((data as any)?.bottleneckHighlight);
  // Allow Research even if the first "load related" attempt returned empty: in that case the node isn't expanded,
  // but the user should still be able to run scouting from it.
  const isSelected = Boolean(selected);
  const canExplainNode = Boolean((data as any)?.onExplainNode && (data.fragmentId || id));
  // Node expand/collapse (load related fragments).
  // NOTE:
  // - When a stack is COLLAPSED, the controller node should NOT show "expand relations" because the "N" pill
  //   already controls the stack expansion and having two expand affordances is confusing.
  // - When a stack is EXPANDED, the controller node behaves like a normal node and SHOULD show expand relations
  //   (otherwise the first visible node feels "broken" vs other nodes).
  const canCollapse0 = Boolean(data.fragmentId && data.onCollapse && isExpanded && !data.isLoadingRelated);
  const showExpand0 = Boolean(data.fragmentId && data.onLoadRelated && !isExpanded);
  const isPinnedToScope = Boolean((data as any)?.pinnedToScope);
  const canTogglePin = Boolean((data as any)?.onTogglePin && data.canonicalPath);
  const nodeId = String((data as any)?.id || id || '').trim();
  const stackKey = String((data as any)?.stackGroupKey || '').trim();
  const stackCount = typeof (data as any)?.stackCount === 'number' ? (data as any).stackCount : undefined;
  const stackExpanded = Boolean((data as any)?.stackExpanded);
  const canToggleStack = Boolean(stackKey && stackCount && stackCount > 1 && typeof (data as any)?.onToggleStack === 'function');
  const showStackExpandButton = Boolean(canToggleStack && !stackExpanded);
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
  const suppressNodeExpandOnCollapsedStackController = Boolean(isStackController && !stackExpanded && (stackCount || 0) > 1);
  const canCollapse = canCollapse0 && !suppressNodeExpandOnCollapsedStackController;
  const showExpand = showExpand0 && !suppressNodeExpandOnCollapsedStackController;
  const canExpandClick = Boolean(showExpand && !isLoadingRelated);
  const stackPreviewOpen = Boolean((data as any)?.stackPreviewOpen);
  const stackPreviewItems = Array.isArray((data as any)?.stackPreviewItems) ? ((data as any)?.stackPreviewItems as any[]) : [];
  const stackPreviewMore = typeof (data as any)?.stackPreviewMore === 'number' ? (data as any).stackPreviewMore : 0;
  const onStackPreviewSelect = (data as any)?.onStackPreviewSelect as ((nodeId: string) => void) | undefined;
  const onStackPreviewMore = (data as any)?.onStackPreviewMore as (() => void) | undefined;
  // Risk Shield button: only on "real" fragment nodes (not group nodes, not stack controllers, not stack members).
  // For now it's a pure UI affordance; wiring happens later.
  const showRiskShieldButton = Boolean(data.fragmentId && !String((data as any)?.stackGroupKey || '').trim());
  const isRiskShieldOpen = Boolean(data.riskShieldOpen);

  // Right Dock (Levels right pane): 4 filters
  const isLevelsRightPane = String((data as any)?.levelsPane || '').trim() === 'right';
  const rightDockEnabled = Array.isArray((data as any)?.rightDockEnabled) ? ((data as any)?.rightDockEnabled as any[]) : [];
  const onToggleRightDock = (data as any)?.onToggleRightDock as
    | ((nodeId: string, key: 'governance' | 'supply' | 'dependencies' | 'structure', nextEnabled: boolean) => void)
    | undefined;
  const canRightDock = Boolean(isLevelsRightPane && typeof onToggleRightDock === 'function' && nodeId);
  const dockEnabledSet = React.useMemo(() => {
    const s = new Set<string>();
    for (const k of rightDockEnabled) s.add(String(k || '').trim().toLowerCase());
    return s;
  }, [rightDockEnabled]);
  const segmentNorms = React.useMemo(() => topMarketSegmentNorms(data, 3), [(data as any)?.marketSegmentNorms, (data as any)?.marketSegments, (data as any)?.canonicalPath, (data as any)?.type]);
  const segmentIconTypes = React.useMemo(() => {
    const fromNorms = (segmentNorms || [])
      .map((n) => ({ norm: n, iconType: segmentIconType(n.normDomain, n.normVertical) }))
      .filter((x) => Boolean(x.iconType));
    if (fromNorms.length > 0) {
      // Dedup by icon type: if multiple segments map to the same icon, show only one.
      const seen = new Set<string>();
      const out: typeof fromNorms = [];
      for (const it of fromNorms) {
        const k = String((it as any)?.iconType || '').trim();
        if (!k || seen.has(k)) continue;
        seen.add(k);
        out.push(it);
      }
      return out;
    }

    // Fallback: if backend didn't send marketSegmentNorms, derive icons from raw /segment paths.
    const segs = topMarketSegments(data, 3);
    const fromRaw = segs
      .map((p) => {
        const parts = String(p || '').trim().split('/').filter(Boolean);
        // Canonical: /segment/{domain}/{vertical}/{geo}
        const domain = String(parts[1] || '').toLowerCase().trim();
        const vertical = String(parts[2] || '').toLowerCase().trim();
        const iconType = segmentIconType(domain, vertical || undefined);
        return {
          norm: {
            segmentPath: p,
            normDomain: domain,
            normVertical: vertical || undefined,
            normPair: vertical ? `${domain}/${vertical}` : domain,
          },
          iconType,
        };
      })
      .filter((x) => Boolean(x.iconType));
    // Dedup by icon type (raw fallback too).
    const seen = new Set<string>();
    const out: typeof fromRaw = [];
    for (const it of fromRaw) {
      const k = String((it as any)?.iconType || '').trim();
      if (!k || seen.has(k)) continue;
      seen.add(k);
      out.push(it);
    }
    return out;
  }, [segmentNorms]);
  const showSegmentBadges = React.useMemo(() => {
    if (!showRiskShieldButton) return false;
    if (!segmentIconTypes || segmentIconTypes.length === 0) return false;
    const t = String((data as any)?.type || '').toUpperCase().trim().replace(/\s+/g, '_');
    const cp = String((data as any)?.canonicalPath || '').trim();
    // Only for org / gov (business interests); segments are not meaningful for most other node types.
    if (t === 'ORGANIZATION') return true;
    if (cp.startsWith('/org/') || cp.startsWith('/gov/')) return true;
    return false;
  }, [showRiskShieldButton, segmentIconTypes, (data as any)?.type, (data as any)?.canonicalPath]);

  // Risk Shield: compute segments from HorizonFragment.state (prefer risk_segment_*; fallback to propagated base metrics)
  const riskState = (data as any)?.state;
  const riskSegments = React.useMemo(() => computeRiskSegments(riskState), [riskState]);
  React.useEffect(() => {
    // Closing panel resets expanded view (tab stays).
    if (!Boolean(data.riskShieldOpen) && riskExpanded) setRiskExpanded(false);
  }, [data.riskShieldOpen, riskExpanded]);
  // Collapsed stacked view: show 2–3 subtle "cards" peeking from underneath.
  const stackLayers = canToggleStack && !stackExpanded
    ? Math.min(3, Math.max(2, Math.ceil((stackCount || 0) / 8)))
    : 0;

  const isLevelsRight = String((data as any)?.levelsPane || '') === 'right';
  const isLevelsLeft = String((data as any)?.levelsPane || '') === 'left';
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
  const visualType = isCollapsedStackController && stackVisualType ? stackVisualType : baseVisualType;
  const colorsForVisual = visualType ? getNodeColors(visualType, theme) : colors;
  const isDomainVisual = String(visualType || '').toUpperCase() === 'DOMAIN';
  const isDecisionVisual = String(visualType || data.type || '').toUpperCase() === 'DECISION_POINT';
  const isMechanismVisual = String(visualType || data.type || '').toUpperCase() === 'MECHANISM';

  // 2026 visual style: neutral surface with tinted accent (less "dashboardy", more product-like)
  const accentColor = isCritical ? '#EF4444' : (colorsForVisual?.accent || colors.accent);
  const textColor = theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.88)';
  const mutedTextColor = theme === 'dark' ? 'rgba(255,255,255,0.62)' : 'rgba(15,23,42,0.56)';
  // Stack controllers: calm single-surface fill (no gradients).
  // Left pane groups: lighter background (1 tone lighter for better visibility)
  const surfaceBg = isCollapsedStackController
    ? (isLevelsLeft
        ? (theme === 'dark' ? 'rgba(27,34,49,0.94)' : 'rgba(255,255,255,0.99)')
        : (theme === 'dark' ? 'rgba(17,24,39,0.94)' : 'rgba(255,255,255,0.98)'))
    : (isLevelsLeft
        ? (theme === 'dark' ? 'rgba(27,34,49,0.90)' : 'rgba(255,255,255,0.98)')
        : (theme === 'dark' ? 'rgba(17,24,39,0.90)' : 'rgba(255,255,255,0.96)'));
  const surfaceBorder = isCollapsedStackController
    ? (theme === 'dark' ? 'rgba(148,163,184,0.28)' : 'rgba(100,116,139,0.24)') // light graphite
    : (theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)');
  const domainBorder = isDomainVisual
    ? (theme === 'dark'
        ? hexToRgba(colorsForVisual?.border || '#8A7E74', 0.6)
        : hexToRgba(colorsForVisual?.border || '#C4B7AE', 0.65))
    : surfaceBorder;
  const nodeBorder = isDomainVisual ? domainBorder : surfaceBorder;
  const stackBorderColor =
    isCollapsedStackController
      ? (isSubcategoryStackController
          ? getEdgeColor(subcategoryEdgeType || 'RELATED_TO')
          : surfaceBorder)
      : surfaceBorder;
  const accentSoft = hexToRgba(accentColor, theme === 'dark' ? 0.22 : 0.16);
  const accentRing = hexToRgba(accentColor, theme === 'dark' ? 0.55 : 0.34);
  const shadowBase = theme === 'dark' ? '0 8px 22px rgba(0,0,0,0.30)' : '0 8px 22px rgba(0,0,0,0.10)';
  const criticalRing = hexToRgba('#EF4444', theme === 'dark' ? 0.55 : 0.35);
  const q = getHorizonNodeVisualQuality();

  // ReactFlow selects nodes on pointer/mouse down. For in-node controls (expand/collapse, stack toggles)
  // we want the action without implicitly selecting the node (selection triggers edge highlighting).
  const stopNodeSelectEvent = React.useCallback((e: any) => {
    try {
      e?.preventDefault?.();
      e?.stopPropagation?.();
    } catch {
      // ignore
    }
  }, []);

  // Additional semantic highlights (rounded ring + halo):
  // Priority: critical > signal > query > top-dependency > domain > beneficiary > hub
  const signalPropagationTone = theme === 'dark'
    ? { ring: 'rgba(248, 113, 113, 0.90)', glow: 'rgba(248, 113, 113, 0.34)' }
    : { ring: 'rgba(220, 38, 38, 0.82)', glow: 'rgba(248, 113, 113, 0.24)' };
  const queryTone = theme === 'dark'
    ? { ring: 'rgba(56, 189, 248, 0.82)', glow: 'rgba(56, 189, 248, 0.22)' }
    : { ring: 'rgba(14, 165, 233, 0.70)', glow: 'rgba(14, 165, 233, 0.18)' };
  const topDependencyTone = theme === 'dark'
    ? { ring: 'rgba(125, 211, 252, 0.90)', glow: 'rgba(125, 211, 252, 0.28)' }
    : { ring: 'rgba(2, 132, 199, 0.76)', glow: 'rgba(2, 132, 199, 0.24)' };
  const domainTone = theme === 'dark'
    ? { ring: 'rgba(56, 189, 248, 0.68)', glow: 'rgba(56, 189, 248, 0.18)' }
    : { ring: 'rgba(2, 132, 199, 0.58)', glow: 'rgba(2, 132, 199, 0.14)' };
  const beneficiaryTone = theme === 'dark'
    ? { ring: 'rgba(52, 211, 153, 0.64)', glow: 'rgba(52, 211, 153, 0.18)' }
    : { ring: 'rgba(16, 185, 129, 0.54)', glow: 'rgba(16, 185, 129, 0.14)' };
  const hubTone = theme === 'dark'
    ? { ring: 'rgba(251, 191, 36, 0.66)', glow: 'rgba(251, 191, 36, 0.18)' }
    : { ring: 'rgba(245, 158, 11, 0.56)', glow: 'rgba(245, 158, 11, 0.14)' };
  const bottleneckTone = theme === 'dark'
    ? { ring: 'rgba(245, 158, 11, 0.74)', glow: 'rgba(245, 158, 11, 0.26)' }
    : { ring: 'rgba(217, 119, 6, 0.66)', glow: 'rgba(217, 119, 6, 0.20)' };
  const expandGlowTone = theme === 'dark'
    ? { ring: 'rgba(56, 189, 248, 0.78)', glow: 'rgba(56, 189, 248, 0.32)' }
    : { ring: 'rgba(14, 165, 233, 0.72)', glow: 'rgba(14, 165, 233, 0.28)' };
  const highlightTone = !isCritical
    ? (
        isExpandGlow
          ? expandGlowTone
          : isSignalPropagationHighlight
          ? signalPropagationTone
          : isQueryHighlight
          ? queryTone
          : isTopDependencyHighlight
            ? topDependencyTone
            : isDomainHighlight
              ? domainTone
              : isUltimateBeneficiary
                ? beneficiaryTone
                : isBottleneckHighlight
                  ? bottleneckTone
                  : isHub
                    ? hubTone
                    : null
      )
    : null;

  const displayLabel =
    (isCollapsedStackController && stackTitle)
      ? stackTitle
      : ((() => {
          const preferred = preferDisplayLabel(data.label, data.canonicalPath, data.type);
          return preferred || humanizeCanonicalPath(data.canonicalPath) || 'Fragment';
        })());
  const titleLineClamp = isCollapsed ? 2 : (data.showMetrics ? 2 : 6);
  const displayType = getHorizonNodeDisplayType(data.type, data.canonicalPath, data.label);
  // Keep node size consistent with ELK layout defaults used in the canvas (see LensCanvasCockpit)
  const NODE_WIDTH_PX = 180;
  const NODE_CARD_HEIGHT_PX = 148;

  // MARKET sizing summary (compact, for node bottom area).
  const isMarketNode = React.useMemo(() => {
    const t = String((data as any)?.type || '').trim().toUpperCase();
    const cp = String((data as any)?.canonicalPath || '').trim();
    return t === 'MARKET' || cp.startsWith('/market/');
  }, [data]);
  const marketSummary = React.useMemo(() => {
    if (!isMarketNode) return null;
    const st = ((data as any)?.state || {}) as Record<string, number>;
    const num = (v: any): number | null => {
      const n = typeof v === 'number' ? v : Number(v);
      return Number.isFinite(n) ? n : null;
    };
    const get = (k: string): number | null => {
      if (!st) return null;
      if (Object.prototype.hasOwnProperty.call(st, k)) return num((st as any)[k]);
      return null;
    };
    // Support both canonical and legacy keys (backend normalizes, but UI should be tolerant).
    const tam =
      get('/metric/market/tam_usd') ??
      get('tam_usd') ??
      get('tam') ??
      get('market_tam_usd') ??
      null;
    const cagr =
      get('/metric/market/cagr_percent') ??
      get('cagr_percent') ??
      get('cagr') ??
      get('market_cagr_percent') ??
      null;

    const MIN_TAM_USD = 1_000_000;
    const MIN_CAGR_PCT = 1; // hide sub-1% as noise

    const formatUSD = (v: number | null): string => {
      if (v == null) return '';
      const x = Number(v);
      if (!Number.isFinite(x)) return '';
      if (Math.abs(x) < MIN_TAM_USD) return '';
      const abs = Math.abs(x);
      const sign = x < 0 ? '-' : '';
      const fmt = (n: number, suf: string) => {
        const d = n >= 10 ? 0 : 1;
        const s = n.toFixed(d).replace(/\.0$/, '');
        return `${sign}$${s}${suf}`;
      };
      if (abs >= 1e12) return fmt(abs / 1e12, 'T');
      if (abs >= 1e9) return fmt(abs / 1e9, 'B');
      if (abs >= 1e6) return fmt(abs / 1e6, 'M');
      if (abs >= 1e3) return fmt(abs / 1e3, 'K');
      return `${sign}$${abs.toFixed(0)}`;
    };
    const toPercent = (v: number | null): number | null => {
      if (v == null) return null;
      const x = Number(v);
      if (!Number.isFinite(x)) return null;
      // Heuristic: if it's 0..1, treat as ratio and convert to percent.
      const pct = x > 0 && x < 1 ? x * 100 : x;
      return pct;
    };
    const formatPercent = (v: number | null): string => {
      const pct = toPercent(v);
      if (pct == null) return '';
      if (Math.abs(pct) < MIN_CAGR_PCT) return '';
      const d = Math.abs(pct) >= 10 ? 0 : 1;
      const s = pct.toFixed(d).replace(/\.0$/, '');
      return `${s}%`;
    };

    const tamS = formatUSD(tam);
    const cagrS = formatPercent(cagr);
    if (!tamS && !cagrS) return null;

    // Conservative UX: default to TAM only; add CAGR only when the combined label is short enough.
    const full = tamS && cagrS ? `TAM ${tamS} · CAGR ${cagrS}` : (tamS ? `TAM ${tamS}` : `CAGR ${cagrS}`);
    const compact = tamS ? `TAM ${tamS}` : `CAGR ${cagrS}`;
    const showBoth = Boolean(tamS && cagrS && full.length <= 18);

    return {
      text: showBoth ? full : compact,
      title: full, // always include the full detail in tooltip
    };
  }, [data, isMarketNode]);

  // Форматирование типа для отображения
  const formatType = (type: string) => {
    return type.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
  };

  // Compact node rendering (for left pane groups in Levels layout)
  const isCompact = data.nodeSize === 'compact';
  
  if (isCompact) {
    const compactShadowBase = theme === 'dark' ? '0 8px 22px rgba(0,0,0,0.30)' : '0 8px 22px rgba(0,0,0,0.10)';
    const compactBaseShadow = isCritical
      ? `0 0 0 2px ${criticalRing}, ${compactShadowBase}`
      : (q === 'max' ? `${compactShadowBase}, 0 18px 60px rgba(0,0,0,0.18)` : compactShadowBase);
    const compactShadow = highlightTone
      ? `0 0 0 2px ${highlightTone.ring}, 0 0 0 5px ${highlightTone.glow}, 0 0 20px ${highlightTone.glow}, ${compactBaseShadow}`
      : compactBaseShadow;

    return (
      <>
        <div
          className={`hzn-node-2026 hzn-node-compact ${isDomainVisual ? 'is-domain' : ''} ${isDecisionVisual ? 'is-decision' : ''} ${isMechanismVisual ? 'is-mechanism' : ''} ${isSelected ? 'is-selected' : ''} ${isQueryHighlight ? 'is-query-highlight' : ''} ${isDomainHighlight ? 'is-domain-highlight' : ''} ${isTopDependencyHighlight ? 'is-topdep-highlight' : ''}`}
          onMouseEnter={() => setIsHovering(true)}
          onMouseLeave={() => setIsHovering(false)}
          style={{
            // CSS variables for 2026 styles (reuse the same effects as full nodes)
            ['--hzn-surface' as any]: surfaceBg,
            ['--hzn-border' as any]: nodeBorder,
            ['--hzn-stack-border' as any]: nodeBorder,
            ['--hzn-border-width' as any]: '1px',
            ['--hzn-dash-size' as any]: '6px',
            ['--hzn-dash-gap' as any]: '6px',
            ['--hzn-accent' as any]: accentColor,
            ['--hzn-accent-soft' as any]: accentSoft,
            ['--hzn-accent-ring' as any]: isCritical ? criticalRing : accentRing,
            ['--hzn-shadow' as any]: compactShadow,

            width: `${COMPACT_NODE_WIDTH}px`,
            height: `${COMPACT_NODE_HEIGHT}px`,
            boxSizing: 'border-box',
            position: 'relative',
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            padding: '6px 10px',
            cursor: 'default',
            overflow: 'hidden',
          }}
        >
          {/* Handles (match full node IDs + hover behavior). Must be inside `.hzn-node-2026` for CSS to hide them by default. */}
          {/* Top */}
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
          {/* Bottom */}
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
          {/* Left */}
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
              top: '35%',
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
              top: '65%',
            }}
          />
          {/* Right */}
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

          {/* Type icon with accent background (smaller than full node) */}
          <div
            style={{
              width: 22,
              height: 22,
              borderRadius: 7,
              background: accentSoft,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              flexShrink: 0,
            }}
          >
            <HorizonNodeIcon type={visualType || data.type} color={accentColor} size={12} />
          </div>

          {/* Label (2 lines, clamp) */}
          <span
            style={{
              flex: 1,
              minWidth: 0,
              fontSize: 11,
              fontWeight: 650,
              color: textColor,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'normal',
              lineHeight: 1.18,
              display: '-webkit-box',
              WebkitLineClamp: 2,
              WebkitBoxOrient: 'vertical' as any,
            }}
            title={displayLabel}
          >
            {displayLabel}
          </span>

          {/* Expand/Collapse button (same size/icon/color logic as full nodes) */}
          {(showExpand || canCollapse) && (
            <>
              {showExpand && (
                <button
                  type="button"
                  onPointerDown={stopNodeSelectEvent as any}
                  onMouseDown={stopNodeSelectEvent as any}
                  onClick={(e) => {
                    stopNodeSelectEvent(e);
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
                    flexShrink: 0,
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
                  onPointerDown={stopNodeSelectEvent as any}
                  onMouseDown={stopNodeSelectEvent as any}
                  onClick={(e) => {
                    stopNodeSelectEvent(e);
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
                    flexShrink: 0,
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
            </>
          )}
        </div>
      </>
    );
  }

  return (
    <>
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
        className={`hzn-node-2026 ${isDomainVisual ? 'is-domain' : ''} ${isDecisionVisual ? 'is-decision' : ''} ${isMechanismVisual ? 'is-mechanism' : ''} ${isSelected ? 'is-selected' : ''} ${isCollapsedStackController ? 'is-stack' : ''} ${isQueryHighlight ? 'is-query-highlight' : ''} ${isDomainHighlight ? 'is-domain-highlight' : ''} ${isTopDependencyHighlight ? 'is-topdep-highlight' : ''}`}
        onMouseEnter={() => setIsHovering(true)}
        onMouseLeave={() => setIsHovering(false)}
        style={{
          // CSS variables for 2026 styles
          ['--hzn-surface' as any]: surfaceBg,
          ['--hzn-border' as any]: nodeBorder,
          ['--hzn-stack-border' as any]: stackBorderColor,
          ['--hzn-border-width' as any]: '1px',
          ['--hzn-dash-size' as any]: '6px',
          ['--hzn-dash-gap' as any]: '6px',
          ['--hzn-accent' as any]: accentColor,
          ['--hzn-accent-soft' as any]: accentSoft,
          ['--hzn-accent-ring' as any]: isCritical ? criticalRing : accentRing,
          ['--hzn-stack-accent' as any]: accentColor,
          ['--hzn-shadow' as any]: (() => {
            const baseShadow = isCritical
              ? `0 0 0 2px ${criticalRing}, ${shadowBase}`
              : (q === 'max' ? `${shadowBase}, 0 18px 60px rgba(0,0,0,0.18)` : shadowBase);
            return highlightTone
              ? `0 0 0 2px ${highlightTone.ring}, 0 0 0 5px ${highlightTone.glow}, 0 0 20px ${highlightTone.glow}, ${baseShadow}`
              : baseShadow;
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
      {/* Risk panel (below node). Shield visuals removed (per latest UX direction). */}
      {Boolean(data.riskShieldOpen) && (
        <div
          style={{
            position: 'absolute',
            left: '50%',
            top: '100%',
            transform: 'translateX(-50%)',
            marginTop: 10,
            width: 360,
            maxWidth: 420,
            pointerEvents: 'auto',
            zIndex: 6,
          }}
          onMouseDown={(e) => e.stopPropagation()}
          onClick={(e) => e.stopPropagation()}
        >
          {/* Accent separator (echo node outline color): dashed and full panel width */}
          <div
            style={{
              height: 0,
              borderTop: `2px dashed ${theme === 'dark' ? hexToRgba(accentColor, 0.55) : hexToRgba(accentColor, 0.38)}`,
              margin: '0 0 10px',
              width: '100%',
              boxShadow: theme === 'dark' ? `0 0 18px ${hexToRgba(accentColor, 0.20)}` : 'none',
            }}
          />

          <div
            style={{
              borderRadius: 16,
              border: 'none',
              background: theme === 'dark' ? 'rgba(10,10,12,0.70)' : 'rgba(255,255,255,0.72)',
              backdropFilter: 'blur(12px)',
              boxShadow: theme === 'dark' ? '0 22px 70px rgba(0,0,0,0.55)' : '0 22px 70px rgba(0,0,0,0.16)',
              padding: 12,
              position: 'relative',
            }}
          >
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                if (!data.fragmentId) return;
                (data as any)?.onToggleRiskShield?.(data.fragmentId);
              }}
              aria-label="Close risk panel"
              title="Close"
              style={{
                position: 'absolute',
                top: 8,
                right: 8,
                width: 28,
                height: 28,
                borderRadius: 10,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}`,
                background: 'transparent',
                color: 'var(--text-heading)',
                cursor: 'pointer',
                opacity: 0.75,
                fontSize: 16,
                fontWeight: 900,
                lineHeight: 1,
              }}
            >
              ×
            </button>

            {/* Tabs: 8 risk icons (order = polygon, top clockwise) */}
            <div style={{ display: 'flex', flexWrap: 'nowrap', gap: 8, marginBottom: 10 }}>
              {riskSegments.map((seg) => {
                const isActive = seg.id === riskTab;
                const op = 0.05 + 0.95 * clamp01(seg.score);
                const title = `${seg.label}: ${formatRiskLevel(seg.level)} (${seg.score.toFixed(2)}) — ${seg.description}`;
                return (
                  <button
                    key={`risk-tab-${seg.id}`}
                    type="button"
                    title={title}
                    onClick={() => setRiskTab(seg.id)}
                    aria-label={`Risk tab: ${seg.label}`}
                    aria-pressed={isActive}
                    style={{
                      // Match node expand/collapse control size for visual rhyme.
                      width: 24,
                      height: 24,
                      borderRadius: 9,
                      border: isActive
                        ? `1px solid ${hexToRgba(seg.color, theme === 'dark' ? 0.55 : 0.40)}`
                        : `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)'}`,
                      background: isActive
                        ? (theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.05)')
                        : 'transparent',
                      cursor: 'pointer',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      boxShadow: isActive
                        ? (theme === 'dark' ? `0 0 0 2px ${hexToRgba(seg.color, 0.16)}` : `0 0 0 2px ${hexToRgba(seg.color, 0.10)}`)
                        : 'none',
                    }}
                  >
                    <RiskSegmentIcon id={seg.id} size={14} color={seg.color} opacity={isActive ? 1 : op} />
                  </button>
                );
              })}
            </div>

            {(() => {
              const current = riskSegments.find((s) => s.id === riskTab) || riskSegments[0];
              const score = clamp01(current?.score ?? 0);
              const pct = Math.round(score * 100);
              const source = computeRiskSegmentSource((data as any)?.state, current.id);

              if (!riskExpanded) {
                // Compact view: bar + number
                return (
                  <button
                    type="button"
                    onClick={() => setRiskExpanded(true)}
                    style={{
                      width: '100%',
                      border: 'none',
                      background: 'transparent',
                      padding: 0,
                      cursor: 'pointer',
                      textAlign: 'left',
                    }}
                    title="Show details"
                  >
                    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
                      <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>
                        {current.label}
                      </div>
                      <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                        {pct}%
                      </div>
                    </div>
                    <div
                      style={{
                        marginTop: 8,
                        height: 8,
                        borderRadius: 999,
                        background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)',
                        overflow: 'hidden',
                      }}
                    >
                      <div
                        style={{
                          width: `${pct}%`,
                          height: '100%',
                          background: current.color,
                          opacity: 0.9,
                        }}
                      />
                    </div>
                    <div style={{ marginTop: 8, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.4 }}>
                      {current.description}
                    </div>
                  </button>
                );
              }

              // Expanded view: deterministic “profile” (no LLM)
              return (
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                      <RiskSegmentIcon id={current.id} size={18} color={current.color} opacity={1} />
                      <div style={{ minWidth: 0 }}>
                        <div style={{ fontSize: 13, fontWeight: 950, color: 'var(--text-heading)', lineHeight: 1.2 }}>
                          {current.label}
                        </div>
                        <div style={{ marginTop: 3, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                          {current.description}
                        </div>
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={() => setRiskExpanded(false)}
                      title="Collapse"
                      style={{
                        width: 28,
                        height: 28,
                        borderRadius: 10,
                        border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}`,
                        background: 'transparent',
                        color: 'var(--text-heading)',
                        cursor: 'pointer',
                        opacity: 0.75,
                        fontSize: 16,
                        fontWeight: 900,
                        lineHeight: 1,
                        flexShrink: 0,
                      }}
                    >
                      ×
                    </button>
                  </div>

                  <div style={{ marginTop: 10, display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                    <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                      Level: {formatRiskLevel(current.level)}
                    </div>
                    <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                      Score: {score.toFixed(2)} ({pct}%)
                    </div>
                  </div>

                  <div
                    style={{
                      marginTop: 8,
                      height: 8,
                      borderRadius: 999,
                      background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)',
                      overflow: 'hidden',
                    }}
                  >
                    <div
                      style={{
                        width: `${pct}%`,
                        height: '100%',
                        background: current.color,
                        opacity: 0.9,
                      }}
                    />
                  </div>

                  <div style={{ marginTop: 10, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.5 }}>
                    {source.kind === 'none' ? (
                      <div>Source metric: — (no direct metric available)</div>
                    ) : (
                      <div>
                        Source metric: <span style={{ fontWeight: 900, color: 'var(--text-heading)' }}>{source.key}</span>{' '}
                        <span style={{ opacity: 0.8 }}>
                          ({source.kind === 'segment_metric' ? 'segment' : 'fallback'}; value={source.value.toFixed(2)})
                        </span>
                      </div>
                    )}
                  </div>
                </div>
              );
            })()}
          </div>
        </div>
      )}

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
      
      {/* Left handles: 2 handles для левой группы (market) — центральные 50% */}
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
          top: '35%',
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
          top: '65%',
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
          {/* Stack controller: compact aggregate layout (quiet, structural). */}
          <div
            style={{
              position: 'absolute',
              inset: '12px 14px 34px 14px',
              display: 'flex',
              flexDirection: 'row',
              alignItems: 'flex-start',
              justifyContent: 'flex-start',
              textAlign: 'left',
              gap: 10,
              pointerEvents: 'none',
            }}
          >
            <div
              style={{
                width: 30,
                height: 30,
                borderRadius: 9,
                background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'rgba(15,23,42,0.045)',
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.08)'}`,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                boxShadow: 'none',
                flexShrink: 0,
              }}
            >
              {isSubcategoryStackController ? (
                <div style={{ transform: 'translateY(-1px)' }}>
                  <EdgeTypeIcon
                    edgeType={subcategoryEdgeType || 'RELATED_TO'}
                    color={getEdgeColor(subcategoryEdgeType || 'RELATED_TO')}
                    size={13}
                  />
                </div>
              ) : (
                <HorizonNodeIcon
                  type={visualType || data.type}
                  color={theme === 'dark' ? 'rgba(255,255,255,0.76)' : 'rgba(15,23,42,0.72)'}
                  size={14}
                />
              )}
            </div>
            {!isSubcategoryStackController && extractCountryCode(data.canonicalPath) && (
              <div
                style={{
                  width: 30,
                  height: 30,
                  borderRadius: 9,
                  background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'rgba(15,23,42,0.045)',
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.08)'}`,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  boxShadow: 'none',
                  flexShrink: 0,
                }}
              >
                <CountryFlag countryCode={extractCountryCode(data.canonicalPath)} size={18} />
              </div>
            )}

            <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0, width: '100%', paddingTop: 2 }}>
              <div
                style={{
                  fontSize: 13,
                  fontWeight: 700,
                  color: textColor,
                  letterSpacing: '0.01em',
                  lineHeight: 1.18,
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
                    fontSize: 11,
                    fontWeight: 500,
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
            {/* Count badge in the top-right slot (quiet replacement for decorative stack icon). */}
            {isCollapsedStackController && !canToggleStack && typeof stackCount === 'number' && (
              <div
                style={{
                  minWidth: 24,
                  height: 22,
                  padding: '0 8px',
                  borderRadius: 11,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.16)' : 'rgba(15,23,42,0.12)'}`,
                  background: theme === 'dark' ? 'rgba(2, 6, 23, 0.34)' : 'rgba(255,255,255,0.76)',
                  color: theme === 'dark' ? 'rgba(255,255,255,0.78)' : 'rgba(15,23,42,0.66)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  boxShadow: 'none',
                  pointerEvents: 'none',
                  fontSize: 11,
                  fontWeight: 700,
                }}
                title={`${stackCount} nodes in stack`}
                aria-hidden="true"
              >
                {stackCount}
              </div>
            )}

            {/* Pin + Info stacked (mirror the left icon + flag column) */}
            {(canTogglePin || canExplainNode) && !isCollapsedStackController && !isSubcategoryStackController && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {canTogglePin && (
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
                {canExplainNode && (
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      const cb = (data as any)?.onExplainNode as
                        | ((args: { nodeId: string; fragmentId?: string; canonicalPath?: string; label?: string; type?: string }) => void)
                        | undefined;
                      cb?.({
                        nodeId: String(id || ''),
                        fragmentId: data.fragmentId,
                        canonicalPath: data.canonicalPath,
                        label: data.label,
                        type: data.type,
                      });
                    }}
                    aria-label="Explain entity"
                    title="Explain entity"
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
                      pointerEvents: 'auto',
                    }}
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
                      <circle cx="12" cy="12" r="10" />
                      <line x1="12" y1="16" x2="12" y2="12" />
                      <line x1="12" y1="8" x2="12.01" y2="8" />
                    </svg>
                  </button>
                )}
              </div>
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
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4, flexShrink: 0 }}>
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
            {extractCountryCode(data.canonicalPath) && (
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
                <CountryFlag countryCode={extractCountryCode(data.canonicalPath)} size={14} />
              </div>
            )}
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
            {/* Pin + Info stacked (mirror the left icon + flag column) */}
            {(canTogglePin || canExplainNode) && !isSubcategoryStackController && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {canTogglePin && (
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
                {canExplainNode && (
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      const cb = (data as any)?.onExplainNode as
                        | ((args: { nodeId: string; fragmentId?: string; canonicalPath?: string; label?: string; type?: string }) => void)
                        | undefined;
                      cb?.({
                        nodeId: String(id || ''),
                        fragmentId: data.fragmentId,
                        canonicalPath: data.canonicalPath,
                        label: data.label,
                        type: data.type,
                      });
                    }}
                    aria-label="Explain entity"
                    title="Explain entity"
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
                      <circle cx="12" cy="12" r="10" />
                      <line x1="12" y1="16" x2="12" y2="12" />
                      <line x1="12" y1="8" x2="12.01" y2="8" />
                    </svg>
                  </button>
                )}
              </div>
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
      {(showExpand || canCollapse || showStackExpandButton) && (
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
          {showStackExpandButton && (
            <button
              type="button"
              onPointerDown={stopNodeSelectEvent as any}
              onMouseDown={stopNodeSelectEvent as any}
              onClick={(e) => {
                stopNodeSelectEvent(e);
                const cb = (data as any)?.onToggleStack as ((k: string, next: boolean) => void) | undefined;
                cb?.(stackKey, true);
              }}
              aria-label={`Expand stack: ${stackTitle || displayLabel}`}
              title={`Expand stack (${stackCount || 0})`}
              style={{
                width: 24,
                height: 24,
                borderRadius: 9,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.78)',
                color: theme === 'dark' ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.78)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                boxShadow: theme === 'dark' ? '0 8px 18px rgba(0,0,0,0.42)' : '0 8px 18px rgba(0,0,0,0.12)',
              }}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <circle cx="6" cy="12" r="1.6" />
                <circle cx="12" cy="12" r="1.6" />
                <circle cx="18" cy="12" r="1.6" />
              </svg>
            </button>
          )}
          {showExpand && (
            <button
              type="button"
              onPointerDown={stopNodeSelectEvent as any}
              onMouseDown={stopNodeSelectEvent as any}
              onClick={(e) => {
                stopNodeSelectEvent(e);
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
              onPointerDown={stopNodeSelectEvent as any}
              onMouseDown={stopNodeSelectEvent as any}
              onClick={(e) => {
                stopNodeSelectEvent(e);
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

      {/* Risk Shield affordance (bottom-left, symmetric to expand button). */}
      {showRiskShieldButton && (
        <div
          style={{
            position: 'absolute',
            left: 12,
            bottom: 12,
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            zIndex: 2,
          }}
        >
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              if (data.fragmentId) data.onToggleRiskShield?.(data.fragmentId);
            }}
            aria-label="Risk shield"
            title={isRiskShieldOpen ? 'Risk shield (on)' : 'Risk shield'}
            style={{
              width: 24,
              height: 24,
              borderRadius: 9,
              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
              // Match expand/collapse affordance: when active, use the same amber fill as "collapse" state.
              background: isRiskShieldOpen
                ? (theme === 'dark' ? 'rgba(245, 158, 11, 0.28)' : 'rgba(245, 158, 11, 0.18)')
                : (theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.78)'),
              color: theme === 'dark' ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.78)',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              boxShadow: theme === 'dark' ? '0 8px 18px rgba(0,0,0,0.42)' : '0 8px 18px rgba(0,0,0,0.12)',
            }}
          >
            {/* Exclamation icon (attention / risk mode) */}
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
              <path d="M12 7v7" />
              <path d="M12 18h.01" />
            </svg>
          </button>
        </div>
      )}

      {/* MARKET: compact sizing summary between the two bottom buttons (non-interactive). */}
      {marketSummary && (
        (() => {
          // Reserve space so we don't overlap the bottom-left and bottom-right controls.
          const leftReserve = showRiskShieldButton ? 12 + 24 + 8 : 12;
          const rightButtons = (showExpand ? 1 : 0) + (canCollapse ? 1 : 0) + (showStackExpandButton ? 1 : 0);
          const rightReserve = (() => {
            // If the bottom-right controls are not rendered, keep a minimal padding.
            if (!(showExpand || canCollapse || showStackExpandButton)) return 12;
            const buttonsW = rightButtons > 0 ? (rightButtons * 24 + Math.max(0, rightButtons - 1) * 6) : 0;
            // +12 right margin + small safety gap.
            return 12 + buttonsW + 8;
          })();

          return (
            <div
              title={marketSummary.title}
              style={{
                position: 'absolute',
                left: leftReserve,
                right: rightReserve,
                bottom: 12,
                height: 24,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                pointerEvents: 'none', // avoid intercepting clicks on bottom buttons
                zIndex: 1,
              }}
            >
              <div
                style={{
                  fontSize: 11,
                  fontWeight: 800,
                  letterSpacing: '0.01em',
                  color: mutedTextColor,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  maxWidth: '100%',
                }}
              >
                {marketSummary.text}
              </div>
            </div>
          );
        })()
      )}

      {/* Top market segments (max 3): centered between left (!) and right expand/collapse controls. */}
      {showSegmentBadges && (
        <div
          style={{
            position: 'absolute',
            left: '50%',
            bottom: 12,
            transform: 'translateX(-50%)',
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            zIndex: 2,
            pointerEvents: 'none',
            // Keep a little breathing room so it doesn't visually touch the left/right controls.
            paddingLeft: 6,
            paddingRight: 6,
          }}
        >
          {segmentIconTypes.slice(0, 3).map(({ norm: n, iconType }) => {
            if (!iconType) return null;
            const p = String(n?.segmentPath || '').trim() || String(n?.normPair || '').trim();
            return (
              <div
                key={`seg-${String(data.id || '')}-${String(n?.normPair || '')}`}
                title={p}
                style={{
                  width: 24,
                  height: 24,
                  borderRadius: 9,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.78)',
                  color: theme === 'dark' ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.78)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  boxShadow: theme === 'dark' ? '0 8px 18px rgba(0,0,0,0.42)' : '0 8px 18px rgba(0,0,0,0.12)',
                  opacity: 0.92,
                }}
              >
                <HorizonNodeIcon type={iconType} color={accentColor} size={14} />
              </div>
            );
          })}
        </div>
      )}

      {/* Stack toggle: keep only expanded-state collapse control here.
          Collapsed-state expand now uses the standard bottom-right action button ("..."). */}
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
            display: stackExpanded ? 'inline-flex' : 'none',
            position: 'absolute',
            ...(stackExpanded ? {
              left: '100%',
              top: '50%',
              transform: 'translateY(-50%)',
              marginLeft: 8,
            } : {
              right: 10,
              top: 10,
              transform: 'none',
            }),
            height: stackExpanded ? 26 : 22,
            minWidth: stackExpanded ? undefined : 24,
            padding: stackExpanded ? '0 10px' : '0 8px',
            borderRadius: 999,
            border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.16)' : 'rgba(15,23,42,0.12)'}`,
            background: theme === 'dark' ? 'rgba(2, 6, 23, 0.34)' : 'rgba(255,255,255,0.76)',
            color: theme === 'dark' ? 'rgba(255,255,255,0.82)' : 'rgba(15,23,42,0.70)',
            cursor: 'pointer',
            alignItems: 'center',
            gap: stackExpanded ? 8 : 0,
            boxShadow: 'none',
            fontSize: stackExpanded ? 12 : 11,
            fontWeight: stackExpanded ? 800 : 700,
            lineHeight: 1,
            pointerEvents: 'auto',
            zIndex: 3,
          }}
        >
          {stackExpanded ? (
            <>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M18 15l-6-6-6 6" />
              </svg>
              <span>Collapse</span>
            </>
          ) : (
            <span>{stackCount || 0}</span>
          )}
        </button>
      )}

      {/* Right Dock: 4 filter icons (Levels right pane). Place BELOW node to avoid edge overlap. */}
      {canRightDock && (() => {
        const dockFocus = Boolean(isHovering || isSelected || rightFiltersOpen);
        // Quiet mode: show only for expanded nodes, and only the enabled modes (prevents visual noise).
        const quiet = !dockFocus;
        if (quiet && !isExpanded) return null;

        const itemsAll = ([
          { key: 'governance', label: 'Governance', icon: 'gavel' },
          { key: 'supply', label: 'Supply chain', icon: 'supply' },
          { key: 'dependencies', label: 'Dependencies', icon: 'link' },
          { key: 'structure', label: 'Structure', icon: 'layers' },
        ] as const);

        const items = quiet ? itemsAll.filter((it) => dockEnabledSet.has(it.key)) : itemsAll;
        if (!items || items.length === 0) return null;

        // Scout integration: show when node is SELECTED and canScopeScout is true.
        // canScopeScout covers: expanded OR loadRelatedAttempted (tried to expand but nothing to show).
        // This ensures Scout is available when user needs to fetch more data.
        const showScoutDock = Boolean(
          isSelected &&
          canScopeScout &&
          data.fragmentId &&
          typeof data.onScopeScout === 'function',
        );
        const scoutW = 18; // icon-only (no circular button)
        const dividerW = 1;
        const scoutGap = 8;
        const extraW = showScoutDock ? (dividerW + scoutGap + scoutW) : 0; // [|][gap][scout]

        return (
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              position: 'absolute',
              left: '50%',
              top: '100%',
              marginTop: 3,
              transform: 'translateX(-50%)',
              display: 'flex',
              flexDirection: 'row',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 0,
              zIndex: 4,
              opacity: dockFocus ? 1 : 1,
              pointerEvents: dockFocus ? 'auto' : 'none',
              padding: '4px 10px',
              borderRadius: 999,
              // No border in any state (user request).
              border: 'none',
              background: dockFocus ? (theme === 'dark' ? 'rgba(0,0,0,0.32)' : 'rgba(255,255,255,0.70)') : 'transparent',
              backdropFilter: dockFocus ? 'blur(8px)' : 'none',
              boxShadow: dockFocus ? (theme === 'dark' ? '0 14px 40px rgba(0,0,0,0.32)' : '0 14px 40px rgba(0,0,0,0.14)') : 'none',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              {items.map((it) => {
                const active = dockEnabledSet.has(it.key);
                // Toggle is allowed in focus mode (hover/selection). Layout changes, if any, are handled upstream.
                const canToggle = Boolean(dockFocus);
                // Full-color only when focused (hover/selected). Quiet mode shows only active icons anyway.
                const fg = dockFocus
                  ? (active ? accentColor : (theme === 'dark' ? 'rgba(255,255,255,0.70)' : 'rgba(15,23,42,0.62)'))
                  : (theme === 'dark' ? 'rgba(255,255,255,0.78)' : 'rgba(15,23,42,0.72)');
                // No per-icon borders/backgrounds (avoids "squares"). Active state is conveyed by color/opacity only.
                const btnBorder = 'none';
                const btnBg = 'transparent';

                return (
                  <button
                    key={`${nodeId}-dock-${it.key}`}
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      if (!canToggle) return;
                      onToggleRightDock?.(nodeId, it.key, !active);
                    }}
                    title={it.label}
                    aria-label={it.label}
                    style={{
                      width: 24,
                      height: 24,
                      borderRadius: 8,
                      border: btnBorder,
                      background: btnBg,
                      color: fg,
                      cursor: canToggle ? 'pointer' : 'default',
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      boxShadow: 'none',
                      padding: 0,
                      opacity: dockFocus ? (active ? 1 : 0.66) : 0.92,
                      transition: 'opacity 120ms ease, transform 120ms ease',
                      transform: dockFocus && active ? 'translateY(-0.5px)' : 'none',
                    }}
                  >
                    {it.icon === 'gavel' && (
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                        {/* Courthouse / governance (clear at 14px) */}
                        <path d="M3 21h18" />
                        <path d="M4 10h16" />
                        <path d="M6 10V20" />
                        <path d="M10 10V20" />
                        <path d="M14 10V20" />
                        <path d="M18 10V20" />
                        <path d="M12 3 3 8h18L12 3Z" />
                      </svg>
                    )}
                    {it.icon === 'supply' && (
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M3 7h11v10H3z" />
                        <path d="M14 10h4l3 3v4h-7z" />
                        <path d="M7 17a2 2 0 1 0 0 .01" />
                        <path d="M17 17a2 2 0 1 0 0 .01" />
                      </svg>
                    )}
                    {it.icon === 'link' && (
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M10 13a5 5 0 0 1 0-7l1.2-1.2a5 5 0 0 1 7 7L17 13" />
                        <path d="M14 11a5 5 0 0 1 0 7L12.8 19.2a5 5 0 0 1-7-7L7 11" />
                      </svg>
                    )}
                    {it.icon === 'layers' && (
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M12 2 2 7l10 5 10-5-10-5Z" />
                        <path d="M2 12l10 5 10-5" />
                        <path d="M2 17l10 5 10-5" />
                      </svg>
                    )}
                  </button>
                );
              })}
            </div>

            {/* Divider + Scout: only on selected expanded nodes; animate in to the RIGHT of icons */}
            <div
              style={{
                width: extraW,
                opacity: showScoutDock ? 1 : 0,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'flex-end',
                overflow: 'hidden',
                transition: 'width 160ms ease, opacity 140ms ease',
                pointerEvents: showScoutDock ? 'auto' : 'none',
                marginLeft: showScoutDock ? 10 : 0,
              }}
            >
              <div
                aria-hidden="true"
                style={{
                  width: dividerW,
                  height: 16,
                  borderRadius: 999,
                  background: theme === 'dark' ? 'rgba(255,255,255,0.18)' : 'rgba(15,23,42,0.16)',
                  flexShrink: 0,
                }}
              />
              <div style={{ width: scoutGap }} />
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  if (!data.fragmentId) return;
                  if (data.isScopeScouting) return;
                  data.onScopeScout?.(data.fragmentId, data.canonicalPath);
                }}
                disabled={data.isScopeScouting}
                aria-label="Scout it"
                title={data.isScopeScouting ? 'Scouting…' : 'Scout it'}
                style={{
                  width: scoutW,
                  height: 18,
                  borderRadius: 6,
                  border: 'none',
                  background: 'transparent',
                  color: theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.90)',
                  cursor: data.isScopeScouting ? 'wait' : 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  flexShrink: 0,
                  padding: 0,
                  opacity: data.isScopeScouting ? 0.7 : 1,
                }}
              >
                <svg
                  width="18"
                  height="18"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke={accentColor}
                  strokeWidth="2.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <circle cx="11" cy="11" r="7" />
                  <path d="M21 21l-4.3-4.3" />
                </svg>
              </button>
            </div>
          </div>
        );
      })()}

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
  horizonContextCard: HorizonContextCardNode,
});

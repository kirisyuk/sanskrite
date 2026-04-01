import React from 'react';
import { HorizonNodeIcon, formatHorizonFragmentType, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';
import type { GeoObject } from '../hooks/useGeoObjects';
import type { GeoEdgeTraceSummary } from '../hooks/useGeoEdgeLayer';
import type { SupplyChainObjectRiskOverlay, SupplyChainOverlayMechanism, SupplyChainOverlayMetric } from '../lib/api';
import { geoCodeToShortName, humanizeCanonicalPath } from '../lib/uiUtils';
import { mapFamilyAccentColor, mapLayerColor } from '../lib/mapRenderPolicy';

export interface MapObjectDetailProps {
  object: GeoObject;
  isDark: boolean;
  paletteTheme: 'dark' | 'light';
  topOffset?: number;
  layout?: 'overlay' | 'panel';
  edgesLoading: boolean;
  edgesCount: number;
  visibleEdgesCount: number;
  traceSummary: GeoEdgeTraceSummary | null;
  activeNarrativeKey: string | null;
  onNarrativeHover: (key: string | null) => void;
  activeSupplyRouteIds?: string[];
  onSupplyRouteHover?: (artifactIds: string[]) => void;
  directedExploreSupported?: boolean;
  directedExploreBusy?: 'upstream' | 'downstream' | null;
  directedExploreError?: string | null;
  directedExploreDisabledReason?: string;
  onDirectedExplore?: (direction: 'upstream' | 'downstream') => void;
  onClose: () => void;
}

export type MapObjectInspectorPayload = Omit<MapObjectDetailProps, 'isDark' | 'paletteTheme' | 'topOffset' | 'layout'>;

type SectionKey = 'routes' | 'context' | 'evidence';
type DetailLayoutMode = 'overlay' | 'panel';

function formatFacilityKind(value?: string): string {
  return String(value || '')
    .trim()
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function extractCountryLabel(canonicalPath?: string): string {
  const path = String(canonicalPath || '').trim().toLowerCase();
  if (!path) return '';
  const parts = path.split('/').filter(Boolean);
  if (parts.length < 2) return '';
  const namespace = parts[0];
  if (!['org', 'gov', 'market', 'loc', 'fac', 'deposit', 'community', 'zone'].includes(namespace)) return '';
  const codes = String(parts[1] || '')
    .split('-')
    .map((value) => value.trim().toLowerCase())
    .filter((value) => value.length === 2 && /^[a-z]{2}$/.test(value) && value !== 'na');
  if (codes.length === 0) return '';
  return codes.map((code) => geoCodeToShortName(code)).join(' / ');
}

function getObjectDisplayName(object: GeoObject): string {
  return String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath;
}

function humanizeToken(value?: string): string {
  return String(value || '')
    .trim()
    .toLowerCase()
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function familyAccent(family: string, isDark: boolean): string {
  return mapFamilyAccentColor(family, isDark ? 'dark' : 'light');
}

function translucentAccent(color: string, opacity: number): string {
  const normalized = String(color || '').trim();
  const hex = normalized.startsWith('#') ? normalized.slice(1) : normalized;
  if (!/^[0-9a-f]{6}$/i.test(hex)) return color;
  const r = parseInt(hex.slice(0, 2), 16);
  const g = parseInt(hex.slice(2, 4), 16);
  const b = parseInt(hex.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${opacity})`;
}

function sectionCardStyle(isDark: boolean, layout: DetailLayoutMode = 'overlay'): React.CSSProperties {
  if (layout === 'panel') {
    return {
      padding: '14px',
      borderRadius: 16,
      background: isDark ? 'rgba(255, 255, 255, 0.026)' : 'rgba(15, 23, 42, 0.028)',
      border: '1px solid var(--border-subtle)',
      boxShadow: isDark ? 'inset 0 1px 0 rgba(255,255,255,0.03)' : 'inset 0 1px 0 rgba(255,255,255,0.7)',
    };
  }
  return {
    padding: '12px 13px',
    borderRadius: 14,
    background: isDark ? 'rgba(148, 163, 184, 0.08)' : 'rgba(15, 23, 42, 0.035)',
    border: isDark ? '1px solid rgba(148, 163, 184, 0.14)' : '1px solid rgba(15, 23, 42, 0.06)',
  };
}

function formatPercentScore(value?: number): string {
  if (!Number.isFinite(Number(value))) return '';
  return `${Math.round(Math.max(0, Math.min(1, Number(value))) * 100)}`;
}

function compactOverlayLabel(value?: string): string {
  const normalized = String(value || '').trim();
  if (!normalized || ['<nil>', '<Nil>', 'nil', 'null', 'undefined'].includes(normalized)) return '';
  return normalized
    .replace(/[_-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function normalizeBusinessCopy(value?: string): string {
  const normalized = String(value || '').trim();
  if (!normalized) return '';
  return normalized
    .replace(/\bcriticality\b/gi, 'business importance')
    .replace(/\broute fragility\b/gi, 'route resilience risk')
    .replace(/\bmapped direct relations\b/gi, 'direct business links')
    .replace(/\bcontext anchors\b/gi, 'business context')
    .replace(/\bpropagation\b/gi, 'secondary spread')
    .replace(/\bselected object\b/gi, 'this asset')
    .replace(/\bobject\b/gi, 'asset')
    .replace(/\s+/g, ' ')
    .trim();
}

function pluralize(count: number, singular: string, plural?: string): string {
  return `${count} ${count === 1 ? singular : (plural || `${singular}s`)}`;
}

function scoreLevel(score?: number): {
  label: string;
  note: string;
  accent: string;
} {
  if (typeof score !== 'number' || !Number.isFinite(score)) {
    return { label: 'Unknown', note: 'No scored business importance yet', accent: 'var(--text-secondary-strong)' };
  }
  if (score >= 0.82) return { label: 'Very high', note: 'Major business dependency', accent: '#f97316' };
  if (score >= 0.62) return { label: 'High', note: 'Meaningful business exposure', accent: '#f59e0b' };
  if (score >= 0.42) return { label: 'Elevated', note: 'Watch for disruption', accent: '#38bdf8' };
  return { label: 'Limited', note: 'Lower immediate exposure', accent: '#94a3b8' };
}

function directionLabel(direction: 'inbound' | 'outbound' | 'context'): string {
  if (direction === 'inbound') return 'Input to this asset';
  if (direction === 'outbound') return 'Output from this asset';
  return 'Related route';
}

function metricValueText(metric?: SupplyChainOverlayMetric | null): string {
  if (!metric) return '';
  if (metric.formatted) return metric.formatted;
  if (typeof metric.value === 'number' && Number.isFinite(metric.value)) {
    const rounded = Math.abs(metric.value) >= 100 ? Math.round(metric.value) : Number(metric.value.toFixed(2));
    return metric.unit ? `${rounded} ${metric.unit}` : String(rounded);
  }
  return '';
}

function buildBusinessBrief(
  objectName: string,
  overlay: SupplyChainObjectRiskOverlay | null,
  traceSummary: GeoEdgeTraceSummary | null,
): string {
  const overlaySummary = normalizeBusinessCopy(overlay?.summary);
  if (overlaySummary) return overlaySummary;
  const firstReason = normalizeBusinessCopy(traceSummary?.whyItMatters?.[0]?.body);
  if (firstReason) return firstReason;
  const routeCount = traceSummary?.supplyPathCount || 0;
  const stressedCount = traceSummary?.stressedSupplyPathCount || 0;
  if (routeCount > 0 && stressedCount > 0) {
    return `${objectName} is exposed through ${pluralize(routeCount, 'mapped route')}, with ${pluralize(stressedCount, 'route')} already under pressure.`;
  }
  if (routeCount > 0) {
    return `${objectName} sits on ${pluralize(routeCount, 'mapped route')} in the current network view.`;
  }
  const relationCount = (traceSummary?.directCount || 0) + (traceSummary?.contextCount || 0);
  if (relationCount > 0) {
    return `${objectName} is linked to ${pluralize(relationCount, 'business node')} in the surrounding network.`;
  }
  return `${objectName} is visible on the map, but the current view does not yet contain enough linked business evidence for a stronger brief.`;
}

function buildActionItems(
  objectName: string,
  traceSummary: GeoEdgeTraceSummary | null,
  overlay: SupplyChainObjectRiskOverlay | null,
): string[] {
  const actions: string[] = [];
  const topGroup = traceSummary?.supplyGroups?.[0];
  const topProduct = overlay?.topProducts?.[0]?.name || topGroup?.productLabel || '';
  const bottleneckLabel = overlay?.bottleneckNodes?.[0]?.name
    || humanizeCanonicalPath(String(overlay?.bottleneckNodes?.[0]?.canonicalPath || ''))
    || '';
  const stressedCount = traceSummary?.stressedSupplyPathCount || 0;
  const directCount = traceSummary?.directCount || 0;
  const contextCount = traceSummary?.contextCount || 0;

  if (stressedCount > 0) {
    actions.push(
      bottleneckLabel
        ? `Validate a fallback route or operating workaround around ${bottleneckLabel}.`
        : `Review alternatives for the ${pluralize(stressedCount, 'route')} currently under pressure.`,
    );
  }
  if (topProduct) {
    actions.push(`Check near-term supply continuity for ${topProduct} before pressure spreads downstream.`);
  }
  if (directCount + contextCount > 0) {
    actions.push(`Review the ${pluralize(directCount + contextCount, 'connected node')} most likely to amplify disruption around ${objectName}.`);
  }
  if (actions.length === 0 && traceSummary?.supplyPathCount) {
    actions.push(`Review the mapped route set for ${objectName} and confirm whether backup paths exist.`);
  }
  if (actions.length === 0) {
    actions.push(`Gather one more layer of commercial or operating context before treating ${objectName} as a material business risk.`);
  }
  return actions.slice(0, 3);
}

function buildDriverItems(
  mechanisms: SupplyChainOverlayMechanism[],
  metrics: SupplyChainOverlayMetric[],
  bottleneckLabel: string,
): Array<{ title: string; body: string }> {
  const items: Array<{ title: string; body: string }> = [];
  for (const mechanism of mechanisms) {
    const title = compactOverlayLabel(mechanism.label || mechanism.key);
    if (!title) continue;
    items.push({
      title,
      body: normalizeBusinessCopy(mechanism.explanation) || `${title} is one of the strongest drivers behind the current risk signal.`,
    });
    if (items.length >= 2) return items;
  }
  for (const metric of metrics) {
    const title = compactOverlayLabel(metric.label || metric.key);
    if (!title) continue;
    const valueText = metricValueText(metric);
    items.push({
      title,
      body: valueText
        ? `${title} is currently tracked at ${valueText} in the latest overlay.`
        : `${title} is one of the signals contributing to the current view.`,
    });
    if (items.length >= 2) return items;
  }
  if (bottleneckLabel) {
    items.push({
      title: 'Key bottleneck',
      body: `${bottleneckLabel} is the most visible concentration point in the current network trace.`,
    });
  }
  return items.slice(0, 2);
}

function consoleAccent(isDark: boolean): string {
  return isDark ? '#7da6a6' : '#5f8b8b';
}

function narrativeBlockStyle(
  isDark: boolean,
  isActive: boolean,
  accent: string,
  layout: DetailLayoutMode = 'overlay',
): React.CSSProperties {
  if (layout === 'panel') {
    return {
      padding: '11px 12px',
      borderRadius: 14,
      background: isActive
        ? (isDark ? 'rgba(125, 166, 166, 0.12)' : 'rgba(95, 139, 139, 0.08)')
        : (isDark ? 'rgba(255, 255, 255, 0.024)' : 'rgba(15, 23, 42, 0.024)'),
      border: isActive
        ? `1px solid ${translucentAccent(accent, isDark ? 0.48 : 0.28)}`
        : '1px solid var(--border-subtle)',
      boxShadow: 'none',
      transition: 'background 140ms ease, border-color 140ms ease',
      cursor: 'pointer',
    };
  }
  return {
    padding: '10px 11px',
    borderRadius: 12,
    background: isActive
      ? (isDark ? 'rgba(15, 23, 42, 0.48)' : 'rgba(255, 255, 255, 0.94)')
      : (isDark ? 'rgba(148, 163, 184, 0.05)' : 'rgba(15, 23, 42, 0.025)'),
    border: isActive
      ? `1px solid ${accent}`
      : (isDark ? '1px solid rgba(148, 163, 184, 0.1)' : '1px solid rgba(15, 23, 42, 0.06)'),
    boxShadow: isActive ? `0 0 0 1px ${accent}20 inset` : 'none',
    transition: 'background 140ms ease, border-color 140ms ease, box-shadow 140ms ease',
    cursor: 'pointer',
  };
}

function loadingPlaceholderStyle(isDark: boolean, width: string, height: number): React.CSSProperties {
  return {
    width,
    height,
    borderRadius: 999,
    background: isDark ? 'rgba(148,163,184,0.16)' : 'rgba(15,23,42,0.08)',
  };
}

const LoadingBusinessState: React.FC<{ isDark: boolean; layout?: DetailLayoutMode }> = ({ isDark, layout = 'overlay' }) => (
  <div
    style={sectionCardStyle(isDark, layout)}
    aria-live="polite"
    aria-busy="true"
  >
    <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
      Loading context
    </div>
    <div style={{ marginTop: 8, fontSize: 12, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
      Fetching connected routes, nearby assets and business signals for this selection.
    </div>

    <div style={{ marginTop: 14, display: 'flex', flexDirection: 'column', gap: 9 }}>
      <div style={loadingPlaceholderStyle(isDark, '72%', 10)} />
      <div style={loadingPlaceholderStyle(isDark, '100%', 10)} />
      <div style={loadingPlaceholderStyle(isDark, '86%', 10)} />
    </div>

    <div style={{ marginTop: 14, display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 8 }}>
      {[0, 1, 2, 3].map((index) => (
        <div
          key={index}
          style={{
            padding: '10px 10px 9px',
            borderRadius: 12,
            background: isDark ? 'rgba(15,23,42,0.22)' : 'rgba(255,255,255,0.62)',
            border: isDark ? '1px solid rgba(148,163,184,0.10)' : '1px solid rgba(15,23,42,0.05)',
          }}
        >
          <div style={loadingPlaceholderStyle(isDark, '56%', 8)} />
          <div style={{ marginTop: 9, ...loadingPlaceholderStyle(isDark, '68%', 14) }} />
          <div style={{ marginTop: 8, ...loadingPlaceholderStyle(isDark, '92%', 8) }} />
          <div style={{ marginTop: 5, ...loadingPlaceholderStyle(isDark, '74%', 8) }} />
        </div>
      ))}
    </div>
  </div>
);

const AccordionSection: React.FC<{
  isDark: boolean;
  layout?: DetailLayoutMode;
  title: string;
  subtitle?: string;
  open: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}> = ({ isDark, layout = 'overlay', title, subtitle, open, onToggle, children }) => (
  <div style={sectionCardStyle(isDark, layout)}>
    <button
      type="button"
      onClick={onToggle}
      style={{
        width: '100%',
        padding: 0,
        border: 'none',
        background: 'transparent',
        color: 'inherit',
        cursor: 'pointer',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: 12,
        textAlign: 'left',
      }}
    >
      <div style={{ minWidth: 0 }}>
        <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
          {title}
        </div>
        {subtitle && (
          <div style={{ marginTop: 4, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
            {subtitle}
          </div>
        )}
      </div>
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          minWidth: 58,
          padding: '5px 10px',
          borderRadius: 999,
          background: layout === 'panel'
            ? (isDark ? 'rgba(255,255,255,0.04)' : 'rgba(15,23,42,0.04)')
            : (isDark ? 'rgba(148,163,184,0.12)' : 'rgba(15,23,42,0.05)'),
          border: layout === 'panel'
            ? '1px solid var(--border-subtle)'
            : (isDark ? '1px solid rgba(148,163,184,0.16)' : '1px solid rgba(15,23,42,0.08)'),
          fontSize: 10,
          fontWeight: 800,
          letterSpacing: '0.04em',
          textTransform: 'uppercase',
          color: 'var(--text-secondary-strong)',
          flexShrink: 0,
        }}
      >
        {open ? 'Hide' : 'Show'}
      </span>
    </button>
    {open && <div style={{ marginTop: 12 }}>{children}</div>}
  </div>
);

export const MapObjectDetail: React.FC<MapObjectDetailProps> = ({
  object,
  isDark,
  paletteTheme,
  topOffset = 10,
  layout = 'overlay',
  edgesLoading,
  edgesCount,
  visibleEdgesCount,
  traceSummary,
  activeNarrativeKey,
  onNarrativeHover,
  activeSupplyRouteIds = [],
  onSupplyRouteHover,
  directedExploreSupported = false,
  directedExploreBusy = null,
  directedExploreError = null,
  directedExploreDisabledReason,
  onDirectedExplore,
  onClose: _onClose,
}) => {
  const [viewport, setViewport] = React.useState(() => ({
    width: typeof window === 'undefined' ? 1440 : window.innerWidth,
    height: typeof window === 'undefined' ? 900 : window.innerHeight,
  }));
  const autoCompact = viewport.width < 1280 || viewport.height < 760;
  const [isCollapsed, setIsCollapsed] = React.useState(autoCompact);
  const [openSections, setOpenSections] = React.useState<Record<SectionKey, boolean>>({
    routes: true,
    context: false,
    evidence: false,
  });

  React.useEffect(() => {
    if (typeof window === 'undefined') return;
    const handleResize = () => setViewport({ width: window.innerWidth, height: window.innerHeight });
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  React.useEffect(() => {
    setIsCollapsed(autoCompact);
  }, [autoCompact, object.id]);

  /** Collapsing unmounts Route exposure blocks without firing mouseleave; clear map focus hooks. */
  React.useEffect(() => {
    if (!isCollapsed) return;
    onNarrativeHover(null);
    onSupplyRouteHover?.([]);
  }, [isCollapsed, onNarrativeHover, onSupplyRouteHover]);

  /** Hiding Route exposure via accordion unmounts the same blocks without mouseleave. */
  React.useEffect(() => {
    if (openSections.routes) return;
    onSupplyRouteHover?.([]);
    const supplyKeys = new Set(
      (traceSummary?.supplyGroups || [])
        .map((group) => String(group.narrativeKey || '').trim())
        .filter(Boolean),
    );
    if (activeNarrativeKey && supplyKeys.has(activeNarrativeKey)) {
      onNarrativeHover(null);
    }
  }, [activeNarrativeKey, onNarrativeHover, onSupplyRouteHover, openSections.routes, traceSummary?.supplyGroups]);

  React.useEffect(() => {
    setOpenSections({
      routes: Boolean(traceSummary?.supplyGroups?.length),
      context: false,
      evidence: false,
    });
  }, [object.id, traceSummary?.supplyGroups?.length]);

  const visualType = getHorizonNodeVisualType(object.type, object.canonicalPath, object.label, object.facilityKind);
  const inline = layout === 'panel';
  const accent = getNodeColors(visualType || object.type, paletteTheme).accent;
  const panelAccent = consoleAccent(isDark);
  const displayAccent = inline ? panelAccent : accent;
  const facilityKindLabel = formatFacilityKind(object.facilityKind);
  const typeLabel = object.type === 'FACILITY' && facilityKindLabel
    ? `Facility (${facilityKindLabel})`
    : formatHorizonFragmentType(object.type, object.canonicalPath, object.label);
  const countryLabel = extractCountryLabel(object.canonicalPath);
  const overlayWidth = autoCompact ? 320 : 392;
  const overlayMaxWidth = viewport.width < 980 ? 'calc(100% - 20px)' : 'calc(100% - 360px)';
  const objectRiskOverlay = (traceSummary?.objectRiskOverlay || null) as SupplyChainObjectRiskOverlay | null;
  const topProducts = Array.isArray(objectRiskOverlay?.topProducts) ? objectRiskOverlay!.topProducts!.slice(0, 3) : [];
  const topMechanisms = Array.isArray(objectRiskOverlay?.topMechanisms) ? objectRiskOverlay!.topMechanisms!.slice(0, 3) : [];
  const topMetrics = Array.isArray(objectRiskOverlay?.topMetrics) ? objectRiskOverlay!.topMetrics!.slice(0, 3) : [];
  const bottleneckLabel = objectRiskOverlay?.bottleneckNodes?.[0]?.name
    || humanizeCanonicalPath(String(objectRiskOverlay?.bottleneckNodes?.[0]?.canonicalPath || ''))
    || '';
  const objectName = getObjectDisplayName(object);
  const importance = scoreLevel(objectRiskOverlay?.criticality);
  const directCount = traceSummary?.directCount ?? edgesCount;
  const contextCount = traceSummary?.contextCount ?? 0;
  const routeCount = traceSummary?.supplyPathCount ?? 0;
  const stressedCount = traceSummary?.stressedSupplyPathCount ?? 0;
  const relationCount = directCount + contextCount;
  const businessBrief = buildBusinessBrief(objectName, objectRiskOverlay, traceSummary);
  const isDetailLoading = edgesLoading && !traceSummary;
  const whyItems = (traceSummary?.whyItMatters || [])
    .map((item) => ({
      title: normalizeBusinessCopy(item.title) || 'Why it matters',
      body: normalizeBusinessCopy(item.body),
    }))
    .filter((item) => item.body)
    .slice(0, 3);
  const actionItems = buildActionItems(objectName, traceSummary, objectRiskOverlay);
  const driverItems = buildDriverItems(topMechanisms, topMetrics, bottleneckLabel);
  const contextItems = [...(traceSummary?.directRelations || []), ...(traceSummary?.contextRelations || [])]
    .filter((relation) => relation.businessMeaning || relation.whyRelevant)
    .slice(0, 6);
  const summaryStrip = isDetailLoading
    ? [
      humanizeToken(object.status || 'ACTIVE'),
      'Loading route and business context',
    ]
    : [
      humanizeToken(object.status || 'ACTIVE'),
      `Business importance: ${importance.label.toLowerCase()}`,
      routeCount > 0 ? `${pluralize(routeCount, 'route')}` : null,
      stressedCount > 0 ? `${pluralize(stressedCount, 'route')} under strain` : null,
      relationCount > 0 ? `${pluralize(relationCount, 'connected node')}` : null,
    ].filter(Boolean);

  const impactCards = [
    {
      label: 'Business importance',
      value: importance.label,
      note: typeof objectRiskOverlay?.criticality === 'number'
        ? `${formatPercentScore(objectRiskOverlay.criticality)} score • ${importance.note}`
        : importance.note,
      accentColor: importance.accent,
    },
    {
      label: 'Route exposure',
      value: routeCount > 0 ? pluralize(routeCount, 'route') : 'Limited',
      note: routeCount > 0
        ? `${pluralize(traceSummary?.supplyGroups?.length || 0, 'product lane')}`
        : 'No mapped product routes yet',
      accentColor: mapLayerColor('supply', isDark ? 'dark' : 'light'),
    },
    {
      label: 'Active pressure',
      value: stressedCount > 0 ? pluralize(stressedCount, 'route') : 'Stable',
      note: bottleneckLabel
        ? `Main bottleneck: ${bottleneckLabel}`
        : (stressedCount > 0 ? 'Requires route review' : 'No stressed route highlighted'),
      accentColor: stressedCount > 0 ? (isDark ? '#fb7185' : '#be123c') : (isDark ? '#94a3b8' : '#64748b'),
    },
    {
      label: 'Products in play',
      value: topProducts.length > 0 ? pluralize(topProducts.length, 'product') : 'Not specified',
      note: topProducts.length > 0
        ? topProducts.map((item) => item.name || humanizeCanonicalPath(item.ref) || item.ref).filter(Boolean).join(', ')
        : 'No dominant product surfaced yet',
      accentColor: isDark ? '#60a5fa' : '#2563eb',
    },
  ];

  const hasEvidence = driverItems.length > 0 || topMetrics.length > 0 || topMechanisms.length > 0 || traceSummary?.propagationEdgeCount;
  const supplyAccent = inline ? panelAccent : mapLayerColor('supply', isDark ? 'dark' : 'light');
  const routeBadgeBackground = translucentAccent(supplyAccent, isDark ? 0.16 : 0.12);

  const toggleSection = (key: SectionKey) => {
    setOpenSections((current) => ({ ...current, [key]: !current[key] }));
  };

  return (
    <div
      style={{
        position: inline ? 'relative' : 'absolute',
        left: inline ? 'auto' : 10,
        top: inline ? 'auto' : topOffset,
        width: inline ? '100%' : overlayWidth,
        maxWidth: inline ? '100%' : overlayMaxWidth,
        maxHeight: inline ? 'none' : `calc(100% - ${topOffset + 10}px)`,
        overflow: 'auto',
        zIndex: 20,
        borderRadius: inline ? 18 : 20,
        background: inline
          ? 'color-mix(in srgb, var(--bg-elevated) 94%, black 6%)'
          : (isDark
            ? 'linear-gradient(180deg, rgba(6,18,31,0.97) 0%, rgba(3,12,22,0.96) 100%)'
            : 'linear-gradient(180deg, rgba(255,255,255,0.985) 0%, rgba(245,248,252,0.97) 100%)'),
        border: inline
          ? '1px solid var(--border-subtle)'
          : (isDark ? '1px solid rgba(148,163,184,0.22)' : '1px solid rgba(15,23,42,0.14)'),
        boxShadow: inline
          ? 'none'
          : (isDark ? '0 18px 44px rgba(0,0,0,0.44)' : '0 16px 40px rgba(15,23,42,0.16)'),
        backdropFilter: inline ? 'blur(12px)' : 'blur(18px)',
        color: 'var(--text-heading)',
      }}
    >
      {!inline && <div style={{ position: 'absolute', inset: 0, width: 3, background: accent }} />}

      <button
        type="button"
        onClick={() => setIsCollapsed((value) => !value)}
        aria-label={isCollapsed ? 'Expand asset brief' : 'Collapse asset brief'}
        style={{
          position: 'absolute',
          top: 10,
          right: 10,
          width: 24,
          height: 24,
          borderRadius: 999,
          cursor: 'pointer',
          display: 'grid',
          placeItems: 'center',
          fontSize: 15,
          lineHeight: 1,
          color: isDark ? 'rgba(226,232,240,0.88)' : 'rgba(15,23,42,0.74)',
          background: inline
            ? (isDark ? 'rgba(255,255,255,0.05)' : 'rgba(15,23,42,0.05)')
            : (isDark ? 'rgba(148,163,184,0.12)' : 'rgba(15,23,42,0.06)'),
          border: inline ? '1px solid var(--border-subtle)' : 'none',
        }}
      >
        {isCollapsed ? '+' : '−'}
      </button>

      <div
        style={{
          padding: inline
            ? (autoCompact ? '14px 40px 16px 14px' : '16px 40px 18px 16px')
            : (autoCompact ? '12px 44px 12px 14px' : '14px 44px 14px 14px'),
          display: 'flex',
          flexDirection: 'column',
          gap: inline ? 14 : 12,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
          <div
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: autoCompact ? 28 : 32,
              height: autoCompact ? 28 : 32,
              borderRadius: 999,
              flexShrink: 0,
              background: inline
                ? (isDark ? 'rgba(125,166,166,0.12)' : 'rgba(95,139,139,0.10)')
                : (isDark ? 'rgba(148,163,184,0.12)' : 'rgba(15,23,42,0.05)'),
              border: inline
                ? `1px solid ${translucentAccent(panelAccent, isDark ? 0.34 : 0.18)}`
                : (isDark ? '1px solid rgba(148,163,184,0.16)' : '1px solid rgba(15,23,42,0.08)'),
            }}
          >
            <HorizonNodeIcon type={visualType || object.type} color={displayAccent} size={15} />
          </div>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div
              style={{
                marginBottom: 3,
                fontSize: 10,
                fontWeight: 700,
                letterSpacing: '0.11em',
                textTransform: 'uppercase',
                color: inline ? 'var(--text-secondary-strong)' : accent,
              }}
            >
              {typeLabel}
            </div>
            <div style={{ fontSize: autoCompact ? 16 : 19, lineHeight: 1.12, fontWeight: 780, letterSpacing: '-0.02em' }}>
              {objectName}
            </div>
            {countryLabel && (
              <div style={{ marginTop: 4, fontSize: 12, lineHeight: 1.35, opacity: 0.78 }}>
                {countryLabel}
              </div>
            )}
          </div>
        </div>

        <div
          style={{
            padding: inline ? '10px 12px' : '9px 11px',
            borderRadius: inline ? 14 : 12,
            background: inline
              ? (isDark ? 'rgba(255,255,255,0.026)' : 'rgba(15,23,42,0.028)')
              : (isDark ? 'rgba(148,163,184,0.08)' : 'rgba(15,23,42,0.035)'),
            border: inline
              ? '1px solid var(--border-subtle)'
              : (isDark ? '1px solid rgba(148,163,184,0.12)' : '1px solid rgba(15,23,42,0.06)'),
            fontSize: 11,
            lineHeight: 1.5,
            color: 'var(--text-secondary-strong)',
          }}
        >
          {summaryStrip.join(' • ')}
        </div>

        {isCollapsed ? null : (
          isDetailLoading ? (
            <LoadingBusinessState isDark={isDark} layout={layout} />
          ) : (
          <>
            <div style={sectionCardStyle(isDark, layout)}>
              <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: inline ? 'var(--text-secondary-strong)' : accent }}>
                Business snapshot
              </div>
              <div style={{ marginTop: 7, fontSize: 13, lineHeight: 1.55, fontWeight: 650 }}>
                {businessBrief}
              </div>
              {whyItems.length > 0 && (
                <div style={{ marginTop: 12, display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {whyItems.map((item) => (
                    <div key={`${item.title}:${item.body}`} style={{ paddingLeft: 10, borderLeft: `2px solid ${displayAccent}` }}>
                      <div style={{ fontSize: 11, fontWeight: 750 }}>{item.title}</div>
                      <div style={{ marginTop: 3, fontSize: 11, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
                        {item.body}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            <div style={sectionCardStyle(isDark, layout)}>
              <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                Impact
              </div>
              <div style={{ marginTop: 10, display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 8 }}>
                {impactCards.map((item) => (
                  <div
                    key={item.label}
                    style={{
                      padding: '10px 10px 9px',
                      borderRadius: 12,
                      background: inline
                        ? (isDark ? 'rgba(255,255,255,0.026)' : 'rgba(15,23,42,0.024)')
                        : (isDark ? 'rgba(15,23,42,0.34)' : 'rgba(255,255,255,0.7)'),
                      border: inline
                        ? '1px solid var(--border-subtle)'
                        : (isDark ? '1px solid rgba(148,163,184,0.12)' : '1px solid rgba(15,23,42,0.06)'),
                    }}
                  >
                    <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                      {item.label}
                    </div>
                    <div
                      style={{
                        marginTop: 6,
                        fontSize: 15,
                        lineHeight: 1.15,
                        fontWeight: 780,
                        color: inline
                          ? (item.label === 'Route exposure' ? panelAccent : 'var(--text-heading)')
                          : item.accentColor,
                      }}
                    >
                      {item.value}
                    </div>
                    <div style={{ marginTop: 6, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                      {item.note}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div style={sectionCardStyle(isDark, layout)}>
              <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                Suggested next checks
              </div>
              <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 8 }}>
                {actionItems.map((item) => (
                  <div key={item} style={{ display: 'flex', gap: 9, alignItems: 'flex-start' }}>
                    <span
                      style={{
                        width: 7,
                        height: 7,
                        borderRadius: 999,
                        background: displayAccent,
                        marginTop: 5,
                        flexShrink: 0,
                      }}
                    />
                    <div style={{ fontSize: 11, lineHeight: 1.48, color: 'var(--text-secondary-strong)' }}>{item}</div>
                  </div>
                ))}
              </div>
              {(onDirectedExplore || directedExploreDisabledReason || directedExploreError) && (
                <div style={{ marginTop: 12 }}>
                  <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                    Analyst tools
                  </div>
                  <div style={{ marginTop: 8, display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 8 }}>
                    {([
                      {
                        direction: 'downstream' as const,
                        label: 'Trace downstream',
                        desc: 'Show exposed customers, flows and receiving hubs.',
                      },
                      {
                        direction: 'upstream' as const,
                        label: 'Trace upstream',
                        desc: 'Show suppliers, origins and source infrastructure.',
                      },
                    ]).map((action) => {
                      const active = directedExploreBusy === action.direction;
                      const disabled = !directedExploreSupported || !onDirectedExplore || Boolean(directedExploreBusy);
                      return (
                        <button
                          key={action.direction}
                          type="button"
                          onClick={() => {
                            if (disabled) return;
                            onDirectedExplore?.(action.direction);
                          }}
                          disabled={disabled}
                          style={{
                            textAlign: 'left',
                            padding: '10px 10px 9px',
                            borderRadius: 10,
                            border: inline
                              ? `1px solid ${active ? translucentAccent(panelAccent, isDark ? 0.4 : 0.24) : 'var(--border-subtle)'}`
                              : '1px solid rgba(96,165,250,0.18)',
                            background: active
                              ? (inline
                                ? (isDark ? 'rgba(125,166,166,0.12)' : 'rgba(95,139,139,0.08)')
                                : (isDark ? 'rgba(59,130,246,0.18)' : 'rgba(59,130,246,0.10)'))
                              : (inline
                                ? (isDark ? 'rgba(255,255,255,0.024)' : 'rgba(15,23,42,0.024)')
                                : (isDark ? 'rgba(148,163,184,0.06)' : 'rgba(15,23,42,0.025)')),
                            color: 'var(--text-heading)',
                            cursor: disabled ? 'not-allowed' : 'pointer',
                            opacity: disabled ? 0.55 : 1,
                          }}
                        >
                          <div style={{ fontSize: 12, fontWeight: 700, lineHeight: 1.2 }}>
                            {active ? 'Starting…' : action.label}
                          </div>
                          <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                            {action.desc}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                  {!directedExploreSupported && directedExploreDisabledReason && (
                    <div style={{ marginTop: 8, fontSize: 11, lineHeight: 1.4, color: 'var(--text-secondary-strong)' }}>
                      {normalizeBusinessCopy(directedExploreDisabledReason)}
                    </div>
                  )}
                  {directedExploreError && (
                    <div
                      style={{
                        marginTop: 8,
                        padding: '8px 9px',
                        borderRadius: 10,
                        background: isDark ? 'rgba(239,68,68,0.12)' : 'rgba(239,68,68,0.08)',
                        border: isDark ? '1px solid rgba(248,113,113,0.18)' : '1px solid rgba(239,68,68,0.16)',
                        fontSize: 11,
                        lineHeight: 1.4,
                        color: isDark ? '#fca5a5' : '#b91c1c',
                      }}
                    >
                      {normalizeBusinessCopy(directedExploreError)}
                    </div>
                  )}
                </div>
              )}
            </div>

            {traceSummary?.supplyGroups?.length ? (
              <AccordionSection
                isDark={isDark}
                layout={layout}
                title="Route exposure"
                subtitle={`${pluralize(traceSummary.supplyGroups.length, 'product lane')} linked to this asset`}
                open={openSections.routes}
                onToggle={() => toggleSection('routes')}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {traceSummary.supplyGroups.map((group) => (
                    <div
                      key={group.id}
                      onMouseEnter={() => onNarrativeHover(group.narrativeKey)}
                      onMouseLeave={() => {
                        onNarrativeHover(null);
                        onSupplyRouteHover?.([]);
                      }}
                      style={narrativeBlockStyle(isDark, activeNarrativeKey === group.narrativeKey, supplyAccent, layout)}
                    >
                      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 10 }}>
                        <div style={{ minWidth: 0 }}>
                          <div style={{ fontSize: 12, fontWeight: 780, color: inline ? 'var(--text-heading)' : supplyAccent }}>
                            {group.productLabel}
                          </div>
                          <div style={{ marginTop: 2, fontSize: 10, fontWeight: 800, letterSpacing: '0.06em', textTransform: 'uppercase', color: inline ? 'var(--text-secondary-strong)' : supplyAccent }}>
                            {directionLabel(group.direction)}
                          </div>
                          <div style={{ marginTop: 4, fontSize: 11, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
                            {normalizeBusinessCopy(group.summary)}
                          </div>
                        </div>
                        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 5, justifyContent: 'flex-end' }}>
                          <span style={{ fontSize: 10, padding: '3px 7px', borderRadius: 999, background: routeBadgeBackground }}>
                            {pluralize(group.routeCount, 'route')}
                          </span>
                          {group.stressedRouteCount > 0 && (
                            <span
                              style={{
                                fontSize: 10,
                                padding: '3px 7px',
                                borderRadius: 999,
                                background: inline
                                  ? (isDark ? 'rgba(255,255,255,0.05)' : 'rgba(15,23,42,0.05)')
                                  : (isDark ? 'rgba(248,113,113,0.16)' : 'rgba(239,68,68,0.10)'),
                              }}
                            >
                              {pluralize(group.stressedRouteCount, 'stressed route')}
                            </span>
                          )}
                          {group.hasBottleneck && (
                            <span
                              style={{
                                fontSize: 10,
                                padding: '3px 7px',
                                borderRadius: 999,
                                background: inline
                                  ? (isDark ? 'rgba(255,255,255,0.05)' : 'rgba(15,23,42,0.05)')
                                  : (isDark ? 'rgba(251,146,60,0.16)' : 'rgba(249,115,22,0.10)'),
                              }}
                            >
                              Bottleneck
                            </span>
                          )}
                        </div>
                      </div>
                      <div style={{ marginTop: 9, display: 'flex', flexDirection: 'column', gap: 6 }}>
                        {group.paths.slice(0, 2).map((path) => {
                          const routeActive = activeSupplyRouteIds.includes(String(path.id || '').trim());
                          return (
                            <div
                              key={path.id}
                              onMouseEnter={() => {
                                onNarrativeHover(null);
                                onSupplyRouteHover?.([String(path.id || '').trim()]);
                              }}
                              onMouseLeave={() => {
                                onSupplyRouteHover?.([]);
                              }}
                              style={{
                                paddingLeft: 10,
                                borderLeft: routeActive
                                  ? `2px solid ${translucentAccent(displayAccent, isDark ? 0.9 : 0.7)}`
                                  : `2px solid ${translucentAccent(displayAccent, isDark ? 0.4 : 0.24)}`,
                                borderRadius: 8,
                                background: routeActive
                                  ? translucentAccent(displayAccent, isDark ? 0.1 : 0.08)
                                  : 'transparent',
                                transition: 'background 140ms ease, border-color 140ms ease',
                              }}
                            >
                              <div style={{ fontSize: 11, fontWeight: 700 }}>
                                {path.title}
                              </div>
                              <div style={{ marginTop: 1, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                                {directionLabel(path.direction)}
                                {path.legCount > 0 ? ` • ${pluralize(path.legCount, 'leg')}` : ''}
                                {path.stressScore >= 0.58 ? ` • ${Math.round(path.stressScore * 100)} stress` : ''}
                                {path.riskTag ? ` • ${path.riskTag}` : ''}
                              </div>
                              <div style={{ marginTop: 3, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                                {normalizeBusinessCopy(path.reason)}
                              </div>
                            </div>
                          );
                        })}
                        {group.paths.length > 2 && (
                          <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>
                            +{group.paths.length - 2} more route{group.paths.length - 2 > 1 ? 's' : ''} in this lane
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </AccordionSection>
            ) : null}

            {contextItems.length ? (
              <AccordionSection
                isDark={isDark}
                layout={layout}
                title="Business context"
                subtitle={`${pluralize(contextItems.length, 'linked node')} with business meaning in the current view`}
                open={openSections.context}
                onToggle={() => toggleSection('context')}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {contextItems.map((relation) => (
                    <div
                      key={relation.edgeId || `${relation.relatedFragmentId}:${relation.type}`}
                      onMouseEnter={() => onNarrativeHover(relation.narrativeKey)}
                      onMouseLeave={() => onNarrativeHover(null)}
                      style={narrativeBlockStyle(isDark, activeNarrativeKey === relation.narrativeKey, familyAccent(relation.family, isDark), layout)}
                    >
                      <div style={{ display: 'flex', gap: 10 }}>
                        <div style={{ width: 8, flexShrink: 0, borderRadius: 999, background: familyAccent(relation.family, isDark) }} />
                        <div style={{ minWidth: 0 }}>
                          <div style={{ fontSize: 11, fontWeight: 700 }}>
                            {relation.relatedLabel}
                          </div>
                          <div style={{ marginTop: 1, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                            {humanizeToken(relation.type)}
                            {relation.relatedType ? ` • ${humanizeToken(relation.relatedType)}` : ''}
                          </div>
                          <div style={{ marginTop: 4, fontSize: 11, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
                            {normalizeBusinessCopy(relation.businessMeaning) || normalizeBusinessCopy(relation.whyRelevant)}
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </AccordionSection>
            ) : null}

            {hasEvidence ? (
              <AccordionSection
                isDark={isDark}
                layout={layout}
                title="Analyst evidence"
                subtitle="Technical drivers stay here by default so the main brief stays business-first"
                open={openSections.evidence}
                onToggle={() => toggleSection('evidence')}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {driverItems.map((item) => (
                    <div key={`${item.title}:${item.body}`} style={{ paddingLeft: 10, borderLeft: `2px solid ${displayAccent}` }}>
                      <div style={{ fontSize: 11, fontWeight: 750 }}>{item.title}</div>
                      <div style={{ marginTop: 3, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                        {item.body}
                      </div>
                    </div>
                  ))}
                  {topMetrics.slice(0, 3).map((metric) => {
                    const title = compactOverlayLabel(metric.label || metric.key);
                    if (!title) return null;
                    return (
                      <div key={`${metric.key}:${metricValueText(metric)}`} style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                        <strong style={{ color: 'var(--text-heading)' }}>{title}</strong>
                        {metricValueText(metric) ? ` • ${metricValueText(metric)}` : ''}
                      </div>
                    );
                  })}
                  {topMechanisms.slice(0, 3).map((mechanism) => {
                    const title = compactOverlayLabel(mechanism.label || mechanism.key);
                    if (!title) return null;
                    return (
                      <div key={`${mechanism.key}:${title}`} style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                        <strong style={{ color: 'var(--text-heading)' }}>{title}</strong>
                        {typeof mechanism.score === 'number' && Number.isFinite(mechanism.score) ? ` • ${formatPercentScore(mechanism.score)} score` : ''}
                      </div>
                    );
                  })}
                  {(traceSummary?.propagationEdgeCount || 0) > 0 && (
                    <div
                      onMouseEnter={() => onNarrativeHover('propagation:overview')}
                      onMouseLeave={() => onNarrativeHover(null)}
                      style={{
                        ...narrativeBlockStyle(isDark, activeNarrativeKey === 'propagation:overview', isDark ? '#94a3b8' : '#64748b', layout),
                        cursor: 'pointer',
                      }}
                    >
                      <div style={{ fontSize: 11, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
                        Secondary spread is visible across {pluralize(traceSummary?.propagationEdgeCount || 0, 'gray link')} touching {pluralize(traceSummary?.propagationNodeCount || 0, 'node')}.
                      </div>
                    </div>
                  )}
                  <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>
                    Map lines on screen: {visibleEdgesCount || traceSummary?.visibleLineCount || 0}
                  </div>
                </div>
              </AccordionSection>
            ) : null}
          </>
          )
        )}
      </div>
    </div>
  );
};

export default MapObjectDetail;

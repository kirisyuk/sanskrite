import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ThemeContext } from './ThemeProvider';
import type { FragmentProfile } from './FragmentProfileModal';

export interface MarketProfileDashboardProps {
  profile: FragmentProfile | null;
}

type ProfileSection = FragmentProfile['sections'][string];
type ProfileDataset = NonNullable<ProfileSection['datasets']>[number];

function clamp01(v: number) {
  if (v < 0) return 0;
  if (v > 1) return 1;
  return v;
}

function formatProb(p: number) {
  const pp = clamp01(p);
  return `${Math.round(pp * 100)}%`;
}

function severityColor(severity: string | undefined) {
  switch ((severity || '').toLowerCase()) {
    case 'positive':
      return '#10b981';
    case 'warning':
      return '#f59e0b';
    case 'negative':
      return '#f87171';
    default:
      return 'var(--text-secondary-strong)';
  }
}

function probabilityAccent(p: number) {
  const pp = clamp01(p);
  if (pp >= 0.66) return { fg: 'rgba(16,185,129,1)', bg: 'rgba(16,185,129,0.18)' }; // emerald
  if (pp >= 0.33) return { fg: 'rgba(245,158,11,1)', bg: 'rgba(245,158,11,0.18)' }; // amber
  return { fg: 'rgba(248,113,113,1)', bg: 'rgba(248,113,113,0.16)' }; // rose
}

function inferSignalSeverity(signal?: string) {
  const s = String(signal || '').toLowerCase();
  if (!s) return 'neutral';
  if (/(critical|severe|crisis|breach|ban|default|shutdown|attack)/.test(s)) return 'negative';
  if (/(warning|risk|tight|slow|decline|fragile|uncertain|pressure)/.test(s)) return 'warning';
  if (/(easing|improving|tailwind|accelerat|growth|momentum)/.test(s)) return 'positive';
  return 'neutral';
}

function trimTrailingZeros(s: string) {
  // "1.00" -> "1", "1.20" -> "1.2"
  return s.replace(/(\.\d*?[1-9])0+$/g, '$1').replace(/\.0+$/g, '');
}

function formatNumberWithSeparators(n: number): string {
  // Format large numbers with dot separators: 5500000000 -> 5.500.000.000
  const abs = Math.abs(n);
  const sign = n < 0 ? '-' : '';
  const intPart = Math.floor(abs);
  const fracPart = abs - intPart;
  
  // For numbers >= 1000, add dot separators (thousands separators)
  if (intPart >= 1000) {
    const intStr = intPart.toString();
    const parts: string[] = [];
    for (let i = intStr.length; i > 0; i -= 3) {
      parts.unshift(intStr.slice(Math.max(0, i - 3), i));
    }
    const formattedInt = parts.join('.');
    if (fracPart > 0) {
      const fracStr = trimTrailingZeros(fracPart.toFixed(2)).replace(/^0\./, '');
      return `${sign}${formattedInt},${fracStr}`;
    }
    return `${sign}${formattedInt}`;
  }
  
  // For smaller numbers, just format normally
  if (fracPart > 0) {
    return `${sign}${trimTrailingZeros(abs.toFixed(abs >= 10 ? 0 : 2))}`;
  }
  return `${sign}${intPart}`;
}

function sourceLabel(url: string) {
  const raw = String(url || '').trim();
  if (!raw) return '';
  try {
    const u = new URL(raw);
    return (u.hostname || raw).replace(/^www\./, '');
  } catch {
    return raw.replace(/^https?:\/\//, '').slice(0, 56);
  }
}

function humanizeScenarioMetricKey(key: string) {
  const raw = String(key || '').trim();
  if (!raw) return { label: '', period: '', unit: '' };
  const norm = raw.replace(/\s+/g, '_').replace(/__+/g, '_');
  let parts = norm.split('_').filter(Boolean);
  let period = '';
  let unit = '';
  if (parts.length > 0 && /^\d{4}$/.test(parts[0])) {
    period = parts[0];
    parts = parts.slice(1);
  }
  if (parts.length > 0) {
    const last = parts[parts.length - 1].toLowerCase();
    if (['usd', 'eur', 'gbp'].includes(last)) {
      unit = last.toUpperCase();
      parts = parts.slice(0, -1);
    } else if (['percent', 'pct'].includes(last)) {
      unit = 'percent';
      parts = parts.slice(0, -1);
    }
  }
  const words = parts.map((p) => {
    const low = p.toLowerCase();
    if (low === 'ai') return 'AI';
    if (['us', 'eu', 'sea'].includes(low)) return low.toUpperCase();
    if (low === 'mfg') return 'Manufacturing';
    return low.charAt(0).toUpperCase() + low.slice(1);
  });
  return { label: words.join(' '), period, unit };
}

function humanizeFactKey(key: string) {
  // Reuse the same heuristics.
  return humanizeScenarioMetricKey(key);
}

function parseNumberLoose(v: unknown): number | null {
  if (typeof v === 'number' && Number.isFinite(v)) return v;
  if (typeof v !== 'string') return null;
  const s = v.trim();
  if (!s) return null;
  // Strip common formatting, but preserve dots that might be decimal separators.
  // For dot-separated thousands (e.g., "5.500.000"), we need to distinguish from decimals.
  // Strategy: if there are multiple dots and the last part is 3 digits, treat as thousands separators.
  let cleaned = s
    .replace(/[, ]+/g, '')
    .replace(/^\+/, '')
    .replace(/\$/g, '')
    .replace(/€/g, '')
    .replace(/£/g, '')
    .replace(/EC\$/gi, '')
    .replace(/%/g, '');
  
  // Check if dots are thousands separators (e.g., "5.500.000" -> "5500000")
  const dotParts = cleaned.split('.');
  if (dotParts.length > 1) {
    const lastPart = dotParts[dotParts.length - 1];
    // If last part is 3 digits and there are multiple parts, likely thousands separators
    if (dotParts.length > 2 && /^\d{3}$/.test(lastPart)) {
      cleaned = cleaned.replace(/\./g, '');
    } else if (dotParts.length === 2 && /^\d{1,2}$/.test(lastPart)) {
      // Single dot with 1-2 digits after: likely decimal separator, keep it
      cleaned = cleaned.replace(/\./g, '.');
    } else {
      // Multiple dots but last part not 3 digits: might be mixed, try removing all dots
      cleaned = cleaned.replace(/\./g, '');
    }
  }
  
  const n = Number(cleaned);
  return Number.isFinite(n) ? n : null;
}

function formatPercent(n: number): string {
  // n may be 0..1 or 0..100; handle both.
  const v = n <= 1 ? n * 100 : n;
  const digits = v >= 10 ? 0 : 1;
  return `${v.toFixed(digits)}%`;
}

type UnitNorm = {
  kind: 'percent' | 'currency' | 'number' | 'other';
  currency?: 'USD' | 'EUR' | 'GBP' | 'XCD' | 'EC$' | string;
  scale?: 'K' | 'M' | 'B' | 'T';
  unitLabel?: string;
};

function normalizeUnit(unit?: string, currency?: string): UnitNorm {
  const u0 = String(unit || '').trim();
  const c0 = String(currency || '').trim();
  const blob = `${u0} ${c0}`.toLowerCase().replace(/[_/]+/g, ' ').replace(/\s+/g, ' ').trim();
  if (!blob) return { kind: 'number' };

  // Percent-like
  if (blob.includes('%') || blob.includes('percent') || blob.includes('percentage') || blob.includes('pct')) {
    return { kind: 'percent', unitLabel: '%' };
  }

  // Currency inference (extended to support EC$, XCD, Ec Dollars, etc.)
  // Check EC$ first (before USD) to avoid false positives
  let cur: 'USD' | 'EUR' | 'GBP' | 'XCD' | 'EC$' | string | undefined;
  if (blob.includes('ec$') || blob.includes('ec dollars') || blob.includes('eastern caribbean') || blob.includes('xcd')) {
    cur = 'EC$';
  } else if (blob.includes('usd') || (blob.includes('$') && !blob.includes('ec'))) {
    cur = 'USD';
  } else if (blob.includes('eur') || blob.includes('€')) {
    cur = 'EUR';
  } else if (blob.includes('gbp') || blob.includes('£')) {
    cur = 'GBP';
  }

  // Scale inference
  let scale: UnitNorm['scale'];
  if (blob.includes('trillion') || /\b(t)\b/.test(blob)) scale = 'T';
  else if (blob.includes('billion') || /\b(b)\b/.test(blob)) scale = 'B';
  else if (blob.includes('million') || /\b(m)\b/.test(blob)) scale = 'M';
  else if (blob.includes('thousand') || /\b(k)\b/.test(blob)) scale = 'K';

  if (cur) {
    const symbol = cur === 'USD' ? '$' : cur === 'EUR' ? '€' : cur === 'GBP' ? '£' : cur === 'EC$' ? 'EC$' : cur;
    return { kind: 'currency', currency: cur, scale, unitLabel: symbol };
  }

  // Non-currency unit text: keep but normalize "percentage"->"%"
  return { kind: 'other', unitLabel: u0 || c0 || '' };
}

function formatCurrencyCompact(n: number, currency: string): string {
  const cur = String(currency || '').toUpperCase();
  const symbol = cur === 'USD' ? '$' : cur === 'EUR' ? '€' : cur === 'GBP' ? '£' : cur === 'EC$' || cur === 'XCD' ? 'EC$' : `${cur} `;
  const abs = Math.abs(n);
  const sign = n < 0 ? '-' : '';
  
  // For very large numbers (>= 1B), use compact notation (B/T)
  if (abs >= 1_000_000_000_000) return `${sign}${symbol}${trimTrailingZeros((abs / 1_000_000_000_000).toFixed(1))}T`;
  if (abs >= 1_000_000_000) return `${sign}${symbol}${trimTrailingZeros((abs / 1_000_000_000).toFixed(1))}B`;
  
  // For numbers >= 1000 but < 1B, use dot separators (thousands separators)
  if (abs >= 1000) {
    const intPart = Math.floor(abs);
    const fracPart = abs - intPart;
    const intStr = intPart.toString();
    const parts: string[] = [];
    for (let i = intStr.length; i > 0; i -= 3) {
      parts.unshift(intStr.slice(Math.max(0, i - 3), i));
    }
    const formattedInt = parts.join('.');
    if (fracPart > 0) {
      const fracStr = trimTrailingZeros(fracPart.toFixed(2)).replace(/^0\./, '');
      return `${sign}${symbol}${formattedInt},${fracStr}`;
    }
    return `${sign}${symbol}${formattedInt}`;
  }
  
  // For smaller numbers, format normally
  return `${sign}${symbol}${trimTrailingZeros(abs.toFixed(abs >= 10 ? 0 : 2))}`;
}

function formatWithUnit(n: number, unit?: string, currency?: string): string {
  const nu = normalizeUnit(unit, currency);
  if (nu.kind === 'percent') return formatPercent(n);
  if (nu.kind === 'currency' && nu.currency) {
    // Apply scale if explicit.
    if (nu.scale === 'T') return formatCurrencyCompact(n * 1_000_000_000_000, nu.currency);
    if (nu.scale === 'B') return formatCurrencyCompact(n * 1_000_000_000, nu.currency);
    if (nu.scale === 'M') return formatCurrencyCompact(n * 1_000_000, nu.currency);
    if (nu.scale === 'K') return formatCurrencyCompact(n * 1_000, nu.currency);
    return formatCurrencyCompact(n, nu.currency);
  }
  // Non-currency scale only
  const u = String(unit || '').toLowerCase().trim();
  if (u === 'billion') return `${trimTrailingZeros(n.toFixed(n >= 10 ? 0 : 1))}B`;
  if (u === 'million') return `${trimTrailingZeros(n.toFixed(n >= 10 ? 0 : 1))}M`;
  if (u === 'trillion') return `${trimTrailingZeros(n.toFixed(n >= 10 ? 0 : 1))}T`;
  return `${n}`;
}

function formatFactValue(value: unknown, unit?: string): string {
  const raw = String(value ?? '').trim();
  if (!raw) return '';
  // If already contains a unit/currency marker, keep as-is (but may need to add separators).
  if (/[€$£%]/.test(raw) || /\b(usd|eur|gbp|ec\$|xcd|ec dollars|million|billion|trillion|weeks?|days?|hours?)\b/i.test(raw)) {
    // Try to extract and reformat the number part if it's a long unformatted number
    const numMatch = raw.match(/(\d{4,})/);
    if (numMatch && numMatch[1]) {
      const longNum = parseInt(numMatch[1], 10);
      if (longNum >= 1000 && longNum < 10000) {
        return raw;
      }
      if (longNum >= 1_000_000_000_000) {
        return raw.replace(numMatch[1], `${trimTrailingZeros((longNum / 1_000_000_000_000).toFixed(1))}T`);
      }
      if (longNum >= 1_000_000_000) {
        return raw.replace(numMatch[1], `${trimTrailingZeros((longNum / 1_000_000_000).toFixed(1))}B`);
      }
      if (longNum >= 1_000_000) {
        return raw.replace(numMatch[1], `${trimTrailingZeros((longNum / 1_000_000).toFixed(1))}M`);
      }
      if (longNum >= 10000) {
        return raw.replace(numMatch[1], formatNumberWithSeparators(longNum));
      }
    }
    return raw;
  }
  const n = parseNumberLoose(raw);
  if (n === null) return raw;
  const nu = normalizeUnit(unit);
  if (nu.kind === 'percent') return formatPercent(n);
  if (nu.kind === 'currency' && nu.currency) return formatCurrencyCompact(n, nu.currency);
  // For very large plain numbers, use compact format (B/T)
  const abs = Math.abs(n);
  if (abs >= 1_000_000_000_000) return `${trimTrailingZeros((n / 1_000_000_000_000).toFixed(1))}T`;
  if (abs >= 1_000_000_000) return `${trimTrailingZeros((n / 1_000_000_000).toFixed(1))}B`;
  if (abs >= 1_000_000) return `${trimTrailingZeros((n / 1_000_000).toFixed(1))}M`;
  if (abs >= 1000 && abs < 10000) return raw;
  if (abs >= 10000) return formatNumberWithSeparators(n);
  return raw; // unknown unit: don't guess
}

/**
 * Format any value for display - handles raw numbers, pre-formatted strings, 
 * and mixed content. Used for KPIs, scenario metrics, and other dashboard values.
 */
function formatDisplayValue(value: unknown, unit?: string): string {
  const raw = String(value ?? '').trim();
  if (!raw) return '';
  
  // If already formatted with currency symbols or known units, just clean up large numbers
  if (/[€$£]/.test(raw)) {
    // Already has currency symbol - check if number needs formatting
    const numMatch = raw.match(/(\d{4,})/g);
    if (numMatch) {
      let result = raw;
      for (const num of numMatch) {
        const longNum = parseInt(num, 10);
        if (longNum >= 10000 && !num.includes('.')) {
          result = result.replace(num, formatNumberWithSeparators(longNum));
        }
      }
      return result;
    }
    return raw;
  }
  
  // If has B/M/T suffix, already compact - just return
  if (/\d+\.?\d*\s*[BMT]\b/i.test(raw)) {
    return raw;
  }
  
  // If contains "billion", "million", "trillion" - already formatted
  if (/\b(million|billion|trillion)\b/i.test(raw)) {
    return raw;
  }
  
  // Try to parse as pure number
  const n = parseNumberLoose(raw);
  if (n !== null && Number.isFinite(n)) {
    const abs = Math.abs(n);
    
    // Check if unit suggests currency
    const nu = normalizeUnit(unit);
    if (nu.kind === 'currency' && nu.currency) {
      return formatCurrencyCompact(n, nu.currency);
    }
    if (nu.kind === 'percent') {
      return formatPercent(n);
    }
    
    // For large numbers without explicit unit, use compact format or separators
    if (abs >= 1_000_000_000_000) return `${trimTrailingZeros((n / 1_000_000_000_000).toFixed(1))}T`;
    if (abs >= 1_000_000_000) return `${trimTrailingZeros((n / 1_000_000_000).toFixed(1))}B`;
    if (abs >= 1_000_000) return `${trimTrailingZeros((n / 1_000_000).toFixed(1))}M`;
    // Avoid reformatting 4-digit values (e.g., years)
    if (abs >= 1000 && abs < 10000) return raw;
    if (abs >= 10000) return formatNumberWithSeparators(n);
    
    // Small number - return as-is with reasonable precision
    return trimTrailingZeros(n.toFixed(n >= 10 ? 0 : 2));
  }
  
  // Not a pure number - might be mixed text like "550000000000 USD"
  // Try to find and format embedded numbers
  const numMatch = raw.match(/(\d{4,})/g);
  if (numMatch) {
    let result = raw;
    for (const num of numMatch) {
      const longNum = parseInt(num, 10);
      if (longNum >= 1_000_000_000_000) {
        result = result.replace(num, `${trimTrailingZeros((longNum / 1_000_000_000_000).toFixed(1))}T`);
      } else if (longNum >= 1_000_000_000) {
        result = result.replace(num, `${trimTrailingZeros((longNum / 1_000_000_000).toFixed(1))}B`);
      } else if (longNum >= 1_000_000) {
        result = result.replace(num, `${trimTrailingZeros((longNum / 1_000_000).toFixed(1))}M`);
      } else if (longNum >= 10000) {
        result = result.replace(num, formatNumberWithSeparators(longNum));
      }
    }
    return result;
  }
  
  return raw;
}

function formatUnitBadge(unit?: string, currency?: string): string {
  const nu = normalizeUnit(unit, currency);
  if (nu.kind === 'percent') return '%';
  if (nu.kind === 'currency' && nu.currency) {
    const sym = nu.currency === 'USD' ? '$' : nu.currency === 'EUR' ? '€' : nu.currency === 'GBP' ? '£' : nu.currency === 'EC$' || nu.currency === 'XCD' ? 'EC$' : nu.currency;
    if (nu.scale) return `${sym}${nu.scale}`;
    return sym;
  }
  return '';
}

function extractExchangeRate(text: string): string | null {
  // Look for exchange rate patterns like "US$1.00 to EC$2.70", "1 USD = 2.70 EC$", "peg of US$1.00 to EC$2.70", etc.
  const patterns = [
    /(?:exchange\s+rate|peg|fixed\s+peg|rate)[:\s]+(?:US\$|USD)\s*1\.00\s*(?:to|=\s*)?\s*([A-Z]{2,3}\$?\s*\d+\.?\d*)/i,
    /(?:US\$|USD)\s*1\.00\s*(?:to|=\s*)\s*([A-Z]{2,3}\$?\s*\d+\.?\d*)/i,
    /(\d+\.?\d*)\s*(?:EC\$|XCD|Ec Dollars?)\s*(?:per|to|=\s*)?\s*(?:US\$|USD)\s*1/i,
    /(?:US\$|USD)\s*1\s*(?:=\s*|to\s*)(\d+\.?\d*)\s*(?:EC\$|XCD|Ec Dollars?)/i,
  ];
  
  for (const pattern of patterns) {
    const match = text.match(pattern);
    if (match && match[1]) {
      const rate = match[1].trim();
      // Normalize: if it's just a number, assume it's EC$ per USD
      if (/^\d+\.?\d*$/.test(rate)) {
        return `US$1.00 = EC$${rate}`;
      }
      // If it already includes currency, use as-is but normalize format
      return `US$1.00 = ${rate.replace(/EC\$/gi, 'EC$').replace(/XCD/gi, 'EC$')}`;
    }
  }
  
  return null;
}

const SectionCard: React.FC<{
  title: string;
  right?: React.ReactNode;
  children: React.ReactNode;
  theme: string;
}> = ({ title, right, children, theme }) => {
  return (
    <div
      className="hzn-print-card hzn-print-surface"
      style={{
        borderRadius: 14,
        border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
        background:
          theme === 'dark'
            ? 'radial-gradient(1200px 420px at 10% 0%, rgba(59,130,246,0.10) 0%, rgba(0,0,0,0.00) 55%), rgba(0,0,0,0.14)'
            : 'radial-gradient(1200px 420px at 10% 0%, rgba(59,130,246,0.08) 0%, rgba(255,255,255,0.0) 60%), rgba(0,0,0,0.02)',
        padding: 16,
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
        boxShadow: theme === 'dark' ? '0 18px 55px rgba(0,0,0,0.35)' : '0 18px 55px rgba(0,0,0,0.08)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
        <div style={{ fontSize: 14, fontWeight: 900, color: 'var(--text-heading)' }}>{title}</div>
        {right}
      </div>
      {children}
    </div>
  );
};

const DatasetView: React.FC<{ dataset: ProfileDataset; theme: string }> = ({ dataset, theme }) => {
  const [hoverIdx, setHoverIdx] = React.useState<number | null>(null);
  const type = dataset.type;
  const series = dataset.series || [];

  // Lightweight chart primitives (no external deps):
  const renderTimeSeries = () => {
    const s0 = series[0];
    const pts = (s0?.points || []).filter((p: { y: number }) => Number.isFinite(p.y));
    if (pts.length < 2) {
      return (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          Not enough data points to render a chart.
        </div>
      );
    }

    const w = 520;
    const h = 120;
    // Reserve space for Y-axis labels (left).
    const padR = 10;
    const padT = 10;
    const padB = 10;
    const padL = 52;
    const ys = pts.map((p) => p.y);
    const minY = Math.min(...ys);
    const maxY = Math.max(...ys);
    const range = maxY - minY || 1;

    const plotLeft = padL;
    const plotRight = w - padR;
    const plotTop = padT;
    const plotBottom = h - padB;
    const plotW = plotRight - plotLeft;
    const plotH = plotBottom - plotTop;

    const toX = (i: number) => plotLeft + (i * plotW) / (pts.length - 1);
    const toY = (v: number) => plotTop + plotH * (1 - (v - minY) / range);

    const d = pts
      .map((p, i: number) => `${i === 0 ? 'M' : 'L'} ${toX(i).toFixed(2)} ${toY(p.y).toFixed(2)}`)
      .join(' ');

    const area = `${d} L ${plotRight.toFixed(2)} ${plotBottom.toFixed(2)} L ${plotLeft.toFixed(2)} ${plotBottom.toFixed(2)} Z`;
    const gradId = `ts-grad-${dataset.id || dataset.title}`.replace(/[^a-zA-Z0-9_-]/g, '').slice(0, 48);
    const lastIdx = pts.length - 1;
    const last = pts[lastIdx];
    const lastX = toX(lastIdx);
    const lastY = toY(last.y);
    const lastLabel = formatWithUnit(last.y, dataset.unit, dataset.currency);
    const stroke = theme === 'dark' ? 'rgba(96,165,250,0.98)' : 'rgba(59,130,246,0.98)';
    const glow = theme === 'dark' ? 'rgba(59,130,246,0.55)' : 'rgba(59,130,246,0.28)';
    const labelAnchor: 'start' | 'end' = lastX > w * 0.72 ? 'end' : 'start';
    const labelX = labelAnchor === 'end' ? Math.max(plotLeft, lastX - 10) : Math.min(plotRight, lastX + 10);
    const labelY = Math.min(plotBottom, Math.max(plotTop + 12, lastY - 10));

    const activeIdx = hoverIdx === null ? null : Math.max(0, Math.min(lastIdx, hoverIdx));
    const active = activeIdx === null ? null : pts[activeIdx];
    const activeX = activeIdx === null ? 0 : toX(activeIdx);
    const activeY = activeIdx === null ? 0 : toY(active!.y);
    const tooltipValue = active ? formatWithUnit(active.y, dataset.unit, dataset.currency) : '';
    const tooltipLabel = active ? String(active.x || '') : '';
    const tooltipAnchor: 'start' | 'end' = activeIdx !== null && activeX > w * 0.66 ? 'end' : 'start';
    const tooltipX = activeIdx === null ? 0 : tooltipAnchor === 'end' ? Math.max(plotLeft, activeX - 10) : Math.min(plotRight, activeX + 10);
    const tooltipY = activeIdx === null ? 0 : Math.max(plotTop + 12, Math.min(plotBottom - 12, activeY - 10));

    const tickVals = [maxY, (maxY + minY) / 2, minY];
    const tickKey = (v: number) => `${Math.round(v * 1e6) / 1e6}`;

    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <svg
          width="100%"
          viewBox={`0 0 ${w} ${h}`}
          style={{ borderRadius: 12, cursor: 'crosshair' }}
          onMouseLeave={() => setHoverIdx(null)}
          onMouseMove={(e) => {
            const svg = e.currentTarget;
            const rect = svg.getBoundingClientRect();
            const x = ((e.clientX - rect.left) / rect.width) * w;
            const t = (x - plotLeft) / plotW;
            const idx = Math.round(t * (pts.length - 1));
            setHoverIdx(Math.max(0, Math.min(pts.length - 1, idx)));
          }}
        >
          <defs>
            <linearGradient id={gradId} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor={stroke} stopOpacity="0.34" />
              <stop offset="60%" stopColor={stroke} stopOpacity="0.10" />
              <stop offset="100%" stopColor={stroke} stopOpacity="0.00" />
            </linearGradient>
          </defs>
          {/* Y-axis grid & labels */}
          {tickVals.map((tv) => (
            <g key={tickKey(tv)} style={{ pointerEvents: 'none' as any }}>
              <line
                x1={plotLeft}
                x2={plotRight}
                y1={toY(tv)}
                y2={toY(tv)}
                stroke={theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}
              />
              <text
                x={plotLeft - 8}
                y={toY(tv) + 4}
                textAnchor="end"
                fill={theme === 'dark' ? 'rgba(255,255,255,0.55)' : 'rgba(15,23,42,0.55)'}
                fontSize="10"
                fontWeight="800"
              >
                {formatWithUnit(tv, dataset.unit, dataset.currency)}
              </text>
            </g>
          ))}
          <path d={area} fill={`url(#${gradId})`} />
          <path
            d={d}
            fill="none"
            stroke={stroke}
            strokeWidth="2.6"
          />
          {pts.map((p, i: number) => (
            <circle
              key={i}
              cx={toX(i)}
              cy={toY(p.y)}
              r={i === lastIdx ? 4 : 3}
              fill={stroke}
              opacity={activeIdx !== null && i !== activeIdx ? 0.25 : 1}
            />
          ))}
          {/* Direct label near last point */}
          <text
            x={labelX}
            y={labelY}
            textAnchor={labelAnchor}
            fill={theme === 'dark' ? 'rgba(255,255,255,0.82)' : 'rgba(15,23,42,0.74)'}
            fontSize="11"
            fontWeight="800"
            style={{ paintOrder: 'stroke', stroke: glow, strokeWidth: 3 }}
          >
            {lastLabel}
          </text>

          {/* Hover affordances */}
          {active && (
            <g style={{ pointerEvents: 'none' as any }}>
              <line
                x1={activeX}
                x2={activeX}
                y1={plotTop}
                y2={plotBottom}
                stroke={theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}
                strokeDasharray="4 4"
              />
              <circle cx={activeX} cy={activeY} r={5.5} fill={stroke} />
              <circle cx={activeX} cy={activeY} r={9} fill={stroke} opacity={0.14} />

              <text
                x={tooltipX}
                y={tooltipY}
                textAnchor={tooltipAnchor}
                fill={theme === 'dark' ? 'rgba(255,255,255,0.86)' : 'rgba(15,23,42,0.82)'}
                fontSize="11"
                fontWeight="900"
                style={{ paintOrder: 'stroke', stroke: theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.8)', strokeWidth: 4 }}
              >
                {tooltipLabel ? `${tooltipLabel}: ` : ''}{tooltipValue}
              </text>
            </g>
          )}
        </svg>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-secondary-strong)' }}>
          <span>{pts[0]?.x}</span>
          <span>
            {formatUnitBadge(dataset.unit, dataset.currency) ? `(${formatUnitBadge(dataset.unit, dataset.currency)})` : ''}
          </span>
          <span>{pts[pts.length - 1]?.x}</span>
        </div>
      </div>
    );
  };

  const renderBars = () => {
    const s0 = series[0];
    const pts = (s0?.points || []).filter((p: { y: number }) => Number.isFinite(p.y));
    if (pts.length === 0) {
      return (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          No bar data available.
        </div>
      );
    }
    const top = [...pts].sort((a, b) => b.y - a.y).slice(0, 8);
    const maxV = Math.max(...top.map((p) => p.y)) || 1;
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {top.map((p, idx) => (
          <div key={idx} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <div style={{ width: 220, fontSize: 12, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {p.x}
            </div>
            <div
              style={{
                flex: 1,
                height: 10,
                borderRadius: 999,
                background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'rgba(15,23,42,0.06)',
                overflow: 'hidden',
              }}
            >
              <div
                style={{
                  width: `${Math.max(2, (p.y / maxV) * 100)}%`,
                  height: '100%',
                  background:
                    theme === 'dark'
                      ? 'linear-gradient(90deg, rgba(96,165,250,0.95) 0%, rgba(59,130,246,0.95) 60%, rgba(34,211,238,0.75) 120%)'
                      : 'linear-gradient(90deg, rgba(59,130,246,0.92) 0%, rgba(37,99,235,0.92) 55%, rgba(6,182,212,0.70) 120%)',
                  boxShadow: theme === 'dark' ? '0 0 0 1px rgba(59,130,246,0.18), 0 10px 22px rgba(59,130,246,0.18)' : '0 10px 22px rgba(59,130,246,0.10)',
                }}
              />
            </div>
            <div style={{ width: 96, textAlign: 'right', fontSize: 12, color: 'var(--text-secondary-strong)', fontVariantNumeric: 'tabular-nums' as any }}>
              {formatWithUnit(p.y, dataset.unit, dataset.currency)}
            </div>
          </div>
        ))}
      </div>
    );
  };

  const renderTable = () => {
    const table = dataset.table;
    const cols = table?.columns || [];
    const rows = table?.rows || [];
    if (!table || cols.length === 0 || rows.length === 0) {
      return (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          No table data available.
        </div>
      );
    }
    return (
      <div style={{ overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
          <thead>
            <tr>
              {cols.map((c: { id: string; title: string; format?: string }) => (
                <th
                  key={c.id}
                  style={{
                    textAlign: 'left',
                    padding: '10px 10px',
                    borderBottom: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                    color: 'var(--text-secondary-strong)',
                    fontWeight: 800,
                    whiteSpace: 'nowrap',
                  }}
                >
                  {c.title || c.id}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, 20).map((r: Record<string, any>, i: number) => (
              <tr key={i}>
                {cols.map((c: { id: string; format?: string }) => {
                  const raw = (r as any)[c.id];
                  const fmt = (c.format || '').toLowerCase();
                  const n = typeof raw === 'number' ? raw : parseNumberLoose(raw);
                  let rendered = String(raw ?? '');
                  if (n !== null && fmt === 'percent') {
                    rendered = formatPercent(n);
                  } else if (n !== null && fmt === 'currency') {
                    rendered = formatCurrencyCompact(n, dataset.currency || 'USD');
                  } else if (n !== null && (fmt === 'number' || fmt === '')) {
                    // If dataset has unit/currency, apply it, otherwise keep raw.
                    rendered = dataset.unit || dataset.currency ? formatWithUnit(n, dataset.unit, dataset.currency) : String(raw ?? '');
                  }
                  return (
                  <td
                    key={c.id}
                    style={{
                      padding: '10px 10px',
                      borderBottom: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}`,
                      color: 'var(--text-heading)',
                      verticalAlign: 'top',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {rendered}
                  </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  };

  return (
    <div
      className="hzn-print-card hzn-print-surface"
      style={{
        padding: '14px 16px',
        borderRadius: 14,
        border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
        background:
          theme === 'dark'
            ? 'radial-gradient(900px 260px at 12% 0%, rgba(59,130,246,0.16) 0%, rgba(0,0,0,0) 55%), rgba(0,0,0,0.10)'
            : 'radial-gradient(900px 260px at 12% 0%, rgba(59,130,246,0.10) 0%, rgba(255,255,255,0) 55%), rgba(0,0,0,0.015)',
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
        boxShadow: theme === 'dark' ? '0 18px 55px rgba(0,0,0,0.30)' : '0 18px 55px rgba(0,0,0,0.07)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
        <div style={{ fontSize: 13, fontWeight: 900, color: 'var(--text-heading)' }}>{dataset.title}</div>
        {formatUnitBadge(dataset.unit, dataset.currency) ? (
          <div
            style={{
              fontSize: 11,
              fontWeight: 900,
              color: theme === 'dark' ? 'rgba(255,255,255,0.72)' : 'rgba(15,23,42,0.62)',
              padding: '4px 10px',
              borderRadius: 999,
              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)'}`,
              background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
              fontVariantNumeric: 'tabular-nums' as any,
            }}
          >
            {formatUnitBadge(dataset.unit, dataset.currency)}
          </div>
        ) : null}
      </div>
      {dataset.description && (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.5 }}>
          {dataset.description}
        </div>
      )}
      {type === 'timeseries' && renderTimeSeries()}
      {(type === 'bar' || type === 'pie') && renderBars()}
      {type === 'table' && renderTable()}
      {(dataset.sources || []).length > 0 && (
        <details style={{ marginTop: 2 }}>
          <summary style={{ cursor: 'pointer', fontSize: 11, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
            Sources ({(dataset.sources || []).length})
          </summary>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 8 }}>
            {(dataset.sources || []).slice(0, 10).map((s: string, i: number) => (
              <a
                key={i}
                href={s}
                target="_blank"
                rel="noopener noreferrer"
                title={s}
                style={{
                  fontSize: 11,
                  color: theme === 'dark' ? 'rgba(255,255,255,0.70)' : 'rgba(15,23,42,0.70)',
                  textDecoration: 'none',
                  padding: '5px 10px',
                  borderRadius: 999,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)'}`,
                  background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
                  maxWidth: 260,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {sourceLabel(s) || s}
              </a>
            ))}
          </div>
        </details>
      )}
    </div>
  );
};

/**
 * Dashboard component for displaying Market fragment profiles.
 * Shows structured sections: overview, size_metrics, growth_outlook, competition, trends, barriers, regional.
 */
export const MarketProfileDashboard: React.FC<MarketProfileDashboardProps> = ({ profile }) => {
  const { theme } = React.useContext(ThemeContext);

  if (!profile) {
    return (
      <div
        style={{
          padding: '40px 20px',
          textAlign: 'center',
          color: 'var(--text-secondary-strong)',
          fontSize: 13,
        }}
      >
        No profile data available
      </div>
    );
  }

  // Get sections in order
  const sectionKeys = Object.keys(profile.sections || {}).sort((a, b) => {
    const orderA = profile.sections[a]?.order || 100;
    const orderB = profile.sections[b]?.order || 100;
    return orderA - orderB;
  });

  if (sectionKeys.length === 0) {
    return (
      <div
        style={{
          padding: '40px 20px',
          textAlign: 'center',
          color: 'var(--text-secondary-strong)',
          fontSize: 13,
        }}
      >
        Profile has no sections
      </div>
    );
  }

  return (
    <div
      style={{
        padding: '24px',
        display: 'flex',
        flexDirection: 'column',
        gap: 24,
      }}
    >
      {/* Updated time */}
      <div
        style={{
          fontSize: 11,
          color: 'var(--text-secondary-strong)',
          marginTop: -6,
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
        }}
      >
        <div>
          <span style={{ fontWeight: 800 }}>Updated at</span>{' '}
          {new Date(profile.capturedAt).toLocaleString('en-US', {
            year: 'numeric',
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
          })}
        </div>
        <div style={{ color: 'var(--text-secondary)' }}>
          AI-assisted document. Underlying methodology is proprietary to Etigram Horizon.{' '}
          <a
            href="/terms"
            target="_blank"
            rel="noopener noreferrer"
            style={{
              color: 'var(--text-secondary-strong)',
              fontWeight: 700,
              textDecoration: 'none',
            }}
          >
            Terms of Use
          </a>
        </div>
      </div>

      {/* Dashboard blocks (v2+) */}
      {profile.dashboard && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {profile.dashboard.thesis && (
            <SectionCard
              theme={theme}
              title="Thesis"
            >
              <div
                style={{
                  fontSize: 15,
                  color: 'var(--text-heading)',
                  lineHeight: 1.7,
                  letterSpacing: '0.01em',
                }}
              >
                {profile.dashboard.thesis}
              </div>
            </SectionCard>
          )}

          {(profile.dashboard.kpis || []).length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12 }}>
              {(profile.dashboard.kpis || []).slice(0, 12).map((kpi) => (
                <div
                  key={kpi.id}
                  style={{
                    flex: '1 1 180px',
                    minWidth: 180,
                    padding: '12px 14px',
                    borderRadius: 14,
                    border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)'}`,
                    background:
                      theme === 'dark'
                        ? 'radial-gradient(420px 160px at 12% 0%, rgba(59,130,246,0.18) 0%, rgba(0,0,0,0) 58%), rgba(0,0,0,0.12)'
                        : 'radial-gradient(420px 160px at 12% 0%, rgba(59,130,246,0.12) 0%, rgba(255,255,255,0) 58%), rgba(0,0,0,0.015)',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 6,
                    boxShadow: theme === 'dark' ? '0 18px 55px rgba(0,0,0,0.25)' : '0 18px 55px rgba(0,0,0,0.06)',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
                    <div style={{ fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>{kpi.label}</div>
                  </div>
                  <div
                    style={{
                      fontSize: 18,
                      fontWeight: 950 as any,
                      color: 'var(--text-heading)',
                      fontVariantNumeric: 'tabular-nums' as any,
                    }}
                  >
                    {formatDisplayValue(kpi.value)}
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{kpi.subtext || ''}</div>
                    {kpi.trendValue ? (
                      <div style={{ fontSize: 11, fontWeight: 800, color: severityColor(kpi.severity) }}>{kpi.trendValue}</div>
                    ) : (
                      <div />
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}

          {(profile.dashboard.scenarios || []).length > 0 && (
            <SectionCard theme={theme} title="Scenarios">
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))',
                  gap: 12,
                }}
              >
                {(profile.dashboard.scenarios || []).slice(0, 6).map((sc) => {
                  const pp = clamp01(sc.probability);
                  const acc = probabilityAccent(pp);
                  const pct = formatProb(pp);
                  return (
                  <div
                    key={sc.id}
                    className="hzn-print-card hzn-print-surface"
                    style={{
                      padding: 14,
                      borderRadius: 14,
                      border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                      background:
                        theme === 'dark'
                          ? `radial-gradient(520px 220px at 10% 0%, ${acc.bg} 0%, rgba(0,0,0,0) 60%), rgba(255,255,255,0.03)`
                          : `radial-gradient(520px 220px at 10% 0%, ${acc.bg} 0%, rgba(255,255,255,0) 60%), rgba(0,0,0,0.01)`,
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 10,
                      boxShadow: theme === 'dark' ? '0 18px 55px rgba(0,0,0,0.28)' : '0 18px 55px rgba(0,0,0,0.07)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                      <div style={{ fontSize: 13, fontWeight: 950 as any, color: 'var(--text-heading)' }}>{sc.name}</div>
                      <div
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 10,
                          padding: '6px 10px',
                          borderRadius: 999,
                          border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)'}`,
                          background: theme === 'dark' ? 'rgba(0,0,0,0.20)' : 'rgba(255,255,255,0.65)',
                        }}
                        title="Scenario probability"
                      >
                        <div style={{ fontSize: 11, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>Prob</div>
                        <div style={{ width: 86, height: 6, borderRadius: 999, background: theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)', overflow: 'hidden' }}>
                          <div style={{ width: `${Math.max(3, pp * 100)}%`, height: '100%', background: acc.fg }} />
                        </div>
                        <div style={{ fontSize: 11, fontWeight: 950 as any, color: 'var(--text-heading)', fontVariantNumeric: 'tabular-nums' as any }}>{pct}</div>
                      </div>
                    </div>
                    {Array.isArray((sc as any).metricItems) && (sc as any).metricItems.length > 0 ? (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
                        {((sc as any).metricItems as Array<any>).slice(0, 6).map((m, idx) => {
                          const label = String(m?.label || '').trim() || humanizeScenarioMetricKey(String(m?.key || '')).label || String(m?.key || '').trim();
                          const period = String(m?.period || '').trim();
                          const rawValue = String(m?.value || '').trim();
                          const unitHint = String(m?.unit || '').trim();
                          const value = formatDisplayValue(rawValue, unitHint);
                          if (!label || !value) return null;
                          return (
                            <div
                              key={`${m?.key || idx}`}
                              title={String(m?.key || '')}
                              style={{
                                padding: '8px 10px',
                                borderRadius: 12,
                                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}`,
                                background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.01)',
                                flex: '1 1 220px',
                                minWidth: 220,
                              }}
                            >
                              <div style={{ fontSize: 11, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                                {label}{period ? ` (${period})` : ''}
                              </div>
                              <div style={{ fontSize: 13, fontWeight: 950 as any, color: 'var(--text-heading)', marginTop: 4, fontVariantNumeric: 'tabular-nums' as any }}>
                                {value}
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    ) : sc.metrics && Object.keys(sc.metrics).length > 0 ? (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
                        {Object.entries(sc.metrics).slice(0, 6).map(([k, v]) => {
                          const { label, period, unit } = humanizeScenarioMetricKey(k);
                          const showLabel = label || k;
                          const suffix = period ? ` (${period})` : '';
                          const unitSuffix = unit && !suffix.includes(unit) ? ` [${unit}]` : '';
                          const formattedValue = formatDisplayValue(v, unit);
                          return (
                            <div key={k} style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }} title={k}>
                              <span style={{ fontWeight: 800 }}>{showLabel}{suffix}{unitSuffix}:</span>{' '}
                              <span style={{ color: 'var(--text-heading)' }}>{formattedValue}</span>
                            </div>
                          );
                        })}
                      </div>
                    ) : null}
                    {((sc.assumptions || []).length > 0 || (sc.triggers || []).length > 0) && (
                      <details style={{ marginTop: 2 }}>
                        <summary style={{ cursor: 'pointer', fontSize: 12, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                          Evidence & conditions
                        </summary>
                        {(sc.assumptions || []).length > 0 && (
                          <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.6, marginTop: 8 }}>
                            <div style={{ fontWeight: 900, marginBottom: 6 }}>Assumptions</div>
                            <ul style={{ margin: 0, paddingLeft: 18 }}>
                              {(sc.assumptions || []).slice(0, 6).map((a, i) => (
                                <li key={i}>{a}</li>
                              ))}
                            </ul>
                          </div>
                        )}
                        {(sc.triggers || []).length > 0 && (
                          <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.6, marginTop: 10 }}>
                            <div style={{ fontWeight: 900, marginBottom: 6 }}>Triggers</div>
                            <ul style={{ margin: 0, paddingLeft: 18 }}>
                              {(sc.triggers || []).slice(0, 6).map((t, i) => (
                                <li key={i}>{t}</li>
                              ))}
                            </ul>
                          </div>
                        )}
                      </details>
                    )}
                  </div>
                  );
                })}
              </div>
            </SectionCard>
          )}

          {(profile.dashboard.watchlist || []).length > 0 && (
            <SectionCard theme={theme} title="Watchlist (leading indicators)">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {(profile.dashboard.watchlist || []).slice(0, 12).map((w) => {
                  const sev = inferSignalSeverity(w.signal);
                  const sevColor = severityColor(sev);
                  return (
                  <div
                    key={w.id}
                    className="hzn-print-card hzn-print-surface"
                    style={{
                      padding: '10px 12px',
                      borderRadius: 12,
                      border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                      background: theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.01)',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 10,
                      boxShadow: theme === 'dark' ? '0 16px 44px rgba(0,0,0,0.22)' : '0 16px 44px rgba(0,0,0,0.06)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                        <span
                          style={{
                            width: 8,
                            height: 8,
                            borderRadius: 999,
                            background: sevColor,
                            boxShadow: `0 0 0 4px ${theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(15,23,42,0.03)'}`,
                          }}
                        />
                        <div style={{ fontSize: 12, fontWeight: 950 as any, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {w.indicator}
                        </div>
                      </div>
                      <div style={{ fontSize: 12, fontWeight: 900, color: sevColor }}>
                        {w.signal || '—'}
                      </div>
                    </div>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10, fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                      <div>
                        <span style={{ fontWeight: 900 }}>Now</span>{' '}
                        <span style={{ color: 'var(--text-heading)', fontVariantNumeric: 'tabular-nums' as any }}>{w.current ? formatDisplayValue(w.current) : '—'}</span>
                      </div>
                      <div style={{ opacity: 0.6 }}>→</div>
                      <div>
                        <span style={{ fontWeight: 900 }}>Threshold</span>{' '}
                        <span style={{ color: 'var(--text-heading)', fontVariantNumeric: 'tabular-nums' as any }}>{w.threshold ? formatDisplayValue(w.threshold) : '—'}</span>
                      </div>
                    </div>
                    {(w.sources || []).length > 0 && (
                      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                        {(w.sources || []).slice(0, 4).map((s, i) => (
                          <a
                            key={i}
                            href={s}
                            target="_blank"
                            rel="noopener noreferrer"
                            title={s}
                            style={{
                              fontSize: 11,
                              color: theme === 'dark' ? 'rgba(255,255,255,0.70)' : 'rgba(15,23,42,0.70)',
                              textDecoration: 'none',
                              padding: '4px 10px',
                              borderRadius: 999,
                              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)'}`,
                              background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
                              maxWidth: 260,
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                              whiteSpace: 'nowrap',
                            }}
                          >
                            {sourceLabel(s) || s}
                          </a>
                        ))}
                      </div>
                    )}
                  </div>
                  );
                })}
              </div>
            </SectionCard>
          )}

          {(profile.dashboard.charts ?? []).length > 0 && (
            <SectionCard theme={theme} title="Dashboard charts">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {((profile.dashboard.charts ?? []) as ProfileDataset[]).map((ds) => (
                  <DatasetView key={ds.id} dataset={ds} theme={theme} />
                ))}
              </div>
            </SectionCard>
          )}

          {(profile.dashboard.tables ?? []).length > 0 && (
            <SectionCard theme={theme} title="Dashboard tables">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {((profile.dashboard.tables ?? []) as ProfileDataset[]).map((ds) => (
                  <DatasetView key={ds.id} dataset={ds} theme={theme} />
                ))}
              </div>
            </SectionCard>
          )}

          {profile.dashboard.notes && (
            <SectionCard theme={theme} title="Notes">
              <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.6 }}>
                {profile.dashboard.notes}
              </div>
            </SectionCard>
          )}
        </div>
      )}

      {/* Sections */}
      {sectionKeys.map((sectionKey) => {
        const section = profile.sections[sectionKey];
        if (!section) return null;

        const hasContent = Boolean(section.content?.trim());
        const hasFacts = Array.isArray((section as any).facts) && ((section as any).facts as any[]).length > 0;
        const hasStructured = section.structured && Object.keys(section.structured).length > 0;
        const hasSources = section.sources && section.sources.length > 0;
        const hasDatasets = (section.datasets || []).length > 0;

        if (!hasContent && !hasFacts && !hasStructured && !hasDatasets) return null;

        // Extract exchange rate from section content (once per section, reused for all facts)
        const sectionContent = String(section.content || '').trim();
        const sectionExchangeRate = extractExchangeRate(sectionContent);

        return (
          <div
            key={sectionKey}
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
            }}
          >
            {/* Section header */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
              }}
            >
              <h3
                style={{
                  fontSize: 16,
                  fontWeight: 800,
                  color: 'var(--text-heading)',
                  margin: 0,
                }}
              >
                {section.title || sectionKey}
              </h3>
            </div>

            {/* Facts (typed; preferred over raw JSON) */}
            {(hasFacts || hasStructured) && (
              <div
                style={{
                  padding: '12px 16px',
                  borderRadius: 12,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.12)' : 'rgba(0,0,0,0.015)',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 10,
                }}
              >
                <div style={{ fontSize: 11, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>Key facts</div>

                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
                  {hasFacts
                    ? (((section as any).facts as any[]) || []).slice(0, 10).map((f, idx) => {
                        const rawLabel = String(f?.label || '').trim() || humanizeFactKey(String(f?.key || '')).label || String(f?.key || '').trim();
                        const period = String(f?.period || '').trim();
                        
                        // Extract unit hint from label brackets if unit field is empty
                        // e.g., "Estimated Net Worth [USD]" -> extract "USD"
                        let unitHint = String(f?.unit || '').trim();
                        let label = rawLabel;
                        if (!unitHint) {
                          const bracketMatch = rawLabel.match(/\[([^\]]+)\]\s*$/);
                          if (bracketMatch) {
                            unitHint = bracketMatch[1].trim();
                            label = rawLabel.replace(/\s*\[[^\]]+\]\s*$/, '').trim();
                          }
                        }
                        
                        const value = formatDisplayValue(f?.value, unitHint);
                        if (!label || !value) return null;
                        
                        // Check if this fact has a currency that's not USD, and try to extract exchange rate
                        const unitStr = String(unitHint || '').trim();
                        const nu = normalizeUnit(unitStr);
                        const isNonUSDCurrency = nu.kind === 'currency' && nu.currency && nu.currency !== 'USD';
                        
                        // Try to extract exchange rate from notes or section content (reuse section-level extraction)
                        let exchangeRate: string | null = null;
                        if (isNonUSDCurrency) {
                          const notes = String(f?.notes || '').trim();
                          exchangeRate = extractExchangeRate(notes) || sectionExchangeRate;
                        }
                        
                        return (
                          <div
                            key={`${f?.key || idx}`}
                            title={String(f?.key || '')}
                            style={{
                              padding: '8px 10px',
                              borderRadius: 12,
                              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}`,
                              background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.01)',
                              minWidth: 220,
                              flex: '1 1 220px',
                            }}
                          >
                            <div style={{ fontSize: 11, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                              {label}
                              {period ? ` (${period})` : ''}
                            </div>
                            <div style={{ fontSize: 13, fontWeight: 900, color: 'var(--text-heading)', marginTop: 4 }}>{value}</div>
                            {exchangeRate && (
                              <div style={{ fontSize: 10, color: 'var(--text-secondary)', marginTop: 4, fontStyle: 'italic' }}>
                                {exchangeRate}
                              </div>
                            )}
                            {Array.isArray(f?.sources) && f.sources.length > 0 && (
                              <div style={{ marginTop: 6, display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                                {f.sources.slice(0, 2).map((s: string, i: number) => (
                                  <a
                                    key={i}
                                    href={s}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    style={{
                                      fontSize: 11,
                                      color: theme === 'dark' ? 'rgba(255,255,255,0.70)' : 'rgba(15,23,42,0.70)',
                                      textDecoration: 'none',
                                      maxWidth: 260,
                                      overflow: 'hidden',
                                      textOverflow: 'ellipsis',
                                      whiteSpace: 'nowrap',
                                    }}
                                    title={s}
                                  >
                                    {sourceLabel(s) || s}
                                  </a>
                                ))}
                              </div>
                            )}
                          </div>
                        );
                      })
                    : Object.entries(section.structured || {}).slice(0, 10).map(([k, v]) => {
                        const { label, period, unit } = humanizeFactKey(k);
                        const showLabel = label || k;
                        const suffix = period ? ` (${period})` : '';
                        const unitSuffix = unit ? ` [${unit}]` : '';
                        const value = formatDisplayValue(v, unit);
                        return (
                          <div
                            key={k}
                            title={k}
                            style={{
                              padding: '8px 10px',
                              borderRadius: 12,
                              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}`,
                              background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.01)',
                              minWidth: 220,
                              flex: '1 1 220px',
                            }}
                          >
                            <div style={{ fontSize: 11, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                              {showLabel}
                              {suffix}
                              {unitSuffix}
                            </div>
                            <div style={{ fontSize: 13, fontWeight: 900, color: 'var(--text-heading)', marginTop: 4 }}>{value}</div>
                          </div>
                        );
                      })}
                </div>
              </div>
            )}

            {/* Datasets */}
            {hasDatasets && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {(section.datasets || []).map((ds) => (
                  <DatasetView key={ds.id} dataset={ds as ProfileDataset} theme={theme} />
                ))}
              </div>
            )}

            {/* Content */}
            {hasContent && (
              <div
                style={{
                  fontSize: 13,
                  color: 'var(--text-heading)',
                  lineHeight: 1.6,
                }}
              >
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {section.content}
                </ReactMarkdown>
              </div>
            )}

            {/* Sources */}
            {hasSources && (
              <div
                style={{
                  marginTop: 4,
                  paddingTop: 12,
                  borderTop: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}`,
                }}
              >
                <details>
                  <summary style={{ cursor: 'pointer', fontSize: 11, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                    Sources ({(section.sources ?? []).length})
                  </summary>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 10 }}>
                    {(section.sources ?? []).slice(0, 16).map((source, idx) => (
                      <a
                        key={idx}
                        href={source}
                        target="_blank"
                        rel="noopener noreferrer"
                        title={source}
                        style={{
                          fontSize: 11,
                          color: theme === 'dark' ? 'rgba(255,255,255,0.70)' : 'rgba(15,23,42,0.70)',
                          textDecoration: 'none',
                          padding: '5px 10px',
                          borderRadius: 999,
                          border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)'}`,
                          background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
                          maxWidth: 320,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                      >
                        {sourceLabel(source) || source}
                      </a>
                    ))}
                  </div>
                </details>
              </div>
            )}
          </div>
        );
      })}

      {/* Generation notes if available */}
      {profile.generation && (profile.generation as any).generationNotes && (
        <div
          style={{
            padding: '12px 16px',
            borderRadius: 10,
            border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
            background: theme === 'dark' ? 'rgba(0,0,0,0.15)' : 'rgba(0,0,0,0.02)',
            fontSize: 12,
            color: 'var(--text-secondary-strong)',
            fontStyle: 'italic',
          }}
        >
          {(profile.generation as any).generationNotes}
        </div>
      )}
    </div>
  );
};

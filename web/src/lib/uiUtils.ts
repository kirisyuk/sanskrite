/**
 * UI utility functions for common UI operations
 */

/**
 * Get user's preferred language from localStorage
 * Returns lowercase 2-letter code (default: 'en')
 */
export function getPreferredLanguage(): string {
  if (typeof window === 'undefined') return 'en';
  const preferred = (localStorage.getItem('preferredLanguage') || 'EN').toLowerCase();
  return preferred && preferred.length === 2 ? preferred : 'en';
}

/**
 * Convert language code to full language name for prompts
 */
export function getLanguageName(langCode: string): string {
  const code = langCode.toLowerCase();
  switch (code) {
    case 'ru': return 'Russian';
    case 'en': return 'English';
    case 'kk':
    case 'kz': return 'Kazakh';
    default: return 'English';
  }
}

/**
 * Get anchor rectangle for modals/palettes, with fallback chain
 * Returns rect with top, left, width, height, or null if element not found
 */
export function getAnchorRect(): { top: number; left: number; width: number; height: number } | null {
  try {
    const el = (document.querySelector('#unfold-request-container') || 
                document.querySelector('#app') || 
                document.body) as HTMLElement;
    const rect = el.getBoundingClientRect();
    return { top: rect.top - 8, left: rect.left, width: rect.width, height: rect.height };
  } catch {
    return null;
  }
}

/**
 * Generate SX system instruction (short version)
 * Used for edit operations and simple interactions
 */
export function getSxSystemInstructionShort(): string {
  return `Hidden-instructions protocol (do not show to users):\n- When you need to output service instructions (entities, metrics, watchlist, relations), emit ONLY HTML comments <!-- @sx:{JSON} -->.`;
}

/**
 * Generate SX system instruction (full version)
 * Used for agent interactions with full context
 */
export function getSxSystemInstructionFull(): string {
  return (
    `Hidden-instructions protocol (do not show to users):\n` +
    `- When you need to output service instructions (entities, metrics to monitor, watchlist, relations), emit ONLY HTML comments of the form <!-- @sx:{JSON} -->.\n` +
    `- JSON must be valid and match v: "1" with one of types: entity_mark, monitor_metric, watchlist_add, link_reference.\n` +
    `- Do not duplicate @sx content in visible markdown. Keep normal answer readable.\n` +
    `- Examples: <!-- @sx:{"v":"1","type":"entity_mark","id":"ent-001","kind":"Person","name":"…","description":"…"} -->`
  );
}

/**
 * Humanize a slug-ish identifier for UI display (best-effort).
 * Example: "turki-al-saud" -> "turki al saud"
 */
export function humanizeSlug(slug?: string): string {
  const s = String(slug || '').trim();
  if (!s) return '';
  return s.replace(/[-_]+/g, ' ').replace(/\s+/g, ' ').trim();
}

function titleCaseWords(s: string): string {
  const t = String(s || '').trim();
  if (!t) return '';
  if (/[A-Z]/.test(t)) return t;
  return t
    .split(/\s+/g)
    .filter(Boolean)
    .map((w) => {
      const first = w.charAt(0);
      if (!first || first.toLowerCase() === first.toUpperCase()) return w;
      return first.toUpperCase() + w.slice(1);
    })
    .join(' ')
    .trim();
}

function domainBaseTitle(contractType: string): string {
  const ct = String(contractType || '').trim().toLowerCase();
  switch (ct) {
    // Supply chain / industry
    case 'supply-chain': return 'Supply chain';
    case 'value-chain': return 'Value chain';
    case 'logistics-flow': return 'Logistics flow';
    case 'trade-flow': return 'Trade flow';
    case 'sourcing-map': return 'Sourcing map';
    case 'procurement-map': return 'Procurement map';

    // Risk / geopolitics
    case 'risk-assessment': return 'Risk assessment';
    case 'geopolitical-risk': return 'Geopolitical risk';
    case 'bilateral-relations': return 'Bilateral relations';
    case 'sanctions-exposure': return 'Sanctions exposure';
    case 'conflict-dynamics': return 'Conflict dynamics';

    // Markets / opportunity
    case 'market-opportunity': return 'Market opportunity';
    case 'competitive-landscape': return 'Competitive landscape';
    case 'market-entry': return 'Market entry';
    case 'market-sizing': return 'Market sizing';
    case 'demand-forecast': return 'Demand forecast';
    case 'pricing-dynamics': return 'Pricing dynamics';

    // Investment / finance
    case 'investment-thesis': return 'Investment thesis';
    case 'funding-landscape': return 'Funding landscape';
    case 'exit-strategy': return 'Exit strategy';
    case 'valuation-drivers': return 'Valuation drivers';
    case 'portfolio-exposure': return 'Portfolio exposure';

    // Technology / innovation
    case 'technology-landscape': return 'Technology landscape';
    case 'innovation-map': return 'Innovation map';
    case 'patent-landscape': return 'Patent landscape';
    case 'rd-landscape': return 'R&D landscape';
    case 'adoption-curve': return 'Adoption curve';
    case 'disruption-analysis': return 'Disruption analysis';

    // Ecosystem / stakeholder
    case 'ecosystem-mapping': return 'Ecosystem mapping';
    case 'stakeholder-mapping': return 'Stakeholder mapping';
    case 'partnership-landscape': return 'Partnership landscape';
    case 'influence-network': return 'Influence network';
    case 'power-dynamics': return 'Power dynamics';

    // Policy / regulation
    case 'regulatory-impact': return 'Regulatory impact';
    case 'policy-and-regulation': return 'Policy and regulation';
    case 'compliance-landscape': return 'Compliance landscape';
    case 'security-and-compliance': return 'Security and compliance';
    case 'licensing-requirements': return 'Licensing requirements';
    case 'esg-landscape': return 'ESG landscape';

    // Scenarios / trends
    case 'scenario-analysis': return 'Scenario analysis';
    case 'macro-trends': return 'Macro trends';
    case 'megatrends': return 'Megatrends';
    case 'future-outlook': return 'Future outlook';

    // Sector / industry
    case 'sector-overview': return 'Sector overview';
    case 'industry-structure': return 'Industry structure';
    case 'vertical-deep-dive': return 'Vertical deep-dive';

    // "other" is handled specially in humanizeDomainCanonicalPath
    case 'other': return 'Other';

    default:
      return titleCaseWords(humanizeSlug(ct));
  }
}

function humanizeDomainCanonicalPath(path: string): string {
  const p = String(path || '').trim();
  if (!p.startsWith('/domain/v1/')) return '';
  const parts = p.split('/').filter(Boolean);
  // domain/v1/{contractType}/(k/v)* OR domain/v1/other/{custom-slug}/(k/v)*
  if (parts.length < 3) return '';
  const contractType = String(parts[2] || '').trim().toLowerCase();

  let base: string;
  let facetStartIdx: number;

  // Handle "other" contract type specially: use custom slug as base title.
  if (contractType === 'other') {
    if (parts.length < 4) return 'Other';
    const customSlug = String(parts[3] || '').trim();
    base = titleCaseWords(humanizeSlug(customSlug)) || 'Other';
    facetStartIdx = 4; // facets start after custom slug
  } else {
    base = domainBaseTitle(contractType) || 'Domain';
    facetStartIdx = 3; // facets start after contractType
  }

  const rest = parts.slice(facetStartIdx);
  if (rest.length < 2 || rest.length % 2 !== 0) return base;

  const geos: string[] = [];
  for (let i = 0; i + 1 < rest.length; i += 2) {
    const k = String(rest[i] || '').trim().toLowerCase();
    const v = String(rest[i + 1] || '').trim().toLowerCase();
    if (!k || !v) continue;
    if (k !== 'geo') continue;
    const toks = v.includes('-') ? v.split('-') : [v];
    for (const raw of toks) {
      const cc = String(raw || '').trim().toLowerCase();
      if (cc.length === 2) geos.push(cc);
    }
  }

  const uniq = Array.from(new Set(geos)).sort();
  if (uniq.length === 0) return base;
  const label = uniq.map((cc) => cc.toUpperCase()).join(' · ');
  return `${base} — ${label}`;
}

/** Short real names for geo/region codes (ISO2 + regional buckets). Used for domain path display. */
const GEO_CODE_TO_SHORT_NAME: Record<string, string> = {
  // ISO2 countries (common)
  br: 'Brazil', us: 'USA', gb: 'UK', de: 'Germany', fr: 'France', cn: 'China', jp: 'Japan', in: 'India',
  ru: 'Russia', kz: 'Kazakhstan', sa: 'Saudi Arabia', ae: 'UAE', eg: 'Egypt', tr: 'Turkey', my: 'Malaysia',
  id: 'Indonesia', sg: 'Singapore', kr: 'South Korea', tw: 'Taiwan', au: 'Australia', nz: 'New Zealand',
  ca: 'Canada', mx: 'Mexico', ar: 'Argentina', cl: 'Chile', co: 'Colombia', pe: 'Peru', za: 'South Africa',
  ng: 'Nigeria', ke: 'Kenya', pl: 'Poland', nl: 'Netherlands', it: 'Italy', es: 'Spain', pt: 'Portugal',
  se: 'Sweden', no: 'Norway', fi: 'Finland', dk: 'Denmark', ch: 'Switzerland', at: 'Austria', be: 'Belgium',
  ie: 'Ireland', il: 'Israel', qa: 'Qatar', kw: 'Kuwait', bh: 'Bahrain', om: 'Oman', pk: 'Pakistan',
  th: 'Thailand', vn: 'Vietnam', ph: 'Philippines', hk: 'Hong Kong', ua: 'Ukraine', cz: 'Czechia',
  ro: 'Romania', hu: 'Hungary', gr: 'Greece',
  // Regional buckets (from backend)
  mena: 'MENA', emea: 'EMEA', apac: 'APAC', latam: 'Latam', na: 'North America', gcc: 'GCC',
  eu: 'EU', asean: 'ASEAN', nordics: 'Nordics', benelux: 'Benelux', anz: 'ANZ', cis: 'CIS',
  ssa: 'Sub-Saharan Africa', usmca: 'USMCA', nafta: 'NAFTA', global: 'Global',
};

/** Returns short real name for a geo code (ISO2 or regional). */
export function geoCodeToShortName(code: string): string {
  const c = String(code || '').trim().toLowerCase();
  if (!c) return '';
  return GEO_CODE_TO_SHORT_NAME[c] || titleCaseWords(c) || c.toUpperCase();
}

/** Extracts geo codes from domain path and returns first region label for display. */
export function getDomainRegionLabel(path: string): string | null {
  const codes = extractDomainGeoCodes(path);
  if (codes.length === 0) return null;
  const first = codes[0];
  const name = geoCodeToShortName(first);
  return name || null;
}

/**
 * Extracts geo/country from scope fragment path (org, market, loc, gov, community, segment)
 * and returns short real name for display chip. Skips "na" (not applicable/unknown).
 */
export function getScopeGeoLabel(path: string): string | null {
  const p = String(path || '').trim();
  if (!p.startsWith('/')) return null;
  const parts = p.split('/').filter(Boolean);
  if (parts.length < 2) return null;

  const ns = String(parts[0] || '').toLowerCase();
  let geoCode = '';

  switch (ns) {
    case 'org':
    case 'market':
    case 'loc':
    case 'gov':
    case 'community':
      geoCode = String(parts[1] || '').trim().toLowerCase();
      break;
    case 'segment':
      if (parts.length >= 4) geoCode = String(parts[3] || '').trim().toLowerCase();
      break;
    default:
      return null;
  }

  if (!geoCode || geoCode === 'na') return null;

  const name = geoCodeToShortName(geoCode);
  if (!name) return null;

  const composite = geoCode.includes('-');
  if (composite) {
    const first = geoCode.split('-')[0];
    if (first === 'na') return null;
    return geoCodeToShortName(first) || name;
  }

  return name;
}

function extractDomainGeoCodes(path: string): string[] {
  const p = String(path || '').trim();
  if (!p.startsWith('/domain/')) return [];
  const parts = p.split('/').filter(Boolean);
  if (parts.length < 3) return [];
  // Support both /domain/v1/{contract}/... and /domain/{contract}/...
  const hasV1 = parts[1] === 'v1';
  const contractIdx = hasV1 ? 2 : 1;
  const contractType = String(parts[contractIdx] || '').trim().toLowerCase();
  const facetStartIdx = contractType === 'other' ? (hasV1 ? 4 : 3) : (hasV1 ? 3 : 2);
  const rest = parts.slice(facetStartIdx);
  if (rest.length < 2 || rest.length % 2 !== 0) return [];
  const geos: string[] = [];
  for (let i = 0; i + 1 < rest.length; i += 2) {
    const k = String(rest[i] || '').trim().toLowerCase();
    const v = String(rest[i + 1] || '').trim().toLowerCase();
    if (k !== 'geo' || !v) continue;
    const toks = v.includes('-') ? v.split('-') : [v];
    for (const raw of toks) {
      const cc = String(raw || '').trim().toLowerCase();
      if (cc.length === 2) geos.push(cc);
    }
  }
  return Array.from(new Set(geos)).sort();
}

/**
 * Prefer a provided label, but for DOMAIN nodes ignore low-quality labels like raw geo codes ("Sa")
 * and fall back to deterministic domain humanization.
 */
export function preferDisplayLabel(label?: string, canonicalPath?: string, type?: string): string {
  const lbl = String(label || '').trim();
  const cp = String(canonicalPath || '').trim();
  const t = String(type || '').trim().toUpperCase();
  const isDomain = t === 'DOMAIN' || cp.startsWith('/domain/');
  if (!isDomain) return lbl || '';
  if (!lbl) return '';
  if (cp.startsWith('/domain/v1/')) {
    const geos = extractDomainGeoCodes(cp);
    const l = lbl.toLowerCase();
    // If label is just a geo code present in the domain facets, it's not a meaningful title.
    if (l.length === 2 && geos.includes(l)) return '';
  }
  return lbl;
}

/**
 * Normalize canonical path for consistent matching (trim, remove trailing slash).
 * Matches backend newsByFragments key format.
 */
export function normalizeCanonicalPath(path?: string): string {
  let p = String(path || '').trim();
  while (p.length > 1 && p.endsWith('/')) {
    p = p.slice(0, -1);
  }
  return p;
}

/**
 * Humanize a canonicalPath for UI display by taking the last segment.
 * Example: "/org/turki-al-saud" -> "turki al saud"
 */
export function humanizeCanonicalPath(path?: string): string {
  const p = String(path || '').trim();
  if (!p) return '';
  if (p.startsWith('/domain/v1/')) {
    const d = humanizeDomainCanonicalPath(p);
    if (d) return d;
  }
  const seg = p.split('/').filter(Boolean).pop() || '';
  const human = humanizeSlug(seg);
  const s = (human || seg).trim();
  if (!s) return '';
  // Best-effort Title Case when string looks slug-ish (no uppercase letters).
  return titleCaseWords(s) || s;
}


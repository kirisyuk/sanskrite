export type ScoutingMode = 'general' | 'supply_chain' | 'governance';

type ScoutingIntentLike = {
  anchors?: Array<{ canonicalPath?: string }>;
  domains?: Array<{ canonicalPath?: string; firstOrderEntities?: string[] }>;
};

export function defaultScoutingModeForCanonicalPath(canonicalPath?: string): ScoutingMode {
  const path = String(canonicalPath || '').trim().toLowerCase();
  if (path.startsWith('/org/') || path.startsWith('/gov/')) {
    return 'governance';
  }
  return 'general';
}

export function defaultScoutingModeForIntent(intent?: ScoutingIntentLike | null): ScoutingMode {
  if (!intent) return 'general';

  const paths: string[] = [];
  for (const anchor of intent.anchors || []) {
    const canonicalPath = String(anchor?.canonicalPath || '').trim();
    if (canonicalPath) paths.push(canonicalPath);
  }
  for (const domain of intent.domains || []) {
    const canonicalPath = String(domain?.canonicalPath || '').trim();
    if (canonicalPath) paths.push(canonicalPath);
    for (const entityPath of domain?.firstOrderEntities || []) {
      const normalized = String(entityPath || '').trim();
      if (normalized) paths.push(normalized);
    }
  }

  if (paths.length > 0 && paths.every((path) => defaultScoutingModeForCanonicalPath(path) === 'governance')) {
    return 'governance';
  }
  return 'general';
}

export function recommendedDepthForScoutingMode(mode: ScoutingMode): number {
  return mode === 'supply_chain' ? 2 : 1;
}

export function specialtiesForScoutingMode(mode: ScoutingMode): string[] {
  switch (mode) {
    case 'supply_chain':
      return ['supply_chain', 'products_and_services'];
    case 'governance':
      return ['management', 'equity'];
    default:
      return [];
  }
}

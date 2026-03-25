export type MapRenderTheme = 'dark' | 'light';

export type MapLayerTone = 'direct' | 'context' | 'supply' | 'propagation' | 'simulation_stress';

const MAP_LAYER_PALETTE: Record<MapLayerTone, { dark: string; light: string }> = {
  direct: {
    dark: '#60a5fa',
    light: '#2563eb',
  },
  context: {
    dark: '#2dd4bf',
    light: '#0d9488',
  },
  supply: {
    dark: '#fb923c',
    light: '#c2410c',
  },
  propagation: {
    dark: '#94a3b8',
    light: '#64748b',
  },
  simulation_stress: {
    dark: '#fca5a5',
    light: '#e88b94',
  },
};

export function mapLayerColor(tone: MapLayerTone, theme: MapRenderTheme): string {
  return MAP_LAYER_PALETTE[tone][theme];
}

export function simulationStressColor(theme: MapRenderTheme): string {
  return mapLayerColor('simulation_stress', theme);
}

export function mapFamilyAccentColor(family: string, theme: MapRenderTheme): string {
  switch (String(family || '').trim().toLowerCase()) {
    case 'supply':
      return mapLayerColor('supply', theme);
    case 'control':
      return mapLayerColor('direct', theme);
    case 'location':
    case 'market':
      return mapLayerColor('context', theme);
    case 'social':
      return theme === 'dark' ? '#c4b5fd' : '#7c3aed';
    case 'risk':
    case 'influence':
      return theme === 'dark' ? '#fb7185' : '#be123c';
    default:
      return mapLayerColor('propagation', theme);
  }
}

import type { ExpressionSpecification } from 'maplibre-gl';
import { MAP_TEXT_FONT_STACK } from './mapTypography';

export type MapCaptionTheme = 'dark' | 'light';

/** MapLibre `text-font` for all horizon map captions. */
export function mapCaptionTextFont(): string[] {
  return [...MAP_TEXT_FONT_STACK];
}

/**
 * Canonical zoom curve for **node / object names** (radial labels beside an icon).
 * Shared by geo markers and path-leg endpoint object labels.
 */
export const MAP_NODE_CAPTION_TEXT_SIZE: ExpressionSpecification = [
  'interpolate',
  ['linear'],
  ['zoom'],
  4,
  11,
  6,
  11.5,
  8,
  12,
  10,
  12.5,
];

export function mapNodeCaptionTextPaint(
  theme: MapCaptionTheme,
  options?: { variant?: 'default' | 'selected'; haloWidth?: number },
): Record<string, string | number> {
  const variant = options?.variant ?? 'default';
  if (variant === 'selected') {
    return {
      'text-color': theme === 'dark' ? '#ffffff' : 'rgba(2,6,23,0.98)',
      'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
      'text-halo-width': options?.haloWidth ?? 1.6,
    };
  }
  return {
    'text-color': theme === 'dark' ? '#f8fafc' : 'rgba(2,6,23,0.96)',
    'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
    'text-halo-width': options?.haloWidth ?? 1.55,
  };
}

/** Geo markers — collisions respected (labels can hide). */
export function mapMarkerRadialNodeCaptionLayout(): Record<string, unknown> {
  return {
    'text-font': mapCaptionTextFont(),
    'text-size': MAP_NODE_CAPTION_TEXT_SIZE,
    'text-variable-anchor': ['literal', ['right', 'left', 'top-right', 'bottom-right', 'top', 'bottom']],
    'text-radial-offset': 1.3,
    'text-max-width': 10,
    'text-allow-overlap': false,
    'text-ignore-placement': false,
  };
}

/** Path-leg object pins — same typography, top-weighted anchors, no collision hiding. */
export function mapPathLegEndpointNodeCaptionLayout(): Record<string, unknown> {
  return {
    'text-font': mapCaptionTextFont(),
    'text-size': MAP_NODE_CAPTION_TEXT_SIZE,
    'text-variable-anchor': ['literal', ['top', 'top-right', 'top-left', 'right', 'left']],
    'text-radial-offset': 1.6,
    'text-allow-overlap': true,
    'text-ignore-placement': true,
  };
}

/** Selected-object highlight ring — fixed size, overlaps allowed. */
export function mapHighlightSelectedNodeCaptionLayout(): Record<string, unknown> {
  return {
    'text-font': mapCaptionTextFont(),
    'text-size': 12.5,
    'text-variable-anchor': ['literal', ['right', 'left', 'top-right', 'bottom-right', 'top', 'bottom']],
    'text-radial-offset': 1.3,
    'text-max-width': 10,
    'text-allow-overlap': true,
    'text-ignore-placement': true,
  };
}

/** Mid-line supply path chips (tighter). */
export const MAP_CONTEXT_CHIP_FIT_PADDING_COMPACT: [number, number, number, number] = [4, 7, 4, 7];

/** Path-leg meaning chips and trace endpoint chips. */
export const MAP_CONTEXT_CHIP_FIT_PADDING_STANDARD: [number, number, number, number] = [4, 8, 4, 8];

/** Text size inside supply-path chips (centered on line). */
export const MAP_SUPPLY_PATH_CHIP_TEXT_SIZE: ExpressionSpecification = [
  'interpolate',
  ['linear'],
  ['zoom'],
  4,
  10,
  6,
  10.8,
  8,
  11.4,
];

/** Path-leg “meaning” row — slightly smaller at low zoom. */
export const MAP_PATH_LEG_MEANING_CHIP_TEXT_SIZE: ExpressionSpecification = [
  'interpolate',
  ['linear'],
  ['zoom'],
  2,
  11,
  3,
  10.8,
  4,
  10,
  6,
  10.5,
  8,
  11,
];

/**
 * Artifact-related types and utilities
 * These extend the auto-generated CoreDoc type with artifact-specific helpers
 */

import type { CoreDoc } from './generated';

// Type guards
export function isArtifact(doc: CoreDoc): boolean {
  return doc.docType === 'dynamic_artifact' && doc.artifactConfig !== undefined;
}

export function isStaticDoc(doc: CoreDoc): boolean {
  return !doc.docType || doc.docType === 'static';
}

// Artifact-specific types for convenience
export type Artifact = CoreDoc & {
  docType: 'dynamic_artifact';
  artifactConfig: NonNullable<CoreDoc['artifactConfig']>;
};

export type StaticDoc = CoreDoc & {
  docType?: 'static';
  artifactConfig?: never;
};

// Format and Focus display names
export const ArtifactFormatLabels: Record<string, string> = {
  text: 'Text Document',
  table: 'Data Table',
  report: 'Report',
  presentation: 'Presentation',
};

export const ArtifactFocusLabels: Record<string, string> = {
  brief: 'Brief Summary',
  analysis: 'In-depth Analysis',
  narrative: 'Narrative',
  proposal: 'Proposal',
  technical: 'Technical Documentation',
  executive: 'Executive Summary',
};

// Version comparison utilities
export interface VersionComparison {
  versionA: string;
  versionB: string;
  documentDiff: {
    added: string[];
    removed: string[];
    modified: string[];
  };
  sectionDiffs: Record<string, {
    contentChanged: boolean;
    promptChanged: boolean;
    modelChanged: boolean;
    qualityDelta: number;
  }>;
}

// Quality helpers
export function getQualityColor(quality: number): string {
  if (quality >= 0.8) return 'hsl(120, 60%, 50%)'; // green
  if (quality >= 0.6) return 'hsl(45, 80%, 55%)'; // yellow
  return 'hsl(0, 70%, 55%)'; // red
}

export function getQualityLabel(quality: number): string {
  if (quality >= 0.9) return 'Excellent';
  if (quality >= 0.8) return 'Good';
  if (quality >= 0.7) return 'Fair';
  if (quality >= 0.6) return 'Moderate';
  return 'Needs Improvement';
}

// Section quality calculation
export function calculateSectionQuality(
  coherence: number,
  completeness: number,
  relevance: number
): number {
  // Weighted average: relevance is most important
  return (coherence * 0.3 + completeness * 0.3 + relevance * 0.4);
}

// Context change detection (for auto-update triggers)
export interface ContextChangeMetrics {
  newMessagesCount: number;
  newDocumentsCount: number;
  contextChangePercent: number;
  keywordsMatched: string[];
  searchResultsChanged: boolean;
}

export function shouldTriggerAutoUpdate(
  metrics: ContextChangeMetrics,
  config: NonNullable<NonNullable<CoreDoc['artifactConfig']>['autoUpdateConfig']>
): boolean {
  if (!config.conditions) return true;

  const conditions = config.conditions;

  // Check minimum new messages
  if (conditions.minNewMessages && metrics.newMessagesCount < conditions.minNewMessages) {
    return false;
  }

  // Check minimum context change
  if (conditions.minContextChange && metrics.contextChangePercent < conditions.minContextChange) {
    return false;
  }

  // Check keywords
  if (conditions.keywords && conditions.keywords.length > 0) {
    if (metrics.keywordsMatched.length === 0) {
      return false;
    }
  }

  // Check search query changed
  if (conditions.searchQueryChanged && !metrics.searchResultsChanged) {
    return false;
  }

  return true;
}

// Version retention helpers
export function shouldRetainVersion(
  version: NonNullable<CoreDoc['artifactConfig']>['versions'][0],
  allVersions: NonNullable<CoreDoc['artifactConfig']>['versions'],
  retentionCount: number
): boolean {
  // Always keep the first version
  if (version.id === allVersions[allVersions.length - 1]?.id) {
    return true;
  }

  // Always keep the last N versions
  const recentVersions = allVersions.slice(0, retentionCount);
  if (recentVersions.some(v => v.id === version.id)) {
    return true;
  }

  // For older versions, keep one per day/week (compression logic)
  // This would be implemented based on timestamp analysis
  return false;
}


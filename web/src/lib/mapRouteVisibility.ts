export interface MapRouteVisibilitySelection {
  artifactIds?: Iterable<string>;
  productRefs?: Iterable<string>;
}

type RouteArtifactLike = {
  id?: string;
  productAxis?: string;
  payload?: {
    legs?: any[];
  };
};

type NormalizedMapRouteVisibilitySelection = {
  artifactIds: Set<string>;
  productRefs: Set<string>;
  active: boolean;
};

function normalizeStringSet(values?: Iterable<string>): Set<string> {
  const out = new Set<string>();
  if (!values) return out;
  for (const value of values) {
    const trimmed = String(value || '').trim();
    if (trimmed) out.add(trimmed);
  }
  return out;
}

function normalizeRouteVisibility(
  selection?: MapRouteVisibilitySelection | null,
): NormalizedMapRouteVisibilitySelection {
  const artifactIds = normalizeStringSet(selection?.artifactIds);
  const productRefs = normalizeStringSet(selection?.productRefs);
  return {
    artifactIds,
    productRefs,
    active: artifactIds.size > 0 || productRefs.size > 0,
  };
}

function artifactIdMatches(
  artifact: RouteArtifactLike | null | undefined,
  selection: NormalizedMapRouteVisibilitySelection,
): boolean {
  if (selection.artifactIds.size === 0) return true;
  const artifactId = String(artifact?.id || '').trim();
  return Boolean(artifactId) && selection.artifactIds.has(artifactId);
}

function productRefMatches(
  productRef: string,
  selection: NormalizedMapRouteVisibilitySelection,
): boolean {
  if (selection.productRefs.size === 0) return true;
  return Boolean(productRef) && selection.productRefs.has(productRef);
}

export function legMatchesRouteVisibility(
  artifact: RouteArtifactLike | null | undefined,
  leg: any,
  selection?: MapRouteVisibilitySelection | null,
): boolean {
  const normalized = normalizeRouteVisibility(selection);
  if (!normalized.active) return true;
  if (!artifactIdMatches(artifact, normalized)) return false;
  const legProductRef = String(leg?.productRef || '').trim();
  const artifactProductRef = String(artifact?.productAxis || '').trim();
  if (normalized.productRefs.size === 0) return true;
  return productRefMatches(legProductRef, normalized) || (!legProductRef && productRefMatches(artifactProductRef, normalized));
}

export function artifactMatchesRouteVisibility(
  artifact: RouteArtifactLike | null | undefined,
  selection?: MapRouteVisibilitySelection | null,
): boolean {
  const normalized = normalizeRouteVisibility(selection);
  if (!normalized.active) return true;
  if (!artifactIdMatches(artifact, normalized)) return false;
  if (normalized.productRefs.size === 0) return true;
  const artifactProductRef = String(artifact?.productAxis || '').trim();
  if (productRefMatches(artifactProductRef, normalized)) return true;
  const legs = Array.isArray(artifact?.payload?.legs) ? artifact!.payload!.legs! : [];
  return legs.some((leg) => legMatchesRouteVisibility(artifact, leg, normalized));
}

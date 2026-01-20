export const isSphereCanonicalPath = (p?: string | null): boolean => {
  const cp = String(p || '').trim().toLowerCase();
  return cp.startsWith('/sphere/');
};

export const isSphereType = (t?: string | null): boolean => {
  return String(t || '').trim().toUpperCase() === 'SPHERE';
};

export const isSphereEntity = (
  type?: string | null,
  canonicalPath?: string | null,
): boolean => isSphereType(type) || isSphereCanonicalPath(canonicalPath);

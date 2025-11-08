import { api } from './api';

interface TimingCacheEntry {
  estimatedSeconds: number | null;
  cachedAt: number; // timestamp in milliseconds
}

// Cache for 30 minutes
const CACHE_TTL_MS = 30 * 60 * 1000;

// Maximum cache size to prevent memory leaks (keep last 100 entries)
const MAX_CACHE_SIZE = 100;

// In-memory cache: key is "provider:model"
const cache = new Map<string, TimingCacheEntry>();

/**
 * Get average timing estimate for a model (without token count)
 * Uses 30-minute cache to avoid excessive API calls
 */
export async function getAverageTiming(
  provider: string,
  model: string
): Promise<number | null> {
  const cacheKey = `${provider}:${model}`;
  const cached = cache.get(cacheKey);

  // Check if cache is valid
  if (cached && Date.now() - cached.cachedAt < CACHE_TTL_MS) {
    return cached.estimatedSeconds;
  }

  // Fetch from API
  try {
    const result = await api.getAverageTiming({
      provider,
      model,
    });

    // Cache the result
    cache.set(cacheKey, {
      estimatedSeconds: result.estimatedSeconds,
      cachedAt: Date.now(),
    });

    // Enforce maximum cache size to prevent memory leaks
    if (cache.size > MAX_CACHE_SIZE) {
      // Remove oldest entries (by cachedAt timestamp)
      const entries = Array.from(cache.entries());
      entries.sort((a, b) => a[1].cachedAt - b[1].cachedAt);
      const toRemove = entries.slice(0, cache.size - MAX_CACHE_SIZE);
      for (const [key] of toRemove) {
        cache.delete(key);
      }
    }

    return result.estimatedSeconds;
  } catch (error) {
    // Return cached value if available, even if expired
    if (cached) {
      return cached.estimatedSeconds;
    }
    return null;
  }
}

/**
 * Clear the cache (useful for testing or forced refresh)
 */
export function clearTimingCache(): void {
  cache.clear();
}


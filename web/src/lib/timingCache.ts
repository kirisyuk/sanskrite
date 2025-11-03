import { api } from './api';

interface TimingCacheEntry {
  estimatedSeconds: number | null;
  cachedAt: number; // timestamp in milliseconds
}

// Cache for 30 minutes
const CACHE_TTL_MS = 30 * 60 * 1000;

// In-memory cache: key is "provider:model:estimatedTokens"
const cache = new Map<string, TimingCacheEntry>();

/**
 * Get average timing estimate for a model and token count
 * Uses 30-minute cache to avoid excessive API calls
 */
export async function getAverageTiming(
  provider: string,
  model: string,
  estimatedOutputTokens: number
): Promise<number | null> {
  const cacheKey = `${provider}:${model}:${estimatedOutputTokens}`;
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
      estimatedOutputTokens,
    });

    // Cache the result
    cache.set(cacheKey, {
      estimatedSeconds: result.estimatedSeconds,
      cachedAt: Date.now(),
    });

    return result.estimatedSeconds;
  } catch (error) {
    console.error('[TimingCache] Failed to fetch average timing:', error);
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


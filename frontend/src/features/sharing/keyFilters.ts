import type { MarketplaceModel } from './types';

/** Remove blank/duplicate entries and catalog routes covered by a model or scope block. */
export function normalizeKeyBlockedModels(
  values: readonly string[],
  routes: readonly Pick<MarketplaceModel, 'id' | 'base_model' | 'channel'>[] = []
): string[] {
  const cleaned: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const model = value.trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    cleaned.push(model);
  }

  const covered = new Set(
    routes
      .filter((route) => seen.has(route.base_model) || seen.has(route.channel))
      .map((route) => route.id)
  );
  return cleaned.filter((model) => !covered.has(model));
}

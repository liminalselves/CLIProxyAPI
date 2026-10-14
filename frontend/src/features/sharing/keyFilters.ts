/** Remove blank/duplicate entries and pinned routes covered by a bare model or scope block. */
export function normalizeKeyBlockedModels(values: readonly string[]): string[] {
  const cleaned: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const model = value.trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    cleaned.push(model);
  }

  const bareScopes = new Set(cleaned.filter((model) => !model.includes('/')));
  return cleaned.filter((model) => {
    const separator = model.lastIndexOf('/');
    if (separator < 0) return true;
    const prefix = model.slice(0, separator);
    const base = model.slice(separator + 1);
    // A bare model block covers every pinned route of that model, and a
    // credential/channel scope covers every route under that prefix.
    return !bareScopes.has(base) && !bareScopes.has(prefix);
  });
}

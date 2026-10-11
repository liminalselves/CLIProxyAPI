/** Remove blank/duplicate entries and redundant pinned routes covered by a bare model block. */
export function normalizeKeyBlockedModels(values: readonly string[]): string[] {
  const cleaned: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const model = value.trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    cleaned.push(model);
  }

  const bareModels = new Set(cleaned.filter((model) => !model.includes('/')));
  return cleaned.filter((model) => {
    const separator = model.lastIndexOf('/');
    return separator < 0 || !bareModels.has(model.slice(separator + 1));
  });
}

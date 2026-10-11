import { normalizeAutocompleteText } from '@/components/ui/autocompleteMatching';

type UsageScope = 'mine' | 'contributed' | 'site';

// Model candidates must not depend on the applied model filter itself;
// otherwise applying one collapses the dropdown to just that model.
export function buildModelCandidateQuery(
  queryString: string,
  scope: UsageScope,
  me: string
): string {
  const values = Object.fromEntries(new URLSearchParams(queryString));
  delete values.scope;
  delete values.model;
  delete values.page;
  delete values.page_size;
  if (scope === 'mine') values.consumer = me;
  if (scope === 'contributed') values.owner = me;
  return new URLSearchParams(values).toString();
}

export interface UsageKeyOption {
  label: string;
  keyId?: string;
  consumer?: string;
}

export function resolveUsageModel(text: string, models: string[], appliedModel = ''): string | null {
  const normalized = normalizeAutocompleteText(text);
  if (!normalized) return '';
  return (
    models.find((model) => normalizeAutocompleteText(model) === normalized) ??
    (appliedModel && normalizeAutocompleteText(appliedModel) === normalized ? appliedModel : null)
  );
}

export function resolveUsageKey(text: string, options: UsageKeyOption[]): UsageKeyOption | null {
  const normalized = normalizeAutocompleteText(text);
  if (!normalized) return { label: '' };
  return options.find((option) => normalizeAutocompleteText(option.label) === normalized) ?? null;
}

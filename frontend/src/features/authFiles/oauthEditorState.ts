import { normalizeProviderKey } from './constants';

type ModelAliasDraftEntry = {
  id?: string;
  name?: string;
  alias?: string;
};

export const getStringSetSignature = (values: Iterable<string>): string =>
  JSON.stringify(
    Array.from(new Set(values), (value) => value.trim())
      .filter(Boolean)
      .sort()
  );

export const getModelAliasDraftSignature = (entries: ModelAliasDraftEntry[]): string =>
  JSON.stringify(
    entries
      .map((entry) => ({
        name: entry.name ?? '',
        alias: entry.alias ?? '',
      }))
      .filter((entry) => entry.name !== '' || entry.alias !== '')
  );

export const isOAuthEditorDirty = (
  initialProvider: string,
  currentProvider: string,
  baselineContentSignature: string,
  currentContentSignature: string
): boolean =>
  normalizeProviderKey(initialProvider) !== normalizeProviderKey(currentProvider) ||
  baselineContentSignature !== currentContentSignature;

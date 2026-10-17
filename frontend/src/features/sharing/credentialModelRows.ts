/**
 * Row conversion between the credential model management wire contract and the
 * shared provider-style model editor. Omitted editor fields stay omitted on the
 * wire, so the backend keeps inheriting them from the source catalog.
 */
import { buildModelOptions, readModelOptions, validateModelOptions } from '@/features/providers/modelOptions';
import type { ModelEntryInput } from '@/features/providers/types';
import type {
  CredentialAvailableModel,
  CredentialModelCapabilities,
  CredentialModelEntry,
} from '@/services/api/credentialModelManagement';

const formatThinking = (thinking?: Record<string, unknown>): string =>
  thinking === undefined ? '' : JSON.stringify(thinking, null, 2);

export function credentialModelsToRows(entries: CredentialModelEntry[]): ModelEntryInput[] {
  return entries.map((entry) => ({
    name: entry.name,
    alias: entry.alias ?? '',
    thinkingJson: formatThinking(entry.thinking),
    ...readModelOptions({
      name: entry.name,
      maxContextLength: entry['max-context-length'],
      isCompat: entry['is-compat'],
      supportConfigurationUpdate: entry['support-configuration-update'],
      inputModalities: entry['input-modalities'],
      outputModalities: entry['output-modalities'],
      thinking: entry.thinking,
    }),
  }));
}

export function rowsToCredentialModels(
  rows: ModelEntryInput[],
  capabilities: CredentialModelCapabilities
): CredentialModelEntry[] {
  return rows
    .filter((row) => row.name.trim())
    .map((row) => {
      const options = buildModelOptions(row);
      const entry: CredentialModelEntry = { name: row.name.trim() };
      const alias = row.alias?.trim();
      if (alias) entry.alias = alias;
      if (capabilities.max_context_length && options.maxContextLength !== undefined) {
        entry['max-context-length'] = options.maxContextLength;
      }
      if (capabilities.is_compat && options.isCompat !== undefined) {
        entry['is-compat'] = options.isCompat;
      }
      if (
        capabilities.support_configuration_update &&
        options.supportConfigurationUpdate !== undefined
      ) {
        entry['support-configuration-update'] = options.supportConfigurationUpdate;
      }
      if (capabilities.modalities) {
        if (options.inputModalities !== undefined) entry['input-modalities'] = options.inputModalities;
        if (options.outputModalities !== undefined) {
          entry['output-modalities'] = options.outputModalities;
        }
      }
      if (capabilities.thinking && options.thinking !== undefined) {
        entry.thinking = options.thinking;
      }
      return entry;
    });
}

export type CredentialModelRowsError =
  | { key: string }
  | { key: 'sharing.credential_models_duplicate'; name: string }
  | { key: 'sharing.credential_models_not_available'; name: string };

export function validateCredentialModelRows(
  rows: ModelEntryInput[],
  capabilities: CredentialModelCapabilities,
  available: CredentialAvailableModel[]
): CredentialModelRowsError | null {
  const editorError = validateModelOptions(rows);
  if (editorError) return { key: editorError };
  const seen = new Set<string>();
  const availableIds = new Set(available.map((model) => model.id.toLowerCase()));
  for (const row of rows) {
    const name = row.name.trim();
    if (!name) continue;
    const publicName = (row.alias?.trim() || name).toLowerCase();
    if (seen.has(publicName)) {
      return { key: 'sharing.credential_models_duplicate', name: row.alias?.trim() || name };
    }
    seen.add(publicName);
    if (!capabilities.manual_models && available.length > 0 && !availableIds.has(name.toLowerCase())) {
      return { key: 'sharing.credential_models_not_available', name };
    }
  }
  return null;
}

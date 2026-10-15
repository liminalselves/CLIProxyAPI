import {
  buildCredentialAliases,
  readCredentialAliases,
  type CredentialAliasDraft,
} from './authFilePolicyAdapter';
import type { SharedCredential } from './sharingCredentials';

export interface SharedCredentialModelDraft extends CredentialAliasDraft {
  inherited?: { name: string; alias: string };
}

export function readSharedCredentialModels(
  file: SharedCredential
): SharedCredentialModelDraft[] | null {
  const aliases = readCredentialAliases(file.model_aliases);
  if (aliases === null) return null;
  const rows: SharedCredentialModelDraft[] = [...aliases];
  const mappedNames = new Set(aliases.map((row) => row.name.toLowerCase()));
  const seen = new Set<string>();
  for (const model of file.model_definitions ?? []) {
    if (mappedNames.has(model.name.toLowerCase())) continue;
    const prefix = `${file.channel}/`;
    const id = model.id.startsWith(prefix) ? model.id.slice(prefix.length) : model.id;
    const key = `${model.name}\0${id}`;
    if (seen.has(key)) continue;
    seen.add(key);
    const alias = id === model.name ? '' : id;
    rows.push({
      name: model.name,
      alias,
      ...(alias ? { inherited: { name: model.name, alias } } : {}),
    });
  }
  return rows;
}

export function buildSharedCredentialModelAliases(rows: SharedCredentialModelDraft[]) {
  const aliases = rows
    .map((row) => ({ ...row, name: row.name.trim(), alias: row.alias.trim() }))
    .filter(
      (row) =>
        row.alias &&
        row.alias !== row.name &&
        !(row.inherited?.name === row.name && row.inherited.alias === row.alias)
    );
  const seen = new Set<string>();
  for (const row of aliases) {
    const key = row.alias.toLowerCase();
    if (!row.name || seen.has(key)) throw new Error('auth_files.policy_aliases_invalid');
    seen.add(key);
  }
  return buildCredentialAliases(aliases);
}

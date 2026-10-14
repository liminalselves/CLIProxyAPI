import { describe, expect, test } from 'bun:test';
import {
  buildSharedCredentialModelAliases,
  readSharedCredentialModels,
} from '../src/services/api/sharedCredentialModels';
import type { SharedCredential } from '../src/services/api/sharingCredentials';

const file: SharedCredential = {
  id: 'codex.json',
  route_id: 'route',
  name: 'codex.json',
  owner: 'alice',
  channel_id: 'channel',
  channel: '573-codex#1',
  channel_slug: '573',
  published: true,
  manageable: true,
  claimable: false,
  models: ['573-codex#1/gpt-5'],
  model_definitions: [{ id: '573-codex#1/gpt-5', name: 'gpt-5' }],
};

describe('subscription credential model editor', () => {
  test('loads upstream names without routing prefixes and saves only renamed models', () => {
    const rows = readSharedCredentialModels(file)!;
    expect(rows).toEqual([{ name: 'gpt-5', alias: '' }]);
    expect(buildSharedCredentialModelAliases(rows)).toEqual([]);
    rows[0].alias = ' my-gpt ';
    expect(buildSharedCredentialModelAliases(rows)).toEqual([{ name: 'gpt-5', alias: 'my-gpt' }]);
  });

  test('reopening preserves upstream names, advanced settings and currently unavailable aliases', () => {
    const aliases = [
      {
        name: 'gpt-5',
        alias: 'my-gpt',
        fork: true,
        'display-name': 'My GPT',
        'force-mapping': true,
      },
      { name: 'unavailable-model', alias: 'saved-model', extension: 'preserved' },
    ];
    const rows = readSharedCredentialModels({
      ...file,
      model_aliases: aliases,
      model_definitions: [
        { id: '573-codex#1/my-gpt', name: 'gpt-5' },
        { id: '573-codex#1/gpt-5', name: 'gpt-5' },
        { id: 'gpt-5', name: 'gpt-5' },
      ],
    })!;
    expect(rows).toHaveLength(2);
    expect(buildSharedCredentialModelAliases(rows)).toEqual(aliases);
    rows[0].alias = 'renamed-again';
    expect(buildSharedCredentialModelAliases(rows)[0]).toEqual({
      ...aliases[0],
      alias: 'renamed-again',
    });
    rows[0].alias = '';
    expect(buildSharedCredentialModelAliases(rows)).toEqual([aliases[1]]);
  });

  test('preserves inherited aliases without pinning them and keeps slashes in upstream IDs', () => {
    const rows = readSharedCredentialModels({
      ...file,
      model_definitions: [
        { id: '573-codex#1/public-name', name: 'org/model' },
        { id: 'public-name', name: 'org/model' },
        { id: '573-codex#1/org/other', name: 'org/other' },
      ],
    })!;
    expect(rows).toEqual([
      {
        name: 'org/model',
        alias: 'public-name',
        inherited: { name: 'org/model', alias: 'public-name' },
      },
      { name: 'org/other', alias: '' },
    ]);
    expect(buildSharedCredentialModelAliases(rows)).toEqual([]);
    rows[1].alias = 'other';
    expect(buildSharedCredentialModelAliases(rows)).toEqual([
      { name: 'org/other', alias: 'other' },
    ]);
    rows[0].forceMapping = true;
    expect(buildSharedCredentialModelAliases(rows)[0]).toEqual({
      name: 'org/model',
      alias: 'public-name',
      'force-mapping': true,
    });
  });

  test('does not overwrite unreadable settings and rejects missing upstream names or duplicate aliases', () => {
    expect(readSharedCredentialModels({ ...file, model_aliases: { unexpected: true } })).toBeNull();
    expect(() => buildSharedCredentialModelAliases([{ name: '', alias: 'alias' }])).toThrow();
    expect(() =>
      buildSharedCredentialModelAliases([
        { name: 'a', alias: 'same' },
        { name: 'b', alias: 'SAME' },
      ])
    ).toThrow();
    expect(buildSharedCredentialModelAliases([{ name: 'gpt-5', alias: 'gpt-5' }])).toEqual([]);
  });
});

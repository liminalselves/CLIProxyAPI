import { describe, expect, test } from 'bun:test';
import {
  normalizeOauthModelAlias,
  serializeOauthModelAliases,
} from '../src/services/api/authFiles';

describe('OAuth model alias entries', () => {
  test('normalizes legacy entries down to name and alias only', () => {
    const normalized = normalizeOauthModelAlias({
      codex: [
        { name: 'gpt-source', alias: 'gpt-alias', 'force-mapping': true },
        { name: 'gpt-source-2', alias: 'gpt-alias-2', forceMapping: false, fork: true },
      ],
    });

    expect(normalized.codex).toEqual([
      { name: 'gpt-source', alias: 'gpt-alias' },
      { name: 'gpt-source-2', alias: 'gpt-alias-2' },
    ]);
    expect(serializeOauthModelAliases(normalized.codex)).toEqual([
      { name: 'gpt-source', alias: 'gpt-alias' },
      { name: 'gpt-source-2', alias: 'gpt-alias-2' },
    ]);
  });
});

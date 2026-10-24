import { describe, expect, spyOn, test } from 'bun:test';
import { apiClient } from '../src/services/api/client';
import { sharingApi } from '../src/services/api/sharing';
import { normalizeKeyBlockedModels } from '../src/features/sharing/keyFilters';

describe('key model filter', () => {
  test('normalizes legacy keys without a filter list to an empty array', async () => {
    const key = {
      id: 'k1',
      owner: 'alice',
      name: 'phone',
      hint: 'sk-...1234',
      auto_balance: true,
      disabled: false,
      created_at: '2026-10-10T00:00:00Z',
    };
    const get = spyOn(apiClient, 'get').mockResolvedValue({
      keys: [
        { ...key, blocked_models: null },
        { ...key, id: 'k2' },
      ],
    });
    try {
      const response = await sharingApi.keys();
      expect(get).toHaveBeenCalledWith('/v8/hub/keys', { signal: undefined });
      expect(response.keys.map((entry) => entry.blocked_models)).toEqual([[], []]);
    } finally {
      get.mockRestore();
    }
  });

  test('sends the blocked model list verbatim when updating a key', async () => {
    const patch = spyOn(apiClient, 'patch').mockResolvedValue({});
    try {
      await sharingApi.updateKey('k1', { blocked_models: ['deepseek', '123/gpt-5'] });
      expect(patch).toHaveBeenCalledWith('/v8/hub/keys/k1', {
        blocked_models: ['deepseek', '123/gpt-5'],
      });
      await sharingApi.updateKey('k1', { blocked_models: [] });
      expect(patch).toHaveBeenCalledWith('/v8/hub/keys/k1', { blocked_models: [] });
    } finally {
      patch.mockRestore();
    }
  });

  test('keeps namespaced models distinct from their last segment and preserves unknown routes', () => {
    const base = 'vanchin/deepseek-v4.1-flash';
    const route = { id: `573-provider/${base}`, base_model: base, channel: '573-provider' };
    expect(normalizeKeyBlockedModels([base, 'deepseek-v4.1-flash'], [route])).toEqual([
      base,
      'deepseek-v4.1-flash',
    ]);
    expect(normalizeKeyBlockedModels([route.id, base], [route])).toEqual([base]);
    expect(normalizeKeyBlockedModels([route.id, '573-provider'], [route])).toEqual([
      '573-provider',
    ]);
    expect(normalizeKeyBlockedModels([route.id, base])).toEqual([route.id, base]);
  });

  test('normalizeKeyBlockedModels trims, dedupes, and drops catalog routes covered by a block', () => {
    const routes = [
      { id: '123/deepseek', base_model: 'deepseek', channel: '123' },
      { id: '564/deepseek', base_model: 'deepseek', channel: '564' },
      { id: '123-kimi#1/gpt-6', base_model: 'gpt-6', channel: '123-kimi#1' },
      { id: '456/gpt-6', base_model: 'gpt-6', channel: '456' },
    ];
    expect(normalizeKeyBlockedModels([])).toEqual([]);
    expect(normalizeKeyBlockedModels([' deepseek ', '', 'deepseek'])).toEqual(['deepseek']);
    // A bare model block already covers every pinned route of that model.
    expect(normalizeKeyBlockedModels(['123/deepseek', 'deepseek', '564/deepseek'], routes)).toEqual([
      'deepseek',
    ]);
    // Routes of other models and unmatched pinned routes stay.
    expect(normalizeKeyBlockedModels(['123/deepseek', 'gpt-5'])).toEqual(['123/deepseek', 'gpt-5']);
    // A credential or channel scope covers the routes under its prefix.
    expect(normalizeKeyBlockedModels(['123-kimi#1/gpt-6', '123-kimi#1', '456/gpt-6'], routes)).toEqual([
      '123-kimi#1',
      '456/gpt-6',
    ]);
  });
});

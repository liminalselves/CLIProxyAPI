import { afterEach, describe, expect, spyOn, test } from 'bun:test';
import i18n from '@/i18n';
import { KIMI_CONFIG } from '@/features/quota/providers/kimi/data';
import { apiCallApi, authFilesApi } from '@/services/api';
import { useQuotaStore } from '@/stores/useQuotaStore';
import { KIMI_USAGE_URL } from '@/utils/quota';

const file = { name: 'kimi-ai-fixture.json', provider: 'kimi', authIndex: 'fixture-index' };
const response = { statusCode: 200, header: {}, bodyText: '', body: { limits: [] } };
afterEach(() => useQuotaStore.getState().clearQuotaCache());

describe('Kimi server-resolved quota domain', () => {
  test.each([{}, { runtimeOnly: true }])(
    'queries quota without downloading an authentication file: %j',
    async (fields) => {
      const download = spyOn(authFilesApi, 'downloadText').mockRejectedValue(new Error('403'));
      const request = spyOn(apiCallApi, 'request').mockResolvedValue(response);
      try {
        const rows = await KIMI_CONFIG.fetchQuota({ ...file, ...fields }, i18n.t);
        expect(download).not.toHaveBeenCalled();
        expect(request.mock.calls[0][0]).toMatchObject({
          authIndex: 'fixture-index',
          url: KIMI_USAGE_URL,
          header: { Authorization: 'Bearer $TOKEN$' },
        });
        expect(KIMI_CONFIG.buildSuccessState(rows).status).toBe('success');
      } finally {
        download.mockRestore();
        request.mockRestore();
      }
    }
  );

  test('rejects a missing credential identity before requesting quota', async () => {
    const request = spyOn(apiCallApi, 'request');
    try {
      await expect(KIMI_CONFIG.fetchQuota({ ...file, authIndex: '' }, i18n.t)).rejects.toThrow(
        i18n.t('kimi_quota.missing_auth_index')
      );
      expect(request).not.toHaveBeenCalled();
    } finally {
      request.mockRestore();
    }
  });

  for (const scope of ['session', 'file', 'unrelated'] as const) {
    test(`handles ${scope} invalidation while requesting quota`, async () => {
      const request = spyOn(apiCallApi, 'request').mockImplementation(async () => {
        useQuotaStore
          .getState()
          .clearQuotaCache(
            scope === 'session' ? undefined : [scope === 'file' ? file.name : 'unrelated.json']
          );
        return response;
      });
      try {
        if (scope === 'unrelated') {
          await KIMI_CONFIG.fetchQuota(file, i18n.t);
        } else {
          await expect(KIMI_CONFIG.fetchQuota(file, i18n.t)).rejects.toThrow(
            i18n.t('kimi_quota.stale_request')
          );
        }
      } finally {
        request.mockRestore();
      }
    });
  }
});

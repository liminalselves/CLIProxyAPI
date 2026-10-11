import { afterEach, expect, spyOn, test } from 'bun:test';
import { META_CONFIG } from '@/features/quota/providers/meta/data';
import { authFilesApi } from '@/services/api/authFiles';
import { apiCallApi } from '@/services/api/apiCall';
import { apiClient } from '@/services/api/client';
import { metaQuotaResponse } from './fixtures/metaQuota';
import { useQuotaStore } from '@/stores/useQuotaStore';
import i18n from '@/i18n';

const file = { name: 'meta-fixture.json', type: 'meta', authIndex: 'fixture-index' };
afterEach(() => useQuotaStore.getState().clearQuotaCache());

for (const hasUsage of [true, false]) {
  test(`Meta adapter queries ${hasUsage ? 'populated' : 'missing'} usage without credential export`, async () => {
    const download = spyOn(authFilesApi, 'downloadText').mockRejectedValue(new Error('403'));
    const post = spyOn(apiClient, 'post').mockResolvedValue({
      status_code: 200,
      header: {},
      body: JSON.stringify({
        ...metaQuotaResponse,
        subs_usage: hasUsage ? metaQuotaResponse.subs_usage : undefined,
      }),
    });
    try {
      const data = await META_CONFIG.fetchQuota(file, i18n.t);
      expect(download).not.toHaveBeenCalled();
      expect(post).toHaveBeenCalledWith(
        '/requests/api-call',
        {
          authIndex: 'fixture-index',
          method: 'POST',
          url: 'https://api.meta.ai/muse-code/key',
          header: {
            Accept: 'application/json',
            'Content-Type': 'application/json',
            Authorization: 'Bearer $DCA_TOKEN$',
            'x-api-version': '1.0.0',
          },
          data: '{}',
        },
        undefined
      );
      const state = META_CONFIG.buildSuccessState(data);
      expect(state.data?.windows.map((window) => window.usedPercent)).toEqual(
        hasUsage ? [2, 0] : [null, null]
      );
      for (const secret of ['dca:', 'LLM|', 'fixture@example.invalid', 'Fixture User']) {
        expect(JSON.stringify(state)).not.toContain(secret);
      }
    } finally {
      download.mockRestore();
      post.mockRestore();
    }
  });
}

for (const invalidation of ['session', 'file', 'unrelated'] as const) {
  test(`Meta adapter handles ${invalidation} invalidation during a quota request`, async () => {
    const request = spyOn(apiCallApi, 'request').mockImplementation(async () => {
      useQuotaStore
        .getState()
        .clearQuotaCache(
          invalidation === 'session'
            ? undefined
            : [invalidation === 'file' ? file.name : 'unrelated.json']
        );
      return { statusCode: 200, header: {}, bodyText: '', body: metaQuotaResponse };
    });
    try {
      if (invalidation === 'unrelated') {
        const data = await META_CONFIG.fetchQuota(file, i18n.t);
        expect(data.windows[1].usedPercent).toBe(0);
      } else {
        await expect(META_CONFIG.fetchQuota(file, i18n.t)).rejects.toThrow(
          i18n.t('meta_quota.stale_request')
        );
      }
    } finally {
      request.mockRestore();
    }
  });
}

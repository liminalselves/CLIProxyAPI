import { afterEach, describe, expect, spyOn, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import i18n from '@/i18n';
import { apiCallApi, authFilesApi } from '@/services/api';
import { useQuotaStore } from '@/stores/useQuotaStore';
import { getQuotaCacheKey } from '@/utils/quota/identity';
import { classifyQuotaFiles, filterEntriesByTab } from '@/features/quota/logic';
import { buildTimelineLane } from '@/features/quota/quotaTimelineModel';
import { collectQuotaRowInstants, nextRecoveryMs } from '@/features/quota/resetSchedule';
import { QUOTA_CLASS_KEYS, type QuotaClassMap } from '@/features/quota/types';
import {
  ZCODE_CONFIG,
  ZCODE_QUOTA_URL,
  ZCODE_RESET_STATUS_URL,
  ZCODE_RESET_USE_URL,
} from '@/features/quota/providers/zcode/data';
import { parseZCodeQuota, parseZCodeResetCredits } from '@/features/quota/providers/zcode/parse';
import { ZCodeQuotaBody } from '@/features/quota/providers/zcode/ZCodeQuotaBody';
import { CodexQuotaBody } from '@/features/quota/providers/codex/CodexQuotaBody';

const file = { name: 'zcode-fixture.json', provider: 'zcode', authIndex: 'fixture-index' };
const now = Date.now();
const quotaPayload = {
  code: 200,
  success: true,
  data: {
    limits: [
      {
        type: 'TOKENS_LIMIT',
        unit: 3,
        number: 5,
        percentage: 25,
        nextResetTime: now + 3600000,
        usageDetails: [{ modelCode: 'model:glm-5.3', displayName: 'GLM-5.3' }],
      },
      { type: 'CREDIT_LIMIT', unit: 6, percentage: 90, nextResetTime: now + 86400000 },
      {
        type: 'TIME_LIMIT',
        unit: 5,
        number: 1,
        percentage: 100,
        nextResetTime: now + 20 * 86400000,
      },
    ],
  },
};
const resetPayload = {
  code: 0,
  data: {
    available_five_hour_resets: [{ expire_at: now + 7200000 }, { expire_at: now - 1000 }],
    available_week_resets: [{ expire_at: now + 86400000 }],
    latest_five_hour_reset_history: null,
    latest_week_reset_history: null,
    has_unread_history: false,
  },
};
const response = (body: unknown, statusCode = 200) => ({
  statusCode,
  header: {},
  bodyText: '',
  body,
});
const classes = Object.fromEntries(
  QUOTA_CLASS_KEYS.map((key) => [key, key])
) as unknown as QuotaClassMap;
afterEach(() => useQuotaStore.getState().clearQuotaCache());

describe('ZCode quota and reset cards', () => {
  test('uses official used percentages, millisecond timestamps and Codex labels', () => {
    const { windows } = parseZCodeQuota(quotaPayload, i18n.t);
    expect(windows.map((w) => w.usedPercent)).toEqual([25, 90, 100]);
    expect(windows.map((w) => w.periodHours)).toEqual([5, 168, null]);
    expect(windows[0].resetAtMs).toBe(now + 3600000);
    expect(windows[0].models).toEqual(['GLM-5.3']);
    expect(windows.map((w) => w.labelKey)).toEqual([
      'codex_quota.primary_window',
      'codex_quota.secondary_window',
      'codex_quota.team_secondary_window',
    ]);
  });

  test('preserves unknown usage and count fallback, rejecting malformed quota', () => {
    const { windows } = parseZCodeQuota(
      {
        code: 0,
        data: {
          limits: [
            { type: 'TOKENS_LIMIT', percentage: 0 },
            { type: 'TOKENS_LIMIT', percentage: '42' },
            { type: 'CREDIT_LIMIT', number: 100, remaining: 25 },
          ],
        },
      },
      i18n.t
    );
    expect(windows.map((w) => w.usedPercent)).toEqual([0, null, 75]);
    expect(parseZCodeQuota({ code: 0, data: { limits: [] } }, i18n.t).windows).toEqual([]);
    expect(() => parseZCodeQuota({ code: 0, data: {} }, i18n.t)).toThrow();
  });

  test('keeps reset-card scope and expiry, excludes expired cards and rejects fake zero counts', () => {
    const data = parseZCodeResetCredits(resetPayload, now);
    expect(data.rateLimitResetCreditsAvailableCount).toBe(2);
    expect(data.rateLimitResetCredits.map((c) => c.resetType)).toEqual(['FIVE_HOUR', 'WEEK']);
    expect(Date.parse(data.rateLimitResetCredits[0].expiresAt)).toBe(now + 7200000);
    expect(() => parseZCodeResetCredits({ code: 0, data: {} })).toThrow();
  });

  test('queries only quota and reset cards without downloading credentials', async () => {
    const download = spyOn(authFilesApi, 'downloadText');
    const request = spyOn(apiCallApi, 'request')
      .mockResolvedValueOnce(response(quotaPayload))
      .mockResolvedValueOnce(response(resetPayload));
    try {
      const data = await ZCODE_CONFIG.fetchQuota(file, i18n.t);
      expect(request.mock.calls.map(([r]) => r.url)).toEqual([
        ZCODE_QUOTA_URL,
        ZCODE_RESET_STATUS_URL,
      ]);
      expect(request.mock.calls[0][0].header?.Authorization).toBe('$TOKEN$');
      expect(request.mock.calls[1][0].header?.Authorization).toBeUndefined();
      expect(download).not.toHaveBeenCalled();
      expect(data.rateLimitResetCreditsAvailableCount).toBe(2);
    } finally {
      request.mockRestore();
      download.mockRestore();
    }
  });

  test('reset-card errors retain quota, but unauthorized quota still fails', async () => {
    const request = spyOn(apiCallApi, 'request')
      .mockResolvedValueOnce(response(quotaPayload))
      .mockResolvedValueOnce(response({ error: 'fixture authorization expired' }, 401))
      .mockResolvedValueOnce(response({}, 401));
    try {
      const data = await ZCODE_CONFIG.fetchQuota(file, i18n.t);
      expect(data.windows).toHaveLength(3);
      expect(data.rateLimitResetCreditsAvailableCount).toBeUndefined();
      expect(data.rateLimitResetCreditsError).toContain('401');
      expect(ZCODE_CONFIG.canResetQuota!(ZCODE_CONFIG.buildSuccessState(data))).toBe(false);
      await expect(ZCODE_CONFIG.fetchQuota(file, i18n.t)).rejects.toMatchObject({ status: 401 });
    } finally {
      request.mockRestore();
    }
  });

  test('invalidated observations are not committed after reset-card loading', async () => {
    const request = spyOn(apiCallApi, 'request').mockImplementation(async ({ url }) => {
      if (url === ZCODE_RESET_STATUS_URL) useQuotaStore.getState().clearQuotaCache([file.name]);
      return response(url === ZCODE_QUOTA_URL ? quotaPayload : resetPayload);
    });
    try {
      await expect(ZCODE_CONFIG.fetchQuota(file, i18n.t)).rejects.toThrow(
        i18n.t('kimi_quota.stale_request')
      );
    } finally {
      request.mockRestore();
    }
  });

  test('consumes exactly the chosen card type with an idempotency key and refreshes quota', async () => {
    const quota = ZCODE_CONFIG.buildSuccessState({
      ...parseZCodeQuota(quotaPayload, i18n.t),
      ...parseZCodeResetCredits(resetPayload, now),
    });
    useQuotaStore.getState().setZCodeQuota({ [getQuotaCacheKey(file)]: quota });
    expect(ZCODE_CONFIG.getResetOptions!(quota, i18n.t).map((option) => option.value)).toEqual([
      'FIVE_HOUR',
      'WEEK',
    ]);
    const request = spyOn(apiCallApi, 'request')
      .mockResolvedValueOnce(response({ code: 0, data: { used: true } }))
      .mockResolvedValueOnce(response(quotaPayload))
      .mockResolvedValueOnce(
        response({
          code: 0,
          data: {
            available_five_hour_resets: [{ expire_at: now + 7200000 }],
            available_week_resets: [],
          },
        })
      );
    try {
      const updated = await ZCODE_CONFIG.resetQuota!(file, i18n.t, 'WEEK');
      expect(request.mock.calls.map(([r]) => r.url)).toEqual([
        ZCODE_RESET_USE_URL,
        ZCODE_QUOTA_URL,
        ZCODE_RESET_STATUS_URL,
      ]);
      const consume = request.mock.calls[0][0];
      expect(consume.method).toBe('POST');
      expect(JSON.parse(consume.data!)).toMatchObject({ reset_type: 'WEEK' });
      expect(JSON.parse(consume.data!).idempotency_key).toMatch(/^[0-9a-f-]{36}$/);
      expect(updated.rateLimitResetCreditsAvailableCount).toBe(1);
    } finally {
      request.mockRestore();
    }
  });

  test('reuse renders identical Codex UI without subscription data and includes timeline credits', () => {
    const quota = ZCODE_CONFIG.buildSuccessState({
      ...parseZCodeQuota(quotaPayload, i18n.t),
      ...parseZCodeResetCredits(resetPayload, now),
    });
    const markup = renderToStaticMarkup(createElement(ZCodeQuotaBody, { quota, classes }));
    expect(markup).toBe(
      renderToStaticMarkup(createElement(CodexQuotaBody, { quota, classes, provider: 'zcode' }))
    );
    expect(markup).toContain(i18n.t('codex_quota.reset_credits_label'));
    expect(markup).toContain(i18n.t('codex_quota.primary_window'));
    expect(markup).toContain('75%');
    expect(markup).not.toContain('GLM Coding Pro');
    expect(markup).not.toContain('zcode_quota.');
    const lane = buildTimelineLane({
      name: file.name,
      displayName: 'ZCode',
      provider: 'zcode',
      quota,
      maxPeriodHours: 336,
    });
    expect(lane.periodHours).toBe(168);
    expect(lane.remaining).toBe(10);
    expect(lane.resetCredits).toHaveLength(1);
    expect(collectQuotaRowInstants('zcode', quota)).toHaveLength(5);
    expect(nextRecoveryMs('zcode', quota, now)).toBe(now + 3600000);
    const entries = classifyQuotaFiles([file, { ...file, name: 'disabled.json', disabled: true }]);
    expect(filterEntriesByTab(entries, 'zcode').map((e) => e.file.name)).toEqual([file.name]);
  });
});

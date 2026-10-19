import { afterEach, describe, expect, spyOn, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import i18n from '@/i18n';
import { apiCallApi, antigravitySubscriptionApi } from '@/services/api';
import { ANTIGRAVITY_CONFIG } from '@/features/quota/providers/antigravity/data';
import { AntigravityQuotaBody } from '@/features/quota/providers/antigravity/AntigravityQuotaBody';
import { collectExpiringQuotaRows } from '@/features/quota/expiringQuota';
import { bindQuotaClasses, QUOTA_CLASS_KEYS } from '@/features/quota/types';
import { ANTIGRAVITY_REQUEST_HEADERS, buildAntigravityQuotaGroups } from '@/utils/quota';
import type { AntigravityQuotaSummaryPayload } from '@/types';

const payload: AntigravityQuotaSummaryPayload = {
  groups: [
    {
      displayName: 'Gemini Models',
      buckets: [
        {
          bucketId: 'gemini-weekly',
          window: 'weekly',
          remainingFraction: 0.77,
          resetTime: '2030-01-04T00:00:00Z',
        },
        {
          bucketId: 'gemini-5h',
          window: '5h',
          remainingFraction: 0.73,
          resetTime: '2030-01-01T04:00:00Z',
        },
      ],
    },
    {
      displayName: 'Claude and GPT models',
      buckets: [
        {
          bucketId: '3p-weekly',
          window: 'weekly',
          remainingFraction: 1,
          resetTime: '2030-01-08T00:00:00Z',
        },
        {
          bucketId: '3p-5h',
          window: '5h',
          remainingFraction: 1,
          resetTime: '2030-01-01T05:00:00Z',
        },
      ],
    },
    {
      displayName: 'Gemini 4 Argon',
      buckets: [
        {
          bucketId: 'gemini-argon-wtu-weekly',
          displayName: 'Gemini 4 Argon Weekly Limit',
          window: 'weekly',
          remainingFraction: 0.79,
          resetTime: '2030-01-07T00:00:00Z',
        },
      ],
    },
  ],
};

const originalRequest = apiCallApi.request;
const originalSubscription = antigravitySubscriptionApi.get;
afterEach(() => {
  apiCallApi.request = originalRequest;
  antigravitySubscriptionApi.get = originalSubscription;
});

describe('Antigravity split Gemini quota', () => {
  test('leaves the current Hub user agent to the server', () => {
    expect(ANTIGRAVITY_REQUEST_HEADERS).not.toHaveProperty('User-Agent');
  });

  test('preserves the independent Gemini, third-party and Argon windows', () => {
    const groups = buildAntigravityQuotaGroups(payload);
    expect(groups.map((group) => group.label)).toEqual([
      'Gemini Models',
      'Claude and GPT models',
      'Gemini 4 Argon',
    ]);
    expect(groups.map((group) => group.buckets.map((bucket) => bucket.remainingFraction))).toEqual([
      [0.73, 0.77],
      [1, 1],
      [0.79],
    ]);
    expect(groups[2].buckets[0].periodHours).toBe(168);
    const rows = collectExpiringQuotaRows(
      'antigravity',
      { status: 'success', groups },
      Date.parse('2030-01-01T00:00:00Z')
    );
    expect(rows.map((row) => row.remainingPercent)).toEqual([77, 79, 100]);
    expect(rows[1].prefix).toBe('Gemini 4 Argon');
  });

  test('fetches and renders all three groups without inventing an Argon five-hour window', async () => {
    const request = spyOn(apiCallApi, 'request').mockResolvedValue({
      statusCode: 200,
      header: {},
      body: payload,
      bodyText: JSON.stringify(payload),
    });
    spyOn(antigravitySubscriptionApi, 'get').mockResolvedValue(null);
    const data = await ANTIGRAVITY_CONFIG.fetchQuota(
      { name: 'antigravity.json', auth_index: '1', project_id: 'quota-project' },
      i18n.t
    );
    const quota = ANTIGRAVITY_CONFIG.buildSuccessState(data);
    expect(request).toHaveBeenCalledTimes(1);
    expect(request.mock.calls[0][0].header).not.toHaveProperty('User-Agent');
    const markup = renderToStaticMarkup(
      createElement(AntigravityQuotaBody, {
        quota,
        classes: bindQuotaClasses(
          Object.fromEntries(QUOTA_CLASS_KEYS.map((key) => [key, key])),
          'test-host'
        ),
      })
    );
    expect(quota.groups).toHaveLength(3);
    expect(markup).toContain('Gemini 4 Argon');
    expect(markup).toContain('Gemini 4 Argon Weekly Limit');
    expect(markup).toContain('79%');
    expect(markup.match(/class="antigravityQuotaGroup"/g)).toHaveLength(3);
    expect(markup.match(/class="quotaRow"/g)).toHaveLength(5);
  });
});

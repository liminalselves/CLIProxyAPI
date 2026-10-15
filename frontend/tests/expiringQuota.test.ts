/**
 * Expiring-quota collector contract.
 *
 * The page shows weekly/monthly subscription quota that is about to reset with
 * remainder still left — "use it or lose it". The collector must reject short
 * cycles, past resets and empty remainders, and must read each provider's
 * distinct state shape on its own terms.
 */

import { describe, expect, test } from 'bun:test';
import {
  buildExpiringEntries,
  collectExpiringQuotaRows,
  expiryUrgency,
  WEEKLY_PERIOD_HOURS,
} from '@/features/quota/expiringQuota';
import { DAY_MS, HOUR_MS } from '@/utils/time/durations';
import type { QuotaFileEntry } from '@/features/quota/logic';
import type { AuthFileItem } from '@/types';

const NOW = Date.parse('2026-10-14T12:00:00Z');
const WEEK_MS = WEEKLY_PERIOD_HOURS * HOUR_MS;

const entry = (type: QuotaFileEntry['type'], name = `${type}.json`): QuotaFileEntry => ({
  file: { name } as AuthFileItem,
  type,
});

describe('collectExpiringQuotaRows shared guards', () => {
  test('ignores non-success states', () => {
    const quota = { status: 'loading', windows: [] };
    expect(collectExpiringQuotaRows('claude', quota, NOW)).toEqual([]);
    expect(collectExpiringQuotaRows('claude', undefined, NOW)).toEqual([]);
  });

  test('drops short cycles, past resets and exhausted rows', () => {
    const rows = collectExpiringQuotaRows(
      'claude',
      {
        status: 'success',
        windows: [
          // 5-hour rolling window — recycles constantly, not a planning target.
          { id: 'five-hour', usedPercent: 10, resetAtMs: NOW + 3 * HOUR_MS, periodHours: 5 },
          // Weekly but fully consumed.
          { id: 'empty', usedPercent: 100, resetAtMs: NOW + DAY_MS, periodHours: 168 },
          // Weekly but already in the past.
          { id: 'past', usedPercent: 10, resetAtMs: NOW - HOUR_MS, periodHours: 168 },
          // Weekly with remainder — the only survivor.
          { id: 'weekly', usedPercent: 40, resetAtMs: NOW + 2 * DAY_MS, periodHours: 168 },
        ],
      },
      NOW
    );
    expect(rows.map((row) => row.id)).toEqual(['weekly']);
    expect(rows[0].remainingPercent).toBe(60);
  });

  test('treats unknown-period windows as long when the reset is over a day out', () => {
    const rows = collectExpiringQuotaRows(
      'codex',
      {
        status: 'success',
        windows: [
          { id: 'soon', usedPercent: 10, resetAtMs: NOW + 12 * HOUR_MS, periodHours: null },
          { id: 'far', usedPercent: 10, resetAtMs: NOW + 5 * DAY_MS, periodHours: null },
        ],
      },
      NOW
    );
    expect(rows.map((row) => row.id)).toEqual(['far']);
  });

  test('keeps rows with unknown remaining but drops known-zero ones', () => {
    const rows = collectExpiringQuotaRows(
      'codex',
      {
        status: 'success',
        windows: [
          { id: 'unknown', usedPercent: null, resetAtMs: NOW + WEEK_MS, periodHours: 168 },
          { id: 'zero', usedPercent: 100, resetAtMs: NOW + WEEK_MS, periodHours: 168 },
        ],
      },
      NOW
    );
    expect(rows.map((row) => row.id)).toEqual(['unknown']);
    expect(rows[0].remainingPercent).toBeNull();
  });

  test('sorts rows by reset instant', () => {
    const rows = collectExpiringQuotaRows(
      'claude',
      {
        status: 'success',
        windows: [
          { id: 'later', usedPercent: 10, resetAtMs: NOW + 6 * DAY_MS, periodHours: 168 },
          { id: 'sooner', usedPercent: 10, resetAtMs: NOW + DAY_MS, periodHours: 168 },
        ],
      },
      NOW
    );
    expect(rows.map((row) => row.id)).toEqual(['sooner', 'later']);
  });
});

describe('collectExpiringQuotaRows provider shapes', () => {
  test('antigravity reads group buckets and prefixes the group label', () => {
    const rows = collectExpiringQuotaRows(
      'antigravity',
      {
        status: 'success',
        groups: [
          {
            id: 'gemini-models',
            label: 'Gemini Models',
            buckets: [
              {
                id: 'w1',
                label: 'Weekly limit',
                remainingFraction: 0.25,
                resetAtMs: NOW + 2 * DAY_MS,
                periodHours: 168,
              },
              {
                id: 'h1',
                label: '5 hour limit',
                remainingFraction: 0.9,
                resetAtMs: NOW + 2 * HOUR_MS,
                periodHours: 5,
              },
            ],
          },
        ],
      },
      NOW
    );
    expect(rows).toHaveLength(1);
    expect(rows[0].remainingPercent).toBe(25);
    expect(rows[0].prefixKey).toBe('antigravity_quota.group_gemini_models');
    expect(rows[0].labelKey).toBe('antigravity_quota.weekly_limit');
  });

  test('kimi derives remaining from used/limit', () => {
    const rows = collectExpiringQuotaRows(
      'kimi',
      {
        status: 'success',
        rows: [
          {
            id: 'weekly',
            labelKey: 'kimi_quota.weekly_limit',
            used: 25,
            limit: 100,
            resetAtMs: NOW + 3 * DAY_MS,
            periodHours: 168,
          },
        ],
      },
      NOW
    );
    expect(rows).toHaveLength(1);
    expect(rows[0].remainingPercent).toBe(75);
  });

  test('xai weekly uses usagePercent; monthly uses usedPercent', () => {
    const weekly = collectExpiringQuotaRows(
      'xai',
      {
        status: 'success',
        billing: {
          periodType: 'weekly',
          usagePercent: 30,
          usedPercent: 80,
          planLabel: 'SuperGrok',
          resetAtMs: NOW + DAY_MS,
        },
      },
      NOW
    );
    expect(weekly.map((row) => row.id)).toEqual(['xai:weekly']);
    expect(weekly[0].remainingPercent).toBe(70);
    expect(weekly[0].prefix).toBe('SuperGrok');

    const monthly = collectExpiringQuotaRows(
      'xai',
      {
        status: 'success',
        billing: {
          periodType: 'monthly',
          usagePercent: 40,
          usedPercent: 40,
          resetAtMs: NOW + 10 * DAY_MS,
        },
      },
      NOW
    );
    expect(monthly.map((row) => row.id)).toEqual(['xai:monthly']);
    expect(monthly[0].labelKey).toBe('sharing.quota_expiry_period_monthly');
    expect(monthly[0].remainingPercent).toBe(60);
  });

  test('xai weekly summary also lists a monthly billing rollover when a cap exists', () => {
    const rows = collectExpiringQuotaRows(
      'xai',
      {
        status: 'success',
        billing: {
          periodType: 'weekly',
          usagePercent: 30,
          usedPercent: 80,
          monthlyLimitCents: 5000,
          billingPeriodEnd: new Date(NOW + 10 * DAY_MS).toISOString(),
          resetAtMs: NOW + DAY_MS,
        },
      },
      NOW
    );
    expect(rows.map((row) => row.id)).toEqual(['xai:weekly', 'xai:monthly-billing']);
    expect(rows[1].remainingPercent).toBe(20);
    expect(rows[1].resetAtMs).toBe(NOW + 10 * DAY_MS);
  });

  test('xai monthly rollover without any quota figure is skipped', () => {
    const rows = collectExpiringQuotaRows(
      'xai',
      {
        status: 'success',
        billing: {
          periodType: 'monthly',
          usagePercent: null,
          usedPercent: null,
          monthlyLimitCents: null,
          resetAtMs: NOW + 10 * DAY_MS,
        },
      },
      NOW
    );
    expect(rows).toEqual([]);
  });

  test('devin keeps only the weekly window', () => {
    const rows = collectExpiringQuotaRows(
      'devin',
      {
        status: 'success',
        windows: [
          { id: 'daily', remainingPercent: 90, resetAtMs: NOW + 4 * HOUR_MS, periodHours: 24 },
          { id: 'weekly', remainingPercent: 55, resetAtMs: NOW + 4 * DAY_MS, periodHours: 168 },
        ],
      },
      NOW
    );
    expect(rows.map((row) => row.id)).toEqual(['weekly']);
    expect(rows[0].remainingPercent).toBe(55);
  });

  test('meta reads resetAt seconds on the weekly window', () => {
    const resetAtSec = (NOW + 5 * DAY_MS) / 1000;
    const rows = collectExpiringQuotaRows(
      'meta',
      {
        status: 'success',
        data: {
          planName: 'Pro',
          windows: [
            { id: 'window', usedPercent: 10, resetAt: (NOW + 4 * HOUR_MS) / 1000 },
            { id: 'weekly', usedPercent: 60, resetAt: resetAtSec },
          ],
        },
      },
      NOW
    );
    expect(rows.map((row) => row.id)).toEqual(['weekly']);
    expect(rows[0].remainingPercent).toBe(40);
    expect(rows[0].resetAtMs).toBe(NOW + 5 * DAY_MS);
    expect(rows[0].prefix).toBe('Pro');
  });
});

describe('buildExpiringEntries', () => {
  test('keeps only credentials with rows and sorts soonest first', () => {
    const entries = [entry('claude', 'a.json'), entry('codex', 'b.json'), entry('kimi', 'c.json')];
    const quotaByKey: Record<string, unknown> = {
      'a.json': {
        status: 'success',
        windows: [{ id: 'w', usedPercent: 10, resetAtMs: NOW + 5 * DAY_MS, periodHours: 168 }],
      },
      'b.json': {
        status: 'success',
        windows: [{ id: 'w', usedPercent: 10, resetAtMs: NOW + DAY_MS, periodHours: 168 }],
      },
      'c.json': { status: 'success', rows: [] },
    };
    const result = buildExpiringEntries(
      entries,
      ({ file }) => quotaByKey[file.name] as never,
      NOW
    );
    expect(result.map((item) => item.entry.file.name)).toEqual(['b.json', 'a.json']);
    expect(result[0].soonestAtMs).toBe(NOW + DAY_MS);
  });
});

describe('expiryUrgency', () => {
  test('critical under a day, warning under three', () => {
    expect(expiryUrgency(NOW + HOUR_MS, NOW)).toBe('critical');
    expect(expiryUrgency(NOW + DAY_MS, NOW)).toBe('critical');
    expect(expiryUrgency(NOW + 2 * DAY_MS, NOW)).toBe('warning');
    expect(expiryUrgency(NOW + 3 * DAY_MS, NOW)).toBe('warning');
    expect(expiryUrgency(NOW + 6 * DAY_MS, NOW)).toBe('calm');
  });
});

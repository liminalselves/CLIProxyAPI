import type { TFunction } from 'i18next';
import type { CodexRateLimitResetCredit, ZCodeQuotaWindow } from '@/types';
import { isRecord } from '@/utils/helpers';

const finite = (value: unknown): number | null =>
  typeof value === 'number' && Number.isFinite(value) ? value : null;
const text = (value: unknown): string | null =>
  typeof value === 'string' && value.trim() ? value.trim() : null;
const clampPercent = (value: number) => Math.max(0, Math.min(100, value));

export function unwrapZCodeResponse(payload: unknown): unknown {
  if (!isRecord(payload) || payload.success === false) throw new Error('Invalid ZCode response');
  const code = payload.code;
  if (code !== undefined && ![0, 200, '0', '200'].includes(code as string | number)) {
    throw new Error(`ZCode request rejected (${String(code)})`);
  }
  if (code === undefined && payload.success !== true) throw new Error('Invalid ZCode response');
  return payload.data;
}

export function parseZCodeQuota(payload: unknown, t: TFunction) {
  const data = unwrapZCodeResponse(payload);
  if (!isRecord(data) || !Array.isArray(data.limits)) throw new Error('Invalid ZCode quota');
  const windows: ZCodeQuotaWindow[] = data.limits.flatMap((limit, index) => {
    if (!isRecord(limit) || !text(limit.type)) return [];
    const isModelLimit = ['TOKENS_LIMIT', 'CREDIT_LIMIT'].includes(String(limit.type));
    const fiveHour = isModelLimit && limit.unit === 3 && limit.number === 5;
    const weekly = isModelLimit && limit.unit === 6;
    const monthly = limit.type === 'TIME_LIMIT' && limit.unit === 5 && limit.number === 1;
    const labelKey = fiveHour
      ? 'codex_quota.primary_window'
      : weekly
        ? 'codex_quota.secondary_window'
        : monthly
          ? 'codex_quota.team_secondary_window'
          : 'kimi_quota.limit_index';
    const labelParams = { index: index + 1 };
    const percentage = finite(limit.percentage);
    const remaining = finite(limit.remaining);
    const number = finite(limit.number);
    // Official Coding Plan percentages are used capacity, not remaining capacity.
    const usedPercent =
      percentage !== null
        ? clampPercent(percentage)
        : remaining !== null && number !== null && number > 0
          ? clampPercent(100 - (remaining / number) * 100)
          : null;
    const reset = finite(limit.nextResetTime);
    const resetAtMs =
      reset !== null && reset > 0 && Number.isFinite(new Date(reset).getTime()) ? reset : null;
    const models = Array.isArray(limit.usageDetails)
      ? [
          ...new Set(
            limit.usageDetails.flatMap((detail) => {
              if (!isRecord(detail) || !text(detail.modelCode)) return [];
              return [text(detail.displayName) ?? String(detail.modelCode).replace(/^model:/i, '')];
            })
          ),
        ]
      : [];
    return [
      {
        id: `${limit.type}:${limit.unit ?? ''}:${limit.number ?? ''}:${index}`,
        label: t(labelKey, labelParams),
        labelKey,
        labelParams,
        resetLabel: '',
        usedPercent,
        resetAtMs,
        // Calendar-month tool quotas have no fixed-duration timeline projection.
        periodHours: fiveHour ? 5 : weekly ? 168 : null,
        models,
      },
    ];
  });
  return { windows };
}

export function parseZCodeResetCredits(payload: unknown, now = Date.now()) {
  const data = unwrapZCodeResponse(payload);
  if (
    !isRecord(data) ||
    !Array.isArray(data.available_five_hour_resets) ||
    !Array.isArray(data.available_week_resets)
  ) {
    throw new Error('Invalid ZCode reset credits');
  }
  const credits: CodexRateLimitResetCredit[] = [];
  for (const [resetType, items] of [
    ['FIVE_HOUR', data.available_five_hour_resets],
    ['WEEK', data.available_week_resets],
  ] as const) {
    for (const [index, item] of items.entries()) {
      if (!isRecord(item) || finite(item.expire_at) === null || (item.expire_at as number) <= 0) {
        throw new Error('Invalid ZCode reset credit expiry');
      }
      // The official client compares expire_at directly with Date.now().
      const expiresAtMs = item.expire_at as number;
      if (expiresAtMs <= now) continue;
      credits.push({
        id: `${resetType}:${expiresAtMs}:${index}`,
        status: 'available',
        grantedAt: '',
        expiresAt: new Date(expiresAtMs).toISOString(),
        resetType,
        labelKey:
          resetType === 'FIVE_HOUR' ? 'codex_quota.primary_window' : 'codex_quota.secondary_window',
      });
    }
  }
  credits.sort((a, b) => a.expiresAt.localeCompare(b.expiresAt));
  return { rateLimitResetCredits: credits, rateLimitResetCreditsAvailableCount: credits.length };
}

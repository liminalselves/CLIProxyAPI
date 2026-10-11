import type { TFunction } from 'i18next';
import { apiCallApi, getApiCallErrorMessage } from '@/services/api';
import { captureQuotaCacheGeneration, commitIfQuotaCacheCurrent, useQuotaStore } from '@/stores';
import type { AuthFileItem, ZCodeQuotaState } from '@/types';
import { createStatusError, isDisabledAuthFile, normalizeAuthIndex } from '@/utils/quota';
import { getQuotaCacheKey } from '@/utils/quota/identity';
import { resolveAuthProvider } from '@/utils/quota/validators';
import type { QuotaProviderData } from '../types';
import { parseZCodeQuota, parseZCodeResetCredits, unwrapZCodeResponse } from './parse';
import { isRecord } from '@/utils/helpers';

export const ZCODE_QUOTA_URL = 'https://bigmodel.cn/api/monitor/usage/quota/limit';
export const ZCODE_RESET_STATUS_URL = 'https://zcode.z.ai/api/v1/coding-plan/reset/status';
export const ZCODE_RESET_USE_URL = 'https://zcode.z.ai/api/v1/coding-plan/reset/use';
type ZCodeQuotaData = Omit<ZCodeQuotaState, 'status' | 'error' | 'errorStatus'>;

async function queryZCode(file: AuthFileItem, t: TFunction, url: string, data?: string) {
  const authIndex = normalizeAuthIndex(file.authIndex ?? file.auth_index);
  if (!authIndex) throw new Error(t('codex_quota.missing_auth_index'));
  const result = await apiCallApi.request({
    authIndex,
    method: data === undefined ? 'GET' : 'POST',
    url,
    // Reset requests use two different tokens, attached by the server.
    header:
      url === ZCODE_QUOTA_URL
        ? { Authorization: '$TOKEN$' }
        : { 'Content-Type': 'application/json' },
    data,
  });
  if (result.statusCode < 200 || result.statusCode >= 300) {
    throw createStatusError(getApiCallErrorMessage(result), result.statusCode);
  }
  return result.body;
}

async function fetchZCodeQuota(file: AuthFileItem, t: TFunction): Promise<ZCodeQuotaData> {
  const generation = captureQuotaCacheGeneration(file.name);
  const checkCurrent = () => {
    if (!commitIfQuotaCacheCurrent(generation, () => {}))
      throw new Error(t('kimi_quota.stale_request'));
  };
  const payload = await queryZCode(file, t, ZCODE_QUOTA_URL);
  checkCurrent();
  const quota = parseZCodeQuota(payload, t);
  let resetCredits: Pick<
    ZCodeQuotaData,
    'rateLimitResetCredits' | 'rateLimitResetCreditsAvailableCount' | 'rateLimitResetCreditsError'
  >;
  try {
    resetCredits = parseZCodeResetCredits(await queryZCode(file, t, ZCODE_RESET_STATUS_URL));
  } catch (error) {
    resetCredits = {
      rateLimitResetCreditsError:
        error instanceof Error ? error.message : t('common.unknown_error'),
    };
  }
  checkCurrent();
  return { ...quota, ...resetCredits };
}

const availableCredits = (quota: ZCodeQuotaState | undefined) =>
  (quota?.rateLimitResetCredits ?? []).filter(
    (credit) => Date.parse(credit.expiresAt) > Date.now()
  );

async function resetZCodeQuota(
  file: AuthFileItem,
  t: TFunction,
  resetType?: string
): Promise<ZCodeQuotaData> {
  const quota = useQuotaStore.getState().zcodeQuota[getQuotaCacheKey(file)];
  const selectedType = resetType ?? availableCredits(quota)[0]?.resetType;
  if (!availableCredits(quota).some((credit) => credit.resetType === selectedType)) {
    throw new Error(t('codex_quota.empty_windows'));
  }
  const result = unwrapZCodeResponse(
    await queryZCode(
      file,
      t,
      ZCODE_RESET_USE_URL,
      JSON.stringify({
        idempotency_key: crypto.randomUUID(),
        reset_type: selectedType,
      })
    )
  );
  if (!isRecord(result) || result.used !== true)
    throw new Error(t('codex_quota.reset_credits_invalid_payload'));
  return fetchZCodeQuota(file, t);
}

export const ZCODE_CONFIG: QuotaProviderData<ZCodeQuotaState, ZCodeQuotaData> = {
  type: 'zcode',
  i18nPrefix: 'zcode_quota',
  filterFn: (file) => resolveAuthProvider(file) === 'zcode' && !isDisabledAuthFile(file),
  fetchQuota: fetchZCodeQuota,
  resetQuota: resetZCodeQuota,
  canResetQuota: (quota) => availableCredits(quota).length > 0,
  getResetOptions: (quota, t) =>
    [...new Set(availableCredits(quota).map((credit) => credit.resetType!))].map((value) => ({
      value,
      label: t(
        value === 'FIVE_HOUR' ? 'codex_quota.primary_window' : 'codex_quota.secondary_window'
      ),
    })),
  storeSelector: (state) => state.zcodeQuota,
  storeSetter: 'setZCodeQuota',
  buildLoadingState: () => ({ status: 'loading', windows: [] }),
  buildSuccessState: (data) => ({ status: 'success', ...data }),
  buildErrorState: (message, status) => ({
    status: 'error',
    windows: [],
    error: message,
    errorStatus: status,
  }),
};

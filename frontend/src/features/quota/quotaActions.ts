/** React-free single-credential quota refresh, shared by every surface. */

import type { TFunction } from 'i18next';
import {
  captureQuotaCacheGeneration,
  commitIfQuotaCacheCurrent,
  useNotificationStore,
} from '@/stores';
import type { AuthFileItem } from '@/types';
import { getStatusFromError } from '@/utils/quota';
import { getQuotaCacheKey } from '@/utils/quota/identity';
import { enrichQuotaInBackground } from './quotaEnrichment';
import { getQuotaSetter, type QuotaAdapter } from './providers';

/**
 * Fetch one credential's quota: mark loading, commit success (with background
 * enrichment) or error, and surface the outcome as a notification. The quota
 * page card and the credential-card quota section both call this so a refresh
 * behaves identically no matter where it was triggered.
 */
export async function runQuotaRefresh(
  adapter: QuotaAdapter,
  file: AuthFileItem,
  t: TFunction
): Promise<void> {
  const cacheKey = getQuotaCacheKey(file);
  const cacheGeneration = captureQuotaCacheGeneration(file.name);
  const setQuota = getQuotaSetter(adapter);
  const showNotification = useNotificationStore.getState().showNotification;

  setQuota((prev) => ({
    ...prev,
    [cacheKey]: adapter.buildLoadingState(),
  }));

  try {
    const data = await adapter.fetchQuota(file, t);
    commitIfQuotaCacheCurrent(cacheGeneration, () => {
      const successState = adapter.buildSuccessState(data);
      setQuota((prev) => ({
        ...prev,
        [cacheKey]: successState,
      }));
      void enrichQuotaInBackground(adapter, file, data, successState, t);
      showNotification(t('auth_files.quota_refresh_success', { name: file.name }), 'success');
    });
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : t('common.unknown_error');
    const status = getStatusFromError(err);
    commitIfQuotaCacheCurrent(cacheGeneration, () => {
      setQuota((prev) => ({
        ...prev,
        [cacheKey]: adapter.buildErrorState(message, status),
      }));
      showNotification(
        t('auth_files.quota_refresh_failed', { name: file.name, message }),
        'error'
      );
    });
  }
}

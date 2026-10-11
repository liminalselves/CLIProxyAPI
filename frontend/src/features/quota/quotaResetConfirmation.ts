import { createElement } from 'react';
import type { TFunction } from 'i18next';
import type { AuthFileItem } from '@/types';
import { deriveAuthFileIdentity } from '@/features/authFiles/identity';
import type { QuotaAdapter, QuotaCardState } from './providers';
import { QuotaResetConfirmation } from './components/QuotaResetConfirmation';

/** Dialogs name the credential by its card identity, never the raw file name. */
export function quotaResetParams(adapter: QuotaAdapter, file: AuthFileItem) {
  const name = deriveAuthFileIdentity(file).primary || file.name;
  return { name, provider: adapter.type === 'zcode' ? 'ZCode' : 'Codex' };
}

export function buildQuotaResetConfirmation(
  adapter: QuotaAdapter,
  quota: QuotaCardState | undefined,
  file: AuthFileItem,
  t: TFunction
) {
  const options = quota ? (adapter.getResetOptions?.(quota, t) ?? []) : [];
  let option = options[0]?.value;
  const message = t('codex_quota.reset_confirm_message', quotaResetParams(adapter, file));
  return {
    message:
      options.length > 0
        ? createElement(QuotaResetConfirmation, {
            options,
            message,
            onChange: (value) => {
              option = value;
            },
          })
        : message,
    getOption: () => option,
  };
}

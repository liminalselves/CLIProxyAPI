import type { AuthFileItem, MetaQuotaData } from '@/types';
import type { ApiCallRequest, ApiCallResult } from '@/services/api/apiCall';
import { parseMetaQuotaPayload } from '@/services/api/metaQuota';
import { normalizeAuthIndex } from '@/utils/authIndex';

export const META_MUSE_QUOTA_URL = 'https://api.meta.ai/muse-code/key';

export type MetaQuotaErrorCode =
  'missing_auth_index' | 'missing_file' | 'stale_request' | 'invalid_response' | 'request_failed';

export class MetaQuotaError extends Error {
  readonly status?: number;

  constructor(
    public readonly code: MetaQuotaErrorCode,
    status?: number
  ) {
    super(code);
    this.name = 'MetaQuotaError';
    this.status = status;
  }
}

interface MetaQuotaDependencies {
  request: (payload: ApiCallRequest) => Promise<ApiCallResult>;
  captureCurrent: (name: string) => () => boolean;
}

/** DCA tokens are resolved server-side and never enter browser state. */
export function createMetaQuotaFetcher(deps: MetaQuotaDependencies) {
  return async (file: AuthFileItem): Promise<MetaQuotaData> => {
    const authIndex = normalizeAuthIndex(file.authIndex ?? file.auth_index);
    if (!authIndex) throw new MetaQuotaError('missing_auth_index');
    if (!file.name?.trim()) {
      throw new MetaQuotaError('missing_file');
    }
    const isCurrent = deps.captureCurrent(file.name);
    const assertCurrent = () => {
      if (!isCurrent()) throw new MetaQuotaError('stale_request');
    };
    assertCurrent();
    let response: ApiCallResult;
    try {
      assertCurrent();
      response = await deps.request({
        authIndex,
        method: 'POST',
        url: META_MUSE_QUOTA_URL,
        header: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          Authorization: 'Bearer $DCA_TOKEN$',
          'x-api-version': '1.0.0',
        },
        data: '{}',
      });
    } catch (error: unknown) {
      assertCurrent();
      const status =
        error !== null &&
        typeof error === 'object' &&
        typeof (error as { status?: unknown }).status === 'number'
          ? (error as { status: number }).status
          : undefined;
      throw new MetaQuotaError('request_failed', status);
    }
    assertCurrent();

    // Never derive an error message from body/bodyText: the endpoint can echo
    // api_key and other PII even on errors.
    if (!(response.statusCode >= 200 && response.statusCode < 300)) {
      throw new MetaQuotaError('request_failed', response.statusCode);
    }

    const quota = parseMetaQuotaPayload(response.body ?? response.bodyText);
    if (!quota) throw new MetaQuotaError('invalid_response');
    return quota;
  };
}

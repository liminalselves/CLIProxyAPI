/**
 * Per-credential model management API.
 *
 * Hub members use `/v8/hub/credentials/:route_id/models`; administrators use
 * `/v8/management/credentials/model-management?name=...`. Both share one
 * payload contract: the editable selection, the source catalog, the registered
 * result, and the source refresh policy.
 */

import { apiClient } from './client';
import { sharingPath } from './sharing';
import { useAuthStore } from '@/stores/useAuthStore';
import type { ApiError } from '@/types';

/** Wire shape of one configured model. Omitted fields inherit the source entry. */
export interface CredentialModelEntry {
  name: string;
  alias?: string;
  'max-context-length'?: number;
  thinking?: Record<string, unknown>;
  'is-compat'?: boolean;
  'support-configuration-update'?: boolean;
  'input-modalities'?: string[];
  'output-modalities'?: string[];
}

export type CredentialModelSourceKind =
  | 'remote_catalog'
  | 'file_catalog'
  | 'builtin'
  | 'disabled'
  | 'account_upstream'
  | (string & {});

export interface CredentialModelSource {
  kind: CredentialModelSourceKind;
  auto_refresh: boolean;
  refresh_interval_seconds: number;
  supports_refresh: boolean;
  upstream_discovery: boolean;
}

export interface CredentialModelCapabilities {
  manual_models: boolean;
  aliases: boolean;
  thinking: boolean;
  max_context_length: boolean;
  modalities: boolean;
  is_compat: boolean;
  support_configuration_update: boolean;
  merge: boolean;
  reset: boolean;
}

export interface CredentialAvailableModel {
  id: string;
  name?: string;
  display_name?: string;
  context_length?: number;
  thinking?: Record<string, unknown>;
  supportedInputModalities?: string[];
  supportedOutputModalities?: string[];
  supports_web_search?: boolean;
}

export interface CredentialModelManagement {
  models: CredentialModelEntry[];
  configured: boolean;
  available_models: CredentialAvailableModel[];
  catalog_available: boolean;
  registered_models: { id: string; name: string }[];
  source: CredentialModelSource;
  enabled_models?: unknown;
  excluded_models?: unknown;
  capabilities: CredentialModelCapabilities;
}

export type CredentialModelTarget = {
  name: string;
  /** Hub credentials are addressed by their route id. */
  routeId?: string;
  /** Management lookups may disambiguate same-name files. */
  authIndex?: string | number | null;
};

const endpoints = (target: CredentialModelTarget) => {
  if (useAuthStore.getState().account) {
    if (!target.routeId) throw new Error('Credential route is unavailable');
    const base = sharingPath(`/credentials/${encodeURIComponent(target.routeId)}/models`);
    return { models: base, discover: `${base}/discover`, params: undefined };
  }
  const params: Record<string, string> = { name: target.name };
  const authIndex = String(target.authIndex ?? '').trim();
  if (authIndex) params.auth_index = authIndex;
  return {
    models: '/credentials/model-management',
    discover: '/credentials/models/discover',
    params,
  };
};

/** The backend answers 409 when this credential kind has no model management. */
export const isCredentialModelsUnsupported = (error: unknown): boolean =>
  (error as ApiError | undefined)?.status === 409;

export const credentialModelManagement = {
  get: (target: CredentialModelTarget) => {
    const route = endpoints(target);
    return apiClient.get<CredentialModelManagement>(route.models, { params: route.params });
  },
  replace: (target: CredentialModelTarget, models: CredentialModelEntry[]) => {
    const route = endpoints(target);
    return apiClient.put<CredentialModelManagement>(
      route.models,
      { models },
      { params: route.params }
    );
  },
  discover: (target: CredentialModelTarget) => {
    const route = endpoints(target);
    return apiClient.post<CredentialModelManagement>(
      route.discover,
      undefined,
      { params: route.params }
    );
  },
};

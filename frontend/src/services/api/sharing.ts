import { apiClient } from './client';
import type {
  Account,
  AuditEntry,
  BoardMessage,
  CallEntry,
  Channel,
  HistoryPage,
  Invitation,
  MarketplaceModel,
  ModelPerformance,
  PersonalKey,
  Summary,
  UsageEntry,
} from '@/features/sharing/types';

export interface ProviderModelInput {
  name: string;
  alias?: string;
}

export interface ProviderPayload {
  name?: string;
  label?: string;
  base_url?: string;
  api_key?: string;
  proxy_url?: string;
  models?: ProviderModelInput[];
  disabled?: boolean;
}

export const sharingPath = (path: string) => `/v8/hub${path}`;
export const sharingApi = {
  status: () => apiClient.get<{ needs_setup: boolean }>(sharingPath('/status')),
  me: () => apiClient.get<{ user: Account }>(sharingPath('/me')),
  login: (username: string, password: string) =>
    apiClient.post<{ user: Account; token: string }>(sharingPath('/login'), { username, password }),
  register: (body: { username: string; password: string; invite: string }) =>
    apiClient.post<{ user: Account }>(sharingPath('/register'), body),
  logout: () => apiClient.post(sharingPath('/logout')),
  channels: () => apiClient.get<{ channels: Channel[] }>(sharingPath('/channels')),
  createChannel: (body: { slug: string; public?: boolean }) =>
    apiClient.post<{ channel: Channel }>(sharingPath('/channels'), body),
  updateChannel: (
    id: string,
    body: { published?: boolean; announcement?: string; public?: boolean }
  ) => apiClient.patch(sharingPath(`/channels/${encodeURIComponent(id)}`), body),
  deleteChannel: (id: string) =>
    apiClient.delete(sharingPath(`/channels/${encodeURIComponent(id)}`)),
  credentialApiCall: (
    routeID: string,
    body: { method: string; url: string; header?: Record<string, string>; data?: string }
  ) =>
    apiClient.post<Record<string, unknown>>(
      sharingPath(`/credentials/${encodeURIComponent(routeID)}/api-call`),
      body
    ),
  boardMessages: (channelID: string, beforeID?: string, limit?: number) =>
    apiClient.get<{ messages: BoardMessage[] }>(
      sharingPath(`/channels/${encodeURIComponent(channelID)}/messages`),
      { params: { ...(beforeID ? { before_id: beforeID } : {}), ...(limit ? { limit } : {}) } }
    ),
  postBoardMessage: (channelID: string, content: string) =>
    apiClient.post<{ message: BoardMessage }>(
      sharingPath(`/channels/${encodeURIComponent(channelID)}/messages`),
      { content }
    ),
  deleteBoardMessage: (channelID: string, messageID: string) =>
    apiClient.delete(
      sharingPath(
        `/channels/${encodeURIComponent(channelID)}/messages/${encodeURIComponent(messageID)}`
      )
    ),
  createProvider: (channelID: string, body: ProviderPayload) =>
    apiClient.post<{ status: string; provider_id: string; prefix: string }>(
      sharingPath('/providers'),
      body,
      { params: { channel_id: channelID } }
    ),
  updateProvider: (routeID: string, body: ProviderPayload) =>
    apiClient.patch(sharingPath(`/providers/${encodeURIComponent(routeID)}`), body),
  discoverProviderModels: (base_url: string, api_key: string, proxy_url?: string) =>
    apiClient.post<{ models: string[] }>(sharingPath('/providers/discover'), {
      base_url,
      api_key,
      ...(proxy_url ? { proxy_url } : {}),
    }),
  marketplace: () =>
    apiClient.get<{ models: MarketplaceModel[]; channels: Channel[] }>(sharingPath('/marketplace')),
  marketplacePerformance: () =>
    apiClient.get<{ models: ModelPerformance[] }>(sharingPath('/marketplace/performance')),
  keys: () => apiClient.get<{ keys: PersonalKey[] }>(sharingPath('/keys')),
  createKey: (name: string) =>
    apiClient.post<{ key: PersonalKey; secret: string }>(sharingPath('/keys'), { name }),
  updateKey: (id: string, body: { name?: string; disabled?: boolean }) =>
    apiClient.patch(sharingPath(`/keys/${encodeURIComponent(id)}`), body),
  revokeKey: (id: string) => apiClient.delete(sharingPath(`/keys/${encodeURIComponent(id)}`)),
  users: () => apiClient.get<{ users: Account[] }>(sharingPath('/users')),
  updateUser: (id: string, body: { disabled?: boolean; role?: string }) =>
    apiClient.patch(sharingPath(`/users/${encodeURIComponent(id)}`), body),
  invites: () => apiClient.get<{ invites: Invitation[] }>(sharingPath('/invites')),
  createInvite: (max_uses: number, days: number) =>
    apiClient.post<{ invite: Invitation }>(sharingPath('/invites'), { max_uses, days }),
  revokeInvite: (id: string) => apiClient.delete(sharingPath(`/invites/${encodeURIComponent(id)}`)),
  password: (current_password: string, password: string) =>
    apiClient.post(sharingPath('/me/password'), { current_password, password }),
  usage: (params: Record<string, string | number>, signal?: AbortSignal) =>
    apiClient.get<HistoryPage<UsageEntry>>(sharingPath('/usage'), { params, signal }),
  calls: (params: Record<string, string | number>, signal?: AbortSignal) =>
    apiClient.get<HistoryPage<CallEntry>>(sharingPath('/calls'), { params, signal }),
  summary: (params: Record<string, string | number>, signal?: AbortSignal) =>
    apiClient.get<{ total: Summary; groups: Summary[] }>(sharingPath('/usage/summary'), {
      params,
      signal,
    }),
  audits: (params: Record<string, string | number>, signal?: AbortSignal) =>
    apiClient.get<HistoryPage<AuditEntry>>(sharingPath('/audit'), { params, signal }),
  credential: (routeID: string, body: Record<string, unknown>) =>
    apiClient.patch(sharingPath(`/credentials/${encodeURIComponent(routeID)}`), body),
  claim: (routeID: string, channel_id: string) =>
    apiClient.post(sharingPath(`/credentials/${encodeURIComponent(routeID)}/claim`), {
      channel_id,
    }),
};

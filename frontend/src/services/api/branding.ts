import { apiClient } from './client';

export interface BrandingInfo {
  siteName: string;
  siteSubtitle: string;
  siteLogo: string;
}

interface RawBranding {
  'site-name'?: unknown;
  'site-subtitle'?: unknown;
  'site-logo'?: unknown;
}

const asString = (value: unknown): string => (typeof value === 'string' ? value.trim() : '');

export const normalizeBranding = (raw: RawBranding | null | undefined): BrandingInfo => ({
  siteName: asString(raw?.['site-name']),
  siteSubtitle: asString(raw?.['site-subtitle']),
  siteLogo: asString(raw?.['site-logo']),
});

export const brandingApi = {
  /** Public endpoint, available before authentication (login page branding). */
  getPublicBranding: async (): Promise<BrandingInfo> =>
    normalizeBranding(await apiClient.get<RawBranding>('/v8/branding')),
  putBranding: (info: BrandingInfo): Promise<unknown> =>
    // Management endpoints resolve against apiBase, which already carries /v8/management.
    apiClient.put('/branding', {
      'site-name': info.siteName,
      'site-subtitle': info.siteSubtitle,
      'site-logo': info.siteLogo,
    }),
};

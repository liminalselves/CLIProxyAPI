/**
 * Site branding state.
 * Holds server-side branding overrides (site name, subtitle, logo) and applies
 * them to the document title and favicon. Empty values keep the defaults.
 */

import { create } from 'zustand';
import { brandingApi, type BrandingInfo } from '@/services/api/branding';
import { INLINE_LOGO_JPEG } from '@/assets/logoInline';

export const DEFAULT_BRAND_TITLE = 'CLI Proxy API Management Center';

const faviconTypeFor = (href: string): string => {
  if (href.startsWith('data:image/svg+xml') || href.endsWith('.svg')) return 'image/svg+xml';
  if (href.startsWith('data:image/png') || href.endsWith('.png')) return 'image/png';
  if (href.startsWith('data:image/webp') || href.endsWith('.webp')) return 'image/webp';
  return 'image/jpeg';
};

const applyDocumentBranding = (info: BrandingInfo) => {
  document.title = info.siteName || DEFAULT_BRAND_TITLE;
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (!link) return;
  link.href = info.siteLogo || INLINE_LOGO_JPEG;
  link.type = faviconTypeFor(link.href);
};

interface BrandingState extends BrandingInfo {
  loaded: boolean;
  fetchBranding: () => Promise<void>;
  setBranding: (info: BrandingInfo) => void;
}

export const useBrandingStore = create<BrandingState>((set) => ({
  siteName: '',
  siteSubtitle: '',
  siteLogo: '',
  loaded: false,

  fetchBranding: async () => {
    try {
      const info = await brandingApi.getPublicBranding();
      applyDocumentBranding(info);
      set({ ...info, loaded: true });
    } catch {
      // Older or unreachable backends keep the default branding.
      set({ loaded: true });
    }
  },

  setBranding: (info) => {
    applyDocumentBranding(info);
    set({ ...info, loaded: true });
  },
}));

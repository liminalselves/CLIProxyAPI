import { afterEach, beforeEach, describe, expect, spyOn, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import english from '../src/i18n/locales/en.json';
import { AccountLoginForm } from '../src/features/sharing/AccountLoginForm';
import { AccountPage } from '../src/features/sharing/AccountPage';
import { RequestStatus } from '../src/features/sharing/UsagePage';
import { obfuscatedStorage } from '../src/services/storage/secureStorage';
import { readFileSync } from 'node:fs';
import { apiClient } from '../src/services/api/client';
import { useAuthStore } from '../src/stores/useAuthStore';
import { sharingApi } from '../src/services/api/sharing';
import { sharingCredentials, clearSharedCredentials } from '../src/services/api/sharingCredentials';
import { authFilesApi } from '../src/services/api/authFiles';
import { oauthApi } from '../src/services/api/oauth';
import { useChannelStore } from '../src/features/sharing/useChannelStore';

const i18n = createInstance();
void i18n.init({ lng: 'en', initAsync: false, resources: { en: { translation: english } } });
const render = (element: ReturnType<typeof createElement>) =>
  renderToStaticMarkup(
    createElement(I18nextProvider, { i18n }, createElement(MemoryRouter, null, element))
  );

const account = {
  username: 'alice',
  role: 'member' as const,
  disabled: false,
  created_at: '2026-10-06T00:00:00Z',
};
const credential = {
  id: 'alice-gpt.json',
  route_id: 'opaque-id',
  name: 'alice-gpt.json',
  owner: 'alice',
  provider: 'codex',
  type: 'codex',
  channel_id: 'channel-a',
  channel: 'alice-gpt',
  channel_slug: 'alice-gpt',
  published: true,
  manageable: true,
  claimable: false,
  models: ['alice-gpt/gpt-5'],
};

let storageWrite: ReturnType<typeof spyOn>;
beforeEach(() => {
  storageWrite = spyOn(obfuscatedStorage, 'setItem').mockImplementation(() => {});
});
afterEach(() => {
  useAuthStore.setState({ account: null });
  useChannelStore.getState().clear();
  clearSharedCredentials();
  storageWrite.mockRestore();
});

describe('official frontend account integration', () => {
  test('uses the account namespace for account endpoints', async () => {
    const get = spyOn(apiClient, 'get').mockResolvedValue({ user: account });
    try {
      await sharingApi.me();
      expect(get).toHaveBeenCalledWith('/v8/hub/me');
    } finally {
      get.mockRestore();
    }
  });

  test('registration and channel creation send only the canonical identity', async () => {
    const post = spyOn(apiClient, 'post').mockResolvedValue({});
    const patch = spyOn(apiClient, 'patch').mockResolvedValue({});
    try {
      await sharingApi.register({ username: 'alice', password: 'password-alice', invite: '' });
      expect(post).toHaveBeenCalledWith('/v8/hub/register', {
        username: 'alice', password: 'password-alice', invite: '',
      });
      await sharingApi.createChannel({ slug: '573', public: true });
      expect(post).toHaveBeenCalledWith('/v8/hub/channels', { slug: '573', public: true });
      await sharingApi.updateChannel('channel-a', { public: false });
      expect(patch).toHaveBeenCalledWith('/v8/hub/channels/channel-a', { public: false });
    } finally {
      post.mockRestore();
      patch.mockRestore();
    }
  });

  test('registration has one name and account settings keep that name read-only', () => {
    const registration = render(createElement(AccountLoginForm, { apiBase: '', needsSetup: true }));
    expect(registration).toContain('name="username"');
    expect(registration).not.toContain('Display name');
    expect(registration).toContain('Cannot be changed after registration.');
    const settings = render(createElement(AccountPage));
    expect(settings).toMatch(/<input[^>]*readOnly=""/);
    expect(settings).not.toContain('Display name');
    expect(settings.match(/>Save<\/span>/g)).toHaveLength(1);
  });

  test('reuses auth file operations with owner-scoped paths', async () => {
    useAuthStore.setState({ account });
    const get = spyOn(apiClient, 'get').mockResolvedValue({ credentials: [credential] });
    const patch = spyOn(apiClient, 'patch').mockResolvedValue({});
    const post = spyOn(apiClient, 'post').mockResolvedValue({});
    try {
      const response = await authFilesApi.list();
      expect(response.files[0].owner).toBe('alice');
      await authFilesApi.setStatus(credential.name, true);
      expect(patch).toHaveBeenCalledWith('/v8/hub/credentials/opaque-id', { disabled: true });
      await authFilesApi.patchFields(credential.name, { note: 'owned setting' });
      expect(patch).toHaveBeenCalledWith('/v8/hub/credentials/opaque-id/fields', {
        note: 'owned setting',
      });
      await authFilesApi.requestManualRefresh(credential.name);
      expect(post).toHaveBeenCalledWith('/v8/hub/credentials/opaque-id/refresh');
      expect(await authFilesApi.getModelsForAuthFile(credential.name)).toEqual([
        { id: 'alice-gpt/gpt-5', type: 'codex', owned_by: 'alice' },
      ]);
    } finally {
      get.mockRestore();
      patch.mockRestore();
      post.mockRestore();
    }
  });

  test('refresh-all does not refresh other members credentials', async () => {
    const get = spyOn(apiClient, 'get').mockResolvedValue({
      credentials: [
        credential,
        {
          ...credential,
          id: 'bob.json',
          name: 'bob.json',
          route_id: 'bob-id',
          owner: 'bob',
          manageable: false,
        },
      ],
    });
    const post = spyOn(apiClient, 'post').mockResolvedValue({});
    try {
      const results = await sharingCredentials.refreshAll();
      expect(results).toEqual([{ id: credential.id, success: true }]);
      expect(post).toHaveBeenCalledTimes(1);
      expect(post).toHaveBeenCalledWith('/v8/hub/credentials/opaque-id/refresh');
    } finally {
      get.mockRestore();
      post.mockRestore();
    }
  });

  test('OAuth keeps the existing provider flow and adds the selected channel', async () => {
    useAuthStore.setState({ account });
    useChannelStore.getState().select('channel-a');
    const get = spyOn(apiClient, 'get').mockResolvedValue({
      state: 'state',
      url: 'https://example.test/login',
    });
    try {
      await oauthApi.startAuth('anthropic');
      expect(get).toHaveBeenCalledWith('/v8/hub/oauth/auth-url', {
        params: { provider: 'claude', channel_id: 'channel-a', is_webui: true },
      });
    } finally {
      get.mockRestore();
    }
  });

  test('does not substitute a second layout or a hard-coded color palette', () => {
    const routes = readFileSync('src/router/MainRoutes.tsx', 'utf8');
    expect(routes).toContain('<AuthFilesPage />');
    expect(routes).toContain('<OAuthPage />');
    const styles = readFileSync('src/features/sharing/Sharing.module.scss', 'utf8');
    expect(styles).toContain('var(--text-primary)');
    expect(styles).not.toMatch(/#[0-9a-fA-F]{3,8}\b/);
    const entry = readFileSync('src/App.tsx', 'utf8');
    expect(entry).toContain('<MainLayout />');
  });

  test('shows 499 as caller interruption in both lists and details in every locale', () => {
    for (const locale of ['zh-CN', 'zh-TW', 'en', 'ru']) {
      const dictionary = JSON.parse(readFileSync(`src/i18n/locales/${locale}.json`, 'utf8'));
      const translations = createInstance();
      void translations.init({
        lng: locale,
        initAsync: false,
        resources: { [locale]: { translation: dictionary } },
      });
      for (const showLabel of [false, true]) {
        for (const failed of [false, true]) {
          const markup = renderToStaticMarkup(
            createElement(
              I18nextProvider,
              { i18n: translations },
              createElement(RequestStatus, { status: 499, failed, showLabel })
            )
          );
          expect(markup).toContain(dictionary.sharing.status_interrupted);
          expect(markup).toContain(dictionary.sharing.status_interrupted_hint);
          expect(markup).toContain('499');
          expect(markup).not.toContain(dictionary.sharing.status_failed);
          expect(markup).not.toContain(dictionary.sharing.status_success);
        }
      }
    }
  });

  test('preserves success and service failure labels for non-interrupted requests', () => {
    const success = render(createElement(RequestStatus, { status: 200, showLabel: true }));
    expect(success).toContain('Success');
    expect(success).not.toContain('Request interrupted');
    for (const status of [502, 504]) {
      const failure = render(createElement(RequestStatus, { status, showLabel: true }));
      expect(failure).toContain('Failed');
      expect(failure).toContain(String(status));
      expect(failure).not.toContain('Request interrupted');
    }
    const failedOK = render(
      createElement(RequestStatus, { status: 200, failed: true, showLabel: true })
    );
    expect(failedOK).toContain('Failed');
  });

  test('includes every sharing label in the existing four locale catalogs', () => {
    const dictionaries = ['zh-CN', 'zh-TW', 'en', 'ru'].map(
      (locale) =>
        JSON.parse(readFileSync(`src/i18n/locales/${locale}.json`, 'utf8')).sharing as Record<
          string,
          string
        >
    );
    const keys = Object.keys(dictionaries[0]).sort();
    for (const dictionary of dictionaries) {
      expect(Object.keys(dictionary).sort()).toEqual(keys);
      for (const value of Object.values(dictionary)) expect(value.trim()).not.toBe('');
    }
  });
});

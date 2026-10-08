import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Button } from '@/components/ui/Button';
import { useBrandingStore, useNotificationStore } from '@/stores';
import { brandingApi, type BrandingInfo } from '@/services/api/branding';
import { INLINE_LOGO_JPEG } from '@/assets/logoInline';
import { DEFAULT_BRAND_TITLE } from '@/stores/useBrandingStore';
import styles from './ThemePage.module.scss';

const MAX_LOGO_FILE_SIZE = 2 * 1024 * 1024;

export function ThemePage() {
  const { t } = useTranslation();
  const notify = useNotificationStore((state) => state.showNotification);
  const siteName = useBrandingStore((state) => state.siteName);
  const siteSubtitle = useBrandingStore((state) => state.siteSubtitle);
  const siteLogo = useBrandingStore((state) => state.siteLogo);
  const loaded = useBrandingStore((state) => state.loaded);
  const fetchBranding = useBrandingStore((state) => state.fetchBranding);
  const setBranding = useBrandingStore((state) => state.setBranding);

  const [form, setForm] = useState<BrandingInfo>({ siteName, siteSubtitle, siteLogo });
  const [logoInput, setLogoInput] = useState(siteLogo.startsWith('http') ? siteLogo : '');
  const [initialized, setInitialized] = useState(false);
  const [saving, setSaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (!loaded) void fetchBranding();
  }, [loaded, fetchBranding]);

  useEffect(() => {
    if (!loaded || initialized) return;
    setForm({ siteName, siteSubtitle, siteLogo });
    setLogoInput(siteLogo.startsWith('http') ? siteLogo : '');
    setInitialized(true);
  }, [loaded, initialized, siteName, siteSubtitle, siteLogo]);

  const dirty =
    form.siteName !== siteName || form.siteSubtitle !== siteSubtitle || form.siteLogo !== siteLogo;

  const updateForm = (patch: Partial<BrandingInfo>) => setForm((prev) => ({ ...prev, ...patch }));

  const handleLogoFile = (file: File | undefined) => {
    if (!file) return;
    if (!file.type.startsWith('image/')) {
      notify(t('theme_page.logo_type_error'), 'error');
      return;
    }
    if (file.size > MAX_LOGO_FILE_SIZE) {
      notify(t('theme_page.logo_size_error'), 'error');
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      updateForm({ siteLogo: String(reader.result || '') });
      setLogoInput('');
    };
    reader.onerror = () => notify(t('theme_page.logo_read_error'), 'error');
    reader.readAsDataURL(file);
  };

  const handleLogoInputChange = (value: string) => {
    setLogoInput(value);
    const trimmed = value.trim();
    updateForm({ siteLogo: trimmed === '' ? '' : trimmed });
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const payload: BrandingInfo = {
        siteName: form.siteName.trim(),
        siteSubtitle: form.siteSubtitle.trim(),
        siteLogo: form.siteLogo.trim(),
      };
      await brandingApi.putBranding(payload);
      setBranding(payload);
      setForm(payload);
      notify(t('theme_page.saved'), 'success');
    } catch (error) {
      notify(
        `${t('theme_page.save_failed')}${error instanceof Error ? `: ${error.message}` : ''}`,
        'error'
      );
    } finally {
      setSaving(false);
    }
  };

  const handleReset = () => {
    setForm({ siteName: '', siteSubtitle: '', siteLogo: '' });
    setLogoInput('');
  };

  const previewLogo = form.siteLogo || INLINE_LOGO_JPEG;
  const previewTitle = form.siteName || t('title.abbr');
  const previewSubtitle = form.siteSubtitle || t('sidebar.subtitle');
  const previewTabTitle = form.siteName || DEFAULT_BRAND_TITLE;

  return (
    <div className={styles.container}>
      <h1 className={styles.pageTitle}>{t('theme_page.title')}</h1>
      <div className={styles.content}>
        <Card title={t('theme_page.brand_card')}>
          <p className={styles.cardDescription}>{t('theme_page.description')}</p>
          <Input
            label={t('theme_page.site_name')}
            placeholder={t('title.abbr')}
            value={form.siteName}
            maxLength={100}
            onChange={(event) => updateForm({ siteName: event.target.value })}
          />
          <Input
            label={t('theme_page.site_subtitle')}
            placeholder={t('sidebar.subtitle')}
            value={form.siteSubtitle}
            maxLength={200}
            onChange={(event) => updateForm({ siteSubtitle: event.target.value })}
          />
          <div className={styles.logoField}>
            <div className={styles.logoLabel}>{t('theme_page.site_logo')}</div>
            <div className={styles.logoRow}>
              <img src={previewLogo} alt="" className={styles.logoPreview} />
              <div className={styles.logoActions}>
                <Button size="sm" variant="secondary" onClick={() => fileInputRef.current?.click()}>
                  {t('theme_page.logo_upload')}
                </Button>
                {form.siteLogo ? (
                  <Button size="sm" variant="ghost" onClick={() => handleLogoInputChange('')}>
                    {t('theme_page.logo_remove')}
                  </Button>
                ) : null}
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/*"
                  hidden
                  onChange={(event) => {
                    handleLogoFile(event.target.files?.[0]);
                    event.target.value = '';
                  }}
                />
              </div>
            </div>
            <Input
              label={t('theme_page.logo_url')}
              placeholder="https://example.com/logo.png"
              value={logoInput}
              onChange={(event) => handleLogoInputChange(event.target.value)}
              hint={t('theme_page.logo_url_hint')}
            />
          </div>
        </Card>

        <Card title={t('theme_page.preview_card')}>
          <div className={styles.previewGroup}>
            <div className={styles.previewLabel}>{t('theme_page.preview_sidebar')}</div>
            <div className={styles.sidebarPreview}>
              <img src={previewLogo} alt="" className={styles.sidebarPreviewLogo} />
              <div className={styles.sidebarPreviewText}>
                <span className={styles.sidebarPreviewTitle}>{previewTitle}</span>
                <span className={styles.sidebarPreviewSubtitle}>{previewSubtitle}</span>
              </div>
            </div>
          </div>
          <div className={styles.previewGroup}>
            <div className={styles.previewLabel}>{t('theme_page.preview_tab')}</div>
            <div className={styles.tabPreviewFrame}>
              <div className={styles.tabPreview}>
                <img src={previewLogo} alt="" className={styles.tabPreviewIcon} />
                <span className={styles.tabPreviewTitle}>{previewTabTitle}</span>
              </div>
            </div>
          </div>
        </Card>
      </div>

      <div className={styles.actions}>
        <Button onClick={() => void handleSave()} loading={saving} disabled={!dirty || saving}>
          {t('common.save')}
        </Button>
        <Button variant="ghost" onClick={handleReset} disabled={!dirty && !form.siteLogo}>
          {t('theme_page.reset')}
        </Button>
      </div>
    </div>
  );
}

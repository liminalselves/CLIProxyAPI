import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Select } from '@/components/ui/Select';
import { useAuthStore } from '@/stores';
import { useChannelStore } from './useChannelStore';
import styles from './ChannelPicker.module.scss';

export function ChannelPicker() {
  const { t } = useTranslation();
  const account = useAuthStore((state) => state.account);
  const { channels, selected, select, load } = useChannelStore();
  const [error, setError] = useState('');
  const refresh = useCallback(async () => {
    try {
      await load();
      setError('');
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    }
  }, [load]);
  useEffect(() => {
    void refresh();
  }, [refresh]);
  if (!account) return null;
  const owned = channels.filter(
    (channel) => channel.owner === account.username || account.role === 'admin' || channel.public
  );
  return (
    <div className={`card ${styles.picker}`}>
      <div className="form-group">
        <label>{t('sharing.target_channel')}</label>
        <Select
          value={selected}
          onChange={select}
          ariaLabel={t('sharing.target_channel')}
          options={owned.map((channel) => ({
            value: channel.id,
            label: `${channel.slug}（${channel.owner}）`,
          }))}
          placeholder={t('sharing.select_channel')}
        />
        <p className="hint">
          {t('sharing.channel_upload_hint')} <Link to="/channels">{t('sharing.channels')}</Link>
        </p>
        {error && <div className="error-box">{error}</div>}
      </div>
    </div>
  );
}

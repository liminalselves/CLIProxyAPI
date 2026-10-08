import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Button } from '@/components/ui/Button';
import { useAuthStore, useNotificationStore } from '@/stores';
import { sharingApi } from '@/services/api/sharing';
import { PageHeader } from './shared';

import styles from './Sharing.module.scss';

export function AccountPage() {
  const { t } = useTranslation();
  const account = useAuthStore((state) => state.account);
  const logout = useAuthStore((state) => state.logout);
  const notify = useNotificationStore((state) => state.showNotification);
  const [current, setCurrent] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const savePassword = async () => {
    setLoading(true);
    try {
      await sharingApi.password(current, password);
      logout();
      notify(t('sharing.saved'), 'success');
    } catch (error) {
      notify(error instanceof Error ? error.message : String(error), 'error');
    } finally {
      setLoading(false);
    }
  };
  return (
    <div className={styles.page}>
      <PageHeader title={t('sharing.account')} />
      <div className={styles.split}>
        <Card title={t('sharing.profile')}>
          <Input
            label={t('sharing.username')}
            value={account?.username || ''}
            readOnly
            hint={t('sharing.username_hint')}
          />
        </Card>
        <Card title={t('sharing.change_password')}>
          <Input
            label={t('sharing.current_password')}
            type="password"
            value={current}
            onChange={(event) => setCurrent(event.target.value)}
            autoComplete="current-password"
          />
          <Input
            label={t('sharing.new_password')}
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="new-password"
            minLength={8}
          />
          <p className="hint">{t('sharing.password_hint')}</p>
          <Button loading={loading} onClick={() => void savePassword()}>
            {t('common.save')}
          </Button>
        </Card>
      </div>
    </div>
  );
}

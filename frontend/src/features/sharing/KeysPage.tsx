import { useCallback, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/Table';
import { sharingApi } from '@/services/api/sharing';
import { LoadState, PageHeader } from './shared';
import { useSharingActions, useSharingData, date } from './hooks';
import styles from './Sharing.module.scss';

export function KeysPage() {
  const { t } = useTranslation();
  const { data, loading, error, refresh } = useSharingData(
    useCallback(() => sharingApi.keys(), [])
  );
  const { run, confirm, copy } = useSharingActions(refresh);
  const [editing, setEditing] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [secret, setSecret] = useState('');
  const [saving, setSaving] = useState(false);
  const save = async () => {
    setSaving(true);
    await run(async () => {
      if (editing) await sharingApi.updateKey(editing, { name });
      else setSecret((await sharingApi.createKey(name)).secret);
      setEditing(null);
    });
    setSaving(false);
  };
  return (
    <div className={styles.page}>
      <PageHeader
        title={t('sharing.keys')}
        hint={t('sharing.keys_hint')}
        actions={
          <Button
            onClick={() => {
              setEditing('');
              setName('');
            }}
          >
            {t('sharing.create_key')}
          </Button>
        }
      />
      <LoadState loading={loading} error={error} />
      <Table>
        <TableHeader>
          <TableRow>
            {['name', 'key_hint', 'enabled', 'created', 'actions'].map((key) => (
              <TableHead key={key}>{t('sharing.' + key)}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {data?.keys.map((key) => (
            <TableRow key={key.id}>
              <TableCell>{key.name}</TableCell>
              <TableCell className={styles.mono}>{key.hint}</TableCell>
              <TableCell>
                <ToggleSwitch
                  checked={!key.disabled}
                  ariaLabel={key.name}
                  onChange={(value) =>
                    void run(() => sharingApi.updateKey(key.id, { disabled: !value }))
                  }
                />
              </TableCell>
              <TableCell>{date(key.created_at)}</TableCell>
              <TableCell>
                <div className={styles.actions}>
                  <Link to={'/usage?key_id=' + key.id} className="btn btn-secondary btn-sm">
                    {t('sharing.view_usage')}
                  </Link>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => {
                      setName(key.name);
                      setEditing(key.id);
                    }}
                  >
                    {t('common.edit')}
                  </Button>
                  <Button
                    variant="danger"
                    size="sm"
                    onClick={() =>
                      confirm(t('sharing.revoke_key_confirm'), () => sharingApi.revokeKey(key.id))
                    }
                  >
                    {t('sharing.revoke')}
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {!loading && !data?.keys.length && <p className="hint">{t('sharing.no_keys')}</p>}
      <Modal
        open={editing !== null}
        onClose={() => setEditing(null)}
        title={t(editing ? 'sharing.rename_key' : 'sharing.create_key')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setEditing(null)}>
              {t('common.cancel')}
            </Button>
            <Button loading={saving} disabled={!name.trim()} onClick={() => void save()}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Input
          label={t('sharing.name')}
          value={name}
          maxLength={64}
          onChange={(event) => setName(event.target.value)}
        />
      </Modal>
      <Modal
        open={Boolean(secret)}
        onClose={() => setSecret('')}
        title={t('sharing.save_key')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setSecret('')}>
              {t('common.close')}
            </Button>
            <Button onClick={() => void copy(secret)}>{t('common.copy')}</Button>
          </>
        }
      >
        <p className="hint">{t('sharing.secret_once')}</p>
        <div className={styles.secret}>{secret}</div>
      </Modal>
    </div>
  );
}

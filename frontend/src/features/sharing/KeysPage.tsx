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
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { LoadState, PageHeader } from './shared';
import { useSharingActions, useSharingData, date } from './hooks';
import { KeyModelFilterEditor } from './KeyModelFilterEditor';
import { normalizeKeyBlockedModels } from './keyFilters';
import type { PersonalKey } from './types';
import styles from './Sharing.module.scss';

export function KeysPage() {
  const { t } = useTranslation();
  const isMobile = useMediaQuery('(max-width: 768px)');
  const { data, loading, error, refresh } = useSharingData(
    useCallback(() => sharingApi.keys(), [])
  );
  const { run, confirm, copy } = useSharingActions(refresh);
  const [editing, setEditing] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [blockedModels, setBlockedModels] = useState<string[]>([]);
  const [secret, setSecret] = useState('');
  const [saving, setSaving] = useState(false);

  const editingKey = data?.keys.find((key) => key.id === editing) ?? null;
  const openCreate = () => {
    setEditing('');
    setName('');
    setBlockedModels([]);
  };
  const openEdit = (key: PersonalKey) => {
    setEditing(key.id);
    setName(key.name);
    setBlockedModels(key.blocked_models);
  };
  const filterSummary = (key: PersonalKey) =>
    key.blocked_models.length
      ? t('sharing.blocked_models_count', { count: key.blocked_models.length })
      : t('sharing.blocked_models_all');
  const save = async () => {
    setSaving(true);
    await run(async () => {
      if (editing) {
        await sharingApi.updateKey(editing, {
          name: name.trim(),
          blocked_models: normalizeKeyBlockedModels(blockedModels),
        });
      } else {
        setSecret((await sharingApi.createKey(name.trim())).secret);
      }
      setEditing(null);
    });
    setSaving(false);
  };

  return (
    <div className={styles.page}>
      <PageHeader
        title={t('sharing.keys')}
        hint={t('sharing.keys_hint')}
        actions={<Button onClick={openCreate}>{t('sharing.create_key')}</Button>}
      />
      <LoadState loading={loading} error={error} />
      {isMobile ? (
        <div className={styles.keyCards}>
          {data?.keys.map((key) => (
            <div key={key.id} className={styles.keyCard}>
              <div className={styles.keyCardHead}>
                <span className={styles.cellPrimary}>{key.name || key.hint}</span>
                <ToggleSwitch
                  checked={!key.disabled}
                  ariaLabel={key.name || key.hint}
                  onChange={(value) =>
                    void run(() => sharingApi.updateKey(key.id, { disabled: !value }))
                  }
                />
              </div>
              {key.name && (
                <span className={`${styles.cellSecondary} ${styles.mono}`}>{key.hint}</span>
              )}
              <span className={styles.cellSecondary}>
                {t('sharing.created')}：{date(key.created_at)}
              </span>
              <div className={styles.keyCardBalance}>
                <span className={styles.cellSecondary}>{t('sharing.auto_balance')}</span>
                <ToggleSwitch
                  checked={key.auto_balance}
                  ariaLabel={t('sharing.auto_balance')}
                  onChange={(value) =>
                    void run(() => sharingApi.updateKey(key.id, { auto_balance: value }))
                  }
                />
              </div>
              <div className={styles.keyCardBalance}>
                <span className={styles.cellSecondary}>{t('sharing.blocked_models')}</span>
                <span
                  className={
                    key.blocked_models.length ? styles.keyFilterActive : styles.cellSecondary
                  }
                >
                  {filterSummary(key)}
                </span>
              </div>
              <div className={styles.keyCardActions}>
                <Link to={'/usage?key_id=' + key.id} className="btn btn-secondary btn-sm">
                  {t('sharing.view_usage')}
                </Link>
                <Button variant="secondary" size="sm" onClick={() => openEdit(key)}>
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
            </div>
          ))}
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              {[
                'name',
                'key_hint',
                'enabled',
                'auto_balance',
                'blocked_models',
                'created',
                'actions',
              ].map((key) => (
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
                <TableCell>
                  <ToggleSwitch
                    checked={key.auto_balance}
                    ariaLabel={t('sharing.auto_balance') + ' ' + key.name}
                    onChange={(value) =>
                      void run(() => sharingApi.updateKey(key.id, { auto_balance: value }))
                    }
                  />
                </TableCell>
                <TableCell>
                  <span
                    className={
                      key.blocked_models.length ? styles.keyFilterActive : styles.cellSecondary
                    }
                  >
                    {filterSummary(key)}
                  </span>
                </TableCell>
                <TableCell>{date(key.created_at)}</TableCell>
                <TableCell>
                  <div className={styles.actions}>
                    <Link to={'/usage?key_id=' + key.id} className="btn btn-secondary btn-sm">
                      {t('sharing.view_usage')}
                    </Link>
                    <Button variant="secondary" size="sm" onClick={() => openEdit(key)}>
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
      )}
      {!loading && !data?.keys.length && <p className="hint">{t('sharing.no_keys')}</p>}
      {!loading && Boolean(data?.keys.length) && (
        <p className="hint">{t('sharing.auto_balance_hint')}</p>
      )}
      <Modal
        open={editing !== null}
        onClose={() => setEditing(null)}
        title={t(editing ? 'sharing.edit_key' : 'sharing.create_key')}
        width={editing ? 760 : 520}
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
        {editingKey && <KeyModelFilterEditor value={blockedModels} onChange={setBlockedModels} />}
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

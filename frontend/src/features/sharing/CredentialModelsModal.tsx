import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { ModelEntriesEditor } from '@/features/providers/sheets/forms/ModelEntriesEditor';
import { sharingCredentials, type SharedCredential } from '@/services/api/sharingCredentials';
import {
  buildSharedCredentialModelAliases,
  readSharedCredentialModels,
} from '@/services/api/sharedCredentialModels';
import { useSharingActions } from './hooks';
import styles from './Sharing.module.scss';

export function CredentialModelsModal({
  file,
  onClose,
  refresh,
}: {
  file: SharedCredential;
  onClose: () => void;
  refresh: () => Promise<void>;
}) {
  const { t } = useTranslation();
  const { run } = useSharingActions(refresh);
  const [rows, setRows] = useState(() => readSharedCredentialModels(file));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const editable = file.manageable && file.kind !== 'provider';

  const save = async () => {
    if (!rows || saving) return;
    let aliases: ReturnType<typeof buildSharedCredentialModelAliases>;
    try {
      aliases = buildSharedCredentialModelAliases(rows);
      setError('');
    } catch {
      setError(t('auth_files.policy_aliases_invalid'));
      return;
    }
    setSaving(true);
    try {
      if (await run(() => sharingCredentials.patch(file.name, { model_aliases: aliases })))
        onClose();
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      open
      onClose={onClose}
      closeDisabled={saving}
      title={t('auth_files.models_button')}
      width={640}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={saving}>
            {t(editable ? 'common.cancel' : 'common.close')}
          </Button>
          {editable && (
            <Button onClick={() => void save()} loading={saving} disabled={rows === null}>
              {t('common.save')}
            </Button>
          )}
        </>
      }
    >
      {editable ? (
        rows === null ? (
          <p className="hint">{t('auth_files.policy_unsupported')}</p>
        ) : (
          <>
            <p className="hint">{t('sharing.credential_models_hint')}</p>
            <ModelEntriesEditor
              oauthAliasOnly
              aliasRequired={false}
              models={rows}
              supportsImage={false}
              supportsThinking={false}
              mutating={saving}
              removeDisabled={false}
              onUpdate={(index, patch) =>
                setRows(rows.map((row, i) => (i === index ? { ...row, ...patch } : row)))
              }
              onAdd={() => setRows([...rows, { name: '', alias: '' }])}
              onRemove={(index) => setRows(rows.filter((_, i) => i !== index))}
            />
            {error && (
              <div className="error-box" role="alert">
                {error}
              </div>
            )}
          </>
        )
      ) : file.models.length === 0 ? (
        <p className={styles.meta}>{t('sharing.no_models')}</p>
      ) : (
        <ul className={styles.modelList}>
          {file.models.map((model) => (
            <li key={model} className={styles.mono}>
              {model}
            </li>
          ))}
        </ul>
      )}
    </Modal>
  );
}

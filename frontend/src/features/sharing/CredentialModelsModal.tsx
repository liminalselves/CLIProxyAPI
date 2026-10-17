import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { IconRefreshCw } from '@/components/ui/icons';
import { ModelEntriesEditor } from '@/features/providers/sheets/forms/ModelEntriesEditor';
import type { ProviderModelCapabilities } from '@/features/providers/descriptors';
import type { ModelEntryInput } from '@/features/providers/types';
import {
  credentialModelManagement,
  isCredentialModelsUnsupported,
  type CredentialModelManagement,
  type CredentialModelTarget,
} from '@/services/api/credentialModelManagement';
import { authFilesApi } from '@/services/api/authFiles';
import type { SharedCredential } from '@/services/api/sharingCredentials';
import { useNotificationStore } from '@/stores';
import { useAuthStore } from '@/stores/useAuthStore';
import {
  credentialModelsToRows,
  rowsToCredentialModels,
  validateCredentialModelRows,
} from './credentialModelRows';
import { useSharingActions } from './hooks';
import styles from './Sharing.module.scss';

const stringList = (value: unknown): string[] =>
  Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];

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
  const { run, confirm } = useSharingActions(refresh);
  const { showNotification } = useNotificationStore();
  const account = useAuthStore((state) => state.account);
  const target = useMemo<CredentialModelTarget>(() => {
    const rawIndex = file.authIndex ?? file['auth_index'];
    return {
      name: file.name,
      routeId: file.route_id,
      authIndex:
        typeof rawIndex === 'string' || typeof rawIndex === 'number' ? rawIndex : undefined,
    };
  }, [file]);
  const [payload, setPayload] = useState<CredentialModelManagement | null>(null);
  const [rows, setRows] = useState<ModelEntryInput[]>([]);
  const [dirty, setDirty] = useState(false);
  const [status, setStatus] = useState<'loading' | 'ready' | 'unsupported' | 'failed'>('loading');
  const [readonlyModels, setReadonlyModels] = useState<string[] | null>(null);
  const [loadError, setLoadError] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [discovering, setDiscovering] = useState(false);
  const nameListId = 'credential-model-names';

  // The hub route enforces ownership itself; read-only stays for unmanaged files.
  const editable = account ? file.manageable && file.kind !== 'provider' : true;

  const loadPayload = async (cancelled: () => boolean) => {
    setStatus('loading');
    setLoadError('');
    try {
      const next = await credentialModelManagement.get(target);
      if (cancelled()) return;
      setPayload(next);
      setRows(credentialModelsToRows(next.models));
      setDirty(false);
      setStatus('ready');
    } catch (err) {
      if (cancelled()) return;
      if (isCredentialModelsUnsupported(err)) {
        setStatus('unsupported');
      } else {
        setLoadError(err instanceof Error ? err.message : String(err));
        setStatus('failed');
      }
    }
  };

  useEffect(() => {
    if (!editable) return;
    let cancelled = false;
    void loadPayload(() => cancelled);
    return () => {
      cancelled = true;
    };
    // The modal mounts once per selected credential.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Credential kinds without model management keep the legacy read-only list.
  useEffect(() => {
    if (status !== 'unsupported' || readonlyModels !== null) return;
    let cancelled = false;
    authFilesApi
      .getModelsForAuthFile(file.name)
      .then((models) => {
        if (!cancelled) setReadonlyModels(models.map((model) => model.id));
      })
      .catch(() => {
        if (!cancelled) setReadonlyModels([]);
      });
    return () => {
      cancelled = true;
    };
  }, [status, readonlyModels, file.name]);

  const editorCapabilities: ProviderModelCapabilities | undefined = payload
    ? {
        maxContextLength: payload.capabilities.max_context_length,
        isCompat: payload.capabilities.is_compat,
        configurationUpdate: payload.capabilities.support_configuration_update,
        modalities: payload.capabilities.modalities,
        maxCompletionTokens: false,
      }
    : undefined;

  const updateRows = (next: (current: ModelEntryInput[]) => ModelEntryInput[]) => {
    setDirty(true);
    setRows((current) => next(current));
  };

  const save = async () => {
    if (!payload || saving) return;
    const validation = validateCredentialModelRows(
      rows,
      payload.capabilities,
      payload.available_models
    );
    if (validation) {
      setError(
        t(validation.key, 'name' in validation ? { name: validation.name } : undefined)
      );
      return;
    }
    setError('');
    setSaving(true);
    try {
      const models = rowsToCredentialModels(rows, payload.capabilities);
      const saved = await run(async () => {
        setPayload(await credentialModelManagement.replace(target, models));
      });
      if (saved) onClose();
    } finally {
      setSaving(false);
    }
  };

  const reset = () => {
    if (!payload?.capabilities.reset || saving) return;
    confirm(t('sharing.credential_models_reset_confirm'), async () => {
      setPayload(await credentialModelManagement.reset(target));
      onClose();
    });
  };

  const discover = async () => {
    if (!payload?.source.supports_refresh || discovering) return;
    setDiscovering(true);
    try {
      const next = await credentialModelManagement.discover(target);
      setPayload(next);
      // Keep unsaved edits; a refreshed catalog only replaces untouched rows.
      if (!dirty) setRows(credentialModelsToRows(next.models));
      showNotification(t('sharing.credential_models_refreshed'), 'success');
    } catch (err) {
      showNotification(err instanceof Error ? err.message : String(err), 'error');
    } finally {
      setDiscovering(false);
    }
  };

  const fallbackModels =
    readonlyModels ?? (Array.isArray(file.models) ? file.models : []);
  const enabledFilter = payload ? stringList(payload.enabled_models) : [];
  const excludedFilter = payload ? stringList(payload.excluded_models) : [];
  const hours = payload ? Math.round(payload.source.refresh_interval_seconds / 3600) : 0;

  return (
    <Modal
      open
      onClose={onClose}
      closeDisabled={saving}
      title={t('auth_files.models_button')}
      width={640}
      footer={
        editable && status === 'ready' ? (
          <div className={styles.modelsFooter}>
            <div className={styles.modelsFooterSide}>
              {payload?.configured && payload.capabilities.reset ? (
                <Button variant="secondary" onClick={reset} disabled={saving || discovering}>
                  {t('sharing.credential_models_reset')}
                </Button>
              ) : null}
            </div>
            <div className={styles.modelsFooterSide}>
              <Button variant="secondary" onClick={onClose} disabled={saving}>
                {t('common.cancel')}
              </Button>
              <Button onClick={() => void save()} loading={saving} disabled={discovering}>
                {t('common.save')}
              </Button>
            </div>
          </div>
        ) : (
          <Button variant="secondary" onClick={onClose} disabled={saving}>
            {t('common.close')}
          </Button>
        )
      }
    >
      {!editable || status === 'unsupported' ? (
        <>
          <p className="hint">
            {t(
              status === 'unsupported'
                ? 'sharing.credential_models_unsupported'
                : 'auth_files.policy_unsupported'
            )}
          </p>
          {fallbackModels.length > 0 && (
            <ul className={styles.modelList}>
              {fallbackModels.map((model) => (
                <li key={model} className={styles.mono}>
                  {model}
                </li>
              ))}
            </ul>
          )}
        </>
      ) : status === 'loading' ? (
        <p className="hint">{t('auth_files.models_loading', { defaultValue: '正在加载模型列表...' })}</p>
      ) : status === 'failed' ? (
        <>
          <div className="error-box" role="alert">
            {loadError}
          </div>
          <Button variant="secondary" size="sm" onClick={() => void loadPayload(() => false)}>
            {t('common.retry', { defaultValue: '重试' })}
          </Button>
        </>
      ) : payload ? (
        <>
          <div className={styles.modelsSource}>
            <span className={styles.badge}>
              {t(`sharing.credential_models_source_${payload.source.kind}`)}
            </span>
            {payload.source.auto_refresh && hours > 0 && (
              <span className={styles.modelsSourceMeta}>
                {t('sharing.credential_models_auto_refresh', { hours })}
              </span>
            )}
            <span className={styles.modelsSourceMeta}>
              {t(
                payload.configured
                  ? 'sharing.credential_models_configured'
                  : 'sharing.credential_models_inherited'
              )}
            </span>
            {payload.source.supports_refresh && (
              <button
                type="button"
                className={styles.modelsRefreshBtn}
                onClick={() => void discover()}
                disabled={discovering || saving}
                title={t('sharing.credential_models_refresh')}
                aria-label={t('sharing.credential_models_refresh')}
              >
                <IconRefreshCw size={13} className={discovering ? styles.spinning : undefined} />
              </button>
            )}
          </div>
          {!payload.catalog_available && (
            <p className={styles.modelsWarning}>{t('sharing.credential_models_catalog_missing')}</p>
          )}
          {!payload.capabilities.manual_models && (
            <p className="hint">{t('sharing.credential_models_account_only')}</p>
          )}
          {enabledFilter.length > 0 && (
            <p className="hint">
              {t('sharing.credential_models_enabled_filter', { list: enabledFilter.join('、') })}
            </p>
          )}
          {excludedFilter.length > 0 && (
            <p className="hint">
              {t('sharing.credential_models_excluded_filter', { list: excludedFilter.join('、') })}
            </p>
          )}
          <p className="hint">{t('sharing.credential_models_hint')}</p>
          <datalist id={nameListId}>
            {payload.available_models.map((model) => (
              <option key={model.id} value={model.id} />
            ))}
          </datalist>
          <ModelEntriesEditor
            aliasRequired={false}
            models={rows}
            capabilities={editorCapabilities}
            supportsImage={false}
            supportsThinking={payload.capabilities.thinking}
            mutating={saving || discovering}
            removeDisabled={false}
            nameListId={nameListId}
            addDisabled={!payload.capabilities.manual_models}
            onUpdate={(index, patch) =>
              updateRows((current) =>
                current.map((row, i) => (i === index ? { ...row, ...patch } : row))
              )
            }
            onAdd={() => updateRows((current) => [...current, { name: '', alias: '' }])}
            onRemove={(index) => updateRows((current) => current.filter((_, i) => i !== index))}
          />
          {error && (
            <div className="error-box" role="alert">
              {error}
            </div>
          )}
        </>
      ) : null}
    </Modal>
  );
}

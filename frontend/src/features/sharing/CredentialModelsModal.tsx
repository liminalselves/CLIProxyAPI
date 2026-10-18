import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { IconDownload } from '@/components/ui/icons';
import { ModelDiscoveryPanel } from '@/features/providers/sheets/forms/ModelDiscoveryPanel';
import { ModelEntriesEditor } from '@/features/providers/sheets/forms/ModelEntriesEditor';
import formStyles from '@/features/providers/sheets/forms/sharedForm.module.scss';
import type { ProviderModelCapabilities } from '@/features/providers/descriptors';
import type { ModelEntryInput } from '@/features/providers/types';
import type { ModelInfo } from '@/utils/models';
import {
  credentialModelManagement,
  isCredentialModelsUnsupported,
  type CredentialModelManagement,
  type CredentialModelTarget,
} from '@/services/api/credentialModelManagement';
import { authFilesApi } from '@/services/api/authFiles';
import type { SharedCredential } from '@/services/api/sharingCredentials';
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
  const { run } = useSharingActions(refresh);
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
  const [status, setStatus] = useState<'loading' | 'ready' | 'unsupported' | 'failed'>('loading');
  const [readonlyModels, setReadonlyModels] = useState<string[] | null>(null);
  const [loadError, setLoadError] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [discovering, setDiscovering] = useState(false);
  const [discoveryOpen, setDiscoveryOpen] = useState(false);
  const [hasDiscovered, setHasDiscovered] = useState(false);
  const [discoverError, setDiscoverError] = useState<string | null>(null);
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

  const canDiscover = Boolean(payload?.source.supports_refresh);

  const discoveryModels = useMemo<ModelInfo[]>(
    () => (payload?.available_models ?? []).map((model) => ({ name: model.id })),
    [payload]
  );

  const existingRowNames = useMemo(
    () => new Set(rows.map((row) => row.name.trim()).filter(Boolean)),
    [rows]
  );

  // Discovery refreshes the server-side source catalog and returns fresh
  // available models; it never touches the rows being edited.
  const discover = async () => {
    if (!canDiscover || discovering) return;
    setDiscovering(true);
    setDiscoverError(null);
    try {
      const next = await credentialModelManagement.discover(target);
      setPayload(next);
      setHasDiscovered(true);
    } catch (err) {
      setDiscoverError(err instanceof Error ? err.message : String(err));
      setHasDiscovered(true);
    } finally {
      setDiscovering(false);
    }
  };

  const applyPicked = (picked: ModelInfo[]) => {
    if (!picked.length) return;
    updateRows((current) => {
      const existing = new Set(current.map((row) => row.name.trim()).filter(Boolean));
      const additions = picked
        .filter((model) => !existing.has(model.name))
        .map((model) => ({ name: model.name, alias: '' }));
      return additions.length ? [...current, ...additions] : current;
    });
  };

  const fallbackModels =
    readonlyModels ?? (Array.isArray(file.models) ? file.models : []);
  const enabledFilter = payload ? stringList(payload.enabled_models) : [];
  const excludedFilter = payload ? stringList(payload.excluded_models) : [];

  return (
    <Modal
      open
      onClose={onClose}
      closeDisabled={saving}
      title={t('auth_files.models_button')}
      width={640}
      footer={
        editable && status === 'ready' ? (
          <>
            <Button variant="secondary" onClick={onClose} disabled={saving}>
              {t('common.cancel')}
            </Button>
            <Button onClick={() => void save()} loading={saving} disabled={discovering}>
              {t('common.save')}
            </Button>
          </>
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
        <div className={formStyles.form}>
          <p className="hint">{t('sharing.credential_models_hint')}</p>
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
          <datalist id={nameListId}>
            {payload.available_models.map((model) => (
              <option key={model.id} value={model.id} />
            ))}
          </datalist>
          <div className={formStyles.entriesList}>
            {canDiscover ? (
              <div className={formStyles.entriesToolbar}>
              <button
                type="button"
                className={formStyles.connectivityBtn}
                onClick={() => setDiscoveryOpen((open) => !open)}
                disabled={saving || discovering}
                aria-expanded={discoveryOpen}
              >
                  <IconDownload size={14} />
                  <span>{t('sharing.credential_models_fetch')}</span>
                </button>
              </div>
            ) : null}
            {canDiscover && discoveryOpen ? (
              <ModelDiscoveryPanel
                loading={discovering}
                error={discoverError}
                models={discoveryModels}
                hasFetched={hasDiscovered || payload.catalog_available}
                existingNames={existingRowNames}
                mutating={saving}
                onApply={applyPicked}
                onReload={() => void discover()}
                onClose={() => setDiscoveryOpen(false)}
              />
            ) : null}
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
          </div>
          {error && (
            <div className="error-box" role="alert">
              {error}
            </div>
          )}
        </div>
      ) : null}
    </Modal>
  );
}

import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/Input';
import { SelectionCheckbox } from '@/components/ui/SelectionCheckbox';
import { sharingApi } from '@/services/api/sharing';
import { LoadState } from './shared';
import { useSharingData } from './hooks';
import { groupMarketplaceModels } from './marketplace';
import styles from './Sharing.module.scss';

interface KeyModelFilterEditorProps {
  value: string[];
  onChange: (value: string[]) => void;
}

export function KeyModelFilterEditor({ value, onChange }: KeyModelFilterEditorProps) {
  const { t } = useTranslation();
  const { data, loading, error } = useSharingData(useCallback(() => sharingApi.marketplace(), []));
  const [search, setSearch] = useState('');

  const selected = useMemo(
    () => new Set(value.map((model) => model.trim()).filter(Boolean)),
    [value]
  );
  const models = useMemo(() => data?.models ?? [], [data?.models]);
  const groups = useMemo(() => groupMarketplaceModels(models, { search }), [models, search]);
  const staleModels = useMemo(() => {
    const query = search.trim().toLowerCase();
    const knownRoutes = new Set(models.map((model) => model.id));
    const knownBases = new Set(models.map((model) => model.base_model));
    return [...selected]
      .filter((model) => !knownRoutes.has(model) && !knownBases.has(model))
      .filter((model) => !query || model.toLowerCase().includes(query))
      .sort((left, right) => left.localeCompare(right, undefined, { numeric: true }));
  }, [models, search, selected]);

  const setBlocked = (model: string, blocked: boolean) => {
    const next = new Set(selected);
    if (blocked) next.add(model);
    else next.delete(model);
    onChange([...next]);
  };

  // Single-route groups are one merged row; blocking writes the bare model so
  // channels added later stay covered, and clears any stale route-level entry.
  const setSoloBlocked = (baseModel: string, routeId: string, blocked: boolean) => {
    const next = new Set(selected);
    next.delete(routeId);
    if (blocked) next.add(baseModel);
    else next.delete(baseModel);
    onChange([...next]);
  };

  if (loading || error) {
    return (
      <section className={styles.keyFilter}>
        <LoadState loading={loading} error={error} />
      </section>
    );
  }

  return (
    <section className={styles.keyFilter} aria-labelledby="key-model-filter-title">
      <div className={styles.keyFilterHeader}>
        <div>
          <h2 id="key-model-filter-title">{t('sharing.blocked_models')}</h2>
          <p>{t('sharing.key_filter_hint')}</p>
        </div>
        <span className={styles.badge}>
          {selected.size
            ? t('sharing.key_filter_selected', { count: selected.size })
            : t('sharing.blocked_models_all')}
        </span>
      </div>
      <Input
        label={t('sharing.search_models')}
        value={search}
        onChange={(event) => setSearch(event.target.value)}
        placeholder={t('sharing.search_models')}
      />
      {groups.length === 0 && staleModels.length === 0 ? (
        <p className={styles.keyFilterEmpty}>{t('sharing.key_filter_empty')}</p>
      ) : (
        <div className={styles.keyFilterList}>
          {groups.map((group) => {
            const allBlocked = selected.has(group.baseModel);
            const solo = group.routes.length === 1 ? group.routes[0] : null;
            if (solo) {
              const blocked = allBlocked || selected.has(solo.id);
              const source = t('sharing.key_filter_source', {
                channel: solo.channel_slug || solo.channel,
                owner: solo.owner,
                provider: solo.provider,
              });
              return (
                <section className={styles.keyFilterGroup} key={group.baseModel}>
                  <SelectionCheckbox
                    className={styles.keyFilterSolo}
                    checked={blocked}
                    onChange={(value) => setSoloBlocked(group.baseModel, solo.id, value)}
                    ariaLabel={t('sharing.key_filter_block_group', { model: group.baseModel })}
                    title={t('sharing.key_filter_all_channels_hint')}
                    label={
                      <span className={styles.keyFilterRouteLabel}>
                        <span className={styles.keyFilterSoloModel}>{group.baseModel}</span>
                        <span className={styles.keyFilterRouteMeta}>
                          {solo.id !== group.baseModel && (
                            <span className={styles.mono}>{solo.id}</span>
                          )}
                          <span>{source}</span>
                          {!solo.available && (
                            <span className={styles.keyFilterUnavailable}>
                              {t('sharing.key_filter_unavailable')}
                            </span>
                          )}
                        </span>
                      </span>
                    }
                  />
                </section>
              );
            }
            return (
              <section className={styles.keyFilterGroup} key={group.baseModel}>
                <div className={styles.keyFilterGroupHeader}>
                  <SelectionCheckbox
                    checked={allBlocked}
                    onChange={(blocked) => setBlocked(group.baseModel, blocked)}
                    ariaLabel={t('sharing.key_filter_block_group', { model: group.baseModel })}
                    title={t('sharing.key_filter_all_channels_hint')}
                    label={
                      <span className={styles.keyFilterGroupTitle}>
                        <span className={styles.mono}>{group.baseModel}</span>
                        <span className={styles.keyFilterMeta}>
                          {t('sharing.key_filter_all_channels')}
                        </span>
                      </span>
                    }
                  />
                </div>
                <div className={styles.keyFilterRoutes}>
                  {group.routes.map((route) => {
                    const blocked = allBlocked || selected.has(route.id);
                    const source = t('sharing.key_filter_source', {
                      channel: route.channel_slug || route.channel,
                      owner: route.owner,
                      provider: route.provider,
                    });
                    return (
                      <SelectionCheckbox
                        key={route.id}
                        className={styles.keyFilterRoute}
                        checked={blocked}
                        disabled={allBlocked}
                        onChange={(value) => setBlocked(route.id, value)}
                        ariaLabel={t('sharing.key_filter_block_route', { model: route.id })}
                        label={
                          <span className={styles.keyFilterRouteLabel}>
                            <span className={styles.keyFilterRouteId}>{route.id}</span>
                            <span className={styles.keyFilterRouteMeta}>
                              {source}
                              {!route.available && (
                                <span className={styles.keyFilterUnavailable}>
                                  {t('sharing.key_filter_unavailable')}
                                </span>
                              )}
                            </span>
                          </span>
                        }
                      />
                    );
                  })}
                </div>
              </section>
            );
          })}
          {staleModels.length > 0 && (
            <section className={styles.keyFilterGroup}>
              <div className={styles.keyFilterGroupHeader}>
                <div>
                  <strong>{t('sharing.key_filter_saved_unavailable')}</strong>
                  <p>{t('sharing.key_filter_saved_unavailable_hint')}</p>
                </div>
              </div>
              <div className={styles.keyFilterRoutes}>
                {staleModels.map((model) => (
                  <SelectionCheckbox
                    key={model}
                    className={styles.keyFilterRoute}
                    checked
                    onChange={(blocked) => setBlocked(model, blocked)}
                    ariaLabel={t('sharing.key_filter_block_route', { model })}
                    label={
                      <span className={styles.keyFilterRouteLabel}>
                        <span className={styles.keyFilterRouteId}>{model}</span>
                        <span className={styles.keyFilterRouteMeta}>
                          {model.includes('/')
                            ? t('sharing.key_filter_unavailable')
                            : t('sharing.key_filter_all_channels')}
                        </span>
                      </span>
                    }
                  />
                ))}
              </div>
            </section>
          )}
        </div>
      )}
    </section>
  );
}

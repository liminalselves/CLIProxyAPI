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
  const channels = useMemo(() => data?.channels ?? [], [data?.channels]);
  const groups = useMemo(() => groupMarketplaceModels(models, { search }), [models, search]);

  // Channel prefixes for scope entries: the pinned prefix ("123-kimi#1",
  // "123-provider") blocks one credential or provider, the channel prefix
  // ("123") blocks the whole channel.
  const channelPrefixByID = useMemo(
    () => new Map(channels.map((channel) => [channel.id, channel.prefix])),
    [channels]
  );

  const query = search.trim().toLowerCase();
  const staleModels = useMemo(() => {
    const known = new Set<string>();
    for (const model of models) {
      known.add(model.id);
      known.add(model.base_model);
      known.add(model.channel);
    }
    for (const channel of channels) known.add(channel.prefix);
    return [...selected]
      .filter((model) => !known.has(model))
      .filter((model) => !query || model.toLowerCase().includes(query))
      .sort((left, right) => left.localeCompare(right, undefined, { numeric: true }));
  }, [models, channels, query, selected]);

  // One row per distinct credential/provider prefix inside each channel.
  const channelScopes = useMemo(
    () =>
      channels
        .map((channel) => {
          const scopes = new Map<string, { provider: string; count: number }>();
          for (const model of models) {
            if (model.channel_id !== channel.id) continue;
            const scope = scopes.get(model.channel) ?? { provider: model.provider, count: 0 };
            scope.count += 1;
            scopes.set(model.channel, scope);
          }
          const prefixes = [...scopes.entries()]
            .map(([prefix, meta]) => ({ prefix, ...meta }))
            .sort((left, right) => left.prefix.localeCompare(right.prefix, undefined, { numeric: true }));
          return { channel, prefixes };
        })
        .filter(
          ({ channel, prefixes }) =>
            !query ||
            channel.slug.toLowerCase().includes(query) ||
            channel.owner.toLowerCase().includes(query) ||
            prefixes.some((scope) => scope.prefix.toLowerCase().includes(query))
        ),
    [channels, models, query]
  );

  const setBlocked = (entry: string, blocked: boolean) => {
    const next = new Set(selected);
    if (blocked) next.add(entry);
    else next.delete(entry);
    onChange([...next]);
  };

  const scopeBlocked = (channelID: string, prefix: string) =>
    selected.has(prefix) || selected.has(channelPrefixByID.get(channelID) ?? '');

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
      {groups.length === 0 && channelScopes.length === 0 && staleModels.length === 0 ? (
        <p className={styles.keyFilterEmpty}>{t('sharing.key_filter_empty')}</p>
      ) : (
        <div className={styles.keyFilterList}>
          {groups.map((group) => {
            const allBlocked = selected.has(group.baseModel);
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
                    const scoped = scopeBlocked(route.channel_id, route.channel);
                    const blocked = allBlocked || scoped || selected.has(route.id);
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
                        disabled={allBlocked || scoped}
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
          {channelScopes.map(({ channel, prefixes }) => {
            const channelBlocked = selected.has(channel.prefix);
            return (
              <section className={styles.keyFilterGroup} key={channel.id}>
                <div className={styles.keyFilterGroupHeader}>
                  <SelectionCheckbox
                    checked={channelBlocked}
                    onChange={(blocked) => setBlocked(channel.prefix, blocked)}
                    ariaLabel={t('sharing.key_filter_block_channel', { channel: channel.slug })}
                    title={t('sharing.key_filter_channel_hint')}
                    label={
                      <span className={styles.keyFilterGroupTitle}>
                        <span className={styles.mono}>{channel.slug}</span>
                        <span className={styles.keyFilterMeta}>
                          {t('sharing.key_filter_channel_all', { owner: channel.owner })}
                        </span>
                      </span>
                    }
                  />
                </div>
                <div className={styles.keyFilterRoutes}>
                  {prefixes.map((scope) => (
                    <SelectionCheckbox
                      key={scope.prefix}
                      className={styles.keyFilterRoute}
                      checked={channelBlocked || selected.has(scope.prefix)}
                      disabled={channelBlocked}
                      onChange={(blocked) => setBlocked(scope.prefix, blocked)}
                      ariaLabel={t('sharing.key_filter_block_scope', { scope: scope.prefix })}
                      label={
                        <span className={styles.keyFilterRouteLabel}>
                          <span className={styles.keyFilterRouteId}>{scope.prefix}</span>
                          <span className={styles.keyFilterRouteMeta}>
                            {t('sharing.key_filter_scope_meta', {
                              provider: scope.provider,
                              count: scope.count,
                            })}
                          </span>
                        </span>
                      }
                    />
                  ))}
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

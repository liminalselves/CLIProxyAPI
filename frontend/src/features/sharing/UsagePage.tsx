import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { AutocompleteInput } from '@/components/ui/AutocompleteInput';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { DatePickerInput } from '@/components/ui/DatePickerInput';
import { EmptyState } from '@/components/ui/EmptyState';
import { Select } from '@/components/ui/Select';
import { Sheet } from '@/components/ui/Sheet';
import { useAuthStore } from '@/stores';
import { sharingApi } from '@/services/api/sharing';
import { formatCompactNumber } from '@/utils/format';
import { normalizePaginationParams, parsePage, parsePageSize } from '@/utils/pagination';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/Table';
import type { Account, Channel, PersonalKey, SharedKeyRef, UsageEntry } from './types';
import { DailyTrend, LoadState, PageHeader, Pager } from './shared';
import { date, duration, number, useSharingData } from './hooks';
import {
  buildModelCandidateQuery,
  resolveUsageKey,
  resolveUsageModel,
  type UsageKeyOption,
} from './usageFilters';
import styles from './Sharing.module.scss';

type UsageScope = 'mine' | 'contributed' | 'site';
type RangePreset = 'all' | 'today' | 'yesterday' | '7d' | '30d' | 'custom';

const DAY_MS = 86400000;
const RANGE_PRESETS: RangePreset[] = ['all', 'today', 'yesterday', '7d', '30d', 'custom'];

const startOfDay = (value: Date) =>
  new Date(value.getFullYear(), value.getMonth(), value.getDate());

const parseDayInput = (value: string) => {
  const [year, month, day] = value.split('-').map(Number);
  return new Date(year, (month || 1) - 1, day || 1);
};

const toDayInput = (iso: string | null, offsetDays: number) => {
  if (!iso) return '';
  const time = new Date(iso).getTime();
  if (Number.isNaN(time)) return '';
  const shifted = startOfDay(new Date(time + offsetDays * DAY_MS));
  return `${shifted.getFullYear()}-${String(shifted.getMonth() + 1).padStart(2, '0')}-${String(shifted.getDate()).padStart(2, '0')}`;
};

function presetRange(preset: Exclude<RangePreset, 'all' | 'custom'>) {
  const today = startOfDay(new Date());
  const tomorrow = new Date(today.getTime() + DAY_MS);
  switch (preset) {
    case 'today':
      return { from: today, to: tomorrow };
    case 'yesterday':
      return { from: new Date(today.getTime() - DAY_MS), to: today };
    case '7d':
      return { from: new Date(today.getTime() - 6 * DAY_MS), to: tomorrow };
    case '30d':
      return { from: new Date(today.getTime() - 29 * DAY_MS), to: tomorrow };
  }
}

function detectPreset(from: string, to: string): RangePreset {
  if (!from && !to) return 'all';
  for (const preset of RANGE_PRESETS) {
    if (preset === 'all' || preset === 'custom') continue;
    const range = presetRange(preset);
    if (range.from.toISOString() === from && range.to.toISOString() === to) return preset;
  }
  return 'custom';
}

const stripJsonExt = (name: string) => name.replace(/\.json$/i, '');
// Channel-mounted credential files are named "<channel>-<type>#<n>-<internal>.json";
// the pinned prefix alone identifies the credential, the rest is internal.
const credentialDisplayName = (name: string) =>
  stripJsonExt(name).replace(/^(.+#\d+)-[^#]+$/, '$1');
const tokensPerSecond = (entry: UsageEntry) =>
  entry.tokens_known && entry.latency_ms > 0 && entry.output_tokens > 0
    ? Math.round(entry.output_tokens / (entry.latency_ms / 1000))
    : null;

// Cache hit rate: the share of total input tokens served from the upstream cache.
export const cacheHitRate = (
  entry: Pick<UsageEntry, 'input_tokens' | 'cache_read_tokens'>
): number | null =>
  entry.input_tokens > 0 && entry.cache_read_tokens > 0
    ? (entry.cache_read_tokens / entry.input_tokens) * 100
    : null;

const statusKind = (status: number, failed: boolean): 'ok' | 'fail' | 'interrupted' =>
  status === 499 ? 'interrupted' : failed || status >= 400 ? 'fail' : 'ok';

const entryStatus = (entry: UsageEntry): 'ok' | 'fail' | 'interrupted' =>
  statusKind(entry.status, entry.failed);

export function RequestStatus({ status, failed = false }: { status: number; failed?: boolean }) {
  const { t } = useTranslation();
  const kind = statusKind(status, failed);
  const label =
    kind === 'interrupted'
      ? t('sharing.status_interrupted')
      : kind === 'fail'
        ? t('sharing.status_failed')
        : t('sharing.status_success');
  return (
    <span
      className={
        kind === 'interrupted'
          ? styles.pillInterrupted
          : kind === 'fail'
            ? styles.pillFail
            : styles.pillOk
      }
    >
      {label} {status}
    </span>
  );
}

function Field({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className={styles.field}>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function UsageDetailSheet({
  entry,
  showConsumer,
  onClose,
}: {
  entry: UsageEntry | null;
  showConsumer: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  // Canonical accounting: cache buckets are subsets of input, reasoning is a
  // subset of output; the total is input + output with no double counting.
  const inputParts = entry
    ? [
        entry.cache_read_tokens > 0
          ? t('sharing.token_cache', { count: number(entry.cache_read_tokens) })
          : '',
        entry.cache_creation_tokens > 0
          ? t('sharing.token_cache_write', { count: number(entry.cache_creation_tokens) })
          : '',
      ].filter(Boolean)
    : [];
  const cacheRate = entry ? cacheHitRate(entry) : null;
  const rate = entry ? tokensPerSecond(entry) : null;
  return (
    <Sheet
      open={!!entry}
      onClose={onClose}
      size="md"
      eyebrow={entry ? date(entry.at) : undefined}
      title={entry?.model}
      description={
        entry
          ? `${entry.endpoint} · ${t(entry.stream ? 'sharing.stream' : 'sharing.nonstream')}`
          : undefined
      }
    >
      {entry && (
        <dl className={styles.fieldGrid}>
          <Field label={t('sharing.status_label')}>
            <div>
              <RequestStatus status={entry.status} failed={entry.failed} />
              {entry.status === 499 && (
                <div className={styles.cellSecondary}>{t('sharing.status_interrupted_hint')}</div>
              )}
            </div>
          </Field>
          <Field label={t('sharing.field_latency')}>{duration(entry.latency_ms)}</Field>
          {entry.ttft_ms > 0 && (
            <Field label={t('sharing.field_ttft')}>{duration(entry.ttft_ms)}</Field>
          )}
          {showConsumer && <Field label={t('sharing.consumer')}>{entry.consumer}</Field>}
          <Field label={t('sharing.field_key')}>{entry.key_name || '—'}</Field>
          <Field label={t('sharing.channel')}>{entry.channel}</Field>
          {entry.owner && entry.owner !== entry.consumer && (
            <Field label={t('sharing.owner_label')}>{entry.owner}</Field>
          )}
          <Field label={t('sharing.field_credential')}>
            {entry.credential_name ? credentialDisplayName(entry.credential_name) : '—'}
          </Field>
          <Field label={t('sharing.field_model')}>
            <span className={styles.mono}>
              {entry.model}
              {servingModelId(entry) && (
                <span className={styles.cellSecondary}>（{servingModelId(entry)}）</span>
              )}
            </span>
          </Field>
          {entry.upstream_model && entry.upstream_model !== entry.model && (
            <Field label={t('sharing.field_upstream_model')}>
              <span className={styles.mono}>{entry.upstream_model}</span>
            </Field>
          )}
          <Field label={t('sharing.field_tokens')}>
            {entry.tokens_known ? (
              <>
                <div>
                  {t('sharing.token_input')} {number(entry.input_tokens)}
                  {inputParts.length > 0 && (
                    <span className={styles.cellSecondary}>（{inputParts.join(' · ')}）</span>
                  )}
                </div>
                <div>
                  {t('sharing.token_output')} {number(entry.output_tokens)}
                  {entry.reasoning_tokens > 0 && (
                    <span className={styles.cellSecondary}>
                      （{t('sharing.token_reasoning', { count: number(entry.reasoning_tokens) })}）
                    </span>
                  )}
                </div>
              </>
            ) : (
              t('sharing.unknown')
            )}
          </Field>
          {cacheRate !== null && (
            <Field label={t('sharing.field_cache_hit')}>{cacheRate.toFixed(1)}%</Field>
          )}
          {entry.reasoning_effort && (
            <Field label={t('sharing.field_reasoning_effort')}>
              <span className={styles.mono}>{entry.reasoning_effort}</span>
            </Field>
          )}
          {rate !== null && <Field label={t('sharing.field_generation_speed')}>{rate} t/s</Field>}
          {entry.trace_id && (
            <Field label={t('sharing.field_trace')}>
              <span className={styles.mono}>{entry.trace_id}</span>
            </Field>
          )}
          {entry.request_id && (
            <Field label={t('sharing.field_request')}>
              <span className={styles.mono}>{entry.request_id}</span>
            </Field>
          )}
        </dl>
      )}
    </Sheet>
  );
}

function RequestStatusPill({ kind }: { kind: 'fail' | 'interrupted' }) {
  const { t } = useTranslation();
  return (
    <span className={kind === 'interrupted' ? styles.pillInterrupted : styles.pillFail}>
      {kind === 'interrupted' ? t('sharing.status_interrupted') : t('sharing.status_failed')}
    </span>
  );
}

// Bare base-model calls (keys with even load balancing) are served by one
// channel's pinned model ID; surface it next to the requested name.
export const servingModelId = (entry: Pick<UsageEntry, 'model' | 'channel'>): string =>
  entry.model && !entry.model.includes('/') && entry.channel
    ? `${entry.channel}/${entry.model}`
    : '';

export function UsageLogRow({
  entry,
  showConsumer,
  onOpen,
}: {
  entry: UsageEntry;
  showConsumer: boolean;
  onOpen: () => void;
}) {
  const { t } = useTranslation();
  const status = entryStatus(entry);
  const rate = tokensPerSecond(entry);
  const latencyClass =
    status === 'fail'
      ? styles.latencyFail
      : status === 'interrupted'
        ? styles.latencyInterrupted
        : '';
  return (
    <TableRow className={styles.clickableRow} onClick={onOpen}>
      <TableCell>
        <div className={`${styles.cellPrimary} ${styles.mono}`}>{date(entry.at)}</div>
        {status !== 'ok' && (
          <div className={styles.logStatusLine}>
            <RequestStatusPill kind={status} />{' '}
            <span className={styles.cellSecondary}>{entry.status}</span>
          </div>
        )}
      </TableCell>
      <TableCell>
        <div className={styles.cellPrimary}>{entry.key_name || '—'}</div>
        {showConsumer ? (
          <div className={styles.cellSecondary}>@{entry.consumer}</div>
        ) : (
          <div className={styles.cellSecondary}>{entry.channel}</div>
        )}
      </TableCell>
      <TableCell>
        <div className={`${styles.cellPrimary} ${styles.mono}`}>
          {entry.model}
          {servingModelId(entry) && (
            <span className={styles.cellSecondary}>（{servingModelId(entry)}）</span>
          )}
        </div>
        {showConsumer && <div className={styles.cellSecondary}>{entry.channel}</div>}
      </TableCell>
      <TableCell>
        <div className={styles.cellPrimary}>
          {t(entry.stream ? 'sharing.stream' : 'sharing.nonstream')}
        </div>
        {rate !== null && <div className={styles.cellSecondary}>{rate} t/s</div>}
      </TableCell>
      <TableCell>
        {entry.tokens_known ? (
          <div className={`${styles.cellPrimary} ${styles.mono}`}>
            {`${number(entry.input_tokens)}${
              entry.cache_read_tokens > 0 ? `（${number(entry.cache_read_tokens)}）` : ''
            } / ${number(entry.output_tokens)}${
              entry.reasoning_tokens > 0 ? `（${number(entry.reasoning_tokens)}）` : ''
            }`}
          </div>
        ) : (
          <span className={styles.cellSecondary}>{t('sharing.unknown')}</span>
        )}
      </TableCell>
      <TableCell>
        <div className={`${styles.latency} ${latencyClass}`}>
          {entry.ttft_ms > 0 && (
            <div>
              {t('sharing.latency_ttft_short')} {duration(entry.ttft_ms)}
            </div>
          )}
          <div>
            {t('sharing.latency_total_short')} {duration(entry.latency_ms)}
          </div>
        </div>
      </TableCell>
    </TableRow>
  );
}

function UsageLogCard({
  entry,
  showConsumer,
  onOpen,
}: {
  entry: UsageEntry;
  showConsumer: boolean;
  onOpen: () => void;
}) {
  const { t } = useTranslation();
  const status = entryStatus(entry);
  const rate = tokensPerSecond(entry);
  return (
    <button type="button" className={styles.keyCard} onClick={onOpen}>
      <span className={styles.keyCardHead}>
        <span className={`${styles.cellPrimary} ${styles.mono}`}>
          {entry.model}
          {servingModelId(entry) && (
            <span className={styles.cellSecondary}>（{servingModelId(entry)}）</span>
          )}
        </span>
      </span>
      <span className={styles.cellSecondary}>{date(entry.at)}</span>
      <span className={styles.cellSecondary}>
        {entry.key_name || '—'}
        {showConsumer ? ` · @${entry.consumer}` : ''} · {entry.channel}
      </span>
      <span className={styles.cellSecondary}>
        {duration(entry.latency_ms)}
        {rate !== null ? ` · ${rate} t/s` : ''}
        {entry.tokens_known
          ? ` · ${t('sharing.hint_tokens_io', {
              input: `${number(entry.input_tokens)}${
                entry.cache_read_tokens > 0 ? `（${number(entry.cache_read_tokens)}）` : ''
              }`,
              output: `${number(entry.output_tokens)}${
                entry.reasoning_tokens > 0 ? `（${number(entry.reasoning_tokens)}）` : ''
              }`,
            })}`
          : ''}
        {` · ${t(entry.stream ? 'sharing.stream' : 'sharing.nonstream')}`}
      </span>
      {status !== 'ok' && (
        <span className={styles.logStatusLine}>
          <RequestStatusPill kind={status} />{' '}
          <span className={styles.cellSecondary}>{entry.status}</span>
        </span>
      )}
    </button>
  );
}

export function UsagePage() {
  const { t } = useTranslation();
  const account = useAuthStore((state) => state.account);
  const isMobile = useMediaQuery('(max-width: 768px)');
  const me = account?.username || '';
  const isAdmin = account?.role === 'admin';
  const [params, setParams] = useSearchParams();

  const scope: UsageScope =
    params.get('scope') === 'contributed'
      ? 'contributed'
      : params.get('scope') === 'site' && isAdmin
        ? 'site'
        : 'mine';
  const page = parsePage(params.get('page'));
  const pageSize = parsePageSize(params.get('page_size'));
  const appliedKeyID = params.get('key_id') || '';
  const queryString = params.toString();
  const appliedModel = params.get('model') || '';
  const appliedChannel = params.get('channel_id') || '';
  const appliedConsumer = scope === 'mine' ? '' : params.get('consumer') || '';
  const appliedFrom = params.get('from') || '';
  const appliedTo = params.get('to') || '';

  const [model, setModel] = useState(params.get('model') || '');
  const [channelID, setChannelID] = useState(params.get('channel_id') || '');
  const [keyDraft, setKeyText] = useState<string | null>(null);
  const [preset, setPreset] = useState<RangePreset>(() =>
    detectPreset(params.get('from') || '', params.get('to') || '')
  );
  const [customFrom, setCustomFrom] = useState(() => toDayInput(params.get('from'), 0));
  const [customTo, setCustomTo] = useState(() => toDayInput(params.get('to'), -1));

  useEffect(() => {
    setModel(appliedModel);
    setChannelID(appliedChannel);
    setKeyText(null);
    setPreset(detectPreset(appliedFrom, appliedTo));
    setCustomFrom(toDayInput(appliedFrom, 0));
    setCustomTo(toDayInput(appliedTo, -1));
  }, [appliedModel, appliedChannel, appliedKeyID, appliedConsumer, appliedFrom, appliedTo, scope]);

  const [options, setOptions] = useState<{
    channels: Channel[];
    keys: PersonalKey[];
    users: Account[];
    allKeys: SharedKeyRef[];
  } | null>(null);
  const [selected, setSelected] = useState<UsageEntry | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [channels, keys, users, allKeys] = await Promise.all([
          sharingApi.channels(),
          sharingApi.keys(),
          sharingApi.users(),
          sharingApi.allKeys(),
        ]);
        if (!cancelled)
          setOptions({
            channels: channels.channels,
            keys: keys.keys,
            users: users.users,
            allKeys: allKeys.keys,
          });
      } catch {
        // Usage records remain available if filter suggestions fail to load.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const filterQuery = useMemo(() => {
    const values = new URLSearchParams(queryString);
    values.delete('page');
    values.delete('page_size');
    values.delete('scope');
    if (scope === 'mine') values.set('consumer', me);
    if (scope === 'contributed') values.set('owner', me);
    return values.toString();
  }, [queryString, scope, me]);
  const {
    data: summary,
    loading: summaryLoading,
    error: summaryError,
    refresh: refreshSummary,
  } = useSharingData(
    useCallback(
      () => sharingApi.summary(Object.fromEntries(new URLSearchParams(filterQuery))),
      [filterQuery]
    ),
    { keepPreviousData: false, headerRefresh: false }
  );
  const {
    data: usage,
    loading,
    error,
    refresh: refreshUsage,
  } = useSharingData(
    useCallback(
      () =>
        sharingApi.usage({
          ...Object.fromEntries(new URLSearchParams(filterQuery)),
          page,
          page_size: pageSize,
        }),
      [filterQuery, page, pageSize]
    ),
    { keepPreviousData: false, headerRefresh: false }
  );
  useEffect(() => {
    const next = normalizePaginationParams(params, !loading && !error ? usage?.total : undefined);
    if (next.toString() !== params.toString()) setParams(next, { replace: true });
  }, [params, setParams, usage, loading, error]);

  const groups = useMemo(() => summary?.groups ?? [], [summary]);

  // Model suggestions use a summary that ignores the applied model filter and
  // pagination, so choosing a model never shrinks the dropdown to itself.
  const candidateQuery = useMemo(
    () => buildModelCandidateQuery(queryString, scope, me),
    [queryString, scope, me]
  );
  const { data: candidateSummary, refresh: refreshCandidates } = useSharingData(
    useCallback(
      () => sharingApi.summary(Object.fromEntries(new URLSearchParams(candidateQuery))),
      [candidateQuery]
    ),
    { keepPreviousData: false, headerRefresh: false }
  );
  useHeaderRefresh(
    useCallback(async () => {
      await Promise.all([refreshSummary(), refreshUsage(), refreshCandidates()]);
    }, [refreshSummary, refreshUsage, refreshCandidates])
  );

  // Candidates pair every bare model (even load balancing) with each channel
  // that served it, in the same display form the log entries use; selecting
  // one pins the filter to that exact serving channel server-side.
  const models = useMemo(() => {
    const set = new Set<string>();
    for (const group of candidateSummary?.groups ?? []) {
      if (!group.model) continue;
      set.add(group.model);
      if (!group.model.includes('/') && group.channel)
        set.add(`${group.model}（${group.channel}/${group.model}）`);
    }
    return [...set].sort((a, b) => a.localeCompare(b));
  }, [candidateSummary]);

  const keyLabel = useCallback(
    (key: PersonalKey | SharedKeyRef) =>
      key.name || ('hint' in key ? key.hint : '') || t('sharing.unnamed_key'),
    [t]
  );

  const keyOptions = useMemo<UsageKeyOption[]>(() => {
    if (!options) return [];
    if (scope === 'mine')
      return options.keys.map((key) => ({ label: keyLabel(key), keyId: key.id }));
    return [
      ...options.users.map((user) => ({ label: `@${user.username}`, consumer: user.username })),
      ...options.allKeys.map((key) => ({
        label: `${keyLabel(key)}（@${key.owner}）`,
        keyId: key.id,
      })),
    ];
  }, [options, scope, keyLabel]);
  const keyFilterOptions = useMemo(() => keyOptions.map((option) => option.label), [keyOptions]);
  const keyText =
    keyDraft ??
    (appliedKeyID
      ? keyOptions.find((option) => option.keyId === appliedKeyID)?.label || ''
      : appliedConsumer
        ? `@${appliedConsumer}`
        : '');
  const resolvedModel = resolveUsageModel(model, models, params.get('model') || '');
  const resolvedKey =
    keyDraft === null && (appliedKeyID || appliedConsumer)
      ? { label: keyText, keyId: appliedKeyID || undefined, consumer: appliedConsumer || undefined }
      : resolveUsageKey(keyText, keyOptions);
  const canApply = resolvedModel !== null && resolvedKey !== null;

  const apply = () => {
    if (resolvedModel === null || resolvedKey === null) return;
    const next = new URLSearchParams();
    if (scope !== 'mine') next.set('scope', scope);
    if (pageSize !== 50) next.set('page_size', String(pageSize));
    if (resolvedModel) next.set('model', resolvedModel);
    if (channelID) next.set('channel_id', channelID);
    if (resolvedKey.keyId) next.set('key_id', resolvedKey.keyId);
    if (resolvedKey.consumer && scope !== 'mine') next.set('consumer', resolvedKey.consumer);
    if (preset === 'custom') {
      if (customFrom) next.set('from', startOfDay(parseDayInput(customFrom)).toISOString());
      if (customTo)
        next.set(
          'to',
          new Date(startOfDay(parseDayInput(customTo)).getTime() + DAY_MS).toISOString()
        );
    } else if (preset !== 'all') {
      const range = presetRange(preset);
      next.set('from', range.from.toISOString());
      next.set('to', range.to.toISOString());
    }
    setParams(next);
  };

  const reset = () => {
    setModel('');
    setChannelID('');
    setKeyText('');
    setPreset('all');
    setCustomFrom('');
    setCustomTo('');
    const next = new URLSearchParams();
    if (scope !== 'mine') next.set('scope', scope);
    if (pageSize !== 50) next.set('page_size', String(pageSize));
    setParams(next);
  };

  const setScope = (value: UsageScope) => {
    const next = new URLSearchParams(params);
    next.delete('page');
    next.delete('key_id');
    next.delete('consumer');
    setKeyText('');
    if (value === 'mine') next.delete('scope');
    else next.set('scope', value);
    setParams(next);
  };

  const setPageSize = (size: number) => {
    const next = new URLSearchParams(params);
    if (size === 50) next.delete('page_size');
    else next.set('page_size', String(size));
    next.delete('page');
    setParams(next);
  };

  const total = summary?.total;
  const failureRate =
    total && total.executions > 0 ? (total.failures / total.executions) * 100 : null;
  const tokenHint = total
    ? [
        number(total.total_tokens),
        t('sharing.hint_tokens_io', {
          input: number(total.input_tokens),
          output: number(total.output_tokens),
        }),
        total.unknown_token_records > 0
          ? t('sharing.hint_tokens_unknown', { count: number(total.unknown_token_records) })
          : '',
      ]
        .filter(Boolean)
        .join('；')
    : undefined;

  const showConsumer = scope !== 'mine';

  return (
    <div className={styles.page}>
      <PageHeader
        title={t('sharing.usage')}
        actions={
          <div className={styles.segmented}>
            {(['mine', 'contributed', 'site'] as const)
              .filter((value) => value !== 'site' || isAdmin)
              .map((value) => (
                <button
                  key={value}
                  type="button"
                  className={`${styles.segmentedItem} ${scope === value ? styles.active : ''}`}
                  onClick={() => setScope(value)}
                >
                  {value === 'mine'
                    ? t('sharing.my_usage')
                    : value === 'contributed'
                      ? t('sharing.my_contribution')
                      : t('sharing.scope_all')}
                </button>
              ))}
          </div>
        }
      />
      <Card>
        <div className={styles.filters}>
          <div className="form-group">
            <label>{t('sharing.range_label')}</label>
            <Select
              value={preset}
              onChange={(value) => setPreset(value as RangePreset)}
              ariaLabel={t('sharing.range_label')}
              options={RANGE_PRESETS.map((value) => ({
                value,
                label: t('sharing.range_' + value),
              }))}
            />
          </div>
          {preset === 'custom' && (
            <>
              <DatePickerInput
                label={t('sharing.from')}
                value={customFrom}
                onChange={setCustomFrom}
              />
              <DatePickerInput label={t('sharing.to')} value={customTo} onChange={setCustomTo} />
            </>
          )}
          <AutocompleteInput
            label={t('sharing.model')}
            value={model}
            onChange={setModel}
            options={models}
            placeholder={t('sharing.all_models')}
            emptyHint={t('sharing.no_model_options')}
            clearLabel={t('sharing.clear_filter')}
          />
          <div className="form-group">
            <label>{t('sharing.channel')}</label>
            <Select
              value={channelID}
              onChange={setChannelID}
              ariaLabel={t('sharing.channel')}
              options={[
                { value: '', label: t('sharing.all_channels') },
                ...(options?.channels ?? []).map((channel) => ({
                  value: channel.id,
                  label: `${channel.slug}（${channel.owner}）`,
                })),
              ]}
            />
          </div>
          <AutocompleteInput
            label={scope === 'mine' ? t('sharing.key_label') : t('sharing.key_member_label')}
            value={keyText}
            onChange={setKeyText}
            options={keyFilterOptions}
            placeholder={scope === 'mine' ? t('sharing.all_keys') : t('sharing.key_or_member')}
            clearLabel={t('sharing.clear_filter')}
          />
          <div className={styles.actions}>
            <Button onClick={apply} disabled={!canApply}>
              {t('sharing.apply_filter')}
            </Button>
            <Button variant="secondary" onClick={reset}>
              {t('sharing.clear_filter')}
            </Button>
          </div>
        </div>
        <div className={styles.statPills}>
          <span className={styles.statPill} title={t('sharing.hint_executions')}>
            <span className={styles.statPillLabel}>{t('sharing.pill_calls')}</span>
            <span className={styles.statPillValue}>{total ? number(total.executions) : '—'}</span>
          </span>
          <span className={styles.statPill} title={tokenHint}>
            <span className={styles.statPillLabel}>{t('sharing.pill_tokens')}</span>
            <span className={styles.statPillValue}>
              {total ? formatCompactNumber(total.total_tokens) : '—'}
            </span>
          </span>
          <span
            className={`${styles.statPill} ${
              failureRate !== null && failureRate > 0
                ? styles.statPillDanger
                : styles.statPillSuccess
            }`}
            title={
              total
                ? t('sharing.hint_failures_detail', {
                    failures: number(total.failures),
                    executions: number(total.executions),
                  })
                : undefined
            }
          >
            <span className={styles.statPillLabel}>{t('sharing.pill_failure_rate')}</span>
            <span className={styles.statPillValue}>
              {failureRate !== null ? `${failureRate.toFixed(1)}%` : '—'}
            </span>
          </span>
        </div>
      </Card>
      <Card title={t('sharing.trend_title')}>
        {summaryLoading && !summary ? null : <DailyTrend groups={groups} />}
      </Card>
      <LoadState loading={loading || summaryLoading} error={error || summaryError} />
      {!loading && !error && !usage?.items.length && (
        <EmptyState title={t('sharing.no_records')} description={t('sharing.no_records_hint')} />
      )}
      {Boolean(usage?.items.length) &&
        (isMobile ? (
          <div className={styles.keyCards}>
            {usage?.items.map((entry) => (
              <UsageLogCard
                key={entry.id}
                entry={entry}
                showConsumer={showConsumer}
                onOpen={() => setSelected(entry)}
              />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                {['time', 'key_label', 'model', 'col_mode', 'col_tokens', 'col_latency'].map(
                  (key) => (
                    <TableHead key={key}>{t('sharing.' + key)}</TableHead>
                  )
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {usage?.items.map((entry) => (
                <UsageLogRow
                  key={entry.id}
                  entry={entry}
                  showConsumer={showConsumer}
                  onOpen={() => setSelected(entry)}
                />
              ))}
            </TableBody>
          </Table>
        ))}
      <Pager
        page={page}
        total={usage?.total || 0}
        pageSize={pageSize}
        loading={loading}
        onPageSize={setPageSize}
        onPage={(value) => {
          const next = new URLSearchParams(params);
          if (value === 1) next.delete('page');
          else next.set('page', String(value));
          setParams(next);
        }}
      />
      <UsageDetailSheet
        entry={selected}
        showConsumer={showConsumer}
        onClose={() => setSelected(null)}
      />
    </div>
  );
}

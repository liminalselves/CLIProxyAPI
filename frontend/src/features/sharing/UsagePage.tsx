import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { AutocompleteInput } from '@/components/ui/AutocompleteInput';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { DatePickerInput } from '@/components/ui/DatePickerInput';
import { Select } from '@/components/ui/Select';
import { Sheet } from '@/components/ui/Sheet';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/Table';
import { EmptyState } from '@/components/ui/EmptyState';
import { useAuthStore } from '@/stores';
import { sharingApi } from '@/services/api/sharing';
import type { Channel, UsageEntry } from './types';
import { DailyTrend, LoadState, PageHeader, Pager } from './shared';
import { useSharingData, date, duration, number } from './hooks';
import styles from './Sharing.module.scss';

type UsageTab = 'detail' | 'calls' | 'groups';
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
const shortTrace = (id: string) => (id.length > 8 ? id.slice(0, 8) + '…' : id);
const tokensPerSecond = (entry: UsageEntry) =>
  entry.tokens_known && entry.latency_ms > 0 && entry.output_tokens > 0
    ? Math.round(entry.output_tokens / (entry.latency_ms / 1000))
    : null;

function Field({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className={styles.field}>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function UsageDetailSheet({ entry, onClose }: { entry: UsageEntry | null; onClose: () => void }) {
  const { t } = useTranslation();
  const failed = entry ? entry.failed || entry.status >= 400 : false;
  const tokenParts = entry
    ? [
        entry.reasoning_tokens > 0
          ? t('sharing.token_reasoning', { count: number(entry.reasoning_tokens) })
          : '',
        entry.cache_read_tokens > 0
          ? t('sharing.token_cache', { count: number(entry.cache_read_tokens) })
          : '',
        entry.cache_creation_tokens > 0
          ? t('sharing.token_cache_write', { count: number(entry.cache_creation_tokens) })
          : '',
      ].filter(Boolean)
    : [];
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
            <span className={failed ? styles.pillFail : styles.pillOk}>
              {t(failed ? 'sharing.status_failed' : 'sharing.status_success')}
            </span>{' '}
            <span className={styles.cellSecondary}>{entry.status}</span>
          </Field>
          <Field label={t('sharing.field_latency')}>{duration(entry.latency_ms)}</Field>
          {entry.ttft_ms > 0 && (
            <Field label={t('sharing.field_ttft')}>{duration(entry.ttft_ms)}</Field>
          )}
          <Field label={t('sharing.consumer')}>{entry.consumer}</Field>
          <Field label={t('sharing.field_key')}>{entry.key_name || '—'}</Field>
          <Field label={t('sharing.channel')}>{entry.channel}</Field>
          <Field label={t('sharing.field_credential')}>
            {entry.credential_name ? stripJsonExt(entry.credential_name) : '—'}
          </Field>
          <Field label={t('sharing.owner')}>{entry.owner}</Field>
          <Field label={t('sharing.field_model')}>
            <span className={styles.mono}>{entry.model}</span>
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
                  {t('sharing.token_input')} {number(entry.input_tokens)} ·{' '}
                  {t('sharing.token_output')} {number(entry.output_tokens)} ·{' '}
                  {t('sharing.token_total')} {number(entry.total_tokens)}
                </div>
                {tokenParts.length > 0 && (
                  <div className={styles.cellSecondary}>{tokenParts.join(' · ')}</div>
                )}
              </>
            ) : (
              t('sharing.unknown')
            )}
          </Field>
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

export function UsagePage() {
  const { t } = useTranslation();
  const account = useAuthStore((state) => state.account);
  const [params, setParams] = useSearchParams();

  const tab: UsageTab =
    params.get('tab') === 'calls' || params.get('tab') === 'groups'
      ? (params.get('tab') as UsageTab)
      : 'detail';
  const page = Number(params.get('page') || 1);
  const pageSize = Number(params.get('page_size') || 50);
  const keyID = params.get('key_id') || '';
  const queryString = params.toString();
  const appliedConsumer = params.get('consumer') || '';
  const appliedOwner = params.get('owner') || '';

  const [consumer, setConsumer] = useState(appliedConsumer);
  const [owner, setOwner] = useState(appliedOwner);
  const [model, setModel] = useState(params.get('model') || '');
  const [channelID, setChannelID] = useState(params.get('channel_id') || '');
  const [preset, setPreset] = useState<RangePreset>(() =>
    detectPreset(params.get('from') || '', params.get('to') || '')
  );
  const [customFrom, setCustomFrom] = useState(() => toDayInput(params.get('from'), 0));
  const [customTo, setCustomTo] = useState(() => toDayInput(params.get('to'), -1));

  const [options, setOptions] = useState<{ usernames: string[]; channels: Channel[] } | null>(null);
  const [selected, setSelected] = useState<UsageEntry | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [users, channels] = await Promise.all([sharingApi.users(), sharingApi.channels()]);
        if (!cancelled)
          setOptions({
            usernames: users.users.map((user) => user.username),
            channels: channels.channels,
          });
      } catch {
        // Filter suggestions are best-effort; free-text filtering keeps working.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // The summary feed drives the stat cards, the daily trend and the groups tab;
  // only the active tab's paged dataset is fetched alongside it.
  const { data, loading, error } = useSharingData(
    useCallback(async () => {
      const values: Record<string, string | number> = {
        ...Object.fromEntries(new URLSearchParams(queryString)),
        page_size: pageSize,
      };
      delete values.tab;
      const [summary, usage, calls] = await Promise.all([
        sharingApi.summary(values),
        tab === 'detail' ? sharingApi.usage(values) : Promise.resolve(null),
        tab === 'calls' ? sharingApi.calls(values) : Promise.resolve(null),
      ]);
      return { summary, usage, calls };
    }, [queryString, tab, pageSize])
  );

  const groups = useMemo(() => data?.summary.groups ?? [], [data]);
  const distinct = (values: Array<string | undefined>) =>
    [...new Set(values.filter((value): value is string => Boolean(value)))].sort((a, b) =>
      a.localeCompare(b)
    );
  const consumers = distinct([...(options?.usernames ?? []), ...groups.map((g) => g.consumer)]);
  const owners = distinct([...(options?.usernames ?? []), ...groups.map((g) => g.owner)]);
  const models = distinct(groups.map((g) => g.model));

  const apply = () => {
    const next = new URLSearchParams();
    if (consumer.trim()) next.set('consumer', consumer.trim());
    if (owner.trim()) next.set('owner', owner.trim());
    if (model.trim()) next.set('model', model.trim());
    if (channelID) next.set('channel_id', channelID);
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
    if (keyID) next.set('key_id', keyID);
    if (tab !== 'detail') next.set('tab', tab);
    setParams(next);
  };

  const reset = () => {
    setConsumer('');
    setOwner('');
    setModel('');
    setChannelID('');
    setPreset('all');
    setCustomFrom('');
    setCustomTo('');
    const next = new URLSearchParams();
    if (tab !== 'detail') next.set('tab', tab);
    setParams(next);
  };

  const me = account?.username || '';
  const scope =
    me && appliedConsumer === me && appliedOwner !== me
      ? 'mine'
      : me && appliedOwner === me && appliedConsumer !== me
        ? 'contributed'
        : !appliedConsumer && !appliedOwner
          ? 'all'
          : 'custom';
  const setScope = (value: 'all' | 'mine' | 'contributed') => {
    const next = new URLSearchParams(params);
    next.delete('consumer');
    next.delete('owner');
    next.delete('page');
    if (value === 'mine') next.set('consumer', me);
    if (value === 'contributed') next.set('owner', me);
    setConsumer(next.get('consumer') || '');
    setOwner(next.get('owner') || '');
    setParams(next);
  };

  const setTab = (value: UsageTab) => {
    const next = new URLSearchParams(params);
    if (value === 'detail') next.delete('tab');
    else next.set('tab', value);
    next.delete('page');
    setParams(next);
  };

  const setPageSize = (size: number) => {
    const next = new URLSearchParams(params);
    if (size === 50) next.delete('page_size');
    else next.set('page_size', String(size));
    next.delete('page');
    setParams(next);
  };

  const total = data?.summary.total;
  const failureRate =
    total && total.executions > 0 ? ((total.failures / total.executions) * 100).toFixed(1) : null;
  const stats = [
    {
      label: t('sharing.calls'),
      value: total ? number(total.calls) : '—',
      hint: t('sharing.hint_requests'),
    },
    {
      label: t('sharing.executions'),
      value: total ? number(total.executions) : '—',
      hint: t('sharing.hint_executions'),
    },
    {
      label: t('sharing.failures'),
      value: total ? number(total.failures) : '—',
      hint: failureRate !== null ? t('sharing.hint_failures', { rate: failureRate }) : '',
    },
    {
      label: t('sharing.total_tokens'),
      value: total ? number(total.total_tokens) : '—',
      hint:
        total && total.unknown_token_records > 0
          ? t('sharing.hint_tokens_unknown', { count: number(total.unknown_token_records) })
          : '',
    },
  ];

  const statusPill = (status: number, failed?: boolean) => (
    <span className={failed || status >= 400 ? styles.pillFail : styles.pillOk}>{status}</span>
  );

  const tabHint =
    tab === 'detail'
      ? t('sharing.hint_detail')
      : tab === 'calls'
        ? t('sharing.hint_calls')
        : t('sharing.hint_groups');

  return (
    <div className={styles.page}>
      <PageHeader
        title={t('sharing.usage')}
        hint={t('sharing.usage_hint')}
        actions={
          me ? (
            <div className={styles.segmented}>
              {(['all', 'mine', 'contributed'] as const).map((value) => (
                <button
                  key={value}
                  type="button"
                  className={`${styles.segmentedItem} ${scope === value ? styles.active : ''}`}
                  onClick={() => setScope(value)}
                >
                  {value === 'all'
                    ? t('sharing.scope_all')
                    : value === 'mine'
                      ? t('sharing.my_usage')
                      : t('sharing.my_contribution')}
                </button>
              ))}
            </div>
          ) : undefined
        }
      />
      <Card>
        <div className={styles.filters}>
          <AutocompleteInput
            label={t('sharing.consumer')}
            value={consumer}
            onChange={setConsumer}
            options={consumers}
          />
          <AutocompleteInput
            label={t('sharing.owner')}
            value={owner}
            onChange={setOwner}
            options={owners}
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
            label={t('sharing.model')}
            value={model}
            onChange={setModel}
            options={models}
          />
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
          <div className={styles.actions}>
            <Button onClick={apply}>{t('sharing.apply_filter')}</Button>
            <Button variant="secondary" onClick={reset}>
              {t('sharing.clear_filter')}
            </Button>
          </div>
        </div>
        {keyID && (
          <p className={styles.meta}>
            <span className={styles.badge}>
              {t('sharing.key_chip', { id: shortTrace(keyID) })}
            </span>{' '}
            <button
              type="button"
              className={styles.boardDelete}
              onClick={() => {
                const next = new URLSearchParams(params);
                next.delete('key_id');
                next.delete('page');
                setParams(next);
              }}
            >
              {t('sharing.clear_filter')}
            </button>
          </p>
        )}
      </Card>
      <div className={styles.statCards}>
        {stats.map((stat) => (
          <div key={stat.label} className={styles.statCard}>
            <span className={styles.statLabel}>{stat.label}</span>
            <strong className={styles.statValue}>{stat.value}</strong>
            {stat.hint && <span className={styles.statHint}>{stat.hint}</span>}
          </div>
        ))}
      </div>
      <Card title={t('sharing.trend_title')}>
        {loading && !data ? null : <DailyTrend groups={groups} />}
      </Card>
      <div className={styles.actions}>
        <div className={styles.segmented}>
          {(['detail', 'calls', 'groups'] as const).map((value) => (
            <button
              key={value}
              type="button"
              className={`${styles.segmentedItem} ${tab === value ? styles.active : ''}`}
              onClick={() => setTab(value)}
            >
              {value === 'detail'
                ? t('sharing.tab_executions')
                : value === 'calls'
                  ? t('sharing.tab_calls')
                  : t('sharing.tab_summary')}
            </button>
          ))}
        </div>
      </div>
      <p className="hint">{tabHint}</p>
      <LoadState loading={loading} error={error} />
      {tab === 'detail' && (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                {['time', 'consumer', 'source', 'model', 'tokens_io', 'status_label', 'col_latency'].map(
                  (key) => (
                    <TableHead key={key}>{t('sharing.' + key)}</TableHead>
                  )
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.usage?.items.map((entry) => {
                const failed = entry.failed || entry.status >= 400;
                const rate = tokensPerSecond(entry);
                return (
                  <TableRow
                    key={entry.id}
                    className={styles.clickableRow}
                    onClick={() => setSelected(entry)}
                  >
                    <TableCell>
                      <div className={`${styles.cellPrimary} ${styles.mono}`}>{date(entry.at)}</div>
                    </TableCell>
                    <TableCell>
                      <div className={styles.cellPrimary}>{entry.consumer}</div>
                      {entry.key_name && (
                        <div className={styles.cellSecondary}>
                          {t('sharing.line_key', { name: entry.key_name })}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className={styles.cellPrimary}>{entry.channel}</div>
                      <div className={styles.cellSecondary}>
                        {t('sharing.line_owner', { name: entry.owner })}
                      </div>
                      {entry.credential_name && (
                        <div className={styles.cellSecondary}>
                          {t('sharing.line_credential', {
                            name: stripJsonExt(entry.credential_name),
                          })}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className={styles.cellPrimary}>
                        {entry.provider && (
                          <span className={styles.badge}>{entry.provider}</span>
                        )}{' '}
                        <span className={styles.mono}>{entry.model}</span>
                      </div>
                      <div className={styles.cellSecondary}>
                        {entry.endpoint} ·{' '}
                        {t(entry.stream ? 'sharing.stream' : 'sharing.nonstream')}
                      </div>
                      {entry.trace_id && (
                        <div
                          className={`${styles.cellSecondary} ${styles.mono}`}
                          title={entry.trace_id}
                        >
                          {t('sharing.line_trace', { id: shortTrace(entry.trace_id) })}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      {entry.tokens_known ? (
                        <>
                          <div className={styles.cellPrimary}>
                            {number(entry.input_tokens)} / {number(entry.output_tokens)} /{' '}
                            {number(entry.total_tokens)}
                          </div>
                          {(() => {
                            const parts = [
                              entry.cache_read_tokens > 0
                                ? t('sharing.token_cache', {
                                    count: number(entry.cache_read_tokens),
                                  })
                                : '',
                              entry.reasoning_tokens > 0
                                ? t('sharing.token_reasoning', {
                                    count: number(entry.reasoning_tokens),
                                  })
                                : '',
                            ].filter(Boolean);
                            return parts.length > 0 ? (
                              <div className={styles.cellSecondary}>{parts.join(' · ')}</div>
                            ) : null;
                          })()}
                        </>
                      ) : (
                        <span className={styles.cellSecondary}>{t('sharing.unknown')}</span>
                      )}
                    </TableCell>
                    <TableCell>{statusPill(entry.status, entry.failed)}</TableCell>
                    <TableCell>
                      <div className={`${styles.latency} ${failed ? styles.latencyFail : ''}`}>
                        <div className={styles.cellPrimary}>{duration(entry.latency_ms)}</div>
                        {rate !== null && (
                          <div className={styles.cellSecondary}>{rate} t/s</div>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
          {!loading && !data?.usage?.items.length && (
            <EmptyState title={t('sharing.no_records')} />
          )}
          <Pager
            page={page}
            total={data?.usage?.total || 0}
            pageSize={pageSize}
            onPageSize={setPageSize}
            onPage={(value) => {
              const next = new URLSearchParams(params);
              next.set('page', String(value));
              setParams(next);
            }}
          />
        </>
      )}
      {tab === 'calls' && (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                {['time', 'consumer', 'endpoint', 'model', 'status_label', 'col_latency'].map(
                  (key) => (
                    <TableHead key={key}>{t('sharing.' + key)}</TableHead>
                  )
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.calls?.items.map((entry) => (
                <TableRow key={entry.id}>
                  <TableCell>
                    <div className={`${styles.cellPrimary} ${styles.mono}`}>{date(entry.at)}</div>
                  </TableCell>
                  <TableCell>
                    <div className={styles.cellPrimary}>{entry.consumer}</div>
                    {entry.key_name && (
                      <div className={styles.cellSecondary}>
                        {t('sharing.line_key', { name: entry.key_name })}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <span className={styles.mono}>{entry.endpoint}</span>
                  </TableCell>
                  <TableCell>
                    <span className={styles.mono}>{entry.model}</span>
                  </TableCell>
                  <TableCell>{statusPill(entry.status)}</TableCell>
                  <TableCell>
                    <div
                      className={`${styles.latency} ${entry.status >= 400 ? styles.latencyFail : ''}`}
                    >
                      <div className={styles.cellPrimary}>{duration(entry.latency_ms)}</div>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!loading && !data?.calls?.items.length && <EmptyState title={t('sharing.no_records')} />}
          <Pager
            page={page}
            total={data?.calls?.total || 0}
            pageSize={pageSize}
            onPageSize={setPageSize}
            onPage={(value) => {
              const next = new URLSearchParams(params);
              next.set('page', String(value));
              setParams(next);
            }}
          />
        </>
      )}
      {tab === 'groups' && (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                {[
                  'col_day',
                  'consumer',
                  'source',
                  'model',
                  'calls',
                  'executions',
                  'failures',
                  'total_tokens',
                ].map((key) => (
                  <TableHead key={key}>{t('sharing.' + key)}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {groups.map((entry, index) => (
                <TableRow key={index}>
                  <TableCell>{entry.day}</TableCell>
                  <TableCell>{entry.consumer}</TableCell>
                  <TableCell>
                    <div className={styles.cellPrimary}>{entry.channel}</div>
                    <div className={styles.cellSecondary}>
                      {t('sharing.line_owner', { name: entry.owner })}
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className={styles.mono}>{entry.model}</span>
                  </TableCell>
                  <TableCell>{number(entry.calls)}</TableCell>
                  <TableCell>{number(entry.executions)}</TableCell>
                  <TableCell>{number(entry.failures)}</TableCell>
                  <TableCell>{number(entry.total_tokens)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!loading && !groups.length && <EmptyState title={t('sharing.no_records')} />}
        </>
      )}
      <UsageDetailSheet entry={selected} onClose={() => setSelected(null)} />
    </div>
  );
}

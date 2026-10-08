import { useMemo, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { Select } from '@/components/ui/Select';
import { number } from './hooks';
import type { Summary } from './types';
import styles from './Sharing.module.scss';

export function PageHeader({
  title,
  hint,
  actions,
}: {
  title: string;
  hint?: string;
  actions?: ReactNode;
}) {
  return (
    <header className={styles.header}>
      <div>
        <h1>{title}</h1>
        {hint && <p>{hint}</p>}
      </div>
      <div className={styles.actions}>{actions}</div>
    </header>
  );
}

export function LoadState({ loading, error }: { loading: boolean; error: string }) {
  if (error)
    return (
      <div className="error-box" role="alert">
        {error}
      </div>
    );
  return loading ? <LoadingSpinner /> : null;
}

export function Pager({
  page,
  total,
  pageSize = 50,
  onPage,
  onPageSize,
}: {
  page: number;
  total: number;
  pageSize?: number;
  onPage: (page: number) => void;
  onPageSize?: (size: number) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className={styles.pager}>
      <Button variant="secondary" size="sm" disabled={page <= 1} onClick={() => onPage(page - 1)}>
        {t('auth_files.pagination_prev')}
      </Button>
      <span>{t('sharing.pagination', { page, total })}</span>
      <Button
        variant="secondary"
        size="sm"
        disabled={page * pageSize >= total}
        onClick={() => onPage(page + 1)}
      >
        {t('auth_files.pagination_next')}
      </Button>
      {onPageSize && (
        <span className={styles.pageSize}>
          {t('sharing.page_size')}
          <Select
            value={String(pageSize)}
            onChange={(value) => onPageSize(Number(value))}
            ariaLabel={t('sharing.page_size')}
            options={['50', '100', '200'].map((value) => ({ value, label: value }))}
          />
        </span>
      )}
    </div>
  );
}

/** Daily token bars aggregated from summary groups; maxDays keeps the latest N days. */
export function DailyTrend({ groups, maxDays }: { groups: Summary[]; maxDays?: number }) {
  const { t } = useTranslation();
  const days = useMemo(() => {
    const byDay = new Map<string, { day: string; tokens: number; calls: number }>();
    groups.forEach((group) => {
      if (!group.day) return;
      const entry = byDay.get(group.day) || { day: group.day, tokens: 0, calls: 0 };
      entry.tokens += group.total_tokens;
      entry.calls += group.calls;
      byDay.set(group.day, entry);
    });
    const all = [...byDay.values()].sort((a, b) => a.day.localeCompare(b.day));
    return maxDays && all.length > maxDays ? all.slice(-maxDays) : all;
  }, [groups, maxDays]);
  if (!days.length) return <p className={styles.chartEmpty}>{t('sharing.trend_empty')}</p>;
  const max = Math.max(...days.map((entry) => entry.tokens), 1);
  const mid = days.length > 2 ? days[Math.floor(days.length / 2)] : null;
  return (
    <div className={styles.dayChart}>
      <div className={styles.dayBars}>
        {days.map((entry, index) => (
          <div
            key={entry.day}
            className={styles.dayBar}
            title={t('sharing.trend_point', {
              date: entry.day,
              calls: number(entry.calls),
              tokens: number(entry.tokens),
            })}
          >
            <div
              className={styles.dayBarFill}
              style={{
                height: `${Math.max(2, (entry.tokens / max) * 100)}%`,
                animationDelay: `${Math.min(index * 12, 400)}ms`,
              }}
            />
          </div>
        ))}
      </div>
      <div className={styles.chartAxis}>
        <span>{days[0].day.slice(5)}</span>
        {mid && <span>{mid.day.slice(5)}</span>}
        {days.length > 1 && <span>{days[days.length - 1].day.slice(5)}</span>}
      </div>
    </div>
  );
}

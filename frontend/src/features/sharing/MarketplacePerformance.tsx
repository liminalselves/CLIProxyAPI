import { useCallback, useMemo } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Modal } from '@/components/ui/Modal';
import { getTypeLabel } from '@/features/authFiles/constants';
import { number } from './hooks';
import {
  formatMS,
  formatPercent,
  formatTPS,
  marketplaceUsagePath,
  mergePerformance,
  performanceMetrics,
  type MarketplaceModelGroup,
  type PerformanceMetrics,
} from './marketplace';
import type { MarketplaceModel, ModelPerformance, PerformanceBucket } from './types';
import styles from './Sharing.module.scss';

interface MarketplacePerformanceProps {
  group: MarketplaceModelGroup;
  performance: ModelPerformance[];
  onClose: () => void;
}

const hourLabel = (unix: number): string => {
  const at = new Date(unix * 1000);
  return `${String(at.getHours()).padStart(2, '0')}:00`;
};

interface TrendChartProps {
  buckets: PerformanceBucket[];
  pick: (metrics: PerformanceMetrics) => number | null;
  format: (value: number) => string;
  tone: 'primary' | 'success';
  label: string;
}

const CHART_W = 600;
const CHART_H = 160;
const PAD_X = 8;
const PAD_Y = 16;

function TrendChart({ buckets, pick, format, tone, label }: TrendChartProps) {
  const { t } = useTranslation();
  const points = useMemo(
    () =>
      buckets.map((bucket, index) => ({
        x:
          buckets.length > 1
            ? PAD_X + (index * (CHART_W - 2 * PAD_X)) / (buckets.length - 1)
            : CHART_W / 2,
        value: pick(performanceMetrics(bucket)),
        start: bucket.start,
      })),
    [buckets, pick]
  );
  const values = points.map((point) => point.value).filter((value): value is number => value !== null);
  if (!values.length) {
    return <div className={styles.chartEmpty}>{t('sharing.no_performance')}</div>;
  }
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || Math.max(max, 1) * 0.1;
  const low = min - span * 0.15;
  const high = max + span * 0.15;
  const y = (value: number) => PAD_Y + (1 - (value - low) / (high - low)) * (CHART_H - 2 * PAD_Y);

  // Split into contiguous runs so missing buckets become gaps, not cliffs.
  const runs: string[] = [];
  let current: string[] = [];
  for (const point of points) {
    if (point.value === null) {
      if (current.length > 1) runs.push(current.join(' '));
      current = [];
      continue;
    }
    current.push(`${point.x.toFixed(1)},${y(point.value).toFixed(1)}`);
  }
  if (current.length > 1) runs.push(current.join(' '));

  return (
    <figure className={styles.trendChart} aria-label={label}>
      <div className={styles.chartScale}>
        <span>{format(max)}</span>
        <span>{format(min)}</span>
      </div>
      <svg viewBox={`0 0 ${CHART_W} ${CHART_H}`} role="img" aria-hidden="true">
        <line className={styles.chartGrid} x1={0} x2={CHART_W} y1={y(max)} y2={y(max)} />
        <line className={styles.chartGrid} x1={0} x2={CHART_W} y1={y(min)} y2={y(min)} />
        {runs.map((run) => (
          <polyline key={run} className={`${styles.chartLine} ${styles[tone]}`} points={run} />
        ))}
        {points.map((point, index) =>
          point.value === null ? null : (
            <circle
              key={index}
              className={`${styles.chartDot} ${styles[tone]}`}
              cx={point.x}
              cy={y(point.value)}
              r={3}
            >
              <title>{`${hourLabel(point.start)} · ${format(point.value)}`}</title>
            </circle>
          )
        )}
      </svg>
      <div className={styles.chartAxis}>
        <span>{hourLabel(buckets[0].start)}</span>
        <span>{hourLabel(buckets[buckets.length - 1].start)}</span>
      </div>
    </figure>
  );
}

const rangeText = (values: number[]): string => {
  const sorted = [...new Set(values.filter((value) => value > 0))].sort((a, b) => a - b);
  if (!sorted.length) return '';
  return sorted.length === 1
    ? number(sorted[0])
    : `${number(sorted[0])}–${number(sorted[sorted.length - 1])}`;
};

const thinkingText = (route: MarketplaceModel): string => {
  const thinking = route.thinking;
  if (!thinking) return '';
  if (thinking.levels?.length) return thinking.levels.join(', ');
  if (thinking.max && thinking.max > 0) {
    return `${number(thinking.min ?? 0)}–${number(thinking.max)}`;
  }
  return '';
};

export function MarketplacePerformance({ group, performance, onClose }: MarketplacePerformanceProps) {
  const { t } = useTranslation();
  const routes = group.routes;
  const performanceByModel = useMemo(
    () => new Map(performance.map((entry) => [entry.model, entry])),
    [performance]
  );
  const merged = useMemo(() => mergePerformance(performance), [performance]);
  const overall = merged ? performanceMetrics(merged.totals) : null;

  const description = routes.map((route) => route.description?.trim()).find(Boolean);
  const providerNames = [...new Set(routes.map((route) => getTypeLabel(t, route.provider)))];
  const contextRange = rangeText(routes.map((route) => route.context_length));
  const outputRange = rangeText(routes.map((route) => route.max_completion_tokens ?? 0));
  const thinking = [...new Set(routes.map(thinkingText).filter(Boolean))];

  const pickTTFT = useCallback((m: PerformanceMetrics) => m.avgTTFTMS, []);
  const pickSuccess = useCallback((m: PerformanceMetrics) => m.successRate, []);
  const formatRate = useCallback((value: number) => `${(value * 100).toFixed(2)}%`, []);
  const formatLatency = useCallback((value: number) => formatMS(value), []);

  return (
    <Modal open onClose={onClose} title={group.baseModel} width={720}>
      <div className={styles.performance}>
        {description && <p className={styles.marketplaceDescription}>{description}</p>}
        <div className={styles.specs}>
          <span className={styles.chip}>
            <span className={styles.chipLabel}>{t('sharing.provider')}</span>
            {providerNames.join(', ')}
          </span>
          {contextRange && (
            <span className={styles.chip}>
              <span className={styles.chipLabel}>{t('sharing.context_length')}</span>
              {contextRange}
            </span>
          )}
          {outputRange && (
            <span className={styles.chip}>
              <span className={styles.chipLabel}>{t('sharing.max_output')}</span>
              {outputRange}
            </span>
          )}
          {thinking.map((text) => (
            <span key={text} className={styles.chip}>
              <span className={styles.chipLabel}>{t('sharing.thinking_levels')}</span>
              {text}
            </span>
          ))}
        </div>
        {!overall || overall.requests === 0 ? (
          <p className={styles.chartEmpty}>{t('sharing.no_performance')}</p>
        ) : (
          <>
            <div className={styles.statCards}>
              <div className={styles.statCard}>
                <span className={styles.statLabel}>TPS</span>
                <strong className={styles.statValue}>{formatTPS(overall.tps)}</strong>
                <span className={styles.statHint}>{t('sharing.tps_hint')}</span>
              </div>
              <div className={styles.statCard}>
                <span className={styles.statLabel}>{t('sharing.avg_latency')}</span>
                <strong className={styles.statValue}>{formatMS(overall.avgLatencyMS)}</strong>
                <span className={styles.statHint}>{t('sharing.performance_window')}</span>
              </div>
              <div className={styles.statCard}>
                <span className={styles.statLabel}>{t('sharing.success_rate')}</span>
                <strong className={styles.statValue}>{formatPercent(overall.successRate)}</strong>
                <span className={styles.statHint}>
                  {(merged?.totals.failures ?? 0) === 0
                    ? t('sharing.no_anomalies')
                    : t('sharing.performance_window')}
                </span>
              </div>
            </div>
          </>
        )}
        <section className={styles.performanceSection}>
          <h3>{t('sharing.per_channel_performance')}</h3>
          <p className={styles.sectionHint}>{t('sharing.per_channel_performance_hint')}</p>
          <div className={styles.tableScroll}>
            <table className={styles.performanceTable}>
              <thead>
                <tr>
                  <th>{t('sharing.call_id')}</th>
                  <th>{t('sharing.channel')}</th>
                  <th>{t('sharing.owner_label')}</th>
                  <th>TPS</th>
                  <th>{t('sharing.avg_ttft')}</th>
                  <th>{t('sharing.avg_latency')}</th>
                  <th>{t('sharing.success_rate')}</th>
                  <th>{t('sharing.requests_label')}</th>
                  <th>{t('sharing.status_label')}</th>
                </tr>
              </thead>
              <tbody>
                {routes.map((route) => {
                  const entry = performanceByModel.get(route.id);
                  const metrics = entry ? performanceMetrics(entry.totals) : null;
                  return (
                    <tr key={route.id}>
                      <td>
                        <code>{route.id}</code>
                      </td>
                      <td>{route.channel_slug}</td>
                      <td>{route.owner}</td>
                      <td>{formatTPS(metrics?.tps ?? null)}</td>
                      <td>{formatMS(metrics?.avgTTFTMS ?? null)}</td>
                      <td>{formatMS(metrics?.avgLatencyMS ?? null)}</td>
                      <td>{formatPercent(metrics?.successRate ?? null)}</td>
                      <td>{metrics ? number(metrics.requests) : '—'}</td>
                      <td>
                        <span
                          className={`${styles.status} ${route.available ? styles.active : styles.inactive}`}
                        >
                          {t(route.available ? 'sharing.available' : 'sharing.unavailable')}
                        </span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
        {overall && overall.requests > 0 && merged && (
          <>
            <section className={styles.performanceSection}>
              <h3>{t('sharing.latency_trend')}</h3>
              <p className={styles.sectionHint}>{t('sharing.avg_ttft')}</p>
              <TrendChart
                buckets={merged.buckets}
                pick={pickTTFT}
                format={formatLatency}
                tone="primary"
                label={t('sharing.latency_trend')}
              />
            </section>
            <section className={styles.performanceSection}>
              <h3>{t('sharing.availability_trend')}</h3>
              <p className={styles.sectionHint}>{t('sharing.availability_hint')}</p>
              <TrendChart
                buckets={merged.buckets}
                pick={pickSuccess}
                format={formatRate}
                tone="success"
                label={t('sharing.availability_trend')}
              />
            </section>
          </>
        )}
        <div className={styles.cardActions}>
          {routes.map((route) => (
            <Link
              key={route.id}
              to={marketplaceUsagePath(route)}
              onClick={onClose}
              className={styles.detailLink}
            >
              {t('sharing.view_usage')}（{route.channel_slug}）
            </Link>
          ))}
        </div>
      </div>
    </Modal>
  );
}

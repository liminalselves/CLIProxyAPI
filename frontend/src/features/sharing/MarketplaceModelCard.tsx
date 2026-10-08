import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { IconCopy, IconModelCluster } from '@/components/ui/icons';
import { getTypeLabel } from '@/features/authFiles/constants';
import {
  bestPerformance,
  compactTokens,
  formatMS,
  formatTPS,
  type MarketplaceModelGroup,
} from './marketplace';
import type { ModelPerformance } from './types';
import styles from './Sharing.module.scss';

interface MarketplaceModelCardProps {
  group: MarketplaceModelGroup;
  performance: ModelPerformance[];
  onCopy: (modelID: string) => void | Promise<void>;
  onShowDetail: (group: MarketplaceModelGroup) => void;
}

export function MarketplaceModelCard({
  group,
  performance,
  onCopy,
  onShowDetail,
}: MarketplaceModelCardProps) {
  const { t } = useTranslation();
  const titleID = useId();
  const routes = group.routes;
  if (!routes.length) return null;

  const anyAvailable = routes.some((route) => route.available);
  const best = bestPerformance(performance);
  const providerNames = [...new Set(routes.map((route) => getTypeLabel(t, route.provider)))];
  const contexts = [...new Set(routes.map((route) => route.context_length).filter((v) => v > 0))];
  const contextText =
    contexts.length === 1
      ? compactTokens(contexts[0])
      : contexts.length > 1
        ? `${compactTokens(Math.min(...contexts))}–${compactTokens(Math.max(...contexts))}`
        : '';

  return (
    <article className={styles.marketplaceEntry} aria-labelledby={titleID}>
      <Card
        className={styles.marketplaceCard}
        title={
          <div className={styles.marketplaceTitle}>
            <span className={styles.marketplaceIcon} aria-hidden="true">
              <IconModelCluster size={18} />
            </span>
            <h2 id={titleID}>{group.baseModel}</h2>
          </div>
        }
        extra={
          <span className={styles.cardActions}>
            <button
              type="button"
              className={styles.detailLink}
              onClick={() => onShowDetail(group)}
            >
              {t('sharing.detail')}
            </button>
          </span>
        }
      >
        <div className={styles.marketplaceBody}>
          <div className={styles.callIdList}>
            {routes.map((route) => (
              <button
                key={route.id}
                type="button"
                className={styles.callId}
                title={t('sharing.copy_model')}
                onClick={() => void onCopy(route.id)}
              >
                <code>{route.id}</code>
                <IconCopy size={13} />
              </button>
            ))}
          </div>
          <div className={styles.cardFooter}>
            <div className={styles.cardTags}>
              <span className={styles.tagMeta}>{providerNames.join(', ')}</span>
              {contextText && <span className={styles.tagMeta}>{contextText}</span>}
            </div>
            <dl className={styles.miniMetrics}>
              <div className={styles.miniMetric}>
                <dt>{t('sharing.metric_latency')}</dt>
                <dd>{formatMS(best.avgLatencyMS)}</dd>
              </div>
              <div className={styles.miniMetric}>
                <dt>{t('sharing.metric_tps')}</dt>
                <dd>{formatTPS(best.tps)}</dd>
              </div>
              <div className={styles.miniMetric}>
                <dt>{t('sharing.status_label')}</dt>
                <dd>
                  <span
                    className={`${styles.statusDot} ${anyAvailable ? styles.active : styles.inactive}`}
                    aria-hidden="true"
                  />
                </dd>
              </div>
            </dl>
          </div>
        </div>
      </Card>
    </article>
  );
}

import { useCallback, useEffect, useMemo, useState } from 'react';
import { paginate } from '@/utils/pagination';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { EmptyState } from '@/components/ui/EmptyState';
import { Modal } from '@/components/ui/Modal';
import { useAuthStore } from '@/stores';
import { sharingApi } from '@/services/api/sharing';
import { getTypeLabel } from '@/features/authFiles/constants';
import { LoadState, PageHeader, Pager } from './shared';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { useSharingActions, useSharingData } from './hooks';
import { groupMarketplaceModels, type MarketplaceModelGroup } from './marketplace';
import { MarketplaceModelCard } from './MarketplaceModelCard';
import { MarketplacePerformance } from './MarketplacePerformance';
import styles from './Sharing.module.scss';

export function MarketplacePage() {
  const { t } = useTranslation();
  const base = useAuthStore((state) => state.apiBase);
  const { data, loading, error, refresh: refreshModels } = useSharingData(
    useCallback((signal: AbortSignal) => sharingApi.marketplace(signal), []),
    { headerRefresh: false }
  );
  const {
    data: performance,
    loading: performanceLoading,
    error: performanceError,
    refresh: refreshPerformance,
  } = useSharingData(
    useCallback((signal: AbortSignal) => sharingApi.marketplacePerformance(signal), []),
    { headerRefresh: false }
  );
  const refresh = useCallback(async () => {
    await Promise.all([refreshModels(), refreshPerformance()]);
  }, [refreshModels, refreshPerformance]);
  useHeaderRefresh(refresh);
  const { copy } = useSharingActions(refresh);
  const [search, setSearch] = useState('');
  const [owner, setOwner] = useState('');
  const [provider, setProvider] = useState('');
  const [page, setPage] = useState(1);
  const [instructions, setInstructions] = useState(false);
  const [detail, setDetail] = useState<MarketplaceModelGroup | null>(null);
  const filtered = useMemo(
    () => groupMarketplaceModels(data?.models || [], { search, owner, provider }),
    [data, owner, provider, search]
  );
  const { pageItems, currentPage } = useMemo(() => paginate(filtered, page, 50), [filtered, page]);
  useEffect(() => {
    if (!loading && !error && page !== currentPage) setPage(currentPage);
  }, [loading, error, page, currentPage]);
  const performanceByModel = useMemo(
    () => new Map((performance?.models || []).map((entry) => [entry.model, entry])),
    [performance]
  );
  const groupPerformance = useCallback(
    (group: MarketplaceModelGroup) =>
      group.routes
        .map((route) => performanceByModel.get(route.id))
        .filter((entry) => entry !== undefined),
    [performanceByModel]
  );
  const reset = (setter: (value: string) => void, value: string) => {
    setter(value);
    setPage(1);
  };
  return (
    <div className={styles.page}>
      <PageHeader
        title={t('sharing.marketplace')}
        hint={t('sharing.marketplace_hint')}
        actions={
          <Button variant="secondary" onClick={() => setInstructions(true)}>
            {t('sharing.connect_client')}
          </Button>
        }
      />
      <Card>
        <div className={styles.filters}>
          <Input
            label={t('sharing.search_models')}
            value={search}
            onChange={(event) => reset(setSearch, event.target.value)}
          />
          <div className="form-group">
            <label>{t('sharing.owner')}</label>
            <Select
              value={owner}
              onChange={(value) => reset(setOwner, value)}
              options={[
                { value: '', label: t('sharing.all_owners') },
                ...[...new Set(data?.channels.map((channel) => channel.owner))].map((value) => ({
                  value,
                  label: value,
                })),
              ]}
            />
          </div>
          <div className="form-group">
            <label>{t('sharing.provider')}</label>
            <Select
              value={provider}
              onChange={(value) => reset(setProvider, value)}
              options={[
                { value: '', label: t('sharing.all_providers') },
                ...[...new Set(data?.models.map((model) => model.provider))].map((value) => ({
                  value,
                  label: getTypeLabel(t, value),
                })),
              ]}
            />
          </div>
        </div>
      </Card>
      <LoadState loading={loading || performanceLoading} error={error || performanceError} />
      <div className={styles.grid}>
        {pageItems.map((group) => (
          <MarketplaceModelCard
            key={group.baseModel}
            group={group}
            performance={groupPerformance(group)}
            onCopy={copy}
            onShowDetail={setDetail}
          />
        ))}
      </div>
      {!loading && !filtered.length && (
        <EmptyState title={t('sharing.no_models')} description={t('sharing.no_models_hint')} />
      )}
      {filtered.length > 50 && (
        <Pager page={currentPage} total={filtered.length} onPage={setPage} />
      )}
      {detail && (
        <MarketplacePerformance
          group={detail}
          performance={groupPerformance(detail)}
          onClose={() => setDetail(null)}
        />
      )}
      <Modal
        open={instructions}
        onClose={() => setInstructions(false)}
        title={t('sharing.connect_client')}
      >
        <Input label={t('sharing.api_base')} readOnly value={base + '/v1'} />
        <p className="hint">{t('sharing.connect_hint')}</p>
        <Link to="/keys" onClick={() => setInstructions(false)}>
          {t('sharing.keys')}
        </Link>
      </Modal>
    </div>
  );
}

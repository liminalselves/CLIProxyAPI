import { useCallback, useEffect, useState } from 'react';
import { getPagination } from '@/utils/pagination';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Button } from '@/components/ui/Button';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/Table';
import { sharingApi } from '@/services/api/sharing';
import { LoadState, PageHeader, Pager } from './shared';
import { useSharingData, date } from './hooks';
import styles from './Sharing.module.scss';

export function AuditPage() {
  const { t } = useTranslation();
  const [page, setPage] = useState(1);
  const [actor, setActor] = useState('');
  const [action, setAction] = useState('');
  const [filter, setFilter] = useState({ actor: '', action: '' });
  const { data, loading, error } = useSharingData(
    useCallback(
      (signal: AbortSignal) => sharingApi.audits({ ...filter, page, page_size: 50 }, signal),
      [filter, page]
    ),
    // Page turns keep the previous rows; changing the filter drops them.
    { keepPreviousData: false, dataKey: JSON.stringify([filter.actor, filter.action]) }
  );
  useEffect(() => {
    if (loading || error || !data) return;
    const { currentPage } = getPagination(data.total, page, 50);
    if (page !== currentPage) setPage(currentPage);
  }, [data, loading, error, page]);
  return (
    <div className={styles.page}>
      <PageHeader title={t('sharing.audit')} hint={t('sharing.audit_hint')} />
      <Card>
        <div className={styles.filters}>
          <Input
            label={t('sharing.actor')}
            value={actor}
            onChange={(event) => setActor(event.target.value)}
          />
          <Input
            label={t('sharing.action')}
            value={action}
            onChange={(event) => setAction(event.target.value)}
          />
          <div className={styles.actions}>
            <Button
              onClick={() => {
                setFilter({ actor, action });
                setPage(1);
              }}
            >
              {t('sharing.filter')}
            </Button>
          </div>
        </div>
      </Card>
      <LoadState loading={loading} error={error} />
      <Table>
        <TableHeader>
          <TableRow>
            {['time', 'actor', 'action', 'target', 'result'].map((key) => (
              <TableHead key={key}>{t('sharing.' + key)}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {data?.items.map((entry) => (
            <TableRow key={entry.id}>
              <TableCell>{date(entry.at)}</TableCell>
              <TableCell>{entry.actor}</TableCell>
              <TableCell>{entry.action}</TableCell>
              <TableCell className={styles.mono}>{entry.target}</TableCell>
              <TableCell>{entry.status}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Pager page={page} total={data?.total || 0} onPage={setPage} loading={loading} />
    </div>
  );
}

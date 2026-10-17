import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { useNotificationStore } from '@/stores';
import { copyToClipboard } from '@/utils/clipboard';
import { apiClient } from '@/services/api/client';

const pad2 = (value: number) => String(value).padStart(2, '0');
export const date = (value: string) => {
  if (!value) return '—';
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) return '—';
  return `${time.getFullYear()}-${pad2(time.getMonth() + 1)}-${pad2(time.getDate())} ${pad2(time.getHours())}:${pad2(time.getMinutes())}:${pad2(time.getSeconds())}`;
};
export const compactDate = (value: string) => {
  if (!value) return '—';
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) return '—';
  return `${pad2(time.getMonth() + 1)}-${pad2(time.getDate())} ${pad2(time.getHours())}:${pad2(time.getMinutes())}:${pad2(time.getSeconds())}`;
};
export const number = (value: number | undefined) => Number(value || 0).toLocaleString();
export const duration = (ms: number) =>
  ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${number(ms)}ms`;

export function useSharingData<T>(
  loader: () => Promise<T>,
  {
    keepPreviousData = true,
    headerRefresh = true,
  }: {
    keepPreviousData?: boolean;
    headerRefresh?: boolean;
  } = {}
) {
  const [state, setState] = useState<{
    loader: typeof loader;
    data: T | null;
    error: string;
    loading: boolean;
  }>({ loader, data: null, error: '', loading: true });
  const revision = useRef(0);
  const refresh = useCallback(async () => {
    const id = ++revision.current;
    const connection = apiClient.getConnectionRevision();
    const isCurrent = () =>
      id === revision.current && connection === apiClient.getConnectionRevision();
    setState((current) => ({
      loader,
      data: keepPreviousData || current.loader === loader ? current.data : null,
      error: '',
      loading: true,
    }));
    try {
      const data = await loader();
      if (isCurrent()) setState({ loader, data, error: '', loading: false });
    } catch (error) {
      if (isCurrent())
        setState((current) => ({
          ...current,
          error: error instanceof Error ? error.message : String(error),
          loading: false,
        }));
    }
  }, [loader, keepPreviousData]);
  useEffect(() => {
    const scope = revision;
    void refresh();
    return () => {
      scope.current++;
    };
  }, [refresh]);
  useHeaderRefresh(refresh, headerRefresh);
  const matchesLoader = state.loader === loader;
  return {
    data: keepPreviousData || matchesLoader ? state.data : null,
    loading: !matchesLoader || state.loading,
    error: matchesLoader ? state.error : '',
    refresh,
  };
}

export function useSharingActions(refresh: () => Promise<void>) {
  const { t } = useTranslation();
  const { showNotification, showConfirmation } = useNotificationStore();
  const run = async (operation: () => Promise<unknown>) => {
    try {
      await operation();
      await refresh();
      showNotification(t('sharing.saved'), 'success');
      return true;
    } catch (error) {
      showNotification(error instanceof Error ? error.message : String(error), 'error');
      return false;
    }
  };
  const confirm = (message: string, operation: () => Promise<unknown>) =>
    showConfirmation({
      title: t('common.confirm'),
      message,
      variant: 'danger',
      onConfirm: async () => {
        await run(operation);
      },
    });
  const copy = async (value: string) => {
    if (await copyToClipboard(value)) showNotification(t('sharing.copied'), 'success');
    else showNotification(t('sharing.copy_failed'), 'error');
  };
  return { run, confirm, copy };
}

import { Navigate, useRoutes, type Location } from 'react-router-dom';
import { DashboardPage } from '@/features/dashboard/DashboardPage';
import { ProvidersWorkbenchPage } from '@/features/providers/ProvidersWorkbenchPage';
import { AuthFilesPage } from '@/features/authFiles/AuthFilesPage';
import { AuthFilesOAuthExcludedEditPage } from '@/pages/AuthFilesOAuthExcludedEditPage';
import { AuthFilesOAuthModelAliasEditPage } from '@/pages/AuthFilesOAuthModelAliasEditPage';
import { OAuthPage } from '@/pages/OAuthPage';
import { QuotaPage } from '@/features/quota/QuotaPage';
import { PluginResourcePage } from '@/features/plugins/PluginResourcePage';
import { PluginsPage } from '@/features/plugins/PluginsPage';
import { PluginStorePage } from '@/features/plugins/PluginStorePage';
import { ConfigPage } from '@/features/config/ConfigPage';
import { LogsPage } from '@/features/logs/LogsPage';
import { SystemPage } from '@/pages/SystemPage';
import { ThemePage } from '@/pages/ThemePage';
import { useAuthStore } from '@/stores';
import { SharingDashboard } from '@/features/sharing/SharingDashboard';
import { MarketplacePage } from '@/features/sharing/MarketplacePage';
import { ChannelsPage } from '@/features/sharing/ChannelsPage';
import { ChannelDetailPage } from '@/features/sharing/ChannelDetailPage';
import { KeysPage } from '@/features/sharing/KeysPage';
import { UsagePage } from '@/features/sharing/UsagePage';
import { AuditPage } from '@/features/sharing/AuditPage';
import { AccountPage } from '@/features/sharing/AccountPage';
import { MembersPage } from '@/features/sharing/MembersPage';

const createMainRoutes = (supportsPlugin: boolean) => [
  { path: '/', element: <DashboardPage /> },
  { path: '/dashboard', element: <DashboardPage /> },
  { path: '/settings', element: <Navigate to="/config" replace /> },
  { path: '/api-keys', element: <Navigate to="/config" replace /> },
  { path: '/quick-start', element: <ProvidersWorkbenchPage fixedBrand="apikeyFun" /> },
  { path: '/quick-start/*', element: <Navigate to="/quick-start" replace /> },
  { path: '/ai-providers', element: <ProvidersWorkbenchPage /> },
  { path: '/ai-providers/*', element: <Navigate to="/ai-providers" replace /> },
  { path: '/auth-files', element: <AuthFilesPage /> },
  { path: '/auth-files/oauth-excluded', element: <AuthFilesOAuthExcludedEditPage /> },
  { path: '/auth-files/oauth-model-alias', element: <AuthFilesOAuthModelAliasEditPage /> },
  { path: '/oauth', element: <OAuthPage /> },
  { path: '/quota', element: <QuotaPage /> },
  ...(supportsPlugin
    ? [
        { path: '/plugin-pages/:pluginId/:menuIndex', element: <PluginResourcePage /> },
        { path: '/plugins', element: <PluginsPage /> },
        { path: '/plugin-store', element: <PluginStorePage /> },
        { path: '/plugins/*', element: <Navigate to="/plugins" replace /> },
      ]
    : [
        { path: '/plugin-pages/*', element: <Navigate to="/" replace /> },
        { path: '/plugins/*', element: <Navigate to="/" replace /> },
        { path: '/plugin-store', element: <Navigate to="/" replace /> },
      ]),
  { path: '/config', element: <ConfigPage /> },
  { path: '/theme', element: <ThemePage /> },
  { path: '/logs', element: <LogsPage /> },
  { path: '/system', element: <SystemPage /> },
  { path: '*', element: <Navigate to="/" replace /> },
];

export function MainRoutes({ location }: { location?: Location }) {
  const supportsPlugin = useAuthStore((state) => state.supportsPlugin);
  const account = useAuthStore((state) => state.account);
  const routes = account
    ? [
        { path: '/', element: <SharingDashboard /> },
        { path: '/dashboard', element: <SharingDashboard /> },
        { path: '/marketplace', element: <MarketplacePage /> },
        { path: '/channels', element: <ChannelsPage /> },
        { path: '/channels/:id', element: <ChannelDetailPage /> },
        { path: '/auth-files', element: <AuthFilesPage /> },
        { path: '/oauth', element: <OAuthPage /> },
        { path: '/keys', element: <KeysPage /> },
        { path: '/usage', element: <UsagePage /> },
        { path: '/audit', element: <AuditPage /> },
        { path: '/account', element: <AccountPage /> },
        ...(account.role === 'admin'
          ? [
              { path: '/members', element: <MembersPage /> },
              ...createMainRoutes(supportsPlugin).filter((route) =>
                [
                  '/quota',
                  '/logs',
                  '/auth-files/oauth-excluded',
                  '/auth-files/oauth-model-alias',
                  '/config',
                  '/theme',
                  '/system',
                  '/plugins',
                  '/plugin-store',
                ].includes(route.path)
              ),
            ]
          : []),
        { path: '*', element: <Navigate to="/" replace /> },
      ]
    : createMainRoutes(supportsPlugin);
  return useRoutes(routes, location);
}

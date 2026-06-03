import React, { Suspense, lazy, useCallback, useEffect } from 'react';
import { App as AntdApp, ConfigProvider, Spin, message } from 'antd';
import { Routes, Route, Navigate, BrowserRouter, useLocation } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { translationsApi } from '@/api/translations';
import { syncCustomTranslationResources } from '@/i18n/customTranslations';
import { useAppLocale } from '@/i18n/useAppLocale';
import { useUiStore } from '@/store/uiStore';
import { useAuthStore } from '@/store/authStore';
import { useLocalizationStore } from '@/store/localizationStore';
import { getThemeConfig, getThemeCssVariables, resolveThemePalette } from '@/theme/themeConfig';
import { paletteFromProfileConfig } from '@/theme/profileConfig';
import { SSEProvider } from '@/features/realtime/SSEProvider';

const MainLayout = lazy(() => import('@/components/layout/MainLayout'));
const LoginPage = lazy(() => import('@/pages/auth/LoginPage'));
const Dashboard = lazy(() => import('@/pages/Dashboard'));
const InfoPage = lazy(() => import('@/pages/InfoPage'));
const ArticleDetailsPage = lazy(() => import('@/pages/articles/ArticleDetailsPage'));
const ArticleEditorPage = lazy(() => import('@/pages/articles/ArticleEditorPage'));
const SearchPage = lazy(() => import('@/pages/SearchPage'));
const TasksPage = lazy(() => import('@/pages/TasksPage'));
const TicketsPage = lazy(() => import('@/pages/TicketsPage'));
const TicketDetailsPage = lazy(() => import('@/pages/TicketDetailsPage'));
const TelephonyUserCallsPage = lazy(() => import('@/pages/telephony/TelephonyUserCallsPage'));
const CompanyPage = lazy(() => import('@/pages/companies/CompanyPage'));
const CompaniesListPage = lazy(() => import('@/pages/companies/CompaniesListPage'));
const AcceptancePage = lazy(() => import('@/pages/candidates/AcceptancePage'));
const NetworkAcceptancePage = lazy(() => import('@/pages/candidates/NetworkAcceptancePage'));
const ProfilePage = lazy(() => import('@/pages/ProfilePage'));
const ServerDetails = lazy(() => import('@/pages/equipment/ServerDetails'));
const FiscalDetails = lazy(() => import('@/pages/equipment/FiscalDetails'));
const WorkstationDetails = lazy(() => import('@/pages/equipment/WorkstationDetails'));
const ServersPage = lazy(() => import('@/pages/equipment/ServersPage'));
const WorkstationsPage = lazy(() => import('@/pages/equipment/WorkstationsPage'));
const FiscalsPage = lazy(() => import('@/pages/equipment/FiscalsPage'));
const UsersAdminPage = lazy(() => import('@/pages/admin/UsersAdminPage'));
const AdminTranslationsPage = lazy(() => import('@/pages/admin/AdminTranslationsPage'));
const AdminTelephonyPage = lazy(() => import('@/pages/admin/AdminTelephonyPage'));
const AdminSynchronizationsPage = lazy(() => import('@/pages/admin/AdminSynchronizationsPage'));
const ServicePointsImportPage = lazy(() => import('@/pages/admin/ServicePointsImportPage'));
const AgentsPage = lazy(() => import('@/pages/AgentsPage'));
const AgentDiagnosticsPage = lazy(() => import('@/pages/AgentDiagnosticsPage'));
const AgentObservationsPage = lazy(() => import('@/pages/AgentObservationsPage'));
const CompanyContractsReportPage = lazy(() => import('@/pages/reports/CompanyContractsReportPage'));

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

const INLINE_MESSAGE_HOST_ID = 'inline-message-host';
const MESSAGE_TOP_OFFSET = 68;

const resolveInlineMessageHost = () => document.getElementById(INLINE_MESSAGE_HOST_ID) || document.body;

const routeFallback = (
  <div style={{ display: 'flex', justifyContent: 'center', padding: 32 }}>
    <Spin size="large" />
  </div>
);

const ProtectedRoute = ({ children }: { children: React.ReactNode }) => {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
};

const AdminRoute = ({ children }: { children: React.ReactNode }) => {
  const user = useAuthStore((state) => state.user);
  const isAdmin = Boolean(user?.roles?.includes('admin'));
  if (!isAdmin) {
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
};

const SupportOrAdminRoute = ({ children }: { children: React.ReactNode }) => {
  const user = useAuthStore((state) => state.user);
  const canAccess = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('support_specialist'));
  if (!canAccess) {
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
};

const InfoArticlesRedirect = () => {
  const location = useLocation();
  return <Navigate to={`/info${location.search}`} replace />;
};

const App: React.FC = () => {
  const { antdLocale } = useAppLocale();
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const themeMode = useUiStore((state) => state.themeMode);
  const profileConfig = useAuthStore((state) => state.user?.profile_config);
  const localizationCatalog = useLocalizationStore((state) => state.catalog);
  const setLocalizationCatalog = useLocalizationStore((state) => state.setCatalog);
  const resetLocalizationCatalog = useLocalizationStore((state) => state.resetCatalog);
  const paletteByMode = paletteFromProfileConfig(profileConfig, themeMode);
  const resolvedPalette = resolveThemePalette(themeMode, paletteByMode);
  const getInlineMessageContainer = useCallback(() => resolveInlineMessageHost(), []);

  useEffect(() => {
    document.body.style.backgroundColor = resolvedPalette.bgLayout;
  }, [resolvedPalette.bgLayout]);

  useEffect(() => {
    const root = document.documentElement;
    const cssVars = getThemeCssVariables(themeMode, paletteByMode);
    Object.entries(cssVars).forEach(([name, value]) => {
      root.style.setProperty(name, value);
    });
  }, [themeMode, paletteByMode]);

  useEffect(() => {
    if (!isAuthenticated) {
      resetLocalizationCatalog();
      return;
    }

    let cancelled = false;

    void translationsApi.getCatalog()
      .then((response) => {
        if (!cancelled) {
          setLocalizationCatalog((response as any)?.data || null);
        }
      })
      .catch(() => {
        if (!cancelled) {
          resetLocalizationCatalog();
        }
      });

    return () => {
      cancelled = true;
    };
  }, [isAuthenticated, resetLocalizationCatalog, setLocalizationCatalog]);

  useEffect(() => {
    syncCustomTranslationResources(localizationCatalog);
  }, [localizationCatalog]);

  useEffect(() => {
    message.config({
      duration: 5,
      maxCount: 6,
      top: MESSAGE_TOP_OFFSET,
      getContainer: getInlineMessageContainer,
    });
  }, [getInlineMessageContainer]);

  return (
    <QueryClientProvider client={queryClient}>
      <ConfigProvider locale={antdLocale} theme={getThemeConfig(themeMode, paletteByMode)}>
        <AntdApp
          message={{
            duration: 5,
            maxCount: 6,
            top: MESSAGE_TOP_OFFSET,
            getContainer: getInlineMessageContainer,
          }}
        >
          <BrowserRouter>
            <SSEProvider>
              <Suspense fallback={routeFallback}>
                <Routes>
                  <Route path="/login" element={<LoginPage />} />

                  <Route
                    path="/"
                    element={(
                      <ProtectedRoute>
                        <MainLayout />
                      </ProtectedRoute>
                    )}
                  >
                    <Route index element={<Dashboard />} />
                    <Route path="info" element={<InfoPage />} />
                    <Route path="info/articles" element={<InfoArticlesRedirect />} />
                    <Route
                      path="info/articles/new"
                      element={(
                        <SupportOrAdminRoute>
                          <ArticleEditorPage />
                        </SupportOrAdminRoute>
                      )}
                    />
                    <Route path="info/articles/:id" element={<ArticleDetailsPage />} />
                    <Route
                      path="info/articles/:id/edit"
                      element={(
                        <SupportOrAdminRoute>
                          <ArticleEditorPage />
                        </SupportOrAdminRoute>
                      )}
                    />
                    <Route path="info/releases" element={<Navigate to="/info?type=release_note" replace />} />
                    <Route path="info/news" element={<Navigate to="/info?type=company_news" replace />} />
                    <Route path="info/wiki" element={<Navigate to="/info?type=wiki" replace />} />
                    <Route path="search" element={<SearchPage />} />
                    <Route
                      path="tasks"
                      element={(
                        <AdminRoute>
                          <TasksPage />
                        </AdminRoute>
                      )}
                    />
                    <Route path="tickets" element={<TicketsPage />} />
                    <Route path="tickets/:id" element={<TicketDetailsPage />} />
                    <Route
                      path="telephony/users/:id/calls"
                      element={(
                        <SupportOrAdminRoute>
                          <TelephonyUserCallsPage />
                        </SupportOrAdminRoute>
                      )}
                    />
                    <Route path="profile" element={<ProfilePage />} />

                    <Route path="companies" element={<CompaniesListPage />} />
                    <Route path="companies/:id" element={<CompanyPage />} />
                    <Route
                      path="acceptance"
                      element={(
                        <SupportOrAdminRoute>
                          <AcceptancePage />
                        </SupportOrAdminRoute>
                      )}
                    />
                    <Route
                      path="network-acceptance"
                      element={(
                        <SupportOrAdminRoute>
                          <NetworkAcceptancePage />
                        </SupportOrAdminRoute>
                      )}
                    />

                    <Route path="servers" element={<ServersPage />} />
                    <Route path="servers/:id" element={<ServerDetails />} />

                    <Route path="workstations" element={<WorkstationsPage />} />
                    <Route path="workstations/:id" element={<WorkstationDetails />} />

                    <Route path="fiscals" element={<FiscalsPage />} />
                    <Route path="fiscals/:id" element={<FiscalDetails />} />

                    <Route
                      path="agents"
                      element={(
                        <SupportOrAdminRoute>
                          <AgentsPage />
                        </SupportOrAdminRoute>
                      )}
                    />
                    <Route
                      path="agent-diagnostics/:uuid"
                      element={(
                        <SupportOrAdminRoute>
                          <AgentDiagnosticsPage />
                        </SupportOrAdminRoute>
                      )}
                    />
                    <Route
                      path="agent-observations"
                      element={(
                        <SupportOrAdminRoute>
                          <AgentObservationsPage />
                        </SupportOrAdminRoute>
                      )}
                    />
                    <Route
                      path="reports/companies-contracts"
                      element={(
                        <AdminRoute>
                          <CompanyContractsReportPage />
                        </AdminRoute>
                      )}
                    />
                    <Route
                      path="admin"
                      element={(
                        <AdminRoute>
                          <Navigate to="/admin/users" replace />
                        </AdminRoute>
                      )}
                    />
                    <Route
                      path="admin/users"
                      element={(
                        <AdminRoute>
                          <UsersAdminPage />
                        </AdminRoute>
                      )}
                    />
                    <Route
                      path="admin/translations"
                      element={(
                        <AdminRoute>
                          <AdminTranslationsPage />
                        </AdminRoute>
                      )}
                    />
                    <Route
                      path="admin/synchronizations"
                      element={(
                        <AdminRoute>
                          <AdminSynchronizationsPage />
                        </AdminRoute>
                      )}
                    />
                    <Route
                      path="admin/telephony"
                      element={(
                        <AdminRoute>
                          <AdminTelephonyPage />
                        </AdminRoute>
                      )}
                    />
                    <Route
                      path="admin/synchronizations/service-points-import"
                      element={(
                        <AdminRoute>
                          <ServicePointsImportPage />
                        </AdminRoute>
                      )}
                    />
                    <Route
                      path="admin/service-points-import"
                      element={<Navigate to="/admin/synchronizations/service-points-import" replace />}
                    />
                  </Route>

                  <Route path="*" element={<Navigate to="/" replace />} />
                </Routes>
              </Suspense>
            </SSEProvider>
          </BrowserRouter>
        </AntdApp>
      </ConfigProvider>
    </QueryClientProvider>
  );
};

export default App;

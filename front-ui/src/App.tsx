import React, { useEffect } from 'react';
import { ConfigProvider } from 'antd';
import { Routes, Route, Navigate, BrowserRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import ruRU from 'antd/locale/ru_RU';

import MainLayout from '@/components/layout/MainLayout';
import LoginPage from '@/pages/auth/LoginPage';
import Dashboard from '@/pages/Dashboard';
import SearchPage from '@/pages/SearchPage';
import TasksPage from '@/pages/TasksPage';
import TicketsPage from '@/pages/TicketsPage';
import TicketDetailsPage from '@/pages/TicketDetailsPage';
import CompanyPage from '@/pages/companies/CompanyPage';
import CompaniesListPage from '@/pages/companies/CompaniesListPage';
import ProfilePage from '@/pages/ProfilePage';

import ServerDetails from '@/pages/equipment/ServerDetails';
import FiscalDetails from '@/pages/equipment/FiscalDetails';
import WorkstationDetails from '@/pages/equipment/WorkstationDetails';
import ServersPage from '@/pages/equipment/ServersPage';
import WorkstationsPage from '@/pages/equipment/WorkstationsPage';
import FiscalsPage from '@/pages/equipment/FiscalsPage';
import UsersAdminPage from '@/pages/admin/UsersAdminPage';

import { useUiStore } from '@/store/uiStore';
import { useAuthStore } from '@/store/authStore';
import { getThemeConfig } from '@/theme/themeConfig';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

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

const App: React.FC = () => {
  const themeMode = useUiStore((state) => state.themeMode);

  useEffect(() => {
    const colorBgLayout = themeMode === 'dark' ? '#000000' : '#f0f2f5';
    document.body.style.backgroundColor = colorBgLayout;
  }, [themeMode]);

  return (
    <QueryClientProvider client={queryClient}>
      <ConfigProvider locale={ruRU} theme={getThemeConfig(themeMode)}>
        <BrowserRouter>
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
              <Route path="profile" element={<ProfilePage />} />

              <Route path="companies" element={<CompaniesListPage />} />
              <Route path="companies/:id" element={<CompanyPage />} />

              <Route path="servers" element={<ServersPage />} />
              <Route path="servers/:id" element={<ServerDetails />} />

              <Route path="workstations" element={<WorkstationsPage />} />
              <Route path="workstations/:id" element={<WorkstationDetails />} />

              <Route path="fiscals" element={<FiscalsPage />} />
              <Route path="fiscals/:id" element={<FiscalDetails />} />

              <Route
                path="admin"
                element={(
                  <AdminRoute>
                    <UsersAdminPage />
                  </AdminRoute>
                )}
              />
            </Route>

            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </BrowserRouter>
      </ConfigProvider>
    </QueryClientProvider>
  );
};

export default App;

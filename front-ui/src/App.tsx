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
import CompanyPage from '@/pages/companies/CompanyPage';

import { useUiStore } from '@/store/uiStore';
import { useAuthStore } from '@/store/authStore';
import { getThemeConfig } from '@/theme/themeConfig';

// Настройка React Query
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

// Компонент защиты роутов
const ProtectedRoute = ({ children }: { children: React.ReactNode }) => {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
};

const App: React.FC = () => {
  const themeMode = useUiStore((state) => state.themeMode);
  
  // Обновляем логику фона: синхронизируем с themeConfig
  useEffect(() => {
    // #000000 для темной, #f0f2f5 для светлой
    const colorBgLayout = themeMode === 'dark' ? '#000000' : '#f0f2f5';
    document.body.style.backgroundColor = colorBgLayout;
  }, [themeMode]);

  return (
    <QueryClientProvider client={queryClient}>
      <ConfigProvider 
        locale={ruRU}
        theme={getThemeConfig(themeMode)}
      >
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            
            <Route path="/" element={
              <ProtectedRoute>
                <MainLayout />
              </ProtectedRoute>
            }>
              <Route index element={<Dashboard />} />
              <Route path="search" element={<SearchPage />} />
              <Route path="tasks" element={<TasksPage />} />
              <Route path="tickets" element={<div>Тикеты</div>} />
              
              <Route path="companies" element={<div>Список компаний</div>} />
              <Route path="companies/:id" element={<CompanyPage />} />
              
              {/* Роуты для оборудования */}
              <Route path="servers" element={<div>Список серверов</div>} />
              <Route path="servers/:id" element={<div>Детали сервера (в разработке)</div>} />
              
              <Route path="workstations" element={<div>Список РС</div>} />
              <Route path="workstations/:id" element={<div>Детали РС (в разработке)</div>} />
              
              <Route path="fiscals" element={<div>Список ФР</div>} />
              <Route path="fiscals/:id" element={<div>Детали ФР (в разработке)</div>} />
              
              <Route path="admin" element={<div>Админка</div>} />
            </Route>
            
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </BrowserRouter>
      </ConfigProvider>
    </QueryClientProvider>
  );
};

export default App;
import React, { useEffect } from 'react';
import { ConfigProvider } from 'antd';
import { Routes, Route, Navigate, BrowserRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import ruRU from 'antd/locale/ru_RU';

import MainLayout from '@/components/layout/MainLayout';
import LoginPage from '@/pages/auth/LoginPage';
import Dashboard from '@/pages/Dashboard';

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
  // ReactNode валиден для возврата
  return <>{children}</>;
};

const App: React.FC = () => {
  const themeMode = useUiStore((state) => state.themeMode);
  
  // Применение темы к body для смены общего фона (за пределами React компонентов)
  useEffect(() => {
    const colorBgLayout = themeMode === 'dark' ? '#0a111b' : '#f0f5ff';
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
              {/* Вложенные роуты внутри MainLayout */}
              <Route index element={<Dashboard />} />
              <Route path="tasks" element={<div>Задачи</div>} />
              <Route path="tickets" element={<div>Тикеты</div>} />
              <Route path="companies" element={<div>Компании</div>} />
              <Route path="servers" element={<div>Серверы</div>} />
              <Route path="workstations" element={<div>РС</div>} />
              <Route path="fiscals" element={<div>ФР</div>} />
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
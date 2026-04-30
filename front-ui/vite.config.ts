import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const apiTarget = env.VITE_API_TARGET || 'http://localhost:8080';

  return {
    plugins: [react()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    server: {
      // Важно для Docker/Remote контейнеров: слушать все интерфейсы
      host: true,
      // Разрешаем доступ из контейнера Playwright (MCP)
      allowedHosts: ['host.docker.internal', 'localhost', '127.0.0.1'],
      // Явно задаем порт
      port: 5178,
      // Если порт занят, Vite выйдет с ошибкой, а не будет искать следующий
      strictPort: true,
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
        },
      },
    },
  };
});

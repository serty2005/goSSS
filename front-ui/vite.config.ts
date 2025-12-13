import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    // Важно для Docker/Remote контейнеров: слушать все интерфейсы
    host: true, 
    // Явно задаем порт
    port: 5173,
    // Если порт занят, Vite выйдет с ошибкой, а не будет искать следующий
    strictPort: true,
    proxy: {
      '/api': {
        // Если бэкенд тоже запущен внутри этого контейнера на 9999:
        target: 'http://etalon-server:9999',
        // Если бэкенд на хост-машине, возможно понадобится http://host.docker.internal:9999
        changeOrigin: true,
      },
    },
  },
});
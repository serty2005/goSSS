// Исправленные импорты: разделяем значения и типы
import { theme } from 'antd';
import type { ThemeConfig } from 'antd';

// Цветовые палитры для тем
const lightColors = {
  primary: '#1890ff',
  bgContainer: 'rgba(230, 247, 255, 0.65)',
  bgLayout: '#f0f5ff',
};

const darkColors = {
  primary: '#177ddc',
  bgContainer: 'rgba(17, 29, 44, 0.65)',
  bgLayout: '#0a111b',
};

export const getThemeConfig = (mode: 'light' | 'dark'): ThemeConfig => {
  const isDark = mode === 'dark';
  const colors = isDark ? darkColors : lightColors;

  return {
    // Теперь theme.darkAlgorithm и theme.defaultAlgorithm будут доступны
    algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: colors.primary,
      colorBgContainer: colors.bgContainer,
      colorBgLayout: colors.bgLayout,
      borderRadius: 8,
      wireframe: false,
      boxShadow: isDark 
        ? '0 4px 12px rgba(0, 0, 0, 0.4)' 
        : '0 4px 12px rgba(24, 144, 255, 0.15)',
    },
    components: {
      Layout: {
        colorBgHeader: isDark ? 'rgba(20, 20, 20, 0.6)' : 'rgba(255, 255, 255, 0.6)',
        colorBgTrigger: colors.primary,
      },
      Menu: {
        colorBgContainer: 'transparent',
      },
      Card: {
        colorBgContainer: colors.bgContainer,
        lineWidth: 1,
      }
    },
  };
};
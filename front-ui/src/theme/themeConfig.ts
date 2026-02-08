import { theme } from 'antd';
import type { ThemeConfig } from 'antd';

// Цветовые палитры
const lightColors = {
  primary: '#1677ff', // Чуть более глубокий синий (AntD v5 default)
  bgContainer: 'rgba(255, 255, 255, 0.85)', // Почти белый, слегка прозрачный
  bgLayout: '#f0f2f5a9', // Нейтральный светло-серый, приятный глазу
  borderColor: '#d9d9d960', // Стандартный серый бордер
};

const darkColors = {
  primary: '#177ddc',
  bgContainer: 'rgba(20, 20, 20, 0.65)', // Темный, прозрачный
  bgLayout: '#000000', // Глубокий черный для контраста в OLED стиле
  borderColor: '#303030',
};

export const getThemeConfig = (mode: 'light' | 'dark'): ThemeConfig => {
  const isDark = mode === 'dark';
  const colors = isDark ? darkColors : lightColors;

  return {
    algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: colors.primary,
      colorBgContainer: colors.bgContainer,
      colorBgLayout: colors.bgLayout,
      colorBorder: colors.borderColor, // Явный цвет границ
      borderRadius: 8,
      wireframe: false,
      // В светлой теме делаем тень мягче, но заметнее для отделения слоев
      boxShadow: isDark 
        ? '0 4px 12px rgba(0, 0, 0, 0.4)' 
        : '0 2px 8px rgba(0, 0, 0, 0.08)', 
    },
    components: {
      Layout: {
        // Хедер в светлой теме делаем чисто белым (или с легким блюром), чтобы отделить от серого фона
        headerBg: isDark ? 'rgba(20, 20, 20, 0.6)' : 'rgba(255, 255, 255, 0.7)',
      },
      Menu: {
        colorBgContainer: 'transparent',
      },
      Card: {
        colorBgContainer: colors.bgContainer,
        // Убираем лишние обводки в светлой теме, полагаемся на тень, 
        // но оставляем тонкий бордер для четкости
        lineWidth: 1, 
      }
    },
  };
};
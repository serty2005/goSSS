import { theme } from 'antd';
import type { ThemeConfig } from 'antd';

export type ThemeMode = 'light' | 'dark';

export interface ThemePalette {
  primary: string;
  bgContainer: string;
  bgLayout: string;
  borderColor: string;
}

export const defaultThemePalettes: Record<ThemeMode, ThemePalette> = {
  light: {
    primary: '#1677ff',
    bgContainer: '#ffffff',
    bgLayout: '#f0f2f5',
    borderColor: '#d9d9d9',
  },
  dark: {
    primary: '#177ddc',
    bgContainer: '#141414',
    bgLayout: '#000000',
    borderColor: '#303030',
  },
};

const normalizeHex = (value: string): string => {
  const clean = value.trim();
  if (/^#[\da-fA-F]{6}$/.test(clean)) {
    return clean.toLowerCase();
  }
  return clean;
};

const withAlpha = (value: string, alpha: number): string => {
  const color = normalizeHex(value);
  if (!/^#[\da-fA-F]{6}$/.test(color)) {
    return color;
  }

  const r = Number.parseInt(color.slice(1, 3), 16);
  const g = Number.parseInt(color.slice(3, 5), 16);
  const b = Number.parseInt(color.slice(5, 7), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
};

export const resolveThemePalette = (
  mode: ThemeMode,
  overrides?: Partial<ThemePalette>,
): ThemePalette => {
  const base = defaultThemePalettes[mode];
  return {
    primary: normalizeHex(overrides?.primary || base.primary),
    bgContainer: (overrides?.bgContainer || base.bgContainer).trim(),
    bgLayout: (overrides?.bgLayout || base.bgLayout).trim(),
    borderColor: normalizeHex(overrides?.borderColor || base.borderColor),
  };
};

export const getThemeConfig = (
  mode: ThemeMode,
  overrides?: Partial<ThemePalette>,
): ThemeConfig => {
  const isDark = mode === 'dark';
  const colors = resolveThemePalette(mode, overrides);

  return {
    algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: colors.primary,
      colorBgContainer: colors.bgContainer,
      colorBgLayout: colors.bgLayout,
      colorBorder: colors.borderColor,
      borderRadius: 8,
      wireframe: false,
      boxShadow: isDark
        ? '0 4px 12px rgba(0, 0, 0, 0.4)'
        : '0 2px 8px rgba(0, 0, 0, 0.08)',
    },
    components: {
      Layout: {
        headerBg: isDark ? withAlpha(colors.bgContainer, 0.9) : withAlpha('#ffffff', 0.78),
      },
      Menu: {
        colorBgContainer: 'transparent',
      },
      Card: {
        colorBgContainer: colors.bgContainer,
        lineWidth: 1,
      },
    },
  };
};

export const getThemeCssVariables = (
  mode: ThemeMode,
  overrides?: Partial<ThemePalette>,
): Record<string, string> => {
  const colors = resolveThemePalette(mode, overrides);
  const isDark = mode === 'dark';

  return {
    '--app-bg-layout': colors.bgLayout,
    '--app-bg-container': colors.bgContainer,
    '--app-color-primary': colors.primary,
    '--app-color-text': isDark ? 'rgba(255, 255, 255, 0.88)' : 'rgba(0, 0, 0, 0.88)',
    '--app-color-border': colors.borderColor,
    '--app-color-border-strong': withAlpha(colors.borderColor, isDark ? 0.8 : 0.55),
    '--app-color-divider': withAlpha(colors.borderColor, isDark ? 0.6 : 0.35),
    '--app-color-scrollbar': withAlpha(colors.borderColor, isDark ? 0.9 : 0.9),
    '--app-color-scrollbar-hover': withAlpha(colors.borderColor, isDark ? 1 : 1),
    '--app-color-muted-text': isDark ? 'rgba(255, 255, 255, 0.65)' : 'rgba(0, 0, 0, 0.65)',
    '--app-color-row-highlight-bg': withAlpha(colors.primary, 0.08),
    '--app-color-row-highlight-border': withAlpha(colors.primary, 0.2),
    '--app-color-primary-soft': withAlpha(colors.primary, 0.12),
  };
};

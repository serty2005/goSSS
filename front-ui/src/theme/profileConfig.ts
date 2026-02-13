import type { UserProfileConfigDTO } from '@/types/api';
import type { ThemeMode, ThemePalette } from '@/theme/themeConfig';
import { defaultThemePalettes } from '@/theme/themeConfig';

const normalizeHex = (value: unknown, fallback: string) => {
  const candidate = String(value || '').trim().toLowerCase();
  return /^#[\da-f]{6}$/.test(candidate) ? candidate : fallback;
};

export const paletteFromProfileConfig = (
  profileConfig: UserProfileConfigDTO | undefined,
  mode: ThemeMode,
): Partial<ThemePalette> => {
  const palette = profileConfig?.interface?.theme_palettes?.[mode];
  const defaults = defaultThemePalettes[mode];

  return {
    primary: normalizeHex(palette?.primary, defaults.primary),
    bgLayout: normalizeHex(palette?.bg_layout, defaults.bgLayout),
    bgContainer: normalizeHex(palette?.bg_container, defaults.bgContainer),
    borderColor: normalizeHex(palette?.border_color, defaults.borderColor),
  };
};

export const buildProfileConfigWithPalettes = (
  current: UserProfileConfigDTO | undefined,
  values: Record<ThemeMode, Pick<ThemePalette, 'primary' | 'bgLayout' | 'bgContainer' | 'borderColor'>>,
  themeMode?: ThemeMode,
): UserProfileConfigDTO => {
  const next: UserProfileConfigDTO = {
    ...(current || {}),
    interface: {
      ...(current?.interface || {}),
      ...(themeMode ? { theme_mode: themeMode } : {}),
      theme_palettes: {
        ...(current?.interface?.theme_palettes || {}),
        light: {
          primary: values.light.primary,
          bg_layout: values.light.bgLayout,
          bg_container: values.light.bgContainer,
          border_color: values.light.borderColor,
        },
        dark: {
          primary: values.dark.primary,
          bg_layout: values.dark.bgLayout,
          bg_container: values.dark.bgContainer,
          border_color: values.dark.borderColor,
        },
      },
    },
  };
  return next;
};

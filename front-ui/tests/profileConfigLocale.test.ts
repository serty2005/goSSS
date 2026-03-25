import { describe, expect, it } from 'vitest';
import { buildProfileConfigWithLocale } from '@/theme/profileConfig';

describe('buildProfileConfigWithLocale', () => {
  it('устанавливает locale и сохраняет остальные interface-настройки', () => {
    const result = buildProfileConfigWithLocale({
      interface: {
        theme_mode: 'dark',
        search: {
          cards_columns: 4,
        },
      },
      notifications: {
        personal_enabled: true,
      },
    }, 'en');

    expect(result).toEqual({
      interface: {
        theme_mode: 'dark',
        locale: 'en',
        search: {
          cards_columns: 4,
        },
      },
      notifications: {
        personal_enabled: true,
      },
    });
  });

  it('создаёт interface-блок, если его не было', () => {
    const result = buildProfileConfigWithLocale(undefined, 'ru');

    expect(result).toEqual({
      interface: {
        locale: 'ru',
      },
    });
  });
});

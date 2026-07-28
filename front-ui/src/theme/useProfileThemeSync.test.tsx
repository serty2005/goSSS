// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { useAuthStore } from '@/store/authStore';
import { useUiStore } from '@/store/uiStore';
import { useProfileThemeSync } from '@/theme/useProfileThemeSync';
import type { User } from '@/store/authStore';

const buildUser = (id: number, themeMode?: 'light' | 'dark'): User => ({
  id,
  username: `user-${id}`,
  full_name: 'Тест Тестов',
  first_name: 'Тест',
  last_name: 'Тестов',
  position: 'Инженер',
  roles: ['support'],
  schedule_type: 'default',
  is_active: true,
  has_logged_in: true,
  profile_config: themeMode ? { interface: { theme_mode: themeMode } } : {},
});

describe('useProfileThemeSync', () => {
  beforeEach(() => {
    useAuthStore.setState({ token: null, user: null, isAuthenticated: false });
    useUiStore.setState({ themeMode: 'light' });
  });

  it('поднимает режим темы из профиля, когда localStorage пуст (новый домен)', () => {
    useAuthStore.setState({ token: 't', user: buildUser(1, 'dark'), isAuthenticated: true });

    renderHook(() => useProfileThemeSync());

    expect(useUiStore.getState().themeMode).toBe('dark');
  });

  it('не перетирает ручное переключение темы после первой синхронизации', () => {
    useAuthStore.setState({ token: 't', user: buildUser(1, 'dark'), isAuthenticated: true });
    const { rerender } = renderHook(() => useProfileThemeSync());

    useUiStore.getState().setTheme('light');
    useAuthStore.setState({ user: buildUser(1, 'dark') });
    rerender();

    expect(useUiStore.getState().themeMode).toBe('light');
  });

  it('синхронизирует заново для другого пользователя', () => {
    useAuthStore.setState({ token: 't', user: buildUser(1, 'light'), isAuthenticated: true });
    const { rerender } = renderHook(() => useProfileThemeSync());

    useAuthStore.setState({ user: buildUser(2, 'dark') });
    rerender();

    expect(useUiStore.getState().themeMode).toBe('dark');
  });

  it('оставляет текущий режим, если в профиле его нет', () => {
    useUiStore.setState({ themeMode: 'dark' });
    useAuthStore.setState({ token: 't', user: buildUser(1), isAuthenticated: true });

    renderHook(() => useProfileThemeSync());

    expect(useUiStore.getState().themeMode).toBe('dark');
  });
});

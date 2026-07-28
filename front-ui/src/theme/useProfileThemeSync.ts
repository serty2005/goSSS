import { useEffect, useRef } from 'react';

import { useAuthStore } from '@/store/authStore';
import { useUiStore } from '@/store/uiStore';
import { themeModeFromProfileConfig } from '@/theme/profileConfig';

/**
 * Применяет сохранённый в профиле режим темы один раз на пользователя.
 * Дальше источником истины остаётся uiStore: переключатель темы и его откат
 * при неудачном сохранении не должны перетираться этой синхронизацией.
 */
export const useProfileThemeSync = () => {
  const userID = useAuthStore((state) => state.user?.id);
  const profileConfig = useAuthStore((state) => state.user?.profile_config);
  const setTheme = useUiStore((state) => state.setTheme);
  const syncedUserIDRef = useRef<number | null>(null);

  useEffect(() => {
    if (!userID) {
      syncedUserIDRef.current = null;
      return;
    }
    if (syncedUserIDRef.current === userID) {
      return;
    }

    const profileMode = themeModeFromProfileConfig(profileConfig);
    if (!profileMode) {
      // Профиль ещё может догрузиться (/profile/me) — пробуем на следующем изменении.
      return;
    }

    syncedUserIDRef.current = userID;
    if (useUiStore.getState().themeMode !== profileMode) {
      setTheme(profileMode);
    }
  }, [profileConfig, setTheme, userID]);
};

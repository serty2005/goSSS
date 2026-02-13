import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { UserIntegrationDTO, UserProfileConfigDTO } from '@/types/api';

// Типы согласно API Reference
export interface User {
  id: number;
  username: string;
  full_name: string;
  first_name: string;
  last_name: string;
  position: string;
  roles: string[];
  external_system_id?: string;
  external_type?: string;
  integrations?: UserIntegrationDTO[];
  profile_config?: UserProfileConfigDTO;
  schedule_type: string;
  is_active: boolean;
  has_logged_in: boolean;
}

interface AuthState {
  token: string | null;
  user: User | null;
  isAuthenticated: boolean;
  login: (token: string, user: User) => void;
  setUser: (user: User) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      user: null,
      isAuthenticated: false,
      login: (token, user) => set({ token, user, isAuthenticated: true }),
      setUser: (user) => set({ user }),
      logout: () => set({ token: null, user: null, isAuthenticated: false }),
    }),
    {
      name: 'etalon-auth-storage',
    }
  )
);


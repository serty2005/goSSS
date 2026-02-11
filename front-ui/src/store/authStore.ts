import { create } from 'zustand';
import { persist } from 'zustand/middleware';

// Типы согласно API Reference
export interface User {
  id: number;
  username: string;
  fullName: string;
  firstName: string;
  lastName: string;
  position: string;
  roles: string[];
  externalSystemId?: string;
  externalType?: string;
  scheduleType: string;
  isActive: boolean;
  hasLoggedIn: boolean;
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

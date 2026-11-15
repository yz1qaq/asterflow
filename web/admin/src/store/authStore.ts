import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

export interface User {
  id: string
  email: string
  name: string
  role: string
  status: string
  created_at?: string
}

interface AuthState {
  token: string | null
  user: User | null
  expiresAt: string | null
  setAuth: (token: string, user: User, expiresAt: string) => void
  logout: () => void
}

export function isSessionValid(state: Pick<AuthState, 'token' | 'expiresAt'>) {
  return Boolean(state.token && state.expiresAt && Date.parse(state.expiresAt) > Date.now())
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      user: null,
      expiresAt: null,
      setAuth: (token, user, expiresAt) => set({ token, user, expiresAt }),
      logout: () => set({ token: null, user: null, expiresAt: null }),
    }),
    { name: 'asterflow-auth-v1', storage: createJSONStorage(() => localStorage) },
  ),
)

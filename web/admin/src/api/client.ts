import axios from 'axios'
import { isSessionValid, useAuthStore } from '@/store/authStore'

export const api = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
})

api.interceptors.request.use((config) => {
  const state = useAuthStore.getState()
  if (state.token && !isSessionValid(state)) state.logout()
  if (config.url !== '/auth/login' && isSessionValid(state)) {
    config.headers.Authorization = `Bearer ${state.token}`
    const path = (config.url ?? '').split('?')[0]
    const unscoped = ['/me', '/users', '/workspaces'].includes(path)
      || path.startsWith('/auth/') || path.startsWith('/workspaces/')
    if (state.workspaceId && !unscoped && !config.headers.has('X-Workspace-ID')) {
      config.headers.set('X-Workspace-ID', state.workspaceId)
    }
  }
  return config
})

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (axios.isAxiosError(error) && error.response?.status === 401 && error.config?.url !== '/auth/login') {
      const session = useAuthStore.getState()
      if (!session.token || error.config?.headers?.Authorization !== `Bearer ${session.token}`) return Promise.reject(error)
      session.logout()
      if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
        window.location.assign('/login')
      }
    }
    return Promise.reject(error)
  },
)

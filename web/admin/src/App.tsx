import { lazy, Suspense, useEffect } from 'react'
import { Redirect, Route, Switch, useLocation } from 'wouter'
import { AppLayout } from '@/components/AppLayout'
import { SystemStatusPage } from '@/pages/SystemStatusPage'
import { useAuthStore } from '@/store/authStore'
const UsersPage = lazy(() => import('@/pages/UsersPage').then(module => ({ default: module.UsersPage })))
const LoginPage = lazy(() => import('@/pages/LoginPage').then(module => ({ default: module.LoginPage })))

export default function App() {
  const [location] = useLocation()
  useEffect(() => {
    document.title = `${location === '/login' ? '登录' : location === '/users' ? '用户管理' : '系统状态'} · AsterFlow`
  }, [location])
  const expiresAt = useAuthStore(state => state.expiresAt)
  const logout = useAuthStore(state => state.logout)
  useEffect(() => {
    if (!expiresAt) return
    const remaining = Date.parse(expiresAt) - Date.now()
    if (!(remaining > 0)) { logout(); return }
    const timer = window.setTimeout(logout, Math.min(remaining, 2_147_483_647))
    return () => window.clearTimeout(timer)
  }, [expiresAt, logout])

  return <Suspense fallback={<div role="status">正在加载页面…</div>}><Switch>
    <Route path="/login" component={LoginPage} />
    <Route path="/users"><AppLayout><UsersPage /></AppLayout></Route>
    <Route path="/"><AppLayout><SystemStatusPage /></AppLayout></Route>
    <Route><Redirect to="/" /></Route>
  </Switch></Suspense>
}

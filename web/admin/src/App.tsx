import { lazy, Suspense, useEffect } from 'react'
import { Redirect, Route, Switch, useLocation } from 'wouter'
import { AppLayout } from '@/components/AppLayout'
import { SystemStatusPage } from '@/pages/SystemStatusPage'
import { useAuthStore } from '@/store/authStore'
import { NAV } from '@/nav'
const ModelsPage = lazy(() => import('@/pages/ModelsPage').then(module => ({ default: module.ModelsPage })))
const PromptsPage = lazy(() => import('@/pages/PromptsPage').then(module => ({ default: module.PromptsPage })))
const AgentsPage = lazy(() => import('@/pages/AgentsPage').then(module => ({ default: module.AgentsPage })))
const ChatPage = lazy(() => import('@/pages/ChatPage').then(module => ({ default: module.ChatPage })))
const ConversationsPage = lazy(() => import('@/pages/ConversationsPage').then(module => ({ default: module.ConversationsPage })))
const WorkspacesPage = lazy(() => import('@/pages/WorkspacesPage').then(module => ({ default: module.WorkspacesPage })))
const UsersPage = lazy(() => import('@/pages/UsersPage').then(module => ({ default: module.UsersPage })))
const LoginPage = lazy(() => import('@/pages/LoginPage').then(module => ({ default: module.LoginPage })))

export default function App() {
  const [location] = useLocation()
  useEffect(() => {
    const label = location === '/login' ? '登录' : NAV.find(item => item.path === location)?.label ?? '系统状态'
    document.title = `${label} · AsterFlow`
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
    <Route path="/workspaces"><AppLayout><WorkspacesPage /></AppLayout></Route>
    <Route path="/models"><AppLayout><ModelsPage /></AppLayout></Route>
    <Route path="/prompts"><AppLayout><PromptsPage /></AppLayout></Route>
    <Route path="/agents"><AppLayout><AgentsPage /></AppLayout></Route>
    <Route path="/chat"><AppLayout><ChatPage /></AppLayout></Route>
    <Route path="/conversations"><AppLayout><ConversationsPage /></AppLayout></Route>
    <Route path="/users"><AppLayout><UsersPage /></AppLayout></Route>
    <Route path="/"><AppLayout><SystemStatusPage /></AppLayout></Route>
    <Route><Redirect to="/" /></Route>
  </Switch></Suspense>
}

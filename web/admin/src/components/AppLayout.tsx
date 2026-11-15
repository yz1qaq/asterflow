import type { ReactNode } from 'react'
import { Avatar, Breadcrumb, Button, Layout, Tag, Typography } from 'antd'
import { LogoutOutlined } from '@ant-design/icons'
import { Link, useLocation } from 'wouter'
import { useQueryClient } from '@tanstack/react-query'
import { useAuthStore } from '@/store/authStore'
import { NAV } from '@/nav'
import { Brand } from './Brand'

export function AppLayout({ children }: { children: ReactNode }) {
  const [location, navigate] = useLocation()
  const { token, user, logout } = useAuthStore()
  const queryClient = useQueryClient()
  const navigation = NAV.filter(item => !item.adminOnly || user?.role === 'admin')
  const selected = NAV.find(item => item.path === location)

  function onLogout() {
    logout()
    queryClient.clear()
    navigate('/login')
  }

  return <Layout className="app-shell">
    <a href="#main-content" className="skip-link">跳转到主要内容</a>
    <header className="app-header">
      <Link href="/" className="app-home" aria-label="AsterFlow 首页"><Brand /></Link>
      <nav className="top-navigation" aria-label="主导航">
        {navigation.map(item => <Link key={item.key} href={item.path} className="navigation-link"
          aria-current={location === item.path ? 'page' : undefined}>
          <span className="navigation-icon" aria-hidden="true">{item.icon}</span><span>{item.label}</span>
        </Link>)}
      </nav>
      <div className="header-actions">
        <Tag className="environment-tag" bordered={false}>本地开发</Tag>
        {token && user ? <>
          <Avatar className="account-avatar" size={32} aria-hidden="true">{Array.from(user.name || user.email)[0]?.toUpperCase()}</Avatar>
          <div className="account-identity">
            <Typography.Text className="header-user-name" title={user.email}>{user.name || user.email}</Typography.Text>
            <span className="header-user-role">{user.role === 'admin' ? '管理员' : '普通用户'}</span>
          </div>
          <Button className="logout-button" aria-label="退出登录" type="text" icon={<LogoutOutlined />} onClick={onLogout}>退出登录</Button>
        </> : <Button type="primary" onClick={() => navigate('/login')}>登录</Button>}
      </div>
    </header>
    <main className="app-content" id="main-content" tabIndex={-1}>
      <Breadcrumb className="app-breadcrumb" items={[{ title: '控制台' }, { title: selected?.label ?? '系统状态' }]} />
      <div className="app-page-surface">{children}</div>
      <footer className="content-footer"><span>AsterFlow</span><span>本地开发环境</span></footer>
    </main>
  </Layout>
}

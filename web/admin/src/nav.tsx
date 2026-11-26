import type { ReactNode } from 'react'
import { DashboardOutlined, TeamOutlined, ApartmentOutlined, ApiOutlined, FileTextOutlined, RobotOutlined, CommentOutlined, ToolOutlined, HistoryOutlined } from '@ant-design/icons'
interface NavItem { key: string; path: string; label: string; icon: ReactNode; adminOnly?: boolean }
export const NAV: NavItem[] = [
  { key: 'system', path: '/', label: '系统状态', icon: <DashboardOutlined /> },
  { key: 'workspaces', path: '/workspaces', label: '工作空间', icon: <ApartmentOutlined /> },
  { key: 'models', path: '/models', label: '模型管理', icon: <ApiOutlined /> },
  { key: 'prompts', path: '/prompts', label: '提示词管理', icon: <FileTextOutlined /> },
  { key: 'tools', path: '/tools', label: '工具管理', icon: <ToolOutlined /> },
  { key: 'agents', path: '/agents', label: 'Agent 管理', icon: <RobotOutlined /> },
  { key: 'chat', path: '/chat', label: '聊天', icon: <CommentOutlined /> },
  { key: 'conversations', path: '/conversations', label: '会话历史', icon: <HistoryOutlined /> },
  { key: 'users', path: '/users', label: '用户管理', icon: <TeamOutlined />, adminOnly: true },
]

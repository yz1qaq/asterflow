import { Alert, Button, Empty, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Redirect, useLocation, useSearchParams } from 'wouter'
import { getWorkspaceContext } from '@/api/iam'
import { getConversation, listConversations, type Conversation } from '@/api/conversations'
import { ConversationMessages, timeLabel } from '@/components/ConversationMessages'
import { isSessionValid, useAuthStore } from '@/store/authStore'

const statusColor: Record<string, string> = { active: 'processing', running: 'warning', completed: 'green', error: 'red' }

export function ConversationsPage() {
  const session = useAuthStore()
  const [, navigate] = useLocation()
  const [search, setSearch] = useSearchParams()
  const selectedID = search.get('conversation_id') || ''
  const userID = session.user?.id || ''
  const signedIn = Boolean(userID && isSessionValid(session))
  const workspaceID = session.workspaceId
  const context = useQuery({
    queryKey: ['workspace-context', userID, workspaceID ?? ''],
    enabled: signedIn && Boolean(workspaceID), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => getWorkspaceContext(workspaceID!, signal),
  })
  const conversations = useQuery({
    queryKey: ['conversations', userID, workspaceID ?? ''],
    enabled: signedIn && Boolean(workspaceID), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => listConversations(workspaceID!, signal),
  })
  const detail = useQuery({
    queryKey: ['conversation', userID, workspaceID ?? '', selectedID],
    enabled: signedIn && Boolean(workspaceID && selectedID), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => getConversation(workspaceID!, selectedID, signal),
  })

  const current = detail.data?.conversation

  if (!signedIn) return <Redirect to="/login" />
  if (!workspaceID) return <Empty description="请先选择工作空间"><Button type="primary" onClick={() => navigate('/workspaces')}>前往选择空间</Button></Empty>
  if (!context.isFetchedAfterMount || context.isPending) return <Skeleton active />
  if (context.isError) return <Alert role="alert" type="error" showIcon message="无法读取空间权限，请刷新或重新选择空间" />

  return <section aria-labelledby="conversations-heading">
    <div className="page-heading"><div><Typography.Title level={2} id="conversations-heading">会话历史</Typography.Title>
      <Typography.Paragraph className="page-description">查看已保存的消息和会话详情。</Typography.Paragraph></div></div>
    <div className="workspace-current model-workspace"><Space wrap>
      <Typography.Text strong>当前空间</Typography.Text>
      <Typography.Text className="workspace-id" copyable>{workspaceID}</Typography.Text>
      <Tag color="blue">空间角色：{context.data.workspace_role}</Tag></Space></div>
    {conversations.isError ? <Alert role="alert" type="error" showIcon message="无法读取会话，请刷新后重试" />
      : <Table<Conversation> rowKey="id" dataSource={conversations.data ?? []} loading={conversations.isFetching}
        pagination={{ pageSize: 10, hideOnSinglePage: true }} locale={{ emptyText: <Empty description="暂无会话" /> }}
        columns={[
          { title: '会话 / ID', render: (_, row) => <Space direction="vertical" size={2}>
            <Tag color={statusColor[row.status] ?? 'default'}>{row.status}</Tag>
            <Typography.Text className="workspace-id" copyable>{row.id}</Typography.Text></Space> },
          { title: '用户', dataIndex: 'user_id', render: (value: string) => <Typography.Text copyable>{value.slice(0, 8)}</Typography.Text> },
          { title: 'Agent 版本', dataIndex: 'agent_version_id', render: (value: string) => <Typography.Text copyable>{value.slice(0, 8)}</Typography.Text> },
          { title: '开始时间', dataIndex: 'started_at', width: 185, render: timeLabel },
          { title: '操作', width: 110, render: (_, row) => <Button size="small" type={selectedID === row.id ? 'primary' : 'default'}
            onClick={() => setSearch({ conversation_id: row.id })}>查看</Button> },
        ]} />}
    {selectedID && (detail.isError ? <Alert role="alert" type="error" showIcon message="无法读取会话消息" />
      : !detail.isFetchedAfterMount ? <Skeleton active />
      : current && <section className="prompt-detail" aria-label={`会话 ${current.id} 详情`}>
        <div className="model-section-heading"><div><Typography.Title level={4}>会话消息</Typography.Title>
          <Typography.Text copyable>{current.id}</Typography.Text></div></div>
        {detail.data?.messages.length
          ? <ConversationMessages messages={detail.data.messages} />
          : <Empty description="暂无已保存的消息" />}
      </section>)}
  </section>
}
